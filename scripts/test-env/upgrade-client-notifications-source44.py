#!/usr/bin/env python3
"""Replace only the existing positive schema27 candidate with verified source44.

Run on test-env as root after independent review and remote guard verification.
The preflight phase makes no HTTP requests, service changes, or evidence writes.
The deploy phase owns one new evidence directory and exactly one stop/start.
Every prior database row, sequence, catalog fact, credential, recovery file and
all three receipted media groups must remain unchanged. The historical empty
Extras validator is never changed or called for the accepted positive profile.
Failures retain their last durable phase; this operator never resumes, retries
a mutation, restores a database, rolls back a binary, or deploys native assets.
"""

from __future__ import annotations

import argparse
import copy
import datetime as dt
import fcntl
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time
import types


WORK = Path('/opt/goby-test/exec-work-m3e')
TOOL = WORK / 'client-notifications-source44-tool-01'
OUTPUT = WORK / 'client-notifications-source44-upgrade-v1'
MARKER = 'goby-client-notifications-source44-upgrade-v1'
SOURCE = WORK / 'source-attempt-44'
MANIFEST_SHA = 'c2c9492589360b058c6533d0719219cf85ab5bc056bd76290cee51e7dc897f8b'
PUBLICATION = '35ae3d000f812fa18d234921cedb3c33d88190e0'
RUN_ID = '20260912_032033_aeb954fd9c60'
REPORT = WORK / ('client-backup-run-' + RUN_ID) / 'report.json'
REPORT_SHA = '2d82c22f5d335314373cd042de5f8a75f66706e5dae87d7126ec05427c79fe16'
BINARY_SHA = 'cd67f2e71ff1b63e3c138cdba1f9c9d1e584e2788b47964a332b38311cda0e2d'
BINARY_BYTES = 28357495
BINARY = REPORT.parent / 'tmp/goby-linux-amd64'
BINARY_REPORT = {'path': str(BINARY), 'sha256': BINARY_SHA, 'bytes': BINARY_BYTES}
TERMINAL = WORK / 'library-changed-capacity-full-execution-02/terminal.json'
TERMINAL_SHA = '46eeeda58a44b691a05346f9db9c38a6b8f363dc0e7deb4ba0d8db8b01c86a86'
CONTROLLER = 'goby-library-changed-capacity-full-controller-v2.service'
CONTROLLER_INVOCATION = 'd18910a201d44bbfa9bbb77cd9cb8c25'
CATALOG_SHA = '1fc91c2e380805bff0f87867547d307bc7830ffeb49c3489da4e1713a5c0047d'
MIGRATION_SHA = 'b62d0422dddb9e258f46589898f672b08fc6e4c12fea7456059f855a6600353c'
OLD_BINARY_SHA = 'af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620'
OLD_STATE_SHA = '5319bc49b2753b84ca04f279523f2482a49a94fabd9944dc369347b6d87224e1'
OLD_RUNTIME_SHA = 'd8689a4e0b36816ed462816856dfa73af6fba5f31f044632ed173db99c8842df'
OLD_MANIFEST_SHA = 'a65070315ce3b31dd70143267cbf774a838bc0e5b1ed99f34c3c759d392daa65'
OLD_PROCESS = {'boot_id': '6bdfc486-7bc8-412f-82b5-70095a09dde7', 'pid': 748513, 'start_ticks': 6996875}
OLD_INVOCATION = 'b7a9ae3d00364e0993d2af2c7d3e1063'
PRIMARY_UNIT = 'goby-foundation-test.service'
PRIMARY_PROCESS = {'boot_id': OLD_PROCESS['boot_id'], 'pid': 762090, 'start_ticks': 7637121}
PRIMARY_INVOCATION = 'bb94d74b475f4382a6ec6f6df181dd74'
BASELINE = WORK / 'client-library-permission-ui-v1/after-full.json'
BASELINE_SHA = '12278b4117f352b433c246fec0b53d7879700f37245f1c6999beea00587591fd'
PERMISSION_REPORT = WORK / 'client-library-permission-ui-v1/report.json'
PERMISSION_REPORT_SHA = '4755b35026c47b692d5f3e68f57e7f2e6e4846b53643bd714cc92f493fe62f2d'
PROFILE = WORK / 'client-special-features-protocol-finalization-v1/completed.json'
PROFILE_SHA = 'b443e5f6d5faceb3486d68644298b0a1527f6dcc1e4ac1109ff6d0c252fdfb36'
B_USER = 'ecbbe4cb82403879bc4b4f78894c5738'
HELPERS = {
    'op': (WORK / 'prepare-client-fixture.py', '84d21e8ac0b48c5dfd3d2c7ae35b0aec0d7f4f811e65658081490aa44947b49c'),
    'restriction': (WORK / 'client-library-restriction-tool-01/verify-client-library-restriction.py', '99cb5b89f0da94940bed34a86607a545278da914750ae09ac7b52d458810004e'),
    'profile': (WORK / 'client-special-features-profile-tool-01/client-special-features-profile.py', '6bbc0bfd17b1c7eef44028fe34481436d39a3e6645442340189e2da35c382252'),
    'ext': (WORK / 'client-extra-root-tool-02/extend-client-special-features-root.py', '7a9cda4a36ebbe9c2311031db1f6a57265eef8fc8807057a3d650ef6c16b2a46'),
}
PRIMARY_FILES = {
    '/opt/goby-dev/goby': OLD_BINARY_SHA,
    '/etc/systemd/system/goby-foundation-test.service': '91a9b0e55baf053db25281236d0d7c8c055f895b093d4474d9c27e888610117d',
    '/etc/systemd/system/goby-foundation-test.service.d/20-application-keys.conf': 'ed9e8b91416918ce797ab3b5507d8843f62109d033cb30a679a44e3abbea3dd9',
    '/etc/systemd/system/goby-foundation-test.service.d/30-observability.conf': 'cc8fecb588840dd49848352936747a712d8790f8f7bd25d8d31a8a41069a46e5',
    '/etc/systemd/system/goby-foundation-test.service.d/40-backup-recovery.conf': 'e62ad56f0e93c1df2b89a6b5f7dbbcb032fa98d9d4cdcb8ebb04ba1e01b5682e',
    '/opt/goby-test/runtime.env': '2043e72115338d04775485dd63702c6084d36d09331ca5cdac66819152619607',
    '/opt/goby-test/recovery-m5j.env': '6847d34cfd9af7c53a8b16406db6f341f418b6ffdacb83bfe8c1f9bfd36b4c7c',
}
ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8'}


class Failure(Exception):
    """A source, ownership, continuity or one-shot boundary was violated."""


def require(value, message):
    if not value:
        raise Failure(message)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def identity(info):
    return {'device': info.st_dev, 'inode': info.st_ino, 'uid': info.st_uid, 'gid': info.st_gid,
            'mode': stat.S_IMODE(info.st_mode), 'links': info.st_nlink, 'bytes': info.st_size,
            'mtime_ns': info.st_mtime_ns, 'ctime_ns': info.st_ctime_ns}


def protected(path, expected=None, modes=(0o600, 0o644), limit=64 << 20):
    require(isinstance(path, Path) and path.is_absolute() and '..' not in path.parts,
            'An input path is not absolute and canonical.')
    for parent in reversed(path.parents):
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                'An input parent is not a protected root-owned directory.')
    before = path.lstat()
    require(stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and before.st_nlink == 1 and
            stat.S_IMODE(before.st_mode) in modes and 0 < before.st_size <= limit,
            'An input file has an unexpected owner, mode, link count or size.')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as handle:
        require(identity(os.fstat(handle.fileno())) == identity(before), 'An input changed before opening.')
        raw = handle.read(limit + 1)
        require(identity(os.fstat(handle.fileno())) == identity(before), 'An input changed while being read.')
    require(identity(path.lstat()) == identity(before) and len(raw) == before.st_size and
            (expected is None or digest(raw) == expected), 'An input changed or failed its exact digest pin.')
    return raw


def load_helper(path, expected, name):
    raw = protected(path, expected)
    module = types.ModuleType('source44_' + name)
    module.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), module.__dict__)
    return module


def validate_product_report(op, report):
    require(report.get('marker') == 'goby-client-backup-pair-m3e-v1' and report.get('status') == 'passed' and
            report.get('mode') == 'full' and type(report.get('schema')) is int and report['schema'] == 27 and
            report.get('catalog_sha256') == CATALOG_SHA and report.get('source') == str(SOURCE) and
            report.get('source_manifest_sha256') == MANIFEST_SHA and report.get('run_id') == RUN_ID and
            type(report.get('unit_exit')) is int and report['unit_exit'] == 0,
            'The full report does not identify the exact successful source44/schema27 run.')
    cleanup = report.get('cleanup', {})
    required = {'unit_terminal', 'hba_restored_exactly', 'goby_backup_m3e_source_removed',
                'goby_backup_m3e_target_removed', 'preexisting_catalog_unchanged', 'receipt_saved'}
    require(isinstance(cleanup, dict) and set(cleanup) == required and all(value is True for value in cleanup.values()),
            'The complete run has not finished its six owned cleanup checks.')
    tests, packages = report.get('tests', {}), report.get('packages')
    passed = tests.get('passed')
    require(type(tests.get('top_level_passes')) is int and tests['top_level_passes'] == 2002 and
            type(tests.get('failures')) is int and tests['failures'] == 0 and
            type(tests.get('skips')) is int and tests['skips'] == 0 and isinstance(passed, list) and
            all(isinstance(name, str) and name for name in passed) and len(passed) == len(set(passed)) == 2002 and
            (op.PRODUCT_REQUIRED_TESTS | op.PRODUCT_27_REQUIRED_TESTS | {
                'TestHTTPLibraryRefreshDurablyQueuesEveryLibraryBeyondScannerCapacity',
                'TestManagerRetriesCapacityAndIndependentScansBeyondTerminalPage'}) <= set(passed),
            'The complete run has failed, skipped, missing or duplicate test evidence.')
    require(isinstance(packages, list) and all(isinstance(name, str) for name in packages) and
            len(packages) == len(set(packages)) == 25 and
            set(packages) == op.PRODUCT_PACKAGES | {'github.com/moooyo/goby/internal/storagebinding'},
            'The full report does not cover the exact 25 source44 packages.')
    require(op.equal_json(report.get('binary'), BINARY_REPORT), 'The final accepted binary descriptor changed.')


def validate_terminal(document):
    require(isinstance(document, dict), 'The terminal receipt is not an object.')
    require(document.get('unit') == CONTROLLER and document.get('run_id') == RUN_ID and
            document.get('report_sha256') == REPORT_SHA and document.get('binary') == BINARY_REPORT and
            type(document.get('binary', {}).get('bytes')) is int and
            document.get('recursive_cgroup_empty') is True and document.get('state') == {
                'MainPID': '0', 'Result': 'success', 'ExecMainStatus': '0', 'ControlGroup': '',
                'SubState': 'exited', 'InvocationID': CONTROLLER_INVOCATION},
            'The exact full-run terminal receipt is incomplete or differs.')


def validate_starting_state(op, state):
    require(state.get('marker') == op.MARKER and type(state.get('schema')) is int and state['schema'] == 27 and
            state.get('phase') == 'ready' and state.get('stage') == 'complete' and
            state.get('binary_sha256') == OLD_BINARY_SHA and state.get('runtime_sha256') == OLD_RUNTIME_SHA and
            state.get('process') == OLD_PROCESS and state.get('viewer_id') == B_USER and
            type(state.get('process', {}).get('pid')) is int and type(state.get('process', {}).get('start_ticks')) is int and
            state.get('upgrade', {}).get('phase') == 'complete' and 'notifications_upgrade' not in state and
            all(state.get(key) is False for key in ('binary_pending', 'bootstrap_pending', 'credentials_pending',
                'database_creation_pending', 'role_creation_pending', 'unit_pending', 'start_pending', 'viewer_pending')),
            'The candidate is not the freshly checked, completed source32 starting state.')
    binding = op.schema27_binding(state)
    require(binding.get('source') == str(WORK / 'source-attempt-32') and
            binding.get('source_manifest_sha256') == OLD_MANIFEST_SHA,
            'The candidate no longer retains its accepted source32 catalog binding.')
    profile = state.get('special_features_profile', {})
    require(profile.get('phase') == 'complete' and profile.get('receipt_path') == str(PROFILE) and
            profile.get('receipt_sha256') == PROFILE_SHA and state.get('media_root_extension', {}).get('phase') == 'complete',
            'The receipted positive Extras profile or media-root extension changed.')


def validate_scope_snapshot(op, restriction, profile, snapshot, state):
    profile.validate_structure(op, snapshot, state)
    restriction.quiescent(snapshot)
    tables = snapshot['database']['tables']
    counts = {'sessions': 73, 'devices': 62, 'activity_entries': 163, 'items': 22, 'libraries': 4,
              'play_sessions': 26, 'user_item_data': 7, 'item_extra_resources': 4, 'extra_reserved_paths': 3,
              'client_playback_references': 0, 'encoding_jobs': 0}
    require(all(len(tables[name]) == count for name, count in counts.items()),
            'The current complete database population differs from the latest permission authority.')
    users = [row for row in tables['users'] if row.get('id') == B_USER]
    require(len(users) == 1 and type(users[0].get('management_revision')) is int and users[0]['management_revision'] == 5,
            'The latest B policy revision is not five.')


def new_binding(op):
    return {'marker': op.SCHEMA_27_SOURCE_MARKER, 'schema': 27, 'source': str(SOURCE),
            'source_manifest_sha256': MANIFEST_SHA, 'catalog_sha256': CATALOG_SHA, 'migration_27_sha256': MIGRATION_SHA}


def validate_state_delta(op, before, after, new_process, upgrade):
    allowed = {'phase', 'stage', 'binary_sha256', 'binary_identity', 'process', 'schema27_source',
               'upgrade', 'upgrade_history', 'notifications_upgrade'}
    require(set(after) == set(before) | {'notifications_upgrade'} and
            all(op.equal_json(value, after[key]) for key, value in before.items() if key not in allowed),
            'Final candidate state changed an unrelated or historical field.')
    installed = after.get('binary_identity')
    require(isinstance(installed, dict) and set(installed) == {'device', 'inode'} and
            all(type(value) is int and value > 0 for value in installed.values()) and installed != before.get('binary_identity') and
            installed == upgrade.get('installed_binary_identity') and upgrade.get('installed_binary_sha256') == BINARY_SHA,
            'Final state does not identify the newly installed binary inode.')
    require(upgrade.get('marker') == MARKER and upgrade.get('phase') == 'complete' and
            upgrade.get('old_process') == before['process'] and upgrade.get('new_process') == new_process and
            upgrade.get('from_sha256') == OLD_BINARY_SHA and upgrade.get('to_sha256') == BINARY_SHA and
            type(upgrade.get('from_schema')) is int and upgrade['from_schema'] == 27 and
            type(upgrade.get('to_schema')) is int and upgrade['to_schema'] == 27,
            'The appended receipt does not bind this exact same-schema binary replacement.')
    require(after.get('phase') == 'ready' and after.get('stage') == 'complete' and type(after.get('schema')) is int and after['schema'] == 27 and
            after.get('binary_sha256') == BINARY_SHA and after.get('process') == new_process and
            new_process != before['process'] and new_process.get('boot_id') == before['process']['boot_id'] and
            type(new_process.get('pid')) is int and new_process['pid'] > 1 and
            type(new_process.get('start_ticks')) is int and new_process['start_ticks'] > before['process']['start_ticks'] and
            after.get('schema27_source') == new_binding(op) and op.equal_json(after.get('upgrade'), upgrade) and
            op.equal_json(after.get('upgrade_history'), before.get('upgrade_history', []) + [upgrade]) and
            after.get('notifications_upgrade') == {'marker': MARKER, 'phase': 'complete',
                'evidence_directory': str(OUTPUT), 'publication': PUBLICATION, 'binary_sha256': BINARY_SHA},
            'Final candidate state lacks its exact new process, product binding or appended upgrade receipt.')


class ServiceFence:
    """Reserve a candidate stop and subsequent start before either is dispatched."""

    def __init__(self):
        self.stage, self.reserved = 'preflight', set()

    def approve(self, arguments, unit):
        if arguments and Path(str(arguments[0])).name == 'systemctl':
            require(arguments[0] == '/usr/bin/systemctl' and len(arguments) > 1,
                    'A noncanonical service command is forbidden.')
            if arguments[1] == 'show':
                return
            action = arguments[1]
            require(arguments == ['/usr/bin/systemctl', action, unit] and action in ('stop', 'start') and
                    self.stage == action + '_requested' and action not in self.reserved and
                    (action != 'start' or 'stop' in self.reserved),
                    'A service mutation is unowned, repeated, or out of order.')
            self.reserved.add(action)


class ScopedReadiness:
    """Own exactly two public GETs and retain every response under OUTPUT."""

    ROUTES = ('/readyz', '/emby/System/Info/Public')
    LIMIT = 2 << 20

    def __init__(self, run, candidate):
        self.run, self.state, self.requests = run, candidate, 0
        self.cookie = self.csrf = None

    def request(self, route):
        require(self.requests < len(self.ROUTES) and route == self.ROUTES[self.requests],
                'Readiness exceeded its exact two-GET route order.')
        require(self.run.op.verify_service(self.state) == self.state['process'],
                'The owned replacement changed before its readiness GET.')
        label = ('readyz', 'public-info')[self.requests]
        self.requests += 1
        self.run.save('http-' + label + '-intent.json',
                      {'method': 'GET', 'path': route, 'request': self.requests, 'authenticated': False})
        connection = http.client.HTTPConnection('127.0.0.1', 18198, timeout=15)
        try:
            connection.request('GET', route, None,
                               {'Accept': 'application/json', 'Origin': 'http://127.0.0.1:18196'})
            response = connection.getresponse()
            raw = response.read(self.LIMIT + 1)
            code, cookie = response.status, response.getheader('Set-Cookie')
        finally:
            connection.close()
        self.run.save('http-' + label + '-result.json', {'method': 'GET', 'path': route,
            'status': code, 'body_sha256': digest(raw), 'body_hex': raw.hex(), 'bytes': len(raw), 'cookie_present': cookie is not None})
        require(len(raw) <= self.LIMIT and code == 200 and cookie is None,
                'A public readiness response was oversized, unsuccessful or introduced a credential.')
        require(self.run.op.verify_service(self.state) == self.state['process'],
                'The owned replacement changed during its readiness GET.')
        return self.run.op.precise_json(raw)


class Upgrade:
    def __init__(self, args):
        self.args, self.op = args, None
        self.lock = self.root_fd = None
        self.state = self.original = self.state_raw = self.state_identity = None
        self.before = self.media = self.primary = self.source_proof = None
        self.fence, self.records = ServiceFence(), {}
        self.phase, self.root_identity, self.command_index = 'preflight', None, 0
        self.source_checks = []

    def command(self, arguments, text=None, environment=None, timeout=30):
        arguments = [str(value) for value in arguments]
        self.fence.approve(arguments, 'goby-client-m3e.service')
        executable = arguments[0] if arguments else ''
        if executable == '/usr/bin/systemctl':
            if arguments[1] == 'show':
                require(len(arguments) == 5 and arguments[2] in ('goby-client-m3e.service', PRIMARY_UNIT, CONTROLLER) and
                        arguments[3] == '--no-pager' and arguments[4].startswith('--property='),
                        'A service read escaped the declared units.')
        elif executable == '/usr/bin/ss':
            require(arguments == ['/usr/bin/ss', '-H', '-ltnp', 'sport = :18198'], 'A listener read escaped the candidate port.')
        elif arguments[:6] == ['/usr/sbin/runuser', '-u', 'goby', '--', '/usr/bin/test', '-r']:
            require(len(arguments) == 7 and text is None and Path(arguments[6]).is_relative_to(self.op.MEDIA_ROOT) and
                    '..' not in Path(arguments[6]).parts, 'A media readability check escaped the original fixture root.')
        else:
            require(arguments[:5] == ['/usr/sbin/runuser', '-u', 'postgres', '--', '/usr/lib/postgresql/17/bin/psql'] and
                    text is not None, 'A command escaped the pinned read-only PostgreSQL helper.')
            environment = dict(environment or ENV, PGOPTIONS='-c default_transaction_read_only=on')
        try:
            result = subprocess.run(arguments, input=text, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                    env=environment or ENV, timeout=min(timeout, 60), check=False)
        except (OSError, subprocess.TimeoutExpired):
            raise Failure('A bounded command failed or timed out; its mutation will not be retried.') from None
        self.command_index += 1
        if result.returncode:
            if self.root_fd is not None:
                self.save('command-failure-' + str(self.command_index) + '.json',
                          {'exit': result.returncode, 'stdout': result.stdout, 'stderr': result.stderr})
            raise Failure('A bounded command failed; review retained private command evidence.')
        return result.stdout.strip()

    def properties(self, unit, names):
        raw = self.command(['/usr/bin/systemctl', 'show', unit, '--no-pager', '--property=' + ','.join(names)])
        result = {}
        for line in raw.splitlines():
            name, separator, value = line.partition('=')
            require(separator and name not in result, 'Service properties contain a missing or duplicate field.')
            result[name] = value
        require(set(result) == set(names), 'Service properties omitted an expected field.')
        return result

    def primary_fact(self):
        names = ('MainPID', 'InvocationID', 'ActiveState', 'SubState', 'FragmentPath', 'DropInPaths',
                 'ControlGroup', 'User', 'Group', 'WorkingDirectory', 'Restart')
        props = self.properties(PRIMARY_UNIT, names)
        require(props['MainPID'] == str(PRIMARY_PROCESS['pid']) and props['InvocationID'] == PRIMARY_INVOCATION and
                props['ActiveState'] == 'active' and props['SubState'] == 'running' and
                props['ControlGroup'] == '/system.slice/' + PRIMARY_UNIT and props['User'] == props['Group'] == 'goby',
                'The currently observed primary process or invocation differs from the read-only pin.')
        require(self.op.process_identity(PRIMARY_PROCESS['pid']) == PRIMARY_PROCESS, 'The primary PID was reused or restarted.')
        process = Path('/proc') / str(PRIMARY_PROCESS['pid'])
        require(os.readlink(process / 'exe') == '/opt/goby-dev/goby' and process.stat().st_uid == 995 and
                digest((process / 'exe').read_bytes()) == OLD_BINARY_SHA and
                (process / 'cgroup').read_text().strip() == '0::/system.slice/' + PRIMARY_UNIT,
                'The primary executable or service membership changed.')
        files = {}
        for name, expected in PRIMARY_FILES.items():
            path = Path(name)
            protected(path, expected, modes=(0o600, 0o644, 0o755))
            files[name] = {'sha256': expected, 'identity': identity(path.lstat())}
        result = {'properties': props, 'process': self.op.process_identity(PRIMARY_PROCESS['pid']), 'files': files,
                  'cmdline_sha256': digest((process / 'cmdline').read_bytes()),
                  'environment_sha256': digest((process / 'environ').read_bytes())}
        require(result['process'] == PRIMARY_PROCESS and
                result['cmdline_sha256'] == 'c2c8b1f234839029e4dcd80ada8d765fa6d4ebcb21be2f4b72665945d9458d6e' and
                result['environment_sha256'] == 'cfa0a59530f5e302b8ea23bb011823d7e68126be05e8b19c6360e450cbee9186',
                'The primary command, environment, or process changed while being witnessed.')
        return result

    def product(self):
        op = self.op
        raw = protected(REPORT, REPORT_SHA)
        validate_product_report(op, op.precise_json(raw))
        validate_terminal(op.precise_json(protected(TERMINAL, TERMINAL_SHA)))
        current = self.properties(CONTROLLER, ('MainPID', 'InvocationID', 'Result', 'ExecMainStatus', 'ControlGroup', 'SubState'))
        require(current == {'MainPID': '0', 'InvocationID': CONTROLLER_INVOCATION, 'Result': 'success',
                            'ExecMainStatus': '0', 'ControlGroup': '', 'SubState': 'exited'},
                'The accepted full-run controller is no longer terminal with its original invocation.')
        op.verify_complete_product_source(SOURCE, MANIFEST_SHA)
        artifacts = op.verify_schema_upgrade_sources(BINARY, 27, SOURCE, MANIFEST_SHA,
                                                     schema27_catalog_sha256=CATALOG_SHA)
        content, binary_identity = op.verify_upgrade_input(BINARY, BINARY_SHA)
        require(len(content) == BINARY_BYTES, 'The accepted source44 binary size changed.')
        return {'source': str(SOURCE), 'manifest_sha256': MANIFEST_SHA, 'binary': BINARY_REPORT,
                'report_path': str(REPORT), 'report_sha256': REPORT_SHA, 'terminal_path': str(TERMINAL),
                'terminal_sha256': TERMINAL_SHA, 'publication': PUBLICATION,
                'schema_artifacts': artifacts, 'binary_identity': binary_identity}, content

    def check_root(self):
        if self.root_fd is not None:
            require(identity(os.fstat(self.root_fd))['inode'] == self.root_identity['inode'] and
                    identity(OUTPUT.lstat())['inode'] == self.root_identity['inode'] and
                    OUTPUT.lstat().st_dev == self.root_identity['device'] and OUTPUT.lstat().st_uid == OUTPUT.lstat().st_gid == 0 and
                    stat.S_IMODE(OUTPUT.lstat().st_mode) == 0o700 and not OUTPUT.is_symlink(),
                    'The owned evidence directory was replaced.')

    def save(self, name, value):
        self.check_root()
        require(re.fullmatch(r'[a-z0-9][a-z0-9.-]{0,95}', name), 'An evidence filename escaped the owned directory.')
        self.op.create(OUTPUT / name, value)
        raw = protected(OUTPUT / name)
        self.records[name] = {'path': str(OUTPUT / name), 'sha256': digest(raw)}
        return self.records[name]

    def check(self, live=True, product=False):
        self.check_root()
        for path, expected in self.source_checks:
            protected(path, expected)
        require(protected(self.op.STATE_FILE) == self.state_raw and identity(self.op.STATE_FILE.lstat()) == self.state_identity,
                'The candidate state changed outside this operation.')
        self.op.verify_fixture_directories(self.state)
        self.op.verify_database(self.state)
        if live:
            require(self.op.verify_service(self.state) == self.state['process'], 'The live candidate process changed.')
        require(self.primary_fact() == self.primary, 'The primary identity changed during candidate work.')
        require(self.op.equal_json(self.ext.media_witness(self.op, self.state), self.media), 'A receipted media group changed.')
        if product:
            current, _ = self.product()
            require(self.op.equal_json(current, self.source_proof), 'The accepted source44 inputs changed during this operation.')

    def write_state(self, next_state, phase):
        self.check_root()
        require(protected(self.op.STATE_FILE) == self.state_raw and identity(self.op.STATE_FILE.lstat()) == self.state_identity,
                'The state changed before atomic publication.')
        temporary = OUTPUT / ('state-' + phase + '.json')
        self.op.create(temporary, next_state)
        next_raw = protected(temporary)
        os.replace(temporary, self.op.STATE_FILE)
        self.op.sync(WORK)
        require(protected(self.op.STATE_FILE) == next_raw, 'The atomically published state differs.')
        self.state, self.state_raw = copy.deepcopy(next_state), next_raw
        self.state_identity = identity(self.op.STATE_FILE.lstat())

    def phase_record(self, phase):
        self.phase, self.fence.stage = phase, phase
        self.save(phase + '.json', {'marker': MARKER, 'phase': phase,
                  'at': dt.datetime.now(dt.timezone.utc).isoformat(), 'reserved_service_actions': sorted(self.fence.reserved)})
        next_state = copy.deepcopy(self.state)
        next_state['phase'], next_state['stage'] = 'upgrading', 'notifications_' + phase
        next_state['notifications_upgrade'] = {'marker': MARKER, 'phase': phase, 'evidence_directory': str(OUTPUT),
                                              'publication': PUBLICATION, 'binary_sha256': BINARY_SHA}
        self.write_state(next_state, phase)

    def snapshot(self, name, state=None):
        chosen = state or self.state
        value = self.op.preservation_snapshot(chosen, 27)
        validate_scope_snapshot(self.op, self.restriction, self.profile, value, chosen)
        self.restriction.compare_fixed_snapshot(self.op, self.before, value)
        if self.root_fd is not None:
            self.save(name, value)
        return value

    def start_candidate(self, installed_identity):
        # The historical start helper persists an upgrade state itself. Keep
        # all state publication under this operator's exact atomic writer.
        candidate = copy.deepcopy(self.state)
        candidate.update(binary_sha256=BINARY_SHA, binary_identity=installed_identity, process=None, start_pending=True)
        self.op.require_candidate_stopped()
        self.op.verify_database(self.state)
        require(self.op.verify_service(candidate) is None, 'The staged replacement is not stopped.')
        self.command(['/usr/bin/systemctl', 'start', self.op.UNIT], timeout=55)
        deadline = time.monotonic() + 50
        while time.monotonic() < deadline:
            if self.command(['/usr/bin/ss', '-H', '-ltnp', 'sport = :18198']):
                break
            require(self.op.properties().get('ActiveState') not in ('failed', 'inactive'),
                    'The owned replacement exited during startup.')
            time.sleep(0.5)
        observed = self.op.verify_service(candidate, allow_new=True)
        require(observed is not None, 'The owned replacement did not establish its process before the startup bound.')
        candidate.update(process=observed, start_pending=False)
        return candidate

    def prepare(self):
        require(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0 and os.environ.get('SSH_CONNECTION'),
                'Use authorized root SSH on test-env, forwarding its authentic connection to any owned controller.')
        require(Path(__file__).absolute() == TOOL / 'upgrade-client-notifications-source44.py',
                'The source44 operator must use its new fixed reviewed tool path.')
        protected(Path(__file__).absolute(), self.args.script_sha256)
        guard_path = TOOL / 'test-upgrade-client-notifications-source44.py'
        protected(guard_path, self.args.guard_sha256)
        require(self.args.guard_report == TOOL / 'guards-report.json', 'The guard report left the dedicated tool directory.')
        guard_raw = protected(self.args.guard_report, self.args.guard_report_sha256)
        for name, (path, expected) in HELPERS.items():
            setattr(self, name, load_helper(path, expected, name))
        op = self.op
        guards = op.precise_json(guard_raw)
        require(guards.get('suite') == 'client-notifications-source44-upgrade-guards' and guards.get('status') == 'passed' and
                guards.get('operator_sha256') == self.args.script_sha256 and guards.get('guard_sha256') == self.args.guard_sha256 and
                type(guards.get('test_count')) is int and guards['test_count'] >= 16 and
                all(type(guards.get(key)) is int and guards[key] == 0 for key in ('failures', 'errors', 'skips')),
                'The exact independent source44 operator guards have not passed.')
        self.source_checks = [(Path(__file__).absolute(), self.args.script_sha256), (guard_path, self.args.guard_sha256),
                              (self.args.guard_report, self.args.guard_report_sha256), *HELPERS.values(),
                              (BASELINE, BASELINE_SHA), (PERMISSION_REPORT, PERMISSION_REPORT_SHA), (PROFILE, PROFILE_SHA)]
        for path, expected in self.source_checks:
            protected(path, expected)
        require(op.WORK == WORK and op.UNIT == 'goby-client-m3e.service' and op.PORT == 18198 and
                op.ROLE == 'goby_client_m3e' and op.PG_PORT == 15432 and op.BINARY == Path('/opt/goby-client-m3e/goby'),
                'The pinned fixture helper targets another candidate.')
        op.command = self.command
        op.host_inputs(initial=False)
        self.lock = os.open(op.LOCK, os.O_RDONLY | os.O_NOFOLLOW)
        require(identity(os.fstat(self.lock)) == identity(op.regular(op.LOCK)), 'The candidate lock identity changed.')
        fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        require(not os.path.lexists(OUTPUT), 'This one-shot evidence root exists; review it without replay or adoption.')
        self.state_raw = protected(op.STATE_FILE, OLD_STATE_SHA)
        self.state_identity = identity(op.STATE_FILE.lstat())
        self.state = op.precise_json(self.state_raw)
        self.original = copy.deepcopy(self.state)
        validate_starting_state(op, self.state)
        candidate = self.properties(op.UNIT, ('MainPID', 'InvocationID', 'ActiveState', 'SubState'))
        require(candidate == {'MainPID': str(OLD_PROCESS['pid']), 'InvocationID': OLD_INVOCATION,
                              'ActiveState': 'active', 'SubState': 'running'}, 'The freshly observed candidate invocation changed.')
        require(op.verify_service(self.state) == OLD_PROCESS, 'The current candidate identity differs from its state.')
        op.verify_database(self.state)
        self.primary = self.primary_fact()
        permission = op.precise_json(protected(PERMISSION_REPORT, PERMISSION_REPORT_SHA))
        require(permission.get('result') == 'passed' and permission.get('permission_ui_acceptance') is True and
                permission.get('evidence', {}).get('after-full.json') == {'path': str(BASELINE), 'sha256': BASELINE_SHA},
                'The latest permission authority does not bind its accepted complete after snapshot.')
        baseline = op.precise_json(protected(BASELINE, BASELINE_SHA))
        validate_scope_snapshot(op, self.restriction, self.profile, baseline, self.state)
        self.before = op.preservation_snapshot(self.state, 27)
        validate_scope_snapshot(op, self.restriction, self.profile, self.before, self.state)
        self.restriction.compare_fixed_snapshot(op, baseline, self.before)
        self.media = self.ext.media_witness(op, self.state)
        self.source_proof, self.binary_content = self.product()
        self.check(product=True)

    def execute(self):
        op = self.op
        OUTPUT.mkdir(mode=0o700)
        self.root_fd = os.open(OUTPUT, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        self.root_identity = identity(os.fstat(self.root_fd))
        op.sync(WORK)
        self.save('intent.json', {'marker': MARKER, 'source': self.source_proof, 'old_state_sha256': OLD_STATE_SHA,
                  'baseline': {'path': str(BASELINE), 'sha256': BASELINE_SHA}, 'primary': self.primary,
                  'candidate_process': OLD_PROCESS, 'candidate_invocation': OLD_INVOCATION,
                  'tool_inputs': {str(path): expected for path, expected in self.source_checks},
                  'schema': 27, 'web_assets_replaced': False, 'automatic_retry': False, 'automatic_rollback': False})
        self.save('before-state.json', self.state_raw)
        self.save('before-full.json', self.before)
        self.save('media-before.json', self.media)
        self.save('rollback.bin', protected(op.BINARY, OLD_BINARY_SHA, modes=(0o755,)))
        require(digest(protected(OUTPUT / 'rollback.bin')) == OLD_BINARY_SHA, 'The retained old binary differs.')
        staged = OUTPUT / 'candidate.bin'
        op.create(staged, self.binary_content, 0o755)
        staged_identity = identity(staged.lstat())
        self.phase_record('staged')
        self.check(product=True)
        self.snapshot('pre-stop-full.json')
        self.phase_record('stop_requested')
        require(op.verify_service(self.state) == OLD_PROCESS, 'The candidate changed before stop dispatch.')
        self.command(['/usr/bin/systemctl', 'stop', op.UNIT], timeout=45)
        op.require_candidate_stopped()
        self.phase_record('stopped')
        self.check(live=False, product=True)
        self.snapshot('stopped-full.json')
        require(protected(op.BINARY, OLD_BINARY_SHA, modes=(0o755,)) == protected(OUTPUT / 'rollback.bin') and
                identity(staged.lstat()) == staged_identity and digest(protected(staged, modes=(0o755,))) == BINARY_SHA,
                'A retained or staged binary changed before replacement.')
        self.phase_record('replace_requested')
        op.require_candidate_stopped()
        require(op.identity(op.regular(op.BINARY, mode=0o755, limit=64 << 20)) == self.original['binary_identity'] and
                digest(protected(op.BINARY, modes=(0o755,))) == OLD_BINARY_SHA and
                identity(staged.lstat()) == staged_identity and digest(protected(staged, modes=(0o755,))) == BINARY_SHA,
                'The old or staged binary inode changed immediately before replacement.')
        require(op.BINARY.parent.stat().st_dev == staged.stat().st_dev, 'Atomic binary publication would cross filesystems.')
        os.replace(staged, op.BINARY)
        op.sync(op.BINARY.parent)
        protected(op.BINARY, BINARY_SHA, modes=(0o755,))
        installed_identity = op.identity(op.regular(op.BINARY, mode=0o755, limit=64 << 20))
        self.phase_record('replaced')
        self.snapshot('before-start-full.json')
        self.check(live=False, product=True)
        self.phase_record('start_requested')
        candidate = self.start_candidate(installed_identity)
        require(candidate['process'] != OLD_PROCESS, 'The replacement did not establish a new candidate process.')
        new_service = self.properties(op.UNIT, ('MainPID', 'InvocationID', 'ActiveState', 'SubState', 'ControlGroup'))
        require(new_service['MainPID'] == str(candidate['process']['pid']) and
                re.fullmatch('[0-9a-f]{32}', new_service['InvocationID']) and new_service['InvocationID'] != OLD_INVOCATION and
                new_service['ActiveState'] == 'active' and new_service['SubState'] == 'running' and
                new_service['ControlGroup'] == '/system.slice/' + op.UNIT,
                'The replacement lacks its own fresh active service invocation.')
        self.save('started.json', {'process': candidate['process'], 'binary_identity': installed_identity,
                  'binary_sha256': BINARY_SHA, 'service': new_service, 'reserved_service_actions': sorted(self.fence.reserved)})
        api = ScopedReadiness(self, candidate)
        require(api.request('/readyz') == {'Status': 'ready'}, 'The replacement did not become ready.')
        information = api.request('/emby/System/Info/Public')
        require(information.get('Id') == self.original['server_id'] and information.get('ProductName') == 'Goby' and
                information.get('Version') == '4.9.5.0' and api.requests == 2 and api.cookie is None and api.csrf is None,
                'The replacement changed its stable server or product identity.')
        self.after = self.snapshot('after-full.json', candidate)
        self.save('media-after.json', self.ext.media_witness(op, candidate))
        require(op.equal_json(self.ext.media_witness(op, candidate), self.media) and self.primary_fact() == self.primary and
                op.verify_service(candidate) == candidate['process'] and self.fence.reserved == {'stop', 'start'} and
                self.properties(op.UNIT, tuple(new_service)) == new_service,
                'Final candidate, media, primary or service-action preservation failed.')
        current, _ = self.product()
        require(op.equal_json(current, self.source_proof), 'The verified product changed after startup.')
        completed = {'marker': MARKER, 'id': MARKER, 'phase': 'complete', 'source': str(BINARY),
            'from_schema': 27, 'to_schema': 27, 'from_sha256': OLD_BINARY_SHA, 'to_sha256': BINARY_SHA,
            'old_process': OLD_PROCESS, 'new_process': candidate['process'], 'evidence_directory': str(OUTPUT),
            'new_service': new_service,
            'installed_binary_identity': installed_identity, 'installed_binary_sha256': BINARY_SHA,
            'rollback_binary': str(OUTPUT / 'rollback.bin'), 'publication': PUBLICATION,
            'schema_artifacts': {**self.source_proof['schema_artifacts'], 'operator': {'path': str(Path(__file__).absolute()),
                'sha256': self.args.script_sha256}, 'product_verification': {'report_path': str(REPORT), 'report_sha256': REPORT_SHA,
                'terminal_path': str(TERMINAL), 'terminal_sha256': TERMINAL_SHA, 'package_count': 25, 'test_count': 2002}},
            'preservation': op.preservation_summary(self.before), 'after_preservation': op.preservation_summary(self.after),
            'protocol_version': information.get('Version'), 'product_version': information.get('GobyVersion'),
            'completed_at': dt.datetime.now(dt.timezone.utc).isoformat()}
        final = copy.deepcopy(self.original)
        final.update(phase='ready', stage='complete', binary_sha256=BINARY_SHA, binary_identity=installed_identity,
                     process=candidate['process'], schema27_source=new_binding(op), upgrade=completed,
                     upgrade_history=self.original.get('upgrade_history', []) + [completed],
                     notifications_upgrade={'marker': MARKER, 'phase': 'complete', 'evidence_directory': str(OUTPUT),
                                            'publication': PUBLICATION, 'binary_sha256': BINARY_SHA})
        validate_state_delta(op, self.original, final, candidate['process'], completed)
        self.save('completed.json', completed)
        self.write_state(final, 'complete')
        require(op.verify_service(self.state) == self.state['process'] and self.primary_fact() == self.primary and
                self.properties(op.UNIT, tuple(new_service)) == new_service,
                'The final state does not identify the unchanged primary and running replacement.')
        self.phase = 'complete'
        self.save('report.json', {'marker': MARKER, 'status': 'passed', 'schema': 27, 'binary': BINARY_REPORT,
            'old_process': OLD_PROCESS, 'new_process': self.state['process'], 'state_sha256': digest(self.state_raw),
            'source_manifest_sha256': MANIFEST_SHA, 'publication': PUBLICATION, 'full_report_sha256': REPORT_SHA,
            'complete_rows_sequences_catalog_preserved': True, 'credentials_recovery_and_three_media_groups_preserved': True,
            'primary_identity_unchanged': True, 'http_requests': api.requests, 'web_assets_replaced': False,
            'service_actions': ['stop', 'start'], 'schema_migration_performed': False,
            'client_acceptance': False, 'automatic_rollback': False, 'automatic_retry': False, 'evidence': dict(self.records)})
        return {'status': 'passed', 'report': self.records['report.json'], 'state_sha256': digest(self.state_raw),
                'binary_sha256': BINARY_SHA, 'client_acceptance': False}

    def run(self):
        try:
            self.prepare()
            if self.args.mode == 'preflight':
                return {'status': 'preflight_passed', 'schema': 27, 'binary_sha256': BINARY_SHA,
                        'old_state_sha256': digest(self.state_raw), 'new_evidence_writes': 0, 'http_requests': 0,
                        'service_actions': [], 'client_acceptance': False}
            return self.execute()
        except BaseException as error:
            if self.root_fd is not None:
                failed_observation = 'not_available'
                try:
                    self.save('failure-current-full.json', self.op.preservation_snapshot(self.state, 27))
                    self.save('failure-service.json', self.properties(self.op.UNIT,
                              ('MainPID', 'InvocationID', 'ActiveState', 'SubState', 'ControlGroup')))
                    failed_observation = 'retained_without_mutation'
                except BaseException:
                    pass
                try:
                    self.save('failed.json', {'marker': MARKER, 'status': 'retained_for_review', 'phase': self.phase,
                        'failure_type': type(error).__name__, 'reason': str(error) if isinstance(error, Failure) else 'A pinned helper or system operation failed.',
                        'reserved_service_actions': sorted(self.fence.reserved), 'automatic_retry': False, 'automatic_rollback': False,
                        'failure_observation': failed_observation,
                        'last_state_sha256': digest(self.state_raw) if self.state_raw else None, 'evidence': dict(self.records)})
                except BaseException:
                    pass
            raise
        finally:
            if self.root_fd is not None:
                os.close(self.root_fd)
            if self.lock is not None:
                os.close(self.lock)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('preflight', 'deploy'))
    for name in ('script-sha256', 'guard-sha256', 'guard-report-sha256'):
        parser.add_argument('--' + name, required=True)
    parser.add_argument('--guard-report', type=Path, required=True)
    args = parser.parse_args()
    try:
        require(all(re.fullmatch('[0-9a-f]{64}', getattr(args, name)) for name in
                    ('script_sha256', 'guard_sha256', 'guard_report_sha256')), 'An explicit tool digest is invalid.')
        print(json.dumps(Upgrade(args).run(), sort_keys=True))
        return 0
    except BaseException as error:
        print(json.dumps({'status': 'retained_for_review', 'failure_type': type(error).__name__,
                          'automatic_retry': False, 'automatic_rollback': False}), file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
