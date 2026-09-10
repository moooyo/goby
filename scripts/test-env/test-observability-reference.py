#!/usr/bin/env python3
"""Exercise observability study boundaries with synthetic memory only.

Run through authorized root SSH beside the frozen preparation and recorder
sources. The suite reads only inert source bytes before installing its effect
fence. It never contacts an actual server, writes a capture, changes a service,
or inspects fixture data. Passing guards is not live study acceptance evidence.
"""

from __future__ import annotations

import _io
import argparse
import base64
import binascii
import contextlib
import copy
import datetime
# Preload only the codecs explicitly used by the frozen sanitizer before the
# import fence rejects lazy filesystem-backed standard-library imports.
import encodings.utf_8
import encodings.utf_16_le
import encodings.utf_16_be
import hashlib
import http.client
import importlib.util
import io
import json
import linecache
import os
from pathlib import Path
import re
import secrets
import shutil
import signal
import socket
import stat
import subprocess
import sys
import tempfile
import time
import types
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, quote, unquote, urlencode, urlsplit

sys.dont_write_bytecode = True
RECORDER: types.ModuleType
OPERATOR: types.ModuleType
SOURCE_LINES: dict[str, list[str]] = {}
SOURCE_HASHES: dict[str, str] = {}


@contextlib.contextmanager
def memory_tracebacks():
    with patch.object(linecache, "checkcache", lambda filename=None: None), \
            patch.object(linecache, "lazycache", lambda filename, module_globals: False), \
            patch.object(linecache, "getlines", lambda filename, module_globals=None:
                         list(SOURCE_LINES.get(str(filename), ()))):
        yield


class EffectFence(contextlib.ExitStack):
    """Reject unmocked effects during imports, constructors and cleanup."""

    def __init__(self) -> None:
        super().__init__()
        self.violations: list[str] = []

    def __enter__(self) -> EffectFence:
        super().__enter__()
        import builtins
        self.enter_context(memory_tracebacks())
        fence = self

        class RejectUncachedImports:
            def find_spec(self, fullname, path=None, target=None):
                fence.violations.append("import:" + fullname)
                raise AssertionError("Uncached import: " + fullname)

        self.enter_context(patch.object(sys, "meta_path", [RejectUncachedImports()]))
        targets = (
            (builtins, ("open",)), (io, ("open", "open_code", "FileIO")), (_io, ("open", "open_code", "FileIO")),
            (subprocess, ("run", "Popen", "call", "check_call", "check_output")),
            (socket, ("socket", "create_connection", "getaddrinfo")),
            (http.client, ("HTTPConnection", "HTTPSConnection")),
            (shutil, ("copy", "copy2", "copyfile", "copytree", "move", "rmtree", "disk_usage")),
            (tempfile, ("TemporaryDirectory", "NamedTemporaryFile", "TemporaryFile", "mkstemp", "mkdtemp")),
            (signal, ("signal", "getsignal", "setitimer", "getitimer", "pthread_sigmask")), (time, ("sleep",)),
            (os, ("open", "close", "fdopen", "stat", "lstat", "fstat", "readlink", "scandir", "listdir", "walk",
                  "read", "write", "fsync", "umask", "kill", "killpg", "system", "popen", "fork", "posix_spawn",
                  "posix_spawnp", "execve", "execvp", "replace", "rename", "mkdir", "makedirs", "remove", "unlink",
                  "rmdir", "chmod", "chown", "link", "symlink", "truncate", "waitpid")),
            (os.path, ("ismount",)),
            (Path, ("open", "exists", "is_symlink", "is_dir", "is_file", "resolve", "stat", "lstat", "read_bytes",
                    "read_text", "write_bytes", "write_text", "mkdir", "rmdir", "unlink", "chmod", "touch",
                    "rename", "replace", "glob", "rglob", "iterdir")),
        )
        for owner, names in targets:
            for name in names:
                if not hasattr(owner, name):
                    continue
                label = owner.__name__ + "." + name

                def denied(*args, _label=label, **kwargs):
                    self.violations.append(_label)
                    raise AssertionError("Unfaked external effect: " + _label)

                self.enter_context(patch.object(owner, name, denied))
        return self

    def __exit__(self, *arguments) -> None:
        super().__exit__(*arguments)
        if self.violations:
            raise AssertionError("External effects attempted: " + ", ".join(self.violations))


class FakeResponse:
    def __init__(self, body=b"{}", *, status=200, headers=None, eof=True):
        self.body, self.offset, self.eof = body, 0, eof
        self.status, self.reason, self.version = status, "Synthetic response", 11
        self.headers = headers if headers is not None else [("Content-Length", str(len(body)))]

    @property
    def length(self):
        return len(self.body) - self.offset if self.eof else None

    def getheaders(self):
        return list(self.headers)

    def read(self, limit):
        value = self.body[self.offset:self.offset + limit]
        self.offset += len(value)
        return value

    def isclosed(self):
        return self.eof and self.offset == len(self.body)


class FakeConnection:
    def __init__(self, response):
        self.response, self.calls, self.closed = response, [], False

    def request(self, method, path, body=None, headers=None):
        self.calls.append((method, path, body, copy.deepcopy(headers)))

    def getresponse(self):
        return self.response

    def close(self):
        self.closed = True


class ObservabilityGuards(unittest.TestCase):
    def setUp(self):
        fence = EffectFence()
        fence.__enter__()
        self.addCleanup(fence.__exit__, None, None, None)
        output = contextlib.redirect_stdout(io.StringIO())
        output.__enter__()
        self.addCleanup(output.__exit__, None, None, None)

    def replace(self, owner, name, value):
        replacement = patch.object(owner, name, value)
        replacement.start()
        self.addCleanup(replacement.stop)
        return value

    def recorder(self):
        value = RECORDER.Recorder.__new__(RECORDER.Recorder)
        value.manifest = {"serverId": "synthetic-fresh-server", "oldServices": {"old": {"networkNamespace": "net:[old]"}}}
        value.authority = {"pid": 12345, "networkNamespace": "net:[fresh]"}
        value.started, value.deadline, value.finishing, value.initialized = 100.0, 1000.0, False, True
        value.cleanup_deadline, value.cleanup_phases = None, []
        value.credentials = {name: {"REFERENCE_USERNAME": fixed["username"], "REFERENCE_PASSWORD": "admin-secret" if name == "admin" else "",
            "REFERENCE_TOKEN": "synthetic-" + name + "-token", "REFERENCE_USER_ID": "owned-" + name, "REFERENCE_DEVICE_ID": fixed["deviceId"]}
            for name, fixed in RECORDER.ACCOUNTS.items()}
        value.logins, value.secrets = {}, {"admin-secret", *(row["REFERENCE_TOKEN"] for row in value.credentials.values())}
        value.invalid_token = "synthetic-invalid-token"
        value.secrets.add(value.invalid_token)
        value.secret_regex, value.secret_signature, value.secret_byte_sequences = None, None, ()
        value.labels, value.records, value.mutations = set(), {}, []
        value.record_count = value.json_bytes = value.log_bytes = value.incomplete_count = value.charged_json = value.charged_logs = 0
        value.persistence_failures, value.cleanup_errors, value.observations = [], [], []
        value.capture_failure, value.cleanup_ok, value.evidence_unavailable = None, False, False
        value.key_create_attempted = value.key_revoke_attempted = value.key_ownership_failed = value.key_baseline_empty = False
        value.owned_key = value.key_invalidity = value.key_revoke_status = None
        value.user_create_attempted, value.created_user, value.user_cause_proved = False, None, False
        value.ordinary_logout_attempted, value.logout_statuses, value.invalidity = set(), {}, {}
        value.allowed_log_names, value.selected_log_name, value.baselines, value.allowed_dates = set(), None, {}, set()
        value.writes_blocked, value.setup_baseline = False, {"synthetic": "immutable setup"}
        self.replace(RECORDER, "operator_module", lambda: OPERATOR)
        return value

    def memory_store(self):
        files = {}

        def write(path, content):
            self.assertTrue(RECORDER.ROOT in path.parents)
            if path in files:
                raise FileExistsError("Synthetic evidence is exclusive")
            files[path] = bytes(content)

        self.replace(RECORDER, "save_bytes", write)
        self.replace(RECORDER, "load", lambda path: json.loads(files[path]))
        self.replace(RECORDER, "canonical", lambda path, **kwargs: None)
        self.replace(RECORDER, "digest", lambda path: hashlib.sha256(files[path]).hexdigest())
        self.replace(Path, "read_bytes", lambda path: files[path])
        self.replace(Path, "iterdir", lambda path: iter(sorted(item for item in files if item.parent == path)))
        self.replace(Path, "rglob", lambda path, pattern: iter(sorted(item for item in files if path in item.parents)))
        self.replace(Path, "is_file", lambda path: path in files)
        self.replace(Path, "is_symlink", lambda path: False)
        self.replace(Path, "stat", lambda path: types.SimpleNamespace(st_size=len(files[path])))
        return files

    def wire(self, recorder, responses):
        connections = []
        pending = list(responses)

        def connect(host, port, *, timeout):
            self.assertEqual((host, port), ("127.0.0.1", RECORDER.PORT))
            self.assertLessEqual(timeout, 5)
            connection = FakeConnection(pending.pop(0))
            connections.append(connection)
            return connection

        self.replace(http.client, "HTTPConnection", connect)
        self.replace(signal, "setitimer", lambda *args: None)
        self.replace(signal, "getitimer", lambda *args: (0, 0))
        self.replace(time, "monotonic", lambda: 100.0)
        self.replace(recorder, "check_authority", lambda: None)
        return connections

    def install_handoff(self, recorder):
        values, rows, objects, texts, hashes, proofs = copy.deepcopy(recorder.credentials), {}, {}, {}, {}, {}
        for identity, fixed in RECORDER.ACCOUNTS.items():
            credential_path = RECORDER.EVIDENCE / "private" / (identity + "-credentials.env")
            login_path = RECORDER.EVIDENCE / "private" / (identity + "-login-response.json")
            row = {**fixed, "userId": values[identity]["REFERENCE_USER_ID"], "sessionId": "session-" + identity,
                   "internalDeviceId": "internal-" + identity, "administrator": identity == "admin", "credentialState": "LIVE_HANDOFF",
                   "tokenSha256": RECORDER.digest_bytes(values[identity]["REFERENCE_TOKEN"].encode()), "liveProbeStatus": 200,
                   "credentialsFile": str(credential_path), "loginResponseFile": str(login_path),
                   "credentialsSha256": "a" * 64, "loginResponseSha256": "b" * 64}
            rows[identity], hashes[credential_path], hashes[login_path] = row, "a" * 64, "b" * 64
            texts[credential_path] = "\n".join(key + "=" + value for key, value in values[identity].items())
            objects[login_path] = {"AccessToken": values[identity]["REFERENCE_TOKEN"], "ServerId": recorder.manifest["serverId"],
                "User": {"Id": row["userId"], "Name": row["username"], "Policy": {"IsAdministrator": identity == "admin"}},
                "SessionInfo": {"Id": row["sessionId"], "UserId": row["userId"], "DeviceId": row["deviceId"],
                                "Client": row["client"], "InternalDeviceId": row["internalDeviceId"]}}
            capture = RECORDER.EVIDENCE / "private/raw" / (OPERATOR.PREFIX + identity + "-live.json")
            proofs[identity] = {"status": 200, "capture": str(capture)}
            objects[capture] = {"request": {"method": "GET", "path": RECORDER.SESSIONS_ROUTE,
                "headers": [["X-Emby-Token", values[identity]["REFERENCE_TOKEN"]]]},
                "response": {"status": 200, "body": [{"DeviceId": row["deviceId"], "UserId": row["userId"]}]},
                "observation": {"completeHTTP": True}}
        admin = rows["admin"]
        recorder.manifest.update({"credentials": rows, "ordinaryProtectedAccessProofs": proofs,
            "freshAdministratorOwnership": {"serverId": recorder.manifest["serverId"], **{key: admin[key] for key in
                ("userId", "deviceId", "internalDeviceId", "sessionId", "administrator", "tokenSha256")}}})
        self.replace(RECORDER, "canonical", lambda path: self.assertIn(path, hashes))
        self.replace(RECORDER, "digest", lambda path: hashes[path])
        self.replace(RECORDER, "load", lambda path: copy.deepcopy(objects[path]))
        self.replace(Path, "read_text", lambda path, **kwargs: texts[path])
        recorder.credentials, recorder.logins = {}, {}
        return types.SimpleNamespace(values=values, rows=rows, objects=objects, texts=texts, hashes=hashes, proofs=proofs)

    def bootstrap(self):
        value = OPERATOR.Bootstrap.__new__(OPERATOR.Bootstrap)
        value.identity = {"networkNamespace": "net:[fresh]"}
        value.tokens, value.secrets, value.passwords = {}, set(), {"admin": "synthetic-password", "viewer": ""}
        value.count = value.wire = value.complete_count = value.incomplete_count = value.cleanup_count = value.cleanup_wire = 0
        value.server_id, value.deadline, value.prefix = "synthetic-fresh-server", 1000.0, OPERATOR.PREFIX
        value.revoked, value.login_attempted, value.viewer_creation_attempted = set(), set(), False
        value.persistence_failures, value.cleanup_by_account = [], {name: 0 for name in OPERATOR.ACCOUNTS}
        self.replace(value, "sanitize", lambda body, field="": {"synthetic": "redacted"})
        self.replace(OPERATOR, "same_new_identity", lambda identity: None)
        self.replace(os, "readlink", lambda path: "net:[fresh]")
        self.replace(Path, "exists", lambda path: False)
        self.replace(OPERATOR, "emit", lambda value: None)
        return value

    def test_fixed_instances_historical_corpus_and_independent_phase_budgets(self):
        self.assertEqual((OPERATOR.ROOT, OPERATOR.DATA, OPERATOR.UNIT, OPERATOR.MARKER, OPERATOR.PORT),
                         (RECORDER.EVIDENCE, RECORDER.DATA, RECORDER.UNIT, RECORDER.MARKER, 18101))
        self.assertEqual(OPERATOR.OLD_UNITS, {"goby-emby-reference.service": 3131777, "goby-foundation-test.service": 3641418})
        self.assertEqual(OPERATOR.MAIN_START_TICKS, "26048863")
        self.assertEqual(OPERATOR.MAIN_BINARY_SHA256, "62729fa1ba6b7d5f191d598c79a14606cb139bcdec6346ff5c3f1676551afd54")
        self.assertEqual(RECORDER.OLD_RECORDS, 2366)
        self.assertEqual((OPERATOR.MAX_REQUESTS, RECORDER.MAIN_REQUESTS, RECORDER.MAX_REQUESTS, OPERATOR.MAX_CLEANUP_REQUESTS), (24, 106, 118, 8))
        self.assertEqual(24 + 118 + 8, RECORDER.ALL_HTTP_REQUESTS)
        self.assertEqual((RECORDER.MAIN_SECONDS, RECORDER.CLEANUP_SECONDS), (240, 90))
        self.assertEqual((RECORDER.CREDENTIAL_PHASE_SECONDS, RECORDER.INITIALIZATION_PHASE_SECONDS), (30, 40))
        self.assertEqual(len(OPERATOR.PREVIOUS_BUNDLE_FILES), 21)
        self.assertTrue({str(OPERATOR.PREVIOUS_DATA), str(OPERATOR.PREVIOUS_SOURCE)} <= set(OPERATOR.ALL_HISTORICAL_REMOVED))
        self.assertEqual(RECORDER.OPERATOR_SOURCE_SHA256, SOURCE_HASHES["prepare-observability-fresh.py"])
        for name, digest in OPERATOR.SANITIZER_SOURCES.items():
            self.assertEqual(digest, SOURCE_HASHES[name])
        self.replace(RECORDER, "_operator", OPERATOR)
        self.replace(RECORDER, "digest", lambda path: "0" * 64)
        self.replace(Path, "is_symlink", lambda path: False)
        with self.assertRaisesRegex(RuntimeError, "frozen reviewed source"):
            RECORDER.operator_module()
        saved, commands = {}, []
        self.replace(OPERATOR, "save", lambda path, value, **kwargs: saved.update({path: value}))
        self.replace(OPERATOR, "run", lambda arguments, **kwargs: commands.append(arguments))
        OPERATOR.write_configuration()
        OPERATOR.start_service()
        launcher = saved[OPERATOR.RUNTIME / "launch.sh"]
        self.assertLess(launcher.index('cd "$APP_DIR"'), launcher.index('exec "$APP_DIR/system/EmbyServer"'))
        self.assertIn("--property=WorkingDirectory=" + str(OPERATOR.RUNTIME), commands[0])
        self.assertIn("--property=MemoryMax=768M", commands[0])

    def test_ready_requires_exact_two_ordinary_logins_no_library_and_all_history(self):
        self.recorder()
        manifest = {"state": "READY", "marker": RECORDER.MARKER, "unit": RECORDER.UNIT, "port": 18101, "httpsPort": 18501,
            "programData": str(RECORDER.DATA), "evidenceRoot": str(RECORDER.EVIDENCE), "operatorSha256": RECORDER.OPERATOR_SOURCE_SHA256,
            "credentialState": "LIVE_HANDOFF", "ordinaryLoginCount": 2, "serverId": "fresh", "credentials": {"admin": {}, "viewer": {}},
            "oldBaseline": {"records": {str(index): "a" for index in range(4732)}, "media": {str(index): "b" for index in range(240)}},
            "setupRecordCount": 18, "bootstrapLibraries": [], "oldPreservationVerified": True, "allFreshProgramDataOwned": True,
            "bootstrapConfigurationSafetyVerified": True, "viewerEmptyPasswordLoginVerified": True,
            **{name: 0 for name in ("bootstrapLibraryMutationRequests", "bootstrapConfigurationMutationRequests",
                "bootstrapNamedConfigurationRequests", "bootstrapTaskRequests", "bootstrapTaskMutationRequests",
                "bootstrapApplicationKeyRequests", "bootstrapMediaRequests")}}
        self.replace(OPERATOR, "load", lambda path: copy.deepcopy(manifest))
        self.assertEqual(RECORDER.load_manifest()["ordinaryLoginCount"], 2)
        for field, bad in (("ordinaryLoginCount", 3), ("serverId", OPERATOR.OLD_SERVER_ID), ("setupRecordCount", True),
                           ("bootstrapLibraries", [{}]), ("bootstrapApplicationKeyRequests", 1)):
            with self.subTest(field=field):
                original, manifest[field] = manifest[field], bad
                with self.assertRaises(RuntimeError):
                    RECORDER.load_manifest()
                manifest[field] = original
        manifest["oldBaseline"]["records"].pop("0")
        with self.assertRaisesRegex(RuntimeError, "corpus membership"):
            RECORDER.load_manifest()

    def test_dual_handoff_matches_token_user_role_session_and_preserves_earlier_ack(self):
        recorder = self.recorder()
        fixture = self.install_handoff(recorder)
        recorder.load_handoff()
        self.assertEqual(set(recorder.credentials), {"admin", "viewer"})
        self.assertEqual(recorder.credentials["viewer"]["REFERENCE_PASSWORD"], "")
        viewer = fixture.objects[Path(fixture.rows["viewer"]["loginResponseFile"])]
        for container, field, bad in ((viewer["User"]["Policy"], "IsAdministrator", True),
                (viewer["SessionInfo"], "UserId", "foreign"), (viewer, "ServerId", OPERATOR.OLD_SERVER_ID),
                (viewer["User"], "Name", "a-name-is-not-authority")):
            with self.subTest(field=field):
                previous, container[field] = container[field], bad
                recorder.credentials, recorder.logins = {}, {}
                with self.assertRaisesRegex(RuntimeError, "acknowledged ordinary login"):
                    recorder.load_handoff()
                self.assertEqual(set(recorder.credentials), {"admin"})
                container[field] = previous

    def test_handoff_owner_and_complete_protected_access_are_independent_proofs(self):
        recorder = self.recorder()
        fixture = self.install_handoff(recorder)
        recorder.manifest["freshAdministratorOwnership"]["administrator"] = False
        with self.assertRaisesRegex(RuntimeError, "ownership attestation"):
            recorder.load_handoff()
        recorder.manifest["freshAdministratorOwnership"]["administrator"] = True
        recorder.credentials, recorder.logins = {}, {}
        capture = Path(fixture.proofs["viewer"]["capture"])
        fixture.objects[capture]["observation"]["completeHTTP"] = False
        with self.assertRaisesRegex(RuntimeError, "live-access proof"):
            recorder.load_handoff()
        self.assertEqual(set(recorder.credentials), {"admin", "viewer"})

    def test_four_read_routes_use_observed_names_and_application_key_has_no_extra_role(self):
        recorder = self.recorder()
        recorder.select_log({"Items": [{"Name": "embyserver.txt", "Size": 15}]})
        recorder.owned_key = {"AccessToken": "synthetic-application-key", "AppName": RECORDER.APP_NAME}
        routes = [RECORDER.LOGS_ROUTE, RECORDER.ACTIVITY_ROUTE, RECORDER.LOG_PREFIX + "embyserver.txt", RECORDER.LOG_PREFIX + "embyserver.txt/Lines"]
        for principal in ("anonymous", "invalid", "viewer", "admin", "application"):
            for route in routes:
                self.assertIn(recorder.authorize("GET", route, principal, RECORDER.MISSING), {"json", "log"})
        for route in (RECORDER.USERS_ROUTE, RECORDER.KEYS_PAGE, RECORDER.PUBLIC_ROUTE, RECORDER.LOGS_ROUTE + "?Limit=1"):
            with self.assertRaises(RuntimeError):
                recorder.authorize("GET", route, "application", RECORDER.MISSING)
        with self.assertRaises(RuntimeError):
            recorder.authorize("GET", RECORDER.ACTIVITY_ROUTE + "?MinDate=guessed", "admin", RECORDER.MISSING)
        checked = []
        self.replace(OPERATOR, "same_new_identity", lambda identity: checked.append(copy.deepcopy(identity)))
        self.replace(os, "readlink", lambda path: "net:[host]")
        with self.assertRaisesRegex(RuntimeError, "attested fresh namespace"):
            recorder.check_authority()
        self.assertEqual(checked, [recorder.authority])

    def test_observed_filename_and_route_fences_refuse_traversal_foreign_files_and_mutations(self):
        recorder = self.recorder()
        for name in ("../server.log", "/etc/passwd", "C:\\private.log", "x/y", "%2fetc%2fpasswd", ".."):
            with self.subTest(name=name), self.assertRaises(RuntimeError):
                recorder.select_log({"Items": [{"Name": name, "Size": 1}]})
        recorder.select_log({"Items": [{"Name": "safe name.log", "Size": 1}]})
        for path in ("https://foreign/emby/System/Logs/Query", "//host/emby/System/Logs/Query", "/emby/System/Logs/%2fetc%2fpasswd",
                "/emby/System/Logs/unobserved.log", "/emby/System/Logs/safe%20name.log#fragment", "/emby/System/Logs/Query?Limit=1&Limit=2",
                "/emby/System/Logs", "/emby/System/Logs/Log", "/emby/System/Configuration", "/emby/ScheduledTasks", "/emby/Devices"):
            with self.subTest(path=path), self.assertRaises(RuntimeError):
                recorder.authorize("GET", path, "admin", RECORDER.MISSING)
        for method, path in (("POST", "/emby/Users/AuthenticateByName"), ("POST", "/emby/System/Configuration"),
                             ("DELETE", "/emby/Users/owned-viewer"), ("POST", "/emby/Library/VirtualFolders")):
            with self.assertRaises(RuntimeError):
                recorder.authorize(method, path, "admin", RECORDER.MISSING)

    def test_single_causal_user_and_single_application_creation_require_prior_proofs(self):
        recorder = self.recorder()
        user_body = {"Name": RECORDER.CREATED_USER_NAME}
        key_path = RECORDER.KEYS_ROUTE + "?" + urlencode({"App": RECORDER.APP_NAME})
        for method, path, body in (("POST", RECORDER.NEW_USER_ROUTE, user_body), ("POST", key_path, RECORDER.MISSING)):
            with self.assertRaises(RuntimeError):
                recorder.authorize(method, path, "admin", body)
        recorder.baselines["activity-before-user"], recorder.key_baseline_empty = {}, True
        self.assertEqual(recorder.authorize("POST", RECORDER.NEW_USER_ROUTE, "admin", user_body), "json")
        self.assertEqual(recorder.authorize("POST", key_path, "admin", RECORDER.MISSING), "json")
        recorder.user_create_attempted = recorder.key_create_attempted = True
        for path, body in ((RECORDER.NEW_USER_ROUTE, user_body), (key_path, RECORDER.MISSING)):
            with self.assertRaises(RuntimeError):
                recorder.authorize("POST", path, "admin", body)
        recorder.acknowledge("POST", RECORDER.NEW_USER_ROUTE, "admin", 200, {"Id": "cause-user", "Name": RECORDER.CREATED_USER_NAME}, True, "cause")
        self.assertEqual(recorder.created_user["Id"], "cause-user")
        self.assertEqual(len(recorder.credentials), 2)

    def test_application_ownership_requires_unique_complete_list_and_distinct_token(self):
        recorder = self.recorder()
        good = {"Items": [{"AppName": RECORDER.APP_NAME, "AccessToken": "synthetic-application-key"}], "TotalRecordCount": 1}
        recorder.key_create_attempted = True
        recorder.acknowledge("GET", RECORDER.KEYS_PAGE, "admin", 200, good, False, "short")
        self.assertIsNone(recorder.owned_key)
        for bad in ({"Items": good["Items"] * 2, "TotalRecordCount": 2}, {**good, "TotalRecordCount": True},
                    {"Items": [{"AppName": "foreign", "AccessToken": "x"}], "TotalRecordCount": 1},
                    {"Items": [{"AppName": RECORDER.APP_NAME, "AccessToken": recorder.token("admin")}], "TotalRecordCount": 1}):
            recorder.key_ownership_failed, recorder.owned_key = False, None
            recorder.acknowledge("GET", RECORDER.KEYS_PAGE, "admin", 200, bad, True, "bad")
            self.assertTrue(recorder.key_ownership_failed)
            self.assertIsNone(recorder.owned_key)
        recorder.key_ownership_failed = False
        recorder.acknowledge("GET", RECORDER.KEYS_PAGE, "admin", 200, good, True, "owned")
        self.assertEqual(recorder.token("application"), "synthetic-application-key")

    def test_fresh_user_membership_is_exactly_two_then_three_with_observed_roles(self):
        recorder = self.recorder()
        rows = [{"Id": values["REFERENCE_USER_ID"], "Name": values["REFERENCE_USERNAME"], "Policy": {"IsAdministrator": identity == "admin"}}
                for identity, values in recorder.credentials.items()]
        self.replace(recorder, "request", lambda *args, **kwargs: (200, copy.deepcopy(rows)))
        recorder.users("before", after=False)
        recorder.created_user = {"Id": "cause-user", "Name": RECORDER.CREATED_USER_NAME}
        rows.append({**recorder.created_user, "Policy": {"IsAdministrator": False}})
        recorder.users("after", after=True)
        rows.append({"Id": "foreign", "Name": "unowned"})
        with self.assertRaisesRegex(RuntimeError, "exact owned scope"):
            recorder.users("foreign", after=True)

    def test_sanitizer_removes_known_secret_encodings_paths_and_late_echoes(self):
        recorder = self.recorder()
        secret = "synthetic-late-credential-77"
        forms = [secret, quote(secret, safe=""), "".join("%" + format(value, "02x") for value in secret.encode()),
                 "".join("\\u" + format(ord(value), "04x") for value in secret),
                 "".join("\\x" + format(value, "02x") for value in secret.encode()), secret.encode().hex(),
                 base64.b64encode(("prefix " + secret + " suffix").encode()).decode()]
        raw = {"Diagnostic": forms, secret: "an echoed key", "Nested": {"Headers": [["Proxy-Authorization", secret]]},
               "Path": str(RECORDER.DATA / "logs/embyserver.txt"), "EncodedPath": quote(str(RECORDER.DATA), safe=""),
               "Count": 2, "IsAdministrator": False, "Name": "embyserver.txt", "Size": 128,
               "Level": "Info", "Description": "a", "Code": "0"}
        recorder.collect_secrets(raw)
        cleaned = recorder.sanitize(raw)
        text = json.dumps(cleaned)
        for form in forms:
            self.assertNotIn(form, text)
        self.assertNotIn(str(RECORDER.DATA), text)
        for name in ("Count", "IsAdministrator", "Name", "Size", "Level", "Description", "Code"):
            self.assertEqual(cleaned[name], raw[name])
            self.assertIs(type(cleaned[name]), type(raw[name]))
        low_entropy = {"Key": "0", "Password": "a", "Size": "100", "Date": "2026-09-10",
                       "Headers": [["Content-Length", "100"]], "Name": "safe data.log"}
        recorder.collect_secrets(low_entropy)
        cleaned = recorder.sanitize(low_entropy)
        self.assertNotEqual(cleaned["Key"], "0")
        self.assertNotEqual(cleaned["Password"], "a")
        for name in ("Size", "Date", "Headers", "Name"):
            self.assertEqual(cleaned[name], low_entropy[name])

    def test_sanitized_dictionary_collision_is_rejected_and_encoded_depth_is_bounded(self):
        recorder = self.recorder()
        recorder.secrets.update({"synthetic-first-secret", "synthetic-second-secret"})
        with self.assertRaisesRegex(RuntimeError, "keys collide"):
            recorder.sanitize({"synthetic-first-secret": 1, "synthetic-second-secret": 2})
        value = "https://user:password@host/" + str(RECORDER.DATA)
        for _ in range(5):
            value = quote(value, safe="")
        self.assertNotEqual(recorder.clean_text(value), value)
        self.assertLess(len(recorder.clean_text(value)), 100)

    def test_complete_log_bytes_json_errors_and_redirects_remain_exact_without_following(self):
        recorder = self.recorder()
        recorder.allowed_log_names, recorder.selected_log_name = {"embyserver.txt"}, "embyserver.txt"
        files = self.memory_store()
        body = "complete UTF-8 log: café\n".encode()
        connections = self.wire(recorder, [FakeResponse(body), FakeResponse(b'{"Error":"missing"}', status=404,
            headers=[("Content-Type", "application/json"), ("Content-Length", "19")]),
            FakeResponse(b"", status=302, headers=[("Location", "https://foreign/private"), ("Content-Length", "0")])])
        alarms = []
        self.replace(signal, "getitimer", lambda which: (30.0, 0.0))
        self.replace(signal, "setitimer", lambda *arguments: alarms.append(arguments))
        status, result = recorder.request("log", "GET", RECORDER.LOG_PREFIX + "embyserver.txt")
        self.assertEqual((status, result), (200, body.decode()))
        self.assertEqual(files[RECORDER.WIRE / "log.body"], body)
        self.assertEqual(recorder.records["log"]["record"]["observation"]["wireBytes"], len(body))
        recorder.request("error", "GET", RECORDER.LOG_PREFIX + RECORDER.UNKNOWN_LOG_NAME)
        self.assertEqual(recorder.records["error"]["wire"]["bodyBudget"], "json")
        self.assertEqual(recorder.request("redirect", "GET", RECORDER.LOGS_ROUTE)[0], 302)
        self.assertEqual(sum(len(connection.calls) for connection in connections), 3)
        self.assertTrue(all(connection.closed for connection in connections))
        self.assertEqual(alarms[-1], (signal.ITIMER_REAL, 30.0, 0.0))
        self.assertTrue(all(arguments[1] > 0 for arguments in alarms))
        RECORDER.restore_alarm((0.5, 0.0), 99.0)
        self.assertEqual(alarms[-1], (signal.ITIMER_REAL, 0.001, 0.0))

    def test_partial_binary_and_oversized_bodies_cannot_claim_complete_exportable_evidence(self):
        for name, response, complete, kind in (
            ("short", FakeResponse(b"{}", headers=[("Content-Length", "3")]), False, "json"),
            ("no-eof", FakeResponse(b"{}", eof=False), False, "json"),
            ("duplicate-length", FakeResponse(b"{}", headers=[("Content-Length", "2"), ("Content-Length", "3")]), False, "json"),
            ("binary", FakeResponse(b"\xff\x00"), True, "private-binary"),
            ("oversized", FakeResponse(b"x" * (RECORDER.MAX_BODY + 1)), False, "text")):
            with self.subTest(name=name):
                recorder = self.recorder()
                self.memory_store()
                self.wire(recorder, [response])
                with self.assertRaisesRegex(RuntimeError, "complete bounded UTF-8"):
                    recorder.request(name, "GET", RECORDER.LOGS_ROUTE)
                row = recorder.records[name]["record"]
                self.assertEqual((row["observation"]["completeHTTP"], row["response"]["bodyType"]), (complete, kind))
                self.assertEqual(row["observation"]["bodyExportable"], kind != "private-binary")
                self.assertTrue(recorder.writes_blocked)

    def install_audit(self, recorder, files):
        files[RECORDER.PRIVATE / "baseline.json"] = RECORDER.json_bytes(recorder.setup_baseline)
        files[RECORDER.PRIVATE / "mutation-results.json"] = RECORDER.json_bytes(recorder.mutations)
        files[RECORDER.ROOT / ".goby-managed"] = (RECORDER.CAPTURE_MARKER + "\n").encode()

    def test_wire_audit_and_export_collect_later_secrets_before_exporting_earlier_echoes(self):
        recorder = self.recorder()
        files = self.memory_store()
        secret = "late-received-diagnostic-secret-77"
        self.wire(recorder, [FakeResponse(json.dumps({"Message": secret}).encode()),
                             FakeResponse(json.dumps({"Password": secret}).encode())])
        recorder.request("a-echo", "GET", RECORDER.ACTIVITY_ROUTE)
        recorder.request("z-secret", "GET", RECORDER.LOGS_ROUTE)
        self.assertNotIn(secret, recorder.secrets)
        self.assertFalse(any(RECORDER.EXPORT in path.parents for path in files))
        self.install_audit(recorder, files)
        audited = recorder.audit_http()
        self.assertIn(secret, recorder.secrets)
        exported = recorder.export_records(audited)
        self.assertEqual(set(exported), {"a-echo", "z-secret"})
        for path, content in files.items():
            if RECORDER.EXPORT in path.parents:
                self.assertNotIn(secret.encode(), content)
        with self.assertRaisesRegex(RuntimeError, "earlier export"):
            recorder.export_records(audited)
        files[RECORDER.WIRE / "a-echo.body"] = b"different bytes"
        with self.assertRaisesRegex(RuntimeError, "bytes changed"):
            recorder.audit_http()

    def test_wire_audit_refuses_dto_type_headers_and_body_mismatches(self):
        recorder = self.recorder()
        files = self.memory_store()
        self.wire(recorder, [FakeResponse(b'{"Count":1}')])
        recorder.request("one", "GET", RECORDER.LOGS_ROUTE)
        self.install_audit(recorder, files)
        recorder.audit_http()
        path = RECORDER.RAW / (RECORDER.PREFIX + "one.json")
        original = files[path]
        for field, bad in (("body", {"Count": True}), ("headers", [["Content-Length", "0"]]), ("status", 401)):
            value = json.loads(original)
            value["response"][field] = bad
            files[path] = RECORDER.json_bytes(value)
            with self.assertRaisesRegex(RuntimeError, "bytes changed"):
                recorder.audit_http()
        files[path] = original
        recorder.audit_http()

    def test_owned_application_ack_is_in_memory_before_evidence_failure(self):
        recorder = self.recorder()
        recorder.key_create_attempted = True
        response = {"Items": [{"AppName": RECORDER.APP_NAME, "AccessToken": "synthetic-owned-key"}], "TotalRecordCount": 1}
        self.wire(recorder, [FakeResponse(json.dumps(response).encode())])
        self.replace(RECORDER, "save_bytes", lambda *args: (_ for _ in ()).throw(OSError("Synthetic storage failure")))
        with self.assertRaises(OSError):
            recorder.request("key-proof", "GET", RECORDER.KEYS_PAGE)
        self.assertEqual(recorder.owned_key, response["Items"][0])
        self.assertIn("synthetic-owned-key", recorder.secrets)
        recorder.finishing = True
        self.assertEqual(recorder.authorize("DELETE", RECORDER.KEYS_ROUTE + "/synthetic-owned-key", "admin", RECORDER.MISSING), "json")

    def test_cleanup_storage_failure_still_retires_key_viewer_admin_with_reserved_requests(self):
        recorder = self.recorder()
        recorder.finishing, recorder.key_create_attempted = True, True
        recorder.owned_key = {"AccessToken": "synthetic-owned-key", "AppName": RECORDER.APP_NAME}
        recorder.record_count, recorder.charged_json = RECORDER.MAIN_REQUESTS, RECORDER.MAIN_JSON_TOTAL
        responses = [FakeResponse(b"", status=204), FakeResponse(b"", status=401),
                     FakeResponse(b'{"Items":[],"TotalRecordCount":0}'),
                     FakeResponse(b"", status=204), FakeResponse(b"", status=401),
                     FakeResponse(b"", status=204), FakeResponse(b"", status=401)]
        connections = self.wire(recorder, responses)
        self.replace(RECORDER, "save_bytes", lambda *args: (_ for _ in ()).throw(OSError("Synthetic full storage")))
        recorder.retire_key()
        recorder.logout("viewer")
        recorder.logout("admin")
        calls = [connection.calls[0] for connection in connections]
        self.assertEqual([call[0] for call in calls], ["DELETE", "GET", "GET", "POST", "GET", "POST", "GET"])
        self.assertEqual([calls[index][3]["X-Emby-Token"] for index in (1, 4, 6)],
                         ["synthetic-owned-key", recorder.token("viewer"), recorder.token("admin")])
        self.assertEqual(recorder.key_invalidity["status"], 401)
        self.assertEqual({name: proof["status"] for name, proof in recorder.invalidity.items()}, {"viewer": 401, "admin": 401})
        self.assertTrue(recorder.persistence_failures)
        self.assertFalse(recorder.cleanup_ok)
        recorder.record_count, recorder.charged_json = RECORDER.MAX_REQUESTS - 4, RECORDER.MAX_JSON_TOTAL - 4 * RECORDER.MAX_BODY
        with self.assertRaisesRegex(RuntimeError, "budget exhausted"):
            recorder.request("not-ordinary-reserve", "GET", RECORDER.KEYS_PAGE)
        reserved = self.recorder()
        reserved.finishing = True
        reserved.record_count, reserved.charged_json = RECORDER.MAX_REQUESTS - 4, RECORDER.MAX_JSON_TOTAL - 4 * RECORDER.MAX_BODY
        reserve_connections = self.wire(reserved, [FakeResponse(b"", status=204), FakeResponse(b"", status=401),
                                                  FakeResponse(b"", status=204), FakeResponse(b"", status=401)])
        reserved.logout("viewer")
        reserved.logout("admin")
        self.assertEqual((len(reserve_connections), reserved.record_count), (4, RECORDER.MAX_REQUESTS))
        slow = self.recorder()
        clock, phases = [100.0], []
        self.replace(time, "monotonic", lambda: clock[0])

        def exhausted(identity):
            phases.append((identity, clock[0], slow.deadline))
            clock[0] += 30
            raise TimeoutError("Synthetic credential phase exhausted")

        self.replace(slow, "retire_key", lambda: exhausted("application"))
        self.replace(slow, "logout", exhausted)
        self.replace(slow, "finish_evidence", lambda: self.fail("No evidence budget remains after all three phases expire"))
        with self.assertRaisesRegex(RuntimeError, "evidence phase did not complete"):
            slow.finish()
        self.assertEqual(phases, [("application", 100.0, 130.0), ("viewer", 130.0, 160.0), ("admin", 160.0, 190.0)])
        self.assertEqual([row["allocationSeconds"] for row in slow.cleanup_phases], [30, 30, 30, 30])

    def test_initialization_cleanup_never_writes_existing_evidence_and_attempts_both_tokens(self):
        recorder = self.recorder()
        connections = self.wire(recorder, [FakeResponse(b"", status=403), FakeResponse(b"", status=401),
                                           FakeResponse(b"", status=204), FakeResponse(b"", status=401)])
        self.replace(RECORDER, "save_bytes", lambda *args: self.fail("Existing capture was touched"))
        recorder.initialization_cleanup()
        self.assertEqual(len(connections), 4)
        self.assertEqual(recorder.invalidity["viewer"]["status"], 401)
        self.assertEqual(recorder.invalidity["admin"]["status"], 401)
        self.assertTrue(recorder.evidence_unavailable)
        expired = self.recorder()
        expired.finishing, expired.deadline = True, 99.0
        checked = []
        self.replace(expired, "check_authority", lambda: checked.append("unexpected expired authority probe"))
        with self.assertRaises((RuntimeError, TimeoutError)):
            expired.request("expired-phase", "GET", RECORDER.KEYS_PAGE)
        self.assertEqual(checked, [])
        slow = self.recorder()
        clock, phases = [100.0], []
        self.replace(time, "monotonic", lambda: clock[0])

        def slow_logout(identity):
            phases.append((identity, clock[0], slow.deadline))
            clock[0] += 40
            if identity == "viewer":
                raise TimeoutError("Synthetic viewer time slice exhausted")

        self.replace(slow, "logout", slow_logout)
        slow.initialization_cleanup()
        self.assertEqual(phases, [("viewer", 100.0, 140.0), ("admin", 140.0, 180.0)])

    def test_bootstrap_ack_precedes_storage_and_one_login_per_account_never_retries(self):
        bootstrap = self.bootstrap()
        fake_recorder = self.recorder()
        self.wire(fake_recorder, [FakeResponse(b'{"AccessToken":"synthetic-issued-token"}')])
        saved = []

        def atomic(path, value):
            saved.append(path)
            if path == OPERATOR.LOGIN_RESPONSES["admin"]:
                raise OSError("Synthetic acknowledgement storage failure")

        self.replace(OPERATOR, "atomic_save", atomic)
        with self.assertRaises(OSError):
            bootstrap.request("login", "POST", "/emby/Users/AuthenticateByName", account="admin",
                              body={"Username": OPERATOR.ACCOUNTS["admin"]["username"], "Pw": bootstrap.passwords["admin"]})
        self.assertEqual(bootstrap.tokens, {"admin": "synthetic-issued-token"})
        self.assertIn("synthetic-issued-token", bootstrap.secrets)
        with self.assertRaisesRegex(RuntimeError, "already attempted"):
            bootstrap.request("repeat", "POST", "/emby/Users/AuthenticateByName", account="admin",
                              body={"Username": OPERATOR.ACCOUNTS["admin"]["username"], "Pw": bootstrap.passwords["admin"]})
        self.assertEqual(saved, [OPERATOR.LOGIN_INTENTS["admin"], OPERATOR.LOGIN_RESPONSES["admin"]])

    def test_bootstrap_cleanup_has_separate_per_account_budget_despite_storage_failure(self):
        bootstrap = self.bootstrap()
        bootstrap.tokens = {"admin": "synthetic-admin", "viewer": "synthetic-viewer"}
        bootstrap.count = OPERATOR.MAX_REQUESTS
        fake_recorder = self.recorder()
        connections = self.wire(fake_recorder, [FakeResponse(b"", status=204), FakeResponse(b"", status=401),
                                               FakeResponse(b"", status=204), FakeResponse(b"", status=401)])
        self.replace(OPERATOR, "save", lambda *args: (_ for _ in ()).throw(OSError("Synthetic storage failure")))
        report = bootstrap.revoke_all()
        self.assertEqual(len(connections), 4)
        self.assertEqual(bootstrap.cleanup_by_account, {"admin": 2, "viewer": 2})
        self.assertEqual(bootstrap.revoked, {"admin", "viewer"})
        self.assertFalse(report["allInvalidOrNeverAttempted"])
        self.assertTrue(all(row["invalidityProven"] and not row["persistenceComplete"] for row in report["accounts"].values()))

    def test_operator_stops_before_failed_preservation_without_ready_or_deleting_data(self):
        saved, calls = {}, []
        present = {OPERATOR.PRIVATE, OPERATOR.INTENT, OPERATOR.DATA}
        intent = {"marker": OPERATOR.MARKER, "unit": OPERATOR.UNIT, "programData": str(OPERATOR.DATA), "evidenceRoot": str(OPERATOR.ROOT),
                  "mediaCopiesCreated": 0, "sourceDirectoriesCreated": 0, "ordinaryLoginBudget": 2, "oldServices": {}, "oldBaseline": {}}
        self.replace(os, "environ", {"SSH_CONNECTION": "synthetic"})
        self.replace(os, "geteuid", lambda: 0)
        self.replace(os, "readlink", lambda path: "net:[host]")
        self.replace(Path, "exists", lambda path: path in present)
        self.replace(OPERATOR, "canonical", lambda *args, **kwargs: None)
        self.replace(OPERATOR, "owned_root", lambda path: self.assertEqual(path, OPERATOR.ROOT))
        self.replace(OPERATOR, "recover_atomic_publications", lambda *args: None)
        self.replace(OPERATOR, "properties", lambda unit: {"MainPID": "12345"})
        self.replace(OPERATOR, "revoke_via_namespace", lambda expected: (_ for _ in ()).throw(RuntimeError("No API acknowledgement")))
        self.replace(OPERATOR, "stop_owned", lambda expected: calls.append(("stop", expected)) or {"stopped": True})
        self.replace(OPERATOR, "common_preconditions", lambda **kwargs: None)
        self.replace(OPERATOR, "recover_empty_removal_root", lambda: None)
        self.replace(OPERATOR, "load", lambda path: copy.deepcopy(intent) if path == OPERATOR.INTENT else self.fail("Unexpected READY read"))
        self.replace(OPERATOR, "check_old_services", lambda expected: None)
        self.replace(OPERATOR, "verify_baseline", lambda expected: calls.append(("preservation", None)) or (_ for _ in ()).throw(RuntimeError("Old evidence changed")))
        self.replace(OPERATOR, "remove_owned_data", lambda: self.fail("Unproven preservation cannot permit deletion"))
        self.replace(OPERATOR, "save", lambda path, value: saved.update({path: copy.deepcopy(value)}))
        self.replace(OPERATOR, "emit", lambda value: None)
        with self.assertRaisesRegex(RuntimeError, "Cleanup is incomplete"):
            OPERATOR.cleanup()
        self.assertEqual(calls, [("stop", None), ("preservation", None)])
        report = next(iter(saved.values()))
        self.assertFalse(report["dataRemoved"])
        self.assertTrue(report["stop"]["stopped"])

    def test_removal_requires_fixed_data_inode_and_evidence_files_are_exclusive(self):
        proof = {"path": str(OPERATOR.DATA), "device": 17, "inode": 29, "marker": OPERATOR.MARKER}
        current = copy.deepcopy(proof)
        self.replace(OPERATOR, "data_identity", lambda: copy.deepcopy(current))
        self.replace(OPERATOR, "load", lambda path: copy.deepcopy(proof))
        self.assertEqual(OPERATOR.removal_proof(OPERATOR.DATA)[1], proof)
        current["inode"] = 30
        with self.assertRaisesRegex(RuntimeError, "immutable creation identity"):
            OPERATOR.removal_proof(OPERATOR.DATA)
        for path in (OPERATOR.OLD_DATA, OPERATOR.PREVIOUS_DATA, OPERATOR.PREVIOUS_SOURCE, OPERATOR.ROOT, OPERATOR.ROOT / "source"):
            with self.assertRaisesRegex(RuntimeError, "fixed DATA"):
                OPERATOR.remove_owned_tree(path)
        flags = []
        self.replace(Path, "is_symlink", lambda path: False)
        self.replace(RECORDER, "canonical", lambda *args, **kwargs: None)

        def exclusive(path, value, mode):
            flags.append(value)
            raise FileExistsError("Synthetic original response already exists")

        self.replace(os, "open", exclusive)
        with self.assertRaises(FileExistsError):
            RECORDER.save_bytes(RECORDER.WIRE / "original.body", b"replacement")
        self.assertEqual(flags, [os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW])

    def test_owned_stop_uses_durable_dispatch_without_ready_or_current_launcher_bytes(self):
        state = {"Id": OPERATOR.UNIT, "Description": OPERATOR.DESCRIPTION, "WorkingDirectory": str(OPERATOR.RUNTIME),
                 "ExecStart": "{ path=" + str(OPERATOR.RUNTIME / "launch.sh") + " ; argv[]=owned ; }",
                 "ControlGroup": "/system.slice/" + OPERATOR.UNIT, "InvocationID": "a" * 32,
                 "LoadState": "loaded", "ActiveState": "activating", "MainPID": "12345"}
        process = {"pid": 12345, "uid": 0, "startTicks": "56789", "exe": "/usr/bin/bash",
                   "cgroup": "0::/system.slice/" + OPERATOR.UNIT, "cmdline": ["/bin/bash", str(OPERATOR.RUNTIME / "launch.sh")]}
        proof, calls = {"launcherSha256": "b" * 64}, []
        self.replace(OPERATOR, "owned_authority", lambda: None)
        self.replace(Path, "exists", lambda path: path == OPERATOR.DISPATCH)
        self.replace(OPERATOR, "load", lambda path: copy.deepcopy(proof) if path == OPERATOR.DISPATCH else self.fail("Stop read READY or launcher bytes"))
        self.replace(OPERATOR, "properties", lambda unit: copy.deepcopy(state) if unit == OPERATOR.UNIT else self.fail("Foreign service"))

        def process_identity(pid):
            self.assertEqual(pid, 12345)
            if state["MainPID"] == "0":
                raise FileNotFoundError("Synthetic exited launcher")
            return copy.deepcopy(process)

        def stop(arguments, **kwargs):
            self.assertEqual(arguments, ["systemctl", "stop", OPERATOR.UNIT])
            calls.append(arguments)
            state.update({"MainPID": "0", "ActiveState": "inactive"})

        self.replace(OPERATOR, "process_identity", process_identity)
        self.replace(OPERATOR, "run", stop)
        proof.update(OPERATOR.dispatch_identity(state, stopping=True))
        self.assertTrue(OPERATOR.stop_owned(None)["stopped"])
        self.assertEqual(len(calls), 1)
        state.update({"MainPID": "12345", "ActiveState": "activating"})
        for field, bad in (("InvocationID", "c" * 32), ("ControlGroup", "/system.slice/foreign.service"),
                           ("WorkingDirectory", str(OPERATOR.APP)), ("ExecStart", "{ path=/bin/foreign ; }")):
            with self.subTest(field=field):
                previous, state[field] = state[field], bad
                with self.assertRaises(RuntimeError):
                    OPERATOR.stop_owned(None)
                state[field] = previous
        with self.assertRaisesRegex(RuntimeError, "replacement process"):
            OPERATOR.stop_owned({"pid": 12345, "startTicks": "different"})
        self.assertEqual(len(calls), 1)


def main():
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION") or len(sys.argv) != 2:
        print(json.dumps({"suite": "observability-reference-guards", "result": "blocked",
                          "reason": "Authorized root SSH and one observability recorder source path are required"}))
        raise SystemExit(2)
    source = Path(sys.argv[1]).resolve(strict=True)
    operator = source.with_name("prepare-observability-fresh.py")
    paths = [source, operator, *[source.with_name(name) for name in
             ("reference-configuration.py", "reference-scheduled-tasks.py", "reference-devices.py", "reference-api-keys.py")]]
    contents = {str(path): path.read_bytes() for path in paths}
    suite_bytes = Path(__file__).read_bytes()
    for filename, content in contents.items():
        SOURCE_LINES[filename] = content.decode().splitlines(keepends=True)
        SOURCE_HASHES[Path(filename).name] = hashlib.sha256(content).hexdigest()
    SOURCE_LINES[__file__] = suite_bytes.decode().splitlines(keepends=True)
    code = {}

    class MemoryLoader:
        def __init__(self, filename):
            if filename not in code:
                raise AssertionError("Unexpected study source: " + filename)
            self.filename = filename

        def create_module(self, specification):
            return None

        def exec_module(self, module):
            module.__file__ = self.filename
            exec(code[self.filename], module.__dict__)

    def specification(name, filename, **kwargs):
        if kwargs:
            raise AssertionError("Unexpected source import options")
        return importlib.util.spec_from_loader(name, MemoryLoader(str(filename)), origin=str(filename))

    def cached_path(path, **kwargs):
        if str(path) not in contents:
            raise AssertionError("Unexpected import path inspection: " + str(path))
        return path

    global RECORDER, OPERATOR
    RECORDER, OPERATOR = types.ModuleType("synthetic_observability_recorder"), types.ModuleType("synthetic_observability_operator")
    RECORDER.__file__, OPERATOR.__file__ = str(source), str(operator)
    result, import_failure = unittest.TestResult(), None
    with memory_tracebacks():
        try:
            code.update({filename: compile(content, filename, "exec") for filename, content in contents.items()})
            with EffectFence(), patch.object(importlib.util, "spec_from_file_location", specification), \
                    patch.object(os, "environ", {"GOBY_DEVICE_REFERENCE_RUN": "2"}), \
                    patch.object(Path, "resolve", cached_path), \
                    patch.object(Path, "is_symlink", lambda path: False if str(cached_path(path)) in contents else True), \
                    patch.object(Path, "read_bytes", lambda path: contents[str(cached_path(path))]):
                exec(code[str(operator)], OPERATOR.__dict__)
                exec(code[str(source)], RECORDER.__dict__)
        except BaseException as error:
            import_failure = {"type": type(error).__name__, "message": str(error)[:1000]}
        else:
            unittest.defaultTestLoader.loadTestsFromTestCase(ObservabilityGuards).run(result)
    failures = [*result.failures, *result.errors]
    summaries = [{"test": test.id(), "message": detail.rstrip().splitlines()[-1]} for test, detail in failures[:8]]
    passed = import_failure is None and result.wasSuccessful() and result.testsRun > 0
    print(json.dumps({"suite": "observability-reference-guards", "result": "passed" if passed else "failed",
        "tests": result.testsRun, "failures": len(result.failures), "errors": len(result.errors), "skipped": len(result.skipped),
        "sourceSha256": SOURCE_HASHES, "testSha256": hashlib.sha256(suite_bytes).hexdigest(),
        "httpRequests": 0, "subprocesses": 0, "captureWrites": 0, "fixtures": "synthetic-memory-only",
        "importFailure": import_failure, "failureSummaries": summaries, "failureSummariesOmitted": max(0, len(failures) - 8)}))
    raise SystemExit(0 if passed else 1)


if __name__ == "__main__":
    main()
