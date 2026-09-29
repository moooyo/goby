#!/usr/bin/env python3
"""Exercise one explicitly provisioned cmd/goby deployment and its supervisor.

Required private JSON: base_url, control_script, media_root, sample_path,
setup_token, admin_name, admin_password. Optional: timeout_seconds (180), and
baseline_path pointing to a private catalog/UserData/settings checkpoint.
The control executable accepts start, stop, kill TERM, kill KILL, pg-stop,
pg-start, and status. It owns the fixed unit and private PostgreSQL cluster.
This driver never installs services, constructs shell commands, or reads secrets
from ambient environment variables. Output contains only acceptance evidence.
"""

import argparse
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import signal
import stat
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid


class Failure(Exception):
    pass


def need(condition, message):
    if not condition:
        raise Failure(message)


def emit(event, **values):
    print(json.dumps({"event": event, **values}, sort_keys=True), flush=True)


def load_config(path):
    path = Path(path)
    need(path.is_absolute(), "config_path_must_be_absolute")
    metadata = path.lstat()
    need(stat.S_ISREG(metadata.st_mode) and metadata.st_mode & 0o077 == 0,
         "config_must_be_a_private_regular_file")
    need(metadata.st_size <= 16384, "config_is_too_large")
    value = json.loads(path.read_text(encoding="utf-8"))
    required = {"base_url", "control_script", "media_root", "sample_path",
                "setup_token", "admin_name", "admin_password"}
    need(type(value) is dict and required <= value.keys()
         and value.keys() <= required | {"timeout_seconds", "baseline_path"}, "invalid_config_fields")
    need(all(type(value[key]) is str and value[key] for key in required),
         "config_fields_must_be_nonempty_strings")
    origin = urllib.parse.urlsplit(value["base_url"])
    need(origin.scheme == "http" and origin.hostname in {"127.0.0.1", "::1", "localhost"}
         and origin.port is not None and origin.username is None and origin.password is None
         and origin.path in {"", "/"} and not origin.query and not origin.fragment,
         "base_url_must_be_one_explicit_loopback_origin")
    value["base_url"] = value["base_url"].rstrip("/")
    for key in ("control_script", "media_root", "sample_path"):
        need(Path(value[key]).is_absolute(), key + "_must_be_absolute")
    control = Path(value["control_script"])
    need(control.is_file() and os.access(control, os.X_OK), "control_script_is_not_executable")
    root, sample = Path(value["media_root"]).resolve(), Path(value["sample_path"]).resolve()
    need(root.is_dir() and sample.is_relative_to(root), "sample_is_outside_fixture_media")
    files = sorted(root.glob("*.mp4"))
    need(len(files) == 20 and sample in [entry.resolve() for entry in files],
         "fixture_requires_twenty_flat_mp4_files_and_one_existing_sample")
    need(0 < sample.stat().st_size < 32 << 20, "sample_exceeds_bounded_http_profile")
    timeout = value.get("timeout_seconds", 180)
    need(type(timeout) in {int, float} and 30 <= timeout <= 1800, "invalid_timeout_seconds")
    value["timeout_seconds"] = timeout
    if "baseline_path" in value:
        need(type(value["baseline_path"]) is str and value["baseline_path"], "invalid_baseline_path")
        baseline_path = Path(value["baseline_path"])
        need(baseline_path.is_absolute(), "baseline_path_must_be_absolute")
        metadata = baseline_path.lstat()
        need(stat.S_ISREG(metadata.st_mode) and stat.S_IMODE(metadata.st_mode) == 0o600,
             "baseline_must_be_a_0600_regular_file")
        need(0 < metadata.st_size <= 1 << 20, "baseline_size_is_invalid")
        checkpoint = json.loads(baseline_path.read_text(encoding="utf-8"))
        need(type(checkpoint) is dict and checkpoint.keys() == {"catalog", "userdata", "settings"},
             "invalid_baseline_fields")
        catalog = checkpoint["catalog"]
        need(type(catalog) is dict and len(catalog) == 20
             and all(type(name) is str and name and type(item_id) is str and item_id
                     for name, item_id in catalog.items()) and len(set(catalog.values())) == 20,
             "invalid_baseline_catalog")
        need(type(checkpoint["userdata"]) is dict and checkpoint["userdata"]
             and type(checkpoint["settings"]) is dict and checkpoint["settings"],
             "invalid_baseline_state")
        value["_baseline"] = checkpoint
    return value


class Driver:
    def __init__(self, config):
        self.config = config
        self.timeout = config["timeout_seconds"]
        self.run_id = uuid.uuid4().hex[:12]
        self.cookies = http.cookiejar.CookieJar()
        self.http = urllib.request.build_opener(
            urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(self.cookies))
        self.csrf = None
        self.token = None
        self.phase = "initialization"
        self.library_id = None
        self.user_id = None
        self.sample_id = None
        self.baseline = None
        self.user_data = None
        self.settings = None
        self.completed = []

    def control(self, operation, *arguments):
        permitted = {("start",), ("stop",), ("kill", "TERM"), ("kill", "KILL"),
                     ("pg-stop",), ("pg-start",), ("status",)}
        need((operation, *arguments) in permitted, "unsupported_control_operation")
        try:
            result = subprocess.run([self.config["control_script"], operation, *arguments],
                                    stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                    stderr=subprocess.PIPE, timeout=self.timeout, check=False)
        except subprocess.TimeoutExpired:
            raise Failure("control_timeout_" + operation) from None
        need(result.returncode == 0, "control_failed_" + operation)
        if operation == "status":
            need(len(result.stdout) <= 65536, "control_status_too_large")
            value = json.loads(result.stdout)
            fields = {"active_state", "sub_state", "main_pid", "n_restarts", "exec_main_pid",
                      "exec_main_code", "exec_main_status", "invocation_id", "cgroup_pids", "pg_running"}
            need(type(value) is dict and fields <= value.keys(), "invalid_control_status")
            for field in ("main_pid", "n_restarts", "exec_main_pid", "exec_main_status"):
                need(type(value[field]) is int and value[field] >= 0, "invalid_control_status_number")
            need(type(value["cgroup_pids"]) is list and all(type(pid) is int and pid > 0
                 for pid in value["cgroup_pids"]) and type(value["pg_running"]) is bool,
                 "invalid_control_process_inventory")
            return value
        emit("control", phase=self.phase, operation=operation,
             signal=arguments[0] if arguments else None)
        return None

    def raw_request(self, method, path, body=None, admin=False, public=False, timeout=10):
        need(path.startswith("/") and not path.startswith("//"), "invalid_http_route")
        headers = {"Origin": self.config["base_url"]}
        if admin and method != "GET" and self.csrf:
            headers["X-CSRF-Token"] = self.csrf
        if public:
            need(self.token is not None, "public_session_is_missing")
            headers["X-Emby-Token"] = self.token
        if path == "/emby/Users/AuthenticateByName":
            headers["Authorization"] = ('Emby Client="CLIRecovery", DeviceId="phase3-cli-recovery", '
                                        'Device="Linux", Version="1.0"')
        data = None
        if body is not None:
            headers["Content-Type"] = "application/json"
            data = json.dumps(body, separators=(",", ":")).encode()
        request = urllib.request.Request(self.config["base_url"] + path,
                                         data=data, headers=headers, method=method)
        try:
            response = self.http.open(request, timeout=timeout)
        except urllib.error.HTTPError as error:
            response = error
        except (urllib.error.URLError, TimeoutError, OSError):
            return 0, b""
        with response:
            content = response.read((32 << 20) + 1)
            need(len(content) <= 32 << 20, "http_response_exceeds_fixture_limit")
            return response.status, content

    def request(self, method, path, body=None, expected=200, admin=False, public=False, binary=False):
        status, content = self.raw_request(method, path, body, admin, public)
        if status != expected:
            emit("http_failure", phase=self.phase, method=method, route=path.split("?", 1)[0],
                 status=status, expected=expected)
        need(status == expected, "http_status_" + str(status) + "_expected_" + str(expected))
        if binary:
            return content
        try:
            value = json.loads(content)
        except (ValueError, UnicodeError):
            raise Failure("http_response_is_not_json") from None
        need(type(value) is dict, "http_response_is_not_an_object")
        return value

    def wait(self, label, predicate, timeout=None):
        deadline = time.monotonic() + (self.timeout if timeout is None else timeout)
        while True:
            result = predicate()
            if result:
                return result
            need(time.monotonic() < deadline, "deadline_" + label)
            time.sleep(0.2)

    def ready(self, previous=None):
        def check():
            status = self.control("status")
            if status["active_state"] != "active" or status["main_pid"] <= 0:
                return None
            if previous is not None:
                if status["main_pid"] == previous["main_pid"]:
                    return None
                need(status["invocation_id"] != previous["invocation_id"], "restarted_invocation_did_not_change")
                need(status["n_restarts"] > previous["n_restarts"], "supervisor_restart_not_observed")
            code, _ = self.raw_request("GET", "/readyz", timeout=3)
            return status if code == 200 else None
        state = self.wait("application_ready", check)
        emit("ready", phase=self.phase, pid=state["main_pid"], restarts=state["n_restarts"])
        return state

    def observe_exit(self, previous, expected_code, expected_status):
        def matches(entry):
            aliases = {1: {1, "exited"}, 2: {2, "killed"}}
            return (entry.get("pid") == previous["main_pid"]
                    and entry.get("code") in aliases[expected_code]
                    and entry.get("status") == expected_status)

        def check():
            status = self.control("status")
            record = {"pid": status["exec_main_pid"], "code": status["exec_main_code"],
                      "status": status["exec_main_status"]}
            records = [record, *status.get("recent_exits", [])]
            if status["main_pid"] != previous["main_pid"] and any(matches(entry) for entry in records):
                return status
            return None
        state = self.wait("expected_process_exit", check)
        emit("process_exit", phase=self.phase, pid=previous["main_pid"],
             code=expected_code, status=expected_status)
        return state

    def scan_job(self, job_id):
        page = self.request("GET", "/admin/v1/jobs", admin=True)
        jobs = [job for job in page["Items"] if job["Id"] == job_id]
        need(len(jobs) == 1, "scan_job_missing_or_duplicated")
        return jobs[0]

    def start_scan(self):
        result = self.request("POST", "/admin/v1/libraries/" + self.library_id + "/scan",
                              {"ForceProbe": True}, expected=202, admin=True)
        return result["Job"]["Id"]

    def finish_scan(self, job_id):
        def check():
            job = self.scan_job(job_id)
            if job["Status"] in {"pending", "running"}:
                return False
            need(job["Status"] == "completed" and not job["Error"] and job["Scanned"] == 20,
                 "scan_did_not_complete_exact_population")
            return True
        self.wait("scan_completion", check)

    def preview_run(self, run_id):
        value = self.request("GET", "/admin/v1/task-runs/" + run_id + "?StartIndex=0&Limit=100", admin=True)
        children = value["Children"]
        need(children["Items"] and children["TotalRecordCount"] == len(children["Items"]),
             "preview_children_are_missing_or_truncated")
        return value

    def start_preview(self, ids, label):
        # These are the exact source selectors used by the passing real-media
        # Go fixtures. Do not add unsupported Filters or playback query fields.
        result = self.request("POST", "/admin/v1/media-analysis/runs", {
            "Kind": "previews", "RequestId": "cli-" + self.run_id + "-" + label,
            "LibraryIds": [], "ItemIds": ids, "Force": True,
        }, expected=202, admin=True)
        return result["RunId"]

    def active_work(self, label):
        scan_id = self.start_scan()

        def running():
            state = self.scan_job(scan_id)["Status"]
            need(state in {"pending", "running"}, "scan_finished_before_preview_admission")
            return state == "running"
        self.wait("scan_running", running)
        run_id = self.start_preview(list(self.baseline.values()), label)

        def check():
            scan, task = self.scan_job(scan_id), self.preview_run(run_id)
            need(scan["Status"] in {"pending", "running"}, "scan_finished_before_active_fault_window")
            need(task["Run"]["State"] in {"pending", "running"}, "preview_finished_before_active_fault_window")
            state = self.control("status")
            running_children = [child["Id"] for child in task["Children"]["Items"] if child["State"] == "running"]
            media_pids = [pid for pid in state["cgroup_pids"] if pid != state["main_pid"]]
            if scan["Status"] == "running" and scan["Scanned"] < 17 and running_children and media_pids:
                emit("active_fault_window", phase=self.phase, pid=state["main_pid"], scan_id=scan_id,
                     scan_scanned=scan["Scanned"], task_run_id=run_id,
                     running_task_children=len(running_children), media_child_pids=media_pids)
                return state
            return None
        return scan_id, run_id, self.wait("active_scan_preview_and_media_process", check)

    def catalog(self):
        query = urllib.parse.urlencode({"ParentId": self.library_id, "Recursive": "true",
                                        "IncludeItemTypes": "Movie", "SortBy": "SortName", "Limit": 100})
        page = self.request("GET", "/emby/Items?" + query, public=True)
        need(page["TotalRecordCount"] == 20 and len(page["Items"]) == 20, "catalog_population_changed")
        result = {item["Name"]: item["Id"] for item in page["Items"]}
        need(len(result) == 20 and len(set(result.values())) == 20, "catalog_identity_not_unique")
        return result

    def user_data_path(self):
        return "/emby/Users/" + self.user_id + "/Items/" + self.sample_id + "/UserData"

    def assert_durable(self):
        need(self.catalog() == self.baseline, "confirmed_catalog_ids_changed")
        need(self.request("GET", self.user_data_path(), public=True) == self.user_data,
             "confirmed_userdata_changed")
        current = self.request("GET", "/admin/v1/settings", admin=True)
        need(current["Revision"] == self.settings["Revision"]
             and current["Overrides"] == self.settings["Overrides"]
             and current["Effective"]["ServerName"] == "CLI supervisor recovery",
             "confirmed_settings_changed")

    def assert_interrupted(self, scan_id, run_id):
        need(self.scan_job(scan_id)["Status"] == "interrupted", "abandoned_scan_not_interrupted")
        task = self.preview_run(run_id)
        children = task["Children"]["Items"]
        need(task["Run"]["State"] == "interrupted"
             and any(child["State"] == "interrupted" for child in children)
             and all(child["State"] in {"completed", "interrupted"} for child in children),
             "abandoned_preview_not_interrupted")

    def assert_working(self, label):
        self.finish_scan(self.start_scan())
        self.assert_durable()
        actual = self.request("GET", "/emby/Videos/" + self.sample_id + "/stream.mp4?Static=true",
                              public=True, binary=True)
        expected = Path(self.config["sample_path"]).read_bytes()
        need(actual == expected, "recovered_direct_media_bytes_changed")
        run_id = self.start_preview([self.sample_id], label + "-fresh")

        def check():
            task = self.preview_run(run_id)
            if task["Run"]["State"] in {"pending", "running"}:
                return False
            need(task["Run"]["State"] == "completed"
                 and all(child["State"] == "completed" and not child["ErrorCode"]
                         for child in task["Children"]["Items"]), "recovered_preview_execution_failed")
            return True
        self.wait("new_preview_completion", check)
        bif = self.request("GET", "/emby/Videos/" + self.sample_id + "/index.bif?Width=240", public=True, binary=True)
        need(len(bif) >= 80 and bif[:8] == b"\x89BIF\r\n\x1a\n", "recovered_bif_not_delivered")
        emit("recovery_verified", phase=self.phase, media_count=20, catalog_ids=True, userdata=True,
             settings=True, new_scan=True, direct_media_bytes=len(actual),
             direct_media_sha256=hashlib.sha256(actual).hexdigest(), new_preview=True, bif_bytes=len(bif))

    def initialize(self):
        bootstrap = self.request("GET", "/admin/v1/bootstrap")
        checkpoint = self.config.get("_baseline")
        if checkpoint is None:
            need(bootstrap["Initialized"] is False, "a_fresh_private_database_is_required")
            created = self.request("POST", "/admin/v1/bootstrap", {
                "SetupToken": self.config["setup_token"], "Name": self.config["admin_name"],
                "Password": self.config["admin_password"],
            }, expected=201)
            self.user_id = created["User"]["Id"]
        else:
            need(bootstrap["Initialized"] is True, "checkpoint_requires_initialized_private_database")
        login = self.request("POST", "/admin/v1/session", {
            "Name": self.config["admin_name"], "Password": self.config["admin_password"],
        })
        self.csrf = login["CSRFToken"]
        login = self.request("POST", "/emby/Users/AuthenticateByName", {
            "Username": self.config["admin_name"], "Pw": self.config["admin_password"],
        })
        self.token = login["AccessToken"]
        authenticated_user = login["User"]["Id"]
        need(self.user_id is None or self.user_id == authenticated_user, "authenticated_user_changed")
        self.user_id = authenticated_user
        availability = self.request("GET", "/admin/v1/media-analysis", admin=True)
        need(availability["Runtime"]["PreviewAvailable"] is True, "real_preview_runtime_is_unavailable")
        if checkpoint is not None:
            libraries = self.request("GET", "/admin/v1/libraries", admin=True)
            need(libraries["TotalRecordCount"] == 1 and len(libraries["Items"]) == 1,
                 "checkpoint_requires_exactly_one_existing_library")
            self.library_id = libraries["Items"][0]["Id"]
            self.baseline = checkpoint["catalog"]
            self.sample_id = self.baseline.get(Path(self.config["sample_path"]).stem)
            need(self.sample_id is not None, "checkpoint_does_not_identify_sample_movie")
            self.user_data, self.settings = checkpoint["userdata"], checkpoint["settings"]
            need(self.catalog() == self.baseline, "checkpoint_catalog_ids_changed")
            need(self.request("GET", self.user_data_path(), public=True) == self.user_data,
                 "checkpoint_userdata_changed")
            need(self.request("GET", "/admin/v1/settings", admin=True) == self.settings,
                 "checkpoint_settings_changed")
            emit("baseline_confirmed", media_count=20, catalog_ids=True, userdata=True,
                 settings=True, restored_checkpoint=True)
            return
        library = self.request("POST", "/admin/v1/libraries", {
            "Name": "CLI supervisor recovery", "CollectionType": "movies",
            "Paths": [self.config["media_root"]], "Scan": True,
        }, expected=201, admin=True)
        self.library_id = library["Library"]["Id"]
        self.finish_scan(library["Job"]["Id"])
        self.baseline = self.catalog()
        self.sample_id = self.baseline.get(Path(self.config["sample_path"]).stem)
        need(self.sample_id is not None, "sample_filename_did_not_match_one_catalog_movie")
        self.request("POST", self.user_data_path(), {
            "PlaybackPositionTicks": 5_000_000, "PlayCount": 7, "Played": False,
            "IsFavorite": True, "LastPlayedDate": "2026-09-01T03:04:05Z",
        }, public=True)
        self.user_data = self.request("GET", self.user_data_path(), public=True)
        need(self.user_data["PlaybackPositionTicks"] == 5_000_000 and self.user_data["PlayCount"] == 7
             and self.user_data["IsFavorite"] is True, "nondefault_userdata_not_persisted")
        current = self.request("GET", "/admin/v1/settings", admin=True)
        self.settings = self.request("PUT", "/admin/v1/settings", {
            "Revision": current["Revision"], "Overrides": {
                "ServerName": "CLI supervisor recovery", "MaxBitrate": None,
                "MaxWidth": None, "MaxHeight": None, "MaxAudioChannels": None,
            },
        }, admin=True)
        self.assert_durable()
        emit("baseline_confirmed", media_count=20, catalog_ids=True, userdata=True, settings=True)

    def execute(self):
        self.control("start")
        self.ready()
        self.initialize()

        self.phase = "normal_sigterm"
        previous = self.control("status")
        self.control("kill", "TERM")
        self.observe_exit(previous, 1, 0)

        def clean_exit():
            state = self.control("status")
            need(state["main_pid"] == 0 and state["n_restarts"] == previous["n_restarts"],
                 "normal_exit_was_automatically_restarted")
            return state["active_state"] == "inactive" and not state["cgroup_pids"]
        self.wait("normal_exit_cleanup", clean_exit)
        deadline = time.monotonic() + 3
        while time.monotonic() < deadline:
            state = self.control("status")
            need(state["main_pid"] == 0 and state["active_state"] == "inactive"
                 and state["n_restarts"] == previous["n_restarts"] and not state["cgroup_pids"],
                 "normal_exit_was_restarted_or_retained_processes")
            time.sleep(0.2)
        self.control("start")
        restarted = self.ready()
        need(restarted["main_pid"] != previous["main_pid"], "manual_restart_kept_the_old_pid")
        self.assert_durable()
        self.completed.append(self.phase)
        emit("normal_stop_verified", exit_status=0, automatic_restart=False, durable_state=True)

        self.phase = "sigkill_supervisor_restart"
        scan_id, run_id, previous = self.active_work("sigkill")
        self.control("kill", "KILL")
        self.observe_exit(previous, 2, 9)
        self.ready(previous)
        self.assert_durable()
        self.assert_interrupted(scan_id, run_id)
        self.assert_working("sigkill")
        self.completed.append(self.phase)

        self.phase = "postgres_lease_loss_supervisor_restart"
        scan_id, run_id, previous = self.active_work("postgres")
        self.control("pg-stop")
        need(self.control("status")["pg_running"] is False, "private_postgres_did_not_stop")
        # Keep this outage observation shorter than RestartSec, so it cannot
        # consume the old invocation's actual exit-status observation window.
        code, _ = self.raw_request("GET", "/emby/Items?Ids=" + self.sample_id, public=True, timeout=0.5)
        need(code != 200, "catalog_succeeded_while_postgres_was_stopped")
        emit("database_outage_observed", http_status=code)
        self.observe_exit(previous, 1, 1)
        # Only the supervisor may start the successor application in this case.
        self.control("pg-start")
        state = self.control("status")
        need(state["pg_running"] is True, "private_postgres_did_not_restart")
        if previous.get("pg_start_time") and state.get("pg_start_time"):
            need(previous["pg_start_time"] != state["pg_start_time"], "postgres_start_identity_did_not_change")
        self.ready(previous)
        self.assert_durable()
        self.assert_interrupted(scan_id, run_id)
        self.assert_working("postgres")
        self.completed.append(self.phase)

    def close(self):
        self.phase = "closure"
        self.control("stop")
        state = self.wait("unit_and_cgroup_closure", lambda: self.closed_status())
        if not state["pg_running"]:
            self.control("pg-start")
            state = self.control("status")
        need(state["main_pid"] == 0 and not state["cgroup_pids"] and state["pg_running"],
             "private_runtime_closure_incomplete")
        code, _ = self.raw_request("GET", "/readyz", timeout=2)
        need(code == 0, "owned_http_listener_remained_open_after_unit_stop")
        emit("closed", app_pid=0, cgroup_processes=0, http_listener_closed=True,
             private_postgres_running=True)

    def closed_status(self):
        state = self.control("status")
        return state if state["main_pid"] == 0 and not state["cgroup_pids"] else None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True, help="Absolute path to private deployment JSON")
    args = parser.parse_args()
    driver = None
    failure = None
    failed_phase = None

    def interrupted(_signum, _frame):
        raise Failure("driver_interrupted")

    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        driver = Driver(load_config(args.config))
        driver.execute()
    except Failure as error:
        failure = str(error)
        failed_phase = None if driver is None else driver.phase
    except Exception as error:
        failure = "unexpected_" + type(error).__name__
        failed_phase = None if driver is None else driver.phase
    finally:
        if driver is not None:
            try:
                driver.close()
            except Failure as error:
                emit("closure_failed", reason=str(error))
                failure = failure or "closure_failed"
            except Exception as error:
                emit("closure_failed", reason="unexpected_" + type(error).__name__)
                failure = failure or "closure_failed"
    emit("result", passed=failure is None, failure=failure, failed_phase=failed_phase,
         completed=[] if driver is None else driver.completed,
         scope="actual_cmd_goby_private_systemd_and_private_postgres; no_host_reboot_or_large_capacity_claim")
    return 0 if failure is None else 1


if __name__ == "__main__":
    sys.exit(main())
