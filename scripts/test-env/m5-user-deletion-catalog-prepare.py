#!/usr/bin/env python3
"""Draft one owned PG 17 schema-29 catalog bootstrap; never run a test suite."""

import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import stat
import sys
import time
import types


DRAFT_ONLY = True
SCOPE = None
NONCE = None
UNIT = None
SOURCE = None
REVIEWED_RUNTIME_GUARD = None

PURPOSE = "schema29-catalog-generation-with-disposable-postgres"
WORKER = {"path": "/opt/goby-test/m5-final-regression-20260914/private/verify-m5-final-20260914.py",
          "bytes": 43763, "sha256": "3290bba192f1fd6e2b15d328a7c7ba2cbf3c8058ad7c43b7ef8e0eb2e5c64f7f"}
SUPPORT = {"path": "/opt/goby-test/m5-final-regression-20260914/private/m5-final-regression-prepare.py",
           "bytes": 29086, "sha256": "86e1e16de130c441dd16861d3ae7635eddaf18e352c7d87a0db87dcb98d35945"}
VOLUME = {"path": "/opt/goby-test/livetv-programs-disk-final-20260916/private/verify-livetv-programs-final-r03.py",
          "bytes": 57717, "sha256": "c87ae09be2858ca29af5062edde5241b9a5d4f76f3ac50124848ff1e7f5a2560"}
TOOLS = {"path": "/opt/goby-test/m5-final-regression-20260914/private/tool-pins.json",
         "bytes": 1915, "sha256": "79e91ddd2c1150b9dc30f3889d378e407d1ac9d6899f56bb6b0d53fc2970ee41"}
INFRASTRUCTURE = {"path": "/opt/goby-test/m5-final-regression-20260914/private/infrastructure-tools.json",
                  "bytes": 710, "sha256": "ad1c9058083f81d2400e80b588b89a96d6c107fa17ff51a1fc9ccab17b761d5c"}
MODULES = Path("/opt/goby-test/audit-fixes-20260913-20260913T141732Z-a393812c3356/modules")
MIGRATION_29 = "cc2bc10bdd78866854559485b05c34018b4d6ea2ec9e25c880ff234609388564"
CATALOG_NAME = "schema-29-postgresql-17.json"
CATALOG_LIMIT = 4 << 20
RETAINED_LIMIT = 256 << 20
VOLUME_BYTES = 2 << 30
MEMORY_BYTES = 1536 << 20
ROOT_RESERVE = 512 << 20
OUTER_WORK_SECONDS = 1260
OUTER_CLOSE_SECONDS = 180
WORKER_PASS = "catalog_generated_and_pg_closed"


class Rejected(Exception):
    """A fixed nonsecret preparation or execution boundary failed."""


def need(value, code):
    if not value:
        raise Rejected(code)


def encoded(value):
    return (json.dumps(value, sort_keys=True, ensure_ascii=True) + "\n").encode()


def strict_json(raw, **options):
    def unique(pairs):
        value = {}
        for key, item in pairs:
            need(key not in value, "duplicate_json_key")
            value[key] = item
        return value

    def invalid(_value):
        raise Rejected("nonfinite_json_number")

    return json.loads(raw, object_pairs_hook=unique, parse_constant=invalid, **options)


def read_file(path, maximum):
    path = Path(path)
    need(path.is_absolute() and path.resolve() == path, "regular_file_path")
    descriptor = os.open(path, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(descriptor)
        need(stat.S_ISREG(before.st_mode) and before.st_uid == 0 and before.st_nlink == 1
             and not before.st_mode & 0o022 and before.st_size <= maximum, "regular_file_metadata")
        with os.fdopen(descriptor, "rb", closefd=False) as incoming:
            raw = incoming.read(maximum + 1)
        fields = ("st_dev", "st_ino", "st_uid", "st_gid", "st_mode", "st_nlink", "st_size", "st_mtime_ns", "st_ctime_ns")
        after, named = os.fstat(descriptor), path.lstat()
        need(len(raw) == before.st_size and all(getattr(before, key) == getattr(after, key) == getattr(named, key)
                                               for key in fields), "regular_file_changed")
        return raw
    finally:
        os.close(descriptor)


def pinned(value, maximum=4 << 20):
    need(isinstance(value, dict) and set(value) in ({"path", "sha256"}, {"path", "sha256", "bytes"})
         and re.fullmatch(r"[0-9a-f]{64}", value.get("sha256", "")), "pin_shape")
    raw = read_file(value["path"], maximum)
    need(hashlib.sha256(raw).hexdigest() == value["sha256"] and
         ("bytes" not in value or len(raw) == value["bytes"]), "pin_changed")
    return raw


def pin(path, maximum=4 << 20):
    raw = read_file(path, maximum)
    return {"path": str(path), "bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest()}


def load_module(selected, name, raw=None):
    if raw is None:
        raw = pinned(selected).decode()
    module = types.ModuleType(name)
    module.__file__ = selected["path"]
    exec(compile(raw, selected["path"], "exec"), module.__dict__)
    return module


def bindings():
    need(DRAFT_ONLY is False, "draft_only_not_admitted")
    need(isinstance(SCOPE, str) and isinstance(NONCE, str) and re.fullmatch(r"[0-9a-f]{12}", NONCE)
         and isinstance(SOURCE, dict) and isinstance(REVIEWED_RUNTIME_GUARD, dict), "fresh_bindings_required")
    root = Path(SCOPE)
    need(root.parent == Path("/opt/goby-test") and root.name.startswith("m5-user-deletion-catalog-")
         and root.name.endswith(NONCE) and root.resolve() == root
         and UNIT == "goby-m5-user-deletion-catalog-" + NONCE + ".service", "fixed_scope_or_unit")
    return root


def input_value(selected, worker=False):
    root = bindings()
    need(selected["path"] == str(root / "private/input.json"), "fresh_input_path")
    value = strict_json(pinned(selected))
    need(set(value) == {"kind", "version", "purpose", "scope", "nonce", "unit", "adapter", "source", "runtimeGuard"}
         and value["kind"] == "m5-user-deletion-catalog-input" and type(value["version"]) is int
         and value["version"] == 1 and value["purpose"] == PURPOSE and value["scope"] == SCOPE
         and value["nonce"] == NONCE and value["unit"] == UNIT
         and value["source"] == SOURCE and value["runtimeGuard"] == REVIEWED_RUNTIME_GUARD, "catalog_input_binding")
    adapter = root / "private/m5-user-deletion-catalog-prepare.py"
    expected_self = root / "workspace/job/verify-isolated.py" if worker else adapter
    need(value["adapter"]["path"] == str(adapter) and Path(__file__) == expected_self
         and pinned(value["adapter"]) == read_file(expected_self, 4 << 20), "adapter_source_changed")
    source = value["source"]
    need(set(source) == {"gitHead", "archive", "manifest", "preparation", "files", "uncompressedBytes"}
         and re.fullmatch(r"[0-9a-f]{40}", source["gitHead"])
         and type(source["files"]) is int and 0 < source["files"] <= 30000
         and type(source["uncompressedBytes"]) is int and 0 < source["uncompressedBytes"] <= 256 << 20,
         "source_descriptor_shape")
    need(set(value["runtimeGuard"]) == {"helper", "input"}, "runtime_guard_roles")
    guard_input = strict_json(pinned(value["runtimeGuard"]["input"]))
    need(guard_input.get("purpose") == PURPOSE, "fresh_runtime_guard_purpose")
    return value


def libraries():
    root = bindings()
    support = load_module(SUPPORT, "catalog_owned_support")
    resources = load_module(VOLUME, "catalog_owned_volume")
    resources.E = root
    resources.COMPILER = root / "workspace"
    resources.COMPILER_IMAGE = root / "private/compiler.ext4"
    resources.COMPILER_DESCRIPTOR = root / "private/compiler-volume.json"
    resources.COMPILER_IMAGE_BYTES = VOLUME_BYTES
    resources.FIXTURES = root / "workspace/fixtures"
    support.E, support.RAM = root, root / "private"
    support.EXT4 = support.FIXTURES = resources.COMPILER
    support.IMAGE = resources.COMPILER_IMAGE
    return support, resources


class JSONNumber(str):
    """Preserve the real exporter JSON number spelling during fingerprinting."""


def canonical(value):
    if isinstance(value, JSONNumber):
        return str(value)
    if isinstance(value, dict):
        return "{" + ",".join(canonical(key) + ":" + canonical(value[key]) for key in sorted(value)) + "}"
    if isinstance(value, list):
        return "[" + ",".join(canonical(item) for item in value) + "]"
    return json.dumps(value, ensure_ascii=False, separators=(",", ":")).replace("\u2028", "\\u2028").replace("\u2029", "\\u2029")


def review_catalog(path, source, manifest):
    raw = read_file(path, CATALOG_LIMIT)
    need(stat.S_IMODE(path.lstat().st_mode) == 0o600, "catalog_mode")
    value = strict_json(raw)
    need(set(value) == {"version", "postgresql_major", "migrations", "catalog", "objects"}
         and type(value["version"]) is int and value["version"] == 29
         and type(value["postgresql_major"]) is int and value["postgresql_major"] == 17, "catalog_version")
    migrations = []
    for name, row in sorted(manifest.items()):
        if name.startswith("internal/database/migrations/") and name.endswith(".sql"):
            basename = name.rsplit("/", 1)[1]
            need(re.fullmatch(r"[0-9]{4}_[a-z0-9_]+\.sql", basename), "migration_name")
            migrations.append({"version": int(basename[:4]), "name": basename, "sha256": row["sha256"]})
    need(isinstance(value["migrations"], list) and all(isinstance(row, dict) and type(row.get("version")) is int for row in value["migrations"])
         and [row["version"] for row in migrations] == list(range(1, 30)) and value["migrations"] == migrations
         and migrations[-1] == {"version": 29, "name": "0029_user_deletion_activity.sql", "sha256": MIGRATION_29},
         "catalog_migration_prefix")
    old_name = "internal/backuppg/catalogs/schema-28-postgresql-17.json"
    old_raw = read_file(source / old_name, CATALOG_LIMIT)
    need(hashlib.sha256(old_raw).hexdigest() == manifest[old_name]["sha256"], "historical_catalog_source_changed")
    old = strict_json(old_raw)
    catalog = value["catalog"]
    need(set(catalog) == {"Schema", "Tables", "Sequences", "SHA256", "Constraints"}
         and catalog["Schema"] == "" and len(catalog["Tables"]) == 35 and len(catalog["Sequences"]) == 5
         and value["migrations"][:-1] == old["migrations"]
         and all(catalog[key] == old["catalog"][key] for key in ("Schema", "Tables", "Sequences", "Constraints")),
         "catalog_inventory_or_unchanged_foreign_keys")
    numbered = strict_json(raw, parse_int=JSONNumber, parse_float=JSONNumber)
    need(hashlib.sha256(canonical(numbered["objects"]).encode()).hexdigest() == catalog["SHA256"], "catalog_object_fingerprint")
    def objects(items):
        need(isinstance(items, list), "catalog_objects_array")
        keys = []
        result = {}
        for item in items:
            need(isinstance(item, dict) and set(item) == {"kind", "name", "value"}
                 and isinstance(item["kind"], str) and isinstance(item["name"], str), "catalog_object_shape")
            key = (item["kind"], item["name"])
            keys.append(key)
            result[key] = item["value"]
        need(keys == sorted(set(keys)), "catalog_object_order_or_duplicate")
        return result
    actual, historical = objects(value["objects"]), objects(old["objects"])
    need(set(actual) == set(historical), "catalog_object_inventory_changed")
    changed = {key for key in actual if actual[key] != historical[key]}
    expected = {("constraint", "activity_entries.activity_entries_action_check"),
                ("constraint", "activity_entries.activity_entries_action_resource_check")}
    need(changed == expected, "catalog_changed_objects")
    for key in changed:
        before, after = historical[key], actual[key]
        need(set(before) == set(after) and after["type"] == "c"
             and all(before[field] == after[field] for field in before if field != "definition")
             and after["definition"].count("'user.deleted'::text") == 1
             and after["definition"].replace("'user.deleted'::text, ", "", 1) == before["definition"],
             "catalog_check_definition_delta")
    artifact = {"path": str(path), "bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest()}
    return {"artifact": artifact, "version": 29, "postgresqlMajor": 17,
            "tables": 35, "sequences": 5, "migrations": migrations, "objectsSHA256": catalog["SHA256"],
            "changedObjects": [list(key) for key in sorted(changed)], "actualDatabaseExport": True}


def worker_module(value, selected, resources, volume_pin, volume_gate):
    raw = pinned(WORKER).decode()
    def replace(before, after, count=1):
        nonlocal raw
        need(raw.count(before) == count, "worker_derivation_anchor")
        raw = raw.replace(before, after)
    replace(r'r"goby-m5-final-regression-20260914-[0-9a-f]{12}\.service"',
            r'r"goby-m5-user-deletion-catalog-[0-9a-f]{12}\.service"', count=1)
    replace('    regression_ram_required()\n    protected(FIXTURES, directory=True)\n    ext4_fixtures_required()',
            '    CATALOG_VOLUME_GATE()\n    protected(FIXTURES, directory=True)')
    replace('args.mode == "full", "only_complete_ordinary_suite_admitted"',
            'args.mode == "catalog", "only_catalog_generation_admitted"')
    replace('    nonce = secrets.token_hex(6)\n    stamp = dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")\n    scope = BASE / (PREFIX + stamp + "-" + nonce)',
            '    nonce = CATALOG_NONCE\n    scope = CATALOG_WORKER_SCOPE')
    replace('    unit = "goby-m5-final-regression-20260914-" + nonce + ".service"', '    unit = CATALOG_UNIT')
    replace('"Goby M5 final regression "', '"Goby schema29 catalog "')
    replace('"--property=Description=Goby M5 final regression "', '"--property=Description=Goby schema29 catalog "')
    replace('"singleRunFullSuite": True', '"singleRunFullSuite": False', count=2)
    replace('"--property=MemoryMax=3G"', '"--property=MemoryMax=1536M"')
    replace('"--property=TasksMax=512"', '"--property=TasksMax=256"')
    replace('"--property=ReadWritePaths=" + str(scope) + " " + str(FIXTURES),',
            '"--property=ReadWritePaths=" + str(scope) + " " + str(FIXTURES) + " " + str(BASE),')
    replace('"--property=LimitNOFILE=4096",', '"--property=LimitNOFILE=4096", "--property=LimitFSIZE=128M",')
    replace('"expected_packages": list(EXPECTED_PACKAGES),',
            '"expected_packages": [], "catalog_input": CATALOG_INPUT, "compiler_volume": CATALOG_VOLUME_PIN,')
    replace('    dispatched = False\n    try:',
            '    dispatched = False\n    result["unitDispatchAttempted"] = False\n    try:', count=1)
    replace('        dispatched = True\n',
            '        dispatched = True\n        result["unitDispatchAttempted"] = True\n', count=1)
    module = load_module(WORKER, "schema29_owned_worker", raw)
    module.__file__ = str(Path(__file__))
    module.BASE, module.FIXTURES = resources.COMPILER, resources.FIXTURES
    module.CATALOG_NONCE, module.CATALOG_UNIT = NONCE, UNIT
    module.CATALOG_WORKER_SCOPE = resources.COMPILER / "job"
    module.CATALOG_INPUT, module.CATALOG_VOLUME_PIN = selected, volume_pin
    module.CATALOG_VOLUME_GATE = volume_gate
    module.TEST_SECONDS, module.COMMAND_SECONDS, module.BUSINESS_SECONDS = 120, 300, 420
    module.RUNTIME_SECONDS, module.UNIT_STOP_SECONDS, module.STOP_COMMAND_SECONDS = 600, 180, 210
    module.SUPERVISOR_WAIT_SECONDS, module.REQUIRED_OUTER_SECONDS = 630, 900
    module.PASS_STATUS = WORKER_PASS
    module.Rejected = Rejected
    inherited = module.Worker
    class CatalogWorker(inherited):
        def __init__(self, scope):
            super().__init__(scope)
            self.environment.update(PATH=str(module.GO.parent) + ":" + module.ENV["PATH"],
                                    GOCACHE=str(resources.COMPILER / "build-cache"), TMPDIR=str(resources.COMPILER / "tmp"),
                                    GOTMPDIR=str(resources.COMPILER / "tmp"), CGO_ENABLED="0")
            self.environment.pop("CC", None)
            self.report.update(singleRunFullSuite=False, ordinary_suite_passed=False, build_executed=False,
                               catalogGenerationOnly=True, generatorAttempts=0)

        def admission(self):
            need(self.scope == module.CATALOG_WORKER_SCOPE and self.config["scope"] == str(self.scope)
                 and self.config["unit"] == UNIT and self.config["nonce"] == NONCE and self.config["mode"] == "catalog"
                 and self.config["catalog_input"] == selected and self.config["compiler_volume"] == volume_pin
                 and self.config["source_archive_sha256"] == value["source"]["archive"]["sha256"], "catalog_worker_binding")
            state = module.property_map(UNIT, ["MainPID", "PrivateNetwork", "PrivateMounts", "PrivateTmp", "ProtectSystem",
                                               "ProtectHome", "NoNewPrivileges", "Restart", "ControlGroup", "MemoryMax",
                                               "MemorySwapMax", "CPUQuotaPerSecUSec", "TasksMax", "KillMode", "RuntimeMaxUSec",
                                               "TimeoutStopUSec", "LimitNOFILE", "LimitFSIZE", "UMask", "ReadOnlyPaths"])
            need(state.get("MainPID") == str(os.getpid()) and state.get("ControlGroup") == "/system.slice/" + UNIT
                 and all(state.get(key) == "yes" for key in ("PrivateNetwork", "PrivateMounts", "PrivateTmp", "ProtectHome", "NoNewPrivileges"))
                 and state.get("ProtectSystem") == "strict" and state.get("Restart") == "no"
                 and state.get("MemoryMax") == str(MEMORY_BYTES) and state.get("MemorySwapMax") == "0"
                 and state.get("CPUQuotaPerSecUSec") == "1.500000s" and state.get("TasksMax") == "256"
                 and state.get("KillMode") == "control-group" and state.get("RuntimeMaxUSec") == "10min"
                 and state.get("TimeoutStopUSec") == "3min" and state.get("LimitNOFILE") == "4096"
                 and state.get("LimitFSIZE") == str(128 << 20) and state.get("UMask") == "0077"
                 and state.get("ReadOnlyPaths", "").split() == [str(self.source)], "catalog_unit_limits")
            need(os.readlink("/proc/self/ns/net") != os.readlink("/proc/1/ns/net")
                 and os.readlink("/proc/self/ns/mnt") != os.readlink("/proc/1/ns/mnt"), "private_namespaces_required")
            need(module.digest(self.scope / "source.tar") == value["source"]["archive"]["sha256"]
                 and module.inventory(self.source) == strict_json(read_file(self.scope / "source-manifest.json", 4 << 20)),
                 "catalog_worker_source_changed")
            self.report["unit"] = state
            self.report["workspace"] = resources.check_compiler_worker(self, module)
            self.report["tools"] = module.verify_tool_pins(self.scope / "tool-pins.json", TOOLS["sha256"])

        def verify(self):
            self.start_postgres()
            output = self.scope / CATALOG_NAME
            need(not os.path.lexists(output), "catalog_output_exists")
            environment = {key: val for key, val in self.environment.items() if not key.startswith("GOBY_")}
            environment.update(GOBY_TEST_BACKUP_DISPOSABLE_DATABASES="1",
                               GOBY_TEST_BACKUP_SOURCE_DATABASE_URL=self.environment["GOBY_TEST_BACKUP_SOURCE_DATABASE_URL"])
            self.report["generatorAttempts"] = 1
            self.command("generate-schema29-catalog", [module.GO, "run", "-p=1",
                         "scripts/test-env/generate-backuppg-catalog.go", output], timeout=300, environment=environment)
            self.report["generatorCompiledAndExecuted"] = True
            manifest = strict_json(pinned(value["source"]["manifest"]))
            self.report["catalog"] = review_catalog(output, self.source, manifest)
    module.Worker = CatalogWorker
    return module


def materialize(base, resources, report):
    scope = resources.COMPILER / "job"
    if not scope.exists():
        return
    output = Path(SCOPE) / "retained"
    base.mkdir(output, 0o700)
    paths = [scope / name for name in (CATALOG_NAME, "worker-report.json", "report.json", "source-manifest.json",
                                      "module-cache-manifest.json", "config.json", "OWNER.json", "worker.stdout",
                                      "worker.stderr", "unit.stdout", "unit.stderr") if os.path.lexists(scope / name)]
    if (scope / "logs").exists():
        paths.extend(sorted((scope / "logs").iterdir()))
    need(len(paths) <= 128, "retained_file_count")
    state = report.setdefault("materialization", {"files": {}, "bytes": 0, "complete": False})
    for index, path in enumerate(paths):
        maximum = CATALOG_LIMIT if path.name == CATALOG_NAME else 128 << 20
        raw = read_file(path, maximum)
        need(state["bytes"] + len(raw) <= RETAINED_LIMIT, "retained_byte_limit")
        target = output / (f"{index:03d}-" + path.name)
        state["files"][str(path)] = {"destination": str(target), "creationAttempted": True}
        base.write_new(target, raw)
        need(read_file(target, maximum) == raw, "retained_readback_changed")
        state["files"][str(path)].update(pin={"path": str(target), "bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest()},
                                        readbackMatched=True)
        state["bytes"] += len(raw)
    state["complete"] = True


def execute(value, selected):
    root = Path(SCOPE)
    support, resources = libraries()
    base = support.load_base()
    volume = resources.compiler_primitives(support)
    volume.private_directory(base, root)
    volume.private_directory(base, root / "private")
    need(not any(os.path.lexists(root / name) for name in ("execution.json", "execution-intent.json", "retained")), "input_consumed")
    base.E, base.P, base.serial = root, root / "private/commands", 0
    base.P.mkdir(mode=0o700)
    report = {"kind": "m5-user-deletion-catalog-execution", "version": 1, "status": "preparing", "purpose": PURPOSE,
              "input": selected, "commands": [], "ownedUnits": {}, "createdPaths": [], "cleanupErrors": [],
              "singleRunFullSuite": False, "ordinarySuitePassed": False, "productBuildExecuted": False,
              "testsExecuted": 0, "generatorAttempts": 0, "resourcesClosed": False, "lockReleased": False,
              "limits": {"workerMemoryBytes": MEMORY_BYTES, "workerSwapBytes": 0, "workspaceBytes": VOLUME_BYTES,
                         "generatorCommandSeconds": 300, "generatorContextSeconds": 120, "workerBusinessSeconds": 420,
                         "workerRuntimeSeconds": 600, "workerStopSeconds": 180, "supervisorWaitSeconds": 630,
                         "supervisorStopCommandSeconds": 210, "outerWorkSeconds": OUTER_WORK_SECONDS,
                         "outerClosureSeconds": OUTER_CLOSE_SECONDS, "retainedBytes": RETAINED_LIMIT},
              "compilerVolume": {"status": "not_started", "closed": False, "imagePath": str(resources.COMPILER_IMAGE),
                                 "mountPath": str(resources.COMPILER), "imageBytes": VOLUME_BYTES}}
    base.report = report
    lock = None
    guard = None
    worker = None
    protected = None
    supervisor_started = False
    def interrupted(_number, _frame):
        raise Rejected("catalog_outer_interrupted")
    for number in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP, signal.SIGALRM):
        signal.signal(number, interrupted)
    signal.alarm(OUTER_WORK_SECONDS)
    try:
        report["sourceReview"] = support.source_snapshot(base, value["source"])
        guard = load_module(value["runtimeGuard"]["helper"], "catalog_current_protection")
        guard_input = strict_json(pinned(value["runtimeGuard"]["input"]))
        lock, lock_metadata = guard.acquire_lock(base)
        protected = guard.protected(base, guard_input)
        report["protectedBefore"] = protected
        volume.tool_snapshot(base, strict_json(pinned(INFRASTRUCTURE))["files"])
        root_capacity = os.statvfs(root)
        memory = dict(line.split(":", 1) for line in Path("/proc/meminfo").read_text().splitlines())
        capacity = {"rootFreeBytes": root_capacity.f_bavail * root_capacity.f_frsize,
                    "rootFreeInodes": root_capacity.f_favail,
                    "memAvailableBytes": int(memory["MemAvailable"].split()[0]) * 1024}
        need(capacity["rootFreeBytes"] >= VOLUME_BYTES + RETAINED_LIMIT + ROOT_RESERVE
             and capacity["rootFreeInodes"] >= 10000 and capacity["memAvailableBytes"] >= 3 << 30, "catalog_capacity")
        report["capacityBefore"] = capacity
        base.write_new(root / "execution-intent.json", encoded({"input": selected, "purpose": PURPOSE, "singleUse": True}))
        descriptor = resources.prepare_compiler_volume(support, base, volume, report)
        base.mkdir(resources.FIXTURES, 0o700)
        def volume_gate():
            resources.compiler_mount(volume, report["compilerVolume"])
            need(base.metadata(resources.COMPILER)["inode"] == report["compilerVolume"]["mountMetadata"]["inode"], "workspace_mount_changed")
        worker = worker_module(value, selected, resources, descriptor, volume_gate)
        need(worker.property_map(UNIT, ["LoadState"]).get("LoadState") == "not-found", "catalog_unit_occupied")
        worker.verify_tool_pins(Path(TOOLS["path"]), TOOLS["sha256"])
        need(guard.protected(base, guard_input) == protected and base.metadata(base.LOCK) == lock_metadata, "protected_changed_before_catalog")
        args = types.SimpleNamespace(mode="catalog", archive=Path(value["source"]["archive"]["path"]),
                                     archive_sha256=value["source"]["archive"]["sha256"], module_cache=MODULES,
                                     tool_pins=Path(TOOLS["path"]), tool_pins_sha256=TOOLS["sha256"], command_timeout=300, runtime=600)
        supervisor_started = True
        report["generatorAttempts"] = None
        report["workerExitCode"] = worker.supervisor(args)
        scope = resources.COMPILER / "job"
        result = strict_json(read_file(scope / "worker-report.json", 4 << 20))
        need(type(result.get("generatorAttempts")) is int and result["generatorAttempts"] in (0, 1), "generator_attempt_count")
        report["generatorAttempts"] = result["generatorAttempts"]
        invocations = [row for row in result["commands"] if row.get("label") == "generate-schema29-catalog"]
        need(report["workerExitCode"] == 0 and result["status"] == WORKER_PASS and result["generatorAttempts"] == 1
             and len(invocations) == 1 and invocations[0]["exit_code"] == 0
             and invocations[0]["interrupted_or_timed_out"] is False
             and result["singleRunFullSuite"] is False and result["ordinary_suite_passed"] is False
             and result["build_executed"] is False and all(count == 0 for count in result["test_counts"].values())
             and all(result["cleanup"].get(key) is True for key in ("owned_postgres_stopped", "private_bind_removed", "source_unchanged", "only_worker_process_remains")),
             "catalog_worker_or_pg_closure_failed")
        report["catalog"] = review_catalog(scope / CATALOG_NAME, scope / "source", strict_json(pinned(value["source"]["manifest"])))
        need(report["catalog"] == result["catalog"], "catalog_result_changed_after_worker")
        report["status"] = "catalog_generated"
    except BaseException as error:
        report.update(status="failed", failure={"type": type(error).__name__, "code": str(error) if isinstance(error, Rejected) else "catalog_execution_failed"})
    finally:
        signal.alarm(OUTER_CLOSE_SECONDS)
        for number in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
            signal.signal(number, signal.SIG_IGN)
        try:
            if supervisor_started:
                scope = resources.COMPILER / "job"
                terminal = worker.property_map(UNIT, ["LoadState", "ActiveState", "MainPID"])
                need(worker.own_cgroup_empty(UNIT) and terminal.get("MainPID") == "0"
                     and terminal.get("ActiveState") not in ("active", "activating", "deactivating"), "owned_catalog_unit_not_closed")
                if (scope / "report.json").exists():
                    result = strict_json(read_file(scope / "report.json", 4 << 20))
                    need(result.get("scope") == str(scope) and result.get("unit") == UNIT, "supervisor_record_binding")
                    report["unitDispatchAttempted"] = result.get("unitDispatchAttempted")
                    if result.get("unitDispatchAttempted") is False:
                        need(result.get("status") == "failed" and terminal.get("LoadState") == "not-found",
                             "undispatched_supervisor_closure")
                        # The actual absent unit/cgroup checks above close only
                        # this explicitly undispatched failed preparation.
                        report["status"] = "failed"
                    else:
                        need(result.get("recursive_cgroup_empty") is True and result.get("private_unit_empty") is True,
                             "supervisor_closure_record")
                else:
                    report["supervisorReportMissing"] = True
                    raise Rejected("supervisor_closure_record_missing")
                report["terminalUnit"] = terminal
            report["ownedProcessesClosed"] = True
            materialize(base, resources, report)
            resources.close_compiler_volume(support, base, volume, report)
            report["resourcesClosed"] = report["compilerVolume"].get("closed") is True
            if guard is not None and protected is not None:
                report["protectedAfter"] = guard.protected(base, guard_input)
                need(report["protectedAfter"] == protected and base.metadata(base.LOCK) == lock_metadata, "protected_changed_after_catalog")
        except BaseException as error:
            report["status"] = "failed"
            failure = {"type": type(error).__name__, "code": str(error) if isinstance(error, Rejected) else "catalog_closure_failed"}
            report["cleanupErrors"].append(failure)
            report.setdefault("failure", dict(failure))
        finally:
            signal.alarm(0)
            if lock is not None:
                lock_cleanup = {"unlockSucceeded": False, "closeSucceeded": False}
                report["lockCleanup"] = lock_cleanup
                try:
                    try:
                        fcntl.flock(lock, fcntl.LOCK_UN)
                        lock_cleanup["unlockSucceeded"] = True
                    except BaseException as error:
                        failure = {"phase": "lockUnlock", "type": type(error).__name__, "code": "lock_unlock_failed"}
                        report["cleanupErrors"].append(failure)
                        report.setdefault("failure", dict(failure))
                        report["status"] = "failed"
                finally:
                    try:
                        os.close(lock)
                        lock_cleanup["closeSucceeded"] = True
                    except BaseException as error:
                        failure = {"phase": "lockClose", "type": type(error).__name__, "code": "lock_close_failed"}
                        report["cleanupErrors"].append(failure)
                        report.setdefault("failure", dict(failure))
                        report["status"] = "failed"
                report["lockReleased"] = lock_cleanup["unlockSucceeded"] and lock_cleanup["closeSucceeded"]
            report["allOwnedCommandsClosed"] = all(row.get("closed") is True for row in report["commands"])
            if report["status"] == "catalog_generated" and report["resourcesClosed"] and report["allOwnedCommandsClosed"]:
                report["status"] = "catalog_generated_and_closed"
            else:
                report["status"] = "failed"
            receipt = None
            try:
                created = base.write_new(root / "execution.json", encoded(report))
                receipt = pin(root / "execution.json", 8 << 20)
                need(receipt["sha256"] == created["sha256"], "execution_readback_changed")
            except BaseException:
                receipt = None
                report["status"] = "failed"
                report["executionPublicationFailed"] = True
                try:
                    created = base.write_new(root / "execution-publication-failure.json", encoded(report))
                    receipt = pin(root / "execution-publication-failure.json", 8 << 20)
                    need(receipt["sha256"] == created["sha256"], "fallback_readback_changed")
                except BaseException:
                    receipt = None
                    report["fallbackPublicationFailed"] = True
            print(json.dumps({"status": report["status"], "receipt": receipt, "resourcesClosed": report["resourcesClosed"],
                              "recordPublicationFailed": receipt is None, "scope": str(root),
                              "testsExecuted": 0, "productBuildExecuted": False}, sort_keys=True))
    return 0 if report["status"] == "catalog_generated_and_closed" else 1


def main():
    root = bindings()
    need(sys.platform == "linux" and os.geteuid() == 0 and sys.flags.isolated and sys.flags.dont_write_bytecode,
         "isolated_linux_root_required")
    os.umask(0o077)
    if len(sys.argv) == 3 and sys.argv[1] == "--worker":
        scope = Path(sys.argv[2])
        need(scope == root / "workspace/job", "catalog_worker_path")
        config = strict_json(read_file(scope / "config.json", 4 << 20))
        selected = config["catalog_input"]
        value = input_value(selected, worker=True)
        _support, resources = libraries()
        worker = worker_module(value, selected, resources, config["compiler_volume"], None)
        return worker.Worker(scope).run()
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True)
    parser.add_argument("--input-sha256", required=True)
    arguments = parser.parse_args()
    need("SSH_CONNECTION" in os.environ, "remote_execution_required")
    selected = {"path": arguments.input, "sha256": arguments.input_sha256}
    return execute(input_value(selected), selected)


if __name__ == "__main__":
    raise SystemExit(main())
