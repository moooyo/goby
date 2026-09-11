#!/usr/bin/env python3
"""Index two owned movies, then refresh and capture their local extras.

Each phase is exclusive and cannot be retried. --check-only reads protected
inputs and process/media identities without HTTP or new evidence. The actual
worker uses only the fixed reference namespace and new library. All source
dependencies are bound to the retained, completed auxiliary-library report.
"""

from __future__ import annotations

import argparse
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
MEDIA = Path('/opt/goby-fixtures/client-special-features-m3e-v1')
CONTROL = WORK / 'reference-special-features-v1'
GENERATOR_CONTROL = WORK / 'special-features-media-v1'
INDEXED = CONTROL / 'mains-indexed.json'
AUXILIARY_INDEX = WORK / 'reference-auxiliary-libraries-v1/export/report.json'
AUXILIARY_INDEX_SHA = '457a4d88f86617c448b980f31b7dd5b7d8bbeb31e3cfe807103371859f2d7998'
MEDIA_MARKER = 'goby-client-special-features-media-m3e-v1'
MARKER = 'goby-reference-special-features-m3e-v1'
LIBRARY_NAME = 'M3e Special Features Movies'
REFERENCE_SHA = 'c109c9817dea25cc516b9969a87aa1ffa48e41adcb5e3dbc87d686c7bcb28ac2'
GENERATOR_SHA = '5fb72eaca167b087fc68a599cd82db0010468f98c00b6cb7be15be11ec901887'
PID, TICKS = 332054, '357218'
OLD_ROOTS = (Path('/opt/goby-fixtures/client-m3e'), Path('/opt/goby-fixtures/client-aux-m3e-v1'))
MAIN_DIRS = {key: f'Movies/M3e Special Features {name} (2026)' for key, name in
             (('positive', 'Positive'), ('empty', 'Empty'))}
MAIN_PATHS = {key: directory + '/' + PurePosixPath(directory).name + '.mp4' for key, directory in MAIN_DIRS.items()}
EXTRA_PATHS = {MAIN_DIRS['positive'] + '/' + name for name in (
    'featurettes/Zeta Bonus.mp4', 'featurettes/Alpha Bonus.mp4',
    'deleted scenes/Middle Deleted Scene.mp4', 'trailers/Delta Local Trailer.mp4',
    'featurettes/nested/Hidden Nested.mp4', 'featurettes/notes.txt')}
FIELDS = 'Path,ParentId,SortName,MediaSources,MediaStreams,Overview,Genres,Tags,People,Studios,ProviderIds,DateCreated,ProductionYear'
TYPES = 'Movie,Series,Season,Episode,MusicAlbum,Audio,Video,Folder'
REFRESH = {'Recursive': 'true', 'MetadataRefreshMode': 'FullRefresh', 'ImageRefreshMode': 'ValidationOnly'}
SHA = re.compile(r'[0-9a-f]{64}')
ID = re.compile(r'[A-Za-z0-9_-]{1,128}')
MAX_BODY, MAX_TOTAL = 2 << 20, 96 << 20
REVOCATION_BODY, REVOCATION_TOTAL = 64 << 10, 256 << 10


def require(value, message):
    if not value:
        raise RuntimeError(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def file_version(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid, info.st_nlink,
            info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def encoded(value):
    return (json.dumps(value, sort_keys=True, ensure_ascii=True, separators=(',', ':'), allow_nan=False) + '\n').encode()


def same(left, right):
    return encoded(left) == encoded(right)


def safe_id(value):
    require(isinstance(value, str) and ID.fullmatch(value), 'An identifier cannot enter the fixed route grammar.')
    return value


def directory_identity(support, op, path, mode):
    info = support.identity(op.canonical(path, directory=True, mode=mode))
    return {key: info[key] for key in ('device', 'inode', 'uid', 'gid', 'mode')}


def load_module(path, raw, name):
    module = types.ModuleType(name)
    module.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), module.__dict__)
    return module


def protected_source(path, limit=2 << 20):
    require(path.is_absolute() and '..' not in path.parts, 'An input path is not absolute and canonical.')
    for parent in reversed(path.parents):
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                'An input ancestor is not protected.')
    before = path.lstat()
    require(stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and before.st_nlink == 1 and
            stat.S_IMODE(before.st_mode) in (0o600, 0o644) and before.st_size <= limit, 'An input file is not protected.')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as handle:
        opened = os.fstat(handle.fileno())
        require((opened.st_dev, opened.st_ino) == (before.st_dev, before.st_ino), 'An input was replaced while opening.')
        raw = handle.read(limit + 1)
        require(file_version(os.fstat(handle.fileno())) == file_version(before), 'An input changed while reading.')
    require(file_version(path.lstat()) == file_version(before) and len(raw) == before.st_size, 'An input changed after reading.')
    return raw


def load_inputs(args):
    require(sys.platform == 'linux' and os.geteuid() == 0 and os.environ.get('SSH_CONNECTION'),
            'Use authorized root SSH on test-env.')
    for key, value in vars(args).items():
        if key.endswith('_sha256') and value is not None:
            require(SHA.fullmatch(value) is not None, 'An explicit input digest is invalid.')
    require(sha(protected_source(Path(__file__).absolute())) == args.script_sha256, 'The selected operator changed.')
    index_raw = protected_source(AUXILIARY_INDEX)
    require(args.auxiliary_index_sha256 == AUXILIARY_INDEX_SHA and sha(index_raw) == AUXILIARY_INDEX_SHA,
            'The completed auxiliary-library receipt changed.')
    index = json.loads(index_raw)
    require(index.get('result') == 'complete' and index.get('failure') is None and
            index.get('newRecorderTokenRevoked') is True and index.get('allMediaPreserved') is True and
            index.get('originalLibrariesMetadataUserDataConfigurationPolicyPreserved') is True and
            len(index.get('existingUserIds', [])) == 5 and len(set(index['existingUserIds'])) == 5,
            'The retained six-library/five-user provenance is incomplete.')
    require(args.support_path.name == 'add-client-auxiliary-reference.py' and args.support_path.is_relative_to(WORK) and
            args.support_sha256 == index['scriptSha256'] and args.operator_sha256 == index['operatorSha256'] and
            args.owner_sha256 == index['ownerSha256'], 'The dependency selection differs from retained accepted evidence.')
    raw = protected_source(args.support_path)
    require(sha(raw) == args.support_sha256, 'The accepted auxiliary operator changed.')
    support = load_module(args.support_path, raw, 'accepted_special_features_support')
    op, owner, browser = support.load_inputs(types.SimpleNamespace(
        script_sha256=args.support_sha256, operator_sha256=args.operator_sha256,
        owner_sha256=args.owner_sha256, manifest_sha256=index['manifestSha256']))
    require(owner['serviceIdentity']['pid'] == PID and owner['serviceIdentity']['startTicks'] == TICKS and
            index['serverId'] == owner['serverId'], 'The fresh reference identity changed.')
    original = op.read_private(op.REPORT)
    old_libraries = {safe_id(row['ItemId']) for row in original['libraries']} | set(index['newLibraryIds'].values())
    require(len(old_libraries) == 6, 'The old library identity set is not exactly six.')
    return support, op, owner, browser, index, old_libraries


def new_media(args, support, op, owner, index):
    manifest_path = MEDIA / (args.phase + '-manifest.json')
    completed_path = GENERATOR_CONTROL / args.phase / 'completed.json'
    manifest_raw, completed_raw = protected_source(manifest_path), protected_source(completed_path)
    require(sha(manifest_raw) == args.media_manifest_sha256 and sha(completed_raw) == args.media_completed_sha256,
            'The selected generator phase changed.')
    manifest, completed = support.decode(manifest_raw), support.decode(completed_raw)
    require(manifest.get('marker') == MEDIA_MARKER and manifest.get('version') == 1 and manifest.get('phase') == args.phase and
            manifest.get('root') == str(MEDIA) and manifest.get('control') == str(GENERATOR_CONTROL) and
            manifest.get('generator_sha256') == GENERATOR_SHA and manifest.get('http_requests') == 0 and
            manifest.get('library_scan_executed') is False,
            'The media manifest identifies another fixture or generator.')
    require(completed.get('marker') == MEDIA_MARKER and completed.get('version') == 1 and
            completed.get('phase') == args.phase and completed.get('status') == 'completed' and completed.get('root') == str(MEDIA) and
            completed.get('control') == str(GENERATOR_CONTROL) and
            completed.get('generator_sha256') == manifest['generator_sha256'] and
            completed.get('manifest', {}).get('path') == str(manifest_path) and
            completed['manifest'].get('sha256') == args.media_manifest_sha256 and
            completed.get('protected_media_preserved') is True and completed.get('library_scan_executed') is False and
            completed.get('http_requests') == 0 and completed.get('protected_before_sha256') == completed.get('protected_after_sha256') and
            SHA.fullmatch(completed.get('protected_after_sha256', '')), 'The generator completion is not an accepted media-only phase.')
    generator_owner = support.decode(protected_source(GENERATOR_CONTROL / 'OWNER.json'))
    require(generator_owner.get('marker') == MEDIA_MARKER and generator_owner.get('version') == 1 and
            generator_owner.get('root') == str(MEDIA) and generator_owner.get('control') == str(GENERATOR_CONTROL) and
            generator_owner.get('generator_sha256') == GENERATOR_SHA and
            generator_owner.get('control_identity') == directory_identity(support, op, GENERATOR_CONTROL, 0o700),
            'The generator control directory lost its original identity.')
    root_fields = ('device', 'inode', 'uid', 'gid', 'mode')
    root_identity = {key: support.identity(op.canonical(MEDIA, directory=True, mode=0o755))[key] for key in root_fields}
    require(root_identity == manifest['root_identity'], 'The new media root was replaced.')
    actual_files, actual_directories, total = {}, {}, 0
    for path in sorted(MEDIA.rglob('*')):
        relative = path.relative_to(MEDIA).as_posix()
        info = path.lstat()
        if stat.S_ISDIR(info.st_mode):
            info = op.canonical(path, directory=True, mode=0o755)
            actual_directories[relative] = {key: support.identity(info)[key] for key in root_fields}
        else:
            info = op.canonical(path, mode=0o644)
            require(info.st_size <= 16 << 20, 'A generated media member exceeds its reviewed bound.')
            total += info.st_size
            require(total <= 32 << 20, 'The generated media tree exceeds its reviewed bound.')
            actual_files[relative] = {'sha256': op.digest(path), 'bytes': info.st_size, 'identity': support.identity(info)}
    require(actual_directories == manifest['directories'] and
            {key: value for key, value in actual_files.items() if key != manifest_path.name} == manifest['files'] and
            completed['manifest'].get('identity') == actual_files[manifest_path.name]['identity'] and
            completed.get('media_snapshot') == {'root_identity': root_identity, 'directories': actual_directories, 'files': actual_files},
            'The complete generated media membership, bytes or identities changed.')
    expected_files = {'.goby-managed', *MAIN_PATHS.values(), *(directory + '/movie.nfo' for directory in MAIN_DIRS.values()), 'mains-manifest.json'}
    if args.phase == 'extras':
        expected_files |= EXTRA_PATHS | {'extras-manifest.json'}
    require(set(actual_files) == expected_files and protected_source(MEDIA / '.goby-managed') == (MEDIA_MARKER + '\n').encode(),
            'The generated phase has unexpected media or controls.')
    mounts = {}
    for line in Path(f'/proc/{PID}/mountinfo').read_text().splitlines():
        fields = line.split()
        mounts[fields[4]] = set(fields[5].split(','))
    candidates = [path for path in mounts if str(MEDIA) == path or str(MEDIA).startswith(path.rstrip('/') + '/')]
    require(candidates and 'ro' in mounts[max(candidates, key=len)], 'The reference can write the new media tree.')
    service_root = Path(f'/proc/{PID}/root') / str(MEDIA).lstrip('/')
    require(support.identity(service_root.stat()) == support.identity(MEDIA.stat()), 'The reference sees another new media root.')
    for relative, fact in actual_files.items():
        require(support.identity((service_root / relative).stat()) == fact['identity'], 'The reference sees another generated file.')
    for relative in actual_directories:
        require(support.identity((service_root / relative).stat()) == support.identity((MEDIA / relative).stat()),
                'The reference sees another generated directory.')
    items = {item.get('fixture_id'): item for item in manifest.get('items', [])}
    require(set(items) == set(MAIN_PATHS) and len(manifest['items']) == 2 and all(
        item.get('intended_type') == 'Movie' and item.get('media') == MAIN_PATHS[key] and
        item.get('nfo') == MAIN_DIRS[key] + '/movie.nfo' and item.get('year') == 2026 and
        item.get('title') == 'M3e Special Features ' + ('Positive' if key == 'positive' else 'Empty') for key, item in items.items()),
        'The two main-movie identities differ from the selected fixture.')
    mains = None
    if args.phase == 'extras':
        require(args.mains_indexed_sha256 is not None, 'Extras require the completed mains-indexed receipt.')
        raw = protected_source(INDEXED)
        require(sha(raw) == args.mains_indexed_sha256, 'The mains-indexed receipt changed.')
        mains = support.decode(raw)
        require(mains.get('schema') == 'goby-client-special-features-mains-indexed' and mains.get('version') == 1 and
                mains.get('status') == 'indexed' and mains.get('no_extras') is True and mains.get('media_root') == str(MEDIA) and
                mains.get('media_root_identity') == root_identity and mains.get('generator_sha256') == manifest['generator_sha256'] and
                mains.get('reference', {}).get('server_id') == owner['serverId'] and
                mains['reference'].get('service') == 'goby-emby-client-m3e.service' and
                mains['reference'].get('binary_sha256') == REFERENCE_SHA and mains['reference'].get('library_path') == str(MEDIA / 'Movies') and
                set(mains.get('items', {})) == set(MAIN_PATHS), 'The first-stage indexing proof is incomplete.')
        safe_id(mains['reference']['library_id'])
        require(len({mains['reference']['library_id'], *(item['Id'] for item in mains['items'].values())}) == 3,
                'The library and main identities are not distinct.')
        for key, item in mains['items'].items():
            safe_id(item.get('Id'))
            require(item.get('Type') == 'Movie' and item.get('Path') == str(MEDIA / MAIN_PATHS[key]), 'A main movie was rebound.')
        require(Path(mains['evidence']['path']) == CONTROL / 'mains/export/report.json' and
                sha(protected_source(Path(mains['evidence']['path']))) == mains['evidence']['sha256'], 'The first-stage evidence changed.')
        report = support.decode(protected_source(Path(mains['evidence']['path'])))
        require(report.get('marker') == MARKER and report.get('phase') == 'mains' and report.get('result') == 'complete' and
                report.get('noExtras') is True and report.get('oldLibrariesAndFiveUsersPreserved') is True and
                report.get('allMediaPreserved') is True and report.get('newTokensRevoked') is True and
                report.get('mainItems') == mains['items'] and report.get('libraryId') == mains['reference']['library_id'] and
                report.get('mediaManifestSha256') == mains['mains_manifest_sha256'] and
                report.get('mediaCompletedSha256') == mains['mains_completed_sha256'], 'The first-stage report does not prove its indexing receipt.')
        for record in (manifest, completed):
            require(record.get('index_receipt_path') == str(INDEXED) and record.get('index_receipt_sha256') == args.mains_indexed_sha256 and
                    record.get('mains_manifest_sha256') == mains['mains_manifest_sha256'] and
                    record.get('mains_completed_sha256') == mains['mains_completed_sha256'], 'The extras phase broke its indexing lineage.')
        require(actual_files['mains-manifest.json']['sha256'] == mains['mains_manifest_sha256'] and
                sha(protected_source(GENERATOR_CONTROL / 'mains/completed.json')) == mains['mains_completed_sha256'],
                'The immutable first generator phase changed.')
    else:
        require(args.mains_indexed_sha256 is None and completed.get('index_receipt_sha256') is None,
                'The mains phase cannot consume an earlier indexing result.')
    _, old_media = support.media_snapshot(op, owner, index['manifestSha256'])
    return manifest, completed, mains, {'new': completed['media_snapshot'], 'old': old_media}


def library_body(op):
    result = copy.deepcopy(op.library_body('Movies'))
    result['Name'], result['Paths'] = LIBRARY_NAME, [str(MEDIA / 'Movies')]
    result['LibraryOptions']['PathInfos'] = [{'Path': str(MEDIA / 'Movies')}]
    require(result['RefreshLibrary'] is False and result['CollectionType'] == 'movies', 'The accepted create template changed.')
    return result


def verify_library(op, row):
    require(row.get('Name') == LIBRARY_NAME and row.get('CollectionType') == 'movies' and row.get('Locations') == [str(MEDIA / 'Movies')],
            'The new library name, type or path changed.')
    converted = copy.deepcopy(row)
    converted['Name'], converted['Locations'] = 'M3e Reference Movies', [str(op.MEDIA / 'Movies')]
    options = converted.get('LibraryOptions', {})
    require(isinstance(options, dict) and isinstance(options.get('PathInfos'), list) and len(options['PathInfos']) == 1 and
            options['PathInfos'][0].get('Path') == str(MEDIA / 'Movies') and not options['PathInfos'][0].get('NetworkPath'),
            'The new library path options changed.')
    options['PathInfos'][0]['Path'] = str(op.MEDIA / 'Movies')
    op.verify_library(converted, 'Movies')
    return safe_id(row.get('ItemId'))


class Recorder:
    def __init__(self, job, actor):
        self.job, self.actor = job, actor
        self.op, self.support = job.op, job.support
        self.user = job.browser['accounts'][actor]
        self.admin = actor == 'admin'
        self.private = job.phase_root / 'private' / actor
        self.private.mkdir(mode=0o700)
        self.device = job.control['devices'][actor]
        self.token, self.proven, self.revoked = None, False, False
        self.authentication = {'loginStatus': None, 'ownedIdentityProven': False, 'logoutStatus': None, 'exactTokenStatus': None}
        self.secrets = {entry['password'] for entry in job.browser['accounts'].values()}
        self.sequence, self.received, self.finishing = 0, 0, False
        self.limit, self.main_limit = (220, 198) if self.admin else (48, 44)
        self.deadline = time.monotonic() + (420 if self.admin else 150)
        self.allowed, self.mutations, self.cases = {'/emby/System/Info/Public'}, set(), {}
        if self.admin:
            self.allowed.update(('/emby/Library/VirtualFolders/Query', '/emby/Users', '/emby/System/Configuration'))

    def save(self, name, value):
        self.job.save(self.private / name, value)

    def route(self, path, query=None):
        require(path.startswith('/emby/') and '?' not in path and '#' not in path, 'A fixed read path is malformed.')
        route = path + ('?' + urlencode(query) if query else '')
        self.allowed.add(route)
        return route

    def request(self, label, method, route, body=None, mutation=None):
        self.job.check()
        require(time.monotonic() < self.deadline and self.sequence < (self.limit if self.finishing else self.main_limit),
                'The actor request or time budget is exhausted.')
        require(re.fullmatch(r'[a-z0-9-]{1,80}', label), 'A request label is invalid.')
        login = mutation == 'login'
        if method == 'GET':
            require(route in self.allowed and body is None and mutation is None, 'An unapproved read was requested.')
        else:
            require(method == 'POST' and mutation not in self.mutations, 'A mutation cannot be repeated.')
            if login:
                require(not self.finishing and self.token is None and route == '/emby/Users/AuthenticateByName' and
                        body == {'Username': self.user['username'], 'Pw': self.user['password']}, 'The fresh login scope changed.')
            elif mutation == 'logout':
                require(route == '/emby/Sessions/Logout' and body is None and self.proven, 'Only a proven owned token may log out.')
            else:
                require(self.admin and self.proven and not self.finishing, 'Only the proven administrator may mutate this fixture.')
                if mutation == 'create':
                    require(self.job.args.phase == 'mains' and self.job.baseline is not None and self.job.library_id is None and
                            route == '/emby/Library/VirtualFolders' and same(body, library_body(self.op)), 'The sole new-library create changed.')
                else:
                    require(mutation == 'refresh' and self.job.library_id is not None and
                            self.job.library_id not in self.job.old_libraries and route == self.job.refresh_route() and body == {},
                            'Only the acknowledged new library may be refreshed once.')
                self.job.check_media()
        require(login or route == '/emby/System/Info/Public' or self.proven, 'The request has no proven recorder identity.')
        revocation = self.proven and (mutation == 'logout' or
                     method == 'GET' and route == '/emby/System/Info' and self.finishing and 'logout' in self.mutations)
        response_limit = REVOCATION_BODY if revocation else MAX_BODY
        self.sequence += 1
        if mutation is not None:
            self.mutations.add(mutation)
        self.save(f'{self.sequence:04d}-{label}-intent.json', {'phase': self.job.args.phase, 'actor': self.actor,
            'method': method, 'route': route, 'body': None if login else body, 'mutation': mutation, 'deviceId': self.device})
        headers = {'Accept': 'application/json', 'Accept-Encoding': 'identity', 'Authorization':
                   f'Emby Client="Goby Special Features Recorder", Device="Linux Recorder", DeviceId="{self.device}", Version="1.0"'}
        if self.proven and not login:
            headers['X-Emby-Token'] = self.token
        payload = None if body is None else encoded(body)
        if payload is not None:
            require(len(payload) <= 32768, 'The request body exceeds its bound.')
            headers['Content-Type'] = 'application/json'
        status, value, raw, mime, complete, failure = None, None, b'', None, False, None
        connection = http.client.HTTPConnection('127.0.0.1', 18097, timeout=8)
        signal.setitimer(signal.ITIMER_REAL, min(12, max(0.01, self.deadline - time.monotonic())))
        try:
            connection.request(method, route, payload, headers)
            response = connection.getresponse()
            status, mime = response.status, response.getheader('Content-Type')
            require(not 300 <= status < 400, 'Redirects are not allowed.')
            length = response.getheader('Content-Length')
            require(length is None or length.isdigit() and int(length) <= response_limit, 'The response length is invalid.')
            raw = response.read(response_limit + 1)
            require(len(raw) <= response_limit and (length is None or int(length) == len(raw)), 'The response is incomplete or oversized.')
            self.received += len(raw)
            self.job.received += len(raw)
            if revocation:
                self.job.revocation_bytes += len(raw)
                require(self.job.revocation_bytes <= REVOCATION_TOTAL, 'The dedicated token-revocation byte budget is exhausted.')
            else:
                require(self.job.received <= (MAX_TOTAL if self.finishing else 80 << 20), 'The phase response budget is exhausted.')
            value = self.support.decode(raw) if raw and mime and mime.lower().split(';', 1)[0] == 'application/json' else raw.decode('utf-8') if raw else None
            complete = True
            if login and isinstance(value, dict) and isinstance(value.get('AccessToken'), str) and 0 < len(value['AccessToken']) <= 8192:
                self.token = value['AccessToken']
                self.secrets.add(self.token)
                user, session = value.get('User', {}), value.get('SessionInfo', {})
                self.proven = (status == 200 and value.get('ServerId') == self.job.owner['serverId'] and
                    user.get('Id') == self.user['userId'] and user.get('Name') == self.user['username'] and
                    user.get('Policy', {}).get('IsAdministrator') is self.admin and
                    session.get('DeviceId') == self.device and session.get('UserId') == self.user['userId'])
                # Evidence failure still rejects business work; an already
                # proven token retains its eligibility for bounded cleanup.
                self.save('recorder-login.json', value)
        except Exception as error:
            failure = type(error).__name__
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            connection.close()
            record = {'request': {'method': method, 'path': route, 'actor': self.actor, 'authenticated': not login and self.proven},
                      'response': {'status': status, 'contentType': mime, 'bytes': len(raw), 'sha256': sha(raw),
                                   'complete': complete, 'body': value, 'failureType': failure}}
            self.save(f'{self.sequence:04d}-{label}-response.json', record)
        require(failure is None and complete, 'The request outcome is uncertain; no mutation may be retried.')
        self.job.check()
        if login:
            self.authentication.update({'loginStatus': status, 'ownedIdentityProven': self.proven})
            require(self.proven, 'The fresh server/user/role/device identity was not proven; retain its private acknowledgement.')
        return status, value, record

    def get(self, label, route):
        status, body, _ = self.request(label, 'GET', route)
        require(status == 200, 'A required identity or preservation read failed.')
        return body

    def login(self):
        self.request('recorder-login', 'POST', '/emby/Users/AuthenticateByName',
                     {'Username': self.user['username'], 'Pw': self.user['password']}, 'login')

    def logout(self):
        self.finishing, self.deadline = True, time.monotonic() + 45
        if not self.proven:
            require(self.token is None, 'An unproven token remains quarantined for independent review.')
            return
        status, body, _ = self.request('recorder-logout', 'POST', '/emby/Sessions/Logout', mutation='logout')
        self.authentication['logoutStatus'] = status
        require(status == 204 and body is None, 'The exact recorder logout was not acknowledged.')
        status, _, _ = self.request('recorder-token-invalid', 'GET', self.route('/emby/System/Info'))
        self.authentication['exactTokenStatus'] = status
        require(status == 401, 'The exact logged-out token remains accepted.')
        self.revoked = True

    def sanitize(self, value):
        if isinstance(value, dict):
            return {key: '[redacted]' if key.lower() in {'accesstoken', 'token', 'api_key', 'x-emby-token', 'password', 'pw', 'authorization', 'cookie', 'set-cookie'}
                    else self.sanitize(item) for key, item in value.items()}
        if isinstance(value, list):
            return [self.sanitize(item) for item in value]
        if isinstance(value, str):
            for secret in sorted(self.secrets, key=len, reverse=True):
                value = value.replace(secret, '[redacted]')
            return re.sub(r'(?:https?|wss?)://[^\s"\'<>]+', '[redacted URL]', value, flags=re.I)
        return value


class Job:
    def __init__(self, args, inputs, media_inputs, control):
        self.args = args
        self.support, self.op, self.owner, self.browser, self.index, self.old_libraries = inputs
        self.manifest, self.completed, self.mains, self.media = media_inputs
        self.control, self.phase_root = control, CONTROL / args.phase
        self.library_id = self.mains['reference']['library_id'] if self.mains else None
        self.baseline, self.after, self.old_ids, self.old_facts = None, None, [], {}
        self.main_items, self.members, self.records = {}, {}, {}
        self.received, self.revocation_bytes, self.preserved, self.media_preserved = 0, 0, False, False
        self.actors = []

    def save(self, path, value):
        require(path.is_relative_to(self.phase_root) or path == INDEXED and self.args.phase == 'mains',
                'Evidence escaped this new phase.')
        self.op.save(path, value)

    def check(self):
        require(sha(protected_source(Path(__file__).absolute())) == self.args.script_sha256 and
                sha(protected_source(self.args.support_path)) == self.args.support_sha256 and
                sha(protected_source(self.support.OPERATOR)) == self.args.operator_sha256 and
                sha(protected_source(self.op.OWNER)) == self.args.owner_sha256 and
                sha(protected_source(AUXILIARY_INDEX)) == AUXILIARY_INDEX_SHA,
                'A pinned dependency or retained report changed.')
        require(same(self.support.decode(protected_source(self.phase_root / 'OWNER.json')), self.control), 'The phase owner changed.')
        require(sha(protected_source(CONTROL / 'OWNER.json')) == self.control['commonOwnerSha256'] and
                directory_identity(self.support, self.op, CONTROL, 0o700) == self.control['controlIdentity'] and
                directory_identity(self.support, self.op, self.phase_root, 0o700) == self.control['phaseIdentity'],
                'A control directory or its immutable owner changed.')
        require(self.op.digest(self.op.REPORT) == self.owner['reportSha256'] and
                self.op.digest(self.op.BROWSER) == self.owner['browserSha256'], 'Old reference evidence or credentials changed.')
        self.op.same_service(self.owner)
        require(os.readlink('/proc/self/ns/net') == self.owner['serviceIdentity']['networkNamespace'], 'HTTP left the reference namespace.')

    def check_media(self):
        _, _, _, observed = new_media(self.args, self.support, self.op, self.owner, self.index)
        require(same(observed, self.media), 'Original, auxiliary or new media changed during this phase.')

    def libraries(self, actor, label, pending=False):
        body = actor.get(label, '/emby/Library/VirtualFolders/Query')
        require(isinstance(body, dict) and isinstance(body.get('Items'), list) and len(body['Items']) <= 7 and
                body.get('TotalRecordCount', len(body['Items'])) == len(body['Items']), 'The library inventory is incomplete.')
        result = {}
        for row in body['Items']:
            key = safe_id(row.get('ItemId'))
            require(key not in result, 'A library identity is duplicated.')
            result[key] = row
        require(self.old_libraries <= set(result), 'An existing library disappeared.')
        if self.baseline is not None:
            require(all(same(row, result[key]) for key, row in self.baseline['libraries'].items()), 'An existing library changed.')
        new = {key: row for key, row in result.items() if key not in self.old_libraries}
        if self.library_id is not None:
            require(set(new) == {self.library_id} and verify_library(self.op, new[self.library_id]) == self.library_id,
                    'The acknowledged new library changed.')
        elif pending:
            require(len(new) == 1, 'The new create has no unique library acknowledgement.')
            verify_library(self.op, next(iter(new.values())))
        else:
            require(not new and not any(row.get('Name') == LIBRARY_NAME or str(MEDIA / 'Movies') in row.get('Locations', [])
                                       for row in result.values()), 'A new library cannot be adopted.')
        return result

    def item_rows(self, actor, label, user_id, ids=None, parent=None):
        query = {'Recursive': 'true', 'IncludeItemTypes': TYPES, 'Fields': FIELDS,
                 'Limit': '256', 'EnableTotalRecordCount': 'true', 'EnableUserData': 'true'}
        if ids is not None:
            query['Ids'] = ','.join(ids)
        if parent is not None:
            query['ParentId'] = parent
            # New extras may have an observed type absent from the old catalog.
            del query['IncludeItemTypes']
        route = actor.route('/emby/Users/' + safe_id(user_id) + '/Items', query)
        result = self.support.rows(actor.get(label, route), 256)
        for row in result.values():
            if parent is None:
                require(row.get('Type') in TYPES.split(',') and
                        (row['Type'] == 'Folder' or isinstance(row.get('UserData'), dict)),
                        'A catalog item lacks its expected type or UserData witness.')
            path = row.get('Path')
            require(path is None or path == '' or isinstance(path, str) and '..' not in PurePosixPath(path).parts and
                    any(path == str(root) or path.startswith(str(root) + '/') for root in (*OLD_ROOTS, MEDIA)),
                    'A catalog row escaped the three owned media roots.')
        return result

    def snapshot(self, actor, label, initial=False):
        libraries = self.libraries(actor, label + '-libraries')
        users_raw = actor.get(label + '-users', '/emby/Users')
        require(isinstance(users_raw, list) and len(users_raw) == 5, 'The reference user population changed.')
        users = {}
        for row in users_raw:
            key = safe_id(row.get('Id'))
            require(key not in users and isinstance(row.get('Configuration'), dict) and isinstance(row.get('Policy'), dict),
                    'A user lacks its configuration/policy witness.')
            users[key] = {field: row[field] for field in ('Id', 'Name', 'Configuration', 'Policy')}
        require(set(users) == set(self.index['existingUserIds']), 'The five users differ from retained ownership evidence.')
        for role, account in self.browser['accounts'].items():
            require(users[account['userId']]['Name'] == account['username'] and
                    users[account['userId']]['Policy'].get('IsAdministrator') is (role == 'admin'), 'An original account changed authority.')
        config = actor.get(label + '-configuration', '/emby/System/Configuration')
        projections = {}
        if initial:
            catalog = self.item_rows(actor, label + '-catalog', actor.user['userId'])
            old = {key: row for key, row in catalog.items() if not isinstance(row.get('Path'), str) or
                   not (row['Path'] == str(MEDIA) or row['Path'].startswith(str(MEDIA) + '/'))}
            expected = {row['Id']: (row.get('Type'), row.get('Path')) for row in self.op.read_private(self.op.REPORT)['items']
                        if row.get('Type') in ('Movie', 'Episode', 'Audio')}
            for entry in self.index['catalog'].values():
                retained = {row['Id']: row for row in entry['items']}
                for relative, item_id in entry['primaryIdsByManifestPath'].items():
                    row = retained[item_id]
                    require(row['Path'] == str(OLD_ROOTS[1] / relative), 'A retained auxiliary primary path is inconsistent.')
                    expected[item_id] = (row['Type'], row['Path'])
            require(set(expected) <= set(old) and all((old[key].get('Type'), old[key].get('Path')) == fact
                    for key, fact in expected.items()), 'An acknowledged old playable identity, type or path changed.')
            self.old_ids = sorted(old)
            self.old_facts = {key: (row.get('Type'), row.get('Path')) for key, row in old.items()}
        for number, user_id in enumerate(sorted(users)):
            rows = self.item_rows(actor, f'{label}-user-{number:02d}', user_id, self.old_ids)
            require(set(rows) <= set(self.old_ids) and all((row.get('Type'), row.get('Path')) == self.old_facts[key]
                    for key, row in rows.items()), 'An old user projection changed its item identities.')
            if user_id == actor.user['userId']:
                require(set(rows) == set(self.old_ids), 'The administrator lost an old catalog witness.')
            projections[user_id] = rows
        result = {'libraries': {key: libraries[key] for key in sorted(self.old_libraries)}, 'users': users,
                  'configuration': config, 'itemsByUser': projections}
        self.save(self.phase_root / 'private' / (label + '-snapshot.json'), result)
        return result

    def refresh_route(self):
        return '/emby/Items/' + safe_id(self.library_id) + '/Refresh?' + urlencode(REFRESH)

    def read_members(self, actor, number):
        members = self.item_rows(actor, f'new-members-{number:02d}', actor.user['userId'], parent=self.library_id)
        require(not set(members).intersection(self.old_ids), 'The new library reused an old catalog identity.')
        for row in members.values():
            path = row.get('Path')
            require(path is None or path == '' or path == str(MEDIA / 'Movies') or path.startswith(str(MEDIA / 'Movies') + '/'),
                    'The new-library query returned an old or foreign media path.')
        selected = {}
        for key, relative in MAIN_PATHS.items():
            matches = [row for row in members.values() if row.get('Path') == str(MEDIA / relative)]
            if len(matches) != 1:
                return members, None
            row = matches[0]
            require(row.get('Type') == 'Movie', 'A main movie has another actual type.')
            sources = row.get('MediaSources', [])
            if not isinstance(sources, list) or not sources:
                return members, None
            streams = [stream for source in sources if isinstance(source, dict) and isinstance(source.get('MediaStreams'), list)
                       for stream in source['MediaStreams'] if isinstance(stream, dict)]
            if not {'Video', 'Audio'} <= {stream.get('Type') for stream in streams}:
                return members, None
            declaration = next(item for item in self.manifest['items'] if item['fixture_id'] == key)
            if row.get('Name') != declaration['title']:
                return members, None
            if self.mains:
                require(row['Id'] == self.mains['items'][key]['Id'], 'An indexed main movie changed its ID.')
            selected[key] = {field: row[field] for field in ('Id', 'Type', 'Path')}
        return members, selected

    def index_library(self, actor):
        if self.args.phase == 'mains':
            self.libraries(actor, 'before-create')
            status, body, _ = actor.request('new-library-create', 'POST', '/emby/Library/VirtualFolders', library_body(self.op), 'create')
            require(status in (200, 204) and body in (None, {}), 'The create outcome is unknown; no retry is permitted.')
            libraries = self.libraries(actor, 'after-create', pending=True)
            self.library_id = next(key for key in libraries if key not in self.old_libraries)
            self.save(self.phase_root / 'private/library-acknowledged.json', libraries[self.library_id])
        else:
            _, selected = self.read_members(actor, 99)
            require(selected == self.mains['items'], 'The acknowledged main movies are not intact before the extras refresh.')
        status, body, _ = actor.request('new-library-refresh', 'POST', self.refresh_route(), {}, 'refresh')
        require(status in (200, 204) and body in (None, {}), 'The refresh outcome is unknown; no retry is permitted.')
        previous, bound = None, time.monotonic() + 100
        for number in range(32):
            require(time.monotonic() < bound, 'The new library did not stabilize within its bounded scan observation.')
            libraries = self.libraries(actor, f'scan-libraries-{number:02d}')
            require(all(same(row, libraries[key]) for key, row in self.baseline['libraries'].items()), 'An old library changed during refresh.')
            members, selected = self.read_members(actor, number)
            observed = {'library': libraries[self.library_id], 'members': members, 'items': selected}
            ready = selected is not None and self.support.refresh_shape(libraries[self.library_id]) == {}
            if ready and previous is not None and same(previous, observed):
                self.members, self.main_items = members, selected
                self.save(self.phase_root / 'private/index-observed.json', observed)
                return
            previous = observed if ready else None
            time.sleep(2)
        raise RuntimeError('The owned scan did not yield two stable main-movie observations.')

    def capture(self, actor):
        user_path = '/emby/Users/' + safe_id(actor.user['userId'])
        before = actor.get('viewer-before', actor.route(user_path))
        require(isinstance(before, dict) and before.get('Id') == actor.user['userId'] and
                isinstance(before.get('Configuration'), dict) and isinstance(before.get('Policy'), dict),
                'The ordinary viewer lacks its own configuration and policy witness.')
        projections = self.item_rows(actor, 'mains-before', actor.user['userId'], sorted(item['Id'] for item in self.main_items.values()))
        expected = {item['Id']: item for item in self.main_items.values()}
        require(set(projections) == set(expected) and all(
            all(row.get(field) == expected[key][field] for field in ('Id', 'Type', 'Path')) for key, row in projections.items()),
            'The ordinary viewer cannot see both exact indexed main movies.')
        targets = dict(self.main_items)
        if self.args.phase == 'extras':
            admin_rows = self.baseline['itemsByUser'][self.browser['accounts']['admin']['userId']]
            for key, kind, relative in (
                ('series', 'Series', 'TV/M3e Client Series'),
                ('episode', 'Episode', 'TV/M3e Client Series/Season 01/M3e Client Series S01E01.mp4')):
                matches = [row for row in admin_rows.values() if row.get('Type') == kind and row.get('Path') == str(OLD_ROOTS[0] / relative)]
                require(len(matches) == 1, 'An existing Series/Episode control is not uniquely bound.')
                targets[key] = matches[0]
        for key, item in targets.items():
            families = ('SpecialFeatures', 'LocalTrailers') if self.args.phase == 'mains' else ('SpecialFeatures', 'LocalTrailers', 'Intros')
            for family in families:
                for fields in (False, True) if self.args.phase == 'extras' else (False,):
                    label = key + '-' + family.lower() + ('-fields' if fields else '-default')
                    route = actor.route(user_path + '/Items/' + safe_id(item['Id']) + '/' + family,
                                        {'Fields': FIELDS} if fields else None)
                    status, body, record = actor.request(label, 'GET', route)
                    if self.args.phase == 'mains':
                        require(status == 200 and body == [], 'The two indexed mains do not prove empty local extras.')
                    self.records[label] = record
        after = actor.get('viewer-after', actor.route(user_path))
        after_projections = self.item_rows(actor, 'mains-after', actor.user['userId'], sorted(item['Id'] for item in self.main_items.values()))
        require(all(before.get(key) == after.get(key) for key in ('Id', 'Name', 'Configuration', 'Policy')) and
                same(projections, after_projections), 'The ordinary capture changed configuration, policy or main item state.')

    def run(self):
        failure = None
        admin = Recorder(self, 'admin')
        self.actors.append(admin)
        try:
            public = admin.get('public-identity', '/emby/System/Info/Public')
            require(isinstance(public, dict) and public.get('Id') == self.owner['serverId'] and public.get('Version') == '4.9.5.0',
                    'The public reference identity differs from the frozen official package.')
            admin.login()
            self.baseline = self.snapshot(admin, 'before', initial=True)
            require(all(self.support.refresh_shape(row) == {} for row in self.baseline['libraries'].values()), 'An old library has an unreviewed active refresh state.')
            self.index_library(admin)
            viewer = Recorder(self, 'viewer')
            self.actors.append(viewer)
            try:
                viewer.login()
                self.capture(viewer)
            finally:
                viewer.logout()
        except Exception as error:
            failure = type(error).__name__
        finally:
            admin.finishing, admin.deadline = True, time.monotonic() + 100
            try:
                if admin.proven and self.baseline is not None:
                    self.after = self.snapshot(admin, 'after')
                    require(same(self.baseline, self.after), 'Old six-library metadata, five-user state or server configuration changed.')
                    self.preserved = True
            except Exception as error:
                failure = failure or type(error).__name__
            try:
                admin.logout()
            except Exception as error:
                failure = failure or type(error).__name__
            try:
                self.check_media()
                self.check()
                self.media_preserved = True
            except Exception as error:
                failure = failure or type(error).__name__
        complete = failure is None and self.preserved and self.media_preserved and len(self.actors) == 2 and all(actor.revoked for actor in self.actors)
        report = {'marker': MARKER, 'phase': self.args.phase, 'result': 'complete' if complete else 'retained_for_review',
                  'referenceIdentity': self.owner['serviceIdentity'], 'serverId': self.owner['serverId'],
                  'libraryId': self.library_id, 'mainItems': self.main_items, 'scriptSha256': self.args.script_sha256,
                  'supportSha256': self.args.support_sha256, 'auxiliaryIndexSha256': AUXILIARY_INDEX_SHA,
                  'commonOwnerSha256': self.control['commonOwnerSha256'], 'controlIdentity': self.control['controlIdentity'],
                  'mediaManifestSha256': self.args.media_manifest_sha256, 'mediaCompletedSha256': self.args.media_completed_sha256,
                  'oldLibrariesAndFiveUsersPreserved': self.preserved, 'allMediaPreserved': self.media_preserved,
                  'preservationScope': {'libraries': sorted(self.old_libraries), 'userCount': 5, 'catalogTypes': TYPES.split(','),
                                        'oldVisibleItemIdsByUser': {key: sorted(rows) for key, rows in self.baseline['itemsByUser'].items()}
                                        if self.baseline is not None else {},
                                        'note': 'Exact old API projections and Configuration/Policy; not a raw database or session-history equality claim.'},
                  'newTokensRevoked': all(actor.revoked for actor in self.actors), 'noExtras': complete and self.args.phase == 'mains',
                  'requests': {actor.actor: actor.sequence for actor in self.actors}, 'responseBytes': self.received,
                  'authentication': {actor.actor: actor.authentication for actor in self.actors}, 'revocationResponseBytes': self.revocation_bytes,
                  'caseResults': self.records, 'scannedCatalog': self.members, 'failureType': failure,
                  'pending': ['positive membership, names and ExtraType mapping require review of actual cases',
                              'sorting, image switches, restricted/foreign-user errors and media playback are not verified',
                              'Series/Episode captures are existing controls, not positive Series support'],
                  'boundary': 'Black-box indexing and protocol capture only; no complete feature or original-client acceptance claim.'}
        for actor in self.actors:
            report = actor.sanitize(report)
        require(not any(secret in encoded(report).decode() for actor in self.actors for secret in actor.secrets),
                'A recorder credential survived the safe export.')
        report_path = self.phase_root / 'export/report.json'
        self.save(report_path, report)
        if complete and self.args.phase == 'mains':
            receipt = {'schema': 'goby-client-special-features-mains-indexed', 'version': 1, 'status': 'indexed',
                       'media_root': str(MEDIA), 'media_root_identity': self.manifest['root_identity'],
                       'generator_sha256': self.manifest['generator_sha256'], 'mains_manifest_sha256': self.args.media_manifest_sha256,
                       'mains_completed_sha256': self.args.media_completed_sha256,
                       'reference': {'service': 'goby-emby-client-m3e.service', 'binary_sha256': REFERENCE_SHA,
                                     'server_id': self.owner['serverId'], 'library_id': self.library_id, 'library_path': str(MEDIA / 'Movies')},
                       'items': self.main_items, 'no_extras': True,
                       'evidence': {'path': str(report_path), 'sha256': sha(protected_source(report_path))}}
            self.save(INDEXED, receipt)
        print(json.dumps({'result': report['result'], 'phase': self.args.phase, 'report': str(report_path),
                          'newTokensRevoked': report['newTokensRevoked']}), flush=True)
        return 0 if complete else 1


def input_bindings(args):
    result = {key: value for key, value in vars(args).items() if key.endswith('_sha256')}
    result.update({'phase': args.phase, 'support_path': str(args.support_path)})
    return result


def existing_control(args, inputs, media_inputs):
    support, op, owner, _, _, old_libraries = inputs
    manifest, _, mains, _ = media_inputs
    require(op.present(CONTROL) and args.phase == 'extras' and mains is not None,
            'Only an acknowledged second phase may use this control root.')
    identity = directory_identity(support, op, CONTROL, 0o700)
    raw = protected_source(CONTROL / 'OWNER.json')
    common = support.decode(raw)
    report = support.decode(protected_source(CONTROL / 'mains/export/report.json'))
    require(common == {'marker': MARKER, 'version': 1, 'path': str(CONTROL), 'controlIdentity': identity,
                       'scriptSha256': args.script_sha256, 'supportSha256': args.support_sha256,
                       'operatorSha256': args.operator_sha256, 'ownerSha256': args.owner_sha256,
                       'auxiliaryIndexSha256': AUXILIARY_INDEX_SHA, 'referenceIdentity': owner['serviceIdentity'],
                       'serverId': owner['serverId'], 'oldLibraryIds': sorted(old_libraries),
                       'mediaRoot': str(MEDIA), 'mediaRootIdentity': manifest['root_identity']},
            'The common owner does not belong to this exact reference, code and media root.')
    require(report.get('commonOwnerSha256') == sha(raw) and report.get('controlIdentity') == identity and
            report.get('scriptSha256') == args.script_sha256,
            'The completed mains evidence does not bind this original control directory.')
    return common


def check_phase_absence(args, inputs, media_inputs):
    _, op, _, _, _, _ = inputs
    if args.phase == 'mains':
        require(not op.present(CONTROL), 'An existing mains control cannot be adopted, overwritten or retried.')
    else:
        existing_control(args, inputs, media_inputs)
        require(not op.present(CONTROL / 'extras'), 'An existing extras phase cannot be overwritten or retried.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--phase', choices=('mains', 'extras'), required=True)
    parser.add_argument('--check-only', action='store_true')
    parser.add_argument('--support-path', type=Path, required=True)
    for name in ('script', 'support', 'operator', 'owner', 'auxiliary-index', 'media-manifest', 'media-completed'):
        parser.add_argument('--' + name + '-sha256', required=True)
    parser.add_argument('--mains-indexed-sha256')
    parser.add_argument('--worker', action='store_true', help=argparse.SUPPRESS)
    parser.add_argument('--lock-fd', type=int, help=argparse.SUPPRESS)
    args = parser.parse_args()
    os.umask(0o077)
    signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(TimeoutError('A bounded request expired.')))
    require(not (args.worker and args.check_only) and (args.worker or args.lock_fd is None), 'The execution mode is inconsistent.')
    inputs = load_inputs(args)
    support, op, owner, _, _, old_libraries = inputs
    media_inputs = new_media(args, support, op, owner, inputs[4])
    manifest, _, _, media = media_inputs
    bindings = input_bindings(args)
    phase_root = CONTROL / args.phase
    if args.worker:
        require(type(args.lock_fd) is int and args.lock_fd > 2, 'The worker lacks its inherited supervisor lock.')
        control = support.decode(protected_source(phase_root / 'OWNER.json'))
        require(control.get('marker') == MARKER and control.get('path') == str(phase_root) and control.get('phase') == args.phase and
                same(control.get('inputs'), bindings) and same(control.get('referenceIdentity'), owner['serviceIdentity']) and
                same(op.process_identity(os.getppid()), control.get('supervisor')) and
                same(support.identity(os.fstat(args.lock_fd)), support.identity(op.canonical(op.LOCK, mode=0o600))) and
                os.readlink('/proc/self/ns/net') == owner['serviceIdentity']['networkNamespace'] and same(control.get('media'), media),
                'The worker is not bound to this live supervisor, lock, inputs, media and namespace.')
        require(isinstance(control.get('devices'), dict) and set(control['devices']) == {'admin', 'viewer'} and all(
            re.fullmatch('goby-m3e-special-features-' + args.phase + '-' + role + '-[0-9a-f]{32}', value)
            for role, value in control['devices'].items()), 'The two recorder device identities are malformed.')
        job = Job(args, inputs, media_inputs, control)
        job.check()
        job.save(phase_root / 'private/worker.json', op.process_identity(os.getpid()))
        return job.run()
    op.canonical(op.LOCK, mode=0o600)
    lock = os.open(op.LOCK, os.O_RDONLY | os.O_NOFOLLOW)
    namespace = None
    try:
        require(same(support.identity(os.fstat(lock)), support.identity(op.canonical(op.LOCK, mode=0o600))),
                'The reference lock changed while opening.')
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        check_phase_absence(args, inputs, media_inputs)
        op.same_service(owner)
        if args.check_only:
            print(json.dumps({'result': 'preflight_passed', 'phase': args.phase, 'httpRequests': 0, 'evidenceWrites': 0,
                              'scriptSha256': args.script_sha256, 'mediaManifestSha256': args.media_manifest_sha256,
                              'mediaCompletedSha256': args.media_completed_sha256, 'oldLibraryCount': len(old_libraries)}), flush=True)
            return 0
        if args.phase == 'mains':
            CONTROL.mkdir(mode=0o700)
            common = {'marker': MARKER, 'version': 1, 'path': str(CONTROL),
                      'controlIdentity': directory_identity(support, op, CONTROL, 0o700),
                      'scriptSha256': args.script_sha256, 'supportSha256': args.support_sha256,
                      'operatorSha256': args.operator_sha256, 'ownerSha256': args.owner_sha256,
                      'auxiliaryIndexSha256': AUXILIARY_INDEX_SHA, 'referenceIdentity': owner['serviceIdentity'],
                      'serverId': owner['serverId'], 'oldLibraryIds': sorted(old_libraries),
                      'mediaRoot': str(MEDIA), 'mediaRootIdentity': manifest['root_identity']}
            op.save(CONTROL / 'OWNER.json', common)
            op.sync_directory(WORK)
        else:
            common = existing_control(args, inputs, media_inputs)
        phase_root.mkdir(mode=0o700)
        (phase_root / 'private').mkdir(mode=0o700)
        (phase_root / 'export').mkdir(mode=0o700)
        control = {'marker': MARKER, 'path': str(phase_root), 'phase': args.phase, 'inputs': bindings,
                   'referenceIdentity': owner['serviceIdentity'], 'supervisor': op.process_identity(os.getpid()),
                   'devices': {role: 'goby-m3e-special-features-' + args.phase + '-' + role + '-' + secrets.token_hex(16)
                               for role in ('admin', 'viewer')}, 'media': media,
                   'commonOwnerSha256': sha(protected_source(CONTROL / 'OWNER.json')),
                   'controlIdentity': common['controlIdentity'], 'phaseIdentity': directory_identity(support, op, phase_root, 0o700)}
        op.save(phase_root / 'OWNER.json', control)
        op.sync_directory(CONTROL)
        namespace = os.open(f'/proc/{PID}/ns/net', os.O_RDONLY)
        require(os.fstat(namespace).st_ino == int(owner['serviceIdentity']['networkNamespace'][5:-1]), 'The reference namespace changed.')
        op.same_service(owner)
        require(sha(protected_source(Path(__file__).absolute())) == args.script_sha256, 'The worker source changed before dispatch.')
        arguments = ['/usr/bin/nsenter', '--net=/proc/self/fd/' + str(namespace), '/usr/bin/python3', '-I', '-B',
                     str(Path(__file__).absolute()), '--worker', '--lock-fd', str(lock), '--phase', args.phase,
                     '--support-path', str(args.support_path)]
        for key, value in bindings.items():
            if key.endswith('_sha256') and value is not None:
                arguments.extend(['--' + key.replace('_', '-'), value])
        op.save(phase_root / 'private/worker-dispatch-intent.json', {'marker': MARKER, 'inputs': bindings, 'supervisor': control['supervisor']})
        result = subprocess.run(arguments, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=900, check=False, pass_fds=(lock, namespace),
                                env=dict(op.BASE_ENV, SSH_CONNECTION=os.environ['SSH_CONNECTION']))
        require(len(result.stdout) <= 8192 and len(result.stderr) <= 8192, 'The worker exceeded its terminal output bound.')
        op.save(phase_root / 'private/worker-exit.json', {'returnCode': result.returncode,
                'stdoutSha256': sha(result.stdout), 'stderrSha256': sha(result.stderr)})
        print(json.dumps({'result': 'complete' if result.returncode == 0 else 'retained_for_review',
                          'phase': args.phase, 'report': str(phase_root / 'export/report.json'), 'evidence': str(phase_root)}), flush=True)
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
