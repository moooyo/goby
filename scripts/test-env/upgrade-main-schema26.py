#!/usr/bin/env python3
"""Upgrade the pinned source18 primary from schema25 to schema26 once.

Preflight is read-only. Upgrade creates new private evidence, stops only the
recorded main service, exports one baseline/dump snapshot, rehearses in a fresh
receipted database, and migrates the original database in place. Only the fresh
rehearsal can receive pg_restore. There is no resume, automatic rollback, main
restore, old-binary restart, backend termination, or historical evidence edit.

A release report is an explicit review attestation over actual retained client
and candidate evidence. This operator never produces that attestation itself.
The required finite startup plan proves the unchanged 30-day retention policy
and no expiry through its deadline. The historical one-day count is retained
as evidence, not substituted for the effective policy or silently discarded.
Importing this module performs no filesystem, process, network, or DB work.
Explicit baseline repair uses a separate root and revalidates the fixed failed
read-only preparation. It never edits that failure or starts the old binary.
"""

from __future__ import annotations

import argparse
from contextlib import contextmanager
import datetime as dt
import hashlib
import http.client
from http.cookies import SimpleCookie
import json
import os
from pathlib import Path
import re
import secrets
import shlex
import stat
import subprocess
import sys
import time
import types

sys.dont_write_bytecode = True
WORK = Path('/opt/goby-test/exec-work-m3e')
NORMAL_ROOT = Path('/opt/goby-test/backups/main-schema26-v1')
REPAIR_ROOT = Path('/opt/goby-test/backups/main-schema26-baseline-repair-v1')
REHEARSAL_REPAIR_ROOT = Path('/opt/goby-test/backups/main-schema26-rehearsal-repair-v1')
BASELINE_REPAIR_MODES = ('repair-preflight', 'repair-baseline')
REHEARSAL_REPAIR_MODES = ('rehearsal-repair-preflight', 'rehearsal-repair')
REPAIR_MODES = (*BASELINE_REPAIR_MODES, *REHEARSAL_REPAIR_MODES)
ROOT = NORMAL_ROOT
FAILED_RUN = NORMAL_ROOT / 'run-20260911T125911Z-a3ceb3e924e2ea287959135c'
REHEARSAL_FAILED_RUN = REPAIR_ROOT / 'run-20260911T131730Z-85526803ca1a8b52a26f0ab7'
FAILED_OPERATOR_SHA = 'd46f7591ba23b51dfd1fdd63e3f4c80da38467831d549a9dde323dc49f89372f'
FAILED_HELPER_SOURCE_SHA = '621306bfd3068ea40b49d0c74de3e61b54a5b9e3cd210cb29ce74b11dc4fe32d'
FAILURE_OBSERVATION_SHA = 'd553670aa7c2de9b5630ae20227d284050b846b786b14d7dd38aaadde62cf49d'
REHEARSAL_FAILURE_OBSERVATION_SHA = '7ac34458ccea3b168d76a1981d717996d8ba050a48fb01fdc6c673565ec53764'
BASELINE_FAILED_OPERATOR_SHA = '69466f1c539662212f01a271f26a267b239c09cdefde7493304ca2e10b64e83d'
BASELINE_FAILED_HELPER_SOURCE_SHA = '105a5a0db7388224383db8a0851966e1ff159869767f5d985359de47df72cee0'
MARKER = 'goby-main-schema26-upgrade-v1'
PUBLISHED_ROOT = Path('/opt/goby-test/backups/client-schema25-v1')
PUBLISHED_RUN = PUBLISHED_ROOT / 'run-20260911T062405Z-975ef4c2e0c2b3078d517dc5'
OLD_TERMINAL_SHA = '5dd971af11bc04b815759776d074ce7dad336edf374994400f09908a2a3721f4'
OLD_SOURCE_SHA = 'fa46e1547416afcb65c15f97aa5a1b7d3c344579386a424478ec310f0e01660b'
OLD_BINARY_SHA = '665df2d3851dc1b4a251012805678559e17e274c08f2548aead560e593a08d2b'
OLD_PID, OLD_TICKS = 539535, '3115871'
OLD_BOOT = '6bdfc486-7bc8-412f-82b5-70095a09dde7'
SERVICE = 'goby-foundation-test.service'
LIVE = Path('/opt/goby-dev')
MAIN = {'database': 'goby_test', 'database_oid': 16385, 'role': 'goby_test', 'role_oid': 16384}
SYSTEM_IDENTIFIER = '7683277964552005578'
DEPLOYMENT_ID = 'f58d5e0c8ff49fca916499e666bffd9f'
PORT = 18096
PG = Path('/usr/lib/postgresql/17/bin')
MAX_DOCUMENT, MAX_FILE = 16 << 20, 512 << 20
HASH = re.compile(r'[0-9a-f]{64}')
RUN = re.compile(r'run-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{24}')
REHEARSAL = re.compile(r'goby_upgrade26_m3e_[0-9a-f]{24}')
SOURCE_MANIFEST = 'backup-source-inputs.json'
TOOL_MANIFEST = 'main-schema26-tool-inputs.json'
TOOL_FILES = {'scripts/test-env/upgrade-main-schema26.py', 'scripts/test-env/test-upgrade-main-schema26.py',
              'scripts/test-env/migrate-main-schema26.go'}
SERVICE_PROPERTIES = ('MainPID', 'ActiveState', 'SubState', 'User', 'Group', 'FragmentPath', 'DropInPaths',
    'WorkingDirectory', 'EnvironmentFiles', 'ProtectSystem', 'ReadWritePaths', 'UMask', 'NoNewPrivileges',
    'ControlGroup', 'MemoryMax', 'MemorySwapMax', 'CPUQuotaPerSecUSec', 'TasksMax', 'LimitNOFILE',
    'Restart', 'KillMode', 'TimeoutStopUSec', 'PrivateTmp', 'ProtectHome', 'CapabilityBoundingSet')
DYNAMIC_PROPERTIES = {'MainPID', 'ActiveState', 'SubState', 'ControlGroup'}
ACTIONS = {'stop', 'repair-baseline', 'repair-rehearsal', 'baseline-dump', 'save-materials', 'rehearsal-role', 'rehearsal-database', 'rehearsal-mark',
    'rehearsal-schema-owner', 'rehearsal-restore', 'rehearsal-migrate', 'rehearsal-close',
    'rehearsal-drop-database', 'rehearsal-drop-role', 'main-migrate', 'publish-file', 'start', 'smoke-login', 'smoke-logout'}
LOCK_HELD = False
STARTUP_CONTEXT = None
STARTUP_CHECKING = False
STARTUP_CLEANUP = False
RETENTION_ENV = 'GOBY_ACTIVITY_RETENTION_DAYS'
RETENTION_CODE_SHA256 = {
    'internal/config/observability.go': '4f26e6af24160c6d93ffc52a87725ac3bb88cd3148bdd603df5594c5d71acc7f',
    'internal/server/activity_retention.go': '88a2831005a3013fc4d88162a7e75606d3f6a87ee70d93b5b862c012fbe32180',
    'internal/activity/store.go': 'af8690eefbf9ee82cfe8adbc7b5e9cfe2942c2afe3cbe1a36dfe6658663240fe',
}
STARTUP_ENV_HASHES = {
    'runtime_env': '2043e72115338d04775485dd63702c6084d36d09331ca5cdac66819152619607',
    'recovery_env': '6847d34cfd9af7c53a8b16406db6f341f418b6ffdacb83bfe8c1f9bfd36b4c7c',
}
ACTIVE_COUNTERS = ('runnable_triggers', 'active_runs', 'active_children', 'active_scans', 'active_encodings')
ABSENT_SOURCES = ('process', 'manager', 'unit', 'runtime_env', 'recovery_env', 'pass_environment', 'unset_environment')


class Failure(Exception):
    """A reviewed condition failed; raw diagnostics remain private."""


def require(value, message):
    if not value:
        raise Failure(message)


def failure_reason(op, error):
    # Only these two reviewed exception classes contain deliberately sanitized
    # operator messages. Preserve no arbitrary subprocess or credential error.
    dependency_failure = getattr(op, 'Failure', None)
    known = isinstance(error, Failure) or (isinstance(dependency_failure, type) and isinstance(error, dependency_failure))
    return str(error) if known else type(error).__name__


def canonical(value):
    return (json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=True, allow_nan=False) + '\n').encode()


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def decode(raw):
    def unique(pairs):
        value = {}
        for key, entry in pairs:
            require(key not in value, 'A control document repeats a field.')
            value[key] = entry
        return value
    return json.loads(raw, object_pairs_hook=unique,
                      parse_constant=lambda _: (_ for _ in ()).throw(Failure('A control number is not finite.')))


def file_identity(info):
    return (info.st_dev, info.st_ino, info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode),
            info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def read_pinned_source(path, expected, *, modes=(0o600, 0o644)):
    require(path.is_absolute() and '..' not in path.parts, 'A source path is not canonical and absolute.')
    for parent in reversed(path.parents):
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                'A source ancestor permits untrusted changes.')
    before = path.lstat()
    require(stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and before.st_nlink == 1 and
            stat.S_IMODE(before.st_mode) in modes and 0 < before.st_size <= MAX_DOCUMENT,
            'The source type, owner, permissions, link count, or size differs.')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as handle:
        require(file_identity(os.fstat(handle.fileno())) == file_identity(before), 'The source changed while opening.')
        raw = handle.read(MAX_DOCUMENT + 1)
        require(len(raw) == before.st_size and file_identity(os.fstat(handle.fileno())) == file_identity(before),
                'The source changed while reading.')
    require(file_identity(path.lstat()) == file_identity(before) and sha(raw) == expected, 'The source digest or identity differs.')
    return raw


def validate_arguments(args):
    require(args.mode in ('preflight', 'upgrade', *REPAIR_MODES), 'Only an explicit one-shot operation is supported.')
    failure_path, failure_digest = getattr(args, 'failure_report', None), getattr(args, 'failure_report_sha256', None)
    if args.mode in REPAIR_MODES:
        require(isinstance(failure_path, Path) and failure_path.is_absolute() and failure_path.is_relative_to(WORK) and
                '..' not in failure_path.parts and failure_path.suffix == '.json' and HASH.fullmatch(failure_digest or ''),
                'Baseline repair requires one explicitly pinned private failure attestation.')
    else:
        require(failure_path is None and failure_digest is None, 'A normal operation cannot adopt failed-run authority.')
    second_path, second_digest = getattr(args, 'rehearsal_failure_report', None), getattr(args, 'rehearsal_failure_report_sha256', None)
    if args.mode in REHEARSAL_REPAIR_MODES:
        require(isinstance(second_path, Path) and second_path.is_absolute() and second_path.is_relative_to(WORK) and
                '..' not in second_path.parts and second_path.suffix == '.json' and HASH.fullmatch(second_digest or ''),
                'Rehearsal repair requires the separately pinned second failure attestation.')
    else:
        require(second_path is None and second_digest is None, 'Only explicit rehearsal repair accepts second-failure authority.')
    for name in ('script', 'operator', 'deployment_receipt', 'manifest', 'candidate', 'assets', 'full_report',
                 'helper', 'tool_manifest', 'helper_build_report', 'guard_report', 'service_pin', 'release_report', 'startup_plan'):
        require(HASH.fullmatch(getattr(args, name + '_sha256', '') or ''), 'Every input requires an explicit digest.')
    require(args.deployment_receipt == PUBLISHED_RUN / 'terminal.json' and args.deployment_receipt_sha256 == OLD_TERMINAL_SHA,
            'Only the published source18 installation may be upgraded.')
    require(type(args.old_pid) is int and args.old_pid == OLD_PID and args.old_start_ticks == OLD_TICKS and args.old_boot_id == OLD_BOOT,
            'The old source18 process lifetime differs.')
    require(args.source.parent == WORK and re.fullmatch(r'source-attempt-(?:2[89]|[3-9][0-9]|[1-9][0-9]{2,})', args.source.name),
            'The source is not a new frozen product snapshot.')
    require(args.operator.name == 'deploy-client-schema25.py' and len(args.operator.parents) >= 4 and
            args.operator.parents[0].name == 'test-env' and args.operator.parents[1].name == 'scripts' and
            args.operator.parents[3] == WORK and re.fullmatch(r'tool-build-[a-z0-9][a-z0-9_-]{1,79}', args.operator.parents[2].name),
            'The published dependency is not its independently frozen source.')
    require(args.tool_source.parent == WORK and re.fullmatch(r'tool-build-[a-z0-9][a-z0-9_-]{1,79}', args.tool_source.name),
            'The independent new tool source escaped its workspace.')
    for name in ('source', 'candidate', 'assets', 'full_report', 'helper', 'tool_source', 'helper_build_report',
                 'guard_report', 'service_pin', 'release_report', 'startup_plan'):
        path = getattr(args, name)
        require(path.is_absolute() and path.is_relative_to(WORK) and '..' not in path.parts,
                'An input escaped the private workspace.')
    require(args.candidate_sha256 != OLD_BINARY_SHA and args.source != args.tool_source,
            'The candidate is an old restart or a mixed product/tool snapshot.')


def load_published_operator(args):
    read_pinned_source(Path(__file__).absolute(), args.script_sha256)
    raw = read_pinned_source(args.operator, args.operator_sha256)
    terminal = decode(read_pinned_source(args.deployment_receipt, OLD_TERMINAL_SHA, modes=(0o600,)))
    require(terminal.get('status') == 'passed' and terminal.get('schema_version') == 25 and
            terminal.get('binary_sha256') == OLD_BINARY_SHA and terminal.get('old_archives_retained') is True and
            terminal.get('main_database_restored') is False and terminal.get('active_service', {}).get('MainPID') == str(OLD_PID) and
            terminal['active_service'].get('start_ticks') == OLD_TICKS, 'The source18 completion receipt differs.')
    prepared = decode(read_pinned_source(PUBLISHED_RUN / 'prepared.json', terminal['prepared_sha256'], modes=(0o600,)))
    inputs = decode(read_pinned_source(PUBLISHED_RUN / 'inputs.json', prepared['inputs_sha256'], modes=(0o600,)))
    require(prepared.get('phase') == 'prepared' and prepared.get('rehearsal_passed') is True and prepared.get('rehearsal_removed') is True and
            inputs.get('source_manifest_sha256') == OLD_SOURCE_SHA and inputs.get('candidate', {}).get('sha256') == OLD_BINARY_SHA and
            inputs.get('tool_source', {}).get('path') == str(args.operator.parents[2]) and
            inputs['tool_source']['files'].get('scripts/test-env/deploy-client-schema25.py') == args.operator_sha256,
            'The published preparation does not authorize these dependency bytes.')
    require(all(inputs.get('source_files', {}).get(name) == digest for name, digest in RETENTION_CODE_SHA256.items()),
            'The deployed source18 retention implementation differs from the reviewed default policy.')
    protected = decode(read_pinned_source(PUBLISHED_RUN / 'protected-before.json', prepared['files']['protected-before.json'], modes=(0o600,)))
    module = types.ModuleType('published_source18_main_deployment')
    module.__file__ = str(args.operator)
    exec(compile(raw, str(args.operator), 'exec'), module.__dict__)
    require(module.WORK == WORK and module.ROOT == PUBLISHED_ROOT and module.SERVICE == SERVICE and module.LIVE == LIVE and
            module.SYSTEM_IDENTIFIER == SYSTEM_IDENTIFIER and module.DEPLOYMENT_ID == DEPLOYMENT_ID and module.MAIN == MAIN,
            'The frozen dependency targets a different primary installation.')
    return module, {'terminal_sha256': OLD_TERMINAL_SHA, 'protected_before': protected,
                    'asset_members': inputs['asset_members'], 'operator_sha256': args.operator_sha256}


@contextmanager
def deployment_lock(op):
    global LOCK_HELD
    require(not LOCK_HELD, 'Nested deployment ownership is not supported.')
    with op.operator_lock(False):
        LOCK_HELD = True
        try:
            yield
        finally:
            LOCK_HELD = False


@contextmanager
def operation_scope(args):
    global ROOT
    require(ROOT == NORMAL_ROOT and not LOCK_HELD, 'Operation roots cannot change inside an existing deployment.')
    ROOT = REHEARSAL_REPAIR_ROOT if args.mode in REHEARSAL_REPAIR_MODES else REPAIR_ROOT if args.mode in BASELINE_REPAIR_MODES else NORMAL_ROOT
    try:
        yield
    finally:
        ROOT = NORMAL_ROOT


def inventory_source(op, path, manifest_name):
    op.directory(path)
    result, total = {}, 0
    for member in path.rglob('*'):
        info = op.path_info(member)
        require(info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022, 'A source member permits untrusted writes.')
        if stat.S_ISDIR(info.st_mode):
            continue
        name = op.safe_relative(member.relative_to(path).as_posix())
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_size <= MAX_FILE,
                'A source member is not a bounded independent regular file.')
        total += info.st_size
        require(total <= 1 << 30 and len(result) < 20000, 'The complete source inventory exceeds its bound.')
        if name != manifest_name:
            result[name] = sha(op.read_file(member, modes=(0o600, 0o644, 0o755), limit=MAX_FILE))
    return result


def candidate_inputs(op, args, published):
    require(LOCK_HELD, 'Candidate evidence requires the existing deployment lock.')
    raw = op.read_file(args.source / SOURCE_MANIFEST, modes=(0o600, 0o644))
    source = decode(raw)
    require(sha(raw) == args.manifest_sha256 and set(source) == {'marker', 'files'} and
            source['marker'] == 'goby-client-backup-source-m3e-v1', 'The product manifest differs.')
    files = inventory_source(op, args.source, SOURCE_MANIFEST)
    require(len(files) > 100 and files == source['files'] and not TOOL_FILES.intersection(files),
            'The complete product source differs or contains the independent tools.')
    require(all(files.get(name) == digest for name, digest in RETENTION_CODE_SHA256.items()),
            'The candidate retention implementation differs from the deployed policy.')
    for version, digest in op.CATALOGS.items():
        require(files.get(f'internal/backuppg/catalogs/schema-{version}-postgresql-17.json') == digest,
                'A historical trusted catalog changed.')
    catalog_name = 'internal/backuppg/catalogs/schema-26-postgresql-17.json'
    catalog = decode(op.read_file(args.source / catalog_name, modes=(0o600, 0o644)))
    old_catalog = decode(op.read_file(args.source / 'internal/backuppg/catalogs/schema-25-postgresql-17.json', modes=(0o600, 0o644)))
    require(catalog['version'] == 26 and len(catalog['migrations']) == 26 and catalog['migrations'][:25] == old_catalog['migrations'] and
            {f"internal/database/migrations/{row['name']}": row['sha256'] for row in catalog['migrations']} ==
            {name: digest for name, digest in files.items() if name.startswith('internal/database/migrations/')},
            'The candidate does not retain the exact published migration prefix.')
    require({row['Name'] for row in catalog['catalog']['Tables']} ==
            {row['Name'] for row in old_catalog['catalog']['Tables']} | {'theme_owner_ids', 'theme_reserved_paths', 'item_theme_resources'},
            'The candidate schema has an unreviewed table population.')
    tool_raw = op.read_file(args.tool_source / TOOL_MANIFEST, modes=(0o600, 0o644))
    tool = decode(tool_raw)
    actual_tools = inventory_source(op, args.tool_source, TOOL_MANIFEST)
    expected_product = {**files, SOURCE_MANIFEST: args.manifest_sha256}
    require(sha(tool_raw) == args.tool_manifest_sha256 and set(tool) ==
            {'marker', 'product_source_manifest_sha256', 'published_operator_sha256', 'files'} and
            tool['marker'] == 'goby-main-schema26-tool-source-v1' and tool['product_source_manifest_sha256'] == args.manifest_sha256 and
            tool['published_operator_sha256'] == published['operator_sha256'] and actual_tools == tool['files'] and
            set(actual_tools) == set(expected_product) | TOOL_FILES and
            all(actual_tools[name] == digest for name, digest in expected_product.items()) and
            actual_tools['scripts/test-env/upgrade-main-schema26.py'] == args.script_sha256,
            'The independent tools changed product bytes, membership, or dependency authority.')
    candidate = op.file_fact(args.candidate, modes=(0o600, 0o755))
    helper = op.file_fact(args.helper, modes=(0o755,))
    assets = op.file_fact(args.assets, modes=(0o600, 0o644))
    require(candidate['sha256'] == args.candidate_sha256 and candidate['identity']['bytes'] > 1 << 20 and
            helper['sha256'] == args.helper_sha256 and assets['sha256'] == args.assets_sha256, 'Candidate artifacts differ.')
    reports = {}
    for name in ('full_report', 'helper_build_report', 'guard_report', 'release_report'):
        data = op.read_file(getattr(args, name))
        require(sha(data) == getattr(args, name + '_sha256'), 'A supplied acceptance report changed.')
        reports[name] = decode(data)
    full = reports['full_report']
    tests, packages = full.get('tests', {}), full.get('packages', [])
    passed = tests.get('passed', [])
    require(full.get('status') == 'passed' and full.get('mode') == 'full' and full.get('schema') == 26 and
            full.get('source') == str(args.source) and full.get('source_manifest_sha256') == args.manifest_sha256 and
            full.get('catalog_sha256') == files[catalog_name] and full.get('binary', {}).get('sha256') == args.candidate_sha256 and
            full['binary'].get('bytes') == candidate['identity']['bytes'] and type(full.get('unit_exit')) is int and full['unit_exit'] == 0 and
            all(type(tests.get(key)) is int and tests[key] == 0 for key in ('failures', 'skips')) and
            isinstance(passed, list) and all(isinstance(name, str) and name.startswith('Test') for name in passed) and
            type(tests.get('top_level_passes')) is int and tests['top_level_passes'] == len(passed) == len(set(passed)) and len(passed) > 1700 and
            isinstance(packages, list) and len(packages) == len(set(packages)) == 24 and set(packages) == op.EXPECTED_PACKAGES and
            set(full.get('cleanup', {})) == op.FULL_CLEANUP and all(value is True for value in full['cleanup'].values()),
            'No complete successful regression proves these exact product and binary bytes.')
    build = reports['helper_build_report']
    require(build.get('schema') == 'goby-main-schema26-helper-build' and build.get('version') == 1 and build.get('status') == 'passed' and
            build.get('product_source_manifest_sha256') == args.manifest_sha256 and build.get('tool_manifest_sha256') == args.tool_manifest_sha256 and
            build.get('helper_sha256') == args.helper_sha256 and build.get('go_version') == 'go1.27.1' and build.get('goos') == 'linux' and
            build.get('goarch') == 'amd64' and build.get('cgo_enabled') is False, 'The helper build has no matching independent provenance.')
    guards = reports['guard_report']
    cases = guards.get('cases', [])
    require(guards.get('suite') == 'main-schema26-upgrade-guards' and guards.get('status') == 'passed' and
            all(type(guards.get(key)) is int and guards[key] == 0 for key in ('failures', 'errors', 'skips')) and
            isinstance(cases, list) and all(isinstance(name, str) for name in cases) and type(guards.get('tests')) is int and
            guards['tests'] == len(cases) == len(set(cases)) and len(cases) >= 10 and
            guards.get('operator_sha256') == args.script_sha256 and
            guards.get('guard_sha256') == actual_tools['scripts/test-env/test-upgrade-main-schema26.py'] and
            guards.get('fixtures') == 'synthetic-memory-only' and
            all(guards.get(key) is False for key in ('real_backup_acceptance', 'real_migration_acceptance', 'real_deployment_acceptance')),
            'The exact independent operator guards have not passed.')
    release = reports['release_report']
    require(release.get('schema') == 'goby-main-schema26-release-acceptance' and release.get('version') == 1 and
            release.get('status') == 'passed' and release.get('schema_version') == 26 and
            release.get('source_manifest_sha256') == args.manifest_sha256 and release.get('binary_sha256') == args.candidate_sha256 and
            release.get('full_report_sha256') == args.full_report_sha256 and release.get('candidate_verified') is True and
            release.get('client_verified') is True and isinstance(release.get('evidence'), list) and 1 <= len(release['evidence']) <= 32,
            'Candidate/client acceptance is not yet proven for the final release.')
    evidence = {}
    for row in release['evidence']:
        path = Path(row['path'])
        require(path.is_absolute() and path.is_relative_to(WORK) and '..' not in path.parts and path.suffix == '.json' and
                str(path) not in evidence and HASH.fullmatch(row['sha256']), 'A release evidence path or digest is invalid.')
        require(sha(op.read_file(path)) == row['sha256'], 'A reviewed candidate/client artifact changed.')
        evidence[str(path)] = row['sha256']
    members = {name: sha(data) for name, data in op.read_assets(args.assets).items()}
    return {'source': str(args.source), 'source_manifest_sha256': args.manifest_sha256, 'source_files': files,
            'catalog_sha256': files[catalog_name], 'tool_source': {'path': str(args.tool_source), 'manifest_sha256': args.tool_manifest_sha256, 'files': actual_tools},
            'candidate': {'path': str(args.candidate), **candidate}, 'helper': {'path': str(args.helper), **helper},
            'assets': {'path': str(args.assets), **assets}, 'asset_members': members,
            'full_report_sha256': args.full_report_sha256, 'helper_build_report_sha256': args.helper_build_report_sha256,
            'guard_report_sha256': args.guard_report_sha256, 'release_report_sha256': args.release_report_sha256,
            'release_evidence': evidence, 'published_operator_sha256': published['operator_sha256'],
            'published_operator_path': str(args.operator), 'published_deployment_receipt_sha256': OLD_TERMINAL_SHA,
            'service_pin': {'path': str(args.service_pin), 'sha256': args.service_pin_sha256},
            'startup_plan': {'path': str(args.startup_plan), 'sha256': args.startup_plan_sha256},
            'repair_baseline': args.mode in BASELINE_REPAIR_MODES,
            'repair_rehearsal': args.mode in REHEARSAL_REPAIR_MODES,
            'failure_report': ({'path': str(args.failure_report), 'sha256': args.failure_report_sha256}
                               if args.mode in REPAIR_MODES else None),
            'rehearsal_failure_report': ({'path': str(args.rehearsal_failure_report), 'sha256': args.rehearsal_failure_report_sha256}
                                        if args.mode in REHEARSAL_REPAIR_MODES else None),
            'old_process': {'pid': OLD_PID, 'start_ticks': OLD_TICKS, 'boot_id': OLD_BOOT}}


def parse_startup_time(value):
    require(isinstance(value, str) and re.fullmatch(r'[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z', value),
            'A startup-plan time must be exact UTC with six fractional digits.')
    try:
        return dt.datetime.fromisoformat(value.removesuffix('Z') + '+00:00')
    except ValueError:
        raise Failure('A startup-plan UTC timestamp is invalid.') from None


def load_startup_plan(op, args):
    require(LOCK_HELD, 'Startup-plan verification requires deployment ownership.')
    raw = op.read_file(args.startup_plan)
    require(sha(raw) == args.startup_plan_sha256, 'The reviewed startup plan changed.')
    plan = decode(raw)
    require(plan.get('schema') == 'goby-main-schema26-startup-plan' and type(plan.get('version')) is int and plan['version'] == 1 and
            plan.get('target') == MAIN and plan.get('system_identifier') == SYSTEM_IDENTIFIER and
            plan.get('source_manifest_sha256') == args.manifest_sha256 and plan.get('full_report_sha256') == args.full_report_sha256 and
            plan.get('candidate_sha256') == args.candidate_sha256 and plan.get('deployment_receipt_sha256') == OLD_TERMINAL_SHA and
            plan.get('service_pin_sha256') == args.service_pin_sha256 and
            plan.get('old_process') == {'pid': OLD_PID, 'start_ticks': OLD_TICKS, 'boot_id': OLD_BOOT} and
            plan.get('unit_sha256') == op.UNIT_SHA and
            plan.get('dropins_sha256') == {str(path): digest for path, digest in zip(op.DROPINS, op.DROPIN_SHAS)} and
            plan.get('runtime_env_sha256') == STARTUP_ENV_HASHES['runtime_env'] and
            plan.get('recovery_env_sha256') == STARTUP_ENV_HASHES['recovery_env'],
            'The startup plan belongs to different source, process, primary, or configuration inputs.')
    retention = plan.get('retention', {})
    require(retention == {'environment_key': RETENTION_ENV, 'effective_days': 30, 'source': 'unchanged-code-default',
            'first_prune_delay_seconds': 60, 'prune_interval_seconds': 60, 'batch_limit': 1000,
            'clock': 'PostgreSQL clock_timestamp', 'comparison': 'created_at < clock_timestamp() - retention',
            'code_sha256': RETENTION_CODE_SHA256, 'overrides_absent': {key: True for key in ABSENT_SOURCES}} and
            all(type(retention.get(key)) is int for key in ('effective_days', 'first_prune_delay_seconds', 'prune_interval_seconds', 'batch_limit')) and
            all(value is True for value in retention['overrides_absent'].values()), 'The exact observed retention behavior is not proven.')
    observed = plan.get('observed', {})
    require(set(observed) == {'database_clock', 'activity_total', 'earliest_activity', 'legacy_one_day_eligible',
                             'effective_retention_eligible', 'active'} and
            all(type(observed.get(key)) is int and observed[key] >= 0 for key in
                ('activity_total', 'legacy_one_day_eligible', 'effective_retention_eligible')) and
            observed['legacy_one_day_eligible'] <= observed['activity_total'] and observed['effective_retention_eligible'] == 0 and
            isinstance(observed['active'], dict) and set(observed['active']) == set(ACTIVE_COUNTERS) and
            all(type(value) is int and value == 0 for value in observed['active'].values()),
            'The observed startup activity or task state is incomplete or unsafe.')
    observed_at, deadline = parse_startup_time(observed['database_clock']), parse_startup_time(plan.get('deadline_utc'))
    require(type(plan.get('startup_reserve_seconds')) is int and plan['startup_reserve_seconds'] == 900 and
            dt.timedelta(seconds=900) < deadline - observed_at <= dt.timedelta(hours=6), 'The startup plan has no finite protected window.')
    if observed['activity_total'] == 0:
        require(observed['earliest_activity'] is None, 'An empty activity population has a nonempty earliest row.')
    else:
        earliest = parse_startup_time(observed['earliest_activity'])
        require(earliest <= observed_at and earliest + dt.timedelta(days=30) > deadline,
                'An observed activity row can expire inside the planned window.')
    evidence = plan.get('evidence', {})
    require(isinstance(evidence, dict) and set(evidence) == {'environment', 'counts'}, 'The startup plan omits its original observation receipts.')
    for row in evidence.values():
        require(isinstance(row, dict) and set(row) == {'path', 'sha256'} and HASH.fullmatch(row['sha256']),
                'A startup observation receipt has an invalid digest or shape.')
        path = Path(row['path'])
        require(path.is_absolute() and path.is_relative_to(WORK) and '..' not in path.parts and path.suffix == '.json' and
                sha(op.read_file(path)) == row['sha256'], 'A pinned startup observation receipt changed.')
    return plan


def retention_environment(op, args, plan):
    require(LOCK_HELD, 'Retention configuration inspection requires deployment ownership.')
    require(op.file_fact(op.UNIT)['sha256'] == plan['unit_sha256'] and all(
        op.file_fact(path, modes=(0o644,))['sha256'] == plan['dropins_sha256'][str(path)] for path in op.DROPINS),
        'The startup unit or a fixed drop-in changed.')
    for relative, digest in RETENTION_CODE_SHA256.items():
        require(op.file_fact(args.source / relative, modes=(0o600, 0o644))['sha256'] == digest,
                'The accepted retention implementation changed.')
    absent = {key: True for key in ABSENT_SOURCES}
    for key, path in (('runtime_env', op.RUNTIME), ('recovery_env', op.RECOVERY_ENV)):
        raw = op.read_file(path)
        require(sha(raw) == plan[key + '_sha256'], 'A fixed startup environment file changed.')
        absent[key] = not any(re.match(r'^(?:export\s+)?' + RETENTION_ENV + r'\s*=', line.strip()) for line in raw.decode().splitlines())
    names = ('MainPID', 'ActiveState', 'SubState', 'Environment', 'PassEnvironment', 'UnsetEnvironment')
    properties = op.systemd_properties(op.command(['/usr/bin/systemctl', 'show', SERVICE, *('-p' + name for name in names)]), names)
    for key, field in (('unit', 'Environment'), ('pass_environment', 'PassEnvironment'), ('unset_environment', 'UnsetEnvironment')):
        try:
            selected = shlex.split(properties[field])
        except ValueError:
            raise Failure('A startup environment property cannot be interpreted.') from None
        absent[key] = not any(value.partition('=')[0] == RETENTION_ENV for value in selected)
    # Raw environment bytes stay in memory only. Persist or display no other
    # setting, password, token, or manager environment entry.
    manager = op.command(['/usr/bin/systemctl', 'show-environment'])
    require(len(manager) <= 1 << 20, 'The manager environment exceeds its read bound.')
    absent['manager'] = not any(line.partition('=')[0] == RETENTION_ENV for line in manager.decode().splitlines())
    require(re.fullmatch(r'[0-9]{1,10}', properties['MainPID']), 'The current startup PID is invalid.')
    pid = int(properties['MainPID'])
    process_checked = False
    if pid:
        expected_sha = OLD_BINARY_SHA if pid == OLD_PID else args.candidate_sha256
        state = op.service_state(active=True, expected_binary=expected_sha)
        require(state['MainPID'] == str(pid) and os.readlink(f'/proc/{pid}/exe') == str(LIVE / 'goby') and
                Path(f'/proc/{pid}/cgroup').read_text().splitlines() == ['0::/system.slice/' + SERVICE],
                'Retention configuration came from a different running service.')
        if pid == OLD_PID:
            require(state['start_ticks'] == OLD_TICKS, 'The old process changed before configuration inspection.')
        with (Path('/proc') / str(pid) / 'environ').open('rb') as handle:
            raw = handle.read((1 << 20) + 1)
        require(len(raw) <= 1 << 20, 'The process environment exceeds its read bound.')
        absent['process'] = not any(entry.partition(b'=')[0] == RETENTION_ENV.encode() for entry in raw.split(b'\0'))
        process_checked = True
    else:
        require(properties['ActiveState'] in ('inactive', 'activating') and properties['SubState'] in ('dead', 'start', 'start-pre', 'start-post'),
                'An unexpected service state prevents prospective startup configuration inspection.')
    require(all(absent.values()), 'A retention environment override appeared; the default-only plan no longer applies.')
    require(Path('/proc/sys/kernel/random/boot_id').read_text().strip() == OLD_BOOT, 'The planned startup boot changed.')
    return {'overrides_absent': absent, 'process_environment_checked': process_checked, 'observed_pid': pid,
            'effective_retention_days': 30}


def startup_counts(op, args, plan):
    require(LOCK_HELD, 'Startup activity inspection requires deployment ownership.')
    environment = op.database_environment(op.runtime_policy(), MAIN['database'], MAIN['role'], readonly=True)
    deadline = plan['deadline_utc']
    parse_startup_time(deadline)
    statement = """BEGIN READ ONLY; WITH moment AS MATERIALIZED(SELECT clock_timestamp() AS now)
    SELECT json_build_object('database_clock',to_char(moment.now AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
      'activity_total',(SELECT count(*) FROM activity_entries),
      'earliest_activity',(SELECT to_char(min(created_at) AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') FROM activity_entries),
      'legacy_one_day_eligible',(SELECT count(*) FROM activity_entries WHERE created_at < moment.now-interval '1 day'),
      'effective_retention_eligible',(SELECT count(*) FROM activity_entries WHERE created_at < moment.now-interval '30 days'),
      'deadline_eligible',(SELECT count(*) FROM activity_entries WHERE created_at <= '%s'::timestamptz-interval '30 days'),
      'active',json_build_object(
        'runnable_triggers',(SELECT count(*) FROM task_triggers t JOIN task_definitions d ON d.id=t.task_id WHERE d.enabled AND t.retired_at IS NULL AND t.calculation_error=''),
        'active_runs',(SELECT count(*) FROM task_runs WHERE state IN ('pending','running','stopping')),
        'active_children',(SELECT count(*) FROM task_run_children WHERE state IN ('waiting','queued','running')),
        'active_scans',(SELECT count(*) FROM scan_jobs WHERE status IN ('Queued','Running')),
        'active_encodings',(SELECT count(*) FROM encoding_jobs WHERE state NOT IN ('completed','failed','cancelled','interrupted'))))
    FROM moment; ROLLBACK;""" % deadline
    return decode(pg(op, statement, database=MAIN['database'], environment=environment))


def startup_plan_gate(op, args, action=None):
    require(LOCK_HELD, 'A startup checkpoint requires deployment ownership.')
    plan = load_startup_plan(op, args)
    configuration = retention_environment(op, args, plan)
    current = startup_counts(op, args, plan)
    now, deadline = parse_startup_time(current['database_clock']), parse_startup_time(plan['deadline_utc'])
    require(parse_startup_time(plan['observed']['database_clock']) <= now <= deadline, 'The startup plan is stale or the database clock moved backward.')
    if action in ('start', 'smoke-login'):
        require(now + dt.timedelta(seconds=plan['startup_reserve_seconds']) <= deadline,
                'The plan no longer leaves the reserved startup and smoke window.')
    require(set(current) == {'database_clock', 'activity_total', 'earliest_activity', 'legacy_one_day_eligible',
                            'effective_retention_eligible', 'deadline_eligible', 'active'} and
            all(type(current.get(key)) is int and current[key] >= 0 for key in
                ('activity_total', 'legacy_one_day_eligible', 'effective_retention_eligible', 'deadline_eligible')) and
            current['activity_total'] >= plan['observed']['activity_total'] and
            plan['observed']['legacy_one_day_eligible'] <= current['legacy_one_day_eligible'] <= current['activity_total'] and
            (plan['observed']['earliest_activity'] is None or current['earliest_activity'] == plan['observed']['earliest_activity']) and
            current['effective_retention_eligible'] == current['deadline_eligible'] == 0 and
            isinstance(current['active'], dict) and set(current['active']) == set(ACTIVE_COUNTERS) and
            all(type(value) is int and value == 0 for value in current['active'].values()),
            'Work, lost activity history, or retention eligibility violates the finite startup plan.')
    return {'startup_plan_sha256': args.startup_plan_sha256, 'deadline_utc': plan['deadline_utc'],
            'effective_retention_days': 30, 'legacy_one_day_rows_are_preserved': True,
            'observed_legacy_one_day_eligible': plan['observed']['legacy_one_day_eligible'],
            'configuration': configuration, **current}


@contextmanager
def startup_plan_checks(op, args):
    global STARTUP_CONTEXT
    require(LOCK_HELD and STARTUP_CONTEXT is None, 'Nested or unowned startup-plan contexts are not supported.')
    STARTUP_CONTEXT = (op, args)
    try:
        yield
    finally:
        STARTUP_CONTEXT = None


def startup_checkpoint(action=None, required=False):
    global STARTUP_CHECKING
    if STARTUP_CHECKING or STARTUP_CLEANUP:
        return None
    require(not required or STARTUP_CONTEXT is not None, 'A mutation lacks its startup-plan context.')
    if STARTUP_CONTEXT is None:
        return None
    STARTUP_CHECKING = True
    try:
        return startup_plan_gate(*STARTUP_CONTEXT, action=action)
    finally:
        STARTUP_CHECKING = False


@contextmanager
def owned_smoke_cleanup():
    global STARTUP_CLEANUP
    require(not STARTUP_CLEANUP, 'Nested smoke cleanup is not supported.')
    STARTUP_CLEANUP = True
    try:
        yield
    finally:
        STARTUP_CLEANUP = False


def checked_cluster_state(op):
    startup_checkpoint()
    return op.cluster_state()


def pg(op, query, *, database='postgres', environment=None, run=None, label='primary-read'):
    require(LOCK_HELD, 'Primary SQL requires deployment ownership.')
    startup_checkpoint()
    require(database in ('postgres', MAIN['database'], op.RECOVERY['database']) or REHEARSAL.fullmatch(database),
            'SQL escaped the fixed primary cluster or new rehearsal namespace.')
    command = [PG / 'psql', '-X', '--no-password', '-qAt', '-v', 'ON_ERROR_STOP=1', '-d', database]
    if environment is None:
        command = ['/usr/sbin/runuser', '-u', 'postgres', '--', *command, '-p', '5432']
    else:
        require(environment.get('PGPORT') == '5432' and environment.get('PGDATABASE') == database and
                environment.get('PGUSER') == database and environment.get('PGHOST') == '127.0.0.1',
                'An ordinary SQL environment selected another target.')
    return op.command(command, input_bytes=query.encode(), environment=environment, timeout=130, run=run, label=label)


def primary_proof(op, args):
    require(LOCK_HELD, 'Primary identity inspection requires the shared lock.')
    startup_checkpoint()
    process = op.read_primary_process()
    require(process['boot_id'] == args.old_boot_id == OLD_BOOT, 'The main cluster boot changed.')
    sql = decode(pg(op, """BEGIN READ ONLY; SELECT json_build_object(
      'system_identifier',(SELECT system_identifier::text FROM pg_control_system()),
      'port',current_setting('port')::int,'version',current_setting('server_version_num')::int,
      'data_directory',current_setting('data_directory'),
      'start_microseconds',(extract(epoch FROM pg_postmaster_start_time())*1000000)::bigint); ROLLBACK;"""))
    require(sql['system_identifier'] == SYSTEM_IDENTIFIER and sql['port'] == 5432 and sql['version'] == 170011 and
            sql['data_directory'] == '/var/lib/postgresql/17/main' and type(sql['start_microseconds']) is int and
            sql['start_microseconds'] > 0 and op.read_primary_process() == process, 'The main PostgreSQL identity changed.')
    return {'process': process, 'sql': sql}


def listener_inodes():
    listeners = []
    for table in ('tcp', 'tcp6'):
        for line in (Path('/proc/net') / table).read_text().splitlines()[1:]:
            fields = line.split()
            require(len(fields) >= 10, 'A TCP process inventory is malformed.')
            if fields[3] == '0A' and fields[1].rsplit(':', 1)[1] == f'{PORT:04X}':
                listeners.append({'table': table, 'address': fields[1], 'inode': fields[9]})
    return listeners


def exact_service(op, args, *, active=True, expected_sha=OLD_BINARY_SHA, old=True, listener_required=True):
    require(LOCK_HELD, 'Service inspection requires the existing shared lock.')
    startup_checkpoint()
    raw = op.read_file(args.service_pin)
    require(sha(raw) == args.service_pin_sha256, 'The reviewed main-service pin changed.')
    pin = decode(raw)
    require(set(pin) == {'schema', 'version', 'deployment_receipt_sha256', 'boot_id', 'properties'} and
            pin['schema'] == 'goby-main-schema26-service-pin' and type(pin['version']) is int and pin['version'] == 1 and
            pin['deployment_receipt_sha256'] == OLD_TERMINAL_SHA and pin['boot_id'] == OLD_BOOT and
            set(pin['properties']) == set(SERVICE_PROPERTIES), 'The complete main service/resource pin is absent.')
    properties = op.systemd_properties(op.command(['/usr/bin/systemctl', 'show', SERVICE,
        *('-p' + name for name in SERVICE_PROPERTIES)]), SERVICE_PROPERTIES, ('EnvironmentFiles',))
    require({key: value for key, value in properties.items() if key not in DYNAMIC_PROPERTIES} ==
            {key: value for key, value in pin['properties'].items() if key not in DYNAMIC_PROPERTIES} and
            properties['Restart'] == 'on-failure' and Path('/proc/sys/kernel/random/boot_id').read_text().strip() == OLD_BOOT,
            'The exact main service resources, sandbox, or boot changed.')
    require(op.file_fact(op.UNIT)['sha256'] == op.UNIT_SHA and all(
        op.file_fact(path, modes=(0o644,))['sha256'] == digest for path, digest in zip(op.DROPINS, op.DROPIN_SHAS)),
        'A published main unit or drop-in changed.')
    state = op.service_state(active=active, expected_binary=expected_sha)
    listeners = listener_inodes()
    if not active:
        require(not listeners and properties['MainPID'] == '0' and properties['ActiveState'] == 'inactive' and
                properties['SubState'] == 'dead', 'The stopped service still owns or shares its listener.')
        return {'properties': properties, 'boot_id': OLD_BOOT, 'pid': 0, 'binary_sha256': expected_sha}
    pid, ticks = int(state['MainPID']), state['start_ticks']
    require(properties['MainPID'] == str(pid) and properties['ActiveState'] == 'active' and properties['SubState'] == 'running' and
            properties['ControlGroup'] == '/system.slice/' + SERVICE and
            os.readlink(f'/proc/{pid}/exe') == str(LIVE / 'goby') and
            Path(f'/proc/{pid}/cmdline').read_bytes() == (str(LIVE / 'goby') + '\0').encode() and
            Path(f'/proc/{pid}/cgroup').read_text().splitlines() == ['0::/system.slice/' + SERVICE],
            'The main executable, command, or cgroup identity differs.')
    if old:
        require(pid == OLD_PID and ticks == OLD_TICKS and expected_sha == OLD_BINARY_SHA and properties == pin['properties'],
                'The old source18 process lifetime or exact service pin changed.')
    if not listeners and not listener_required:
        return {'properties': properties, 'boot_id': OLD_BOOT, 'pid': pid, 'start_ticks': ticks,
                'binary_sha256': expected_sha, 'listener': None}
    require(len(listeners) == 1 and listeners[0]['table'] == 'tcp' and listeners[0]['address'] == f'0100007F:{PORT:04X}',
            'The main listener is missing, duplicated, or bound outside loopback.')
    sockets = set()
    for path in Path(f'/proc/{pid}/fd').iterdir():
        try:
            sockets.add(os.readlink(path))
        except FileNotFoundError:
            # Other connections can close while the stable listener remains.
            continue
    require('socket:[' + listeners[0]['inode'] + ']' in sockets, 'The expected main PID does not own its listener.')
    return {'properties': properties, 'boot_id': OLD_BOOT, 'pid': pid, 'start_ticks': ticks,
            'binary_sha256': expected_sha, 'listener': listeners[0]}


def protected_state(op, *, diagnostics=False):
    require(LOCK_HELD, 'Private state inspection requires the deployment lock.')
    startup_checkpoint()
    postgres_uid, postgres_gid = op.postgres_identity()
    result = {'runtime': op.file_fact(op.RUNTIME), 'recovery_environment': op.file_fact(op.RECOVERY_ENV),
        'browser': op.file_fact(op.BROWSER), 'master': op.file_fact(op.MASTER, uid=995, gid=995, limit=32),
        'hba': op.file_fact(op.HBA, uid=postgres_uid, gid=postgres_gid, modes=(0o600, 0o640, 0o644)),
        'stores': op.store_state(), 'assets': op.tree_state(LIVE / 'admin', 0, 0, directory_mode=0o755),
        'published_deployment': op.tree_state(PUBLISHED_ROOT, 0, 0),
        'cache': op.directory(Path('/dev/shm/goby-transcodes-test'), 995, 986),
        'cache_owner': op.file_fact(Path('/opt/goby-test/transcode-cache.owner'))}
    result['recovery_database'] = decode(pg(op, """BEGIN READ ONLY; SELECT json_build_object('objects',
      (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname !~ '^pg_' AND n.nspname<>'information_schema')+
      (SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname !~ '^pg_' AND n.nspname<>'information_schema')+
      (SELECT count(*) FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname !~ '^pg_' AND n.nspname<>'information_schema'),
      'extensions',(SELECT count(*) FROM pg_extension WHERE extname<>'plpgsql')); ROLLBACK;""", database=op.RECOVERY['database']))
    require(result['recovery_database'] == {'objects': 0, 'extensions': 0}, 'The preexisting inactive recovery database is not empty.')
    require(result['cache_owner']['sha256'] == 'd28cdc05d8a25c2f067e1507e30bc4a6b0f9dbd99526c0430855fbcd2470a2e7',
            'The previously created cache has another external owner.')
    if diagnostics:
        result['diagnostics'] = op.tree_state(op.DIAGNOSTICS, 995, 986)
    if ROOT in (REPAIR_ROOT, REHEARSAL_REPAIR_ROOT):
        result['failed_upgrade'] = failure_tree(op)
    if ROOT == REHEARSAL_REPAIR_ROOT:
        result['failed_baseline_repair'] = failure_tree(op, second=True)
    return result


def database_activity(op):
    return decode(pg(op, """BEGIN READ ONLY; SELECT json_build_object(
      'sessions',(SELECT count(*) FROM pg_stat_activity WHERE datid=16385),
      'prepared',(SELECT count(*) FROM pg_prepared_xacts WHERE database='goby_test'),
      'slots',(SELECT count(*) FROM pg_replication_slots WHERE database='goby_test')); ROLLBACK;"""))


def failure_tree(op, *, second=False):
    """Read the immutable failed operation, including all directory identities."""
    require(LOCK_HELD and ROOT in (REPAIR_ROOT, REHEARSAL_REPAIR_ROOT) and (not second or ROOT == REHEARSAL_REPAIR_ROOT),
            'Failed-run evidence is available only in its explicit subsequent repair scope.')
    failed_root, failed_run = (REPAIR_ROOT, REHEARSAL_FAILED_RUN) if second else (NORMAL_ROOT, FAILED_RUN)
    result, total = {}, 0
    for path in [failed_root, *sorted(failed_root.rglob('*'))]:
        before = op.path_info(path)
        name = '.' if path == failed_root else op.safe_relative(path.relative_to(failed_root).as_posix())
        value = {'device': before.st_dev, 'inode': before.st_ino, 'uid': before.st_uid, 'gid': before.st_gid,
                 'mode': stat.S_IMODE(before.st_mode), 'nlink': before.st_nlink,
                 'mtime_ns': before.st_mtime_ns, 'ctime_ns': before.st_ctime_ns}
        require(before.st_uid == before.st_gid == 0 and len(result) < 10000,
                'The failed operation has untrusted ownership or unbounded membership.')
        if stat.S_ISDIR(before.st_mode):
            require(path in (failed_root, failed_run, failed_run / 'materials') or path.is_relative_to(failed_run / 'materials'),
                    'The old failed root contains an unrelated operation directory.')
            op.directory(path)
            value['type'] = 'directory'
        else:
            require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and stat.S_IMODE(before.st_mode) == 0o600,
                    'A failed-run file has unexpected type, links, or permissions.')
            raw = op.read_file(path, limit=MAX_FILE)
            total += len(raw)
            require(total <= 1 << 30 and file_identity(op.path_info(path)) == file_identity(before),
                    'Failed-run contents changed during inspection or exceeded the inventory bound.')
            value.update(type='file', bytes=len(raw), sha256=sha(raw))
        result[name] = value
    return result


def read_failure_evidence(op, args, inputs):
    require(LOCK_HELD and args.mode in REPAIR_MODES and ROOT in (REPAIR_ROOT, REHEARSAL_REPAIR_ROOT),
            'Failed-run authority cannot enter a normal operation.')
    raw = op.read_file(args.failure_report)
    require(sha(raw) == args.failure_report_sha256, 'The reviewed failure attestation changed.')
    report = decode(raw)
    require(report.get('schema') == 'goby-main-schema26-baseline-failure' and type(report.get('version')) is int and
            report['version'] == 1 and report.get('failed_root') == str(NORMAL_ROOT) and report.get('failed_run') == str(FAILED_RUN) and
            report.get('source_manifest_sha256') == args.manifest_sha256 and report.get('candidate_sha256') == args.candidate_sha256 and
            report.get('full_report_sha256') == args.full_report_sha256 and report.get('service_pin_sha256') == args.service_pin_sha256 and
            report.get('deployment_receipt_sha256') == OLD_TERMINAL_SHA and report.get('target') == MAIN and
            report.get('system_identifier') == SYSTEM_IDENTIFIER and
            report.get('old_process') == {'pid': OLD_PID, 'start_ticks': OLD_TICKS, 'boot_id': OLD_BOOT} and
            report.get('error_code') == 'column_acl_capture_failed' and report.get('observed_schema') == 25 and
            type(report.get('observed_activity_total')) is int and report['observed_activity_total'] == 15,
            'The explicit repair attestation does not bind this stopped source18 failure.')
    observation_pin = report.get('observation', {})
    require(set(observation_pin) == {'path', 'sha256'} and observation_pin['sha256'] == FAILURE_OBSERVATION_SHA,
            'Repair does not reference the independently observed original failure.')
    observation_path = Path(observation_pin['path'])
    require(observation_path == WORK / 'main-schema26-baseline-failure-review-01/failure-observation.json',
            'The original failure observation path differs.')
    observation_raw = op.read_file(observation_path)
    require(sha(observation_raw) == FAILURE_OBSERVATION_SHA, 'The original failure observation changed.')
    observation = decode(observation_raw)
    require(observation.get('root') == str(NORMAL_ROOT) and observation.get('run') == str(FAILED_RUN) and
            observation.get('failure_code') == 'column_acl_capture_failed' and observation.get('baseline_absent') is True and
            observation.get('pending_empty') is True and observation.get('intent_actions') == ['stop', 'save-materials', 'baseline-dump'] and
            observation.get('observed_service') == {'MainPID': '0', 'ActiveState': 'inactive', 'SubState': 'dead'} and
            observation.get('main_binary_sha256') == OLD_BINARY_SHA,
            'The independent observation did not establish the exact stopped read-only failure.')
    tree = failure_tree(op)
    require(tree == observation.get('inventory'), 'The complete old failed operation changed after review.')
    require(decode(op.read_file(NORMAL_ROOT / 'OWNER.json')) ==
            {'marker': MARKER, 'version': 1, 'path': str(NORMAL_ROOT), 'run_id': FAILED_RUN.name} and
            decode(op.read_file(FAILED_RUN / 'OWNER.json')) == {'marker': MARKER, 'version': 1, 'run_id': FAILED_RUN.name},
            'The failed root or run has another owner.')
    files = {name: fact for name, fact in tree.items() if fact['type'] == 'file'}
    prefix = FAILED_RUN.name + '/'
    require(all(name == 'OWNER.json' or name.startswith(prefix) for name in files), 'The failed root contains another run or file.')
    members = {name.removeprefix(prefix) for name in files if name.startswith(prefix)}
    fixed = {'OWNER.json', 'inputs.json', 'online-preflight.json', 'protected-before.json', 'stopped.json', 'materials.json',
             'terminal.json', 'intent-0001.json', 'intent-0002.json', 'intent-0003.json', 'baseline-cluster.json', '.baseline.json.reserved'}
    pending = [name for name in members if re.fullmatch(r'\.main-schema26-report-[0-9a-f]{32}\.pending', name)]
    diagnostics = {name for name in members if re.fullmatch(r'(?:baseline|stop-main)-[0-9a-f]{12}\.(?:stdout|stderr)', name)}
    materials = decode(op.read_file(FAILED_RUN / 'materials.json'))
    material_files = materials.get('files', {})
    require(isinstance(material_files, dict) and material_files and
            all(op.safe_relative(name) == name and HASH.fullmatch(digest) for name, digest in material_files.items()) and
            materials.get('old_binary_sha256') == OLD_BINARY_SHA and materials.get('automatic_restore_supported') is False,
            'The original private material inventory is not complete source18 evidence.')
    require(len(pending) == 1 and files[prefix + pending[0]]['bytes'] == 0 and
            files[prefix + pending[0]]['sha256'] == sha(b'') and
            members == fixed | set(pending) | diagnostics | {'materials/' + name for name in material_files},
            'The failed operation has a nonempty pending result, missing evidence, or a later mutation artifact.')
    require(op.read_file(FAILED_RUN / '.baseline.json.reserved') == b'goby-main-schema26-migration\nbaseline.json\n',
            'The failed helper did not reserve only its read-only baseline output.')
    for label in ('baseline', 'stop-main'):
        outputs = [name for name in diagnostics if name.startswith(label + '-') and name.endswith('.stdout')]
        errors = [name for name in diagnostics if name.startswith(label + '-') and name.endswith('.stderr')]
        require(len(outputs) == len(errors) == 1 and outputs[0].removesuffix('.stdout') == errors[0].removesuffix('.stderr'),
                'The failed operation has ambiguous command receipts.')
        require(op.read_file(FAILED_RUN / outputs[0]) == b'', 'A failed preparation command has unexpected success output.')
        error = op.read_file(FAILED_RUN / errors[0])
        require((decode(error) == {'schema': 'goby-main-schema26-migration', 'version': 1, 'status': 'failed',
                                  'code': 'column_acl_capture_failed'}) if label == 'baseline' else error == b'',
                'The failure is not the proven pre-snapshot ACL capture error.')
    old_inputs_raw = op.read_file(FAILED_RUN / 'inputs.json')
    old_inputs = decode(old_inputs_raw)
    require(not old_inputs.get('repair_baseline') and old_inputs.get('source_manifest_sha256') == args.manifest_sha256 and
            old_inputs.get('full_report_sha256') == args.full_report_sha256 and old_inputs.get('candidate', {}).get('sha256') == args.candidate_sha256 and
            old_inputs.get('asset_members') == inputs['asset_members'] and old_inputs.get('old_process') == inputs['old_process'] and
            old_inputs.get('service_pin') == inputs['service_pin'] and
            old_inputs.get('published_deployment_receipt_sha256') == OLD_TERMINAL_SHA,
            'The failed operation targeted another product, service, or original deployment.')
    old_tool = old_inputs['tool_source']
    tool_path = Path(old_tool['path'])
    require(tool_path.parent == WORK and re.fullmatch(r'tool-build-[a-z0-9][a-z0-9_-]{1,79}', tool_path.name) and
            old_tool['files'].get('scripts/test-env/upgrade-main-schema26.py') == FAILED_OPERATOR_SHA and
            old_tool['files'].get('scripts/test-env/migrate-main-schema26.go') == FAILED_HELPER_SOURCE_SHA and
            sha(op.read_file(tool_path / 'scripts/test-env/upgrade-main-schema26.py', modes=(0o600, 0o644))) == FAILED_OPERATOR_SHA and
            sha(op.read_file(tool_path / 'scripts/test-env/migrate-main-schema26.go', modes=(0o600, 0o644))) == FAILED_HELPER_SOURCE_SHA,
            'The failed helper invocation did not originate in the exact reviewed read-only code.')
    require(op.file_fact(Path(old_inputs['helper']['path']), modes=(0o755,)) ==
            {key: old_inputs['helper'][key] for key in ('identity', 'sha256')}, 'The old failed helper artifact changed.')
    previous = None
    for number, action in enumerate(('stop', 'save-materials', 'baseline-dump'), 1):
        intent_raw = op.read_file(FAILED_RUN / f'intent-{number:04d}.json')
        intent = decode(intent_raw)
        require(intent.get('marker') == MARKER and intent.get('run_id') == FAILED_RUN.name and intent.get('sequence') == number and
                intent.get('action') == action and intent.get('previous_sha256') == previous and
                intent.get('inputs_sha256') == sha(old_inputs_raw), 'The failed operation has another or incomplete mutation history.')
        previous = sha(intent_raw)
    terminal = decode(op.read_file(FAILED_RUN / 'terminal.json'))
    require(terminal == observation.get('terminal'), 'The original terminal changed after the independent observation.')
    require(terminal.get('marker') == MARKER and terminal.get('version') == 1 and terminal.get('run_id') == FAILED_RUN.name and
            terminal.get('status') == 'failed' and terminal.get('last_completed_phase') == 'stopped' and
            terminal.get('last_intent_sha256') == previous and terminal.get('error_type') == 'Failure' and
            terminal.get('mutation_outcome_must_be_reviewed') is True and terminal.get('automatic_recovery_executed') is False and
            terminal.get('main_database_restored') is False and terminal.get('old_binary_rollback') is False and terminal.get('evidence_retained') is True,
            'The failed terminal does not authorize only the stopped pre-baseline boundary.')
    original = decode(op.read_file(FAILED_RUN / 'online-preflight.json'))
    stopped = decode(op.read_file(FAILED_RUN / 'stopped.json'))
    before = decode(op.read_file(FAILED_RUN / 'protected-before.json'))
    require(original.get('phase') == 'preflight' and original.get('inputs_sha256') == sha(canonical(old_inputs)) and
            original.get('service', {}).get('pid') == OLD_PID and original['service'].get('start_ticks') == OLD_TICKS and
            stopped.get('database_activity') == {'sessions': 0, 'prepared': 0, 'slots': 0} and
            stopped.get('service', {}).get('pid') == 0 and stopped['service'].get('binary_sha256') == OLD_BINARY_SHA and
            {key: value for key, value in before.items() if key != 'diagnostics'} == original['protected'],
            'The original online, stopped, and saved-state evidence disagree.')
    proof = decode(op.read_file(FAILED_RUN / 'baseline-cluster.json'))
    require(proof.get('schema') == 'goby-main-schema26-cluster-proof' and proof.get('run_id') == FAILED_RUN.name and
            all(proof.get(key) == value for key, value in MAIN.items()) and proof.get('system_identifier') == SYSTEM_IDENTIFIER and
            proof.get('source_manifest_sha256') == args.manifest_sha256 and proof.get('tool_manifest_sha256') == old_tool['manifest_sha256'] and
            proof.get('candidate_sha256') == args.candidate_sha256 and proof.get('helper_sha256') == old_inputs['helper']['sha256'] and
            proof.get('postmaster_start_microseconds') == original['primary']['sql']['start_microseconds'],
            'The failed baseline proof belongs to another database or invocation.')
    require({name: fact['sha256'] for name, fact in op.tree_state(FAILED_RUN / 'materials', 0, 0)['files'].items()} == material_files,
            'A saved private material changed after the failed preparation.')
    return {'attestation_sha256': args.failure_report_sha256, 'failed_tree': tree, 'original': original,
            'stopped': stopped, 'protected_before': before, 'activity_total': report['observed_activity_total']}


def read_rehearsal_failure_evidence(op, args, inputs):
    require(LOCK_HELD and ROOT == REHEARSAL_REPAIR_ROOT and args.mode in REHEARSAL_REPAIR_MODES,
            'Second-failure authority requires the explicit rehearsal-repair scope.')
    first = read_failure_evidence(op, args, inputs)
    raw = op.read_file(args.rehearsal_failure_report)
    require(sha(raw) == args.rehearsal_failure_report_sha256, 'The second failure attestation changed.')
    report = decode(raw)
    bindings = {'schema': 'goby-main-schema26-rehearsal-failure', 'version': 1, 'failed_root': str(REPAIR_ROOT),
        'failed_run': str(REHEARSAL_FAILED_RUN), 'source_manifest_sha256': args.manifest_sha256,
        'candidate_sha256': args.candidate_sha256, 'full_report_sha256': args.full_report_sha256,
        'service_pin_sha256': args.service_pin_sha256, 'deployment_receipt_sha256': OLD_TERMINAL_SHA,
        'baseline_failure_report_sha256': args.failure_report_sha256, 'target': MAIN,
        'system_identifier': SYSTEM_IDENTIFIER, 'old_process': {'pid': OLD_PID, 'start_ticks': OLD_TICKS, 'boot_id': OLD_BOOT},
        'error_code': 'pg_authid_rolconfig_missing', 'observed_schema': 25, 'observed_activity_total': 15}
    require(all(report.get(key) == value for key, value in bindings.items()) and type(report.get('version')) is int and
            type(report.get('observed_activity_total')) is int, 'The second failure attestation belongs to another preparation.')
    observation_path = WORK / 'main-schema26-repair-failure-review-01/failure-observation.json'
    require(report.get('observation') == {'path': str(observation_path), 'sha256': REHEARSAL_FAILURE_OBSERVATION_SHA},
            'The independently observed rehearsal-read failure is not pinned.')
    raw = op.read_file(observation_path)
    require(sha(raw) == REHEARSAL_FAILURE_OBSERVATION_SHA, 'The second independent failure observation changed.')
    observation = decode(raw)
    require(observation.get('root') == str(REPAIR_ROOT) and observation.get('run') == str(REHEARSAL_FAILED_RUN) and
            observation.get('failure_code') == 'pg_authid_rolconfig_missing' and observation.get('rehearsal_artifacts_absent') is True and
            observation.get('intent_actions') == ['repair-baseline', 'save-materials', 'baseline-dump'] and
            observation.get('observed_service') == {'MainPID': '0', 'ActiveState': 'inactive', 'SubState': 'dead'} and
            observation.get('main_binary_sha256') == OLD_BINARY_SHA and observation.get('database_observation') ==
            {'activity_total': 15, 'rehearsal_databases': 0, 'rehearsal_roles': 0, 'schema': 25, 'sessions': 0},
            'The second observation does not prove the unchanged stopped pre-rehearsal boundary.')
    diagnosis = observation.get('diagnosis', {})
    require(diagnosis.get('path') == str(WORK / 'main-schema26-repair-failure-review-01/rehearsal-read.stderr') and
            HASH.fullmatch(diagnosis.get('sha256', '')) and sha(op.read_file(Path(diagnosis['path']))) == diagnosis['sha256'],
            'The independently reproduced catalog-column failure changed.')
    tree = failure_tree(op, second=True)
    require(tree == observation.get('inventory'), 'The complete second failed root changed after independent review.')
    run, prefix = REHEARSAL_FAILED_RUN, REHEARSAL_FAILED_RUN.name + '/'
    require(decode(op.read_file(REPAIR_ROOT / 'OWNER.json')) ==
            {'marker': MARKER, 'version': 1, 'path': str(REPAIR_ROOT), 'run_id': run.name} and
            decode(op.read_file(run / 'OWNER.json')) == {'marker': MARKER, 'version': 1, 'run_id': run.name},
            'The second failed operation has another owner.')
    files = {name: fact for name, fact in tree.items() if fact['type'] == 'file'}
    require(all(name == 'OWNER.json' or name.startswith(prefix) for name in files), 'The second failed root contains unrelated files.')
    members = {name.removeprefix(prefix) for name in files if name.startswith(prefix)}
    materials = decode(op.read_file(run / 'materials.json'))
    material_files = materials.get('files', {})
    require(isinstance(material_files, dict) and material_files and materials.get('old_binary_sha256') == OLD_BINARY_SHA and
            materials.get('automatic_restore_supported') is False and
            {name: fact['sha256'] for name, fact in op.tree_state(run / 'materials', 0, 0)['files'].items()} == material_files,
            'The second operation lost its complete original private materials.')
    logs = {name for name in members if re.fullmatch(r'baseline-[0-9a-f]{12}\.(?:stdout|stderr)', name)}
    fixed = {'OWNER.json', 'inputs.json', 'online-preflight.json', 'protected-before.json', 'materials.json', 'terminal.json',
             'intent-0001.json', 'intent-0002.json', 'intent-0003.json', 'baseline-cluster.json', '.baseline.json.reserved',
             'baseline.json', 'database-schema25.dump', 'dump.stderr', 'backup-complete.json'}
    require(members == fixed | logs | {'materials/' + name for name in material_files} and len(logs) == 2 and
            op.read_file(run / '.baseline.json.reserved') == b'goby-main-schema26-migration\nbaseline.json\n' and
            op.read_file(run / 'dump.stderr') == b'', 'The second failure has unfinished output or a later mutation artifact.')
    outputs, errors = ([name for name in logs if name.endswith(suffix)] for suffix in ('.stdout', '.stderr'))
    require(len(outputs) == len(errors) == 1 and outputs[0].removesuffix('.stdout') == errors[0].removesuffix('.stderr') and
            op.read_file(run / errors[0]) == b'', 'The successful read-only baseline has ambiguous command evidence.')
    old_inputs_raw = op.read_file(run / 'inputs.json')
    old_inputs = decode(old_inputs_raw)
    require(old_inputs.get('repair_baseline') is True and not old_inputs.get('repair_rehearsal') and
            old_inputs.get('failure_report') == inputs['failure_report'] and old_inputs.get('source_manifest_sha256') == args.manifest_sha256 and
            old_inputs.get('full_report_sha256') == args.full_report_sha256 and old_inputs.get('candidate', {}).get('sha256') == args.candidate_sha256 and
            old_inputs.get('asset_members') == inputs['asset_members'] and old_inputs.get('service_pin') == inputs['service_pin'] and
            old_inputs.get('old_process') == inputs['old_process'] and
            old_inputs.get('published_deployment_receipt_sha256') == OLD_TERMINAL_SHA,
            'The second failed preparation does not preserve the original accepted input chain.')
    tool = old_inputs['tool_source']
    tool_path = Path(tool['path'])
    require(tool_path.parent == WORK and re.fullmatch(r'tool-build-[a-z0-9][a-z0-9_-]{1,79}', tool_path.name) and
            tool['files'].get('scripts/test-env/upgrade-main-schema26.py') == BASELINE_FAILED_OPERATOR_SHA and
            tool['files'].get('scripts/test-env/migrate-main-schema26.go') == BASELINE_FAILED_HELPER_SOURCE_SHA and
            sha(op.read_file(tool_path / 'scripts/test-env/upgrade-main-schema26.py', modes=(0o600, 0o644))) == BASELINE_FAILED_OPERATOR_SHA and
            sha(op.read_file(tool_path / 'scripts/test-env/migrate-main-schema26.go', modes=(0o600, 0o644))) == BASELINE_FAILED_HELPER_SOURCE_SHA and
            op.file_fact(Path(old_inputs['helper']['path']), modes=(0o755,)) ==
            {key: old_inputs['helper'][key] for key in ('identity', 'sha256')}, 'The second failure originated in different tool bytes.')
    previous = None
    for number, action in enumerate(('repair-baseline', 'save-materials', 'baseline-dump'), 1):
        raw = op.read_file(run / f'intent-{number:04d}.json')
        value = decode(raw)
        require(value.get('marker') == MARKER and value.get('run_id') == run.name and value.get('sequence') == number and
                value.get('action') == action and value.get('previous_sha256') == previous and
                value.get('inputs_sha256') == sha(old_inputs_raw), 'The second failure has another mutation history.')
        previous = sha(raw)
    terminal = decode(op.read_file(run / 'terminal.json'))
    require(terminal == observation.get('terminal') and terminal.get('status') == 'failed' and
            terminal.get('last_completed_phase') == 'backed-up' and terminal.get('last_intent_sha256') == previous and
            terminal.get('automatic_recovery_executed') is False and terminal.get('main_database_restored') is False and
            terminal.get('old_binary_rollback') is False, 'The second terminal crossed the permitted repair boundary.')
    original = decode(op.read_file(run / 'online-preflight.json'))
    before = decode(op.read_file(run / 'protected-before.json'))
    require(original.get('phase') == 'repair-preflight' and original.get('inputs_sha256') == sha(old_inputs_raw) and
            original.get('repair_authority_sha256') == args.failure_report_sha256 and
            before.get('failed_upgrade') == first['failed_tree'] and
            {key: value for key, value in before.items() if key != 'diagnostics'} == original.get('protected'),
            'The second protected-state receipt no longer retains the complete first failure.')
    backup = decode(op.read_file(run / 'backup-complete.json'))
    baseline_raw = op.read_file(run / 'baseline.json')
    baseline = decode(baseline_raw)
    require(backup == observation.get('backup_complete') and backup.get('source_schema') == 25 and
            backup.get('same_exported_snapshot') is True and backup.get('private_materials_retained') is True and
            backup.get('baseline_sha256') == sha(baseline_raw) and backup.get('materials_sha256') == sha(op.read_file(run / 'materials.json')) and
            backup.get('dump') == op.file_fact(run / 'database-schema25.dump'), 'The prior complete baseline and dump changed.')
    proof_raw = op.read_file(run / 'baseline-cluster.json')
    proof = decode(proof_raw)
    require(baseline.get('schema') == 'goby-main-schema26-migration' and baseline.get('mode') == 'inspect' and
            baseline.get('status') == 'inspected' and baseline.get('repair_baseline') is True and not baseline.get('repair_rehearsal') and
            baseline.get('source_schema_version') == 25 and baseline.get('target_schema_version') == 26 and
            baseline.get('rehearsal') is False and baseline.get('run_id') == run.name and baseline.get('target') == MAIN and
            baseline.get('source_manifest_sha256') == args.manifest_sha256 and baseline.get('tool_manifest_sha256') == tool['manifest_sha256'] and
            baseline.get('candidate_sha256') == args.candidate_sha256 and baseline.get('helper_sha256') == old_inputs['helper']['sha256'] and
            baseline.get('cluster_proof_sha256') == sha(proof_raw) and baseline.get('state_sha256') == backup.get('state_sha256') and
            HASH.fullmatch(baseline.get('state_sha256', '')) and baseline.get('state', {}).get('schema_version') == 25 and
            baseline['state'].get('trusted_catalog_verified') is True and len(baseline['state'].get('tables', [])) == 30 and
            proof.get('run_id') == run.name and proof.get('repair_baseline') is True and
            proof.get('postmaster_start_microseconds') == original['primary']['sql']['start_microseconds'],
            'The previous successful baseline is not an exact read-only schema25 report.')
    require(decode(op.read_file(run / outputs[0])) == {'schema': 'goby-main-schema26-migration', 'version': 1,
            'mode': 'inspect', 'status': 'inspected', 'state_sha256': baseline['state_sha256']}, 'The baseline acknowledgement differs.')
    return {'attestation_sha256': args.rehearsal_failure_report_sha256, 'failed_tree': tree, 'first_failed_tree': first['failed_tree'],
            'original': original, 'stopped': {'service': original['service']}, 'protected_before': before,
            'activity_total': 15, 'baseline': baseline}


def repair_preflight(op, args, inputs, published):
    require(LOCK_HELD and ROOT in (REPAIR_ROOT, REHEARSAL_REPAIR_ROOT) and args.mode in REPAIR_MODES and not os.path.lexists(ROOT),
            'Baseline repair must begin in its missing independent root.')
    second = ROOT == REHEARSAL_REPAIR_ROOT
    evidence = (read_rehearsal_failure_evidence if second else read_failure_evidence)(op, args, inputs)
    service = exact_service(op, args, active=False)
    proof = primary_proof(op, args)
    require(service == evidence['stopped']['service'] and proof == evidence['original']['primary'],
            'The stopped main or PostgreSQL process changed after the failed baseline.')
    old_process = Path('/proc') / str(OLD_PID)
    if old_process.exists():
        require((old_process / 'stat').read_text().rsplit(') ', 1)[1].split()[19] != OLD_TICKS,
                'The source18 process lifetime still exists despite the stopped-unit evidence.')
    environment = op.database_environment(op.runtime_policy(), MAIN['database'], MAIN['role'], readonly=True)
    state = decode(pg(op, """BEGIN READ ONLY; SELECT json_build_object(
      'database',current_database(),'database_oid',d.oid::bigint,'owner_oid',d.datdba::bigint,
      'role',current_user,'role_oid',r.oid::bigint,'schema',(SELECT max(version) FROM public.schema_migrations),
      'activity_total',(SELECT count(*) FROM public.activity_entries),
      'safe',NOT(r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication OR r.rolbypassrls) AND r.rolcanlogin,
      'memberships',(SELECT count(*) FROM pg_auth_members m WHERE m.member=r.oid OR m.roleid=r.oid))
      FROM pg_database d JOIN pg_roles r ON r.rolname=current_user WHERE d.datname=current_database(); ROLLBACK;""",
      database=MAIN['database'], environment=environment))
    require(state == {'database': MAIN['database'], 'database_oid': MAIN['database_oid'], 'owner_oid': MAIN['role_oid'],
                     'role': MAIN['role'], 'role_oid': MAIN['role_oid'], 'schema': 25,
                     'activity_total': evidence['activity_total'], 'safe': True, 'memberships': 0},
            'The stopped database no longer has the exact source18 schema, authority, or retained activity population.')
    require(database_activity(op) == {'sessions': 0, 'prepared': 0, 'slots': 0}, 'The stopped primary acquired outside work.')
    absent = decode(pg(op, """BEGIN READ ONLY; SELECT json_build_object(
      'databases',(SELECT count(*) FROM pg_database WHERE datname ~ '^goby_upgrade26_m3e_[0-9a-f]{24}$'),
      'roles',(SELECT count(*) FROM pg_roles WHERE rolname ~ '^goby_upgrade26_m3e_[0-9a-f]{24}$')); ROLLBACK;"""))
    require(absent == {'databases': 0, 'roles': 0}, 'A rehearsal already exists; this boundary cannot be repaired automatically.')
    current = protected_state(op, diagnostics=True)
    retained_key = 'failed_baseline_repair' if second else 'failed_upgrade'
    require({key: value for key, value in current.items() if key != retained_key} == evidence['protected_before'] and
            current[retained_key] == evidence['failed_tree'], 'Private data, materials, or old failure evidence changed while stopped.')
    require(all(current[key] == published['protected_before'][key] for key in ('runtime', 'recovery_environment', 'master')),
            'The published source18 private configuration no longer matches the stopped installation.')
    checked_cluster_state(op)
    return {'marker': MARKER, 'phase': 'repair-preflight', 'service': service, 'primary': proof,
            'protected': {key: value for key, value in current.items() if key != 'diagnostics'}, 'identity': state,
            'quiescent': startup_checkpoint(required=True), 'activity': {'sessions': 0, 'prepared': 0, 'slots': 0},
            'inputs_sha256': sha(canonical(inputs)), 'repair_authority_sha256': evidence['attestation_sha256'],
            'failed_run': str(REHEARSAL_FAILED_RUN if second else FAILED_RUN), 'failed_tree_sha256': sha(canonical(evidence['failed_tree']))}


def preflight(op, args, inputs, published):
    require(LOCK_HELD, 'Preflight requires the deployment lock before any observation.')
    require(not os.path.lexists(ROOT), 'This one-shot upgrade already has evidence; review it instead of retrying.')
    service = exact_service(op, args)
    proof = primary_proof(op, args)
    url = op.runtime_policy()
    environment = op.database_environment(url, MAIN['database'], MAIN['role'], readonly=True)
    identity = decode(pg(op, """BEGIN READ ONLY; SELECT json_build_object(
      'database',current_database(),'database_oid',d.oid::bigint,'owner_oid',d.datdba::bigint,
      'role',current_user,'role_oid',r.oid::bigint,'schema',(SELECT max(version) FROM public.schema_migrations),
      'safe',NOT(r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication OR r.rolbypassrls) AND r.rolcanlogin,
      'memberships',(SELECT count(*) FROM pg_auth_members m WHERE m.member=r.oid OR m.roleid=r.oid))
      FROM pg_database d JOIN pg_roles r ON r.rolname=current_user WHERE d.datname=current_database(); ROLLBACK;""",
      database=MAIN['database'], environment=environment))
    require(identity == {'database': MAIN['database'], 'database_oid': MAIN['database_oid'], 'owner_oid': MAIN['role_oid'],
                         'role': MAIN['role'], 'role_oid': MAIN['role_oid'], 'schema': 25, 'safe': True, 'memberships': 0},
            'Online preflight selected another database, role, or schema.')
    quiescent = startup_checkpoint(required=True)
    activity = database_activity(op)
    require(activity['prepared'] == activity['slots'] == 0, 'The primary has prepared work or replication slots.')
    protected = protected_state(op)
    require(all(protected[key] == published['protected_before'][key] for key in ('runtime', 'recovery_environment', 'master')),
            'The published startup configuration or matching master changed.')
    require(all(protected['assets']['files'].get(name, {}).get('sha256') == digest for name, digest in published['asset_members'].items()),
            'A source18 published asset changed.')
    require(exact_service(op, args) == service and primary_proof(op, args) == proof, 'A pinned process changed during online preflight.')
    return {'marker': MARKER, 'phase': 'preflight', 'service': service, 'primary': proof, 'protected': protected,
            'identity': identity, 'quiescent': quiescent, 'activity': activity, 'inputs_sha256': sha(canonical(inputs))}


def fresh_evidence(op, inputs, observed, run_id):
    require(LOCK_HELD and RUN.fullmatch(run_id), 'Fresh evidence requires locked canonical operation identity.')
    startup_checkpoint(required=True)
    require(not os.path.lexists(ROOT), 'Existing upgrade evidence is never adopted or retried.')
    op.directory(ROOT.parent)
    ROOT.mkdir(mode=0o700)
    op.directory(ROOT)
    op.write_exclusive(ROOT / 'OWNER.json', canonical({'marker': MARKER, 'version': 1, 'path': str(ROOT), 'run_id': run_id}))
    run = ROOT / run_id
    run.mkdir(mode=0o700)
    op.write_exclusive(run / 'OWNER.json', canonical({'marker': MARKER, 'version': 1, 'run_id': run_id}))
    op.write_exclusive(run / 'inputs.json', canonical(inputs))
    op.write_exclusive(run / 'online-preflight.json', canonical(observed))
    op.sync_directory(ROOT.parent)
    op.sync_directory(ROOT)
    return run


def append_intent(op, run, sequence, action, previous_sha256, binding):
    require(LOCK_HELD and run.parent == ROOT and RUN.fullmatch(run.name) and type(sequence) is int and 1 <= sequence <= 9999 and
            action in ACTIONS and (previous_sha256 is None if sequence == 1 else bool(HASH.fullmatch(previous_sha256 or ''))),
            'An intent escaped its locked forward-only operation.')
    op.directory(run)
    require(decode(op.read_file(run / 'OWNER.json')) == {'marker': MARKER, 'version': 1, 'run_id': run.name} and
            not os.path.lexists(run / 'terminal.json'), 'The run is terminal or has another owner.')
    paths = sorted(run.glob('intent-*.json'))
    require(paths == [run / f'intent-{number:04d}.json' for number in range(1, sequence)], 'The intent sequence already advanced or has a gap.')
    expected = None
    inputs_sha = sha(op.read_file(run / 'inputs.json'))
    for number, path in enumerate(paths, 1):
        raw = op.read_file(path)
        value = decode(raw)
        require(value.get('marker') == MARKER and value.get('run_id') == run.name and value.get('sequence') == number and
                value.get('previous_sha256') == expected and value.get('inputs_sha256') == inputs_sha,
                'An existing intent was replaced or belongs to different inputs.')
        expected = sha(raw)
    require(expected == previous_sha256, 'The preceding intent digest changed.')
    value = {'marker': MARKER, 'run_id': run.name, 'sequence': sequence, 'action': action,
             'previous_sha256': expected, 'inputs_sha256': inputs_sha, 'binding': binding,
             'created_at': dt.datetime.now(dt.timezone.utc).isoformat()}
    raw = canonical(value)
    op.write_exclusive(run / f'intent-{sequence:04d}.json', raw)
    return sha(raw)


class Journal:
    def __init__(self, op, run):
        self.op, self.run, self.sequence, self.previous = op, run, 0, None

    def intent(self, action, binding):
        startup = startup_checkpoint(action, required=True)
        digest = append_intent(self.op, self.run, self.sequence + 1, action, self.previous,
                               {**binding, 'startup_plan_check': startup})
        self.sequence += 1
        self.previous = digest
        return digest


def stop_main(op, args, journal, observed):
    require(LOCK_HELD, 'Stopping the main service requires deployment ownership.')
    require(exact_service(op, args) == observed['service'] and primary_proof(op, args) == observed['primary'],
            'The accepted process changed before its stop intent.')
    journal.intent('stop', {'service': observed['service'], 'primary': observed['primary']})
    op.command(['/usr/bin/systemctl', 'stop', SERVICE], timeout=45, run=journal.run, label='stop-main')
    # systemctl stop is synchronous. A failed or unexpected result is not a
    # reason to send another stop, kill a PID, or manipulate an external client.
    stopped = exact_service(op, args, active=False)
    process = Path('/proc') / str(OLD_PID)
    if process.exists():
        require((process / 'stat').read_text().rsplit(') ', 1)[1].split()[19] != OLD_TICKS,
                'The recorded old process survived the stopped unit.')
    deadline = time.monotonic() + 15
    while True:
        activity = database_activity(op)
        require(activity['prepared'] == activity['slots'] == 0, 'Prepared or replication work appeared after stopping.')
        if activity['sessions'] == 0:
            break
        require(time.monotonic() < deadline, 'Primary database clients remain; no external backend will be terminated.')
        time.sleep(0.25)
    checked_cluster_state(op)
    require(primary_proof(op, args) == observed['primary'], 'The primary cluster changed while the main service stopped.')
    op.write_exclusive(journal.run / 'stopped.json', canonical({'service': stopped, 'database_activity': activity}))


def invoke_helper(op, args, inputs, journal, target, url, mode, label, *, snapshot=None, baseline=None, expected_schema=25):
    require(LOCK_HELD, 'The migration helper requires deployment ownership.')
    startup_checkpoint()
    require(mode in ('inspect', 'migrate') and re.fullmatch(r'[a-z][a-z0-9-]{0,54}', label), 'The helper mode or report label is invalid.')
    main = target == MAIN
    require(main or (REHEARSAL.fullmatch(target.get('database', '')) and target.get('role') == target['database'] and
            target.get('owner_comment') == MARKER + ':' + journal.run.name), 'The helper target escaped this primary/rehearsal operation.')
    require((mode == 'inspect' and baseline is None) or (mode == 'migrate' and baseline == journal.run / 'baseline.json' and snapshot is None),
            'Migration requires this run\'s exact baseline and cannot import a transaction snapshot.')
    if mode == 'inspect' and main and expected_schema == 25 and snapshot is None:
        with op.exported_snapshot(op.database_environment(url, MAIN['database'], MAIN['role'])) as exported:
            return invoke_helper(op, args, inputs, journal, target, url, mode, label,
                                 snapshot=exported, expected_schema=expected_schema)
    proof = primary_proof(op, args)
    require(proof == decode(op.read_file(journal.run / 'online-preflight.json'))['primary'],
            'The primary process changed before a helper invocation.')
    value = {'schema': 'goby-main-schema26-cluster-proof', 'version': 1, 'run_id': journal.run.name,
        'repair_baseline': ROOT == REPAIR_ROOT,
        'repair_rehearsal': ROOT == REHEARSAL_REPAIR_ROOT,
        'source_manifest_sha256': inputs['source_manifest_sha256'], 'tool_manifest_sha256': inputs['tool_source']['manifest_sha256'],
        'candidate_sha256': inputs['candidate']['sha256'], 'helper_sha256': inputs['helper']['sha256'],
        **{key: target[key] for key in MAIN}, 'system_identifier': SYSTEM_IDENTIFIER,
        'postmaster_pid': proof['process']['pid'], 'postmaster_start_ticks': proof['process']['start_ticks'], 'boot_id': OLD_BOOT,
        'postmaster_start_microseconds': proof['sql']['start_microseconds'], 'port': 5432, 'server_version': 170011,
        'data_directory': '/var/lib/postgresql/17/main'}
    proof_path = journal.run / (label + '-cluster.json')
    proof_raw = canonical(value)
    op.write_exclusive(proof_path, proof_raw)
    output = journal.run / (label + '.json')
    command = [inputs['helper']['path'], mode, '--output', output, '--expected-os-uid', '0',
        '--expected-database', target['database'], '--expected-database-oid', str(target['database_oid']),
        '--expected-role', target['role'], '--expected-role-oid', str(target['role_oid']),
        '--expected-system-identifier', SYSTEM_IDENTIFIER, '--expected-port', '5432', '--expected-deployment-id', DEPLOYMENT_ID,
        '--cluster-proof', proof_path, '--cluster-proof-sha256', sha(proof_raw), '--run-id', journal.run.name,
        '--source-manifest-sha256', inputs['source_manifest_sha256'], '--tool-manifest-sha256', inputs['tool_source']['manifest_sha256'],
        '--candidate-sha256', inputs['candidate']['sha256'], '--expected-helper-sha256', inputs['helper']['sha256'],
        '--expected-schema', str(expected_schema)]
    if not main:
        command += ['--rehearsal', '--expected-owner-comment', target['owner_comment']]
    if ROOT == REPAIR_ROOT:
        command += ['--repair-baseline']
    if ROOT == REHEARSAL_REPAIR_ROOT:
        command += ['--repair-rehearsal']
    if snapshot:
        command += ['--snapshot-id', snapshot]
    if baseline:
        baseline_sha = sha(op.read_file(baseline))
        command += ['--baseline', baseline, '--baseline-sha256', baseline_sha]
        journal.intent('main-migrate' if main else 'rehearsal-migrate',
                       {'target': target, 'baseline_sha256': baseline_sha, 'cluster_proof_sha256': sha(proof_raw)})
    require(op.file_fact(Path(inputs['helper']['path']), modes=(0o755,)) ==
            {key: inputs['helper'][key] for key in ('identity', 'sha256')}, 'The accepted helper binary changed.')
    startup_checkpoint(('main-migrate' if main else 'rehearsal-migrate') if mode == 'migrate' else None, required=True)
    op.command(command, environment={**op.ENV, 'GOBY_DATABASE_URL': url, 'GOMEMLIMIT': '128MiB'},
               timeout=240, run=journal.run, label=label)
    result = decode(op.read_file(output))
    state = result.get('state', {})
    require(result.get('schema') == 'goby-main-schema26-migration' and result.get('version') == 1 and
            result.get('mode') == mode and result.get('status') == ('inspected' if mode == 'inspect' else 'committed') and
            result.get('source_schema_version') == expected_schema and result.get('target_schema_version') == 26 and
            result.get('repair_baseline') is (ROOT == REPAIR_ROOT) and
            result.get('repair_rehearsal') is (ROOT == REHEARSAL_REPAIR_ROOT) and
            result.get('rehearsal') is (not main) and HASH.fullmatch(result.get('state_sha256', '')) and
            result.get('run_id') == journal.run.name and result.get('source_manifest_sha256') == inputs['source_manifest_sha256'] and
            result.get('tool_manifest_sha256') == inputs['tool_source']['manifest_sha256'] and
            result.get('candidate_sha256') == inputs['candidate']['sha256'] and result.get('helper_sha256') == inputs['helper']['sha256'] and
            result.get('cluster_proof_sha256') == sha(proof_raw) and result.get('target') == {key: target[key] for key in MAIN} and
            state.get('schema_version') == (26 if mode == 'migrate' else expected_schema) and state.get('trusted_catalog_verified') is True and
            len(state.get('tables', [])) == (33 if state['schema_version'] == 26 else 30),
            'The helper did not publish a completion proof bound to the exact invocation.')
    if mode == 'migrate':
        transition = result.get('theme_transition', {})
        require(result.get('input_baseline_sha256') == baseline_sha and HASH.fullmatch(result.get('before_state_sha256', '')) and
                result['before_state_sha256'] == result.get('preserved_state_sha256') and result.get('new_theme_defaults_verified') is True and
                transition.get('virtual_root_count') == 1 and transition.get('resource_count') == 0 and
                transition.get('owner_count') == transition.get('mapped_item_count', -2) + 1 and
                HASH.fullmatch(transition.get('expected_paths_sha256', '')) and
                transition['expected_paths_sha256'] == transition.get('reserved_paths_sha256'),
                'The helper did not preserve old state and verify only the new Theme defaults.')
    require(primary_proof(op, args) == proof, 'The primary process changed during the helper operation.')
    return result


def dump_snapshot(op, journal, environment, snapshot):
    require(LOCK_HELD and environment.get('PGDATABASE') == MAIN['database'] and environment.get('PGPORT') == '5432' and
            re.fullmatch(r'[0-9A-Fa-f]+-[0-9A-Fa-f]+-[0-9]+', snapshot), 'The dump lost its exact main exported snapshot.')
    startup_checkpoint(required=True)
    path, error_path = journal.run / 'database-schema25.dump', journal.run / 'dump.stderr'
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb') as output:
        with os.fdopen(os.open(error_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb') as errors:
            process = subprocess.Popen([str(PG / 'pg_dump'), '--no-password', '--format=custom', '--schema=public',
                                        '--snapshot=' + snapshot, '--no-owner', '--no-privileges'],
                                       env=environment, stdout=output, stderr=errors)
            try:
                deadline = time.monotonic() + 180
                while process.poll() is None:
                    require(time.monotonic() < deadline and path.stat().st_size <= MAX_FILE and error_path.stat().st_size <= 8 << 20,
                            'The private snapshot dump exceeded its time or byte bound.')
                    time.sleep(0.1)
                require(process.returncode == 0, 'The exported main snapshot dump failed.')
            finally:
                if process.poll() is None:
                    # Only this newly spawned child is stopped, never a server
                    # process or another PostgreSQL backend.
                    process.kill()
                    process.wait(timeout=5)
            output.flush()
            errors.flush()
            os.fsync(output.fileno())
            os.fsync(errors.fileno())
    require(0 < path.stat().st_size <= MAX_FILE and error_path.stat().st_size <= 8 << 20,
            'The completed snapshot archive or diagnostic exceeds its bound.')
    op.sync_directory(journal.run)
    return op.file_fact(path)


def save_materials(op, journal, before):
    require(LOCK_HELD, 'Private material capture requires deployment ownership.')
    journal.intent('save-materials', {'protected_sha256': sha(canonical(before))})
    materials = journal.run / 'materials'
    materials.mkdir(mode=0o700)
    entries = [(LIVE / 'goby', 'goby-source18', 0, 0, (0o755,)), (op.RUNTIME, 'runtime.env', 0, 0, (0o600,)),
        (op.RECOVERY_ENV, 'recovery.env', 0, 0, (0o600,)), (op.BROWSER, 'browser.env', 0, 0, (0o600,)),
        (op.MASTER, 'master.key', 995, 995, (0o600,)), (op.UNIT, 'unit.service', 0, 0, (0o600,))]
    entries += [(path, path.name, 0, 0, (0o644,)) for path in op.DROPINS]
    for prefix, directory, uid, gid, inventory in [
        ('admin', LIVE / 'admin', 0, 0, before['assets']['files']),
        *[(path.name, path, 995, 986, before['stores']['trees'][str(path)]['files']) for path in op.STORES],
        ('diagnostics', op.DIAGNOSTICS, 995, 986, before['diagnostics']['files']),
        ('native-pairing', op.PAIRING, 995, 986, before['stores']['pairing']['files'])]:
        entries += [(directory / relative, prefix + '/' + relative, uid, gid, (0o600, 0o644, 0o755)) for relative in inventory]
    copied = {}
    for source, relative, uid, gid, modes in entries:
        target = materials / op.safe_relative(relative)
        op.ensure_material_directory(materials, target.parent)
        raw = op.read_file(source, uid=uid, gid=gid, modes=modes, limit=MAX_FILE)
        op.write_exclusive(target, raw)
        copied[relative] = sha(raw)
    require(copied['goby-source18'] == OLD_BINARY_SHA, 'The captured rollback evidence is not the original source18 binary.')
    op.write_exclusive(journal.run / 'materials.json', canonical({'files': copied, 'old_binary_sha256': OLD_BINARY_SHA,
                                                                'automatic_restore_supported': False}))


def compare_snapshots(before, after):
    require(before.get('schema') == after.get('schema') == 'goby-main-schema26-migration' and
            before.get('state_sha256') == after.get('state_sha256') and before.get('state') == after.get('state'),
            'A complete schema, business row, sequence, identity, privilege, or binding snapshot changed.')
    return {'status': 'equal', 'state_sha256': before['state_sha256']}


def verify_backup(op, journal, receipt):
    require(LOCK_HELD, 'Operational backup verification requires deployment ownership.')
    startup_checkpoint()
    require(decode(op.read_file(journal.run / 'backup-complete.json')) == receipt and
            sha(op.read_file(journal.run / 'baseline.json')) == receipt['baseline_sha256'] and
            op.file_fact(journal.run / 'database-schema25.dump') == receipt['dump'],
            'The baseline or exact operational backup changed before main migration.')
    raw = op.read_file(journal.run / 'materials.json')
    require(sha(raw) == receipt['materials_sha256'], 'The copied private material inventory changed.')
    materials = decode(raw)
    actual = op.tree_state(journal.run / 'materials', 0, 0)
    require(materials.get('old_binary_sha256') == OLD_BINARY_SHA and materials.get('automatic_restore_supported') is False and
            {name: fact['sha256'] for name, fact in actual['files'].items()} == materials['files'],
            'An operational private material copy changed or disappeared.')


def rehearsal_observation_sql(name):
    require(isinstance(name, str) and REHEARSAL.fullmatch(name), 'Rehearsal inspection escaped its new namespace.')
    return """BEGIN READ ONLY; SELECT json_build_object(
      'system_identifier',(SELECT system_identifier::text FROM pg_control_system()),
      'start_microseconds',(extract(epoch FROM pg_postmaster_start_time())*1000000)::bigint,
      'database',(SELECT json_build_object('name',d.datname,'oid',d.oid::bigint,'owner_oid',d.datdba::bigint,
        'comment',shobj_description(d.oid,'pg_database'),'allow',d.datallowconn,'template',d.datistemplate,
        'encoding',pg_encoding_to_char(d.encoding),'acl',d.datacl,
        'clients',(SELECT count(*) FROM pg_stat_activity a WHERE a.datid=d.oid),
        'prepared',(SELECT count(*) FROM pg_prepared_xacts x WHERE x.database=d.datname),
        'slots',(SELECT count(*) FROM pg_replication_slots x WHERE x.database=d.datname))
        FROM pg_database d WHERE d.datname='%s'),
      'role',(SELECT json_build_object('name',r.rolname,'oid',r.oid::bigint,'comment',shobj_description(r.oid,'pg_authid'),
        'login',r.rolcanlogin,'inherit',r.rolinherit,'limit',r.rolconnlimit,
        'config',(SELECT v.rolconfig FROM pg_roles v WHERE v.oid=r.oid),'valid_until',r.rolvaliduntil,
        'safe',NOT(r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication OR r.rolbypassrls),
        'scram',r.rolpassword LIKE 'SCRAM-SHA-256$%%',
        'memberships',(SELECT count(*) FROM pg_auth_members m WHERE m.member=r.oid OR m.roleid=r.oid OR m.grantor=r.oid),
        'settings',(SELECT count(*) FROM pg_db_role_setting WHERE setrole=r.oid),
        'outside_dependencies',(SELECT count(*) FROM pg_shdepend s WHERE s.refclassid='pg_authid'::regclass AND s.refobjid=r.oid
          AND NOT(s.dbid=COALESCE((SELECT oid FROM pg_database WHERE datname='%s'),0) AND s.dbid<>0)
          AND NOT(s.dbid=0 AND s.classid='pg_database'::regclass AND s.objid=COALESCE((SELECT oid FROM pg_database WHERE datname='%s'),0) AND s.deptype='o')))
        FROM pg_authid r WHERE r.rolname='%s')); ROLLBACK;""" % (name, name, name, name)


def observe_rehearsal(op, name):
    require(LOCK_HELD, 'Rehearsal inspection requires deployment ownership.')
    return decode(pg(op, rehearsal_observation_sql(name)))


def validate_rehearsal(receipt, observed, *, database_required=True, closed=False):
    require(LOCK_HELD, 'Rehearsal validation requires shared operator ownership.')
    target = receipt['target']
    require(receipt.get('marker') == MARKER and receipt.get('phase') == 'created' and RUN.fullmatch(receipt.get('run_id', '')) and
            REHEARSAL.fullmatch(target['database']) and target['role'] == target['database'] and
            target['owner_comment'] == MARKER + ':' + receipt['run_id'] and
            type(target['database_oid']) is int and target['database_oid'] > 0 and target['database_oid'] not in (16385, 994944) and
            type(target['role_oid']) is int and target['role_oid'] > 0 and target['role_oid'] not in (16384, 994943) and
            observed['system_identifier'] == SYSTEM_IDENTIFIER and observed['start_microseconds'] == receipt['start_microseconds'],
            'A rehearsal target or cluster escaped its exact creation receipt.')
    role = observed['role']
    require(role and role['name'] == target['role'] and role['oid'] == target['role_oid'] and
            role['comment'] == target['owner_comment'] and role['safe'] is True and role['scram'] is True and
            role['login'] is True and role['inherit'] is False and role['limit'] == 4 and role['config'] is None and
            role['valid_until'] is None and role['memberships'] == role['settings'] == role['outside_dependencies'] == 0,
            'The owned rehearsal role has different authority or outside dependencies.')
    database = observed['database']
    if not database_required:
        require(database is None, 'The rehearsal database still exists.')
        return
    require(database and database['name'] == target['database'] and database['oid'] == target['database_oid'] and
            database['owner_oid'] == target['role_oid'] and database['comment'] == target['owner_comment'] and
            database['allow'] is (not closed) and database['template'] is False and database['encoding'] == 'UTF8' and
            database['acl'] == receipt['database_acl'] and database['clients'] == database['prepared'] == database['slots'] == 0,
            'The rehearsal database identity, permissions, or quiescence changed.')


def create_rehearsal(op, args, journal):
    require(LOCK_HELD, 'Rehearsal creation requires the primary deployment lock.')
    primary = primary_proof(op, args)
    require(primary == decode(op.read_file(journal.run / 'online-preflight.json'))['primary'],
            'The primary process changed before any rehearsal creation.')
    name = 'goby_upgrade26_m3e_' + secrets.token_hex(12)
    before = observe_rehearsal(op, name)
    require(before['database'] is None and before['role'] is None and before['system_identifier'] == SYSTEM_IDENTIFIER and
            before['start_microseconds'] == primary['sql']['start_microseconds'],
            'The fresh rehearsal identity already exists.')
    comment = MARKER + ':' + journal.run.name
    password = secrets.token_hex(32)
    url = 'postgresql://' + name + ':' + password + '@127.0.0.1:5432/' + name + '?sslmode=disable'
    op.write_exclusive(journal.run / 'rehearsal-credentials.json', canonical({'database_url': url}))
    journal.intent('rehearsal-role', {'name': name, 'comment': comment, 'before': before})
    require(primary_proof(op, args) == primary, 'The primary changed before the new role creation.')
    pg(op, 'BEGIN; SET LOCAL password_encryption=\'scram-sha-256\'; CREATE ROLE "' + name + '" LOGIN PASSWORD \'' + password +
       '\' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 4; COMMENT ON ROLE "' + name +
       '" IS \'' + comment + '\'; COMMIT;', run=journal.run, label='create-rehearsal-role')
    role = observe_rehearsal(op, name)['role']
    require(role and type(role['oid']) is int and role['oid'] > 0 and role['comment'] == comment and role['safe'] is True and
            role['memberships'] == role['outside_dependencies'] == 0, 'The new role was not acknowledged as this operation\'s own identity.')
    op.write_exclusive(journal.run / 'rehearsal-role.json', canonical(role))
    checked = observe_rehearsal(op, name)
    require(checked['role'] == role and checked['database'] is None and checked['start_microseconds'] == before['start_microseconds'],
            'The fresh role or empty database namespace changed before creation.')
    journal.intent('rehearsal-database', {'name': name, 'role': role})
    pg(op, 'CREATE DATABASE "' + name + '" OWNER "' + name + '" TEMPLATE template0 ENCODING \'UTF8\';',
       run=journal.run, label='create-rehearsal-database')
    unmarked = observe_rehearsal(op, name)
    database = unmarked['database']
    require(database and database['name'] == name and type(database['oid']) is int and database['oid'] > 0 and
            database['owner_oid'] == role['oid'] and database['comment'] is None and database['clients'] == database['prepared'] == database['slots'] == 0 and
            unmarked['start_microseconds'] == before['start_microseconds'], 'The new database identity was not acknowledged before marking.')
    op.write_exclusive(journal.run / 'rehearsal-database.json', canonical(unmarked))
    journal.intent('rehearsal-mark', {'database': database, 'comment': comment})
    require(observe_rehearsal(op, name) == unmarked, 'The new database changed before its owner comment.')
    pg(op, 'COMMENT ON DATABASE "' + name + '" IS \'' + comment + '\';', run=journal.run, label='mark-rehearsal-database')
    target = {'database': name, 'database_oid': database['oid'], 'role': name, 'role_oid': role['oid'], 'owner_comment': comment}
    receipt = {'marker': MARKER, 'phase': 'created', 'run_id': journal.run.name, 'target': target,
               'start_microseconds': before['start_microseconds'], 'database_acl': database['acl']}
    validate_rehearsal(receipt, observe_rehearsal(op, name))
    require(primary_proof(op, args) == decode(op.read_file(journal.run / 'online-preflight.json'))['primary'],
            'The primary cluster changed during rehearsal creation.')
    op.write_exclusive(journal.run / 'rehearsal-created.json', canonical(receipt))
    return receipt, url


def restore_rehearsal(op, journal, receipt, url, dump):
    require(LOCK_HELD, 'Rehearsal import requires deployment ownership.')
    target = receipt['target']
    require(target != MAIN and REHEARSAL.fullmatch(target['database']) and
            decode(op.read_file(journal.run / 'rehearsal-created.json')) == receipt,
            'Only this freshly created rehearsal may receive an archive import.')
    validate_rehearsal(receipt, observe_rehearsal(op, target['database']))
    require(op.file_fact(journal.run / 'database-schema25.dump') == dump, 'The exact exported main archive changed.')
    environment = op.database_environment(url, target['database'], target['role'], readonly=False)
    count = decode(pg(op, """BEGIN READ ONLY; SELECT (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public')+
      (SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public')+
      (SELECT count(*) FROM pg_namespace WHERE nspname !~ '^pg_' AND nspname NOT IN ('public','information_schema')); ROLLBACK;""",
      database=target['database'], environment=environment))
    require(type(count) is int and count == 0, 'The fresh rehearsal is no longer empty.')
    owner_kind = decode(op.read_file(journal.run / 'baseline.json'))['state']['identity']['schema_owner_kind']
    require(owner_kind in ('database_role', 'pg_database_owner'), 'The source public owner mapping is unsupported.')
    if owner_kind == 'database_role':
        journal.intent('rehearsal-schema-owner', {'target': target, 'schema_owner_kind': owner_kind})
        pg(op, 'ALTER SCHEMA public OWNER TO "' + target['role'] + '";', database=target['database'], environment=environment,
           run=journal.run, label='match-rehearsal-schema-owner')
    toc = op.filtered_restore_toc(op.command([PG / 'pg_restore', '--list', journal.run / 'database-schema25.dump'],
                                           run=journal.run, label='archive-inventory'))
    toc_path = journal.run / 'restore-toc.list'
    op.write_exclusive(toc_path, toc)
    validate_rehearsal(receipt, observe_rehearsal(op, target['database']))
    journal.intent('rehearsal-restore', {'target': target, 'dump': dump, 'toc_sha256': sha(toc)})
    # This is the only pg_restore invocation. MAIN is impossible by the
    # explicit namespace, new creation receipt, target OIDs, and URL checks.
    op.command([PG / 'pg_restore', '--no-password', '--no-owner', '--no-privileges', '--exit-on-error', '--single-transaction',
        '--use-list=' + str(toc_path), '--dbname=' + target['database'], journal.run / 'database-schema25.dump'],
        environment=environment, timeout=240, run=journal.run, label='rehearsal-restore')


def dispose_rehearsal(op, args, inputs, journal, receipt, url, migrated):
    require(LOCK_HELD, 'Rehearsal disposal requires deployment ownership.')
    target = receipt['target']
    require(decode(op.read_file(journal.run / 'rehearsal-created.json')) == receipt,
            'The new rehearsal creation receipt changed before disposal.')
    checked = invoke_helper(op, args, inputs, journal, target, url, 'inspect', 'rehearsal-before-disposal', expected_schema=26)
    compare_snapshots(migrated, checked)
    before = observe_rehearsal(op, target['database'])
    validate_rehearsal(receipt, before)
    journal.intent('rehearsal-close', {'receipt': receipt, 'state_sha256': checked['state_sha256'], 'before': before})
    require(observe_rehearsal(op, target['database']) == before, 'The rehearsal changed at its disposal boundary.')
    pg(op, 'ALTER DATABASE "' + target['database'] + '" ALLOW_CONNECTIONS false;', run=journal.run, label='close-rehearsal')
    closed = observe_rehearsal(op, target['database'])
    validate_rehearsal(receipt, closed, closed=True)
    journal.intent('rehearsal-drop-database', {'receipt': receipt, 'closed': closed})
    require(observe_rehearsal(op, target['database']) == closed, 'Work appeared after fencing the owned rehearsal.')
    pg(op, 'DROP DATABASE "' + target['database'] + '";', run=journal.run, label='drop-rehearsal-database')
    remaining = observe_rehearsal(op, target['database'])
    validate_rehearsal(receipt, remaining, database_required=False)
    journal.intent('rehearsal-drop-role', {'receipt': receipt, 'before': remaining})
    require(observe_rehearsal(op, target['database']) == remaining, 'The isolated rehearsal role changed before deletion.')
    pg(op, 'DROP ROLE "' + target['role'] + '";', run=journal.run, label='drop-rehearsal-role')
    after = observe_rehearsal(op, target['database'])
    require(after['database'] is None and after['role'] is None and after['system_identifier'] == SYSTEM_IDENTIFIER and
            after['start_microseconds'] == receipt['start_microseconds'], 'The new rehearsal disposal was not proven.')
    op.write_exclusive(journal.run / 'rehearsal-disposed.json', canonical({'marker': MARKER, 'status': 'disposed', 'receipt': receipt,
                                                                      'after': after, 'force_or_backend_termination_used': False}))


def rehearse(op, args, inputs, journal, dump):
    receipt, url = create_rehearsal(op, args, journal)
    restore_rehearsal(op, journal, receipt, url, dump)
    result = invoke_helper(op, args, inputs, journal, receipt['target'], url, 'migrate', 'rehearsal-migrated',
                           baseline=journal.run / 'baseline.json')
    dispose_rehearsal(op, args, inputs, journal, receipt, url, result)
    return result


def install_candidate(op, args, inputs, journal, before):
    require(LOCK_HELD, 'Candidate publication requires deployment ownership.')
    exact_service(op, args, active=False)
    assets = op.read_assets(Path(inputs['assets']['path']))
    require({name: sha(raw) for name, raw in assets.items()} == inputs['asset_members'], 'Candidate asset bytes changed before publication.')
    old_assets = before['assets']['files']
    for name in sorted(assets, key=lambda value: (value == 'index.html', value)):
        previous = old_assets.get(name, {}).get('sha256')
        journal.intent('publish-file', {'path': str(LIVE / 'admin' / name), 'before_sha256': previous, 'after_sha256': sha(assets[name])})
        startup_checkpoint('publish-file', required=True)
        op.install_file(LIVE / 'admin' / name, assets[name], previous, journal.run)
    payload = op.read_file(Path(inputs['candidate']['path']), modes=(0o600, 0o755), limit=MAX_FILE)
    require(sha(payload) == inputs['candidate']['sha256'], 'The accepted candidate binary changed.')
    journal.intent('publish-file', {'path': str(LIVE / 'goby'), 'before_sha256': OLD_BINARY_SHA, 'after_sha256': sha(payload)})
    startup_checkpoint('publish-file', required=True)
    op.install_file(LIVE / 'goby', payload, OLD_BINARY_SHA, journal.run)
    verify_installed(op, inputs, before)
    exact_service(op, args, active=False, expected_sha=inputs['candidate']['sha256'], old=False)


def verify_installed(op, inputs, before):
    require(LOCK_HELD, 'Installation verification requires deployment ownership.')
    startup_checkpoint()
    require(op.file_fact(LIVE / 'goby', modes=(0o755,))['sha256'] == inputs['candidate']['sha256'], 'The installed binary differs.')
    current = op.tree_state(LIVE / 'admin', 0, 0, directory_mode=0o755)
    expected = {name: fact['sha256'] for name, fact in before['assets']['files'].items()}
    expected.update(inputs['asset_members'])
    require({name: fact['sha256'] for name, fact in current['files'].items()} == expected,
            'An installed candidate or retained historical asset differs.')
    for name, fact in before['assets']['files'].items():
        if name not in inputs['asset_members']:
            require(current['files'][name] == fact, 'An unrelated historical asset was replaced.')


def wait_ready(op, args, inputs):
    require(LOCK_HELD, 'Startup observation requires deployment ownership.')
    deadline, first = time.monotonic() + 45, None
    while time.monotonic() < deadline:
        state = op.systemd_properties(op.command(['/usr/bin/systemctl', 'show', SERVICE, '-pMainPID', '-pActiveState', '-pSubState']),
                                      ('MainPID', 'ActiveState', 'SubState'))
        if state['ActiveState'] == 'activating' or (state['MainPID'] == '0' and state['ActiveState'] == 'inactive'):
            time.sleep(0.25)
            continue
        active = exact_service(op, args, expected_sha=inputs['candidate']['sha256'], old=False, listener_required=False)
        require((active['pid'], active['start_ticks']) != (OLD_PID, OLD_TICKS), 'Startup reused the old source18 lifetime.')
        if first is None:
            first = (active['pid'], active['start_ticks'])
        require((active['pid'], active['start_ticks']) == first, 'The new process restarted during startup.')
        if active['listener'] is None:
            time.sleep(0.25)
            continue
        connection = http.client.HTTPConnection('127.0.0.1', PORT, timeout=1)
        try:
            connection.request('GET', '/readyz', headers={'Connection': 'close'})
            response = connection.getresponse()
            raw = response.read(65537)
            require(len(raw) <= 65536, 'The readiness response exceeded its bound.')
            if response.status == 200:
                return active
            require(response.status == 503, 'Readiness returned an unexpected status.')
        except (OSError, http.client.HTTPException):
            pass
        finally:
            connection.close()
        time.sleep(0.25)
    raise Failure('The new main process did not become ready in its bounded startup window.')


def native_smoke(op, args, inputs, journal, active, old_backups):
    require(LOCK_HELD, 'Native smoke requires deployment ownership.')
    credentials = op.private_values(op.BROWSER, {'GOBY_SMOKE_NAME', 'GOBY_SMOKE_PASSWORD'})
    cookie, csrf, login_user = '', '', ''
    acknowledgement = None
    login_intent = None
    logout_intent_written = False
    cleanup_intent_error = None
    calls = []

    def request(method, path, body=None, expected=200):
        nonlocal cookie, csrf, acknowledgement
        require((method == 'GET' and path in (*op.NATIVE_READS, '/healthz', '/readyz', '/admin/')) or
                (method in ('POST', 'DELETE') and path == op.SESSION), 'Smoke escaped its fixed bounded request set.')
        require(len(calls) < 24 and exact_service(op, args, expected_sha=inputs['candidate']['sha256'], old=False) == active,
                'The exact installed process changed during smoke.')
        headers = {'Accept': 'application/json', 'Origin': f'http://127.0.0.1:{PORT}', 'Connection': 'close'}
        if cookie:
            headers['Cookie'] = 'goby_session=' + cookie
            headers['X-CSRF-Token'] = csrf
        payload = canonical(body) if body is not None else None
        if payload is not None:
            headers['Content-Type'] = 'application/json'
        connection = http.client.HTTPConnection('127.0.0.1', PORT, timeout=10)
        try:
            if method == 'POST':
                startup_checkpoint('smoke-login', required=True)
            connection.request(method, path, payload, headers)
            response = connection.getresponse()
            if method == 'POST':
                cookies = SimpleCookie()
                cookies.load(response.getheader('Set-Cookie', ''))
                if 'goby_session' in cookies:
                    issued = cookies['goby_session'].value
                    require(re.fullmatch(r'[A-Za-z0-9_-]{32,512}', issued) and not cookie, 'The new smoke cookie has an unexpected shape.')
                    cookie, csrf = issued, sha(('goby:admin:csrf:' + issued).encode())
                    # Persist the acknowledged new credential before reading or
                    # trusting its identity response. Existing sessions are never used.
                    acknowledgement = canonical({'cookie': cookie, 'csrf': csrf, 'run_id': journal.run.name,
                                                 'service': active, 'username': credentials['GOBY_SMOKE_NAME']})
                    op.write_exclusive(journal.run / 'smoke-session-private.json', acknowledgement)
            raw = response.read((2 << 20) + 1)
            calls.append({'method': method, 'path': path, 'status': response.status, 'bytes': len(raw)})
            require(len(raw) <= 2 << 20 and response.status == expected, 'A smoke response failed its bounded expectation.')
            if path == '/admin/':
                require(sha(raw) == inputs['asset_members']['index.html'], 'The served native index differs from the accepted assets.')
            return decode(raw) if path.startswith('/admin/v1/') and raw else None
        finally:
            connection.close()

    logout_verified = False
    try:
        request('GET', '/healthz')
        request('GET', '/readyz')
        request('GET', '/admin/')
        login_intent = journal.intent('smoke-login', {'service': active, 'credential_file_sha256': op.file_fact(op.BROWSER)['sha256'],
            'conditional_cleanup': {'method': 'DELETE', 'path': op.SESSION, 'only_this_new_login_cookie': True}})
        value = request('POST', op.SESSION, {'Name': credentials['GOBY_SMOKE_NAME'], 'Password': credentials['GOBY_SMOKE_PASSWORD']})
        require(cookie and csrf and value.get('CSRFToken') == csrf and
                value.get('User', {}).get('Name') == credentials['GOBY_SMOKE_NAME'] and value['User'].get('IsAdministrator') is True,
                'The new smoke login did not prove its expected administrator identity.')
        login_user = value['User']['Id']
        for path in op.NATIVE_READS:
            value = request('GET', path)
            if path == op.SESSION:
                require(value.get('User', {}).get('Id') == login_user and value['User'].get('IsAdministrator') is True,
                        'The fresh smoke session changed identity.')
            if path == op.BACKUPS:
                listed = {row.get('Id'): row for row in value.get('Items', [])}
                require(all(row['id'] in listed and listed[row['id']].get('SHA256') == row['sha256'] for row in old_backups),
                        'The native backup list lost or changed a retained archive.')
    finally:
        try:
            if cookie:
                require(acknowledgement is not None and login_intent, 'A cleanup cookie has no preceding login intent.')
                try:
                    journal.intent('smoke-logout', {'private_session_sha256': sha(acknowledgement), 'service': active})
                    logout_intent_written = True
                except Exception as error:
                    # The durable login intent already authorizes cleanup of
                    # only its newly issued cookie. A full disk must not stop
                    # that logout merely because the acknowledgement or the
                    # additional cleanup evidence cannot be persisted.
                    require(HASH.fullmatch(login_intent), 'New-session cleanup has no durable paired intent.')
                    cleanup_intent_error = type(error).__name__
                with owned_smoke_cleanup():
                    request('DELETE', op.SESSION, expected=204)
                    request('GET', op.SESSION, expected=401)
                logout_verified = True
        finally:
            op.write_exclusive(journal.run / 'native-smoke.json', canonical({'calls': calls, 'login_acknowledged': bool(cookie),
                'logout_verified': logout_verified, 'new_session_only': True, 'backup_create_requests': 0,
                'restore_requests': 0, 'archive_delete_requests': 0, 'legitimate_authentication_history_retained': True,
                'separate_logout_intent_written': logout_intent_written, 'paired_login_cleanup_intent_sha256': login_intent,
                'cleanup_checkpoint_error_type': cleanup_intent_error}))
    require(logout_verified, 'The newly issued smoke session has no logout proof.')
    require(cleanup_intent_error is None, 'Only new-session cleanup completed after a failed startup or evidence checkpoint.')


def preserved_private(op, before, *, installed_inputs=None, allow_log_append=False):
    current = protected_state(op, diagnostics=not allow_log_append)
    # Logs may legitimately append after startup; private configuration,
    # recovery state, paired secrets, archives, and old evidence may not drift.
    ignored = {'diagnostics'} if allow_log_append else set()
    if installed_inputs is not None:
        verify_installed(op, installed_inputs, before)
        ignored.add('assets')
    require({key: value for key, value in current.items() if key not in ignored} ==
            {key: value for key, value in before.items() if key not in ignored},
            'Protected private state, old archives, paired secrets, or historical evidence changed.')
    return current


def upgrade(op, args, inputs, published):
    require(LOCK_HELD, 'Upgrade requires the existing exclusive main deployment lock.')
    second = args.mode == 'rehearsal-repair'
    repairing = args.mode in ('repair-baseline', 'rehearsal-repair')
    require((ROOT == REHEARSAL_REPAIR_ROOT) is second and ((ROOT in (REPAIR_ROOT, REHEARSAL_REPAIR_ROOT)) is repairing),
            'The selected operation root does not match its explicit mode.')
    read_repair = read_rehearsal_failure_evidence if second else read_failure_evidence
    failed_run = REHEARSAL_FAILED_RUN if second else FAILED_RUN
    observed = repair_preflight(op, args, inputs, published) if repairing else preflight(op, args, inputs, published)
    run_id = 'run-' + dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '-' + secrets.token_hex(12)
    run = fresh_evidence(op, inputs, observed, run_id)
    journal = Journal(op, run)
    phase = 'preflight-recorded'
    try:
        require(candidate_inputs(op, args, published) == inputs, 'Accepted artifacts changed before stopping the main service.')
        if repairing:
            evidence = read_repair(op, args, inputs)
            require(sha(canonical(evidence['failed_tree'])) == observed['failed_tree_sha256'] and
                    exact_service(op, args, active=False) == observed['service'], 'The reviewed stopped repair boundary changed.')
            journal.intent('repair-rehearsal' if second else 'repair-baseline', {'failed_run': str(failed_run),
                'failure_report_sha256': evidence['attestation_sha256'],
                'failed_tree_sha256': observed['failed_tree_sha256'], 'old_run_read_only': True, 'old_service_restart': False})
        else:
            stop_main(op, args, journal, observed)
        phase = 'stopped'
        url = op.runtime_policy()
        environment = op.database_environment(url, MAIN['database'], MAIN['role'], readonly=True)
        before = protected_state(op, diagnostics=True)
        require({key: value for key, value in before.items() if key != 'diagnostics'} == observed['protected'],
                'Stopping the service changed protected preflight state.')
        op.write_exclusive(run / 'protected-before.json', canonical(before))
        save_materials(op, journal, before)
        journal.intent('baseline-dump', {'primary': observed['primary'], 'source_schema': 25})
        with op.exported_snapshot(environment) as snapshot:
            baseline = invoke_helper(op, args, inputs, journal, MAIN, url, 'inspect', 'baseline', snapshot=snapshot)
            if second:
                compare_snapshots(evidence['baseline'], baseline)
            dump = dump_snapshot(op, journal, environment, snapshot)
        phase = 'backed-up'
        backup_receipt = {'source_schema': 25, 'baseline_sha256': sha(op.read_file(run / 'baseline.json')),
            'state_sha256': baseline['state_sha256'], 'dump': dump, 'same_exported_snapshot': True,
            'private_materials_retained': True, 'materials_sha256': sha(op.read_file(run / 'materials.json'))}
        op.write_exclusive(run / 'backup-complete.json', canonical(backup_receipt))
        exact_service(op, args, active=False)
        checked_cluster_state(op)
        preserved_private(op, before)
        rehearsed = rehearse(op, args, inputs, journal, dump)
        phase = 'rehearsed'
        preserved_private(op, before)
        exact_service(op, args, active=False)
        checked_cluster_state(op)
        current = invoke_helper(op, args, inputs, journal, MAIN, url, 'inspect', 'main-before-migration')
        compare_snapshots(baseline, current)
        verify_backup(op, journal, backup_receipt)
        require(candidate_inputs(op, args, published) == inputs, 'Acceptance evidence changed before main migration.')
        if repairing:
            require(sha(canonical(read_repair(op, args, inputs)['failed_tree'])) == observed['failed_tree_sha256'],
                    'The original failed preparation changed before the sole main migration.')
        # The sole main schema mutation is the helper's in-place migration.
        # The operational dump is never an input to a main-database command.
        migrated = invoke_helper(op, args, inputs, journal, MAIN, url, 'migrate', 'main-migrated', baseline=run / 'baseline.json')
        phase = 'migrated'
        checked_cluster_state(op)
        preserved_private(op, before)
        checked = invoke_helper(op, args, inputs, journal, MAIN, url, 'inspect', 'main-after-migration', expected_schema=26)
        compare_snapshots(migrated, checked)
        install_candidate(op, args, inputs, journal, before)
        phase = 'installed'
        preserved_private(op, before, installed_inputs=inputs)
        startup_checkpoint(required=True)
        require(database_activity(op) == {'sessions': 0, 'prepared': 0, 'slots': 0}, 'Work appeared before startup.')
        journal.intent('start', {'candidate_sha256': inputs['candidate']['sha256'], 'migrated_state_sha256': migrated['state_sha256']})
        exact_service(op, args, active=False, expected_sha=inputs['candidate']['sha256'], old=False)
        startup_checkpoint('start', required=True)
        op.command(['/usr/bin/systemctl', 'start', SERVICE], timeout=45, run=run, label='start-main')
        phase = 'start-requested'
        active = wait_ready(op, args, inputs)
        phase = 'ready'
        native_smoke(op, args, inputs, journal, active, before['stores']['backups'])
        final_private = preserved_private(op, before, installed_inputs=inputs, allow_log_append=True)
        require(exact_service(op, args, expected_sha=inputs['candidate']['sha256'], old=False) == active and
                primary_proof(op, args) == observed['primary'], 'A main process changed before terminal publication.')
        terminal = {'marker': MARKER, 'version': 1, 'status': 'passed', 'run_id': run.name,
            'source_manifest_sha256': inputs['source_manifest_sha256'], 'binary_sha256': inputs['candidate']['sha256'],
            'schema_version': 26, 'old_schema_version': 25, 'active_service': active,
            'repair_baseline': repairing and not second, 'repair_rehearsal': second, 'failed_run': str(failed_run) if repairing else None,
            'failure_report_sha256': args.failure_report_sha256 if repairing else None,
            'rehearsal_failure_report_sha256': args.rehearsal_failure_report_sha256 if second else None,
            'baseline_sha256': sha(op.read_file(run / 'baseline.json')), 'dump': dump,
            'rehearsal_state_sha256': rehearsed['state_sha256'], 'migration_report_sha256': sha(op.read_file(run / 'main-migrated.json')),
            'old_state_preserved_at_migration_commit': True, 'pre_start_state_rechecked': True,
            'new_theme_defaults_verified': True, 'private_state_preserved': True,
            'final_private_sha256': sha(canonical(final_private)), 'old_archives_retained': True,
            'main_database_restored': False, 'old_binary_rollback': False, 'automatic_retry': False,
            'authentication_history_retained': True, 'post_smoke_all_table_equality_claimed': False,
            'last_intent_sha256': journal.previous}
        op.write_exclusive(run / 'terminal.json', canonical(terminal))
        return {'status': 'passed', 'report': str(run / 'terminal.json'), 'sha256': sha(canonical(terminal))}
    except BaseException as error:
        # A timed-out command or missing commit acknowledgement has unknown
        # outcome. Retain it for explicit forward repair, without cleanup,
        # resubmission, main restore, service restart, or old-binary rollback.
        failed = {'marker': MARKER, 'version': 1, 'run_id': run.name, 'status': 'failed', 'last_completed_phase': phase,
                  'error_type': type(error).__name__, 'reason': failure_reason(op, error), 'last_intent_sha256': journal.previous,
                  'mutation_outcome_must_be_reviewed': True, 'automatic_recovery_executed': False,
                  'main_database_restored': False, 'old_binary_rollback': False, 'evidence_retained': True}
        if not os.path.lexists(run / 'terminal.json'):
            op.write_exclusive(run / 'terminal.json', canonical(failed))
        raise


def parser():
    value = argparse.ArgumentParser(description=__doc__)
    value.add_argument('--mode', choices=('preflight', 'upgrade', *REPAIR_MODES), required=True)
    value.add_argument('--failure-report', type=Path)
    value.add_argument('--failure-report-sha256')
    value.add_argument('--rehearsal-failure-report', type=Path)
    value.add_argument('--rehearsal-failure-report-sha256')
    for name in ('operator', 'deployment-receipt', 'source', 'candidate', 'assets', 'full-report', 'helper', 'tool-source',
                 'helper-build-report', 'guard-report', 'service-pin', 'release-report', 'startup-plan'):
        value.add_argument('--' + name, type=Path, required=True)
    for name in ('script', 'operator', 'deployment-receipt', 'manifest', 'candidate', 'assets', 'full-report', 'helper',
                 'tool-manifest', 'helper-build-report', 'guard-report', 'service-pin', 'release-report', 'startup-plan'):
        value.add_argument('--' + name + '-sha256', required=True)
    value.add_argument('--old-pid', type=int, required=True)
    value.add_argument('--old-start-ticks', required=True)
    value.add_argument('--old-boot-id', required=True)
    return value


def main(argv=None):
    args = parser().parse_args(argv)
    op = None
    try:
        require(sys.platform == 'linux' and os.getuid() == os.geteuid() == os.getgid() == os.getegid() == 0 and
                os.environ.get('SSH_CONNECTION'), 'Run only through authorized root SSH on test-env.')
        os.umask(0o077)
        validate_arguments(args)
        op, published = load_published_operator(args)
        with operation_scope(args):
            with deployment_lock(op):
                inputs = candidate_inputs(op, args, published)
                with startup_plan_checks(op, args):
                    if args.mode in ('preflight', 'repair-preflight', 'rehearsal-repair-preflight'):
                        observed = (preflight if args.mode == 'preflight' else repair_preflight)(op, args, inputs, published)
                        result = {'status': 'preflight-passed', 'inputs_sha256': sha(canonical(inputs)),
                                  'preflight_sha256': sha(canonical(observed)), 'mutations_executed': False}
                    else:
                        result = upgrade(op, args, inputs, published)
        print(json.dumps(result))
        return 0
    except Exception as error:
        print(json.dumps({'status': 'blocked', 'reason': failure_reason(op, error),
                          'automatic_recovery_executed': False, 'main_database_restored': False}))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
