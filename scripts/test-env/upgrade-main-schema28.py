#!/usr/bin/env python3
"""Prepare and deploy source55/schema28 to the independently owned main service.

Prepare observes existing resources and writes only a new private evidence
bundle. Run requires its exact fresh bundle and independent passing client
acceptance before any HBA, database, or service mutation. Old scopes are never
resumed. Main PostgreSQL5432 and the candidate workspace15432 stay distinct.
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
import urllib.parse

sys.dont_write_bytecode = True
MARKER = 'goby-main-schema28-source55-upgrade-v1'
WORK = Path('/opt/goby-test/exec-work-m3e')
TOOL = WORK / 'main-schema28-source55-tool-01'
BACKUPS = Path('/opt/goby-test/backups/main-schema28-v1')
SOURCE = WORK / 'source-attempt-55'
SOURCE_SHA = '7d2548603e209ebce6154853321147aeec33ccbb40cc12b3ca37418ad765937a'
INSTALL = Path('/opt/goby-dev')
BINARY = INSTALL / 'goby'
WEB = INSTALL / 'admin'
UNIT = 'goby-foundation-test.service'
UNIT_FILE = Path('/etc/systemd/system') / UNIT
RUNTIME = Path('/opt/goby-test/runtime.env')
RECOVERY_ENV = Path('/opt/goby-test/recovery-m5j.env')
BROWSER = Path('/opt/goby-test/browser.env')
MASTER = Path('/var/lib/goby-test/application-key-vault/master.key')
LIFECYCLE = Path('/var/lib/goby-test/recovery-m5j')
LIFECYCLE_LOCK = LIFECYCLE / '.goby-lifecycle.lock'
STORES = (MASTER.parent, LIFECYCLE, Path('/var/lib/goby-test/backups-m5j'), Path('/var/lib/goby-test/recovery-operations-m5j'))
PAIRING = Path('/var/lib/goby-test/operator-secrets-m5j/attempt-01')
DIAGNOSTICS = Path('/var/log/goby-test')
CACHE = Path('/dev/shm/goby-transcodes-test')
CACHE_OWNER = Path('/opt/goby-test/transcode-cache.owner')
CACHE_OWNER_SHA = 'd28cdc05d8a25c2f067e1507e30bc4a6b0f9dbd99526c0430855fbcd2470a2e7'
DEPLOYMENT_LOCK = WORK / 'main-deployment-schema25.lock'
DEPLOYMENT_LOCK_BYTES = b'goby-client-schema25-deployment-v1\n'
OLD_PROCESS = {'boot_id': '6bdfc486-7bc8-412f-82b5-70095a09dde7', 'pid': 762090, 'start_ticks': 7637121}
OLD_INVOCATION = 'bb94d74b475f4382a6ec6f6df181dd74'
OLD_BINARY_SHA = 'af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620'
MAIN = {'database': 'goby_test', 'role': 'goby_test', 'database_oid': 16385, 'role_oid': 16384}
RECOVERY = 'goby_recovery_m5j'
SYSTEM_IDENTIFIER = '7683277964552005578'
DEPLOYMENT_ID = 'f58d5e0c8ff49fca916499e666bffd9f'
PGDATA = Path('/var/lib/postgresql/17/main')
PG = Path('/usr/lib/postgresql/17/bin')
FULL_RUN = '20260912_084241_db776aacc1a7'
FULL_UNIT = 'goby-collection-folder-source55-full-controller-v1.service'
FULL_INVOCATION = '20f0d83200c34228aa5862e07ce24c2c'
FULL_REPORT = WORK / ('client-backup-run-' + FULL_RUN) / 'report.json'
FULL_REPORT_SHA = '2c0b5a54b51f66ffa0b11f3160cf50325df520322ec79af1fd92fc9bff93596c'
FULL_TERMINAL = WORK / 'collection-folder-source55-full-execution-01/terminal.json'
FULL_TERMINAL_SHA = '7d43a77d904bb212f9231bf427133eb0034a9f74a2187004e18775ccd334880c'
NEW_BINARY_SHA = '6a8c46cdd0dcff56af28f11084eabcf2497daf5ce11ac072eaad7a5dbf486e81'
WEB_SOURCE = WORK / 'storage-binding-workflow-web-03/dist'
WEB_REPORT_SHA = '2b33e2805afb7422eeb8486db9c4f531cd074a636e4dcddc08c91c8d0808a9d7'
CATALOG27_SHA = '1fc91c2e380805bff0f87867547d307bc7830ffeb49c3489da4e1713a5c0047d'
CATALOG28_SHA = '8e7569c8fe2073ee2ed4c51147f9abc21061d1aac9554843101b826fa5a1cc2b'
SELECTED = ('internal/config/config.go', 'internal/backuppg/catalog.go',
            'internal/backuppg/catalogs/schema-27-postgresql-17.json',
            'internal/backuppg/catalogs/schema-28-postgresql-17.json')
TOOL_FILES = frozenset(('upgrade-main-schema28.py', 'test-upgrade-main-schema28.py', 'migrate-main-schema28.go',
    'migrate-main-schema28_test.go', 'migrate-main-schema28', 'helper-build.json', 'helper-go-guards.json',
    'guards-report.json', 'accepted-web-report.json', 'publication.json'))
HASH_RE = r'[0-9a-f]{64}'
RUN_RE = r'[0-9]{8}_[0-9]{6}_[0-9a-f]{12}'
ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8'}
NEW_COLUMNS = {'library_roots': ['binding_revision', 'storage_binding', 'bound_at', 'bound_by'],
               'activity_entries': ['previous_revision', 'observation_fingerprint']}
SERVICE_PROPERTIES = ('MainPID', 'InvocationID', 'ActiveState', 'SubState', 'FragmentPath', 'DropInPaths', 'User', 'Group',
    'WorkingDirectory', 'EnvironmentFiles', 'Restart', 'RestartForceExitStatus', 'ControlGroup', 'KillMode', 'TimeoutStopUSec',
    'NoNewPrivileges', 'ProtectSystem', 'ProtectHome', 'ReadWritePaths', 'UMask', 'MemoryMax', 'MemorySwapMax',
    'CPUQuotaPerSecUSec', 'TasksMax', 'LimitNOFILE', 'PrivateTmp', 'CapabilityBoundingSet')
SERVICE_DYNAMIC = {'MainPID', 'InvocationID', 'ActiveState', 'SubState', 'ControlGroup'}
FORBIDDEN = 'upgrade-main-schema25.py'
CLIENT_GATE_MARKER = 'goby-source55-movies-library-changed-terminal-v1'
CLIENT_GATE_MODE = 'b-movies-name-automatic-refresh'
CLIENT_UPGRADE_ATTEST_SHA = 'ec20d286a1998f0b27667603819853129e88b91579a97a25141bd6258e450032'
HELPER_VERIFICATION = WORK / 'main-schema28-source55-helper-verification-01/report.json'
HELPER_VERIFICATION_SHA = '450fd5d54b25924b8b4b2bf03b5142148c2c1b25f487a20ff60c59f26d7cb3ba'

ERROR_CODES = frozenset(('guard_rejected', 'external_failure', 'master_runtime_path_invalid', 'master_state_invalid',
    'master_required_missing', 'master_unexpected_presence', 'master_unexpected_absence', 'master_state_changed',
    'backup_materials_incomplete', 'command_failed', 'command_timeout', 'helper_failed', 'helper_retained',
    'helper_outcome_unknown', 'helper_committed_cleanup_failed', 'helper_committed_report_failed'))
HELPER_FAILURES = frozenset(('source_snapshot_rejected', 'source_facts_rejected', 'source_sequences_changed_during_dump',
    'snapshot_dump_failed', 'snapshot_dump_configuration', 'snapshot_dump_database', 'snapshot_dump_archive',
    'snapshot_dump_limit', 'snapshot_dump_schema', 'snapshot_dump_target', 'snapshot_dump_command',
    'snapshot_dump_unsupported', 'snapshot_dump_cancelled', 'snapshot_dump_deadline',
    'role_properties_invalid', 'main_profile_capture_failed', 'backup_baseline_drift', 'rehearsal_restore_failed', 'rehearsal_privileges_changed',
    'lifecycle_fd_arguments_invalid', 'lifecycle_scope_invalid', 'lifecycle_fence_invalid',
    'main_lease_busy_or_unavailable', 'main_lease_close_failed', 'rehearsal_lease_close_failed', 'rollback_outcome_unknown',
    'migration_failed', 'migration_commit_outcome_unknown', 'migration_report_publication_failed'))
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
    if type(value) is dict and set(value) == {'marker', 'status', 'error'} and value['marker'] == 'goby-main-schema28-helper-v1':
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
    require(Path(value['path']).name != FORBIDDEN, 'The prohibited historical operator is outside this scope.')


def validate_intent(value, mode):
    exact(value, {'marker', 'version', 'run_id', 'tool', 'output', 'prepared', 'source', 'helper', 'web', 'guards', 'go_guards',
                  'source_closure', 'main_baseline', 'client_gate', 'startup', 'protected_services', 'historical'})
    require(value['marker'] == MARKER and type(value['version']) is int and value['version'] == 1 and
            matches(RUN_RE, value['run_id']) and value['tool'] == str(TOOL), 'The main scope is invalid.')
    run = value['run_id']
    require(value['output'] == str(BACKUPS / ('run-' + run)) and value['prepared'] == str(WORK / ('main-schema28-source55-prepared-' + run)),
            'A main evidence path escaped its fresh scope.')
    source = value['source']
    exact(source, {'root', 'manifest_sha256', 'full_report', 'terminal', 'binary', 'publication'})
    require(source['root'] == str(SOURCE) and source['manifest_sha256'] == SOURCE_SHA, 'The product source differs.')
    for key, path, digest in (('full_report', FULL_REPORT, FULL_REPORT_SHA), ('terminal', FULL_TERMINAL, FULL_TERMINAL_SHA)):
        descriptor(source[key], path=path)
        require(source[key]['sha256'] == digest, 'An accepted full product receipt changed.')
    descriptor(source['publication'], path=TOOL / 'publication.json')
    require(source['binary'] == {'path': str(FULL_REPORT.parent / 'tmp/goby-linux-amd64'), 'sha256': NEW_BINARY_SHA, 'bytes': 29337989},
            'The source55 binary descriptor differs.')
    exact(value['helper'], {'source', 'binary', 'build'})
    for key, filename in (('source', 'migrate-main-schema28.go'), ('binary', 'migrate-main-schema28'), ('build', 'helper-build.json')):
        descriptor(value['helper'][key], path=TOOL / filename)
    exact(value['web'], {'source', 'report'})
    require(value['web']['source'] == str(WEB_SOURCE), 'The accepted frontend path differs.')
    descriptor(value['web']['report'], path=TOOL / 'accepted-web-report.json')
    require(value['web']['report']['sha256'] == WEB_REPORT_SHA, 'The frontend acceptance receipt differs.')
    descriptor(value['guards'], path=TOOL / 'guards-report.json')
    descriptor(value['go_guards'], path=TOOL / 'helper-go-guards.json')
    closure = value['source_closure']
    require(type(closure) is dict and set(closure) == TOOL_FILES and all(matches(HASH_RE, item) and item != '0' * 64 for item in closure.values()),
            'The exact main tool closure is incomplete.')
    for item in (*value['helper'].values(), value['guards'], value['go_guards'], value['web']['report'], source['publication']):
        require(closure[Path(item['path']).name] == item['sha256'], 'An input descriptor disagrees with the tool closure.')
    if value['main_baseline'] is not None:
        descriptor(value['main_baseline'], path=Path(value['prepared']) / 'baseline.json')
    if mode != 'prepare':
        require(value['main_baseline'] is not None and value['client_gate'] is not None,
                'Run requires a fresh main baseline and passing client gate.')
    if value['client_gate'] is not None:
        descriptor(value['client_gate'])
    startup = value['startup']
    exact(startup, {'deadline_utc', 'min_remaining_seconds', 'retention_days'})
    require(type(startup['min_remaining_seconds']) is int and 900 <= startup['min_remaining_seconds'] <= 3600 and
            type(startup['retention_days']) is int and 1 <= startup['retention_days'] <= 365, 'The startup bounds are invalid.')
    parse_time(startup['deadline_utc'])
    require(type(value['protected_services']) is list and len(value['protected_services']) == 3, 'Three protected service/process pins are required.')
    names = set()
    for entry in value['protected_services']:
        exact(entry, {'name', 'pid', 'start_ticks', 'boot_id', 'exe', 'sha256', 'uid', 'cgroup'})
        require(entry['name'] in ('candidate', 'proxy', 'workspace') and entry['name'] not in names and
                type(entry['pid']) is int and entry['pid'] > 1 and type(entry['start_ticks']) is int and entry['start_ticks'] > 0 and
                entry['boot_id'] == OLD_PROCESS['boot_id'] and matches(HASH_RE, entry['sha256']) and type(entry['uid']) is int,
                'A protected process pin is invalid.')
        names.add(entry['name'])
        if entry['name'] == 'candidate':
            require(entry['exe'] == '/opt/goby-client-m3e/goby' and entry['uid'] == 995 and entry['cgroup'] == '/system.slice/goby-client-m3e.service',
                    'The candidate preservation pin names another process.')
        elif entry['name'] == 'workspace':
            require(entry['exe'] == str(PG / 'postgres'), 'The workspace preservation pin names another executable.')
        else:
            require(entry['pid'] == 334022 and entry['start_ticks'] == 378464 and entry['uid'] == 0 and
                    matches(r'/usr/bin/python3(?:\.[0-9]+)?', entry['exe']), 'The proxy pin is not the previously accepted self-written proxy.')
    require(type(value['historical']) is list and len(value['historical']) >= 2, 'Historical main provenance is missing.')
    for item in value['historical']:
        descriptor(item)
    return value


def parse_time(value):
    require(type(value) is str and re.fullmatch(r'[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z', value), 'A UTC deadline is invalid.')
    try:
        result = dt.datetime.fromisoformat(value[:-1] + '+00:00')
    except ValueError:
        raise Failure('A UTC deadline is invalid.') from None
    return result


def input_core(value):
    return {key: child for key, child in value.items() if key not in ('main_baseline', 'client_gate')}


def load_intent(path, digest, mode):
    raw = protected(path, digest)
    value = validate_intent(decode(raw), mode)
    directory = WORK / ('main-schema28-source55-inputs-' + value['run_id'])
    require(path == directory / ('prepare.json' if mode == 'prepare' else 'run.json'), 'The intent is not its fixed phase-specific input.')
    return value, sha(raw)


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
    require(Path(__file__) == TOOL / 'upgrade-main-schema28.py', 'Use the frozen new operator entrypoint.')
    for name, digest in intent['source_closure'].items():
        protected(TOOL / name, digest, modes=(0o600, 0o644, 0o755))
    actual = set()
    for path in TOOL.iterdir():
        info = path.lstat()
        require(stat.S_ISREG(info.st_mode) and info.st_uid == info.st_gid == 0 and info.st_nlink == 1,
                'The tool root contains an unexpected member.')
        actual.add(path.name)
    require(actual == TOOL_FILES, 'The exact tool membership changed.')
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
                  'binary_sha256', 'go', 'argv', 'environment', 'exit_code', 'verification_report'})
    descriptor(build['verification_report'], path=HELPER_VERIFICATION)
    require(build['verification_report']['sha256'] == HELPER_VERIFICATION_SHA, 'The main helper verification provenance differs.')
    verification = decode(protected(HELPER_VERIFICATION, HELPER_VERIFICATION_SHA))
    build_root = WORK / 'main-schema28-source55-build-01'
    helper_source = intent['helper']['source']['sha256']
    expected_files = {**product_files, 'cmd/main-schema28-migration/main.go': helper_source,
                      'cmd/main-schema28-migration/main_test.go': intent['source_closure']['migrate-main-schema28_test.go']}
    require(build['marker'] == 'goby-main-schema28-helper-build-v1' and build['source'] == str(SOURCE) and
            build['source_manifest_sha256'] == SOURCE_SHA and build['build_root'] == str(build_root) and
            build['files'] == expected_files and build['helper_source_sha256'] == helper_source and
            build['binary_sha256'] == intent['helper']['binary']['sha256'] and type(build['exit_code']) is int and build['exit_code'] == 0,
            'The helper lacks a successful independent source-copy build.')
    require(verification.get('marker') == 'goby-main-schema28-helper-verification-v1' and verification.get('status') == 'passed' and
            verification.get('database') is False and type(verification.get('service_mutations')) is int and verification['service_mutations'] == 0 and
            verification.get('source') == str(SOURCE) and verification.get('source_manifest_sha256') == SOURCE_SHA and
            verification.get('build_root') == str(build_root) and verification.get('files') == expected_files and
            verification.get('binary', {}).get('sha256') == build['binary_sha256'] and
            verification.get('formatted_sources') == {name: intent['source_closure'][name] for name in ('migrate-main-schema28.go', 'migrate-main-schema28_test.go')} and
            verification.get('test_counts') == {'pass': 15, 'fail': 0, 'skip': 0}, 'The derived helper receipt differs from actual remote verification.')
    descriptor(build['go'], path=Path('/opt/goby-toolchains/go1.27.1/bin/go'))
    require(build['go']['sha256'] == '30969f97169d7f43fe6a085873d75613adc21e30818a8c61d95bd27275df4624' and
            build['argv'] == [build['go']['path'], 'build', '-mod=readonly', '-trimpath', '-buildvcs=false', '-o',
                             str(TOOL / 'migrate-main-schema28'), './cmd/main-schema28-migration/main.go'] and
            build['environment'] == {'GOOS': 'linux', 'GOARCH': 'amd64', 'CGO_ENABLED': '0', 'GOWORK': 'off',
                                    'GOTOOLCHAIN': 'local', 'GOPROXY': 'off', 'GOSUMDB': 'off'},
            'The helper was not built with the declared offline product toolchain.')
    build_checks = [item for item in verification.get('checks', []) if item.get('name') == 'build']
    require(len(build_checks) == 1 and type(build_checks[0].get('exit_code')) is int and build_checks[0]['exit_code'] == 0 and
            build_checks[0].get('argv') == build['argv'], 'The derived build command differs from actual remote execution.')
    protected(Path(build['go']['path']), build['go']['sha256'], modes=(0o755,))
    verify_tree(build_root, expected_files)
    protected(TOOL / 'migrate-main-schema28', build['binary_sha256'], modes=(0o755,))
    guards = decode(protected(Path(intent['guards']['path']), intent['guards']['sha256']))
    require(guards.get('suite') == 'main-schema28-upgrade-guards' and guards.get('status') == 'passed' and
            guards.get('operator_sha256') == intent['source_closure']['upgrade-main-schema28.py'] and
            guards.get('guard_sha256') == intent['source_closure']['test-upgrade-main-schema28.py'] and
            type(guards.get('test_count')) is int and guards['test_count'] >= 20 and
            guards.get('actual_controller_flow') is True and all(type(guards.get(key)) is int and guards[key] == 0
            for key in ('failures', 'errors', 'skips', 'unexpected_effects')), 'The exact operator guards have not passed.')
    go_guards = decode(protected(Path(intent['go_guards']['path']), intent['go_guards']['sha256']))
    require(go_guards.get('marker') == 'goby-main-schema28-helper-go-guards-v1' and go_guards.get('status') == 'passed' and
            type(go_guards.get('exit_code')) is int and go_guards['exit_code'] == 0 and go_guards.get('database_access') is False and
            type(go_guards.get('failures_or_skips')) is int and go_guards['failures_or_skips'] == 0 and
            go_guards.get('helper_source_sha256') == intent['source_closure']['migrate-main-schema28.go'] and
            go_guards.get('guard_source_sha256') == intent['source_closure']['migrate-main-schema28_test.go'] and
            go_guards.get('verification_report') == build['verification_report'] and
            type(go_guards.get('tests')) is list and len(go_guards['tests']) == 15 and
            all(matches(r'Test[A-Za-z0-9_]+', name) for name in go_guards['tests']) and len(go_guards['tests']) == len(set(go_guards['tests'])),
            'The actual main helper guards have not passed.')
    for item in intent['historical']:
        protected(Path(item['path']), item['sha256'])
    return {'selected': selected, 'catalog': catalog, 'web': web, 'frontend_routes': routes, 'binary': binary, 'publication': publication,
            'full_terminal': terminal, 'source_manifest': manifest, 'build': build}


def small_identity(info):
    return {key: value for key, value in identity(info).items() if key in ('device', 'inode', 'uid', 'gid', 'mode')}


def tree_fact(root, *, uid, gid=None, total_limit=1 << 30):
    require(root.is_absolute() and root.name != FORBIDDEN, 'A tree root is invalid.')
    before = root.lstat()
    require(stat.S_ISDIR(before.st_mode) and before.st_uid == uid and (gid is None or before.st_gid == gid) and
            not before.st_mode & 0o022, 'A tree root is unowned or writable.')
    files, directories, total = {}, {}, 0
    for path in sorted(root.rglob('*')):
        require(path.name != FORBIDDEN, 'Historical operator source must not be traversed.')
        relative = path.relative_to(root).as_posix()
        current = path.lstat()
        require(current.st_uid == uid and (gid is None or current.st_gid == gid) and not current.st_mode & 0o022 and
                len(files) + len(directories) < 20000, 'A tree member is unowned or exceeds its bound.')
        if stat.S_ISDIR(current.st_mode):
            directories[relative] = small_identity(current)
        else:
            require(stat.S_ISREG(current.st_mode) and current.st_nlink == 1, 'A tree contains a link or special member.')
            total += current.st_size
            require(total <= total_limit, 'A private tree exceeds its byte limit.')
            raw = protected(path, uid=uid, gid=current.st_gid, modes=(stat.S_IMODE(current.st_mode),), limit=512 << 20)
            files[relative] = {'identity': identity(current), 'sha256': sha(raw)}
    require(small_identity(root.lstat()) == small_identity(before), 'A tree root changed while read.')
    return {'root': str(root), 'identity': small_identity(before), 'directories': directories, 'files': files}


def process_fact(pid, *, executable=None):
    require(type(pid) is int and pid > 1, 'A process identifier is invalid.')
    root = Path('/proc') / str(pid)
    raw = (root / 'stat').read_text()
    fields = raw[raw.rfind(')') + 2:].split()
    require(len(fields) >= 20 and fields[19].isdigit(), 'A process stat is invalid.')
    result = {'pid': pid, 'start_ticks': int(fields[19]), 'boot_id': Path('/proc/sys/kernel/random/boot_id').read_text().strip(),
              'uid': root.stat().st_uid, 'exe': os.readlink(root / 'exe'),
              'cgroup': (root / 'cgroup').read_text().strip().removeprefix('0::')}
    require(executable is None or result['exe'] == str(executable), 'A process executable path differs.')
    with (root / 'exe').open('rb') as stream:
        content = stream.read((128 << 20) + 1)
    require(len(content) <= 128 << 20, 'A process executable exceeds its bound.')
    result['sha256'] = sha(content)
    require((root / 'stat').read_text().rsplit(') ', 1)[1].split()[19] == str(result['start_ticks']), 'A process was replaced during observation.')
    return result


def cgroup_empty(unit):
    root = Path('/sys/fs/cgroup/system.slice') / unit
    if not present(root):
        return
    require(root.is_dir() and not root.is_symlink(), 'The unit cgroup is invalid.')
    files = list(root.rglob('cgroup.procs'))
    require(1 <= len(files) <= 512 and all(not item.is_symlink() and not item.read_text().strip() for item in files),
            'The unit cgroup is not recursively empty.')


def compare_database(before, after, catalog28=None):
    left, right = copy.deepcopy(before), copy.deepcopy(after)
    for value in (left, right):
        value['metadata'].pop('captured_at', None)
    if catalog28 is None:
        require(canonical(left) == canonical(right), 'The database drifted from its complete baseline.')
        return
    require(left['version'] == 27 and right['version'] == 28 and canonical(right['catalog']) == canonical(catalog28['objects']),
            'The database migration catalog differs.')
    require(set(left['tables']) == set(right['tables']) and len(left['tables']) == 35 and
            canonical(left['sequences']) == canonical(right['sequences']), 'Tables or old sequence states changed.')
    for key in ('database', 'server_version_num', 'schemas', 'public_schema', 'relations', 'role'):
        require(canonical(left['metadata'][key]) == canonical(right['metadata'][key]), 'An existing database identity or ACL changed.')
    for name, columns in left['metadata']['columns'].items():
        require(right['metadata']['columns'][name] == columns + NEW_COLUMNS.get(name, []), 'Old columns changed.')
        rows = right['tables'][name]
        if name == 'schema_migrations':
            added = [row for row in rows if row['version'] == 28]
            require(len(added) == 1 and added[0]['name'] == '0028_storage_root_bindings.sql', 'Migration28 history differs.')
            rows = [row for row in rows if row['version'] != 28]
        require(sorted(canonical({key: row[key] for key in columns}) for row in rows) == sorted(map(canonical, left['tables'][name])),
                'An old row changed during migration.')
    require(all(type(row['binding_revision']) is int and row['binding_revision'] == 1 and row['storage_binding'] is None and
                row['bound_at'] is None and row['bound_by'] is None for row in right['tables']['library_roots']), 'Migration inferred root approval.')
    require(all(type(row['previous_revision']) is int and row['previous_revision'] == 0 and row['observation_fingerprint'] == ''
                for row in right['tables']['activity_entries']), 'Migration rewrote historical audit facts.')


class ServiceFence:
    def __init__(self):
        self.stage, self.actions = 'prepare', []

    def approve(self, args):
        if args and Path(args[0]).name == 'systemctl':
            require(args[0] == '/usr/bin/systemctl' and len(args) > 1, 'A noncanonical service command is forbidden.')
            if args[1] == 'show':
                return
            expected = {'restart_fence_install': ('daemon-reload', []), 'stop_requested': ('stop', ['reload-install']),
                        'start_requested': ('start', ['reload-install', 'stop']),
                        'restart_fence_restore': ('daemon-reload', ['reload-install', 'stop', 'start'])}
            require(self.stage in expected, 'A service action is outside its durable phase.')
            action, previous = expected[self.stage]
            require(args == ['/usr/bin/systemctl', action] + ([] if action == 'daemon-reload' else [UNIT]) and self.actions == previous,
                    'A service action is repeated, unowned or out of order.')
            self.actions.append('reload-install' if self.stage == 'restart_fence_install' else 'reload-restore' if self.stage == 'restart_fence_restore' else action)

GLOBAL_SQL = """SELECT jsonb_build_object(
 'roles',(SELECT coalesce(jsonb_agg(jsonb_build_object('oid',oid,'name',rolname,'login',rolcanlogin,
 'super',rolsuper,'createdb',rolcreatedb,'createrole',rolcreaterole,'inherit',rolinherit,
 'replication',rolreplication,'bypass',rolbypassrls,'limit',rolconnlimit,'config',rolconfig,
 'valid_until',rolvaliduntil,'tag',shobj_description(oid,'pg_authid'),
 'verifier_sha256',(SELECT CASE WHEN rolpassword IS NULL THEN NULL ELSE encode(sha256(convert_to(rolpassword,'UTF8')),'hex') END
                    FROM pg_authid a WHERE a.oid=pg_roles.oid)) ORDER BY oid),'[]') FROM pg_roles),
 'databases',(SELECT coalesce(jsonb_agg(jsonb_build_object('oid',oid,'name',datname,'owner',datdba,
 'acl',datacl,'connect',datallowconn,'limit',datconnlimit,'template',datistemplate,
 'tag',shobj_description(oid,'pg_database'),'stable',to_jsonb(d)-'datfrozenxid'-'datminmxid') ORDER BY oid),'[]') FROM pg_database d),
 'tablespaces',(SELECT coalesce(jsonb_agg(to_jsonb(t)||jsonb_build_object('location',pg_tablespace_location(oid)) ORDER BY oid),'[]') FROM pg_tablespace t),
 'memberships',(SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY oid),'[]') FROM pg_auth_members m),
 'settings',(SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY setdatabase,setrole),'[]') FROM pg_db_role_setting s));"""


def unsupported_sql(role_oid):
    require(type(role_oid) is int and role_oid > 0, "The object owner OID is invalid.")
    checks = ["SELECT 1 FROM pg_namespace WHERE nspname NOT IN ('public','pg_catalog','pg_toast','information_schema')",
              "SELECT 1 FROM pg_extension WHERE extname<>'plpgsql'",
              "SELECT 1 FROM pg_language WHERE lanname NOT IN ('internal','c','sql','plpgsql')",
              "SELECT 1 FROM pg_inherits",
              "SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND "
              f"(c.relowner<>{role_oid} OR c.relacl IS NOT NULL OR c.relkind NOT IN ('r','i','S'))",
              "SELECT 1 FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace "
              "WHERE n.nspname='public' AND a.attacl IS NOT NULL",
              "SELECT 1 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public' AND "
              f"(p.proowner<>{role_oid} OR p.proacl IS NOT NULL)",
              "SELECT 1 FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname='public' AND "
              f"(t.typowner<>{role_oid} OR t.typacl IS NOT NULL OR NOT ((t.typtype='c' AND t.typrelid IN "
              "(SELECT oid FROM pg_class WHERE relnamespace=n.oid AND relkind='r')) OR (t.typtype='b' AND t.typelem IN "
              "(SELECT oid FROM pg_type WHERE typnamespace=n.oid AND typtype='c' AND typrelid<>0))))",
              "SELECT 1 FROM pg_subscription WHERE subdbid=(SELECT oid FROM pg_database WHERE datname=current_database())"]
    for table, column in (("pg_collation", "collnamespace"), ("pg_conversion", "connamespace"),
                          ("pg_operator", "oprnamespace"), ("pg_opclass", "opcnamespace"), ("pg_opfamily", "opfnamespace"),
                          ("pg_ts_config", "cfgnamespace"), ("pg_ts_dict", "dictnamespace"),
                          ("pg_ts_parser", "prsnamespace"), ("pg_ts_template", "tmplnamespace")):
        checks.append(f"SELECT 1 FROM {table} o JOIN pg_namespace n ON n.oid=o.{column} WHERE n.nspname='public'")
    for table in ("pg_event_trigger", "pg_foreign_server", "pg_foreign_data_wrapper", "pg_publication",
                  "pg_largeobject_metadata", "pg_default_acl", "pg_seclabel", "pg_policy", "pg_statistic_ext", "pg_transform"):
        checks.append("SELECT 1 FROM " + table)
    checks.append("SELECT 1 FROM pg_rewrite r JOIN pg_class c ON c.oid=r.ev_class JOIN pg_namespace n "
                  "ON n.oid=c.relnamespace WHERE n.nspname='public'")
    return "SELECT EXISTS(" + " UNION ALL ".join(checks) + ");"


class Controller:
    """Own one prepared baseline, rehearsal pair and bounded main transition."""

    def __init__(self, args):
        self.args = args
        self.intent = self.inputs = self.baseline = self.before = None
        self.output = self.private = self.prepared = self.intent_sha = None
        self.lock = None
        self.lifecycle_fds = []
        self.fence, self.phase_name = ServiceFence(), 'prepare'
        self.records, self.sequence = {}, 0
        self.created = self.hba_changed = self.hba_restore_reserved = False
        self.restart_installed = self.restart_restore_reserved = False
        self.pair = {'phase': 'absent', 'role_oid': None, 'database_oid': None}
        self.password = secrets.token_hex(32)
        self.env_values = {}
        self.expected_process, self.expected_binary = dict(OLD_PROCESS), OLD_BINARY_SHA

    def command(self, args, *, data=None, env=None, timeout=30, pass_fds=()):
        args = [str(value) for value in args]
        self.fence.approve(args)
        require(0 < timeout <= 600, 'A command exceeds its deadline.')
        try:
            result = subprocess.run(args, input=data, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                    env=env or ENV, timeout=timeout, check=False, pass_fds=pass_fds)
        except (OSError, subprocess.TimeoutExpired):
            raise Failure('The bounded command did not complete; it will not be retried.', 'command_timeout') from None
        if result.returncode:
            code = helper_failure_code(result.stdout) if args[0] == str(TOOL / 'migrate-main-schema28') else 'command_failed'
            if self.created:
                self.sequence += 1
                self.save('command-failure-%03d.json' % self.sequence, {'phase': self.phase_name, 'code': code, 'exit': result.returncode,
                    'stdout_sha256': sha(result.stdout), 'stderr_sha256': sha(result.stderr)})
            raise Failure('A bounded command failed; its outcome is retained.', code)
        require(len(result.stdout) <= 64 << 20 and len(result.stderr) <= 8 << 20, 'A command output exceeds its bound.')
        return result.stdout.strip()

    def sql(self, statement, database='postgres', *, write=False):
        require(self.lock is not None, 'Main SQL requires the existing deployment lock.')
        require(database in ('postgres', MAIN['database'], RECOVERY, self.rehearsal_name), 'SQL selected an unowned database.')
        if write:
            require(self.created and database == 'postgres' and statement in self.allowed_ddl(), 'SQL is not an exact owned rehearsal mutation.')
        environment = dict(ENV, PGCONNECT_TIMEOUT='5', PGOPTIONS='-c statement_timeout=20000 -c lock_timeout=5000' +
                           ('' if write else ' -c default_transaction_read_only=on'))
        args = ['/usr/sbin/runuser', '-u', 'postgres', '--', str(PG / 'psql'), '-X', '--no-password', '-h', self.socket,
                '-p', '5432', '-U', 'postgres', '-d', database, '-v', 'ON_ERROR_STOP=1', '-A', '-t', '-q']
        return self.command(args, data=(statement + '\n').encode(), env=environment, timeout=60)

    def properties(self, unit, names):
        require(unit in (UNIT, FULL_UNIT, self.controller_unit, getattr(self, 'client_gate_unit', None)),
                'A service query escaped the declared units.')
        raw = self.command(['/usr/bin/systemctl', 'show', unit, '--no-pager', '--property=' + ','.join(names)]).decode()
        result = {}
        for line in raw.splitlines():
            name, separator, value = line.partition('=')
            require(separator and (name == 'EnvironmentFiles' or name not in result), 'Service properties are ambiguous.')
            if name == 'EnvironmentFiles':
                result.setdefault(name, []).append(value)
            else:
                result[name] = value
        require(set(result) == set(names), 'A service property is missing.')
        return result

    def acquire_lock(self):
        require(protected(DEPLOYMENT_LOCK, modes=(0o600,)) == DEPLOYMENT_LOCK_BYTES,
                'The accepted main deployment lock is missing or foreign.')
        fd = os.open(DEPLOYMENT_LOCK, os.O_RDONLY | os.O_NOFOLLOW)
        try:
            require(identity(os.fstat(fd)) == identity(DEPLOYMENT_LOCK.lstat()), 'The main lock identity changed.')
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BaseException:
            os.close(fd)
            raise
        self.lock = fd

    def load_inputs(self):
        require(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0 and os.environ.get('SSH_CONNECTION'),
                'Use the authorized Linux root SSH environment.')
        self.intent, self.intent_sha = load_intent(self.args.input, self.args.input_sha256, self.args.mode)
        self.output, self.prepared = Path(self.intent['output']), Path(self.intent['prepared'])
        self.private = self.output / 'private'
        self.rehearsal_name = 'goby_main_s55_rehearsal_' + self.intent['run_id'].replace('_', '')
        self.controller_unit = 'goby-main-schema28-source55-' + self.intent['run_id'].replace('_', '-') + '.service'
        self.restart_path = Path('/run/systemd/system') / (UNIT + '.d') / ('95-main-schema28-' + self.intent['run_id'] + '.conf')
        self.restart_bytes = b'[Service]\nRestart=no\n'
        # No application-private read, SQL, service query or evidence write may
        # precede the actual lock used by the previously accepted main upgrade.
        self.acquire_lock()
        self.inputs = verify_inputs(self.intent)
        require(self.properties(FULL_UNIT, tuple(self.inputs['full_terminal']['state'])) == self.inputs['full_terminal']['state'],
                'The accepted full controller no longer has its original terminal state.')
        cgroup_empty(FULL_UNIT)
        self.socket = '/var/run/postgresql'
        if self.args.mode == 'attest':
            completed = decode(protected(self.output / 'report.json'))
            require(completed.get('marker') == MARKER and completed.get('intent_sha256') == self.intent_sha and
                    completed.get('status') == 'awaiting_outer_attestation' and completed.get('binary_sha256') == NEW_BINARY_SHA,
                    'Attestation lacks the exact successful run proposal.')
            self.expected_process = completed['main_process']
            require(type(self.expected_process) is dict and set(self.expected_process) == set(OLD_PROCESS) and
                    self.expected_process != OLD_PROCESS and self.expected_process['boot_id'] == OLD_PROCESS['boot_id'] and
                    type(self.expected_process['pid']) is int and self.expected_process['pid'] > 1 and
                    type(self.expected_process['start_ticks']) is int and self.expected_process['start_ticks'] > OLD_PROCESS['start_ticks'],
                    'The proposed new main process is invalid.')
            self.expected_binary = NEW_BINARY_SHA
        self.env_values = self.read_process_environment()
        self.main_url = self.env_values.get('GOBY_DATABASE_URL', '')
        self.recovery_url = self.env_values.get('GOBY_RECOVERY_DATABASE_URL', '')
        self.validate_url(self.main_url, MAIN['database'])
        self.validate_url(self.recovery_url, RECOVERY)
        require(self.env_values.get('GOBY_WEB_DIR') == str(WEB) and self.env_values.get('GOBY_API_KEY_MASTER_KEY_FILE') == str(MASTER) and
                self.env_values.get('GOBY_RECOVERY_STATE_DIR') == str(LIFECYCLE) and
                self.env_values.get('GOBY_BACKUP_DIR') == str(STORES[2]) and self.env_values.get('GOBY_RECOVERY_OPERATIONS_DIR') == str(STORES[3]),
                'The effective main configuration names another deployment.')
        self.cluster = self.cluster_fact()
        self.socket = self.cluster['socket']
        self.hba_path = Path(self.cluster['hba_file'])
        self.pg_owner = pwd.getpwnam('postgres')
        self.hba_before = protected(self.hba_path, uid=self.pg_owner.pw_uid, gid=self.pg_owner.pw_gid, modes=(0o600, 0o640, 0o644))
        self.hba_metadata = identity(self.hba_path.lstat())
        require(not present(self.restart_path), 'The temporary restart-fence path is occupied.')

    @staticmethod
    def validate_url(raw, database):
        value = urllib.parse.urlsplit(raw)
        require(value.scheme in ('postgres', 'postgresql') and value.hostname == '127.0.0.1' and value.port == 5432 and
                value.username == database and value.password and value.path == '/' + database and
                value.query == 'sslmode=disable' and not value.fragment, 'An ordinary database URL escaped the exact main cluster.')

    def read_process_environment(self):
        fact = process_fact(self.expected_process['pid'], executable=BINARY)
        require(all(fact[key] == value for key, value in self.expected_process.items()) and fact['sha256'] == self.expected_binary and fact['uid'] == 995 and
                fact['cgroup'] == '/system.slice/' + UNIT, 'The original main process differs.')
        raw = (Path('/proc') / str(self.expected_process['pid']) / 'environ').read_bytes()
        require(len(raw) <= 1 << 20, 'The process environment exceeds its bound.')
        result = {}
        for item in raw.rstrip(b'\0').split(b'\0'):
            key, separator, value = item.partition(b'=')
            require(separator and key not in result, 'The process environment contains duplicate fields.')
            result[key] = value
        return {key.decode(): value.decode() for key, value in result.items()}

    def cluster_fact(self):
        value = decode(self.sql("SELECT jsonb_build_object('system_identifier',(SELECT system_identifier::text FROM pg_control_system()),"
            "'port',current_setting('port')::int,'version',current_setting('server_version_num')::int,'data',current_setting('data_directory'),"
            "'hba_file',current_setting('hba_file'),'config_file',current_setting('config_file'),'socket',current_setting('unix_socket_directories'),"
            "'start_microseconds',(extract(epoch FROM pg_postmaster_start_time())*1000000)::bigint);"))
        require(value['system_identifier'] == SYSTEM_IDENTIFIER and value['port'] == 5432 and value['version'] == 170011 and
                value['data'] == str(PGDATA) and value['socket'] == '/var/run/postgresql' and
                value['hba_file'] == '/etc/postgresql/17/main/pg_hba.conf' and value['config_file'] == '/etc/postgresql/17/main/postgresql.conf',
                'The peer connection is not the expected primary PostgreSQL cluster.')
        info = pwd.getpwnam('postgres')
        lines = protected(PGDATA / 'postmaster.pid', uid=info.pw_uid, gid=info.pw_gid, modes=(0o600,)).decode().splitlines()
        require(len(lines) >= 8 and lines[0].isdigit() and lines[1] == str(PGDATA) and lines[3] == '5432', 'The main postmaster PID file differs.')
        process = process_fact(int(lines[0]), executable=PG / 'postgres')
        require(process['uid'] == info.pw_uid and process['boot_id'] == OLD_PROCESS['boot_id'], 'The main postmaster ownership differs.')
        return {**value, 'process': process}

    def service_fact(self, *, stopped=False):
        protected(BINARY, self.expected_binary, modes=(0o755,))
        value = self.properties(UNIT, SERVICE_PROPERTIES)
        require(value['FragmentPath'] == str(UNIT_FILE) and value['User'] == value['Group'] == 'goby' and value['WorkingDirectory'] == '/var/lib/goby-test' and
                value['KillMode'] == 'control-group' and value['NoNewPrivileges'] == 'yes' and value['ProtectSystem'] == 'strict' and
                value['RestartForceExitStatus'] == '' and value['Restart'] == ('no' if self.restart_installed else 'on-failure'),
                'The effective main service policy changed.')
        require(value['EnvironmentFiles'] == [str(RUNTIME) + ' (ignore_errors=no)', str(RECOVERY_ENV) + ' (ignore_errors=no)'],
                'Both main environment-file bindings must remain present and ordered.')
        if stopped:
            require(value['MainPID'] == '0' and value['ActiveState'] == 'inactive' and value['SubState'] == 'dead', 'The main service is not cleanly stopped.')
            cgroup_empty(UNIT)
            require(not self.command(['/usr/bin/ss', '-H', '-ltnp', 'sport = :18096']), 'The main listener remains occupied.')
            return {'properties': value, 'process': None}
        fact = process_fact(int(value['MainPID']), executable=BINARY)
        require(all(fact[key] == expected for key, expected in self.expected_process.items()) and fact['sha256'] == self.expected_binary and
                fact['uid'] == 995 and fact['cgroup'] == '/system.slice/' + UNIT and value['ActiveState'] == 'active' and value['SubState'] == 'running',
                'The main process identity changed.')
        if self.expected_process == OLD_PROCESS:
            require(value['InvocationID'] == OLD_INVOCATION, 'The original main invocation changed.')
        lines = self.command(['/usr/bin/ss', '-H', '-ltnp', 'sport = :18096']).decode().splitlines()
        require(len(lines) == 1 and '127.0.0.1:18096' in lines[0].split() and re.search(r'\bpid=' + str(fact['pid']) + ',', lines[0]),
                'The main listener is not exclusively owned on loopback.')
        return {'properties': value, 'process': fact}

    def snapshot_database(self, database):
        names = decode(self.sql("SELECT jsonb_build_object('version',CASE WHEN to_regclass('public.schema_migrations') IS NULL THEN 0 ELSE "
            "(SELECT 0) END,'role_oid',(SELECT datdba::bigint FROM pg_database WHERE datname=current_database()),"
            "'has_migrations',to_regclass('public.schema_migrations') IS NOT NULL);", database))
        version = int(self.sql('SELECT COALESCE(max(version),0) FROM public.schema_migrations;', database)) if names['has_migrations'] else 0
        query = re.findall(r'(?m)^const catalogObjectsSQL = `([^`]+)`', self.inputs['selected'][SELECTED[1]].decode())
        require(len(query) == 1, 'The frozen catalog query is ambiguous.')
        catalog_sql = query[0].replace('$1', "'public'")
        sql = """BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL search_path=pg_catalog,public; SET LOCAL statement_timeout='20s'; SET LOCAL TIME ZONE 'UTC';
SELECT jsonb_build_object('section','metadata','value',jsonb_build_object('captured_at',clock_timestamp(),
 'database',current_database(),'server_version_num',current_setting('server_version_num')::int,
 'schemas',(SELECT jsonb_agg(nspname ORDER BY nspname COLLATE "C") FROM pg_namespace WHERE nspname !~ '^pg_' AND nspname<>'information_schema'),
 'public_schema',(SELECT jsonb_build_object('oid',oid::bigint,'owner',nspowner::bigint,'acl',nspacl) FROM pg_namespace WHERE nspname='public'),
 'role',(SELECT jsonb_build_object('database_oid',d.oid::bigint,'owner_oid',d.datdba::bigint,'database_acl',d.datacl,'properties',to_jsonb(r))
         FROM pg_database d JOIN pg_roles r ON r.oid=d.datdba WHERE d.datname=current_database()),
 'columns',(SELECT jsonb_object_agg(c.relname,ARRAY(SELECT attname::text FROM pg_attribute WHERE attrelid=c.oid AND attnum>0 AND NOT attisdropped ORDER BY attnum))
            FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='r'),
 'relations',(SELECT jsonb_object_agg(c.relname,jsonb_build_object('oid',c.oid::bigint,'owner',c.relowner::bigint,'acl',c.relacl,
              'column_acl',ARRAY(SELECT jsonb_build_object('name',attname,'acl',attacl) FROM pg_attribute WHERE attrelid=c.oid AND attnum>0 AND NOT attisdropped AND attacl IS NOT NULL ORDER BY attnum)))
              FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public')));
SELECT jsonb_build_object('section','catalog','value',(""" + catalog_sql + """));
SELECT format($q$SELECT jsonb_build_object('section','table','name',%L,'value',coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text COLLATE "C"),'[]'::jsonb)) FROM public.%I t;$q$,relname,relname)
FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND relkind='r' ORDER BY relname COLLATE "C"
\gexec
SELECT format($q$SELECT jsonb_build_object('section','sequence','name',%L,'value',jsonb_build_object('last_value',last_value,'log_cnt',log_cnt,'is_called',is_called)) FROM public.%I;$q$,relname,relname)
FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND relkind='S' ORDER BY relname COLLATE "C"
\gexec
COMMIT;
"""
        value = {'version': version, 'tables': {}, 'sequences': {}}
        for line in self.sql(sql, database).splitlines():
            row = decode(line)
            if row['section'] in ('table', 'sequence'):
                target = value['tables' if row['section'] == 'table' else 'sequences']
                require(row['name'] not in target, 'The full snapshot contains duplicate object names.')
                target[row['name']] = row['value']
            else:
                require(row['section'] not in value, 'A snapshot section repeats.')
                value[row['section']] = row['value']
        require(self.sql(unsupported_sql(names['role_oid']), database) == b'f', 'The database contains unsupported objects or grants.')
        if version:
            require(23 <= version <= 28, 'The database has an unsupported schema version.')
            path = 'internal/backuppg/catalogs/schema-%d-postgresql-17.json' % version
            trusted = decode(protected(SOURCE / path, self.inputs['source_manifest']['files'][path]))
            require(canonical(value['catalog']) == canonical(trusted['objects']), 'The database differs from its trusted published catalog.')
        else:
            require(value['catalog'] == [] and not value['tables'] and not value['sequences'], 'An unversioned database is not empty.')
        return value

    def private_fact(self, database):
        props = self.properties(UNIT, SERVICE_PROPERTIES)
        dropins = props['DropInPaths'].split()
        expected = [str(UNIT_FILE.parent / (UNIT + '.d') / name) for name in
                    ('20-application-keys.conf', '30-observability.conf', '40-backup-recovery.conf')]
        require(dropins == expected + ([str(self.restart_path)] if self.restart_installed else []), 'The main drop-in inventory changed.')
        files = {}
        paths = [(RUNTIME, 0, 0), (RECOVERY_ENV, 0, 0), (BROWSER, 0, 0), (UNIT_FILE, 0, 0), (MASTER, 995, 995)]
        paths.append((CACHE_OWNER, 0, 0))
        paths += [(Path(path), 0, 0) for path in expected]
        paths += [(Path(self.cluster[key]), self.pg_owner.pw_uid, self.pg_owner.pw_gid) for key in ('config_file',)]
        for path, uid, gid in paths:
            raw = protected(path, uid=uid, gid=gid, modes=(0o600, 0o640, 0o644))
            files[str(path)] = {'identity': identity(path.lstat()), 'sha256': sha(raw)}
        require(files[str(MASTER)]['identity']['bytes'] == 32 and files[str(MASTER)]['identity']['mode'] == 0o600,
                'The main master must remain present, private and exactly32 bytes.')
        cache = CACHE.lstat()
        require(stat.S_ISDIR(cache.st_mode) and (cache.st_uid, cache.st_gid) == (995, 986) and not cache.st_mode & 0o022 and
                files[str(CACHE_OWNER)]['sha256'] == CACHE_OWNER_SHA, 'The existing main transcode cache ownership changed.')
        trees = {str(path): tree_fact(path, uid=995, gid=None if path == MASTER.parent else 986) for path in (*STORES, PAIRING)}
        lifecycle = trees[str(LIFECYCLE)]
        require(set(lifecycle['files']) == {'.goby-lifecycle.json', '.goby-lifecycle.lock', 'generation-registry.json'} and
                not lifecycle['directories'], 'The observed main default generation changed or has a pending activation.')
        marker = decode(protected(LIFECYCLE / '.goby-lifecycle.json', uid=995, gid=986, modes=(0o600,)))
        registry = decode(protected(LIFECYCLE / 'generation-registry.json', uid=995, gid=986, modes=(0o600,)))
        exact(marker, {'version', 'deploymentId', 'lock'})
        exact(registry, {'version', 'deploymentId', 'baselineDigest', 'generations'})
        lock_identity = small_identity(LIFECYCLE_LOCK.lstat())
        require(type(marker['version']) is int and type(registry['version']) is int and marker['version'] == registry['version'] == 1 and
                marker['deploymentId'] == registry['deploymentId'] == DEPLOYMENT_ID and
                marker['lock'] == {key: lock_identity[key] for key in ('device', 'inode')} and registry['baselineDigest'] == sha(b'') and
                registry['generations'] == [], 'The local lifecycle no longer selects the sealed primary/default generation.')
        rows = [row for row in database['tables']['server_settings'] if row['key'] == 'goby.recovery.binding.v1']
        require(len(rows) == 1 and type(rows[0]['value']) is str, 'The main database generation marker is absent.')
        binding = decode(rows[0]['value'].encode())
        require(binding == {'version': 1, 'deploymentId': DEPLOYMENT_ID, 'generationId': '', 'slot': 'primary'},
                'The database and local lifecycle generations differ.')
        control = decode(protected(STORES[3] / 'current.json', uid=995, gid=986, modes=(0o600,)))
        payload = control.get('payload', {})
        require(control.get('deploymentId') == payload.get('deploymentId') == DEPLOYMENT_ID and payload.get('transition') is None and
                all(row.get('state') in ('completed', 'failed', 'cancelled', 'interrupted') for row in payload.get('operations', [])),
                'The recovery operation journal has pending work.')
        slots = {row.get('slot'): row for row in payload.get('slots', [])}
        require(set(slots) == {'primary', 'recovery'} and slots['primary'].get('state') == 'active' and
                slots['recovery'].get('state') in ('unclaimed', 'retained'), 'The recovery slot selection is not stable.')
        archive_catalog = decode(protected(STORES[2] / '.goby-backup-catalog.json', uid=995, gid=986, modes=(0o600,)))
        archives = []
        for row in archive_catalog.get('entries', []):
            metadata = row.get('metadata', {})
            require(row.get('deleting', False) is False and metadata.get('state') == 'ready' and metadata.get('verified') is True and
                    matches(r'[0-9a-f]{32}', metadata.get('id')) and matches(HASH_RE, metadata.get('digest')),
                    'An archive is incomplete or pending deletion.')
            stored = trees[str(STORES[2])]['files'].get('object-' + metadata['id'] + '.age')
            require(stored is not None and stored['sha256'] == metadata['digest'] and stored['identity']['bytes'] == metadata['size'],
                    'An encrypted archive differs from its catalog.')
            archives.append({'id': metadata['id'], 'sha256': metadata['digest'], 'bytes': metadata['size']})
        require(archives, 'The existing native archive inventory is empty.')
        pair = decode(protected(PAIRING / 'backup.json', uid=995, gid=986, modes=(0o600,)))
        require(any(row['id'] == pair.get('backup_id') for row in archives) and matches(r'[0-9a-f]{32}\.passphrase', pair.get('passphrase_file')) and
                pair['passphrase_file'] in trees[str(PAIRING)]['files'], 'The existing archive/passphrase pairing is missing.')
        return {'files': files, 'trees': trees, 'lifecycle': {'directory': str(LIFECYCLE), 'lock_file': str(LIFECYCLE_LOCK),
                'directory_identity': lifecycle['identity'], 'lock_identity': lock_identity},
                'binding_sha256': sha(rows[0]['value'].encode()), 'archives': archives, 'control_revision': control.get('revision'),
                'cache_identity': small_identity(cache)}

    def startup_fact(self, database):
        tables = database['tables']
        require(len(tables) == 35 and len(database['sequences']) == 5 and database['version'] in (27, 28), 'The main schema is incomplete.')
        for name, field in (('scan_jobs', 'status'), ('task_runs', 'state'), ('task_run_children', 'state')):
            require(not any(str(row[field]).lower() in ('waiting', 'pending', 'queued', 'running', 'stopping') for row in tables[name]),
                    'A scan or task remains active.')
        require(all(row['state'] in ('completed', 'failed', 'cancelled', 'interrupted') for row in tables['encoding_jobs']) and
                not tables['client_playback_references'], 'Playback/encoding work is not quiescent.')
        require(not any(row['enabled'] and any(trigger['task_id'] == row['id'] and trigger.get('retired_at') is None and
                    trigger.get('calculation_error', '') == '' for trigger in tables['task_triggers']) for row in tables['task_definitions']),
                'An enabled trigger can schedule work during startup.')
        require(all(row['state'] in ('Prepared', 'Stopped', 'Expired') for row in tables['play_sessions']), 'Playback remains active.')
        days = int(self.env_values.get('GOBY_ACTIVITY_RETENTION_DAYS', '30'))
        require(days == self.intent['startup']['retention_days'], 'The actual activity retention policy differs from the plan.')
        deadline = self.intent['startup']['deadline_utc']
        current = decode(self.sql("SELECT jsonb_build_object('now',to_char(clock_timestamp() AT TIME ZONE 'UTC','YYYY-MM-DD\"T\"HH24:MI:SS.US\"Z\"'),"
            f"'eligible',(SELECT count(*) FROM activity_entries WHERE created_at <= '{deadline}'::timestamptz-interval '{days} days'));", MAIN['database']))
        require(type(current['eligible']) is int and current['eligible'] == 0 and
                parse_time(current['now']) + dt.timedelta(seconds=self.intent['startup']['min_remaining_seconds']) < parse_time(deadline),
                'The finite startup/retention window is stale or unsafe.')
        return {'deadline_utc': deadline, 'retention_days': days, 'eligible': 0}

    def media_fact(self, database):
        rows = database['tables']['library_roots']
        require(1 <= len(rows) <= 128, 'The main root inventory exceeds its reviewed bound.')
        result = {}
        for row in rows:
            path = Path(row['path'])
            require(path.is_absolute() and path.is_relative_to('/opt/goby-fixtures') and path != Path('/opt/goby-fixtures') and '..' not in path.parts,
                    'A main media root escaped the fixture namespace.')
            result[row['id']] = {'mapping': {name: row[name] for name in ('id', 'library_id', 'path', 'allowed_path', 'relative_path')},
                                 'tree': tree_fact(path, uid=0, gid=0)}
        return result

    def protected_fact(self):
        values = []
        for expected in self.intent['protected_services']:
            actual = process_fact(expected['pid'], executable=Path(expected['exe']))
            require(all(actual[key] == value for key, value in expected.items() if key != 'name'), 'A protected process changed.')
            values.append({'name': expected['name'], **actual})
        require(os.readlink('/proc/self/ns/mnt') == os.readlink('/proc/1/ns/mnt'), 'The controller must use the host mount namespace.')
        return {'processes': values, 'mountinfo_sha256': sha(Path('/proc/1/mountinfo').read_bytes())}

    def capture(self, *, stopped=False):
        database = self.snapshot_database(MAIN['database'])
        role = database['metadata']['role']
        require(role['database_oid'] == MAIN['database_oid'] and role['owner_oid'] == MAIN['role_oid'] and
                role['properties']['rolname'] == MAIN['role'] and role['properties']['rolcanlogin'] is True and
                not any(role['properties'][key] for key in ('rolsuper', 'rolcreatedb', 'rolcreaterole', 'rolreplication', 'rolbypassrls')),
                'The main database/role identity or privileges changed.')
        inactive = self.snapshot_database(RECOVERY)
        require(inactive['metadata']['role']['database_oid'] == 994944 and inactive['metadata']['role']['owner_oid'] == 994943,
                'The existing recovery slot OIDs changed.')
        return {'service': self.service_fact(stopped=stopped), 'cluster': self.cluster_fact(), 'database': database, 'inactive': inactive,
                'private': self.private_fact(database), 'media': self.media_fact(database), 'startup': self.startup_fact(database),
                'protected': self.protected_fact(), 'web': tree_fact(WEB, uid=0, gid=0),
                'global': decode(self.sql(GLOBAL_SQL)), 'logs': tree_fact(DIAGNOSTICS, uid=995, gid=986, total_limit=256 << 20),
                'hba_sha256': sha(protected(self.hba_path, uid=self.pg_owner.pw_uid,
                    gid=self.pg_owner.pw_gid, modes=(self.hba_metadata['mode'],)))}

    def prepare(self):
        require(not present(self.prepared), 'The prepared evidence scope already exists.')
        before = self.capture()
        after = self.capture()
        self.compare_frames(before, after)
        self.prepared.mkdir(mode=0o700)
        sync(self.prepared.parent)
        baseline = {'marker': MARKER + '-baseline', 'version': 1, 'run_id': self.intent['run_id'],
                    'input_core_sha256': sha(canonical(input_core(self.intent))), 'prepare_input_sha256': self.intent_sha, 'captured': after}
        create(self.prepared / 'baseline.json', canonical(baseline))
        result = {'marker': MARKER + '-prepared', 'status': 'prepared', 'run_id': self.intent['run_id'],
                  'baseline': {'path': str(self.prepared / 'baseline.json'), 'sha256': sha(canonical(baseline))},
                  'business_sql_writes': 0, 'service_writes': 0, 'hba_writes': 0, 'client_gate_checked': False}
        create(self.prepared / 'report.json', canonical(result))
        return result

    @staticmethod
    def compare_frames(before, after, *, migrated=False, catalog=None, service=False, web=False):
        compare_database(before['database'], after['database'], catalog if migrated else None)
        compare_database(before['inactive'], after['inactive'])
        for key in ('cluster', 'private', 'media', 'startup', 'protected', 'global', 'hba_sha256'):
            require(canonical(before[key]) == canonical(after[key]), 'A protected main or recovery fact changed: ' + key)
        if not service:
            require(canonical(before['service']) == canonical(after['service']), 'The main service identity changed.')
        if not web:
            require(canonical(before['web']) == canonical(after['web']), 'The main native web tree changed.')
        old_logs, new_logs = before['logs'], after['logs']
        require(old_logs['identity'] == new_logs['identity'] and old_logs['directories'] == new_logs['directories'] and
                set(old_logs['files']) <= set(new_logs['files']), 'An old diagnostics file or root disappeared.')
        growth = 0
        for name, item in new_logs['files'].items():
            metadata = item['identity']
            if name in old_logs['files']:
                previous = old_logs['files'][name]
                require(all(metadata[key] == previous['identity'][key] for key in ('device', 'inode', 'uid', 'gid', 'mode', 'links')) and
                        metadata['bytes'] >= previous['identity']['bytes'], 'An old diagnostics file was replaced or truncated.')
                if item != previous:
                    raw = protected(Path(new_logs['root']) / name, uid=995, gid=986, modes=(metadata['mode'],), limit=128 << 20)
                    require(sha(raw[:previous['identity']['bytes']]) == previous['sha256'], 'Existing diagnostics bytes changed.')
                growth += metadata['bytes'] - previous['identity']['bytes']
            else:
                require(matches(r'[A-Za-z0-9_.-]+\.(?:log|jsonl)', name) and metadata['mode'] == 0o600 and metadata['bytes'] <= 8 << 20,
                        'An unrecognized diagnostics file appeared.')
                growth += metadata['bytes']
        require(growth <= 16 << 20 and len(new_logs['files']) - len(old_logs['files']) <= 32, 'Diagnostics append exceeded its explicit budget.')

    def validate_client_gate(self):
        gate = self.intent['client_gate']
        require(gate is not None, 'A real passing client acceptance gate is required.')
        def read(item, path=None):
            descriptor(item, path=path)
            require(Path(item['path']).is_relative_to(WORK), 'A client gate artifact escaped the private workspace.')
            return decode(protected(Path(item['path']), item['sha256'], limit=64 << 20))

        terminal = read(gate)
        scope = Path(terminal.get('scope', ''))
        match = re.fullmatch(r'client-library-changed-ui-source55-v([1-9][0-9]?)', scope.name)
        require(scope.parent == WORK and match is not None and terminal.get('marker') == CLIENT_GATE_MARKER and
                type(terminal.get('version')) is int and terminal['version'] == 1 and terminal.get('status') == 'passed' and
                terminal.get('mode') == CLIENT_GATE_MODE and terminal.get('library_changed_client_acceptance') is True and
                terminal.get('full_m3_complete') is False, 'No independently passing Movies and LibraryChanged terminal is present.')
        artifacts = terminal.get('artifacts')
        exact(artifacts, {'input', 'report', 'browser', 'after_snapshot'})
        docs = {name: read(artifacts[name], scope / filename) for name, filename in
                (('input', 'input.json'), ('report', 'report.json'), ('browser', 'browser/report.json'), ('after_snapshot', 'after-full.json'))}
        source, report, browser, after = (docs[key] for key in ('input', 'report', 'browser', 'after_snapshot'))
        require(source.get('marker') == 'goby-client-library-changed-input-v1' and type(source.get('version')) is int and
                source['version'] == 1 and source.get('mode') == CLIENT_GATE_MODE and source.get('root') == str(scope) and
                source.get('output') == str(scope / 'browser'), 'The passing client input belongs to another scope.')
        candidate = source.get('candidate', {})
        pin = next(item for item in self.intent['protected_services'] if item['name'] == 'candidate')
        require(candidate.get('source') == str(SOURCE) and candidate.get('source_manifest_sha256') == SOURCE_SHA and
                candidate.get('binary_sha256') == NEW_BINARY_SHA and candidate.get('process') == {key: pin[key] for key in OLD_PROCESS} and
                matches(r'[0-9a-f]{32}', candidate.get('invocation_id')) and
                candidate.get('base_url') == 'http://127.0.0.1:18196' and candidate.get('direct_url') == 'http://127.0.0.1:18198',
                'Client acceptance used another source, candidate process or endpoint.')
        for value, marker in ((report, 'goby-client-library-changed-observation-v1'), (browser, 'goby-client-library-changed-report-v1')):
            require(value.get('marker') == marker and type(value.get('version')) is int and value['version'] == 1 and
                    value.get('mode') == CLIENT_GATE_MODE and value.get('input_sha256') == artifacts['input']['sha256'] and
                    value.get('controller') == source.get('controller') and value.get('full_m3_complete') is False and
                    value.get('client_acceptance') is False, 'An original client report disagrees with its input or scope.')
        ledger = {'new_sessions': 2, 'new_devices': 1, 'new_audits': 6, 'metadata_revision_delta': 2,
                  'old_rows_sequences_private_preserved': True, 'owned_sessions_closed': True}
        require(report.get('status') == 'passed' and report.get('worker_chain_ledger_passed') is True and
                report.get('acceptance_ready_for_outer_terminal') is True and report.get('outer_controller_terminal_required') is True and
                report.get('library_changed_client_acceptance') is False and report.get('errors') == [] and
                report.get('restoration') == 'confirmed' and canonical(report.get('ledger')) == canonical(ledger) and
                all(report.get(key) is False for key in ('automatic_retry', 'sql_business_writes', 'candidate_or_primary_service_writes',
                                                        'restoration_required', 'browser_fallback_used')) and
                report.get('candidate_process') == candidate['process'] and report.get('candidate_invocation') == candidate['invocation_id'] and
                report.get('state_sha256') == candidate.get('state_sha256') and report.get('authority') == source.get('authority') and
                report.get('evidence', {}).get('browser-report.json') == artifacts['browser'] and
                report.get('evidence', {}).get('after-full.json') == artifacts['after_snapshot'],
                'The actual client controller did not complete the exact edit, restoration and credential ledger.')
        require(browser.get('result') == 'passed' and browser.get('library_changed_client_acceptance') is True and
                browser.get('failure') is None and browser.get('restoration') == 'confirmed' and browser.get('candidate') == candidate and
                browser.get('authority') == source['authority'] and browser.get('target') == source.get('target') and
                browser.get('node_process') == report.get('node_process') and browser.get('actor', {}).get('page_error_count') == 0 and
                all(browser.get('closure', {}).get(key) is True for key in ('context_closed', 'browser_closed', 'proxy_closed')),
                'The original browser did not pass or retain its clean terminal.')
        worker = report.get('worker_terminal', {})
        require(worker.get('MainPID') == '0' and worker.get('ExecMainStatus') == '0' and worker.get('Result') == 'success' and
                worker.get('cgroup_empty') is True, 'The original client worker was not successfully terminal.')
        controller_intent = read(report.get('evidence', {}).get('intent.json'), scope / 'intent.json')
        require(controller_intent.get('marker') == report['marker'] and controller_intent.get('mode') == CLIENT_GATE_MODE and
                controller_intent.get('root') == str(scope) and controller_intent.get('controller') == source['controller'],
                'The original controller intent is not bound to the client report.')
        unit = 'goby-client-library-changed-ui-source55-controller-v' + match[1] + '.service'
        self.client_gate_unit = unit
        state = terminal.get('state', {})
        names = ('MainPID', 'InvocationID', 'ActiveState', 'SubState', 'ExecMainStatus', 'Result', 'RemainAfterExit', 'ControlGroup')
        require(terminal.get('unit') == unit and source.get('controller', {}).get('unit') == unit and
                state.get('MainPID') == '0' and state.get('ExecMainStatus') == '0' and state.get('Result') == 'success' and
                state.get('ActiveState') == 'active' and state.get('SubState') == 'exited' and state.get('RemainAfterExit') == 'yes' and
                state.get('ControlGroup') in ('', '/system.slice/' + unit) and
                matches(r'[0-9a-f]{32}', state.get('InvocationID')) and state['InvocationID'] == controller_intent.get('controller_invocation') and
                terminal.get('recursive_cgroup_empty') is True and self.properties(unit, names) == {key: state[key] for key in names},
                'The independently observed original client controller is not still terminal.')
        cgroup_empty(unit)
        authority = source['authority']
        upgrade_intent = read(authority['upgrade_intent'], WORK / 'client-schema28-source55-tool-05/intent.json')
        upgrade_root = Path(upgrade_intent.get('output', ''))
        require(upgrade_root.parent == WORK and matches(r'client-schema28-source55-upgrade-' + RUN_RE, upgrade_root.name),
                'The candidate upgrade has no exact owned output.')
        upgraded = read(authority['upgrade_report'], upgrade_root / 'report.json')
        attested = read(authority['upgrade_attestation'], upgrade_root / 'attestation.json')
        original = read(authority['current_snapshot'], upgrade_root / 'after-full.json')
        require(authority['upgrade_attestation']['sha256'] == CLIENT_UPGRADE_ATTEST_SHA and
                upgraded.get('status') == 'awaiting_outer_attestation' and attested.get('status') == 'passed' and
                attested.get('marker') == upgraded.get('marker') == 'goby-client-schema28-source55-upgrade-v1' and
                upgraded.get('intent_sha256') == attested.get('intent_sha256') == authority['upgrade_intent']['sha256'] and
                attested.get('report_sha256') == authority['upgrade_report']['sha256'] and attested.get('recursive_cgroup_empty') is True and
                upgraded.get('new_process') == candidate['process'] and upgraded.get('state_sha256') == attested.get('state_sha256') == candidate['state_sha256'] and
                upgraded.get('publication') == candidate.get('publication') and upgraded.get('binary', {}).get('sha256') == NEW_BINARY_SHA and
                upgraded.get('evidence', {}).get('after-full.json') == authority['current_snapshot'] and
                upgrade_intent.get('source', {}).get('root') == str(SOURCE) and upgrade_intent['source'].get('manifest_sha256') == SOURCE_SHA,
                'The browser authority does not bind the accepted candidate upgrade.')
        state_doc = decode(protected(WORK / 'client-fixture.json', candidate['state_sha256']))
        require(type(state_doc.get('schema')) is int and state_doc['schema'] == 28 and state_doc.get('process') == candidate['process'] and
                state_doc.get('binary_sha256') == NEW_BINARY_SHA and state_doc.get('runtime_sha256') == candidate.get('runtime_sha256') and
                state_doc.get('schema28_upgrade', {}).get('intent_sha256') == authority['upgrade_intent']['sha256'] and
                state_doc.get('schema28_source', {}).get('source_manifest_sha256') == SOURCE_SHA and
                state_doc['schema28_source'].get('catalog_sha256') == CATALOG28_SHA,
                'The preserved candidate state no longer identifies its independently accepted upgrade.')
        for snapshot in (original, after):
            require(type(snapshot.get('schema')) is int and snapshot['schema'] == 28 and
                    type(snapshot.get('database', {}).get('tables')) is dict and type(snapshot['database'].get('sequences')) is dict and
                    snapshot['database'].get('unsupported') is False and snapshot['database'].get('catalog') == self.inputs['catalog']['objects'],
                    'A bound client snapshot is not a complete schema28 snapshot.')
        require(set(after['database']['tables']) == set(original['database']['tables']) and
                set(after['database']['sequences']) == set(original['database']['sequences']),
                'The final client snapshot omits an accepted old table or sequence.')
        return {'terminal': gate, 'artifacts': artifacts, 'mode': CLIENT_GATE_MODE,
                'library_changed_client_acceptance': True, 'full_m3_complete': False}

    def preflight(self):
        self.client_gate = self.validate_client_gate()
        item = self.intent['main_baseline']
        self.baseline = decode(protected(Path(item['path']), item['sha256']))
        require(self.baseline.get('marker') == MARKER + '-baseline' and self.baseline.get('run_id') == self.intent['run_id'] and
                self.baseline.get('input_core_sha256') == sha(canonical(input_core(self.intent))), 'The prepared baseline belongs to another main scope.')
        self.before = self.baseline['captured']
        require(self.before['database']['version'] == 27, 'The prepared main baseline is not schema27.')
        current = self.capture()
        self.compare_frames(self.before, current)
        require(not present(self.output), 'This main execution scope is consumed.')
        return {'status': 'preflight_passed', 'main_baseline_sha256': item['sha256'], 'client_gate': self.client_gate, 'writes': 0}

    def save(self, name, value):
        require(self.created and matches(r'[a-z0-9][a-z0-9.-]{0,95}', name), 'An evidence name escaped the main run.')
        require(directory(self.output) == self.output_identity, 'The owned main evidence root changed.')
        raw = value if type(value) is bytes else canonical(value)
        create(self.output / name, raw)
        self.records[name] = {'path': str(self.output / name), 'sha256': sha(raw)}
        return self.records[name]

    def phase(self, name):
        require(name in ('backed_up', 'rehearsed', 'restart_fence_install', 'stop_requested', 'stopped', 'migrate_requested',
                        'migrated', 'installed', 'start_requested', 'started', 'restart_fence_restore', 'complete'), 'An unknown durable phase was requested.')
        self.save('phase-' + name.replace('_', '-') + '.json', {'marker': MARKER, 'phase': name, 'service_actions': list(self.fence.actions)})
        self.phase_name, self.fence.stage = name, name

    def check(self, *, stopped=False, migrated=False, web=False):
        current = self.capture(stopped=stopped)
        self.compare_frames(self.before, current, migrated=migrated, catalog=self.inputs['catalog'], service=self.restart_installed or stopped or migrated, web=web)
        expected = dict(self.before['service']['properties'])
        if self.restart_installed:
            expected.update(Restart='no', DropInPaths=expected['DropInPaths'] + ' ' + str(self.restart_path))
        require(all(current['service']['properties'][key] == value for key, value in expected.items() if key not in SERVICE_DYNAMIC),
                'An undeclared effective service policy changed.')
        return current

    def begin(self):
        require(not present(self.output), 'The new main run already exists.')
        directory(BACKUPS.parent, stat.S_IMODE(BACKUPS.parent.lstat().st_mode))
        if not present(BACKUPS):
            BACKUPS.mkdir(mode=0o700)
            sync(BACKUPS.parent)
        directory(BACKUPS)
        self.output.mkdir(mode=0o700)
        sync(BACKUPS)
        self.output_identity, self.created = directory(self.output), True
        self.private.mkdir(mode=0o700)
        sync(self.output)
        self.save('intent.json', self.intent)
        self.save('baseline.json', self.baseline)
        self.save('controller.json', self.controller)

    def backup_materials(self):
        self.save('hba-before', self.hba_before)
        root = self.private / 'materials'
        root.mkdir(mode=0o700)
        sources = {name: item for name, item in self.before['private']['files'].items()}
        sources[str(BINARY)] = {'identity': identity(BINARY.lstat()), 'sha256': OLD_BINARY_SHA}
        for tree in (*self.before['private']['trees'].values(), self.before['web'], self.before['logs']):
            for name, item in tree['files'].items():
                sources[str(Path(tree['root']) / name)] = item
        materials = {}
        for index, (name, item) in enumerate(sorted(sources.items())):
            metadata = item['identity']
            raw = protected(Path(name), item['sha256'], uid=metadata['uid'], gid=metadata['gid'], modes=(metadata['mode'],), limit=512 << 20)
            output = root / ('file-%05d' % index)
            create(output, raw)
            materials[name] = {'path': str(output), 'sha256': sha(raw), 'original_identity': metadata}
        self.materials = materials
        create(self.private / 'materials.json', canonical(materials))
        self.save('materials-proof.json', {'sha256': sha(canonical(materials)), 'files': len(materials), 'master_retained': str(MASTER) in materials})

    def verify_materials(self):
        require(sha(protected(self.output / 'hba-before')) == self.before['hba_sha256'], 'The original main HBA backup changed.')
        proof = decode(protected(self.output / 'materials-proof.json'))
        raw = protected(self.private / 'materials.json')
        materials = decode(raw)
        require(type(materials) is dict and proof == {'sha256': sha(raw), 'files': len(materials), 'master_retained': True},
                'The sealed complete private-material inventory changed.')
        expected = dict(self.before['private']['files'])
        for tree in (*self.before['private']['trees'].values(), self.before['web'], self.before['logs']):
            expected.update({str(Path(tree['root']) / name): item for name, item in tree['files'].items()})
        require(set(materials) == set(expected) | {str(BINARY)}, 'A preserved private file, archive, old asset or binary is missing.')
        names = set()
        for index, (source, item) in enumerate(sorted(materials.items())):
            exact(item, {'path', 'sha256', 'original_identity'})
            path = self.private / 'materials' / ('file-%05d' % index)
            require(item['path'] == str(path), 'A private-material path escaped its bounded inventory.')
            original = expected.get(source)
            require(item['sha256'] == (original['sha256'] if original is not None else OLD_BINARY_SHA) and
                    (original is None or item['original_identity'] == original['identity']), 'A private backup no longer matches its captured source.')
            saved = protected(path, item['sha256'], modes=(0o600,), limit=512 << 20)
            require(len(saved) == item['original_identity']['bytes'], 'A private backup size differs from the original file.')
            names.add(path.name)
        tree = tree_fact(self.private / 'materials', uid=0, gid=0)
        require(not tree['directories'] and set(tree['files']) == names and tree['identity']['mode'] == 0o700 and
                all(item['identity']['mode'] == 0o600 for item in tree['files'].values()), 'The backup contains an extra or unsafe material member.')
        backup = decode(protected(self.private / 'helper-backup.json'))
        dump = backup.get('dump', {})
        require(dump.get('path') == str(self.private / 'main-schema27.dump') and type(dump.get('bytes')) is int and
                0 < dump['bytes'] <= 512 << 20 and len(protected(Path(dump['path']), dump['sha256'], modes=(0o600,), limit=512 << 20)) == dump['bytes'],
                'The independently rehearsed main dump is not retained intact.')

    def verify_readiness_receipts(self):
        routes = [('/readyz', None), ('/emby/System/Info/Public', None), *self.inputs['frontend_routes']]
        require(len(routes) == 5, 'The main anonymous readiness budget differs.')
        for index, (route, asset) in enumerate(routes, 1):
            request = decode(protected(self.output / ('http-%d-intent.json' % index)))
            response = decode(protected(self.output / ('http-%d-result.json' % index)))
            exact(response, {'method', 'path', 'status', 'sha256', 'bytes', 'cookie_present'})
            require(request == {'method': 'GET', 'path': route, 'authenticated': False} and response['method'] == 'GET' and
                    response['path'] == route and type(response['status']) is int and response['status'] == 200 and
                    response['cookie_present'] is False and type(response['bytes']) is int and 0 < response['bytes'] <= 8 << 20 and
                    matches(HASH_RE, response['sha256']) and (asset is None or response['sha256'] == self.inputs['web']['files'][asset]),
                    'A stored anonymous main readiness result is incomplete.')

    def helper_document(self):
        database = self.before['database']
        schema = database['metadata']['public_schema']
        main = {**MAIN, 'schema_oid': schema['oid'], 'schema_owner_oid': schema['owner'], 'recovery_deployment_id': DEPLOYMENT_ID,
                'recovery_binding_sha256': self.before['private']['binding_sha256']}
        cluster = {'system_identifier': SYSTEM_IDENTIFIER, 'postmaster_pid': self.cluster['process']['pid'],
                   'postmaster_start_ticks': str(self.cluster['process']['start_ticks']),
                   'postmaster_start_microseconds': self.cluster['start_microseconds'], 'boot_id': self.cluster['process']['boot_id'],
                   'data_directory': str(PGDATA), 'postgresql_version_num': 170011, 'port': 5432}
        value = {'marker': 'goby-main-schema28-helper-intent-v1', 'version': 1, 'run_id': self.intent['run_id'],
                 'source_manifest_sha256': SOURCE_SHA, 'source55_binary_sha256': NEW_BINARY_SHA,
                 'helper_sha256': self.intent['helper']['binary']['sha256'], 'evidence_root': str(self.output),
                 'catalog27': {'path': str(SOURCE / SELECTED[2]), 'sha256': CATALOG27_SHA},
                 'catalog28': {'path': str(SOURCE / SELECTED[3]), 'sha256': CATALOG28_SHA}, 'cluster': cluster, 'main': main,
                 'rehearsal': {'database': self.rehearsal_name, 'role': self.rehearsal_name, 'owner_tag': MARKER + ':' + self.intent['run_id']},
                 'lifecycle': self.before['private']['lifecycle']}
        self.helper_intent = value
        create(self.private / 'helper-intent.json', canonical(value))
        create(self.private / 'helper-credentials.json', canonical({'main_url': self.main_url,
            'rehearsal_url': f'postgresql://{self.rehearsal_name}:{self.password}@127.0.0.1:5432/{self.rehearsal_name}?sslmode=disable'}))

    def helper(self, mode, label=None, *, baseline=False, target=False):
        label = label or mode
        require(mode in ('backup', 'inspect', 'rehearse', 'migrate') and matches(r'[a-z][a-z-]{0,32}', label), 'An undeclared helper action was requested.')
        path = self.private / ('helper-' + label + '.json')
        require(not present(path), 'A helper result already exists; do not replay it.')
        protected(TOOL / 'migrate-main-schema28', self.intent['helper']['binary']['sha256'], modes=(0o755,))
        raw = protected(self.private / 'helper-intent.json')
        require(decode(raw) == self.helper_intent, 'The helper intent changed.')
        args = [TOOL / 'migrate-main-schema28', '--mode', mode, '--intent', self.private / 'helper-intent.json',
                '--intent-sha256', sha(raw), '--credentials', self.private / 'helper-credentials.json', '--output', path]
        if baseline:
            args += ['--baseline', self.private / 'helper-backup.json', '--baseline-sha256', self.backup_sha]
        if target:
            args += ['--target-receipt', self.private / 'helper-target.json', '--target-receipt-sha256', sha(protected(self.private / 'helper-target.json'))]
        descriptors = ()
        if mode == 'migrate':
            require(self.phase_name == 'migrate_requested' and len(self.lifecycle_fds) == 2, 'Main migration lacks the lifecycle fences.')
            descriptors = tuple(self.lifecycle_fds)
            args += ['--lifecycle-directory-fd', str(descriptors[0]), '--lifecycle-lock-fd', str(descriptors[1])]
        self.command(args, timeout=600, pass_fds=descriptors)
        data = protected(path)
        value = decode(data)
        require(value.get('marker') == 'goby-main-schema28-helper-v1' and value.get('run_id') == self.intent['run_id'] and
                value.get('intent_sha256') == sha(raw) and value.get('mode') == mode and
                value.get('status') == {'backup': 'backed_up', 'inspect': 'inspected', 'rehearse': 'rehearsed', 'migrate': 'committed'}[mode] and
                value.get('source_manifest_sha256') == SOURCE_SHA and value.get('source55_binary_sha256') == NEW_BINARY_SHA and
                value.get('helper_sha256') == self.intent['helper']['binary']['sha256'], 'The helper did not publish the exact successful result.')
        if baseline:
            require(value.get('baseline_sha256') == self.backup_sha, 'A helper used another baseline.')
            if mode == 'inspect':
                require(value['state_sha256'] == self.backup_report['state_sha256'], 'The current state differs from the actual exported backup.')
            else:
                require(value.get('before_state_sha256') == self.backup_report['state_sha256'] == value.get('preserved_state_sha256') and
                        value.get('root_bindings_no_auto_binding') is True and value.get('historical_audit_defaults') is True,
                        'A migration/rehearsal omitted full preservation.')
        if mode == 'migrate':
            require(value.get('lifecycle_fence_verified') is True, 'The migration did not verify both inherited lifecycle fences.')
        self.records['helper-' + label + '.json'] = {'path': str(path), 'sha256': sha(data)}
        return value

    def backup(self):
        self.helper_document()
        self.backup_report = self.helper('backup')
        require(self.backup_report.get('same_exported_snapshot') is True and self.backup_report.get('source_schema_version') == 27,
                'The main facts and dump do not share one exported snapshot.')
        dump = self.backup_report['dump']
        require(dump['path'] == str(self.private / 'main-schema27.dump') and type(dump['bytes']) is int and 0 < dump['bytes'] <= 512 << 20,
                'The main dump descriptor is invalid.')
        require(len(protected(Path(dump['path']), dump['sha256'], limit=512 << 20)) == dump['bytes'], 'The completed main dump changed.')
        self.backup_sha = sha(protected(self.private / 'helper-backup.json'))
        self.check()
        self.phase('backed_up')

    def allowed_ddl(self):
        name, tag = self.rehearsal_name, MARKER + ':' + self.intent['run_id']
        return {f"BEGIN; SET LOCAL password_encryption='scram-sha-256'; CREATE ROLE {name} LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 12 PASSWORD '{self.password}'; COMMENT ON ROLE {name} IS '{tag}'; COMMIT;",
                f"CREATE DATABASE {name} OWNER {name} TEMPLATE template0 ENCODING 'UTF8';",
                f"COMMENT ON DATABASE {name} IS '{tag}'; REVOKE CONNECT,TEMPORARY ON DATABASE {name} FROM PUBLIC;",
                f'ALTER DATABASE {name} ALLOW_CONNECTIONS false;', f'DROP DATABASE {name};', f'DROP ROLE {name};'}

    def pair_rows(self):
        name = self.rehearsal_name
        return decode(self.sql("SELECT jsonb_build_object('role',(SELECT jsonb_build_object('oid',oid::bigint,'name',rolname,'login',rolcanlogin,"
            "'super',rolsuper,'createdb',rolcreatedb,'createrole',rolcreaterole,'replication',rolreplication,'bypass',rolbypassrls,"
            "'inherit',rolinherit,'limit',rolconnlimit,'config',rolconfig,'valid_until',rolvaliduntil,'tag',shobj_description(oid,'pg_authid'),"
            "'verifier_sha256',(SELECT encode(sha256(convert_to(rolpassword,'UTF8')),'hex') FROM pg_authid a WHERE a.oid=pg_roles.oid)) "
            f"FROM pg_roles WHERE rolname='{name}'),'database',(SELECT jsonb_build_object('oid',oid::bigint,'owner',datdba::bigint,"
            "'allow',datallowconn,'acl',datacl,'tag',shobj_description(oid,'pg_database'),'grants',"
            "(SELECT jsonb_agg(jsonb_build_object('grantor',grantor::bigint,'grantee',grantee::bigint,'privilege',privilege_type,'grantable',is_grantable) "
            "ORDER BY privilege_type) FROM aclexplode(coalesce(datacl,acldefault('d',datdba))))) "
            f"FROM pg_database WHERE datname='{name}'));"))

    def pair_check(self, *, closed=False):
        rows = self.pair_rows()
        role, database = rows['role'], rows['database']
        name, tag = self.rehearsal_name, MARKER + ':' + self.intent['run_id']
        expected = {'oid': self.pair['role_oid'], 'name': name, 'login': True, 'super': False, 'createdb': False, 'createrole': False,
                    'replication': False, 'bypass': False, 'inherit': False, 'limit': 12, 'config': None, 'valid_until': None, 'tag': tag,
                    'verifier_sha256': self.pair['verifier_sha256']}
        require(canonical(role) == canonical(expected) and database == {**self.pair['database'], 'allow': not closed}, 'The exact rehearsal pair changed.')
        grants = database['grants']
        require(database['owner'] == self.pair['role_oid'] and database['oid'] == self.pair['database_oid'] and database['tag'] == tag and
                len(grants) == 3 and {item['privilege'] for item in grants} == {'CREATE', 'CONNECT', 'TEMPORARY'} and
                all(item['grantor'] == item['grantee'] == self.pair['role_oid'] and item['grantable'] is False for item in grants),
                'The new rehearsal database grants are not owner-only.')
        roid, dbid = self.pair['role_oid'], self.pair['database_oid']
        require(self.sql(f"SELECT (SELECT count(*) FROM pg_auth_members WHERE member={roid} OR roleid={roid} OR grantor={roid})+"
            f"(SELECT count(*) FROM pg_db_role_setting WHERE setrole={roid} OR setdatabase={dbid})+"
            f"(SELECT count(*) FROM pg_shdepend WHERE refclassid='pg_authid'::regclass AND refobjid={roid} AND NOT "
            f"(dbid={dbid} OR (dbid=0 AND classid='pg_database'::regclass AND objid={dbid} AND deptype='o')));") == b'0',
            'The rehearsal pair has external dependencies or settings.')
        current = decode(self.sql(GLOBAL_SQL))
        for key, oid in (('roles', roid), ('databases', dbid)):
            selected = [row for row in current[key] if row['name'] == name]
            require(len(selected) == 1 and str(selected[0]['oid']) == str(oid), 'The global rehearsal OID changed.')
            current[key] = [row for row in current[key] if row['name'] != name]
        require(canonical(current) == canonical(self.before['global']), 'An unrelated global5432 catalog row changed.')

    def pair_journal(self):
        self.sequence += 1
        self.save('pair-%03d.json' % self.sequence, {'pair': self.pair, 'hba_changed': self.hba_changed})

    def replace_hba(self, expected, replacement):
        require(protected(self.hba_path, uid=self.pg_owner.pw_uid, gid=self.pg_owner.pw_gid, modes=(self.hba_metadata['mode'],)) == expected,
                'The main HBA changed outside its owned transition.')
        temp = self.hba_path.with_name('pg_hba.conf.main28-' + self.intent['run_id'] + ('-restore' if replacement == self.hba_before else '-install'))
        fd = os.open(temp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, self.hba_metadata['mode'])
        with os.fdopen(fd, 'wb') as stream:
            os.fchown(stream.fileno(), self.pg_owner.pw_uid, self.pg_owner.pw_gid)
            os.fchmod(stream.fileno(), self.hba_metadata['mode'])
            stream.write(replacement)
            stream.flush()
            os.fsync(stream.fileno())
        require(protected(self.hba_path, uid=self.pg_owner.pw_uid, gid=self.pg_owner.pw_gid, modes=(self.hba_metadata['mode'],)) == expected,
                'The main HBA changed before atomic publication.')
        os.replace(temp, self.hba_path)
        sync(self.hba_path.parent)
        require(self.sql('SELECT pg_reload_conf();') == b't' and self.sql('SELECT count(*) FROM pg_hba_file_rules WHERE error IS NOT NULL;') == b'0',
                'The main HBA reload failed.')

    def restore_hba(self):
        if self.hba_changed:
            require(not self.hba_restore_reserved, 'The HBA restoration must not be retried.')
            self.hba_restore_reserved = True
            self.replace_hba(self.hba_after, self.hba_before)
            self.hba_changed = False
            self.pair_journal()

    def rehearse(self):
        require(self.pair_rows() == {'role': None, 'database': None}, 'A rehearsal name already exists.')
        self.pair['phase'] = 'role_pending'
        self.pair_journal()
        name, tag = self.rehearsal_name, MARKER + ':' + self.intent['run_id']
        statements = self.allowed_ddl()
        self.sql(next(value for value in statements if value.startswith('BEGIN;')), write=True)
        row = self.pair_rows()
        require(row['database'] is None and row['role']['tag'] == tag, 'The new role acknowledgement is ambiguous.')
        require(matches(HASH_RE, row['role']['verifier_sha256']), 'The new role verifier proof is absent.')
        self.pair.update(role_oid=row['role']['oid'], verifier_sha256=row['role']['verifier_sha256'], phase='database_pending')
        self.pair_journal()
        self.sql(f"CREATE DATABASE {name} OWNER {name} TEMPLATE template0 ENCODING 'UTF8';", write=True)
        row = self.pair_rows()
        require(row['database']['owner'] == self.pair['role_oid'] and row['database']['tag'] is None, 'The new database acknowledgement is ambiguous.')
        self.pair.update(database_oid=row['database']['oid'], phase='tag_pending')
        self.pair_journal()
        self.sql(f"COMMENT ON DATABASE {name} IS '{tag}'; REVOKE CONNECT,TEMPORARY ON DATABASE {name} FROM PUBLIC;", write=True)
        self.pair.update(database=self.pair_rows()['database'], phase='owned')
        self.pair_check()
        empty = self.snapshot_database(name)
        require(empty['version'] == 0 and empty['catalog'] == [], 'The fresh rehearsal is not empty.')
        target = {'marker': 'goby-main-schema28-rehearsal-target-v1', 'version': 1, 'run_id': self.intent['run_id'],
                  'intent_sha256': sha(protected(self.private / 'helper-intent.json')), 'database': name, 'role': name,
                  'database_oid': self.pair['database_oid'], 'role_oid': self.pair['role_oid'],
                  'schema_oid': empty['metadata']['public_schema']['oid'], 'schema_owner_oid': empty['metadata']['public_schema']['owner'], 'owner_tag': tag}
        create(self.private / 'helper-target.json', canonical(target))
        self.hba_after = (f'# {tag}\nhost {name} {name} 127.0.0.1/32 scram-sha-256\n'.encode() + self.hba_before)
        self.hba_changed = True
        self.pair_journal()
        self.replace_hba(self.hba_before, self.hba_after)
        self.helper('rehearse', baseline=True, target=True)
        self.pair_check()
        final = self.snapshot_database(name)
        self.save('rehearsal-full.json', final)
        self.restore_hba()
        self.dispose_rehearsal(final)
        self.phase('rehearsed')

    def dispose_rehearsal(self, expected):
        self.pair_check()
        compare_database(expected, self.snapshot_database(self.rehearsal_name))
        db, role, name = self.pair['database_oid'], self.pair['role_oid'], self.rehearsal_name
        require(self.sql(f"SELECT (SELECT count(*) FROM pg_stat_activity WHERE datid={db})+(SELECT count(*) FROM pg_prepared_xacts WHERE database='{name}')+"
                         f"(SELECT count(*) FROM pg_replication_slots WHERE database='{name}');") == b'0', 'The rehearsal still has unowned sessions/work.')
        self.sql(f'ALTER DATABASE {name} ALLOW_CONNECTIONS false;', write=True)
        self.pair['phase'] = 'closed'
        self.pair_journal()
        self.pair_check(closed=True)
        require(self.sql(f'SELECT count(*) FROM pg_stat_activity WHERE datid={db};') == b'0', 'A connection raced rehearsal closure.')
        self.sql(f'DROP DATABASE {name};', write=True)
        self.pair['phase'] = 'database_removed'
        self.pair_journal()
        require(self.sql(f"SELECT (SELECT count(*) FROM pg_shdepend WHERE refclassid='pg_authid'::regclass AND refobjid={role})+"
                         f"(SELECT count(*) FROM pg_auth_members WHERE member={role} OR roleid={role} OR grantor={role});") == b'0', 'The role still has dependencies.')
        row = self.pair_rows()
        require(row['database'] is None and row['role']['oid'] == role and row['role']['tag'] == MARKER + ':' + self.intent['run_id'], 'The role identity changed before drop.')
        self.sql(f'DROP ROLE {name};', write=True)
        require(self.pair_rows() == {'role': None, 'database': None} and canonical(decode(self.sql(GLOBAL_SQL))) == canonical(self.before['global']),
                'Ordinary cleanup did not restore the global catalog.')
        self.pair['phase'] = 'removed'
        self.pair_journal()

    def assert_baseline(self, label, *, stopped=False):
        current = self.check(stopped=stopped)
        self.save(label + '-full.json', current)
        self.helper('inspect', label, baseline=True)
        return current

    def install_restart_fence(self):
        require(self.pair['phase'] == 'removed' and not self.hba_changed and not present(self.restart_path), 'Rehearsal cleanup must finish before the restart fence.')
        self.phase('restart_fence_install')
        directory(self.restart_path.parent.parent, 0o755)
        self.restart_parent_created = not present(self.restart_path.parent)
        if self.restart_parent_created:
            self.restart_path.parent.mkdir(mode=0o755)
            os.chmod(self.restart_path.parent, 0o755)
            sync(self.restart_path.parent.parent)
        self.restart_parent_identity = directory(self.restart_path.parent, 0o755)
        create(self.restart_path, self.restart_bytes, 0o644)
        self.restart_identity = identity(self.restart_path.lstat())
        self.restart_installed = True
        self.save('restart-fence.json', {'path': str(self.restart_path), 'file_identity': self.restart_identity,
                  'parent_identity': self.restart_parent_identity, 'parent_created': self.restart_parent_created,
                  'sha256': sha(self.restart_bytes)})
        self.command(['/usr/bin/systemctl', 'daemon-reload'])
        self.service_fact()
        self.check()

    def restore_restart_fence(self):
        require(self.restart_installed and not self.restart_restore_reserved, 'Restart policy restoration cannot be repeated.')
        self.restart_restore_reserved = True
        self.phase('restart_fence_restore')
        require(protected(self.restart_path, modes=(0o644,)) == self.restart_bytes and identity(self.restart_path.lstat()) == self.restart_identity,
                'The exact owned restart fence changed.')
        self.restart_path.unlink()
        sync(self.restart_path.parent)
        if self.restart_parent_created:
            require(directory(self.restart_path.parent, 0o755) == self.restart_parent_identity, 'The owned restart directory was replaced.')
            require(not list(self.restart_path.parent.iterdir()), 'The created restart directory acquired an unowned member.')
            self.restart_path.parent.rmdir()
            sync(self.restart_path.parent.parent)
        self.restart_installed = False
        self.command(['/usr/bin/systemctl', 'daemon-reload'])
        self.service_fact()

    def service(self, action):
        require(action in ('stop', 'start'), 'Only one stop and start are allowed.')
        if action == 'stop':
            self.service_fact()
        else:
            require(not self.lifecycle_fds, 'Lifecycle fences must be released immediately before start.')
            self.service_fact(stopped=True)
        self.phase(action + '_requested')
        self.command(['/usr/bin/systemctl', action, UNIT], timeout=120 if action == 'stop' else 60)
        if action == 'stop':
            self.service_fact(stopped=True)
            deadline = time.monotonic() + 30
            while True:
                pending = int(self.sql(f"SELECT (SELECT count(*) FROM pg_stat_activity WHERE datid={MAIN['database_oid']})+"
                    "(SELECT count(*) FROM pg_prepared_xacts WHERE database='goby_test')+(SELECT count(*) FROM pg_replication_slots WHERE database='goby_test');"))
                if pending == 0:
                    break
                require(time.monotonic() < deadline, 'Main connections did not drain; no backend will be terminated.')
                time.sleep(0.25)
            self.phase('stopped')
            return
        deadline, observed = time.monotonic() + 60, None
        while time.monotonic() < deadline:
            props = self.properties(UNIT, ('MainPID', 'InvocationID', 'ActiveState', 'SubState'))
            require(props['ActiveState'] not in ('failed', 'inactive'), 'The first main launch failed.')
            if int(props['MainPID']) > 1:
                fact = process_fact(int(props['MainPID']), executable=BINARY)
                selected = {key: fact[key] for key in ('pid', 'start_ticks', 'boot_id')}
                require(selected != OLD_PROCESS and selected['boot_id'] == OLD_PROCESS['boot_id'] and selected['start_ticks'] > OLD_PROCESS['start_ticks'] and
                        fact['sha256'] == NEW_BINARY_SHA and props['InvocationID'] != OLD_INVOCATION, 'The replacement process is not fresh.')
                require(observed is None or observed == (selected, props['InvocationID']), 'The first main launch restarted.')
                observed = (selected, props['InvocationID'])
                if self.command(['/usr/bin/ss', '-H', '-ltnp', 'sport = :18096']):
                    self.expected_process = selected
                    self.new_invocation = props['InvocationID']
                    self.service_fact()
                    self.phase('started')
                    return
            time.sleep(0.25)
        raise Failure('The first main launch did not become ready within its bound.')

    def acquire_lifecycle(self):
        require(not self.lifecycle_fds and self.phase_name == 'stopped', 'Lifecycle ownership must follow the sole stop.')
        proof = self.before['private']['lifecycle']
        for path, identity_key, flags in ((LIFECYCLE, 'directory_identity', os.O_RDONLY | os.O_DIRECTORY),
                                          (LIFECYCLE_LOCK, 'lock_identity', os.O_RDONLY)):
            fd = os.open(path, flags | os.O_NOFOLLOW)
            try:
                require(small_identity(os.fstat(fd)) == proof[identity_key] == small_identity(path.lstat()), 'A lifecycle fence inode changed.')
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BaseException:
                os.close(fd)
                raise
            self.lifecycle_fds.append(fd)

    def release_lifecycle(self):
        require(len(self.lifecycle_fds) == 2, 'Both lifecycle fences must be accounted for.')
        for fd in reversed(self.lifecycle_fds):
            os.close(fd)
        self.lifecycle_fds = []

    def migrate(self):
        require(len(self.lifecycle_fds) == 2, 'Migration lacks local lifecycle ownership.')
        self.phase('migrate_requested')
        self.helper('migrate', baseline=True)
        self.check(stopped=True, migrated=True)
        self.phase('migrated')

    def install_file(self, path, raw, expected, mode):
        if expected is None:
            require(not present(path), 'A new publication path is occupied.')
        else:
            require(sha(protected(path, modes=(0o600, 0o644, 0o755))) == expected, 'A replaced file differs from its sealed original.')
        temp = path.with_name(path.name + '.main28-' + self.intent['run_id'])
        create(temp, raw, mode)
        require(temp.stat().st_dev == path.parent.stat().st_dev, 'Atomic publication would cross filesystems.')
        if expected is not None:
            require(sha(protected(path, modes=(0o600, 0o644, 0o755))) == expected, 'A file changed immediately before replacement.')
        else:
            require(not present(path), 'A new publication path became occupied.')
        os.replace(temp, path)
        sync(path.parent)

    def installed_web(self):
        current = tree_fact(WEB, uid=0, gid=0)
        expected = {name: item['sha256'] for name, item in self.before['web']['files'].items()}
        expected.update(self.inputs['web']['files'])
        require({name: item['sha256'] for name, item in current['files'].items()} == expected, 'The main asset union differs.')
        require(current['identity']['mode'] == 0o755 and all(item['mode'] == 0o755 for item in current['directories'].values()) and
                all(item['identity']['mode'] == 0o644 for item in current['files'].values()), 'The main frontend permissions differ.')
        for name, item in self.before['web']['files'].items():
            if name not in self.inputs['web']['files']:
                require(current['files'][name] == item, 'An old hashed asset outside the release was changed.')
        return current

    def install(self):
        self.check(stopped=True, migrated=True)
        old = self.before['web']['files']
        for name in sorted(self.inputs['web']['files'], key=lambda value: (value == 'index.html', value)):
            relative = Path(name)
            path = WEB / relative
            require(not relative.is_absolute() and '..' not in relative.parts, 'An asset escaped the existing main frontend.')
            if not present(path.parent):
                require(path.parent.parent == WEB, 'An asset requires an undeclared directory.')
                path.parent.mkdir(mode=0o755)
                os.chmod(path.parent, 0o755)
                sync(WEB)
            data = protected(WEB_SOURCE / name, self.inputs['web']['files'][name])
            self.install_file(path, data, old.get(name, {}).get('sha256'), 0o644)
        self.installed_web()
        self.install_file(BINARY, self.inputs['binary'], OLD_BINARY_SHA, 0o755)
        self.expected_binary = NEW_BINARY_SHA
        self.check(stopped=True, migrated=True, web=True)
        self.phase('installed')

    def public_readiness(self):
        rows = self.before['database']['tables']['server_settings']
        server_ids = [row['value'] for row in rows if row['key'] == 'server_id']
        require(len(server_ids) == 1, 'The main server ID is missing.')
        self.http_requests = 0
        for route, asset in [('/readyz', None), ('/emby/System/Info/Public', None), *self.inputs['frontend_routes']]:
            self.service_fact()
            self.http_requests += 1
            self.save('http-%d-intent.json' % self.http_requests, {'method': 'GET', 'path': route, 'authenticated': False})
            connection = http.client.HTTPConnection('127.0.0.1', 18096, timeout=15)
            try:
                connection.request('GET', route, headers={'Accept': '*/*', 'Accept-Encoding': 'identity', 'Connection': 'close'})
                response = connection.getresponse()
                raw = response.read((8 << 20) + 1)
                status, cookie = response.status, response.getheader('Set-Cookie')
            finally:
                connection.close()
            self.save('http-%d-result.json' % self.http_requests, {'method': 'GET', 'path': route, 'status': status,
                      'sha256': sha(raw), 'bytes': len(raw), 'cookie_present': cookie is not None})
            require(status == 200 and cookie is None and len(raw) <= 8 << 20, 'An anonymous main readiness response failed.')
            if asset:
                require(sha(raw) == self.inputs['web']['files'][asset], 'The new main server did not serve the accepted asset bytes.')
            elif route == '/readyz':
                require(decode(raw) == {'Status': 'ready'}, 'The replacement is not ready.')
            else:
                value = decode(raw)
                require(value.get('Id') == server_ids[0] and value.get('ProductName') == 'Goby' and value.get('Version') == '4.9.5.0',
                        'The main server ID or product identity changed.')
            self.service_fact()
        require(self.http_requests == 5, 'The anonymous request budget differs.')

    def admit_controller(self):
        names = ('MainPID', 'InvocationID', 'ActiveState', 'Description', 'MemoryMax', 'MemorySwapMax', 'CPUQuotaPerSecUSec',
                 'TasksMax', 'RuntimeMaxUSec', 'RemainAfterExit', 'KillMode', 'ControlGroup')
        value = self.properties(self.controller_unit, names)
        require(value['MainPID'] == str(os.getpid()) and value['InvocationID'] == os.environ.get('INVOCATION_ID') and
                value['Description'] == MARKER + ':' + self.intent['run_id'] and value['MemoryMax'] == str(2 << 30) and
                value['MemorySwapMax'] == '0' and value['CPUQuotaPerSecUSec'] == '1.500000s' and value['TasksMax'] == '128' and
                value['RuntimeMaxUSec'] == '30min' and value['RemainAfterExit'] == 'yes' and value['KillMode'] == 'control-group' and
                value['ControlGroup'] == '/system.slice/' + self.controller_unit, 'The caller is not the bounded new main controller.')
        self.controller = {'unit': self.controller_unit, 'invocation_id': value['InvocationID'], 'pid': os.getpid()}

    def finalize(self):
        after = self.check(migrated=True, web=True)
        require(not self.restart_installed and not self.hba_changed and not self.lifecycle_fds and self.pair['phase'] == 'removed' and
                self.fence.actions == ['reload-install', 'stop', 'start', 'reload-restore'], 'Owned resources or service budgets are incomplete.')
        self.installed_web()
        self.save('after-full.json', after)
        group = Path('/sys/fs/cgroup/system.slice') / self.controller_unit
        pids = {int(line) for path in group.rglob('cgroup.procs') for line in path.read_text().splitlines()}
        require(pids == {os.getpid()}, 'A helper remains; forced unit cleanup cannot count as success.')
        self.phase('complete')
        result = {'marker': MARKER, 'version': 1, 'status': 'awaiting_outer_attestation', 'run_id': self.intent['run_id'],
                  'intent_sha256': self.intent_sha, 'main_baseline': self.intent['main_baseline'], 'client_gate': self.client_gate,
                  'controller': self.controller, 'main_process': self.expected_process, 'main_invocation': self.new_invocation,
                  'schema': 28, 'binary_sha256': NEW_BINARY_SHA, 'service_actions': self.fence.actions, 'http_requests': 5,
                  'preserved': {'old_columns_rows_sequences': True, 'private_and_recovery': True, 'archives_media': True,
                                'unrelated_global_catalog': True, 'candidate_proxy_workspace': True},
                  'cleanup': {'rehearsal_removed': True, 'hba_restored': True, 'restart_policy_restored': True, 'lifecycle_released': True},
                  'automatic_retry': False, 'automatic_rollback': False, 'main_client_acceptance': False, 'evidence': dict(self.records)}
        self.save('report.json', result)
        return {'status': result['status'], 'report': self.records['report.json']}

    def execute(self):
        self.admit_controller()
        self.begin()
        self.backup_materials()
        self.backup()
        self.rehearse()
        self.assert_baseline('before-fence')
        self.install_restart_fence()
        self.assert_baseline('pre-stop')
        self.service('stop')
        self.assert_baseline('stopped', stopped=True)
        self.acquire_lifecycle()
        self.migrate()
        self.install()
        self.release_lifecycle()
        self.service('start')
        self.public_readiness()
        self.restore_restart_fence()
        return self.finalize()

    def run(self):
        try:
            self.load_inputs()
            if self.args.mode == 'prepare':
                return self.prepare()
            result = self.preflight()
            if self.args.mode == 'preflight':
                return result
            return self.execute()
        except BaseException as error:
            if self.created:
                failed_phase = self.phase_name
                try:
                    if not self.hba_restore_reserved:
                        self.restore_hba()
                except BaseException:
                    pass
                try:
                    self.save('failed.json', {'marker': MARKER, 'status': 'retained_for_review', 'run_id': self.intent['run_id'],
                        'phase': failed_phase, 'error_code': safe_error_code(error), 'service_actions': self.fence.actions,
                        'pair': self.pair, 'hba_restored': not self.hba_changed, 'restart_fence_retained': self.restart_installed,
                        'automatic_retry': False, 'automatic_rollback': False})
                except BaseException:
                    pass
            raise
        finally:
            for fd in reversed(self.lifecycle_fds):
                os.close(fd)
            self.lifecycle_fds = []
            if self.lock is not None:
                os.close(self.lock)
                self.lock = None


def attest(args):
    controller = Controller(args)
    c = controller
    try:
        c.load_inputs()
        require(not present(c.output / 'failed.json') and not present(c.output / 'attestation.json'), 'A failed or already attested scope cannot be adopted.')
        raw = protected(c.output / 'report.json')
        report = decode(raw)
        require(report.get('marker') == MARKER and report.get('status') == 'awaiting_outer_attestation' and
                report.get('intent_sha256') == c.intent_sha and report.get('run_id') == c.intent['run_id'] and
                report.get('schema') == 28 and report.get('binary_sha256') == NEW_BINARY_SHA and
                report.get('service_actions') == ['reload-install', 'stop', 'start', 'reload-restore'] and report.get('http_requests') == 5 and
                type(report.get('preserved')) is dict and set(report['preserved']) == {'old_columns_rows_sequences', 'private_and_recovery', 'archives_media', 'unrelated_global_catalog', 'candidate_proxy_workspace'} and
                all(value is True for value in report['preserved'].values()) and type(report.get('cleanup')) is dict and
                set(report['cleanup']) == {'rehearsal_removed', 'hba_restored', 'restart_policy_restored', 'lifecycle_released'} and
                all(value is True for value in report['cleanup'].values()), 'The main run did not propose complete success.')
        origin = report['controller']
        require(origin['unit'] == c.controller_unit and origin['pid'] != os.getpid(), 'The controller cannot attest itself.')
        state = c.properties(c.controller_unit, ('MainPID', 'InvocationID', 'ActiveState', 'SubState', 'ExecMainStatus', 'Result', 'RemainAfterExit', 'Description'))
        require(state['MainPID'] == '0' and state['InvocationID'] == origin['invocation_id'] and state['ActiveState'] == 'active' and
                state['SubState'] == 'exited' and state['ExecMainStatus'] == '0' and state['Result'] == 'success' and state['RemainAfterExit'] == 'yes' and
                state['Description'] == MARKER + ':' + c.intent['run_id'], 'The original main controller is not successfully terminal.')
        cgroup_empty(c.controller_unit)
        c.client_gate = c.validate_client_gate()
        require(canonical(c.client_gate) == canonical(report.get('client_gate')), 'The final scoped client gate differs from deployment admission.')
        c.baseline = decode(protected(Path(c.intent['main_baseline']['path']), c.intent['main_baseline']['sha256']))
        require(c.baseline.get('marker') == MARKER + '-baseline' and c.baseline.get('run_id') == c.intent['run_id'] and
                c.baseline.get('input_core_sha256') == sha(canonical(input_core(c.intent))), 'The final baseline does not bind this main input.')
        c.before = c.baseline['captured']
        c.expected_process, c.expected_binary = report['main_process'], NEW_BINARY_SHA
        for item in report['evidence'].values():
            descriptor(item)
            require(Path(item['path']).parent in (c.output, c.private), 'A main evidence descriptor escaped its scope.')
            protected(Path(item['path']), item['sha256'], limit=512 << 20)
        actual = c.check(migrated=True, web=True)
        c.compare_frames(decode(protected(c.output / 'after-full.json')), actual)
        c.installed_web()
        c.verify_materials()
        c.verify_readiness_receipts()
        require(c.pair_rows() == {'role': None, 'database': None} and not present(c.restart_path), 'Owned temporary resources remain.')
        require(actual['service']['properties']['InvocationID'] == report['main_invocation'], 'The new main invocation changed.')
        cgroup_empty(c.controller_unit)
        result = {'marker': MARKER, 'version': 1, 'status': 'passed', 'run_id': c.intent['run_id'], 'intent_sha256': c.intent_sha,
                  'report_sha256': sha(raw), 'controller': state, 'recursive_cgroup_empty': True, 'main_process': c.expected_process,
                  'schema': 28, 'binary_sha256': NEW_BINARY_SHA, 'full_preservation': True, 'main_client_acceptance': False}
        create(c.output / 'attestation.json', canonical(result))
        return result
    finally:
        if c.lock is not None:
            os.close(c.lock)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('prepare', 'preflight', 'run', 'attest'))
    parser.add_argument('--input', type=Path, required=True)
    parser.add_argument('--input-sha256', required=True)
    args = parser.parse_args(argv)
    os.umask(0o077)
    try:
        result = attest(args) if args.mode == 'attest' else Controller(args).run()
        print(canonical(result).decode().strip())
        return 0
    except BaseException as error:
        print(canonical({'marker': MARKER, 'status': 'failed', 'error_code': safe_error_code(error), 'automatic_retry': False}).decode().strip())
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
