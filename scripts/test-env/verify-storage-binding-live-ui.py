#!/usr/bin/env python3
"""Accept the frozen schema28 administrator UI in one disposable Linux scope.

The run entrypoint must be the main process of a fresh bounded controller unit.
It never adopts an old pair or changes a shared application. The worker executes
inside its own mount namespace; a later independent attest entrypoint proves the
controller's terminal state. Secrets and incomplete evidence remain private.
"""

from __future__ import annotations

import argparse
import base64
import datetime
import fcntl
import hashlib
import http.client
import http.cookies
import importlib.util
import json
import os
from pathlib import Path
import pwd
import re
import secrets
import shutil
import signal
import socket
import stat
import subprocess
import sys
import time
import types
import urllib.parse

sys.dont_write_bytecode = True
MARKER = "goby-storage-binding-live-ui-v1"
IPC_MARKER = "goby-storage-binding-live-ui-ipc-v1"
WORK = Path("/opt/goby-test/exec-work-m3e")
TOOL = WORK / "storage-binding-live-ui-tool-05"
SOURCE = WORK / "source-attempt-54"
SOURCE_MANIFEST_SHA = "c5a5cf6bf0afa973fbd2c087d300dbe6bd3079f92c7237b5a108d76ef8ae6fc3"
BINARY = WORK / "client-backup-run-20260912_053517_9cb0074fc731/tmp/goby-linux-amd64"
BINARY_SHA = "5f88c432d96825d5c3f8c2faccc64be673dab85849670707571c20fddd71594e"
WEB = WORK / "storage-binding-workflow-web-03/dist"
WEB_REPORT_SHA = "2b33e2805afb7422eeb8486db9c4f531cd074a636e4dcddc08c91c8d0808a9d7"
CATALOG_SHA = "8e7569c8fe2073ee2ed4c51147f9abc21061d1aac9554843101b826fa5a1cc2b"
CONTROL = Path("/opt/goby-test/postgres-workspace-v1")
DATA = Path("/var/lib/postgresql/goby-workspace-v1/data")
SOCKET = DATA.parent / "socket"
HBA = DATA / "pg_hba.conf"
HISTORY = CONTROL / "client-backup-pair.json"
PG = Path("/usr/lib/postgresql/17/bin")
NODE = WORK / "client-library-changed-source44-tool-01/node"
NODE_SHA = "3517c2df0b2f8cd7f422b4b8450ef81c6889f08eb03e281d6de9079b15e6a327"
NODE_MODULES = Path("/opt/goby-test/inactive-dependencies-m5h/node_modules")
BROWSER_CACHE = Path("/root/.cache/ms-playwright")
BROWSER_ROOT = BROWSER_CACHE / "chromium_headless_shell-1243"
BROWSER_EXE = BROWSER_ROOT / "chrome-headless-shell-linux64/chrome-headless-shell"
BROWSER_SHA = "ded93a9c9a53a1ae040f08124badcca95c938e9d5015ff340c3b5538c41bf39e"
BROWSERS_JSON_SHA = "545d52f8382c391e605562c330e9c1c534a16045898203037a49bb8bd769a946"
ORIGIN = "http://127.0.0.1:18288"
PORT = 18288
ENV = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8"}
RUN_RE = r"[0-9]{8}_[0-9]{6}_[0-9a-f]{12}"
HASH_RE = r"[0-9a-f]{64}"
ID_RE = r"[0-9a-f]{32}"
SOURCE_FILES = ("scripts/test-env/prepare-postgres-workspace.py", "internal/backuppg/catalog.go",
                "internal/backuppg/catalogs/schema-28-postgresql-17.json")
TOOL_FILES = frozenset(("verify-storage-binding-live-ui.py", "package.json", "package-lock.json",
                        "playwright.config.ts", "e2e/root-binding-live.spec.ts",
                        "accepted-web-report.json", "dependency-files.json"))
STAGES = ("browser_ready", "baseline_created", "unbound_exposed", "a_source2",
          "controller_approved", "a_source3", "a_unavailable", "a_restored",
          "final_bindings", "browser_logged_out")
BROWSER_CHECKS = frozenset(("browser_version_pinned", "backend_ready_before_http", "bootstrap_created_by_ui", "native_login",
    "configured_storage_only", "automatic_registration_and_unbound", "refresh_and_selection_clear_consent", "real_stale_conflict",
    "browser_replacement_saved", "unavailable_preserves_approval", "narrow_selection_and_initial_bind", "independent_final_state",
    "owned_ui_logout_and_exact_rejection", "exact_mutation_budget", "browser_context_closed"))
PROTECTED = (
    {"pid": 1264063, "start_ticks": 11104222, "boot_id": "6bdfc486-7bc8-412f-82b5-70095a09dde7",
     "exe": "/opt/goby-client-m3e/goby", "sha256": "cd67f2e71ff1b63e3c138cdba1f9c9d1e584e2788b47964a332b38311cda0e2d",
     "cgroup": "/system.slice/goby-client-m3e.service"},
    {"pid": 762090, "start_ticks": 7637121, "boot_id": "6bdfc486-7bc8-412f-82b5-70095a09dde7",
     "exe": "/opt/goby-dev/goby", "sha256": "af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620",
     "cgroup": "/system.slice/goby-foundation-test.service"},
)


class Failure(Exception):
    """A concrete ownership or acceptance boundary could not be established."""


def require(value, message):
    if not value:
        raise Failure(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def exact(value, keys, message="A document has unexpected fields."):
    require(type(value) is dict and set(value) == set(keys), message)


def matches(pattern, value):
    return type(value) is str and re.fullmatch(pattern, value) is not None


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True, allow_nan=False).encode() + b"\n"


def decode(raw, limit=4 << 20):
    require(type(raw) is bytes and len(raw) <= limit, "A JSON document exceeds its byte budget.")
    def pairs(items):
        value = {}
        for key, item in items:
            require(key not in value, "A JSON document contains duplicate fields.")
            value[key] = item
        return value
    try:
        value = json.loads(raw, object_pairs_hook=pairs,
                           parse_constant=lambda _: (_ for _ in ()).throw(Failure("Nonfinite JSON is forbidden.")))
    except (ValueError, UnicodeError, RecursionError):
        raise Failure("A JSON document is invalid.") from None
    def bounded(item, depth=0):
        require(depth <= 32, "A JSON document exceeds its depth budget.")
        if isinstance(item, (list, dict)):
            require(len(item) <= 20000, "A JSON collection exceeds its item budget.")
            for child in (item.values() if isinstance(item, dict) else item):
                bounded(child, depth + 1)
    bounded(value)
    return value


def present(path):
    try:
        path.lstat()
        return True
    except FileNotFoundError:
        return False


def path_info(path):
    require(type(path) is Path or isinstance(path, Path), "An owned path must be a Path.")
    require(path.is_absolute() and str(path) == os.path.normpath(str(path)), "An owned path is not canonical.")
    for ancestor in reversed((path, *path.parents)):
        info = ancestor.lstat()
        require(not stat.S_ISLNK(info.st_mode), "An owned path contains a symlink.")
    return info


def identity(info):
    return {"device": info.st_dev, "inode": info.st_ino, "uid": info.st_uid,
            "gid": info.st_gid, "mode": stat.S_IMODE(info.st_mode)}


def directory(path, uid=0, gid=0, mode=0o700):
    info = path_info(path)
    require(stat.S_ISDIR(info.st_mode) and (info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)) == (uid, gid, mode),
            "An owned directory has unexpected metadata.")
    return identity(info)


def read_file(path, *, uid=0, gid=0, modes=(0o600, 0o644), limit=8 << 20):
    before = path_info(path)
    require(stat.S_ISREG(before.st_mode) and before.st_uid == uid and before.st_gid == gid and
            stat.S_IMODE(before.st_mode) in modes and before.st_nlink == 1 and before.st_size <= limit,
            "An owned file has unexpected metadata or size.")
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
        require(identity(os.fstat(stream.fileno())) == identity(before), "An owned file changed while opening.")
        raw = stream.read(limit + 1)
        after = os.fstat(stream.fileno())
    require(len(raw) <= limit and (after.st_size, after.st_mtime_ns, after.st_ctime_ns) ==
            (before.st_size, before.st_mtime_ns, before.st_ctime_ns) and identity(path.lstat()) == identity(before),
            "An owned file changed during its bounded read.")
    return raw


def create_file(path, raw, *, uid=0, gid=0, mode=0o600):
    require(type(raw) is bytes and len(raw) <= 128 << 20, "A new file exceeds its write budget.")
    path_info(path.parent)
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode), "wb") as stream:
        os.fchown(stream.fileno(), uid, gid)
        os.fchmod(stream.fileno(), mode)
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())


def hash_file(path, *, modes=(0o600, 0o644, 0o755), limit=512 << 20):
    before = path_info(path)
    require(stat.S_ISREG(before.st_mode) and before.st_uid == 0 and before.st_gid == 0 and
            stat.S_IMODE(before.st_mode) in modes and before.st_nlink == 1 and before.st_size <= limit,
            "A hashed dependency file has unexpected metadata or size.")
    digest, total = hashlib.sha256(), 0
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
        require(identity(os.fstat(stream.fileno())) == identity(before), "A dependency changed while opening.")
        while True:
            block = stream.read(1 << 20)
            if not block:
                break
            total += len(block)
            require(total <= limit, "A dependency exceeded its streaming byte budget.")
            digest.update(block)
        after = os.fstat(stream.fileno())
    require(total == before.st_size and (after.st_size, after.st_mtime_ns, after.st_ctime_ns) ==
            (before.st_size, before.st_mtime_ns, before.st_ctime_ns) and identity(path.lstat()) == identity(before),
            "A dependency changed during hashing.")
    return digest.hexdigest()


def replace_file(path, before, after, *, uid=0, gid=0):
    require(read_file(path, uid=uid, gid=gid, modes=(0o600,)) == before, "A managed file changed outside its transition.")
    temporary = path.with_name(path.name + ".binding-ui-" + secrets.token_hex(8))
    create_file(temporary, after, uid=uid, gid=gid)
    require(read_file(path, uid=uid, gid=gid, modes=(0o600,)) == before, "A managed file changed before replacement.")
    os.replace(temporary, path)
    descriptor = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def command(argv, *, stdin=None, timeout=15, env=None, limit=8 << 20):
    try:
        result = subprocess.run([str(item) for item in argv], input=stdin, stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, env=env or ENV, timeout=timeout, check=False)
    except (OSError, subprocess.TimeoutExpired):
        raise Failure("An owned command failed to complete within its deadline.") from None
    require(result.returncode == 0 and len(result.stdout) <= limit and len(result.stderr) <= limit,
            "An owned command failed or exceeded its output budget.")
    return result.stdout.strip()


def process_fact(pid, *, binary=False):
    require(type(pid) is int and pid > 1, "A process identifier is invalid.")
    root = Path("/proc") / str(pid)
    raw = (root / "stat").read_text(encoding="ascii")
    fields = raw[raw.rfind(")") + 2:].split()
    require(len(fields) >= 20 and fields[19].isdigit(), "A process start witness is invalid.")
    boot = Path("/proc/sys/kernel/random/boot_id").read_text(encoding="ascii").strip()
    group = (root / "cgroup").read_text(encoding="ascii").splitlines()
    require(len(group) == 1 and group[0].startswith("0::/"), "A process cgroup is ambiguous.")
    exe = str((root / "exe").readlink())
    value = {"pid": pid, "start_ticks": int(fields[19]), "boot_id": boot, "uid": root.stat().st_uid,
             "exe": exe, "cgroup": group[0][3:], "namespace": str((root / "ns/mnt").readlink())}
    if binary:
        # This exception reads the exact already selected process executable.
        with (root / "exe").open("rb") as handle:
            content = handle.read((128 << 20) + 1)
        require(len(content) <= 128 << 20, "A process executable exceeds its byte budget.")
        value["sha256"] = sha(content)
    return value


def protected_facts():
    result = []
    for expected in PROTECTED:
        fact = process_fact(expected["pid"], binary=True)
        require(fact["uid"] == 995 and all(fact[key] == value for key, value in expected.items()),
                "A protected Goby process differs from the accepted identity.")
        result.append(fact)
    return result


def unit_state(unit):
    require(re.fullmatch(r"goby-storage-binding-live-ui-(?:controller|worker)-[0-9]{8}-[0-9]{6}-[0-9a-f]{12}\.service", unit),
            "A unit is outside this operator's namespace.")
    names = ("LoadState,ActiveState,SubState,MainPID,InvocationID,ExecMainStatus,Result,ControlGroup,Description,"
             "MemoryMax,MemorySwapMax,CPUQuotaPerSecUSec,KillMode,TasksMax,RuntimeMaxUSec,RemainAfterExit")
    raw = command(["/usr/bin/systemctl", "show", unit, "--property=" + names]).decode()
    return dict(line.split("=", 1) for line in raw.splitlines() if "=" in line)


def unit_owner(state, unit, tag):
    require(state.get("Description") == tag and state.get("ControlGroup") in ("", "/system.slice/" + unit) and
            re.fullmatch(ID_RE, state.get("InvocationID", "")), "A unit does not identify this exact run.")


def unit_terminal(unit, tag, invocation):
    state = unit_state(unit)
    unit_owner(state, unit, tag)
    require(state["InvocationID"] == invocation and state.get("MainPID") == "0" and
            state.get("RemainAfterExit") == "yes" and state.get("ActiveState") == "active" and state.get("SubState") == "exited" and
            state.get("Result") == "success" and state.get("ExecMainStatus") == "0",
            "An owned unit is not successfully terminal at its recorded invocation.")
    cgroup_empty(unit)
    return state


def cgroup_empty(unit):
    root = Path("/sys/fs/cgroup/system.slice") / unit
    if not present(root):
        return
    require(root.is_dir() and not root.is_symlink(), "The owned cgroup path changed type.")
    files = list(root.rglob("cgroup.procs"))
    require(1 <= len(files) <= 512 and all(not path.is_symlink() and not path.read_text().strip() for path in files),
            "The owned cgroup or a descendant still contains processes.")


def listener(pid=None):
    lines = command(["/usr/bin/ss", "-H", "-ltnp", "sport = :18288"]).decode().splitlines()
    if pid is None:
        require(not lines, "The fixed application port is occupied.")
        with socket.socket() as held:
            held.bind(("127.0.0.1", PORT))
        return
    require(len(lines) == 1 and "127.0.0.1:18288" in lines[0].split() and
            re.search(rf"\bpid={pid},", lines[0]) is not None,
            "The application listener is not owned by the exact new backend.")


def host_fact():
    namespace = str(Path("/proc/self/ns/mnt").readlink())
    require(namespace == str(Path("/proc/1/ns/mnt").readlink()), "The controller is outside the host mount namespace.")
    return {"namespace": namespace, "mountinfo_sha256": sha(Path("/proc/1/mountinfo").read_bytes())}


def validate_intent(value):
    exact(value, {"marker", "version", "run_id", "nonce", "tool", "output", "runtime", "database", "role",
                  "origin", "controller_unit", "worker_unit", "host_namespace", "source_closure", "history_sha256"})
    require(value["marker"] == MARKER and type(value["version"]) is int and value["version"] == 1 and
            matches(RUN_RE, value["run_id"]) and matches(HASH_RE, value["nonce"]), "The run identity is invalid.")
    run, suffix = value["run_id"], value["run_id"].replace("_", "")
    expected = {"tool": str(TOOL), "output": str(WORK / ("storage-binding-live-ui-" + run)),
                "runtime": "/opt/goby-binding-ui-runtime-" + run, "database": "goby_binding_ui_" + suffix,
                "role": "goby_binding_ui_r_" + suffix, "origin": ORIGIN,
                "controller_unit": "goby-storage-binding-live-ui-controller-" + run.replace("_", "-") + ".service",
                "worker_unit": "goby-storage-binding-live-ui-worker-" + run.replace("_", "-") + ".service"}
    require(all(value[key] == item for key, item in expected.items()), "The run escapes its fixed fresh scope.")
    require(matches(r"mnt:\[[0-9]+\]", value["host_namespace"]) and matches(HASH_RE, value["history_sha256"]),
            "A host or historical receipt witness is invalid.")
    exact(value["source_closure"], TOOL_FILES, "The runtime tool closure has missing or extra files.")
    require(all(type(item) is str and re.fullmatch(HASH_RE, item) for item in value["source_closure"].values()),
            "A tool closure digest is invalid.")
    require(value["source_closure"]["accepted-web-report.json"] == WEB_REPORT_SHA, "The accepted frontend report changed.")
    return value


def load_intent(path, expected):
    require(path == TOOL / "intent.json" and matches(HASH_RE, expected), "The intent path or digest is invalid.")
    raw = read_file(path, modes=(0o600,), limit=65536)
    require(sha(raw) == expected, "The externally pinned intent changed.")
    return validate_intent(decode(raw, 65536))


def verify_inputs(intent):
    directory(WORK)
    directory(TOOL)
    for name, digest in intent["source_closure"].items():
        require(sha(read_file(TOOL / name, limit=8 << 20)) == digest, "A frozen tool file changed.")
    actual = {str(path.relative_to(TOOL)) for path in TOOL.rglob("*") if path.is_file()}
    require(actual == TOOL_FILES | {"intent.json"}, "The tool directory contains an unlisted file.")
    for path in TOOL.rglob("*"):
        info = path_info(path)
        require(info.st_uid == 0 and info.st_gid == 0 and not info.st_mode & 0o022 and
                (stat.S_ISREG(info.st_mode) or stat.S_ISDIR(info.st_mode)), "The tool closure has untrusted or special entries.")
    directory(SOURCE)
    for location in (SOURCE, WEB):
        for ancestor in (location, *location.parents):
            info = path_info(ancestor)
            require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_gid == 0 and not info.st_mode & 0o022,
                    "A product input ancestor permits untrusted writes.")
    require(Path(__file__) == TOOL / "verify-storage-binding-live-ui.py", "The operator is not its frozen entrypoint.")
    source_raw = read_file(SOURCE / "backup-source-inputs.json", limit=4 << 20)
    require(sha(source_raw) == SOURCE_MANIFEST_SHA, "The frozen source54 manifest changed.")
    source = decode(source_raw)
    exact(source, {"marker", "files"})
    require(source["marker"] == "goby-client-backup-source-m3e-v1" and type(source["files"]) is dict,
            "The source54 manifest marker or file map changed.")
    selected = {}
    for name in SOURCE_FILES:
        expected = source["files"].get(name)
        require(type(expected) is str and re.fullmatch(HASH_RE, expected), "A required source54 helper is absent.")
        raw = read_file(SOURCE / name)
        require(sha(raw) == expected, "A selected source54 helper changed.")
        selected[name] = raw
    catalog = selected[SOURCE_FILES[2]]
    decoded_catalog = decode(catalog)
    require(sha(catalog) == CATALOG_SHA and type(decoded_catalog) is dict and decoded_catalog.get("version") == 28,
            "The trusted schema28 catalog changed.")
    require(sha(read_file(BINARY, modes=(0o755,), limit=128 << 20)) == BINARY_SHA, "The accepted backend executable changed.")
    require(sha(read_file(NODE, modes=(0o755,), limit=128 << 20)) == NODE_SHA, "The root-owned Node executable changed.")
    web_report = decode(read_file(TOOL / "accepted-web-report.json", limit=4 << 20))
    require(type(web_report) is dict and web_report.get("status") == "passed" and type(web_report.get("tests")) is dict,
            "The frontend prerequisite is not accepted.")
    for key, expected in (("expected", 68), ("unexpected", 0), ("flaky", 0), ("skipped", 0)):
        require(type(web_report["tests"].get(key)) is int and web_report["tests"][key] == expected,
                "The frontend prerequisite has an invalid test count.")
    web = web_report.get("dist_files")
    require(type(web) is dict and 1 < len(web) <= 256 and "index.html" in web, "The frontend inventory is invalid.")
    actual_web = {}
    for path in WEB.rglob("*"):
        info = path_info(path)
        require(info.st_uid == 0 and info.st_gid == 0 and not info.st_mode & 0o022, "A frontend asset permits untrusted writes.")
        if stat.S_ISREG(info.st_mode):
            actual_web[str(path.relative_to(WEB))] = sha(read_file(path, modes=(0o600, 0o644), limit=8 << 20))
        else:
            require(stat.S_ISDIR(info.st_mode), "The frontend contains an unsupported entry.")
    require(actual_web == web, "The frontend membership or bytes differ from the accepted build.")
    require(sha(read_file(HISTORY, modes=(0o600,))) == intent["history_sha256"], "The historical pair receipt changed.")
    dependencies = verify_dependencies(intent)
    return {"selected": selected, "catalog": decoded_catalog, "web": web, "dependencies": dependencies}


def verify_dependencies(intent):
    value = decode(read_file(TOOL / "dependency-files.json", limit=4 << 20))
    exact(value, {"marker", "root", "root_identity", "files", "directories", "browser"})
    require(value["marker"] == "goby-storage-binding-live-ui-dependencies-v1" and value["root"] == str(NODE_MODULES),
            "The dependency manifest addresses an unrelated code tree.")
    packages = ("@playwright/test", "playwright", "playwright-core")
    fixed = {"@playwright/test/package.json": "5587f932b8979b6889654b60e3c28aeb7ca0abbac00eec8c176c939a16149536",
             "playwright/package.json": "8bc2ddebe8d9b051b9ecb87d62c6c63877e59d08617f32c3215681ca8bb70998",
             "playwright-core/package.json": "f061c58427e47e843e26d201f0a57076c734e57c734ecbbd87d4a23b7a20db9b"}
    def tree(root, expected, starts):
        info = path_info(root)
        require(identity(info) == expected["root_identity"] and stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and
                info.st_gid == 0 and not info.st_mode & 0o022, "A dependency root identity changed.")
        for ancestor in root.parents:
            info = path_info(ancestor)
            require(info.st_uid == 0 and not info.st_mode & 0o022, "A dependency ancestor permits untrusted writes.")
        require(type(expected["files"]) is dict and type(expected["directories"]) is dict and
                1 <= len(expected["files"]) <= 12000, "The dependency tree manifest is invalid.")
        actual_files, actual_dirs, total = {}, {}, 0
        selected = set()
        for start in starts:
            base = root / start if start else root
            if base != root:
                selected.add(base)
                for ancestor in base.parents:
                    if ancestor == root:
                        break
                    selected.add(ancestor)
            selected.update(base.rglob("*"))
        require(len(selected) <= 20000, "The dependency tree exceeds its entry budget.")
        for path in sorted(selected):
            relative = str(path.relative_to(root))
            info = path_info(path)
            require(info.st_uid == 0 and info.st_gid == 0 and not info.st_mode & 0o022,
                    "A dependency entry is writable by an untrusted account.")
            if stat.S_ISDIR(info.st_mode):
                actual_dirs[relative] = stat.S_IMODE(info.st_mode)
            else:
                require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1, "A dependency tree contains a special file or hard link.")
                total += info.st_size
                require(total <= 1 << 30, "The dependency tree exceeds its byte budget.")
                actual_files[relative] = {"sha256": hash_file(path), "mode": stat.S_IMODE(info.st_mode)}
        require(actual_files == expected["files"] and actual_dirs == expected["directories"],
                "The full dependency tree membership, modes or bytes changed.")
    tree(NODE_MODULES, value, packages)
    locked = decode(read_file(TOOL / "package-lock.json"))
    for relative, digest in fixed.items():
        require(value["files"].get(relative, {}).get("sha256") == digest,
                "A fixed Playwright package identity changed.")
        package = relative.rsplit("/", 1)[0]
        require(locked.get("packages", {}).get("node_modules/" + package, {}).get("version") == "1.63.0",
                "The browser source lockfile does not match Playwright 1.63.0.")
    browser = value["browser"]
    exact(browser, {"root", "root_identity", "executable", "files", "directories"})
    root = Path(browser["root"])
    require(root == BROWSER_ROOT, "The browser installation is outside the exact Chromium1243 root.")
    executable = Path(browser["executable"])
    require(not executable.is_absolute() and ".." not in executable.parts and str(executable) in browser["files"] and
            browser["files"][str(executable)].get("mode") == 0o755 and root / executable == BROWSER_EXE and
            browser["files"][str(executable)].get("sha256") == BROWSER_SHA, "The browser executable is not the pinned headless shell.")
    tree(root, browser, ("",))
    catalog_raw = read_file(NODE_MODULES / "playwright-core/browsers.json")
    require(sha(catalog_raw) == BROWSERS_JSON_SHA, "The pinned Playwright browser catalog changed.")
    catalog = decode(catalog_raw)
    name = "chromium-headless-shell" if root.name.startswith("chromium_headless_shell-") else "chromium"
    revisions = [entry["revision"] for entry in catalog.get("browsers", []) if entry.get("name") == name]
    require(revisions == [root.name.rsplit("-", 1)[1]], "The Chromium installation does not match the pinned Playwright revision.")
    return {"node_sha256": NODE_SHA, "manifest_sha256": intent["source_closure"]["dependency-files.json"],
            "executable": str(root / executable), "browser_sha256": browser["files"][str(executable)]["sha256"]}


def load_workspace(selected):
    path = SOURCE / SOURCE_FILES[0]
    require(read_file(path) == selected[SOURCE_FILES[0]], "The workspace helper changed before import.")
    module = types.ModuleType("binding_ui_workspace_readers")
    module.__file__ = str(path)
    exec(compile(selected[SOURCE_FILES[0]], str(path), "exec"), module.__dict__)
    require(module.CONTROL == CONTROL and module.DATA == DATA and module.SOCKET == SOCKET and module.PORT == 15432,
            "The selected helper addresses an unrelated cluster.")
    return module


GLOBAL_SQL = """SELECT jsonb_build_object(
 'roles',(SELECT coalesce(jsonb_agg(jsonb_build_object('oid',oid,'name',rolname,'login',rolcanlogin,
 'super',rolsuper,'createdb',rolcreatedb,'createrole',rolcreaterole,'inherit',rolinherit,
 'replication',rolreplication,'bypass',rolbypassrls,'limit',rolconnlimit,'config',rolconfig,
 'valid_until',rolvaliduntil,'tag',shobj_description(oid,'pg_authid')) ORDER BY oid),'[]') FROM pg_roles),
 'databases',(SELECT coalesce(jsonb_agg(jsonb_build_object('oid',oid,'name',datname,'owner',datdba,
 'acl',datacl,'connect',datallowconn,'limit',datconnlimit,'template',datistemplate,
 'tag',shobj_description(oid,'pg_database')) ORDER BY oid),'[]') FROM pg_database),
 'memberships',(SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY oid),'[]') FROM pg_auth_members m),
 'settings',(SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY setdatabase,setrole),'[]') FROM pg_db_role_setting s));"""


def unsupported_sql(role_oid):
    require(type(role_oid) is int and role_oid > 0, "The object owner OID is invalid.")
    checks = ["SELECT 1 FROM pg_namespace WHERE nspname NOT IN ('public','pg_catalog','pg_toast','information_schema')",
              "SELECT 1 FROM pg_extension WHERE extname<>'plpgsql'",
              "SELECT 1 FROM pg_language WHERE lanname NOT IN ('internal','c','sql','plpgsql')",
              "SELECT 1 FROM pg_inherits",
              "SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND "
              f"(c.relowner<>{role_oid} OR c.relacl IS NOT NULL OR c.relkind NOT IN ('r','i','S'))",
              "SELECT 1 FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace "
              "WHERE n.nspname='public' AND a.attacl IS NOT NULL",
              "SELECT 1 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public' AND "
              f"(p.proowner<>{role_oid} OR p.proacl IS NOT NULL)",
              "SELECT 1 FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname='public' AND "
              f"(t.typowner<>{role_oid} OR t.typacl IS NOT NULL OR NOT ((t.typtype='c' AND t.typrelid IN "
              "(SELECT oid FROM pg_class WHERE relnamespace=n.oid AND relkind='r')) OR (t.typtype='b' AND t.typelem IN "
              "(SELECT oid FROM pg_type WHERE typnamespace=n.oid AND typtype='c' AND typrelid<>0))))",
              "SELECT 1 FROM pg_subscription WHERE subdbid=(SELECT oid FROM pg_database WHERE datname=current_database())"]
    for table, column in (("pg_collation", "collnamespace"), ("pg_conversion", "connamespace"),
                          ("pg_operator", "oprnamespace"), ("pg_opclass", "opcnamespace"), ("pg_opfamily", "opfnamespace"),
                          ("pg_ts_config", "cfgnamespace"), ("pg_ts_dict", "dictnamespace"),
                          ("pg_ts_parser", "prsnamespace"), ("pg_ts_template", "tmplnamespace")):
        checks.append(f"SELECT 1 FROM {table} o JOIN pg_namespace n ON n.oid=o.{column} WHERE n.nspname='public'")
    for table in ("pg_event_trigger", "pg_foreign_server", "pg_foreign_data_wrapper", "pg_publication",
                  "pg_largeobject_metadata", "pg_default_acl", "pg_seclabel", "pg_policy", "pg_statistic_ext", "pg_transform"):
        checks.append("SELECT 1 FROM " + table)
    checks.append("SELECT 1 FROM pg_rewrite r JOIN pg_class c ON c.oid=r.ev_class JOIN pg_namespace n "
                  "ON n.oid=c.relnamespace WHERE n.nspname='public'")
    return "SELECT EXISTS(" + " UNION ALL ".join(checks) + ");"


class Database:
    """Own exactly one new database; maintenance SQL stays on this cluster."""

    def __init__(self, intent, inputs, password, pair=None):
        self.intent, self.inputs, self.password = intent, inputs, password
        self.name, self.role = intent["database"], intent["role"]
        self.tag = MARKER + ":" + intent["run_id"]
        self.pair = pair or {"role_oid": None, "database_oid": None, "phase": "absent"}
        self.journal = lambda: None

    def maintenance(self, sql):
        return command(["/usr/sbin/runuser", "--user", "postgres", "--", PG / "psql", "-X", "--no-password",
                        "-h", SOCKET, "-p", "15432", "-U", "postgres", "-d", "postgres", "-v", "ON_ERROR_STOP=1",
                        "-A", "-t", "-q"], stdin=(sql + "\n").encode(), timeout=20)

    def query(self, sql, *, administrative=False):
        require(type(self.pair["database_oid"]) is int and type(self.pair["role_oid"]) is int,
                "The database lacks exact owned OIDs.")
        user = "postgres" if administrative else self.role
        expected_owner = self.role
        guard = f"""BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='5s'; SET LOCAL lock_timeout='2s'; SET LOCAL search_path=pg_catalog,public;
DO $guard$ BEGIN IF current_database()<>'{self.name}' OR current_user<>'{user}'
 OR current_setting('port')<>'15432'
 OR (SELECT oid::bigint FROM pg_database WHERE datname=current_database())<>{self.pair['database_oid']}
 OR (SELECT datdba::bigint FROM pg_database WHERE datname=current_database())<>{self.pair['role_oid']}
 OR pg_get_userbyid({self.pair['role_oid']}::oid)<>'{expected_owner}'
 OR (SELECT shobj_description(oid,'pg_database') FROM pg_database WHERE datname=current_database()) IS DISTINCT FROM '{self.tag}'
 THEN RAISE EXCEPTION 'Unexpected binding UI database identity'; END IF; END $guard$;
"""
        args = [PG / "psql", "-X", "--no-password", "-h", SOCKET if administrative else "127.0.0.1",
                "-p", "15432", "-U", user, "-d", self.name, "-v", "ON_ERROR_STOP=1", "-A", "-t", "-q"]
        env = dict(ENV, PGCONNECT_TIMEOUT="5", PGAPPNAME="binding_ui_" + self.intent["run_id"])
        if administrative:
            args = ["/usr/sbin/runuser", "--user", "postgres", "--", *args]
        else:
            env["PGPASSWORD"] = self.password
        return command(args, stdin=(guard + sql + "\nCOMMIT;\n").encode(), env=env, timeout=15)

    def rows(self):
        return decode(self.maintenance("SELECT jsonb_build_object('role',(SELECT jsonb_build_object('oid',oid::bigint,"
            "'tag',shobj_description(oid,'pg_authid'),'login',rolcanlogin,'super',rolsuper,'createdb',rolcreatedb,"
            "'createrole',rolcreaterole,'replication',rolreplication,'bypass',rolbypassrls,'inherit',rolinherit,"
            "'limit',rolconnlimit,'config',rolconfig,'valid_until',rolvaliduntil,'scram',"
            "(SELECT rolpassword LIKE 'SCRAM-SHA-256$%' FROM pg_authid WHERE oid=r.oid)) FROM pg_roles r "
            f"WHERE rolname='{self.role}'),'database',(SELECT jsonb_build_object('oid',oid::bigint,'owner_oid',datdba::bigint,"
            "'tag',shobj_description(oid,'pg_database'),'allow',datallowconn,'template',datistemplate,'limit',datconnlimit,"
            "'encoding',pg_encoding_to_char(encoding),'acl',(SELECT jsonb_agg(jsonb_build_object('grantor',a.grantor::bigint,"
            "'grantee',a.grantee::bigint,'privilege_type',a.privilege_type,'is_grantable',a.is_grantable) ORDER BY grantee,privilege_type) "
            "FROM aclexplode(coalesce(datacl,acldefault('d',datdba))) a)) FROM pg_database "
            f"WHERE datname='{self.name}'));"))

    def verify_owned(self, *, closed=False):
        rows = self.rows()
        role = {"oid": self.pair["role_oid"], "tag": self.tag, "login": True, "super": False,
                "createdb": False, "createrole": False, "replication": False, "bypass": False,
                "inherit": False, "limit": 12, "config": None, "valid_until": None, "scram": True}
        require(rows["role"] == role, "The owned role identity or privileges changed.")
        expected = dict(self.pair["database"], allow=not closed)
        require(rows["database"] == expected and expected["oid"] == self.pair["database_oid"] and
                expected["owner_oid"] == self.pair["role_oid"] and expected["tag"] == self.tag and
                expected["template"] is False and expected["limit"] == -1 and expected["encoding"] == "UTF8",
                "The owned database identity or grants changed.")
        acl = expected["acl"]
        require(len(acl) == 3 and {entry["privilege_type"] for entry in acl} == {"CREATE", "CONNECT", "TEMPORARY"} and
                all(entry["grantor"] == self.pair["role_oid"] == entry["grantee"] and not entry["is_grantable"] for entry in acl),
                "The owned database has an unexpected grant.")
        self.verify_isolated()

    def verify_isolated(self):
        role, db = self.pair["role_oid"], self.pair["database_oid"]
        require(type(role) is int and type(db) is int, "The owned pair OIDs are incomplete.")
        count = self.maintenance(f"SELECT (SELECT count(*) FROM pg_auth_members WHERE member={role} OR roleid={role} OR grantor={role})+"
            f"(SELECT count(*) FROM pg_db_role_setting WHERE setrole={role} OR setdatabase={db})+"
            f"(SELECT count(*) FROM pg_shdepend WHERE refclassid='pg_authid'::regclass AND refobjid={role} AND NOT "
            f"(dbid={db} OR (dbid=0 AND classid='pg_database'::regclass AND objid={db} AND objsubid=0 AND deptype='o')))+"
            f"(SELECT count(*) FROM pg_shseclabel WHERE (classoid='pg_authid'::regclass AND objoid={role}) OR "
            f"(classoid='pg_database'::regclass AND objoid={db}));")
        require(count == b"0", "The owned pair has external dependencies, grants or settings.")

    def create(self):
        require(self.rows() == {"role": None, "database": None}, "The new role/database names are already occupied.")
        self.pair["phase"] = "role_pending"
        self.journal()
        self.maintenance(f"BEGIN; SET LOCAL password_encryption='scram-sha-256'; CREATE ROLE {self.role} LOGIN NOINHERIT "
            f"NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 12 PASSWORD '{self.password}'; "
            f"COMMENT ON ROLE {self.role} IS '{self.tag}'; COMMIT;")
        rows = self.rows()
        require(rows["database"] is None and rows["role"] is not None and rows["role"]["tag"] == self.tag,
                "Role creation lacks an exact owned acknowledgement.")
        self.pair.update(role_oid=rows["role"]["oid"], phase="database_pending")
        self.journal()
        self.maintenance(f"CREATE DATABASE {self.name} OWNER {self.role} TEMPLATE template0 ENCODING 'UTF8';")
        rows = self.rows()
        require(rows["database"] is not None and rows["database"]["owner_oid"] == self.pair["role_oid"] and
                rows["database"]["tag"] is None, "Database creation lacks an exact owned acknowledgement.")
        self.pair.update(database_oid=rows["database"]["oid"], phase="database_tag_pending")
        self.journal()
        self.maintenance(f"COMMENT ON DATABASE {self.name} IS '{self.tag}'; REVOKE CONNECT,TEMPORARY ON DATABASE {self.name} FROM PUBLIC;")
        self.pair["database"] = self.rows()["database"]
        self.verify_owned()
        self.pair["public"] = decode(self.query("SELECT jsonb_build_object('oid',oid::bigint,'owner',pg_get_userbyid(nspowner),"
            "'acl',nspacl,'comment',obj_description(oid,'pg_namespace')) FROM pg_namespace WHERE nspname='public';", administrative=True))
        require(self.pair["public"]["owner"] == "pg_database_owner" and self.pair["public"]["comment"] == "standard public schema" and
                self.pair["public"]["acl"] == ["pg_database_owner=UC/pg_database_owner", "=U/pg_database_owner"],
                "The fresh public namespace differs from the standard empty template.")
        self.pair["casts"] = decode(self.query("SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY oid),'[]') FROM pg_cast c;", administrative=True))
        require(self.objects() == [], "The freshly created database contains unexpected objects.")
        self.pair["phase"] = "owned"
        self.journal()

    def objects(self):
        require(self.query(unsupported_sql(self.pair["role_oid"]), administrative=True) == b"f",
                "The owned database contains unrecognized objects or grants.")
        source = self.inputs["selected"][SOURCE_FILES[1]].decode()
        queries = re.findall(r"(?m)^const catalogObjectsSQL = `([^`]+)`", source)
        require(len(queries) == 1 and queries[0].startswith("WITH namespace AS") and ";" not in queries[0],
                "The frozen catalog query is ambiguous.")
        return decode(self.query(queries[0].replace("$1", "'public'") + ";", administrative=True))

    def snapshot(self):
        require(self.objects() == self.inputs["catalog"]["objects"], "The live catalog differs from trusted schema28.")
        tables = [entry["Name"] for entry in self.inputs["catalog"]["catalog"]["Tables"]]
        sequences = [entry["Name"] for entry in self.inputs["catalog"]["catalog"]["Sequences"]]
        require(len(tables) == 35 and all(matches(r"[a-z_]+", name) for name in tables + sequences), "The catalog table names are invalid.")
        counts = " + ".join(f"(SELECT count(*) FROM public.{name})" for name in tables)
        require(int(self.query("SELECT " + counts + ";", administrative=True)) <= 2000, "The fixture exceeded its row budget.")
        entries = [f"SELECT '{name}' AS name,coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]') AS rows FROM public.{name} t" for name in tables]
        # PostgreSQL sequences do not expose a composite row type.
        seqs = [f"SELECT '{name}' AS name,jsonb_build_object('last_value',s.last_value,'log_cnt',s.log_cnt,"
                f"'is_called',s.is_called) AS value FROM public.{name} s" for name in sequences]
        value = decode(self.query("SELECT jsonb_build_object('rows',(SELECT jsonb_object_agg(name,rows) FROM (" +
            " UNION ALL ".join(entries) + ") x),'sequences',(SELECT jsonb_object_agg(name,value) FROM (" +
            " UNION ALL ".join(seqs) + ") x));", administrative=True), 8 << 20)
        require(set(value["rows"]) == set(tables) and set(value["sequences"]) == set(sequences), "The database snapshot is incomplete.")
        return value

    def verify_library_defaults(self, library_id):
        require(matches(ID_RE, library_id), "The initialized library item identifier is invalid.")
        result = self.query("SELECT count(*)=1 AND bool_and(m.revision=1 AND m.overrides='{}'::jsonb AND "
            "m.locked_values='{}'::jsonb AND m.last_edited_by IS NULL AND m.last_edited_at IS NULL AND "
            "m.automatic=public.catalog_metadata_automatic_values(i.name,i.sort_name,i.overview,i.type,"
            "i.index_number,i.parent_index_number,i.local_metadata) AND m.source_key=public.catalog_metadata_source_key(i) "
            "AND m.effective IS NOT DISTINCT FROM i.local_metadata AND m.updated_at=i.created_at) "
            "FROM public.item_metadata_state m JOIN public.items i ON i.id=m.item_id "
            f"WHERE i.id='{library_id}' AND i.type='CollectionFolder';", administrative=True)
        require(result == b"t", "The collection folder metadata differs from its unedited trigger initialization.")

    def dispose(self, expected_snapshot):
        require(self.pair["phase"] == "owned", "An incomplete pair is retained for independent inspection.")
        self.verify_owned()
        require(self.snapshot() == expected_snapshot, "The final owned data changed before disposal.")
        public = decode(self.query("SELECT jsonb_build_object('oid',oid::bigint,'owner',pg_get_userbyid(nspowner),"
            "'acl',nspacl,'comment',obj_description(oid,'pg_namespace')) FROM pg_namespace WHERE nspname='public';", administrative=True))
        require(public == self.pair["public"] and decode(self.query("SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY oid),'[]') FROM pg_cast c;",
                administrative=True)) == self.pair["casts"], "The namespace or cast inventory changed.")
        db, role = self.pair["database_oid"], self.pair["role_oid"]
        require(self.maintenance(f"SELECT (SELECT count(*) FROM pg_stat_activity WHERE datid={db})+"
            f"(SELECT count(*) FROM pg_prepared_xacts WHERE database='{self.name}')+"
            f"(SELECT count(*) FROM pg_replication_slots WHERE database='{self.name}');") == b"0",
            "The owned database still has connections or prepared work; no backend will be terminated.")
        self.maintenance(f"ALTER DATABASE {self.name} ALLOW_CONNECTIONS false;")
        self.pair["phase"] = "closed"
        self.journal()
        self.verify_owned(closed=True)
        require(self.maintenance(f"SELECT count(*) FROM pg_stat_activity WHERE datid={db};") == b"0", "A connection raced the database fence.")
        self.maintenance(f"DROP DATABASE {self.name};")
        self.pair["phase"] = "database_removed"
        self.journal()
        require(self.maintenance(f"SELECT (SELECT count(*) FROM pg_shdepend WHERE refclassid='pg_authid'::regclass AND refobjid={role})+"
            f"(SELECT count(*) FROM pg_auth_members WHERE member={role} OR roleid={role} OR grantor={role});") == b"0",
            "The owned role still has dependencies.")
        current = self.rows()
        require(current["database"] is None and current["role"]["oid"] == role and current["role"]["tag"] == self.tag,
                "The role identity changed before disposal.")
        self.maintenance(f"DROP ROLE {self.role};")
        require(self.rows() == {"role": None, "database": None}, "The owned pair remains after ordinary disposal.")
        self.pair["phase"] = "removed"
        self.journal()


class Controller:
    """The only owner of database DDL, HBA transitions and ordinary disposal."""

    def __init__(self, intent, intent_sha, inputs):
        self.intent, self.intent_sha, self.inputs = intent, intent_sha, inputs
        self.output, self.runtime = Path(intent["output"]), Path(intent["runtime"])
        self.tag = MARKER + ":" + intent["run_id"]
        self.lock = None
        self.module = None
        self.owner = None
        self.created = False
        self.hba_before = self.hba_after = None
        self.hba_intent = False
        self.worker_invocation = None
        self.worker_started = False
        self.worker_process = None
        self.sequence = 0
        self.password = secrets.token_hex(32)
        self.db = Database(intent, inputs, self.password)
        self.db.journal = self.save_receipt
        self.report = {"marker": MARKER, "version": 1, "run_id": intent["run_id"], "intent_sha256": intent_sha,
                       "status": "running", "cleanup": {}, "pair": self.db.pair}

    def admit(self):
        require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION"),
                "Run only through the authorized Linux root SSH controller.")
        require(host_fact()["namespace"] == self.intent["host_namespace"], "The explicit host namespace changed.")
        state = unit_state(self.intent["controller_unit"])
        unit_owner(state, self.intent["controller_unit"], self.tag)
        require(state.get("MainPID") == str(os.getpid()) and state.get("ActiveState") in ("activating", "active") and
                state.get("MemoryMax") == str(2 << 30) and state.get("MemorySwapMax") == "0" and
                state.get("CPUQuotaPerSecUSec") == "1.500000s" and state.get("KillMode") == "control-group" and
                state.get("RemainAfterExit") == "yes",
                "The caller is not the exact bounded new controller.")
        self.report["controller"] = {"unit": self.intent["controller_unit"], "invocation_id": state["InvocationID"],
                                     "process": process_fact(os.getpid())}
        require(self.report["controller"]["process"]["cgroup"] == "/system.slice/" + self.intent["controller_unit"],
                "The controller cgroup differs from its expected unit.")
        directory(Path("/opt"), mode=0o755)
        require(not present(self.output) and not present(self.runtime), "The new evidence/runtime path is already occupied.")
        require(unit_state(self.intent["worker_unit"]).get("LoadState") == "not-found", "The new worker unit name is occupied.")
        listener()
        self.report["protected_before"] = protected_facts()
        self.report["host_before"] = host_fact()
        self.module = load_workspace(self.inputs["selected"])
        self.postgres = pwd.getpwnam("postgres")
        self.goby = pwd.getpwnam("goby")
        require(self.goby.pw_uid == 995, "The application service user changed.")
        self.module.verify_parents(self.postgres)
        self.lock = self.module.acquire_lock()
        binaries = self.module.binaries()
        raw = read_file(self.module.OWNER, modes=(0o600,))
        self.owner = decode(raw)
        self.module.validate_owner(self.owner, self.module.expected_owner(self.postgres, binaries),
                                   self.module.directory(CONTROL, 0, 0))
        self.module.verify_directories(self.postgres, self.owner)
        self.module.verify_configuration(self.postgres)
        self.owner_sha = sha(raw)
        self.hba_before = read_file(HBA, uid=self.postgres.pw_uid, gid=self.postgres.pw_gid, modes=(0o600,))
        require(self.hba_before == self.module.HBA.encode(), "The cluster HBA is not at its exact baseline.")
        self.check_cluster(self.hba_before)
        require(self.db.rows() == {"role": None, "database": None}, "The new role/database is occupied.")
        self.before_catalog = decode(self.db.maintenance(GLOBAL_SQL))

    def check_cluster(self, expected_hba):
        require(sha(read_file(self.module.OWNER, modes=(0o600,))) == self.owner_sha,
                "The workspace ownership record changed.")
        require(self.module.verify_process(self.postgres, self.owner) == self.owner["process"] and
                self.module.cluster_identifier() == self.owner["system_identifier"], "The workspace cluster identity changed.")
        self.module.verify_server(self.owner)
        require(read_file(HBA, uid=self.postgres.pw_uid, gid=self.postgres.pw_gid, modes=(0o600,)) == expected_hba,
                "An unowned HBA edit prevents this operation.")
        require(self.module.read_file(DATA / "postgresql.conf", uid=self.postgres.pw_uid, gid=self.postgres.pw_gid) == self.module.CONF,
                "The persistent PostgreSQL configuration changed.")
        for name in ("postgresql.auto.conf", "pg_ident.conf"):
            raw = self.module.read_file(DATA / name, uid=self.postgres.pw_uid, gid=self.postgres.pw_gid)
            require(all(not line.strip() or line.lstrip().startswith("#") for line in raw.splitlines()),
                    "A PostgreSQL override appeared during the run.")

    def save_receipt(self):
        if not self.created:
            return
        self.sequence += 1
        create_file(self.output / ("receipt-%03d.json" % self.sequence), canonical({"marker": MARKER,
            "run_id": self.intent["run_id"], "intent_sha256": self.intent_sha, "sequence": self.sequence,
            "pair": self.db.pair, "output_identity": self.output_identity,
            "worker_invocation": self.worker_invocation, "hba_intent": self.hba_intent}))

    def prepare_runtime(self):
        # Diagnostics opens every ancestor with O_RDONLY, so the service group
        # needs read access as well as traversal on this new runtime root.
        self.runtime.mkdir(mode=0o750)
        os.chown(self.runtime, 0, self.goby.pw_gid)
        os.chmod(self.runtime, 0o750)
        self.runtime_identity = directory(self.runtime, 0, self.goby.pw_gid, 0o750)
        create_file(self.runtime / "owner.json", canonical({"marker": MARKER, "run_id": self.intent["run_id"],
            "intent_sha256": self.intent_sha, "identity": self.runtime_identity}))
        create_file(self.runtime / "goby", read_file(BINARY, modes=(0o755,), limit=128 << 20), gid=self.goby.pw_gid, mode=0o550)
        web = self.runtime / "web"
        web.mkdir(mode=0o750)
        os.chown(web, 0, self.goby.pw_gid)
        for name, digest in self.inputs["web"].items():
            relative = Path(name)
            require(not relative.is_absolute() and ".." not in relative.parts and str(relative) == name,
                    "An asset name escapes its new runtime root.")
            target = web / relative
            for ancestor in reversed(target.parent.parents):
                if ancestor == web or web in ancestor.parents:
                    if not present(ancestor):
                        ancestor.mkdir(mode=0o750)
                        os.chown(ancestor, 0, self.goby.pw_gid)
            if not present(target.parent):
                target.parent.mkdir(mode=0o750)
                os.chown(target.parent, 0, self.goby.pw_gid)
            raw = read_file(WEB / name, limit=8 << 20)
            require(sha(raw) == digest, "A source asset changed before copying.")
            create_file(target, raw, gid=self.goby.pw_gid, mode=0o440)
        for path in sorted((path for path in web.rglob("*") if path.is_dir()), reverse=True):
            os.chmod(path, 0o550)
        os.chmod(web, 0o550)
        app = self.runtime / "app"
        app.mkdir(mode=0o700)
        os.chown(app, 995, self.goby.pw_gid)
        for name in ("media", "cache", "diagnostics", "recovery", "backups", "operations", "tmp"):
            target = app / name
            target.mkdir(mode=0o700)
            os.chown(target, 995, self.goby.pw_gid)
        self.report["runtime_identity"] = self.runtime_identity

    def provision(self):
        self.output.mkdir(mode=0o700)
        self.created = True
        self.output_identity = directory(self.output)
        (self.output / "private").mkdir(mode=0o700)
        create_file(self.output / "intent.json", canonical(self.intent))
        create_file(self.output / "catalog-before.json", canonical(self.before_catalog))
        create_file(self.output / "hba-before", self.hba_before)
        self.save_receipt()
        self.db.create()
        self.check_cluster(self.hba_before)
        self.hba_after = (f"# {self.tag}\nhost {self.db.name} {self.db.role} 127.0.0.1/32 scram-sha-256\n".encode() + self.hba_before)
        create_file(self.output / "hba-temporary", self.hba_after)
        self.hba_intent = True
        self.save_receipt()
        replace_file(HBA, self.hba_before, self.hba_after, uid=self.postgres.pw_uid, gid=self.postgres.pw_gid)
        self.reload_hba(self.hba_after)
        self.prepare_runtime()
        request = {"marker": MARKER, "run_id": self.intent["run_id"], "intent_sha256": self.intent_sha,
                   "pair": self.db.pair, "password": self.password, "runtime_identity": self.runtime_identity,
                   "output_identity": self.output_identity, "goby_gid": self.goby.pw_gid,
                   "controller": self.report["controller"], "cluster": self.owner["process"]}
        raw = canonical(request)
        create_file(self.output / "private/worker-input.json", raw)
        self.worker_input_sha = sha(raw)

    def reload_hba(self, expected):
        self.check_cluster(expected)
        require(self.db.maintenance("SELECT pg_reload_conf();") == b"t" and
                self.db.maintenance("SELECT count(*) FROM pg_hba_file_rules WHERE error IS NOT NULL;") == b"0",
                "The owned HBA reload failed.")
        require(read_file(HBA, uid=self.postgres.pw_uid, gid=self.postgres.pw_gid, modes=(0o600,)) == expected,
                "The HBA changed after its reload.")

    def dispatch(self):
        require(directory(self.output) == self.output_identity, "The evidence root changed before dispatch.")
        verify_inputs(self.intent)
        self.check_cluster(self.hba_after)
        self.db.verify_owned()
        listener()
        args = ["/usr/bin/systemd-run", "--quiet", "--unit=" + self.intent["worker_unit"],
                "--description=" + self.tag, "--service-type=exec", "--remain-after-exit", "--property=MemoryMax=2G",
                "--property=MemorySwapMax=0", "--property=CPUQuota=150%", "--property=KillMode=control-group",
                "--property=TasksMax=256", "--property=RuntimeMaxSec=600", "--property=TimeoutStopSec=30",
                "--property=UMask=0077", "--property=StandardOutput=append:" + str(self.output / "private/worker.log"),
                "--property=StandardError=append:" + str(self.output / "private/worker.log"),
                "/usr/bin/unshare", "--mount", "--fork", "--kill-child", "--propagation", "private",
                "/usr/bin/python3", str(TOOL / "verify-storage-binding-live-ui.py"), "--mode", "worker",
                "--intent", str(TOOL / "intent.json"), "--intent-sha256", self.intent_sha,
                "--worker-input-sha256", self.worker_input_sha]
        self.worker_started = True
        self.save_receipt()
        command(args, timeout=15)
        deadline = time.monotonic() + 630
        while time.monotonic() < deadline:
            state = unit_state(self.intent["worker_unit"])
            if state.get("LoadState") != "not-found":
                unit_owner(state, self.intent["worker_unit"], self.tag)
                if self.worker_invocation is None:
                    self.worker_invocation = state["InvocationID"]
                    self.save_receipt()
                require(state["InvocationID"] == self.worker_invocation, "The worker invocation changed.")
                if state.get("MainPID") not in (None, "0") and self.worker_process is None:
                    self.worker_process = process_fact(int(state["MainPID"]))
                    require(self.worker_process["cgroup"] == "/system.slice/" + self.intent["worker_unit"],
                            "The namespace launcher escaped its owned cgroup.")
                if state.get("ActiveState") in ("inactive", "failed") or (state.get("ActiveState") == "active" and state.get("SubState") == "exited"):
                    break
            time.sleep(0.1)
        require(self.worker_invocation is not None and self.worker_process is not None, "The worker was never observed running.")
        terminal = unit_terminal(self.intent["worker_unit"], self.tag, self.worker_invocation)
        self.report["worker_terminal"] = terminal
        raw = read_file(self.output / "private/worker-result.json", modes=(0o600,), limit=8 << 20)
        result = decode(raw, 8 << 20)
        require(result.get("marker") == MARKER and result.get("run_id") == self.intent["run_id"] and
                result.get("intent_sha256") == self.intent_sha and result.get("status") == "workflow_complete" and
                result.get("stages") == list(STAGES) and all(result.get("cleanup", {}).values()) and
                set(result.get("cleanup", {})) == {"browser_terminal", "sessions_revoked", "backend_terminal", "mounts_removed"},
                "The worker did not complete its exact live workflow and cleanup.")
        self.report["worker_result_sha256"] = sha(raw)
        self.report["worker_namespace"] = result["namespace"]
        require(result["namespace"] != self.intent["host_namespace"], "The worker did not enter a private mount namespace.")
        self.check_cluster(self.hba_after)
        self.db.verify_owned()
        self.final_snapshot = self.db.snapshot()
        worker_final = decode(read_file(self.output / "private/final-database.json", modes=(0o600,), limit=8 << 20), 8 << 20)
        require(self.final_snapshot == worker_final, "The independently read final database differs from the worker evidence.")
        verify_final_database(self.final_snapshot, result["binding_proof"], result["session_proof"])
        self.db.verify_library_defaults(result["binding_proof"]["library_id"])
        create_file(self.output / "private/controller-final-database.json", canonical(self.final_snapshot))
        self.report["final_database_sha256"] = sha(canonical(self.final_snapshot))
        self.report["binding_proof"] = result["binding_proof"]
        self.report["session_proof"] = result["session_proof"]
        self.report["workflow_verified"] = True

    def stop_worker(self):
        if not self.worker_started:
            return
        state = unit_state(self.intent["worker_unit"])
        if state.get("LoadState") == "not-found":
            require(self.worker_invocation is None, "A previously observed worker unit disappeared.")
            return
        unit_owner(state, self.intent["worker_unit"], self.tag)
        require(self.worker_invocation is not None and state["InvocationID"] == self.worker_invocation,
                "An unknown worker invocation will not be stopped.")
        if state.get("MainPID") not in (None, "0"):
            require(self.worker_process is not None and process_fact(int(state["MainPID"])) == self.worker_process,
                    "An unknown worker process will not be stopped.")
            command(["/usr/bin/systemctl", "stop", self.intent["worker_unit"]], timeout=40)
            raise Failure("The worker required an emergency stop; the run remains failed.")
        unit_terminal(self.intent["worker_unit"], self.tag, self.worker_invocation)

    def restore_hba(self):
        if not self.hba_intent:
            return
        actual = read_file(HBA, uid=self.postgres.pw_uid, gid=self.postgres.pw_gid, modes=(0o600,))
        require(actual in (self.hba_before, self.hba_after), "An unknown HBA edit will not be overwritten.")
        self.check_cluster(actual)
        if actual == self.hba_after:
            replace_file(HBA, actual, self.hba_before, uid=self.postgres.pw_uid, gid=self.postgres.pw_gid)
        self.reload_hba(self.hba_before)

    def remove_runtime(self):
        require(directory(self.runtime, 0, self.goby.pw_gid, 0o750) == self.runtime_identity,
                "The new runtime root changed before cleanup.")
        marker = decode(read_file(self.runtime / "owner.json", modes=(0o600,)))
        require(marker == {"marker": MARKER, "run_id": self.intent["run_id"], "intent_sha256": self.intent_sha,
                           "identity": self.runtime_identity}, "The new runtime marker changed.")
        require(self.runtime.parent == Path("/opt") and self.runtime.name == "goby-binding-ui-runtime-" + self.intent["run_id"],
                "The runtime disposal path escaped its exact new root.")
        mounts = Path("/proc/self/mountinfo").read_text()
        require(not any(str(self.runtime) in line for line in mounts.splitlines()), "A fixture mount remains visible to the controller.")
        for path in self.runtime.rglob("*"):
            info = path.lstat()
            require((stat.S_ISREG(info.st_mode) or stat.S_ISDIR(info.st_mode)) and info.st_uid in (0, 995),
                    "The disposable runtime contains an unknown object.")
        shutil.rmtree(self.runtime)
        require(not present(self.runtime), "The owned runtime remains after cleanup.")

    def cleanup(self):
        def attempt(name, operation):
            try:
                operation()
                self.report["cleanup"][name] = True
            except Exception as error:
                self.report["cleanup"][name] = False
                self.report.setdefault("cleanup_errors", {})[name] = str(error) if isinstance(error, Failure) else type(error).__name__
        attempt("worker_terminal", self.stop_worker)
        attempt("hba_restored", self.restore_hba)
        if self.report.get("workflow_verified") and all(self.report["cleanup"].values()):
            attempt("pair_removed", lambda: self.db.dispose(self.final_snapshot))
            if self.report["cleanup"].get("pair_removed"):
                def catalog_unchanged():
                    current = decode(self.db.maintenance(GLOBAL_SQL))
                    create_file(self.output / "catalog-after.json", canonical(current))
                    require(current == self.before_catalog, "The preexisting global catalog changed.")
                attempt("global_catalog_unchanged", catalog_unchanged)
                attempt("runtime_removed", self.remove_runtime)
        elif self.db.pair["phase"] != "absent":
            self.report["pair_retained"] = True
        if self.created:
            attempt("historical_receipt_unchanged", lambda: require(sha(read_file(HISTORY, modes=(0o600,))) == self.intent["history_sha256"],
                                                                    "The historical pair receipt changed."))
            attempt("protected_unchanged", lambda: require(protected_facts() == self.report["protected_before"], "A protected process changed."))
            attempt("host_unchanged", lambda: require(host_fact() == self.report["host_before"], "The host mount state changed."))
            attempt("port_closed", listener)
            attempt("inputs_unchanged", lambda: verify_inputs(self.intent))
            self.save_receipt()
        if self.lock is not None:
            fcntl.flock(self.lock, fcntl.LOCK_UN)
            os.close(self.lock)
            self.lock = None

    def run_all(self):
        try:
            self.admit()
            self.provision()
            self.dispatch()
        except Exception as error:
            self.report["error"] = str(error) if isinstance(error, Failure) else type(error).__name__
        finally:
            self.cleanup()
        required = {"worker_terminal", "hba_restored", "pair_removed", "global_catalog_unchanged", "runtime_removed",
                    "historical_receipt_unchanged", "protected_unchanged", "host_unchanged", "port_closed", "inputs_unchanged"}
        success = not self.report.get("error") and set(self.report["cleanup"]) == required and all(self.report["cleanup"].values())
        self.report["status"] = "awaiting_outer_attestation" if success else "failed"
        if self.created:
            create_file(self.output / "report.json", canonical(self.report))
        return 0 if success else 1


def token_valid(token):
    if not matches(r"[A-Za-z0-9_-]{43}", token):
        return False
    raw = base64.urlsafe_b64decode(token + "=")
    return len(raw) == 32 and base64.urlsafe_b64encode(raw).decode().rstrip("=") == token


def csrf(token):
    require(token_valid(token), "The native token is malformed.")
    return sha(("goby:admin:csrf:" + token).encode())


def stored_topology(document):
    exact(document, {"version", "mapping", "anchor", "registered_root", "boundaries"})
    require(document["version"] == 1 and type(document["boundaries"]) is list and len(document["boundaries"]) <= 4,
            "The fixture storage document is outside its bounded model.")
    exact(document["mapping"], {"approved_path", "registered_path"})
    def ordered_identity(value):
        exact(value, {"version", "profile", "filesystem_uuid", "handle_type", "handle"})
        require(value["version"] == 1 and value["profile"] == "linux-fsuuid-filehandle-v1" and
                matches(ID_RE, value["filesystem_uuid"]) and value["filesystem_uuid"] != "0" * 32 and
                type(value["handle_type"]) is int and type(value["handle"]) is str,
                "The stored identity is incomplete.")
        return {name: value[name] for name in ("version", "profile", "filesystem_uuid", "handle_type", "handle")}
    anchor, registered = ordered_identity(document["anchor"]), ordered_identity(document["registered_root"])
    boundaries = []
    for value in sorted(document["boundaries"], key=lambda entry: entry["relative_path"]):
        exact(value, {"relative_path", "identity"})
        require(value["relative_path"] == "archive", "The fixture has an unexpected stored boundary.")
        boundaries.append({"relative_path": value["relative_path"], "identity": ordered_identity(value["identity"])})
    require(len({entry["relative_path"] for entry in boundaries}) == len(boundaries), "The stored boundaries are duplicated.")
    ordered = {"version": 1, "mapping": {name: document["mapping"][name] for name in ("approved_path", "registered_path")},
               "anchor": anchor, "registered_root": registered, "boundaries": boundaries}
    def go_bytes(value):
        # This fixture uses ASCII paths and opaque base64 fields; none needs Go's HTML escaping.
        raw = json.dumps(value, separators=(",", ":"), ensure_ascii=True).encode()
        require(not any(character in raw for character in (b"<", b">", b"&", b"\\u")), "The fixture contains an unreviewed JSON escape.")
        return raw
    def public(value):
        return {"Profile": value["profile"], "FilesystemUUID": value["filesystem_uuid"], "Digest": sha(go_bytes(value))}
    projection = {"Anchor": public(anchor), "RegisteredRoot": public(registered),
                  "Boundaries": [{"RelativePath": entry["relative_path"], "Identity": public(entry["identity"])} for entry in boundaries]}
    return sha(go_bytes(ordered)), projection


def match_binding_row(binding, row, status):
    required = {"Id", "LibraryId", "Path", "AllowedPath", "RelativePath", "Revision", "Status"}
    optional = {"ApprovedFingerprint", "ObservedFingerprint", "Approved", "Observed", "BoundAt", "BoundBy"}
    require(type(binding) is dict and required <= set(binding) <= required | optional, "A binding DTO has unexpected fields.")
    mapping = {"Id": "id", "LibraryId": "library_id", "Path": "path", "AllowedPath": "allowed_path", "RelativePath": "relative_path"}
    require(all(binding[key] == row[column] for key, column in mapping.items()) and
            binding["Revision"] == str(row["binding_revision"]) and binding["Status"] == status,
            "A real binding DTO disagrees with its independent persisted mapping.")
    if row["storage_binding"] is None:
        require(row["bound_at"] is None and row["bound_by"] is None and
                not ({"Approved", "ApprovedFingerprint", "BoundAt", "BoundBy"} & set(binding)),
                "An unbound root acquired an invented approval.")
    else:
        fingerprint, approved = stored_topology(row["storage_binding"])
        require(row["storage_binding"]["mapping"] == {"approved_path": row["allowed_path"], "registered_path": row["path"]} and
                binding.get("ApprovedFingerprint") == fingerprint and binding.get("Approved") == approved and
                binding.get("BoundBy") == row["bound_by"], "A binding approval differs from its complete stored topology.")
        try:
            actual_time = datetime.datetime.fromisoformat(binding["BoundAt"].replace("Z", "+00:00"))
            row_time = datetime.datetime.fromisoformat(row["bound_at"].replace("Z", "+00:00"))
        except (ValueError, TypeError, KeyError):
            raise Failure("The binding approval timestamp is invalid.") from None
        require(actual_time == row_time, "The binding approval timestamp differs from its row.")
    if status == "verified":
        require(binding.get("Observed") == binding.get("Approved") and
                binding.get("ObservedFingerprint") == binding.get("ApprovedFingerprint"), "A verified binding lacks matching observations.")
    elif status == "unavailable":
        require("Observed" not in binding and "ObservedFingerprint" not in binding, "Unavailable storage invented an observation.")
    else:
        require(matches(HASH_RE, binding.get("ObservedFingerprint")) and type(binding.get("Observed")) is dict,
                "The actionable binding lacks a complete observation.")
        if status == "mismatch":
            require(binding["ObservedFingerprint"] != binding["ApprovedFingerprint"], "The mismatch fingerprint did not change.")


def verify_final_database(snapshot, proof, sessions):
    exact(proof, {"administrator_id", "library_id", "root_ids", "bindings", "baseline_b", "controller_fingerprint"})
    exact(proof["root_ids"], {"A", "B", "U"})
    exact(proof["bindings"], {"A", "B", "U"})
    require(matches(ID_RE, proof["administrator_id"]) and matches(ID_RE, proof["library_id"]) and
            all(matches(ID_RE, value) for value in proof["root_ids"].values()), "The final binding proof has invalid IDs.")
    rows = snapshot["rows"]
    require(len(rows["users"]) == 1 and rows["users"][0]["id"] == proof["administrator_id"] and
            rows["users"][0]["is_administrator"] is True and rows["users"][0]["is_disabled"] is False,
            "The isolated administrator changed.")
    require(len(rows["libraries"]) == 1 and rows["libraries"][0]["id"] == proof["library_id"] and
            len(rows["items"]) == 1 and rows["items"][0]["id"] == proof["library_id"] and
            rows["items"][0]["type"] == "CollectionFolder" and len(rows["library_roots"]) == 3,
            "The final catalog contains unowned libraries or media items.")
    by_id = {row["id"]: row for row in rows["library_roots"]}
    for key, revision in (("A", 3), ("B", 1), ("U", 2)):
        row = by_id[proof["root_ids"][key]]
        require(row["binding_revision"] == revision and row["bound_by"] == proof["administrator_id"], "A final revision or actor is incorrect.")
        match_binding_row(proof["bindings"][key], row, "verified")
    require(by_id[proof["root_ids"]["B"]] == proof["baseline_b"], "Root B changed during another root's approval.")
    expected = {(proof["root_ids"]["A"], 1, 2): proof["controller_fingerprint"],
                (proof["root_ids"]["A"], 2, 3): proof["bindings"]["A"]["ApprovedFingerprint"],
                (proof["root_ids"]["U"], 1, 2): proof["bindings"]["U"]["ApprovedFingerprint"]}
    updates = [row for row in rows["activity_entries"] if row["action"] == "library.root_binding.updated"]
    require(len(updates) == 3 and {(entry["resource_id"], entry["previous_revision"], entry["revision"]) for entry in updates} == set(expected),
            "The final audit must contain each exact binding transition once.")
    for entry in updates:
        key = (entry["resource_id"], entry["previous_revision"], entry["revision"])
        require(key in expected and entry["observation_fingerprint"] == expected[key] and entry["source"] == "native" and
                entry["actor_kind"] == "user" and entry["actor_id"] == proof["administrator_id"] and
                entry["resource_kind"] == "library_root", "A binding audit does not prove its exact transition.")
    require({row["id"] for row in rows["sessions"]} == set(sessions) and len(sessions) == 2 and
            all(row["kind"] == "admin" and row["user_id"] == proof["administrator_id"] and row["revoked_at"] is not None
                for row in rows["sessions"]), "The new native sessions are not both revoked.")
    for session in rows["sessions"]:
        require(session["token_hash"] == "\\x" + sessions[session["id"]]["token_sha256"], "A final session verifier changed.")
        for action in ("session.login", "session.revoked"):
            events = [row for row in rows["activity_entries"] if row["action"] == action and row["resource_id"] == session["id"]]
            require(len(events) == 1 and events[0]["resource_kind"] == "session" and events[0]["source"] == "native" and
                    events[0]["actor_id"] == proof["administrator_id"], "A new session lacks its exact login/revocation audit.")
    allowed_actions = {"user.created", "session.login", "session.revoked", "library.created", "library.root_binding.updated"}
    require(all(row["action"] in allowed_actions for row in rows["activity_entries"]), "An unrelated mutation audit appeared.")
    for action, resource, resource_id in (("user.created", "user", proof["administrator_id"]),
                                         ("library.created", "library", proof["library_id"])):
        events = [row for row in rows["activity_entries"] if row["action"] == action]
        require(len(events) == 1 and events[0]["resource_kind"] == resource and events[0]["resource_id"] == resource_id,
                "A fixture creation audit identifies an unexpected resource.")
    require(all(row["resource_id"] in sessions for row in rows["activity_entries"] if row["action"] in
                ("session.login", "session.revoked")), "An audit refers to an unowned native session.")
    metadata = rows["item_metadata_state"]
    require(len(metadata) == 1 and metadata[0]["item_id"] == proof["library_id"] and metadata[0]["revision"] == 1 and
            metadata[0]["overrides"] == {} and metadata[0]["locked_values"] == {} and metadata[0]["last_edited_by"] is None and
            metadata[0]["last_edited_at"] is None, "The collection folder acquired unrelated or edited metadata state.")
    owners = rows["theme_owner_ids"]
    virtual = [row for row in owners if row["virtual_root"] is True and row["item_id"] is None]
    item_owner = [row for row in owners if row["virtual_root"] is False and row["item_id"] == proof["library_id"]]
    require(len(owners) == 2 and len(virtual) == 1 and len(item_owner) == 1 and virtual[0]["id"] != item_owner[0]["id"],
            "The new collection folder lacks its unique owner mapping or changed the virtual root.")
    for table in ("scan_jobs", "devices", "application_keys", "application_key_clients", "application_key_devices",
                  "play_sessions", "encoding_jobs", "user_item_data", "item_entities", "item_images",
                  "item_subtitles", "item_extra_resources", "item_theme_resources", "catalog_entities", "task_runs",
                  "task_run_requests", "task_run_children", "task_occurrences"):
        require(rows[table] == [], "An unrelated fixture table changed.")


def write_ipc(path, value):
    pending = path.with_name(path.name + ".pending")
    create_file(pending, canonical(value))
    os.link(pending, path, follow_symlinks=False)
    pending.unlink()


def read_ipc(path):
    if not present(path):
        return None
    info = path.lstat()
    if info.st_nlink == 2:
        pending = path.with_name(path.name + ".pending")
        require(present(pending) and identity(pending.lstat()) == identity(info), "The IPC publication has an unknown second link.")
        return None
    return read_file(path, modes=(0o600,), limit=512 << 10)


class Worker:
    """Own the fresh mount namespace, backend, live browser and exact IPC."""

    def __init__(self, intent, intent_sha, inputs, worker_input_sha):
        self.intent, self.intent_sha, self.inputs = intent, intent_sha, inputs
        self.output, self.runtime = Path(intent["output"]), Path(intent["runtime"])
        self.private = self.output / "private"
        raw = read_file(self.private / "worker-input.json", modes=(0o600,), limit=1 << 20)
        require(sha(raw) == worker_input_sha, "The controller-pinned worker input changed.")
        self.request = decode(raw)
        exact(self.request, {"marker", "run_id", "intent_sha256", "pair", "password", "runtime_identity",
                             "output_identity", "goby_gid", "controller", "cluster"})
        require(self.request["marker"] == MARKER and self.request["run_id"] == intent["run_id"] and
                self.request["intent_sha256"] == intent_sha and matches(HASH_RE, self.request["password"]),
                "The worker request belongs to another run.")
        self.gid = self.request["goby_gid"]
        require(type(self.gid) is int and self.gid > 0, "The worker service group is invalid.")
        exact(self.request["pair"], {"role_oid", "database_oid", "phase", "database", "public", "casts"})
        require(self.request["pair"]["phase"] == "owned" and all(type(self.request["pair"][key]) is int and
                self.request["pair"][key] > 0 for key in ("role_oid", "database_oid")), "The worker pair is not a completely owned fresh database.")
        exact(self.request["controller"], {"unit", "invocation_id", "process"})
        require(self.request["controller"]["unit"] == intent["controller_unit"] and
                matches(ID_RE, self.request["controller"]["invocation_id"]), "The worker lacks its exact controller invocation.")
        self.db = Database(intent, inputs, self.request["password"], self.request["pair"])
        self.backend = self.browser = None
        self.backend_fact = None
        self.browser_fact = None
        self.backend_fd = None
        self.mounts = {}
        self.directory_ids = {}
        self.a_mode_changed = False
        self.controller_token = None
        self.browser_token = None
        self.tokens = {}
        self.deleted_tokens = set()
        self.http_count = 0
        self.put_attempted = False
        self.login_attempted = False
        self.library_id = self.admin_id = None
        self.root_ids = None
        self.baseline = None
        self.stages = []
        self.report = {"marker": MARKER, "run_id": intent["run_id"], "intent_sha256": intent_sha,
                       "status": "running", "cleanup": {}, "stages": self.stages}
        self.paths = {key: str(self.runtime / "app/media" / key) for key in ("A", "B", "U")}
        self.administrator = {"name": "binding-ui-" + intent["run_id"], "password": secrets.token_hex(24),
                              "setup_token": secrets.token_hex(32)}

    def admit(self):
        require(sys.platform == "linux" and os.geteuid() == 0, "The namespace worker requires Linux root.")
        require(pwd.getpwnam("goby").pw_uid == 995 and pwd.getpwnam("goby").pw_gid == self.gid,
                "The worker service account or group changed.")
        require(directory(self.output) == self.request["output_identity"] and
                directory(self.runtime, 0, self.gid, 0o750) == self.request["runtime_identity"], "The worker roots changed.")
        current = process_fact(os.getpid())
        require(current["namespace"] != self.intent["host_namespace"] and
                current["cgroup"] == "/system.slice/" + self.intent["worker_unit"], "The worker is outside its private namespace/unit.")
        state = unit_state(self.intent["worker_unit"])
        unit_owner(state, self.intent["worker_unit"], MARKER + ":" + self.intent["run_id"])
        require(state.get("MemoryMax") == str(2 << 30) and state.get("MemorySwapMax") == "0" and
                state.get("CPUQuotaPerSecUSec") == "1.500000s" and state.get("KillMode") == "control-group" and
                state.get("RemainAfterExit") == "yes",
                "The worker resource limits changed.")
        self.namespace = current["namespace"]
        self.report["namespace"] = self.namespace
        require(matches(r"[1-9][0-9]*", state.get("MainPID")), "The worker has no observed namespace launcher.")
        self.launcher_fact = process_fact(int(state["MainPID"]))
        require(self.launcher_fact["exe"] == "/usr/bin/unshare" and self.launcher_fact["cgroup"] == current["cgroup"] and
                self.launcher_fact["namespace"] == self.namespace, "The exact namespace launcher changed.")
        controller = self.request["controller"]
        controller_state = unit_state(controller["unit"])
        unit_owner(controller_state, controller["unit"], MARKER + ":" + self.intent["run_id"])
        require(controller_state["InvocationID"] == controller["invocation_id"] and
                controller_state["MainPID"] == str(controller["process"]["pid"]) and
                process_fact(controller["process"]["pid"]) == controller["process"],
                "The exact controller is no longer supervising this worker.")
        self.db.verify_owned()
        listener()
        (self.private / "ipc").mkdir(mode=0o700)
        create_file(self.private / "worker-admitted.json", canonical({"process": current, "invocation_id": state["InvocationID"],
                    "intent_sha256": self.intent_sha, "controller": controller}))

    def make_directory(self, path):
        path.mkdir(mode=0o550)
        os.chown(path, 0, self.gid)
        os.chmod(path, 0o550)
        self.directory_ids[str(path)] = directory(path, 0, self.gid, 0o550)

    def mount_info(self, target):
        raw = Path("/proc/self/mountinfo").read_bytes()
        require(len(raw) <= 4 << 20 and process_fact(os.getpid())["namespace"] == self.namespace,
                "The private mount observation changed or exceeds its budget.")
        records = []
        for line in raw.decode().splitlines():
            left, separator, right = line.partition(" - ")
            fields = left.split()
            require(separator and len(fields) >= 6, "The private mountinfo is malformed.")
            if fields[4] == str(target):
                records.append({"id": fields[0], "parent": fields[1], "device": fields[2], "root": fields[3],
                                "target": fields[4], "filesystem": right.split()[0]})
        require(len(records) <= 1, "The fixture contains stacked or ambiguous mounts.")
        return records[0] if records else None

    def mount(self, target, source=None):
        require(str(target) in self.directory_ids and self.mount_info(target) is None and str(target) not in self.mounts,
                "A fixture mount target is not the empty owned location.")
        require(identity(target.lstat()) == self.directory_ids[str(target)], "A fixture mount target was replaced.")
        if source is None:
            require(str(target) == self.paths["U"], "Only root U may receive the empty unsupported fixture.")
            args = ["/usr/bin/mount", "-t", "ramfs", "-o", "nosuid,nodev,noexec",
                    "binding-ui-" + self.intent["run_id"], target]
        else:
            require(str(source) in self.directory_ids and identity(source.lstat()) == self.directory_ids[str(source)],
                    "A fixture bind source changed.")
            args = ["/usr/bin/mount", "--bind", source, target]
        command(args, timeout=5)
        observed = self.mount_info(target)
        require(observed is not None, "The fixture mount lacks an observed mount record.")
        if source is not None:
            require(os.path.samestat(source.stat(), target.stat()), "The bind mount does not name its exact source.")
        else:
            require(observed["filesystem"] == "ramfs", "The unbound fixture is not the owned ramfs.")
            # The empty ramfs is readable by the service group but accepts no media writes.
            os.chown(target, 0, self.gid)
            os.chmod(target, 0o550)
            directory(target, 0, self.gid, 0o550)
            require(not any(target.iterdir()), "The owned ramfs fixture is not empty.")
        self.mounts[str(target)] = observed

    def unmount(self, target):
        require(str(target) in self.mounts and self.mount_info(target) == self.mounts[str(target)],
                "An unowned mount will not be removed.")
        command(["/usr/bin/umount", target], timeout=5)
        require(self.mount_info(target) is None and identity(target.lstat()) == self.directory_ids[str(target)],
                "The exact underlying directory was not restored after unmount.")
        del self.mounts[str(target)]

    def fixtures(self):
        for path in self.paths.values():
            self.make_directory(Path(path))
        self.archive = Path(self.paths["A"]) / "archive"
        self.make_directory(self.archive)
        backing = self.runtime / "backing"
        self.make_directory(backing)
        self.sources = []
        for number in (1, 2, 3):
            path = backing / ("source" + str(number))
            self.make_directory(path)
            self.sources.append(path)
        self.mount(self.archive, self.sources[0])
        self.mount(Path(self.paths["U"]))

    def environment(self):
        app = self.runtime / "app"
        return dict(ENV, GOBY_LISTEN="127.0.0.1:18288", GOBY_PUBLIC_URL=ORIGIN,
            GOBY_DATABASE_URL=f"postgresql://{self.db.role}:{self.db.password}@127.0.0.1:15432/{self.db.name}?sslmode=disable",
            GOBY_RECOVERY_DATABASE_URL="", GOBY_SETUP_TOKEN=self.administrator["setup_token"],
            GOBY_SERVER_NAME="Storage binding live UI " + self.intent["run_id"], GOBY_COOKIE_SECURE="false",
            GOBY_TRUSTED_PROXIES="", GOBY_WEB_DIR=str(self.runtime / "web"), GOBY_MEDIA_ROOTS=str(app / "media"),
            GOBY_API_KEY_MASTER_KEY_FILE=str(app / "master.key"), GOBY_RECOVERY_STATE_DIR=str(app / "recovery"),
            GOBY_RECOVERY_OPERATIONS_DIR=str(app / "operations"), GOBY_BACKUP_DIR=str(app / "backups"),
            GOBY_LOG_DIR=str(app / "diagnostics"), GOBY_LOG_MAX_FILE_BYTES="65536", GOBY_LOG_MAX_FILES="4",
            GOBY_LOG_MIN_FREE_BYTES=str(8 << 20), GOBY_TRANSCODING_ENABLED="false", GOBY_TRANSCODE_CACHE=str(app / "cache"),
            GOBY_FFMPEG="/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg",
            GOBY_FFPROBE="/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffprobe", GOBY_PG_DUMP=str(PG / "pg_dump"),
            GOBY_PG_RESTORE=str(PG / "pg_restore"), GOBY_BACKUP_TIMEOUT="3m", GOBY_BACKUP_MAX_OBJECT_BYTES=str(16 << 20),
            GOBY_BACKUP_MAX_TOTAL_BYTES=str(96 << 20), GOBY_BACKUP_MAX_OBJECTS="12", GOBY_BACKUP_MIN_FREE_BYTES=str(16 << 20),
            GOBY_STARTUP_TIMEOUT="30s", TMPDIR=str(app / "tmp"), GOMAXPROCS="2", GOMEMLIMIT="384MiB")

    def pin_backend(self):
        require(self.backend is not None and self.backend.poll() is None, "The exact new backend is no longer running.")
        fact = process_fact(self.backend.pid, binary=True)
        fact["start_ticks"] = str(fact["start_ticks"])
        require(fact["uid"] == 995 and fact["exe"] == str(self.runtime / "goby") and fact["sha256"] == BINARY_SHA and
                fact["namespace"] == self.namespace and fact["cgroup"] == "/system.slice/" + self.intent["worker_unit"],
                "The new backend process changed its owned identity.")
        status = dict(line.split(":", 1) for line in (Path("/proc") / str(self.backend.pid) / "status").read_text().splitlines() if ":" in line)
        require(status.get("NoNewPrivs", "").strip() == "1" and all(int(status[key].strip(), 16) == 0 for key in
                ("CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb")) and not status.get("Groups", "").strip(),
                "The backend retained capabilities or supplementary groups.")
        if self.backend_fact is not None:
            require(fact == self.backend_fact, "The backend process changed after admission.")
        listener(self.backend.pid)
        return fact

    def start_backend(self):
        binary = self.runtime / "goby"
        require(sha(read_file(binary, gid=self.gid, modes=(0o550,), limit=128 << 20)) == BINARY_SHA,
                "The private executable copy changed.")
        self.backend_fd = os.open(binary, os.O_RDONLY | os.O_NOFOLLOW)
        with os.fdopen(os.dup(self.backend_fd), "rb") as stream:
            require(sha(stream.read()) == BINARY_SHA, "The retained executable descriptor changed.")
        os.lseek(self.backend_fd, 0, os.SEEK_SET)
        self.app_log = (self.private / "application.log").open("xb")
        os.chmod(self.private / "application.log", 0o600)
        args = ["/usr/bin/setpriv", "--bounding-set=-all", "--inh-caps=-all", "--ambient-caps=-all", "--no-new-privs",
                "--reuid=995", "--regid=" + str(self.gid), "--clear-groups", "/proc/self/fd/" + str(self.backend_fd)]
        self.backend = subprocess.Popen(args, pass_fds=(self.backend_fd,), env=self.environment(), cwd=self.runtime / "app",
            stdin=subprocess.DEVNULL, stdout=self.app_log, stderr=self.app_log, start_new_session=True)
        deadline = time.monotonic() + 40
        while time.monotonic() < deadline:
            require(self.backend.poll() is None, "The isolated backend exited during startup.")
            try:
                fact = self.pin_backend()
                status, _ = self.http("GET", "/readyz", expected=(200, 503), pin=False, decode_json=False)
                if status == 200:
                    self.backend_fact = fact
                    break
            except (OSError, Failure):
                time.sleep(0.1)
        require(self.backend_fact is not None, "The isolated backend did not reach pinned readiness.")
        self.initial = self.db.snapshot()
        require(not self.initial["rows"]["users"] and not self.initial["rows"]["libraries"] and not self.initial["rows"]["sessions"],
                "The new database is not an uninitialized application.")
        create_file(self.private / "initial-database.json", canonical(self.initial))

    def http(self, method, route, *, body=None, token=None, expected=(200,), pin=True, decode_json=True, login=False):
        routes = {"/readyz", "/admin/v1/session", "/admin/v1/libraries"}
        if self.library_id is not None and self.root_ids is not None:
            routes.add("/admin/v1/libraries/" + self.library_id + "/roots")
            routes.update(self.binding_route(key) for key in ("A", "B", "U"))
        require(route in routes, "A control request escaped its exact native route scope.")
        require(method in ("GET", "POST", "PUT", "DELETE"), "A control method is unsupported.")
        if method == "POST":
            require(login and route == "/admin/v1/session" and not self.login_attempted, "A native control login cannot be repeated.")
            self.login_attempted = True
        if method == "PUT":
            require(self.root_ids is not None and route == self.binding_route("A") and not self.put_attempted and
                    token == self.controller_token and self.stages == list(STAGES[:4]), "A control binding write is outside its single conflict stage.")
            self.put_attempted = True
        if method == "DELETE":
            require(route == "/admin/v1/session" and token in self.tokens and token not in self.deleted_tokens,
                    "A session revocation is outside the exact proven new session.")
            self.deleted_tokens.add(token)
        if pin:
            self.pin_backend()
        self.http_count += 1
        require(self.http_count <= 160, "The bounded native control request budget was exceeded.")
        headers = {"Accept": "application/json", "Origin": ORIGIN}
        if token is not None:
            require(token_valid(token), "A control request has an invalid native token.")
            headers["Cookie"] = "goby_session=" + token
            if method != "GET":
                headers["X-CSRF-Token"] = csrf(token)
        raw_body = None if body is None else canonical(body)
        if raw_body is not None:
            require(len(raw_body) <= 4096, "A native mutation body exceeds its exact request limit.")
            headers["Content-Type"] = "application/json"
        connection = http.client.HTTPConnection("127.0.0.1", PORT, timeout=5)
        response = None
        raw = b""
        try:
            connection.request(method, route, body=raw_body, headers=headers)
            response = connection.getresponse()
            if login and response.status == 200:
                cookies = [value for name, value in response.getheaders() if name.lower() == "set-cookie"]
                require(len(cookies) == 1, "The new native login returned an ambiguous cookie.")
                values = http.cookies.SimpleCookie()
                values.load(cookies[0])
                require(set(values) == {"goby_session"}, "The new native login returned an unexpected cookie.")
                cookie = values["goby_session"]
                require(token_valid(cookie.value) and cookie["path"] == "/admin" and cookie["httponly"] and
                        not cookie["secure"] and cookie["samesite"] == "Strict", "The native cookie attributes changed.")
                self.controller_token = cookie.value
                # Preserve a provisional token before reading a possibly truncated body.
                create_file(self.private / "controller-session-provisional.json", canonical({"token": cookie.value}))
            raw = response.read((4 << 20) + 1)
            require(len(raw) <= 4 << 20 and response.status in expected, "A real control response has an unexpected status or size.")
            status = response.status
            if decode_json and raw:
                require(response.getheader("Content-Type", "").split(";", 1)[0] == "application/json", "A native response is not JSON.")
                value = decode(raw)
            else:
                value = None
            create_file(self.private / ("http-%03d.json" % self.http_count), canonical({"method": method, "route": route,
                "status": status, "response_bytes": len(raw), "response_sha256": sha(raw)}))
            return status, value
        finally:
            connection.close()

    def binding_route(self, key):
        require(self.library_id is not None and self.root_ids is not None and key in self.root_ids, "A binding route lacks owned IDs.")
        return f"/admin/v1/libraries/{self.library_id}/roots/{self.root_ids[key]}/binding"

    def observe(self, key, token=None):
        _, result = self.http("GET", self.binding_route(key), token=token or self.controller_token or self.browser_token)
        exact(result, {"Binding"})
        return result["Binding"]

    def root_rows(self, snapshot):
        roots = snapshot["rows"]["library_roots"]
        require(len(roots) == 3 and self.root_ids is not None, "The fixture root row set is incomplete.")
        by_id = {row["id"]: row for row in roots}
        require(set(by_id) == set(self.root_ids.values()), "The fixture root IDs changed.")
        result = {key: by_id[value] for key, value in self.root_ids.items()}
        for key, row in result.items():
            require(row["library_id"] == self.library_id and row["path"] == self.paths[key] and
                    row["allowed_path"] == str(self.runtime / "app/media") and row["relative_path"] == key,
                    "The registered root mapping escaped its configured fixture.")
        return result

    def prove_session(self, token, *, revoked=False):
        require(token_valid(token), "A session proof token is malformed.")
        snapshot = self.db.snapshot()
        users = snapshot["rows"]["users"]
        require(len(users) == 1 and users[0]["name"] == self.administrator["name"] and users[0]["is_administrator"] is True and
                users[0]["is_disabled"] is False, "The token is not tied to the sole new administrator.")
        administrator = users[0]["id"]
        require(self.admin_id is None or self.admin_id == administrator, "The administrator ID changed.")
        rows = [row for row in snapshot["rows"]["sessions"] if row["token_hash"] == "\\x" + sha(token.encode())]
        require(len(rows) == 1 and rows[0]["kind"] == "admin" and rows[0]["user_id"] == administrator and
                (rows[0]["revoked_at"] is not None) == revoked, "The exact native token lacks its owned session row.")
        row = rows[0]
        audit = [entry for entry in snapshot["rows"]["activity_entries"] if entry["action"] == "session.login" and
                 entry["resource_kind"] == "session" and entry["resource_id"] == row["id"]]
        require(len(audit) == 1 and audit[0]["actor_id"] == administrator and audit[0]["source"] == "native",
                "The exact new session lacks its independent native login audit.")
        proof = {"session_id": row["id"], "user_id": administrator, "token_sha256": sha(token.encode())}
        if token in self.tokens:
            require(self.tokens[token] == proof, "A previously proven native session changed.")
        self.tokens[token] = proof
        return proof

    def load_browser_session(self, descriptor=None):
        path = self.private / "browser-session.json"
        if descriptor is not None:
            exact(descriptor, {"path", "sha256"})
            require(descriptor["path"] == str(path) and matches(HASH_RE, descriptor["sha256"]), "The browser session descriptor escaped its private file.")
        raw = read_file(path, modes=(0o600,), limit=8192)
        if descriptor is not None:
            require(sha(raw) == descriptor["sha256"], "The browser session file changed.")
        value = decode(raw, 8192)
        exact(value, {"marker", "run_id", "nonce", "token", "csrf_token", "user_id", "validated"})
        require(value["marker"] == "goby-storage-binding-live-ui-session-v1" and value["run_id"] == self.intent["run_id"] and
                value["nonce"] == self.intent["nonce"] and token_valid(value["token"]) and
                value["csrf_token"] == csrf(value["token"]) and type(value["validated"]) is bool,
                "The browser session file has an invalid identity.")
        require((value["validated"] and matches(ID_RE, value["user_id"])) or
                (not value["validated"] and value["user_id"] is None), "The browser session validation claim is invalid.")
        self.browser_token = value["token"]
        return value

    def login_controller(self):
        _, result = self.http("POST", "/admin/v1/session", body={"Name": self.administrator["name"],
                            "Password": self.administrator["password"]}, login=True)
        exact(result, {"User", "CSRFToken"})
        require(self.controller_token is not None and result["CSRFToken"] == csrf(self.controller_token) and
                result["User"].get("Id") == self.admin_id and result["User"].get("Name") == self.administrator["name"] and
                result["User"].get("IsAdministrator") is True, "The controller native login DTO is invalid.")
        self.prove_session(self.controller_token)

    def phase_alive(self):
        require(self.browser is not None and self.browser.poll() is None and time.monotonic() < self.phase_deadline,
                "The browser stage expired before its next controlled action.")
        self.pin_backend()

    def check_phase_rows(self, snapshot, revisions, updates):
        roots = self.root_rows(snapshot)
        require({key: str(row["binding_revision"]) for key, row in roots.items()} == revisions and
                not snapshot["rows"]["scan_jobs"], "A stage has an unexpected revision or scan job.")
        events = [entry for entry in snapshot["rows"]["activity_entries"] if entry["action"] == "library.root_binding.updated"]
        require(len(events) == updates, "A stage has an unexpected binding audit count.")
        if self.baseline is not None:
            require(roots["B"] == self.baseline["B"], "The untouched root B changed.")
        return roots

    def stage(self, index, payload):
        action = STAGES[index]
        expected_keys = (
            {"fixture_sha256", "runner", "origin"},
            {"administrator_id", "library", "roots", "bindings", "session", "ui_puts"},
            {"library_id", "root_ids"},
            {"library_id", "root_ids", "unbound", "control", "consent_reset", "ui_puts"},
            {"library_id", "root_ids", "stale", "ui_puts"},
            {"library_id", "root_ids", "conflict", "refreshed", "ui_puts"},
            {"library_id", "root_ids", "accepted", "ui_puts"},
            {"library_id", "root_ids", "unavailable", "ui_puts"},
            {"library_id", "root_ids", "bindings", "narrow", "ui_puts"},
            {"library_id", "root_ids", "logout_status", "exact_cookie_status", "ui_puts"},
        )
        exact(payload, expected_keys[index], "The browser stage payload has unexpected fields.")
        if index >= 2:
            require(payload["library_id"] == self.library_id and payload["root_ids"] == self.root_ids,
                    "The browser stage switched its library or roots.")
        expected_puts = {1: 0, 3: 0, 4: 0, 5: 1, 6: 2, 7: 2, 8: 3, 9: 3}
        if index in expected_puts:
            require(type(payload["ui_puts"]) is int and payload["ui_puts"] == expected_puts[index], "The browser emitted an unexpected binding PUT count.")
        self.phase_alive()
        snapshot = self.db.snapshot()
        updates = len([entry for entry in snapshot["rows"]["activity_entries"] if entry["action"] == "library.root_binding.updated"])
        extra = {}
        if index == 0:
            require(payload["fixture_sha256"] == self.fixture_sha and payload["origin"] == ORIGIN,
                    "The browser did not load the exact zero-HTTP fixture.")
            exact(payload["runner"], {"pid", "start_ticks", "namespace"})
            runner = process_fact(payload["runner"]["pid"], binary=True)
            require(str(runner["start_ticks"]) == payload["runner"]["start_ticks"] and runner["namespace"] == self.namespace ==
                    payload["runner"]["namespace"] and runner["cgroup"] == self.backend_fact["cgroup"] and runner["sha256"] == NODE_SHA,
                    "The Playwright runner is outside the owned namespace/code identity.")
            verify_inputs(self.intent)
            require(snapshot == self.initial, "The browser performed application I/O before its ready ACK.")
        elif index == 1:
            require(matches(ID_RE, payload["administrator_id"]), "The browser administrator ID is invalid.")
            self.admin_id = payload["administrator_id"]
            session = self.load_browser_session(payload["session"])
            require(session["validated"] and session["user_id"] == self.admin_id,
                    "The browser cannot continue from an unvalidated login response.")
            self.prove_session(self.browser_token)
            library = payload["library"]
            require(type(library) is dict and matches(ID_RE, library.get("Id")), "The created library DTO is invalid.")
            self.library_id = library["Id"]
            exact(payload["roots"], {"A", "B", "U"})
            exact(payload["bindings"], {"A", "B", "U"})
            self.root_ids = {key: value["Id"] for key, value in payload["roots"].items()}
            require(len(set(self.root_ids.values())) == 3 and all(matches(ID_RE, value) for value in self.root_ids.values()),
                    "The browser root IDs are invalid or repeated.")
            _, listed = self.http("GET", "/admin/v1/libraries", token=self.browser_token)
            require(listed.get("Items") == [library], "The created library differs from an independent list.")
            snapshot = self.db.snapshot()
            roots = self.check_phase_rows(snapshot, {"A": "1", "B": "1", "U": "1"}, 0)
            require(roots["U"]["storage_binding"] is None, "The ramfs registration did not produce a real unbound root.")
            for key in ("A", "B", "U"):
                status = "unavailable" if key == "U" else "verified"
                match_binding_row(payload["bindings"][key], roots[key], status)
                require(self.observe(key) == payload["bindings"][key], "The baseline binding differs from an independent GET.")
                expected_root = {name: payload["bindings"][key][name] for name in
                                 ("Id", "LibraryId", "Path", "AllowedPath", "RelativePath", "Revision")}
                require(payload["roots"][key] == expected_root, "The selected root list DTO is incomplete.")
            self.baseline = roots
            self.db.verify_library_defaults(self.library_id)
            self.registration = snapshot
        elif index == 2:
            self.check_phase_rows(snapshot, {"A": "1", "B": "1", "U": "1"}, 0)
            self.phase_alive()
            self.unmount(Path(self.paths["U"]))
        elif index == 3:
            roots = self.check_phase_rows(snapshot, {"A": "1", "B": "1", "U": "1"}, 0)
            require(payload["consent_reset"] is True, "The browser did not demonstrate consent reset.")
            match_binding_row(payload["unbound"], roots["U"], "unbound")
            match_binding_row(payload["control"], roots["B"], "verified")
            require(self.observe("U") == payload["unbound"] and self.observe("B") == payload["control"],
                    "The unbound/control observations differ from independent reads.")
            self.phase_alive()
            self.unmount(self.archive)
            self.mount(self.archive, self.sources[1])
        elif index == 4:
            roots = self.check_phase_rows(snapshot, {"A": "1", "B": "1", "U": "1"}, 0)
            match_binding_row(payload["stale"], roots["A"], "mismatch")
            self.login_controller()
            observed = self.observe("A", self.controller_token)
            require(observed == payload["stale"], "The controller did not independently observe the same stale candidate.")
            self.stale = observed
            self.phase_alive()
            _, response = self.http("PUT", self.binding_route("A"), token=self.controller_token,
                body={"Revision": "1", "ObservedFingerprint": observed["ObservedFingerprint"], "AcknowledgeMissingRemoval": True})
            exact(response, {"Binding"})
            roots = self.check_phase_rows(self.db.snapshot(), {"A": "2", "B": "1", "U": "1"}, 1)
            match_binding_row(response["Binding"], roots["A"], "verified")
            require(self.observe("A") == response["Binding"], "The control approval differs from an independent GET.")
            self.control_binding = response["Binding"]
            extra["binding"] = response["Binding"]
        elif index == 5:
            roots = self.check_phase_rows(snapshot, {"A": "2", "B": "1", "U": "1"}, 1)
            require(payload["conflict"] == {"status": 409, "code": "root_binding_conflict", "request": {
                "Revision": "1", "ObservedFingerprint": self.stale["ObservedFingerprint"], "AcknowledgeMissingRemoval": True}},
                "The browser did not observe the one exact real stale-write conflict.")
            match_binding_row(payload["refreshed"], roots["A"], "verified")
            require(payload["refreshed"] == self.control_binding == self.observe("A"), "The explicit conflict refresh differs from the committed approval.")
            self.phase_alive()
            self.unmount(self.archive)
            self.mount(self.archive, self.sources[2])
        elif index == 6:
            roots = self.check_phase_rows(snapshot, {"A": "3", "B": "1", "U": "1"}, 2)
            match_binding_row(payload["accepted"], roots["A"], "verified")
            require(payload["accepted"] == self.observe("A") and payload["accepted"]["ApprovedFingerprint"] !=
                    self.control_binding["ApprovedFingerprint"], "The browser rebind did not accept the changed real storage.")
            self.accepted_a = payload["accepted"]
            self.phase_alive()
            target = Path(self.paths["A"])
            require(identity(target.lstat()) == self.directory_ids[str(target)], "The unavailable fixture directory changed.")
            os.chmod(target, 0)
            self.a_mode_changed = True
        elif index == 7:
            roots = self.check_phase_rows(snapshot, {"A": "3", "B": "1", "U": "1"}, 2)
            match_binding_row(payload["unavailable"], roots["A"], "unavailable")
            require(payload["unavailable"] == self.observe("A"), "The unavailable observation was not real.")
            self.phase_alive()
            self.restore_permissions()
            require(self.observe("A") == self.accepted_a, "Restoring access did not recover the same approval.")
        elif index == 8:
            roots = self.check_phase_rows(snapshot, {"A": "3", "B": "1", "U": "2"}, 3)
            exact(payload["bindings"], {"A", "B", "U"})
            require(payload["narrow"] == {"width": 390, "height": 844, "no_horizontal_overflow": True,
                    "paths_ids_visible": True, "consent_reset": True}, "The narrow real UI proof is incomplete.")
            for key in ("A", "B", "U"):
                match_binding_row(payload["bindings"][key], roots[key], "verified")
                require(payload["bindings"][key] == self.observe(key), "A final binding differs from an independent observation.")
            self.binding_proof = {"administrator_id": self.admin_id, "library_id": self.library_id, "root_ids": self.root_ids,
                "bindings": payload["bindings"], "baseline_b": self.baseline["B"],
                "controller_fingerprint": self.control_binding["ApprovedFingerprint"]}
            extra["bindings"] = payload["bindings"]
        elif index == 9:
            self.check_phase_rows(snapshot, {"A": "3", "B": "1", "U": "2"}, 3)
            require(payload["logout_status"] == 204 and payload["exact_cookie_status"] == 401,
                    "The browser did not prove its exact logout and rejection.")
            self.prove_session(self.browser_token, revoked=True)
            self.http("GET", "/admin/v1/session", token=self.browser_token, expected=(401,))
        final = self.db.snapshot()
        create_file(self.private / ("stage-%02d-database.json" % (index + 1)), canonical(final))
        revisions = None if self.root_ids is None else {key: str(row["binding_revision"]) for key, row in self.root_rows(final).items()}
        return {"backend": self.backend_fact, "library_id": self.library_id, "root_ids": self.root_ids,
                "revisions": revisions, "binding_updates": len([row for row in final["rows"]["activity_entries"]
                 if row["action"] == "library.root_binding.updated"]), "scan_jobs": len(final["rows"]["scan_jobs"]), **extra}

    def start_browser(self):
        fixture = {"marker": MARKER, "version": 1, "run_id": self.intent["run_id"], "nonce": self.intent["nonce"],
                   "origin": ORIGIN, "output": str(self.output), "runtime": str(self.runtime), "paths": self.paths,
                   "library": {"name": "Storage binding live UI " + self.intent["run_id"], "type": "movies"},
                   "administrator": self.administrator, "backend": self.backend_fact,
                   "ipc": {"directory": str(self.private / "ipc"), "ack_timeout_ms": 15000},
                   "result_path": str(self.private / "browser-result.json"), "session_path": str(self.private / "browser-session.json"),
                   "web_files": self.inputs["web"]}
        raw = canonical(fixture)
        self.fixture_sha = sha(raw)
        create_file(self.private / "browser-fixture.json", raw)
        browser_work = self.private / "browser"
        browser_work.mkdir(mode=0o700)
        (browser_work / "e2e").mkdir(mode=0o700)
        for name in ("package.json", "package-lock.json", "playwright.config.ts", "e2e/root-binding-live.spec.ts"):
            data = read_file(TOOL / name)
            require(sha(data) == self.intent["source_closure"][name], "A browser source changed before launch.")
            create_file(browser_work / name, data)
        (browser_work / "node_modules").symlink_to(NODE_MODULES, target_is_directory=True)
        self.browser_log = (self.private / "browser-stdout.json").open("xb")
        self.browser_error = (self.private / "browser-stderr.txt").open("xb")
        os.chmod(self.private / "browser-stdout.json", 0o600)
        os.chmod(self.private / "browser-stderr.txt", 0o600)
        environment = dict(ENV, PLAYWRIGHT_BROWSERS_PATH=str(BROWSER_CACHE), CI="1", FORCE_COLOR="0",
            GOBY_SMOKE_BASE_URL=ORIGIN, GOBY_BINDING_LIVE_FIXTURE=str(self.private / "browser-fixture.json"),
            GOBY_BINDING_LIVE_FIXTURE_SHA256=self.fixture_sha,
            GOBY_BINDING_CHROMIUM_EXECUTABLE=self.inputs["dependencies"]["executable"], TMPDIR=str(self.private))
        args = [str(NODE), str(NODE_MODULES / "@playwright/test/cli.js"), "test", "e2e/root-binding-live.spec.ts",
                "--workers=1", "--retries=0", "--reporter=json", "--timeout=240000"]
        self.browser = subprocess.Popen(args, cwd=browser_work, env=environment, stdin=subprocess.DEVNULL,
                                        stdout=self.browser_log, stderr=self.browser_error, start_new_session=True)
        self.browser_fact = process_fact(self.browser.pid, binary=True)
        require(self.browser_fact["sha256"] == NODE_SHA and self.browser_fact["namespace"] == self.namespace and
                self.browser_fact["cgroup"] == self.backend_fact["cgroup"], "The browser launcher escaped its owned identity.")

    def workflow(self):
        deadline = time.monotonic() + 240
        for index, action in enumerate(STAGES):
            path = self.private / "ipc" / ("%02d-%s.request.json" % (index + 1, action))
            raw = None
            while time.monotonic() < deadline:
                raw = read_ipc(path)
                if raw is not None:
                    break
                require(self.browser.poll() is None, "The real browser exited before its next stage.")
                time.sleep(0.03)
            require(raw is not None, "The real browser omitted a required stage before its deadline.")
            request = decode(raw, 512 << 10)
            exact(request, {"marker", "run_id", "nonce", "sequence", "action", "payload"})
            require(request["marker"] == IPC_MARKER and request["run_id"] == self.intent["run_id"] and
                    request["nonce"] == self.intent["nonce"] and type(request["sequence"]) is int and
                    request["sequence"] == index + 1 and request["action"] == action,
                    "The browser stage is stale, reordered or belongs to another run.")
            self.phase_deadline = min(deadline, time.monotonic() + 14)
            proof = self.stage(index, request["payload"])
            self.phase_alive()
            acknowledgement = dict(request, request_sha256=sha(raw), status="ok", proof=proof)
            write_ipc(path.with_name("%02d-%s.ack.json" % (index + 1, action)), acknowledgement)
            self.stages.append(action)
        remaining = deadline - time.monotonic()
        require(remaining > 0, "The live browser exhausted its overall deadline.")
        require(self.browser.wait(timeout=remaining) == 0, "The real Playwright workflow failed.")
        self.browser_log.close()
        self.browser_error.close()
        output = decode(read_file(self.private / "browser-stdout.json", modes=(0o600,), limit=8 << 20), 8 << 20)
        require(output.get("stats", {}).get("expected") == 1 and output["stats"].get("unexpected") == 0 and
                output["stats"].get("flaky") == 0 and output["stats"].get("skipped") == 0,
                "Playwright did not complete exactly one real, unskipped workflow.")
        self.browser_result = decode(read_file(self.private / "browser-result.json", modes=(0o600,), limit=1 << 20))
        self.verify_browser_result(self.browser_result)

    def verify_browser_result(self, value):
        exact(value, {"marker", "version", "run_id", "nonce", "complete", "status", "checks", "failure", "fixture_sha256",
                      "browser_version", "administrator_id", "library_id", "root_ids", "bindings", "session", "stages",
                      "requests", "screenshots", "violations", "page_errors", "console_errors", "static_requests", "static_bytes"})
        require(value["marker"] == "goby-storage-binding-live-ui-browser-result-v1" and type(value["version"]) is int and
                value["version"] == 1 and value["run_id"] == self.intent["run_id"] and value["nonce"] == self.intent["nonce"] and
                value["complete"] is True and value["status"] == "passed" and value["failure"] is None and
                value["fixture_sha256"] == self.fixture_sha and value["browser_version"] == "153.0.8010.12" and
                value["administrator_id"] == self.admin_id and value["library_id"] == self.library_id and
                value["root_ids"] == self.root_ids and value["bindings"] == self.binding_proof["bindings"],
                "The browser result is not the exact successful live gate.")
        exact(value["checks"], BROWSER_CHECKS)
        require(all(item is True for item in value["checks"].values()) and value["violations"] == [] and
                type(value["page_errors"]) is int and value["page_errors"] == 0 and type(value["console_errors"]) is int and
                0 <= value["console_errors"] <= 16, "The browser result omits a required check or contains an unexpected error.")
        require(type(value["static_requests"]) is int and 1 <= value["static_requests"] <= 192 and
                type(value["static_bytes"]) is int and 1 <= value["static_bytes"] <= 64 << 20,
                "The real static asset observations are absent or exceed their budget.")
        exact(value["session"], {"path", "sha256", "validated"})
        require(value["session"]["path"] == str(self.private / "browser-session.json") and value["session"]["validated"] is True and
                sha(read_file(self.private / "browser-session.json", modes=(0o600,), limit=8192)) == value["session"]["sha256"],
                "The browser result changed its privately proven native session.")
        require(type(value["stages"]) is list and len(value["stages"]) == len(STAGES), "The browser result omitted a required IPC stage.")
        for index, action in enumerate(STAGES):
            stage = value["stages"][index]
            exact(stage, {"sequence", "action", "request_sha256", "ack_sha256"})
            base = self.private / "ipc" / ("%02d-%s" % (index + 1, action))
            require(type(stage["sequence"]) is int and stage["sequence"] == index + 1 and stage["action"] == action and
                    stage["request_sha256"] == sha(read_file(Path(str(base) + ".request.json"), modes=(0o600,), limit=512 << 10)) and
                    stage["ack_sha256"] == sha(read_file(Path(str(base) + ".ack.json"), modes=(0o600,), limit=512 << 10)),
                    "A browser IPC receipt differs from the exact acknowledged request.")
        require(type(value["requests"]) is list and 1 <= len(value["requests"]) <= 128, "The browser request ledger is invalid.")
        writes = []
        for index, entry in enumerate(value["requests"]):
            keys = {"sequence", "phase", "method", "path", "status", "request_bytes", "body_keys", "complete",
                    "response_bytes"}
            if type(entry) is dict and entry.get("path") not in ("/admin/v1/bootstrap", "/admin/v1/session"):
                keys.add("response_sha256")
            require(type(entry) is dict and set(entry) in (keys, keys | {"binding_input"}) and
                    type(entry["sequence"]) is int and entry["sequence"] == index + 1 and entry["complete"] is True and entry["phase"] in STAGES and
                    type(entry["request_bytes"]) is int and 0 <= entry["request_bytes"] <= 4096 and
                    type(entry["response_bytes"]) is int and 0 <= entry["response_bytes"] <= 128 << 10 and
                    ("response_sha256" not in keys or matches(HASH_RE, entry.get("response_sha256"))) and
                    type(entry["body_keys"]) is list and all(type(key) is str for key in entry["body_keys"]) and
                    len(set(entry["body_keys"])) == len(entry["body_keys"]) and
                    (entry["method"] == "PUT") == ("binding_input" in entry),
                    "A browser exchange is incomplete or exceeds its exact ledger contract.")
            require(type(entry["path"]) is str and "?" not in entry["path"] and "#" not in entry["path"],
                    "A browser request ledger contains an unowned query or fragment.")
            if entry["method"] == "GET":
                allowed = {"/admin/v1/bootstrap", "/admin/v1/session", "/admin/v1/libraries", "/admin/v1/storage/roots",
                           "/admin/v1/libraries/" + self.library_id + "/roots"} | {self.binding_route(key) for key in ("A", "B", "U")}
                require(entry["path"] in allowed and entry["status"] in (200, 401) and entry["request_bytes"] == 0 and
                        entry["body_keys"] == [] and "binding_input" not in entry,
                        "A browser read escaped the native acceptance routes.")
            else:
                writes.append(entry)
        expected_writes = [("POST", "/admin/v1/bootstrap", 201, {"SetupToken", "Name", "Password"}),
                           ("POST", "/admin/v1/session", 200, {"Name", "Password"}),
                           ("POST", "/admin/v1/libraries", 201, {"Name", "CollectionType", "Paths", "Scan"}),
                           ("PUT", self.binding_route("A"), 409, {"Revision", "ObservedFingerprint", "AcknowledgeMissingRemoval"}),
                           ("PUT", self.binding_route("A"), 200, {"Revision", "ObservedFingerprint", "AcknowledgeMissingRemoval"}),
                           ("PUT", self.binding_route("U"), 200, {"Revision", "ObservedFingerprint", "AcknowledgeMissingRemoval"}),
                           ("DELETE", "/admin/v1/session", 204, set())]
        require(len(writes) == len(expected_writes), "The browser mutation ledger contains missing or repeated writes.")
        for entry, (method, route, status, body_keys) in zip(writes, expected_writes):
            require((entry["method"], entry["path"], entry["status"], set(entry["body_keys"])) == (method, route, status, body_keys),
                    "The browser mutation order or input fields changed.")
        for entry, key, revision, fingerprint in ((writes[3], "A", "1", self.stale["ObservedFingerprint"]),
                (writes[4], "A", "2", self.binding_proof["bindings"]["A"]["ApprovedFingerprint"]),
                (writes[5], "U", "1", self.binding_proof["bindings"]["U"]["ApprovedFingerprint"])):
            require(entry.get("binding_input") == {"Revision": revision, "ObservedFingerprint": fingerprint, "AcknowledgeMissingRemoval": True},
                    "A real browser approval did not use its exact observed revision/fingerprint.")
        require(type(value["screenshots"]) is list and len(value["screenshots"]) == 2, "The browser screenshots are incomplete.")
        for entry, name in zip(value["screenshots"], ("desktop", "narrow")):
            exact(entry, {"path", "sha256"})
            expected = self.private / "screenshots" / (name + ".png")
            require(entry["path"] == str(expected) and matches(HASH_RE, entry["sha256"]), "A screenshot escaped its owned path.")
            raw = read_file(expected, modes=(0o600,), limit=4 << 20)
            require(raw.startswith(b"\x89PNG\r\n\x1a\n") and sha(raw) == entry["sha256"], "A real browser screenshot changed.")
        self.report["browser_result_sha256"] = sha(read_file(self.private / "browser-result.json", modes=(0o600,), limit=512 << 10))
        self.report["browser_version"] = value["browser_version"]
        self.report["screenshots"] = value["screenshots"]

    def restore_permissions(self):
        if not self.a_mode_changed:
            return
        target = Path(self.paths["A"])
        expected = dict(self.directory_ids[str(target)], mode=0)
        require(identity(target.lstat()) == expected, "The unavailable directory changed before permission restoration.")
        os.chmod(target, 0o550)
        require(identity(target.lstat()) == self.directory_ids[str(target)], "The original directory mode was not restored.")
        self.a_mode_changed = False

    def stop_child(self, child, fact, *, expected_exit=True):
        if child is None:
            return
        if child.poll() is None:
            require(fact is not None, "An unobserved child will not be signalled.")
            current = process_fact(child.pid, binary=True)
            expected = dict(fact, start_ticks=int(fact["start_ticks"]))
            require(current == expected, "An unknown child process will not be signalled.")
            descriptor = os.pidfd_open(child.pid)
            try:
                require(process_fact(child.pid, binary=True) == expected, "The owned child changed before signalling.")
                signal.pidfd_send_signal(descriptor, signal.SIGTERM)
                child.wait(timeout=25)
            finally:
                os.close(descriptor)
        if expected_exit:
            require(child.returncode == 0, "An owned process did not exit successfully.")

    def revoke_sessions(self):
        if self.backend is None:
            return
        self.pin_backend()
        if self.browser_token is None and present(self.private / "browser-session.json"):
            self.load_browser_session()
        failures = []
        for token in (self.browser_token, self.controller_token):
            if token is None:
                continue
            try:
                snapshot = self.db.snapshot()
                rows = [row for row in snapshot["rows"]["sessions"] if row["token_hash"] == "\\x" + sha(token.encode())]
                require(len(rows) == 1, "A provisional cookie lacks an exact newly created session.")
                revoked = rows[0]["revoked_at"] is not None
                self.prove_session(token, revoked=revoked)
                uncertain = False
                if not revoked:
                    try:
                        self.http("DELETE", "/admin/v1/session", token=token, expected=(204,))
                    except Exception:
                        uncertain = True
                # A missing mutation acknowledgement never suppresses the independent rejection check.
                self.http("GET", "/admin/v1/session", token=token, expected=(401,))
                self.prove_session(token, revoked=True)
                require(not uncertain, "A session cleanup acknowledgement was uncertain; independent rejection was checked.")
            except Exception as error:
                failures.append(str(error) if isinstance(error, Failure) else type(error).__name__)
        require(all(row["revoked_at"] is not None and row["kind"] == "admin" for row in self.db.snapshot()["rows"]["sessions"]),
                "An unowned or unrevoked session remains.")
        require(not failures, "One or more exact native session cleanup proofs failed.")

    def cleanup(self):
        def attempt(name, operation):
            try:
                operation()
                self.report["cleanup"][name] = True
            except Exception as error:
                self.report["cleanup"][name] = False
                self.report.setdefault("cleanup_errors", {})[name] = str(error) if isinstance(error, Failure) else type(error).__name__
        attempt("browser_terminal", lambda: self.stop_child(self.browser, self.browser_fact))
        attempt("sessions_revoked", self.revoke_sessions)
        try:
            self.restore_permissions()
        except Exception as error:
            self.report["error"] = str(error) if isinstance(error, Failure) else type(error).__name__
        attempt("backend_terminal", lambda: self.stop_child(self.backend, self.backend_fact))
        def remove_mounts():
            require(self.report["cleanup"].get("backend_terminal"), "Backend descriptors may still hold fixture mounts.")
            for path in reversed(list(self.mounts)):
                self.unmount(Path(path))
            require(not self.mounts and all(self.mount_info(Path(path)) is None for path in self.directory_ids),
                    "Owned or partially acknowledged mounts remain after cleanup.")
        attempt("mounts_removed", remove_mounts)
        if self.backend_fd is not None:
            os.close(self.backend_fd)
            self.backend_fd = None

    def assert_worker_alone(self):
        state = unit_state(self.intent["worker_unit"])
        require(state.get("MainPID") == str(self.launcher_fact["pid"]) and
                process_fact(self.launcher_fact["pid"]) == self.launcher_fact,
                "The namespace launcher changed before the worker's final process proof.")
        root = Path("/sys/fs/cgroup/system.slice") / self.intent["worker_unit"]
        files = list(root.rglob("cgroup.procs"))
        require(1 <= len(files) <= 512, "The worker process inventory is incomplete or oversized.")
        members = set()
        for path in files:
            require(not path.is_symlink(), "A worker process inventory path is a symlink.")
            values = path.read_text().split()
            require(all(matches(r"[1-9][0-9]*", value) for value in values), "The worker process inventory is malformed.")
            members.update(int(value) for value in values)
        require(members == {os.getpid(), self.launcher_fact["pid"]},
                "A browser/backend descendant remained before systemd cleanup; the workflow cannot pass.")

    def run_all(self):
        try:
            self.admit()
            self.fixtures()
            self.start_backend()
            self.start_browser()
            self.workflow()
        except Exception as error:
            self.report["error"] = str(error) if isinstance(error, Failure) else type(error).__name__
        finally:
            self.cleanup()
        success = not self.report.get("error") and all(self.report["cleanup"].values()) and self.stages == list(STAGES)
        try:
            if success:
                final = self.db.snapshot()
                sessions = {proof["session_id"]: {"user_id": proof["user_id"], "token_sha256": proof["token_sha256"]} for proof in self.tokens.values()}
                verify_final_database(final, self.binding_proof, sessions)
                self.db.verify_library_defaults(self.library_id)
                stable = set(final["rows"]) - {"users", "sessions", "libraries", "library_roots", "items", "activity_entries",
                                              "server_settings", "item_metadata_state", "theme_owner_ids"}
                require(all(final["rows"][table] == self.initial["rows"][table] for table in stable), "An unrelated initial table changed.")
                require(all(final["rows"][table] == self.registration["rows"][table] for table in
                            ("item_metadata_state", "theme_owner_ids")), "An initialized collection metadata/owner row changed after creation.")
                initial_owners = self.initial["rows"]["theme_owner_ids"]
                require(len(initial_owners) == 1 and initial_owners[0]["virtual_root"] is True and initial_owners[0]["item_id"] is None and
                        initial_owners[0] in final["rows"]["theme_owner_ids"], "The initial virtual theme owner changed.")
                settings = [row for row in final["rows"]["server_settings"] if row["key"] != "setup_completed"]
                completed = [row for row in final["rows"]["server_settings"] if row["key"] == "setup_completed"]
                require(settings == self.initial["rows"]["server_settings"] and len(completed) == 1 and completed[0]["value"] == "true",
                        "Server settings changed outside the single public bootstrap.")
                for name, state in final["sequences"].items():
                    if name not in ("activity_entries_id_seq", "theme_owner_ids_id_seq"):
                        require(state == self.initial["sequences"][name], "An unrelated sequence changed.")
                owner_before, owner_after = self.initial["sequences"]["theme_owner_ids_id_seq"], final["sequences"]["theme_owner_ids_id_seq"]
                require(owner_before["is_called"] is True and owner_after["is_called"] is True and
                        owner_after["last_value"] == owner_before["last_value"] + 1 and
                        owner_after["last_value"] == next(row["id"] for row in final["rows"]["theme_owner_ids"] if not row["virtual_root"]),
                        "The collection creation did not consume exactly one theme owner identity.")
                audit_sequence = final["sequences"]["activity_entries_id_seq"]
                require(audit_sequence["is_called"] is True and audit_sequence["last_value"] ==
                        max(row["id"] for row in final["rows"]["activity_entries"]), "The audit sequence has an unexplained increment.")
                self.assert_worker_alone()
                create_file(self.private / "final-database.json", canonical(final))
                self.report.update(binding_proof=self.binding_proof, session_proof=sessions, status="workflow_complete")
        except Exception as error:
            success = False
            self.report["error"] = str(error) if isinstance(error, Failure) else type(error).__name__
        if not success:
            self.report["status"] = "failed"
        create_file(self.private / "worker-result.json", canonical(self.report))
        return 0 if success else 1


def attest(intent, intent_sha, inputs):
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION"),
            "Attestation requires the independent authorized root SSH observer.")
    output = Path(intent["output"])
    directory(output)
    require(not present(output / "terminal.json"), "The terminal receipt is immutable and cannot be replayed.")
    raw = read_file(output / "report.json", modes=(0o600,), limit=8 << 20)
    report = decode(raw, 8 << 20)
    required = {"worker_terminal", "hba_restored", "pair_removed", "global_catalog_unchanged", "runtime_removed",
                "historical_receipt_unchanged", "protected_unchanged", "host_unchanged", "port_closed", "inputs_unchanged"}
    require(report.get("marker") == MARKER and report.get("run_id") == intent["run_id"] and report.get("intent_sha256") == intent_sha and
            report.get("status") == "awaiting_outer_attestation" and report.get("workflow_verified") is True and
            set(report.get("cleanup", {})) == required and all(value is True for value in report["cleanup"].values()),
            "The controller did not propose this exact completed and cleaned workflow.")
    tag = MARKER + ":" + intent["run_id"]
    controller = report["controller"]
    require(controller["unit"] == intent["controller_unit"] and controller["process"]["pid"] != os.getpid(),
            "The controller cannot attest its own terminal state.")
    controller_terminal = unit_terminal(intent["controller_unit"], tag, controller["invocation_id"])
    worker_terminal = unit_terminal(intent["worker_unit"], tag, report["worker_terminal"]["InvocationID"])
    require(host_fact() == report["host_before"] and protected_facts() == report["protected_before"],
            "Protected host or application identities changed before final attestation.")
    listener()
    require(not present(Path(intent["runtime"])), "The owned runtime remains before final attestation.")
    verify_inputs(intent)
    worker_raw = read_file(output / "private/worker-result.json", modes=(0o600,), limit=8 << 20)
    require(sha(worker_raw) == report["worker_result_sha256"], "The accepted worker evidence changed.")
    final_raw = read_file(output / "private/controller-final-database.json", modes=(0o600,), limit=8 << 20)
    require(sha(final_raw) == report["final_database_sha256"], "The accepted final data snapshot changed.")
    verify_final_database(decode(final_raw, 8 << 20), report["binding_proof"], report["session_proof"])
    inspector = Controller(intent, intent_sha, inputs)
    inspector.module = load_workspace(inputs["selected"])
    inspector.postgres = pwd.getpwnam("postgres")
    lock = inspector.module.acquire_lock()
    try:
        owner_raw = read_file(inspector.module.OWNER, modes=(0o600,))
        inspector.owner = decode(owner_raw)
        inspector.owner_sha = sha(owner_raw)
        inspector.module.validate_owner(inspector.owner, inspector.module.expected_owner(inspector.postgres, inspector.module.binaries()),
                                        inspector.module.directory(CONTROL, 0, 0))
        inspector.check_cluster(inspector.module.HBA.encode())
        require(inspector.db.rows() == {"role": None, "database": None}, "The disposed database or role reappeared.")
        before = decode(read_file(output / "catalog-before.json", modes=(0o600,)))
        after = decode(read_file(output / "catalog-after.json", modes=(0o600,)))
        require(before == after == decode(inspector.db.maintenance(GLOBAL_SQL)), "The preserved global catalog changed before attestation.")
    finally:
        fcntl.flock(lock, fcntl.LOCK_UN)
        os.close(lock)
    terminal = {"marker": MARKER, "version": 1, "run_id": intent["run_id"], "status": "passed", "intent_sha256": intent_sha,
                "report_sha256": sha(raw), "controller": controller_terminal, "worker": worker_terminal,
                "observed_by": process_fact(os.getpid()), "protected_unchanged": True, "host_unchanged": True,
                "pair_absent": True, "runtime_absent": True, "port_closed": True}
    create_file(output / "terminal.json", canonical(terminal))
    return 0


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mode", choices=("run", "worker", "attest"), required=True)
    parser.add_argument("--intent", type=Path, required=True)
    parser.add_argument("--intent-sha256", required=True)
    parser.add_argument("--worker-input-sha256")
    args = parser.parse_args(argv)
    try:
        require(sys.platform == "linux" and os.geteuid() == 0, "This operator only runs as the authorized Linux root process.")
        require((args.mode == "worker" and matches(HASH_RE, args.worker_input_sha256)) or
                (args.mode != "worker" and args.worker_input_sha256 is None), "The worker input digest is invalid for this entrypoint.")
        os.umask(0o077)
        intent = load_intent(args.intent, args.intent_sha256)
        inputs = verify_inputs(intent)
        interrupted = False
        def interrupt(_number, _frame):
            nonlocal interrupted
            if not interrupted:
                interrupted = True
                raise Failure("The controlled operator was interrupted; owned evidence is retained.")
        if args.mode != "attest":
            for number in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
                signal.signal(number, interrupt)
        if args.mode == "worker":
            code = Worker(intent, args.intent_sha256, inputs, args.worker_input_sha256).run_all()
        elif args.mode == "run":
            code = Controller(intent, args.intent_sha256, inputs).run_all()
        else:
            code = attest(intent, args.intent_sha256, inputs)
        print(json.dumps({"status": "completed" if code == 0 else "failed", "mode": args.mode, "run_id": intent["run_id"]}))
        return code
    except Exception as error:
        print(json.dumps({"status": "failed", "error": str(error) if isinstance(error, Failure) else type(error).__name__}))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
