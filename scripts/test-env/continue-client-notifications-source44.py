#!/usr/bin/env python3
"""Complete source44 only from the exact retained pre-stop v1 failure.

The v1 tool, output and sealed execution tree are immutable inputs. The durable
fixture is still notifications_staged: the rejected stop_requested filename
never published that next state or dispatched a service command. This operator
does not reset that state or resume the old CLI. It proves the complete known
control-field delta, uses a fresh evidence directory and preserves all old
business, media, credential, recovery and primary identities.

The frozen v1 module supplies unchanged product/process/positive-profile reads,
the service fence, bounded candidate startup and the scoped two-GET reader.
Its prepare, execute, run and artifact/state publication methods are never used.
This operator owns new canonical phase filenames and all state publication.
"""

from __future__ import annotations

import argparse
import copy
import datetime as dt
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import sys
import types


WORK = Path('/opt/goby-test/exec-work-m3e')
BASE_OPERATOR = WORK / 'client-notifications-source44-tool-01/upgrade-client-notifications-source44.py'
BASE_SHA = '92da604ba21390d4c861b47a3ff3d7a05e905a646ee3f894392b860ee4a705c1'
BASE_GUARD = BASE_OPERATOR.with_name('test-upgrade-client-notifications-source44.py')
BASE_GUARD_SHA = 'b4155ad60084c495c0deb58875bdc68813da287b602de53028ab1ab6259a7ed6'
BASE_GUARD_REPORT = BASE_OPERATOR.with_name('guards-report.json')
BASE_GUARD_REPORT_SHA = '7e11c575b6ba2c34b1f515f455090ac089860fcaa050e34e85d4ab0de8418128'
TOOL = WORK / 'client-notifications-source44-continuation-tool-01'
OUTPUT = WORK / 'client-notifications-source44-continuation-v1'
MARKER = 'goby-client-notifications-source44-continuation-v1'
FAILED_OUTPUT = WORK / 'client-notifications-source44-upgrade-v1'
FAILED_EXECUTION = WORK / 'client-notifications-source44-execution-01'
FAILED_STATE_SHA = '5d528680edffc64c4720d6024de0db3ad3bc0846eede6040302bef63941cc04a'
FAILED_STATE_IDENTITY = {'device': 2049, 'inode': 2433967, 'uid': 0, 'gid': 0, 'mode': 0o600,
                         'links': 1, 'bytes': 79167, 'mtime_ns': 1789186907616305861, 'ctime_ns': 1789186907616305861}
FAILED_REPORT_SHA = '5dec5eb91b562184ad82e48f711a52dbc1d40c0093fc402ab7606be6a68583c4'
FAILED_SERVICE_SHA = 'f460de31ad683a04121d1bd1fa44a207dc337c13b3fc30b28172f0761ab8edc2'
FAILED_INTENT_SHA = '1c62320f826624645a3df05d616b04369008b3475f0b6c455f136f154a029352'
FAILED_TERMINAL = FAILED_EXECUTION / 'terminal.json'
FAILED_TERMINAL_SHA = '33f875dc0aa69c00ab8928083f7f17da3deced3f33343d314bd10005a532ea12'
FAILED_UNIT = 'goby-client-notifications-source44-controller-v1.service'
FAILED_INVOCATION = '29ee7565d2d1487bab232c8b0b5b0780'
FAILED_OUTPUT_TREE_SHA = '324dd5683a5c9f18452b2985eb5330b2ce07bd38a24e72c11b5b96593f586ec5'
FAILED_EXECUTION_TREE_SHA = '9652fc7b15a3a7a899fbf8fb2e5658c46643fb6ee40859b68b2b120d2a9e8733'
FAILED_OUTPUT_NAMES = {'before-full.json', 'before-state.json', 'candidate.bin', 'failed.json',
    'failure-current-full.json', 'failure-service.json', 'intent.json', 'media-before.json',
    'pre-stop-full.json', 'rollback.bin', 'staged.json'}
FAILED_EXECUTION_NAMES = {'intent.json', 'started.txt', 'stderr', 'stdout', 'terminal.json'}
FAILED_SNAPSHOTS = {
    'before-full.json': '98202cd7ea8617a8f19be03265c91caa5bca8e7c17219230805d1265b676634c',
    'pre-stop-full.json': 'aeb96d0854f58514615f04257076eb5feeee48cf484ec249b8052101a0dd6945',
    'failure-current-full.json': '6bc1d0bd17ae55a3f3c7b9332b73db4f473ce64513d51b084fa29e95e90781b2',
}
FAILED_MEDIA_SHA = 'da593eb64e417260ee97653ea04526429332533b286fc16cda683560bf0ff476'
PHASES = ('staged', 'stop_requested', 'stopped', 'replace_requested', 'replaced', 'start_requested')


class Failure(Exception):
    """The exact continuation authority or an owned publication boundary failed."""


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
    require(isinstance(path, Path) and path.is_absolute() and '..' not in path.parts, 'An input path is not canonical.')
    for parent in reversed(path.parents):
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                'An input parent is not protected and root owned.')
    before = path.lstat()
    require(stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and before.st_nlink == 1 and
            stat.S_IMODE(before.st_mode) in modes and 0 <= before.st_size <= limit,
            'An input file changed ownership, mode, link count or size.')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as handle:
        require(identity(os.fstat(handle.fileno())) == identity(before), 'An input changed before reading.')
        raw = handle.read(limit + 1)
        require(identity(os.fstat(handle.fileno())) == identity(before), 'An input changed during reading.')
    require(identity(path.lstat()) == identity(before) and len(raw) == before.st_size and
            (expected is None or digest(raw) == expected), 'An input identity or exact digest changed.')
    return raw


def load_base():
    raw = protected(BASE_OPERATOR, BASE_SHA)
    module = types.ModuleType('frozen_source44_upgrade_helpers')
    module.__file__ = str(BASE_OPERATOR)
    exec(compile(raw, str(BASE_OPERATOR), 'exec'), module.__dict__)
    return module


def valid_evidence_name(name):
    require(isinstance(name, str) and re.fullmatch(r'[a-z0-9][a-z0-9.-]{0,95}', name) and
            name not in ('.', '..'), 'An evidence filename escaped the continuation directory.')
    return name


def origin_link(base):
    return {'failed_output': str(FAILED_OUTPUT), 'failed_output_tree_sha256': FAILED_OUTPUT_TREE_SHA,
            'failed_execution': str(FAILED_EXECUTION), 'failed_execution_tree_sha256': FAILED_EXECUTION_TREE_SHA,
            'failed_state_sha256': FAILED_STATE_SHA, 'original_state_sha256': base.OLD_STATE_SHA,
            'failed_report_sha256': FAILED_REPORT_SHA, 'failed_terminal_sha256': FAILED_TERMINAL_SHA,
            'failed_invocation': FAILED_INVOCATION, 'failed_operator_sha256': BASE_SHA,
            'durable_phase': 'staged', 'failed_memory_phase': 'stop_requested', 'dispatched_service_actions': []}


def validate_failed_state(base, op, failed, original):
    base.validate_starting_state(op, original)
    expected = copy.deepcopy(original)
    expected.update(phase='upgrading', stage='notifications_staged', notifications_upgrade={
        'marker': base.MARKER, 'phase': 'staged', 'evidence_directory': str(FAILED_OUTPUT),
        'publication': base.PUBLICATION, 'binary_sha256': base.BINARY_SHA})
    require(op.equal_json(failed, expected),
            'Current state differs from the exact receipted staged control-field delta; no reset or adoption is allowed.')


def validate_failure_receipts(base, failed_report, failure_service, terminal):
    require(failed_report.get('marker') == base.MARKER and failed_report.get('status') == 'retained_for_review' and
            failed_report.get('failure_type') == 'Failure' and failed_report.get('phase') == 'stop_requested' and
            failed_report.get('reason') == 'An evidence filename escaped the owned directory.' and
            failed_report.get('reserved_service_actions') == [] and failed_report.get('automatic_retry') is False and
            failed_report.get('automatic_rollback') is False and failed_report.get('last_state_sha256') == FAILED_STATE_SHA and
            failed_report.get('failure_observation') == 'retained_without_mutation',
            'The retained failure is not the exact known filename rejection before service dispatch.')
    expected_service = {'MainPID': str(base.OLD_PROCESS['pid']), 'InvocationID': base.OLD_INVOCATION,
                        'ActiveState': 'active', 'SubState': 'running', 'ControlGroup': '/system.slice/goby-client-m3e.service'}
    require(failure_service == expected_service, 'The failed attempt did not retain the original running candidate.')
    require(terminal.get('unit') == FAILED_UNIT and terminal.get('state') == {
                'MainPID': '0', 'Result': 'exit-code', 'ExecMainStatus': '1', 'ControlGroup': '',
                'SubState': 'failed', 'InvocationID': FAILED_INVOCATION} and
            terminal.get('recursive_cgroup_empty') is True and terminal.get('failed_report_sha256') == FAILED_REPORT_SHA and
            terminal.get('last_state_sha256') == FAILED_STATE_SHA and terminal.get('failure_phase') == 'stop_requested' and
            terminal.get('reserved_service_actions') == [], 'The original controller lacks its exact failed terminal boundary.')


def pinned_tree(root, names, expected_sha):
    before = identity(root.lstat())
    require(root.is_absolute() and root.parent == WORK and stat.S_ISDIR(root.lstat().st_mode) and
            before['uid'] == before['gid'] == 0 and before['mode'] == 0o700 and not root.is_symlink(),
            'A retained evidence tree root changed ownership.')
    members = list(root.iterdir())
    require(len(members) == len(names) and {entry.name for entry in members} == names,
            'A retained evidence tree changed its exact file membership.')
    files = {}
    for path in sorted(members):
        raw = protected(path, modes=(0o600, 0o755))
        files[path.name] = {'identity': identity(path.lstat()), 'sha256': digest(raw)}
    require(identity(root.lstat()) == before, 'The retained directory changed while it was witnessed.')
    result = {'root': before, 'files': files}
    require(digest(json.dumps(result, sort_keys=True, separators=(',', ':')).encode()) == expected_sha,
            'The complete retained tree differs in bytes or filesystem identity.')
    return result


def validate_final_state(base, op, original, final, new_process, receipt, link):
    allowed = {'phase', 'stage', 'binary_sha256', 'binary_identity', 'process', 'schema27_source',
               'upgrade', 'upgrade_history', 'notifications_upgrade'}
    require(set(final) == set(original) | {'notifications_upgrade'} and
            all(op.equal_json(value, final[key]) for key, value in original.items() if key not in allowed),
            'Continuation completion changed an unrelated original state or history field.')
    installed = final.get('binary_identity')
    require(isinstance(installed, dict) and set(installed) == {'device', 'inode'} and
            all(type(value) is int and value > 0 for value in installed.values()) and installed != original.get('binary_identity') and
            installed == receipt.get('installed_binary_identity') and receipt.get('installed_binary_sha256') == base.BINARY_SHA,
            'The completed continuation lacks its new installed binary identity.')
    require(new_process != original['process'] and new_process.get('boot_id') == original['process']['boot_id'] and
            type(new_process.get('pid')) is int and new_process['pid'] > 1 and
            type(new_process.get('start_ticks')) is int and new_process['start_ticks'] > original['process']['start_ticks'],
            'The completed continuation lacks a fresh candidate process.')
    require(receipt.get('marker') == MARKER and receipt.get('phase') == 'complete' and
            receipt.get('old_process') == original['process'] and receipt.get('new_process') == new_process and
            receipt.get('from_sha256') == base.OLD_BINARY_SHA and receipt.get('to_sha256') == base.BINARY_SHA and
            type(receipt.get('from_schema')) is int and receipt['from_schema'] == 27 and
            type(receipt.get('to_schema')) is int and receipt['to_schema'] == 27 and
            op.equal_json(receipt.get('continuation'), link) and op.equal_json(link, origin_link(base)),
            'The final receipt does not bind the exact failed origin and same-schema replacement.')
    require(final.get('phase') == 'ready' and final.get('stage') == 'complete' and
            type(final.get('schema')) is int and final['schema'] == 27 and final.get('binary_sha256') == base.BINARY_SHA and
            final.get('process') == new_process and final.get('schema27_source') == base.new_binding(op) and
            op.equal_json(final.get('upgrade'), receipt) and
            op.equal_json(final.get('upgrade_history'), original.get('upgrade_history', []) + [receipt]) and
            final.get('notifications_upgrade') == {'marker': MARKER, 'phase': 'complete', 'evidence_directory': str(OUTPUT),
                'publication': base.PUBLICATION, 'binary_sha256': base.BINARY_SHA, 'continuation': link},
            'Final state omitted the explicit continuation link or rewrote original upgrade history.')


class Continuation:
    def __init__(self, args, base):
        base.Upgrade.__init__(self, args)
        self.base, self.link = base, origin_link(base)
        self.failed_trees = self.failed_original = self.failed_intent = None

    def command(self, arguments, text=None, environment=None, timeout=30):
        values = [str(value) for value in arguments]
        if values[:3] == ['/usr/bin/systemctl', 'show', FAILED_UNIT]:
            require(len(values) == 5 and values[3] == '--no-pager' and values[4].startswith('--property=') and text is None,
                    'The retained controller observation escaped its fixed read-only form.')
            result = self.base.subprocess.run(values, text=True, stdout=self.base.subprocess.PIPE,
                stderr=self.base.subprocess.PIPE, env=self.base.ENV, timeout=min(timeout, 30), check=False)
            require(result.returncode == 0, 'The retained controller cannot be observed.')
            return result.stdout.strip()
        return self.base.Upgrade.command(self, arguments, text, environment, timeout)

    def properties(self, unit, names):
        return self.base.Upgrade.properties(self, unit, names)

    def primary_fact(self):
        return self.base.Upgrade.primary_fact(self)

    def product(self):
        return self.base.Upgrade.product(self)

    def snapshot(self, name, state=None):
        return self.base.Upgrade.snapshot(self, name, state)

    def start_candidate(self, installed_identity):
        return self.base.Upgrade.start_candidate(self, installed_identity)

    def check_root(self):
        if self.root_fd is not None:
            fd, live = identity(os.fstat(self.root_fd)), identity(OUTPUT.lstat())
            require(all(fd[key] == live[key] == self.root_identity[key] for key in ('device', 'inode', 'uid', 'gid', 'mode')) and
                    live['uid'] == live['gid'] == 0 and live['mode'] == 0o700 and not OUTPUT.is_symlink(),
                    'The new continuation evidence directory changed identity.')

    def save(self, name, value):
        self.check_root()
        valid_evidence_name(name)
        self.op.create(OUTPUT / name, value)
        raw = protected(OUTPUT / name)
        self.records[name] = {'path': str(OUTPUT / name), 'sha256': digest(raw)}
        return self.records[name]

    def write_state(self, next_state, phase):
        self.check_root()
        require(protected(self.op.STATE_FILE) == self.state_raw and identity(self.op.STATE_FILE.lstat()) == self.state_identity,
                'The candidate state changed before continuation publication.')
        name = valid_evidence_name('state-' + phase.replace('_', '-') + '.json')
        temporary = OUTPUT / name
        self.op.create(temporary, next_state)
        raw = protected(temporary)
        require(self.op.STATE_FILE.parent.stat().st_dev == temporary.stat().st_dev,
                'Atomic fixture-state publication would cross filesystems.')
        os.replace(temporary, self.op.STATE_FILE)
        self.op.sync(WORK)
        require(protected(self.op.STATE_FILE) == raw, 'The atomically published continuation state differs.')
        self.state, self.state_raw, self.state_identity = copy.deepcopy(next_state), raw, identity(self.op.STATE_FILE.lstat())

    def phase_record(self, phase):
        require(phase in PHASES, 'An undeclared continuation phase was requested.')
        name = valid_evidence_name(phase.replace('_', '-') + '.json')
        self.save(name, {'marker': MARKER, 'phase': phase, 'continuation': self.link,
            'at': dt.datetime.now(dt.timezone.utc).isoformat(), 'reserved_service_actions': sorted(self.fence.reserved)})
        next_state = copy.deepcopy(self.state)
        next_state.update(phase='upgrading', stage='notifications_continuation_' + phase,
            notifications_upgrade={'marker': MARKER, 'phase': phase, 'evidence_directory': str(OUTPUT),
                'publication': self.base.PUBLICATION, 'binary_sha256': self.base.BINARY_SHA, 'continuation': self.link})
        self.write_state(next_state, phase)
        # Neither the service fence nor the in-memory phase advances unless
        # both the legal journal name and exact atomic state publish succeed.
        self.phase, self.fence.stage = phase, phase

    def check_origin(self):
        actual = {'output': pinned_tree(FAILED_OUTPUT, FAILED_OUTPUT_NAMES, FAILED_OUTPUT_TREE_SHA),
                  'execution': pinned_tree(FAILED_EXECUTION, FAILED_EXECUTION_NAMES, FAILED_EXECUTION_TREE_SHA)}
        if self.failed_trees is not None:
            require(self.op.equal_json(actual, self.failed_trees), 'The immutable failure evidence changed during continuation.')
        for path, expected in ((BASE_OPERATOR, BASE_SHA), (BASE_GUARD, BASE_GUARD_SHA), (BASE_GUARD_REPORT, BASE_GUARD_REPORT_SHA)):
            protected(path, expected)
        terminal = self.properties(FAILED_UNIT, ('MainPID', 'Result', 'ExecMainStatus', 'ControlGroup', 'SubState', 'InvocationID'))
        require(terminal == {'MainPID': '0', 'Result': 'exit-code', 'ExecMainStatus': '1', 'ControlGroup': '',
                             'SubState': 'failed', 'InvocationID': FAILED_INVOCATION},
                'The consumed failed controller changed or became active.')
        return actual

    def check(self, live=True, product=False):
        self.base.Upgrade.check(self, live, product)
        self.check_origin()

    def prepare(self):
        base = self.base
        require(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0 and os.environ.get('SSH_CONNECTION'),
                'Run the continuation only through authorized root SSH on test-env.')
        require(Path(__file__).absolute() == TOOL / 'continue-client-notifications-source44.py',
                'The continuation must use its fresh reviewed tool path.')
        guard = TOOL / 'test-continue-client-notifications-source44.py'
        require(self.args.guard_report == TOOL / 'guards-report.json', 'The new guard report selected another tool directory.')
        for path, expected in ((Path(__file__).absolute(), self.args.script_sha256), (guard, self.args.guard_sha256),
                               (self.args.guard_report, self.args.guard_report_sha256)):
            protected(path, expected)
        for name, (path, expected) in base.HELPERS.items():
            setattr(self, name, base.load_helper(path, expected, 'continuation_' + name))
        op = self.op
        guards = op.precise_json(protected(self.args.guard_report, self.args.guard_report_sha256))
        require(guards.get('suite') == 'client-notifications-source44-continuation-guards' and guards.get('status') == 'passed' and
                guards.get('operator_sha256') == self.args.script_sha256 and guards.get('guard_sha256') == self.args.guard_sha256 and
                type(guards.get('test_count')) is int and guards['test_count'] >= 10 and
                all(type(guards.get(key)) is int and guards[key] == 0 for key in ('failures', 'errors', 'skips')),
                'The exact continuation guards, including full in-memory execution, have not passed.')
        self.source_checks = [(Path(__file__).absolute(), self.args.script_sha256), (guard, self.args.guard_sha256),
            (self.args.guard_report, self.args.guard_report_sha256), (BASE_OPERATOR, BASE_SHA), (BASE_GUARD, BASE_GUARD_SHA),
            (BASE_GUARD_REPORT, BASE_GUARD_REPORT_SHA), *base.HELPERS.values(),
            (base.BASELINE, base.BASELINE_SHA), (base.PERMISSION_REPORT, base.PERMISSION_REPORT_SHA), (base.PROFILE, base.PROFILE_SHA)]
        for path, expected in self.source_checks:
            protected(path, expected)
        require(op.WORK == WORK and op.UNIT == 'goby-client-m3e.service' and op.PORT == 18198 and
                op.ROLE == 'goby_client_m3e' and op.PG_PORT == 15432 and op.BINARY == Path('/opt/goby-client-m3e/goby'),
                'A helper targets another candidate or database.')
        op.command = self.command
        op.host_inputs(initial=False)
        self.lock = os.open(op.LOCK, os.O_RDONLY | os.O_NOFOLLOW)
        require(identity(os.fstat(self.lock)) == identity(op.regular(op.LOCK)), 'The candidate lock was replaced.')
        fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        require(not os.path.lexists(OUTPUT), 'This continuation output already exists; no replay or adoption is allowed.')
        self.failed_trees = self.check_origin()
        self.state_raw = protected(op.STATE_FILE, FAILED_STATE_SHA)
        self.state_identity = identity(op.STATE_FILE.lstat())
        require(self.state_identity == FAILED_STATE_IDENTITY, 'The exact durable failed-state file was replaced.')
        self.state = op.precise_json(self.state_raw)
        self.failed_original = copy.deepcopy(self.state)
        original_raw = protected(FAILED_OUTPUT / 'before-state.json', base.OLD_STATE_SHA)
        self.original = op.precise_json(original_raw)
        validate_failed_state(base, op, self.state, self.original)
        failure = op.precise_json(protected(FAILED_OUTPUT / 'failed.json', FAILED_REPORT_SHA))
        service = op.precise_json(protected(FAILED_OUTPUT / 'failure-service.json', FAILED_SERVICE_SHA))
        terminal = op.precise_json(protected(FAILED_TERMINAL, FAILED_TERMINAL_SHA))
        validate_failure_receipts(base, failure, service, terminal)
        self.failed_intent = op.precise_json(protected(FAILED_OUTPUT / 'intent.json', FAILED_INTENT_SHA))
        require(self.failed_intent.get('marker') == base.MARKER and self.failed_intent.get('old_state_sha256') == base.OLD_STATE_SHA and
                self.failed_intent.get('candidate_process') == base.OLD_PROCESS and self.failed_intent.get('candidate_invocation') == base.OLD_INVOCATION,
                'The failed intent does not bind the original running candidate.')
        current_service = self.properties(op.UNIT, tuple(service))
        require(current_service == service and op.verify_service(self.state) == base.OLD_PROCESS,
                'The actual candidate no longer matches the undispatched failure boundary.')
        op.verify_database(self.state)
        self.primary = self.primary_fact()
        require(op.equal_json(self.primary, self.failed_intent.get('primary')), 'The primary changed since the failed attempt.')
        baseline = op.precise_json(protected(base.BASELINE, base.BASELINE_SHA))
        base.validate_scope_snapshot(op, self.restriction, self.profile, baseline, self.original)
        for name, expected in FAILED_SNAPSHOTS.items():
            snapshot = op.precise_json(protected(FAILED_OUTPUT / name, expected))
            base.validate_scope_snapshot(op, self.restriction, self.profile, snapshot, self.original)
            self.restriction.compare_fixed_snapshot(op, baseline, snapshot)
        self.before = op.preservation_snapshot(self.state, 27)
        base.validate_scope_snapshot(op, self.restriction, self.profile, self.before, self.state)
        self.restriction.compare_fixed_snapshot(op, baseline, self.before)
        self.media = self.ext.media_witness(op, self.state)
        old_media = op.precise_json(protected(FAILED_OUTPUT / 'media-before.json', FAILED_MEDIA_SHA))
        require(op.equal_json(self.media, old_media), 'A media group changed after the failed attempt.')
        self.source_proof, self.binary_content = self.product()
        require(op.equal_json(self.source_proof, self.failed_intent.get('source')),
                'The verified source44 artifact chain changed after the failed attempt.')
        protected(FAILED_OUTPUT / 'candidate.bin', base.BINARY_SHA, modes=(0o755,))
        protected(FAILED_OUTPUT / 'rollback.bin', base.OLD_BINARY_SHA)
        self.check(product=True)

    def execute(self):
        op, base = self.op, self.base
        OUTPUT.mkdir(mode=0o700)
        self.root_fd = os.open(OUTPUT, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        self.root_identity = identity(os.fstat(self.root_fd))
        op.sync(WORK)
        self.save('intent.json', {'marker': MARKER, 'continuation': self.link, 'source': self.source_proof,
            'old_state_sha256': base.OLD_STATE_SHA, 'failed_state_sha256': FAILED_STATE_SHA, 'primary': self.primary,
            'tool_inputs': {str(path): expected for path, expected in self.source_checks},
            'automatic_retry': False, 'automatic_rollback': False, 'web_assets_replaced': False, 'schema': 27})
        self.save('original-before-state.json', protected(FAILED_OUTPUT / 'before-state.json', base.OLD_STATE_SHA))
        self.save('failed-state.json', self.state_raw)
        self.save('before-full.json', self.before)
        self.save('media-before.json', self.media)
        self.save('failure-trees-before.json', self.failed_trees)
        self.save('rollback.bin', protected(op.BINARY, base.OLD_BINARY_SHA, modes=(0o755,)))
        staged = OUTPUT / 'candidate.bin'
        op.create(staged, self.binary_content, 0o755)
        staged_identity = identity(staged.lstat())
        self.phase_record('staged')
        self.check(product=True)
        self.snapshot('pre-stop-full.json')
        self.phase_record('stop_requested')
        require(op.verify_service(self.state) == base.OLD_PROCESS, 'The candidate changed immediately before its owned stop.')
        self.command(['/usr/bin/systemctl', 'stop', op.UNIT], timeout=45)
        op.require_candidate_stopped()
        self.phase_record('stopped')
        self.check(live=False, product=True)
        self.snapshot('stopped-full.json')
        self.phase_record('replace_requested')
        op.require_candidate_stopped()
        require(op.identity(op.regular(op.BINARY, mode=0o755, limit=64 << 20)) == self.original['binary_identity'] and
                digest(protected(op.BINARY, modes=(0o755,))) == base.OLD_BINARY_SHA and
                identity(staged.lstat()) == staged_identity and digest(protected(staged, modes=(0o755,))) == base.BINARY_SHA,
                'A live or newly staged binary changed before replacement.')
        require(op.BINARY.parent.stat().st_dev == staged.stat().st_dev, 'Atomic binary publication would cross filesystems.')
        os.replace(staged, op.BINARY)
        op.sync(op.BINARY.parent)
        protected(op.BINARY, base.BINARY_SHA, modes=(0o755,))
        installed = op.identity(op.regular(op.BINARY, mode=0o755, limit=64 << 20))
        self.phase_record('replaced')
        self.snapshot('before-start-full.json')
        self.check(live=False, product=True)
        self.phase_record('start_requested')
        candidate = self.start_candidate(installed)
        new_service = self.properties(op.UNIT, ('MainPID', 'InvocationID', 'ActiveState', 'SubState', 'ControlGroup'))
        require(new_service['MainPID'] == str(candidate['process']['pid']) and
                re.fullmatch('[0-9a-f]{32}', new_service['InvocationID']) and new_service['InvocationID'] != base.OLD_INVOCATION and
                new_service['ActiveState'] == 'active' and new_service['SubState'] == 'running' and
                new_service['ControlGroup'] == '/system.slice/' + op.UNIT,
                'The replacement lacks its own active process invocation.')
        self.save('started.json', {'process': candidate['process'], 'service': new_service,
            'binary_identity': installed, 'binary_sha256': base.BINARY_SHA, 'service_actions': sorted(self.fence.reserved)})
        api = base.ScopedReadiness(self, candidate)
        require(api.request('/readyz') == {'Status': 'ready'}, 'The replacement did not become ready.')
        information = api.request('/emby/System/Info/Public')
        require(information.get('Id') == self.original['server_id'] and information.get('ProductName') == 'Goby' and
                information.get('Version') == '4.9.5.0' and api.requests == 2 and api.cookie is None and api.csrf is None,
                'The replacement changed its stable public identity or exceeded the two public GETs.')
        self.after = self.snapshot('after-full.json', candidate)
        self.save('media-after.json', self.ext.media_witness(op, candidate))
        trees_after = self.check_origin()
        self.save('failure-trees-after.json', trees_after)
        require(op.equal_json(self.ext.media_witness(op, candidate), self.media) and self.primary_fact() == self.primary and
                op.verify_service(candidate) == candidate['process'] and self.fence.reserved == {'stop', 'start'} and
                self.properties(op.UNIT, tuple(new_service)) == new_service,
                'Final candidate, primary, media or service-action continuity failed.')
        current, _ = self.product()
        require(op.equal_json(current, self.source_proof), 'The accepted source44 chain changed after startup.')
        receipt = {'marker': MARKER, 'id': MARKER, 'phase': 'complete', 'continuation': self.link,
            'source': str(base.BINARY), 'from_schema': 27, 'to_schema': 27,
            'from_sha256': base.OLD_BINARY_SHA, 'to_sha256': base.BINARY_SHA,
            'old_process': self.original['process'], 'new_process': candidate['process'], 'new_service': new_service,
            'installed_binary_identity': installed, 'installed_binary_sha256': base.BINARY_SHA,
            'evidence_directory': str(OUTPUT), 'rollback_binary': str(OUTPUT / 'rollback.bin'), 'publication': base.PUBLICATION,
            'schema_artifacts': {**self.source_proof['schema_artifacts'],
                'operator': {'path': str(Path(__file__).absolute()), 'sha256': self.args.script_sha256},
                'product_verification': {'report_path': str(base.REPORT), 'report_sha256': base.REPORT_SHA,
                    'terminal_path': str(base.TERMINAL), 'terminal_sha256': base.TERMINAL_SHA, 'package_count': 25, 'test_count': 2002}},
            'preservation': op.preservation_summary(self.before), 'after_preservation': op.preservation_summary(self.after),
            'protocol_version': information.get('Version'), 'product_version': information.get('GobyVersion'),
            'completed_at': dt.datetime.now(dt.timezone.utc).isoformat()}
        final = copy.deepcopy(self.original)
        final.update(phase='ready', stage='complete', binary_sha256=base.BINARY_SHA, binary_identity=installed,
            process=candidate['process'], schema27_source=base.new_binding(op), upgrade=receipt,
            upgrade_history=self.original.get('upgrade_history', []) + [receipt],
            notifications_upgrade={'marker': MARKER, 'phase': 'complete', 'evidence_directory': str(OUTPUT),
                'publication': base.PUBLICATION, 'binary_sha256': base.BINARY_SHA, 'continuation': self.link})
        validate_final_state(base, op, self.original, final, candidate['process'], receipt, self.link)
        self.save('completed.json', receipt)
        self.write_state(final, 'complete')
        require(op.verify_service(self.state) == self.state['process'] and self.primary_fact() == self.primary and
                self.properties(op.UNIT, tuple(new_service)) == new_service,
                'The completed state lost the unchanged primary or new candidate identity.')
        self.check_origin()
        self.phase = 'complete'
        self.save('report.json', {'marker': MARKER, 'status': 'passed', 'schema': 27, 'continuation': self.link,
            'binary': base.BINARY_REPORT, 'old_process': base.OLD_PROCESS, 'new_process': self.state['process'],
            'state_sha256': digest(self.state_raw), 'source_manifest_sha256': base.MANIFEST_SHA,
            'publication': base.PUBLICATION, 'full_report_sha256': base.REPORT_SHA,
            'complete_rows_sequences_catalog_preserved': True, 'credentials_recovery_and_three_media_groups_preserved': True,
            'primary_identity_unchanged': True, 'original_failure_trees_preserved': True,
            'http_requests': api.requests, 'service_actions': ['stop', 'start'], 'web_assets_replaced': False,
            'schema_migration_performed': False, 'client_acceptance': False,
            'automatic_retry': False, 'automatic_rollback': False, 'evidence': dict(self.records)})
        return {'status': 'passed', 'report': self.records['report.json'], 'state_sha256': digest(self.state_raw),
                'binary_sha256': base.BINARY_SHA, 'client_acceptance': False}

    def run(self):
        try:
            self.prepare()
            if self.args.mode == 'preflight':
                return {'status': 'preflight_passed', 'schema': 27, 'continuation': self.link,
                        'failed_state_sha256': digest(self.state_raw), 'binary_sha256': self.base.BINARY_SHA,
                        'new_evidence_writes': 0, 'http_requests': 0, 'service_actions': [], 'client_acceptance': False}
            return self.execute()
        except BaseException as error:
            if self.root_fd is not None:
                observed = False
                observed_state_sha = None
                try:
                    actual_state = protected(self.op.STATE_FILE)
                    self.save('failure-fixture-state.json', actual_state)
                    observed_state_sha = digest(actual_state)
                    self.save('failure-current-full.json', self.op.preservation_snapshot(self.state, 27))
                    self.save('failure-service.json', self.properties(self.op.UNIT,
                              ('MainPID', 'InvocationID', 'ActiveState', 'SubState', 'ControlGroup')))
                    self.save('failure-origin-trees.json', self.check_origin())
                    observed = True
                except BaseException:
                    pass
                try:
                    self.save('failed.json', {'marker': MARKER, 'status': 'retained_for_review', 'phase': self.phase,
                        'continuation': self.link, 'failure_type': type(error).__name__,
                        'reason': str(error) if isinstance(error, (Failure, self.base.Failure)) else 'An owned helper or system operation failed.',
                        'reserved_service_actions': sorted(self.fence.reserved), 'failure_observed': observed,
                        'last_acknowledged_state_sha256': digest(self.state_raw) if self.state_raw else None,
                        'observed_state_sha256': observed_state_sha,
                        'automatic_retry': False, 'automatic_rollback': False, 'evidence': dict(self.records)})
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
                    ('script_sha256', 'guard_sha256', 'guard_report_sha256')), 'An explicit tool hash is invalid.')
        print(json.dumps(Continuation(args, load_base()).run(), sort_keys=True))
        return 0
    except BaseException as error:
        print(json.dumps({'status': 'retained_for_review', 'failure_type': type(error).__name__,
                          'automatic_retry': False, 'automatic_rollback': False}), file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
