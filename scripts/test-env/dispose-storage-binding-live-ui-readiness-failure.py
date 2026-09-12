#!/usr/bin/env python3
"""Dispose only the sealed storage-binding UI readiness inspection failure.

The fixed failed report, runtime identity, database OIDs and unit invocations
are mandatory authority. Check-only performs bounded read-only inspection and
does not create an execution directory. An execution is permanently one-shot,
including a partial failure. The initialized schema28 database must exactly
match the sealed bootstrap-only rows and sequence states. It never invokes
the historical operator's broken sequence snapshot, starts a browser,
changes HBA, terminates a connection, or rewrites the failed scope.
"""

from __future__ import annotations

import argparse
import copy
import fcntl
import hashlib
import json
import os
from pathlib import Path
import pwd
import re
import signal
import stat
import sys
import types

sys.dont_write_bytecode = True
WORK = Path("/opt/goby-test/exec-work-m3e")
RUN = "20260912_075346_bf9011cca3e0"
TOOL = WORK / "storage-binding-live-ui-tool-02"
OPERATOR = TOOL / "verify-storage-binding-live-ui.py"
OUTPUT = WORK / ("storage-binding-live-ui-" + RUN)
RUNTIME = Path("/opt/goby-binding-ui-runtime-" + RUN)
HISTORICAL_EXECUTION = WORK / "storage-binding-live-ui-execution-02"
EXECUTION = WORK / "storage-binding-live-ui-readiness-disposal-01"
INITIAL_SNAPSHOT = HISTORICAL_EXECUTION / "initial-state-inspection.json"
INITIAL_SNAPSHOT_SHA = "7a1a8ba0b351eba71581d605b723a04ebe475e88c1d7112b2bce162880f1df79"
OPERATOR_SHA = "19d00d6311901119583df5b84ab6eec1d3c9b93c6efb160b7f53f00461aaed7b"
INTENT_SHA = "2194deada12a61654bf49c170a4d6bab382d59a333cca0f2f156d3ac749df6a2"
REPORT_SHA = "7a96989dc7306f79ea596f8d5b0795953507a5a26f45312215118c8386e1a675"
WORKER_SHA = "216bec0f7f8ede599f59af535a35441172803c455c4022b4cf9ac155c743274f"
CATALOG_SHA = "78c26be6733cc43406dcfa7d798ec5d40e9b4050bef89e021c5a3fdf7d0fc9f8"
RUNTIME_OWNER_SHA = "e608ce9e63a04a3e9ceb46528271de42cc9cebcb975e2b3762d776b63b8849be"
ROLE_OID, DATABASE_OID = 18926080, 18926081
TAG = "goby-storage-binding-live-ui-v1:" + RUN
MARKER = "goby-storage-binding-live-ui-readiness-disposal-v1"
UNIT_PINS = {
    "controller": "d44b1bb27c8d48deb484bedd17452f0a",
    "worker": "36bac17cf2274fdda0b35e11fd86463a",
}
PINNED = {
    OUTPUT / "report.json": REPORT_SHA,
    OUTPUT / "private/worker-result.json": WORKER_SHA,
    OUTPUT / "catalog-before.json": CATALOG_SHA,
    OUTPUT / "private/application.log": "f6045e5b43fec41c2a078014dc5045a5acf874211c85258bad6161f74d3b28f7",
    OUTPUT / "private/http-001.json": "0b838e0b6ccce8fffcd8c6d8835408ad737c9ab8e732c9465e43919e76102522",
    OUTPUT / "private/worker-admitted.json": "cb9cbd86c4c9d60eef3ea4cc8d21a99c2cd7696a7fa515fe75007398bf4a3877",
    OUTPUT / "receipt-009.json": "e6a7da918c3d8409f057462e00c39362fd008662e08de4d17ac6ccc433cb2c97",
    HISTORICAL_EXECUTION / "failed-terminal.json": "fc9a4b8528a7a0c0d9b9f0f05334b1593df97f220226cba8c0c521738f86bccf",
    INITIAL_SNAPSHOT: INITIAL_SNAPSHOT_SHA,
}
INITIAL_COUNTS = {"managed_settings": 1, "schema_migrations": 28, "server_settings": 2,
                  "task_definitions": 1, "theme_owner_ids": 1}
APP_FILES = {
    "operations/.goby-recovery-control.json": 148,
    "operations/cas-proof.json": 502,
    "operations/current.json": 526,
    "operations/.goby-recovery-control.lock": 0,
    "diagnostics/.goby-diagnostics.lock": 0,
    "diagnostics/.goby-diagnostics.json": 266,
    "diagnostics/goby-6a4e8938cdf4dc3209f44ceb71ff0ad6-6a15b2dce19eda06a758ecaa147ddd07.jsonl": 709,
    "backups/.goby-backup-store.json": 75,
    "backups/.goby-backup-store.lock": 0,
    "backups/.goby-backup-catalog.json": 69,
    "recovery/.goby-lifecycle.lock": 0,
    "recovery/.goby-lifecycle.json": 103,
    "recovery/generation-registry.json": 165,
}
PREVIOUS_ROOTS = (
    WORK / "storage-binding-live-ui-20260912_074733_ca8294cb9f47",
    WORK / "storage-binding-live-ui-execution-01",
    WORK / "storage-binding-live-ui-startup-disposal-01",
)
PREVIOUS_DISPOSAL_SHA = "a5c248135d8c7d4abe4a6727de523431a9a45e917cad1f83f90754b7ba007b24"
MAX_TREE_ENTRIES = 512
MAX_TREE_BYTES = 128 << 20


def require(value, message):
    if not value:
        raise RuntimeError(message)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def stable_stat(info):
    return (info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode,
            info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def global_oid(value):
    """PostgreSQL's JSON representation of the oid type is a decimal string."""
    require(type(value) is str and re.fullmatch(r"[1-9][0-9]{0,9}", value) is not None,
            "A global catalog OID is not canonical decimal text.")
    number = int(value)
    require(number <= 4294967295, "A global catalog OID exceeds PostgreSQL's range.")
    return number


def global_rows(value):
    require(type(value) is list and len(value) <= 20000 and all(type(row) is dict and
            type(row.get("name")) is str and row["name"] for row in value), "Invalid global catalog rows.")
    identifiers = [global_oid(row.get("oid")) for row in value]
    require(identifiers == sorted(identifiers) and len(set(identifiers)) == len(value) and
            len({row["name"] for row in value}) == len(value), "Global catalog identities are unordered or ambiguous.")
    return value


def sync_directory(directory):
    descriptor = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def secure_read(filename, limit=4 << 20):
    """Read the wrapper and frozen operator before trusting imported helpers."""
    require(filename.is_absolute() and str(filename) == os.path.normpath(str(filename)), "Noncanonical source path.")
    for ancestor in reversed((filename, *filename.parents)):
        info = ancestor.lstat()
        require(not stat.S_ISLNK(info.st_mode) and info.st_uid == 0 and info.st_gid == 0 and
                not info.st_mode & 0o022, "A source path is not exclusively root controlled.")
    before = filename.lstat()
    require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_size <= limit, "Invalid source file.")
    with os.fdopen(os.open(filename, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
        require(stable_stat(os.fstat(stream.fileno())) == stable_stat(before), "Source changed before read.")
        raw = stream.read(limit + 1)
        require(stable_stat(os.fstat(stream.fileno())) == stable_stat(before), "Source changed during read.")
    require(len(raw) == before.st_size and len(raw) <= limit and stable_stat(filename.lstat()) == stable_stat(before),
            "Source changed after read.")
    return raw


def load_operator():
    raw = secure_read(OPERATOR)
    require(digest(raw) == OPERATOR_SHA, "The exact historical operator is required.")
    module = types.ModuleType("storage_binding_readiness_disposal_operator")
    module.__file__ = str(OPERATOR)
    exec(compile(raw, str(OPERATOR), "exec"), module.__dict__)
    require(module.TOOL == TOOL and module.WORK == WORK and module.PORT == 18288 and
            module.MARKER == "goby-storage-binding-live-ui-v1", "Historical operator scope mismatch.")
    return module


def tree(module, root, *, runtime=False, gid=0):
    """Bound membership before retaining entries; reject links and special files."""
    result, pending, total = {}, [root], 0
    while pending:
        current = pending.pop()
        before = module.path_info(current)
        require(len(result) < MAX_TREE_ENTRIES, "Tree entry budget exceeded.")
        is_directory = stat.S_ISDIR(before.st_mode)
        require(is_directory or stat.S_ISREG(before.st_mode), "A retained tree contains an unsupported object.")
        require((before.st_uid in (0, 995) and before.st_gid in (0, gid)) if runtime else
                (before.st_uid == 0 and before.st_gid == 0), "A retained tree owner changed.")
        require(not before.st_mode & 0o022, "A retained tree permits untrusted writes.")
        row = dict(module.identity(before), kind="directory" if is_directory else "file",
                   mtime_ns=before.st_mtime_ns, ctime_ns=before.st_ctime_ns)
        if is_directory:
            children = []
            with os.scandir(current) as entries:
                for entry in entries:
                    require(len(result) + len(pending) + len(children) < MAX_TREE_ENTRIES, "Tree entry budget exceeded.")
                    children.append(current / entry.name)
            require(stable_stat(current.lstat()) == stable_stat(before), "Tree membership changed during enumeration.")
            pending.extend(sorted(children, reverse=True))
        else:
            require(before.st_nlink == 1 and before.st_size <= MAX_TREE_BYTES - total, "Tree file/link budget exceeded.")
            total += before.st_size
            raw = module.read_file(current, uid=before.st_uid, gid=before.st_gid,
                                   modes=(stat.S_IMODE(before.st_mode),), limit=before.st_size)
            require(stable_stat(current.lstat()) == stable_stat(before), "Tree file changed during observation.")
            row.update(bytes=len(raw), nlink=1, sha256=digest(raw))
        result[str(current.relative_to(root))] = row
    return result


def failed_units(module, intent):
    result = {}
    for kind, invocation in UNIT_PINS.items():
        unit = intent[kind + "_unit"]
        state = module.unit_state(unit)
        module.unit_owner(state, unit, TAG)
        expected = {"LoadState": "loaded", "ActiveState": "failed", "SubState": "failed", "MainPID": "0",
                    "InvocationID": invocation, "ExecMainStatus": "1", "Result": "exit-code"}
        require(all(state.get(key) == value for key, value in expected.items()), "A sealed failed unit changed or is live.")
        module.cgroup_empty(unit)
        result[kind] = {key: state[key] for key in expected}
    return result


class Disposal:
    def __init__(self, module, script_path, script_sha):
        self.m, self.script_path, self.script_sha = module, script_path, script_sha
        self.intent = module.load_intent(TOOL / "intent.json", INTENT_SHA)
        require(self.intent["run_id"] == RUN and self.intent["output"] == str(OUTPUT) and
                self.intent["runtime"] == str(RUNTIME), "The intent is outside the authorized failed run.")
        self.inputs = module.verify_inputs(self.intent)
        self.controller = module.Controller(self.intent, INTENT_SHA, self.inputs)
        self.controller.module = module.load_workspace(self.inputs["selected"])
        self.controller.postgres = pwd.getpwnam("postgres")
        self.controller.goby = pwd.getpwnam("goby")
        require(self.controller.goby.pw_uid == 995, "The service account changed.")
        self.lock = None
        self.started = False
        self.sequence = 0
        self.previous = None
        self.phase = "inspection"
        self.created = {}

    def acquire(self):
        c, m = self.controller, self.m
        c.module.verify_parents(c.postgres)
        self.lock = c.module.acquire_lock()
        raw = m.read_file(c.module.OWNER, modes=(0o600,))
        c.owner, c.owner_sha = m.decode(raw), digest(raw)
        c.module.validate_owner(c.owner, c.module.expected_owner(c.postgres, c.module.binaries()),
                                c.module.directory(m.CONTROL, 0, 0))
        c.module.verify_directories(c.postgres, c.owner)
        c.module.verify_configuration(c.postgres)
        self.hba = c.module.HBA.encode()
        c.check_cluster(self.hba)

    def historical(self):
        m = self.m
        for filename, expected in PINNED.items():
            require(digest(m.read_file(filename, modes=(0o600,), limit=8 << 20)) == expected, "Pinned failure evidence changed.")
        require(digest(m.read_file(PREVIOUS_ROOTS[2] / "report.json", modes=(0o600,), limit=8 << 20)) == PREVIOUS_DISPOSAL_SHA,
                "The preceding startup disposal evidence changed.")
        require(m.decode(m.read_file(OUTPUT / "intent.json", modes=(0o600,))) == self.intent, "The failed intent copy changed.")
        require(m.read_file(OUTPUT / "hba-before", modes=(0o600,)) == self.hba, "Historical HBA differs from the baseline.")
        require({item.name for item in OUTPUT.glob("receipt-*.json")} ==
                {"receipt-%03d.json" % number for number in range(1, 10)}, "The failed receipt sequence changed.")
        require(not m.present(OUTPUT / "terminal.json"), "The failed scope acquired an unexpected success terminal.")
        private = OUTPUT / "private"
        for name in ("browser", "browser-fixture.json", "browser-result.json", "browser-session.json",
                     "browser-stdout.json", "browser-stderr.txt", "initial-database.json", "final-database.json",
                     "controller-final-database.json"):
            require(not m.present(private / name), "Browser or completed workflow evidence appeared.")
        require({item.name for item in private.glob("http-*.json")} == {"http-001.json"},
                "HTTP observations differ from the single pinned readiness response.")
        m.directory(private / "ipc")
        require(not any((private / "ipc").iterdir()), "A browser IPC stage appeared.")
        return {str(root): tree(m, root) for root in (OUTPUT, HISTORICAL_EXECUTION, *PREVIOUS_ROOTS)}

    def runtime(self):
        m, c = self.m, self.controller
        require(m.directory(RUNTIME, 0, c.goby.pw_gid, 0o750) == c.runtime_identity, "The original 0750 runtime changed.")
        raw = m.read_file(RUNTIME / "owner.json", modes=(0o600,))
        require(digest(raw) == RUNTIME_OWNER_SHA and m.decode(raw) == {
            "marker": m.MARKER, "run_id": RUN, "intent_sha256": INTENT_SHA, "identity": c.runtime_identity},
            "The runtime owner marker changed.")
        inventory = tree(m, RUNTIME, runtime=True, gid=c.goby.pw_gid)
        expected_files = {"owner.json", "goby"} | {"web/" + name for name in self.inputs["web"]}
        actual_files = {name for name, row in inventory.items() if row["kind"] == "file"}
        app_files = {"app/" + name: size for name, size in APP_FILES.items()}
        require(actual_files == expected_files | set(app_files),
                "The failed runtime contains a missing or unrelated file.")
        require(c.goby.pw_gid == 986 and all(inventory[name]["uid"] == 995 and inventory[name]["gid"] == 986 and
            inventory[name]["mode"] == 0o600 and inventory[name]["bytes"] == size for name, size in app_files.items()),
            "An initialized app file differs from its observed private metadata.")
        require(inventory["goby"]["sha256"] == m.BINARY_SHA and inventory["goby"]["mode"] == 0o550,
                "The failed runtime executable changed.")
        require(all(inventory["web/" + name]["sha256"] == value for name, value in self.inputs["web"].items()),
                "The runtime web copy changed.")
        mounts = Path("/proc/self/mountinfo").read_bytes()
        require(len(mounts) <= 4 << 20 and str(RUNTIME).encode() not in mounts, "A runtime mount remains visible on the host.")
        return inventory

    def no_sessions(self):
        db = self.controller.db
        value = self.m.decode(db.maintenance("SELECT jsonb_build_object('sessions',"
            f"(SELECT count(*) FROM pg_stat_activity WHERE datid={DATABASE_OID} OR usesysid={ROLE_OID}),"
            f"'prepared',(SELECT count(*) FROM pg_prepared_xacts WHERE database='{db.name}' OR owner='{db.role}'),"
            f"'slots',(SELECT count(*) FROM pg_replication_slots WHERE database='{db.name}'));"))
        require(value == {"sessions": 0, "prepared": 0, "slots": 0}, "The owned pair has live or prepared work.")
        return value

    def initialized_database(self):
        m, db = self.m, self.controller.db
        db.verify_owned()
        self.no_sessions()
        objects = db.objects()
        require(objects == self.inputs["catalog"]["objects"], "The initialized objects differ from trusted schema28.")
        public = m.decode(db.query("SELECT jsonb_build_object('oid',oid::bigint,'owner',pg_get_userbyid(nspowner),"
            "'acl',nspacl,'comment',obj_description(oid,'pg_namespace')) FROM pg_namespace WHERE nspname='public';", administrative=True))
        casts = m.decode(db.query("SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY oid),'[]') FROM pg_cast c;", administrative=True))
        require(public == db.pair["public"] and casts == db.pair["casts"], "The original public namespace or cast inventory changed.")
        tables = [entry["Name"] for entry in self.inputs["catalog"]["catalog"]["Tables"]]
        sequences = [entry["Name"] for entry in self.inputs["catalog"]["catalog"]["Sequences"]]
        require(len(tables) == len(set(tables)) == 35 and len(sequences) == len(set(sequences)) == 5 and
                all(type(name) is str and re.fullmatch(r"[a-z_]+", name) for name in tables + sequences),
                "The trusted initialized table or sequence inventory is invalid.")
        counts_sql = "SELECT " + "+".join(f"(SELECT count(*) FROM public.{name})" for name in tables) + ";"
        require(db.query(counts_sql, administrative=True) == b"33", "The initialized database no longer contains exactly its bootstrap rows.")
        entries = [f"SELECT '{name}' AS name,coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]') AS rows "
                   f"FROM public.{name} t" for name in tables]
        # A sequence relation is not a supported whole-row composite for
        # to_jsonb(s). Project its three actual columns explicitly.
        sequence_entries = [f"SELECT '{name}' AS name,jsonb_build_object('last_value',s.last_value,"
                            f"'log_cnt',s.log_cnt,'is_called',s.is_called) AS value FROM public.{name} s"
                            for name in sequences]
        observed = m.decode(db.query("SELECT jsonb_build_object('rows',(SELECT jsonb_object_agg(name,rows) FROM (" +
            " UNION ALL ".join(entries) + ") x),'sequences',(SELECT jsonb_object_agg(name,value) FROM (" +
            " UNION ALL ".join(sequence_entries) + ") x));", administrative=True), 8 << 20)
        m.exact(observed, {"rows", "sequences"})
        require(type(observed["rows"]) is dict and set(observed["rows"]) == set(tables) and
                type(observed["sequences"]) is dict and set(observed["sequences"]) == set(sequences),
                "The initialized snapshot has missing or extra tables or sequences.")
        require(all(type(rows) is list and len(rows) == INITIAL_COUNTS.get(name, 0)
                    for name, rows in observed["rows"].items()), "Application rows appeared outside the sealed bootstrap state.")
        for value in observed["sequences"].values():
            m.exact(value, {"last_value", "log_cnt", "is_called"})
            require(type(value["last_value"]) is int and value["last_value"] >= 1 and
                    type(value["log_cnt"]) is int and value["log_cnt"] >= 0 and type(value["is_called"]) is bool,
                    "An initialized sequence state is invalid.")
        require(observed == self.initial_snapshot and digest(m.canonical(observed)) == INITIAL_SNAPSHOT_SHA,
                "The initialized rows or sequence states differ from the sealed independent inspection.")
        require(db.objects() == objects, "The initialized catalog changed during the complete snapshot.")
        db.verify_owned()
        self.no_sessions()
        return {"objects_sha256": digest(m.canonical(objects)), "public": public,
                "casts_sha256": digest(m.canonical(casts)), "snapshot_sha256": INITIAL_SNAPSHOT_SHA,
                "row_counts": {name: len(rows) for name, rows in observed["rows"].items()}, "sequence_count": len(sequences)}

    def global_catalog(self, phase):
        current = self.m.decode(self.controller.db.maintenance(self.m.GLOBAL_SQL))
        for key in ("roles", "databases"):
            global_rows(current[key])
        expected = copy.deepcopy(self.before_catalog)
        if phase in ("owned", "closed", "database_removed"):
            expected["roles"] = sorted(expected["roles"] + [self.global_role], key=lambda row: global_oid(row["oid"]))
        if phase in ("owned", "closed"):
            database = dict(self.global_database, connect=phase == "owned")
            expected["databases"] = sorted(expected["databases"] + [database], key=lambda row: global_oid(row["oid"]))
        require(current == expected, "The global catalog changed outside the exact owned pair transition.")
        return current

    def preservation(self, *, runtime=True):
        m, c = self.m, self.controller
        require(digest(secure_read(self.script_path)) == self.script_sha, "The disposal script changed.")
        require(digest(secure_read(OPERATOR)) == OPERATOR_SHA, "The historical operator changed.")
        c.check_cluster(self.hba)
        require(m.host_fact() == self.failed["host_before"] and m.protected_facts() == self.failed["protected_before"],
                "The host or protected application changed.")
        m.listener()
        failed_units(m, self.intent)
        require(self.historical() == self.before_trees, "The historical evidence tree changed.")
        require(digest(m.read_file(m.HISTORY, modes=(0o600,))) == self.intent["history_sha256"], "The shared historical receipt changed.")
        if runtime:
            require(self.runtime() == self.before_runtime, "The failed runtime changed before removal.")
        else:
            require(not m.present(RUNTIME), "The disposed runtime reappeared.")
        for filename, expected in self.created.items():
            require(digest(m.read_file(filename, modes=(0o600,), limit=8 << 20)) == expected, "Disposal evidence changed.")

    def preflight(self):
        m, c = self.m, self.controller
        require(not m.present(EXECUTION), "This disposal namespace is consumed; automatic retry is forbidden.")
        self.acquire()
        self.failed = m.decode(m.read_file(OUTPUT / "report.json", modes=(0o600,), limit=8 << 20))
        worker = m.decode(m.read_file(OUTPUT / "private/worker-result.json", modes=(0o600,)))
        receipt = m.decode(m.read_file(OUTPUT / "receipt-009.json", modes=(0o600,)))
        for document in (self.failed, worker, receipt):
            require(document.get("marker") == m.MARKER and document.get("run_id") == RUN and
                    document.get("intent_sha256") == INTENT_SHA, "A failure record identifies another scope.")
        require(self.failed.get("status") == worker.get("status") == "failed" and
                self.failed.get("workflow_verified") is not True and self.failed.get("pair_retained") is True and
                worker.get("stages") == [], "This tool cannot dispose a completed or user-operated UI scope.")
        require(worker.get("cleanup", {}).get("backend_terminal") is True and
                worker.get("cleanup", {}).get("mounts_removed") is True,
                "The ready backend or private mounts did not complete cleanup.")
        require(all(self.failed.get("cleanup", {}).get(name) is True for name in
            ("hba_restored", "historical_receipt_unchanged", "protected_unchanged", "host_unchanged", "port_closed", "inputs_unchanged")),
            "The failed controller did not preserve its required boundaries.")
        require(self.failed["controller"]["invocation_id"] == UNIT_PINS["controller"] and
                receipt["worker_invocation"] == UNIT_PINS["worker"] and receipt["sequence"] == 9,
                "A failure invocation or receipt sequence changed.")
        pair = self.failed["pair"]
        m.exact(pair, {"role_oid", "database_oid", "phase", "database", "public", "casts"})
        require(pair["role_oid"] == ROLE_OID and pair["database_oid"] == DATABASE_OID and
                pair["phase"] == "owned" and receipt["pair"] == pair, "The fixed pair is no longer completely owned.")
        c.db.pair = copy.deepcopy(pair)
        # Never connect using the historical application's secret, and never
        # let an inherited journal append to its immutable receipt sequence.
        c.db.password = None
        c.db.journal = lambda: None
        c.runtime_identity = self.failed["runtime_identity"]
        require(m.directory(OUTPUT) == receipt["output_identity"], "The failed evidence root changed.")
        initial_raw = m.read_file(INITIAL_SNAPSHOT, modes=(0o600,), limit=8 << 20)
        require(digest(initial_raw) == INITIAL_SNAPSHOT_SHA, "The independent initialized snapshot changed.")
        self.initial_snapshot = m.decode(initial_raw, 8 << 20)
        m.exact(self.initial_snapshot, {"rows", "sequences"})
        self.before_catalog = m.decode(m.read_file(OUTPUT / "catalog-before.json", modes=(0o600,)))
        m.exact(self.before_catalog, {"roles", "databases", "memberships", "settings"})
        for key in ("roles", "databases"):
            global_rows(self.before_catalog[key])
        actual = m.decode(c.db.maintenance(m.GLOBAL_SQL))
        roles = [row for row in global_rows(actual["roles"]) if global_oid(row["oid"]) == ROLE_OID or row["name"] == c.db.role]
        databases = [row for row in global_rows(actual["databases"]) if global_oid(row["oid"]) == DATABASE_OID or row["name"] == c.db.name]
        require(len(roles) == len(databases) == 1 and global_oid(roles[0]["oid"]) == ROLE_OID and roles[0]["name"] == c.db.role and
                global_oid(databases[0]["oid"]) == DATABASE_OID and databases[0]["name"] == c.db.name, "The named pair OIDs are ambiguous.")
        self.global_role, self.global_database = roles[0], databases[0]
        require(all(global_oid(row["oid"]) != ROLE_OID and row["name"] != c.db.role for row in self.before_catalog["roles"]) and
                all(global_oid(row["oid"]) != DATABASE_OID and row["name"] != c.db.name for row in self.before_catalog["databases"]),
                "The original global catalog already contained a target identity.")
        self.before_trees = self.historical()
        self.before_runtime = self.runtime()
        self.global_catalog("owned")
        initialized = self.initialized_database()
        self.role_before = c.db.rows()["role"]
        self.preservation()
        return {"marker": MARKER, "run_id": RUN, "status": "preflight_passed", "script_sha256": self.script_sha,
                "operator_sha256": OPERATOR_SHA, "intent_sha256": INTENT_SHA, "failed_report_sha256": REPORT_SHA,
                "role_oid": ROLE_OID, "database_oid": DATABASE_OID, "database_initialized": True,
                "bootstrap_only": True, "browser_executed": False, "live_ui_acceptance": False,
                "initialized_database": initialized, "initial_snapshot_sha256": INITIAL_SNAPSHOT_SHA,
                "units": failed_units(m, self.intent), "historical_trees_sha256": digest(m.canonical(self.before_trees)),
                "runtime_tree_sha256": digest(m.canonical(self.before_runtime)), "catalog_before_sha256": CATALOG_SHA,
                "hba_sha256": digest(self.hba), "sessions_prepared_slots": self.no_sessions()}

    def publish(self, name, value):
        require(re.fullmatch(r"(?:preflight|historical-trees-before|runtime-tree-before|catalog-after|report|failure|journal-[0-9]{2})\.json", name),
                "A disposal publication escaped the new execution scope.")
        require(self.m.directory(EXECUTION) == self.execution_identity, "The execution directory changed.")
        raw = self.m.canonical(value)
        require(len(raw) <= 8 << 20, "Disposal evidence exceeds its byte budget.")
        filename = EXECUTION / name
        self.m.create_file(filename, raw)
        sync_directory(EXECUTION)
        self.created[filename] = digest(raw)
        return self.created[filename]

    def journal(self, phase):
        self.sequence += 1
        require(self.sequence <= 12, "Disposal journal budget exceeded.")
        self.phase = phase
        self.previous = self.publish("journal-%02d.json" % self.sequence, {"marker": MARKER, "run_id": RUN,
            "sequence": self.sequence, "phase": phase, "previous_sha256": self.previous,
            "role_oid": ROLE_OID, "database_oid": DATABASE_OID, "script_sha256": self.script_sha})

    def execute(self, preflight):
        m, c, db = self.m, self.controller, self.controller.db
        self.preservation()
        self.initialized_database()
        self.global_catalog("owned")
        EXECUTION.mkdir(mode=0o700)
        self.started = True
        self.execution_identity = m.directory(EXECUTION)
        # Persist the one-shot namespace before publishing intent or issuing DDL.
        sync_directory(WORK)
        self.publish("preflight.json", preflight)
        self.publish("historical-trees-before.json", self.before_trees)
        self.publish("runtime-tree-before.json", self.before_runtime)
        self.journal("close_database_intent")
        self.preservation()
        self.initialized_database()
        self.global_catalog("owned")
        self.no_sessions()
        db.maintenance(f"ALTER DATABASE {db.name} ALLOW_CONNECTIONS false;")
        db.pair["phase"] = "closed"
        db.verify_owned(closed=True)
        self.no_sessions()
        self.global_catalog("closed")
        self.journal("database_closed")
        self.preservation()
        db.verify_owned(closed=True)
        self.no_sessions()
        self.global_catalog("closed")
        self.journal("drop_database_intent")
        db.maintenance(f"DROP DATABASE {db.name};")
        db.pair["phase"] = "database_removed"
        require(db.rows() == {"role": self.role_before, "database": None}, "The exact role changed after database removal.")
        self.global_catalog("database_removed")
        self.journal("database_removed")
        self.preservation()
        self.no_sessions()
        require(db.maintenance(f"SELECT (SELECT count(*) FROM pg_shdepend WHERE refclassid='pg_authid'::regclass AND refobjid={ROLE_OID})+"
            f"(SELECT count(*) FROM pg_auth_members WHERE member={ROLE_OID} OR roleid={ROLE_OID} OR grantor={ROLE_OID})+"
            f"(SELECT count(*) FROM pg_db_role_setting WHERE setrole={ROLE_OID})+"
            f"(SELECT count(*) FROM pg_shseclabel WHERE classoid='pg_authid'::regclass AND objoid={ROLE_OID});") == b"0",
            "The exact role still has dependencies, settings or security labels.")
        require(db.rows() == {"role": self.role_before, "database": None}, "The exact role changed before removal.")
        self.global_catalog("database_removed")
        self.journal("drop_role_intent")
        db.maintenance(f"DROP ROLE {db.role};")
        db.pair["phase"] = "removed"
        require(db.rows() == {"role": None, "database": None}, "The owned pair remains after ordinary removal.")
        self.global_catalog("removed")
        self.journal("role_removed")
        self.preservation()
        self.journal("remove_runtime_intent")
        c.remove_runtime()
        self.journal("runtime_removed")
        require(db.rows() == {"role": None, "database": None}, "The removed pair reappeared.")
        final = self.global_catalog("removed")
        self.publish("catalog-after.json", final)
        m.verify_inputs(self.intent)
        self.preservation(runtime=False)
        require(self.historical() == self.before_trees and self.global_catalog("removed") == self.before_catalog,
                "The final historical tree or global catalog changed.")
        self.journal("complete")
        report = dict(preflight, status="disposed", execution=str(EXECUTION), pair_absent=True, runtime_absent=True,
            global_catalog_restored=True, historical_trees_unchanged=True, protected_host_history_inputs_preserved=True,
            ordinary_drop_only=True, connections_terminated=False, schema_initialized_by_disposal=False,
            initial_state_unchanged_before_disposal=True, journal_sha256=self.previous)
        report_sha = self.publish("report.json", report)
        return {"status": "disposed", "run_id": RUN, "report_sha256": report_sha, "live_ui_acceptance": False}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check-only", action="store_true")
    parser.add_argument("--script-sha256", required=True)
    args = parser.parse_args(argv)
    operation = None
    try:
        require(sys.platform == "linux" and os.geteuid() == os.getegid() == 0 and os.environ.get("SSH_CONNECTION"),
                "Use only the authorized root SSH environment on test-env.")
        require(re.fullmatch(r"[0-9a-f]{64}", args.script_sha256) is not None, "An exact script SHA-256 is required.")
        script_path = Path(__file__).absolute()
        require(script_path.is_relative_to(WORK) and digest(secure_read(script_path)) == args.script_sha256,
                "The executing disposal script differs from its reviewed bytes.")
        os.umask(0o077)
        def interrupted(_number, _frame):
            raise RuntimeError("The bounded disposal was interrupted; no retry is permitted after execution starts.")
        for number in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP, signal.SIGALRM):
            signal.signal(number, interrupted)
        signal.alarm(300)
        operation = Disposal(load_operator(), script_path, args.script_sha256)
        result = operation.preflight()
        if not args.check_only:
            result = operation.execute(result)
        code = 0
    except Exception as error:
        reason = str(error) if type(error) is RuntimeError or (operation is not None and isinstance(error, operation.m.Failure)) else type(error).__name__
        if operation is not None and operation.started:
            try:
                operation.publish("failure.json", {"marker": MARKER, "run_id": RUN, "status": "failed",
                    "phase": operation.phase, "last_journal_sha256": operation.previous,
                    "error_type": type(error).__name__, "reason": reason,
                    "retry_permitted": False, "live_ui_acceptance": False})
            except Exception:
                pass
        result = {"status": "failed", "run_id": RUN, "error_type": type(error).__name__, "reason": reason,
                  "execution_started": bool(operation and operation.started), "live_ui_acceptance": False}
        code = 1
    finally:
        if hasattr(signal, "alarm"):
            signal.alarm(0)
        if operation is not None and operation.lock is not None:
            fcntl.flock(operation.lock, fcntl.LOCK_UN)
            os.close(operation.lock)
    print(json.dumps(result, sort_keys=True))
    return code


if __name__ == "__main__":
    raise SystemExit(main())
