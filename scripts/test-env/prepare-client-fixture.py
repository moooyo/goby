#!/usr/bin/env python3
"""Prepare or inspect the independently owned M3e Goby client fixture.

Run only as root through ssh test-env. This operator uses the persistent
workspace cluster on port 15432 and never connects to the shared port 5432.
Preparation is additive: no database, credential, media, evidence, or service is
deleted. Interrupted ownership publication fails closed and retains evidence.
The prepare mode starts the dedicated service; inspect never starts it. Upgrade
requires an explicitly pinned root-owned binary in this marked workspace and
retains the old binary and state. Failed upgrades never trigger rollback.
The add-viewer mode appends only the named AV acceptance account. Its private
receipt is required before subsequent inspections accept that third account.
"""

from __future__ import annotations

import argparse
import datetime as dt
from decimal import Decimal
import fcntl
import hashlib
import http.client
import json
import os
from pathlib import Path
import pwd
import re
import secrets
import socket
import stat
import subprocess
import sys
import time
import traceback
from urllib.parse import urlsplit


WORK = Path("/opt/goby-test/exec-work-m3e")
MARKER = "goby-m3e-client-acceptance-v1"
STATE_FILE = WORK / "client-fixture.json"
BROWSER = WORK / "browser.json"
RUNTIME = WORK / "runtime.env"
LOCK = WORK / "client-fixture.lock"
INSTALL = Path("/opt/goby-client-m3e")
BINARY = INSTALL / "goby"
SOURCE = Path("/opt/goby-dev/goby")
SOURCE_SHA = "a4abba7b289ceb74ca0916484d714dc6baa1b3c5230b06964b89d363a64e8b81"
WEB = Path("/opt/goby-dev/admin")
DATA = Path("/var/lib/goby-test/client-m3e")
UNIT = "goby-client-m3e.service"
UNIT_FILE = Path("/etc/systemd/system") / UNIT
PORT = 18198
PUBLIC = "http://127.0.0.1:18196"
ROLE = "goby_client_m3e"
PG_PORT = 15432
PG_CONTROL = Path("/opt/goby-test/postgres-workspace-v1")
PG_BASE = Path("/var/lib/postgresql/goby-workspace-v1")
PG_DATA = PG_BASE / "data"
PG_SOCKET = PG_BASE / "socket"
PG_BIN = Path("/usr/lib/postgresql/17/bin")
FFMPEG = Path("/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg")
FFPROBE = Path("/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffprobe")
SCHEMA_ARTIFACTS = {
    23: (WORK / "catalog-24-source-01/internal/backuppg/catalogs/schema-23-postgresql-17.json",
         "de85f4917dd7409e7e7bed20c7cbe63f0d7afe68f6ed72faff6b5bbb598ed00b"),
    24: (WORK / "schema-24-postgresql-17.json",
         "6ba8a30d7648f3fdd73f977cc5d2aafac39232704542f28f20f0898d93ef575c"),
}
MIGRATION_24_SHA = "2a9c159f111073a1c01bbbda158cfe3d98d25f417d70c7eda14b70a7798a3972"
MIGRATION_24_NAME = "0024_user_settings.sql"
MIGRATION_25_NAME = "0025_music_artists.sql"
MIGRATION_25_SHA = "3b21cac79173b4b9184d73b76066aff4199a4d6d4ad6fc64d2d0d826796b6eaf"
MIGRATION_26_NAME = "0026_theme_owners.sql"
MIGRATION_26_SHA = "c4b7485174fe655524fe5de6973f9d75a6ce7917f4ebd82476ac1c13be5817e8"
CATALOG_25_SHA = "e269a7eb6b31d2eb3fff734896ca074a6113761f321f2f23f4b07441e7dc617b"
CATALOG_26_SHA = "e02c46a49dd4200bb67954f70ca0bbe90b3ef99d8821ea56a97e3dcb97e696de"
CATALOG_26_OBJECTS_SHA = "6cbf3a672e965c4e62a137bccea21f27ff6914dc9413d95272da7d860945df31"
SCHEMA_TABLE_COUNTS = {23: 29, 24: 30, 25: 30, 26: 33}
SCHEMA_25_SOURCE_MARKER = "goby-client-schema25-source-m3e-v1"
SCHEMA_26_SOURCE_MARKER = "goby-client-schema26-source-m3e-v1"
SCHEMA_25_NEW_COLUMNS = {"item_metadata_state": ("music_source", {}), "item_entities": ("credit_group", 0)}
SCHEMA_26_TABLES = {"theme_owner_ids", "theme_reserved_paths", "item_theme_resources"}
SCHEMA_26_SEQUENCE = "theme_owner_ids_id_seq"
THEME_AUDIO_EXTENSIONS = {"mp3", "flac", "m4a", "aac", "ogg", "opus", "wav", "wma", "aiff", "aif", "alac", "ape", "mka"}
PRODUCT_PACKAGES = {"github.com/moooyo/goby/cmd/goby"} | {"github.com/moooyo/goby/internal/" + name for name in (
    "activity", "artwork", "backupformat", "backuppg", "backupstore", "config", "database", "diagnostics", "events",
    "identity", "library", "lifecycle", "media", "metadata", "playback", "recovery", "recoverycontrol", "recoverydb",
    "server", "settings", "subtitle", "tasks", "transcode")}
PRODUCT_REQUIRED_TESTS = {"TestThemeOwnersMigrationPreservesSchema25RowsSequencesAndRollback",
    "TestThemeReservedPathsMigrationPreservesSchema25AndReservesOnlyCanonicalLayouts",
    "TestPostgreSQLThemeRestorePreservesInactiveClassificationAndRetriesSemanticFinalizer",
    "TestPostgreSQLThemeRestoreRejectsSemanticallyInvalidRealArchives",
    "TestRecoveryManagerNativeWorkflow", "TestRecoveryDatabaseStoreIntegration"}
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
MAX_SNAPSHOT_BYTES = 64 << 20
ENV = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8"}
DIRECTORIES = ("cache", "diagnostics", "recovery", "backups", "operations", "tmp")
MEDIA_ROOT = Path("/opt/goby-fixtures/client-m3e")
MEDIA_MARKER = "goby-client-media-m3e-v1"
LIBRARIES = (
    {"key": "movies", "name": "M3e Client Movies", "type": "movies",
     "path": str(MEDIA_ROOT / "Movies")},
    {"key": "tv", "name": "M3e Client Television", "type": "tvshows",
     "path": str(MEDIA_ROOT / "TV")},
    {"key": "music", "name": "M3e Client Music", "type": "music",
     "path": str(MEDIA_ROOT / "Music")},
)
ACCOUNTS = {"admin": "m3e-client-administrator", "viewer": "m3e-client-viewer"}
AV_NAME = "m3e-goby-av-client"
AV_MARKER = "goby-m3e-goby-av-user-v1"
AV_ROOT = WORK / "goby-av-user-v1"
AV_BROWSER = WORK / "goby-av-browser.json"
AV_RECEIPT = WORK / "goby-av-user-receipt.json"
UNIT_TEXT = f"""[Unit]
Description=Goby owned M3e client acceptance fixture
After=network.target

[Service]
Type=simple
User=goby
Group=goby
WorkingDirectory={DATA}
EnvironmentFile={RUNTIME}
ExecStart={BINARY}
Restart=no
KillMode=control-group
TimeoutStopSec=30
UMask=0077
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths={DATA}
ReadOnlyPaths=/opt/goby-fixtures {INSTALL} {WEB}
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
RestrictSUIDSGID=true
LockPersonality=true
MemoryMax=512M
CPUQuota=100%
TasksMax=96
LimitNOFILE=4096
StandardOutput=journal
StandardError=journal
"""

# Keep this read-only projection aligned with backuppg.catalogObjectsSQL. The
# pinned catalog bytes independently determine the accepted object facts.
CATALOG_OBJECTS_SQL = """WITH namespace AS (SELECT oid FROM pg_catalog.pg_namespace WHERE nspname='public'),
relations AS (SELECT c.* FROM pg_catalog.pg_class c WHERE c.relnamespace=(SELECT oid FROM namespace)),
objects AS (
SELECT 'relation' AS kind,c.relname::text AS name,jsonb_build_object('kind',c.relkind,'persistence',c.relpersistence,
 'partition',c.relispartition,'replica_identity',c.relreplident,'row_security',c.relrowsecurity,'force_row_security',c.relforcerowsecurity,'options',c.reloptions) AS value FROM relations c
UNION ALL SELECT 'column',c.relname||'.'||lpad(a.attnum::text,5,'0'),jsonb_build_object('name',a.attname,'type',pg_catalog.format_type(a.atttypid,a.atttypmod),
 'not_null',a.attnotnull,'identity',a.attidentity,'generated',a.attgenerated,'dropped',a.attisdropped,
 'storage',a.attstorage,'compression',a.attcompression,'collation',CASE WHEN a.attcollation=0 THEN NULL ELSE a.attcollation::regcollation::text END,
 'default',pg_catalog.pg_get_expr(d.adbin,d.adrelid,true)) FROM relations c JOIN pg_catalog.pg_attribute a ON a.attrelid=c.oid
 LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid=c.oid AND d.adnum=a.attnum WHERE a.attnum>0
UNION ALL SELECT 'constraint',c.relname||'.'||k.conname,jsonb_build_object('type',k.contype,'definition',pg_catalog.pg_get_constraintdef(k.oid,true),
 'validated',k.convalidated,'deferrable',k.condeferrable,'deferred',k.condeferred,'no_inherit',k.connoinherit) FROM relations c JOIN pg_catalog.pg_constraint k ON k.conrelid=c.oid
UNION ALL SELECT 'index',c.relname,jsonb_build_object('table',t.relname,'method',am.amname,'unique',i.indisunique,'primary',i.indisprimary,
 'exclusion',i.indisexclusion,'immediate',i.indimmediate,'valid',i.indisvalid,'ready',i.indisready,'live',i.indislive,
 'nulls_not_distinct',i.indnullsnotdistinct,'key_count',i.indnkeyatts,'options',i.indoption::text,
 'operator_classes',ARRAY(SELECT n.nspname||'.'||o.opcname FROM unnest(i.indclass) WITH ORDINALITY k(oid,position) JOIN pg_catalog.pg_opclass o ON o.oid=k.oid JOIN pg_catalog.pg_namespace n ON n.oid=o.opcnamespace ORDER BY k.position),
 'collations',ARRAY(SELECT CASE WHEN k.oid=0 THEN NULL ELSE k.oid::regcollation::text END FROM unnest(i.indcollation) WITH ORDINALITY k(oid,position) ORDER BY k.position),
 'columns',ARRAY(SELECT pg_catalog.pg_get_indexdef(c.oid,k,true) FROM generate_series(1,i.indnatts) k),
 'predicate',pg_catalog.pg_get_expr(i.indpred,i.indrelid,true)) FROM relations c JOIN pg_catalog.pg_index i ON i.indexrelid=c.oid JOIN relations t ON t.oid=i.indrelid JOIN pg_catalog.pg_am am ON am.oid=c.relam
UNION ALL SELECT 'sequence',c.relname,jsonb_build_object('type',pg_catalog.format_type(s.seqtypid,NULL),'start',s.seqstart,'increment',s.seqincrement,
 'max',s.seqmax,'min',s.seqmin,'cache',s.seqcache,'cycle',s.seqcycle,'owned_by',t.relname||'.'||a.attname)
 FROM relations c JOIN pg_catalog.pg_sequence s ON s.seqrelid=c.oid LEFT JOIN pg_catalog.pg_depend d ON d.classid='pg_catalog.pg_class'::regclass AND d.objid=c.oid AND d.refclassid='pg_catalog.pg_class'::regclass AND d.deptype IN ('a','i')
 LEFT JOIN relations t ON t.oid=d.refobjid LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid=t.oid AND a.attnum=d.refobjsubid
UNION ALL SELECT 'function',p.proname||'('||pg_catalog.pg_get_function_identity_arguments(p.oid)||')',jsonb_build_object('kind',p.prokind,'language',l.lanname,
 'return_type',pg_catalog.pg_get_function_result(p.oid),'arguments',pg_catalog.pg_get_function_arguments(p.oid),'source',p.prosrc,'binary',p.probin,
 'security_definer',p.prosecdef,'leakproof',p.proleakproof,'strict',p.proisstrict,'returns_set',p.proretset,'volatility',p.provolatile,'parallel',p.proparallel,'config',p.proconfig,
 'cost',p.procost,'rows',p.prorows,'support',CASE WHEN p.prosupport=0 THEN NULL ELSE p.prosupport::regproc::text END)
 FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_language l ON l.oid=p.prolang WHERE p.pronamespace=(SELECT oid FROM namespace)
UNION ALL SELECT 'trigger',c.relname||'.'||t.tgname,jsonb_build_object('function',t.tgfoid::regprocedure::text,'enabled',t.tgenabled,'type',t.tgtype,
 'arguments',encode(t.tgargs,'hex'),'columns',t.tgattr::text,'when',pg_catalog.pg_get_expr(t.tgqual,t.tgrelid,true),'old_table',t.tgoldtable,'new_table',t.tgnewtable)
 FROM relations c JOIN pg_catalog.pg_trigger t ON t.tgrelid=c.oid WHERE NOT t.tgisinternal
) SELECT COALESCE(jsonb_agg(jsonb_build_object('kind',kind,'name',name,'value',value) ORDER BY kind COLLATE "C",name COLLATE "C"),'[]'::jsonb) FROM objects"""


class FixtureError(Exception):
    """A guard rejected an unsafe or ambiguous fixture operation."""


def require(condition, message):
    if not condition:
        raise FixtureError(message)


def utc():
    return dt.datetime.now(dt.timezone.utc).isoformat()


def exists(path):
    return os.path.lexists(path)


def identity(info):
    return {"device": info.st_dev, "inode": info.st_ino}


def canonical(path):
    require(path.is_absolute(), "An operator path is not absolute.")
    for part in reversed((path, *path.parents)):
        require(not stat.S_ISLNK(part.lstat().st_mode), "An operator path contains a symlink.")
    return path.lstat()


def directory(path, uid, mode, gid=None):
    info = canonical(path)
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == uid and
            (gid is None or info.st_gid == gid) and stat.S_IMODE(info.st_mode) == mode,
            "A managed directory has unexpected ownership, permissions, or type.")
    return identity(info)


def regular(path, uid=0, mode=0o600, limit=2 << 20):
    info = canonical(path)
    require(stat.S_ISREG(info.st_mode) and info.st_uid == uid and stat.S_IMODE(info.st_mode) == mode and
            info.st_nlink == 1 and info.st_size <= limit,
            "A managed file has unexpected ownership, permissions, type, size, or hard links.")
    return info


def read(path, uid=0, mode=0o600, limit=2 << 20):
    expected = regular(path, uid, mode, limit)
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
        require(identity(os.fstat(stream.fileno())) == identity(expected), "A managed file changed during open.")
        content = stream.read(limit + 1)
    require(len(content) <= limit, "A managed file exceeded its read budget.")
    return content


def sha(path, uid=0, mode=0o600, limit=2 << 20):
    return hashlib.sha256(read(path, uid, mode, limit)).hexdigest()


def strict_object(pairs):
    value = {}
    for key, entry in pairs:
        require(key not in value, "A private JSON record contains duplicate fields.")
        value[key] = entry
    return value


def load(path):
    return precise_json(read(path))


def sync(path):
    descriptor = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def create(path, content, mode=0o600):
    if not isinstance(content, bytes):
        content = content.encode() if isinstance(content, str) else canonical_json(content) + b"\n"
    canonical(path.parent)
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode), "wb") as stream:
        os.fchmod(stream.fileno(), mode)
        stream.write(content)
        stream.flush()
        os.fsync(stream.fileno())
    sync(path.parent)


def save_state(state):
    regular(STATE_FILE)
    temporary = WORK / ("client-fixture.next-" + secrets.token_hex(12))
    create(temporary, state)
    os.replace(temporary, STATE_FILE)
    sync(WORK)


def command(arguments, text=None, environment=None, timeout=30):
    try:
        result = subprocess.run([str(part) for part in arguments], input=text, text=True,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False,
                                env=environment or ENV, timeout=timeout)
    except (OSError, subprocess.TimeoutExpired):
        raise FixtureError("A bounded operator command did not complete.") from None
    if result.returncode:
        # Command output may contain SQL, connection details, or response data.
        # It remains root-private; only the diagnostic filename is reported.
        diagnostic = WORK / ("client-command-failure-" + secrets.token_hex(12) + ".txt")
        create(diagnostic, result.stdout + "\n" + result.stderr)
        raise FixtureError("An operator command failed; inspect " + diagnostic.name + ".")
    return result.stdout.strip()


def process_identity(pid):
    process = Path("/proc") / str(pid)
    raw = (process / "stat").read_text(encoding="ascii")
    fields = raw[raw.rfind(")") + 2:].split()
    require(len(fields) > 19 and fields[19].isdigit(), "A process start identity is malformed.")
    return {"pid": pid, "start_ticks": int(fields[19]),
            "boot_id": Path("/proc/sys/kernel/random/boot_id").read_text(encoding="ascii").strip()}


def properties():
    names = ("Id", "LoadState", "ActiveState", "SubState", "MainPID", "FragmentPath", "DropInPaths",
             "User", "Group", "ExecStart", "EnvironmentFiles", "WorkingDirectory", "Restart", "ControlGroup")
    value = command(["/usr/bin/systemctl", "show", UNIT, "--no-pager", "--property=" + ",".join(names)])
    return dict(line.split("=", 1) for line in value.splitlines() if "=" in line)


def host_inputs(initial=False):
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION"),
            "Run this operator through authorized root SSH on test-env.")
    require(os.readlink("/proc/self/ns/net") == os.readlink("/proc/1/ns/net"),
            "The fixture operator must run in the host network namespace.")
    require(directory(WORK, 0, 0o700, 0) and load(WORK / "OWNER.json") == {"marker": MARKER, "path": str(WORK)},
            "The existing private workspace owner does not match.")
    account = pwd.getpwnam("goby")
    require(account.pw_uid == 995 and account.pw_gid == 986, "The application account identity differs.")
    directory(DATA.parent, 995, 0o750, 986)
    if initial:
        require(sha(SOURCE, 0, 0o755, 64 << 20) == SOURCE_SHA, "The accepted initial candidate binary changed.")
    directory(WEB, 0, 0o755, 0)
    regular(WEB / "index.html", 0, 0o644)
    for path in (FFMPEG, FFPROBE, PG_BIN / "psql", PG_BIN / "postgres"):
        regular(path, 0, 0o755, 512 << 20)
    return account


def media_snapshot():
    root_identity = directory(MEDIA_ROOT, 0, 0o755, 0)
    require(read(MEDIA_ROOT / ".goby-managed", mode=0o644, limit=4096).decode().strip() == MEDIA_MARKER,
            "The dedicated synthetic media marker differs.")
    manifest_bytes = read(MEDIA_ROOT / "manifest.json", mode=0o644)
    manifest = json.loads(manifest_bytes, object_pairs_hook=strict_object)
    require(manifest.get("marker") == MEDIA_MARKER and manifest.get("movieProfile") ==
            {"audio": "aac", "durationSeconds": 600, "fps": 30, "video": "h264"} and
            isinstance(manifest.get("files"), dict), "The dedicated synthetic media manifest differs.")
    manifest_files = manifest["files"]
    require(manifest_files.get("Movies/M3e Client Movie.mp4") ==
            "7265bc56bd7f495bcbd5224adcf6df94478a99d1994ba713274a194f8f9db088",
            "The normal-frame-rate direct-play movie differs from the prepared fixture.")
    entries, files, links, digests = {}, set(), {}, {}
    for path in sorted(MEDIA_ROOT.rglob("*")):
        info = canonical(path)
        require(info.st_uid == 0 and info.st_gid == 0 and ((stat.S_ISDIR(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o755) or
                (stat.S_ISREG(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o644 and info.st_size <= 64 << 20)),
                "A shared media entry has unexpected type, ownership, permissions, or size.")
        require(len(entries) < 128, "The dedicated synthetic library exceeds its entry budget.")
        relative = str(path.relative_to(MEDIA_ROOT))
        entries[relative] = {**identity(info), "mode": stat.S_IMODE(info.st_mode),
            "size": info.st_size, "mtime_ns": info.st_mtime_ns, "ctime_ns": info.st_ctime_ns}
        if not stat.S_ISREG(info.st_mode):
            continue
        files.add(relative)
        key = (info.st_dev, info.st_ino)
        links.setdefault(key, []).append((path, info.st_nlink))
        if key not in digests:
            with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
                require(identity(os.fstat(stream.fileno())) == identity(info), "A shared media file changed during open.")
                digest = hashlib.sha256()
                for block in iter(lambda: stream.read(1 << 20), b""):
                    digest.update(block)
                after = os.fstat(stream.fileno())
                require((after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns) ==
                        (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns),
                        "A shared media file changed while hashing.")
                digests[key] = digest.hexdigest()
        if relative != "manifest.json":
            require(manifest_files.get(relative) == digests[key], "A synthetic media hash differs from its manifest.")
        command(["/usr/sbin/runuser", "-u", "goby", "--", "/usr/bin/test", "-r", path], timeout=5)
    require(files == set(manifest_files) | {"manifest.json"}, "The dedicated synthetic media inventory differs.")
    require(all(all(count == len(group) for _, count in group) for group in links.values()),
            "A synthetic hard link points beyond the complete recorded media inventory.")
    return {"root": root_identity, "manifest_sha256": hashlib.sha256(manifest_bytes).hexdigest(), "entries": entries}


def postgres(sql, database="postgres"):
    require(database in ("postgres", ROLE), "An administration query selected an unowned database.")
    return command(["/usr/sbin/runuser", "-u", "postgres", "--", PG_BIN / "psql", "-X", "--no-password",
                    "-h", PG_SOCKET, "-p", str(PG_PORT), "-U", "postgres", "-d", database,
                    "-v", "ON_ERROR_STOP=1", "-A", "-t", "-q"], text=sql, timeout=20)


def cluster_identity():
    directory(PG_CONTROL, 0, 0o700, 0)
    owner = load(PG_CONTROL / "owner.json")
    require(owner.get("marker") == "goby-postgres-persistent-workspace-v1" and owner.get("data") == str(PG_DATA) and
            owner.get("socket") == str(PG_SOCKET) and owner.get("port") == PG_PORT and owner.get("phase") == "initialized",
            "The dedicated PostgreSQL workspace owner is not ready.")
    current = owner.get("process")
    require(isinstance(current, dict) and type(current.get("pid")) is int and current["pid"] > 1,
            "The dedicated cluster has no recorded live process.")
    process = Path("/proc") / str(current["pid"])
    require(process_identity(current["pid"]) == current and process.stat().st_uid == pwd.getpwnam("postgres").pw_uid and
            (process / "exe").readlink() == PG_BIN / "postgres" and
            (process / "cmdline").read_bytes().rstrip(b"\0").split(b"\0") ==
            [str(PG_BIN / "postgres").encode(), b"-D", str(PG_DATA).encode()],
            "The dedicated PostgreSQL process identity differs from its owner.")
    for path in (PG_BASE, PG_DATA, PG_SOCKET, PG_BASE / "log"):
        metadata = canonical(path)
        require(identity(metadata) == owner.get("directories", {}).get(str(path)), "A cluster directory identity changed.")
    actual = postgres("SELECT current_setting('data_directory'),current_setting('port'),"
                      "current_setting('listen_addresses'),system_identifier FROM pg_control_system();")
    require(actual == f"{PG_DATA}|{PG_PORT}|127.0.0.1|{owner.get('system_identifier')}",
            "The administration connection reached another PostgreSQL cluster.")
    require(process_identity(current["pid"]) == current, "The PostgreSQL process changed during inspection.")
    return {"system_identifier": owner["system_identifier"], "data": str(PG_DATA), "port": PG_PORT}


def credentials(state):
    require(state.get("credentials_pending") is not True,
            "An interrupted credential publication requires inspection; existing files will not be adopted or rotated.")
    if "credentials_pending" not in state:
        require(not exists(RUNTIME) and not exists(BROWSER), "Unrecorded candidate credentials already exist.")
        state["credentials_pending"] = True
        save_state(state)
    if not exists(RUNTIME):
        require(state["credentials_pending"] is True and "runtime_sha256" not in state,
                "Recorded runtime credentials are missing; no password was rotated.")
        values = {"GOBY_DATABASE_URL": f"postgresql://{ROLE}:{secrets.token_hex(32)}@127.0.0.1:{PG_PORT}/{ROLE}?sslmode=disable",
            "GOBY_SETUP_TOKEN": secrets.token_hex(32), "GOBY_LISTEN": f"127.0.0.1:{PORT}", "GOBY_PUBLIC_URL": PUBLIC,
            "GOBY_COOKIE_SECURE": "false", "GOBY_SERVER_NAME": "Goby M3e Client Fixture", "GOBY_WEB_DIR": str(WEB),
            "GOBY_MEDIA_ROOTS": ":".join(library["path"] for library in LIBRARIES), "GOBY_TRANSCODING_ENABLED": "false",
            "GOBY_TRANSCODE_CACHE": str(DATA / "cache"), "GOBY_LOG_DIR": str(DATA / "diagnostics"),
            "GOBY_LOG_MAX_FILE_BYTES": "1048576", "GOBY_LOG_MAX_FILES": "4", "GOBY_LOG_MIN_FREE_BYTES": str(8 << 20),
            "GOBY_API_KEY_MASTER_KEY_FILE": str(DATA / "master.key"), "GOBY_RECOVERY_STATE_DIR": str(DATA / "recovery"),
            "GOBY_BACKUP_DIR": str(DATA / "backups"), "GOBY_RECOVERY_OPERATIONS_DIR": str(DATA / "operations"),
            "GOBY_PG_DUMP": str(PG_BIN / "pg_dump"), "GOBY_PG_RESTORE": str(PG_BIN / "pg_restore"),
            "GOBY_BACKUP_MIN_FREE_BYTES": str(16 << 20), "GOBY_STARTUP_TIMEOUT": "45s",
            "GOBY_FFMPEG": str(FFMPEG), "GOBY_FFPROBE": str(FFPROBE), "GOMAXPROCS": "2", "GOMEMLIMIT": "384MiB",
            "TMPDIR": str(DATA / "tmp")}
        require(all("'" not in value and "\n" not in value for value in values.values()), "An environment value is unsafe.")
        create(RUNTIME, "".join(f"{key}='{value}'\n" for key, value in sorted(values.items())))
    if not exists(BROWSER):
        require(state["credentials_pending"] is True and "browser_sha256" not in state,
                "Recorded browser credentials are missing; no password was rotated.")
        create(BROWSER, {"marker": MARKER, "base_url": PUBLIC, "direct_url": f"http://127.0.0.1:{PORT}",
                        **{key: {"username": name, "password": secrets.token_hex(24)} for key, name in ACCOUNTS.items()}})
    values = {}
    for line in read(RUNTIME).decode().splitlines():
        match = re.fullmatch(r"([A-Z][A-Z0-9_]*)='([^'\n]*)'", line)
        require(match is not None and match.group(1) not in values, "The private runtime environment is malformed.")
        values[match.group(1)] = match.group(2)
    url = urlsplit(values.get("GOBY_DATABASE_URL", ""))
    require(url.scheme == "postgresql" and url.hostname == "127.0.0.1" and url.port == PG_PORT and url.username == ROLE and
            url.path == "/" + ROLE and url.query == "sslmode=disable" and not url.fragment and
            re.fullmatch(r"[0-9a-f]{64}", url.password or ""), "The private connection does not identify the dedicated database.")
    browser = load(BROWSER)
    require(browser.get("marker") == MARKER and browser.get("base_url") == PUBLIC and
            browser.get("direct_url") == f"http://127.0.0.1:{PORT}" and all(browser.get(key, {}).get("username") == name and
            re.fullmatch(r"[0-9a-f]{48}", browser[key].get("password", "")) for key, name in ACCOUNTS.items()),
            "The private browser credentials differ from the fixture scope.")
    for path, name in ((RUNTIME, "runtime_sha256"), (BROWSER, "browser_sha256")):
        current = sha(path)
        require(name not in state or state[name] == current, "A recorded credential file changed.")
        state[name] = current
    state["credentials_pending"] = False
    save_state(state)
    return values, browser, url.password


def role_row():
    value = postgres("SELECT jsonb_build_object('oid',oid::bigint,'tag',shobj_description(oid,'pg_authid'),"
        "'login',rolcanlogin,'superuser',rolsuper,'createdb',rolcreatedb,'createrole',rolcreaterole,"
        "'replication',rolreplication,'bypassrls',rolbypassrls,'inherit',rolinherit) FROM pg_roles WHERE rolname='" + ROLE + "';")
    return json.loads(value) if value else None


def database_row():
    value = postgres("SELECT jsonb_build_object('oid',oid::bigint,'owner',pg_get_userbyid(datdba),"
        "'tag',shobj_description(oid,'pg_database')) FROM pg_database WHERE datname='" + ROLE + "';")
    return json.loads(value) if value else None


def verify_database(state):
    require(cluster_identity() == state["cluster"], "The recorded database cluster changed.")
    role = role_row()
    expected = {"oid": state.get("role_oid"), "tag": state["tag"], "login": True, "superuser": False,
                "createdb": False, "createrole": False, "replication": False, "bypassrls": False, "inherit": False}
    require(role == expected, "The candidate database role identity or privileges changed.")
    require(postgres("SELECT count(*) FROM pg_auth_members WHERE member=" + str(role["oid"]) +
                     " OR roleid=" + str(role["oid"]) + ";") == "0", "The candidate role has unexpected memberships.")
    require(database_row() == {"oid": state.get("database_oid"), "owner": ROLE, "tag": state["tag"]},
            "The candidate database has no matching recorded ownership.")


def prepare_database(state, password):
    require(cluster_identity() == state["cluster"], "The dedicated cluster identity changed before provisioning.")
    role = role_row()
    if role is None:
        require("role_oid" not in state, "The recorded candidate role is missing.")
        state["role_creation_pending"] = True
        save_state(state)
        postgres("BEGIN; SET LOCAL password_encryption='scram-sha-256'; CREATE ROLE " + ROLE +
                 " LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 20 PASSWORD '" +
                 password + "'; COMMENT ON ROLE " + ROLE + " IS '" + state["tag"] + "'; COMMIT;")
        role = role_row()
    require(role is not None and role["tag"] == state["tag"] and
            (state.get("role_oid") == role["oid"] or state.get("role_creation_pending") is True),
            "An existing database role is not owned by this fixture; no password was rotated.")
    state["role_oid"], state["role_creation_pending"] = role["oid"], False
    save_state(state)
    database = database_row()
    if database is None:
        require("database_oid" not in state, "The recorded candidate database is missing.")
        state["database_creation_pending"] = True
        save_state(state)
        postgres("CREATE DATABASE " + ROLE + " OWNER " + ROLE + ";\nCOMMENT ON DATABASE " + ROLE + " IS '" + state["tag"] + "';")
        database = database_row()
    require(database is not None and database["owner"] == ROLE and database["tag"] == state["tag"] and
            (state.get("database_oid") == database["oid"] or state.get("database_creation_pending") is True),
            "An existing database lacks recorded ownership; interrupted creation requires private evidence review.")
    if "database_oid" not in state:
        require(postgres("SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace "
                         "WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%';", ROLE) == "0",
                "The newly created candidate database is not empty.")
    state["database_oid"], state["database_creation_pending"] = database["oid"], False
    save_state(state)
    verify_database(state)
    actual = command([PG_BIN / "psql", "-X", "--no-password", "-h", "127.0.0.1", "-p", str(PG_PORT),
                      "-U", ROLE, "-d", ROLE, "-v", "ON_ERROR_STOP=1", "-Atq"],
                     text="SELECT current_user,current_database(),inet_server_port();",
                     environment=dict(ENV, PGPASSWORD=password, PGCONNECT_TIMEOUT="5"))
    require(actual == f"{ROLE}|{ROLE}|{PG_PORT}", "The candidate SCRAM login reached an unexpected database.")


def managed_directory(state, path, uid, gid, mode):
    recorded = state.setdefault("directories", {})
    key = str(path)
    if key not in recorded:
        require(not exists(path), "An unrecorded candidate directory already exists.")
        # Publish intent before mkdir. An interruption before identity recording
        # does not authorize adoption of an arbitrary subsequently occupied path.
        state["directory_pending"] = key
        save_state(state)
        path.mkdir(mode=mode)
        os.chown(path, uid, gid)
        os.chmod(path, mode)
        recorded[key] = directory(path, uid, mode, gid)
        state.pop("directory_pending", None)
        save_state(state)
    require(directory(path, uid, mode, gid) == recorded[key], "A recorded candidate directory was replaced.")


def prepare_files(state):
    require(state.get("binary_pending") is not True and state.get("unit_pending") is not True,
            "An interrupted service-file publication requires inspection; existing files will not be adopted.")
    managed_directory(state, INSTALL, 0, 0, 0o755)
    if not exists(BINARY):
        require("binary_sha256" not in state, "The recorded candidate executable is missing.")
        state["binary_pending"] = True
        save_state(state)
        create(BINARY, read(SOURCE, 0, 0o755, 64 << 20), 0o755)
    require(sha(BINARY, 0, 0o755, 64 << 20) == SOURCE_SHA and
            (state.get("binary_sha256") == SOURCE_SHA or state.get("binary_pending") is True),
            "The installed candidate executable is not owned by this attempt.")
    state["binary_sha256"], state["binary_pending"] = SOURCE_SHA, False
    save_state(state)
    managed_directory(state, DATA, 995, 986, 0o700)
    for name in DIRECTORIES:
        managed_directory(state, DATA / name, 995, 986, 0o700)
    # State, backups, and operations are siblings, disjoint from all media,
    # cache and diagnostic roots. The master key is created by Goby once.
    paths = [DATA / name for name in DIRECTORIES] + [Path(library["path"]) for library in LIBRARIES]
    require(all(left != right and left not in right.parents and right not in left.parents
                for index, left in enumerate(paths) for right in paths[index + 1:]), "Candidate runtime paths overlap.")
    expected = hashlib.sha256(UNIT_TEXT.encode()).hexdigest()
    if not exists(UNIT_FILE):
        require("unit_sha256" not in state and properties().get("LoadState") == "not-found",
                "The candidate unit exists outside the recorded service file.")
        state["unit_pending"] = True
        save_state(state)
        create(UNIT_FILE, UNIT_TEXT, 0o644)
    require(sha(UNIT_FILE, mode=0o644) == expected and
            (state.get("unit_sha256") == expected or state.get("unit_pending") is True),
            "An existing candidate unit is not owned by this attempt.")
    state["unit_sha256"], state["unit_pending"] = expected, False
    save_state(state)
    command(["/usr/bin/systemctl", "daemon-reload"])


def verify_fixture_directories(state):
    expected = {str(INSTALL): (0, 0, 0o755), str(DATA): (995, 986, 0o700),
                **{str(DATA / name): (995, 986, 0o700) for name in DIRECTORIES}}
    require(set(state.get("directories", {})) == set(expected), "The fixture directory inventory differs.")
    for path, (uid, gid, mode) in expected.items():
        require(directory(Path(path), uid, mode, gid) == state["directories"][path],
                "A recorded fixture directory was replaced.")
    require(sha(RUNTIME) == state["runtime_sha256"] and sha(BROWSER) == state["browser_sha256"],
            "Private fixture credentials changed.")


def verify_service(state, allow_new=False):
    expected_sha = state.get("binary_sha256", "")
    require(re.fullmatch(r"[0-9a-f]{64}", expected_sha), "The candidate has no valid recorded executable hash.")
    require(sha(UNIT_FILE, mode=0o644) == state["unit_sha256"] and
            state["unit_sha256"] == hashlib.sha256(UNIT_TEXT.encode()).hexdigest() and
            sha(BINARY, mode=0o755, limit=64 << 20) == expected_sha and sha(RUNTIME) == state["runtime_sha256"],
            "Candidate service inputs changed.")
    if "binary_identity" in state:
        require(identity(regular(BINARY, mode=0o755, limit=64 << 20)) == state["binary_identity"],
                "The recorded candidate executable inode changed.")
    status = properties()
    require(status.get("FragmentPath") == str(UNIT_FILE) and not status.get("DropInPaths") and
            status.get("User") == "goby" and status.get("Group") == "goby" and status.get("WorkingDirectory") == str(DATA) and
            status.get("Restart") == "no" and status.get("EnvironmentFiles") == str(RUNTIME) + " (ignore_errors=no)",
            "The loaded candidate unit does not match its private configuration.")
    pid = int(status.get("MainPID", "0"))
    if pid <= 1:
        require(status.get("ActiveState") in ("inactive", "failed"), "The candidate service is in an ambiguous transition.")
        return None
    current = process_identity(pid)
    process = Path("/proc") / str(pid)
    require((allow_new and state.get("start_pending") is True) or state.get("process") == current,
            "The running candidate lacks matching PID, boot, and start-tick ownership.")
    require(process.stat().st_uid == 995 and (process / "exe").readlink() == BINARY and
            (process / "cmdline").read_bytes() == str(BINARY).encode() + b"\0" and
            "/system.slice/" + UNIT in (process / "cgroup").read_text(),
            "The candidate PID belongs to another account, executable, invocation, or unit.")
    require(hashlib.sha256((process / "exe").read_bytes()).hexdigest() == expected_sha,
            "The running executable differs from the recorded candidate.")
    listeners = command(["/usr/bin/ss", "-H", "-ltnp", f"sport = :{PORT}"]).splitlines()
    require(len(listeners) == 1 and f"127.0.0.1:{PORT}" in listeners[0].split() and
            re.search(rf"\bpid={pid},", listeners[0]), "The candidate listener is not exclusively owned on loopback.")
    require(process_identity(pid) == current, "The candidate process changed during verification.")
    return current


def start_service(state):
    status = properties()
    if int(status.get("MainPID", "0")) > 1:
        current = verify_service(state, allow_new=True)
    else:
        require(not state.get("start_pending"), "An interrupted service start requires private evidence review.")
        require(verify_service(state) is None, "The candidate service state changed.")
        require(not command(["/usr/bin/ss", "-H", "-ltnp", f"sport = :{PORT}"]),
                "The candidate port is occupied by another listener.")
        with socket.socket() as probe:
            probe.bind(("127.0.0.1", PORT))
        verify_database(state)
        state["start_pending"] = True
        save_state(state)
        command(["/usr/bin/systemctl", "start", UNIT], timeout=55)
        deadline = time.monotonic() + 50
        while time.monotonic() < deadline:
            if command(["/usr/bin/ss", "-H", "-ltnp", f"sport = :{PORT}"]):
                break
            require(properties().get("ActiveState") not in ("failed", "inactive"), "The owned candidate exited during startup.")
            time.sleep(0.5)
        current = verify_service(state, allow_new=True)
    require(current is not None, "The candidate did not establish its owned process.")
    state["process"], state["start_pending"] = current, False
    save_state(state)


def verify_upgrade_input(path, expected_sha):
    require(isinstance(path, Path) and path.is_absolute() and ".." not in path.parts and WORK in path.parents and
            path not in (SOURCE, BINARY) and re.fullmatch(r"[0-9a-f]{64}", expected_sha or ""),
            "Upgrade requires an absolute binary path within the marked source workspace and an explicit SHA-256.")
    require(path.resolve(strict=True) == path, "The upgrade input does not retain its exact canonical workspace path.")
    source_mode = stat.S_IMODE(canonical(path).st_mode)
    require(source_mode in (0o700, 0o755), "The upgrade source lacks protected executable permissions.")
    info = regular(path, mode=source_mode, limit=64 << 20)
    for parent in path.parents:
        metadata = canonical(parent)
        require(stat.S_ISDIR(metadata.st_mode) and metadata.st_uid == 0 and not metadata.st_mode & 0o022,
                "An upgrade source parent is not protected from non-root writes.")
    content = read(path, mode=source_mode, limit=64 << 20)
    require(len(content) > 64 and content.startswith(b"\x7fELF") and hashlib.sha256(content).hexdigest() == expected_sha,
            "The upgrade source is not the explicitly pinned Linux executable.")
    after = regular(path, mode=source_mode, limit=64 << 20)
    require((info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns) ==
            (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns),
            "The upgrade source changed while it was read.")
    return content, identity(info)


def require_candidate_stopped():
    status = properties()
    require(status.get("ActiveState") in ("inactive", "failed") and int(status.get("MainPID", "0")) == 0,
            "The candidate service has not reached a terminal stopped state.")
    require(not command(["/usr/bin/ss", "-H", "-ltnp", f"sport = :{PORT}"]),
            "A listener still occupies the candidate port; it will not be stopped or adopted.")
    for path in Path("/proc").iterdir():
        if not path.name.isdigit():
            continue
        try:
            arguments = (path / "cmdline").read_bytes().rstrip(b"\0").split(b"\0")
        except (FileNotFoundError, ProcessLookupError):
            continue
        require(str(BINARY).encode() not in arguments,
                "An unaccounted process references the candidate executable; replacement is refused.")


def recovery_snapshot():
    entries = {}
    total = 0
    for name in ("recovery", "backups", "operations"):
        root = DATA / name
        for path in sorted(root.rglob("*")):
            info = canonical(path)
            require(info.st_uid == 995 and not info.st_mode & 0o077 and
                    (stat.S_ISDIR(info.st_mode) or stat.S_ISREG(info.st_mode) and info.st_nlink == 1),
                    "A recovery-state entry is not private and service-owned.")
            require(len(entries) < 4096, "The fixture recovery inventory exceeds its bounded upgrade scope.")
            entry = {"mode": stat.S_IMODE(info.st_mode), "directory": stat.S_ISDIR(info.st_mode)}
            if stat.S_ISREG(info.st_mode):
                total += info.st_size
                require(total <= 256 << 20 and info.st_size <= 128 << 20,
                        "Recovery-state bytes exceed the bounded upgrade scope.")
                entry.update(size=info.st_size, sha256=sha(path, uid=995, mode=stat.S_IMODE(info.st_mode), limit=128 << 20))
            entries[str(path.relative_to(DATA))] = entry
    master = DATA / "master.key"
    if exists(master):
        require(regular(master, 995, 0o600, 32).st_size == 32, "The private master key has an unexpected size.")
        entries["master.key"] = {"size": 32, "sha256": sha(master, 995, 0o600, 32)}
    return entries


def canonical_json(value):
    # PostgreSQL numeric/jsonb values can exceed binary-float precision. Emit
    # Decimal as a JSON number, retaining every digit and its decimal scale;
    # never turn it into a string, tagged object, or lossy Python float.
    def encode(item):
        if item is None:
            return "null"
        if type(item) is bool:
            return "true" if item else "false"
        if type(item) is int:
            return str(item)
        if isinstance(item, Decimal):
            require(item.is_finite(), "A JSON number is not finite.")
            return str(item)
        if isinstance(item, str):
            return json.dumps(item, ensure_ascii=False)
        if isinstance(item, list):
            return "[" + ",".join(encode(child) for child in item) + "]"
        if isinstance(item, dict):
            require(all(isinstance(key, str) for key in item), "A JSON object key is not a string.")
            return "{" + ",".join(json.dumps(key, ensure_ascii=False) + ":" + encode(item[key]) for key in sorted(item)) + "}"
        raise FixtureError("A preservation value is not an exact supported JSON primitive.")
    return encode(value).encode()


def precise_json(content):
    def invalid_constant(_):
        raise FixtureError("A JSON record contains a non-finite number.")
    def exact_integer(value):
        # Avoid Python's configurable decimal-to-int digit cap for otherwise
        # valid PostgreSQL numeric/jsonb values without changing ordinary IDs.
        return int(value) if len(value.lstrip("-")) <= 1000 else Decimal(value)
    return json.loads(content, object_pairs_hook=strict_object, parse_int=exact_integer, parse_float=Decimal, parse_constant=invalid_constant)


def equal_json(left, right):
    # Python equality treats True and 1 as equal. Preserve JSON primitive types
    # when comparing complete database rows, catalog facts, and guard fixtures.
    return canonical_json(left) == canonical_json(right)


def target_schema_version(state, requested=None):
    current = state.get("schema")
    target = current if requested is None else requested
    require(type(current) is int and type(target) is int and
            (current, target) in ((23, 23), (23, 24), (24, 24), (24, 25), (25, 25), (25, 26), (26, 26)),
            "Only explicit published adjacent schema upgrades and supported same-schema replacements are permitted.")
    return target


def source_manifest(source, expected):
    require(isinstance(source, Path) and source.is_absolute() and ".." not in source.parts and WORK in source.parents and
            re.fullmatch(r"[0-9a-f]{64}", expected or ""), "The explicit source snapshot or digest is invalid.")
    require(source.resolve(strict=True) == source, "The explicit source snapshot is not canonical.")
    for parent in (source, *source.parents):
        metadata = canonical(parent)
        require(stat.S_ISDIR(metadata.st_mode) and metadata.st_uid == 0 and not metadata.st_mode & 0o022,
                "The explicit source snapshot is not protected from non-root writes.")
    content = read(source / "backup-source-inputs.json", mode=0o644)
    require(hashlib.sha256(content).hexdigest() == expected,
            "The explicit source manifest differs from its supplied digest.")
    manifest = precise_json(content)
    require(isinstance(manifest, dict) and set(manifest) == {"marker", "files"} and
            manifest["marker"] == "goby-client-backup-source-m3e-v1" and isinstance(manifest["files"], dict),
            "The explicit source manifest is not an exact recorded client backup source snapshot.")
    for name, digest in manifest["files"].items():
        require(isinstance(name, str) and not name.startswith("/") and "\\" not in name and "\x00" not in name and
                all(part not in ("", ".", "..") for part in name.split("/")) and
                isinstance(digest, str) and re.fullmatch(r"[0-9a-f]{64}", digest),
                "The source manifest contains an invalid path or digest.")
    return manifest


def schema25_binding(state):
    binding = state.get("schema25_source")
    if binding is None and state.get("phase") == "upgrading" and state.get("upgrade", {}).get("to_schema") == 25:
        binding = state["upgrade"].get("schema_artifacts", {}).get("schema25_binding")
    require(isinstance(binding, dict) and set(binding) == {"marker", "schema", "source", "source_manifest_sha256",
            "catalog_sha256", "migration_25_sha256"} and binding["marker"] == SCHEMA_25_SOURCE_MARKER and
            type(binding["schema"]) is int and binding["schema"] == 25 and isinstance(binding["source"], str) and
            all(isinstance(binding[key], str) and re.fullmatch(r"[0-9a-f]{64}", binding[key])
                for key in ("source_manifest_sha256", "catalog_sha256", "migration_25_sha256")),
            "Schema25 requires its exact explicitly bound source and catalog receipt.")
    return binding


def schema26_binding(state):
    binding = state.get("schema26_source")
    if binding is None and state.get("phase") == "upgrading" and state.get("upgrade", {}).get("to_schema") == 26:
        binding = state["upgrade"].get("schema_artifacts", {}).get("schema26_binding")
    require(isinstance(binding, dict) and set(binding) == {"marker", "schema", "source", "source_manifest_sha256",
            "catalog_sha256", "migration_26_sha256"} and binding["marker"] == SCHEMA_26_SOURCE_MARKER and
            type(binding["schema"]) is int and binding["schema"] == 26 and isinstance(binding["source"], str) and
            all(isinstance(binding[key], str) and re.fullmatch(r"[0-9a-f]{64}", binding[key])
                for key in ("source_manifest_sha256", "catalog_sha256", "migration_26_sha256")) and
            binding["catalog_sha256"] == CATALOG_26_SHA and binding["migration_26_sha256"] == MIGRATION_26_SHA,
            "Schema26 requires the exact reviewed source, catalog, and migration binding.")
    return binding


def verify_source_schema_membership(source, baseline, manifest=None):
    version = baseline["version"]
    names = {row["name"] for row in baseline["migrations"]}
    directory_path = source / "internal/database/migrations"
    require({path.name for path in directory_path.glob("*.sql")} == names,
            "The source migration membership differs from the explicitly selected schema.")
    expected_catalogs = {f"schema-{number}-postgresql-17.json" for number in range(23, version + 1)}
    require({path.name for path in (source / "internal/backuppg/catalogs").glob("*.json")} == expected_catalogs,
            "The source catalog membership differs from the explicitly selected schema.")
    for number in range(23, min(version, 25) + 1):
        relative = f"internal/backuppg/catalogs/schema-{number}-postgresql-17.json"
        expected = CATALOG_25_SHA if number == 25 else SCHEMA_ARTIFACTS[number][1]
        require(sha(source / relative, mode=0o644) == expected and
                (manifest is None or manifest["files"].get(relative) == expected),
                "A historical source catalog differs from its immutable identity.")
    for migration in baseline["migrations"]:
        relative = "internal/database/migrations/" + migration["name"]
        require(sha(source / relative, mode=0o644) == migration["sha256"] and
                (manifest is None or manifest["files"].get(relative) == migration["sha256"]),
                "A source migration is missing, changed, or absent from the explicit manifest.")


def validate_schema25_delta(previous, baseline):
    require(equal_json(previous["migrations"], baseline["migrations"][:24]),
            "Schema25 changed an immutable historical migration identity.")
    old_tables = {table["Name"]: table for table in previous["catalog"]["Tables"]}
    new_tables = {table["Name"]: table for table in baseline["catalog"]["Tables"]}
    require(set(old_tables) == set(new_tables), "Schema25 changed the owned table inventory.")
    for name, table in old_tables.items():
        expected = dict(table)
        if name == "item_metadata_state":
            expected["Columns"] = table["Columns"] + ["music_source"]
        elif name == "item_entities":
            expected["Columns"] = table["Columns"] + ["credit_group"]
            expected["PrimaryKey"] = ["item_id", "entity_id", "credit_group", "position"]
            expected["SortKey"] = expected["PrimaryKey"]
        require(equal_json(new_tables[name], expected), "Schema25 changed an unowned table descriptor.")
    require(equal_json(previous["catalog"]["Sequences"], baseline["catalog"]["Sequences"]) and
            equal_json(previous["catalog"]["Constraints"], baseline["catalog"]["Constraints"]),
            "Schema25 changed an unowned sequence or foreign-key descriptor.")
    old = {(row["kind"], row["name"]): row["value"] for row in previous["objects"]}
    new = {(row["kind"], row["name"]): row["value"] for row in baseline["objects"]}
    changed = {
        ("constraint", "catalog_entities.catalog_entities_kind_check"),
        ("constraint", "item_entities.item_entities_pkey"),
        ("index", "item_entities_pkey"),
        ("column", "item_entities_pkey.00003"),
        ("function", "sync_catalog_item_entities(p_item_id text, p_metadata jsonb)"),
    }
    added = {("column", "item_metadata_state.00011"), ("column", "item_entities.00008"),
             ("column", "item_entities_pkey.00004"),
             ("constraint", "item_metadata_state.item_metadata_state_music_source_check"),
             ("constraint", "item_entities.item_entities_credit_group_check")}
    require(set(new) - set(old) == added and not set(old) - set(new) and changed <= set(old),
            "Schema25 changed the bounded catalog object membership.")
    require(all(key in changed or equal_json(value, new[key]) for key, value in old.items()),
            "Schema25 changed an unowned catalog object.")
    for key in changed:
        allowed = {"source"} if key[0] == "function" else {"definition"} if key[0] == "constraint" else \
            {"key_count", "options", "operator_classes", "collations", "columns"} if key[0] == "index" else {"name", "type"}
        require(equal_json({name: value for name, value in old[key].items() if name not in allowed},
                           {name: value for name, value in new[key].items() if name not in allowed}),
                "Schema25 changed unowned attributes of an updated catalog object.")
    require(equal_json(new[("column", "item_entities_pkey.00004")], old[("column", "item_entities_pkey.00003")]),
            "The rebuilt primary key did not preserve the original position column facts.")
    music = new[("column", "item_metadata_state.00011")]
    require(music.get("name") == "music_source" and music.get("type") == "jsonb" and music.get("not_null") is True and
            music.get("default") == "'{}'::jsonb" and music.get("generated") == "" and music.get("identity") == "" and
            music.get("dropped") is False, "Schema25 music_source is not the exact stored object column.")
    group = new[("column", "item_entities.00008")]
    require(group.get("name") == "credit_group" and group.get("type") == "smallint" and group.get("not_null") is True and
            group.get("default") in ("0", "0::smallint", "'0'::smallint") and group.get("generated") == "" and
            group.get("identity") == "" and group.get("dropped") is False,
            "Schema25 credit_group is not the exact bounded stored role discriminator.")
    group_check = new[("constraint", "item_entities.item_entities_credit_group_check")].get("definition", "")
    require(re.sub(r"\s+", "", group_check) == "CHECK(credit_group=ANY(ARRAY[0,1,2]))" and
            new[("constraint", "item_metadata_state.item_metadata_state_music_source_check")].get("definition") ==
            "CHECK (jsonb_typeof(music_source) = 'object'::text)" and
            new[("constraint", "item_entities.item_entities_pkey")].get("definition") ==
            'PRIMARY KEY (item_id, entity_id, credit_group, "position")',
            "Schema25 column validation or relationship primary key differs from the reviewed DDL.")
    require(new[("constraint", "catalog_entities.catalog_entities_kind_check")].get("definition") ==
            "CHECK (kind = ANY (ARRAY['Genre'::text, 'Tag'::text, 'Studio'::text, 'Person'::text, 'MusicArtist'::text]))",
            "Schema25 changed the catalog entity kinds beyond the reviewed music artist kind.")


def validate_schema26_delta(previous, baseline):
    require(type(previous.get("version")) is int and previous["version"] == 25 and
            type(baseline.get("version")) is int and baseline["version"] == 26 and
            all(type(row.get("version")) is int for row in baseline["migrations"]) and
            equal_json(previous["migrations"], baseline["migrations"][:25]) and
            baseline["migrations"][25:] == [{"version": 26, "name": MIGRATION_26_NAME, "sha256": MIGRATION_26_SHA}],
            "Schema26 changed historical migrations or its frozen Theme migration.")
    old_tables = {row["Name"]: row for row in previous["catalog"]["Tables"]}
    new_tables = {row["Name"]: row for row in baseline["catalog"]["Tables"]}
    require(len(old_tables) == 30 and len(new_tables) == 33 and set(new_tables) - set(old_tables) == SCHEMA_26_TABLES and
            set(old_tables) <= set(new_tables) and all(equal_json(row, new_tables[name]) for name, row in old_tables.items()),
            "Schema26 changed a historical table descriptor or added an unowned table.")
    for name, columns, key in (
            ("theme_owner_ids", ["id", "item_id", "virtual_root"], ["id"]),
            ("theme_reserved_paths", ["root_id", "relative_path", "is_directory"], ["root_id", "relative_path"]),
            ("item_theme_resources", ["resource_item_id", "owner_item_id", "kind", "active"], ["resource_item_id"])):
        require(equal_json(new_tables[name], {"Name": name, "Columns": columns, "PrimaryKey": key, "SortKey": key}),
                "A Theme table's complete descriptor changed.")
    expected_sequence = {"Name": SCHEMA_26_SEQUENCE, "Table": "theme_owner_ids", "Column": "id", "MinValue": 1,
                         "MaxValue": 9223372036854775807, "Increment": 1,
                         "Consumers": [{"Table": "theme_owner_ids", "Column": "id"}]}
    sequences = baseline["catalog"]["Sequences"]
    require(equal_json([row for row in sequences if row["Name"] != SCHEMA_26_SEQUENCE], previous["catalog"]["Sequences"]) and
            [row for row in sequences if row["Name"] == SCHEMA_26_SEQUENCE] == [expected_sequence],
            "Schema26 changed an old sequence or its independent owner sequence.")
    constraints = baseline["catalog"]["Constraints"]
    expected_foreign = [
        {"Table": table, "Name": name, "Definition": f"FOREIGN KEY ({column}) REFERENCES {parent}(id) ON DELETE CASCADE"}
        for table, name, column, parent in (
            ("item_theme_resources", "item_theme_resources_owner_item_id_fkey", "owner_item_id", "items"),
            ("item_theme_resources", "item_theme_resources_resource_item_id_fkey", "resource_item_id", "items"),
            ("theme_owner_ids", "theme_owner_ids_item_id_fkey", "item_id", "items"),
            ("theme_reserved_paths", "theme_reserved_paths_root_id_fkey", "root_id", "library_roots"))]
    require(equal_json([row for row in constraints if row["Table"] not in SCHEMA_26_TABLES], previous["catalog"]["Constraints"]) and
            equal_json([row for row in constraints if row["Table"] in SCHEMA_26_TABLES], expected_foreign),
            "Schema26 changed historical foreign keys or Theme ownership cascades.")
    old = {(row["kind"], row["name"]): row["value"] for row in previous["objects"]}
    new = {(row["kind"], row["name"]): row["value"] for row in baseline["objects"]}
    relation_columns = {"item_theme_resources": 4, "item_theme_resources_owner_idx": 3, "item_theme_resources_pkey": 1,
                        "theme_owner_ids": 3, SCHEMA_26_SEQUENCE: 3, "theme_owner_ids_item_id_key": 1,
                        "theme_owner_ids_pkey": 1, "theme_owner_ids_virtual_root_idx": 1,
                        "theme_reserved_paths": 3, "theme_reserved_paths_pkey": 2}
    constraint_names = {
        "item_theme_resources": ("distinct_owner_check", "kind_check", "owner_item_id_fkey", "pkey", "resource_item_id_fkey"),
        "theme_owner_ids": ("id_check", "item_id_fkey", "item_id_key", "kind_check", "pkey"),
        "theme_reserved_paths": ("canonical_check", "pkey", "root_id_fkey")}
    added = {("relation", name) for name in relation_columns}
    added |= {("index", name) for name in relation_columns if name not in SCHEMA_26_TABLES and name != SCHEMA_26_SEQUENCE}
    added |= {("column", f"{name}.{number:05d}") for name, count in relation_columns.items() for number in range(1, count + 1)}
    added |= {("constraint", f"{table}.{table}_{suffix}") for table, suffixes in constraint_names.items() for suffix in suffixes}
    added |= {("sequence", SCHEMA_26_SEQUENCE), ("function", "assign_theme_owner_id()"), ("trigger", "items.items_assign_theme_owner_id")}
    require(set(new) - set(old) == added and not set(old) - set(new) and
            all(equal_json(value, new[key]) for key, value in old.items()),
            "Schema26 changed an old object or introduced an unowned catalog object.")
    normalized = canonical_json(baseline["objects"]).decode().replace("\u2028", "\\u2028").replace("\u2029", "\\u2029").encode()
    require(baseline["catalog"]["SHA256"] == CATALOG_26_OBJECTS_SHA and
            hashlib.sha256(normalized).hexdigest() == CATALOG_26_OBJECTS_SHA,
            "Theme object definitions differ from the actually generated, reviewed catalog26.")


def trusted_schema_baseline(version, binding=None):
    require(type(version) is int and version in SCHEMA_TABLE_COUNTS, "The requested schema has no trusted catalog.")
    manifest = None
    if version in (25, 26):
        binding = schema25_binding({"schema25_source": binding}) if version == 25 else schema26_binding({"schema26_source": binding})
        source = Path(binding["source"])
        manifest = source_manifest(source, binding["source_manifest_sha256"])
        relative = f"internal/backuppg/catalogs/schema-{version}-postgresql-17.json"
        path, expected = source / relative, binding["catalog_sha256"]
        migration_name = MIGRATION_25_NAME if version == 25 else MIGRATION_26_NAME
        require(manifest["files"].get(relative) == expected and
                expected == (CATALOG_25_SHA if version == 25 else CATALOG_26_SHA) and
                manifest["files"].get("internal/database/migrations/" + migration_name) == binding[f"migration_{version}_sha256"],
                "The manifest does not bind the requested catalog and migration.")
    else:
        path, expected = SCHEMA_ARTIFACTS[version]
    content = read(path, mode=0o644)
    require(hashlib.sha256(content).hexdigest() == expected, "A trusted schema catalog file changed.")
    baseline = precise_json(content)
    require(isinstance(baseline, dict) and set(baseline) == {"version", "postgresql_major", "migrations", "catalog", "objects"} and
            type(baseline.get("version")) is int and baseline["version"] == version and
            type(baseline.get("postgresql_major")) is int and baseline["postgresql_major"] == 17 and
            isinstance(baseline.get("catalog"), dict) and
            set(baseline["catalog"]) == {"Schema", "Tables", "Sequences", "SHA256", "Constraints"} and
            baseline["catalog"]["Schema"] == "" and isinstance(baseline["catalog"]["SHA256"], str) and
            re.fullmatch(r"[0-9a-f]{64}", baseline["catalog"]["SHA256"]) and
            isinstance(baseline["catalog"]["Sequences"], list) and isinstance(baseline["catalog"]["Constraints"], list) and
            isinstance(baseline["catalog"].get("Tables"), list) and
            len(baseline["catalog"]["Tables"]) == SCHEMA_TABLE_COUNTS[version] and
            isinstance(baseline.get("objects"), list), "The trusted catalog has an unexpected schema identity.")
    history = baseline.get("migrations", [])
    require(isinstance(history, list) and len(history) == version and all(isinstance(row, dict) and set(row) == {"version", "name", "sha256"} and
            type(row["version"]) is int and isinstance(row["name"], str) and
            re.fullmatch(r"[0-9]{4}_[a-z0-9_]+\.sql", row["name"]) and
            isinstance(row["sha256"], str) and re.fullmatch(r"[0-9a-f]{64}", row["sha256"]) for row in history) and
            [row.get("version") for row in history] == list(range(1, version + 1)),
            "The trusted migration history is incomplete.")
    if version >= 24:
        require(history[23] == {"version": 24, "name": MIGRATION_24_NAME, "sha256": MIGRATION_24_SHA},
                "The trusted preference migration identity changed.")
    for table in baseline["catalog"]["Tables"]:
        require(isinstance(table, dict) and set(table) == {"Name", "Columns", "PrimaryKey", "SortKey"} and
                isinstance(table["Name"], str) and re.fullmatch(r"[a-z][a-z0-9_]{0,62}", table["Name"]) and
                isinstance(table["Columns"], list) and table["Columns"] and
                len(table["Columns"]) == len(set(table["Columns"])) and all(isinstance(column, str) and re.fullmatch(r"[a-z][a-z0-9_]{0,62}", column)
                    for column in table["Columns"]), "A trusted table descriptor is malformed.")
    require(all(isinstance(row, dict) and set(row) == {"kind", "name", "value"} and
                isinstance(row["kind"], str) and row["kind"] in CATALOG_OBJECT_FIELDS and
                isinstance(row["name"], str) and isinstance(row["value"], dict) and
                set(row["value"]) == CATALOG_OBJECT_FIELDS[row["kind"]]
                for row in baseline["objects"]) and
            len({(row["kind"], row["name"]) for row in baseline["objects"]}) == len(baseline["objects"]),
            "A trusted catalog object is malformed or duplicated.")
    if version == 25:
        require(binding["migration_25_sha256"] == MIGRATION_25_SHA and
                history[24] == {"version": 25, "name": MIGRATION_25_NAME, "sha256": MIGRATION_25_SHA},
                "The music migration differs from its explicit source identity.")
        # Match Go's sorted-map JSON normalization, including its mandatory
        # JavaScript line-separator escapes, without converting exact numbers.
        normalized = canonical_json(baseline["objects"]).decode().replace("\u2028", "\\u2028").replace("\u2029", "\\u2029").encode()
        require(hashlib.sha256(normalized).hexdigest() == baseline["catalog"]["SHA256"],
                "The schema25 catalog object fingerprint differs from its complete definitions.")
        validate_schema25_delta(trusted_schema_baseline(24), baseline)
        verify_source_schema_membership(source, baseline, manifest)
    elif version == 26:
        previous_raw = read(source / "internal/backuppg/catalogs/schema-25-postgresql-17.json", mode=0o644)
        require(hashlib.sha256(previous_raw).hexdigest() == CATALOG_25_SHA,
                "The schema26 source does not retain the immutable schema25 catalog.")
        validate_schema26_delta(precise_json(previous_raw), baseline)
        verify_source_schema_membership(source, baseline, manifest)
    return baseline


def verify_schema_upgrade_sources(path, target, source=None, source_manifest_sha256=None, schema25_catalog_sha256=None,
                                  schema26_catalog_sha256=None):
    require(type(target) is int and target in SCHEMA_TABLE_COUNTS, "The requested source schema is unsupported or untyped.")
    require((source is None) == (source_manifest_sha256 is None), "An explicit source snapshot requires its manifest SHA-256.")
    require(target == 25 or schema25_catalog_sha256 is None, "Only schema25 accepts a new explicit catalog digest.")
    require(target == 26 or schema26_catalog_sha256 is None, "Only schema26 accepts its explicit catalog digest.")
    if target == 26:
        require(source is not None and schema26_catalog_sha256 == CATALOG_26_SHA,
                "Schema26 requires its actually generated catalog and an explicit source manifest.")
        manifest = source_manifest(source, source_manifest_sha256)
        binding = {"marker": SCHEMA_26_SOURCE_MARKER, "schema": 26, "source": str(source),
                   "source_manifest_sha256": source_manifest_sha256, "catalog_sha256": schema26_catalog_sha256,
                   "migration_26_sha256": manifest["files"].get("internal/database/migrations/" + MIGRATION_26_NAME)}
        trusted_schema_baseline(26, binding)
        return {"source": str(source), "catalog_sha256": schema26_catalog_sha256,
                "source_manifest_sha256": source_manifest_sha256, "migration_count": 26,
                "migration_24_sha256": MIGRATION_24_SHA, "migration_25_sha256": MIGRATION_25_SHA,
                "schema26_binding": binding}
    if target == 25:
        require(source is not None and isinstance(schema25_catalog_sha256, str) and
                re.fullmatch(r"[0-9a-f]{64}", schema25_catalog_sha256),
                "Schema25 requires an explicit trusted catalog digest and source manifest binding.")
        manifest = source_manifest(source, source_manifest_sha256)
        binding = {"marker": SCHEMA_25_SOURCE_MARKER, "schema": 25, "source": str(source),
                   "source_manifest_sha256": source_manifest_sha256, "catalog_sha256": schema25_catalog_sha256,
                   "migration_25_sha256": manifest["files"].get("internal/database/migrations/" + MIGRATION_25_NAME)}
        trusted_schema_baseline(25, binding)
        return {"source": str(source), "catalog_sha256": schema25_catalog_sha256,
                "source_manifest_sha256": source_manifest_sha256, "migration_count": 25,
                "migration_24_sha256": MIGRATION_24_SHA, "schema25_binding": binding}
    baseline = trusted_schema_baseline(target)
    manifest = None
    if source is None:
        roots = [parent for parent in path.parents if parent != WORK and WORK in parent.parents and
                 exists(parent / "go.mod") and exists(parent / "internal/database/migrations")]
        require(len(roots) == 1, "The upgrade binary requires an explicit protected source snapshot.")
        source = roots[0]
    else:
        manifest = source_manifest(source, source_manifest_sha256)
    catalog = source / "internal/backuppg/catalogs" / f"schema-{target}-postgresql-17.json"
    require(sha(catalog, mode=0o644) == SCHEMA_ARTIFACTS[target][1],
            "The candidate source catalog differs from the trusted schema catalog.")
    if manifest is not None:
        require(manifest["files"].get(str(catalog.relative_to(source))) == SCHEMA_ARTIFACTS[target][1],
                "The supplied source manifest does not attest the trusted catalog input.")
    verify_source_schema_membership(source, baseline, manifest)
    return {"source": str(source), "catalog_sha256": SCHEMA_ARTIFACTS[target][1],
            "source_manifest_sha256": source_manifest_sha256, "migration_count": target,
            "migration_24_sha256": MIGRATION_24_SHA if target == 24 else None}


def verify_complete_product_source(source, manifest_sha256):
    require(isinstance(source, Path) and source.parent == WORK and
            re.fullmatch(r"source-attempt-([1-9][0-9]*)", source.name) is not None and
            int(source.name.rsplit("-", 1)[1]) > 22,
            "Schema26 product upgrades cannot use historical, catalog-only source21, or database-only source22 inputs.")
    directory(source, 0, 0o700, 0)
    manifest = source_manifest(source, manifest_sha256)
    actual = {}
    for path in source.rglob("*"):
        before = canonical(path)
        require(before.st_uid == 0 and before.st_gid == 0 and not before.st_mode & 0o022,
                "A complete product source member allows an untrusted write.")
        if stat.S_ISDIR(before.st_mode):
            continue
        require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1,
                "The complete product source contains a special file or hard link.")
        digest = hashlib.sha256()
        with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
            opened = os.fstat(stream.fileno())
            require((opened.st_dev, opened.st_ino) == (before.st_dev, before.st_ino), "A source member changed before hashing.")
            while chunk := stream.read(1 << 20):
                digest.update(chunk)
        after = canonical(path)
        require((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) ==
                (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns),
                "A complete source member changed while it was hashed.")
        relative = str(path.relative_to(source))
        if relative != "backup-source-inputs.json":
            actual[relative] = digest.hexdigest()
    require(actual == manifest["files"] and len(actual) > 100 and
            {"go.mod", "go.sum", "cmd/goby/main.go", "internal/database/theme_visibility.go",
             "internal/database/migrations/" + MIGRATION_26_NAME,
             "internal/backuppg/catalogs/schema-26-postgresql-17.json"} <= set(actual) and
            sha(source / "backup-source-inputs.json", mode=0o644) == manifest_sha256,
            "The complete product source bytes, membership, or manifest changed.")


def verify_product_upgrade(path, expected_sha, source, manifest_sha256, full_report, full_report_sha256):
    require(isinstance(full_report, Path) and full_report.name == "report.json" and full_report.parent.parent == WORK and
            re.fullmatch(r"client-backup-run-[0-9]{8}_[0-9]{6}_[0-9a-f]{12}", full_report.parent.name) is not None and
            isinstance(full_report_sha256, str) and re.fullmatch(r"[0-9a-f]{64}", full_report_sha256),
            "Schema26 upgrade requires an explicitly pinned complete verification report.")
    directory(full_report.parent, 0, 0o700, 0)
    require(canonical(full_report).st_gid == 0, "The complete verification report has a different group owner.")
    raw = read(full_report)
    require(hashlib.sha256(raw).hexdigest() == full_report_sha256, "The complete verification report changed.")
    report = precise_json(raw)
    require(isinstance(report, dict) and report.get("marker") == "goby-client-backup-pair-m3e-v1" and
            report.get("status") == "passed" and report.get("mode") == "full" and type(report.get("schema")) is int and
            report["schema"] == 26 and report.get("catalog_sha256") == CATALOG_26_SHA and report.get("source") == str(source) and
            report.get("source_manifest_sha256") == manifest_sha256 and type(report.get("unit_exit")) is int and
            report["unit_exit"] == 0 and report.get("run_id") == full_report.parent.name.removeprefix("client-backup-run-"),
            "The report is not a successful complete schema26 run for this exact product source.")
    cleanup = report.get("cleanup")
    required_cleanup = {"unit_terminal", "hba_restored_exactly", "goby_backup_m3e_source_removed",
                        "goby_backup_m3e_target_removed", "preexisting_catalog_unchanged", "receipt_saved"}
    require(isinstance(cleanup, dict) and required_cleanup <= set(cleanup) and all(value is True for value in cleanup.values()),
            "Complete verification did not finish its unit, pair, HBA, and original-catalog cleanup.")
    tests, packages = report.get("tests"), report.get("packages")
    require(isinstance(tests, dict) and type(tests.get("failures")) is int and tests["failures"] == 0 and
            type(tests.get("skips")) is int and tests["skips"] == 0 and type(tests.get("top_level_passes")) is int and
            isinstance(tests.get("passed"), list) and all(isinstance(name, str) and name for name in tests["passed"]) and
            tests["top_level_passes"] == len(tests["passed"]) == len(set(tests["passed"])) and
            PRODUCT_REQUIRED_TESTS <= set(tests["passed"]) and isinstance(packages, list) and
            all(isinstance(name, str) for name in packages) and len(set(packages)) == len(packages) and set(packages) == PRODUCT_PACKAGES,
            "The report lacks complete package coverage or reports skipped, failed, or empty tests.")
    binary = report.get("binary")
    expected_binary = full_report.parent / "tmp/goby-linux-amd64"
    require(isinstance(binary, dict) and set(binary) == {"path", "sha256", "bytes"} and binary["path"] == str(expected_binary) and
            binary["sha256"] == expected_sha and type(binary["bytes"]) is int and binary["bytes"] > 1 << 20,
            "The upgrade binary is not the final artifact of complete verification.")
    built, _ = verify_upgrade_input(expected_binary, expected_sha)
    require(len(built) == binary["bytes"], "The verified binary size changed.")
    verify_upgrade_input(path, expected_sha)
    verify_complete_product_source(source, manifest_sha256)
    require(sha(full_report) == full_report_sha256, "The complete verification report changed during input validation.")
    return {"report_path": str(full_report), "report_sha256": full_report_sha256}


def upgrade_operator_proof():
    path = WORK / "prepare-client-fixture.py"
    require(Path(__file__).absolute() == path, "Schema26 upgrades must use the fixed reviewed candidate operator path.")
    mode = stat.S_IMODE(canonical(path).st_mode)
    require(mode in (0o600, 0o644, 0o700, 0o755), "The candidate operator has an unexpected source mode.")
    return {"path": str(path), "sha256": sha(path, mode=mode)}


def baseline_table_columns(baseline):
    # Backup restore descriptors intentionally omit generated columns. A
    # preservation snapshot must compare their stored/computed values too, so
    # obtain the complete ordinal list from the trusted column catalog facts.
    result = {}
    for table in baseline["catalog"]["Tables"]:
        prefix = table["Name"] + "."
        facts = sorted((row for row in baseline["objects"] if row["kind"] == "column" and row["name"].startswith(prefix)
                        and not row["value"]["dropped"]), key=lambda row: row["name"])
        columns = [row["value"]["name"] for row in facts]
        require(columns and len(columns) == len(set(columns)) and set(table["Columns"]).issubset(columns),
                "The trusted full column facts do not cover every restore column.")
        result[table["Name"]] = columns
    return result


def full_database_snapshot(state):
    verify_database(state)
    # psql's generated statements contain only this fixed SELECT template and
    # catalog identifiers escaped by PostgreSQL format(%I/%L). All catalog and
    # table reads share one read-only REPEATABLE READ transaction. No table,
    # field, row, authentication session, audit entry, or task is omitted.
    sql = """BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL search_path=pg_catalog,public;
SET LOCAL statement_timeout='20s';
SELECT jsonb_build_object('section','metadata','value',jsonb_build_object(
 'captured_at',clock_timestamp(),'server_version_num',current_setting('server_version_num')::integer,
 'database',current_database(),
 'schemas',(SELECT jsonb_agg(n.nspname ORDER BY n.nspname COLLATE "C") FROM pg_namespace n
            WHERE n.nspname !~ '^pg_' AND n.nspname<>'information_schema'),
 'public_schema',(SELECT jsonb_build_object('oid',oid::bigint,'owner',pg_get_userbyid(nspowner),'acl',nspacl)
                  FROM pg_namespace WHERE nspname='public'),
 'columns',(SELECT jsonb_object_agg(c.relname,ARRAY(SELECT a.attname::text FROM pg_attribute a
              WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped ORDER BY a.attnum))
            FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='r'),
 'relations',(SELECT jsonb_object_agg(c.relname,jsonb_build_object('oid',c.oid::bigint,'owner',pg_get_userbyid(c.relowner),
               'acl',c.relacl,'column_acl',ARRAY(SELECT jsonb_build_object('name',a.attname,'acl',a.attacl)
                 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped AND a.attacl IS NOT NULL ORDER BY a.attnum)))
             FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public')));
SELECT jsonb_build_object('section','catalog','value',(""" + CATALOG_OBJECTS_SQL + """));
SELECT jsonb_build_object('section','unsupported','value',EXISTS(
 SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'
   AND (c.relkind NOT IN ('r','i','S') OR c.relpersistence<>'p' OR c.relispartition OR c.relrowsecurity OR c.relforcerowsecurity OR c.reloftype<>0)
 UNION ALL SELECT 1 FROM pg_inherits i JOIN pg_class c ON c.oid=i.inhrelid OR c.oid=i.inhparent JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'
 UNION ALL SELECT 1 FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname='public' AND t.typtype NOT IN ('c','b')
 UNION ALL SELECT 1 FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname='public' AND t.typtype='b' AND t.typelem=0
 UNION ALL SELECT 1 FROM pg_extension e JOIN pg_namespace n ON n.oid=e.extnamespace WHERE n.nspname='public'
 UNION ALL SELECT 1 FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'
 UNION ALL SELECT 1 FROM pg_rewrite r JOIN pg_class c ON c.oid=r.ev_class JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'
 UNION ALL SELECT 1 FROM pg_operator o JOIN pg_namespace n ON n.oid=o.oprnamespace WHERE n.nspname='public'
 UNION ALL SELECT 1 FROM pg_opclass o JOIN pg_namespace n ON n.oid=o.opcnamespace WHERE n.nspname='public'
 UNION ALL SELECT 1 FROM pg_opfamily o JOIN pg_namespace n ON n.oid=o.opfnamespace WHERE n.nspname='public'
 UNION ALL SELECT 1 FROM pg_collation c JOIN pg_namespace n ON n.oid=c.collnamespace WHERE n.nspname='public'
 UNION ALL SELECT 1 FROM pg_conversion c JOIN pg_namespace n ON n.oid=c.connamespace WHERE n.nspname='public'
 UNION ALL SELECT 1 FROM pg_ts_config c JOIN pg_namespace n ON n.oid=c.cfgnamespace WHERE n.nspname='public'
 UNION ALL SELECT 1 FROM pg_ts_dict c JOIN pg_namespace n ON n.oid=c.dictnamespace WHERE n.nspname='public'
 UNION ALL SELECT 1 FROM pg_ts_parser c JOIN pg_namespace n ON n.oid=c.prsnamespace WHERE n.nspname='public'
 UNION ALL SELECT 1 FROM pg_ts_template c JOIN pg_namespace n ON n.oid=c.tmplnamespace WHERE n.nspname='public'));
SELECT format($read$SELECT jsonb_build_object('section','table','name',%L,'value',
 COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text COLLATE "C"),'[]'::jsonb)) FROM public.%I t;$read$,c.relname,c.relname)
FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='r'
ORDER BY c.relname COLLATE "C"
\\gexec
SELECT format($read$SELECT jsonb_build_object('section','sequence','name',%L,'value',
 jsonb_build_object('last_value',last_value,'is_called',is_called)) FROM public.%I;$read$,c.relname,c.relname)
FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='S'
ORDER BY c.relname COLLATE "C"
\\gexec
COMMIT;
"""
    output = postgres(sql, ROLE)
    require(len(output.encode()) <= MAX_SNAPSHOT_BYTES, "The complete database snapshot exceeds its private evidence budget.")
    result = {"tables": {}, "sequences": {}}
    for line in output.split("\n"):
        row = precise_json(line)
        section = row.get("section")
        if section in ("metadata", "catalog", "unsupported"):
            require(section not in result and set(row) == {"section", "value"}, "A database snapshot section is ambiguous.")
            result[section] = row["value"]
        else:
            require(section in ("table", "sequence") and set(row) == {"section", "name", "value"},
                    "A database snapshot contains an unexpected section.")
            target = result["tables" if section == "table" else "sequences"]
            require(row["name"] not in target, "A database snapshot repeats an object.")
            target[row["name"]] = row["value"]
    require(all(key in result for key in ("metadata", "catalog", "unsupported")), "The complete database snapshot is missing its catalog identity.")
    return result


def canonical_theme_path(value):
    return (isinstance(value, str) and value != "" and "\\" not in value and "\x00" not in value and
            re.match(r"^[A-Za-z]:", value) is None and all(part not in ("", ".", "..") for part in value.split("/")))


def expected_theme_reserved_paths(tables):
    """Independently derive only the frozen migration's old-path reservations."""
    markers = {}
    fold = str.maketrans("ABCDEFGHIJKLMNOPQRSTUVWXYZ", "abcdefghijklmnopqrstuvwxyz")
    for item in tables["items"]:
        root, path = item.get("root_id"), item.get("relative_path")
        if root is None or not canonical_theme_path(path):
            continue
        require(isinstance(root, str) and type(item.get("is_folder")) is bool,
                "An old theme path has an untyped root or directory flag.")
        parts = path.split("/")
        for index, component in enumerate(parts):
            if (index < len(parts) - 1 or item["is_folder"]) and component.translate(fold) in ("theme-music", "backdrops"):
                markers[(root, "/".join(parts[:index + 1]))] = True
        if not item["is_folder"] and parts[-1].translate(fold) in {"theme." + extension for extension in THEME_AUDIO_EXTENSIONS}:
            markers.setdefault((root, path), False)
    return [{"root_id": root, "relative_path": path, "is_directory": directory}
            for (root, path), directory in sorted(markers.items())]


def validate_theme_state(tables):
    """Validate the complete snapshot without allocating or repairing records."""
    def index(table, key):
        rows = tables[table]
        require(isinstance(rows, list) and all(isinstance(row, dict) and isinstance(row.get(key), str) for row in rows),
                "A Theme reference table contains an untyped identity.")
        result = {row[key]: row for row in rows}
        require(len(result) == len(rows), "A Theme reference table contains duplicate identities.")
        return result
    items, roots, libraries = index("items", "id"), index("library_roots", "id"), index("libraries", "id")
    owner_rows, markers = tables["theme_owner_ids"], tables["theme_reserved_paths"]
    require(isinstance(owner_rows, list) and isinstance(markers, list), "Theme ownership or reservations are not complete arrays.")
    owner_ids, mapped, virtual_roots = set(), set(), 0
    for row in owner_rows:
        require(isinstance(row, dict) and set(row) == {"id", "item_id", "virtual_root"} and
                type(row["id"]) is int and 0 < row["id"] <= 9223372036854775807 and row["id"] not in owner_ids and
                type(row["virtual_root"]) is bool, "A Theme owner identity is invalid or duplicated.")
        owner_ids.add(row["id"])
        if row["virtual_root"]:
            require(row["item_id"] is None, "The virtual Theme root aliases a catalog item.")
            virtual_roots += 1
        else:
            require(isinstance(row["item_id"], str) and row["item_id"] in items and row["item_id"] not in mapped,
                    "A canonical item owner is missing, orphaned, or duplicated.")
            mapped.add(row["item_id"])
    require(virtual_roots == 1 and mapped == set(items) and len(owner_rows) == len(items) + 1,
            "Theme owners do not cover every item and exactly one virtual root.")
    marker_keys, by_root = set(), {}
    for marker in markers:
        require(isinstance(marker, dict) and set(marker) == {"root_id", "relative_path", "is_directory"} and
                isinstance(marker["root_id"], str) and marker["root_id"] in roots and
                canonical_theme_path(marker["relative_path"]) and type(marker["is_directory"]) is bool,
                "A Theme reservation has invalid path, kind, or root ownership.")
        key = marker["root_id"], marker["relative_path"]
        require(key not in marker_keys, "A Theme reservation has a duplicate root/path key.")
        marker_keys.add(key)
        by_root.setdefault(marker["root_id"], []).append(marker)
    resources = index("item_theme_resources", "resource_item_id")
    def reserved(item):
        path = item.get("relative_path")
        return isinstance(path, str) and any(path == marker["relative_path"] or
            (marker["is_directory"] and path.startswith(marker["relative_path"] + "/")) for marker in by_root.get(item.get("root_id"), []))
    for resource_id, link in resources.items():
        require(set(link) == {"resource_item_id", "owner_item_id", "kind", "active"} and
                isinstance(link["owner_item_id"], str) and link["owner_item_id"] in items and resource_id in items and
                resource_id != link["owner_item_id"] and link["kind"] in ("song", "video") and type(link["active"]) is bool,
                "A Theme classification has invalid identity, kind, or state.")
        resource, owner = items[resource_id], items[link["owner_item_id"]]
        root = roots.get(resource.get("root_id"))
        require(resource.get("is_folder") is False and resource.get("type") == {"song": "Audio", "video": "Video"}[link["kind"]] and
                resource.get("parent_id") == owner["id"] and isinstance(resource.get("library_id"), str) and
                resource["library_id"] in libraries and owner.get("library_id") == resource["library_id"] and
                root is not None and root.get("library_id") == resource["library_id"] and reserved(resource),
                "A Theme resource lost its media type, canonical parent, library, root, or reservation.")
        if link["active"]:
            same_root = owner.get("root_id") == resource["root_id"]
            library_owner = owner["id"] == resource["library_id"] and owner.get("type") == "CollectionFolder" and owner.get("is_folder") is True
            require((same_root or library_owner) and owner["id"] not in resources and not reserved(owner),
                    "An active Theme resource has an ineligible owner.")


def theme_summary(tables):
    owners, paths, resources = (tables[name] for name in ("theme_owner_ids", "theme_reserved_paths", "item_theme_resources"))
    digest = lambda rows: hashlib.sha256(canonical_json(sorted(rows, key=canonical_json))).hexdigest()
    return {"owner_count": len(owners), "virtual_root_count": sum(row["virtual_root"] is True for row in owners),
            "mapped_item_count": sum(row["virtual_root"] is False for row in owners),
            "reserved_path_count": len(paths), "resource_count": len(resources),
            "active_resource_count": sum(row["active"] is True for row in resources),
            "owner_rows_sha256": digest(owners), "reserved_paths_sha256": digest(paths), "resource_rows_sha256": digest(resources)}


def validate_database_snapshot(database, version, state):
    binding = schema26_binding(state) if version == 26 else schema25_binding(state) if version == 25 else None
    baseline = trusted_schema_baseline(version, binding)
    if version == 26:
        trusted_schema_baseline(25, schema25_binding(state))
    metadata = database.get("metadata", {})
    require(metadata.get("database") == ROLE and type(metadata.get("server_version_num")) is int and
            metadata["server_version_num"] // 10000 == 17 and metadata.get("schemas") == ["public"],
            "The full snapshot identifies another database, server major, or schema set.")
    require(equal_json(database.get("catalog"), baseline["objects"]), "The live catalog differs from the trusted schema catalog.")
    require(database.get("unsupported") is False, "The live schema contains an unsupported catalog object.")
    columns = baseline_table_columns(baseline)
    tables = database.get("tables", {})
    require(equal_json(metadata.get("columns"), columns) and set(tables) == set(columns),
            "The complete table or column inventory differs from the trusted schema.")
    for name, rows in tables.items():
        require(isinstance(rows, list) and all(isinstance(row, dict) and set(row) == set(columns[name]) for row in rows),
                "A complete table snapshot omitted or changed a column.")
    sequences = {sequence["Name"] for sequence in baseline["catalog"].get("Sequences", [])}
    require(set(database.get("sequences", {})) == sequences, "The live sequence inventory differs from the trusted schema.")
    history = sorted(tables["schema_migrations"], key=lambda row: row["version"])
    require([{key: row[key] for key in ("version", "name")} for row in history] ==
            [{key: row[key] for key in ("version", "name")} for row in baseline["migrations"]],
            "The full migration history differs from the trusted version and filename sequence.")
    additional = added_viewer_receipt(state)
    expected_users = {state["admin_id"], state["viewer_id"]}
    if additional:
        expected_users.add(additional["user_id"])
        added = [row for row in tables["users"] if row["id"] == additional["user_id"]]
        require(len(added) == 1 and added[0]["name"] == AV_NAME and added[0]["is_administrator"] is False and
                added[0]["is_disabled"] is False and added[0]["has_password"] is True,
                "The receipted AV account changed its fixed identity or authority.")
    require(len(tables["users"]) == len(expected_users) and {row["id"] for row in tables["users"]} == expected_users,
            "The full snapshot changed the fixture account identities.")
    require(any(row["key"] == "server_id" and row["value"] == state["server_id"] for row in tables["server_settings"]),
            "The full snapshot changed the stable server identity.")
    relation_names = {row["name"] for row in baseline["objects"] if row["kind"] == "relation"}
    require(set(metadata.get("relations", {})) == relation_names and
            all(row.get("owner") == ROLE for row in metadata["relations"].values()),
            "The live relation ownership inventory differs from the candidate role.")
    if version == 26:
        validate_theme_state(tables)
        for sequence in baseline["catalog"]["Sequences"]:
            value = database["sequences"][sequence["Name"]]
            require(isinstance(value, dict) and set(value) == {"last_value", "is_called"} and
                    type(value["last_value"]) is int and type(value["is_called"]) is bool and
                    sequence["MinValue"] <= value["last_value"] <= sequence["MaxValue"],
                    "A schema26 sequence has invalid state or precision.")
            next_value = value["last_value"] + (sequence["Increment"] if value["is_called"] else 0)
            require(next_value <= sequence["MaxValue"] and all(
                all(type(row[consumer["Column"]]) is int and row[consumer["Column"]] < next_value
                    for row in tables[consumer["Table"]]) for consumer in sequence["Consumers"]),
                    "A schema26 sequence may collide with a retained identifier.")


def preservation_snapshot(state, version=None):
    verify_fixture_directories(state)
    version = state["schema"] if version is None else version
    database = full_database_snapshot(state)
    return {"schema": version, "database": database, "recovery": recovery_snapshot(),
            "runtime_sha256": sha(RUNTIME), "browser_sha256": sha(BROWSER),
            "added_viewer_credentials": added_viewer_credentials(state)}


def validate_preservation_snapshot(snapshot, version, state):
    require(type(snapshot.get("schema")) is int and snapshot["schema"] == version, "The preservation record labels the wrong schema.")
    validate_database_snapshot(snapshot["database"], version, state)
    require(snapshot["runtime_sha256"] == state["runtime_sha256"] and snapshot["browser_sha256"] == state["browser_sha256"],
            "The preservation record contains changed private credentials.")
    require(equal_json(snapshot.get("added_viewer_credentials"), added_viewer_credentials(state)),
            "The preservation record contains changed additional-account credentials or receipt.")


def preservation_summary(snapshot):
    tables = snapshot["database"]["tables"]
    result = {"schema": snapshot["schema"], "database_sha256": hashlib.sha256(canonical_json(snapshot["database"])).hexdigest(),
            "recovery_sha256": hashlib.sha256(canonical_json(snapshot["recovery"])).hexdigest(),
            "runtime_sha256": snapshot["runtime_sha256"], "browser_sha256": snapshot["browser_sha256"],
            "table_count": len(tables), "row_counts": {name: len(rows) for name, rows in sorted(tables.items())},
            "item_count": len(tables.get("items", [])), "library_count": len(tables.get("libraries", [])),
            "added_viewer_credentials": snapshot.get("added_viewer_credentials")}
    if snapshot["schema"] == 26:
        result["theme"] = theme_summary(tables)
    return result


def compare_preservation_snapshots(before, after, source_schema, target_schema, state):
    require(target_schema_version({"schema": source_schema}, target_schema) == target_schema, "The schema comparison is unsupported.")
    validate_preservation_snapshot(before, source_schema, state)
    validate_preservation_snapshot(after, target_schema, state)
    require(all(equal_json(before[key], after[key]) for key in ("recovery", "runtime_sha256", "browser_sha256")),
            "Recovery state, the master key, or private credentials changed during upgrade.")
    require(equal_json(before.get("added_viewer_credentials"), after.get("added_viewer_credentials")),
            "The additional-account credentials or receipt changed during upgrade.")
    left, right = before["database"], after["database"]
    for key in ("database", "server_version_num", "schemas", "public_schema"):
        require(equal_json(left["metadata"][key], right["metadata"][key]), "A database or schema ownership fact changed during upgrade.")
    if (source_schema, target_schema) == (25, 26):
        require(set(right["sequences"]) - set(left["sequences"]) == {SCHEMA_26_SEQUENCE} and
                set(left["sequences"]) <= set(right["sequences"]) and
                all(equal_json(value, right["sequences"][name]) for name, value in left["sequences"].items()),
                "A Theme migration changed an old sequence or added an unowned sequence.")
    else:
        require(equal_json(left["sequences"], right["sequences"]), "An existing sequence state changed during upgrade.")
    for name, facts in left["metadata"]["relations"].items():
        actual = right["metadata"]["relations"].get(name)
        if (source_schema, target_schema) == (24, 25) and name == "item_entities_pkey":
            require(isinstance(actual, dict) and set(actual) == set(facts) and
                    type(facts.get("oid")) is int and facts["oid"] > 0 and
                    type(actual.get("oid")) is int and actual["oid"] > 0 and
                    equal_json({key: value for key, value in facts.items() if key != "oid"},
                               {key: value for key, value in actual.items() if key != "oid"}),
                    "The rebuilt music primary-key index changed privileges or lost its identity.")
        else:
            require(equal_json(facts, actual), "An existing relation identity or privilege changed during upgrade.")
    for name, columns in left["metadata"]["columns"].items():
        expected = columns + [SCHEMA_25_NEW_COLUMNS[name][0]] if (source_schema, target_schema) == (24, 25) and name in SCHEMA_25_NEW_COLUMNS else columns
        require(equal_json(expected, right["metadata"]["columns"].get(name)), "An existing table's column facts changed during upgrade.")
    for name, rows in left["tables"].items():
        if name == "schema_migrations" and source_schema != target_schema:
            new_rows = [row for row in right["tables"][name] if row["version"] != target_schema]
            require(equal_json(rows, new_rows), "An old migration history row changed during upgrade.")
        elif (source_schema, target_schema) == (24, 25) and name in SCHEMA_25_NEW_COLUMNS:
            updated = right["tables"][name]
            column, default = SCHEMA_25_NEW_COLUMNS[name]
            require(all(equal_json(row.get(column), default) for row in updated),
                    "Every migrated row must contain the exact new column default: " + column + ".")
            projected = [{key: value for key, value in row.items() if key != column} for row in updated]
            require(sorted(canonical_json(row) for row in rows) == sorted(canonical_json(row) for row in projected),
                    "A preexisting row or column changed while adding " + column + ".")
        else:
            require(equal_json(rows, right["tables"].get(name)), "An existing table changed during upgrade: " + name + ".")
    if source_schema == target_schema:
        require(equal_json(left["catalog"], right["catalog"]) and set(left["metadata"]["relations"]) == set(right["metadata"]["relations"]),
                "A same-schema binary upgrade changed the catalog.")
        return
    if (source_schema, target_schema) == (23, 24):
        require(set(right["tables"]) - set(left["tables"]) == {"user_settings"} and right["tables"]["user_settings"] == [],
                "The preference migration must add only an empty user_settings table.")
        original_objects = {(row["kind"], row["name"]): row["value"] for row in left["catalog"]}
        updated_objects = {(row["kind"], row["name"]): row["value"] for row in right["catalog"]}
        require(all(equal_json(value, updated_objects.get(key)) for key, value in original_objects.items()),
                "The preference migration changed a preexisting catalog object.")
        added_relations = set(right["metadata"]["relations"]) - set(left["metadata"]["relations"])
        require(added_relations == {"user_settings", "user_settings_pkey"} and all(
            right["metadata"]["relations"][name]["acl"] is None and right["metadata"]["relations"][name]["column_acl"] == []
            for name in added_relations), "The preference migration added an unexpected relation or explicit privilege.")
    elif (source_schema, target_schema) == (25, 26):
        require(set(right["tables"]) - set(left["tables"]) == SCHEMA_26_TABLES and set(left["tables"]) <= set(right["tables"]),
                "The Theme migration changed the complete table inventory.")
        validate_schema26_delta(trusted_schema_baseline(25, schema25_binding(state)),
                                trusted_schema_baseline(26, schema26_binding(state)))
        expected_markers = expected_theme_reserved_paths(left["tables"])
        require(sorted(canonical_json(row) for row in right["tables"]["theme_reserved_paths"]) ==
                sorted(canonical_json(row) for row in expected_markers) and right["tables"]["item_theme_resources"] == [],
                "Theme migration reservations differ from old paths or infer unaccepted resources.")
        added_relations = set(right["metadata"]["relations"]) - set(left["metadata"]["relations"])
        baseline = trusted_schema_baseline(26, schema26_binding(state))
        expected_relations = {row["name"] for row in baseline["objects"] if row["kind"] == "relation"} - set(left["metadata"]["relations"])
        require(added_relations == expected_relations and all(
            type(right["metadata"]["relations"][name].get("oid")) is int and right["metadata"]["relations"][name]["oid"] > 0 and
            right["metadata"]["relations"][name].get("owner") == ROLE and right["metadata"]["relations"][name].get("acl") is None and
            right["metadata"]["relations"][name].get("column_acl") == [] for name in added_relations),
            "A new Theme relation has unowned identity or grants.")
    else:
        require((source_schema, target_schema) == (24, 25) and set(left["tables"]) == set(right["tables"]) and
                set(left["metadata"]["relations"]) == set(right["metadata"]["relations"]),
                "The music migration added or removed an unowned table or relation.")
    migration = next(row for row in right["tables"]["schema_migrations"] if row["version"] == target_schema)
    try:
        applied = dt.datetime.fromisoformat(migration["applied_at"])
        lower = dt.datetime.fromisoformat(left["metadata"]["captured_at"])
        upper = dt.datetime.fromisoformat(right["metadata"]["captured_at"])
        in_window = applied.tzinfo is not None and lower <= applied <= upper
    except (TypeError, ValueError):
        in_window = False
    require(in_window and migration["name"] == {24: MIGRATION_24_NAME, 25: MIGRATION_25_NAME, 26: MIGRATION_26_NAME}[target_schema],
            "The new migration history entry is outside the controlled upgrade window.")


def upgrade_phase(state, phase):
    state["upgrade"]["phase"] = phase
    state["stage"] = "upgrade_" + phase
    save_state(state)


def start_upgrade_candidate(state, binary_sha256, binary_identity):
    # Keep the last verified binary/PID/schema authoritative until the full
    # preservation and target-catalog checks succeed. In-progress observations
    # live under the upgrade record; they cannot masquerade as a ready fixture.
    candidate = json.loads(json.dumps(state))
    candidate.update(binary_sha256=binary_sha256, binary_identity=binary_identity, process=None, start_pending=True)
    require_candidate_stopped()
    verify_database(state)
    require(verify_service(candidate) is None, "The staged candidate is not in the expected stopped state.")
    command(["/usr/bin/systemctl", "start", UNIT], timeout=55)
    deadline = time.monotonic() + 50
    while time.monotonic() < deadline:
        if command(["/usr/bin/ss", "-H", "-ltnp", f"sport = :{PORT}"]):
            break
        require(properties().get("ActiveState") not in ("failed", "inactive"), "The staged candidate exited during startup.")
        time.sleep(0.5)
    observed = verify_service(candidate, allow_new=True)
    require(observed is not None, "The staged candidate did not establish its owned process.")
    state["upgrade"].update(new_process=observed, installed_binary_sha256=binary_sha256,
                            installed_binary_identity=binary_identity)
    save_state(state)
    candidate.update(process=observed, start_pending=False)
    return candidate


def upgrade_fixture(state, path, expected_sha, target_schema=None, source=None, source_manifest_sha256=None, schema25_catalog_sha256=None,
                    schema26_catalog_sha256=None, full_report=None, full_report_sha256=None):
    target_schema = target_schema_version(state, target_schema)
    source_schema = state["schema"]
    require(state.get("phase") == "ready" and not state.get("start_pending") and
            not state.get("credentials_pending") and not state.get("binary_pending") and not state.get("unit_pending") and
            ("upgrade" not in state or state["upgrade"].get("phase") == "complete"),
            "An incomplete or interrupted fixture operation must be inspected; no automatic restart or rollback is performed.")
    verify_fixture_directories(state)
    verify_database(state)
    original_process = verify_service(state)
    require(original_process is not None and original_process == state.get("process"),
            "Upgrade requires the currently recorded live candidate process.")
    api = NativeAPI(state)
    require(api.request("/readyz") == {"Status": "ready"} and
            api.request("/emby/System/Info/Public").get("Id") == state["server_id"],
            "The live candidate identity is not ready for a controlled upgrade.")
    content, input_identity = verify_upgrade_input(path, expected_sha)
    source_artifacts = verify_schema_upgrade_sources(path, target_schema, source, source_manifest_sha256, schema25_catalog_sha256,
                                                      schema26_catalog_sha256)
    if source_schema == target_schema == 25:
        require(source_artifacts["catalog_sha256"] == schema25_binding(state)["catalog_sha256"],
                "A same-schema upgrade cannot replace the accepted schema25 catalog identity.")
    if target_schema == 26:
        source_artifacts["product_verification"] = verify_product_upgrade(path, expected_sha, source, source_manifest_sha256,
                                                                          full_report, full_report_sha256)
        source_artifacts["operator"] = upgrade_operator_proof()
        # The earlier schema25 source remains a separate immutable historical
        # binding, including after later schema26-to26 binary replacements.
        trusted_schema_baseline(25, schema25_binding(state))
    else:
        require(full_report is None and full_report_sha256 is None, "Only schema26 upgrades accept a full-source report.")
    original_sha = state["binary_sha256"]
    require(expected_sha != original_sha, "The requested executable is already current; use inspect without restarting.")
    # Freeze every read-only preservation check before scheduling a stop. A
    # second snapshot after the stop captures any final in-flight state writes.
    live_snapshot = preservation_snapshot(state, source_schema)
    validate_preservation_snapshot(live_snapshot, source_schema, state)
    before = json.loads(json.dumps(state))
    nonce = secrets.token_hex(16)
    retained = WORK / ("client-upgrade-" + nonce)
    staged = INSTALL / (".goby-upgrade-" + nonce)
    require(not exists(retained) and not exists(staged), "A new upgrade evidence or staging path is already occupied.")
    state["phase"] = "upgrading"
    state["upgrade"] = {"id": nonce, "created_at": utc(), "phase": "creating_evidence", "source": str(path),
        "source_identity": input_identity, "from_sha256": original_sha, "to_sha256": expected_sha,
        "from_schema": source_schema, "to_schema": target_schema, "schema_artifacts": source_artifacts,
        "old_process": original_process, "old_binary_identity": identity(regular(BINARY, mode=0o755, limit=64 << 20)),
        "evidence_directory": str(retained), "staged_path": str(staged), "rollback_binary": str(retained / "rollback.bin")}
    save_state(state)
    retained.mkdir(mode=0o700)
    state["upgrade"]["evidence_identity"] = directory(retained, 0, 0o700, 0)
    create(retained / "before-state.json", before)
    create(retained / "rollback.bin", read(BINARY, mode=0o755, limit=64 << 20))
    require(sha(retained / "rollback.bin", limit=64 << 20) == original_sha,
            "The retained rollback executable differs from the recorded original.")
    create(staged, content, 0o755)
    require(sha(staged, mode=0o755, limit=64 << 20) == expected_sha, "The staged upgrade executable changed.")
    state["upgrade"]["staged_identity"] = identity(regular(staged, mode=0o755, limit=64 << 20))
    upgrade_phase(state, "staged")
    verify_fixture_directories(state)
    verify_database(state)
    require(verify_service(state) == original_process and media_snapshot() == state["media"],
            "The candidate or media changed immediately before its owned stop.")
    if target_schema == 26:
        require(verify_product_upgrade(path, expected_sha, source, source_manifest_sha256, full_report, full_report_sha256) ==
                source_artifacts["product_verification"] and upgrade_operator_proof() == source_artifacts["operator"],
                "The verified product or candidate operator changed before stop.")
    upgrade_phase(state, "stop_requested")
    # The unit has Restart=no, an exact private EnvironmentFile, no drop-ins,
    # and the just-verified PID/executable/start ticks. Stop only this unit.
    require(verify_service(state) == original_process, "The recorded candidate changed before stop dispatch.")
    command(["/usr/bin/systemctl", "stop", UNIT], timeout=45)
    require_candidate_stopped()
    upgrade_phase(state, "stopped")
    preserved = preservation_snapshot(state, source_schema)
    create(retained / "preservation-full.json", preserved)
    validate_preservation_snapshot(preserved, source_schema, state)
    state["upgrade"]["preservation"] = preservation_summary(preserved)
    create(retained / "preservation.json", state["upgrade"]["preservation"])
    require(identity(regular(BINARY, mode=0o755, limit=64 << 20)) == state["upgrade"]["old_binary_identity"] and
            sha(BINARY, mode=0o755, limit=64 << 20) == original_sha and
            identity(regular(staged, mode=0o755, limit=64 << 20)) == state["upgrade"]["staged_identity"] and
            sha(staged, mode=0o755, limit=64 << 20) == expected_sha,
            "An executable changed before atomic replacement.")
    upgrade_phase(state, "replace_requested")
    require_candidate_stopped()
    os.replace(staged, BINARY)
    sync(INSTALL)
    require(sha(BINARY, mode=0o755, limit=64 << 20) == expected_sha,
            "The atomically installed executable differs from the upgrade input.")
    installed_identity = identity(regular(BINARY, mode=0o755, limit=64 << 20))
    state["upgrade"].update(installed_binary_sha256=expected_sha, installed_binary_identity=installed_identity)
    upgrade_phase(state, "replaced")
    before_start = preservation_snapshot(state, source_schema)
    create(retained / "before-start-full.json", before_start)
    compare_preservation_snapshots(preserved, before_start, source_schema, source_schema, state)
    if target_schema == 26:
        require(verify_product_upgrade(path, expected_sha, source, source_manifest_sha256, full_report, full_report_sha256) ==
                source_artifacts["product_verification"] and upgrade_operator_proof() == source_artifacts["operator"],
                "The verified product or candidate operator changed before startup.")
    upgrade_phase(state, "start_requested")
    candidate = start_upgrade_candidate(state, expected_sha, installed_identity)
    upgrade_phase(state, "started")
    api = NativeAPI(candidate)
    require(api.request("/readyz") == {"Status": "ready"}, "The upgraded candidate is not ready.")
    information = api.request("/emby/System/Info/Public")
    require(information.get("Id") == state["server_id"] and information.get("ProductName") == "Goby",
            "The upgraded candidate changed its stable server or product identity.")
    after_start = preservation_snapshot(state, target_schema)
    create(retained / "after-start-full.json", after_start)
    compare_preservation_snapshots(preserved, after_start, source_schema, target_schema, state)
    require(media_snapshot() == state["media"], "The upgraded candidate changed the read-only media snapshot.")
    require(verify_service(candidate) == candidate["process"], "The upgraded candidate process changed during acceptance.")
    state["binary_sha256"], state["binary_identity"], state["process"] = expected_sha, installed_identity, candidate["process"]
    state["schema"] = target_schema
    if target_schema == 25:
        state["schema25_source"] = dict(source_artifacts["schema25_binding"])
    elif target_schema == 26:
        state["schema26_source"] = dict(source_artifacts["schema26_binding"])
    state["upgrade"].update(new_process=state["process"], completed_at=utc(), phase="complete",
                            protocol_version=information.get("Version"), product_version=information.get("GobyVersion"),
                            after_preservation=preservation_summary(after_start))
    state.setdefault("upgrade_history", []).append(dict(state["upgrade"]))
    create(retained / "completed.json", state["upgrade"])
    state["phase"], state["stage"] = "ready", "complete"
    save_state(state)


class NativeAPI:
    def __init__(self, state):
        self.state, self.cookie, self.csrf, self.requests = state, None, None, 0

    def received(self, route, method, code, raw, cookie):
        """Allow a scoped operator to durably acknowledge a mutation response."""

    def request(self, route, method="GET", body=None, expected=(200,), admin=False):
        require(route.startswith("/admin/v1/") or route in ("/readyz", "/emby/System/Info/Public"),
                "A fixture request left its fixed native initialization scope.")
        require("?" not in route and "#" not in route and "\n" not in route, "A fixture route is malformed.")
        require(self.requests < 240, "The fixture HTTP request budget was exceeded.")
        self.requests += 1
        verify_service(self.state)
        headers = {"Accept": "application/json", "Origin": PUBLIC}
        if admin:
            require(self.cookie and self.csrf, "The fixture administrator is not authenticated.")
            headers.update({"Cookie": self.cookie, "X-CSRF-Token": self.csrf})
        payload = None if body is None else json.dumps(body).encode()
        if payload is not None:
            headers["Content-Type"] = "application/json"
        connection = http.client.HTTPConnection("127.0.0.1", PORT, timeout=15)
        try:
            connection.request(method, route, payload, headers)
            response = connection.getresponse()
            raw = response.read((2 << 20) + 1)
            cookie = response.getheader("Set-Cookie")
            code = response.status
        finally:
            connection.close()
        require(len(raw) <= 2 << 20, "A native response exceeded its size budget.")
        self.received(route, method, code, raw, cookie)
        verify_service(self.state)
        if code not in expected:
            diagnostic = WORK / ("client-http-failure-" + secrets.token_hex(12) + ".bin")
            create(diagnostic, raw)
            raise FixtureError(f"Native {method} {route} returned {code}; inspect {diagnostic.name}.")
        data = json.loads(raw) if raw else None
        if route == "/admin/v1/session" and method == "POST":
            require(cookie and cookie.startswith("goby_session=") and data.get("CSRFToken"), "Native login omitted its session.")
            self.cookie, self.csrf = cookie.split(";", 1)[0], data["CSRFToken"]
        return data


def validate_added_viewer_user(user, state):
    require(isinstance(user, dict) and isinstance(user.get("Id"), str) and re.fullmatch(r"[0-9a-f]{32}", user["Id"]) and
            user["Id"] not in (state["admin_id"], state["viewer_id"]) and user.get("Name") == AV_NAME and
            user.get("IsAdministrator") is False and user.get("IsDisabled") is False and user.get("HasPassword") is True,
            "The additional account is not the exact new, enabled, password-protected ordinary AV user.")


def added_viewer_receipt(state):
    record = state.get("added_viewer")
    if record is None:
        return None
    require(isinstance(record, dict) and record.get("marker") == AV_MARKER and record.get("phase") == "complete" and
            record.get("username") == AV_NAME and record.get("evidence_directory") == str(AV_ROOT) and
            record.get("receipt_path") == str(AV_RECEIPT) and record.get("browser_alias") == str(AV_BROWSER),
            "An incomplete AV account operation is retained; it cannot be retried, adopted, or used to widen account ownership.")
    require(directory(AV_ROOT, 0, 0o700, 0) == record.get("evidence_identity") and
            load(AV_ROOT / "OWNER.json") == {"marker": AV_MARKER, "fixture_tag": state["tag"], "server_id": state["server_id"]},
            "The additional account evidence directory changed ownership.")
    require(sha(AV_RECEIPT) == record.get("receipt_sha256") and sha(AV_BROWSER) == record.get("browser_sha256") and
            sha(AV_ROOT / "credentials.json") == record.get("credentials_sha256"),
            "The additional account receipt or private credentials changed.")
    receipt, alias, credential = load(AV_RECEIPT), load(AV_BROWSER), load(AV_ROOT / "credentials.json")
    require(receipt.get("marker") == AV_MARKER and receipt.get("fixture_tag") == state["tag"] and
            receipt.get("server_id") == state["server_id"] and receipt.get("user_id") == record.get("user_id") and
            receipt.get("username") == AV_NAME and receipt.get("is_administrator") is False and
            receipt.get("is_disabled") is False and receipt.get("has_password") is True and
            receipt.get("admin_session_revoked") is True and record.get("admin_session_revoked") is True and
            all(receipt.get(key) == state.get(key) for key in ("cluster", "database_oid", "role_oid")) and
            receipt.get("source_process") == record.get("source_process"), "The additional account receipt does not bind this fixture.")
    validate_added_viewer_user(receipt.get("creation_user"), state)
    require(receipt["creation_user"]["Id"] == record["user_id"] and receipt.get("browser_alias") == str(AV_BROWSER) and
            receipt.get("browser_sha256") == record["browser_sha256"] and
            receipt.get("credentials_sha256") == record["credentials_sha256"], "The account receipt and alias identities differ.")
    require(receipt.get("creation_response_sha256") == sha(AV_ROOT / "creation-response.json") and
            receipt.get("before_snapshot_sha256") == sha(AV_ROOT / "before.json", limit=MAX_SNAPSHOT_BYTES) and
            receipt.get("after_snapshot_sha256") == sha(AV_ROOT / "after.json", limit=MAX_SNAPSHOT_BYTES),
            "The recorded account creation or preservation evidence changed.")
    response = load(AV_ROOT / "creation-response.json")
    require(response.get("status") == 201 and
            precise_json(response["body_utf8"]).get("User") == receipt["creation_user"], "The receipt lacks an exact successful creation response.")
    require(load(AV_ROOT / "admin-session-revoked.json") == {"logout_status": 204, "readback_status": 401},
            "The owned native session lacks its exact logout and invalidity proof.")
    require(credential.get("marker") == AV_MARKER and credential.get("fixture_tag") == state["tag"] and
            credential.get("username") == AV_NAME and re.fullmatch(r"[0-9a-f]{48}", credential.get("password", "")),
            "The new account's pre-created private credential differs.")
    require(sha(BROWSER) == state["browser_sha256"], "The original browser credentials changed.")
    original = load(BROWSER)
    require(alias == {"marker": MARKER, "account_marker": AV_MARKER, "base_url": PUBLIC,
                     "direct_url": f"http://127.0.0.1:{PORT}", "admin": original["admin"],
                     "viewer": {"username": AV_NAME, "password": credential["password"], "userId": record["user_id"],
                                "serverId": state["server_id"]}, "receipt_path": str(AV_RECEIPT)},
            "The new private browser alias does not match the receipted user and unchanged administrator credentials.")
    return receipt


def added_viewer_credentials(state):
    if added_viewer_receipt(state) is None:
        return None
    return {key: state["added_viewer"][key] for key in ("receipt_sha256", "browser_sha256", "credentials_sha256")}


def compare_added_viewer_accounts(before, after, user):
    left, right = before["database"]["tables"], after["database"]["tables"]
    previous = {row["id"]: row for row in left["users"]}
    current = {row["id"]: row for row in right["users"]}
    require(len(current) == len(right["users"]) == len(previous) + 1 and set(current) == set(previous) | {user["Id"]} and
            all(equal_json(row, current[key]) for key, row in previous.items()),
            "Appending the AV viewer changed an existing user's complete stored row or added an unexpected account.")
    row = current[user["Id"]]
    require(row["name"] == AV_NAME and row["is_administrator"] is False and row["is_disabled"] is False and row["has_password"] is True,
            "The newly stored account differs from its native creation acknowledgement.")
    for table in ("user_settings", "user_item_data"):
        require(equal_json(left.get(table), right.get(table)), "Existing user preferences or playback state changed: " + table + ".")
    require(all(equal_json(before[key], after[key]) for key in ("recovery", "runtime_sha256", "browser_sha256")),
            "Appending the AV viewer changed existing private credentials or recovery state.")


class AddedViewerAPI(NativeAPI):
    """Limit the additive mode to one creation and its own native session."""

    def __init__(self, state, browser, credential):
        super().__init__(state)
        self.browser, self.credential = browser, credential
        self.verified_admin, self.creation_sent, self.created_id = False, False, None

    def received(self, route, method, code, raw, cookie):
        name = {("/admin/v1/session", "POST"): "admin-login-response.json",
                ("/admin/v1/users", "POST"): "creation-response.json"}.get((route, method))
        if name:
            create(AV_ROOT / name, {"status": code, "body_utf8": raw.decode("utf-8", errors="replace"), "set_cookie": cookie})

    def request(self, route, method="GET", body=None, expected=(200,), admin=False):
        require(self.requests < 12, "The bounded AV account request budget was exhausted.")
        if route == "/admin/v1/session" and method == "POST":
            require(not self.cookie and not admin and body == {"Name": self.browser["admin"]["username"],
                    "Password": self.browser["admin"]["password"]}, "Only a new exact fixture administrator session may authenticate.")
        elif route in ("/readyz", "/emby/System/Info/Public"):
            require(method == "GET" and body is None and not admin, "The public fixture identity check must be read-only.")
        else:
            require(self.verified_admin and admin, "Only the newly proven administrator session may operate on the AV account.")
            if route == "/admin/v1/users" and method == "POST":
                require(not self.creation_sent and self.state["added_viewer"]["phase"] == "create_requested" and
                        body == {"Name": AV_NAME, "Password": self.credential["password"], "IsAdministrator": False},
                        "Only the explicitly pending single AV account creation may be sent.")
                self.creation_sent = True
            else:
                require(body is None and ((route == "/admin/v1/session" and method in ("GET", "DELETE")) or
                        (route == "/admin/v1/users" and method == "GET") or
                        (self.created_id is not None and route == "/admin/v1/users/" + self.created_id and method == "GET")),
                        "The request is outside exact account readback or owned-session cleanup.")
        return super().request(route, method, body, expected, admin)


def add_viewer(state):
    require(state.get("phase") == "ready" and state.get("stage") == "complete" and not any(state.get(key) for key in
            ("start_pending", "credentials_pending", "binary_pending", "unit_pending", "directory_pending", "bootstrap_pending",
             "viewer_pending", "role_creation_pending", "database_creation_pending")) and
            not any(row.get("creation_pending") for row in state.get("libraries", {}).values()) and
            ("upgrade" not in state or state["upgrade"].get("phase") == "complete"), "The fixture is not ready for an additive account operation.")
    existing = added_viewer_receipt(state)
    verify_fixture_directories(state)
    verify_database(state)
    require(verify_service(state) == state.get("process") and state.get("process") is not None and media_snapshot() == state["media"],
            "The additive account operation requires the exact live fixture and unchanged media.")
    version = target_schema_version(state)
    before = preservation_snapshot(state, version)
    validate_preservation_snapshot(before, version, state)
    if existing:
        return
    require(not any(exists(path) for path in (AV_ROOT, AV_BROWSER, AV_RECEIPT)),
            "An unrecorded AV account path already exists; its contents will not be adopted or overwritten.")
    library_ids = {record["id"] for record in state["libraries"].values()}
    require(len(library_ids) == 3 and {row["id"] for row in before["database"]["tables"]["libraries"]} == library_ids,
            "The fixture does not contain exactly its three recorded synthetic libraries.")
    browser = load(BROWSER)
    record = {"marker": AV_MARKER, "phase": "publishing_credentials", "username": AV_NAME, "created_at": utc(),
              "evidence_directory": str(AV_ROOT), "receipt_path": str(AV_RECEIPT), "browser_alias": str(AV_BROWSER),
              "source_process": state["process"], "admin_session_revoked": False}
    state["added_viewer"] = record
    save_state(state)
    AV_ROOT.mkdir(mode=0o700)
    record["evidence_identity"] = directory(AV_ROOT, 0, 0o700, 0)
    create(AV_ROOT / "OWNER.json", {"marker": AV_MARKER, "fixture_tag": state["tag"], "server_id": state["server_id"]})
    credential = {"marker": AV_MARKER, "fixture_tag": state["tag"], "username": AV_NAME, "password": secrets.token_hex(24)}
    create(AV_ROOT / "credentials.json", credential)
    create(AV_ROOT / "before.json", before)
    record.update(credentials_sha256=sha(AV_ROOT / "credentials.json"), phase="authenticating")
    save_state(state)
    api = AddedViewerAPI(state, browser, credential)
    try:
        require(api.request("/readyz") == {"Status": "ready"} and
                api.request("/emby/System/Info/Public").get("Id") == state["server_id"], "The native candidate identity changed.")
        account = api.request("/admin/v1/session", "POST", {"Name": browser["admin"]["username"],
                              "Password": browser["admin"]["password"]})["User"]
        require(account.get("Id") == state["admin_id"] and account.get("Name") == ACCOUNTS["admin"] and
                account.get("IsAdministrator") is True and account.get("IsDisabled") is False,
                "The new native session is not the recorded fixture administrator.")
        api.verified_admin = True
        users = api.request("/admin/v1/users", admin=True)["Items"]
        require(len(users) == 2 and {row.get("Id") for row in users} == {state["admin_id"], state["viewer_id"]} and
                all(any(row.get("Id") == state[key + "_id"] and row.get("Name") == name and
                        row.get("IsAdministrator") is (key == "admin") and row.get("IsDisabled") is False for row in users)
                    for key, name in ACCOUNTS.items()) and
                not any(row.get("Name", "").casefold() == AV_NAME.casefold() for row in users),
                "The native two-account baseline changed or the proposed AV username is occupied.")
        record["phase"] = "create_requested"
        save_state(state)
        user = api.request("/admin/v1/users", "POST", {"Name": AV_NAME, "Password": credential["password"],
                           "IsAdministrator": False}, expected=(201,), admin=True)["User"]
        validate_added_viewer_user(user, state)
        api.created_id = record["user_id"] = user["Id"]
        record["phase"] = "created"
        save_state(state)
        detail = api.request("/admin/v1/users/" + user["Id"], admin=True)["User"]
        validate_added_viewer_user(detail, state)
        require(detail["Id"] == user["Id"] and detail.get("Policy", {}).get("EnableAllFolders") is True and
                detail["Policy"].get("EnableMediaPlayback") is True, "The new ordinary user lacks its default owned-library playback access.")
        create(AV_ROOT / "managed-detail.json", detail)
    finally:
        if api.verified_admin:
            api.request("/admin/v1/session", "DELETE", expected=(204,), admin=True)
            api.request("/admin/v1/session", expected=(401,), admin=True)
            create(AV_ROOT / "admin-session-revoked.json", {"logout_status": 204, "readback_status": 401})
            record["admin_session_revoked"] = True
            save_state(state)
    # Keep the receipt unpublished until both the API acknowledgement and the
    # complete old user rows, preferences, and playback state have been checked.
    after = {"schema": version, "database": full_database_snapshot(state), "recovery": recovery_snapshot(),
             "runtime_sha256": sha(RUNTIME), "browser_sha256": sha(BROWSER)}
    compare_added_viewer_accounts(before, after, user)
    create(AV_ROOT / "after.json", after)
    create(AV_BROWSER, {"marker": MARKER, "account_marker": AV_MARKER, "base_url": PUBLIC, "direct_url": f"http://127.0.0.1:{PORT}",
                       "admin": browser["admin"], "viewer": {"username": AV_NAME, "password": credential["password"],
                       "userId": user["Id"], "serverId": state["server_id"]}, "receipt_path": str(AV_RECEIPT)})
    record["browser_sha256"] = sha(AV_BROWSER)
    receipt = {"marker": AV_MARKER, "fixture_tag": state["tag"], "server_id": state["server_id"], "user_id": user["Id"],
               "username": AV_NAME, "is_administrator": False, "is_disabled": False, "has_password": True,
               "creation_user": user, "source_process": record["source_process"], "browser_alias": str(AV_BROWSER),
               "browser_sha256": record["browser_sha256"], "credentials_sha256": record["credentials_sha256"],
               "creation_response_sha256": sha(AV_ROOT / "creation-response.json"), "admin_session_revoked": True,
               "before_snapshot_sha256": sha(AV_ROOT / "before.json", limit=MAX_SNAPSHOT_BYTES),
               "after_snapshot_sha256": sha(AV_ROOT / "after.json", limit=MAX_SNAPSHOT_BYTES),
               **{key: state[key] for key in ("cluster", "database_oid", "role_oid")}}
    create(AV_RECEIPT, receipt)
    completed = dict(record, phase="complete", completed_at=utc(), receipt_sha256=sha(AV_RECEIPT))
    candidate = dict(state, added_viewer=completed)
    validate_database_snapshot(after["database"], version, candidate)
    require(verify_service(state) == record["source_process"] and media_snapshot() == state["media"],
            "The fixture process or read-only media changed before account ownership publication.")
    state["added_viewer"] = completed
    save_state(state)


def seed_accounts(api, state, runtime, browser):
    additional = added_viewer_receipt(state)
    initialized = api.request("/admin/v1/bootstrap").get("Initialized")
    if not initialized:
        require("admin_id" not in state, "The recorded initialized administrator is missing.")
        state["bootstrap_pending"] = True
        save_state(state)
        account = api.request("/admin/v1/bootstrap", "POST", {"SetupToken": runtime["GOBY_SETUP_TOKEN"],
            "Name": browser["admin"]["username"], "Password": browser["admin"]["password"]}, expected=(201,))["User"]
        state["admin_id"] = account["Id"]
        save_state(state)
    require(state.get("admin_id") or state.get("bootstrap_pending") is True,
            "The candidate was initialized without a recorded bootstrap intent.")
    account = api.request("/admin/v1/session", "POST", {"Name": browser["admin"]["username"],
                          "Password": browser["admin"]["password"]})["User"]
    require(account.get("Name") == ACCOUNTS["admin"] and account.get("IsAdministrator") is True and
            not account.get("IsDisabled") and ("admin_id" not in state or state["admin_id"] == account["Id"]),
            "Native login resolved to an unexpected administrator.")
    state["admin_id"], state["bootstrap_pending"] = account["Id"], False
    save_state(state)
    users = api.request("/admin/v1/users", admin=True)["Items"]
    names = set(ACCOUNTS.values()) | ({AV_NAME} if additional else set())
    require(all(user.get("Name") in names for user in users) and len(users) in ((3,) if additional else (1, 2)),
            "The candidate contains unrecorded accounts.")
    if additional:
        extra = [user for user in users if user.get("Id") == additional["user_id"]]
        require(len(extra) == 1, "The receipted AV account is missing from the native account inventory.")
        validate_added_viewer_user(extra[0], state)
    matches = [user for user in users if user["Name"] == ACCOUNTS["viewer"]]
    if not matches:
        require("viewer_id" not in state, "The recorded viewer account is missing.")
        state["viewer_pending"] = True
        save_state(state)
        viewer = api.request("/admin/v1/users", "POST", {"Name": browser["viewer"]["username"],
            "Password": browser["viewer"]["password"], "IsAdministrator": False}, expected=(201,), admin=True)["User"]
    else:
        require(len(matches) == 1 and (state.get("viewer_id") == matches[0]["Id"] or state.get("viewer_pending") is True),
                "The existing viewer does not match a recorded creation intent.")
        viewer = matches[0]
    require(viewer.get("Name") == ACCOUNTS["viewer"] and viewer.get("IsAdministrator") is False and not viewer.get("IsDisabled"),
            "The viewer account has unexpected authority or state.")
    state["viewer_id"], state["viewer_pending"] = viewer["Id"], False
    save_state(state)


def seed_libraries(api, state):
    records = state.setdefault("libraries", {})
    for specification in LIBRARIES:
        key = specification["key"]
        record = records.setdefault(key, {})
        libraries = api.request("/admin/v1/libraries", admin=True)["Items"]
        require(all(library.get("Name") in {entry["name"] for entry in LIBRARIES} for library in libraries),
                "The candidate contains an unrecorded library.")
        matches = [library for library in libraries if library.get("Name") == specification["name"] or
                   specification["path"] in library.get("Paths", [])]
        if not matches:
            require("id" not in record, "A recorded candidate library is missing.")
            record["creation_pending"] = True
            save_state(state)
            library = api.request("/admin/v1/libraries", "POST", {"Name": specification["name"],
                "CollectionType": specification["type"], "Paths": [specification["path"]], "Scan": False},
                expected=(201,), admin=True)["Library"]
        else:
            require(len(matches) == 1 and (record.get("id") == matches[0]["Id"] or record.get("creation_pending") is True),
                    "The candidate library lacks recorded ownership.")
            library = matches[0]
        require(library.get("Name") == specification["name"] and library.get("CollectionType") == specification["type"] and
                library.get("Paths") == [specification["path"]], "The candidate library definition differs from its intent.")
        record["id"], record["creation_pending"] = library["Id"], False
        save_state(state)
        if not record.get("scan_id"):
            jobs = api.request("/admin/v1/jobs", admin=True)["Items"]
            if "scan_before" in record:
                new = [job for job in jobs if job["LibraryId"] == record["id"] and job["Id"] not in record["scan_before"]]
                require(len(new) == 1, "An interrupted scan dispatch is ambiguous; no duplicate scan was issued.")
                job = new[0]
            else:
                record["scan_before"] = [job["Id"] for job in jobs]
                save_state(state)
                job = api.request("/admin/v1/libraries/" + record["id"] + "/scan", "POST", {},
                                  expected=(202,), admin=True)["Job"]
            record["scan_id"] = job["Id"]
            save_state(state)
        deadline = time.monotonic() + 300
        while True:
            jobs = api.request("/admin/v1/jobs", admin=True)["Items"]
            matches = [job for job in jobs if job["Id"] == record["scan_id"] and job["LibraryId"] == record["id"]]
            require(len(matches) == 1, "The recorded scan job is missing or belongs to another library.")
            job = matches[0]
            if job["Status"] not in ("pending", "queued", "running"):
                require(job["Status"] == "completed" and not job.get("Error") and job.get("Scanned", 0) > 0,
                        "An owned library scan failed or produced no inspected media.")
                record["scan"] = {name: job.get(name) for name in ("Status", "Scanned", "Added", "Updated")}
                save_state(state)
                break
            require(time.monotonic() < deadline, "The owned scan remains active beyond this observation window.")
            time.sleep(3)


def summarize(state):
    result = {"marker": MARKER, "at": utc(), "phase": state.get("phase"), "stage": state.get("stage"), "unit": UNIT, "process": state.get("process"),
            "listen": f"127.0.0.1:{PORT}", "public_url": PUBLIC, "binary_sha256": state.get("binary_sha256"),
            "cluster": state.get("cluster"), "database": {"name": ROLE, "oid": state.get("database_oid"), "role_oid": state.get("role_oid")},
            "server_id": state.get("server_id"), "schema": state.get("schema"),
            "accounts": {key: state.get(key + "_id") for key in ACCOUNTS},
            "libraries": {key: {name: record.get(name) for name in ("id", "scan_id", "scan")}
                          for key, record in state.get("libraries", {}).items()},
            "state_directory": str(DATA), "private_browser_file": str(BROWSER), "media_read_only": True,
            "client_acceptance": "not_run"}
    if "upgrade" in state:
        result["upgrade"] = {key: state["upgrade"].get(key) for key in
            ("id", "phase", "from_sha256", "to_sha256", "old_process", "new_process", "rollback_binary",
             "evidence_directory", "protocol_version", "product_version", "preservation", "after_preservation",
             "from_schema", "to_schema", "schema_artifacts")}
    if isinstance(state.get("added_viewer"), dict):
        result["added_viewer"] = {key: state["added_viewer"].get(key) for key in
                                 ("phase", "username", "user_id", "receipt_path", "receipt_sha256", "browser_alias",
                                  "browser_sha256", "admin_session_revoked")}
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("prepare", "inspect", "upgrade", "add-viewer"))
    parser.add_argument("--binary", type=Path)
    parser.add_argument("--sha256")
    parser.add_argument("--target-schema", type=int)
    parser.add_argument("--source", type=Path)
    parser.add_argument("--source-manifest-sha256")
    parser.add_argument("--schema25-catalog-sha256")
    parser.add_argument("--schema26-catalog-sha256")
    parser.add_argument("--full-report", type=Path)
    parser.add_argument("--full-report-sha256")
    options = parser.parse_args()
    os.umask(0o077)
    state, report, descriptor = None, None, None
    try:
        require((options.mode == "upgrade" and options.binary is not None and options.sha256 is not None) or
                (options.mode != "upgrade" and options.binary is None and options.sha256 is None and options.target_schema is None and
                 options.source is None and options.source_manifest_sha256 is None and options.schema25_catalog_sha256 is None and
                 options.schema26_catalog_sha256 is None and options.full_report is None and options.full_report_sha256 is None),
                "Only upgrade accepts and requires --binary ABSOLUTE_PATH --sha256 DIGEST.")
        require((options.source is None) == (options.source_manifest_sha256 is None), "--source and --source-manifest-sha256 must be supplied together.")
        require((options.full_report is None) == (options.full_report_sha256 is None), "--full-report and its SHA-256 must be supplied together.")
        host_inputs(initial=options.mode == "prepare")
        if not exists(STATE_FILE):
            require(not exists(LOCK), "An operator lock exists without a recorded fixture; it will not be adopted.")
        if not exists(LOCK):
            require(options.mode == "prepare" and not exists(STATE_FILE), "The recorded operator lock is missing.")
            create(LOCK, "")
        regular(LOCK)
        descriptor = os.open(LOCK, os.O_RDWR | os.O_NOFOLLOW)
        require(identity(os.fstat(descriptor)) == identity(regular(LOCK)), "The operator lock changed during open.")
        fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        cluster = cluster_identity()
        media = media_snapshot()
        if not exists(STATE_FILE):
            require(options.mode == "prepare" and not any(exists(path) for path in (RUNTIME, BROWSER, DATA, INSTALL, UNIT_FILE)) and
                    properties().get("LoadState") == "not-found" and role_row() is None and database_row() is None,
                    "Fresh candidate paths, service, or database identities are already occupied.")
            state = {"marker": MARKER, "work": str(WORK), "work_identity": directory(WORK, 0, 0o700, 0),
                     "tag": MARKER + ":" + secrets.token_hex(16), "cluster": cluster, "media": media,
                     "phase": "preparing", "created_at": utc()}
            create(STATE_FILE, state)
        else:
            state = load(STATE_FILE)
        require(state.get("marker") == MARKER and state.get("work") == str(WORK) and
                state.get("work_identity") == directory(WORK, 0, 0o700, 0) and state.get("cluster") == cluster and
                state.get("media") == media and re.fullmatch(re.escape(MARKER) + r":[0-9a-f]{32}", state.get("tag", "")),
                "Candidate ownership, cluster identity, or read-only media changed.")
        require(not state.get("directory_pending"), "An interrupted directory publication requires operator inspection.")
        require("upgrade" not in state or state["upgrade"].get("phase") == "complete",
                "An interrupted upgrade is retained for inspection; no automatic replacement, restart, or rollback is performed.")
        added_viewer_receipt(state)
        if options.mode == "prepare":
            require(state.get("binary_sha256", SOURCE_SHA) == SOURCE_SHA and state.get("schema", 23) == 23,
                    "Prepare retains the initial accepted baseline; an upgraded fixture must use inspect or explicit upgrade.")
            state["stage"] = "credentials"
            save_state(state)
            runtime, browser, password = credentials(state)
            state["stage"] = "database"
            save_state(state)
            prepare_database(state, password)
            state["stage"] = "service_files"
            save_state(state)
            prepare_files(state)
            state["stage"] = "service_start"
            save_state(state)
            start_service(state)
            api = NativeAPI(state)
            require(api.request("/readyz") == {"Status": "ready"}, "The candidate service is not ready.")
            info = api.request("/emby/System/Info/Public")
            require("server_id" not in state or state["server_id"] == info["Id"], "The recorded candidate server identity changed.")
            state["server_id"] = info["Id"]
            state["stage"] = "accounts"
            save_state(state)
            seed_accounts(api, state, runtime, browser)
            state["stage"] = "library_scans"
            save_state(state)
            seed_libraries(api, state)
            verify_database(state)
            schema = postgres("SELECT coalesce(json_agg(version ORDER BY version),'[]'::json) FROM public.schema_migrations;", ROLE)
            require(json.loads(schema) == list(range(1, 24)), "The accepted candidate did not establish exact schema 23.")
            state["schema"] = 23
            require(media_snapshot() == state["media"], "A shared read-only media snapshot changed during preparation.")
            if exists(DATA / "master.key"):
                require(regular(DATA / "master.key", 995, 0o600, 32).st_size == 32, "The isolated master key is invalid.")
                state["master_key_present"] = True
            else:
                # The application vault creates its private key lazily on the
                # first application-key issuance. Viewer login needs no vault.
                require(postgres("SELECT count(*) FROM public.application_keys;", ROLE) == "0" and
                        state.get("master_key_present") is not True,
                        "A previously required application-key master file is missing.")
                state["master_key_present"] = False
            state["phase"] = "ready"
            state["stage"] = "complete"
            save_state(state)
        elif options.mode == "upgrade":
            upgrade_fixture(state, options.binary, options.sha256, options.target_schema, options.source,
                            options.source_manifest_sha256, options.schema25_catalog_sha256,
                            options.schema26_catalog_sha256, options.full_report, options.full_report_sha256)
        elif options.mode == "add-viewer":
            add_viewer(state)
        else:
            verify_database(state)
            verify_fixture_directories(state)
            require(state.get("phase") == "ready" and verify_service(state) == state.get("process"),
                    "The recorded fixture is not ready with its original owned process.")
            require(sha(BROWSER) == state["browser_sha256"], "The private browser credential file changed.")
            current = target_schema_version(state)
            snapshot = preservation_snapshot(state, current)
            validate_preservation_snapshot(snapshot, current, state)
            report_snapshot = preservation_summary(snapshot)
        report = summarize(state)
        if options.mode == "inspect":
            report["inspection"] = report_snapshot
        report["result"] = "ready"
    except Exception as error:
        try:
            directory(WORK, 0, 0o700, 0)
            create(WORK / ("client-private-trace-" + secrets.token_hex(12) + ".txt"), traceback.format_exc())
        except Exception:
            pass
        report = summarize(state) if state is not None else {"marker": MARKER, "at": utc()}
        report.update(result="failed", failure_type=type(error).__name__,
                      reason=str(error) if isinstance(error, FixtureError) else "An unexpected operator error requires private inspection.",
                      evidence_retained=True)
    finally:
        if descriptor is not None:
            os.close(descriptor)
    if report is not None:
        # Reports are append-only and contain selected metadata, never response
        # bodies, passwords, tokens, environment values, or connection URLs.
        try:
            directory(WORK, 0, 0o700, 0)
            path = WORK / ("client-fixture-report-" + secrets.token_hex(12) + ".json")
            create(path, report)
            report["report"] = str(path)
        except Exception:
            pass
        print(json.dumps(report, sort_keys=True))
    return 0 if report is not None and report.get("result") == "ready" else 1


if __name__ == "__main__":
    raise SystemExit(main())
