#!/usr/bin/env python3
"""Observe one reference Name change and exact restoration through a passive UI.

The business-read-only preflight owns a separate fresh administrator login.
Neither mode reads a reference database, implementation asset, or executable
content. Only the new observation mode may reserve its two metadata POSTs.
"""

from __future__ import annotations

import sys
sys.dont_write_bytecode = True

import argparse
import copy
import datetime as dt
from decimal import Decimal
import fcntl
import hashlib
import http.client
import json
import math
import os
from pathlib import Path
import re
import secrets
import signal
import stat
import subprocess
import time
import types
from urllib.parse import parse_qsl, urlencode, urlsplit

WORK = Path('/opt/goby-test/exec-work-m3e')
TOOL = WORK / 'reference-library-changed-ui-tool-02'
ROOT = WORK / 'reference-library-changed-ui-v2'
PREFLIGHT_ROOT = WORK / 'reference-library-changed-ui-preflight-v2'
WORKER_UNIT = 'goby-reference-library-changed-ui-v2.service'
CONTROLLER_UNIT = 'goby-reference-library-changed-ui-controller-v2.service'
PREFLIGHT_UNIT = 'goby-reference-library-changed-ui-preflight-v2.service'
MARKER = 'goby-reference-library-changed-observation-v1'
INPUT_MARKER = 'goby-reference-library-changed-input-v1'
SNAPSHOT_MARKER = 'goby-reference-library-changed-public-snapshot-v1'
PREFLIGHT_MARKER = 'goby-reference-library-changed-preflight-v1'
MODE = 'reference-movies-name-automatic-refresh'
SERVER = 'f56dec8ff7414847873064c4be9fba74'
ADMIN = 'efe2137dc3394ed4a23f9c337598f105'
VIEWER = 'c5f36699a54f4971a891682cd9de410f'
ADMIN_NAME, VIEWER_NAME = 'm3e-reference-admin', 'm3e-library-changed-viewer-v1'
LIBRARY, FOLDER, ITEM, ANCHOR = '93', '99', '100', '96'
BASE_URL = 'http://127.0.0.1:18197'
PID, TICKS = 332054, '357218'
BOOT = '6bdfc486-7bc8-412f-82b5-70095a09dde7'
NAMESPACE = 'net:[4026532602]'
REFERENCE_INVOCATION = 'aa59e192c1c846459bc3f218bd923364'
OPERATOR = WORK / 'prepare-client-reference.py'
OPERATOR_SHA = '29f159a2580e178dce65b6c23ef14ad9875971240e534546eb5e8a14ce88c160'
OWNER = WORK / 'reference-owner.json'
OWNER_SHA = '3bdbeee28406f9809624f3619b38915ab4445fa1a7c8aca30cbe445d3d1ddb40'
ACCOUNTS = WORK / 'reference-browser.json'
ACCOUNTS_SHA = 'e72b02b97fb526d026056567d278ff4668f200f5acba25230d1ec46735d87d6d'
VIEWER_CREDENTIALS = WORK / 'reference-library-changed-continuation-v1/private/credentials.json'
VIEWER_CREDENTIALS_SHA = 'd8b03f3b0f68c29b24fbc96023cb9c4f51a8a0409ccbc948babf02e3f1bf32b4'
PROXY_STATUS = WORK / 'dual-proxy-status-02.json'
PROXY_STATUS_SHA = '769684e6c84eb4ea83c01f86c671d94d488626581c8dde664fa6f7ab7c05310f'
NODE = WORK / 'client-library-changed-source44-tool-01/node'
NODE_SHA = '3517c2df0b2f8cd7f422b4b8450ef81c6889f08eb03e281d6de9079b15e6a327'
MEDIA_ROOTS = tuple(Path('/opt/goby-fixtures') / name for name in (
    'client-m3e', 'client-aux-m3e-v1', 'client-special-features-m3e-v1', 'client-library-changed-v1'))
MOVIES = MEDIA_ROOTS[-1] / 'Movies'
TARGET_PATH = str(MOVIES / 'LibraryChanged Observed (2031)/LibraryChanged Observed (2031).mp4')
ANCHOR_PATH = str(MOVIES / 'LibraryChanged Anchor (2030)/LibraryChanged Anchor (2030).mp4')
JS_NAMES = ('client-browser-library-changed-reference.mjs', 'client-browser-library-changed-reference-runtime.mjs',
            'client-browser-session-proof.mjs')
EDIT_FIELDS = ('Name', 'SortName', 'ForcedSortName', 'OriginalTitle', 'Overview', 'ProductionYear', 'PremiereDate',
    'EndDate', 'CommunityRating', 'CriticRating', 'OfficialRating', 'CustomRating', 'ProviderIds', 'Genres', 'Tags', 'TagItems',
    'Studios', 'People', 'LockedFields', 'LockData', 'Taglines', 'ProductionLocations', 'PreferredMetadataLanguage',
    'PreferredMetadataCountryCode', 'IndexNumber', 'ParentIndexNumber', 'SortIndexNumber', 'SortParentIndexNumber', 'DisplayOrder', 'Status', 'DateCreated')
AUTOMATIC_FIELDS = frozenset(('Etag', 'ETag', 'DateLastSaved', 'DateLastRefreshed'))
SORT_FIELDS = ('SortName', 'ForcedSortName')
MARKER_NAME = 'reference library changed ui two'
AUTH_TIME_FIELDS = frozenset(('LastLoginDate', 'LastActivityDate'))
FIELDS = 'Path,ParentId,SortName,MediaSources,MediaStreams,Overview,Genres,Tags,People,Studios,ProviderIds,DateCreated,ProductionYear'
STAGES, CONTROLS = ('discovery', 'armed', 'restore-armed', 'restored'), ('reserved', 'forward', 'restored', 'close')
MAX_JSON, MAX_IPC, MAX_REPORT = 16 << 20, 512 << 10, 4 << 20
MAX_HTTP, MAX_TOTAL, TOTAL_SECONDS = 256, 160 << 20, 900
WORK_HTTP, CLEANUP_HTTP = 240, 16
WORK_BYTES, CLEANUP_BYTES = 144 << 20, 16 << 20
HASH, USER_ID, ITEM_ID = re.compile('[0-9a-f]{64}'), re.compile('[0-9a-f]{32}'), re.compile('[1-9][0-9]{0,18}')
ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8'}


class ObservationError(Exception):
    """An owned reference observation or exact preservation boundary failed."""


def require(value, message):
    if not value:
        raise ObservationError(message)


def exact(value):
    if value is None: return 'null'
    if type(value) is bool: return 'true' if value else 'false'
    if type(value) is int or isinstance(value, Decimal):
        require(not isinstance(value, Decimal) or value.is_finite(), 'A JSON number is nonfinite.')
        return str(value)
    if isinstance(value, str): return json.dumps(value, ensure_ascii=False)
    if isinstance(value, list): return '[' + ','.join(exact(entry) for entry in value) + ']'
    require(isinstance(value, dict) and all(isinstance(key, str) for key in value), 'Unsupported JSON value.')
    return '{' + ','.join(exact(key) + ':' + exact(value[key]) for key in sorted(value)) + '}'


def canonical(value): return exact(value).encode('utf-8')
def sha(raw): return hashlib.sha256(raw).hexdigest()
def same(left, right): return canonical(left) == canonical(right)
def digest(value): return isinstance(value, str) and HASH.fullmatch(value) is not None
def text(value, bound=256): return isinstance(value, str) and 0 < len(value) <= bound and not any(ord(char) < 32 for char in value)
def numeric(value): return type(value) is int or type(value) is float and math.isfinite(value) or isinstance(value, Decimal) and value.is_finite()


def worker_cgroup_path(raw):
    path = '/system.slice/' + WORKER_UNIT
    require(isinstance(raw, str) and raw in ('0::' + path, '0::' + path + '\n'),
            'The browser worker lacks its single exact unified cgroup entry.')
    return path


def decode(raw):
    def pairs(values):
        result = {}
        for key, value in values:
            require(key not in result, 'A JSON object repeats a key.')
            result[key] = value
        return result
    return json.loads(raw, object_pairs_hook=pairs, parse_float=Decimal,
        parse_constant=lambda _: (_ for _ in ()).throw(ObservationError('A JSON number is nonfinite.')))


def instant(value):
    require(isinstance(value, str) and re.fullmatch(r'[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,7})?Z', value),
            'A public UTC timestamp has an unsupported shape.')
    try:
        base = dt.datetime.fromisoformat(value[:19] + '+00:00')
        delta = base - dt.datetime(1970, 1, 1, tzinfo=dt.timezone.utc)
        fraction = value[20:-1] if len(value) > 20 else ''
        return (delta.days * 86400 + delta.seconds) * 10000000 + int(fraction.ljust(7, '0'))
    except ValueError as error: raise ObservationError('A public UTC timestamp has invalid calendar fields.') from error


def utc_now(): return dt.datetime.now(dt.timezone.utc).isoformat().replace('+00:00', 'Z')
def identity(info): return (info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode, info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def protected(path, expected=None, limit=MAX_JSON, modes=(0o600,)):
    require(path.is_absolute() and '..' not in path.parts, 'An input path is not canonical.')
    for parent in reversed(path.parents):
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                'An input ancestor is not root protected.')
    before = path.lstat()
    require(stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and stat.S_IMODE(before.st_mode) in modes and
            before.st_nlink == 1 and 0 <= before.st_size <= limit, 'An input is not a bounded protected regular file.')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as stream:
        require(identity(os.fstat(stream.fileno())) == identity(before), 'An input changed before reading.')
        raw = stream.read(limit + 1)
        require(identity(os.fstat(stream.fileno())) == identity(before), 'An input changed during reading.')
    require(identity(path.lstat()) == identity(before) and len(raw) == before.st_size and (expected is None or sha(raw) == expected),
            'An input digest or identity changed.')
    return raw


def read_record(descriptor, limit=MAX_JSON):
    require(isinstance(descriptor, dict) and set(descriptor) == {'path', 'sha256'} and digest(descriptor['sha256']), 'Invalid descriptor.')
    return decode(protected(Path(descriptor['path']), descriptor['sha256'], limit))


def load_operator():
    raw = protected(OPERATOR, OPERATOR_SHA, 2 << 20)
    module = types.ModuleType('reference_ui_identity_reader')
    module.__file__ = str(OPERATOR)
    exec(compile(raw, str(OPERATOR), 'exec'), module.__dict__)
    # Deliberately do not call preconditions: it hashes original executable bytes.
    return module


def media_snapshot():
    result = {}
    for root in MEDIA_ROOTS:
        paths = [root, *sorted(root.rglob('*'))]
        require(len(paths) <= 512, 'A synthetic media tree exceeds its inventory bound.')
        rows = {}
        for path in paths:
            before = path.lstat()
            require(before.st_uid == before.st_gid == 0 and not stat.S_ISLNK(before.st_mode), 'A synthetic media owner or type changed.')
            row = {'identity': list(identity(before))}
            if stat.S_ISREG(before.st_mode):
                require(before.st_size <= 64 << 20 and stat.S_IMODE(before.st_mode) == 0o644, 'An owned synthetic media file exceeds its bound.')
                checksum = hashlib.sha256()
                with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as handle:
                    require(identity(os.fstat(handle.fileno())) == identity(before), 'Synthetic media changed while opening.')
                    for block in iter(lambda: handle.read(1 << 20), b''): checksum.update(block)
                    require(identity(os.fstat(handle.fileno())) == identity(before), 'Synthetic media changed while hashing.')
                row['sha256'] = checksum.hexdigest()
            else:
                require(stat.S_ISDIR(before.st_mode) and stat.S_IMODE(before.st_mode) == 0o755, 'A synthetic media directory changed.')
            require(identity(path.lstat()) == identity(before), 'A synthetic media pathname changed.')
            rows[str(path.relative_to(root))] = row
        result[str(root)] = rows
    return result


def edit_body(detail):
    require(isinstance(detail, dict) and detail.get('Id') == ITEM and detail.get('Type') == 'Movie' and
            detail.get('Path') == TARGET_PATH and detail.get('ParentId') == FOLDER and detail.get('IsFolder') is False and text(detail.get('Name')),
            'The metadata DTO is not the fresh exact Movie 100.')
    return {'Id': ITEM, **{key: copy.deepcopy(detail[key]) for key in EDIT_FIELDS if key in detail}}


def reserve_edit(detail):
    original = edit_body(detail)
    sorting = reversible_sorting(detail)
    marker = MARKER_NAME
    require(detail['Name'] != marker, 'The single fixed Name marker is already present.')
    forward = copy.deepcopy(original)
    forward['Name'] = marker
    require({key for key in set(original) | set(forward) if not same(original.get(key), forward.get(key))} == {'Name'},
            'A metadata reservation changed more than Name.')
    return {'detail': copy.deepcopy(detail), 'forward_body': forward, 'restore_body': original, 'sorting': sorting,
        'public': {'target_id': ITEM, 'library_id': LIBRARY, 'original_name': detail['Name'], 'marker_name': marker,
            'original_body_sha256': sha(canonical(original)), 'forward_body_sha256': sha(canonical(forward)),
            'restore_body_sha256': sha(canonical(original))}}


def reversible_sorting(detail):
    locks = detail.get('LockedFields')
    require(detail.get('LockData') is False and isinstance(locks, list) and len(locks) <= 32 and
            all(text(field, 80) for field in locks) and len(locks) == len(set(locks)) and not any(field.lower() == 'sortname' for field in locks) and
            isinstance(detail.get('Name'), str) and re.fullmatch('[A-Za-z0-9]+(?: [A-Za-z0-9]+)*', detail['Name']) and
            all(field in detail and detail[field] == detail['Name'] for field in SORT_FIELDS),
            'The current lock and sorting state has no reviewed exact Name-only round trip.')
    return {'mode': 'unlocked-follow-name', 'original_name': detail['Name'],
            'original_sorting': {field: detail[field] for field in SORT_FIELDS}}


def automatic_changes(before, after, start, end, permitted):
    changes = {}
    for key in AUTOMATIC_FIELDS:
        old, new = {'present': key in before, 'value': before.get(key)}, {'present': key in after, 'value': after.get(key)}
        if same(old, new): continue
        require(permitted, 'An automatic target field changed without a dispatched metadata intent.')
        for point in (old, new):
            if not point['present']: continue
            require(isinstance(point['value'], str), 'An automatic field changed type.')
            if key in ('Etag', 'ETag'):
                require(re.fullmatch('[A-Za-z0-9_-]{1,128}', point['value']), 'An ETag has an unsupported shape.')
            else:
                instant(point['value'])
        if new['present'] and key not in ('Etag', 'ETag'):
            require(instant(start) <= instant(new['value']) <= instant(end), 'An automatic timestamp is outside this owned operation.')
        changes[key] = {'before': old, 'after': new}
    return changes


def classify_metadata(original, current, reservation, start, end, dispatched):
    edit_body(current)
    changes = automatic_changes(original, current, start, end, dispatched)
    left, right = ({key: value for key, value in row.items() if key not in AUTOMATIC_FIELDS} for row in (original, current))
    if same(left, right): return 'original', changes
    expected = copy.deepcopy(left)
    expected['Name'] = reservation['public']['marker_name']
    require(reservation.get('sorting', {}).get('mode') == 'unlocked-follow-name', 'The metadata reservation omitted its reversible sorting contract.')
    for field in SORT_FIELDS:
        if field in expected: expected[field] = reservation['public']['marker_name']
    if same(right, expected): return 'mutated', changes
    return 'foreign-or-unknown', changes


def indexed_page(value, key='Id', bound=256):
    require(isinstance(value, dict) and isinstance(value.get('Items'), list) and len(value['Items']) <= bound and
            type(value.get('TotalRecordCount')) is int and value['TotalRecordCount'] == len(value['Items']), 'A public page is incomplete or oversized.')
    rows = {}
    for row in value['Items']:
        require(isinstance(row, dict) and text(row.get(key), 128) and row[key] not in rows, 'A public identity is missing or duplicated.')
        rows[row[key]] = row
    return rows


def validate_public_snapshot(value):
    require(isinstance(value, dict) and set(value) == {'marker', 'version', 'captured_at', 'server', 'roster', 'configuration', 'libraries',
            'catalog_by_library', 'items_by_user', 'preferences', 'details', 'devices'} and value['marker'] == SNAPSHOT_MARKER and
            type(value['version']) is int and value['version'] == 1, 'The complete public snapshot shape changed.')
    instant(value['captured_at'])
    require(value['server'].get('Id') == SERVER and value['server'].get('Version') == '4.9.5.0', 'The fresh public server is not the pinned reference.')
    users, libraries = value['roster'], value['libraries']
    require(isinstance(users, dict) and 2 <= len(users) <= 16 and ADMIN in users and VIEWER in users and
            all(USER_ID.fullmatch(key) and row.get('Id') == key and isinstance(row.get('Configuration'), dict) and
                isinstance(row.get('Policy'), dict) for key, row in users.items()), 'The complete user roster is invalid.')
    require(users[ADMIN].get('Name') == ADMIN_NAME and users[ADMIN]['Policy'].get('IsAdministrator') is True and
            users[VIEWER].get('Name') == VIEWER_NAME and users[VIEWER]['Policy'].get('IsAdministrator') is False and
            users[VIEWER]['Policy'].get('IsDisabled') is False, 'The fresh administrator or ordinary viewer differs.')
    require(isinstance(libraries, dict) and 1 <= len(libraries) <= 16 and LIBRARY in libraries and
            all(ITEM_ID.fullmatch(key) and row.get('ItemId') == key and not any(field in row for field in ('RefreshStatus', 'RefreshProgress'))
                for key, row in libraries.items()) and libraries[LIBRARY].get('Locations') == [str(MOVIES)] and
            libraries[LIBRARY].get('CollectionType') == 'movies', 'A library is unknown, active, or outside its fixed Movies path.')
    require(libraries[LIBRARY].get('LibraryOptions', {}).get('SaveLocalMetadata') is False and
            libraries[LIBRARY]['LibraryOptions'].get('MetadataSavers') == [], 'The reference may write metadata beside synthetic media.')
    require(set(value['catalog_by_library']) == set(libraries) and set(value['items_by_user']) == set(value['preferences']) == set(users),
            'The public witness omitted a current library, user projection or preference document.')
    members = value['catalog_by_library'][LIBRARY]
    require('98' not in members and '97' not in members and ITEM in members and ANCHOR in members and FOLDER in members,
            'The controlled catalog reused removed IDs or omitted its current target.')
    movies = {key: row for key, row in members.items() if row.get('Type') == 'Movie'}
    require(set(movies) == {ITEM, ANCHOR}, 'The Movies query is not the complete two-movie experiment.')
    detail = value['details']
    require(set(detail) == {'admin', 'viewer'} and set(detail['admin']) == {LIBRARY, FOLDER, ITEM, ANCHOR} and
            set(detail['viewer']) == {ITEM, ANCHOR}, 'The target and anchor details are incomplete.')
    for role in ('admin', 'viewer'):
        edit_body(detail[role][ITEM])
        anchor = detail[role][ANCHOR]
        require(anchor.get('Id') == ANCHOR and anchor.get('Type') == 'Movie' and anchor.get('Path') == ANCHOR_PATH and
                anchor.get('IsFolder') is False and text(anchor.get('Name')) and anchor['Name'] != detail[role][ITEM]['Name'],
                'The visible anchor is not its exact separate movie.')
    reversible_sorting(detail['admin'][ITEM])
    for family in ('catalog_by_library', 'items_by_user', 'details'):
        for rows in value[family].values():
            if ITEM in rows:
                require(rows[ITEM].get('Name') == detail['admin'][ITEM]['Name'] and
                        all(rows[ITEM][field] == detail['admin'][ITEM]['Name'] for field in SORT_FIELDS if field in rows[ITEM]),
                        'A target projection has an unreviewed sorting representation that cannot be restored exactly.')
    require(detail['admin'][FOLDER].get('Id') == FOLDER and detail['admin'][FOLDER].get('IsFolder') is True and
            detail['admin'][FOLDER].get('Path') == str(Path(TARGET_PATH).parent) and
            detail['admin'][LIBRARY].get('Id') == LIBRARY and detail['admin'][LIBRARY].get('Type') == 'CollectionFolder',
            'The fresh folder or library detail differs from the controlled hierarchy.')
    require(isinstance(value['devices'], dict) and len(value['devices']) <= 256 and
            all(row.get('Id') == key and text(row.get('ReportedDeviceId'), 256) for key, row in value['devices'].items()),
            'The current public device registry is incomplete.')
    return value


def compare_public(before, after, *, expected_name=None, metadata_sent=False, owned_devices=(), active_users=(), prior_closed_devices=()):
    validate_public_snapshot(before)
    validate_public_snapshot(after)
    require(instant(before['captured_at']) <= instant(after['captured_at']), 'The public capture clock moved backwards.')
    changes = []
    for key in ('server', 'configuration', 'libraries', 'preferences'):
        require(same(before[key], after[key]), 'A preserved public document changed: ' + key)
    require(set(before['roster']) == set(after['roster']), 'The complete roster changed membership.')
    for user_id, old in before['roster'].items():
        new = after['roster'][user_id]
        allowed = AUTH_TIME_FIELDS if user_id in active_users else frozenset()
        require(same({key: value for key, value in old.items() if key not in allowed},
                     {key: value for key, value in new.items() if key not in allowed}), 'An account configuration, policy or unrelated field changed.')
        for key in allowed:
            previous, current = {'present': key in old, 'value': old.get(key)}, {'present': key in new, 'value': new.get(key)}
            if same(previous, current): continue
            require(current['present'] and instant(before['captured_at']) <= instant(current['value']) <= instant(after['captured_at']),
                    'An owned account timestamp changed outside this capture interval.')
            changes.append({'kind': 'owned-authentication-time', 'user_id': user_id, 'field': key, 'before': previous, 'after': current})
    for family in ('catalog_by_library', 'items_by_user', 'details'):
        require(set(before[family]) == set(after[family]), 'A public projection family changed membership.')
        for group, rows in before[family].items():
            current = after[family][group]
            require(set(rows) == set(current), 'A visible catalog, target or anchor appeared or disappeared.')
            for item_id, old in rows.items():
                new = current[item_id]
                if item_id != ITEM:
                    require(same(old, new), 'An unrelated catalog or anchor DTO changed.')
                    continue
                expected = copy.deepcopy(old)
                if expected_name is not None:
                    expected['Name'] = expected_name
                    if expected_name != old['Name']:
                        require(metadata_sent, 'Derived sorting changed without an owned metadata dispatch.')
                        for field in SORT_FIELDS:
                            if field in expected: expected[field] = expected_name
                require(same({key: value for key, value in expected.items() if key not in AUTOMATIC_FIELDS},
                             {key: value for key, value in new.items() if key not in AUTOMATIC_FIELDS}),
                        'The target changed outside Name or its controlled automatic fields.')
                automatic = automatic_changes(old, new, before['captured_at'], after['captured_at'], metadata_sent)
                if automatic: changes.append({'kind': 'metadata-automatic', 'family': family, 'group': group, 'item_id': ITEM, 'fields': automatic})
                sorting = {field: {'before': old.get(field), 'after': new.get(field)} for field in SORT_FIELDS if field in old and old[field] != new.get(field)}
                if sorting: changes.append({'kind': 'name-derived-sorting', 'family': family, 'group': group, 'item_id': ITEM, 'fields': sorting})
    old_devices, new_devices = before['devices'], after['devices']
    require(set(old_devices) <= set(new_devices), 'An existing public device disappeared.')
    owned = {entry['device_id']: entry for entry in owned_devices}
    historical = {entry['client']['device_id']: entry for entry in prior_closed_devices}
    require(len(owned) == len(owned_devices), 'The two owned clients share a reported device identity.')
    for key, row in new_devices.items():
        match = owned.get(row['ReportedDeviceId'])
        if key in old_devices:
            if match is None:
                prior = historical.get(row['ReportedDeviceId'])
                if prior is not None:
                    require(same({name: value for name, value in row.items() if name != 'DateLastActivity'},
                                 {name: value for name, value in old_devices[key].items() if name != 'DateLastActivity'}) and
                            row.get('AppName') == prior['client']['client_name'] and row.get('LastUserId') == prior['client']['user_id'],
                            'A closed preflight device changed business fields.')
                    if row['DateLastActivity'] != old_devices[key].get('DateLastActivity'):
                        require(instant(prior['last_observed_at']) <= instant(row['DateLastActivity']) <= instant(prior['logout_completed_at']),
                                'The closed preflight device changed outside its receipted logout interval.')
                        changes.append({'kind': 'closed-preflight-device-time', 'id': key, 'before': old_devices[key], 'after': row,
                                        'logout_completed_at': prior['logout_completed_at']})
                    continue
                require(same(row, old_devices[key]), 'An old unowned device changed.')
                continue
            require(same({name: value for name, value in row.items() if name != 'DateLastActivity'},
                         {name: value for name, value in old_devices[key].items() if name != 'DateLastActivity'}), 'An owned device changed structural fields.')
        else:
            require(match is not None and all(previous['ReportedDeviceId'] != row['ReportedDeviceId'] for previous in old_devices.values()),
                    'A new device lacks one fresh observed owned client identity.')
        if match is not None:
            require(row.get('AppName') == match['client_name'] and row.get('AppVersion') == match['client_version'] and
                    row.get('Name') == match['device_name'] and row.get('LastUserId') == match['user_id'] and
                    row.get('LastUserName') == match['username'],
                    'An owned device is not bound to its actual account and client metadata.')
            if key not in old_devices or row['DateLastActivity'] != old_devices[key].get('DateLastActivity'):
                require(instant(before['captured_at']) <= instant(row['DateLastActivity']) <= instant(after['captured_at']),
                        'An owned device activity timestamp changed outside this operation.')
            if key not in old_devices or not same(row, old_devices[key]):
                changes.append({'kind': 'owned-authentication-device', 'id': key, 'before': old_devices.get(key), 'after': row})
    return {'passed': True, 'automatic_changes': changes, 'public_state_after_authentication': True, 'authentication_effects_present': True}


class Administrator:
    """Own one fresh recorder session; durable acknowledgement precedes use."""
    def __init__(self, run, password):
        self.run, self.password = run, password
        self.token = self.session_id = None
        self.owned = self.proven = self.closed = False
        self.acknowledged = False
        self.device = 'goby-reference-ui-' + run.mode + '-' + secrets.token_hex(16)
        self.client = {'device_id': self.device, 'client_name': 'Goby Reference UI Controller',
            'client_version': '1.0', 'device_name': 'Linux Reference UI Recorder', 'user_id': ADMIN, 'username': ADMIN_NAME}
        self.results = {}

    def login(self):
        result = self.run.request('admin-login', 'POST', '/emby/Users/AuthenticateByName',
            {'Username': ADMIN_NAME, 'Pw': self.password}, operation='admin-login')
        self.proven = bool(self.owned and self.acknowledged and result['complete'] is True and result['status'] == 200 and 'failure_type' not in result)
        self.results['login'] = result
        require(self.proven, 'The new administrator login is not durably owned and completely acknowledged.')
        self.run.save('admin-proven.json', {'user_id': ADMIN, 'session_id': self.session_id, 'token_sha256': sha(self.token.encode()),
            'client': self.client, 'acknowledgement': self.run.records['admin-acknowledgement-private.json'],
            'status': result['status'], 'complete': result['complete'], 'body_sha256': result['body_sha256']})

    def receive(self, result):
        body = result.get('body')
        require(isinstance(body, dict), 'The fresh administrator response has no complete JSON identity.')
        token = body.get('AccessToken')
        if text(token, 8192):
            self.token = token
            user, session = body.get('User', {}), body.get('SessionInfo', {})
            self.owned = body.get('ServerId') == SERVER and user.get('Id') == ADMIN and user.get('Name') == ADMIN_NAME and \
                user.get('Policy', {}).get('IsAdministrator') is True and session.get('UserId') == ADMIN and \
                session.get('DeviceId') == self.device and text(session.get('Id'), 128)
            self.session_id = session.get('Id') if self.owned else None
            self.run.save('admin-acknowledgement-private.json', {'marker': MARKER, 'version': 1,
                'response': body, 'token_sha256': sha(token.encode()), 'transport_complete': result['complete'],
                'status': result['status'], 'owned_identity': bool(self.owned), 'normal_use_authorized': False})
            self.acknowledged = True

    def logout(self):
        if self.token is None: return
        require(self.owned, 'A received administrator token has no safe cleanup ownership.')
        self.run.cleanup_deadline = min(self.run.hard_deadline, max(self.run.cleanup_deadline or 0, time.monotonic() + 30))
        try:
            self.results['logout'] = self.run.request('admin-logout', 'POST', '/emby/Sessions/Logout', operation='admin-logout', cleanup=True)
        finally:
            self.results['exact401'] = self.run.request('admin-exact401', 'GET', '/emby/System/Info', cleanup=True)
        self.closed = all(self.results.get(key, {}).get('complete') is True and self.results[key].get('status') == status
                          for key, status in (('logout', 204), ('exact401', 401)))
        require(self.closed, 'The exact administrator logout204 and same-token401 were not both completed.')

    def public(self):
        result = {'user_id': ADMIN, 'session_id': self.session_id, 'token_sha256': sha(self.token.encode()) if self.token else None, 'closed': self.closed}
        for name in ('login', 'logout', 'exact401'):
            result[name + '_status'] = self.results.get(name, {}).get('status')
            result[name + '_complete'] = self.results.get(name, {}).get('complete') is True
        return result


class Run:
    def __init__(self, args):
        self.args, self.mode = args, args.mode
        self.root = PREFLIGHT_ROOT if self.mode == 'preflight' else ROOT
        self.root_fd = self.lock = None
        self.op = self.owner = self.admin = self.before = self.after = self.preflight = None
        self.input = self.input_sha = self.child = self.terminal = self.browser = self.reservation = None
        self.forward_sent = self.restore_sent = self.forward_ack = self.restore_ack = False
        self.restoration = 'not_required'
        self.stages, self.controls, self.errors, self.records = [], [], [], {}
        self.reserved, self.dispatched, self.labels, self.source_pins = [], [], set(), {}
        self.responses = {}
        self.http_requests = self.bytes_received = self.viewer_api_gets = 0
        self.work_requests = self.cleanup_requests = self.work_received = self.cleanup_received = 0
        self.started_at = utc_now()
        started_clock = time.monotonic()
        self.work_deadline = started_clock + 780
        self.hard_deadline = started_clock + TOTAL_SECONDS
        self.cleanup_deadline = None
        self.phase = 'load'
        self.js_sources = {}
        self.viewer_token = None
        self.closed = False

    def error(self, phase, error):
        value = {'stage': phase, 'failure_type': type(error).__name__}
        if type(error) is ObservationError: value['reason'] = str(error)
        self.errors.append(value)

    def save(self, name, value, ipc=False):
        require(self.root_fd is not None and re.fullmatch('[a-z][a-z0-9-]*\.json', name), 'An output escaped the owned root.')
        raw = canonical(value) + b'\n'
        require(len(raw) <= (MAX_IPC if ipc else MAX_JSON), 'An evidence document exceeds its fixed byte budget.')
        temporary = name + '.pending' if ipc else name
        descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=self.root_fd)
        with os.fdopen(descriptor, 'wb') as output:
            output.write(raw); output.flush(); os.fsync(output.fileno())
        if ipc:
            os.link(temporary, name, src_dir_fd=self.root_fd, dst_dir_fd=self.root_fd, follow_symlinks=False)
            os.fsync(self.root_fd)
            os.unlink(temporary, dir_fd=self.root_fd)
        os.fsync(self.root_fd)
        record = {'path': str(self.root / name), 'sha256': sha(raw)}
        self.records[name] = record
        return record

    def check(self, cleanup=False):
        limit = min(self.cleanup_deadline or self.hard_deadline, self.hard_deadline) if cleanup else self.work_deadline
        require(time.monotonic() < limit, 'The bounded operation deadline expired.')
        for path, digest_value in self.source_pins.items(): protected(Path(path), digest_value, modes=(0o600, 0o644))
        self.op.same_service(self.owner)
        require(os.readlink('/proc/self/ns/net') == os.readlink('/proc/1/ns/net'), 'The new controller left the host proxy namespace.')
        proxy = self.op.process_identity(334022)
        require(proxy['startTicks'] == '378464' and proxy['bootId'] == BOOT and proxy['networkNamespace'] == os.readlink('/proc/1/ns/net'),
                'The fixed existing reference proxy process changed.')
        if self.root_fd is not None:
            named, opened = self.root.lstat(), os.fstat(self.root_fd)
            require((named.st_dev, named.st_ino) == (opened.st_dev, opened.st_ino) and
                    stat.S_ISDIR(named.st_mode) and named.st_uid == named.st_gid == 0 and stat.S_IMODE(named.st_mode) == 0o700,
                    'The owned output root changed identity.')

    def allowed_get(self, route, viewer=False, cleanup=False):
        parsed = urlsplit(route)
        if parsed.scheme or parsed.netloc or parsed.fragment: return False
        query = parse_qsl(parsed.query, keep_blank_values=True)
        if len(query) != len({key.lower() for key, _ in query}) or any(re.search('token|password|secret|authorization|api.?key', key, re.I) for key, _ in query): return False
        path = parsed.path
        if cleanup:
            allowed = ('/emby/Users/' + (VIEWER if viewer else ADMIN) + '/Items/' + ITEM,)
            return not query and (path in allowed or not viewer and path == '/emby/System/Info')
        if viewer: return path in ('/emby/Users/' + VIEWER, '/emby/Users/' + VIEWER + '/Items/' + ITEM, '/emby/Users/' + VIEWER + '/Items/' + ANCHOR) and not query
        if path in ('/emby/System/Info/Public', '/emby/Users', '/emby/System/Configuration', '/emby/Library/VirtualFolders/Query', '/emby/Devices'):
            return not query
        roster = set(self.before['roster']) if self.before else {ADMIN, VIEWER} | getattr(self, 'roster_ids', set())
        for user_id in roster:
            if path in ('/emby/Users/' + user_id, '/emby/UserSettings/' + user_id): return not query
            if path == '/emby/Users/' + user_id + '/Items':
                values = dict(query)
                return set(values) == {'Recursive', 'Fields', 'EnableUserData', 'EnableTotalRecordCount', 'Limit', 'ParentId'} and \
                    values['ParentId'] in getattr(self, 'library_ids', set()) and values['Recursive'] == 'true' and values['Fields'] == FIELDS and \
                    values['EnableUserData'] == values['EnableTotalRecordCount'] == 'true' and values['Limit'] == '256' or \
                    set(values) == {'Recursive', 'Fields', 'EnableUserData', 'EnableTotalRecordCount', 'Limit', 'Ids'} and \
                    values['Recursive'] == 'true' and values['Fields'] == FIELDS and values['EnableUserData'] == values['EnableTotalRecordCount'] == 'true' and \
                    values['Limit'] == '256' and set(values['Ids'].split(',')) <= getattr(self, 'catalog_ids', set())
            if path in tuple('/emby/Users/' + user_id + '/Items/' + item for item in (LIBRARY, FOLDER, ITEM, ANCHOR)):
                return not query
        return False

    def request(self, label, method, route, body=None, operation=None, viewer=False, cleanup=False):
        self.check(cleanup)
        require(re.fullmatch('[a-z][a-z0-9-]{0,95}', label) and label not in self.labels and self.http_requests < MAX_HTTP and
                (self.cleanup_requests < CLEANUP_HTTP and self.cleanup_received < CLEANUP_BYTES if cleanup else
                 self.work_requests < WORK_HTTP and self.work_received < WORK_BYTES),
                'A physical request was repeated or exceeded its budget.')
        if method == 'GET':
            require(body is None and operation is None and self.allowed_get(route, viewer, cleanup), 'An API read escaped its exact public scope.')
            if cleanup:
                if route == '/emby/System/Info':
                    require(label == 'admin-exact401' and 'admin-logout' in self.dispatched, 'The unique administrator rejection proof must follow its logout.')
                else:
                    require(self.mode == 'observe' and self.reservation is not None and 'forward' in self.dispatched and label in
                            ('restore-fresh-admin', 'restore-admin-readback', 'restore-viewer-readback', 'restore-reconcile'),
                            'A cleanup read lacks a pending owned metadata operation.')
        else:
            require(method == 'POST' and not viewer and operation in ('admin-login', 'admin-logout', 'forward', 'restore') and
                    operation not in self.reserved, 'An unowned or repeated public write was requested.')
            if operation in ('forward', 'restore'):
                require(self.mode == 'observe' and self.reservation is not None and route == '/emby/Items/' + ITEM and
                        (not cleanup or operation == 'restore') and
                        same(body, self.reservation['forward_body' if operation == 'forward' else 'restore_body']), 'A metadata write differs from its exact Name reservation.')
            else:
                require(route == ('/emby/Users/AuthenticateByName' if operation == 'admin-login' else '/emby/Sessions/Logout'), 'An authentication route changed.')
                require(not cleanup or operation == 'admin-logout', 'A fresh login cannot consume protected cleanup capacity.')
            self.reserved.append(operation)
        require(operation == 'admin-login' or self.admin is not None and (self.admin.owned if cleanup else self.admin.proven), 'The request lacks an owned administrator.')
        require(not viewer or self.viewer_token is not None, 'A viewer readback lacks its observed UI credential.')
        self.labels.add(label)
        payload = None if body is None else canonical(body)
        self.save(label + '-intent.json', {'channel': 'controller_api', 'method': method, 'path': route, 'operation': operation,
            'body_sha256': sha(payload) if payload is not None else None, 'body_bytes': len(payload) if payload else 0,
            'body': body if operation in ('forward', 'restore') else None, 'one_shot': True})
        client = self.browser['proof'] if viewer else self.admin.client
        require(all(text(client.get(key), 256) and '"' not in client[key] and '\\' not in client[key]
                    for key in ('client_name', 'device_name', 'device_id', 'client_version')), 'A client metadata value cannot be quoted safely.')
        headers = {'Accept': 'application/json', 'Accept-Encoding': 'identity', 'Connection': 'close',
            'Authorization': 'Emby Client="' + client['client_name'] + '", Device="' + client['device_name'] +
                '", DeviceId="' + client['device_id'] + '", Version="' + client['client_version'] + '"'}
        token = self.viewer_token if viewer else self.admin.token
        if token is not None and operation != 'admin-login': headers['X-Emby-Token'] = token
        if payload is not None: headers['Content-Type'] = 'application/json'
        result = {'channel': 'controller_api', 'status': None, 'complete': False, 'body': None, 'body_bytes': 0,
                  'body_sha256': sha(b''), 'token_sha256': sha(token.encode()) if token else None, 'operation': operation}
        connection = None
        previous = signal.getsignal(signal.SIGALRM)
        signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(ObservationError('A public HTTP exchange exceeded its deadline.')))
        signal.setitimer(signal.ITIMER_REAL, 12)
        self.http_requests += 1
        if cleanup: self.cleanup_requests += 1
        else: self.work_requests += 1
        if viewer: self.viewer_api_gets += 1
        try:
            connection = http.client.HTTPConnection('127.0.0.1', 18197, timeout=8)
            if operation is not None: self.dispatched.append(operation)
            connection.request(method, route, payload, headers)
            response = connection.getresponse()
            result['status'] = response.status
            require(not 300 <= response.status < 400, 'Public redirects are not accepted.')
            length = response.getheader('Content-Length')
            remaining = (CLEANUP_BYTES - self.cleanup_received) if cleanup else (WORK_BYTES - self.work_received)
            maximum = min(2 << 20, remaining)
            require(length is None or length.isdigit() and int(length) <= maximum, 'An HTTP length is invalid or oversized.')
            raw = response.read(maximum + 1)
            self.bytes_received += len(raw)
            if cleanup: self.cleanup_received += len(raw)
            else: self.work_received += len(raw)
            require(len(raw) <= maximum and self.bytes_received <= MAX_TOTAL and (length is None or int(length) == len(raw)), 'An HTTP body is incomplete or oversized.')
            result.update(body_bytes=len(raw), body_sha256=sha(raw), complete=True,
                body=decode(raw) if raw and (response.getheader('Content-Type') or '').lower().split(';')[0] == 'application/json' else raw.decode('utf-8') if raw else None,
                completed_at=utc_now())
            if operation == 'admin-login': self.admin.receive(result)
        except Exception as error:
            result['failure_type'] = type(error).__name__
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0); signal.signal(signal.SIGALRM, previous)
            if connection is not None: connection.close()
            self.responses[label] = {key: result.get(key) for key in ('status', 'complete', 'completed_at', 'operation', 'body_sha256', 'body_bytes', 'token_sha256')}
            journal = copy.deepcopy(result)
            if operation == 'admin-login': journal['body'] = None
            self.save(label + '-result.json', journal)
        return result

    def get(self, label, route, viewer=False, cleanup=False):
        result = self.request(label, 'GET', route, viewer=viewer, cleanup=cleanup)
        require(result['complete'] is True and result['status'] == 200, 'A required fresh public read did not complete with HTTP 200.')
        return result['body']

    def snapshot(self, label):
        server = self.get(label + '-server', '/emby/System/Info/Public')
        roster_rows = self.get(label + '-roster', '/emby/Users')
        require(isinstance(roster_rows, list) and 2 <= len(roster_rows) <= 16 and
                all(isinstance(row, dict) and isinstance(row.get('Id'), str) and USER_ID.fullmatch(row['Id']) for row in roster_rows),
                'The complete current roster is unavailable.')
        roster = {row['Id']: row for row in roster_rows}
        require(len(roster) == len(roster_rows), 'The current roster repeats an ID.')
        self.roster_ids = set(roster)
        libraries = indexed_page(self.get(label + '-libraries', '/emby/Library/VirtualFolders/Query'), 'ItemId', 16)
        require(all(ITEM_ID.fullmatch(key) for key in libraries), 'A current library ID is not canonical.')
        self.library_ids = set(libraries)
        catalogs, all_ids = {}, set()
        query_base = {'Recursive': 'true', 'Fields': FIELDS, 'EnableUserData': 'true', 'EnableTotalRecordCount': 'true', 'Limit': '256'}
        for index, library_id in enumerate(sorted(libraries)):
            catalogs[library_id] = indexed_page(self.get(label + '-catalog-' + str(index), '/emby/Users/' + ADMIN + '/Items?' +
                urlencode(dict(query_base, ParentId=library_id))))
            require(all(ITEM_ID.fullmatch(key) for key in catalogs[library_id]), 'A current catalog item ID is not canonical.')
            all_ids.update(catalogs[library_id])
        require(0 < len(all_ids) <= 256, 'The current complete public catalog exceeds its bound.')
        self.catalog_ids = all_ids
        projections, preferences = {}, {}
        for index, user_id in enumerate(sorted(roster)):
            projections[user_id] = indexed_page(self.get(label + '-visible-' + str(index), '/emby/Users/' + user_id + '/Items?' +
                urlencode(dict(query_base, Ids=','.join(sorted(all_ids))))))
            require(set(projections[user_id]) <= all_ids, 'A user projection returned an unrequested identity.')
            preferences[user_id] = self.get(label + '-preferences-' + str(index), '/emby/UserSettings/' + user_id)
        details = {role: {item: self.get(label + '-' + role + '-' + item, '/emby/Users/' + user + '/Items/' + item)
            for item in ((LIBRARY, FOLDER, ITEM, ANCHOR) if role == 'admin' else (ITEM, ANCHOR))}
            for role, user in (('admin', ADMIN), ('viewer', VIEWER))}
        devices = self.get(label + '-devices', '/emby/Devices')
        require(isinstance(devices, dict) and isinstance(devices.get('Items'), list) and len(devices['Items']) <= 256, 'The public device list is invalid.')
        device_rows = {row['Id']: row for row in devices['Items']}
        require(len(device_rows) == len(devices['Items']), 'The public device registry repeats an ID.')
        configuration = self.get(label + '-configuration', '/emby/System/Configuration')
        value = {'marker': SNAPSHOT_MARKER, 'version': 1, 'captured_at': utc_now(), 'server': server, 'roster': roster,
            'configuration': configuration, 'libraries': libraries,
            'catalog_by_library': catalogs, 'items_by_user': projections, 'preferences': preferences, 'details': details, 'devices': device_rows}
        validate_public_snapshot(value)
        self.save(label + '-public.json', value)
        return value

    def properties(self, unit):
        require(unit in (WORKER_UNIT, CONTROLLER_UNIT, PREFLIGHT_UNIT), 'Another observer unit was requested.')
        fields = 'Id,LoadState,ActiveState,SubState,MainPID,ExecMainCode,ExecMainStatus,Result,ControlGroup,InvocationID,Transient,User,Group,WorkingDirectory,Restart'
        result = subprocess.run(['/usr/bin/systemctl', 'show', unit, '--no-pager', '--property=' + fields],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=10, check=False, env=ENV)
        require(result.returncode in (0, 1, 4) and len(result.stdout) <= 16384, 'An observer service state is unavailable.')
        return dict(line.split('=', 1) for line in result.stdout.decode().splitlines() if '=' in line)

    def load(self):
        require(sys.platform == 'linux' and os.getuid() == os.geteuid() == os.getgid() == os.getegid() == 0 and
                os.readlink('/proc/self/ns/net') == os.readlink('/proc/1/ns/net'), 'Run only as root in the existing host proxy namespace.')
        script = TOOL / 'observe-client-library-changed-reference-ui.py'
        require(Path(__file__).absolute() == script, 'The controller is outside its fresh fixed tool directory.')
        protected(script, self.args.script_sha256, 2 << 20)
        self.op = load_operator()
        self.owner = decode(protected(OWNER, OWNER_SHA))
        self.op.validate_owner(self.owner)
        service = self.owner['serviceIdentity']
        require(self.owner.get('phase') == 'ready' and self.owner.get('serverId') == SERVER and service.get('pid') == PID and
                service.get('startTicks') == TICKS and service.get('bootId') == BOOT and service.get('networkNamespace') == NAMESPACE and
                service.get('invocationId') == REFERENCE_INVOCATION, 'The reference owner is another lifetime or server.')
        self.op.same_service(self.owner)
        require(self.op.verify_media() == self.owner['media'], 'The original synthetic media fixture changed.')
        accounts = decode(protected(ACCOUNTS, ACCOUNTS_SHA))
        administrator = accounts.get('accounts', {}).get('admin', {})
        require(administrator.get('userId') == ADMIN and administrator.get('username') == ADMIN_NAME and text(administrator.get('password'), 1024),
                'The frozen credential source names another administrator.')
        viewer = decode(protected(VIEWER_CREDENTIALS, VIEWER_CREDENTIALS_SHA))
        require(set(viewer) == {'devices', 'marker', 'password', 'userId', 'username'} and
                viewer['marker'] == 'goby-reference-library-changed-continuation-v1' and viewer['userId'] == VIEWER and
                viewer['username'] == VIEWER_NAME and text(viewer['password'], 1024), 'The viewer credential is not the acknowledged continuation account.')
        self.viewer_credentials = {'viewer': {key: viewer[key] for key in ('username', 'password', 'userId')}}
        self.source_pins = {str(script): self.args.script_sha256, str(OPERATOR): OPERATOR_SHA, str(OWNER): OWNER_SHA,
            str(ACCOUNTS): ACCOUNTS_SHA, str(VIEWER_CREDENTIALS): VIEWER_CREDENTIALS_SHA, str(PROXY_STATUS): PROXY_STATUS_SHA}
        if self.mode == 'observe':
            source = decode(protected(self.args.source_closure, self.args.source_closure_sha256))
            require(isinstance(source, dict) and set(source) == {'marker', 'files'} and
                    source['marker'] == 'goby-reference-library-changed-sources-v1' and
                    set(source['files']) == {str(TOOL / name) for name in JS_NAMES} and all(digest(value) for value in source['files'].values()),
                    'The browser has another exact reviewed source closure.')
            self.js_sources = source['files']
            for path, expected in self.js_sources.items(): protected(Path(path), expected, 2 << 20)
            protected(self.args.node, self.args.node_sha256, 512 << 20, modes=(0o755,))
            self.source_pins.update(self.js_sources)
            self.source_pins[str(self.args.source_closure)] = self.args.source_closure_sha256
            self.preflight = read_record({'path': str(PREFLIGHT_ROOT / 'report.json'), 'sha256': self.args.preflight_sha256})
            validate_preflight(self.preflight, self.args.script_sha256, service)
            self.preflight_snapshot = read_record(self.preflight['public_snapshot'])
            validate_public_snapshot(self.preflight_snapshot)
            self.source_pins[str(PREFLIGHT_ROOT / 'report.json')] = self.args.preflight_sha256
            self.source_pins[self.preflight['public_snapshot']['path']] = self.preflight['public_snapshot']['sha256']
        self.check()
        self.lock = os.open(self.op.LOCK, os.O_RDONLY | os.O_NOFOLLOW)
        require(identity(os.fstat(self.lock)) == identity(self.op.canonical(self.op.LOCK, mode=0o600)), 'The existing reference lock changed.')
        fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        require(not os.path.lexists(self.root), 'The fresh evidence scope already exists; adoption or replay is forbidden.')
        unit = PREFLIGHT_UNIT if self.mode == 'preflight' else CONTROLLER_UNIT
        props = self.properties(unit)
        require(props.get('MainPID') == str(os.getpid()) and props.get('ActiveState') == 'active' and
                props.get('SubState') == 'running' and props.get('InvocationID') == os.environ.get('INVOCATION_ID') and
                re.fullmatch('[0-9a-f]{32}', props.get('InvocationID', '')), 'The controller lacks its exact independent live service lifetime.')
        own = self.op.process_identity(os.getpid())
        self.controller = {'pid': own['pid'], 'start_ticks': own['startTicks'], 'boot_id': own['bootId'],
                           'unit': unit, 'invocation_id': props['InvocationID']}
        self.root.mkdir(mode=0o700)
        self.root_fd = os.open(self.root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        self.admin = Administrator(self, administrator['password'])
        self.media = media_snapshot()
        self.save('owner.json', {'marker': MARKER, 'version': 1, 'mode': self.mode, 'path': str(self.root),
            'controller': self.controller, 'reference': self.reference(), 'source_pins': self.source_pins, 'created_at': self.started_at})
        self.save('media-before.json', self.media)
        self.admin.login()
        self.before = self.snapshot('before')
        preview = reserve_edit(self.before['details']['admin'][ITEM])
        require(preview['public']['marker_name'] != self.before['details']['admin'][ANCHOR]['Name'], 'The one marker would collide with the visible anchor.')
        require(all(row['ReportedDeviceId'] != self.admin.device for row in getattr(self, 'preflight_snapshot', {}).get('devices', {}).values()),
                'The new administrator reused a previously listed device identity.')
        if self.mode == 'observe':
            comparison = compare_public(self.preflight_snapshot, self.before, owned_devices=[self.admin.client], active_users=[ADMIN],
                                        prior_closed_devices=[self.preflight['ledger']['closed_device']])
            self.save('preflight-business-comparison.json', comparison)
            require(same(self.preflight['target'], self.target()) and same(self.preflight['anchor'], self.anchor()),
                    'The current target or anchor changed after the independent preflight.')

    def reference(self):
        return {'server_id': SERVER, 'base_url': BASE_URL, 'service_identity': self.owner['serviceIdentity']}

    def target(self):
        value = self.before['details']['admin'][ITEM]
        return {'id': ITEM, 'library_id': LIBRARY, 'parent_id': FOLDER, 'name': value['Name'], 'path': TARGET_PATH, 'type': 'Movie'}

    def anchor(self):
        return {'id': ANCHOR, 'name': self.before['details']['admin'][ANCHOR]['Name'], 'path': ANCHOR_PATH, 'type': 'Movie'}

    def expected_libraries(self):
        return sorted([{'id': key, 'name': row['Name']} for key, row in self.before['libraries'].items()], key=lambda row: row['id'])

    def finish_preflight(self):
        self.after = self.snapshot('after')
        self.preservation = compare_public(self.before, self.after, owned_devices=[self.admin.client], active_users=[ADMIN])
        self.save('public-preservation.json', self.preservation)
        require(same(media_snapshot(), self.media), 'Business-read-only preflight changed synthetic media.')

    def prepare_browser(self):
        self.reservation = reserve_edit(self.before['details']['admin'][ITEM])
        require(self.reservation['public']['marker_name'] != self.anchor()['name'], 'The marker would collide with the anchor title.')
        self.save('reservation-private.json', self.reservation)
        credentials = self.save('viewer-credentials.json', self.viewer_credentials)
        self.input = {'marker': INPUT_MARKER, 'version': 1, 'mode': MODE, 'root': str(ROOT), 'output': str(ROOT / 'browser'),
            'actor': {'slot': 'B', 'user_id': VIEWER, 'username': VIEWER_NAME, 'credentials': credentials, 'account_key': 'viewer'},
            'reference': self.reference(), 'expected_libraries': self.expected_libraries(), 'target': self.target(), 'anchor': self.anchor(),
            'source_closure': self.js_sources, 'controller': self.controller,
            'authority': {'owner': {'path': str(OWNER), 'sha256': OWNER_SHA},
                'preflight': {'path': str(PREFLIGHT_ROOT / 'report.json'), 'sha256': self.args.preflight_sha256},
                'before_snapshot': self.records['before-public.json']}}
        self.input_sha = self.save('input.json', self.input)['sha256']
        require(self.properties(WORKER_UNIT).get('LoadState') == 'not-found' and
                not Path('/sys/fs/cgroup/system.slice/' + WORKER_UNIT).exists(), 'The fresh browser worker already exists.')
        arguments = ['/usr/bin/systemd-run', '--unit=' + WORKER_UNIT, '--service-type=exec', '--quiet',
            '--property=User=root', '--property=Group=root', '--property=WorkingDirectory=' + str(ROOT), '--property=Restart=no',
            '--property=RemainAfterExit=yes', '--property=RuntimeMaxSec=900', '--property=TimeoutStopSec=15', '--property=KillMode=control-group',
            '--property=UMask=0077', '--property=PrivateTmp=yes', '--property=NoNewPrivileges=yes', '--property=ProtectSystem=strict',
            '--property=ReadWritePaths=' + str(ROOT), '--property=CPUQuota=150%', '--property=MemoryMax=1G', '--property=TasksMax=128',
            '--property=IPAddressDeny=any', '--property=IPAddressAllow=localhost', '--property=UnsetEnvironment=DEBUG PWDEBUG NODE_OPTIONS NODE_PATH',
            '--setenv=HOME=/root', '--setenv=LANG=C.UTF-8', '--property=StandardOutput=append:' + str(ROOT / 'node.stdout'),
            '--property=StandardError=append:' + str(ROOT / 'node.stderr'), '--', str(self.args.node),
            str(TOOL / JS_NAMES[0]), '--input', str(ROOT / 'input.json'), '--input-sha256', self.input_sha, '--output', str(ROOT / 'browser')]
        self.save('browser-launch-intent.json', {'unit': WORKER_UNIT, 'arguments': arguments, 'input_sha256': self.input_sha})
        response = subprocess.run(arguments, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15, check=False, env=ENV)
        self.save('browser-launch-result.json', {'returncode': response.returncode, 'stdout_sha256': sha(response.stdout), 'stderr_sha256': sha(response.stderr)})
        require(response.returncode == 0, 'The one browser worker launch failed; retry is forbidden.')
        until = min(self.work_deadline, time.monotonic() + 15)
        while time.monotonic() < until:
            props = self.properties(WORKER_UNIT)
            if int(props.get('MainPID', '0')) > 1:
                current = self.op.process_identity(int(props['MainPID']))
                require(current['uid'] == 0 and current['bootId'] == BOOT and current['exe'] == str(self.args.node) and
                        current['cmdline'] == arguments[arguments.index('--') + 1:] and
                        current['networkNamespace'] == os.readlink('/proc/1/ns/net'), 'The browser worker has another process, command or namespace.')
                expected_exe, running_exe = self.args.node.stat(), (Path('/proc') / str(current['pid']) / 'exe').stat()
                require((expected_exe.st_dev, expected_exe.st_ino) == (running_exe.st_dev, running_exe.st_ino), 'The running Node inode differs from its pinned executable.')
                self.child = {'pid': current['pid'], 'start_ticks': current['startTicks'], 'boot_id': current['bootId'], 'uid': 0, 'gid': 0,
                    'executable_path': current['exe'], 'executable_sha256': self.args.node_sha256, 'cgroup': worker_cgroup_path(current['cgroup'])}
                self.worker_invocation = props.get('InvocationID')
                require(isinstance(self.worker_invocation, str) and USER_ID.fullmatch(self.worker_invocation), 'The browser invocation is missing.')
                self.save('browser-process.json', {'node_process': self.child, 'invocation_id': self.worker_invocation})
                return
            require(props.get('ActiveState') not in ('failed', 'inactive'), 'The browser exited before its live identity was captured.')
            time.sleep(0.1)
        raise ObservationError('The browser live process did not appear within the one launch deadline.')

    def worker_live(self):
        require(self.child is not None, 'No browser worker was captured.')
        props = self.properties(WORKER_UNIT)
        current = self.op.process_identity(self.child['pid'])
        require(props.get('MainPID') == str(self.child['pid']) and props.get('InvocationID') == self.worker_invocation and
                props.get('ActiveState') == 'active' and props.get('SubState') == 'running' and
                current['startTicks'] == self.child['start_ticks'] and current['exe'] == self.child['executable_path'] and
                worker_cgroup_path(current['cgroup']) == self.child['cgroup'], 'The browser worker was replaced or restarted.')

    def ipc(self, name):
        require(re.fullmatch('(stage-(discovery|armed|restore-armed|restored)|abort)\.json', name), 'An IPC filename escaped the browser scope.')
        path = ROOT / 'browser' / name
        if not os.path.lexists(path): return None
        info = path.lstat()
        require(stat.S_ISREG(info.st_mode) and info.st_uid == info.st_gid == 0 and stat.S_IMODE(info.st_mode) == 0o600 and
                info.st_nlink in (1, 2) and info.st_size <= MAX_IPC, 'A browser publication has unsafe type, ownership or size.')
        pending = path.with_name(path.name + '.pending')
        if os.path.lexists(pending):
            other = pending.lstat()
            require(info.st_nlink == 2 and identity(info) == identity(other), 'The browser pending and final publications are unrelated.')
            self.publications = getattr(self, 'publications', {})
            started = self.publications.setdefault(name, time.monotonic())
            require(time.monotonic() - started <= 5, 'An IPC publication remained pending beyond its bound.')
            return None
        require(info.st_nlink == 1 and info.st_size > 0, 'A ready IPC file is incomplete.')
        raw = protected(path, limit=MAX_IPC)
        return {'path': str(path), 'sha256': sha(raw), 'value': decode(raw)}

    def receive_browser(self, descriptor):
        require(isinstance(descriptor, dict) and set(descriptor) == {'path', 'sha256'} and
                descriptor['path'] == str(ROOT / 'browser/session-private.json') and digest(descriptor['sha256']), 'The viewer private receipt escaped its fixed path.')
        value = read_record(descriptor, 32768)
        validate_private_session(value, self.input, self.input_sha, self.child, self.before)
        if self.browser is not None: require(same(self.browser, value), 'The immutable viewer private acknowledgement changed.')
        self.browser, self.viewer_token = value, value['token']
        self.source_pins[descriptor['path']] = descriptor['sha256']

    def stage(self, name, timeout):
        require(name == STAGES[len(self.stages)], 'A browser stage was reordered or replayed.')
        until = min(self.work_deadline, time.monotonic() + timeout)
        while time.monotonic() < until:
            self.check(); self.worker_live()
            aborted = self.ipc('abort.json')
            require(aborted is None, 'The original browser aborted its bounded observation.')
            record = self.ipc('stage-' + name + '.json')
            if record is not None:
                self.receive_browser(record['value'].get('session_private'))
                validate_stage(record['value'], self.input, self.input_sha, self.child, name,
                    self.controls[-1]['sha256'] if self.controls else None, self.browser['proof'],
                    self.stages[0]['value']['observation'] if self.stages else None, self.reservation['public'])
                if name in ('restore-armed', 'restored'):
                    window = record['value']['observation']['forward' if name == 'restore-armed' else 'restored']
                    earlier = self.stages[-1]['value']['observation']['boundary']
                    require(all(same(window['boundary'].get(key), value) for key, value in earlier.items()) and
                            window['control_sha256'] == self.controls[-1]['sha256'] and same(window['commit'], self.controls[-1]['value']['commit']),
                            'A completed window does not extend its own acknowledged armed boundary.')
                self.stages.append({'name': name, **record})
                self.save('accepted-stage-' + name + '.json', record)
                return record['value']['observation']
            time.sleep(0.1)
        raise ObservationError('The browser did not complete its full bounded stage.')

    def control(self, name, commit=None, restoration='pending'):
        require(name in CONTROLS and not any(value['name'] == name for value in self.controls), 'A controller control was replayed.')
        if name != 'close': require(name == CONTROLS[len(self.controls)], 'A control crossed its ordered stage barrier.')
        else: require(restoration in ('confirmed', 'not_required'), 'An unknown restoration cannot release a successful close.')
        value = {'marker': 'goby-reference-library-changed-control-v1', 'version': 1, 'name': name, 'input_sha256': self.input_sha,
            'source_closure_sha256': sha(canonical(self.js_sources)), 'controller': self.controller, 'node_process': self.child,
            'previous_stage_sha256': self.stages[-1]['sha256'] if self.stages else None,
            'reservation': self.reservation['public'] if self.reservation else None, 'commit': commit, 'restoration': restoration}
        record = self.save('control-' + name + '.json', value, ipc=True)
        self.controls.append({'name': name, **record, 'value': value})

    def detail_readback(self, label, viewer=False, cleanup=False):
        user = VIEWER if viewer else ADMIN
        return self.get(label, '/emby/Users/' + user + '/Items/' + ITEM, viewer=viewer, cleanup=cleanup)

    def metadata_post(self, restoring=False, cleanup=False):
        label = 'restore' if restoring else 'forward'
        require(not (self.restore_sent if restoring else self.forward_sent), 'A physical metadata write cannot be retried.')
        current = self.detail_readback(label + '-fresh-admin', cleanup=cleanup)
        state, changes = classify_metadata(self.reservation['detail'], current, self.reservation, self.before['captured_at'], utc_now(), self.forward_sent)
        self.save(label + '-classification.json', {'state': state, 'automatic_changes': changes, 'current': current})
        if restoring and state == 'original':
            require(same(edit_body(current), self.reservation['restore_body']), 'The apparent original differs in complete edit fields.')
            terminal_forward = self.responses.get('forward', {}).get('complete') is True
            self.restoration = 'not_required' if 'forward' not in self.dispatched else 'confirmed' if terminal_forward else 'restoration_required'
            self.save('restore-original-observation.json', {'original_observed': True, 'forward_dispatched': 'forward' in self.dispatched,
                'forward_response_complete': terminal_forward, 'outcome_unknown': self.restoration == 'restoration_required'})
            return None
        require(state == ('mutated' if restoring else 'original'), 'The current metadata is foreign or unknown; no overwrite is authorized.')
        require(same(media_snapshot(), self.media), 'Synthetic media changed before the exact metadata write.')
        if restoring: self.restore_sent = True
        else:
            self.forward_sent = True
            self.restoration = 'restoration_required'
        result = self.request(label, 'POST', '/emby/Items/' + ITEM, self.reservation['restore_body' if restoring else 'forward_body'],
                              operation=label, cleanup=cleanup)
        acknowledged = result['complete'] is True and result['status'] == 204 and result['body'] is None
        if restoring: self.restore_ack = acknowledged
        else: self.forward_ack = acknowledged
        require(acknowledged, 'The one metadata POST is not a complete HTTP 204; its physical request cannot be resent.')
        admin = self.detail_readback(label + '-admin-readback', cleanup=cleanup)
        viewer = self.detail_readback(label + '-viewer-readback', viewer=True, cleanup=cleanup)
        for role, row in (('admin', admin), ('viewer', viewer)):
            original = self.before['details'][role][ITEM]
            state, changed = classify_metadata(original, row, self.reservation, self.before['captured_at'], utc_now(), True)
            require(state == ('original' if restoring else 'mutated'), 'A complete admin or viewer API readback disagrees with the exact Name operation.')
            self.save(label + '-' + role + '-confirmed.json', {'detail': row, 'automatic_changes': changed})
        if restoring: self.restoration = 'confirmed'
        return {'write_completed_at': result['completed_at'], 'native_result_sha256': self.records[label + '-result.json']['sha256'],
            'admin_readback_sha256': self.records[label + '-admin-readback-result.json']['sha256'],
            'viewer_readback_sha256': self.records[label + '-viewer-readback-result.json']['sha256']}

    def execute_observation(self):
        self.phase = 'launch'; self.prepare_browser()
        self.phase = 'discovery'; self.stage('discovery', 90)
        identity = self.get('viewer-fresh-identity', '/emby/Users/' + VIEWER, viewer=True)
        require(identity.get('Id') == VIEWER and identity.get('Name') == VIEWER_NAME and identity.get('Policy', {}).get('IsAdministrator') is False and
                identity['Policy'].get('IsDisabled') is False, 'The actual UI token is not the expected ordinary viewer.')
        self.control('reserved')
        self.phase = 'armed'; self.stage('armed', 100)
        forward = self.metadata_post()
        self.control('forward', forward)
        self.phase = 'forward'; self.stage('restore-armed', 150)
        restored = self.metadata_post(True)
        require(restored is not None, 'The restored window has no actual owned restoration POST to observe.')
        self.control('restored', restored, 'confirmed')
        self.phase = 'restored'; self.stage('restored', 150)
        self.control('close', restoration='confirmed')

    def recover(self):
        self.cleanup_deadline = min(self.hard_deadline, time.monotonic() + 110)
        def reconcile_restore():
            try:
                current = self.detail_readback('restore-reconcile', cleanup=True)
                state, changes = classify_metadata(self.reservation['detail'], current, self.reservation, self.before['captured_at'], utc_now(), True)
                self.save('restore-reconciliation.json', {'state': state, 'automatic_changes': changes, 'restore_dispatched': 'restore' in self.dispatched,
                    'restore_response_complete': self.responses.get('restore', {}).get('complete') is True, 'second_restore_permitted': False})
                self.restoration = 'confirmed' if state == 'original' and same(edit_body(current), self.reservation['restore_body']) else 'restoration_required'
            except Exception as error: self.error('restore-reconciliation', error)
        if 'forward' not in self.dispatched: self.restoration = 'not_required'
        elif self.restore_sent:
            reconcile_restore()
        elif not self.restore_sent:
            try: self.metadata_post(True, cleanup=True)
            except Exception as error:
                self.error('exact-restoration', error)
                if self.restore_sent:
                    reconcile_restore()
        if self.child is not None:
            if 'abort.json' not in self.records:
                try:
                    self.save('abort.json', {'marker': 'goby-reference-library-changed-abort-v1', 'version': 1,
                        'input_sha256': self.input_sha, 'source_closure_sha256': sha(canonical(self.js_sources)), 'controller': self.controller,
                        'node_process': self.child, 'name': self.phase, 'failure': 'reference_controller_failed',
                        'previous_stage_sha256': self.stages[-1]['sha256'] if self.stages else None,
                        'previous_control_sha256': self.controls[-1]['sha256'] if self.controls else None,
                        'token_sha256': self.browser['proof']['token_sha256'] if self.browser else None,
                        'session_private': self.stages[-1]['value']['session_private'] if self.stages else None}, ipc=True)
                except Exception as error: self.error('abort-publication', error)
            if self.restoration in ('confirmed', 'not_required') and not any(value['name'] == 'close' for value in self.controls):
                try: self.control('close', restoration=self.restoration)
                except Exception as error: self.error('close-publication', error)

    def wait_browser(self):
        if self.child is None: return
        until = min(self.hard_deadline - 35, time.monotonic() + 100)
        cgroup = Path('/sys/fs/cgroup') / self.child['cgroup'].lstrip('/')
        while time.monotonic() < until:
            props = self.properties(WORKER_UNIT)
            require(props.get('InvocationID') == self.worker_invocation, 'The browser terminal belongs to another invocation.')
            if props.get('MainPID') == '0':
                files = list(cgroup.rglob('cgroup.procs')) if cgroup.exists() else []
                require(len(files) <= 64 and all(not path.read_text().strip() for path in files), 'The browser recursive cgroup is not empty.')
                require(props.get('ActiveState') in ('active', 'failed', 'inactive') and props.get('SubState') in ('exited', 'failed', 'dead'),
                        'The browser has no unambiguous terminal state.')
                self.terminal = {'properties': props, 'recursive_cgroup_empty': True, 'invocation_id': self.worker_invocation}
                self.save('browser-terminal.json', self.terminal)
                self.closed = True
                descriptor = {'path': str(ROOT / 'browser/report.json'), 'sha256': sha(protected(ROOT / 'browser/report.json', limit=MAX_REPORT))}
                self.browser_report = read_record(descriptor, MAX_REPORT)
                self.records['browser-report.json'] = descriptor
                if self.browser_report.get('session_private') is not None: self.receive_browser(self.browser_report['session_private'])
                validate_browser_report(self.browser_report, self.input, self.input_sha, self.child, self.browser, self.stages, self.controls)
                return
            time.sleep(0.2)
        raise ObservationError('The owned browser failed to reach a closed terminal within its cleanup budget.')

    def final_public_state(self):
        if self.before is None or self.admin is None or not self.admin.proven: return
        self.after = self.snapshot('after')
        devices = [self.admin.client]
        users = [ADMIN]
        if self.browser is not None:
            proof = self.browser['proof']
            devices.append({key: proof[key] for key in ('device_id', 'client_name', 'client_version', 'device_name', 'user_id')} | {'username': VIEWER_NAME})
            users.append(VIEWER)
        self.preservation = compare_public(self.before, self.after, expected_name=self.before['details']['admin'][ITEM]['Name'],
            metadata_sent='forward' in self.dispatched, owned_devices=devices, active_users=users)
        self.save('public-preservation.json', self.preservation)

    def report(self):
        preserved = getattr(self, 'preservation', {}).get('passed') is True
        admin = self.admin.public() if self.admin is not None else None
        ledger = {'http_requests': self.http_requests, 'bytes_received': self.bytes_received,
            'work_requests': self.work_requests, 'cleanup_requests': self.cleanup_requests,
            'work_bytes': self.work_received, 'cleanup_bytes': self.cleanup_received, 'viewer_api_gets': self.viewer_api_gets,
            'authentication_posts': sum(name in ('admin-login', 'admin-logout') for name in self.dispatched),
            'metadata_posts': sum(name in ('forward', 'restore') for name in self.dispatched)}
        if self.mode == 'preflight':
            ledger['closed_device'] = {'client': self.admin.client if self.admin else None,
                'last_observed_at': self.before['captured_at'] if self.before else None,
                'logout_completed_at': self.responses.get('admin-logout', {}).get('completed_at')}
            success = not self.errors and preserved and admin is not None and admin['closed']
            value = {'marker': PREFLIGHT_MARKER, 'version': 1, 'mode': 'business-read-only-preflight', 'root': str(self.root),
                'reference': self.reference(), 'script_sha256': self.args.script_sha256, 'source_closure_sha256': None,
                'public_snapshot': self.records.get('after-public.json'), 'target': self.target() if self.before else None,
                'anchor': self.anchor() if self.before else None, 'expected_libraries': self.expected_libraries() if self.before else None,
                'admin': admin, 'ledger': ledger, 'preservation': getattr(self, 'preservation', None), 'errors': self.errors,
                'status': 'passed' if success else 'failed', 'completed_at': utc_now(), 'evidence': dict(self.records)}
        else:
            observed = getattr(self, 'browser_report', None)
            success = (not self.errors and preserved and admin is not None and admin['closed'] and self.closed and
                self.restoration == 'confirmed' and observed is not None and observed.get('protocol_observation_complete') is True)
            outcomes = {name: observed.get(name) for name in ('forward', 'restored')} if observed else {}
            value = {'marker': MARKER, 'version': 1, 'mode': MODE, 'status': 'protocol_observation_complete' if success else 'failed',
                'protocol_observation_complete': success, 'library_changed_client_acceptance': False, 'main_acceptance': False,
                'reference': self.reference(), 'controller': getattr(self, 'controller', None), 'node_process': self.child,
                'worker_terminal': self.terminal, 'input_sha256': self.input_sha, 'admin': admin, 'ledger': ledger,
                'outcomes': outcomes, 'restoration': self.restoration, 'restoration_required': self.restoration == 'restoration_required',
                'reserved_operations': self.reserved, 'dispatched_operations': self.dispatched, 'preservation': getattr(self, 'preservation', None),
                'errors': self.errors, 'evidence': dict(self.records), 'completed_at': utc_now()}
        self.save('report.json', value)
        return value

    def run(self):
        try:
            self.load()
            try:
                if self.mode == 'preflight': self.finish_preflight()
                else: self.execute_observation()
            except Exception as error:
                self.error(self.phase, error)
                if self.mode == 'observe': self.recover()
            finally:
                self.cleanup_deadline = self.cleanup_deadline or min(self.hard_deadline, time.monotonic() + 110)
                if self.mode == 'observe':
                    try: self.wait_browser()
                    except Exception as error: self.error('browser-terminal', error)
                    try: self.final_public_state()
                    except Exception as error: self.error('final-public-preservation', error)
        except Exception as error:
            self.error('load-or-dispatch', error)
        finally:
            if self.admin is not None and self.admin.token is not None:
                try:
                    self.cleanup_deadline = self.cleanup_deadline or min(self.hard_deadline, time.monotonic() + 110)
                    self.admin.logout()
                except Exception as error: self.error('administrator-cleanup', error)
            if self.root_fd is not None:
                try:
                    after_media = media_snapshot()
                    self.save('media-after.json', after_media)
                    require(same(after_media, self.media), 'Synthetic media changed during this scope.')
                    self.check(cleanup=True)
                except Exception as error: self.error('final-source-and-media', error)
        try:
            if self.root_fd is not None: return self.report()
            return {'marker': MARKER, 'status': 'failed', 'errors': self.errors, 'protocol_observation_complete': False}
        finally:
            if self.root_fd is not None: os.close(self.root_fd)
            if self.lock is not None: os.close(self.lock)


def validate_preflight(value, script_sha, service):
    require(isinstance(value, dict) and set(value) == {'marker', 'version', 'mode', 'root', 'reference', 'script_sha256',
            'source_closure_sha256', 'public_snapshot', 'target', 'anchor', 'expected_libraries', 'admin', 'ledger', 'preservation', 'errors',
            'status', 'completed_at', 'evidence'} and value['marker'] == PREFLIGHT_MARKER and type(value['version']) is int and value['version'] == 1 and
            value['mode'] == 'business-read-only-preflight' and value['root'] == str(PREFLIGHT_ROOT) and value['status'] == 'passed' and
            value['errors'] == [] and value['script_sha256'] == script_sha and value['source_closure_sha256'] is None and
            same(value['reference'], {'server_id': SERVER, 'base_url': BASE_URL, 'service_identity': service}),
            'The independent preflight lacks its exact successful source and service authority.')
    require(value['public_snapshot'].get('path') == str(PREFLIGHT_ROOT / 'after-public.json') and digest(value['public_snapshot'].get('sha256')) and
            value['preservation'].get('passed') is True and value['admin'].get('user_id') == ADMIN and value['admin'].get('closed') is True and
            all(value['admin'].get(name + '_status') == status and value['admin'].get(name + '_complete') is True
                for name, status in (('login', 200), ('logout', 204), ('exact401', 401))) and
            value['ledger'].get('authentication_posts') == 2 and value['ledger'].get('metadata_posts') == 0 and
            type(value['ledger'].get('http_requests')) is int and value['ledger']['http_requests'] >= 4,
            'Business-read-only preflight did not retain real authentication HTTP and exact cleanup.')
    instant(value['completed_at'])


HTTP_FIELDS = frozenset(('id', 'index', 'method', 'kind', 'route', 'query', 'hidden_query', 'shape_sha256', 'request_sha256',
    'token_sha256', 'request_sequence', 'start_elapsed_ms', 'response_elapsed_ms', 'finished_elapsed_ms', 'phase', 'document_id',
    'page_route', 'sourceworker', 'main_frame', 'from_service_worker', 'content_type', 'status', 'completed', 'failed', 'projection',
    'outcome', 'reason', 'request_bytes', 'response_bytes'))
WIRE_FIELDS = frozenset(('phase', 'physical_exchange_id', 'frame_request_index', 'body_sha256', 'shape_sha256', 'request_sha256', 'token_sha256', 'message_id'))
MESSAGE_ARRAYS = ('CollectionFolders', 'FoldersAddedTo', 'FoldersRemovedFrom', 'ItemsAdded', 'ItemsRemoved', 'ItemsUpdated')


def movies_route(value):
    if not text(value, 4096): return False
    parsed = urlsplit(BASE_URL + value)
    if parsed.scheme != 'http' or parsed.netloc != '127.0.0.1:18197' or parsed.path not in ('/web', '/web/', '/web/index.html'):
        return False
    if parsed.fragment.split('?', 1)[0] != '!/videos': return False
    values = parse_qsl(parsed.fragment.partition('?')[2], keep_blank_values=True)
    if any(re.search('token|password|api_key|authorization', key, re.I) for key, _ in values + parse_qsl(parsed.query)): return False
    parents, servers = ([word for key, word in values if key.lower() == name] for name in ('parentid', 'serverid'))
    return parents == [LIBRARY] and (servers == [] or servers == [SERVER])


def http_shape(row):
    require(isinstance(row, dict) and set(row) == HTTP_FIELDS and text(row['id'], 128) and type(row['index']) is int and row['index'] >= 0 and
            text(row['route'], 4096) and row['route'].startswith('/') and not row['route'].startswith('//') and
            isinstance(row['query'], dict) and len(row['query']) <= 32 and isinstance(row['hidden_query'], list) and len(row['hidden_query']) <= 32,
            'A browser HTTP row lost its complete normalized inventory.')
    require(all(text(key, 80) and isinstance(value, str) and len(value) <= 4096 for key, value in row['query'].items()) and
            all(isinstance(value, dict) and set(value) == {'key', 'value_sha256'} and text(value['key'], 80) and digest(value['value_sha256'])
                for value in row['hidden_query']), 'An HTTP query projection has an unsupported shape.')
    pairs = sorted(row['query'].items())
    hidden = sorted(row['hidden_query'], key=lambda value: (value['key'], value['value_sha256']))
    expected = sha(row['route'].encode() + b'\n' + canonical([list(pair) for pair in pairs]) + b'\n' + canonical(hidden))
    require(row['shape_sha256'] == expected and row['hidden_query'] == hidden and digest(row['request_sha256']) and
            type(row['request_sequence']) is int and row['request_sequence'] >= 0 and numeric(row['start_elapsed_ms']) and row['start_elapsed_ms'] >= 0 and
            type(row['completed']) is bool and type(row['failed']) is bool, 'An HTTP shape digest, sequence or completion type changed.')
    require(all(row[key] is None or type(row[key]) is int and 0 <= row[key] <= 2 << 20 for key in ('request_bytes', 'response_bytes')) and
            (row['outcome'] not in ('failed', 'rejected') or row['failed'] is True), 'HTTP byte or failure accounting changed its actual outcome.')
    if row['projection'] is not None:
        projection = row['projection']
        require(row['kind'] in ('items', 'target') and isinstance(projection, dict) and set(projection) == {'items', 'count', 'body_sha256', 'body_bytes'} and
                isinstance(projection['items'], list) and type(projection['count']) is int and 0 <= projection['count'] == len(projection['items']) <= 128 and
                digest(projection['body_sha256']) and type(projection['body_bytes']) is int and 0 < projection['body_bytes'] <= 2 << 20 and
                all(isinstance(item, dict) and set(item) == {'Id', 'Name', 'Type', 'IsFolder', 'ParentId'} and isinstance(item['Id'], str) and
                    ITEM_ID.fullmatch(item['Id']) and text(item['Name']) and text(item['Type'], 80) and type(item['IsFolder']) is bool and
                    (item['ParentId'] is None or isinstance(item['ParentId'], str)) for item in projection['items']),
                'A catalog response does not retain its bounded real item projection.')


def pair_reads(physical, frames):
    require(isinstance(physical, list) and isinstance(frames, list) and len(physical) <= 2000 and len(frames) <= 2000,
            'A browser HTTP inventory exceeds its fixed bound.')
    for row in physical + frames: http_shape(row)
    require(len({row['id'] for row in physical}) == len(physical) and len({row['index'] for row in frames}) == len(frames),
            'A physical exchange or frame request was duplicated.')
    eligible = [row for row in frames if digest(row['token_sha256']) and row['completed'] is True and row['failed'] is False and
        type(row['status']) is int and row['status'] == 200 and row['content_type'] == 'application/json' and row['from_service_worker'] is False and
        (row['main_frame'] is True and row['sourceworker'] is False or row['main_frame'] is False and row['sourceworker'] is True)]
    def fits(frame, wire):
        return wire['completed'] is True and wire['failed'] is False and type(wire['status']) is int and wire['status'] == 200 and \
            wire['projection'] is not None and frame['kind'] == wire['kind'] and all(frame[key] == wire[key]
                for key in ('token_sha256', 'request_sha256', 'shape_sha256')) and \
            all(numeric(row['finished_elapsed_ms']) for row in (frame, wire)) and \
            frame['start_elapsed_ms'] <= wire['start_elapsed_ms'] <= wire['finished_elapsed_ms'] <= frame['finished_elapsed_ms'] + 1000 and \
            wire['start_elapsed_ms'] <= frame['finished_elapsed_ms']
    pairs = []
    for frame in eligible:
        matches = [wire for wire in physical if fits(frame, wire)]
        wire = matches[0] if len(matches) == 1 else None
        unique = wire is not None and sum(fits(other, wire) for other in eligible) == 1
        pairs.append({'frame': frame, 'physical': wire if unique else None, 'complete': unique, 'unambiguous': unique})
    return pairs


def full_movie_query(row):
    if row['kind'] != 'items' or row['route'] not in ('/Items', '/Users/' + VIEWER + '/Items'): return False
    query = {key.lower(): value for key, value in row['query'].items()}
    return len(query) == len(row['query']) and 'ids' not in query and all(query.get(key) == value for key, value in
        {'parentid': LIBRARY, 'includeitemtypes': 'Movie', 'recursive': 'true', 'startindex': '0', 'limit': '50'}.items())


def response_matches(wire, input_record, expected, full=False):
    projection = wire.get('projection')
    if not isinstance(projection, dict): return False
    items = projection['items']
    if any(row['Type'] != 'Movie' or row['IsFolder'] for row in items): return False
    target = [row for row in items if row['Id'] == ITEM]
    if len(target) != 1 or target[0]['Name'] != expected: return False
    if full or wire['kind'] == 'items':
        return len(items) == 2 and {row['Id'] for row in items} == {ITEM, ANCHOR} and expected != input_record['anchor']['name'] and \
            next(row for row in items if row['Id'] == ANCHOR)['Name'] == input_record['anchor']['name']
    return wire['kind'] == 'target' and len(items) == 1 and wire['route'] in ('/Items/' + ITEM, '/Users/' + VIEWER + '/Items/' + ITEM)


def library_messages(events):
    require(isinstance(events, dict) and set(events) == {'physical', 'browser'} and
            all(isinstance(events[key], list) and len(events[key]) <= 2048 for key in events), 'A raw WebSocket message inventory exceeds its bound.')
    selected = {key: [row for row in rows if isinstance(row, dict) and row.get('direction') == 'server' and
                     isinstance(row.get('json'), dict) and row['json'].get('MessageType') == 'LibraryChanged']
                for key, rows in events.items()}
    require(all(len(rows) <= 64 for rows in selected.values()), 'The LibraryChanged subset exceeds its separate bound.')
    return selected


def paired_messages(events, boundary):
    events = library_messages(events)
    def relevant(event):
        message = event.get('json')
        if not isinstance(message, dict) or message.get('MessageType') != 'LibraryChanged': return False
        raw = event.get('json_text')
        if not isinstance(raw, str) or event.get('original_message') is not True or event.get('payload_retained') is not True: return False
        body = raw.encode('utf-8')
        require(0 < len(body) <= 65536 and event.get('bytes') == len(body) and event.get('body_sha256') == sha(body) and same(decode(body), message),
                'An original WebSocket payload no longer matches its actual bytes, digest and JSON.')
        data = message.get('Data')
        shape = set(message) == {'Data', 'MessageId', 'MessageType'} and isinstance(message.get('MessageId'), str) and USER_ID.fullmatch(message['MessageId']) and \
            isinstance(data, dict) and set(data) == {*MESSAGE_ARRAYS, 'IsEmpty'} and data['IsEmpty'] is False and \
            all(isinstance(data[key], list) and len(data[key]) <= 128 and all(text(value, 128) for value in data[key]) for key in MESSAGE_ARRAYS)
        return bool(shape and data['ItemsUpdated'] == [ITEM] and event.get('phase') == boundary['name'] and type(event.get('sequence')) is int and event['sequence'] > boundary['started_sequence'] and
            numeric(event.get('elapsed_ms')) and boundary['started_elapsed_ms'] <= event['elapsed_ms'] <= boundary['end_elapsed_ms'] and
            event.get('connection_id') == boundary['connection_id'] and event.get('token_sha256') == boundary['token_sha256'])
    result = []
    eligible = [row for row in events['browser'] if relevant(row)]
    wires = [row for row in events['physical'] if relevant(row)]
    for browser in eligible:
        matches = [wire for wire in wires if wire.get('forwarded') is True and wire['elapsed_ms'] <= browser['elapsed_ms'] and
            all(same(wire[key], browser[key]) for key in ('body_sha256', 'bytes', 'json', 'json_text'))]
        if len(matches) == 1 and sum(row['body_sha256'] == browser['body_sha256'] for row in eligible) == 1 and \
                browser.get('document_id') == boundary['document_id'] and browser.get('page_route') == boundary['route']:
            result.append({'physical': matches[0], 'browser': browser})
    return result


def message_evidence(events, boundary, qualified):
    selected, reasons = library_messages(events), set()
    def complete(row):
        raw = row.get('json_text')
        if row.get('original_message') is not True or row.get('payload_retained') is not True or not isinstance(raw, str): return False
        body = raw.encode()
        if len(body) != row.get('bytes') or sha(body) != row.get('body_sha256'): return False
        try: return same(decode(body), row.get('json'))
        except (ValueError, ObservationError): return False
    def inside(row):
        return type(row.get('sequence')) is int and row['sequence'] > boundary['started_sequence'] and numeric(row.get('elapsed_ms')) and \
            boundary['started_elapsed_ms'] <= row['elapsed_ms'] <= boundary['end_elapsed_ms'] and row.get('phase') == boundary['name'] and \
            row.get('connection_id') == boundary['connection_id'] and row.get('token_sha256') == boundary['token_sha256']
    received = [row for row in selected['browser'] if inside(row)]
    transport = [row for row in received if complete(row) and row.get('page_route') == boundary['route'] and row.get('document_id') == boundary['document_id'] and
        len([wire for wire in selected['physical'] if inside(wire) and complete(wire) and wire.get('forwarded') is True and
            wire['elapsed_ms'] <= row['elapsed_ms'] and wire['json_text'] == row['json_text'] and wire['body_sha256'] == row['body_sha256']]) == 1 and
        sum(other.get('body_sha256') == row['body_sha256'] for other in received) == 1]
    hashes = {row['browser']['body_sha256'] for row in qualified}
    for row in received:
        if not complete(row): reasons.add('message_integrity_not_proven')
        elif row not in transport: reasons.add('transport_not_paired')
        elif row['body_sha256'] not in hashes:
            message = row['json']
            if not isinstance(message.get('MessageId'), str) or not USER_ID.fullmatch(message['MessageId']): reasons.add('message_id_not_proven')
            elif not isinstance(message.get('Data'), dict): reasons.add('data_shape_not_proven')
            elif message['Data'].get('ItemsUpdated') != [ITEM]: reasons.add('target_update_not_proven')
            else: reasons.add('data_shape_not_proven')
    return {'received_count': len(received), 'transport_paired_count': len(transport), 'qualified_count': len(qualified),
            'unsupported_schema_count': sum(row['body_sha256'] not in hashes for row in transport), 'reasons': sorted(reasons)}


def dom_shape(value, input_record, expected, route, document_id):
    require(isinstance(value, dict) and value.get('route') == route and movies_route(route) and value.get('document_id') == document_id and
            value.get('expected_name') == expected and value.get('anchor_name') == input_record['anchor']['name'] and
            value.get('media_inactive') is True and numeric(value.get('started_elapsed_ms')) and value['started_elapsed_ms'] >= 0 and
            all(type(value.get(key)) is int and 0 <= value[key] <= (32 if key in ('visible_items_containers', 'visible_card_containers') else 64)
                for key in ('visible_items_containers', 'visible_card_containers', 'visible_cards', 'visible_title_buttons',
                            'target_title_count', 'anchor_title_count', 'forbidden_title_count')) and
            all(type(value.get(key)) is bool for key in ('structural_match', 'explicit_identity_consistent', 'identity_proven', 'passed')),
            'A DOM sample has another route, document, target, type or visibility bound.')
    require(value.get('identity_mode') in ('unbound', 'reference-two-movie-wire-and-cards'), 'A DOM identity mode is missing.')
    if value['identity_mode'] == 'unbound':
        require(value.get('target_id') is None and value.get('anchor_id') is None and value.get('wire_identity') is None and
                value['identity_proven'] is False and value['passed'] is False, 'An unbound DOM sample claims an identity.')
    else:
        require(value['identity_mode'] == 'reference-two-movie-wire-and-cards' and value.get('target_id') == ITEM and value.get('anchor_id') == ANCHOR and
                value['identity_proven'] is True and value['passed'] is True and isinstance(value.get('wire_identity'), dict) and set(value['wire_identity']) == WIRE_FIELDS,
                'A bound DOM sample omitted its independently recomputable wire identity.')


def dom_binding(value, pairs, input_record, phase, token, discovery=None, messages=(), after_sequence=None):
    expected = value['expected_name']
    structural = value['structural_match'] is True and value['explicit_identity_consistent'] is True and value['visible_cards'] == 2 and \
        value['visible_title_buttons'] == 2 and value['visible_card_containers'] == 1 and value['target_title_count'] == value['anchor_title_count'] == 1 and \
        value['forbidden_title_count'] == 0 and value['observed_title'] == expected and value['observed_anchor_title'] == input_record['anchor']['name']
    if not structural: return None
    for pair in pairs:
        if not pair['complete']: continue
        wire, frame = pair['physical'], pair['frame']
        full = full_movie_query(wire) and (phase == 'discovery' or any(row['shape_sha256'] == wire['shape_sha256'] for row in discovery['query_allowlist']))
        if not full and (phase == 'discovery' or wire['kind'] != 'target'): continue
        if not response_matches(wire, input_record, expected, phase == 'discovery'): continue
        for message in ([None] if phase == 'discovery' else messages):
            after = after_sequence if message is None else message['browser']['sequence']
            elapsed = 0 if message is None else message['browser']['elapsed_ms']
            if all(row['phase'] == phase and row['token_sha256'] == token and row['request_sequence'] > after and
                    row['start_elapsed_ms'] >= elapsed and row['finished_elapsed_ms'] <= value['started_elapsed_ms'] for row in (wire, frame)) and \
                    wire['method'] == 'GET' and frame['route'] == wire['route'] and frame['page_route'] == value['route'] and frame['document_id'] == value['document_id']:
                return {'phase': phase, 'physical_exchange_id': wire['id'], 'frame_request_index': frame['index'],
                    'body_sha256': wire['projection']['body_sha256'], 'shape_sha256': wire['shape_sha256'], 'request_sha256': wire['request_sha256'],
                    'token_sha256': token, 'message_id': None if message is None else message['browser']['json']['MessageId']}
    return None


def discovery_evidence(value, input_record, token):
    require(isinstance(value, dict) and set(value) == {'home', 'dom', 'reads', 'query_allowlist', 'socket', 'navigation'} and
            isinstance(value['reads'], list) and 1 <= len(value['reads']) <= 8 and isinstance(value['query_allowlist'], list) and
            1 <= len(value['query_allowlist']) <= 8, 'The complete reference discovery contract changed.')
    home, navigation, dom, socket = (value[key] for key in ('home', 'navigation', 'dom', 'socket'))
    library = next(row for row in input_record['expected_libraries'] if row['id'] == LIBRARY)
    require(set(home) == {'route', 'document_id', 'library_id', 'library_name', 'control_count', 'identity_proven'} and
            home['library_id'] == LIBRARY and home['library_name'] == library['name'] and home['control_count'] == 1 and home['identity_proven'] is True and
            set(navigation) == {'before_route', 'after_route', 'before_sequence'} and navigation['before_route'] == home['route'] and
            navigation['after_route'] == dom['route'] and navigation['before_route'] != navigation['after_route'] and
            type(navigation['before_sequence']) is int and navigation['before_sequence'] >= 0 and
            set(socket) == {'connection_id', 'token_sha256', 'seen', 'opened'} and socket['token_sha256'] == token and
            text(socket['connection_id'], 128) and type(socket['seen']) is int and type(socket['opened']) is int and 1 <= socket['opened'] <= socket['seen'] <= 2,
            'The Home click, Movies navigation or original socket is not independently bound.')
    dom_shape(dom, input_record, input_record['target']['name'], navigation['after_route'], dom['document_id'])
    require(all(isinstance(row, dict) and set(row) == {'physical', 'frame', 'complete', 'unambiguous'} for row in value['reads']),
            'The discovery does not retain full physical and frame requests.')
    pairs = pair_reads([row['physical'] for row in value['reads']], [row['frame'] for row in value['reads']])
    require(same(pairs, value['reads']) and all(row['complete'] and full_movie_query(row['physical']) and
            response_matches(row['physical'], input_record, input_record['target']['name'], True) for row in pairs),
            'The two-movie catalog was not completely and unambiguously transferred.')
    queries = [{key: row['physical'][key] for key in ('kind', 'route', 'query', 'hidden_query', 'shape_sha256')} for row in pairs]
    require({canonical(row) for row in value['query_allowlist']} == {canonical(row) for row in queries} and
            len(value['query_allowlist']) == len({canonical(row) for row in queries}), 'The query allowlist does not exactly freeze the discovery requests.')
    bound = dom_binding(dom, pairs, input_record, 'discovery', token, after_sequence=navigation['before_sequence'])
    require(bound is not None and dom['passed'] is True and same(dom['wire_identity'], bound), 'The two visible movie cards lack their real initial wire identities.')
    return pairs


def validate_boundary(value, name, discovery):
    require(isinstance(value, dict) and value.get('name') == name and value.get('route') == discovery['dom']['route'] and
            value.get('document_id') == discovery['dom']['document_id'] and value.get('connection_id') == discovery['socket']['connection_id'] and
            value.get('token_sha256') == discovery['socket']['token_sha256'] and value.get('websocket_seen') == discovery['socket']['seen'] and
            value.get('websocket_opened') == discovery['socket']['opened'] and type(value.get('started_sequence')) is int and value['started_sequence'] >= 0 and
            numeric(value.get('started_elapsed_ms')) and value['started_elapsed_ms'] >= 0, 'An armed window changed its document or socket lifetime.')


def window_evidence(value, input_record, reservation, discovery):
    require(isinstance(value, dict) and set(value) == {'name', 'control_sha256', 'commit', 'boundary', 'events', 'http', 'dom', 'actions', 'lifecycle',
            'result', 'outcome', 'status', 'proof', 'notification_context', 'message_evidence'} and value['name'] in ('forward', 'restored'), 'A reference observation window changed shape.')
    name, boundary = value['name'], value['boundary']
    expected = reservation['marker_name'] if name == 'forward' else reservation['original_name']
    validate_boundary(boundary, name, discovery)
    discovery_evidence(discovery, input_record, boundary['token_sha256'])
    require(digest(value['control_sha256']) and isinstance(value['commit'], dict) and set(value['commit']) ==
            {'write_completed_at', 'native_result_sha256', 'admin_readback_sha256', 'viewer_readback_sha256'} and
            all(digest(value['commit'][key]) for key in ('native_result_sha256', 'admin_readback_sha256', 'viewer_readback_sha256')),
            'A window lost its exact physical write and two independent API readbacks.')
    instant(value['commit']['write_completed_at'])
    require(type(boundary.get('duration_ms')) is int and boundary['duration_ms'] == 120000 and
            all(numeric(boundary.get(key)) for key in ('end_elapsed_ms', 'response_completed_elapsed_ms', 'completed_elapsed_ms')) and
            boundary['end_elapsed_ms'] == boundary['response_completed_elapsed_ms'] + 120000 and
            boundary['started_elapsed_ms'] <= boundary['response_completed_elapsed_ms'] + 1000 and
            boundary['end_elapsed_ms'] <= boundary['completed_elapsed_ms'] <= boundary['end_elapsed_ms'] + 5000 and
            value['actions'] == value['lifecycle'] == [] and isinstance(value['dom'], list) and 1 <= len(value['dom']) <= 260,
            'The passive window was shortened, interrupted or changed by an action.')
    http = value['http']
    require(isinstance(http, dict) and set(http) == {'physical', 'frames', 'pairs'}, 'The original HTTP arrays were omitted.')
    pairs = pair_reads(http['physical'], http['frames'])
    refs = [{'frame_request_index': row['frame']['index'], 'physical_exchange_id': row['physical']['id'] if row['physical'] else None,
             'complete': row['complete'], 'unambiguous': row['unambiguous']} for row in pairs]
    require(same(http['pairs'], refs), 'The compact HTTP references differ from independently recomputed full arrays.')
    for index, sample in enumerate(value['dom']):
        require(isinstance(sample, dict) and set(sample) == {'sequence', 'elapsed_ms', 'started_elapsed_ms', 'observation'} and
                type(sample['sequence']) is int and sample['sequence'] > boundary['started_sequence'] and
                numeric(sample['started_elapsed_ms']) and numeric(sample['elapsed_ms']) and
                boundary['started_elapsed_ms'] <= sample['started_elapsed_ms'] <= sample['observation']['started_elapsed_ms'] <= sample['elapsed_ms'] <= boundary['end_elapsed_ms'],
                'A passive DOM sample is outside its original ordered interval.')
        dom_shape(sample['observation'], input_record, expected, boundary['route'], boundary['document_id'])
        if index:
            prior = value['dom'][index - 1]
            require(sample['sequence'] > prior['sequence'] and 0 <= sample['started_elapsed_ms'] - prior['elapsed_ms'] <= 4000,
                    'The passive sample sequence contains a gap or reversal.')
    require(value['dom'][0]['started_elapsed_ms'] <= boundary['started_elapsed_ms'] + 4000 and
            value['dom'][-1]['elapsed_ms'] >= boundary['end_elapsed_ms'] - 4000, 'The window omitted its initial or final passive samples.')
    messages = paired_messages(value['events'], boundary)
    message_summary = message_evidence(value['events'], boundary, messages)
    accepted = []
    for pair in pairs:
        if not pair['complete']: continue
        wire, frame = pair['physical'], pair['frame']
        if wire['method'] == 'GET' and wire['phase'] == frame['phase'] == name and wire['route'] == frame['route'] and \
                wire['token_sha256'] == frame['token_sha256'] == boundary['token_sha256'] and frame['document_id'] == boundary['document_id'] and \
                frame['page_route'] == boundary['route'] and wire['finished_elapsed_ms'] <= boundary['end_elapsed_ms'] and \
                frame['finished_elapsed_ms'] <= boundary['end_elapsed_ms'] and response_matches(wire, input_record, expected) and \
                (wire['kind'] == 'target' or full_movie_query(wire) and any(row['shape_sha256'] == wire['shape_sha256'] for row in discovery['query_allowlist'])) and \
                any(all(row['request_sequence'] > message['browser']['sequence'] and row['start_elapsed_ms'] >= message['browser']['elapsed_ms']
                    for row in (wire, frame)) for message in messages): accepted.append(pair)
    bound = [(sample, dom_binding(sample['observation'], pairs, input_record, name, boundary['token_sha256'], discovery, messages))
             for sample in value['dom']]
    def matches(observed, title):
        return observed['structural_match'] is True and observed['explicit_identity_consistent'] is True and observed['visible_cards'] == 2 and \
            observed['visible_title_buttons'] == 2 and observed['visible_card_containers'] == 1 and observed['anchor_title_count'] == 1 and \
            observed['observed_title'] == title and observed['observed_anchor_title'] == input_record['anchor']['name']
    previous_name = reservation['original_name'] if name == 'forward' else reservation['marker_name']
    first = next(((sample, proof) for sample, proof in bound if proof is not None and sample['observation']['passed'] is True and
        same(sample['observation']['wire_identity'], proof) and any(prior['elapsed_ms'] < sample['started_elapsed_ms'] and
            matches(prior['observation'], previous_name) for prior in value['dom'])), None)
    stable = first is not None and all(matches(sample['observation'], expected) for sample, _ in bound if sample['started_elapsed_ms'] >= first[0]['started_elapsed_ms'])
    final = value['dom'][-1]['observation']
    changed = bool(first and stable and matches(final, expected) and bound[-1][1] is not None and final['passed'] is True and same(final['wire_identity'], bound[-1][1]))
    context = {'additional_items': False, 'parent_context': False}
    for event in value['events']['browser']:
        message = event.get('json')
        data = message.get('Data') if isinstance(message, dict) else None
        if not isinstance(message, dict) or message.get('MessageType') != 'LibraryChanged' or not isinstance(data, dict): continue
        context['additional_items'] |= any(isinstance(data.get(key), list) and bool(data[key]) for key in ('ItemsAdded', 'ItemsRemoved')) or \
            isinstance(data.get('ItemsUpdated'), list) and any(item != ITEM for item in data['ItemsUpdated'])
        context['parent_context'] |= any(isinstance(data.get(key), list) and bool(data[key]) for key in ('CollectionFolders', 'FoldersAddedTo', 'FoldersRemovedFrom'))
    return {'result': 'changed' if changed else 'not_observed',
        'outcome': 'automatic_http_and_visible_transition_observed' if changed else
            'matches_expected_without_proven_transition' if matches(final, expected) else 'automatic_refresh_not_observed_within_window',
        'status': {'message_observed': message_summary['received_count'] > 0, 'http_observed': bool(accepted), 'dom_matches_expected': bool(matches(final, expected)),
                   'visible_transition_observed': changed},
        'proof': {**first[1], 'first_dom_sequence': first[0]['sequence'], 'last_dom_sequence': value['dom'][-1]['sequence']} if changed else None,
        'notification_context': context, 'message_evidence': message_summary}


def validate_private_session(value, input_record, input_sha, child, before):
    require(isinstance(child, dict) and child.get('cgroup') == '/system.slice/' + WORKER_UNIT and
            isinstance(value, dict) and set(value) == {'marker', 'version', 'input_sha256', 'source_closure_sha256', 'controller', 'node_process', 'token', 'proof'} and
            value['marker'] == 'goby-reference-library-changed-session-private-v1' and type(value['version']) is int and value['version'] == 1 and
            value['input_sha256'] == input_sha and value['source_closure_sha256'] == sha(canonical(input_record['source_closure'])) and
            same(value['controller'], input_record['controller']) and same(value['node_process'], child) and text(value['token'], 8192),
            'The private UI acknowledgement is outside its current source, controller and browser process.')
    proof = value['proof']
    require(isinstance(proof, dict) and set(proof) == {'token_sha256', 'session_id', 'user_id', 'client_name', 'device_id', 'device_name',
            'client_version', 'server_id', 'created_at', 'created_at_source', 'kind', 'slot', 'frame_login_finished', 'physical_login_completed', 'request_metadata_matches'} and
            proof['token_sha256'] == sha(value['token'].encode()) and proof['user_id'] == VIEWER and proof['server_id'] == SERVER and
            proof['kind'] == 'emby' and proof['slot'] == 'B' and proof['created_at_source'] == 'SessionInfo.LastActivityDate' and
            all(proof[key] is True for key in ('frame_login_finished', 'physical_login_completed', 'request_metadata_matches')) and
            all(text(proof[key], 256) for key in ('session_id', 'client_name', 'device_id', 'device_name', 'client_version')),
            'The viewer login is not one complete frame and physical acknowledgement of the same actual client.')
    require(instant(before['captured_at']) <= instant(proof['created_at']) and
            all(proof['device_id'] not in (key, row['Id'], row['ReportedDeviceId']) for key, row in before['devices'].items()),
            'The UI reused a device or predates its independent public baseline.')
    return proof


def validate_stage(value, input_record, input_sha, child, name, previous, proof, discovery, reservation):
    require(isinstance(child, dict) and child.get('cgroup') == '/system.slice/' + WORKER_UNIT and
            isinstance(value, dict) and set(value) == {'marker', 'version', 'input_sha256', 'source_closure_sha256', 'controller', 'node_process',
            'name', 'token_sha256', 'session_private', 'previous_control_sha256', 'observation'} and
            value['marker'] == 'goby-reference-library-changed-stage-v1' and type(value['version']) is int and value['version'] == 1 and
            value['name'] == name and value['input_sha256'] == input_sha and value['source_closure_sha256'] == sha(canonical(input_record['source_closure'])) and
            same(value['controller'], input_record['controller']) and same(value['node_process'], child) and
            value['token_sha256'] == proof['token_sha256'] and value['previous_control_sha256'] == previous and
            isinstance(value['session_private'], dict) and set(value['session_private']) == {'path', 'sha256'} and
            value['session_private']['path'] == str(ROOT / 'browser/session-private.json') and digest(value['session_private']['sha256']),
            'The IPC stage lost its exact current input, token, worker or previous control.')
    observation = value['observation']
    if name == 'discovery': return discovery_evidence(observation, input_record, proof['token_sha256'])
    require(discovery is not None, 'A later stage lacks its already proved discovery.')
    if name == 'armed':
        require(isinstance(observation, dict) and set(observation) == {'quiet', 'boundary'}, 'The armed stage changed shape.')
        validate_boundary(observation['boundary'], 'forward', discovery)
        quiet = observation['quiet']
        require(isinstance(quiet, dict) and set(quiet) == {'started_elapsed_ms', 'completed_elapsed_ms', 'started_sequence', 'duration_ms',
                'catalog_requests', 'library_changed_messages', 'http', 'events', 'samples', 'passed'} and
                quiet['passed'] is True and type(quiet['duration_ms']) is int and quiet['duration_ms'] == 75000 and
                type(quiet['started_sequence']) is int and quiet['started_sequence'] >= 0 and
                numeric(quiet['started_elapsed_ms']) and numeric(quiet['completed_elapsed_ms']) and
                75000 <= quiet['completed_elapsed_ms'] - quiet['started_elapsed_ms'] <= 80000 and
                isinstance(quiet['samples'], list) and 2 <= len(quiet['samples']) <= 200 and
                observation['boundary']['started_elapsed_ms'] >= quiet['completed_elapsed_ms'], 'The full 75-second semantic quiet interval is missing.')
        require(isinstance(quiet['http'], dict) and set(quiet['http']) == {'physical', 'frames'}, 'The quiet raw HTTP interval is missing.')
        pair_reads(quiet['http']['physical'], quiet['http']['frames'])
        for row in quiet['http']['physical'] + quiet['http']['frames']:
            require(row['request_sequence'] > quiet['started_sequence'] and quiet['started_elapsed_ms'] <= row['start_elapsed_ms'] <= quiet['completed_elapsed_ms'],
                    'A quiet HTTP row is outside its original interval.')
        selected = library_messages(quiet['events'])
        for rows in quiet['events'].values():
            for row in rows:
                require(type(row.get('sequence')) is int and row['sequence'] > quiet['started_sequence'] and
                        numeric(row.get('elapsed_ms')) and quiet['started_elapsed_ms'] <= row['elapsed_ms'] <= quiet['completed_elapsed_ms'],
                        'A quiet application message is outside its original interval.')
        catalog_count = sum(row['kind'] in ('items', 'target') for rows in quiet['http'].values() for row in rows)
        message_count = sum(len(rows) for rows in selected.values())
        require(type(quiet['catalog_requests']) is int and type(quiet['library_changed_messages']) is int and
                quiet['catalog_requests'] == catalog_count == 0 and quiet['library_changed_messages'] == message_count == 0,
                'The quiet interval includes a catalog request or LibraryChanged message.')
        pairs = discovery_evidence(discovery, input_record, proof['token_sha256'])
        previous_sample = None
        for sample in quiet['samples']:
            dom = sample['observation']
            dom_shape(dom, input_record, reservation['original_name'], discovery['dom']['route'], discovery['dom']['document_id'])
            bound = dom_binding(dom, pairs, input_record, 'discovery', proof['token_sha256'], after_sequence=discovery['navigation']['before_sequence'])
            require(bound is not None and dom['passed'] is True and same(dom['wire_identity'], bound) and
                    type(sample.get('sequence')) is int and numeric(sample.get('started_elapsed_ms')) and numeric(sample.get('elapsed_ms')) and
                    quiet['started_elapsed_ms'] <= sample['started_elapsed_ms'] <= dom['started_elapsed_ms'] <= sample['elapsed_ms'] <= quiet['completed_elapsed_ms'],
                    'A quiet sample lost its exact persistent card and discovery-wire binding.')
            if previous_sample:
                require(sample['sequence'] > previous_sample['sequence'] and 0 <= sample['started_elapsed_ms'] - previous_sample['elapsed_ms'] <= 4000,
                        'Quiet sampling omitted an interval.')
            previous_sample = sample
        require(quiet['samples'][0]['started_elapsed_ms'] <= quiet['started_elapsed_ms'] + 4000 and
                quiet['samples'][-1]['elapsed_ms'] >= quiet['started_elapsed_ms'] + 71000, 'Quiet sampling was truncated.')
    elif name in ('restore-armed', 'restored'):
        require(isinstance(observation, dict) and set(observation) == ({'forward', 'boundary'} if name == 'restore-armed' else {'restored'}),
                'A completed window stage changed shape.')
        window = observation['forward' if name == 'restore-armed' else 'restored']
        derived = window_evidence(window, input_record, reservation, discovery)
        require(same(derived, {key: window[key] for key in derived}), 'The reported reference outcome differs from independent raw evidence.')
        if name == 'restore-armed': validate_boundary(observation['boundary'], 'restored', discovery)
    else: raise ObservationError('An unsupported stage was requested.')
    return observation


def validate_browser_report(report, input_record, input_sha, child, private, stages, controls):
    require(isinstance(child, dict) and child.get('cgroup') == '/system.slice/' + WORKER_UNIT and
            isinstance(report, dict) and report.get('marker') == 'goby-reference-library-changed-browser-v1' and
            type(report.get('version')) is int and report['version'] == 1 and report.get('input_sha256') == input_sha and
            report.get('source_closure_sha256') == sha(canonical(input_record['source_closure'])) and
            same(report.get('controller'), input_record['controller']) and same(report.get('node_process'), child) and
            same(report.get('reference'), input_record['reference']) and same(report.get('target'), input_record['target']) and
            same(report.get('anchor'), input_record['anchor']) and report.get('library_changed_client_acceptance') is False and
            report.get('main_acceptance') is False and private is not None and same(report.get('login_proof'), private['proof']),
            'The final browser report changed its source, identity, scope or acceptance boundary.')
    proof, actor = private['proof'], report.get('actor', {})
    require(actor.get('closed') is True and actor.get('cleanup_failures') == [] and actor.get('session_proof_owner') == 'runtime_viewer_only' and
            actor.get('user_id') == VIEWER and actor.get('server_id') == SERVER and actor.get('token_sha256') == proof['token_sha256'] and
            actor.get('http', {}).get('login') == actor['http'].get('logout') == 1 and actor['http'].get('active') == 0 and
            actor.get('websocket', {}).get('active') == 0 and actor['websocket'].get('opened') == actor['websocket'].get('closed') and
            actor.get('logout', {}).get('status') == 204 and actor['logout'].get('physical_completed') is True and
            actor['logout'].get('login_view_visible') is True and actor['logout'].get('post_logout_status') == 401 and
            actor['logout'].get('token_sha256') == actor['logout'].get('frame_token_sha256') == proof['token_sha256'],
            'The runtime did not close the exact viewer UI session, physical logout and browser/socket resources.')
    session = actor.get('session_proof', {})
    entries = session.get('entries')
    require(session.get('outcome') == 'all_observed_logout_tokens_rejected' and isinstance(entries, list) and len(entries) == 1 and
            entries[0].get('token_fingerprint') == proof['token_sha256'] and entries[0].get('result') == 'logout_token_rejected' and
            entries[0].get('ui_request', {}).get('response_status') == 204 and
            entries[0].get('verification', {}).get('status') == 401 and entries[0]['verification'].get('is_ui_request') is False and
            entries[0]['verification'].get('method') == 'GET' and entries[0]['verification'].get('route') == '/emby/System/Info' and
            entries[0]['verification'].get('result') == 'token_rejected', 'The runtime-owned unique same-token 401 proof is missing.')
    if report.get('protocol_observation_complete') is True:
        require(report.get('failure') is None and report.get('result') == 'protocol_observation_complete' and report.get('restoration') == 'confirmed' and
                len(stages) == 4 and len(controls) == 4 and
                same(report.get('stages'), [{key: row[key] for key in ('name', 'path', 'sha256')} for row in stages]) and
                same(report.get('controls'), [{key: row[key] for key in ('name', 'path', 'sha256')} for row in controls]) and
                same(report.get('discovery'), stages[0]['value']['observation']) and same(report.get('armed'), stages[1]['value']['observation']) and
                same(report.get('forward'), stages[2]['value']['observation']['forward']) and same(report.get('restored'), stages[3]['value']['observation']['restored']),
                'The completed report does not retain all four exact stages and controls.')
    else:
        require(report.get('result') == 'failed' and report.get('failure') is not None, 'An incomplete browser run was relabelled as complete.')


def arguments(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--mode', choices=('preflight', 'observe'), required=True)
    parser.add_argument('--script-sha256', required=True)
    parser.add_argument('--preflight-sha256')
    parser.add_argument('--source-closure', type=Path)
    parser.add_argument('--source-closure-sha256')
    parser.add_argument('--node', type=Path)
    parser.add_argument('--node-sha256')
    args = parser.parse_args(argv)
    require(digest(args.script_sha256), 'An explicit reviewed controller hash is required.')
    values = (args.preflight_sha256, args.source_closure, args.source_closure_sha256, args.node, args.node_sha256)
    if args.mode == 'preflight':
        require(all(value is None for value in values), 'Business-read-only preflight cannot accept browser or metadata inputs.')
    else:
        require(digest(args.preflight_sha256) and digest(args.source_closure_sha256) and args.source_closure == TOOL / 'source-closure.json' and
                args.node == NODE and args.node_sha256 == NODE_SHA, 'The actual observation requires its exact preflight, source closure and owned Node pins.')
    return args


def main():
    try:
        args = arguments()
        result = Run(args).run()
        print(canonical({'marker': result['marker'], 'status': result['status'], 'protocol_observation_complete': result.get('protocol_observation_complete', False),
            'library_changed_client_acceptance': False, 'main_acceptance': False}).decode())
        return 0 if result['status'] in ('passed', 'protocol_observation_complete') else 1
    except Exception as error:
        print(canonical({'marker': MARKER, 'status': 'failed', 'failure_type': type(error).__name__,
            'protocol_observation_complete': False, 'library_changed_client_acceptance': False, 'main_acceptance': False}).decode())
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
