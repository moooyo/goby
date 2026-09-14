#!/usr/bin/env python3
"""Pure admission guards; never provision, inspect a process, or contact a host."""
import copy
import importlib.util
import io
from pathlib import Path
import unittest
from unittest.mock import Mock, call, patch

SPEC = importlib.util.spec_from_file_location("audited_candidate", Path(__file__).with_name("prepare-audited-candidate.py"))
M = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(M)


class Guards(unittest.TestCase):
    def input(self):
        return {"kind": "audited-candidate-provision-input", "version": 1, "runId": "20260913T070000Z-0123456789ab",
                "backendReport": {}, "sourceManifest": {}, "frontendReport": {}, "ports": {"postgres": 25432, "http": 28298},
                "public_url": "http://127.0.0.1:28296", "transcodingProfile": "software-baseline-v1"}

    def test_rejects_existing_service_ports_even_when_other_port_is_fresh(self):
        for role in ("postgres", "http"):
            for port in M.RESERVED:
                value = self.input()
                value["ports"][role] = port
                with self.subTest(role=role, port=port), self.assertRaises(ValueError):
                    M.validate_input(value)

    def test_rejects_shared_boolean_or_privileged_ports(self):
        for ports in ({"postgres": 28298, "http": 28298}, {"postgres": True, "http": 28298}, {"postgres": 80, "http": 28298}):
            value = self.input()
            value["ports"] = ports
            with self.assertRaises(ValueError):
                M.validate_input(value)

    def test_rejects_undeclared_credentials_and_path_injection(self):
        for changed in ({"runId": "../../old-instance"}, {"runId": "20260913T070000Z-0123456789ab\nExecStart=unsafe"}, {"databaseURL": "postgres://secret"}):
            value = self.input()
            value.update(changed)
            with self.assertRaises(ValueError):
                M.validate_input(value)

    def test_rejects_old_or_credential_bearing_gateway_origins(self):
        for origin in ("http://127.0.0.1:18196", "http://127.0.0.1:25432", "http://127.0.0.1:28298", "http://user:secret@127.0.0.1:28296", "http://remote:28296", "http://127.0.0.1:28296/web"):
            value = self.input()
            value["public_url"] = origin
            with self.subTest(origin=origin), self.assertRaises(ValueError):
                M.validate_input(value)

    def test_manifest_cannot_escape_or_read_prohibited_history(self):
        for name in ("../runtime.env", "/etc/passwd", "assets\\escape.js", *M.FORBIDDEN):
            with self.subTest(name=name), self.assertRaises(ValueError):
                M.relative(name)

    def test_missing_hash_or_boolean_size_cannot_disable_file_verification(self):
        for row in ({"sha256": None, "bytes": 1}, {"sha256": "0" * 64, "bytes": True}, {"sha256": "0" * 64, "bytes": -1}):
            with self.assertRaises((ValueError, TypeError)):
                M.file_spec(row)

    def test_candidate_cannot_hide_software_engine_by_disabling_profile(self):
        value = self.input()
        value["transcodingProfile"] = "disabled"
        with self.assertRaises(ValueError):
            M.validate_input(value)

    def test_targeted_or_unclean_backend_cannot_reach_artifact_reads(self):
        full = {"status": "passed", "mode": "full", "archive_sha256": M.ARCHIVE, "unit_exit_code": 0,
                "recursive_cgroup_empty": True, "existing_services_modified": False,
                "worker": {"status": "passed", "mode": "full", "cleanup": {"only_worker_process_remains": True,
                    "owned_postgres_stopped": True, "private_bind_removed": True, "source_unchanged": True}}}
        cases = []
        for field, value in (("mode", "target"), ("status", "failed"), ("recursive_cgroup_empty", False), ("existing_services_modified", True)):
            case = copy.deepcopy(full)
            case[field] = value
            cases.append(case)
        case = copy.deepcopy(full)
        case["worker"]["cleanup"]["owned_postgres_stopped"] = False
        cases.append(case)
        for case in cases:
            with patch.object(M, "descriptor", return_value=case), patch.object(M, "read", side_effect=AssertionError("No artifact read is permitted.")), self.assertRaises(ValueError):
                M.verify_products(self.input())

    def test_run_collisions_refuse_before_any_resource_creation_or_command(self):
        for collision in ("root", "unit-file", "unit-loaded", "postgres-port", "http-port"):
            job = M.Provision(self.input(), {}, {})
            pg_socket, http_socket = Mock(), Mock()
            if collision.endswith("-port"):
                (pg_socket if collision == "postgres-port" else http_socket).bind.side_effect = OSError(98, "Address already in use")
            unit_path = Path("/run/systemd/system") / job.units["postgres"]
            def exists(path):
                return collision == "root" and Path(path) == job.root or collision == "unit-file" and Path(path) == unit_path
            def show(name):
                if name in job.units.values():
                    return {"LoadState": "loaded" if collision == "unit-loaded" else "not-found"}
                return {"ActiveState": "inactive", "MainPID": "0"}
            with self.subTest(collision=collision), patch.object(M, "verify_products", return_value=({}, b"binary", {})) as product, \
                 patch.object(M.os.path, "lexists", side_effect=exists), patch.object(job, "show", side_effect=show), \
                 patch.object(M.socket, "socket", side_effect=[pg_socket, http_socket]), \
                 patch.object(M, "owned", side_effect=AssertionError("No filesystem admission after collision.")), \
                 patch.object(job, "mkdir", side_effect=AssertionError("No directory creation.")) as mkdir, \
                 patch.object(job, "write", side_effect=AssertionError("No file publication.")) as write, \
                 patch.object(job, "command", side_effect=AssertionError("No initdb or command.")) as command, \
                 patch.object(job, "start", side_effect=AssertionError("No service start.")) as start, \
                 patch.object(job, "psql", side_effect=AssertionError("No SQL.")) as sql:
                with self.assertRaises((ValueError, OSError)):
                    job.run()
                product.assert_called_once()
                for action in (mkdir, write, command, start, sql):
                    action.assert_not_called()
                self.assertFalse(job.created)
                self.assertEqual(job.starts, [])
            if collision.endswith("-port"):
                pg_socket.close.assert_called_once()
                if collision == "http-port":
                    http_socket.close.assert_called_once()

    def test_listener_rejects_foreign_socket_even_with_matching_process(self):
        for inode, accepted in (("999", False), ("444", True)):
            job = M.Provision(self.input(), {}, {})
            process = {"pid": 55, "startTicks": "123", "invocationId": "a" * 32}
            observed = {"MainPID": "55"}
            tcp = ("header\n0: 0100007F:%04X 00000000:0000 0A 0:0 00:0 0 995 0 444 1\n" % self.input()["ports"]["http"]).encode()
            with self.subTest(inode=inode), patch.object(job, "show", return_value=observed), \
                 patch.object(job, "process", return_value=process), patch.object(Path, "read_bytes", return_value=tcp), \
                 patch.object(Path, "iterdir", return_value=iter([Path("/proc/55/fd/7")])), \
                 patch.object(M.os, "readlink", return_value="socket:[" + inode + "]"), patch.object(job, "save") as saved:
                if accepted:
                    self.assertEqual(job.listener(observed, process)["socketInode"], "444")
                else:
                    with self.assertRaises(ValueError):
                        job.listener(observed, process)
                saved.assert_called_once()

    def test_sql_timeout_terminates_its_new_session_and_keeps_unknown_outcome(self):
        job = M.Provision(self.input(), {}, {})
        process = Mock(pid=77, returncode=-15)
        process.communicate.side_effect = M.subprocess.TimeoutExpired(["psql"], 45)
        with patch.object(job, "save"), patch.object(job, "write"), patch.object(M.subprocess, "Popen", return_value=process) as spawn, \
             patch.object(M, "stop_command_group", return_value=((b"", b"timeout"), True)) as stopped:
            with self.assertRaises(ValueError):
                job.command("create-owned-role", ["/usr/sbin/runuser", "-u", "postgres", "--", str(M.PG / "psql")], b"private SQL")
            self.assertTrue(spawn.call_args.kwargs["start_new_session"])
            stopped.assert_called_once_with(process)
            state = job.command_responsibilities[0]
            self.assertEqual(state["sqlOutcome"], "unknown")
            self.assertTrue(state["timedOut"])
            self.assertTrue(state["processGroupClosed"])

    def test_command_group_escalates_after_bounded_term_wait(self):
        process = Mock(pid=77)
        process.communicate.side_effect = [M.subprocess.TimeoutExpired(["initdb"], 5), (b"", b"")]
        with patch.object(M.os, "killpg") as kill, patch.object(M, "group_alive", side_effect=[True, True, False, False]), \
             patch.object(M.time, "monotonic", side_effect=[0, 2, 3]), patch.object(M.time, "sleep"):
            self.assertEqual(M.stop_command_group(process), ((b"", b""), True))
        self.assertEqual(kill.call_args_list, [call(77, M.signal.SIGTERM), call(77, M.signal.SIGKILL)])
        self.assertEqual(process.communicate.call_args_list, [call(timeout=5), call(timeout=5)])
        process.wait.assert_called_once_with(timeout=1)

    def test_failure_receipt_write_error_retains_structured_resource_responsibility(self):
        job = M.Provision(self.input(), {}, {})
        job.created, job.stage = True, "recovery_database_setup"
        job.starts = [{"unit": job.units["postgres"], "acknowledged": True}]
        with patch.object(job, "save", side_effect=OSError(28, "No space left")):
            report = M.failure_report(job, ValueError("Uncertain SQL"))
        self.assertEqual(report["receiptErrorType"], "OSError")
        self.assertIsNone(report["receipt"])
        self.assertEqual(report["scope"], str(job.root))
        self.assertEqual(report["startCalls"], job.starts)
        self.assertEqual(report["stage"], "recovery_database_setup")
        self.assertFalse(report["resumeAllowed"])

    def test_units_never_restart_or_receive_a_shared_write_scope(self):
        root = Path("/opt/goby-audited-candidate-20260913T070000Z-0123456789ab")
        for user, directory in (("goby", "data"), ("postgres", "postgres")):
            text = M.unit_text(user, "/safe/executable", root / directory, root / directory, root / "private/unit.log").decode()
            self.assertIn("User=" + user + "\nGroup=" + user, text)
            self.assertIn("Type=exec\n", text)
            self.assertIn("Restart=no\n", text)
            self.assertIn("ProtectSystem=strict\n", text)
            self.assertIn("ReadWritePaths=" + str(root / directory) + "\n", text)
            self.assertIn("InaccessiblePaths=" + " ".join("-" + path for path in M.INACCESSIBLE), text)
            self.assertNotIn("ReadWritePaths=/var/lib/postgresql", text)
            self.assertNotIn("ReadWritePaths=/var/lib/goby-test", text)


class EmbeddedGuards(unittest.TestCase):
    """Exercise the closed E2 evidence chain without opening retained files."""

    artifact = {"path": "/opt/goby-test/m6-embedded-20260914/artifacts/linux-amd64/goby",
                "sha256": "59096592c1f145004e4f664a833227bb7ce019acee746cf345379349b2784312", "bytes": 30678868}
    prior = "/opt/goby-test/full-regression-20260914"
    continuation = "/opt/goby-test/full-regression-continuation-20260914"
    scope = continuation + "/ram/full-regression-continuation-20260914-20260914T070836Z-1465ffb74e7e"
    preserved = "/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b"

    def evidence(self):
        value = Guards.input(self)
        value.pop("backendReport")
        value.pop("frontendReport")
        value.update(version=2, dashboardProfile="embedded-administrator-v1",
                     sourceArchive={"path": self.prior + "/retained/source.tar.gz",
                                    "sha256": "418f9803237e02e0859f9bef8f6ed646730acb590a3482867a288cd64e8a0fad"},
                     sourceManifest={"path": self.continuation + "/retained/source-manifest.json",
                                     "sha256": "95fe6a40ecabdfe6b48260dcf200cf8a49d0cf9664c407ca91cd9eaba539d6f6"},
                     embeddedBuildManifest={"path": "/opt/goby-test/m6-embedded-20260914/artifacts/linux-amd64/manifest.json",
                                            "sha256": "a732425002323e0e29a4ca7cf9e0fd517d773c1de027d810488173b75957e683"},
                     productionSourceBinding={"path": self.prior + "/production-source-binding.json",
                                              "sha256": "823870bb9bcf9e2ca4b35e483d617de34e1c541b273cd6bf02e647a066e920dc"},
                     regressionReview=copy.deepcopy(M.REGRESSION_REVIEW),
                     regressionClosure=copy.deepcopy(M.REGRESSION_CLOSURE))
        prior_packages = ["github.com/moooyo/goby/" + name for name in
                          ("cmd/goby", "internal/activity", "internal/artwork", "internal/backupformat", "internal/backuppg",
                           "internal/backupstore", "internal/config", "internal/database", "internal/diagnostics", "internal/events", "internal/identity")]
        current_packages = ["github.com/moooyo/goby/" + name for name in
                            ("internal/library", "internal/lifecycle", "internal/media", "internal/metadata", "internal/playback",
                             "internal/recovery", "internal/recoverycontrol", "internal/server", "internal/settings", "internal/storagebinding",
                             "internal/subtitle", "internal/tasks", "internal/transcode", "internal/recoverydb")]
        library = current_packages[0]
        skipped = {"package": library, "test": "TestRootBindingFullScanMountNamespaceHelper"}
        reason = "the full-scan mount scenario requires its explicit reviewed opt-in scope"
        inert = ["TestRootTopologyMountNamespaceHelper", "TestRootBindingScanMountNamespaceHelper"]
        declared = [{**skipped, "reason": reason, "profileStatus": "not_executed"}]
        profiles = [{**skipped, "rawGoAction": "skip", "status": "paused", "reason": reason, "sevenFullScanStagesExecuted": False}]
        profiles.extend({"package": library, "test": name, "rawGoAction": "pass", "status": "not_exercised",
                         "reason": "The helper returns before its scenario when its explicit operator opt-in is absent."} for name in inert)
        reused = [{"package": name, "commandExitCode": 0, "topLevelPasses": 25 if index == 0 else 45}
                  for index, name in enumerate(prior_packages)]
        current = [{"package": name, "commandExitCode": 0, "topLevelPasses": 3 if name == library else 1,
                    "skippedTests": [skipped["test"]] if name == library else []} for name in current_packages]
        passed = [{"package": name, "test": "TestRepresentativeOrdinaryRegression"} for name in current_packages]
        passed.extend({"package": library, "test": name} for name in inert)
        current_counts = {"passed": 16, "failed": 0, "skipped": 1}
        combined_counts = {"passed": 491, "failed": 0, "skipped": 1}
        worker = {"status": "passed_with_explicit_profile_gap", "mode": "full", "scope": self.scope,
                  "ordinarySuiteCompletedAcrossPhases": True, "ordinary_suite_passed": True, "continuationPackagesPassed": True,
                  "build_executed": True, "singleRunFullSuite": False,
                  "cleanup": {"only_worker_process_remains": True, "owned_postgres_stopped": True,
                              "private_bind_removed": True, "source_unchanged": True},
                  "expectedAllPackages": prior_packages + current_packages, "expected_packages": current_packages,
                  "combinedPackageCount": 25, "combinedTestCounts": copy.deepcopy(combined_counts), "test_counts": copy.deepcopy(current_counts),
                  "tests": {"passed": passed, "failed": [], "skipped": [copy.deepcopy(skipped)]},
                  "packages": [{"package": row["package"], "result": "pass", "exit_code": 0, "top_level_passes": row["topLevelPasses"],
                                "failed": 0, "skipped": 1 if row["package"] == library else 0} for row in current],
                  "declared_profile_skips": copy.deepcopy(declared), "profile_not_executed": copy.deepcopy(profiles),
                  "binary": {"path": "bin/goby-linux-amd64", "sha256": "9" * 64, "bytes": 123456,
                             "GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "0", "embeddedAdministrator": False}}
        pins = {"sourceManifest": {**value["sourceManifest"], "bytes": 974367},
                "parent": {"path": self.continuation + "/retained/report.json", "sha256": "3" * 64, "bytes": 48000},
                "worker": {"path": self.continuation + "/retained/worker-report.json", "sha256": "4" * 64, "bytes": 44000}}
        originals = {key: {**pin, "path": self.scope + "/" + Path(pin["path"]).name} for key, pin in pins.items()}
        execution_pin = {"path": self.continuation + "/execution.json", "sha256": "5" * 64}
        parent = {"status": "passed_with_explicit_profile_gap", "mode": "full", "scope": self.scope, "unit_exit_code": 0,
                  "private_unit_empty": True, "recursive_cgroup_empty": True, "singleRunFullSuite": False,
                  "worker": worker, "worker_report_sha256": pins["worker"]["sha256"], "archive_sha256": value["sourceArchive"]["sha256"]}
        execution = {"status": "passed_with_explicit_profile_gap",
                     "runner": {"reportSha256": pins["parent"]["sha256"], "scope": self.scope},
                     "protectedBefore": {"goby-foundation-test.service": {"ActiveState": "inactive", "MainPID": "0"}},
                     "protectedAfter": {"goby-foundation-test.service": {"ActiveState": "inactive", "MainPID": "0"}}}
        observed = {"parentStatus": "passed_with_explicit_profile_gap", "workerStatus": "passed_with_explicit_profile_gap",
                    "executionStatus": "passed_with_explicit_profile_gap", "currentTestCounts": copy.deepcopy(current_counts)}
        review = {"kind": "full-regression-continuation-independent-review", "version": 1, "status": "passed",
                  "ordinaryAcceptanceStatus": "passed_with_explicit_profile_gap", "ordinarySuiteCompletedAcrossPhases": True,
                  "singleRunFullSuite": False, "gobyEmbedAdminSuiteExecuted": False, "failures": [], "scope": self.scope,
                  "cleanup": {"workerFactsExact": True, "ownedProcessesAbsent": True, "cgroupEmpty": True, "fixtureEmpty": True,
                              "scopeTmpEmpty": True, "postgresNormalShutdownRecorded": True,
                              "terminalUnit": {"MainPID": "0", "LoadState": "loaded", "ActiveState": "inactive", "SubState": "dead"}},
                  "source": {"archiveSha256": value["sourceArchive"]["sha256"], "sourceManifestSha256": value["sourceManifest"]["sha256"],
                             "files": 5244, "gitCommit": "badf39635014ab221da7a75762f3fb40aee28033"},
                  "evidence": {"parent": {key: originals["parent"][key] for key in ("path", "sha256")},
                               "worker": {key: originals["worker"][key] for key in ("path", "sha256")}, "execution": execution_pin,
                               "priorWorker": {"path": self.prior + "/private/retained-worker-report.json",
                                               "sha256": "2a2b8e82dfa16b81afec4e73ba3460fcd44155ce2f10ea378a733fab3fc4e99d"},
                               "priorReview": {"path": self.prior + "/partial-review.json",
                                               "sha256": "70e2d76ee0c59764cf2b7b461ef5b35518f6899ea2402d523a89feb727759b00"}},
                  "packageCoverage": {"reusedCompletePackages": reused, "currentCompletePackages": current,
                                      "expectedAllPackages": prior_packages + current_packages, "reusedTopLevelPasses": 475,
                                      "priorPartialLibraryPassesReused": 0, "currentTopLevelPasses": 16, "packageCount": 25,
                                      "combinedTestCounts": copy.deepcopy(combined_counts)},
                  "profiles": copy.deepcopy(profiles), "observedResult": copy.deepcopy(observed),
                  "build": {"executed": True, "verified": True, "ordinary": True, "gobyEmbedAdminTagged": False,
                            "artifact": {"path": self.scope + "/bin/goby-linux-amd64", "sha256": "9" * 64, "bytes": 123456}}}
        closure = {"kind": "full-regression-continuation-seal", "version": 1,
                   "status": "continuation_evidence_preserved_and_owned_volumes_closed", "scope": self.scope,
                   "archivesComplete": True, "ownedProcessesClosed": True, "ext4Unmounted": True, "loopDetached": True,
                   "ramUnmounted": True, "lockReleased": True, "sourceArchiveStillRetainedInE1": True,
                   "profileGapClosed": False, "independentReviewStatus": "passed", "review": copy.deepcopy(value["regressionReview"]),
                   "retainedRecords": {key: {"original": copy.deepcopy(originals[key]), "retained": copy.deepcopy(pin), "sameBytes": True}
                                       for key, pin in pins.items()},
                   "sourceManifest": copy.deepcopy(originals["sourceManifest"]), "parent": copy.deepcopy(originals["parent"]),
                   "worker": copy.deepcopy(originals["worker"]), "execution": copy.deepcopy(execution_pin),
                   "rawDeclaredProfileSkips": copy.deepcopy(declared), "rawProfileNotExecuted": copy.deepcopy(profiles),
                   "reportedResults": {**copy.deepcopy(observed), "combinedTestCounts": copy.deepcopy(combined_counts),
                                       "combinedPackageCount": 25, "ordinarySuiteCompletedAcrossPhases": True, "singleRunFullSuite": False},
                   "sourceAlreadyRetained": {**value["sourceArchive"], "bytes": 32143107, "permanentCopyCreatedByThisSeal": False}}
        assets = [{"name": "index.html" if index == 0 else "assets/chunk-%02d.js" % index, "sha256": "a" * 64,
                   "bytes": 106130 if index == 0 else (18000 if index <= 55 else 10000)} for index in range(57)]
        references = [row["name"] for row in assets[1:6]]
        manifest = {"kind": "goby-linux-embedded-administrator-build", "version": 1,
                    "target": {"os": "linux", "arch": "amd64", "cgoEnabled": False},
                    "binary": {"name": "goby", "sha256": self.artifact["sha256"], "bytes": self.artifact["bytes"]},
                    "administratorAssets": copy.deepcopy(assets), "administratorEntryReferences": references, "administratorAssetBytes": 1106130}
        binding = {"kind": "m6-to-ordinary-full-production-source-binding", "version": 1, "status": "passed",
                   "productionSourceBindingPassed": True, "productionArchiveSetsExact": True, "productionBytesExact": True,
                   "m6WholeArchiveManifestMatched": True, "readerWorkComplete": True, "productionDifferences": [],
                   "comparedFiles": 374, "productionGoFiles": 338, "productionEmbedResourceFiles": 34, "moduleFiles": 2,
                   "ordinarySourceManifestFiles": 5244, "ordinarySuiteIsTaggedFullSuite": False, "fullSuiteCompletionEstablished": False,
                   "inputs": {"ordinaryFullSourceArchive": copy.deepcopy(value["sourceArchive"])},
                   "ordinarySourceManifest": copy.deepcopy(value["sourceManifest"]),
                   "binary": {"descriptor": copy.deepcopy(self.artifact), "manifest": copy.deepcopy(value["embeddedBuildManifest"]),
                              "tag": "goby_embed_admin", "elfMachine": 62},
                   "administratorAssets": {"archiveManifestHashMatches": True, "ordinarySourceArchiveContainsDist": False,
                                           "fileCount": 57, "totalBytes": 1106130, "entryReferencesMatched": 5,
                                           "files": assets, "entryReferences": copy.deepcopy(references)}}
        sources = {"internal/fixture/source_%04d.go" % index: {"sha256": "b" * 64, "bytes": 11} for index in range(5244)}
        return {"input": value, "review": review, "closure": closure, "parent": parent, "worker": worker, "execution": execution,
                "binding": binding, "manifest": manifest, "sources": sources, "pins": pins, "reads": [], "binary": self.elf()}

    def elf(self, machine=62):
        return b"\x7fELF\x02\x01" + bytes(12) + machine.to_bytes(2, "little") + bytes(44)

    def verify(self, evidence, binary_reader=None):
        value = evidence["input"]
        descriptors = {value[field]["path"]: (copy.deepcopy(value[field]), evidence[name]) for field, name in
                       (("regressionReview", "review"), ("regressionClosure", "closure"),
                        ("productionSourceBinding", "binding"), ("embeddedBuildManifest", "manifest"))}
        execution = evidence["review"]["evidence"]["execution"]
        descriptors[execution["path"]] = (copy.deepcopy(execution), evidence["execution"])
        retained = {pin["path"]: (pin, evidence["sources" if name == "sourceManifest" else name]) for name, pin in evidence["pins"].items()}

        def load_descriptor(pin):
            self.assertIn(pin["path"], descriptors, "Only standalone sealed descriptors may be opened.")
            expected, document = descriptors[pin["path"]]
            self.assertEqual(pin, expected)
            return document

        def load_bytes(path, checksum=None, size=None):
            request = (str(path), checksum, size)
            evidence["reads"].append(request)
            if str(path) == self.artifact["path"]:
                self.assertEqual(request, (self.artifact["path"], self.artifact["sha256"], self.artifact["bytes"]))
                return binary_reader(path, checksum, size) if binary_reader else evidence["binary"]
            if str(path) == value["sourceArchive"]["path"]:
                self.assertEqual(request, (value["sourceArchive"]["path"], value["sourceArchive"]["sha256"], 32143107))
                return b"Retained source archive fixture."
            self.assertIn(str(path), retained, "Source trees, prior partial reports, and ordinary binaries must not be read.")
            pin, document = retained[str(path)]
            self.assertEqual(request, (pin["path"], pin["sha256"], pin["bytes"]))
            return M.encoded(document)

        with patch.object(M, "descriptor", side_effect=load_descriptor), patch.object(M, "read", side_effect=load_bytes), \
             patch.object(Path, "rglob", side_effect=AssertionError("No source tree expansion is permitted.")):
            return M.verify_products(value)

    def reject_before_binary(self, evidence):
        with self.assertRaises(ValueError):
            self.verify(evidence)
        self.assertNotIn(self.artifact["path"], [row[0] for row in evidence["reads"]])

    def test_closed_ordinary_evidence_selects_only_the_fixed_embedded_artifact(self):
        evidence = self.evidence()
        self.assertEqual(evidence["input"]["regressionReview"], {"path": self.continuation + "/independent-review.json",
                         "sha256": "5b946d4b4bfee2b177b53861c89fad690c08c406b832a26f778d563ed4174b72"})
        self.assertEqual(evidence["input"]["regressionClosure"], {"path": self.continuation + "/closure.json",
                         "sha256": "6cd0b024834315db240d6268c94b578e4beb99977fd1740d51c5494cf4508b1c"})
        self.assertIs(M.validate_input(evidence["input"]), evidence["input"])
        review, binary, assets = self.verify(evidence)
        self.assertIs(review, evidence["review"])
        self.assertEqual(binary, evidence["binary"])
        self.assertEqual(assets, {})
        expected = [(pin["path"], pin["sha256"], pin["bytes"]) for pin in evidence["pins"].values()]
        expected.extend([(evidence["input"]["sourceArchive"]["path"], evidence["input"]["sourceArchive"]["sha256"], 32143107),
                         (self.artifact["path"], self.artifact["sha256"], self.artifact["bytes"])])
        self.assertEqual(evidence["reads"], expected)

    def test_version_contracts_do_not_inherit_each_others_acceptance(self):
        legacy = Guards.input(self)
        self.assertIs(M.validate_input(legacy), legacy)
        partial = {"status": "passed_with_explicit_profile_gap", "mode": "full",
                   "worker": {"status": "passed_with_explicit_profile_gap", "mode": "full"}}
        with patch.object(M, "descriptor", return_value=partial), patch.object(M, "read") as read, \
             patch.object(M, "verify_embedded_products") as embedded, self.assertRaises(ValueError):
            M.verify_products(legacy)
        read.assert_not_called()
        embedded.assert_not_called()
        for value in (dict(legacy, version=2), dict(self.evidence()["input"], version=1),
                      dict(self.evidence()["input"], backendReport={}), dict(self.evidence()["input"], version=True)):
            with self.subTest(version=value["version"]), self.assertRaises(ValueError):
                M.validate_input(value)

    def test_v2_rejects_unselected_inputs_and_external_dashboard_profiles(self):
        for field in ("sourceArchive", "sourceManifest", "embeddedBuildManifest", "productionSourceBinding", "regressionReview", "regressionClosure"):
            for changed in ("path", "sha256"):
                evidence = self.evidence()
                evidence["input"][field][changed] = "/opt/goby-test/other-evidence.json" if changed == "path" else "0" * 64
                with self.subTest(field=field, changed=changed):
                    self.reject_before_binary(evidence)
        for profile in ("external-administrator-v1", "", None):
            evidence = self.evidence()
            evidence["input"]["dashboardProfile"] = profile
            self.reject_before_binary(evidence)

    def test_partial_failed_and_running_phases_reject_before_artifact_read(self):
        for name in ("review", "closure", "parent", "worker", "execution", "binding"):
            for status in ("partial", "failed", "running"):
                evidence = self.evidence()
                evidence[name]["status"] = status
                with self.subTest(document=name, status=status):
                    self.reject_before_binary(evidence)
        for terminal in ({"MainPID": "41", "LoadState": "loaded", "ActiveState": "active", "SubState": "running"},
                         {"MainPID": "0", "LoadState": "not-found", "ActiveState": "active", "SubState": "running"}):
            evidence = self.evidence()
            evidence["review"]["cleanup"]["terminalUnit"] = terminal
            with self.subTest(terminal=terminal):
                self.reject_before_binary(evidence)

    def test_two_phase_package_membership_cannot_be_missing_duplicated_replaced_or_overlapping(self):
        for name, opposite in (("reusedCompletePackages", "currentCompletePackages"), ("currentCompletePackages", "reusedCompletePackages")):
            for mutation in ("missing", "duplicate", "replacement", "overlap"):
                evidence = self.evidence()
                coverage = evidence["review"]["packageCoverage"]
                rows = coverage[name]
                if mutation == "missing":
                    rows.pop()
                elif mutation == "duplicate":
                    rows[-1] = copy.deepcopy(rows[0])
                else:
                    rows[-1]["package"] = coverage[opposite][0]["package"] if mutation == "overlap" else "github.com/moooyo/goby/internal/foreign"
                if name == "currentCompletePackages":
                    evidence["worker"]["expected_packages"] = [row["package"] for row in rows]
                with self.subTest(phase=name, mutation=mutation):
                    self.reject_before_binary(evidence)
        evidence = self.evidence()
        evidence["review"]["packageCoverage"]["expectedAllPackages"][-1] = evidence["review"]["packageCoverage"]["expectedAllPackages"][0]
        evidence["worker"]["expectedAllPackages"] = evidence["review"]["packageCoverage"]["expectedAllPackages"][:]
        self.reject_before_binary(evidence)

    def test_prior_partial_library_passes_cannot_join_the_complete_phase_count(self):
        evidence = self.evidence()
        coverage = evidence["review"]["packageCoverage"]
        coverage["priorPartialLibraryPassesReused"] = 7
        for counts in (coverage["combinedTestCounts"], evidence["worker"]["combinedTestCounts"], evidence["closure"]["reportedResults"]["combinedTestCounts"]):
            counts["passed"] += 7
        self.reject_before_binary(evidence)

    def test_tagged_dashboard_package_cannot_replace_ordinary_storage_binding_coverage(self):
        evidence = self.evidence()
        ordinary = "github.com/moooyo/goby/internal/storagebinding"
        tagged = "github.com/moooyo/goby/web/admin"
        coverage, worker = evidence["review"]["packageCoverage"], evidence["worker"]
        for rows in (coverage["currentCompletePackages"], worker["packages"], worker["tests"]["passed"]):
            for row in rows:
                if row["package"] == ordinary:
                    row["package"] = tagged
        for owner, key in ((coverage, "expectedAllPackages"), (worker, "expectedAllPackages"), (worker, "expected_packages")):
            owner[key] = [tagged if name == ordinary else name for name in owner[key]]
        self.reject_before_binary(evidence)

    def test_boolean_counts_cannot_impersonate_completed_integer_results(self):
        for target, field in (("current-row", "topLevelPasses"), ("reused-row", "commandExitCode"),
                              ("coverage", "priorPartialLibraryPassesReused"), ("coverage", "currentTopLevelPasses"),
                              ("coverage", "packageCount"), ("combined-counts", "failed"), ("current-counts", "skipped"),
                              ("worker-package", "exit_code"), ("source", "files")):
            evidence = self.evidence()
            coverage = evidence["review"]["packageCoverage"]
            objects = {"current-row": coverage["currentCompletePackages"][0], "reused-row": coverage["reusedCompletePackages"][0],
                       "coverage": coverage, "combined-counts": coverage["combinedTestCounts"], "current-counts": evidence["worker"]["test_counts"],
                       "worker-package": evidence["worker"]["packages"][0], "source": evidence["review"]["source"]}
            objects[target][field] = False if field in ("failed", "exit_code", "commandExitCode", "priorPartialLibraryPassesReused") else True
            with self.subTest(target=target, field=field):
                self.reject_before_binary(evidence)

    def test_unique_skip_and_inert_helpers_keep_their_original_meaning(self):
        for mutation in ("missing-skip", "moved-skip", "missing-declaration", "changed-reason", "promoted-profile", "promoted-inert", "duplicate-pass", "missing-inert"):
            evidence = self.evidence()
            worker, review, closure = evidence["worker"], evidence["review"], evidence["closure"]
            if mutation == "missing-skip":
                worker["tests"]["skipped"] = []
            elif mutation == "moved-skip":
                worker["tests"]["skipped"][0]["package"] = worker["expected_packages"][1]
            elif mutation == "missing-declaration":
                worker["declared_profile_skips"] = []
            elif mutation == "changed-reason":
                for rows in (worker["declared_profile_skips"], closure["rawDeclaredProfileSkips"], worker["profile_not_executed"], review["profiles"], closure["rawProfileNotExecuted"]):
                    rows[0]["reason"] = "The scenario was intentionally waived."
            elif mutation in ("promoted-profile", "promoted-inert"):
                index = 0 if mutation == "promoted-profile" else 1
                for rows in (worker["profile_not_executed"], review["profiles"], closure["rawProfileNotExecuted"]):
                    rows[index]["status"] = "passed"
                    if index == 0:
                        rows[index]["sevenFullScanStagesExecuted"] = True
            elif mutation == "duplicate-pass":
                worker["tests"]["passed"][-1] = copy.deepcopy(worker["tests"]["passed"][-2])
            else:
                worker["tests"]["passed"][-1]["test"] = "TestUnrelatedLibraryBehavior"
            with self.subTest(mutation=mutation):
                self.reject_before_binary(evidence)

    def test_review_and_closure_must_bind_the_same_original_and_retained_records(self):
        for key in ("sourceManifest", "parent", "worker"):
            for mutation in ("original-path", "retained-path", "retained-hash", "retained-bytes", "same-bytes"):
                evidence = self.evidence()
                row = evidence["closure"]["retainedRecords"][key]
                if mutation == "original-path":
                    row["original"]["path"] = row["retained"]["path"]
                elif mutation == "retained-path":
                    row["retained"]["path"] = row["original"]["path"]
                elif mutation == "retained-hash":
                    row["retained"]["sha256"] = "f" * 64
                elif mutation == "retained-bytes":
                    row["retained"]["bytes"] += 1
                else:
                    row["sameBytes"] = False
                with self.subTest(record=key, mutation=mutation):
                    self.reject_before_binary(evidence)
        for key in ("review", "sourceManifest", "parent", "worker", "execution"):
            evidence = self.evidence()
            evidence["closure"][key]["sha256"] = "e" * 64
            with self.subTest(closure=key):
                self.reject_before_binary(evidence)

    def test_each_required_cleanup_and_seal_fact_must_be_true(self):
        cases = {"review": ("workerFactsExact", "ownedProcessesAbsent", "cgroupEmpty", "fixtureEmpty", "scopeTmpEmpty", "postgresNormalShutdownRecorded"),
                 "closure": ("archivesComplete", "ownedProcessesClosed", "ext4Unmounted", "loopDetached", "ramUnmounted", "lockReleased", "sourceArchiveStillRetainedInE1"),
                 "worker": ("only_worker_process_remains", "owned_postgres_stopped", "private_bind_removed", "source_unchanged")}
        for name, fields in cases.items():
            for field in fields:
                evidence = self.evidence()
                target = evidence[name] if name == "closure" else evidence[name]["cleanup"]
                target[field] = False
                with self.subTest(document=name, field=field):
                    self.reject_before_binary(evidence)

    def test_terminal_evidence_cannot_promote_unexecuted_work_or_boolean_exit_codes(self):
        cases = (("review", "ordinarySuiteCompletedAcrossPhases", False), ("review", "singleRunFullSuite", True),
                 ("review", "gobyEmbedAdminSuiteExecuted", True), ("closure", "profileGapClosed", True),
                 ("parent", "private_unit_empty", False), ("parent", "recursive_cgroup_empty", False),
                 ("parent", "unit_exit_code", False), ("parent", "singleRunFullSuite", True),
                 ("worker", "ordinarySuiteCompletedAcrossPhases", False), ("worker", "ordinary_suite_passed", False),
                 ("worker", "continuationPackagesPassed", False), ("worker", "build_executed", False),
                 ("worker", "singleRunFullSuite", True))
        for name, field, value in cases:
            evidence = self.evidence()
            evidence[name][field] = value
            with self.subTest(document=name, field=field):
                self.reject_before_binary(evidence)
        for name in ("review", "closure", "binding", "manifest"):
            evidence = self.evidence()
            evidence[name]["version"] = True
            with self.subTest(document=name):
                self.reject_before_binary(evidence)

    def test_ordinary_binary_and_changed_m6_manifest_cannot_select_an_artifact(self):
        evidence = self.evidence()
        evidence["worker"]["binary"]["sha256"] = self.artifact["sha256"]
        evidence["review"]["build"]["artifact"]["sha256"] = self.artifact["sha256"]
        self.reject_before_binary(evidence)
        for target, field, changed in (("manifest-binary", "sha256", "9" * 64), ("manifest-binary", "bytes", self.artifact["bytes"] + 1),
                                       ("target", "arch", "arm64"), ("target", "os", "windows"), ("target", "cgoEnabled", 0),
                                       ("binding-binary", "path", self.scope + "/bin/goby-linux-amd64"),
                                       ("binding-binary", "sha256", "9" * 64), ("binding-binary", "bytes", self.artifact["bytes"] - 1)):
            evidence = self.evidence()
            objects = {"manifest-binary": evidence["manifest"]["binary"], "target": evidence["manifest"]["target"],
                       "binding-binary": evidence["binding"]["binary"]["descriptor"]}
            objects[target][field] = changed
            with self.subTest(target=target, field=field):
                self.reject_before_binary(evidence)

    def test_production_bridge_cannot_substitute_sources_or_claim_a_tagged_suite(self):
        for field in ("productionSourceBindingPassed", "productionArchiveSetsExact", "productionBytesExact", "m6WholeArchiveManifestMatched", "readerWorkComplete"):
            evidence = self.evidence()
            evidence["binding"][field] = False
            with self.subTest(field=field):
                self.reject_before_binary(evidence)
        for mutation in ("archive", "source-manifest", "binary-manifest", "tag", "machine", "difference", "tagged-suite", "bridge-completes-suite"):
            evidence = self.evidence()
            binding = evidence["binding"]
            if mutation == "archive":
                binding["inputs"]["ordinaryFullSourceArchive"]["sha256"] = "0" * 64
            elif mutation == "source-manifest":
                binding["ordinarySourceManifest"]["sha256"] = "0" * 64
            elif mutation == "binary-manifest":
                binding["binary"]["manifest"]["sha256"] = "0" * 64
            elif mutation == "tag":
                binding["binary"]["tag"] = "ordinary"
            elif mutation == "machine":
                binding["binary"]["elfMachine"] = 183
            elif mutation == "difference":
                binding["productionDifferences"] = ["internal/server/items.go"]
            else:
                binding["ordinarySuiteIsTaggedFullSuite" if mutation == "tagged-suite" else "fullSuiteCompletionEstablished"] = True
            with self.subTest(mutation=mutation):
                self.reject_before_binary(evidence)

    def test_binary_bytes_must_still_describe_an_amd64_elf(self):
        for binary in (b"ordinary executable", self.elf(183), self.elf()[:18], b"\x7fELF\x01\x01" + self.elf()[6:]):
            evidence = self.evidence()
            evidence["binary"] = binary
            with self.subTest(header=binary[:20]), self.assertRaises(ValueError):
                self.verify(evidence)
            self.assertEqual(evidence["reads"][-1], (self.artifact["path"], self.artifact["sha256"], self.artifact["bytes"]))

    def test_real_artifact_reader_checks_the_selected_hash_without_accessing_a_file(self):
        class InMemoryAuthority(io.BytesIO):
            def fileno(self):
                return 77

        evidence = self.evidence()
        binary = evidence["binary"]
        info = Mock(st_dev=1, st_ino=2, st_mode=0o100600, st_uid=0, st_gid=0, st_nlink=1,
                    st_size=len(binary), st_mtime_ns=3, st_ctime_ns=4)
        real_read = M.read
        with patch.object(M, "owned", return_value=info), patch.object(M.os, "open", return_value=77) as opened, \
             patch.object(M.os, "fstat", return_value=info), patch.object(Path, "lstat", return_value=info), \
             patch.object(M.os, "fdopen", return_value=InMemoryAuthority(binary)), self.assertRaisesRegex(ValueError, "Authority digest differs"):
            self.verify(evidence, binary_reader=real_read)
        opened.assert_called_once_with(Path(self.artifact["path"]), M.os.O_RDONLY | M.os.O_NOFOLLOW)
        self.assertEqual(evidence["reads"][-1], (self.artifact["path"], self.artifact["sha256"], self.artifact["bytes"]))

    def test_v2_units_hide_the_preserved_candidate_and_remove_external_web_override(self):
        value = self.evidence()["input"]
        root = Path("/opt/goby-audited-candidate-" + value["runId"])
        legacy_hidden = ("/opt/goby-test", "/opt/goby-dev", "/opt/goby-client-m3e", "/opt/goby-fixtures", "/var/lib/goby-test", "/var/lib/postgresql")
        self.assertEqual(M.INACCESSIBLE, legacy_hidden)
        expected = (*legacy_hidden, self.preserved)
        self.assertEqual(M.inaccessible_paths(value), expected)
        self.assertEqual(M.dashboard_environment(value, root / "install"), {})
        for role, user in (("server", "goby"), ("postgres", "postgres")):
            writable = root / ("data" if role == "server" else "postgres")
            text = M.unit_text(user, "/safe/executable", writable, writable, root / "private/unit.log",
                               root / "private/runtime.env" if role == "server" else None,
                               inaccessible=M.inaccessible_paths(value), embedded=role == "server").decode()
            with self.subTest(role=role):
                self.assertIn("InaccessiblePaths=" + " ".join("-" + path for path in expected) + "\n", text)
                self.assertIn("ReadWritePaths=" + str(writable) + "\n", text)
                self.assertEqual("UnsetEnvironment=GOBY_WEB_DIR\n" in text, role == "server")
                self.assertFalse(any(line.startswith("Environment=GOBY_WEB_DIR") for line in text.splitlines()))
        legacy = Guards.input(self)
        self.assertEqual(M.inaccessible_paths(legacy), M.INACCESSIBLE)
        self.assertEqual(M.dashboard_environment(legacy, root / "install"), {"GOBY_WEB_DIR": str(root / "install/admin")})
        legacy_unit = M.unit_text("goby", "/safe/executable", root / "data", root / "data", root / "private/unit.log").decode()
        self.assertNotIn("UnsetEnvironment=", legacy_unit)
        self.assertNotIn(self.preserved, legacy_unit)

    def test_loaded_unit_hiding_requires_exact_membership_for_each_version(self):
        for value in (Guards.input(self), self.evidence()["input"]):
            expected = list(M.inaccessible_paths(value))
            for paths in (expected, ["-" + path for path in reversed(expected)]):
                M.verify_inaccessible_paths(paths, value)
            for paths in (expected[:-1], expected + [expected[0]], expected[:-1] + [expected[0]],
                          expected[:-1] + ["/opt/foreign-candidate"], expected + ["/opt/foreign-candidate"]):
                with self.subTest(version=value["version"], paths=paths), self.assertRaises(ValueError):
                    M.verify_inaccessible_paths(paths, value)
        with self.assertRaises(ValueError):
            M.verify_inaccessible_paths(list(M.INACCESSIBLE), self.evidence()["input"])

    def test_embedded_dashboard_installation_never_creates_an_external_directory(self):
        for assets, exists in (({}, False), ({"index.html": b"external dashboard"}, False), ({}, True)):
            job = M.Provision(self.evidence()["input"], {}, {})
            with self.subTest(assets=bool(assets), exists=exists), patch.object(M.os.path, "lexists", return_value=exists) as present, \
                 patch.object(job, "mkdir") as mkdir, patch.object(job, "write") as write:
                if assets or exists:
                    with self.assertRaises(ValueError):
                        job.install_dashboard(assets)
                else:
                    job.install_dashboard(assets)
                    present.assert_called_once_with(job.install / "admin")
                mkdir.assert_not_called()
                write.assert_not_called()

    def test_legacy_dashboard_still_installs_the_verified_external_assets(self):
        job = M.Provision(Guards.input(self), {}, {})
        assets = {"index.html": b"legacy dashboard", "assets/app.js": b"legacy script"}
        with patch.object(job, "mkdir") as mkdir, patch.object(job, "write") as write, patch.object(Path, "exists", return_value=True):
            job.install_dashboard(assets)
        mkdir.assert_called_once_with(job.install / "admin", mode=0o755)
        self.assertEqual(write.call_args_list, [call(job.install / "admin/index.html", assets["index.html"], mode=0o644),
                                               call(job.install / "admin/assets/app.js", assets["assets/app.js"], mode=0o644)])

    def test_embedded_runtime_rejects_overrides_or_late_external_directories(self):
        observed = {"MainPID": "55", "InvocationID": "a" * 32}
        for environment, exists, changed in ((b"PATH=/safe\0OTHER_GOBY_WEB_DIR=/unused\0", False, False),
                                             (b"GOBY_WEB_DIR=/external\0", False, False), (b"GOBY_WEB_DIR=\0", False, False),
                                             (b"X" * ((1 << 20) + 1), False, False), (b"PATH=/safe\0", True, False),
                                             (b"PATH=/safe\0", False, True)):
            job = M.Provision(self.evidence()["input"], {}, {})
            with self.subTest(exists=exists, changed=changed, environment_prefix=environment[:32]), \
                 patch.object(M.os.path, "lexists", return_value=exists), patch.object(Path, "open", autospec=True, return_value=io.BytesIO(environment)) as opened, \
                 patch.object(job, "show", return_value={**observed, "MainPID": "56"} if changed else observed):
                if exists or changed or environment.startswith((b"GOBY_WEB_DIR=", b"X")):
                    with self.assertRaises(ValueError):
                        job.verify_embedded_runtime(observed)
                else:
                    job.verify_embedded_runtime(observed)
                if exists:
                    opened.assert_not_called()
                else:
                    opened.assert_called_once_with(Path("/proc/55/environ"), "rb")

    def provision_to_loaded_guard(self, *, changed_role=None, hiding=None, unset=None):
        """Stop at the first start call with every host-facing operation mocked."""
        job = M.Provision(self.evidence()["input"], {}, {})
        written = {}

        def write(path, raw, *args, **kwargs):
            written[str(path)] = raw
            return {"path": str(path), "sha256": "c" * 64}

        def show(unit):
            return {"LoadState": "not-found"} if unit in job.units.values() else {"ActiveState": "inactive", "MainPID": "0"}

        def loaded(argv, **kwargs):
            self.assertEqual(argv[:2], ["/usr/bin/systemctl", "show"])
            role = next(role for role, unit in job.units.items() if unit == argv[2])
            if argv[3] == "--property=EnvironmentFiles":
                return ("EnvironmentFiles=" + (str(job.private / "runtime.env") + " (ignore_errors=no)" if role == "server" else "") + "\n").encode()
            user = "goby" if role == "server" else "postgres"
            writable = job.data if role == "server" else job.pgroot
            paths = list(M.inaccessible_paths(job.value))
            if role == changed_role:
                if hiding == "missing":
                    paths.remove(self.preserved)
                elif hiding == "duplicate":
                    paths[-1] = paths[0]
            result = {"Type": "exec", "User": user, "Group": user, "Restart": "no", "ProtectSystem": "strict", "ProtectHome": "yes",
                      "PrivateTmp": "yes", "NoNewPrivileges": "yes", "ReadWritePaths": str(writable),
                      "InaccessiblePaths": " ".join("-" + path for path in paths),
                      "ExecStart": "{ argv[]=" + " ".join(job.argv[role]) + " ; }",
                      "MemoryMax": str((256 if role == "postgres" else 768) << 20), "MemorySwapMax": "0", "TasksMax": "128"}
            if role == "server":
                self.assertIn(",UnsetEnvironment", argv[3])
                if unset != "missing":
                    result["UnsetEnvironment"] = "GOBY_WEB_DIR" if unset is None else unset
            return "".join(key + "=" + value + "\n" for key, value in result.items()).encode()

        with patch.object(M, "verify_products", return_value=({}, self.elf(), {})), patch.object(M.os.path, "lexists", return_value=False), \
             patch.object(job, "show", side_effect=show), patch.object(M.pwd, "getpwnam", side_effect=lambda user: Mock(pw_uid=995 if user == "goby" else 111)), \
             patch.object(M.socket, "socket", side_effect=[Mock(), Mock()]), patch.object(M, "owned"), patch.object(job, "mkdir") as mkdir, \
             patch.object(job, "save"), patch.object(job, "write", side_effect=write), patch.object(job, "command"), \
             patch.object(M.secrets, "token_hex", side_effect=["1" * 64, "2" * 64, "3" * 64]), \
             patch.object(M.subprocess, "check_output", side_effect=loaded), \
             patch.object(job, "start", side_effect=RuntimeError("The loaded-unit guard has completed.")) as start, \
             patch.object(job, "psql", side_effect=AssertionError("No SQL is permitted.")):
            if hiding is not None or unset is not None:
                with self.assertRaises(ValueError):
                    job.run()
                start.assert_not_called()
            else:
                with self.assertRaisesRegex(RuntimeError, "The loaded-unit guard has completed"):
                    job.run()
                start.assert_called_once_with("postgres")
        self.assertNotIn(call(job.install / "admin", mode=0o755), mkdir.call_args_list)
        return job, written

    def test_v2_provision_wires_both_loaded_units_and_embedded_runtime_environment(self):
        job, written = self.provision_to_loaded_guard()
        runtime = written[str(job.private / "runtime.env")].decode()
        self.assertFalse(any(line.startswith("GOBY_WEB_DIR=") for line in runtime.splitlines()))
        for role in ("postgres", "server"):
            unit = written[str(Path("/run/systemd/system") / job.units[role])].decode()
            with self.subTest(role=role):
                self.assertIn("-" + self.preserved, unit)
                self.assertEqual("UnsetEnvironment=GOBY_WEB_DIR\n" in unit, role == "server")
                self.assertEqual(job.loaded_inaccessible_paths[role], sorted(M.inaccessible_paths(job.value)))

    def test_v2_loaded_units_reject_missing_or_duplicate_hiding_before_start(self):
        for role in ("postgres", "server"):
            for hiding in ("missing", "duplicate"):
                with self.subTest(role=role, hiding=hiding):
                    self.provision_to_loaded_guard(changed_role=role, hiding=hiding)
        for unset in ("missing", "", "GOBY_WEB_DIR GOBY_WEB_DIR", "GOBY_WEB_DIR OTHER_OVERRIDE"):
            with self.subTest(unset=unset):
                self.provision_to_loaded_guard(unset=unset)

    def test_product_manifest_keeps_the_two_phase_and_embedded_claim_boundaries(self):
        value = self.evidence()["input"]
        fields = M.product_manifest_fields(value)
        self.assertEqual(fields["provisionVersion"], 2)
        self.assertEqual(fields["ordinaryRegressionStatus"], "passed_with_explicit_profile_gap")
        self.assertEqual(fields["ordinaryRegressionPhases"], 2)
        self.assertIs(fields["taggedFullRegressionClaimed"], False)
        self.assertEqual(fields["productEvidence"], {key: value[key] for key in
                         ("sourceArchive", "sourceManifest", "embeddedBuildManifest", "productionSourceBinding", "regressionReview", "regressionClosure")})
        self.assertEqual(fields["dashboard"], {"mode": "embedded", "buildManifest": value["embeddedBuildManifest"], "assetCount": 57,
                                               "externalDirectoryInstalled": False, "webDirectoryOverridePresent": False})
        legacy = Guards.input(self)
        self.assertEqual(M.product_manifest_fields(legacy), {"frontendReport": legacy["frontendReport"], "backendReport": legacy["backendReport"]})


if __name__ == "__main__":
    unittest.main()
