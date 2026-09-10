#!/usr/bin/env python3
"""Deploy one accepted M5j candidate to the existing isolated Linux test service.

Install: --candidate-manifest PATH --manifest-sha256 SHA256.
Recover an unpublished interrupted attempt: --restore.
Finalize the specifically pinned healthy interrupted installation without
restarting or restoring it: --finalize --candidate-manifest PATH
--manifest-sha256 SHA256 --remediation-report PATH --remediation-sha256 SHA256.

The canonical candidate JSON contains owner, binary, assets, source and gates.
binary={path,sha256,size}; assets={path,sha256,files}; source={path,resolved_path,
owner_marker,files}, where files maps every Go/module/migration and compiled
PostgreSQL catalog JSON path to SHA-256.
gates has final_go/full/browser/recovery entries, each {status,evidence_path,sha256}.
Every gate must explicitly be passed and its evidence bytes must match.

All old public rows and the dump use one exported PostgreSQL snapshot. The
complete private backup, generated recovery SQL and their byte manifest are
durable and checked before stopping the service. Media remain in place and
are protected by full-byte hashes and file identities. Runtime, unit, drop-in
and the matching master are never rewritten. One new owned drop-in provisions
three private recovery directories and a separately owned empty recovery
database. Recovery removes only the exact new configuration and independently
owned database; local state and diagnostic files are retained as evidence.
Before service stop, the complete archive is actually restored into a fresh
owned disposable database and all
twenty-nine old tables must match exactly. Restore preserves the existing public
schema and restores its application objects in one transaction. The rehearsal
is recorded separately from restoration of the live service; pure tests prove
neither kind of real database restore.

Migration 23 preserves the same twenty-nine public tables. Initial startup
may add only one server_settings binding for the newly owned local primary
deployment, with an empty generation; all preexisting rows remain exact.
This operator uses readiness and static-asset GET requests only. Product
backup creation and download acceptance is a separate deployed workflow;
destructive product restore and rollback remain isolated fixture work.
"""

import argparse
import base64
from contextlib import contextmanager
from datetime import datetime, timezone
import fcntl
import hashlib
import hmac
import http.client
import json
import os
from pathlib import Path, PurePosixPath
import re
import secrets
import selectors
import shlex
import shutil
import signal
import stat
import subprocess
import sys
import tarfile
import time
from urllib.parse import unquote, urlsplit

sys.dont_write_bytecode = True
ROOT = Path('/opt/goby-test')
LIVE = Path('/opt/goby-dev')
REPOSITORY = ROOT / 'repository'
SCRATCH = ROOT / 'exec-scratch'
BACKUP = ROOT / 'backups/m5j-20260910'
EVIDENCE = BACKUP / 'm5j-deployment-evidence.json'
RECOVERY_EVIDENCE = BACKUP / 'm5j-recovery-evidence.json'
INSTALLATION_STARTED = BACKUP / 'installation-started.json'
DEPLOYMENT_LOCK = SCRATCH / '.m5j-deployment.lock'
DEPLOYMENT_LOCK_OWNER = b'goby-m5j-deployment-lock-v1\n'
DEPLOYMENT_LOCK_HELD = False
FINALIZATION_READ_ONLY = False
FINALIZATION_CANDIDATE_SHA = '8d6c9f55107682329fe94ec70cbb2f8e32af5609a0c23107d38ecbcfb2f4593b'
FINALIZATION_BINARY_SHA = 'a4abba7b289ceb74ca0916484d714dc6baa1b3c5230b06964b89d363a64e8b81'
FINALIZATION_BINARY_BYTES = 25715909
FINALIZATION_PID = 3750313
FINALIZATION_START = '28911319'
FINALIZATION_ORIGINAL_OPERATOR_SHA = '610be9cba0910b7edcac1d576be3b3dca7d325954a79e12e9e684872574bf154'
FINALIZATION_REQUIRED_TESTS = {
    'test_systemd_properties_preserves_requested_repeated_values',
    'test_systemd_properties_rejects_missing_unknown_and_duplicate_singletons',
    'test_protected_state_preserves_legacy_evidence_with_real_environment_file_lines',
    'test_finalize_refuses_unpublished_scope_drift',
    'test_finalize_publishes_only_after_final_readonly_revalidation',
    'test_finalize_failure_never_enters_install_or_restore',
    'test_remediation_report_binds_operator_suite_and_required_contracts',
}
OWNER = 'goby-m5j-deployment-backup-v1'
CANDIDATE_OWNER = 'goby-m5j-candidate-v1'
SERVICE = 'goby-foundation-test.service'
UNIT = Path('/etc/systemd/system/goby-foundation-test.service')
DROPIN = Path('/etc/systemd/system/goby-foundation-test.service.d/20-application-keys.conf')
OBSERVABILITY_DROPIN = DROPIN.with_name('30-observability.conf')
LOG_DIRECTORY = Path('/var/log/goby-test')
OBSERVABILITY_BYTES = (b'[Service]\nEnvironment=GOBY_LOG_DIR=/var/log/goby-test\n'
                       b'LogsDirectory=goby-test\nLogsDirectoryMode=0700\n')
RECOVERY_DROPIN = DROPIN.with_name('40-backup-recovery.conf')
RECOVERY_ENV = ROOT / 'recovery-m5j.env'
RECOVERY_PARENT = Path('/var/lib/goby-test')
RECOVERY_DIRECTORY = RECOVERY_PARENT / 'recovery-m5j'
BACKUP_DIRECTORY = RECOVERY_PARENT / 'backups-m5j'
OPERATIONS_DIRECTORY = RECOVERY_PARENT / 'recovery-operations-m5j'
RECOVERY_DIRECTORIES = (RECOVERY_DIRECTORY, BACKUP_DIRECTORY, OPERATIONS_DIRECTORY)
RECOVERY_INTENT = BACKUP / 'recovery-installation-intent.json'
RECOVERY_BYTES = (b'[Service]\n'
                  b'Environment=GOBY_RECOVERY_STATE_DIR=/var/lib/goby-test/recovery-m5j\n'
                  b'Environment=GOBY_BACKUP_DIR=/var/lib/goby-test/backups-m5j\n'
                  b'Environment=GOBY_RECOVERY_OPERATIONS_DIR=/var/lib/goby-test/recovery-operations-m5j\n'
                  b'Environment=GOBY_PG_DUMP=/usr/lib/postgresql/17/bin/pg_dump\n'
                  b'Environment=GOBY_PG_RESTORE=/usr/lib/postgresql/17/bin/pg_restore\n'
                  b'EnvironmentFile=/opt/goby-test/recovery-m5j.env\n'
                  b'ReadWritePaths=/var/lib/goby-test/recovery-m5j /var/lib/goby-test/backups-m5j /var/lib/goby-test/recovery-operations-m5j\n')
RECOVERY_BINDING_KEY = 'goby.recovery.binding.v1'
RUNTIME = ROOT / 'runtime.env'
VAULT = Path('/var/lib/goby-test/application-key-vault')
MASTER = VAULT / 'master.key'
OLD = '1ead2fcaa22df227d3d8b6b607978887ccfee7868139fc38f40523dbe24752ef'
OLD_PID = 3668655
OLD_START = '26912384'
OLD_SOURCE_MANIFEST_SHA = '0d3d597cb5a7369cc735f3728633edf223b70f03487e44ab9c3f839150919594'
OLD_SOURCE_COUNT = 470
OLD_ASSETS_ARCHIVE = SCRATCH / 'goby-m5i-admin-assets.tar.gz'
OLD_ASSETS_SHA = '20ed0e16b721515f1e56dddeef818e3102a7c92f25f2a1d8d08aa1d2b756f85b'
OLD_ASSET_COUNT = 54
REFERENCE_SERVICE = 'goby-emby-reference.service'
REFERENCE_PID = 3131777
REFERENCE_START = '13964831'
BASE_SCHEMA, TARGET_SCHEMA = 22, 23
MIB = 1024 * 1024
HASH = re.compile(r'[0-9a-f]{64}')
OLD_TABLES = set('schema_migrations server_settings users sessions libraries library_roots items scan_jobs '
                 'catalog_entities item_entities item_images user_item_data play_sessions item_subtitles '
                 'encoding_jobs client_playback_references item_metadata_state application_keys application_key_clients '
                 'devices application_key_devices task_definitions task_triggers task_runs task_run_requests '
                 'task_run_children task_occurrences managed_settings activity_entries'.split())
ADDED_TABLES = set()
# Migration 23 changes only existing activity constraints and one index.
# pg_restore --clean recreates the baseline objects within the same public
# schema. No additional table drop, CASCADE or schema replacement is needed.
UPGRADE_DROP_SQL = b''



class Failure(RuntimeError):
    """Expose a fixed operator assertion, never raw command or secret text."""


class Interrupted(BaseException):
    """Enter controlled recovery instead of terminating between mutations."""


def check(condition, label):
    if not condition:
        raise Failure(label)


def interrupt_signal(_number, _frame):
    raise Interrupted()


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=True, allow_nan=False).encode()


def sha(path):
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def file_identity(info):
    return [info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns,
            info.st_mode, info.st_uid, info.st_gid, info.st_nlink]


def regular(path, maximum=128 * MIB, mode=None, owner=0):
    info = path.lstat()
    check(stat.S_ISREG(info.st_mode) and info.st_uid == owner and info.st_nlink == 1 and
          path.resolve(strict=True) == path and info.st_size <= maximum and
          (mode is None or stat.S_IMODE(info.st_mode) == mode),
          'An expected file changed type, owner, mode, size or path')
    return info


def directory(path, mode=None, owner=0):
    info = path.lstat()
    check(stat.S_ISDIR(info.st_mode) and info.st_uid == owner and path.resolve(strict=True) == path and
          info.st_mode & 0o022 == 0 and (mode is None or stat.S_IMODE(info.st_mode) == mode),
          'An expected directory has unsafe type, ownership or permissions')
    return info


def private(path, payload):
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb') as output:
        output.write(payload)
        output.flush()
        os.fsync(output.fileno())


def sync_directory(path):
    descriptor = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def publish_exclusive(path, payload):
    # The final name appears only after the complete payload is fsynced. link
    # is atomic and refuses an existing destination; no report is overwritten.
    stage = path.with_name('.' + path.name + '.' + secrets.token_hex(8))
    created = False
    try:
        private(stage, payload)
        created = True
        os.link(stage, path, follow_symlinks=False)
        sync_directory(path.parent)
    finally:
        if created:
            stage.unlink()
            sync_directory(path.parent)


@contextmanager
def deployment_lock():
    global DEPLOYMENT_LOCK_HELD
    check(not DEPLOYMENT_LOCK_HELD, 'The protected deployment lock is not reentrant')
    directory(SCRATCH, 0o700)
    created = False
    try:
        descriptor = os.open(DEPLOYMENT_LOCK, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        created = True
    except FileExistsError:
        regular(DEPLOYMENT_LOCK, len(DEPLOYMENT_LOCK_OWNER), 0o600)
        descriptor = os.open(DEPLOYMENT_LOCK, os.O_RDWR | os.O_NOFOLLOW)
    try:
        info = os.fstat(descriptor)
        check(pinned_identity(info) == pinned_identity(regular(DEPLOYMENT_LOCK, len(DEPLOYMENT_LOCK_OWNER), 0o600)),
              'The deployment lock changed before its descriptor was opened')
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise Failure('Another protected M5j deployment or recovery operator is active') from None
        if created:
            check(info.st_size == 0 and os.write(descriptor, DEPLOYMENT_LOCK_OWNER) == len(DEPLOYMENT_LOCK_OWNER),
                  'The exclusive deployment lock marker could not be persisted')
            os.fsync(descriptor)
            sync_directory(SCRATCH)
            os.lseek(descriptor, 0, os.SEEK_SET)
        check(os.read(descriptor, len(DEPLOYMENT_LOCK_OWNER) + 1) == DEPLOYMENT_LOCK_OWNER and
              pinned_identity(os.fstat(descriptor)) == pinned_identity(regular(DEPLOYMENT_LOCK, len(DEPLOYMENT_LOCK_OWNER), 0o600)),
              'The deployment lock owner marker or inode differs')
        DEPLOYMENT_LOCK_HELD = True
        yield
    finally:
        DEPLOYMENT_LOCK_HELD = False
        # Keep the fixed inode in place. Unlinking a lock file would let another
        # process obtain a different lock while an existing waiter still ran.
        os.close(descriptor)


def command(argv, label, *, environment=None, data=None, timeout=60, limit=32 * MIB):
    if FINALIZATION_READ_ONLY:
        observation = argv[:2] == ['/usr/bin/systemctl', 'show']
        postgres = (argv[:1] == ['/usr/bin/psql'] or
                    argv[:5] == ['/usr/sbin/runuser', '-u', 'postgres', '--', '/usr/bin/psql'])
        check(observation or postgres and environment is not None and
              '-c default_transaction_read_only=on' in environment.get('PGOPTIONS', ''),
              'Forward finalization refuses a command outside read-only service or PostgreSQL observation')
    result = subprocess.run(argv, env=environment, input=data, capture_output=True, timeout=timeout)
    if result.returncode != 0 or len(result.stdout) > limit or len(result.stderr) > MIB:
        # Failure diagnostics are private, separate from immutable backup
        # proofs. Raw stderr is never emitted in the terminal or public report.
        if not FINALIZATION_READ_ONLY and BACKUP.is_dir() and BACKUP.resolve() == BACKUP:
            name = 'command-error-' + secrets.token_hex(8) + '.json'
            private(BACKUP / name, canonical({'operation': label, 'returncode': result.returncode,
                                             'stderr': result.stderr[:MIB].decode('utf-8', 'replace')}))
        raise Failure(label + (' failed during read-only finalization' if FINALIZATION_READ_ONLY else
                               ' failed; any command diagnostic remains in the private M5j backup'))
    return result.stdout


def private_values(wanted):
    regular(RUNTIME, 65536, 0o600)
    result = {}
    for line in RUNTIME.read_text().splitlines():
        name, separator, raw = line.strip().removeprefix('export ').partition('=')
        if separator and name in wanted:
            values = shlex.split(raw, comments=False, posix=True)
            check(name not in result and len(values) == 1 and values[0], 'A private runtime assignment is ambiguous')
            result[name] = values[0]
    check(set(result) == set(wanted), 'A required private runtime assignment is missing')
    return result


class Database:
    def __init__(self):
        parsed = urlsplit(private_values({'GOBY_DATABASE_URL'})['GOBY_DATABASE_URL'])
        check(parsed.scheme in {'postgres', 'postgresql'} and parsed.hostname == '127.0.0.1' and
              parsed.port in {None, 5432} and unquote(parsed.path) == '/goby_test' and
              unquote(parsed.username or '') == 'goby_test' and parsed.password and
              parsed.query in {'', 'sslmode=disable'} and not parsed.fragment,
              'The database is outside the dedicated local test scope')
        self.environment = {'PATH': '/usr/bin:/bin', 'LANG': 'C.UTF-8', 'PGHOST': '127.0.0.1', 'PGPORT': '5432',
                            'PGDATABASE': 'goby_test', 'PGUSER': 'goby_test', 'PGPASSWORD': unquote(parsed.password),
                            'PGPASSFILE': '/dev/null', 'PGCONNECT_TIMEOUT': '5', 'PGSSLMODE': 'disable',
                            'PGCLIENTENCODING': 'UTF8', 'PGOPTIONS': '-c default_transaction_read_only=on '
                            '-c statement_timeout=15000 -c lock_timeout=3000 -c timezone=UTC -c bytea_output=hex -c DateStyle=ISO,YMD'}

    def read(self, query):
        data = command(['/usr/bin/psql', '-X', '-q', '-A', '-t', '-v', 'ON_ERROR_STOP=1'],
                       'Read-only database observation', environment=self.environment, data=query.encode(), timeout=25)
        return json.loads(data)


@contextmanager
def exported_snapshot(database):
    process = subprocess.Popen(['/usr/bin/psql', '-X', '-q', '-A', '-t', '-v', 'ON_ERROR_STOP=1'],
                               stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                               env=database.environment)
    try:
        process.stdin.write(b'BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SELECT pg_export_snapshot();\n')
        process.stdin.flush()
        data = bytearray()
        deadline = time.monotonic() + 10
        with selectors.DefaultSelector() as selector:
            selector.register(process.stdout, selectors.EVENT_READ)
            while b'\n' not in data:
                check(time.monotonic() < deadline, 'Exporting the backup snapshot timed out')
                if selector.select(max(0, min(0.25, deadline - time.monotonic()))):
                    block = os.read(process.stdout.fileno(), 256)
                    check(block and len(data) + len(block) <= 256, 'The backup snapshot export failed')
                    data.extend(block)
        value = data.decode().strip()
        check(re.fullmatch(r'[0-9A-Fa-f]+-[0-9A-Fa-f]+-[0-9]+', value), 'The exported backup snapshot identity is invalid')
        yield value
    finally:
        if process.poll() is None:
            try:
                process.communicate(input=b'ROLLBACK;\n\\q\n', timeout=5)
            except BaseException:
                process.kill()
                process.wait(timeout=5)
        for stream in (process.stdin, process.stdout, process.stderr):
            stream.close()


def snapshot(database, version, exported=None, check_catalog=True):
    tables = OLD_TABLES | (ADDED_TABLES if version == TARGET_SCHEMA else set())
    parts = ["'" + name + "',COALESCE((SELECT json_agg(raw ORDER BY raw) FROM "
             '(SELECT to_jsonb(t)::text raw FROM public."' + name + '" t) source_rows),\'[]\'::json)' for name in sorted(tables)]
    query = 'BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; '
    if exported is not None:
        check(re.fullmatch(r'[0-9A-Fa-f]+-[0-9A-Fa-f]+-[0-9]+', exported), 'A backup snapshot identity is invalid')
        query += "SET TRANSACTION SNAPSHOT '" + exported + "'; "
    query += ("SELECT json_build_object('inventory',(SELECT json_agg(tablename ORDER BY tablename) FROM pg_tables WHERE schemaname='public'),"
              "'rows',json_build_object(" + ','.join(parts) + ')); COMMIT;')
    result = database.read(query)
    check(set(result['inventory']) == tables and set(result['rows']) == tables and
          sum(map(len, result['rows'].values())) <= 10000, 'The complete public table inventory or row bound differs')
    rows = result['rows']
    for values in rows.values():
        values.sort()
    migrations = parsed(rows, 'schema_migrations')
    check(len(migrations) == version and {row['version'] for row in migrations} == set(range(1, version + 1)), 'The schema version differs')
    media = [item['media'] for item in parsed(rows, 'items') if item['media'] is not None]
    if check_catalog:
        check(len(rows['items']) == 21 and len(media) == 11 and len(rows['libraries']) == 5 and
              all(item.get('ProbeVersion') == 6 for item in media), 'The accepted catalog or media probe version differs')
    return rows


def parsed(value, table):
    return [json.loads(raw) for raw in value[table]]


def summaries(value):
    return {name: {'count': len(rows), 'sha256': hashlib.sha256(canonical(rows)).hexdigest()} for name, rows in sorted(value.items())}


def systemd_properties(raw, names, repeated=()):
    # systemctl emits one EnvironmentFiles= line per configured file. Keep
    # those values ordered; a dictionary comprehension silently drops all but
    # the last file. Every other requested property must occur exactly once.
    check(isinstance(raw, bytes) and len(set(names)) == len(names) and set(repeated) <= set(names),
          'The requested systemd property contract is invalid')
    try:
        lines = raw.decode('utf-8').splitlines()
    except UnicodeDecodeError:
        raise Failure('A systemd property response is not valid UTF-8') from None
    result = {}
    for line in lines:
        name, separator, value = line.partition('=')
        check(separator and name in names and (name in repeated or name not in result),
              'A systemd property response has an unknown, malformed or duplicate singleton entry')
        if name in repeated:
            result.setdefault(name, []).append(value)
        else:
            result[name] = value
    check(set(result) == set(names), 'A required systemd property is missing')
    return result


def process_identity(expected):
    values = systemd_properties(command(['/usr/bin/systemctl', 'show', SERVICE, '-p', 'MainPID', '-p', 'User', '-p', 'ActiveState'],
                                        'Service identity observation'), ('MainPID', 'User', 'ActiveState'))
    check(values.get('User') == 'goby' and values.get('ActiveState') == 'active', 'The expected service is not active as goby')
    pid = int(values.get('MainPID', '0'))
    check(pid > 1, 'The expected service has no live process')
    process = Path('/proc') / str(pid)
    status_lines = (process / 'status').read_text().splitlines()
    uid = next(line for line in status_lines if line.startswith('Uid:'))
    gid = next(line for line in status_lines if line.startswith('Gid:'))
    check([int(value) for value in uid.split()[1:]] == [995] * 4 and
          [int(value) for value in gid.split()[1:]] == [986] * 4 and sha(process / 'exe') == expected,
          'The active service executable, UID or GID differs')
    start = (process / 'stat').read_text().rsplit(') ', 1)[1].split()[19]
    return {'main_pid': pid, 'uid': 995, 'gid': 986, 'start_ticks': start, 'binary_sha256': expected}


def reference_state():
    values = systemd_properties(command(
        ['/usr/bin/systemctl', 'show', REFERENCE_SERVICE, '-p', 'MainPID', '-p', 'ActiveState'],
        'Original reference process observation'), ('MainPID', 'ActiveState'))
    check(values == {'MainPID': str(REFERENCE_PID), 'ActiveState': 'active'},
          'The unrelated original reference process changed')
    process = Path('/proc') / str(REFERENCE_PID)
    start = (process / 'stat').read_text().rsplit(') ', 1)[1].split()[19]
    check(start == REFERENCE_START, 'The original reference PID was reused')
    return {'pid': REFERENCE_PID, 'start_ticks': start,
            'binary_sha256': sha(process / 'exe')}


def protected_state(allow_recovery=False):
    regular(RUNTIME, 65536, 0o600)
    check(private_values({'GOBY_API_KEY_MASTER_KEY_FILE'})['GOBY_API_KEY_MASTER_KEY_FILE'] == str(MASTER),
          'The existing matching-master assignment differs')
    directory(VAULT, 0o700, 995)
    info = regular(MASTER, 32, 0o600, 995)
    check(info.st_size == 32, 'The matching master must remain exactly thirty-two bytes')
    properties = ('FragmentPath', 'DropInPaths', 'User', 'Group', 'WorkingDirectory', 'ProtectSystem', 'ReadWritePaths',
                  'EnvironmentFiles', 'UMask', 'NoNewPrivileges', 'StateDirectory', 'LogsDirectory', 'LogsDirectoryMode')
    argv = ['/usr/bin/systemctl', 'show', SERVICE]
    for name in properties:
        argv.extend(['-p', name])
    unit = systemd_properties(command(argv, 'Service configuration observation'), properties, repeated=('EnvironmentFiles',))
    original_dropins = {str(DROPIN), str(OBSERVABILITY_DROPIN)}
    expected_dropins = (original_dropins, original_dropins | {str(RECOVERY_DROPIN)}) if allow_recovery else (original_dropins,)
    original_writes = {'/dev/shm/goby-transcodes-test', str(VAULT)}
    new_writes = original_writes | {str(path) for path in RECOVERY_DIRECTORIES}
    runtime_files = str(RUNTIME) + ' (ignore_errors=no)'
    original_environment = [runtime_files]
    recovery_environment = [runtime_files, str(RECOVERY_ENV) + ' (ignore_errors=no)']
    check(set(unit) == set(properties) and unit['FragmentPath'] == str(UNIT) and set(unit['DropInPaths'].split()) in expected_dropins and
          unit['User'] == unit['Group'] == 'goby' and unit['WorkingDirectory'] == '/var/lib/goby-test' and
          unit['ProtectSystem'] == 'strict' and
          set(unit['ReadWritePaths'].split()) in ((original_writes, new_writes) if allow_recovery else (original_writes,)) and
          unit['EnvironmentFiles'] in ((original_environment, recovery_environment) if allow_recovery else (original_environment,)) and
          unit['UMask'] == '0077' and unit['NoNewPrivileges'] == 'yes' and
          unit['StateDirectory'] == '' and
          unit['LogsDirectory'] == 'goby-test' and unit['LogsDirectoryMode'] == '0700',
          'The original service configuration differs')
    if str(RECOVERY_DROPIN) in set(unit['DropInPaths'].split()):
        check(set(unit['ReadWritePaths'].split()) == new_writes and unit['EnvironmentFiles'] == recovery_environment,
              'The loaded recovery drop-in has incomplete sandbox or environment additions')
    else:
        check(set(unit['ReadWritePaths'].split()) == original_writes and unit['EnvironmentFiles'] == original_environment,
              'The original service has unowned sandbox or environment additions')
    if RECOVERY_DROPIN.exists() or RECOVERY_DROPIN.is_symlink():
        check(allow_recovery, 'An unexpected recovery drop-in exists before deployment')
        installed_recovery_dropin_identity()
    # The new settings are verified independently. Remove only these explicit
    # additions so all original service properties retain an exact comparison.
    unit['DropInPaths'] = ' '.join(sorted(original_dropins))
    unit['ReadWritePaths'] = ' '.join(sorted(original_writes))
    unit['EnvironmentFiles'] = runtime_files
    for path, mode in ((UNIT, 0o600), (DROPIN, 0o644), (OBSERVABILITY_DROPIN, 0o644)):
        regular(path, 65536, mode)
    check(OBSERVABILITY_DROPIN.read_bytes() == OBSERVABILITY_BYTES,
          'The accepted existing diagnostic drop-in changed')
    log_info = directory(LOG_DIRECTORY, 0o700, 995)
    check(log_info.st_gid == 986, 'The accepted existing diagnostic directory group changed')
    return {'runtime': {'identity': file_identity(RUNTIME.lstat()), 'sha256': sha(RUNTIME)},
            'master': {'identity': file_identity(info), 'sha256': sha(MASTER)}, 'vault_identity': file_identity(VAULT.lstat()),
            'unit_sha256': sha(UNIT), 'dropin_sha256': sha(DROPIN),
            'observability_dropin_sha256': sha(OBSERVABILITY_DROPIN), 'log_directory_identity': pinned_identity(log_info),
            'service': unit, 'reference': reference_state()}


def pinned_identity(info):
    return [info.st_dev, info.st_ino, stat.S_IMODE(info.st_mode), info.st_uid, info.st_gid]


def recovery_baseline():
    return {'owner': OWNER, 'directories': [str(path) for path in RECOVERY_DIRECTORIES],
            'directories_absent': True, 'dropin': str(RECOVERY_DROPIN), 'dropin_absent': True,
            'dropin_sha256': hashlib.sha256(RECOVERY_BYTES).hexdigest()}


def recovery_policy_name(name):
    return name.startswith(('GOBY_RECOVERY_', 'GOBY_BACKUP_')) or name in {'GOBY_PG_DUMP', 'GOBY_PG_RESTORE'}


def preflight_recovery():
    parent = directory(RECOVERY_PARENT, 0o750, 995)
    check(parent.st_gid == 986, 'The original service working directory group differs')
    directory(RECOVERY_DROPIN.parent, 0o755)
    for path in (*RECOVERY_DIRECTORIES, RECOVERY_DROPIN, RECOVERY_ENV):
        check(not path.exists() and not path.is_symlink(),
              'A preexisting recovery path is outside this new deployment ownership')
    # EnvironmentFile assignments override Environment=. Reject conflicts
    # before creating any state, including an alternative private database URL.
    regular(RUNTIME, 65536, 0o600)
    for line in RUNTIME.read_text().splitlines():
        name, separator, _value = line.strip().removeprefix('export ').partition('=')
        check(not separator or not recovery_policy_name(name),
              'A preexisting runtime override conflicts with the new recovery policy')
    effective = command(['/usr/bin/systemctl', 'show', SERVICE, '-p', 'Environment', '--value'],
                        'Preexisting recovery policy observation').decode()
    check(not any(recovery_policy_name(value.partition('=')[0])
                  for value in shlex.split(effective, comments=False, posix=True)),
          'A preexisting systemd override conflicts with the new recovery policy')
    for path in (Path('/usr/lib/postgresql/17/bin/pg_dump'), Path('/usr/lib/postgresql/17/bin/pg_restore')):
        info = regular(path, 16 * MIB, 0o755)
        check(info.st_gid == 0, 'A PostgreSQL command has an unexpected group')
    check(shutil.disk_usage(RECOVERY_PARENT).free > 768 * MIB,
          'The private recovery stores lack their minimum free-space allowance')
    return recovery_baseline()


def installed_recovery_dropin_identity():
    info = RECOVERY_DROPIN.lstat()
    check(stat.S_ISREG(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o644 and info.st_uid == 0 and
          info.st_gid == 0 and info.st_nlink in {1, 2} and info.st_size == len(RECOVERY_BYTES) and
          RECOVERY_DROPIN.resolve(strict=True) == RECOVERY_DROPIN and RECOVERY_DROPIN.read_bytes() == RECOVERY_BYTES,
          'The new recovery drop-in changed its fixed bytes, path, type, owner or mode')
    return pinned_identity(info)


def load_recovery_intent(backup):
    if not RECOVERY_INTENT.exists() and not RECOVERY_INTENT.is_symlink():
        return None
    regular(RECOVERY_INTENT, 8192, 0o600)
    value = json.loads(RECOVERY_INTENT.read_bytes())
    check(isinstance(value, dict) and set(value) == {'owner', 'candidate_sha256', 'baseline', 'stage', 'identity', 'directories'} and
          value['owner'] == OWNER and value['candidate_sha256'] == backup['candidate_sha256'] and
          value['baseline'] == recovery_baseline() and value['stage'] == str(BACKUP / 'recovery-dropin.stage') and
          isinstance(value['identity'], list) and len(value['identity']) == 5 and
          all(type(part) is int for part in value['identity']) and value['identity'][2:] == [0o644, 0, 0] and
          isinstance(value['directories'], dict) and set(value['directories']) == {str(path) for path in RECOVERY_DIRECTORIES} and
          all(isinstance(parts, list) and len(parts) == 5 and all(type(part) is int for part in parts) and
              parts[2:] == [0o700, 995, 986] for parts in value['directories'].values()),
          'The durable recovery installation intent has an unexpected binding')
    return value


def install_recovery(backup):
    check(installation_proven(backup), 'Recovery provisioning requires durable installation authorization')
    check(json.loads((BACKUP / 'recovery-before.json').read_bytes()) == recovery_baseline(),
          'Recovery provisioning has no accepted initial absence proof')
    check(command(['/usr/bin/systemctl', 'show', SERVICE, '-p', 'MainPID', '--value'],
                  'Recovery provisioning stopped-process observation').strip() == b'0',
          'The service must be stopped before private recovery directory provisioning')
    directory(RECOVERY_PARENT, 0o750, 995)
    directory(RECOVERY_DROPIN.parent, 0o755)
    for path in (*RECOVERY_DIRECTORIES, RECOVERY_DROPIN):
        check(not path.exists() and not path.is_symlink(), 'A recovery target appeared after its initial absence proof')
    directories = {}
    for path in RECOVERY_DIRECTORIES:
        # mkdir refuses an existing target. The descriptor pins the exact new
        # inode before changing ownership; recovery never deletes these stores.
        path.mkdir(mode=0o700)
        initial = directory(path, 0o700)
        descriptor = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            check(pinned_identity(os.fstat(descriptor)) == pinned_identity(initial),
                  'A new private recovery directory changed before its descriptor was opened')
            os.fchown(descriptor, 995, 986)
            os.fchmod(descriptor, 0o700)
            os.fsync(descriptor)
            final = os.fstat(descriptor)
        finally:
            os.close(descriptor)
        check(pinned_identity(final) == pinned_identity(directory(path, 0o700, 995)) and final.st_gid == 986,
              'The new recovery directory ownership changed during provisioning')
        directories[str(path)] = pinned_identity(final)
        sync_directory(path.parent)
    stage = BACKUP / 'recovery-dropin.stage'
    private(stage, RECOVERY_BYTES)
    stage.chmod(0o644)
    descriptor = os.open(stage, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        os.fsync(descriptor)
        info = os.fstat(descriptor)
    finally:
        os.close(descriptor)
    check(info.st_dev == RECOVERY_DROPIN.parent.stat().st_dev,
          'The recovery drop-in cannot be published atomically from its private backup filesystem')
    intent = {'owner': OWNER, 'candidate_sha256': backup['candidate_sha256'], 'baseline': recovery_baseline(),
              'stage': str(stage), 'identity': pinned_identity(info), 'directories': directories}
    publish_exclusive(RECOVERY_INTENT, canonical(intent))
    os.link(stage, RECOVERY_DROPIN, follow_symlinks=False)
    sync_directory(RECOVERY_DROPIN.parent)
    check(installed_recovery_dropin_identity() == intent['identity'], 'Recovery publication did not retain the intended inode')
    stage.unlink()
    sync_directory(BACKUP)
    command(['/usr/bin/systemctl', 'daemon-reload'], 'Load only the newly owned recovery drop-in')

def diagnostic_marker_bytes():
    path = LOG_DIRECTORY / '.goby-diagnostics.json'
    info = regular(path, 128 * 1024, 0o600, 995)
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(descriptor, 'rb') as source:
        opened = os.fstat(source.fileno())
        check(file_identity(opened) == file_identity(info), 'The diagnostic registry changed before its bounded open')
        data = source.read(info.st_size + 1)
        check(len(data) == info.st_size and file_identity(os.fstat(source.fileno())) == file_identity(info) == file_identity(path.lstat()),
              'The diagnostic registry changed during its bounded read')
    return data


def diagnostic_snapshot():
    info = directory(LOG_DIRECTORY, 0o700, 995)
    marker = LOG_DIRECTORY / '.goby-diagnostics.json'
    marker_bytes = diagnostic_marker_bytes()
    value = json.loads(marker_bytes)
    check(isinstance(value, dict) and set(value) == {'version', 'token', 'files'} and value['version'] == 1 and
          isinstance(value['token'], str) and re.fullmatch(r'[0-9a-f]{32}', value['token']) and
          isinstance(value['files'], list) and 1 <= len(value['files']) <= 16,
          'The new diagnostic store does not have a bounded owned registry')
    lock = LOG_DIRECTORY / '.goby-diagnostics.lock'
    check(regular(lock, 0, 0o600, 995).st_size == 0, 'The diagnostic ownership lock differs')
    files, records = {}, 0
    for entry in value['files']:
        check(isinstance(entry, dict) and {'name', 'created', 'identity', 'closed', 'size'} <= set(entry) <=
              {'name', 'created', 'identity', 'closed', 'size', 'deleting'} and
              isinstance(entry['name'], str) and re.fullmatch(r'goby-' + value['token'] + r'-[0-9a-f]{32}\.jsonl', entry['name']) and
              entry['name'] not in files and type(entry['closed']) is bool and not entry.get('deleting', False) and
              type(entry['size']) is int and 0 <= entry['size'] <= 4 * MIB,
              'A diagnostic registry entry escaped its owned JSONL contract')
        path = LOG_DIRECTORY / entry['name']
        before = regular(path, 4 * MIB, 0o600, 995)
        check(entry['identity'] == {'device': before.st_dev, 'inode': before.st_ino} and
              (not entry['closed'] or entry['size'] == before.st_size), 'A registered diagnostic file was replaced')
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(descriptor, 'rb') as source:
            opened = os.fstat(source.fileno())
            check(pinned_identity(opened) == pinned_identity(before), 'A diagnostic file changed before snapshot open')
            data = source.read(before.st_size)
            after = os.fstat(source.fileno())
        check(len(data) == before.st_size and pinned_identity(after) == pinned_identity(before) == pinned_identity(path.lstat()) and
              after.st_size >= before.st_size and (not data or data.endswith(b'\n')),
              'The fixed diagnostic snapshot was truncated or incomplete')
        for line in data.splitlines():
            record = json.loads(line)
            check(len(line) + 1 <= 8192 and isinstance(record, dict) and {'time', 'level', 'msg', 'event'} <= set(record),
                  'A new diagnostic record is not a bounded structured JSON line')
            records += 1
        files[entry['name']] = {'identity': pinned_identity(before), 'snapshot_size': len(data),
                               'snapshot_sha256': hashlib.sha256(data).hexdigest()}
    check({path.name for path in LOG_DIRECTORY.iterdir()} == set(files) | {marker.name, lock.name} and
          diagnostic_marker_bytes() == marker_bytes and pinned_identity(LOG_DIRECTORY.lstat()) == pinned_identity(info),
          'The diagnostic registry or directory changed during its bounded snapshot')
    check(records > 0, 'The new diagnostic store has not recorded a real service event')
    return {'directory_identity': pinned_identity(info), 'files': files, 'record_count': records,
            'registry_sha256': hashlib.sha256(marker_bytes).hexdigest()}


def read_private_store_json(path, maximum=2 * MIB):
    before = regular(path, maximum, 0o600, 995)
    check(before.st_gid == 986, 'A private recovery metadata file group differs')
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(descriptor, 'rb') as source:
        check(file_identity(os.fstat(source.fileno())) == file_identity(before),
              'A recovery metadata file changed before its bounded open')
        data = source.read(before.st_size + 1)
        check(len(data) == before.st_size and file_identity(os.fstat(source.fileno())) == file_identity(before) == file_identity(path.lstat()),
              'A recovery metadata file changed during its bounded read')
    def unique_object(pairs):
        value = {}
        for name, content in pairs:
            check(name not in value, 'Private recovery metadata repeats a field')
            value[name] = content
        return value
    return json.loads(data, object_pairs_hook=unique_object)


def initial_deployment_id(backup):
    intent = load_recovery_intent(backup)
    check(intent is not None and pinned_identity(directory(RECOVERY_DIRECTORY, 0o700, 995)) == intent['directories'][str(RECOVERY_DIRECTORY)],
          'The initial lifecycle directory has no exact installation identity')
    marker = read_private_store_json(RECOVERY_DIRECTORY / '.goby-lifecycle.json', 16384)
    lock = regular(RECOVERY_DIRECTORY / '.goby-lifecycle.lock', 0, 0o600, 995)
    check(isinstance(marker, dict) and set(marker) == {'version', 'deploymentId', 'lock'} and
          type(marker['version']) is int and marker['version'] == 1 and isinstance(marker['deploymentId'], str) and
          re.fullmatch(r'[0-9a-f]{32}', marker['deploymentId']) and
          marker['lock'] == {'device': lock.st_dev, 'inode': lock.st_ino} and lock.st_size == 0,
          'The initial lifecycle owner marker is invalid')
    return marker['deploymentId']


def verify_recovery(backup):
    intent = load_recovery_intent(backup)
    check(intent is not None and installed_recovery_dropin_identity() == intent['identity'],
          'The running recovery drop-in lacks its exact durable ownership proof')
    values = systemd_properties(command(
        ['/usr/bin/systemctl', 'show', SERVICE, '-p', 'Environment'],
        'Recovery service settings observation'), ('Environment',))
    assignments = shlex.split(values.get('Environment', ''), comments=False, posix=True)
    selected = [value for value in assignments if recovery_policy_name(value.partition('=')[0])]
    expected = ['GOBY_RECOVERY_STATE_DIR=' + str(RECOVERY_DIRECTORY), 'GOBY_BACKUP_DIR=' + str(BACKUP_DIRECTORY),
                'GOBY_RECOVERY_OPERATIONS_DIR=' + str(OPERATIONS_DIRECTORY),
                'GOBY_PG_DUMP=/usr/lib/postgresql/17/bin/pg_dump', 'GOBY_PG_RESTORE=/usr/lib/postgresql/17/bin/pg_restore']
    check(set(values) == {'Environment'} and len(selected) == len(expected) and set(selected) == set(expected),
          'The effective recovery systemd environment differs')
    deployment = initial_deployment_id(backup)
    expected_files = {
        RECOVERY_DIRECTORY: {'.goby-lifecycle.json', '.goby-lifecycle.lock', 'generation-registry.json'},
        BACKUP_DIRECTORY: {'.goby-backup-store.json', '.goby-backup-store.lock', '.goby-backup-catalog.json'},
        OPERATIONS_DIRECTORY: {'.goby-recovery-control.json', '.goby-recovery-control.lock', 'current.json', 'cas-proof.json'},
    }
    stores = {}
    for path, names in expected_files.items():
        info = directory(path, 0o700, 995)
        check(pinned_identity(info) == intent['directories'][str(path)] and info.st_gid == 986 and
              {entry.name for entry in path.iterdir()} == names, 'An initial recovery store changed identity or inventory')
        files = {}
        for name in sorted(names):
            entry = path / name
            file_info = regular(entry, 2 * MIB, 0o600, 995)
            check(file_info.st_gid == 986, 'An initial recovery store file has an unexpected group')
            files[name] = {'identity': pinned_identity(file_info), 'sha256': sha(entry)}
        stores[str(path)] = {'identity': pinned_identity(info), 'files': files}
    registry = read_private_store_json(RECOVERY_DIRECTORY / 'generation-registry.json')
    check(isinstance(registry, dict) and type(registry.get('version')) is int and
          registry == {'version': 1, 'deploymentId': deployment, 'baselineDigest': hashlib.sha256(b'').hexdigest(), 'generations': []},
          'Startup unexpectedly published a recovery generation')
    owner = read_private_store_json(BACKUP_DIRECTORY / '.goby-backup-store.json')
    catalog = read_private_store_json(BACKUP_DIRECTORY / '.goby-backup-catalog.json')
    check(isinstance(owner, dict) and set(owner) == {'format', 'token'} and owner['format'] == 'goby-backupstore-v1' and
          isinstance(owner['token'], str) and re.fullmatch(r'[0-9a-f]{32}', owner['token']) and
          isinstance(catalog, dict) and type(catalog.get('version')) is int and
          catalog == {'version': 1, 'token': owner['token'], 'entries': []}, 'Startup unexpectedly created a backup object')
    control_owner = read_private_store_json(OPERATIONS_DIRECTORY / '.goby-recovery-control.json')
    control = read_private_store_json(OPERATIONS_DIRECTORY / 'current.json')
    lock = regular(OPERATIONS_DIRECTORY / '.goby-recovery-control.lock', 0, 0o600, 995)
    check(isinstance(control_owner, dict) and set(control_owner) == {'version', 'deploymentId', 'storeId', 'lock'} and
          type(control_owner['version']) is int and control_owner['version'] == 1 and control_owner['deploymentId'] == deployment and
          isinstance(control_owner['storeId'], str) and re.fullmatch(r'[0-9a-f]{32}', control_owner['storeId']) and
          control_owner['lock'] == {'device': lock.st_dev, 'inode': lock.st_ino} and
          isinstance(control, dict) and set(control) == {'version', 'deploymentId', 'storeId', 'revision', 'previousDigest', 'payload'} and
          type(control['version']) is int and control['version'] == 1 and control['deploymentId'] == deployment and control['storeId'] == control_owner['storeId'] and
          type(control['revision']) is int and control['revision'] == 1 and isinstance(control['previousDigest'], str) and HASH.fullmatch(control['previousDigest']),
          'The operation journal is not the initial locally bound record')
    expected_slots = [{'slot': slot, 'state': state, 'imageId': '', 'name': '', 'captured': '0001-01-01T00:00:00Z', 'operation': ''}
                      for slot, state in (('primary', 'active'), ('recovery', 'unclaimed'))]
    check(isinstance(control['payload'], dict) and type(control['payload'].get('version')) is int and
          control['payload'] == {'version': 1, 'deploymentId': deployment, 'operations': [], 'slots': expected_slots},
          'Startup unexpectedly admitted a backup or recovery operation')
    return {'deployment_id': deployment, 'directory_mode': '0700', 'directory_uid': 995, 'directory_gid': 986,
            'dropin_sha256': hashlib.sha256(RECOVERY_BYTES).hexdigest(), 'stores': stores,
            'initial_primary_generation': True, 'backup_objects': 0, 'operations': 0}


def preserve_recovery(backup):
    check(command(['/usr/bin/systemctl', 'show', SERVICE, '-p', 'MainPID', '--value'],
                  'Recovery configuration stopped-process observation').strip() == b'0',
          'Recovery cannot alter configuration for a running service')
    intent = load_recovery_intent(backup)
    exists = RECOVERY_DROPIN.exists() or RECOVERY_DROPIN.is_symlink()
    check(not exists or intent is not None, 'An unowned recovery drop-in prevents automatic recovery')
    if exists:
        check(installed_recovery_dropin_identity() == intent['identity'], 'Recovery refuses a replaced recovery drop-in')
        RECOVERY_DROPIN.unlink()
        sync_directory(RECOVERY_DROPIN.parent)
    if intent is not None:
        stage = Path(intent['stage'])
        if stage.exists() or stage.is_symlink():
            info = regular(stage, len(RECOVERY_BYTES), 0o644)
            check(pinned_identity(info) == intent['identity'] and stage.read_bytes() == RECOVERY_BYTES,
                  'Recovery refuses a replaced recovery publication stage')
            stage.unlink()
            sync_directory(BACKUP)
        command(['/usr/bin/systemctl', 'daemon-reload'], 'Restore the original loaded service configuration')
    # These directories may contain private operation evidence. Never delete,
    # empty, or claim one without its exact separately persisted inode proof.
    retained = {}
    for path in RECOVERY_DIRECTORIES:
        if path.exists() or path.is_symlink():
            retained[str(path)] = 'retained_without_inventory'
            if intent is not None:
                try:
                    observed = directory(path, 0o700, 995)
                    if pinned_identity(observed) == intent['directories'][str(path)]:
                        retained[str(path)] = 'owned_directory_retained'
                except (Failure, OSError):
                    pass
    return {'directories_deleted': False, 'retained_directories': retained, 'dropin_removed_or_never_installed': True}


def preserve_diagnostic_history(before):
    check(pinned_identity(directory(LOG_DIRECTORY, 0o700, 995)) == before['directory_identity'],
          'The original diagnostic directory was replaced')
    for name, expected in before['files'].items():
        check(isinstance(name, str) and re.fullmatch(r'goby-[0-9a-f]{32}-[0-9a-f]{32}\.jsonl', name),
              'An original diagnostic name escaped its bound')
        path = LOG_DIRECTORY / name
        info = regular(path, 4 * MIB, 0o600, 995)
        check(pinned_identity(info) == expected['identity'] and info.st_size >= expected['snapshot_size'],
              'An original diagnostic file was removed, truncated or replaced')
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(descriptor, 'rb') as source:
            check(pinned_identity(os.fstat(source.fileno())) == expected['identity'],
                  'An original diagnostic file changed before its bounded open')
            data = source.read(expected['snapshot_size'])
            check(hashlib.sha256(data).hexdigest() == expected['snapshot_sha256'] and
                  pinned_identity(os.fstat(source.fileno())) == expected['identity'] == pinned_identity(path.lstat()),
                  'Original diagnostic bytes changed during deployment')
    return True

def media_state(rows):
    roots = {row['id']: row for row in parsed(rows, 'library_roots')}
    allowed = {Path(path) for path in private_values({'GOBY_MEDIA_ROOTS'})['GOBY_MEDIA_ROOTS'].split(os.pathsep)}
    result, total = {}, 0
    for item in parsed(rows, 'items'):
        if item['media'] is None:
            continue
        root = roots[item['root_id']]
        base, path = Path(root['path']), Path(item['path'])
        check(Path(root['allowed_path']) in allowed and base == Path(root['allowed_path']) / root['relative_path'] and
              root['library_id'] == item['library_id'] and path != base and path.is_relative_to(base) and path.resolve(strict=True) == path,
              'An existing media source escaped its accepted library root')
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NOATIME)
        with os.fdopen(descriptor, 'rb') as source:
            info = os.fstat(source.fileno())
            check(stat.S_ISREG(info.st_mode) and 0 < info.st_size <= 128 * MIB and info.st_size == item['file_size'], 'An existing media file escaped its bound')
            total += info.st_size
            check(total <= 512 * MIB, 'The complete existing media population exceeded its byte bound')
            value = hashlib.file_digest(source, 'sha256').hexdigest()
            check(file_identity(info) == file_identity(os.fstat(source.fileno())) == file_identity(path.lstat()), 'An existing media file changed during observation')
        result[item['id']] = {'identity': file_identity(info), 'sha256': value}
    check(len(result) == 11, 'The complete existing media population differs')
    return result


def source_entry(relative):
    if not isinstance(relative, str):
        return False
    path = PurePosixPath(relative)
    if path.is_absolute() or str(path) != relative or '..' in path.parts or not path.parts:
        return False
    return (relative in {'go.mod', 'go.sum'} or path.parts[0] in {'cmd', 'internal'} and path.suffix == '.go' or
            re.fullmatch(r'internal/database/migrations/[0-9]{4}_[a-z0-9_]+\.sql', relative) is not None or
            re.fullmatch(r'internal/backuppg/catalogs/schema-[0-9]+-postgresql-[0-9]+\.json', relative) is not None)


def validate_candidate_document(value):
    check(isinstance(value, dict) and set(value) == {'owner', 'binary', 'assets', 'source', 'gates'} and value['owner'] == CANDIDATE_OWNER,
          'The candidate manifest has an unexpected contract')
    binary, assets, source = value['binary'], value['assets'], value['source']
    check(isinstance(binary, dict) and set(binary) == {'path', 'sha256', 'size'} and
          binary['path'] == str(SCRATCH / 'goby-m5j-linux-amd64') and isinstance(binary['sha256'], str) and HASH.fullmatch(binary['sha256']) and
          binary['sha256'] != OLD and type(binary['size']) is int and 0 < binary['size'] <= 256 * MIB, 'The candidate executable contract differs')
    check(isinstance(assets, dict) and set(assets) == {'path', 'sha256', 'files'} and assets['path'] == str(SCRATCH / 'goby-m5j-admin-assets.tar.gz') and
          isinstance(assets['sha256'], str) and HASH.fullmatch(assets['sha256']) and type(assets['files']) is int and 1 < assets['files'] <= 256,
          'The candidate administrator asset contract differs')
    check(isinstance(source, dict) and set(source) == {'path', 'resolved_path', 'owner_marker', 'files'} and
          isinstance(source['path'], str) and re.fullmatch(r'/opt/goby-test/verify-m5j[-a-z0-9]*', source['path']) and
          isinstance(source['resolved_path'], str) and re.fullmatch(r'/(?:dev/shm/goby-verify-m5j|opt/goby-test/verify-m5j)[-a-z0-9]*', source['resolved_path']) and
          isinstance(source['owner_marker'], str) and re.fullmatch(r'goby-m5j[-a-z0-9]*', source['owner_marker']) and
          isinstance(source['files'], dict) and 20 < len(source['files']) <= 1000 and {'go.mod', 'go.sum'} <= set(source['files']) and
          all(source_entry(path) and isinstance(digest, str) and HASH.fullmatch(digest) for path, digest in source['files'].items()),
          'The accepted source snapshot contract differs')
    migrations = [path for path in source['files'] if path.endswith('.sql')]
    check(len(migrations) == TARGET_SCHEMA and {int(PurePosixPath(path).name[:4]) for path in migrations} == set(range(1, TARGET_SCHEMA + 1)) and
          {'internal/database/migrations/0022_activity_entries.sql',
           'internal/database/migrations/0023_backup_activity.sql'} <= set(migrations), 'The candidate must bind all twenty-three migrations')
    check('internal/backuppg/catalogs/schema-23-postgresql-17.json' in source['files'],
          'The candidate must include its compiled PostgreSQL catalog input')
    check(isinstance(value['gates'], dict) and set(value['gates']) == {'final_go', 'full', 'browser', 'recovery'}, 'The final acceptance gates are incomplete')
    for gate in value['gates'].values():
        check(isinstance(gate, dict) and set(gate) == {'status', 'evidence_path', 'sha256'} and gate['status'] == 'passed' and
              isinstance(gate['sha256'], str) and HASH.fullmatch(gate['sha256']) and isinstance(gate['evidence_path'], str) and
              Path(gate['evidence_path']).is_absolute() and str(PurePosixPath(gate['evidence_path'])) == gate['evidence_path'] and
              '..' not in PurePosixPath(gate['evidence_path']).parts and Path(gate['evidence_path']).suffix in {'.json', '.log'} and
              (Path(gate['evidence_path']).is_relative_to(ROOT) or Path(gate['evidence_path']).is_relative_to(Path(source['resolved_path']))),
              'Final Go, full-suite, browser and recovery evidence must all be explicitly accepted before deployment')
    return value


def source_inventory(root):
    paths = [*root.glob('cmd/**/*.go'), *root.glob('internal/**/*.go'), *root.glob('internal/database/migrations/*.sql'),
             *root.glob('internal/backuppg/catalogs/*.json'), root / 'go.mod', root / 'go.sum']
    result = {}
    for path in paths:
        regular(path, 2 * MIB, 0o644)
        relative = path.relative_to(root).as_posix()
        check(source_entry(relative) and relative not in result, 'A source inventory has an unexpected or duplicate path')
        result[relative] = sha(path)
    return result


def asset_entry(relative):
    path = PurePosixPath(relative)
    return (not path.is_absolute() and str(path) == relative and '..' not in path.parts and
            (relative == 'index.html' or len(path.parts) > 1 and path.parts[0] == 'assets'))


def read_assets(path):
    result = {}
    with tarfile.open(path) as archive:
        members = archive.getmembers()
        check(len(members) <= 512, 'The asset archive has too many entries')
        for member in members:
            name = PurePosixPath(member.name)
            check(not name.is_absolute() and '..' not in name.parts and (member.isdir() or member.isfile()), 'An asset archive entry is unsafe')
            if member.isdir():
                check(str(name) in {'.', 'assets'} or str(name).startswith('assets/'), 'An asset directory escaped its scope')
                continue
            relative = str(name)
            check(asset_entry(relative) and relative not in result and 0 <= member.size <= 2 * MIB, 'An asset entry is unexpected or oversized')
            result[relative] = archive.extractfile(member).read()
    check('index.html' in result and sum(map(len, result.values())) <= 32 * MIB, 'The complete asset payload escaped its bound')
    return result


def target(root, relative):
    path = root / relative
    check(path != root and path.is_relative_to(root) and path.parent.resolve() == path.parent, 'An installation target escaped its fixed root')
    current = root
    directory(current, 0o755)
    for part in path.parent.relative_to(root).parts:
        current = current / part
        if current.exists() or current.is_symlink():
            directory(current, 0o755)
    if path.exists() or path.is_symlink():
        regular(path, 2 * MIB, 0o644)
    return path


def parent_for(root, path):
    current = root
    for part in path.parent.relative_to(root).parts:
        current = current / part
        if not current.exists():
            current.mkdir(mode=0o755)
            current.chmod(0o755)
            sync_directory(current.parent)
        directory(current, 0o755)


def replace_file(path, payload, mode):
    directory(path.parent, 0o755)
    stage = path.with_name('.' + path.name + '.m5j-stage')
    if stage.exists() or stage.is_symlink():
        info = regular(stage, len(payload))
        check(stat.S_IMODE(info.st_mode) in {0o600, mode} and stage.read_bytes() == payload[:info.st_size],
              'An interrupted file stage does not match its accepted intended bytes')
        stage.unlink()
    try:
        private(stage, payload)
        stage.chmod(mode)
        os.replace(stage, path)
        sync_directory(path.parent)
    finally:
        if stage.exists() and not stage.is_symlink():
            info = regular(stage, len(payload))
            if stage.read_bytes() == payload[:info.st_size]:
                stage.unlink()


def restore_sql_command(dump_path, toc_path):
    return ['/usr/lib/postgresql/17/bin/pg_restore', '--clean', '--if-exists', '--no-owner', '--no-acl', '--file=-',
            '--use-list=' + str(toc_path), str(dump_path)]


def restore_database_command(sql_path):
    return ['/usr/bin/psql', '-X', '-q', '-v', 'ON_ERROR_STOP=1', '--single-transaction', '--file=' + str(sql_path)]


def filtered_restore_toc(raw):
    check(isinstance(raw, bytes) and len(raw) <= 2 * MIB, 'The recovery archive inventory is invalid')
    result = []
    for line in raw.decode('utf-8').splitlines():
        if re.match(r'^\d+;\s+\d+\s+\d+\s+(?:SCHEMA\s+-\s+public(?:\s|$)|(?:COMMENT|ACL)\s+-\s+SCHEMA\s+public(?:\s|$))', line):
            continue
        result.append(line)
    check(any(re.match(r'^\d+;.*\sTABLE\s+public\s+sessions\s', line) for line in result), 'The recovery archive omitted the old application tables')
    return ('\n'.join(result) + '\n').encode()


def validate_backup_document(value):
    required = {'candidate.json', 'operator.py', 'business-before.json', 'media-before.json', 'protected-before.json',
                'goby-m5i', 'runtime.env', 'unit.service', 'dropin.conf', 'master.key', 'database-schema22.dump',
                'restore-toc.list', 'restore-schema22.sql', 'restore-rehearsal.json', 'source-before.json', 'source-after.json',
                'recovery-before.json', 'diagnostics-before.json', 'observability-dropin.conf', 'recovery-database-before.json',
                'candidate-assets.tar.gz'}
    check(isinstance(value, dict) and value.get('owner') == OWNER and value.get('phase') == 'backup-complete' and
          value.get('old_binary_sha256') == OLD and isinstance(value.get('candidate_sha256'), str) and HASH.fullmatch(value['candidate_sha256']) and
          isinstance(value.get('candidate_manifest_sha256'), str) and HASH.fullmatch(value['candidate_manifest_sha256']) and
          isinstance(value.get('files'), dict) and required <= set(value['files']) and
          all(isinstance(name, str) and str(PurePosixPath(name)) == name and not PurePosixPath(name).is_absolute() and '..' not in PurePosixPath(name).parts and
              isinstance(digest, str) and HASH.fullmatch(digest) for name, digest in value['files'].items()) and
          value.get('restore_sql_generated') is True and value.get('isolated_restore_verified') is True and value.get('live_restore_verified') is False,
          'Only a complete matching M5j backup can authorize recovery')
    check(value['files']['goby-m5i'] == OLD and value['files']['candidate.json'] == value['candidate_manifest_sha256'],
          'The complete backup executable or accepted candidate manifest binding differs')
    return value


def load_backup():
    directory(BACKUP, 0o700)
    regular(BACKUP / 'OWNER.txt', 256, 0o600)
    check((BACKUP / 'OWNER.txt').read_text().strip() == OWNER, 'The M5j backup ownership marker differs')
    regular(BACKUP / 'backup-manifest.json', 2 * MIB, 0o600)
    result = validate_backup_document(json.loads((BACKUP / 'backup-manifest.json').read_bytes()))
    for relative, digest in result['files'].items():
        path = BACKUP / relative
        regular(path, 256 * MIB, 0o600)
        check(sha(path) == digest, 'An acknowledged complete backup file changed')
    return result


def peer_argv(database):
    return ['/usr/sbin/runuser', '-u', 'postgres', '--', '/usr/bin/psql', '-X', '-q', '-A', '-t',
            '-v', 'ON_ERROR_STOP=1', '-h', '/var/run/postgresql', '-p', '5432', '-d', database]


def peer_environment(read_only=True):
    return {'PATH': '/usr/bin:/bin', 'LANG': 'C.UTF-8', 'PGPASSFILE': '/dev/null', 'PGCONNECT_TIMEOUT': '5',
            'PGOPTIONS': '-c default_transaction_read_only=' + ('on' if read_only else 'off') +
            ' -c statement_timeout=60000 -c lock_timeout=5000 -c timezone=UTC -c bytea_output=hex -c DateStyle=ISO,YMD'}


def peer_read(query):
    return json.loads(command(peer_argv('postgres'), 'Isolated recovery administration observation',
                              environment=peer_environment(), data=query.encode(), timeout=25))


RECOVERY_DATABASE_NAME = 'goby_recovery_m5j'
RECOVERY_ROLE_NAME = 'goby_recovery_m5j'
RECOVERY_DB_INTENT = BACKUP / 'recovery-database-intent.json'
RECOVERY_ENV_IDENTITY = BACKUP / 'recovery-environment-identity.json'
RECOVERY_ROLE_IDENTITY = BACKUP / 'recovery-role-identity.json'
RECOVERY_DATABASE_IDENTITY = BACKUP / 'recovery-database-identity.json'
RECOVERY_DATABASE_OPENING = BACKUP / 'recovery-database-opening.json'
RECOVERY_DATABASE_READY = BACKUP / 'recovery-database-ready.json'
RECOVERY_DATABASE_CLOSURE = BACKUP / 'recovery-database-closure.json'


def _recovery_present(path):
    return path.exists() or path.is_symlink()


def _recovery_unpublished():
    check(not _recovery_present(EVIDENCE) and not _recovery_present(RECOVERY_EVIDENCE),
          'A published deployment or recovery is an absolute recovery-database cleanup barrier')


def _recovery_paths():
    return (RECOVERY_ENV, RECOVERY_DB_INTENT, RECOVERY_ENV_IDENTITY,
            RECOVERY_ROLE_IDENTITY, RECOVERY_DATABASE_IDENTITY, RECOVERY_DATABASE_OPENING,
            RECOVERY_DATABASE_READY, RECOVERY_DATABASE_CLOSURE)


def _recovery_digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def _recovery_literal(value):
    check(isinstance(value, str) and '\x00' not in value, 'A recovery SQL literal is invalid')
    return "'" + value.replace("'", "''") + "'"


def _recovery_private_bytes(path, maximum=2 * MIB):
    info = regular(path, maximum, 0o600)
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(descriptor, 'rb') as source:
        check(file_identity(os.fstat(source.fileno())) == file_identity(info),
              'A private recovery proof changed before its bounded read')
        payload = source.read(info.st_size + 1)
        check(len(payload) == info.st_size and file_identity(os.fstat(source.fileno())) ==
              file_identity(info) == file_identity(path.lstat()),
              'A private recovery proof changed during its bounded read')
    return payload


def _recovery_document(path):
    payload = _recovery_private_bytes(path)
    try:
        result = json.loads(payload)
    except (UnicodeDecodeError, json.JSONDecodeError):
        raise Failure('A private recovery proof is not valid JSON') from None
    check(isinstance(result, dict) and payload == canonical(result),
          'A private recovery proof is not an exact canonical JSON object')
    return result


def _recovery_publish(path, value):
    check(not _recovery_present(path), 'A new recovery proof path already exists')
    publish_exclusive(path, canonical(value))
    check(_recovery_document(path) == value, 'A durable recovery proof differs from its intended bytes')


def _recovery_role_expression(alias='r'):
    return ("jsonb_build_object('oid'," + alias + ".oid::bigint,'name'," + alias + ".rolname,"
            "'login'," + alias + ".rolcanlogin,'superuser'," + alias + ".rolsuper,"
            "'createdb'," + alias + ".rolcreatedb,'createrole'," + alias + ".rolcreaterole,"
            "'inherit'," + alias + ".rolinherit,'replication'," + alias + ".rolreplication,"
            "'bypassrls'," + alias + ".rolbypassrls,'connection_limit'," + alias + ".rolconnlimit,"
            "'valid_until'," + alias + ".rolvaliduntil,'settings'," + alias + ".rolconfig,"
            "'comment',pg_catalog.shobj_description(" + alias + ".oid,'pg_authid'))")


def _recovery_database_expression(alias='d'):
    return ("jsonb_build_object('oid'," + alias + ".oid::bigint,'name'," + alias + ".datname,"
            "'owner_oid'," + alias + ".datdba::bigint,'owner',pg_catalog.pg_get_userbyid(" + alias + ".datdba),"
            "'encoding',pg_catalog.pg_encoding_to_char(" + alias + ".encoding),'template'," + alias + ".datistemplate,"
            "'allow_connections'," + alias + ".datallowconn,'connection_limit'," + alias + ".datconnlimit,"
            "'tablespace_oid'," + alias + ".dattablespace::bigint,'collation'," + alias + ".datcollate,"
            "'ctype'," + alias + ".datctype,'locale_provider'," + alias + ".datlocprovider,"
            "'collation_version'," + alias + ".datcollversion,'acl'," + alias + ".datacl::text,"
            "'safe_acl',NOT EXISTS(SELECT 1 FROM pg_catalog.aclexplode(COALESCE(" + alias +
            ".datacl,pg_catalog.acldefault('d'," + alias + ".datdba))) a WHERE a.grantee<>" + alias + ".datdba),"
            "'comment',pg_catalog.shobj_description(" + alias + ".oid,'pg_database'))")


def _recovery_catalog():
    # Passwords and authentication verifiers are deliberately never queried.
    value = peer_read(
        "SELECT jsonb_build_object('cluster',jsonb_build_object("
        "'system_identifier',(SELECT system_identifier::text FROM pg_catalog.pg_control_system()),"
        "'postmaster_start',pg_catalog.pg_postmaster_start_time(),"
        "'version_num',current_setting('server_version_num')::integer,"
        "'port',current_setting('port')::integer,'user',current_user,'database',current_database()),"
        "'databases',COALESCE((SELECT jsonb_agg(" + _recovery_database_expression() +
        " ORDER BY d.datname) FROM pg_catalog.pg_database d),'[]'::jsonb),"
        "'roles',COALESCE((SELECT jsonb_agg(" + _recovery_role_expression() +
        " ORDER BY r.rolname) FROM pg_catalog.pg_roles r),'[]'::jsonb),"
        "'memberships',COALESCE((SELECT jsonb_agg(jsonb_build_object('role_oid',roleid::bigint,"
        "'member_oid',member::bigint,'grantor_oid',grantor::bigint,'admin',admin_option,"
        "'inherit',inherit_option,'set',set_option) ORDER BY roleid,member,grantor)"
        " FROM pg_catalog.pg_auth_members),'[]'::jsonb),"
        "'settings',COALESCE((SELECT jsonb_agg(jsonb_build_object('database_oid',setdatabase::bigint,"
        "'role_oid',setrole::bigint,'settings',setconfig) ORDER BY setdatabase,setrole)"
        " FROM pg_catalog.pg_db_role_setting),'[]'::jsonb),"
        "'security_labels',COALESCE((SELECT jsonb_agg(jsonb_build_object('object_oid',objoid::bigint,"
        "'class_oid',classoid::bigint,'provider',provider,'label',label) ORDER BY classoid,objoid,provider)"
        " FROM pg_catalog.pg_shseclabel),'[]'::jsonb));")
    check(isinstance(value, dict) and set(value) == {'cluster', 'databases', 'roles', 'memberships', 'settings', 'security_labels'} and
          isinstance(value['cluster'], dict) and value['cluster'].get('user') == 'postgres' and
          value['cluster'].get('database') == 'postgres' and value['cluster'].get('port') == 5432 and
          type(value['cluster'].get('version_num')) is int and value['cluster']['version_num'] // 10000 == 17 and
          isinstance(value['cluster'].get('system_identifier'), str) and
          re.fullmatch(r'[0-9]+', value['cluster']['system_identifier']) and
          all(isinstance(value[key], list) for key in ('databases', 'roles', 'memberships', 'settings', 'security_labels')),
          'The recovery catalog observation is outside the expected PostgreSQL seventeen cluster')
    # Existing comments, custom role settings, and security labels are opaque
    # configuration: retain exact digests without returning their free text.
    # The new pair's own fixed tag and empty settings remain directly checked.
    for kind, own_name in (('databases', RECOVERY_DATABASE_NAME), ('roles', RECOVERY_ROLE_NAME)):
        for entry in value[kind]:
            if entry['name'] != own_name:
                for field in ('comment', 'settings'):
                    if field in entry and entry[field] is not None:
                        entry[field] = {'sha256': _recovery_digest(entry[field])}
    for entry in value['settings']:
        entry['settings'] = {'sha256': _recovery_digest(entry['settings'])}
    for entry in value['security_labels']:
        entry['label'] = {'sha256': _recovery_digest(entry['label'])}
    return value


def _recovery_entry(catalog, kind, name):
    entries = [item for item in catalog[kind] if item.get('name') == name]
    check(len(entries) <= 1, 'A recovery catalog name is ambiguous')
    return entries[0] if entries else None


def _recovery_main_identity(database, catalog):
    observed = database.read(
        "SELECT jsonb_build_object('database',current_database(),'user',current_user,"
        "'address',host(inet_server_addr()),'port',inet_server_port(),"
        "'postmaster_start',pg_catalog.pg_postmaster_start_time(),"
        "'version_num',current_setting('server_version_num')::integer,"
        "'database_oid',(SELECT oid::bigint FROM pg_catalog.pg_database WHERE datname=current_database()),"
        "'role_oid',(SELECT oid::bigint FROM pg_catalog.pg_roles WHERE rolname=current_user));")
    main_database = _recovery_entry(catalog, 'databases', 'goby_test')
    main_role = _recovery_entry(catalog, 'roles', 'goby_test')
    check(main_database is not None and main_role is not None and observed == {
        'database': 'goby_test', 'user': 'goby_test', 'address': '127.0.0.1', 'port': 5432,
        'postmaster_start': catalog['cluster']['postmaster_start'],
        'version_num': catalog['cluster']['version_num'],
        'database_oid': main_database['oid'], 'role_oid': main_role['oid']},
        'The private primary connection and recovery administration are not the same accepted cluster')
    return observed


def recovery_database_baseline(database):
    """Read and prove the fixed recovery names and every new path are absent."""
    root_info = directory(ROOT)
    check(RECOVERY_DATABASE_NAME == RECOVERY_ROLE_NAME == 'goby_recovery_m5j' and
          RECOVERY_DATABASE_NAME != 'goby_test' and not any(_recovery_present(path) for path in _recovery_paths()),
          'A recovery database, environment, or ownership-proof path has a preexisting conflict')
    catalog = _recovery_catalog()
    check(_recovery_entry(catalog, 'databases', RECOVERY_DATABASE_NAME) is None and
          _recovery_entry(catalog, 'roles', RECOVERY_ROLE_NAME) is None,
          'The fixed recovery database or role already exists and cannot be adopted')
    return {'owner': OWNER, 'database': RECOVERY_DATABASE_NAME, 'role': RECOVERY_ROLE_NAME,
            'environment_path': str(RECOVERY_ENV), 'new_paths_absent': [str(path) for path in _recovery_paths()],
            'root_identity': pinned_identity(root_info), 'main': _recovery_main_identity(database, catalog),
            'catalog': catalog, 'catalog_sha256': _recovery_digest(catalog)}


def _recovery_baseline(backup):
    check(load_backup() == backup and 'recovery-database-before.json' in backup['files'],
          'Recovery provisioning requires the complete independently checked backup')
    value = _recovery_document(BACKUP / 'recovery-database-before.json')
    check(value.get('owner') == OWNER and value.get('database') == RECOVERY_DATABASE_NAME and
          value.get('role') == RECOVERY_ROLE_NAME and value.get('environment_path') == str(RECOVERY_ENV) and
          value.get('new_paths_absent') == [str(path) for path in _recovery_paths()] and
          value.get('root_identity') == pinned_identity(directory(ROOT)) and
          isinstance(value.get('catalog'), dict) and value.get('catalog_sha256') == _recovery_digest(value['catalog']) and
          _recovery_entry(value['catalog'], 'databases', RECOVERY_DATABASE_NAME) is None and
          _recovery_entry(value['catalog'], 'roles', RECOVERY_ROLE_NAME) is None,
          'The durable recovery-database absence proof differs from the fixed deployment scope')
    return value


def _recovery_protected_catalog(current, baseline):
    role = _recovery_entry(current, 'roles', RECOVERY_ROLE_NAME)
    target = _recovery_entry(current, 'databases', RECOVERY_DATABASE_NAME)
    protected = dict(current)
    protected['roles'] = [entry for entry in current['roles'] if entry['name'] != RECOVERY_ROLE_NAME]
    protected['databases'] = [entry for entry in current['databases'] if entry['name'] != RECOVERY_DATABASE_NAME]
    check(protected == baseline['catalog'],
          'A preexisting database, role, membership, setting, or cluster changed during recovery provisioning')
    if role is not None:
        check(not any(role['oid'] in (entry['role_oid'], entry['member_oid'], entry['grantor_oid'])
                      for entry in current['memberships']),
              'The recovery role acquired an unexpected role membership')
    if target is not None:
        check(role is not None and target['owner_oid'] == role['oid'] and target['owner'] == RECOVERY_ROLE_NAME,
              'The recovery database no longer belongs only to the separately owned recovery role')
    return role, target


def _recovery_intent(backup, baseline):
    value = _recovery_document(RECOVERY_DB_INTENT)
    check(set(value) == {'owner', 'candidate_sha256', 'baseline_sha256', 'tag', 'environment_sha256'} and
          value['owner'] == OWNER and value['candidate_sha256'] == backup['candidate_sha256'] and
          value['baseline_sha256'] == _recovery_digest(baseline) and
          isinstance(value['tag'], str) and re.fullmatch(re.escape(OWNER) + r':recovery:[0-9a-f]{32}', value['tag']) and
          isinstance(value['environment_sha256'], str) and HASH.fullmatch(value['environment_sha256']),
          'The recovery installation intent has an unexpected owner, candidate, or baseline')
    return value


def _recovery_receipt(path, intent, field):
    value = _recovery_document(path)
    check(set(value) == {'owner', 'intent_sha256', field, 'fingerprint_sha256'} and
          value['owner'] == OWNER and value['intent_sha256'] == _recovery_digest(intent) and
          isinstance(value[field], dict) and value['fingerprint_sha256'] == _recovery_digest(value[field]),
          'An immutable recovery ownership receipt changed its binding or fingerprint')
    return value[field]


def _recovery_record(path, intent, field, value):
    _recovery_publish(path, {'owner': OWNER, 'intent_sha256': _recovery_digest(intent), field: value,
                             'fingerprint_sha256': _recovery_digest(value)})


def _recovery_environment_proof(intent):
    info = regular(RECOVERY_ENV, 4096, 0o600)
    payload = _recovery_private_bytes(RECOVERY_ENV, 4096)
    check(file_identity(RECOVERY_ENV.lstat()) == file_identity(info) and
          hashlib.sha256(payload).hexdigest() == intent['environment_sha256'],
          'The recovery environment changed its intended private bytes')
    return {'path': str(RECOVERY_ENV), 'identity': file_identity(info),
            'sha256': intent['environment_sha256']}


def _recovery_environment(intent):
    expected = _recovery_receipt(RECOVERY_ENV_IDENTITY, intent, 'environment')
    check(_recovery_environment_proof(intent) == expected,
          'The recovery environment changed its exact owned file identity')
    payload = _recovery_private_bytes(RECOVERY_ENV, 4096)
    prefix = ('GOBY_RECOVERY_DATABASE_URL=postgresql://' + RECOVERY_ROLE_NAME + ':').encode()
    suffix = ('@127.0.0.1:5432/' + RECOVERY_DATABASE_NAME + '?sslmode=disable\n').encode()
    check(payload.startswith(prefix) and payload.endswith(suffix),
          'The private recovery environment is not the exact fixed loopback URL assignment')
    try:
        password = payload[len(prefix):-len(suffix)].decode('ascii')
    except UnicodeDecodeError:
        raise Failure('The private recovery password has an invalid bounded encoding') from None
    check(re.fullmatch(r'[0-9a-f]{64}', password) is not None,
          'The private recovery password does not match the generated bounded format')
    environment = peer_environment()
    environment.update({'PGHOST': '127.0.0.1', 'PGPORT': '5432', 'PGDATABASE': RECOVERY_DATABASE_NAME,
                        'PGUSER': RECOVERY_ROLE_NAME, 'PGPASSWORD': password,
                        'PGSSLMODE': 'disable', 'PGCLIENTENCODING': 'UTF8'})
    return environment


def _recovery_secret_sql(payload):
    # command() intentionally retains raw stderr. Never use it for password
    # SQL: psql diagnostics and exception output must not enter any evidence.
    try:
        result = subprocess.run(peer_argv('postgres'), env=peer_environment(False), input=payload,
                                capture_output=True, timeout=30)
    except subprocess.TimeoutExpired:
        raise Failure('Private recovery-role creation timed out; ownership receipts govern cleanup') from None
    check(result.returncode == 0 and len(result.stdout) <= 4096 and len(result.stderr) <= MIB,
          'Private recovery-role creation failed; secret command diagnostics were discarded')


def _recovery_scram_verifier(password):
    # The generated hexadecimal password is already an ASCII SASLprep fixed
    # point. Build PostgreSQL's SCRAM verifier client-side so the plaintext
    # password never enters SQL, including any server-side statement logs.
    check(re.fullmatch(r'[0-9a-f]{64}', password) is not None,
          'Only the generated hexadecimal password may enter recovery SCRAM derivation')
    salt = secrets.token_bytes(16)
    salted = hashlib.pbkdf2_hmac('sha256', password.encode('ascii'), salt, 4096)
    client_key = hmac.new(salted, b'Client Key', hashlib.sha256).digest()
    stored_key = hashlib.sha256(client_key).digest()
    server_key = hmac.new(salted, b'Server Key', hashlib.sha256).digest()
    encode = lambda value: base64.b64encode(value).decode('ascii')
    return 'SCRAM-SHA-256$4096:' + encode(salt) + '$' + encode(stored_key) + ':' + encode(server_key)


def _recovery_expected_role(role, intent):
    check(isinstance(role, dict) and type(role.get('oid')) is int and role['oid'] > 0 and
          role.get('name') == RECOVERY_ROLE_NAME and role.get('comment') == intent['tag'] and
          role.get('login') is True and role.get('inherit') is False and
          all(role.get(key) is False for key in ('superuser', 'createdb', 'createrole', 'replication', 'bypassrls')) and
          role.get('connection_limit') == 16 and role.get('valid_until') is None and role.get('settings') is None,
          'The independently owned recovery role is not the exact low-privilege LOGIN role')


def _recovery_schema_query():
    # An empty public namespace must also have no hidden user objects, global
    # database hooks, foreign grants, or objects in another user namespace.
    catalogs = (('pg_class', 'relnamespace'), ('pg_proc', 'pronamespace'), ('pg_type', 'typnamespace'),
                ('pg_collation', 'collnamespace'), ('pg_conversion', 'connamespace'),
                ('pg_operator', 'oprnamespace'), ('pg_opclass', 'opcnamespace'),
                ('pg_opfamily', 'opfnamespace'), ('pg_ts_config', 'cfgnamespace'),
                ('pg_ts_dict', 'dictnamespace'), ('pg_ts_parser', 'prsnamespace'),
                ('pg_ts_template', 'tmplnamespace'))
    objects = ['SELECT 1 FROM pg_catalog.' + table + ' o JOIN pg_catalog.pg_namespace n ON n.oid=o.' + column +
               " WHERE n.nspname !~ '^pg_' AND n.nspname<>'information_schema'" for table, column in catalogs]
    objects += ["SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname !~ '^pg_' AND nspname NOT IN ('public','information_schema')",
                "SELECT 1 FROM pg_catalog.pg_extension WHERE extname<>'plpgsql'",
                "SELECT 1 FROM pg_catalog.pg_language WHERE lanname NOT IN ('internal','c','sql','plpgsql')"]
    objects += ['SELECT 1 FROM pg_catalog.' + table for table in
                ('pg_event_trigger', 'pg_foreign_server', 'pg_foreign_data_wrapper', 'pg_publication',
                 'pg_largeobject_metadata', 'pg_default_acl', 'pg_seclabel')]
    # pg_subscription is shared; another existing database's subscription is
    # outside this empty target. Never read its connection string.
    objects += ["SELECT 1 FROM pg_catalog.pg_subscription WHERE subdbid=(SELECT oid FROM pg_catalog.pg_database WHERE datname=current_database())"]
    return (
        "SELECT jsonb_build_object('database',current_database(),'user',current_user,'session_user',session_user,"
        "'postmaster_start',pg_catalog.pg_postmaster_start_time(),'version_num',current_setting('server_version_num')::integer,"
        "'encoding',current_setting('server_encoding'),'address',host(inet_server_addr()),'port',inet_server_port(),"
        "'database_oid',(SELECT oid::bigint FROM pg_catalog.pg_database WHERE datname=current_database()),"
        "'public',(SELECT jsonb_build_object('oid',n.oid::bigint,'owner_oid',n.nspowner::bigint,"
        "'owner',pg_catalog.pg_get_userbyid(n.nspowner),'comment',pg_catalog.obj_description(n.oid,'pg_namespace'),"
        "'acl',n.nspacl::text,'safe_owner',n.nspowner IN (SELECT oid FROM pg_catalog.pg_roles WHERE rolname IN "
        "('pg_database_owner'," + _recovery_literal(RECOVERY_ROLE_NAME) + ")),"
        "'safe_acl',NOT EXISTS(SELECT 1 FROM pg_catalog.aclexplode(COALESCE(n.nspacl,pg_catalog.acldefault('n',n.nspowner))) a "
        "WHERE a.grantee<>n.nspowner AND a.grantee<>(SELECT oid FROM pg_catalog.pg_roles WHERE rolname=" +
        _recovery_literal(RECOVERY_ROLE_NAME) + ") AND NOT(a.grantee=0 AND a.privilege_type='USAGE' AND NOT a.is_grantable))) "
        "FROM pg_catalog.pg_namespace n WHERE n.nspname='public'),"
        "'empty',NOT EXISTS(" + ' UNION ALL '.join(objects) + "));")


def _recovery_schema_observation():
    # The role must have no access to pg_subscription; run catalog inspection
    # as peer postgres, and use the separate small query for its TCP login.
    return json.loads(command(peer_argv(RECOVERY_DATABASE_NAME), 'Read-only independent recovery namespace observation',
                              environment=peer_environment(), data=_recovery_schema_query().encode(), timeout=25))


def _recovery_check_empty(schema, target, baseline):
    check(isinstance(schema, dict), 'The recovery namespace observation is not a JSON object')
    namespace = schema.get('public')
    check(schema.get('database') == RECOVERY_DATABASE_NAME and schema.get('user') == 'postgres' and
          schema.get('session_user') == 'postgres' and schema.get('database_oid') == target['oid'] and
          schema.get('postmaster_start') == baseline['catalog']['cluster']['postmaster_start'] and
          schema.get('version_num') == baseline['catalog']['cluster']['version_num'] and
          schema.get('encoding') == 'UTF8' and schema.get('address') is None and schema.get('port') is None and
          schema.get('empty') is True and isinstance(namespace, dict) and
          type(namespace.get('oid')) is int and namespace['oid'] > 0 and namespace.get('safe_owner') is True and
          namespace.get('safe_acl') is True and namespace.get('comment') in (None, 'standard public schema'),
          'The owned recovery database no longer has a safe completely empty public namespace')


def _recovery_tcp_identity(intent, target, baseline):
    query = ("SELECT jsonb_build_object('database',current_database(),'user',current_user,'session_user',session_user,"
             "'address',host(inet_server_addr()),'port',inet_server_port(),"
             "'postmaster_start',pg_catalog.pg_postmaster_start_time(),"
             "'version_num',current_setting('server_version_num')::integer,"
             "'database_oid',(SELECT oid::bigint FROM pg_catalog.pg_database WHERE datname=current_database()),"
             "'role_oid',(SELECT oid::bigint FROM pg_catalog.pg_roles WHERE rolname=current_user),"
             "'can_create_public',pg_catalog.has_schema_privilege(current_user,'public','CREATE'));")
    result = json.loads(command(['/usr/bin/psql', '-X', '-q', '-A', '-t', '-v', 'ON_ERROR_STOP=1'],
                                'Verify the private recovery role through its configured loopback connection',
                                environment=_recovery_environment(intent), data=query.encode(), timeout=25))
    check(result == {'database': RECOVERY_DATABASE_NAME, 'user': RECOVERY_ROLE_NAME, 'session_user': RECOVERY_ROLE_NAME,
                     'address': '127.0.0.1', 'port': 5432,
                     'postmaster_start': baseline['catalog']['cluster']['postmaster_start'],
                     'version_num': baseline['catalog']['cluster']['version_num'],
                     'database_oid': target['oid'], 'role_oid': target['owner_oid'], 'can_create_public': True},
          'The independent low-privilege recovery URL did not reach the exact intended database and role')
    return result


def provision_recovery_database(backup, database):
    """Create only the new isolated pair after durable backup and installation authorization."""
    _recovery_unpublished()
    check(DEPLOYMENT_LOCK_HELD, 'Recovery provisioning requires the exclusive deployment operator lock')
    baseline = _recovery_baseline(backup)
    check(installation_proven(backup), 'Recovery database provisioning requires durable installation authorization')
    rehearsal = _recovery_document(BACKUP / 'restore-rehearsal.json')
    check(backup.get('isolated_restore_verified') is True and rehearsal.get('status') == 'passed' and
          rehearsal.get('schema_version') == 22 and rehearsal.get('isolated_database_import_executed') is True and
          rehearsal.get('temporary_database_removed') is True and isinstance(rehearsal.get('tables'), dict) and
          len(rehearsal['tables']) == 29 and set(rehearsal['tables']) == OLD_TABLES,
          'Recovery provisioning requires the completed fresh restore of all twenty-nine existing tables')
    check(command(['/usr/bin/systemctl', 'show', SERVICE, '-p', 'MainPID', '--value'],
                  'Observe the stopped service before recovery database provisioning').strip() == b'0',
          'The application service must be stopped before recovery database provisioning')
    check(recovery_database_baseline(database) == baseline,
          'The preexisting recovery catalog or absence proof changed after the complete backup')
    password = secrets.token_hex(32)
    payload = ('GOBY_RECOVERY_DATABASE_URL=postgresql://' + RECOVERY_ROLE_NAME + ':' + password +
               '@127.0.0.1:5432/' + RECOVERY_DATABASE_NAME + '?sslmode=disable\n').encode()
    intent = {'owner': OWNER, 'candidate_sha256': backup['candidate_sha256'],
              'baseline_sha256': _recovery_digest(baseline), 'tag': OWNER + ':recovery:' + secrets.token_hex(16),
              'environment_sha256': hashlib.sha256(payload).hexdigest()}
    _recovery_publish(RECOVERY_DB_INTENT, intent)
    # A crash before the environment identity receipt leaves a private file
    # that cleanup deliberately cannot adopt or unlink by content alone.
    private(RECOVERY_ENV, payload)
    sync_directory(ROOT)
    _recovery_record(RECOVERY_ENV_IDENTITY, intent, 'environment', _recovery_environment_proof(intent))
    _recovery_unpublished()
    verifier = _recovery_scram_verifier(password)
    _recovery_secret_sql((
        "BEGIN; SET LOCAL standard_conforming_strings=on; "
        'CREATE ROLE "' + RECOVERY_ROLE_NAME + '" LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE '
        'NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 16 PASSWORD ' + _recovery_literal(verifier) + '; '
        'COMMENT ON ROLE "' + RECOVERY_ROLE_NAME + '" IS ' + _recovery_literal(intent['tag']) + '; COMMIT;').encode())
    del password, payload, verifier
    current = _recovery_catalog()
    role, target = _recovery_protected_catalog(current, baseline)
    check(target is None, 'A recovery database appeared before its authorized creation')
    _recovery_expected_role(role, intent)
    _recovery_record(RECOVERY_ROLE_IDENTITY, intent, 'role', role)
    _recovery_unpublished()
    command(peer_argv('postgres'), 'Create only the closed independent recovery database',
            environment=peer_environment(False), data=(
                'CREATE DATABASE "' + RECOVERY_DATABASE_NAME + '" OWNER "' + RECOVERY_ROLE_NAME +
                '" TEMPLATE template0 ENCODING \'UTF8\' ALLOW_CONNECTIONS false CONNECTION LIMIT 16;').encode())
    current = _recovery_catalog()
    current_role, target = _recovery_protected_catalog(current, baseline)
    check(current_role == role and target is not None and type(target.get('oid')) is int and target['oid'] > 0 and
          target['allow_connections'] is False and target['template'] is False and target['encoding'] == 'UTF8' and
          target['connection_limit'] == 16 and target['comment'] is None,
          'The newly created closed recovery database has an unexpected catalog identity')
    # CREATE DATABASE cannot share a transaction with its tag or filesystem
    # receipt. Interruption anywhere before this receipt is durable leaves an
    # ambiguous database that cleanup must preserve, even when its name fits.
    expected = _recovery_literal(canonical(target).decode())
    _recovery_unpublished()
    command(peer_argv('postgres'), 'Bind the exact newly created recovery database ownership',
            environment=peer_environment(False), data=(
                'BEGIN; SET LOCAL standard_conforming_strings=on; DO $recovery$ BEGIN IF '
                '(SELECT ' + _recovery_database_expression() + ' FROM pg_catalog.pg_database d WHERE d.datname=' +
                _recovery_literal(RECOVERY_DATABASE_NAME) + ') IS DISTINCT FROM ' + expected +
                "::jsonb THEN RAISE EXCEPTION 'Recovery database identity changed'; END IF; END $recovery$; "
                'COMMENT ON DATABASE "' + RECOVERY_DATABASE_NAME + '" IS ' + _recovery_literal(intent['tag']) + '; '
                'REVOKE ALL ON DATABASE "' + RECOVERY_DATABASE_NAME + '" FROM PUBLIC; COMMIT;').encode())
    current = _recovery_catalog()
    current_role, closed = _recovery_protected_catalog(current, baseline)
    check(current_role == role and closed is not None and closed['safe_acl'] is True and
          closed == dict(target, comment=intent['tag'], acl=closed['acl'], safe_acl=True),
          'The tagged recovery database did not retain its exact intended OID and role')
    opened = dict(closed, allow_connections=True)
    _recovery_record(RECOVERY_DATABASE_IDENTITY, intent, 'database', {'closed': closed, 'open': opened})
    _recovery_record(RECOVERY_DATABASE_OPENING, intent, 'transition',
                     {'closed_sha256': _recovery_digest(closed), 'open_sha256': _recovery_digest(opened)})
    _recovery_unpublished()
    check(_recovery_protected_catalog(_recovery_catalog(), baseline) == (role, closed),
          'The recovery database changed before its planned connection enablement')
    command(peer_argv('postgres'), 'Enable connections only to the independently owned recovery database',
            environment=peer_environment(False), data=(
                'ALTER DATABASE "' + RECOVERY_DATABASE_NAME + '" ALLOW_CONNECTIONS true;').encode())
    schema = _recovery_schema_observation()
    _recovery_check_empty(schema, opened, baseline)
    _recovery_record(RECOVERY_DATABASE_READY, intent, 'ready', {'schema': schema})
    return verify_recovery_database(backup, database, require_empty=True)


def verify_recovery_database(backup, database, require_empty=True):
    """Return only safe identity evidence; never expose the URL or password."""
    baseline = _recovery_baseline(backup)
    intent = _recovery_intent(backup, baseline)
    role = _recovery_receipt(RECOVERY_ROLE_IDENTITY, intent, 'role')
    identity = _recovery_receipt(RECOVERY_DATABASE_IDENTITY, intent, 'database')
    _recovery_expected_role(role, intent)
    check(set(identity) == {'closed', 'open'} and identity['open'] == dict(identity['closed'], allow_connections=True) and
          identity['closed']['allow_connections'] is False and identity['open']['comment'] == intent['tag'] and
          identity['open']['owner_oid'] == role['oid'],
          'The durable recovery database identity does not contain the exact planned catalog states')
    check(_recovery_receipt(RECOVERY_DATABASE_OPENING, intent, 'transition') == {
        'closed_sha256': _recovery_digest(identity['closed']), 'open_sha256': _recovery_digest(identity['open'])} and
        not _recovery_present(RECOVERY_DATABASE_CLOSURE),
        'The recovery database connection transition proof changed or cleanup has already started')
    current = _recovery_catalog()
    check(_recovery_protected_catalog(current, baseline) == (role, identity['open']),
          'The recovery role, database OID, ownership tag, or catalog fingerprint changed')
    _recovery_main_identity(database, current)
    _recovery_environment(intent)
    schema = _recovery_schema_observation()
    if require_empty:
        _recovery_check_empty(schema, identity['open'], baseline)
        ready = _recovery_receipt(RECOVERY_DATABASE_READY, intent, 'ready')
        check(ready == {'schema': schema}, 'The recovery namespace changed after its durable empty-state proof')
    connection = _recovery_tcp_identity(intent, identity['open'], baseline)
    return {'owner': OWNER, 'status': 'verified', 'database': RECOVERY_DATABASE_NAME, 'role': RECOVERY_ROLE_NAME,
            'database_oid': identity['open']['oid'], 'role_oid': role['oid'],
            'ownership_tag': intent['tag'], 'database_fingerprint_sha256': _recovery_digest(identity['open']),
            'role_fingerprint_sha256': _recovery_digest(role), 'environment_path': str(RECOVERY_ENV),
            'environment_identity_verified': True, 'environment_mode': '0600', 'environment_owner_uid': 0,
            'postgresql_major': 17, 'same_primary_cluster': True, 'low_privilege_login': True,
            'no_role_memberships': True, 'public_empty': schema.get('empty') is True,
            'empty_state_required': require_empty, 'loopback_connection_verified': True,
            'connection_identity': connection, 'preexisting_catalog_unchanged': True}


def _recovery_quiescent(role, target):
    role_oid = role['oid'] if role is not None else 0
    database_oid = target['oid'] if target is not None else 0
    value = peer_read(
        "SELECT jsonb_build_object('sessions',EXISTS(SELECT 1 FROM pg_catalog.pg_stat_activity WHERE "
        'usesysid=' + str(role_oid) + ' OR datid=' + str(database_oid) + "),"
        "'prepared',EXISTS(SELECT 1 FROM pg_catalog.pg_prepared_xacts WHERE owner=" + _recovery_literal(RECOVERY_ROLE_NAME) +
        ' OR database=' + _recovery_literal(RECOVERY_DATABASE_NAME) + "),"
        "'slots',EXISTS(SELECT 1 FROM pg_catalog.pg_replication_slots WHERE datoid=" + str(database_oid) + "));")
    check(value == {'sessions': False, 'prepared': False, 'slots': False},
          'The owned recovery role or database has an active session, prepared transaction, or replication slot')


def _recovery_close_empty(intent, baseline, role, target):
    check(target['allow_connections'] is True and not _recovery_present(RECOVERY_DATABASE_CLOSURE),
          'The recovery database was reopened after closure or has an unexpected cleanup state')
    expected_database = _recovery_literal(canonical(target).decode())
    expected_role = _recovery_literal(canonical(role).decode())
    _recovery_unpublished()
    # Keep this exact peer backend connected across ALTER DATABASE. Once the
    # false state commits, new connections are rejected; any existing writer
    # makes the following observation fail. Inspect the empty namespace before
    # closing this last backend, then persist a closure receipt before DROP.
    quiet = (
        'EXISTS(SELECT 1 FROM pg_catalog.pg_stat_activity WHERE pid<>pg_catalog.pg_backend_pid() AND '
        '(datid=' + str(target['oid']) + ' OR usesysid=' + str(role['oid']) + ')) OR EXISTS('
        'SELECT 1 FROM pg_catalog.pg_prepared_xacts WHERE owner=' + _recovery_literal(RECOVERY_ROLE_NAME) +
        ' OR database=' + _recovery_literal(RECOVERY_DATABASE_NAME) + ') OR EXISTS('
        'SELECT 1 FROM pg_catalog.pg_replication_slots WHERE datoid=' + str(target['oid']) + ')')
    sql = (
        'BEGIN; SET LOCAL standard_conforming_strings=on; DO $recovery$ BEGIN IF current_database()<>' +
        _recovery_literal(RECOVERY_DATABASE_NAME) + ' OR (SELECT ' + _recovery_database_expression() +
        ' FROM pg_catalog.pg_database d WHERE d.datname=current_database()) IS DISTINCT FROM ' +
        expected_database + '::jsonb OR (SELECT ' + _recovery_role_expression() +
        ' FROM pg_catalog.pg_roles r WHERE r.rolname=' + _recovery_literal(RECOVERY_ROLE_NAME) +
        ') IS DISTINCT FROM ' + expected_role +
        "::jsonb THEN RAISE EXCEPTION 'Recovery closure identity changed'; END IF; END $recovery$; "
        'ALTER DATABASE "' + RECOVERY_DATABASE_NAME + '" ALLOW_CONNECTIONS false; COMMIT; '
        'BEGIN ISOLATION LEVEL READ COMMITTED READ ONLY; DO $recovery$ BEGIN IF ' + quiet +
        " THEN RAISE EXCEPTION 'Recovery database is not quiescent'; END IF; END $recovery$; "
        # Quiescence and schema inspection must be different READ COMMITTED
        # statements. A writer that committed just before its backend vanished
        # must be visible to the new schema snapshot after the first statement.
        'SELECT jsonb_build_object(\'schema\',(' + _recovery_schema_query().rstrip(';') + '),'
        "'exclusive',NOT EXISTS(SELECT 1 FROM pg_catalog.pg_stat_activity WHERE pid<>pg_catalog.pg_backend_pid() AND "
        '(datid=' + str(target['oid']) + ' OR usesysid=' + str(role['oid']) + ")),'no_prepared',NOT EXISTS("
        'SELECT 1 FROM pg_catalog.pg_prepared_xacts WHERE owner=' + _recovery_literal(RECOVERY_ROLE_NAME) +
        ' OR database=' + _recovery_literal(RECOVERY_DATABASE_NAME) + "),'no_slots',NOT EXISTS("
        'SELECT 1 FROM pg_catalog.pg_replication_slots WHERE datoid=' + str(target['oid']) + ')); COMMIT;')
    value = json.loads(command(peer_argv(RECOVERY_DATABASE_NAME),
                               'Fence and inspect only the exact unpublished recovery database before cleanup',
                               environment=peer_environment(False), data=sql.encode(), timeout=30))
    check(isinstance(value, dict) and set(value) == {'schema', 'exclusive', 'no_prepared', 'no_slots'} and
          value['exclusive'] is True and value['no_prepared'] is True and value['no_slots'] is True,
          'Recovery cleanup encountered another session or durable transaction; the closed database was retained')
    closed = dict(target, allow_connections=False)
    _recovery_check_empty(value['schema'], closed, baseline)
    if _recovery_present(RECOVERY_DATABASE_READY):
        check(_recovery_receipt(RECOVERY_DATABASE_READY, intent, 'ready') == {'schema': value['schema']},
              'Recovery cleanup found a changed namespace; the closed database was retained')
    check(_recovery_protected_catalog(_recovery_catalog(), baseline) == (role, closed),
          'The recovery catalog changed during connection fencing; the closed database was retained')
    _recovery_record(RECOVERY_DATABASE_CLOSURE, intent, 'closure', {'closed': closed, **value})
    return closed


def remove_recovery_database(backup):
    """Remove only exact unpublished ownership receipts; ambiguous residues remain private."""
    _recovery_unpublished()
    check(DEPLOYMENT_LOCK_HELD, 'Recovery cleanup requires the exclusive deployment operator lock')
    baseline = _recovery_baseline(backup)
    current = _recovery_catalog()
    role, target = _recovery_protected_catalog(current, baseline)
    if not _recovery_present(RECOVERY_DB_INTENT):
        check(role is None and target is None and not any(_recovery_present(path) for path in _recovery_paths()),
              'Recovery resources exist without a durable creation intent; all ambiguous residues were retained')
        return {'owner': OWNER, 'status': 'never_provisioned', 'database_removed_or_absent': True,
                'role_removed_or_absent': True, 'environment_removed_or_absent': True,
                'preexisting_catalog_unchanged': True}
    intent = _recovery_intent(backup, baseline)
    check(command(['/usr/bin/systemctl', 'show', SERVICE, '-p', 'MainPID', '--value'],
                  'Observe the stopped service before removing owned recovery resources').strip() == b'0',
          'The application service must be stopped before recovery resource cleanup')
    environment = None
    if _recovery_present(RECOVERY_ENV):
        check(_recovery_present(RECOVERY_ENV_IDENTITY),
              'The private recovery environment has no durable inode proof and was retained')
        environment = _recovery_receipt(RECOVERY_ENV_IDENTITY, intent, 'environment')
        check(_recovery_environment_proof(intent) == environment,
              'The recovery environment differs from its exact inode and content proof and was retained')
    owned_role = None
    if role is not None:
        check(_recovery_present(RECOVERY_ROLE_IDENTITY),
              'The recovery role has no durable OID receipt and was retained without guessing ownership')
        owned_role = _recovery_receipt(RECOVERY_ROLE_IDENTITY, intent, 'role')
        _recovery_expected_role(owned_role, intent)
        check(role == owned_role, 'The recovery role changed its exact OID, tag, or fingerprint and was retained')
    if target is not None:
        check(owned_role is not None and _recovery_present(RECOVERY_DATABASE_IDENTITY),
              'The recovery database has no complete durable OID receipt and was retained without adopting it')
        identity = _recovery_receipt(RECOVERY_DATABASE_IDENTITY, intent, 'database')
        check(set(identity) == {'closed', 'open'} and identity['open'] == dict(identity['closed'], allow_connections=True) and
              identity['closed']['allow_connections'] is False and target in (identity['closed'], identity['open']) and
              target['comment'] == intent['tag'] and target['owner_oid'] == owned_role['oid'],
              'The recovery database changed its exact OID, tag, or catalog fingerprint and was retained')
        opening = _recovery_present(RECOVERY_DATABASE_OPENING)
        if opening:
            check(_recovery_receipt(RECOVERY_DATABASE_OPENING, intent, 'transition') == {
                'closed_sha256': _recovery_digest(identity['closed']), 'open_sha256': _recovery_digest(identity['open'])},
                'The durable recovery connection transition does not match the owned database')
        if target['allow_connections']:
            check(opening, 'The recovery database accepted connections without its durable transition intent')
            target = _recovery_close_empty(intent, baseline, role, target)
        elif opening:
            check(_recovery_present(RECOVERY_DATABASE_CLOSURE),
                  'The recovery database closed after connection enablement without a complete closure proof and was retained')
        else:
            check(not _recovery_present(RECOVERY_DATABASE_READY) and not _recovery_present(RECOVERY_DATABASE_CLOSURE),
                  'A supposedly unopened recovery database has conflicting later ownership receipts')
        if opening:
            closure = _recovery_receipt(RECOVERY_DATABASE_CLOSURE, intent, 'closure')
            check(set(closure) == {'closed', 'schema', 'exclusive', 'no_prepared', 'no_slots'} and
                  closure['closed'] == target and closure['exclusive'] is True and
                  closure['no_prepared'] is True and closure['no_slots'] is True,
                  'The recovery closure receipt does not prove this exact quiescent database')
            _recovery_check_empty(closure['schema'], target, baseline)
            if _recovery_present(RECOVERY_DATABASE_READY):
                check(_recovery_receipt(RECOVERY_DATABASE_READY, intent, 'ready') == {'schema': closure['schema']},
                      'The recovery closure differs from its original empty namespace proof')
        _recovery_quiescent(role, target)
        _recovery_unpublished()
        check(_recovery_protected_catalog(_recovery_catalog(), baseline) == (role, target),
              'The exact recovery database ownership changed immediately before cleanup')
        # No FORCE, IF EXISTS, session termination, DROP OWNED, or CASCADE is
        # used. PostgreSQL must refuse any concurrent connection or dependency.
        command(peer_argv('postgres'), 'Remove only the exact unpublished owned recovery database',
                environment=peer_environment(False), data=('DROP DATABASE "' + RECOVERY_DATABASE_NAME + '";').encode())
    role, target = _recovery_protected_catalog(_recovery_catalog(), baseline)
    check(target is None, 'The owned recovery database removal was not proven')
    if role is not None:
        check(owned_role is not None and role == owned_role,
              'The recovery role changed before its dependency-restricted removal')
        _recovery_quiescent(role, None)
        _recovery_unpublished()
        expected = _recovery_literal(canonical(owned_role).decode())
        command(peer_argv('postgres'), 'Remove only the exact unpublished owned recovery role',
                environment=peer_environment(False), data=(
                    'BEGIN; SET LOCAL standard_conforming_strings=on; DO $recovery$ BEGIN IF '
                    '(SELECT ' + _recovery_role_expression() + ' FROM pg_catalog.pg_roles r WHERE r.rolname=' +
                    _recovery_literal(RECOVERY_ROLE_NAME) + ') IS DISTINCT FROM ' + expected +
                    '::jsonb OR EXISTS(SELECT 1 FROM pg_catalog.pg_auth_members WHERE roleid=' + str(role['oid']) +
                    ' OR member=' + str(role['oid']) + ' OR grantor=' + str(role['oid']) +
                    ") THEN RAISE EXCEPTION 'Recovery role ownership changed'; END IF; END $recovery$; "
                    'DROP ROLE "' + RECOVERY_ROLE_NAME + '"; COMMIT;').encode())
    check(_recovery_catalog() == baseline['catalog'],
          'The preexisting database and role catalog was not restored exactly after owned cleanup')
    if environment is not None:
        _recovery_unpublished()
        check(_recovery_environment_proof(intent) == environment,
              'The exact recovery environment file changed immediately before removal')
        RECOVERY_ENV.unlink()
        sync_directory(ROOT)
    check(not _recovery_present(RECOVERY_ENV), 'The owned recovery environment removal was not proven')
    return {'owner': OWNER, 'status': 'owned_resources_removed', 'database_removed_or_absent': True,
            'role_removed_or_absent': True, 'environment_removed_or_absent': True,
            'ownership_receipts_retained': True, 'preexisting_catalog_unchanged': True,
            'business_database_modified': False, 'hba_modified': False}


class RehearsalDatabase:
    def __init__(self, name):
        check(re.fullmatch(r'goby_m5j_restore_[0-9a-f]{24}', name), 'The rehearsal database escaped its fresh owned namespace')
        self.name = name

    def read(self, query):
        return json.loads(command(peer_argv(self.name), 'Isolated restored database observation', environment=peer_environment(),
                                  data=('SET ROLE goby_test; ' + query).encode(), timeout=25))


def rehearsal(database, baseline, sql_path):
    # No HBA, role or main-database setting is changed. Peer administration
    # creates one random database; restoration itself runs SET LOCAL ROLE
    # goby_test inside its transaction, matching the live application role.
    name = 'goby_m5j_restore_' + secrets.token_hex(12)
    main_start = database.read('SELECT to_json(pg_postmaster_start_time());')
    observed = peer_read("SELECT json_build_object('start',pg_postmaster_start_time(),'user',current_user,"
                         "'exists',EXISTS(SELECT 1 FROM pg_database WHERE datname='" + name + "'));")
    check(observed == {'start': main_start, 'user': 'postgres', 'exists': False}, 'The isolated recovery database server or fresh name differs')
    private(BACKUP / 'restore-rehearsal-intent.json', canonical({'owner': OWNER, 'database': name, 'postmaster_start': main_start}))
    sync_directory(BACKUP)
    created = False
    database_oid = None
    result = None
    try:
        command(['/usr/sbin/runuser', '-u', 'postgres', '--', '/usr/bin/createdb', '-h', '/var/run/postgresql', '-p', '5432',
                 '--owner=goby_test', '--template=template0', name], 'Create the fresh isolated recovery database',
                environment=peer_environment(False))
        created = True
        identity = peer_read("SELECT json_build_object('oid',oid::bigint,'owner',pg_get_userbyid(datdba)) FROM pg_database WHERE datname='" + name + "';")
        check(identity['owner'] == 'goby_test' and type(identity['oid']) is int, 'The created rehearsal database owner differs')
        database_oid = identity['oid']
        # The postgres OS account cannot read the root-private backup directory.
        # Root reads the already verified script and supplies it on stdin.
        command(peer_argv(name) + ['--single-transaction', '--file=-'],
                'Execute the real isolated schema-twenty-two recovery', environment=peer_environment(False),
                data=sql_path.read_bytes(), timeout=120)
        restored = snapshot(RehearsalDatabase(name), BASE_SCHEMA)
        check(restored == baseline, 'The real isolated recovery did not restore every old public row exactly')
        result = {'status': 'passed', 'schema_version': BASE_SCHEMA, 'tables': summaries(restored), 'restored_as_role': 'goby_test',
                  'isolated_database_import_executed': True, 'all_twenty_nine_tables_raw_exact': True,
                  'main_database_restore_executed': False, 'temporary_database_removed': False}
    finally:
        # Even a lost createdb response can only concern this previously absent
        # cryptographically fresh name recorded before the creation attempt.
        current = peer_read("SELECT COALESCE((SELECT json_build_object('oid',oid::bigint,'owner',pg_get_userbyid(datdba)) "
                            "FROM pg_database WHERE datname='" + name + "'),'null'::json);")
        if current is not None:
            check(current['owner'] == 'goby_test' and (database_oid is None or current['oid'] == database_oid) and
                  peer_read('SELECT to_json(pg_postmaster_start_time());') == main_start,
                  'Refusing to remove a rehearsal database whose ownership or server changed')
            command(['/usr/sbin/runuser', '-u', 'postgres', '--', '/usr/bin/dropdb', '-h', '/var/run/postgresql', '-p', '5432', name],
                    'Remove only the fresh owned recovery database', environment=peer_environment(False))
        check(peer_read("SELECT to_json(EXISTS(SELECT 1 FROM pg_database WHERE datname='" + name + "'));") is False,
              'The isolated recovery database cleanup was not proven')
    check(created and result is not None, 'The isolated recovery did not complete')
    result['temporary_database_removed'] = True
    return result


def create_backup(candidate, manifest_bytes, baseline, media, protected, old_sources, retained_assets, database, exported):
    parent = BACKUP.parent
    if not parent.exists():
        directory(parent.parent)
        parent.mkdir(mode=0o700)
        sync_directory(parent.parent)
    directory(parent)
    check(not BACKUP.exists() and not BACKUP.is_symlink(), 'The fixed M5j backup already exists')
    BACKUP.mkdir(mode=0o700)
    private(BACKUP / 'OWNER.txt', (OWNER + '\n').encode())
    sync_directory(parent)
    files = {}
    def save(relative, payload):
        path = BACKUP / relative
        path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        private(path, payload)
        sync_directory(path.parent)
        files[relative] = sha(path)
    save('candidate.json', manifest_bytes)
    save('candidate-assets.tar.gz', Path(candidate['assets']['path']).read_bytes())
    save('operator.py', Path(__file__).resolve().read_bytes())
    save('business-before.json', canonical(baseline))
    save('media-before.json', canonical(media))
    save('protected-before.json', canonical(protected))
    save('recovery-before.json', canonical(preflight_recovery()))
    save('recovery-database-before.json', canonical(recovery_database_baseline(database)))
    diagnostics = diagnostic_snapshot()
    check(len(diagnostics['files']) < 15, 'The existing diagnostic registry lacks restart retention headroom')
    save('diagnostics-before.json', canonical(diagnostics))
    save('source-before.json', canonical(old_sources))
    save('source-after.json', canonical(candidate['source']['files']))
    for name, path in (('goby-m5i', LIVE / 'goby'), ('runtime.env', RUNTIME), ('unit.service', UNIT), ('dropin.conf', DROPIN),
                       ('observability-dropin.conf', OBSERVABILITY_DROPIN), ('master.key', MASTER)):
        save(name, path.read_bytes())
    for relative in retained_assets:
        save('admin/' + relative, (LIVE / 'admin' / relative).read_bytes())
    for relative in old_sources:
        save('repository/' + relative, (REPOSITORY / relative).read_bytes())
    check(files['candidate-assets.tar.gz'] == candidate['assets']['sha256'] and
          all(files['repository/' + relative] == digest for relative, digest in old_sources.items()) and
          all(files['admin/' + relative] == digest for relative, digest in retained_assets.items()) and
          files['runtime.env'] == protected['runtime']['sha256'] and files['master.key'] == protected['master']['sha256'] and
          files['unit.service'] == protected['unit_sha256'] and files['dropin.conf'] == protected['dropin_sha256'] and
          files['observability-dropin.conf'] == protected['observability_dropin_sha256'],
          'A copied backup file differs from its independently observed source bytes')
    dump = BACKUP / 'database-schema22.dump'
    with os.fdopen(os.open(dump, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb') as output:
        result = subprocess.run(['/usr/lib/postgresql/17/bin/pg_dump', '--format=custom', '--schema=public', '--no-owner', '--no-acl', '--snapshot=' + exported],
                                env=database.environment, stdout=output, stderr=subprocess.PIPE, timeout=90)
        output.flush()
        os.fsync(output.fileno())
    if result.returncode != 0:
        private(BACKUP / 'dump-error.txt', result.stderr[:MIB])
    check(result.returncode == 0 and 0 < dump.stat().st_size <= 64 * MIB, 'The complete exported-snapshot database backup failed')
    files['database-schema22.dump'] = sha(dump)
    toc = command(['/usr/lib/postgresql/17/bin/pg_restore', '--list', str(dump)], 'Read the complete recovery archive inventory', limit=2 * MIB)
    save('restore-toc.list', filtered_restore_toc(toc))
    sql = command(restore_sql_command(dump, BACKUP / 'restore-toc.list'), 'Generate complete recovery SQL without a database connection', limit=128 * MIB)
    check(sql and not re.search(rb'(?im)^(?:CREATE|DROP|ALTER)\s+SCHEMA\b', sql), 'Recovery SQL must preserve the existing public schema itself')
    save('restore-schema22.sql', b'SET LOCAL ROLE goby_test;\n' + UPGRADE_DROP_SQL + sql)
    proof = rehearsal(database, baseline, BACKUP / 'restore-schema22.sql')
    save('restore-rehearsal.json', canonical(proof))
    files['restore-rehearsal-intent.json'] = sha(BACKUP / 'restore-rehearsal-intent.json')
    backup = {'owner': OWNER, 'phase': 'backup-complete', 'old_binary_sha256': OLD,
              'candidate_sha256': candidate['binary']['sha256'], 'candidate_manifest_sha256': hashlib.sha256(manifest_bytes).hexdigest(),
              'files': files, 'restore_sql_generated': True, 'isolated_restore_verified': True, 'live_restore_verified': False}
    validate_backup_document(backup)
    publish_exclusive(BACKUP / 'backup-manifest.json', canonical(backup))
    check(load_backup() == backup, 'The complete durable backup failed its independent byte checks')
    return backup


def verify_initial_binding(before, after, expected_deployment, earliest=None, latest=None, allow_unbound=False):
    # The only new server_settings row is a local startup binding. Existing
    # settings retain their complete raw JSON, including timestamps and types.
    check(not any(row.get('key') == RECOVERY_BINDING_KEY for row in parsed(before, 'server_settings')),
          'The accepted old service already has an unrelated recovery binding')
    old_rows, new_rows = before['server_settings'], after['server_settings']
    check(set(old_rows).issubset(new_rows), 'Upgrade changed or removed an old server setting')
    additions = [row for row in new_rows if row not in old_rows]
    if allow_unbound and not additions:
        check(new_rows == old_rows, 'The unbound startup changed original server settings')
        return False
    check(len(new_rows) == len(old_rows) + 1 and len(additions) == 1 and
          isinstance(expected_deployment, str) and re.fullmatch(r'[0-9a-f]{32}', expected_deployment),
          'Startup must add exactly one locally owned recovery binding')
    row = json.loads(additions[0])
    expected = json.dumps({'version': 1, 'deploymentId': expected_deployment, 'generationId': '', 'slot': 'primary'},
                          separators=(',', ':'))
    check(isinstance(row, dict) and set(row) == {'key', 'value', 'created_at', 'updated_at'} and
          row['key'] == RECOVERY_BINDING_KEY and row['value'] == expected and
          isinstance(row['created_at'], str) and row['created_at'] == row['updated_at'],
          'Startup added an unexpected server setting or foreign generation binding')
    try:
        observed = datetime.fromisoformat(row['created_at'].replace('Z', '+00:00'))
    except ValueError:
        raise Failure('The recovery binding timestamp is invalid') from None
    check(observed.tzinfo is not None and (earliest is None or observed >= earliest) and
          (latest is None or observed <= latest), 'The recovery binding timestamp is outside this deployment')
    return True


def verify_upgrade(database, before, after, earliest, latest, expected_deployment=None, allow_unbound=False):
    check(set(before) == OLD_TABLES and set(after) == OLD_TABLES,
          'The M5j upgrade must retain the exact twenty-nine table inventory')
    for table in OLD_TABLES - {'schema_migrations', 'server_settings'}:
        check(before[table] == after[table], 'Upgrade changed a preexisting public business field or row')
    check(set(before['schema_migrations']).issubset(after['schema_migrations']) and
          len(after['schema_migrations']) == len(before['schema_migrations']) + 1 and
          all(type(row['version']) is int for row in parsed(after, 'schema_migrations')) and
          {(row['version'], row['name']) for row in parsed(after, 'schema_migrations') if row['version'] > BASE_SCHEMA} ==
          {(TARGET_SCHEMA, '0023_backup_activity.sql')},
          'Upgrade changed prior migration history or added an unexpected migration')
    verify_initial_binding(before, after, expected_deployment, earliest, latest, allow_unbound)
    check(len(after['managed_settings']) == 1, 'The managed-settings singleton row count differs')

def http_bytes(route):
    connection = http.client.HTTPConnection('127.0.0.1', 18096, timeout=2)
    try:
        connection.request('GET', route)
        response = connection.getresponse()
        data = response.read(2 * MIB + 1)
        check(response.status == 200 and len(data) <= 2 * MIB, 'A bounded local HTTP observation failed')
        return data
    finally:
        connection.close()


def ready():
    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        try:
            http_bytes('/readyz')
            return
        except (OSError, Failure, http.client.HTTPException):
            time.sleep(0.2)
    raise Failure('The selected service did not become ready')


def check_assets(assets):
    for relative, payload in assets.items():
        path = target(LIVE / 'admin', relative)
        check(path.read_bytes() == payload, 'An installed administrator asset differs from its accepted bytes')
    check(http_bytes('/admin/') == assets['index.html'], 'The live administrator index differs')
    entries = re.findall(r'(?:src|href)="/admin/(assets/[^\"]+\.(?:js|css))"', assets['index.html'].decode())
    check(any(path.endswith('.js') for path in entries), 'The accepted index has no recognized JavaScript entry')
    for relative in entries:
        check(relative in assets and http_bytes('/admin/' + relative) == assets[relative], 'A live administrator entry asset differs')


def installation_proven(backup):
    if not INSTALLATION_STARTED.exists() and not INSTALLATION_STARTED.is_symlink():
        return False
    regular(INSTALLATION_STARTED, 1024, 0o600)
    try:
        value = json.loads(INSTALLATION_STARTED.read_bytes())
    except (UnicodeDecodeError, json.JSONDecodeError):
        return False
    check(value == {'owner': OWNER, 'candidate_sha256': backup['candidate_sha256']},
          'The durable installation-started proof has an unexpected owner or candidate')
    return True


def restart_uninstalled(database, backup, protected, baseline, media, old_sources):
    # A complete backup alone does not prove installation began. Never apply
    # its older rows to a service that could have kept accepting business writes.
    check(sha(LIVE / 'goby') == OLD and source_inventory(REPOSITORY) == old_sources and
          all(sha(LIVE / name) == expected for name, expected in backup['files'].items() if name.startswith('admin/')) and
          protected_state() == protected and media_state(baseline) == media,
          'Without installation proof only the untouched original service may be restarted')
    current_rows = snapshot(database, BASE_SCHEMA, check_catalog=False)
    pid = int(command(['/usr/bin/systemctl', 'show', SERVICE, '-p', 'MainPID', '--value'], 'Uninstalled recovery process observation').strip())
    if pid:
        check(sha(Path('/proc') / str(pid) / 'exe') == OLD, 'Uninstalled recovery found an unrelated running executable')
    command(['/usr/bin/systemctl', 'start', SERVICE], 'Restart the unchanged old service without applying a backup')
    ready()
    current = process_identity(OLD)
    check(snapshot(database, BASE_SCHEMA, check_catalog=False) == current_rows and protected_state() == protected,
          'The untouched old-service restart changed the latest current rows or configuration')
    return {'owner': OWNER, 'status': 'original_service_restarted', **current, 'schema_version': BASE_SCHEMA,
            'latest_schema22_rows_raw_exact': True, 'actual_live_database_restore_executed': False}


def restore():
    check(not EVIDENCE.exists() and not EVIDENCE.is_symlink() and not RECOVERY_EVIDENCE.exists() and not RECOVERY_EVIDENCE.is_symlink(),
          'A completed deployment or recovery cannot be overwritten with an old backup')
    check(DEPLOYMENT_LOCK_HELD, 'Recovery requires the exclusive deployment lock')
    backup = load_backup()
    database = Database()
    protected = json.loads((BACKUP / 'protected-before.json').read_bytes())
    baseline = json.loads((BACKUP / 'business-before.json').read_bytes())
    media = json.loads((BACKUP / 'media-before.json').read_bytes())
    diagnostics = json.loads((BACKUP / 'diagnostics-before.json').read_bytes())
    old_sources = json.loads((BACKUP / 'source-before.json').read_bytes())
    candidate_sources = json.loads((BACKUP / 'source-after.json').read_bytes())
    candidate_assets = read_assets(BACKUP / 'candidate-assets.tar.gz')
    check(json.loads((BACKUP / 'recovery-before.json').read_bytes()) == recovery_baseline(),
          'Recovery has no matching preexisting recovery-path absence proof')
    check(protected_state(allow_recovery=True) == protected and (BACKUP / 'master.key').read_bytes() == MASTER.read_bytes() and
          media_state(baseline) == media and preserve_diagnostic_history(diagnostics),
          'Recovery requires the original configuration and matching unchanged master, media and diagnostic history')
    regular(LIVE / 'goby', 256 * MIB, 0o755)
    check(sha(LIVE / 'goby') in {OLD, backup['candidate_sha256']}, 'Recovery found an unrelated installed executable')
    if not installation_proven(backup):
        return restart_uninstalled(database, backup, protected, baseline, media, old_sources)
    pid = int(command(['/usr/bin/systemctl', 'show', SERVICE, '-p', 'MainPID', '--value'], 'Recovery process observation').strip())
    if pid:
        actual = sha(Path('/proc') / str(pid) / 'exe')
        check(actual in {OLD, backup['candidate_sha256']}, 'Recovery refuses an unrelated live executable')
        process_identity(actual)
    command(['/usr/bin/systemctl', 'stop', SERVICE], 'Stop only the owned service for complete recovery')
    check(command(['/usr/bin/systemctl', 'show', SERVICE, '-p', 'MainPID', '--value'], 'Recovery stopped-process observation').strip() == b'0',
          'The service did not stop for recovery')
    version = database.read('SELECT to_json(max(version)) FROM public.schema_migrations;')
    check(type(version) is int and version in {BASE_SCHEMA, TARGET_SCHEMA}, 'Recovery found an unrelated migration version')
    observed = snapshot(database, version, check_catalog=False)
    binding_present = any(row.get('key') == RECOVERY_BINDING_KEY for row in parsed(observed, 'server_settings'))
    deployment = initial_deployment_id(backup) if binding_present else None
    # This is the final compare-and-swap barrier before every destructive
    # restore. Even one new session, activity row or backup admission refuses it.
    if version == BASE_SCHEMA:
        check(observed == baseline, 'Recovery refuses business writes accepted after the complete backup')
    else:
        verify_upgrade(database, baseline, observed, None, None, deployment, allow_unbound=True)
    for relative, digest in source_inventory(REPOSITORY).items():
        check(relative in set(old_sources) | set(candidate_sources) and
              digest in {old_sources.get(relative), candidate_sources.get(relative)},
              'Recovery refuses a source changed outside this installation')
    old_assets = {name[6:]: digest for name, digest in backup['files'].items() if name.startswith('admin/')}
    for relative, digest in old_assets.items():
        path = target(LIVE / 'admin', relative)
        expected = {digest}
        if relative in candidate_assets:
            expected.add(hashlib.sha256(candidate_assets[relative]).hexdigest())
        check(path.exists() and sha(path) in expected, 'Recovery refuses an administrator asset changed outside this installation')
    retained_recovery = preserve_recovery(backup)
    check(protected_state() == protected, 'The original loaded service configuration was not restored')
    # Cleanup requires the independently owned inactive database to remain
    # exactly empty. Any restore work accepted by the candidate blocks cleanup.
    recovery_database = remove_recovery_database(backup)
    database_restored = version == TARGET_SCHEMA
    if database_restored:
        environment = dict(database.environment)
        environment['PGOPTIONS'] = peer_environment(False)['PGOPTIONS']
        command(restore_database_command(BACKUP / 'restore-schema22.sql'), 'Execute complete live schema-twenty-two recovery', environment=environment, timeout=120)
    check(snapshot(database, BASE_SCHEMA) == baseline, 'Recovered database rows differ from the complete current baseline')
    replace_file(LIVE / 'goby', (BACKUP / 'goby-m5i').read_bytes(), 0o755)
    for relative in old_assets:
        path = target(LIVE / 'admin', relative)
        parent_for(LIVE / 'admin', path)
        replace_file(path, (BACKUP / 'admin' / relative).read_bytes(), 0o644)
    for relative in set(candidate_sources) | set(old_sources):
        path = target(REPOSITORY, relative)
        if relative in old_sources:
            parent_for(REPOSITORY, path)
            replace_file(path, (BACKUP / 'repository' / relative).read_bytes(), 0o644)
        elif path.exists():
            check(sha(path) == candidate_sources[relative], 'Recovery refuses to remove a source not installed by this candidate')
            path.unlink()
            sync_directory(path.parent)
    check(source_inventory(REPOSITORY) == old_sources and sha(LIVE / 'goby') == OLD and
          all(sha(LIVE / 'admin' / relative) == digest for relative, digest in old_assets.items()) and
          protected_state() == protected and media_state(baseline) == media and preserve_diagnostic_history(diagnostics),
          'Recovered executable, sources, assets or protected state differ')
    command(['/usr/bin/systemctl', 'start', SERVICE], 'Start the restored original service')
    ready()
    current = process_identity(OLD)
    check(snapshot(database, BASE_SCHEMA) == baseline and protected_state() == protected and media_state(baseline) == media and
          preserve_diagnostic_history(diagnostics), 'The restored original service changed protected rows or files')
    report = {'owner': OWNER, 'status': 'restored', **current, 'schema_version': BASE_SCHEMA, 'all_old_public_rows_raw_exact': True,
              'full_old_source_binary_assets_verified': True, 'matching_master_media_configuration_preserved': True,
              'actual_live_database_restore_executed': database_restored, 'isolated_restore_rehearsal_passed': True,
              'original_diagnostic_dropin_and_history_preserved': True, 'retained_recovery': retained_recovery,
              'recovery_database_cleanup': recovery_database}
    publish_exclusive(RECOVERY_EVIDENCE, canonical(report))
    return report

def load_candidate(path, expected):
    check(isinstance(expected, str) and HASH.fullmatch(expected) and path.parent == SCRATCH and path.suffix == '.json',
          'The accepted candidate manifest must be a fixed scratch JSON file with an explicit SHA-256')
    regular(path, 2 * MIB, 0o600)
    raw = path.read_bytes()
    check(hashlib.sha256(raw).hexdigest() == expected, 'The accepted candidate manifest bytes differ')
    candidate = validate_candidate_document(json.loads(raw))
    check(raw == canonical(candidate), 'The accepted candidate manifest is not canonical JSON')
    for gate in candidate['gates'].values():
        evidence = Path(gate['evidence_path'])
        regular(evidence, 128 * MIB)
        check(evidence.stat().st_mode & 0o022 == 0 and sha(evidence) == gate['sha256'], 'An accepted final gate evidence file changed or is writable by others')
    source = Path(candidate['source']['resolved_path'])
    check(Path(candidate['source']['path']).resolve(strict=True) == source, 'The accepted source snapshot target differs')
    directory(source)
    regular(source / 'OWNER.txt', 256)
    check((source / 'OWNER.txt').read_text().strip() == candidate['source']['owner_marker'], 'The accepted source snapshot ownership marker differs')
    check(source_inventory(source) == candidate['source']['files'], 'The complete accepted source inventory or its bytes differ')
    binary = Path(candidate['binary']['path'])
    regular(binary, 256 * MIB, 0o755)
    check(binary.stat().st_size == candidate['binary']['size'] and sha(binary) == candidate['binary']['sha256'], 'The accepted executable bytes differ')
    archive = Path(candidate['assets']['path'])
    regular(archive, 32 * MIB)
    check(archive.stat().st_mode & 0o022 == 0 and sha(archive) == candidate['assets']['sha256'], 'The accepted administrator asset archive differs')
    assets = read_assets(archive)
    check(len(assets) == candidate['assets']['files'], 'The accepted current administrator asset count differs')
    return candidate, raw, assets


def preflight_paths(candidate, assets):
    for path in (LIVE, LIVE / 'admin', LIVE / 'admin/assets', REPOSITORY):
        directory(path, 0o755)
    regular(LIVE / 'goby', 256 * MIB, 0o755)
    check(sha(LIVE / 'goby') == OLD, 'The installed old executable differs')
    old_sources = source_inventory(REPOSITORY)
    check(len(old_sources) == OLD_SOURCE_COUNT and
          hashlib.sha256(canonical(old_sources)).hexdigest() == OLD_SOURCE_MANIFEST_SHA,
          'The installed source no longer matches the accepted M5i baseline')
    regular(OLD_ASSETS_ARCHIVE, 32 * MIB)
    check(OLD_ASSETS_ARCHIVE.stat().st_mode & 0o022 == 0 and sha(OLD_ASSETS_ARCHIVE) == OLD_ASSETS_SHA,
          'The retained accepted M5i asset archive differs')
    old_current_assets = read_assets(OLD_ASSETS_ARCHIVE)
    check(len(old_current_assets) == OLD_ASSET_COUNT and
          all(target(LIVE / 'admin', relative).read_bytes() == payload for relative, payload in old_current_assets.items()),
          'The currently served M5i administrator assets differ from their accepted bytes')
    for relative in set(candidate['source']['files']) | set(old_sources):
        target(REPOSITORY, relative)
    retained = {}
    for path in (LIVE / 'admin').rglob('*'):
        if path.is_dir():
            directory(path, 0o755)
        else:
            regular(path, 2 * MIB, 0o644)
            relative = path.relative_to(LIVE / 'admin').as_posix()
            check(asset_entry(relative), 'A retained administrator file escaped the static asset scope')
            retained[relative] = sha(path)
    check('index.html' in retained and len(retained) <= 1000, 'The complete retained administrator asset inventory differs')
    for relative, payload in assets.items():
        path = target(LIVE / 'admin', relative)
        if path.exists() and relative != 'index.html':
            check(path.read_bytes() == payload, 'The candidate attempted to change an old immutable asset')
    return old_sources, retained


def quiescent(database, baseline):
    check(not any(row.get('key') == RECOVERY_BINDING_KEY for row in parsed(baseline, 'server_settings')),
          'The preexisting service already has a recovery binding outside this deployment')
    check(not any(row['status'] in {'Queued', 'Running'} for row in parsed(baseline, 'scan_jobs')) and
          not any(row['state'] in {'queued', 'running'} for row in parsed(baseline, 'encoding_jobs')),
          'An active scan or encoding job prevents exact restart preservation')
    check(not any(row['state'] in {'pending', 'running', 'stopping'} for row in parsed(baseline, 'task_runs')) and
          not any(row['state'] in {'waiting', 'queued', 'running'} for row in parsed(baseline, 'task_run_children')),
          'An active task run or child prevents exact task-history preservation')
    definitions = parsed(baseline, 'task_definitions')
    by_id = {row['id']: row for row in definitions}
    executable = [row for row in definitions if row['key'] == 'library.scan']
    check(len(executable) == 1 and
          tuple(executable[0][name] for name in ('emby_key', 'name', 'description', 'category')) ==
          ('RefreshLibrary', 'Scan media library', 'Scan all registered media libraries.', 'Library') and
          not any(row['key'] != 'library.scan' and row['enabled'] for row in definitions),
          'Startup task reconciliation would change a preexisting definition')
    check(not any(by_id[row['task_id']]['key'] == 'library.scan' and by_id[row['task_id']]['enabled'] and
                  row['retired_at'] is None and row['calculation_error'] == ''
                  for row in parsed(baseline, 'task_triggers')),
          'A runnable startup or timed trigger could change old task history during deployment')
    check(database.read("SELECT to_json(EXISTS(SELECT 1 FROM play_sessions WHERE state IN ('Prepared','Playing','Paused') "
                        "AND expires_at > clock_timestamp()));") is False, 'An unexpired playback prevents this deployment')


def verify_installed_candidate(database, candidate, assets, backup, before, protected, media, retained, diagnostic_before,
                               manifest_sha256, started=None, expected_process=None):
    # Shared post-install observations never start, stop or restore a service.
    current = process_identity(candidate['binary']['sha256'])
    check(expected_process is None or current == expected_process, 'The installed candidate process differs from its pinned finalization identity')
    recovery = verify_recovery(backup)
    recovery_database = verify_recovery_database(backup, database)
    after = snapshot(database, TARGET_SCHEMA)
    latest = datetime.fromisoformat(database.read('SELECT to_json(clock_timestamp());').replace('Z', '+00:00'))
    verify_upgrade(database, before, after, started, latest, recovery['deployment_id'])
    check(source_inventory(REPOSITORY) == candidate['source']['files'] and protected_state(allow_recovery=True) == protected and media_state(before) == media,
          'The installed source or original configuration/master/media changed')
    check(all(sha(LIVE / 'admin' / relative) == expected for relative, expected in retained.items() if relative != 'index.html'),
          'An old immutable administrator asset was removed or changed')
    check_assets(assets)
    check(verify_recovery(backup) == recovery and verify_recovery_database(backup, database) == recovery_database and
          preserve_diagnostic_history(diagnostic_before), 'The accepted initial recovery state or original diagnostic bytes changed')
    diagnostics = diagnostic_snapshot()
    check(process_identity(candidate['binary']['sha256']) == current and snapshot(database, TARGET_SCHEMA) == after and
          protected_state(allow_recovery=True) == protected and media_state(before) == media and load_backup() == backup,
          'The completed candidate changed during final preservation checks')
    report = {'owner': OWNER, 'status': 'passed', **current, 'schema_version': TARGET_SCHEMA, 'probe_version': 6,
              'previous_binary_sha256': OLD, 'previous_pid': OLD_PID, 'previous_start_ticks': OLD_START,
              'candidate_manifest_sha256': manifest_sha256, 'accepted_gates': candidate['gates'],
              'all_old_public_business_fields_preserved': True, 'old_task_history_preserved': True, 'old_server_settings_preserved': True,
              'managed_settings_rows': len(after['managed_settings']),
              'managed_settings_revision_before': parsed(before, 'managed_settings')[0]['revision'],
              'managed_settings_revision_after': parsed(after, 'managed_settings')[0]['revision'],
              'managed_settings_old_fields_revision_timestamps_exact': True,
              'managed_settings_name_mode': parsed(after, 'managed_settings')[0]['server_name_mode'],
              'managed_settings_compatibility_max_width': parsed(after, 'managed_settings')[0]['compatibility_max_width'],
              'managed_settings_all_schema22_fields_raw_exact': True, 'activity_entries_count': len(after['activity_entries']),
              'activity_history_inferred_or_startup_event_inserted': False, 'diagnostics': diagnostics,
              'original_diagnostic_dropin_and_history_preserved': True, 'original_reference_process_preserved': True,
              'new_owned_recovery_dropin_verified': True, 'recovery': recovery, 'recovery_database': recovery_database,
              'server_settings_added_initial_recovery_binding': 1, 'recovery_database_url_configured': True,
              'master_runtime_unit_media_preserved': True, 'media_files_hashed': len(media),
              'current_asset_files': len(assets), 'retained_asset_files': len(retained), 'http_index_and_entries_verified': True,
              'installed_source_files': len(candidate['source']['files']), 'source_files_sha256': hashlib.sha256(canonical(candidate['source']['files'])).hexdigest(),
              'tables_before': summaries(before), 'tables_after': summaries(after), 'backup_directory': str(BACKUP),
              'backup_complete_before_service_stop': True, 'restore_sql_generated': True, 'isolated_restore_verified': True,
              'isolated_restore_tables_raw_exact': 29, 'isolated_restore_database_removed': True,
              'live_database_restore_executed': False, 'media_scan_or_task_execution_issued': False,
              'deployment_script_sha256': sha(Path(__file__).resolve())}
    return report


@contextmanager
def forward_read_only():
    global FINALIZATION_READ_ONLY
    check(not FINALIZATION_READ_ONLY, 'Forward finalization is not reentrant')
    FINALIZATION_READ_ONLY = True
    try:
        yield
    finally:
        FINALIZATION_READ_ONLY = False


def expected_finalization_process():
    return {'main_pid': FINALIZATION_PID, 'uid': 995, 'gid': 986, 'start_ticks': FINALIZATION_START,
            'binary_sha256': FINALIZATION_BINARY_SHA}


def load_remediation_evidence(path, expected_sha):
    check(isinstance(expected_sha, str) and HASH.fullmatch(expected_sha) and
          path.is_absolute() and path.is_relative_to(ROOT / 'exec-work-m5j') and path.suffix == '.json',
          'Forward finalization requires an explicit new remediation report and SHA-256')
    info = regular(path, 2 * MIB)
    check(info.st_mode & 0o022 == 0, 'The remediation report is writable outside its owner')
    raw = path.read_bytes()
    check(hashlib.sha256(raw).hexdigest() == expected_sha, 'The accepted remediation report bytes changed')
    value = json.loads(raw)
    operator = Path(__file__).resolve()
    suite = operator.with_name('test-deploy-backup-recovery.py')
    for artifact in (operator, suite):
        artifact_info = regular(artifact, 2 * MIB)
        check(artifact.is_relative_to(ROOT / 'exec-work-m5j') and artifact_info.st_mode & 0o022 == 0,
              'A remediation source is outside its protected staging directory')
    tests = value.get('contractTestNames') if isinstance(value, dict) else None
    check(isinstance(value, dict) and value.get('suite') == 'm5j-deployment-contracts' and value.get('status') == 'passed' and
          value.get('fixtures') == 'synthetic-memory-only' and value.get('failures') == value.get('errors') == value.get('skipped') == 0 and
          value.get('realDatabaseRestoreAcceptance') is False and value.get('realDeploymentAcceptance') is False and
          value.get('operatorSha256') == sha(operator) and value.get('suiteSha256') == sha(suite) and
          isinstance(tests, list) and all(isinstance(name, str) for name in tests) and tests == sorted(set(tests)) and
          FINALIZATION_REQUIRED_TESTS <= set(tests) and type(value.get('testsRun')) is int and
          value['testsRun'] == len(tests) and value['testsRun'] > 43,
          'The remediation report does not bind the current operator, suite and required passing contracts')
    return {'path': str(path), 'sha256': expected_sha, 'operator_sha256': value['operatorSha256'],
            'suite_path': str(suite), 'suite_sha256': value['suiteSha256'], 'tests': value['testsRun'], 'contract_tests': tests}


def finalization_start_time():
    properties = systemd_properties(command(
        ['/usr/bin/systemctl', 'show', SERVICE, '-p', 'MainPID', '-p', 'ExecMainStartTimestamp'],
        'Observe the pinned installation service start time', environment={'PATH': '/usr/bin:/bin', 'LANG': 'C', 'TZ': 'UTC'}),
        ('MainPID', 'ExecMainStartTimestamp'))
    check(properties['MainPID'] == str(FINALIZATION_PID), 'The service start timestamp belongs to a different process')
    match = re.fullmatch(r'[A-Za-z]{3} ([0-9]{4}-[0-9]{2}-[0-9]{2}) ([0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,6})?) UTC',
                         properties['ExecMainStartTimestamp'])
    check(match is not None, 'The pinned service start timestamp is not a complete UTC observation')
    try:
        return datetime.fromisoformat(match[1] + 'T' + match[2] + '+00:00')
    except ValueError:
        raise Failure('The pinned service start timestamp is invalid') from None


def finalization_baseline(backup, candidate, manifest_bytes, assets):
    check(backup['candidate_manifest_sha256'] == FINALIZATION_CANDIDATE_SHA and
          backup['candidate_sha256'] == FINALIZATION_BINARY_SHA and
          backup['files']['operator.py'] == FINALIZATION_ORIGINAL_OPERATOR_SHA and
          (BACKUP / 'candidate.json').read_bytes() == manifest_bytes and
          read_assets(BACKUP / 'candidate-assets.tar.gz') == assets and installation_proven(backup),
          'Forward finalization does not have the exact original deployment and candidate proofs')
    before = json.loads((BACKUP / 'business-before.json').read_bytes())
    old_sources = json.loads((BACKUP / 'source-before.json').read_bytes())
    after_sources = json.loads((BACKUP / 'source-after.json').read_bytes())
    check(set(before) == OLD_TABLES and len(before['schema_migrations']) == BASE_SCHEMA and
          {row['version'] for row in parsed(before, 'schema_migrations')} == set(range(1, BASE_SCHEMA + 1)) and
          len(old_sources) == OLD_SOURCE_COUNT and hashlib.sha256(canonical(old_sources)).hexdigest() == OLD_SOURCE_MANIFEST_SHA and
          after_sources == candidate['source']['files'] and len(after_sources) == 576 and
          all(backup['files']['repository/' + name] == digest for name, digest in old_sources.items()),
          'The original complete backup rows or old and installed source inventories differ')
    rehearsal = json.loads((BACKUP / 'restore-rehearsal.json').read_bytes())
    check(rehearsal.get('status') == 'passed' and rehearsal.get('schema_version') == BASE_SCHEMA and
          rehearsal.get('tables') == summaries(before) and rehearsal.get('restored_as_role') == 'goby_test' and
          rehearsal.get('isolated_database_import_executed') is True and rehearsal.get('all_twenty_nine_tables_raw_exact') is True and
          rehearsal.get('main_database_restore_executed') is False and rehearsal.get('temporary_database_removed') is True,
          'Forward finalization requires the original actual complete twenty-nine-table isolated restore rehearsal')
    protected = json.loads((BACKUP / 'protected-before.json').read_bytes())
    media = json.loads((BACKUP / 'media-before.json').read_bytes())
    diagnostic_before = json.loads((BACKUP / 'diagnostics-before.json').read_bytes())
    retained = {name[6:]: digest for name, digest in backup['files'].items() if name.startswith('admin/')}
    check('index.html' in retained and (BACKUP / 'master.key').read_bytes() == MASTER.read_bytes() and
          json.loads((BACKUP / 'recovery-before.json').read_bytes()) == recovery_baseline(),
          'The original retained administrator assets, matching master or recovery absence proof differ')
    return {'before': before, 'protected': protected, 'media': media, 'retained': retained, 'diagnostic_before': diagnostic_before}


def finalize_installation(args):
    check(DEPLOYMENT_LOCK_HELD, 'Forward finalization requires the exclusive deployment lock')
    check(not EVIDENCE.exists() and not EVIDENCE.is_symlink() and not RECOVERY_EVIDENCE.exists() and not RECOVERY_EVIDENCE.is_symlink(),
          'A published deployment or recovery cannot be finalized again')
    check(args.candidate_manifest == SCRATCH / 'm5j-candidate.json' and args.manifest_sha256 == FINALIZATION_CANDIDATE_SHA,
          'Forward finalization is restricted to the specifically reviewed original candidate')
    with forward_read_only():
        remediation = load_remediation_evidence(args.remediation_report, args.remediation_sha256)
        candidate, manifest_bytes, assets = load_candidate(args.candidate_manifest, args.manifest_sha256)
        check(candidate['binary'] == {'path': str(SCRATCH / 'goby-m5j-linux-amd64'), 'sha256': FINALIZATION_BINARY_SHA,
                                      'size': FINALIZATION_BINARY_BYTES}, 'The installed forward-finalization binary differs from its reviewed bytes')
        backup = load_backup()
        baseline = finalization_baseline(backup, candidate, manifest_bytes, assets)
        directory(LIVE, 0o755)
        installed = regular(LIVE / 'goby', 256 * MIB, 0o755)
        check(installed.st_size == FINALIZATION_BINARY_BYTES and sha(LIVE / 'goby') == FINALIZATION_BINARY_SHA,
              'The installed executable path differs from the reviewed running candidate')
        for relative in baseline['retained']:
            target(LIVE / 'admin', relative)
        expected = expected_finalization_process()
        check(process_identity(FINALIZATION_BINARY_SHA) == expected, 'The installed candidate PID, start ticks, UID or GID changed')
        started = finalization_start_time()
        database = Database()
        http_bytes('/readyz')
        report = verify_installed_candidate(database, candidate, assets, backup, baseline['before'], baseline['protected'],
                                            baseline['media'], baseline['retained'], baseline['diagnostic_before'],
                                            args.manifest_sha256, started=started, expected_process=expected)
        observed_at = datetime.fromisoformat(database.read('SELECT to_json(clock_timestamp());').replace('Z', '+00:00'))
        check(started <= observed_at and load_candidate(args.candidate_manifest, args.manifest_sha256) == (candidate, manifest_bytes, assets) and
              load_backup() == backup and load_remediation_evidence(args.remediation_report, args.remediation_sha256) == remediation,
              'The original candidate, complete backup or remediation evidence changed during finalization')
        check(source_inventory(REPOSITORY) == candidate['source']['files'] and
              regular(LIVE / 'goby', 256 * MIB, 0o755).st_size == FINALIZATION_BINARY_BYTES and
              sha(LIVE / 'goby') == FINALIZATION_BINARY_SHA and
              summaries(snapshot(database, TARGET_SCHEMA)) == report['tables_after'] and
              protected_state(allow_recovery=True) == baseline['protected'] and media_state(baseline['before']) == baseline['media'] and
              verify_recovery(backup) == report['recovery'] and verify_recovery_database(backup, database) == report['recovery_database'] and
              preserve_diagnostic_history(baseline['diagnostic_before']),
              'The final candidate rows, source, sandbox, stores, master, media or reference state changed before publication')
        report['deployment_script_sha256'] = backup['files']['operator.py']
        report['original_operator_sha256'] = backup['files']['operator.py']
        report['remediation_operator_sha256'] = remediation['operator_sha256']
        report['forward_finalization'] = {'mode': 'read-only-verification-and-evidence-publication',
            'reason': 'systemd-repeated-environment-files', 'remediation_report': remediation,
            'original_candidate_manifest_sha256': args.manifest_sha256,
            'original_backup_manifest_sha256': sha(BACKUP / 'backup-manifest.json'),
            'binding_time_source': 'systemd.ExecMainStartTimestamp', 'binding_time_lower_bound': started.isoformat(),
            'binding_time_upper_bound': observed_at.isoformat(), 'service_stop_or_restart_executed': False,
            'database_write_executed': False, 'original_backup_files_unchanged': True,
            'current_pid_revalidated_before_publication': True}
        check(not EVIDENCE.exists() and not EVIDENCE.is_symlink() and not RECOVERY_EVIDENCE.exists() and not RECOVERY_EVIDENCE.is_symlink() and
              process_identity(FINALIZATION_BINARY_SHA) == expected,
              'The pinned process or unpublished evidence boundary changed immediately before publication')
        publish_exclusive(EVIDENCE, canonical(report))
        return report


def arguments():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate-manifest', type=Path)
    parser.add_argument('--manifest-sha256')
    parser.add_argument('--restore', action='store_true')
    parser.add_argument('--finalize', action='store_true')
    parser.add_argument('--remediation-report', type=Path)
    parser.add_argument('--remediation-sha256')
    result = parser.parse_args()
    candidate_args = result.candidate_manifest is not None and result.manifest_sha256 is not None
    remediation_args = result.remediation_report is not None and result.remediation_sha256 is not None
    no_remediation_args = result.remediation_report is None and result.remediation_sha256 is None
    check((result.restore and not result.finalize and result.candidate_manifest is None and result.manifest_sha256 is None and no_remediation_args) or
          (not result.restore and not result.finalize and candidate_args and no_remediation_args) or
          (result.finalize and not result.restore and candidate_args and remediation_args),
          'Supply restore alone, an installation candidate, or finalize with that candidate and the new remediation report hashes')
    return result


def main():
    check(sys.platform == 'linux' and os.geteuid() == 0 and os.environ.get('SSH_CONNECTION'), 'Run only through authorized Linux root SSH')
    signal.signal(signal.SIGTERM, interrupt_signal)
    signal.signal(signal.SIGHUP, interrupt_signal)
    os.umask(0o077)
    args = arguments()
    directory(ROOT)
    directory(SCRATCH, 0o700)
    with deployment_lock():
        locked_main(args)


def locked_main(args):
    check(DEPLOYMENT_LOCK_HELD, 'Deployment requires its exclusive operator lock')
    if getattr(args, 'finalize', False):
        # This branch is outside the installation exception handler. Neither a
        # failed observation nor a failed stdout write can enter restore().
        report = finalize_installation(args)
        print(json.dumps({'status': 'passed', 'main_pid': report['main_pid'], 'schema_version': TARGET_SCHEMA,
                          'binary_sha256': report['binary_sha256'], 'evidence': str(EVIDENCE), 'forward_finalized': True}))
        return
    if args.restore:
        print(json.dumps(restore(), sort_keys=True))
        return
    check(not BACKUP.exists() and not BACKUP.is_symlink(), 'This M5j backup already exists; no prior attempt is overwritten')
    candidate, manifest_bytes, assets = load_candidate(args.candidate_manifest, args.manifest_sha256)
    old_sources, retained = preflight_paths(candidate, assets)
    preflight_recovery()
    prior = process_identity(OLD)
    check(prior['main_pid'] == OLD_PID and prior['start_ticks'] == OLD_START, 'The accepted M5i process identity changed')
    protected = protected_state()
    database = Database()
    recovery_database_baseline(database)
    started = datetime.fromisoformat(database.read('SELECT to_json(clock_timestamp());').replace('Z', '+00:00'))
    source = Path(candidate['source']['resolved_path'])
    # The root filesystem now has dedicated backup capacity. Never spill this
    # persistent recovery bundle into any preceding deployment backup directory.
    payload_bytes = sum((source / path).stat().st_size for path in candidate['source']['files'])
    old_bytes = sum((REPOSITORY / path).stat().st_size for path in old_sources) + sum((LIVE / 'admin' / path).stat().st_size for path in retained)
    check(shutil.disk_usage(ROOT).free > old_bytes + payload_bytes + candidate['binary']['size'] + 512 * MIB,
          'Persistent root storage cannot hold the bounded complete backup and installation staging')
    with exported_snapshot(database) as exported:
        before = snapshot(database, BASE_SCHEMA, exported)
        quiescent(database, before)
        media = media_state(before)
        backup = create_backup(candidate, manifest_bytes, before, media, protected, old_sources, retained, database, exported)
    diagnostic_before = json.loads((BACKUP / 'diagnostics-before.json').read_bytes())
    check(snapshot(database, BASE_SCHEMA) == before and process_identity(OLD) == prior and protected_state() == protected and media_state(before) == media,
          'The live old database or protected files changed while the complete backup was prepared')
    check(load_candidate(args.candidate_manifest, args.manifest_sha256)[0] == candidate and
          preflight_paths(candidate, assets) == (old_sources, retained) and load_backup() == backup and
          preflight_recovery() == recovery_baseline() and preserve_diagnostic_history(diagnostic_before),
          'A candidate, target permission or complete backup proof changed before stop')
    stopped = installation_started = False
    try:
        stopped = True
        command(['/usr/bin/systemctl', 'stop', SERVICE], 'Stop the accepted old service after complete recovery rehearsal')
        check(command(['/usr/bin/systemctl', 'show', SERVICE, '-p', 'MainPID', '--value'], 'Stopped service observation').strip() == b'0',
              'The accepted old service did not stop')
        # A backup never authorizes discarding business writes accepted while
        # it was being prepared. Abort before any install if this differs.
        check(snapshot(database, BASE_SCHEMA) == before and protected_state() == protected and media_state(before) == media,
              'The old service accepted changes after its backup; restart it without restoring the older dump')
        installation_started = True
        private(INSTALLATION_STARTED, canonical({'owner': OWNER, 'candidate_sha256': candidate['binary']['sha256']}))
        sync_directory(BACKUP)
        provision_recovery_database(backup, database)
        install_recovery(backup)
        for relative in set(old_sources) - set(candidate['source']['files']):
            path = target(REPOSITORY, relative)
            check(sha(path) == old_sources[relative], 'An obsolete old source changed before its owned removal')
            path.unlink()
            sync_directory(path.parent)
        for relative, expected in candidate['source']['files'].items():
            path = target(REPOSITORY, relative)
            payload = (source / relative).read_bytes()
            check(hashlib.sha256(payload).hexdigest() == expected, 'A candidate source changed before installation')
            parent_for(REPOSITORY, path)
            replace_file(path, payload, 0o644)
        for relative, payload in assets.items():
            if relative != 'index.html':
                path = target(LIVE / 'admin', relative)
                if not path.exists():
                    parent_for(LIVE / 'admin', path)
                    private(path, payload)
                    path.chmod(0o644)
                    sync_directory(path.parent)
        replace_file(LIVE / 'admin/index.html', assets['index.html'], 0o644)
        payload = Path(candidate['binary']['path']).read_bytes()
        check(hashlib.sha256(payload).hexdigest() == candidate['binary']['sha256'], 'The candidate executable changed before installation')
        replace_file(LIVE / 'goby', payload, 0o755)
        command(['/usr/bin/systemctl', 'start', SERVICE], 'Start the accepted M5j candidate')
        ready()
        report = verify_installed_candidate(database, candidate, assets, backup, before, protected, media, retained, diagnostic_before,
                                            args.manifest_sha256, started)
        current = {name: report[name] for name in ('main_pid', 'uid', 'gid', 'start_ticks', 'binary_sha256')}
        publish_exclusive(EVIDENCE, canonical(report))
        print(json.dumps({'status': 'passed', 'main_pid': current['main_pid'], 'schema_version': TARGET_SCHEMA,
                          'binary_sha256': current['binary_sha256'], 'evidence': str(EVIDENCE)}))
    except BaseException:
        if stopped and not EVIDENCE.exists() and not EVIDENCE.is_symlink():
            try:
                if installation_started:
                    recovered = restore()
                    print(json.dumps({'status': 'failed', 'recovery': recovered['status'], 'recovery_evidence': str(RECOVERY_EVIDENCE)}))
                else:
                    # No candidate file or database change has started. Keep
                    # the latest old-service rows instead of restoring a dump.
                    check(sha(LIVE / 'goby') == OLD and protected_state() == protected, 'The untouched original service changed before its restart')
                    command(['/usr/bin/systemctl', 'start', SERVICE], 'Restart the unchanged old service after pre-install interruption')
                    ready()
                    process_identity(OLD)
                    print(json.dumps({'status': 'failed', 'recovery': 'original_service_restarted', 'database_restore_executed': False}))
            except BaseException as recovery_error:
                print(json.dumps({'status': 'failed', 'recovery': 'requires_operator', 'backup_directory': str(BACKUP),
                                  'reason': str(recovery_error) if isinstance(recovery_error, Failure) else 'A protected recovery operation did not complete'}))
        raise


if __name__ == '__main__':
    try:
        main()
    except SystemExit:
        raise
    except BaseException as error:
        print(json.dumps({'status': 'failed', 'type': type(error).__name__,
                          'message': str(error) if isinstance(error, Failure) else 'A protected deployment operation did not complete'}))
        sys.exit(1)
