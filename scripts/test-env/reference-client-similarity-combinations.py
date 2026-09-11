#!/usr/bin/env python3
"""Observe two reversible metadata combinations in the receipted auxiliary fixture.

This is protocol research, not client acceptance. Run only through authorized
root SSH after independent review and remote guards. Supply this source hash
and the path to the exact accepted auxiliary-library operator. Only its pure
media snapshot and library-validation helpers are used; no create, refresh,
service-control, account-update, media-write, or old-reference entry is called.

Select exactly one independent eligibility or ranking variant. Normal work has
a 120-second deadline. Recovery/preservation has a separate 45-second deadline
and exact-token cleanup has 20 seconds. Eligibility plans 37 requests; ranking
plans 43, with four consecutive Similar observations per accepted mutation.
At most 45 attempts are permitted, including the reserved two cleanup requests.
Every POST has one durable intent and is sent at most once. Existing output is
never adopted. An uncertain or foreign metadata state remains for review.
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
from urllib.parse import urlencode

sys.dont_write_bytecode = True
WORK = Path('/opt/goby-test/exec-work-m3e')
CONTROL = WORK / 'reference-similarity-combinations-v3-unselected'
PRIVATE, EXPORT = CONTROL / 'private', CONTROL / 'export'
OPERATOR = WORK / 'prepare-client-reference.py'
INDEX = WORK / 'reference-auxiliary-libraries-v1/export/report.json'
MEDIA = Path('/opt/goby-fixtures/client-aux-m3e-v1')
MARKER = 'goby-reference-similarity-combinations-m3e-v3-unselected'
VARIANTS = ('eligibility', 'ranking')
NORMAL_REQUESTS = {'eligibility': 37, 'ranking': 43}
SIMILAR_READS = {'eligibility': 1, 'ranking': 4}
CLIENT = 'Goby Similarity Combination Recorder'
PID, TICKS = 332054, '357218'
BINARY_SHA = 'c109c9817dea25cc516b9969a87aa1ffa48e41adcb5e3dbc87d686c7bcb28ac2'
HELPER_SHA = '9af752745a5e9d9a9047491c03b9f32da2e9a70d01d78d17e1504ad760204d75'
OPERATOR_SHA = '29f159a2580e178dce65b6c23ef14ad9875971240e534546eb5e8a14ce88c160'
OWNER_SHA = '3bdbeee28406f9809624f3619b38915ab4445fa1a7c8aca30cbe445d3d1ddb40'
INDEX_SHA = '457a4d88f86617c448b980f31b7dd5b7d8bbeb31e3cfe807103371859f2d7998'
MANIFEST_SHA = 'dad99c4883fde8bba00c9061179341b1a5869dd20212de55b92e0a53353703a7'
MAX_BODY, MAX_TOTAL, MAX_REQUESTS = 2 << 20, 48 << 20, 45
MAIN_SECONDS, RECOVERY_SECONDS, LOGOUT_SECONDS = 120, 45, 20
EDIT_FIELDS = (
    'Name', 'SortName', 'ForcedSortName', 'OriginalTitle', 'Overview', 'ProductionYear', 'PremiereDate',
    'EndDate', 'CommunityRating', 'CriticRating', 'OfficialRating', 'CustomRating', 'ProviderIds',
    'Genres', 'Tags', 'TagItems', 'Studios', 'People', 'LockedFields', 'LockData', 'Taglines', 'ProductionLocations',
    'PreferredMetadataLanguage', 'PreferredMetadataCountryCode', 'IndexNumber', 'ParentIndexNumber',
    'SortIndexNumber', 'SortParentIndexNumber', 'DisplayOrder', 'Status', 'DateCreated',
)
SIMILAR_FIELDS = ('Path,Genres,People,Studios,Tags,ProductionYear,ParentId,SortName,DateCreated,Overview,'
                  'ProviderIds,PrimaryImageAspectRatio')
WITNESS_FIELDS = ('Path,MediaSources,MediaStreams,Overview,ProviderIds,Genres,Tags,People,Studios,SortName,'
                  'DateCreated,ProductionYear,PremiereDate,OriginalTitle')
TARGETS = {'35': ('Seed', 'Movies/Seed/movie.mp4'), '40': ('GenreOne', 'Movies/GenreOne/movie.mp4'),
           '49': ('Actor', 'Movies/Actor/movie.mp4')}
# These are the only raw-detail exclusions, never exclusions from EDIT_FIELDS
# or UserData. Every observed change is recorded; no raw-equality claim is made.
AUTOMATIC_FIELDS = frozenset(('Etag', 'ETag', 'DateLastSaved', 'DateLastRefreshed'))
SHA = re.compile(r'[0-9a-f]{64}')
ID = re.compile(r'[A-Za-z0-9_-]{1,128}')
URL = re.compile(r'(?:https?|wss?)://[^\s\"\'<>]+', re.IGNORECASE)


class Failure(Exception):
    """A fixed ownership, transport, or preservation boundary failed."""


def require(condition, message):
    if not condition:
        raise Failure(message)


def select_variant(variant):
    global CONTROL, PRIVATE, EXPORT, MARKER
    require(variant in VARIANTS, 'An explicit approved similarity variant is required.')
    CONTROL = WORK / ('reference-similarity-combinations-v3-' + variant)
    PRIVATE, EXPORT = CONTROL / 'private', CONTROL / 'export'
    MARKER = 'goby-reference-similarity-combinations-m3e-v3-' + variant


def encoded(value):
    return (json.dumps(value, sort_keys=True, ensure_ascii=True, separators=(',', ':'), allow_nan=False) + '\n').encode()


def equal(left, right):
    return encoded(left) == encoded(right)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def decode(raw):
    def pairs(entries):
        result = {}
        for key, value in entries:
            require(key not in result, 'A JSON object repeats a member.')
            result[key] = value
        return result

    def invalid(_value):
        raise Failure('A JSON number is not finite.')

    return json.loads(raw, object_pairs_hook=pairs, parse_constant=invalid)


def identity(info):
    return (info.st_dev, info.st_ino, info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode),
            info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def protected_bytes(path, limit=MAX_BODY):
    require(path.is_absolute() and '..' not in path.parts, 'An input path is not canonical.')
    for parent in reversed(path.parents):
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                'An input ancestor is not protected and root-owned.')
    before = path.lstat()
    require(stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and
            stat.S_IMODE(before.st_mode) in (0o600, 0o644) and before.st_nlink == 1 and before.st_size <= limit,
            'An input owner, type, permissions, link count, or size differs.')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as stream:
        require(identity(os.fstat(stream.fileno())) == identity(before), 'An input changed while opening.')
        raw = stream.read(limit + 1)
        require(len(raw) == before.st_size and identity(os.fstat(stream.fileno())) == identity(before),
                'An input changed while reading.')
    require(identity(path.lstat()) == identity(before), 'An input changed after reading.')
    return raw


def load_module(path, digest, name):
    raw = protected_bytes(path)
    require(sha(raw) == digest, 'An explicitly accepted helper source changed.')
    module = types.ModuleType(name)
    module.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), module.__dict__)
    return module


def identifier(value):
    require(isinstance(value, str) and ID.fullmatch(value), 'An identifier cannot enter a fixed route.')
    return value


def edit_body(detail):
    return {'Id': identifier(detail.get('Id')),
            **{key: copy.deepcopy(detail[key]) for key in EDIT_FIELDS if key in detail}}


def business(detail):
    return {key: value for key, value in detail.items() if key not in AUTOMATIC_FIELDS}


def automatic_changes(before, after):
    changes = {}
    for key in sorted(AUTOMATIC_FIELDS):
        old, new = {'present': key in before, 'value': before.get(key)}, {'present': key in after, 'value': after.get(key)}
        if equal(old, new):
            continue
        for item in (old, new):
            value = item['value']
            if item['present']:
                pattern = r'[A-Za-z0-9_-]{1,128}' if key in ('Etag', 'ETag') else r'\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,7})?Z'
                require(isinstance(value, str) and re.fullmatch(pattern, value), 'An automatic field has an unreviewed shape.')
        changes[key] = {'before': old, 'after': new}
    return changes


def verify_target(row, item_id):
    name, path = TARGETS[item_id]
    require(isinstance(row, dict) and row.get('Id') == item_id and row.get('Type') == 'Movie' and
            row.get('Name') == name and row.get('Path') == str(MEDIA / path) and isinstance(row.get('UserData'), dict),
            'A target is not its exact receipted movie, physical path, and user projection.')


def load_inputs(args):
    require(sys.platform == 'linux' and os.geteuid() == 0 and os.environ.get('SSH_CONNECTION'),
            'Run only through authorized root SSH on test-env.')
    require(SHA.fullmatch(args.script_sha256 or ''), 'Supply the reviewed operator SHA-256.')
    source = Path(__file__).absolute()
    require(source.is_relative_to(Path('/opt/goby-test')) and sha(protected_bytes(source)) == args.script_sha256,
            'The reviewed operator bytes or location changed.')
    helper_path = Path(args.snapshot_helper)
    require(helper_path.is_relative_to(Path('/opt/goby-test')), 'The accepted snapshot helper is outside test-env work.')
    helper = load_module(helper_path, HELPER_SHA, 'accepted_auxiliary_snapshot_helpers')
    op = load_module(OPERATOR, OPERATOR_SHA, 'accepted_current_reference_preparer')
    require(helper.WORK == WORK and helper.ROOT == MEDIA and helper.PID == PID and helper.TICKS == TICKS and
            op.WORK == WORK and op.PORT == 18097 and op.BINARY_SHA256 == BINARY_SHA and
            op.MEDIA == Path('/opt/goby-fixtures/client-m3e'), 'A helper addresses an unapproved fixture.')
    op.preconditions()
    owner_raw = protected_bytes(op.OWNER)
    require(sha(owner_raw) == OWNER_SHA, 'The explicitly accepted reference owner changed.')
    owner = decode(owner_raw)
    op.validate_owner(owner)
    require(owner.get('phase') == 'ready' and owner['serviceIdentity']['pid'] == PID and
            owner['serviceIdentity']['startTicks'] == TICKS, 'The expected process lifetime changed.')
    op.same_service(owner)
    require(op.digest(op.REPORT) == owner['reportSha256'] and op.digest(op.BROWSER) == owner['browserSha256'],
            'The retained reference receipt or current browser credentials changed.')
    browser = op.read_private(op.BROWSER)
    require(browser.get('marker') == op.MARKER and browser.get('serverId') == owner['serverId'] and
            set(browser.get('accounts', {})) == set(op.NAMES), 'The current browser input belongs to another fixture.')
    for name, account in browser['accounts'].items():
        require(account.get('username') == op.NAMES[name] and SHA.fullmatch(account.get('password', '')) and
                re.fullmatch(r'[0-9a-f]{32}', account.get('userId', '')), 'An acknowledged fixture credential differs.')
    raw = protected_bytes(INDEX)
    require(sha(raw) == INDEX_SHA, 'The completed auxiliary-library receipt changed.')
    index = decode(raw)
    require(index.get('marker') == helper.MARKER and index.get('result') == 'complete' and
            index.get('scriptSha256') == HELPER_SHA and index.get('operatorSha256') == OPERATOR_SHA and
            index.get('ownerSha256') == OWNER_SHA and index.get('manifestSha256') == MANIFEST_SHA and
            index.get('referencePID') == PID and index.get('referenceStartTicks') == TICKS and
            index.get('serverId') == owner['serverId'] and index.get('newRecorderTokenRevoked') is True and
            index.get('allMediaPreserved') is True and
            index.get('originalLibrariesMetadataUserDataConfigurationPolicyPreserved') is True and
            index.get('failure') is None and len(set(index.get('existingUserIds', []))) == 5,
            'The exact new-library receipt is not a completed preservation proof.')
    for item_id, (name, path) in TARGETS.items():
        require(index['catalog']['Movies']['primaryIdsByManifestPath'].get(path) == item_id,
                'A target does not match the accepted manifest-path identity.')
        matches = [row for row in index['catalog']['Movies']['items'] if row.get('Id') == item_id]
        require(len(matches) == 1 and matches[0].get('Name') == name and matches[0].get('Type') == 'Movie' and
                matches[0].get('Path') == str(MEDIA / path), 'A target lacks a unique accepted catalog witness.')
    manifest, media = helper.media_snapshot(op, owner, MANIFEST_SHA)
    require(all(path in manifest['files'] for _, path in TARGETS.values()), 'A target is absent from the exact media inventory.')
    return helper, op, owner, browser, index, media


class Recorder:
    def __init__(self, args, inputs, control):
        require(args.variant in VARIANTS and
                CONTROL == WORK / ('reference-similarity-combinations-v3-' + args.variant) and
                MARKER == 'goby-reference-similarity-combinations-m3e-v3-' + args.variant,
                'The recorder variant does not match its independent evidence directory.')
        self.args = args
        self.helper, self.op, self.owner, self.browser, self.index, self.media = inputs
        self.control, self.user = control, self.browser['accounts']['admin']
        self.device, self.token, self.proven, self.revoked = control['deviceId'], None, False, False
        self.secrets = {account['password'] for account in self.browser['accounts'].values()}
        self.started = time.monotonic()
        self.deadline = self.started + MAIN_SECONDS
        self.count, self.received, self.phase = 0, 0, 'login'
        self.sent, self.allowed_gets, self.allowed_posts = set(), set(), {}
        self.baseline, self.details, self.active = None, {}, None
        self.cases, self.failure, self.recovery, self.restored = [], None, None, {}
        self.old_ids, self.old_facts = [], {}
        self.preserved, self.media_preserved, self.final_snapshot_attempted = False, False, False
        self.seed_preserved, self.seed_check_attempted = False, False
        self.cleanup_proof = None
        self.get_route('/emby/Users')
        self.get_route('/emby/System/Configuration')
        self.get_route('/emby/Library/VirtualFolders/Query')
        self.get_route('/emby/Sessions')

    def save(self, name, value, public=False):
        require(re.fullmatch(r'[a-z0-9-]{1,100}\.json', name), 'An evidence filename is not canonical.')
        self.op.save((EXPORT if public else PRIVATE) / name, value)

    def check(self):
        require(time.monotonic() < self.deadline, 'The bounded operation deadline expired.')
        require(sha(protected_bytes(Path(__file__).absolute())) == self.args.script_sha256 and
                sha(protected_bytes(Path(self.args.snapshot_helper))) == HELPER_SHA and
                sha(protected_bytes(OPERATOR)) == OPERATOR_SHA and sha(protected_bytes(self.op.OWNER)) == OWNER_SHA and
                sha(protected_bytes(INDEX)) == INDEX_SHA, 'An accepted input changed during the operation.')
        require(equal(decode(protected_bytes(CONTROL / 'OWNER.json')), self.control) and
                self.op.digest(self.op.BROWSER) == self.owner['browserSha256'] and
                self.op.digest(self.op.REPORT) == self.owner['reportSha256'], 'A current ownership or credential receipt changed.')
        self.op.same_service(self.owner)
        require(os.readlink('/proc/self/ns/net') == self.owner['serviceIdentity']['networkNamespace'],
                'HTTP is outside the exact reference namespace.')

    def get_route(self, path, query=None):
        require(path.startswith('/emby/') and '?' not in path and '#' not in path, 'A read path is not canonical.')
        route = path + ('?' + urlencode(query) if query else '')
        self.allowed_gets.add(route)
        return route

    def request(self, label, method, route, body=None, mutation=None):
        self.check()
        cleanup = mutation == 'logout' or label == 'logout-denied'
        require(self.count < (MAX_REQUESTS if cleanup else MAX_REQUESTS - 2), 'The reserved request budget is exhausted.')
        require(re.fullmatch(r'[a-z0-9-]{1,70}', label), 'A request label is not bounded.')
        login = mutation == 'login'
        if method == 'GET':
            require(route in self.allowed_gets and body is None and mutation is None and self.proven,
                    'A read is outside the registered authenticated scope.')
        else:
            require(method == 'POST' and mutation not in self.sent, 'A POST must have a unique one-shot intent.')
            if login:
                require(route == '/emby/Users/AuthenticateByName' and self.token is None and
                        equal(body, {'Username': self.user['username'], 'Pw': self.user['password']}), 'The sole login differs.')
            elif mutation == 'logout':
                require(route == '/emby/Sessions/Logout' and body is None and self.token is not None and self.proven,
                        'Logout does not refer to the fully proven new administrator and device token.')
            else:
                require(self.proven and mutation in self.allowed_posts and
                        equal(self.allowed_posts[mutation], {'route': route, 'body': body}),
                        'A metadata write differs from its exact durable intent.')
                require(route in ('/emby/Items/40', '/emby/Items/49'), 'A metadata write escaped the two owned movies.')
        payload = None if body is None else encoded(body)
        require(payload is None or len(payload) <= 65536, 'An outgoing body is oversized.')
        self.count += 1
        sequence = self.count
        self.save(f'{sequence:04d}-{label}-intent.json',
                  {'sequence': sequence, 'phase': self.phase, 'method': method, 'route': route,
                   'mutation': mutation, 'body': None if login else body, 'elapsedSeconds': time.monotonic() - self.started})
        if mutation is not None:
            self.sent.add(mutation)
        headers = {'Accept': 'application/json', 'Accept-Encoding': 'identity',
                   'Authorization': f'Emby Client="{CLIENT}", Device="Linux Similarity Recorder", DeviceId="{self.device}", Version="1.0"'}
        if self.token is not None and not login:
            headers['X-Emby-Token'] = self.token
        if payload is not None:
            headers['Content-Type'] = 'application/json'
        connection = http.client.HTTPConnection('127.0.0.1', 18097, timeout=min(6, max(.01, self.deadline - time.monotonic())))
        status, value, raw, content_type, complete, failure = None, None, b'', None, False, None
        signal.setitimer(signal.ITIMER_REAL, min(9, max(.01, self.deadline - time.monotonic())))
        try:
            connection.request(method, route, payload, headers)
            response = connection.getresponse()
            status, content_type = response.status, response.getheader('Content-Type')
            require(not 300 <= status < 400, 'Redirects are not accepted.')
            length = response.getheader('Content-Length')
            require(length is None or length.isdigit() and int(length) <= MAX_BODY, 'A response length is invalid or oversized.')
            raw = response.read(MAX_BODY + 1)
            self.received += len(raw)
            require(len(raw) <= MAX_BODY and self.received <= MAX_TOTAL and
                    (length is None or int(length) == len(raw)), 'A response is oversized or incomplete.')
            if raw:
                value = decode(raw) if content_type and content_type.lower().split(';', 1)[0].strip() == 'application/json' else raw.decode('utf-8')
            if login and isinstance(value, dict) and isinstance(value.get('AccessToken'), str) and value['AccessToken']:
                self.token = value['AccessToken']
                self.secrets.add(self.token)
                self.save('recorder-login.json', value)
            complete = True
        except Exception as error:
            failure = type(error).__name__
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            connection.close()
            self.save(f'{sequence:04d}-{label}-response.json',
                      {'sequence': sequence, 'status': status, 'contentType': content_type, 'bytes': len(raw),
                       'sha256': sha(raw), 'complete': complete, 'body': value, 'failureType': failure,
                       'elapsedSeconds': time.monotonic() - self.started})
        if login and self.token is not None:
            self.proven = (complete and status == 200 and isinstance(value, dict) and
                           value.get('ServerId') == self.owner['serverId'] and value.get('User', {}).get('Id') == self.user['userId'] and
                           value['User'].get('Name') == self.user['username'] and value['User'].get('Policy', {}).get('IsAdministrator') is True and
                           value.get('SessionInfo', {}).get('DeviceId') == self.device and value['SessionInfo'].get('UserId') == self.user['userId'])
        require(failure is None and complete, 'A request outcome is incomplete; its POST cannot be retried.')
        self.check()
        require(not login or self.proven, 'The new token does not prove the exact server, administrator, and device.')
        return status, value, sequence

    def get(self, label, route):
        status, value, _ = self.request(label, 'GET', route)
        require(status == 200, 'A required read did not return HTTP 200.')
        return value

    def detail(self, label, item_id):
        require(item_id in TARGETS, 'A detail read escaped the three receipted movies.')
        row = self.get(label, self.get_route('/emby/Users/' + self.user['userId'] + '/Items/' + item_id))
        verify_target(row, item_id)
        return row

    def snapshot(self, label):
        users = self.get(label + '-users', '/emby/Users')
        require(isinstance(users, list) and len(users) == 5, 'The receipted five-account population changed.')
        result = {'users': {}, 'libraries': self.get(label + '-libraries', '/emby/Library/VirtualFolders/Query'),
                  'configuration': self.get(label + '-configuration', '/emby/System/Configuration'), 'itemsByUser': {}}
        for row in users:
            require(isinstance(row, dict) and isinstance(row.get('Configuration'), dict) and isinstance(row.get('Policy'), dict),
                    'A current account lacks Configuration or Policy.')
            key = identifier(row.get('Id'))
            require(key not in result['users'], 'The account population repeats an identity.')
            result['users'][key] = {field: row[field] for field in ('Id', 'Name', 'Configuration', 'Policy')}
        require(set(result['users']) == set(self.index['existingUserIds']), 'An unreceipted account appeared or disappeared.')
        original = self.op.read_private(self.op.REPORT)
        if not self.old_ids:
            self.old_facts = {identifier(row.get('Id')): (row.get('Path'), row.get('Type')) for row in original['items']
                              if row.get('Type') in ('Movie', 'Audio', 'Episode')}
            require(set(self.old_facts) == {'9', '10', '11', '17', '18', '19'} and all(
                path.startswith(str(self.op.MEDIA) + '/') for path, _ in self.old_facts.values()), 'The six original playable receipts differ.')
            self.old_ids = sorted(self.old_facts)
        expected_libraries = {row['ItemId'] for row in original['libraries']} | set(self.index['newLibraryIds'].values())
        # Virtual-folder DTOs use ItemId rather than the catalog Id field.
        require(isinstance(result['libraries'], dict) and isinstance(result['libraries'].get('Items'), list) and
                result['libraries'].get('TotalRecordCount', 6) == 6, 'The virtual-library inventory is incomplete.')
        library_rows = result['libraries'].get('Items', [])
        require(len(library_rows) == 6 and {row.get('ItemId') for row in library_rows} == expected_libraries,
                'The six acknowledged libraries changed identity.')
        for row in library_rows:
            matches = [name for name, key in self.index['newLibraryIds'].items() if key == row.get('ItemId')]
            if matches:
                self.helper.verify_new_library(self.op, row, matches[0])
            else:
                names = [name for name in self.op.LIBRARIES if row.get('Name') == 'M3e Reference ' + name]
                require(len(names) == 1, 'An original library changed its name.')
                self.op.verify_library(row, names[0])
        for number, user_id in enumerate(sorted(result['users'])):
            query = {'Recursive': 'true', 'IncludeItemTypes': 'Movie,Episode,Audio', 'Fields': WITNESS_FIELDS,
                     'Ids': ','.join(self.old_ids + ['40', '49']), 'Limit': '100',
                     'EnableTotalRecordCount': 'true', 'EnableUserData': 'true'}
            rows = self.helper.rows(self.get(f'{label}-user-{number}-items', self.get_route('/emby/Users/' + user_id + '/Items', query)), 100)
            expected_visible = set(self.index['originalVisibleItemIdsByUser'][user_id])
            require(set(rows) & set(self.old_ids) == expected_visible and set(rows) <= set(self.old_ids + ['40', '49']),
                    'A user changed its visible original-item subset or escaped the bounded witness.')
            require(user_id != self.user['userId'] or set(rows) == set(self.old_ids + ['40', '49']),
                    'The administrator cannot witness all eight scoped items.')
            for item_id, row in rows.items():
                require(isinstance(row.get('UserData'), dict), 'A witnessed item lacks UserData.')
                if item_id in self.old_facts:
                    require((row.get('Path'), row.get('Type')) == self.old_facts[item_id], 'An original identity or path changed.')
                else:
                    verify_target(row, item_id)
            result['itemsByUser'][user_id] = rows
        self.save(label + '-snapshot.json', result)
        return result

    def prepare_case(self, item_id):
        require(self.args.variant in VARIANTS and item_id in ('40', '49'), 'An unapproved case or variant was requested.')
        original = self.details[item_id]
        seed = self.details['35']
        body = edit_body(original)
        genres = seed.get('Genres')
        genre_items = seed.get('GenreItems')
        require(genres == ['Drama', 'Adventure'] and isinstance(genre_items, list) and len(genre_items) == 2,
                'The seed does not expose the two actually indexed genre names and references.')
        drama = [row for row in genre_items if row.get('Id') == 53 and row.get('Name') == 'Drama']
        adventure = [row for row in genre_items if row.get('Id') == 54 and row.get('Name') == 'Adventure']
        studios = [row for row in seed.get('Studios', []) if row.get('Id') == 50 and row.get('Name') == 'SharedStudio']
        seed_actor = [row for row in seed.get('People', []) if row.get('Id') == '51' and
                      row.get('Name') == 'CommonActor' and row.get('Type') == 'Actor']
        require(len(drama) == len(adventure) == len(studios) == len(seed_actor) == 1,
                'The actual seed genre, studio, or shared actor identity is not unique.')
        require(not original.get('Studios'), 'A target already has a studio outside its original observed baseline.')
        changed, expected = [], {}
        if item_id == '40':
            require(original.get('Genres') == [drama[0]['Name']] and equal(original.get('GenreItems'), drama) and
                    not original.get('People') and not original.get('Tags') and not original.get('TagItems'),
                    'GenreOne differs from its observed single-genre baseline.')
            body['Studios'] = copy.deepcopy(studios)
            changed.append('Studios')
            expected['Studios'] = copy.deepcopy(studios)
        else:
            require(equal(original.get('People'), seed_actor) and not original.get('Genres') and not original.get('GenreItems'),
                    'Actor differs from its observed shared-actor and empty-genre baseline.')
        if self.args.variant == 'ranking' or item_id == '49':
            selected = copy.deepcopy(genre_items if self.args.variant == 'ranking' else drama)
            body['Genres'] = copy.deepcopy(genres if self.args.variant == 'ranking' else [drama[0]['Name']])
            changed.extend(('Genres', 'GenreItems'))
            expected.update(Genres=copy.deepcopy(body['Genres']), GenreItems=selected)
        original_body = edit_body(original)
        edited = {key for key in set(body) | set(original_body) if not equal(
                  {'present': key in body, 'value': body.get(key)},
                  {'present': key in original_body, 'value': original_body.get(key)})}
        require(edited and edited <= {'Genres', 'Studios'}, 'A variant changed an unapproved metadata source field.')
        return {'itemId': item_id, 'variant': self.args.variant, 'original': copy.deepcopy(original), 'body': body, 'changed': tuple(changed),
                'expected': expected, 'mutation': 'modify-' + item_id, 'restore': 'restore-' + item_id,
                'mutationSent': False, 'restoreSent': False, 'restored': False, 'automaticChanges': []}

    def classify(self, case, current):
        verify_target(current, case['itemId'])
        changes = automatic_changes(case['original'], current)
        original, observed = business(case['original']), business(current)
        if equal(original, observed):
            return 'original', changes
        unchanged_original = {key: value for key, value in original.items() if key not in case['changed']}
        unchanged_observed = {key: value for key, value in observed.items() if key not in case['changed']}
        if not equal(unchanged_original, unchanged_observed):
            return 'foreign-or-uncertain', changes
        accepted = all(key in current and equal(current[key], expected) for key, expected in case['expected'].items())
        return ('mutated' if accepted else 'foreign-or-uncertain'), changes

    def authorize_post(self, case, restoring):
        mutation = case['restore'] if restoring else case['mutation']
        body = edit_body(case['original']) if restoring else case['body']
        _, current_media = self.helper.media_snapshot(self.op, self.owner, MANIFEST_SHA)
        require(equal(self.media, current_media), 'Media changed before a metadata write.')
        self.save(mutation + '-durable-intent.json',
                  {'marker': MARKER, 'itemId': case['itemId'], 'method': 'POST', 'path': '/emby/Items/' + case['itemId'],
                   'body': body, 'originalDetailSha256': sha(encoded(case['original'])), 'oneShot': True})
        self.allowed_posts[mutation] = {'route': '/emby/Items/' + case['itemId'], 'body': copy.deepcopy(body)}
        # Mark before invoking transport; a failed request is never resent.
        case['restoreSent' if restoring else 'mutationSent'] = True
        status, _, _ = self.request(mutation, 'POST', '/emby/Items/' + case['itemId'], body, mutation)
        require(status in (200, 204), 'A metadata POST did not return an accepted status.')

    def run_case(self, item_id):
        case = self.prepare_case(item_id)
        self.active = case
        self.phase = 'case-' + item_id
        self.save('case-' + item_id + '-original.json', {'detail': case['original'], 'editFields': edit_body(case['original'])})
        current = self.detail('case-' + item_id + '-prewrite', item_id)
        state, changes = self.classify(case, current)
        require(state == 'original', 'The movie changed after its baseline; no write is authorized.')
        case['automaticChanges'].append({'phase': 'prewrite', 'fields': changes})
        self.authorize_post(case, False)
        current = self.detail('case-' + item_id + '-accepted', item_id)
        state, changes = self.classify(case, current)
        require(state == 'mutated', 'The server did not accept exactly the intended metadata combination.')
        case['automaticChanges'].append({'phase': 'accepted', 'fields': changes})
        route = self.get_route('/emby/Items/35/Similar', {'UserId': self.user['userId'], 'Limit': '12',
                              'Fields': SIMILAR_FIELDS, 'ImageTypeLimit': '1'})
        observations = []
        for number in range(1, SIMILAR_READS[self.args.variant] + 1):
            label = f'case-{item_id}-similar-{number:02d}'
            status, value, sequence = self.request(label, 'GET', route)
            require(status == 200 and isinstance(value, dict) and isinstance(value.get('Items'), list) and
                    len(value['Items']) <= 12 and type(value.get('TotalRecordCount')) is int,
                    'The bounded Similar response has an unreviewed shape.')
            self.save(label + '.json', {'sequence': sequence, 'request': {'method': 'GET', 'route': route},
                      'status': status, 'body': value, 'classification': 'Protocol observation; no score or rank is asserted.'})
            observations.append({'sequence': sequence, 'observation': number,
                                 'returnedIds': [identifier(row.get('Id')) for row in value['Items']],
                                 'totalRecordCount': value['TotalRecordCount'],
                                 'targetReturned': any(row.get('Id') == item_id for row in value['Items'])})
        case['similar'] = {'observations': observations, 'acceptedMetadata': copy.deepcopy(case['expected']),
                           'mutatedBusinessStateConfirmedBeforeAndAfterReads': False}
        current = self.detail('case-' + item_id + '-prerestore', item_id)
        state, changes = self.classify(case, current)
        require(state == 'mutated', 'The accepted movie changed before restoration; no overwrite is authorized.')
        case['automaticChanges'].append({'phase': 'prerestore', 'fields': changes})
        case['similar']['mutatedBusinessStateConfirmedBeforeAndAfterReads'] = True
        self.authorize_post(case, True)
        restored = self.detail('case-' + item_id + '-restored', item_id)
        state, changes = self.classify(case, restored)
        require(state == 'original' and equal(edit_body(restored), edit_body(case['original'])),
                'The complete original business metadata and edit fields were not restored.')
        case['automaticChanges'].append({'phase': 'restored', 'fields': changes})
        case['restored'] = True
        self.restored[item_id] = True
        self.cases.append({key: case[key] for key in ('itemId', 'variant', 'similar', 'restored', 'automaticChanges')})
        self.active = None

    def recover(self):
        case = self.active
        if case is None or not case['mutationSent']:
            self.recovery = {'result': 'no-pending-metadata-write'}
            return
        current = self.detail('recovery-readback', case['itemId'])
        state, changes = self.classify(case, current)
        self.recovery = {'result': state, 'itemId': case['itemId'], 'automaticChanges': changes,
                         'restoreAlreadySent': case['restoreSent']}
        self.save('recovery-readback-state.json', self.recovery)
        if state == 'original':
            require(equal(edit_body(current), edit_body(case['original'])), 'The recovered original edit fields differ.')
            self.restored[case['itemId']] = True
            self.recovery['result'] = 'original-observed-no-write-needed'
            return
        require(state == 'mutated' and not case['restoreSent'], 'A foreign, uncertain, or already-restored state cannot authorize another POST.')
        self.authorize_post(case, True)
        after = self.detail('recovery-restored', case['itemId'])
        state, changes = self.classify(case, after)
        require(state == 'original' and equal(edit_body(after), edit_body(case['original'])), 'The one-shot recovery did not restore original business metadata.')
        self.restored[case['itemId']] = True
        self.recovery.update(result='original-restored-once', automaticChanges=changes)

    def preserve(self):
        if self.baseline is None or self.final_snapshot_attempted:
            return
        self.final_snapshot_attempted = True
        after = self.snapshot('after')
        for field in ('users', 'libraries', 'configuration'):
            require(equal(self.baseline[field], after[field]), 'An account, policy, configuration, or library changed.')
        differences = []
        for user_id, old_rows in self.baseline['itemsByUser'].items():
            new_rows = after['itemsByUser'][user_id]
            require(set(old_rows) == set(new_rows), 'A user changed its scoped item visibility.')
            for item_id, old in old_rows.items():
                new = new_rows[item_id]
                if item_id in ('40', '49'):
                    changed = automatic_changes(old, new)
                    require(equal(business(old), business(new)) and equal(old['UserData'], new['UserData']),
                            'A modified movie did not preserve original business metadata and UserData.')
                    if changed:
                        differences.append({'userId': user_id, 'itemId': item_id, 'fields': changed})
                else:
                    require(equal(old, new), 'Original playable metadata or UserData changed.')
        self.save('preservation-automatic-differences.json', differences)
        self.preserved = True

    def preserve_seed(self):
        if '35' not in self.details or self.seed_check_attempted:
            return
        self.seed_check_attempted = True
        require(equal(self.details['35'], self.detail('seed-final', '35')), 'The read-only seed detail changed.')
        self.seed_preserved = True

    def logout(self):
        if self.token is None:
            self.cleanup_proof = {'result': 'no-token-acknowledged', 'revocationProven': False}
            return
        if not self.proven:
            self.cleanup_proof = {'result': 'unproven-acknowledgement-quarantined', 'revocationProven': False,
                                  'source': 'private/recorder-login.json', 'identityProven': False,
                                  'tokenUsedForCleanup': False, 'retainedForReview': True}
            self.save('unproven-login-quarantine.json', self.cleanup_proof)
            return
        token = self.token
        outcome = {'result': 'unresolved', 'logoutStatus': None, 'exactTokenStatus': None}
        self.save('logout-owned-token-intent.json', {'source': 'private/recorder-login.json', 'deviceId': self.device,
                                                   'provenLoginIdentity': self.proven, 'oneShot': True})
        try:
            status, _, _ = self.request('logout', 'POST', '/emby/Sessions/Logout', mutation='logout')
            outcome['logoutStatus'] = status
        finally:
            try:
                require(self.proven and self.token == token, 'The proven cleanup token or identity changed.')
                status, _, _ = self.request('logout-denied', 'GET', '/emby/Sessions')
                outcome['exactTokenStatus'] = status
                self.revoked = outcome['logoutStatus'] == 204 and status == 401
                outcome['result'] = 'logout204-exact-token401' if self.revoked else 'unresolved'
            finally:
                self.cleanup_proof = outcome
                self.save('logout-proof.json', outcome)
        require(self.revoked, 'The acknowledged token lacks logout 204 and exact-token 401 proof.')

    def safe(self, value):
        if isinstance(value, dict):
            return {key: self.safe(entry) for key, entry in value.items()}
        if isinstance(value, list):
            return [self.safe(entry) for entry in value]
        if isinstance(value, str):
            for secret in sorted(self.secrets, key=len, reverse=True):
                value = value.replace(secret, '[secret]')
            return URL.sub('[url]', value)
        return value

    def run(self):
        try:
            self.request('login', 'POST', '/emby/Users/AuthenticateByName',
                         {'Username': self.user['username'], 'Pw': self.user['password']}, 'login')
            self.phase = 'baseline'
            self.baseline = self.snapshot('before')
            for item_id in ('35', '40', '49'):
                self.details[item_id] = self.detail('baseline-detail-' + item_id, item_id)
            self.save('baseline-details.json', self.details)
            self.run_case('40')
            self.run_case('49')
            self.phase = 'seed-preservation'
            self.preserve_seed()
            self.phase = 'preservation'
            self.preserve()
        except Exception as error:
            self.failure = {'type': type(error).__name__, 'phase': self.phase}
        finally:
            self.deadline = time.monotonic() + RECOVERY_SECONDS
            if self.proven:
                try:
                    self.phase = 'bounded-recovery'
                    self.recover()
                except Exception as error:
                    self.recovery = {**(self.recovery or {}), 'failureType': type(error).__name__, 'retainedForReview': True}
                try:
                    self.phase = 'preservation'
                    self.preserve()
                except Exception as error:
                    self.failure = self.failure or {'type': type(error).__name__, 'phase': self.phase}
                try:
                    self.phase = 'seed-preservation'
                    self.preserve_seed()
                except Exception as error:
                    self.failure = self.failure or {'type': type(error).__name__, 'phase': self.phase}
            try:
                self.phase = 'media-preservation'
                _, media = self.helper.media_snapshot(self.op, self.owner, MANIFEST_SHA)
                require(equal(self.media, media), 'Original or auxiliary media bytes, inodes, or manifests changed.')
                self.media_preserved = True
            except Exception as error:
                self.failure = self.failure or {'type': type(error).__name__, 'phase': self.phase}
            self.deadline = time.monotonic() + LOGOUT_SECONDS
            try:
                self.phase = 'logout'
                self.logout()
            except Exception as error:
                self.failure = self.failure or {'type': type(error).__name__, 'phase': self.phase}
        success = (self.failure is None and len(self.cases) == 2 and all(self.restored.get(key) for key in ('40', '49')) and
                   self.preserved and self.seed_preserved and self.media_preserved and self.revoked)
        report = {'marker': MARKER, 'result': 'complete' if success else 'retained_for_review',
                  'variant': self.args.variant,
                  'classification': 'Reference-only metadata protocol research; not client acceptance or a scoring specification.',
                  'scriptSha256': self.args.script_sha256, 'helperSha256': HELPER_SHA, 'operatorSha256': OPERATOR_SHA,
                  'ownerSha256': OWNER_SHA, 'indexReportSha256': INDEX_SHA, 'manifestSha256': MANIFEST_SHA,
                  'referencePID': PID, 'referenceStartTicks': TICKS, 'referenceBinarySha256': BINARY_SHA,
                  'cases': self.cases, 'restoredBusinessMetadata': self.restored, 'recovery': self.recovery,
                  'originalSixItemsAndAllFiveUsersPreserved': self.preserved, 'allMediaPreserved': self.media_preserved,
                  'readOnlySeedFullDetailPreserved': self.seed_preserved,
                  'userDataScope': 'Exact equality of every returned UserData field for all five users visible subsets of the original six playables and the two target movies, using the accepted bounded Items projection.',
                  'unreturnedUserDataFieldsClaimed': False,
                  'rawDetailEqualityClaimedForModifiedItems': False, 'automaticFieldAllowlist': sorted(AUTOMATIC_FIELDS),
                  'newRecorderTokenRevoked': self.revoked, 'cleanup': self.cleanup_proof,
                  'requestAttempts': self.count, 'maximumRequestAttempts': MAX_REQUESTS,
                  'normalRequestPlan': NORMAL_REQUESTS[self.args.variant], 'similarReadsPerCase': SIMILAR_READS[self.args.variant],
                  'normalSeconds': MAIN_SECONDS, 'recoverySeconds': RECOVERY_SECONDS, 'logoutSeconds': LOGOUT_SECONDS,
                  'bytesRead': self.received, 'elapsedSeconds': time.monotonic() - self.started,
                  'sentMutationIntents': sorted(self.sent), 'failure': self.failure, 'retryPermitted': False}
        safe = self.safe(report)
        require(not any(secret in encoded(safe).decode() for secret in self.secrets), 'A credential survived safe export.')
        self.save('report.json', safe, public=True)
        print(json.dumps({'result': report['result'], 'report': str(EXPORT / 'report.json'),
                          'newRecorderTokenRevoked': self.revoked, 'retryPermitted': False}), flush=True)
        return 0 if success else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--variant', required=True, choices=VARIANTS)
    parser.add_argument('--script-sha256', required=True)
    parser.add_argument('--snapshot-helper', required=True, help='Absolute path to accepted helper SHA ' + HELPER_SHA)
    parser.add_argument('--worker', action='store_true', help=argparse.SUPPRESS)
    parser.add_argument('--lock-fd', type=int, help=argparse.SUPPRESS)
    args = parser.parse_args()
    select_variant(args.variant)
    os.umask(0o077)
    signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(TimeoutError('A bounded request expired.')))
    inputs = load_inputs(args)
    _, op, owner, _, _, media = inputs
    binding = {'scriptSha256': args.script_sha256, 'snapshotHelper': args.snapshot_helper, 'variant': args.variant,
               'helperSha256': HELPER_SHA, 'operatorSha256': OPERATOR_SHA, 'ownerSha256': OWNER_SHA,
               'indexReportSha256': INDEX_SHA, 'manifestSha256': MANIFEST_SHA}
    if args.worker:
        require(type(args.lock_fd) is int and args.lock_fd > 2, 'The worker lacks its inherited lock.')
        control = decode(protected_bytes(CONTROL / 'OWNER.json'))
        require(control.get('marker') == MARKER and control.get('path') == str(CONTROL) and equal(control.get('inputs'), binding) and
                equal(op.process_identity(os.getppid()), control.get('supervisor')) and equal(control.get('media'), media) and
                identity(os.fstat(args.lock_fd)) == identity(op.canonical(op.LOCK, mode=0o600)) and
                os.readlink('/proc/self/ns/net') == owner['serviceIdentity']['networkNamespace'] and
                re.fullmatch(r'goby-m3e-similarity-combinations-[0-9a-f]{32}', control.get('deviceId', '')),
                'The worker is not bound to the live supervisor, exact inputs, lock, media, and namespace.')
        op.save(PRIVATE / 'worker.json', op.process_identity(os.getpid()))
        return Recorder(args, inputs, control).run()
    require(args.lock_fd is None and not op.present(CONTROL), 'Existing combination evidence cannot be adopted or retried.')
    lock = os.open(op.LOCK, os.O_RDWR | os.O_NOFOLLOW)
    namespace = None
    try:
        require(identity(os.fstat(lock)) == identity(op.canonical(op.LOCK, mode=0o600)), 'The reference lock changed while opening.')
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        require(not op.present(CONTROL), 'The output path became occupied.')
        CONTROL.mkdir(mode=0o700)
        PRIVATE.mkdir(mode=0o700)
        EXPORT.mkdir(mode=0o700)
        control = {'marker': MARKER, 'path': str(CONTROL), 'inputs': binding, 'media': media,
                   'serviceIdentity': owner['serviceIdentity'], 'supervisor': op.process_identity(os.getpid()),
                   'deviceId': 'goby-m3e-similarity-combinations-' + secrets.token_hex(16)}
        op.save(CONTROL / 'OWNER.json', control)
        op.sync_directory(WORK)
        namespace = os.open(f'/proc/{PID}/ns/net', os.O_RDONLY)
        require(os.fstat(namespace).st_ino == int(owner['serviceIdentity']['networkNamespace'][5:-1]), 'The namespace handle changed.')
        op.same_service(owner)
        require(sha(protected_bytes(Path(__file__).absolute())) == args.script_sha256, 'The source changed before worker dispatch.')
        arguments = ['/usr/bin/nsenter', '--net=/proc/self/fd/' + str(namespace), '/usr/bin/python3', '-I', '-B',
                     str(Path(__file__).absolute()), '--script-sha256', args.script_sha256,
                     '--snapshot-helper', args.snapshot_helper, '--variant', args.variant, '--worker', '--lock-fd', str(lock)]
        op.save(PRIVATE / 'worker-dispatch-intent.json', {'inputs': binding, 'supervisor': control['supervisor']})
        result = subprocess.run(arguments, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=MAIN_SECONDS + RECOVERY_SECONDS + LOGOUT_SECONDS + 45, check=False,
                                pass_fds=(lock, namespace), env=dict(op.BASE_ENV, SSH_CONNECTION=os.environ['SSH_CONNECTION']))
        require(len(result.stdout) <= 8192 and len(result.stderr) <= 8192, 'The worker exceeded its output bound.')
        op.save(PRIVATE / 'worker-exit.json', {'returnCode': result.returncode, 'stdoutSha256': sha(result.stdout), 'stderrSha256': sha(result.stderr)})
        print(json.dumps({'result': 'complete' if result.returncode == 0 else 'retained_for_review',
                          'report': str(EXPORT / 'report.json'), 'evidence': str(CONTROL), 'retryPermitted': False}), flush=True)
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
