#!/usr/bin/env python3
"""Append three receipted auxiliary libraries to the existing M3e reference.

Invoke only through root SSH with explicit SHA-256 bindings for this script,
the accepted reference preparer, the current owner, and the completed auxiliary
media manifest. The supervisor enters only the existing network namespace.
No service, old library, account policy, preference, or media file is changed by this
operator. One fresh administrator recorder performs three creates and three
item-scoped refreshes. Any existing evidence or uncertain mutation is terminal;
there is no adoption, retry, deletion, global refresh, or recovery mode.
"""

from __future__ import annotations

import argparse
from collections import Counter
import copy
import fcntl
import hashlib
import http.client
import json
import os
from pathlib import Path, PurePosixPath
import re
import secrets
import signal
import stat
import subprocess
import sys
import time
import types
from urllib.parse import urlencode

sys.dont_write_bytecode = True
WORK = Path('/opt/goby-test/exec-work-m3e')
ROOT = Path('/opt/goby-fixtures/client-aux-m3e-v1')
CONTROL = WORK / 'reference-auxiliary-libraries-v1'
PRIVATE, EXPORT = CONTROL / 'private', CONTROL / 'export'
OPERATOR = WORK / 'prepare-client-reference.py'
MARKER = 'goby-reference-auxiliary-libraries-m3e-v1'
MEDIA_MARKER = 'goby-client-auxiliary-media-m3e-v1'
PID, TICKS = 332054, '357218'
LIBRARIES = {'Movies': 'movies', 'TV': 'tvshows', 'Music': 'music'}
CLIENT = 'Goby Auxiliary Library Recorder'
MAX_BODY, MAX_TOTAL, MAX_REQUESTS = 2 << 20, 96 << 20, 260
MAIN_REQUESTS, MAIN_SECONDS, CLEANUP_SECONDS = 226, 420, 150
ITEM_FIELDS = 'Path,MediaSources,MediaStreams,Overview,ProviderIds,Genres,Tags,People,Studios,SortName,DateCreated,ProductionYear,PremiereDate,OriginalTitle'
REFRESH_QUERY = {'Recursive': 'true', 'MetadataRefreshMode': 'FullRefresh', 'ImageRefreshMode': 'ValidationOnly'}
ID = re.compile(r'[A-Za-z0-9_-]{1,128}')
SHA = re.compile(r'[0-9a-f]{64}')
URL = re.compile(r'https?://[^\s\"\'<>]+', re.IGNORECASE)
SAFE_ITEM_FIELDS = ('Id', 'Name', 'Type', 'Path', 'ParentId', 'MediaType', 'IndexNumber', 'ParentIndexNumber',
                    'ProductionYear', 'PremiereDate', 'CommunityRating', 'OfficialRating', 'Genres', 'Tags',
                    'Studios', 'People', 'Artists', 'ArtistItems', 'Album', 'AlbumId', 'AlbumArtist', 'AlbumArtists',
                    'RunTimeTicks', 'SortName', 'Overview', 'ProviderIds')


class Failure(Exception):
    """An exact operation boundary or preservation proof failed."""


def require(condition, message):
    if not condition:
        raise Failure(message)


def encoded(value):
    return (json.dumps(value, sort_keys=True, ensure_ascii=True, separators=(',', ':'), allow_nan=False) + '\n').encode()


def equal(left, right):
    return encoded(left) == encoded(right)


def decode(raw):
    def pairs(entries):
        result = {}
        for key, value in entries:
            require(key not in result, 'A JSON object repeats a member.')
            result[key] = value
        return result

    def invalid(_value):
        raise Failure('A JSON response contains a non-finite number.')

    return json.loads(raw, object_pairs_hook=pairs, parse_constant=invalid)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def identity(info):
    return {'device': info.st_dev, 'inode': info.st_ino, 'uid': info.st_uid, 'gid': info.st_gid,
            'mode': stat.S_IMODE(info.st_mode), 'links': info.st_nlink, 'bytes': info.st_size,
            'mtimeNs': info.st_mtime_ns, 'ctimeNs': info.st_ctime_ns}


def protected_bytes(path, limit=MAX_BODY):
    require(path.is_absolute() and '..' not in path.parts, 'An input path is not absolute and canonical.')
    for parent in reversed(path.parents):
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                'An input ancestor is not protected and root-owned.')
    before = path.lstat()
    require(stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and
            stat.S_IMODE(before.st_mode) in (0o600, 0o644) and before.st_nlink == 1 and before.st_size <= limit,
            'An input file has an unreviewed owner, type, mode, link count, or size.')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as source:
        require(identity(os.fstat(source.fileno())) == identity(before), 'An input changed while opening.')
        raw = source.read(limit + 1)
        require(len(raw) == before.st_size and identity(os.fstat(source.fileno())) == identity(before),
                'An input changed during its bounded read.')
    require(identity(path.lstat()) == identity(before), 'An input changed after reading.')
    return raw


def save(op, path, value):
    require(path.is_relative_to(CONTROL), 'Evidence escaped the new control directory.')
    op.save(path, value)


def identifier(value):
    require(isinstance(value, str) and ID.fullmatch(value), 'An API identifier cannot enter a fixed route.')
    return value


def rows(body, maximum=256):
    require(isinstance(body, dict) and isinstance(body.get('Items'), list) and
            type(body.get('TotalRecordCount')) is int and 0 <= body['TotalRecordCount'] <= maximum and
            body['TotalRecordCount'] == len(body['Items']), 'A bounded catalog response is incomplete.')
    result = {}
    for row in body['Items']:
        require(isinstance(row, dict), 'A catalog row is malformed.')
        key = identifier(row.get('Id'))
        require(key not in result, 'The catalog repeats an item identity.')
        result[key] = row
    return result


def refresh_shape(row):
    return {key: row[key] for key in ('RefreshStatus', 'RefreshProgress') if key in row}


def library_body(op, name):
    require(name in LIBRARIES, 'An unknown auxiliary library was requested.')
    body = copy.deepcopy(op.library_body(name))
    body['Name'] = 'M3e Auxiliary ' + name
    body['Paths'] = [str(ROOT / name)]
    body['LibraryOptions']['PathInfos'] = [{'Path': str(ROOT / name)}]
    require(body['RefreshLibrary'] is False and body['CollectionType'] == LIBRARIES[name],
            'The accepted library template changed its create contract.')
    return body


def verify_new_library(op, row, name):
    # Reuse the original strict option checks without mutating its module globals.
    expected = library_body(op, name)
    require(row.get('Name') == expected['Name'] and row.get('CollectionType') == expected['CollectionType'] and
            row.get('Locations') == expected['Paths'], 'The new library does not have its exact owned name and path.')
    converted = copy.deepcopy(row)
    converted['Name'] = 'M3e Reference ' + name
    converted['Locations'] = [str(op.MEDIA / name)]
    options = converted.get('LibraryOptions')
    require(isinstance(options, dict) and isinstance(options.get('PathInfos'), list) and len(options['PathInfos']) == 1 and
            options['PathInfos'][0].get('Path') == str(ROOT / name) and not options['PathInfos'][0].get('NetworkPath'),
            'The new library has an unowned path mapping.')
    options['PathInfos'][0]['Path'] = str(op.MEDIA / name)
    op.verify_library(converted, name)
    return identifier(row.get('ItemId'))


def load_inputs(args):
    require(sys.platform == 'linux' and os.geteuid() == 0 and os.environ.get('SSH_CONNECTION'),
            'Run only through authorized root SSH on test-env.')
    for value in (args.script_sha256, args.operator_sha256, args.owner_sha256, args.manifest_sha256):
        require(isinstance(value, str) and SHA.fullmatch(value), 'Every exact input SHA-256 must be supplied.')
    source = Path(__file__).absolute()
    require(sha(protected_bytes(source)) == args.script_sha256, 'The reviewed auxiliary operator bytes changed.')
    raw = protected_bytes(OPERATOR)
    require(sha(raw) == args.operator_sha256, 'The accepted reference preparer bytes changed.')
    op = types.ModuleType('owned_auxiliary_reference_preparer')
    op.__file__ = str(OPERATOR)
    exec(compile(raw, str(OPERATOR), 'exec'), op.__dict__)
    require(op.WORK == WORK and op.PORT == 18097 and op.MEDIA == Path('/opt/goby-fixtures/client-m3e') and
            op.MARKER == 'goby-emby-client-reference-m3e-v1', 'The preparer addresses another reference.')
    op.preconditions()
    owner_raw = protected_bytes(op.OWNER)
    require(sha(owner_raw) == args.owner_sha256, 'The explicitly reviewed reference owner changed.')
    owner = decode(owner_raw)
    op.validate_owner(owner)
    require(owner.get('phase') == 'ready' and owner.get('serviceIdentity', {}).get('pid') == PID and
            owner['serviceIdentity'].get('startTicks') == TICKS, 'The expected reference process lifetime changed.')
    op.same_service(owner)
    require(op.digest(op.REPORT) == owner['reportSha256'] and op.digest(op.BROWSER) == owner['browserSha256'],
            'The completed reference evidence or browser credentials changed.')
    require(op.verify_media() == owner['media'], 'The original fourteen-file reference media changed.')
    browser = op.read_private(op.BROWSER)
    require(browser.get('marker') == op.MARKER and browser.get('serverId') == owner['serverId'] and
            set(browser.get('accounts', {})) == set(op.NAMES), 'The retained three-account browser input differs.')
    for key, account in browser['accounts'].items():
        require(account.get('username') == op.NAMES[key] and re.fullmatch(r'[0-9a-f]{64}', account.get('password', '')) and
                re.fullmatch(r'[0-9a-f]{32}', account.get('userId', '')), 'An existing account credential differs.')
    return op, owner, browser


def media_snapshot(op, owner, expected_manifest):
    manifest_raw = protected_bytes(ROOT / 'manifest.json')
    require(sha(manifest_raw) == expected_manifest, 'The auxiliary media manifest changed.')
    manifest = decode(manifest_raw)
    completed_raw = protected_bytes(WORK / 'auxiliary-media-v1/completed.json')
    completed = decode(completed_raw)
    require(completed.get('marker') == MEDIA_MARKER and completed.get('phase') == 'completed' and
            completed.get('manifest') == str(ROOT / 'manifest.json') and completed.get('manifestSha256') == expected_manifest and
            type(completed.get('fileCountExcludingManifest')) is int and completed['fileCountExcludingManifest'] == 43 and
            completed.get('oldBytesInodesAndLinksPreserved') is True and completed.get('libraryScanExecuted') is False,
            'The auxiliary media lacks its completed exact-manifest generation receipt.')
    require(manifest.get('marker') == MEDIA_MARKER and type(manifest.get('version')) is int and manifest['version'] == 1 and manifest.get('root') == str(ROOT) and
            manifest.get('oldManifestSha256') == owner['media']['manifestSha256'] and
            manifest.get('oldBytesInodesAndLinksPreserved') is True and manifest.get('libraryScanExecuted') is False and
            manifest.get('referenceContractVerified') is False, 'The auxiliary manifest is not the completed unscanned fixture.')
    files, directories, profiles = manifest.get('files'), manifest.get('directories'), manifest.get('profiles')
    require(isinstance(files, dict) and len(files) == 43 and isinstance(directories, list) and len(directories) == 25 and
            directories == sorted(set(directories)) and isinstance(profiles, dict) and len(profiles) == 25,
            'The auxiliary fixture inventory differs from its reviewed generation contract.')
    primary = manifest.get('expectedItems')
    require(isinstance(primary, list) and len(primary) == 19 and all(isinstance(row, dict) for row in primary) and
            Counter(row.get('kind') for row in primary) == {'Movie': 12, 'Episode': 2, 'Audio': 5} and
            len({row.get('media') for row in primary}) == 19, 'The primary auxiliary fixture declaration is incomplete.')
    require(set(profiles) == {name for name in files if PurePosixPath(name).suffix in ('.mp4', '.mp3', '.flac')} and
            all(row.get('media') in profiles for row in primary), 'A primary item lacks its generated media profile.')
    require(protected_bytes(ROOT / '.goby-managed') == (MEDIA_MARKER + '\n').encode(), 'The auxiliary media owner marker differs.')
    for relative in [*files, *directories]:
        path = PurePosixPath(relative)
        require(isinstance(relative, str) and relative == path.as_posix() and not path.is_absolute() and
                '..' not in path.parts and all(part not in ('', '.') for part in path.parts), 'A manifest member escapes its exact root.')
    actual_files, actual_directories, facts, total = {}, [], {}, 0
    op.canonical(ROOT, directory=True, mode=0o755)
    for path in sorted(ROOT.rglob('*')):
        relative = path.relative_to(ROOT).as_posix()
        info = path.lstat()
        if stat.S_ISDIR(info.st_mode):
            op.canonical(path, directory=True, mode=0o755)
            actual_directories.append(relative)
        elif path != ROOT / 'manifest.json':
            op.canonical(path, mode=0o644)
            require(relative in files and info.st_size <= 16 << 20, 'An auxiliary media member is unknown or oversized.')
            total += info.st_size
            require(total <= 160 << 20, 'The auxiliary media tree exceeds its bound.')
            actual_files[relative] = {'sha256': op.digest(path), 'bytes': info.st_size,
                                      'device': info.st_dev, 'inode': info.st_ino, 'links': info.st_nlink}
        facts[relative] = identity(info)
    require(equal(actual_files, files) and actual_directories == directories, 'Auxiliary media bytes, identities, or membership changed.')
    require(type(completed.get('generatedBytesExcludingManifest')) is int and completed['generatedBytesExcludingManifest'] == total,
            'The generation receipt byte total differs from the actual media population.')
    require(len({(row['device'], row['inode']) for row in actual_files.values()}) == len(files),
            'Auxiliary files unexpectedly share an inode.')
    # ProtectSystem=strict already covers this new tree. Verify the actual
    # service mount and file view; never add a mount or restart the service.
    mounts = {}
    for line in Path(f'/proc/{PID}/mountinfo').read_text().splitlines():
        fields = line.split()
        mounts[fields[4]] = set(fields[5].split(','))
    candidates = [path for path in mounts if str(ROOT) == path or str(ROOT).startswith(path.rstrip('/') + '/')]
    require(candidates and 'ro' in mounts[max(candidates, key=len)], 'The reference can write the new media tree.')
    for relative, fact in facts.items():
        observed = Path(f'/proc/{PID}/root') / str(ROOT).lstrip('/') / relative
        require(identity(observed.stat()) == fact, 'The existing service cannot see the exact new media member.')
    old = {'root': identity(op.canonical(op.MEDIA, directory=True)), 'manifest': identity(op.canonical(op.MEDIA / 'manifest.json')),
           'files': {name: identity(op.canonical(op.MEDIA / name, allow_links=True)) for name in owner['media']['files']}}
    require(op.verify_media() == owner['media'], 'The original media content changed.')
    return manifest, {'manifestSha256': expected_manifest, 'completedSha256': sha(completed_raw),
                      'root': identity(ROOT.stat()), 'members': facts, 'old': old}


class Recorder:
    def __init__(self, args, op, owner, browser, control, manifest, media):
        self.args, self.op, self.owner, self.browser = args, op, owner, browser
        self.control, self.manifest, self.media = control, manifest, media
        self.user = browser['accounts']['admin']
        self.device = control['deviceId']
        self.token, self.proven, self.revoked = None, False, False
        self.secrets = {account['password'] for account in browser['accounts'].values()}
        self.started, self.deadline = time.monotonic(), time.monotonic() + MAIN_SECONDS
        self.count, self.received_bytes, self.finishing = 0, 0, False
        self.intents, self.new_libraries, self.new_items, self.themes = set(), {}, {}, {}
        self.baseline, self.after, self.old_ids, self.refresh_baseline = None, None, [], None
        self.old_item_facts = {}
        self.allowed_gets = {'/emby/System/Info/Public', '/emby/Sessions', '/emby/Users',
                             '/emby/System/Configuration', '/emby/Library/VirtualFolders/Query'}
        self.phase = 'initialization'

    def check(self):
        require(time.monotonic() < self.deadline, 'The bounded operation deadline expired.')
        require(sha(protected_bytes(Path(__file__).absolute())) == self.args.script_sha256 and
                sha(protected_bytes(OPERATOR)) == self.args.operator_sha256 and
                sha(protected_bytes(self.op.OWNER)) == self.args.owner_sha256,
                'An exact operator or owner input changed during this operation.')
        require(equal(decode(protected_bytes(CONTROL / 'OWNER.json')), self.control), 'The independent control owner changed.')
        require(self.op.digest(self.op.REPORT) == self.owner['reportSha256'] and
                self.op.digest(self.op.BROWSER) == self.owner['browserSha256'], 'The existing reference evidence or credentials changed.')
        self.op.same_service(self.owner)
        require(os.readlink('/proc/self/ns/net') == self.owner['serviceIdentity']['networkNamespace'],
                'HTTP is outside the exact reference network namespace.')

    def register_get(self, path, query=None):
        route = path + ('?' + urlencode(query) if query else '')
        self.allowed_gets.add(route)
        return route

    def request(self, label, method, route, body=None, *, login=False, mutation=None):
        self.check()
        require(self.count < (MAX_REQUESTS if self.finishing else MAIN_REQUESTS), 'The HTTP request budget is exhausted.')
        require(re.fullmatch(r'[a-z0-9-]{1,80}', label), 'An evidence label is not bounded.')
        if method == 'GET':
            require(route in self.allowed_gets and body is None and not login and mutation is None, 'An unapproved read was requested.')
        else:
            require(method == 'POST' and isinstance(mutation, str) and mutation not in self.intents,
                    'A mutating operation cannot be repeated or adopted.')
            if login:
                require(route == '/emby/Users/AuthenticateByName' and mutation == 'login' and self.token is None and
                        body == {'Username': self.user['username'], 'Pw': self.user['password']}, 'The sole login differs from its owned credential.')
            elif route == '/emby/Sessions/Logout':
                require(mutation == 'logout' and body is None and self.proven, 'Logout is not bound to the proven new token.')
            elif route == '/emby/Library/VirtualFolders':
                name = mutation.removeprefix('create-')
                require(self.baseline is not None and mutation == 'create-' + name and name in LIBRARIES and
                        equal(body, library_body(self.op, name)), 'An auxiliary create differs from its reviewed intent.')
            else:
                name = mutation.removeprefix('refresh-')
                require(name in self.new_libraries and mutation == 'refresh-' + name and body == {} and
                        route == self.refresh_route(name), 'Only an acknowledged new library may be refreshed.')
        require(login or route == '/emby/System/Info/Public' or self.proven, 'The request lacks its proven new administrator session.')
        if mutation is not None and mutation.startswith(('create-', 'refresh-')):
            _, current_media = media_snapshot(self.op, self.owner, self.args.manifest_sha256)
            require(equal(self.media, current_media), 'The media changed before a new library mutation.')
        self.count += 1
        intent = {'marker': MARKER, 'sequence': self.count, 'phase': self.phase, 'label': label,
                  'method': method, 'route': route, 'mutation': mutation, 'deviceId': self.device,
                  'authenticated': self.proven and not login, 'body': None if login else body}
        save(self.op, PRIVATE / f'{self.count:04d}-{label}-intent.json', intent)
        if mutation is not None:
            self.intents.add(mutation)
        headers = {'Accept': 'application/json', 'Accept-Encoding': 'identity', 'Authorization':
                   f'Emby Client="{CLIENT}", Device="Linux Auxiliary Recorder", DeviceId="{self.device}", Version="1.0"'}
        if self.proven and not login:
            headers['X-Emby-Token'] = self.token
        payload = None if body is None else encoded(body)
        if payload is not None:
            require(len(payload) <= 32768, 'An outgoing JSON body exceeds its bound.')
            headers['Content-Type'] = 'application/json'
        connection = http.client.HTTPConnection('127.0.0.1', 18097, timeout=8)
        status, raw, value, complete, failure, content_type = None, b'', None, False, None, None
        signal.setitimer(signal.ITIMER_REAL, min(12, max(0.01, self.deadline - time.monotonic())))
        try:
            connection.request(method, route, payload, headers)
            response = connection.getresponse()
            status, content_type = response.status, response.getheader('Content-Type')
            require(not 300 <= status < 400, 'Redirects are not accepted by this fixed-target operator.')
            length = response.getheader('Content-Length')
            require(length is None or length.isdigit() and int(length) <= MAX_BODY, 'A response declares an invalid or excessive length.')
            raw = response.read(MAX_BODY + 1)
            require(len(raw) <= MAX_BODY and (length is None or int(length) == len(raw)), 'The response is incomplete or oversized.')
            self.received_bytes += len(raw)
            require(self.received_bytes <= MAX_TOTAL, 'The total response-byte budget is exhausted.')
            if raw:
                if content_type and content_type.lower().split(';', 1)[0].strip() == 'application/json':
                    value = decode(raw)
                else:
                    # The observed exact-token denial is a complete text/plain
                    # 401. Transport completeness does not require JSON.
                    value = raw.decode('utf-8')
            complete = True
            if login and isinstance(value, dict) and isinstance(value.get('AccessToken'), str) and value['AccessToken']:
                self.token = value['AccessToken']
                self.secrets.add(self.token)
                # Preserve the acknowledgement before any identity assertion.
                save(self.op, PRIVATE / 'recorder-login.json', value)
        except Exception as error:
            failure = type(error).__name__
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            connection.close()
            save(self.op, PRIVATE / f'{self.count:04d}-{label}-response.json',
                 {'status': status, 'contentType': content_type, 'complete': complete, 'bytes': len(raw),
                  'sha256': sha(raw), 'body': value, 'failureType': failure})
        if login and self.token is not None:
            self.proven = (status == 200 and complete and value.get('ServerId') == self.owner['serverId'] and
                value.get('User', {}).get('Id') == self.user['userId'] and value['User'].get('Name') == self.user['username'] and
                value['User'].get('Policy', {}).get('IsAdministrator') is True and
                value.get('SessionInfo', {}).get('DeviceId') == self.device and
                value['SessionInfo'].get('UserId') == self.user['userId'])
        require(failure is None and complete, 'A request outcome is incomplete; its mutation will not be retried.')
        self.check()
        if login:
            require(self.proven, 'The acknowledged token does not prove the exact server, administrator, and new device.')
        return status, value

    def get(self, label, route):
        status, body = self.request(label, 'GET', route)
        require(status == 200, 'A required read did not return HTTP 200.')
        return body

    def virtuals(self, label):
        body = self.get(label, '/emby/Library/VirtualFolders/Query')
        require(isinstance(body, dict) and isinstance(body.get('Items'), list) and len(body['Items']) <= 6 and
                body.get('TotalRecordCount', len(body['Items'])) == len(body['Items']), 'The virtual-library inventory is incomplete.')
        result = {}
        for row in body['Items']:
            require(isinstance(row, dict), 'A virtual-library row is malformed.')
            key = identifier(row.get('ItemId'))
            require(key not in result, 'A virtual-library identity is duplicated.')
            result[key] = row
        return result

    def user_rows(self, label):
        body = self.get(label, '/emby/Users')
        expected = {account['userId']: account['username'] for account in self.browser['accounts'].values()}
        require(isinstance(body, list) and 1 <= len(body) <= 16, 'The current account population exceeds its read bound.')
        result = {}
        for user in body:
            require(isinstance(user, dict) and isinstance(user.get('Id'), str) and re.fullmatch(r'[0-9a-f]{32}', user['Id']) and
                    isinstance(user.get('Name'), str) and 0 < len(user['Name']) <= 256 and
                    isinstance(user.get('Configuration'), dict) and isinstance(user.get('Policy'), dict), 'A reference account lacks its expected configuration or policy.')
            require(user['Id'] not in result, 'The reference account inventory repeats an identity.')
            result[user['Id']] = {key: user[key] for key in ('Id', 'Name', 'Configuration', 'Policy')}
        require(set(expected) <= set(result) and all(result[key]['Name'] == name for key, name in expected.items()),
                'An original browser account disappeared or changed its exact identity.')
        for key, account in self.browser['accounts'].items():
            require(result[account['userId']]['Policy'].get('IsAdministrator') is (key == 'admin'),
                    'An original account changed its administrator role.')
        return result

    def user_items(self, label, user_id, ids=None):
        query = {'Recursive': 'true', 'IncludeItemTypes': 'Movie,Episode,Audio', 'Fields': ITEM_FIELDS,
                 'Limit': '100', 'EnableTotalRecordCount': 'true', 'EnableUserData': 'true'}
        if ids is not None:
            query['Ids'] = ','.join(ids)
        route = self.register_get('/emby/Users/' + identifier(user_id) + '/Items', query)
        result = rows(self.get(label, route), 100)
        require(len(result) <= 6 and all(isinstance(row.get('UserData'), dict) for row in result.values()),
                'The original six-playable user-data witness is incomplete.')
        if user_id == self.user['userId']:
            require(len(result) == 6, 'The administrator cannot witness all six original playable items.')
        if ids is not None:
            require(set(result) <= set(ids) and all((row.get('Path'), row.get('Type')) == self.old_item_facts[key]
                    for key, row in result.items()), 'A user projection escaped the fixed original playable identities or paths.')
            if user_id == self.user['userId']:
                require(set(result) == set(ids), 'A fixed original playable identity disappeared or was replaced.')
        return result

    def snapshot(self, label, initial=False):
        result = {'libraries': self.virtuals(label + '-libraries'), 'users': self.user_rows(label + '-users'),
                  'configuration': self.get(label + '-configuration', '/emby/System/Configuration'), 'itemsByUser': {}}
        if initial:
            require(len(result['libraries']) == 3, 'The reference no longer contains only its three owned libraries.')
            original = self.op.read_private(self.op.REPORT)
            require(set(result['libraries']) == {identifier(row.get('ItemId')) for row in original['libraries']},
                    'An original virtual-library ID differs from its retained creation receipt.')
            for name in LIBRARIES:
                candidates = [row for row in result['libraries'].values() if row.get('Name') == 'M3e Reference ' + name]
                require(len(candidates) == 1, 'An original virtual library is missing or ambiguous.')
                self.op.verify_library(candidates[0], name)
            shapes = [refresh_shape(row) for row in result['libraries'].values()]
            save(self.op, PRIVATE / 'existing-refresh-shapes.json', shapes)
            # The retained 0022-libraries-final capture has neither field.
            # Any newly observed state needs review, not a guessed enum.
            require(all(shape == {} for shape in shapes), 'The original libraries expose an unreviewed refresh-state shape.')
            self.refresh_baseline = shapes[0]
            initial_items = self.user_items(label + '-admin-items', self.user['userId'])
            expected_paths = {str(self.op.MEDIA / name) for name in self.owner['media']['files']
                              if PurePosixPath(name).suffix in ('.mp4', '.mp3', '.flac')}
            require({row.get('Path') for row in initial_items.values()} == expected_paths and
                    Counter(row.get('Type') for row in initial_items.values()) == {'Movie': 1, 'Episode': 3, 'Audio': 2},
                    'The original playable paths or types differ from the fixed fixture.')
            original_leaves = {identifier(row.get('Id')): (row.get('Path'), row.get('Type')) for row in original['items']
                               if row.get('Type') in ('Movie', 'Episode', 'Audio')}
            require({key: (row.get('Path'), row.get('Type')) for key, row in initial_items.items()} == original_leaves,
                    'An original playable ID, path, or type differs from its retained creation receipt.')
            self.old_ids = sorted(initial_items)
            self.old_item_facts = {key: (row['Path'], row['Type']) for key, row in initial_items.items()}
            result['itemsByUser'][self.user['userId']] = initial_items
        elif self.baseline is not None:
            require(set(result['users']) == set(self.baseline['users']), 'The current account population changed during the operation.')
        for number, user_id in enumerate(sorted(result['users'])):
            if user_id not in result['itemsByUser']:
                result['itemsByUser'][user_id] = self.user_items(f'{label}-user-{number:02d}-items', user_id, self.old_ids)
        save(self.op, PRIVATE / (label + '-snapshot.json'), result)
        return result

    def preserve(self, result):
        require(self.baseline is not None and equal(self.baseline['users'], result['users']) and
                equal(self.baseline['configuration'], result['configuration']) and
                equal(self.baseline['itemsByUser'], result['itemsByUser']), 'Original metadata, UserData, configuration, or policy changed.')
        require(all(key in result['libraries'] and equal(row, result['libraries'][key])
                    for key, row in self.baseline['libraries'].items()), 'An original virtual library changed or disappeared.')

    def assert_library_population(self, libraries, pending=None):
        self.preserve_libraries(libraries)
        expected_names = {'M3e Auxiliary ' + name for name in self.new_libraries}
        if pending is not None:
            expected_names.add('M3e Auxiliary ' + pending)
        added = [row for key, row in libraries.items() if key not in self.baseline['libraries']]
        require(len(added) == len(expected_names) and {row.get('Name') for row in added} == expected_names,
                'An unowned or duplicate library appeared during the operation.')
        for name, identifier_value in self.new_libraries.items():
            require(identifier_value in libraries and verify_new_library(self.op, libraries[identifier_value], name) == identifier_value,
                    'A newly acknowledged library changed identity.')

    def preserve_libraries(self, libraries):
        require(all(key in libraries and equal(row, libraries[key]) for key, row in self.baseline['libraries'].items()),
                'An original virtual library changed during a scoped operation.')

    def refresh_route(self, name):
        return '/emby/Items/' + identifier(self.new_libraries[name]) + '/Refresh?' + urlencode(REFRESH_QUERY)

    def members(self, name, round_number):
        route = self.register_get('/emby/Users/' + self.user['userId'] + '/Items',
                                 {'ParentId': self.new_libraries[name], 'Recursive': 'true', 'Fields': ITEM_FIELDS,
                                  'Limit': '256', 'EnableTotalRecordCount': 'true', 'EnableUserData': 'true'})
        result = rows(self.get(f'{name.lower()}-members-{round_number:02d}', route))
        require(not set(result).intersection(self.old_ids) and not set(result).intersection(self.baseline['libraries']),
                'A new library reused an original library or playable identity.')
        prefix = str(ROOT / name)
        for row in result.values():
            path = row.get('Path')
            require(path is None or path == '' or isinstance(path, str) and
                    (path == prefix or path.startswith(prefix + '/')) and '..' not in PurePosixPath(path).parts,
                    'A scoped library member exposes an unowned physical path.')
        return result

    def primary_ready(self, name, members):
        expected = [row for row in self.manifest['expectedItems'] if row['media'].startswith(name + '/')]
        identities = {}
        for item in expected:
            matches = [row for row in members.values() if row.get('Path') == str(ROOT / item['media'])]
            if len(matches) != 1:
                return None
            actual = matches[0]
            require(actual.get('Type') == item['kind'], 'A primary fixture path has an unexpected actual item type.')
            sources = actual.get('MediaSources')
            if not isinstance(sources, list) or not sources:
                return None
            streams = [stream for source in sources if isinstance(source, dict)
                       for stream in source.get('MediaStreams', []) if isinstance(stream, dict)]
            required = {'Audio'} if item['kind'] == 'Audio' else {'Audio', 'Video'}
            if not required <= {stream.get('Type') for stream in streams}:
                return None
            identities[item['media']] = actual['Id']
        return identities

    def create_libraries(self):
        for name in LIBRARIES:
            self.phase = 'create-' + name
            current = self.virtuals(name.lower() + '-before-create')
            self.assert_library_population(current)
            proposed = library_body(self.op, name)
            require(not any(row.get('Name') == proposed['Name'] or str(ROOT / name) in row.get('Locations', [])
                            for row in current.values()), 'A proposed name or path already exists; it cannot be adopted.')
            status, body = self.request(name.lower() + '-create', 'POST', '/emby/Library/VirtualFolders', proposed, mutation='create-' + name)
            require(status in (200, 204) and body in (None, {}), 'The new virtual-library create was not acknowledged.')
            current = self.virtuals(name.lower() + '-created')
            self.assert_library_population(current, pending=name)
            matches = [row for row in current.values() if row.get('Name') == proposed['Name']]
            require(len(matches) == 1, 'The acknowledged new library could not be uniquely enumerated.')
            new_id = verify_new_library(self.op, matches[0], name)
            require(new_id not in self.baseline['libraries'] and new_id not in self.new_libraries.values(),
                    'A new library reused an existing identity.')
            self.new_libraries[name] = new_id
            save(self.op, PRIVATE / (name.lower() + '-library-acknowledged.json'), matches[0])
            self.phase = 'refresh-' + name
            status, body = self.request(name.lower() + '-refresh', 'POST', self.refresh_route(name), {}, mutation='refresh-' + name)
            require(status in (200, 204) and body in (None, {}), 'The single-library refresh was not acknowledged.')
            previous, stable, bound = None, 0, time.monotonic() + 100
            for number in range(32):
                require(time.monotonic() < bound, 'The scoped scan exceeded its observation deadline.')
                libraries = self.virtuals(f'{name.lower()}-scan-state-{number:02d}')
                self.assert_library_population(libraries)
                observed = self.members(name, number)
                primary = self.primary_ready(name, observed)
                candidate = {'library': libraries[new_id], 'members': observed, 'primary': primary}
                ready = primary is not None and equal(refresh_shape(libraries[new_id]), self.refresh_baseline)
                stable = stable + 1 if ready and previous is not None and equal(candidate, previous) else int(ready)
                if stable >= 2:
                    self.new_items[name] = candidate
                    save(self.op, PRIVATE / (name.lower() + '-scan-observed.json'), candidate)
                    break
                previous = candidate if ready else None
                time.sleep(2)
            require(name in self.new_items, 'The primary members and observed refresh shape did not stabilize; no refresh retry is permitted.')

    def theme_observations(self):
        targets = {}
        for key in ('Seed', 'AllMatches', 'NoShared'):
            expected = next(row for row in self.manifest['expectedItems'] if row.get('key') == key and row['kind'] == 'Movie')
            targets[key] = self.new_items['Movies']['primary'][expected['media']]
        tv = self.new_items['TV']['members']
        for key, kind, path in (('Series', 'Series', ROOT / 'TV/M3e Auxiliary Series'),
                                ('Season02', 'Season', ROOT / 'TV/M3e Auxiliary Series/Season 02')):
            matches = [row for row in tv.values() if row.get('Type') == kind and row.get('Path') == str(path)]
            require(len(matches) == 1, 'An auxiliary theme target has no unique observed hierarchy identity.')
            targets[key] = matches[0]['Id']
        for key, item_id in targets.items():
            route = self.register_get('/emby/Items/' + identifier(item_id) + '/ThemeMedia',
                                      {'UserId': self.user['userId'], 'InheritFromParent': 'false',
                                       'EnableThemeSongs': 'true', 'EnableThemeVideos': 'true'})
            body = self.get('theme-' + key.lower(), route)
            require(isinstance(body, dict), 'The observed ThemeMedia result is not an object.')
            self.themes[key] = {'itemId': item_id, 'groups': {}}
            for group in ('ThemeSongsResult', 'ThemeVideosResult', 'SoundtrackSongsResult'):
                value = body.get(group)
                require(isinstance(value, dict) and isinstance(value.get('Items'), list) and len(value['Items']) <= 32 and
                        type(value.get('TotalRecordCount')) is int and value['TotalRecordCount'] >= 0,
                        'An observed theme group is not a bounded count/list object.')
                self.themes[key]['groups'][group] = {'ownerId': value.get('OwnerId'), 'totalRecordCount': value['TotalRecordCount'],
                                                    'returnedItems': [self.item_export(row) for row in value['Items']]}

    @staticmethod
    def item_export(row):
        require(isinstance(row, dict), 'A result item is not an object.')
        return {key: row[key] for key in SAFE_ITEM_FIELDS if key in row}

    def primary_evidence(self, observed):
        result = {}
        source_fields = ('Id', 'Path', 'Container', 'RunTimeTicks', 'Size')
        stream_fields = ('Index', 'Type', 'Codec', 'Channels', 'SampleRate', 'Width', 'Height',
                         'BitRate', 'BitDepth', 'AverageFrameRate', 'RealFrameRate')
        for relative, item_id in observed['primary'].items():
            actual = observed['members'][item_id]
            witnesses = []
            for source in actual['MediaSources']:
                require(isinstance(source, dict), 'A primary media-source witness is malformed.')
                witness = {field: source[field] for field in source_fields if field in source}
                witness['MediaStreams'] = [{field: stream[field] for field in stream_fields if field in stream}
                                          for stream in source.get('MediaStreams', []) if isinstance(stream, dict)]
                witnesses.append(witness)
            result[relative] = {'itemId': item_id, 'manifestExpected': next(row for row in self.manifest['expectedItems'] if row['media'] == relative),
                                'generatedProfile': self.manifest['profiles'][relative], 'actualMetadata': self.item_export(actual),
                                'actualMediaSources': witnesses}
        return result

    def sanitize(self, value):
        if isinstance(value, dict):
            return {key: ('[redacted]' if key.lower() in {'accesstoken', 'token', 'password', 'pw', 'authorization', 'cookie', 'set-cookie'}
                          else self.sanitize(item)) for key, item in value.items()}
        if isinstance(value, list):
            return [self.sanitize(item) for item in value]
        if isinstance(value, str):
            for secret in sorted(self.secrets, key=len, reverse=True):
                value = value.replace(secret, '[redacted]')
            return URL.sub('[redacted URL]', value)
        return value

    def logout(self):
        if self.token is None:
            return
        require(self.proven, 'An unproven acknowledgement requires independent credential review; no unknown token is revoked.')
        status, _ = self.request('recorder-logout', 'POST', '/emby/Sessions/Logout', mutation='logout')
        check, _ = self.request('recorder-exact-token-invalid', 'GET', '/emby/Sessions')
        require(status == 204 and check == 401, 'The new recorder did not complete exact-token 204-to-401 logout.')
        self.revoked = True
        save(self.op, PRIVATE / 'recorder-revocation.json', {'marker': MARKER, 'deviceId': self.device,
                                                          'logoutStatus': status, 'invalidTokenStatus': check})

    def run(self):
        failure, preserved, media_preserved = None, False, False
        try:
            public = self.get('public-identity', '/emby/System/Info/Public')
            require(isinstance(public, dict) and public.get('Id') == self.owner['serverId'] and public.get('Version') == '4.9.5.0',
                    'The public reference server identity differs.')
            self.phase = 'login'
            self.request('recorder-login', 'POST', '/emby/Users/AuthenticateByName',
                         {'Username': self.user['username'], 'Pw': self.user['password']}, login=True, mutation='login')
            self.phase = 'baseline'
            self.baseline = self.snapshot('before', initial=True)
            self.create_libraries()
            self.phase = 'theme-observation'
            self.theme_observations()
        except Exception as error:
            failure = {'type': type(error).__name__, 'phase': self.phase}
        finally:
            self.finishing, self.deadline = True, time.monotonic() + 70
            try:
                if self.proven and self.baseline is not None:
                    self.phase = 'preservation'
                    self.after = self.snapshot('after')
                    self.preserve(self.after)
                    if failure is None:
                        self.assert_library_population(self.after['libraries'])
                        require(set(self.new_libraries) == set(LIBRARIES) and set(self.new_items) == set(LIBRARIES),
                                'Not all three scoped operations completed.')
                    preserved = True
            except Exception as error:
                failure = failure or {'type': type(error).__name__, 'phase': self.phase}
            try:
                self.phase = 'logout'
                # Preservation reads cannot consume the recorder's independent
                # cleanup budget. This never authorizes another login or retry.
                self.deadline = time.monotonic() + 50
                self.logout()
            except Exception as error:
                failure = failure or {'type': type(error).__name__, 'phase': self.phase}
            try:
                self.phase = 'media-preservation'
                self.deadline = time.monotonic() + 30
                _, after_media = media_snapshot(self.op, self.owner, self.args.manifest_sha256)
                require(equal(self.media, after_media), 'Original or auxiliary media identities changed during the operation.')
                require(self.op.digest(self.op.BROWSER) == self.owner['browserSha256'] and
                        self.op.digest(self.op.REPORT) == self.owner['reportSha256'], 'Existing reference evidence changed.')
                self.check()
                media_preserved = True
            except Exception as error:
                failure = failure or {'type': type(error).__name__, 'phase': self.phase}
        catalog = {}
        for name, observed in self.new_items.items():
            primary_paths = {row['media'] for row in self.manifest['expectedItems']}
            auxiliary_paths = {path for path in self.manifest['profiles'] if path.startswith(name + '/') and path not in primary_paths}
            catalog[name] = {'libraryId': self.new_libraries[name], 'refreshShape': refresh_shape(observed['library']),
                             'actualTypeCounts': dict(Counter(row.get('Type', '<missing>') for row in observed['members'].values())),
                             'primaryIdsByManifestPath': observed['primary'],
                             'primaryEvidence': self.primary_evidence(observed),
                             'nonprimaryMediaInScopedRecursiveCatalog': {path: [row['Id'] for row in observed['members'].values()
                                                                                  if row.get('Path') == str(ROOT / path)] for path in sorted(auxiliary_paths)},
                             'items': [self.item_export(row) for row in observed['members'].values()]}
        report = {'marker': MARKER, 'result': 'complete' if failure is None and self.revoked and preserved and media_preserved else 'retained_for_review',
                  'scriptSha256': self.args.script_sha256, 'operatorSha256': self.args.operator_sha256,
                  'ownerSha256': self.args.owner_sha256, 'manifestSha256': self.args.manifest_sha256,
                  'referencePID': PID, 'referenceStartTicks': TICKS, 'serverId': self.owner['serverId'],
                  'newRecorderDeviceId': self.device, 'newRecorderTokenRevoked': self.revoked,
                  'originalLibrariesMetadataUserDataConfigurationPolicyPreserved': preserved, 'allMediaPreserved': media_preserved,
                  'existingUserIds': sorted(self.baseline['users']) if self.baseline is not None else [],
                  'originalVisibleItemIdsByUser': {user_id: sorted(items) for user_id, items in self.baseline['itemsByUser'].items()}
                      if self.baseline is not None else {},
                  'userDataScope': 'The administrator witnesses all six original playables; every other current user keeps its entire visible subset of those six, without changing permissions.',
                  'requests': self.count, 'bytesRead': self.received_bytes, 'mutatingIntentsReserved': sorted(self.intents),
                  'newLibraryIds': self.new_libraries, 'catalog': catalog, 'themeObservations': self.themes, 'failure': failure,
                  'scanEvidence': 'Refresh acceptance plus complete expected primary paths and two unchanged observations matching the observed original-library refresh shape.',
                  'themeBoundary': 'Theme membership, owner IDs, and main-catalog presence are observations; no theme exclusion or inheritance behavior is assumed.'}
        safe = self.sanitize(report)
        require(not any(secret in encoded(safe).decode() for secret in self.secrets), 'A recorder credential survived safe export.')
        save(self.op, EXPORT / 'report.json', safe)
        print(json.dumps({'result': report['result'], 'report': str(EXPORT / 'report.json'),
                          'newRecorderTokenRevoked': self.revoked, 'newLibraryIds': self.new_libraries}), flush=True)
        return 0 if report['result'] == 'complete' else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('script', 'operator', 'owner', 'manifest'):
        parser.add_argument('--' + name + '-sha256', required=True)
    parser.add_argument('--worker', action='store_true', help=argparse.SUPPRESS)
    parser.add_argument('--lock-fd', type=int, help=argparse.SUPPRESS)
    args = parser.parse_args()
    os.umask(0o077)
    signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(TimeoutError('A bounded request expired.')))
    op, owner, browser = load_inputs(args)
    manifest, media = media_snapshot(op, owner, args.manifest_sha256)
    inputs = {name: getattr(args, name + '_sha256') for name in ('script', 'operator', 'owner', 'manifest')}
    if args.worker:
        require(type(args.lock_fd) is int and args.lock_fd > 2, 'The worker lacks its inherited supervisor lock.')
        control = decode(protected_bytes(CONTROL / 'OWNER.json'))
        require(control.get('marker') == MARKER and control.get('path') == str(CONTROL) and equal(control.get('inputs'), inputs) and
                equal(op.process_identity(os.getppid()), control.get('supervisor')) and
                equal(identity(os.fstat(args.lock_fd)), identity(op.canonical(op.LOCK, mode=0o600))) and
                os.readlink('/proc/self/ns/net') == owner['serviceIdentity']['networkNamespace'],
                'The internal worker is not bound to this live supervisor, lock, inputs, and namespace.')
        require(re.fullmatch(r'goby-m3e-auxiliary-library-[0-9a-f]{32}', control.get('deviceId', '')),
                'The new recorder device is not canonical.')
        require(equal(control.get('media'), media), 'The media changed between supervisor and worker.')
        save(op, PRIVATE / 'worker.json', op.process_identity(os.getpid()))
        return Recorder(args, op, owner, browser, control, manifest, media).run()
    require(args.lock_fd is None and not op.present(CONTROL), 'Existing auxiliary control evidence cannot be adopted or retried.')
    op.canonical(op.LOCK, mode=0o600)
    lock = os.open(op.LOCK, os.O_RDWR | os.O_NOFOLLOW)
    namespace = None
    try:
        require(equal(identity(os.fstat(lock)), identity(op.canonical(op.LOCK, mode=0o600))), 'The reference lock changed while opening.')
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        require(not op.present(CONTROL), 'The auxiliary control path became occupied.')
        CONTROL.mkdir(mode=0o700)
        PRIVATE.mkdir(mode=0o700)
        EXPORT.mkdir(mode=0o700)
        control = {'marker': MARKER, 'path': str(CONTROL), 'inputs': inputs, 'serviceIdentity': owner['serviceIdentity'],
                   'supervisor': op.process_identity(os.getpid()), 'deviceId': 'goby-m3e-auxiliary-library-' + secrets.token_hex(16),
                   'media': media, 'newLibraries': {name: library_body(op, name) for name in LIBRARIES}}
        save(op, CONTROL / 'OWNER.json', control)
        op.sync_directory(WORK)
        namespace = os.open(f'/proc/{PID}/ns/net', os.O_RDONLY)
        require(os.fstat(namespace).st_ino == int(owner['serviceIdentity']['networkNamespace'][5:-1]), 'The namespace handle changed.')
        op.same_service(owner)
        require(sha(protected_bytes(Path(__file__).absolute())) == args.script_sha256, 'The worker source changed before dispatch.')
        arguments = ['/usr/bin/nsenter', '--net=/proc/self/fd/' + str(namespace), '/usr/bin/python3', '-I', '-B',
                     str(Path(__file__).absolute()), '--worker', '--lock-fd', str(lock)]
        for name, digest in inputs.items():
            arguments.extend(['--' + name + '-sha256', digest])
        save(op, PRIVATE / 'worker-dispatch-intent.json', {'marker': MARKER, 'inputs': inputs, 'supervisor': control['supervisor']})
        result = subprocess.run(arguments, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=MAIN_SECONDS + CLEANUP_SECONDS + 45, check=False, pass_fds=(lock, namespace),
                                env=dict(op.BASE_ENV, SSH_CONNECTION=os.environ['SSH_CONNECTION']))
        require(len(result.stdout) <= 8192 and len(result.stderr) <= 8192, 'The worker exceeded its bounded terminal output.')
        save(op, PRIVATE / 'worker-exit.json', {'returnCode': result.returncode, 'stdoutSha256': sha(result.stdout), 'stderrSha256': sha(result.stderr)})
        print(json.dumps({'result': 'complete' if result.returncode == 0 else 'retained_for_review',
                          'report': str(EXPORT / 'report.json'), 'evidence': str(CONTROL)}), flush=True)
        return 0 if result.returncode == 0 else 1
    finally:
        if namespace is not None:
            os.close(namespace)
        os.close(lock)


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except Exception as error:
        print(json.dumps({'result': 'retained_for_review', 'failureType': type(error).__name__,
                          'evidence': str(CONTROL), 'retryPermitted': False}), file=sys.stderr)
        raise SystemExit(1)
