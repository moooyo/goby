#!/usr/bin/env python3
"""Run the frozen Phase 3 build and regression campaign on the owned guest.

This runner never provisions services, admits a workload, resets a guest, or
retries a failed command. Raw logs are private; its report retains every failure.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import select
import shutil
import signal
import stat
import subprocess
import sys
import time
from urllib.parse import parse_qsl, unquote, urlsplit


SAFE = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,95}\Z")
SHA = re.compile(r"[a-f0-9]{64}\Z")
MODULE = "github.com/moooyo/goby"
BACKUP_PACKAGES = {MODULE + "/internal/" + name for name in ("backuppg", "recoverydb", "recovery")}


class Failure(RuntimeError):
    pass


def need(value, code):
    if not value:
        raise Failure(code)


def canonical(value):
    return (json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False) + "\n").encode()


def file_hash(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def database_identity(value, *, backup=False):
    need(type(value) is str and value and not any(character in value for character in "\0\r\n"), "database_url_required")
    parsed = urlsplit(value)
    need(parsed.scheme in {"postgres", "postgresql"} and parsed.hostname in {"127.0.0.1", "::1"}
         and parsed.port == 55461 and parsed.username and parsed.password and not parsed.fragment,
         "owned_database_url")
    need(parse_qsl(parsed.query, keep_blank_values=True, strict_parsing=True) in ([], [("sslmode", "disable")]), "database_url_overrides")
    name, role = unquote(parsed.path.removeprefix("/")), unquote(parsed.username)
    pattern = r"goby_backup_phase3_[a-z0-9_]{1,45}" if backup else r"goby_phase3_[a-z0-9_]{1,48}"
    need(re.fullmatch(pattern, name) and re.fullmatch(pattern, role), "owned_database_name")
    # IPv4 and IPv6 spellings address the same frozen local PostgreSQL cluster.
    # Credentials and role aliases cannot turn one database into two identities.
    return parsed.port, name


def read_private(path, expected=None):
    path = Path(path)
    need(path.is_absolute() and path.resolve(strict=True) == path, "private_path")
    fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(fd)
        need(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1
             and stat.S_IMODE(info.st_mode) == 0o600 and 0 < info.st_size <= 16 << 20, "private_file")
        with os.fdopen(fd, "rb", closefd=False) as stream:
            raw = stream.read((16 << 20) + 1)
        after = os.fstat(fd)
        need((info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns) ==
             (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns)
             and len(raw) == info.st_size, "private_file_changed")
        if expected is not None:
            need(SHA.fullmatch(expected) and hashlib.sha256(raw).hexdigest() == expected, "private_file_hash")
        def pairs(rows):
            value = {}
            for key, item in rows:
                need(key not in value, "duplicate_json_key")
                value[key] = item
            return value
        return json.loads(raw, object_pairs_hook=pairs,
                          parse_constant=lambda _: (_ for _ in ()).throw(Failure("nonfinite_json")))
    finally:
        os.close(fd)


def write_new(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as stream:
        stream.write(canonical(value))
        stream.flush()
        os.fsync(stream.fileno())
    directory = os.open(Path(path).parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


def process_identity(pid):
    fields = Path("/proc/%d/stat" % pid).read_text().rsplit(")", 1)[1].split()
    return {"pid": pid, "start_ticks": fields[19], "process_group": int(fields[2])}


def source_inventory(root):
    result = []
    for path in sorted(root.rglob("*"), key=lambda value: value.relative_to(root).as_posix()):
        info = path.lstat()
        need(not stat.S_ISLNK(info.st_mode), "source_symlink")
        if stat.S_ISDIR(info.st_mode):
            continue
        need(stat.S_ISREG(info.st_mode) and info.st_nlink == 1, "source_special_file")
        need(len(result) < 20000 and info.st_size <= 128 << 20, "source_inventory_budget")
        result.append({"path": path.relative_to(root).as_posix(), "bytes": info.st_size, "sha256": file_hash(path)})
    return result


class Runner:
    def __init__(self, profile):
        self.p = profile
        need(sys.platform == "linux" and os.geteuid() == 0, "owned_linux_guest_required")
        need(profile["schema_version"] == 1 and profile["released"] is True and profile["vmid"] == 106, "profile_not_released")
        need(SAFE.fullmatch(profile["run_id"]) and re.fullmatch(r"[a-f0-9]{40}", profile["source_revision"]), "run_identity")
        need(Path("/etc/machine-id").read_text().strip() == profile["machine_id"], "guest_identity")
        self.root = Path(profile["campaign_root"])
        need(self.root.is_absolute() and self.root.resolve(strict=True) == self.root, "campaign_root")
        self.work = self.root / "build-work" / profile["run_id"]
        need(not self.work.exists(), "run_already_exists")
        self.runtime = read_private(profile["runtime_context"]["path"], profile["runtime_context"]["sha256"])
        for key in ("schema_version", "released", "vmid", "machine_id", "source_revision"):
            need(self.runtime[key] == profile[key], "runtime_scope")
        need(self.runtime["mode"] == "regression-only", "regression_database_scope")
        self.owner = read_private(self.runtime["owner_file"])
        need(self.owner["owner_id"] == profile["owner_id"] and self.owner["source_revision"] == profile["source_revision"], "runtime_owner")
        self.unit = profile["unit"]
        need(SAFE.fullmatch(self.unit) and self.unit.endswith(".service"), "runner_unit")
        result = subprocess.run(["/usr/bin/systemctl", "show", self.unit, "-p", "MainPID", "-p", "ControlGroup"],
                                capture_output=True, check=True, timeout=10)
        unit = dict(line.split("=", 1) for line in result.stdout.decode().splitlines() if "=" in line)
        self.cgroup = Path("/sys/fs/cgroup" + unit["ControlGroup"])
        need(int(unit["MainPID"]) == os.getpid() and unit["ControlGroup"] == profile["cgroup"]
             and "0::" + unit["ControlGroup"] in Path("/proc/self/cgroup").read_text().splitlines(), "runner_cgroup")
        self.identity = process_identity(os.getpid())
        self.started = time.time_ns()
        self.deadline = time.monotonic() + profile["budgets"]["total_seconds"]
        need(60 <= profile["budgets"]["total_seconds"] <= 43200, "campaign_deadline")
        need(1 << 30 <= profile["budgets"]["max_allocated_bytes"] <= 20 << 30, "campaign_disk_budget")
        need(2 << 30 <= profile["budgets"]["free_floor_bytes"] <= 12 << 30, "campaign_disk_floor")
        self.tools = self.runtime["tools"]
        for name in ("go", "node", "npm_cli", "ffmpeg", "ffprobe", "intro_fingerprint", "pg_dump", "pg_restore", "psql"):
            binding = self.tools[name]
            path = Path(binding["path"])
            info = path.lstat()
            need(path.is_absolute() and path.resolve(strict=True) == path and stat.S_ISREG(info.st_mode)
                 and info.st_uid == 0 and not info.st_mode & 0o022
                 and SHA.fullmatch(binding["sha256"]) and file_hash(path) == binding["sha256"], "tool_binding")
        self.assert_postgres()
        self.source = Path(profile["source_directory"])
        need(self.source.is_absolute() and self.source.resolve(strict=True) == self.source
             and self.source.is_relative_to(self.root / "sources"), "frozen_source_directory")
        inventory = read_private(profile["source_manifest"]["path"], profile["source_manifest"]["sha256"])
        need(inventory["source_revision"] == profile["source_revision"] and inventory["files"] == source_inventory(self.source), "frozen_source_inventory")
        self.input_inventory = inventory["files"]
        self.work.mkdir(mode=0o700)
        (self.work / "private").mkdir(mode=0o700)
        for name in ("cache", "modules", "tmp", "home", "npm-cache", "artifacts"):
            (self.work / name).mkdir(mode=0o700)
        self.copy = self.work / "source"
        shutil.copytree(self.source, self.copy, symlinks=False)
        need(source_inventory(self.copy) == self.input_inventory, "build_copy_inventory")
        self.records = []
        self.packages = []
        self.env = self.environment()
        self.allocated_peak = 0
        self.minimum_free = None
        self.resource_guard()

    def environment(self):
        env = {"PATH": str(Path(self.tools["go"]["path"]).parent) + ":" + str(Path(self.tools["node"]["path"]).parent) + ":/usr/bin:/bin",
               "HOME": str(self.work / "home"), "TMPDIR": str(self.work / "tmp"), "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8",
               "GOCACHE": str(self.work / "cache"), "GOMODCACHE": str(self.work / "modules"),
               "GOTOOLCHAIN": "local", "CGO_ENABLED": "0", "GOMAXPROCS": "2", "GOMEMLIMIT": "1200MiB",
               "GOFLAGS": "-mod=readonly -p=1", "CI": "1", "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD": "1",
               "npm_config_cache": str(self.work / "npm-cache"), "NODE_OPTIONS": "--max-old-space-size=1024",
               "GOBY_FFMPEG": self.tools["ffmpeg"]["path"], "GOBY_FFPROBE": self.tools["ffprobe"]["path"],
               "GOBY_INTRO_FINGERPRINT": self.tools["intro_fingerprint"]["path"],
               "GOBY_TEST_PG_DUMP": self.tools["pg_dump"]["path"], "GOBY_TEST_PG_RESTORE": self.tools["pg_restore"]["path"]}
        allowed = {"GOBY_TEST_DATABASE_URL", "GOBY_TEST_RECOVERY_PORT"}
        supplied = self.runtime["environment"]
        need(set(supplied) <= allowed and "GOBY_TEST_DATABASE_URL" in supplied, "regression_environment")
        need(all(type(value) is str and "\0" not in value and "\n" not in value for value in supplied.values()), "environment_value")
        env.update(supplied)
        ordinary_identity = database_identity(env["GOBY_TEST_DATABASE_URL"])
        overrides = self.runtime["environment_by_package"]
        need(set(overrides) == BACKUP_PACKAGES, "backup_package_database_pairs")
        pairs = {ordinary_identity}
        for value in overrides.values():
            need(set(value) == {"GOBY_TEST_BACKUP_SOURCE_DATABASE_URL", "GOBY_TEST_BACKUP_TARGET_DATABASE_URL",
                                "GOBY_TEST_BACKUP_DISPOSABLE_DATABASES", "GOBY_TEST_RECOVERY_PORT"}
                 and value["GOBY_TEST_BACKUP_DISPOSABLE_DATABASES"] == "1", "backup_package_environment")
            for key in ("GOBY_TEST_BACKUP_SOURCE_DATABASE_URL", "GOBY_TEST_BACKUP_TARGET_DATABASE_URL"):
                identity = database_identity(value[key], backup=True)
                need(identity not in pairs, "backup_database_reuse")
                pairs.add(identity)
            need(value["GOBY_TEST_RECOVERY_PORT"] == "55461", "backup_port_changed")
        return env

    def assert_postgres(self):
        expected = self.runtime["postgres"]
        observed = process_identity(expected["pid"])
        need(observed["start_ticks"] == str(expected["start_ticks"]), "postgres_generation_changed")
        path = Path(expected["cgroup_path"])
        need(path.is_relative_to("/sys/fs/cgroup") and path != self.cgroup
             and "0::/" + path.relative_to("/sys/fs/cgroup").as_posix()
             in Path("/proc/%d/cgroup" % expected["pid"]).read_text().splitlines(), "postgres_cgroup_changed")

    def assert_source_inputs(self):
        for expected in self.input_inventory:
            path = self.copy / expected["path"]
            info = path.lstat()
            need(stat.S_ISREG(info.st_mode) and not stat.S_ISLNK(info.st_mode)
                 and info.st_size == expected["bytes"] and file_hash(path) == expected["sha256"], "build_source_input_changed")

    def members(self):
        members = {}
        paths = [self.cgroup / "cgroup.procs", *self.cgroup.rglob("cgroup.procs")]
        for path in paths:
            try:
                pids = path.read_text().split()
            except FileNotFoundError:
                continue
            for value in pids:
                pid = int(value)
                if pid == os.getpid():
                    continue
                try:
                    members[pid] = process_identity(pid)
                except (FileNotFoundError, ProcessLookupError):
                    pass
        return members

    def resource_guard(self):
        need(time.monotonic() < self.deadline, "campaign_deadline")
        allocated, seen = 0, set()
        for base, dirs, files in os.walk(self.work, followlinks=False):
            for name in [*dirs, *files]:
                path = Path(base) / name
                try:
                    info = path.lstat()
                except FileNotFoundError:
                    continue
                key = (info.st_dev, info.st_ino)
                if key not in seen:
                    seen.add(key)
                    allocated += info.st_blocks * 512
                need(len(seen) <= 500000, "campaign_file_budget")
        free = os.statvfs(self.root)
        free_bytes = free.f_bavail * free.f_frsize
        self.allocated_peak = max(self.allocated_peak, allocated)
        self.minimum_free = free_bytes if self.minimum_free is None else min(self.minimum_free, free_bytes)
        need(allocated <= self.p["budgets"]["max_allocated_bytes"] and free_bytes >= self.p["budgets"]["free_floor_bytes"], "campaign_storage_budget")

    def close_children(self):
        # Only actual members of this exact owned service may be signaled.
        # pidfds prevent PID reuse from redirecting cleanup to another process.
        for pid, before in self.members().items():
            try:
                fd = os.pidfd_open(pid)
                try:
                    if process_identity(pid) == before and pid in self.members():
                        signal.pidfd_send_signal(fd, signal.SIGKILL)
                finally:
                    os.close(fd)
            except (FileNotFoundError, ProcessLookupError):
                pass
        until = time.monotonic() + 10
        while self.members() and time.monotonic() < until:
            time.sleep(0.1)
        return not self.members()

    def stage(self, name, argv, timeout, cwd=None, environment=None):
        need(SAFE.fullmatch(name) and not self.members(), "stage_not_exclusive")
        self.assert_postgres()
        self.assert_source_inputs()
        self.resource_guard()
        directory = self.work / "private"
        outputs = [directory / (name + suffix) for suffix in (".stdout", ".stderr")]
        files = [path.open("xb") for path in outputs]
        for path in outputs:
            path.chmod(0o600)
        started = time.time_ns()
        record = {"name": name, "started_unix_ns": started, "status": "failed", "exit_code": None,
                  "timed_out": False, "forced_cleanup": False, "children_closed": False}
        self.records.append(record)
        process = None
        error_code = None
        try:
            process = subprocess.Popen(argv, cwd=cwd or self.copy, env=environment or self.env,
                                       stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True)
            record["process"] = process_identity(process.pid)
            pipes = {process.stdout.fileno(): (process.stdout, files[0]), process.stderr.fileno(): (process.stderr, files[1])}
            count = 0
            deadline = min(self.deadline, time.monotonic() + timeout)
            next_guard = time.monotonic() + 10
            while pipes or process.poll() is None:
                if time.monotonic() >= deadline:
                    record["timed_out"] = True
                    raise Failure("stage_deadline")
                if time.monotonic() >= next_guard:
                    self.resource_guard()
                    next_guard = time.monotonic() + 10
                ready, _, _ = select.select(list(pipes), [], [], 0.2) if pipes else ([], [], [])
                if not pipes:
                    time.sleep(0.1)
                for descriptor in ready:
                    raw = os.read(descriptor, 65536)
                    if not raw:
                        pipes.pop(descriptor)[0].close()
                        continue
                    count += len(raw)
                    need(count <= 256 << 20, "stage_log_budget")
                    pipes[descriptor][1].write(raw)
            record["exit_code"] = process.wait(timeout=1)
            until = time.monotonic() + 10
            while self.members() and time.monotonic() < until:
                time.sleep(0.1)
            record["children_closed"] = not self.members()
            need(record["children_closed"], "stage_children_remain")
            need(record["exit_code"] == 0, "stage_nonzero_exit")
            record["status"] = "passed"
        except Exception as error:
            error_code = str(error) if isinstance(error, Failure) else "stage_execution_failed"
        finally:
            if self.members():
                record["forced_cleanup"] = True
                record["children_closed"] = self.close_children()
            if process is not None:
                try:
                    record["exit_code"] = process.wait(timeout=2)
                except subprocess.TimeoutExpired:
                    record["children_closed"] = False
                for stream in (process.stdout, process.stderr):
                    stream.close()
            record["children_closed"] = not self.members()
            for stream in files:
                stream.flush()
                os.fsync(stream.fileno())
                stream.close()
            record.update(completed_unix_ns=time.time_ns(), error_code=error_code,
                          logs=[{"name": path.name, "bytes": path.stat().st_size, "sha256": file_hash(path)} for path in outputs])
            write_new(directory / (name + ".receipt.json"), record)
            print(json.dumps({"stage": name, "status": record["status"], "children_closed": record["children_closed"]}), flush=True)
        need(record["children_closed"], "owned_children_not_closed")
        return record["status"] == "passed", outputs[0]

    def go_summary(self, path):
        terminal, packages = {}, {}
        with path.open("rb") as stream:
            for raw in stream:
                need(len(raw) <= 2 << 20, "go_event_bound")
                event = json.loads(raw)
                action = event.get("Action")
                if action not in {"pass", "fail", "skip"}:
                    continue
                package, name = event.get("Package"), event.get("Test")
                if name:
                    terminal[(package, name)] = action
                else:
                    packages[package] = action
        parents = [{"package": key[0], "test": key[1], "status": value}
                   for key, value in sorted(terminal.items()) if "/" not in key[1]]
        return {"parent_results": parents, "package_results": packages,
                "passed": sum(row["status"] == "pass" for row in parents),
                "failed": sum(row["status"] == "fail" for row in parents),
                "skipped": sum(row["status"] == "skip" for row in parents)}

    def run(self):
        go, node, npm = (self.tools[name]["path"] for name in ("go", "node", "npm_cli"))
        testdir = self.copy / "scripts/test-env"
        python_tests = sorted({*testdir.glob("media-analysis-phase3-*-tests.py"), *testdir.glob("test-media-analysis-phase3-*.py")})
        need(4 <= len(python_tests) <= 20, "python_test_inventory")
        for index, path in enumerate(python_tests):
            self.stage("python-%02d" % index, ["/usr/bin/python3", "-I", "-B", str(path)], 180)
        frontend_ok, _ = self.stage("frontend-install", [node, npm, "ci", "--no-audit", "--no-fund"], 1800, self.copy / "web/admin")
        if frontend_ok:
            frontend_ok, _ = self.stage("frontend-build", [node, npm, "run", "build"], 900, self.copy / "web/admin")
        frontend = None
        if frontend_ok:
            directory = self.copy / "web/admin/dist"
            files = source_inventory(directory)
            need(any(row["path"] == "index.html" for row in files), "frontend_index_missing")
            frontend = {"directory": str(directory), "source_revision": self.p["source_revision"], "files": files}
        self.stage("build-ordinary", [go, "build", "-trimpath", "-o", str(self.work / "artifacts/goby"), "./cmd/goby"], 1800)
        if frontend_ok:
            self.stage("build-embedded", [go, "build", "-trimpath", "-tags=goby_embed_admin", "-o",
                                         str(self.work / "artifacts/goby-embedded"), "./cmd/goby"], 1800)
        discovered, packages_path = self.stage("go-package-inventory", [go, "list", "./..."], 600)
        if discovered:
            self.packages = packages_path.read_text().splitlines()
            need(1 <= len(self.packages) <= 100 and len(set(self.packages)) == len(self.packages)
                 and all(value.startswith(MODULE + "/") and re.fullmatch(r"[A-Za-z0-9_./-]+", value) for value in self.packages), "package_inventory")
            for index, package in enumerate(self.packages):
                env = dict(self.env, **self.runtime["environment_by_package"].get(package, {}))
                _, log = self.stage("go-package-%02d" % index, [go, "test", "-json", "-p=1", "-parallel=1", "-count=1", "-timeout=90m", package], 5700, environment=env)
                write_new(self.work / "private" / ("go-package-%02d.summary.json" % index), self.go_summary(log))
            if frontend_ok:
                _, log = self.stage("go-embedded-command", [go, "test", "-json", "-tags=goby_embed_admin", "-p=1", "-parallel=1",
                                                          "-count=1", "-timeout=15m", "./cmd/goby"], 960)
                write_new(self.work / "private/go-embedded-command.summary.json", self.go_summary(log))
        self.assert_postgres()
        self.assert_source_inputs()
        need(source_inventory(self.source) == self.input_inventory, "frozen_source_changed")
        if frontend is not None:
            need(source_inventory(Path(frontend["directory"])) == frontend["files"], "frontend_changed_after_build")
        self.resource_guard()
        artifacts = [{"name": path.name, "bytes": path.stat().st_size, "sha256": file_hash(path)}
                     for path in sorted((self.work / "artifacts").iterdir()) if path.is_file()]
        report = {"schema_version": 1, "run_id": self.p["run_id"], "source_revision": self.p["source_revision"],
                  "vmid": 106, "machine_id": self.p["machine_id"], "runner": self.identity,
                  "started_unix_ns": self.started, "completed_unix_ns": time.time_ns(), "stages": self.records,
                  "go_packages": self.packages, "artifacts": artifacts, "frontend": frontend, "allocated_peak_bytes": self.allocated_peak,
                  "minimum_free_bytes": self.minimum_free, "children_closed": not self.members(),
                  "postgres_closed": False, "databases_retained": True, "capacity_accepted": False,
                  "fault_recovery_accepted": False,
                  "status": "passed" if self.records and all(row["status"] == "passed" for row in self.records) else "failed"}
        write_new(self.work / "regression-result.json", report)
        return 0 if report["status"] == "passed" else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--profile", required=True)
    arguments = parser.parse_args()
    os.umask(0o077)
    runner = None
    try:
        runner = Runner(read_private(arguments.profile))
        return runner.run()
    except Exception as error:
        code = str(error) if isinstance(error, Failure) else "campaign_failed"
        result = {"status": "failed", "error_code": code, "children_closed": None}
        if runner is not None:
            result.update(children_closed=runner.close_children(), stages=runner.records,
                          source_revision=runner.p["source_revision"])
            path = runner.work / "regression-failure.json"
            if not path.exists():
                write_new(path, result)
        print(json.dumps({"status": "failed", "error_code": code}), flush=True)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
