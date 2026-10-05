#!/usr/bin/env python3
"""Exercise installation boundaries and safe diagnostics without a Docker daemon."""

import importlib.util
import hashlib
import json
import os
from pathlib import Path
import subprocess
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


class PlayerOperationsTests(unittest.TestCase):
    setUp = OperationsTests.setUp
    assert_code = OperationsTests.assert_code

    def catalog(self, with_player=True):
        contents = {"compose.yaml": b"services: {}\n", "goby.env.example": b"GOBY_SERVER_NAME=Goby\n",
                    "compose.background-previews.yaml": b"services: {goby: {}}\n",
                    "compose.player.yaml": b"services: {player: {}}\n"}
        backend_archive = b"recorded backend image archive"
        player_archive = b"recorded player image archive"
        catalog = {"kind": "goby-docker-current-release", "version": 1,
                   "application": {"sourceRevision": "b" * 40, "sha256": "c" * 64},
                   "profiles": {"software": {"imageId": "sha256:" + "d" * 64,
                                "archive": {"name": "backend.tar", "bytes": len(backend_archive),
                                            "sha256": hashlib.sha256(backend_archive).hexdigest()},
                                "companions": list(contents),
                                "companionHashes": {name: hashlib.sha256(data).hexdigest() for name, data in contents.items()}}}}
        if with_player:
            catalog["player"] = {"imageId": "sha256:" + "e" * 64,
                                 "archive": {"name": "player.tar", "bytes": len(player_archive),
                                             "sha256": hashlib.sha256(player_archive).hexdigest()}}
        for name, data in {**contents, "backend.tar": backend_archive, "player.tar": player_archive}.items():
            (self.release / name).write_bytes(data)
        (self.release / "current-release.json").write_text(json.dumps(catalog), encoding="utf-8")
        return catalog, contents

    def prepare_arguments(self, *extra):
        database = self.root / "database.url"
        database.write_text("postgres://goby:private@database.example/goby\n", encoding="utf-8")
        database.chmod(0o600)
        return HELPER.parser().parse_args(["prepare", "--release-dir", str(self.release),
                                         "--directory", str(self.root / "installation"),
                                         "--media-dir", str(self.media), "--database-url-file", str(database),
                                         "--public-url", "http://localhost:8080", *extra])

    def test_player_is_optional_for_existing_release_catalogs(self):
        catalog, contents = self.catalog(with_player=False)
        self.assertEqual(HELPER.release_profile(self.release, "software")[0], catalog)
        self.assert_code("release_player_missing", HELPER.release_player, catalog, contents)
        self.assertEqual(HELPER.selected_services({}), ["goby"])
        self.assertFalse(HELPER.writable_media({}))
        self.assertTrue(HELPER.writable_media({"writable_subtitles": True}))

    def test_player_identity_archive_and_companion_are_required_together(self):
        catalog, contents = self.catalog()
        self.assertEqual(HELPER.release_player(catalog, contents), catalog["player"])
        del contents["compose.player.yaml"]
        self.assert_code("player_companion_missing", HELPER.release_player, catalog, contents)
        catalog["player"]["archive"]["name"] = "../outside.tar"
        self.assert_code("release_player_archive_invalid", HELPER.release_player, catalog, contents)

    def test_prepare_records_player_and_writable_media_without_changing_media_ownership(self):
        catalog, _ = self.catalog()
        args = self.prepare_arguments("--with-player", "--player-host-port", "8081", "--writable-media")
        with mock.patch.object(HELPER.os, "geteuid", return_value=0), \
                mock.patch.object(HELPER.os, "chown") as chown, \
                mock.patch.object(HELPER, "inspect_image", return_value={"Id": "present"}) as inspect, \
                mock.patch.object(HELPER, "command"), mock.patch.object(HELPER, "emit"):
            HELPER.prepare(args)
        directory, metadata = HELPER.installation(self.root / "installation")
        self.assertEqual(metadata["compose_files"], ["compose.yaml", "compose.background-previews.yaml", "compose.player.yaml"])
        self.assertEqual(HELPER.selected_services(metadata), ["goby", "player"])
        self.assertTrue(HELPER.writable_media(metadata))
        self.assertEqual(HELPER.configured_image(directory, "player"), catalog["player"]["imageId"])
        self.assertEqual(HELPER.configured_port(directory, "player"), 8081)
        self.assertEqual(HELPER.configured_value(directory, "GOBY_PLAYER_API_UPSTREAM"), "http://goby:8096")
        self.assertEqual({call.args[0] for call in chown.call_args_list},
                         {directory / "state", directory / "cache", directory / "logs"})
        self.assertIn(mock.call(catalog["player"]["imageId"], "101:101"), inspect.call_args_list)

    def test_prepare_rejects_mismatched_player_archive_before_creating_installation(self):
        self.catalog()
        (self.release / "player.tar").write_bytes(b"wrong player release")
        args = self.prepare_arguments("--with-player")
        with mock.patch.object(HELPER.os, "geteuid", return_value=0), \
                mock.patch.object(HELPER, "inspect_image", return_value={"Id": "present"}), \
                mock.patch.object(HELPER, "command"):
            self.assert_code("archive_hash_mismatch", HELPER.prepare, args)
        self.assertFalse((self.root / "installation").exists())

    def test_prepare_rejects_unselected_or_conflicting_player_ports(self):
        self.catalog()
        with mock.patch.object(HELPER.os, "geteuid", return_value=0):
            self.assert_code("player_option_without_service", HELPER.prepare,
                             self.prepare_arguments("--player-host-port", "8081"))
            self.assert_code("player_host_port_invalid", HELPER.prepare,
                             self.prepare_arguments("--with-player", "--player-host-port", "8096"))

    def test_player_loopback_mapping_must_match_the_player_container(self):
        correct = {"Ports": {"8080/tcp": [{"HostIp": "127.0.0.1", "HostPort": "8081"}]}}
        self.assertEqual(HELPER.published_loopback_port(correct, 8081, "player"), 8081)
        self.assert_code("container_port_mismatch", HELPER.published_loopback_port, correct, 8096, "player")
        self.assert_code("container_port_mismatch", HELPER.published_loopback_port, correct, 8081)

    def test_selected_player_lifecycle_and_logs_include_both_services(self):
        metadata = {"player": {"image_id": "sha256:" + "e" * 64}}
        environment = self.root / "goby.env"
        environment.write_text("GOBY_SETUP_TOKEN=private-token\n", encoding="utf-8")
        environment.chmod(0o600)
        completed = subprocess.CompletedProcess([], 0, b"goby | private-token\nplayer | healthy\n", b"")
        for action, expected in (("start", ["up", "--detach", "--no-build", "--pull", "never", "goby", "player"]),
                                 ("stop", ["stop", "player", "goby"]),
                                 ("logs", ["logs", "--no-color", "--tail", "100", "goby", "player"])):
            with self.subTest(action=action), \
                    mock.patch.object(HELPER, "installation", return_value=(self.root, metadata)), \
                    mock.patch.object(HELPER, "check_installation"), \
                    mock.patch.object(HELPER, "status_installation", return_value={"status": "ready"}), \
                    mock.patch.object(HELPER, "compose", return_value=completed) as compose, \
                    mock.patch.object(HELPER, "emit"), mock.patch("builtins.print") as output:
                args = HELPER.parser().parse_args([action, "--directory", str(self.root)])
                self.assertEqual(HELPER.operate(args), 0)
                self.assertEqual(compose.call_args.args[2], expected)
                if action == "logs":
                    self.assertNotIn("private-token", output.call_args.args[0])
                    self.assertIn("player | healthy", output.call_args.args[0])

    def test_status_requires_both_selected_services_ready(self):
        metadata = {"player": {"image_id": "sha256:" + "e" * 64}}
        for backend, player, expected in (("ready", "ready", "ready"), ("ready", "not_ready", "not_ready"),
                                          ("stopped", "stopped", "stopped"), ("stopped", "not_ready", "not_ready"),
                                          ("not_created", "not_created", "not_created")):
            with self.subTest(backend=backend, player=player), mock.patch.object(HELPER, "service_status",
                    side_effect=[{"status": backend}, {"status": player}]):
                result = HELPER.status_installation(self.root, metadata)
            self.assertEqual(result["status"], expected)
            self.assertEqual(result["services"]["goby"]["status"], backend)
            self.assertEqual(result["services"]["player"]["status"], player)

    def test_player_health_alone_cannot_pass_api_proxy_readiness(self):
        def check(bodies):
            connections = []
            for body in bodies:
                connection = mock.Mock()
                connection.getresponse.return_value.status = 200
                connection.getresponse.return_value.read.return_value = body
                connections.append(connection)
            with mock.patch.object(HELPER.http.client, "HTTPConnection", side_effect=connections):
                return HELPER.player_readiness(8081, 8096), connections
        result, connections = check([b"ok\n", b'{"Id":"expected"}', b'{"Id":"expected"}'])
        self.assertEqual(result, ("ready", None))
        self.assertTrue(all(connection.close.called for connection in connections))
        for proxied in (b'<html>fallback page</html>', b'{"Id":"another-backend"}', b'{"ServerName":"Goby"}'):
            result, _ = check([b"ok\n", b'{"Id":"expected"}', proxied])
            self.assertEqual(result, ("not_ready", "player_proxy_not_ready"))

    def test_media_write_failure_is_explained_without_permission_mutation(self):
        metadata = {"profile": "software", "project_name": "goby-" + "a" * 16, "writable_media": True}
        failed = subprocess.CompletedProcess([], 1, b"MEDIA_NOT_WRITABLE\n", b"")
        with mock.patch.object(HELPER, "configured_image", return_value="sha256:" + "b" * 64), \
                mock.patch.object(HELPER, "inspect_image", return_value={}), \
                mock.patch.object(HELPER, "compose", return_value=subprocess.CompletedProcess([], 0, b"", b"")), \
                mock.patch.object(HELPER, "container_check", return_value=failed) as checked, \
                mock.patch.object(HELPER.os, "chown") as chown:
            self.assert_code("media_not_writable", HELPER.check_installation, self.root, metadata)
        self.assertEqual(checked.call_args.args[-1], ["writable", "software"])
        chown.assert_not_called()


if __name__ == "__main__":
    unittest.main()
