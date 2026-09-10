#!/usr/bin/env python3
"""Capture bounded task mutations inside one attested disposable Emby instance.

Only the uniquely identified RefreshLibrary definition is mutable. Unknown-ID
controls use one proved-missing reserved ID. Two fixed media batches may each
be registered once, with scanning and provider side effects constrained below.
No existing reference task, service, user policy, library, or source is changed.
The fixture operator owns final service/data teardown; this recorder leaves
its two possible library definitions inside that disposable program data.
"""

from __future__ import annotations

import copy
import http.client
import importlib.util
import json
import os
from pathlib import Path
import signal
import stat
import sys
import time
from urllib.parse import parse_qs, quote, quote_plus, urlencode, urlsplit

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("fresh_tasks_read_reference", Path(__file__).with_name("reference-scheduled-tasks.py"))
read = importlib.util.module_from_spec(spec)
spec.loader.exec_module(read)
device, base = read.device, read.base
READ_SOURCE_SHA256 = "b037ab51f5d0fb9e84d4121466a34263ebbf166c99eff2e56cc2dc5b68ada4f1"
OPERATOR_SOURCE_SHA256 = "bca453513b40c157ecffceb51133d2790c24f13e7f66fb75bdccbab84ec6a50b"
EVIDENCE = Path("/opt/goby-test/exec-scratch/emby-scheduled-tasks-fresh-m5f-20260910-01")
DATA = Path("/dev/shm/goby-emby-scheduled-tasks-fresh-m5f-20260910-01")
SOURCE = EVIDENCE / "source"
UNIT = "goby-emby-scheduled-tasks-fresh-m5f-20260910-01.service"
MARKER = "goby-emby-scheduled-tasks-fresh-m5f-20260910-01-owned-v1"
PORT, PRIOR_RECORDS = 18099, 1794
MANIFEST = EVIDENCE / "private/manifest.json"
OPERATOR_SOURCE = Path(__file__).with_name("prepare-scheduled-tasks-fresh.py")
SOURCE_BATCHES = {"a": SOURCE / "batch-a", "b": SOURCE / "batch-b"}
LIBRARY_NAMES = {name: "Goby Scheduled Tasks Fresh M5f Batch " + name.upper() + " 01" for name in SOURCE_BATCHES}
UNKNOWN_ID = "goby-scheduled-tasks-fresh-m5f-20260910-01-missing"
MAX_MAIN_REQUESTS, MAX_REQUESTS = 256, 480
PHASE_SECONDS, STOP_SECONDS, POLL_LIMIT = 360, 45, 30
ACTIVE_NAMESPACE: str | None = None
_operator = None
MISSING = device.MISSING
base.__file__ = __file__
base.ROOT = EVIDENCE / "runtime/task-capture"
base.PRIVATE, base.RAW, base.EXPORT = base.ROOT / "private", base.ROOT / "private/raw", base.ROOT / "export"
base.PREFIX = "scheduled-tasks-fresh-m5f-"
base.MARKER = "goby-reference-scheduled-tasks-fresh-m5f-owned-v1"
device.DEVICE_PREFIX = "goby-scheduled-tasks-fresh-m5f-20260910-01-capture-"
device.CLIENT_PREFIX = "Goby Scheduled Tasks Fresh M5f 20260910 01 Capture "
device.IDENTITIES = {"control": ("admin", "control", "Control", "Fresh Tasks Control", "0.1.0"),
                     "viewer": ("viewer", "viewer", "Viewer", "Fresh Tasks Viewer", "0.1.0")}
TRIGGER_CASES = [
    ("clear", []),
    ("duplicate-interval", [{"Type": "IntervalTrigger", "IntervalTicks": 432000000000}] * 2),
    ("daily", [{"Type": "DailyTrigger", "TimeOfDayTicks": 72000000000, "MaxRuntimeTicks": 144000000000}]),
    ("startup", [{"Type": "StartupTrigger"}]),
    ("system-event", [{"Type": "SystemEventTrigger", "SystemEvent": "DisplayConfigurationChange"}]),
    ("unknown", [{"Type": "GobyOwnedUnknownTrigger"}]),
    ("mixed", [{"Type": "IntervalTrigger", "IntervalTicks": 432000000000}, {"Type": "GobyOwnedUnknownTrigger"}]),
]


def operator_module():
    global _operator
    base.require(base.digest(OPERATOR_SOURCE) == OPERATOR_SOURCE_SHA256, "Fresh task operator source differs from the reviewed dependency")
    if _operator is None:
        spec = importlib.util.spec_from_file_location("fresh_tasks_operator_read_only", OPERATOR_SOURCE)
        _operator = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(_operator)
    base.require(_operator.ROOT == EVIDENCE and _operator.DATA == DATA and _operator.SOURCE == SOURCE and
                 _operator.UNIT == UNIT and _operator.PORT == PORT, "Operator authority constants differ")
    return _operator


def load_manifest() -> dict:
    base.private_file(MANIFEST)
    base.require(MANIFEST.stat().st_size <= 8 * 1024 * 1024, "Fresh task manifest exceeds its bound")
    manifest = json.loads(MANIFEST.read_text())
    base.require(isinstance(manifest, dict) and manifest.get("schemaVersion") == 1 and manifest.get("state") == "READY" and
                 manifest.get("unit") == UNIT and manifest.get("port") == PORT and manifest.get("programData") == str(DATA) and
                 manifest.get("sourceRoot") == str(SOURCE) and manifest.get("sourceBatches") == {name: str(path) for name, path in SOURCE_BATCHES.items()} and
                 manifest.get("operatorSha256") == OPERATOR_SOURCE_SHA256 and manifest.get("allFreshProgramDataOwned") is True and
                 manifest.get("sourceManifest") == str(EVIDENCE / "private/source-manifest.json") and
                 manifest.get("sourceFilesAreIndependent") is True and manifest.get("sourceCount") == 512 and
                 manifest.get("bootstrapCredentialsRevoked") is True and manifest.get("bootstrapCredentialsInvalidity") == {"admin": 401, "viewer": 401},
                 "Fresh task READY authority is incomplete or differs")
    base.require(manifest.get("bootstrapLibraries") == [] and manifest.get("bootstrapLibraryMutationRequests") == 0 and
                 manifest.get("bootstrapTaskMutationRequests") == 0 and manifest.get("bootstrapApplicationKeyRequests") == 0 and
                 isinstance(manifest.get("bootstrapTasks"), list) and 0 < len(manifest["bootstrapTasks"]) <= 64 and
                 isinstance(manifest.get("sourceFiles"), dict) and len(manifest["sourceFiles"]) == 512,
                 "Fresh bootstrap contains unexpected library/task state or source membership")
    return manifest


def preconditions() -> int:
    global ACTIVE_NAMESPACE
    ACTIVE_NAMESPACE = None
    base.require(sys.platform == "linux" and os.geteuid() == 0 and bool(os.environ.get("SSH_CONNECTION")), "Run only through authorized root SSH")
    base.require(base.digest(Path(read.__file__)) == READ_SOURCE_SHA256 and base.digest(Path(device.__file__)) == read.DEVICE_SOURCE_SHA256 and
                 base.digest(Path(__file__).with_name("reference-api-keys.py")) == read.BASE_SOURCE_SHA256, "A pinned recorder dependency changed")
    op, manifest = operator_module(), load_manifest()
    base.require(base.digest(Path(manifest["sourceManifest"])) == manifest.get("sourceManifestSha256"), "Owned source manifest changed after READY")
    actual = op.new_identity()
    base.require(all(actual[name] == manifest[name] for name in actual), "Fresh task process or sandbox changed after READY")
    op.check_old_services(manifest["oldServices"])
    op.verify_baseline(manifest["oldBaseline"])
    op.verify_source(op.load(Path(manifest["sourceManifest"])))
    base.require(actual["networkNamespace"] != os.readlink("/proc/1/ns/net") and
                 actual["networkNamespace"] not in {row["networkNamespace"] for row in manifest["oldServices"].values()},
                 "Fresh task namespace overlaps a protected authority")
    if os.readlink("/proc/self/ns/net") != actual["networkNamespace"]:
        os.execvp("nsenter", ["nsenter", "-t", str(actual["pid"]), "-n", sys.executable, "-B", str(Path(__file__).resolve())])
    base.require(os.readlink("/proc/self/ns/net") == actual["networkNamespace"] and
                 base.shutil.disk_usage(EVIDENCE).free > 64 * 1024 * 1024, "Fresh task transport or evidence reserve is unavailable")
    ACTIVE_NAMESPACE = actual["networkNamespace"]
    return actual["pid"]


def static_task(item: dict) -> dict:
    return {name: value for name, value in item.items() if name not in read.RUNTIME_TASK_FIELDS}


def execution_fingerprint(item: dict) -> str:
    return json.dumps({"present": "LastExecutionResult" in item, "value": item.get("LastExecutionResult")}, sort_keys=True, separators=(",", ":"))


def library_body(batch: str) -> dict:
    base.require(batch in SOURCE_BATCHES, "Library source batch is not owned")
    return {"Name": LIBRARY_NAMES[batch], "CollectionType": "movies", "Paths": [str(SOURCE_BATCHES[batch])], "RefreshLibrary": False,
        "LibraryOptions": {"PathInfos": [{"Path": str(SOURCE_BATCHES[batch])}], "SaveLocalMetadata": False, "MetadataSavers": [],
            "EnableRealtimeMonitor": False, "SampleIgnoreSize": 0, "EnableAutomaticSeriesGrouping": False,
            "EnableChapterImageExtraction": False, "EnableMarkerDetection": False, "AutomaticRefreshIntervalDays": 0,
            "TypeOptions": [{"Type": "Movie", "MetadataFetchers": [], "MetadataFetcherOrder": [], "ImageFetchers": [], "ImageFetcherOrder": []}]}}


class Recorder(read.Recorder):
    def __init__(self, pid: int) -> None:
        self.manifest = load_manifest()
        self.source_manifest = operator_module().load(Path(self.manifest["sourceManifest"]))
        self.bootstrap_tokens: set[str] = set()
        self.selected_id: str | None = None
        self.initial_triggers: list | None = None
        self.all_task_baseline: dict[str, dict] = {}
        self.latest_task: dict | None = None
        self.task_write_proof: dict | None = None
        self.unknown_proved = False
        self.library_attempts: set[str] = set()
        self.libraries_owned: dict[str, dict] = {}
        self.latest_library_count: int | None = None
        self.library_proof_sequence = -1
        self.trigger_write_possible = False
        self.run_possible = False
        self.poll_observations: list[dict] = []
        self.experiments: list[dict] = []
        self.task_mutation_acks: list[dict] = []
        self.library_mutation_acks: list[dict] = []
        self.other_task_changes: list[dict] = []
        self.start_counts: dict[str, int] = {}
        self.current_experiment = "setup"
        self.initial_task_membership: set[str] = set()
        super().__init__(pid)
        self.read_ids = set()
        self.forbidden_tokens.update(self.bootstrap_tokens)
        self.secrets.update(self.bootstrap_tokens)

    def _sanitize_tree(self, value: object, key: str = "") -> object:
        cleaned = super()._sanitize_tree(value, key)
        if isinstance(cleaned, str):
            for secret in sorted(self.secrets, key=len, reverse=True):
                if not secret:
                    continue
                encoded = {quote(secret, safe=""), quote_plus(secret, safe="")}
                encoded.update(quote(item, safe="") for item in tuple(encoded))
                encoded.update(base.re.sub(r"%[0-9A-Fa-f]{2}", lambda match: match.group(0).lower(), item) for item in tuple(encoded))
                for item in sorted(encoded, key=len, reverse=True):
                    cleaned = cleaned.replace(item, "[REDACTED_SECRET]")
        return cleaned

    def credentials(self, account: str) -> dict:
        base.require(account in {"admin", "viewer"}, "Unknown fresh credential account")
        path = EVIDENCE / "private" / (account + "-credentials.env")
        base.private_file(path)
        values = dict(line.split("=", 1) for line in path.read_text().splitlines())
        base.require(set(values) == {"REFERENCE_USERNAME", "REFERENCE_PASSWORD"} and values["REFERENCE_USERNAME"], "Fresh credential fields differ")
        self.secrets.update(value for name, value in values.items() if name == "REFERENCE_PASSWORD" and value)
        return values

    def snapshot(self) -> dict:
        op = operator_module()
        op.verify_baseline(self.manifest["oldBaseline"])
        op.verify_source(self.source_manifest)
        old = self.manifest["oldBaseline"]
        base.require(len(old["records"]) == PRIOR_RECORDS * 2, "Expected exactly 1794 preceding raw/export pairs")
        setup_raw = sorted((EVIDENCE / "private/raw").glob("*.json"))
        setup_export = sorted((EVIDENCE / "export").glob("*.json"))
        base.require(len(setup_raw) == len(setup_export) == self.manifest["setupRecordCount"] and
                     {path.name for path in setup_raw} == {path.name for path in setup_export}, "Fresh setup evidence membership differs")
        def collect_tokens(value: object) -> None:
            if isinstance(value, dict):
                for name, child in value.items():
                    if name.lower() == "accesstoken" and isinstance(child, str) and child:
                        self.bootstrap_tokens.add(child)
                    else:
                        collect_tokens(child)
            elif isinstance(value, list):
                for child in value:
                    collect_tokens(child)
        for path in setup_raw:
            base.private_file(path)
            collect_tokens(json.loads(path.read_text()))
        private_paths = sorted(path for path in (EVIDENCE / "private").rglob("*") if path.is_file())
        base.require(len(private_paths) < 1024 and sum(path.stat().st_size for path in private_paths) < 32 * 1024 * 1024,
                     "Fresh private baseline exceeds its bound")
        for path in [*setup_export, *private_paths]:
            base.private_file(path)
        return {"records": {**old["records"], **{str(path): base.digest(path) for path in [*setup_raw, *setup_export]}},
                "media": old["media"], "privateFiles": {**old["privateFiles"], **{str(path): base.digest(path) for path in private_paths}},
                "retainedHistoricalFiles": old["retainedHistoricalFiles"], "historicalRemovedPaths": old["historicalRemovedPaths"],
                "ownedSourceFiles": self.manifest["sourceFiles"], "ownedSourceSnapshot": op.source_snapshot(complete=True)}

    def task_route(self, operation: str, target: str) -> tuple[str, str]:
        encoded = quote(target, safe="")
        routes = {"triggers": ("POST", "/emby/ScheduledTasks/" + encoded + "/Triggers"),
                  "start": ("POST", "/emby/ScheduledTasks/Running/" + encoded),
                  "stop": ("DELETE", "/emby/ScheduledTasks/Running/" + encoded),
                  "stop-alias": ("POST", "/emby/ScheduledTasks/Running/" + encoded + "/Delete")}
        base.require(operation in routes, "Unknown task operation")
        return routes[operation]

    def task_operation(self, method: str, path: str) -> tuple[str, str] | None:
        for target in ({self.selected_id, UNKNOWN_ID} - {None}):
            for operation in ("triggers", "start", "stop", "stop-alias"):
                if (method, path) == self.task_route(operation, target):
                    return operation, target
        return None

    def allowed_trigger_body(self, body: object) -> bool:
        return isinstance(body, list) and self.initial_triggers is not None and any(body == candidate for candidate in
            [self.initial_triggers, *[candidate for _name, candidate in TRIGGER_CASES]])

    def authorize(self, method: str, path: str, token: str, body: object, identity: str) -> None:
        parsed = urlsplit(path)
        base.require(not parsed.scheme and not parsed.netloc and not parsed.fragment and path.startswith("/emby/") and
                     "\\" not in path and len(path.encode()) <= 2048 and not any(ord(char) < 32 for char in path), "Request escaped the bounded fresh API")
        query = parse_qs(parsed.query, keep_blank_values=True, strict_parsing=True)
        base.require(all(len(value) == 1 for value in query.values()), "Duplicate query values are outside this study")
        issued = {row["AccessToken"] for row in self.logins.values()}
        base.require(not token or token in issued and token not in self.forbidden_tokens, "Credential is not freshly acknowledged and owned")
        route, allowed = parsed.path, False
        if method == "GET" and body is MISSING and not identity:
            if route in {"/emby/System/Info/Public", "/emby/ScheduledTasks", "/emby/Users", "/emby/Devices", "/emby/Sessions"}:
                allowed = not query
            elif route == "/emby/Library/VirtualFolders/Query":
                allowed = query == {"StartIndex": ["0"], "Limit": ["4"]}
            elif route in {"/emby/Devices/Info", "/emby/Devices/Options"}:
                allowed = set(query) == {"Id"} and query["Id"][0] in self.read_ids
            elif not query:
                allowed = route in {"/emby/ScheduledTasks/" + quote(value, safe="") for value in self.task_ids | {UNKNOWN_ID}}
        elif route == "/emby/Users/AuthenticateByName" and method == "POST" and not token and not query and identity in device.IDENTITIES and identity not in self.login_attempts:
            values = self.account_credentials[device.IDENTITIES[identity][0]]
            allowed = body == {"Username": values["REFERENCE_USERNAME"], "Pw": values["REFERENCE_PASSWORD"]}
        elif route == "/emby/Sessions/Logout":
            allowed = method == "POST" and not query and not identity and body is MISSING and bool(token)
        elif route == "/emby/Library/VirtualFolders":
            matches = [name for name in SOURCE_BATCHES if body == library_body(name)]
            allowed = (method == "POST" and not query and not identity and token == self.admin() and len(matches) == 1 and
                       matches[0] not in self.library_attempts and self.latest_library_count == len(self.libraries_owned) and
                       self.selected_id is not None and self.latest_task is not None and self.latest_task.get("State") == "Idle" and
                       self.latest_task.get("Triggers") == [] and self.task_write_proof is not None and
                       self.task_write_proof.get("requestSequence") == self.record_count)
        elif not query and not identity:
            operation = self.task_operation(method, route)
            if operation:
                kind, target = operation
                proof = self.task_write_proof
                proved = (self.selected_id is not None and self.initial_triggers is not None and proof is not None and
                          proof.get("taskId") == self.selected_id and proof.get("taskIdentifier") == "RefreshLibrary" and
                          proof.get("requestSequence") == self.record_count and (target == self.selected_id or self.unknown_proved))
                if kind == "triggers":
                    allowed = proved and self.allowed_trigger_body(body) and (self.latest_library_count == 0 or
                              self.finishing and (body == self.initial_triggers or body == []))
                else:
                    allowed = proved and body is MISSING and (self.latest_library_count == 0 or token == self.admin())
                    if kind in {"stop", "stop-alias"} and self.latest_library_count != 0 and not self.finishing:
                        allowed = allowed and proof.get("state") == "Running"
                if target == UNKNOWN_ID:
                    allowed = allowed and self.latest_library_count == 0
        base.require(allowed, "Request is outside the owned RefreshLibrary mutation allowlist")

    def request(self, label: str, method: str, path: str, *, token: str = "", body: object = MISSING,
                identity: str = "") -> tuple[int, object]:
        self.authorize(method, path, token, body, identity)
        base.require(ACTIVE_NAMESPACE == self.manifest["networkNamespace"] and os.readlink("/proc/self/ns/net") == ACTIVE_NAMESPACE,
                     "Fresh task transport authority is unavailable")
        base.require(self.record_count < (MAX_REQUESTS if self.finishing else MAX_MAIN_REQUESTS), "Task HTTP phase exhausted its request reserve")
        remaining = self.deadline - time.monotonic()
        base.require(remaining > 0, "Task HTTP phase deadline expired")
        limit = min(base.MAX_BODY, (base.MAX_TOTAL if self.finishing else base.MAX_TOTAL - 8 * 1024 * 1024) - self.charged_bytes)
        base.require(limit > 0, "Task HTTP byte reserve exhausted")
        headers, wire = {"Accept": "application/json"}, None
        metadata = self.metadata(identity) if identity else None
        if metadata:
            headers["Authorization"] = "Emby " + ", ".join(name + '=\"' + value + '\"' for name, value in metadata.items())
        if token:
            headers["X-Emby-Token"] = token
        if body is not MISSING:
            headers["Content-Type"] = "application/json"
            wire = json.dumps(body, separators=(",", ":")).encode()
            base.require(len(wire) <= 4096, "Task request body exceeds its bound")
        request = {"method": method, "path": path, "headers": headers, "body": None if body is MISSING else body,
                   "bodyPresent": body is not MISSING, "clientMetadata": metadata}
        started_at = base.dt.datetime.now(base.dt.timezone.utc).isoformat()
        started_monotonic = time.monotonic()
        parsed = urlsplit(path)
        task_operation = self.task_operation(method, parsed.path) if method != "GET" else None
        if task_operation:
            request["ownedTaskWriteProof"] = copy.deepcopy(self.task_write_proof)
            request["experiment"] = self.current_experiment
        mutation = None
        if method != "GET":
            mutation = {"label": label, "request": request, "responseStatus": None, "acknowledgedLogin": False, "transportFailure": None}
            self.mutations.append(mutation)
            base.save(base.PRIVATE / "mutations" / (str(len(self.mutations)).zfill(3) + "-intent.json"), request)
        if identity:
            self.login_attempts.add(identity)
            self.login_statuses[identity] = None
        if method == "POST" and parsed.path == "/emby/Library/VirtualFolders":
            self.library_attempts.add(next(name for name in SOURCE_BATCHES if body == library_body(name)))
        if task_operation and task_operation[1] == self.selected_id:
            self.trigger_write_possible = self.trigger_write_possible or task_operation[0] == "triggers"
            self.run_possible = self.run_possible or task_operation[0] == "start"
        self.record_count += 1
        connection = http.client.HTTPConnection("127.0.0.1", PORT, timeout=min(5, remaining))
        signal.setitimer(signal.ITIMER_REAL, min(15, remaining))
        try:
            connection.request(method, path, wire, headers)
            response = connection.getresponse()
            content = response.read(limit)
            complete = response.isclosed() or response.length == 0
            status, response_headers = response.status, response.getheaders()
        except Exception as error:
            self.charged_bytes += limit
            self.incomplete_count += 1
            if mutation is not None:
                mutation["transportFailure"] = type(error).__name__
            self.write(label, {"request": request, "observation": {"completeHTTP": False, "captureIncomplete": True,
                "transportFailure": type(error).__name__, "unmeasuredWireBytesUpperBound": limit,
                "requestStartedAtUtc": started_at, "requestFinishedAtUtc": base.dt.datetime.now(base.dt.timezone.utc).isoformat()}})
            raise
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            connection.close()
        self.total += len(content)
        self.charged_bytes += len(content)
        self.incomplete_count += int(not complete)
        text = content.decode("utf-8", errors="replace")
        try:
            result, kind = json.loads(text), "json"
        except json.JSONDecodeError:
            result, kind = text, "text"
        if mutation is not None:
            mutation["responseStatus"] = status
        # Record all acknowledged effects before persistence or DTO checks.
        if task_operation:
            self.task_mutation_acks.append({"capture": label, "operation": task_operation[0], "targetId": task_operation[1], "status": status})
        if method == "POST" and parsed.path == "/emby/Library/VirtualFolders":
            self.library_mutation_acks.append({"capture": label, "libraryName": body["Name"], "status": status})
        if identity:
            self.login_statuses[identity] = status
        if identity and isinstance(result, dict) and isinstance(result.get("AccessToken"), str) and result["AccessToken"]:
            self.secrets.add(result["AccessToken"])
            if result["AccessToken"] not in self.forbidden_tokens:
                self.logins[identity] = result
                mutation["acknowledgedLogin"] = True
            else:
                self.cleanup_errors.append({"stage": "login", "identity": identity, "error": "ExistingCredentialReturned"})
            base.save(base.PRIVATE / (identity + "-login-response.json"), result)
        self.write(label, {"request": request, "response": {"status": status, "headers": response_headers, "bodyType": kind, "body": result},
                           "observation": {"completeHTTP": complete, "captureIncomplete": not complete, "wireBytes": len(content),
                               "requestStartedAtUtc": started_at, "requestFinishedAtUtc": base.dt.datetime.now(base.dt.timezone.utc).isoformat(),
                               "elapsedSeconds": round(time.monotonic() - started_monotonic, 6)}})
        print(json.dumps({"capture": base.PREFIX + label, "status": status, "completeHTTP": complete, "bytes": len(content)}), flush=True)
        base.require(complete, "Task response exceeded its bounded capture")
        return status, result

    def observe_tasks(self, label: str, *, initial: bool = False) -> dict:
        status, rows = self.request(label, "GET", "/emby/ScheduledTasks", token=self.admin())
        base.require(status == 200 and isinstance(rows, list) and 0 < len(rows) <= 64 and all(isinstance(row, dict) and
                     isinstance(row.get("Id"), str) and base.re.fullmatch(r"[A-Za-z0-9_-]{1,128}", row["Id"]) for row in rows),
                     "Fresh task list is not a complete bounded definition set")
        by_id = {row["Id"]: row for row in rows}
        base.require(len(by_id) == len(rows) and UNKNOWN_ID not in by_id and not any(row.get("Key") == UNKNOWN_ID for row in rows),
                     "Task identity membership or missing-ID nonce is ambiguous")
        matches = [row for row in rows if row.get("Key") == "RefreshLibrary"]
        base.require(len(matches) == 1, "Exactly one fresh RefreshLibrary definition is required")
        selected = matches[0]
        if initial:
            self.selected_id = selected["Id"]
            self.task_ids = set(by_id)
            self.initial_task_membership = set(by_id)
            self.all_task_baseline = copy.deepcopy(by_id)
            base.require(isinstance(selected.get("Triggers"), list) and len(selected["Triggers"]) <= 8, "Original trigger array is unavailable or unbounded")
            self.initial_triggers = copy.deepcopy(selected["Triggers"])
            base.save(base.PRIVATE / "initial-task-definitions.json", rows)
            base.save(base.PRIVATE / "initial-refresh-library-triggers.json", self.initial_triggers)
        else:
            base.require(set(by_id) == self.initial_task_membership and selected["Id"] == self.selected_id, "Fresh task definition membership changed")
            changes = [task_id for task_id, row in by_id.items() if task_id != self.selected_id and static_task(row) != static_task(self.all_task_baseline[task_id])]
            if changes:
                self.other_task_changes.append({"capture": label, "taskIds": changes})
            base.require(not changes, "An unrelated fresh task configuration changed")
        self.latest_task = selected
        self.task_write_proof = {"taskId": selected["Id"], "taskIdentifier": "RefreshLibrary", "state": selected.get("State"),
                                 "capture": label, "requestSequence": self.record_count}
        self.task_observations.append({"capture": label, "selectedTask": self.task_summary(selected), "definitionCount": len(rows),
                                       "otherTaskConfigurationChecked": not initial})
        return selected

    def observe_detail(self, label: str) -> dict:
        status, item = self.request(label, "GET", "/emby/ScheduledTasks/" + quote(self.selected_id, safe=""), token=self.admin())
        base.require(status == 200 and isinstance(item, dict) and item.get("Id") == self.selected_id and item.get("Key") == "RefreshLibrary",
                     "Fresh positive task detail no longer identifies the owned definition")
        self.latest_task = item
        self.task_write_proof = {"taskId": self.selected_id, "taskIdentifier": "RefreshLibrary", "state": item.get("State"),
                                 "capture": label, "requestSequence": self.record_count}
        self.task_observations.append({"capture": label, "selectedTask": self.task_summary(item), "detailIdentityMatched": True})
        return item

    def libraries(self, label: str) -> list[dict]:
        status, result = self.request(label, "GET", "/emby/Library/VirtualFolders/Query?StartIndex=0&Limit=4", token=self.admin())
        base.require(status == 200 and isinstance(result, dict) and isinstance(result.get("Items"), list) and
                     isinstance(result.get("TotalRecordCount"), int) and not isinstance(result["TotalRecordCount"], bool) and
                     result["TotalRecordCount"] == len(result["Items"]) <= 2,
                     "Fresh library membership is not complete or exceeds the two-library bound")
        rows = result["Items"]
        for row in rows:
            matches = [name for name in SOURCE_BATCHES if row.get("Name") == LIBRARY_NAMES[name] and row.get("Locations") == [str(SOURCE_BATCHES[name])]]
            base.require(len(matches) == 1 and matches[0] in self.library_attempts and isinstance(row.get("ItemId"), str) and row["ItemId"],
                         "A library is outside the two attempted owned source batches")
            self.libraries_owned[matches[0]] = row
        base.require(len({row["ItemId"] for row in rows}) == len(rows), "Owned library identifiers collide")
        self.latest_library_count = len(rows)
        self.library_proof_sequence = self.record_count
        return rows

    def ensure_proof(self, label: str) -> dict:
        if self.task_write_proof is None or self.task_write_proof.get("requestSequence") != self.record_count:
            return self.observe_detail(label)
        return self.latest_task

    def mutate_task(self, label: str, operation: str, *, actor: str = "control", target: str | None = None,
                    body: object = MISSING, audit_others: bool = True) -> tuple[int, dict]:
        base.require(audit_others or self.finishing and actor == "control" and target in {None, self.selected_id},
                     "Only owned target restoration may defer the unrelated-task audit")
        before = copy.deepcopy(self.ensure_proof(label + "-owned-proof"))
        method, path = self.task_route(operation, target or self.selected_id)
        token = "" if actor == "anonymous" else self.logins[actor]["AccessToken"]
        status, _ = self.request(label, method, path, token=token, body=body)
        after = self.observe_tasks(label + "-after") if audit_others else self.observe_detail(label + "-after")
        self.experiments.append({"capture": label, "operation": operation, "actor": actor, "targetId": target or self.selected_id,
            "status": status, "before": self.task_summary(before), "after": self.task_summary(after),
            "resultChanged": execution_fingerprint(before) != execution_fingerprint(after)})
        return status, after

    def wait_idle(self, label: str, *, before_result: str | None = None) -> dict:
        prior_deadline = self.deadline
        self.deadline = min(prior_deadline, time.monotonic() + STOP_SECONDS)
        try:
            for index in range(POLL_LIMIT):
                item = self.observe_detail(label + "-" + str(index))
                changed = before_result is None or execution_fingerprint(item) != before_result
                if item.get("State") == "Idle" and changed:
                    self.poll_observations.append({"capture": label, "observations": index + 1, "terminalTask": self.task_summary(item),
                                                   "newResultRequired": before_result is not None})
                    return item
                remaining = self.deadline - time.monotonic()
                base.require(remaining > 0, "Owned task did not settle within its bounded observation window")
                time.sleep(min(1.5, remaining))
            raise RuntimeError("Owned task exhausted its bounded settlement observations")
        finally:
            self.deadline = prior_deadline

    def settle_empty(self, label: str) -> dict:
        item = self.latest_task or self.observe_detail(label + "-state")
        if item.get("State") != "Idle":
            return self.wait_idle(label)
        return item

    def clear_triggers(self, label: str) -> None:
        self.settle_empty(label + "-settle")
        _status, after = self.mutate_task(label, "triggers", body=[])
        if after.get("State") != "Idle":
            after = self.wait_idle(label + "-idle")
        base.require(after.get("Triggers") == [], "Owned trigger clearing could not be verified")

    def capture(self) -> None:
        status, public = self.request("public-before", "GET", "/emby/System/Info/Public")
        base.require(status == 200 and isinstance(public, dict) and public.get("Version") == "4.9.5.0" and
                     public.get("Id") == self.manifest["serverId"], "Fresh task server identity differs")
        self.login("control")
        self.login("viewer")
        status, users = self.request("users-baseline", "GET", "/emby/Users", token=self.admin())
        base.require(status == 200 and isinstance(users, list), "Fresh user baseline is unavailable")
        self.old_users = users
        rows = self.devices("devices-baseline")
        self.control_id = self.find_owned(rows, "control", "Control")
        self.old_devices = {row["Id"]: row for row in rows if not self.owned_pair(row)}
        base.require(set(self.old_devices) == {row["Id"] for row in self.manifest["bootstrapDevices"]}, "Fresh bootstrap device membership changed")
        self.read_ids.update(self.old_devices)
        for index, device_id in enumerate(sorted(self.old_devices)):
            status, body = self.request("old-options-before-" + str(index), "GET", self.route("/Options", device_id), token=self.admin())
            self.old_options[device_id] = {"status": status, "body": body}
        self.observe_tasks("task-definitions-initial", initial=True)
        self.observe_detail("refresh-library-initial-detail")
        self.libraries("libraries-empty-baseline")
        base.require(self.latest_library_count == 0, "Trigger and permission experiments require an empty library")
        self.observe_detail("empty-library-task-baseline")
        self.settle_empty("initial-task-idle")
        status, _ = self.request("unknown-task-before", "GET", "/emby/ScheduledTasks/" + UNKNOWN_ID, token=self.admin())
        base.require(status == 404, "Reserved unknown task identity was not absent")
        self.unknown_proved = True
        self.current_experiment = "empty-library-permissions"
        self.clear_triggers("initial-clear")
        for actor in ("anonymous", "viewer", "control"):
            targets = [UNKNOWN_ID] if actor == "control" else [self.selected_id, UNKNOWN_ID]
            for target in targets:
                for operation in ("triggers", "start", "stop", "stop-alias"):
                    self.settle_empty("permission-idle-" + actor + "-" + ("unknown" if target == UNKNOWN_ID else "known") + "-" + operation)
                    label = "permission-" + actor + "-" + ("unknown" if target == UNKNOWN_ID else "known") + "-" + operation
                    self.mutate_task(label, operation, actor=actor, target=target, body=[] if operation == "triggers" else MISSING)
                    self.settle_empty(label + "-settle")
                    base.require(self.latest_task.get("Triggers") == [], "Permission or unknown-ID write changed the cleared schedule")
        for operation in ("stop", "stop-alias"):
            self.mutate_task("idle-" + operation, operation)
        self.current_experiment = "empty-library-triggers"
        for name, value in [("original", self.initial_triggers), *TRIGGER_CASES]:
            self.libraries("trigger-" + name + "-empty-proof")
            base.require(self.latest_library_count == 0, "Trigger experiments no longer have an empty library")
            self.ensure_proof("trigger-" + name + "-owned-proof")
            self.settle_empty("trigger-" + name + "-idle")
            self.mutate_task("trigger-" + name, "triggers", body=copy.deepcopy(value))
            self.clear_triggers("trigger-" + name + "-clear")
        self.current_experiment = "empty-library-manual-start"
        before = self.observe_detail("manual-empty-before")
        base.require(before.get("State") == "Idle" and before.get("Triggers") == [], "Manual empty start lacks an idle unscheduled baseline")
        status, _ = self.mutate_task("manual-empty-start", "start")
        if status in {200, 204}:
            self.wait_idle("manual-empty-result", before_result=execution_fingerprint(before))
        for batch, stop_operation in (("a", "stop"), ("b", "stop-alias")):
            self.cancellation_case(batch, stop_operation)

    def cancellation_case(self, batch: str, stop_operation: str) -> None:
        self.current_experiment = "owned-batch-" + batch
        self.libraries("batch-" + batch + "-libraries-before")
        self.observe_detail("batch-" + batch + "-before-create")
        self.settle_empty("batch-" + batch + "-precreate-idle")
        status, _ = self.request("batch-" + batch + "-library-create", "POST", "/emby/Library/VirtualFolders", token=self.admin(), body=library_body(batch))
        self.libraries("batch-" + batch + "-libraries-after-create")
        base.require(batch in self.libraries_owned, "The single owned library creation attempt was not discoverable")
        self.experiments.append({"experiment": self.current_experiment, "libraryCreateStatus": status,
                                 "libraryId": self.libraries_owned[batch]["ItemId"], "sourceBatch": batch, "sourceFiles": 256})
        after_create = self.observe_tasks("batch-" + batch + "-task-after-create")
        self.settle_empty("batch-" + batch + "-automatic-work-idle")
        before = self.observe_detail("batch-" + batch + "-manual-before")
        base.require(before.get("State") == "Idle" and before.get("Triggers") == [], "Owned library manual start lacks a new idle unscheduled baseline")
        self.start_counts[batch] = self.start_counts.get(batch, 0) + 1
        base.require(self.start_counts[batch] == 1, "Only one planned scan start per source batch is permitted")
        status, after = self.mutate_task("batch-" + batch + "-manual-start", "start")
        base.require(status in {200, 204}, "Owned library manual start was not acknowledged")
        running = after.get("State") == "Running"
        for index in range(8):
            if running or after.get("State") == "Idle" and execution_fingerprint(after) != execution_fingerprint(before):
                break
            time.sleep(0.25)
            after = self.observe_detail("batch-" + batch + "-running-observation-" + str(index))
            running = after.get("State") == "Running"
        if batch == "a" and running:
            self.mutate_task("batch-a-duplicate-start", "start")
        proof = self.observe_detail("batch-" + batch + "-stop-precondition")
        if proof.get("State") == "Running":
            stop_status, _ = self.mutate_task("batch-" + batch + "-" + stop_operation, stop_operation)
            terminal = self.wait_idle("batch-" + batch + "-stop-result")
            self.experiments.append({"experiment": self.current_experiment, "stopOperation": stop_operation, "runningPrecondition": True,
                "stopStatus": stop_status, "terminalTask": self.task_summary(terminal),
                "interpretation": "Cancellation request and final result are separate observations; Completed remains a possible race"})
        else:
            terminal = self.wait_idle("batch-" + batch + "-completed-before-stop", before_result=execution_fingerprint(before))
            self.experiments.append({"experiment": self.current_experiment, "stopOperation": stop_operation, "runningPrecondition": False,
                "stopRequestIssued": False, "terminalTask": self.task_summary(terminal), "race": "Completed or settled before the stop precondition"})
        self.observe_tasks("batch-" + batch + "-all-tasks-final")
        self.experiments.append({"experiment": self.current_experiment, "afterLibraryCreation": self.task_summary(after_create),
                                 "manualStartBaseline": self.task_summary(before), "manualResultChanged": execution_fingerprint(terminal) != execution_fingerprint(before)})

    def cleanup_task(self) -> None:
        if self.selected_id is None or self.initial_triggers is None:
            return
        self.current_experiment = "cleanup"
        item = self.observe_detail("cleanup-owned-task-before")
        if item.get("State") != "Idle":
            self.mutate_task("cleanup-owned-task-stop", "stop", audit_others=False)
            self.wait_idle("cleanup-owned-task-idle")
        self.mutate_task("cleanup-restore-original-triggers", "triggers", body=copy.deepcopy(self.initial_triggers), audit_others=False)
        final = self.latest_task
        if final.get("State") != "Idle":
            self.mutate_task("cleanup-restored-task-stop", "stop", audit_others=False)
            final = self.wait_idle("cleanup-restored-task-idle")
        base.require(final.get("State") == "Idle" and final.get("Triggers") == self.initial_triggers,
                     "Original trigger array or idle state could not be restored")
        self.checks["originalTriggersRestoredAndTaskIdle"] = True

    def audit_task_configuration(self) -> None:
        if self.selected_id is None:
            return
        final = self.observe_tasks("cleanup-task-definitions-final")
        base.require(static_task(final) == static_task(self.all_task_baseline[self.selected_id]), "Owned task configuration was not exactly restored")
        self.checks["otherTaskConfigurationUnchanged"] = not self.other_task_changes
        self.checks["ownedTaskStaticConfigurationRestored"] = True

    def finish(self) -> None:
        self.finishing = True
        self.deadline = time.monotonic() + PHASE_SECONDS
        if "control" in self.logins:
            self.cleanup_step("restore-owned-task", self.cleanup_task)
            self.cleanup_step("audit-all-task-configuration", self.audit_task_configuration)
            self.cleanup_step("owned-library-membership", lambda: self.libraries("cleanup-libraries-final"))
            if self.old_devices is not None:
                self.cleanup_step("old-devices", lambda: device.Recorder.compare_devices(self))
                self.checks.pop("onlyControlOwnedDeviceRetained", None)
            if self.old_users is not None:
                self.cleanup_step("old-users", self.compare_users)
            self.cleanup_step("sessions-before-logout", lambda: self.request("cleanup-sessions-before-logout", "GET", "/emby/Sessions", token=self.admin()))
        for identity in ("viewer", "control"):
            self.cleanup_step("logout-" + identity, lambda identity=identity: self.logout(identity))
        self.checks["allAcknowledgedLoginsInvalid"] = set(self.logins) == set(self.invalid_login_proofs)
        self.checks["allLoginAttemptsAcknowledgedOrRejected"] = all(name in self.logins or self.login_statuses.get(name) in {400, 401, 403} for name in self.login_attempts)
        self.checks["freshAndOriginalProcessAuthorityPreserved"] = self.cleanup_step("process-authority", preconditions) == self.pid
        self.checks["oldEvidenceAndAllOwnedSourcesUnchanged"] = self.cleanup_step("source-and-evidence-audit", self.snapshot) == self.baseline
        self.cleanup_step("mutation-journal", lambda: base.save(base.PRIVATE / "mutation-results.json", self.mutations))
        redaction_ok = True
        for path in base.RAW.glob(base.PREFIX + "*.json"):
            base.private_file(path)
            base.private_file(base.EXPORT / path.name)
            redaction_ok = redaction_ok and self.sanitize(json.loads(path.read_text())) == json.loads((base.EXPORT / path.name).read_text())
        self.checks["newRedactionAuditPassed"] = redaction_ok
        self.cleanup_ok = not self.cleanup_errors and all(self.checks.values())
        self.write("audit", {"kind": "capture-audit-observation", "captureFailureType": self.capture_failure, "cleanupPassed": self.cleanup_ok,
            "checks": self.checks, "cleanupErrors": self.cleanup_errors, "referencePID": self.pid, "serverId": self.manifest["serverId"],
            "mutableTaskId": self.selected_id, "mutableTaskIdentifier": "RefreshLibrary", "initialTriggers": self.initial_triggers,
            "experiments": self.experiments, "taskObservations": self.task_observations, "pollObservations": self.poll_observations,
            "taskMutationAcknowledgements": self.task_mutation_acks, "libraryMutationAcknowledgements": self.library_mutation_acks,
            "libraryCreationAttempts": sorted(self.library_attempts), "ownedLibraryIds": [row["ItemId"] for row in self.libraries_owned.values()],
            "ownedLibrariesLeftInsideDisposableDataForOperatorTeardown": True, "otherTaskConfigurationChanges": self.other_task_changes,
            "ownedLoginInvalidityProofs": self.invalid_login_proofs, "ownedLoginLogoutStatuses": self.logout_statuses,
            "ownedLoginAttempts": sorted(self.login_attempts), "ownedLoginStatuses": self.login_statuses,
            "retainedOwnedDeviceHistory": None if self.final_devices is None else [row for row in self.final_devices if self.owned_pair(row)],
            "attributedUserActivityDifferences": self.user_date_changes, "preservedPriorRecords": PRIOR_RECORDS,
            "preservedSetupRecords": self.manifest["setupRecordCount"], "preservedOldRecordFiles": len(self.baseline["records"]),
            "preservedOldMediaFiles": len(self.baseline["media"]), "preservedOwnedSourceFiles": len(self.baseline["ownedSourceFiles"]),
            "historicalRemovedPathsRemainHistorical": self.baseline["historicalRemovedPaths"], "originalServiceHTTPRequests": 0,
            "originalTaskMutations": 0, "applicationKeyRequests": 0, "userPolicyWrites": 0, "sourceWrites": 0,
            "freshServiceLeftAliveForOperatorTeardown": True, "httpAttempts": self.record_count, "incompleteHTTP": self.incomplete_count,
            "wireBytes": self.total, "wireByteBudgetCharged": self.charged_bytes, "elapsedSeconds": round(time.monotonic() - self.started, 3),
            "limits": {"mainHTTPAttempts": MAX_MAIN_REQUESTS, "allHTTPAttempts": MAX_REQUESTS, "responseBytes": base.MAX_BODY,
                       "totalResponseBytes": base.MAX_TOTAL, "phaseSeconds": PHASE_SECONDS, "stopObservationSeconds": STOP_SECONDS,
                       "pollObservationsPerWindow": POLL_LIMIT, "libraryCreations": 2, "filesPerBatch": 256}})
        base.require(self.cleanup_ok, "Fresh task cleanup or preservation proof is incomplete")

    def write(self, label: str, value: dict) -> None:
        value["transportAuthority"] = {"unit": UNIT, "pid": self.manifest["pid"], "startTicks": self.manifest["startTicks"],
                                       "networkNamespace": self.manifest["networkNamespace"], "port": PORT}
        super().write(label, value)


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
    print(json.dumps({"capture": base.PREFIX, "result": "complete" if failure is None else "partial", "failureType": failure, "cleanup": recorder.cleanup_ok}), flush=True)
    if failure:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
