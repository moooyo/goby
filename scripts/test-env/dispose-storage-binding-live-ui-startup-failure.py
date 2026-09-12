#!/usr/bin/env python3
"""Dispose only the sealed, uninitialized storage-binding UI startup failure.

The fixed failed report, runtime identity, database OIDs and unit invocations
are mandatory authority. Check-only performs bounded read-only inspection and
does not create an execution directory. An execution is permanently one-shot,
including a partial failure. It never initializes a schema, starts a browser,
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
RUN = "20260912_074733_ca8294cb9f47"
TOOL = WORK / "storage-binding-live-ui-tool-01"
OPERATOR = TOOL / "verify-storage-binding-live-ui.py"
OUTPUT = WORK / ("storage-binding-live-ui-" + RUN)
RUNTIME = Path("/opt/goby-binding-ui-runtime-" + RUN)
HISTORICAL_EXECUTION = WORK / "storage-binding-live-ui-execution-01"
EXECUTION = WORK / "storage-binding-live-ui-startup-disposal-01"
OPERATOR_SHA = "90d920ce367399480f2228e10b4c7fb4bde06acc196792e86d3a922f78c76a8a"
INTENT_SHA = "ddef05716037cea8a1ea451b741638f115f4b20a002e204ca07b91f20bb66abc"
REPORT_SHA = "1596f8941f1938ced001d55379152475f0871641ce5b493673b54153875a1d6d"
WORKER_SHA = "a87de371f522fb70b93d820e81663d66865467d3c22a953f1160ddaa781759c6"
CATALOG_SHA = "78c26be6733cc43406dcfa7d798ec5d40e9b4050bef89e021c5a3fdf7d0fc9f8"
RUNTIME_OWNER_SHA = "9aacb5769abbd2febfb4666c5b810f0aae0fed87e2d55aadb02f7cf89ee84ac0"
ROLE_OID, DATABASE_OID = 18926078, 18926079
TAG = "goby-storage-binding-live-ui-v1:" + RUN
MARKER = "goby-storage-binding-live-ui-startup-disposal-v1"
UNIT_PINS = {
    "controller": "6f1883bd35074e77a645235021b34979",
    "worker": "fb0ff6acfd8745cdbbedb4cd1a188f00",
}
PINNED = {
    OUTPUT / "report.json": REPORT_SHA,
    OUTPUT / "private/worker-result.json": WORKER_SHA,
    OUTPUT / "catalog-before.json": CATALOG_SHA,
    OUTPUT / "private/application.log": "e81fbca993a87b940829f7ff2482a146f5e3477235eb206dad012bbc8ab672cf",
    OUTPUT / "private/worker-admitted.json": "17303fc58330a66dbd9041e9c229f1dffac05d86549c268280b3e476e050159c",
    OUTPUT / "receipt-009.json": "a3c7eec80ba09cb25ad8b67400a3f89cdc15bf0a36c153a512b38aca8b3f37db",
    HISTORICAL_EXECUTION / "failed-terminal.json": "ed5eb5066e2350940f9e6e7252511e8d59ceb7b023cb06cc70717bfa3b01a580",
}
OBJECT_CATALOGS = (
    "pg_class", "pg_proc", "pg_type", "pg_constraint", "pg_trigger", "pg_attrdef",
    "pg_collation", "pg_conversion", "pg_operator", "pg_opclass", "pg_opfamily",
    "pg_ts_config", "pg_ts_dict", "pg_ts_parser", "pg_ts_template", "pg_rewrite",
    "pg_extension", "pg_language", "pg_am", "pg_amop", "pg_amproc",
)
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
    module = types.ModuleType("storage_binding_startup_disposal_operator")
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
        require(m.decode(m.read_file(OUTPUT / "intent.json", modes=(0o600,))) == self.intent, "The failed intent copy changed.")
        require(m.read_file(OUTPUT / "hba-before", modes=(0o600,)) == self.hba, "Historical HBA differs from the baseline.")
        require({item.name for item in OUTPUT.glob("receipt-*.json")} ==
                {"receipt-%03d.json" % number for number in range(1, 10)}, "The failed receipt sequence changed.")
        require(not m.present(OUTPUT / "terminal.json"), "The failed scope acquired an unexpected success terminal.")
        private = OUTPUT / "private"
        for name in ("browser", "browser-fixture.json", "browser-result.json", "browser-session.json",
                     "browser-stdout.json", "browser-stderr.txt", "initial-database.json", "final-database.json",
                     "controller-final-database.json"):
            require(not m.present(private / name), "Browser or initialized database evidence appeared.")
        m.directory(private / "ipc")
        require(not any((private / "ipc").iterdir()), "A browser IPC stage appeared.")
        return {str(root): tree(m, root) for root in (OUTPUT, HISTORICAL_EXECUTION)}

    def runtime(self):
        m, c = self.m, self.controller
        require(m.directory(RUNTIME, 0, c.goby.pw_gid, 0o710) == c.runtime_identity, "The original 0710 runtime changed.")
        raw = m.read_file(RUNTIME / "owner.json", modes=(0o600,))
        require(digest(raw) == RUNTIME_OWNER_SHA and m.decode(raw) == {
            "marker": m.MARKER, "run_id": RUN, "intent_sha256": INTENT_SHA, "identity": c.runtime_identity},
            "The runtime owner marker changed.")
        inventory = tree(m, RUNTIME, runtime=True, gid=c.goby.pw_gid)
        expected_files = {"owner.json", "goby"} | {"web/" + name for name in self.inputs["web"]}
        require({name for name, row in inventory.items() if row["kind"] == "file"} == expected_files,
                "The failed runtime contains a generated, missing or unknown file.")
        require(inventory["goby"]["sha256"] == m.BINARY_SHA and inventory["goby"]["mode"] == 0o550,
                "The failed runtime executable changed.")
        require(all(inventory["web/" + name]["sha256"] == value for name, value in self.inputs["web"].items()),
                "The runtime web copy changed.")
        require(all(row["kind"] == "directory" for name, row in inventory.items() if name == "app" or name.startswith("app/")),
                "The app directory is no longer file-empty.")
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

    def empty_database(self):
        m, db = self.m, self.controller.db
        db.verify_owned()
        self.no_sessions()
        require(db.objects() == [], "The failed database is initialized or contains objects.")
        public = m.decode(db.query("SELECT jsonb_build_object('oid',oid::bigint,'owner',pg_get_userbyid(nspowner),"
            "'acl',nspacl,'comment',obj_description(oid,'pg_namespace')) FROM pg_namespace WHERE nspname='public';", administrative=True))
        casts = m.decode(db.query("SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY oid),'[]') FROM pg_cast c;", administrative=True))
        require(public == db.pair["public"] and casts == db.pair["casts"], "The original public namespace or cast inventory changed.")
        # PostgreSQL 17 template0 has no post-init objects in these local catalogs.
        count_sql = "SELECT " + "+".join("(SELECT count(*) FROM " + name + " WHERE oid>=16384)" for name in OBJECT_CATALOGS) + ";"
        require(db.query(count_sql, administrative=True) == b"0", "Post-init objects appeared in an empty database.")
        require(db.maintenance(f"SELECT count(*) FROM pg_shdepend WHERE dbid={DATABASE_OID} "
            f"AND refclassid='pg_authid'::regclass AND refobjid={ROLE_OID};") == b"0", "Local role dependencies appeared.")
        db.verify_owned()
        self.no_sessions()
        return {"objects": [], "public": public, "casts_sha256": digest(m.canonical(casts)), "post_init_objects": 0}

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
                worker.get("stages") == [], "This tool cannot dispose a completed or initialized UI scope.")
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
        empty = self.empty_database()
        self.role_before = c.db.rows()["role"]
        self.preservation()
        return {"marker": MARKER, "run_id": RUN, "status": "preflight_passed", "script_sha256": self.script_sha,
                "operator_sha256": OPERATOR_SHA, "intent_sha256": INTENT_SHA, "failed_report_sha256": REPORT_SHA,
                "role_oid": ROLE_OID, "database_oid": DATABASE_OID, "database_initialized": False,
                "browser_executed": False, "live_ui_acceptance": False, "empty_database": empty,
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
        self.empty_database()
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
        self.empty_database()
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
            ordinary_drop_only=True, connections_terminated=False, schema_initialized=False, journal_sha256=self.previous)
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
