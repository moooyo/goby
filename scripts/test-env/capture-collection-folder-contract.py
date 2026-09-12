#!/usr/bin/env python3
"""Capture public CollectionFolder details in a separate bounded protocol scope.

This operator reads only owned Goby integrity inputs and synthetic credential
descriptors. Reference interaction is HTTP through the existing port-18197
proxy, whose own identity checks remain unchanged. No reference process,
executable, namespace, implementation, assets, or database is inspected here.
"""

from __future__ import annotations

import argparse
from collections import Counter
import datetime as dt
from decimal import Decimal
import fcntl
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import secrets
import signal
import stat
import subprocess
import sys
import time
import types

sys.dont_write_bytecode = True
WORK = Path('/opt/goby-test/exec-work-m3e')
ROOT = WORK / 'collection-folder-contract-v1'
TOOL = WORK / 'collection-folder-contract-tool-02'
MARKER = 'goby-collection-folder-contract-v1'
STATE = WORK / 'client-fixture.json'
STATE_SHA = 'd6b8872eab70848b834c50e26cc2b84e9d137dd68049a1be85d09afa811829d4'
BASELINE = WORK / 'client-library-changed-ui-source44-v1/after-full.json'
BASELINE_SHA = '1277a8034d738ed451cf99e71b88c53e31c9731576bca7fbae16085e573ce870'
PRIOR_TERMINAL = WORK / 'library-changed-execution-01/terminal.json'
PRIOR_TERMINAL_SHA = '7fc5c52d33c5ae4044da0a5fe761a23bbb4dbced9c82ca4bba1018e8fd802134'
PRIOR_ROOTS = (BASELINE.parent, PRIOR_TERMINAL.parent)
CREDENTIALS = WORK / 'browser.json'
CREDENTIALS_SHA = '0be6df4acef0565537b3b3a6e8c1518f6bed4023bfdfb5dea60b67dd32fee790'
REFERENCE_CREDENTIALS = WORK / 'reference-browser.json'
REFERENCE_CREDENTIALS_SHA = 'e72b02b97fb526d026056567d278ff4668f200f5acba25230d1ec46735d87d6d'
PROCESS = {'pid': 1264063, 'start_ticks': 11104222, 'boot_id': '6bdfc486-7bc8-412f-82b5-70095a09dde7'}
INVOCATION = 'c0a5244ae25646c8bd92c3e3c1636575'
BINARY_SHA = 'cd67f2e71ff1b63e3c138cdba1f9c9d1e584e2788b47964a332b38311cda0e2d'
PRIMARY_PROCESS = {'pid': 762090, 'start_ticks': 7637121, 'boot_id': PROCESS['boot_id']}
PRIMARY_INVOCATION = 'bb94d74b475f4382a6ec6f6df181dd74'
PRIMARY_BINARY_SHA = 'af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620'
PRIMARY_UNIT = 'goby-foundation-test.service'
PRIMARY_FILES = {
    '/opt/goby-dev/goby': PRIMARY_BINARY_SHA,
    '/etc/systemd/system/goby-foundation-test.service': '91a9b0e55baf053db25281236d0d7c8c055f895b093d4474d9c27e888610117d',
    '/etc/systemd/system/goby-foundation-test.service.d/20-application-keys.conf': 'ed9e8b91416918ce797ab3b5507d8843f62109d033cb30a679a44e3abbea3dd9',
    '/etc/systemd/system/goby-foundation-test.service.d/30-observability.conf': 'cc8fecb588840dd49848352936747a712d8790f8f7bd25d8d31a8a41069a46e5',
    '/etc/systemd/system/goby-foundation-test.service.d/40-backup-recovery.conf': 'e62ad56f0e93c1df2b89a6b5f7dbbcb032fa98d9d4cdcb8ebb04ba1e01b5682e',
    '/opt/goby-test/runtime.env': '2043e72115338d04775485dd63702c6084d36d09331ca5cdac66819152619607',
    '/opt/goby-test/recovery-m5j.env': '6847d34cfd9af7c53a8b16406db6f341f418b6ffdacb83bfe8c1f9bfd36b4c7c',
}
HELPERS = {
    'op': (WORK / 'prepare-client-fixture.py', '84d21e8ac0b48c5dfd3d2c7ae35b0aec0d7f4f811e65658081490aa44947b49c'),
    'profile': (WORK / 'client-special-features-profile-tool-01/client-special-features-profile.py', '6bbc0bfd17b1c7eef44028fe34481436d39a3e6645442340189e2da35c382252'),
    'media_reader': (WORK / 'client-extra-root-tool-02/extend-client-special-features-root.py', '7a9cda4a36ebbe9c2311031db1f6a57265eef8fc8807057a3d650ef6c16b2a46'),
}
TARGETS = {
    'candidate': {'port': 18198, 'server_id': 'c7cfd76b1dee728b2bad523793a37ccb',
        'user_id': 'ecbbe4cb82403879bc4b4f78894c5738', 'username': 'm3e-client-viewer',
        'library_name': 'M3e Client Movies', 'library_id': 'a9993591e72f0f2e7babcbf8b9c50790'},
    'reference': {'port': 18197, 'server_id': 'f56dec8ff7414847873064c4be9fba74',
        'user_id': '46c1e1006a2c47ada1b482a96e89c549', 'username': 'm3e-reference-viewer',
        'library_name': 'M3e Reference Movies', 'library_id': None},
}
VARIANT = '?Fields=LockedFields,DisplayPreferencesId,ItemCounts&EnableImages=false&EnableUserData=false'
TABLES = {'schema_migrations', 'server_settings', 'users', 'sessions', 'libraries', 'library_roots', 'items',
    'scan_jobs', 'catalog_entities', 'item_entities', 'item_images', 'user_item_data', 'play_sessions', 'item_subtitles',
    'encoding_jobs', 'client_playback_references', 'application_keys', 'application_key_clients', 'devices',
    'application_key_devices', 'task_definitions', 'task_triggers', 'task_runs', 'task_run_requests', 'task_occurrences',
    'task_run_children', 'item_metadata_state', 'user_settings', 'managed_settings', 'activity_entries',
    'theme_owner_ids', 'theme_reserved_paths', 'item_theme_resources', 'extra_reserved_paths', 'item_extra_resources'}
ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8'}
HASH = re.compile('[0-9a-f]{64}')
IDENTIFIER = re.compile('[A-Za-z0-9_-]{1,128}')
SECRET_KEY = re.compile('password|token|secret|authorization|cookie|api.?key|^pw$', re.I)
MAX_FILE = 64 << 20


class CaptureError(Exception):
    """A closed protocol or preservation boundary was not proven."""


def require(value, message):
    if not value:
        raise CaptureError(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def exact(value):
    if value is None: return 'null'
    if type(value) is bool: return 'true' if value else 'false'
    if type(value) is int or isinstance(value, Decimal):
        require(not isinstance(value, Decimal) or value.is_finite(), 'Nonfinite JSON is forbidden.')
        return str(value)
    if isinstance(value, str): return json.dumps(value, ensure_ascii=False)
    if isinstance(value, list): return '[' + ','.join(exact(item) for item in value) + ']'
    require(isinstance(value, dict) and all(isinstance(key, str) for key in value), 'Unsupported JSON shape.')
    return '{' + ','.join(exact(key) + ':' + exact(value[key]) for key in sorted(value)) + '}'


def canonical(value):
    return exact(value).encode('utf-8')


def same(left, right):
    return canonical(left) == canonical(right)


def decode(raw):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, 'Duplicate JSON keys are forbidden.')
            result[key] = value
        return result
    def invalid(_value):
        raise CaptureError('Nonfinite JSON is forbidden.')
    return json.loads(raw, object_pairs_hook=unique, parse_float=Decimal, parse_constant=invalid)


def instant(value):
    require(isinstance(value, str), 'A ledger timestamp is missing.')
    result = dt.datetime.fromisoformat(value.replace('Z', '+00:00'))
    require(result.tzinfo is not None, 'A ledger timestamp lacks its timezone.')
    return result


def sanitize(value, secret_values):
    if isinstance(value, dict):
        return {key: '[redacted]' if SECRET_KEY.search(key) else sanitize(item, secret_values) for key, item in value.items()}
    if isinstance(value, list): return [sanitize(item, secret_values) for item in value]
    if isinstance(value, str):
        for secret in sorted((entry for entry in secret_values if entry), key=len, reverse=True):
            value = value.replace(secret, '[redacted]')
        return re.sub(r'(?i)([?&](?:api_key|token|access_token|password)=)[^&\s"<>]+', r'\1[redacted]', value)
    return value


def allowed_request(target, method, path, *, cleanup=False, library_id=None):
    if target not in TARGETS or not isinstance(path, str): return False
    if cleanup:
        return (method, path) in (('POST', '/emby/Sessions/Logout'), ('GET', '/emby/System/Info'))
    user = '/emby/Users/' + TARGETS[target]['user_id']
    if (method, path) in (('GET', '/emby/System/Info/Public'), ('POST', '/emby/Users/AuthenticateByName'),
                          ('GET', user), ('GET', user + '/Views')): return True
    if method != 'GET' or not isinstance(library_id, str) or not IDENTIFIER.fullmatch(library_id): return False
    if target == 'candidate' and library_id != TARGETS[target]['library_id']: return False
    return path in (user + '/Items/' + library_id, user + '/Items/' + library_id + VARIANT)


def allowed_label(target,label,method,path,cleanup,library_id):
    user = '/emby/Users/' + TARGETS[target]['user_id']
    routes = {'public-info':('GET','/emby/System/Info/Public',False),
        'login':('POST','/emby/Users/AuthenticateByName',False),
        'user-before':('GET',user,False),'user-after':('GET',user,False),'views':('GET',user + '/Views',False),
        'logout':('POST','/emby/Sessions/Logout',True),'exact-token':('GET','/emby/System/Info',True)}
    if library_id is not None:
        routes.update({'detail-default':('GET',user + '/Items/' + library_id,False),
            'detail-switches':('GET',user + '/Items/' + library_id + VARIANT,False)})
    return routes.get(label) == (method,path,cleanup)


class Budget:
    def __init__(self, max_requests=20, max_response=1 << 20, max_total=8 << 20, deadline_seconds=12, clock=time.monotonic):
        require((max_requests, max_response, max_total, deadline_seconds) == (20, 1 << 20, 8 << 20, 12), 'HTTP bounds cannot be enlarged.')
        self.max_requests, self.max_response, self.max_total, self.deadline_seconds = max_requests, max_response, max_total, deadline_seconds
        self.clock, self.requests, self.total = clock, 0, 0

    def charge_request(self, cleanup=False):
        require(self.requests < (self.max_requests if cleanup else self.max_requests - 4), 'HTTP capacity reserved for exact cleanup is exhausted.')
        self.requests += 1
        return self.requests

    def charge_response(self, size):
        require(type(size) is int and 0 <= size <= self.max_response and self.total + size <= self.max_total, 'Response bytes exceed the approved limit.')
        self.total += size


def validate_login(payload, expected_server, user, device, forbidden_tokens=(), forbidden_devices=()):
    proof = validate_login_ownership(payload,expected_server,user,device,forbidden_tokens,forbidden_devices)
    account, session = payload['User'], payload['SessionInfo']
    require(account.get('Policy', {}).get('IsAdministrator') is False and account['Policy'].get('IsDisabled') is False and
        session.get('DeviceName') == device['device_name'] and session.get('Client') == device['client_name'] and
        session.get('ApplicationVersion') == device['client_version'], 'The owned login violates the ordinary actor contract.')
    return proof


def validate_login_ownership(payload, expected_server, user, device, forbidden_tokens=(), forbidden_devices=()):
    require(isinstance(payload, dict), 'Login acknowledgement is not an object.')
    token, account, session = payload.get('AccessToken'), payload.get('User'), payload.get('SessionInfo')
    require(isinstance(token, str) and 16 <= len(token) <= 8192 and not re.search(r'[\x00-\x20\x7f]', token), 'Login has no bounded usable token.')
    require(isinstance(account, dict) and isinstance(session, dict) and payload.get('ServerId') == expected_server and
        account.get('Id') == user['user_id'] and account.get('Name') == user['username'] and
        session.get('UserId') == user['user_id'] and isinstance(session.get('Id'), str) and IDENTIFIER.fullmatch(session['Id']) and
        session.get('DeviceId') == device['device_id'], 'The login does not prove the exact new actor and device nonce.')
    require(sha(token.encode()) not in forbidden_tokens and device['device_id'] not in forbidden_devices,
        'An existing credential or device was reused.')
    return {**device, 'session_id': session['Id'], 'user_id': user['user_id'], 'server_id': expected_server, 'token_sha256': sha(token.encode())}


def select_library(target, body):
    require(target in TARGETS and isinstance(body, dict) and isinstance(body.get('Items'), list) and
        type(body.get('TotalRecordCount')) is int and body['TotalRecordCount'] == len(body['Items']) and len(body['Items']) <= 64,
        'Views does not contain a complete bounded library inventory.')
    matches = [row for row in body['Items'] if isinstance(row, dict) and row.get('Name') == TARGETS[target]['library_name']]
    require(len(matches) == 1, 'The original Movies view is not uniquely identified.')
    row = matches[0]
    require(row.get('Type') == 'CollectionFolder' and row.get('IsFolder') is True and row.get('CollectionType') == 'movies' and
        row.get('ServerId') == TARGETS[target]['server_id'] and isinstance(row.get('Id'), str) and IDENTIFIER.fullmatch(row['Id']) and
        (target != 'candidate' or row['Id'] == TARGETS[target]['library_id']), 'The selected Movies view has another public identity.')
    return row['Id']


def compare_fixed_snapshot(before, after):
    require(set(before) == set(after), 'Complete snapshot keys changed.')
    for key in before:
        if key != 'database': require(same(before[key], after[key]), 'Private/schema/recovery binding changed.')
    left, right = before['database'], after['database']
    require(set(left) == set(right), 'Complete database keys changed.')
    for key in left:
        if key != 'metadata': require(same(left[key], right[key]), 'Full rows, schema or sequences changed before login.')
    require(set(left['metadata']) == set(right['metadata']) and instant(left['metadata']['captured_at']) <= instant(right['metadata']['captured_at']) and
        all(same(value, right['metadata'][key]) for key, value in left['metadata'].items() if key != 'captured_at'), 'Database identity or capture order changed.')


def validate_ledger(before, after, proof=None, closed=True):
    require(set(before) == set(after) and before.get('schema') == after.get('schema') == 27, 'Snapshot schema/shape changed.')
    for key in before:
        if key != 'database': require(same(before[key], after[key]), 'A private/runtime/recovery value changed.')
    left, right = before['database'], after['database']
    require(set(left) == set(right) and set(left['tables']) == set(right['tables']) == TABLES, 'The complete 35-table inventory changed.')
    for key in left:
        if key not in ('tables', 'sequences', 'metadata'): require(same(left[key], right[key]), 'The schema catalog changed.')
    require(set(left['metadata']) == set(right['metadata']) and all(same(value, right['metadata'][key])
        for key, value in left['metadata'].items() if key != 'captured_at'), 'Complete catalog metadata changed.')
    start, end = instant(left['metadata']['captured_at']), instant(right['metadata']['captured_at'])
    require(start <= end, 'Snapshot time moved backwards.')
    additions = {}
    for name, rows in left['tables'].items():
        current, columns = right['tables'][name], left['metadata']['columns'][name]
        require(isinstance(rows, list) and isinstance(current, list) and all(isinstance(row, dict) and set(row) == set(columns) for row in rows + current),
            'A complete table row lost or added columns: ' + name)
        old, new = Counter(canonical(row) for row in rows), Counter(canonical(row) for row in current)
        require(not old - new, 'An old row changed or disappeared: ' + name)
        extra = new - old
        additions[name] = [row for row in current if extra[canonical(row)] > 0]
        require(len({canonical(row) for row in additions[name]}) == len(additions[name]), 'Duplicate additions are forbidden.')
        if name not in ('sessions', 'devices', 'activity_entries'): require(not additions[name], 'An unrelated table acquired rows: ' + name)
    count = int(proof is not None)
    require(len(additions['sessions']) == len(additions['devices']) == count and len(additions['activity_entries']) == count * (2 if closed else 1),
        'Authentication, device or audit additions exceed the exact owned session.')
    if proof is not None:
        require(proof['user_id'] == TARGETS['candidate']['user_id'] and proof['server_id'] == TARGETS['candidate']['server_id'], 'Another actor owns the candidate delta.')
        row, device = additions['sessions'][0], additions['devices'][0]
        require(row['id'] == proof['session_id'] and row['user_id'] == proof['user_id'] and row['token_hash'] == '\\x' + proof['token_sha256'] and
            row['kind'] == 'emby' and row['client_capabilities'] == {} and row['device_registry_id'] == device['id'] and
            all(row[column] == proof[field] for column, field in (('device_id','device_id'),('device_name','device_name'),('client_name','client_name'),('client_version','client_version'))) and
            all(old['id'] != row['id'] and old['token_hash'] != row['token_hash'] for old in left['tables']['sessions']) and
            all(old['reported_device_id'] != proof['device_id'] for old in left['tables']['devices']), 'The new session is not the exact acknowledged ordinary login.')
        require(start <= instant(row['created_at']) <= instant(row['last_seen_at']) <= end and
            instant(row['expires_at']) == instant(row['created_at']) + dt.timedelta(days=30), 'The new session lifetime is invalid.')
        require((row['revoked_at'] is not None) is closed and (not closed or instant(row['created_at']) <= instant(row['revoked_at']) <= end), 'The owned session cleanup state differs.')
        require(type(device['id']) is int and type(device['revision']) is int and device['revision'] == 1 and
            device['reported_device_id'] == proof['device_id'] and device['reported_name'] == proof['device_name'] and
            device['app_name'] == proof['client_name'] and device['app_version'] == proof['client_version'] and
            device['last_user_id'] == proof['user_id'] and device['ip_address'] == '127.0.0.1' and device['custom_name'] is None and device['deleted_at'] is None and
            start <= instant(device['created_at']) <= instant(row['created_at']) and instant(device['created_at']) <= instant(device['last_seen_at']) <= end,
            'The device delta differs from the fresh recorder identity.')
        expected = Counter(('session.login', 'session.revoked') if closed else ('session.login',))
        require(Counter(value['action'] for value in additions['activity_entries']) == expected, 'The exact login/revocation audit pair is absent.')
        for audit in additions['activity_entries']:
            require(type(audit['id']) is int and type(audit['revision']) is int and audit['revision'] == 0 and
                type(audit['affected_count']) is int and audit['affected_count'] == 1 and audit['source'] == 'emby' and
                audit['severity'] == 'Info' and audit['actor_kind'] == 'user' and audit['actor_id'] == proof['user_id'] and
                audit['actor_credential_id'] == audit['resource_id'] == proof['session_id'] and audit['resource_kind'] == 'session' and
                audit['request_id'] == audit['state'] == '' and audit['changed_fields'] == [] and
                start <= instant(audit['created_at']) <= end, 'An audit row is not the exact owned authentication action.')
    require(set(left['sequences']) == set(right['sequences']), 'Sequence membership changed.')
    for name, initial in left['sequences'].items():
        final = right['sequences'][name]
        require(all(isinstance(value, dict) and set(value) == {'last_value','is_called'} and type(value['last_value']) is int and type(value['is_called']) is bool
            for value in (initial, final)), 'Sequence values changed JSON type or shape.')
        table = {'devices_id_seq':'devices', 'activity_entries_id_seq':'activity_entries'}.get(name)
        added = additions[table] if table else []
        if added:
            first = initial['last_value'] + int(initial['is_called'])
            require(same(final, {'last_value':first + len(added) - 1,'is_called':True}) and
                sorted(row['id'] for row in added) == list(range(first, first + len(added))), 'Sequence growth lacks exact contiguous owned rows.')
        else: require(same(initial, final), 'An unrelated sequence changed.')
    return {'new_sessions':count, 'new_devices':count, 'new_audits':count * (2 if closed else 1),
        'old_rows_sequences_private_preserved':True, 'owned_sessions_closed':closed}


def file_identity(info):
    return {'device':info.st_dev, 'inode':info.st_ino, 'links':info.st_nlink, 'bytes':info.st_size,
        'mode':stat.S_IMODE(info.st_mode), 'uid':info.st_uid, 'gid':info.st_gid,
        'mtime_ns':info.st_mtime_ns, 'ctime_ns':info.st_ctime_ns}


def protected(path, expected=None, *, modes=(0o600,), limit=MAX_FILE):
    path = Path(path)
    require(path.is_absolute() and '..' not in path.parts, 'An input path is not canonical.')
    for parent in reversed(path.parents):
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o022, 'An input ancestor is not protected.')
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == info.st_gid == 0 and info.st_nlink == 1 and
        stat.S_IMODE(info.st_mode) in modes and 0 <= info.st_size <= limit, 'An input is not an owned bounded regular file.')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, 'rb') as stream:
        require(file_identity(os.fstat(stream.fileno())) == file_identity(info), 'An input changed while opening.')
        raw = stream.read(limit + 1)
        require(len(raw) == info.st_size and file_identity(os.fstat(stream.fileno())) == file_identity(info) and
            file_identity(path.lstat()) == file_identity(info), 'An input changed while reading.')
    require(expected is None or HASH.fullmatch(expected) and sha(raw) == expected, 'An input hash differs from its authority.')
    return raw


def preserved_tree(root):
    root = Path(root)
    initial = root.lstat()
    require(stat.S_ISDIR(initial.st_mode) and initial.st_uid == initial.st_gid == 0 and not initial.st_mode & 0o077,
        'A consumed scope is not an owned private directory.')
    values, total = {}, 0
    for path in sorted(root.rglob('*')):
        require(len(values) < 4096, 'The consumed evidence tree exceeds its bound.')
        info = path.lstat()
        require(info.st_uid == info.st_gid == 0 and not stat.S_ISLNK(info.st_mode), 'A consumed scope contains an unowned or linked entry.')
        row = file_identity(info)
        if stat.S_ISREG(info.st_mode):
            raw = protected(path, modes=(0o600, 0o644), limit=MAX_FILE)
            total += len(raw)
            require(total <= 128 << 20, 'Consumed evidence bytes exceed the observation bound.')
            row['sha256'] = sha(raw)
        else: require(stat.S_ISDIR(info.st_mode) and not info.st_mode & 0o022, 'A consumed scope contains a special entry.')
        values[str(path.relative_to(root))] = row
    require(file_identity(root.lstat()) == file_identity(initial), 'A consumed directory changed while observed.')
    return {'root':file_identity(initial), 'entries':values}


def quiescent(snapshot):
    tables = snapshot['database']['tables']
    require(snapshot.get('schema') == 27 and set(tables) == TABLES and not tables['client_playback_references'] and not tables['encoding_jobs'],
        'The candidate is not an idle complete schema27 catalog.')
    for table, field in (('scan_jobs','status'),('task_runs','state'),('task_run_children','state')):
        require(not any(str(row[field]).lower() in ('waiting','pending','queued','running','stopping') for row in tables[table]), 'A scan or task is active.')
    sessions = {row['id']:row for row in tables['sessions']}
    for row in tables['play_sessions']:
        require(row['state'] in ('Prepared','Stopped','Expired'), 'Playback is active or unrecognized.')
        if row['state'] == 'Prepared':
            owner = sessions.get(row['auth_session_id'], {})
            require(row['started_at'] is None and row['counted'] is False and row['application_client_id'] is None and
                owner.get('kind') == 'emby' and owner.get('user_id') == row['user_id'] and owner.get('revoked_at') is not None,
                'Prepared history is not bound to an already revoked ordinary session.')


class Actor:
    def __init__(self, run, target, credential, device):
        require(target in TARGETS, 'Unknown capture target.')
        self.run, self.target, self.credential, self.device = run, target, credential, device
        self.token, self.proof, self.owned_proof, self.library_id = None, None, None, None
        self.login_sent = self.logout_sent = self.exact_sent = self.closed = False

    def acknowledge_ownership(self,response):
        """Keep complete owned acknowledgements before fallible journal writes."""
        payload = response.get('body')
        if response['complete'] and isinstance(payload, dict) and isinstance(payload.get('AccessToken'), str) and payload['AccessToken']:
            require(self.token is None or self.token == payload['AccessToken'], 'The acknowledged token changed.')
            self.token = payload['AccessToken']
            self.run.secret_values.add(self.token)
        require(response['complete'] and response['status'] == 200, 'Login lacks a complete successful acknowledgement.')
        old = self.run.before['database']['tables']
        forbidden_tokens = [row['token_hash'].removeprefix('\\x') for row in old['sessions']] if self.target == 'candidate' else []
        forbidden_devices = [row['reported_device_id'] for row in old['devices']] if self.target == 'candidate' else []
        proof = validate_login_ownership(payload, TARGETS[self.target]['server_id'], TARGETS[self.target], self.device, forbidden_tokens, forbidden_devices)
        require(self.owned_proof is None or same(self.owned_proof,proof), 'The acknowledged session ownership changed.')
        self.owned_proof = proof

    def login(self):
        require(not self.login_sent and self.token is None, 'Login cannot be replayed.')
        self.login_sent = True
        response = self.run.request(self, 'login', 'POST', '/emby/Users/AuthenticateByName',
            body={'Username':self.credential['username'], 'Pw':self.credential['password']})
        self.acknowledge_ownership(response)
        payload = response['body']
        self.run.save(self.target + '-owned-login-proof.json',self.owned_proof)
        self.proof = validate_login(payload, TARGETS[self.target]['server_id'], TARGETS[self.target], self.device)
        require(all(other is self or other.token is None or other.token != self.token for other in self.run.actors), 'Two endpoints returned the same credential.')
        self.run.save(self.target + '-login-proof.json', self.proof)

    def cleanup(self):
        if self.logout_sent or self.exact_sent:
            return
        if self.owned_proof is None:
            require(self.token is None, 'An unproven credential remains in its private acknowledgement for review.')
            return
        acknowledgement = exact_response = None
        try:
            self.logout_sent = True
            acknowledgement = self.run.request(self, 'logout', 'POST', '/emby/Sessions/Logout', cleanup=True)
        finally:
            # A lost logout acknowledgement does not license replay. Probe the
            # same token independently once and preserve both physical outcomes.
            self.exact_sent = True
            exact_response = self.run.request(self, 'exact-token', 'GET', '/emby/System/Info', cleanup=True)
            self.closed = bool(acknowledgement and acknowledgement['complete'] and acknowledgement['status'] == 204 and
                exact_response['complete'] and exact_response['status'] == 401)
        require(self.closed, 'Exact owned logout204 and token401 cleanup is incomplete.')


class Run:
    def __init__(self, args):
        self.args, self.budget = args, Budget()
        self.records, self.errors, self.labels, self.secret_values = {}, [], set(), set()
        self.actors, self.lock, self.root_fd = [], None, None
        self.before = self.after = self.ledger = None
        self.media = self.primary = self.prior = None
        self.started = time.monotonic()

    def error(self, stage, error):
        self.errors.append({'stage':stage, 'failure_type':type(error).__name__,
            'reason':str(error) if type(error) is CaptureError else 'The bounded operation failed.'})

    def command(self, arguments, text=None, environment=None, timeout=25):
        values = [str(value) for value in arguments]
        allowed = values == ['/usr/bin/ss','-H','-ltnp','sport = :18198'] or (len(values) == 5 and
            values[:3] == ['/usr/bin/systemctl','show','goby-client-m3e.service'] and values[3] == '--no-pager' and values[4].startswith('--property='))
        pg = ['/usr/sbin/runuser','-u','postgres','--','/usr/lib/postgresql/17/bin/psql','-X','--no-password',
            '-h','/var/lib/postgresql/goby-workspace-v1/socket','-p','15432','-U','postgres','-d']
        is_pg = len(values) == 20 and values[:14] == pg and values[14] in ('postgres','goby_client_m3e') and values[15:] == ['-v','ON_ERROR_STOP=1','-A','-t','-q']
        if is_pg:
            require(isinstance(text, str) and text.lstrip().startswith(('SELECT ', 'BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;')), 'A helper requested non-read-only SQL.')
        media = (len(values) == 7 and values[:6] == ['/usr/sbin/runuser','-u','goby','--','/usr/bin/test','-r'] and
            Path(values[6]).is_relative_to(Path('/opt/goby-fixtures/client-m3e')) and '..' not in Path(values[6]).parts)
        require((allowed or is_pg or media) and environment is None, 'A helper requested an unapproved command or environment.')
        env = dict(ENV)
        if is_pg: env['PGOPTIONS'] = '-c default_transaction_read_only=on -c statement_timeout=20000 -c lock_timeout=5000'
        result = subprocess.run(values, input=text, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            timeout=min(timeout,25), check=False, env=env)
        require(result.returncode == 0 and len(result.stdout.encode()) <= MAX_FILE, 'An owned read-only integrity command failed.')
        return result.stdout.strip()

    def properties(self, unit):
        require(unit in ('goby-client-m3e.service', PRIMARY_UNIT), 'An unrelated service cannot be inspected.')
        fields = 'Id,LoadState,ActiveState,SubState,MainPID,InvocationID,ControlGroup,User,Group'
        result = subprocess.run(['/usr/bin/systemctl','show',unit,'--no-pager','--property=' + fields],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=10, check=False, env=ENV)
        require(result.returncode == 0 and len(result.stdout) <= 32768, 'Owned service identity is unavailable.')
        return dict(line.split('=',1) for line in result.stdout.splitlines() if '=' in line)

    def primary_fact(self):
        props = self.properties(PRIMARY_UNIT)
        require(props.get('MainPID') == str(PRIMARY_PROCESS['pid']) and props.get('InvocationID') == PRIMARY_INVOCATION and
            props.get('ActiveState') == 'active' and props.get('SubState') == 'running' and
            props.get('ControlGroup') == '/system.slice/' + PRIMARY_UNIT and props.get('User') == props.get('Group') == 'goby', 'The protected primary identity changed.')
        process = Path('/proc') / str(PRIMARY_PROCESS['pid'])
        require(self.op.process_identity(PRIMARY_PROCESS['pid']) == PRIMARY_PROCESS and process.stat().st_uid == 995 and
            os.readlink(process / 'exe') == '/opt/goby-dev/goby' and sha((process / 'exe').read_bytes()) == PRIMARY_BINARY_SHA and
            (process / 'cgroup').read_text().strip() == '0::/system.slice/' + PRIMARY_UNIT, 'The protected Goby executable membership changed.')
        files = {}
        for filename, digest in PRIMARY_FILES.items():
            protected(Path(filename), digest, modes=(0o600,0o644,0o755))
            files[filename] = {'sha256':digest, 'identity':file_identity(Path(filename).lstat())}
        require(sha((process / 'cmdline').read_bytes()) == 'c2c8b1f234839029e4dcd80ada8d765fa6d4ebcb21be2f4b72665945d9458d6e' and
            sha((process / 'environ').read_bytes()) == 'cfa0a59530f5e302b8ea23bb011823d7e68126be05e8b19c6360e450cbee9186' and
            self.op.process_identity(PRIMARY_PROCESS['pid']) == PRIMARY_PROCESS, 'The primary command or environment changed.')
        return {'properties':props, 'process':PRIMARY_PROCESS, 'files':files}

    def check(self):
        require(time.monotonic() - self.started <= 540, 'The complete capture window expired.')
        for path, expected in self.pins.items(): protected(Path(path), expected, modes=(0o600,0o644))
        require(self.op.verify_service(self.state) == PROCESS, 'The source44 candidate process changed.')
        require(self.properties('goby-client-m3e.service').get('InvocationID') == INVOCATION, 'The candidate invocation changed.')
        require(same(self.primary_fact(), self.primary), 'The protected primary changed during capture.')
        if self.root_fd is not None:
            info, opened = ROOT.lstat(), os.fstat(self.root_fd)
            require((info.st_dev,info.st_ino) == (opened.st_dev,opened.st_ino) and stat.S_ISDIR(info.st_mode) and
                info.st_uid == info.st_gid == 0 and stat.S_IMODE(info.st_mode) == 0o700, 'The new private evidence root changed.')

    def cleanup_identity(self,actor):
        require(actor in self.actors and actor.target in TARGETS, 'Cleanup has no owned target.')
        if actor.target == 'reference':
            # The approved existing proxy retains its own identity checks. This
            # recorder never reads a reference process or executable itself.
            return
        require(self.op.verify_service(self.state) == PROCESS, 'The candidate cleanup process, binary or listener changed.')
        props = self.properties('goby-client-m3e.service')
        require(props.get('MainPID') == str(PROCESS['pid']) and props.get('InvocationID') == INVOCATION and
            props.get('ActiveState') == 'active' and props.get('SubState') == 'running', 'The candidate cleanup invocation changed.')

    def snapshot(self):
        value = self.op.preservation_snapshot(self.state,27)
        self.profile.validate_structure(self.op,value,self.state)
        quiescent(value)
        return value

    def load(self):
        require(sys.platform == 'linux' and os.getuid() == os.geteuid() == os.getgid() == os.getegid() == 0 and os.environ.get('SSH_CONNECTION'), 'Use authorized remote root SSH only.')
        require(Path(__file__).absolute() == TOOL / 'capture-collection-folder-contract.py' and HASH.fullmatch(self.args.script_sha256), 'The tool requires its exact new source path and hash.')
        protected(Path(__file__).absolute(),self.args.script_sha256,modes=(0o600,0o644),limit=2 << 20)
        self.pins = {str(Path(__file__).absolute()):self.args.script_sha256, str(STATE):STATE_SHA, str(BASELINE):BASELINE_SHA,
            str(PRIOR_TERMINAL):PRIOR_TERMINAL_SHA, str(CREDENTIALS):CREDENTIALS_SHA, str(REFERENCE_CREDENTIALS):REFERENCE_CREDENTIALS_SHA,
            **{str(path):digest for path,digest in HELPERS.values()}}
        for name,(path,digest) in HELPERS.items():
            raw = protected(path,digest,modes=(0o600,0o644),limit=2 << 20)
            module = types.ModuleType('collection_folder_owned_' + name)
            module.__file__ = str(path)
            exec(compile(raw,str(path),'exec'),module.__dict__)
            setattr(self,name,module)
        self.op.command = self.command
        self.state = decode(protected(STATE,STATE_SHA))
        require(self.state.get('schema') == 27 and self.state.get('phase') == 'ready' and self.state.get('stage') == 'complete' and
            self.state.get('marker') == 'goby-m3e-client-acceptance-v1' and self.state.get('work') == str(WORK) and
            self.state.get('server_id') == TARGETS['candidate']['server_id'] and same(self.state.get('process'),PROCESS) and
            self.state.get('binary_sha256') == BINARY_SHA and self.state.get('browser_sha256') == CREDENTIALS_SHA and
            self.state.get('viewer_id') == TARGETS['candidate']['user_id'], 'The source44 candidate state identity differs.')
        prior = decode(protected(PRIOR_TERMINAL,PRIOR_TERMINAL_SHA))
        require(prior.get('marker') == 'goby-client-library-changed-failed-terminal-v1' and prior.get('status') == 'failed' and
            prior.get('phase') == 'discovery' and prior.get('browser_sessions_closed') is True and prior.get('normal_ui_logout_and_exact401') is True and
            prior.get('native_requests') == prior.get('metadata_writes') == 0 and prior.get('media_preserved') is True and prior.get('primary_preserved') is True,
            'The consumed failed UI scope does not prove its closed ordinary session and no catalog writes.')
        require(isinstance(prior.get('states'),list) and len(prior['states']) == 2 and all(
            row.get('recursive_cgroup_empty') is True and row.get('properties',{}).get('MainPID') == '0' and
            row['properties'].get('ControlGroup') == '' and row['properties'].get('Result') == 'exit-code' and
            row['properties'].get('ExecMainStatus') == '1' for row in prior['states']) and
            prior.get('evidence_sha256',{}).get('after-full.json') == BASELINE_SHA and
            prior.get('ledger',{}).get('old_rows_sequences_private_preserved') is True and prior['ledger'].get('owned_sessions_closed') is True,
            'The consumed scope terminal or exact after-snapshot binding is incomplete.')
        self.lock = os.open(self.op.LOCK,os.O_RDONLY | os.O_NOFOLLOW)
        require(self.op.identity(os.fstat(self.lock)) == self.op.identity(self.op.regular(self.op.LOCK)), 'The existing Goby fixture lock changed.')
        fcntl.flock(self.lock,fcntl.LOCK_EX | fcntl.LOCK_NB)
        require(not os.path.lexists(ROOT), 'This one-shot capture already exists; replay and adoption are forbidden.')
        self.primary = self.primary_fact()
        self.check()
        self.prior = {str(path):preserved_tree(path) for path in PRIOR_ROOTS}
        self.media = self.media_reader.media_witness(self.op,self.state)
        self.before = self.snapshot()
        compare_fixed_snapshot(decode(protected(BASELINE,BASELINE_SHA)),self.before)
        tables = self.before['database']['tables']
        require(tuple(len(tables[name]) for name in ('sessions','devices','activity_entries')) == (74,63,165), 'The new before-snapshot differs from the post-attempt authority.')
        candidate = decode(protected(CREDENTIALS,CREDENTIALS_SHA))
        reference = decode(protected(REFERENCE_CREDENTIALS,REFERENCE_CREDENTIALS_SHA))
        require(candidate.get('marker') == 'goby-m3e-client-acceptance-v1' and candidate.get('base_url') == 'http://127.0.0.1:18196' and
            candidate.get('direct_url') == 'http://127.0.0.1:18198' and set(candidate.get('viewer',{})) == {'username','password'} and
            candidate['viewer']['username'] == TARGETS['candidate']['username'] and re.fullmatch('[0-9a-f]{48}',candidate['viewer']['password']), 'The candidate viewer descriptor differs.')
        require(set(reference) == {'accounts','marker','password','port','schemaVersion','serverId','unit','userId','username'} and
            reference['marker'] == 'goby-emby-client-reference-m3e-v1' and reference['serverId'] == TARGETS['reference']['server_id'] and
            reference['port'] == 18097 and reference['schemaVersion'] == 1 and reference['unit'] == 'goby-emby-client-m3e.service' and
            set(reference['accounts']) == {'admin','viewer','viewer2'} and
            all(set(row) == {'username','password','userId'} for row in reference['accounts'].values()) and
            same({key:reference[key] for key in ('username','password','userId')},reference['accounts']['viewer']) and
            reference['username'] == TARGETS['reference']['username'] and reference['userId'] == TARGETS['reference']['user_id'] and
            re.fullmatch('[0-9a-f]{64}',reference['password']), 'The owned reference ordinary-user descriptor differs.')
        user = [row for row in tables['users'] if row['id'] == TARGETS['candidate']['user_id']]
        require(len(user) == 1 and user[0]['name'] == TARGETS['candidate']['username'] and user[0]['is_administrator'] is False and user[0]['is_disabled'] is False,
            'The candidate ordinary actor changed current role or identity.')
        self.credentials = {'candidate':candidate['viewer'], 'reference':{key:reference[key] for key in ('username','password')}}
        self.secret_values = {entry['password'] for entry in self.credentials.values()}

    def check_only(self):
        self.load()
        return {'marker':MARKER, 'status':'preflight_passed', 'http_requests':0, 'evidence_created':False, 'before_authority_sha256':BASELINE_SHA}

    def save(self,name,value):
        require(self.root_fd is not None and re.fullmatch(r'[a-z][a-z0-9-]*\.(json|bin)',name), 'A private evidence name is outside the new scope.')
        info = ROOT.lstat()
        require((info.st_dev,info.st_ino) == (os.fstat(self.root_fd).st_dev,os.fstat(self.root_fd).st_ino), 'The evidence root was replaced.')
        raw = value if isinstance(value,bytes) else canonical(value) + b'\n'
        require(len(raw) <= MAX_FILE, 'A private evidence artifact exceeds its bound.')
        fd = os.open(name,os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_WRONLY,0o600,dir_fd=self.root_fd)
        with os.fdopen(fd,'wb') as stream:
            stream.write(raw); stream.flush(); os.fsync(stream.fileno())
        os.fsync(self.root_fd)
        require(protected(ROOT / name) == raw, 'Published private evidence changed.')
        self.records[name] = {'path':str(ROOT / name), 'sha256':sha(raw)}
        return self.records[name]

    def prepare(self):
        os.umask(0o077)
        ROOT.mkdir(mode=0o700)
        self.root_fd = os.open(ROOT,os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        parent = os.open(WORK,os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try: os.fsync(parent)
        finally: os.close(parent)
        self.save('intent.json',{'marker':MARKER,'sources':self.pins,'candidate_before_authority':{'path':str(BASELINE),'sha256':BASELINE_SHA},
            'scope':'One new ordinary HTTP session per endpoint; complete public CollectionFolder details; no catalog writes.',
            'reference_transport':'Existing port-18197 proxy; its internal process/executable identity checks remain unchanged.',
            'reference_implementation_or_database_read':False,'old_ui_attempt_replayed':False,'max_requests':20,'max_response_bytes':1 << 20,'max_total_bytes':8 << 20})
        self.save('before-full.json',self.before)
        self.save('primary-before.json',self.primary)
        self.save('media-before.json',self.media)
        self.save('consumed-scopes-before.json',self.prior)
        old_devices = {row['reported_device_id'] for row in self.before['database']['tables']['devices']}
        for target in TARGETS:
            device = {'device_id':'goby-collection-folder-' + target + '-' + secrets.token_hex(16), 'device_name':'HeadlessChrome Linux',
                'client_name':'Emby Web', 'client_version':'4.9.5.0'}
            require(device['device_id'] not in old_devices, 'A newly generated device ID collided with retained history.')
            old_devices.add(device['device_id'])
            self.actors.append(Actor(self,target,self.credentials[target],device))
        self.save('fresh-devices.json',{actor.target:actor.device for actor in self.actors})

    def request(self,actor,label,method,path,body=None,cleanup=False):
        require(actor in self.actors and allowed_request(actor.target,method,path,cleanup=cleanup,library_id=actor.library_id), 'HTTP route or method is outside the exact public allowlist.')
        require(allowed_label(actor.target,label,method,path,cleanup,actor.library_id), 'A request label does not own this exact exchange.')
        login = method == 'POST' and path == '/emby/Users/AuthenticateByName'
        require(body == {'Username':actor.credential['username'],'Pw':actor.credential['password']} if login else body is None,
            'Only the owned login may contain a request body.')
        require(login or path == '/emby/System/Info/Public' or (actor.owned_proof is not None if cleanup else actor.proof is not None),
            'A protected read lacks an owned actor.')
        key = actor.target + '-' + label
        require(re.fullmatch('[a-z][a-z0-9-]*',key) and key not in self.labels, 'A physical request label cannot be replayed.')
        if cleanup: self.cleanup_identity(actor)
        else: self.check()
        sequence = self.budget.charge_request(cleanup)
        self.labels.add(key)
        self.save(key + '-intent.json',{'marker':MARKER,'sequence':sequence,'target':actor.target,'host':'127.0.0.1',
            'port':TARGETS[actor.target]['port'],'method':method,'path':path,'cleanup':cleanup,'login_body_retained':False,
            'credential_fingerprint':sha(actor.token.encode()) if actor.token else None})
        headers = {'Accept':'application/json','Accept-Encoding':'identity','Connection':'close',
            'Authorization':'Emby Client="{client_name}", Device="{device_name}", DeviceId="{device_id}", Version="{client_version}"'.format(**actor.device)}
        if actor.owned_proof is not None: headers['X-Emby-Token'] = actor.token
        if body is not None: headers['Content-Type'] = 'application/json'
        result = {'status':None,'complete':False,'body':None,'content_type':None,'bytes':0,'sha256':None}
        raw, connection = b'', None
        started = self.budget.clock()
        previous = signal.getsignal(signal.SIGALRM)
        signal.signal(signal.SIGALRM,lambda *_: (_ for _ in ()).throw(CaptureError('The absolute HTTP deadline expired.')))
        signal.setitimer(signal.ITIMER_REAL,self.budget.deadline_seconds)
        try:
            connection = http.client.HTTPConnection('127.0.0.1',TARGETS[actor.target]['port'],timeout=8)
            connection.request(method,path,None if body is None else canonical(body),headers)
            response = connection.getresponse()
            result['status'], result['content_type'] = response.status, response.getheader('Content-Type') or ''
            length = response.getheader('Content-Length')
            require(length is None or length.isdecimal() and int(length) <= self.budget.max_response, 'The declared HTTP response is excessive.')
            raw = response.read(self.budget.max_response + 1)
            self.budget.charge_response(len(raw))
            require((length is None or len(raw) == int(length)) and self.budget.clock() - started <= self.budget.deadline_seconds,
                'The response is incomplete or exceeded its absolute deadline.')
            result.update(bytes=len(raw),sha256=sha(raw))
            if raw:
                result['body'] = decode(raw) if result['content_type'].lower().split(';')[0].strip() == 'application/json' else raw.decode('utf-8',errors='replace')
            result['complete'] = True
        except Exception as error:
            result['failure_type'] = type(error).__name__
        finally:
            signal.setitimer(signal.ITIMER_REAL,0)
            signal.signal(signal.SIGALRM,previous)
            if connection is not None: connection.close()
            result['elapsed_ms'] = int((self.budget.clock() - started) * 1000)
            # These raw bytes stay private, including a login acknowledgement
            # received before actor identity checks. They are never printed.
            if isinstance(result['body'],dict) and isinstance(result['body'].get('AccessToken'),str):
                self.secret_values.add(result['body']['AccessToken'])
            if login and result['complete'] and result['status'] == 200:
                try:
                    # Save the minimum in-memory cleanup authority before any
                    # raw/response journal can fail. A later contract failure
                    # remains a failed capture even after successful cleanup.
                    actor.acknowledge_ownership(result)
                except Exception as error:
                    result['ownership_failure_type'] = type(error).__name__
            self.save(key + '-raw.bin',raw)
            self.save(key + '-response.json',result)
        return result

    def get(self,actor,label,path):
        response = self.request(actor,label,'GET',path)
        require(response['complete'] and response['status'] == 200 and isinstance(response['body'],dict), 'A required public API response is incomplete or unsuccessful.')
        return response['body']

    def capture_actor(self,actor):
        target = TARGETS[actor.target]
        public = self.get(actor,'public-info','/emby/System/Info/Public')
        require(public.get('Id') == target['server_id'] and public.get('Version') == '4.9.5.0', 'The public server identity differs from its intended endpoint.')
        actor.login()
        user_path = '/emby/Users/' + target['user_id']
        before = self.get(actor,'user-before',user_path)
        require(before.get('Id') == target['user_id'] and before.get('Name') == target['username'] and
            isinstance(before.get('Policy'),dict) and isinstance(before.get('Configuration'),dict) and
            before['Policy'].get('IsAdministrator') is False and before['Policy'].get('IsDisabled') is False, 'The current ordinary-user policy is unproven.')
        views = self.get(actor,'views',user_path + '/Views')
        actor.library_id = select_library(actor.target,views)
        path = user_path + '/Items/' + actor.library_id
        detail = self.get(actor,'detail-default',path)
        variant = self.get(actor,'detail-switches',path + VARIANT)
        for value in (detail,variant):
            require(value.get('Id') == actor.library_id and value.get('Type') == 'CollectionFolder' and value.get('IsFolder') is True and
                value.get('Name') == target['library_name'] and value.get('CollectionType') == 'movies' and value.get('ServerId') == target['server_id'],
                'Direct detail changed the publicly selected original Movies identity.')
        after = self.get(actor,'user-after',user_path)
        require(after.get('Id') == target['user_id'] and all(same(before[key],after.get(key)) for key in ('Policy','Configuration')),
            'Public viewer configuration or policy changed during DTO research.')
        self.save(actor.target + '-projection.json',sanitize({'server_id':target['server_id'],'user_id':target['user_id'],
            'library_id':actor.library_id,'views':views,'detail_default':detail,'detail_switches':variant,
            'user_configuration_policy_preserved':True},self.secret_values))

    def execute(self):
        self.load()
        self.prepare()
        try:
            for actor in self.actors: self.capture_actor(actor)
        except Exception as error: self.error('capture',error)
        finally:
            for actor in self.actors:
                try: actor.cleanup()
                except Exception as error: self.error(actor.target + '-cleanup',error)
            try:
                self.after = self.snapshot()
                self.save('after-full.json',self.after)
                candidate = next(actor for actor in self.actors if actor.target == 'candidate')
                self.ledger = validate_ledger(self.before,self.after,candidate.owned_proof,closed=True)
                self.save('candidate-ledger.json',self.ledger)
            except Exception as error: self.error('candidate-ledger',error)
            for label,current,baseline in (
                    ('media',lambda:self.media_reader.media_witness(self.op,self.state),self.media),
                    ('primary',self.primary_fact,self.primary),
                    ('consumed-scopes',lambda:{str(path):preserved_tree(path) for path in PRIOR_ROOTS},self.prior)):
                try:
                    observed = current()
                    self.save(label + '-after.json',observed)
                    require(same(observed,baseline),'Protected ' + label + ' changed.')
                except Exception as error: self.error(label + '-preservation',error)
            try: self.check()
            except Exception as error: self.error('final-integrity',error)
        complete = not self.errors and self.ledger is not None and all(actor.closed for actor in self.actors)
        report = {'marker':MARKER,'status':'passed' if complete else 'retained_for_review','protocol_research':True,'client_acceptance':False,
            'http_requests':self.budget.requests,'response_bytes':self.budget.total,'candidate_before_sha256':BASELINE_SHA,
            'candidate_ledger':self.ledger,'reference_database_preservation_claimed':False,
            'reference_transport':'Existing public loopback proxy; its internal identity checks were not modified or reimplemented.',
            'reference_program_process_namespace_or_database_read_by_operator':False,'old_ui_attempt_replayed':False,
            'client_headers':'Observed Emby Web 4.9.5.0 / HeadlessChrome Linux strings with fresh per-endpoint device IDs.',
            'actual_browser_used':False,'capabilities_full_sent':False,'complete_browser_profile_equivalence_claimed':False,
            'catalog_writes':0,'authentication':{actor.target:{'owned_identity_proven':actor.owned_proof is not None,'ordinary_contract_proven':actor.proof is not None,
                'session_id':actor.owned_proof['session_id'] if actor.owned_proof else None,'logout_attempted':actor.logout_sent,'exact_token_attempted':actor.exact_sent,
                'logout204_exact401':actor.closed} for actor in self.actors},'errors':self.errors,'evidence':dict(self.records)}
        self.save('report.json',sanitize(report,self.secret_values))
        return {'marker':MARKER,'status':report['status'],'report':str(ROOT / 'report.json'),'http_requests':self.budget.requests}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--script-sha256',required=True)
    parser.add_argument('--check-only',action='store_true')
    args = parser.parse_args()
    run = Run(args)
    try:
        result = run.check_only() if args.check_only else run.execute()
        print(exact(result))
        return 0 if result['status'] in ('passed','preflight_passed') else 1
    finally:
        if run.root_fd is not None: os.close(run.root_fd)
        if run.lock is not None: os.close(run.lock)


if __name__ == '__main__':
    try: sys.exit(main())
    except Exception as error:
        print(json.dumps({'marker':MARKER,'status':'failed','failure_type':type(error).__name__}),file=sys.stderr)
        sys.exit(1)
