#!/usr/bin/env python3
"""Exercise installation boundaries and safe diagnostics without a Docker daemon."""

import importlib.util
import hashlib
import json
import os
from pathlib import Path
import tempfile
import unittest
from urllib.parse import parse_qs, urlsplit
from unittest import mock


SPEC = importlib.util.spec_from_file_location(
    "goby_docker", Path(__file__).resolve().parents[1] / "deploy/oci/goby-docker.py")
HELPER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(HELPER)


class OperationsTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.media = self.root / "media"
        self.release = self.root / "release"
        self.media.mkdir()
        self.release.mkdir()

    def assert_code(self, code, function, *arguments):
        with self.assertRaises(HELPER.OperationError) as result:
            function(*arguments)
        self.assertEqual(result.exception.code, code)

    def test_existing_installation_is_never_overwritten(self):
        installation = self.root / "installed"
        installation.mkdir()
        original = installation / "precious.txt"
        original.write_bytes(b"original state")
        self.assert_code("installation_already_exists", HELPER.new_directory,
                         installation, self.media, self.release)
        self.assertEqual(original.read_bytes(), b"original state")
        self.assertEqual(list(installation.iterdir()), [original])

    def test_media_release_and_installation_must_not_overlap(self):
        for directory in (self.media / "nested", self.release / "nested"):
            with self.subTest(directory=directory.name):
                self.assert_code("installation_path_overlap", HELPER.new_directory,
                                 directory, self.media, self.release)
                self.assertFalse(directory.exists())

    def test_database_rejects_container_loopback_and_query_overrides(self):
        for hostname in ("localhost", "LOCALHOST.", "127.0.0.1", "[::1]", "[::ffff:127.0.0.1]", "0.0.0.0"):
            with self.subTest(hostname=hostname):
                self.assert_code("database_host_is_container_loopback", HELPER.database_url,
                                 f"postgres://goby:private@{hostname}/goby")
        self.assert_code("database_url_override", HELPER.database_url,
                         "postgres://goby:private@database.example/goby?host=localhost")
        self.assert_code("database_url_invalid", HELPER.database_url,
                         "postgres://goby:private@database.example/goby\nOTHER=value")

    def test_database_ca_rewrite_preserves_password_and_other_settings(self):
        original = "postgres://goby:p%40ss%24word@database.example:5432/goby?sslmode=verify-full&sslrootcert=/old.crt&connect_timeout=5"
        revised = HELPER.database_url(original, with_ca=True)
        parsed = urlsplit(revised)
        self.assertEqual(parsed.password, "p%40ss%24word")
        self.assertEqual(parse_qs(parsed.query), {"sslmode": ["verify-full"],
                         "sslrootcert": ["/etc/goby/postgres-ca.crt"], "connect_timeout": ["5"]})

    def test_private_database_file_must_be_one_private_line(self):
        path = self.root / "database.txt"
        value = "postgres://goby:private@database.example/goby"
        path.write_text(value + "\n", encoding="utf-8")
        path.chmod(0o600)
        self.assertEqual(HELPER.database_url_file(path), value)
        path.chmod(0o644)
        self.assert_code("secret_file_permissions", HELPER.database_url_file, path)
        path.chmod(0o600)
        path.write_text(value + "\n" + value, encoding="utf-8")
        self.assert_code("database_url_invalid", HELPER.database_url_file, path)

    def test_raw_application_values_and_literal_compose_paths(self):
        self.assertEqual(HELPER.dotenv_quote("/srv/Goby $archive/O'Brien"),
                         "'/srv/Goby $archive/O\\'Brien'")
        self.assertEqual(HELPER.raw_environment("KEY=a$literal#value\n")['KEY'], "a$literal#value")
        self.assert_code("environment_value_invalid", HELPER.dotenv_quote, "path\nGOBY_OCI_IMAGE=other")
        self.assert_code("environment_duplicate", HELPER.raw_environment, "KEY=one\nKEY=two\n")

    def test_redaction_covers_raw_encoded_and_json_escaped_secrets(self):
        database = "postgres://goby:p%40ss%24word@database.example/goby"
        token = 'private-token-with-"quote'
        environment = {"GOBY_DATABASE_URL": database, "GOBY_SETUP_TOKEN": token,
                       "GOBY_TMDB_TOKEN": "separate-provider-secret"}
        text = (f"database={database} password=p@ss$word encoded=p%40ss%24word\n"
                f"token={token} escaped={json.dumps(token)[1:-1]} provider=separate-provider-secret\n"
                "unknown=postgresql://other:hidden@other.example/data status=failed")
        safe = HELPER.redact(text, HELPER.secret_values(environment))
        for secret in (database, "p@ss$word", "p%40ss%24word", token,
                       json.dumps(token)[1:-1], "separate-provider-secret", "hidden"):
            self.assertNotIn(secret, safe)
        self.assertIn("status=failed", safe)

    def test_status_distinguishes_lifecycle_and_safe_readiness_reasons(self):
        self.assertEqual(HELPER.classify_status({})["status"], "not_created")
        self.assertEqual(HELPER.classify_status({"Status": "exited", "ExitCode": 0})["status"], "stopped")
        self.assertEqual(HELPER.classify_status({"Status": "exited", "ExitCode": 1})["status"], "exited")
        running = {"Status": "running"}
        self.assertEqual(HELPER.classify_status(running, "unreachable")["status"], "starting")
        self.assertEqual(HELPER.classify_status(running, "ready")["status"], "ready")
        for observed, expected in (("not_ready", "database_not_ready"), ("catalog_not_ready", "catalog_not_ready"),
                                   ("tasks_not_ready", "tasks_not_ready"), ("diagnostics_not_ready", "diagnostics_not_ready"),
                                   ("transcoding_cache_failed", "transcode_not_ready")):
            with self.subTest(observed=observed):
                answer = HELPER.classify_status(running, "not_ready", observed)
                self.assertEqual(answer["status"], "not_ready")
                self.assertEqual(answer["safeErrorCode"], expected)
        answer = HELPER.classify_status(running, "not_ready", "private-password-from-untrusted-response")
        self.assertNotIn("private-password", json.dumps(answer))

    def test_shell_settings_cannot_override_the_selected_installation(self):
        with mock.patch.dict(os.environ, {"GOBY_OCI_IMAGE": "wrong", "COMPOSE_PROJECT_NAME": "wrong",
                                          "DOCKER_HOST": "unix:///owned/docker.sock"}):
            environment = HELPER.command_environment()
        self.assertNotIn("GOBY_OCI_IMAGE", environment)
        self.assertNotIn("COMPOSE_PROJECT_NAME", environment)
        self.assertEqual(environment["DOCKER_HOST"], "unix:///owned/docker.sock")

    def test_manual_image_selection_requires_an_immutable_id(self):
        path = self.root / "deployment.env"
        path.write_text("GOBY_OCI_IMAGE='sha256:" + "a" * 64 + "'\n", encoding="utf-8")
        path.chmod(0o600)
        self.assertEqual(HELPER.configured_image(self.root), "sha256:" + "a" * 64)
        path.write_text("GOBY_OCI_IMAGE=goby:latest\n", encoding="utf-8")
        self.assert_code("configured_image_invalid", HELPER.configured_image, self.root)

    def test_public_origin_determines_cookie_security_without_credentials(self):
        self.assertEqual(HELPER.public_url("http://127.0.0.1:8096/"), ("http://127.0.0.1:8096", False))
        self.assertEqual(HELPER.public_url("https://goby.example/"), ("https://goby.example", True))
        self.assert_code("public_url_invalid", HELPER.public_url, "https://user:secret@goby.example")

    def test_catalog_rejects_mixed_companion_bytes(self):
        contents = {"compose.yaml": b"services: {}\n", "goby.env.example": b"GOBY_SERVER_NAME=Goby\n"}
        for name, data in contents.items():
            (self.release / name).write_bytes(data)
        catalog = {"kind": "goby-docker-current-release", "version": 1,
                   "application": {"sourceRevision": "b" * 40, "sha256": "c" * 64},
                   "profiles": {"software": {"imageId": "sha256:" + "d" * 64,
                                "archive": {"name": "image.tar", "bytes": 1, "sha256": "e" * 64},
                                "companions": list(contents),
                                "companionHashes": {name: hashlib.sha256(data).hexdigest() for name, data in contents.items()}}}}
        (self.release / "current-release.json").write_text(json.dumps(catalog), encoding="utf-8")
        self.assertEqual(HELPER.release_profile(self.release, "software")[2], contents)
        (self.release / "compose.yaml").write_bytes(b"services: {changed: {}}\n")
        self.assert_code("companion_hash_mismatch", HELPER.release_profile, self.release, "software")


class PortBindingTests(unittest.TestCase):
    def test_terminal_diagnostics_use_only_current_sanitized_events(self):
        actual = b'{"event":"server.starting"}\n{"event":"server.stopped","error_class":"database_lease_unavailable","extra":"private-value"}\n'
        result = HELPER.terminal_diagnostic(actual)
        self.assertEqual(result[0], "database_connection_lost")
        self.assertNotIn("private-value", repr(result))
        self.assertIsNone(HELPER.terminal_diagnostic(actual + b'{"event":"server.starting"}\n'))
        self.assertIsNone(HELPER.terminal_diagnostic(b'{"event":"server.stopped","error_class":"private-value"}\n'))

    def test_readiness_uses_only_the_selected_container_loopback_mapping(self):
        correct = {"Ports": {"8096/tcp": [{"HostIp": "127.0.0.1", "HostPort": "38962"}]}}
        self.assertEqual(HELPER.published_loopback_port(correct, 38962), 38962)
        for ports in ({}, {"8096/tcp": None},
                      {"8096/tcp": [{"HostIp": "127.0.0.1", "HostPort": "38963"}]},
                      {"8096/tcp": [{"HostIp": "0.0.0.0", "HostPort": "38962"}]},
                      {"8096/tcp": [{"HostIp": "127.0.0.1", "HostPort": "38962"}] * 2}):
            with self.subTest(ports=ports), self.assertRaises(HELPER.OperationError) as error:
                HELPER.published_loopback_port({"Ports": ports}, 38962)
            self.assertEqual(error.exception.code, "container_port_mismatch")


if __name__ == "__main__":
    unittest.main()
