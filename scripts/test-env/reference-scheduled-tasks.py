#!/usr/bin/env python3
"""Capture read-only ScheduledTaskService contracts from the original reference.

Only two reserved ordinary logins and their logout requests may mutate state.
Every ScheduledTasks mutation, device mutation, application-key request, media
request, and user-policy update is denied. Existing task definitions are never
started, stopped, or rescheduled. Prior evidence is retained, including the
historical fresh-instance evidence whose disposable program data was removed.
Run only through authorized root SSH beside the two pinned recorder modules.
"""

from __future__ import annotations

import copy
import importlib.util
import json
import os
from pathlib import Path
import signal
import stat
import sys
import time
from urllib.parse import parse_qs, quote, urlencode, urlsplit

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("scheduled_device_reference", Path(__file__).with_name("reference-devices.py"))
device = importlib.util.module_from_spec(spec)
spec.loader.exec_module(device)
base = device.base
DEVICE_SOURCE_SHA256 = "96d5378d82ce3fcd5dd9a325b02ea3b49bb9c045bcd6b320952222cc888b556d"
BASE_SOURCE_SHA256 = "ead25f9d47e8faf0b2b579ca8641b4ddcf4f7993c9e5a16710624c2927e749a8"
BASE_SOURCE = Path(__file__).with_name("reference-api-keys.py")
base.__file__ = __file__
base.ROOT = Path("/opt/goby-test/exec-scratch/scheduled-tasks-m5f")
base.PRIVATE, base.RAW, base.EXPORT = base.ROOT / "private", base.ROOT / "private/raw", base.ROOT / "export"
base.PREFIX = "scheduled-tasks-m5f-"
base.MARKER = "goby-reference-scheduled-tasks-m5f-read-only-v1"
device.DEVICE_PREFIX = "goby-scheduled-tasks-m5f-20260910-01-"
device.CLIENT_PREFIX = "Goby Scheduled Tasks M5f 20260910 01 "
device.IDENTITIES = {"control": ("admin", "control", "Control", "Scheduled Tasks Control", "0.1.0"),
                     "viewer": ("viewer", "viewer", "Viewer", "Scheduled Tasks Viewer", "0.1.0")}
device.REFERENCE_RUN = 2
MISSING = device.MISSING
EXPECTED_SERVER_ID = "ec69ef1cf84140e88489c30326529308"
REFERENCE_PID, MAIN_PID, MAIN_START_TICKS = 3131777, 3535438, "23604510"
MAIN_UNIT = "goby-foundation-test.service"
OLD_RECORDS, MAX_TASKS = 1665, 64
UNKNOWN_TASK_ID = device.DEVICE_PREFIX + "unknown-task"
HIDDEN_DEVICE_ID = "15"
FRESH_EVIDENCE = Path("/opt/goby-test/exec-scratch/emby-key-devices-fresh-m5e-20260910-02")
PRIOR_CAPTURE = FRESH_EVIDENCE / "runtime/key-devices-capture-2"
PRIOR_RECOVERY = FRESH_EVIDENCE / "runtime/key-devices-capture-2-offline-audit"
REMOVED_FRESH_MARKER = Path("/dev/shm/goby-emby-key-devices-fresh-m5e-20260910-02/.goby-managed")
ORIGINAL_DEVICE_FINAL = Path("/opt/goby-test/exec-scratch/key-devices-m5e/private/raw/key-devices-m5e-devices-final-before-control-logout.json")
PUBLIC_IDENTIFIER_FIELD = "__goby_public_task_identifier__"
RUNTIME_TASK_FIELDS = {"State", "CurrentProgressPercentage", "LastExecutionResult"}
FILTERS = [("all", {})]
for field, label in (("IsHidden", "hidden"), ("IsEnabled", "enabled")):
    for value in ("true", "false"):
        FILTERS.append((label + "-" + value, {field: value}))
for hidden in ("true", "false"):
    for enabled in ("true", "false"):
        FILTERS.append(("hidden-" + hidden + "-enabled-" + enabled, {"IsHidden": hidden, "IsEnabled": enabled}))


def main_identity() -> dict:
    output = base.subprocess.check_output(["systemctl", "show", MAIN_UNIT, "-p", "MainPID", "-p", "ActiveState",
                                          "-p", "User", "-p", "WorkingDirectory", "-p", "ExecStart"], text=True, timeout=5)
    properties = dict(line.split("=", 1) for line in output.splitlines())
    folder = Path("/proc") / str(MAIN_PID)
    identity = {"pid": MAIN_PID, "uid": folder.stat().st_uid,
                "startTicks": (folder / "stat").read_text().rsplit(")", 1)[1].split()[19],
                "networkNamespace": os.readlink(folder / "ns/net"), "executable": os.readlink(folder / "exe")}
    base.require(properties.get("MainPID") == str(MAIN_PID) and properties.get("ActiveState") == "active" and
                 identity["uid"] == 995 and identity["startTicks"] == MAIN_START_TICKS and
                 identity["executable"] == "/opt/goby-dev/goby", "The protected M5e Goby process differs")
    identity["binarySha256"] = base.digest(Path(identity["executable"]))
    identity["serviceProperties"] = properties
    return identity


def preconditions() -> int:
    base.require(base.digest(Path(device.__file__)) == DEVICE_SOURCE_SHA256 and base.digest(BASE_SOURCE) == BASE_SOURCE_SHA256,
                 "A pinned recorder dependency changed")
    pid = base.preconditions()
    base.require(pid == REFERENCE_PID and base.ROOT.parent.resolve(strict=True) == base.ROOT.parent and not base.ROOT.is_symlink(),
                 "Original reference process or capture location differs")
    base.require(base.shutil.disk_usage(base.ROOT.parent).free > 96 * 1024 * 1024, "Capture scratch lacks the bounded evidence reserve")
    main_identity()
    return pid


def task_identifier_paths(record: object) -> list[tuple]:
    if not isinstance(record, dict) or not isinstance(record.get("request"), dict) or not isinstance(record.get("response"), dict):
        return []
    request, response = record["request"], record["response"]
    route = urlsplit(request.get("path", "")).path
    if request.get("method") != "GET" or response.get("status") != 200 or response.get("bodyType") != "json":
        return []
    body, candidates = response.get("body"), []
    if route == "/emby/ScheduledTasks" and isinstance(body, list):
        candidates = [(item, ("response", "body", index)) for index, item in enumerate(body)]
    elif route.startswith("/emby/ScheduledTasks/") and route.count("/") == 3 and isinstance(body, dict):
        candidates = [(body, ("response", "body"))]
    result = []
    for item, path in candidates:
        if not isinstance(item, dict) or not isinstance(item.get("Id"), str) or not isinstance(item.get("Name"), str) or not isinstance(item.get("State"), str):
            continue
        if "Key" in item:
            result.append(path + ("Key",))
        execution = item.get("LastExecutionResult")
        if isinstance(execution, dict) and "Key" in execution:
            result.append(path + ("LastExecutionResult", "Key"))
    return result


def classified_task_identifiers(record: object) -> tuple[object, list[tuple]]:
    paths = task_identifier_paths(record)
    if not paths:
        return record, []
    value = copy.deepcopy(record)
    for path in paths:
        owner = value
        for segment in path[:-1]:
            owner = owner[segment]
        base.require(PUBLIC_IDENTIFIER_FIELD not in owner, "Public task-identifier classification would collide with an input field")
        owner[PUBLIC_IDENTIFIER_FIELD] = owner.pop("Key")
    return value, paths


class Recorder(device.Recorder):
    def __init__(self, pid: int) -> None:
        self.task_ids: set[str] = set()
        self.task_baseline: dict[str, dict] = {}
        self.baseline_filters: dict[str, set[str]] = {}
        self.task_observations: list[dict] = []
        self.task_configuration_changes: list[dict] = []
        self.task_runtime_changes: list[dict] = []
        self.task_bound_exceeded = False
        self.hidden_device_before: dict | None = None
        self.hidden_options_before: dict | None = None
        self.previous_device_ids: set[str] = set()
        self.historical_missing_paths: list[str] = []
        self.main_before = main_identity()
        super().__init__(pid)
        self.read_ids = {HIDDEN_DEVICE_ID}

    def snapshot(self) -> dict:
        baseline_path = PRIOR_CAPTURE / "private/baseline.json"
        recovery_path = PRIOR_RECOVERY / "private/raw/key-devices-fresh-m5e-2-attempt-2-recovery-audit.json"
        for path in (baseline_path, recovery_path, ORIGINAL_DEVICE_FINAL):
            base.private_file(path)
        prior = json.loads(baseline_path.read_text())
        recovered = json.loads(recovery_path.read_text())
        base.require(recovered.get("cleanupPassed") is True and recovered.get("verificationMode") == "offline-recovery" and
                     recovered.get("recoveredVerification", {}).get("httpCount") == 109,
                     "The prior fresh capture lacks its completed offline recovery proof")
        paths = [Path(name) for name in prior["records"]]
        for root in (PRIOR_CAPTURE, PRIOR_RECOVERY):
            for folder in (root / "private/raw", root / "export"):
                paths.extend(folder.glob("*.json"))
        base.require(len(paths) == OLD_RECORDS * 2 and len(set(paths)) == OLD_RECORDS * 2, "Expected exactly 1665 prior raw/export record pairs")
        media = [Path(name) for name in prior["media"]]
        base.require(len(media) == 240 and len(set(media)) == 240, "Known source-file membership differs")
        for group in ("records", "media"):
            base.require(all(base.digest(Path(name)) == expected for name, expected in prior[group].items()),
                         "An immutable earlier record or known source changed")
        original_final = json.loads(ORIGINAL_DEVICE_FINAL.read_text())["response"]
        base.require(original_final.get("status") == 200 and isinstance(original_final.get("body"), dict) and
                     isinstance(original_final["body"].get("Items"), list), "Original reference's latest device snapshot is unavailable")
        self.previous_device_ids = {item["Id"] for item in original_final["body"]["Items"]}
        base.require(len(self.previous_device_ids) == 20, "The protected original reference device membership differs")
        roots = {path.parent.parent for path in paths if path.parent.name == "raw"}
        private_paths = set()
        for root in roots:
            for path in root.rglob("*"):
                base.require(not path.is_symlink(), "A protected private tree contains a symbolic link")
                if path.is_file():
                    private_paths.add(path)
                base.require(len(private_paths) < 8192, "Prior private membership exceeds its finite bound")
        historical = {}
        self.historical_missing_paths = []
        for group in ("authorityFiles", "previousFailureFiles", "previousCaptureFailureFiles"):
            for name, expected in prior.get(group, {}).items():
                path = Path(name)
                if path == REMOVED_FRESH_MARKER and not path.exists() and not path.is_symlink():
                    self.historical_missing_paths.append(name)
                    continue
                base.require(base.digest(path) == expected, "A retained historical authority or failure file changed")
                historical[name] = expected
        base.require(sum(path.stat().st_size for path in media) < 32 * 1024 * 1024 and
                     sum(path.stat().st_size for path in private_paths) < 128 * 1024 * 1024,
                     "Preserved source or private bytes exceed their finite bounds")
        for path in [*paths, *media, *private_paths, *(Path(name) for name in historical)]:
            base.require(path.resolve(strict=True) == path and stat.S_ISREG(path.lstat().st_mode), "A preserved path is not canonical and regular")
        return {"records": {str(path): base.digest(path) for path in paths},
                "media": {str(path): base.digest(path) for path in media},
                "privateFiles": {str(path): base.digest(path) for path in sorted(private_paths)}, "retainedHistoricalFiles": historical,
                "historicalRemovedPaths": self.historical_missing_paths}

    def collect_secrets(self, value: object, key: str = "") -> None:
        classified, _paths = classified_task_identifiers(value) if not key else (value, [])
        super().collect_secrets(classified, key)

    def _sanitize_tree(self, value: object, key: str = "") -> object:
        if key.lower() == "headers":
            pairs = [(self._sanitize_tree(name), "[REDACTED_SECRET]" if base.sensitive_header(name) and content else self._sanitize_tree(content, name))
                     for name, content in base.header_pairs(value)]
            base.require(len({name for name, _ in pairs}) == len(pairs) or not isinstance(value, dict), "Sanitized header keys collide")
            return dict(pairs) if isinstance(value, dict) else [list(pair) for pair in pairs]
        if isinstance(value, dict):
            result = {}
            for child, item in value.items():
                cleaned = self._sanitize_tree(child)
                base.require(cleaned not in result, "Sanitized dictionary keys collide")
                result[cleaned] = self._sanitize_tree(item, child)
            return result
        if isinstance(value, (list, tuple)):
            return [self._sanitize_tree(item, key) for item in value]
        return base.Recorder.sanitize(self, value, key)

    def sanitize(self, value: object, key: str = "") -> object:
        classified, paths = classified_task_identifiers(value) if not key else (value, [])
        result = self._sanitize_tree(classified, key)
        for path in paths:
            owner = result
            for segment in path[:-1]:
                owner = owner[segment]
            owner["Key"] = owner.pop(PUBLIC_IDENTIFIER_FIELD)
        return result

    def authorize(self, method: str, path: str, token: str, body: object, identity: str) -> None:
        parsed = urlsplit(path)
        base.require(not parsed.scheme and not parsed.netloc and not parsed.fragment and path.startswith("/emby/") and
                     "\\" not in path and len(path.encode()) <= 2048 and not any(ord(char) < 32 for char in path),
                     "Request is not a bounded relative reference API path")
        query = parse_qs(parsed.query, keep_blank_values=True, strict_parsing=True)
        base.require(all(len(values) == 1 for values in query.values()), "Duplicate query fields are outside this read-only study")
        issued = {row["AccessToken"] for row in self.logins.values()}
        base.require(not token or token in issued and token not in self.forbidden_tokens, "HTTP credential is not freshly acknowledged and owned")
        route, allowed = parsed.path, False
        if method == "GET" and not identity and body is MISSING:
            if route in {"/emby/System/Info/Public", "/emby/Users", "/emby/Devices", "/emby/Sessions"}:
                allowed = not query
            elif route in {"/emby/Devices/Info", "/emby/Devices/Options"}:
                allowed = set(query) == {"Id"} and query["Id"][0] in self.read_ids
            elif route == "/emby/ScheduledTasks":
                allowed = set(query) <= {"IsHidden", "IsEnabled"} and all(values[0] in {"true", "false"} for values in query.values())
            elif not query:
                allowed = route in {"/emby/ScheduledTasks/" + quote(value, safe="") for value in self.task_ids | {UNKNOWN_TASK_ID}}
        elif route == "/emby/Users/AuthenticateByName":
            if method == "POST" and not query and not token and identity in device.IDENTITIES and identity not in self.login_attempts:
                account = self.account_credentials[device.IDENTITIES[identity][0]]
                allowed = body == {"Username": account["REFERENCE_USERNAME"], "Pw": account["REFERENCE_PASSWORD"]}
        elif route == "/emby/Sessions/Logout":
            allowed = method == "POST" and not query and not identity and body is MISSING and bool(token)
        base.require(allowed, "Request is outside the read-only ScheduledTasks and owned-login allowlist")

    def request(self, label: str, method: str, path: str, *, token: str = "", body: object = MISSING,
                identity: str = "") -> tuple[int, object]:
        status, result = super().request(label, method, path, token=token, body=body, identity=identity)
        if method == "GET" and urlsplit(path).path == "/emby/ScheduledTasks" and status == 200 and isinstance(result, list) and len(result) > MAX_TASKS:
            self.task_bound_exceeded = True
            self.task_observations.append({"capture": label, "status": status, "itemsCount": len(result),
                                           "completeDefinitionSample": False, "definitionBoundExceeded": True})
            raise RuntimeError("A task-list response exceeds the bounded sample; its full captured response is retained")
        return status, result

    @staticmethod
    def task_summary(item: dict) -> dict:
        result = {"id": item.get("Id"), "taskIdentifier": item.get("Key"), "name": item.get("Name"), "state": item.get("State"),
                  "progress": item.get("CurrentProgressPercentage"), "hidden": item.get("IsHidden"), "triggers": item.get("Triggers")}
        execution = item.get("LastExecutionResult")
        if isinstance(execution, dict):
            result["lastExecution"] = {("taskIdentifier" if name == "Key" else name): value for name, value in execution.items()}
        return result

    def task_list(self, label: str, filters: dict, *, baseline: bool) -> list[dict]:
        path = "/emby/ScheduledTasks" + ("?" + urlencode(filters) if filters else "")
        status, body = self.request(label, "GET", path, token=self.admin())
        observation = {"capture": label, "filters": filters, "status": status, "responseShape": type(body).__name__,
                       "itemsCount": len(body) if isinstance(body, list) else None, "completeDefinitionSample": False}
        self.task_observations.append(observation)
        base.require(status == 200 and isinstance(body, list), "Administrator task-list response differs from the declared TaskInfo array")
        if len(body) > MAX_TASKS:
            self.task_bound_exceeded = True
            raise RuntimeError("Task definitions exceed the bounded sample; no truncated set is treated as complete")
        base.require(all(isinstance(item, dict) and isinstance(item.get("Id"), str) and
                     base.re.fullmatch(r"[A-Za-z0-9_-]{1,128}", item["Id"]) and isinstance(item.get("Name"), str) and
                     isinstance(item.get("State"), str) for item in body) and len({item["Id"] for item in body}) == len(body),
                     "Task-list identity or DTO shape is ambiguous")
        ids = {item["Id"] for item in body}
        base.require(UNKNOWN_TASK_ID not in ids, "Reserved unknown task ID collides with an observed definition")
        if len(self.task_ids | ids) > MAX_TASKS:
            self.task_bound_exceeded = True
            raise RuntimeError("Combined filtered task definitions exceed the bounded sample")
        observation.update({"completeDefinitionSample": True, "taskIds": sorted(ids), "tasks": [self.task_summary(item) for item in body]})
        if baseline:
            self.task_ids.update(ids)
            for item in body:
                self.task_baseline.setdefault(item["Id"], copy.deepcopy(item))
        return body

    def task_detail(self, label: str, task_id: str, *, final: bool = False) -> dict:
        status, body = self.request(label, "GET", "/emby/ScheduledTasks/" + quote(task_id, safe=""), token=self.admin())
        base.require(status == 200 and isinstance(body, dict) and body.get("Id") == task_id and
                     isinstance(body.get("Name"), str) and isinstance(body.get("State"), str), "A known task detail does not match its observed ID")
        self.task_observations.append({"capture": label, "status": status, "task": self.task_summary(body), "detailIdentityMatched": True})
        if final:
            before = self.task_baseline[task_id]
            changed = sorted(name for name in set(before) | set(body) if name not in RUNTIME_TASK_FIELDS and
                             ((name in before) != (name in body) or before.get(name) != body.get(name)))
            if changed:
                self.task_configuration_changes.append({"taskId": task_id, "changedFields": changed,
                    "presenceChanges": [name for name in changed if (name in before) != (name in body)]})
            dynamic = sorted(name for name in RUNTIME_TASK_FIELDS if (name in before) != (name in body) or before.get(name) != body.get(name))
            if dynamic:
                self.task_runtime_changes.append({"taskId": task_id, "changedFields": dynamic,
                    "presenceChanges": [name for name in dynamic if (name in before) != (name in body)],
                    "before": self.task_summary(before), "after": self.task_summary(body),
                    "attribution": "Observed during a read-only study; no run, cancel, or trigger-write request was issued"})
        return body

    def capture(self) -> None:
        status, public = self.request("public-before", "GET", "/emby/System/Info/Public")
        base.require(status == 200 and isinstance(public, dict) and public.get("Version") == "4.9.5.0" and public.get("Id") == EXPECTED_SERVER_ID,
                     "Original reference product or server identity differs")
        self.login("control")
        rows = self.devices("devices-baseline")
        self.control_id = self.find_owned(rows, "control", "Control")
        self.old_devices = {row["Id"]: row for row in rows if row["Id"] != self.control_id}
        base.require(set(self.old_devices) == self.previous_device_ids and len(self.old_devices) <= device.MAX_OLD_DEVICES and
                     all(not str(row.get("ReportedDeviceId", "")).startswith(device.DEVICE_PREFIX) for row in self.old_devices.values()),
                     "Original device baseline differs or reserved metadata collides")
        self.read_ids.update(self.old_devices)
        base.save(base.PRIVATE / "old-devices.json", self.old_devices)
        for index, device_id in enumerate(sorted(self.old_devices)):
            status, body = self.request("old-options-before-" + str(index), "GET", self.route("/Options", device_id), token=self.admin())
            self.old_options[device_id] = {"status": status, "body": body}
        base.save(base.PRIVATE / "old-options.json", self.old_options)
        status, hidden = self.request("hidden-server-info-before", "GET", self.route("/Info", HIDDEN_DEVICE_ID), token=self.admin())
        base.require(status == 200 and isinstance(hidden, dict) and hidden.get("Id") == HIDDEN_DEVICE_ID and
                     hidden.get("ReportedDeviceId") == EXPECTED_SERVER_ID, "The protected hidden server device differs")
        self.hidden_device_before = hidden
        status, options = self.request("hidden-server-options-before", "GET", self.route("/Options", HIDDEN_DEVICE_ID), token=self.admin())
        self.hidden_options_before = {"status": status, "body": options}
        status, users = self.request("users-baseline", "GET", "/emby/Users", token=self.admin())
        base.require(status == 200 and isinstance(users, list) and all(isinstance(row, dict) and row.get("Id") for row in users), "User baseline is unavailable")
        self.old_users = users
        base.save(base.PRIVATE / "old-users.json", users)
        self.login("viewer")
        self.devices("devices-after-viewer")
        for identity in device.IDENTITIES:
            base.require(self.probe_login(identity + "-protected-before", identity) == 200, "Fresh owned login cannot establish authenticated access")
        for label, filters in FILTERS:
            body = self.task_list("tasks-baseline-" + label, filters, baseline=True)
            self.baseline_filters[label] = {item["Id"] for item in body}
        base.require(bool(self.task_ids), "No task ID is available for the bounded known-detail permission study")
        base.save(base.PRIVATE / "task-baseline.json", self.task_baseline)
        for index, task_id in enumerate(sorted(self.task_ids)):
            self.task_detail("task-detail-" + str(index), task_id)
        chosen = sorted(self.task_ids)[0]
        for actor, token in (("anonymous", ""), ("viewer", self.logins["viewer"]["AccessToken"])):
            for name, path in (("list", "/emby/ScheduledTasks"), ("known-detail", "/emby/ScheduledTasks/" + quote(chosen, safe="")),
                               ("unknown-detail", "/emby/ScheduledTasks/" + quote(UNKNOWN_TASK_ID, safe=""))):
                status, _ = self.request(actor + "-tasks-" + name, "GET", path, token=token)
                self.task_observations.append({"capture": actor + "-tasks-" + name, "actor": actor, "operation": name, "status": status})
        status, _ = self.request("admin-tasks-unknown-detail", "GET", "/emby/ScheduledTasks/" + quote(UNKNOWN_TASK_ID, safe=""), token=self.admin())
        self.task_observations.append({"capture": "admin-tasks-unknown-detail", "actor": "administrator", "operation": "unknown-detail", "status": status})

    def compare_tasks(self) -> None:
        matched = True
        for label, filters in FILTERS:
            rows = self.task_list("tasks-final-" + label, filters, baseline=False)
            matched = matched and {item["Id"] for item in rows} == self.baseline_filters[label]
        for index, task_id in enumerate(sorted(self.task_ids)):
            self.task_detail("task-final-detail-" + str(index), task_id, final=True)
        self.checks["taskDefinitionMembershipUnchanged"] = matched
        self.checks["taskConfigurationUnchanged"] = not self.task_configuration_changes

    def compare_devices(self) -> None:
        super().compare_devices()
        self.checks.pop("onlyControlOwnedDeviceRetained", None)
        self.checks["onlyThisRunOwnedLoginDeviceHistoryAdded"] = all(row["Id"] in self.old_devices or self.owned_pair(row) for row in self.final_devices)
        status, hidden = self.request("hidden-server-info-final", "GET", self.route("/Info", HIDDEN_DEVICE_ID), token=self.admin())
        option_status, options = self.request("hidden-server-options-final", "GET", self.route("/Options", HIDDEN_DEVICE_ID), token=self.admin())
        self.checks["hiddenServerDeviceInfoAndOptionsUnchanged"] = (self.hidden_device_before is not None and status == 200 and
            hidden == self.hidden_device_before and self.hidden_options_before == {"status": option_status, "body": options})

    def finish(self) -> None:
        self.finishing = True
        self.deadline = time.monotonic() + device.PHASE_SECONDS
        if "control" in self.logins:
            if len(self.baseline_filters) == len(FILTERS) and self.task_ids and not self.task_bound_exceeded:
                self.cleanup_step("compare-task-definitions", self.compare_tasks)
            if self.old_devices is not None:
                self.cleanup_step("compare-devices", self.compare_devices)
            if self.old_users is not None:
                self.cleanup_step("compare-users", self.compare_users)
            self.cleanup_step("sessions-before-logout", lambda: self.request("sessions-before-owned-logouts", "GET", "/emby/Sessions", token=self.admin()))
        for identity in ("viewer", "control"):
            self.cleanup_step("logout-" + identity, lambda identity=identity: self.logout(identity))
        self.checks["allAcknowledgedLoginsInvalid"] = set(self.logins) == set(self.invalid_login_proofs)
        self.checks["allAttemptedLoginsAcknowledgedOrRejected"] = all(name in self.logins or self.login_statuses.get(name) in {400, 401, 403} for name in self.login_attempts)
        self.checks["originalReferenceProcessUnchanged"] = self.cleanup_step("reference-process-audit", preconditions) == self.pid
        self.checks["mainGobyProcessAndBinaryUnchanged"] = self.cleanup_step("main-process-audit", main_identity) == self.main_before
        self.checks["allPriorEvidenceSourcesAndPrivateFilesUnchanged"] = self.cleanup_step("prior-file-audit", self.snapshot) == self.baseline
        self.checks["scheduledTaskMutationRequestsZero"] = all(not row["request"]["path"].startswith("/emby/ScheduledTasks") for row in self.mutations)
        self.cleanup_step("mutation-journal", lambda: base.save(base.PRIVATE / "mutation-results.json", self.mutations))
        redaction_ok = True
        for path in base.RAW.glob(base.PREFIX + "*.json"):
            base.private_file(path)
            base.private_file(base.EXPORT / path.name)
            redaction_ok = redaction_ok and self.sanitize(json.loads(path.read_text())) == json.loads((base.EXPORT / path.name).read_text())
        self.checks["newRedactionAuditPassed"] = redaction_ok
        self.cleanup_ok = not self.cleanup_errors and all(self.checks.values())
        self.write("audit", {"kind": "capture-audit-observation", "referencePID": self.pid, "serverId": EXPECTED_SERVER_ID,
            "captureFailureType": self.capture_failure, "cleanupPassed": self.cleanup_ok, "checks": self.checks, "cleanupErrors": self.cleanup_errors,
            "preservedOldRecords": OLD_RECORDS, "preservedOldRecordFiles": len(self.baseline["records"]),
            "preservedOldMediaFiles": len(self.baseline["media"]), "preservedOldPrivateFiles": len(self.baseline["privateFiles"]),
            "historicallyRemovedFreshPathsNotRequiredToExist": self.historical_missing_paths,
            "mainGobyBefore": self.main_before, "oldListedDeviceCount": None if self.old_devices is None else len(self.old_devices),
            "oldDeviceActivityDifferences": self.old_device_date_changes, "attributedUserActivityDifferences": self.user_date_changes,
            "ownedLoginAttempts": sorted(self.login_attempts), "ownedLoginStatuses": self.login_statuses,
            "ownedLoginInvalidityProofs": self.invalid_login_proofs, "ownedLoginLogoutStatuses": self.logout_statuses,
            "retainedOwnedDeviceHistory": None if self.final_devices is None else [row for row in self.final_devices if self.owned_pair(row)],
            "retainedHistoryAttribution": "Only this run's reserved ordinary-login device metadata; both acknowledged credentials independently end with 401",
            "taskObservations": self.task_observations, "taskConfigurationChanges": self.task_configuration_changes,
            "taskRuntimeChanges": self.task_runtime_changes, "taskDefinitionBoundExceeded": self.task_bound_exceeded,
            "completeInitialFilterSet": len(self.baseline_filters) == len(FILTERS), "observedTaskDefinitions": len(self.task_ids),
            "taskModelPublicIdentifierHandling": "Only TaskInfo.Key and its TaskResult.Key in successful ScheduledTasks response DTOs are public identifiers; known credentials are still redacted everywhere",
            "scheduledTaskMutationRequests": 0, "deviceMutationRequests": 0, "applicationKeyRequests": 0,
            "existingCredentialHTTPRequests": 0, "mediaOrEncoderRequests": 0, "sourceWrites": 0,
            "httpAttempts": self.record_count, "incompleteHTTP": self.incomplete_count, "wireBytes": self.total,
            "wireByteBudgetCharged": self.charged_bytes, "elapsedSeconds": round(time.monotonic() - self.started, 3),
            "limits": {"taskDefinitions": MAX_TASKS, "httpAttempts": device.MAX_REQUESTS, "mainHTTPAttempts": device.MAIN_REQUESTS,
                "responseBytes": base.MAX_BODY, "totalResponseBytes": base.MAX_TOTAL, "phaseSeconds": device.PHASE_SECONDS},
            "dependencies": [{"sourceFile": "reference-devices.py", "sha256": DEVICE_SOURCE_SHA256},
                             {"sourceFile": "reference-api-keys.py", "sha256": BASE_SOURCE_SHA256}]})
        base.require(self.cleanup_ok, "Cleanup or preservation proof is incomplete; inspect retained private evidence")


def main() -> None:
    signal.signal(signal.SIGALRM, device.deadline_expired)
    recorder = Recorder(preconditions())
    failure = None
    try:
        recorder.capture()
    except Exception as error:
        failure = type(error).__name__
        recorder.capture_failure = failure
        base.save(base.PRIVATE / "failure.txt", failure + ": " + str(error) + "\n")
    finally:
        try:
            recorder.finish()
        except Exception as error:
            base.save(base.PRIVATE / "cleanup-failure.txt", type(error).__name__ + ": " + str(error) + "\n")
            print(json.dumps({"capture": base.PREFIX, "cleanup": "failed", "failureType": type(error).__name__}), flush=True)
            raise SystemExit(1)
    print(json.dumps({"capture": base.PREFIX, "result": "complete" if failure is None else "partial", "failureType": failure,
                      "cleanup": recorder.cleanup_ok}), flush=True)
    if failure:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
