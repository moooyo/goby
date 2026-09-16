"""Bounded test-env-only synthetic checks of the frozen control transport.

Run only with /usr/bin/python3 -I -B. Each CLI source/path/hash is fixed below.
Import pinned source bytes under fresh non-main names; never invoke an actor,
workload execute, journey cleanup, SQL, a unit operation, or a source main.

The real ControlTransport constructor, install, bind, private journey request
method, counters, parser, callbacks, framing validator and receipt writer run.
Only external infrastructure/application/namespace checks and the connection
constructor are substitutes. The definitions-only reader loader is adapted to
the new check root; source hashes and private file checks still run. Source and
output constants are rebound to the stated new private check locations.

The real transport single-thread entry gate runs before a peer exists. A peer
thread is created lazily inside the substituted socket constructor, after that
gate. Its only connection is an AF_UNIX socketpair. connect_ex is virtual and
cannot reach TCP. Every peer has a finite deadline and mandatory join. The
transport endpoint is checked closed before harness cleanup can touch it.

All response bodies and credentials are declared synthetic protocol fixtures.
Original journey credential callbacks run to retain these in memory when a
reply later fails. This is no evidence for a Goby server, native workload,
credential issuance/revocation, namespace admission, or the previous twelve
reader checks. No existing environment or key file is read.
"""

import argparse
from contextlib import contextmanager
import errno
import hashlib
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
import types


ROOT = Path('/opt/goby-test/native-scan-http-capacity-running-checks-20260916')
CANDIDATE = ROOT / 'private/source'
OUTPUT = ROOT / 'private/transport-checks'
PINS = {
    'transport': {'path': str(CANDIDATE / 'capacity-transport.py'),
                  'sha256': 'a2a9599869ea47bdd2f40e3b520317e3e225348d050aea0b111896faa119696a', 'bytes': 53050},
    'reader': {'path': str(CANDIDATE / 'capacity-reader.py'),
               'sha256': '609d6ea7d3bcffcddce724007f7c12ad68940dcdbe818e15117a2bcc6d7792bb', 'bytes': 49598},
    'journey': {'path': str(CANDIDATE / 'native-catalog-journey.py'),
                'sha256': '63125715ad415349f3ae95a008f56783800c7334d90efcbcb394a14215e695d4', 'bytes': 39059}}
REQUEST_ID = 'c' * 32
USER_ID = 'd' * 32
LIBRARY_ID = 'e' * 32
SYNTHETIC_TOKEN = 'A' * 43
AUDIT_DENIALS = []


def require(condition, code):
    if not condition:
        raise AssertionError(code)


def encoded(value):
    return (json.dumps(value, sort_keys=True, separators=(',', ':'), allow_nan=False) + '\n').encode()


def code(error, fixture=None):
    if fixture is not None and isinstance(error, (fixture.module.Rejected, fixture.transport.reader.Rejected,
                                                 fixture.journey_module.JourneyError)):
        return str(error)
    return str(error) if isinstance(error, AssertionError) else type(error).__name__


def private_directory(path):
    require(path.is_absolute() and path.resolve() == path and path.is_relative_to(ROOT), 'check_directory_scope')
    for current in (path, *path.parents):
        info = current.lstat()
        require(stat.S_ISDIR(info.st_mode) and not stat.S_ISLNK(info.st_mode), 'check_directory_symlink')
        if current == ROOT or current.is_relative_to(ROOT):
            require(info.st_uid == info.st_gid == 0 and stat.S_IMODE(info.st_mode) == 0o700,
                    'check_directory_permissions')


def read_private(path, maximum, *, empty=False):
    private_directory(path.parent)
    fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW)
    try:
        before = os.fstat(fd)
        require(stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and
                stat.S_IMODE(before.st_mode) == 0o600 and before.st_nlink == 1 and
                (0 if empty else 1) <= before.st_size <= maximum, 'check_file_contract')
        parts, size = [], 0
        while size <= maximum:
            part = os.read(fd, min(65536, maximum + 1 - size))
            if not part:
                break
            parts.append(part)
            size += len(part)
        raw = b''.join(parts)
        identity = lambda row: (row.st_dev, row.st_ino, row.st_uid, row.st_gid, row.st_mode,
                                row.st_nlink, row.st_size, row.st_mtime_ns, row.st_ctime_ns)
        require(len(raw) == before.st_size and
                identity(before) == identity(os.fstat(fd)) == identity(path.lstat()), 'check_file_changed')
    finally:
        os.close(fd)
    return raw, {'path': str(path), 'sha256': hashlib.sha256(raw).hexdigest(), 'bytes': len(raw)}


def write_private(path, raw):
    private_directory(path.parent)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_CLOEXEC | os.O_NOFOLLOW, 0o600)
    try:
        offset = 0
        while offset < len(raw):
            count = os.write(fd, raw[offset:])
            require(count > 0, 'check_write_incomplete')
            offset += count
        os.fsync(fd)
    finally:
        os.close(fd)
    observed, pin = read_private(path, max(1, len(raw)), empty=True)
    require(observed == raw, 'check_write_readback')
    return pin


def definitions(name, kind, sources):
    require(name != '__main__' and kind in PINS, 'definition_import_contract')
    module = types.ModuleType(name)
    module.__file__ = PINS[kind]['path']
    exec(compile(sources[kind], module.__file__, 'exec', dont_inherit=True), module.__dict__)
    return module


def audit(event, _arguments):
    forbidden = {'socket.bind', 'socket.connect', 'socket.getaddrinfo', 'subprocess.Popen',
                 'os.system', 'os.exec', 'os.posix_spawn', 'os.fork', 'os.forkpty', 'os.kill', 'os.killpg'}
    if event in forbidden:
        AUDIT_DENIALS.append(event)
        raise RuntimeError('forbidden_external_or_child_action')


def timeout(_number, _frame):
    raise TimeoutError('whole_transport_suite_deadline')


class SyntheticConstructorError(Exception):
    pass


class SyntheticSendError(Exception):
    pass


class SyntheticResponseCloseError(Exception):
    pass


class SyntheticSocketCloseError(Exception):
    pass


class SyntheticNamespaceExitError(Exception):
    pass


class Endpoint:
    def __init__(self, actual, *, partial_send=False, close_error=False):
        self.actual = actual
        self.partial_send = partial_send
        self.close_error = close_error
        self.sends = self.connects = 0
        self.positive_partial_sent = False

    def connect_ex(self, destination):
        require(destination == ('127.0.0.1', 18099), 'unexpected_virtual_destination')
        self.connects += 1
        require(self.connects == 1, 'unexpected_connect_retry')
        return 0

    def setblocking(self, value):
        self.actual.setblocking(value)

    def fileno(self):
        return self.actual.fileno()

    def send(self, raw):
        self.sends += 1
        if self.partial_send:
            if self.positive_partial_sent:
                raise SyntheticSendError('synthetic_failure_after_positive_send')
            count = self.actual.send(raw[:7])
            self.positive_partial_sent = count > 0
            return count
        return self.actual.send(raw)

    def recv(self, maximum):
        return self.actual.recv(maximum)

    def close(self):
        self.actual.close()
        if self.close_error:
            raise SyntheticSocketCloseError('synthetic_socket_close_failure')


class Peer:
    def __init__(self, method, path, plan, *, partial_send=False, close_error=False):
        actual, self.peer = socket.socketpair(socket.AF_UNIX, socket.SOCK_STREAM)
        require(actual.family == self.peer.family == socket.AF_UNIX, 'socketpair_family')
        self.endpoint = Endpoint(actual, partial_send=partial_send, close_error=close_error)
        self.peer.setblocking(False)
        self.method, self.path, self.plan = method, path, plan
        self.partial_send = partial_send
        self.stop = threading.Event()
        self.deadline = time.monotonic() + 4.0
        self.request_complete = False
        self.request_bytes = 0
        self.sent_bytes = 0
        self.error = None
        self.thread = threading.Thread(target=self.serve, name='synthetic-control-peer', daemon=False)
        self.thread.start()

    def remaining(self):
        require(not self.stop.is_set(), 'peer_stopped')
        remaining = self.deadline - time.monotonic()
        require(remaining > 0, 'peer_deadline')
        return min(0.02, remaining)

    def request(self):
        raw = bytearray()
        required = None
        while required is None or len(raw) < required:
            ready, _, _ = select.select([self.peer], [], [], self.remaining())
            if not ready:
                continue
            try:
                part = self.peer.recv(4096)
            except BlockingIOError:
                continue
            if not part:
                require(self.partial_send and 0 < len(raw) < 8, 'peer_request_eof')
                self.request_bytes = len(raw)
                return
            raw.extend(part)
            require(len(raw) <= 24 << 10, 'peer_request_limit')
            if required is None and b'\r\n\r\n' in raw:
                boundary = raw.find(b'\r\n\r\n') + 4
                header = bytes(raw[:boundary])
                require(header.startswith((self.method + ' ' + self.path + ' HTTP/1.1\r\n').encode()),
                        'peer_request_target')
                length = 0
                for line in header.split(b'\r\n'):
                    if line.lower().startswith(b'content-length:'):
                        length = int(line.split(b':', 1)[1].strip())
                require(0 <= length <= 16 << 10, 'peer_request_body_limit')
                required = boundary + length
        require(len(raw) == required, 'peer_request_extra_bytes')
        self.request_complete = True
        self.request_bytes = len(raw)

    def send(self, raw):
        offset = 0
        while offset < len(raw):
            _, ready, _ = select.select([], [self.peer], [], self.remaining())
            if not ready:
                continue
            try:
                count = self.peer.send(raw[offset:offset + 65536])
            except BlockingIOError:
                continue
            require(count > 0, 'peer_send_incomplete')
            offset += count
            self.sent_bytes += count

    def serve(self):
        try:
            self.request()
            for action, value in self.plan:
                if action == 'send':
                    self.send(value)
                elif action == 'pause':
                    end = time.monotonic() + value
                    while time.monotonic() < end:
                        self.stop.wait(min(self.remaining(), max(0, end - time.monotonic())))
                else:
                    raise AssertionError('unknown_peer_action')
        except (BrokenPipeError, ConnectionResetError):
            pass
        except OSError as error:
            if not (self.stop.is_set() and error.errno in (errno.EBADF, errno.ENOTSOCK)):
                self.error = type(error).__name__
        except BaseException as error:
            if not (isinstance(error, AssertionError) and str(error) == 'peer_stopped' and self.stop.is_set()):
                self.error = code(error)
        finally:
            self.peer.close()

    def close_and_join(self):
        transport_closed = self.endpoint.fileno() == -1
        if transport_closed:
            self.thread.join(timeout=0.5)
        self.stop.set()
        self.endpoint.actual.close()
        self.thread.join(timeout=4.5)
        require(not self.thread.is_alive(), 'peer_thread_not_joined')
        require(self.peer.fileno() == -1, 'peer_endpoint_not_closed')
        require(self.error is None, 'peer_failure_' + str(self.error))
        require(transport_closed, 'harness_needed_to_close_transport')
        return {'transportClosedBeforeHarnessCleanup': transport_closed, 'peerClosed': True,
                'threadJoined': True, 'requestComplete': self.request_complete,
                'requestBytesObserved': self.request_bytes, 'responseBytesSent': self.sent_bytes,
                'virtualConnectCalls': self.endpoint.connects, 'sendCalls': self.endpoint.sends}


class SocketFactory:
    def __init__(self):
        self.pending = None
        self.peer = None
        self.constructor_attempts = self.pairs_created = 0
        self.closures = []

    def arm(self, method, path, plan, **options):
        require(self.pending is None and self.peer is None, 'socket_factory_still_owned')
        self.pending = (method, path, plan, options)

    def socket(self, family, kind):
        require(family == socket.AF_INET and kind == socket.SOCK_STREAM, 'transport_socket_signature')
        self.constructor_attempts += 1
        require(self.pending is not None and self.peer is None, 'unarmed_socket_constructor')
        method, path, plan, options = self.pending
        self.pending = None
        if options.pop('constructor_error', False):
            raise SyntheticConstructorError('synthetic_constructor_failure')
        self.peer = Peer(method, path, plan, **options)
        self.pairs_created += 1
        return self.peer.endpoint

    def close_peer(self):
        if self.peer is not None:
            try:
                self.closures.append(self.peer.close_and_join())
            finally:
                self.peer = None


class Fixture:
    def __init__(self, sources, name, ordinal):
        self.root = OUTPUT / name
        self.root.mkdir(mode=0o700)
        private = self.root / 'private'
        private.mkdir(mode=0o700)
        trace = private / 'journey'
        trace.mkdir(mode=0o700)
        self.module = definitions('_synthetic_transport_' + str(ordinal), 'transport', sources)
        self.module.E = self.root
        self.module.SOURCE = Path(PINS['transport']['path'])
        self.module.READER = Path(PINS['reader']['path'])
        self.module.JOURNEY = Path(PINS['journey']['path'])
        self.factory = SocketFactory()
        self.module.socket = types.SimpleNamespace(socket=self.factory.socket, AF_INET=socket.AF_INET,
            SOCK_STREAM=socket.SOCK_STREAM, SOL_SOCKET=socket.SOL_SOCKET, SO_ERROR=socket.SO_ERROR)
        self.ownership_checks = {'infrastructure': 0, 'application': 0}
        self.namespace_exits_fail = False
        self.reservations = []
        self.events = []
        self.rows = []
        context = {'scope': str(self.root), 'fixtureRoot': str(self.module.F), 'origin': self.module.ORIGIN,
                   'syntheticProtocolFixture': True}

        def check_infrastructure(_base, value):
            require(value is context and value['syntheticProtocolFixture'], 'synthetic_infrastructure_context')
            self.ownership_checks['infrastructure'] += 1

        def check_application():
            self.ownership_checks['application'] += 1

        @contextmanager
        def app_network(value):
            require(value is context and value['syntheticProtocolFixture'], 'synthetic_namespace_context')
            try:
                yield
            finally:
                if self.namespace_exits_fail:
                    raise SyntheticNamespaceExitError('synthetic_namespace_exit_failure')

        support = types.SimpleNamespace(check_infrastructure=check_infrastructure, app_network=app_network)
        application = types.SimpleNamespace(check_owned=check_application)
        module = self.module

        class CheckedScopeTransport(module.ControlTransport):
            def _load_reader(inner, pin):
                require(pin == PINS['reader'], 'adapted_reader_loader_pin')
                reader = definitions('_synthetic_transport_wire_' + str(ordinal), 'reader', sources)
                reader.E = ROOT
                reader.private_directory(CANDIDATE)
                reader.pinned(pin, Path(pin['path']), 256 << 10)
                return reader

        self.transport = CheckedScopeTransport(object(), support, context, application,
            deadline=time.monotonic() + 5.0, closure_deadline=time.monotonic() + 8.0,
            reader_pin=dict(PINS['reader']), source_pin=dict(PINS['transport']),
            reserve=lambda event: self.reservations.append(dict(event)),
            record=lambda event: self.events.append(dict(event)))
        self.journey_module = definitions('_synthetic_control_journey_' + str(ordinal), 'journey', sources)
        self.transport.install(self.journey_module)
        self.journey = self.journey_module.CatalogJourney(self.module.ORIGIN, str(self.module.F / 'media'),
            'synthetic-setup-token-only', {role: 'Synthetic-password-for-' + role for role in ('admin', 'visible', 'hidden')},
            'transport' + str(ordinal).zfill(2), str(trace))
        self.journey.bucket = 'setup'
        self.journey.budget = {'setup': 0}
        self.transport.bind(self.journey)

    def call(self, operation, expected=None):
        prior = len(self.transport.receipts)
        caught, returned = None, None
        try:
            returned = operation()
        except BaseException as error:
            caught = error
        self.factory.close_peer()
        if expected is None:
            require(caught is None, 'unexpected_transport_error_' + code(caught, self))
        else:
            require(caught is not None and code(caught, self) == expected,
                    'wrong_transport_error_' + code(caught, self))
        require(self.transport.active is None, 'transport_active_after_return')
        require(len(self.transport.receipts) == prior + 1, 'transport_receipt_not_persisted')
        pin = self.transport.receipts[-1]
        raw, observed = read_private(Path(pin['path']), self.module.RECEIPT_CAP)
        require(observed == pin, 'transport_receipt_pin')
        row = self.transport.reader.parse(raw)
        require(row['error'] == expected and row['connectionClosed'] is True and
                row['ownershipBefore'] is True and row['ownershipAfter'] is True and
                row['persistenceErrors'] == [] and row['postOwnershipErrors'] == [],
                'transport_receipt_outcome')
        if expected is None:
            require(row['bodyComplete'] is True and row['decoderBodyComplete'] is True and
                    row['rawFraming']['passed'] is True and row['responseClosed'] is True and
                    row['responseCloseErrors'] == row['connectionCloseErrors'] == row['namespaceExitErrors'] == [],
                    'successful_transport_response_incomplete')
        for field in ('rawHeader', 'rawWireBody', 'body'):
            _raw, observed = read_private(Path(row[field]['path']), self.module.CONTROL_CAP + self.module.WIRE_OVERHEAD + 1,
                                          empty=True)
            require(observed == row[field], 'transport_raw_evidence_pin')
        self.rows.append({'receipt': pin, 'error': row['error'], 'bodyComplete': row['bodyComplete'],
                          'dispatched': row['dispatched'], 'requestBytesSent': row['requestBytesSent'],
                          'bodyBytes': row['bodyBytes']})
        return returned, row

    def get(self, path, label='synthetic-get', expected=200):
        return self.journey._request('GET', path, expected, label=label)

    def finish(self):
        self.factory.close_peer()
        require(self.factory.pending is None, 'unused_armed_transport')
        return {'summary': self.transport.summary(), 'receipts': self.rows,
                'reservations': self.reservations, 'recordEvents': self.events,
                'ownershipSubstituteCalls': self.ownership_checks,
                'socketConstructorAttempts': self.factory.constructor_attempts,
                'socketpairsCreated': self.factory.pairs_created, 'closures': self.factory.closures,
                'journeyTracePersisted': self.journey.trace_persisted}


def header(length=None, *, chunked=False, status=200, cookie=False):
    reason = 'OK' if status == 200 else 'Synthetic Failure'
    raw = ('HTTP/1.1 %d %s\r\nContent-Type: application/json\r\nX-Request-ID: %s\r\nConnection: close\r\n' %
           (status, reason, REQUEST_ID)).encode()
    if length is not None:
        raw += ('Content-Length: %d\r\n' % length).encode()
    if chunked:
        raw += b'Transfer-Encoding: chunked\r\n'
    if cookie:
        raw += ('Set-Cookie: goby_session=' + SYNTHETIC_TOKEN + '; Path=/; HttpOnly\r\n').encode()
    return raw + b'\r\n'


def chunk(raw):
    return ('%x\r\n' % len(raw)).encode() + raw + b'\r\n'


def large_control(fixture):
    body = encoded({'Padding': 'x' * (768 << 10)})
    path = '/admin/v1/jobs'
    fixture.factory.arm('GET', path, [('send', header(len(body)) + body)])
    returned, row = fixture.call(lambda: fixture.get(path))
    require(fixture.module.CATALOG_CAP < len(body) <= fixture.module.CONTROL_CAP and
            row['bodyBytes'] == len(body) and row['bodyCap'] == fixture.module.CONTROL_CAP and
            row['wireCap'] == fixture.module.CONTROL_CAP + fixture.module.WIRE_OVERHEAD and
            returned == {'Padding': 'x' * (768 << 10)} and row['rawFraming']['kind'] == 'content-length',
            'control_body_cap_increment_not_exercised')
    require(fixture.reservations[0]['bodyCap'] == fixture.module.CONTROL_CAP,
            'control_reservation_used_catalog_cap')
    return {'checks': ['real CL decoding above both 512 KiB and the original reader wire cap',
                       'control body and reservation keep the full 1 MiB allowance'], 'bodyBytes': len(body)}


def oversized_catalog(fixture):
    body = encoded({'Padding': 'x' * fixture.module.CATALOG_CAP})
    path = '/emby/Users/' + USER_ID + '/Items?Limit=0'
    fixture.factory.arm('GET', path, [('send', header(chunked=True) + chunk(body) + b'0\r\n\r\n')])
    _returned, row = fixture.call(lambda: fixture.get(path), 'response_body_limit')
    require(row['bodyCap'] == fixture.module.CATALOG_CAP and row['bodyBytes'] == fixture.module.CATALOG_CAP + 1 and
            row['bodyComplete'] is False and row['dispatched'] is True,
            'catalog_body_cap_was_widened')
    return {'checks': ['chunked catalog body remains limited to 512 KiB', 'oversized body stays partial and failed']}


def complete_chunked(fixture):
    body = encoded({'Items': [], 'TotalRecordCount': 0})
    path = '/admin/v1/jobs'
    wire = chunk(body[:9]) + chunk(body[9:]) + b'0\r\n\r\n'
    fixture.factory.arm('GET', path, [('send', header(chunked=True) + wire[:12]),
                                    ('pause', 0.02), ('send', wire[12:])])
    returned, row = fixture.call(lambda: fixture.get(path))
    wire_raw, _pin = read_private(Path(row['rawWireBody']['path']), 65536)
    body_raw, _pin = read_private(Path(row['body']['path']), 65536)
    require(returned == {'Items': [], 'TotalRecordCount': 0} and wire_raw == wire and body_raw == body and
            wire_raw != body_raw and row['rawFraming']['kind'] == 'chunked' and
            row['rawFraming']['terminalBoundaryComplete'] is True, 'complete_chunked_binding')
    return {'checks': ['complete chunked framing accepted', 'raw transfer and decoded JSON remain distinct']}


def missing_chunk_terminator(fixture):
    body = encoded({'AccessToken': SYNTHETIC_TOKEN})
    path = '/emby/Users/AuthenticateByName'
    fixture.factory.arm('POST', path, [('send', header(chunked=True) + chunk(body) + b'0\r\n')])
    _returned, row = fixture.call(lambda: fixture.journey._login_emby('visible'),
                                  'transport_chunk_trailer_incomplete')
    require(row['decoderBodyComplete'] is True and row['bodyComplete'] is False and
            row['rawFraming'] == {'checked': True, 'passed': False} and
            fixture.journey.emby['visible']['value'] == SYNTHETIC_TOKEN and
            fixture.journey.emby['visible'] in fixture.journey.credentials and
            row['unknownMutationOutcome'] is True, 'framing_failure_lost_captured_credential')
    return {'checks': ['missing trailer final empty line rejected after decoder completion',
                       'real Emby on_json callback retained the synthetic credential before framing failure'],
            'credentialCaptured': True, 'credentialIsSynthetic': True}


def credential_callback_order(fixture):
    path = '/admin/v1/session'
    partial = b'{"Incomplete":true}'
    fixture.factory.arm('POST', path, [('send', header(len(partial) + 9, cookie=True) + partial)])
    _returned, first = fixture.call(fixture.journey._login_admin, 'response_body_incomplete')
    require(fixture.journey.admin is not None and fixture.journey.admin['value'] == SYNTHETIC_TOKEN and
            fixture.journey.admin in fixture.journey.credentials and first['bodyComplete'] is False,
            'header_callback_lost_cookie_before_body_failure')
    body = encoded({'AccessToken': SYNTHETIC_TOKEN})
    path = '/emby/Users/AuthenticateByName'
    fixture.factory.arm('POST', path, [('send', header(len(body), status=500) + body)])
    _returned, second = fixture.call(lambda: fixture.journey._login_emby('visible'), 'unexpected_http_status')
    require(fixture.journey.emby['visible']['value'] == SYNTHETIC_TOKEN and
            fixture.journey.emby['visible'] in fixture.journey.credentials and second['decoderBodyComplete'] is True and
            second['rawFraming']['passed'] is True and second['status'] == 500 and
            len(fixture.journey.credentials) == 2, 'json_callback_lost_token_before_status_failure')
    return {'checks': ['real admin on_headers retained a synthetic cookie before a short-body failure',
                       'real Emby on_json retained a synthetic token before unexpected status rejection'],
            'syntheticCredentialKindsCaptured': ['admin', 'emby']}


def first_error_with_cleanup_failures(fixture):
    real_client = fixture.module.http.client

    class FailingCloseResponse(real_client.HTTPResponse):
        def close(self):
            super().close()
            raise SyntheticResponseCloseError('synthetic_response_close_failure')

    fixture.module.http = types.SimpleNamespace(client=types.SimpleNamespace(
        HTTPResponse=FailingCloseResponse, IncompleteRead=real_client.IncompleteRead))
    fixture.namespace_exits_fail = True
    path = '/admin/v1/jobs'
    partial = b'{"Incomplete":true}'
    fixture.factory.arm('GET', path, [('send', header(len(partial) + 9) + partial)], close_error=True)
    _returned, row = fixture.call(lambda: fixture.get(path), 'response_body_incomplete')
    require(row['responseCloseErrors'] == ['SyntheticResponseCloseError'] and
            row['connectionCloseErrors'] == ['SyntheticSocketCloseError'] and
            row['namespaceExitErrors'] == ['SyntheticNamespaceExitError'] and
            row['responseClosed'] is False and row['connectionClosed'] is True and row['bodyComplete'] is False,
            'cleanup_failure_overwrote_first_exchange_error')
    return {'checks': ['first truncated-body rejection retained',
                       'response/socket/context failures recorded separately without inventing responseClosed'],
            'injection': 'real response and socket close first; fixed synthetic exceptions follow'}


def policy_body():
    return {'Revision': '1', 'Name': 'synthetic-visible', 'IsAdministrator': False, 'IsDisabled': False,
            'Policy': {'EnableAllFolders': False, 'EnabledFolders': [LIBRARY_ID], 'EnableMediaPlayback': True,
                       'EnablePlaybackRemuxing': False, 'EnableAudioPlaybackTranscoding': False,
                       'EnableVideoPlaybackTranscoding': False}}


def put_route_and_mutation_guard(fixture):
    path = '/admin/v1/users/' + USER_ID
    body = encoded({'User': {'Id': USER_ID}})
    fixture.factory.arm('PUT', path, [('send', header(len(body)) + body)])
    _returned, first = fixture.call(lambda: fixture.journey._request('PUT', path, 200,
        body=policy_body(), label='set-policy-visible'))
    before = dict(fixture.transport.counts)
    _returned, duplicate = fixture.call(lambda: fixture.journey._request('PUT', path, 200,
        body=policy_body(), label='set-policy-visible'), 'transport_mutation_already_attempted')
    require(first['route'] == 'set-user-policy' and first['method'] == 'PUT' and
            duplicate['dispatched'] is False and duplicate['connectionCreated'] is False and
            fixture.transport.counts == before and fixture.factory.pairs_created == 1,
            'mutation_duplicate_created_second_request')
    receipts = len(fixture.transport.receipts)
    try:
        fixture.journey._request('POST', path, 200, body=policy_body(), label='wrong-policy-method')
    except fixture.module.Rejected as error:
        require(str(error) == 'transport_route_not_admitted', 'wrong_policy_method_error')
    else:
        raise AssertionError('post_policy_route_was_admitted')
    require(len(fixture.transport.receipts) == receipts and fixture.factory.pairs_created == 1,
            'invalid_route_reached_exchange')
    fixture.journey.phase = 'cached'
    fixture.factory.arm('PUT', path, [('send', header(len(body)) + body)])
    _returned, next_phase = fixture.call(lambda: fixture.journey._request('PUT', path, 200,
        body=policy_body(), label='set-policy-visible'))
    require(next_phase['dispatched'] is True and fixture.transport.counts['setup'] == 2 and
            fixture.journey.writes == {('prepared', 'set-policy-visible'), ('cached', 'set-policy-visible')},
            'mutation_guard_lost_phase_binding')
    return {'checks': ['real fixed user-policy route uses PUT and rejects POST',
                       'same phase and label cannot dispatch again', 'a distinct phase has a distinct mutation key']}


def dispatch_accounting(fixture):
    path = '/admin/v1/jobs'
    fixture.factory.arm('GET', path, [], constructor_error=True)
    _returned, before_connect = fixture.call(lambda: fixture.get(path), 'SyntheticConstructorError')
    require(before_connect['connectionCreated'] is False and before_connect['connectionAttempted'] is False and
            before_connect['dispatched'] is False and fixture.transport.counts['setup'] == 0 and
            fixture.transport.connection_attempts['setup'] == 0 and fixture.factory.pairs_created == 0,
            'preconnect_failure_counted_dispatch')
    path = '/admin/v1/session'
    fixture.factory.arm('POST', path, [], partial_send=True)
    _returned, partial = fixture.call(lambda: fixture.journey._request('POST', path, 200,
        body={'Name': 'synthetic-admin', 'Password': 'Synthetic-password-only'}, label='partial-native-login'),
        'SyntheticSendError')
    require(partial['dispatched'] is True and partial['requestBytesSent'] == 7 and
            partial['requestFullySent'] is False and partial['unknownMutationOutcome'] is True and
            fixture.transport.counts['setup'] == 1 and fixture.transport.connection_attempts['setup'] == 1 and
            fixture.factory.closures[-1]['requestBytesObserved'] == 7 and
            fixture.factory.closures[-1]['requestComplete'] is False,
            'partial_positive_send_count_not_exact')
    return {'checks': ['constructor failure before connect is zero dispatch',
                       'a real positive partial send counts once and retains unknown mutation outcome'],
            'partialBytesActuallySent': 7}


def permissions():
    files, directories = 0, 0
    for path in (OUTPUT, *OUTPUT.rglob('*')):
        info = path.lstat()
        require(not stat.S_ISLNK(info.st_mode), 'output_symlink')
        if stat.S_ISDIR(info.st_mode):
            private_directory(path)
            directories += 1
        else:
            require(stat.S_ISREG(info.st_mode) and info.st_uid == info.st_gid == 0 and
                    stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1, 'output_file_permissions')
            files += 1
    return {'directories': directories, 'filesBeforeResult': files,
            'directoriesRoot0700': True, 'filesRoot0600SingleLink': True}


def main():
    parser = argparse.ArgumentParser(description='Check frozen control transport using private synthetic socketpairs.')
    for kind in PINS:
        parser.add_argument('--' + kind, required=True)
        parser.add_argument('--' + kind + '-sha256', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    require(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0 and
            sys.flags.isolated == 1 and sys.flags.dont_write_bytecode == 1,
            'remote_linux_root_isolated_no_bytecode_required')
    require(Path(__file__) == CANDIDATE / 'capacity-transport-checks.py' and Path(args.output) == OUTPUT,
            'fixed_check_script_or_output')
    os.umask(0o077)
    sources = {}
    for kind, expected in PINS.items():
        require(getattr(args, kind) == expected['path'] and getattr(args, kind + '_sha256') == expected['sha256'],
                'fixed_source_cli_pin')
        raw, observed = read_private(Path(expected['path']), 256 << 10)
        require(observed == expected, 'source_pin_changed')
        sources[kind] = raw
    _raw, script_pin = read_private(Path(__file__), 256 << 10)
    private_directory(OUTPUT.parent)
    OUTPUT.mkdir(mode=0o700)
    sys.addaudithook(audit)
    previous = signal.signal(signal.SIGALRM, timeout)
    signal.setitimer(signal.ITIMER_REAL, 60.0)
    started = time.monotonic_ns()
    cases = [('large-control-content-length', large_control), ('catalog-body-cap', oversized_catalog),
             ('complete-chunked', complete_chunked), ('incomplete-chunk-trailer', missing_chunk_terminator),
             ('credential-callback-order', credential_callback_order),
             ('first-error-and-cleanup-errors', first_error_with_cleanup_failures),
             ('put-route-and-mutation-guard', put_route_and_mutation_guard),
             ('positive-send-dispatch-accounting', dispatch_accounting)]
    results = []
    try:
        for ordinal, (name, operation) in enumerate(cases, 1):
            fixture = None
            row = {'name': name, 'passed': False}
            try:
                fixture = Fixture(sources, name, ordinal)
                row['checks'] = operation(fixture)
                row['passed'] = True
            except BaseException as error:
                row['error'] = code(error, fixture)
            finally:
                if fixture is not None:
                    try:
                        row['evidence'] = fixture.finish()
                    except BaseException as error:
                        row['passed'] = False
                        row['closureError'] = code(error, fixture)
            results.append(row)
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, previous)
    passed = len(results) == 8 and all(row['passed'] for row in results) and not AUDIT_DENIALS
    result = {'kind': 'native-capacity-control-transport-synthetic-checks', 'version': 1, 'scope': str(ROOT),
              'sources': PINS, 'operator': script_pin, 'status': 'passed' if passed else 'failed',
              'elapsedSeconds': (time.monotonic_ns() - started) / 1_000_000_000,
              'cases': results, 'forbiddenActionAttempts': AUDIT_DENIALS, 'permissions': permissions(),
              'boundaries': {
                  'real': ['ControlTransport constructor, install and bind', 'installed private Journey._request',
                           'original journey credential callbacks', 'route, body and mutation validation',
                           'real WireFile and HTTPResponse parsing', 'independent raw framing validation',
                           'actual positive socketpair send counts', 'private trace and raw receipt persistence'],
                  'sourceAdmission': 'pin-checked frozen definitions; only private file/output scope and reader loader adapted',
                  'ownershipAndNamespace': 'explicit no-operation application/infrastructure/namespace substitutes',
                  'connection': 'virtual AF_INET constructor/connect backed solely by an AF_UNIX socketpair',
                  'singleThreadEntry': 'real entry gate runs before the lazily created synthetic peer thread',
                  'closeErrors': 'one case raises fixed errors after real response/socket closes and on synthetic context exit',
                  'workloadBinding': 'actual private Journey with minimal external budget/bucket metadata; no workload execute',
                  'responsesAndCredentials': 'synthetic fixtures only; no Goby or upstream response is claimed',
                  'processes': 'no child process, actor, main, SQL, unit operation or external network request',
                  'notProven': ['actual controller or namespace admission', 'actual Goby HTTP or credential behavior',
                                'native scanning or throughput', 'parent resource closure',
                                'the existing twelve reader cases or other reader behavior']}}
    pin = write_private(OUTPUT / 'result.json', encoded(result))
    print(json.dumps({'status': result['status'], 'result': pin,
                      'passedCases': sum(row['passed'] for row in results), 'totalCases': len(results)}, sort_keys=True))
    return 0 if passed else 1


if __name__ == '__main__':
    sys.tracebacklimit = 0
    try:
        raise SystemExit(main())
    except Exception as error:
        print(json.dumps({'kind': 'capacity-transport-checks-rejected', 'error': code(error),
                          'nativeWorkloadExecuted': False}, sort_keys=True))
        raise SystemExit(1)
