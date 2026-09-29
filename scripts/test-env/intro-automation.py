#!/usr/bin/env python3
"""Accept library-driven intro automation on one explicitly owned test deployment.

Required private JSON: base_url, control_script, setup_token, admin_name,
admin_password, positive_tv_root, negative_tv_root, disabled_tv_root, output_dir.
Library roots are disjoint container paths. The enabled TV library contains the
positive and negative roots; the second TV library remains disabled. Negative
fixtures must belong to separate series/cohorts from the accepted positives.
Optional: allow_existing_admin (false), timeout_seconds (600), quiet_seconds (3),
expected_positive_episodes (3), expected_negative_episodes (2), and
expected_disabled_episodes (3). At least one positive must actually qualify.

The executable root-owned controller accepts start, recreate, stop, status.
Status returns running (bool), pid (int), and optionally process_pids (int[]).
It owns the exact Docker deployment and database. No service is inferred.

--phase all runs enable, rescan, disable. Individual phases reuse the private
intro-automation-state.json checkpoint in output_dir. Enable initializes two
disabled libraries, scans them once, and enables only the selected library.
Rescan requires a new automatic run. Disable checks marker removal and durable
disablement after recreation. Every invocation stops the owned application.

Only library options and scans trigger analysis: no analysis-start, task-start,
manual intro, decision, or global analysis configuration write is permitted.
Failed background work is an acceptance failure with task error evidence; it
must never be reported as a successful no-match. This driver injects no faults
and makes no broader detector accuracy claim.
"""

import argparse
import importlib.util
import json
import os
from pathlib import Path, PurePosixPath
import re
import signal
import stat
import subprocess
import sys
import time
import urllib.parse


spec = importlib.util.spec_from_file_location("oci_delivery", Path(__file__).with_name("oci-delivery.py"))
oci = importlib.util.module_from_spec(spec)
spec.loader.exec_module(oci)
need, emit, Failure = oci.need, oci.emit, oci.Failure
PHASES = ("enable", "rescan", "disable")


def load_config(filename):
    path = Path(filename)
    need(path.is_absolute(), "config_path_must_be_absolute")
    metadata = path.lstat()
    need(stat.S_ISREG(metadata.st_mode) and metadata.st_mode & 0o077 == 0
         and 0 < metadata.st_size <= 16384, "config_must_be_a_bounded_private_regular_file")
    value = json.loads(path.read_bytes())
    required = {"base_url", "control_script", "setup_token", "admin_name", "admin_password",
                "positive_tv_root", "negative_tv_root", "disabled_tv_root", "output_dir"}
    optional = {"allow_existing_admin", "timeout_seconds", "quiet_seconds", "expected_positive_episodes",
                "expected_negative_episodes", "expected_disabled_episodes"}
    need(type(value) is dict and required <= value.keys() <= required | optional, "invalid_config_fields")
    need(all(type(value[key]) is str and value[key] for key in required), "invalid_config_strings")
    origin = urllib.parse.urlsplit(value["base_url"])
    need(origin.scheme == "http" and origin.hostname in {"127.0.0.1", "::1", "localhost"}
         and origin.port is not None and origin.username is None and origin.password is None
         and origin.path in {"", "/"} and not origin.query and not origin.fragment,
         "base_url_must_be_explicit_loopback_http")
    value["base_url"] = value["base_url"].rstrip("/")
    controller = Path(value["control_script"])
    need(controller.is_absolute() and controller.is_file() and os.access(controller, os.X_OK),
         "controller_must_be_an_absolute_executable")
    control_stat = controller.stat()
    need(control_stat.st_uid == 0 and control_stat.st_mode & 0o022 == 0,
         "controller_must_be_root_owned_without_group_or_other_write")
    output = Path(value["output_dir"])
    need(output.is_absolute() and output.is_dir(), "output_directory_must_exist_and_be_absolute")
    roots = []
    for key in ("positive_tv_root", "negative_tv_root", "disabled_tv_root"):
        root = PurePosixPath(value[key])
        need(root.is_absolute() and ".." not in root.parts, "invalid_container_library_root")
        roots.append(root)
    need(all(not left.is_relative_to(right) and not right.is_relative_to(left)
             for index, left in enumerate(roots) for right in roots[index + 1:]),
         "fixture_roots_must_not_overlap")
    for key, default in (("expected_positive_episodes", 3), ("expected_negative_episodes", 2),
                         ("expected_disabled_episodes", 3)):
        value.setdefault(key, default)
        need(type(value[key]) is int and 1 <= value[key] <= 25, "invalid_fixture_population")
    value.setdefault("timeout_seconds", 600)
    value.setdefault("quiet_seconds", 3)
    value.setdefault("allow_existing_admin", False)
    need(type(value["timeout_seconds"]) is int and 30 <= value["timeout_seconds"] <= 3600,
         "invalid_timeout_seconds")
    need(type(value["quiet_seconds"]) is int and 1 <= value["quiet_seconds"] <= 15,
         "invalid_quiet_seconds")
    need(type(value["allow_existing_admin"]) is bool, "invalid_existing_admin_flag")
    return value


class IntroAutomation(oci.Delivery):
    def __init__(self, config):
        super().__init__(config)
        self.checkpoint_path = self.output / "intro-automation-state.json"
        self.state = None
        self.intro_task_id = None
        self.last_progress = None

    def raw_request(self, method, path, body=None, admin=False, public=False, timeout=10):
        if method != "GET":
            route = path.split("?", 1)[0]
            need(not route.startswith(("/admin/v1/media-analysis", "/admin/v1/tasks/", "/admin/v1/task-runs/"))
                 and not route.endswith(("/intro", "/decision")), "manual_analysis_write_is_prohibited")
        return super().raw_request(method, path, body, admin, public, timeout)

    def safe_text(self, value):
        text = str(value or "")
        for secret in (self.config["admin_password"], self.config["setup_token"], self.token, self.csrf):
            if secret:
                text = text.replace(secret, "[redacted]")
        return "".join(character if character.isprintable() else " " for character in text)[:512]

    def control(self, operation):
        need(operation in {"start", "recreate", "stop", "status"}, "unsupported_control_operation")
        try:
            result = subprocess.run([self.config["control_script"], operation], stdin=subprocess.DEVNULL,
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=self.timeout, check=False)
        except subprocess.TimeoutExpired:
            raise Failure("control_timeout_" + operation) from None
        need(result.returncode == 0, "control_failed_" + operation)
        if operation != "status":
            emit("container_control", phase=self.phase, operation=operation)
            return None
        need(len(result.stdout) <= 65536, "control_status_too_large")
        state = json.loads(result.stdout)
        need(type(state) is dict and type(state.get("running")) is bool
             and type(state.get("pid")) is int and state["pid"] >= 0, "invalid_control_status")
        need((state["running"] and state["pid"] > 0) or (not state["running"] and state["pid"] == 0),
             "inconsistent_control_process_status")
        return state

    def ready(self):
        def check():
            code, _ = self.raw_request("GET", "/readyz", timeout=3)
            state = self.control("status")
            return state if code == 200 and state["running"] else None
        state = self.wait("owned_application_ready", check)
        emit("application_ready", phase=self.phase, pid=state["pid"])
        return state

    def save_state(self):
        temporary = self.checkpoint_path.with_suffix(".tmp")
        descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, "w", encoding="utf-8") as output:
            json.dump(self.state, output, sort_keys=True)
            output.write("\n")
        os.replace(temporary, self.checkpoint_path)

    def load_state(self):
        metadata = self.checkpoint_path.lstat()
        need(stat.S_ISREG(metadata.st_mode) and metadata.st_mode & 0o077 == 0
             and 0 < metadata.st_size <= 1 << 20, "checkpoint_must_be_a_bounded_private_regular_file")
        self.state = json.loads(self.checkpoint_path.read_bytes())
        need(self.state["version"] == 1 and self.state["base_url"] == self.config["base_url"]
             and self.state["roots"] == self.fixture_roots(), "checkpoint_does_not_match_owned_fixture")
        self.user_id = self.state["user_id"]
        self.intro_task_id = self.state["task_id"]
        self.completed = list(self.state["completed"])

    def fixture_roots(self):
        return {kind: self.config[kind + "_tv_root"] for kind in ("positive", "negative", "disabled")}

    def task_definition(self):
        items = self.request("GET", "/admin/v1/tasks", admin=True)["Items"]
        matches = [item for item in items if item["Key"] == "media.intro_analysis"]
        need(len(matches) == 1 and matches[0]["Enabled"] is True, "automatic_intro_task_is_missing_or_disabled")
        self.intro_task_id = matches[0]["Id"]
        return matches[0]

    def runs(self):
        page = self.request("GET", "/admin/v1/tasks/" + self.intro_task_id + "/runs?Limit=200", admin=True)
        need(page["TotalRecordCount"] == len(page["Items"]), "owned_task_history_exceeds_bounded_acceptance")
        return page["Items"]

    def run_detail(self, run_id):
        value = self.request("GET", "/admin/v1/task-runs/" + run_id + "?Limit=200", admin=True)
        need(value["Children"]["TotalRecordCount"] == len(value["Children"]["Items"]),
             "automatic_run_children_are_truncated")
        return value

    def wait_automatic_run(self, previous):
        self.last_progress = None

        def check():
            fresh = [run for run in self.runs() if run["Id"] not in previous]
            for summary in sorted(fresh, key=lambda value: (value["CreatedAt"], value["Id"])):
                detail = self.run_detail(summary["Id"])
                run, children = detail["Run"], detail["Children"]["Items"]
                need(run["Source"] == "system_event", "intro_run_was_not_triggered_by_library_event")
                need(all(child["LibraryId"] == self.state["libraries"]["enabled"] for child in children),
                     "automatic_intro_run_touched_a_disabled_library")
                progress = (run["Id"], run["State"], run["TerminalChildren"], run["TotalChildren"])
                if progress != self.last_progress:
                    emit("automatic_run_progress", phase=self.phase, run_id=run["Id"], state=run["State"],
                         terminal_children=run["TerminalChildren"], total_children=run["TotalChildren"])
                    self.last_progress = progress
                bad = [entry for entry in [run, *children]
                       if entry["State"] in {"failed", "unavailable", "cancelled", "interrupted"}]
                if bad:
                    readable = all(entry.get("ErrorCode") and entry.get("ErrorMessage") for entry in bad)
                    emit("automatic_run_failure", phase=self.phase, run_id=run["Id"],
                         task_error_readable=readable, treated_as_no_match=False,
                         outcomes=[{"id": entry["Id"], "state": entry["State"],
                                    "code": self.safe_text(entry.get("ErrorCode")),
                                    "message": self.safe_text(entry.get("ErrorMessage"))} for entry in bad])
                    need(readable, "failed_background_work_has_no_readable_task_error")
                    raise Failure("automatic_intro_work_failed")
                if run["State"] in {"pending", "running", "stopping"}:
                    continue
                need(run["State"] == "completed" and children and not run["ErrorCode"]
                     and not run["ErrorMessage"] and all(child["State"] == "completed"
                     and not child["ErrorCode"] and not child["ErrorMessage"] for child in children),
                     "automatic_run_did_not_complete_successfully")
                emit("automatic_run_completed", phase=self.phase, run_id=run["Id"],
                     source=run["Source"], children=len(children), enabled_library_only=True)
                return run["Id"]
            return None
        return self.wait("new_successful_automatic_intro_run", check)

    def library(self, kind):
        return self.request("GET", "/admin/v1/libraries/" + self.state["libraries"][kind], admin=True)["Library"]

    def set_enabled(self, enabled):
        current = self.library("enabled")
        need(current["LibraryOptions"]["EnableIntroDetection"] is not enabled, "library_option_already_has_target_value")
        updated = self.request("PATCH", "/admin/v1/libraries/" + current["Id"], {
            "Revision": current["Revision"], "LibraryOptions": {"EnableIntroDetection": enabled}}, admin=True)["Library"]
        need(updated["LibraryOptions"]["EnableIntroDetection"] is enabled, "library_option_was_not_saved")
        emit("library_intro_option", phase=self.phase, enabled=enabled, library_id=current["Id"])

    def episodes(self, kind):
        library_kind = "disabled" if kind == "disabled" else "enabled"
        query = urllib.parse.urlencode({"ParentId": self.state["libraries"][library_kind], "Recursive": "true",
                                        "IncludeItemTypes": "Episode", "Fields": "Path", "Limit": 100})
        page = self.request("GET", "/emby/Items?" + query, public=True)
        need(page["TotalRecordCount"] == len(page["Items"]), "fixture_catalog_is_truncated")
        root = PurePosixPath(self.config[kind + "_tv_root"])
        values = {item["Id"]: item["Path"] for item in page["Items"]
                  if PurePosixPath(item.get("Path", "")).is_relative_to(root)}
        need(len(values) == self.config["expected_" + kind + "_episodes"], "fixture_episode_count_changed_" + kind)
        return values

    def assert_catalog(self):
        for kind, expected in self.state["episodes"].items():
            need(self.episodes(kind) == expected, "fixture_episode_identity_changed_" + kind)

    def detection(self, item_id):
        result = self.request("GET", "/admin/v1/media-analysis/items/" + item_id, admin=True)["Detection"]
        need(result["ManualRevision"] == "0", "fixture_was_modified_by_manual_intro_decision")
        return result

    def playback_markers(self, item_id):
        value = self.request("POST", "/emby/Items/" + item_id + "/PlaybackInfo", {
            "EnableDirectPlay": True, "EnableDirectStream": False, "EnableTranscoding": False,
            "IsPlayback": False}, public=True)
        need(not value.get("ErrorCode") and len(value["MediaSources"]) == 1,
             "fixture_playback_info_is_unavailable")
        source = value["MediaSources"][0]
        need(source["SupportsDirectPlay"] is True, "fixture_direct_play_is_unavailable")
        chapters = source.get("Chapters", [])
        markers = [chapter for chapter in chapters if chapter.get("MarkerType") in {"IntroStart", "IntroEnd"}]
        if not markers:
            return None
        starts = [chapter["StartPositionTicks"] for chapter in markers if chapter["MarkerType"] == "IntroStart"]
        ends = [chapter["StartPositionTicks"] for chapter in markers if chapter["MarkerType"] == "IntroEnd"]
        need(len(markers) == 2 and len(starts) == 1 and len(ends) == 1
             and type(starts[0]) is int and type(ends[0]) is int
             and 0 <= starts[0] < ends[0] <= source["RunTimeTicks"], "playback_intro_interval_is_invalid")
        return {"StartTicks": starts[0], "EndTicks": ends[0]}

    def assert_no_markers(self, kinds):
        for kind in kinds:
            for item_id in self.state["episodes"][kind]:
                need(self.playback_markers(item_id) is None, "unexpected_intro_marker_" + kind)
                need(self.detection(item_id)["Effective"] is None, "unexpected_effective_intro_" + kind)

    def assert_results(self):
        self.assert_catalog()
        qualified = []
        for kind in ("positive", "negative"):
            for item_id in self.state["episodes"][kind]:
                result = self.detection(item_id)
                need(result["Status"] in {"qualified", "review", "no_result"}, "analysis_result_is_not_terminal")
                interval = self.playback_markers(item_id)
                if kind == "positive" and result["Status"] == "qualified":
                    effective = result["Effective"]
                    need(type(effective) is dict and effective["Provenance"] == "Detected"
                         and interval == {key: effective[key] for key in ("StartTicks", "EndTicks")},
                         "qualified_intro_was_not_automatically_published_to_playback")
                    qualified.append(item_id)
                else:
                    need(result["Status"] != "qualified" and result["Effective"] is None and interval is None,
                         "unqualified_or_negative_fixture_was_published")
                    need(type(result["Reasons"]) is list and result["Reasons"], "unqualified_result_has_no_reason")
                emit("intro_outcome", phase=self.phase, fixture=kind, item_id=item_id,
                     status=result["Status"], reasons=[self.safe_text(reason) for reason in result["Reasons"]],
                     playback_markers=interval, manual_decision=False)
        need(qualified, "accepted_positive_fixture_produced_no_qualified_playback_intro")
        self.assert_no_markers(("disabled",))
        need(self.library("disabled")["LibraryOptions"]["EnableIntroDetection"] is False,
             "disabled_library_was_enabled")
        emit("automatic_publication_verified", phase=self.phase, qualified_items=len(qualified),
             negative_items=len(self.state["episodes"]["negative"]), disabled_library_markers=0,
             analysis_start_requests=0, manual_decision_requests=0, broader_accuracy_claim=False)

    def assert_no_new_runs(self, before):
        deadline = time.monotonic() + self.config["quiet_seconds"]
        while True:
            need({run["Id"] for run in self.runs()} == before, "disabled_library_triggered_intro_analysis")
            if time.monotonic() >= deadline:
                return
            time.sleep(0.25)

    def enable_phase(self):
        need(not self.checkpoint_path.exists(), "enable_phase_requires_a_new_checkpoint")
        bootstrap = self.request("GET", "/admin/v1/bootstrap")
        if bootstrap["Initialized"]:
            need(self.config["allow_existing_admin"], "fresh_admin_required_unless_explicitly_selected")
        else:
            self.request("POST", "/admin/v1/bootstrap", {"SetupToken": self.config["setup_token"],
                "Name": self.config["admin_name"], "Password": self.config["admin_password"]}, expected=201)
        self.authenticate()
        need(self.request("GET", "/admin/v1/libraries", admin=True)["TotalRecordCount"] == 0,
             "acceptance_requires_no_existing_libraries")
        self.task_definition()
        overview = self.request("GET", "/admin/v1/media-analysis", admin=True)
        need(overview["Runtime"]["IntroAvailable"] is True, "real_intro_runtime_is_unavailable")
        need(overview["Configuration"]["Profile"]["AutoPublishIntros"] is True,
             "automatic_publication_is_disabled_in_provisioned_profile")
        before = {run["Id"] for run in self.runs()}
        self.state = {"version": 1, "base_url": self.config["base_url"], "roots": self.fixture_roots(),
                      "user_id": self.user_id, "task_id": self.intro_task_id,
                      "libraries": {}, "episodes": {}, "run_ids": [], "completed": []}
        for kind, roots in (("enabled", [self.config["positive_tv_root"], self.config["negative_tv_root"]]),
                            ("disabled", [self.config["disabled_tv_root"]])):
            created = self.request("POST", "/admin/v1/libraries", {"Name": "Intro automation " + kind,
                "CollectionType": "tvshows", "Paths": roots, "Scan": True,
                "LibraryOptions": {"EnableIntroDetection": False}}, expected=201, admin=True)
            self.state["libraries"][kind] = created["Library"]["Id"]
            self.scan_complete(created["Job"]["Id"])
        self.state["episodes"] = {kind: self.episodes(kind) for kind in ("positive", "negative", "disabled")}
        self.assert_no_markers(("positive", "negative", "disabled"))
        self.assert_no_new_runs(before)
        self.set_enabled(True)
        self.state["run_ids"].append(self.wait_automatic_run(before))
        self.assert_results()

    def rescan_phase(self):
        need("enable" in self.completed and "disable" not in self.completed, "rescan_requires_enabled_checkpoint")
        need(self.library("enabled")["LibraryOptions"]["EnableIntroDetection"] is True,
             "enabled_library_option_was_not_persisted")
        self.assert_catalog()
        before = {run["Id"] for run in self.runs()}
        scan = self.request("POST", "/admin/v1/libraries/" + self.state["libraries"]["enabled"] + "/scan",
                            {"ForceProbe": True}, expected=202, admin=True)
        self.scan_complete(scan["Job"]["Id"])
        self.state["run_ids"].append(self.wait_automatic_run(before))
        self.assert_results()

    def disable_phase(self):
        need("enable" in self.completed, "disable_requires_enabled_checkpoint")
        self.set_enabled(False)
        self.assert_no_markers(("positive", "negative", "disabled"))
        before = {run["Id"] for run in self.runs()}
        previous = self.control("status")
        self.control("recreate")
        current = self.ready()
        need(current["pid"] != previous["pid"], "application_was_not_recreated")
        self.authenticate()
        for kind in ("enabled", "disabled"):
            need(self.library(kind)["LibraryOptions"]["EnableIntroDetection"] is False,
                 "disabled_library_option_was_not_persisted")
        self.assert_catalog()
        self.assert_no_markers(("positive", "negative", "disabled"))
        self.assert_no_new_runs(before)
        emit("disable_persistence_verified", phase=self.phase, markers_after_disable=0,
             markers_after_recreation=0, library_options_persisted=True)

    def execute(self, selected):
        if selected != "enable" and selected != "all":
            self.load_state()
        self.control("start")
        self.ready()
        if self.state is not None:
            self.authenticate()
            definition = self.task_definition()
            need(definition["Id"] == self.state["task_id"], "checkpoint_task_identity_changed")
        for phase in PHASES if selected == "all" else (selected,):
            self.phase = phase
            need(phase not in self.completed, "phase_already_completed")
            getattr(self, phase + "_phase")()
            self.completed.append(phase)
            self.state["completed"] = list(self.completed)
            self.save_state()
            emit("phase_completed", phase=phase)

    def close(self):
        self.phase = "closure"
        self.control("stop")
        state = self.control("status")
        need(not state["running"] and state["pid"] == 0 and not state.get("process_pids"),
             "owned_application_processes_remain")
        code, _ = self.raw_request("GET", "/readyz", timeout=2)
        need(code == 0, "owned_http_listener_remains_open")
        emit("closed", application_running=False, http_listener_closed=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True, help="Absolute path to private acceptance JSON")
    parser.add_argument("--phase", choices=("all", *PHASES), default="all")
    arguments = parser.parse_args()
    driver, failure, failed_phase = None, None, None

    def interrupted(_signum, _frame):
        raise Failure("driver_interrupted")

    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        driver = IntroAutomation(load_config(arguments.config))
        driver.execute(arguments.phase)
    except Exception as error:
        failure = str(error) if isinstance(error, Failure) else "unexpected_" + type(error).__name__
        failed_phase = None if driver is None else driver.phase
    finally:
        if driver is not None:
            try:
                driver.close()
            except Exception as error:
                emit("closure_failed", reason=str(error) if isinstance(error, Failure)
                     else "unexpected_" + type(error).__name__)
                failure, failed_phase = failure or "closure_failed", failed_phase or "closure"
    emit("result", passed=failure is None, failure=failure, failed_phase=failed_phase,
         selected_phase=arguments.phase, completed=[] if driver is None else driver.completed,
         scope="real_library_option_automatic_intro_playback_and_disabled_negative_boundaries",
         injected_failure_case=False, broader_accuracy_claim=False)
    return 0 if failure is None else 1


if __name__ == "__main__":
    sys.exit(main())
