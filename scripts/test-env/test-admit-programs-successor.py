#!/usr/bin/env python3
"""Finite Programs admission checks using existing fixtures and in-memory IO substitutes."""
import contextlib
import copy
import importlib.util
import io as text_io
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch
from urllib.parse import parse_qs, urlsplit


spec = importlib.util.spec_from_file_location("programs_tv_fixtures", Path(__file__).with_name("test-admit-tv-parent-successor.py"))
tv = importlib.util.module_from_spec(spec)
spec.loader.exec_module(tv)
m = tv.m
pin = tv.pin


def authority():
    """Synthetic descriptors remain in memory; this does not prepare an execution input."""
    old_input, previous, old_binding, seed, _, _, older, _ = tv.authority()
    value = {key: copy.deepcopy(old_input[key]) for key in (
        "kind", "runId", "runtimeHelper", "admissionHelper", "compiledCatalog", "budgets")}
    value.update(version=5, admissionKind="affected_programs", output=m.TV_ROOT + "candidate-live-admission-programs-fixture",
                 runtimeEpoch=pin("programs-epoch"), seedRuntimeBinding=pin("programs-binding"),
                 reusedAdmission05=copy.deepcopy(m.PROGRAMS_REUSED), transitionCloseout=pin("programs-closeout"))
    epoch = copy.deepcopy(previous)
    epoch.update(version=4, operationKind="programs_successor", previousEpoch=copy.deepcopy(m.TV_EPOCH),
                 previousCurrentRuntime=pin("previous-current-runtime"), before=pin("programs-before"), after=pin("programs-after"),
                 sourceBefore=pin("programs-source-before"), sourceAfter=pin("programs-source-after"),
                 preservation=pin("programs-preservation"), transitionHelper=pin("programs-transition-helper"),
                 candidateProcess={"pid": 33}, helpers={"seed": pin("seed-helper")},
                 currentSource={"sourceManifest": {"path": "/opt/goby-test/programs-source", "sha256": "d" * 64},
                                "binary": {"path": "/opt/candidate/goby", "sha256": "e" * 64}})
    binding = copy.deepcopy(old_binding)
    binding.update(version=4, runtimeEpoch=value["runtimeEpoch"], previousBinding=copy.deepcopy(m.TV_BINDING),
                   previousCurrentRuntime=epoch["previousCurrentRuntime"])
    transition = {"compiledCatalog": value["compiledCatalog"], "previousEpoch": epoch["previousEpoch"],
                  "previousBinding": binding["previousBinding"], "currentRuntime": epoch["previousCurrentRuntime"]}
    reused = copy.deepcopy(older)
    reused.update(version=3, admissionKind="affected_tv_parent", runtimeEpoch=copy.deepcopy(m.TV_EPOCH),
                  seedRuntimeBinding=copy.deepcopy(m.TV_BINDING), currentSource=copy.deepcopy(previous["currentSource"]),
                  freshChecks=dict.fromkeys(m.TV_CHECKS, True), requests={"normal": 20, "cleanup": 4},
                  controllerSessions={role: {"sameTokenRejected": True} for role in ("P", "Q")},
                  reusedAdmission04={"report": copy.deepcopy(m.TV_REUSED), "runtimeEpoch": older["runtimeEpoch"],
                                     "seedRuntimeBinding": older["seedRuntimeBinding"], "currentSource": older["currentSource"],
                                     "contracts": list(m.TV_REUSED_CONTRACTS)})
    closeout = {key: copy.deepcopy(epoch[key]) for key in (
        "currentSource", "previousEpoch", "previousCurrentRuntime", "before", "after", "sourceBefore", "sourceAfter",
        "preservation", "runtimeHelper", "candidateProcess", "postgresProcess", "calls")}
    closeout.update(kind="audited-candidate-programs-transition-closeout", version=1,
                    status="successor_running_awaiting_affected_live_admission", runtimeEpoch=value["runtimeEpoch"],
                    seedRuntimeBinding=value["seedRuntimeBinding"], candidateAdmissionComplete=False, clientAcceptance=False,
                    currentProcessPinned=True, oldProcessAbsent=True, stagedBinaryAbsent=True, installedBinaryVerified=True)
    return tuple(copy.deepcopy(row) for row in (value, epoch, binding, seed, transition, previous, reused, older, closeout))


def response(value, status=200):
    raw = b"" if status == 204 else value if isinstance(value, bytes) else m.canonical(value)
    return {"body": value, "raw": raw, "status": status, "complete": True, "receipt": pin("response"),
            "headers": [("Content-Type", "application/json; charset=utf-8"), ("Content-Length", str(len(raw)))]}


def runner_fixture():
    values = authority()
    value, epoch, binding, seed, transition, previous, reused, older, closeout = values
    latest = {"capturedAt": "2026-09-15T12:00:00Z", "tables": {"sessions": [{"id": "latest-closed-session"}]}, "sequences": {}}
    baseline = {"source": copy.deepcopy(latest), "inactiveStage": {"capturedAt": latest["capturedAt"], "tables": {}, "sequences": {}}}
    records = {selected["path"]: copy.deepcopy(record) for selected, record in (
        (value["runtimeEpoch"], epoch), (value["seedRuntimeBinding"], binding), (epoch["seedProvenance"], seed),
        (epoch["previousEpoch"], previous), (value["reusedAdmission05"], reused), (m.TV_REUSED, older),
        (value["transitionCloseout"], closeout), (value["compiledCatalog"], {}),
        (epoch["currentSource"]["sourceManifest"], {}), (epoch["after"], baseline), (epoch["sourceAfter"], latest))}
    captured, opened = {}, []
    helper = SimpleNamespace(read_checked=lambda *args, **kwargs: b"ignored")
    def write(path, data):
        captured[Path(path).name] = copy.deepcopy(data)
        return pin(Path(path).name)
    helper.write_json_once = write
    runtime_io = SimpleNamespace(created=False, private=Path("/opt/not-written/private"), output=Path(value["output"]),
                                 reader=object(), requests={"normal": 16, "cleanup": 4}, request_states={})
    def open_io():
        opened.append(True)
        runtime_io.created = True
    runtime_io.open = open_io
    runtime = SimpleNamespace(validate_epoch=lambda row: row, resolve_epoch_lineage=lambda *args: {"productInput": transition},
                              validate_programs_successor_input=lambda row: row, load_helper=lambda *args: helper,
                              validate_seed_runtime_binding=lambda *args: binding, EpochIO=Mock(return_value=runtime_io))
    class ControlReaders:
        def file(self, *args):
            return None
        def tree(self, *args):
            return None
    transition_reader = SimpleNamespace(ProgramsSuccessor=ControlReaders)
    def read(path, checksum):
        if path in (value["runtimeHelper"]["path"], epoch["transitionHelper"]["path"]):
            return b"not-executed"
        if path not in records:
            raise m.AdmissionError("fixture_missing_saved_pin")
        return m.canonical(records[path])
    def import_module(name, path, raw):
        return runtime if path == value["runtimeHelper"]["path"] else transition_reader
    class Job:
        def __init__(self, runtime_io, *args, baseline, **kwargs):
            self.io, self.baseline, self.phase = runtime_io, baseline, "programs"
            self.cleanup_end, self.inspector, self.control_reader = None, None, None
            self.failures, self.samples, self.phase_counts = [], [], {"programs": 10}
            self.fresh_checks = dict.fromkeys(m.PROGRAMS_CHECKS, True)
            self.evidence = {label: pin("new-admission-" + label) for label in (
                "source-before", "source-after", "inactive-before", "inactive-after", "controls-before", "controls-after")}
            self.logins = {role: {"initialSession": {"id": role}, "auth": {"token": "fixture-" + role}, "logout401": True}
                           for role in ("P", "Q")}
            captured["job"] = self
        def run(self):
            pass
        def close_sessions(self):
            captured["cleanup"] = True
        def reconcile(self):
            return {"preservedTables": 32, "newSessions": 2, "newDevices": 2, "newActivityRows": 4}
    return SimpleNamespace(values=values, records=records, read=read, import_module=import_module, runtime=runtime,
                           helper=helper, Job=Job, captured=captured, opened=opened, latest=latest)


def reader_module(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


@contextlib.contextmanager
def recovery_controls_fixture(*, missing_reference=False, changed_reference=False):
    """Use the source readers with in-memory historical JSON and filesystem metadata."""
    runtime = reader_module("admission_recovery_runtime", "audited-candidate-runtime.py")
    transition = reader_module("admission_recovery_transition", "transition-audited-candidate.py")
    seed = reader_module("admission_recovery_seed", "seed-audited-candidate.py")
    root = runtime.C / "data/recovery"
    relative = "generation-" + runtime.PROGRAMS_RECOVERY_GENERATION
    directory, key = root / relative, root / relative / "master.key"
    directory_facts = {"directory": True, "dev": 1, "ino": 2, "uid": 995, "gid": 995,
                       "mode": 0o700, "mtimeNs": 3, "ctimeNs": 4}
    root_facts = {**directory_facts, "ino": 1}
    key_facts = {"access": "stat_only", "dev": 1, "ino": 5, "uid": 995, "gid": 995,
                 "mode": 0o600, "bytes": 32, "mtimeNs": 6, "ctimeNs": 7}
    marker = {"version": 1, "deploymentId": runtime.PROGRAMS_RECOVERY_DEPLOYMENT,
              "generationId": runtime.PROGRAMS_RECOVERY_GENERATION, "slot": "recovery"}
    generation = {"id": runtime.PROGRAMS_RECOVERY_GENERATION, "complete": True,
                  "master": {"name": "master.key", "size": 32, "identity": {"device": 1, "inode": 5}},
                  "directory": {"device": 1, "inode": 2}}
    restore = {"id": runtime.PROGRAMS_RECOVERY_OPERATION, "generationId": runtime.PROGRAMS_RECOVERY_GENERATION,
               "kind": "restore", "state": "cancelled", "phase": "finished", "authorized": True,
               "applyAuthorized": False, "cancelAuthorized": True,
               "sourceState": {"deploymentId": runtime.PROGRAMS_RECOVERY_DEPLOYMENT, "databaseSlot": "primary", "master": "default"}}
    reference = {"controlDocuments": {
        "recovery/generation-registry.json": {"version": 1, "deploymentId": runtime.PROGRAMS_RECOVERY_DEPLOYMENT,
                                               "generations": [generation]},
        "operations/current.json": {"payload": {"operations": [restore], "slots": [
            {"slot": "primary", "state": "active"},
            {"slot": "recovery", "state": "staged", "imageId": runtime.PROGRAMS_RECOVERY_GENERATION,
             "operation": runtime.PROGRAMS_RECOVERY_OPERATION, "retained": {"marker": marker}}]}}},
        "inactiveStage": {"tables": {"server_settings": [{"key": "goby.recovery.binding.v1", "value": m.canonical(marker).decode()}]}},
        "trees": {str(root): {".": root_facts, relative: directory_facts,
                              relative + "/master.key": {**key_facts, "mtimeNs": 2, "ctimeNs": 3, "sha256": "e" * 64}}}}
    reference_raw = m.canonical(reference)
    reference_pin = {**runtime.PROGRAMS_RECOVERY_REFERENCE, "sha256": m.sha(reference_raw)}
    raw = reference_raw + b"\n" if changed_reference else reference_raw
    tree = {".": root_facts, relative: directory_facts, relative + "/master.key": key_facts}
    baseline = {"trees": {str(root): tree}, "controlDocuments": {}, "fixedFiles": {str(key): key_facts},
                "loadedUnits": {}, "protected": {unit: {"Id": unit, "MainPID": str(index + 100)}
                    for index, unit in enumerate(sorted(runtime.PROGRAMS_PROTECTED))}, "hostingBefore": {}, "hostingAfter": {}}
    def metadata(facts):
        return SimpleNamespace(st_mode=(0o040000 if facts.get("directory") else 0o100000) | facts["mode"],
                               st_nlink=1, st_dev=facts["dev"], st_ino=facts["ino"], st_uid=facts["uid"], st_gid=facts["gid"],
                               st_size=facts.get("bytes", 0), st_mtime_ns=facts["mtimeNs"], st_ctime_ns=facts["ctimeNs"])
    reference_info = metadata({**key_facts, "uid": 0, "gid": 0, "mode": 0o600, "bytes": len(raw)})
    observed = {str(root): metadata(root_facts), str(directory): metadata(directory_facts),
                str(key): metadata(key_facts), reference_pin["path"]: reference_info}
    reads, stats = [], []
    def safe_path(path, *args):
        name = str(path)
        if name == reference_pin["path"] and missing_reference:
            raise FileNotFoundError("fixture_reference_missing")
        stats.append(name)
        return observed[name]
    def open_reference(path, flags):
        if str(path) != reference_pin["path"]:
            raise AssertionError("key_body_open_forbidden")
        reads.append(str(path))
        return 42
    class ReferenceStream(text_io.BytesIO):
        def fileno(self):
            return 42
    show = Mock(side_effect=lambda unit: copy.deepcopy(baseline["protected"][unit]))
    runtime_io = SimpleNamespace(modules={"seed": seed, "gateway": object(), "provision": object()}, provision=SimpleNamespace(show=show))
    with contextlib.ExitStack() as stack:
        for target, name, value in ((runtime, "TREE_ROOTS", (str(root),)),
                                    (runtime, "PROGRAMS_RECOVERY_REFERENCE", reference_pin)):
            stack.enter_context(patch.object(target, name, value))
        descriptor = stack.enter_context(patch.object(seed, "descriptor", wraps=seed.descriptor))
        project = stack.enter_context(patch.object(runtime, "programs_recovery_master_authority", wraps=runtime.programs_recovery_master_authority))
        hosting = stack.enter_context(patch.object(transition.BinarySuccessor, "hosting", return_value={}))
        stack.enter_context(patch.object(transition.Transition, "loaded_units", return_value={}))
        stack.enter_context(patch.object(transition.Transition, "file", side_effect=AssertionError("legacy_body_reader_forbidden")))
        stack.enter_context(patch.object(seed, "safe_path", side_effect=safe_path))
        stack.enter_context(patch.object(seed.pwd, "getpwnam", return_value=SimpleNamespace(pw_uid=995)))
        stack.enter_context(patch.object(seed.os, "open", side_effect=open_reference))
        stack.enter_context(patch.object(seed.os, "fdopen", side_effect=lambda *args: ReferenceStream(raw)))
        stack.enter_context(patch.object(seed.os, "fstat", return_value=reference_info))
        stack.enter_context(patch.object(transition.os.path, "lexists", side_effect=lambda path: str(path) in observed))
        scan = stack.enter_context(patch.object(transition.Path, "rglob", return_value=[directory, key]))
        stack.enter_context(patch.object(transition.Path, "is_symlink", return_value=False))
        stack.enter_context(patch.object(transition.Path, "is_dir", autospec=True, side_effect=lambda path: path in (root, directory)))
        stack.enter_context(patch.object(transition.Path, "lstat", autospec=True, side_effect=lambda path: observed[str(path)]))
        yield SimpleNamespace(runtime=runtime, transition=transition, io=runtime_io, baseline=baseline, reference=reference,
                              pin=reference_pin, descriptor=descriptor, project=project, hosting=hosting, scan=scan,
                              reads=reads, stats=stats, show=show, key=str(key), relative=relative + "/master.key", root=str(root))


class ProgramsAdmissionGuards(unittest.TestCase):
    def test_v5_input_keeps_historical_dispatch_and_request_limits(self):
        value = authority()[0]
        self.assertEqual(m.validate_input(value), value)
        self.assertEqual(m.validate_input(tv.input_value())["version"], 3)
        self.assertEqual(value["budgets"], {"maximumSeconds": 180, "cleanupSeconds": 60, "maximumRequests": 28, "cleanupRequests": 8})
        for update in ({"version": 4}, {"admissionKind": "fresh_embedded"}, {"runtimeEpoch": m.TV_EPOCH},
                       {"seedRuntimeBinding": m.TV_BINDING}, {"transitionCloseout": m.TV_CLOSEOUT},
                       {"reusedAdmission05": pin("unreviewed-report")}, {"budgets": dict(value["budgets"], maximumRequests=29)},
                       {"budgets": dict(value["budgets"], maximumSeconds=180.0)}):
            changed = copy.deepcopy(value)
            changed.update(update)
            with self.subTest(update=tuple(update)), self.assertRaises(m.AdmissionError):
                m.validate_input(changed)
        del value["transitionCloseout"]
        with self.assertRaises(m.AdmissionError):
            m.validate_input(value)

    def test_authority_preserves_seed_and_admission05_to04_scope(self):
        values = authority()
        original = copy.deepcopy(values)
        self.assertEqual(m.validate_programs_authority(*values)["pid"], 33)
        self.assertEqual(values, original)
        for mutate in (
                lambda v: v[2].update(seedExecutor=pin("other-seed")),
                lambda v: v[3]["actors"].update(unreviewed={}),
                lambda v: v[4].update(currentRuntime=pin("other-runtime")),
                lambda v: v[6].update(currentSource=v[1]["currentSource"]),
                lambda v: v[6]["freshChecks"].update(tvDetailParents=False),
                lambda v: v[6].update(requests={"normal": 16, "cleanup": 4}),
                lambda v: v[6]["reusedAdmission04"].update(report=pin("other-admission04")),
                lambda v: v[7]["controllerSessions"]["admin"].update(sameTokenRejected=False),
                lambda v: v[7].update(candidateAdmissionComplete=False)):
            changed = list(authority())
            mutate(changed)
            with self.assertRaises(m.AdmissionError):
                m.validate_programs_authority(*changed)

    def test_current_transition_requires_complete_independent_closeout(self):
        for key, replacement in (("runtimeEpoch", pin("wrong-epoch")), ("seedRuntimeBinding", pin("wrong-binding")),
                                 ("sourceAfter", pin("prior-source")), ("preservation", pin("other-proof")),
                                 ("previousCurrentRuntime", pin("wrong-current-runtime")), ("candidateProcess", {"pid": 99}),
                                 ("installedBinaryVerified", False), ("oldProcessAbsent", False),
                                 ("candidateAdmissionComplete", True), ("version", True)):
            values = list(authority())
            values[-1][key] = replacement
            with self.subTest(key=key), self.assertRaises(m.AdmissionError):
                m.validate_programs_authority(*values)

    def test_empty_query_requires_exact_typed_dto_and_content_type(self):
        m.validate_programs_empty(response({"Items": [], "TotalRecordCount": 0}))
        for value in (None, [], {"Items": [], "TotalRecordCount": False}, {"Items": [], "TotalRecordCount": 0.0},
                      {"Items": [], "TotalRecordCount": "0"}, {"Items": None, "TotalRecordCount": 0},
                      {"Items": [{}], "TotalRecordCount": 0}, {"Items": [], "TotalRecordCount": 0, "StartIndex": 0}):
            with self.subTest(value=value), self.assertRaises(m.AdmissionError):
                m.validate_programs_empty(response(value))
        changed = response({"Items": [], "TotalRecordCount": 0})
        changed["headers"][0] = ("Content-Type", "text/plain")
        with self.assertRaises(m.AdmissionError):
            m.validate_programs_empty(changed)

    def test_fixed_request_plan_uses_retained_actors_and_only_twenty_requests(self):
        clock, calls = [0.0], []
        runtime_io = SimpleNamespace(started=0.0, value={"runId": "fixture"}, candidate={"publicUrl": "http://candidate"},
                                     requests={"normal": 0, "cleanup": 0}, private=Path("/opt/not-written/private"))
        runtime_io.deadline = lambda cleanup=False: (180 if cleanup else 120) - clock[0]
        runtime_io.pin = lambda: {"pid": 33}
        def request(label, method, route, *, auth, expected, cleanup, **kwargs):
            bucket = "cleanup" if cleanup else "normal"
            runtime_io.requests[bucket] += 1
            self.assertLessEqual(runtime_io.requests[bucket], 8 if cleanup else 20)
            path, query = urlsplit(route).path, parse_qs(urlsplit(route).query)
            actor = auth["token"][-1] if auth else None
            calls.append((method, path, query, actor, cleanup))
            if path == "/healthz":
                result = response({"Status": "ok"})
            elif path == "/readyz":
                result = response({"Status": "ready"})
            elif path == "/emby/System/Info/Public":
                result = response({"Id": "server", "StartupWizardCompleted": True, "LocalAddress": "http://candidate"})
            elif path == "/emby/Users/AuthenticateByName":
                result = response({})
            elif path.endswith("/Views"):
                result = response({"Items": [{"Id": "library", "Type": "CollectionFolder", "ServerId": "server"}] if actor == "P" else []})
            elif path == "/emby/LiveTv/Programs":
                if actor == "Q" and query["UserId"] == ["actor-P"]:
                    result = response({"ResponseStatus": {"ErrorCode": "access_denied"}}, 403)
                elif actor == "Q":
                    result = response({"ResponseStatus": {"ErrorCode": "not_found"}}, 404)
                elif query.get("LibrarySeriesId") == ["episode"]:
                    result = response({"ResponseStatus": {"ErrorCode": "invalid_input"}}, 400)
                else:
                    result = response({"Items": [], "TotalRecordCount": 0})
            elif path == "/emby/Sessions/Logout":
                result = response(b"", 204)
            elif path == "/emby/System/Info":
                result = response(b"Access token is invalid or expired.", 401)
            else:
                self.fail("Unexpected request outside the fixed Programs admission")
            self.assertIn(result["status"], expected)
            return result
        runtime_io.request = request
        helper = SimpleNamespace(items=lambda value: value["Items"], write_json_once=lambda *args: pin("saved"))
        inspector = SimpleNamespace(deployment_lease=lambda: {"revision": 1})
        with patch.object(m.time, "monotonic", side_effect=lambda: clock[0]):
            job = m.ProgramsAdmission(runtime_io, helper, inspector, {"catalog": {"series": {"id": "series"}},
                    "libraries": {"TV": {"Id": "library"}}}, {}, epoch={}, baseline={}, expected_process={"pid": 33})
            def preflight():
                job.server_id, job.lease, job.detail_ids = "server", {"revision": 1}, ["season", "episode"]
                job.credentials = {role: {"actorId": "actor-" + role} for role in ("P", "Q")}
                job.fresh_checks["runtimeIdentity"] = True
                job.health_due()
            job.preflight = preflight
            def login(role):
                job.req("login-" + role, "POST", "/emby/Users/AuthenticateByName", body={"Username": role, "Pw": "fixture"})
                job.logins[role] = {"auth": {"kind": "emby", "token": "fixture-" + role}, "initialSession": {"id": role}, "logout401": False}
            job.login = login
            def advance(seconds):
                clock[0] += seconds
                job.health_due()
            job.wait = advance
            job.run()
            self.assertEqual(runtime_io.requests, {"normal": 16, "cleanup": 0})
            self.assertEqual([row["elapsedMilliseconds"] for row in job.samples], [0, 30000, 60000])
            job.close_sessions()
        self.assertEqual(runtime_io.requests, {"normal": 16, "cleanup": 4})
        self.assertEqual(len(calls), 20)
        self.assertEqual(job.phase_counts, {"programs": 10})
        self.assertTrue(job.fresh_checks["sessionCleanup"])
        program_calls = [row for row in calls if row[1] == "/emby/LiveTv/Programs"]
        self.assertEqual(len(program_calls), 5)
        for _, _, query, _, _ in program_calls:
            self.assertEqual({key: query[key] for key in m.PROGRAMS_QUERY}, {
                "HasAired": ["false"], "SortBy": ["StartDate"], "ImageTypeLimit": ["1"],
                "EnableImageTypes": ["Primary,Thumb,Backdrop"], "EnableUserData": ["false"],
                "Fields": ["PrimaryImageAspectRatio,ChannelInfo"], "Limit": ["12"], "X-Emby-Language": ["en-us"]})
        self.assertNotIn("LibrarySeriesId", program_calls[0][2])
        self.assertEqual([row[3] for row in program_calls], ["P", "P", "Q", "Q", "P"])
        self.assertEqual(program_calls[-1][2]["LibrarySeriesId"], ["episode"])

    def test_transition_failure_or_missing_pin_cannot_open_runtime(self):
        for failure in ("missing-closeout", "wrong-closeout", "wrong-source-after"):
            fixture = runner_fixture()
            value, epoch, _, _, _, _, _, _, _ = fixture.values
            if failure == "missing-closeout":
                del fixture.records[value["transitionCloseout"]["path"]]
            elif failure == "wrong-closeout":
                fixture.records[value["transitionCloseout"]["path"]]["oldProcessAbsent"] = False
            else:
                fixture.records[epoch["sourceAfter"]["path"]]["tables"]["sessions"] = []
            with patch.object(m, "initial_read", side_effect=fixture.read), patch.object(m, "import_bytes", side_effect=fixture.import_module), \
                    patch.object(m, "validate_catalog"), self.assertRaises(m.AdmissionError):
                m.run_tv_parent_admission(value, pin("input"), 0.0, programs=True)
            self.assertEqual(fixture.opened, [])

    def test_report_exports_its_own_post_admission_snapshots(self):
        fixture = runner_fixture()
        value, epoch = fixture.values[:2]
        with patch.object(m, "initial_read", side_effect=fixture.read), patch.object(m, "import_bytes", side_effect=fixture.import_module), \
                patch.object(m, "validate_catalog"), patch.object(m, "ProgramsAdmission", fixture.Job), \
                patch.object(m.signal, "signal"), patch.object(m.signal, "setitimer"), patch.object(m.time, "monotonic", return_value=0.0), \
                contextlib.redirect_stdout(text_io.StringIO()):
            self.assertEqual(m.run_tv_parent_admission(value, pin("input"), 0.0, programs=True), 0)
        report = fixture.captured["report.json"]
        self.assertEqual((report["version"], report["admissionKind"]), (5, "affected_programs"))
        self.assertEqual(fixture.captured["job"].baseline["source"], fixture.latest)
        self.assertEqual(report["sourceAfter"], pin("new-admission-source-after"))
        self.assertNotEqual(report["sourceAfter"], epoch["sourceAfter"])
        self.assertEqual(set(report["freshChecks"]), set(m.PROGRAMS_CHECKS))
        self.assertEqual(report["reusedAdmission05"]["report"], m.PROGRAMS_REUSED)
        self.assertEqual(report["reusedAdmission05"]["reusedAdmission04"]["report"], m.TV_REUSED)
        self.assertFalse(report["clientAcceptance"])
        self.assertTrue(report["candidateAdmissionComplete"])
        self.assertTrue(fixture.captured["cleanup"])

    def test_cleanup_exception_preserves_first_failure_and_pending_responsibility(self):
        fixture = runner_fixture()
        runtime_io = fixture.runtime.EpochIO.return_value
        runtime_io.requests = {"normal": 3, "cleanup": 0}
        runtime_io.request_states = {"owned-login": {"cleanupRequired": True}}
        def fail_run(job):
            for login in job.logins.values():
                login["logout401"] = False
            raise m.AdmissionError("fixture_normal_failure")
        def fail_cleanup(job):
            raise m.AdmissionError("fixture_cleanup_deadline")
        fixture.Job.run, fixture.Job.close_sessions = fail_run, fail_cleanup
        with patch.object(m, "initial_read", side_effect=fixture.read), patch.object(m, "import_bytes", side_effect=fixture.import_module), \
                patch.object(m, "validate_catalog"), patch.object(m, "ProgramsAdmission", fixture.Job), \
                patch.object(m.signal, "signal"), patch.object(m.signal, "setitimer"), patch.object(m.time, "monotonic", return_value=0.0), \
                contextlib.redirect_stdout(text_io.StringIO()):
            self.assertEqual(m.run_tv_parent_admission(fixture.values[0], pin("input"), 0.0, programs=True), 2)
        report = fixture.captured["report.json"]
        self.assertEqual(report["failure"]["code"], "fixture_normal_failure")
        self.assertEqual(report["cleanupFailures"][0]["code"], "fixture_cleanup_deadline")
        self.assertEqual(report["requestResponsibilities"], {"owned-login": {"cleanupRequired": True}})
        self.assertEqual(report["requests"], {"normal": 3, "cleanup": 0})
        self.assertFalse(report["candidateAdmissionComplete"])
        self.assertTrue(all(row["sameTokenRejected"] is False for row in report["controllerSessions"].values()))

    def test_private_result_publication_failure_is_terminal_without_retry(self):
        fixture, attempted, output = runner_fixture(), [], text_io.StringIO()
        def fail_write(path, value):
            attempted.append(Path(path).name)
            raise OSError("synthetic private storage failure")
        fixture.helper.write_json_once = fail_write
        with patch.object(m, "initial_read", side_effect=fixture.read), patch.object(m, "import_bytes", side_effect=fixture.import_module), \
                patch.object(m, "validate_catalog"), patch.object(m, "ProgramsAdmission", fixture.Job), \
                patch.object(m.signal, "signal"), patch.object(m.signal, "setitimer"), patch.object(m.time, "monotonic", return_value=0.0), \
                contextlib.redirect_stdout(output):
            self.assertEqual(m.run_tv_parent_admission(fixture.values[0], pin("input"), 0.0, programs=True), 2)
        summary = m.parse(output.getvalue().encode())
        self.assertEqual(attempted, ["report.json"])
        self.assertEqual(summary["status"], "admission_report_unavailable_resources_retained")
        self.assertFalse(summary["candidateAdmissionComplete"])
        self.assertEqual(summary["reportUnavailable"]["code"], "admission_report_publication_failed")
        self.assertEqual(summary["requests"], {"normal": 16, "cleanup": 4})

    def test_programs_controls_never_fall_back_to_old_file_reader(self):
        touched, ordinary_reads = [], []
        facts = {"/private/runtime.env": {"sha256": "a" * 64}, "/private/master.key": {"access": "stat_only"},
                 "/private/absent-master.key": {"access": "stat_only", "absent": True}, "/private/catalog.json": {"sha256": "b" * 64}}
        baseline = {"trees": {"/tree": {}}, "controlDocuments": {}, "fixedFiles": facts, "loadedUnits": {},
                    "protected": {}, "hostingBefore": {}, "hostingAfter": {}}
        class OldReaders:
            def file(probe, path, prefix=None):
                if str(path) != "/private/catalog.json":
                    raise AssertionError("Protected file reached the old body reader")
                ordinary_reads.append(str(path))
                return copy.deepcopy(facts[str(path)]), b"{}"
            def loaded_units(probe):
                return {}
        class ProgramsReaders(OldReaders):
            def file(probe, path, prefix=None):
                touched.append(str(path))
                if str(path) == "/private/catalog.json":
                    return super().file(path, prefix)
                return copy.deepcopy(facts[str(path)]), None
            def tree(probe, root):
                touched.append(root)
                return {}, {}
        transition = SimpleNamespace(Transition=OldReaders, ProgramsSuccessor=ProgramsReaders,
                                     BinarySuccessor=SimpleNamespace(hosting=lambda probe: {}))
        runtime = SimpleNamespace(TREE_ROOTS=("/tree",), PROTECTED=(), PROGRAMS_PROTECTED=(),
                                  PROGRAMS_STAT_ONLY={"/private/master.key", "/private/absent-master.key"},
                                  PROGRAMS_RECOVERY_REFERENCE=pin("recovery-reference"), programs_recovery_master_authority=Mock(return_value={}))
        runtime_io = SimpleNamespace(modules={"seed": SimpleNamespace(descriptor=Mock(return_value={})),
                                             "gateway": object(), "provision": object()}, provision=object())
        self.assertEqual(m.capture_tv_controls(runtime, transition, runtime_io, baseline, lambda: 60, programs=True), baseline)
        self.assertEqual(touched, ["/tree", "/private/runtime.env", "/private/master.key", "/private/absent-master.key", "/private/catalog.json"])
        self.assertEqual(ordinary_reads, ["/private/catalog.json"])

    def test_programs_controls_use_hashed_reference_with_original_stat_only_readers(self):
        with recovery_controls_fixture() as fixture:
            observed = m.capture_tv_controls(fixture.runtime, fixture.transition, fixture.io, fixture.baseline, lambda: 60, programs=True)
            fixture.descriptor.assert_called_once_with(fixture.pin)
            fixture.project.assert_called_once_with(fixture.reference, fixture.pin)
            self.assertEqual(fixture.reads, [fixture.pin["path"]])
            self.assertIn(fixture.key, fixture.stats)
            self.assertEqual(observed, fixture.baseline)
            self.assertEqual(len(fixture.runtime.PROTECTED), 4)
            self.assertEqual(len(fixture.runtime.PROGRAMS_PROTECTED), 6)
            self.assertEqual({call.args[0] for call in fixture.show.call_args_list}, fixture.runtime.PROGRAMS_PROTECTED)
            self.assertEqual(fixture.show.call_count, 6)
            for facts in (observed["trees"][fixture.root][fixture.relative], observed["fixedFiles"][fixture.key]):
                self.assertEqual(facts["access"], "stat_only")
                self.assertEqual((facts["mtimeNs"], facts["ctimeNs"]), (6, 7))
                self.assertNotIn("sha256", facts)
                self.assertNotIn("prefixSha256", facts)
        with recovery_controls_fixture() as fixture:
            peer_units = fixture.runtime.PROGRAMS_PROTECTED - set(fixture.runtime.PROTECTED)
            self.assertEqual(len(peer_units), 2)
            for unit in peer_units:
                with self.subTest(changed_protected_unit=unit):
                    def changed(selected):
                        value = copy.deepcopy(fixture.baseline["protected"][selected])
                        if selected == unit:
                            value["MainPID"] = "99999"
                        return value
                    fixture.show.side_effect = changed
                    with self.assertRaisesRegex(m.AdmissionError, "tv_control_configuration_or_hosting_changed"):
                        m.capture_tv_controls(fixture.runtime, fixture.transition, fixture.io, fixture.baseline, lambda: 60, programs=True)

    def test_programs_controls_reject_missing_or_changed_reference_before_scan(self):
        for options, error, message in (({"missing_reference": True}, FileNotFoundError, "fixture_reference_missing"),
                                        ({"changed_reference": True}, ValueError, "Authority bytes changed")):
            with self.subTest(options=options), recovery_controls_fixture(**options) as fixture:
                with self.assertRaisesRegex(error, message):
                    m.capture_tv_controls(fixture.runtime, fixture.transition, fixture.io, fixture.baseline, lambda: 60, programs=True)
                fixture.descriptor.assert_called_once_with(fixture.pin)
                fixture.project.assert_not_called()
                fixture.hosting.assert_not_called()
                fixture.scan.assert_not_called()
                self.assertNotIn(fixture.key, fixture.stats)
        for change in ("missing", "extra"):
            with self.subTest(protected_inventory=change), recovery_controls_fixture() as fixture:
                if change == "missing":
                    peer = next(iter(fixture.runtime.PROGRAMS_PROTECTED - set(fixture.runtime.PROTECTED)))
                    del fixture.baseline["protected"][peer]
                else:
                    fixture.baseline["protected"]["unreviewed.service"] = {"Id": "unreviewed.service", "MainPID": "0"}
                with self.assertRaisesRegex(m.AdmissionError, "programs_protected_unit_inventory"):
                    m.capture_tv_controls(fixture.runtime, fixture.transition, fixture.io, fixture.baseline, lambda: 60, programs=True)
                fixture.descriptor.assert_not_called()
                fixture.hosting.assert_not_called()
                fixture.scan.assert_not_called()
                fixture.show.assert_not_called()

    def test_legacy_tv_controls_do_not_require_programs_recovery_reference(self):
        facts = {"sha256": "b" * 64}
        baseline = {"trees": {"/legacy": {}}, "controlDocuments": {}, "fixedFiles": {"/legacy/catalog.json": facts},
                    "loadedUnits": {}, "protected": {}, "hostingBefore": {}, "hostingAfter": {}}
        readers = SimpleNamespace(file=Mock(return_value=(facts, b"{}")), tree=Mock(return_value=({}, {})),
                                  loaded_units=Mock(return_value={}))
        transition = SimpleNamespace(Transition=readers, BinarySuccessor=SimpleNamespace(hosting=lambda probe: {}))
        runtime = SimpleNamespace(TREE_ROOTS=("/legacy",), PROTECTED=())
        seed = SimpleNamespace(descriptor=Mock(side_effect=AssertionError("legacy_reference_read_forbidden")))
        runtime_io = SimpleNamespace(modules={"seed": seed, "gateway": object(), "provision": object()}, provision=object())
        self.assertEqual(m.capture_tv_controls(runtime, transition, runtime_io, baseline, lambda: 60), baseline)
        self.assertEqual(readers.tree.call_count, 1)
        self.assertEqual(readers.file.call_count, 1)
        seed.descriptor.assert_not_called()

    def test_only_owned_authentication_delta_can_change_latest_state(self):
        before, after, logins = tv.state_delta()
        for state in (before, after):
            del state["tables"]["table_0"]
            state["tables"]["client_playback_references"] = [{"id": "foreign-audio-1"}, {"id": "foreign-audio-2"}]
            del state["tables"]["table_1"]
            state["tables"]["task_definitions"] = [{"id": "retained-refresh", "key": "library.refresh_media", "revision": 1}]
        self.assertEqual(m.reconcile_tv_source(before, after, logins)["newSessions"], 2)
        for mutate in (lambda row: row["tables"]["client_playback_references"].pop(),
                       lambda row: row["tables"]["play_sessions"][0].update(state="Expired"),
                       lambda row: row["tables"]["user_item_data"][0].update(position_ticks=0),
                       lambda row: row["tables"]["task_definitions"][0].update(revision=2),
                       lambda row: row["tables"]["sessions"][-1].update(revoked_at=None),
                       lambda row: row["sequences"]["users_seq"].update(lastValue="9")):
            changed = copy.deepcopy(after)
            mutate(changed)
            with self.assertRaises(m.AdmissionError):
                m.reconcile_tv_source(before, changed, logins)


if __name__ == "__main__":
    unittest.main()
