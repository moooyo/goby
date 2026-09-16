"""Check the frozen reader pool using real, bounded, zero-request stub children.

Future execution only: ssh test-env, root, python3 -I -B. Pass --pool PATH,
--pool-sha256 SHA, --reader PATH and --reader-sha256 SHA. Sources are the three
fixed filenames in private/source under
/opt/goby-test/native-scan-http-capacity-running-checks-20260916. The exclusive
output is the separate private/pool-checks directory in that same new check scope.

The pool's process creation, birth checks, ready/receipt reads, WNOWAIT, pipe
draining, group checks, signals and waitpid are real. Only the application and
anchor descriptions, source scope, and module-local Popen target are adapted.
No real reader main, network namespace entry, TCP, SQL or Goby operation runs.
The worker emits lifecycle fixtures, never synthetic upstream HTTP responses.
Any harness kill or reap is separately recorded and makes its case fail.
"""

import argparse
import base64
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import sys
import time
import types


CHECK = Path('/opt/goby-test/native-scan-http-capacity-running-pool-checks-20260916-r02/private')
SOURCES = CHECK / 'source'
SOURCE_PATHS = frozenset(SOURCES / name for name in
                         ('capacity-pool-checks.py', 'capacity-reader-pool.py', 'capacity-reader.py'))
OUTPUT = CHECK / 'pool-checks'
CASE_NAMES = ('zero_dispatch_pair', 'one_side_missing_receipt', 'worker_observes_cancel_first',
              'pool_forces_blocked_child', 'controller_namespace_description_drift', 'source_and_ready_rejection')
APP_PID, ANCHOR_PID = 2147483001, 2147483002


WORKER_SOURCE = r'''"""Bounded zero-request lifecycle fixture; never enter a namespace or open HTTP."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import resource
import signal
import stat
import sys
import time

signal.signal(signal.SIGALRM, signal.SIG_DFL)
signal.alarm(8)
FALLBACK_END = time.monotonic() + 7.0

def anchor():
    before = time.monotonic_ns()
    wall = time.time_ns()
    return {'monotonicBeforeNs': before, 'wallTimeNs': wall, 'monotonicAfterNs': time.monotonic_ns()}

def encoded(value):
    return (json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + '\n').encode()

def metadata(info):
    return {'device': info.st_dev, 'inode': info.st_ino, 'uid': info.st_uid, 'gid': info.st_gid,
        'mode': stat.S_IMODE(info.st_mode), 'bytes': info.st_size, 'links': info.st_nlink,
        'mtimeNs': info.st_mtime_ns, 'ctimeNs': info.st_ctime_ns}

def read(path):
    raw = path.read_bytes()
    return raw, {'path': str(path), 'sha256': hashlib.sha256(raw).hexdigest(),
        'bytes': len(raw), 'metadata': metadata(path.stat())}

def small(pin):
    return {key: pin[key] for key in ('path', 'sha256', 'bytes')}

def write(path, value):
    raw = encoded(value)
    pending = path.with_name(path.name + '.pending')
    fd = os.open(pending, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        os.fchmod(fd, 0o600)
        with os.fdopen(fd, 'wb', closefd=False) as stream:
            stream.write(raw)
            stream.flush()
            os.fsync(fd)
    finally:
        os.close(fd)
    if path.exists():
        raise RuntimeError('stub publication already exists')
    os.rename(pending, path)
    return small(read(path)[1])

def identity():
    root = Path('/proc') / str(os.getpid())
    fields = (root / 'stat').read_text().rsplit(') ', 1)[1].split()
    status = dict(line.split(':', 1) for line in (root / 'status').read_text().splitlines() if ':' in line)
    executable = (root / 'exe').stat()
    return {'pid': os.getpid(), 'startTicks': fields[19],
        'uids': [int(value) for value in status['Uid'].split()],
        'gids': [int(value) for value in status['Gid'].split()],
        'exe': os.readlink(root / 'exe'), 'executableDevice': executable.st_dev,
        'executableInode': executable.st_ino, 'cgroup': (root / 'cgroup').read_text().strip(),
        'networkNamespace': os.readlink(root / 'ns/net')}

def event(path):
    raw, pin = read(path)
    return {'event': json.loads(raw), 'source': pin}

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--input', type=Path, required=True)
    parser.add_argument('--input-sha256', required=True)
    parser.add_argument('--stub-mode', choices=('normal', 'cancel_first', 'fail_no_receipt', 'hang', 'bad_ready'), required=True)
    args = parser.parse_args()
    created = anchor()
    resource.setrlimit(resource.RLIMIT_AS, (128 << 20, 128 << 20))
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    resource.setrlimit(resource.RLIMIT_FSIZE, (2 << 20, 2 << 20))
    resource.setrlimit(resource.RLIMIT_CPU, (2, 2))
    if args.stub_mode == 'hang':
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
    raw, input_pin = read(args.input)
    if hashlib.sha256(raw).hexdigest() != args.input_sha256:
        return 121
    value = json.loads(raw)
    context = json.loads(Path(value['context']['path']).read_bytes())
    phase = args.input.parent
    output = phase / value['reader']
    output.mkdir(mode=0o700)
    os.chmod(output, 0o700)
    worker = identity()
    ready = {'kind': 'native-scan-http-capacity-reader-ready', 'version': 1, 'scope': value['scope'],
        'phase': value['phase'], 'reader': value['reader'], 'runId': value['runId'], 'input': input_pin,
        'worker': worker, 'parentPid': os.getppid() + (1 if args.stub_mode == 'bad_ready' else 0),
        'processGroup': os.getpgrp(), 'sessionId': os.getsid(0), 'anchor': anchor()}
    write_started = time.monotonic_ns()
    ready_pin = write(output / 'ready.json', ready)
    write_ns = time.monotonic_ns() - write_started
    start = cancel = None
    while time.monotonic() < FALLBACK_END:
        if args.stub_mode != 'cancel_first' and start is None and (phase / 'start.json').exists():
            start = event(phase / 'start.json')
            if args.stub_mode == 'fail_no_receipt':
                return 7
        if args.stub_mode != 'hang' and (phase / 'cancel.json').exists():
            cancel = event(phase / 'cancel.json')
            break
        time.sleep(0.01)
    if cancel is None:
        return 124
    status = 'cancelled_before_start' if start is None else 'stopped_by_controller'
    # This lifecycle stub performs no clock-domain measurement.
    final = {'kind': 'native-scan-http-capacity-reader-receipt', 'version': 1,
        **{key: value[key] for key in ('scope', 'phase', 'reader', 'taskId', 'requestId', 'runId', 'userId', 'libraryId')},
        'source': value['self'], 'context': value['context'], 'librarySide': 'A' if value['reader'] == 'left' else 'B',
        'input': input_pin, 'ready': ready_pin, 'worker': worker, 'parentPid': os.getppid(),
        'processGroup': os.getpgrp(), 'sessionId': os.getsid(0), 'start': start, 'cancel': cancel,
        'status': status, 'error': None, 'signalNumber': None, 'hostNamespaceRestored': True,
        'namespaceAtFinish': os.readlink('/proc/self/ns/net'), 'workerFunctionReturned': True,
        'workerRequestedExitCode': 0, 'allOwnedConnectionsClosed': True,
        'actualProcessExitRequiresParentWait': True, 'createdAnchor': created, 'finishedAnchor': anchor(),
        'clockDomainBefore': {'version': 1, 'available': False, 'code': 'clock_domain_unavailable'},
        'clockDomainAfter': {'version': 1, 'available': False, 'code': 'clock_domain_unavailable'},
        'evidenceBytesBeforeReceipt': ready_pin['bytes'], 'evidenceWriteNsBeforeReceipt': write_ns,
        'requests': [], 'dispatchedRequests': 0,
        'limits': {'requests': 60, 'windowSeconds': 120, 'absoluteHttpSeconds': 10,
            'minimumSpacingNs': 2000000000, 'bodyBytes': 512 << 10, 'headerBytes': 32 << 10,
            'wireBytes': 576 << 10, 'addressSpaceBytes': 128 << 20, 'outputBytes': 72 << 20},
        'transport': {'origin': context['origin'], 'method': 'GET', 'connection': 'close',
            'acceptEncoding': 'identity', 'retry': False, 'concurrentConnections': 1},
        'syntheticLifecycleStub': True, 'namespaceEntryPerformed': False, 'httpAttempts': 0}
    receipt = write(output / 'receipt.json', final)
    print(json.dumps({'kind': 'native-scan-http-capacity-reader-result', 'status': status, 'receipt': receipt,
        'dispatchedRequests': 0, 'signalNumberAfterReceipt': None, 'workerRequestedExitCode': 0}, sort_keys=True), flush=True)
    return 0

if __name__ == '__main__':
    sys.exit(main())
'''


def require(value, code):
    if not value:
        raise AssertionError(code)


def encoded(value):
    return (json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + '\n').encode()


def read_frozen(path, expected=None):
    require(path.is_absolute() and path in SOURCE_PATHS and path.resolve() == path, 'source_scope')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        require(stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and
                before.st_nlink == 1 and stat.S_IMODE(before.st_mode) == 0o600 and 0 < before.st_size <= 2 << 20,
                'source_metadata')
        raw = os.read(fd, (2 << 20) + 1)
        after = os.fstat(fd)
        require(len(raw) == before.st_size and all(getattr(before, key) == getattr(after, key) for key in
                ('st_dev', 'st_ino', 'st_uid', 'st_gid', 'st_mode', 'st_size', 'st_nlink', 'st_mtime_ns', 'st_ctime_ns')),
                'source_changed')
        pin = {'path': str(path), 'sha256': hashlib.sha256(raw).hexdigest(), 'bytes': len(raw)}
        require(expected is None or pin['sha256'] == expected, 'source_digest')
        return raw, pin
    finally:
        os.close(fd)


def write_new(path, raw):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        os.fchmod(fd, 0o600)
        with os.fdopen(fd, 'wb', closefd=False) as stream:
            stream.write(raw)
            stream.flush()
            os.fsync(fd)
    finally:
        os.close(fd)
    return {'path': str(path), 'sha256': hashlib.sha256(raw).hexdigest(), 'bytes': len(raw)}


def load(raw, path, name):
    module = types.ModuleType(name)
    module.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), module.__dict__)
    return module


class ProcessTrace:
    """Observe module-local calls and forward the real kernel operation once."""
    def __init__(self):
        self.children = {}
        self.redirects, self.waits, self.terminals, self.signals = [], [], [], []
        self.waitid_calls = 0

    def __getattr__(self, name):
        return getattr(os, name)

    def waitid(self, kind, pid, options):
        require(kind == os.P_PID and pid in self.children and
                options == os.WEXITED | os.WNOHANG | os.WNOWAIT, 'pool_waitid_not_wnowait')
        self.waitid_calls += 1
        result = os.waitid(kind, pid, options)
        if result is not None:
            self.terminals.append({'pid': pid, 'code': result.si_code, 'status': result.si_status,
                'wnowait': True, 'observedNs': time.monotonic_ns()})
        return result

    def waitpid(self, pid, options):
        require(options == 0 and pid in self.children and any(row['pid'] == pid for row in self.terminals) and
                not any(row['pid'] == pid for row in self.waits), 'pool_wait_without_unique_terminal')
        state = (Path('/proc') / str(pid) / 'stat').read_text().rsplit(') ', 1)[1].split()[0]
        require(state == 'Z', 'leader_not_reserved_until_wait')
        result = os.waitpid(pid, options)
        self.waits.append({'pid': pid, 'status': result[1], 'unreapedStateBeforeWait': state,
            'waitedNs': time.monotonic_ns()})
        return result

    def killpg(self, pgid, number):
        require(pgid in self.children and number in (signal.SIGTERM, signal.SIGKILL) and
                not any(row['pid'] == pgid for row in self.waits) and
                not any(row['pgid'] == pgid and row['signal'] == number for row in self.signals),
                'pool_signal_scope_or_duplicate')
        row = {'pgid': pgid, 'signal': number, 'attemptedNs': time.monotonic_ns(), 'submitted': False}
        self.signals.append(row)
        os.killpg(pgid, number)
        row['submitted'] = True


class Case:
    def __init__(self, name, pool_raw, reader_raw, pool_path, stub):
        self.name, self.stub = name, stub
        self.scope = OUTPUT / name
        self.scope.mkdir(mode=0o700)
        (self.scope / 'private').mkdir(mode=0o700)
        self.pool_module = load(pool_raw, pool_path, 'pool_lifecycle_' + name)
        module = self.pool_module
        module.E, module.F = self.scope, self.scope / 'synthetic-fixture'
        module.SOURCE = self.scope / 'private/capacity-reader.py'
        module.CONTEXT = self.scope / 'private/reader-context.json'
        self.reader_pin = write_new(module.SOURCE, reader_raw)
        require(self.reader_pin['sha256'] == module.READER_SHA256 and self.reader_pin['bytes'] == module.READER_BYTES,
                'reader_source_not_pool_pin')
        self.reader = load(reader_raw, module.SOURCE, 'reader_dependencies_' + name)
        self.reader.E, self.reader.F = module.E, module.F
        self.reader.SOURCE, self.reader.CONTEXT = module.SOURCE, module.CONTEXT
        real_identity = self.reader.process_identity
        self.controller = real_identity(os.getpid())
        host = os.readlink('/proc/1/ns/net')
        private = 'net:[999999999]' if host != 'net:[999999999]' else 'net:[999999998]'
        application = {**self.controller, 'pid': APP_PID, 'startTicks': '100', 'uids': [995] * 4, 'gids': [986] * 4,
            'exe': str(module.F / 'goby'), 'executableDevice': 1, 'executableInode': 101,
            'cgroup': '0::/system.slice/synthetic-application.service', 'networkNamespace': private}
        anchor = {**self.controller, 'pid': ANCHOR_PID, 'startTicks': '101', 'exe': '/usr/bin/sleep',
            'executableDevice': 1, 'executableInode': 102,
            'cgroup': '0::/system.slice/synthetic-anchor.service', 'networkNamespace': private}
        self.drift = False

        def identity(pid):
            if pid == APP_PID:
                return copy.deepcopy(application)
            if pid == ANCHOR_PID:
                return copy.deepcopy(anchor)
            value = real_identity(pid)
            if pid == os.getpid() and self.drift:
                value['networkNamespace'] = private
            return value

        self.reader.process_identity = identity
        self.context = {'scope': str(module.E), 'fixtureRoot': str(module.F), 'anchorPid': ANCHOR_PID,
            'networkNamespace': private, 'bootId': Path('/proc/sys/kernel/random/boot_id').read_text().strip(),
            'anchor': {'process': anchor}}
        self.application = types.SimpleNamespace(record={'configurationAccepted': True, 'closed': False,
            'pid': APP_PID, 'process': application})
        self.trace = ProcessTrace()
        module.os = self.trace
        self.allocations = []
        self.pool = None
        self.fallback = []
        self.events = []

        def popen(argv, **kwargs):
            expected_prefix = ['/usr/bin/python3', '-I', '-B', str(module.SOURCE), '--input']
            require(argv[:5] == expected_prefix and len(argv) == 8 and argv[6] == '--input-sha256' and
                    kwargs.get('start_new_session') is True and kwargs.get('close_fds') is True and
                    kwargs.get('stdin') == subprocess.DEVNULL and kwargs.get('stdout') == subprocess.PIPE and
                    kwargs.get('stderr') == subprocess.PIPE and kwargs.get('cwd') == str(module.E / 'private'),
                    'popen_adapter_boundary')
            side = Path(argv[5]).name.split('-', 1)[0]
            require(side in ('left', 'right'), 'popen_input_side')
            mode = 'normal'
            if name == 'one_side_missing_receipt' and side == 'left': mode = 'fail_no_receipt'
            if name == 'worker_observes_cancel_first': mode = 'cancel_first'
            if name == 'pool_forces_blocked_child' and side == 'left': mode = 'hang'
            if name == 'source_and_ready_rejection' and side == 'left': mode = 'bad_ready'
            actual = [*argv[:3], str(stub), *argv[4:], '--stub-mode', mode]
            child = subprocess.Popen(actual, **kwargs)
            self.trace.children[child.pid] = {'process': child, 'birth': None}
            birth = module._stat_process(child.pid)
            require(birth['pid'] == birth['processGroup'] == birth['sessionId'] and birth['parentPid'] == os.getpid(),
                    'adapter_child_birth')
            self.trace.children[child.pid]['birth'] = birth
            self.trace.redirects.append({'pid': child.pid, 'requestedArgv': list(argv), 'actualStubArgv': actual,
                'stubMode': mode, 'stubSource': str(stub), 'side': side})
            return child

        module.subprocess = types.SimpleNamespace(Popen=popen, PIPE=subprocess.PIPE, DEVNULL=subprocess.DEVNULL)
        self.run = {'Id': '1' * 32, 'TaskId': '2' * 32, 'RequestId': '3' * 32, 'Source': 'manual', 'TotalChildren': 2}
        self.mappings = {role: {'libraryLabel': label, 'userId': digit * 32, 'libraryId': library * 32,
            'token': base64.urlsafe_b64encode(token * 32).decode().rstrip('=')}
            for role, label, digit, library, token in (('visible', 'A', '4', '6', b'L'), ('hidden', 'B', '5', '7', b'R'))}

    def construct(self, pin=None):
        return self.pool_module.ReaderPool(types.SimpleNamespace(), self.reader, self.context, self.application,
            reader_pin=self.reader_pin if pin is None else pin, deadline=time.monotonic() + 200,
            reserve=lambda value: self.allocations.append(value))

    def caught(self, operation):
        try:
            return operation()
        except self.pool_module.ReaderPoolError as error:
            self.events.append(str(error))
            return None

    def cleanup_fallback(self):
        for pid, item in self.trace.children.items():
            child = item['process']
            if child.returncode is not None:
                continue
            record = {'pid': pid, 'harnessIntervened': True, 'signalSubmitted': False, 'waited': False}
            self.fallback.append(record)
            try:
                current = self.pool_module._stat_process(pid)
                birth = item['birth']
                require(birth is not None and all(current[key] == birth[key] for key in
                        ('pid', 'parentPid', 'processGroup', 'sessionId', 'startTicks')), 'fallback_birth_changed')
                os.killpg(pid, signal.SIGKILL)
                record['signalSubmitted'] = True
            except (FileNotFoundError, ProcessLookupError):
                pass
            except BaseException as error:
                record['signalError'] = type(error).__name__
            # A failed birth observation cannot justify a guessed group kill.
            # Still wait through the stub's fixed eight-second self-exit cap.
            until = time.monotonic() + (2.0 if record['signalSubmitted'] else 9.0)
            while time.monotonic() < until:
                try:
                    waited, status = os.waitpid(pid, os.WNOHANG)
                except ChildProcessError:
                    record['waitOwnershipLost'] = True
                    break
                if waited:
                    child.returncode = os.waitstatus_to_exitcode(status)
                    record.update(waited=True, status=status)
                    break
                time.sleep(0.01)

    def exercise(self):
        started = time.monotonic()
        if self.name == 'source_and_ready_rejection':
            error = None
            try:
                self.construct({**self.reader_pin, 'sha256': '0' * 64})
            except self.pool_module.ReaderPoolError as rejected:
                error = str(rejected)
            require(error == 'reader_pool_source_scope' and not self.trace.children, 'source_binding_not_rejected_before_spawn')
        self.pool = self.construct()
        birth = self.caught(lambda: self.pool.start('cold', self.run, self.mappings))
        if self.name == 'source_and_ready_rejection':
            require(birth is None and 'reader_pool_ready_binding' in self.events, 'bad_ready_not_rejected')
        else:
            require(birth is not None and birth['startPublished'] is True, 'normal_pair_did_not_start')
        if self.name == 'one_side_missing_receipt':
            until = time.monotonic() + 4.0
            while self.pool.record['firstError'] is None and time.monotonic() < until:
                self.caught(self.pool.poll)
                time.sleep(0.01)
            require(self.pool.record['firstError'] is not None, 'missing_receipt_not_detected')
        if self.name == 'controller_namespace_description_drift':
            self.drift = True
            before_count = len(self.trace.children)
            value = self.caught(lambda: self.pool.start('cached', {**self.run, 'Id': '8' * 32, 'RequestId': '9' * 32}, self.mappings))
            require(value is None and self.events[-1] == 'reader_pool_controller_not_in_host_namespace' and
                    len(self.trace.children) == before_count, 'drift_allowed_new_start')
        self.caught(lambda: self.pool.finish('cold', time.monotonic() + 4.0))
        state = self.pool._phases['cold']
        require(state.get('joined') is True and self.pool.record['allChildrenJoined'] is True and
                len(state['workers']) == len(self.trace.children) == len(self.trace.waits) == 2,
                'pool_did_not_join_both_children')
        for worker in state['workers']:
            row = worker['record']
            require(not (row['wait']['exited'] and row['wait']['exitStatus'] == 124) and
                    not (row['wait']['signaled'] and row['wait']['signal'] == signal.SIGALRM),
                    'stub_self_timeout_is_not_pool_cleanup')
            require(row['terminalObservation']['leaderNotReaped'] is True and row['joined'] is True and
                    row['groupEmptyAfterWait'] is True and row['stdout']['eof'] is row['stderr']['eof'] is True and
                    row['wait']['pid'] == row['birth']['pid'] and
                    len([value for value in self.trace.waits if value['pid'] == row['birth']['pid']]) == 1,
                    'wait_or_pipe_lifecycle_incomplete')
        result = state['record']['result']
        normal = self.name in ('zero_dispatch_pair', 'worker_observes_cancel_first')
        require(result['status'] == ('passed' if normal else 'failed'), 'unexpected_pool_classification')
        if normal:
            require(result['actualRequests'] == 0 and result['actualCountsComplete'] is True and not self.trace.signals and
                    all(row['metricCompleteness'] == 'incomplete_no_requests' for row in result['workers']),
                    'zero_dispatch_result_changed')
        else:
            require(result['actualCountsComplete'] is False and result['unusedAllocationMayBeReleased'] is False,
                    'failure_allocation_was_released')
        if self.name == 'worker_observes_cancel_first':
            require(state['start'] is not None and all(worker['record']['readerReport']['start'] is None and
                    worker['record']['readerReport']['status'] == 'cancelled_before_start' for worker in state['workers']),
                    'worker_observed_start_before_cancel')
        if self.name == 'one_side_missing_receipt':
            left, right = state['workers']
            require(left['record']['wait']['exitStatus'] == 7 and 'readerReceipt' not in left['record'] and
                    right['record']['wait']['exitStatus'] == 0 and state['cancel']['event']['reason'] == 'controller_failure' and
                    not right['record']['forced'] and time.monotonic() - started < 4.0,
                    'peer_not_cancelled_and_joined_promptly')
        if self.name == 'pool_forces_blocked_child':
            left, right = state['workers']
            sent = [row['signal'] for row in left['record']['signals'] if row['submitted']]
            require(sent == [signal.SIGTERM, signal.SIGKILL] and left['record']['forced'] is True and
                    left['record']['wait']['signaled'] is True and left['record']['wait']['signal'] == signal.SIGKILL and
                    right['record']['wait']['exitStatus'] == 0 and not right['record']['signals'] and
                    time.monotonic() - started < 6.0, 'force_close_was_not_pool_owned')
        if self.name == 'controller_namespace_description_drift':
            require(self.pool.record.get('controllerNamespaceChangedDuringCleanup') is not None and
                    self.pool.record['controllerFailure'] is not None and not self.trace.signals,
                    'namespace_drift_did_not_cleanup_original_children')
        if self.name == 'source_and_ready_rejection':
            require(all(worker['record']['wait']['exitStatus'] == 0 and not worker['record']['forced']
                        for worker in state['workers']) and time.monotonic() - started < 4.0,
                    'bad_ready_did_not_cancel_both_children_promptly')
        snapshot = {'children': len(self.trace.children), 'waits': copy.deepcopy(self.trace.waits),
            'signals': copy.deepcopy(self.trace.signals), 'joinDeadline': state['joinDeadline']}
        again = self.pool.finish('cold', time.monotonic() + 100)
        require(again == result and snapshot == {'children': len(self.trace.children), 'waits': self.trace.waits,
            'signals': self.trace.signals, 'joinDeadline': state['joinDeadline']}, 'repeated_finish_refreshed_or_reaped_again')
        return {'poolStatus': result['status'], 'joined': True, 'children': 2, 'elapsedSeconds': time.monotonic() - started,
            'actualRequests': result['actualRequests'], 'repeatedFinishRaisedNoNewFailure': True, 'joinDeadlineUnchanged': True}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--pool', type=Path, required=True)
    parser.add_argument('--pool-sha256', required=True)
    parser.add_argument('--reader', type=Path, required=True)
    parser.add_argument('--reader-sha256', required=True)
    args = parser.parse_args()
    require(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0 and sys.flags.isolated and
            sys.flags.dont_write_bytecode and 'SSH_CONNECTION' in os.environ and
            all(re.fullmatch('[0-9a-f]{64}', value) for value in (args.pool_sha256, args.reader_sha256)),
            'fixed_remote_root_actor')
    require(args.pool == SOURCES / 'capacity-reader-pool.py' and args.reader == SOURCES / 'capacity-reader.py' and
            Path(__file__) == SOURCES / 'capacity-pool-checks.py', 'fixed_source_selection')
    os.umask(0o077)
    info = CHECK.lstat()
    require(CHECK.resolve() == CHECK and stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and
            stat.S_IMODE(info.st_mode) == 0o700 and not os.path.lexists(OUTPUT), 'exclusive_check_scope')
    _, self_pin = read_frozen(Path(__file__))
    pool_raw, pool_pin = read_frozen(args.pool, args.pool_sha256)
    reader_raw, reader_pin = read_frozen(args.reader, args.reader_sha256)
    OUTPUT.mkdir(mode=0o700)
    os.chmod(OUTPUT, 0o700)
    stub = OUTPUT / 'zero-request-worker.py'
    stub_pin = write_new(stub, WORKER_SOURCE.encode())
    results = []
    for name in CASE_NAMES:
        case = None
        result = {'name': name, 'passed': False}
        try:
            case = Case(name, pool_raw, reader_raw, args.pool, stub)
            result.update(case.exercise(), passed=True)
        except BaseException as error:
            result['errorType'] = type(error).__name__
            result['errorCode'] = str(error) if re.fullmatch('[A-Za-z0-9_.-]{1,160}', str(error)) else type(error).__name__
        finally:
            if case is not None:
                case.cleanup_fallback()
                if case.fallback:
                    result.update(passed=False, harnessCleanupIntervened=True)
                evidence = {'case': result, 'pool': case.pool.record if case.pool is not None else None,
                    'redirects': case.trace.redirects, 'terminalObservations': case.trace.terminals,
                    'realWaitpidCalls': case.trace.waits, 'realSignalCalls': case.trace.signals,
                    'waitidCallCount': case.trace.waitid_calls, 'harnessFallback': case.fallback, 'caughtErrors': case.events}
                result['evidence'] = write_new(case.scope / 'private/check-result.json', encoded(evidence))
        results.append(result)
    report = {'kind': 'native-capacity-pool-process-checks', 'version': 1, 'scope': str(OUTPUT),
        'status': 'passed' if all(row['passed'] for row in results) else 'failed', 'caseCount': len(results), 'cases': results,
        'operator': self_pin, 'poolSource': pool_pin, 'readerDependencySource': reader_pin, 'actualStubSource': stub_pin,
        'boundaries': ['Pool and reader E/F/SOURCE/CONTEXT rebound to a new private case tree.',
            'Only application/anchor identities are synthetic; controller and child /proc identities are real.',
            'Namespace drift changes only the controller descriptor returned by the reader dependency.',
            'Reader pure context/input/anchor parsers remain real; no reader main or namespace operation executes.',
            'Module-local Popen redirects the recorded reader target to the recorded fixed stub argv.',
            'Module-local os observers forward actual waitid(WNOWAIT), waitpid and killpg without changing pool algorithms.',
            'Ready and zero-request final receipts are explicit lifecycle fixtures, not upstream HTTP responses.',
            'Cancel-first means the worker observes cancel before reading start; the parent still publishes start.',
            'Stub fallback is exit 124 after seven seconds or SIGALRM at eight seconds; neither counts as pool forced-close success.',
            'Any harness kill or reap fails its case, even when the final process group is empty.'],
        'realReaderMainExecuted': False, 'realNamespaceEntryPerformed': False, 'actualHttpRequests': 0,
        'gobyCommands': 0, 'postgresCommands': 0, 'systemctlCommands': 0, 'sqlConnections': 0, 'businessAccepted': False}
    receipt = write_new(OUTPUT / 'receipt.json', encoded(report))
    print(json.dumps({'status': report['status'], 'caseCount': len(results), 'receipt': receipt}, sort_keys=True))
    return 0 if report['status'] == 'passed' else 1


if __name__ == '__main__':
    sys.exit(main())
