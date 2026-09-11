#!/usr/bin/env python3
"""Exercise fresh client-reference guards with memory dependencies only.

Run through root SSH on test-env. The suite never starts a service, reads old
reference data, contacts an API, or changes any fixture media.
"""

import contextlib
import copy
import fcntl
import hashlib
import http.client
import importlib.util
import io
import json
import linecache
import os
from pathlib import Path
import socket
import stat
import subprocess
import sys
import time
import types
import unittest
from unittest.mock import Mock, patch

sys.dont_write_bytecode = True
OPERATOR = None
SOURCES = {}


class Fence(contextlib.ExitStack):
    def __enter__(self):
        super().__enter__()
        import builtins
        self.violations = []
        self.enter_context(patch.object(linecache, "checkcache", lambda filename=None: None))
        self.enter_context(patch.object(linecache, "getlines", lambda filename, module_globals=None: SOURCES.get(str(filename), [])))
        targets = ((builtins, ("open",)), (io, ("open", "FileIO")),
                   (os, ("open", "close", "fdopen", "fstat", "stat", "lstat", "readlink", "mkdir", "chmod", "chown",
                         "fsync", "replace", "unlink", "remove", "rmdir", "umask", "kill", "system", "popen")),
                   (Path, ("lstat", "stat", "exists", "is_symlink", "resolve", "readlink", "read_bytes", "read_text",
                           "write_bytes", "write_text", "mkdir", "iterdir", "rglob", "unlink", "open", "is_file")),
                   (subprocess, ("run", "Popen", "call", "check_call", "check_output")),
                   (socket, ("socket", "create_connection", "getaddrinfo")),
                   (http.client, ("HTTPConnection", "HTTPSConnection")), (fcntl, ("flock",)), (time, ("sleep",)))
        for owner, names in targets:
            for name in names:
                label = owner.__name__ + "." + name

                def denied(*args, _label=label, **kwargs):
                    self.violations.append(_label)
                    raise AssertionError("Unmocked external effect: " + _label)

                self.enter_context(patch.object(owner, name, denied))
        return self

    def __exit__(self, *args):
        super().__exit__(*args)
        if self.violations:
            raise AssertionError("Unexpected effects: " + ", ".join(self.violations))


class ReferenceGuards(unittest.TestCase):
    def setUp(self):
        self.enterContext(Fence())
        self.output = self.enterContext(contextlib.redirect_stdout(io.StringIO()))
        self.owner = {"marker": OPERATOR.MARKER, "unit": OPERATOR.UNIT, "data": str(OPERATOR.DATA),
                      "binary": str(OPERATOR.BINARY), "binarySha256": OPERATOR.BINARY_SHA256, "port": OPERATOR.PORT,
                      "phase": "ready", "media": {"files": {}}, "serviceIdentity": {"pid": 42, "startTicks": "12345",
                      "networkNamespace": "net:[123]"}, "serverId": "owned-server", "reportSha256": "report", "browserSha256": "browser"}
        self.api = OPERATOR.API.__new__(OPERATOR.API)
        self.api.state = {"serverId": "owned-server", "userIds": {"admin": "a" * 32, "viewer": "b" * 32, "viewer2": "c" * 32}}
        self.api.credentials = {"marker": OPERATOR.MARKER, "accounts": {
            key: {"username": name, "password": key + "-synthetic-secret"} for key, name in OPERATOR.NAMES.items()}}

    def replace(self, owner, name, value):
        return self.enterContext(patch.object(owner, name, value))

    def reject(self, callback):
        with self.assertRaises(OPERATOR.ReferenceError):
            callback()

    def metadata(self, kind=stat.S_IFREG, mode=0o600, uid=0, gid=0, links=1):
        return types.SimpleNamespace(st_mode=kind | mode, st_uid=uid, st_gid=gid, st_nlink=links,
                                     st_dev=41, st_ino=51, st_size=20)

    def test_owner_rejects_changed_marker_path_binary_unit_and_port(self):
        OPERATOR.validate_owner(self.owner)
        for key in ("marker", "unit", "data", "binary", "binarySha256", "port"):
            self.reject(lambda key=key: OPERATOR.validate_owner(dict(self.owner, **{key: "foreign"})))

    def test_canonical_rejects_symlink_ancestor(self):
        self.replace(Path, "lstat", lambda path: self.metadata(kind=stat.S_IFLNK if path == OPERATOR.WORK else stat.S_IFDIR))
        self.reject(lambda: OPERATOR.canonical(OPERATOR.DATA, directory=True))

    def test_canonical_rejects_wrong_owner_mode_and_hardlinks(self):
        for info in (self.metadata(uid=10), self.metadata(gid=10), self.metadata(mode=0o666), self.metadata(links=2)):
            self.replace(Path, "lstat", lambda path, info=info: info if path == OPERATOR.OWNER else self.metadata(kind=stat.S_IFDIR, mode=0o755))
            self.reject(lambda: OPERATOR.canonical(OPERATOR.OWNER, mode=0o600))

    def test_dangling_symlink_counts_as_occupied(self):
        self.replace(Path, "lstat", lambda path: self.metadata(kind=stat.S_IFLNK))
        self.assertTrue(OPERATOR.present(OPERATOR.DATA))

    def test_unowned_authority_file_cannot_be_replaced(self):
        self.reject(lambda: OPERATOR.update(OPERATOR.CREDENTIALS, {}))
        self.reject(lambda: OPERATOR.update(OPERATOR.WORK / "OWNER.json", {}))

    def test_http_refuses_unattested_mutations(self):
        self.api.state.pop("serverId")
        self.reject(lambda: self.api.approve("POST", "/emby/Library/Refresh", None))

    def test_http_refuses_foreign_origin_routes_and_ids(self):
        for method, path in (("GET", "https://foreign.example/emby/Users"), ("GET", "/emby/Users/foreign/Items"),
                             ("POST", "/emby/System/Restart"), ("DELETE", "/emby/Users/" + "b" * 32),
                             ("POST", "/emby/Auth/Keys"), ("POST", "/emby/ScheduledTasks/Running/foreign")):
            self.reject(lambda method=method, path=path: self.api.approve(method, path, None))

    def test_http_refuses_malformed_owned_user_id(self):
        self.api.state["userIds"]["viewer"] = "../foreign"
        self.reject(lambda: self.api.approve("GET", "/emby/Users/../foreign/Items", None))

    def test_http_allows_only_persisted_login_credentials(self):
        for key, credential in self.api.credentials["accounts"].items():
            body = {"Username": credential["username"], "Pw": credential["password"]}
            self.api.approve("POST", "/emby/Users/AuthenticateByName", body)
            self.reject(lambda body=body: self.api.approve("POST", "/emby/Users/AuthenticateByName", dict(body, Pw="foreign")))

    def test_http_allows_only_planned_account_creation(self):
        self.api.approve("POST", "/emby/Users/New", {"Name": OPERATOR.NAMES["viewer"]})
        self.reject(lambda: self.api.approve("POST", "/emby/Users/New", {"Name": "foreign"}))
        self.reject(lambda: self.api.approve("POST", "/emby/Users/New", {"Name": OPERATOR.NAMES["admin"]}))

    def test_http_refuses_remote_access_enablement(self):
        self.api.approve("POST", "/emby/Startup/RemoteAccess", {"EnableAutomaticPortMapping": "false"})
        self.reject(lambda: self.api.approve("POST", "/emby/Startup/RemoteAccess", {"EnableAutomaticPortMapping": "true"}))

    def test_http_refuses_different_startup_credentials(self):
        admin = self.api.credentials["accounts"]["admin"]
        self.api.approve("POST", "/emby/Startup/User", {"Name": admin["username"], "Password": admin["password"]})
        self.reject(lambda: self.api.approve("POST", "/emby/Startup/User", {"Name": "foreign", "Password": "foreign"}))

    def test_http_refuses_password_rotation_and_admin_password_mutation(self):
        user_id = self.api.state["userIds"]["viewer"]
        body = {"Id": user_id, "NewPw": self.api.credentials["accounts"]["viewer"]["password"], "ResetPassword": False}
        self.api.approve("POST", f"/emby/Users/{user_id}/Password", body)
        self.reject(lambda: self.api.approve("POST", f"/emby/Users/{user_id}/Password", dict(body, NewPw="foreign")))
        self.reject(lambda: self.api.approve("POST", "/emby/Users/" + "a" * 32 + "/Password", body))

    def test_http_refuses_privilege_elevation_and_lost_viewer_access(self):
        body = {"IsAdministrator": False, "IsDisabled": False, "EnableAllFolders": True, "EnableMediaPlayback": True}
        route = "/emby/Users/" + "b" * 32 + "/Policy"
        self.api.approve("POST", route, body)
        for key in body:
            self.reject(lambda key=key: self.api.approve("POST", route, dict(body, **{key: not body[key]})))

    def test_libraries_are_exact_owned_sources_with_no_provider_or_write_flags(self):
        for name in OPERATOR.LIBRARIES:
            body = OPERATOR.library_body(name)
            self.api.approve("POST", "/emby/Library/VirtualFolders", body)
            self.assertEqual(body["Paths"], [str(OPERATOR.MEDIA / name)])
            options = body["LibraryOptions"]
            self.assertEqual(options["PathInfos"], [{"Path": str(OPERATOR.MEDIA / name)}])
            for key in ("EnableRealtimeMonitor", "SaveLocalMetadata", "SaveSubtitlesWithMedia", "SaveLyricsWithMedia"):
                self.assertIs(options[key], False)
            self.assertEqual(options["SampleIgnoreSize"], 0)
            self.assertTrue(all(row["MetadataFetchers"] == row["ImageFetchers"] == [] for row in options["TypeOptions"]))

    def test_library_mutation_refuses_foreign_path_provider_or_realtime_monitor(self):
        body = OPERATOR.library_body("Movies")
        foreign = copy.deepcopy(body)
        foreign["Paths"] = ["/opt/goby-test/emby-reference-data"]
        provider = copy.deepcopy(body)
        provider["LibraryOptions"]["TypeOptions"][0]["MetadataFetchers"] = ["Internet"]
        realtime = copy.deepcopy(body)
        realtime["LibraryOptions"]["EnableRealtimeMonitor"] = True
        for candidate in (foreign, provider, realtime):
            self.reject(lambda candidate=candidate: self.api.approve("POST", "/emby/Library/VirtualFolders", candidate))

    def test_library_verification_rejects_wrong_source_and_enabled_provider(self):
        body = OPERATOR.library_body("Movies")
        row = {"Name": body["Name"], "CollectionType": body["CollectionType"], "Locations": body["Paths"],
               "ItemId": "123", "LibraryOptions": body["LibraryOptions"]}
        OPERATOR.verify_library(row, "Movies")
        self.reject(lambda: OPERATOR.verify_library(dict(row, Locations=["/foreign"]), "Movies"))
        row["LibraryOptions"]["TypeOptions"][0]["ImageFetchers"] = ["Internet"]
        self.reject(lambda: OPERATOR.verify_library(row, "Movies"))

    def test_namespace_is_checked_before_http_connection(self):
        self.api.owner = self.owner
        self.api.count = 0
        self.api.deadline = float("inf")
        self.replace(OPERATOR, "same_service", Mock())
        self.replace(os, "readlink", lambda path: "net:[999]")
        self.reject(lambda: self.api.request("test", "GET", "/emby/System/Info/Public"))

    def test_service_change_is_checked_before_http_connection(self):
        self.api.owner = self.owner
        self.api.count = 0
        self.api.deadline = float("inf")
        self.replace(OPERATOR, "same_service", Mock(side_effect=OPERATOR.ReferenceError("changed")))
        self.reject(lambda: self.api.request("test", "GET", "/emby/System/Info/Public"))

    def test_unknown_service_unit_is_never_started(self):
        self.replace(OPERATOR, "properties", lambda unit: {"LoadState": "loaded"})
        self.reject(OPERATOR.start_service)

    def test_command_failure_does_not_expose_private_output(self):
        self.replace(subprocess, "run", lambda *args, **kwargs: types.SimpleNamespace(returncode=1, stdout=b"secret", stderr=b"secret"))
        with self.assertRaises(OPERATOR.ReferenceError) as caught:
            OPERATOR.run(["synthetic"])
        self.assertNotIn("secret", str(caught.exception))

    def test_process_identity_rejects_invalid_pid(self):
        self.reject(lambda: OPERATOR.process_identity(0))
        self.reject(lambda: OPERATOR.process_identity(1))
        self.reject(lambda: OPERATOR.process_identity(True))

    def test_main_refuses_local_execution_before_effects(self):
        self.replace(sys, "platform", "win32")
        self.reject(lambda: OPERATOR.main([]))

    def test_main_refuses_unknown_mode_before_effects(self):
        self.reject(lambda: OPERATOR.main(["--reset"]))

    def main_fixture(self, fresh=False):
        self.replace(OPERATOR, "preconditions", Mock())
        self.replace(os, "umask", Mock())
        self.replace(OPERATOR, "present", lambda path: not fresh)
        self.replace(OPERATOR, "canonical", Mock())
        self.replace(os, "open", lambda *args: 45)
        self.replace(os, "close", Mock())
        self.replace(fcntl, "flock", Mock())
        self.replace(OPERATOR, "read_private", lambda path: self.owner)
        self.replace(OPERATOR, "same_service", Mock())
        self.replace(OPERATOR, "verify_media", lambda: self.owner["media"])
        self.replace(OPERATOR, "digest", lambda path: "report" if path == OPERATOR.REPORT else "browser")

    def test_main_check_does_not_create_fresh_fixture(self):
        self.main_fixture(fresh=True)
        self.reject(lambda: OPERATOR.main(["--check"]))

    def test_main_refuses_unknown_data_or_evidence_before_creation(self):
        self.main_fixture(fresh=True)
        for target in (OPERATOR.DATA, OPERATOR.CREDENTIALS, OPERATOR.BROWSER, OPERATOR.HTTP_LOG, OPERATOR.LAUNCHER, OPERATOR.LOCK):
            self.replace(OPERATOR, "present", lambda path, target=target: path == target)
            self.reject(lambda: OPERATOR.main([]))

    def test_ready_reuse_never_starts_service_or_reauthenticates(self):
        self.main_fixture()
        start = self.replace(OPERATOR, "start_service", Mock())
        child = self.replace(OPERATOR, "bootstrap_child", Mock())
        OPERATOR.main([])
        OPERATOR.main(["--check"])
        start.assert_not_called()
        child.assert_not_called()

    def test_incomplete_initialization_is_not_automatically_erased_or_adopted(self):
        self.main_fixture()
        self.owner["phase"] = "preparing"
        self.reject(lambda: OPERATOR.main([]))

    def test_lock_contention_rejects_before_service_actions(self):
        self.main_fixture()
        self.replace(fcntl, "flock", Mock(side_effect=BlockingIOError()))
        self.reject(lambda: OPERATOR.main([]))

    def test_ready_fixture_cannot_reenter_internal_bootstrap(self):
        self.main_fixture()
        self.reject(lambda: OPERATOR.main(["_bootstrap"]))

    def test_bootstrap_logout_requires_token_invalidity(self):
        self.api.tokens = {"viewer": "synthetic-token"}
        self.api.state["tokens"] = [{"account": "viewer", "token": "synthetic-token", "revoked": False}]
        self.replace(self.api, "persist", Mock())
        self.replace(self.api, "request", Mock(side_effect=[(204, None), (200, {})]))
        self.reject(lambda: self.api.logout("viewer"))
        self.assertNotIn("viewer", self.api.state.get("logoutVerified", {}))

    def test_bootstrap_logout_records_confirmed_revocation(self):
        self.api.tokens = {"viewer": "synthetic-token"}
        self.api.state["tokens"] = [{"account": "viewer", "token": "synthetic-token", "revoked": False}]
        self.replace(self.api, "persist", Mock())
        self.replace(self.api, "request", Mock(side_effect=[(204, None), (401, None)]))
        self.api.logout("viewer")
        self.assertTrue(self.api.state["logoutVerified"]["viewer"])
        self.assertTrue(self.api.state["tokens"][0]["revoked"])

    def test_older_confirmed_token_prevents_premature_account_revocation_claim(self):
        self.api.tokens = {"viewer": "new-token"}
        self.api.state["tokens"] = [{"account": "viewer", "token": "old-token", "revoked": False},
                                    {"account": "viewer", "token": "new-token", "revoked": False}]
        self.replace(self.api, "persist", Mock())
        self.replace(self.api, "request", Mock(side_effect=[(204, None), (401, None), (401, None), (401, None)]))
        self.api.logout("viewer")
        self.assertFalse(self.api.state["logoutVerified"]["viewer"])
        self.api.revoke("viewer", "old-token")
        self.assertTrue(self.api.state["logoutVerified"]["viewer"])

    def test_request_sequence_is_durable_before_connection_dispatch(self):
        self.api.owner = self.owner
        self.api.tokens = {}
        self.api.count = 0
        self.api.deadline = float("inf")
        self.replace(OPERATOR, "same_service", Mock())
        self.replace(os, "readlink", lambda path: "net:[123]")
        persisted = []
        self.replace(self.api, "persist", lambda: persisted.append(copy.deepcopy(self.api.state)))

        def unavailable(*args, **kwargs):
            self.assertEqual(persisted[-1]["requestCount"], 1)
            raise OPERATOR.ReferenceError("Synthetic interruption after durable reservation")

        self.replace(http.client, "HTTPConnection", unavailable)
        self.reject(lambda: self.api.request("test", "GET", "/emby/System/Info/Public"))

    def service_fixture(self):
        owner = dict(self.owner, workIdentity={"inode": 51}, dataIdentity={"inode": 61}, launcherSha256="launcher",
                     protectedReference={"process": {"networkNamespace": "net:[20]"}})
        state = {"ActiveState": "active", "MainPID": "42", "Id": OPERATOR.UNIT, "Description": OPERATOR.DESCRIPTION,
                 "PrivateNetwork": "yes", "PrivateTmp": "yes", "NoNewPrivileges": "yes", "ProtectSystem": "strict",
                 "ProtectHome": "yes", "ReadWritePaths": str(OPERATOR.DATA), "WorkingDirectory": str(OPERATOR.DATA),
                 "ReadOnlyPaths": " ".join(map(str, (OPERATOR.WORK, OPERATOR.APP, OPERATOR.MEDIA))),
                 "MemoryMax": str(1024 ** 3), "TasksMax": "256", "LimitNOFILE": "65536", "UMask": "0077",
                 "KillMode": "control-group", "CapabilityBoundingSet": "", "ControlGroup": "/system.slice/" + OPERATOR.UNIT,
                 "DropInPaths": "", "TimeoutStopUSec": "25s", "User": "root", "CPUQuotaPerSecUSec": "1.5s",
                 "ExecStart": "{ path=" + str(OPERATOR.LAUNCHER) + " ; argv[]=owned ; }",
                 "FragmentPath": "/run/systemd/transient/" + OPERATOR.UNIT, "InvocationID": "d" * 32}
        process = dict(self.owner["serviceIdentity"], uid=0, exe=str(OPERATOR.BINARY), cmdline=OPERATOR.service_arguments(),
                       cgroup="0::/system.slice/" + OPERATOR.UNIT + "\n")
        self.replace(OPERATOR, "work_identity", lambda: owner["workIdentity"])
        self.replace(OPERATOR, "data_identity", lambda: owner["dataIdentity"])
        self.replace(OPERATOR, "digest", lambda path: "launcher")
        self.replace(OPERATOR, "properties", lambda unit: state)
        self.replace(OPERATOR, "process_identity", lambda pid: copy.deepcopy(process))
        self.replace(os, "readlink", lambda path: "net:[1]")
        self.replace(Path, "read_text", lambda path: "1 0 0:1 / / ro - ext4 /dev/test ro\n2 0 0:1 / " + str(OPERATOR.DATA) + " rw - ext4 /dev/test rw\n")
        return owner, state, process

    def test_service_accepts_exact_process_and_readonly_sandbox(self):
        owner, state, process = self.service_fixture()
        self.assertEqual(OPERATOR.service_identity(owner), dict(process, invocationId="d" * 32))

    def test_service_rejects_changed_work_directory_identity(self):
        owner, state, process = self.service_fixture()
        self.replace(OPERATOR, "work_identity", lambda: {"inode": 99})
        self.reject(lambda: OPERATOR.service_identity(owner))

    def test_service_rejects_weaker_sandbox_and_different_executable(self):
        for key, value in (("PrivateNetwork", "no"), ("CapabilityBoundingSet", "cap_sys_admin"),
                           ("ReadWritePaths", "/opt"), ("MemoryMax", "infinity"), ("CPUQuotaPerSecUSec", "infinity"),
                           ("DropInPaths", "/foreign.conf")):
            owner, state, process = self.service_fixture()
            state[key] = value
            self.reject(lambda: OPERATOR.service_identity(owner))
        owner, state, process = self.service_fixture()
        process["exe"] = "/foreign"
        self.reject(lambda: OPERATOR.service_identity(owner))

    def test_service_rejects_shared_namespace_or_changed_start_ticks(self):
        for namespace in ("net:[1]", "net:[20]"):
            owner, state, process = self.service_fixture()
            process["networkNamespace"] = namespace
            self.reject(lambda: OPERATOR.service_identity(owner))
        owner, state, process = self.service_fixture()
        identities = iter((process, dict(process, startTicks="999")))
        self.replace(OPERATOR, "process_identity", lambda pid: next(identities))
        self.reject(lambda: OPERATOR.service_identity(owner))

    def test_service_rejects_actual_writable_media_mount(self):
        owner, state, process = self.service_fixture()
        self.replace(Path, "read_text", lambda path: "1 0 0:1 / / rw - ext4 /dev/test rw\n")
        self.reject(lambda: OPERATOR.service_identity(owner))

    def media_fixture(self, group_links):
        names = [".goby-managed", "Movies/a.mp4", "TV/b.mp4", "TV/c.mp4", "TV/d.mp4"] + ["Music/file-" + str(index) for index in range(9)]
        records = {name: "synthetic-hash-" + name for name in names}
        manifest = {"marker": OPERATOR.MEDIA_MARKER, "files": records}
        metadata = {}
        for index, name in enumerate(names):
            info = self.metadata()
            info.st_ino = 500 if 1 <= index <= 4 else 600 + index
            info.st_nlink = group_links if 1 <= index <= 4 else 1
            metadata[OPERATOR.MEDIA / name] = info
        self.replace(OPERATOR, "canonical", Mock())
        self.replace(Path, "read_text", lambda path, **kwargs: json.dumps(manifest) if path.name == "manifest.json" else OPERATOR.MEDIA_MARKER)
        self.replace(Path, "rglob", lambda path, pattern: iter(metadata))
        self.replace(Path, "lstat", lambda path: metadata[path])
        self.replace(Path, "is_file", lambda path: True)
        self.replace(OPERATOR, "digest", lambda path, **kwargs: "manifest-hash" if path.name == "manifest.json" else records[str(path.relative_to(OPERATOR.MEDIA))])
        return records

    def test_media_accepts_hardlinks_closed_within_complete_owned_manifest(self):
        expected = self.media_fixture(4)
        self.assertEqual(OPERATOR.verify_media()["files"], expected)

    def test_media_rejects_any_unaccounted_external_hardlink(self):
        self.media_fixture(5)
        self.reject(OPERATOR.verify_media)


def main():
    global OPERATOR
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION"):
        raise SystemExit("Run reference guard tests only through authorized root SSH.")
    if len(sys.argv) != 2:
        raise SystemExit("Usage: test-prepare-client-reference.py OPERATOR_SOURCE")
    source = Path(sys.argv[1])
    for path in (source, Path(__file__)):
        SOURCES[str(path)] = path.read_text(encoding="utf-8").splitlines(keepends=True)
    spec = importlib.util.spec_from_file_location("client_reference_operator", source)
    OPERATOR = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(OPERATOR)
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(ReferenceGuards))
    print("Memory-only guards: no service start, reference-data access, API request, or media mutation occurred.")
    raise SystemExit(0 if result.wasSuccessful() else 1)


if __name__ == "__main__":
    main()
