#!/usr/bin/env python3
"""Dispose only the sealed root-selection failure from the fourth live UI attempt.

This wrapper reuses the exact TOOL04 snapshot, ordinary pair disposal and
runtime removal. The only writes it adds are durable, exclusive disposal
evidence in a new directory. A successful disposal is not UI acceptance.
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
RUN = "20260912_081819_2ede994eef07"
TOOL = WORK / "storage-binding-live-ui-tool-04"
OPERATOR = TOOL / "verify-storage-binding-live-ui.py"
OUTPUT = WORK / ("storage-binding-live-ui-" + RUN)
RUNTIME = Path("/opt/goby-binding-ui-runtime-" + RUN)
HISTORY_ROOT = WORK / "storage-binding-live-ui-execution-04"
EXECUTION = WORK / "storage-binding-live-ui-selection-disposal-01"
SNAPSHOT = HISTORY_ROOT / "after-full.json"
LINK = OUTPUT / "private/browser/node_modules"
LINKS = (LINK, WORK / "storage-binding-live-ui-20260912_080512_4d076f299a7a/private/browser/node_modules")
LINK_TARGET = "/opt/goby-test/inactive-dependencies-m5h/node_modules"
MARKER = "goby-storage-binding-live-ui-selection-disposal-v1"
OPERATOR_SHA = "3b2919956d89d9cb61617fdd106bc9794d347f5c259b641cae64d8e2f238b1a2"
INTENT_SHA = "98df8b1a9cf6a06aade003060c91493b4250e30bbf313b0fbcf305574b251f12"
SNAPSHOT_SHA = "3afac856c175a15a79b69dc04264bb95b48e7bb1444fb65dc85842d9c164ddbf"
CATALOG_SHA = "78c26be6733cc43406dcfa7d798ec5d40e9b4050bef89e021c5a3fdf7d0fc9f8"
OWNER_SHA = "b8a8e9ec26c7ad745a1f56b1529f456a7d9f5ce5421a816da540a7656efc3faf"
ROLE_OID, DATABASE_OID = 18928018, 18928019
INVOCATIONS = {"controller": "be5471dec2c3498b96a54bb4e6969cf0", "worker": "ecae35dab99f4c038644734a92000fd8"}
PINS = {
    OUTPUT / "report.json": "7c6c4f0b1f0581615fc303a78409019c3ef2b5134bf64245317ec41f8f0f8efa",
    OUTPUT / "private/worker-result.json": "d507187c2d7f5a010a2affdf97b417523eedf9699fdd25428bac42ca9dc20ce5",
    OUTPUT / "private/browser-result.json": "035f760987c55130541c65df20976a22082f357af1a7469ff20cbf7a6c1831b1",
    OUTPUT / "private/browser-session.json": "ad05c53cb185535ddbe033e23916e8b2f4e98a8137938b70d102bb94eb180854",
    OUTPUT / "private/application.log": "22a8d934a2af35d2bfc5e24cbac73c22db54aaffe166181a4e9aca23bc60e044",
    OUTPUT / "private/http-002.json": "1b7f30907e4487e3c9ee6b3df9a2a6f04886a2170bcb047a515dd25030955d10",
    OUTPUT / "private/http-003.json": "d6324940c931bc060a922cc4cc9cd9e8e86af13ad233c2021987a940f5859f2f",
    OUTPUT / "private/worker-admitted.json": "3b08f39d0caadf507b35af91a92101492ebb59b88275ada4bd78759ca2a3c26a",
    OUTPUT / "receipt-009.json": "974b51297e66adda271e3232c0833f6e196f698be7563e0c488f13b223a82034",
    OUTPUT / "catalog-before.json": CATALOG_SHA,
    HISTORY_ROOT / "failed-terminal.json": "9f7aa2dc98df98e74fb7d761231a49e79dd7dd19c3106363e0528ba96033ae36",
    SNAPSHOT: SNAPSHOT_SHA,
    WORK / "storage-binding-live-ui-startup-disposal-01/report.json": "a5c248135d8c7d4abe4a6727de523431a9a45e917cad1f83f90754b7ba007b24",
    WORK / "storage-binding-live-ui-readiness-disposal-01/report.json": "8d62871ab5a69ba686a717b410650c3dcda358142140759b1595c078c8f0fa1a",
    WORK / "storage-binding-live-ui-baseline-disposal-01/report.json": "a936fdf304192ac22102820140df4e7a87474b18364e8d1d90d8b3b1fbee823b",
}
OLD_ROOTS = (OUTPUT, HISTORY_ROOT, *(WORK / name for name in (
    "storage-binding-live-ui-20260912_074733_ca8294cb9f47", "storage-binding-live-ui-execution-01",
    "storage-binding-live-ui-startup-disposal-01", "storage-binding-live-ui-20260912_075346_bf9011cca3e0",
    "storage-binding-live-ui-execution-02", "storage-binding-live-ui-readiness-disposal-01",
    "storage-binding-live-ui-20260912_080512_4d076f299a7a", "storage-binding-live-ui-execution-03",
    "storage-binding-live-ui-baseline-disposal-01")))


def require(value, message):
    if not value:
        raise RuntimeError(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def stamp(info):
    return (info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode,
            info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def source(path, expected):
    require(path.is_absolute() and str(path) == os.path.normpath(str(path)), "Noncanonical source path.")
    for part in reversed((path, *path.parents)):
        info = part.lstat()
        require(not stat.S_ISLNK(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                "Source is not exclusively root controlled.")
    before = path.lstat()
    require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_size <= 4 << 20, "Invalid source file.")
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
        require(stamp(os.fstat(stream.fileno())) == stamp(before), "Source changed before read.")
        raw = stream.read((4 << 20) + 1)
        require(stamp(os.fstat(stream.fileno())) == stamp(before), "Source changed during read.")
    require(stamp(path.lstat()) == stamp(before) and len(raw) == before.st_size and sha(raw) == expected, "Source digest changed.")
    return raw


def sync(directory):
    descriptor = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def oid(row):
    value = row["oid"]
    require(type(value) is str and re.fullmatch(r"[1-9][0-9]{0,9}", value) and int(value) <= 4294967295, "Invalid global OID text.")
    return int(value)


def tree(m, root):
    result, pending, total = {}, [root], 0
    while pending:
        path = pending.pop()
        before = path.lstat()
        require(len(result) < 512, "Tree entry budget exceeded.")
        row = dict(m.identity(before), mtime_ns=before.st_mtime_ns, ctime_ns=before.st_ctime_ns)
        if stat.S_ISLNK(before.st_mode):
            require(path in LINKS and before.st_uid == before.st_gid == 0 and before.st_nlink == 1 and
                    os.readlink(path) == LINK_TARGET, "An unapproved symlink appeared.")
            row.update(kind="symlink", target=os.readlink(path), nlink=1)
        else:
            m.path_info(path)
            require(before.st_uid in ((0, 995) if root == RUNTIME else (0,)) and
                    before.st_gid in ((0, 986) if root == RUNTIME else (0,)) and not before.st_mode & 0o022,
                    "A retained tree owner or mode changed.")
            if stat.S_ISDIR(before.st_mode):
                row["kind"] = "directory"
                children = []
                with os.scandir(path) as entries:
                    for entry in entries:
                        require(len(result) + len(pending) + len(children) < 512, "Tree entry budget exceeded.")
                        children.append(path / entry.name)
                pending.extend(sorted(children, reverse=True))
            else:
                require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_size <= (128 << 20) - total,
                        "A retained tree contains an unsupported or oversized file.")
                raw = m.read_file(path, uid=before.st_uid, gid=before.st_gid, modes=(stat.S_IMODE(before.st_mode),), limit=before.st_size)
                total += len(raw)
                row.update(kind="file", bytes=len(raw), sha256=sha(raw), nlink=1)
        require(stamp(path.lstat()) == stamp(before), "A tree entry changed during observation.")
        result[str(path.relative_to(root))] = row
    return result


class Wrapper:
    def __init__(self, script, script_sha):
        self.script, self.script_sha = script, script_sha
        self.m = types.ModuleType("selection_disposal_tool04")
        self.m.__file__ = str(OPERATOR)
        exec(compile(source(OPERATOR, OPERATOR_SHA), str(OPERATOR), "exec"), self.m.__dict__)
        m = self.m
        require(m.WORK == WORK and m.TOOL == TOOL and m.PORT == 18288, "Unexpected TOOL04 scope.")
        self.intent = m.load_intent(TOOL / "intent.json", INTENT_SHA)
        require(self.intent["run_id"] == RUN and self.intent["output"] == str(OUTPUT) and self.intent["runtime"] == str(RUNTIME), "Unexpected run.")
        self.inputs = m.verify_inputs(self.intent)
        self.c = m.Controller(self.intent, INTENT_SHA, self.inputs)
        self.c.module = m.load_workspace(self.inputs["selected"])
        self.c.postgres, self.c.goby = pwd.getpwnam("postgres"), pwd.getpwnam("goby")
        require((self.c.goby.pw_uid, self.c.goby.pw_gid) == (995, 986), "Service account changed.")
        self.lock, self.started, self.last = None, False, None
        self.records, self.phases = {}, []

    def read(self, path):
        return self.m.decode(self.m.read_file(path, modes=(0o600,), limit=8 << 20), 8 << 20)

    def units(self):
        for kind, invocation in INVOCATIONS.items():
            unit = self.intent[kind + "_unit"]
            state = self.m.unit_state(unit)
            self.m.unit_owner(state, unit, self.c.tag)
            expected = {"LoadState": "loaded", "ActiveState": "failed", "SubState": "failed", "MainPID": "0",
                        "InvocationID": invocation, "Result": "exit-code", "ExecMainStatus": "1"}
            require(all(state.get(key) == value for key, value in expected.items()), "A sealed failed unit changed.")
            self.m.cgroup_empty(unit)

    def quiescent(self):
        db = self.c.db
        require(db.maintenance(f"SELECT (SELECT count(*) FROM pg_stat_activity WHERE datid={DATABASE_OID} OR usesysid={ROLE_OID})+"
            f"(SELECT count(*) FROM pg_prepared_xacts WHERE database='{db.name}' OR owner='{db.role}')+"
            f"(SELECT count(*) FROM pg_replication_slots WHERE database='{db.name}');") == b"0", "The pair has live or prepared work.")

    def catalog(self):
        current = self.m.decode(self.c.db.maintenance(self.m.GLOBAL_SQL))
        expected, phase = copy.deepcopy(self.before_catalog), self.c.db.pair["phase"]
        for key in ("roles", "databases"):
            values = [oid(row) for row in current[key]]
            require(values == sorted(values) and len(set(values)) == len(values) and
                    len({row["name"] for row in current[key]}) == len(values), "Ambiguous global catalog identities.")
        if phase in ("owned", "closed", "database_removed"):
            expected["roles"] = sorted(expected["roles"] + [self.role], key=oid)
        if phase in ("owned", "closed"):
            expected["databases"] = sorted(expected["databases"] + [dict(self.database, connect=phase == "owned")], key=oid)
        require(current == expected, "Global catalog drift exceeds this owned pair transition.")
        return current

    def preserve(self, runtime=True):
        m, c = self.m, self.c
        source(self.script, self.script_sha)
        source(OPERATOR, OPERATOR_SHA)
        c.check_cluster(self.hba)
        self.units()
        m.listener()
        require(m.host_fact() == self.failed["host_before"] and m.protected_facts() == self.failed["protected_before"], "Host/protected state changed.")
        require(sha(m.read_file(m.HISTORY, modes=(0o600,))) == self.intent["history_sha256"], "Shared historical receipt changed.")
        require({str(root): tree(m, root) for root in OLD_ROOTS} == self.old_trees, "Historical evidence trees changed.")
        if runtime:
            require(tree(m, RUNTIME) == self.runtime_tree, "The retained runtime changed.")
        else:
            require(not m.present(RUNTIME), "The runtime remains after removal.")
        for path, expected in self.records.items():
            require(sha(m.read_file(path, modes=(0o600,), limit=8 << 20)) == expected, "Disposal evidence changed.")

    def snapshot(self):
        db, m = self.c.db, self.m
        db.verify_owned()
        self.quiescent()
        actual = db.snapshot()
        require(actual == self.sealed and sha(m.canonical(actual)) == SNAPSHOT_SHA, "The complete sealed fixture state changed.")
        public = m.decode(db.query("SELECT jsonb_build_object('oid',oid::bigint,'owner',pg_get_userbyid(nspowner),"
            "'acl',nspacl,'comment',obj_description(oid,'pg_namespace')) FROM pg_namespace WHERE nspname='public';", administrative=True))
        casts = m.decode(db.query("SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY oid),'[]') FROM pg_cast c;", administrative=True))
        require(public == db.pair["public"] and casts == db.pair["casts"], "Public namespace or casts changed.")
        db.verify_library_defaults(self.library_id)
        self.quiescent()

    def preflight(self):
        m, c = self.m, self.c
        require(not m.present(EXECUTION), "This one-shot disposal namespace is consumed.")
        c.module.verify_parents(c.postgres)
        self.lock = c.module.acquire_lock()
        owner_raw = m.read_file(c.module.OWNER, modes=(0o600,))
        c.owner, c.owner_sha = m.decode(owner_raw), sha(owner_raw)
        c.module.validate_owner(c.owner, c.module.expected_owner(c.postgres, c.module.binaries()), c.module.directory(m.CONTROL, 0, 0))
        c.module.verify_directories(c.postgres, c.owner)
        c.module.verify_configuration(c.postgres)
        self.hba = c.module.HBA.encode()
        c.check_cluster(self.hba)
        for path, expected in PINS.items():
            require(sha(m.read_file(path, modes=(0o600,), limit=8 << 20)) == expected, "Pinned scope evidence changed.")
        self.failed, worker = self.read(OUTPUT / "report.json"), self.read(OUTPUT / "private/worker-result.json")
        browser, receipt = self.read(OUTPUT / "private/browser-result.json"), self.read(OUTPUT / "receipt-009.json")
        for document in (self.failed, worker, receipt):
            require(document["marker"] == m.MARKER and document["run_id"] == RUN and document["intent_sha256"] == INTENT_SHA, "Wrong failure scope.")
        require(self.failed["status"] == worker["status"] == browser["status"] == "failed" and self.failed.get("pair_retained") is True and
                self.failed.get("workflow_verified") is not True and worker["stages"] == ["browser_ready"] and
                browser["failure"] == "live_ui_assertion_failed" and len(browser["stages"]) == 1 and
                browser["stages"][0]["action"] == "browser_ready" and not any(row["method"] == "PUT" for row in browser["requests"]),
                "The sealed failure is outside the authorized baseline boundary.")
        require(all(self.failed["cleanup"].get(key) is True for key in
            ("hba_restored", "historical_receipt_unchanged", "protected_unchanged", "host_unchanged", "port_closed", "inputs_unchanged")) and
                worker["cleanup"] == {"browser_terminal": False, "sessions_revoked": True,
                                      "backend_terminal": True, "mounts_removed": True} and
                worker.get("cleanup_errors") == {"browser_terminal": "An owned process did not exit successfully."} and
                browser.get("checks", {}).get("browser_context_closed") is True,
                "The failed-browser and completed resource cleanup evidence differs.")
        require(self.failed["controller"]["invocation_id"] == INVOCATIONS["controller"] and
                receipt["worker_invocation"] == INVOCATIONS["worker"] and receipt["sequence"] == 9 and
                self.failed["pair"] == receipt["pair"], "Failure receipt identity changed.")
        c.db.pair = copy.deepcopy(receipt["pair"])
        require(c.db.pair["phase"] == "owned" and c.db.pair["role_oid"] == ROLE_OID and c.db.pair["database_oid"] == DATABASE_OID, "Wrong owned pair.")
        c.db.password, c.db.journal = None, self.progress
        c.runtime_identity = self.failed["runtime_identity"]
        require(m.directory(OUTPUT) == receipt["output_identity"] and m.directory(RUNTIME, 0, 986, 0o750) == c.runtime_identity, "Scope roots changed.")
        owner_raw = m.read_file(RUNTIME / "owner.json", modes=(0o600,))
        require(sha(owner_raw) == OWNER_SHA and m.decode(owner_raw) == {"marker": m.MARKER, "run_id": RUN,
            "intent_sha256": INTENT_SHA, "identity": c.runtime_identity}, "Runtime owner changed.")
        self.sealed = self.read(SNAPSHOT)
        rows = self.sealed["rows"]
        counts = {"users": 1, "sessions": 1, "libraries": 1, "items": 1, "library_roots": 3,
                  "activity_entries": 4, "scan_jobs": 0, "item_metadata_state": 1, "theme_owner_ids": 2}
        require(all(len(rows[name]) == count for name, count in counts.items()), "The synthetic fixture population changed.")
        user, session, library = rows["users"][0], rows["sessions"][0], rows["libraries"][0]
        self.library_id = library["id"]
        proof = self.read(OUTPUT / "private/browser-session.json")
        require(proof["marker"] == "goby-storage-binding-live-ui-session-v1" and proof["run_id"] == RUN and proof["nonce"] == self.intent["nonce"] and
                proof["validated"] is True and m.token_valid(proof["token"]) and proof["user_id"] == user["id"] == session["user_id"] and
                session["kind"] == "admin" and session["revoked_at"] is not None and session["token_hash"] == "\\x" + sha(proof["token"].encode()) and
                user["is_administrator"] is True and user["is_disabled"] is False, "The unique owned session is not proven revoked.")
        proof["token"] = proof["csrf_token"] = ""
        require(rows["items"][0]["id"] == self.library_id and rows["items"][0]["type"] == "CollectionFolder" and
                {root["path"] for root in rows["library_roots"]} == {str(RUNTIME / "app/media" / name) for name in ("A", "B", "U")} and
                all(root["library_id"] == self.library_id and root["binding_revision"] == 1 and
                    ((root["storage_binding"] is None and root["bound_by"] is None and root["bound_at"] is None)
                     if root["path"] == str(RUNTIME / "app/media/U") else
                     (root["storage_binding"] is not None and root["bound_by"] == user["id"] and root["bound_at"] is not None))
                    for root in rows["library_roots"]), "Registered fixture roots changed.")
        require(sorted(row["action"] for row in rows["activity_entries"]) == ["library.created", "session.login", "session.revoked", "user.created"], "Unexpected mutation audit.")
        for action, identifier in (("user.created", user["id"]), ("library.created", self.library_id),
                                   ("session.login", session["id"]), ("session.revoked", session["id"])):
            require(next(row for row in rows["activity_entries"] if row["action"] == action)["resource_id"] == identifier, "Audit resource changed.")
        self.before_catalog = self.read(OUTPUT / "catalog-before.json")
        current = m.decode(c.db.maintenance(m.GLOBAL_SQL))
        roles = [row for row in current["roles"] if oid(row) == ROLE_OID or row["name"] == c.db.role]
        databases = [row for row in current["databases"] if oid(row) == DATABASE_OID or row["name"] == c.db.name]
        require(len(roles) == len(databases) == 1 and oid(roles[0]) == ROLE_OID and roles[0]["name"] == c.db.role and
                oid(databases[0]) == DATABASE_OID and databases[0]["name"] == c.db.name, "Global pair identities are ambiguous.")
        self.role, self.database = roles[0], databases[0]
        self.catalog()
        self.snapshot()
        self.role_before = c.db.rows()["role"]
        require(all(link.is_symlink() and os.readlink(link) == LINK_TARGET for link in LINKS), "A recorded browser dependency link changed.")
        self.old_trees = {str(root): tree(m, root) for root in OLD_ROOTS}
        self.runtime_tree = tree(m, RUNTIME)
        require(self.runtime_tree["goby"]["sha256"] == m.BINARY_SHA and all(
            self.runtime_tree["web/" + name]["sha256"] == value for name, value in self.inputs["web"].items()), "Runtime product copies changed.")
        self.preserve()
        return {"marker": MARKER, "run_id": RUN, "status": "preflight_passed", "script_sha256": self.script_sha,
                "operator_sha256": OPERATOR_SHA, "snapshot_sha256": SNAPSHOT_SHA, "synthetic_rows": counts,
                "old_trees_sha256": sha(m.canonical(self.old_trees)), "runtime_tree_sha256": sha(m.canonical(self.runtime_tree)),
                "role_oid": ROLE_OID, "database_oid": DATABASE_OID, "live_ui_acceptance": False}

    def write(self, name, value):
        require(re.fullmatch(r"(?:intent|old-trees|runtime-tree|catalog-after|report|failure|journal-[1-3])\.json", name), "Unknown disposal evidence path.")
        require(self.m.directory(EXECUTION) == self.execution_identity, "Execution directory changed.")
        raw = self.m.canonical(value)
        require(len(raw) <= 8 << 20, "Evidence byte budget exceeded.")
        path = EXECUTION / name
        self.m.create_file(path, raw)
        sync(EXECUTION)
        self.records[path] = sha(raw)
        return self.records[path]

    def progress(self):
        phase = self.c.db.pair["phase"]
        require(self.started and len(self.phases) < 3 and phase == ("closed", "database_removed", "removed")[len(self.phases)], "Unexpected disposal phase.")
        self.preserve()
        self.quiescent()
        self.catalog()
        if phase == "closed":
            self.c.db.verify_owned(closed=True)
        elif phase == "database_removed":
            require(self.c.db.rows() == {"role": self.role_before, "database": None}, "Role changed after database removal.")
        else:
            require(self.c.db.rows() == {"role": None, "database": None}, "The pair remains after disposal.")
        self.phases.append(phase)
        self.last = self.write("journal-%d.json" % len(self.phases), {"marker": MARKER, "run_id": RUN,
            "phase": phase, "pair": copy.deepcopy(self.c.db.pair), "previous_sha256": self.last})

    def execute(self, preflight):
        self.preserve()
        self.snapshot()
        self.catalog()
        EXECUTION.mkdir(mode=0o700)
        self.started = True
        self.execution_identity = self.m.directory(EXECUTION)
        sync(WORK)
        self.last = self.write("intent.json", dict(preflight, action="dispose_exact_sealed_fixture_and_runtime"))
        self.write("old-trees.json", self.old_trees)
        self.write("runtime-tree.json", self.runtime_tree)
        self.preserve()
        self.c.db.dispose(self.sealed)
        require(self.phases == ["closed", "database_removed", "removed"], "Incomplete ordinary disposal.")
        self.preserve()
        self.c.remove_runtime()
        self.m.verify_inputs(self.intent)
        self.preserve(runtime=False)
        after = self.catalog()
        require(after == self.before_catalog and self.c.db.rows() == {"role": None, "database": None}, "Final catalog or absent pair changed.")
        self.write("catalog-after.json", after)
        report = dict(preflight, status="disposed", pair_absent=True, runtime_absent=True, old_trees_unchanged=True,
                      global_catalog_restored=True, ordinary_tool04_dispose_used=True, journal_sha256=self.last)
        return {"status": "disposed", "run_id": RUN, "report_sha256": self.write("report.json", report), "live_ui_acceptance": False}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check-only", action="store_true")
    parser.add_argument("--script-sha256", required=True)
    args, wrapper = parser.parse_args(argv), None
    try:
        require(sys.platform == "linux" and os.geteuid() == os.getegid() == 0 and os.environ.get("SSH_CONNECTION"), "Authorized test-env root SSH is required.")
        require(re.fullmatch(r"[0-9a-f]{64}", args.script_sha256), "An exact script SHA-256 is required.")
        script = Path(__file__).absolute()
        require(script.is_relative_to(WORK), "Unexpected disposal source location.")
        source(script, args.script_sha256)
        os.umask(0o077)
        def interrupted(_number, _frame):
            raise RuntimeError("Bounded disposal interrupted; a started namespace cannot be retried.")
        for number in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP, signal.SIGALRM):
            signal.signal(number, interrupted)
        signal.alarm(300)
        wrapper = Wrapper(script, args.script_sha256)
        result = wrapper.preflight()
        if not args.check_only:
            result = wrapper.execute(result)
        code = 0
    except Exception as error:
        reason = str(error) if type(error) is RuntimeError or (wrapper and isinstance(error, wrapper.m.Failure)) else type(error).__name__
        result = {"status": "failed", "run_id": RUN, "reason": reason, "execution_started": bool(wrapper and wrapper.started), "live_ui_acceptance": False}
        if wrapper and wrapper.started:
            try:
                wrapper.write("failure.json", dict(result, phases=wrapper.phases, last_journal_sha256=wrapper.last, retry_permitted=False))
            except Exception:
                pass
        code = 1
    finally:
        if hasattr(signal, "alarm"):
            signal.alarm(0)
        if wrapper and wrapper.lock is not None:
            fcntl.flock(wrapper.lock, fcntl.LOCK_UN)
            os.close(wrapper.lock)
    print(json.dumps(result, sort_keys=True))
    return code


if __name__ == "__main__":
    raise SystemExit(main())
