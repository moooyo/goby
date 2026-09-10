#!/usr/bin/env python3
"""Observe real native daily and weekly firing in the frozen isolated fixture.

No browser, restart, reference capture, direct database mutation, or clock change
is performed. The frozen task runner supplies two real MP4 files, two libraries,
one native administrator, and complete credential/database/process cleanup.
"""

import argparse
from datetime import timedelta
import importlib.util
import json
import os
from pathlib import Path
import signal
import sys
import time
from types import MethodType


ACCEPTED_BINARY_SHA256 = "2993870cce6f4e0ae2c3630b645985cab664ce5e5e18123fdc920fb241bb2abf"
ACCEPTED_ASSETS_SHA256 = "b432bbaa8363016ab7a6de2e73b82765c7e01a913ae1f2788d7eccf0595cff76"
EXPECTED_MAIN_PID = "3570491"
TOTAL_SECONDS = 60
WORK_SECONDS = 40


def load_tasks(path):
    sys.dont_write_bytecode = True
    specification = importlib.util.spec_from_file_location("goby_calendar_fixture", path)
    if specification is None or specification.loader is None:
        raise RuntimeError("The frozen task fixture could not be loaded.")
    module = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(module)
    return module


def configure(core, tasks, args):
    runner = tasks.create_runner(core, args)
    runner.output = core.EXEC_ROOT / ("goby-task-calendars-" + runner.run_id)
    runner.report.update({"scenario": "native_calendar_firing", "browser_started": False,
                          "application_restarts": 0, "reference_records_added": 0,
                          "total_budget_seconds": TOTAL_SECONDS, "active_work_budget_seconds": WORK_SECONDS})
    runner.calendar_cleaning = False
    runner.calendar_started = False
    runner.calendar_deadline = time.monotonic() + WORK_SECONDS
    original_http = runner.http_json
    original_start = runner.start_app
    original_wait = runner.wait_no_work

    def allocate(self):
        # The historical browser runner protects its former main PID. Retain
        # core isolation checks, then pin this separate scenario to today's PID.
        core.Runner.allocate(self)
        core.require(self.report["shared_service_before"] == {"MainPID": EXPECTED_MAIN_PID, "ActiveState": "active"},
                     "The accepted main service identity changed; no service was modified.")
        core.require(self.goby.pw_uid == 995 and os.fstat(self.binary_fd).st_size == 22529996
                     and os.fstat(self.binary_fd).st_mode & 0o001
                     and self.args.binary_sha256 == ACCEPTED_BINARY_SHA256,
                     "The calendar fixture requires the accepted binary and UID 995.")
        core.require(self.args.assets.stat().st_size == 1000448
                     and core.file_digest(self.args.assets) == ACCEPTED_ASSETS_SHA256,
                     "The calendar fixture requires the accepted administrator asset archive.")
        self.report["calendar_verifier_sha256"] = core.file_digest(Path(__file__).resolve())
        self.report["tasks_runner_sha256"] = core.file_digest(args.tasks_runner.resolve())
        self.report["shared_runner_sha256"] = core.file_digest(args.core_runner.resolve())

    def http_json(self, route, **options):
        remaining = 5 if self.calendar_cleaning else self.calendar_deadline - time.monotonic()
        core.require(remaining > 0, "The native calendar work exceeded its bounded time budget.")
        options["timeout"] = min(options.get("timeout", 5), remaining)
        return original_http(route, **options)

    def start_once(self):
        core.require(not self.calendar_started, "This calendar scenario does not restart its application.")
        self.calendar_started = True
        return original_start()

    def wait_no_work(self, *, cancel=False, timeout=30):
        remaining = 5 if self.calendar_cleaning else self.calendar_deadline - time.monotonic()
        core.require(remaining > 0, "The calendar work did not settle within its time budget.")
        return original_wait(cancel=cancel, timeout=min(timeout, remaining))

    runner.allocate = MethodType(allocate, runner)
    runner.http_json = MethodType(http_json, runner)
    runner.start_app = MethodType(start_once, runner)
    runner.wait_no_work = MethodType(wait_no_work, runner)
    return runner


def runs(runner, core):
    value = runner.http_json(f"/admin/v1/tasks/{runner.task_id}/runs?StartIndex=0&Limit=200", administrator=True)
    items = value.get("Items")
    core.require(isinstance(items, list) and value.get("TotalRecordCount") == len(items) <= 2,
                 "The calendar fixture has an unexpected or unbounded run history.")
    return items


def observe_calendar(runner, core, tasks, kind):
    before = {item["Id"] for item in runs(runner, core)}
    definition = runner.task()
    core.require(not definition["Triggers"] and definition["CurrentRun"] is None,
                 "Each calendar check must begin without triggers or active work.")
    route = f"/admin/v1/tasks/{runner.task_id}/triggers"
    clock = runner.http_json(route + "/preview", administrator=True,
                             body={"ScheduleTimezone": "UTC", "Triggers": []})
    target = tasks.timestamp(clock["ServerTime"]) + timedelta(seconds=3)
    day_ticks = ((target.hour * 3600 + target.minute * 60 + target.second) * 10000000
                 + target.microsecond * 10)
    trigger = {"Kind": kind, "TimeOfDayTicks": str(day_ticks), "MaxRuntimeTicks": "100000000"}
    if kind == "weekly":
        trigger["DayOfWeek"] = (target.weekday() + 1) % 7
    body = {"ScheduleTimezone": "UTC", "Triggers": [trigger]}
    preview = runner.http_json(route + "/preview", administrator=True, body=body)
    occurrences = preview.get("Items", [{}])[0].get("Occurrences", [])
    core.require(len(occurrences) == 3 and tasks.timestamp(occurrences[0]) == target
                 and tasks.timestamp(preview["ServerTime"]) < target,
                 "The calendar target was not the imminent database-clock occurrence.")
    saved = runner.http_json(route, method="PUT", administrator=True,
                             body=dict(body, Revision=definition["Revision"]))["Task"]
    core.require(saved["Id"] == runner.task_id and len(saved["Triggers"]) == 1
                 and saved["Triggers"][0]["Kind"] == kind,
                 "The native calendar rule did not persist.")
    rule = saved["Triggers"][0]
    core.require(tasks.valid_id(rule.get("Id")) and str(saved["Revision"]).isdecimal()
                 and rule.get("CalculationError") == "" and tasks.timestamp(rule["NextFireAt"]) == target,
                 "Saving missed the imminent calendar occurrence; refusing to wait for a later day.")
    deadline = min(runner.calendar_deadline, time.monotonic() + 15)
    admitted = None
    while time.monotonic() < deadline:
        current = runs(runner, core)
        fresh = [item for item in current if item["Id"] not in before]
        core.require(len(fresh) <= 1, "One calendar occurrence admitted multiple executions.")
        if fresh and fresh[0]["State"] in tasks.TERMINAL_STATES:
            admitted = fresh[0]
            break
        time.sleep(0.1)
    core.require(admitted is not None and admitted["Source"] == "schedule" and admitted["State"] == "completed",
                 "The real calendar trigger did not complete a new scheduled run within its deadline.")
    runner.completed_run(admitted["Id"], source="schedule", cached=kind == "weekly")
    # Only validated native IDs enter this owned, read-only observation query.
    trigger_id = rule["Id"]
    observed = json.loads(runner.owned_database_read(f"""SELECT jsonb_build_object(
        'trigger', (SELECT jsonb_build_object('task_id', task_id, 'revision', schedule_revision,
            'last_due', last_due_at, 'next_fire', next_fire_at, 'error', calculation_error)
            FROM task_triggers WHERE id = '{trigger_id}'),
        'runs', (SELECT COALESCE(jsonb_agg(jsonb_build_object('id', id, 'task_id', task_id,
            'revision', trigger_revision, 'source', source, 'state', state, 'due', scheduled_for)), '[]'::jsonb)
            FROM task_runs WHERE trigger_id = '{trigger_id}'),
        'occurrences', (SELECT COALESCE(jsonb_agg(jsonb_build_object('task_id', task_id,
            'revision', schedule_revision, 'run_id', run_id, 'due', due_at, 'last_due', last_due_at,
            'disposition', disposition, 'count', occurrence_count)), '[]'::jsonb)
            FROM task_occurrences WHERE trigger_id = '{trigger_id}'));
    """))
    stored, linked_runs, linked_occurrences = observed["trigger"], observed["runs"], observed["occurrences"]
    core.require(len(linked_runs) == len(linked_occurrences) == 1,
                 "The saved trigger must own exactly one run and exactly one occurrence receipt.")
    linked, occurrence = linked_runs[0], linked_occurrences[0]
    revision = int(saved["Revision"])
    core.require(stored["task_id"] == linked["task_id"] == occurrence["task_id"] == runner.task_id
                 and stored["revision"] == linked["revision"] == occurrence["revision"] == revision
                 and linked["id"] == occurrence["run_id"] == admitted["Id"] and admitted["Id"] not in before
                 and linked["source"] == "schedule" and linked["state"] == "completed"
                 and occurrence["disposition"] == "admitted" and occurrence["count"] == 1
                 and occurrence["last_due"] is None and stored["error"] == ""
                 and all(tasks.timestamp(value) == target for value in (stored["last_due"], linked["due"], occurrence["due"]))
                 and tasks.timestamp(stored["next_fire"]) == tasks.timestamp(occurrences[1]) > target,
                 "Calendar firing lost its exact trigger, run, occurrence, or next-fire linkage.")
    runner.report.setdefault("calendar_results", {})[kind] = {
        "source": "schedule", "run_id": admitted["Id"], "trigger_id": trigger_id,
        "due_at": occurrences[0], "next_fire_at": occurrences[1], "completed_libraries": 2,
        "occurrence_count": 1, "database_linkage_verified": True,
    }
    runner.report["checks"][kind + "_produced_real_completed_run"] = True
    core.require(runner.replace_triggers([])["Triggers"] == [], "The completed calendar rule was not cleared.")
    runner.wait_no_work()
    core.require(len(runs(runner, core)) == len(before) + 1, "Calendar cleanup changed the expected run count.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--snapshot", type=Path, required=True)
    parser.add_argument("--binary", type=Path, default=Path("/opt/goby-test/exec-scratch/goby-m5f-linux-amd64"))
    parser.add_argument("--binary-sha256", default=ACCEPTED_BINARY_SHA256)
    parser.add_argument("--assets", type=Path, default=Path("/opt/goby-test/exec-scratch/goby-m5f-admin-assets.tar"))
    parser.add_argument("--tasks-runner", type=Path, default=Path(__file__).with_name("verify-tasks.py"))
    parser.add_argument("--core-runner", type=Path)
    args = parser.parse_args()
    if args.core_runner is None:
        args.core_runner = args.tasks_runner.with_name("verify-managed-users.py")
    tasks = load_tasks(args.tasks_runner)
    core = tasks.load_core(args.core_runner)
    core.require(args.binary_sha256 == ACCEPTED_BINARY_SHA256, "Use the fixed accepted candidate binary digest.")
    runner = configure(core, tasks, args)
    started = time.monotonic()
    def interrupted(signum, frame):
        raise core.VerificationError("The native calendar work was interrupted or exceeded its 40-second budget.")
    for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP, signal.SIGALRM):
        signal.signal(signum, interrupted)
    signal.setitimer(signal.ITIMER_REAL, WORK_SECONDS)
    try:
        runner.allocate()
        runner.prepare_database()
        runner.prepare_files()
        runner.start_app()
        runner.bootstrap()
        count = runner.report["checks"].pop("restart_snapshot_public_table_count", None)
        runner.report["checks"]["fixture_public_table_count"] = count
        observe_calendar(runner, core, tasks, "daily")
        observe_calendar(runner, core, tasks, "weekly")
        runner.assert_media_unchanged()
        sessions = runner.list_sessions()
        core.require(len(sessions) == 1 and sessions[0]["IsCurrent"] and sessions[0]["Status"] == "active",
                     "This scenario must issue only its single native control login.")
        core.require(runner.browser is None and len(runner.report.get("application_pids", [])) == 1,
                     "The calendar scenario unexpectedly started a browser or restarted the application.")
        log = core.private_file(runner.output / "app-private.log", maximum=8 * 1024 * 1024)
        core.require(not any(secret and secret.encode() in log for secret in runner.secrets),
                     "The private application log contains a credential and must not be exported.")
        runner.report["checks"].update({"one_native_control_login": True, "no_browser_or_restart": True,
                                         "original_media_bytes_unchanged": True, "rules_cleared_and_work_settled": True})
        runner.report["status"] = "passed"
    except core.VerificationError as error:
        runner.report.update({"status": "failed", "failure": str(error)})
    except Exception as error:
        runner.report.update({"status": "failed", "failure": "Calendar verification failed: " + type(error).__name__})
    finally:
        # Reserve cleanup time; never abandon credential/HBA/database cleanup
        # merely to force a successful timing result. Over-budget runs fail.
        signal.setitimer(signal.ITIMER_REAL, 0)
        for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
            signal.signal(signum, signal.SIG_IGN)
        runner.calendar_cleaning = True
        runner.cleanup()
        elapsed = time.monotonic() - started
        runner.report["elapsed_seconds"] = round(elapsed, 3)
        runner.report["checks"]["within_60_second_budget"] = elapsed <= TOTAL_SECONDS
        if elapsed > TOTAL_SECONDS:
            runner.report.update({"status": "failed", "time_budget_exceeded": True})
        if runner.output_created:
            report = runner.output / "native-calendar-report.json"
            core.private_write(report, (json.dumps(runner.report, sort_keys=True, indent=2) + "\n").encode())
            print(json.dumps({"status": runner.report["status"], "scenario": "native_calendar_firing", "report": str(report)}))
        else:
            print(json.dumps({"status": "failed", "scenario": "native_calendar_firing", "failure": runner.report.get("failure", "Preflight failed.")}))
    return 0 if runner.report["status"] == "passed" else 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        print(json.dumps({"status": "failed", "scenario": "native_calendar_firing", "failure": "Calendar setup failed: " + type(error).__name__}))
        sys.exit(1)
