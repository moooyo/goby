#!/usr/bin/env python3
"""Attest the already-running source55 candidate without another service start.

The failed restart02 terminal remains unchanged. This independent single-use
reader binds its one acknowledged start, samples the current fixed process,
makes exactly three public GETs, and compares complete owned database and
non-diagnostic filesystem state. It never explains the unrecorded first failed
binding field, invokes the old worker, or reads active diagnostic file bytes.
"""

from __future__ import annotations

import argparse
import ast
import base64
from copy import deepcopy
from datetime import datetime, timezone
import fcntl
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import types

W = Path("/opt/goby-test/exec-work-m3e")
ROOT = W / "candidate-source55-restart-attestation-02"
OLD = W / "candidate-source55-restart-02"
HELPER = OLD / "restart-source55-candidate.py"
HELPER_SHA = "e0667ddda789c0cb0101cbc8df2e6289a7049330605701ff9ee5517b03253b1b"
PID = 1814145
INVOCATION = "3d9fccdb4f4d4f129ee02b33f6c73ce1"
INODE = 2825322
OUTER = W / "reference-nextup-client-discovery-execution-02/independent-preservation-closeout.json"
PINS = {
    "input.json": "60d0f147e8c6a554c47099a896ebd8f145cd2b76d31e264acd60510b1a164bf5",
    "terminal.json": "2476acb99b1309152b06170026e503b7be19d3480ddd41e491aa22d3f2e11760",
    "start-intent.json": "a06ab370bd8e634066e1d09ae4f72a113befd04d7f9cd8d136c85dd82fc8141a",
    "start-result.json": "14378cbc3e2531aceade4c0216e565de608e4e6f84f114857fd1e20e17ef7d0b",
    "goby-before.json": "5070fb4d23f5c6e32dfa251ddfa6170697bc60219c367a66c613d9899ef31769",
    "goby-after.json": "5d97250102df58e9c8fae467d1cea17e29e77a889d84e1c9b61f321e3596438c",
    "files-before.json": "18dba20a811e76a4e5326c89deb0bb6d3ae6584a55001a503d41f6c8b7249822",
    "files-after.json": "455fa77b532f509067d37441a2d3c7bd69ecdc15dddc43b50b4d0294f8f066f4",
    "public-probes.json": "37517e5f3dc66819f61f5a7bb8ace1921282415f10551d2defa5c3eb0985b570",
    "restart-source55-candidate.py": HELPER_SHA,
}
EXTRA_PINS = {
    W / "reference-nextup-client-discovery-execution-02/independent-client-terminal.json": "f04cd6a1b00caf16073accc7b972b2d8d42cb1ea30dee94ff3a831104f65ce04",
    W / "reference-nextup-client-discovery-execution-01/independent-client-terminal-04.json": "f123a6421aa3ff86eda190dc81bd5338b7fcdeb8ac31e09b5555f8c74cd2f001",
    OUTER: "58ca2da84067beb3a8f26fd7d6bd1cb3aca71b6b54a884f2803ef7dd6af52b6d",
}
ENV = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8"}
READONLY_OPTIONS = "-c default_transaction_read_only=on -c statement_timeout=20000 -c lock_timeout=5000"
UNITS = {"goby-client-m3e.service", "goby-foundation-test.service", "goby-emby-client-m3e.service",
         "goby-nextup-client-discovery-01.service", "goby-nextup-client-discovery-02.service"}
PG_PREFIX = ["/usr/sbin/runuser", "-u", "postgres", "--", "/usr/lib/postgresql/17/bin/psql", "-X", "--no-password",
             "-h", "/var/lib/postgresql/goby-workspace-v1/socket", "-p", "15432", "-U", "postgres", "-d"]
PG_SUFFIX = ["-v", "ON_ERROR_STOP=1", "-A", "-t", "-q"]


class AttestationError(ValueError):
    """A named independent source, observation, or preservation check failed."""


def require(value, label):
    if not value:
        raise AttestationError(label)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def encoded(value):
    return (json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True, allow_nan=False) + "\n").encode()


def precise_object(pairs):
    value = {}
    for key, item in pairs:
        require(key not in value, "duplicate-json-key")
        value[key] = item
    return value


def document(raw):
    def bad_constant(unused):
        raise AttestationError("nonfinite-json-value")
    return json.loads(raw, object_pairs_hook=precise_object, parse_constant=bad_constant)


def file_identity(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid,
            info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


class Attestor:
    def __init__(self, source_sha):
        self.source_sha = source_sha
        self.reads, self.records, self.probes = {}, {}, []
        self.active_command = self.pending_spawn = None
        self.stage = "source-admission"
        self.counters = {"subprocessCalls": 0, "posixSpawnBackendCalls": 0, "socketConnections": 0, "publicHttpAttempts": 0,
                         "forbiddenServiceCalls": 0, "forbiddenWrites": 0, "forbiddenReads": 0}
        self.root_info = ROOT.lstat()
        require(ROOT.is_dir() and not ROOT.is_symlink() and self.root_info.st_uid == 0 and
                stat.S_IMODE(self.root_info.st_mode) == 0o700 and
                {path.name for path in ROOT.iterdir()} == {"attest-source55-candidate-restart.py"}, "fresh-private-attestation-root")
        self.self_path = Path(__file__).absolute()
        require(self.self_path == ROOT / "attest-source55-candidate-restart.py", "fixed-attestor-path")
        self.read(self.self_path, source_sha)
        sys.addaudithook(self.audit)

    def read(self, path, checksum=None):
        path = Path(path)
        require(path.is_absolute() and ".." not in path.parts and all(not item.is_symlink() for item in (path, *path.parents)), "pinned-read-path")
        info = path.lstat()
        require(stat.S_ISREG(info.st_mode) and info.st_size <= 256 << 20 and not info.st_mode & 0o022, "pinned-read-type-bound")
        with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
            require(file_identity(os.fstat(stream.fileno())) == file_identity(info), "pinned-read-open-race")
            raw = stream.read((256 << 20) + 1)
            require(file_identity(os.fstat(stream.fileno())) == file_identity(info), "pinned-read-race")
        require(file_identity(path.lstat()) == file_identity(info) and len(raw) <= 256 << 20, "pinned-read-after-race")
        actual = digest(raw)
        require(checksum is None or checksum == actual, "pinned-read-sha256")
        require(str(path) not in self.reads or self.reads[str(path)] == actual, "pinned-read-changed")
        self.reads[str(path)] = actual
        return raw

    def audit(self, event, args):
        if event == "subprocess.Popen":
            executable, argv, cwd, environment = args
            values = list(argv) if isinstance(argv, (tuple, list)) else []
            show = len(values) == 5 and values[:2] == ["/usr/bin/systemctl", "show"] and values[2] in UNITS and values[3] == "--no-pager" and values[4].startswith("--property=")
            pg = (values[:len(PG_PREFIX)] == PG_PREFIX and len(values) == len(PG_PREFIX) + 1 + len(PG_SUFFIX) and
                 values[len(PG_PREFIX)] in ("postgres", "goby_client_m3e") and values[len(PG_PREFIX) + 1:] == PG_SUFFIX and
                 isinstance(environment, dict) and environment.get("PGOPTIONS") == READONLY_OPTIONS)
            expected = self.active_command
            bound = (expected is not None and expected["popenObserved"] is False and self.pending_spawn is None and cwd is None and
                     executable == expected["executable"] and values == expected["argv"] and environment == expected["environment"])
            if not (bound and (show or pg)):
                self.counters["forbiddenServiceCalls"] += 1
                raise AttestationError("subprocess-outside-readonly-contract")
            self.counters["subprocessCalls"] += 1
            require(self.counters["subprocessCalls"] <= 64, "subprocess-budget")
            expected["popenObserved"] = True
            self.pending_spawn = {"executable": executable, "argv": list(values), "environment": dict(environment)}
        elif event == "os.posix_spawn":
            expected = self.pending_spawn
            bound = (expected is not None and self.active_command is not None and self.active_command["popenObserved"] is True and
                     len(args) == 3 and args[0] == expected["executable"] and isinstance(args[1], (list, tuple)) and
                     list(args[1]) == expected["argv"] and args[2] == expected["environment"])
            if not bound:
                self.counters["forbiddenServiceCalls"] += 1
                raise AttestationError("standalone-or-mismatched-posix-spawn-forbidden")
            self.pending_spawn = None
            self.counters["posixSpawnBackendCalls"] += 1
        elif event in ("os.system", "os.fork", "os.forkpty"):
            self.counters["forbiddenServiceCalls"] += 1
            raise AttestationError("process-launch-forbidden")
        elif event == "socket.connect":
            require(args[1] == ("127.0.0.1", 18198), "public-loopback-endpoint-only")
            self.counters["socketConnections"] += 1
            require(self.counters["socketConnections"] <= 3, "three-public-connections-only")
        elif event in ("os.remove", "os.rename", "os.mkdir", "os.rmdir", "os.chmod", "os.chown", "os.link", "os.symlink", "os.truncate"):
            self.counters["forbiddenWrites"] += 1
            raise AttestationError("filesystem-mutation-forbidden")
        elif event == "open":
            path, mode, flags = args
            if not isinstance(path, (str, bytes, os.PathLike)):
                return
            name = os.fsdecode(path)
            writing = (isinstance(mode, str) and any(letter in mode for letter in "wax+") or
                       isinstance(flags, int) and bool(flags & (os.O_WRONLY | os.O_RDWR | os.O_CREAT | os.O_TRUNC | os.O_APPEND)))
            if writing and Path(name).parent != ROOT:
                self.counters["forbiddenWrites"] += 1
                raise AttestationError("write-outside-new-attestation-root")
            forbidden = ("/dev/shm/goby-emby-reference", str(W / "reference-data"), "/var/lib/goby-test/client-m3e/diagnostics/")
            proc_exe = re.fullmatch(r"/proc/([0-9]+)/exe", name)
            if (any(name == root or name.startswith(root.rstrip("/") + "/") for root in forbidden) or
                    (proc_exe is not None and int(proc_exe.group(1)) != PID)):
                self.counters["forbiddenReads"] += 1
                raise AttestationError("original-or-active-diagnostic-byte-read-forbidden")

    def publish(self, name, value, serializer=encoded):
        require(re.fullmatch(r"[a-z0-9-]+\.json", name), "new-output-name")
        now = ROOT.lstat()
        require((now.st_dev, now.st_ino, now.st_mode, now.st_uid, now.st_gid) ==
                (self.root_info.st_dev, self.root_info.st_ino, self.root_info.st_mode, self.root_info.st_uid, self.root_info.st_gid), "attestation-root-changed")
        path, raw = ROOT / name, serializer(value)
        with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), "wb") as stream:
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())
        parent = os.open(ROOT, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(parent)
        finally:
            os.close(parent)
        descriptor = {"path": str(path), "sha256": digest(raw)}
        self.records[name] = descriptor
        return descriptor

    def command(self, argv, *, text=None, timeout=10, postgres=False):
        environment = dict(ENV)
        if postgres:
            environment["PGOPTIONS"] = READONLY_OPTIONS
        require(self.active_command is None and self.pending_spawn is None, "readonly-command-already-active")
        self.active_command = {"executable": argv[0], "argv": list(argv), "environment": dict(environment), "popenObserved": False}
        try:
            result = subprocess.run(argv, input=text, text=True, capture_output=True, check=False, timeout=timeout, env=environment)
        finally:
            self.active_command = self.pending_spawn = None
        require(result.returncode == 0, "readonly-command-failed")
        return result.stdout.strip()

    def load(self, path, checksum, name):
        raw = self.read(path, checksum)
        module = types.ModuleType(name)
        module.__file__ = str(path)
        exec(compile(raw, str(path), "exec"), module.__dict__)
        return module

    def show(self, unit, fields):
        raw = self.command(["/usr/bin/systemctl", "show", unit, "--no-pager", "--property=" + ",".join(sorted(fields))])
        result = dict(line.split("=", 1) for line in raw.splitlines() if "=" in line)
        require(set(result) == fields, "exact-unit-properties")
        return result

    def worker_absent(self):
        paths = [path for path in Path("/proc").iterdir() if path.name.isdigit()]
        require(len(paths) <= 4096, "process-inventory-bound")
        needle = str(HELPER).encode()
        for path in paths:
            try:
                arguments = (path / "cmdline").read_bytes().split(b"\0")
            except (FileNotFoundError, ProcessLookupError):
                continue
            require(needle not in arguments, "old-restart-worker-still-running")

    def runtime(self):
        h = self.helper
        self.worker_absent()
        service = self.show(h.UNIT, h.CANDIDATE_FIELDS)
        require(service == self.old_terminal["observedServiceAfterFailure"] and service["MainPID"] == str(PID) and
                service["InvocationID"] == INVOCATION and service["NRestarts"] == "0", "fixed-running-service-continuity")
        h.candidate_properties(service, failed=False)
        samples = []
        for unused in range(3):
            raw = h.metadata(PID)
            executable = h.BINARY.stat()
            require(raw["uid"] == 995 and raw["exe"] == str(h.BINARY) and raw["cmdline"] == [str(h.BINARY)] and
                    raw["cgroup"] == "0::/system.slice/" + h.UNIT + "\n" and raw["bootId"] == self.fixture["process"]["boot_id"] and
                    (raw["exeDevice"], raw["exeInode"]) == (executable.st_dev, executable.st_ino) and raw["exeInode"] == INODE and
                    h.socket_owned(PID, 18198), "current-candidate-binding")
            require(not samples or raw == samples[0], "current-candidate-sampling-race")
            samples.append(raw)
        protected = self.old_intent["protectedRuntime"]
        services = {unit: self.show(unit, h.SERVICE_FIELDS) for unit in protected["services"]}
        require(services == protected["services"], "protected-services-changed")
        primary = h.metadata(protected["primary"]["pid"])
        expected_reference = protected["reference"]
        reference = {name: h.metadata(expected_reference[name]["pid"]) for name in ("application", "endpoint")}
        reference["endpoint"]["listener"] = expected_reference["endpoint"]["listener"]
        reference["workerNetworkNamespace"] = os.readlink("/proc/self/ns/net")
        listener = reference["endpoint"]["listener"]
        require(primary == protected["primary"] and reference == expected_reference and
                h.socket_owned(reference["endpoint"]["pid"], listener["port"], expected_inode=listener["socketInode"]), "protected-process-metadata-changed")
        closed = {}
        for unit, expected in self.old_intent["closedClientUnits"].items():
            value = self.show(unit, h.SERVICE_FIELDS)
            require(value == expected and value["MainPID"] == "0" and h.empty_cgroup(unit) and
                    not Path("/proc", str(self.closures[unit]["unit"]["formerPid"])).exists(), "closed-client-worker-changed")
            closed[unit] = value
        return {"candidateService": service, "candidateSamples": samples, "protectedServices": services,
                "primary": primary, "reference": reference, "closedClientUnits": closed, "originalWorkerCurrentlyAbsent": True}

    def tree(self, root):
        h = self.helper
        values, pending, total = {}, [root], 0
        while pending:
            path = pending.pop()
            info = path.lstat()
            require(not stat.S_ISLNK(info.st_mode) and (stat.S_ISDIR(info.st_mode) or stat.S_ISREG(info.st_mode)), "current-file-type")
            require(len(values) < 4096, "current-file-count")
            name = str(path.relative_to(root))
            value = {"device": info.st_dev, "inode": info.st_ino, "mode": info.st_mode, "uid": info.st_uid, "gid": info.st_gid}
            if root == h.DATA and name == "diagnostics":
                require(stat.S_ISDIR(info.st_mode), "diagnostics-directory-required")
                values[name] = value
                continue
            value.update(links=info.st_nlink, sizeBytes=info.st_size, mtimeNs=info.st_mtime_ns, ctimeNs=info.st_ctime_ns)
            if stat.S_ISREG(info.st_mode):
                total += info.st_size
                require(total <= 512 << 20 and info.st_size <= 128 << 20, "current-file-byte-bound")
                value["sha256"] = digest(self.read(path))
            else:
                pending.extend(sorted(path.iterdir(), reverse=True))
            require(file_identity(path.lstat()) == file_identity(info), "current-file-metadata-race")
            values[name] = value
        return values

    def stable_database(self, snapshot):
        value = deepcopy(snapshot)
        value["database"]["metadata"].pop("captured_at")
        return self.op.canonical_json(value)

    def public_get(self, ordinal, route):
        before = self.runtime()
        require(before == self.anchor, "pre-public-runtime-anchor")
        headers = {"Accept": "application/json", "Connection": "close"}
        intent = self.publish("public-%02d-intent.json" % ordinal,
            {"ordinal": ordinal, "method": "GET", "host": "127.0.0.1", "port": 18198, "route": route,
             "headers": headers, "candidatePid": PID, "candidateInvocationId": INVOCATION, "at": datetime.now(timezone.utc).isoformat()})
        self.counters["publicHttpAttempts"] += 1
        require(self.counters["publicHttpAttempts"] <= 3, "public-request-budget")
        connection = http.client.HTTPConnection("127.0.0.1", 18198, timeout=5)
        result = {"ordinal": ordinal, "intent": intent, "status": None, "headers": [], "bodyBase64": "", "failure": None, "completeHTTP": False}
        response = None
        raw = b""
        try:
            connection.request("GET", route, headers=headers)
            response = connection.getresponse()
            result.update(status=response.status, headers=response.getheaders())
            raw = response.read(65537)
            if len(raw) <= 65536 and not response.isclosed():
                raw += response.read(1)
            result["completeHTTP"] = len(raw) <= 65536 and response.isclosed()
        except BaseException as error:
            result["failure"] = type(error).__name__
            if isinstance(error, http.client.IncompleteRead):
                raw = error.partial[:65537]
        finally:
            connection.close()
            result.update(bodyBase64=base64.b64encode(raw).decode(), completedAt=datetime.now(timezone.utc).isoformat())
            self.probes.append(self.publish("public-%02d-response.json" % ordinal, result))
        require(result["failure"] is None and result["completeHTTP"] is True and result["status"] == 200 and
                len(result["headers"]) <= 100 and not any(key.lower() == "set-cookie" for key, value in result["headers"]), "complete-public-200-without-cookie")
        lengths = [value for key, value in result["headers"] if key.lower() == "content-length"]
        require(not lengths or len(set(lengths)) == 1 and lengths[0].isdigit() and int(lengths[0]) == len(raw), "public-http-framing")
        body = document(raw)
        require(body == {"Status": "ok"} if route == "/healthz" else body == {"Status": "ready"} if route == "/readyz"
                else isinstance(body, dict) and body.get("Id") == self.fixture["server_id"], "public-body-and-server-identity")
        require(self.runtime() == self.anchor, "post-public-runtime-anchor")

    def run(self):
        require({path.name for path in OLD.iterdir()} == set(PINS), "original-journal-file-membership")
        raw_helper = self.read(HELPER, HELPER_SHA)
        start_nodes = [node for node in ast.walk(ast.parse(raw_helper)) if isinstance(node, ast.Call) and
            isinstance(node.func, ast.Attribute) and isinstance(node.func.value, ast.Name) and node.func.value.id == "subprocess" and
            node.func.attr == "run" and node.args and isinstance(node.args[0], ast.List) and len(node.args[0].elts) == 4 and
            [item.value if isinstance(item, ast.Constant) else None for item in node.args[0].elts[:3]] == ["/usr/bin/systemctl", "--no-block", "start"]]
        require(len(start_nodes) == 1, "frozen-single-start-callsite")
        h = self.helper = self.load(HELPER, HELPER_SHA, "source55_readonly_restart_methods")

        def forbidden(*args, **kwargs):
            raise AttestationError("old-worker-entry-and-publication-forbidden")

        h.main = h.publish = forbidden
        old = {name: document(self.read(OLD / name, checksum)) for name, checksum in PINS.items() if name != "restart-source55-candidate.py"}
        self.old_terminal, self.old_intent = old["terminal.json"], old["start-intent.json"]
        terminal, intent, approved = self.old_terminal, self.old_intent, old["input.json"]
        require(terminal["status"] == "recovery_required" and terminal["startCalls"] == 1 and terminal["publicHttpRequests"] == 0 and
                terminal["failedStage"] == "bounded-readiness" and terminal["failedCheck"] == "new-candidate-process-binding" and
                terminal["retentionErrors"] == [] and terminal["retryAllowed"] is False and old["public-probes.json"] == [], "original-failure-preserved")
        require(terminal["source"] == intent["source"] == approved["source"] == {"path": str(HELPER), "sha256": HELPER_SHA} and
                terminal["inputSha256"] == intent["inputSha256"] == PINS["input.json"] and
                intent["command"] == ["/usr/bin/systemctl", "--no-block", "start", h.UNIT] and intent["maximumStartCalls"] == 1 and
                old["start-result.json"] == {"returncode": 0, "stdoutSha256": digest(b""), "stderrSha256": digest(b"")}, "original-single-start-acknowledged")
        for label, name in {"intent": "start-intent.json", "startResult": "start-result.json", "databaseBefore": "goby-before.json",
                            "databaseAfter": "goby-after.json", "filesBefore": "files-before.json", "filesAfter": "files-after.json", "probes": "public-probes.json"}.items():
            require(terminal["records"][label] == {"path": str(OLD / name), "sha256": PINS[name]}, "original-receipt-binding")
        for path, checksum in {**h.PINS, **EXTRA_PINS}.items():
            self.read(path, checksum)
        outer = document(self.read(OUTER, EXTRA_PINS[OUTER]))
        require(outer["kind"] == "nextup-client-independent-preservation-closeout" and outer["version"] == 1 and
                outer["status"] == "verified" and outer["rootCount"] == 198, "closed-client-outer-preservation")
        self.closures = {}
        for path, checksum in EXTRA_PINS.items():
            if path == OUTER:
                continue
            closure = document(self.read(path, checksum))
            require(closure["kind"] == "nextup-client-discovery-independent-terminal" and closure["version"] == 1 and
                    closure["failure"] is None and closure["clientDiscoveryEvidenceVerified"] is True and
                    closure["uiLoginAndLogoutDualChannelBound"] is True and closure["exactUIToken401MetadataVerified"] is True and
                    closure["status"] in ("client_closed_global_request_unobserved", "client_discovery_evidence_reconstructed"), "closed-client-independent-proof")
            self.closures[closure["unit"]["name"]] = closure
        require(set(self.closures) == set(intent["closedClientUnits"]) == {h.CLIENT_UNIT, h.PREVIOUS_CLIENT_UNIT}, "both-client-closures")
        op = self.op = self.load(h.READER, h.PINS[h.READER], "source55_readonly_preservation_reader")

        def readonly_command(arguments, text=None, environment=None, timeout=30):
            argv = [str(value) for value in arguments]
            require(argv[:len(PG_PREFIX)] == PG_PREFIX and len(argv) == len(PG_PREFIX) + 1 + len(PG_SUFFIX) and
                    argv[len(PG_PREFIX)] in ("postgres", "goby_client_m3e") and argv[len(PG_PREFIX) + 1:] == PG_SUFFIX and
                    isinstance(text, str) and environment is None and 0 < timeout <= 20, "frozen-readonly-postgres-reader")
            return self.command(argv, text=text, timeout=timeout, postgres=True)

        op.command = readonly_command
        op.main = op.create = op.save_state = forbidden
        self.fixture = op.precise_json(self.read(h.FIXTURE, h.PINS[h.FIXTURE]))
        original_db = [op.precise_json(self.read(OLD / name, PINS[name])) for name in ("goby-before.json", "goby-after.json")]
        baseline = op.precise_json(self.read(h.BASELINE, h.PINS[h.BASELINE]))
        require(all(self.stable_database(value) == self.stable_database(baseline) for value in original_db), "original-database-before-after-preserved")
        original_files = [h.stable_files(old[name]) for name in ("files-before.json", "files-after.json")]
        require(original_files[0] == original_files[1], "original-nondiagnostic-files-preserved")
        self.stage = "current-runtime-before"
        self.anchor = self.runtime()
        self.publish("runtime-before.json", self.anchor)
        with Path("/proc", str(PID), "exe").open("rb") as stream:
            info = os.fstat(stream.fileno())
            require(info.st_ino == INODE and (info.st_dev, info.st_ino) == (h.BINARY.stat().st_dev, h.BINARY.stat().st_ino), "current-executable-open-binding")
            executable_sha = digest(stream.read((256 << 20) + 1))
            require(file_identity(os.fstat(stream.fileno())) == file_identity(info), "current-executable-read-race")
        require(executable_sha == h.PINS[h.BINARY] and self.runtime() == self.anchor, "current-source55-executable-bytes")
        self.stage = "current-preservation-before"
        before = op.preservation_snapshot(self.fixture, 28)
        self.publish("goby-current-before.json", before, lambda value: op.canonical_json(value) + b"\n")
        require(self.stable_database(before) == self.stable_database(baseline), "current-database-before-preserved")
        files_before = {"data": self.tree(h.DATA), "media": self.tree(h.MEDIA)}
        self.publish("files-current-before.json", files_before)
        require(files_before == original_files[0], "current-nondiagnostic-files-before-preserved")
        leases_before = op.precise_json(op.postgres(h.LEASE_SQL))
        require(len(leases_before) == 1 and leases_before[0]["granted"] is True and leases_before[0]["mode"] == "ExclusiveLock" and
                leases_before[0]["usename"] == "goby_client_m3e" and leases_before[0]["client_addr"] == "127.0.0.1" and
                type(leases_before[0]["client_port"]) is int and h.socket_owned(PID, leases_before[0]["client_port"], 15432), "current-database-lease-owned")
        self.stage = "three-public-readiness-requests"
        for ordinal, route in enumerate(("/healthz", "/readyz", "/emby/System/Info/Public"), 1):
            self.public_get(ordinal, route)
        self.stage = "current-preservation-after"
        after = op.preservation_snapshot(self.fixture, 28)
        self.publish("goby-current-after.json", after, lambda value: op.canonical_json(value) + b"\n")
        require(self.stable_database(after) == self.stable_database(before), "current-database-after-preserved")
        files_after = {"data": self.tree(h.DATA), "media": self.tree(h.MEDIA)}
        self.publish("files-current-after.json", files_after)
        require(files_after == files_before == original_files[0], "current-nondiagnostic-files-after-preserved")
        leases_after = op.precise_json(op.postgres(h.LEASE_SQL))
        stable_lease = lambda row: {key: value for key, value in row.items() if key != "state"}
        require(len(leases_after) == 1 and stable_lease(leases_after[0]) == stable_lease(leases_before[0]) and
                h.socket_owned(PID, leases_after[0]["client_port"], 15432), "current-lease-continuity")
        current_after = self.runtime()
        self.publish("runtime-after.json", current_after)
        require(current_after == self.anchor and {path.name for path in OLD.iterdir()} == set(PINS), "current-runtime-and-original-journal-continuity")
        for path, checksum in list(self.reads.items()):
            self.read(path, checksum)
        require(self.counters["publicHttpAttempts"] == self.counters["socketConnections"] == 3 and
                self.active_command is None and self.pending_spawn is None and
                self.counters["posixSpawnBackendCalls"] <= self.counters["subprocessCalls"] and
                all(self.counters[key] == 0 for key in ("forbiddenServiceCalls", "forbiddenWrites", "forbiddenReads")), "independent-operation-boundary")
        return {"status": "currently_ready_preservation_verified", "currentlyReady": True, "preservationVerified": True,
                "candidateProcess": self.anchor["candidateSamples"][0], "candidateInvocationId": INVOCATION,
                "executableSha256": executable_sha, "sourceBoundSingleStartAcknowledged": True,
                "originalWorkerCurrentlyAbsent": True, "originalJournalUnchanged": True,
                "fullDatabaseEqualExceptCaptureTime": True, "nonDiagnosticDataAndMediaEqual": True,
                "diagnosticsDirectoryIdentityPreserved": True, "diagnosticContentsCompared": False,
                "databaseLeaseBefore": leases_before, "databaseLeaseAfter": leases_after,
                "protectedPrimaryReferenceProxyAndClosedClientsUnchanged": True,
                "clientClosures": {str(path): checksum for path, checksum in EXTRA_PINS.items()},
                "gobyCounts": {key: len(after["database"]["tables"][key]) for key in ("sessions", "devices", "activity_entries")}}

    def execute(self):
        result = {"kind": "source55-candidate-independent-restart-attestation", "version": 1, "status": "attestation_failed",
                  "sourceSha256": self.source_sha, "startCalls": 0, "serviceWrites": 0, "clientAcceptanceClaim": False,
                  "originalRestartTerminal": {"path": str(OLD / "terminal.json"), "sha256": PINS["terminal.json"]},
                  "originalRestartStatus": "recovery_required", "originalRestartTerminalReclassified": False,
                  "originalFirstFailedBindingField": "unknown", "originalFirstBindingRawRetained": False}
        lock_fd = None
        try:
            lock = W / "client-fixture.lock"
            before = lock.lstat()
            require(stat.S_ISREG(before.st_mode) and before.st_uid == 0 and before.st_nlink == 1 and
                    stat.S_IMODE(before.st_mode) == 0o600, "existing-fixture-lock")
            lock_fd = os.open(lock, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
            require(file_identity(os.fstat(lock_fd)) == file_identity(before), "fixture-lock-open-race")
            fcntl.flock(lock_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            result.update(self.run())
            require(file_identity(os.fstat(lock_fd)) == file_identity(lock.lstat()) == file_identity(before), "fixture-lock-preserved")
        except BaseException as error:
            result.update(status="attestation_failed", currentlyReady=False, preservationVerified=False, failedStage=self.stage,
                          errorType=type(error).__name__, failedCheck=str(error) if isinstance(error, AttestationError) else "bound-independent-observation-failed")
        finally:
            if lock_fd is not None:
                os.close(lock_fd)
        result.update(capturedAt=datetime.now(timezone.utc).isoformat(), counters=self.counters,
                      evidence=deepcopy(self.records), readSetSha256=digest(encoded(self.reads)))
        receipt = self.publish("terminal.json", result)
        print(json.dumps({"status": result["status"], "terminal": receipt, "startCalls": 0,
                          "originalRestartTerminalReclassified": False}))
        return 0 if result["status"] == "currently_ready_preservation_verified" else 2


def main():
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and
            sys.flags.isolated and sys.flags.dont_write_bytecode, "remote-root-isolated-no-bytecode")
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-sha256", required=True)
    arguments = parser.parse_args()
    require(re.fullmatch(r"[0-9a-f]{64}", arguments.source_sha256), "source-sha256-format")
    return Attestor(arguments.source_sha256).execute()


if __name__ == "__main__":
    raise SystemExit(main())
