#!/usr/bin/env python3
"""Prepare and deploy one reviewed schema25 candidate over the stopped M5j service.

This is a new operator, not a continuation of the historical M5i deployment.
Prepare creates a fresh operational backup and upgrades only its new rehearsal
database. Deploy accepts an exact prepared receipt, migrates the original main
database in place, installs the candidate, and performs a bounded native smoke.
There is no main-database restore, recovery-store removal, automatic retry, or
old-binary rollback path. Failed work and all preexisting data remain retained.
"""

from __future__ import annotations

import argparse
from contextlib import contextmanager
import datetime as dt
import fcntl
import hashlib
import http.client
from http.cookies import SimpleCookie
import json
import os
from pathlib import Path
import re
import secrets
import selectors
import shlex
import stat
import subprocess
import sys
import tarfile
import time
from urllib.parse import unquote, urlsplit

sys.dont_write_bytecode = True

WORK = Path('/opt/goby-test/exec-work-m3e')
ROOT = Path('/opt/goby-test/backups/client-schema25-v1')
MARKER = 'goby-client-schema25-deployment-v1'
SOURCE_MARKER = 'goby-client-backup-source-m3e-v1'
SOURCE_MANIFEST = 'backup-source-inputs.json'
TOOL_MANIFEST = 'deployment-tool-inputs.json'
TOOL_FILES = {'scripts/test-env/deploy-client-schema25.py', 'scripts/test-env/test-deploy-client-schema25.py',
              'scripts/test-env/migrate-client-schema25.go'}
SERVICE = 'goby-foundation-test.service'
LIVE = Path('/opt/goby-dev')
RUNTIME = Path('/opt/goby-test/runtime.env')
RECOVERY_ENV = Path('/opt/goby-test/recovery-m5j.env')
BROWSER = Path('/opt/goby-test/browser.env')
MASTER = Path('/var/lib/goby-test/application-key-vault/master.key')
STORES = tuple(Path('/var/lib/goby-test') / name for name in
               ('recovery-m5j', 'backups-m5j', 'recovery-operations-m5j'))
PAIRING = Path('/var/lib/goby-test/operator-secrets-m5j/attempt-01')
OLD_BACKUP = Path('/opt/goby-test/backups/m5j-20260910')
UNIT = Path('/etc/systemd/system') / SERVICE
DROPINS = tuple(UNIT.with_name(SERVICE + '.d') / name for name in
               ('20-application-keys.conf', '30-observability.conf', '40-backup-recovery.conf'))
UNIT_SHA = '91a9b0e55baf053db25281236d0d7c8c055f895b093d4474d9c27e888610117d'
DROPIN_SHAS = ('ed9e8b91416918ce797ab3b5507d8843f62109d033cb30a679a44e3abbea3dd9',
               'cc8fecb588840dd49848352936747a712d8790f8f7bd25d8d31a8a41069a46e5',
               'e62ad56f0e93c1df2b89a6b5f7dbbcb032fa98d9d4cdcb8ebb04ba1e01b5682e')
DIAGNOSTICS = Path('/var/log/goby-test')
PG = Path('/usr/lib/postgresql/17/bin')
HBA = Path('/etc/postgresql/17/main/pg_hba.conf')
OLD_BINARY_SHA = 'a4abba7b289ceb74ca0916484d714dc6baa1b3c5230b06964b89d363a64e8b81'
OLD_ARCHIVE = {'id': '1f0070537a1288408a9cb116631a2711', 'bytes': 177366,
               'sha256': 'd6dddf3fbb7e437409a5a09fbbca9a853b35673ca19e8910b66e49c32cb78f86'}
SYSTEM_IDENTIFIER = '7683277964552005578'
DEPLOYMENT_ID = 'f58d5e0c8ff49fca916499e666bffd9f'
RECOVERY_COMMENT = 'goby-m5j-deployment-backup-v1:recovery:c14dded5b29a108a9b220c0787fc6528'
MAIN = {'database': 'goby_test', 'database_oid': 16385, 'role': 'goby_test', 'role_oid': 16384}
RECOVERY = {'database': 'goby_recovery_m5j', 'database_oid': 994944,
            'role': 'goby_recovery_m5j', 'role_oid': 994943}
CATALOGS = {
    23: 'de85f4917dd7409e7e7bed20c7cbe63f0d7afe68f6ed72faff6b5bbb598ed00b',
    24: '6ba8a30d7648f3fdd73f977cc5d2aafac39232704542f28f20f0898d93ef575c',
    25: 'e269a7eb6b31d2eb3fff734896ca074a6113761f321f2f23f4b07441e7dc617b',
}
HASH = re.compile(r'[0-9a-f]{64}')
REHEARSAL = re.compile(r'goby_upgrade_m3e_[0-9a-f]{24}')
RUN_NAME = re.compile(r'run-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{24}')
ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8'}
MAX_FILE = 512 << 20
MAX_SOURCE = 1 << 30
MAX_DOCUMENT = 16 << 20
SESSION = '/admin/v1/session'
BACKUPS = '/admin/v1/backups'
NATIVE_READS = (SESSION, '/admin/v1/overview', '/admin/v1/capabilities',
                '/admin/v1/libraries', '/admin/v1/tasks', BACKUPS, BACKUPS + '/status')
EXPECTED_PACKAGES = {'github.com/moooyo/goby/cmd/goby'} | {
    'github.com/moooyo/goby/internal/' + name for name in
    'activity artwork backupformat backuppg backupstore config database diagnostics events identity library lifecycle media metadata playback recovery recoverycontrol recoverydb server settings subtitle tasks transcode'.split()}
FULL_CLEANUP = {'unit_terminal', 'hba_restored_exactly', 'goby_backup_m3e_source_removed',
                'goby_backup_m3e_target_removed', 'preexisting_catalog_unchanged', 'receipt_saved'}


class Failure(Exception):
    """A fixed operator condition failed; no secret diagnostic is public."""


def require(value, message):
    if not value:
        raise Failure(message)


def sha(value):
    return hashlib.sha256(value).hexdigest()


def canonical(value):
    return (json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False) + '\n').encode()


def unique_object(pairs):
    value = {}
    for key, entry in pairs:
        require(key not in value, 'A control document contains duplicate fields.')
        value[key] = entry
    return value


def decode(raw):
    try:
        return json.loads(raw, object_pairs_hook=unique_object)
    except (ValueError, UnicodeError):
        raise Failure('A control document is not valid JSON.') from None


def identity(info):
    return {'device': info.st_dev, 'inode': info.st_ino, 'uid': info.st_uid,
            'gid': info.st_gid, 'mode': stat.S_IMODE(info.st_mode), 'bytes': info.st_size}


def path_info(path):
    require(isinstance(path, Path) and path.is_absolute() and '..' not in path.parts,
            'An operator path is not absolute and contained.')
    for parent in reversed((path, *path.parents)):
        info = parent.lstat()
        require(not stat.S_ISLNK(info.st_mode), 'An operator path contains a symlink.')
    return info


def directory(path, uid=0, gid=0, mode=0o700):
    info = path_info(path)
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == uid and info.st_gid == gid and
            stat.S_IMODE(info.st_mode) == mode, 'A directory identity differs from its reviewed scope.')
    return identity(info)


def read_file(path, *, uid=0, gid=0, modes=(0o600,), limit=MAX_DOCUMENT):
    before = path_info(path)
    require(stat.S_ISREG(before.st_mode) and before.st_uid == uid and before.st_gid == gid and
            stat.S_IMODE(before.st_mode) in modes and before.st_nlink == 1 and before.st_size <= limit,
            'A file has unexpected ownership, links, permissions, or size.')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as handle:
        opened = os.fstat(handle.fileno())
        require(identity(opened) == identity(before), 'A file changed while opening it.')
        raw = handle.read(limit + 1)
        require(identity(os.fstat(handle.fileno())) == identity(opened) and
                len(raw) == before.st_size, 'A file changed while it was being read.')
    require(identity(path_info(path)) == identity(before), 'A file was replaced during its read.')
    return raw


def file_fact(path, *, uid=0, gid=0, modes=(0o600,), limit=MAX_FILE):
    raw = read_file(path, uid=uid, gid=gid, modes=modes, limit=limit)
    return {'identity': identity(path_info(path)), 'sha256': sha(raw)}


def write_exclusive(path, raw, mode=0o600):
    directory(path.parent)
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode)
    with os.fdopen(descriptor, 'wb') as handle:
        handle.write(raw)
        handle.flush()
        os.fsync(handle.fileno())
    sync_directory(path.parent)


def sync_directory(path):
    descriptor = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def safe_relative(value):
    require(isinstance(value, str) and value and not value.startswith('/') and '\\' not in value and
            all(part not in ('', '.', '..') for part in value.split('/')) and
            all(ord(c) >= 32 and ord(c) != 127 for c in value), 'An artifact member path is unsafe.')
    return value


def validate_arguments(args):
    require(args.mode in ('preflight', 'prepare', 'deploy'), 'The operator phase is invalid.')
    require(args.source.parent == WORK and re.fullmatch(r'source-attempt-(?:0[1-9]|[1-9][0-9]+)', args.source.name),
            'The source is not a frozen direct workspace member.')
    for name in ('manifest_sha256', 'candidate_sha256', 'assets_sha256', 'full_report_sha256', 'helper_sha256',
                 'tool_manifest_sha256', 'helper_build_report_sha256', 'guard_report_sha256'):
        require(HASH.fullmatch(getattr(args, name) or ''), 'An explicit artifact digest is missing or invalid.')
    for path in (args.source, args.candidate, args.assets, args.full_report, args.helper,
                 args.tool_source, args.helper_build_report, args.guard_report):
        require(path.is_absolute() and path.is_relative_to(WORK) and '..' not in path.parts,
                'An input artifact escaped the private workspace.')
    if args.mode == 'deploy':
        require(args.prepared is not None and args.prepared.name == 'prepared.json' and
                args.prepared.parent.parent == ROOT and RUN_NAME.fullmatch(args.prepared.parent.name) and
                HASH.fullmatch(args.prepared_sha256 or ''), 'Deploy requires one explicitly pinned prepared receipt.')
    else:
        require(args.prepared is None and args.prepared_sha256 is None,
                'A non-deployment phase supplied a prepared receipt.')


def verify_source(source, expected_digest):
    directory(WORK)
    source_identity = directory(source)
    raw = read_file(source / SOURCE_MANIFEST, modes=(0o600, 0o644))
    require(sha(raw) == expected_digest, 'The source manifest digest differs.')
    manifest = decode(raw)
    require(isinstance(manifest, dict) and set(manifest) == {'marker', 'files'} and
            manifest['marker'] == SOURCE_MARKER and isinstance(manifest['files'], dict),
            'The source manifest has an unexpected format.')
    actual, total = {}, 0
    for path in source.rglob('*'):
        info = path_info(path)
        require(info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                'A source member allows untrusted writes.')
        if stat.S_ISDIR(info.st_mode):
            continue
        relative = safe_relative(path.relative_to(source).as_posix())
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_size <= MAX_FILE,
                'A source member is not a bounded independent regular file.')
        total += info.st_size
        require(total <= MAX_SOURCE and len(actual) < 20000, 'The source inventory exceeds its bound.')
        if relative != SOURCE_MANIFEST:
            actual[relative] = sha(read_file(path, modes=(0o600, 0o644, 0o755), limit=MAX_FILE))
    require(len(actual) > 100 and actual == manifest['files'], 'The complete source bytes or membership differ.')
    for version, digest in CATALOGS.items():
        name = f'internal/backuppg/catalogs/schema-{version}-postgresql-17.json'
        require(actual.get(name) == digest, 'A published current or historical catalog differs.')
    catalog = decode(read_file(source / 'internal/backuppg/catalogs/schema-25-postgresql-17.json', modes=(0o600, 0o644)))
    migrations = {f"internal/database/migrations/{row['name']}": row['sha256'] for row in catalog['migrations']}
    require(catalog['version'] == 25 and len(migrations) == 25 and migrations ==
            {name: digest for name, digest in actual.items() if name.startswith('internal/database/migrations/')},
            'The full embedded migration prefix differs from the real schema25 catalog.')
    require({'go.mod', 'go.sum'} <= actual.keys() and not TOOL_FILES.intersection(actual),
            'The product source omits module inputs or contains separate deployment tools.')
    return {'identity': source_identity, 'manifest_sha256': expected_digest, 'files': actual}


def verify_tool_source(path, expected_digest, product_files, product_manifest_sha256):
    require(path.parent == WORK and re.fullmatch(r'tool-build-[a-z0-9][a-z0-9_-]{1,79}', path.name),
            'The tool source is not an independently contained build directory.')
    directory(path)
    raw = read_file(path / TOOL_MANIFEST, modes=(0o600, 0o644))
    require(sha(raw) == expected_digest, 'The independent tool manifest digest differs.')
    manifest = decode(raw)
    require(isinstance(manifest, dict) and set(manifest) == {'marker', 'product_source_manifest_sha256', 'files'} and
            manifest['marker'] == 'goby-client-schema25-tool-source-v1' and
            manifest['product_source_manifest_sha256'] == product_manifest_sha256,
            'The tool manifest belongs to a different product source.')
    actual, total = {}, 0
    for member in path.rglob('*'):
        info = path_info(member)
        require(info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                'A tool source member permits untrusted writes.')
        if stat.S_ISDIR(info.st_mode):
            continue
        name = safe_relative(member.relative_to(path).as_posix())
        total += info.st_size
        require(total <= MAX_SOURCE and len(actual) < 20000, 'The tool source exceeds its inventory bound.')
        if name != TOOL_MANIFEST:
            actual[name] = sha(read_file(member, modes=(0o600, 0o644, 0o755), limit=MAX_FILE))
    expected = {**product_files, SOURCE_MANIFEST: product_manifest_sha256}
    require(set(actual) == set(expected) | TOOL_FILES and actual == manifest['files'] and
            all(actual[name] == digest for name, digest in expected.items()),
            'The tool build changed product bytes or its complete reviewed membership.')
    require(actual['scripts/test-env/deploy-client-schema25.py'] == sha(Path(__file__).read_bytes()),
            'The executing operator differs from the reviewed tool source.')
    return {'path': str(path), 'manifest_sha256': expected_digest, 'files': actual}


def read_assets(path):
    raw = read_file(path, modes=(0o600, 0o644), limit=MAX_FILE)
    entries, total = {}, 0
    import io
    with tarfile.open(fileobj=io.BytesIO(raw), mode='r:gz') as archive:
        for member in archive.getmembers():
            name = member.name.removeprefix('./')
            if member.isdir():
                if name in ('', '.'):
                    continue
                safe_relative(name.rstrip('/'))
                continue
            safe_relative(name)
            require(member.isfile() and name not in entries and member.size <= 32 << 20,
                    'An asset archive contains a special, duplicate, or excessive member.')
            total += member.size
            require(len(entries) < 4096 and total <= 128 << 20, 'The asset archive exceeds its unpacked bound.')
            data = archive.extractfile(member).read(member.size + 1)
            require(len(data) == member.size, 'An asset archive member is truncated.')
            entries[name] = data
    require('index.html' in entries and len(entries) > 1, 'The candidate assets have no native application index.')
    require(not any('/'.join(name.split('/')[:index]) in entries for name in entries
                    for index in range(1, len(name.split('/')))), 'An asset file conflicts with another member directory.')
    return entries


def load_inputs(args):
    source = verify_source(args.source, args.manifest_sha256)
    tool_source = verify_tool_source(args.tool_source, args.tool_manifest_sha256, source['files'], args.manifest_sha256)
    guards_raw = read_file(args.guard_report)
    require(sha(guards_raw) == args.guard_report_sha256, 'The deployment guard report digest differs.')
    guards = decode(guards_raw)
    cases = guards.get('cases', [])
    require(guards.get('suite') == 'client-schema25-deployment-guards' and guards.get('status') == 'passed' and
            guards.get('failures') == guards.get('errors') == guards.get('skips') == 0 and
            guards.get('tests', 0) >= 40 and guards['tests'] == len(cases) == len(set(cases)) and
            guards.get('operator_sha256') == tool_source['files']['scripts/test-env/deploy-client-schema25.py'] and
            guards.get('guard_sha256') == tool_source['files']['scripts/test-env/test-deploy-client-schema25.py'] and
            guards.get('fixtures') == 'synthetic-memory-only' and guards.get('real_database_restore_acceptance') is False and
            guards.get('real_deployment_acceptance') is False,
            'The exact independent deployment tool guards have not passed.')
    candidate = file_fact(args.candidate, modes=(0o600, 0o755))
    helper = file_fact(args.helper, modes=(0o755,))
    assets = file_fact(args.assets, modes=(0o600, 0o644))
    require(candidate['sha256'] == args.candidate_sha256 and helper['sha256'] == args.helper_sha256 and
            assets['sha256'] == args.assets_sha256, 'A candidate artifact digest differs.')
    build_raw = read_file(args.helper_build_report)
    require(sha(build_raw) == args.helper_build_report_sha256, 'The independent helper build report digest differs.')
    build = decode(build_raw)
    require(build.get('schema') == 'goby-client-schema25-helper-build' and build.get('version') == 1 and
            build.get('status') == 'passed' and build.get('product_source_manifest_sha256') == args.manifest_sha256 and
            build.get('tool_manifest_sha256') == args.tool_manifest_sha256 and build.get('helper_sha256') == args.helper_sha256 and
            build.get('go_version') == 'go1.27.1' and build.get('goos') == 'linux' and build.get('goarch') == 'amd64' and
            build.get('cgo_enabled') is False, 'The helper build is not bound to the reviewed product and tool inputs.')
    raw = read_file(args.full_report)
    require(sha(raw) == args.full_report_sha256, 'The complete regression report digest differs.')
    report = decode(raw)
    require(report.get('status') == 'passed' and report.get('mode') == 'full' and report.get('schema') == 25 and
            report.get('source') == str(args.source) and report.get('source_manifest_sha256') == args.manifest_sha256 and
            report.get('binary', {}).get('sha256') == args.candidate_sha256 and report.get('unit_exit') == 0 and
            report.get('tests', {}).get('failures') == 0 and report.get('tests', {}).get('skips') == 0 and
            report.get('tests', {}).get('top_level_passes', 0) > 1600 and
            len(report.get('packages', [])) == 24 and set(report.get('packages', [])) == EXPECTED_PACKAGES and
            set(report.get('cleanup', {})) == FULL_CLEANUP and all(value is True for value in report['cleanup'].values()),
            'The final complete regression does not authorize this exact candidate and source.')
    asset_members = read_assets(args.assets)
    return {'source': str(args.source), 'source_identity': source['identity'],
            'source_manifest_sha256': args.manifest_sha256, 'source_files': source['files'], 'tool_source': tool_source,
            'guard_report': {'path': str(args.guard_report), 'sha256': args.guard_report_sha256},
            'helper_build_report': {'path': str(args.helper_build_report), 'sha256': args.helper_build_report_sha256},
            'candidate': {'path': str(args.candidate), **candidate},
            'helper': {'path': str(args.helper), **helper}, 'assets': {'path': str(args.assets), **assets},
            'asset_members': {name: sha(raw) for name, raw in asset_members.items()},
            'full_report': {'path': str(args.full_report), 'sha256': args.full_report_sha256}}


def command(arguments, *, input_bytes=None, environment=None, timeout=30, run=None, label='command'):
    require(re.fullmatch(r'[a-z0-9-]{1,48}', label), 'A command label is invalid.')
    try:
        result = subprocess.run([str(value) for value in arguments], input=input_bytes,
                                env=environment or ENV, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=timeout, check=False)
    except (OSError, subprocess.TimeoutExpired):
        raise Failure('A bounded command did not complete; no automatic retry is permitted.') from None
    require(len(result.stdout) <= 64 << 20 and len(result.stderr) <= 8 << 20,
            'A command diagnostic exceeded its private output bound.')
    if run is not None:
        prefix = label + '-' + secrets.token_hex(6)
        write_exclusive(run / (prefix + '.stdout'), result.stdout)
        write_exclusive(run / (prefix + '.stderr'), result.stderr)
    require(result.returncode == 0, 'A controlled command failed; its private evidence is retained.')
    return result.stdout


def systemd_properties(raw, names, repeated=()):
    values = {}
    for line in raw.decode().splitlines():
        key, separator, value = line.partition('=')
        require(separator and key in names and (key in repeated or key not in values),
                'A service property is unknown, duplicated, or malformed.')
        if key in repeated:
            values.setdefault(key, []).append(value)
        else:
            values[key] = value
    require(set(values) == set(names), 'A required service property is missing.')
    return values


def service_state(active=False, expected_binary=OLD_BINARY_SHA):
    names = ('MainPID', 'ActiveState', 'SubState', 'User', 'Group', 'FragmentPath', 'DropInPaths',
             'WorkingDirectory', 'EnvironmentFiles', 'ProtectSystem', 'ReadWritePaths', 'UMask', 'NoNewPrivileges')
    state = systemd_properties(command(['/usr/bin/systemctl', 'show', SERVICE, *('-p' + x for x in names)]),
                               names, ('EnvironmentFiles',))
    require(state['User'] == state['Group'] == 'goby' and state['FragmentPath'] == str(UNIT) and
            state['WorkingDirectory'] == '/var/lib/goby-test' and state['ProtectSystem'] == 'strict' and
            state['UMask'] == '0077' and state['NoNewPrivileges'] == 'yes' and
            state['EnvironmentFiles'] == [str(RUNTIME) + ' (ignore_errors=no)', str(RECOVERY_ENV) + ' (ignore_errors=no)'] and
            set(state['DropInPaths'].split()) == {str(p) for p in DROPINS}, 'The effective main service policy differs.')
    require(file_fact(LIVE / 'goby', modes=(0o755,))['sha256'] == expected_binary,
            'The installed main binary differs.')
    if active:
        require(state['ActiveState'] == 'active' and state['SubState'] == 'running' and int(state['MainPID']) > 1,
                'The candidate service is not running.')
        process = Path('/proc') / state['MainPID']
        status = (process / 'status').read_text().splitlines()
        for field, expected in (('Uid:', 995), ('Gid:', 986)):
            require(next(line.split()[1:] for line in status if line.startswith(field)) == [str(expected)] * 4,
                    'The candidate process has an unexpected effective identity.')
        with (process / 'exe').open('rb') as handle:
            require(sha(handle.read(MAX_FILE)) == expected_binary, 'The running executable differs from the accepted candidate.')
        state['start_ticks'] = (process / 'stat').read_text().rsplit(') ', 1)[1].split()[19]
    else:
        require(state['MainPID'] == '0' and state['ActiveState'] == 'inactive' and state['SubState'] == 'dead',
                'The main service must already be stopped; this operator does not stop an unknown process.')
    return state


def private_values(path, wanted):
    values = {}
    for line in read_file(path, limit=65536).decode().splitlines():
        key, separator, raw = line.strip().removeprefix('export ').partition('=')
        if separator and key in wanted:
            fields = shlex.split(raw, comments=False, posix=True)
            require(key not in values and len(fields) == 1 and fields[0], 'A private assignment is missing or ambiguous.')
            values[key] = fields[0]
    require(set(values) == set(wanted), 'A required private assignment is missing.')
    return values


def runtime_policy():
    wanted = {'GOBY_DATABASE_URL', 'GOBY_LISTEN', 'GOBY_PUBLIC_URL', 'GOBY_WEB_DIR', 'GOBY_API_KEY_MASTER_KEY_FILE'}
    values = private_values(RUNTIME, wanted)
    require(values['GOBY_LISTEN'] == '127.0.0.1:18096' and values['GOBY_PUBLIC_URL'] == 'http://127.0.0.1:18096' and
            values['GOBY_WEB_DIR'] == str(LIVE / 'admin') and values['GOBY_API_KEY_MASTER_KEY_FILE'] == str(MASTER),
            'The actual runtime endpoint, assets, or matching-master policy differs.')
    for line in read_file(RUNTIME, limit=65536).decode().splitlines():
        key, separator, _ = line.strip().removeprefix('export ').partition('=')
        require(not separator or not (key.startswith(('GOBY_RECOVERY_', 'GOBY_BACKUP_')) or key in ('GOBY_PG_DUMP', 'GOBY_PG_RESTORE')),
                'A runtime override conflicts with the fixed recovery drop-in.')
    names = []
    for line in read_file(RECOVERY_ENV, limit=65536).decode().splitlines():
        key, separator, _ = line.strip().removeprefix('export ').partition('=')
        if separator:
            names.append(key)
    require(names == ['GOBY_RECOVERY_DATABASE_URL'], 'The recovery environment contains an unreviewed policy override.')
    recovery_url = private_values(RECOVERY_ENV, {'GOBY_RECOVERY_DATABASE_URL'})['GOBY_RECOVERY_DATABASE_URL']
    database_environment(recovery_url, RECOVERY['database'], RECOVERY['role'])
    return values['GOBY_DATABASE_URL']


def database_environment(url, name, role, *, readonly=True):
    parsed = urlsplit(url)
    require(parsed.scheme in ('postgres', 'postgresql') and parsed.hostname == '127.0.0.1' and
            parsed.port in (None, 5432) and unquote(parsed.path) == '/' + name and
            unquote(parsed.username or '') == role and parsed.password and not parsed.fragment and
            parsed.query in ('', 'sslmode=disable'), 'A database URL escaped the fixed primary cluster scope.')
    return {**ENV, 'PGHOST': '127.0.0.1', 'PGPORT': '5432', 'PGDATABASE': name, 'PGUSER': role,
            'PGPASSWORD': unquote(parsed.password), 'PGPASSFILE': '/dev/null', 'PGSSLMODE': 'disable',
            'PGCONNECT_TIMEOUT': '5', 'PGCLIENTENCODING': 'UTF8',
            'PGOPTIONS': '-c default_transaction_read_only=' + ('on' if readonly else 'off') +
                         ' -c statement_timeout=120000 -c lock_timeout=3000 -c timezone=UTC -c bytea_output=hex -c DateStyle=ISO,YMD'}


def pg(query, *, database='postgres', environment=None, run=None, label='postgres-read'):
    require(database in ('postgres', MAIN['database'], RECOVERY['database']) or REHEARSAL.fullmatch(database),
            'A PostgreSQL command selected an unrelated database.')
    arguments = [PG / 'psql', '-X', '--no-password', '-qAt', '-v', 'ON_ERROR_STOP=1', '-d', database]
    if environment is None:
        arguments = ['/usr/sbin/runuser', '-u', 'postgres', '--', *arguments, '-p', '5432']
    return command(arguments, input_bytes=query.encode(), environment=environment, run=run, label=label, timeout=130)


def cluster_state(allow_snapshot=False):
    query = """BEGIN READ ONLY; SELECT json_build_object('system_identifier',(SELECT system_identifier::text FROM pg_control_system()),
    'port',current_setting('port')::int,'version',current_setting('server_version_num')::int,
    'postmaster_start',pg_postmaster_start_time(),'data_directory',current_setting('data_directory'),
    'databases',(SELECT json_agg(json_build_object('database',d.datname,'database_oid',d.oid::bigint,'role',r.rolname,
    'role_oid',r.oid::bigint,'comment',shobj_description(d.oid,'pg_database'),'role_comment',shobj_description(r.oid,'pg_authid'),
    'sessions',(SELECT count(*) FROM pg_stat_activity a WHERE a.datid=d.oid),'safe',NOT(r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication OR r.rolbypassrls),
    'memberships',(SELECT count(*) FROM pg_auth_members m WHERE m.member=r.oid OR m.roleid=r.oid)))
    FROM pg_database d JOIN pg_roles r ON r.oid=d.datdba WHERE d.datname IN ('goby_test','goby_recovery_m5j'))); ROLLBACK;"""
    state = decode(pg(query))
    require(state['system_identifier'] == SYSTEM_IDENTIFIER and state['port'] == 5432 and
            state['version'] == 170011 and state['data_directory'] == '/var/lib/postgresql/17/main',
            'The primary PostgreSQL cluster identity differs.')
    for expected in (MAIN, RECOVERY):
        values = [x for x in state['databases'] if x['database'] == expected['database']]
        require(len(values) == 1 and all(values[0].get(k) == v for k, v in expected.items()) and
                values[0]['safe'] and values[0]['memberships'] == 0 and
                values[0]['sessions'] == (1 if allow_snapshot and expected == MAIN else 0),
                'A protected database, ordinary role, or quiescent state differs.')
        if expected == RECOVERY:
            require(values[0]['comment'] == values[0]['role_comment'] == RECOVERY_COMMENT,
                    'The preexisting recovery ownership marker differs.')
    return state


def tree_state(root, uid, gid, *, directory_mode=0o700):
    result = {'root': directory(root, uid, gid, directory_mode), 'files': {}}
    total = 0
    for path in sorted(root.rglob('*')):
        info = path_info(path)
        require(info.st_uid == uid and info.st_gid == gid and not info.st_mode & 0o022,
                'A protected tree member has an unexpected owner or permissions.')
        if stat.S_ISDIR(info.st_mode):
            continue
        total += info.st_size
        require(len(result['files']) < 10000 and total <= 1 << 30, 'A protected tree exceeds its reviewed bound.')
        result['files'][safe_relative(path.relative_to(root).as_posix())] = file_fact(
            path, uid=uid, gid=gid, modes=(0o600, 0o644, 0o755))
    return result


def store_state():
    trees = {str(path): tree_state(path, 995, 986) for path in STORES}
    require(all(stat.S_ISREG(member.lstat().st_mode) for path in STORES for member in path.iterdir()),
            'An initial recovery store contains an unregistered directory or special member.')
    marker = decode(read_file(STORES[0] / '.goby-lifecycle.json', uid=995, gid=986))
    registry = decode(read_file(STORES[0] / 'generation-registry.json', uid=995, gid=986))
    require(marker.get('deploymentId') == registry.get('deploymentId') == DEPLOYMENT_ID and
            registry.get('generations') == [] and registry.get('baselineDigest') == sha(b''),
            'The deployment no longer selects its original primary and master.')
    require(set(trees[str(STORES[0])]['files']) ==
            {'.goby-lifecycle.json', '.goby-lifecycle.lock', 'generation-registry.json'},
            'A pending or activated generation requires separate review.')
    control = decode(read_file(STORES[2] / 'current.json', uid=995, gid=986))
    require(set(trees[str(STORES[2])]['files']) ==
            {'.goby-recovery-control.json', '.goby-recovery-control.lock', 'current.json', 'cas-proof.json'},
            'The recovery control store contains an unregistered member.')
    payload = control.get('payload', {})
    require(control.get('deploymentId') == payload.get('deploymentId') == DEPLOYMENT_ID and
            payload.get('transition') is None and
            all(x.get('state') in ('completed', 'failed', 'cancelled', 'interrupted') and
                x.get('kind') not in ('delete', 'apply', 'rollback') for x in payload.get('operations', [])),
            'An unfinished recovery operation or deletion requires separate review.')
    slots = {x.get('slot'): x for x in payload.get('slots', [])}
    require(set(slots) == {'primary', 'recovery'} and slots['primary'].get('state') == 'active' and
            slots['recovery'].get('state') == 'unclaimed' and
            not any(x.get('retained') for x in slots.values()), 'The retained database slots differ.')
    catalog = decode(read_file(STORES[1] / '.goby-backup-catalog.json', uid=995, gid=986))
    backups = []
    for record in catalog.get('entries', []):
        item = record.get('metadata', {})
        require(record.get('deleting', False) is False and item.get('state') == 'ready' and item.get('verified') is True and
                re.fullmatch(r'[0-9a-f]{32}', item.get('id', '')) and HASH.fullmatch(item.get('digest', '')),
                'An existing archive is unfinished, deleting, or unverified.')
        name = 'object-' + item['id'] + '.age'
        fact = trees[str(STORES[1])]['files'].get(name)
        require(fact and fact['sha256'] == item['digest'] and fact['identity']['bytes'] == item['size'],
                'An existing ciphertext differs from its retained catalog.')
        backups.append({'id': item['id'], 'bytes': item['size'], 'sha256': item['digest']})
    require(backups, 'The retained native archive is missing.')
    require(OLD_ARCHIVE in backups and set(trees[str(STORES[1])]['files']) ==
            {'.goby-backup-store.json', '.goby-backup-store.lock', '.goby-backup-catalog.json'} |
            {'object-' + item['id'] + '.age' for item in backups},
            'The accepted M5j archive is missing or its strict store has an unregistered member.')
    pairing = tree_state(PAIRING, 995, 986)
    pairing_record = decode(read_file(PAIRING / 'backup.json', uid=995, gid=986))
    phrase_name = pairing_record.get('passphrase_file', '')
    require(pairing_record.get('backup_id') == OLD_ARCHIVE['id'] and
            re.fullmatch(r'[0-9a-f]{32}\.passphrase', phrase_name) and phrase_name in pairing['files'] and
            0 < pairing['files'][phrase_name]['identity']['bytes'] <= 1024 and
            pairing['files'].get(OLD_ARCHIVE['id'] + '.age', {}).get('sha256') == OLD_ARCHIVE['sha256'],
            'The accepted archive no longer has its retained private secret pairing.')
    return {'trees': trees, 'backups': backups, 'control_revision': control.get('revision'),
            'pairing': pairing, 'old_operational_backup': tree_state(OLD_BACKUP, 0, 0)}


def quiescent(environment):
    raw = pg("""BEGIN READ ONLY; SELECT json_build_object(
    'runnable_triggers',(SELECT count(*) FROM task_triggers t JOIN task_definitions d ON d.id=t.task_id
      WHERE d.enabled AND t.retired_at IS NULL AND t.calculation_error=''),
    'active_runs',(SELECT count(*) FROM task_runs WHERE state IN ('pending','running','stopping')),
    'active_children',(SELECT count(*) FROM task_run_children WHERE state IN ('waiting','queued','running')),
    'active_scans',(SELECT count(*) FROM scan_jobs WHERE status IN ('Queued','Running')),
    'active_encodings',(SELECT count(*) FROM encoding_jobs WHERE state NOT IN ('completed','failed','cancelled','interrupted')),
    'minimum_retention_eligible',(SELECT count(*) FROM activity_entries WHERE created_at<now()-interval '1 day'));
    ROLLBACK;""", database=MAIN['database'], environment=environment)
    values = decode(raw)
    require(all(value == 0 for value in values.values()),
            'A schedule, unfinished work, or minimum-retention boundary requires a separate startup plan.')
    return values


def protected_state(environment):
    service = service_state()
    runtime_policy()
    require(file_fact(UNIT)['sha256'] == UNIT_SHA and
            all(file_fact(path, modes=(0o644,))['sha256'] == digest for path, digest in zip(DROPINS, DROPIN_SHAS)),
            'An accepted main unit or drop-in changed its fixed bytes.')
    expected_writes = {'/dev/shm/goby-transcodes-test', '/var/lib/goby-test/application-key-vault', *(str(p) for p in STORES)}
    require(set(service['ReadWritePaths'].split()) == expected_writes, 'The service write sandbox differs.')
    inactive = decode(pg("""BEGIN READ ONLY; SELECT json_build_object('objects',
    (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname !~ '^pg_' AND n.nspname<>'information_schema')+
    (SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname !~ '^pg_' AND n.nspname<>'information_schema')+
    (SELECT count(*) FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname !~ '^pg_' AND n.nspname<>'information_schema'),
    'extensions',(SELECT count(*) FROM pg_extension WHERE extname<>'plpgsql'));
    ROLLBACK;""", database=RECOVERY['database']))
    require(inactive == {'objects': 0, 'extensions': 0}, 'The preexisting inactive recovery database is not empty.')
    return {'service': service, 'cluster': cluster_state(), 'stores': store_state(),
            'runtime': file_fact(RUNTIME), 'recovery_environment': file_fact(RECOVERY_ENV),
            'master': file_fact(MASTER, uid=995, gid=995, limit=32),
            'unit': file_fact(UNIT), 'dropins': {str(p): file_fact(p, modes=(0o644,)) for p in DROPINS},
            'hba': file_fact(HBA, uid=postgres_identity()[0], gid=postgres_identity()[1], modes=(0o640, 0o600, 0o644)),
            'diagnostics': tree_state(DIAGNOSTICS, 995, 986),
            'assets': tree_state(LIVE / 'admin', 0, 0, directory_mode=0o755),
            'quiescent': quiescent(environment)}


def postgres_identity():
    import pwd
    account = pwd.getpwnam('postgres')
    return account.pw_uid, account.pw_gid


def postgres_start_microseconds(value):
    require(isinstance(value, str) and len(value) <= 64, 'The SQL startup timestamp is invalid.')
    try:
        started = dt.datetime.fromisoformat(value)
        require(started.tzinfo is not None, 'The SQL startup timestamp has no timezone.')
        delta = started.astimezone(dt.timezone.utc) - dt.datetime(1970, 1, 1, tzinfo=dt.timezone.utc)
    except (ValueError, OverflowError):
        raise Failure('The SQL startup timestamp is invalid.') from None
    return (delta.days * 86400 + delta.seconds) * 1000000 + delta.microseconds


def read_primary_process():
    pid_lines = Path('/var/lib/postgresql/17/main/postmaster.pid').read_text().splitlines()
    require(len(pid_lines) >= 4 and re.fullmatch(r'[1-9][0-9]{0,9}', pid_lines[0]) and
            int(pid_lines[0]) > 1 and pid_lines[1] == '/var/lib/postgresql/17/main' and
            re.fullmatch(r'[1-9][0-9]{0,19}', pid_lines[2]) and pid_lines[3] == '5432',
            'The primary PID file has an unexpected process, path, or port.')
    pid = int(pid_lines[0])
    process = Path('/proc') / str(pid)
    ticks = process.joinpath('stat').read_text().rsplit(') ', 1)[1].split()[19]
    boot = Path('/proc/sys/kernel/random/boot_id').read_text().strip()
    require(re.fullmatch(r'[0-9]+', ticks) and
            re.fullmatch(r'[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}', boot),
            'The primary process or boot identity is invalid.')
    return {'pid': pid, 'start_ticks': ticks, 'boot_id': boot, 'pid_file': pid_lines[:4]}


def cluster_proof(run, inputs, target, label, allow_snapshot=False):
    stable = cluster_state(allow_snapshot)
    process = read_primary_process()
    value = decode(pg("""BEGIN READ ONLY; SELECT json_build_object('system_identifier',(SELECT system_identifier::text FROM pg_control_system()),
      'postmaster_start_microseconds',(extract(epoch FROM pg_postmaster_start_time())*1000000)::bigint);
      ROLLBACK;"""))
    require(value['system_identifier'] == SYSTEM_IDENTIFIER and
            type(value['postmaster_start_microseconds']) is int and
            value['postmaster_start_microseconds'] == postgres_start_microseconds(stable['postmaster_start']),
            'The SQL cluster or exact startup timestamp changed during observation.')
    # PID-file start seconds and SQL startup microseconds are independent
    # observations. Keep each exact across its own reread; never compare their
    # rounded values or add a time tolerance. The helper joins its live backend
    # PPID to this process and rechecks the SQL microseconds before any DDL.
    require(read_primary_process() == process, 'The primary OS process identity changed during observation.')
    require(cluster_state(allow_snapshot) == stable, 'The cluster changed while its proof was prepared.')
    proof = {'schema': 'goby-client-schema25-cluster-proof', 'version': 1,
             'run_id': run.name, 'source_manifest_sha256': inputs['source_manifest_sha256'],
             'tool_manifest_sha256': inputs['tool_source']['manifest_sha256'],
             'candidate_sha256': inputs['candidate']['sha256'], 'helper_sha256': inputs['helper']['sha256'],
             **{key: target[key] for key in MAIN}, 'system_identifier': SYSTEM_IDENTIFIER, 'postmaster_pid': process['pid'],
             'postmaster_start_ticks': process['start_ticks'], 'boot_id': process['boot_id'],
             'postmaster_start_microseconds': value['postmaster_start_microseconds'], 'port': 5432,
             'server_version': 170011, 'data_directory': '/var/lib/postgresql/17/main'}
    path = run / (label + '-cluster.json')
    write_exclusive(path, canonical(proof))
    return path, sha(canonical(proof))


@contextmanager
def operator_lock(create):
    directory(WORK)
    lock = WORK / 'main-deployment-schema25.lock'
    if not lock.exists():
        require(create, 'The deployment lock has not been prepared.')
        write_exclusive(lock, (MARKER + '\n').encode())
    require(read_file(lock) == (MARKER + '\n').encode(), 'The deployment lock owner differs.')
    descriptor = os.open(lock, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        os.close(descriptor)
        raise Failure('Another deployment operator owns the exclusive lock.') from None
    try:
        yield
    finally:
        fcntl.flock(descriptor, fcntl.LOCK_UN)
        os.close(descriptor)


def create_run(inputs):
    directory(ROOT.parent)
    if not ROOT.exists():
        ROOT.mkdir(mode=0o700)
        write_exclusive(ROOT / 'OWNER.json', canonical({'marker': MARKER, 'version': 1, 'path': str(ROOT)}))
    directory(ROOT)
    require(decode(read_file(ROOT / 'OWNER.json')) == {'marker': MARKER, 'version': 1, 'path': str(ROOT)},
            'The operational backup root has an unknown owner.')
    for previous in ROOT.iterdir():
        if previous.name == 'OWNER.json':
            continue
        require(previous.is_dir() and RUN_NAME.fullmatch(previous.name) and (previous / 'terminal.json').is_file(),
                'An unfinished prior deployment must be reviewed before a new run.')
    name = 'run-' + dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '-' + secrets.token_hex(12)
    run = ROOT / name
    run.mkdir(mode=0o700)
    write_exclusive(run / 'inputs.json', canonical(inputs))
    write_exclusive(run / 'OWNER.json', canonical({'marker': MARKER, 'run_id': name, 'version': 1}))
    sync_directory(ROOT)
    return run


def validate_phase(run, inputs, required):
    require(run.parent == ROOT and RUN_NAME.fullmatch(run.name), 'The run directory escaped its owned root.')
    directory(run)
    require(decode(read_file(run / 'OWNER.json')) == {'marker': MARKER, 'run_id': run.name, 'version': 1} and
            decode(read_file(run / 'inputs.json')) == inputs, 'The run belongs to different inputs or ownership.')
    require(not (run / 'terminal.json').exists(), 'A terminal run cannot be resumed automatically.')
    if required:
        require((run / (required + '.json')).is_file(), 'A prerequisite deployment phase is missing.')


@contextmanager
def exported_snapshot(environment):
    process = subprocess.Popen([str(PG / 'psql'), '-X', '--no-password', '-qAt', '-v', 'ON_ERROR_STOP=1'],
                               stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=environment)
    try:
        process.stdin.write(b'BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SELECT pg_export_snapshot();\n')
        process.stdin.flush()
        data = bytearray()
        deadline = time.monotonic() + 10
        with selectors.DefaultSelector() as selector:
            selector.register(process.stdout, selectors.EVENT_READ)
            while b'\n' not in data:
                require(time.monotonic() < deadline, 'The consistent snapshot export timed out.')
                if selector.select(0.2):
                    block = os.read(process.stdout.fileno(), 256)
                    require(block and len(data) + len(block) <= 256, 'The consistent snapshot export failed.')
                    data.extend(block)
        snapshot = data.decode().strip()
        require(re.fullmatch(r'[0-9A-Fa-f]+-[0-9A-Fa-f]+-[0-9]+', snapshot), 'The exported snapshot identifier is invalid.')
        yield snapshot
    finally:
        if process.poll() is None:
            try:
                process.communicate(input=b'ROLLBACK;\n\\q\n', timeout=5)
            except (OSError, subprocess.TimeoutExpired):
                process.kill()
                process.wait(timeout=5)
        for stream in (process.stdin, process.stdout, process.stderr):
            stream.close()


def invoke_helper(run, inputs, target, url, mode, label, *, snapshot=None, baseline=None, expected_schema=23):
    if mode == 'inspect' and target == MAIN and expected_schema == 23 and snapshot is None:
        with exported_snapshot(database_environment(url, MAIN['database'], MAIN['role'])) as exported:
            return invoke_helper(run, inputs, target, url, mode, label, snapshot=exported,
                                 baseline=baseline, expected_schema=expected_schema)
    proof, proof_sha = cluster_proof(run, inputs, target, label, snapshot is not None)
    output = run / (label + '.json')
    args = [inputs['helper']['path'], mode, '--output', output, '--expected-os-uid', '0',
            '--expected-database', target['database'], '--expected-database-oid', str(target['database_oid']),
            '--expected-role', target['role'], '--expected-role-oid', str(target['role_oid']),
            '--expected-system-identifier', SYSTEM_IDENTIFIER, '--expected-port', '5432',
            '--expected-deployment-id', DEPLOYMENT_ID, '--cluster-proof', proof, '--cluster-proof-sha256', proof_sha,
            '--run-id', run.name, '--source-manifest-sha256', inputs['source_manifest_sha256'],
            '--tool-manifest-sha256', inputs['tool_source']['manifest_sha256'],
            '--candidate-sha256', inputs['candidate']['sha256'], '--expected-helper-sha256', inputs['helper']['sha256']]
    if target != MAIN:
        args += ['--rehearsal', '--expected-owner-comment', target['owner_comment']]
    if mode == 'inspect':
        args += ['--expected-schema', str(expected_schema)]
    if snapshot:
        args += ['--snapshot-id', snapshot]
    if baseline:
        args += ['--baseline', baseline, '--baseline-sha256', sha(read_file(baseline))]
    command(args, environment={**ENV, 'GOBY_DATABASE_URL': url, 'GOMEMLIMIT': '128MiB'},
            timeout=240, run=run, label=label)
    result = decode(read_file(output))
    require(result.get('schema') == 'goby-client-schema25-migration' and result.get('version') == 1 and
            result.get('mode') == mode and result.get('status') == ('inspected' if mode == 'inspect' else 'committed') and
            HASH.fullmatch(result.get('state_sha256', '')) and result.get('run_id') == run.name and
            result.get('source_manifest_sha256') == inputs['source_manifest_sha256'] and
            result.get('tool_manifest_sha256') == inputs['tool_source']['manifest_sha256'] and
            result.get('candidate_sha256') == inputs['candidate']['sha256'] and
            result.get('helper_sha256') == inputs['helper']['sha256'] and result.get('cluster_proof_sha256') == proof_sha and
            result.get('target') == {key: target[key] for key in MAIN},
            'The offline helper did not publish a completion proof bound to this exact invocation.')
    if baseline:
        require(result.get('input_baseline_sha256') == sha(read_file(baseline)) and
                HASH.fullmatch(result.get('before_state_sha256', '')) and
                HASH.fullmatch(result.get('preserved_state_sha256', '')) and
                result.get('before_state_sha256') == result.get('preserved_state_sha256'),
                'The migration helper did not preserve its entire old state before committing.')
    cluster_state(snapshot is not None)
    return result


def ensure_material_directory(root, target):
    directory(root)
    require(target.is_relative_to(root) and '..' not in target.parts,
            'A material directory escaped its new private backup root.')
    current = root
    for component in target.relative_to(root).parts:
        current = current / component
        try:
            current.lstat()
        except FileNotFoundError:
            # parents=True applies mode only to the leaf. Creating each level
            # explicitly keeps intermediates private even with a 0022 umask.
            current.mkdir(mode=0o700)
            sync_directory(current.parent)
        # Never repair or adopt an existing permissive directory, symlink, or
        # different owner, including artifacts from a prior failed attempt.
        directory(current)


def save_materials(run, before):
    materials = run / 'materials'
    materials.mkdir(mode=0o700)
    pairs = [(LIVE / 'goby', 'goby-m5j', 0, 0, (0o755,)), (RUNTIME, 'runtime.env', 0, 0, (0o600,)),
             (RECOVERY_ENV, 'recovery.env', 0, 0, (0o600,)), (BROWSER, 'browser.env', 0, 0, (0o600,)),
             (MASTER, 'master.key', 995, 995, (0o600,)), (UNIT, 'unit.service', 0, 0, (0o600,))]
    pairs += [(p, p.name, 0, 0, (0o644,)) for p in DROPINS]
    for prefix, root, uid, gid, inventory in [
        ('admin', LIVE / 'admin', 0, 0, before['assets']['files']),
        *[(p.name, p, 995, 986, before['stores']['trees'][str(p)]['files']) for p in STORES],
        ('diagnostics', DIAGNOSTICS, 995, 986, before['diagnostics']['files']),
        ('native-pairing', PAIRING, 995, 986, before['stores']['pairing']['files'])]:
        for relative in inventory:
            pairs.append((root / relative, prefix + '/' + relative, uid, gid, (0o600, 0o644, 0o755)))
    files = {}
    for source, relative, uid, gid, modes in pairs:
        target = materials / safe_relative(relative)
        ensure_material_directory(materials, target.parent)
        raw = read_file(source, uid=uid, gid=gid, modes=modes, limit=MAX_FILE)
        write_exclusive(target, raw)
        files[relative] = sha(raw)
    write_exclusive(run / 'materials.json', canonical({'files': files, 'old_binary_sha256': OLD_BINARY_SHA}))
    return files


def dump_snapshot(run, environment, snapshot):
    path = run / 'database-schema23.dump'
    error_path = run / 'dump.stderr'
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb') as output:
        with os.fdopen(os.open(error_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb') as errors:
            process = subprocess.Popen([str(PG / 'pg_dump'), '--no-password', '--format=custom', '--schema=public',
                                        '--snapshot=' + snapshot, '--no-owner', '--no-privileges'],
                                       env=environment, stdout=output, stderr=errors)
            deadline = time.monotonic() + 180
            try:
                while process.poll() is None:
                    require(time.monotonic() < deadline and path.stat().st_size <= MAX_FILE and
                            error_path.stat().st_size <= 8 << 20, 'The snapshot dump exceeded its time or byte bound.')
                    time.sleep(0.1)
                require(process.returncode == 0, 'The fresh snapshot dump failed; its evidence is retained.')
            finally:
                if process.poll() is None:
                    process.kill()
                    process.wait(timeout=5)
            output.flush()
            errors.flush()
            os.fsync(output.fileno())
            os.fsync(errors.fileno())
    require(0 < path.stat().st_size <= MAX_FILE, 'The fresh snapshot dump is empty or oversized.')
    sync_directory(run)
    return file_fact(path)


def observe_rehearsal(name):
    require(REHEARSAL.fullmatch(name), 'A rehearsal identity escaped its new random namespace.')
    query = """BEGIN READ ONLY; SELECT json_build_object('system_identifier',(SELECT system_identifier::text FROM pg_control_system()),
    'postmaster_start',pg_postmaster_start_time(),'database',(SELECT json_build_object('name',d.datname,'oid',d.oid::bigint,
    'owner',pg_get_userbyid(d.datdba),'owner_oid',d.datdba::bigint,'comment',shobj_description(d.oid,'pg_database'),
    'clients',(SELECT count(*) FROM pg_stat_activity a WHERE a.datid=d.oid),
    'prepared',(SELECT count(*) FROM pg_prepared_xacts x WHERE x.database=d.datname)) FROM pg_database d WHERE d.datname='%s'),
    'role',(SELECT json_build_object('name',r.rolname,'oid',r.oid::bigint,'comment',shobj_description(r.oid,'pg_authid'),
    'safe',NOT(r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication OR r.rolbypassrls),
    'memberships',(SELECT count(*) FROM pg_auth_members m WHERE m.member=r.oid OR m.roleid=r.oid OR m.grantor=r.oid),
    'outside_dependencies',(SELECT count(*) FROM pg_shdepend s WHERE s.refclassid='pg_authid'::regclass AND s.refobjid=r.oid
    AND NOT(s.dbid=COALESCE((SELECT oid FROM pg_database WHERE datname='%s'),0) AND s.dbid<>0)
    AND NOT(s.dbid=0 AND s.classid='pg_database'::regclass AND s.objid=(SELECT oid FROM pg_database WHERE datname='%s') AND s.deptype='o')))
    FROM pg_roles r WHERE r.rolname='%s')); ROLLBACK;""" % (name, name, name, name)
    return decode(pg(query))


def validate_rehearsal(receipt, observed, *, database_required=True):
    target = receipt['target']
    require(receipt.get('marker') == MARKER and receipt.get('phase') == 'created' and
            REHEARSAL.fullmatch(target['database']) and target['database'] == target['role'] and
            target['database'] not in (MAIN['database'], RECOVERY['database']) and
            type(target['database_oid']) is int and target['database_oid'] > 0 and
            type(target['role_oid']) is int and target['role_oid'] > 0 and
            target['database_oid'] not in (MAIN['database_oid'], RECOVERY['database_oid']) and
            target['role_oid'] not in (MAIN['role_oid'], RECOVERY['role_oid']) and
            observed['system_identifier'] == SYSTEM_IDENTIFIER and
            observed['postmaster_start'] == receipt['postmaster_start'], 'The rehearsal cluster or creation authority differs.')
    role = observed['role']
    require(role and role['name'] == target['role'] and role['oid'] == target['role_oid'] and role['safe'] and
            role['comment'] == target['owner_comment'] and role['memberships'] == role['outside_dependencies'] == 0,
            'The rehearsal role has drifted ownership, membership, or outside dependencies.')
    database = observed['database']
    if database_required:
        require(database and database['name'] == target['database'] and database['oid'] == target['database_oid'] and
                database['owner'] == target['role'] and database['owner_oid'] == target['role_oid'] and
                database['comment'] == target['owner_comment'] and database['clients'] == database['prepared'] == 0,
                'The rehearsal database identity or quiescent state differs.')
    else:
        require(database is None, 'The rehearsal database was not removed.')


def create_rehearsal(run):
    name = 'goby_upgrade_m3e_' + secrets.token_hex(12)
    comment = MARKER + ':' + run.name
    before = observe_rehearsal(name)
    require(before['database'] is None and before['role'] is None and before['system_identifier'] == SYSTEM_IDENTIFIER,
            'The new rehearsal name already exists or the cluster differs.')
    write_exclusive(run / 'rehearsal-intent.json', canonical({'marker': MARKER, 'name': name, 'comment': comment,
                                                          'cluster': before, 'run_id': run.name}))
    password = secrets.token_hex(32)
    url = 'postgresql://' + name + ':' + password + '@127.0.0.1:5432/' + name + '?sslmode=disable'
    write_exclusive(run / 'rehearsal-credentials.json', canonical({'database_url': url}))
    pg('BEGIN; SET LOCAL password_encryption=\'scram-sha-256\'; CREATE ROLE "' + name + '" LOGIN PASSWORD \'' + password +
       '\' NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 4; COMMENT ON ROLE "' +
       name + '" IS \'' + comment + '\'; COMMIT;', run=run, label='create-rehearsal-role')
    role = observe_rehearsal(name)['role']
    require(role and role['safe'] and role['comment'] == comment and role['memberships'] == 0,
            'The new rehearsal role was not created with its exact owner marker.')
    write_exclusive(run / 'rehearsal-role.json', canonical(role))
    pg('CREATE DATABASE "' + name + '" OWNER "' + name + '" TEMPLATE template0 ENCODING \'UTF8\';',
       run=run, label='create-rehearsal-database')
    pg('COMMENT ON DATABASE "' + name + '" IS \'' + comment + '\';', run=run, label='mark-rehearsal-database')
    observed = observe_rehearsal(name)
    target = {'database': name, 'database_oid': observed['database']['oid'], 'role': name,
              'role_oid': role['oid'], 'owner_comment': comment}
    receipt = {'marker': MARKER, 'phase': 'created', 'run_id': run.name, 'target': target,
               'postmaster_start': before['postmaster_start']}
    validate_rehearsal(receipt, observed)
    write_exclusive(run / 'rehearsal-created.json', canonical(receipt))
    return receipt, url


def filtered_restore_toc(raw):
    require(len(raw) <= 2 << 20, 'The archive inventory exceeds its bound.')
    lines = []
    for line in raw.decode().splitlines():
        if re.match(r'^\d+;\s+\d+\s+\d+\s+(?:SCHEMA\s+-\s+public(?:\s|$)|(?:COMMENT|ACL)\s+-\s+SCHEMA\s+public(?:\s|$))', line):
            continue
        lines.append(line)
    require(any(re.match(r'^\d+;.*\sTABLE\s+public\s+sessions\s', line) for line in lines),
            'The fresh archive omits the historical application tables.')
    return ('\n'.join(lines) + '\n').encode()


def rehearse(run, inputs):
    receipt, url = create_rehearsal(run)
    target = receipt['target']
    validate_rehearsal(receipt, observe_rehearsal(target['database']))
    environment = database_environment(url, target['database'], target['role'], readonly=False)
    empty = decode(pg("BEGIN READ ONLY; SELECT to_json((SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public')); ROLLBACK;",
                      database=target['database'], environment=environment))
    require(empty == 0, 'The newly created rehearsal database was not empty.')
    schema_owner_kind = decode(read_file(run / 'baseline.json'))['state']['identity']['schema_owner_kind']
    require(schema_owner_kind in ('database_role', 'pg_database_owner'), 'The source public owner mapping is unsupported.')
    if schema_owner_kind == 'database_role':
        pg('ALTER SCHEMA public OWNER TO "' + target['role'] + '";', database=target['database'],
           environment=environment, run=run, label='match-rehearsal-schema-owner')
    toc = filtered_restore_toc(command([PG / 'pg_restore', '--list', run / 'database-schema23.dump'], run=run, label='archive-inventory'))
    write_exclusive(run / 'restore-toc.list', toc)
    # This is the sole archive-import command. Its database and ordinary role
    # are freshly created and revalidated; no main or recovery database is a
    # possible target. The archive came from our pinned, trusted schema23 dump.
    command([PG / 'pg_restore', '--no-password', '--no-owner', '--no-privileges', '--exit-on-error', '--single-transaction',
             '--use-list=' + str(run / 'restore-toc.list'), '--dbname=' + target['database'], run / 'database-schema23.dump'],
            environment=environment, timeout=240, run=run, label='rehearsal-restore')
    result = invoke_helper(run, inputs, target, url, 'migrate', 'rehearsal-migrated', baseline=run / 'baseline.json')
    require(result.get('rehearsal') is True and result.get('target_schema_version') == 25,
            'The rehearsal did not preserve and migrate the real current backup.')
    return receipt, url, result


def cleanup_rehearsal(run, inputs, receipt, url, migrated):
    target = receipt['target']
    require(receipt.get('run_id') == run.name and target.get('owner_comment') == MARKER + ':' + run.name and
            decode(read_file(run / 'rehearsal-created.json')) == receipt,
            'The rehearsal creation receipt changed.')
    checked = invoke_helper(run, inputs, target, url, 'inspect', 'rehearsal-before-cleanup', expected_schema=25)
    require(checked['state_sha256'] == migrated['state_sha256'],
            'The rehearsal acquired unknown objects, rows, or sequence state before cleanup.')
    first = observe_rehearsal(target['database'])
    validate_rehearsal(receipt, first)
    write_exclusive(run / 'rehearsal-disposal-intent.json', canonical({'receipt': receipt, 'verified_state_sha256': checked['state_sha256'],
                                                                   'before': first}))
    second = observe_rehearsal(target['database'])
    validate_rehearsal(receipt, second)
    require(second == first, 'The rehearsal identity changed immediately before removal.')
    pg('DROP DATABASE "' + target['database'] + '";', run=run, label='remove-rehearsal-database')
    observed = observe_rehearsal(target['database'])
    validate_rehearsal(receipt, observed, database_required=False)
    pg('DROP ROLE "' + target['role'] + '";', run=run, label='remove-rehearsal-role')
    after = observe_rehearsal(target['database'])
    require(after['database'] is None and after['role'] is None and after['system_identifier'] == SYSTEM_IDENTIFIER,
            'The exact rehearsal cleanup was not proven.')
    write_exclusive(run / 'rehearsal-disposed.json', canonical({'marker': MARKER, 'status': 'disposed', 'receipt': receipt,
                                                            'after': after, 'evidence_retained': True}))


def prepared_files(run):
    return {p.relative_to(run).as_posix(): file_fact(p)['sha256'] for p in sorted(run.rglob('*'))
            if p.is_file() and p.name != 'prepared.json'}


def prepare(args, inputs, url, environment):
    before = protected_state(environment)
    run = create_run(inputs)
    try:
        write_exclusive(run / 'protected-before.json', canonical(before))
        save_materials(run, before)
        with exported_snapshot(environment) as snapshot:
            baseline = invoke_helper(run, inputs, MAIN, url, 'inspect', 'baseline', snapshot=snapshot)
            dump = dump_snapshot(run, environment, snapshot)
        require(baseline.get('source_schema_version') == 23 and baseline.get('state', {}).get('schema_version') == 23,
                'The fresh operational backup is not schema23.')
        write_exclusive(run / 'backup-complete.json', canonical({'marker': MARKER, 'source_schema': 23,
                                                               'baseline_sha256': sha(read_file(run / 'baseline.json')), 'dump': dump}))
        require(protected_state(environment) == before, 'The protected deployment changed during its fresh backup.')
        receipt, rehearsal_url, migrated = rehearse(run, inputs)
        cleanup_rehearsal(run, inputs, receipt, rehearsal_url, migrated)
        require(protected_state(environment) == before, 'The rehearsal changed existing main, recovery, or private state.')
        final = invoke_helper(run, inputs, MAIN, url, 'inspect', 'main-after-rehearsal')
        require(final['state_sha256'] == baseline['state_sha256'], 'Current main data changed after its fresh backup.')
        receipt = {'marker': MARKER, 'version': 1, 'phase': 'prepared', 'run_id': run.name,
                   'inputs_sha256': sha(read_file(run / 'inputs.json')), 'files': prepared_files(run),
                   'main_state_sha256': baseline['state_sha256'], 'main_database_restored': False,
                   'rehearsal_passed': True, 'rehearsal_removed': True}
        write_exclusive(run / 'prepared.json', canonical(receipt))
        return {'status': 'prepared', 'receipt': str(run / 'prepared.json'), 'sha256': sha(canonical(receipt))}
    except BaseException as error:
        write_exclusive(run / 'terminal.json', canonical({'marker': MARKER, 'status': 'failed',
                                                        'error_code': type(error).__name__, 'evidence_retained': True}))
        raise


def verify_prepared(args, inputs):
    run = args.prepared.parent
    validate_phase(run, inputs, 'rehearsal-disposed')
    raw = read_file(args.prepared)
    require(sha(raw) == args.prepared_sha256, 'The prepared receipt digest differs.')
    receipt = decode(raw)
    require(receipt.get('marker') == MARKER and receipt.get('version') == 1 and receipt.get('phase') == 'prepared' and
            receipt.get('run_id') == run.name and receipt.get('inputs_sha256') == sha(read_file(run / 'inputs.json')) and
            receipt.get('main_database_restored') is False and receipt.get('rehearsal_passed') is True and
            receipt.get('rehearsal_removed') is True and receipt.get('files') == prepared_files(run),
            'The prepared backup, rehearsal, or complete artifact membership differs.')
    return run, receipt


def install_file(path, payload, expected_sha, run):
    require(path == LIVE / 'goby' or path.is_relative_to(LIVE / 'admin'), 'Installation escaped the main binary and assets.')
    parent = path.parent
    if not parent.exists():
        require(parent.is_relative_to(LIVE / 'admin'), 'An installation parent escaped the asset root.')
        current = LIVE / 'admin'
        for component in parent.relative_to(current).parts:
            current = current / component
            if not current.exists():
                current.mkdir(mode=0o755)
                os.chmod(current, 0o755)
            current_info = path_info(current)
            require(stat.S_ISDIR(current_info.st_mode) and current_info.st_uid == current_info.st_gid == 0 and
                    not current_info.st_mode & 0o022, 'A candidate asset parent permits untrusted writes.')
    info = path_info(parent)
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
            'An installation parent permits untrusted writes.')
    if path.exists():
        require(file_fact(path, modes=(0o600, 0o644, 0o755))['sha256'] == expected_sha,
                'An installed file changed before candidate publication.')
    else:
        require(expected_sha is None, 'A previously installed file disappeared.')
    stage = parent / ('.schema25-stage-' + secrets.token_hex(12))
    descriptor = os.open(stage, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o755 if path == LIVE / 'goby' else 0o644)
    with os.fdopen(descriptor, 'wb') as handle:
        os.fchmod(handle.fileno(), 0o755 if path == LIVE / 'goby' else 0o644)
        handle.write(payload)
        handle.flush()
        os.fsync(handle.fileno())
    if path.exists():
        require(file_fact(path, modes=(0o600, 0o644, 0o755))['sha256'] == expected_sha,
                'An installed file changed at its publication boundary.')
    os.replace(stage, path)
    sync_directory(parent)


def install_candidate(run, inputs, before):
    assets = read_assets(Path(inputs['assets']['path']))
    old_assets = before['assets']['files']
    # Publish the HTML entry point last; keep every unrelated historical asset.
    for name in sorted(assets, key=lambda entry: (entry == 'index.html', entry)):
        install_file(LIVE / 'admin' / name, assets[name], old_assets.get(name, {}).get('sha256'), run)
    install_file(LIVE / 'goby', read_file(Path(inputs['candidate']['path']), modes=(0o600, 0o755), limit=MAX_FILE),
                 OLD_BINARY_SHA, run)
    require(file_fact(LIVE / 'goby', modes=(0o755,))['sha256'] == inputs['candidate']['sha256'],
            'The installed candidate binary digest differs.')
    for name, digest in inputs['asset_members'].items():
        require(file_fact(LIVE / 'admin' / name, modes=(0o644, 0o755))['sha256'] == digest,
                'A published native asset differs from its candidate manifest.')
    write_exclusive(run / 'installed.json', canonical({'marker': MARKER, 'binary_sha256': inputs['candidate']['sha256'],
                                                      'assets': inputs['asset_members']}))


def native_smoke(run, inputs, old_backups):
    credentials = private_values(BROWSER, {'GOBY_SMOKE_NAME', 'GOBY_SMOKE_PASSWORD'})
    cookie, csrf = '', ''
    calls = []

    def request(method, path, body=None, expected=200):
        nonlocal cookie, csrf
        allowed = ((method == 'GET' and path in (*NATIVE_READS, '/healthz', '/readyz', '/admin/')) or
                   (method in ('POST', 'DELETE') and path == SESSION))
        require(allowed and len(calls) < 24, 'The native smoke escaped its fixed request scope.')
        service_state(active=True, expected_binary=inputs['candidate']['sha256'])
        headers = {'Accept': 'application/json', 'Origin': 'http://127.0.0.1:18096', 'Connection': 'close'}
        if cookie:
            headers['Cookie'] = 'goby_session=' + cookie
        if csrf:
            headers['X-CSRF-Token'] = csrf
        payload = canonical(body) if body is not None else None
        if payload is not None:
            headers['Content-Type'] = 'application/json'
        connection = http.client.HTTPConnection('127.0.0.1', 18096, timeout=10)
        try:
            connection.request(method, path, payload, headers)
            response = connection.getresponse()
            if method == 'POST':
                cookies = SimpleCookie()
                cookies.load(response.getheader('Set-Cookie', ''))
                if 'goby_session' in cookies:
                    candidate = cookies['goby_session'].value
                    require(re.fullmatch(r'[A-Za-z0-9_-]{32,512}', candidate), 'The login cookie has an unexpected shape.')
                    cookie = candidate
                    csrf = sha(('goby:admin:csrf:' + cookie).encode())
            raw = response.read((2 << 20) + 1)
            require(len(raw) <= 2 << 20 and response.status == expected, 'A native smoke response failed its bounded expectation.')
            if path == '/admin/':
                require(sha(raw) == inputs['asset_members']['index.html'], 'The served native index differs from the installed candidate.')
            calls.append({'method': method, 'path': path, 'status': response.status, 'bytes': len(raw)})
            return decode(raw) if path.startswith('/admin/v1/') and raw else None
        finally:
            connection.close()

    try:
        request('GET', '/healthz')
        request('GET', '/readyz')
        request('GET', '/admin/')
        request('POST', SESSION, {'Name': credentials['GOBY_SMOKE_NAME'], 'Password': credentials['GOBY_SMOKE_PASSWORD']})
        require(cookie and csrf, 'The native smoke did not acknowledge its new login.')
        for path in NATIVE_READS:
            value = request('GET', path)
            if path == BACKUPS:
                listed = {x.get('Id'): x for x in value.get('Items', [])}
                require(all(old['id'] in listed and listed[old['id']].get('SHA256') == old['sha256'] for old in old_backups),
                        'The native backup list lost or changed a preexisting archive.')
    finally:
        logout_verified = False
        try:
            if cookie:
                request('DELETE', SESSION, expected=204)
                request('GET', SESSION, expected=401)
                logout_verified = True
        finally:
            write_exclusive(run / 'native-smoke.json', canonical({'calls': calls, 'login_acknowledged': bool(cookie),
                                                                'logout_verified': logout_verified, 'create_requests': 0,
                                                                'restore_requests': 0, 'archive_delete_requests': 0}))


def wait_ready(expected_binary):
    deadline = time.monotonic() + 45
    first = None
    while time.monotonic() < deadline:
        state = service_state(active=True, expected_binary=expected_binary)
        if first is None:
            first = (state['MainPID'], state['start_ticks'])
        require((state['MainPID'], state['start_ticks']) == first, 'The candidate process changed during readiness observation.')
        connection = http.client.HTTPConnection('127.0.0.1', 18096, timeout=1)
        try:
            connection.request('GET', '/readyz', headers={'Connection': 'close'})
            response = connection.getresponse()
            raw = response.read(65537)
            require(len(raw) <= 65536, 'The readiness response exceeds its bound.')
            if response.status == 200:
                return state
        except (OSError, http.client.HTTPException):
            pass
        finally:
            connection.close()
        time.sleep(0.25)
    raise Failure('The candidate did not become ready within its bounded startup window.')


def deploy(args, inputs, url, environment):
    run, prepared = verify_prepared(args, inputs)
    before = decode(read_file(run / 'protected-before.json'))
    require(protected_state(environment) == before, 'Protected deployment state changed since preparation.')
    current = invoke_helper(run, inputs, MAIN, url, 'inspect', 'main-before-deploy')
    require(current['state_sha256'] == prepared['main_state_sha256'], 'Current data no longer matches the fresh prepared backup.')
    write_exclusive(run / 'deployment-intent.json', canonical({'marker': MARKER, 'prepared_sha256': args.prepared_sha256,
                                                            'candidate_sha256': inputs['candidate']['sha256']}))
    try:
        result = invoke_helper(run, inputs, MAIN, url, 'migrate', 'main-migrated', baseline=run / 'baseline.json')
        require(result.get('rehearsal') is False and result.get('target_schema_version') == 25,
                'The main migration did not publish its exact schema25 commit proof.')
        require(protected_state(environment) == before, 'Offline migration changed protected runtime, recovery, or backup state.')
        install_candidate(run, inputs, before)
        service_state(expected_binary=inputs['candidate']['sha256'])
        quiescent(environment)
        write_exclusive(run / 'start-intent.json', canonical({'marker': MARKER, 'candidate_sha256': inputs['candidate']['sha256']}))
        command(['/usr/bin/systemctl', 'start', SERVICE], run=run, label='start-reviewed-candidate', timeout=40)
        deadline = time.monotonic() + 40
        while True:
            try:
                active = service_state(active=True, expected_binary=inputs['candidate']['sha256'])
                break
            except Failure:
                require(time.monotonic() < deadline, 'The candidate did not become active within its startup bound.')
                time.sleep(0.25)
        active = wait_ready(inputs['candidate']['sha256'])
        native_smoke(run, inputs, before['stores']['backups'])
        after_stores = store_state()
        require(after_stores['backups'] == before['stores']['backups'] and after_stores['pairing'] == before['stores']['pairing'] and
                after_stores['old_operational_backup'] == before['stores']['old_operational_backup'],
                'Startup or smoke changed protected archives, paired secrets, or historical evidence.')
        terminal = {'marker': MARKER, 'status': 'passed', 'schema_version': 25, 'binary_sha256': inputs['candidate']['sha256'],
                    'active_service': active, 'main_database_restored': False, 'old_binary_rollback': False,
                    'old_archives_retained': True, 'prepared_sha256': args.prepared_sha256}
        write_exclusive(run / 'terminal.json', canonical(terminal))
        return {'status': 'passed', 'report': str(run / 'terminal.json')}
    except BaseException as error:
        # Schema25 may already be committed and the service may have started.
        # Never mask this with a schema23 binary or a restore of older data.
        write_exclusive(run / 'terminal.json', canonical({'marker': MARKER, 'status': 'failed',
                                                        'error_code': type(error).__name__, 'forward_repair_required': True,
                                                        'main_database_restored': False, 'old_binary_rollback': False,
                                                        'evidence_retained': True}))
        raise


def parser():
    value = argparse.ArgumentParser(description=__doc__)
    value.add_argument('--mode', choices=('preflight', 'prepare', 'deploy'), required=True)
    for name in ('source', 'candidate', 'assets', 'full-report', 'helper', 'tool-source', 'helper-build-report', 'guard-report'):
        value.add_argument('--' + name, type=Path, required=True)
    for name in ('manifest', 'candidate', 'assets', 'full-report', 'helper', 'tool-manifest', 'helper-build-report', 'guard-report'):
        value.add_argument('--' + name + '-sha256', required=True)
    value.add_argument('--prepared', type=Path)
    value.add_argument('--prepared-sha256')
    return value


def main(argv=None):
    args = parser().parse_args(argv)
    try:
        require(os.geteuid() == 0 and sys.platform == 'linux', 'This operator requires the reviewed Linux root environment.')
        validate_arguments(args)
        inputs = load_inputs(args)
        url = runtime_policy()
        environment = database_environment(url, MAIN['database'], MAIN['role'])
        if args.mode == 'preflight':
            protected_state(environment)
            print(json.dumps({'status': 'preflight-passed', 'source_manifest_sha256': args.manifest_sha256,
                              'candidate_sha256': args.candidate_sha256, 'writes_executed': False}))
            return 0
        with operator_lock(args.mode == 'prepare'):
            result = prepare(args, inputs, url, environment) if args.mode == 'prepare' else deploy(args, inputs, url, environment)
        print(json.dumps(result))
        return 0
    except BaseException as error:
        if isinstance(error, KeyboardInterrupt):
            reason = 'interrupted'
        else:
            reason = str(error) if isinstance(error, Failure) else type(error).__name__
        print(json.dumps({'status': 'failed', 'reason': reason, 'automatic_recovery_executed': False}))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
