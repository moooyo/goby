"""One fixed, single-threaded native catalog reader; no workload is admitted yet.

Run only on test-env as root with /usr/bin/python3 -I -B. The controller owns
start_new_session=True, both reader groups, cancellation, waitpid, and exit facts.
This module starts no process or service and accepts no arbitrary URL or method.

All JSON objects below have exactly the documented members. A descriptor is
{path, sha256, bytes}. Identifiers are lowercase 32-hex Emby/task identifiers.

Input: E/private/readers/{phase}/{reader}-input.json, at most 16 KiB:
  kind="native-scan-http-capacity-reader-input", version=1, scope=str(E),
  self=descriptor(E/private/capacity-reader.py),
  context=descriptor(E/private/reader-context.json), phase="cold"|"cached",
  reader="left"|"right", taskId, requestId, runId, userId, libraryId, token.
requestId is the controller's new 32-hex task-admission RequestId. token is the
normal restricted user's canonical 43-character base64url token, never admin.
left is library A; right is library B. The controller verifies policy binding.

Context: E/private/reader-context.json, at most 32 KiB:
  kind="native-scan-http-capacity-reader-context", version=1, scope=str(E),
  fixtureRoot=str(F), origin="http://127.0.0.1:18099", bootId,
  hostNetworkNamespace, networkNamespace, anchor, application, controller.
Each process has {pid, startTicks, uids, gids, exe, executableDevice,
  executableInode, cgroup, networkNamespace}. uids/gids are four-entry integer
lists. anchor/controller are root; application is UID 995/GID 986 and its
executable is a regular file strictly below F. The parent PID is controller.pid.
The context is new and contains no preexisting environment/key material.

The controller creates E/private/readers/{phase}, but the worker alone creates
its exclusive {reader}/ output directory. All directories are root:root 0700;
all files are root:root 0600 regular single-link files. Each worker publishes
ready.json before waiting, then receipt.json at termination. The controller
must verify both ready receipts, including worker process/start/group identity,
and atomically publish shared start.json only after both readers are ready.

start.json: {kind="native-scan-http-capacity-reader-start", version=1,
  scope, phase, runId, inputs={left:inputSha256,right:inputSha256}, anchor}.
anchor is {monotonicBeforeNs, wallTimeNs, monotonicAfterNs}; sample the wall clock
between the two monotonic reads. start time is anchor.monotonicAfterNs. The
120-second window is fixed from that value, not from either reader's discovery.
Publish once through a same-directory private temporary file and os.rename
after fsync; never replace a consumed control event. The phase input hashes
must differ. The start anchor must follow this reader's ready anchor and be no
more than ten seconds old when first read. Ready wait is at most ten seconds.

cancel.json has the same fields and binding, kind ending in "reader-cancel",
plus reason="task_terminal"|"controller_failure"|"window_end". It may precede
start during cleanup. It prevents all new dispatches. task_terminal/window_end
allow the currently owned exchange to finish within its unchanged deadline;
controller_failure aborts it promptly. SIGTERM/SIGINT abort the exchange and are
recorded as failed normal exit. The parent still owns a 15-second join ceiling
and any exact-group SIGTERM/SIGKILL fallback, and must record actual wait status.

receipt.json is a worker statement, never proof of process exit or join. A
cancelled/failed partial exchange remains partial. A successful row validation
is only response-local; every observed ID and cached UserData must additionally
be reconciled by the controller against its authoritative terminal SQL map.
Raw headers and transfer-encoded body are preserved separately from decoded
JSON body. All are private. No token is stored in any output or request header
list. Intent fsync precedes dispatch; HTTP latency excludes evidence writes.
"""

import argparse
import base64
from contextlib import contextmanager
import errno
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import resource
import select
import signal
import socket
import stat
import sys
import time


E = Path('/opt/goby-test/native-scan-http-capacity-20260915')
F = Path('/opt/goby-native-scan-http-capacity-20260915')
SOURCE = E / 'private/capacity-reader.py'
CONTEXT = E / 'private/reader-context.json'
ORIGIN = 'http://127.0.0.1:18099'
PHASE_SECONDS = 120
HTTP_SECONDS = 10
START_SECONDS = 10
SPACING_NS = 2_000_000_000
REQUEST_CAP = 60
BODY_CAP = 512 << 10
HEADER_CAP = 32 << 10
WIRE_CAP = BODY_CAP + (64 << 10)
OUTPUT_CAP = 72 << 20
MEMORY_CAP = 128 << 20
PROCESS_FIELDS = {'pid', 'startTicks', 'uids', 'gids', 'exe',
                  'executableDevice', 'executableInode', 'cgroup', 'networkNamespace'}
SIGNAL_NUMBER = None


class Rejected(Exception):
    """A fixed error code without user-controlled or credential content."""


def need(condition, code):
    if not condition:
        raise Rejected(code)


def error_code(error):
    return str(error) if isinstance(error, Rejected) else type(error).__name__


def keys(value, expected, code):
    need(type(value) is dict and set(value) == set(expected), code)


def integer(value, minimum=0):
    return type(value) is int and value >= minimum


def identifier(value):
    need(type(value) is str and re.fullmatch('[0-9a-f]{32}', value) is not None,
         'identifier_contract')
    return value


def sha(value):
    need(type(value) is str and re.fullmatch('[0-9a-f]{64}', value) is not None,
         'sha256_contract')
    return value


def parse(raw):
    def unique(pairs):
        result = dict(pairs)
        need(len(result) == len(pairs), 'duplicate_json_member')
        return result

    def constant(_value):
        raise Rejected('nonfinite_json_number')

    return json.loads(raw.decode('utf-8', 'strict'), object_pairs_hook=unique,
                      parse_constant=constant)


def encoded(value):
    return (json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + '\n').encode()


def clock_anchor():
    before = time.monotonic_ns()
    wall = time.time_ns()
    after = time.monotonic_ns()
    return {'monotonicBeforeNs': before, 'wallTimeNs': wall,
            'monotonicAfterNs': after}


def check_anchor(value):
    keys(value, ('monotonicBeforeNs', 'wallTimeNs', 'monotonicAfterNs'), 'clock_anchor_contract')
    need(all(integer(part, 1) for part in value.values()) and
         value['monotonicBeforeNs'] <= value['monotonicAfterNs'] and
         value['monotonicAfterNs'] - value['monotonicBeforeNs'] <= 1_000_000_000,
         'clock_anchor_order')
    return value


def query_shape(user_id, sequence):
    """Return exactly one of the two admitted GET targets; this is a pure helper."""
    identifier(user_id)
    need(integer(sequence, 1) and sequence <= REQUEST_CAP, 'request_sequence_contract')
    prefix = '/emby/Users/' + user_id + '/Items?Recursive=true&IncludeItemTypes=Movie,Episode,Audio'
    if sequence % 2:
        return 'page', prefix + '&SortBy=SortName&StartIndex=0&Limit=64'
    return 'count', prefix + '&Limit=0'


def validate_catalog(raw, shape, phase):
    """Check the real Items envelope, leaving terminal-map/ACL reconciliation external."""
    need(shape in ('page', 'count') and phase in ('cold', 'cached'), 'catalog_check_contract')
    need(type(raw) is bytes and len(raw) <= BODY_CAP, 'catalog_body_limit')
    value = parse(raw)
    keys(value, ('Items', 'TotalRecordCount'), 'catalog_envelope_contract')
    rows, total = value['Items'], value['TotalRecordCount']
    need(type(rows) is list and integer(total) and total <= 500, 'catalog_count_type_or_bound')
    expected = min(64, total) if shape == 'page' else 0
    need(len(rows) == expected, 'catalog_pagination_contract')
    if phase == 'cached':
        need(total == 500, 'cached_catalog_count')
    observed = []
    for row in rows:
        need(type(row) is dict, 'catalog_row_type')
        item_id = identifier(row.get('Id'))
        need(row.get('Type') in ('Movie', 'Episode', 'Audio') and
             row.get('IsFolder') is False, 'catalog_leaf_contract')
        observed.append({'id': item_id, 'kind': row['Type']})
    need(len({row['id'] for row in observed}) == len(observed), 'catalog_duplicate_id')
    return {'passed': True, 'totalRecordCount': total, 'pageLength': len(rows),
            'observedRows': observed, 'terminalMapReconciliation': 'required',
            'cachedUserDataReconciliation': 'required' if phase == 'cached' else 'not_applicable'}


def check_process_schema(value, role, host_ns, private_ns):
    keys(value, PROCESS_FIELDS, 'process_descriptor_contract')
    need(integer(value['pid'], 2) and type(value['startTicks']) is str and
         re.fullmatch('[1-9][0-9]*', value['startTicks']) is not None,
         'process_birth_contract')
    uid, gid = (995, 986) if role == 'application' else (0, 0)
    for field, expected in (('uids', uid), ('gids', gid)):
        need(type(value[field]) is list and len(value[field]) == 4 and
             all(type(part) is int and part == expected for part in value[field]),
             'process_account_contract')
    need(type(value['exe']) is str and value['exe'].startswith('/') and
         '..' not in value['exe'].split('/') and
         integer(value['executableDevice']) and integer(value['executableInode'], 1) and
         type(value['cgroup']) is str and value['cgroup'].startswith('0::/') and
         '\n' not in value['cgroup'], 'process_executable_or_cgroup_contract')
    if role == 'application':
        need(value['exe'].startswith(str(F) + '/'), 'application_outside_fixture')
    need(value['networkNamespace'] == (host_ns if role == 'controller' else private_ns),
         'process_namespace_contract')


def validate_context(value):
    keys(value, ('kind', 'version', 'scope', 'fixtureRoot', 'origin', 'bootId',
                 'hostNetworkNamespace', 'networkNamespace', 'anchor', 'application', 'controller'),
         'reader_context_contract')
    need(value['kind'] == 'native-scan-http-capacity-reader-context' and
         type(value['version']) is int and value['version'] == 1 and
         value['scope'] == str(E) and value['fixtureRoot'] == str(F) and
         value['origin'] == ORIGIN, 'reader_context_scope')
    need(type(value['bootId']) is str and
         re.fullmatch('[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}',
                      value['bootId']) is not None, 'boot_id_contract')
    for field in ('hostNetworkNamespace', 'networkNamespace'):
        need(type(value[field]) is str and re.fullmatch(r'net:\[[1-9][0-9]*\]', value[field]) is not None,
             'namespace_descriptor_contract')
    need(value['hostNetworkNamespace'] != value['networkNamespace'], 'namespace_not_isolated')
    for role in ('anchor', 'application', 'controller'):
        check_process_schema(value[role], role, value['hostNetworkNamespace'], value['networkNamespace'])
    need(len({value[role]['pid'] for role in ('anchor', 'application', 'controller')}) == 3,
         'process_identity_alias')
    return value


def descriptor(value, path, maximum):
    keys(value, ('path', 'sha256', 'bytes'), 'file_descriptor_contract')
    need(value['path'] == str(path) and integer(value['bytes'], 1) and
         value['bytes'] <= maximum, 'file_descriptor_scope')
    sha(value['sha256'])


def validate_input(value):
    keys(value, ('kind', 'version', 'scope', 'self', 'context', 'phase', 'reader',
                 'taskId', 'requestId', 'runId', 'userId', 'libraryId', 'token'),
         'reader_input_contract')
    need(value['kind'] == 'native-scan-http-capacity-reader-input' and
         type(value['version']) is int and value['version'] == 1 and value['scope'] == str(E) and
         value['phase'] in ('cold', 'cached') and value['reader'] in ('left', 'right'),
         'reader_input_scope')
    descriptor(value['self'], SOURCE, 256 << 10)
    descriptor(value['context'], CONTEXT, 32 << 10)
    for field in ('taskId', 'requestId', 'runId', 'userId', 'libraryId'):
        identifier(value[field])
    token = value['token']
    need(type(token) is str and re.fullmatch('[A-Za-z0-9_-]{43}', token) is not None,
         'credential_shape')
    decoded = base64.urlsafe_b64decode(token + '=')
    need(len(decoded) == 32 and base64.urlsafe_b64encode(decoded).decode().rstrip('=') == token,
         'credential_encoding')
    return value


def private_directory(path):
    need(path.is_absolute() and path.resolve() == path and path.is_relative_to(E),
         'private_directory_scope')
    for current in (path, *path.parents):
        info = current.lstat()
        need(stat.S_ISDIR(info.st_mode) and not stat.S_ISLNK(info.st_mode), 'directory_symlink')
        if current == E or current.is_relative_to(E):
            need(info.st_uid == 0 and info.st_gid == 0 and stat.S_IMODE(info.st_mode) == 0o700,
                 'private_directory_permissions')


def file_identity(info):
    return {'device': info.st_dev, 'inode': info.st_ino, 'uid': info.st_uid, 'gid': info.st_gid,
            'mode': stat.S_IMODE(info.st_mode), 'bytes': info.st_size, 'links': info.st_nlink,
            'mtimeNs': info.st_mtime_ns, 'ctimeNs': info.st_ctime_ns}


def read_private(path, maximum):
    private_directory(path.parent)
    fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW)
    try:
        before = os.fstat(fd)
        need(stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and
             stat.S_IMODE(before.st_mode) == 0o600 and before.st_nlink == 1 and
             0 < before.st_size <= maximum, 'private_file_contract')
        chunks, count = [], 0
        while count <= maximum:
            part = os.read(fd, min(65536, maximum + 1 - count))
            if not part:
                break
            chunks.append(part)
            count += len(part)
        raw = b''.join(chunks)
        need(len(raw) == before.st_size and file_identity(before) == file_identity(os.fstat(fd)) and
             file_identity(before) == file_identity(path.lstat()), 'private_file_changed')
        return raw, {'path': str(path), 'sha256': hashlib.sha256(raw).hexdigest(),
                     'bytes': len(raw), 'metadata': file_identity(before)}
    finally:
        os.close(fd)


def pinned(value, path, maximum):
    descriptor(value, path, maximum)
    raw, observed = read_private(path, maximum)
    need(all(observed[key] == value[key] for key in ('path', 'sha256', 'bytes')), 'file_pin_changed')
    return raw


def process_identity(pid):
    root = Path('/proc') / str(pid)
    before = (root / 'stat').read_text().rsplit(') ', 1)[1].split()
    status = dict(line.split(':', 1) for line in (root / 'status').read_text().splitlines() if ':' in line)
    executable = (root / 'exe').stat()
    value = {'pid': pid, 'startTicks': before[19],
             'uids': [int(part) for part in status['Uid'].split()],
             'gids': [int(part) for part in status['Gid'].split()],
             'exe': os.readlink(root / 'exe'), 'executableDevice': executable.st_dev,
             'executableInode': executable.st_ino, 'cgroup': (root / 'cgroup').read_text().strip(),
             'networkNamespace': os.readlink(root / 'ns/net')}
    after = (root / 'stat').read_text().rsplit(') ', 1)[1].split()
    need(before[0] not in ('Z', 'X') and after[0] not in ('Z', 'X') and before[19] == after[19],
         'bound_process_changed_during_read')
    return value


def check_bound_processes(context):
    need(Path('/proc/sys/kernel/random/boot_id').read_text().strip() == context['bootId'],
         'boot_changed')
    for role in ('anchor', 'application', 'controller'):
        need(process_identity(context[role]['pid']) == context[role], role + '_identity_changed')
    need(os.getppid() == context['controller']['pid'], 'reader_parent_changed')


@contextmanager
def app_network(context):
    """Reuse the reviewed one-thread namespace discipline with fresh process pins."""
    need(hasattr(os, 'setns') and len(list(Path('/proc/self/task').iterdir())) == 1,
         'single_threaded_setns_required')
    check_bound_processes(context)
    host = os.readlink('/proc/self/ns/net')
    target = '/proc/' + str(context['anchor']['pid']) + '/ns/net'
    need(host == context['hostNetworkNamespace'] == os.readlink('/proc/1/ns/net') and
         os.readlink(target) == context['networkNamespace'], 'namespace_context_changed')
    host_fd = os.open('/proc/self/ns/net', os.O_RDONLY | os.O_CLOEXEC)
    target_fd = None
    try:
        target_fd = os.open(target, os.O_RDONLY | os.O_CLOEXEC)
        need('net:[' + str(os.fstat(target_fd).st_ino) + ']' == context['networkNamespace'],
             'opened_namespace_changed')
        check_bound_processes(context)
        os.setns(target_fd, 0)
        need(os.readlink('/proc/self/ns/net') == context['networkNamespace'], 'namespace_not_entered')
        yield
    finally:
        try:
            if os.readlink('/proc/self/ns/net') != host:
                os.setns(host_fd, 0)
            need(os.readlink('/proc/self/ns/net') == host, 'host_namespace_not_restored')
        finally:
            if target_fd is not None:
                os.close(target_fd)
            os.close(host_fd)
    check_bound_processes(context)


def signal_handler(number, _frame):
    global SIGNAL_NUMBER
    SIGNAL_NUMBER = number


class WireFile:
    """A private HTTPResponse file with an absolute deadline on every socket read.

    HTTPResponse header and chunk parsing may perform multiple blocking reads.
    All such reads pass here, so a slow-drip header or chunk cannot restart a
    socket-relative timeout. No shared http.client object is replaced.
    """
    def __init__(self, sock, checkpoint, deadline):
        self.sock, self.checkpoint, self.deadline = sock, checkpoint, deadline
        self.buffer = bytearray()
        self.raw = bytearray()
        self.header_end = None
        self.eof = False
        self.closed = False

    def fill(self):
        need(not self.closed, 'response_file_closed_early')
        while not self.eof:
            self.checkpoint()
            timeout = min(0.05, max(0, (self.deadline - time.monotonic_ns()) / 1_000_000_000))
            readable, _, _ = select.select([self.sock], [], [], timeout)
            if not readable:
                continue
            self.checkpoint()
            try:
                part = self.sock.recv(min(65536, WIRE_CAP + 1 - len(self.raw)))
            except BlockingIOError:
                continue
            if not part:
                self.eof = True
                self.checkpoint()
                return
            self.raw.extend(part)
            self.buffer.extend(part)
            need(len(self.raw) <= WIRE_CAP, 'wire_response_limit')
            if self.header_end is None:
                end = self.raw.find(b'\r\n\r\n')
                if end >= 0:
                    self.header_end = end + 4
                    need(self.header_end <= HEADER_CAP, 'response_header_limit')
                else:
                    need(len(self.raw) <= HEADER_CAP, 'response_header_limit')
            self.checkpoint()
            return

    def read(self, amount=-1):
        if amount < 0:
            while not self.eof:
                self.fill()
            amount = len(self.buffer)
        while len(self.buffer) < amount and not self.eof:
            self.fill()
        self.checkpoint()
        result = bytes(self.buffer[:amount])
        del self.buffer[:amount]
        return result

    def read1(self, amount=-1):
        if not self.buffer and not self.eof:
            self.fill()
        return self.read(min(len(self.buffer), amount) if amount >= 0 else len(self.buffer))

    def readline(self, limit=-1):
        maximum = HEADER_CAP + 1 if limit < 0 else min(limit, HEADER_CAP + 1)
        while True:
            self.checkpoint()
            end = self.buffer.find(b'\n', 0, maximum)
            if end >= 0:
                return self.read(end + 1)
            if len(self.buffer) >= maximum or self.eof:
                return self.read(min(len(self.buffer), maximum))
            self.fill()

    def close(self):
        self.closed = True

    def flush(self):
        """No buffered writes exist on this read-only response file."""
        pass


class ResponseSocket:
    def __init__(self, wire):
        self.wire = wire

    def makefile(self, mode):
        need(mode == 'rb', 'response_file_mode')
        return self.wire


def response_headers(response, raw):
    need(raw.startswith(b'HTTP/1.1 200 ') and raw.endswith(b'\r\n\r\n'), 'response_status_line')
    rows = raw[:-4].split(b'\r\n')[1:]
    need(all(row and row[:1] not in (b' ', b'\t') and b':' in row for row in rows),
         'response_header_syntax')
    pairs = response.getheaders()
    values = {}
    for name, value in pairs:
        values.setdefault(name.lower(), []).append(value)
    for name in ('content-type', 'content-length', 'transfer-encoding', 'content-encoding', 'x-request-id'):
        need(len(values.get(name, [])) <= 1, 'duplicate_response_header')
    content_type = values.get('content-type', [''])[0]
    need(content_type.split(';', 1)[0].strip().lower() == 'application/json', 'response_content_type')
    need(values.get('content-encoding', ['identity'])[0].lower() == 'identity', 'response_content_encoding')
    request_id = identifier(values.get('x-request-id', [None])[0])
    length = values.get('content-length', [None])[0]
    transfer = values.get('transfer-encoding', [None])[0]
    need(not (length is not None and transfer is not None), 'ambiguous_response_framing')
    if length is not None:
        need(re.fullmatch('[0-9]+', length) is not None and int(length) <= BODY_CAP,
             'response_content_length')
    need(transfer is None or transfer.lower() == 'chunked', 'response_transfer_encoding')
    return {'status': response.status, 'contentType': content_type, 'contentLength': length,
            'transferEncoding': transfer, 'responseRequestId': request_id, 'headers': pairs}


class Reader:
    def __init__(self, value, context, input_pin):
        self.value, self.context, self.input_pin = value, context, input_pin
        self.phase_dir = E / 'private/readers' / value['phase']
        self.output = self.phase_dir / value['reader']
        self.bytes_written = 0
        self.evidence_write_ns = 0
        self.start = None
        self.cancel = None
        self.cancel_pin = None
        self.output.mkdir(mode=0o700)
        private_directory(self.output)
        self.report = {'kind': 'native-scan-http-capacity-reader-receipt', 'version': 1,
            'scope': str(E), 'phase': value['phase'], 'reader': value['reader'],
            'librarySide': 'A' if value['reader'] == 'left' else 'B',
            'taskId': value['taskId'], 'requestId': value['requestId'], 'runId': value['runId'],
            'userId': value['userId'], 'libraryId': value['libraryId'],
            'input': input_pin, 'source': value['self'], 'context': value['context'],
            'createdAnchor': clock_anchor(), 'worker': process_identity(os.getpid()),
            'parentPid': os.getppid(), 'processGroup': os.getpgrp(), 'sessionId': os.getsid(0),
            'limits': {'requests': REQUEST_CAP, 'windowSeconds': PHASE_SECONDS,
                'absoluteHttpSeconds': HTTP_SECONDS, 'minimumSpacingNs': SPACING_NS,
                'bodyBytes': BODY_CAP, 'headerBytes': HEADER_CAP, 'wireBytes': WIRE_CAP,
                'addressSpaceBytes': MEMORY_CAP, 'outputBytes': OUTPUT_CAP},
            'transport': {'origin': ORIGIN, 'method': 'GET', 'connection': 'close',
                'acceptEncoding': 'identity', 'retry': False, 'concurrentConnections': 1},
            'status': 'starting', 'requests': [], 'dispatchedRequests': 0,
            'error': None, 'allOwnedConnectionsClosed': True, 'actualProcessExitRequiresParentWait': True}

    def save(self, name, raw):
        need(re.fullmatch('[a-z0-9][a-z0-9.-]*', name) is not None, 'output_name_contract')
        need(type(raw) is bytes and len(raw) <= 2 << 20 and
             self.bytes_written + len(raw) <= OUTPUT_CAP, 'worker_evidence_budget')
        started = time.monotonic_ns()
        path = self.output / name
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_CLOEXEC | os.O_NOFOLLOW, 0o600)
        try:
            offset = 0
            while offset < len(raw):
                written = os.write(fd, raw[offset:])
                need(written > 0, 'evidence_write_incomplete')
                offset += written
            os.fsync(fd)
            info = os.fstat(fd)
            need(info.st_size == len(raw) and info.st_uid == info.st_gid == 0 and
                 stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1, 'evidence_file_identity')
        finally:
            os.close(fd)
        self.bytes_written += len(raw)
        self.evidence_write_ns += time.monotonic_ns() - started
        return {'path': str(path), 'sha256': hashlib.sha256(raw).hexdigest(), 'bytes': len(raw)}

    def event(self, name, cancel=False):
        path = self.phase_dir / name
        try:
            raw, pin = read_private(path, 4096)
        except FileNotFoundError:
            return None
        value = parse(raw)
        fields = {'kind', 'version', 'scope', 'phase', 'runId', 'inputs', 'anchor'}
        if cancel:
            fields.add('reason')
        keys(value, fields, 'control_event_contract')
        need(value['kind'] == 'native-scan-http-capacity-reader-' + ('cancel' if cancel else 'start') and
             type(value['version']) is int and value['version'] == 1 and value['scope'] == str(E) and
             value['phase'] == self.value['phase'] and value['runId'] == self.value['runId'],
             'control_event_binding')
        keys(value['inputs'], ('left', 'right'), 'control_inputs_contract')
        for input_sha in value['inputs'].values():
            sha(input_sha)
        need(value['inputs']['left'] != value['inputs']['right'] and
             value['inputs'][self.value['reader']] == self.input_pin['sha256'], 'control_input_binding')
        check_anchor(value['anchor'])
        need(value['anchor']['monotonicAfterNs'] <= time.monotonic_ns(), 'control_anchor_in_future')
        if cancel:
            need(value['reason'] in ('task_terminal', 'controller_failure', 'window_end'),
                 'cancel_reason_contract')
        return value, pin

    def checkpoint(self, deadline=None, in_flight=False):
        need(SIGNAL_NUMBER is None, 'reader_signal_received')
        if self.cancel is None:
            event = self.event('cancel.json', cancel=True)
            if event is not None:
                self.cancel, self.cancel_pin = event
                self.report['cancelObservedAnchor'] = clock_anchor()
                self.report['cancel'] = {'event': self.cancel, 'source': self.cancel_pin}
        if self.cancel is not None:
            need(not in_flight or self.cancel['reason'] != 'controller_failure', 'controller_failure_cancel')
        if deadline is not None:
            need(time.monotonic_ns() < deadline, 'absolute_http_deadline')

    def wait_start(self):
        ready = {'kind': 'native-scan-http-capacity-reader-ready', 'version': 1,
                 'scope': str(E), 'phase': self.value['phase'], 'reader': self.value['reader'],
                 'runId': self.value['runId'], 'input': self.input_pin,
                 'worker': self.report['worker'], 'parentPid': os.getppid(),
                 'processGroup': os.getpgrp(), 'sessionId': os.getsid(0), 'anchor': clock_anchor()}
        ready_pin = self.save('ready-pending.json', encoded(ready))
        ready_path = self.output / 'ready.json'
        need(not ready_path.exists(), 'ready_receipt_already_exists')
        publication_started = time.monotonic_ns()
        os.rename(self.output / 'ready-pending.json', ready_path)
        directory_fd = os.open(self.output, os.O_RDONLY | os.O_CLOEXEC | os.O_DIRECTORY)
        try:
            os.fsync(directory_fd)
        finally:
            os.close(directory_fd)
        self.evidence_write_ns += time.monotonic_ns() - publication_started
        self.report['ready'] = dict(ready_pin, path=str(ready_path))
        deadline = time.monotonic_ns() + START_SECONDS * 1_000_000_000
        while time.monotonic_ns() < deadline:
            self.checkpoint()
            if self.cancel is not None:
                self.report['status'] = 'cancelled_before_start'
                return False
            event = self.event('start.json')
            if event is not None:
                self.start, pin = event
                started = self.start['anchor']['monotonicAfterNs']
                need(ready['anchor']['monotonicAfterNs'] <= started and
                     time.monotonic_ns() - started <= START_SECONDS * 1_000_000_000,
                     'start_anchor_stale_or_before_ready')
                self.report['start'] = {'event': self.start, 'source': pin}
                self.report['startObservedAnchor'] = clock_anchor()
                self.phase_deadline = started + PHASE_SECONDS * 1_000_000_000
                return True
            time.sleep(0.05)
        raise Rejected('reader_start_timeout')

    def exchange(self, sequence):
        shape, target = query_shape(self.value['userId'], sequence)
        record = {'sequence': sequence, 'phase': self.value['phase'], 'reader': self.value['reader'],
                  'runId': self.value['runId'], 'shape': shape, 'method': 'GET', 'path': target,
                  'dispatchMonotonicNs': None, 'headersMonotonicNs': None,
                  'bodyCompleteMonotonicNs': None, 'connectionCloseMonotonicNs': None,
                  'status': None, 'contentType': None, 'contentLength': None,
                  'responseRequestId': None, 'headers': [],
                  'bodyComplete': False, 'connectionClosed': False, 'responseClosed': False,
                  'connectionCreated': False, 'responseCreated': False,
                  'responseCloseErrors': [], 'connectionCloseErrors': [],
                  'dispatched': False, 'error': None, 'correctness': {'passed': False}}
        self.report['requests'].append(record)
        record['intentAnchor'] = clock_anchor()
        intent_write_before = self.evidence_write_ns
        record['intent'] = self.save('%03d-intent.json' % sequence, encoded(record))
        record['intentEvidenceWriteNs'] = self.evidence_write_ns - intent_write_before
        wire, response, sock = None, None, None
        chunks = []
        write_before = self.evidence_write_ns
        body_size = 0
        caught = None
        try:
            with app_network(self.context):
                try:
                    self.checkpoint()
                    need(self.cancel is None, 'cancel_before_dispatch')
                    need(time.monotonic_ns() < self.phase_deadline, 'phase_window_closed_before_dispatch')
                    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
                    record['connectionCreated'] = True
                    sock.setblocking(False)
                    self.checkpoint()
                    need(self.cancel is None, 'cancel_before_dispatch')
                    record['dispatchAnchor'] = clock_anchor()
                    record['dispatchMonotonicNs'] = time.monotonic_ns()
                    need(record['dispatchMonotonicNs'] < self.phase_deadline,
                         'phase_window_closed_before_dispatch')
                    deadline = min(record['dispatchMonotonicNs'] + HTTP_SECONDS * 1_000_000_000,
                                   self.phase_deadline)
                    record['deadlineMonotonicNs'] = deadline
                    record['dispatched'] = True
                    self.report['dispatchedRequests'] += 1
                    status = sock.connect_ex(('127.0.0.1', 18099))
                    need(status in (0, errno.EINPROGRESS, errno.EWOULDBLOCK, errno.EALREADY),
                         'http_connect_failed')
                    while status != 0:
                        self.checkpoint(deadline, True)
                        timeout = min(0.05, max(0, (deadline - time.monotonic_ns()) / 1_000_000_000))
                        _, writable, _ = select.select([], [sock], [], timeout)
                        if writable:
                            self.checkpoint(deadline, True)
                            status = sock.getsockopt(socket.SOL_SOCKET, socket.SO_ERROR)
                            need(status == 0, 'http_connect_failed')
                    outgoing = ('GET ' + target + ' HTTP/1.1\r\nHost: 127.0.0.1:18099\r\n'
                                'Accept: application/json\r\nAccept-Encoding: identity\r\n'
                                'Connection: close\r\nX-Emby-Token: ' + self.value['token'] + '\r\n\r\n').encode('ascii')
                    offset = 0
                    while offset < len(outgoing):
                        self.checkpoint(deadline, True)
                        timeout = min(0.05, max(0, (deadline - time.monotonic_ns()) / 1_000_000_000))
                        _, writable, _ = select.select([], [sock], [], timeout)
                        if not writable:
                            continue
                        self.checkpoint(deadline, True)
                        try:
                            sent = sock.send(outgoing[offset:])
                        except BlockingIOError:
                            continue
                        need(sent > 0, 'http_send_incomplete')
                        offset += sent
                    record['requestSentMonotonicNs'] = time.monotonic_ns()
                    wire = WireFile(sock, lambda: self.checkpoint(deadline, True), deadline)
                    response = http.client.HTTPResponse(ResponseSocket(wire), method='GET')
                    record['responseCreated'] = True
                    response.begin()
                    self.checkpoint(deadline, True)
                    record['headersMonotonicNs'] = time.monotonic_ns()
                    record.update(status=response.status, headers=response.getheaders(),
                                  contentType=response.getheader('Content-Type'),
                                  contentLength=response.getheader('Content-Length'),
                                  responseRequestId=response.getheader('X-Request-ID'))
                    need(wire.header_end is not None, 'raw_header_incomplete')
                    record.update(response_headers(response, bytes(wire.raw[:wire.header_end])))
                    while True:
                        self.checkpoint(deadline, True)
                        try:
                            part = response.read1(min(65536, BODY_CAP + 1 - body_size))
                        except http.client.IncompleteRead as error:
                            chunks.append(error.partial)
                            body_size += len(error.partial)
                            raise Rejected('response_body_incomplete') from None
                        if part:
                            chunks.append(part)
                            body_size += len(part)
                            need(body_size <= BODY_CAP, 'response_body_limit')
                        if not part or response.isclosed():
                            break
                    self.checkpoint(deadline, True)
                    need(response.length in (None, 0), 'response_body_incomplete')
                    record['bodyComplete'] = True
                    record['bodyCompleteMonotonicNs'] = time.monotonic_ns()
                    record['bodyCompleteAnchor'] = clock_anchor()
                except BaseException as error:
                    caught = error
                    raise
                finally:
                    if response is not None:
                        try:
                            response.close()
                            record['responseClosed'] = True
                        except BaseException as error:
                            record['responseCloseErrors'].append(error_code(error))
                            if caught is None:
                                caught = Rejected('response_close_failed')
                    if sock is not None:
                        try:
                            sock.close()
                        except BaseException as error:
                            record['connectionCloseErrors'].append(error_code(error))
                            if caught is None:
                                caught = Rejected('connection_close_failed')
                        record['connectionClosed'] = sock.fileno() == -1
                    else:
                        record['connectionClosed'] = True
                    record['connectionCloseMonotonicNs'] = time.monotonic_ns()
            if caught is not None:
                raise caught
            record['correctness'] = validate_catalog(b''.join(chunks), shape, self.value['phase'])
        except BaseException as error:
            if caught is None:
                caught = error
            elif error is not caught and isinstance(caught, Rejected) and str(caught) in (
                    'cancel_before_dispatch', 'phase_window_closed_before_dispatch'):
                # A benign control event cannot hide a later context-exit failure.
                caught = error
            record['error'] = error_code(caught)
            clean_close = not record['responseCloseErrors'] and not record['connectionCloseErrors']
            if isinstance(caught, Rejected) and str(caught) == 'cancel_before_dispatch' and clean_close:
                record['cancelledBeforeDispatch'] = True
                record['dispatchMonotonicNs'] = None
                record['error'] = None
                record['correctness'] = {'passed': False, 'notApplicable': 'not_dispatched'}
                caught = None
            if isinstance(caught, Rejected) and str(caught) == 'phase_window_closed_before_dispatch' and clean_close:
                record['windowClosedBeforeDispatch'] = True
                record['dispatchMonotonicNs'] = None
                record['error'] = None
                record['correctness'] = {'passed': False, 'notApplicable': 'not_dispatched'}
                caught = None
        finally:
            if sock is None:
                record['connectionClosed'] = True
                record['connectionCloseMonotonicNs'] = time.monotonic_ns()
            if sock is not None and sock.fileno() != -1:
                try:
                    sock.close()
                    record['connectionClosed'] = sock.fileno() == -1
                except BaseException as error:
                    record['connectionCloseErrors'].append(error_code(error))
                    record['connectionClosed'] = sock.fileno() == -1
                    if caught is None:
                        caught = Rejected('connection_close_failed')
                        record['error'] = error_code(caught)
                record['connectionCloseMonotonicNs'] = time.monotonic_ns()
            self.report['allOwnedConnectionsClosed'] &= record['connectionClosed']
            raw = bytes(wire.raw) if wire is not None else b''
            header_end = wire.header_end if wire is not None else None
            raw_header = raw[:header_end] if header_end is not None else raw
            raw_body = raw[header_end:] if header_end is not None else b''
            record['rawHeaderComplete'] = header_end is not None
            record['rawHeader'] = self.save('%03d-response-header.raw' % sequence, raw_header)
            record['rawWireBody'] = self.save('%03d-response-wire-body.raw' % sequence, raw_body)
            record['body'] = self.save('%03d-response-body.raw' % sequence, b''.join(chunks))
            record['bodyBytes'] = body_size
            record['evidenceWriteNsBeforeRecord'] = (self.evidence_write_ns - write_before +
                                                    record['intentEvidenceWriteNs'])
            record['finishedAnchor'] = clock_anchor()
            record['receipt'] = self.save('%03d-response.json' % sequence, encoded(record))
        if caught is not None:
            raise caught
        need(record['connectionClosed'] and
             (not record['responseCreated'] or record['responseClosed']), 'owned_connection_not_closed')
        return record

    def run(self):
        need(os.getpgrp() == os.getpid() == os.getsid(0), 'reader_requires_owned_new_session')
        check_bound_processes(self.context)
        if not self.wait_start():
            return
        next_dispatch = self.start['anchor']['monotonicAfterNs']
        self.report['status'] = 'observing'
        for sequence in range(1, REQUEST_CAP + 1):
            while time.monotonic_ns() < next_dispatch:
                self.checkpoint()
                if self.cancel is not None:
                    break
                time.sleep(min(0.05, max(0, (next_dispatch - time.monotonic_ns()) / 1_000_000_000)))
            self.checkpoint()
            if self.cancel is not None:
                self.report['status'] = 'stopped_by_controller'
                return
            if time.monotonic_ns() >= self.phase_deadline:
                self.report['status'] = 'window_complete'
                return
            record = self.exchange(sequence)
            if not record['dispatched'] and record.get('cancelledBeforeDispatch') is True:
                self.report['status'] = 'stopped_by_controller'
                return
            if not record['dispatched'] and record.get('windowClosedBeforeDispatch') is True:
                self.report['status'] = 'window_complete'
                return
            next_dispatch = record['dispatchMonotonicNs'] + SPACING_NS
        self.report['status'] = 'request_limit_reached'

    def finish(self, error=None):
        namespace = os.readlink('/proc/self/ns/net')
        if SIGNAL_NUMBER is not None and error is None:
            error = Rejected('reader_signal_received')
        if namespace != self.context['hostNetworkNamespace'] and error is None:
            error = Rejected('final_namespace_not_restored')
        if error is not None:
            self.report['error'] = error_code(error)
            self.report['status'] = 'failed'
        self.report.update(signalNumber=SIGNAL_NUMBER, finishedAnchor=clock_anchor(),
                           evidenceBytesBeforeReceipt=self.bytes_written,
                           evidenceWriteNsBeforeReceipt=self.evidence_write_ns,
                           namespaceAtFinish=namespace,
                           hostNamespaceRestored=namespace == self.context['hostNetworkNamespace'],
                           workerFunctionReturned=True,
                           workerRequestedExitCode=1 if self.report['status'] == 'failed' else 0)
        pin = self.save('receipt.json', encoded(self.report))
        code = self.report['workerRequestedExitCode']
        if SIGNAL_NUMBER is not None:
            code = 1
        return pin, code


def main():
    sys.tracebacklimit = 0
    parser = argparse.ArgumentParser(description='Run one fixed private capacity GET reader.')
    parser.add_argument('--input', required=True)
    parser.add_argument('--input-sha256', required=True)
    args = parser.parse_args()
    reader = None
    error = None
    try:
        need(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0 and
             sys.flags.isolated == 1 and sys.flags.dont_write_bytecode == 1,
             'linux_root_isolated_no_bytecode_required')
        os.umask(0o077)
        sha(args.input_sha256)
        path = Path(args.input)
        need(path in {E / 'private/readers' / phase / (side + '-input.json')
                      for phase in ('cold', 'cached') for side in ('left', 'right')},
             'input_path_not_fixed')
        raw, input_pin = read_private(path, 16 << 10)
        need(input_pin['sha256'] == args.input_sha256, 'input_hash_changed')
        value = validate_input(parse(raw))
        need(path == E / 'private/readers' / value['phase'] / (value['reader'] + '-input.json'),
             'input_path_binding')
        need(Path(__file__) == SOURCE, 'reader_source_path')
        pinned(value['self'], SOURCE, 256 << 10)
        context = validate_context(parse(pinned(value['context'], CONTEXT, 32 << 10)))
        resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
        resource.setrlimit(resource.RLIMIT_AS, (MEMORY_CAP, MEMORY_CAP))
        resource.setrlimit(resource.RLIMIT_FSIZE, (2 << 20, 2 << 20))
        signal.signal(signal.SIGTERM, signal_handler)
        signal.signal(signal.SIGINT, signal_handler)
        check_bound_processes(context)
        need(os.readlink('/proc/self/ns/net') == context['hostNetworkNamespace'],
             'reader_not_in_host_namespace')
        reader = Reader(value, context, input_pin)
        reader.run()
    except BaseException as caught:
        error = caught
    if reader is None:
        print(json.dumps({'kind': 'native-scan-http-capacity-reader-rejected',
                          'error': error_code(error), 'fixtureActions': False}, sort_keys=True))
        return 1
    try:
        pin, code = reader.finish(error)
        print(json.dumps({'kind': 'native-scan-http-capacity-reader-result',
                          'status': reader.report['status'], 'receipt': pin,
                          'dispatchedRequests': reader.report['dispatchedRequests'],
                          'signalNumberAfterReceipt': SIGNAL_NUMBER,
                          'workerRequestedExitCode': code}, sort_keys=True))
        return 1 if SIGNAL_NUMBER is not None else code
    except BaseException as caught:
        print(json.dumps({'kind': 'native-scan-http-capacity-reader-receipt-failed',
                          'error': error_code(caught), 'output': str(reader.output),
                          'allOwnedConnectionsClosed': reader.report['allOwnedConnectionsClosed']}, sort_keys=True))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
