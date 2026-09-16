"""Run bounded, explicitly synthetic reader protocol checks on test-env only.

Example remote invocation (never execute this on the local workstation):
  /usr/bin/python3 -I -B capacity-reader-checks.py \
    --source /opt/goby-test/native-scan-http-capacity-running-checks-20260916/private/source/capacity-reader.py \
    --source-sha256 609d6ea7d3bcffcddce724007f7c12ad68940dcdbe818e15117a2bcc6d7792bb \
    --output /opt/goby-test/native-scan-http-capacity-running-checks-20260916/private/reader-checks

Only definitions are imported, under a name different from __main__. These
checks call Reader.exchange, event, checkpoint, wait_start, finish, and the
catalog parser. The module main and Reader.run are never invoked. Admission of
input files, application/controller identities, namespaces, and parent-owned
process termination is outside this suite.

Each transport is one real AF_UNIX socketpair. A module-local adapter replaces
the reader's AF_INET socket constructor and connect_ex; no TCP listener or
connection is created. app_network is an explicit no-operation substitute.
The context and catalog replies are synthetic protocol fixtures, not product
responses. They provide no evidence for native scanning, compatibility, ACLs,
terminal SQL reconciliation, throughput, or main-service acceptance.

The real reader writes evidence into fresh private directories. The tests
inspect request receipts before harness cleanup, so a harness close cannot
stand in for the reader's own connection close. Peer threads have independent
finite budgets, stop events, and mandatory joins. No child process, service,
SQL command, existing environment file, key, or business namespace is used.
"""

import argparse
from contextlib import contextmanager
import errno
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import select
import signal
import socket
import stat
import sys
import threading
import time
from types import SimpleNamespace


ROOT = Path('/opt/goby-test/native-scan-http-capacity-running-checks-20260916')
SOURCE = ROOT / 'private/source/capacity-reader.py'
SOURCE_SHA256 = '609d6ea7d3bcffcddce724007f7c12ad68940dcdbe818e15117a2bcc6d7792bb'
SOURCE_BYTES = 49598
OUTPUT = ROOT / 'private/reader-checks'
REQUEST_ID = 'a' * 32
INPUT_SHA = '1' * 64
OTHER_INPUT_SHA = '2' * 64
SYNTHETIC_TOKEN = 'A' * 43
AUDIT_DENIALS = []


def require(condition, code):
    if not condition:
        raise AssertionError(code)


def safe_error(error, module=None):
    if module is not None and isinstance(error, module.Rejected):
        return str(error)
    if isinstance(error, AssertionError):
        return str(error)
    return type(error).__name__


def encoded(value):
    return (json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + '\n').encode()


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def private_directory(path):
    require(path.is_absolute() and path.resolve() == path and path.is_relative_to(ROOT),
            'test_directory_scope')
    for current in (path, *path.parents):
        info = current.lstat()
        require(stat.S_ISDIR(info.st_mode) and not stat.S_ISLNK(info.st_mode),
                'test_directory_symlink')
        if current == ROOT or current.is_relative_to(ROOT):
            require(info.st_uid == info.st_gid == 0 and stat.S_IMODE(info.st_mode) == 0o700,
                    'test_directory_permissions')


def private_read(path, maximum):
    private_directory(path.parent)
    fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW)
    try:
        info = os.fstat(fd)
        require(stat.S_ISREG(info.st_mode) and info.st_uid == info.st_gid == 0 and
                stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1 and
                0 <= info.st_size <= maximum, 'test_file_permissions_or_size')
        raw = b''
        while len(raw) <= maximum:
            part = os.read(fd, min(65536, maximum + 1 - len(raw)))
            if not part:
                break
            raw += part
        after = os.fstat(fd)
        current = path.lstat()
        identity = lambda row: (row.st_dev, row.st_ino, row.st_size, row.st_mtime_ns,
                                row.st_ctime_ns, row.st_uid, row.st_gid, row.st_mode,
                                row.st_nlink)
        require(len(raw) == info.st_size and identity(info) == identity(after) == identity(current),
                'test_file_changed')
    finally:
        os.close(fd)
    return raw, {'path': str(path), 'sha256': digest(raw), 'bytes': len(raw)}


def write_new(path, raw):
    private_directory(path.parent)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_CLOEXEC | os.O_NOFOLLOW, 0o600)
    try:
        offset = 0
        while offset < len(raw):
            count = os.write(fd, raw[offset:])
            require(count > 0, 'test_write_incomplete')
            offset += count
        os.fsync(fd)
    finally:
        os.close(fd)
    observed, pin = private_read(path, max(1, len(raw)))
    require(observed == raw, 'test_write_readback')
    return pin


def audit(event, _arguments):
    forbidden = {'socket.bind', 'socket.connect', 'socket.getaddrinfo',
                 'subprocess.Popen', 'os.system', 'os.exec', 'os.posix_spawn',
                 'os.fork', 'os.forkpty', 'os.kill', 'os.killpg'}
    if event in forbidden:
        AUDIT_DENIALS.append(event)
        raise RuntimeError('forbidden_external_or_child_action')


def hard_timeout(_number, _frame):
    raise TimeoutError('whole_suite_deadline')


@contextmanager
def synthetic_network(context):
    require(context.get('syntheticProtocolFixture') is True, 'synthetic_context_required')
    yield


class ConnectedPairAdapter:
    """Expose the already-connected Unix endpoint to the real reader exchange."""

    def __init__(self, endpoint):
        self.endpoint = endpoint
        self.connect_calls = 0

    def connect_ex(self, address):
        require(address == ('127.0.0.1', 18099), 'unexpected_reader_destination')
        self.connect_calls += 1
        require(self.connect_calls == 1, 'unexpected_reader_reconnect')
        return 0

    def setblocking(self, value):
        self.endpoint.setblocking(value)

    def send(self, raw):
        return self.endpoint.send(raw)

    def recv(self, count):
        return self.endpoint.recv(count)

    def fileno(self):
        return self.endpoint.fileno()

    def close(self):
        self.endpoint.close()


class PairTransport:
    """One finite peer, with no TCP, subprocess, or inherited actor handles."""

    def __init__(self, plan, target):
        client, self.peer = socket.socketpair(socket.AF_UNIX, socket.SOCK_STREAM)
        require(client.family == socket.AF_UNIX and self.peer.family == socket.AF_UNIX,
                'socketpair_family')
        self.client = ConnectedPairAdapter(client)
        self.peer.setblocking(False)
        self.plan = plan
        self.target = target
        self.created = 0
        self.stop = threading.Event()
        self.events = []
        self.error = None
        self.request_received = False
        self.sent_bytes = 0
        self.deadline = time.monotonic() + 4.0
        self.thread = threading.Thread(target=self.serve, name='synthetic-capacity-peer', daemon=False)
        self.thread.start()

    def remaining(self):
        require(not self.stop.is_set(), 'peer_stopped')
        remaining = self.deadline - time.monotonic()
        require(remaining > 0, 'peer_deadline')
        return min(0.02, remaining)

    def receive_request(self):
        raw = bytearray()
        while not raw.endswith(b'\r\n\r\n'):
            timeout = self.remaining()
            ready, _, _ = select.select([self.peer], [], [], timeout)
            if not ready:
                continue
            try:
                part = self.peer.recv(4096)
            except BlockingIOError:
                continue
            require(bool(part), 'peer_request_eof')
            raw.extend(part)
            require(len(raw) <= 8192, 'peer_request_limit')
        require(bytes(raw).startswith(('GET ' + self.target + ' HTTP/1.1\r\n').encode()),
                'peer_request_target')
        require(bytes(raw).count(b'X-Emby-Token: ' + SYNTHETIC_TOKEN.encode() + b'\r\n') == 1,
                'peer_synthetic_credential_header')
        self.request_received = True
        self.events.append({'kind': 'request_received', 'monotonicNs': time.monotonic_ns()})

    def send(self, raw):
        offset = 0
        while offset < len(raw):
            timeout = self.remaining()
            _, ready, _ = select.select([], [self.peer], [], timeout)
            if not ready:
                continue
            try:
                count = self.peer.send(raw[offset:offset + 65536])
            except BlockingIOError:
                continue
            require(count > 0, 'peer_send_incomplete')
            offset += count
            self.sent_bytes += count
        self.events.append({'kind': 'sent', 'bytes': len(raw), 'monotonicNs': time.monotonic_ns()})

    def pause(self, seconds):
        end = time.monotonic() + seconds
        while time.monotonic() < end:
            self.stop.wait(min(self.remaining(), max(0, end - time.monotonic())))

    def serve(self):
        try:
            self.receive_request()
            for action, value in self.plan:
                if action == 'send':
                    self.send(value)
                elif action == 'pause':
                    self.pause(value)
                elif action == 'control':
                    value()
                    self.events.append({'kind': 'control_published', 'monotonicNs': time.monotonic_ns()})
                else:
                    raise AssertionError('unknown_peer_action')
        except (BrokenPipeError, ConnectionResetError):
            self.events.append({'kind': 'receiver_closed', 'monotonicNs': time.monotonic_ns()})
        except OSError as error:
            if self.stop.is_set() and error.errno in (errno.EBADF, errno.ENOTSOCK):
                self.events.append({'kind': 'harness_stop', 'monotonicNs': time.monotonic_ns()})
            else:
                self.error = type(error).__name__
        except BaseException as error:
            if isinstance(error, AssertionError) and str(error) == 'peer_stopped' and self.stop.is_set():
                self.events.append({'kind': 'harness_stop', 'monotonicNs': time.monotonic_ns()})
            else:
                self.error = safe_error(error)
        finally:
            self.peer.close()

    def socket(self, family, kind):
        require(family == socket.AF_INET and kind == socket.SOCK_STREAM,
                'reader_socket_signature')
        self.created += 1
        require(self.created == 1, 'new_connection_after_first_exchange')
        return self.client

    def module(self):
        return SimpleNamespace(socket=self.socket, AF_INET=socket.AF_INET,
                               SOCK_STREAM=socket.SOCK_STREAM, SOL_SOCKET=socket.SOL_SOCKET,
                               SO_ERROR=socket.SO_ERROR)

    def close_and_join(self):
        reader_closed_before_cleanup = self.client.fileno() == -1
        self.stop.set()
        self.client.close()
        self.thread.join(timeout=4.5)
        require(not self.thread.is_alive(), 'peer_thread_not_joined')
        require(self.peer.fileno() == -1, 'peer_endpoint_not_closed')
        require(self.error is None, 'peer_failure_' + str(self.error))
        return {'readerEndpointClosedBeforeHarnessCleanup': reader_closed_before_cleanup,
                'peerEndpointClosed': True, 'peerThreadJoined': True,
                'socketConstructorCalls': self.created, 'virtualConnectCalls': self.client.connect_calls,
                'requestReceived': self.request_received, 'sentBytes': self.sent_bytes,
                'events': self.events}


def no_socket(_family, _kind):
    raise AssertionError('socket_created_without_transport')


class Fixture:
    def __init__(self, module, output, name, source_pin):
        self.module = module
        self.root = output / name
        self.root.mkdir(mode=0o700)
        private_directory(self.root)
        private = self.root / 'private'
        private.mkdir(mode=0o700)
        readers = private / 'readers'
        readers.mkdir(mode=0o700)
        self.phase = readers / 'cold'
        self.phase.mkdir(mode=0o700)
        self.context = {'syntheticProtocolFixture': True,
                        'hostNetworkNamespace': os.readlink('/proc/self/ns/net'),
                        'processAndNamespaceAdmission': 'not_executed'}
        context_pin = write_new(private / 'synthetic-context.json', encoded(self.context))
        self.value = {'kind': 'native-scan-http-capacity-reader-input', 'version': 1,
                      'scope': str(self.root), 'self': source_pin, 'context': context_pin,
                      'phase': 'cold', 'reader': 'left', 'taskId': '3' * 32,
                      'requestId': '4' * 32, 'runId': '5' * 32, 'userId': '6' * 32,
                      'libraryId': '7' * 32, 'token': SYNTHETIC_TOKEN}
        self.input_pin = {'path': str(self.phase / 'left-input.json'), 'sha256': INPUT_SHA, 'bytes': 1}
        self.previous = {name: getattr(module, name)
                         for name in ('E', 'socket', 'http', 'app_network', 'SIGNAL_NUMBER')}
        module.E = self.root
        module.socket = SimpleNamespace(socket=no_socket, AF_INET=socket.AF_INET,
                                        SOCK_STREAM=socket.SOCK_STREAM)
        module.app_network = synthetic_network
        module.SIGNAL_NUMBER = None
        self.reader = module.Reader(self.value, self.context, self.input_pin)
        self.reader.phase_deadline = time.monotonic_ns() + 2_000_000_000
        self.transport = None

    def cancel(self, reason='task_terminal', change=None):
        value = {'kind': 'native-scan-http-capacity-reader-cancel', 'version': 1,
                 'scope': str(self.root), 'phase': 'cold', 'runId': self.value['runId'],
                 'inputs': {'left': INPUT_SHA, 'right': OTHER_INPUT_SHA},
                 'anchor': self.module.clock_anchor(), 'reason': reason}
        if change is not None:
            change(value)
        pending = self.phase / 'cancel-pending.json'
        final = self.phase / 'cancel.json'
        write_new(pending, encoded(value))
        require(not final.exists(), 'test_control_already_exists')
        os.rename(pending, final)
        fd = os.open(self.phase, os.O_RDONLY | os.O_CLOEXEC | os.O_DIRECTORY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
        return value

    def attach(self, plan):
        require(self.transport is None, 'test_transport_reused')
        _shape, target = self.module.query_shape(self.value['userId'], 1)
        self.transport = PairTransport(plan, target)
        self.module.socket = self.transport.module()

    def exchange(self, expected_error=None):
        caught = None
        started = time.monotonic_ns()
        try:
            self.reader.exchange(1)
        except BaseException as error:
            caught = error
        ended = time.monotonic_ns()
        require(len(self.reader.report['requests']) == 1, 'missing_exchange_record')
        record = self.reader.report['requests'][0]
        if expected_error is None:
            require(caught is None, 'unexpected_exchange_error_' + safe_error(caught, self.module))
            require(record['bodyComplete'] and record['correctness']['passed'], 'successful_body_not_validated')
        else:
            require(isinstance(caught, self.module.Rejected) and str(caught) == expected_error,
                    'wrong_exchange_failure_' + safe_error(caught, self.module))
            require(record['error'] == expected_error and not record['correctness']['passed'],
                    'failure_marked_successful')
        require(record['dispatched'] and record['connectionCreated'] and record['connectionClosed'] and
                self.transport.client.fileno() == -1 and self.reader.report['allOwnedConnectionsClosed'],
                'reader_connection_closure')
        require(not record['responseCreated'] or record['responseClosed'], 'reader_response_closure')
        require(record['connectionCloseMonotonicNs'] >= record['dispatchMonotonicNs'],
                'connection_close_order')
        raw, receipt_pin = private_read(Path(record['receipt']['path']), 2 << 20)
        saved = self.module.parse(raw)
        require(receipt_pin == record['receipt'] and saved['connectionClosed'] is True and
                saved['dispatched'] is True and saved['error'] == record['error'],
                'saved_exchange_receipt')
        require(SYNTHETIC_TOKEN.encode() not in raw, 'synthetic_credential_in_receipt')
        for field in ('rawHeader', 'rawWireBody', 'body'):
            content, observed = private_read(Path(record[field]['path']), 2 << 20)
            require(observed == record[field], 'saved_response_pin')
            require(SYNTHETIC_TOKEN.encode() not in content, 'synthetic_credential_in_response_evidence')
        return record, (ended - started) / 1_000_000_000

    def close(self):
        try:
            return self.transport.close_and_join() if self.transport is not None else {
                'socketConstructorCalls': 0, 'peerThreadJoined': True, 'peerEndpointClosed': True}
        finally:
            for name, value in self.previous.items():
                setattr(self.module, name, value)


def body():
    return encoded({'Items': [{'Id': 'b' * 32, 'Type': 'Movie', 'IsFolder': False}],
                    'TotalRecordCount': 1})


def header(length=None, chunked=False):
    raw = (b'HTTP/1.1 200 OK\r\nContent-Type: application/json; charset=utf-8\r\n'
           b'X-Request-ID: ' + REQUEST_ID.encode() + b'\r\nConnection: close\r\n')
    if length is not None:
        raw += ('Content-Length: %d\r\n' % length).encode()
    if chunked:
        raw += b'Transfer-Encoding: chunked\r\n'
    return raw + b'\r\n'


def chunk(raw):
    return ('%x\r\n' % len(raw)).encode() + raw + b'\r\n'


def read_record_file(record, name):
    raw, observed = private_read(Path(record[name]['path']), 2 << 20)
    require(observed == record[name], 'record_file_pin')
    return raw


def complete_length(fixture):
    payload = body()
    prefix = header(len(payload))
    fixture.attach([('send', prefix + payload[:7]), ('pause', 0.03), ('send', payload[7:])])
    record, elapsed = fixture.exchange()
    require(read_record_file(record, 'rawHeader') == prefix and
            read_record_file(record, 'rawWireBody') == payload and
            read_record_file(record, 'body') == payload, 'content_length_preservation')
    require(record['bodyBytes'] == len(payload) and record['responseRequestId'] == REQUEST_ID and
            record['correctness']['observedRows'] == [{'id': 'b' * 32, 'kind': 'Movie'}] and
            record['correctness']['terminalMapReconciliation'] == 'required',
            'content_length_local_contract')
    return {'checks': ['complete fixed-length body', 'separate raw header and decoded body',
                       'local IDs retained with external reconciliation still required'], 'seconds': elapsed}


def complete_chunked(fixture):
    payload = body()
    prefix = header(chunked=True)
    wire = chunk(payload[:19]) + chunk(payload[19:]) + b'0\r\n\r\n'
    fixture.attach([('send', prefix + wire[:13]), ('pause', 0.03), ('send', wire[13:])])
    record, elapsed = fixture.exchange()
    require(read_record_file(record, 'rawHeader') == prefix and
            read_record_file(record, 'rawWireBody') == wire and
            read_record_file(record, 'body') == payload and wire != payload,
            'chunked_wire_and_decoded_preservation')
    return {'checks': ['complete chunked body', 'transfer framing retained separately from decoded JSON'],
            'seconds': elapsed}


def truncated_header(fixture):
    prefix = b'HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nX-Request-ID: ' + REQUEST_ID.encode()
    fixture.attach([('send', prefix)])
    record, elapsed = fixture.exchange('raw_header_incomplete')
    require(not record['rawHeaderComplete'] and not record['bodyComplete'] and
            read_record_file(record, 'rawHeader') == prefix and
            read_record_file(record, 'body') == b'', 'truncated_header_partial_evidence')
    return {'checks': ['EOF before header terminator rejected', 'partial raw header retained'], 'seconds': elapsed}


def truncated_body(fixture):
    payload = body()
    partial = payload[:-11]
    fixture.attach([('send', header(len(payload)) + partial)])
    record, elapsed = fixture.exchange('response_body_incomplete')
    require(record['rawHeaderComplete'] and not record['bodyComplete'] and
            read_record_file(record, 'body') == partial and
            read_record_file(record, 'rawWireBody') == partial, 'truncated_body_partial_evidence')
    return {'checks': ['short fixed-length body rejected', 'partial decoded body remains partial'],
            'seconds': elapsed}


def oversized_chunked_body(fixture):
    payload = b'x' * (fixture.module.BODY_CAP + 1)
    fixture.attach([('send', header(chunked=True) + chunk(payload) + b'0\r\n\r\n')])
    record, elapsed = fixture.exchange('response_body_limit')
    require(not record['bodyComplete'] and record['bodyBytes'] == fixture.module.BODY_CAP + 1 and
            len(read_record_file(record, 'body')) == fixture.module.BODY_CAP + 1,
            'decoded_body_cap_not_enforced')
    return {'checks': ['decoded body cap enforced without Content-Length',
                       'oversized partial evidence is not accepted JSON'], 'seconds': elapsed}


def absolute_slow_drip(fixture):
    prefix = b'HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nX-Pad: '
    plan = [('send', prefix), ('pause', 0.025),
            ('control', lambda: fixture.cancel('task_terminal'))]
    for _ in range(40):
        plan.extend([('pause', 0.025), ('send', b'x')])
    fixture.attach(plan)
    fixture.reader.phase_deadline = time.monotonic_ns() + 300_000_000
    frozen_deadline = fixture.reader.phase_deadline
    record, elapsed = fixture.exchange('absolute_http_deadline')
    received = read_record_file(record, 'rawHeader')
    require(record['deadlineMonotonicNs'] == frozen_deadline and elapsed < 1.0 and
            len(received) >= len(prefix) + 3 and not record['rawHeaderComplete'] and
            not record['bodyComplete'] and fixture.reader.cancel['reason'] == 'task_terminal',
            'slow_drip_extended_absolute_deadline')
    return {'checks': ['multiple timely recv calls cannot refresh the absolute deadline',
                       'normal cancellation cannot extend the in-flight absolute deadline',
                       'incomplete header and closed connection retained'],
            'seconds': elapsed, 'receivedDripBytes': len(received) - len(prefix),
            'deadlineMonotonicNs': frozen_deadline}


def normal_cancel(fixture):
    payload = body()
    split = len(payload) // 2
    fixture.attach([('send', header(len(payload)) + payload[:split]), ('pause', 0.04),
                    ('control', lambda: fixture.cancel('task_terminal')), ('pause', 0.10),
                    ('send', payload[split:])])
    frozen_deadline = fixture.reader.phase_deadline
    record, elapsed = fixture.exchange()
    require(fixture.reader.cancel['reason'] == 'task_terminal' and
            record['deadlineMonotonicNs'] == frozen_deadline and
            record['bodyCompleteMonotonicNs'] >= fixture.reader.cancel['anchor']['monotonicAfterNs'],
            'normal_cancel_changed_inflight_contract')
    second = fixture.reader.exchange(2)
    require(second.get('cancelledBeforeDispatch') is True and not second['dispatched'] and
            second['dispatchMonotonicNs'] is None and not second['connectionCreated'] and
            second['connectionClosed'] and second['error'] is None and
            fixture.transport.created == 1 and fixture.reader.report['dispatchedRequests'] == 1,
            'normal_cancel_allowed_new_dispatch')
    return {'checks': ['normal cancel allows in-flight completion under the original deadline',
                       'second exchange cannot create or dispatch a connection'], 'seconds': elapsed}


def failure_cancel(fixture):
    payload = body()
    split = len(payload) // 2
    fixture.attach([('send', header(len(payload)) + payload[:split]), ('pause', 0.04),
                    ('control', lambda: fixture.cancel('controller_failure')), ('pause', 0.8),
                    ('send', payload[split:])])
    frozen_deadline = fixture.reader.phase_deadline
    record, elapsed = fixture.exchange('controller_failure_cancel')
    require(record['deadlineMonotonicNs'] == frozen_deadline and elapsed < 0.7 and
            not record['bodyComplete'] and record['bodyBytes'] < len(payload) and
            record['connectionCloseMonotonicNs'] < frozen_deadline,
            'failure_cancel_waited_for_exchange_deadline')
    return {'checks': ['controller failure aborts a pending response before its HTTP deadline',
                       'partial response remains failed and owned connection closes'], 'seconds': elapsed}


def response_close_preserves_failure(fixture):
    """Inject only a close error after the real parser has rejected a short header."""
    actual_client = fixture.module.http.client

    class SyntheticResponseCloseError(Exception):
        pass

    class FailingCloseResponse(actual_client.HTTPResponse):
        def close(self):
            super().close()
            raise SyntheticResponseCloseError('synthetic_response_close_failure')

    fixture.module.http = SimpleNamespace(client=SimpleNamespace(
        HTTPResponse=FailingCloseResponse, IncompleteRead=actual_client.IncompleteRead))
    prefix = b'HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nX-Request-ID: ' + REQUEST_ID.encode()
    fixture.attach([('send', prefix)])
    caught = None
    try:
        fixture.reader.exchange(1)
    except BaseException as error:
        caught = error
    require(isinstance(caught, fixture.module.Rejected) and str(caught) == 'raw_header_incomplete',
            'response_close_replaced_original_failure_' + safe_error(caught, fixture.module))
    require(len(fixture.reader.report['requests']) == 1, 'close_failure_exchange_record_missing')
    record = fixture.reader.report['requests'][0]
    require(record['error'] == 'raw_header_incomplete' and
            record['responseCloseErrors'] == ['SyntheticResponseCloseError'] and
            record['connectionCloseErrors'] == [] and record['responseCreated'] and
            record['responseClosed'] is False and record['connectionCreated'] and
            record['connectionClosed'] and fixture.transport.client.fileno() == -1 and
            record['dispatched'] and not record['bodyComplete'] and
            not record['correctness']['passed'] and
            fixture.reader.report['allOwnedConnectionsClosed'], 'close_failure_evidence_contract')
    require(read_record_file(record, 'rawHeader') == prefix and
            read_record_file(record, 'body') == b'', 'close_failure_partial_header_not_retained')
    raw, saved_pin = private_read(Path(record['receipt']['path']), 2 << 20)
    saved = fixture.module.parse(raw)
    require(saved_pin == record['receipt'] and saved['error'] == 'raw_header_incomplete' and
            saved['responseCloseErrors'] == ['SyntheticResponseCloseError'] and
            saved['responseClosed'] is False and saved['connectionClosed'] is True,
            'close_failure_saved_exchange_receipt')
    receipt_pin, exit_code = fixture.reader.finish(caught)
    raw, observed = private_read(Path(receipt_pin['path']), 2 << 20)
    receipt = fixture.module.parse(raw)
    require(observed == receipt_pin and exit_code == 1 and receipt['status'] == 'failed' and
            receipt['error'] == 'raw_header_incomplete' and receipt['workerRequestedExitCode'] == 1 and
            receipt['actualProcessExitRequiresParentWait'] is True and
            receipt['requests'][0]['responseCloseErrors'] == ['SyntheticResponseCloseError'],
            'close_failure_worker_incorrectly_passed')
    return {'checks': ['original header rejection survives a response.close exception',
                       'close error remains separate and responseClosed is not invented',
                       'socket closes before harness cleanup and finish still requests exit code 1'],
            'injection': 'module-local HTTPResponse subclass calls real close, then raises a fixed synthetic error',
            'originalError': 'raw_header_incomplete',
            'responseCloseErrors': ['SyntheticResponseCloseError'], 'receipt': receipt_pin}


def context_exit_after_cancel(fixture):
    """A benign pre-dispatch cancel cannot hide a failing context exit."""
    class SyntheticContextExitError(Exception):
        pass

    @contextmanager
    def failing_network_context(context):
        require(context.get('syntheticProtocolFixture') is True, 'synthetic_context_required')
        try:
            yield
        finally:
            raise SyntheticContextExitError('synthetic_context_exit_failure')

    fixture.cancel('task_terminal')
    fixture.module.app_network = failing_network_context
    caught = None
    try:
        fixture.reader.exchange(1)
    except BaseException as error:
        caught = error
    require(isinstance(caught, SyntheticContextExitError),
            'benign_cancel_hid_context_exit_failure_' + safe_error(caught, fixture.module))
    require(len(fixture.reader.report['requests']) == 1, 'context_exit_exchange_record_missing')
    record = fixture.reader.report['requests'][0]
    require(record['error'] == 'SyntheticContextExitError' and
            record.get('cancelledBeforeDispatch') is not True and
            not record['dispatched'] and record['dispatchMonotonicNs'] is None and
            not record['connectionCreated'] and not record['responseCreated'] and
            record['connectionClosed'] and record['responseCloseErrors'] == [] and
            record['connectionCloseErrors'] == [] and
            fixture.reader.report['dispatchedRequests'] == 0 and
            fixture.reader.report['allOwnedConnectionsClosed'] and
            fixture.reader.cancel['reason'] == 'task_terminal',
            'context_exit_failure_converted_to_normal_cancel')
    raw, saved_pin = private_read(Path(record['receipt']['path']), 2 << 20)
    saved = fixture.module.parse(raw)
    require(saved_pin == record['receipt'] and saved['error'] == 'SyntheticContextExitError' and
            not saved['dispatched'] and not saved['connectionCreated'] and saved['connectionClosed'],
            'context_exit_saved_exchange_receipt')
    receipt_pin, exit_code = fixture.reader.finish(caught)
    raw, observed = private_read(Path(receipt_pin['path']), 2 << 20)
    receipt = fixture.module.parse(raw)
    require(observed == receipt_pin and exit_code == 1 and receipt['status'] == 'failed' and
            receipt['error'] == 'SyntheticContextExitError' and receipt['workerRequestedExitCode'] == 1 and
            receipt['dispatchedRequests'] == 0 and
            receipt['actualProcessExitRequiresParentWait'] is True,
            'context_exit_failure_worker_incorrectly_passed')
    return {'checks': ['normal pre-dispatch cancellation is observed through the real control parser',
                       'new context-exit failure remains failed instead of becoming a normal cancellation',
                       'no connection is created or dispatched and finish requests exit code 1'],
            'injection': 'module-local namespace context manager raises only on exit; exchange decisions remain real',
            'dispatchedRequests': 0, 'receipt': receipt_pin}


def expect_rejected(module, operation, expected):
    try:
        operation()
    except module.Rejected as error:
        require(str(error) == expected, 'unexpected_rejection_' + str(error))
        return expected
    raise AssertionError('invalid_value_accepted_' + expected)


def control_binding_group(module, output, source_pin):
    variations = [
        ('wrong-run', lambda value: value.update(runId='9' * 32), 'control_event_binding'),
        ('wrong-input', lambda value: value['inputs'].update(left='9' * 64), 'control_input_binding'),
        ('aliased-inputs', lambda value: value['inputs'].update(right=INPUT_SHA), 'control_input_binding')]
    results = []
    for name, change, expected in variations:
        fixture = Fixture(module, output, 'control-' + name, source_pin)
        try:
            fixture.cancel(change=change)
            actual = expect_rejected(module, lambda: fixture.reader.event('cancel.json', cancel=True), expected)
            require(fixture.reader.report['dispatchedRequests'] == 0 and not fixture.reader.report['requests'],
                    'invalid_control_dispatched_work')
            results.append({'name': name, 'error': actual, 'dispatchedRequests': 0})
        finally:
            fixture.close()
    fixture = Fixture(module, output, 'control-cancel-before-ready', source_pin)
    try:
        fixture.cancel('task_terminal')
        require(fixture.reader.wait_start() is False and
                fixture.reader.report['status'] == 'cancelled_before_start', 'prestart_cancel_not_honored')
        ready_path = fixture.reader.output / 'ready.json'
        raw, ready_pin = private_read(ready_path, 32768)
        ready = module.parse(raw)
        require(not (fixture.reader.output / 'ready-pending.json').exists() and
                ready_pin == fixture.reader.report['ready'] and ready['input'] == fixture.input_pin and
                ready['worker'] == fixture.reader.report['worker'], 'ready_publication_readback')
        receipt_pin, code = fixture.reader.finish()
        raw, observed = private_read(Path(receipt_pin['path']), 2 << 20)
        receipt = module.parse(raw)
        require(observed == receipt_pin and code == 0 and receipt['dispatchedRequests'] == 0 and
                receipt['requests'] == [] and receipt['status'] == 'cancelled_before_start' and
                receipt['hostNamespaceRestored'] is True and
                receipt['actualProcessExitRequiresParentWait'] is True,
                'cancelled_receipt_overclaims_exit')
        results.append({'name': 'cancel-before-ready', 'ready': ready_pin, 'receipt': receipt_pin,
                        'checks': ['real wait_start publishes ready through rename and directory fsync',
                                   'ready post-state and private final receipt read back',
                                   'worker statement still requires parent wait proof'],
                        'atomicFinalReceiptClaimed': False})
    finally:
        fixture.close()
    return {'variants': results, 'externalActions': 0}


def catalog_group(module):
    rows = [{'Id': ('%032x' % (index + 1)), 'Type': kind, 'IsFolder': False}
            for index, kind in enumerate(('Movie', 'Episode', 'Audio'))]
    accepted = module.validate_catalog(encoded({'Items': rows, 'TotalRecordCount': 3}), 'page', 'cold')
    require(accepted['observedRows'] == [{'id': row['Id'], 'kind': row['Type']} for row in rows] and
            accepted['terminalMapReconciliation'] == 'required', 'local_catalog_id_contract')
    duplicates = [dict(rows[0]), dict(rows[0]), dict(rows[2])]
    bad_kind = [dict(rows[0], Type='Series'), dict(rows[1]), dict(rows[2])]
    bad_id = [dict(rows[0], Id='G' * 32), dict(rows[1]), dict(rows[2])]
    not_boolean = [dict(rows[0], IsFolder=0), dict(rows[1]), dict(rows[2])]
    variants = []
    for name, changed, expected in (
            ('duplicate-id', duplicates, 'catalog_duplicate_id'),
            ('non-leaf-type', bad_kind, 'catalog_leaf_contract'),
            ('malformed-id', bad_id, 'identifier_contract'),
            ('numeric-folder-flag', not_boolean, 'catalog_leaf_contract')):
        raw = encoded({'Items': changed, 'TotalRecordCount': 3})
        error = expect_rejected(module, lambda: module.validate_catalog(raw, 'page', 'cold'), expected)
        variants.append({'name': name, 'error': error})
    for name, raw, expected in (
            ('boolean-count', b'{"Items":[],"TotalRecordCount":true}', 'catalog_count_type_or_bound'),
            ('duplicate-json-member', b'{"Items":[],"TotalRecordCount":0,"TotalRecordCount":0}',
             'duplicate_json_member')):
        error = expect_rejected(module, lambda: module.validate_catalog(raw, 'count', 'cold'), expected)
        variants.append({'name': name, 'error': error})
    cached = module.validate_catalog(b'{"Items":[],"TotalRecordCount":500}', 'count', 'cached')
    require(cached['cachedUserDataReconciliation'] == 'required' and
            cached['terminalMapReconciliation'] == 'required', 'cached_reconciliation_overclaimed')
    return {'checks': ['response-local Movie, Episode, and Audio identifiers retained',
                       'invalid row identity and scalar types fail closed',
                       'cached user data and terminal SQL reconciliation remain external'],
            'variants': variants}


def inspect_output_tree(output):
    files = 0
    directories = 0
    for path in (output, *output.rglob('*')):
        info = path.lstat()
        require(not stat.S_ISLNK(info.st_mode), 'output_symlink')
        if stat.S_ISDIR(info.st_mode):
            private_directory(path)
            directories += 1
        else:
            require(stat.S_ISREG(info.st_mode) and info.st_uid == info.st_gid == 0 and
                    stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1,
                    'output_private_file_contract')
            files += 1
    return {'directories': directories, 'filesBeforeResult': files,
            'allDirectoriesRoot0700': True, 'allFilesRoot0600SingleLink': True}


def main():
    parser = argparse.ArgumentParser(description='Run synthetic socketpair checks of the frozen capacity reader.')
    parser.add_argument('--source', required=True)
    parser.add_argument('--source-sha256', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    require(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0 and
            sys.flags.isolated == 1 and sys.flags.dont_write_bytecode == 1,
            'remote_linux_root_isolated_no_bytecode_required')
    require(Path(args.source) == SOURCE and args.source_sha256 == SOURCE_SHA256 and
            Path(args.output) == OUTPUT, 'fixed_test_cli_scope')
    require(Path(__file__) == ROOT / 'private/source/capacity-reader-checks.py',
            'test_script_path')
    os.umask(0o077)
    private_directory(SOURCE.parent)
    private_directory(OUTPUT.parent)
    source_raw, source_pin = private_read(SOURCE, 256 << 10)
    require(source_pin['sha256'] == SOURCE_SHA256 and source_pin['bytes'] == SOURCE_BYTES,
            'reader_source_pin_changed')
    _raw, script_pin = private_read(Path(__file__), 256 << 10)
    OUTPUT.mkdir(mode=0o700)
    private_directory(OUTPUT)
    sys.addaudithook(audit)
    previous_alarm = signal.signal(signal.SIGALRM, hard_timeout)
    signal.setitimer(signal.ITIMER_REAL, 45.0)
    started = time.monotonic_ns()
    module = None
    results = []
    error = None
    try:
        spec = importlib.util.spec_from_file_location('_capacity_reader_synthetic_checks', SOURCE)
        require(spec is not None and spec.loader is not None and spec.name != '__main__',
                'definition_import_name')
        module = importlib.util.module_from_spec(spec)
        exec(compile(source_raw, str(SOURCE), 'exec', dont_inherit=True), module.__dict__)
        require(module.__name__ != '__main__' and module.E != OUTPUT,
                'definition_import_scope')
        cases = [('complete-content-length', complete_length), ('complete-chunked', complete_chunked),
                 ('truncated-header', truncated_header), ('truncated-body', truncated_body),
                 ('chunked-body-cap', oversized_chunked_body), ('absolute-slow-drip', absolute_slow_drip),
                 ('normal-cancel', normal_cancel), ('controller-failure-cancel', failure_cancel),
                 ('response-close-preserves-failure', response_close_preserves_failure),
                 ('context-exit-after-normal-cancel', context_exit_after_cancel)]
        for name, operation in cases:
            fixture = None
            row = {'name': name, 'passed': False}
            try:
                fixture = Fixture(module, OUTPUT, name, source_pin)
                row['evidence'] = operation(fixture)
                row['passed'] = True
            except BaseException as caught:
                row['error'] = safe_error(caught, module)
            finally:
                if fixture is not None:
                    try:
                        row['transportClosure'] = fixture.close()
                        if fixture.transport is not None:
                            require(row['transportClosure']['readerEndpointClosedBeforeHarnessCleanup'],
                                    'harness_was_needed_to_close_reader_endpoint')
                            require(row['transportClosure']['requestReceived'], 'peer_did_not_receive_request')
                    except BaseException as caught:
                        row['passed'] = False
                        row['closureError'] = safe_error(caught, module)
            results.append(row)
        for name, operation in (
                ('control-binding-and-private-publication', lambda: control_binding_group(module, OUTPUT, source_pin)),
                ('response-local-identities-and-types', lambda: catalog_group(module))):
            row = {'name': name, 'passed': False}
            try:
                row['evidence'] = operation()
                row['passed'] = True
            except BaseException as caught:
                row['error'] = safe_error(caught, module)
            results.append(row)
        require(len(results) == 12 and all(row['passed'] for row in results), 'one_or_more_checks_failed')
        require(AUDIT_DENIALS == [], 'forbidden_action_attempted')
    except BaseException as caught:
        error = safe_error(caught, module)
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, previous_alarm)
    permissions = inspect_output_tree(OUTPUT)
    result = {'kind': 'native-scan-http-capacity-reader-synthetic-checks', 'version': 1,
              'scope': str(ROOT), 'source': source_pin, 'operator': script_pin,
              'definitionImportName': '_capacity_reader_synthetic_checks',
              'status': 'passed' if error is None else 'failed', 'error': error,
              'elapsedSeconds': (time.monotonic_ns() - started) / 1_000_000_000,
              'cases': results, 'forbiddenActionAttempts': AUDIT_DENIALS, 'permissions': permissions,
              'boundaries': {
                  'realCalls': ['Reader.exchange', 'Reader.event', 'Reader.checkpoint', 'Reader.wait_start',
                                'Reader.finish', 'WireFile', 'HTTPResponse', 'response_headers',
                                'validate_catalog', 'private reader evidence writes'],
                  'transport': 'AF_UNIX socketpair with module-local virtual AF_INET connect adapter',
                  'namespace': 'app_network replaced with a no-operation context manager',
                  'context': 'new synthetic context; only current test-process identity is read',
                  'inputs': 'synthetic direct method arguments; main input/source admission is not executed',
                  'responses': 'synthetic protocol fixtures, never product or upstream compatibility evidence',
                  'closeFailure': 'one module-local HTTPResponse subclass raises after real close; exchange remains real',
                  'contextExitFailure': 'one module-local namespace context raises after pre-dispatch cancel; no real namespace entry',
                  'deadline': 'original HTTP limit retained; shorter fresh phase deadline used for slow-drip case',
                  'lifecycle': 'no main, run, subprocess, service, signal to another process, or SQL call',
                  'closure': 'reader endpoint checked before harness cleanup; all peer threads joined',
                  'readyPublication': 'real wait_start rename/fsync path, checked through published post-state',
                  'finalReceipt': 'real private O_EXCL write/readback; no atomic publication claim',
                  'notProven': ['native scan workload', 'ACL policy', 'terminal SQL reconciliation',
                                'cached user data reconciliation', 'TCP behavior', 'private namespace entry',
                                'parent wait/exit proof', 'throughput capacity', 'main-service acceptance']}}
    pin = write_new(OUTPUT / 'result.json', encoded(result))
    print(json.dumps({'status': result['status'], 'result': pin,
                      'passedCases': sum(row['passed'] for row in results), 'totalCases': len(results)},
                     sort_keys=True))
    return 0 if error is None else 1


if __name__ == '__main__':
    sys.tracebacklimit = 0
    try:
        raise SystemExit(main())
    except Exception as error:
        print(json.dumps({'kind': 'native-scan-http-capacity-reader-checks-rejected',
                          'error': safe_error(error), 'workloadExecuted': False}, sort_keys=True))
        raise SystemExit(1)
