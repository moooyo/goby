#!/usr/bin/env python3
"""Deploy source55/schema28 to the existing candidate once, after rehearsal.

The preflight entrypoint only reads. Run owns one new private output, one new
rehearsal pair and one candidate stop/start. Backup and rehearsal finish while
the old candidate is running; any drift aborts before stop. No failure resumes,
retries a mutation or rolls back. An independent attest proves the outer unit.
Build the Go helper in a separate, receipted source copy, never in source55.
"""

from __future__ import annotations

import argparse
import copy
import datetime as dt
from decimal import Decimal
import fcntl
import hashlib
import http.client
from html.parser import HTMLParser
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
import types
import urllib.parse

sys.dont_write_bytecode = True
MARKER = 'goby-client-schema28-source55-upgrade-v1'
WORK = Path('/opt/goby-test/exec-work-m3e')
TOOL = WORK / 'client-schema28-source55-tool-05'
SOURCE = WORK / 'source-attempt-55'
SOURCE_SHA = '7d2548603e209ebce6154853321147aeec33ccbb40cc12b3ca37418ad765937a'
STATE = WORK / 'client-fixture.json'
STATE_SHA = 'd6b8872eab70848b834c50e26cc2b84e9d137dd68049a1be85d09afa811829d4'
BASELINE = WORK / 'collection-folder-contract-v1/after-full.json'
BASELINE_SHA = '2659f45dfa82d8568b07375d04c08cd4ba216defcfa2a4a1c269867b176573be'
RUNTIME = WORK / 'runtime.env'
RUNTIME_SHA = 'd8689a4e0b36816ed462816856dfa73af6fba5f31f044632ed173db99c8842df'
MASTER = Path('/var/lib/goby-test/client-m3e/master.key')
INSTALL = Path('/opt/goby-client-m3e')
BINARY = INSTALL / 'goby'
WEB = INSTALL / 'admin'
WEB_SOURCE = WORK / 'storage-binding-workflow-web-03/dist'
WEB_REPORT_SHA = '2b33e2805afb7422eeb8486db9c4f531cd074a636e4dcddc08c91c8d0808a9d7'
OLD_BINARY_SHA = 'cd67f2e71ff1b63e3c138cdba1f9c9d1e584e2788b47964a332b38311cda0e2d'
OLD_PROCESS = {'boot_id': '6bdfc486-7bc8-412f-82b5-70095a09dde7', 'pid': 1264063, 'start_ticks': 11104222}
OLD_INVOCATION = 'c0a5244ae25646c8bd92c3e3c1636575'
FULL_RUN = '20260912_084241_db776aacc1a7'
FULL_UNIT = 'goby-collection-folder-source55-full-controller-v1.service'
FULL_INVOCATION = '20f0d83200c34228aa5862e07ce24c2c'
FULL_REPORT = WORK / ('client-backup-run-' + FULL_RUN) / 'report.json'
FULL_TERMINAL = WORK / 'collection-folder-source55-full-execution-01/terminal.json'
UNIT = 'goby-client-m3e.service'
PRIMARY_UNIT = 'goby-foundation-test.service'
CATALOG27_SHA = '1fc91c2e380805bff0f87867547d307bc7830ffeb49c3489da4e1713a5c0047d'
CATALOG28_SHA = '8e7569c8fe2073ee2ed4c51147f9abc21061d1aac9554843101b826fa5a1cc2b'
CONTROL = Path('/opt/goby-test/postgres-workspace-v1')
PGDATA = Path('/var/lib/postgresql/goby-workspace-v1/data')
HBA = PGDATA / 'pg_hba.conf'
HISTORY = CONTROL / 'client-backup-pair.json'
PG = Path('/usr/lib/postgresql/17/bin')
ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8'}
HASH_RE = r'[0-9a-f]{64}'
RUN_RE = r'[0-9]{8}_[0-9]{6}_[0-9a-f]{12}'
HELPERS = {
    'base': (WORK / 'client-notifications-source44-tool-01/upgrade-client-notifications-source44.py',
             '92da604ba21390d4c861b47a3ff3d7a05e905a646ee3f894392b860ee4a705c1'),
    'op': (WORK / 'prepare-client-fixture.py', '84d21e8ac0b48c5dfd3d2c7ae35b0aec0d7f4f811e65658081490aa44947b49c'),
    'profile': (WORK / 'client-special-features-profile-tool-01/client-special-features-profile.py',
                '6bbc0bfd17b1c7eef44028fe34481436d39a3e6645442340189e2da35c382252'),
    'ext': (WORK / 'client-extra-root-tool-02/extend-client-special-features-root.py',
            '7a9cda4a36ebbe9c2311031db1f6a57265eef8fc8807057a3d650ef6c16b2a46'),
    'dbtools': (WORK / 'storage-binding-live-ui-tool-05/verify-storage-binding-live-ui.py',
                '87c401d9acabc383e074c4c5f9fd493c63222b5d545d73f9cf8c5f11e797a685'),
}
TOOL_FILES = frozenset(('upgrade-client-schema28.py', 'test-upgrade-client-schema28.py',
                        'migrate-client-schema28.go', 'migrate-client-schema28_test.go', 'migrate-client-schema28',
                        'helper-build.json', 'guards-report.json', 'accepted-web-report.json', 'publication.json'))
SELECTED = ('scripts/test-env/prepare-postgres-workspace.py', 'internal/backuppg/catalog.go',
            'internal/backuppg/catalogs/schema-27-postgresql-17.json',
            'internal/backuppg/catalogs/schema-28-postgresql-17.json')
NEW_COLUMNS = {'library_roots': ['binding_revision', 'storage_binding', 'bound_at', 'bound_by'],
               'activity_entries': ['previous_revision', 'observation_fingerprint']}
COUNTS = {'sessions': 75, 'devices': 64, 'activity_entries': 167, 'items': 22, 'libraries': 4,
          'library_roots': 4, 'item_extra_resources': 4, 'extra_reserved_paths': 3,
          'client_playback_references': 0, 'encoding_jobs': 0}
ERROR_CODES = frozenset(('guard_rejected', 'external_failure', 'master_runtime_path_invalid', 'master_state_invalid',
    'master_required_missing', 'master_unexpected_presence', 'master_unexpected_absence', 'master_state_changed',
    'backup_materials_incomplete', 'command_failed', 'command_timeout', 'helper_failed', 'helper_retained',
    'helper_outcome_unknown', 'helper_committed_cleanup_failed', 'helper_committed_report_failed'))
HELPER_FAILURES = frozenset(('source_snapshot_rejected', 'source_facts_rejected', 'source_sequences_changed_during_dump',
    'snapshot_dump_failed', 'snapshot_dump_configuration', 'snapshot_dump_database', 'snapshot_dump_archive',
    'snapshot_dump_limit', 'snapshot_dump_schema', 'snapshot_dump_target', 'snapshot_dump_command',
    'snapshot_dump_unsupported', 'snapshot_dump_cancelled', 'snapshot_dump_deadline',
    'role_properties_invalid', 'backup_baseline_drift', 'rehearsal_restore_failed', 'rehearsal_privileges_changed',
    'candidate_lease_busy_or_unavailable', 'migration_failed', 'migration_commit_outcome_unknown',
    'migration_report_publication_failed', 'candidate_lease_close_failed'))
ERROR_CODES = ERROR_CODES | frozenset('helper_' + value for value in HELPER_FAILURES)
OPERATION_STAGES = frozenset(('preflight', 'output_setup', 'backup_materials_fixed', 'backup_materials_recovery',
    'backup_materials_master', 'helper_intent', 'helper_backup', 'helper_inspect', 'helper_rehearse', 'helper_migrate',
    'backup_baseline', 'rehearsal', 'rehearsal_readback', 'rehearsal_hba_restore', 'rehearsal_dispose',
    'pre_stop_baseline', 'stopped_baseline', 'candidate_stop', 'candidate_start', 'migration_readback',
    'install', 'readiness', 'finalize', 'unknown'))


class Failure(Exception):
    """A fixed identity, preservation or one-shot boundary failed."""

    def __init__(self, message, code='guard_rejected'):
        super().__init__(message)
        self.code = code if code in ERROR_CODES else 'guard_rejected'


def require(value, message, code='guard_rejected'):
    if not value:
        raise Failure(message, code)


def safe_error_code(error):
    code = getattr(error, 'code', None)
    return code if type(code) is str and code in ERROR_CODES else 'external_failure'


def helper_failure_code(raw):
    """Map only the helper's fixed failure envelope, never its free text."""
    try:
        value = decode(raw, 4096)
    except (Failure, TypeError):
        return 'helper_failed'
    codes = {'failed': 'helper_failed', 'retained': 'helper_retained', 'unknown': 'helper_outcome_unknown',
             'committed_cleanup_failed': 'helper_committed_cleanup_failed', 'committed_report_failed': 'helper_committed_report_failed'}
    if type(value) is dict and set(value) == {'marker', 'status', 'error'} and value['marker'] == 'goby-client-schema28-helper-v1':
        if type(value.get('status')) is str and value['status'] in codes:
            if type(value.get('error')) is str and value['error'] in HELPER_FAILURES:
                return 'helper_' + value['error']
            return codes[value['status']]
    return 'helper_failed'


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def exact(value, keys):
    require(type(value) is dict and set(value) == set(keys), 'A document has unexpected fields.')


def matches(pattern, value):
    return type(value) is str and re.fullmatch(pattern, value) is not None


def canonical(value):
    def encode(item):
        if item is None:
            return 'null'
        if type(item) is bool:
            return 'true' if item else 'false'
        if type(item) is int:
            return str(item)
        if isinstance(item, Decimal):
            require(item.is_finite(), 'Nonfinite JSON is forbidden.')
            return str(item)
        if type(item) is str:
            return json.dumps(item, ensure_ascii=True)
        if type(item) is list:
            return '[' + ','.join(encode(child) for child in item) + ']'
        if type(item) is dict:
            require(all(type(key) is str for key in item), 'JSON keys must be strings.')
            return '{' + ','.join(json.dumps(key, ensure_ascii=True) + ':' + encode(item[key]) for key in sorted(item)) + '}'
        raise Failure('A JSON value has an unsupported type.')
    return encode(value).encode() + b'\n'


def decode(raw, limit=64 << 20):
    require(type(raw) is bytes and len(raw) <= limit, 'A JSON document exceeds its bound.')
    def pairs(items):
        value = {}
        for key, item in items:
            require(key not in value, 'Duplicate JSON fields are forbidden.')
            value[key] = item
        return value
    try:
        value = json.loads(raw, object_pairs_hook=pairs, parse_float=Decimal,
                           parse_constant=lambda _: (_ for _ in ()).throw(Failure('Nonfinite JSON is forbidden.')))
    except (ValueError, UnicodeError, RecursionError):
        raise Failure('A JSON document is invalid.') from None
    remaining = [1000000]
    def visit(item, depth=0):
        remaining[0] -= 1
        require(depth <= 40 and remaining[0] >= 0, 'A JSON document exceeds its structural bound.')
        if type(item) in (dict, list):
            for child in (item.values() if type(item) is dict else item):
                visit(child, depth + 1)
    visit(value)
    return value


def identity(info):
    return {'device': info.st_dev, 'inode': info.st_ino, 'uid': info.st_uid, 'gid': info.st_gid,
            'mode': stat.S_IMODE(info.st_mode), 'links': info.st_nlink, 'bytes': info.st_size,
            'mtime_ns': info.st_mtime_ns, 'ctime_ns': info.st_ctime_ns}


def present(path):
    return os.path.lexists(path)


def protected(path, expected=None, *, uid=0, gid=0, modes=(0o600, 0o644), limit=128 << 20):
    require(isinstance(path, Path) and path.is_absolute() and str(path) == os.path.normpath(str(path)),
            'An input path is not canonical.')
    for parent in reversed(path.parents):
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and not info.st_mode & 0o022 and info.st_uid in (0, uid),
                'An input parent is untrusted or a symlink.')
    before = path.lstat()
    require(stat.S_ISREG(before.st_mode) and (before.st_uid, before.st_gid) == (uid, gid) and
            before.st_nlink == 1 and stat.S_IMODE(before.st_mode) in modes and 0 <= before.st_size <= limit,
            'An input file has unexpected metadata.')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as stream:
        require(identity(os.fstat(stream.fileno())) == identity(before), 'An input changed before opening.')
        raw = stream.read(limit + 1)
        require(identity(os.fstat(stream.fileno())) == identity(before), 'An input changed during reading.')
    require(identity(path.lstat()) == identity(before) and len(raw) == before.st_size and
            (expected is None or sha(raw) == expected), 'An input identity or digest changed.')
    return raw


def directory(path, mode=0o700):
    for parent in reversed((path, *path.parents)):
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                'A directory is untrusted or contains a symlink.')
    require(stat.S_IMODE(path.lstat().st_mode) == mode, 'A directory mode differs.')
    info = path.lstat()
    return {'device': info.st_dev, 'inode': info.st_ino, 'uid': info.st_uid, 'gid': info.st_gid, 'mode': mode}


def sync(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def create(path, raw, mode=0o600):
    require(type(raw) is bytes, 'An output must contain explicit bytes.')
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode)
    with os.fdopen(fd, 'wb') as stream:
        os.fchmod(stream.fileno(), mode)
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    sync(path.parent)


def descriptor(value, *, path=None):
    exact(value, {'path', 'sha256'})
    require(matches(HASH_RE, value['sha256']) and value['sha256'] != '0' * 64 and
            type(value['path']) is str and Path(value['path']).is_absolute() and
            str(Path(value['path'])) == os.path.normpath(value['path']) and
            (path is None or value['path'] == str(path)), 'An artifact descriptor is invalid.')


def load_module(path, expected, name):
    raw = protected(path, expected)
    module = types.ModuleType('schema28_candidate_' + name)
    module.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), module.__dict__)
    return module


def validate_intent(value):
    exact(value, {'marker', 'version', 'run_id', 'tool', 'output', 'source', 'helper', 'web',
                  'guards', 'candidate', 'controller', 'source_closure', 'history_sha256'})
    require(value['marker'] == MARKER and type(value['version']) is int and value['version'] == 1 and
            matches(RUN_RE, value['run_id']) and value['tool'] == str(TOOL), 'The deployment scope is invalid.')
    run = value['run_id']
    require(value['output'] == str(WORK / ('client-schema28-source55-upgrade-' + run)), 'The output is outside its new scope.')
    exact(value['controller'], {'unit'})
    require(value['controller']['unit'] == 'goby-client-schema28-source55-' + run.replace('_', '-') + '.service',
            'The controller unit is outside its new scope.')
    candidate = value['candidate']
    exact(candidate, {'state', 'baseline', 'process', 'invocation_id'})
    descriptor(candidate['state'], path=STATE)
    descriptor(candidate['baseline'], path=BASELINE)
    require(candidate['state']['sha256'] == STATE_SHA and candidate['baseline']['sha256'] == BASELINE_SHA and
            candidate['process'] == OLD_PROCESS and candidate['invocation_id'] == OLD_INVOCATION,
            'The current source44 candidate authority differs.')
    source = value['source']
    exact(source, {'root', 'manifest_sha256', 'full_report', 'terminal', 'binary', 'publication'})
    require(source['root'] == str(SOURCE) and source['manifest_sha256'] == SOURCE_SHA, 'The source55 identity differs.')
    for name, path in (('full_report', FULL_REPORT), ('terminal', FULL_TERMINAL), ('publication', TOOL / 'publication.json')):
        descriptor(source[name], path=path)
    exact(source['binary'], {'path', 'sha256', 'bytes'})
    descriptor({key: source['binary'][key] for key in ('path', 'sha256')})
    require(type(source['binary']['bytes']) is int and 1 << 20 < source['binary']['bytes'] <= 64 << 20 and
            source['binary']['sha256'] != OLD_BINARY_SHA, 'The replacement binary is invalid or unchanged.')
    helper = value['helper']
    exact(helper, {'source', 'binary', 'build'})
    for name, filename in (('source', 'migrate-client-schema28.go'), ('binary', 'migrate-client-schema28'), ('build', 'helper-build.json')):
        descriptor(helper[name], path=TOOL / filename)
    exact(value['web'], {'source', 'report'})
    require(value['web']['source'] == str(WEB_SOURCE), 'The accepted frontend root differs.')
    descriptor(value['web']['report'], path=TOOL / 'accepted-web-report.json')
    require(value['web']['report']['sha256'] == WEB_REPORT_SHA, 'The accepted frontend receipt differs.')
    descriptor(value['guards'], path=TOOL / 'guards-report.json')
    closure = value['source_closure']
    require(type(closure) is dict and set(closure) == TOOL_FILES and
            all(matches(HASH_RE, item) and item != '0' * 64 for item in closure.values()), 'The exact tool closure differs.')
    for item in (helper['source'], helper['binary'], helper['build'], value['guards'], value['web']['report'], source['publication']):
        require(closure[Path(item['path']).name] == item['sha256'], 'A descriptor disagrees with the tool closure.')
    require(matches(HASH_RE, value['history_sha256']), 'The preserved pair history lacks a digest.')
    return value


def load_intent(path, expected):
    require(path == TOOL / 'intent.json' and matches(HASH_RE, expected), 'Use the exact frozen input descriptor.')
    raw = protected(path, expected)
    return validate_intent(decode(raw, 4 << 20)), sha(raw)


def compare_baseline(before, after):
    left, right = copy.deepcopy(before), copy.deepcopy(after)
    for value in (left, right):
        value['database']['metadata'].pop('captured_at', None)
    require(canonical(left) == canonical(right), 'Candidate state drifted from the exact backup baseline; no retry is permitted.')


def validate_population(snapshot):
    tables = snapshot['database']['tables']
    require(len(tables) == 35 and len(snapshot['database']['sequences']) == 5 and
            all(len(tables.get(name, [])) == count for name, count in COUNTS.items()), 'The sealed candidate population differs.')
    require(all(row.get('relative_path') == '.' and row.get('allowed_path') == row.get('path')
                for row in tables['library_roots']), 'An existing root mapping differs from its sealed authority.')
    for name, field in (('scan_jobs', 'status'), ('task_runs', 'state'), ('task_run_children', 'state')):
        require(all(type(row.get(field)) is str and row[field].lower() not in ('waiting', 'pending', 'queued', 'running', 'stopping')
                    for row in tables[name]), 'The candidate has active or queued work.')
    sessions = {row['id']: row for row in tables['sessions']}
    require(len(sessions) == len(tables['sessions']), 'A session identity is repeated.')
    for row in tables['play_sessions']:
        require(row['state'] in ('Prepared', 'Stopped', 'Expired'), 'Playback is active or unrecognized.')
        if row['state'] == 'Prepared':
            owner = sessions.get(row['auth_session_id'], {})
            require(row['started_at'] is None and row['counted'] is False and row['application_client_id'] is None and
                    owner.get('kind') == 'emby' and owner.get('user_id') == row['user_id'] and owner.get('revoked_at') is not None,
                    'Prepared playback is not owned by an already revoked ordinary session.')


def validate_master_runtime(raw):
    expected = b"GOBY_API_KEY_MASTER_KEY_FILE='" + str(MASTER).encode('ascii') + b"'\n"
    require(type(raw) is bytes and raw.count(expected) == 1 and
            len(re.findall(rb'(?m)^GOBY_API_KEY_MASTER_KEY_FILE=', raw)) == 1,
            'The candidate master-key configuration does not identify its exact existing path.', 'master_runtime_path_invalid')


def validate_master_baseline(snapshot, runtime):
    """Keep an absent master only for a completely empty API-key population."""
    validate_master_runtime(runtime)
    recovery, tables = snapshot.get('recovery'), snapshot.get('database', {}).get('tables')
    require(type(recovery) is dict and type(tables) is dict and
            all(type(tables.get(name)) is list for name in ('application_keys', 'application_key_devices', 'sessions')),
            'The master-key baseline lacks the complete key population.', 'master_state_invalid')
    sessions = tables['sessions']
    require(all(type(row) is dict and type(row.get('kind')) is str for row in sessions),
            'The master-key baseline has an invalid session population.', 'master_state_invalid')
    counts = {'application_keys': len(tables['application_keys']), 'application_key_devices': len(tables['application_key_devices']),
              'application_key_sessions': sum(row['kind'] == 'application_key' for row in sessions)}
    proof = {'marker': 'goby-client-schema28-master-state-v1', 'path': str(MASTER), **counts}
    if 'master.key' not in recovery:
        require(all(value == 0 for value in counts.values()), 'A populated key scope is missing its master key.', 'master_required_missing')
        return {**proof, 'state': 'absent', 'bytes': 0, 'sha256': None}
    entry = recovery['master.key']
    require(type(entry) is dict and set(entry) == {'size', 'sha256'} and type(entry['size']) is int and entry['size'] == 32 and
            matches(HASH_RE, entry['sha256']), 'The existing master-key descriptor is invalid.', 'master_state_invalid')
    return {**proof, 'state': 'present', 'bytes': 32, 'sha256': entry['sha256']}


def compare_migrated(before, after, catalog, *, runtime_sha256):
    require(type(before['schema']) is int and before['schema'] == 27 and type(after['schema']) is int and after['schema'] == 28,
            'The migration labels differ.')
    left, right = before['database'], after['database']
    require(canonical(right['catalog']) == canonical(catalog['objects']) and right['unsupported'] is False and
            set(left['tables']) == set(right['tables']) and canonical(left['sequences']) == canonical(right['sequences']),
            'The migrated catalog or original sequence state differs.')
    for key in ('database', 'server_version_num', 'schemas', 'public_schema', 'relations'):
        require(canonical(left['metadata'][key]) == canonical(right['metadata'][key]), 'An existing namespace, relation or privilege changed.')
    require(set(left['metadata']['columns']) == set(right['metadata']['columns']), 'The table inventory changed.')
    for name, columns in left['metadata']['columns'].items():
        require(right['metadata']['columns'][name] == columns + NEW_COLUMNS.get(name, []), 'A table column projection changed.')
        rows = right['tables'][name]
        if name == 'schema_migrations':
            additions = [row for row in rows if row['version'] == 28]
            require(len(additions) == 1 and additions[0]['name'] == catalog['migrations'][-1]['name'], 'Migration28 history is missing.')
            rows = [row for row in rows if row['version'] != 28]
        projection = [{key: row[key] for key in columns} for row in rows]
        require(sorted(map(canonical, projection)) == sorted(map(canonical, left['tables'][name])), 'An original database row changed.')
    require(all(type(row['binding_revision']) is int and row['binding_revision'] == 1 and row['storage_binding'] is None and row['bound_at'] is None and row['bound_by'] is None
                for row in right['tables']['library_roots']), 'A historical root acquired approval during migration.')
    require(all(type(row['previous_revision']) is int and row['previous_revision'] == 0 and row['observation_fingerprint'] == '' for row in right['tables']['activity_entries']),
            'Historical audit fields changed during migration.')
    for key in ('recovery', 'browser_sha256', 'added_viewer_credentials'):
        require(canonical(before[key]) == canonical(after[key]), 'A private credential or recovery material changed.')
    require(after['runtime_sha256'] == runtime_sha256, 'The candidate runtime changed outside the declared web path.')
    validate_population(after)


def patch_runtime(raw):
    old = b"GOBY_WEB_DIR='/opt/goby-dev/admin'\n"
    new = b"GOBY_WEB_DIR='/opt/goby-client-m3e/admin'\n"
    require(raw.count(old) == 1 and len(re.findall(rb'(?m)^GOBY_WEB_DIR=', raw)) == 1,
            'The runtime does not contain exactly one original web directory.')
    result = raw.replace(old, new)
    require(result.replace(new, old) == raw, 'The runtime edit changed unrelated bytes.')
    return result


def frontend_routes(index, manifest):
    """Select only the accepted document's unique module and stylesheet."""
    class Entries(HTMLParser):
        def __init__(self):
            super().__init__(convert_charrefs=True)
            self.modules, self.styles = [], []

        def handle_starttag(self, tag, attributes):
            values = dict(attributes)
            require(len(values) == len(attributes), 'The accepted HTML repeats an attribute.')
            if tag == 'script' and values.get('type') == 'module':
                self.modules.append(values.get('src'))
            if tag == 'link' and values.get('rel') == 'stylesheet':
                self.styles.append(values.get('href'))
    parser = Entries()
    try:
        parser.feed(index.decode('utf-8'))
        parser.close()
    except (UnicodeError, ValueError):
        raise Failure('The accepted frontend document is invalid.') from None
    require(len(parser.modules) == len(parser.styles) == 1, 'The accepted frontend has no unique module and stylesheet.')
    result = []
    for reference, suffix in ((parser.modules[0], '.js'), (parser.styles[0], '.css')):
        require(type(reference) is str and reference.startswith('/admin/assets/') and reference.endswith(suffix) and
                not any(value in reference for value in ('?', '#', '\\', '%')) and '..' not in reference.split('/'),
                'A frontend entrypoint escaped its fixed public asset route.')
        relative = reference.removeprefix('/admin/')
        require(relative in manifest and matches(HASH_RE, manifest[relative]), 'A frontend entrypoint is absent from the accepted manifest.')
        result.append((reference, relative))
    return [('/admin/', 'index.html'), *result]


class ServiceFence:
    """Reserve each exact candidate service action before dispatch."""
    def __init__(self):
        self.stage, self.reserved = 'preflight', set()

    def approve(self, arguments):
        if arguments and Path(arguments[0]).name == 'systemctl':
            require(arguments[0] == '/usr/bin/systemctl' and len(arguments) > 1, 'A noncanonical service command is forbidden.')
            if arguments[1] == 'show':
                return
            action = arguments[1]
            require(action in ('stop', 'start') and arguments == ['/usr/bin/systemctl', action, UNIT] and
                    self.stage == action + '_requested' and action not in self.reserved and
                    (action != 'start' or self.reserved == {'stop'}), 'A service mutation is repeated, unowned or out of order.')
            self.reserved.add(action)


def verify_tree(root, expected, *, file_modes=(0o600, 0o644), maximum=256 << 20):
    require(type(expected) is dict and 1 <= len(expected) <= 15000, 'A tree inventory is invalid.')
    directory(root, stat.S_IMODE(root.lstat().st_mode))
    actual, total = {}, 0
    for path in sorted(root.rglob('*')):
        info = path.lstat()
        require(info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022, 'A tree member is untrusted.')
        relative = path.relative_to(root).as_posix()
        if stat.S_ISDIR(info.st_mode):
            continue
        require(relative in expected and stat.S_ISREG(info.st_mode), 'A tree contains an unlisted or special member.')
        total += info.st_size
        require(total <= maximum, 'A tree exceeds its byte budget.')
        actual[relative] = sha(protected(path, modes=file_modes, limit=128 << 20))
    require(actual == expected, 'A tree member or digest changed.')
    return {'root': str(root), 'files': actual, 'root_identity': directory(root, stat.S_IMODE(root.lstat().st_mode))}


def verify_candidate_web(files):
    directory(WEB, 0o755)
    for path in WEB.rglob('*'):
        info = path.lstat()
        require(info.st_uid == info.st_gid == 0 and
                ((stat.S_ISDIR(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o755) or
                 (stat.S_ISREG(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o644)), 'A candidate frontend mode or owner differs.')
    return verify_tree(WEB, files, file_modes=(0o644,), maximum=64 << 20)


def validate_full_terminal_summary(terminal, report):
    """Bind the observer's typed summary to its separately verified report."""
    summary = terminal.get('tests')
    exact(summary, {'failures', 'skips', 'top_level_passes'})
    tests, packages = report.get('tests'), report.get('packages')
    require(type(tests) is dict and all(type(summary[key]) is int and type(tests.get(key)) is int and
            summary[key] == tests[key] for key in ('failures', 'skips', 'top_level_passes')),
            'The full terminal test summary differs from the complete report.')
    require(type(packages) is list and type(terminal.get('packages')) is int and terminal['packages'] == len(packages),
            'The full terminal package count differs from the complete report.')


def verify_inputs(intent):
    directory(WORK)
    directory(TOOL)
    require(Path(__file__) == TOOL / 'upgrade-client-schema28.py', 'Use the frozen new operator entrypoint.')
    for name, digest in intent['source_closure'].items():
        protected(TOOL / name, digest, modes=(0o600, 0o644, 0o755))
    actual = set()
    for path in TOOL.iterdir():
        info = path.lstat()
        require(stat.S_ISREG(info.st_mode) and info.st_uid == info.st_gid == 0 and info.st_nlink == 1,
                'The tool root contains an unexpected member.')
        actual.add(path.name)
    require(actual == TOOL_FILES | {'intent.json'}, 'The exact tool membership changed.')
    source = intent['source']
    manifest = decode(protected(SOURCE / 'backup-source-inputs.json', SOURCE_SHA), 4 << 20)
    exact(manifest, {'marker', 'files'})
    require(manifest['marker'] == 'goby-client-backup-source-m3e-v1' and type(manifest['files']) is dict and
            len(manifest['files']) == 4243, 'The full source55 manifest differs.')
    # Inspect only product inputs and the selected self-written workspace helper.
    # In particular, do not open historical schema25 operators or client assets.
    product_files = {name: digest for name, digest in manifest['files'].items()
                     if name in ('go.mod', 'go.sum') or name.startswith(('cmd/', 'internal/'))}
    require(len(product_files) > 100, 'The source55 product closure is incomplete.')
    selected = {}
    for name, digest in {**product_files, **{name: manifest['files'].get(name) for name in SELECTED}}.items():
        require(matches(HASH_RE, digest) and not Path(name).is_absolute() and '..' not in Path(name).parts,
                'A selected source member is invalid.')
        raw = protected(SOURCE / name, digest)
        if name in SELECTED:
            selected[name] = raw
    require(sha(selected[SELECTED[2]]) == CATALOG27_SHA and sha(selected[SELECTED[3]]) == CATALOG28_SHA,
            'A published schema catalog changed.')
    catalog = decode(selected[SELECTED[3]])
    require(catalog.get('version') == 28, 'The target catalog is not schema28.')
    report = decode(protected(Path(source['full_report']['path']), source['full_report']['sha256']))
    full_path = Path(source['full_report']['path'])
    require(full_path.name == 'report.json' and full_path.parent.parent == WORK and
            matches('client-backup-run-' + RUN_RE, full_path.parent.name), 'The full report path is invalid.')
    run = full_path.parent.name.removeprefix('client-backup-run-')
    require(report.get('marker') == 'goby-client-backup-pair-m3e-v1' and report.get('status') == 'passed' and
            report.get('mode') == 'full' and type(report.get('schema')) is int and report['schema'] == 28 and
            report.get('source') == str(SOURCE) and report.get('source_manifest_sha256') == SOURCE_SHA and
            report.get('run_id') == run and report.get('catalog_sha256') == CATALOG28_SHA and
            type(report.get('unit_exit')) is int and report['unit_exit'] == 0,
            'The source55 full verification is not complete and successful.')
    cleanup_keys = {'unit_terminal', 'hba_restored_exactly', 'goby_backup_m3e_source_removed',
                    'goby_backup_m3e_target_removed', 'preexisting_catalog_unchanged', 'receipt_saved'}
    require(type(report.get('cleanup')) is dict and set(report['cleanup']) == cleanup_keys and
            all(item is True for item in report['cleanup'].values()), 'Full verification cleanup is incomplete.')
    tests = report.get('tests', {})
    require(type(tests.get('top_level_passes')) is int and tests['top_level_passes'] >= 2173 and
            type(tests.get('passed')) is list and all(type(name) is str for name in tests['passed']) and
            len(set(tests['passed'])) == len(tests['passed']) == tests['top_level_passes'] and
            all(type(tests.get(key)) is int and tests[key] == 0 for key in ('failures', 'skips')) and
            {'TestHTTPLibraryPermissionsCSRFAndLivePolicyRevocation', 'TestHTTPLibraryScanBrowseFieldsAndDeletePreservesMedia',
             'TestPostgreSQLRootBindingArchiveMigratesSchema27WithoutInferringApproval'} <= set(tests['passed']),
            'Full verification contains missing, failed, skipped or duplicate tests.')
    packages = report.get('packages')
    require(type(packages) is list and len(packages) == len(set(packages)) == 25 and
            all(type(name) is str and name.startswith('github.com/moooyo/goby/') for name in packages),
            'Full verification does not cover 25 product packages.')
    require(report.get('binary') == source['binary'] and Path(source['binary']['path']) == full_path.parent / 'tmp/goby-linux-amd64',
            'The accepted binary is not the full-run output.')
    binary = protected(Path(source['binary']['path']), source['binary']['sha256'], modes=(0o755,))
    require(len(binary) == source['binary']['bytes'], 'The full-run binary size changed.')
    terminal = decode(protected(Path(source['terminal']['path']), source['terminal']['sha256']))
    require(terminal.get('marker') == 'goby-collection-folder-source55-full-terminal-v1' and terminal.get('status') == 'passed' and
            terminal.get('run_id') == run == FULL_RUN and terminal.get('unit') == FULL_UNIT and
            terminal.get('source') == str(SOURCE) and terminal.get('source_manifest_sha256') == SOURCE_SHA and
            terminal.get('report') == str(FULL_REPORT) and terminal.get('report_sha256') == source['full_report']['sha256'] and
            terminal.get('binary') == source['binary'] and terminal.get('recursive_cgroup_empty') is True,
            'The independent full-run terminal proof is incomplete.')
    terminal_state = terminal.get('state', {})
    require(terminal_state.get('MainPID') == '0' and terminal_state.get('Result') == 'success' and
            terminal_state.get('ExecMainStatus') == '0' and terminal_state.get('SubState') == 'exited' and
            terminal_state.get('InvocationID') == FULL_INVOCATION, 'The full-run controller was not successful and terminal.')
    validate_full_terminal_summary(terminal, report)
    require(canonical(terminal.get('cleanup')) == canonical(report['cleanup']) and
            matches(HASH_RE, terminal.get('finished_receipt_sha256')),
            'The full terminal does not retain the same complete cleanup evidence.')
    protected(FULL_TERMINAL.with_name('finished-receipt.json'), terminal['finished_receipt_sha256'])
    publication = decode(protected(Path(source['publication']['path']), source['publication']['sha256']))
    exact(publication, {'marker', 'commit', 'source_manifest_sha256', 'binary_sha256', 'full_report_sha256', 'full_terminal_sha256'})
    require(publication['marker'] == 'goby-source55-publication-v1' and matches(r'[0-9a-f]{40}', publication['commit']) and
            publication['commit'] != '0' * 40 and publication['source_manifest_sha256'] == SOURCE_SHA and
            publication['binary_sha256'] == source['binary']['sha256'] and
            publication['full_report_sha256'] == source['full_report']['sha256'] and
            publication['full_terminal_sha256'] == source['terminal']['sha256'], 'The publication does not bind the accepted product.')
    web_report = decode(protected(Path(intent['web']['report']['path']), WEB_REPORT_SHA))
    require(web_report.get('status') == 'passed' and all(type(web_report.get('tests', {}).get(key)) is int and
            web_report['tests'][key] == expected for key, expected in (('expected', 68), ('unexpected', 0), ('flaky', 0), ('skipped', 0))),
            'The frontend build prerequisite is not accepted.')
    web_files = web_report.get('dist_files')
    require(type(web_files) is dict and 1 < len(web_files) <= 256 and 'index.html' in web_files, 'The frontend inventory is incomplete.')
    web = verify_tree(WEB_SOURCE, web_files, maximum=64 << 20)
    routes = frontend_routes(protected(WEB_SOURCE / 'index.html', web_files['index.html']), web_files)
    build = decode(protected(Path(intent['helper']['build']['path']), intent['helper']['build']['sha256']))
    exact(build, {'marker', 'source', 'source_manifest_sha256', 'build_root', 'files', 'helper_source_sha256',
                  'binary_sha256', 'go', 'argv', 'environment', 'exit_code'})
    build_root = WORK / ('client-schema28-source55-build-' + intent['run_id'])
    helper_source = intent['helper']['source']['sha256']
    expected_files = {**product_files, 'cmd/client-schema28-migration/main.go': helper_source,
                      'cmd/client-schema28-migration/main_test.go': intent['source_closure']['migrate-client-schema28_test.go']}
    require(build['marker'] == 'goby-client-schema28-helper-build-v1' and build['source'] == str(SOURCE) and
            build['source_manifest_sha256'] == SOURCE_SHA and build['build_root'] == str(build_root) and
            build['files'] == expected_files and build['helper_source_sha256'] == helper_source and
            build['binary_sha256'] == intent['helper']['binary']['sha256'] and type(build['exit_code']) is int and build['exit_code'] == 0,
            'The helper lacks a successful independent source-copy build.')
    descriptor(build['go'], path=Path('/opt/goby-toolchains/go1.27.1/bin/go'))
    require(build['go']['sha256'] == '30969f97169d7f43fe6a085873d75613adc21e30818a8c61d95bd27275df4624' and
            build['argv'] == [build['go']['path'], 'build', '-mod=readonly', '-trimpath', '-buildvcs=false', '-o',
                             str(TOOL / 'migrate-client-schema28'), './cmd/client-schema28-migration/main.go'] and
            build['environment'] == {'GOOS': 'linux', 'GOARCH': 'amd64', 'CGO_ENABLED': '0', 'GOWORK': 'off',
                                    'GOTOOLCHAIN': 'local', 'GOPROXY': 'off', 'GOSUMDB': 'off'},
            'The helper was not built with the declared offline product toolchain.')
    protected(Path(build['go']['path']), build['go']['sha256'], modes=(0o755,))
    verify_tree(build_root, expected_files)
    protected(TOOL / 'migrate-client-schema28', build['binary_sha256'], modes=(0o755,))
    guards = decode(protected(Path(intent['guards']['path']), intent['guards']['sha256']))
    require(guards.get('suite') == 'client-schema28-upgrade-guards' and guards.get('status') == 'passed' and
            guards.get('operator_sha256') == intent['source_closure']['upgrade-client-schema28.py'] and
            guards.get('guard_sha256') == intent['source_closure']['test-upgrade-client-schema28.py'] and
            type(guards.get('test_count')) is int and guards['test_count'] >= 20 and
            guards.get('actual_controller_flow') is True and all(type(guards.get(key)) is int and guards[key] == 0
            for key in ('failures', 'errors', 'skips', 'unexpected_effects')), 'The exact operator guards have not passed.')
    protected(HISTORY, intent['history_sha256'])
    return {'selected': selected, 'catalog': catalog, 'web': web, 'frontend_routes': routes, 'binary': binary, 'publication': publication,
            'full_terminal': terminal, 'source_manifest': manifest, 'build': build}


class Controller:
    """Own the new journal, rehearsal and one candidate service transition."""

    def __init__(self, args):
        self.args = args
        self.intent = self.inputs = self.op = self.state = self.original = self.before = None
        self.intent_sha = self.output = self.private = None
        self.phase_name, self.fence = 'preflight', ServiceFence()
        self.operation_stage, self.master_proof = 'preflight', None
        self.created, self.output_identity = False, None
        self.records, self.locks = {}, []
        self.primary = self.media = self.host = self.global_before = None
        self.state_raw = self.state_identity = self.hba_before = self.hba_after = None
        self.hba_active, self.hba_restore_reserved, self.pair_mutations = False, False, False
        self.db = self.backup_report = self.helper_intent = None
        self.journal_sequence, self.command_sequence = 0, 0

    def command(self, arguments, *, stdin=None, env=None, timeout=30):
        arguments = [str(value) for value in arguments]
        self.fence.approve(arguments)
        require(0 < timeout <= 600, 'A command exceeds its fixed time budget.')
        try:
            result = subprocess.run(arguments, input=stdin, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                    env=env or ENV, timeout=timeout, check=False)
        except (OSError, subprocess.TimeoutExpired):
            raise Failure('A bounded command failed or timed out; no mutation will be retried.', 'command_timeout') from None
        self.command_sequence += 1
        if result.returncode != 0:
            code = helper_failure_code(result.stdout) if arguments and arguments[0] == str(TOOL / 'migrate-client-schema28') else 'command_failed'
            if self.created:
                # Tool and database errors may contain private source values.
                self.save('command-failure-' + str(self.command_sequence) + '.json',
                          {'exit': result.returncode, 'operation_stage': self.safe_operation_stage(), 'error_code': code,
                           'stdout_sha256': sha(result.stdout), 'stderr_sha256': sha(result.stderr)})
            raise Failure('A bounded command failed; the scope is retained without retry.', code)
        require(len(result.stdout) <= 64 << 20 and len(result.stderr) <= 8 << 20, 'A command output exceeds its bound.')
        return result.stdout.strip()

    def legacy_command(self, arguments, text=None, environment=None, timeout=30):
        args = [str(value) for value in arguments]
        if args and args[0] == '/usr/bin/systemctl':
            require(len(args) == 5 and args[1] == 'show' and args[2] in (UNIT, PRIMARY_UNIT, self.intent['controller']['unit'],
                    self.inputs['full_terminal']['unit']) and args[3] == '--no-pager' and args[4].startswith('--property='),
                    'A legacy service read escaped its declared unit.')
        elif args and args[0] == '/usr/bin/ss':
            require(args == ['/usr/bin/ss', '-H', '-ltnp', 'sport = :18198'], 'A legacy listener read escaped the candidate.')
        elif args[:6] == ['/usr/sbin/runuser', '-u', 'goby', '--', '/usr/bin/test', '-r']:
            require(len(args) == 7 and Path(args[6]).is_relative_to(self.op.MEDIA_ROOT) and '..' not in Path(args[6]).parts,
                    'A readability check escaped the original receipted media root.')
        else:
            require(args[:5] == ['/usr/sbin/runuser', '-u', 'postgres', '--', str(PG / 'psql')] and
                    '-d' in args and args[args.index('-d') + 1] in ('postgres', 'goby_client_m3e') and
                    args[args.index('-p') + 1] == '15432' and text is not None,
                    'A legacy read escaped the exact candidate cluster.')
            environment = dict(environment or ENV, PGOPTIONS='-c default_transaction_read_only=on')
        return self.command(args, stdin=None if text is None else text.encode(), env=environment, timeout=min(timeout, 60)).decode()

    def properties(self, unit, names):
        require(unit in (UNIT, PRIMARY_UNIT, self.intent['controller']['unit'], self.inputs['full_terminal']['unit']),
                'A service read escaped the declared units.')
        raw = self.command(['/usr/bin/systemctl', 'show', unit, '--no-pager', '--property=' + ','.join(names)]).decode()
        result = {}
        for line in raw.splitlines():
            name, separator, value = line.partition('=')
            require(separator and name not in result, 'Service properties contain duplicate or malformed fields.')
            result[name] = value
        require(set(result) == set(names), 'Service properties omitted a requested field.')
        return result

    def db_command(self, arguments, *, stdin=None, env=None, timeout=20):
        args = [str(value) for value in arguments]
        executable = str(PG / 'psql')
        require(executable in args and '-d' in args and '-p' in args and args[args.index('-p') + 1] == '15432' and
                type(stdin) is bytes, 'A rehearsal command escaped the pinned PostgreSQL client.')
        target = args[args.index('-d') + 1]
        require(target in ('postgres', self.db.name), 'A rehearsal query selected another database.')
        if target == 'postgres':
            require(args[:4] == ['/usr/sbin/runuser', '--user', 'postgres', '--'], 'Maintenance is not peer authenticated.')
            sql = stdin.decode().strip()
            mutations = self.allowed_pair_sql()
            if not sql.startswith('SELECT '):
                require(self.created and self.pair_mutations and sql in mutations,
                        'A maintenance mutation is not part of this exact new pair.')
            else:
                env = dict(env or ENV, PGOPTIONS='-c default_transaction_read_only=on')
        else:
            env = dict(env or ENV, PGOPTIONS='-c default_transaction_read_only=on')
        return self.command(args, stdin=stdin, env=env, timeout=min(timeout, 30))

    def allowed_pair_sql(self):
        if self.db is None:
            return set()
        name, role, tag, password = self.db.name, self.db.role, self.db.tag, self.password
        return {
            f"BEGIN; SET LOCAL password_encryption='scram-sha-256'; CREATE ROLE {role} LOGIN NOINHERIT "
            f"NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 12 PASSWORD '{password}'; "
            f"COMMENT ON ROLE {role} IS '{tag}'; COMMIT;",
            f"CREATE DATABASE {name} OWNER {role} TEMPLATE template0 ENCODING 'UTF8';",
            f"COMMENT ON DATABASE {name} IS '{tag}'; REVOKE CONNECT,TEMPORARY ON DATABASE {name} FROM PUBLIC;",
            f'ALTER DATABASE {name} ALLOW_CONNECTIONS false;', f'DROP DATABASE {name};', f'DROP ROLE {role};',
        }

    def workspace_command(self, arguments, input_text=None, environment=None, timeout=20):
        args = [str(value) for value in arguments]
        if args == ['/usr/bin/findmnt', '--json', '--target', '/var/lib/postgresql', '--output', 'SOURCE,FSTYPE,TARGET,OPTIONS']:
            pass
        elif len(args) == 2 and Path(args[0]).parent == PG and args[1] == '--version':
            require(Path(args[0]).name in self.workspace.TOOLS, 'An unlisted PostgreSQL executable was requested.')
        elif args == ['/usr/bin/ss', '-H', '-ltnp', 'sport = :15432']:
            pass
        else:
            require(str(PG / 'psql') in args and '-d' in args and args[args.index('-d') + 1] == 'postgres' and
                    args[args.index('-p') + 1] == '15432' and input_text is not None,
                    'A workspace read escaped the exact maintenance connection.')
            # The frozen helper uses env -i; inject the read-only option after it.
            if '/usr/bin/env' in args:
                args.insert(args.index('-i') + 1, 'PGOPTIONS=-c default_transaction_read_only=on')
            else:
                environment = dict(environment or ENV, PGOPTIONS='-c default_transaction_read_only=on')
        return self.command(args, stdin=None if input_text is None else input_text.encode(), env=environment,
                            timeout=min(timeout, 60)).decode()

    def primary_fact(self):
        return self.base.Upgrade.primary_fact(self)

    def safe_operation_stage(self):
        return self.operation_stage if type(self.operation_stage) is str and self.operation_stage in OPERATION_STAGES else 'unknown'

    def check_master(self):
        require(self.master_proof is not None, 'The master-key baseline has not been established.', 'master_state_invalid')
        validate_master_runtime(protected(RUNTIME))
        if self.master_proof['state'] == 'absent':
            require(not present(MASTER), 'A master key appeared in the preserved empty-key scope.', 'master_unexpected_presence')
        else:
            require(present(MASTER), 'The existing master key disappeared.', 'master_unexpected_absence')
            raw = protected(MASTER, self.master_proof['sha256'], uid=995, gid=986, modes=(0o600,), limit=32)
            require(len(raw) == 32, 'The existing master key changed size.', 'master_state_changed')

    def save(self, name, value):
        require(self.created and directory(self.output) == self.output_identity and
                matches(r'[a-z0-9][a-z0-9.-]{0,95}', name), 'An evidence write escaped the owned output.')
        raw = value if type(value) is bytes else canonical(value)
        path = self.output / name
        create(path, raw)
        self.records[name] = {'path': str(path), 'sha256': sha(raw)}
        return self.records[name]

    def write_state(self, value, phase):
        require(protected(STATE) == self.state_raw and identity(STATE.lstat()) == self.state_identity,
                'The fixture state changed outside this operation.')
        path = self.output / ('state-' + phase.replace('_', '-') + '.json')
        raw = canonical(value)
        create(path, raw)
        require(path.stat().st_dev == STATE.parent.stat().st_dev, 'State publication would cross filesystems.')
        require(protected(STATE) == self.state_raw and identity(STATE.lstat()) == self.state_identity,
                'The fixture state changed before atomic publication.')
        os.replace(path, STATE)
        sync(WORK)
        require(protected(STATE) == raw, 'The atomically published state differs.')
        self.state, self.state_raw, self.state_identity = copy.deepcopy(value), raw, identity(STATE.lstat())

    def phase(self, name):
        allowed = ('backed_up', 'rehearsed', 'stop_requested', 'stopped', 'migrate_requested', 'migrated',
                   'replace_requested', 'replaced', 'start_requested')
        require(name in allowed, 'An undeclared migration phase was requested.')
        self.save('phase-' + name.replace('_', '-') + '.json', {'marker': MARKER, 'run_id': self.intent['run_id'],
                  'phase': name, 'reserved_service_actions': sorted(self.fence.reserved)})
        if name in allowed[2:]:
            value = copy.deepcopy(self.state)
            value.update(phase='upgrading', stage='schema28_' + name)
            value['schema28_upgrade'] = {'marker': MARKER, 'phase': name, 'output': str(self.output), 'intent_sha256': self.intent_sha}
            self.write_state(value, name)
        self.phase_name, self.fence.stage = name, name

    def journal_pair(self):
        self.journal_sequence += 1
        self.save('pair-%03d.json' % self.journal_sequence, {'marker': MARKER, 'run_id': self.intent['run_id'],
                  'intent_sha256': self.intent_sha, 'pair': self.db.pair, 'hba_active': self.hba_active})

    def check_cluster(self, hba):
        workspace, postgres = self.workspace, self.postgres
        require(protected(workspace.OWNER) == self.owner_raw, 'The workspace owner record changed.')
        require(workspace.verify_process(postgres, self.owner) == self.owner['process'], 'The cluster process changed.')
        workspace.verify_server(self.owner)
        require(protected(HBA, uid=postgres.pw_uid, gid=postgres.pw_gid, modes=(0o600,)) == hba,
                'The HBA changed outside its owned transition.')
        for path, expected in self.cluster_files.items():
            require(protected(Path(path), uid=postgres.pw_uid, gid=postgres.pw_gid, modes=(0o600,)) == expected,
                    'A PostgreSQL configuration input changed.')
        protected(HISTORY, self.intent['history_sha256'])

    def check(self, live=True):
        require(protected(STATE) == self.state_raw and identity(STATE.lstat()) == self.state_identity,
                'The candidate control state changed outside this scope.')
        self.op.verify_database(self.state)
        self.op.verify_fixture_directories(self.state)
        self.check_master()
        if live:
            require(self.op.verify_service(self.state) == self.state['process'], 'The candidate process changed.')
        require(self.primary_fact() == self.primary and self.ext.media_witness(self.op, self.state) == self.media,
                'The primary or a receipted media group changed.')
        require(self.dbtools.host_fact() == self.host, 'The host mount namespace or mount table changed.')
        verify_tree(Path('/opt/goby-dev/admin'), self.shared_web['files'], maximum=64 << 20)
        require(directory(Path('/opt/goby-dev/admin'), 0o755) == self.shared_web['root_identity'], 'The primary web root changed.')
        self.check_cluster(self.hba_after if self.hba_active else self.hba_before)
        self.verify_full_terminal()
        for name, digest in self.intent['source_closure'].items():
            protected(TOOL / name, digest, modes=(0o600, 0o644, 0o755))

    def snapshot(self, schema=27):
        value = self.op.preservation_snapshot(self.state, schema)
        if schema == 27:
            self.profile.validate_structure(self.op, value, self.original)
        validate_population(value)
        if self.master_proof is not None:
            require(canonical(validate_master_baseline(value, protected(RUNTIME))) == canonical(self.master_proof),
                    'The captured master-key state changed.', 'master_state_changed')
        return value

    def assert_baseline(self, name, live=True):
        self.check(live=live)
        value = self.snapshot()
        compare_baseline(self.before, value)
        self.save(name, value)
        if name in ('pre-stop-full.json', 'stopped-full.json'):
            self.helper('inspect', baseline=True, expected_schema=27,
                        evidence_name='helper-pre-stop.json' if live else 'helper-stopped.json')
        return value

    def verify_full_terminal(self):
        expected = self.inputs['full_terminal']['state']
        require(type(expected) is dict and all(type(key) is str and matches(r'[A-Za-z][A-Za-z0-9]*', key) for key in expected),
                'The accepted full terminal property names are invalid.')
        require(self.properties(FULL_UNIT, tuple(expected)) == expected and expected.get('InvocationID') == FULL_INVOCATION,
                'The accepted full controller no longer has its independently recorded terminal identity.')
        self.dbtools.cgroup_empty(FULL_UNIT)

    def prepare(self):
        require(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0 and os.environ.get('SSH_CONNECTION'),
                'Use authorized root SSH on test-env with its authentic connection forwarded to the controller.')
        self.intent, self.intent_sha = load_intent(self.args.input, self.args.input_sha256)
        self.output, self.private = Path(self.intent['output']), Path(self.intent['output']) / 'private'
        self.inputs = verify_inputs(self.intent)
        for name, (path, digest) in HELPERS.items():
            setattr(self, name, load_module(path, digest, name))
        self.op.command = self.legacy_command
        require(self.op.UNIT == UNIT and self.op.BINARY == BINARY and self.op.PORT == 18198 and self.op.PG_PORT == 15432 and
                self.op.ROLE == 'goby_client_m3e', 'The frozen candidate reader addresses another scope.')
        self.op.host_inputs(initial=False)
        require(not present(self.output) and not present(WEB), 'A new output or candidate frontend root already exists.')
        self.state_raw = protected(STATE, STATE_SHA)
        self.state_identity = identity(STATE.lstat())
        self.original = self.state = decode(self.state_raw)
        require(self.state.get('marker') == self.op.MARKER and self.state.get('schema') == 27 and
                self.state.get('phase') == 'ready' and self.state.get('stage') == 'complete' and
                self.state.get('binary_sha256') == OLD_BINARY_SHA and self.state.get('runtime_sha256') == RUNTIME_SHA and
                self.state.get('process') == OLD_PROCESS and self.state.get('notifications_upgrade', {}).get('phase') == 'complete' and
                'schema28_upgrade' not in self.state and 'schema28_source' not in self.state,
                'The current candidate is not the sealed completed source44 fixture.')
        require(self.op.schema27_binding(self.state).get('source') == str(WORK / 'source-attempt-44'),
                'The candidate no longer identifies source44.')
        require(self.properties(UNIT, ('MainPID', 'InvocationID', 'ActiveState', 'SubState')) ==
                {'MainPID': str(OLD_PROCESS['pid']), 'InvocationID': OLD_INVOCATION, 'ActiveState': 'active', 'SubState': 'running'},
                'The exact current candidate invocation changed.')
        protected(RUNTIME, RUNTIME_SHA)
        self.runtime_before = protected(RUNTIME, RUNTIME_SHA)
        self.runtime_after = patch_runtime(self.runtime_before)
        require(self.op.verify_service(self.state) == OLD_PROCESS, 'The live candidate differs from its pinned state.')
        self.primary = self.primary_fact()
        assets = {}
        for path in sorted(Path('/opt/goby-dev/admin').rglob('*')):
            info = path.lstat()
            if stat.S_ISREG(info.st_mode):
                assets[path.relative_to('/opt/goby-dev/admin').as_posix()] = sha(protected(path, modes=(0o644,)))
            else:
                require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                        'The primary frontend contains a special or untrusted entry.')
        self.shared_web = verify_tree(Path('/opt/goby-dev/admin'), assets, maximum=64 << 20)
        self.host = self.dbtools.host_fact()
        path = SOURCE / SELECTED[0]
        self.workspace = load_module(path, sha(self.inputs['selected'][SELECTED[0]]), 'workspace')
        self.workspace.command = self.workspace_command
        self.postgres = pwd.getpwnam('postgres')
        self.workspace.verify_parents(self.postgres)
        # Every resource operation uses this one order; locks are nonblocking.
        self.locks.append(self.workspace.acquire_lock())
        lock = os.open(self.op.LOCK, os.O_RDONLY | os.O_NOFOLLOW)
        try:
            require(identity(os.fstat(lock)) == identity(self.op.LOCK.lstat()), 'The candidate lock was replaced.')
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BaseException:
            os.close(lock)
            raise
        self.locks.append(lock)
        self.owner_raw = protected(self.workspace.OWNER)
        self.owner = decode(self.owner_raw)
        self.workspace.validate_owner(self.owner, self.workspace.expected_owner(self.postgres, self.workspace.binaries()),
                                      self.workspace.directory(CONTROL, 0, 0))
        self.workspace.verify_directories(self.postgres, self.owner)
        self.workspace.verify_configuration(self.postgres)
        self.hba_before = protected(HBA, uid=self.postgres.pw_uid, gid=self.postgres.pw_gid, modes=(0o600,))
        require(self.hba_before == self.workspace.HBA.encode(), 'The workspace HBA is not its exact baseline.')
        self.cluster_files = {str(PGDATA / name): protected(PGDATA / name, uid=self.postgres.pw_uid, gid=self.postgres.pw_gid,
                             modes=(0o600,)) for name in ('postgresql.conf', 'postgresql.auto.conf', 'pg_ident.conf')}
        self.password = secrets.token_hex(32)
        name = 'goby_client_s55_rehearsal_' + self.intent['run_id'].replace('_', '')
        self.db = self.dbtools.Database({'run_id': self.intent['run_id'], 'database': name, 'role': name},
                                       {'selected': self.inputs['selected'], 'catalog': self.inputs['catalog']}, self.password)
        self.db.tag = MARKER + ':' + self.intent['run_id']
        self.dbtools.command = self.db_command
        self.db.journal = self.journal_pair
        require(self.db.rows() == {'role': None, 'database': None}, 'The new rehearsal names already exist.')
        self.global_before = decode(self.db.maintenance(self.dbtools.GLOBAL_SQL))
        self.before = self.snapshot()
        self.master_proof = validate_master_baseline(self.before, self.runtime_before)
        require(self.op.DATA == MASTER.parent, 'The candidate data root differs from its master-key path.', 'master_runtime_path_invalid')
        self.check_master()
        compare_baseline(decode(protected(BASELINE, BASELINE_SHA)), self.before)
        self.media = self.ext.media_witness(self.op, self.state)
        self.check()
        report_packages = decode(protected(Path(self.intent['source']['full_report']['path']),
                                          self.intent['source']['full_report']['sha256']))['packages']
        require(set(report_packages) == self.op.PRODUCT_PACKAGES | {'github.com/moooyo/goby/internal/storagebinding'},
                'The full verification package set differs from the product.')
        if self.args.mode == 'run':
            self.admit_controller()

    def admit_controller(self):
        names = ('MainPID', 'InvocationID', 'ActiveState', 'SubState', 'Description', 'ControlGroup', 'MemoryMax',
                 'MemorySwapMax', 'CPUQuotaPerSecUSec', 'TasksMax', 'RuntimeMaxUSec', 'KillMode', 'RemainAfterExit')
        unit = self.intent['controller']['unit']
        value = self.properties(unit, names)
        require(value['MainPID'] == str(os.getpid()) and matches(r'[0-9a-f]{32}', value['InvocationID']) and
                os.environ.get('INVOCATION_ID') == value['InvocationID'] and value['ActiveState'] in ('activating', 'active') and
                value['Description'] == MARKER + ':' + self.intent['run_id'] and value['ControlGroup'] == '/system.slice/' + unit and
                value['MemoryMax'] == str(2 << 30) and value['MemorySwapMax'] == '0' and value['CPUQuotaPerSecUSec'] == '1.500000s' and
                value['TasksMax'] == '128' and value['RuntimeMaxUSec'] == '20min' and value['KillMode'] == 'control-group' and
                value['RemainAfterExit'] == 'yes', 'The caller is not the exact bounded fresh controller.')
        self.controller = {'unit': unit, 'invocation_id': value['InvocationID'], 'process': self.dbtools.process_fact(os.getpid())}
        require(self.controller['process']['cgroup'] == '/system.slice/' + unit, 'The controller process belongs to another cgroup.')

    def begin(self):
        self.operation_stage = 'output_setup'
        require(not present(self.output), 'The one-shot output is already occupied.')
        self.output.mkdir(mode=0o700)
        sync(WORK)
        self.created = True
        self.output_identity = directory(self.output)
        self.private.mkdir(mode=0o700)
        sync(self.output)
        self.save('intent.json', self.intent)
        self.save('controller.json', self.controller)
        self.save('before-state.json', self.state_raw)
        self.save('before-full.json', self.before)
        self.save('primary-before.json', self.primary)
        self.save('shared-web-before.json', self.shared_web)
        self.save('media-before.json', self.media)
        self.save('host-before.json', self.host)
        self.save('catalog-before.json', self.global_before)
        self.save('hba-before', self.hba_before)
        self.save('cluster-before.json', {'owner_sha256': sha(self.owner_raw),
                  'configuration': {path: sha(raw) for path, raw in self.cluster_files.items()}})

    def backup_materials(self):
        self.operation_stage = 'backup_materials_fixed'
        self.check_master()
        materials = self.private / 'materials'
        materials.mkdir(mode=0o700)
        copied = {}
        fixed = {'runtime.env': RUNTIME, 'browser.json': self.op.BROWSER, 'goby-av-browser.json': self.op.AV_BROWSER,
                 'goby-av-credentials.json': self.op.AV_ROOT / 'credentials.json', 'candidate.service': self.op.UNIT_FILE,
                 'source44.bin': BINARY}
        for name, path in fixed.items():
            raw = protected(path, modes=(0o600, 0o644, 0o755))
            create(materials / name, raw)
            copied[name] = {'source': str(path), 'sha256': sha(raw), 'bytes': len(raw)}
        self.operation_stage = 'backup_materials_recovery'
        for name, entry in self.before['recovery'].items():
            if entry.get('directory'):
                continue
            relative = Path(name)
            require(not relative.is_absolute() and '..' not in relative.parts, 'A recovery material path escapes its root.')
            source = self.op.DATA / relative
            raw = protected(source, entry['sha256'], uid=995, gid=986, modes=(0o600,))
            target = materials / 'application' / relative
            parents = []
            parent = target.parent
            while parent != materials:
                parents.append(parent)
                parent = parent.parent
            for parent in reversed(parents):
                if not present(parent):
                    parent.mkdir(mode=0o700)
                    sync(parent.parent)
            create(target, raw)
            copied['application/' + relative.as_posix()] = {'source': str(source), 'sha256': sha(raw), 'bytes': len(raw)}
        self.operation_stage = 'backup_materials_master'
        self.check_master()
        require(copied['runtime.env']['sha256'] == RUNTIME_SHA and copied['source44.bin']['sha256'] == OLD_BINARY_SHA and
                copied['browser.json']['sha256'] == self.before['browser_sha256'],
                'A necessary private backup material is missing or changed.', 'backup_materials_incomplete')
        if self.master_proof['state'] == 'present':
            require(copied.get('application/master.key') == {'source': str(MASTER), 'sha256': self.master_proof['sha256'], 'bytes': 32},
                    'The existing master-key backup is missing or changed.', 'backup_materials_incomplete')
        else:
            require('application/master.key' not in copied and not present(materials / 'application/master.key'),
                    'An absent master key must not be created in the backup.', 'master_unexpected_presence')
        self.save('master-proof.json', self.master_proof)
        create(self.private / 'materials.json', canonical(copied))
        self.materials = copied

    def helper_document(self):
        self.operation_stage = 'helper_intent'
        values = re.findall(rb"(?m)^GOBY_DATABASE_URL='([^'\r\n]+)'$", self.runtime_before)
        require(len(values) == 1, 'The candidate database URL is missing or ambiguous.')
        url = values[0].decode('ascii')
        parsed = urllib.parse.urlsplit(url)
        require(parsed.scheme in ('postgres', 'postgresql') and parsed.hostname == '127.0.0.1' and parsed.port == 15432 and
                parsed.username == 'goby_client_m3e' and parsed.password and parsed.path == '/goby_client_m3e' and
                parsed.query == 'sslmode=disable' and not parsed.fragment, 'The private candidate URL addresses another database.')
        candidate = decode(self.op.postgres("SELECT jsonb_build_object('database_oid',(SELECT oid::bigint FROM pg_database WHERE datname=current_database()),"
            "'role_oid',(SELECT oid::bigint FROM pg_roles WHERE rolname='goby_client_m3e'),'schema_oid',n.oid::bigint,"
            "'schema_owner_oid',n.nspowner::bigint,'recovery_deployment_id',"
            "(SELECT (value::jsonb)->>'deploymentId' FROM public.server_settings WHERE key='goby.recovery.binding.v1')) "
            "FROM pg_namespace n WHERE n.nspname='public';", 'goby_client_m3e').encode())
        require(candidate['database_oid'] == self.state['database_oid'] and candidate['role_oid'] == self.state['role_oid'] and
                matches(r'[0-9a-f]{32}', candidate['recovery_deployment_id']), 'The helper candidate binding is invalid.')
        candidate.update(database='goby_client_m3e', role='goby_client_m3e')
        facts = decode(self.op.postgres("SELECT jsonb_build_object('postmaster_start_microseconds',"
            "(extract(epoch FROM pg_postmaster_start_time())*1000000)::bigint,'postgresql_version_num',"
            "current_setting('server_version_num')::integer);").encode())
        cluster = {'system_identifier': self.owner['system_identifier'], 'postmaster_pid': self.owner['process']['pid'],
                   'postmaster_start_ticks': str(self.owner['process']['start_ticks']), 'boot_id': self.owner['process']['boot_id'],
                   'data_directory': str(PGDATA), 'port': 15432, **facts}
        value = {'marker': 'goby-client-schema28-helper-intent-v1', 'version': 1, 'run_id': self.intent['run_id'],
                 'source_manifest_sha256': SOURCE_SHA, 'source55_binary_sha256': self.intent['source']['binary']['sha256'],
                 'helper_sha256': self.intent['helper']['binary']['sha256'], 'evidence_root': str(self.output),
                 'catalog27': {'path': str(SOURCE / SELECTED[2]), 'sha256': CATALOG27_SHA},
                 'catalog28': {'path': str(SOURCE / SELECTED[3]), 'sha256': CATALOG28_SHA},
                 'cluster': cluster, 'candidate': candidate,
                 'rehearsal': {'database': self.db.name, 'role': self.db.role, 'owner_tag': self.db.tag}}
        self.helper_intent = value
        create(self.private / 'helper-intent.json', canonical(value))
        create(self.private / 'helper-credentials.json', canonical({'candidate_url': url,
            'rehearsal_url': f'postgresql://{self.db.role}:{self.password}@127.0.0.1:15432/{self.db.name}?sslmode=disable'}))
        return value

    def helper(self, mode, *, baseline=False, expected_schema=None, target=False, evidence_name=None):
        allowed = {'backup': 'preflight', 'rehearse': 'backed_up', 'migrate': 'migrate_requested'}
        if mode == 'inspect':
            require((self.phase_name, evidence_name) in (('rehearsed', 'helper-pre-stop.json'), ('stopped', 'helper-stopped.json')),
                    'A baseline inspection is outside its declared phase.')
        else:
            require(mode in allowed and self.phase_name == allowed[mode] and evidence_name is None,
                    'A helper mutation is out of its declared phase.')
        self.operation_stage = 'helper_' + mode
        self.check_master()
        name = evidence_name or ('helper-' + mode + '.json')
        path = self.private / name
        require(not present(path), 'A helper output is already occupied; the invocation cannot be replayed.')
        helper = TOOL / 'migrate-client-schema28'
        protected(helper, self.intent['helper']['binary']['sha256'], modes=(0o755,))
        intent_raw = protected(self.private / 'helper-intent.json')
        require(decode(intent_raw) == self.helper_intent, 'The private helper intent changed.')
        args = [str(helper), '--mode', mode, '--intent', str(self.private / 'helper-intent.json'),
                '--intent-sha256', sha(intent_raw), '--credentials', str(self.private / 'helper-credentials.json'), '--output', str(path)]
        if expected_schema is not None:
            args += ['--expected-schema', str(expected_schema)]
        if baseline:
            args += ['--baseline', str(self.private / 'helper-backup.json'), '--baseline-sha256', self.backup_sha]
        if target:
            target_path = self.private / 'helper-target.json'
            args += ['--target-receipt', str(target_path), '--target-receipt-sha256', sha(protected(target_path))]
        self.command(args, timeout=600)
        self.check_master()
        raw = protected(path, modes=(0o600,))
        result = decode(raw)
        require(result.get('mode') == mode and result.get('run_id') == self.intent['run_id'] and
                result.get('intent_sha256') == sha(intent_raw) and
                result.get('status') == {'backup': 'backed_up', 'rehearse': 'rehearsed', 'migrate': 'committed', 'inspect': 'inspected'}[mode] and
                matches(HASH_RE, result.get('state_sha256')), 'The helper did not publish its exact successful acknowledgement.')
        require(result.get('marker') == 'goby-client-schema28-helper-v1' and type(result.get('version')) is int and result['version'] == 1 and
                result.get('source_manifest_sha256') == SOURCE_SHA and result.get('source55_binary_sha256') == self.intent['source']['binary']['sha256'] and
                result.get('helper_sha256') == self.intent['helper']['binary']['sha256'], 'The helper result source binding differs.')
        if baseline and mode != 'inspect':
            require(result.get('baseline_sha256') == self.backup_sha and result.get('root_bindings_no_auto_binding') is True and
                    result.get('historical_audit_defaults') is True and
                    result.get('before_state_sha256') == result.get('preserved_state_sha256'),
                    'The helper preservation or neutral-default proof is incomplete.')
        if baseline and mode == 'inspect':
            require(result['state_sha256'] == self.backup_report['state_sha256'], 'The live candidate differs from the exported backup snapshot.')
        self.records[name] = {'path': str(path), 'sha256': sha(raw)}
        return result

    def backup(self):
        self.helper_document()
        self.backup_report = self.helper('backup', expected_schema=27)
        require(self.backup_report.get('source_schema_version') == 27 and self.backup_report.get('same_exported_snapshot') is True,
                'Backup facts and dump do not share the held schema27 snapshot.')
        dump = self.backup_report.get('dump', {})
        require(dump.get('path') == str(self.private / 'candidate-schema27.dump') and type(dump.get('bytes')) is int and
                0 < dump['bytes'] <= 256 << 20 and matches(HASH_RE, dump.get('sha256')), 'The backup archive descriptor is invalid.')
        require(len(protected(Path(dump['path']), dump['sha256'], limit=256 << 20)) == dump['bytes'], 'The backup archive changed.')
        self.backup_sha = sha(protected(self.private / 'helper-backup.json'))
        self.operation_stage = 'backup_baseline'
        self.assert_baseline('backup-current-full.json')
        self.phase('backed_up')

    def verify_global(self):
        current = decode(self.db.maintenance(self.dbtools.GLOBAL_SQL))
        filtered = copy.deepcopy(current)
        for key, oid_key in (('roles', 'role_oid'), ('databases', 'database_oid')):
            selected = [row for row in current[key] if row['name'] == self.db.name]
            oid = self.db.pair.get(oid_key)
            require((len(selected) == 1 and str(selected[0]['oid']) == str(oid)) if oid is not None else not selected,
                    'A global catalog row does not belong to the recorded new pair.')
            filtered[key] = [row for row in current[key] if row['name'] != self.db.name]
        require(filtered == self.global_before, 'An unrelated global role, database, membership or setting changed.')

    def reload_hba(self, expected):
        self.check_cluster(expected)
        require(self.db.maintenance('SELECT pg_reload_conf();') == b't' and
                self.db.maintenance('SELECT count(*) FROM pg_hba_file_rules WHERE error IS NOT NULL;') == b'0',
                'The owned HBA reload failed.')
        self.check_cluster(expected)

    def restore_hba(self):
        if self.hba_active:
            require(not self.hba_restore_reserved, 'The HBA restoration was already attempted; do not retry it.')
            self.hba_restore_reserved = True
            self.check_cluster(self.hba_after)
            self.dbtools.replace_file(HBA, self.hba_after, self.hba_before, uid=self.postgres.pw_uid, gid=self.postgres.pw_gid)
            self.reload_hba(self.hba_before)
            self.hba_active = False
            self.journal_pair()

    def rehearse(self):
        self.operation_stage = 'rehearsal'
        self.check()
        require(self.global_before == decode(self.db.maintenance(self.dbtools.GLOBAL_SQL)), 'The pre-rehearsal global catalog changed.')
        self.pair_mutations = True
        self.db.create()
        self.verify_global()
        self.hba_after = (f'# {self.db.tag}\nhost {self.db.name} {self.db.role} 127.0.0.1/32 scram-sha-256\n'.encode() + self.hba_before)
        self.save('hba-rehearsal', self.hba_after)
        # Publish the transition intent before replacing the shared HBA bytes.
        self.hba_active = True
        self.journal_pair()
        self.dbtools.replace_file(HBA, self.hba_before, self.hba_after, uid=self.postgres.pw_uid, gid=self.postgres.pw_gid)
        self.reload_hba(self.hba_after)
        public = self.db.pair['public']
        target = {'marker': 'goby-client-schema28-rehearsal-target-v1', 'version': 1, 'run_id': self.intent['run_id'],
                  'intent_sha256': sha(protected(self.private / 'helper-intent.json')), 'database': self.db.name, 'role': self.db.role,
                  'database_oid': self.db.pair['database_oid'], 'role_oid': self.db.pair['role_oid'], 'schema_oid': public['oid'],
                  'schema_owner_oid': int(self.db.maintenance("SELECT oid::bigint FROM pg_roles WHERE rolname='pg_database_owner';")),
                  'owner_tag': self.db.tag}
        create(self.private / 'helper-target.json', canonical(target))
        self.helper('rehearse', baseline=True, target=True)
        self.operation_stage = 'rehearsal_readback'
        self.db.verify_owned()
        self.verify_global()
        rehearsal_snapshot = self.db.snapshot()
        self.save('rehearsal-schema28.json', rehearsal_snapshot)
        self.operation_stage = 'rehearsal_hba_restore'
        self.restore_hba()
        self.operation_stage = 'rehearsal_dispose'
        self.db.dispose(rehearsal_snapshot)
        self.pair_mutations = False
        require(self.db.pair['phase'] == 'removed' and self.global_before == decode(self.db.maintenance(self.dbtools.GLOBAL_SQL)),
                'The rehearsal did not finish ordinary cleanup with global preservation.')
        self.save('rehearsal-completed.json', {'marker': MARKER, 'run_id': self.intent['run_id'], 'backup_sha256': self.backup_sha,
                  'pair': self.db.pair, 'hba_restored_exactly': True, 'global_catalog_preserved': True, 'normal_drop_only': True})
        self.phase('rehearsed')

    def service(self, action):
        require(action in ('stop', 'start'), 'An undeclared service action was requested.')
        self.operation_stage = 'candidate_' + action
        if action == 'stop':
            require(self.op.verify_service(self.state) == OLD_PROCESS and
                    self.properties(UNIT, ('InvocationID',))['InvocationID'] == OLD_INVOCATION, 'The candidate changed before its only stop.')
        else:
            require(action == 'start', 'An undeclared service action was requested.')
            self.op.require_candidate_stopped()
        self.command(['/usr/bin/systemctl', action, UNIT], timeout=55)
        if action == 'stop':
            self.op.require_candidate_stopped()
            return
        deadline = time.monotonic() + 50
        while time.monotonic() < deadline:
            status = self.properties(UNIT, ('MainPID', 'ActiveState'))
            require(status['ActiveState'] not in ('failed', 'inactive'), 'The replacement exited during startup.')
            if self.command(['/usr/bin/ss', '-H', '-ltnp', 'sport = :18198']):
                break
            time.sleep(0.5)
        self.state['start_pending'] = True
        observed = self.op.verify_service(self.state, allow_new=True)
        require(observed is not None and observed != OLD_PROCESS and observed['boot_id'] == OLD_PROCESS['boot_id'] and
                observed['start_ticks'] > OLD_PROCESS['start_ticks'], 'The new candidate process is not fresh.')
        value = copy.deepcopy(self.state)
        value.update(process=observed, start_pending=False)
        self.write_state(value, 'started')
        self.new_service = self.properties(UNIT, ('MainPID', 'InvocationID', 'ActiveState', 'SubState'))
        require(self.new_service['MainPID'] == str(observed['pid']) and self.new_service['InvocationID'] != OLD_INVOCATION and
                matches(r'[0-9a-f]{32}', self.new_service['InvocationID']) and self.new_service['ActiveState'] == 'active' and
                self.new_service['SubState'] == 'running', 'The replacement lacks its own active invocation.')

    def migrate(self):
        self.phase('migrate_requested')
        self.op.require_candidate_stopped()
        self.helper('migrate', baseline=True, expected_schema=27)
        self.operation_stage = 'migration_readback'
        after = self.snapshot(28)
        compare_migrated(self.before, after, self.inputs['catalog'], runtime_sha256=RUNTIME_SHA)
        self.save('migrated-full.json', after)
        self.phase('migrated')

    def install(self):
        self.operation_stage = 'install'
        self.phase('replace_requested')
        self.check(live=False)
        self.op.require_candidate_stopped()
        require(not present(WEB), 'The new candidate frontend path became occupied.')
        WEB.mkdir(mode=0o755)
        os.chmod(WEB, 0o755)
        sync(INSTALL)
        for name, digest in self.inputs['web']['files'].items():
            relative = Path(name)
            require(not relative.is_absolute() and '..' not in relative.parts, 'An asset path escaped the candidate web root.')
            target = WEB / relative
            if target.parent != WEB and not present(target.parent):
                require(target.parent.parent == WEB, 'An asset requires an undeclared directory depth.')
                target.parent.mkdir(mode=0o755)
                os.chmod(target.parent, 0o755)
                sync(WEB)
            create(target, protected(WEB_SOURCE / relative, digest), 0o644)
        verify_candidate_web(self.inputs['web']['files'])
        staged = INSTALL / ('goby.schema28-' + self.intent['run_id'])
        create(staged, self.inputs['binary'], 0o755)
        protected(BINARY, OLD_BINARY_SHA, modes=(0o755,))
        require(self.op.identity(self.op.regular(BINARY, mode=0o755, limit=128 << 20)) == self.original['binary_identity'],
                'The candidate binary inode changed before replacement.')
        os.replace(staged, BINARY)
        sync(INSTALL)
        protected(BINARY, self.intent['source']['binary']['sha256'], modes=(0o755,))
        self.dbtools.replace_file(RUNTIME, self.runtime_before, self.runtime_after)
        require(protected(RUNTIME) == self.runtime_after, 'The candidate runtime publication differs.')
        value = copy.deepcopy(self.state)
        value.update(schema=28, binary_sha256=self.intent['source']['binary']['sha256'],
                     binary_identity=self.op.identity(self.op.regular(BINARY, mode=0o755, limit=128 << 20)),
                     runtime_sha256=sha(self.runtime_after), process=None, start_pending=False)
        self.write_state(value, 'installed')
        self.phase('replaced')

    def public_readiness(self):
        self.operation_stage = 'readiness'
        self.http_requests = 0
        routes = [('/readyz', None), ('/emby/System/Info/Public', None), *self.inputs['frontend_routes']]
        require(len(routes) == 5, 'The exact readiness route count differs.')
        for route, asset in routes:
            require(self.op.verify_service(self.state) == self.state['process'], 'The replacement changed before readiness.')
            self.http_requests += 1
            self.save('http-%d-intent.json' % self.http_requests, {'method': 'GET', 'path': route, 'authenticated': False})
            connection = http.client.HTTPConnection('127.0.0.1', 18198, timeout=15)
            try:
                connection.request('GET', route, headers={'Accept': 'application/json' if asset is None else '*/*',
                                   'Accept-Encoding': 'identity', 'Origin': 'http://127.0.0.1:18196'})
                response = connection.getresponse()
                raw = response.read((8 << 20) + 1)
                status, cookie = response.status, response.getheader('Set-Cookie')
            finally:
                connection.close()
            self.save('http-%d-result.json' % self.http_requests, {'method': 'GET', 'path': route, 'status': status, 'bytes': len(raw), 'body_sha256': sha(raw),
                       'cookie_present': cookie is not None})
            require(status == 200 and len(raw) <= 8 << 20 and cookie is None, 'A public readiness response failed.')
            if asset is not None:
                require(sha(raw) == self.inputs['web']['files'][asset], 'The running candidate did not serve the accepted frontend bytes.')
            else:
                value = decode(raw, 2 << 20)
                if route == '/readyz':
                    require(value == {'Status': 'ready'}, 'The candidate is not ready.')
                else:
                    require(value.get('Id') == self.original['server_id'] and value.get('ProductName') == 'Goby' and
                            value.get('Version') == '4.9.5.0', 'The public product or server identity changed.')
            require(self.op.verify_service(self.state) == self.state['process'], 'The replacement changed during readiness.')

    def finalize(self):
        self.operation_stage = 'finalize'
        self.check()
        after = self.snapshot(28)
        compare_migrated(self.before, after, self.inputs['catalog'], runtime_sha256=sha(self.runtime_after))
        self.save('after-full.json', after)
        verify_candidate_web(self.inputs['web']['files'])
        require(self.global_before == decode(self.db.maintenance(self.dbtools.GLOBAL_SQL)) and self.fence.reserved == {'stop', 'start'} and
                self.db.pair['phase'] == 'removed' and not self.hba_active and self.http_requests == 5,
                'The final resource, service-action or readiness budget differs.')
        value = copy.deepcopy(self.original)
        upgrade = {'marker': MARKER, 'phase': 'complete', 'from_schema': 27, 'to_schema': 28, 'output': str(self.output),
                   'intent_sha256': self.intent_sha, 'old_process': OLD_PROCESS, 'new_process': self.state['process'],
                   'from_sha256': OLD_BINARY_SHA, 'to_sha256': self.intent['source']['binary']['sha256'],
                   'publication': self.inputs['publication']['commit'], 'backup_sha256': self.backup_sha,
                   'rehearsal': self.records['rehearsal-completed.json'], 'service': self.new_service}
        value.update(schema=28, phase='ready', stage='complete', process=self.state['process'],
                     binary_sha256=self.state['binary_sha256'], binary_identity=self.state['binary_identity'],
                     runtime_sha256=self.state['runtime_sha256'], upgrade=upgrade,
                     upgrade_history=self.original.get('upgrade_history', []) + [upgrade], schema28_upgrade=upgrade,
                     schema28_source={'marker': 'goby-client-schema28-source-v1', 'schema': 28, 'source': str(SOURCE),
                                      'source_manifest_sha256': SOURCE_SHA, 'catalog_sha256': CATALOG28_SHA,
                                      'publication': self.inputs['publication']['commit']})
        allowed = {'schema', 'phase', 'stage', 'process', 'binary_sha256', 'binary_identity', 'runtime_sha256', 'upgrade', 'upgrade_history'}
        require(set(value) == set(self.original) | {'schema28_upgrade', 'schema28_source'} and
                all(canonical(item) == canonical(value[key]) for key, item in self.original.items() if key not in allowed),
                'The final state changed an unrelated or historical field.')
        self.write_state(value, 'complete')
        self.check()
        require(self.properties(UNIT, tuple(self.new_service)) == self.new_service, 'The final candidate invocation changed.')
        members = Path('/sys/fs/cgroup/system.slice') / self.intent['controller']['unit']
        pids = {int(line) for path in members.rglob('cgroup.procs') for line in path.read_text().splitlines()}
        require(pids == {os.getpid()}, 'A helper process remains; forced service cleanup cannot count as success.')
        self.phase_name = 'complete'
        report = {'marker': MARKER, 'version': 1, 'run_id': self.intent['run_id'], 'intent_sha256': self.intent_sha,
                  'status': 'awaiting_outer_attestation', 'controller': self.controller, 'candidate_service': self.new_service,
                  'new_process': self.state['process'], 'state_sha256': sha(self.state_raw), 'schema': 28,
                  'binary': self.intent['source']['binary'], 'publication': self.inputs['publication']['commit'],
                  'preserved_rows_sequences_credentials_recovery': True, 'primary_unchanged': True,
                  'candidate_web': str(WEB), 'shared_web_unchanged': True, 'rehearsal_removed': True, 'hba_restored_exactly': True,
                  'service_actions': ['stop', 'start'], 'http_requests': 5, 'client_acceptance': False,
                  'automatic_retry': False, 'automatic_rollback': False, 'evidence': dict(self.records)}
        self.save('report.json', report)
        return {'status': 'awaiting_outer_attestation', 'report': self.records['report.json'], 'client_acceptance': False}

    def execute(self):
        self.begin()
        self.backup_materials()
        self.backup()
        self.rehearse()
        # Neither a successful rehearsal nor elapsed time authorizes old data.
        self.operation_stage = 'pre_stop_baseline'
        self.assert_baseline('pre-stop-full.json')
        self.phase('stop_requested')
        self.service('stop')
        self.phase('stopped')
        self.operation_stage = 'stopped_baseline'
        self.assert_baseline('stopped-full.json', live=False)
        self.migrate()
        self.install()
        self.phase('start_requested')
        self.service('start')
        self.public_readiness()
        return self.finalize()

    def run(self):
        try:
            self.prepare()
            if self.args.mode == 'preflight':
                return {'status': 'preflight_passed', 'input_sha256': self.intent_sha, 'new_evidence_writes': 0,
                        'http_requests': 0, 'service_actions': [], 'client_acceptance': False}
            return self.execute()
        except BaseException as error:
            if self.created:
                failure_stage, failure_code = self.safe_operation_stage(), safe_error_code(error)
                hba_restored = not self.hba_active
                try:
                    if not self.hba_restore_reserved:
                        self.restore_hba()
                        hba_restored = True
                except BaseException:
                    pass
                try:
                    self.save('failed.json', {'marker': MARKER, 'run_id': self.intent['run_id'], 'intent_sha256': self.intent_sha,
                        'status': 'retained_for_review', 'phase': self.phase_name, 'failure_type': type(error).__name__,
                        'operation_stage': failure_stage, 'error_code': failure_code,
                        'service_actions': sorted(self.fence.reserved), 'hba_restored_exactly': hba_restored,
                        'pair': None if self.db is None else self.db.pair, 'automatic_retry': False,
                        'automatic_rollback': False, 'client_acceptance': False})
                except BaseException:
                    pass
            raise
        finally:
            for fd in reversed(self.locks):
                os.close(fd)
            self.locks = []


def attest(args):
    """Read independent terminal/process evidence, then publish one attestation."""
    require(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0 and os.environ.get('SSH_CONNECTION'),
            'Use authorized root SSH for independent attestation.')
    controller = Controller(args)
    c = controller
    try:
        c.intent, c.intent_sha = load_intent(args.input, args.input_sha256)
        c.output, c.private = Path(c.intent['output']), Path(c.intent['output']) / 'private'
        c.inputs = verify_inputs(c.intent)
        c.output_identity = directory(c.output)
        require(not present(c.output / 'attestation.json') and not present(c.output / 'failed.json'),
                'This scope was already attested or failed; neither result may be overwritten.')
        raw = protected(c.output / 'report.json')
        report = decode(raw)
        require(report.get('marker') == MARKER and report.get('run_id') == c.intent['run_id'] and
                report.get('intent_sha256') == c.intent_sha and report.get('status') == 'awaiting_outer_attestation' and
                report.get('schema') == 28 and report.get('binary') == c.intent['source']['binary'] and
                report.get('service_actions') == ['stop', 'start'] and report.get('http_requests') == 5 and
                report.get('client_acceptance') is False and report.get('automatic_retry') is False and report.get('automatic_rollback') is False and
                all(report.get(key) is True for key in ('preserved_rows_sequences_credentials_recovery', 'primary_unchanged',
                    'shared_web_unchanged', 'rehearsal_removed', 'hba_restored_exactly')), 'The run did not propose a complete successful deployment.')
        for name, item in report['evidence'].items():
            descriptor(item)
            require(matches(r'[a-z0-9][a-z0-9.-]{0,95}', name) and Path(item['path']).parent in (c.output, c.private),
                    'A run evidence descriptor escaped its scope.')
            protected(Path(item['path']), item['sha256'], limit=256 << 20)
        for name, (path, digest) in HELPERS.items():
            setattr(c, name, load_module(path, digest, name))
        c.op.command = c.legacy_command
        c.dbtools.command = c.db_command
        unit = c.intent['controller']['unit']
        origin = report.get('controller', {})
        require(origin.get('unit') == unit and matches(r'[0-9a-f]{32}', origin.get('invocation_id')) and
                origin.get('process', {}).get('pid') != os.getpid(), 'The caller is not an independent observer of the recorded controller.')
        names = ('MainPID', 'InvocationID', 'ActiveState', 'SubState', 'Result', 'ExecMainStatus', 'ControlGroup', 'Description', 'RemainAfterExit')
        terminal = c.properties(unit, names)
        require(terminal['MainPID'] == '0' and terminal['InvocationID'] == origin['invocation_id'] and
                terminal['ActiveState'] == 'active' and terminal['SubState'] == 'exited' and terminal['Result'] == 'success' and
                terminal['ExecMainStatus'] == '0' and terminal['RemainAfterExit'] == 'yes' and
                terminal['Description'] == MARKER + ':' + c.intent['run_id'] and terminal['ControlGroup'] in ('', '/system.slice/' + unit),
                'The original controller is not independently successful and terminal.')
        c.dbtools.cgroup_empty(unit)
        c.state_raw = protected(STATE, report['state_sha256'])
        c.state, c.state_identity = decode(c.state_raw), identity(STATE.lstat())
        c.original = decode(protected(c.output / 'before-state.json', STATE_SHA))
        require(c.state.get('schema') == 28 and c.state.get('phase') == 'ready' and c.state.get('stage') == 'complete' and
                c.state.get('process') == report['new_process'] and c.state.get('binary_sha256') == c.intent['source']['binary']['sha256'] and
                c.state.get('schema28_upgrade', {}).get('intent_sha256') == c.intent_sha, 'The final candidate state differs from the run.')
        c.before = decode(protected(c.output / 'before-full.json'))
        compare_baseline(decode(protected(BASELINE, BASELINE_SHA)), c.before)
        c.runtime_before = protected(c.private / 'materials/runtime.env', RUNTIME_SHA)
        c.runtime_after = patch_runtime(c.runtime_before)
        c.master_proof = validate_master_baseline(c.before, c.runtime_before)
        require(canonical(decode(protected(c.output / 'master-proof.json'))) == canonical(c.master_proof),
                'The recorded master-key proof differs from the complete backup baseline.', 'master_state_changed')
        master_copy = c.private / 'materials/application/master.key'
        if c.master_proof['state'] == 'present':
            require(len(protected(master_copy, c.master_proof['sha256'], modes=(0o600,), limit=32)) == 32,
                    'The preserved master-key backup changed.', 'backup_materials_incomplete')
        else:
            require(not present(master_copy), 'An absent master key was created in the backup.', 'master_unexpected_presence')
        c.primary = decode(protected(c.output / 'primary-before.json'))
        c.media = decode(protected(c.output / 'media-before.json'))
        c.host = decode(protected(c.output / 'host-before.json'))
        c.shared_web = decode(protected(c.output / 'shared-web-before.json'))
        c.global_before = decode(protected(c.output / 'catalog-before.json'))
        c.workspace = load_module(SOURCE / SELECTED[0], sha(c.inputs['selected'][SELECTED[0]]), 'attest_workspace')
        c.workspace.command = c.workspace_command
        c.postgres = pwd.getpwnam('postgres')
        c.locks.append(c.workspace.acquire_lock())
        lock = os.open(c.op.LOCK, os.O_RDONLY | os.O_NOFOLLOW)
        try:
            require(identity(os.fstat(lock)) == identity(c.op.LOCK.lstat()), 'The candidate lock identity changed.')
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BaseException:
            os.close(lock)
            raise
        c.locks.append(lock)
        cluster = decode(protected(c.output / 'cluster-before.json'))
        c.owner_raw = protected(c.workspace.OWNER, cluster['owner_sha256'])
        c.owner = decode(c.owner_raw)
        c.hba_before = protected(c.output / 'hba-before')
        require(c.hba_before == c.workspace.HBA.encode(), 'The old HBA baseline is not the accepted workspace configuration.')
        c.cluster_files = {path: protected(Path(path), digest, uid=c.postgres.pw_uid, gid=c.postgres.pw_gid, modes=(0o600,))
                           for path, digest in cluster['configuration'].items()}
        require(set(c.cluster_files) == {str(PGDATA / name) for name in ('postgresql.conf', 'postgresql.auto.conf', 'pg_ident.conf')},
                'The preserved cluster configuration inventory differs.')
        c.password = ''
        name = 'goby_client_s55_rehearsal_' + c.intent['run_id'].replace('_', '')
        c.db = c.dbtools.Database({'run_id': c.intent['run_id'], 'database': name, 'role': name},
                                 {'selected': c.inputs['selected'], 'catalog': c.inputs['catalog']}, '')
        require(c.db.rows() == {'role': None, 'database': None} and
                decode(c.db.maintenance(c.dbtools.GLOBAL_SQL)) == c.global_before, 'The rehearsal or an unrelated global catalog row remains changed.')
        c.check()
        require(c.properties(UNIT, tuple(report['candidate_service'])) == report['candidate_service'], 'The replacement invocation changed.')
        after = c.snapshot(28)
        compare_migrated(c.before, after, c.inputs['catalog'], runtime_sha256=sha(c.runtime_after))
        compare_baseline(decode(protected(c.output / 'after-full.json')), after)
        verify_candidate_web(c.inputs['web']['files'])
        routes = [('/readyz', None), ('/emby/System/Info/Public', None), *c.inputs['frontend_routes']]
        for index, (route, asset) in enumerate(routes, 1):
            request = decode(protected(c.output / ('http-%d-intent.json' % index)))
            response = decode(protected(c.output / ('http-%d-result.json' % index)))
            require(request == {'method': 'GET', 'path': route, 'authenticated': False} and response.get('method') == 'GET' and
                    response.get('path') == route and type(response.get('status')) is int and response['status'] == 200 and
                    response.get('cookie_present') is False and type(response.get('bytes')) is int and 0 < response['bytes'] <= 8 << 20 and
                    matches(HASH_RE, response.get('body_sha256')) and
                    (asset is None or response['body_sha256'] == c.inputs['web']['files'][asset]), 'A direct readiness receipt is incomplete.')
        c.dbtools.cgroup_empty(unit)
        result = {'marker': MARKER, 'version': 1, 'run_id': c.intent['run_id'], 'intent_sha256': c.intent_sha,
                  'status': 'passed', 'report_sha256': sha(raw), 'controller': terminal, 'recursive_cgroup_empty': True,
                  'state_sha256': report['state_sha256'], 'binary': c.intent['source']['binary'], 'schema': 28,
                  'rows_sequences_credentials_recovery_preserved': True, 'primary_unchanged': True,
                  'rehearsal_removed': True, 'hba_restored_exactly': True, 'client_acceptance': False}
        create(c.output / 'attestation.json', canonical(result))
        return result
    finally:
        for fd in reversed(c.locks):
            os.close(fd)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('preflight', 'run', 'attest'))
    parser.add_argument('--input', required=True, type=Path)
    parser.add_argument('--input-sha256', required=True)
    args = parser.parse_args(argv)
    os.umask(0o077)
    if args.mode == 'run':
        def expired(_signal, _frame):
            raise Failure('The one-shot controller exceeded its wall-clock budget.')
        signal.signal(signal.SIGALRM, expired)
        signal.alarm(1100)
    try:
        result = attest(args) if args.mode == 'attest' else Controller(args).run()
        # Console output contains only scope/result descriptors, never vaults.
        print(canonical(result).decode().strip())
        return 0
    except BaseException as error:
        print(canonical({'marker': MARKER, 'status': 'failed', 'failure_type': type(error).__name__,
                         'error_code': safe_error_code(error),
                         'automatic_retry': False, 'automatic_rollback': False, 'client_acceptance': False}).decode().strip())
        return 1
    finally:
        if args.mode == 'run':
            signal.alarm(0)


if __name__ == '__main__':
    raise SystemExit(main())
