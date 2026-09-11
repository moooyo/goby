#!/usr/bin/env python3
"""Capture LibraryChanged around three changes in one new owned reference library.

This is bounded protocol research, not original-client acceptance. The existing
seven libraries, five accounts and three media trees are immutable. Setup and
each change are separately recorded; unknown mutation outcomes are never retried.
No original implementation source or database is read.
"""

from __future__ import annotations

import argparse
import copy
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
from urllib.parse import urlencode, parse_qs

sys.dont_write_bytecode = True
W = Path('/opt/goby-test/exec-work-m3e')
ROOT = W / 'reference-library-changed-v1'
MEDIA = Path('/opt/goby-fixtures/client-library-changed-v1')
MOVIES = MEDIA / 'Movies'
QUARANTINE = MEDIA / 'quarantine'
MARKER = 'goby-reference-library-changed-v1'
LIBRARY_NAME = 'M3e Controlled LibraryChanged Movies'
VIEWER_NAME = 'm3e-library-changed-viewer-v1'
OPERATOR = W / 'prepare-client-reference.py'
OPERATOR_SHA = '29f159a2580e178dce65b6c23ef14ad9875971240e534546eb5e8a14ce88c160'
TRANSPORT_SHA = '54fd18d50cf254aa6e93ac587f0d8fdf17d6897432e0448c2d00ddeb75ec7f0e'
OWNER_SHA = '3bdbeee28406f9809624f3619b38915ab4445fa1a7c8aca30cbe445d3d1ddb40'
BROWSER_SHA = 'e72b02b97fb526d026056567d278ff4668f200f5acba25230d1ec46735d87d6d'
REPORT_SHA = 'd5b79102efd94b7dc8c339c78aaa67cc7a989faab0b1e37ba4cd79594c332b26'
SPECIAL_REPORT = W / 'reference-special-features-v1/extras/export/report.json'
SPECIAL_SHA = '8e4f9323495408540e6d97ecba19a6ba31f679ca0129a4c8eaf4d5277019c51e'
OLD_ROOTS = (Path('/opt/goby-fixtures/client-m3e'), Path('/opt/goby-fixtures/client-aux-m3e-v1'),
    Path('/opt/goby-fixtures/client-special-features-m3e-v1'))
OLD_LIBRARIES = {'3', '5', '7', '20', '57', '66', '83'}
OLD_USERS = {'38431ababffb4705b447fe23833d36b8', '46c1e1006a2c47ada1b482a96e89c549',
    '7d777e79b35541889f738bb5f5a8da23', 'bd655b2117fc468aa37780b1e8bbf532', 'efe2137dc3394ed4a23f9c337598f105'}
SOURCE_MOVIE = OLD_ROOTS[0] / 'Movies/M3e Client Movie.mp4'
SOURCE_MOVIE_SHA = '7265bc56bd7f495bcbd5224adcf6df94478a99d1994ba713274a194f8f9db088'
ANCHOR = MOVIES / 'LibraryChanged Anchor (2030)'
ADDED = MOVIES / 'LibraryChanged Observed (2031)'
ANCHOR_FILE = ANCHOR / 'LibraryChanged Anchor (2030).mp4'
ADDED_FILE = ADDED / 'LibraryChanged Observed (2031).mp4'
FIELDS = 'Path,ParentId,SortName,MediaSources,MediaStreams,Overview,Genres,Tags,People,Studios,ProviderIds,DateCreated,ProductionYear'
REFRESH = {'Recursive': 'true', 'MetadataRefreshMode': 'FullRefresh', 'ImageRefreshMode': 'ValidationOnly'}
ID = re.compile('[A-Za-z0-9_-]{1,128}')
HASH = re.compile('[0-9a-f]{64}')


class CaptureError(Exception):
    """A bounded ownership or preservation condition failed."""


def need(value, message):
    if not value: raise CaptureError(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def encoded(value):
    return (json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False) + '\n').encode('utf-8')


def same(a, b):
    return encoded(a) == encoded(b)


def file_id(info):
    return {'device': info.st_dev, 'inode': info.st_ino, 'uid': info.st_uid, 'gid': info.st_gid, 'mode': stat.S_IMODE(info.st_mode),
        'links': info.st_nlink, 'bytes': info.st_size, 'mtime_ns': info.st_mtime_ns, 'ctime_ns': info.st_ctime_ns}


def directory_id(path):
    info = path.lstat()
    need(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0, 'A scope directory was replaced or is not root-owned.')
    return {key: file_id(info)[key] for key in ('device', 'inode', 'uid', 'gid', 'mode')}


def read(path, expected=None, maximum=16 << 20, mode=0o600):
    need(path.is_absolute() and '..' not in path.parts, 'A read path is not canonical.')
    for parent in reversed(path.parents):
        info = parent.lstat()
        need(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
            'A recorder input ancestor is not root protected.')
    info = path.lstat()
    need(stat.S_ISREG(info.st_mode) and info.st_uid == info.st_gid == 0 and stat.S_IMODE(info.st_mode) == mode and
        info.st_nlink == 1 and info.st_size <= maximum, 'A recorder input is not a bounded owned regular file.')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as handle:
        need(file_id(os.fstat(handle.fileno())) == file_id(info), 'An input changed before opening.')
        raw = handle.read(maximum + 1)
        need(file_id(os.fstat(handle.fileno())) == file_id(info), 'An input changed while reading.')
    need(file_id(path.lstat()) == file_id(info) and len(raw) == info.st_size and (expected is None or sha(raw) == expected),
        'An input digest or identity changed.')
    return raw


def put(path, value, mode=0o600):
    raw = value if isinstance(value, bytes) else encoded(value)
    need(path.is_relative_to(ROOT) or path.is_relative_to(MEDIA), 'An output escaped this new scope.')
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode)
    with os.fdopen(descriptor, 'wb') as handle:
        os.fchmod(handle.fileno(), mode); handle.write(raw); handle.flush(); os.fsync(handle.fileno())
    sync(path.parent)
    return {'path': str(path), 'sha256': sha(raw)}


def sync(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try: os.fsync(fd)
    finally: os.close(fd)


def new_directory(path, mode):
    path.mkdir(mode=mode)
    os.chmod(path, mode)
    sync(path.parent)


def module(path, digest, name):
    raw = read(path, digest, 2 << 20)
    value = types.ModuleType(name); value.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), value.__dict__)
    return value


def media_tree(root, original=False):
    entries = {}
    paths = [root, *sorted(root.rglob('*'))]
    need(len(paths) <= 512, 'A media tree exceeds its bounded inventory.')
    for path in paths:
        info = path.lstat()
        need(info.st_uid == info.st_gid == 0 and (stat.S_ISDIR(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o755 or
            stat.S_ISREG(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o644 and (original or info.st_nlink == 1)),
            'A media member has an unknown type, link or permissions.')
        row = file_id(info)
        if stat.S_ISREG(info.st_mode):
            need(info.st_size <= 64 << 20, 'A media file exceeds the copied synthetic-file bound.')
            digest = hashlib.sha256()
            with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as stream:
                need(file_id(os.fstat(stream.fileno())) == row, 'A media inode changed before hashing.')
                for block in iter(lambda: stream.read(1 << 20), b''): digest.update(block)
                need(file_id(os.fstat(stream.fileno())) == row, 'A media inode changed while hashing.')
            need(file_id(path.lstat()) == row, 'A named media inode changed while hashing.')
            row['sha256'] = digest.hexdigest()
        entries[str(path.relative_to(root))] = row
    return entries


def old_media(op, owner):
    need(op.verify_media() == owner['media'], 'The original fourteen-file fixture changed.')
    return {str(root): media_tree(root, original=root == OLD_ROOTS[0]) for root in OLD_ROOTS}


def nfo(title, overview):
    return ('<?xml version="1.0" encoding="utf-8"?>\n<movie><title>' + title + '</title><plot>' + overview + '</plot><year>2031</year></movie>\n').encode()


def copy_movie(target):
    need(target in (ANCHOR_FILE, ADDED_FILE), 'An unowned media copy was requested.')
    original = SOURCE_MOVIE.lstat()
    need(stat.S_ISREG(original.st_mode) and original.st_uid == original.st_gid == 0 and stat.S_IMODE(original.st_mode) == 0o644,
        'The copy source is not the known synthetic movie.')
    with os.fdopen(os.open(SOURCE_MOVIE, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as source:
        descriptor = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o644)
        digest = hashlib.sha256()
        with os.fdopen(descriptor, 'wb') as output:
            os.fchmod(output.fileno(), 0o644)
            for block in iter(lambda: source.read(1 << 20), b''): digest.update(block); output.write(block)
            output.flush(); os.fsync(output.fileno())
        need(file_id(os.fstat(source.fileno())) == file_id(original) and digest.hexdigest() == SOURCE_MOVIE_SHA,
            'The source synthetic movie changed during its independent copy.')
    sync(target.parent)
    need(target.stat().st_nlink == 1 and target.stat().st_ino != original.st_ino, 'The new copy unexpectedly shares its source inode.')


def library_events(record):
    return [event for event in record.get('events', []) if event.get('direction') == 'server-to-client' and
        isinstance(event.get('json'), dict) and event['json'].get('MessageType') == 'LibraryChanged']


def sanitize(value, secrets_set):
    if isinstance(value, dict):
        return {key: '[redacted]' if re.search('password|token|secret|authorization|cookie|api.?key', key, re.I) else
            sanitize(item, secrets_set) for key, item in value.items()}
    if isinstance(value, list): return [sanitize(item, secrets_set) for item in value]
    if isinstance(value, str):
        for secret in sorted(secrets_set, key=len, reverse=True):
            if secret: value = value.replace(secret, '[redacted]')
        return re.sub(r'(?i)(?:https?|wss?)://[^\s"<>]+', '[redacted URL]', value)
    return value


class Actor:
    def __init__(self, run, role, username, password, user_id=None):
        self.run, self.role, self.username, self.password, self.user_id = run, role, username, password, user_id
        self.device = run.credentials['devices'][role]
        self.token = None; self.proven = self.closed = False
        self.login_sent = self.logout_sent = self.exact_sent = False

    def login(self):
        need(not self.login_sent, 'A login cannot be retried.'); self.login_sent = True
        value = self.run.request(self, 'login-' + self.role, 'POST', '/emby/Users/AuthenticateByName',
            {'Username': self.username, 'Pw': self.password}, mutation='login')
        need(value['complete'] and value['status'] == 200 and self.proven, 'The new recorder login is not completely owned.')

    def acknowledge(self, response):
        body = response.get('body') or {}; user, session = body.get('User', {}), body.get('SessionInfo', {})
        token = body.get('AccessToken')
        if isinstance(token, str) and 0 < len(token) <= 8192:
            self.token = token; self.run.secrets.add(token)
        self.proven = bool(response.get('complete') and response.get('status') == 200 and self.token and
            body.get('ServerId') == self.run.owner['serverId'] and user.get('Name') == self.username and
            re.fullmatch('[0-9a-f]{32}', user.get('Id', '')) and (self.user_id is None or user['Id'] == self.user_id) and
            user.get('Policy', {}).get('IsAdministrator') is (self.role == 'admin') and
            session.get('UserId') == user.get('Id') and session.get('DeviceId') == self.device)
        if self.proven: self.user_id = user['Id']

    def logout(self):
        if not self.proven:
            need(self.token is None, 'An unproven token remains in its private acknowledgement for review.'); return
        self.run.cleanup_deadline = time.monotonic() + 60
        need(not self.logout_sent and not self.exact_sent, 'Recorder revocation cannot be retried.')
        response = exact = None
        try:
            self.logout_sent = True
            response = self.run.request(self, 'logout-' + self.role, 'POST', '/emby/Sessions/Logout', mutation='logout', cleanup=True)
        finally:
            self.exact_sent = True
            exact = self.run.request(self, 'exact-' + self.role, 'GET', '/emby/System/Info', cleanup=True)
        self.closed = bool(response and response['complete'] and response['status'] == 204 and exact and exact['complete'] and exact['status'] == 401)
        need(self.closed, 'The new recorder did not close with logout204 and same-token401.')


class Run:
    def __init__(self, args, op, owner, transport):
        self.args, self.op, self.owner, self.transport = args, op, owner, transport
        self.inputs = json.loads(read(ROOT / 'OWNER.json'))
        self.credentials = json.loads(read(ROOT / 'private/credentials.json'))
        self.secrets = {self.credentials['password']}
        browser = op.read_private(op.BROWSER)
        admin = browser['accounts']['admin']
        self.secrets.update(account['password'] for account in browser['accounts'].values())
        self.admin = Actor(self, 'admin', admin['username'], admin['password'], admin['userId'])
        self.viewer = Actor(self, 'viewer', VIEWER_NAME, self.credentials['password'])
        self.library_id = self.user_id = None
        self.new_policy = None
        self.mutations, self.labels, self.records, self.stages, self.errors = set(), set(), {}, {}, []
        self.baseline = self.after = self.old_facts = self.media_before = None
        self.extra_ids = set()
        self.extra_parent_ids = set()
        self.ws = None; self.capture_count = 0
        self.deadline, self.cleanup_deadline = time.monotonic() + 900, None
        self.requests, self.bytes_read = 0, 0
        self.old_sources = {str(OPERATOR): OPERATOR_SHA, str(op.OWNER): OWNER_SHA, str(op.BROWSER): BROWSER_SHA,
            str(op.REPORT): REPORT_SHA, str(SPECIAL_REPORT): SPECIAL_SHA, str(args.transport_path): args.transport_sha256,
            str(Path(__file__).absolute()): args.script_sha256, str(ROOT / 'OWNER.json'): sha(read(ROOT / 'OWNER.json')),
            str(ROOT / 'private/credentials.json'): sha(read(ROOT / 'private/credentials.json'))}

    def save(self, name, value, private=True, soft=False):
        try:
            need(re.fullmatch('[a-z][a-z0-9-]*\.json', name), 'An evidence name is outside this fixed scope.')
            artifact = put(ROOT / ('private' if private else 'export') / name, value)
            self.records[name] = artifact; return artifact
        except Exception as error:
            if not soft: raise
            self.errors.append({'stage': 'journal-' + name, 'failureType': type(error).__name__})
            return None

    def check(self, cleanup=False):
        need(time.monotonic() < (self.cleanup_deadline if cleanup else self.deadline), 'The bounded recorder window expired.')
        for path, digest in self.old_sources.items(): read(Path(path), digest)
        self.op.same_service(self.owner)
        need(os.readlink('/proc/self/ns/net') == self.owner['serviceIdentity']['networkNamespace'], 'HTTP left the exact M3e reference namespace.')
        need(directory_id(ROOT) == self.inputs['controlIdentity'] and directory_id(MEDIA) == self.inputs['mediaIdentity'],
            'The new control or media root changed its immutable directory identity.')

    def route(self, path, query=None):
        need(path.startswith('/emby/') and '?' not in path and '#' not in path, 'A public API route is not canonical.')
        return path + ('?' + urlencode(query) if query else '')

    def request(self, actor, label, method, route, body=None, mutation=None, cleanup=False):
        self.check(cleanup)
        need(re.fullmatch('[a-z0-9-]{1,90}', label) and label not in self.labels and self.requests < (724 if cleanup else 720),
            'An HTTP request is repeated or exceeds its fixed bound.')
        need(actor in (self.admin, self.viewer) and (mutation == 'login' or actor.proven), 'The request lacks its new owned recorder.')
        if method == 'GET':
            need(body is None and mutation is None and self.allowed_get(actor, route, cleanup), 'An unapproved public read was requested.')
        else:
            need(method == 'POST' and mutation is not None and (actor.role, mutation) not in self.mutations and
                self.allowed_post(actor, route, body, mutation), 'A mutation is repeated or outside the exact new fixture.')
            if mutation not in ('login', 'logout'):
                need(self.media_before is not None and same(old_media(self.op, self.owner), self.media_before),
                    'Old media changed before a new-fixture mutation.')
            self.mutations.add((actor.role, mutation))
        self.labels.add(label)
        self.save(label + '-intent.json', {'method': method, 'route': route, 'actor': actor.role, 'mutation': mutation,
            'body': None if mutation == 'login' else body}, soft=cleanup)
        headers = {'Accept': 'application/json', 'Accept-Encoding': 'identity', 'Connection': 'close',
            'Authorization': f'Emby Client="Goby LibraryChanged Recorder", Device="Linux Recorder", DeviceId="{actor.device}", Version="1.0"'}
        if actor.proven and mutation != 'login': headers['X-Emby-Token'] = actor.token
        if body is not None: headers['Content-Type'] = 'application/json'
        connection = http.client.HTTPConnection('127.0.0.1', 18097, timeout=8)
        result = {'status': None, 'complete': False, 'body': None}
        prior = signal.getsignal(signal.SIGALRM)
        signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(CaptureError('The HTTP request exceeded its total bound.')))
        signal.setitimer(signal.ITIMER_REAL, 12)
        self.requests += 1
        try:
            connection.request(method, route, None if body is None else encoded(body), headers)
            response = connection.getresponse(); result['status'] = response.status
            maximum = 65536 if cleanup else 2 << 20
            length = response.getheader('Content-Length')
            need(length is None or length.isdigit() and int(length) <= maximum, 'An HTTP body exceeds its declared bound.')
            raw = response.read(maximum + 1)
            need(len(raw) <= maximum and (length is None or len(raw) == int(length)), 'An HTTP response is incomplete.')
            self.bytes_read += len(raw); need(self.bytes_read <= 160 << 20, 'The public API response total exceeds its bound.')
            mime = response.getheader('Content-Type') or ''
            parsed = json.loads(raw) if raw and mime.lower().split(';')[0] == 'application/json' else raw.decode('utf-8') if raw else None
            result.update(complete=True, body=parsed, contentType=mime, bytes=len(raw), sha256=sha(raw))
            if mutation == 'login': actor.acknowledge(result)
        except Exception as error: result['failureType'] = type(error).__name__
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0); signal.signal(signal.SIGALRM, prior); connection.close()
            self.save(label + '-response.json', result, soft=cleanup)
        if self.ws is not None and self.ws.open and not cleanup:
            try: self.ws.wait(0.01)
            except Exception as error:
                # Preserve the completed HTTP outcome independently of a failed
                # observation transport. Its private capture is closed by stage cleanup.
                self.errors.append({'stage': 'websocket-pump', 'failureType': type(error).__name__})
                try: self.ws.close()
                except Exception: pass
        return result

    def allowed_get(self, actor, route, cleanup):
        if cleanup: return route == '/emby/System/Info'
        path = route.split('?', 1)[0]
        if actor is self.admin and path in ('/emby/Users', '/emby/System/Info/Public', '/emby/System/Configuration', '/emby/Library/VirtualFolders/Query'): return True
        user_ids = OLD_USERS | ({self.user_id} if self.user_id else set())
        if path.startswith('/emby/UserSettings/'):
            return actor is self.admin and path.removeprefix('/emby/UserSettings/') in OLD_USERS
        for user_id in user_ids:
            if path == '/emby/Users/' + user_id: return actor is self.admin or user_id == self.viewer.user_id
            if path == '/emby/Users/' + user_id + '/Items':
                if actor is self.admin: return True
                query = parse_qs(route.partition('?')[2], keep_blank_values=True)
                return (user_id == self.viewer.user_id and self.library_id is not None and self.library_id not in OLD_LIBRARIES and
                    query.get('ParentId') == [self.library_id] and 'Ids' not in query)
            prefix = '/emby/Users/' + user_id + '/Items/'
            if path.startswith(prefix):
                suffix = path.removeprefix(prefix)
                return actor is self.admin and (suffix in self.extra_ids or any(
                    suffix == parent + '/' + family for parent in self.extra_parent_ids for family in ('SpecialFeatures', 'LocalTrailers')))
        return False

    def allowed_post(self, actor, route, body, mutation):
        if mutation == 'login': return route == '/emby/Users/AuthenticateByName' and body == {'Username': actor.username, 'Pw': actor.password} and not actor.proven
        if mutation == 'logout': return route == '/emby/Sessions/Logout' and body is None and actor.proven
        if actor is not self.admin or not actor.proven: return False
        if mutation == 'create-user': return self.baseline is not None and self.user_id is None and route == '/emby/Users/New' and body == {'Name': VIEWER_NAME}
        if mutation == 'password': return self.user_id is not None and self.user_id not in OLD_USERS and route == '/emby/Users/' + self.user_id + '/Password' and body == {
            'Id': self.user_id, 'NewPw': self.credentials['password'], 'ResetPassword': False}
        if mutation == 'policy': return self.user_id is not None and self.user_id not in OLD_USERS and route == '/emby/Users/' + self.user_id + '/Policy' and body == self.new_policy
        if mutation == 'create-library': return self.baseline is not None and self.library_id is None and route == '/emby/Library/VirtualFolders' and body == self.library_body()
        return (mutation in ('refresh-setup', 'refresh-add', 'refresh-update', 'refresh-remove') and self.library_id not in OLD_LIBRARIES and
            self.library_id is not None and route == self.refresh_route() and body == {})

    def get(self, actor, label, route):
        result = self.request(actor, label, 'GET', route)
        need(result['complete'] and result['status'] == 200 and 'failureType' not in result, 'A required public API read failed.')
        return result['body']

    def rows(self, actor, label, user_id, parent=None, ids=None):
        query = {'Recursive': 'true', 'Fields': FIELDS, 'EnableUserData': 'true', 'EnableTotalRecordCount': 'true', 'Limit': '256'}
        if parent is not None: query['ParentId'] = parent
        if ids is not None: query['Ids'] = ','.join(ids)
        value = self.get(actor, label, self.route('/emby/Users/' + user_id + '/Items', query))
        need(isinstance(value, dict) and isinstance(value.get('Items'), list) and len(value['Items']) <= 256 and
            value.get('TotalRecordCount', len(value['Items'])) == len(value['Items']), 'A scoped item query is incomplete; no truncation is accepted.')
        rows = {item['Id']: item for item in value['Items']}
        need(len(rows) == len(value['Items']) and all(ID.fullmatch(key) for key in rows), 'A catalog identity is malformed or repeated.')
        return rows

    def virtuals(self, label):
        value = self.get(self.admin, label, '/emby/Library/VirtualFolders/Query')
        need(isinstance(value, dict) and isinstance(value.get('Items'), list) and len(value['Items']) <= 8 and
            value.get('TotalRecordCount', len(value['Items'])) == len(value['Items']), 'The virtual-library enumeration is incomplete.')
        result = {row['ItemId']: row for row in value['Items']}
        need(len(result) == len(value['Items']) and OLD_LIBRARIES <= set(result), 'An old library disappeared or an identity repeats.')
        return result

    def snapshot_old(self, label, initial=False):
        libraries = self.virtuals(label + '-libraries')
        need(set(libraries) == OLD_LIBRARIES | ({self.library_id} if self.library_id else set()), 'The reference has an unowned library.')
        users_raw = self.get(self.admin, label + '-users', '/emby/Users')
        need(isinstance(users_raw, list), 'The user roster is not complete.')
        users = {row['Id']: row for row in users_raw}
        need(len(users) == len(users_raw) and set(users) == OLD_USERS | ({self.user_id} if self.user_id else set()), 'The reference has an unowned user.')
        catalog, catalogs_by_library = {}, {}
        for index, library_id in enumerate(sorted(OLD_LIBRARIES)):
            rows = self.rows(self.admin, f'{label}-catalog-{index}', self.admin.user_id, parent=library_id)
            catalogs_by_library[library_id] = rows
            catalog.update(rows)
        if initial:
            need(not any(row.get('Name') == VIEWER_NAME for row in users.values()) and not any(row.get('Name') == LIBRARY_NAME or
                str(MOVIES) in row.get('Locations', []) for row in libraries.values()), 'The proposed new account or library already exists.')
            self.old_facts = catalog
            self.old_catalogs = catalogs_by_library
            # The seventh library's extras are intentionally absent from ordinary
            # enumeration. Bind their previously captured explicit Path DTOs too.
            special = json.loads(read(SPECIAL_REPORT, SPECIAL_SHA))
            self.extra_parent_ids = {row['Id'] for row in special['mainItems'].values()}
            need(len(self.extra_parent_ids) == 2 and self.extra_parent_ids <= set(catalog), 'Known extra parents are absent from the old catalog.')
            for case in special['caseResults'].values():
                body = case.get('response', {}).get('body')
                for row in body if isinstance(body, list) else []:
                    if isinstance(row, dict) and isinstance(row.get('Path'), str) and row['Path'].startswith(str(OLD_ROOTS[2]) + '/') and row.get('Id'):
                        if row['Id'] not in self.old_facts: self.extra_ids.add(row['Id'])
            need(0 < len(self.old_facts) <= 256, 'The complete old-item witness exceeds the bounded query scope.')
        else:
            need(same(catalog, self.old_facts) and same(catalogs_by_library, self.old_catalogs), 'An old library catalog changed membership or metadata.')
        old_ids = sorted(self.old_facts)
        projections, preferences, extras, extra_lists = {}, {}, {}, {}
        for index, user_id in enumerate(sorted(OLD_USERS)):
            selected = self.rows(self.admin, f'{label}-user-items-{index}', user_id, ids=old_ids)
            need(set(selected) <= set(old_ids), 'A user projection escaped the old item set.')
            if user_id == self.admin.user_id: need(set(selected) == set(old_ids), 'The administrator lost an old known item.')
            projections[user_id] = selected
            preferences[user_id] = self.get(self.admin, f'{label}-preferences-{index}', '/emby/UserSettings/' + user_id)
            extras[user_id] = {}
            extra_lists[user_id] = {}
            for parent_index, parent in enumerate(sorted(self.extra_parent_ids)):
                for family in ('SpecialFeatures', 'LocalTrailers'):
                    response = self.request(self.admin, f'{label}-extra-list-{index}-{parent_index}-{family.lower()}', 'GET',
                        self.route('/emby/Users/' + user_id + '/Items/' + parent + '/' + family, {'Fields': FIELDS}))
                    need(response['complete'] and response['status'] in (200, 404), 'An old extra membership list could not be witnessed.')
                    extra_lists[user_id][parent + '/' + family] = {'status': response['status'], 'body': response['body']}
            for extra_index, item_id in enumerate(sorted(self.extra_ids)):
                response = self.request(self.admin, f'{label}-extra-{index}-{extra_index}', 'GET',
                    self.route('/emby/Users/' + user_id + '/Items/' + item_id, {'Fields': FIELDS, 'EnableUserData': 'true'}))
                need(response['complete'] and response['status'] in (200, 404), 'An old explicit extra projection could not be witnessed.')
                extras[user_id][item_id] = {'status': response['status'], 'body': response['body']}
        value = {'libraries': {key: libraries[key] for key in sorted(OLD_LIBRARIES)},
            'users': {key: {field: users[key][field] for field in ('Id', 'Name', 'Configuration', 'Policy')} for key in sorted(OLD_USERS)},
            'configuration': self.get(self.admin, label + '-configuration', '/emby/System/Configuration'),
            'itemsByUser': projections, 'preferences': preferences, 'explicitExtrasByUser': extras,
            'extraListsByUser': extra_lists, 'catalogByLibrary': catalogs_by_library}
        self.save(label + '-old-state.json', value)
        return value

    def library_body(self):
        body = copy.deepcopy(self.op.library_body('Movies'))
        body.update(Name=LIBRARY_NAME, Paths=[str(MOVIES)], RefreshLibrary=False)
        body['LibraryOptions']['PathInfos'] = [{'Path': str(MOVIES)}]
        return body

    def verify_new_library(self, row):
        expected = self.library_body()
        need(row.get('Name') == LIBRARY_NAME and row.get('Locations') == [str(MOVIES)] and row.get('CollectionType') == 'movies' and
            isinstance(row.get('ItemId'), str) and ID.fullmatch(row['ItemId']) and row['ItemId'] not in OLD_LIBRARIES,
            'The new library identity, name or location differs.')
        converted = copy.deepcopy(row)
        converted['Name'], converted['Locations'] = 'M3e Reference Movies', [str(self.op.MEDIA / 'Movies')]
        paths = converted.get('LibraryOptions', {}).get('PathInfos')
        need(isinstance(paths, list) and len(paths) == 1 and paths[0].get('Path') == str(MOVIES) and not paths[0].get('NetworkPath'),
            'The new library has an unexpected path mapping.')
        paths[0]['Path'] = str(self.op.MEDIA / 'Movies')
        self.op.verify_library(converted, 'Movies')
        return row['ItemId']

    def refresh_route(self):
        need(self.library_id is not None and self.library_id not in OLD_LIBRARIES, 'A refresh lacks the acknowledged new library.')
        return self.route('/emby/Items/' + self.library_id + '/Refresh', REFRESH)

    def new_catalog(self, label):
        rows = self.rows(self.viewer, label, self.viewer.user_id, parent=self.library_id)
        need(not set(rows).intersection(self.old_facts) and all(row.get('Path') in (None, '', str(MOVIES)) or
            isinstance(row.get('Path'), str) and row['Path'].startswith(str(MOVIES) + '/') for row in rows.values()),
            'The scoped new catalog contains an old or foreign media path.')
        return rows

    def expected_catalog(self, rows, phase):
        anchor = [row for row in rows.values() if row.get('Path') == str(ANCHOR_FILE)]
        added = [row for row in rows.values() if row.get('Path') == str(ADDED_FILE)]
        if len(anchor) != 1 or anchor[0].get('Type') != 'Movie': return False
        if phase == 'setup': return not added
        if phase == 'remove': return not added and self.added_id not in rows
        if len(added) != 1 or added[0].get('Type') != 'Movie': return False
        if phase == 'update':
            return added[0]['Id'] == self.added_id and added[0].get('Name') == 'M3e LibraryChanged Updated' and added[0].get('Overview') == 'Controlled updated overview for LibraryChanged.'
        return added[0]['Id'] not in self.old_facts

    def refresh(self, phase):
        result = self.request(self.admin, phase + '-refresh', 'POST', self.refresh_route(), {}, mutation='refresh-' + phase)
        need(result['complete'] and result['status'] in (200, 204) and result['body'] in (None, {}), 'The new-library refresh outcome is unknown; it cannot be retried.')
        previous, deadline = None, time.monotonic() + 100
        for number in range(32):
            need(time.monotonic() < deadline, 'The scoped refresh exceeded its finite observation window.')
            libraries = self.virtuals(f'{phase}-refresh-state-{number}')
            need(set(libraries) == OLD_LIBRARIES | {self.library_id} and all(same(self.baseline['libraries'][key], libraries[key]) for key in OLD_LIBRARIES),
                'An old library changed during the scoped refresh.')
            self.verify_new_library(libraries[self.library_id])
            rows = self.new_catalog(f'{phase}-members-{number}')
            state = {'library': libraries[self.library_id], 'items': rows}
            idle_shape = {key: libraries[self.library_id][key] for key in ('RefreshStatus', 'RefreshProgress') if key in libraries[self.library_id]}
            ready = idle_shape == {} and self.expected_catalog(rows, phase)
            if ready and previous is not None and same(state, previous):
                self.save(phase + '-catalog.json', state)
                return rows
            previous = state if ready else None
            if self.ws is not None and self.ws.open: self.ws.wait(2)
            else: time.sleep(2)
        raise CaptureError('The new catalog did not have two stable complete expected observations.')

    def setup(self):
        public = self.get(self.admin, 'public-identity', '/emby/System/Info/Public')
        need(public.get('Id') == self.owner['serverId'] and public.get('Version') == '4.9.5.0', 'The authenticated public server identity differs.')
        self.baseline = self.snapshot_old('before', initial=True)
        need(all(not any(key in row for key in ('RefreshStatus', 'RefreshProgress')) for row in self.baseline['libraries'].values()),
            'An existing library exposes an unreviewed active refresh state.')
        result = self.request(self.admin, 'create-viewer', 'POST', '/emby/Users/New', {'Name': VIEWER_NAME}, mutation='create-user')
        user = result['body'] if isinstance(result['body'], dict) else {}
        need(result['complete'] and result['status'] == 200 and user.get('Name') == VIEWER_NAME and
            re.fullmatch('[0-9a-f]{32}', user.get('Id', '')) and user['Id'] not in OLD_USERS and user.get('Policy', {}).get('IsAdministrator') is False,
            'The new ordinary account creation is not uniquely acknowledged.')
        self.user_id = self.viewer.user_id = user['Id']
        self.save('created-viewer.json', {'user': user, 'credential_file': str(ROOT / 'private/credentials.json')})
        password = self.request(self.admin, 'viewer-password', 'POST', '/emby/Users/' + self.user_id + '/Password',
            {'Id': self.user_id, 'NewPw': self.credentials['password'], 'ResetPassword': False}, mutation='password')
        need(password['complete'] and password['status'] in (200, 204), 'The new ordinary password was not acknowledged.')
        self.new_policy = dict(user['Policy'], IsAdministrator=False, IsDisabled=False, EnableAllFolders=True, EnabledFolders=[],
            EnableMediaPlayback=True, EnableAudioPlaybackTranscoding=True, EnableVideoPlaybackTranscoding=True,
            EnablePlaybackRemuxing=True, EnableContentDeletion=False, EnableContentDownloading=False)
        policy = self.request(self.admin, 'viewer-policy', 'POST', '/emby/Users/' + self.user_id + '/Policy', self.new_policy, mutation='policy')
        need(policy['complete'] and policy['status'] in (200, 204), 'The new ordinary policy was not acknowledged.')
        new_directory(ANCHOR, 0o755); copy_movie(ANCHOR_FILE)
        put(ANCHOR / 'movie.nfo', nfo('M3e LibraryChanged Anchor', 'Controlled anchor for the new library.'), 0o644)
        self.save('setup-media.json', media_tree(MEDIA))
        result = self.request(self.admin, 'create-library', 'POST', '/emby/Library/VirtualFolders', self.library_body(), mutation='create-library')
        need(result['complete'] and result['status'] in (200, 204) and result['body'] in (None, {}), 'Library creation is unconfirmed; do not resend it.')
        libraries = self.virtuals('created-library-lookup')
        new = {key: value for key, value in libraries.items() if key not in OLD_LIBRARIES}
        need(len(new) == 1, 'The new library cannot be uniquely enumerated.')
        self.library_id = self.verify_new_library(next(iter(new.values())))
        self.save('created-library.json', new[self.library_id])
        self.viewer.login()
        identity = self.get(self.viewer, 'viewer-identity', '/emby/Users/' + self.user_id)
        need(identity.get('Id') == self.user_id and identity.get('Name') == VIEWER_NAME and identity.get('Policy', {}).get('IsAdministrator') is False and
            identity['Policy'].get('EnableAllFolders') is True and identity['Policy'].get('EnabledFolders') == [], 'The new authenticated ordinary user has another identity or visibility mode.')
        self.catalog = self.refresh('setup')
        after = self.snapshot_old('setup-after')
        need(same(self.baseline, after), 'Setup changed old seven-library/five-user public state.')
        self.save('setup-completed.json', {'library_id': self.library_id, 'viewer_user_id': self.user_id, 'items': self.catalog,
            'old_public_state_preserved': True, 'observation_window_started': False})

    def quiet(self):
        self.ws.mark('pre-stage-quiet-start')
        deadline, last_count, last_change = time.monotonic() + 90, len(library_events(self.ws.record)), time.monotonic()
        while time.monotonic() < deadline:
            need(self.ws.open, 'The observer closed before the controlled action.')
            self.ws.wait(1)
            need(self.ws.open, 'The observer closed during the quiet interval.')
            count = len(library_events(self.ws.record))
            if count != last_count: last_count, last_change = count, time.monotonic()
            if time.monotonic() - last_change >= 35:
                self.ws.mark('pre-stage-quiet-complete'); return
        raise CaptureError('Prior LibraryChanged traffic did not become quiet within the finite boundary.')

    def stage_media(self, phase):
        need(same(old_media(self.op, self.owner), self.media_before), 'Old media changed before a controlled new-media operation.')
        self.save(phase + '-media-intent.json', {'phase': phase, 'media_root': str(MEDIA),
            'before': media_tree(MEDIA), 'scope': 'Only the two explicitly owned new movie directories and outside-library quarantine.'})
        if phase == 'add':
            new_directory(ADDED, 0o755); copy_movie(ADDED_FILE)
            put(ADDED / 'movie.nfo', nfo('M3e LibraryChanged Observed', 'Controlled original overview for LibraryChanged.'), 0o644)
        elif phase == 'update':
            path = ADDED / 'movie.nfo'; old = read(path, mode=0o644)
            need(old == nfo('M3e LibraryChanged Observed', 'Controlled original overview for LibraryChanged.'), 'The new NFO differs before its one update.')
            self.save('update-original-nfo.json', {'utf8': old.decode(), 'sha256': sha(old), 'identity': file_id(path.stat())})
            pending = ADDED / 'movie.nfo.updated'
            put(pending, nfo('M3e LibraryChanged Updated', 'Controlled updated overview for LibraryChanged.'), 0o644)
            need(read(path, mode=0o644) == old, 'The owned NFO changed before atomic replacement.')
            os.replace(pending, path); sync(ADDED)
        else:
            need(phase == 'remove' and set(path.name for path in ADDED.iterdir()) == {ADDED_FILE.name, 'movie.nfo'}, 'The removal directory has unknown content.')
            destination = QUARANTINE / ADDED.name
            need(not os.path.lexists(destination), 'The outside-library quarantine destination already exists.')
            before = media_tree(ADDED)
            os.rename(ADDED, destination); sync(MOVIES); sync(QUARANTINE)
            after = media_tree(destination)
            # Renaming the owned directory changes its ctime; all file records,
            # including their content and inode/link identities, remain exact.
            need(set(before) == set(after) and all(same(value, after[key]) if 'sha256' in value else
                all(value[field] == after[key][field] for field in value if field != 'ctime_ns') for key, value in before.items()),
                'The quarantine move changed an owned file or directory identity.')
        self.save(phase + '-media-after.json', media_tree(MEDIA))

    def public_capture(self, raw):
        fields = {'at', 'elapsedMs', 'direction', 'kind', 'opcode', 'fin', 'masked', 'payloadLength', 'closeCode'}
        events = []
        for event in raw.get('events', []):
            value = {key: event[key] for key in fields if key in event}
            value['privateEventSha256'] = sha(encoded(event))
            if 'json' in event: value['json'] = sanitize(event['json'], self.secrets)
            events.append(value)
        return {'request': {'method': 'GET', 'path': '/embywebsocket', 'queryKeys': ['api_key', 'deviceId'],
                'credentialFingerprint': sha(self.viewer.token.encode()), 'deviceId': self.viewer.device},
            'response': {key: raw.get('response', {}).get(key) for key in ('status', 'acceptMatchesRequestKey')},
            'events': events, 'annotations': raw.get('annotations', []),
            **{key: raw[key] for key in ('observedDurationMs', 'transportClosed', 'clientCloseSent', 'serverCloseReceived', 'peerEOF') if key in raw},
            'timingBoundary': 'Client receipt times only. API work may buffer frames before the following pump; 101 is not authentication proof.'}

    def capture_stage(self, phase):
        path = '/embywebsocket?' + urlencode({'api_key': self.viewer.token, 'deviceId': self.viewer.device})
        self.ws = self.transport.Capture('127.0.0.1', 18097, path)
        minimum_end = None; complete = False
        before = self.new_catalog(phase + '-before-catalog')
        self.save(phase + '-catalog-before.json', before)
        try:
            self.ws.connect(); self.quiet()
            begin = len(self.ws.record['events'])
            self.ws.mark(phase + '-action-start')
            self.stage_media(phase)
            self.ws.mark(phase + '-refresh-dispatch')
            minimum_end = time.monotonic() + 35
            after = self.refresh(phase)
            need(self.ws.open, 'The observer closed before catalog convergence.')
            self.ws.mark(phase + '-catalog-stable')
            if phase == 'add': self.added_id = next(row['Id'] for row in after.values() if row.get('Path') == str(ADDED_FILE))
            if phase == 'update': need(before[self.added_id].get('Name') != after[self.added_id]['Name'] and
                before[self.added_id].get('Overview') != after[self.added_id]['Overview'], 'The update had no proven public Name and Overview effect.')
            if phase == 'remove':
                removed_subtree = {item_id for item_id, row in before.items() if row.get('Path') == str(ADDED) or
                    isinstance(row.get('Path'), str) and row['Path'].startswith(str(ADDED) + '/')}
                need(self.added_id in removed_subtree and not removed_subtree.intersection(after), 'An item from the moved directory remains in the catalog.')
            minimum_end = max(minimum_end, time.monotonic() + 35)
            while time.monotonic() < minimum_end:
                need(self.ws.open, 'The observer closed before the observation window ended.')
                self.ws.wait(max(0, min(10, minimum_end - time.monotonic())))
                need(self.ws.open, 'The observer closed during the observation window.')
            self.ws.mark(phase + '-window-complete')
            events = library_events({'events': self.ws.record['events'][begin:]})
            self.stages[phase] = {'catalogEffectVerified': True, 'beforeItemIds': list(before), 'afterItemIds': list(after),
                'LibraryChangedObservations': len(events), 'eventOutcome': 'observed' if events else 'not_observed',
                'windowAfterStableCatalogSeconds': 35, 'attribution': 'Receipts within the controlled window; raw arrays and unrelated IDs are retained without inferred private rules.'}
            complete = True
        finally:
            if minimum_end is not None and self.ws.open:
                while time.monotonic() < minimum_end:
                    try: self.ws.wait(max(0, min(10, minimum_end - time.monotonic())))
                    except Exception: break
            try: self.ws.close()
            finally:
                self.save(phase + '-websocket-private.json', self.ws.record, soft=True)
                public = self.public_capture(self.ws.record)
                public.update(phase=phase, controlledCatalogEffectVerified=complete)
                self.save(phase + '-websocket.json', public, private=False, soft=True)
                self.ws = None
        after_old = self.snapshot_old(phase + '-preservation')
        need(same(after_old, self.baseline), 'A controlled stage changed old library or account public state.')
        need(same(old_media(self.op, self.owner), self.media_before), 'A controlled stage changed old media bytes, inodes or links.')

    def execute(self):
        self.save('worker-started.json', {'marker': MARKER, 'process': self.op.process_identity(os.getpid())})
        try:
            self.media_before = old_media(self.op, self.owner)
            self.save('old-media-before.json', self.media_before)
            self.admin.login(); self.setup()
            for phase in ('add', 'update', 'remove'): self.capture_stage(phase)
        except Exception as error: self.errors.append({'stage': 'capture', 'failureType': type(error).__name__, 'reason': str(error) if type(error) is CaptureError else None})
        finally:
            self.deadline = time.monotonic() + 180
            if self.ws is not None:
                try: self.ws.close()
                except Exception as error: self.errors.append({'stage': 'terminal-websocket-close', 'failureType': type(error).__name__})
                finally:
                    self.save('terminal-websocket-private.json', self.ws.record, soft=True)
                    self.ws = None
            if self.admin.proven and self.baseline is not None:
                try:
                    self.after = self.snapshot_old('final')
                    need(same(self.baseline, self.after), 'Final old public state differs.')
                except Exception as error: self.errors.append({'stage': 'final-public-state', 'failureType': type(error).__name__})
            for actor in (self.viewer, self.admin):
                try: actor.logout()
                except Exception as error: self.errors.append({'stage': 'logout-' + actor.role, 'failureType': type(error).__name__})
            try:
                after_media = old_media(self.op, self.owner)
                self.save('old-media-after.json', after_media)
                need(same(after_media, self.media_before), 'The old media changed.')
            except Exception as error: self.errors.append({'stage': 'old-media', 'failureType': type(error).__name__})
        report = {'marker': MARKER, 'result': 'complete' if not self.errors and set(self.stages) == {'add', 'update', 'remove'} else 'retained_for_review',
            'referenceIdentity': self.owner['serviceIdentity'], 'serverId': self.owner['serverId'], 'libraryId': self.library_id, 'newUserId': self.user_id,
            'oldLibraryIds': sorted(OLD_LIBRARIES), 'oldUserIds': sorted(OLD_USERS), 'stages': self.stages, 'errors': self.errors,
            'authentication': {actor.role: {'ownedIdentityProven': actor.proven, 'closed': actor.closed, 'userId': actor.user_id,
                'credentialFingerprint': sha(actor.token.encode()) if actor.token else None} for actor in (self.admin, self.viewer)},
            'oldPublicStatePreserved': self.baseline is not None and same(self.baseline, self.after),
            'httpRequests': self.requests, 'responseBytes': self.bytes_read, 'evidence': self.records,
            'clientAcceptance': False, 'boundary': 'Controlled reference protocol observation only; no observed event is not a general no-notification guarantee. No original database or implementation was read.'}
        need(not any(secret and secret in encoded(report).decode() for secret in self.secrets), 'A known credential survived into the public report.')
        self.save('report.json', report, private=False)
        return report


def preflight(args):
    need(sys.platform == 'linux' and os.getuid() == os.geteuid() == os.getgid() == os.getegid() == 0 and os.environ.get('SSH_CONNECTION'), 'Use authorized root SSH only.')
    read(Path(__file__).absolute(), args.script_sha256)
    need(args.transport_sha256 == TRANSPORT_SHA and args.transport_path.is_relative_to(W) and args.transport_path.name == 'bounded_websocket_capture.py',
        'The transport is not the exact reviewed standalone module.')
    op = module(OPERATOR, OPERATOR_SHA, 'owned_m3e_reference_identity')
    owner = json.loads(read(op.OWNER, OWNER_SHA))
    op.validate_owner(owner)
    need(owner.get('phase') == 'ready' and owner['serviceIdentity']['pid'] == 332054 and owner['serviceIdentity']['startTicks'] == '357218' and
        owner['serviceIdentity']['bootId'] == '6bdfc486-7bc8-412f-82b5-70095a09dde7' and owner['serviceIdentity']['networkNamespace'] == 'net:[4026532602]',
        'The current M3e reference process lifetime differs.')
    op.same_service(owner)
    read(op.BROWSER, BROWSER_SHA); read(op.REPORT, REPORT_SHA); read(SPECIAL_REPORT, SPECIAL_SHA)
    transport = module(args.transport_path, args.transport_sha256, 'owned_bounded_websocket_capture')
    media = old_media(op, owner)
    need(media[str(OLD_ROOTS[0])]['Movies/M3e Client Movie.mp4']['sha256'] == SOURCE_MOVIE_SHA, 'The copied synthetic source changed.')
    return op, owner, transport


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--mode', choices=('check', 'capture', '_worker'), required=True)
    parser.add_argument('--script-sha256', required=True)
    parser.add_argument('--transport-path', type=Path, required=True)
    parser.add_argument('--transport-sha256', required=True)
    args = parser.parse_args()
    need(HASH.fullmatch(args.script_sha256) and HASH.fullmatch(args.transport_sha256), 'Explicit source hashes are required.')
    os.umask(0o077)
    op, owner, transport = preflight(args)
    if args.mode == '_worker':
        need(os.readlink('/proc/self/ns/net') == owner['serviceIdentity']['networkNamespace'], 'The worker is outside the owned reference network namespace.')
        authority = json.loads(read(ROOT / 'OWNER.json'))
        need(authority['marker'] == MARKER and authority['sourceSha256'] == args.script_sha256 and authority['transportSha256'] == args.transport_sha256 and
            authority['referenceIdentity'] == owner['serviceIdentity'] and authority['controlIdentity'] == directory_id(ROOT) and
            authority['mediaIdentity'] == directory_id(MEDIA) and os.getppid() == authority['parentProcess']['pid'] and
            op.process_identity(os.getppid()) == authority['parentProcess'], 'The new scope owner or its live supervisor differs.')
        result = Run(args, op, owner, transport).execute()
        print(json.dumps({'marker': MARKER, 'result': result['result'], 'httpRequests': result['httpRequests']}))
        return 0 if result['result'] == 'complete' else 1
    need(not os.path.lexists(ROOT) and not os.path.lexists(MEDIA), 'The one-shot scope already exists; no retry or adoption is allowed.')
    need(os.readlink('/proc/self/ns/net') == os.readlink('/proc/1/ns/net'), 'The supervisor must start in the host namespace.')
    lock = os.open(op.LOCK, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        info = op.canonical(op.LOCK, mode=0o600)
        need(file_id(os.fstat(lock)) == file_id(info), 'The existing reference lock changed identity.')
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if args.mode == 'check':
            print(json.dumps({'marker': MARKER, 'result': 'preflight_passed', 'httpRequests': 0, 'outputCreated': False})); return 0
        new_directory(ROOT, 0o700)
        put(ROOT / 'intent.json', {'marker': MARKER, 'sourceSha256': args.script_sha256, 'transportSha256': args.transport_sha256,
            'referenceIdentity': owner['serviceIdentity'], 'controlRoot': str(ROOT), 'mediaRoot': str(MEDIA),
            'scope': 'Create one new ordinary viewer and one new Movies library, then add/update/quarantine only two synthetic movies.'})
        new_directory(ROOT / 'private', 0o700); new_directory(ROOT / 'export', 0o700)
        put(ROOT / 'private/credentials.json', {'marker': MARKER, 'username': VIEWER_NAME, 'password': secrets.token_hex(32),
            'devices': {role: 'goby-librarychanged-' + role + '-' + secrets.token_hex(16) for role in ('admin', 'viewer')}})
        new_directory(MEDIA, 0o755); new_directory(MOVIES, 0o755); new_directory(QUARANTINE, 0o755)
        put(MEDIA / '.goby-managed', (MARKER + '\n').encode(), 0o644)
        put(ROOT / 'OWNER.json', {'marker': MARKER, 'sourceSha256': args.script_sha256, 'transportSha256': args.transport_sha256,
            'referenceIdentity': owner['serviceIdentity'], 'mediaRoot': str(MEDIA), 'parentProcess': op.process_identity(os.getpid()),
            'controlIdentity': directory_id(ROOT), 'mediaIdentity': directory_id(MEDIA)})
        namespace = os.open('/proc/332054/ns/net', os.O_RDONLY)
        try:
            need(os.fstat(namespace).st_ino == 4026532602, 'The pinned namespace descriptor changed.')
            op.same_service(owner)
            result = subprocess.run(['/usr/bin/nsenter', '--net=/proc/self/fd/' + str(namespace), '/usr/bin/python3', '-B', str(Path(__file__).absolute()),
                '--mode', '_worker', '--script-sha256', args.script_sha256, '--transport-path', str(args.transport_path), '--transport-sha256', args.transport_sha256],
                capture_output=True, timeout=1250, check=False, pass_fds=(namespace,), env=dict(op.BASE_ENV, SSH_CONNECTION=os.environ['SSH_CONNECTION']))
            put(ROOT / 'private/worker.stdout', result.stdout)
            put(ROOT / 'private/worker.stderr', result.stderr)
            put(ROOT / 'private/worker-exit.json', {'returncode': result.returncode, 'stdoutSha256': sha(result.stdout), 'stderrSha256': sha(result.stderr)})
            print(json.dumps({'marker': MARKER, 'result': 'complete' if result.returncode == 0 else 'retained_for_review', 'report': str(ROOT / 'export/report.json')}))
            return result.returncode
        finally: os.close(namespace)
    finally: os.close(lock)


if __name__ == '__main__':
    try: sys.exit(main())
    except Exception as error:
        print(json.dumps({'marker': MARKER, 'result': 'failed', 'failureType': type(error).__name__}), file=sys.stderr)
        sys.exit(1)
