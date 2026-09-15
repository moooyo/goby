"""Fixed native capacity controller HTTP transport; importing performs no work.

Contract:
  ControlTransport(base, support, context, application, *, deadline,
      closure_deadline, reader_pin, source_pin, reserve, record=None)
  transport.install(private_journey_module)
  transport.readiness('/healthz' | '/readyz') -> {status, body, receipt}
  workload = capacity_workload.make_workload(...,
      prior_setup_requests=transport.counts['setup'])
  transport.bind(workload)
  workload.execute()
  transport.set_phase_deadline(parent_absolute_closure_deadline, cleaning=True)
  workload.cleanup(deadline=parent_absolute_closure_deadline)

deadline and closure_deadline are finite absolute time.monotonic() seconds,
frozen by the parent from the original anchor lifetime before construction.
The setter can shorten business time or make one transition to a deadline no
later than the frozen closure_deadline. It cannot return to business or extend
the closure boundary. The parent remains responsible for all service/resource
ownership, whole-scope deadlines, task/reader cancellation and physical closure.

base is retained for support.check_infrastructure(base, context). support must
provide that method and app_network(context). application.check_owned() must
check the admitted live application. These checks run before and after HTTP;
none run inside socket wait/read callbacks. context is the newly generated
fixture context, with scope, fixtureRoot, origin and namespace bindings. This
module never reads an old environment/configuration or a key, runs SQL, starts
or stops a process, or changes network policy.

reader_pin and source_pin are exact {path, sha256, bytes} descriptors for
E/private/capacity-reader.py and E/private/capacity-transport.py. The reader is
loaded as a fresh, private definitions-only module under a non-main name. Only
its WireFile, clock_anchor, parse and file helpers are used. Its WIRE_CAP is set
in that private instance to this one request's body cap + 64 KiB. This is the
explicit transport increment beyond the verified reader's 512 KiB cap: 1 MiB
control responses are supported, while catalog GETs still allow only 512 KiB.
No running reader module, source file or process-global http.client is patched.

install replaces CatalogJourney only in the admitted private journey module
with a subclass whose _request has the original signature. All inherited
authentication helpers and the capacity workload's own _request remain in the
super chain. This keeps credential callbacks and trace semantics without the
old request method's exception-overwriting repeated close path. bind verifies
the one workload instance and the actual prior readiness dispatch count; it
does not wrap a second method or issue a request.

reserve(event) is required and runs before every possible connection, outside
HTTP timing. It must reject before work if the parent's whole-scope disk, raw
evidence, request or remaining-lifetime budgets cannot cover the maximum next
request. event has event, sequence, bucket, phase, transportEvidenceBytes,
maximumAdditionalBytes, bodyCap, wireCap, cleanup and phaseDeadlineMonotonicNs.
It is an admission callback, not a request to permanently charge maximum bytes
for every prior request. Actual bytes are transport.evidence_bytes; the parent
also accounts for its other files and concurrent workers. This transport alone
enforces a cumulative 32 MiB allowance for its files, including cleanup HTTP.
The parent must not sum reader, metric and cleanup upper bounds into an
unreserved claim that exceeds its global 384 MiB allowance.

record, if supplied, receives only an event, sequence, count and a private
receipt descriptor. summary() likewise exposes counts, limits and descriptors.
Full response headers (including any Set-Cookie) and decoded/transfer-encoded
bodies stay in exclusively created root-private files. Request headers and
credentials are never written; only request-body byte count/hash is retained.

Counters distinguish intent, connection attempt, first-send attempt and actual
positive-byte HTTP dispatch. A successful socket connect does not count as an
HTTP dispatch or prove arrival at the service. A partial positive send counts
once and remains incomplete. Response request ID is separate arrival evidence.
The absolute ten-second HTTP ceiling starts before connect. Dispatch-to-header
and dispatch-to-body timings start at the first positive send attempt; connect
duration and evidence-write duration are recorded separately. No retry occurs.

Raw response capture, callbacks and pure checks share the same received bytes.
on_headers executes before status/body validation, and on_json before status
validation, so newly issued credentials remain available for owned cleanup on
an otherwise failed reply. Emby revoked checks accept text/plain only when the
caller explicitly sets json_body=False; administrator revoked checks stay JSON.
Any first exchange/callback failure is retained through response/socket close.
Close failures are recorded independently and can never make a call successful.

HTTPResponse decoder completion is recorded separately from bodyComplete. The
transport additionally parses the saved raw transfer framing: chunk sizes,
data CRLF, the zero chunk, complete trailer termination and exact byte
consumption must match the decoded body. A Content-Length body must match its
declared length, decoded bytes and all received wire bytes; an unframed body
requires observed EOF. Credential callbacks run before this independent gate,
so a valid credential carried by otherwise malformed framing remains available
for cleanup. The framing gate performs no additional socket read.
"""

import errno
import hashlib
import http.client
import json
import math
import os
from pathlib import Path
import re
import select
import socket
import stat
import sys
import time
import types
from urllib.parse import parse_qsl, urlsplit


E = Path('/opt/goby-test/native-scan-http-capacity-20260915')
F = Path('/opt/goby-native-scan-http-capacity-20260915')
SOURCE = E / 'private/capacity-transport.py'
READER = E / 'private/capacity-reader.py'
JOURNEY = E / 'private/native-catalog-journey.py'
READER_SHA = 'a652245cd5c93e21959d3c328e68ee67211f8bb92fc4c19aef960162275cfb20'
READER_BYTES = 46356
JOURNEY_SHA = '63125715ad415349f3ae95a008f56783800c7334d90efcbcb394a14215e695d4'
ORIGIN = 'http://127.0.0.1:18099'
COUNTER_LIMITS = {'setup': 112, 'task-poll': 184, 'cleanup': 64}
CATALOG_CAP = 512 << 10
CONTROL_CAP = 1 << 20
REQUEST_BODY_CAP = 16 << 10
WIRE_OVERHEAD = 64 << 10
RECEIPT_CAP = 32 << 10
EVIDENCE_CAP = 32 << 20
HTTP_SECONDS = 10
ID = r'[0-9a-f]{32}'


class Rejected(Exception):
    """A fixed, secret-free transport failure code."""


def need(condition, code):
    if not condition:
        raise Rejected(code)


def encoded(value):
    return (json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + '\n').encode()


def finite_deadline(value):
    need(type(value) in (int, float) and math.isfinite(value) and value > time.monotonic(),
         'transport_absolute_deadline_contract')
    return float(value)


def route(method, path):
    """Accept only routes and pagination shapes used by this one workload."""
    need(method in ('GET', 'POST', 'PUT', 'DELETE') and type(path) is str and
         len(path) <= 512 and not path.endswith('?') and '\r' not in path and '\n' not in path,
         'transport_request_shape')
    parsed = urlsplit(path)
    need(not parsed.scheme and not parsed.netloc and not parsed.fragment and
         parsed.path.startswith('/') and not parsed.path.startswith('//'), 'transport_external_target')
    pairs = parse_qsl(parsed.query, keep_blank_values=True, strict_parsing=True) if parsed.query else []
    query = dict(pairs)
    need(len(query) == len(pairs), 'transport_duplicate_query_parameter')
    plain = parsed.path
    result = None
    if method == 'GET' and not query:
        fixed = {'/healthz': 'health', '/readyz': 'ready', '/admin/v1/bootstrap': 'bootstrap-status',
                 '/admin/v1/session': 'native-session', '/admin/v1/libraries': 'libraries',
                 '/admin/v1/jobs': 'jobs', '/admin/v1/tasks': 'tasks'}
        result = fixed.get(plain)
        if re.fullmatch('/admin/v1/users/' + ID, plain):
            result = 'managed-user'
        if re.fullmatch('/admin/v1/tasks/' + ID, plain):
            result = 'task-definition'
        if re.fullmatch('/emby/Users/' + ID + '/Items/' + ID, plain):
            result = 'catalog-detail'
    if method == 'GET' and re.fullmatch('/admin/v1/task-runs/' + ID, plain) and query == {
            'StartIndex': '0', 'Limit': '2'}:
        result = 'run-detail'
    if method == 'GET' and re.fullmatch('/admin/v1/tasks/' + ID + '/runs', plain) and query == {
            'StartIndex': '0', 'Limit': '4'}:
        result = 'run-history'
    if method == 'GET' and re.fullmatch('/emby/Users/' + ID + '/Items', plain):
        common = {'Recursive': 'true', 'IncludeItemTypes': 'Movie,Episode,Audio'}
        if query == {'Limit': '0'} or query == dict(common, Limit='0'):
            result = 'catalog-count'
        if query == dict(common, SortBy='SortName', StartIndex='0', Limit='64'):
            result = 'catalog-page'
    if method == 'POST' and not query:
        result = {'/admin/v1/bootstrap': 'bootstrap', '/admin/v1/session': 'native-login',
                  '/admin/v1/libraries': 'create-library', '/admin/v1/users': 'create-user',
                  '/emby/Users/AuthenticateByName': 'emby-login',
                  '/emby/Sessions/Logout': 'emby-logout'}.get(plain)
        if re.fullmatch('/admin/v1/tasks/' + ID + '/runs', plain):
            result = 'admit-run'
        if re.fullmatch('/admin/v1/task-runs/' + ID + '/cancel', plain):
            result = 'cancel-run'
        if re.fullmatch('/emby/Users/' + ID + '/FavoriteItems/' + ID, plain):
            result = 'favorite'
    if method == 'PUT' and not query and re.fullmatch('/admin/v1/users/' + ID, plain):
        result = 'set-user-policy'
    if method == 'DELETE' and plain == '/admin/v1/session' and not query:
        result = 'native-logout'
    need(result is not None and (not parsed.query or bool(query)), 'transport_route_not_admitted')
    return {'name': result, 'path': plain, 'query': query,
            'bodyCap': CATALOG_CAP if result.startswith('catalog-') else CONTROL_CAP}


def body_contract(kind, body):
    """Restrict mutation bodies to the actual fixed native workload schemas."""
    fields = {'bootstrap': {'SetupToken', 'Name', 'Password'}, 'native-login': {'Name', 'Password'},
              'create-library': {'Name', 'CollectionType', 'Paths', 'Scan'},
              'create-user': {'Name', 'Password', 'IsAdministrator'},
              'set-user-policy': {'Revision', 'Name', 'IsAdministrator', 'IsDisabled', 'Policy'},
              'emby-login': {'Username', 'Pw'}, 'admit-run': {'RequestId'}, 'cancel-run': set()}
    if kind not in fields:
        need(body is None, 'transport_unexpected_request_body')
        return
    need(type(body) is dict and set(body) == fields[kind], 'transport_body_fields')
    if kind == 'create-library':
        need(body['Name'] in ('Capacity A', 'Capacity B') and body['CollectionType'] == 'mixed' and
             body['Paths'] == [str(F / 'media' / body['Name'][-1])] and body['Scan'] is False,
             'transport_library_body')
    if kind == 'create-user':
        need(body['IsAdministrator'] is False, 'transport_user_privilege')
    if kind == 'set-user-policy':
        policy = body['Policy']
        fields = {'EnableAllFolders', 'EnabledFolders', 'EnableMediaPlayback', 'EnablePlaybackRemuxing',
                  'EnableAudioPlaybackTranscoding', 'EnableVideoPlaybackTranscoding'}
        need(type(policy) is dict and set(policy) == fields and body['IsAdministrator'] is False and
             body['IsDisabled'] is False and type(body['Revision']) is str and
             re.fullmatch('[1-9][0-9]*', body['Revision']) is not None and
             policy['EnableAllFolders'] is False and policy['EnableMediaPlayback'] is True and
             all(policy[field] is False for field in ('EnablePlaybackRemuxing',
                 'EnableAudioPlaybackTranscoding', 'EnableVideoPlaybackTranscoding')) and
             type(policy['EnabledFolders']) is list and len(policy['EnabledFolders']) == 1 and
             re.fullmatch(ID, policy['EnabledFolders'][0]) is not None, 'transport_policy_body')
    if kind == 'admit-run':
        need(type(body['RequestId']) is str and re.fullmatch(ID, body['RequestId']) is not None,
             'transport_admission_request_id')


def allowed_status(kind, expected, json_body):
    expected_map = {'health': {200}, 'ready': {200, 503}, 'bootstrap-status': {200},
        'bootstrap': {201}, 'native-login': {200}, 'native-session': {200, 401},
        'native-logout': {204}, 'libraries': {200}, 'create-library': {201}, 'jobs': {200},
        'create-user': {201}, 'managed-user': {200}, 'set-user-policy': {200}, 'emby-login': {200},
        'emby-logout': {204}, 'tasks': {200}, 'task-definition': {200}, 'run-detail': {200},
        'run-history': {200}, 'admit-run': {202}, 'cancel-run': {202}, 'favorite': {200},
        'catalog-detail': {200, 404}, 'catalog-count': {200, 401, 403}, 'catalog-page': {200}}
    statuses = {expected} if type(expected) is int else set(expected)
    need(statuses and statuses <= expected_map[kind] and type(json_body) is bool,
         'transport_expected_status_contract')
    need(json_body is (not (statuses == {204} or kind == 'catalog-count' and statuses == {401})),
         'transport_response_parser_contract')
    return statuses


def _chunked_body(raw, maximum):
    """Independently require complete chunk/trailer framing, including final CRLF."""
    position, decoded, trailer_bytes = 0, bytearray(), 0
    while True:
        end = raw.find(b'\r\n', position)
        need(end >= 0 and end - position <= 32768, 'transport_chunk_size_line_incomplete')
        line = raw[position:end]
        size_text = line.split(b';', 1)[0]
        need(re.fullmatch(b'[0-9A-Fa-f]+', size_text) is not None and
             all(byte >= 32 and byte != 127 for byte in line), 'transport_chunk_size_syntax')
        size = int(size_text, 16)
        position = end + 2
        need(size <= maximum - len(decoded), 'transport_decoded_chunk_body_limit')
        if size == 0:
            while True:
                end = raw.find(b'\r\n', position)
                need(end >= 0, 'transport_chunk_trailer_incomplete')
                trailer = raw[position:end]
                trailer_bytes += end + 2 - position
                need(trailer_bytes <= 32768, 'transport_chunk_trailer_limit')
                position = end + 2
                if not trailer:
                    need(position == len(raw), 'transport_chunk_trailing_bytes')
                    return bytes(decoded)
                name, separator, value = trailer.partition(b':')
                need(separator == b':' and re.fullmatch(b"[!#$%&'*+.^_`|~0-9A-Za-z-]+", name) is not None and
                     name.lower() not in (b'content-length', b'transfer-encoding', b'content-type', b'content-encoding') and
                     all(byte == 9 or byte >= 32 and byte != 127 for byte in value),
                     'transport_chunk_trailer_syntax')
        need(position + size + 2 <= len(raw) and raw[position + size:position + size + 2] == b'\r\n',
             'transport_chunk_data_incomplete')
        decoded.extend(raw[position:position + size])
        position += size + 2


def validate_raw_framing(raw, decoded, *, content_length, transfer_encoding, status, eof, body_cap):
    """Bind every received transfer byte to the decoded body without trusting HTTPResponse."""
    need(type(raw) is bytes and type(decoded) is bytes and type(body_cap) is int and
         body_cap in (CATALOG_CAP, CONTROL_CAP) and
         len(decoded) <= body_cap and len(raw) <= body_cap + WIRE_OVERHEAD and
         type(status) is int and type(eof) is bool, 'transport_raw_framing_contract')
    need(not (content_length is not None and transfer_encoding is not None),
         'transport_ambiguous_response_framing')
    if content_length is not None:
        need(type(content_length) is str and re.fullmatch('[0-9]+', content_length) is not None and
             int(content_length) <= body_cap, 'transport_response_content_length')
    if status == 204:
        need(transfer_encoding is None and (content_length is None or int(content_length) == 0) and raw == decoded == b'',
             'transport_empty_status_raw_framing')
        kind = 'empty-status'
    elif transfer_encoding is not None:
        need(type(transfer_encoding) is str and transfer_encoding.lower() == 'chunked',
             'transport_response_transfer_encoding')
        need(_chunked_body(raw, body_cap) == decoded, 'transport_chunk_decoding_binding')
        kind = 'chunked'
    elif content_length is not None:
        need(len(raw) == int(content_length) and raw == decoded, 'transport_content_length_raw_framing')
        kind = 'content-length'
    else:
        need(eof and raw == decoded, 'transport_eof_raw_framing')
        kind = 'connection-close'
    return {'checked': True, 'passed': True, 'kind': kind, 'wireBytes': len(raw),
            'decodedBytes': len(decoded), 'terminalBoundaryComplete': True, 'exactWireConsumption': True}


class ControlTransport:
    def __init__(self, base, support, context, application, *, deadline, closure_deadline,
                 reader_pin, source_pin, reserve, record=None):
        need(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0 and
             sys.flags.isolated == 1 and sys.flags.dont_write_bytecode == 1,
             'transport_linux_root_isolated_required')
        need(context.get('scope') == str(E) and context.get('fixtureRoot') == str(F) and
             context.get('origin') == ORIGIN and callable(reserve) and
             (record is None or callable(record)), 'transport_parent_contract')
        self.business_deadline = finite_deadline(deadline)
        self.closure_deadline = finite_deadline(closure_deadline)
        need(self.business_deadline <= self.closure_deadline, 'transport_phase_order')
        self.deadline, self.cleaning = self.business_deadline, False
        self.base, self.support, self.context, self.application = base, support, context, application
        self.reserve, self.record_callback = reserve, record
        self.reader = self._load_reader(reader_pin)
        self.reader.pinned(source_pin, SOURCE, 256 << 10)
        need(Path(__file__) == SOURCE, 'transport_source_location')
        self.source_pin, self.reader_pin = dict(source_pin), dict(reader_pin)
        self.output = E / 'private/control-http'
        self.reader.private_directory(self.output.parent)
        self.output.mkdir(mode=0o700)
        self.reader.private_directory(self.output)
        self.evidence_bytes = 0
        self.evidence_write_ns = 0
        self.counts = {key: 0 for key in COUNTER_LIMITS}
        self.connection_attempts = {key: 0 for key in COUNTER_LIMITS}
        self.admitted_attempts = {key: 0 for key in COUNTER_LIMITS}
        self.intent_counts = {key: 0 for key in COUNTER_LIMITS}
        self.receipts = []
        self.sequence = 0
        self.active = None
        self.journey_module = self.journey_class = self.workload = None
        self.installation_pin = None
        self.readiness_requests = 0

    def _load_reader(self, pin):
        need(type(pin) is dict and set(pin) == {'path', 'sha256', 'bytes'} and
             pin == {'path': str(READER), 'sha256': READER_SHA, 'bytes': READER_BYTES},
             'transport_reader_pin_contract')
        need(READER.resolve() == READER, 'transport_reader_symlink')
        fd = os.open(READER, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW)
        try:
            before = os.fstat(fd)
            need(stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and
                 stat.S_IMODE(before.st_mode) == 0o600 and before.st_nlink == 1 and
                 before.st_size == READER_BYTES, 'transport_reader_file_contract')
            raw = os.read(fd, READER_BYTES + 1)
            after = os.fstat(fd)
            need(len(raw) == READER_BYTES and hashlib.sha256(raw).hexdigest() == READER_SHA and
                 (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) ==
                 (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns),
                 'transport_reader_changed')
        finally:
            os.close(fd)
        module = types.ModuleType('_native_capacity_transport_wire_definitions')
        module.__file__ = str(READER)
        exec(compile(raw, str(READER), 'exec'), module.__dict__)
        module.private_directory(READER.parent)
        module.pinned(pin, READER, 256 << 10)
        return module

    def _error(self, error):
        recognized = (Rejected, self.reader.Rejected)
        if self.journey_module is not None:
            recognized += (self.journey_module.JourneyError,)
        return str(error) if isinstance(error, recognized) else type(error).__name__

    def _save(self, name, raw):
        need(re.fullmatch(r'[0-9]{3}-[a-z-]+\.(json|raw)', name) is not None and type(raw) is bytes,
             'transport_evidence_name')
        need(len(raw) <= CONTROL_CAP + WIRE_OVERHEAD + 1 and
             self.evidence_bytes + len(raw) <= EVIDENCE_CAP, 'transport_evidence_allowance')
        started = time.monotonic_ns()
        path = self.output / name
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_CLOEXEC | os.O_NOFOLLOW, 0o600)
        try:
            offset = 0
            while offset < len(raw):
                count = os.write(fd, raw[offset:])
                need(count > 0, 'transport_evidence_short_write')
                offset += count
                self.evidence_bytes += count
            os.fsync(fd)
            info = os.fstat(fd)
            need(info.st_size == len(raw) and info.st_uid == info.st_gid == 0 and info.st_nlink == 1 and
                 stat.S_IMODE(info.st_mode) == 0o600, 'transport_evidence_file_identity')
        finally:
            try:
                os.close(fd)
            finally:
                self.evidence_write_ns += time.monotonic_ns() - started
        return {'path': str(path), 'sha256': hashlib.sha256(raw).hexdigest(), 'bytes': len(raw)}

    def install(self, module):
        need(self.active is None and self.journey_module is None and self.workload is None and
             getattr(module, '__file__', None) == str(JOURNEY), 'transport_journey_install_order')
        raw, pin = self.reader.read_private(JOURNEY, 256 << 10)
        need(hashlib.sha256(raw).hexdigest() == JOURNEY_SHA and
             module.CatalogJourney._request.__module__ == module.__name__, 'transport_journey_source')
        original = module.CatalogJourney
        transport = self

        class CapacityControlJourney(original):
            def _request(self, method, path, expected, *, body=None, credential=None,
                         label, json_body=True, headers=None, on_headers=None, on_json=None,
                         deadline=None, cleanup_request=False):
                return transport._request(self, method, path, expected, body=body, credential=credential,
                    label=label, json_body=json_body, headers=headers, on_headers=on_headers,
                    on_json=on_json, deadline=deadline, cleanup_request=cleanup_request)

        module.CatalogJourney = CapacityControlJourney
        self.journey_module, self.journey_class = module, CapacityControlJourney
        self.installation_pin = {key: pin[key] for key in ('path', 'sha256', 'bytes')}
        return self.installation_pin

    def bind(self, workload):
        need(self.workload is None and self.journey_class is not None and
             isinstance(workload, self.journey_class) and workload.serial == 0 and
             workload.budget['setup'] == self.readiness_requests == self.counts['setup'] and
             workload.origin == ORIGIN, 'transport_workload_binding')
        self.workload = workload

    def set_phase_deadline(self, deadline, cleaning):
        need(self.active is None and type(cleaning) is bool, 'transport_phase_change_during_request')
        target = finite_deadline(deadline)
        if cleaning:
            ceiling = self.deadline if self.cleaning else self.closure_deadline
            need(target <= ceiling, 'transport_closure_deadline_extended')
            self.cleaning = True
        else:
            need(not self.cleaning and target <= self.deadline, 'transport_business_deadline_extended')
        self.deadline = target

    def summary(self):
        need(self.active is None, 'transport_summary_while_active')
        return {'kind': 'native-scan-http-capacity-control-http-summary', 'version': 1,
                'source': self.source_pin, 'readerSource': self.reader_pin,
                'journeySource': self.installation_pin, 'requestCounts': dict(self.counts),
                'connectionAttemptCounts': dict(self.connection_attempts),
                'admittedAttemptCounts': dict(self.admitted_attempts),
                'intentCounts': dict(self.intent_counts), 'priorSetupRequests': self.readiness_requests,
                'limits': dict(COUNTER_LIMITS, evidenceBytes=EVIDENCE_CAP),
                'evidenceBytes': self.evidence_bytes, 'receipts': list(self.receipts)}

    def _owned(self):
        self.support.check_infrastructure(self.base, self.context)
        self.application.check_owned()

    def _trace(self, journey, value, capture, cleanup):
        if journey is None:
            return
        try:
            journey._record(value)
        except Exception as error:
            journey.trace_persisted = False
            capture['traceErrors'].append(self._error(error))
            journey.events.append(dict(value, evidencePersistenceFailed=True))
            if not cleanup:
                raise Rejected('transport_journey_trace_persistence_failed') from None

    def _request(self, journey, method, path, expected, *, body=None, credential=None, label,
                 json_body=True, headers=None, on_headers=None, on_json=None, deadline=None,
                 cleanup_request=False):
        need(journey is self.workload and self.workload is not None, 'transport_unbound_workload')
        bucket = journey.bucket
        need(bucket in COUNTER_LIMITS and (bucket == 'cleanup') is self.cleaning and
             cleanup_request is self.cleaning, 'transport_workload_phase')
        return self._operate(journey, method, path, expected, body=body, credential=credential,
            label=label, json_body=json_body, headers=headers, on_headers=on_headers, on_json=on_json,
            supplied_deadline=deadline, bucket=bucket, phase=journey.phase, cleanup=self.cleaning)

    def readiness(self, path):
        need(self.workload is None and not self.cleaning and path in ('/healthz', '/readyz') and
             self.readiness_requests < 44, 'transport_readiness_scope')
        expected = {200} if path == '/healthz' else {200, 503}
        value, status, pin = self._operate(None, 'GET', path, expected, body=None, credential=None,
            label='readiness-' + path[1:], json_body=True, headers=None, on_headers=None, on_json=None,
            supplied_deadline=None, bucket='setup', phase='readiness', cleanup=False, readiness=True)
        if status == 200:
            need(value == {'Status': 'ok' if path == '/healthz' else 'ready'}, 'transport_readiness_body')
        else:
            need(type(value) is dict and type(value.get('Error')) is dict and
                 type(value['Error'].get('Code')) is str, 'transport_unready_body')
        return {'status': status, 'body': value, 'receipt': pin}

    def _checkpoint(self, deadline_ns):
        need(time.monotonic_ns() < deadline_ns, 'absolute_http_deadline')

    def _headers(self, journey, credential, extra, payload):
        outgoing = journey._headers(credential) if journey is not None else {
            'Accept': 'application/json', 'Accept-Encoding': 'identity', 'Connection': 'close'}
        extra = {} if extra is None else extra
        allowed_extra = {'Origin', 'X-Emby-Client', 'X-Emby-Client-Version',
                         'X-Emby-Device-Id', 'X-Emby-Device-Name'}
        need(type(extra) is dict and set(extra) <= allowed_extra, 'transport_extra_headers')
        outgoing.update(extra)
        if payload is not None:
            outgoing['Content-Type'] = 'application/json'
            outgoing['Content-Length'] = str(len(payload))
        need(outgoing.get('Accept') == 'application/json' and outgoing.get('Accept-Encoding') == 'identity' and
             outgoing.get('Connection') == 'close' and outgoing.get('Origin', ORIGIN) == ORIGIN,
             'transport_fixed_request_headers')
        for name, value in outgoing.items():
            need(type(name) is str and re.fullmatch('[A-Za-z0-9-]+', name) is not None and
                 type(value) is str and len(value) <= 2048 and
                 all(32 <= ord(character) < 127 for character in value), 'transport_header_value')
        return outgoing

    def _response_headers(self, response, wire, cap, capture):
        capture['status'] = response.status
        capture['responseHeaderCount'] = len(response.getheaders())
        capture['contentType'] = response.getheader('Content-Type')
        capture['contentLength'] = response.getheader('Content-Length')
        capture['transferEncoding'] = response.getheader('Transfer-Encoding')
        capture['responseRequestId'] = response.getheader('X-Request-ID')
        need(wire.header_end is not None, 'raw_header_incomplete')
        raw = bytes(wire.raw[:wire.header_end])
        first = raw.split(b'\r\n', 1)[0]
        match = re.fullmatch(rb'HTTP/1\.1 ([0-9]{3}) [^\r\n]*', first)
        need(match is not None and int(match.group(1)) == response.status and raw.endswith(b'\r\n\r\n'),
             'transport_response_status_line')
        rows = raw[:-4].split(b'\r\n')[1:]
        need(all(row and row[:1] not in (b' ', b'\t') and b':' in row for row in rows),
             'transport_response_header_syntax')
        values = {}
        for name, value in response.getheaders():
            values.setdefault(name.lower(), []).append(value)
        for key in ('content-type', 'content-length', 'transfer-encoding', 'content-encoding', 'x-request-id'):
            need(len(values.get(key, [])) <= 1, 'transport_duplicate_response_header')
        length, transfer = capture['contentLength'], capture['transferEncoding']
        need(not (length is not None and transfer is not None), 'transport_ambiguous_response_framing')
        if length is not None:
            need(re.fullmatch('[0-9]+', length) is not None and int(length) <= cap,
                 'transport_response_content_length')
        need(transfer is None or transfer.lower() == 'chunked', 'transport_response_transfer_encoding')
        need(values.get('content-encoding', ['identity'])[0].lower() == 'identity',
             'transport_response_content_encoding')

    def _operate(self, journey, method, path, expected, *, body, credential, label, json_body,
                 headers, on_headers, on_json, supplied_deadline, bucket, phase, cleanup, readiness=False):
        need(self.active is None and len(list(Path('/proc/self/task').iterdir())) == 1,
             'transport_single_request_single_thread_required')
        admission = route(method, path)
        need(readiness is (admission['name'] in ('health', 'ready')), 'transport_readiness_entry_only')
        body_contract(admission['name'], body)
        statuses = allowed_status(admission['name'], expected, json_body)
        need(type(label) is str and re.fullmatch('[A-Za-z0-9-]{1,128}', label) is not None and
             type(phase) is str and re.fullmatch('[a-z-]{1,32}', phase) is not None,
             'transport_label_contract')
        phase_deadline = self.deadline if supplied_deadline is None else min(
            self.deadline, finite_deadline(supplied_deadline))
        need(time.monotonic() < phase_deadline and self.admitted_attempts[bucket] < COUNTER_LIMITS[bucket],
             'transport_request_budget_or_deadline')
        payload = None if body is None else json.dumps(body, separators=(',', ':'), allow_nan=False).encode()
        need(payload is None or len(payload) <= REQUEST_BODY_CAP, 'transport_request_body_limit')
        outgoing = self._headers(journey, credential, headers, payload)
        cap = admission['bodyCap']
        maximum = 2 * (cap + 1) + 2 * WIRE_OVERHEAD
        need(self.evidence_bytes + maximum <= EVIDENCE_CAP, 'transport_next_evidence_reservation')
        self._owned()
        self.sequence += 1
        sequence = self.sequence
        need(sequence <= sum(COUNTER_LIMITS.values()), 'transport_total_intent_budget')
        self.reserve({'event': 'capacity-http-reserve', 'sequence': sequence, 'bucket': bucket, 'phase': phase,
            'transportEvidenceBytes': self.evidence_bytes, 'maximumAdditionalBytes': maximum,
            'bodyCap': cap, 'wireCap': cap + WIRE_OVERHEAD, 'cleanup': cleanup,
            'phaseDeadlineMonotonicNs': int(phase_deadline * 1_000_000_000)})
        self.admitted_attempts[bucket] += 1
        capture = {'kind': 'native-scan-http-capacity-control-http', 'version': 1, 'scope': str(E),
            'sequence': sequence, 'bucket': bucket, 'phase': phase, 'label': label,
            'source': self.source_pin, 'readerSource': self.reader_pin, 'journeySource': self.installation_pin,
            'method': method, 'path': path, 'route': admission['name'], 'expectedStatuses': sorted(statuses),
            'bodyCap': cap, 'wireCap': cap + WIRE_OVERHEAD, 'requestBodyBytes': 0 if payload is None else len(payload),
            'requestBodySha256': None if payload is None else hashlib.sha256(payload).hexdigest(),
            'intentAnchor': self.reader.clock_anchor(), 'connectionAttemptMonotonicNs': None,
            'firstSendAttemptMonotonicNs': None, 'dispatchMonotonicNs': None,
            'headersMonotonicNs': None, 'bodyCompleteMonotonicNs': None,
            'connectionCloseMonotonicNs': None, 'connectionCreated': False, 'connectionAttempted': False,
            'dispatched': False, 'requestFullySent': False, 'requestBytesSent': 0, 'status': None,
            'responseCreated': False, 'responseClosed': False, 'connectionClosed': False,
            'responseCloseErrors': [], 'connectionCloseErrors': [], 'namespaceExitErrors': [],
            'bodyComplete': False, 'bodyBytes': 0, 'eofObserved': False, 'remainingContentLength': None,
            'decoderBodyComplete': False, 'decoderBodyCompleteMonotonicNs': None,
            'rawFraming': {'checked': False, 'passed': False},
            'partialReadError': False, 'traceErrors': [], 'persistenceErrors': [], 'postOwnershipErrors': [],
            'ownershipBefore': True, 'ownershipAfter': False, 'error': None, 'unknownMutationOutcome': False}
        self.active = capture
        write_started = self.evidence_write_ns
        raw_body, chunks, parsed, returned = b'', [], None, None
        wire, response, sock = None, None, None
        first_error = None
        trace = None
        try:
            if journey is not None:
                need(journey.serial < 360, 'transport_journey_request_limit')
                if method != 'GET':
                    key = (journey.phase, label)
                    need(key not in journey.writes, 'transport_mutation_already_attempted')
                    journey.writes.add(key)
                journey.serial += 1
                trace = {'sequence': journey.serial, 'phase': journey.phase, 'label': label, 'method': method,
                         'path': journey._safe_path(path), 'expectedStatus': expected, 'actualStatus': None,
                         'responseBytes': 0, 'requestId': None}
                capture['journeySequence'] = journey.serial
                self._trace(journey, dict(trace, event='request-intent'), capture, cleanup)
            intent = encoded(capture)
            need(len(intent) <= RECEIPT_CAP, 'transport_intent_receipt_limit')
            capture['intent'] = self._save('%03d-intent.json' % sequence, intent)
            self.intent_counts[bucket] += 1
            capture['intentEvidenceWriteNs'] = self.evidence_write_ns - write_started
            with self.support.app_network(self.context):
                try:
                    self._checkpoint(int(phase_deadline * 1_000_000_000))
                    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
                    capture['connectionCreated'] = True
                    sock.setblocking(False)
                    capture['connectionAttemptAnchor'] = self.reader.clock_anchor()
                    connect_time = time.monotonic_ns()
                    deadline_ns = min(connect_time + HTTP_SECONDS * 1_000_000_000,
                                      int(phase_deadline * 1_000_000_000))
                    capture['connectionAttemptMonotonicNs'] = connect_time
                    capture['deadlineMonotonicNs'] = deadline_ns
                    self._checkpoint(deadline_ns)
                    self.connection_attempts[bucket] += 1
                    capture['connectionAttempted'] = True
                    connected = sock.connect_ex(('127.0.0.1', 18099))
                    need(connected in (0, errno.EINPROGRESS, errno.EWOULDBLOCK, errno.EALREADY),
                         'transport_connect_failed')
                    while connected != 0:
                        self._checkpoint(deadline_ns)
                        timeout = min(0.05, max(0, (deadline_ns - time.monotonic_ns()) / 1_000_000_000))
                        _, ready, _ = select.select([], [sock], [], timeout)
                        if ready:
                            self._checkpoint(deadline_ns)
                            connected = sock.getsockopt(socket.SOL_SOCKET, socket.SO_ERROR)
                            need(connected == 0, 'transport_connect_failed')
                    capture['connectedMonotonicNs'] = time.monotonic_ns()
                    message = (method + ' ' + path + ' HTTP/1.1\r\nHost: 127.0.0.1:18099\r\n' +
                               ''.join(name + ': ' + value + '\r\n' for name, value in outgoing.items()) +
                               '\r\n').encode('ascii') + (payload or b'')
                    offset = 0
                    while offset < len(message):
                        self._checkpoint(deadline_ns)
                        timeout = min(0.05, max(0, (deadline_ns - time.monotonic_ns()) / 1_000_000_000))
                        _, ready, _ = select.select([], [sock], [], timeout)
                        if not ready:
                            continue
                        self._checkpoint(deadline_ns)
                        attempted = time.monotonic_ns()
                        if capture['firstSendAttemptMonotonicNs'] is None:
                            capture['firstSendAttemptMonotonicNs'] = attempted
                            capture['firstSendAttemptAnchor'] = self.reader.clock_anchor()
                        try:
                            count = sock.send(message[offset:])
                        except BlockingIOError:
                            continue
                        need(count > 0, 'transport_send_incomplete')
                        if not capture['dispatched']:
                            capture['dispatched'] = True
                            capture['dispatchMonotonicNs'] = attempted
                            self.counts[bucket] += 1
                            if readiness:
                                self.readiness_requests += 1
                        offset += count
                        capture['requestBytesSent'] = offset
                    capture['requestFullySent'] = True
                    capture['requestSentMonotonicNs'] = time.monotonic_ns()
                    self.reader.WIRE_CAP = cap + WIRE_OVERHEAD
                    wire = self.reader.WireFile(sock, lambda: self._checkpoint(deadline_ns), deadline_ns)
                    response = http.client.HTTPResponse(self.reader.ResponseSocket(wire), method=method)
                    capture['responseCreated'] = True
                    response.begin()
                    capture['headersMonotonicNs'] = time.monotonic_ns()
                    capture['headersAnchor'] = self.reader.clock_anchor()
                    capture.update(status=response.status, contentType=response.getheader('Content-Type'),
                                   contentLength=response.getheader('Content-Length'),
                                   transferEncoding=response.getheader('Transfer-Encoding'),
                                   responseRequestId=response.getheader('X-Request-ID'),
                                   responseHeaderCount=len(response.getheaders()))
                    if on_headers is not None:
                        on_headers(response.getheaders())
                    self._response_headers(response, wire, cap, capture)
                    self._checkpoint(deadline_ns)
                    while True:
                        self._checkpoint(deadline_ns)
                        try:
                            part = response.read1(min(65536, cap + 1 - capture['bodyBytes']))
                        except http.client.IncompleteRead as error:
                            chunks.append(error.partial)
                            capture['bodyBytes'] += len(error.partial)
                            capture['partialReadError'] = True
                            raise Rejected('response_body_incomplete') from None
                        if part:
                            chunks.append(part)
                            capture['bodyBytes'] += len(part)
                            need(capture['bodyBytes'] <= cap, 'response_body_limit')
                        if not part or response.isclosed():
                            break
                    capture['remainingContentLength'] = response.length
                    capture['eofObserved'] = wire.eof
                    self._checkpoint(deadline_ns)
                    need(response.length in (None, 0), 'response_body_incomplete')
                    capture['decoderBodyComplete'] = True
                    capture['decoderBodyCompleteMonotonicNs'] = time.monotonic_ns()
                    capture['decoderBodyCompleteAnchor'] = self.reader.clock_anchor()
                    raw_body = b''.join(chunks)
                    if json_body:
                        try:
                            parsed = self.reader.parse(raw_body)
                        except Exception:
                            raise Rejected('invalid_json_response') from None
                        if on_json is not None:
                            on_json(parsed)
                    capture['rawFraming']['checked'] = True
                    capture['rawFraming'] = validate_raw_framing(bytes(wire.raw[wire.header_end:]), raw_body,
                        content_length=capture['contentLength'], transfer_encoding=capture['transferEncoding'],
                        status=response.status, eof=wire.eof, body_cap=cap)
                    capture['framingValidatedMonotonicNs'] = time.monotonic_ns()
                    self._checkpoint(deadline_ns)
                    capture['bodyComplete'] = True
                    capture['bodyCompleteMonotonicNs'] = capture['decoderBodyCompleteMonotonicNs']
                    capture['bodyCompleteAnchor'] = capture['decoderBodyCompleteAnchor']
                    need(type(capture['responseRequestId']) is str and
                         re.fullmatch(ID, capture['responseRequestId']) is not None, 'missing_request_id')
                    need(response.status in statuses, 'unexpected_http_status')
                    media_type = (capture['contentType'] or '').split(';', 1)[0].strip().lower()
                    if json_body:
                        need(media_type == 'application/json', 'unexpected_json_content_type')
                        need(self.reader.parse(raw_body) == parsed, 'transport_raw_and_callback_json_differ')
                    elif response.status == 401:
                        need(admission['name'] == 'catalog-count' and media_type == 'text/plain',
                             'transport_plain_revocation_contract')
                    if response.status == 204:
                        need(not raw_body, 'logout_response_not_empty')
                    returned = parsed if json_body else raw_body
                except BaseException as error:
                    first_error = error
                    raise
                finally:
                    if 'deadlineMonotonicNs' in capture:
                        capture['deadlineBeforeCloseExceeded'] = time.monotonic_ns() >= capture['deadlineMonotonicNs']
                    if response is not None:
                        capture['remainingContentLength'] = response.length
                        try:
                            response.close()
                            capture['responseClosed'] = True
                        except BaseException as error:
                            capture['responseCloseErrors'].append(self._error(error))
                            if first_error is None:
                                first_error = Rejected('response_close_failed')
                    if sock is not None:
                        try:
                            sock.close()
                        except BaseException as error:
                            capture['connectionCloseErrors'].append(self._error(error))
                            if first_error is None:
                                first_error = Rejected('connection_close_failed')
                        capture['connectionClosed'] = sock.fileno() == -1
                    else:
                        capture['connectionClosed'] = True
                    capture['connectionCloseMonotonicNs'] = time.monotonic_ns()
                    if 'deadlineMonotonicNs' in capture:
                        capture['deadlineAfterCloseExceeded'] = (capture['connectionCloseMonotonicNs'] >=
                                                                 capture['deadlineMonotonicNs'])
                        if capture['deadlineAfterCloseExceeded'] and first_error is None:
                            first_error = Rejected('absolute_http_deadline')
        except BaseException as error:
            if first_error is not None and error is not first_error:
                capture['namespaceExitErrors'].append(self._error(error))
            if first_error is None:
                first_error = error
        finally:
            if sock is None:
                capture['connectionClosed'] = True
            elif sock.fileno() != -1:
                try:
                    sock.close()
                except BaseException as error:
                    capture['connectionCloseErrors'].append(self._error(error))
                    if first_error is None:
                        first_error = Rejected('connection_close_failed')
                capture['connectionClosed'] = sock.fileno() == -1
                capture['connectionCloseMonotonicNs'] = time.monotonic_ns()
            try:
                self._owned()
                capture['ownershipAfter'] = True
            except BaseException as error:
                capture['postOwnershipErrors'].append(self._error(error))
                if first_error is None:
                    first_error = error
            raw = bytes(wire.raw) if wire is not None else b''
            boundary = wire.header_end if wire is not None else None
            capture['rawHeaderComplete'] = boundary is not None
            capture['eofObserved'] = wire.eof if wire is not None else False
            raw_body = b''.join(chunks)
            capture['decodedBodySha256'] = hashlib.sha256(raw_body).hexdigest()
            for field, suffix, content in (
                    ('rawHeader', 'response-header', raw[:boundary] if boundary is not None else raw),
                    ('rawWireBody', 'response-wire-body', raw[boundary:] if boundary is not None else b''),
                    ('body', 'response-body', raw_body)):
                try:
                    capture[field] = self._save('%03d-%s.raw' % (sequence, suffix), content)
                except BaseException as error:
                    capture['persistenceErrors'].append(self._error(error))
                    if first_error is None:
                        first_error = Rejected('transport_raw_evidence_persistence_failed')
                    if journey is not None:
                        journey.trace_persisted = False
            if not capture['connectionClosed'] or capture['responseCreated'] and not capture['responseClosed']:
                if first_error is None:
                    first_error = Rejected('transport_owned_connection_not_closed')
            capture['unknownMutationOutcome'] = method != 'GET' and capture['dispatched'] and first_error is not None
            if trace is not None:
                trace.update(actualStatus=capture['status'], responseBytes=capture['bodyBytes'],
                             requestId=capture.get('responseRequestId'))
                event = dict(trace, event='request-complete' if first_error is None else 'request-failed',
                             responseComplete=capture['bodyComplete'], connectionClosed=capture['connectionClosed'],
                             unknownOutcome=capture['unknownMutationOutcome'])
                if first_error is not None:
                    event['error'] = self._error(first_error)
                try:
                    self._trace(journey, event, capture, cleanup)
                except BaseException as error:
                    if first_error is None:
                        first_error = error
            capture['error'] = None if first_error is None else self._error(first_error)
            capture['journeyTracePersisted'] = None if journey is None else journey.trace_persisted
            capture['evidenceWriteNsBeforeReceipt'] = self.evidence_write_ns - write_started
            capture['finishedAnchor'] = self.reader.clock_anchor()
            pin = None
            try:
                receipt = encoded(capture)
                need(len(receipt) <= RECEIPT_CAP, 'transport_final_receipt_limit')
                pin = self._save('%03d-receipt.json' % sequence, receipt)
                self.receipts.append(pin)
                if self.record_callback is not None:
                    self.record_callback({'event': 'capacity-http-receipt', 'sequence': sequence,
                        'requestCount': sum(self.counts.values()), 'receipt': pin})
            except BaseException as error:
                if journey is not None:
                    journey.trace_persisted = False
                if first_error is None:
                    first_error = error
            self.active = None
        if first_error is not None:
            raise first_error
        need(pin is not None and capture['bodyComplete'] and capture['responseClosed'] and
             capture['connectionClosed'] and not capture['responseCloseErrors'] and
             not capture['connectionCloseErrors'] and not capture['persistenceErrors'],
             'transport_capture_incomplete')
        if readiness:
            return returned, capture['status'], pin
        return returned
