#!/usr/bin/env python3
"""Run one original-client scenario with owned workers and offline closeout."""
import argparse
import hashlib
import http.client
import io
import json
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import sys
import time
from datetime import datetime, timedelta, timezone

R = Path("/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14")
EPOCH = {"path": str(R / "candidate-tv-parent-transition-01/private/runtime-epoch.json"), "sha256": "76d7cc71be87851271272537795255f9ad7a5f5c3920dd6546e573f42d06bfac"}
BINDING = {"path": str(R / "candidate-tv-parent-transition-01/private/seed-runtime-binding.json"), "sha256": "94bd35e5523a56c60a9b712684d02785b05d6924820bb25f60c48ec8d3496c43"}
RETAINED_BASELINE = {"path": str(R / "candidate-core-movie04-failure-closeout.json"), "sha256": "5843a790e3c4ba09109d145b64fbda58f94c7ee94092e898ab584bab37880e8d"}
RETAINED_MOVIE_EPOCH = {"path": str(R / "candidate-backup-limits-revision-01/private/runtime-epoch.json"), "sha256": "72e25f907619fbdf82879070c6fce6178cc8c7881e8015a99991f62e64a2a73e"}
RETAINED_SNAPSHOT = {"path": str(R / "candidate-core-client-movie-04/private/source-after.json"), "sha256": "7209b3845d8290cd3883c76f5a95f00066d85d61103b8c5765493472196ad01c"}
RETAINED_PLAY = "play_40969548543a02735b847a89bc34671b"
RETAINED_AUTH = "aadd2636638e28cc6ceaf5bb8f2cb132"
MOVIE05_BASELINE = {"path": str(R / "candidate-core-movie05-owned-state-closeout.json"), "sha256": "7c38d00bcb4ece5e056e06a22be848cbd5e76c77f7d5b642d06f656be3dc27ab"}
MOVIE05_SNAPSHOT = {"path": str(R / "candidate-core-client-movie-05/private/source-after.json"), "sha256": "be0dbd70d4f7ea7d4343a3ea6259f80be216b9239e7acfa6552b2c7858b33bb0"}
MOVIE05_PREPARED = "play_b2977a52015916374e8a8af16188c191"
MOVIE05_AUTH = "e5649a6dc8451164243a4213f12d0ba2"
MOVIE05_HISTORY = {
    RETAINED_PLAY: ("Expired", RETAINED_AUTH, False, 0),
    "play_513080f4c7ffda49dba8a7002defdb6e": ("Expired", "85c836316077f2acd5e4ee2ed56d675d", False, 1197474760),
    MOVIE05_PREPARED: ("Prepared", MOVIE05_AUTH, False, 1217878390),
    "play_d8e2ffb318faa1277e4894f4c0496660": ("Stopped", MOVIE05_AUTH, True, 1217878390),
    "play_fd6ee1392b1de37c01e28d4e32377a06": ("Stopped", "85c836316077f2acd5e4ee2ed56d675d", True, 1197474760),
}
SERVER_LOG = Path("/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b/private/server-unit.log")
SERVER_LOG_LIMIT = 32 << 20
ADMISSION = {"path": str(R / "candidate-live-admission-05/private/report.json"), "sha256": "b73a2d30926c68886bd1674a356e6330eab2072afb53fb1fa1f695c5337f8535"}
ADMISSION_CLOSEOUT = {"path": str(R / "candidate-live-admission05-closeout.json"), "sha256": "86b224298601ff922d6fe136c026b87628282c675b925dae4831ce78068f139b"}
HOSTING = {"path": "/opt/goby-test/exec-work-m3e/core-av-original-client-hosting-reconcile-01/hosting.json", "sha256": "2100142b83941e24503838fdf92942aef862bc785bbcfcbe8f93223dd30c53c0"}
AV_VERIFICATION = {"path": str(R / "native-rejection-observer-verification-02/verification.json"), "sha256": "17c2b2aafb751417cc98827e642c6f6aac30e3202723d424d96f38cf22d3e9dd"}
REUSED_AV_VERIFICATION = {"path": str(R / "tv-parent-client-component-verification-03/verification.json"), "sha256": "85f831e66d1b2f3574141ebcb3c369156b71673066e31802c62f1398280e80df"}
RUNTIME = {"path": str(R / "candidate-successor-tool-verification-01/audited-candidate-runtime.py"), "sha256": "bbb89c798e92b2821b7fe450b11783abeb0fe4526e27bcdbe420bf43e558d922"}
NODE = {"path": "/usr/bin/node", "sha256": "ca0728526aa1cc4e3056decec848ecc6d2c5391cecdd4e21a0ebd221d665c84e"}
SOURCE_FILES = {"closer": "close-audited-candidate-client.mjs", "adapter": "client-browser-audited-candidate.mjs", "gateway": "client-acceptance-gateway.py", "proxy": "client-acceptance-proxy.py",
    "sessionProof": "client-browser-session-proof.mjs", "movie": "client-browser-playback.mjs", "audio": "client-browser-audio-flow.mjs", "subtitles": "client-browser-subtitle-flow.mjs", "tv": "client-browser-tv-flow.mjs"}
FROZEN = {"gateway": "b343f522389bbcb6f704ab3b09d2592ed06573b90961f7b9c8f3eb45b7ab0070", "adapter": "7b8a399d1179dbf4326ebbb860734d3a7355a040a67b50e6e086930bacbd6635", "closer": "94910123694fe6f772cb0cfcc80696ed1691a46d33b0669ef8e969f7d81cbb94",
    "proxy": "388965fc772dff82ff13d2a641ecc0e9383e20b9f2a9bc929f48edcf6f394874", "sessionProof": "fea90503a3e279d1ec63723f8785b3421c72d756af4db95f1762fbd67ece2472",
    "movie": "280f3457e11bc75ed920da2534fd079ba9888a95eb32590fd031345bd16d9325", "audio": "32a828b6c82f3dbd70f3b117bec11e3a6d44192cacb10417e4a75bf781a129e8",
    "subtitles": "bc377470137f6f9ee2804de009690f9f0f99b875a6bdb500d9f4709603b91dc0", "tv": "5acb53c7fd5852f501e6590d09974913e888b12bd64ac8aef233953dc7cdfb65"}
# This historical selection must not follow later edits to FROZEN.
REUSED_AV_SOURCES = {"gateway": "b343f522389bbcb6f704ab3b09d2592ed06573b90961f7b9c8f3eb45b7ab0070", "adapter": "07bd277f3c7eb7636e2625565129588f59660a1c4432c17c1c68e424456b0cf0", "closer": "94910123694fe6f772cb0cfcc80696ed1691a46d33b0669ef8e969f7d81cbb94",
    "proxy": "388965fc772dff82ff13d2a641ecc0e9383e20b9f2a9bc929f48edcf6f394874", "sessionProof": "fea90503a3e279d1ec63723f8785b3421c72d756af4db95f1762fbd67ece2472",
    "movie": "280f3457e11bc75ed920da2534fd079ba9888a95eb32590fd031345bd16d9325", "audio": "32a828b6c82f3dbd70f3b117bec11e3a6d44192cacb10417e4a75bf781a129e8",
    "subtitles": "e98d3ca5289ba8362450147484bc4cffd13f3d0177d00a266d21edfae0558016", "tv": "5acb53c7fd5852f501e6590d09974913e888b12bd64ac8aef233953dc7cdfb65"}
SCENARIOS = {"movie", "episode", "mp3", "flac", "subtitles", "tv-browse"}
BUDGETS = {"maximumSeconds": 1200, "cleanupSeconds": 240, "clientSeconds": 600, "clientCleanupSeconds": 120,
    "gatewayReadySeconds": 45, "workerGraceSeconds": 30, "unitStopSeconds": 30}
GATEWAY_BUDGETS = {"maxRequests": 1000, "cleanupRequests": 64, "maxApiBodyBytes": 1 << 20, "maxApiTotalBytes": 32 << 20,
    "maxSeconds": 1800, "idleSeconds": 600, "maxConcurrent": 32}
INPUT_KEYS = {"kind", "version", "runId", "scenario", "output", "runtimeEpoch", "seedBinding", "admission", "admissionCloseout", "hosting", "hostingInitialization", "avVerification", "runtimeHelper", "node", "compiledCatalog", "sources", "budgets", "gatewayBudgets"}
REVIEWED_BASELINE_PINS = {"closeout", "snapshot", "sourceEpoch", "runtimeEpoch", "seedBinding", "manifest"}
REVIEWED_BASELINE_KEYS = {"kind", "version", "status", "scenario", "actor", "item", "preparedExpirations", "movieProvenance", "boundary"} | REVIEWED_BASELINE_PINS
REVIEWED_TV_BASELINE_KEYS = (REVIEWED_BASELINE_KEYS - {"movieProvenance"}) | {"tvProvenance", "postBrowseAdmission"}
REVIEWED_TV_CATALOG_KEYS = ("tvLibrary", "series", "seasons", "episodes")
REVIEWED_BASELINE_TABLES = set("activity_entries application_key_clients application_key_devices application_keys catalog_entities client_playback_references devices encoding_jobs extra_reserved_paths item_entities item_extra_resources item_images item_metadata_state item_subtitles item_theme_resources items libraries library_roots managed_settings play_sessions scan_jobs schema_migrations server_settings sessions task_definitions task_occurrences task_run_children task_run_requests task_runs task_triggers theme_owner_ids theme_reserved_paths user_item_data user_settings users".split())
HOST_STARTUP_INPUT_KEYS = {"kind", "version", "output", "runtimeEpoch", "seedBinding", "runtimeHelper", "hosting", "gateway", "proxy", "compiledCatalog", "budgets"}
HOST_STARTUP_BUDGETS = {"maximumSeconds": 300, "cleanupSeconds": 60, "normalRequests": 16, "cleanupRequests": 4}
HOST_NETWORK_CONFIGURATION = {"HttpServerPortNumber": 28497, "PublicPort": 28497,
    "ServerName": "Goby Core AV Original Client Host 01", "LocalNetworkAddresses": ["127.0.0.1"], "EnableHttps": False,
    "EnableUPnP": False, "EnableRemoteAccess": False, "EnableAutoUpdate": False, "EnableAutomaticRestart": False,
    "AutoRunWebApp": False}
HOST_UNIT_FIELDS = "Id LoadState ActiveState SubState MainPID InvocationID Result ExecMainStatus ControlGroup NRestarts".split()
# Startup history is independent of the current candidate and transport pins.
HOST_STARTUP_HISTORY = {
    "runtimeEpoch": {"path": str(R / "candidate-backup-limits-revision-01/private/runtime-epoch.json"), "sha256": "72e25f907619fbdf82879070c6fce6178cc8c7881e8015a99991f62e64a2a73e"},
    "seedBinding": {"path": str(R / "candidate-backup-limits-revision-01/private/seed-runtime-binding.json"), "sha256": "92ee92478475390e514f39e554619f322be81062a6d0256820c00bf4c8e0969f"},
    "runtimeHelper": {"path": str(R / "backup-limits-tool-verification-01/audited-candidate-runtime.py"), "sha256": "1650d6267ab78a07f8c5b77130ad009eeb7212a0bb16251322936792a58dbe66"},
    "hostingInitialization": {"path": str(R / "client-host-startup-01/private/report.json"), "sha256": "3b951bbee3f124321ca6f8f05b90b3a3243969aa27fff6bc348d157b044b6a8d"},
}
HOST_STARTUP_TRANSPORT = {"gateway": "b343f522389bbcb6f704ab3b09d2592ed06573b90961f7b9c8f3eb45b7ab0070",
                          "proxy": "388965fc772dff82ff13d2a641ecc0e9383e20b9f2a9bc929f48edcf6f394874"}
AFFECTED_TV_PARENT_ADMISSION04 = {"path": str(R / "candidate-live-admission-04/private/report.json"), "sha256": "05083c7cc5c65c62e30018136b6a7d383c144c9742d96eaecc52e9c2cfc19653"}
AFFECTED_TV_PARENT_CLOSEOUT = {"path": str(R / "candidate-tv-parent-transition-closeout.json"), "sha256": "c9e03c008d0d1dbf0b66b8070692738e50a2ca57c29f89cbd40c1fba38d597ba"}


class RunError(ValueError):
    pass


def need(value, code):
    if not value:
        raise RunError(code)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False)


def descriptor(pin):
    need(isinstance(pin, dict) and set(pin) == {"path", "sha256"} and isinstance(pin["path"], str) and Path(pin["path"]).is_absolute() and
         ".." not in Path(pin["path"]).parts and isinstance(pin["sha256"], str) and re.fullmatch(r"[0-9a-f]{64}", pin["sha256"]), "descriptor_invalid")


COMPONENT_SCOPE = "retained_tv_browse_v5_components"
COMPONENT_FIELDS = {"kind", "version", "scope", "controller", "sources", "runtimeHelper",
                    "retainedBaseline", "currentRuntime", "ancestor", "evidence", "independentReview"}
COMPONENT_EVIDENCE = {"dispatch", "javascriptResult", "cases", "runtimeVerification"}


def component_json(raw):
    need(isinstance(raw, bytes) and len(raw) <= 4 << 20, "component_json_size")
    def unique(pairs):
        value = dict(pairs)
        need(len(value) == len(pairs), "component_json_duplicate_key")
        return value
    def invalid(unused):
        raise RunError("component_json_nonfinite")
    return json.loads(raw, object_pairs_hook=unique, parse_constant=invalid)


def component_read(pin, read_pin):
    descriptor(pin)
    raw = read_pin(pin)
    need(isinstance(raw, bytes) and hashlib.sha256(raw).hexdigest() == pin["sha256"], "component_read_digest")
    return raw


def validate_v5_component_selection(receipt, value, controller_pin, read_pin):
    """Check the selected source bytes before importing a new runtime helper."""
    need(value["version"] == 5 and value["scenario"] == "tv-browse" and value["avVerification"] != AV_VERIFICATION and
         value["sources"]["closer"]["sha256"] != FROZEN["closer"] and value["runtimeHelper"] != RUNTIME,
         "version5_components_not_admitted")
    need(isinstance(receipt, dict) and set(receipt) == COMPONENT_FIELDS and
         receipt["kind"] == "audited-candidate-client-component-admission" and type(receipt["version"]) is int and
         receipt["version"] == 1 and receipt["scope"] == COMPONENT_SCOPE, "component_receipt_schema")
    descriptor(controller_pin)
    need(receipt["controller"] == controller_pin and receipt["sources"] == value["sources"] and
         all(receipt[key] == value[key] for key in ("runtimeHelper", "retainedBaseline", "currentRuntime")), "component_selected_inputs_changed")
    need(receipt["ancestor"] == AV_VERIFICATION and isinstance(receipt["sources"], dict) and
         set(receipt["sources"]) == set(SOURCE_FILES), "component_ancestor_or_roles")
    need(isinstance(receipt["evidence"], dict) and set(receipt["evidence"]) == COMPONENT_EVIDENCE, "component_evidence_inventory")
    for pin in [receipt["ancestor"], receipt["independentReview"], receipt["retainedBaseline"], receipt["currentRuntime"],
                receipt["runtimeHelper"], *receipt["evidence"].values()]:
        descriptor(pin)
    for role, pin in receipt["sources"].items():
        need(role == "closer" or pin["sha256"] == FROZEN[role], "component_reused_source_changed")
    for pin in [controller_pin, receipt["runtimeHelper"], *receipt["sources"].values()]:
        component_read(pin, read_pin)
    return receipt


def validate_v5_component_evidence(receipt, value, controller_pin, read_pin):
    """Read the finite source/check record without authorizing a live action."""
    validate_v5_component_selection(receipt, value, controller_pin, read_pin)
    read_json = lambda pin: component_json(component_read(pin, read_pin))
    validate_native_rejection_verification(read_json(AV_VERIFICATION), read_json(REUSED_AV_VERIFICATION))
    evidence = {key: read_json(pin) for key, pin in receipt["evidence"].items()}
    review = read_json(receipt["independentReview"])
    bound = {key: receipt[key] for key in ("controller", "sources", "runtimeHelper", "retainedBaseline", "currentRuntime", "ancestor", "evidence")}
    need(isinstance(review, dict) and review.get("kind") == "audited-candidate-tv-component-review" and
         type(review.get("version")) is int and review["version"] == 1 and review.get("status") == "passed" and
         review.get("scope") == COMPONENT_SCOPE and canonical(review.get("boundInputs")) == canonical(bound) and
         review.get("testsReplayed") is False and all(type(review.get(key)) is int and review[key] == 0
             for key in ("businessHttpCalls", "sqlCalls", "serviceActions")), "component_independent_review")
    dispatch = evidence["dispatch"]
    need(dispatch.get("kind") == "tv-client-component-verification-dispatch" and type(dispatch.get("version")) is int and
         dispatch["version"] == 1 and dispatch.get("scope") == COMPONENT_SCOPE and
         all(type(dispatch.get(key)) is int and dispatch[key] == 0 for key in ("browserRuns", "businessHttpCalls", "sqlCalls", "serviceActions")),
         "component_dispatch_scope")
    sources = dispatch.get("sources")
    need(isinstance(sources, list) and all(isinstance(row, dict) and {"path", "sha256"} <= set(row) for row in sources) and
         len({row["path"] for row in sources}) == len(sources), "component_dispatch_sources")
    source_map = {row["path"]: row["sha256"] for row in sources}
    for pin in [controller_pin, receipt["runtimeHelper"], *receipt["sources"].values()]:
        need(source_map.get(pin["path"]) == pin["sha256"], "component_tested_source_changed")
    executions = dispatch.get("executions")
    need(isinstance(executions, list) and [row.get("name") for row in executions] == ["python", "javascript", "log-binding"], "component_execution_inventory")
    outputs = {}
    for row in executions:
        need(type(row.get("exitCode")) is int and row["exitCode"] == 0 and row.get("timedOut") is False and row.get("childReaped") is True,
             "component_checks_not_closed")
        argv, test_source = row.get("argv"), row.get("testSource")
        descriptor(test_source)
        need(isinstance(argv, list) and test_source["path"] in argv and source_map.get(test_source["path"]) == test_source["sha256"], "component_test_command")
        if row["name"] in ("python", "javascript"):
            pin = receipt["evidence"]["cases"]
            need(all(flag in argv and argv.count(flag) == 1 and argv.index(flag) + 1 < len(argv) and argv[argv.index(flag) + 1] == expected
                     for flag, expected in (("--reviewed-tv-baseline", pin["path"]), ("--reviewed-tv-baseline-sha256", pin["sha256"]))),
                 "component_saved_replay_command")
        outputs[row["name"]] = {key: component_read({name: row[key][name] for name in ("path", "sha256")}, read_pin) for key in ("stdout", "stderr")}
    counts = review.get("counts")
    need(isinstance(counts, dict) and set(counts) == {"python", "javascript", "log-binding", "runtime"} and
         all(type(count) is int and count > 0 for count in counts.values()), "component_review_test_inventory")
    python_output = outputs["python"]["stderr"].decode("utf-8")
    need(re.findall(r"(?m)^Ran ([0-9]+) tests in [^\n]+$", python_output) == [str(counts["python"])] and
         python_output.rstrip().endswith("\nOK") and not re.search(r"(?m)^(FAILED|OK \()", python_output), "component_python_results")
    javascript = evidence["javascriptResult"]
    need(javascript.get("kind") == "audited-candidate-client-closeout-pure-guards" and javascript.get("source") == receipt["sources"]["closer"] and
         javascript.get("sourceUnchanged") is True and type(javascript.get("testCount")) is int and
         javascript["testCount"] == javascript.get("passed") == counts["javascript"] and type(javascript.get("failed")) is int and
         javascript["failed"] == 0 and javascript.get("clientAcceptanceClaim") is False, "component_javascript_results")
    need(len(javascript.get("tests", [])) == counts["javascript"] and all(row.get("outcome") == "passed" for row in javascript["tests"]), "component_javascript_test_inventory")
    js_output = component_json(outputs["javascript"]["stdout"])
    need({key: js_output.get(key) for key in ("path", "sha256")} == receipt["evidence"]["javascriptResult"], "component_javascript_output_binding")
    log_output = outputs["log-binding"]["stdout"].decode("utf-8")
    for label, expected in (("tests", counts["log-binding"]), ("pass", counts["log-binding"]), ("fail", 0), ("skipped", 0), ("cancelled", 0)):
        need(re.findall(r"(?m)^# " + label + r" ([0-9]+)$", log_output) == [str(expected)], "component_log_results")
    cases = evidence["cases"]
    need(cases.get("kind") == "audited-candidate-reviewed-tv-baseline-replay-input" and type(cases.get("version")) is int and
         cases["version"] == 1 and cases.get("review") == value["retainedBaseline"] and cases.get("seed") == value["seedBinding"] and
         cases.get("inputBinding") == {key: value[key] for key in ("runtimeEpoch", "seedBinding")}, "component_tv_baseline_not_tested")
    replay = javascript.get("reviewedTVBaselineReplay")
    need(isinstance(replay, dict) and replay.get("review") == value["retainedBaseline"] and replay.get("tablesCompared") == 35 and
         replay.get("sequencesCompared") == 5, "component_tv_saved_js_missing")
    python_cases = [component_json(line) for line in outputs["python"]["stdout"].splitlines() if line.startswith(b'{')]
    need(any(row.get("kind") == "audited-candidate-reviewed-tv-baseline-python-replay" and row.get("reviewSha256") == value["retainedBaseline"]["sha256"] and
             row.get("rejectedTableMutations") == 35 and row.get("rejectedSequenceMutations") == 5 for row in python_cases), "component_tv_saved_python_missing")
    runtime = evidence["runtimeVerification"]
    need(runtime.get("kind") == "audited-candidate-current-runtime-verification" and runtime.get("status") == "passed" and
         runtime.get("source") == receipt["runtimeHelper"] and type(runtime.get("passed")) is int and runtime["passed"] == counts["runtime"] and
         type(runtime.get("failed")) is int and runtime["failed"] == 0 and runtime.get("environmentDecoded") is False and
         all(type(runtime.get(key)) is int and runtime[key] == 0 for key in ("sqlCalls", "httpCalls", "serviceActions")), "component_runtime_verification")
    return receipt


def bootstrap_runtime(runtime_pin, source_pin, input_pin):
    """Use the fixed protected reader to review all v5 sources before import."""
    import importlib.util
    def module(pin, raw):
        spec = importlib.util.spec_from_file_location("core_client_runtime", pin["path"])
        selected = importlib.util.module_from_spec(spec)
        exec(compile(raw, pin["path"], "exec"), selected.__dict__)
        return selected
    raw = Path(RUNTIME["path"]).read_bytes()
    need(hashlib.sha256(raw).hexdigest() == RUNTIME["sha256"], "runtime_source_changed")
    trusted = module(RUNTIME, raw)
    trusted.read_bootstrap(RUNTIME)
    trusted.read_bootstrap(source_pin)
    value = validate_input(component_json(trusted.read_bootstrap(input_pin)))
    need(value["runtimeHelper"] == runtime_pin, "input_runtime_helper_changed")
    if value["version"] != 5:
        need(runtime_pin == RUNTIME, "runtime_source_not_frozen")
        return trusted, value
    receipt = component_json(trusted.read_bootstrap(value["avVerification"]))
    validate_v5_component_evidence(receipt, value, source_pin, trusted.read_bootstrap)
    return module(runtime_pin, trusted.read_bootstrap(runtime_pin)), value


def validate_input(value):
    version = value.get("version")
    need(type(version) is int and version in (1, 2, 3, 4, 5) and set(value) == INPUT_KEYS | ({"retainedBaseline"} if version in (2, 3, 4, 5) else set()) |
         ({"currentRuntime"} if version == 5 else set()) and value["kind"] == "audited-candidate-client-run-input" and
         value["scenario"] in SCENARIOS and re.fullmatch(r"[a-z0-9][a-z0-9-]{1,55}", value["runId"]), "client_run_input_invalid")
    if version == 2:
        need(value["scenario"] == "movie" and value["retainedBaseline"] == RETAINED_BASELINE, "retained_movie_input_authority")
    if version == 3:
        need(value["retainedBaseline"] == MOVIE05_BASELINE if value["scenario"] == "movie" else value["retainedBaseline"] is None, "version3_retained_input_authority")
    if version == 4:
        need(value["scenario"] == "movie", "version4_reviewed_movie_required")
        descriptor(value["retainedBaseline"])
    if version == 5:
        need(value["scenario"] == "tv-browse", "version5_reviewed_tv_required")
        descriptor(value["retainedBaseline"])
        descriptor(value["currentRuntime"])
    for key in INPUT_KEYS - {"kind", "version", "runId", "scenario", "output", "sources", "budgets", "gatewayBudgets"}:
        descriptor(value[key])
    need(value["runtimeEpoch"] == EPOCH and value["seedBinding"] == BINDING and value["admission"] == ADMISSION and value["admissionCloseout"] == ADMISSION_CLOSEOUT and
         value["hosting"] == HOSTING and (version == 5 or (value["avVerification"] == AV_VERIFICATION and value["runtimeHelper"] == RUNTIME)),
         "current_admission_authority_changed")
    need(Path(value["output"]) == R / ("candidate-core-client-" + value["runId"]) and value["budgets"] == BUDGETS and value["gatewayBudgets"] == GATEWAY_BUDGETS,
         "client_run_scope_or_budget_invalid")
    initialization = Path(value["hostingInitialization"]["path"])
    need(initialization.is_relative_to(R) and not initialization.is_relative_to(value["output"]), "hosting_initialization_scope_invalid")
    need(value["node"] == NODE and set(value["sources"]) == set(SOURCE_FILES), "client_runtime_source_membership")
    directory = Path(value["sources"]["closer"]["path"]).parent
    for key, filename in SOURCE_FILES.items():
        descriptor(value["sources"][key])
        path = Path(value["sources"][key]["path"])
        need(path.name == filename and path.is_relative_to(R) and not path.is_relative_to(value["output"]) and
             (not filename.endswith(".mjs") or path.parent == directory) and
             ((version == 5 and key == "closer") or value["sources"][key]["sha256"] == FROZEN[key]), "client_source_path_or_revision_changed")
    return value


def validate_native_rejection_verification(verification, reused_verification):
    """Bind fresh diagnostics and subtitle checks without replaying old suites."""
    old = reused_verification
    need(isinstance(old, dict) and old.get("kind") == "tv-parent-candidate-client-component-verification" and
         type(old.get("version")) is int and old["version"] == 1 and old.get("passed") is True and
         old.get("sourceUnchanged") is True and old.get("browserStarted") is False and old.get("businessHttp") is False,
         "reused_component_verification_invalid")
    old_counts = {"adapterCounts": {"tests": 55, "pass": 55, "fail": 0, "skipped": 0}, "adapterSavedReplayChecks": 7,
        "closerCounts": {"testCount": 32, "passed": 32, "failed": 0, "sourceUnchanged": True},
        "version3Counts": {"tests": 12, "pass": 12, "fail": 0, "skipped": 0},
        "savedMovie05ReplayCounts": {"testCount": 13, "passed": 13, "failed": 0, "sourceUnchanged": True},
        "movieCounts": {"tests": 19, "pass": 19, "fail": 0, "skipped": 0}}
    need(all(canonical(old.get(key)) == canonical(expected) for key, expected in old_counts.items()),
         "reused_component_counts_changed")
    old_pins = old.get("sourcePins")
    need(isinstance(old_pins, dict) and all(old_pins.get(SOURCE_FILES[key]) == checksum for key, checksum in REUSED_AV_SOURCES.items()),
         "reused_component_sources_changed")
    need(isinstance(verification, dict) and verification.get("kind") == "candidate-native-rejection-component-verification" and
         type(verification.get("version")) is int and verification["version"] == 1 and verification.get("passed") is True and
         verification.get("sourceUnchanged") is True and verification.get("browserStarted") is True and
         verification.get("businessHttp") is False and type(verification.get("syntheticBrowserRuns")) is int and
         verification["syntheticBrowserRuns"] == 1 and type(verification.get("originalClientRuns")) is int and
         verification["originalClientRuns"] == 0 and isinstance(verification.get("verificationWorker"), dict) and
         verification["verificationWorker"].get("closed") is True, "native_component_verification_scope")
    need(verification.get("freshComponents") == ["adapter", "subtitles"] and
         canonical(verification.get("reusedComponents")) == canonical(REUSED_AV_VERIFICATION) and
         verification.get("reusedChecks") == ["closer", "version3", "savedMovie05", "movie"], "native_component_reuse_scope")
    need(canonical(verification.get("adapterCounts")) == canonical(old_counts["adapterCounts"]) and
         type(verification.get("adapterSavedReplayChecks")) is int and verification["adapterSavedReplayChecks"] == 7,
         "native_component_adapter_checks_incomplete")
    native, subtitle = verification.get("nativeCounts"), verification.get("subtitleCounts")
    need(isinstance(native, dict) and set(native) == {"testCount", "passed", "failed", "sourceUnchanged"} and
         all(type(native[key]) is int for key in ("testCount", "passed", "failed")) and native["testCount"] > 0 and
         native["passed"] == native["testCount"] and native["failed"] == 0 and native["sourceUnchanged"] is True,
         "native_component_diagnostic_checks_incomplete")
    need(isinstance(subtitle, dict) and set(subtitle) == {"tests", "pass", "fail", "skipped"} and
         all(type(number) is int for number in subtitle.values()) and subtitle["tests"] > 0 and
         subtitle["pass"] == subtitle["tests"] and subtitle["fail"] == 0 and subtitle["skipped"] == 0,
         "native_component_subtitle_checks_incomplete")
    pins = verification.get("sourcePins")
    need(isinstance(pins, dict) and all(pins.get(filename) == FROZEN[key] for key, filename in SOURCE_FILES.items()),
         "native_component_sources_changed")
    need(all((FROZEN[key] != checksum if key in ("adapter", "subtitles") else FROZEN[key] == checksum)
             for key, checksum in REUSED_AV_SOURCES.items()), "native_component_fresh_sources_changed")


def admitted(report, value, epoch, *, reused_admission04=None, product_input=None):
    if epoch.get("version") == 3:
        need(type(epoch["version"]) is int and epoch.get("operationKind") == "binary_successor" and isinstance(report, dict) and
             report.get("kind") == "audited-candidate-live-admission" and type(report.get("version")) is int and report["version"] == 3 and
             report.get("admissionKind") == "affected_tv_parent" and report.get("status") == "admitted_for_core_client" and
             report.get("candidateAdmissionComplete") is True and "failure" in report and report["failure"] is None and report.get("cleanupFailures") == [] and
             canonical(report.get("runtimeEpoch")) == canonical(value["runtimeEpoch"]) and
             canonical(report.get("seedRuntimeBinding")) == canonical(value["seedBinding"]) and
             canonical(report.get("currentSource")) == canonical(epoch["currentSource"]), "successful_current_admission_required")
        fresh = report.get("freshChecks")
        need(isinstance(fresh, dict) and set(fresh) == {"runtimeIdentity", "tvDefaultParents", "tvDetailParents", "ordinaryAuthorization",
             "healthWindow60Seconds", "sourceAndInactivePreserved", "sessionCleanup"} and all(check is True for check in fresh.values()) and
             canonical(report.get("transitionCloseout")) == canonical(AFFECTED_TV_PARENT_CLOSEOUT), "affected_tv_parent_admission_contract")
        old = reused_admission04
        need(isinstance(old, dict) and old.get("kind") == "audited-candidate-live-admission" and type(old.get("version")) is int and old["version"] == 2 and
             old.get("status") == "admitted_for_core_client" and old.get("candidateAdmissionComplete") is True and "failure" in old and old["failure"] is None and
             old.get("cleanupFailures") == [] and canonical(old.get("runtimeEpoch")) == canonical(HOST_STARTUP_HISTORY["runtimeEpoch"]) == canonical(epoch.get("previousEpoch")) and
             canonical(old.get("seedRuntimeBinding")) == canonical(HOST_STARTUP_HISTORY["seedBinding"]) and
             isinstance(product_input, dict) and product_input.get("kind") == "audited-candidate-transition-input" and
             type(product_input.get("version")) is int and product_input["version"] == 2 and
             canonical(epoch["productInput"]) == canonical(epoch["transitionInput"]) and
             canonical(product_input.get("previousEpoch")) == canonical(old["runtimeEpoch"]) and
             canonical(product_input.get("previousBinding")) == canonical(old["seedRuntimeBinding"]), "affected_tv_parent_admission_reuse")
        source = old.get("currentSource")
        need(isinstance(source, dict) and set(source) == {"archiveSha256", "sourceManifest", "binary", "fullReport", "schema"} and
             isinstance(source["archiveSha256"], str) and re.fullmatch(r"[0-9a-f]{64}", source["archiveSha256"]) and
             type(source["schema"]) is int and source["schema"] == 28, "affected_tv_parent_admission_reuse_source")
        for key in ("sourceManifest", "binary", "fullReport"):
            descriptor(source[key])
        need(canonical(report.get("reusedAdmission04")) == canonical({"report": AFFECTED_TV_PARENT_ADMISSION04,
             "runtimeEpoch": old["runtimeEpoch"], "seedRuntimeBinding": old["seedRuntimeBinding"], "currentSource": source,
             "contracts": ["native_authentication_and_query_carriers", "storage_and_library_access", "backup_create_download", "restore_ready_cancel_retained_stage"]}),
             "affected_tv_parent_admission_reuse")
        return
    need(report["kind"] == "audited-candidate-live-admission" and report["version"] == 2 and report["status"] == "admitted_for_core_client" and
         report["candidateAdmissionComplete"] is True and report["failure"] is None and report["cleanupFailures"] == [] and
         report["runtimeEpoch"] == value["runtimeEpoch"] and report["seedRuntimeBinding"] == value["seedBinding"] and report["currentSource"] == epoch["currentSource"], "successful_current_admission_required")


def hosting_initialized(report, value, hosting, epoch, read_descriptor, read_bytes, tables, *, lineage=None):
    """Bind historical startup to the fixed parent of the validated successor."""
    if epoch.get("version") != 3:
        return _hosting_initialized_receipt(report, value, hosting, epoch, read_descriptor, read_bytes, tables, FROZEN)
    need(type(epoch["version"]) is int and epoch.get("operationKind") == "binary_successor" and
         canonical(epoch.get("previousEpoch")) == canonical(HOST_STARTUP_HISTORY["runtimeEpoch"]) and
         canonical(value["hostingInitialization"]) == canonical(HOST_STARTUP_HISTORY["hostingInitialization"]) and
         canonical(value["runtimeHelper"]) == canonical(epoch["runtimeHelper"]), "hosting_initialization_successor_authority_changed")
    need(isinstance(lineage, dict) and set(lineage) == {"productEpoch", "productInput", "configurationInput"} and
         canonical(lineage["productEpoch"]) == canonical(epoch), "hosting_initialization_lineage_required")
    historical_epoch = read_descriptor(HOST_STARTUP_HISTORY["runtimeEpoch"])
    historical_binding = read_descriptor(HOST_STARTUP_HISTORY["seedBinding"])
    product, configuration = lineage["productInput"], lineage["configurationInput"]
    need(canonical(report) == canonical(read_descriptor(HOST_STARTUP_HISTORY["hostingInitialization"])) and
         historical_epoch.get("kind") == "audited-candidate-runtime-epoch" and historical_epoch.get("version") == 2 and
         historical_epoch.get("operationKind") == "environment_revision" and canonical(historical_epoch["runtimeHelper"]) == canonical(HOST_STARTUP_HISTORY["runtimeHelper"]) and
         historical_binding.get("kind") == "audited-candidate-seed-runtime-binding" and historical_binding.get("version") == 2 and
         canonical(historical_binding["runtimeEpoch"]) == canonical(HOST_STARTUP_HISTORY["runtimeEpoch"]), "hosting_initialization_history_changed")
    need(isinstance(product, dict) and canonical(product) == canonical(read_descriptor(epoch["transitionInput"])) and
         product.get("kind") == "audited-candidate-transition-input" and type(product.get("version")) is int and product["version"] == 2 and
         canonical(product.get("previousEpoch")) == canonical(HOST_STARTUP_HISTORY["runtimeEpoch"]) and canonical(product.get("previousBinding")) == canonical(HOST_STARTUP_HISTORY["seedBinding"]) and
         canonical(epoch["productInput"]) == canonical(epoch["transitionInput"]) and canonical(epoch["configurationInput"]) == canonical(historical_epoch["transitionInput"]) and
         isinstance(configuration, dict) and canonical(configuration) == canonical(read_descriptor(historical_epoch["transitionInput"])) and
         configuration.get("kind") == "audited-candidate-environment-revision-input" and type(configuration.get("version")) is int and configuration["version"] == 1 and
         canonical(configuration.get("runtimeHelper")) == canonical(HOST_STARTUP_HISTORY["runtimeHelper"]) and
         canonical(configuration.get("previousEpoch")) == canonical(historical_epoch["previousEpoch"]) and
         canonical(configuration.get("previousSeedBinding")) == canonical(historical_binding["previousBinding"]), "hosting_initialization_history_lineage_changed")
    historical_hosting = read_descriptor(report["hosting"])
    need(canonical(_hosting_identity(historical_hosting)) == canonical(_hosting_identity(hosting)) and
         canonical({key: historical_hosting[key] for key in ("serverId", "version", "serverName")}) ==
         canonical({key: hosting[key] for key in ("serverId", "version", "serverName")}), "hosting_initialization_host_changed")
    historical_value = {**HOST_STARTUP_HISTORY, "hosting": report["hosting"]}
    return _hosting_initialized_receipt(report, historical_value, historical_hosting, historical_epoch,
                                        read_descriptor, read_bytes, tables, HOST_STARTUP_TRANSPORT)


def _hosting_identity(hosting):
    return {"process": hosting["process"], "listener": hosting["listener"], "unit": {key: hosting["unitProperties"][key] for key in HOST_UNIT_FIELDS},
            "packageSha256": hosting["packageSha256"], "executableSha256": hosting["executableSha256"]}


def _hosting_initialized_receipt(report, value, hosting, epoch, read_descriptor, read_bytes, tables, transport):
    """Admit the explicit startup receipt using saved evidence only."""
    need(isinstance(report, dict) and report.get("kind") == "audited-original-client-host-startup" and type(report.get("version")) is int and
         report["version"] == 1 and report.get("status") == "ready_for_core_client" and report.get("failure") is None and
         report.get("cleanupFailures") == [] and report.get("wizardCompleted") is True, "hosting_initialization_not_complete")
    for key in ("input", "helper", "hosting", "runtimeEpoch", "seedBinding", "credentials", "sourceBefore", "sourceAfter"):
        descriptor(report[key])
    initialization = read_descriptor(report["input"])
    need(isinstance(initialization, dict) and set(initialization) == HOST_STARTUP_INPUT_KEYS and
         initialization["kind"] == "audited-original-client-host-startup-input" and type(initialization["version"]) is int and initialization["version"] == 1 and
         initialization["output"] == str(R / "client-host-startup-01") and initialization["budgets"] == HOST_STARTUP_BUDGETS and
         report["budgets"] == HOST_STARTUP_BUDGETS, "hosting_initialization_input_invalid")
    for key in HOST_STARTUP_INPUT_KEYS - {"kind", "version", "output", "budgets"}:
        descriptor(initialization[key])
    for key in ("hosting", "runtimeEpoch", "seedBinding"):
        need(report[key] == initialization[key] == value[key], "hosting_initialization_authority_changed")
    need(initialization["runtimeHelper"] == value["runtimeHelper"] == epoch["runtimeHelper"], "hosting_initialization_runtime_changed")
    for key in ("gateway", "proxy"):
        need(initialization[key]["sha256"] == transport[key], "hosting_initialization_transport_changed")
    need(Path(report["helper"]["path"]).name == "initialize-audited-client-host.py" and
         Path(report["helper"]["path"]).is_relative_to(R), "hosting_initialization_helper_invalid")
    host = _hosting_identity(hosting)
    need(report["hostingBefore"] == report["hostingAfter"] == host, "hosting_initialization_host_changed")
    candidate = {**epoch["candidateProcess"], "listener": {"host": "127.0.0.1", "port": epoch["candidate"]["listener"]["port"],
                 "socketInode": epoch["candidate"]["listener"]["socketInode"]}}
    need(report["candidateBefore"] == report["candidateAfter"] == candidate and
         report["postgresBefore"] == report["postgresAfter"] == epoch["postgresProcess"] and
         report["leaseBefore"] == report["leaseAfter"] == epoch["lease"], "hosting_initialization_goby_runtime_changed")
    before, after = [read_descriptor(report[key]) for key in ("sourceBefore", "sourceAfter")]
    need(set(before) == set(after) == {"capturedAt", "tables", "sequences"} and len(tables) == 35 and
         set(before["tables"]) == set(after["tables"]) == set(tables) and before["tables"] == after["tables"] and
         before["sequences"] == after["sequences"], "hosting_initialization_goby_state_changed")
    need(report["preservation"] == {"ownedTablesExact": 35, "sequencesExact": True, "candidateContinuous": True,
         "postgresContinuous": True, "leaseExact": True, "hostingContinuous": True}, "hosting_initialization_preservation_incomplete")
    need(report["publicIdentity"] == {"id": hosting["serverId"], "version": hosting["version"], "serverName": hosting["serverName"]} and
         report["networkConfiguration"] == HOST_NETWORK_CONFIGURATION and
         all(type(report["networkConfiguration"][key]) is type(expected) for key, expected in HOST_NETWORK_CONFIGURATION.items()), "hosting_initialization_public_identity_changed")
    users = report["users"]
    need(set(users) == {"count", "adminId", "adminName"} and type(users["count"]) is int and users["count"] == 1 and
         re.fullmatch(r"[0-9a-f]{32}", users["adminId"]) and users["adminName"] == "goby-client-host-admin-01" and
         report["libraries"] == {"count": 0}, "hosting_initialization_admin_or_libraries_changed")
    web = report["webIndex"]
    need(set(web) == {"status", "location", "bodyRead", "response"} and web["status"] == 200 and web["location"] is None and
         web["bodyRead"] is False, "hosting_initialization_index_not_ready")
    response = read_descriptor(web["response"])
    need(set(response) == {"status", "headers", "headersComplete", "bodyRead", "bodyComplete", "rawHeaders"} and
         response["status"] == 200 and response["headersComplete"] is True and response["bodyRead"] is False and response["bodyComplete"] is None,
         "hosting_initialization_index_receipt_invalid")
    descriptor(response["rawHeaders"])
    raw = read_bytes(response["rawHeaders"])
    need(isinstance(raw, bytes) and len(raw) <= 65536 and raw.endswith(b"\r\n\r\n") and raw.count(b"\r\n\r\n") == 1,
         "hosting_initialization_index_headers_incomplete")
    status_line, headers = raw.split(b"\r\n", 1)
    need(re.fullmatch(rb"HTTP/1\.[01] 200(?: [^\r\n]*)?", status_line) and
         [list(row) for row in http.client.parse_headers(io.BytesIO(headers)).items()] == response["headers"] and
         not any(name.lower() == "location" for name, unused in response["headers"]), "hosting_initialization_index_redirect")
    cleanup = report["cleanup"]
    need(isinstance(cleanup, list) and len(cleanup) == 1 and re.fullmatch(r"[0-9a-f]{64}", cleanup[0]["tokenSha256"]), "hosting_initialization_cleanup_invalid")
    for field, expected in (("logoutStatus", 204), ("sameTokenStatus", 401)):
        receipt = read_descriptor(cleanup[0][field + "Response"])
        need(cleanup[0][field] == receipt["status"] == expected and receipt["complete"] is True, "hosting_initialization_token_not_closed")
    need(report["requests"] == {"normal": 11, "cleanup": 2} and type(report["elapsedMilliseconds"]) is int and
         0 <= report["elapsedMilliseconds"] <= 300000 and report["automaticWriteRetry"] is False and
         report["servicesStartedOrStopped"] == report["gobyBusinessHttpRequests"] == 0, "hosting_initialization_scope_exceeded")


def validate_retained_movie_baseline(closeout, snapshot, binding):
    """Validate the known saved residue without authorizing any business action."""
    need(closeout.get("kind") == "audited-core-movie04-failure-closeout" and closeout.get("status") == "closed_failed_attempt_with_retained_unstarted_preparation" and
         closeout.get("runtimeEpoch") == RETAINED_MOVIE_EPOCH and closeout.get("sourceAfter") == RETAINED_SNAPSHOT and closeout.get("scenario") == "movie" and
         closeout.get("runId") == "movie-04" and closeout.get("browserAndGatewayClosed") is True and closeout.get("allElevenSessionsRevoked") is True and
         closeout.get("clientAcceptance") is False and closeout.get("playbackStarted") is False and closeout.get("clientPlaybackReferences") == closeout.get("encodingJobs") == 0,
         "retained_movie_closeout_invalid")
    need(set(snapshot) == {"capturedAt", "tables", "sequences"} and len(snapshot["tables"]) == 35, "retained_movie_snapshot_inventory")
    tables, actor, movie = snapshot["tables"], binding["actors"]["movie"], binding["catalog"]["movie"]
    users = [row for row in tables["users"] if row["id"] == actor["id"]]
    need(len(users) == 1 and users[0]["name"] == actor["username"] and users[0]["is_administrator"] is False and users[0]["is_disabled"] is False and users[0]["policy"] == {}, "retained_movie_actor_changed")
    need(len(tables["play_sessions"]) == 1 and not tables["client_playback_references"] and not tables["encoding_jobs"] and
         len(tables["sessions"]) == 11 and all(row["revoked_at"] is not None for row in tables["sessions"]), "retained_movie_row_inventory")
    play = tables["play_sessions"][0]
    auth = [row for row in tables["sessions"] if row["id"] == RETAINED_AUTH]
    need(play["id"] == RETAINED_PLAY and play["auth_session_id"] == RETAINED_AUTH and play["user_id"] == actor["id"] and play["item_id"] == movie["id"] and
         play["media_source_id"] == "mediasource_" + movie["id"] and play["duration_ticks"] == movie["runtimeTicks"] and play["application_client_id"] is None and
         play["state"] == "Prepared" and play["counted"] is False and play["started_at"] is None and play["stopped_at"] is None and play["position_ticks"] == 0 and
         len(auth) == 1 and auth[0]["kind"] == "emby" and auth[0]["user_id"] == actor["id"] and auth[0]["device_id"] == play["device_id"], "retained_movie_preparation_changed")
    data = [row for row in tables["user_item_data"] if row["user_id"] == actor["id"]]
    need(len(data) == 1 and data[0]["item_id"] == movie["id"] and data[0]["playback_position_ticks"] == data[0]["play_count"] == 0 and
         data[0]["is_favorite"] is False and data[0]["played"] is False and data[0]["last_played_at"] is None, "retained_movie_history_not_zero")
    residue = closeout.get("retainedPreparation", {})
    need(residue.get("id") == RETAINED_PLAY and residue.get("authSessionId") == RETAINED_AUTH and residue.get("itemId") == movie["id"] and
         residue.get("ownerCredentialRevoked") is True, "retained_movie_closeout_row_binding")
    return play


def validate_movie05_baseline(closeout, snapshot, binding):
    """Retain completed owned-state proof without claiming browser acceptance."""
    need(closeout.get("kind") == "audited-movie05-owned-state-closeout" and closeout.get("status") == "owned_state_closed_client_acceptance_pending" and
         closeout.get("clientAcceptance") is False and closeout.get("failure") is None and closeout.get("inputEvidence", {}).get("after") == MOVIE05_SNAPSHOT and
         closeout["inputEvidence"].get("epoch") == RETAINED_MOVIE_EPOCH and closeout.get("browserOutcome") == "failed" and closeout.get("browserExitCode") == 1 and
         closeout.get("gatewayExitCode") == 0, "movie05_closeout_invalid")
    need(set(snapshot) == {"capturedAt", "tables", "sequences"} and len(snapshot["tables"]) == 35, "movie05_snapshot_inventory")
    tables, actor, movie = snapshot["tables"], binding["actors"]["movie"], binding["catalog"]["movie"]
    users = [row for row in tables["users"] if row["id"] == actor["id"]]
    need(len(users) == 1 and users[0]["name"] == actor["username"] and users[0]["is_administrator"] is False and users[0]["is_disabled"] is False and users[0]["policy"] == {}, "movie05_actor_changed")
    auth = {row["id"]: row for row in tables["sessions"]}
    plays = {row["id"]: row for row in tables["play_sessions"]}
    need(len(tables["sessions"]) == len(auth) == 13 and all(row["revoked_at"] is not None for row in auth.values()) and
         len(tables["play_sessions"]) == len(plays) == 5 and set(plays) == set(MOVIE05_HISTORY) and not tables["client_playback_references"] and not tables["encoding_jobs"], "movie05_history_inventory")
    for identity, (state, credential, counted, position) in MOVIE05_HISTORY.items():
        row, session = plays[identity], auth.get(credential, {})
        need(row["state"] == state and row["auth_session_id"] == credential and row["counted"] is counted and type(row["position_ticks"]) is int and
             row["position_ticks"] == position and row["user_id"] == actor["id"] and row["item_id"] == movie["id"] and row["media_source_id"] == "mediasource_" + movie["id"] and
             row["duration_ticks"] == movie["runtimeTicks"] and row["application_client_id"] is None and row["client_correlated"] is False and
             session.get("kind") == "emby" and session.get("user_id") == actor["id"] and session.get("device_id") == row["device_id"] and
             (row["started_at"] is not None if counted else row["started_at"] is None) and
             (row["stopped_at"] is None if state == "Prepared" else row["stopped_at"] is not None), "movie05_history_identity")
    data = [row for row in tables["user_item_data"] if row["user_id"] == actor["id"]]
    need(len(data) == 1 and data[0]["item_id"] == movie["id"] and type(data[0]["play_count"]) is int and data[0]["play_count"] == 2 and
         type(data[0]["playback_position_ticks"]) is int and data[0]["playback_position_ticks"] == 1217878390 and data[0]["is_favorite"] is False and
         data[0]["played"] is False and data[0]["last_played_at"] is not None, "movie05_userdata_changed")
    checks = closeout.get("checks", {})
    need(checks.get("authentication", {}).get("allSessionsRevoked") == 13 and checks.get("ownedData", {}).get("ownedTables") == 35 and
         checks["ownedData"].get("oldRowsDeleted") == 0 and canonical(checks["ownedData"].get("userData")) == canonical(data[0]), "movie05_closeout_state_binding")
    return plays[MOVIE05_PREPARED]


def reviewed_timestamp_ns(value):
    need(isinstance(value, str), "reviewed_movie_timestamp_invalid")
    match = re.fullmatch(r"(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(?:\.(\d{1,9}))?(Z|[+-]\d\d:\d\d)", value)
    need(match is not None, "reviewed_movie_timestamp_invalid")
    try:
        base = datetime.fromisoformat(match[1] + match[3].replace("Z", "+00:00"))
    except ValueError as error:
        raise RunError("reviewed_movie_timestamp_invalid") from error
    delta = base - datetime(1970, 1, 1, tzinfo=timezone.utc)
    return (delta.days * 86400 + delta.seconds) * 1000000000 + int((match[2] or "").ljust(9, "0"))


def reviewed_integer(value):
    return type(value) is int and 0 <= value <= 9007199254740991


def validate_reviewed_movie_history(retained, binding):
    """Validate an explicit reviewed state without granting browser acceptance."""
    need(isinstance(retained, dict) and set(retained) == {"review", "closeout", "snapshot", "sourceEpoch", "manifest", "inputBinding"},
         "reviewed_movie_evidence_shape")
    review, closeout, snapshot, epoch, manifest, input_binding = (retained[key] for key in
        ("review", "closeout", "snapshot", "sourceEpoch", "manifest", "inputBinding"))
    need(isinstance(review, dict) and set(review) == REVIEWED_BASELINE_KEYS and review["kind"] == "audited-candidate-reviewed-movie-baseline" and
         type(review["version"]) is int and review["version"] == 1 and review["status"] == "reviewed_closed_state" and review["scenario"] == "movie",
         "reviewed_movie_review_schema")
    for key in REVIEWED_BASELINE_PINS:
        descriptor(review[key])
    need(isinstance(input_binding, dict) and set(input_binding) == {"runtimeEpoch", "seedBinding"}, "reviewed_movie_input_binding")
    for key in ("runtimeEpoch", "seedBinding"):
        descriptor(input_binding[key])
    need(canonical(input_binding) == canonical({key: review[key] for key in input_binding}) and
         canonical(binding.get("runtimeEpoch")) == canonical(review["runtimeEpoch"]), "reviewed_movie_input_binding")
    actor, movie = binding["actors"]["movie"], binding["catalog"]["movie"]
    need(isinstance(review["actor"], dict) and set(review["actor"]) == {"id", "username"} and
         all(isinstance(review["actor"][key], str) and review["actor"][key] for key in ("id", "username")) and
         re.fullmatch(r"[a-f0-9]{32}", review["actor"]["id"]) and
         canonical(review["actor"]) == canonical({key: actor[key] for key in ("id", "username")}), "reviewed_movie_actor_binding")
    need(isinstance(review["item"], dict) and set(review["item"]) == {"id", "mediaSourceId", "runtimeTicks"} and
         isinstance(review["item"]["id"], str) and re.fullmatch(r"[a-f0-9]{32}", review["item"]["id"]) and reviewed_integer(review["item"]["runtimeTicks"]) and
         review["item"]["runtimeTicks"] > 0 and canonical(review["item"]) == canonical({"id": movie["id"],
             "mediaSourceId": "mediasource_" + movie["id"], "runtimeTicks": movie["runtimeTicks"]}), "reviewed_movie_item_binding")
    need(isinstance(manifest, dict) and manifest.get("kind") == "audited-candidate-client-input" and type(manifest.get("version")) is int and
         manifest["version"] == 1 and manifest.get("scenario") == "movie" and isinstance(manifest.get("runId"), str) and
         re.fullmatch(r"movie-[0-9]{2,}", manifest["runId"]) and manifest.get("serverId") == binding.get("serverId") and
         canonical(manifest.get("actor")) == canonical(review["actor"]) and
         isinstance(manifest.get("catalog"), dict) and canonical(manifest["catalog"].get("movie")) == canonical(movie), "reviewed_movie_manifest_binding")
    need(isinstance(epoch, dict) and epoch.get("kind") == "audited-candidate-runtime-epoch" and type(epoch.get("version")) is int and
         epoch["version"] in (2, 3), "reviewed_movie_source_epoch")
    source = epoch.get("currentSource")
    need(isinstance(source, dict) and set(source) == {"archiveSha256", "sourceManifest", "binary", "fullReport", "schema"} and
         isinstance(source["archiveSha256"], str) and re.fullmatch(r"[0-9a-f]{64}", source["archiveSha256"]) and
         type(source["schema"]) is int and source["schema"] == 28, "reviewed_movie_source_identity")
    for key in ("sourceManifest", "binary", "fullReport"):
        descriptor(source[key])
    need(canonical(manifest.get("source")) == canonical({"manifestSha256": source["sourceManifest"]["sha256"],
         "binarySha256": source["binary"]["sha256"], "schema": source["schema"]}), "reviewed_movie_source_identity")
    need(isinstance(closeout, dict) and closeout.get("kind") == "audited-" + manifest["runId"].replace("-", "") + "-owned-state-closeout" and
         closeout.get("status") == "owned_state_closed_client_acceptance_pending" and closeout.get("clientAcceptance") is False and
         "failure" not in closeout and closeout.get("browserOutcome") == "failed" and type(closeout.get("browserExitCode")) is int and
         closeout["browserExitCode"] == 1 and type(closeout.get("gatewayExitCode")) is int and closeout["gatewayExitCode"] == 0,
         "reviewed_movie_closeout_invalid")
    evidence = closeout.get("inputEvidence")
    need(isinstance(evidence, dict) and all(canonical(evidence.get(key)) == canonical(review[pin]) for key, pin in
         (("after", "snapshot"), ("epoch", "sourceEpoch"), ("manifest", "manifest"))), "reviewed_movie_closeout_binding")
    checks = closeout.get("checks")
    need(isinstance(checks, dict) and all(isinstance(checks.get(key), dict) for key in ("ownedData", "authentication", "savedRuntimeAndWorkers")),
         "reviewed_movie_closeout_checks")
    data, authentication, state = (checks[key] for key in ("ownedData", "authentication", "savedRuntimeAndWorkers"))
    need(all(type(data.get(key)) is int and data[key] == expected for key, expected in
         (("ownedTables", 35), ("oldRowsDeleted", 0))) and
         all(state.get(key) is True for key in ("candidateContinuous", "postgresContinuous", "leaseExact")) and isinstance(state.get("workers"), dict) and
         all(isinstance(state["workers"].get(role), dict) and state["workers"][role].get("pidAbsentAsRecorded") is True and
             state["workers"][role].get("recursiveCgroupEmptyAsRecorded") is True for role in ("browser", "gateway")), "reviewed_movie_closeout_checks")
    need(isinstance(snapshot, dict) and set(snapshot) == {"capturedAt", "tables", "sequences"} and isinstance(snapshot["capturedAt"], str) and
         isinstance(snapshot["tables"], dict) and set(snapshot["tables"]) == REVIEWED_BASELINE_TABLES and isinstance(snapshot["sequences"], dict) and
         all(isinstance(rows, list) and all(isinstance(row, dict) for row in rows) for rows in snapshot["tables"].values()), "reviewed_movie_snapshot_inventory")
    tables = snapshot["tables"]
    need(all(type(data.get(key)) is int and data[key] == len(tables[table]) for key, table in
         (("clientPlaybackReferences", "client_playback_references"), ("encodingJobs", "encoding_jobs"))), "reviewed_movie_snapshot_inventory")
    reviewed_timestamp_ns(snapshot["capturedAt"])
    users = [row for row in tables["users"] if row.get("id") == actor["id"]]
    need(len(users) == 1 and users[0].get("name") == actor["username"] and users[0].get("is_administrator") is False and
         users[0].get("is_disabled") is False and canonical(users[0].get("policy")) == canonical({}), "reviewed_movie_actor_changed")
    need(all(isinstance(row.get("id"), str) and row["id"] and isinstance(row.get("revoked_at"), str) for row in tables["sessions"]) and
         len({row["id"] for row in tables["sessions"]}) == len(tables["sessions"]) and
         type(authentication.get("allSessionsRevoked")) is int and authentication["allSessionsRevoked"] == len(tables["sessions"]), "reviewed_movie_session_inventory")
    for row in tables["sessions"]:
        reviewed_timestamp_ns(row["revoked_at"])
    need(all(isinstance(row.get("id"), str) and row["id"] for row in tables["play_sessions"]) and
         len({row["id"] for row in tables["play_sessions"]}) == len(tables["play_sessions"]), "reviewed_movie_play_inventory")
    sessions = {row["id"]: row for row in tables["sessions"]}
    plays = [row for row in tables["play_sessions"] if row.get("user_id") == actor["id"]]
    need(len(plays) < 256, "reviewed_movie_play_inventory")
    validate_reviewed_movie_residue(tables, actor["id"])
    required = {"id", "auth_session_id", "user_id", "item_id", "media_source_id", "duration_ticks", "application_client_id", "client_correlated",
                "device_id", "state", "counted", "position_ticks", "started_at", "stopped_at", "expires_at"}
    for row in plays:
        need(required <= set(row), "reviewed_movie_play_identity")
        session = sessions.get(row["auth_session_id"], {})
        need(row["item_id"] == movie["id"] and row["media_source_id"] == review["item"]["mediaSourceId"] and type(row["duration_ticks"]) is int and
             row["duration_ticks"] == movie["runtimeTicks"] and row["application_client_id"] is None and row["client_correlated"] is False and
             session.get("kind") == "emby" and session.get("user_id") == actor["id"] and session.get("device_id") == row["device_id"] and
             reviewed_integer(row["position_ticks"]) and row["position_ticks"] <= row["duration_ticks"], "reviewed_movie_play_identity")
        need(row["state"] == "Stopped" and row["counted"] is True and isinstance(row["started_at"], str) and isinstance(row["stopped_at"], str) or
             row["state"] in ("Prepared", "Expired") and row["counted"] is False and row["started_at"] is None and
             (row["stopped_at"] is None if row["state"] == "Prepared" else isinstance(row["stopped_at"], str)), "reviewed_movie_play_state")
        reviewed_timestamp_ns(row["expires_at"])
    expirations = review["preparedExpirations"]
    need(isinstance(expirations, list) and len(expirations) < 256 and all(isinstance(row, dict) and set(row) == {"playSessionId", "authSessionId"} and
         all(isinstance(row[key], str) and row[key] for key in ("playSessionId", "authSessionId")) for row in expirations) and
         len({row["playSessionId"] for row in expirations}) == len(expirations), "reviewed_movie_expiration_allowlist")
    need(all(re.fullmatch(r"play_[A-Za-z0-9_]+", row["playSessionId"]) and re.fullmatch(r"[A-Za-z0-9_-]+", row["authSessionId"])
             for row in expirations), "reviewed_movie_expiration_allowlist")
    prepared = {row["id"]: row["auth_session_id"] for row in plays if row["state"] == "Prepared"}
    need(canonical({row["playSessionId"]: row["authSessionId"] for row in expirations}) == canonical(prepared), "reviewed_movie_expiration_allowlist")
    userdata = [row for row in tables["user_item_data"] if row.get("user_id") == actor["id"]]
    need(len(userdata) == 1 and userdata[0].get("item_id") == movie["id"] and canonical(userdata[0]) == canonical(data.get("userData")) and
         reviewed_integer(userdata[0].get("play_count")) and reviewed_integer(userdata[0].get("playback_position_ticks")) and
         type(userdata[0].get("is_favorite")) is bool and type(userdata[0].get("played")) is bool,
         "reviewed_movie_userdata_changed")
    return [row for row in plays if row["state"] == "Prepared"]


def validate_reviewed_actor_residue(tables, actor, code):
    sessions = {row["id"] for row in tables["sessions"] if row.get("user_id") == actor}
    plays = {row["id"] for row in tables["play_sessions"] if row.get("user_id") == actor}
    need(not any(row.get("user_id") == actor or row.get("auth_session_id") in sessions or row.get("play_session_id") in plays
         for table in ("client_playback_references", "encoding_jobs") for row in tables[table]), code)


def validate_reviewed_movie_residue(tables, actor):
    validate_reviewed_actor_residue(tables, actor, "reviewed_movie_owned_residue")


def validate_reviewed_subtitles_closed_state(retained, binding):
    review, closeout, snapshot, epoch, manifest, boundary = (retained[key] for key in
        ("review", "closeout", "snapshot", "sourceEpoch", "manifest", "boundary"))
    need(isinstance(closeout, dict) and closeout.get("kind") == "audited-candidate-subtitles-owned-state-closeout" and
         closeout.get("status") == "owned_state_closed_client_acceptance_pending" and closeout.get("clientAcceptance") is False and
         "checks" not in closeout and "failure" not in closeout and closeout.get("sourcePinsUnchanged") is True and
         isinstance(closeout.get("originalUIRejection"), dict) and closeout["originalUIRejection"].get("originalObservationUnchanged") is True,
         "reviewed_movie_current_closeout")
    evidence = closeout.get("inputEvidence")
    pins = {"manifest", "observation", "summary", "gatewayAttestation", "gatewayIndex", "runtimeEpoch", "admission", "seedBinding", "sourceBefore", "sourceAfter", "boundary", "serverLog"}
    need(isinstance(evidence, dict) and set(evidence) == pins | {"kind", "version", "sources", "output", "retainedBaseline"} and
         evidence["kind"] == "audited-candidate-client-closeout-input" and type(evidence["version"]) is int and evidence["version"] == 3 and
         evidence["retainedBaseline"] is None and isinstance(evidence["sources"], dict) and set(evidence["sources"]) == set(SOURCE_FILES) and
         isinstance(evidence["output"], str) and evidence["output"].startswith("/opt/goby-test/"), "reviewed_movie_current_input")
    for key in pins:
        descriptor(evidence[key])
    for pin in evidence["sources"].values():
        descriptor(pin)
    need(all(canonical(evidence[key]) == canonical(review[pin]) for key, pin in
         (("sourceAfter", "snapshot"), ("runtimeEpoch", "sourceEpoch"), ("manifest", "manifest"), ("seedBinding", "seedBinding"), ("boundary", "boundary"))),
         "reviewed_movie_current_input_binding")
    need(isinstance(epoch, dict) and epoch.get("kind") == "audited-candidate-runtime-epoch" and type(epoch.get("version")) is int and
         epoch["version"] in (2, 3), "reviewed_movie_source_epoch")
    source = epoch.get("currentSource")
    need(isinstance(source, dict) and set(source) == {"archiveSha256", "sourceManifest", "binary", "fullReport", "schema"} and
         isinstance(source["archiveSha256"], str) and re.fullmatch(r"[0-9a-f]{64}", source["archiveSha256"]) and
         type(source["schema"]) is int and source["schema"] == 28, "reviewed_movie_source_identity")
    for key in ("sourceManifest", "binary", "fullReport"):
        descriptor(source[key])
    actor = binding["actors"]["subtitles"]
    need(isinstance(manifest, dict) and manifest.get("kind") == "audited-candidate-client-input" and type(manifest.get("version")) is int and
         manifest["version"] == 1 and manifest.get("scenario") == "subtitles" and isinstance(manifest.get("runId"), str) and
         re.fullmatch(r"subtitles-[0-9]{2,}", manifest["runId"]) and manifest.get("serverId") == binding.get("serverId") and
         canonical(manifest.get("actor")) == canonical({key: actor[key] for key in ("id", "username")}) and
         isinstance(manifest.get("catalog"), dict) and
         canonical(manifest.get("source")) == canonical({"manifestSha256": source["sourceManifest"]["sha256"],
             "binarySha256": source["binary"]["sha256"], "schema": source["schema"]}), "reviewed_movie_current_manifest")
    need(isinstance(snapshot, dict) and set(snapshot) == {"capturedAt", "tables", "sequences"} and isinstance(snapshot["tables"], dict) and
         set(snapshot["tables"]) == REVIEWED_BASELINE_TABLES and isinstance(snapshot["sequences"], dict) and
         all(isinstance(rows, list) and all(isinstance(row, dict) for row in rows) for rows in snapshot["tables"].values()), "reviewed_movie_snapshot_inventory")
    reviewed_timestamp_ns(snapshot["capturedAt"])
    tables, state = snapshot["tables"], closeout.get("sourceState")
    counts = {"afterSessions": "sessions", "afterPlays": "play_sessions", "afterUserData": "user_item_data",
              "afterClientReferences": "client_playback_references", "afterEncodingJobs": "encoding_jobs"}
    need(isinstance(state, dict) and state.get("allSessionsRevoked") is True and
         all(type(state.get(key)) is int and state[key] == len(tables[table]) for key, table in counts.items()), "reviewed_movie_current_counts")
    for table in ("sessions", "play_sessions"):
        need(all(isinstance(row.get("id"), str) and row["id"] for row in tables[table]) and
             len({row["id"] for row in tables[table]}) == len(tables[table]), "reviewed_movie_current_row_inventory")
    for session in tables["sessions"]:
        reviewed_timestamp_ns(session.get("revoked_at"))
    boundary_keys = {"afterMonotonicNs", "beforeMonotonicNs", "candidateAfter", "candidateBefore", "clientWorker", "database", "gatewayWorker", "kind",
                     "leaseAfter", "leaseBefore", "postgresAfter", "postgresBefore", "runId", "runtimeEpoch", "sourceAfter", "sourceBefore", "version"}
    need(isinstance(boundary, dict) and set(boundary) == boundary_keys and boundary["kind"] == "audited-candidate-client-boundary" and
         type(boundary["version"]) is int and boundary["version"] == 1 and boundary["runId"] == manifest["runId"] and
         canonical(boundary["runtimeEpoch"]) == canonical(review["sourceEpoch"]) and canonical(boundary["sourceAfter"]) == canonical(review["snapshot"]) and
         canonical(boundary["sourceBefore"]) == canonical(evidence["sourceBefore"]), "reviewed_movie_current_boundary")
    candidate = manifest.get("processes", {}).get("candidate")
    need(isinstance(candidate, dict) and "listener" in candidate and isinstance(epoch.get("candidateProcess"), dict) and
         isinstance(epoch.get("postgresProcess"), dict) and isinstance(epoch.get("lease"), dict) and
         isinstance(epoch.get("candidate"), dict) and isinstance(epoch["candidate"].get("database"), str) and epoch["candidate"]["database"] and
         canonical(boundary["candidateBefore"]) == canonical(boundary["candidateAfter"]) == canonical(candidate) and
         canonical({key: value for key, value in candidate.items() if key != "listener"}) == canonical(epoch.get("candidateProcess")) and
         canonical(boundary["postgresBefore"]) == canonical(boundary["postgresAfter"]) == canonical(epoch.get("postgresProcess")) and
         canonical(boundary["leaseBefore"]) == canonical(boundary["leaseAfter"]) == canonical(epoch.get("lease")) and
         boundary["database"] == epoch.get("candidate", {}).get("database"), "reviewed_movie_current_runtime")
    need(canonical(boundary["clientWorker"]) == canonical({"exitCode": 0, "mainPID": 0, "remainingBrowserPids": [], "workerPidAbsent": True}) and
         canonical(boundary["gatewayWorker"]) == canonical({"exitCode": 0, "mainPID": 0, "workerPidAbsent": True, "index": evidence["gatewayIndex"]}),
         "reviewed_movie_current_workers")
    need(all(isinstance(boundary[key], str) and re.fullmatch(r"[0-9]+", boundary[key]) for key in ("beforeMonotonicNs", "afterMonotonicNs")) and
         int(boundary["beforeMonotonicNs"]) <= int(boundary["afterMonotonicNs"]), "reviewed_movie_current_boundary_time")


def validate_reviewed_subtitles_state(retained, binding):
    validate_reviewed_subtitles_closed_state(retained, binding)
    need(canonical(retained["manifest"]["catalog"].get("movie")) == canonical(binding["catalog"]["movie"]), "reviewed_movie_current_manifest")
    validate_reviewed_movie_residue(retained["snapshot"]["tables"], binding["actors"]["movie"]["id"])


def validate_reviewed_movie_baseline(retained, binding):
    """Keep the latest database state separate from the movie actor's history."""
    need(isinstance(retained, dict) and set(retained) == {"review", "closeout", "snapshot", "sourceEpoch", "manifest", "inputBinding", "movieProvenance", "boundary"},
         "reviewed_movie_evidence_shape")
    review, provenance = retained["review"], retained["movieProvenance"]
    need(isinstance(review, dict) and set(review) == REVIEWED_BASELINE_KEYS and isinstance(review["movieProvenance"], dict) and
         set(review["movieProvenance"]) == {"closeout", "snapshot", "sourceEpoch", "manifest"} and isinstance(provenance, dict) and
         set(provenance) == set(review["movieProvenance"]), "reviewed_movie_provenance_shape")
    for pin in review["movieProvenance"].values():
        descriptor(pin)
    if review["boundary"] is not None:
        descriptor(review["boundary"])
    movie_history = {**provenance, "review": {**review, **review["movieProvenance"]}, "inputBinding": retained["inputBinding"]}
    prepared = validate_reviewed_movie_history(movie_history, binding)
    need(isinstance(retained["manifest"], dict), "reviewed_movie_current_manifest")
    if retained["manifest"].get("scenario") == "movie":
        need(review["boundary"] is None and retained["boundary"] is None, "reviewed_movie_boundary_authority")
        validate_reviewed_movie_history({key: retained[key] for key in ("review", "closeout", "snapshot", "sourceEpoch", "manifest", "inputBinding")}, binding)
    else:
        need(review["boundary"] is not None and retained["boundary"] is not None, "reviewed_movie_boundary_authority")
        for key in REVIEWED_BASELINE_PINS:
            descriptor(review[key])
        validate_reviewed_subtitles_state(retained, binding)
    actor = binding["actors"]["movie"]["id"]
    for table, key in (("users", "id"), ("sessions", "user_id"), ("play_sessions", "user_id"), ("user_item_data", "user_id")):
        current = [row for row in retained["snapshot"]["tables"][table] if row.get(key) == actor]
        historic = [row for row in provenance["snapshot"]["tables"][table] if row.get(key) == actor]
        need(canonical(current) == canonical(historic), "reviewed_movie_history_changed")
    need(reviewed_timestamp_ns(retained["snapshot"]["capturedAt"]) >= reviewed_timestamp_ns(provenance["snapshot"]["capturedAt"]),
         "reviewed_movie_history_time")
    return prepared


def load_reviewed_movie_baseline(review_pin, input_binding, binding, read_descriptor):
    """Load only the explicitly pinned review and its closed-state evidence."""
    descriptor(review_pin)
    review = read_descriptor(review_pin)
    need(isinstance(review, dict) and set(review) == REVIEWED_BASELINE_KEYS, "reviewed_movie_review_schema")
    for key in REVIEWED_BASELINE_PINS:
        descriptor(review[key])
    need(isinstance(review["movieProvenance"], dict) and set(review["movieProvenance"]) == {"closeout", "snapshot", "sourceEpoch", "manifest"},
         "reviewed_movie_provenance_shape")
    for pin in review["movieProvenance"].values():
        descriptor(pin)
    if review["boundary"] is not None:
        descriptor(review["boundary"])
    retained = {"review": review, **{key: read_descriptor(review[key]) for key in ("closeout", "snapshot", "sourceEpoch", "manifest")},
                "inputBinding": input_binding,
                "movieProvenance": {key: read_descriptor(pin) for key, pin in review["movieProvenance"].items()},
                "boundary": None if review["boundary"] is None else read_descriptor(review["boundary"])}
    validate_reviewed_movie_baseline(retained, binding)
    return retained


def validate_reviewed_tv_baseline(retained, binding):
    """Bind one consumed TV browse actor without authorizing playback."""
    need(isinstance(retained, dict) and set(retained) == {"review", "closeout", "snapshot", "sourceEpoch", "manifest", "inputBinding", "tvProvenance", "boundary", "postBrowseAdmission"},
         "reviewed_tv_evidence_shape")
    review, provenance, input_binding = retained["review"], retained["tvProvenance"], retained["inputBinding"]
    need(isinstance(review, dict) and set(review) == REVIEWED_TV_BASELINE_KEYS and review["kind"] == "audited-candidate-reviewed-tv-baseline" and
         type(review["version"]) is int and review["version"] == 1 and review["status"] == "reviewed_closed_state" and review["scenario"] == "tv-browse",
         "reviewed_tv_review_schema")
    for key in REVIEWED_BASELINE_PINS | {"boundary"}:
        descriptor(review[key])
    need(isinstance(provenance, dict) and set(provenance) == {"closeout", "snapshot", "sourceEpoch", "manifest"} and
         isinstance(review["tvProvenance"], dict) and set(review["tvProvenance"]) == set(provenance), "reviewed_tv_provenance_shape")
    for pin in review["tvProvenance"].values():
        descriptor(pin)
    need(review["postBrowseAdmission"] == ADMISSION, "reviewed_tv_post_browse_authority")
    need(isinstance(input_binding, dict) and set(input_binding) == {"runtimeEpoch", "seedBinding"}, "reviewed_tv_input_binding")
    for pin in input_binding.values():
        descriptor(pin)
    need(canonical(input_binding) == canonical({key: review[key] for key in input_binding}) and
         canonical(binding.get("runtimeEpoch")) == canonical(review["runtimeEpoch"]), "reviewed_tv_input_binding")
    actor = binding["actors"]["tv-browse"]
    need(isinstance(review["actor"], dict) and set(review["actor"]) == {"id", "username"} and
         isinstance(review["actor"]["id"], str) and re.fullmatch(r"[a-f0-9]{32}", review["actor"]["id"]) and
         isinstance(review["actor"]["username"], str) and review["actor"]["username"] and
         canonical(review["actor"]) == canonical({key: actor[key] for key in ("id", "username")}), "reviewed_tv_actor_binding")
    episodes = binding["catalog"].get("episodes")
    need(isinstance(episodes, list) and all(isinstance(row, dict) for row in episodes), "reviewed_tv_catalog_identity")
    targets = [row for row in episodes if row.get("type") == "Episode" and type(row.get("indexNumber")) is int and row["indexNumber"] == 1 and
               type(row.get("parentIndexNumber")) is int and row["parentIndexNumber"] == 2]
    need(len(targets) == 1, "reviewed_tv_detail_target")
    item = targets[0]
    need(isinstance(item.get("id"), str) and re.fullmatch(r"[a-f0-9]{32}", item["id"]) and
         reviewed_integer(item.get("runtimeTicks")) and item["runtimeTicks"] > 0, "reviewed_tv_detail_target")
    need(isinstance(review["item"], dict) and set(review["item"]) == {"id", "mediaSourceId", "runtimeTicks"} and
         isinstance(review["item"]["id"], str) and re.fullmatch(r"[a-f0-9]{32}", review["item"]["id"]) and
         reviewed_integer(review["item"]["runtimeTicks"]) and review["item"]["runtimeTicks"] > 0 and
         canonical(review["item"]) == canonical({"id": item["id"], "mediaSourceId": "mediasource_" + item["id"], "runtimeTicks": item["runtimeTicks"]}),
         "reviewed_tv_item_binding")
    expirations = review["preparedExpirations"]
    need(isinstance(expirations, list) and len(expirations) == 1 and isinstance(expirations[0], dict) and set(expirations[0]) == {"playSessionId", "authSessionId"} and
         isinstance(expirations[0]["playSessionId"], str) and re.fullmatch(r"play_[A-Za-z0-9_]+", expirations[0]["playSessionId"]) and
         isinstance(expirations[0]["authSessionId"], str) and re.fullmatch(r"[A-Za-z0-9_-]+", expirations[0]["authSessionId"]), "reviewed_tv_prepared_authority")
    validate_reviewed_subtitles_closed_state(retained, binding)
    need(all(key in binding["catalog"] and canonical(retained["manifest"]["catalog"].get(key)) == canonical(binding["catalog"][key])
             for key in REVIEWED_TV_CATALOG_KEYS), "reviewed_tv_current_catalog")
    validate_reviewed_actor_residue(retained["snapshot"]["tables"], actor["id"], "reviewed_tv_owned_residue")
    closeout, prior, epoch, manifest = (provenance[key] for key in ("closeout", "snapshot", "sourceEpoch", "manifest"))
    need(isinstance(epoch, dict) and epoch.get("kind") == "audited-candidate-runtime-epoch" and type(epoch.get("version")) is int and
         epoch["version"] in (2, 3), "reviewed_tv_source_epoch")
    source = epoch.get("currentSource")
    need(isinstance(source, dict) and set(source) == {"archiveSha256", "sourceManifest", "binary", "fullReport", "schema"} and
         isinstance(source["archiveSha256"], str) and re.fullmatch(r"[0-9a-f]{64}", source["archiveSha256"]) and
         type(source["schema"]) is int and source["schema"] == 28, "reviewed_tv_source_identity")
    for key in ("sourceManifest", "binary", "fullReport"):
        descriptor(source[key])
    need(isinstance(manifest, dict) and manifest.get("kind") == "audited-candidate-client-input" and type(manifest.get("version")) is int and
         manifest["version"] == 1 and manifest.get("scenario") == "tv-browse" and isinstance(manifest.get("runId"), str) and
         re.fullmatch(r"tv-browse-[0-9]{2,}", manifest["runId"]) and manifest.get("serverId") == binding.get("serverId") and
         canonical(manifest.get("actor")) == canonical(review["actor"]) and isinstance(manifest.get("catalog"), dict) and
         all(canonical(manifest["catalog"].get(key)) == canonical(binding["catalog"][key]) for key in REVIEWED_TV_CATALOG_KEYS) and
         canonical(manifest.get("source")) == canonical({"manifestSha256": source["sourceManifest"]["sha256"],
             "binarySha256": source["binary"]["sha256"], "schema": source["schema"]}), "reviewed_tv_manifest_binding")
    need(isinstance(closeout, dict) and closeout.get("kind") == "audited-tv-browse" + manifest["runId"].removeprefix("tv-browse-") + "-owned-state-closeout" and
         closeout.get("status") == "owned_state_closed_client_acceptance_pending" and closeout.get("clientAcceptance") is False and "failure" not in closeout and
         closeout.get("browserOutcome") == "failed" and type(closeout.get("browserExitCode")) is int and closeout["browserExitCode"] == 1 and
         type(closeout.get("gatewayExitCode")) is int and closeout["gatewayExitCode"] == 0 and
         all(type(closeout.get(key)) is int and closeout[key] == 0 for key in ("physicalMediaRequests", "playingReports")), "reviewed_tv_closeout_invalid")
    evidence = closeout.get("inputEvidence")
    need(isinstance(evidence, dict) and all(canonical(evidence.get(key)) == canonical(review["tvProvenance"][pin]) for key, pin in
         (("after", "snapshot"), ("epoch", "sourceEpoch"), ("manifest", "manifest"))), "reviewed_tv_closeout_binding")
    checks = closeout.get("checks")
    need(isinstance(checks, dict) and all(isinstance(checks.get(key), dict) for key in ("ownedData", "authentication", "savedRuntimeAndWorkers")),
         "reviewed_tv_closeout_checks")
    data, authentication, state = (checks[key] for key in ("ownedData", "authentication", "savedRuntimeAndWorkers"))
    need(all(type(data.get(key)) is int and data[key] == expected for key, expected in (("ownedTables", 35), ("oldRowsDeleted", 0), ("newCountedPlays", 0))) and
         all(state.get(key) is True for key in ("candidateContinuous", "postgresContinuous", "leaseExact")) and isinstance(state.get("workers"), dict) and
         all(isinstance(state["workers"].get(role), dict) and state["workers"][role].get("pidAbsentAsRecorded") is True and
             state["workers"][role].get("recursiveCgroupEmptyAsRecorded") is True for role in ("browser", "gateway")), "reviewed_tv_closeout_checks")
    need(isinstance(prior, dict) and set(prior) == {"capturedAt", "tables", "sequences"} and isinstance(prior["tables"], dict) and
         set(prior["tables"]) == REVIEWED_BASELINE_TABLES and isinstance(prior["sequences"], dict) and
         all(isinstance(rows, list) and all(isinstance(row, dict) for row in rows) for rows in prior["tables"].values()), "reviewed_tv_snapshot_inventory")
    reviewed_timestamp_ns(prior["capturedAt"])
    tables = prior["tables"]
    for table in ("sessions", "play_sessions"):
        need(all(isinstance(row.get("id"), str) and row["id"] for row in tables[table]) and
             len({row["id"] for row in tables[table]}) == len(tables[table]), "reviewed_tv_row_inventory")
    for session in tables["sessions"]:
        reviewed_timestamp_ns(session.get("revoked_at"))
    need(type(authentication.get("allSessionsRevoked")) is int and authentication["allSessionsRevoked"] == len(tables["sessions"]) and
         all(type(data.get(key)) is int and data[key] == len(tables[table]) for key, table in
             (("clientPlaybackReferences", "client_playback_references"), ("encodingJobs", "encoding_jobs"))), "reviewed_tv_snapshot_counts")
    validate_reviewed_actor_residue(tables, actor["id"], "reviewed_tv_owned_residue")
    users = [row for row in tables["users"] if row.get("id") == actor["id"]]
    need(len(users) == 1 and users[0].get("name") == actor["username"] and users[0].get("is_administrator") is False and
         users[0].get("is_disabled") is False and canonical(users[0].get("policy")) == canonical({}), "reviewed_tv_actor_changed")
    plays = [row for row in tables["play_sessions"] if row.get("user_id") == actor["id"]]
    need(len(plays) == 1, "reviewed_tv_play_inventory")
    play = plays[0]
    need({"id", "auth_session_id", "item_id", "media_source_id", "duration_ticks", "device_id", "application_client_id", "client_correlated", "state",
          "counted", "position_ticks", "started_at", "stopped_at", "expires_at"} <= set(play), "reviewed_tv_play_identity")
    sessions = [row for row in tables["sessions"] if row["id"] == play["auth_session_id"]]
    need(play["item_id"] == item["id"] and play["media_source_id"] == review["item"]["mediaSourceId"] and type(play["duration_ticks"]) is int and
         play["duration_ticks"] == item["runtimeTicks"] and play["application_client_id"] is None and play["client_correlated"] is False and
         play["state"] == "Prepared" and play["counted"] is False and type(play["position_ticks"]) is int and play["position_ticks"] == 0 and
         play["started_at"] is None and play["stopped_at"] is None and isinstance(play["device_id"], str) and len(sessions) == 1 and sessions[0].get("kind") == "emby" and
         sessions[0].get("user_id") == actor["id"] and sessions[0].get("device_id") == play["device_id"], "reviewed_tv_play_identity")
    reviewed_timestamp_ns(play["expires_at"])
    preparations = data.get("unstartedPreparations")
    need(isinstance(preparations, list) and len(preparations) == 1 and isinstance(preparations[0], dict) and
         set(preparations[0]) == {"id", "authSessionId", "itemId", "state", "counted", "positionTicks", "prepareOrdinals"}, "reviewed_tv_preparation_receipt")
    preparation = preparations[0]
    need(isinstance(preparation["prepareOrdinals"], list) and len(preparation["prepareOrdinals"]) == 1 and reviewed_integer(preparation["prepareOrdinals"][0]) and
         preparation["prepareOrdinals"][0] > 0 and canonical({key: preparation[key] for key in preparation if key != "prepareOrdinals"}) == canonical({"id": play["id"],
             "authSessionId": play["auth_session_id"], "itemId": item["id"], "state": "Prepared", "counted": False, "positionTicks": 0}) and
         canonical(expirations[0]) == canonical({"playSessionId": play["id"], "authSessionId": play["auth_session_id"]}), "reviewed_tv_prepared_authority")
    userdata = [row for row in tables["user_item_data"] if row.get("user_id") == actor["id"]]
    need(len(userdata) == 1 and {"item_id", "play_count", "playback_position_ticks", "is_favorite", "played", "last_played_at"} <= set(userdata[0]) and
         userdata[0]["item_id"] == item["id"] and type(userdata[0]["play_count"]) is int and userdata[0]["play_count"] == 0 and
         type(userdata[0]["playback_position_ticks"]) is int and userdata[0]["playback_position_ticks"] == 0 and userdata[0]["is_favorite"] is False and
         userdata[0]["played"] is False and userdata[0]["last_played_at"] is None and canonical(userdata[0]) == canonical(data.get("newUserData")),
         "reviewed_tv_userdata_changed")
    admission = retained["postBrowseAdmission"]
    need(isinstance(admission, dict) and admission.get("kind") == "audited-candidate-live-admission" and
         type(admission.get("version")) is int and admission["version"] == 3 and admission.get("status") == "admitted_for_core_client" and
         admission.get("candidateAdmissionComplete") is True and admission.get("failure", "missing") is None and admission.get("cleanupFailures") == [] and
         admission.get("runtimeEpoch") == review["runtimeEpoch"] and admission.get("seedRuntimeBinding") == review["seedBinding"],
         "reviewed_tv_post_browse_admission")
    proof = admission.get("controllerSessions", {}).get("P")
    need(isinstance(proof, dict) and set(proof) == {"credentialId", "tokenSha256", "sameTokenRejected"} and
         proof["sameTokenRejected"] is True and isinstance(proof["credentialId"], str) and re.fullmatch(r"[0-9a-f]{32}", proof["credentialId"]) and
         isinstance(proof["tokenSha256"], str) and re.fullmatch(r"[0-9a-f]{64}", proof["tokenSha256"]), "reviewed_tv_post_browse_credential")
    for table, key in (("users", "id"), ("sessions", "user_id"), ("play_sessions", "user_id"), ("user_item_data", "user_id")):
        current = [row for row in retained["snapshot"]["tables"][table] if row.get(key) == actor["id"]]
        historical = [row for row in tables[table] if row.get(key) == actor["id"]]
        if table == "sessions":
            extra = [row for row in current if row.get("id") == proof["credentialId"]]
            need(len(extra) == 1 and not any(row.get("id") == proof["credentialId"] for row in historical) and
                 extra[0].get("kind") == "emby" and extra[0].get("token_hash") == "\\x" + proof["tokenSha256"] and
                 not any(row.get("auth_session_id") == proof["credentialId"] for row in retained["snapshot"]["tables"]["play_sessions"]),
                 "reviewed_tv_post_browse_session")
            reviewed_timestamp_ns(extra[0].get("revoked_at"))
            current = [row for row in current if row.get("id") != proof["credentialId"]]
        need(canonical(current) == canonical(historical), "reviewed_tv_history_changed")
    need(reviewed_timestamp_ns(retained["snapshot"]["capturedAt"]) >= reviewed_timestamp_ns(prior["capturedAt"]), "reviewed_tv_history_time")
    return plays


def load_reviewed_tv_baseline(review_pin, input_binding, binding, read_descriptor):
    """Load a TV-specific review without reinterpreting a movie contract."""
    descriptor(review_pin)
    review = read_descriptor(review_pin)
    need(isinstance(review, dict) and set(review) == REVIEWED_TV_BASELINE_KEYS, "reviewed_tv_review_schema")
    for key in REVIEWED_BASELINE_PINS | {"boundary"}:
        descriptor(review[key])
    need(isinstance(review["tvProvenance"], dict) and set(review["tvProvenance"]) == {"closeout", "snapshot", "sourceEpoch", "manifest"}, "reviewed_tv_provenance_shape")
    for pin in review["tvProvenance"].values():
        descriptor(pin)
    retained = {"review": review, **{key: read_descriptor(review[key]) for key in ("closeout", "snapshot", "sourceEpoch", "manifest", "postBrowseAdmission")},
                 "inputBinding": input_binding, "tvProvenance": {key: read_descriptor(pin) for key, pin in review["tvProvenance"].items()},
                "boundary": read_descriptor(review["boundary"])}
    validate_reviewed_tv_baseline(retained, binding)
    return retained


def verify_actor_before(snapshot, binding, scenario, retained=None, version=2):
    if version == 5:
        need(type(version) is int and scenario == "tv-browse" and retained is not None, "reviewed_tv_baseline_required")
        old_plays = validate_reviewed_tv_baseline(retained, binding)
        prior = retained["snapshot"]
        need(isinstance(snapshot, dict) and set(snapshot) == set(prior) and canonical(snapshot["tables"]) == canonical(prior["tables"]) and
             canonical(snapshot["sequences"]) == canonical(prior["sequences"]), "reviewed_tv_fresh_state_changed")
        captured = reviewed_timestamp_ns(snapshot["capturedAt"])
        need(captured >= reviewed_timestamp_ns(prior["capturedAt"]) and len(old_plays) < 256 and all(
             captured + BUDGETS["maximumSeconds"] * 1000000000 < reviewed_timestamp_ns(row["expires_at"]) + 7 * 86400 * 1000000000
             for row in old_plays), "reviewed_tv_pruning_deadline")
        return
    if version == 4:
        need(type(version) is int and scenario == "movie" and retained is not None, "reviewed_movie_baseline_required")
        validate_reviewed_movie_baseline(retained, binding)
        prior = retained["snapshot"]
        need(isinstance(snapshot, dict) and set(snapshot) == set(prior) and canonical(snapshot["tables"]) == canonical(prior["tables"]) and
             canonical(snapshot["sequences"]) == canonical(prior["sequences"]), "reviewed_movie_fresh_state_changed")
        old_plays = [row for row in prior["tables"]["play_sessions"] if row["user_id"] == binding["actors"]["movie"]["id"]]
        captured = reviewed_timestamp_ns(snapshot["capturedAt"])
        need(captured >= reviewed_timestamp_ns(prior["capturedAt"]) and len(old_plays) < 256 and all(
             captured + BUDGETS["maximumSeconds"] * 1000000000 < reviewed_timestamp_ns(row["expires_at"]) + 7 * 86400 * 1000000000
             for row in old_plays), "reviewed_movie_pruning_deadline")
        return
    actor = binding["actors"][scenario]["id"]
    rows = [row for row in snapshot["tables"]["users"] if row["id"] == actor]
    need(len(rows) == 1 and rows[0]["is_administrator"] is False and rows[0]["is_disabled"] is False, "scenario_actor_not_ordinary")
    if retained is None:
        need(not any(row["user_id"] == actor for row in snapshot["tables"]["play_sessions"]) and
             not any(row["user_id"] == actor for row in snapshot["tables"]["client_playback_references"]), "scenario_actor_already_consumed")
        return
    need(scenario == "movie" and set(retained) == {"closeout", "snapshot"}, "retained_movie_scenario_required")
    prior = retained["snapshot"]
    need(version in (2, 3), "retained_movie_version")
    play = validate_movie05_baseline(retained["closeout"], prior, binding) if version == 3 else validate_retained_movie_baseline(retained["closeout"], prior, binding)
    # Python equality conflates booleans and integers inside JSONB documents.
    need(set(snapshot) == set(prior) and canonical(snapshot["tables"]) == canonical(prior["tables"]) and
         canonical(snapshot["sequences"]) == canonical(prior["sequences"]), "retained_movie_fresh_state_changed")
    timestamp = lambda value: datetime.fromisoformat(value.replace("Z", "+00:00"))
    old_plays = [row for row in prior["tables"]["play_sessions"] if row["user_id"] == actor] if version == 3 else [play]
    need(timestamp(snapshot["capturedAt"]) >= timestamp(prior["capturedAt"]) and len(old_plays) < 256 and all(
         timestamp(snapshot["capturedAt"]) + timedelta(seconds=BUDGETS["maximumSeconds"]) < timestamp(row["expires_at"]) + timedelta(days=7) for row in old_plays), "retained_movie_pruning_deadline")


def server_log_file_facts(path, info, stdout):
    need(Path(path) == SERVER_LOG and stat.S_ISREG(info.st_mode) and stat.S_ISREG(stdout.st_mode) and info.st_uid == stdout.st_uid == 0 and
         stat.S_IMODE(info.st_mode) == stat.S_IMODE(stdout.st_mode) == 0o600 and info.st_nlink == stdout.st_nlink == 1 and
         info.st_dev == stdout.st_dev and info.st_ino == stdout.st_ino and info.st_dev > 0 and info.st_ino > 0, "server_log_file_authority")
    return {"path": str(SERVER_LOG), "device": str(info.st_dev), "inode": str(info.st_ino), "uid": 0, "mode": 0o600, "links": 1}


def validate_server_log_read(expected, candidate_before, candidate_after, file_before, file_after, raw, length):
    need(canonical(candidate_before) == canonical(candidate_after) == canonical(expected), "server_log_process_changed")
    need(canonical(file_before) == canonical(file_after), "server_log_rotated")
    need(isinstance(raw, bytes) and type(length) is int and 0 < length <= SERVER_LOG_LIMIT and len(raw) == length and raw.endswith(b"\n"), "server_log_prefix_incomplete")


def validate_server_log_pair(expected, before, after, before_raw, after_raw):
    keys = {"capturedAt", "candidateBefore", "candidateAfter", "file", "length", "content"}
    for snapshot, raw in ((before, before_raw), (after, after_raw)):
        need(set(snapshot) == keys and set(snapshot["file"]) == {"path", "device", "inode", "uid", "mode", "links"}, "server_log_snapshot_schema")
        facts = snapshot["file"]
        need(facts["path"] == str(SERVER_LOG) and all(type(facts[key]) is int for key in ("uid", "mode", "links")) and facts["uid"] == 0 and facts["mode"] == 0o600 and facts["links"] == 1 and
             all(isinstance(facts[key], str) for key in ("device", "inode")) and
             re.fullmatch(r"[1-9][0-9]*", facts["device"]) and re.fullmatch(r"[1-9][0-9]*", facts["inode"]), "server_log_snapshot_file")
        descriptor(snapshot["content"])
        need(hashlib.sha256(raw).hexdigest() == snapshot["content"]["sha256"], "server_log_content_changed")
        validate_server_log_read(expected, snapshot["candidateBefore"], snapshot["candidateAfter"], facts, facts, raw, snapshot["length"])
    need(canonical(before["file"]) == canonical(after["file"]) and after_raw.startswith(before_raw), "server_log_prefix_changed")
    need(datetime.fromisoformat(before["capturedAt"].replace("Z", "+00:00")) <= datetime.fromisoformat(after["capturedAt"].replace("Z", "+00:00")), "server_log_capture_order")


def verify_signal_target(state, row, process, boot_id):
    need(int(row["MainPID"]) == int(row["ExecMainPID"]) == state["pid"] == process["pid"] and process["bootId"] == boot_id and
         process["uid"] == 0 and process["cmdline"] == state["command"] and process["cgroup"] == "0::/system.slice/" + state["unit"] + "\n" and
         (state.get("process") is None or process == state["process"]), "gateway_signal_identity_changed")


def unit_argv(unit, description, command, output, role, ssh_connection):
    properties = {"Description": description, "Type": "exec", "User": "root", "Group": "root", "UMask": "0077", "Restart": "no", "RemainAfterExit": "yes",
        "PrivateNetwork": "yes", "PrivateTmp": "yes", "ProtectSystem": "strict", "ProtectHome": "read-only", "NoNewPrivileges": "yes", "KillMode": "control-group",
        "TimeoutStopSec": "30", "RuntimeMaxSec": "1830" if role == "gateway" else "630", "MemoryMax": "512M" if role == "gateway" else "2G",
        "MemorySwapMax": "0", "TasksMax": "128" if role == "gateway" else "256", "CPUQuota": "100%", "ReadWritePaths": str(output / ("gateway" if role == "gateway" else "browser")),
        "StandardOutput": "append:" + str(output / "private" / (role + "-stdout.log")), "StandardError": "append:" + str(output / "private" / (role + "-stderr.log")),
        "UnsetEnvironment": "DEBUG PWDEBUG"}
    argv = ["/usr/bin/systemd-run", "--unit=" + unit, "--service-type=exec", "--working-directory=" + str(output)]
    argv += ["--property=" + key + "=" + val for key, val in properties.items()]
    argv += ["--setenv=SSH_CONNECTION=" + ssh_connection, "--setenv=PLAYWRIGHT_BROWSERS_PATH=/root/.cache/ms-playwright", "--setenv=HOME=/root", "--", *command]
    return argv, properties


class ClientRun:
    def __init__(self, runtime, value, input_pin, source_pin):
        self.r, self.value, self.input_pin, self.source_pin = runtime, validate_input(value), input_pin, source_pin
        self.started, self.stage, self.created = time.monotonic(), "input_preflight", False
        self.output, self.private = Path(value["output"]), Path(value["output"]) / "private"
        self.units = {role: "goby-core-client-" + value["runId"] + "-" + role + ".service" for role in ("gateway", "browser")}
        self.workers, self.failures = {}, []
        self.server_log_captures, self.server_log_pin = {}, None
        self.current_runtime = None

    def current_identity(self):
        """Keep current process facts separate from immutable product history."""
        return self.current_runtime["current"] if self.current_runtime is not None else self.epoch

    def remaining(self, cleanup=False):
        left = BUDGETS["maximumSeconds"] - (0 if cleanup else BUDGETS["cleanupSeconds"]) - (time.monotonic() - self.started)
        need(left > 0, "outer_client_deadline")
        return left

    def save(self, name, value):
        return self.s.write_json_once(self.private / name, value)

    def pin_file(self, path):
        raw = self.s.read_checked(path, self.hash_file(path))
        return {"path": str(path), "sha256": hashlib.sha256(raw).hexdigest()}

    def hash_file(self, path):
        before = self.s.safe_path(path)
        need(before.st_nlink == 1 and before.st_size <= 32 << 20, "client_artifact_file_bound")
        with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
            raw = stream.read((32 << 20) + 1)
            need(self.s.file_identity(os.fstat(stream.fileno())) == self.s.file_identity(before), "client_artifact_changed")
        need(self.s.file_identity(Path(path).stat()) == self.s.file_identity(before), "client_artifact_replaced")
        return hashlib.sha256(raw).hexdigest()

    def preflight(self):
        need(not os.path.lexists(self.output), "client_output_collision")
        if self.value["version"] == 4:
            need(self.value["sources"]["closer"]["sha256"] != REUSED_AV_SOURCES["closer"], "version4_closer_not_admitted")
        if self.value["version"] == 5:
            need(self.value["sources"]["closer"]["sha256"] != REUSED_AV_SOURCES["closer"], "version5_closer_not_admitted")
            need(canonical(self.value["avVerification"]) != canonical(AV_VERIFICATION), "version5_components_not_admitted")
        epoch = json.loads(self.r.read_bootstrap(self.value["runtimeEpoch"]))
        self.modules = {key: self.r.load_helper(key, pin) for key, pin in epoch["helpers"].items()}
        self.s = self.modules["seed"]
        need(self.s.descriptor(self.input_pin) == self.value and
             (self.value["version"] == 5 or self.value["runtimeHelper"] == epoch["runtimeHelper"]), "outer_input_or_runtime_helper_changed")
        self.epoch = self.r.validate_epoch(epoch)
        self.binding = self.s.descriptor(self.value["seedBinding"])
        self.r.validate_seed_runtime_binding(self.binding, self.value["runtimeEpoch"], epoch, self.s.descriptor(self.r.SEED))
        self.retained = None
        if self.value["version"] == 2:
            closeout = self.s.descriptor(self.value["retainedBaseline"])
            need(closeout["sourceAfter"] == RETAINED_SNAPSHOT, "retained_movie_snapshot_changed")
            self.retained = {"closeout": closeout, "snapshot": self.s.descriptor(closeout["sourceAfter"])}
            validate_retained_movie_baseline(closeout, self.retained["snapshot"], self.binding)
        elif self.value["version"] == 3 and self.value["retainedBaseline"] is not None:
            closeout = self.s.descriptor(self.value["retainedBaseline"])
            need(closeout["inputEvidence"]["after"] == MOVIE05_SNAPSHOT, "movie05_snapshot_changed")
            self.retained = {"closeout": closeout, "snapshot": self.s.descriptor(closeout["inputEvidence"]["after"])}
            validate_movie05_baseline(closeout, self.retained["snapshot"], self.binding)
        elif self.value["version"] == 4:
            self.retained = load_reviewed_movie_baseline(self.value["retainedBaseline"], {key: self.value[key] for key in ("runtimeEpoch", "seedBinding")},
                                                        self.binding, self.s.descriptor)
        elif self.value["version"] == 5:
            self.retained = load_reviewed_tv_baseline(self.value["retainedBaseline"], {key: self.value[key] for key in ("runtimeEpoch", "seedBinding")},
                                                     self.binding, self.s.descriptor)
        report = self.s.descriptor(self.value["admission"])
        admitted(report, self.value, epoch,
                 reused_admission04=self.s.descriptor(AFFECTED_TV_PARENT_ADMISSION04) if epoch.get("version") == 3 else None,
                 product_input=self.s.descriptor(epoch["productInput"]) if epoch.get("version") == 3 else None)
        self.s.descriptor(self.value["admissionCloseout"])
        verification = self.s.descriptor(self.value["avVerification"])
        if self.value["version"] == 5:
            validate_v5_component_evidence(verification, self.value, self.source_pin,
                                           lambda pin: self.s.read_checked(pin["path"], pin["sha256"]))
            self.current_runtime = self.r.load_current_runtime(self.value["currentRuntime"], self.value, self.epoch,
                self.s.descriptor, lambda pin: self.s.read_checked(pin["path"], pin["sha256"]))
        else:
            validate_native_rejection_verification(verification, self.s.descriptor(REUSED_AV_VERIFICATION))
        for pin in [self.value["node"], *self.value["sources"].values()]:
            self.s.read_checked(pin["path"], pin["sha256"])
        self.gateway = self.r.load_helper("browser_gateway", self.value["sources"]["gateway"])
        self.hosting = self.s.descriptor(self.value["hosting"])
        need(self.hosting["kind"] == "core-av-original-client-hosting" and self.hosting["version"] == "4.9.5.0", "original_client_hosting_not_admitted")
        self.verify_hosting()
        # Hosting initialization belongs to the historical product epoch.
        hosting_value = {**self.value, "runtimeHelper": epoch["runtimeHelper"]} if self.value["version"] == 5 else self.value
        hosting_initialized(self.s.descriptor(self.value["hostingInitialization"]), hosting_value, self.hosting, self.epoch,
                            self.s.descriptor, lambda pin: self.s.read_checked(pin["path"], pin["sha256"]), self.r.TABLES,
                            lineage=self.r.resolve_epoch_lineage(self.epoch, self.s.descriptor) if self.epoch.get("version") == 3 else None)
        self.catalog = self.s.descriptor(self.value["compiledCatalog"])
        source_manifest = self.s.descriptor(epoch["currentSource"]["sourceManifest"])
        self.modules["admission"].validate_catalog(self.catalog, source_manifest, self.value["compiledCatalog"])
        probe = self.modules["provision"].Provision({"runId": epoch["candidate"]["runId"], "ports": epoch["candidate"]["ports"]}, self.input_pin, epoch["helpers"]["provision"])
        for unit in self.units.values():
            need(probe.show(unit).get("LoadState") == "not-found" and not any(os.path.lexists(Path(root) / unit) for root in ("/run/systemd/transient", "/run/systemd/system", "/etc/systemd/system")), "client_unit_collision")

    def verify_hosting(self):
        host = self.hosting
        need(self.gateway.metadata(host["process"]["pid"]) == host["process"], "original_client_host_process_changed")
        self.gateway.verify_listener(host["process"]["pid"], host["listener"])
        self.s.read_checked(host["process"]["exe"], host["executableSha256"])

    def open(self):
        budget = {"maximumSeconds": 1200, "cleanupSeconds": 240, "maximumRequests": 2, "cleanupRequests": 1}
        runtime_args = ({"current_runtime": self.current_runtime, "current_runtime_pin": self.value["currentRuntime"]}
                        if self.value["version"] == 5 else {})
        self.io = self.r.EpochIO({"output": str(self.output), "budgets": budget}, self.input_pin, self.source_pin,
                               self.value["runtimeEpoch"], self.value["seedBinding"], self.modules, **runtime_args)
        try:
            self.io.open()
        finally:
            self.created = self.io.created
        self.p, self.reader = self.io.provision, self.io.reader
        for name in ("gateway", "browser", "closeout"):
            self.save("mkdir-" + name + "-intent.json", {"path": str(self.output / name), "mode": "0700"})
            os.mkdir(self.output / name, 0o700)
            self.s.sync_dir(self.output)
        for role in self.units:
            for stream in ("stdout", "stderr"):
                self.s.write_once(self.private / (role + "-" + stream + ".log"), b"")

    def source_sample(self, label):
        self.remaining(cleanup=label == "after")
        before = self.io.pin()
        current = self.current_identity()
        postgres = self.modules["gateway"].metadata(current["postgresProcess"]["pid"])
        lease = self.reader.deployment_lease()
        need(postgres == current["postgresProcess"] and lease == current["lease"], "source_sample_epoch_changed")
        self.reader.assert_target_cluster()
        value = self.reader.sql_json(self.epoch["candidate"]["database"], self.modules["admission"].snapshot_sql(self.catalog))
        need(set(value) == {"capturedAt", "tables", "sequences"} and set(value["tables"]) == self.r.TABLES, "source_sample_inventory_invalid")
        need(self.io.pin() == before and self.reader.deployment_lease() == lease and self.modules["gateway"].metadata(postgres["pid"]) == postgres, "source_sample_changed_during_capture")
        pin = self.save("source-" + label + ".json", value)
        runtime_record = {"candidate": before, "postgres": postgres, "lease": lease}
        if self.value["version"] == 5:
            runtime_record["currentRuntime"] = self.value["currentRuntime"]
        self.save("source-" + label + "-runtime.json", runtime_record)
        return value, pin, before, postgres, lease

    def capture_server_log(self, label):
        need(self.value["version"] in (3, 4, 5) and label in ("before", "after") and label not in self.server_log_captures, "server_log_capture_scope")
        self.remaining(cleanup=label == "after")
        expected = self.current_identity()["candidateProcess"]
        metadata = self.modules["gateway"].metadata
        candidate_before = metadata(expected["pid"])
        need(canonical(candidate_before) == canonical(expected), "server_log_process_changed")
        stdout = Path("/proc") / str(expected["pid"]) / "fd/1"
        path_before = self.s.safe_path(SERVER_LOG)
        file_before = server_log_file_facts(SERVER_LOG, path_before, stdout.stat())
        with os.fdopen(os.open(SERVER_LOG, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
            opened = os.fstat(stream.fileno())
            need(canonical(server_log_file_facts(SERVER_LOG, opened, stdout.stat())) == canonical(file_before), "server_log_rotated")
            length = opened.st_size
            need(path_before.st_size <= length and 0 < length <= SERVER_LOG_LIMIT, "server_log_file_bound")
            raw = stream.read(length)
            final_fd = os.fstat(stream.fileno())
            need(final_fd.st_size >= length and canonical(server_log_file_facts(SERVER_LOG, final_fd, stdout.stat())) == canonical(file_before), "server_log_truncated_or_redirected")
        path_after = self.s.safe_path(SERVER_LOG)
        file_after = server_log_file_facts(SERVER_LOG, path_after, stdout.stat())
        need(path_after.st_size >= length, "server_log_truncated_or_redirected")
        candidate_after = metadata(expected["pid"])
        validate_server_log_read(expected, candidate_before, candidate_after, file_before, file_after, raw, length)
        captured_at = datetime.now(timezone.utc).isoformat()
        content = self.s.write_once(self.private / ("server-stdout-" + label + ".raw"), raw)
        snapshot = {"capturedAt": captured_at, "candidateBefore": candidate_before, "candidateAfter": candidate_after,
                    "file": file_after, "length": length, "content": content}
        self.server_log_captures[label] = self.save("server-log-" + label + ".json", snapshot)
        return snapshot, raw

    def finish_server_log(self):
        need(all(row.get("closure") and row["closure"]["unit"].get("MainPID") == "0" and row["closure"]["workerPidAbsent"] is True and
                 row["closure"]["recursiveCgroupPids"] == [] for row in self.workers.values()), "server_log_workers_not_closed")
        after, raw = self.capture_server_log("after")
        before, before_raw = self.server_log_before
        validate_server_log_pair(self.current_identity()["candidateProcess"], before, after, before_raw, raw)
        receipt = {"kind": "audited-candidate-client-server-log", "version": 1, "runId": self.value["runId"], "runtimeEpoch": self.value["runtimeEpoch"],
                   "input": self.input_pin, "controller": self.source_pin, "before": before, "after": after}
        if self.value["version"] == 5:
            receipt.update(version=2, currentRuntime=self.value["currentRuntime"])
        self.server_log_pin = self.save("server-log.json", receipt)

    def show_worker(self, role):
        fields = "Id,LoadState,ActiveState,SubState,MainPID,ExecMainPID,InvocationID,Result,ExecMainStatus,ExecMainCode,ControlGroup,Description,ExecStart,Transient,User,PrivateNetwork,ProtectHome,Restart,RemainAfterExit"
        result = subprocess.run(["/usr/bin/systemctl", "show", self.units[role], "--property=" + fields], capture_output=True, timeout=10, check=True)
        need(len(result.stdout) <= 65536, "worker_metadata_bound")
        return dict(line.split("=", 1) for line in result.stdout.decode().splitlines() if "=" in line)

    def own_worker(self, role, row):
        state = self.workers[role]
        need(row.get("Id") == self.units[role] and row.get("Transient") == "yes" and row.get("Description") == state["description"] and
             row.get("User") == "root" and row.get("Restart") == "no" and row.get("RemainAfterExit") == "yes" and
             row.get("PrivateNetwork") == "yes" and row.get("ProtectHome") == "read-only", "worker_ownership_changed")
        command = row["ExecStart"]
        need(command.count("argv[]=") == 1 and self.modules["provision"].shlex.split(command.split("argv[]=", 1)[1].split(";", 1)[0]) == state["command"], "worker_command_changed")
        if state.get("invocationId"):
            need(row["InvocationID"] == state["invocationId"], "worker_invocation_changed")

    def start_worker(self, role, command):
        self.remaining()
        description = "Owned core client " + self.input_pin["sha256"] + " " + role
        argv, properties = unit_argv(self.units[role], description, command, self.output, role, os.environ["SSH_CONNECTION"])
        self.workers[role] = {"unit": self.units[role], "description": description, "command": command, "startRequested": True, "stopRequested": False,
                              "startAcknowledged": False, "stopAcknowledged": False, "pid": None, "invocationId": None, "terminal": None, "closure": None}
        self.save(role + "-start-intent.json", {"unit": self.units[role], "argv": argv, "properties": properties})
        self.p.command("start-owned-" + role, argv)
        self.workers[role]["startAcknowledged"] = True
        row = self.show_worker(role)
        self.own_worker(role, row)
        state = self.workers[role]
        state.update(pid=int(row["ExecMainPID"]), invocationId=row["InvocationID"], controlGroup=row["ControlGroup"])
        need(state["pid"] > 1, "worker_pid_not_observed")
        if role == "gateway" and row["MainPID"] != "0":
            process = self.gateway.metadata(state["pid"])
            verify_signal_target(state, row, process, self.current_identity()["candidateProcess"]["bootId"])
            state["process"] = process
        self.save(role + "-started.json", row)
        return state

    def wait_gateway(self):
        deadline = time.monotonic() + BUDGETS["gatewayReadySeconds"]
        path = self.output / "gateway/gateway-attestation.json"
        while time.monotonic() < deadline:
            self.remaining()
            row = self.show_worker("gateway")
            self.own_worker("gateway", row)
            need(row["ActiveState"] == "active" and int(row["MainPID"]) == self.workers["gateway"]["pid"], "gateway_exited_before_ready")
            if path.exists():
                pin = self.pin_file(path)
                value = self.s.descriptor(pin)
                need(value["kind"] == "core-av-client-gateway" and value["runId"] == self.value["runId"] and value["inputSha256"] == self.gateway_input["sha256"] and
                     value["process"] == self.gateway.metadata(self.workers["gateway"]["pid"]) and value["upstreams"] == self.gateway_config["upstreams"] and value["sources"] == self.gateway_config["sources"], "gateway_attestation_binding")
                self.gateway.verify_listener(value["process"]["pid"], value["listener"])
                need(sorted(node.name for node in (self.output / "gateway").iterdir()) == ["gateway-attestation.json", "private"] and not list((self.output / "gateway/private").iterdir()), "gateway_ledger_not_fresh")
                return value, pin
            time.sleep(0.2)
        raise RunError("gateway_ready_deadline")

    def wait_client(self):
        deadline = time.monotonic() + BUDGETS["clientSeconds"] + BUDGETS["workerGraceSeconds"]
        while time.monotonic() < deadline:
            self.remaining()
            row = self.show_worker("browser")
            self.own_worker("browser", row)
            if row["MainPID"] == "0":
                self.record_terminal("browser", row)
                return
            need(row["ActiveState"] == "active", "browser_worker_state_invalid")
            time.sleep(1)
        raise RunError("browser_worker_deadline")

    def record_terminal(self, role, row):
        state = self.workers[role]
        need(row["MainPID"] == "0" and int(row["ExecMainPID"]) == state["pid"], "worker_terminal_identity_changed")
        if state["terminal"] is None:
            state["terminal"] = row
            self.save(role + "-terminal.json", row)

    def finish_gateway(self, row):
        """Observe natural exit before stopping the collectible transient unit."""
        state = self.workers["gateway"]
        if row["MainPID"] != "0":
            need(int(row["MainPID"]) == state["pid"], "gateway_shutdown_pid_changed")
            handle = os.pidfd_open(state["pid"])
            try:
                fresh = self.show_worker("gateway")
                self.own_worker("gateway", fresh)
                process = self.gateway.metadata(state["pid"])
                verify_signal_target(state, fresh, process, self.current_identity()["candidateProcess"]["bootId"])
                if hasattr(self, "gateway_attestation"):
                    need(process == self.gateway_attestation["process"], "gateway_shutdown_identity_changed")
                self.save("gateway-signal-intent.json", {"unit": self.units["gateway"], "invocationId": state["invocationId"], "process": process, "signal": "SIGTERM"})
                state["signalRequested"] = True
                signal.pidfd_send_signal(handle, signal.SIGTERM)
                self.save("gateway-signal-result.json", {"signalAcknowledged": True, "workerPid": state["pid"]})
            finally:
                os.close(handle)
            deadline = time.monotonic() + 25
            while time.monotonic() < deadline:
                self.remaining(cleanup=True)
                row = self.show_worker("gateway")
                self.own_worker("gateway", row)
                if row["MainPID"] == "0":
                    break
                time.sleep(0.2)
        self.record_terminal("gateway", row)

    def close_worker(self, role):
        state = self.workers.get(role)
        if not state or state["stopRequested"]:
            return
        self.remaining(cleanup=True)
        row = self.show_worker(role)
        self.own_worker(role, row)
        if not state.get("pid"):
            state.update(pid=int(row["ExecMainPID"]), invocationId=row["InvocationID"], controlGroup=row["ControlGroup"])
        if role == "gateway":
            try:
                self.finish_gateway(row)
            except Exception as error:
                self.failures.append({"stage": "gateway_graceful_exit", "code": str(error) if isinstance(error, RunError) else "gateway_exit_not_observed", "errorType": type(error).__name__})
        elif row["MainPID"] == "0":
            self.record_terminal(role, row)
        self.save(role + "-stop-intent.json", {"unit": self.units[role], "observed": row})
        state["stopRequested"] = True
        self.p.command("stop-owned-" + role, ["/usr/bin/systemctl", "stop", self.units[role]])
        state["stopAcknowledged"] = True
        final = self.show_worker(role)
        if final["LoadState"] != "not-found":
            self.own_worker(role, final)
        need(state["controlGroup"] == "/system.slice/" + self.units[role], "worker_cgroup_changed")
        group = Path("/sys/fs/cgroup") / state["controlGroup"].lstrip("/")
        pids = sorted({int(pid) for path in ([group / "cgroup.procs", *group.rglob("cgroup.procs")] if group.exists() else []) if path.exists() for pid in path.read_text().split()})
        absent = state["pid"] is not None and state["pid"] > 1 and not Path("/proc", str(state["pid"])).exists()
        state["closure"] = {"unit": final, "workerPid": state["pid"], "workerPidAbsent": absent, "recursiveCgroupPids": pids}
        self.save(role + "-closed.json", state["closure"])
        need(final.get("MainPID") == "0" and absent and not pids, "owned_worker_not_fully_closed")

    def gateway_exit(self):
        state = self.workers["gateway"]
        terminal = state["terminal"]
        need(terminal is not None and terminal["LoadState"] != "not-found" and terminal["Result"] == "success" and terminal["ExecMainStatus"] == "0", "gateway_exit_status_not_observed")
        return int(terminal["ExecMainStatus"])

    def run(self):
        self.preflight()
        self.open()
        self.stage = "source_before"
        before, self.before_pin, self.candidate_before, self.postgres_before, self.lease_before = self.source_sample("before")
        verify_actor_before(before, self.binding, self.value["scenario"], self.retained, self.value["version"])
        if self.value["version"] in (3, 4, 5):
            self.stage = "server_log_before"
            self.server_log_before = self.capture_server_log("before")
        self.before_ns = str(time.clock_gettime_ns(time.CLOCK_MONOTONIC))
        candidate_process = {key: self.candidate_before[key] for key in self.gateway.PROCESS_FIELDS}
        self.gateway_config = {"schemaVersion": 1, "runId": self.value["runId"], "proxyOrigin": self.epoch["candidate"]["publicUrl"], "browserOrigin": self.epoch["candidate"]["publicUrl"],
            "directOrigin": self.epoch["candidate"]["directUrl"], "ledgerRoot": str(self.output / "gateway"), "sources": {key: self.value["sources"][key] for key in ("gateway", "proxy")},
            "upstreams": {"goby": {"process": candidate_process, "listener": self.candidate_before["listener"], "executableSha256": self.epoch["currentSource"]["binary"]["sha256"]},
                          "reference": {"process": self.hosting["process"], "listener": self.hosting["listener"], "executableSha256": self.hosting["executableSha256"]}}, "budgets": GATEWAY_BUDGETS}
        self.gateway_input = self.save("gateway-input.json", self.gateway_config)
        try:
            self.stage = "gateway_start"
            self.start_worker("gateway", ["/usr/bin/python3", "-I", "-B", self.value["sources"]["gateway"]["path"], "--input", self.gateway_input["path"], "--input-sha256", self.gateway_input["sha256"]])
            self.gateway_attestation, self.gateway_pin = self.wait_gateway()
            actor = self.binding["actors"][self.value["scenario"]]
            source = {"manifestSha256": self.epoch["currentSource"]["sourceManifest"]["sha256"], "binarySha256": self.epoch["currentSource"]["binary"]["sha256"], "schema": 28}
            approval = self.save("internal-admission.json", {"kind": "audited-candidate-client-approval", "runId": self.value["runId"], "scenario": self.value["scenario"],
                "sourceManifestSha256": source["manifestSha256"], "binarySha256": source["binarySha256"], "serverId": self.binding["serverId"], "isolatedCandidate": True,
                "authorizedRunInput": self.input_pin, "admission": self.value["admission"], "admissionCloseout": self.value["admissionCloseout"]})
            manifest = {"kind": "audited-candidate-client-input", "version": 1, "runId": self.value["runId"], "scenario": self.value["scenario"],
                "clientUrl": self.gateway_config["browserOrigin"] + "/web/index.html", "browserOrigin": self.gateway_config["browserOrigin"], "directOrigin": self.gateway_config["directOrigin"],
                "serverId": self.binding["serverId"], "actor": {"id": actor["id"], "username": actor["username"]}, "credentials": actor["credentials"], "source": source,
                "processes": {"candidate": self.candidate_before, "gateway": {**self.gateway_attestation["process"], "listener": self.gateway_attestation["listener"]}},
                "catalog": self.binding["catalog"], "budgets": {"maximumSeconds": 600, "cleanupSeconds": 120}, "output": str(self.output / "browser"), "approval": approval, "gatewayAttestation": self.gateway_pin}
            self.manifest_pin = self.save("adapter-input.json", manifest)
            self.stage = "browser_start"
            need(self.io.pin() == self.candidate_before, "candidate_changed_before_browser")
            self.start_worker("browser", ["/usr/bin/nsenter", "-t", str(self.gateway_attestation["process"]["pid"]), "-n", "--", self.value["node"]["path"],
                self.value["sources"]["adapter"]["path"], "--manifest", self.manifest_pin["path"], "--manifest-sha256", self.manifest_pin["sha256"]])
            self.stage = "browser_running"
            self.wait_client()
        except Exception as error:
            self.failures.append({"stage": self.stage, "code": str(error) if isinstance(error, RunError) else "client_worker_operation_failed", "errorType": type(error).__name__})
        finally:
            for role in ("browser", "gateway"):
                try:
                    self.close_worker(role)
                except Exception as error:
                    self.failures.append({"stage": "close_" + role, "code": str(error) if isinstance(error, RunError) else "worker_close_failed", "errorType": type(error).__name__})
        self.stage = "source_after"
        after, after_pin, candidate_after, postgres_after, lease_after = self.source_sample("after")
        if self.value["version"] in (3, 4, 5):
            self.stage = "server_log_after"
            self.finish_server_log()
        self.verify_hosting()
        self.after_ns = str(time.clock_gettime_ns(time.CLOCK_MONOTONIC))
        need(not self.failures, "worker_responsibility_unclosed")
        client = self.workers["browser"]
        if self.value["version"] == 5:
            need((client["terminal"]["Result"], client["terminal"]["ExecMainStatus"]) in (("success", "0"), ("exit-code", "1")), "diagnostic_browser_worker_failed")
        else:
            need(client["terminal"]["Result"] == "success" and client["terminal"]["ExecMainStatus"] == "0", "browser_worker_failed")
        gateway_exit = self.gateway_exit()
        index_pin = self.pin_file(self.output / "gateway/index.json")
        index = self.s.descriptor(index_pin)
        need(index["complete"] is True and index["webMediaAndWebSocketBodiesRetained"] is False, "gateway_ledger_incomplete")
        boundary = {"kind": "audited-candidate-client-boundary", "version": 1, "runId": self.value["runId"], "runtimeEpoch": self.value["runtimeEpoch"],
            "sourceBefore": self.before_pin, "sourceAfter": after_pin, "candidateBefore": self.candidate_before, "candidateAfter": candidate_after,
            "postgresBefore": self.postgres_before, "postgresAfter": postgres_after, "leaseBefore": self.lease_before, "leaseAfter": lease_after,
            "database": self.epoch["candidate"]["database"], "beforeMonotonicNs": self.before_ns, "afterMonotonicNs": self.after_ns,
            "clientWorker": {"exitCode": int(client["terminal"]["ExecMainStatus"]), "mainPID": int(client["closure"]["unit"]["MainPID"]), "workerPidAbsent": client["closure"]["workerPidAbsent"], "remainingBrowserPids": client["closure"]["recursiveCgroupPids"]},
            "gatewayWorker": {"exitCode": gateway_exit, "mainPID": int(self.workers["gateway"]["closure"]["unit"]["MainPID"]), "workerPidAbsent": self.workers["gateway"]["closure"]["workerPidAbsent"], "index": index_pin}}
        if self.value["version"] == 5:
            boundary.update(version=2, currentRuntime=self.value["currentRuntime"])
        boundary_pin = self.save("boundary.json", boundary)
        closeout = {"kind": "audited-candidate-client-closeout-input", "version": self.value["version"], "manifest": self.manifest_pin,
            "observation": self.pin_file(self.output / "browser/observation.json"), "summary": self.pin_file(self.output / "browser/summary.json"), "gatewayAttestation": self.gateway_pin,
            "gatewayIndex": index_pin, "runtimeEpoch": self.value["runtimeEpoch"], "admission": self.value["admission"], "seedBinding": self.value["seedBinding"],
            "sourceBefore": self.before_pin, "sourceAfter": after_pin, "boundary": boundary_pin, "sources": self.value["sources"], "output": str(self.output / "closeout")}
        if self.value["version"] in (2, 3, 4, 5):
            closeout["retainedBaseline"] = self.value["retainedBaseline"]
        if self.value["version"] in (3, 4, 5):
            closeout["serverLog"] = self.server_log_pin
        if self.value["version"] == 5:
            closeout["currentRuntime"] = self.value["currentRuntime"]
        closeout_input = self.save("closeout-input.json", closeout)
        self.stage = "offline_closeout"
        self.remaining(cleanup=True)
        self.p.command("offline-client-closeout", ["/usr/bin/env", "SSH_CONNECTION=" + os.environ["SSH_CONNECTION"], "HOME=/root", self.value["node"]["path"],
            self.value["sources"]["closer"]["path"], "--input", closeout_input["path"], "--input-sha256", closeout_input["sha256"]])
        summary_pin = self.pin_file(self.output / "closeout/summary.json")
        summary = self.s.descriptor(summary_pin)
        if self.value["version"] == 5:
            need(isinstance(summary, dict) and summary.get("status") == "owned_state_closed_diagnostic_result" and summary.get("clientAcceptance") is False and
                 type(summary.get("uiAccepted")) is bool and "originalBrowserOutcome" in summary and "originalBrowserFailure" in summary and
                 isinstance(summary.get("diagnostic"), dict) and summary["diagnostic"].get("status") in ("response_identified", "inconclusive"),
                 "offline_diagnostic_closeout_incomplete")
            return {"status": "owned_state_closed_diagnostic_result", "runId": self.value["runId"], "scenario": self.value["scenario"],
                    "closeout": summary_pin, "boundary": boundary_pin, "clientAcceptance": False,
                    **{key: summary[key] for key in ("uiAccepted", "originalBrowserOutcome", "originalBrowserFailure", "diagnostic")},
                    "controllerBusinessHttpRequests": 0, "candidateRestarted": False, "postgresRestarted": False}
        need(summary["status"] == "core_scenario_closed", "offline_closeout_incomplete")
        return {"status": "core_scenario_closed", "runId": self.value["runId"], "scenario": self.value["scenario"], "closeout": summary_pin, "boundary": boundary_pin,
                "controllerBusinessHttpRequests": 0, "candidateRestarted": False, "postgresRestarted": False}


def main():
    need(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and sys.flags.isolated and sys.flags.dont_write_bytecode, "isolated_root_ssh_required")
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    for key in ("input", "input-sha256", "source-sha256", "runtime-helper", "runtime-helper-sha256"):
        parser.add_argument("--" + key, required=True)
    args = parser.parse_args()
    runtime_pin = {"path": args.runtime_helper, "sha256": args.runtime_helper_sha256}
    source_pin = {"path": str(Path(__file__).absolute()), "sha256": args.source_sha256}
    input_pin = {"path": args.input, "sha256": args.input_sha256}
    runtime, value = bootstrap_runtime(runtime_pin, source_pin, input_pin)
    job = ClientRun(runtime, value, input_pin, source_pin)
    def expired(unused_signal, unused_frame):
        raise RunError("outer_client_absolute_deadline")
    for number in (signal.SIGALRM, signal.SIGTERM, signal.SIGINT):
        signal.signal(number, expired)
    signal.setitimer(signal.ITIMER_REAL, BUDGETS["maximumSeconds"])
    try:
        result = job.run()
        if job.created:
            result["report"] = job.save("report.json", result)
        print(json.dumps(result))
        return 0
    except Exception as error:
        result = {"status": "core_client_run_incomplete", "stage": job.stage, "code": str(error) if isinstance(error, RunError) else "outer_client_operation_failed",
            "errorType": type(error).__name__, "runId": value["runId"], "scenario": value["scenario"], "failures": job.failures, "workers": job.workers,
            "automaticBusinessRetry": False, "controllerBusinessHttpRequests": 0, "receipt": None}
        if value["version"] in (3, 4, 5):
            result.update(serverLog=job.server_log_pin, serverLogCaptures=job.server_log_captures)
        try:
            if job.created:
                result["receipt"] = job.save("failure.json", result)
        except Exception as receipt_error:
            result["receiptErrorType"] = type(receipt_error).__name__
        print(json.dumps(result))
        return 2
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)


if __name__ == "__main__":
    raise SystemExit(main())
