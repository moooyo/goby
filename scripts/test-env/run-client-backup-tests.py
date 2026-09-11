#!/usr/bin/env python3
"""Run backup verification or explicit catalog generation in the owned workspace.

The outer SSH operator owns the lock, temporary HBA rules, fixed disposable
pair, and bounded systemd unit. Failed runs retain their database evidence.
An unfinished receipt is never adopted or automatically restarted.
"""

from __future__ import annotations

import argparse
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import pwd
import re
import secrets
import signal
import stat
import subprocess
import sys
import time

sys.dont_write_bytecode = True
WORK = Path("/opt/goby-test/exec-work-m3e")
CONTROL = Path("/opt/goby-test/postgres-workspace-v1")
DATA = Path("/var/lib/postgresql/goby-workspace-v1/data")
SOCKET = DATA.parent / "socket"
HBA = DATA / "pg_hba.conf"
RECEIPT = CONTROL / "client-backup-pair.json"
PG = Path("/usr/lib/postgresql/17/bin")
GO = Path("/opt/goby-toolchains/go1.27.1/bin/go")
FFMPEG = Path("/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg")
FFPROBE = FFMPEG.with_name("ffprobe")
CACHES = Path("/opt/goby-test/go-caches-m5h")
NAMES = ("goby_backup_m3e_source", "goby_backup_m3e_target")
MARKER = "goby-client-backup-pair-m3e-v1"
SOURCE_MARKER = "goby-client-backup-source-m3e-v1"
MANIFEST = "backup-source-inputs.json"
CATALOG_DIRECTORY = "internal/backuppg/catalogs/"
MIGRATION_DIRECTORY = "internal/database/migrations/"
HISTORICAL_CATALOG_SHA256 = {
    23: "de85f4917dd7409e7e7bed20c7cbe63f0d7afe68f6ed72faff6b5bbb598ed00b",
    24: "6ba8a30d7648f3fdd73f977cc5d2aafac39232704542f28f20f0898d93ef575c",
    25: "e269a7eb6b31d2eb3fff734896ca074a6113761f321f2f23f4b07441e7dc617b",
    26: "e02c46a49dd4200bb67954f70ca0bbe90b3ef99d8821ea56a97e3dcb97e696de",
}
MIGRATION_24_NAME = "0024_user_settings.sql"
MIGRATION_25_NAME = "0025_music_artists.sql"
MIGRATION_26_NAME = "0026_theme_owners.sql"
MIGRATION_27_NAME = "0027_movie_extras.sql"
CATALOG_TABLE_COUNTS = {23: 29, 24: 30, 25: 30, 26: 33, 27: 35}
BOOTSTRAP_MIGRATIONS = {25: MIGRATION_25_NAME, 26: MIGRATION_26_NAME, 27: MIGRATION_27_NAME}
CATALOG_GENERATOR = "scripts/test-env/generate-backuppg-catalog.go"
CATALOG_OBJECT_FIELDS = {
    "column": {"name", "type", "not_null", "identity", "generated", "dropped", "storage", "compression", "collation", "default"},
    "constraint": {"type", "definition", "validated", "deferrable", "deferred", "no_inherit"},
    "function": {"kind", "language", "return_type", "arguments", "source", "binary", "security_definer", "leakproof", "strict",
                 "returns_set", "volatility", "parallel", "config", "cost", "rows", "support"},
    "index": {"table", "method", "unique", "primary", "exclusion", "immediate", "valid", "ready", "live", "nulls_not_distinct",
              "key_count", "options", "operator_classes", "collations", "columns", "predicate"},
    "relation": {"kind", "persistence", "partition", "replica_identity", "row_security", "force_row_security", "options"},
    "sequence": {"type", "start", "increment", "max", "min", "cache", "cycle", "owned_by"},
    "trigger": {"function", "enabled", "type", "arguments", "columns", "when", "old_table", "new_table"},
}
ENVIRONMENT = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8"}
PG_CASES = {"TestPostgreSQLBackupConsistentSnapshotAndRestore",
            "TestPostgreSQLRestoreSchema23ArchivePreservesDataAndMigratesToCurrent",
            "TestPostgreSQLRestoreFingerprintFailureLeavesEmptyTarget",
            "TestPostgreSQLRestoreRejectsPopulatedTarget", "TestPostgreSQLSnapshotRejectsSchemaDrift",
            "TestPostgreSQLSnapshotCancellationAndSettingsIsolation",
            "TestPostgreSQLOfflineRestoreWithUnavailableSource",
            "TestPostgreSQLOfflineSchema23RestoreFinalizerFailureRollsBackAndRetries",
            "TestPostgreSQLOfflineRestoreRejectsIdentityAndOverrides"}


class Failure(Exception):
    """A reviewed operator boundary could not be established."""


def require(value, message):
    if not value:
        raise Failure(message)


def sha(value):
    return hashlib.sha256(value).hexdigest()


def strict_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "A control document contains duplicate fields.")
        result[key] = value
    return result


def decode(value):
    return json.loads(value, object_pairs_hook=strict_object)


def present(path):
    try:
        path.lstat()
        return True
    except FileNotFoundError:
        return False


def canonical(path):
    require(path.is_absolute(), "An operator path is not absolute.")
    for part in reversed((path, *path.parents)):
        info = part.lstat()
        require(not stat.S_ISLNK(info.st_mode), "An operator path contains a symlink.")
    return path.lstat()


def directory(path, mode=0o700):
    info = canonical(path)
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_gid == 0 and
            stat.S_IMODE(info.st_mode) == mode, "An operator directory has an unexpected identity.")
    return {"device": info.st_dev, "inode": info.st_ino}


def private_read(path, *, uid=0, gid=0, limit=8 << 20, modes=(0o600,)):
    before = canonical(path)
    require(stat.S_ISREG(before.st_mode) and before.st_uid == uid and before.st_gid == gid and
            stat.S_IMODE(before.st_mode) in modes and before.st_nlink == 1 and before.st_size <= limit,
            "A private file has unexpected ownership, permissions, links, or size.")
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as handle:
        opened = os.fstat(handle.fileno())
        require((opened.st_dev, opened.st_ino) == (before.st_dev, before.st_ino),
                "A private file was replaced while opening it.")
        value = handle.read(limit + 1)
    require(len(value) <= limit, "A private file exceeds its size limit.")
    return value


def create_private(path, value, uid=0, gid=0):
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), "wb") as handle:
        os.fchown(handle.fileno(), uid, gid)
        handle.write(value)
        handle.flush()
        os.fsync(handle.fileno())


def sync_directory(path):
    descriptor = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def replace_private(path, expected, replacement, uid=0, gid=0):
    require(private_read(path, uid=uid, gid=gid) == expected, "A managed file changed outside this run.")
    temporary = path.with_name(path.name + ".next-" + secrets.token_hex(12))
    create_private(temporary, replacement, uid, gid)
    require(private_read(path, uid=uid, gid=gid) == expected, "A managed file changed before replacement.")
    os.replace(temporary, path)
    sync_directory(path.parent)


def command(arguments, *, input_text=None, timeout=30, environment=None):
    try:
        result = subprocess.run([str(arg) for arg in arguments], input=input_text, text=True,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout,
                                check=False, env=environment or ENVIRONMENT)
    except (OSError, subprocess.TimeoutExpired):
        raise Failure("A controlled command did not complete; private evidence was retained.") from None
    require(result.returncode == 0, "A controlled command failed; private evidence was retained.")
    return result.stdout.strip()


def pg(sql, database="postgres"):
    require(database == "postgres" or database in NAMES, "An administrative query selected an unrelated database.")
    return command(["/usr/sbin/runuser", "--user", "postgres", "--", PG / "psql", "-X", "--no-password",
                    "-h", SOCKET, "-p", "15432", "-U", "postgres", "-d", database,
                    "-v", "ON_ERROR_STOP=1", "-A", "-t", "-q"], input_text=sql + "\n", timeout=25)


def validate_arguments(args):
    require(args.source.is_absolute() and args.source.parent == WORK and
            re.fullmatch(r"source-attempt-(?:0[1-9]|[1-9][0-9]+)", args.source.name),
            "The source must be a directly contained source-attempt-NN snapshot with a canonical positive number.")
    require(re.fullmatch(r"[0-9a-f]{64}", args.manifest_sha256), "The manifest digest is invalid.")
    require(type(args.schema) is int and args.schema in (24, 25, 26, 27), "The verification schema must be explicitly supported.")
    require(args.mode in ("full", "targeted", "catalog"), "The verification mode is invalid.")
    require(not args.run or (args.mode == "targeted" and len(args.run) <= 512 and
            all(32 <= ord(character) < 127 for character in args.run)), "The targeted test expression is invalid.")
    require(args.mode == "targeted" or not args.package, "Full verification and catalog generation cannot narrow their package list.")
    require(args.mode != "catalog" or args.schema in BOOTSTRAP_MIGRATIONS, "Catalog generation requires an explicit supported schema.")
    for package in args.package:
        require(re.fullmatch(r"\./internal/[a-z][a-z0-9_]*", package), "A targeted package is outside the source module.")


def catalog_name(version):
    require(type(version) is int and version in CATALOG_TABLE_COUNTS, "The catalog version is outside the reviewed schemas.")
    return CATALOG_DIRECTORY + f"schema-{version}-postgresql-17.json"


def exact_fields(value, fields, message):
    require(isinstance(value, dict) and set(value) == set(fields), message)


def identifier(value):
    return isinstance(value, str) and re.fullmatch(r"[a-z][a-z0-9_]{0,62}", value)


def identifiers(values, *, allow_empty=False):
    return (isinstance(values, list) and (allow_empty or bool(values)) and
            all(identifier(value) for value in values) and len(values) == len(set(values)))


def validate_catalog(baseline, version):
    require(type(version) is int and version in CATALOG_TABLE_COUNTS, "The catalog version is outside the reviewed schemas.")
    exact_fields(baseline, {"version", "postgresql_major", "migrations", "catalog", "objects"},
                 "A trusted catalog contains missing or unowned fields.")
    require(type(baseline["version"]) is int and baseline["version"] == version and
            type(baseline["postgresql_major"]) is int and baseline["postgresql_major"] == 17,
            "A trusted catalog has an unexpected schema or PostgreSQL identity.")
    history = baseline["migrations"]
    require(isinstance(history, list) and len(history) == version, "The trusted migration history is incomplete.")
    for number, migration in enumerate(history, 1):
        exact_fields(migration, {"version", "name", "sha256"}, "A migration record contains missing or unowned fields.")
        require(type(migration["version"]) is int and migration["version"] == number and
                isinstance(migration["name"], str) and re.fullmatch(f"{number:04d}_[a-z][a-z0-9_]*\\.sql", migration["name"]) and
                isinstance(migration["sha256"], str) and re.fullmatch(r"[0-9a-f]{64}", migration["sha256"]),
                "A trusted migration record is malformed or out of order.")
    if version >= 24:
        require(history[23]["name"] == MIGRATION_24_NAME, "The historical preference migration name changed.")
    if version >= 25:
        require(history[24]["name"] == MIGRATION_25_NAME, "The schema25 migration name differs from the reviewed input.")
    if version >= 26:
        require(history[25]["name"] == MIGRATION_26_NAME, "The schema26 migration name differs from the reviewed input.")
    if version >= 27:
        require(history[26]["name"] == MIGRATION_27_NAME, "The schema27 migration name differs from the reviewed input.")
    catalog = baseline["catalog"]
    exact_fields(catalog, {"Schema", "Tables", "Sequences", "SHA256", "Constraints"},
                 "A catalog descriptor contains missing or unowned fields.")
    require(catalog["Schema"] == "" and isinstance(catalog["Tables"], list) and
            len(catalog["Tables"]) == CATALOG_TABLE_COUNTS[version] and
            isinstance(catalog["SHA256"], str) and re.fullmatch(r"[0-9a-f]{64}", catalog["SHA256"]),
            "The trusted table catalog has an unexpected identity.")
    table_names = []
    for table in catalog["Tables"]:
        exact_fields(table, {"Name", "Columns", "PrimaryKey", "SortKey"}, "A table descriptor contains missing or unowned fields.")
        require(identifier(table["Name"]) and all(identifiers(table[field]) for field in ("Columns", "SortKey")) and
                identifiers(table["PrimaryKey"], allow_empty=True) and
                set(table["PrimaryKey"]) <= set(table["Columns"]) and set(table["SortKey"]) <= set(table["Columns"]),
                "A trusted table descriptor is malformed.")
        table_names.append(table["Name"])
    require(table_names == sorted(set(table_names)), "Trusted table descriptors are duplicated or out of order.")
    require(isinstance(catalog["Sequences"], list) and isinstance(catalog["Constraints"], list),
            "The trusted sequence or constraint catalog is malformed.")
    for sequence in catalog["Sequences"]:
        exact_fields(sequence, {"Name", "Table", "Column", "MinValue", "MaxValue", "Increment", "Consumers"},
                     "A sequence descriptor contains missing or unowned fields.")
        require(all(identifier(sequence[field]) for field in ("Name", "Table", "Column")) and
                sequence["Table"] in table_names and all(type(sequence[field]) is int for field in ("MinValue", "MaxValue", "Increment")) and
                isinstance(sequence["Consumers"], list) and sequence["Consumers"], "A trusted sequence descriptor is malformed.")
        for consumer in sequence["Consumers"]:
            exact_fields(consumer, {"Table", "Column"}, "A sequence consumer contains missing or unowned fields.")
            require(consumer["Table"] in table_names and identifier(consumer["Column"]), "A trusted sequence consumer is malformed.")
    for constraint in catalog["Constraints"]:
        exact_fields(constraint, {"Table", "Name", "Definition"}, "A constraint descriptor contains missing or unowned fields.")
        require(constraint["Table"] in table_names and identifier(constraint["Name"]) and
                isinstance(constraint["Definition"], str) and constraint["Definition"], "A trusted constraint descriptor is malformed.")
    objects = baseline["objects"]
    require(isinstance(objects, list) and objects, "The trusted object inventory is empty or malformed.")
    object_names = []
    for item in objects:
        exact_fields(item, {"kind", "name", "value"}, "A catalog object contains missing or unowned fields.")
        require(isinstance(item["kind"], str) and item["kind"] in CATALOG_OBJECT_FIELDS and
                isinstance(item["name"], str) and item["name"], "A trusted catalog object has an unknown identity.")
        exact_fields(item["value"], CATALOG_OBJECT_FIELDS[item["kind"]], "A catalog object value contains missing or unowned fields.")
        object_names.append((item["kind"], item["name"]))
    require(object_names == sorted(set(object_names)), "Trusted catalog objects are duplicated or out of order.")
    normalized = json.dumps(objects, sort_keys=True, ensure_ascii=False, separators=(",", ":"), allow_nan=False).encode()
    require(sha(normalized) == catalog["SHA256"], "The trusted catalog object fingerprint differs.")


def read_source_catalog(source, files, schema, *, catalog_bootstrap=False):
    """Verify exact source catalogs and migrations; bootstrap has no current catalog."""
    require(type(schema) is int and schema in (24, 25, 26, 27), "The source schema must be explicitly supported.")
    require(type(catalog_bootstrap) is bool and (not catalog_bootstrap or schema in BOOTSTRAP_MIGRATIONS),
            "Catalog bootstrap is only permitted for an explicit supported schema.")
    versions = range(23, schema if catalog_bootstrap else schema + 1)
    require({name for name in files if name.startswith(CATALOG_DIRECTORY)} == {catalog_name(version) for version in versions},
            "The source catalog membership differs from the explicit verification schema.")
    baselines = {}
    for version in versions:
        name = catalog_name(version)
        expected = HISTORICAL_CATALOG_SHA256.get(version, files[name])
        require(isinstance(expected, str) and re.fullmatch(r"[0-9a-f]{64}", expected) and files[name] == expected,
                "The source manifest does not attest an immutable historical catalog.")
        raw = private_read(source / name, modes=(0o600, 0o644))
        require(sha(raw) == expected, "A catalog differs from its trusted source manifest digest.")
        baseline = decode(raw)
        validate_catalog(baseline, version)
        if version > 23:
            require(baseline["migrations"][:-1] == baselines[version - 1]["migrations"],
                    "A source catalog rewrites immutable migration history.")
        baselines[version] = baseline
    baseline = baselines.get(schema)
    history = baselines[schema - 1]["migrations"] if catalog_bootstrap else baseline["migrations"]
    migrations = {MIGRATION_DIRECTORY + row["name"]: row["sha256"] for row in history}
    if catalog_bootstrap:
        name = MIGRATION_DIRECTORY + BOOTSTRAP_MIGRATIONS[schema]
        expected = files.get(name)
        require(isinstance(expected, str) and re.fullmatch(r"[0-9a-f]{64}", expected),
                "The catalog bootstrap manifest omits the reviewed current migration digest.")
        migrations[name] = expected
    require({name: digest for name, digest in files.items() if name.startswith(MIGRATION_DIRECTORY)} == migrations,
            "The source migration membership or hashes differ from the explicit verification schema.")
    for name, expected in migrations.items():
        require(sha(private_read(source / name, modes=(0o600, 0o644))) == expected,
                "A migration differs from its trusted source catalog digest.")
    return baseline


def verify_source(source, expected_digest, schema=24, *, catalog_bootstrap=False):
    require(type(schema) is int and schema in (24, 25, 26, 27), "The source schema must be explicitly supported.")
    require(type(catalog_bootstrap) is bool and (not catalog_bootstrap or schema in BOOTSTRAP_MIGRATIONS),
            "Catalog bootstrap is only permitted for an explicit supported schema.")
    identity = directory(source)
    # The nonsecret hash inventory may retain ordinary source-file mode. Its
    # root-owned 0700 parent and the caller-supplied digest still bind its bytes.
    raw = private_read(source / MANIFEST, modes=(0o600, 0o644))
    require(sha(raw) == expected_digest, "The source manifest digest differs.")
    manifest = decode(raw)
    require(isinstance(manifest, dict) and set(manifest) == {"marker", "files"} and
            manifest["marker"] == SOURCE_MARKER and isinstance(manifest["files"], dict),
            "The source manifest does not have the reviewed format.")
    actual = {}
    for path in source.rglob("*"):
        info = canonical(path)
        require(info.st_uid == 0 and info.st_gid == 0 and not info.st_mode & 0o022,
                "A source entry permits untrusted writes.")
        if stat.S_ISDIR(info.st_mode):
            continue
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1,
                "The source contains a special file or hard link.")
        relative = str(path.relative_to(source))
        if relative != MANIFEST:
            actual[relative] = sha(path.read_bytes())
    require(actual == manifest["files"] and len(actual) > 100, "Source bytes or membership differ from the manifest.")
    required = {"go.mod", "go.sum", "scripts/test-env/prepare-postgres-workspace.py",
                "scripts/test-env/run-backup-full-tests.sh", "internal/backuppg/catalog.go",
                catalog_name(23), catalog_name(24), MIGRATION_DIRECTORY + MIGRATION_24_NAME}
    if schema >= 25:
        required.add(MIGRATION_DIRECTORY + MIGRATION_25_NAME)
    if schema >= 26:
        required.add(MIGRATION_DIRECTORY + MIGRATION_26_NAME)
        required.add(catalog_name(25))
    if schema >= 27:
        required.add(MIGRATION_DIRECTORY + MIGRATION_27_NAME)
        required.add(catalog_name(26))
    if schema >= 25:
        required.add(CATALOG_GENERATOR if catalog_bootstrap else catalog_name(schema))
    require(required <= actual.keys(), "The source omits a required current or historical backup input.")
    read_source_catalog(source, actual, schema, catalog_bootstrap=catalog_bootstrap)
    return identity, actual


def generated_catalog(raw, source_files, schema=25):
    require(type(schema) is int and schema in BOOTSTRAP_MIGRATIONS, "The generated catalog schema is outside the reviewed schemas.")
    baseline = decode(raw)
    validate_catalog(baseline, schema)
    migrations = {MIGRATION_DIRECTORY + row["name"]: row["sha256"] for row in baseline["migrations"]}
    require(migrations == {name: digest for name, digest in source_files.items() if name.startswith(MIGRATION_DIRECTORY)},
            "The generated catalog migration history differs from the frozen bootstrap manifest.")
    return baseline


def load_workspace(source):
    path = source / "scripts/test-env/prepare-postgres-workspace.py"
    spec = importlib.util.spec_from_file_location("client_backup_workspace", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    require(module.CONTROL == CONTROL and module.DATA == DATA and module.SOCKET == SOCKET and
            module.PORT == 15432 and module.MARKER == "goby-postgres-persistent-workspace-v1",
            "The workspace helper addresses a different cluster.")
    return module


def temporary_hba(original, tag, baseline):
    require(original == baseline and original.endswith(b"\n"), "The HBA differs from the exact persistent baseline.")
    require(re.fullmatch(r"goby-client-backup-pair-m3e-v1:[0-9]{8}_[0-9]{6}_[0-9a-f]{12}", tag),
            "The temporary HBA owner tag is invalid.")
    additions = "".join(f"# {tag}\nhost {name} {name} 127.0.0.1/32 scram-sha-256\n" for name in NAMES)
    return additions.encode() + original


def verify_tools():
    versions = {}
    for path, arguments, prefix in ((GO, ["version"], "go version go1.27.1 linux/amd64"),
                                    (FFMPEG, ["-version"], "ffmpeg version 9.0.1 "),
                                    (FFPROBE, ["-version"], "ffprobe version 9.0.1 "),
                                    (PG / "pg_dump", ["--version"], "pg_dump (PostgreSQL) 17.11"),
                                    (PG / "pg_restore", ["--version"], "pg_restore (PostgreSQL) 17.11")):
        info = canonical(path)
        require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_gid == 0 and
                stat.S_IMODE(info.st_mode) == 0o755 and info.st_nlink == 1, "A pinned executable changed identity.")
        for parent in path.parents:
            metadata = canonical(parent)
            require(metadata.st_uid == 0 and stat.S_ISDIR(metadata.st_mode) and not metadata.st_mode & 0o022,
                    "A pinned executable parent allows untrusted writes.")
        version = command([path, *arguments]).splitlines()[0]
        require(version.startswith(prefix), "A verification executable differs from its pinned version.")
        versions[str(path)] = {"version": version, "sha256": sha(path.read_bytes())}
    directory(CACHES)
    require(decode(private_read(CACHES / "OWNER.json")) ==
            {"owner": "goby-m5h-go-cache-relocation-v1", "root": str(CACHES), "schema": 1},
            "The persistent Go cache owner differs.")
    directory(CACHES / "build", 0o755)
    directory(CACHES / "modules", 0o755)
    return versions


def unit_state(unit):
    require(re.fullmatch(r"goby-client-backup-[0-9]{8}-[0-9]{6}-[0-9a-f]{12}\.service", unit),
            "The unit name is outside this operator's namespace.")
    raw = command(["/usr/bin/systemctl", "show", unit,
                   "--property=LoadState,ActiveState,SubState,Description,MainPID,ControlGroup,MemoryMax,MemorySwapMax,CPUQuotaPerSecUSec,KillMode,ExecMainStatus,Result"])
    return dict(line.split("=", 1) for line in raw.splitlines() if "=" in line)


def require_unit_owner(state, unit, tag):
    require(state.get("Description") == tag and state.get("ControlGroup") in ("", "/system.slice/" + unit),
            "The unit is not the exact owned cgroup; unrelated work will not be stopped.")


def require_unit_terminal(state, unit, tag):
    if state.get("LoadState") != "not-found":
        require_unit_owner(state, unit, tag)
        require(state.get("ActiveState") in ("inactive", "failed") and state.get("MainPID") == "0",
                "The recorded unit is still live; it must not be restarted or cleaned up.")
    cgroup = Path("/sys/fs/cgroup/system.slice") / unit / "cgroup.procs"
    require(not present(cgroup) or not cgroup.read_text().strip(), "The recorded cgroup still contains processes.")


def catalog_state():
    return decode(pg("SELECT jsonb_build_object('roles',(SELECT jsonb_agg(jsonb_build_object('oid',oid,'name',rolname,"
        "'login',rolcanlogin,'super',rolsuper,'createdb',rolcreatedb,'createrole',rolcreaterole,'inherit',rolinherit,"
        "'replication',rolreplication,'bypass',rolbypassrls,'limit',rolconnlimit,'config',rolconfig,"
        "'tag',shobj_description(oid,'pg_authid')) ORDER BY oid) FROM pg_roles),"
        "'databases',(SELECT jsonb_agg(jsonb_build_object('oid',oid,'name',datname,'owner',datdba,'acl',datacl,"
        "'connect',datallowconn,'limit',datconnlimit,'template',datistemplate,'tag',shobj_description(oid,'pg_database')) ORDER BY oid) FROM pg_database),"
        "'memberships',(SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY oid),'[]'::jsonb) FROM pg_auth_members m),"
        "'settings',(SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY setdatabase,setrole),'[]'::jsonb) FROM pg_db_role_setting s));"))


def pair_rows(name):
    require(name in NAMES, "A pair name is outside the fixed disposable identities.")
    return decode(pg("SELECT jsonb_build_object('role',(SELECT jsonb_build_object('oid',oid::bigint,"
        "'tag',shobj_description(oid,'pg_authid'),'login',rolcanlogin,'super',rolsuper,'createdb',rolcreatedb,"
        "'createrole',rolcreaterole,'replication',rolreplication,'bypass',rolbypassrls,'inherit',rolinherit,"
        "'limit',rolconnlimit,'config',rolconfig,'valid_until',rolvaliduntil,'scram',"
        "(SELECT a.rolpassword LIKE 'SCRAM-SHA-256$%' FROM pg_authid a WHERE a.oid=r.oid))"
        f" FROM pg_roles r WHERE rolname='{name}'),'database',(SELECT jsonb_build_object('oid',oid::bigint,"
        "'owner_oid',datdba::bigint,'tag',shobj_description(oid,'pg_database'),'allow',datallowconn,"
        "'template',datistemplate,'limit',datconnlimit,'encoding',pg_encoding_to_char(encoding),"
        "'acl',(SELECT jsonb_agg(jsonb_build_object('grantor',a.grantor::bigint,'grantee',a.grantee::bigint,"
        "'privilege_type',a.privilege_type,'is_grantable',a.is_grantable) ORDER BY grantee,privilege_type)"
        " FROM aclexplode(COALESCE(datacl,acldefault('d',datdba))) a))"
        f" FROM pg_database WHERE datname='{name}'));"))


def validate_pair_rows(pair, rows, tag):
    role = {"oid": pair["role_oid"], "tag": tag, "login": True, "super": False, "createdb": False,
            "createrole": False, "replication": False, "bypass": False, "inherit": False, "limit": 12,
            "config": None, "valid_until": None, "scram": True}
    require(rows.get("role") == role and type(pair["role_oid"]) is int and pair["role_oid"] > 0,
            "A disposable role OID, tag, privilege, or verifier changed.")
    database = rows.get("database")
    require(isinstance(database, dict) and database == pair["database_fingerprint"] and
            database["oid"] == pair["database_oid"] and database["owner_oid"] == pair["role_oid"] and
            database["tag"] == tag and database["allow"] is True and database["template"] is False and
            database["limit"] == -1 and database["encoding"] == "UTF8", "A disposable database identity or ACL changed.")
    acl = database["acl"]
    require(isinstance(acl, list) and len(acl) == 3 and
            {entry["privilege_type"] for entry in acl} == {"CREATE", "CONNECT", "TEMPORARY"} and
            all(entry["grantee"] == pair["role_oid"] and entry["grantor"] == pair["role_oid"] and
                entry["is_grantable"] is False for entry in acl), "The disposable database grants access to another principal.")


def validate_disposal(previous, receipt_bytes, disposal, hba_sha256):
    run = previous.get("run_id", "")
    require(re.fullmatch(r"[0-9]{8}_[0-9]{6}_[0-9a-f]{12}", run) and
            previous.get("output") == str(WORK / ("client-backup-run-" + run)), "The previous evidence path is invalid.")
    pairs = [{"name": pair["name"], "role_oid": pair["role_oid"], "database_oid": pair["database_oid"]}
             for pair in previous.get("pairs", [])]
    require(pairs and all(pair["name"] in NAMES and type(pair["role_oid"]) is int and pair["role_oid"] > 0 and
            type(pair["database_oid"]) is int and pair["database_oid"] > 0 for pair in pairs),
            "A prior pair lacks exact disposal identities.")
    require(isinstance(disposal, dict) and disposal.get("marker") == "goby-client-backup-disposal-m3e-v1" and
            disposal.get("status") == "disposed" and disposal.get("run_id") == run and disposal.get("tag") == previous["tag"] and
            disposal.get("source_receipt_sha256") == sha(receipt_bytes) and disposal.get("pairs") == pairs and
            disposal.get("system_identifier") == previous["cluster"]["system_identifier"] and
            disposal.get("hba_sha256") == hba_sha256 and
            disposal.get("report_path") == str(Path(previous["output"]) / "disposal-report.json") and
            re.fullmatch(r"[0-9a-f]{64}", disposal.get("report_sha256", "")),
            "A retained run lacks an exact independently reviewed disposal receipt.")


def require_role_isolated(pair):
    role, database = pair["role_oid"], pair["database_oid"]
    require(type(role) is int and type(database) is int and role > 0 and database > 0, "The pair has incomplete OID receipts.")
    count = pg(f"SELECT (SELECT count(*) FROM pg_auth_members WHERE member={role} OR roleid={role} OR grantor={role})+"
        f"(SELECT count(*) FROM pg_db_role_setting WHERE setrole={role} OR setdatabase={database})+"
        f"(SELECT count(*) FROM pg_shdepend WHERE refclassid='pg_authid'::regclass AND refobjid={role} AND NOT"
        f" (dbid={database} OR (dbid=0 AND classid='pg_database'::regclass AND objid={database} AND objsubid=0 AND deptype='o')))+"
        f"(SELECT count(*) FROM pg_shseclabel WHERE (classoid='pg_authid'::regclass AND objoid={role}) OR"
        f" (classoid='pg_database'::regclass AND objoid={database}));")
    require(count == "0", "A disposable identity has unknown memberships, settings, labels, or external dependencies.")


def unsupported_objects_sql(role):
    extra = [("pg_collation", "collnamespace"), ("pg_conversion", "connamespace"),
             ("pg_operator", "oprnamespace"), ("pg_opclass", "opcnamespace"), ("pg_opfamily", "opfnamespace"),
             ("pg_ts_config", "cfgnamespace"), ("pg_ts_dict", "dictnamespace"),
             ("pg_ts_parser", "prsnamespace"), ("pg_ts_template", "tmplnamespace")]
    queries = [f"SELECT 1 FROM {table} o JOIN pg_namespace n ON n.oid=o.{column} WHERE n.nspname='public'" for table, column in extra]
    queries += ["SELECT 1 FROM pg_namespace WHERE nspname NOT IN ('public','pg_catalog','pg_toast','information_schema')",
                "SELECT 1 FROM pg_extension WHERE extname<>'plpgsql'",
                "SELECT 1 FROM pg_language WHERE lanname NOT IN ('internal','c','sql','plpgsql')",
                "SELECT 1 FROM pg_inherits",
                "SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND"
                f" (c.relowner<>{role} OR c.relacl IS NOT NULL OR c.relkind NOT IN ('r','i','S'))",
                "SELECT 1 FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace"
                " WHERE n.nspname='public' AND a.attacl IS NOT NULL",
                "SELECT 1 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public' AND"
                f" (p.proowner<>{role} OR p.proacl IS NOT NULL)",
                "SELECT 1 FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname='public' AND"
                f" (t.typowner<>{role} OR t.typacl IS NOT NULL OR NOT ((t.typtype='c' AND t.typrelid IN"
                " (SELECT oid FROM pg_class WHERE relnamespace=n.oid AND relkind='r')) OR (t.typtype='b' AND t.typelem IN"
                " (SELECT oid FROM pg_type WHERE typnamespace=n.oid AND typtype='c' AND typrelid<>0))))",
                "SELECT 1 FROM pg_subscription WHERE subdbid=(SELECT oid FROM pg_database WHERE datname=current_database())"]
    queries += ["SELECT 1 FROM " + table for table in ("pg_event_trigger", "pg_foreign_server", "pg_foreign_data_wrapper",
                "pg_publication", "pg_largeobject_metadata", "pg_default_acl", "pg_seclabel", "pg_policy",
                "pg_statistic_ext", "pg_transform")]
    queries += ["SELECT 1 FROM pg_rewrite r JOIN pg_class c ON c.oid=r.ev_class JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'"]
    return "SELECT EXISTS(" + " UNION ALL ".join(queries) + ");"


class Runner:
    def __init__(self, args):
        self.args = args
        self.run = time.strftime("%Y%m%d_%H%M%S", time.gmtime()) + "_" + secrets.token_hex(6)
        self.tag = MARKER + ":" + self.run
        self.unit = "goby-client-backup-" + self.run.replace("_", "-") + ".service"
        self.output = WORK / ("client-backup-run-" + self.run)
        self.catalog_output = self.output / f"schema-{args.schema}-postgresql-17.json"
        self.catalog_artifact = None
        self.module = None
        self.lock = None
        self.cluster = None
        self.pairs = []
        self.hba_before = None
        self.hba_after = None
        self.hba_intent = False
        self.unit_intent = False
        self.unit_observed = False
        self.created_output = False
        self.receipt_bytes = None
        self.receipt = None
        self.before_catalog = None
        self.report = {"marker": MARKER, "run_id": self.run, "status": "running", "mode": args.mode,
                       "schema": args.schema, "source": str(args.source),
                       "source_manifest_sha256": args.manifest_sha256, "unit": self.unit, "cleanup": {}}

    def save(self):
        self.receipt["pairs"] = self.pairs
        encoded = json.dumps(self.receipt, sort_keys=True, indent=2).encode() + b"\n"
        if self.receipt_bytes is None:
            create_private(RECEIPT, encoded)
        else:
            replace_private(RECEIPT, self.receipt_bytes, encoded)
        self.receipt_bytes = encoded
        snapshot = self.output / ("receipt-" + secrets.token_hex(8) + ".json")
        create_private(snapshot, encoded)

    def check_cluster(self, hba=None):
        module, postgres = self.module, self.postgres
        raw = module.read_file(module.OWNER)
        require(sha(raw.encode()) == self.cluster_owner_sha, "The permanent cluster owner record changed.")
        owner = decode(raw)
        module.validate_owner(owner, module.expected_owner(postgres, self.binary_identity), module.directory(CONTROL, 0, 0))
        module.verify_directories(postgres, owner)
        require(module.cluster_identifier() == owner["system_identifier"], "The cluster system identifier changed.")
        require(owner["process"] is not None and module.verify_process(postgres, owner) == owner["process"],
                "The exact recorded persistent cluster process is not running.")
        module.verify_server(owner)
        options = {"uid": postgres.pw_uid, "gid": postgres.pw_gid}
        require(module.read_file(DATA / "postgresql.conf", **options) == module.CONF,
                "The persistent PostgreSQL configuration changed.")
        for name in ("postgresql.auto.conf", "pg_ident.conf"):
            require(all(not line.strip() or line.lstrip().startswith("#")
                        for line in module.read_file(DATA / name, **options).splitlines()),
                    "A PostgreSQL override or identity map changed during verification.")
        require(private_read(HBA, uid=postgres.pw_uid, gid=postgres.pw_gid) ==
                (hba if hba is not None else module.HBA.encode()), "The HBA changed outside the owned transition.")
        if self.cluster is not None:
            require(self.cluster == {"system_identifier": owner["system_identifier"], "process": owner["process"]},
                    "The persistent cluster was restarted during verification.")
        return owner

    def prepare(self):
        require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION"),
                "Run only through authorized root SSH on test-env.")
        validate_arguments(self.args)
        os.umask(0o077)
        directory(WORK)
        self.source_identity, self.source_files = verify_source(self.args.source, self.args.manifest_sha256, self.args.schema,
                                                               catalog_bootstrap=self.args.mode == "catalog")
        self.report["catalog_sha256"] = self.source_files.get(catalog_name(self.args.schema))
        self.module = load_workspace(self.args.source)
        self.postgres = pwd.getpwnam("postgres")
        self.module.verify_parents(self.postgres)
        directory(CONTROL)
        self.lock = self.module.acquire_lock()
        self.binary_identity = self.module.binaries()
        self.cluster_owner_sha = sha(self.module.read_file(self.module.OWNER).encode())
        self.module.verify_configuration(self.postgres)
        owner = self.check_cluster()
        self.cluster = {"system_identifier": owner["system_identifier"], "process": owner["process"]}
        if present(RECEIPT):
            self.receipt_bytes = private_read(RECEIPT)
            previous = decode(self.receipt_bytes)
            require(previous.get("marker") == MARKER and isinstance(previous.get("cluster"), dict) and
                    previous["cluster"].get("system_identifier") == self.cluster["system_identifier"],
                    "An unfinished pair receipt requires operator inspection; no run will be restarted.")
            if not (previous.get("phase") == "finished" and previous.get("cleanup_complete") is True):
                run = previous.get("run_id", "")
                require(previous.get("phase") == "retained" and re.fullmatch(r"[0-9]{8}_[0-9]{6}_[0-9a-f]{12}", run),
                        "An unfinished pair receipt requires operator inspection; no run will be restarted.")
                disposal = decode(private_read(CONTROL / ("client-backup-disposal-" + run + ".json")))
                validate_disposal(previous, self.receipt_bytes, disposal, sha(self.module.HBA.encode()))
                require(directory(Path(previous["output"])) == previous["output_identity"] and
                        sha(private_read(Path(disposal["report_path"]))) == disposal["report_sha256"],
                        "The independently reviewed disposal evidence changed.")
            require_unit_terminal(unit_state(previous["unit"]), previous["unit"], previous["tag"])
        for name in NAMES:
            require(pair_rows(name) == {"role": None, "database": None},
                    "A fixed database or role already exists; unknown state will not be adopted.")
        active = command(["/usr/bin/systemctl", "list-units", "--all", "--no-legend", "--plain", "goby-client-backup-*.service"])
        for line in active.splitlines():
            fields = line.split()
            require(len(fields) >= 4 and fields[2] not in ("active", "activating", "deactivating", "reloading"),
                    "Another backup verification unit may be live; it will not be duplicated.")
        require(unit_state(self.unit).get("LoadState") == "not-found", "The new unit name is already occupied.")
        self.report["tools"] = verify_tools()
        self.output.mkdir(mode=0o700)
        self.created_output = True
        self.output_identity = directory(self.output)
        self.receipt = {"marker": MARKER, "run_id": self.run, "tag": self.tag, "unit": self.unit,
                        "output": str(self.output), "output_identity": self.output_identity,
                        "source": str(self.args.source), "source_manifest_sha256": self.args.manifest_sha256,
                        "schema": self.args.schema, "mode": self.args.mode,
                        "catalog_sha256": self.source_files.get(catalog_name(self.args.schema)),
                        "cluster": self.cluster, "cluster_owner_sha256": self.cluster_owner_sha,
                        "phase": "prepared", "cleanup_complete": False, "pairs": []}
        self.save()
        self.before_catalog = catalog_state()
        create_private(self.output / "catalog-before.json", json.dumps(self.before_catalog, sort_keys=True).encode())
        self.hba_before = private_read(HBA, uid=self.postgres.pw_uid, gid=self.postgres.pw_gid)
        self.hba_after = temporary_hba(self.hba_before, self.tag, self.module.HBA.encode())
        create_private(self.output / "hba-original", self.hba_before)
        create_private(self.output / "hba-temporary", self.hba_after)
        self.report["cluster"] = self.cluster
        self.report["hba_before_sha256"] = sha(self.hba_before)
        passwords = {name: secrets.token_hex(32) for name in NAMES}
        create_private(self.output / "credentials.json", json.dumps(passwords, sort_keys=True).encode())
        for name in NAMES:
            self.create_pair(name, passwords[name])
        self.receipt["phase"] = "hba_pending"
        self.save()
        self.hba_intent = True
        self.check_cluster(self.hba_before)
        replace_private(HBA, self.hba_before, self.hba_after, self.postgres.pw_uid, self.postgres.pw_gid)
        self.reload_hba(self.hba_after)
        self.write_environment(passwords)
        self.receipt["phase"] = "ready"
        self.save()

    def create_pair(self, name, password):
        self.check_cluster(self.hba_before)
        require(pair_rows(name) == {"role": None, "database": None}, "A pair identity appeared before creation.")
        pair = {"name": name, "role_oid": None, "database_oid": None, "database_fingerprint": None,
                "phase": "role_pending"}
        self.pairs.append(pair)
        self.save()
        pg(f"BEGIN; SET LOCAL password_encryption='scram-sha-256'; CREATE ROLE {name} LOGIN NOINHERIT NOSUPERUSER"
           f" NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 12 PASSWORD '{password}';"
           f" COMMENT ON ROLE {name} IS '{self.tag}'; COMMIT;")
        row = pair_rows(name)
        require(row["role"] is not None and row["role"]["tag"] == self.tag and row["database"] is None,
                "Role creation did not produce an exact ownership receipt.")
        pair["role_oid"], pair["phase"] = row["role"]["oid"], "database_pending"
        self.save()
        pg(f"CREATE DATABASE {name} OWNER {name} TEMPLATE template0 ENCODING 'UTF8';")
        row = pair_rows(name)
        require(row["database"] is not None and row["database"]["owner_oid"] == pair["role_oid"] and
                row["database"]["tag"] is None, "Fresh database creation returned an unexpected identity.")
        pair["database_oid"] = row["database"]["oid"]
        self.save()
        pg(f"COMMENT ON DATABASE {name} IS '{self.tag}'; REVOKE CONNECT,TEMPORARY ON DATABASE {name} FROM PUBLIC;")
        pair["database_fingerprint"] = pair_rows(name)["database"]
        validate_pair_rows(pair, pair_rows(name), self.tag)
        require_role_isolated(pair)
        pair["public"] = self.public_identity(name)
        require(pair["public"]["owner"] == "pg_database_owner" and pair["public"]["comment"] == "standard public schema" and
                pair["public"]["acl"] == ["pg_database_owner=UC/pg_database_owner", "=U/pg_database_owner"],
                "The fresh public schema differs from its standard owner and ACL.")
        pair["casts"] = self.cast_inventory(name)
        require(self.inspect_objects(pair) == [], "A newly created disposable database is not empty.")
        pair["phase"] = "owned"
        self.save()

    def public_identity(self, name):
        return decode(pg("SELECT jsonb_build_object('oid',oid::bigint,'owner',pg_get_userbyid(nspowner),"
                         "'acl',nspacl,'comment',obj_description(oid,'pg_namespace')) FROM pg_namespace WHERE nspname='public';", name))

    def cast_inventory(self, name):
        return decode(pg("SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY oid),'[]'::jsonb) FROM pg_cast c;", name))

    def inspect_objects(self, pair):
        name, role = pair["name"], pair["role_oid"]
        require(pg(unsupported_objects_sql(role), name) == "f", "The disposable database contains unknown objects or grants.")
        source = (self.args.source / "internal/backuppg/catalog.go").read_text()
        matches = re.findall(r"(?m)^const catalogObjectsSQL = `([^`]+)`", source)
        require(len(matches) == 1 and matches[0].startswith("WITH namespace AS") and ";" not in matches[0],
                "The trusted catalog query cannot be extracted unambiguously.")
        query = matches[0].replace("$1", "'public'")
        return decode(pg("BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SET LOCAL search_path=pg_catalog,public; " + query + "; COMMIT;", name))

    def reload_hba(self, expected):
        self.check_cluster(expected)
        require(pg("SELECT pg_reload_conf();") == "t" and pg("SELECT count(*) FROM pg_hba_file_rules WHERE error IS NOT NULL;") == "0",
                "The exact HBA reload did not succeed.")
        require(private_read(HBA, uid=self.postgres.pw_uid, gid=self.postgres.pw_gid) == expected,
                "The HBA changed after reload.")

    def write_environment(self, passwords):
        temporary = self.output / "tmp"
        temporary.mkdir(mode=0o700)
        environment = dict(ENVIRONMENT, HOME="/root", GOCACHE=str(CACHES / "build"), GOMODCACHE=str(CACHES / "modules"),
            GOTOOLCHAIN="local", GOWORK="off", GOFLAGS="-mod=readonly", GOMEMLIMIT="384MiB", GOMAXPROCS="2",
            GOTMPDIR=str(temporary), TMPDIR=str(temporary), GOBY_FFMPEG=str(FFMPEG), GOBY_FFPROBE=str(FFPROBE),
            GOBY_TEST_BACKUP_DISPOSABLE_DATABASES="1", GOBY_TEST_PG_DUMP=str(PG / "pg_dump"),
            GOBY_TEST_PG_RESTORE=str(PG / "pg_restore"), GOBY_BACKUP_PG_RUN_ID=self.run)
        for side, name in zip(("SOURCE", "TARGET"), NAMES):
            environment["GOBY_TEST_BACKUP_" + side + "_DATABASE_URL"] = f"postgresql://{name}:{passwords[name]}@127.0.0.1:15432/{name}?sslmode=disable"
            login_env = dict(ENVIRONMENT, PGPASSWORD=passwords[name], PGCONNECT_TIMEOUT="5")
            actual = command([PG / "psql", "-X", "--no-password", "-h", "127.0.0.1", "-p", "15432", "-U", name,
                "-d", name, "-v", "ON_ERROR_STOP=1", "-A", "-t", "-q"],
                input_text="SELECT current_user,current_database(),inet_server_port();", environment=login_env)
            require(actual == f"{name}|{name}|15432", "The SCRAM login did not reach the exact disposable identity.")
        environment["GOBY_TEST_DATABASE_URL"] = environment["GOBY_TEST_BACKUP_SOURCE_DATABASE_URL"]
        create_private(self.output / "run.env", "".join(f"{key}={value}\n" for key, value in environment.items()).encode())
        packages = [] if self.args.mode == "catalog" else (self.args.package or ["./internal/backuppg"])
        for package in packages:
            prefix = package[2:] + "/"
            require(any(name.startswith(prefix) and name.endswith("_test.go") for name in self.source_files),
                    "A targeted package has no tests in the frozen source.")
        if self.args.mode == "catalog":
            import shlex
            body = "exec " + shlex.join([str(GO), "run", CATALOG_GENERATOR, str(self.catalog_output)]) + "\n"
        elif self.args.mode == "full":
            body = f'"{GO}" list ./... > "$GOTMPDIR/expected-packages.txt"\nexec /bin/bash scripts/test-env/run-backup-full-tests.sh\n'
        else:
            expression = [] if not self.args.run else ["-run", self.args.run]
            import shlex
            arguments = [str(GO), "test", "-race", "-p=1", "-count=1", "-timeout=20m", "-json", *expression, *packages]
            body = "exec " + shlex.join(arguments) + "\n"
        create_private(self.output / "run.sh", ("#!/bin/bash\nset -euo pipefail\numask 077\n" + body).encode())

    def execute(self):
        self.check_cluster(self.hba_after)
        require(verify_source(self.args.source, self.args.manifest_sha256, self.args.schema,
                              catalog_bootstrap=self.args.mode == "catalog") == (self.source_identity, self.source_files),
                "The source changed before unit launch.")
        if self.args.mode == "catalog":
            self.check_catalog_launch()
        require(unit_state(self.unit).get("LoadState") == "not-found", "The new unit name became occupied.")
        log = self.output / "go.log"
        create_private(log, b"")
        self.receipt["phase"] = "unit_pending"
        self.save()
        self.unit_intent = True
        arguments = ["/usr/bin/systemd-run", "--quiet", "--wait", "--unit=" + self.unit, "--service-type=exec",
            "--property=Description=" + self.tag, "--property=MemoryMax=1536M", "--property=MemorySwapMax=0",
            "--property=CPUQuota=150%", "--property=KillMode=control-group", "--property=UMask=0077",
            "--property=TasksMax=256", "--property=TimeoutStopSec=15", "--property=RuntimeMaxSec=2400",
            "--property=WorkingDirectory=" + str(self.args.source), "--property=EnvironmentFile=" + str(self.output / "run.env"),
            "--property=StandardOutput=append:" + str(log), "--property=StandardError=append:" + str(log),
            "/bin/bash", str(self.output / "run.sh")]
        with open(self.output / "unit.stdout", "xb") as stdout, open(self.output / "unit.stderr", "xb") as stderr:
            wrapper = subprocess.Popen(arguments, stdin=subprocess.DEVNULL, stdout=stdout, stderr=stderr, env=ENVIRONMENT)
            self.wrapper = wrapper
            deadline = time.monotonic() + 2430
            while wrapper.poll() is None:
                state = unit_state(self.unit)
                if state.get("LoadState") != "not-found":
                    require_unit_owner(state, self.unit, self.tag)
                    require(state.get("MemoryMax") == str(1536 << 20) and state.get("MemorySwapMax") == "0" and
                            state.get("CPUQuotaPerSecUSec") == "1.500000s" and state.get("KillMode") == "control-group",
                            "The unit resource limits differ from their reviewed bounds.")
                    if state.get("MainPID") not in (None, "0"):
                        require(state.get("ControlGroup") == "/system.slice/" + self.unit,
                                "The running process is not bound to the exact owned cgroup.")
                        self.unit_observed = True
                        if "unit_process" not in self.report:
                            self.report["unit_process"] = self.module.process_identity(int(state["MainPID"]))
                            self.receipt["phase"] = "running"
                            self.save()
                require(time.monotonic() < deadline, "The bounded unit exceeded its outer observation deadline.")
                time.sleep(0.2)
            self.report["unit_exit"] = wrapper.returncode
        require(self.unit_observed and wrapper.returncode == 0, "The verified unit failed or was never observed running.")
        require_unit_terminal(unit_state(self.unit), self.unit, self.tag)
        self.inspect_results()
        require(verify_source(self.args.source, self.args.manifest_sha256, self.args.schema,
                              catalog_bootstrap=self.args.mode == "catalog") == (self.source_identity, self.source_files),
                "Verification changed frozen source bytes or membership.")

    def check_catalog_launch(self):
        require(self.args.mode == "catalog" and type(self.args.schema) is int and self.args.schema in BOOTSTRAP_MIGRATIONS and self.catalog_artifact is None,
                "Catalog launch requires a new explicit supported schema generation attempt.")
        require(directory(self.output) == self.output_identity and private_read(RECEIPT) == self.receipt_bytes,
                "The owned catalog output directory or current pair receipt changed before launch.")
        require(self.catalog_output == self.output / f"schema-{self.args.schema}-postgresql-17.json" and not present(self.catalog_output),
                "The catalog output is not an unused path in the owned run directory.")
        self.inspect_catalog_pairs([], [])

    def inspect_catalog_pairs(self, source_objects, target_objects):
        require([pair["name"] for pair in self.pairs] == list(NAMES), "Catalog generation requires the exact disposable pair.")
        for pair, expected in zip(self.pairs, (source_objects, target_objects)):
            require(pair["phase"] == "owned", "Catalog generation requires complete fresh pair receipts.")
            validate_pair_rows(pair, pair_rows(pair["name"]), self.tag)
            require_role_isolated(pair)
            require(self.public_identity(pair["name"]) == pair["public"] and self.cast_inventory(pair["name"]) == pair["casts"],
                    "A catalog database acquired unknown namespace metadata or cast definitions.")
            require(self.inspect_objects(pair) == expected, "A catalog database differs from its exact expected object inventory.")

    def inspect_catalog_result(self, log):
        require(self.args.mode == "catalog" and type(self.args.schema) is int and self.args.schema in BOOTSTRAP_MIGRATIONS and self.catalog_artifact is None,
                "A generated catalog cannot be adopted or replaced by a later attempt.")
        require(log.count(f"Generated trusted schema {self.args.schema} catalog.\n".encode()) == 1,
                "The catalog generator did not report exactly one requested-schema completion.")
        require(directory(self.output) == self.output_identity and self.catalog_output == self.output / f"schema-{self.args.schema}-postgresql-17.json",
                "The catalog output directory or fixed artifact path changed.")
        raw = private_read(self.catalog_output)
        baseline = generated_catalog(raw, self.source_files, self.args.schema)
        self.inspect_catalog_pairs(baseline["objects"], [])
        artifact = {"path": str(self.catalog_output), "sha256": sha(raw), "schema": self.args.schema,
                    "source_manifest_sha256": self.args.manifest_sha256}
        self.receipt["catalog_artifact"] = artifact
        self.receipt["catalog_sha256"] = artifact["sha256"]
        self.save()
        self.catalog_artifact = artifact
        self.report["catalog_artifact"] = artifact
        self.report["catalog_sha256"] = artifact["sha256"]
        self.report["catalog_objects_sha256"] = baseline["catalog"]["SHA256"]

    def read_generated_catalog(self):
        require(self.args.mode == "catalog" and type(self.args.schema) is int and self.args.schema in BOOTSTRAP_MIGRATIONS and self.catalog_artifact is not None,
                "This run has no completed and receipted catalog artifact; pair evidence must be retained.")
        exact_fields(self.catalog_artifact, {"path", "sha256", "schema", "source_manifest_sha256"},
                     "The generated catalog receipt contains missing or unowned fields.")
        require(self.catalog_output == self.output / f"schema-{self.args.schema}-postgresql-17.json" and
                self.catalog_artifact["path"] == str(self.catalog_output) and type(self.catalog_artifact["schema"]) is int and
                self.catalog_artifact["schema"] == self.args.schema and isinstance(self.catalog_artifact["sha256"], str) and
                re.fullmatch(r"[0-9a-f]{64}", self.catalog_artifact["sha256"]) and
                self.catalog_artifact["source_manifest_sha256"] == self.args.manifest_sha256 and
                self.receipt.get("catalog_artifact") == self.catalog_artifact and
                private_read(RECEIPT) == self.receipt_bytes and directory(self.output) == self.output_identity,
                "The exact generated catalog receipt, artifact path, or owned output directory changed.")
        raw = private_read(self.catalog_output)
        require(sha(raw) == self.catalog_artifact["sha256"], "The completed catalog artifact changed; pair evidence must be retained.")
        return generated_catalog(raw, self.source_files, self.args.schema)

    def inspect_results(self):
        log = private_read(self.output / "go.log", limit=128 << 20)
        if self.args.mode == "catalog":
            self.inspect_catalog_result(log)
            self.report["log_sha256"] = sha(log)
            return
        events = [decode(line) for line in log.splitlines() if line.startswith(b"{")]
        require(events and not any(event.get("Action") == "fail" or
                (event.get("Action") == "skip" and event.get("Test")) for event in events) and
                b"WARNING: DATA RACE" not in log, "The test stream contains skips, failures, or a race warning.")
        passed = {event["Test"] for event in events if event.get("Action") == "pass" and event.get("Test") and "/" not in event["Test"]}
        require(passed, "The selected test expression executed no test cases.")
        if self.args.mode == "full" or (not self.args.run and "./internal/backuppg" in (self.args.package or ["./internal/backuppg"])):
            require(PG_CASES <= passed, "The real PostgreSQL cases were not all executed.")
        if self.args.mode == "full":
            packages = set(private_read(self.output / "tmp/expected-packages.txt").decode().splitlines())
            finished = {event.get("Package") for event in events if not event.get("Test") and event.get("Action") in ("pass", "skip")}
            require(len(packages) >= 24 and packages == finished, "Complete verification omitted a source package.")
            for event in events:
                if event.get("Action") == "skip" and not event.get("Test"):
                    require(any(other.get("Package") == event.get("Package") and "[no test files]" in other.get("Output", "") for other in events),
                            "A package was skipped without a no-test-files result.")
            required = {"TestRecoveryDatabaseStoreIntegration", "TestRecoveryManagerNativeWorkflow",
                        "TestRecoveryManagerApplyRestartAndRollback", "TestMigrateUserSettingsPreservesSchema23DataAndLeavesPreferencesEmpty"}
            require(required <= passed, "Complete verification omitted a required migration or recovery workflow.")
            binary = self.output / "tmp/goby-linux-amd64"
            info = canonical(binary)
            require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_gid == 0 and
                    stat.S_IMODE(info.st_mode) == 0o755 and info.st_nlink == 1 and info.st_size > 1 << 20,
                    "Complete verification did not retain its built executable.")
            self.report["binary"] = {"path": str(binary), "sha256": sha(binary.read_bytes()), "bytes": info.st_size}
            self.report["packages"] = sorted(packages)
        self.report["tests"] = {"top_level_passes": len(passed), "passed": sorted(passed), "skips": 0, "failures": 0}
        self.report["log_sha256"] = sha(log)

    def stop_unit(self):
        if not self.unit_intent:
            return
        require(private_read(RECEIPT) == self.receipt_bytes and directory(self.output) == self.output_identity,
                "The unit ownership receipt or output directory changed.")
        state = unit_state(self.unit)
        if state.get("LoadState") != "not-found" and state.get("ActiveState") not in ("inactive", "failed"):
            require_unit_owner(state, self.unit, self.tag)
            if state.get("MainPID") not in (None, "0") and "unit_process" in self.report:
                require(self.module.process_identity(int(state["MainPID"])) == self.report["unit_process"],
                        "The unit process identity changed; an unknown process will not be stopped.")
            command(["/usr/bin/systemctl", "stop", self.unit], timeout=25)
        require_unit_terminal(unit_state(self.unit), self.unit, self.tag)
        wrapper = getattr(self, "wrapper", None)
        if wrapper is not None and wrapper.poll() is None:
            try:
                wrapper.wait(timeout=10)
            except subprocess.TimeoutExpired:
                raise Failure("The recorded systemd observer is still live; pair cleanup is blocked.") from None

    def remove_pair(self, pair):
        self.check_cluster(self.hba_before)
        require(pair["phase"] == "owned", "Incomplete pair creation must be inspected without deletion.")
        validate_pair_rows(pair, pair_rows(pair["name"]), self.tag)
        require_role_isolated(pair)
        name, role, database = pair["name"], pair["role_oid"], pair["database_oid"]
        require(pg(f"SELECT (SELECT count(*) FROM pg_stat_activity WHERE datid={database})+"
            f"(SELECT count(*) FROM pg_prepared_xacts WHERE database='{name}')+"
            f"(SELECT count(*) FROM pg_replication_slots WHERE database='{name}');") == "0",
            "The owned pair has live connections, prepared work, or replication slots; no backend will be terminated.")
        public = self.public_identity(name)
        # The recovery reset deliberately recreates public through trusted SQL.
        # Its namespace OID changes while its owner, ACL, and comment survive.
        require(type(public.get("oid")) is int and public["oid"] > 0 and
                {key: value for key, value in public.items() if key != "oid"} ==
                {key: value for key, value in pair["public"].items() if key != "oid"},
                "The public schema ownership or ACL changed.")
        require(self.cast_inventory(name) == pair["casts"], "The disposable database has unknown cast definitions.")
        objects = self.inspect_objects(pair)
        require(verify_source(self.args.source, self.args.manifest_sha256, self.args.schema,
                              catalog_bootstrap=self.args.mode == "catalog") == (self.source_identity, self.source_files),
                "The frozen source changed before disposable pair cleanup.")
        if self.args.mode == "catalog":
            catalog = self.read_generated_catalog()
            expected = catalog["objects"] if name == NAMES[0] else []
            require(objects == expected, "The catalog generation pair differs from its exact receipted source or empty target.")
        else:
            catalog = read_source_catalog(self.args.source, self.source_files, self.args.schema)
            require(objects == [] or objects == catalog["objects"],
                    f"The pair contains objects outside an empty database or the exact trusted schema{self.args.schema} catalog.")
        create_private(self.output / (name + "-objects.json"), json.dumps(objects, sort_keys=True).encode())
        validate_pair_rows(pair, pair_rows(name), self.tag)
        require_role_isolated(pair)
        # Closing only this verified disposable database fences new connections.
        # Ordinary DROP DATABASE will still refuse a racing backend; never FORCE.
        pg(f"ALTER DATABASE {name} ALLOW_CONNECTIONS false;")
        pair["phase"] = "closed"
        self.save()
        require(pg(f"SELECT count(*) FROM pg_stat_activity WHERE datid={database};") == "0",
                "A backend raced the disposable database fence; evidence was retained.")
        current = pair_rows(name)
        expected = dict(pair["database_fingerprint"], allow=False)
        require(current["database"] == expected, "The fenced database identity changed before deletion.")
        pg(f"DROP DATABASE {name};")
        pair["phase"] = "database_removed"
        self.save()
        require(pg(f"SELECT (SELECT count(*) FROM pg_shdepend WHERE refclassid='pg_authid'::regclass AND refobjid={role})+"
                   f"(SELECT count(*) FROM pg_auth_members WHERE member={role} OR roleid={role} OR grantor={role});") == "0",
                "The disposable role still owns unknown objects or memberships.")
        require(pair_rows(name)["role"] == current["role"], "The role identity changed before deletion.")
        pg(f"DROP ROLE {name};")
        require(pair_rows(name) == {"role": None, "database": None}, "The owned pair remains after cleanup.")
        pair["phase"] = "removed"
        self.save()

    def cleanup(self):
        def attempt(name, action):
            try:
                action()
                self.report["cleanup"][name] = True
            except Exception as error:
                self.report["cleanup"][name] = False
                self.report.setdefault("cleanup_errors", {})[name] = str(error) if isinstance(error, Failure) else type(error).__name__
        for number in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
            signal.signal(number, signal.SIG_IGN)
        attempt("unit_terminal", self.stop_unit)
        if self.hba_intent:
            def restore_hba():
                actual = private_read(HBA, uid=self.postgres.pw_uid, gid=self.postgres.pw_gid)
                require(actual in (self.hba_before, self.hba_after), "An unrelated HBA edit prevents automatic restoration.")
                self.check_cluster(actual)
                if actual == self.hba_after:
                    replace_private(HBA, actual, self.hba_before, self.postgres.pw_uid, self.postgres.pw_gid)
                self.reload_hba(self.hba_before)
                self.report["hba_after_sha256"] = sha(self.hba_before)
            attempt("hba_restored_exactly", restore_hba)
        if self.pairs and self.report["status"] == "passed" and all(self.report["cleanup"].values()):
            for pair in reversed(self.pairs):
                attempt(pair["name"] + "_removed", lambda pair=pair: self.remove_pair(pair))
        elif self.pairs:
            self.report["pair_evidence_retained"] = True
        if self.before_catalog is not None and all(pair["phase"] == "removed" for pair in self.pairs):
            def compare_catalog():
                after = catalog_state()
                create_private(self.output / "catalog-after.json", json.dumps(after, sort_keys=True).encode())
                require(after == self.before_catalog, "Preexisting database, role, membership, or setting metadata changed.")
            attempt("preexisting_catalog_unchanged", compare_catalog)
        if not all(self.report["cleanup"].values()):
            self.report["status"] = "failed"
        if self.receipt is not None:
            complete = self.report["status"] == "passed" and all(pair["phase"] == "removed" for pair in self.pairs)
            self.receipt["phase"] = "finished" if complete else "retained"
            self.receipt["cleanup_complete"] = complete
            attempt("receipt_saved", self.save)
        if not all(self.report["cleanup"].values()):
            self.report["status"] = "failed"
        if self.lock is not None:
            fcntl.flock(self.lock, fcntl.LOCK_UN)
            os.close(self.lock)
            self.lock = None

    def run_all(self):
        try:
            self.prepare()
            self.execute()
            self.report["status"] = "passed"
        except Exception as error:
            self.report["status"] = "failed"
            self.report["error"] = str(error) if isinstance(error, Failure) else type(error).__name__
        finally:
            self.cleanup()
            if self.created_output:
                create_private(self.output / "report.json", json.dumps(self.report, sort_keys=True, indent=2).encode())
        print(json.dumps({"status": self.report["status"], "report": str(self.output / "report.json") if self.created_output else None,
                          "error": self.report.get("error")}, sort_keys=True))
        return 0 if self.report["status"] == "passed" else 1


def main(arguments=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--manifest-sha256", required=True)
    parser.add_argument("--schema", type=int, choices=(24, 25, 26, 27), default=24,
                        help="Explicit source schema; schema25/26/27 require this option and no schema is inferred.")
    parser.add_argument("--mode", choices=("full", "targeted", "catalog"), required=True)
    parser.add_argument("--package", action="append", default=[])
    parser.add_argument("--run", default="")
    args = parser.parse_args(arguments)
    validate_arguments(args)
    def interrupted(_number, _frame):
        raise Failure("The outer verification operator was interrupted; owned evidence will be retained.")
    for number in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
        signal.signal(number, interrupted)
    return Runner(args).run_all()


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Failure as error:
        raise SystemExit(str(error)) from None
