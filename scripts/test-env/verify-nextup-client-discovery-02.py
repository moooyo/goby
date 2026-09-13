#!/usr/bin/env python3
"""Reconstruct discovery02 Suggestions evidence without repeating discovery01.

This remote-only verifier sends no HTTP and never starts a browser. Supply the
final frozen input digest and the actual driver PID/invocation after shutdown.
The unchanged session-proof helper retains status/authority metadata, not raw
401 response bytes. Physical NextUp body bytes are reconstructed and compared
with the independently captured page response's decoded body digest/length.
Outer protected-root/Goby preservation remains a separate controller receipt.
"""

from __future__ import annotations

import argparse
import base64
from copy import deepcopy
from datetime import datetime, timezone
import hashlib
import json
import math
import os
from pathlib import Path
import re
import shlex
import stat
import subprocess
import sys
from urllib.parse import parse_qsl, quote_plus, urlsplit
import zlib

W = Path('/opt/goby-test/exec-work-m3e')
ROOT = W / 'reference-nextup-client-discovery-02'
E = W / 'reference-nextup-client-discovery-execution-02'
TOOL = W / 'nextup-client-discovery-tool-02/revision-01'
UNIT = 'goby-nextup-client-discovery-02.service'
UNIT_FILE = Path('/run/systemd/system') / UNIT
OUTPUT = E / 'independent-client-terminal.json'
FILES = ('client-browser-nextup-discovery-02.mjs', 'client-browser-nextup-discovery-runtime.mjs', 'client-browser-session-proof.mjs')
SOURCE_PINS = dict(zip(FILES, (
    'aa69812f5cea621fd8544049eeb50b9374f3ff6e809c1bcd8338d5aaf6dfe23d',
    'bfce485955fece8beb9a0ebde500b649a349a06b26d1288e6cb73948d48c2a8d',
    'fea90503a3e279d1ec63723f8785b3421c72d756af4db95f1762fbd67ece2472')))
EPISODES = ('A1', 'A2', 'A3', 'B1', 'B2', 'B3')
SUMMARIES = ('A', 'AS1', 'AS2', 'B', 'BS1', 'BS2')
FORBIDDEN = (W / 'reference-data', Path('/dev/shm/goby-emby-reference/package'))
MAX_FILE = 32 * 1024 * 1024
PRIOR_ROOT = W / 'reference-nextup-client-discovery-01'
PRIOR_CLOSURE = W / 'reference-nextup-client-discovery-execution-01/independent-client-terminal-04.json'
PRIOR_CLOSURE_SHA = 'f123a6421aa3ff86eda190dc81bd5338b7fcdeb8ac31e09b5555f8c74cd2f001'
VISIBLE_LABELS = ('home-after-login', 'before-la', 'after-la', 'suggestions-la',
                  'home-between-libraries', 'before-lb', 'after-lb', 'suggestions-lb')


class EvidenceError(ValueError):
    """A retained responsibility lacks sufficient independent evidence."""


def need(value, reason):
    if not value:
        raise EvidenceError(reason)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=True, allow_nan=False)


def same(left, right):
    return canonical(left) == canonical(right)


def strict_json(raw):
    def pairs(rows):
        result = {}
        for key, value in rows:
            need(key not in result, 'duplicate_json_field')
            result[key] = value
        return result
    def invalid(_value):
        raise EvidenceError('nonfinite_json_value')
    return json.loads(raw, object_pairs_hook=pairs, parse_constant=invalid)


def fingerprint(value):
    return isinstance(value, str) and re.fullmatch(r'[0-9a-f]{64}', value) is not None


def identity(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid,
            info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def time_value(value):
    need(type(value) in (int, float) and math.isfinite(value) and 0 <= value <= 1200000, 'elapsed_time_invalid')
    return value


def projected_item(value):
    fields = ('Id', 'Type', 'ParentId', 'SeriesId', 'IndexNumber', 'ParentIndexNumber', 'RunTimeTicks', 'UserData')
    return {key: value[key] for key in fields if key in value}


class Verifier:
    def __init__(self, args):
        self.args = args
        self.evidence = {}
        self.secrets = []
        self.stage = 'admission'
        self.counters = {'network': 0, 'unexpected_process': 0, 'unexpected_write': 0, 'forbidden_read': 0}
        self.summary = {}

    def audit(self, event, args):
        if event.startswith('socket.') or event.startswith('http.client.'):
            self.counters['network'] += 1
            raise EvidenceError('network_forbidden')
        if event == 'subprocess.Popen':
            command = args[1]
            if not (isinstance(command, list) and len(command) == 4 and command[:3] == ['/usr/bin/systemctl', 'show', UNIT]
                    and command[3].startswith('--property=')):
                self.counters['unexpected_process'] += 1
                raise EvidenceError('only_exact_unit_metadata_process_allowed')
        if event == 'open' and isinstance(args[0], (str, bytes, os.PathLike)):
            path = Path(os.path.abspath(os.fsdecode(args[0])))
            if any(path == root or root in path.parents for root in FORBIDDEN) or path.parent.parent == Path('/proc') and path.name == 'exe':
                self.counters['forbidden_read'] += 1
                raise EvidenceError('reference_or_executable_byte_read_forbidden')
            if args[2] & (os.O_WRONLY | os.O_RDWR | os.O_CREAT | os.O_TRUNC | os.O_APPEND) and path != OUTPUT:
                self.counters['unexpected_write'] += 1
                raise EvidenceError('only_exclusive_terminal_write_allowed')

    def read(self, path, checksum=None):
        path = Path(path)
        need(path.is_absolute() and '..' not in path.parts and path.resolve(strict=True) == path, 'evidence_path_not_exact')
        need((W in path.parents or path == UNIT_FILE) and not any(path == root or root in path.parents for root in FORBIDDEN),
             'evidence_path_outside_owned_scope')
        before = path.lstat()
        need(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_uid == 0 and
             not before.st_mode & 0o022 and 0 <= before.st_size <= MAX_FILE, 'evidence_file_not_protected')
        with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC), 'rb') as stream:
            need(identity(os.fstat(stream.fileno())) == identity(before), 'evidence_changed_before_read')
            raw = stream.read(MAX_FILE + 1)
            need(identity(os.fstat(stream.fileno())) == identity(before), 'evidence_changed_during_read')
        need(identity(path.lstat()) == identity(before) and len(raw) == before.st_size, 'evidence_changed_after_read')
        need(checksum is None or fingerprint(checksum) and sha(raw) == checksum, 'evidence_digest_differs')
        row = {'path': str(path), 'sha256': sha(raw), 'bytes': len(raw), 'identity': list(identity(before))}
        need(str(path) not in self.evidence or same(self.evidence[str(path)], row), 'evidence_changed_between_reads')
        self.evidence[str(path)] = row
        return raw

    def doc(self, path, checksum=None):
        return strict_json(self.read(path, checksum))

    def descriptor(self, value, expected=None):
        need(isinstance(value, dict) and set(value) == {'path', 'sha256'} and fingerprint(value['sha256']), 'invalid_descriptor')
        need(expected is None or value['path'] == str(expected), 'descriptor_names_other_scope')
        return self.doc(value['path'], value['sha256'])

    def private(self, name):
        need(re.fullmatch(r'[A-Za-z0-9_.-]+', name) is not None, 'invalid_private_filename')
        return self.doc(ROOT / 'private' / name)

    def raw_response(self, row, field='body_base64', limit=2097152):
        raw = base64.b64decode(row[field], validate=True)
        need(len(raw) <= limit and base64.b64encode(raw).decode() == row[field], 'raw_body_bound_or_encoding')
        return raw

    def recorder(self, phase, offset, matrix, credentials, user, server):
        self.stage = phase + '_recorder'
        routes = [(name, '/emby/Users/' + user + '/Items/' + matrix['items'][name]['id']) for name in EPISODES + SUMMARIES]
        routes += [('profile', '/emby/Users/' + user), ('preferences', '/emby/UserSettings/' + user)]
        schedule = [('login', 'POST', '/emby/Users/AuthenticateByName', 200)]
        schedule += [(name, 'GET', route, 200) for name, route in routes]
        schedule += [('logout', 'POST', '/emby/Sessions/Logout', 204), ('invalid', 'GET', '/emby/Users/' + user, 401)]
        need(len(schedule) == 17, 'recorder_schedule_not_seventeen')
        token, state, previous = None, {}, 0
        device = 'goby-nextup-client-discovery-02-' + phase
        for position, (label, method, route, status) in enumerate(schedule, offset + 1):
            intent_name = 'request-' + str(position) + '-intent.json'
            intent = self.private(intent_name)
            response = self.private('request-' + str(position) + '-response.json')
            self.descriptor(response['intent'], ROOT / 'private' / intent_name)
            need(intent['label'] == phase + '-' + label and intent['method'] == method and intent['route'] == route and
                 intent['device'] == device and intent['input_sha256'] == self.args.input_sha256, 'recorder_request_identity')
            expected_headers = {'Accept': 'application/json', 'X-Emby-Authorization':
                'Emby Client="Goby NextUp Client State", Device="Remote observer", DeviceId="' + device + '", Version="1"'}
            payload = None
            if token:
                expected_headers['X-Emby-Token'] = token
            if label == 'login':
                encode = lambda text: quote_plus(text, safe='*-._').replace('~', '%7E')
                payload = ('Username=' + encode(credentials['username']) + '&Pw=' + encode(credentials['password'])).encode()
                expected_headers.update({'Content-Type': 'application/x-www-form-urlencoded', 'Content-Length': str(len(payload))})
            need(same(intent['headers'], expected_headers) and intent['payload_base64'] ==
                 (base64.b64encode(payload).decode() if payload is not None else None), 'recorder_exact_request_headers_payload')
            need(response['status'] == status and response['json_parse_failed'] is False, 'recorder_response_status')
            raw = self.raw_response(response)
            need(response['body_sha256'] == sha(raw), 'recorder_raw_body_digest')
            headers = response['headers']
            if 'content-length' in headers:
                need(headers['content-length'].isdigit() and int(headers['content-length']) == len(raw), 'recorder_response_framing')
            start, finish = time_value(intent['elapsed_ms']), time_value(response['elapsed_ms'])
            need(previous <= start <= finish, 'recorder_responsibility_time_order')
            previous = finish
            if label == 'login':
                need(intent['body'] == {'Username': credentials['username'], 'Pw': credentials['password']} and
                     intent['token_sha256'] is None, 'recorder_login_payload')
                value = strict_json(raw)
                token = value['AccessToken']
                need(isinstance(token, str) and 0 < len(token) <= 4096 and token not in self.secrets, 'recorder_token_not_fresh')
                self.secrets.append(token)
                need(value['User']['Id'] == user and value['User']['Policy']['IsAdministrator'] is False and value['ServerId'] == server and
                     value['SessionInfo']['UserId'] == user and value['SessionInfo']['DeviceId'] == device, 'recorder_login_principal')
            else:
                need(intent['body'] is None and intent['token_sha256'] == sha(token.encode()), 'recorder_exact_token_binding')
                if status == 200:
                    value = strict_json(raw)
                    if label in EPISODES + SUMMARIES:
                        expected = matrix['items'][label]
                        for key, field in (('Id', 'id'), ('Type', 'type'), ('ParentId', 'parentId'), ('SeriesId', 'seriesId'),
                                           ('IndexNumber', 'indexNumber'), ('ParentIndexNumber', 'parentIndexNumber'), ('RunTimeTicks', 'runtimeTicks')):
                            if field in expected:
                                need(same(value.get(key), expected[field]), 'recorder_item_identity_' + label)
                        need(isinstance(value['UserData'], dict), 'recorder_full_userdata_missing')
                        if label in EPISODES:
                            data = value['UserData']
                            need(data['Played'] is False and type(data['PlayCount']) is int and data['PlayCount'] == 0 and
                                 type(data['PlaybackPositionTicks']) is int and data['PlaybackPositionTicks'] == 0 and
                                 data.get('LastPlayedDate') is None, 'recorder_zero_history_not_retained')
                        state[label] = projected_item(value)
                    else:
                        state[label] = value
            self.summary.setdefault('recorderLedger', []).append({'ordinal': position, 'label': phase + '-' + label,
                'status': status, 'intentSha256': self.evidence[str(ROOT / 'private' / intent_name)]['sha256'],
                'responseSha256': self.evidence[str(ROOT / 'private' / ('request-' + str(position) + '-response.json'))]['sha256']})
        need(state['profile']['Id'] == user and state['profile']['Name'] == credentials['username'] and
             state['profile']['Policy']['IsAdministrator'] is False and state['profile']['Policy']['EnableAllFolders'] is False and
             sorted(state['profile']['Policy']['EnabledFolders']) == sorted(matrix['actors']['P']['allowedFolderIds']), 'recorder_full_profile_authority')
        need(same(state, self.private(phase + '-owned-state.json')), 'recorder_derived_state_not_reconstructed')
        self.summary[phase + 'Recorder'] = {'requestCount': 17, 'tokenSha256': sha(token.encode()), 'sameTokenLogout204AndRejection401': True}
        return state

    def decoded_body(self, receipt):
        raw = self.raw_response(receipt)
        encoding = receipt['encoding'].strip().lower()
        if encoding == 'identity':
            decoded = raw
        elif encoding in ('gzip', 'deflate'):
            decoder = zlib.decompressobj(16 + zlib.MAX_WBITS if encoding == 'gzip' else zlib.MAX_WBITS)
            decoded = decoder.decompress(raw, 1048577)
            need(len(decoded) <= 1048576 and decoder.eof and not decoder.unconsumed_tail and not decoder.unused_data,
                 'compressed_private_body_incomplete_or_unbounded')
        elif encoding == 'br':
            try:
                import brotli
            except ImportError as error:
                raise EvidenceError('recorded_brotli_body_requires_available_decoder') from error
            decoder, chunks, size = brotli.Decompressor(), [], 0
            for offset in range(0, len(raw), 256):
                chunk = decoder.process(raw[offset:offset + 256])
                size += len(chunk)
                need(size <= 1048576, 'brotli_private_body_exceeds_limit')
                chunks.append(chunk)
            need(decoder.is_finished(), 'brotli_private_body_incomplete')
            decoded = b''.join(chunks)
        else:
            raise EvidenceError('recorded_content_encoding_unsupported')
        need(len(decoded) <= 1048576, 'decoded_private_body_exceeds_limit')
        return raw, decoded, strict_json(decoded)

    def paired(self, row, frames):
        matches = [value for value in frames if value['request_sha256'] == row['request_sha256'] and
                   value['token_sha256'] == row['token_sha256'] and value['phase'] == row['phase'] and
                   value['document_id'] == row['document_id'] and value['method'] == row['method'] and value['route'] == row['route'] and
                   same(value.get('exact_public_query'), row.get('exact_public_query')) and value['status'] == row['status'] and
                   value['completed'] is True and value['sourceworker'] is False and value['from_service_worker'] is False and
                   value['main_frame'] is True]
        need(len(matches) == 1, 'physical_page_response_binding_missing_or_ambiguous')
        return matches[0]

    def browser(self, input_value, matrix, credentials, user, server, report):
        self.stage = 'browser_responsibilities'
        network = self.private('browser-network-private.json')
        physical, frames = network['physical'], network['frames']
        snapshot = network['snapshot']
        need(isinstance(physical, list) and isinstance(frames, list) and 0 < len(physical) <= 1800 and
             len({row['id'] for row in physical}) == len(physical) and len({row['id'] for row in frames}) == len(frames), 'browser_channel_inventory')
        need(same(sorted(physical, key=lambda row: row['id']), sorted(snapshot['http']['physical'], key=lambda row: row['id'])) and
             same(sorted(frames, key=lambda row: row['id']), sorted(snapshot['http']['frames'], key=lambda row: row['id'])),
             'browser_finished_and_complete_channel_sets_differ')
        runtime = snapshot['report']
        need(runtime['closed'] is True and runtime['cleanup_failures'] == [] and runtime['failures'] == [] and
             runtime['http']['active'] == 0 and runtime['websocket']['active'] == 0 and runtime['observer_errors'] == 0,
             'browser_not_fully_closed_or_observation_failed')
        need(snapshot['phase'] == 'closed' and runtime['clock_changed'] is False, 'browser_timeline_or_closed_phase_invalid')
        login_rows = [row for row in physical if row['kind'] == 'login']
        logout_rows = [row for row in physical if row['kind'] == 'logout']
        need(len(login_rows) == len(logout_rows) == 1, 'browser_login_logout_responsibility_count')
        login, logout = login_rows[0], logout_rows[0]
        need(login['completed'] is True and login['status'] == 200 and logout['completed'] is True and logout['status'] == 204,
             'browser_physical_authentication_not_completed')
        self.paired(login, frames)
        self.paired(logout, frames)
        saved = self.private('browser-login-private.json')
        token, proof = saved['token'], saved['proof']
        need(isinstance(token, str) and 0 < len(token) <= 4096 and token not in self.secrets, 'browser_token_not_independent')
        self.secrets.append(token)
        token_hash = sha(token.encode())
        login_receipt = self.private('browser-login-response-' + login['id'] + '.json')
        raw, _, value = self.decoded_body(login_receipt)
        need(login_receipt['request']['id'] == login['id'] and value['AccessToken'] == token and
             value['User']['Id'] == user and value['ServerId'] == server and value['User']['Policy']['IsAdministrator'] is False and
             value['SessionInfo']['UserId'] == user and value['SessionInfo']['DeviceId'] not in input_value['existingDeviceIds'], 'browser_raw_login_principal')
        need(proof['token_sha256'] == token_hash and proof['user_id'] == user and proof['server_id'] == server and
             proof['session_id'] == value['SessionInfo']['Id'] and proof['device_id'] == value['SessionInfo']['DeviceId'] and
             runtime['login']['principal']['response_body_sha256'] == sha(raw) and logout['token_sha256'] == token_hash,
             'browser_raw_login_and_logout_token_chain')
        for row in physical:
            need(row['completed'] is True and row['status'] is not None and row['failed'] is False and row['outcome'] == 'completed',
                 'browser_physical_request_incomplete')
            need(row['kind'] in ('read', 'login', 'logout', 'capabilities') and
                 (row['method'] in ('GET', 'HEAD', 'OPTIONS') if row['kind'] == 'read' else row['method'] == 'POST'), 'unauthorized_browser_method')
            # The original Home page reads this exact JSON metadata endpoint.
            # This exception does not admit any other Live TV route or media.
            if re.match(r'^/LiveTv(?:/|$)', row['route'] or '', re.I):
                need(row['route'] == '/LiveTv/Channels' and row['kind'] == 'read' and row['method'] == 'GET' and
                     row['status'] == 200 and row['content_type'] == 'application/json' and
                     row['request_bytes'] == 0 and row['request_body_sha256'] == sha(b''),
                     'unapproved_live_tv_request_observed')
                self.summary.setdefault('liveTvChannelMetadataReads', []).append({'id': row['id'],
                    'requestSha256': row['request_sha256'], 'status': row['status'], 'responseBytes': row['response_bytes']})
            need(re.search(r'/(?:Videos|Audio|PlaybackInfo|Playing|PlayingItems|Transcoding)(?:/|$)|'
                           r'^/Items/[^/]+/(?:Download|File)(?:/|$)|\.(?:m3u8|mp4|mkv|ts|mp3|aac)(?:$|[?])',
                           row['route'] or '', re.I) is None, 'media_or_playback_request_observed')
            if row['kind'] != 'read':
                mutation = self.private('browser-mutation-' + row['id'] + '-intent.json')
                request_raw = self.raw_response(mutation, limit=1048576)
                need(mutation['request']['id'] == row['id'] and sha(request_raw) == row['request_body_sha256'] and
                     len(request_raw) == row['request_bytes'] and row['private_mutation_journal']['completed'] is True,
                     'browser_mutation_not_write_ahead_bound')
                if row['kind'] == 'login':
                    pairs = parse_qsl(request_raw.decode(), keep_blank_values=True, strict_parsing=True)
                    need(len(pairs) == 2 and len(dict(pairs)) == 2 and dict(pairs) ==
                         {'Username': credentials['username'], 'Pw': credentials['password']}, 'browser_ui_login_payload')
        for channel in ('physical', 'browser'):
            for message in snapshot['events'][channel]:
                if message['direction'] == 'client':
                    value = message.get('json')
                    need(isinstance(value, dict) and (value.get('MessageType'), value.get('Data')) in
                         (('SessionsStart', '1000,1000'), ('SessionsStop', '')), 'client_websocket_control_not_read_only')
        session = runtime['session_proof']
        need(len(session['entries']) == 1 and session['observer_errors'] == session['logout_overflow'] == 0, 'ui_logout_proof_entry_count')
        entry = session['entries'][0]
        ui, check = entry['ui_request'], entry['verification']
        need(entry['token_fingerprint'] == token_hash and ui['method'] == 'POST' and ui['route'].lower().rstrip('/') in
             ('/emby/sessions/logout', '/sessions/logout') and ui['response_status'] == 204 and ui['client_request_finished'] is True and
             ui['client_request_failed'] is False and check['source'] == 'independent-node-http-post-logout-verification' and
             check['is_ui_request'] is False and check['method'] == 'GET' and check['route'] == '/emby/System/Info' and check['status'] == 401 and
             check['result'] == 'token_rejected' and check['response_bytes'] <= 65536 and
             ui['response_elapsed_ms'] <= check['started_elapsed_ms'] <= check['finished_elapsed_ms'], 'exact_ui_token_rejection_metadata')
        global_rows = [row for row in physical if row['method'] == 'GET' and (row['route'] or '').lower().rstrip('/') == '/shows/nextup' and
                       row['status'] == 200 and row['token_sha256'] == token_hash and
                       not any(name.lower() == 'seriesid' for name, _value in row['exact_public_query'])]
        reconstructed = []
        for row in global_rows:
            frame = self.paired(row, frames)
            need(sum(other['request_sha256'] == row['request_sha256'] and other['document_id'] == row['document_id'] and
                     other['phase'] == row['phase'] for other in global_rows) == 1, 'global_physical_request_ambiguous')
            raw_receipt = self.private('browser-response-' + row['id'] + '.json')
            raw, decoded, body = self.decoded_body(raw_receipt)
            need(raw_receipt['request']['id'] == row['id'] and raw_receipt['request']['request_sha256'] == row['request_sha256'] and
                 raw_receipt['request']['token_sha256'] == token_hash and raw_receipt['request']['phase'] == row['phase'] and
                 raw_receipt['request']['document_id'] == row['document_id'] and row['response']['body_sha256'] == sha(raw) and
                 row['response']['bytes'] == len(raw) and row['response']['decoded_bytes'] == len(decoded) and
                 row['response']['decoded_body_sha256'] == frame['frame_body']['decoded_body_sha256'] == sha(decoded) and
                 frame['frame_body']['decoded_body_bytes'] == len(decoded) and
                 row['request_body_sha256'] == sha(b''), 'global_private_body_binding')
            need(isinstance(body, dict) and isinstance(body.get('Items'), list) and type(body.get('TotalRecordCount')) is int and
                 body['TotalRecordCount'] >= 0 and all(isinstance(item.get('Id'), str) for item in body['Items']), 'global_response_shape')
            projection = {'Items': [{key: item[key] for key in ('Id', 'Name', 'Type', 'IsFolder', 'ParentId', 'UserData') if key in item}
                                     for item in body['Items']]}
            projection.update({key: body[key] for key in ('TotalRecordCount', 'StartIndex') if key in body})
            need(same(row['response']['json'], projection), 'global_response_projection_not_reconstructed')
            reconstructed.append({'physicalId': row['id'], 'frameId': frame['id'], 'requestSha256': row['request_sha256'],
                'tokenSha256': token_hash, 'query': row['exact_public_query'], 'phase': row['phase'], 'documentId': row['document_id'],
                'requestStartActorMs': row['start_elapsed_ms'], 'physicalFinishedActorMs': row['finished_elapsed_ms'],
                'frameFinishedActorMs': frame['finished_elapsed_ms'],
                'decodedBodySha256': sha(decoded), 'decodedBodyBytes': len(decoded),
                'rawBodySha256': sha(raw), 'orderedIds': [item['Id'] for item in body['Items']], 'totalRecordCount': body['TotalRecordCount']})
            claimed = [item for item in report['nextup'] if item['physical_id'] == row['id']]
            need(len(claimed) == 1 and claimed[0]['actual_client_response_bound'] is True and claimed[0]['frame_id'] == frame['id'] and
                 claimed[0]['frame_decoded_body_sha256'] == sha(decoded) and claimed[0]['ordered_ids'] == reconstructed[-1]['orderedIds'] and
                 same(claimed[0]['exact_public_query'], row['exact_public_query']) and same(claimed[0]['response'], row['response']),
                 'claimed_global_response_not_reconstructed')
        self.summary.update(uiTokenSha256=token_hash, uiLoginAndLogoutDualChannelBound=True, exactUIToken401MetadataVerified=True,
                            ui401RawWireReconstructed=False, pageDecodedBodyDigestIndependentlyCaptured=bool(reconstructed),
                            pageRawBodyBytesRetained=False,
                            globalResponses=reconstructed, browserPlaybackAndMediaRequestsObserved=0)
        self.summary['actualGlobalResponseBound'] = bool(reconstructed)
        self.summary['discoveryOutcome'] = ('actual_client_global_request_unobserved' if not reconstructed else
            'client_positive_requires_separate_frozen_public_control' if any(row['orderedIds'] for row in reconstructed) else
            'reference_global_positive_unresolved')
        return network

    def continuation(self, input_value, report):
        self.stage = 'continuation_budget'
        continuation = input_value['continuation']
        need(set(continuation) == {'priorReport', 'priorIndependentTerminal', 'priorElapsedMilliseconds', 'totalMaximumSeconds'} and
             continuation['priorElapsedMilliseconds'] == 10586 and continuation['totalMaximumSeconds'] == 1200 and
             continuation['priorIndependentTerminal'] == {'path': str(PRIOR_CLOSURE), 'sha256': PRIOR_CLOSURE_SHA},
             'continuation_does_not_bind_completed_discovery01')
        prior = self.descriptor(continuation['priorReport'], PRIOR_ROOT / 'private/report.json')
        closure = self.descriptor(continuation['priorIndependentTerminal'], PRIOR_CLOSURE)
        need(closure['status'] == 'client_closed_global_request_unobserved' and closure['failure'] is None and
             closure['clientDiscoveryEvidenceVerified'] is True and closure['actualGlobalResponseBound'] is False and
             closure['discoveryOutcome'] == 'actual_client_global_request_unobserved' and
             any(row['path'] == str(PRIOR_ROOT / 'private/report.json') and row['sha256'] == continuation['priorReport']['sha256']
                 for row in closure['evidence']), 'prior_successful_closure_report_binding')
        need(prior['run_id'] == 'nextup-reference-client-discovery-01' and prior['outcome'] == 'actual_client_global_request_unobserved' and
             prior['failure'] is None and prior['elapsed_ms'] == 10586 and prior['recorder_requests'] == 34 and
             prior['playback_attempts'] == 0 and prior['state']['comparison']['preserved'] is True and
             prior['cleanup']['browser']['closed'] is True and prior['cleanup']['browser']['exact_token_status'] == 401,
             'prior_discovery_not_cleanly_closed')
        budget = {'maximumSeconds': 1189, 'normalSeconds': 829, 'maximumPlaybackAttempts': 2,
                  'permittedPlaybackAttempts': 0, 'recorderRequests': 34, 'recorderBodyBytes': 2097152, 'browserNavigationActions': 5}
        need(same(input_value['budgets'], budget), 'discovery02_budget_contract')
        current_ms = time_value(report['elapsed_ms'])
        total_ms = 10586 + current_ms
        need(type(report['elapsed_ms']) is int and current_ms <= 1189000 and total_ms <= 1200000,
             'combined_discovery_time_budget_exceeded')
        need(all(time_value(phase['elapsed_ms']) <= current_ms for phase in report['phases']) and
             time_value(self.private('request-34-response.json')['elapsed_ms']) <= current_ms,
             'reported_duration_precedes_retained_completion')
        expected = {'prior_report_sha256': continuation['priorReport']['sha256'],
                    'prior_independent_terminal_sha256': PRIOR_CLOSURE_SHA, 'prior_elapsed_ms': 10586,
                    'total_maximum_seconds': 1200, 'this_scope_maximum_seconds': 1189,
                    'this_scope_normal_seconds': 829, 'total_elapsed_ms': total_ms}
        need(same(report['continuation'], expected), 'reported_continuation_budget_not_reconstructed')
        self.summary['continuation'] = {'priorReport': continuation['priorReport'], 'priorIndependentTerminal': continuation['priorIndependentTerminal'],
            'priorElapsedMilliseconds': 10586, 'currentElapsedMilliseconds': current_ms, 'totalElapsedMilliseconds': total_ms,
            'totalMaximumMilliseconds': 1200000, 'priorBusinessReplayed': False}

    def suggestions(self, input_value, report):
        self.stage = 'suggestions_navigation_and_response_windows'
        need(report['navigation_actions'] == 5 and len(report['suggestions']) == 2, 'five_navigation_actions_not_completed')
        steps = [('navigation-LA-intent.json', 1), ('navigation-LA-suggestions-intent.json', 2),
                 ('navigation-home-intent.json', 3), ('navigation-LB-intent.json', 4), ('navigation-LB-suggestions-intent.json', 5)]
        previous_time = 0
        for name, ordinal in steps:
            step = self.private(name)
            need(step['action_ordinal'] == ordinal and previous_time <= time_value(step['elapsed_ms']), 'navigation_ordinal_or_time_order')
            previous_time = step['elapsed_ms']

        def tokens(value):
            return list(dict.fromkeys((value.get('class_name') or '').split()))

        def accessible(value):
            return value.get('aria_selected') == 'true' or value.get('aria_pressed') == 'true' or value.get('aria_current') in ('true', 'page')

        def marker(value):
            return re.search(r'(?:^|[-_])(?:active|selected|current)$', value, re.I) is not None

        def marked(value):
            return accessible(value) or any(marker(token) for token in tokens(value))

        accepted, window_results = [], []
        for symbol, ordinal in (('LA', 2), ('LB', 5)):
            phase = 'suggestions_' + symbol.lower()
            intent = self.private('navigation-' + symbol + '-suggestions-intent.json')
            result = self.private('suggestions-' + symbol + '-result.json')
            declared = [row for row in report['suggestions'] if row['symbol'] == symbol]
            need(len(declared) == 1, 'suggestions_result_symbol_ambiguous')
            declared = declared[0]
            library = input_value['libraries'][symbol]
            need(intent['action'] == 'click_visible_suggestions_button' and intent['symbol'] == result['symbol'] == symbol and
                 intent['view_id'] == declared['view_id'] == library['viewId'] and
                 intent['action_ordinal'] == result['action_ordinal'] == declared['action_ordinal'] == ordinal and
                 intent['phase'] == result['phase'] == declared['phase'] == phase and
                 intent['transport_sequence'] == result['transport_sequence_before'] <= result['transport_sequence_after'] and
                 intent['elapsed_ms'] <= result['elapsed_ms'] <= report['elapsed_ms'], 'suggestions_intent_result_binding')
            selection = result['selection']
            before, after = selection['before'], selection['after']
            need(same(before, intent['before']), 'suggestions_before_dom_changed')
            for state in (before, after):
                need(set(state) == {'shows', 'suggestions'}, 'suggestions_dom_control_set')
                for label in ('Shows', 'Suggestions'):
                    button = state[label.lower()]
                    need(button['tag'] == 'button' and button['text'] == label and button['disabled'] is False and
                         button['bounds']['width'] > 0 and button['bounds']['height'] > 0, 'suggestions_visible_button_identity')
            transferred = [value for value in tokens(before['shows']) if value not in tokens(after['shows']) and
                           value not in tokens(before['suggestions']) and value in tokens(after['suggestions']) and marker(value)]
            explicit = accessible(after['suggestions'])
            reconstructed = {'selected': not marked(before['suggestions']) and not marked(after['shows']) and (explicit or bool(transferred)),
                'explicit_accessibility_marker': explicit, 'before_suggestions_explicitly_selected': marked(before['suggestions']),
                'after_shows_explicitly_selected': marked(after['shows']), 'transferred_selection_classes': transferred,
                'before': before, 'after': after}
            need(reconstructed['selected'] is True and same(selection, reconstructed) and same(declared['selection'], reconstructed),
                 'suggestions_selected_state_not_reconstructed_from_dom')
            visible = self.private('visible-suggestions-' + symbol.lower() + '.json')
            need(visible['phase'] == phase and visible['route'] == result['route'] and
                 intent['elapsed_ms'] <= visible['elapsed_ms'] <= result['elapsed_ms'],
                 'suggestions_visible_receipt_scope_or_time_differs')
            shared = ('tag', 'text', 'class_name', 'aria_selected', 'aria_pressed', 'aria_current', 'tab_index', 'disabled')
            for label in ('Shows', 'Suggestions'):
                controls = [row for row in visible['controls'] if row['tag'] == 'button' and row['text'] == label]
                need(len(controls) == 1 and same({key: controls[0][key] for key in shared},
                                               {key: after[label.lower()][key] for key in shared}),
                     'suggestions_visible_dom_selection_differs_from_observed_after_state')
            window = intent['window']
            need(set(window) == {'start_actor_ms', 'deadline_actor_ms', 'maximum_wait_ms'} and
                 0 < window['maximum_wait_ms'] <= 20000 and time_value(window['start_actor_ms']) <= time_value(window['deadline_actor_ms']) and
                 abs(window['deadline_actor_ms'] - window['start_actor_ms'] - window['maximum_wait_ms']) <= 0.000001 and
                 same(result['window'], window) and same(declared['window'], window), 'suggestions_response_window_changed')
            for route in (intent['route'], result['route']):
                fragment = urlsplit(route).fragment.lstrip('!/')
                query = parse_qsl(fragment.split('?', 1)[1] if '?' in fragment else '', keep_blank_values=True)
                need(any(key.lower() in ('parentid', 'topparentid', 'id', 'viewid') and value == library['viewId'] for key, value in query),
                     'suggestions_window_left_bound_library')
            eligible = [row for row in self.summary['globalResponses'] if row['phase'] == phase and
                        row['requestStartActorMs'] >= window['start_actor_ms'] and
                        row['physicalFinishedActorMs'] <= window['deadline_actor_ms'] and row['frameFinishedActorMs'] <= window['deadline_actor_ms']]
            eligible_ids = {row['physicalId']: row for row in eligible}
            for row in result['completed_global_responses']:
                known = eligible_ids.get(row['physical_id'])
                need(known is not None and row['actual_client_response_bound'] is True and row['frame_id'] == known['frameId'] and
                     row['request_sha256'] == known['requestSha256'] and row['token_sha256'] == known['tokenSha256'] and
                     row['navigation_cause'] == phase and row['ordered_ids'] == known['orderedIds'] and
                     row['frame_decoded_body_sha256'] == row['response']['decoded_body_sha256'] == known['decodedBodySha256'] and
                     row['response']['decoded_bytes'] == known['decodedBodyBytes'] and row['response']['body_sha256'] == known['rawBodySha256'] and
                     row['request_start_elapsed_ms'] == known['requestStartActorMs'] and
                     row['finished_elapsed_ms'] == known['physicalFinishedActorMs'] and row['frame_finished_elapsed_ms'] == known['frameFinishedActorMs'],
                     'suggestions_result_contains_unbound_or_out_of_window_response')
                final = [value for value in report['nextup'] if value['physical_id'] == row['physical_id']]
                need(len(final) == 1 and same(row, {key: value for key, value in final[0].items() if key != 'within_suggestions_window'}),
                     'suggestions_intermediate_response_differs_from_final_bound_record')
            need(declared['response_physical_ids'] == [row['physical_id'] for row in result['completed_global_responses']] and
                 declared['response_frame_ids'] == [row['frame_id'] for row in result['completed_global_responses']] and
                 declared['completed_global_response_count'] == len(result['completed_global_responses']) and
                 declared['result_receipt_sha256'] == self.evidence[str(ROOT / 'private' / ('suggestions-' + symbol + '-result.json'))]['sha256'],
                 'suggestions_result_summary_or_hash_not_bound')
            accepted.extend(eligible)
            window_results.append({'symbol': symbol, 'actionOrdinal': ordinal, 'selectionReconstructedFromDOM': True,
                                   'window': window, 'boundGlobalPhysicalIds': list(eligible_ids)})
        accepted_ids = {row['physicalId'] for row in accepted}
        for row in report['nextup']:
            need(row['within_suggestions_window'] is (row['physical_id'] in accepted_ids), 'claimed_suggestions_window_membership')
        self.summary.update(suggestions=window_results, navigationActionCount=5, suggestionsResponseWindowsReconstructed=True,
                            actualGlobalResponseBound=bool(accepted), suggestionsWindowGlobalResponseCount=len(accepted),
                            discoveryOutcome='actual_client_global_request_unobserved' if not accepted else
                                'client_positive_requires_separate_frozen_public_control' if any(row['orderedIds'] for row in accepted) else
                                'reference_global_positive_unresolved')

    def visible(self, report):
        self.stage = 'visible_evidence'
        labels = VISIBLE_LABELS
        rows = {row['label']: row for row in report['visible']}
        need(set(rows) == set(labels), 'visible_navigation_receipt_set')
        for label in labels:
            value = self.private('visible-' + label + '.json')
            raw = self.read(ROOT / 'private' / ('visible-' + label + '.png'), rows[label]['screenshot_sha256'])
            need(raw.startswith(b'\x89PNG\r\n\x1a\n') and value['label'] == label and
                 self.evidence[str(ROOT / 'private' / ('visible-' + label + '.json'))]['sha256'] == rows[label]['receipt_sha256'] and
                 same(value['headings'], rows[label]['headings']) and isinstance(value['controls'], list), 'visible_screenshot_or_dom_binding')
        # A separate post-logout receipt is required. It cannot be synthesized
        # from the worker's login_view_visible boolean. The producer contract
        # must be frozen with this receipt before actual execution.
        logout = self.private('logout-visible.json')
        retained = report['cleanup']['logout_visible']
        png = self.read(ROOT / 'private/logout-visible.png', retained['screenshot_sha256'])
        need(self.evidence[str(ROOT / 'private/logout-visible.json')]['sha256'] == retained['receipt_sha256'] and
             all(same(logout[key], retained[key]) for key in ('manual_login_visible', 'password_forms', 'visible_sign_in_buttons')) and
             type(logout['password_forms']) is int and type(logout['visible_sign_in_buttons']) is int and
             png.startswith(b'\x89PNG\r\n\x1a\n') and (logout['manual_login_visible'] is True or logout['password_forms'] > 0),
             'post_logout_login_view_evidence_missing')
        self.summary.update(screenshotHashCount=len(labels) + 1, visibleDOMReceiptsHashBound=True, screenshotsVisuallyInspected=False)

    def complete_inventory_and_order(self, report, network, input_value):
        self.stage = 'complete_inventory_and_order'
        expected = {'admission.json', 'report.json', 'before-owned-state.json', 'after-owned-state.json',
                    'browser-login-private.json', 'browser-login-intent.json', 'browser-logout-intent.json',
                    'browser-network-private.json', 'logout-visible.json', 'logout-visible.png',
                    'navigation-LA-intent.json', 'navigation-LB-intent.json', 'navigation-home-intent.json',
                    'navigation-LA-suggestions-intent.json', 'navigation-LB-suggestions-intent.json',
                    'suggestions-LA-result.json', 'suggestions-LB-result.json'}
        expected.update('request-' + str(index) + '-' + kind + '.json' for index in range(1, 35) for kind in ('intent', 'response'))
        expected.update('visible-' + label + suffix for label in VISIBLE_LABELS for suffix in ('.json', '.png'))
        previous = 0
        for index, phase in enumerate(report['phases'], 1):
            need(previous <= time_value(phase['elapsed_ms']), 'driver_phase_time_order')
            previous = phase['elapsed_ms']
            if phase['name'] != 'tv_destination_confirmed':
                name = 'phase-' + str(index) + '.json'
                expected.add(name)
                need(same(phase, self.private(name)), 'driver_phase_receipt_binding')
        for row in network['physical']:
            if row['kind'] != 'read':
                expected.add('browser-mutation-' + row['id'] + '-intent.json')
            if row['kind'] == 'login':
                expected.add('browser-login-response-' + row['id'] + '.json')
            if row['method'] == 'GET' and (row['route'] or '').lower().rstrip('/') == '/shows/nextup':
                name = 'browser-response-' + row['id'] + '.json'
                expected.add(name)
                self.private(name)
        need({path.name for path in (ROOT / 'private').iterdir()} == expected and
             {path.name for path in (ROOT / 'export').iterdir()} == {'report.json'} and
             {path.name for path in ROOT.iterdir()} == {'private', 'export'}, 'complete_client_evidence_file_set')
        login = self.private('browser-login-intent.json')
        logout = self.private('browser-logout-intent.json')
        visible_logout = self.private('logout-visible.json')
        before_closed = self.private('request-17-response.json')
        after_start = self.private('request-18-intent.json')
        need(before_closed['elapsed_ms'] <= login['elapsed_ms'] <= logout['elapsed_ms'] <= visible_logout['elapsed_ms'] <= after_start['elapsed_ms'],
             'recorder_browser_phase_overlap')
        network_metadata = self.evidence[str(ROOT / 'private/browser-network-private.json')]['identity']
        after_metadata = self.evidence[str(ROOT / 'private/request-18-intent.json')]['identity']
        need(network_metadata[7] <= after_metadata[7], 'closed_browser_receipt_not_before_after_recorder')
        for symbol in ('LA', 'LB'):
            navigation = self.private('navigation-' + symbol + '-intent.json')
            library = input_value['libraries'][symbol]
            before = self.private('visible-before-' + symbol.lower() + '.json')
            after = self.private('visible-after-' + symbol.lower() + '.json')
            need(navigation['action'] == 'click_visible_library_title' and navigation['symbol'] == symbol and
                 navigation['viewId'] == library['viewId'] and type(navigation['candidateCount']) is int and
                 0 < navigation['candidateCount'] <= 3 and before['elapsed_ms'] <= navigation['elapsed_ms'] <= after['elapsed_ms'],
                 'library_navigation_intent_or_timing')
            need(any(row['id'] == library['viewId'] and library['name'] in (row['text'], row['title']) for row in before['controls']),
                 'library_visible_control_not_bound')
            view_rows = [row for row in network['physical'] if row['completed'] is True and row['status'] == 200 and
                         re.search(r'/Views/?$', row['route'] or '', re.I) and row['token_sha256'] == self.summary['uiTokenSha256']]
            catalog_views = []
            for row in view_rows:
                value = row.get('response', {}).get('json')
                catalog_views.extend(value if isinstance(value, list) else value.get('Items', []) if isinstance(value, dict) else [])
            need(any(value.get('Id') == library['viewId'] and value.get('Name') == library['name'] for value in catalog_views),
                 'library_visible_control_not_in_actual_client_views_projection')
            fragment = urlsplit(after['route']).fragment.lstrip('!/')
            query = parse_qsl(fragment.split('?', 1)[1] if '?' in fragment else '', keep_blank_values=True)
            need(any(key.lower() in ('parentid', 'topparentid', 'id', 'viewid') and value == library['viewId'] for key, value in query) and
                 any(any(isinstance(value, str) and value.lower() in ('shows', 'series') for value in
                         (row['text'], row['label'], row['title'])) for row in after['controls']), 'actual_tv_destination_not_bound')
        home_intent = self.private('navigation-home-intent.json')
        home = self.private('visible-home-between-libraries.json')
        need(home_intent['action'] == 'click_visible_home_link' and home_intent['elapsed_ms'] <= home['elapsed_ms'] and
             re.match(r'^!?/home(?:[/?]|$)', urlsplit(home['route']).fragment, re.I) is not None and
             all(any(row['id'] == library['viewId'] and library['name'] in (row['text'], row['title']) for row in home['controls'])
                 for library in input_value['libraries'].values()), 'home_navigation_destination_not_bound')
        self.summary.update(completePrivateAndExportFileSetsVerified=True, recorderBrowserPhasesSeparated=True,
                            libraryNavigationDOMBound=True, privateFileCount=len(expected), exportFileCount=1)

    def unit(self, admission, input_value):
        self.stage = 'unit_shutdown'
        need(admission['pid'] == self.args.former_pid, 'driver_admission_pid_differs')
        launch = self.doc(E / 'launch-result.json')
        intent = self.descriptor(launch['intent'], E / 'launch-intent.json')
        preparation = self.descriptor(intent['preparation'], E / 'preparation.json')
        expected_input = {'path': str(Path(self.args.input)), 'sha256': self.args.input_sha256}
        expected_node = {'path': self.args.node_path, 'sha256': 'ca0728526aa1cc4e3056decec848ecc6d2c5391cecdd4e21a0ebd221d665c84e'}
        expected_unit = {'path': str(UNIT_FILE), 'sha256': self.args.unit_file_sha256}
        need(intent['kind'] == 'nextup-client-discovery-launch-intent' and intent['unitName'] == UNIT and
             intent['input'] == expected_input and intent['node'] == expected_node and intent['unitFile'] == expected_unit and
             same(intent['sourceClosure'], input_value['sourceClosure']) and intent['oneShot'] is True and
             intent['maximumSeconds'] == input_value['budgets']['maximumSeconds'] and intent['permittedPlaybackAttempts'] == 0,
             'actual_launch_intent_binding')
        need(preparation['kind'] == 'nextup-client-discovery-preparation' and preparation['unitName'] == UNIT and
             preparation['input'] == expected_input and preparation['node'] == expected_node and preparation['unitFile'] == expected_unit and
             same(preparation['sourceClosure'], input_value['sourceClosure']) and preparation['notStarted'] is True and
             preparation['startCalls'] == 0 and preparation['outputRootEmpty'] is True, 'actual_preparation_binding')
        need(launch['launchReturnCode'] == 0 and launch['launchStdout'] == launch['launchStderr'] == '' and
             launch['properties']['Id'] == UNIT and launch['properties']['MainPID'] == launch['properties']['ExecMainPID'] == str(self.args.former_pid) and
             launch['properties']['InvocationID'] == self.args.invocation_id, 'actual_launch_process_binding')
        unit_bytes = self.read(UNIT_FILE, self.args.unit_file_sha256)
        unit_lines = unit_bytes.decode('utf-8').splitlines()
        environment_lines = [line for line in unit_lines if line.startswith('Environment=')]
        command_lines = [line for line in unit_lines if line.startswith('ExecStart=')]
        need(environment_lines == ['Environment=PLAYWRIGHT_BROWSERS_PATH=/root/.cache/ms-playwright'] and len(command_lines) == 1,
             'actual_unit_file_environment_or_command')
        need(not any(line.startswith(('EnvironmentFile=', 'PassEnvironment=', 'UnsetEnvironment=')) for line in unit_lines),
             'pinned_unit_has_unapproved_environment_sources')
        prepared = preparation['properties']
        dynamic = {'ActiveState', 'SubState', 'MainPID', 'ExecMainPID', 'InvocationID', 'ExecStart'}
        static = {key: value for key, value in prepared.items() if key not in dynamic}
        need(prepared['ActiveState'] == 'inactive' and prepared['SubState'] == 'dead' and
             prepared['MainPID'] == prepared['ExecMainPID'] == '0' and prepared['InvocationID'] == '' and
             static['Environment'] == 'PLAYWRIGHT_BROWSERS_PATH=/root/.cache/ms-playwright', 'prepared_unit_not_fresh_or_environment_differs')
        fields = set(prepared) | {'Result', 'ExecMainCode', 'ExecMainStatus', 'ControlGroup', 'NRestarts',
                                  'DropInPaths', 'PassEnvironment', 'UnsetEnvironment'}
        result = subprocess.run(['/usr/bin/systemctl', 'show', UNIT, '--property=' + ','.join(sorted(fields))],
                                capture_output=True, text=True, timeout=15, check=False)
        need(result.returncode == 0 and result.stderr == '' and len(result.stdout) <= 65536, 'unit_metadata_unavailable')
        unit = dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)
        need(all(unit.get(key) == value for key, value in static.items()) and
             all(unit.get(key) == '' for key in ('DropInPaths', 'PassEnvironment', 'UnsetEnvironment')),
             'actual_prepared_environment_or_resource_properties_changed')
        need(set(unit) == fields and unit['Id'] == UNIT and unit['LoadState'] == 'loaded' and unit['MainPID'] == '0' and
             unit['ExecMainPID'] == str(self.args.former_pid) and unit['InvocationID'] == self.args.invocation_id and
             unit['Result'] == 'success' and unit['ExecMainCode'] == '1' and unit['ExecMainStatus'] == '0' and
             unit['ActiveState'] == 'active' and unit['SubState'] == 'exited' and unit['ControlGroup'] == '' and unit['NRestarts'] == '0',
             'unit_not_successfully_closed')
        need(not os.path.lexists('/proc/' + str(self.args.former_pid)) and
             not os.path.lexists('/sys/fs/cgroup/system.slice/' + UNIT), 'unit_pid_or_cgroup_still_present')
        def observed_argv(value):
            match = re.fullmatch(r'\{ path=/usr/bin/flock ; argv\[\]=([^{}\r\n]+) ; ignore_errors=no ; [^{}\r\n]+ \}', value)
            need(match is not None, 'unit_command_not_single_flock')
            return shlex.split(match.group(1))
        argv = observed_argv(unit['ExecStart'])
        expected_argv = ['/usr/bin/flock', '--exclusive', '--nonblock', '--no-fork', str(W / 'reference.lock'), self.args.node_path,
                         str(TOOL / FILES[0]), '--input', str(Path(self.args.input)), '--input-sha256', self.args.input_sha256]
        need(argv == observed_argv(prepared['ExecStart']) == shlex.split(command_lines[0].removeprefix('ExecStart=')) == expected_argv,
             'unit_argv_differs_from_frozen_input')
        self.summary['unit'] = {'name': UNIT, 'formerPid': self.args.former_pid, 'invocationId': self.args.invocation_id,
            'argvSha256': sha(canonical(argv).encode()), 'formerPidAbsent': True, 'cgroupAbsent': True,
            'preparedStaticPropertiesExact': True, 'environmentExact': True, 'properties': static, 'unitFile': expected_unit,
            'preparation': intent['preparation'], 'launchIntent': launch['intent'],
            'launchResult': {'path': str(E / 'launch-result.json'), 'sha256': self.evidence[str(E / 'launch-result.json')]['sha256']}}

    def run(self):
        verifier_path = Path(__file__).absolute()
        self.summary['verifierSource'] = {'path': str(verifier_path), 'sha256': sha(self.read(verifier_path))}
        input_value = self.doc(self.args.input, self.args.input_sha256)
        need(input_value['kind'] == 'nextup-reference-client-discovery-input' and input_value['version'] == 1 and
             input_value['runId'] == 'nextup-reference-client-discovery-02' and input_value['root'] == str(ROOT), 'input_scope')
        need(set(input_value['sourceClosure']) == set(FILES), 'source_closure_membership')
        for name in FILES:
            descriptor = input_value['sourceClosure'][name]
            need(descriptor['path'] == str(TOOL / name) and descriptor['sha256'] == SOURCE_PINS[name], 'source_closure_final_pin')
            self.read(descriptor['path'], descriptor['sha256'])
        execution = self.descriptor(input_value['execution'])
        release = self.descriptor(input_value['release'])
        need(release['kind'] == 'nextup-reference-client-discovery-release' and same(release['execution'], input_value['execution']), 'release_execution_binding')
        matrix = execution['matrix']
        credentials = self.descriptor(execution['credentials'])['actors']['P']
        user, server = matrix['actors']['P']['userId'], matrix['serverId']
        need(all(credentials[key] == matrix['actors']['P'][key] for key in ('credentialRef', 'userId', 'username')), 'actor_credential_identity')
        self.secrets.append(credentials['password'])
        expected_release = {'matrixTerminal': 'b08b1a2ea8ce659ac1868df44b557455efccf69ae2dfd5caf33bb0c1b8b627da',
            'independentTerminal': '4689a670ad80efcd0334c3f1956e540a986cfbc735da8f4e6b93acd68d804d8c',
            'independentRuntimeTerminal': '782ab2ddb4e1d979f9f93a42b3c1d9cbb9b1078413d5e8a354242ff715eeae93',
            'operatorCommit': '46e5811f1b2099ad49a20a4915fe0d83bd6adeb3ea0cdd5a51d455b714ba2b18'}
        released = {}
        for key, checksum in expected_release.items():
            need(release[key]['sha256'] == checksum, 'release_does_not_bind_completed_matrix07')
            released[key] = self.descriptor(release[key])
        need(released['operatorCommit']['terminalPrivateSha256'] == release['matrixTerminal']['sha256'] and
             released['operatorCommit']['status'] == released['matrixTerminal']['candidateStatus'] == 'matrix_protocol_complete' and
             released['independentTerminal']['cleanupReconstructed'] is True and
             released['independentRuntimeTerminal']['runtimeIdentityPreservationVerified'] is True and
             release['revokedActors'] == ['P', 'Q'] and all(release[key] is True for key in
                 ('apiClosed', 'zeroBaselineVerified', 'unitInactive', 'originalClientAllowed')), 'matrix07_release_semantic_binding')
        admission = self.private('admission.json')
        report = self.private('report.json')
        need(admission['input_sha256'] == report['input_sha256'] == self.args.input_sha256 and
             same(admission['execution'], input_value['execution']) and same(admission['release'], input_value['release']) and
             same(report['source_sha256'], SOURCE_PINS) and
             report['failure'] is None and report['recorder_requests'] == 34 and report['playback_attempts'] == 0,
             'driver_not_complete_under_frozen_input')
        self.continuation(input_value, report)
        before = self.recorder('before', 0, matrix, credentials, user, server)
        after = self.recorder('after', 17, matrix, credentials, user, server)
        for phase in ('before', 'after'):
            self.descriptor(report['state'][phase], ROOT / 'private' / (phase + '-owned-state.json'))
        for name in EPISODES + SUMMARIES:
            need(same(before[name], after[name]), 'complete_episode_or_summary_state_changed')
        for field in ('Id', 'Name', 'Policy', 'Configuration'):
            need(same(before['profile'][field], after['profile'][field]), 'accepted_profile_field_changed')
        need(same(before['preferences'], after['preferences']), 'full_preferences_changed')
        need(same(report['state']['comparison'], {'preserved': True, 'changed': [], 'full_profile_equal': same(before['profile'], after['profile']),
             'profile_comparison': 'Full profiles retained; acceptance compares Id, Name, Policy, and Configuration.'}),
             'worker_state_comparison_not_reconstructed')
        self.summary.update(fullEpisodeSummaryUserDataPreserved=True, profileAcceptedFieldsPreserved=True,
                            fullProfileEqual=same(before['profile'], after['profile']), fullPreferencesPreserved=True,
                            recorderRequestCount=34, recorderTokensIndependentlyRejected=True)
        network = self.browser(input_value, matrix, credentials, user, server, report)
        self.suggestions(input_value, report)
        self.visible(report)
        self.complete_inventory_and_order(report, network, input_value)
        self.unit(admission, input_value)
        need(report['outcome'] == self.summary['discoveryOutcome'], 'worker_outcome_not_reconstructed')
        exported = self.doc(ROOT / 'export/report.json')
        need(exported['private_report_sha256'] == self.evidence[str(ROOT / 'private/report.json')]['sha256'], 'safe_report_private_binding')
        expected_export = deepcopy(report)
        del expected_export['browser']
        expected_export['state'] = {'comparison': report['state'].get('comparison'), 'before_sha256': report['state']['before']['sha256'],
                                    'after_sha256': report['state']['after']['sha256']}
        expected_export['private_report_sha256'] = self.evidence[str(ROOT / 'private/report.json')]['sha256']
        need(same(exported, expected_export), 'safe_report_projection_not_reconstructed')
        self.stage = 'final_evidence_stability'
        for row in list(self.evidence.values()):
            self.read(row['path'], row['sha256'])
        need(all(value == 0 for value in self.counters.values()), 'independent_operation_boundary_violated')
        self.summary['input'] = {'path': str(Path(self.args.input)), 'sha256': self.args.input_sha256}

    def publish(self, failure=None):
        result = {'kind': 'nextup-client-discovery-independent-terminal', 'version': 1,
            'capturedAt': datetime.now(timezone.utc).isoformat(), 'status': 'failed' if failure else
                'client_closed_global_request_unobserved' if self.summary.get('actualGlobalResponseBound') is False else
                'client_discovery_evidence_reconstructed',
            'failure': failure, 'stage': self.stage, 'clientDiscoveryEvidenceVerified': failure is None,
            'initialEpisodeHistory': 'zero_after_matrix_cleanup', 'playbackAcceptance': False, 'refreshAcceptance': False,
            'watchedMatrixStateClientComparison': False, 'originalClientFullAcceptance': False,
            'outerProtectedRootsAndGobyPreservationVerified': False, 'outerPreservationReceiptRequired': True,
            'outerExpectedProtectedRootCount': 198,
            'ui401RawWireReconstructed': False, 'pageRawBodyBytesRetained': False,
            'rawURLBytesIndependentlyReconstructed': False, 'resumeOrRetryAllowed': False,
            'counters': self.counters, 'evidence': list(self.evidence.values()), **self.summary}
        raw = (json.dumps(result, sort_keys=True, indent=2, allow_nan=False) + '\n').encode()
        with os.fdopen(os.open(OUTPUT, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb') as handle:
            handle.write(raw)
            handle.flush()
            os.fsync(handle.fileno())
        fd = os.open(E, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
        print(canonical({'path': str(OUTPUT), 'sha256': sha(raw), 'status': result['status'],
                         'clientDiscoveryEvidenceVerified': failure is None, 'outerPreservationReceiptRequired': True}))


def main():
    global OUTPUT
    need(sys.platform == 'linux' and os.geteuid() == 0 and os.environ.get('SSH_CONNECTION') and
         sys.flags.isolated and sys.flags.dont_write_bytecode, 'authorized_remote_python_I_B_required')
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input', required=True)
    parser.add_argument('--input-sha256', required=True)
    parser.add_argument('--former-pid', type=int, required=True)
    parser.add_argument('--invocation-id', required=True)
    parser.add_argument('--node-path', required=True)
    parser.add_argument('--unit-file-sha256', required=True)
    parser.add_argument('--receipt-number', type=int, default=1,
                        help='Exclusive closure receipt number, from 1 through 99; 1 retains the original filename.')
    args = parser.parse_args()
    need(fingerprint(args.input_sha256) and fingerprint(args.unit_file_sha256) and args.former_pid > 1 and
         re.fullmatch(r'[0-9a-f]{32}', args.invocation_id), 'required_runtime_binding_invalid')
    need(1 <= args.receipt_number <= 99, 'closure_receipt_number_outside_bound')
    OUTPUT = E / ('independent-client-terminal.json' if args.receipt_number == 1 else
                  'independent-client-terminal-' + format(args.receipt_number, '02d') + '.json')
    need(E.is_dir() and E.resolve(strict=True) == E and not os.path.lexists(OUTPUT), 'exclusive_execution_output_unavailable')
    os.umask(0o077)
    verifier = Verifier(args)
    verifier.summary['closureReceiptNumber'] = args.receipt_number
    sys.addaudithook(verifier.audit)
    try:
        verifier.run()
    except Exception as error:
        verifier.publish({'type': type(error).__name__, 'reason': str(error) if isinstance(error, EvidenceError) else 'required_evidence_missing_or_invalid'})
        return 2
    verifier.publish()
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
