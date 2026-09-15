"""Run one frozen native scan/HTTP capacity profile on test-env.

This fixed controller has no retry or resume mode. Its reviewed input, source
set, original anchor lifetime and private evidence define one attempt. The
preparation, workload and closure ceilings remain 300/1200/900 seconds, within
2700 seconds of the original anchor start and its independent 3600-second cap.
No fixture may start until the separate execution decision binds every helper.

All result files remain root-private. The public stdout summary contains only
status, nonsecret error codes and a receipt descriptor. A successful controller
result remains pending independent measurement and preservation review.
"""

import argparse
import copy
import hashlib
import json
import math
import os
from pathlib import Path
import re
import resource
import secrets
import signal
import stat
import sys
import time
import types


E = Path('/opt/goby-test/native-scan-http-capacity-20260915')
F = Path('/opt/goby-native-scan-http-capacity-20260915')
P = E / 'private'
SELF = P / 'capacity-controller.py'
INPUT = P / 'input.json'
INTENT = P / 'execution-intent.json'
RESULT = E / 'execution.json'
RESERVE_FILE = P / 'cleanup-capture-reserve.bin'
OLD = Path('/opt/goby-test/m6-systemd-install-20260915-r04/private')
OLD_PREPARE = {'path': str(OLD / 'm6-systemd-install-prepare.py'),
               'sha256': '8cb9e46efdbfd76a3a2b05ecc5d40c43f74020ff66843cd63a94956a4a72f1c8', 'bytes': 61473}
OLD_INPUT = {'path': str(OLD / 'prepare-input.json'),
             'sha256': 'e3f7b37dabe4433df78403c7bf8984a14a1848227a42510c9e594b7d22852e89', 'bytes': 2844}
NAMES = {name: 'capacity-' + name + '.py' for name in (
    'controller', 'support', 'units', 'corpus', 'preparation', 'application',
    'reader', 'reader-pool', 'observers', 'transport', 'workload', 'closure')}
NAMES['journey'] = 'native-catalog-journey.py'
JOURNEY_SHA = '63125715ad415349f3ae95a008f56783800c7334d90efcbcb394a14215e695d4'
PROFILE = {'mediaLeaves': 1000, 'albumNfoFiles': 20, 'users': 3, 'credentials': 4,
           'applicationStarts': 1, 'taskAdmissions': 2, 'readersPerPhase': 2,
           'maximumReaderProcesses': 4, 'readonlyWorkloadSqlSlots': 6,
           'setupSqlConnections': 3, 'httpRequests': 600, 'rawEvidenceBytes': 384 << 20,
           'rootAllocationBytes': 2560 << 20, 'rootReserveBytes': 1 << 30,
           'prepareSeconds': 300, 'businessSeconds': 1200, 'closureSeconds': 900,
           'anchorSequenceSeconds': 2700, 'anchorMaximumSeconds': 3600}
RAW_LIMIT, ROOT_LIMIT, ROOT_RESERVE = 384 << 20, 2560 << 20, 1 << 30
READER_PAIR_BYTES, FUTURE_NONREADER_BYTES = 144 << 20, 128 << 20


class Rejected(RuntimeError):
    """A nonsecret fixed rejection code."""


class Deadline(BaseException):
    """Interrupt a bounded phase without converting it into successful work."""


def need(value, code):
    if not value:
        raise Rejected(code)


def code(error):
    value = str(error)
    return value if re.fullmatch('[A-Za-z0-9_.-]{1,160}', value) else type(error).__name__


def encoded(value):
    return (json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + '\n').encode()


def parse(raw):
    def unique(pairs):
        value = dict(pairs)
        need(len(value) == len(pairs), 'duplicate_json_member')
        return value
    def invalid(_value):
        raise Rejected('nonfinite_json_number')
    return json.loads(raw, object_pairs_hook=unique, parse_constant=invalid)


def metadata(path):
    value = path.lstat()
    return {'device': value.st_dev, 'inode': value.st_ino, 'uid': value.st_uid,
            'gid': value.st_gid, 'mode': stat.S_IMODE(value.st_mode),
            'type': stat.S_IFMT(value.st_mode), 'bytes': value.st_size,
            'links': value.st_nlink, 'mtimeNs': value.st_mtime_ns, 'ctimeNs': value.st_ctime_ns}


def private_directory(path):
    row = metadata(path)
    need(path.resolve() == path and row['type'] == stat.S_IFDIR and row['uid'] == row['gid'] == 0 and
         row['mode'] == 0o700, 'private_directory_identity')


def pin(path, expected=None, maximum=16 << 20):
    need(path.is_absolute() and path.resolve() == path, 'pinned_path')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        need(stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and
             before.st_nlink == 1 and stat.S_IMODE(before.st_mode) == 0o600 and
             0 < before.st_size <= maximum, 'pinned_file_metadata')
        parts, remaining = [], before.st_size
        while remaining:
            part = os.read(fd, min(remaining, 1 << 20))
            need(part, 'pinned_file_incomplete')
            parts.append(part)
            remaining -= len(part)
        after = os.fstat(fd)
        need(all(getattr(before, key) == getattr(after, key) for key in
                 ('st_dev', 'st_ino', 'st_mode', 'st_uid', 'st_gid', 'st_nlink', 'st_size', 'st_mtime_ns', 'st_ctime_ns')),
             'pinned_file_changed')
    finally:
        os.close(fd)
    raw = b''.join(parts)
    result = {'path': str(path), 'sha256': hashlib.sha256(raw).hexdigest(), 'bytes': len(raw)}
    need(expected is None or expected == result, 'pinned_descriptor_changed')
    return raw, result


def save(path, value):
    raw = encoded(value)
    need(path.is_relative_to(E) and path.parent.resolve() == path.parent and len(raw) <= 16 << 20,
         'controller_receipt_scope_or_size')
    with path.open('xb') as stream:
        os.fchmod(stream.fileno(), 0o600)
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    return pin(path, maximum=16 << 20)[1]


def load(descriptor, path, name):
    raw, _pin = pin(path, descriptor, 2 << 20)
    module = types.ModuleType('native_capacity_' + name.replace('-', '_'))
    module.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), module.__dict__)
    return module


def input_value(checksum):
    raw, input_pin = pin(INPUT)
    need(input_pin['sha256'] == checksum, 'input_digest_changed')
    value = parse(raw)
    need(type(value) is dict and set(value) == {'kind', 'version', 'scope', 'helpers', 'baselineInput',
         'referencePreparation', 'profile', 'decision'} and value['kind'] == 'native-scan-http-capacity-input' and
         type(value['version']) is int and value['version'] == 1 and value['scope'] == str(E) and
         value['profile'] == PROFILE and value['baselineInput'] == OLD_INPUT and
         value['referencePreparation'] == OLD_PREPARE and set(value['helpers']) == set(NAMES), 'fixed_input_contract')
    for role, filename in NAMES.items():
        pin(P / filename, value['helpers'][role], 2 << 20)
    need(value['helpers']['journey']['sha256'] == JOURNEY_SHA, 'fixed_journey_source')
    decision_raw, _ = pin(P / 'execution-decision.json', value['decision'])
    decision = parse(decision_raw)
    source_set_sha = hashlib.sha256(encoded(value['helpers'])).hexdigest()
    need(decision['kind'] == 'native-scan-http-capacity-execution-decision' and decision['version'] == 1 and
         decision['scope'] == str(E) and decision['status'] == 'approved_for_one_bounded_native_capacity_attempt' and
         decision['sourceSetSha256'] == source_set_sha and decision['profile'] == PROFILE and
         decision['previousInputsReplayed'] is False and decision['capacityAccepted'] is False,
         'execution_decision_binding')
    return value, input_pin


def usage(root, exclusions=()):
    """Count owned allocation from metadata only; never open state/key files."""
    for attempt in range(3):
        try:
            return _usage_once(root, exclusions)
        except (FileNotFoundError, ProcessLookupError):
            # Ready and registry publication use same-directory atomic rename.
            # Retry the bounded observation instead of treating a moved name
            # as a complete zero-byte observation.
            if attempt == 2:
                raise Rejected('owned_usage_changed_during_observation') from None


def _usage_once(root, exclusions):
    result = {'bytes': 0, 'allocatedBytes': 0, 'files': 0, 'directories': 0}
    if not os.path.lexists(root):
        return result
    pending = [root]
    while pending:
        path = pending.pop()
        if any(path == item or path.is_relative_to(item) for item in exclusions):
            continue
        row = path.lstat()
        need(not stat.S_ISLNK(row.st_mode), 'owned_usage_symlink')
        result['allocatedBytes'] += row.st_blocks * 512
        if stat.S_ISDIR(row.st_mode):
            result['directories'] += 1
            need(result['directories'] <= 4096, 'owned_usage_directory_bound')
            pending.extend(path.iterdir())
        else:
            need(stat.S_ISREG(row.st_mode), 'owned_usage_special_file')
            result['files'] += 1
            result['bytes'] += row.st_size
            need(result['files'] <= 30000, 'owned_usage_file_bound')
    return result


class Controller:
    def __init__(self, value, input_pin, modules):
        self.value, self.input_pin, self.m = value, input_pin, modules
        self.support = modules['support']
        self.base = self.support.load_base()
        self.original_run = self.base.run
        self.base.E, self.base.ENV = E, dict(self.support.BASE_ENV)
        self.base.run = self.command
        self.lock = None
        self.ctx = {}
        self.prepared = {'kind': 'native-scan-http-capacity-preparation', 'version': 1, 'scope': str(E),
                         'status': 'not_started', 'commands': [], 'ownedUnits': {}, 'createdPaths': []}
        self.runtime = {'kind': 'native-scan-http-capacity-runtime', 'version': 1, 'scope': str(E),
                        'commands': [], 'ownedUnits': {}, 'createdPaths': []}
        self.close_commands = {'kind': 'native-scan-http-capacity-close-commands', 'version': 1, 'scope': str(E),
                               'commands': [], 'ownedUnits': {}, 'createdPaths': []}
        self.record = {'kind': 'native-scan-http-capacity-execution', 'version': 1, 'scope': str(E),
                       'input': input_pin, 'profile': PROFILE, 'status': 'admitted_not_started',
                       'failures': [], 'componentReceipts': {}, 'lockReleased': False,
                       'capacityAccepted': False, 'wholeM2M6Accepted': False, 'sourceProcess': os.getpid()}
        self.actor = self.app = self.observers = self.pool = self.transport = self.workload = self.closer = None
        self.workload_result = self.workload_cleanup = self.final_validation = None
        self.started = time.monotonic()
        self.phase = 'admission'
        self.phase_deadline = self.started + 120
        self.business_deadline = self.closure_ceiling = self.closure_deadline = None
        self.host = {}
        self.reserve_identity = None
        self.event_serial = 0
        self.allocations = {}
        self.reader_starts = {}
        self.metric_samples_stopped = False
        self.raw_before_closure = None
        self.mutation_admitted = False
        for name in ('net', 'mnt'):
            target = '/proc/self/ns/' + name
            fd = os.open(target, os.O_RDONLY | os.O_CLOEXEC)
            self.host[name] = {'fd': fd, 'link': os.readlink(target), 'inode': os.fstat(fd).st_ino}
            need(self.host[name]['link'] == os.readlink('/proc/1/ns/' + name), 'controller_not_in_host_namespaces')
        self.boot = Path('/proc/sys/kernel/random/boot_id').read_text().strip()

    def fail(self, stage, error):
        item = {'stage': stage, 'code': code(error), 'type': type(error).__name__}
        if not self.record['failures']:
            self.record['firstFailure'] = item
        self.record['failures'].append(item)
        self.record['status'] = 'failed_pending_owned_closure'

    def set_commands(self, directory, report):
        need(directory.parent == P and not os.path.lexists(directory), 'command_directory_consumed')
        directory.mkdir(mode=0o700)
        self.base.P, self.base.serial, self.base.report = directory, 0, report
        self.base.run = self.command

    def set_deadline(self, phase, deadline):
        need(type(deadline) in (int, float) and math.isfinite(deadline) and deadline > time.monotonic(), 'controller_phase_deadline')
        self.phase, self.phase_deadline = phase, deadline
        signal.setitimer(signal.ITIMER_REAL, max(0.001, deadline - time.monotonic()))

    def command(self, label, argv, timeout=30):
        remaining = self.phase_deadline - time.monotonic()
        need(remaining > 11, 'command_failure_closeout_reserve')
        return self.original_run(label, argv, min(float(timeout), remaining - 11))

    def restore_host(self):
        need(Path('/proc/sys/kernel/random/boot_id').read_text().strip() == self.boot, 'controller_boot_changed')
        for name in ('mnt', 'net'):
            saved = self.host[name]
            need(os.fstat(saved['fd']).st_ino == saved['inode'] and
                 os.readlink('/proc/1/ns/' + name) == saved['link'], 'saved_host_namespace_changed')
            if os.readlink('/proc/self/ns/' + name) != saved['link']:
                os.setns(saved['fd'], 0)
                self.record.setdefault('hostNamespaceRestorations', []).append(name)
            need(os.readlink('/proc/self/ns/' + name) == saved['link'], 'host_namespace_not_restored')

    def raw_bytes(self):
        total = usage(P, (RESERVE_FILE, P / 'retained'))['bytes']
        total += usage(F / 'log')['bytes']
        return total

    def check_storage(self, additional=0, future=0):
        raw = self.raw_bytes()
        need(raw + additional + future <= RAW_LIMIT, 'controller_raw_evidence_reserve')
        allocated = usage(E)['allocatedBytes'] + usage(F, (F / 'pg',))['allocatedBytes']
        need(allocated + additional <= ROOT_LIMIT, 'controller_root_allocation_limit')
        fs = os.statvfs(E)
        need(fs.f_bavail * fs.f_frsize >= ROOT_RESERVE + additional, 'controller_root_free_reserve')
        return {'rawEvidenceBytes': raw, 'allocatedBytes': allocated,
                'rootFreeBytes': fs.f_bavail * fs.f_frsize}

    def reserve(self, event):
        need(type(event) is dict, 'reservation_event_shape')
        if event.get('kind') == 'native-capacity-reader-reservation':
            phase = event['phase']
            need(phase in ('cold', 'cached') and phase not in self.allocations and event['workers'] == 2 and
                 event['httpRequests'] == 120 and event['outputBytes'] == READER_PAIR_BYTES,
                 'reader_allocation_contract')
            need(self.phase == 'business' and self.business_deadline - time.monotonic() >= 135,
                 'reader_window_and_join_reserve')
            facts = self.check_storage(READER_PAIR_BYTES, FUTURE_NONREADER_BYTES)
            self.allocations[phase] = {'maximum': dict(event), 'admitted': facts, 'settled': False}
            return
        need(event.get('event') == 'capacity-http-reserve' and
             type(event.get('maximumAdditionalBytes')) is int and
             0 < event['maximumAdditionalBytes'] <= (2 << 20) + (128 << 10) + 2,
             'transport_reservation_contract')
        future = 0 if event.get('cleanup') else 64 << 20
        phase = event.get('phase')
        # This guard runs before the phase's first task POST, not only when
        # its readers are subsequently launched.
        if not event.get('cleanup') and phase in ('cold', 'cached') and phase not in self.allocations:
            future = READER_PAIR_BYTES + FUTURE_NONREADER_BYTES
        self.check_storage(event['maximumAdditionalBytes'], future)

    def event(self, value):
        self.event_serial += 1
        need(self.event_serial <= 4096, 'controller_event_count')
        raw = encoded(value)
        self.check_storage(len(raw), 0 if self.phase == 'closure' else 64 << 20)
        save(P / 'controller-events' / ('%04d.json' % self.event_serial), value)

    def acquire(self):
        self.set_commands(P / 'admission-commands', self.runtime)
        self.lock, lock_metadata = self.support.acquire_lock(self.base)
        self.record['lockMetadata'] = lock_metadata
        self.runtime['lockMetadata'] = self.prepared['lockMetadata'] = self.close_commands['lockMetadata'] = lock_metadata
        self.record['protectedBefore'] = self.support.protected(self.base)
        self.prepared['protectedBefore'] = self.record['protectedBefore']
        reference = load(OLD_PREPARE, Path(OLD_PREPARE['path']), 'original_prepare_definitions')
        reference.base, reference.support = self.base, self.support
        baseline = parse(pin(Path(OLD_INPUT['path']), OLD_INPUT)[0])
        self.tools, prior = reference.verify_tools(baseline)
        need(prior['protectedAfter'] == self.record['protectedBefore'], 'protected_tool_baseline_changed')
        self.payloads, self.build_manifest = reference.package_payloads(baseline)
        self.package = baseline['package']
        self.record['package'] = self.package
        absence = self.m['units'].inspect_absence(self.base)
        self.prepared['capacityAbsence'] = copy.deepcopy(absence)
        fs = os.statvfs(E)
        memory = dict(line.split(':', 1) for line in Path('/proc/meminfo').read_text().splitlines())
        self.record['capacityAtAdmission'] = {'rootFreeBytes': fs.f_bavail * fs.f_frsize,
            'rootFreeInodes': fs.f_favail, 'memAvailableBytes': int(memory['MemAvailable'].split()[0]) * 1024}
        need(self.record['capacityAtAdmission']['rootFreeBytes'] >= 4 << 30 and fs.f_favail >= 30000 and
             self.record['capacityAtAdmission']['memAvailableBytes'] >= 6 << 30, 'fresh_capacity_admission')
        need(self.support.reference_api(self.base) is not None, 'reference_api_unavailable')
        (P / 'controller-events').mkdir(mode=0o700)
        fd = os.open(RESERVE_FILE, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        try:
            self.reserve_identity = metadata(RESERVE_FILE)
            os.posix_fallocate(fd, 0, 64 << 20)
            os.fsync(fd)
        finally:
            try:
                self.reserve_identity = metadata(RESERVE_FILE)
            finally:
                os.close(fd)
        self.record['cleanupCaptureReserve'] = {'path': str(RESERVE_FILE), 'metadata': self.reserve_identity}
        save(INTENT, {'input': self.input_pin, 'sources': self.value['helpers'], 'profile': PROFILE,
                      'entryMonotonicNs': time.monotonic_ns(), 'lockMetadata': lock_metadata})
        self.mutation_admitted = True

    def prepare(self):
        self.set_deadline('preparation', time.monotonic() + 300)
        self.set_commands(P / 'prepare-commands', self.prepared)
        self.prepared['status'] = 'preparing'
        dependencies = {'preparation': OLD_PREPARE, **{name: self.value['helpers'][name] for name in ('support', 'units', 'corpus')}}
        try:
            self.actor = self.m['preparation'].make_preparation(self.base, self.support, self.m['units'], self.m['corpus'],
                package=self.package, tools=self.tools, dependencies=dependencies, deadline=self.phase_deadline)
            self.ctx = self.actor.ctx
            self.actor.initialize_cluster()
            self.actor.install(self.payloads, self.build_manifest)
            self.support.check_installation(self.base, self.ctx)
            self.support.check_infrastructure(self.base, self.ctx, True)
            self.support.anchor_lifetime(self.base, self.ctx, 'prepare_handoff')
            self.prepared['protectedAfter'] = self.support.protected(self.base, self.ctx)
            need(self.prepared['protectedAfter'] == self.record['protectedBefore'], 'preparation_protected_changed')
            self.prepared['context'] = save(P / 'runtime-context.json', self.ctx)
            self.prepared['status'] = 'prepared_for_native_capacity'
        except BaseException as error:
            self.prepared.update(status='failed', errorCode=code(error))
            self.fail('preparation', error)
            raise
        finally:
            self.base.run = self.command
            self.prepared['allOwnedCommandsClosed'] = all(row.get('closed') is True for row in self.prepared['commands'])
            try:
                self.record['componentReceipts']['preparation'] = save(E / 'preparation.json', self.prepared)
            except BaseException as error:
                self.fail('preparation_receipt', error)
            self.payloads = None

    def assert_owned(self):
        need(self.phase != 'business' or not self.record['failures'], 'controller_business_already_failed')
        self.support.check_infrastructure(self.base, self.ctx)
        self.app.check_owned()
        if self.pool is not None and self.phase == 'business':
            self.pool.poll()

    def sample(self):
        if self.metric_samples_stopped:
            return
        observation = self.app.check_owned()
        self.observers.capture_metrics(observation)
        if self.pool is not None:
            self.pool.poll()
        self.check_storage(0, 64 << 20)

    def snapshot(self, slot):
        need(self.phase == 'business' and not self.record['failures'], 'workload_sql_outside_business')
        return self.observers.observe(slot, application=self.app.check_owned(), deadline=self.business_deadline)

    def start_readers(self, phase, run, mappings):
        need(phase not in self.reader_starts, 'controller_reader_start_repeated')
        attempt = {'phase': phase, 'runId': run['Id'], 'poolInvoked': False, 'failed': False,
                   'childrenBefore': self.pool.record['childrenCreated'],
                   'registeredPhasesBefore': sorted(self.pool.record['phases'])}
        self.reader_starts[phase] = attempt
        try:
            self.app.check_owned()
            attempt['poolInvoked'] = True
            result = self.pool.start(phase, run, mappings)
            attempt['returned'] = True
            return result
        except BaseException as error:
            attempt.update(failed=True, errorCode=code(error),
                           childrenAfter=self.pool.record['childrenCreated'],
                           registeredPhasesAfter=sorted(self.pool.record['phases']))
            attempt['noChildrenForThisPhaseEstablished'] = (
                phase not in self.pool.record['phases'] and
                attempt['registeredPhasesBefore'] == attempt['registeredPhasesAfter'] and
                attempt['childrenBefore'] == attempt['childrenAfter'])
            raise

    def finish_readers(self, phase, deadline):
        if phase not in self.pool.record['phases']:
            attempt = self.reader_starts.get(phase)
            need(self.phase == 'closure' and attempt is not None and attempt.get('failed') and
                 attempt.get('noChildrenForThisPhaseEstablished') and
                 self.pool.record['childrenCreated'] == attempt['childrenBefore'] and
                 sorted(self.pool.record['phases']) == attempt['registeredPhasesBefore'],
                 'reader_phase_without_child_absence_authority')
            return {'phase': phase, 'runId': attempt['runId'], 'joined': True, 'workers': [],
                    'status': 'not_started', 'noChildrenCreated': True, 'parentStartEvidence': copy.deepcopy(attempt)}
        try:
            result = self.pool.finish(phase, min(deadline, self.phase_deadline))
        except BaseException as error:
            self.fail('reader_finish_' + phase, error)
            state = self.pool.record['phases'].get(phase, {})
            result = state.get('result')
            need(state.get('joined') is True and isinstance(result, dict) and result.get('joined') is True,
                 'reader_failure_without_completed_join')
            result = copy.deepcopy(result)
        allocation = self.allocations.get(phase)
        if allocation is not None and result.get('joined'):
            allocation.update(physicallyJoined=True, resultStatus=result.get('status'))
            if result.get('unusedAllocationMayBeReleased') is True:
                allocation.update(settled=True, observedRequests=result['actualRequests'],
                                  observedWorkerOutputBytes=result['actualWorkerOutputBytes'])
        return result

    def business(self):
        self.set_commands(P / 'runtime-commands', self.runtime)
        start = int(self.ctx['anchor']['unit']['ExecMainStartTimestampMonotonic']) / 1000000
        self.business_deadline = min(time.monotonic() + 1200, start + 2700 - 1080)
        self.closure_ceiling = min(self.business_deadline + 900, start + 2700 - 180)
        self.set_deadline('business', self.business_deadline)
        self.record['deadlines'] = {'originalAnchorStart': start, 'business': self.business_deadline,
                                    'closureCeiling': self.closure_ceiling, 'originalSequenceCeiling': start + 2700}
        self.support.anchor_lifetime(self.base, self.ctx, 'runtime_entry')
        self.observers = self.m['observers'].Observers(self.base, self.support, self.m['workload'], self.ctx,
                                                     deadline=self.closure_ceiling)
        self.observers.observe('prestart-identity', deadline=self.business_deadline)
        self.app = self.m['application'].Application(self.base, self.support, self.m['units'], self.ctx,
                                                   deadline=self.closure_ceiling)
        self.app.start()
        self.transport = self.m['transport'].ControlTransport(self.base, self.support, self.ctx, self.app,
            deadline=self.business_deadline, closure_deadline=self.closure_ceiling,
            reader_pin=self.value['helpers']['reader'], source_pin=self.value['helpers']['transport'],
            reserve=self.reserve)
        self.transport.install(self.m['journey'])
        ready_end = min(self.business_deadline, time.monotonic() + 10)
        ready = False
        self.phase = 'readiness'
        signal.setitimer(signal.ITIMER_REAL, max(0.001, ready_end - time.monotonic()))
        try:
            while time.monotonic() < ready_end:
                need(self.transport.counts['setup'] <= 42, 'readiness_setup_quota')
                health = self.transport.readiness('/healthz')
                readiness = self.transport.readiness('/readyz')
                need(time.monotonic() <= ready_end, 'readiness_absolute_deadline')
                if health['status'] == readiness['status'] == 200:
                    ready = True
                    break
                time.sleep(min(0.2, max(0, ready_end - time.monotonic())))
        finally:
            self.set_deadline('business', self.business_deadline)
        need(ready, 'native_capacity_not_ready')
        self.pool = self.m['reader-pool'].ReaderPool(self.base, self.m['reader'], self.ctx, self.app,
            reader_pin=self.value['helpers']['reader'], deadline=self.closure_ceiling, reserve=self.reserve)
        records = P / 'workload'
        records.mkdir(mode=0o700)
        self.workload = self.m['workload'].make_workload(self.m['journey'], origin=self.ctx['origin'],
            media_root=str(F / 'media'), setup_token=self.ctx['setupToken'], passwords=self.ctx['passwords'],
            nonce=self.ctx['nonce'], record_dir=str(records), request_ids={name: secrets.token_hex(16) for name in ('cold', 'cached')},
            corpus=self.ctx['media'], prior_setup_requests=self.transport.counts['setup'], callbacks={
                'assert_owned': self.assert_owned, 'sample_resources': self.sample, 'snapshot': self.snapshot,
                'start_readers': self.start_readers, 'finish_readers': self.finish_readers, 'record': self.event,
                'remaining': lambda: self.business_deadline - time.monotonic()})
        self.transport.bind(self.workload)
        self.workload_result = self.workload.execute()
        self.m['corpus'].snapshot_corpus(self.base, self.ctx['media'])
        self.runtime['status'] = 'workload_passed_pending_closure'

    def release_reserve(self):
        if self.reserve_identity is None:
            return
        need(metadata(RESERVE_FILE) == self.reserve_identity, 'cleanup_reserve_identity_changed')
        os.unlink(RESERVE_FILE)
        self.record['cleanupCaptureReserveReleased'] = True
        self.reserve_identity = None

    def save_components(self, suffix):
        builders = {'runtime-commands': lambda: self.runtime,
                    'reader-allocations': lambda: {'allocations': self.allocations, 'starts': self.reader_starts}}
        for key, item in (('application', self.app), ('observers', self.observers), ('reader-pool', self.pool)):
            if item is not None:
                builders[key] = lambda item=item: item.record
        if self.transport is not None:
            builders['transport'] = lambda: self.transport.summary()
        if self.workload is not None:
            builders['workload'] = lambda: {'result': self.workload_result, 'cleanup': self.workload_cleanup,
                'finalValidation': self.final_validation, 'observations': self.workload.observations,
                'readerJoins': self.workload.reader_joins, 'budgets': self.workload.budget,
                'tracePersisted': self.workload.trace_persisted}
        for key, build in builders.items():
            try:
                value = build()
                self.check_storage(len(encoded(value)))
                self.record['componentReceipts'][key + '-' + suffix] = save(P / (key + '-' + suffix + '.json'), value)
            except BaseException as error:
                self.fail('save_' + key, error)

    def cleanup(self):
        for number in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
            signal.signal(number, signal.SIG_IGN)
        self.metric_samples_stopped = True
        now = time.monotonic()
        ceiling = self.closure_ceiling if self.closure_ceiling is not None else self.started + 1320
        self.closure_deadline = min(now + 900, ceiling)
        self.set_deadline('closure', self.closure_deadline)
        self.base.run = self.command
        for stage, operation in (('restore_host', self.restore_host), ('release_capture_reserve', self.release_reserve)):
            try:
                operation()
            except BaseException as error:
                self.fail(stage, error)
        if self.pool is not None and self.record['failures']:
            try:
                self.pool.mark_controller_failure(self.record['firstFailure']['code'])
            except BaseException as error:
                self.fail('mark_reader_failure', error)
        if self.transport is not None:
            try:
                self.transport.set_phase_deadline(self.closure_deadline, cleaning=True)
            except BaseException as error:
                self.fail('transport_cleanup_deadline', error)
        if self.workload is not None:
            try:
                self.workload_cleanup = self.workload.cleanup(deadline=self.closure_deadline)
                if self.workload_cleanup.get('status') != 'closed':
                    self.fail('workload_cleanup', Rejected('workload_responsibilities_incomplete'))
            except BaseException as error:
                self.fail('workload_cleanup', error)
        if self.pool is not None:
            for phase in ('cold', 'cached'):
                if phase not in self.allocations:
                    continue
                try:
                    self.finish_readers(phase, min(self.closure_deadline, time.monotonic() + 15))
                except BaseException as error:
                    self.fail('reader_cleanup_' + phase, error)
        try:
            self.restore_host()
        except BaseException as error:
            self.fail('restore_host_before_stop', error)
        if self.app is not None:
            try:
                self.app.stop(bool(self.app.record.get('configurationAccepted')),
                              min(self.closure_deadline, time.monotonic() + 35))
            except BaseException as error:
                self.fail('application_stop', error)
                if not self.app.record.get('stopDispatched'):
                    try:
                        self.app.stop(False, min(self.closure_deadline, time.monotonic() + 35))
                    except BaseException as retry_error:
                        self.fail('application_undispatched_cleanup', retry_error)
        if self.observers is not None and self.workload is not None and self.app is not None:
            if len(self.observers.record['sql']) == 5 and not self.observers.record['failed'] and self.app.record.get('physicalClosed'):
                try:
                    final = self.observers.observe('final-after-stop', application=self.app.record, deadline=self.closure_deadline)
                    self.final_validation = self.workload.validate_final_snapshot(final)
                except BaseException as error:
                    self.fail('final_sql_or_responsibility_check', error)
            else:
                self.record['finalSqlNotAttempted'] = 'prior_slot_failed_or_incomplete_or_application_not_closed'
        try:
            self.save_components('before-preservation')
        except BaseException as error:
            self.fail('optional_component_receipts', error)
        try:
            self.raw_before_closure = self.raw_bytes()
        except BaseException as error:
            self.fail('raw_budget_read', error)
        try:
            self.set_commands(P / 'closure-commands', self.close_commands)
            budget = None if self.raw_before_closure is None else {
                'rawEvidenceBytes': self.raw_before_closure, 'rawLimitBytes': RAW_LIMIT,
                'rootAllocationLimitBytes': ROOT_LIMIT, 'rootReserveBytes': ROOT_RESERVE}
            self.closer = self.m['closure'].Closure(self.base, self.support, self.m['units'], self.m['corpus'],
                self.ctx, self.prepared, self.app.record if self.app is not None else {},
                deadline=self.closure_deadline, budget=budget)
            closed = self.closer.close()
            self.record['closure'] = closed
            if closed.get('status') != 'owned_resources_preserved_and_closed':
                self.fail('preservation', Rejected('owned_preservation_incomplete'))
        except BaseException as error:
            self.fail('closure_entry', error)
        no_closure_stop_entry = (self.closer is None or
            (self.closer.record.get('status') == 'closure_authority_unavailable_resources_retained' and
             self.closer.record.get('infrastructureStopMethodsEntered') == []))
        if no_closure_stop_entry and self.actor is not None:
            try:
                self.actor.cleanup_limit = self.actor.cleanup_deadline = self.closure_deadline
                self.actor.cleanup_owned('controller_zero_stop_entry_fallback')
                self.record['preparationEmergencyClose'] = {'basis': 'closure_entered_no_infrastructure_stop_methods',
                    'ownedUnits': copy.deepcopy(self.prepared.get('ownedUnits', {})),
                    'commands': copy.deepcopy(self.prepared.get('commands', []))}
            except BaseException as error:
                self.fail('preparation_emergency_close', error)
            finally:
                self.base.run = self.command
        self.runtime['allOwnedCommandsClosed'] = all(row.get('closed') is True for row in self.runtime['commands'])
        self.close_commands['allOwnedCommandsClosed'] = all(row.get('closed') is True for row in self.close_commands['commands'])

    def run(self):
        try:
            self.acquire()
            self.prepare()
            need(not self.record['failures'], 'preparation_not_fully_recorded')
            self.business()
        except BaseException as error:
            self.fail(self.phase, error)
        finally:
            try:
                if self.lock is not None and self.mutation_admitted:
                    self.cleanup()
                elif self.lock is not None:
                    self.release_reserve()
            except BaseException as error:
                self.fail('controller_cleanup', error)
            signal.setitimer(signal.ITIMER_REAL, 0)
            self.base.run = self.command
            try:
                if self.lock is not None:
                    overall_end = self.record.get('deadlines', {}).get('originalSequenceCeiling', self.started + 1320)
                    self.phase_deadline = min(time.monotonic() + 20, overall_end)
                    need(self.phase_deadline > time.monotonic(), 'final_evidence_deadline')
                    self.restore_host()
                    self.record['protectedAfter'] = self.support.protected(self.base, self.ctx or None)
                    need(self.record['protectedAfter'] == self.record['protectedBefore'], 'controller_final_protected_changed')
                    need(metadata(self.base.LOCK) == self.record['lockMetadata'], 'controller_final_lock_changed')
            except BaseException as error:
                self.fail('final_protection', error)
            finally:
                if self.lock is not None:
                    try:
                        os.close(self.lock)
                        self.record['lockReleased'] = True
                    except BaseException as error:
                        self.fail('lock_release', error)
                for name, value in self.host.items():
                    try:
                        os.close(value['fd'])
                    except BaseException as error:
                        self.fail('host_descriptor_' + name, error)
            try:
                for report in (self.prepared, self.runtime, self.close_commands):
                    report['allOwnedCommandsClosed'] = all(row.get('closed') is True for row in report['commands'])
                self.record['componentReceipts']['final-command-reports'] = save(P / 'final-command-reports.json',
                    {'admissionAndRuntime': self.runtime, 'preparation': self.prepared, 'closure': self.close_commands})
            except BaseException as error:
                self.fail('final_command_receipt', error)
            self.record['allOwnedCommandsClosed'] = all(row.get('closed') is True for report in
                (self.prepared, self.runtime, self.close_commands) for row in report['commands'])
            expected = (self.workload_result is not None and self.workload_result.get('status') == 'workload-passed-pending-closure' and
                self.workload_cleanup is not None and self.workload_cleanup.get('status') == 'closed' and
                self.final_validation is not None and self.final_validation.get('status') == 'normal-workload-and-credential-closure-checked' and
                self.app is not None and self.app.record.get('normalExit') and
                self.observers is not None and len(self.observers.record['sql']) == 6 and
                not self.observers.record['failed'] and self.record.get('closure', {}).get('status') == 'owned_resources_preserved_and_closed' and
                self.record['lockReleased'] and self.record['allOwnedCommandsClosed'] and not self.record['failures'])
            self.record['status'] = ('native_workload_and_closure_passed_pending_independent_review' if expected
                                     else 'native_capacity_attempt_failed')
            self.record['finishedMonotonicNs'] = time.monotonic_ns()
            receipt = None
            try:
                receipt = save(RESULT, self.record)
            except BaseException as error:
                self.fail('final_execution_receipt', error)
                self.record['status'] = 'native_capacity_attempt_failed'
                expected = False
                try:
                    receipt = save(P / 'controller-result-fallback.json', self.record)
                    self.record['fallbackReceiptUsed'] = True
                except BaseException as fallback_error:
                    self.fail('fallback_execution_receipt', fallback_error)
            print(json.dumps({'status': self.record['status'], 'receipt': receipt,
                'firstFailure': self.record.get('firstFailure'), 'lockReleased': self.record['lockReleased'],
                'receiptPersisted': receipt is not None,
                'closureStatus': self.record.get('closure', {}).get('status'),
                'capacityAccepted': False, 'wholeM2M6Accepted': False}), flush=True)
        return 0 if expected else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input', type=Path, required=True)
    parser.add_argument('--input-sha256', required=True)
    args = parser.parse_args()
    need(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0 and sys.flags.isolated and
         sys.flags.dont_write_bytecode and 'SSH_CONNECTION' in os.environ and Path(__file__) == SELF and args.input == INPUT and
         re.fullmatch('[0-9a-f]{64}', args.input_sha256), 'fixed_remote_controller_entry')
    os.umask(0o077)
    private_directory(E)
    private_directory(P)
    need(not any(os.path.lexists(path) for path in (INTENT, RESULT, RESERVE_FILE, F)), 'native_scope_consumed')
    value, input_pin = input_value(args.input_sha256)
    modules = {role: load(value['helpers'][role], P / name, role) for role, name in NAMES.items() if role != 'controller'}
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    resource.setrlimit(resource.RLIMIT_AS, (256 << 20, 256 << 20))
    def interrupted(number, _frame):
        raise Deadline('controller_signal_' + str(number))
    for number in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP, signal.SIGALRM):
        signal.signal(number, interrupted)
    return Controller(value, input_pin, modules).run()


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except BaseException as error:
        if isinstance(error, SystemExit):
            raise
        print(json.dumps({'status': 'controller_entry_or_receipt_failed', 'errorCode': code(error),
                          'capacityAccepted': False, 'wholeM2M6Accepted': False}), flush=True)
        raise SystemExit(2) from None
