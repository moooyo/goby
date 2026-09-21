#!/usr/bin/env python3
"""Remote-only regressions for required database admission identities."""

import importlib.util
from pathlib import Path
import tempfile
import unittest


spec = importlib.util.spec_from_file_location("phase3_regression", Path(__file__).with_name("media-analysis-phase3-regression.py"))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class DatabaseAdmissionTests(unittest.TestCase):
    def test_missing_database_cannot_turn_integration_into_a_skip(self):
        for value in (None, "", " ", "postgresql://127.0.0.1:55461/", "postgresql://role:pass@127.0.0.1:55461/unowned"):
            with self.subTest(value=value), self.assertRaises(runner.Failure):
                runner.database_identity(value)

    def test_credentials_and_loopback_aliases_do_not_create_distinct_databases(self):
        left = runner.database_identity("postgresql://goby_phase3_a:one@127.0.0.1:55461/goby_phase3_database?sslmode=disable")
        right = runner.database_identity("postgres://goby_phase3_b:two@[::1]:55461/goby_phase3_database")
        self.assertEqual(left, right)
        with self.assertRaises(runner.Failure):
            runner.database_identity("postgresql://goby_phase3_a:one@127.0.0.1:55461/goby_phase3_database?dbname=other")

    def test_backup_pairs_retain_the_existing_fixture_name_boundary(self):
        value = "postgresql://goby_backup_phase3_source:secret@127.0.0.1:55461/goby_backup_phase3_source?sslmode=disable"
        self.assertEqual(runner.database_identity(value, backup=True), (55461, "goby_backup_phase3_source"))
        with self.assertRaises(runner.Failure):
            runner.database_identity(value)


class SourceInventoryTests(unittest.TestCase):
    def test_path_components_use_the_manifest_posix_string_order(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            names = ["cmd/goby/dashboard_assets.go", "cmd/goby-notification-receiver/main.go", "go.mod"]
            for name in names:
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(name.encode())
            rows = runner.source_inventory(root)
            self.assertEqual([row["path"] for row in rows], sorted(names))
            self.assertEqual(rows[0]["path"], "cmd/goby-notification-receiver/main.go")


if __name__ == "__main__":
    unittest.main()
