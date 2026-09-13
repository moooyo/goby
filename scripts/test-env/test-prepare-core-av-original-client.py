#!/usr/bin/env python3
"""Check pure original-client hosting guards in an owned remote report scope.

The suite does not extract a package, start a service, enter a namespace, make
HTTP requests, or inspect vendor assets or databases. Fixture files are confined
to temporary children of the explicitly supplied private report directory.
"""

from __future__ import annotations

import argparse
from copy import deepcopy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import stat
import sys
import tempfile
import unittest


sys.dont_write_bytecode = True
SOURCE = None
REPORT_PARENT = None
EXPECTED_ROOT = Path("/opt/goby-test/exec-work-m3e/core-av-original-client-hosting-01")
EXPECTED_APP = EXPECTED_ROOT / "package/opt/emby-server"
EXPECTED_DATA = EXPECTED_ROOT / "programdata"
EXPECTED_UNIT = "goby-core-av-original-client-01.service"
EXPECTED_LAUNCHER = EXPECTED_ROOT / "launch.sh"


def unit_fixture():
    return {
        "Id": EXPECTED_UNIT, "LoadState": "loaded", "ActiveState": "inactive", "SubState": "dead", "MainPID": "0",
        "FragmentPath": "/etc/systemd/system/" + EXPECTED_UNIT,
        "DropInPaths": "", "Restart": "no", "NRestarts": "0", "PrivateNetwork": "yes",
        "PrivateTmp": "yes", "PrivateDevices": "yes", "NoNewPrivileges": "yes", "ProtectSystem": "strict",
        "ProtectHome": "yes", "ReadWritePaths": str(EXPECTED_DATA), "ReadOnlyPaths": str(EXPECTED_ROOT),
        "WorkingDirectory": str(EXPECTED_DATA), "User": "root", "Group": "root", "MemoryMax": "1073741824",
        "TasksMax": "256", "CapabilityBoundingSet": "",
        "ExecStart": "{ path=" + str(EXPECTED_LAUNCHER) + " ; argv[]=" + str(EXPECTED_LAUNCHER) +
            " ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }",
    }


class HostingGuardTests(unittest.TestCase):
    def test_01_fixed_static_unit_contract_is_accepted_without_mutation(self):
        observed = unit_fixture()
        original = deepcopy(observed)
        self.assertIsNone(SOURCE.verify_unit(observed))
        self.assertEqual(observed, original)
        self.assertEqual(observed["MainPID"], "0")
        self.assertEqual(observed["ActiveState"], "inactive")

    def test_02_restart_launcher_and_writable_scope_drift_are_rejected(self):
        for field, value in (
            ("Restart", "always"),
            ("ExecStart", "{ path=/opt/goby-test/exec-work-m3e/reference-launch.sh ; argv[]=/opt/goby-test/exec-work-m3e/reference-launch.sh ; }"),
            ("ReadWritePaths", str(EXPECTED_DATA) + " /opt/goby-test/exec-work-m3e/reference-data"),
        ):
            with self.subTest(field=field):
                observed = unit_fixture()
                observed[field] = value
                with self.assertRaisesRegex(SOURCE.PreparationError, "unit_contract_changed"):
                    SOURCE.verify_unit(observed)

    def test_03_canonical_and_exclusive_save_preserve_existing_bytes(self):
        temporary = tempfile.TemporaryDirectory(prefix="original-client-guard-", dir=REPORT_PARENT)
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        root.chmod(0o700)
        self.assertEqual(root.parent, REPORT_PARENT)
        self.assertTrue(stat.S_ISDIR(SOURCE.canonical(root, directory=True).st_mode))
        target = root / "original.txt"
        original = b"synthetic original bytes\n"
        receipt = SOURCE.save(target, original.decode())
        self.assertEqual(receipt, {"path": str(target), "sha256": hashlib.sha256(original).hexdigest()})
        self.assertEqual(target.read_bytes(), original)
        self.assertEqual(stat.S_IMODE(SOURCE.canonical(target).st_mode), 0o600)
        with self.assertRaises(FileExistsError):
            SOURCE.save(target, "replacement bytes")
        self.assertEqual(target.read_bytes(), original)

        alias = root / "alias.txt"
        alias.symlink_to(target)
        with self.assertRaisesRegex(SOURCE.PreparationError, "path_not_protected"):
            SOURCE.canonical(alias)
        with self.assertRaises(FileExistsError):
            SOURCE.save(alias, "replacement through a symlink")
        self.assertEqual(target.read_bytes(), original)
        self.assertTrue(alias.is_symlink())

        child = root / "child"
        child.mkdir(mode=0o700)
        linked_parent = root / "linked-parent"
        linked_parent.symlink_to(child, target_is_directory=True)
        with self.assertRaisesRegex(SOURCE.PreparationError, "path_not_protected"):
            SOURCE.save(linked_parent / "forbidden.txt", "unexpected new bytes")
        self.assertFalse((child / "forbidden.txt").exists())
        self.assertEqual(target.read_bytes(), original)

    def test_04_service_arguments_use_only_the_fresh_package_and_programdata(self):
        expected = [str(EXPECTED_APP / "system/EmbyServer"), "-programdata", str(EXPECTED_DATA),
            "-ffdetect", str(EXPECTED_APP / "bin/ffdetect"), "-ffmpeg", str(EXPECTED_APP / "bin/ffmpeg"),
            "-ffprobe", str(EXPECTED_APP / "bin/ffprobe"), "-restartexitcode", "3",
            "-updatepackage", "emby-server-deb_{version}_amd64.deb"]
        actual = SOURCE.service_arguments()
        self.assertEqual(actual, expected)
        self.assertEqual(SOURCE.APP, EXPECTED_APP)
        self.assertEqual(SOURCE.DATA, EXPECTED_DATA)
        self.assertEqual(SOURCE.BINARY, EXPECTED_APP / "system/EmbyServer")
        for value in actual:
            self.assertFalse(any(path in value for path in (
                "/dev/shm/goby-emby-reference", "/opt/goby-test/exec-work-m3e/reference-data",
                "/opt/goby-test/emby-reference-data")))


class GuardResult(unittest.TestResult):
    def __init__(self):
        super().__init__()
        self.records = []

    def addSuccess(self, test):
        super().addSuccess(test)
        self.records.append({"test": test.id(), "outcome": "passed"})

    def addFailure(self, test, error):
        super().addFailure(test, error)
        self.records.append({"test": test.id(), "outcome": "failed", "exceptionType": error[0].__name__})

    def addError(self, test, error):
        super().addError(test, error)
        self.records.append({"test": test.id(), "outcome": "error", "exceptionType": error[0].__name__})

    def addSubTest(self, test, subtest, error):
        super().addSubTest(test, subtest, error)
        if error is not None:
            self.records.append({"test": subtest.id(), "outcome": "failed", "exceptionType": error[0].__name__})


def protected_report_parent(path):
    if not path.is_absolute() or ".." in path.parts:
        raise RuntimeError("The report parent must be absolute and normalized")
    for current in (path, *path.parents):
        info = current.lstat()
        if (not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or info.st_mode & 0o022):
            raise RuntimeError("The report parent and its ancestors must be protected root-owned directories")
    if stat.S_IMODE(path.stat().st_mode) != 0o700:
        raise RuntimeError("The report parent must be owner-only")


def main():
    global SOURCE, REPORT_PARENT
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, default=Path(__file__).with_name("prepare-core-av-original-client.py"))
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if sys.platform != "linux" or os.geteuid() != 0 or os.getegid() != 0:
        raise RuntimeError("Use the authorized remote Linux root environment")
    REPORT_PARENT = args.output.parent
    protected_report_parent(REPORT_PARENT)
    if os.path.lexists(args.output):
        raise RuntimeError("The report output must be fresh")
    os.umask(0o077)
    source_path = args.source.resolve(strict=True)
    source_bytes = source_path.read_bytes()
    source_sha = hashlib.sha256(source_bytes).hexdigest()
    test_path = Path(__file__).resolve(strict=True)
    test_sha = hashlib.sha256(test_path.read_bytes()).hexdigest()
    spec = importlib.util.spec_from_file_location("original_client_preparation_guards", source_path)
    SOURCE = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = SOURCE
    exec(compile(source_bytes, str(source_path), "exec"), SOURCE.__dict__)
    result = GuardResult()
    unittest.defaultTestLoader.loadTestsFromTestCase(HostingGuardTests).run(result)
    unchanged = hashlib.sha256(source_path.read_bytes()).hexdigest() == source_sha and hashlib.sha256(test_path.read_bytes()).hexdigest() == test_sha
    report = {"schemaVersion": 1, "kind": "core-av-original-client-pure-guard-report",
        "scope": "Static unit and argv guards plus isolated filesystem guards; no startup or extraction acceptance",
        "source": {"path": str(source_path), "sha256": source_sha}, "testsSource": {"path": str(test_path), "sha256": test_sha},
        "reportPath": str(args.output), "fixtureParent": str(REPORT_PARENT), "testCount": result.testsRun,
        "passed": sum(row["outcome"] == "passed" for row in result.records), "failures": len(result.failures),
        "errors": len(result.errors), "sourceUnchanged": unchanged, "tests": result.records,
        "packageExtracted": False, "serviceStarted": False, "namespaceEntered": False, "httpRequests": 0,
        "vendorAssetsRead": False, "referenceDatabaseRead": False}
    raw = (json.dumps(report, sort_keys=True, indent=2, allow_nan=False) + "\n").encode()
    with os.fdopen(os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), "wb") as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    directory = os.open(REPORT_PARENT, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)
    print(json.dumps({"testCount": report["testCount"], "passed": report["passed"], "failures": report["failures"],
        "errors": report["errors"], "path": str(args.output), "sourceSha256": source_sha, "sourceUnchanged": unchanged}))
    return 0 if result.testsRun == 4 and result.wasSuccessful() and unchanged else 1


if __name__ == "__main__":
    raise SystemExit(main())
