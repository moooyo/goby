#!/usr/bin/env python3
"""Guard one source54/schema28 actual-mount failure with no live effects.

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
import json.decoder
import json.scanner
import os
from pathlib import Path, PurePosixPath
import re
import socket
import subprocess
import sys
from types import FunctionType, SimpleNamespace
import unittest

sys.dont_write_bytecode = True


def make_memory_json_decoder():
    """Build private Python callbacks before the import/effect fence closes."""
    decoder = json.JSONDecoder()
    decoder.parse_string = json.decoder.py_scanstring
    # JSONObject normally resolves the module's C scanstring for object keys.
    # Give only this decoder a private callback namespace with the Python
    # implementation, leaving every global stdlib parser and import fence intact.
    object_globals = dict(json.decoder.JSONObject.__globals__)
    object_globals['scanstring'] = json.decoder.py_scanstring
    decoder.parse_object = FunctionType(json.decoder.JSONObject.__code__, object_globals,
        'memory_json_object', json.decoder.JSONObject.__defaults__, json.decoder.JSONObject.__closure__)
    decoder.scan_once = json.scanner.py_make_scanner(decoder)
    return decoder


MEMORY_JSON_DECODER = make_memory_json_decoder()


def memory_json_decode(raw):
    """Decode the known UTF-8 memory log format without the C error scanner."""
    if isinstance(raw, bytes):
        raw = raw.decode('utf-8')
    return MEMORY_JSON_DECODER.decode(raw)


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


EXPECTED_RUNNER = PurePosixPath('/opt/goby-test/exec-work-m3e/source-attempt-54/scripts/test-env/run-client-backup-tests.py')
EXPECTED_RUNNER_SHA = '4c76cfa22fd1915a270756d82e313ce6e7bbf3a5bcc857339fd6d254f5c894a2'
OP = None
TREE = None
REMOVE_PAIR = None
VALIDATE_ROWS = None
VALIDATE_DISPOSAL = None
ATTESTATION = None
RUNNER_CONSTRUCTOR = None
BLOCKED_EFFECTS = []
BLOCKED_EFFECT_DETAILS = []
SAFE_BUILTINS = {name: getattr(builtins, name) for name in
    ('all', 'any', 'bool', 'bytes', 'dict', 'int', 'isinstance', 'len', 'list', 'set', 'sorted', 'str', 'tuple', 'type')}
# Parse only fixed guard expressions before installing the effect fence.
EXPECTED_RETRY_GATE = ast.parse(
    "not m.present(disposal_path) and not any(p.name.startswith('disposal-') for p in OUTPUT.iterdir()) and "
    "not any(m.present(OUTPUT / (p['name'] + '-objects.json')) for p in PAIRS)", mode='eval').body
EXPECTED_TERMINAL_CHECK = ast.parse(
    "m.sha(m.private_read(EXECUTION / 'terminal.json')) == TERMINAL_SHA", mode='eval').body
EXPECTED_BINARY_PATH = ast.parse('binary', mode='eval').body
EXPECTED_PINNED_PATH = ast.parse('OUTPUT / name', mode='eval').body
EXPECTED_PINNED_LOG_LIMIT = ast.parse("LOG_BYTES if name == 'go.log' else 8 << 20", mode='eval').body
EXPECTED_TREE_LIMIT = ast.parse('64 << 20', mode='eval').body
EXPECTED_ARTIFACT_RECHECK = ast.parse('observe_mount_artifacts(m) == failure_artifacts', mode='eval').body
EXPECTED_READ_COMMAND = ast.parse("m.command(['/usr/bin/systemctl', 'show', unit, '--property=' + properties])", mode='eval').body
UNSAFE_CALL_TEMPLATES = [ast.parse(source).body[0] for source in (
    'runner.build()', 'runner.build_binary()', 'runner.run_tests()', 'runner.prepare()',
    'runner.run_all()', 'runner.start_unit()', 'runner.execute()', 'runner.stop_unit()', 'm.Runner(args).run_all()',
    "getattr(runner, 'build')()", "m.run(['go', 'test', './...'])",
    "m.command(['go', 'test', './...'])",
    "m.command(['/usr/local/go/bin/go', 'build', './cmd/goby'])",
    "m.command(['/usr/bin/systemctl', 'start', 'unowned.service'])",
    "subprocess.run(['/usr/local/go/bin/go', 'build', './cmd/goby'])",
    "m.run(['/usr/bin/unshare', '--mount', 'helper'])",
    "subprocess.run(['/usr/bin/mount', 'source', 'target'])",
    "subprocess.run(['/usr/bin/umount', 'target'])",
    "subprocess.run(['/usr/sbin/losetup', '--find'])",
    "m.run(['/usr/bin/systemctl', 'restart', 'unowned.service'])")]


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


def validate_runtime_call_boundary(tree, strict_runner=True):
    forbidden = {'prepare', 'cleanup', 'reload_hba', 'write_environment', 'replace_private', 'run_all',
                 'run_tests', 'build', 'build_binary', 'build_product', 'test', 'start_unit', 'run_unit',
                 'execute', 'stop_unit'}
    allowed_runner = {'cast_inventory', 'public_identity', 'original_inspect', 'remove_pair'}
    for node in memory_walk(tree):
        if not isinstance(node, ast.Call):
            continue
        if isinstance(node.func, ast.Attribute):
            require(node.func.attr not in forbidden, 'A disposal call can prepare, test, build, or restart work.')
            if node.func.attr == 'command':
                require(ast.dump(node) == ast.dump(EXPECTED_READ_COMMAND),
                        'The disposal command entry point escaped its exact read-only service observation.')
            require(node.func.attr not in {'run', 'Popen', 'call', 'check_call', 'check_output', 'getoutput', 'getstatusoutput'},
                    'A disposal process entry point can dispatch an unreviewed command or mount helper.')
            if strict_runner and isinstance(node.func.value, ast.Name) and node.func.value.id == 'runner':
                require(node.func.attr in allowed_runner, 'A runner method escaped the disposal-only call set.')
            if node.func.attr in {'command', 'run', 'Popen', 'call', 'check_call', 'check_output'} and node.args and isinstance(node.args[0], (ast.List, ast.Tuple)):
                words = [part.value for part in node.args[0].elts if isinstance(part, ast.Constant) and isinstance(part.value, str)]
                require(not any(word == 'go' or word.endswith('/go') for word in words) and
                        not any(word in ('build', 'test') for word in words), 'A disposal command dispatches a build or test.')
        if isinstance(node.func, ast.Name) and node.func.id == 'getattr' and node.args:
            require(not isinstance(node.args[0], ast.Name) or node.args[0].id != 'runner',
                    'Dynamic runner dispatch cannot establish a disposal-only boundary.')


def validate_artifact_read_limits(tree):
    direct, pinned, historical = [], [], []
    root = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'root_tree')
    artifacts = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'observe_mount_artifacts')
    for node in memory_walk(tree):
        if not (isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute) and
                node.func.attr == 'private_read' and node.args):
            continue
        limits = [value.value for value in node.keywords if value.arg == 'limit']
        path = ast.dump(node.args[0])
        if artifacts.lineno <= node.lineno <= artifacts.end_lineno:
            require(path == ast.dump(EXPECTED_BINARY_PATH) and len(limits) == 1 and
                    ast.dump(limits[0]) == ast.dump(EXPECTED_TREE_LIMIT),
                    'The compiled helper reader lost its exact path or bounded complete-file limit.')
            direct.append(node)
        elif path == ast.dump(EXPECTED_PINNED_PATH):
            require(len(limits) == 1 and ast.dump(limits[0]) == ast.dump(EXPECTED_PINNED_LOG_LIMIT),
                    'A repeated evidence pin no longer requires the empty pre-dispatch Go log.')
            pinned.append(node)
        elif root.lineno <= node.lineno <= root.end_lineno:
            require(len(limits) == 1 and ast.dump(limits[0]) == ast.dump(EXPECTED_TREE_LIMIT),
                    'The historical tree reader cannot witness the complete compiled helper.')
            historical.append(node)
    require(len(direct) == 1 and len(pinned) == 2 and len(historical) == 1,
            'The exact compiled-helper observation and repeated pin locations changed.')
    return direct + pinned + historical


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
    retained_public = dict(OP.public_expected(pair['name']), oid=2200)
    pair.update(database_fingerprint=identity(pair)['database'], public=retained_public, casts=[])
    return pair


def receipt_fixture():
    return {'marker': 'goby-client-backup-pair-m3e-v1', 'run_id': OP.RUN, 'tag': OP.TAG, 'unit': OP.UNIT,
        'source': str(OP.SOURCE), 'output': str(OP.OUTPUT), 'source_manifest_sha256': OP.MANIFEST_SHA,
        'schema': 28, 'catalog_sha256': OP.CATALOG_SHA, 'mode': 'targeted', 'phase': 'retained',
        'cleanup_complete': False, 'cluster': copy.deepcopy(OP.CLUSTER), 'cluster_owner_sha256': OP.OWNER_SHA,
        'output_identity': copy.deepcopy(OP.OUTPUT_IDENTITY), 'bound_scan_mount': copy.deepcopy(OP.MOUNT_ADMISSION),
        'pairs': [owned_pair(0), owned_pair(1)]}


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


def mount_terminal_fixture():
    state = {'ActiveState': 'failed', 'MainPID': '0', 'Result': 'exit-code', 'ExecMainStatus': '1',
             'ControlGroup': '', 'SubState': 'failed'}
    return {'actual_mount_recovery_accepted': False, 'binary': copy.deepcopy(OP.BINARY),
        'cleanup': {'hba_restored_exactly': True, 'receipt_saved': True, 'unit_terminal': True},
        'failure_boundary': 'The sysfs block directory required by host_witness does not exist; the namespace helper was not dispatched.',
        'fixture_directory_empty': True, 'helper_stdout_exists': False,
        'marker': 'goby-bound-scan-root-mount-failed-terminal-v1', 'mount_report_sha256': OP.MOUNT_REPORT_SHA,
        'observed_at': '2026-09-12T06:16:36.133200+00:00', 'pair_evidence_retained': True,
        'receipt_sha256': OP.RECEIPT_SHA, 'report_sha256': OP.REPORT_SHA, 'run_id': OP.RUN,
        'states': [{'properties': dict(state, InvocationID=OP.UNIT_PINS[unit]['InvocationID']),
                    'recursive_cgroup_empty': True, 'unit': unit} for unit in (OP.OUTER_UNIT, OP.UNIT)], 'status': 'failed'}


def mount_report_fixture():
    return {'binary': copy.deepcopy(OP.BINARY), 'child_cleanup': {'killed': False, 'terminal': True},
        'failure': 'FileNotFoundError', 'invocation_id': OP.UNIT_PINS[OP.UNIT]['InvocationID'],
        'loop_scope': 'observed-only-no-loop-allocation', 'manifest': OP.MANIFEST_SHA, 'run_id': OP.RUN,
        'schema': 28, 'source': str(OP.SOURCE), 'status': 'failed', 'system_reboot_tested': False,
        'verification': OP.VERIFICATION}


def mount_artifacts_fixture():
    return {'binary': copy.deepcopy(OP.BINARY), 'fixture_directory_empty': True,
        'absent_paths': list(OP.ABSENT_MOUNT_ARTIFACTS), 'go_log_bytes': 0,
        'compile_stdout_bytes': 0, 'compile_stderr_bytes': 0}


class DisposalGuards(unittest.TestCase):
    def test_exact_run_source_runner_and_pair_pins(self):
        self.assertEqual(OP.RUN, '20260912_061505_b2b453a716a9')
        self.assertEqual(str(OP.SOURCE), '/opt/goby-test/exec-work-m3e/source-attempt-54')
        self.assertEqual(str(OP.OUTPUT), '/opt/goby-test/exec-work-m3e/client-backup-run-20260912_061505_b2b453a716a9')
        self.assertEqual(str(OP.EXECUTION), '/opt/goby-test/exec-work-m3e/bound-scan-root-mount-execution-01')
        self.assertEqual(OP.MANIFEST_SHA, 'c5a5cf6bf0afa973fbd2c087d300dbe6bd3079f92c7237b5a108d76ef8ae6fc3')
        self.assertEqual(OP.CATALOG_SHA, '8e7569c8fe2073ee2ed4c51147f9abc21061d1aac9554843101b826fa5a1cc2b')
        self.assertEqual(OP.RUN_EXPRESSION, '^TestRootBindingScanMountNamespaceHelper$')
        self.assertEqual(OP.PACKAGES, ('./internal/library',))
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
            {'name': 'goby_backup_m3e_source', 'role_oid': 18925101, 'database_oid': 18925102},
            {'name': 'goby_backup_m3e_target', 'role_oid': 18925103, 'database_oid': 18925104}])
        self.assertEqual(OP.PUBLIC_OIDS, {'goby_backup_m3e_source': 2200, 'goby_backup_m3e_target': 2200})
        self.assertEqual(OP.IDENTITY_SHA, {
            'goby_backup_m3e_source': '27ececc8bfdbffe1f3bbdecf0711fb1c8740fd3932592439734a3d5ce333ad7a',
            'goby_backup_m3e_target': '73a6af41578c6e663a0f310f6f8bd3a81fe427f8ea4e12cc918e97202882f1b3'})
        self.assertEqual(OP.OUTPUT_IDENTITY, {'device': 2049, 'inode': 2526409})
        self.assertEqual(OP.RECEIPT_SHA, '190c202ca956a52b1914fabe7363ea821bd691f0a6f47869edc8785bc3fe288f')
        self.assertEqual(OP.REPORT_SHA, '248fc45c97250da02994d3947b401f43bb74ee568e3ebadb31dec46a291487f6')
        self.assertEqual(OP.LOG_SHA, 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855')
        self.assertEqual(OP.LOG_BYTES, 0)
        self.assertEqual(OP.TERMINAL_SHA, '737c70c0919491a559f0b79457849e34d6b0a19e394b955bf0ff5310ae88de60')
        self.assertEqual(OP.TREE_SHA, {
            str(OP.SOURCE): '1d94233a92f26b4a9f66d6210de1c744d79729b9a6a4d63df9aca94fd866f355',
            str(OP.OUTPUT): 'a55839d5dda0ea825f77c0364e7a14b6e1148bc9969e743a76d04e1462035e3f',
            str(OP.EXECUTION): 'd2daa0c92c12a0d9029929cf3eb8feebf79db73f8220ca6a7077bda14309032a'})

    def test_mount_launch_rejects_changed_selectors_and_wrong_input_types(self):
        expected = {'source': OP.SOURCE, 'manifest_sha256': OP.MANIFEST_SHA, 'schema': 28,
                    'mode': 'targeted', 'run': '^TestRootBindingScanMountNamespaceHelper$',
                    'package': ['./internal/library']}
        m = SimpleNamespace(require=require)
        OP.validate_mount_launch(m, SimpleNamespace(**expected))
        mutations = [('source', OP.WORK / 'source-attempt-47'), ('manifest_sha256', '0' * 64),
            ('schema', 27), ('schema', 28.0), ('schema', True), ('mode', 'full'),
            ('run', ''), ('run', '^TestRootBinding'), ('run', None),
            ('package', []), ('package', ['./internal/database']), ('package', ['./internal/library', './internal/library']),
            ('package', ('./internal/library',)), ('package', None)]
        for key, value in mutations:
            with self.subTest(field=key, value=value), self.assertRaises(ValueError):
                OP.validate_mount_launch(m, SimpleNamespace(**dict(expected, **{key: value})))

    def test_mount_admission_binary_and_failure_artifact_pins_are_independent(self):
        self.assertEqual(OP.MOUNT_REPORT_SHA, '0111e24b9a21e8ba4cc0fdebf02707611c29915adb57610f7852d987463190a3')
        self.assertEqual(OP.MOUNT_WORKER_SHA, 'a605cc2f3bd64c5258b261d4ead54e6d67ee691d84c12f36c6cb62028782f11c')
        self.assertEqual(OP.MOUNT_REQUEST_SHA, '41ad7c2cc37dfc2dedadf4cec9e2da81b0505ebe825e098b9a2bbbf86236a762')
        self.assertEqual(OP.RUN_SCRIPT_SHA, 'fe008e8c899514c0221e1ff736dc175768dde728b88d0e2ca55cb722f030bc82')
        self.assertEqual(OP.VERIFICATION, 'source54-original-storage-scan-recovery-private-mount')
        self.assertEqual(OP.HOST_NAMESPACE, 'mnt:[4026531841]')
        self.assertEqual(OP.BINARY, {'path': str(OP.OUTPUT / 'tmp/library.test'), 'bytes': 34877621,
            'device': 2049, 'inode': 2526708,
            'sha256': 'c9bf6cfd000afe3b0da512d98d7ffcf811885f31fb2cc3e7e8d82195d4fc03a0'})
        self.assertEqual(OP.ABSENT_MOUNT_ARTIFACTS, ('helper.stdout', 'helper.stderr', 'host-mountinfo-before',
            'host-loops-before.json', 'host-mountinfo-after', 'host-loops-after.json',
            'host-mountinfo-terminal', 'host-loops-terminal.json', 'tmp/goby-linux-amd64'))
        self.assertEqual(OP.MOUNT_ADMISSION, {'host_namespace': OP.HOST_NAMESPACE,
            'request_sha256': OP.MOUNT_REQUEST_SHA, 'run_script_sha256': OP.RUN_SCRIPT_SHA,
            'verification': OP.VERIFICATION, 'worker_sha256': OP.MOUNT_WORKER_SHA})
        self.assertEqual(OP.INNER_PROCESS, {'pid': 1329950, 'start_ticks': 11657250,
            'boot_id': '6bdfc486-7bc8-412f-82b5-70095a09dde7'})
        self.assertEqual(OP.UNIT, 'goby-client-backup-20260912-061505-b2b453a716a9.service')
        self.assertEqual(OP.OUTER_UNIT, 'goby-bound-scan-root-mount-controller-v1.service')
        self.assertEqual(OP.UNIT_PINS[OP.UNIT], {'InvocationID': 'bf8018cffd8548e99d177f54628752cc',
            'ExecMainPID': '1329950', 'Description': OP.TAG})
        self.assertEqual(OP.UNIT_PINS[OP.OUTER_UNIT], {'InvocationID': '6c741a464ae74b31839355d3ed2e9f06',
            'ExecMainPID': '1329773', 'Description': '[systemd-run] /usr/bin/python3 -I -B ' + str(OP.WORK) +
            '/bound-scan-root-mount-tool-01/verify-bound-scan-root-mount.py --full-report ' +
            str(OP.WORK / 'client-backup-run-20260912_053517_9cb0074fc731/report.json') +
            ' --full-report-sha256 50f8543b1b11cf7a3e00d3a0a83684080835fdad0f54ca4c983650f0f1e8892e' +
            ' --host-namespace "mnt:[4026531841]"'})
        self.assertEqual(OP.FULL_RUN, '20260912_053517_9cb0074fc731')
        self.assertEqual(str(OP.FULL_REPORT), str(OP.WORK / 'client-backup-run-20260912_053517_9cb0074fc731/report.json'))
        self.assertEqual(OP.FULL_REPORT_SHA, '50f8543b1b11cf7a3e00d3a0a83684080835fdad0f54ca4c983650f0f1e8892e')
        self.assertEqual(OP.PREREQUISITES, {
            'full_controller_scope': str(OP.WORK / 'scan-reconciliation-full-execution-01'),
            'full_controller_terminal': {'cgroup_empty': True, 'properties': {'ActiveState': 'active',
                'ControlGroup': '', 'ExecMainStatus': '0', 'InvocationID': 'c8641f6597f0411ca832d052b885499d',
                'MainPID': '0', 'Result': 'success', 'SubState': 'exited'},
                'unit': 'goby-scan-reconciliation-full-controller-v1.service'},
            'full_inner_unit': 'goby-client-backup-20260912-053517-9cb0074fc731.service',
            'full_receipt_sha256': '6accf5be1fa133d5d9f82a270ead3a0e2e82e1b5e0685f1cfee4d09843a48479',
            'full_report': str(OP.FULL_REPORT), 'full_report_sha256': OP.FULL_REPORT_SHA, 'full_run': OP.FULL_RUN,
            'prior_namespace_report_sha256': '8e3588a232a9838523485c587769e8d8a57bef28cacd31f6cefc6f96a800eb40',
            'prior_namespace_worker_sha256': 'd71fa1157419bff0d7964ea7b442a7b2bdfe20475e429484b12b74ca49f598f4'})

    def test_exact_mount_failure_requires_unexecuted_helper_and_empty_artifacts(self):
        terminal, report, artifacts = mount_terminal_fixture(), mount_report_fixture(), mount_artifacts_fixture()
        original = json.dumps([terminal, report, artifacts], sort_keys=True).encode()
        OP.validate_mount_evidence(SimpleNamespace(require=require), terminal, report, artifacts)
        self.assertEqual(json.dumps([terminal, report, artifacts], sort_keys=True).encode(), original)
        self.assertIs(terminal['actual_mount_recovery_accepted'], False)
        self.assertIs(terminal['helper_stdout_exists'], False)
        self.assertEqual(report['failure'], 'FileNotFoundError')
        self.assertEqual(artifacts['go_log_bytes'], 0)

    def test_mount_evidence_rejects_every_missing_changed_or_extra_top_level_field(self):
        m = SimpleNamespace(require=require)
        originals = [mount_terminal_fixture(), mount_report_fixture(), mount_artifacts_fixture()]
        for index, document in enumerate(originals):
            for key in document:
                for defect in ('missing', 'changed'):
                    changed = copy.deepcopy(originals)
                    if defect == 'missing':
                        del changed[index][key]
                    else:
                        changed[index][key] = None
                    with self.subTest(document=index, field=key, defect=defect), self.assertRaises(ValueError):
                        OP.validate_mount_evidence(m, *changed)
            changed = copy.deepcopy(originals)
            changed[index]['unexpected'] = True
            with self.subTest(document=index, extra=True), self.assertRaises(ValueError):
                OP.validate_mount_evidence(m, *changed)
            for value in (None, [], '', True):
                changed = copy.deepcopy(originals)
                changed[index] = value
                with self.subTest(document=index, value=value), self.assertRaises(ValueError):
                    OP.validate_mount_evidence(m, *changed)

    def test_mount_terminal_rejects_changed_states_cgroups_and_truthy_claims(self):
        m = SimpleNamespace(require=require)
        terminal = mount_terminal_fixture()
        for key, value in [('actual_mount_recovery_accepted', 0), ('fixture_directory_empty', 1),
                ('helper_stdout_exists', 0), ('pair_evidence_retained', 1), ('states', list(reversed(terminal['states'])))]:
            with self.subTest(field=key), self.assertRaises(ValueError):
                OP.validate_mount_evidence(m, dict(terminal, **{key: value}), mount_report_fixture(), mount_artifacts_fixture())
        for key in terminal['cleanup']:
            changed = copy.deepcopy(terminal)
            changed['cleanup'][key] = 1
            with self.subTest(cleanup=key), self.assertRaises(ValueError):
                OP.validate_mount_evidence(m, changed, mount_report_fixture(), mount_artifacts_fixture())
        for index, state in enumerate(terminal['states']):
            for key in state['properties']:
                changed = copy.deepcopy(terminal)
                changed['states'][index]['properties'][key] = 'changed'
                with self.subTest(state=index, field=key), self.assertRaises(ValueError):
                    OP.validate_mount_evidence(m, changed, mount_report_fixture(), mount_artifacts_fixture())
            for key, value in [('recursive_cgroup_empty', 1), ('recursive_cgroup_empty', False), ('unit', 'other.service')]:
                changed = copy.deepcopy(terminal)
                changed['states'][index][key] = value
                with self.subTest(state=index, field=key, value=value), self.assertRaises(ValueError):
                    OP.validate_mount_evidence(m, changed, mount_report_fixture(), mount_artifacts_fixture())

    def test_mount_report_rejects_false_cleanup_schema_types_and_changed_failure(self):
        m = SimpleNamespace(require=require)
        report = mount_report_fixture()
        for key, value in [('schema', 28.0), ('schema', True), ('system_reboot_tested', 0),
                ('failure', 'RuntimeError'), ('status', 'passed'), ('loop_scope', 'loop-allocated'),
                ('verification', 'other'), ('invocation_id', '0' * 32)]:
            with self.subTest(field=key, value=value), self.assertRaises(ValueError):
                OP.validate_mount_evidence(m, mount_terminal_fixture(), dict(report, **{key: value}), mount_artifacts_fixture())
        for key, value in [('killed', 0), ('killed', True), ('terminal', 1), ('terminal', False)]:
            changed = copy.deepcopy(report)
            changed['child_cleanup'][key] = value
            with self.subTest(cleanup=key, value=value), self.assertRaises(ValueError):
                OP.validate_mount_evidence(m, mount_terminal_fixture(), changed, mount_artifacts_fixture())

    def test_mount_artifacts_reject_nonempty_logs_fixtures_and_dispatched_helper_evidence(self):
        m = SimpleNamespace(require=require)
        artifacts = mount_artifacts_fixture()
        for key in ('go_log_bytes', 'compile_stdout_bytes', 'compile_stderr_bytes'):
            for value in (1, -1, False, 0.0, '0'):
                with self.subTest(field=key, value=value), self.assertRaises(ValueError):
                    OP.validate_mount_evidence(m, mount_terminal_fixture(), mount_report_fixture(), dict(artifacts, **{key: value}))
        for value in (False, 1, None):
            with self.subTest(fixture=value), self.assertRaises(ValueError):
                OP.validate_mount_evidence(m, mount_terminal_fixture(), mount_report_fixture(),
                                          dict(artifacts, fixture_directory_empty=value))
        for index in range(len(OP.ABSENT_MOUNT_ARTIFACTS)):
            changed = copy.deepcopy(artifacts)
            del changed['absent_paths'][index]
            with self.subTest(present=OP.ABSENT_MOUNT_ARTIFACTS[index]), self.assertRaises(ValueError):
                OP.validate_mount_evidence(m, mount_terminal_fixture(), mount_report_fixture(), changed)
        for value in (tuple(OP.ABSENT_MOUNT_ARTIFACTS), list(reversed(OP.ABSENT_MOUNT_ARTIFACTS)),
                      list(OP.ABSENT_MOUNT_ARTIFACTS) + ['unreviewed']):
            with self.subTest(absent_paths=value), self.assertRaises(ValueError):
                OP.validate_mount_evidence(m, mount_terminal_fixture(), mount_report_fixture(), dict(artifacts, absent_paths=value))

    def test_compiled_helper_identity_rejects_replacement_and_noninteger_metadata_in_every_document(self):
        m = SimpleNamespace(require=require)
        originals = [mount_terminal_fixture(), mount_report_fixture(), mount_artifacts_fixture()]
        for index in range(len(originals)):
            for key in OP.BINARY:
                changed = copy.deepcopy(originals)
                del changed[index]['binary'][key]
                with self.subTest(document=index, missing=key), self.assertRaises(ValueError):
                    OP.validate_mount_evidence(m, *changed)
                for value in (None, str(OP.BINARY[key]) + '-changed'):
                    changed = copy.deepcopy(originals)
                    changed[index]['binary'][key] = value
                    with self.subTest(document=index, field=key, value=value), self.assertRaises(ValueError):
                        OP.validate_mount_evidence(m, *changed)
            for key in ('bytes', 'device', 'inode'):
                changed = copy.deepcopy(originals)
                changed[index]['binary'][key] = float(OP.BINARY[key])
                with self.subTest(document=index, numeric_type=key), self.assertRaises(ValueError):
                    OP.validate_mount_evidence(m, *changed)
        changed = copy.deepcopy(originals)
        for document in changed:
            document['binary']['inode'] += 1
        with self.assertRaises(ValueError):
            OP.validate_mount_evidence(m, *changed)

    def test_private_json_decoder_rejects_malformed_keys_and_values_without_effects(self):
        self.assertIsInstance(MEMORY_JSON_DECODER.scan_once, FunctionType)
        self.assertIs(MEMORY_JSON_DECODER.parse_string, json.decoder.py_scanstring)
        self.assertIs(MEMORY_JSON_DECODER.parse_object.__globals__['scanstring'], json.decoder.py_scanstring)
        before = len(BLOCKED_EFFECTS)
        self.assertEqual(memory_json_decode(b'{"nested":{"value":[1,true,null,"text"]}}'),
                         {'nested': {'value': [1, True, None, 'text']}})
        for raw in (b'{malformed', b'{"unterminated}', b'{"key":"bad\\q"}', b'{"key":}', b'{"key":[1,]}'):
            with self.subTest(raw=raw), self.assertRaises(json.JSONDecodeError):
                memory_json_decode(raw)
        self.assertEqual(len(BLOCKED_EFFECTS), before)

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
        for key in receipt['bound_scan_mount']:
            changed = copy.deepcopy(receipt)
            changed['bound_scan_mount'][key] = 'changed'
            with self.subTest(mount_admission=key), self.assertRaises(ValueError):
                OP.validate_receipt(m, changed)

    def test_retained_public_identity_matches_both_unmodified_namespaces(self):
        source, target = owned_pair(0), owned_pair(1)
        self.assertEqual(source['public']['oid'], 2200)
        self.assertEqual(target['public']['oid'], 2200)
        self.assertEqual(OP.public_expected(source['name'])['oid'], 2200)
        self.assertEqual(OP.public_expected(target['name'])['oid'], 2200)
        self.assertEqual(source['public'], snapshot_fixture(source)['public'])
        self.assertEqual(target['public'], snapshot_fixture(target)['public'])
        OP.validate_receipt(SimpleNamespace(require=require), receipt_fixture())
        OP.validate_empty_snapshot(SimpleNamespace(require=require), target, snapshot_fixture(target))

    def test_failed_report_proves_the_exact_failure_and_restored_hba(self):
        report = {'marker': 'goby-client-backup-pair-m3e-v1', 'status': 'failed', 'run_id': OP.RUN,
            'source': str(OP.SOURCE), 'source_manifest_sha256': OP.MANIFEST_SHA, 'schema': 28, 'mode': 'targeted',
            'catalog_sha256': OP.CATALOG_SHA, 'unit': OP.UNIT, 'unit_exit': 1, 'unit_process': copy.deepcopy(OP.INNER_PROCESS),
            'cluster': copy.deepcopy(OP.CLUSTER), 'pair_evidence_retained': True, 'hba_before_sha256': OP.HBA_SHA,
            'hba_after_sha256': OP.HBA_SHA, 'error': 'The verified unit failed or was never observed running.',
            'cleanup': {'hba_restored_exactly': True, 'receipt_saved': True, 'unit_terminal': True}, 'tools': {},
            'operator_sha256': OP.MOUNT_WORKER_SHA, 'prerequisites': copy.deepcopy(OP.PREREQUISITES),
            'verification': OP.VERIFICATION}
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
            changed_groups[unit] = ['1252659']
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
            args=SimpleNamespace(source=OP.SOURCE, manifest_sha256=OP.MANIFEST_SHA, schema=28,
                                 mode='targeted', run=OP.RUN_EXPRESSION, package=list(OP.PACKAGES)),
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

    def test_mount_artifact_ast_limits_cover_binary_rechecks_and_tree_reader(self):
        locations = validate_artifact_read_limits(TREE)
        self.assertEqual(len(locations), 4)
        self.assertEqual(OP.LOG_BYTES, 0)
        self.assertGreater(OP.BINARY['bytes'], 8 << 20)
        self.assertLess(OP.BINARY['bytes'], 64 << 20)
        for index in range(len(locations)):
            changed = copy.deepcopy(TREE)
            altered = validate_artifact_read_limits(changed)[index]
            altered.keywords = [keyword for keyword in altered.keywords if keyword.arg != 'limit']
            with self.subTest(location=index), self.assertRaises(ValueError):
                validate_artifact_read_limits(changed)

    def test_mount_launch_evidence_and_artifact_rechecks_precede_mutation(self):
        calls = [node for node in memory_walk(TREE) if isinstance(node, ast.Call)]
        launch = [node for node in calls if isinstance(node.func, ast.Name) and node.func.id == 'validate_mount_launch']
        evidence = [node for node in calls if isinstance(node.func, ast.Name) and node.func.id == 'validate_mount_evidence']
        observations = [node for node in calls if isinstance(node.func, ast.Name) and node.func.id == 'observe_mount_artifacts']
        validate = [node for node in calls if isinstance(node.func, ast.Attribute) and node.func.attr == 'validate_arguments']
        constructors = [node for node in calls if isinstance(node.func, ast.Attribute) and node.func.attr == 'Runner']
        published = [node for node in calls if isinstance(node.func, ast.Name) and node.func.id == 'publish']
        self.assertEqual((len(launch), len(evidence), len(observations), len(validate), len(constructors)), (1, 1, 2, 1, 1))
        self.assertLess(launch[0].lineno, validate[0].lineno)
        self.assertLess(validate[0].lineno, constructors[0].lineno)
        self.assertLess(evidence[0].lineno, constructors[0].lineno)
        self.assertEqual([node.id for node in (evidence[0].args[0], evidence[0].args[3])], ['m', 'failure_artifacts'])
        self.assertLess(constructors[0].lineno, min(node.lineno for node in published))
        assignments = [node for node in memory_walk(TREE) if isinstance(node, ast.Assign)]
        originals = [node for node in assignments if any(isinstance(target, ast.Name) and
            target.id == 'failure_artifacts' for target in node.targets)]
        self.assertEqual(len(originals), 1)
        self.assertEqual(originals[0].value.func.id, 'observe_mount_artifacts')
        self.assertLess(originals[0].lineno, evidence[0].lineno)
        repeated = [node for node in calls if isinstance(node.func, ast.Attribute) and node.func.attr == 'require' and
                    node.args and ast.dump(node.args[0]) == ast.dump(EXPECTED_ARTIFACT_RECHECK)]
        cluster = next(node for node in memory_walk(TREE) if isinstance(node, ast.FunctionDef) and node.name == 'check_cluster')
        self.assertEqual(len(repeated), 1)
        self.assertTrue(cluster.lineno < repeated[0].lineno < cluster.end_lineno)
        pins = [node for node in assignments if any(isinstance(target, ast.Name) and target.id == 'pinned' for target in node.targets)]
        self.assertEqual(len(pins), 1)
        self.assertEqual(constant_value(pins[0].value, OP.__dict__), {
            'report.json': OP.REPORT_SHA, 'go.log': OP.LOG_SHA, 'catalog-before.json': OP.CATALOG_BEFORE_SHA,
            'hba-original': OP.HBA_SHA, 'mount-report.json': OP.MOUNT_REPORT_SHA, 'mount-request.json': OP.MOUNT_REQUEST_SHA,
            'mount-worker.py': OP.MOUNT_WORKER_SHA, 'run.sh': OP.RUN_SCRIPT_SHA,
            'compile.stdout': OP.LOG_SHA, 'compile.stderr': OP.LOG_SHA})
        self.assertLess(pins[0].lineno, originals[0].lineno)

    def test_runtime_ast_and_runner_constructor_reject_build_test_and_restart_paths(self):
        validate_runtime_call_boundary(TREE)
        validate_runtime_call_boundary(RUNNER_CONSTRUCTOR, strict_runner=False)
        for template in UNSAFE_CALL_TEMPLATES:
            changed = copy.deepcopy(TREE)
            changed.body.append(copy.deepcopy(template))
            with self.subTest(call=ast.dump(template)), self.assertRaises(ValueError):
                validate_runtime_call_boundary(changed)

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
    global OP, TREE, REMOVE_PAIR, VALIDATE_ROWS, VALIDATE_DISPOSAL, ATTESTATION, RUNNER_CONSTRUCTOR
    if sys.platform != 'linux' or os.geteuid() != 0 or not os.environ.get('SSH_CONNECTION') or len(sys.argv) != 2:
        raise SystemExit('Run only through authorized root SSH with the source54 mount-failure operator path.')
    path = Path(sys.argv[1])
    if path.name != 'dispose-source54-mount-failed-pair.py':
        raise SystemExit('Only the exact source54 mount-failure disposal operator is supported.')
    raw = path.read_bytes()
    TREE = ast.parse(raw, filename='<reviewed-source54-mount-operator>')
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
        'validate_mount_launch', 'validate_mount_evidence',
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
    constructors = [node for node in runner_class[0].body if isinstance(node, ast.FunctionDef) and node.name == '__init__']
    require(len(constructors) == 1, 'The pinned runner constructor changed.')
    RUNNER_CONSTRUCTOR = constructors[0]
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
    print(json.dumps({'suite': 'source54-disposal-guards', 'status': 'passed' if result.wasSuccessful() else 'failed',
        'tests': result.testsRun, 'failures': len(result.failures), 'errors': len(result.errors), 'skips': len(result.skipped),
        'operator_sha256': operator_sha256, 'runner_sha256': EXPECTED_RUNNER_SHA,
        'fixtures': 'two-source-reads-then-audit-fenced-memory-only', 'initial_source_reads': 2,
        'database_commands': 0, 'filesystem_mutations': 0, 'fence_self_test_blocks': len(BLOCKED_EFFECTS),
        'blocked_effect_details': BLOCKED_EFFECT_DETAILS,
        'summaries': [detail for _, detail in [*result.failures, *result.errors]]}))
    return 0 if result.wasSuccessful() else 1


if __name__ == '__main__':
    raise SystemExit(main())
