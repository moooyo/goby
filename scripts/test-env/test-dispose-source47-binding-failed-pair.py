#!/usr/bin/env python3
"""Guard source47/schema28 targeted disposal with two reads and no live effects.

The operator is never imported. Only constants and selected pure functions are
compiled from its AST. The SHA-pinned runner contributes pure validators and its
actual remove_pair method, whose external dependencies are memory-only mocks.
After loading, an audit hook and API fence deny filesystem, process, network,
dynamic import, and code-loading effects. The final JSON uses existing stdout.
"""

import ast
import builtins
import copy
import hashlib
import importlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import socket
import subprocess
import sys
from types import FunctionType, SimpleNamespace
import unittest

sys.dont_write_bytecode = True


class MemoryPath(PurePosixPath):
    def __init__(self, *segments):
        # The inspected Python 3.13 constructor imports ntpath unconditionally,
        # although it never uses it. Preserve its POSIX raw-path representation
        # for these bounded fixtures without accepting arbitrary fspath hooks.
        paths = []
        for segment in segments:
            if isinstance(segment, PurePosixPath):
                paths.extend(segment._raw_paths)
            elif type(segment) is str:
                paths.append(segment)
            else:
                raise TypeError('Memory paths accept only trusted POSIX paths or strings.')
        self._raw_paths = paths

    def with_segments(self, *segments):
        return type(self)(*segments)


EXPECTED_RUNNER = PurePosixPath('/opt/goby-test/exec-work-m3e/source-attempt-47/scripts/test-env/run-client-backup-tests.py')
EXPECTED_RUNNER_SHA = '4c76cfa22fd1915a270756d82e313ce6e7bbf3a5bcc857339fd6d254f5c894a2'
OP = None
TREE = None
REMOVE_PAIR = None
VALIDATE_ROWS = None
VALIDATE_DISPOSAL = None
ATTESTATION = None
BLOCKED_EFFECTS = []
BLOCKED_EFFECT_DETAILS = []
SAFE_BUILTINS = {name: getattr(builtins, name) for name in
    ('all', 'any', 'bool', 'dict', 'int', 'isinstance', 'len', 'list', 'set', 'sorted', 'str', 'tuple', 'type')}
# Parse only fixed guard expressions before installing the effect fence.
EXPECTED_RETRY_GATE = ast.parse(
    "not m.present(disposal_path) and not any(p.name.startswith('disposal-') for p in OUTPUT.iterdir()) and "
    "not any(m.present(OUTPUT / (p['name'] + '-objects.json')) for p in PAIRS)", mode='eval').body
EXPECTED_TERMINAL_CHECK = ast.parse(
    "m.sha(m.private_read(EXECUTION / 'terminal.json')) == TERMINAL_SHA", mode='eval').body


def memory_walk(node):
    # ast.walk imports collections at call time, even when already cached.
    # This traversal stays inside the code that was loaded before the fence.
    pending = [node]
    position = 0
    while position < len(pending):
        current = pending[position]
        position += 1
        yield current
        pending.extend(ast.iter_child_nodes(current))


def require(value, message):
    if not value:
        raise ValueError(message)


class EffectBlocked(RuntimeError):
    pass


def deny_effect(name):
    def denied(*args, **kwargs):
        BLOCKED_EFFECTS.append(name)
        detail = {'effect': name}
        if name == 'builtins.__import__' and args and isinstance(args[0], str):
            detail['module'] = args[0]
        BLOCKED_EFFECT_DETAILS.append(detail)
        raise EffectBlocked('The memory-only fence blocked ' + name + '.')
    return denied


def install_effect_fence():
    def audit(event, args):
        if event in {'open', 'import', 'exec', 'compile', 'builtins.input', 'builtins.breakpoint'} or event.startswith(
                ('os.', 'subprocess.', 'socket.', 'ctypes.', 'fcntl.', 'mmap.', 'shutil.', 'tempfile.', 'pty.')):
            deny_effect('audit:' + event)()
    sys.addaudithook(audit)
    surfaces = [
        (builtins, ('open', '__import__', 'input', 'breakpoint', 'compile', 'exec', 'eval')),
        (io, ('open', 'open_code', 'FileIO')),
        (sys.modules['_io'], ('open', 'open_code', 'FileIO')),
        (importlib, ('import_module', 'reload')),
        (subprocess, ('Popen', 'run', 'call', 'check_call', 'check_output', 'getoutput', 'getstatusoutput')),
        (socket, ('socket', 'socketpair', 'create_connection', 'create_server', 'getaddrinfo',
                  'gethostbyname', 'gethostbyname_ex', 'gethostbyaddr', 'getnameinfo')),
        (Path, ('open', 'read_bytes', 'read_text', 'write_bytes', 'write_text', 'touch', 'mkdir', 'chmod',
                'lchmod', 'unlink', 'rmdir', 'rename', 'replace', 'symlink_to', 'hardlink_to', 'link_to',
                'iterdir', 'glob', 'rglob', 'stat', 'lstat', 'owner', 'group', 'resolve', 'absolute',
                'exists', 'is_file', 'is_dir', 'is_symlink', 'is_mount', 'readlink', 'samefile')),
        (os, ('open', 'close', 'read', 'write', 'pread', 'pwrite', 'fdopen', 'stat', 'lstat', 'fstat',
              'listdir', 'scandir', 'walk', 'fwalk', 'access', 'chdir', 'fchdir', 'getcwd', 'getcwdb',
              'mkdir', 'makedirs', 'rmdir', 'removedirs', 'remove', 'unlink', 'rename', 'renames',
              'replace', 'chmod', 'fchmod', 'lchmod', 'chown', 'fchown', 'lchown', 'link', 'symlink',
              'readlink', 'truncate', 'ftruncate', 'utime', 'fsync', 'fdatasync', 'sync', 'system',
              'popen', 'fork', 'forkpty', 'posix_spawn', 'posix_spawnp', 'execv', 'execve', 'execvp',
              'execvpe', 'execl', 'execle', 'execlp', 'execlpe', 'spawnv', 'spawnve', 'spawnvp',
              'spawnvpe', 'kill', 'killpg', 'putenv', 'unsetenv', 'setuid', 'setgid', 'setgroups')),
    ]
    for module, names in surfaces:
        for name in names:
            if hasattr(module, name):
                setattr(module, name, deny_effect(getattr(module, '__name__', 'Path') + '.' + name))


def constant_value(node, scope):
    if isinstance(node, ast.Constant):
        return node.value
    if isinstance(node, ast.Name) and node.id in scope:
        return scope[node.id]
    if isinstance(node, (ast.List, ast.Tuple)):
        items = [constant_value(item, scope) for item in node.elts]
        return items if isinstance(node, ast.List) else tuple(items)
    if isinstance(node, ast.Dict):
        return {constant_value(key, scope): constant_value(value, scope) for key, value in zip(node.keys, node.values)}
    if isinstance(node, ast.BinOp) and isinstance(node.op, (ast.Add, ast.Div)):
        left, right = constant_value(node.left, scope), constant_value(node.right, scope)
        return left + right if isinstance(node.op, ast.Add) else left / right
    if isinstance(node, ast.Call) and isinstance(node.func, ast.Name) and node.func.id in ('Path', 'str'):
        require(len(node.args) == 1 and not node.keywords, 'A constant constructor changed.')
        return (MemoryPath if node.func.id == 'Path' else str)(constant_value(node.args[0], scope))
    raise ValueError('An operator constant is not a reviewed literal expression.')


def compile_functions(nodes, scope):
    for function in nodes:
        require(not function.decorator_list, 'An extracted function acquired a decorator.')
        require(not any(isinstance(node, (ast.Import, ast.ImportFrom, ast.Global, ast.Nonlocal, ast.ClassDef))
                        for node in memory_walk(function)), 'An extracted function acquired runtime code loading.')
    scope['__builtins__'] = SAFE_BUILTINS
    exec(compile(ast.Module(nodes, type_ignores=[]), '<memory-only-reviewed-ast>', 'exec'), scope)
    return scope


def identity(pair):
    role, database = pair['role_oid'], pair['database_oid']
    return {'role': {'oid': role, 'tag': OP.TAG, 'login': True, 'super': False, 'createdb': False,
        'createrole': False, 'replication': False, 'bypass': False, 'inherit': False, 'limit': 12,
        'config': None, 'valid_until': None, 'scram': True}, 'database': {
        'oid': database, 'owner_oid': role, 'tag': OP.TAG, 'allow': True, 'template': False, 'limit': -1,
        'encoding': 'UTF8', 'acl': [{'grantor': role, 'grantee': role, 'privilege_type': privilege,
            'is_grantable': False} for privilege in ('CONNECT', 'CREATE', 'TEMPORARY')]}}


def owned_pair(index=0):
    pair = dict(OP.PAIRS[index], phase='owned')
    pair.update(database_fingerprint=identity(pair)['database'], public=OP.public_expected(pair['name']), casts=[])
    return pair


def receipt_fixture():
    return {'marker': 'goby-client-backup-pair-m3e-v1', 'run_id': OP.RUN, 'tag': OP.TAG, 'unit': OP.UNIT,
        'source': str(OP.SOURCE), 'output': str(OP.OUTPUT), 'source_manifest_sha256': OP.MANIFEST_SHA,
        'schema': 28, 'catalog_sha256': OP.CATALOG_SHA, 'mode': 'targeted', 'phase': 'retained',
        'cleanup_complete': False, 'cluster': copy.deepcopy(OP.CLUSTER), 'cluster_owner_sha256': OP.OWNER_SHA,
        'output_identity': copy.deepcopy(OP.OUTPUT_IDENTITY), 'pairs': [owned_pair(0), owned_pair(1)]}


def snapshot_fixture(pair):
    return {'identity': identity(pair), 'public': OP.public_expected(pair['name']), 'catalog_objects': [],
        'public_identities': [], 'post_init_objects': [], 'local_role_dependencies': [],
        'sessions': {'activity': [], 'prepared': 0, 'slots': 0}, 'casts_sha256': OP.CASTS_SHA,
        'table_row_counts': {}, 'sequence_states': {}}


def terminal_fixture():
    terminal = {'LoadState': 'loaded', 'ActiveState': 'failed', 'SubState': 'failed', 'MainPID': '0',
        'ExecMainCode': '1', 'ExecMainStatus': '1', 'Result': 'exit-code', 'ControlGroup': ''}
    return {unit: dict(terminal, Id=unit, **pins) for unit, pins in OP.UNIT_PINS.items()}, {
        unit: [] for unit in OP.UNIT_PINS}


class DisposalGuards(unittest.TestCase):
    def test_exact_run_source_runner_and_pair_pins(self):
        self.assertEqual(OP.RUN, '20260912_040039_8ec465c99d0b')
        self.assertEqual(str(OP.SOURCE), '/opt/goby-test/exec-work-m3e/source-attempt-47')
        self.assertEqual(str(OP.OUTPUT), '/opt/goby-test/exec-work-m3e/client-backup-run-20260912_040039_8ec465c99d0b')
        self.assertEqual(str(OP.EXECUTION), '/opt/goby-test/exec-work-m3e/storage-binding-schema28-execution-01')
        self.assertEqual(OP.MANIFEST_SHA, 'a0bb8413680da27cee7f2159bf34eda1405ddff4ae57f2564747717f03bf8831')
        self.assertEqual(OP.CATALOG_SHA, '8e7569c8fe2073ee2ed4c51147f9abc21061d1aac9554843101b826fa5a1cc2b')
        self.assertEqual(OP.RUN_EXPRESSION,
            '^(Test(RootBinding|StorageRootBindingMigration|PostgreSQL|.*Migration|CatalogAudit|HTTPRootBinding|'
            'HTTPLibraryRefreshDurablyQueuesEveryLibraryBeyondScannerCapacity|ManagerRetriesCapacityAndIndependentScansBeyondTerminalPage))')
        self.assertEqual(OP.PACKAGES, ('./internal/database', './internal/backuppg', './internal/activity',
            './internal/library', './internal/server', './internal/tasks'))
        self.assertEqual(OP.RUNNER, EXPECTED_RUNNER)
        self.assertEqual(OP.RUNNER_SHA, EXPECTED_RUNNER_SHA)
        self.assertEqual(str(OP.GUARDS), '/opt/goby-test/exec-work-m3e/schema28-runner-guard-01')
        self.assertEqual(OP.GUARD_SHA, {
            'stdout': '9ecb9d07c61c61b7510c8ed16e895ad2c146d0244920914906663d1d40703ea9',
            'stderr': 'adcceb7f042242fdd8375aae0fbd1334ce7179489e307babca2d44babde86aac',
            'report.json': 'ba27c2c539e5f18020db811c31d82e56b1e528e456d0a66e59baf0f1f9c8f6d3'})
        self.assertIsInstance(OP.OUTPUT, MemoryPath)
        self.assertEqual(str(OP.OUTPUT / 'disposal-report.json'), str(OP.OUTPUT) + '/disposal-report.json')
        self.assertEqual(str(MemoryPath('/unused').with_segments(OP.OUTPUT, 'disposal-report.json')),
                         str(OP.OUTPUT) + '/disposal-report.json')
        self.assertEqual(str(MemoryPath('/ignored', '/fixed', 'child', '..', 'report.json')),
                         '/fixed/child/../report.json')
        with self.assertRaises(TypeError):
            MemoryPath(object())
        self.assertEqual(OP.PAIRS, [
            {'name': 'goby_backup_m3e_source', 'role_oid': 15921575, 'database_oid': 15921576},
            {'name': 'goby_backup_m3e_target', 'role_oid': 15921577, 'database_oid': 15921578}])
        self.assertEqual(OP.PUBLIC_OIDS, {'goby_backup_m3e_source': 2200, 'goby_backup_m3e_target': 2200})
        self.assertEqual(OP.IDENTITY_SHA, {
            'goby_backup_m3e_source': '58f3d8fe8a3150f952f71078c5ac6e15ac6bbc4734d391bf8d81c5ec45b4f521',
            'goby_backup_m3e_target': 'a2709172881d93de61c140efc1bbb7f441c494624e89619122e94f8af262bca8'})
        self.assertEqual(OP.TERMINAL_SHA, '8a76222c25bf9c6dfecbd34d070df25b30edc44cb790fc2239ebca291921bfb9')
        self.assertEqual(OP.TREE_SHA, {
            str(OP.SOURCE): '716142a31dcbc3d1ae3520432ac2e6fe06c1c3b22a037e5605f14b0a8f602b4f',
            str(OP.OUTPUT): '7c950e28af399c1962de4b83e08e60fc94eecc5f5d14fca2f9e5794a221e909d',
            str(OP.EXECUTION): '10402f5a278b63afd0d5839c4a3fff046c7fe6dd99da74386a9875f8f8e9e915'})

    def test_pair_identity_and_phase_reject_prefixes_types_and_unknown_oids(self):
        for index in (0, 1):
            pair = owned_pair(index)
            self.assertEqual(OP.exact_pair(SimpleNamespace(require=require), pair), OP.PAIRS[index])
            for key, value in [('name', pair['name'] + '_other'), ('name', "x'; DROP DATABASE postgres;--"),
                    ('role_oid', 1), ('role_oid', True), ('role_oid', str(pair['role_oid'])),
                    ('database_oid', 1), ('database_oid', True), ('database_oid', str(pair['database_oid'])),
                    ('phase', 'closed'), ('phase', 'removed'), ('phase', None)]:
                changed = dict(pair, **{key: value})
                with self.subTest(index=index, key=key, value=value), self.assertRaises(ValueError):
                    OP.exact_pair(SimpleNamespace(require=require), changed)
            self.assertEqual(OP.exact_pair(SimpleNamespace(require=require), dict(pair, phase='removed'), 'removed'), OP.PAIRS[index])

    def test_retained_receipt_rejects_every_changed_or_missing_field(self):
        receipt = receipt_fixture()
        m = SimpleNamespace(require=require)
        OP.validate_receipt(m, receipt)
        for key in receipt:
            changed = copy.deepcopy(receipt)
            del changed[key]
            with self.subTest(missing=key), self.assertRaises((ValueError, KeyError)):
                OP.validate_receipt(m, changed)
        mutations = [('run_id', '20260912_000000_000000000000'), ('source', str(OP.SOURCE) + '-other'),
            ('mode', 'full'), ('schema', 27), ('schema', True), ('phase', 'finished'),
            ('cleanup_complete', True), ('cleanup_complete', 0), ('catalog_sha256', '0' * 64),
            ('source_manifest_sha256', '0' * 64), ('output_identity', {'device': 2049, 'inode': 1}),
            ('cluster', {}), ('cluster_owner_sha256', '0' * 64), ('unit', OP.OUTER_UNIT),
            ('tag', OP.TAG + '-other'), ('marker', 'other'), ('output', str(OP.OUTPUT) + '-other'),
            ('pairs', list(reversed(receipt['pairs']))), ('pairs', receipt['pairs'][:1]), ('unknown', True)]
        for key, value in mutations:
            changed = copy.deepcopy(receipt)
            changed[key] = value
            with self.subTest(field=key, value=value), self.assertRaises(ValueError):
                OP.validate_receipt(m, changed)
        for key, value in [('role_oid', 1), ('database_oid', 1), ('phase', 'closed')]:
            changed = copy.deepcopy(receipt)
            changed['pairs'][0][key] = value
            with self.subTest(pair_field=key), self.assertRaises(ValueError):
                OP.validate_receipt(m, changed)

    def test_failed_report_proves_the_exact_failure_and_restored_hba(self):
        report = {'marker': 'goby-client-backup-pair-m3e-v1', 'status': 'failed', 'run_id': OP.RUN,
            'source': str(OP.SOURCE), 'source_manifest_sha256': OP.MANIFEST_SHA, 'schema': 28, 'mode': 'targeted',
            'catalog_sha256': OP.CATALOG_SHA, 'unit': OP.UNIT, 'unit_exit': 1, 'unit_process': copy.deepcopy(OP.INNER_PROCESS),
            'cluster': copy.deepcopy(OP.CLUSTER), 'pair_evidence_retained': True, 'hba_before_sha256': OP.HBA_SHA,
            'hba_after_sha256': OP.HBA_SHA, 'error': 'The verified unit failed or was never observed running.',
            'cleanup': {'hba_restored_exactly': True, 'receipt_saved': True, 'unit_terminal': True}, 'tools': {}}
        m = SimpleNamespace(require=require)
        OP.validate_failed_report(m, report)
        for key in report:
            changed = copy.deepcopy(report)
            del changed[key]
            with self.subTest(missing=key), self.assertRaises(ValueError):
                OP.validate_failed_report(m, changed)
        for key in set(report) - {'tools'}:
            changed = copy.deepcopy(report)
            changed[key] = None
            with self.subTest(changed=key), self.assertRaises(ValueError):
                OP.validate_failed_report(m, changed)
        for key, value in [('status', 'passed'), ('unit_exit', True), ('pair_evidence_retained', 1),
                ('cleanup', {'hba_restored_exactly': False, 'receipt_saved': True, 'unit_terminal': True}), ('unexpected', 1)]:
            with self.subTest(field=key), self.assertRaises(ValueError):
                OP.validate_failed_report(m, dict(report, **{key: value}))

    def test_both_exact_terminal_invocations_cgroups_and_process_lifetimes(self):
        states, cgroups = terminal_fixture()
        m = SimpleNamespace(require=require)
        OP.validate_terminal(m, states, cgroups, None)
        OP.validate_terminal(m, states, cgroups, dict(OP.INNER_PROCESS, start_ticks=OP.INNER_PROCESS['start_ticks'] + 1))
        for unit in states:
            for field in states[unit]:
                changed = copy.deepcopy(states)
                changed[unit][field] = 'unexpected'
                with self.subTest(unit=unit, field=field), self.assertRaises(ValueError):
                    OP.validate_terminal(m, changed, cgroups, None)
            changed_groups = copy.deepcopy(cgroups)
            changed_groups[unit] = ['1237620']
            with self.subTest(cgroup=unit), self.assertRaises(ValueError):
                OP.validate_terminal(m, states, changed_groups, None)
            with self.subTest(missing_unit=unit), self.assertRaises(ValueError):
                OP.validate_terminal(m, {key: value for key, value in states.items() if key != unit}, cgroups, None)
        for process in [OP.INNER_PROCESS, {}, [], 'absent', dict(OP.INNER_PROCESS, pid=1),
                dict(OP.INNER_PROCESS, start_ticks=True), dict(OP.INNER_PROCESS, start_ticks=0),
                dict(OP.INNER_PROCESS, boot_id='different'), dict(OP.INNER_PROCESS, extra=True)]:
            with self.subTest(process=process), self.assertRaises(ValueError):
                OP.validate_terminal(m, states, cgroups, process)

    def test_empty_snapshot_rejects_all_nonempty_or_changed_evidence(self):
        m = SimpleNamespace(require=require)
        for index in (0, 1):
            pair = owned_pair(index)
            snapshot = snapshot_fixture(pair)
            self.assertEqual(OP.fingerprint(snapshot['identity']), OP.IDENTITY_SHA[pair['name']])
            OP.validate_empty_snapshot(m, pair, snapshot)
            for key in snapshot:
                changed = copy.deepcopy(snapshot)
                del changed[key]
                with self.subTest(index=index, missing=key), self.assertRaises(ValueError):
                    OP.validate_empty_snapshot(m, pair, changed)
            mutations = [(key, [{'unexpected': 1}]) for key in ('catalog_objects', 'public_identities',
                'post_init_objects', 'local_role_dependencies')]
            mutations += [('table_row_counts', {'users': 0}), ('sequence_states', {'seq': 1}),
                ('casts_sha256', '0' * 64), ('identity', {}), ('unexpected', []),
                ('sessions', {'activity': [{'pid': 1}], 'prepared': 0, 'slots': 0}),
                ('sessions', {'activity': [], 'prepared': 1, 'slots': 0}),
                ('sessions', {'activity': [], 'prepared': 0, 'slots': 1})]
            for key, value in mutations:
                changed = copy.deepcopy(snapshot)
                changed[key] = value
                with self.subTest(index=index, field=key, value=value), self.assertRaises(ValueError):
                    OP.validate_empty_snapshot(m, pair, changed)
            for part in ('role', 'database'):
                for key in snapshot['identity'][part]:
                    changed = copy.deepcopy(snapshot)
                    changed['identity'][part][key] = 'changed'
                    with self.subTest(index=index, identity=part, field=key), self.assertRaises(ValueError):
                        OP.validate_empty_snapshot(m, pair, changed)
            for key in snapshot['public']:
                changed = copy.deepcopy(snapshot)
                changed['public'][key] = 'changed'
                with self.subTest(index=index, public=key), self.assertRaises(ValueError):
                    OP.validate_empty_snapshot(m, pair, changed)

    def test_observation_sql_uses_only_exact_fixed_names_and_oids(self):
        m = SimpleNamespace(require=require)
        for index in (0, 1):
            pair = owned_pair(index)
            name, oid = pair['name'], pair['database_oid']
            sessions = OP.sessions_sql(m, pair)
            self.assertIn('FROM pg_stat_activity WHERE datid=' + str(oid) + ')', sessions)
            self.assertEqual(sessions.count("WHERE database='" + name + "'"), 2)
            public = OP.public_identities_sql(m, pair)
            for column in ('relnamespace', 'pronamespace', 'typnamespace', 'connamespace'):
                self.assertIn(' WHERE ' + column + '=' + str(OP.PUBLIC_OIDS[name]), public)
            for sql in (sessions, public, OP.post_init_objects_sql()):
                self.assertTrue(sql.startswith('SELECT '))
                self.assertNotIn('LIKE', sql)
                self.assertNotIn('DROP ', sql)
                self.assertNotIn('pg_terminate_backend', sql)
            for changed in [dict(pair, name=name + '_other'), dict(pair, database_oid=1), dict(pair, role_oid=1),
                    dict(pair, phase='closed'), dict(pair, name="x'; SELECT 1;--")]:
                for function in (OP.sessions_sql, OP.public_identities_sql):
                    with self.subTest(function=function.__name__, pair=changed), self.assertRaises(ValueError):
                        function(m, changed)
        sql = OP.post_init_objects_sql()
        self.assertEqual(sql.count(' WHERE oid>=16384'), len(OP.OBJECT_CATALOGS))
        for catalog in OP.OBJECT_CATALOGS:
            self.assertIn(' FROM ' + catalog + ' WHERE oid>=16384', sql)

    def test_preservation_rejects_tree_differences_and_untrusted_original_metadata(self):
        # These small synthetic trees exercise the real hash and ownership gate.
        # The three production digest pins are asserted separately above.
        trees = {root: {'.': {'uid': 0, 'gid': 0, 'mode': 0o700, 'inode': index + 1},
            'evidence': {'uid': 0, 'gid': 0, 'mode': 0o600, 'sha256': 'a' * 64}}
            for index, root in enumerate(OP.TREE_SHA)}
        scope = OP.validate_preservation.__globals__
        original = scope['TREE_SHA']
        try:
            scope['TREE_SHA'] = {root: OP.fingerprint(entries) for root, entries in trees.items()}
            OP.validate_preservation(SimpleNamespace(require=require), trees, copy.deepcopy(trees))
            for root in trees:
                for key, value in [('uid', 1), ('gid', 1), ('mode', 0o622), ('sha256', 'b' * 64), ('extra', True)]:
                    changed = copy.deepcopy(trees)
                    changed[root]['evidence'][key] = value
                    with self.subTest(root=root, changed=key), self.assertRaises(ValueError):
                        OP.validate_preservation(SimpleNamespace(require=require), trees, changed)
                    with self.subTest(root=root, unpinned=key), self.assertRaises(ValueError):
                        OP.validate_preservation(SimpleNamespace(require=require), changed, changed)
                changed = copy.deepcopy(trees)
                del changed[root]['evidence']
                with self.subTest(root=root, removed=True), self.assertRaises(ValueError):
                    OP.validate_preservation(SimpleNamespace(require=require), trees, changed)
                for key, value in [('uid', 1), ('gid', 1), ('mode', 0o622)]:
                    changed = copy.deepcopy(trees)
                    changed[root]['evidence'][key] = value
                    scope['TREE_SHA'] = {path: OP.fingerprint(entries) for path, entries in changed.items()}
                    with self.subTest(root=root, unsafe=key), self.assertRaises(ValueError):
                        OP.validate_preservation(SimpleNamespace(require=require), changed, changed)
                    scope['TREE_SHA'] = {path: OP.fingerprint(entries) for path, entries in trees.items()}
            with self.assertRaises(ValueError):
                OP.validate_preservation(SimpleNamespace(require=require), {}, {})
        finally:
            scope['TREE_SHA'] = original

    def removal_model(self, index=0, before_sessions='0', raced_sessions='0', nonempty=False):
        pair = owned_pair(index)
        name, role, database = pair['name'], pair['role_oid'], pair['database_oid']
        state = identity(pair)
        events, sql_calls = [], []
        source_identity, source_files = {'memory': 'source'}, {'memory': 'manifest'}
        def rows(requested):
            require(requested == name, 'An unknown database name reached the memory model.')
            return copy.deepcopy(state)
        def validate(candidate, current, tag):
            OP.exact_pair(SimpleNamespace(require=require), candidate)
            VALIDATE_ROWS(candidate, current, tag)
        def isolated(candidate):
            OP.exact_pair(SimpleNamespace(require=require), candidate)
        initial = (f'SELECT (SELECT count(*) FROM pg_stat_activity WHERE datid={database})+'
            f"(SELECT count(*) FROM pg_prepared_xacts WHERE database='{name}')+"
            f"(SELECT count(*) FROM pg_replication_slots WHERE database='{name}');")
        dependency = (f"SELECT (SELECT count(*) FROM pg_shdepend WHERE refclassid='pg_authid'::regclass AND refobjid={role})+"
            f'(SELECT count(*) FROM pg_auth_members WHERE member={role} OR roleid={role} OR grantor={role});')
        def pg(sql):
            sql_calls.append(sql)
            if sql == initial:
                return before_sessions
            if sql == f'ALTER DATABASE {name} ALLOW_CONNECTIONS false;':
                require(pair['phase'] == 'owned' and state['database']['allow'] is True, 'Close order changed.')
                events.append(('close', name))
                state['database']['allow'] = False
                return ''
            if sql == f'SELECT count(*) FROM pg_stat_activity WHERE datid={database};':
                return raced_sessions
            if sql == f'DROP DATABASE {name};':
                require(pair['phase'] == 'closed' and state['database']['allow'] is False, 'Database drop order changed.')
                events.append(('drop_database', name))
                state['database'] = None
                return ''
            if sql == dependency:
                return '0'
            if sql == f'DROP ROLE {name};':
                require(pair['phase'] == 'database_removed' and state['database'] is None, 'Role drop order changed.')
                events.append(('drop_role', name))
                state['role'] = None
                return ''
            raise EffectBlocked('An unmodeled PostgreSQL command was requested.')
        def create_private(path, raw):
            require(path == OP.OUTPUT / (name + '-objects.json') and raw == b'[]', 'Object evidence escaped its memory scope.')
            events.append(('evidence', name))
        def inspect(candidate):
            snapshot = snapshot_fixture(candidate)
            if nonempty:
                snapshot['catalog_objects'] = [{'unexpected': True}]
            OP.validate_empty_snapshot(SimpleNamespace(require=require), candidate, snapshot)
            return snapshot['catalog_objects']
        def verify_source(source, manifest, schema, catalog_bootstrap):
            require((source, manifest, schema, catalog_bootstrap) == (OP.SOURCE, OP.MANIFEST_SHA, 28, False), 'Source scope changed.')
            return source_identity, source_files
        runner = SimpleNamespace(hba_before=b'memory-only-hba', tag=OP.TAG, output=OP.OUTPUT,
            args=SimpleNamespace(source=OP.SOURCE, manifest_sha256=OP.MANIFEST_SHA, schema=28, mode='targeted'),
            source_identity=source_identity, source_files=source_files,
            check_cluster=lambda hba: require(hba == b'memory-only-hba', 'HBA changed.'),
            public_identity=lambda requested: OP.public_expected(requested), cast_inventory=lambda requested: [],
            inspect_objects=inspect, save=lambda: events.append(('save', pair['phase'])))
        scope = {'__builtins__': SAFE_BUILTINS, 'require': require, 'json': SimpleNamespace(dumps=json.dumps),
            'validate_pair_rows': validate, 'pair_rows': rows, 'require_role_isolated': isolated, 'pg': pg,
            'verify_source': verify_source, 'read_source_catalog': lambda *args: {'objects': [{'trusted': True}]},
            'create_private': create_private, 'NAMES': tuple(item['name'] for item in OP.PAIRS)}
        function = FunctionType(REMOVE_PAIR.__code__, scope, 'remove_pair')
        return function, runner, pair, state, events, sql_calls

    def test_frozen_remove_pair_closes_then_drops_database_then_role(self):
        original = receipt_fixture()
        retained_bytes = json.dumps(original, sort_keys=True).encode()
        for index in (0, 1):
            function, runner, pair, state, events, sql_calls = self.removal_model(index)
            self.assertEqual(pair['phase'], 'owned')
            function(runner, pair)
            self.assertEqual(events, [('evidence', pair['name']), ('close', pair['name']), ('save', 'closed'),
                ('drop_database', pair['name']), ('save', 'database_removed'), ('drop_role', pair['name']), ('save', 'removed')])
            self.assertEqual(state, {'role': None, 'database': None})
            self.assertEqual(pair['phase'], 'removed')
            self.assertFalse(any('FORCE' in sql or 'pg_terminate_backend' in sql or 'CASCADE' in sql for sql in sql_calls))
        self.assertEqual(json.dumps(original, sort_keys=True).encode(), retained_bytes)
        self.assertEqual(original['phase'], 'retained')
        self.assertIs(original['cleanup_complete'], False)
        self.assertTrue(all(pair['phase'] == 'owned' for pair in original['pairs']))

    def test_racing_session_retains_closed_pair_without_any_drop(self):
        for index in (0, 1):
            function, runner, pair, state, events, sql_calls = self.removal_model(index, raced_sessions='1')
            with self.assertRaises(ValueError):
                function(runner, pair)
            self.assertEqual(events, [('evidence', pair['name']), ('close', pair['name']), ('save', 'closed')])
            self.assertEqual(pair['phase'], 'closed')
            self.assertIs(state['database']['allow'], False)
            self.assertIsNotNone(state['role'])
            self.assertFalse(any(sql.startswith('DROP ') for sql in sql_calls))
            with self.assertRaises(ValueError):
                function(runner, pair)
            self.assertFalse(any(sql.startswith('DROP ') for sql in sql_calls))

    def test_live_work_nonempty_or_unknown_pair_never_reaches_close_or_drop(self):
        for defect in ('sessions', 'objects', 'name', 'role_oid', 'database_oid', 'phase'):
            function, runner, pair, state, events, sql_calls = self.removal_model(
                before_sessions='1' if defect == 'sessions' else '0', nonempty=defect == 'objects')
            if defect in ('name', 'role_oid', 'database_oid', 'phase'):
                pair[defect] = {'name': 'goby_backup_m3e_source_unowned', 'role_oid': 1,
                    'database_oid': 1, 'phase': 'closed'}[defect]
            with self.subTest(defect=defect), self.assertRaises(ValueError):
                function(runner, pair)
            self.assertEqual(events, [])
            self.assertFalse(any(sql.startswith(('ALTER ', 'DROP ')) for sql in sql_calls))

    def test_frozen_disposal_validator_requires_exact_retained_receipt_attestation(self):
        receipt = receipt_fixture()
        original = json.dumps(receipt, sort_keys=True).encode()
        report_path = OP.OUTPUT / 'disposal-report.json'
        digest = hashlib.sha256(b'memory-only-report').hexdigest()
        report = {'marker': 'goby-client-backup-disposal-m3e-v1'}
        actual = ATTESTATION(report, report_path, digest)
        self.assertEqual(actual, {'marker': report['marker'], 'status': 'disposed', 'run_id': OP.RUN, 'tag': OP.TAG,
            'source_receipt_sha256': OP.RECEIPT_SHA, 'pairs': OP.PAIRS,
            'system_identifier': OP.CLUSTER['system_identifier'], 'hba_sha256': OP.HBA_SHA,
            'report_path': str(report_path), 'report_sha256': digest})
        # The synthetic receipt is checked with its actual bytes and actual hash.
        attestation = dict(actual, source_receipt_sha256=hashlib.sha256(original).hexdigest())
        VALIDATE_DISPOSAL(receipt, original, attestation, OP.HBA_SHA)
        for key in attestation:
            changed = copy.deepcopy(attestation)
            changed[key] = [] if key == 'pairs' else 'changed'
            with self.subTest(field=key), self.assertRaises(ValueError):
                VALIDATE_DISPOSAL(receipt, original, changed, OP.HBA_SHA)
        for changed in [dict(OP.PAIRS[0], role_oid=1), dict(OP.PAIRS[0], database_oid=1), dict(OP.PAIRS[0], name='other')]:
            with self.subTest(pair=changed), self.assertRaises(ValueError):
                VALIDATE_DISPOSAL(receipt, original, dict(attestation, pairs=[changed, OP.PAIRS[1]]), OP.HBA_SHA)
        with self.assertRaises(ValueError):
            VALIDATE_DISPOSAL(receipt, original + b' ', attestation, OP.HBA_SHA)
        with self.assertRaises(ValueError):
            VALIDATE_DISPOSAL(receipt, original, attestation, '0' * 64)
        self.assertEqual(json.dumps(receipt, sort_keys=True).encode(), original)

    def test_runtime_ast_preserves_original_receipt_and_commits_attestation_last(self):
        calls = [node for node in memory_walk(TREE) if isinstance(node, ast.Call)]
        attributes = [node.func.attr for node in calls if isinstance(node.func, ast.Attribute)]
        self.assertEqual(attributes.count('remove_pair'), 1)
        self.assertFalse({'prepare', 'cleanup', 'reload_hba', 'write_environment', 'replace_private', 'run_all'} & set(attributes))
        published = [node for node in calls if isinstance(node.func, ast.Name) and node.func.id == 'publish']
        first_publication = min(node.lineno for node in published)
        requirements = [node for node in calls if isinstance(node.func, ast.Attribute) and
            isinstance(node.func.value, ast.Name) and node.func.value.id == 'm' and
            node.func.attr == 'require' and node.args]
        retry_gates = [node for node in requirements if ast.dump(node.args[0]) == ast.dump(EXPECTED_RETRY_GATE)]
        self.assertEqual(len(retry_gates), 1)
        self.assertLess(retry_gates[0].lineno, first_publication)
        main = next(node for node in TREE.body if isinstance(node, ast.FunctionDef) and node.name == 'main')
        nested = [node for node in memory_walk(main) if isinstance(node, ast.FunctionDef) and node is not main]
        self.assertTrue(main.lineno < retry_gates[0].lineno < main.end_lineno)
        self.assertFalse(any(function.lineno <= retry_gates[0].lineno <= function.end_lineno for function in nested))
        cluster = next(node for node in nested if node.name == 'check_cluster')
        terminal_checks = [node for node in requirements if
            ast.dump(node.args[0]) == ast.dump(EXPECTED_TERMINAL_CHECK)]
        self.assertEqual(len(terminal_checks), 2)
        preflight = [node for node in terminal_checks if main.lineno < node.lineno < main.end_lineno and
            not any(function.lineno <= node.lineno <= function.end_lineno for function in nested)]
        self.assertEqual(len(preflight), 1)
        self.assertLess(preflight[0].lineno, first_publication)
        self.assertEqual(len([node for node in terminal_checks if cluster.lineno < node.lineno < cluster.end_lineno]), 1)
        originals = [node for node in published if any(isinstance(part, ast.Constant) and
            part.value == 'disposal-original-receipt.json' for part in memory_walk(node.args[0]))]
        self.assertEqual(len(originals), 1)
        self.assertEqual(ast.dump(originals[0].args[1]), "Name(id='original', ctx=Load())")
        self.assertEqual([(item.arg, item.value.value) for item in originals[0].keywords], [('raw', True)])
        validation = [node for node in calls if isinstance(node.func, ast.Attribute) and node.func.attr == 'validate_disposal']
        self.assertEqual(len(validation), 1)
        self.assertEqual([node.id for node in validation[0].args], ['receipt', 'original', 'attestation', 'HBA_SHA'])
        commits = [node for node in published if isinstance(node.args[0], ast.Name) and node.args[0].id == 'disposal_path']
        self.assertEqual(len(commits), 1)
        self.assertLess(validation[0].lineno, commits[0].lineno)
        self.assertEqual(commits[0].args[1].id, 'attestation')
        self.assertFalse(any(isinstance(node.func, ast.Name) and node.func.id in ('check_cluster', 'check_preservation')
            and node.lineno > commits[0].lineno for node in calls))
        assignments = [node for node in memory_walk(TREE) if isinstance(node, ast.Assign)]
        saves = [node for node in assignments if any(isinstance(target, ast.Attribute) and
            isinstance(target.value, ast.Name) and target.value.id == 'runner' and target.attr == 'save' for target in node.targets)]
        self.assertEqual(len(saves), 1)
        self.assertEqual(saves[0].value.id, 'save_progress')
        pairs = [node for node in assignments if any(isinstance(target, ast.Attribute) and
            isinstance(target.value, ast.Name) and target.value.id == 'runner' and target.attr == 'pairs' for target in node.targets)]
        self.assertEqual(len(pairs), 1)
        self.assertEqual(pairs[0].value.func.attr, 'deepcopy')
        for node in calls:
            if isinstance(node.func, ast.Attribute) and node.func.attr == 'pg':
                constants = [part.value for part in memory_walk(node.args[0]) if isinstance(part, ast.Constant) and isinstance(part.value, str)]
                if constants:
                    self.assertTrue(''.join(constants).startswith('SELECT '))
        terminal = next(node for node in memory_walk(TREE) if isinstance(node, ast.FunctionDef) and node.name == 'terminal_evidence')
        recursive = [node for node in memory_walk(terminal) if isinstance(node, ast.Call) and
            isinstance(node.func, ast.Attribute) and node.func.attr == 'rglob']
        self.assertEqual(len(recursive), 1)
        self.assertEqual(recursive[0].args[0].value, '*')
        self.assertTrue(any(isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute) and
            node.func.attr == 'extend' for node in memory_walk(terminal)))

    def test_effect_fence_blocks_files_processes_network_import_and_dynamic_code(self):
        attempts = [('read', lambda: Path('/memory-only').read_bytes()),
            ('write', lambda: Path('/memory-only').write_bytes(b'blocked')),
            ('stat', lambda: os.stat('/memory-only')), ('open', lambda: builtins.open('/memory-only')),
            ('subprocess', lambda: subprocess.run(['/bin/false'])), ('network', lambda: socket.socket()),
            ('import', lambda: builtins.__import__('json')), ('pg_import', lambda: importlib.import_module('psycopg')),
            ('exec', lambda: builtins.exec('pass')),
            ('audit_open', lambda: sys.audit('open', '/memory-only', 'r', 0)),
            ('audit_process', lambda: sys.audit('subprocess.Popen', '/bin/false', [], None, None)),
            ('audit_network', lambda: sys.audit('socket.connect', None, ('127.0.0.1', 15432)))]
        before = len(BLOCKED_EFFECTS)
        for name, action in attempts:
            with self.subTest(effect=name), self.assertRaises(EffectBlocked):
                action()
        self.assertEqual(len(BLOCKED_EFFECTS) - before, len(attempts))


class MemoryResult(unittest.TestResult):
    def _exc_info_to_string(self, error, test):
        # Read only live traceback metadata. traceback/linecache formatting may
        # import helpers or reopen source files after the strict effect fence.
        frames = []
        current = error[2]
        while current is not None:
            code = current.tb_frame.f_code
            frames.append({'function': code.co_name, 'file': code.co_filename, 'line': current.tb_lineno})
            current = current.tb_next
        return json.dumps({'test': test.id(), 'error_type': error[0].__name__,
            'message': str(error[1]), 'frames': frames}, sort_keys=True)


def main():
    global OP, TREE, REMOVE_PAIR, VALIDATE_ROWS, VALIDATE_DISPOSAL, ATTESTATION
    if sys.platform != 'linux' or os.geteuid() != 0 or not os.environ.get('SSH_CONNECTION') or len(sys.argv) != 2:
        raise SystemExit('Run only through authorized root SSH with the source47 operator path.')
    path = Path(sys.argv[1])
    if path.name != 'dispose-source47-binding-failed-pair.py':
        raise SystemExit('Only the exact source47 disposal operator is supported.')
    raw = path.read_bytes()
    TREE = ast.parse(raw, filename='<reviewed-source47-operator>')
    values = {}
    for node in TREE.body:
        if isinstance(node, ast.Assign) and len(node.targets) == 1 and isinstance(node.targets[0], ast.Name):
            name = node.targets[0].id
            require(name.isupper() and name not in values, 'An operator constant declaration changed.')
            values[name] = constant_value(node.value, values)
    require(values['RUNNER'] == EXPECTED_RUNNER and values['RUNNER_SHA'] == EXPECTED_RUNNER_SHA,
            'The runner path or SHA differs from the independently pinned executable.')
    runner_raw = Path(str(EXPECTED_RUNNER)).read_bytes()
    require(hashlib.sha256(runner_raw).hexdigest() == EXPECTED_RUNNER_SHA, 'The frozen runner source changed.')
    runner_tree = ast.parse(runner_raw, filename='<sha-pinned-runner>')
    selected = {'fingerprint', 'exact_pair', 'validate_receipt', 'validate_failed_report', 'validate_terminal',
        'public_expected', 'validate_empty_snapshot', 'validate_preservation', 'post_init_objects_sql',
        'sessions_sql', 'public_identities_sql'}
    functions = [node for node in TREE.body if isinstance(node, ast.FunctionDef) and node.name in selected]
    require(len(functions) == len(selected), 'An exact pure operator function is missing or duplicated.')
    pure_modules = {'json': SimpleNamespace(dumps=json.dumps), 'hashlib': SimpleNamespace(sha256=hashlib.sha256)}
    scope = compile_functions(functions, {**values, **pure_modules})
    OP = SimpleNamespace(**scope)
    runner_functions = [node for node in runner_tree.body if isinstance(node, ast.FunctionDef) and
        node.name in {'validate_pair_rows', 'validate_disposal'}]
    runner_class = [node for node in runner_tree.body if isinstance(node, ast.ClassDef) and node.name == 'Runner']
    require(len(runner_class) == 1 and len(runner_functions) == 2, 'The pinned runner validators changed.')
    remove = [node for node in runner_class[0].body if isinstance(node, ast.FunctionDef) and node.name == 'remove_pair']
    require(len(remove) == 1, 'The pinned removal method changed.')
    runner_scope = compile_functions(runner_functions + remove, {'require': require, 'WORK': OP.WORK,
        'Path': MemoryPath, 'NAMES': tuple(pair['name'] for pair in OP.PAIRS),
        're': SimpleNamespace(fullmatch=re.fullmatch), 'sha': lambda value: hashlib.sha256(value).hexdigest()})
    REMOVE_PAIR, VALIDATE_ROWS, VALIDATE_DISPOSAL = (runner_scope[name] for name in
        ('remove_pair', 'validate_pair_rows', 'validate_disposal'))
    templates = [node.value for node in memory_walk(TREE) if isinstance(node, ast.Assign) and
        any(isinstance(target, ast.Name) and target.id == 'attestation' for target in node.targets)]
    require(len(templates) == 1 and isinstance(templates[0], ast.Dict), 'The attestation template changed.')
    template = ast.parse('def attestation_template(report, report_path, digest):\n    return None\n').body[0]
    template.body[0].value = copy.deepcopy(templates[0])
    ast.fix_missing_locations(template)
    ATTESTATION = compile_functions([template], {**values})['attestation_template']
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(DisposalGuards)
    result = MemoryResult()
    operator_sha256 = hashlib.sha256(raw).hexdigest()
    install_effect_fence()
    suite.run(result)
    print(json.dumps({'suite': 'source47-disposal-guards', 'status': 'passed' if result.wasSuccessful() else 'failed',
        'tests': result.testsRun, 'failures': len(result.failures), 'errors': len(result.errors), 'skips': len(result.skipped),
        'operator_sha256': operator_sha256, 'runner_sha256': EXPECTED_RUNNER_SHA,
        'fixtures': 'two-source-reads-then-audit-fenced-memory-only', 'initial_source_reads': 2,
        'database_commands': 0, 'filesystem_mutations': 0, 'fence_self_test_blocks': len(BLOCKED_EFFECTS),
        'blocked_effect_details': BLOCKED_EFFECT_DETAILS,
        'summaries': [detail for _, detail in [*result.failures, *result.errors]]}))
    return 0 if result.wasSuccessful() else 1


if __name__ == '__main__':
    raise SystemExit(main())
