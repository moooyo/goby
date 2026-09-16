"""Own the fixed cold/cached pair of capacity readers; import performs no work.

ReaderPool(base, reader_module, context, application, *, reader_pin, deadline,
           reserve, record=None)
  start(phase, run, mappings) -> private pair birth/start evidence
  poll() -> current record, raising a newly observed worker failure once
  mark_controller_failure(code) -> record an existing controller failure and
      publish an owned controller_failure cancel if no cancel was published
  finish(phase, absolute_deadline) -> a retained joined result, or raise a new
      failure after both children have received bounded cleanup attention

All deadlines are absolute time.monotonic() seconds. record is an optional new
empty dict, retained by reference. The parent calls Application.check_owned
before start; this module reads that object's accepted record and actual /proc
identities but never calls a service, APP stop, SQL, HTTP, or another actor.
The same parent process constructs, runs, and closes the pool. It must call
mark_controller_failure with a fixed safe code before workload.cleanup after
an outside business failure. A previous failure remains failed, but is not
raised repeatedly while cleanup closes unrelated owned responsibilities.

reserve receives one allocation per phase:
  {kind: "native-capacity-reader-reservation", phase, workers: 2,
   httpRequests: 120, outputBytes: 150994944}
These are maximum allocations, not observed HTTP dispatches. The parent keeps
them separate from ControlTransport dispatches and settles unused allocations
only from the returned actual counters after a complete joined readback. No
additional reserve call is made here during settlement. The parent separately
accounts for its own reports, projections, input, control and capture files.

Only the two restricted token/user/library bindings are sent to children.
expectedItems, favorites, client headers, and administrator credentials never
enter the reader input. All inputs and evidence are new root:root 0600 files
inside root:root 0700 directories. No existing environment or key file is read.

Popen creates each fixed input once with a new session. Nonblocking stdout and
stderr pipes are drained into bounded private files. waitid(WNOWAIT) observes
termination without reaping the leader. The leader remains reserved until
pipe EOF and a group scan show no other member. Only then does waitpid record
the actual status, followed by a real empty-group observation. TERM/KILL may
target only the still-unreaped, birth-bound new session. A forced close fails
acceptance. Each pair's first join deadline is frozen at at most 15 seconds;
repeated calls cannot extend it or recreate a child.

The final reader receipt is independently bound to ready/input/source/start,
each intent and response receipt, and private raw HTTP evidence. Raw headers,
transfer bytes and decoded JSON are checked before projecting workload pages.
This projection proves only local protocol/row integrity. The workload still
owns final SQL, ACL, UserData and native scan reconciliation. Missing overlap
or zero dispatched requests is retained as an incomplete metric, not invented
traffic or a product failure.
"""

import copy
import errno
import hashlib
import http.client
import io
import json
import math
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import sys
import time


E = Path('/opt/goby-test/native-scan-http-capacity-20260915')
F = Path('/opt/goby-native-scan-http-capacity-20260915')
SOURCE = E / 'private/capacity-reader.py'
CONTEXT = E / 'private/reader-context.json'
READER_SHA256 = '609d6ea7d3bcffcddce724007f7c12ad68940dcdbe818e15117a2bcc6d7792bb'
READER_BYTES = 49598
SIDES = (('visible', 'left', 'A'), ('hidden', 'right', 'B'))
PHASES = ('cold', 'cached')
STREAM_CAP = 32768
SCAN_LIMIT = 32768
JOIN_SECONDS = 15.0
NORMAL_STATUSES = {'stopped_by_controller', 'cancelled_before_start', 'window_complete', 'request_limit_reached'}
INITIAL_REQUEST_FIELDS = (
    'sequence', 'phase', 'reader', 'runId', 'shape', 'method', 'path',
    'dispatchMonotonicNs', 'headersMonotonicNs', 'bodyCompleteMonotonicNs',
    'connectionCloseMonotonicNs', 'status', 'contentType', 'contentLength',
    'responseRequestId', 'headers', 'bodyComplete', 'connectionClosed',
    'responseClosed', 'connectionCreated', 'responseCreated', 'responseCloseErrors',
    'connectionCloseErrors', 'dispatched', 'error', 'correctness', 'intentAnchor')


class ReaderPoolError(RuntimeError):
    """A fixed safe error code, with all detailed evidence retained privately."""


def _need(condition, code):
    if not condition:
        raise ReaderPoolError(code)


def _integer(value, minimum=0):
    return type(value) is int and value >= minimum


def _encoded(value):
    return (json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + '\n').encode()


def _code(error):
    if isinstance(error, ReaderPoolError):
        return str(error)
    return type(error).__name__


def _stat_process(pid):
    fields = (Path('/proc') / str(pid) / 'stat').read_text().rsplit(') ', 1)[1].split()
    return {'pid': pid, 'state': fields[0], 'parentPid': int(fields[1]),
            'processGroup': int(fields[2]), 'sessionId': int(fields[3]), 'startTicks': fields[19]}


def _metadata(info):
    return {'device': info.st_dev, 'inode': info.st_ino, 'uid': info.st_uid, 'gid': info.st_gid,
            'mode': stat.S_IMODE(info.st_mode), 'bytes': info.st_size, 'links': info.st_nlink,
            'mtimeNs': info.st_mtime_ns, 'ctimeNs': info.st_ctime_ns}


def _directory(path):
    _need(path.is_absolute() and path.resolve() == path and path.is_relative_to(E),
          'reader_pool_directory_scope')
    for current in (path, *path.parents):
        row = current.lstat()
        _need(stat.S_ISDIR(row.st_mode) and not stat.S_ISLNK(row.st_mode), 'reader_pool_directory_symlink')
        if current == E or current.is_relative_to(E):
            _need(row.st_uid == row.st_gid == 0 and stat.S_IMODE(row.st_mode) == 0o700,
                  'reader_pool_directory_permissions')


def _read(path, maximum, *, empty=False):
    _directory(path.parent)
    fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW)
    try:
        before = os.fstat(fd)
        _need(stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and
              stat.S_IMODE(before.st_mode) == 0o600 and before.st_nlink == 1 and
              (0 if empty else 1) <= before.st_size <= maximum, 'reader_pool_private_file_contract')
        chunks, size = [], 0
        while size <= maximum:
            part = os.read(fd, min(65536, maximum + 1 - size))
            if not part:
                break
            chunks.append(part)
            size += len(part)
        raw = b''.join(chunks)
        _need(len(raw) == before.st_size and
              _metadata(before) == _metadata(os.fstat(fd)) == _metadata(path.lstat()),
              'reader_pool_private_file_changed')
        return raw, {'path': str(path), 'sha256': hashlib.sha256(raw).hexdigest(),
                     'bytes': len(raw), 'metadata': _metadata(before)}
    finally:
        os.close(fd)


def _small_pin(observed):
    return {key: observed[key] for key in ('path', 'sha256', 'bytes')}


def _pin_read(pin, path, maximum, *, empty=False, metadata=False):
    expected = {'path', 'sha256', 'bytes', 'metadata'} if metadata else {'path', 'sha256', 'bytes'}
    _need(type(pin) is dict and set(pin) == expected and pin['path'] == str(path) and
          _integer(pin['bytes']) and re.fullmatch('[0-9a-f]{64}', pin['sha256']) is not None,
          'reader_pool_file_pin_contract')
    raw, observed = _read(path, maximum, empty=empty)
    _need((observed if metadata else _small_pin(observed)) == pin, 'reader_pool_file_pin_changed')
    return raw


def _write(path, raw):
    _directory(path.parent)
    _need(type(raw) is bytes and len(raw) <= 4 << 20, 'reader_pool_write_bound')
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_CLOEXEC | os.O_NOFOLLOW, 0o600)
    try:
        offset = 0
        while offset < len(raw):
            count = os.write(fd, raw[offset:])
            _need(count > 0, 'reader_pool_write_incomplete')
            offset += count
        os.fsync(fd)
    finally:
        os.close(fd)
    observed, pin = _read(path, max(1, len(raw)), empty=True)
    _need(observed == raw, 'reader_pool_write_readback')
    return _small_pin(pin)


def _publish(path, value):
    pending = path.with_name(path.stem + '-pending.json')
    pin = _write(pending, _encoded(value))
    _need(not os.path.lexists(path), 'reader_pool_control_already_exists')
    os.rename(pending, path)
    fd = os.open(path.parent, os.O_RDONLY | os.O_CLOEXEC | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)
    raw, observed = _read(path, 4096)
    _need(_small_pin(observed) == dict(pin, path=str(path)) and raw == _encoded(value),
          'reader_pool_control_publication')
    return observed


class _MemorySocket:
    def __init__(self, stream):
        self.stream = stream

    def makefile(self, mode):
        _need(mode == 'rb', 'reader_pool_memory_response_mode')
        return self.stream


class _ResponseBuffer(io.BytesIO):
    def close(self):
        if not self.closed:
            self.consumed = self.tell()
        super().close()


def _chunked_body(raw, maximum):
    position, decoded, trailer_bytes = 0, bytearray(), 0
    while True:
        end = raw.find(b'\r\n', position)
        _need(end >= 0 and end - position <= 32768, 'reader_pool_chunk_size_line_incomplete')
        line = raw[position:end]
        size_text = line.split(b';', 1)[0]
        _need(re.fullmatch(b'[0-9A-Fa-f]+', size_text) is not None and
              all(byte >= 32 and byte != 127 for byte in line), 'reader_pool_chunk_size_syntax')
        size = int(size_text, 16)
        position = end + 2
        _need(size <= maximum - len(decoded), 'reader_pool_decoded_chunk_body_limit')
        if size == 0:
            while True:
                end = raw.find(b'\r\n', position)
                _need(end >= 0, 'reader_pool_chunk_trailer_incomplete')
                trailer = raw[position:end]
                trailer_bytes += end + 2 - position
                _need(trailer_bytes <= 32768, 'reader_pool_chunk_trailer_limit')
                position = end + 2
                if not trailer:
                    _need(position == len(raw), 'reader_pool_chunk_trailing_bytes')
                    return bytes(decoded)
                name, separator, value = trailer.partition(b':')
                _need(separator == b':' and re.fullmatch(b"[!#$%&'*+.^_`|~0-9A-Za-z-]+", name) is not None and
                      name.lower() not in (b'content-length', b'transfer-encoding', b'content-type', b'content-encoding') and
                      all(byte == 9 or byte >= 32 and byte != 127 for byte in value),
                      'reader_pool_chunk_trailer_syntax')
        _need(position + size + 2 <= len(raw) and raw[position + size:position + size + 2] == b'\r\n',
              'reader_pool_chunk_data_incomplete')
        decoded.extend(raw[position:position + size])
        position += size + 2


class ReaderPool:
    def __init__(self, base, reader_module, context, application, *, reader_pin,
                 deadline, reserve, record=None):
        _need(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0 and
              sys.flags.isolated == 1 and sys.flags.dont_write_bytecode == 1,
              'reader_pool_linux_root_isolated_required')
        _need(type(deadline) in (int, float) and math.isfinite(deadline) and deadline > time.monotonic()
              and callable(reserve), 'reader_pool_deadline_or_reserve')
        _need(record is None or type(record) is dict and not record, 'reader_pool_record_not_new')
        _need(reader_module.E == E and reader_module.F == F and reader_module.SOURCE == SOURCE and
              reader_module.CONTEXT == CONTEXT and Path(reader_module.__file__) == SOURCE and
              reader_pin == {'path': str(SOURCE), 'sha256': READER_SHA256, 'bytes': READER_BYTES},
              'reader_pool_source_scope')
        _pin_read(reader_pin, SOURCE, 256 << 10)
        self.base, self.reader, self.ctx, self.application = base, reader_module, context, application
        self.deadline, self.reserve = float(deadline), reserve
        self.record = {} if record is None else record
        self.record.update(kind='native-capacity-reader-pool', version=1, scope=str(E),
            source=dict(reader_pin), phases={}, childrenCreated=0, errors=[], firstError=None,
            controllerFailure=None, allChildrenJoined=False, externalHttpRequests=0,
            serviceCommands=0, sqlConnections=0)
        self.record['clockDomainBefore'] = self.reader.clock_domain()
        self._first_error, self._reported_error = None, False
        self._phases, self._active = {}, None
        self._controller_pid = os.getpid()
        self._reader_pin = dict(reader_pin)
        self._python = Path('/usr/bin/python3').resolve()
        self._python_info = self._python.stat()
        _directory(E / 'private')
        _need(context.get('scope') == str(E) and context.get('fixtureRoot') == str(F) and
              application.record.get('configurationAccepted') is True and
              application.record.get('closed') is False, 'reader_pool_application_not_accepted')
        app = self.reader.process_identity(application.record['pid'])
        _need(app == {key: application.record['process'][key] for key in self.reader.PROCESS_FIELDS},
              'reader_pool_application_profile_changed')
        anchor = self.reader.process_identity(context['anchorPid'])
        prior_anchor = context['anchor']['process']
        _need(anchor['pid'] == prior_anchor['pid'] and anchor['startTicks'] == prior_anchor['startTicks'] and
              anchor['cgroup'] == prior_anchor['cgroup'] and anchor['networkNamespace'] == context['networkNamespace'],
              'reader_pool_anchor_profile_changed')
        controller = self.reader.process_identity(self._controller_pid)
        host = os.readlink('/proc/1/ns/net')
        _need(controller['networkNamespace'] == host and controller['uids'] == controller['gids'] == [0] * 4,
              'reader_pool_controller_not_host_root')
        self._context = self.reader.validate_context({
            'kind': 'native-scan-http-capacity-reader-context', 'version': 1, 'scope': str(E),
            'fixtureRoot': str(F), 'origin': self.reader.ORIGIN, 'bootId': context['bootId'],
            'hostNetworkNamespace': host, 'networkNamespace': context['networkNamespace'],
            'anchor': anchor, 'application': app, 'controller': controller})
        _need(Path('/proc/sys/kernel/random/boot_id').read_text().strip() == context['bootId'],
              'reader_pool_boot_changed')
        self._context_pin = _write(CONTEXT, _encoded(self._context))
        self._root = E / 'private/readers'
        self._root.mkdir(mode=0o700)
        _directory(self._root)
        self.record.update(context=dict(self._context_pin), controller=copy.deepcopy(controller))

    def _controller(self, *, cleanup=False):
        current = self.reader.process_identity(self._controller_pid)
        expected = self._context['controller']
        stable = self.reader.PROCESS_FIELDS - {'networkNamespace'}
        _need(os.getpid() == self._controller_pid and
              all(current[key] == expected[key] for key in stable) and
              Path('/proc/sys/kernel/random/boot_id').read_text().strip() == self._context['bootId'],
              'reader_pool_controller_lifetime_changed')
        if current['networkNamespace'] != expected['networkNamespace']:
            _need(cleanup, 'reader_pool_controller_not_in_host_namespace')
            if not self.record.get('controllerNamespaceChangedDuringCleanup'):
                self.record['controllerNamespaceChangedDuringCleanup'] = {
                    'expected': expected['networkNamespace'], 'observed': current['networkNamespace'],
                    'anchor': self.reader.clock_anchor()}
                self._remember(self._active, ReaderPoolError('reader_pool_controller_namespace_changed'))
                self._mark_failure('reader_pool_controller_namespace_changed')

    def _remember(self, phase, error, worker=None):
        row = {'phase': phase, 'code': _code(error), 'monotonicNs': time.monotonic_ns()}
        if worker is not None:
            row['reader'] = worker['side']
            if len(worker['record']['errors']) < 128:
                worker['record']['errors'].append(copy.deepcopy(row))
            else:
                worker['record']['additionalErrorCount'] = worker['record'].get('additionalErrorCount', 0) + 1
        if self._first_error is None:
            self._first_error = error
            self.record['firstError'] = copy.deepcopy(row)
        if len(self.record['errors']) < 256:
            self.record['errors'].append(row)

    def _raise_new(self):
        if self._first_error is not None and not self._reported_error:
            self._reported_error = True
            raise self._first_error

    def _event(self, phase, kind, reason=None):
        value = {'kind': 'native-scan-http-capacity-reader-' + kind, 'version': 1,
                 'scope': str(E), 'phase': phase['name'], 'runId': phase['run']['Id'],
                 'inputs': {side: phase['inputs'][side]['pin']['sha256'] for _role, side, _label in SIDES},
                 'anchor': self.reader.clock_anchor()}
        if reason is not None:
            value['reason'] = reason
        return value

    def _cancel(self, phase, reason):
        if phase.get('cancel') is not None:
            if reason == 'controller_failure' and phase['cancel']['event']['reason'] != reason:
                phase['record']['existingCancelRetainedAfterFailure'] = True
            return
        _need(set(phase['inputs']) == {'left', 'right'}, 'reader_pool_cancel_input_pair_missing')
        _need(not phase['record'].get('cancelPublicationAttempted'), 'reader_pool_cancel_publication_already_attempted')
        phase['record']['cancelPublicationAttempted'] = True
        value = self._event(phase, 'cancel', reason)
        phase['record']['cancelPublicationIntent'] = {'reason': reason, 'anchor': value['anchor']}
        observed = _publish(phase['path'] / 'cancel.json', value)
        phase['cancel'] = {'event': value, 'source': observed}
        phase['record']['cancel'] = copy.deepcopy(phase['cancel'])

    def _mark_failure(self, code):
        _need(type(code) is str and re.fullmatch('[A-Za-z0-9_.-]{1,160}', code) is not None,
              'reader_pool_controller_failure_code')
        if self.record['controllerFailure'] is None:
            self.record['controllerFailure'] = {'code': code, 'anchor': self.reader.clock_anchor()}
        for phase in self._phases.values():
            if not phase.get('joined') and phase['workers']:
                try:
                    self._cancel(phase, 'controller_failure')
                except BaseException as error:
                    self._remember(phase['name'], error)

    def mark_controller_failure(self, code):
        self._controller(cleanup=True)
        _need(type(code) is str and re.fullmatch('[A-Za-z0-9_.-]{1,160}', code) is not None,
              'reader_pool_controller_failure_code')
        if self._first_error is None:
            self._remember(self._active, ReaderPoolError(code))
            self._reported_error = True
        self._mark_failure(code)
        return self.record

    def _group(self, pgid):
        rows, count = [], 0
        scan_deadline = time.monotonic() + 2.0
        with os.scandir('/proc') as entries:
            for entry in entries:
                if not entry.name.isdecimal():
                    continue
                count += 1
                _need(count <= SCAN_LIMIT and time.monotonic() < scan_deadline,
                      'reader_pool_process_scan_limit')
                try:
                    row = _stat_process(int(entry.name))
                except (FileNotFoundError, ProcessLookupError):
                    continue
                if row['processGroup'] == pgid:
                    rows.append(row)
        return sorted(rows, key=lambda row: row['pid'])

    def _birth(self, worker):
        process = worker['process']
        row = _stat_process(process.pid)
        _need(row['pid'] == row['processGroup'] == row['sessionId'] and
              row['parentPid'] == self._controller_pid and row['state'] not in ('X',),
              'reader_pool_child_birth_scope')
        worker['birth'] = row
        worker['record']['birth'] = copy.deepcopy(row)
        if row['state'] != 'Z':
            full = self.reader.process_identity(process.pid)
            _need(full['uids'] == full['gids'] == [0] * 4 and
                  full['exe'] == str(self._python) and
                  (full['executableDevice'], full['executableInode']) ==
                  (self._python_info.st_dev, self._python_info.st_ino) and
                  full['cgroup'] == self._context['controller']['cgroup'] and
                  full['networkNamespace'] == self._context['hostNetworkNamespace'] and
                  full['startTicks'] == row['startTicks'], 'reader_pool_child_python_profile')
            worker['record']['birthProcess'] = full
        worker['record']['birthReceipt'] = _write(
            worker['phase']['path'] / (worker['side'] + '-birth.json'), _encoded(worker['record']))

    def _spawn(self, phase, role, side, label):
        binding = phase['inputs'][side]
        row = {'role': role, 'reader': side, 'libraryLabel': label, 'userId': binding['value']['userId'],
               'libraryId': binding['value']['libraryId'], 'source': dict(self._reader_pin),
               'input': dict(binding['pin']), 'createAttempted': True, 'created': False,
               'joined': False, 'forced': False, 'errors': [], 'signals': [], 'groupObservations': []}
        phase['record']['workers'].append(row)
        worker = {'phase': phase, 'side': side, 'role': role, 'record': row,
                  'process': None, 'birth': None, 'streams': {}, 'terminal': None, 'joined': False}
        phase['workers'].append(worker)
        _write(phase['path'] / (side + '-spawn-intent.json'), _encoded(row))
        env = {'PATH': '/usr/bin:/bin', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8',
               'HOME': '/root', 'TZ': 'UTC', 'PYTHONDONTWRITEBYTECODE': '1'}
        ssh = os.environ.get('SSH_CONNECTION')
        if ssh is not None:
            _need(type(ssh) is str and len(ssh) <= 256 and
                  re.fullmatch(r'[0-9A-Fa-f:.]+ [0-9]{1,5} [0-9A-Fa-f:.]+ [0-9]{1,5}', ssh) is not None,
                  'reader_pool_ssh_connection_shape')
            env['SSH_CONNECTION'] = ssh
        argv = ['/usr/bin/python3', '-I', '-B', str(SOURCE), '--input', binding['pin']['path'],
                '--input-sha256', binding['pin']['sha256']]
        row.update(argv=argv, dispatchAnchor=self.reader.clock_anchor())
        try:
            for name in ('stdout', 'stderr'):
                path = phase['path'] / (side + '.' + name)
                fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_CLOEXEC | os.O_NOFOLLOW, 0o600)
                worker['streams'][name] = {'pipe': None, 'fd': fd, 'path': path,
                    'bytes': 0, 'seenBytes': 0, 'eof': False, 'nonblocking': False}
            process = subprocess.Popen(argv, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                       stderr=subprocess.PIPE, env=env, cwd=str(E / 'private'),
                                       close_fds=True, start_new_session=True)
            worker['process'] = process
            row.update(created=True, pid=process.pid, popenReturnedAnchor=self.reader.clock_anchor())
            self.record['childrenCreated'] += 1
            for name in ('stdout', 'stderr'):
                worker['streams'][name]['pipe'] = getattr(process, name)
            for stream in worker['streams'].values():
                os.set_blocking(stream['pipe'].fileno(), False)
                stream['nonblocking'] = True
            self._birth(worker)
        except BaseException:
            if worker['process'] is None:
                for stream in worker['streams'].values():
                    if stream['fd'] is not None:
                        os.close(stream['fd'])
                        stream['fd'] = None
                    stream['eof'] = True
            raise

    def _drain(self, worker):
        for name, stream in worker['streams'].items():
            if stream['eof']:
                continue
            if not stream['nonblocking']:
                os.set_blocking(stream['pipe'].fileno(), False)
                stream['nonblocking'] = True
            for _ in range(16):
                try:
                    raw = os.read(stream['pipe'].fileno(), 4096)
                except BlockingIOError:
                    break
                if not raw:
                    stream['eof'] = True
                    stream['pipe'].close()
                    try:
                        os.fsync(stream['fd'])
                    finally:
                        os.close(stream['fd'])
                        stream['fd'] = None
                    saved, observed = _read(stream['path'], STREAM_CAP, empty=True)
                    _need(len(saved) == stream['bytes'], 'reader_pool_capture_size_changed')
                    worker['record'][name] = dict(_small_pin(observed), eof=True,
                                                   seenBytes=stream['seenBytes'])
                    break
                stream['seenBytes'] += len(raw)
                retained = raw[:max(0, STREAM_CAP - stream['bytes'])]
                offset = 0
                while offset < len(retained):
                    count = os.write(stream['fd'], retained[offset:])
                    _need(count > 0, 'reader_pool_capture_write_incomplete')
                    offset += count
                stream['bytes'] += len(retained)
                if stream['seenBytes'] > STREAM_CAP:
                    worker['record']['captureOverflow'] = True
                    raise ReaderPoolError('reader_pool_capture_limit')

    def _observe_terminal(self, worker):
        if worker['joined'] or worker['process'] is None:
            return
        self._drain(worker)
        if worker['terminal'] is not None:
            return
        try:
            result = os.waitid(os.P_PID, worker['process'].pid, os.WEXITED | os.WNOHANG | os.WNOWAIT)
        except ChildProcessError:
            worker['record']['waitOwnershipLost'] = True
            raise
        if result is not None:
            _need(result.si_pid == worker['process'].pid and
                  result.si_code in (os.CLD_EXITED, os.CLD_KILLED, os.CLD_DUMPED),
                  'reader_pool_waitid_contract')
            worker['terminal'] = {'pid': result.si_pid, 'uid': result.si_uid, 'code': result.si_code,
                                  'status': result.si_status, 'observedAnchor': self.reader.clock_anchor(),
                                  'leaderNotReaped': True}
            worker['record']['terminalObservation'] = copy.deepcopy(worker['terminal'])

    def _owned_group(self, worker):
        _need(not worker['joined'] and worker['process'] is not None and worker['birth'] is not None and
              not worker['record'].get('waitOwnershipLost'),
              'reader_pool_signal_requires_unreaped_birth')
        birth = worker['birth']
        current = _stat_process(worker['process'].pid)
        _need(all(current[key] == birth[key] for key in
                  ('pid', 'parentPid', 'processGroup', 'sessionId', 'startTicks')) and
              current['parentPid'] == self._controller_pid, 'reader_pool_child_lifetime_changed')
        members = self._group(birth['processGroup'])
        _need(any(row['pid'] == birth['pid'] and row['startTicks'] == birth['startTicks'] for row in members) and
              all(row['sessionId'] == birth['sessionId'] for row in members), 'reader_pool_group_scope_changed')
        return members

    def _signal(self, worker, number):
        if worker['joined'] or worker['process'] is None:
            return
        if any(row['signal'] == number for row in worker['record']['signals']):
            return
        members = self._owned_group(worker)
        row = {'signal': number, 'beforeAnchor': self.reader.clock_anchor(), 'members': members,
               'birth': copy.deepcopy(worker['birth']), 'attempted': True, 'submitted': False}
        worker['record']['signals'].append(row)
        worker['record']['forced'] = True
        try:
            os.killpg(worker['birth']['processGroup'], number)
            row['submitted'] = True
        except ProcessLookupError:
            row['groupDisappearedBeforeSignal'] = True
        finally:
            row['afterAnchor'] = self.reader.clock_anchor()

    def _try_join(self, worker, *, validate=True):
        if worker['joined'] or worker['process'] is None:
            return
        self._observe_terminal(worker)
        if worker['terminal'] is None:
            return
        members = self._owned_group(worker)
        if len(members) != 1:
            worker['record']['unexpectedGroupMembers'] = members
            raise ReaderPoolError('reader_pool_unexpected_group_member')
        if not all(stream['eof'] for stream in worker['streams'].values()) or len(worker['streams']) != 2:
            return
        worker['record']['groupObservations'].append({'beforeWait': True,
            'anchor': self.reader.clock_anchor(), 'members': members})
        try:
            pid, status = os.waitpid(worker['process'].pid, 0)
        except ChildProcessError:
            worker['record']['waitOwnershipLost'] = True
            raise
        worker['process'].returncode = os.waitstatus_to_exitcode(status)
        worker['joined'] = True
        row = worker['record']
        row.update(joined=True, wait={'pid': pid, 'status': status,
            'returnCode': worker['process'].returncode, 'exited': os.WIFEXITED(status),
            'exitStatus': os.WEXITSTATUS(status) if os.WIFEXITED(status) else None,
            'signaled': os.WIFSIGNALED(status),
            'signal': os.WTERMSIG(status) if os.WIFSIGNALED(status) else None,
            'anchor': self.reader.clock_anchor()})
        _need(pid == worker['birth']['pid'] and
              worker['terminal']['status'] == (row['wait']['exitStatus'] if row['wait']['exited'] else row['wait']['signal']),
              'reader_pool_wait_status_binding')
        remaining = self._group(worker['birth']['processGroup'])
        row['groupObservations'].append({'beforeWait': False, 'anchor': self.reader.clock_anchor(), 'members': remaining})
        row['groupEmptyAfterWait'] = remaining == []
        _need(not remaining, 'reader_pool_group_not_empty_after_wait')
        if validate:
            worker['validationAttempted'] = True
            worker['normalized'] = self._validate_worker(worker)

    def _ready(self, worker):
        if worker.get('ready') is not None:
            return
        path = worker['phase']['path'] / worker['side'] / 'ready.json'
        if not os.path.lexists(path):
            return
        raw, pin = _read(path, 32768)
        value = self.reader.parse(raw)
        self.reader.keys(value, ('kind', 'version', 'scope', 'phase', 'reader', 'runId', 'input',
                                'worker', 'parentPid', 'processGroup', 'sessionId', 'anchor'),
                         'reader_pool_ready_fields')
        phase = worker['phase']
        _need(value['kind'] == 'native-scan-http-capacity-reader-ready' and type(value['version']) is int and
              value['version'] == 1 and value['scope'] == str(E) and value['phase'] == phase['name'] and
              value['reader'] == worker['side'] and value['runId'] == phase['run']['Id'] and
              value['parentPid'] == self._controller_pid and
              value['processGroup'] == value['sessionId'] == worker['process'].pid,
              'reader_pool_ready_binding')
        input_row = phase['inputs'][worker['side']]
        _pin_read(value['input'], Path(input_row['pin']['path']), 16384, metadata=True)
        _need(_small_pin(value['input']) == input_row['pin'] and
              value['worker'] == self.reader.process_identity(worker['process'].pid) and
              value['worker'] == worker['record'].get('birthProcess'), 'reader_pool_ready_process_binding')
        current = _stat_process(worker['process'].pid)
        _need(all(current[key] == worker['birth'][key] for key in
                  ('pid', 'parentPid', 'processGroup', 'sessionId', 'startTicks')) and current['state'] not in ('Z', 'X'),
              'reader_pool_ready_not_live')
        limits = (Path('/proc') / str(worker['process'].pid) / 'limits').read_text()
        observed_limits = {}
        for title, expected in (('Max address space', self.reader.MEMORY_CAP),
                                ('Max core file size', 0), ('Max file size', 2 << 20)):
            matches = re.findall('^' + re.escape(title) + r'\s+([0-9]+)\s+([0-9]+)\s+bytes\s*$',
                                 limits, re.MULTILINE)
            _need(matches == [(str(expected), str(expected))], 'reader_pool_ready_resource_limits')
            observed_limits[title] = {'soft': expected, 'hard': expected, 'units': 'bytes'}
        worker['record']['readyResourceLimits'] = observed_limits
        self.reader.check_anchor(value['anchor'])
        _need(worker['record']['dispatchAnchor']['monotonicAfterNs'] <= value['anchor']['monotonicBeforeNs'] <=
              value['anchor']['monotonicAfterNs'] <= time.monotonic_ns(), 'reader_pool_ready_anchor_order')
        worker['ready'] = {'value': value, 'pin': _small_pin(pin)}
        worker['record']['ready'] = copy.deepcopy(worker['ready'])

    def start(self, phase, run, mappings):
        self._controller()
        _need(self._first_error is None and self._active is None and phase in PHASES and
              phase not in self._phases and (phase == 'cold' and not self._phases or
              phase == 'cached' and set(self._phases) == {'cold'} and self._phases['cold'].get('joined')),
              'reader_pool_phase_order')
        _need(self.deadline - time.monotonic() > 145 and self.record['childrenCreated'] <= 2 and
              type(mappings) is dict and set(mappings) == {'visible', 'hidden'}, 'reader_pool_pair_admission')
        _need(type(run) is dict and run.get('Source') == 'manual' and run.get('TotalChildren') == 2,
              'reader_pool_run_scope')
        for key in ('Id', 'TaskId', 'RequestId'):
            self.reader.identifier(run.get(key))
        self._controller()
        _pin_read(self._reader_pin, SOURCE, 256 << 10)
        for role in ('controller', 'anchor', 'application'):
            _need(self.reader.process_identity(self._context[role]['pid']) == self._context[role],
                  'reader_pool_bound_' + role + '_changed')
        values = {}
        for role, side, label in SIDES:
            mapping = mappings[role]
            _need(type(mapping) is dict and mapping.get('libraryLabel') == label, 'reader_pool_role_mapping')
            value = {'kind': 'native-scan-http-capacity-reader-input', 'version': 1, 'scope': str(E),
                     'self': dict(self._reader_pin), 'context': dict(self._context_pin),
                     'phase': phase, 'reader': side, 'taskId': run['TaskId'], 'requestId': run['RequestId'],
                     'runId': run['Id'], 'userId': mapping.get('userId'), 'libraryId': mapping.get('libraryId'),
                     'token': mapping.get('token')}
            self.reader.validate_input(value)
            values[side] = value
        _need(values['left']['userId'] != values['right']['userId'] and
              values['left']['libraryId'] != values['right']['libraryId'] and
              values['left']['token'] != values['right']['token'], 'reader_pool_restricted_bindings_aliased')
        if phase == 'cached':
            cold = self._phases['cold']
            _need(run['TaskId'] == cold['run']['TaskId'] and run['Id'] != cold['run']['Id'] and
                  run['RequestId'] != cold['run']['RequestId'] and
                  all(values[side][key] == cold['inputs'][side]['value'][key]
                      for _role, side, _label in SIDES for key in ('userId', 'libraryId', 'token')),
                  'reader_pool_cached_binding_changed')
        allocation = {'kind': 'native-capacity-reader-reservation', 'phase': phase,
                      'workers': 2, 'httpRequests': 120, 'outputBytes': 2 * self.reader.OUTPUT_CAP}
        self.reserve(copy.deepcopy(allocation))
        state = {'name': phase, 'path': self._root / phase, 'run': {key: run[key] for key in ('Id', 'TaskId', 'RequestId')},
                 'inputs': {}, 'workers': [], 'cancel': None, 'start': None, 'joined': False}
        row = {'phase': phase, 'runId': run['Id'], 'allocation': allocation, 'workers': [],
               'startPublished': False, 'joined': False, 'passed': False}
        state['record'] = row
        self._phases[phase] = state
        self.record['phases'][phase] = row
        self._active = phase
        try:
            state['path'].mkdir(mode=0o700)
            _directory(state['path'])
            for _role, side, _label in SIDES:
                pin = _write(state['path'] / (side + '-input.json'), _encoded(values[side]))
                state['inputs'][side] = {'value': values[side], 'pin': pin}
            row['inputs'] = {side: dict(value['pin']) for side, value in state['inputs'].items()}
            for role, side, label in SIDES:
                self._spawn(state, role, side, label)
            ready_deadline = min(self.deadline - JOIN_SECONDS, time.monotonic() + self.reader.START_SECONDS)
            while time.monotonic() < ready_deadline:
                for worker in state['workers']:
                    self._observe_terminal(worker)
                    _need(worker['terminal'] is None, 'reader_pool_exited_before_start')
                    self._ready(worker)
                if all(worker.get('ready') is not None for worker in state['workers']):
                    break
                time.sleep(min(0.02, max(0, ready_deadline - time.monotonic())))
            _need(len(state['workers']) == 2 and all(worker.get('ready') for worker in state['workers']),
                  'reader_pool_ready_pair_timeout')
            event = self._event(state, 'start')
            _need(all(worker['ready']['value']['anchor']['monotonicAfterNs'] <= event['anchor']['monotonicBeforeNs']
                      for worker in state['workers']), 'reader_pool_start_before_ready')
            pin = _publish(state['path'] / 'start.json', event)
            state['start'] = {'event': event, 'source': pin}
            row.update(startPublished=True, start=copy.deepcopy(state['start']))
            row['birthReceipt'] = _write(state['path'] / 'pair-started.json', _encoded(row))
            return copy.deepcopy(row)
        except BaseException as error:
            self._remember(phase, error)
            self._mark_failure(_code(error))
            self._join_pair(state, min(self.deadline, time.monotonic() + JOIN_SECONDS))
            self._raise_new()
            raise

    def _raw_response(self, row, worker):
        output = worker['phase']['path'] / worker['side']
        sequence = row['sequence']
        raw_header = _pin_read(row['rawHeader'], output / ('%03d-response-header.raw' % sequence),
                               self.reader.WIRE_CAP, empty=True)
        wire_body = _pin_read(row['rawWireBody'], output / ('%03d-response-wire-body.raw' % sequence),
                              self.reader.WIRE_CAP, empty=True)
        body = _pin_read(row['body'], output / ('%03d-response-body.raw' % sequence),
                         self.reader.BODY_CAP, empty=True)
        _need(len(raw_header) <= self.reader.HEADER_CAP and
              len(raw_header) + len(wire_body) <= self.reader.WIRE_CAP and len(body) == row['bodyBytes'],
              'reader_pool_response_evidence_bounds')
        stream = _ResponseBuffer(raw_header + wire_body)
        response = http.client.HTTPResponse(_MemorySocket(stream), method='GET')
        try:
            response.begin()
            header = self.reader.response_headers(response, raw_header)
            if header['transferEncoding'] is not None:
                _need(_chunked_body(wire_body, self.reader.BODY_CAP) == body,
                      'reader_pool_chunk_decoding_binding')
            decoded = response.read(self.reader.BODY_CAP + 1)
            consumed = stream.tell() if not stream.closed else stream.consumed
            _need(decoded == body and len(decoded) <= self.reader.BODY_CAP and response.length in (None, 0) and
                  consumed == len(raw_header) + len(wire_body), 'reader_pool_raw_http_framing')
            for key, value in header.items():
                if key == 'headers':
                    value = [list(pair) for pair in value]
                _need(row.get(key) == value, 'reader_pool_raw_header_record_binding')
        finally:
            response.close()
        correctness = self.reader.validate_catalog(body, row['shape'], worker['phase']['name'])
        _need(row['correctness'] == correctness, 'reader_pool_catalog_validation_binding')
        parsed = self.reader.parse(body)
        items = []
        for item in parsed['Items']:
            _need(all(key in item for key in ('Id', 'Name', 'Type', 'IsFolder', 'UserData')) and
                  type(item['Name']) is str and type(item['UserData']) is dict,
                  'reader_pool_projected_item_fields')
            items.append({key: copy.deepcopy(item[key]) for key in ('Id', 'Name', 'Type', 'IsFolder', 'UserData')})
        return {'shape': row['shape'], 'totalRecordCount': parsed['TotalRecordCount'], 'items': items}

    def _request(self, row, index, worker, final, prior_dispatch):
        phase = worker['phase']
        output = phase['path'] / worker['side']
        value = phase['inputs'][worker['side']]['value']
        _need(type(row) is dict and _integer(row.get('sequence'), 1) and row['sequence'] == index and row.get('phase') == phase['name'] and
              row.get('reader') == worker['side'] and row.get('runId') == phase['run']['Id'] and
              row.get('method') == 'GET', 'reader_pool_request_binding')
        shape, target = self.reader.query_shape(value['userId'], index)
        _need(row.get('shape') == shape and row.get('path') == target, 'reader_pool_request_target')
        raw = _pin_read(row['receipt'], output / ('%03d-response.json' % index), 2 << 20)
        _need(self.reader.parse(raw) == {key: part for key, part in row.items() if key != 'receipt'},
              'reader_pool_response_receipt_binding')
        intent = self.reader.parse(_pin_read(row['intent'], output / ('%03d-intent.json' % index), 32768))
        _need(type(intent) is dict and set(intent) == set(INITIAL_REQUEST_FIELDS) and
              all(intent[key] == row[key] for key in
                  ('sequence', 'phase', 'reader', 'runId', 'shape', 'method', 'path', 'intentAnchor')) and
              intent['dispatched'] is False and intent['error'] is None and
              intent['connectionCreated'] is False and intent['bodyComplete'] is False and
              intent['correctness'] == {'passed': False}, 'reader_pool_intent_binding')
        _need(all(intent[key] is None for key in ('dispatchMonotonicNs', 'headersMonotonicNs',
                  'bodyCompleteMonotonicNs', 'connectionCloseMonotonicNs', 'status', 'contentType',
                  'contentLength', 'responseRequestId')) and
              all(intent[key] is False for key in ('bodyComplete', 'connectionClosed', 'responseClosed',
                                                   'connectionCreated', 'responseCreated', 'dispatched')) and
              all(intent[key] == [] for key in ('headers', 'responseCloseErrors', 'connectionCloseErrors')),
              'reader_pool_intent_precedes_exchange')
        self.reader.check_anchor(row['intentAnchor'])
        self.reader.check_anchor(row['finishedAnchor'])
        _need(row['error'] is None and row['connectionClosed'] is True and
              row['responseCloseErrors'] == [] and row['connectionCloseErrors'] == [] and
              _integer(row['evidenceWriteNsBeforeRecord']) and _integer(row['intentEvidenceWriteNs']),
              'reader_pool_request_not_cleanly_closed')
        if row['dispatched'] is False:
            _need(index == len(final['requests']) and
                  ((row.get('cancelledBeforeDispatch') is True and phase.get('cancel') is not None) or
                   row.get('windowClosedBeforeDispatch') is True) and
                  row['dispatchMonotonicNs'] is None and row['responseCreated'] is False and
                  row['responseClosed'] is False and row['rawHeaderComplete'] is False and
                  row['bodyComplete'] is False and type(row['bodyBytes']) is int and row['bodyBytes'] == 0 and
                  row['correctness'] == {'passed': False, 'notApplicable': 'not_dispatched'},
                  'reader_pool_nondispatched_tail_contract')
            for field, suffix in (('rawHeader', 'response-header.raw'), ('rawWireBody', 'response-wire-body.raw'),
                                  ('body', 'response-body.raw')):
                _need(_pin_read(row[field], output / ('%03d-' % index + suffix), 1, empty=True) == b'',
                      'reader_pool_nondispatched_tail_has_response')
            return None, prior_dispatch
        _need(row['dispatched'] is True and phase.get('start') is not None and row['connectionCreated'] is True and
              row['responseCreated'] is True and row['responseClosed'] is True and row['bodyComplete'] is True and
              row['rawHeaderComplete'] is True and row['correctness'].get('passed') is True,
              'reader_pool_dispatched_response_incomplete')
        start = phase['start']['event']['anchor']['monotonicAfterNs']
        phase_end = start + self.reader.PHASE_SECONDS * 1_000_000_000
        times = [row.get(key) for key in ('dispatchMonotonicNs', 'requestSentMonotonicNs',
                  'headersMonotonicNs', 'bodyCompleteMonotonicNs', 'connectionCloseMonotonicNs')]
        _need(all(_integer(part, 1) for part in times) and times == sorted(times) and
              row['intentAnchor']['monotonicAfterNs'] <= times[0] and start <= times[0] < phase_end and
              row['deadlineMonotonicNs'] == min(times[0] + self.reader.HTTP_SECONDS * 1_000_000_000, phase_end) and
              times[3] < row['deadlineMonotonicNs'] and
              (prior_dispatch is None or times[0] >= prior_dispatch + self.reader.SPACING_NS) and
              times[-1] <= row['finishedAnchor']['monotonicAfterNs'], 'reader_pool_http_time_contract')
        self.reader.check_anchor(row['dispatchAnchor'])
        self.reader.check_anchor(row['bodyCompleteAnchor'])
        return self._raw_response(row, worker), times[0]

    def _validate_worker(self, worker):
        row, phase = worker['record'], worker['phase']
        output = phase['path'] / worker['side']
        result, stderr, evidence_errors = None, None, []
        for name in ('stdout', 'stderr'):
            try:
                capture = row[name]
                raw = _pin_read(_small_pin(capture), Path(capture['path']), STREAM_CAP, empty=True)
                if name == 'stdout' and raw:
                    result = self.reader.parse(raw)
                    row['childStdoutSummary'] = result
                elif name == 'stderr':
                    stderr = raw
                _need(capture['eof'] is True and capture['seenBytes'] == capture['bytes'],
                      'reader_pool_capture_not_complete')
            except BaseException as error:
                row[name + 'ReadbackError'] = _code(error)
                evidence_errors.append(error)
        try:
            if os.path.lexists(output / 'receipt.json'):
                raw, observed = _read(output / 'receipt.json', 2 << 20)
                row['readerReceipt'] = _small_pin(observed)
                row['readerReport'] = self.reader.parse(raw)
        except BaseException as error:
            row['readerReceiptReadbackError'] = _code(error)
            evidence_errors.append(error)
        if evidence_errors:
            raise evidence_errors[0]
        _need(worker['joined'] and row.get('groupEmptyAfterWait') is True and
              row['wait']['exited'] is True and row['wait']['exitStatus'] == 0 and
              not row['forced'] and not row.get('captureOverflow') and not row['errors'],
              'reader_pool_child_exit_not_normal')
        _need(stderr == b'', 'reader_pool_child_stderr')
        self.reader.keys(result, ('kind', 'status', 'receipt', 'dispatchedRequests',
                                 'signalNumberAfterReceipt', 'workerRequestedExitCode'),
                         'reader_pool_stdout_fields')
        _need(result['kind'] == 'native-scan-http-capacity-reader-result' and
              result['status'] in NORMAL_STATUSES and result['signalNumberAfterReceipt'] is None and
              result['workerRequestedExitCode'] == 0, 'reader_pool_child_stdout_failed')
        final_raw = _pin_read(result['receipt'], output / 'receipt.json', 2 << 20)
        final = self.reader.parse(final_raw)
        row['readerReceipt'] = dict(result['receipt'])
        row['readerReport'] = final
        value = phase['inputs'][worker['side']]['value']
        _need(final.get('kind') == 'native-scan-http-capacity-reader-receipt' and
              type(final.get('version')) is int and final['version'] == 1 and final.get('scope') == str(E) and
              all(final.get(key) == value[key] for key in
                  ('phase', 'reader', 'taskId', 'requestId', 'runId', 'userId', 'libraryId')) and
              final.get('source') == self._reader_pin and final.get('context') == self._context_pin and
              final.get('librarySide') == ('A' if worker['side'] == 'left' else 'B'), 'reader_pool_final_scope')
        _pin_read(final['input'], Path(value['self']['path']).parent / 'readers' / phase['name'] /
                  (worker['side'] + '-input.json'), 16384, metadata=True)
        _need(_small_pin(final['input']) == phase['inputs'][worker['side']]['pin'] and
              worker.get('ready') is not None and final.get('ready') == worker['ready']['pin'] and
              final.get('worker') == worker['ready']['value']['worker'] and
              final.get('parentPid') == self._controller_pid and
              final.get('processGroup') == final.get('sessionId') == worker['birth']['pid'],
              'reader_pool_final_process_binding')
        _need(self.reader.parse(_pin_read(final['ready'], output / 'ready.json', 32768)) ==
              worker['ready']['value'], 'reader_pool_final_ready_changed')
        _pin_read(self._reader_pin, SOURCE, 256 << 10)
        _need(self.reader.parse(_pin_read(self._context_pin, CONTEXT, 32768)) == self._context,
              'reader_pool_final_context_changed')
        for name in ('start', 'cancel'):
            if phase.get(name) is not None:
                _need(self.reader.parse(_pin_read(phase[name]['source'], phase['path'] / (name + '.json'),
                                                 4096, metadata=True)) == phase[name]['event'],
                      'reader_pool_final_control_changed')
        if final.get('status') == 'cancelled_before_start':
            _need(final.get('start') is None and final.get('requests') == [] and
                  final.get('dispatchedRequests') == 0 and phase.get('cancel') is not None and
                  phase['cancel']['event']['reason'] in ('task_terminal', 'window_end'),
                  'reader_pool_cancel_before_observed_start')
        else:
            _need(final.get('start') == phase.get('start'), 'reader_pool_observed_start_binding')
        _need(final.get('error') is None and
              final.get('status') == result['status'] and final.get('signalNumber') is None and
              final.get('hostNamespaceRestored') is True and
              final.get('namespaceAtFinish') == self._context['hostNetworkNamespace'] and
              final.get('workerFunctionReturned') is True and final.get('workerRequestedExitCode') == 0 and
              final.get('allOwnedConnectionsClosed') is True and
              final.get('actualProcessExitRequiresParentWait') is True, 'reader_pool_final_not_passed')
        for key in ('clockDomainBefore', 'clockDomainAfter'):
            self.reader.check_clock_domain(final.get(key))
        row['clockDomainMatched'] = (self.record['clockDomainBefore'].get('available') is True and
            final['clockDomainBefore'] == final['clockDomainAfter'] == self.record['clockDomainBefore'])
        for name in ('createdAnchor', 'finishedAnchor'):
            self.reader.check_anchor(final[name])
        _need(row['dispatchAnchor']['monotonicBeforeNs'] <= final['createdAnchor']['monotonicBeforeNs'] <=
              worker['ready']['value']['anchor']['monotonicBeforeNs'] <=
              final['finishedAnchor']['monotonicAfterNs'] <= row['terminalObservation']['observedAnchor']['monotonicAfterNs'] <=
              row['wait']['anchor']['monotonicAfterNs'] and _integer(final.get('evidenceBytesBeforeReceipt')) and
              _integer(final.get('evidenceWriteNsBeforeReceipt')), 'reader_pool_final_anchor_order')
        if 'cancel' in final:
            _need(final['cancel'] == phase.get('cancel') and
                  final['cancel']['event']['reason'] != 'controller_failure', 'reader_pool_final_cancel_binding')
        elif final['status'] in ('stopped_by_controller', 'cancelled_before_start'):
            raise ReaderPoolError('reader_pool_stopped_without_cancel')
        _need(type(final.get('requests')) is list and len(final['requests']) <= self.reader.REQUEST_CAP and
              _integer(final.get('dispatchedRequests')) and
              final['dispatchedRequests'] == result['dispatchedRequests'], 'reader_pool_final_request_count')
        expected_limits = {'requests': 60, 'windowSeconds': 120, 'absoluteHttpSeconds': 10,
            'minimumSpacingNs': 2_000_000_000, 'bodyBytes': 512 << 10, 'headerBytes': 32 << 10,
            'wireBytes': 576 << 10, 'addressSpaceBytes': 128 << 20, 'outputBytes': 72 << 20}
        _need(final.get('limits') == expected_limits and final.get('transport') == {
            'origin': self.reader.ORIGIN, 'method': 'GET', 'connection': 'close',
            'acceptEncoding': 'identity', 'retry': False, 'concurrentConnections': 1},
            'reader_pool_final_limits_or_transport')
        pages, previous = [], None
        expected_files = {'ready.json', 'receipt.json'}
        for index, request in enumerate(final['requests'], 1):
            page, previous = self._request(request, index, worker, final, previous)
            if page is not None:
                pages.append(page)
            for suffix in ('intent.json', 'response-header.raw', 'response-wire-body.raw',
                           'response-body.raw', 'response.json'):
                expected_files.add('%03d-' % index + suffix)
        _need(len(pages) == final['dispatchedRequests'] <= 60 and
              {path.name for path in output.iterdir()} == expected_files, 'reader_pool_output_inventory')
        total = 0
        for name in sorted(expected_files):
            path = output / name
            _raw, observed = _read(path, 2 << 20, empty=True)
            total += observed['bytes']
        _need(total <= self.reader.OUTPUT_CAP and
              final['evidenceBytesBeforeReceipt'] + result['receipt']['bytes'] == total,
              'reader_pool_output_byte_accounting')
        if final['status'] == 'request_limit_reached':
            _need(len(pages) == 60, 'reader_pool_request_limit_exit_count')
        if final['status'] == 'window_complete':
            _need(final['finishedAnchor']['monotonicAfterNs'] >=
                  phase['start']['event']['anchor']['monotonicAfterNs'] + 120_000_000_000,
                  'reader_pool_window_exit_early')
        if self.record['controllerFailure'] is not None:
            raise ReaderPoolError('reader_pool_controller_failure_retained')
        observed_ids = sorted({item['Id'] for page in pages for item in page['items']})
        row['verifiedRawEvidence'] = True
        row['actualWorkerOutputBytes'] = total
        return {'role': worker['role'], 'userId': value['userId'], 'libraryId': value['libraryId'],
                'requestCount': len(pages), 'status': 'passed', 'observedItemIds': observed_ids,
                'pages': pages, 'reader': worker['side'], 'rawStatus': final['status'],
                'input': dict(phase['inputs'][worker['side']]['pin']), 'receipt': dict(result['receipt']),
                'wait': copy.deepcopy(row['wait']), 'groupEmptyAfterWait': True,
                'actualWorkerOutputBytes': total,
                'metricCompleteness': 'incomplete_no_requests' if not pages else 'requires_task_interval_reconciliation'}

    def _join_pair(self, phase, absolute_deadline):
        if phase.get('joined'):
            return
        now = time.monotonic()
        if phase.get('joinDeadline') is None:
            phase['joinDeadline'] = min(self.deadline, absolute_deadline, now + JOIN_SECONDS)
            phase['record']['joinStartedAnchor'] = self.reader.clock_anchor()
            phase['record']['joinDeadlineMonotonic'] = phase['joinDeadline']
        end = phase['joinDeadline']
        while True:
            for worker in phase['workers']:
                if worker['process'] is None:
                    worker['joined'] = True
                    worker['record'].update(joined=True, noChildHandleReturned=True)
                    continue
                try:
                    if worker['birth'] is None:
                        self._birth(worker)
                    self._try_join(worker, validate=False)
                except BaseException as error:
                    self._remember(phase['name'], error, worker)
                    self._mark_failure(_code(error))
            if all(worker['joined'] for worker in phase['workers']):
                phase['joined'] = all(worker['record'].get('noChildHandleReturned') or
                                      worker['record'].get('groupEmptyAfterWait') is True
                                      for worker in phase['workers'])
                phase['record']['joined'] = phase['joined']
                break
            now = time.monotonic()
            for number, threshold in ((signal.SIGTERM, end - 3.0), (signal.SIGKILL, end - 1.0)):
                if now >= threshold:
                    for worker in phase['workers']:
                        if worker['joined'] or worker['process'] is None:
                            continue
                        try:
                            if not worker['record']['forced']:
                                self._remember(phase['name'], ReaderPoolError('reader_pool_forced_termination'), worker)
                                self._mark_failure('reader_pool_forced_termination')
                            self._signal(worker, number)
                        except BaseException as error:
                            self._remember(phase['name'], error, worker)
            if time.monotonic() >= end:
                self._remember(phase['name'], ReaderPoolError('reader_pool_join_deadline'))
                phase['record']['joinDeadlineExpired'] = True
                for worker in phase['workers']:
                    try:
                        self._try_join(worker, validate=False)
                    except BaseException as error:
                        self._remember(phase['name'], error, worker)
                phase['joined'] = all(worker['joined'] and (worker['record'].get('noChildHandleReturned') or
                                      worker['record'].get('groupEmptyAfterWait') is True)
                                      for worker in phase['workers'])
                phase['record']['joined'] = phase['joined']
                break
            time.sleep(min(0.02, max(0, end - time.monotonic())))
        for worker in phase['workers']:
            if worker['process'] is None or not worker['joined'] or worker.get('validationAttempted'):
                continue
            worker['validationAttempted'] = True
            try:
                worker['normalized'] = self._validate_worker(worker)
            except BaseException as error:
                self._remember(phase['name'], error, worker)
                self._mark_failure(_code(error))
        workers = []
        for worker in phase['workers']:
            if worker.get('normalized') is not None:
                workers.append(copy.deepcopy(worker['normalized']))
            else:
                row = worker['record']
                workers.append({'role': worker['role'], 'userId': row['userId'], 'libraryId': row['libraryId'],
                    'status': 'failed', 'requestCount': None, 'observedItemIds': [], 'pages': [],
                    'reader': worker['side'], 'joined': worker['joined'], 'errors': copy.deepcopy(row['errors']),
                    'wait': copy.deepcopy(row.get('wait')), 'readerReceipt': copy.deepcopy(row.get('readerReceipt'))})
        passed = (phase['joined'] and len(workers) == 2 and all(row['status'] == 'passed' for row in workers) and
                  self._first_error is None and self.record['controllerFailure'] is None)
        result = {'phase': phase['name'], 'runId': phase['run']['Id'], 'joined': phase['joined'],
                  'workers': workers, 'status': 'passed' if passed else 'failed',
                  'allocation': copy.deepcopy(phase['record']['allocation']),
                  'actualCountsComplete': passed, 'actualRequests': sum(row['requestCount'] for row in workers) if passed else None,
                  'actualWorkerOutputBytes': sum(row['actualWorkerOutputBytes'] for row in workers) if passed else None,
                  'unusedAllocationMayBeReleased': passed,
                  'metricCompleteness': 'requires_task_interval_reconciliation',
                  'cancel': copy.deepcopy(phase.get('cancel'))}
        phase['record'].update(passed=passed, result=result)
        self.record['clockDomainAfter'] = self.reader.clock_domain()
        if phase['joined']:
            self._active = None
        self.record['allChildrenJoined'] = all(row.get('joined') for row in self._phases.values())

    def poll(self):
        self._controller()
        if self._active is None:
            self._raise_new()
            return self.record
        phase = self._phases[self._active]
        failed = False
        for worker in phase['workers']:
            try:
                self._try_join(worker)
            except BaseException as error:
                self._remember(phase['name'], error, worker)
                failed = True
        if failed:
            self._mark_failure(_code(self._first_error))
            self._join_pair(phase, min(self.deadline, time.monotonic() + JOIN_SECONDS))
            self._raise_new()
        return self.record

    def finish(self, phase, absolute_deadline):
        self._controller(cleanup=True)
        _need(phase in self._phases and type(absolute_deadline) in (int, float) and
              math.isfinite(absolute_deadline), 'reader_pool_finish_scope')
        state = self._phases[phase]
        if not state.get('joined'):
            try:
                reason = 'controller_failure' if self.record['controllerFailure'] is not None else (
                    'window_end' if state.get('start') is not None and time.monotonic_ns() >=
                    state['start']['event']['anchor']['monotonicAfterNs'] + 120_000_000_000 else 'task_terminal')
                self._cancel(state, reason)
            except BaseException as error:
                self._remember(phase, error)
                self._mark_failure(_code(error))
            self._join_pair(state, min(self.deadline, absolute_deadline))
        self._raise_new()
        _need(state.get('joined'), 'reader_pool_owned_children_not_joined')
        return copy.deepcopy(state['record']['result'])
