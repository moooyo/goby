#!/usr/bin/env python3
"""Observe delayed reference notifications without repeating catalog mutations."""

import hashlib
import http.client
import json
import os
from pathlib import Path
import secrets
import signal
import stat
import sys
import time
import types
from urllib.parse import urlencode


W = Path('/opt/goby-test/exec-work-m3e')
ROOT = W / 'reference-library-changed-tail-v1'
PARENT = W / 'reference-library-changed-v1'
SOURCE = W / 'reference-library-changed-tool-01/capture-client-library-changed-reference.py'
SOURCE_SHA = '51c05a0c826cd18983901f631d5c04163372f37591b980459c7fedac2fb5d445'
TRANSPORT = W / 'reference-library-changed-transport-tool-01/bounded_websocket_capture.py'
TRANSPORT_SHA = '54fd18d50cf254aa6e93ac587f0d8fdf17d6897432e0448c2d00ddeb75ec7f0e'
USER = 'c5f36699a54f4971a891682cd9de410f'
SERVER = 'f56dec8ff7414847873064c4be9fba74'


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def main():
    os.umask(0o077)
    assert sys.platform == 'linux' and os.geteuid() == 0
    assert os.readlink('/proc/self/ns/net') == 'net:[4026532602]'
    assert os.readlink('/proc/332054/ns/net') == 'net:[4026532602]'
    info = SOURCE.lstat()
    assert stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1
    raw = SOURCE.read_bytes()
    assert sha(raw) == SOURCE_SHA
    support = types.ModuleType('frozen_library_changed_reference')
    support.__file__ = str(SOURCE)
    exec(compile(raw, str(SOURCE), 'exec'), support.__dict__)
    transport = support.module(TRANSPORT, TRANSPORT_SHA, 'bounded_tail_transport')
    op = support.module(support.OPERATOR, support.OPERATOR_SHA, 'reference_tail_identity')
    owner = json.loads(support.read(op.OWNER, support.OWNER_SHA))
    op.same_service(owner)
    created = json.loads(support.read(PARENT / 'private/created-viewer.json'))
    assert created['user']['Id'] == USER and created['user']['Name'] == support.VIEWER_NAME
    credentials = json.loads(support.read(PARENT / 'private/credentials.json'))
    assert credentials['marker'] == support.MARKER and credentials['username'] == support.VIEWER_NAME
    ROOT.mkdir(mode=0o700)
    (ROOT / 'private').mkdir(mode=0o700)
    (ROOT / 'export').mkdir(mode=0o700)
    token, proven, closed, capture = None, False, False, None
    device = 'goby-librarychanged-tail-' + secrets.token_hex(16)
    used, errors = set(), []

    def save(name, value, private=True):
        raw = value if isinstance(value, bytes) else support.encoded(value)
        with (ROOT / ('private' if private else 'export') / name).open('xb') as stream:
            stream.write(raw); stream.flush(); os.fsync(stream.fileno())
        return sha(raw)

    save('intent.json', {'marker': 'goby-library-changed-tail-v1', 'parentScope': str(PARENT),
        'referenceIdentity': owner['serviceIdentity'], 'userId': USER, 'deviceId': device,
        'durationSeconds': 240, 'catalogMutations': 0, 'sourceSha256': sha(Path(__file__).read_bytes())})

    def request(name, method, route, body=None):
        nonlocal token, proven
        allowed = {
            'login': ('POST', '/emby/Users/AuthenticateByName'),
            'identity': ('GET', '/emby/Users/' + USER),
            'logout': ('POST', '/emby/Sessions/Logout'),
            'exact': ('GET', '/emby/System/Info'),
        }
        assert name not in used and (method, route) == allowed[name]
        assert name == 'login' or proven
        assert body == ({'Username': support.VIEWER_NAME, 'Pw': credentials['password']} if name == 'login' else None)
        op.same_service(owner)
        used.add(name)
        save(name + '-intent.json', {'method': method, 'route': route, 'deviceId': device})
        connection = http.client.HTTPConnection('127.0.0.1', 18097, timeout=8)
        headers = {'Accept': 'application/json', 'Accept-Encoding': 'identity', 'Connection': 'close',
            'Authorization': f'Emby Client="Goby LibraryChanged Tail", Device="Linux Recorder", DeviceId="{device}", Version="1.0"'}
        if name != 'login': headers['X-Emby-Token'] = token
        if body is not None: headers['Content-Type'] = 'application/json'
        result = {'complete': False}
        signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(TimeoutError('HTTP deadline expired.')))
        signal.setitimer(signal.ITIMER_REAL, 12)
        try:
            connection.request(method, route, support.encoded(body) if body is not None else None, headers)
            response = connection.getresponse()
            raw = response.read((2 << 20) + 1)
            assert len(raw) <= 2 << 20
            length = response.getheader('Content-Length')
            assert length is None or int(length) == len(raw)
            data = json.loads(raw) if raw and 'application/json' in (response.getheader('Content-Type') or '') else None
            result.update(status=response.status, complete=True, body=data, responseSha256=sha(raw))
            if name == 'login':
                token = data.get('AccessToken') if isinstance(data, dict) else None
                proven = bool(response.status == 200 and isinstance(token, str) and token and data.get('ServerId') == SERVER and
                    data.get('User', {}).get('Id') == USER and data['User'].get('Name') == support.VIEWER_NAME and
                    data['User'].get('Policy', {}).get('IsAdministrator') is False and
                    data.get('SessionInfo', {}).get('UserId') == USER and data['SessionInfo'].get('DeviceId') == device)
            return result
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            connection.close()
            save(name + '-response.json', result)

    try:
        request('login', 'POST', '/emby/Users/AuthenticateByName', {'Username': support.VIEWER_NAME, 'Pw': credentials['password']})
        assert proven
        identity = request('identity', 'GET', '/emby/Users/' + USER)
        assert identity['status'] == 200 and identity['body']['Id'] == USER
        capture = transport.Capture('127.0.0.1', 18097, '/embywebsocket?' + urlencode({'api_key': token, 'deviceId': device}))
        capture.connect()
        deadline = time.monotonic() + 240
        while time.monotonic() < deadline:
            assert capture.open
            capture.wait(max(0, min(10, deadline - time.monotonic())))
            assert capture.open
    except Exception as error:
        errors.append(type(error).__name__)
    finally:
        if capture is not None:
            try: capture.close()
            except Exception as error: errors.append('close:' + type(error).__name__)
            save('websocket.json', capture.record)
        if proven:
            logout = exact = None
            try: logout = request('logout', 'POST', '/emby/Sessions/Logout')
            except Exception as error: errors.append('logout:' + type(error).__name__)
            try: exact = request('exact', 'GET', '/emby/System/Info')
            except Exception as error: errors.append('exact:' + type(error).__name__)
            closed = bool(logout and logout['status'] == 204 and exact and exact['status'] == 401)
        if not closed: errors.append('session_cleanup_unproven')
    events = []
    if capture is not None:
        for event in support.library_events(capture.record):
            events.append({key: event[key] for key in ('at', 'elapsedMs', 'payloadLength') if key in event} |
                {'json': support.sanitize(event['json'], {credentials['password'], token or ''}), 'privateEventSha256': sha(support.encoded(event))})
    report = {'marker': 'goby-library-changed-tail-v1', 'result': 'complete' if not errors else 'retained_for_review',
        'catalogMutations': 0, 'userId': USER, 'deviceId': device, 'sessionRevoked': closed,
        'credentialFingerprint': sha(token.encode()) if token else None, 'events': events, 'errors': errors,
        'durationMs': capture.record.get('observedDurationMs') if capture else None,
        'boundary': 'Receipt-only observation concurrent with the parent controlled run and its delayed tail; no replay or new catalog action.'}
    save('report.json', report, private=False)
    print(json.dumps({'result': report['result'], 'LibraryChangedObservations': len(events), 'sessionRevoked': closed}))
    return 0 if not errors else 1


if __name__ == '__main__':
    sys.exit(main())
