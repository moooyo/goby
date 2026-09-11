#!/usr/bin/env python3
"""Read-only operator guards for the dedicated M3e fixture, run via SSH only.

The trusted schema catalogs are read as immutable inputs. Database, service,
binary-replacement, and credential mutations are never exercised by this suite.
Schema25 through schema27 positive cases require actual generated catalogs in root-pinned
source manifests; no catalog object inventory or production digest is invented.
"""

from __future__ import annotations

import copy
import argparse
from contextlib import contextmanager, ExitStack
from decimal import Decimal
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import sys
import stat
import types
import unittest
from unittest.mock import Mock, patch

sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location("client_fixture_operator", Path(__file__).with_name("prepare-client-fixture.py"))
OP = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(OP)
SCHEMA25_OPTIONS = None


class ClientFixtureGuards(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.baselines = {version: OP.trusted_schema_baseline(version) for version in (23, 24)}
        options = SCHEMA25_OPTIONS
        if options is None:
            raise OP.FixtureError("The fixture guards require explicitly bound real schema25, schema26, and schema27 catalogs.")
        artifacts = OP.verify_schema_upgrade_sources(OP.WORK / "guard-unused-binary", 25,
            options.schema25_source, options.schema25_source_manifest_sha256, options.schema25_catalog_sha256)
        cls.binding = artifacts["schema25_binding"]
        cls.baselines[25] = OP.trusted_schema_baseline(25, cls.binding)
        cls.catalog25_bytes = OP.read(options.schema25_source / "internal/backuppg/catalogs/schema-25-postgresql-17.json", mode=0o644)
        artifacts26 = OP.verify_schema_upgrade_sources(OP.WORK / "guard-unused-binary", 26,
            options.schema26_source, options.schema26_source_manifest_sha256,
            schema26_catalog_sha256=options.schema26_catalog_sha256)
        cls.binding26 = artifacts26["schema26_binding"]
        cls.baselines[26] = OP.trusted_schema_baseline(26, cls.binding26)
        cls.catalog26_bytes = OP.read(options.schema26_source / "internal/backuppg/catalogs/schema-26-postgresql-17.json", mode=0o644)
        artifacts27 = OP.verify_schema_upgrade_sources(OP.WORK / "guard-unused-binary", 27,
            options.schema27_source, options.schema27_source_manifest_sha256,
            schema27_catalog_sha256=options.schema27_catalog_sha256)
        cls.binding27 = artifacts27["schema27_binding"]
        cls.baselines[27] = OP.trusted_schema_baseline(27, cls.binding27)
        cls.catalog27_bytes = OP.read(options.schema27_source / "internal/backuppg/catalogs/schema-27-postgresql-17.json", mode=0o644)

    def setUp(self):
        self.state = {"schema": 23, "phase": "ready", "admin_id": "a" * 32, "viewer_id": "b" * 32,
                      "server_id": "c" * 32, "runtime_sha256": "d" * 64, "browser_sha256": "e" * 64,
                      "schema25_source": copy.deepcopy(self.binding), "schema26_source": copy.deepcopy(self.binding26),
                      "schema27_source": copy.deepcopy(self.binding27)}

    def snapshot(self, version):
        baseline = self.baselines[version]
        columns = OP.baseline_table_columns(baseline)
        tables = {name: [] for name in columns}

        def row(table_name, **values):
            result = dict.fromkeys(columns[table_name])
            result.update(values)
            return result

        tables["users"] = [row("users", id=self.state[key + "_id"], name=key, password_hash="private-password-hash", has_password=True)
                           for key in ("admin", "viewer")]
        tables["server_settings"] = [row("server_settings", key="server_id", value=self.state["server_id"])]
        tables["schema_migrations"] = [row("schema_migrations", version=migration["version"], name=migration["name"],
            applied_at="2026-09-11T00:00:00+00:00" if migration["version"] < 24 else
                {24: "2026-09-11T01:00:30+00:00", 25: "2026-09-11T01:01:30+00:00",
                 26: "2026-09-11T01:02:30+00:00", 27: "2026-09-11T01:03:30+00:00"}[migration["version"]])
            for migration in baseline["migrations"]]
        relations = {item["name"]: {"oid": int(hashlib.sha256(item["name"].encode()).hexdigest()[:8], 16),
            "owner": OP.ROLE, "acl": None, "column_acl": []} for item in baseline["objects"] if item["kind"] == "relation"}
        database = {"metadata": {"captured_at": {23: "2026-09-11T01:00:00+00:00", 24: "2026-09-11T01:01:00+00:00",
                                                 25: "2026-09-11T01:02:00+00:00", 26: "2026-09-11T01:03:00+00:00", 27: "2026-09-11T01:04:00+00:00"}[version],
            "server_version_num": 170011, "database": OP.ROLE, "schemas": ["public"],
            "public_schema": {"oid": 2200, "owner": "pg_database_owner", "acl": None},
            "columns": copy.deepcopy(columns), "relations": relations},
            "catalog": copy.deepcopy(baseline["objects"]), "unsupported": False,
            "tables": tables, "sequences": {sequence["Name"]: {"last_value": 3, "is_called": True}
                for sequence in baseline["catalog"].get("Sequences", [])}}
        if version >= 26:
            tables["theme_owner_ids"] = [row("theme_owner_ids", id=1, item_id=None, virtual_root=True)]
            database["sequences"]["theme_owner_ids_id_seq"] = {"last_value": 1, "is_called": True}
        return {"schema": version, "database": database, "recovery": {"master.key": {"sha256": "f" * 64, "size": 32}},
                "runtime_sha256": self.state["runtime_sha256"], "browser_sha256": self.state["browser_sha256"]}

    def compare(self, before, after, source=23, target=24):
        def verified_baseline(version, binding=None):
            OP.require(type(version) is int and version in self.baselines,
                       "The comparison requested an unverified schema baseline.")
            expected_binding = self.binding if version == 25 else self.binding26 if version == 26 else self.binding27 if version == 27 else None
            OP.require(OP.equal_json(binding, expected_binding),
                       "The comparison changed the explicitly verified source binding.")
            return copy.deepcopy(self.baselines[version])

        # setUpClass verifies the real catalog bytes and source membership once.
        # Only snapshot comparisons reuse them; source/catalog drift guards
        # retain the original production verifier outside this local context.
        with patch.object(OP, "trusted_schema_baseline", side_effect=verified_baseline):
            OP.compare_preservation_snapshots(before, after, source, target, self.state)

    def test_comparison_baselines_are_isolated_and_source_verification_remains_live(self):
        original = OP.trusted_schema_baseline

        def inspect_comparison(*_args):
            baseline = OP.trusted_schema_baseline(26, self.binding26)
            self.assertIsNot(baseline, self.baselines[26])
            baseline["objects"].clear()
            self.assertTrue(self.baselines[26]["objects"])
            for version, binding in ((True, None), (27, None), (25, None),
                                     (26, dict(self.binding26, source_manifest_sha256="0" * 64))):
                with self.subTest(version=version, binding=binding), self.assertRaises(OP.FixtureError):
                    OP.trusted_schema_baseline(version, binding)

        with patch.object(OP, "compare_preservation_snapshots", side_effect=inspect_comparison):
            self.compare(None, None)
        self.assertIs(OP.trusted_schema_baseline, original)
        for changed in ("catalog", "migration"):
            with self.subTest(changed=changed), self.source26_environment() as (source, model), \
                 patch.object(OP, "trusted_schema_baseline", wraps=original) as verifier:
                if changed == "catalog":
                    model["catalog26_bytes"] = b"{}"
                else:
                    model["actual"]["internal/database/migrations/" + OP.MIGRATION_26_NAME] = "0" * 64
                with self.assertRaises(OP.FixtureError):
                    self.verify_source26(source)
                verifier.assert_called_once()

    def test_only_supported_schema_transitions_are_accepted(self):
        for source, target in ((23, 23), (23, 24), (24, 24), (24, 25), (25, 25), (25, 26), (26, 26), (26, 27), (27, 27)):
            self.assertEqual(OP.target_schema_version({"schema": source}, target), target)
        self.assertEqual(OP.target_schema_version({"schema": 24}), 24)
        self.assertEqual(OP.target_schema_version({"schema": 25}), 25)
        self.assertEqual(OP.target_schema_version({"schema": 26}), 26)
        for source, target in ((24, 23), (23, 25), (25, 24), (24, 26), (26, 25), (25, 27), (27, 26), (22, 23), (True, 24), (23, True), ("23", 24), (23, "24")):
            with self.subTest(source=source, target=target), self.assertRaises(OP.FixtureError):
                OP.target_schema_version({"schema": source}, target)

    def test_exact_schema23_to_24_delta_is_accepted(self):
        before, after = self.snapshot(23), self.snapshot(24)
        self.compare(before, after)
        self.assertEqual(len(before["database"]["tables"]), 29)
        self.assertEqual(len(after["database"]["tables"]), 30)

    def test_same_schema_upgrade_retains_preferences(self):
        for version in (23, 24, 25, 26, 27):
            before = self.snapshot(version)
            if version >= 24:
                before["database"]["tables"]["user_settings"] = [{"user_id": self.state["viewer_id"],
                    "settings": {"layout": "grid", "ratio": Decimal("0.123456789012345678901")},
                    "updated_at": "2026-09-11T01:00:30+00:00"}]
            after = copy.deepcopy(before)
            after["database"]["metadata"]["captured_at"] = "2026-09-11T02:00:00+00:00"
            self.compare(before, after, version, version)

    def music_snapshots(self):
        before, after = self.snapshot(24), self.snapshot(25)
        columns = before["database"]["metadata"]["columns"]["item_metadata_state"]
        row = dict(dict.fromkeys(columns), item_id="owned-music-item", revision=7,
                   automatic={"Name": "Existing album"}, overrides={"Overview": "Keep this"},
                   effective={"Name": "Existing album", "Overview": "Keep this"}, source_key="unchanged-source",
                   locked_values={}, last_edited_by=self.state["viewer_id"], updated_at="old-timestamp")
        before["database"]["tables"]["item_metadata_state"] = [row]
        after["database"]["tables"]["item_metadata_state"] = [dict(row, music_source={})]
        columns = before["database"]["metadata"]["columns"]["item_entities"]
        credit = dict(dict.fromkeys(columns), item_id="owned-music-item", entity_id=1, position=0,
                      display_name="Original credit", role="original role", credit_type="Legacy Credit " + "x" * 6000, sort_order=0)
        before["database"]["tables"]["item_entities"] = [credit]
        after["database"]["tables"]["item_entities"] = [dict(credit, credit_group=0)]
        after["database"]["metadata"]["relations"]["item_entities_pkey"]["oid"] += 1
        return before, after

    def test_schema24_to_25_adds_only_exact_defaults_and_rebuilds_only_owned_index(self):
        before, after = self.music_snapshots()
        self.compare(before, after, 24, 25)
        self.assertEqual(len(before["database"]["tables"]), 30)
        self.assertEqual(len(after["database"]["tables"]), 30)
        self.assertEqual(OP.baseline_table_columns(self.baselines[25])["item_metadata_state"],
                         OP.baseline_table_columns(self.baselines[24])["item_metadata_state"] + ["music_source"])
        self.assertEqual(OP.baseline_table_columns(self.baselines[25])["item_entities"],
                         OP.baseline_table_columns(self.baselines[24])["item_entities"] + ["credit_group"])

    def test_music_migration_preserves_every_old_column_and_all_other_rows(self):
        before, after = self.music_snapshots()
        for table in OP.SCHEMA_25_NEW_COLUMNS:
            for column in before["database"]["metadata"]["columns"][table]:
                changed = copy.deepcopy(after)
                changed["database"]["tables"][table][0][column] = {"unowned": "changed"}
                with self.subTest(table=table, column=column), self.assertRaises(OP.FixtureError):
                    self.compare(before, changed, 24, 25)
        for table in before["database"]["tables"]:
            if table in OP.SCHEMA_25_NEW_COLUMNS:
                continue
            changed = copy.deepcopy(after)
            changed["database"]["tables"][table].append(dict.fromkeys(changed["database"]["metadata"]["columns"][table]))
            with self.subTest(table=table), self.assertRaises((OP.FixtureError, TypeError)):
                self.compare(before, changed, 24, 25)

    def test_music_migration_rejects_nonempty_or_untyped_defaults_and_unowned_column(self):
        before, after = self.music_snapshots()
        for value in (None, [], "{}", False, 0, {"Artists": []}):
            changed = copy.deepcopy(after)
            changed["database"]["tables"]["item_metadata_state"][0]["music_source"] = value
            with self.subTest(value=value), self.assertRaises(OP.FixtureError):
                self.compare(before, changed, 24, 25)
        for value in (None, False, True, "0", 1, 2):
            changed = copy.deepcopy(after)
            changed["database"]["tables"]["item_entities"][0]["credit_group"] = value
            with self.subTest(credit_group=value), self.assertRaises(OP.FixtureError):
                self.compare(before, changed, 24, 25)
        changed = copy.deepcopy(after)
        changed["database"]["tables"]["item_metadata_state"][0]["unowned"] = {}
        with self.assertRaises(OP.FixtureError):
            self.compare(before, changed, 24, 25)
        for name, field, value in (("item_metadata_state", "oid", 1), ("item_entities", "oid", 1),
                                   ("item_entities_pkey", "owner", "postgres"), ("item_entities_pkey", "acl", ["public=rw"])):
            changed = copy.deepcopy(after)
            changed["database"]["metadata"]["relations"][name][field] = value
            with self.subTest(name=name, field=field), self.assertRaises(OP.FixtureError):
                self.compare(before, changed, 24, 25)

    def test_same_schema25_preserves_nonempty_music_source_and_index_identity(self):
        _, before = self.music_snapshots()
        before["database"]["tables"]["item_metadata_state"][0]["music_source"] = {"Artists": ["Exact Artist"]}
        before["database"]["tables"]["item_entities"][0]["credit_group"] = 1
        self.compare(before, copy.deepcopy(before), 25, 25)
        for kind in ("source", "credit_group", "index"):
            after = copy.deepcopy(before)
            if kind == "source":
                after["database"]["tables"]["item_metadata_state"][0]["music_source"] = {}
            elif kind == "credit_group":
                after["database"]["tables"]["item_entities"][0]["credit_group"] = 0
            else:
                after["database"]["metadata"]["relations"]["item_entities_pkey"]["oid"] += 1
            with self.subTest(kind=kind), self.assertRaises(OP.FixtureError):
                self.compare(before, after, 25, 25)

    def test_schema25_migration_history_and_window_are_exact(self):
        before, after = self.music_snapshots()
        for position, field, value in ((23, "name", "0024_changed.sql"), (24, "name", "0025_changed.sql"),
                                      (24, "applied_at", "2026-09-11T01:00:59+00:00"),
                                      (24, "applied_at", "2026-09-11T01:02:01+00:00")):
            changed = copy.deepcopy(after)
            changed["database"]["tables"]["schema_migrations"][position][field] = value
            with self.subTest(position=position, field=field), self.assertRaises(OP.FixtureError):
                self.compare(before, changed, 24, 25)

    def snapshot_row(self, snapshot, table, **values):
        return dict(dict.fromkeys(snapshot["database"]["metadata"]["columns"][table]), **values)

    def theme_snapshots(self):
        before, after = self.snapshot(25), self.snapshot(26)
        tables = before["database"]["tables"]
        tables["libraries"] = [self.snapshot_row(before, "libraries", id="theme-library", name="Theme library", collection_type="mixed")]
        tables["library_roots"] = [self.snapshot_row(before, "library_roots", id=root, library_id="theme-library",
            path="/owned/" + root, allowed_path="/owned", relative_path=root) for root in ("theme-root-a", "theme-root-b")]
        layouts = [("theme-owner", "theme-root-a", "Film", "MusicAlbum", True, None),
                   ("theme-song", "theme-root-a", "Film/theme.mp3", "Audio", False, "theme-owner"),
                   ("theme-video", "theme-root-a", "Film/BACKDROPS/clip.mp4", "Video", False, "theme-owner"),
                   ("ordinary-item", "theme-root-b", "Film/not-theme.mp3", "Audio", False, None)]
        tables["items"] = [self.snapshot_row(before, "items", id=item, library_id="theme-library", root_id=root,
            parent_id=parent, relative_path=path, path="/owned/" + root + "/" + path,
            name=item, sort_name=item, type=kind, is_folder=folder) for item, root, path, kind, folder, parent in layouts]
        tables["item_metadata_state"] = [self.snapshot_row(before, "item_metadata_state", item_id=item["id"],
            automatic={"Name": item["name"]}, effective={"Name": item["name"], "Opaque": Decimal("9007199254740993.125")},
            source_key={}, overrides={"Overview": "Preserve this"}, locked_values={}, revision=7,
            music_source={"Version": 1, "Artists": ["Existing artist"], "AlbumArtists": ["Existing album artist"]})
            for item in tables["items"]]
        tables["user_settings"] = [self.snapshot_row(before, "user_settings", user_id=self.state["viewer_id"],
            settings={"Theme": "dark", "Exact": Decimal("0.123456789012345678901")}, updated_at="2026-09-11T01:00:30+00:00")]
        for table, rows in tables.items():
            if table != "schema_migrations":
                after["database"]["tables"][table] = copy.deepcopy(rows)
        after["database"]["tables"]["theme_owner_ids"] = [{"id": 1, "item_id": None, "virtual_root": True}] + [
            {"id": index + 2, "item_id": item["id"], "virtual_root": False}
            for index, item in enumerate(sorted(tables["items"], key=lambda item: item["id"]))]
        after["database"]["tables"]["theme_reserved_paths"] = [
            {"root_id": "theme-root-a", "relative_path": "Film/BACKDROPS", "is_directory": True},
            {"root_id": "theme-root-a", "relative_path": "Film/theme.mp3", "is_directory": False}]
        after["database"]["sequences"]["theme_owner_ids_id_seq"] = {"last_value": len(tables["items"]) + 1, "is_called": True}
        return before, after

    def test_schema25_to_26_adds_only_theme_tables_sequence_and_path_markers(self):
        before, after = self.theme_snapshots()
        self.compare(before, after, 25, 26)
        self.assertEqual(len(before["database"]["tables"]), 30)
        self.assertEqual(len(after["database"]["tables"]), 33)
        self.assertEqual(set(after["database"]["tables"]) - set(before["database"]["tables"]),
                         {"theme_owner_ids", "theme_reserved_paths", "item_theme_resources"})
        self.assertEqual(set(after["database"]["sequences"]) - set(before["database"]["sequences"]), {"theme_owner_ids_id_seq"})
        self.assertEqual(after["database"]["tables"]["item_theme_resources"], [])
        for table, columns in before["database"]["metadata"]["columns"].items():
            self.assertEqual(after["database"]["metadata"]["columns"][table], columns)

    def test_theme_migration_preserves_all_old_rows_columns_sequences_and_private_files(self):
        before, after = self.theme_snapshots()
        for table, rows in before["database"]["tables"].items():
            if table == "schema_migrations":
                continue
            for column in before["database"]["metadata"]["columns"][table]:
                changed = copy.deepcopy(after)
                target_rows = changed["database"]["tables"][table]
                if not target_rows:
                    target_rows.append(self.snapshot_row(changed, table))
                target_rows[0][column] = {"unowned": "changed"}
                with self.subTest(table=table, column=column), self.assertRaises((OP.FixtureError, TypeError)):
                    self.compare(before, changed, 25, 26)
        for relation in before["database"]["metadata"]["relations"]:
            for field, value in (("oid", 1), ("owner", "postgres"), ("acl", ["public=rw"]),
                                  ("column_acl", [{"name": "id", "acl": ["public=r"]}])):
                changed = copy.deepcopy(after)
                changed["database"]["metadata"]["relations"][relation][field] = value
                with self.subTest(relation=relation, field=field), self.assertRaises(OP.FixtureError):
                    self.compare(before, changed, 25, 26)
        for sequence in before["database"]["sequences"]:
            for field, value in (("last_value", 999), ("is_called", False)):
                changed = copy.deepcopy(after)
                changed["database"]["sequences"][sequence][field] = value
                with self.subTest(sequence=sequence, field=field), self.assertRaises(OP.FixtureError):
                    self.compare(before, changed, 25, 26)
        for key in ("runtime_sha256", "browser_sha256", "recovery"):
            changed = copy.deepcopy(after)
            changed[key] = {} if key == "recovery" else "0" * 64
            with self.subTest(private=key), self.assertRaises(OP.FixtureError):
                self.compare(before, changed, 25, 26)
        for position, field, value in ((24, "name", "0025_changed.sql"), (25, "name", "0026_changed.sql"),
                                      (25, "applied_at", "2026-09-11T01:01:59+00:00"),
                                      (25, "applied_at", "2026-09-11T01:03:01+00:00")):
            changed = copy.deepcopy(after)
            changed["database"]["tables"]["schema_migrations"][position][field] = value
            with self.subTest(history_position=position, field=field), self.assertRaises(OP.FixtureError):
                self.compare(before, changed, 25, 26)

    def test_theme_marker_derivation_uses_only_canonical_ascii_layouts_and_audio_extensions(self):
        before, _ = self.theme_snapshots()
        tables = before["database"]["tables"]
        tables["items"] = []
        expected = [{"root_id": "theme-root-a", "relative_path": "Literal%_/ThEmE-MuSiC", "is_directory": True},
                    {"root_id": "theme-root-a", "relative_path": "Nested/BACKDROPS", "is_directory": True}]
        cases = [("Literal%_/ThEmE-MuSiC/deep/song.flac", False), ("Nested/BACKDROPS", True),
                 ("Nested/BACKDROPS/clip.mp4", False), ("theme.mp3", True), ("theme.mp3/track.mp3", False),
                 ("theme.mp4", False), ("theme.mkv", False), ("theme.mp3.extra", False),
                 ("theme-mu\u017fic/track.mp3", False), ("bac\u212adrops/clip.mp4", False),
                 ("/Absolute/theme-music/song.mp3", False), ("C:/Drive/backdrops/clip.mp4", False),
                 ("c:theme.mp3", False), ("Back\\theme.mp3", False), ("Dot/./theme.mp3", False),
                 ("Parent/../backdrops/clip.mp4", False), ("Empty//theme.mp3", False), ("Trail/theme-music/", True), ("", False)]
        for extension in ("mp3", "flac", "m4a", "aac", "ogg", "opus", "wav", "wma", "aiff", "aif", "alac", "ape", "mka"):
            path = "Extensions/TheMe." + extension.upper()
            cases.append((path, False))
            expected.append({"root_id": "theme-root-a", "relative_path": path, "is_directory": False})
        for index, (path, folder) in enumerate(cases):
            tables["items"].append(self.snapshot_row(before, "items", id="layout-" + str(index), library_id="theme-library",
                root_id="theme-root-a", relative_path=path, type="MusicAlbum" if folder else "Audio", is_folder=folder))
        tables["items"].append(self.snapshot_row(before, "items", id="unrooted", library_id="theme-library",
            root_id=None, relative_path="Unrooted/theme.mp3", type="Audio", is_folder=False))
        self.assertEqual(OP.expected_theme_reserved_paths(tables), sorted(expected, key=lambda row: (row["root_id"], row["relative_path"])))

    def test_theme_migration_requires_exact_markers_complete_owners_and_no_inferred_resources(self):
        before, after = self.theme_snapshots()
        for kind in ("missing-root", "missing-item", "orphan", "duplicate-id", "duplicate-item", "false-is-not-zero",
                     "missing-marker", "extra-marker", "wrong-marker-root", "wrong-marker-kind", "inferred-resource"):
            changed = copy.deepcopy(after)
            tables = changed["database"]["tables"]
            owners, markers = tables["theme_owner_ids"], tables["theme_reserved_paths"]
            if kind == "missing-root":
                owners.pop(0)
            elif kind == "missing-item":
                owners.pop()
            elif kind == "orphan":
                owners[-1]["item_id"] = "unowned-item"
            elif kind == "duplicate-id":
                owners[-1]["id"] = owners[1]["id"]
            elif kind == "duplicate-item":
                owners[-1]["item_id"] = owners[1]["item_id"]
            elif kind == "false-is-not-zero":
                owners[0]["id"] = True
            elif kind == "missing-marker":
                markers.pop()
            elif kind == "extra-marker":
                markers.append({"root_id": "theme-root-b", "relative_path": "Unobserved/theme.mp3", "is_directory": False})
            elif kind == "wrong-marker-root":
                markers[0]["root_id"] = "theme-root-b"
            elif kind == "wrong-marker-kind":
                markers[0]["is_directory"] = False
            else:
                tables["item_theme_resources"] = [{"resource_item_id": "theme-song", "owner_item_id": "theme-owner", "kind": "song", "active": True}]
            with self.subTest(kind=kind), self.assertRaises(OP.FixtureError):
                self.compare(before, changed, 25, 26)

    def test_same_schema26_preserves_nonempty_theme_state_and_inactive_owner_moves(self):
        _, snapshot = self.theme_snapshots()
        tables = snapshot["database"]["tables"]
        tables["item_theme_resources"] = [
            {"resource_item_id": "theme-song", "owner_item_id": "theme-owner", "kind": "song", "active": True},
            {"resource_item_id": "theme-video", "owner_item_id": "theme-owner", "kind": "video", "active": False}]
        self.compare(snapshot, copy.deepcopy(snapshot), 26, 26)
        OP.validate_theme_state(tables)
        moved = copy.deepcopy(snapshot)
        for resource in moved["database"]["tables"]["item_theme_resources"]:
            resource["active"] = False
        owner = next(item for item in moved["database"]["tables"]["items"] if item["id"] == "theme-owner")
        owner["root_id"] = "theme-root-b"
        moved["database"]["tables"]["theme_reserved_paths"].append(
            {"root_id": "theme-root-b", "relative_path": "Film", "is_directory": True})
        self.compare(moved, copy.deepcopy(moved), 26, 26)
        for kind in ("active-moved-owner", "wrong-parent", "wrong-library", "wrong-resource-type", "missing-resource-root"):
            changed = copy.deepcopy(moved)
            changed_tables = changed["database"]["tables"]
            resource = next(item for item in changed_tables["items"] if item["id"] == "theme-song")
            if kind == "active-moved-owner":
                changed_tables["item_theme_resources"][0]["active"] = True
            elif kind == "wrong-parent":
                resource["parent_id"] = "ordinary-item"
            elif kind == "wrong-library":
                resource["library_id"] = "different-library"
            elif kind == "wrong-resource-type":
                resource["type"] = "Video"
            else:
                resource["root_id"] = None
            with self.subTest(kind=kind), self.assertRaises(OP.FixtureError):
                OP.validate_theme_state(changed_tables)
        for table in ("theme_owner_ids", "theme_reserved_paths", "item_theme_resources"):
            changed = copy.deepcopy(snapshot)
            changed["database"]["tables"][table].pop()
            with self.subTest(removed=table), self.assertRaises(OP.FixtureError):
                self.compare(snapshot, changed, 26, 26)

    def test_schema26_summary_exposes_only_theme_counts_and_digests(self):
        _, snapshot = self.theme_snapshots()
        snapshot["database"]["tables"]["item_theme_resources"] = [
            {"resource_item_id": "theme-song", "owner_item_id": "theme-owner", "kind": "song", "active": True}]
        summary = OP.preservation_summary(snapshot)
        self.assertEqual(summary["table_count"], 33)
        self.assertEqual(set(summary["theme"]), {"owner_count", "virtual_root_count", "mapped_item_count", "reserved_path_count",
            "resource_count", "active_resource_count", "owner_rows_sha256", "reserved_paths_sha256", "resource_rows_sha256"})
        self.assertEqual({key: value for key, value in summary["theme"].items() if not key.endswith("sha256")},
            {"owner_count": 5, "virtual_root_count": 1, "mapped_item_count": 4, "reserved_path_count": 2,
             "resource_count": 1, "active_resource_count": 1})
        for key in ("owner_rows_sha256", "reserved_paths_sha256", "resource_rows_sha256"):
            self.assertRegex(summary["theme"][key], r"^[0-9a-f]{64}$")
        encoded = OP.canonical_json(summary).decode()
        for secret in ("theme-owner", "Film/theme.mp3", "private-password-hash"):
            self.assertNotIn(secret, encoded)
        self.assertNotIn("theme", OP.preservation_summary(self.snapshot(25)))

    @contextmanager
    def source25_environment(self):
        source = OP.WORK / "source-attempt-25"
        files = {"internal/database/migrations/" + row["name"]: row["sha256"] for row in self.baselines[25]["migrations"]}
        files.update({f"internal/backuppg/catalogs/schema-{version}-postgresql-17.json": OP.SCHEMA_ARTIFACTS[version][1]
                      for version in (23, 24)})
        files["internal/backuppg/catalogs/schema-25-postgresql-17.json"] = self.binding["catalog_sha256"]
        model = {"files": files, "actual": dict(files), "catalog_bytes": self.catalog25_bytes,
                 "migration_names": [row["name"] for row in self.baselines[25]["migrations"]],
                 "catalog_names": [f"schema-{version}-postgresql-17.json" for version in (23, 24, 25)]}
        original_read = OP.read
        def read(path, **kwargs):
            if path == source / "internal/backuppg/catalogs/schema-25-postgresql-17.json":
                return model["catalog_bytes"]
            return original_read(path, **kwargs)
        def glob(path, _pattern):
            names = model["migration_names"] if path.name == "migrations" else model["catalog_names"]
            return [path / name for name in names]
        with patch.object(OP, "source_manifest", return_value={"marker": "goby-client-backup-source-m3e-v1", "files": files}), \
             patch.object(OP, "read", side_effect=read), patch.object(Path, "glob", side_effect=glob, autospec=True), \
             patch.object(OP, "sha", side_effect=lambda path, **_kwargs: model["actual"].get(str(path.relative_to(source)))):
            yield source, model

    def test_schema25_sources_require_all_actual_migrations_and_exact_catalog_membership(self):
        with self.source25_environment() as (source, model):
            artifacts = OP.verify_schema_upgrade_sources(source / "goby", 25, source, "a" * 64, self.binding["catalog_sha256"])
            self.assertEqual(artifacts["schema25_binding"]["migration_25_sha256"], self.baselines[25]["migrations"][-1]["sha256"])
            for name in (OP.MIGRATION_25_NAME, OP.MIGRATION_24_NAME):
                model["migration_names"].remove(name)
                with self.subTest(missing=name), self.assertRaises(OP.FixtureError):
                    OP.verify_schema_upgrade_sources(source / "goby", 25, source, "a" * 64, self.binding["catalog_sha256"])
                model["migration_names"].append(name)
            model["migration_names"].append("0026_unowned.sql")
            with self.assertRaises(OP.FixtureError):
                OP.verify_schema_upgrade_sources(source / "goby", 25, source, "a" * 64, self.binding["catalog_sha256"])
            model["migration_names"].pop()
            model["catalog_names"].append("schema-26-postgresql-17.json")
            with self.assertRaises(OP.FixtureError):
                OP.verify_schema_upgrade_sources(source / "goby", 25, source, "a" * 64, self.binding["catalog_sha256"])

    def test_schema25_sources_reject_wrong_catalog_or_historical_and_new_migration_hashes(self):
        with self.source25_environment() as (source, model):
            for relative in ("internal/backuppg/catalogs/schema-25-postgresql-17.json",
                             "internal/backuppg/catalogs/schema-24-postgresql-17.json",
                             "internal/backuppg/catalogs/schema-23-postgresql-17.json",
                             "internal/database/migrations/" + OP.MIGRATION_25_NAME,
                             "internal/database/migrations/" + OP.MIGRATION_24_NAME):
                previous = model["files"][relative]
                model["files"][relative] = "0" * 64
                with self.subTest(relative=relative), self.assertRaises(OP.FixtureError):
                    OP.verify_schema_upgrade_sources(source / "goby", 25, source, "a" * 64, self.binding["catalog_sha256"])
                model["files"][relative] = previous
            with self.assertRaises(OP.FixtureError):
                OP.verify_schema_upgrade_sources(source / "goby", 25, source, "a" * 64, "0" * 64)
            for relative in ("internal/database/migrations/" + OP.MIGRATION_24_NAME,
                             "internal/database/migrations/" + OP.MIGRATION_25_NAME):
                previous = model["actual"][relative]
                model["actual"][relative] = "0" * 64
                with self.subTest(changed_bytes=relative), self.assertRaises(OP.FixtureError):
                    OP.verify_schema_upgrade_sources(source / "goby", 25, source, "a" * 64, self.binding["catalog_sha256"])
                model["actual"][relative] = previous
            model["catalog_bytes"] = b'{}'
            with self.assertRaises(OP.FixtureError):
                OP.verify_schema_upgrade_sources(source / "goby", 25, source, "a" * 64, self.binding["catalog_sha256"])

    def test_schema25_never_accepts_unbound_catalogs_or_unknown_receipt_fields(self):
        with patch.object(OP, "source_manifest") as manifest:
            for source, digest in ((None, None), (OP.WORK / "source-attempt-25", None)):
                with self.subTest(source=source), self.assertRaises(OP.FixtureError):
                    OP.verify_schema_upgrade_sources(OP.WORK / "unused", 25, source, "a" * 64 if source else None, digest)
            manifest.assert_not_called()
        for binding in (None, dict(self.binding, schema=26), dict(self.binding, unexpected=True)):
            with self.subTest(binding=binding), self.assertRaises(OP.FixtureError):
                OP.schema25_binding({"schema25_source": binding})

    @contextmanager
    def source26_environment(self):
        source = OP.WORK / "source-attempt-26"
        files = {"internal/database/migrations/" + row["name"]: row["sha256"] for row in self.baselines[26]["migrations"]}
        files.update({f"internal/backuppg/catalogs/schema-{version}-postgresql-17.json": OP.SCHEMA_ARTIFACTS[version][1]
                      for version in (23, 24)})
        files["internal/backuppg/catalogs/schema-25-postgresql-17.json"] = self.binding["catalog_sha256"]
        files["internal/backuppg/catalogs/schema-26-postgresql-17.json"] = self.binding26["catalog_sha256"]
        model = {"files": files, "actual": dict(files), "catalog25_bytes": self.catalog25_bytes, "catalog26_bytes": self.catalog26_bytes,
                 "migration_names": [row["name"] for row in self.baselines[26]["migrations"]],
                 "catalog_names": [f"schema-{version}-postgresql-17.json" for version in (23, 24, 25, 26)]}
        original_read = OP.read
        def read(path, **kwargs):
            for version in (25, 26):
                if path == source / f"internal/backuppg/catalogs/schema-{version}-postgresql-17.json":
                    return model[f"catalog{version}_bytes"]
            return original_read(path, **kwargs)
        def glob(path, _pattern):
            names = model["migration_names"] if path.name == "migrations" else model["catalog_names"]
            return [path / name for name in names]
        with patch.object(OP, "source_manifest", return_value={"marker": "goby-client-backup-source-m3e-v1", "files": files}), \
             patch.object(OP, "read", side_effect=read), patch.object(Path, "glob", side_effect=glob, autospec=True), \
             patch.object(OP, "sha", side_effect=lambda path, **_kwargs: model["actual"].get(str(path.relative_to(source)))):
            yield source, model

    def verify_source26(self, source, catalog_sha256=None):
        return OP.verify_schema_upgrade_sources(source / "goby", 26, source, "a" * 64,
            schema26_catalog_sha256=self.binding26["catalog_sha256"] if catalog_sha256 is None else catalog_sha256)

    def test_schema26_sources_require_actual_prefix_and_exact_catalog_membership(self):
        with self.source26_environment() as (source, model):
            artifacts = self.verify_source26(source)
            self.assertEqual(artifacts["schema26_binding"]["migration_26_sha256"], self.baselines[26]["migrations"][-1]["sha256"])
            for name in (self.baselines[26]["migrations"][-1]["name"], OP.MIGRATION_25_NAME):
                model["migration_names"].remove(name)
                with self.subTest(missing=name), self.assertRaises(OP.FixtureError):
                    self.verify_source26(source)
                model["migration_names"].append(name)
            for collection, name in (("migration_names", "0027_unowned.sql"), ("catalog_names", "schema-27-postgresql-17.json")):
                model[collection].append(name)
                with self.subTest(extra=name), self.assertRaises(OP.FixtureError):
                    self.verify_source26(source)
                model[collection].pop()

    def test_schema26_sources_reject_changed_historical_or_new_bytes(self):
        with self.source26_environment() as (source, model):
            for relative in ("internal/backuppg/catalogs/schema-26-postgresql-17.json",
                             "internal/backuppg/catalogs/schema-25-postgresql-17.json",
                             "internal/database/migrations/" + self.baselines[26]["migrations"][-1]["name"],
                             "internal/database/migrations/" + OP.MIGRATION_25_NAME):
                mappings = ("files",) if relative.endswith("schema-26-postgresql-17.json") else ("files", "actual")
                for mapping in mappings:
                    previous = model[mapping][relative]
                    model[mapping][relative] = "0" * 64
                    with self.subTest(relative=relative, mapping=mapping), self.assertRaises(OP.FixtureError):
                        self.verify_source26(source)
                    model[mapping][relative] = previous
            with self.assertRaises(OP.FixtureError):
                self.verify_source26(source, "0" * 64)
            model["catalog26_bytes"] = b"{}"
            with self.assertRaises(OP.FixtureError):
                self.verify_source26(source)

    def test_schema26_requires_an_explicit_binding_without_unknown_receipt_fields(self):
        with patch.object(OP, "source_manifest") as manifest:
            for source, digest in ((None, None), (OP.WORK / "source-attempt-26", None)):
                with self.subTest(source=source), self.assertRaises(OP.FixtureError):
                    OP.verify_schema_upgrade_sources(OP.WORK / "unused", 26, source, "a" * 64 if source else None,
                        schema26_catalog_sha256=digest)
            manifest.assert_not_called()
        for binding in (None, dict(self.binding26, schema=25), dict(self.binding26, schema=True), dict(self.binding26, unexpected=True)):
            with self.subTest(binding=binding), self.assertRaises(OP.FixtureError):
                OP.schema26_binding({"schema26_source": binding})

    def test_schema26_catalog_delta_preserves_the_published_schema25(self):
        OP.validate_schema26_delta(self.baselines[25], self.baselines[26])
        for kind in ("old-column", "old-object", "old-migration", "extra-table", "extra-sequence", "missing-trigger"):
            changed = copy.deepcopy(self.baselines[26])
            if kind == "old-column":
                next(table for table in changed["catalog"]["Tables"] if table["Name"] == "items")["Columns"].append("unowned")
            elif kind == "old-object":
                next(row for row in changed["objects"] if row["kind"] == "function" and
                     row["name"].startswith("sync_catalog_item_entities("))["value"]["security_definer"] = True
            elif kind == "old-migration":
                changed["migrations"][24]["sha256"] = "0" * 64
            elif kind == "extra-table":
                duplicate = copy.deepcopy(changed["catalog"]["Tables"][-1])
                duplicate["Name"] = "unowned_theme_table"
                changed["catalog"]["Tables"].append(duplicate)
            elif kind == "extra-sequence":
                duplicate = copy.deepcopy(changed["catalog"]["Sequences"][-1])
                duplicate["Name"] = "unowned_theme_sequence"
                changed["catalog"]["Sequences"].append(duplicate)
            else:
                changed["objects"] = [row for row in changed["objects"] if not
                    (row["kind"] == "trigger" and row["name"] == "items.items_assign_theme_owner_id")]
            with self.subTest(kind=kind), self.assertRaises(OP.FixtureError):
                OP.validate_schema26_delta(self.baselines[25], changed)

    @contextmanager
    def product_report_environment(self, target=26):
        source = OP.WORK / f"source-attempt-{target}"
        report_path = OP.WORK / "client-backup-run-20260911_010203_012345abcdef" / "report.json"
        binary = source / "goby"
        built = b"x" * ((1 << 20) + 1)
        packages = ["github.com/moooyo/goby/internal/" + name for name in (
            "activity", "artwork", "backupformat", "backuppg", "backupstore", "config", "database", "diagnostics", "events",
            "identity", "library", "lifecycle", "media", "metadata", "playback", "recovery", "recoverycontrol", "recoverydb",
            "server", "settings", "subtitle", "tasks", "transcode")] + ["github.com/moooyo/goby/cmd/goby"]
        document = {"marker": "goby-client-backup-pair-m3e-v1", "status": "passed", "mode": "full", "schema": 26,
            "catalog_sha256": OP.CATALOG_26_SHA, "source": str(source), "source_manifest_sha256": "a" * 64, "unit_exit": 0,
            "run_id": report_path.parent.name.removeprefix("client-backup-run-"),
            "cleanup": {key: True for key in ("unit_terminal", "hba_restored_exactly", "goby_backup_m3e_source_removed",
                "goby_backup_m3e_target_removed", "preexisting_catalog_unchanged", "receipt_saved")},
            "tests": {"failures": 0, "skips": 0, "top_level_passes": 6, "passed": [
                "TestThemeOwnersMigrationPreservesSchema25RowsSequencesAndRollback",
                "TestThemeReservedPathsMigrationPreservesSchema25AndReservesOnlyCanonicalLayouts",
                "TestPostgreSQLThemeRestorePreservesInactiveClassificationAndRetriesSemanticFinalizer",
                "TestPostgreSQLThemeRestoreRejectsSemanticallyInvalidRealArchives",
                "TestRecoveryManagerNativeWorkflow", "TestRecoveryDatabaseStoreIntegration"]},
            "packages": packages, "binary": {"path": str(report_path.parent / "tmp/goby-linux-amd64"), "sha256": "b" * 64, "bytes": len(built)}}
        model = {"document": document, "final_sha": None}
        if target == 27:
            document.update(schema=27, catalog_sha256=OP.CATALOG_27_SHA)
            document["tests"]["passed"] += sorted(OP.PRODUCT_27_REQUIRED_TESTS)
            document["tests"]["top_level_passes"] = len(document["tests"]["passed"])
        raw = lambda: OP.canonical_json(model["document"])
        with patch.object(OP, "directory"), patch.object(OP, "canonical", return_value=types.SimpleNamespace(st_gid=0)), \
             patch.object(OP, "read", side_effect=lambda *_args, **_kwargs: raw()), \
             patch.object(OP, "sha", side_effect=lambda *_args, **_kwargs: model["final_sha"] or hashlib.sha256(raw()).hexdigest()), \
             patch.object(OP, "verify_upgrade_input", return_value=(built, {"device": 1, "inode": 2})) as inputs, \
             patch.object(OP, "verify_complete_product_source") as complete, \
             patch.object(OP, "schema27_support_proof", return_value={"guard_count": 108}), \
             patch.object(OP, "source_manifest", return_value={"files": {
                 "internal/database/migrations/" + OP.MIGRATION_27_NAME: OP.MIGRATION_27_SHA,
                 "internal/backuppg/catalogs/schema-27-postgresql-17.json": OP.CATALOG_27_SHA}}):
            def invoke():
                return OP.verify_product_upgrade(binary, "b" * 64, source, "a" * 64, report_path, hashlib.sha256(raw()).hexdigest(), target)
            yield model, invoke, inputs, complete, report_path

    @contextmanager
    def source27_environment(self):
        source = OP.WORK / "source-attempt-31"
        files = {"internal/database/migrations/" + row["name"]: row["sha256"] for row in self.baselines[27]["migrations"]}
        for version in (23, 24, 25, 26, 27):
            files[f"internal/backuppg/catalogs/schema-{version}-postgresql-17.json"] = (
                OP.SCHEMA_ARTIFACTS[version][1] if version < 25 else getattr(OP, f"CATALOG_{version}_SHA"))
        model = {"files": files, "actual": dict(files), "catalog25_bytes": self.catalog25_bytes,
                 "catalog26_bytes": self.catalog26_bytes, "catalog27_bytes": self.catalog27_bytes,
                 "migration_names": [row["name"] for row in self.baselines[27]["migrations"]],
                 "catalog_names": [f"schema-{version}-postgresql-17.json" for version in (23, 24, 25, 26, 27)]}
        original_read = OP.read
        def read(path, **kwargs):
            for version in (25, 26, 27):
                if path == source / f"internal/backuppg/catalogs/schema-{version}-postgresql-17.json":
                    return model[f"catalog{version}_bytes"]
            return original_read(path, **kwargs)
        def glob(path, _pattern):
            return [path / name for name in model["migration_names" if path.name == "migrations" else "catalog_names"]]
        with patch.object(OP, "source_manifest", return_value={"files": files}), patch.object(OP, "read", side_effect=read), \
             patch.object(Path, "glob", side_effect=glob, autospec=True), \
             patch.object(OP, "sha", side_effect=lambda path, **_kwargs: model["actual"].get(str(path.relative_to(source)))):
            yield source, model

    def test_schema27_sources_require_exact_new_and_historical_catalog_migration_bytes(self):
        with self.source27_environment() as (source, model):
            invoke = lambda: OP.verify_schema_upgrade_sources(source / "goby", 27, source, "a" * 64, schema27_catalog_sha256=OP.CATALOG_27_SHA)
            self.assertEqual(invoke()["schema27_binding"]["migration_27_sha256"], OP.MIGRATION_27_SHA)
            for name in ("internal/database/migrations/" + OP.MIGRATION_27_NAME,
                         "internal/database/migrations/" + OP.MIGRATION_26_NAME,
                         "internal/backuppg/catalogs/schema-26-postgresql-17.json"):
                old = model["files"][name]
                model["files"][name] = "0" * 64
                with self.subTest(name=name), self.assertRaises(OP.FixtureError):
                    invoke()
                model["files"][name] = old
            model["catalog_names"].append("schema-28-postgresql-17.json")
            with self.assertRaises(OP.FixtureError):
                invoke()
        for binding in (None, dict(self.binding27, schema=True), dict(self.binding27, migration_27_sha256="0" * 64), dict(self.binding27, extra=True)):
            with self.assertRaises(OP.FixtureError):
                OP.schema27_binding({"schema27_source": binding})

    def test_schema27_delta_preserves_every_old_definition_and_has_no_sequence_change(self):
        OP.validate_schema27_delta(self.baselines[26], self.baselines[27])
        for kind in ("old-table", "old-object", "sequence", "foreign", "history"):
            changed = copy.deepcopy(self.baselines[27])
            if kind == "old-table": changed["catalog"]["Tables"][0]["Columns"].append("unowned")
            elif kind == "old-object": changed["objects"][0]["value"] = {}
            elif kind == "sequence": changed["catalog"]["Sequences"][0]["Increment"] = 2
            elif kind == "foreign": changed["catalog"]["Constraints"].pop()
            else: changed["migrations"][0]["sha256"] = "0" * 64
            with self.subTest(kind=kind), self.assertRaises(OP.FixtureError):
                OP.validate_schema27_delta(self.baselines[26], changed)

    def test_schema26_to27_preserves_current_history_preferences_and_all_old_relations(self):
        before, after = self.snapshot(26), self.snapshot(27)
        for table, count in (("play_sessions", 22), ("sessions", 51), ("user_item_data", 5)):
            rows = [dict(dict.fromkeys(before["database"]["metadata"]["columns"][table]), id=f"owned-{number}") for number in range(count)]
            # Set only actual declared columns; some history tables use a composite key.
            rows = [{key: value for key, value in row.items() if key in before["database"]["metadata"]["columns"][table]} for row in rows]
            before["database"]["tables"][table] = copy.deepcopy(rows)
            after["database"]["tables"][table] = copy.deepcopy(rows)
        self.compare(before, after, 26, 27)
        for kind in ("history", "extra", "owner", "sequence", "relation"):
            changed = copy.deepcopy(after)
            if kind == "history": changed["database"]["tables"]["play_sessions"].pop()
            elif kind == "extra": changed["database"]["tables"]["item_extra_resources"].append(dict.fromkeys(changed["database"]["metadata"]["columns"]["item_extra_resources"]))
            elif kind == "owner": changed["database"]["tables"]["theme_owner_ids"][0]["id"] = 2
            elif kind == "sequence": changed["database"]["sequences"]["theme_owner_ids_id_seq"]["last_value"] = 2
            else: changed["database"]["metadata"]["relations"]["users"]["oid"] += 1
            with self.subTest(kind=kind), self.assertRaises(OP.FixtureError):
                self.compare(before, changed, 26, 27)
        self.assertEqual(after["database"]["tables"]["client_playback_references"], [])

    def test_schema27_fixture_scope_rejects_new_reservations_and_active_themes(self):
        tables = self.snapshot(26)["database"]["tables"]
        tables["libraries"] = [{"id": "library", "collection_type": "movies"}]
        tables["library_roots"] = [{"id": "root", "library_id": "library"}]
        def item(path):
            return {"id": "item", "root_id": "root", "library_id": "library", "relative_path": path, "is_folder": False}
        for path in ("Movie/featurettes/a.mp4", "Movie/DELETED SCENES/a.mp4", "Movie/trailers/nested/a.mp4"):
            tables["items"] = [item(path)]
            with self.assertRaises(OP.FixtureError):
                OP.validate_empty_extra_scope(tables)
        for path in ("Movie/backdrops/featurettes/a.mp4", "Movie/featurettes-lookalike/a.mp4", "Movie/movie.mp4"):
            tables["items"] = [item(path)]
            OP.validate_empty_extra_scope(tables)
        tables["item_theme_resources"] = [{"active": True}]
        with self.assertRaises(OP.FixtureError):
            OP.validate_empty_extra_scope(tables)

    def test_schema27_product_requires_new_cases_and_exact_support_runner_guards(self):
        with self.product_report_environment(27) as (model, invoke, _inputs, _complete, _path):
            self.assertEqual(invoke()["schema27_support"], {"guard_count": 108})
            model["document"]["tests"]["passed"].remove(next(iter(OP.PRODUCT_27_REQUIRED_TESTS)))
            model["document"]["tests"]["top_level_passes"] -= 1
            with self.assertRaises(OP.FixtureError):
                invoke()
        with patch.object(OP, "directory"), patch.object(OP, "canonical", return_value=types.SimpleNamespace(st_mode=0o100600)), \
             patch.object(OP, "sha", side_effect=lambda path, **_: OP.SCHEMA_27_SUPPORT_HASHES[path.name]):
            self.assertEqual(OP.schema27_support_proof()["guard_count"], 108)
        with patch.object(OP, "directory"), patch.object(OP, "canonical", return_value=types.SimpleNamespace(st_mode=0o100600)), patch.object(OP, "sha", return_value="0" * 64):
            with self.assertRaises(OP.FixtureError):
                OP.schema27_support_proof()

    def test_schema26_product_report_requires_complete_source_run_and_exact_binary(self):
        # All report I/O is mocked; the real schema catalog is loaded in setUpClass.
        with self.product_report_environment() as (model, invoke, inputs, complete, report_path):
            result = invoke()
            self.assertEqual(result, {"report_path": str(report_path), "report_sha256": hashlib.sha256(OP.canonical_json(model["document"])).hexdigest()})
            self.assertEqual(inputs.call_count, 2)
            complete.assert_called_once_with(OP.WORK / "source-attempt-26", "a" * 64)
            original = copy.deepcopy(model["document"])
            for kind in ("catalog-only", "failed-run", "wrong-schema", "wrong-catalog", "wrong-source", "wrong-manifest", "boolean-exit",
                         "incomplete-cleanup", "failed-tests", "skipped-tests", "boolean-count", "missing-package",
                         "substituted-package", "missing-required-test", "wrong-binary-path", "wrong-binary-sha", "wrong-binary-size", "wrong-run"):
                document = copy.deepcopy(original)
                if kind == "catalog-only": document["mode"] = "catalog"
                elif kind == "failed-run": document["status"] = "failed"
                elif kind == "wrong-schema": document["schema"] = 25
                elif kind == "wrong-catalog": document["catalog_sha256"] = OP.CATALOG_25_SHA
                elif kind == "wrong-source": document["source"] = str(OP.WORK / "source-attempt-22")
                elif kind == "wrong-manifest": document["source_manifest_sha256"] = "c" * 64
                elif kind == "boolean-exit": document["unit_exit"] = False
                elif kind == "incomplete-cleanup": document["cleanup"]["receipt_saved"] = False
                elif kind == "failed-tests": document["tests"]["failures"] = 1
                elif kind == "skipped-tests": document["tests"]["skips"] = 1
                elif kind == "boolean-count": document["tests"]["top_level_passes"] = True
                elif kind == "missing-package": document["packages"].remove("github.com/moooyo/goby/internal/recoverydb")
                elif kind == "substituted-package": document["packages"][document["packages"].index("github.com/moooyo/goby/internal/server")] = "github.com/moooyo/goby/internal/unverified"
                elif kind == "missing-required-test":
                    document["tests"]["passed"][0] = "UnrelatedPassingTest"
                elif kind == "wrong-binary-path": document["binary"]["path"] = str(OP.WORK / "unowned/goby")
                elif kind == "wrong-binary-sha": document["binary"]["sha256"] = "c" * 64
                elif kind == "wrong-binary-size": document["binary"]["bytes"] += 1
                else: document["run_id"] = "different-run"
                model["document"] = document
                with self.subTest(kind=kind), self.assertRaises(OP.FixtureError):
                    invoke()
            model["document"] = original
            model["final_sha"] = "0" * 64
            with self.assertRaises(OP.FixtureError):
                invoke()

    def test_product_upgrade_without_full_report_never_schedules_mutations(self):
        for version, target in ((25, 26), (26, 26), (26, 27), (27, 27)):
            state = dict(self.state, schema=version, binary_sha256="1" * 64, process={"pid": 123})
            original = copy.deepcopy(state)
            api = Mock()
            api.request.side_effect = lambda route: {"Status": "ready"} if route == "/readyz" else {"Id": state["server_id"]}
            binding = self.binding26 if target == 26 else self.binding27
            source = OP.WORK / f"source-attempt-{target}"
            with self.subTest(version=version, target=target), patch.object(OP, "verify_fixture_directories"), patch.object(OP, "verify_database"), \
                 patch.object(OP, "verify_service", return_value=state["process"]), patch.object(OP, "NativeAPI", return_value=api), \
                 patch.object(OP, "verify_upgrade_input", return_value=(b"candidate", {})), \
                 patch.object(OP, "verify_schema_upgrade_sources", return_value={"catalog_sha256": binding["catalog_sha256"]}), \
                 patch.object(OP, "command") as commands, patch.object(OP, "create") as creates, patch.object(OP, "save_state") as saves:
                with self.assertRaises(OP.FixtureError):
                    OP.upgrade_fixture(state, source / "goby", "2" * 64, target, source=source, source_manifest_sha256="a" * 64,
                        **{f"schema{target}_catalog_sha256": binding["catalog_sha256"]})
                commands.assert_not_called()
                creates.assert_not_called()
                saves.assert_not_called()
                self.assertEqual(state, original)

    def test_source_manifest_rejects_extra_fields_and_unowned_paths(self):
        source = OP.WORK / "source-attempt-25"
        documents = [{"marker": "goby-client-backup-source-m3e-v1", "files": {}, "extra": True},
                     {"marker": "goby-client-backup-source-m3e-v1", "files": {"../unowned": "a" * 64}},
                     {"marker": "goby-client-backup-source-m3e-v1", "files": {"/unowned": "a" * 64}}]
        for document in documents:
            raw = OP.canonical_json(document)
            with self.subTest(document=document), patch.object(Path, "resolve", return_value=source), \
                 patch.object(OP, "canonical", return_value=types.SimpleNamespace(st_mode=stat.S_IFDIR | 0o700, st_uid=0)), \
                 patch.object(OP, "read", return_value=raw), self.assertRaises(OP.FixtureError):
                OP.source_manifest(source, hashlib.sha256(raw).hexdigest())

    def test_schema25_catalog_rejects_unowned_columns_objects_and_history(self):
        for kind in ("column", "object", "history", "function-authority", "constraint-authority", "index-method"):
            changed = copy.deepcopy(self.baselines[25])
            if kind == "column":
                next(table for table in changed["catalog"]["Tables"] if table["Name"] == "users")["Columns"].append("unowned")
            elif kind == "object":
                changed["objects"].append({"kind": "function", "name": "unowned()", "value": {}})
            elif kind == "history":
                changed["migrations"][0]["sha256"] = "0" * 64
            elif kind == "function-authority":
                next(row for row in changed["objects"] if row["kind"] == "function" and
                     row["name"].startswith("sync_catalog_item_entities("))["value"]["security_definer"] = True
            elif kind == "constraint-authority":
                next(row for row in changed["objects"] if row["kind"] == "constraint" and
                     row["name"] == "item_entities.item_entities_pkey")["value"]["deferrable"] = True
            else:
                next(row for row in changed["objects"] if row["kind"] == "index" and
                     row["name"] == "item_entities_pkey")["value"]["method"] = "hash"
            with self.subTest(kind=kind), self.assertRaises(OP.FixtureError):
                OP.validate_schema25_delta(self.baselines[24], changed)

    def test_every_old_table_is_compared_without_exemptions(self):
        before, after = self.snapshot(23), self.snapshot(24)
        for name in before["database"]["tables"]:
            changed = copy.deepcopy(after)
            changed["database"]["tables"][name].append(dict.fromkeys(changed["database"]["metadata"]["columns"][name]))
            with self.subTest(table=name), self.assertRaises((OP.FixtureError, TypeError)):
                self.compare(before, changed)

    def test_old_column_facts_cannot_disappear_even_for_empty_tables(self):
        before, after = self.snapshot(23), self.snapshot(24)
        after["database"]["metadata"]["columns"]["application_keys"].pop()
        with self.assertRaises(OP.FixtureError):
            self.compare(before, after)

    def test_password_and_primitive_types_cannot_change(self):
        before, after = self.snapshot(23), self.snapshot(24)
        for field, value in (("password_hash", "different-hash"), ("has_password", 1)):
            changed = copy.deepcopy(after)
            changed["database"]["tables"]["users"][0][field] = value
            with self.subTest(field=field), self.assertRaises(OP.FixtureError):
                self.compare(before, changed)

    def test_sessions_audit_tasks_and_application_credentials_cannot_be_removed(self):
        for table in ("sessions", "activity_entries", "task_runs", "task_triggers", "application_keys", "application_key_clients"):
            before, after = self.snapshot(23), self.snapshot(24)
            before["database"]["tables"][table].append(dict.fromkeys(before["database"]["metadata"]["columns"][table]))
            with self.subTest(table=table), self.assertRaises(OP.FixtureError):
                self.compare(before, after)

    def test_new_preferences_table_must_be_empty_during_migration(self):
        before, after = self.snapshot(23), self.snapshot(24)
        after["database"]["tables"]["user_settings"] = [{"user_id": self.state["viewer_id"], "settings": {}, "updated_at": None}]
        with self.assertRaises(OP.FixtureError):
            self.compare(before, after)

    def test_migration_history_and_window_are_exact(self):
        before, after = self.snapshot(23), self.snapshot(24)
        for position, field, value in ((0, "applied_at", "2026-09-11T00:00:01+00:00"),
                                      (-1, "name", "0024_untrusted.sql"),
                                      (-1, "applied_at", "2026-09-11T00:59:59+00:00"),
                                      (-1, "applied_at", "2026-09-11T01:01:01+00:00")):
            changed = copy.deepcopy(after)
            changed["database"]["tables"]["schema_migrations"][position][field] = value
            with self.subTest(position=position, field=field), self.assertRaises(OP.FixtureError):
                self.compare(before, changed)

    def test_catalog_and_unsupported_objects_are_checked(self):
        before, after = self.snapshot(23), self.snapshot(24)
        changed = copy.deepcopy(after)
        changed["database"]["unsupported"] = True
        with self.assertRaises(OP.FixtureError):
            self.compare(before, changed)
        changed = copy.deepcopy(after)
        changed["database"]["catalog"].pop()
        with self.assertRaises(OP.FixtureError):
            self.compare(before, changed)

    def test_relation_identity_privileges_and_sequences_are_preserved(self):
        before, after = self.snapshot(23), self.snapshot(24)
        for field, value in (("oid", 1), ("acl", ["public=rw"]), ("owner", "postgres")):
            changed = copy.deepcopy(after)
            changed["database"]["metadata"]["relations"]["users"][field] = value
            with self.subTest(field=field), self.assertRaises(OP.FixtureError):
                self.compare(before, changed)
        changed = copy.deepcopy(after)
        sequence = next(iter(changed["database"]["sequences"]))
        changed["database"]["sequences"][sequence]["last_value"] += 1
        with self.assertRaises(OP.FixtureError):
            self.compare(before, changed)

    def test_credentials_master_and_recovery_are_preserved(self):
        before, after = self.snapshot(23), self.snapshot(24)
        for key in ("runtime_sha256", "browser_sha256", "recovery"):
            changed = copy.deepcopy(after)
            changed[key] = {} if key == "recovery" else "0" * 64
            with self.subTest(key=key), self.assertRaises(OP.FixtureError):
                self.compare(before, changed)

    def test_exact_numeric_json_roundtrip_and_comparison(self):
        left = OP.precise_json('{"number":0.123456789012345678901}')
        right = OP.precise_json('{"number":0.123456789012345678902}')
        self.assertFalse(OP.equal_json(left, right))
        self.assertEqual(OP.canonical_json(left), b'{"number":0.123456789012345678901}')
        self.assertFalse(OP.equal_json(left, {"number": "0.123456789012345678901"}))
        huge = '{"number":' + "9" * 5000 + '}'
        self.assertEqual(OP.canonical_json(OP.precise_json(huge)).decode(), huge)
        before = self.snapshot(24)
        before["database"]["tables"]["user_settings"] = [{"user_id": self.state["viewer_id"], "settings": left, "updated_at": None}]
        after = copy.deepcopy(before)
        after["database"]["tables"]["user_settings"][0]["settings"] = right
        with self.assertRaises(OP.FixtureError):
            self.compare(before, after, 24, 24)

    def test_psql_records_preserve_unicode_line_separators_and_exact_numbers(self):
        records = [
            '{"section":"metadata","value":{}}', '{"section":"catalog","value":[]}',
            '{"section":"unsupported","value":false}',
            '{"section":"table","name":"example","value":[{"text":"a\u0085b\u2028c\u2029d","number":0.123456789012345678901}]}',
        ]
        with patch.object(OP, "verify_database"), patch.object(OP, "postgres", return_value="\n".join(records)) as query:
            result = OP.full_database_snapshot(self.state)
        self.assertEqual(result["tables"]["example"][0]["text"], "a\u0085b\u2028c\u2029d")
        self.assertEqual(result["tables"]["example"][0]["number"], Decimal("0.123456789012345678901"))
        sql = query.call_args.args[0]
        self.assertIn("BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY", sql)
        self.assertEqual(sql.count("\\gexec"), 2)
        self.assertIn("d.objid=c.oid", OP.CATALOG_OBJECTS_SQL)
        self.assertIn("FROM pg_type", sql)

    def test_public_summary_contains_only_hashes_and_counts(self):
        snapshot = self.snapshot(24)
        summary = OP.preservation_summary(snapshot)
        encoded = OP.canonical_json(summary).decode()
        self.assertNotIn("private-password-hash", encoded)
        self.assertNotIn("password_hash", encoded)
        self.assertNotIn(self.state["viewer_id"], encoded)
        self.assertEqual(summary["table_count"], 30)

    def test_input_path_escape_is_rejected_before_filesystem_access(self):
        with patch.object(OP, "canonical") as filesystem:
            for path in (Path("relative"), Path("/opt/goby-dev/goby"), OP.WORK / "../outside/goby"):
                with self.subTest(path=path), self.assertRaises(OP.FixtureError):
                    OP.verify_upgrade_input(path, "a" * 64)
            filesystem.assert_not_called()

    def test_interrupted_upgrade_is_not_restarted_or_rolled_back(self):
        for source, target in ((23, 24), (24, 25), (25, 25), (25, 26), (26, 26)):
            state = dict(self.state, schema=source, phase="upgrading", upgrade={"phase": "replace_requested"})
            with self.subTest(source=source, target=target), patch.object(OP, "command") as command, \
                 patch.object(OP, "verify_fixture_directories") as directories:
                with self.assertRaises(OP.FixtureError):
                    OP.upgrade_fixture(state, OP.WORK / "source/goby", "a" * 64, target)
                command.assert_not_called()
                directories.assert_not_called()

    def test_candidate_start_does_not_commit_ready_binary_pid_or_schema(self):
        state = dict(self.state, binary_sha256="1" * 64, process={"pid": 123}, upgrade={"phase": "start_requested"})
        original = copy.deepcopy(state)
        observed = {"pid": 456, "start_ticks": 789, "boot_id": "test-boot"}
        with patch.object(OP, "require_candidate_stopped"), patch.object(OP, "verify_database"), \
             patch.object(OP, "verify_service", side_effect=[None, observed]), \
             patch.object(OP, "command", side_effect=["", "owned listener"]), patch.object(OP, "save_state"):
            candidate = OP.start_upgrade_candidate(state, "2" * 64, {"device": 1, "inode": 2})
        for key in ("binary_sha256", "process", "schema"):
            self.assertEqual(state[key], original[key])
        self.assertEqual(candidate["process"], observed)
        self.assertEqual(state["upgrade"]["new_process"], observed)

    def test_pending_credentials_and_files_are_not_adopted(self):
        with patch.object(OP, "create") as create, patch.object(OP, "managed_directory") as directory:
            with self.assertRaises(OP.FixtureError):
                OP.credentials({"credentials_pending": True})
            for key in ("binary_pending", "unit_pending"):
                with self.assertRaises(OP.FixtureError):
                    OP.prepare_files({key: True})
            create.assert_not_called()
            directory.assert_not_called()

    @contextmanager
    def av_environment(self, *, fail_path=None, creation_error=False, drift=None, occupied=False):
        """Model files and native responses without any filesystem or HTTP write."""
        self.state.update(schema=24, stage="complete", tag=OP.MARKER + ":" + "1" * 32,
                          process={"pid": 123, "start_ticks": 456, "boot_id": "owned-boot"},
                          cluster={"port": 15432, "system_identifier": "owned-cluster"}, database_oid=12, role_oid=13,
                          media={"manifest": "unchanged"}, libraries={key: {"id": key + "-id"} for key in ("movies", "tv", "music")})
        before = self.snapshot(24)
        for key, row in zip(("admin", "viewer"), before["database"]["tables"]["users"]):
            row.update(name=OP.ACCOUNTS[key], is_administrator=key == "admin", is_disabled=False)
        columns = before["database"]["metadata"]["columns"]
        before["database"]["tables"]["libraries"] = [dict(dict.fromkeys(columns["libraries"]), id=key + "-id")
                                                        for key in ("movies", "tv", "music")]
        before["database"]["tables"]["user_settings"] = [{"user_id": self.state["viewer_id"],
            "settings": {"genreLimitOnDetails": "1", "exact": Decimal("0.123456789012345678901")}, "updated_at": "baseline-time"}]
        before["database"]["tables"]["user_item_data"] = [dict(dict.fromkeys(columns["user_item_data"]), user_id=self.state["viewer_id"])]
        user = {"Id": "f" * 32, "Name": OP.AV_NAME, "IsAdministrator": False, "IsDisabled": False,
                "HasPassword": True, "CreatedAt": "2026-09-11T01:00:00Z"}
        after = copy.deepcopy(before)
        new_row = dict(after["database"]["tables"]["users"][1], id=user["Id"], name=OP.AV_NAME)
        after["database"]["tables"]["users"].append(new_row)
        if drift:
            drift(after)
        browser = {"marker": OP.MARKER, "base_url": OP.PUBLIC, "direct_url": f"http://127.0.0.1:{OP.PORT}",
                   "admin": {"username": OP.ACCOUNTS["admin"], "password": "8" * 48},
                   "viewer": {"username": OP.ACCOUNTS["viewer"], "password": "9" * 48}}
        files, directories, calls, saved, publications = {OP.BROWSER: browser}, set(), [], [], []
        if occupied:
            files[OP.AV_BROWSER] = {"unrelated": True}

        def create(path, content, mode=0o600):
            if path == fail_path:
                raise OSError("Simulated publication failure.")
            self.assertNotIn(path, files)
            self.assertEqual(mode, 0o600)
            files[path] = copy.deepcopy(content)
            publications.append(path)

        def digest(path, **_kwargs):
            if path == OP.BROWSER:
                return self.state["browser_sha256"]
            if path == OP.RUNTIME:
                return self.state["runtime_sha256"]
            return hashlib.sha256(OP.canonical_json(files[path])).hexdigest()

        def snapshot(state, version=None):
            result = copy.deepcopy(after if state.get("added_viewer", {}).get("phase") == "complete" else before)
            result["added_viewer_credentials"] = OP.added_viewer_credentials(state)
            return result

        def native(api, route, method="GET", body=None, expected=(200,), admin=False):
            api.requests += 1
            calls.append((method, route))
            code, cookie = 200, None
            if route == "/readyz":
                data = {"Status": "ready"}
            elif route == "/emby/System/Info/Public":
                data = {"Id": self.state["server_id"]}
            elif route == "/admin/v1/session" and method == "POST":
                data = {"User": {"Id": self.state["admin_id"], "Name": OP.ACCOUNTS["admin"],
                                "IsAdministrator": True, "IsDisabled": False}, "CSRFToken": "private-csrf"}
                cookie = "goby_session=private-cookie"
            elif route == "/admin/v1/users" and method == "GET":
                data = {"Items": [{"Id": self.state[key + "_id"], "Name": OP.ACCOUNTS[key], "IsAdministrator": key == "admin",
                                   "IsDisabled": False} for key in ("admin", "viewer")]}
            elif route == "/admin/v1/users" and method == "POST":
                self.assertEqual(self.state["added_viewer"]["phase"], "create_requested")
                self.assertIn(OP.AV_ROOT / "credentials.json", files)
                self.assertTrue(any(row.get("added_viewer", {}).get("phase") == "create_requested" for row in saved))
                if creation_error:
                    raise ConnectionError("Creation outcome is unknown.")
                code, data = 201, {"User": user}
            elif route == "/admin/v1/users/" + user["Id"]:
                data = {"User": dict(user, Policy={"EnableAllFolders": True, "EnableMediaPlayback": True})}
            elif route == "/admin/v1/session" and method == "DELETE":
                code, data = 204, None
            elif route == "/admin/v1/session" and method == "GET":
                code, data = 401, {"Error": {"Code": "authentication_required"}}
            else:
                raise AssertionError("An unexpected native operation escaped the test model.")
            self.assertIn(code, expected)
            raw = json.dumps(data).encode() if data is not None else b""
            api.received(route, method, code, raw, cookie)
            if cookie:
                api.cookie, api.csrf = cookie, data["CSRFToken"]
            return data

        with ExitStack() as stack:
            for name, replacement in {
                "verify_fixture_directories": Mock(), "verify_database": Mock(),
                "verify_service": Mock(return_value=self.state["process"]), "media_snapshot": Mock(return_value=self.state["media"]),
                "trusted_schema_baseline": lambda version, binding=None: copy.deepcopy(self.baselines[version]),
                "preservation_snapshot": snapshot, "full_database_snapshot": Mock(side_effect=lambda _state: copy.deepcopy(after["database"])),
                "recovery_snapshot": Mock(return_value=before["recovery"]), "directory": Mock(return_value={"device": 1, "inode": 2}),
                "create": create, "load": lambda path: copy.deepcopy(files[path]), "sha": digest,
                "exists": lambda path: path in files or path in directories,
                "save_state": lambda state: saved.append(copy.deepcopy(state)),
            }.items():
                stack.enter_context(patch.object(OP, name, replacement))
            stack.enter_context(patch.object(Path, "mkdir", lambda path, **_kwargs: directories.add(path)))
            stack.enter_context(patch.object(OP.NativeAPI, "request", native))
            yield {"files": files, "calls": calls, "saved": saved, "publications": publications,
                   "before": before, "after": after, "user": user, "digest": digest}

    def test_add_viewer_receipt_and_alias_are_required_for_the_third_user(self):
        with self.av_environment() as model:
            with self.assertRaises(OP.FixtureError):
                OP.validate_database_snapshot(model["after"]["database"], 24, self.state)
            OP.add_viewer(self.state)
            receipt = OP.added_viewer_receipt(self.state)
            self.assertEqual(receipt["user_id"], model["user"]["Id"])
            self.assertEqual(self.state["added_viewer"]["phase"], "complete")
            self.assertEqual(model["files"][OP.AV_BROWSER]["viewer"]["userId"], model["user"]["Id"])
            self.assertNotIn(OP.BROWSER, model["publications"])
            self.assertEqual(model["calls"].count(("POST", "/admin/v1/users")), 1)
            self.assertEqual(model["calls"][-2:], [("DELETE", "/admin/v1/session"), ("GET", "/admin/v1/session")])
            OP.validate_database_snapshot(model["after"]["database"], 24, self.state)
            calls = list(model["calls"])
            OP.add_viewer(self.state)
            self.assertEqual(model["calls"], calls, "A complete repeated mode must not create another native session.")

    def test_unknown_creation_and_failed_publications_never_resend_or_adopt(self):
        for failure in ("unknown", "creation-response", "receipt"):
            with self.subTest(failure=failure):
                self.setUp()
                path = {"creation-response": OP.AV_ROOT / "creation-response.json", "receipt": OP.AV_RECEIPT}.get(failure)
                with self.av_environment(fail_path=path, creation_error=failure == "unknown") as model:
                    with self.assertRaises((ConnectionError, OSError)):
                        OP.add_viewer(self.state)
                    self.assertNotEqual(self.state["added_viewer"]["phase"], "complete")
                    self.assertTrue(self.state["added_viewer"]["admin_session_revoked"])
                    self.assertEqual(model["calls"].count(("POST", "/admin/v1/users")), 1)
                    calls = list(model["calls"])
                    with self.assertRaises(OP.FixtureError):
                        OP.add_viewer(copy.deepcopy(model["saved"][-1]))
                    self.assertEqual(model["calls"], calls)

    def test_unknown_alias_is_rejected_before_login_or_credential_creation(self):
        with self.av_environment(occupied=True) as model:
            with self.assertRaises(OP.FixtureError):
                OP.add_viewer(self.state)
            self.assertEqual(model["calls"], [])
            self.assertEqual(model["publications"], [])
            self.assertNotIn("added_viewer", self.state)

    def test_account_append_preserves_complete_old_rows_preferences_and_playback(self):
        mutations = [lambda snap: snap["database"]["tables"]["users"][0].update(password_hash="changed"),
                     lambda snap: snap["database"]["tables"]["users"][1].update(configuration={"changed": True}),
                     lambda snap: snap["database"]["tables"]["user_settings"][0]["settings"].update(genreLimitOnDetails="2"),
                     lambda snap: snap["database"]["tables"]["user_settings"][0].update(updated_at="changed"),
                     lambda snap: snap["database"]["tables"]["user_item_data"][0].update(user_id="changed")]
        for index, mutate in enumerate(mutations):
            with self.subTest(index=index):
                self.setUp()
                with self.av_environment(drift=mutate) as model:
                    with self.assertRaises(OP.FixtureError):
                        OP.add_viewer(self.state)
                    self.assertNotIn(OP.AV_RECEIPT, model["files"])
                    self.assertNotEqual(self.state["added_viewer"]["phase"], "complete")

    def test_receipt_alias_credentials_and_fixture_binding_are_not_interchangeable(self):
        for field in ("receipt", "alias", "credential", "fixture", "source_process"):
            with self.subTest(field=field):
                self.setUp()
                with self.av_environment() as model:
                    OP.add_viewer(self.state)
                    if field == "receipt":
                        model["files"][OP.AV_RECEIPT]["user_id"] = "0" * 32
                    elif field == "alias":
                        model["files"][OP.AV_BROWSER]["viewer"]["password"] = "0" * 48
                    elif field == "credential":
                        model["files"][OP.AV_ROOT / "credentials.json"]["password"] = "0" * 48
                    elif field == "fixture":
                        self.state["tag"] = OP.MARKER + ":" + "2" * 32
                    else:
                        self.state["added_viewer"]["source_process"] = {"pid": 999}
                    with self.assertRaises(OP.FixtureError):
                        OP.added_viewer_receipt(self.state)

    def test_additional_user_validation_rejects_wrong_identity_and_boolean_types(self):
        good = {"Id": "f" * 32, "Name": OP.AV_NAME, "IsAdministrator": False, "IsDisabled": False, "HasPassword": True}
        OP.validate_added_viewer_user(good, self.state)
        for key, value in (("Id", self.state["viewer_id"]), ("Id", None), ("Name", "unowned"),
                           ("IsAdministrator", True), ("IsAdministrator", 0), ("IsDisabled", True),
                           ("HasPassword", False), ("HasPassword", 1)):
            with self.subTest(key=key, value=value), self.assertRaises(OP.FixtureError):
                OP.validate_added_viewer_user(dict(good, **{key: value}), self.state)

    def test_receipted_third_user_and_alias_remain_in_full_upgrade_comparison(self):
        with self.av_environment() as model:
            OP.add_viewer(self.state)
            before = copy.deepcopy(model["after"])
            before["added_viewer_credentials"] = OP.added_viewer_credentials(self.state)
            self.compare(before, copy.deepcopy(before), 24, 24)
            after = self.snapshot(25)
            after["added_viewer_credentials"] = copy.deepcopy(before["added_viewer_credentials"])
            for table, rows in before["database"]["tables"].items():
                if table == "schema_migrations":
                    continue
                after["database"]["tables"][table] = copy.deepcopy(rows)
                if table in OP.SCHEMA_25_NEW_COLUMNS:
                    column, default = OP.SCHEMA_25_NEW_COLUMNS[table]
                    for row in after["database"]["tables"][table]:
                        row[column] = copy.deepcopy(default)
            self.compare(before, after, 24, 25)
            for kind in ("third-password", "old-preference", "alias"):
                changed = copy.deepcopy(before)
                if kind == "third-password":
                    changed["database"]["tables"]["users"][-1]["password_hash"] = "changed"
                elif kind == "old-preference":
                    changed["database"]["tables"]["user_settings"][0]["updated_at"] = "changed"
                else:
                    changed["added_viewer_credentials"]["browser_sha256"] = "0" * 64
                with self.subTest(kind=kind), self.assertRaises(OP.FixtureError):
                    self.compare(before, changed, 24, 24)
                migrated = copy.deepcopy(after)
                if kind == "third-password":
                    migrated["database"]["tables"]["users"][-1]["password_hash"] = "changed"
                elif kind == "old-preference":
                    migrated["database"]["tables"]["user_settings"][0]["updated_at"] = "changed"
                else:
                    migrated["added_viewer_credentials"]["browser_sha256"] = "0" * 64
                with self.subTest(migrated_kind=kind), self.assertRaises(OP.FixtureError):
                    self.compare(before, migrated, 24, 25)

    def test_additional_api_allows_only_one_fixed_creation(self):
        state = dict(self.state, added_viewer={"phase": "create_requested"})
        browser = {"admin": {"username": OP.ACCOUNTS["admin"], "password": "private-admin"}}
        api = OP.AddedViewerAPI(state, browser, {"password": "private-new"})
        api.verified_admin = True
        body = {"Name": OP.AV_NAME, "Password": "private-new", "IsAdministrator": False}
        with patch.object(OP.NativeAPI, "request", return_value={}) as request:
            for route, method, value in (("/admin/v1/users/unowned", "PUT", {}),
                                         ("/admin/v1/users/unowned/password", "POST", {}),
                                         ("/admin/v1/libraries", "POST", {}),
                                         ("/admin/v1/users", "POST", dict(body, Name="unowned")),
                                         ("/admin/v1/users", "POST", dict(body, IsAdministrator=True))):
                with self.subTest(route=route, method=method), self.assertRaises(OP.FixtureError):
                    api.request(route, method, value, admin=True)
            request.assert_not_called()
            api.request("/admin/v1/users", "POST", body, expected=(201,), admin=True)
            with self.assertRaises(OP.FixtureError):
                api.request("/admin/v1/users", "POST", body, expected=(201,), admin=True)
            self.assertEqual(request.call_count, 1)


if __name__ == "__main__":
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION"):
        raise SystemExit("Run these operator guards only through authorized root SSH on test-env.")
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--schema25-source", type=Path, required=True)
    parser.add_argument("--schema25-source-manifest-sha256", required=True)
    parser.add_argument("--schema25-catalog-sha256", required=True)
    parser.add_argument("--schema26-source", type=Path, required=True)
    parser.add_argument("--schema26-source-manifest-sha256", required=True)
    parser.add_argument("--schema26-catalog-sha256", required=True)
    parser.add_argument("--schema27-source", type=Path, required=True)
    parser.add_argument("--schema27-source-manifest-sha256", required=True)
    parser.add_argument("--schema27-catalog-sha256", required=True)
    SCHEMA25_OPTIONS = parser.parse_args()
    output = io.StringIO()
    result = unittest.TextTestRunner(stream=output, verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(ClientFixtureGuards))
    sys.stderr.write(output.getvalue())
    passed = result.wasSuccessful() and not result.skipped
    print(json.dumps({"tests": result.testsRun, "failures": len(result.failures), "errors": len(result.errors),
                      "skipped": len(result.skipped), "passed": passed,
                      "operator_sha256": hashlib.sha256(Path(SPEC.origin).read_bytes()).hexdigest(),
                      "guard_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                      "schema25_catalog_sha256": getattr(ClientFixtureGuards, "binding", {}).get("catalog_sha256"),
                      "schema26_catalog_sha256": getattr(ClientFixtureGuards, "binding26", {}).get("catalog_sha256"),
                      "schema27_catalog_sha256": getattr(ClientFixtureGuards, "binding27", {}).get("catalog_sha256"),
                      "database_mutations": 0, "service_actions": 0}))
    raise SystemExit(0 if passed else 1)
