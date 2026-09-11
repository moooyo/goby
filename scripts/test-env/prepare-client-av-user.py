#!/usr/bin/env python3
"""Create one new, retained ordinary account for owned audio/subtitle UI tests.

All requests target the pinned fresh reference fixture. Existing users, media,
progress, configuration, and library definitions are never modified.
"""

import hashlib
import http.client
import json
import os
from pathlib import Path
import secrets
import stat
import sys
from urllib.parse import quote


ROOT = Path('/opt/goby-test/exec-work-m3e')
WORK = ROOT / 'av-user-setup'
MARKER = 'goby-reference-av-client-v1'
NAME = 'm3e-reference-av-client'
PID = 332054
TICKS = '357218'
EXE = '/dev/shm/goby-emby-reference/package/opt/emby-server/system/EmbyServer'
EXE_SHA = 'c109c9817dea25cc516b9969a87aa1ffa48e41adcb5e3dbc87d686c7bcb28ac2'
ORIGIN = 'http://127.0.0.1:18197'


def require(value, reason):
    if not value:
        raise RuntimeError(reason)


def private(path, maximum=2 << 20):
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and stat.S_IMODE(info.st_mode) == 0o600 and
            info.st_nlink == 1 and info.st_size <= maximum, 'Private input identity or size differs')
    return json.loads(path.read_text())


def write(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as output:
        json.dump(value, output, indent=2)
        output.write('\n')


def pin():
    process = Path('/proc') / str(PID)
    fields = (process / 'stat').read_text().split(') ', 1)[1].split()
    require(fields[19] == TICKS and os.readlink(process / 'exe') == EXE and
            hashlib.sha256((process / 'exe').read_bytes()).hexdigest() == EXE_SHA, 'Fresh reference process changed')


def snapshot(users):
    return {row['Id']: {'Name': row['Name'], 'Policy': row.get('Policy'), 'Configuration': row.get('Configuration')}
            for row in users}


def complete_owned():
    """Finish the acknowledged new account without recreating it or its password."""
    source = private(ROOT / 'reference-browser.json')
    intent = private(WORK / 'private-intent.json')
    created = private(WORK / 'created-account.json')
    earlier = private(WORK / 'setup-report.json')
    before = private(WORK / 'existing-account-baseline.json')
    require(intent['marker'] == created['marker'] == earlier['marker'] == MARKER and
            created['username'] == intent['username'] == NAME and created['account_id'] == earlier['account_id'] and
            earlier['admin_logout_status'] == 204 and earlier['admin_after_logout_status'] == 401,
            'The acknowledged account or previous control cleanup differs')
    identifier = created['account_id']
    report = {'marker': MARKER, 'scope': 'Owned account setup continuation only; not client acceptance',
              'account_id': identifier, 'requests': [], 'complete': False,
              'policy_boundary': 'All three libraries in this isolated instance are owned synthetic fixtures; UI driver limits media paths to Movies and Music'}
    token = None

    def call(label, method, route, body=None):
        pin()
        headers = {'Accept': 'application/json', 'Content-Type': 'application/json',
                   'Authorization': 'Emby Client="Goby AV User Setup Continuation", DeviceId="goby-av-setup-continuation-v1", Device="Linux", Version="1"'}
        if token:
            headers['X-Emby-Token'] = token
        connection = http.client.HTTPConnection('127.0.0.1', 18197, timeout=12)
        try:
            connection.request(method, route, json.dumps(body) if body is not None else None, headers)
            response = connection.getresponse()
            raw = response.read((2 << 20) + 1)
            code = response.status
        finally:
            connection.close()
        pin()
        require(len(raw) <= 2 << 20, 'A continuation response exceeded its limit')
        report['requests'].append({'label': label, 'method': method, 'status': code})
        try:
            data = json.loads(raw) if raw else None
        except ValueError:
            data = None
        return code, data

    try:
        administrator = source['accounts']['admin']
        code, data = call('admin-login', 'POST', '/emby/Users/AuthenticateByName',
                          {'Username': administrator['username'], 'Pw': administrator['password']})
        require(code == 200 and data['User']['Id'] == administrator['userId'] and data['ServerId'] == source['serverId'],
                'Continuation administrator identity differs')
        token = data['AccessToken']
        write(WORK / 'private-continuation-admin-session.json', {'marker': MARKER, 'access_token': token})
        code, libraries = call('verify-isolated-owned-libraries', 'GET', '/emby/Library/VirtualFolders/Query')
        expected = {'M3e Reference ' + label: ['/opt/goby-fixtures/client-m3e/' + label] for label in ('Movies', 'Music', 'TV')}
        require(code == 200 and {row['Name']: row.get('Locations') for row in libraries['Items']} == expected,
                'The fresh instance includes an unowned library')
        code, user = call('acknowledged-account', 'GET', '/emby/Users/' + quote(identifier))
        require(code == 200 and user['Name'] == NAME and user['Policy']['IsAdministrator'] is False and user.get('HasPassword'),
                'The acknowledged ordinary account differs')
        policy = dict(user['Policy'], EnableAllFolders=True, EnabledFolders=[])
        code, _ = call('allow-only-isolated-fixture-catalog', 'POST', '/emby/Users/' + quote(identifier) + '/Policy', policy)
        require(code in (200, 204), 'The isolated catalog policy was not accepted')
        code, items = call('new-media-baseline', 'GET', '/emby/Users/' + quote(identifier) +
                           '/Items?Recursive=true&IncludeItemTypes=Movie,Audio&Fields=Path,MediaSources,MediaStreams&Limit=100')
        require(code == 200, 'The new media baseline is unavailable')
        write(WORK / 'continuation-item-inventory.json', items)
        paths = {'M3e Client Movie': '/opt/goby-fixtures/client-m3e/Movies/M3e Client Movie.mp4',
                 'M3e MP3': '/opt/goby-fixtures/client-m3e/Music/M3e Client Audio.mp3',
                 'M3e FLAC': '/opt/goby-fixtures/client-m3e/Music/M3e Client Audio.flac'}
        require(len(items.get('Items', [])) == 3 and {item['Name']: item.get('Path') for item in items['Items']} == paths,
                'The catalog does not match the exact three owned media leaves')
        baseline = []
        for item in items['Items']:
            code, detail = call('new-detail-baseline', 'GET', '/emby/Users/' + quote(identifier) + '/Items/' + quote(item['Id']))
            state = detail.get('UserData', {})
            require(code == 200 and state.get('PlaybackPositionTicks', 0) == 0 and state.get('PlayCount', 0) == 0 and
                    state.get('Played') is False and not state.get('LastPlayedDate'), 'The new media has existing history')
            baseline.append({'id': item['Id'], 'name': item['Name'], 'path': item['Path'], 'user_data': state,
                             'streams': [{key: stream.get(key) for key in ('Index', 'Type', 'Codec', 'Language', 'IsExternal')}
                                         for stream in detail.get('MediaStreams', [])]})
        code, users = call('existing-accounts-unchanged', 'GET', '/emby/Users')
        require(code == 200 and snapshot([row for row in users if row['Id'] != identifier]) == before,
                'Existing account configuration or policy differs from its preserved baseline')
        report['initial_media_state'] = baseline
        report['existing_accounts_unchanged'] = True
        write(WORK / 'browser.json', {'marker': MARKER, 'base_url': ORIGIN, 'direct_url': 'http://127.0.0.1:18097',
                                     'admin': {'username': administrator['username'], 'password': administrator['password']},
                                     'viewer': {'username': NAME, 'password': intent['password']}})
        report['complete'] = True
    finally:
        if token:
            report['admin_logout_status'], _ = call('admin-logout', 'POST', '/emby/Sessions/Logout')
            report['admin_after_logout_status'], _ = call('admin-token-after-logout', 'GET', '/emby/System/Info')
            require(report['admin_logout_status'] == 204 and report['admin_after_logout_status'] == 401,
                    'The continuation control token was not retired')
        write(WORK / 'completion-report.json', report)
    print(json.dumps({'result': 'owned_account_setup_completed', 'account_id': identifier,
                      'browser': str(WORK / 'browser.json'), 'report': str(WORK / 'completion-report.json'),
                      'admin_logout': report['admin_logout_status'], 'admin_after_logout': report['admin_after_logout_status']}))


def main():
    require(sys.platform == 'linux' and os.geteuid() == 0 and os.environ.get('SSH_CONNECTION'),
            'Run only through the authorized remote SSH environment')
    require(ROOT.resolve() == ROOT and stat.S_IMODE(ROOT.stat().st_mode) == 0o700 and ROOT.stat().st_uid == 0,
            'The parent workspace is not private and owned')
    pin()
    source = private(ROOT / 'reference-browser.json')
    if WORK.exists():
        if sys.argv[1:] == ['--complete-owned']:
            complete_owned()
            return
        if (WORK / 'completion-report.json').exists():
            report = private(WORK / 'completion-report.json')
            require(report.get('complete') is True and report.get('marker') == MARKER,
                    'The completed account setup is not verified')
            print(json.dumps({'result': 'retained_setup_reused', 'account_id': report['account_id'],
                              'browser': str(WORK / 'browser.json'), 'report': str(WORK / 'completion-report.json')}))
            return
        report = private(WORK / 'setup-report.json')
        credentials = private(WORK / 'browser.json')
        require(report['marker'] == credentials['marker'] == MARKER and report.get('complete') and
                report.get('admin_logout_status') == 204 and report.get('admin_after_logout_status') == 401,
                'Existing setup is incomplete; retain it without guessing ownership')
        print(json.dumps({'result': 'retained_setup_reused', 'report': str(WORK / 'setup-report.json'),
                          'account_id': report['account_id'], 'browser': str(WORK / 'browser.json')}))
        return
    WORK.mkdir(mode=0o700)
    intent = {'marker': MARKER, 'username': NAME, 'password': secrets.token_hex(32),
              'device_id': 'goby-av-setup-' + secrets.token_hex(8), 'server_id': source['serverId']}
    write(WORK / 'private-intent.json', intent)
    report = {'marker': MARKER, 'scope': 'Fixture setup only; not client UI acceptance', 'pid': PID, 'start_ticks': TICKS,
              'executable_sha256': EXE_SHA, 'complete': False, 'requests': [], 'account_id': None,
              'account_retained': True, 'existing_accounts_modified': False}
    token = None

    def request(label, method, route, body=None, authenticated=True):
        pin()
        require(route.startswith('/emby/') and not any(value in route for value in ('\r', '\n', '#')),
                'A setup request left the fixed API scope')
        require(len(report['requests']) < 30, 'The setup request budget was exceeded')
        headers = {'Accept': 'application/json', 'Origin': ORIGIN,
                   'Authorization': 'Emby Client="Goby AV User Setup", DeviceId="' + intent['device_id'] +
                                    '", Device="Linux fixture setup", Version="1.0"'}
        if authenticated:
            require(token is not None, 'Setup authentication is unavailable')
            headers['X-Emby-Token'] = token
        payload = json.dumps(body).encode() if body is not None else None
        if payload is not None:
            headers['Content-Type'] = 'application/json'
        connection = http.client.HTTPConnection('127.0.0.1', 18197, timeout=12)
        try:
            connection.request(method, route, payload, headers)
            response = connection.getresponse()
            raw = response.read((2 << 20) + 1)
            code = response.status
        finally:
            connection.close()
        pin()
        require(len(raw) <= 2 << 20, 'A setup response exceeded its limit')
        report['requests'].append({'label': label, 'method': method, 'status': code})
        try:
            data = json.loads(raw) if raw else None
        except ValueError:
            data = None
        return code, data

    try:
        code, public = request('public-identity', 'GET', '/emby/System/Info/Public', authenticated=False)
        require(code == 200 and public['Id'] == source['serverId'], 'Fresh reference server identity differs')
        administrator = source['accounts']['admin']
        code, login = request('admin-login', 'POST', '/emby/Users/AuthenticateByName',
                              {'Username': administrator['username'], 'Pw': administrator['password']}, authenticated=False)
        require(code == 200 and login['User']['Id'] == administrator['userId'] and login['ServerId'] == source['serverId'],
                'The fixture administrator identity differs')
        token = login['AccessToken']
        write(WORK / 'private-admin-session.json', {'marker': MARKER, 'access_token': token, 'user_id': administrator['userId']})
        code, before = request('users-before', 'GET', '/emby/Users')
        require(code == 200 and isinstance(before, list) and not any(row.get('Name') == NAME for row in before),
                'The fixed new account name already exists or inventory is unavailable')
        write(WORK / 'existing-account-baseline.json', snapshot(before))
        code, libraries = request('libraries', 'GET', '/emby/Library/VirtualFolders/Query')
        require(code == 200 and isinstance(libraries.get('Items'), list), 'Library inventory is unavailable')
        selected = []
        for name in ('Movies', 'Music'):
            matches = [row for row in libraries['Items'] if row.get('Name') == 'M3e Reference ' + name]
            require(len(matches) == 1 and matches[0].get('Locations') == ['/opt/goby-fixtures/client-m3e/' + name] and
                    isinstance(matches[0].get('ItemId'), str), 'The owned library scope differs')
            selected.append(matches[0]['ItemId'])
        write(WORK / 'create-intent.json', {'marker': MARKER, 'name_proven_absent': NAME, 'allowed_library_ids': selected})
        code, user = request('create-new-ordinary-user', 'POST', '/emby/Users/New', {'Name': NAME})
        require(code == 200 and user.get('Name') == NAME and user.get('Policy', {}).get('IsAdministrator') is False,
                'New account creation was not acknowledged as an ordinary user')
        identifier = user['Id']
        report['account_id'] = identifier
        write(WORK / 'created-account.json', {'marker': MARKER, 'account_id': identifier, 'username': NAME})
        code, _ = request('set-new-account-password', 'POST', '/emby/Users/' + quote(identifier) + '/Password',
                          {'Id': identifier, 'NewPw': intent['password'], 'ResetPassword': False})
        require(code in (200, 204), 'The new account password was not accepted')
        policy = dict(user['Policy'], IsAdministrator=False, IsDisabled=False, EnableAllFolders=False, EnabledFolders=selected,
                      EnableMediaPlayback=True, EnableAudioPlaybackTranscoding=True, EnableVideoPlaybackTranscoding=True,
                      EnablePlaybackRemuxing=True, EnableContentDeletion=False, EnableContentDownloading=False)
        code, _ = request('restrict-new-account-policy', 'POST', '/emby/Users/' + quote(identifier) + '/Policy', policy)
        require(code in (200, 204), 'The new account policy was not accepted')
        code, current = request('new-account-verify', 'GET', '/emby/Users/' + quote(identifier))
        require(code == 200 and current['Policy']['IsAdministrator'] is False and current['Policy']['IsDisabled'] is False and
                current['Policy']['EnableAllFolders'] is False and set(current['Policy']['EnabledFolders']) == set(selected),
                'The new account policy scope differs')
        code, items = request('new-account-media-baseline', 'GET', '/emby/Users/' + quote(identifier) +
                              '/Items?Recursive=true&IncludeItemTypes=Movie,Audio&Fields=Path,MediaSources,MediaStreams&Limit=100')
        require(code == 200 and len(items.get('Items', [])) == 3, 'The new account does not expose exactly three owned media leaves')
        expected = {'M3e Client Movie': '/opt/goby-fixtures/client-m3e/Movies/M3e Client Movie.mp4',
                    'M3e MP3': '/opt/goby-fixtures/client-m3e/Music/M3e Client Audio.mp3',
                    'M3e FLAC': '/opt/goby-fixtures/client-m3e/Music/M3e Client Audio.flac'}
        baseline = []
        for item in items['Items']:
            require(expected.get(item['Name']) == item.get('Path'), 'An item path left the owned synthetic fixture')
            code, detail = request('new-item-detail-baseline', 'GET', '/emby/Users/' + quote(identifier) + '/Items/' + quote(item['Id']))
            state = detail.get('UserData', {}) if isinstance(detail, dict) else {}
            require(code == 200 and state.get('PlaybackPositionTicks', 0) == 0 and state.get('PlayCount', 0) == 0 and
                    state.get('Played') is False and not state.get('LastPlayedDate'), 'The newly created account has unexpected media history')
            baseline.append({'id': item['Id'], 'name': item['Name'], 'path': item['Path'], 'user_data': state,
                             'streams': [{key: stream.get(key) for key in ('Index', 'Type', 'Codec', 'Language', 'IsExternal')}
                                         for stream in detail.get('MediaStreams', [])]})
        code, after = request('existing-users-unchanged', 'GET', '/emby/Users')
        require(code == 200 and snapshot([row for row in after if row['Id'] != identifier]) == snapshot(before),
                'An existing account configuration or policy changed')
        write(WORK / 'browser.json', {'marker': MARKER, 'base_url': ORIGIN, 'direct_url': 'http://127.0.0.1:18097',
                                     'admin': {'username': administrator['username'], 'password': administrator['password']},
                                     'viewer': {'username': NAME, 'password': intent['password']}})
        report['initial_media_state'] = baseline
        report['allowed_library_ids'] = selected
        report['complete'] = True
    finally:
        if token:
            code, _ = request('admin-logout', 'POST', '/emby/Sessions/Logout')
            report['admin_logout_status'] = code
            code, _ = request('admin-token-after-logout', 'GET', '/emby/System/Info')
            report['admin_after_logout_status'] = code
            require(report['admin_logout_status'] == 204 and code == 401, 'The setup administrator token was not retired')
        write(WORK / 'setup-report.json', report)
    print(json.dumps({'result': 'new_owned_ordinary_account_created', 'account_id': report['account_id'],
                      'report': str(WORK / 'setup-report.json'), 'browser': str(WORK / 'browser.json'),
                      'admin_logout': report['admin_logout_status'], 'admin_token_after_logout': report['admin_after_logout_status']}))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(json.dumps({'result': 'setup_stopped', 'error_type': type(error).__name__,
                          'retained_path': str(WORK)}))
        raise SystemExit(1) from None
