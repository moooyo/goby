"""Read the closed r04 physical catalog once in a fresh PostgreSQL single-user copy.

Preparation only until a reviewed input is supplied to ``python3 -I -B``.
The input contains kind/version, approved=true, sourceSha256, scopeId, bootId,
tool pins, and current protected unit/file metadata. A scopeId is consumed by
exclusive creation of both fixed-prefix directories; the script never retries.

Bounds: one PostgreSQL child, no application, HTTP, listener, service, anchor,
D-Bus, psql, initdb, repair, or migration. A 512 MiB private tmpfs holds the copy.
The child has a 512 MiB address-space ceiling, 30 CPU seconds, a 32 MiB per-file
ceiling (which permits 16 MiB WAL files), and a 45-second wait. stdout and stderr
use separately bounded 1 MiB pipes. Monotonic preparation/business and cleanup
budgets are 180 + 60 seconds, not an external hard wall-clock guarantee. At most
48 metadata/volume commands run. The parent has no CPU/file-size kill limit that
would interrupt copy extraction or prevent its finally block from closing resources.
Original configuration members are hashed without decoding and omitted from the
copy. The initialization-password member is metadata-only and omitted. Master
or unexpected secret members are rejected without opening their payloads.

Reuses the pinned isolated-infrastructure reader/writer/command/process helpers.
Its old main, fixture, protected-state implementation and unit operations are
never called. Single-user mode proves archived logical state only: it does not
recreate the original sealer transaction, role, IPC, locking or network identity.

Source rationale: PostgreSQL REL_17_STABLE commands/copyto.c sends COPY STDOUT
to stdout for a non-remote destination; tcop/postgres.c handles stdin EOF with
normal process exit. A tagged hex COPY frame avoids standalone debug tuple output.
SQL error detection, frame completeness and natural exit are required together.
"""

import argparse
import fcntl
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import pwd
import re
import resource
import selectors
import signal
import stat
import subprocess
import sys
import tarfile
import time
import types


ORIGINAL = Path('/opt/goby-test/m6-systemd-install-20260915-r04')
BASE_PATH = Path('/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/main-isolated-restore-01/private/prepare-isolated-infrastructure.py')
BASE_SHA = 'ce20e4dbe21add70f90370b84e1debcf136313db6b98e8fb62ebe47433e6a9d0'
SEAL_PATH = ORIGINAL / 'private/m6-systemd-install-seal.py'
SEAL_SHA = '79c320f18031044fb386d85bd7fc29c553bea301ae768a52b6a453101e2ac8cb'
ARCHIVE = {'path': str(ORIGINAL / 'private/failure-preserve-retained/closed-postgres-private.tar.gz'),
           'bytes': 6950651, 'sha256': 'cddb636d2651c06466d12eb62c13f9fce9883239513cb9c5b994f25a368beea9'}
SNAPSHOT = {'path': str(ORIGINAL / 'private/runtime/sql/after-journey-2-parsed.json'),
            'bytes': 81236, 'sha256': '33a707546875649322ae7fe8a7395fe16c40d4903630a6f2a629ef0ab860e33b'}
SNAPSHOT_RAW = {'path': str(ORIGINAL / 'private/runtime-commands/43-sql-after-journey-2.stdout'),
                'bytes': 50677, 'sha256': '0d8bf937297ab9f5f5a00d0ede21ea4e6a6149d076768f96ee6bb2bed889be4a'}
LOCK = Path('/opt/goby-test/exec-work-m3e/main-deployment-schema25.lock')
UID, GID = 103, 106
VOLUME_BYTES = 512 << 20
TABLES = ('users', 'libraries', 'library_roots', 'items', 'catalog_entities',
          'item_entities', 'item_metadata_state', 'user_item_data', 'scan_jobs')
COUNTS = dict(zip(TABLES, (3, 2, 2, 14, 1, 16, 14, 5, 2)))
CONFIG_MEMBERS = {'tree/data/' + name for name in (
    'postgresql.conf', 'postgresql.auto.conf', 'pg_hba.conf', 'pg_ident.conf', 'postmaster.opts')}
FORBIDDEN_NAMES = {'standby.signal', 'recovery.signal', 'backup_label', 'tablespace_map', 'postmaster.pid'}
TOOL_PATHS = {'postgres': '/usr/lib/postgresql/17/bin/postgres',
              'control': '/usr/lib/postgresql/17/bin/pg_controldata',
              'mount': '/usr/bin/mount', 'umount': '/usr/bin/umount',
              'systemctl': '/usr/bin/systemctl'}
SAFE_ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C.UTF-8',
            'LC_ALL': 'C.UTF-8', 'TZ': 'UTC', 'SYSTEMD_COLORS': '0'}
FRAME_START, FRAME_END = b'R04_CATALOG_BEGIN:', b':R04_CATALOG_END'
CONTROL_SIGNALS = (signal.SIGTERM, signal.SIGINT, signal.SIGHUP)
PENDING_SIGNAL = None
PROTECTED_UNITS = {
    'goby-foundation-test.service', 'goby-client-m3e.service', 'postgresql@17-main.service',
    'goby-audited-20260913T073217Z-ef77f9ffcf0b-postgres.service',
    'goby-audited-20260913T073217Z-ef77f9ffcf0b-server.service',
    'goby-audited-20260914T083143Z-9b73ad46f2e6-postgres.service',
    'goby-audited-20260914T083143Z-9b73ad46f2e6-server.service',
}
PROTECTED_FILES = {
    str(LOCK), '/opt/goby-dev/goby', '/var/lib/goby-test/application-key-vault/master.key',
    '/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b/install/goby',
    '/opt/goby-audited-candidate-20260914T083143Z-9b73ad46f2e6/install/goby',
}


class Rejected(Exception):
    pass


def need(condition, code):
    if not condition:
        raise Rejected(code)


def parse(raw):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            need(key not in result, 'duplicate_json_key')
            result[key] = value
        return result
    def invalid(_value):
        raise Rejected('nonfinite_json')
    return json.loads(raw, object_pairs_hook=unique, parse_constant=invalid)


def encoded(value):
    return (json.dumps(value, sort_keys=True, separators=(',', ':'), allow_nan=False) + '\n').encode()


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def read_bootstrap(path, maximum):
    path = Path(path)
    need(path.resolve() == path, 'input_symlink')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        need(stat.S_ISREG(before.st_mode) and before.st_uid == 0 and before.st_nlink == 1 and
             not before.st_mode & 0o022 and before.st_size <= maximum, 'input_metadata')
        raw = os.read(fd, maximum + 1)
        after = os.fstat(fd)
        stable = ('st_dev', 'st_ino', 'st_uid', 'st_gid', 'st_mode', 'st_size', 'st_nlink', 'st_mtime_ns', 'st_ctime_ns')
        current = path.lstat()
        need(path.resolve() == path and all(getattr(before, name) == getattr(after, name) == getattr(current, name)
                                           for name in stable) and len(raw) == before.st_size, 'input_changed')
        return raw
    finally:
        os.close(fd)


def load_base():
    raw = read_bootstrap(BASE_PATH, 128 << 10)
    need(sha(raw) == BASE_SHA, 'base_source_pin')
    module = types.ModuleType('r04_archived_catalog_verified_helpers')
    module.__file__ = str(BASE_PATH)
    exec(compile(raw, str(BASE_PATH), 'exec'), module.__dict__)
    return module


def validate_input(value):
    need(set(value) == {'kind', 'version', 'approved', 'sourceSha256', 'scopeId', 'bootId', 'tools', 'protected'}, 'input_keys')
    need(value['kind'] == 'r04-archived-catalog-read-input' and type(value['version']) is int and value['version'] == 1 and
         value['approved'] is True and re.fullmatch('[0-9a-f]{64}', value['sourceSha256']), 'input_authority')
    need(re.fullmatch('20260915-[a-z0-9]{8}', value['scopeId']) and re.fullmatch('[0-9a-f-]{36}', value['bootId']), 'scope_or_boot')
    need(set(value['tools']) == set(TOOL_PATHS), 'tool_inventory')
    for name, pin in value['tools'].items():
        need(set(pin) == {'path', 'bytes', 'sha256'} and pin['path'] == TOOL_PATHS[name] and
             type(pin['bytes']) is int and 0 < pin['bytes'] <= 64 << 20 and re.fullmatch('[0-9a-f]{64}', pin['sha256']), 'tool_pin_shape')
    protected = value['protected']
    need(set(protected) == {'units', 'files'} and set(protected['units']) == PROTECTED_UNITS and
         len(PROTECTED_FILES) <= len(protected['files']) <= 32, 'protected_inventory')
    need(PROTECTED_FILES <= {row.get('path') for row in protected['files']} and
         len({row.get('path') for row in protected['files']}) == len(protected['files']), 'protected_file_inventory')
    for name, expected in protected['units'].items():
        need(re.fullmatch('[a-zA-Z0-9@_.-]+[.]service', name) and not name.startswith('goby-r04-archive-') and isinstance(expected, dict), 'protected_unit')
    for row in protected['files']:
        need(set(row) == {'path', 'mode', 'metadata', 'sha256'} and row['mode'] in ('hash', 'metadata') and
             Path(row['path']).is_absolute() and isinstance(row['metadata'], dict), 'protected_file')
        sensitive = re.search('master|password|secret|[.]key$', Path(row['path']).name, re.I)
        need(not sensitive or row['mode'] == 'metadata', 'protected_secret_content_forbidden')
        need((row['mode'] == 'metadata' and row['sha256'] is None) or
             (row['mode'] == 'hash' and re.fullmatch('[0-9a-f]{64}', row['sha256'])), 'protected_hash')


def sql_text():
    pairs = []
    for table in TABLES:
        pairs.append("'%s',(SELECT jsonb_build_object('count',(SELECT count(*) FROM public.%s),'rows',COALESCE(jsonb_agg(jsonb_build_object('row',t.row,'xmin',t.xmin) ORDER BY t.row::text),'[]'::jsonb)) FROM (SELECT to_jsonb(r) AS row,r.xmin::text AS xmin FROM public.%s r ORDER BY to_jsonb(r)::text LIMIT 129) t)" % (table, table, table))
    payload = "jsonb_build_object('serverId',(SELECT value FROM public.server_settings WHERE key='server_id'),'schema',(SELECT jsonb_build_object('count',count(*),'min',min(version),'max',max(version)) FROM public.schema_migrations),'unrevokedSessions',(SELECT count(*) FROM public.sessions WHERE revoked_at IS NULL),'sessions',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',id,'userId',user_id,'kind',kind,'tokenHash',encode(token_hash,'hex'),'revokedAt',revoked_at) ORDER BY id) FROM public.sessions),'[]'::jsonb),'tables',jsonb_build_object(" + ','.join(pairs) + '))'
    marker = "jsonb_build_object('pid',pg_backend_pid(),'database',current_database(),'databaseOid',(SELECT oid::bigint FROM pg_database WHERE datname=current_database()),'systemId',(SELECT system_identifier::text FROM pg_control_system()),'dataDirectory',current_setting('data_directory'),'readOnly',current_setting('transaction_read_only'),'isolation',current_setting('transaction_isolation'),'recovery',pg_is_in_recovery())"
    query = "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SET LOCAL search_path=pg_catalog; SET LOCAL statement_timeout='15s'; SET LOCAL lock_timeout='1s'; SET LOCAL TIME ZONE 'UTC'; COPY (SELECT 'R04_CATALOG_BEGIN:' || encode(convert_to(jsonb_build_object('identity'," + marker + ",'snapshot'," + payload + ")::text,'UTF8'),'hex') || ':R04_CATALOG_END') TO STDOUT; COMMIT;\n"
    need('\n' not in query[:-1] and len(query.encode()) < 16 << 10 and "COPY (SELECT" in query and "TO STDOUT" in query, 'fixed_sql_shape')
    return query.encode()


def parse_frame(raw, stderr):
    need(len(raw) <= 1 << 20 and len(stderr) <= 1 << 20, 'child_output_bound')
    need(not re.search(rb'\b(ERROR|FATAL|PANIC|WARNING):', stderr), 'postgres_error_or_warning')
    need(raw.count(FRAME_START) == raw.count(FRAME_END) == 1, 'single_complete_frame_required')
    before, frame = raw.split(FRAME_START)
    payload, after = frame.split(FRAME_END)
    need(re.fullmatch(rb'[0-9a-f]+', payload) and len(payload) % 2 == 0 and len(payload) <= 512 << 10, 'frame_hex')
    need(FRAME_START not in stderr and FRAME_END not in stderr, 'frame_channel')
    need(re.fullmatch(rb'\nPostgreSQL stand-alone backend 17[^\r\n]{0,128}\nbackend> ', before) and
         after == b'\nbackend> ', 'standalone_output_envelope')
    value = parse(bytes.fromhex(payload.decode('ascii')))
    need(isinstance(value, dict) and set(value) == {'identity', 'snapshot'}, 'frame_shape')
    return value


def compare_snapshot(actual, expected):
    keys = {'serverId', 'schema', 'unrevokedSessions', 'sessions', 'tables'}
    need(set(actual) == keys and actual['schema'] == {'count': 28, 'min': 1, 'max': 28} and
         type(actual['unrevokedSessions']) is int and actual['unrevokedSessions'] == 0 and len(actual['sessions']) == 8, 'snapshot_shape')
    need(set(actual['tables']) == set(TABLES), 'snapshot_tables')
    for name, count in COUNTS.items():
        row = actual['tables'][name]
        need(type(row['count']) is int and row['count'] == count and len(row['rows']) == count and
             all(set(q) == {'row', 'xmin'} and isinstance(q['xmin'], str) for q in row['rows']), 'snapshot_table_shape')
    need(all(q['revokedAt'] is not None for q in actual['sessions']) and
         encoded(actual) == encoded({key: expected[key] for key in keys}), 'archived_catalog_differs')


def metadata_child_limits():
    """Apply the historical command-output limit to its child, not the reader."""
    resource.setrlimit(resource.RLIMIT_FSIZE, (1 << 20, 1 << 20))
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))


def bounded_metadata_popen(*args, **kwargs):
    need('preexec_fn' not in kwargs, 'metadata_preexec_override')
    return subprocess.Popen(*args, **kwargs, preexec_fn=metadata_child_limits)


def single_child_limits(parent_pid, signal_mask):
    """Limit only the owned PostgreSQL child and close it if its parent dies."""
    import ctypes
    signal.pthread_sigmask(signal.SIG_SETMASK, signal_mask)
    resource.setrlimit(resource.RLIMIT_AS, (512 << 20, 512 << 20))
    resource.setrlimit(resource.RLIMIT_FSIZE, (32 << 20, 32 << 20))
    resource.setrlimit(resource.RLIMIT_CPU, (30, 30))
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    libc = ctypes.CDLL(None, use_errno=True)
    # Linux PR_SET_PDEATHSIG; the parent identity check closes the setup race.
    if libc.prctl(1, signal.SIGKILL, 0, 0, 0) != 0 or os.getppid() != parent_pid:
        os._exit(125)


class Reader:
    def __init__(self, value, base):
        self.value, self.base = value, base
        self.evidence = Path('/opt/goby-test/r04-archived-catalog-' + value['scopeId'])
        self.fixture = Path('/opt/goby-r04-archive-' + value['scopeId'])
        self.volume = self.fixture / 'pg'
        self.data = self.volume / 'data'
        self.deadline = time.monotonic() + 240
        self.business_deadline = self.deadline - 60
        self.cleaning = False
        self.mount = None
        self.child = None
        self.child_identity = None
        self.child_reaped = False
        self.child_exit = None
        self.lock_fd = None
        self.report = {'kind': 'r04-archived-catalog-read', 'version': 1, 'status': 'running',
                       'scope': str(self.evidence), 'commands': [], 'inputPins': [], 'errors': [], 'cleanupErrors': [],
                       'applicationStarts': 0, 'httpRequests': 0, 'postgresStartAttempts': 0, 'postgresStarts': 0, 'serviceActions': 0,
                       'networkListenersRequested': 0, 'sqlConnections': 0, 'standaloneSqlInputs': 0,
                       'originalFilesChanged': False, 'originalFinalObserverRecreated': False, 'installationAccepted': False}
        base.P, base.ENV, base.report = self.evidence / 'private', dict(SAFE_ENV), self.report
        base.subprocess = types.SimpleNamespace(Popen=bounded_metadata_popen, DEVNULL=subprocess.DEVNULL,
                                                TimeoutExpired=subprocess.TimeoutExpired)

    def error(self, error):
        code = str(error)
        return {'type': type(error).__name__, 'code': code if isinstance(error, (Rejected, self.base.Rejected)) and
                re.fullmatch('[a-z0-9_]{1,120}', code) else 'details_retained_privately_or_unavailable'}

    def remaining(self, maximum):
        need(self.cleaning or PENDING_SIGNAL is None, 'interrupted')
        remaining = (self.deadline if self.cleaning else self.business_deadline) - time.monotonic()
        need(remaining > 12, 'phase_time_exhausted')
        return min(maximum, remaining - 12)

    def command(self, label, argv, maximum=10):
        need(len(self.report['commands']) < 48, 'command_budget')
        result = self.base.run(label, argv, self.remaining(maximum))
        need(self.cleaning or PENDING_SIGNAL is None, 'interrupted')
        return result

    def save(self, name, value):
        return self.base.write_new(self.base.P / name, encoded(value))

    def pinned(self, pin, maximum=64 << 20):
        raw = self.base.read_regular(Path(pin['path']), maximum)
        need(len(raw) == pin['bytes'] and sha(raw) == pin['sha256'], 'evidence_pin_changed')
        self.report['inputPins'].append(pin)
        return raw

    def protected(self):
        need(Path('/proc/sys/kernel/random/boot_id').read_text().strip() == self.value['bootId'], 'boot_changed')
        observed = {'units': {}, 'files': []}
        for name, expected in self.value['protected']['units'].items():
            raw = self.command('protected-unit', [TOOL_PATHS['systemctl'], 'show', name, '--no-pager', '--property=' + ','.join(self.base.PROPERTIES)])
            row = dict(line.split('=', 1) for line in raw.decode().splitlines() if '=' in line)
            need(row == expected, 'protected_unit_changed')
            observed['units'][name] = row
        for expected in self.value['protected']['files']:
            path = Path(expected['path'])
            row = {**expected, 'metadata': self.base.metadata(path)}
            if expected['mode'] == 'hash':
                row['sha256'] = sha(self.base.read_regular(path, 64 << 20, owner=expected['metadata']['uid']))
            need(row == expected, 'protected_file_changed')
            observed['files'].append(row)
        return observed

    def capacity(self):
        info = dict((line.split(':')[0], int(line.split()[1]) * 1024) for line in Path('/proc/meminfo').read_text().splitlines() if line.startswith('MemAvailable:'))
        fs = os.statvfs('/opt/goby-test')
        free = fs.f_bavail * fs.f_frsize
        need(info['MemAvailable'] >= 1536 << 20 and free >= 512 << 20 and fs.f_favail >= 4096, 'insufficient_capacity')
        return {'memAvailableBytes': info['MemAvailable'], 'rootAvailableBytes': free, 'rootAvailableInodes': fs.f_favail}

    def create_fixture_directory(self):
        """Apply traversal permissions explicitly after the private process umask."""
        self.fixture.mkdir(mode=0o711)
        fd = os.open(self.fixture, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            os.fchmod(fd, 0o711)
            owned = os.fstat(fd)
            actual = self.base.metadata(self.fixture)
            need(self.fixture.resolve() == self.fixture and actual['device'] == owned.st_dev and
                 actual['inode'] == owned.st_ino and actual['type'] == stat.S_IFDIR and
                 actual['uid'] == actual['gid'] == 0 and actual['mode'] == 0o711,
                 'fixture_directory_identity_or_mode')
            self.report['fixtureDirectory'] = actual
        finally:
            os.close(fd)

    def mount_identity(self):
        lines = [line for line in Path('/proc/self/mountinfo').read_text().splitlines() if line.split()[4] == str(self.volume)]
        need(len(lines) == 1 and ' - tmpfs goby-r04-archive-' + self.value['scopeId'] + ' ' in lines[0], 'new_mount_identity')
        fields = lines[0].split()
        need({'nosuid', 'nodev', 'noexec'} <= set(fields[5].split(',')) and
             os.statvfs(self.volume).f_blocks * os.statvfs(self.volume).f_frsize == VOLUME_BYTES,
             'new_mount_limits')
        return {'id': fields[0], 'device': fields[2], 'root': fields[3], 'path': fields[4], 'metadata': self.base.metadata(self.volume)}

    def archive_members(self, archive):
        members = archive.getmembers()
        need(len(members) == 1518 and len({m.name for m in members}) == len(members), 'archive_member_inventory')
        files, directories, total = 0, 0, 0
        for member in members:
            name = PurePosixPath(member.name)
            need(not name.is_absolute() and '..' not in name.parts and name.parts[0] == 'tree' and
                 (member.isfile() or member.isdir()) and not member.pax_headers, 'archive_member_kind_or_path')
            need(member.name == 'tree' or name.parts[1] in ('data', 'socket', 'initdb-password'), 'archive_top_level')
            need(name.parts[1:] != ('socket',) or member.isdir(), 'archive_socket_directory')
            need(not (len(name.parts) > 2 and name.parts[1] == 'socket'), 'archive_socket_not_empty')
            need(name.name not in FORBIDDEN_NAMES and not (len(name.parts) > 3 and name.parts[:3] == ('tree', 'data', 'pg_tblspc')), 'archive_requires_recovery_or_tablespace')
            # postmaster.opts is an exact, hash-only configuration exception.
            need(member.name in CONFIG_MEMBERS or not re.search('master|secret|[.]key$', name.name, re.I),
                 'archive_master_or_secret_forbidden')
            need('password' not in name.name.lower() or member.name == 'tree/initdb-password', 'unexpected_password_member')
            need(member.uid == UID and member.gid == GID and not member.mode & 0o077 and member.size <= 16 << 20, 'archive_member_metadata')
            files += member.isfile()
            directories += member.isdir()
            total += member.size if member.isfile() else 0
        need((files, directories, total) == (1488, 30, 50863469), 'archive_size_inventory')
        need({'tree/data', 'tree/socket', 'tree/data/PG_VERSION', 'tree/data/global/pg_control'} <= {m.name for m in members}, 'archive_physical_layout')
        return members

    def extract_copy(self):
        archive_bytes = self.pinned(ARCHIVE, 8 << 20)
        manifest = []
        with tarfile.open(fileobj=io.BytesIO(archive_bytes), mode='r:gz') as archive:
            members = self.archive_members(archive)
            for member in sorted(members, key=lambda m: (len(PurePosixPath(m.name).parts), m.name)):
                self.remaining(1)
                parts = PurePosixPath(member.name).parts
                row = {'name': member.name, 'bytes': member.size, 'mode': member.mode, 'uid': member.uid, 'gid': member.gid,
                       'kind': 'file' if member.isfile() else 'directory'}
                manifest.append(row)
                if member.name == 'tree/initdb-password':
                    row['disposition'] = 'metadata_only_omitted'
                    continue
                if member.name in CONFIG_MEMBERS:
                    with archive.extractfile(member) as stream:
                        row['sha256'] = sha(stream.read())
                    row['disposition'] = 'hash_only_omitted'
                    continue
                target = self.volume.joinpath(*parts[1:])
                need(target == self.volume or target.is_relative_to(self.volume), 'copy_destination')
                if member.isdir():
                    if target != self.volume:
                        target.mkdir(mode=0o700)
                        os.chown(target, UID, GID)
                    continue
                need(target.parent.resolve() == target.parent, 'copy_parent_symlink')
                with archive.extractfile(member) as stream:
                    raw = stream.read(16 << 20)
                need(len(raw) == member.size, 'archive_member_truncated')
                if member.name == 'tree/data/PG_VERSION':
                    need(raw == b'17\n', 'archive_postgres_version')
                row['sha256'] = sha(raw)
                self.base.write_new(target, raw, mode=0o600, uid=UID, gid=GID)
        self.save('copy-manifest.json', manifest)
        self.pinned(ARCHIVE, 8 << 20)

    def configure_copy(self):
        config = '\n'.join((
            "data_directory = '" + str(self.data) + "'", "hba_file = '" + str(self.data / 'pg_hba.conf') + "'",
            "ident_file = '" + str(self.data / 'pg_ident.conf') + "'", "external_pid_file = ''", "listen_addresses = ''",
            "unix_socket_directories = ''", "shared_buffers = '16MB'", "work_mem = '4MB'", "maintenance_work_mem = '16MB'",
            "max_connections = 10", "max_wal_size = '64MB'", "min_wal_size = '32MB'", "temp_file_limit = '64MB'",
            "max_worker_processes = 0", "max_parallel_workers = 0", "max_parallel_workers_per_gather = 0",
            "autovacuum = off", "shared_preload_libraries = ''", "session_preload_libraries = ''", "local_preload_libraries = ''",
            "archive_mode = off", "archive_command = ''", "restore_command = ''", "primary_conninfo = ''",
            "logging_collector = off", "log_destination = 'stderr'", "log_min_messages = warning", "log_statement = 'none'",
            "default_transaction_read_only = on", "exit_on_error = on", "fsync = on", "full_page_writes = on",
            "synchronous_commit = on", "timezone = 'UTC'", "log_timezone = 'UTC'", ""))
        for name, raw in (('postgresql.conf', config.encode()), ('postgresql.auto.conf', b''),
                          ('pg_hba.conf', b'local all all reject\n'), ('pg_ident.conf', b'')):
            self.base.write_new(self.data / name, raw, mode=0o600, uid=UID, gid=GID)
        self.report['copyConfiguration'] = 'New explicit standalone configuration; old configuration and password members omitted.'

    def control(self, label):
        raw = self.command(label, [TOOL_PATHS['control'], '-D', str(self.data)])
        fields = dict(line.split(':', 1) for line in raw.decode('ascii').splitlines() if ':' in line)
        fields = {k.strip(): v.strip() for k, v in fields.items()}
        need(fields.get('Database cluster state') == 'shut down' and fields.get('Database system identifier') == self.marker['systemId'], 'physical_cluster_not_clean')
        return {'state': fields['Database cluster state'], 'systemId': fields['Database system identifier'], 'rawSha256': sha(raw)}

    def observe_child_exit(self):
        """Observe exit without releasing the PID that owns the fresh session."""
        need(self.child is not None and not self.child_reaped, 'child_already_reaped')
        result = os.waitid(os.P_PID, self.child.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT)
        if result is not None:
            need(result.si_pid == self.child.pid, 'child_wait_identity')
            self.child_exit = {'code': result.si_code, 'status': result.si_status}
        return self.child_exit

    def owned_group_members(self):
        """The unreaped direct child prevents reuse of this PID/session/PGID."""
        need(self.child is not None and not self.child_reaped, 'group_owner_already_reaped')
        self.observe_child_exit()
        members = []
        entries = list(Path('/proc').iterdir())
        need(len(entries) <= 16384, 'process_inventory_limit')
        for path in entries:
            if not path.name.isdigit():
                continue
            try:
                fields = (path / 'stat').read_text().rsplit(') ', 1)[1].split()
                if int(fields[2]) != self.child.pid:
                    continue
                owner = path.stat().st_uid
                need(int(fields[3]) == self.child.pid and owner == UID, 'child_group_owner_changed')
                if fields[0] not in ('Z', 'X'):
                    need(os.readlink(path / 'exe') == TOOL_PATHS['postgres'], 'unexpected_child_group_executable')
                if int(path.name) == self.child.pid and self.child_identity is not None:
                    need(fields[19] == self.child_identity['startTicks'], 'child_start_changed')
                members.append({'pid': int(path.name), 'state': fields[0], 'startTicks': fields[19]})
            except (FileNotFoundError, ProcessLookupError):
                continue
        return members

    def close_child(self):
        if self.child is None or self.child_reaped:
            return
        process = self.child
        members = self.owned_group_members()
        if self.child_exit is None or any(row['pid'] != process.pid for row in members):
            self.report['forcedChildClosure'] = True
            for sig in (signal.SIGTERM, signal.SIGKILL):
                members = self.owned_group_members()
                if any(row['state'] not in ('Z', 'X') for row in members):
                    os.killpg(process.pid, sig)
                until = min(self.deadline - 2, time.monotonic() + 5)
                while time.monotonic() < until:
                    members = self.owned_group_members()
                    if self.child_exit is not None and not any(row['pid'] != process.pid for row in members):
                        break
                    time.sleep(0.05)
        members = self.owned_group_members()
        need(self.child_exit is not None and not any(row['pid'] != process.pid for row in members), 'child_group_unclosed')
        # Only now release the unique child PID. Never address this PGID again.
        process.wait(timeout=1)
        self.child_reaped = True
        self.report['childExit'] = self.child_exit
        self.report['childClosed'] = True

    def capture_single(self, query, out, err):
        """Feed one fixed SQL input while draining both bounded output pipes."""
        process = self.child
        until = time.monotonic() + self.remaining(45)
        written = 0
        totals = {'stdout': 0, 'stderr': 0}
        streams = (process.stdin, process.stdout, process.stderr)
        try:
            with selectors.DefaultSelector() as selector:
                for stream, event, label in ((process.stdin, selectors.EVENT_WRITE, 'stdin'),
                                             (process.stdout, selectors.EVENT_READ, 'stdout'),
                                             (process.stderr, selectors.EVENT_READ, 'stderr')):
                    os.set_blocking(stream.fileno(), False)
                    selector.register(stream, event, label)
                while selector.get_map():
                    need(PENDING_SIGNAL is None, 'interrupted')
                    remaining = until - time.monotonic()
                    need(remaining > 0, 'single_child_timeout')
                    for key, _mask in selector.select(min(0.25, remaining)):
                        stream = key.fileobj
                        if key.data == 'stdin':
                            try:
                                written += os.write(stream.fileno(), query[written:])
                            except BlockingIOError:
                                continue
                            except BrokenPipeError:
                                selector.unregister(stream)
                                stream.close()
                                continue
                            if written == len(query):
                                self.report['standaloneSqlInputs'] = 1
                                selector.unregister(stream)
                                stream.close()
                            continue
                        try:
                            chunk = os.read(stream.fileno(), 65536)
                        except BlockingIOError:
                            continue
                        if not chunk:
                            selector.unregister(stream)
                            stream.close()
                            continue
                        totals[key.data] += len(chunk)
                        need(totals[key.data] <= 1 << 20, 'single_output_limit')
                        (out if key.data == 'stdout' else err).write(chunk)
                need(written == len(query), 'single_input_incomplete')
                while self.observe_child_exit() is None:
                    need(PENDING_SIGNAL is None, 'interrupted')
                    need(time.monotonic() < until, 'single_child_exit_timeout')
                    time.sleep(0.01)
            self.report['singleOutputBytes'] = totals
        finally:
            for stream in streams:
                if stream is not None and not stream.closed:
                    stream.close()

    def read_once(self):
        self.remaining(1)
        need(self.report['postgresStartAttempts'] == self.report['postgresStarts'] == self.report['standaloneSqlInputs'] == 0, 'single_attempt_consumed')
        query = sql_text()
        self.save('standalone-input.json', {'sqlSha256': sha(query), 'bytes': len(query)})
        self.base.write_new(self.base.P / 'standalone.sql', query)
        argv = [TOOL_PATHS['postgres'], '--single', '-D', str(self.data), '-c', 'config_file=' + str(self.data / 'postgresql.conf'), 'goby_m6_systemd']
        self.report['postgresStartAttempts'] = 1
        self.save('single-start-intent.json', {'argv': argv, 'uid': UID, 'gid': GID, 'sourceSha256': self.value['sourceSha256']})
        out_path, err_path = self.base.P / 'single.stdout', self.base.P / 'single.stderr'
        with open(out_path, 'xb') as out, open(err_path, 'xb') as err:
            parent_pid = os.getpid()
            try:
                old_mask = signal.pthread_sigmask(signal.SIG_BLOCK, CONTROL_SIGNALS)
                try:
                    self.child = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=dict(SAFE_ENV),
                                                  cwd=self.fixture, start_new_session=True, user=UID, group=GID, extra_groups=[],
                                                  preexec_fn=lambda: single_child_limits(parent_pid, old_mask))
                    self.report['postgresStarts'] = 1
                finally:
                    # A pending interrupt is delivered only after self.child is owned.
                    signal.pthread_sigmask(signal.SIG_SETMASK, old_mask)
                self.child_identity = self.base.process_identity(self.child.pid)
                need(self.child_identity['uid'] == UID and self.child_identity['exe'] == TOOL_PATHS['postgres'] and
                     os.getpgid(self.child.pid) == os.getsid(self.child.pid) == self.child.pid, 'single_process_identity')
                self.report['childIdentity'] = self.child_identity
                self.capture_single(query, out, err)
            finally:
                self.close_child()
        need(self.child.returncode == 0 and not self.report.get('forcedChildClosure'), 'single_child_failed')
        raw, errors = self.base.read_regular(out_path, 1 << 20), self.base.read_regular(err_path, 1 << 20)
        result = parse_frame(raw, errors)
        marker = result['identity']
        need(marker['pid'] == self.child.pid and marker['database'] == 'goby_m6_systemd' and
             type(marker['databaseOid']) is int and marker['databaseOid'] == self.marker['databaseOid'] and
             marker['systemId'] == self.marker['systemId'] and marker['dataDirectory'] == str(self.data) and
             marker['readOnly'] == 'on' and marker['isolation'] == 'repeatable read' and marker['recovery'] is False, 'single_result_identity')
        compare_snapshot(result['snapshot'], self.expected)
        self.save('standalone-parsed-private.json', result)
        self.report['comparison'] = {'nineTablesAndXminExact': True, 'allEightRevokedSessionsExact': True,
                                     'schemaAndServerIdExact': True, 'naturalSingleUserExitZero': True,
                                     'concurrentIsolationOrOriginalObserverClaimed': False}

    def run(self):
        need(PENDING_SIGNAL is None, 'interrupted')
        need(sys.flags.isolated and sys.flags.dont_write_bytecode and os.geteuid() == 0, 'python_or_owner_boundary')
        need(pwd.getpwnam('postgres').pw_uid == UID and pwd.getpwnam('postgres').pw_gid == GID, 'postgres_account_changed')
        need(not os.path.lexists(self.evidence) and not os.path.lexists(self.fixture), 'scope_already_exists')
        need(self.evidence.parent.resolve() == self.evidence.parent and self.fixture.parent.resolve() == self.fixture.parent, 'scope_parent_symlink')
        self.evidence.mkdir(mode=0o700)
        self.base.P.mkdir(mode=0o700)
        try:
            for pin in self.value['tools'].values():
                self.pinned(pin)
            need(sha(read_bootstrap(SEAL_PATH, 128 << 10)) == SEAL_SHA, 'sql_source_pin')
            self.lock_fd = os.open(LOCK, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
            fcntl.flock(self.lock_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            self.report['protectedBefore'] = self.protected()
            self.report['capacityBefore'] = self.capacity()
            self.expected = parse(self.pinned(SNAPSHOT, 1 << 20))
            lines = self.pinned(SNAPSHOT_RAW, 1 << 20).splitlines()
            need(len(lines) == 2 and encoded(parse(lines[1])) == encoded(self.expected), 'snapshot_raw_binding')
            self.marker = parse(lines[0])
            need(self.marker['database'] == 'goby_m6_systemd' and type(self.marker['databaseOid']) is int and
                 re.fullmatch('[0-9]+', self.marker['systemId']), 'expected_database_identity')
            self.create_fixture_directory()
            self.volume.mkdir(mode=0o700)
            self.report['underlyingVolume'] = self.base.metadata(self.volume)
            self.report['mountAttempted'] = True
            self.command('mount-new-copy', [TOOL_PATHS['mount'], '-t', 'tmpfs', '-o',
                         'size=512M,nr_inodes=8192,uid=103,gid=106,mode=0700,nosuid,nodev,noexec',
                         'goby-r04-archive-' + self.value['scopeId'], str(self.volume)])
            self.mount = self.mount_identity()
            self.report['mount'] = self.mount
            self.extract_copy()
            self.configure_copy()
            self.report['controlBefore'] = self.control('control-before-single')
            self.read_once()
            self.report['controlAfter'] = self.control('control-after-single')
            self.report['status'] = 'archived_catalog_equal_pending_closure'
        except BaseException as error:
            self.report['errors'].append(self.error(error))
            self.report['status'] = 'failed'
        finally:
            self.cleaning = True
            if PENDING_SIGNAL is not None:
                self.report['interruptedBySignal'] = PENDING_SIGNAL
            def cleanup_signal(signum, _frame):
                signals = self.report.setdefault('cleanupSignals', [])
                if signum not in signals:
                    signals.append(signum)
            for signum in CONTROL_SIGNALS:
                signal.signal(signum, cleanup_signal)
            for name, action in [('child', self.close_child), ('volume', self.close_volume), ('originals', self.check_originals)]:
                try:
                    action()
                except BaseException as error:
                    self.report['cleanupErrors'].append({'phase': name, **self.error(error)})
            if self.lock_fd is not None:
                fcntl.flock(self.lock_fd, fcntl.LOCK_UN)
                os.close(self.lock_fd)
                self.lock_fd = None
            if self.report['status'] == 'archived_catalog_equal_pending_closure' and not self.report['cleanupErrors']:
                self.report['status'] = 'archived_catalog_equal_and_owned_copy_closed'
            self.report['lockReleased'] = self.lock_fd is None
            pin = self.save('result.json', self.report)
            print(json.dumps({'status': self.report['status'], 'result': {'path': pin['path'], 'sha256': pin['sha256']},
                              'installationAccepted': False, 'originalFinalObserverRecreated': False}))
        return 0 if self.report['status'] == 'archived_catalog_equal_and_owned_copy_closed' else 1

    def close_volume(self):
        if not self.report.get('mountAttempted'):
            return
        present = [line for line in Path('/proc/self/mountinfo').read_text().splitlines() if line.split()[4] == str(self.volume)]
        if not present:
            need(self.mount is None, 'owned_mount_disappeared')
            return
        need(self.report.get('childClosed') is True or self.child is None, 'volume_child_not_closed')
        current = self.mount_identity()
        if self.mount is not None:
            need(all(current[k] == self.mount[k] for k in ('id', 'device', 'root', 'path')), 'owned_mount_changed')
        need(Path.cwd() != self.volume and not Path.cwd().is_relative_to(self.volume), 'controller_inside_volume')
        self.command('unmount-owned-copy', [TOOL_PATHS['umount'], str(self.volume)])
        need(not any(line.split()[4] == str(self.volume) for line in Path('/proc/self/mountinfo').read_text().splitlines()), 'owned_mount_remains')
        need(self.base.metadata(self.volume) == self.report['underlyingVolume'], 'underlying_volume_changed')
        self.report['volumeClosed'] = True
        self.report['retainedEmptyDirectories'] = [str(self.fixture), str(self.volume)]

    def check_originals(self):
        for pin in (ARCHIVE, SNAPSHOT, SNAPSHOT_RAW, *self.value['tools'].values()):
            self.pinned(pin)
        need(sha(read_bootstrap(BASE_PATH, 128 << 10)) == BASE_SHA and
             sha(read_bootstrap(SEAL_PATH, 128 << 10)) == SEAL_SHA and
             sha(read_bootstrap(Path(__file__).resolve(), 64 << 10)) == self.value['sourceSha256'], 'source_changed')
        self.report['protectedAfter'] = self.protected()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input', required=True)
    args = parser.parse_args()
    try:
        need(sys.flags.isolated and sys.flags.dont_write_bytecode and os.geteuid() == 0, 'python_or_owner_boundary')
        value = parse(read_bootstrap(Path(args.input), 256 << 10))
        validate_input(value)
        need(sha(read_bootstrap(Path(__file__).resolve(), 64 << 10)) == value['sourceSha256'], 'reader_source_pin')
        os.umask(0o077)
        def interrupted(_signum, _frame):
            # Defer the exception until a command/child handle is safely recorded.
            global PENDING_SIGNAL
            if PENDING_SIGNAL is None:
                PENDING_SIGNAL = _signum
        for signum in CONTROL_SIGNALS:
            signal.signal(signum, interrupted)
        resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
        return Reader(value, load_base()).run()
    except BaseException as error:
        print(json.dumps({'status': 'entry_rejected', 'errorClass': type(error).__name__}))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
