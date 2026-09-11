#!/usr/bin/env python3
"""Capture a pinned positive-profile preparation ledger using one read-only transaction.

Only the verification cluster on port15432 is reachable. This tool never sends
HTTP or runs a service/database mutation. Exact selected rows are private; stdout
contains counts and digests. The accepted setup snapshots remain immutable.
"""

from __future__ import annotations

import argparse
import datetime as dt
from decimal import Decimal
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import types

WORK = Path('/opt/goby-test/exec-work-m3e')
SCOPE = 'schema27-positive-special-features-01'
MARKER = 'goby-client-positive-prepare-observation-v1'
LEDGER = 'goby-client-positive-prepare-ledger-v1'
PROFILE = 'goby-client-special-features-fixture-v1'
CONTINUATION = 'goby-client-special-features-fixture-continuation-v1'
CONTINUATION_ROOT = WORK / 'client-special-features-fixture-continuation-v1'
ORIGINAL_ROOT = WORK / 'client-special-features-fixture-v1'
CONTINUATION_LIBRARY = '57a85c1ca5b6c7ae602c587755250b2f'
CONTINUATION_ROOT_ID = '604d2c0f5c78919a6ee360cda2048066'
ORIGIN_PINS = {
    'origin_failure': (ORIGINAL_ROOT / 'failure.json', '9ce73d72d9ce002dce06230a73213294903800b3b49b06e9b9aa33ad69eda2c0'),
    'origin_state': (CONTINUATION_ROOT / 'origin-state.json', '513d971260e18d24ada666a3ec942391d4679bf2a98c5c852742cc70033fccf1'),
    'origin_before_state': (ORIGINAL_ROOT / 'before-state.json', '6d719ab6f3cd6c13ebf9fe3d6a927abaf5040e6e87e84be5d81a57318440cefe'),
    'origin_before_snapshot': (ORIGINAL_ROOT / 'before-full.json', 'd65097b9916f99723b670faec5d58a01a5380a31c2a369c19db8fbb067f67376'),
    'origin_current_snapshot': (ORIGINAL_ROOT / 'failed-current-full.json', '86cbffbc1b3c0d623ea894aab12843273adcce5c7b097f3b9878dfc276ee8275'),
    'origin_library_ack': (ORIGINAL_ROOT / 'private/library-ack.json', '998dffd8122cc7921df1a21092a357881e676950c6b858cd0f7332a3b8ecb94d'),
}
ORIGIN_SOURCE_PINS = {
    'origin_setup_source': ('prepare-client-special-features-fixture.py', '84af95f1d34227dc9c465b73545c6de6939963d44fb3fc2d2df50e7f0f759465'),
    'origin_profile_source': ('client-special-features-profile.py', '6bbc0bfd17b1c7eef44028fe34481436d39a3e6645442340189e2da35c382252'),
}
FINALIZATION = 'goby-client-special-features-protocol-finalization-v1'
FINALIZATION_ROOT = WORK / 'client-special-features-protocol-finalization-v1'
FINALIZATION_JOB = 'd7aa0acaee023dd4c82ea7a303c354ca'
FINALIZATION_PINS = {
    'continuation_failure': (CONTINUATION_ROOT / 'failure.json', '5696a8f7b57f2fec0b2a6bc4d056426f02faaa860ef830c4abc7989e49a54f4d'),
    'continuation_state': (FINALIZATION_ROOT / 'continuation-state.json', '0897f2bec4723d5a69df8b35978bc4be32e7ea91f1f11aebfda4c500c2116e83'),
    'continuation_snapshot': (CONTINUATION_ROOT / 'failed-current-full.json', '550b6f83cd804485cf227ee3514fb6dec0af550d61ec5926303ce589047877f8'),
    'retained_structure_proof': (WORK / 'client-special-features-protocol-review-01/report.json', '3a6b4d35b27944335bf9838f64618dc41254a79447b7cb9bceb540be1ca540c0'),
    'retained_protocol_proof': (WORK / 'client-special-features-protocol-review-01/protocol-proof.json', 'db65b1016efed40297ab3014105d4748bd8febc5ba4a3d8284721872c3045c21'),
}
FINALIZATION_SOURCE_PINS = {
    'continuation_setup_source': ('continue-client-special-features-fixture-v2.py', 'f5b448b5715456e0539b79afa84fab0ad53acee00bc6e5fb77051ff49131fca8'),
    'continuation_profile_source': ('client-special-features-continuation-profile-v2.py', '71d549fd60193d6813ad97a4cc587460c14d47fc0a96ce74c0367b4acf45019a'),
}
OPERATOR_SHA = '84d21e8ac0b48c5dfd3d2c7ae35b0aec0d7f4f811e65658081490aa44947b49c'
OPERATOR = WORK / 'client-schema27-tool-01/prepare-client-fixture.py'
MAX_BYTES = 32 << 20
HASH = re.compile(r'[0-9a-f]{64}')
ID = re.compile(r'[0-9a-f]{32}')
TABLE_KEYS = {'sessions': ('id',), 'play_sessions': ('id',), 'user_item_data': ('user_id', 'item_id'),
              'client_playback_references': ('user_id', 'auth_session_id', 'application_client_id', 'device_id', 'client_nonce'),
              'encoding_jobs': ('id',)}
AUTH_FIELDS = ('id', 'user_id', 'kind', 'device_id', 'created_at', 'last_seen_at', 'expires_at', 'revoked_at')
USER_FIELDS = ('id', 'is_administrator', 'is_disabled', 'policy', 'configuration')
AUXILIARY = ('item_theme_resources', 'theme_reserved_paths', 'item_extra_resources', 'extra_reserved_paths')
CONNECTION = ['/usr/sbin/runuser', '-u', 'postgres', '--', '/usr/lib/postgresql/17/bin/psql',
              '-X', '--no-password', '-qAt', '-v', 'ON_ERROR_STOP=1',
              '-h', '/var/lib/postgresql/goby-workspace-v1/socket', '-p', '15432', '-d', 'goby_client_m3e']


class ScopeError(Exception):
    """An explicit scope or evidence requirement failed."""


def require(value, code):
    if not value:
        raise ScopeError(code)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def exact(value):
    if value is None:
        return 'null'
    if type(value) is bool:
        return 'true' if value else 'false'
    if type(value) is int or isinstance(value, Decimal):
        require(not isinstance(value, Decimal) or value.is_finite(), 'invalid_json_number')
        return str(value)
    if isinstance(value, str):
        return json.dumps(value, ensure_ascii=False)
    if isinstance(value, list):
        return '[' + ','.join(exact(member) for member in value) + ']'
    require(isinstance(value, dict) and all(isinstance(key, str) for key in value), 'invalid_json_value')
    return '{' + ','.join(exact(key) + ':' + exact(value[key]) for key in sorted(value)) + '}'


def same(left, right):
    return exact(left) == exact(right)


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, 'duplicate_json_key')
        result[key] = value
    return result


def decode(raw):
    return json.loads(raw, parse_float=Decimal, object_pairs_hook=unique_object,
                      parse_constant=lambda _: (_ for _ in ()).throw(ScopeError('invalid_json_number')))


def directory(path, mode=None):
    require(path.is_absolute() and '..' not in path.parts, 'noncanonical_directory')
    for member in reversed((path, *path.parents)):
        info = member.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                'untrusted_directory')
    require(mode is None or stat.S_IMODE(path.lstat().st_mode) == mode, 'directory_mode_mismatch')


def read_record(record, *, modes=(0o600,), raw=False):
    require(isinstance(record, dict) and set(record) == {'path', 'sha256'} and
            isinstance(record['sha256'], str) and HASH.fullmatch(record['sha256']), 'invalid_record_pin')
    path = Path(record['path'])
    require(path.is_absolute() and WORK in path.parents and '..' not in path.parts, 'record_path_escape')
    directory(path.parent)
    before = path.lstat()
    require(stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and before.st_nlink == 1 and
            stat.S_IMODE(before.st_mode) in modes and 0 < before.st_size <= MAX_BYTES, 'untrusted_record_file')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as stream:
        opened = os.fstat(stream.fileno())
        require((opened.st_dev, opened.st_ino) == (before.st_dev, before.st_ino), 'record_open_drift')
        data = stream.read(MAX_BYTES + 1)
    after = path.lstat()
    require((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) ==
            (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns) and
            len(data) == before.st_size and digest(data) == record['sha256'], 'record_identity_or_digest_drift')
    return data if raw else decode(data)


def write_private(path, data):
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb') as stream:
        stream.write(data)
        stream.flush()
        os.fsync(stream.fileno())
    fd = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def instant(value):
    require(isinstance(value, str), 'missing_timestamp')
    try:
        result = dt.datetime.fromisoformat(value.replace('Z', '+00:00'))
    except ValueError:
        raise ScopeError('invalid_timestamp') from None
    require(result.tzinfo is not None, 'unqualified_timestamp')
    return result


def keyed(rows, fields):
    require(isinstance(rows, list), 'missing_rows')
    result = {}
    for row in rows:
        require(isinstance(row, dict) and all(key in row for key in fields), 'invalid_row_key')
        key = tuple(row[field] for field in fields)
        require(key not in result, 'duplicate_row_key')
        result[key] = row
    return result


def ordered(rows):
    return sorted(rows, key=exact)


def projection(tables, actors, movie):
    """Project only nonsecret actor fields; keep exact monitored business rows."""
    users = set(actors.values())
    result = {name: ordered([row for row in tables[name] if row['user_id'] in users]) for name in TABLE_KEYS}
    auth = []
    for row in result['sessions']:
        token = row['token_hash']
        require(isinstance(token, str) and re.fullmatch(r'\\x[0-9a-f]{64}', token), 'invalid_stored_token_fingerprint')
        auth.append(dict({field: row[field] for field in AUTH_FIELDS}, token_fingerprint_sha256=token[2:]))
    result['sessions'] = ordered(auth)
    result['users_state'] = ordered([{field: row[field] for field in USER_FIELDS} for row in tables['users'] if row['id'] in users])
    result['user_settings'] = ordered([row for row in tables['user_settings'] if row['user_id'] in users])
    result['movie_rows'] = [row for row in tables['items'] if row['id'] == movie]
    for name in AUXILIARY:
        result[name] = ordered(tables[name])
    return result


def ordinary_movie(rows, item_id):
    matches = rows['movie_rows']
    require(len(matches) == 1 and matches[0]['id'] == item_id, 'positive_movie_missing')
    item = matches[0]
    require(item['type'] == 'Movie' and item['is_folder'] is False and isinstance(item.get('media'), dict) and
            type(item['media'].get('DurationTicks')) is int and item['media']['DurationTicks'] > 0,
            'positive_movie_shape_changed')
    for links, markers in (('item_theme_resources', 'theme_reserved_paths'), ('item_extra_resources', 'extra_reserved_paths')):
        require(not any(row['resource_item_id'] == item_id for row in rows[links]), 'positive_movie_is_auxiliary')
        require(not any(row['root_id'] == item['root_id'] and
                (item['relative_path'] == row['relative_path'] or row['is_directory'] and
                 item['relative_path'].startswith(row['relative_path'] + '/')) for row in rows[markers]),
                'positive_movie_is_reserved')


def setup_ledger_contract(declared, pin, actors, baseline, tables):
    require({'path': declared['path'], 'sha256': declared['sha256']} == pin and
            declared.get('actors') == {'primary': actors['B'], 'av': actors['A']} and
            declared.get('scope_user_ids') == sorted(actors.values()), 'setup_ledger_binding_mismatch')
    require(same(declared['counts'], {name: len(baseline[name]) for name in TABLE_KEYS}) and
            same(declared['global_counts'], {name: len(tables[name]) for name in TABLE_KEYS}),
            'profile_scoped_or_global_counts_mismatch')


def profile_paths(ledger, completed):
    finalized = ledger['profile']['path'] == str(FINALIZATION_ROOT / 'completed.json')
    continued = finalized or ledger['profile']['path'] == str(CONTINUATION_ROOT / 'completed.json')
    root = FINALIZATION_ROOT if finalized else CONTINUATION_ROOT if continued else ORIGINAL_ROOT
    inspection = ('client-special-features-finalization-inspection-v1' if finalized else
                  'client-special-features-continuation-inspection-v1' if continued else 'client-special-features-inspection-v1')
    require(ledger['profile']['path'] == str(root / 'completed.json') and ledger['profile_report']['path'] == str(root / 'report.json') and
            ledger['profile_inspection']['path'] == str(WORK / inspection / 'report.json') and
            ledger['fixture_state']['path'] == str(WORK / 'client-fixture.json') and
            type(completed.get('profile_version')) is int and completed['profile_version'] == (3 if finalized else 2 if continued else 1) and
            ('continuation' in completed) == continued and ('finalization' in completed) == finalized, 'profile_path_mismatch')
    return 'goby-' + inspection


def continuation_contract(chain, completed, setup, inspection):
    keys = {*ORIGIN_PINS, *ORIGIN_SOURCE_PINS, 'origin_evidence', 'marker', 'library_id', 'root_id',
            'creation_admin_session_id', 'new_scan_admin_session_id', 'new_viewer_session_id', 'scan_job_id',
            'creation_requests', 'continuation_library_creates', 'continuation_scan_dispatches',
            'original_failure_preserved', 'creation_admin_revoked', 'continuation_sessions_revoked'}
    require(isinstance(chain, dict) and set(chain) == keys and chain['marker'] == CONTINUATION and
            same(setup.get('continuation'), chain) and same(inspection.get('continuation'), chain), 'continuation_chain_missing_or_changed')
    require(chain['library_id'] == completed['item_ids']['library'] == CONTINUATION_LIBRARY and
            chain['root_id'] == completed['library']['root_id'] == CONTINUATION_ROOT_ID and
            same({key: chain[key] for key in ('creation_requests', 'continuation_library_creates', 'continuation_scan_dispatches')},
                 {'creation_requests': 5, 'continuation_library_creates': 0, 'continuation_scan_dispatches': 1}) and
            all(chain[key] is True for key in ('original_failure_preserved', 'creation_admin_revoked', 'continuation_sessions_revoked')),
            'continuation_effect_scope_changed')
    ids = [chain[key] for key in ('creation_admin_session_id', 'new_scan_admin_session_id', 'new_viewer_session_id', 'scan_job_id')]
    require(all(isinstance(value, str) and ID.fullmatch(value) for value in ids) and len(set(ids)) == 4 and
            chain['scan_job_id'] == completed['job_id'], 'continuation_actor_or_job_identity_changed')
    for key, (path, expected) in ORIGIN_PINS.items():
        require(chain[key] == {'path': str(path), 'sha256': expected}, 'continuation_origin_pin_changed')
    for key, (name, expected) in ORIGIN_SOURCE_PINS.items():
        require(set(chain[key]) == {'path', 'sha256'} and Path(chain[key]['path']).name == name and chain[key]['sha256'] == expected,
                'continuation_origin_source_changed')
    require(set(chain['origin_evidence']) == {'path', 'sha256'} and
            chain['origin_evidence']['path'] == str(CONTINUATION_ROOT / 'origin-evidence.json') and
            HASH.fullmatch(chain['origin_evidence'].get('sha256', '')), 'continuation_inventory_pin_missing')


def read_continuation(completed, setup, inspection, snapshot):
    chain = completed['continuation']
    continuation_contract(chain, completed, setup, inspection)
    documents = {key: read_record(chain[key]) for key in (*ORIGIN_PINS, 'origin_evidence')}
    for key in ORIGIN_SOURCE_PINS:
        read_record(chain[key], modes=(0o600, 0o644), raw=True)
    failure, state, old_state = (documents[key] for key in ('origin_failure', 'origin_state', 'origin_before_state'))
    require(failure.get('marker') == PROFILE and failure.get('result') == 'retained_for_review' and
            failure.get('phase') == 'library_acknowledged' and failure.get('retry_permitted') is False and
            failure.get('old_rows_preserved') is True and failure.get('library_id') == CONTINUATION_LIBRARY and
            failure.get('job_id') is None and failure.get('root_id') is None and
            state.get('phase') == 'preparing_special_features_fixture' and state.get('stage') == 'library_acknowledged' and
            state.get('special_features_profile', {}).get('library_id') == CONTINUATION_LIBRARY and
            old_state.get('phase') == 'ready' and old_state.get('stage') == 'complete' and
            state.get('process') == old_state.get('process') == completed['candidate']['process'], 'retained_creation_failure_changed')
    auth = failure.get('authentication', {})
    require(all(auth.get('admin', {}).get(key) == value for key, value in
                (('login_status', 200), ('logout_status', 204), ('exact_status', 401))) and
            all(auth.get('viewer', {}).get(key) is None for key in ('login_status', 'logout_status', 'exact_status')),
            'retained_creation_cleanup_changed')
    ack = documents['origin_library_ack']
    require(ack.get('Id') == CONTINUATION_LIBRARY and ack.get('CollectionType') == 'movies' and
            ack.get('Paths') == ['/opt/goby-fixtures/client-special-features-m3e-v1/Movies'], 'retained_library_ack_changed')
    before = documents['origin_before_snapshot']['database']['tables']
    created = documents['origin_current_snapshot']['database']['tables']
    current = snapshot['database']['tables']
    old_auth = keyed(before['sessions'], ('id',))
    creation = keyed(created['sessions'], ('id',))
    creation_id = (chain['creation_admin_session_id'],)
    require(set(creation) - set(old_auth) == {creation_id} and all(same(row, creation.get(key)) for key, row in old_auth.items()),
            'retained_creation_auth_population_changed')
    creation_row = creation[creation_id]
    require(creation_row['kind'] == 'admin' and creation_row['user_id'] == completed['users']['admin_id'] and
            creation_row['revoked_at'] is not None and creation_row['token_hash'] == '\\x' + auth['admin']['token_sha256'],
            'retained_creation_admin_identity_changed')
    final_auth = keyed(current['sessions'], ('id',))
    new_ids = {(chain['new_scan_admin_session_id'],), (chain['new_viewer_session_id'],)}
    require(set(final_auth) - set(creation) == new_ids and all(same(row, final_auth.get(key)) for key, row in creation.items()),
            'continued_auth_population_changed')
    for identifier, user, kind in ((chain['new_scan_admin_session_id'], completed['users']['admin_id'], 'admin'),
                                   (chain['new_viewer_session_id'], completed['users']['av_user_id'], 'emby')):
        row = final_auth[(identifier,)]
        require(row['kind'] == kind and row['user_id'] == user and row['revoked_at'] is not None, 'continued_actor_identity_changed')
    roots = [row for row in created['library_roots'] if row['library_id'] == CONTINUATION_LIBRARY]
    jobs = [row for row in current['scan_jobs'] if row['id'] == chain['scan_job_id']]
    require(len(roots) == 1 and roots[0]['id'] == CONTINUATION_ROOT_ID and
            not any(row['library_id'] == CONTINUATION_LIBRARY for row in created['scan_jobs']) and
            len(jobs) == 1 and jobs[0]['library_id'] == CONTINUATION_LIBRARY and jobs[0]['status'] == 'Completed',
            'continued_existing_library_scan_changed')
    return digest(exact(chain).encode())


def finalization_contract(chain, completed, setup, inspection):
    keys = {*FINALIZATION_PINS, *FINALIZATION_SOURCE_PINS, 'continuation_evidence', 'marker', 'scan_job_id', 'viewer_user_id',
            'new_viewer_session_id', 'new_device_id', 'request_count', 'full_resource_count', 'range_resource_count',
            'library_creates', 'scan_dispatches', 'retained_failures_preserved', 'new_viewer_revoked', 'parent_projection'}
    require(isinstance(chain, dict) and set(chain) == keys and chain['marker'] == FINALIZATION and
            same(setup.get('finalization'), chain) and same(inspection.get('finalization'), chain), 'finalization_chain_missing_or_changed')
    require(chain['scan_job_id'] == completed['job_id'] == FINALIZATION_JOB and
            chain['viewer_user_id'] == completed['users']['av_user_id'] and
            isinstance(chain['new_viewer_session_id'], str) and ID.fullmatch(chain['new_viewer_session_id']) and
            type(chain['new_device_id']) is int and chain['new_device_id'] > 0 and
            same({key: chain[key] for key in ('request_count', 'full_resource_count', 'range_resource_count', 'library_creates', 'scan_dispatches')},
                 {'request_count': 11, 'full_resource_count': 4, 'range_resource_count': 4, 'library_creates': 0, 'scan_dispatches': 0}) and
            chain['retained_failures_preserved'] is True and chain['new_viewer_revoked'] is True and
            same(chain['parent_projection'], {'special_feature_count_present': False, 'local_trailer_count': 1}),
            'finalization_effect_scope_changed')
    for key, (path, expected) in FINALIZATION_PINS.items():
        require(chain[key] == {'path': str(path), 'sha256': expected}, 'finalization_origin_pin_changed')
    for key, (name, expected) in FINALIZATION_SOURCE_PINS.items():
        require(set(chain[key]) == {'path', 'sha256'} and Path(chain[key]['path']).name == name and chain[key]['sha256'] == expected,
                'finalization_origin_source_changed')
    require(set(chain['continuation_evidence']) == {'path', 'sha256'} and
            chain['continuation_evidence']['path'] == str(FINALIZATION_ROOT / 'continuation-evidence.json') and
            HASH.fullmatch(chain['continuation_evidence'].get('sha256', '')), 'finalization_inventory_pin_missing')


def read_finalization(completed, setup, inspection, snapshot):
    chain = completed['finalization']
    finalization_contract(chain, completed, setup, inspection)
    documents = {key: read_record(chain[key]) for key in (*FINALIZATION_PINS, 'continuation_evidence')}
    for key in FINALIZATION_SOURCE_PINS:
        read_record(chain[key], modes=(0o600, 0o644), raw=True)
    failure, state, retained = (documents[key] for key in ('continuation_failure', 'continuation_state', 'continuation_snapshot'))
    require(failure.get('marker') == CONTINUATION and failure.get('result') == 'retained_for_review' and
            failure.get('phase') == 'scan_complete' and failure.get('retry_permitted') is False and
            failure.get('library_id') == CONTINUATION_LIBRARY and failure.get('root_id') == CONTINUATION_ROOT_ID and
            failure.get('job_id') == FINALIZATION_JOB and state.get('phase') == 'continuing_special_features_fixture' and
            state.get('stage') == 'scan_complete' and state.get('process') == completed['candidate']['process'],
            'retained_scan_failure_changed')
    require(all(failure.get('authentication', {}).get(role, {}).get(key) == value for role in ('admin', 'viewer')
                for key, value in (('login_status', 200), ('logout_status', 204), ('exact_status', 401))), 'retained_scan_cleanup_changed')
    proof, scan_proof = completed.get('proof', {}), completed.get('retained_scan_proof', {})
    require(same(scan_proof, documents['retained_structure_proof'].get('proof')) and
            same(setup.get('retained_scan_proof'), scan_proof) and same(inspection.get('retained_scan_proof'), scan_proof) and
            same(setup.get('proof'), proof) and same(inspection.get('proof'), proof) and
            same({key: scan_proof.get(key) for key in ('aggregate_session_count', 'aggregate_audit_count')},
                 {'aggregate_session_count': 3, 'aggregate_audit_count': 9}) and
            same({key: proof.get(key) for key in ('aggregate_session_count', 'aggregate_audit_count', 'finalization_session_count', 'finalization_audit_count')},
                 {'aggregate_session_count': 4, 'aggregate_audit_count': 11, 'finalization_session_count': 1, 'finalization_audit_count': 2}) and
            proof.get('finalizer_session_id') == chain['new_viewer_session_id'] and proof.get('finalizer_device_id') == chain['new_device_id'] and
            proof.get('all_scanned_rows_preserved') is True, 'finalization_stage_proof_changed')
    old, new = retained['database']['tables'], snapshot['database']['tables']
    for name in TABLE_KEYS:
        if name != 'sessions':
            require(same(ordered(old[name]), ordered(new[name])), 'finalization_changed_' + name)
    for name, identifier in (('sessions', chain['new_viewer_session_id']), ('devices', chain['new_device_id'])):
        before, after = keyed(old[name], ('id',)), keyed(new[name], ('id',))
        require(set(after) - set(before) == {(identifier,)} and all(same(row, after.get(key)) for key, row in before.items()),
                'finalization_' + name + '_population_changed')
    auth = next(row for row in new['sessions'] if row['id'] == chain['new_viewer_session_id'])
    device = next(row for row in new['devices'] if row['id'] == chain['new_device_id'])
    require(auth['kind'] == 'emby' and auth['user_id'] == chain['viewer_user_id'] and auth['revoked_at'] is not None and
            auth['device_registry_id'] == device['id'] and auth['device_id'] == device['reported_device_id'] and
            device['last_user_id'] == chain['viewer_user_id'], 'finalization_viewer_identity_changed')
    before_audit, after_audit = keyed(old['activity_entries'], ('id',)), keyed(new['activity_entries'], ('id',))
    added_audit = [row for key, row in after_audit.items() if key not in before_audit]
    require(all(same(row, after_audit.get(key)) for key, row in before_audit.items()) and len(added_audit) == 2 and
            {row['action'] for row in added_audit} == {'session.login', 'session.revoked'} and
            all(row['source'] == 'emby' and row['actor_kind'] == 'user' and row['actor_id'] == chain['viewer_user_id'] and
                row['actor_credential_id'] == auth['id'] and row['resource_kind'] == 'session' and row['resource_id'] == auth['id']
                for row in added_audit), 'finalization_audit_scope_changed')
    return retained, digest(exact(chain).encode())


def profile_contract(ledger, state, completed, setup, inspection):
    require(set(ledger) == {'marker', 'version', 'scope', 'profile', 'profile_report', 'profile_inspection', 'fixture_state'} and
            ledger['marker'] == LEDGER and type(ledger['version']) is int and ledger['version'] == 1 and ledger['scope'] == SCOPE,
            'invalid_expected_ledger')
    inspection_marker = profile_paths(ledger, completed)
    require(completed.get('marker') == PROFILE and completed.get('phase') == 'complete' and
            type(completed.get('schema')) is int and completed['schema'] == 27, 'profile_not_complete')
    require(state.get('marker') == 'goby-m3e-client-acceptance-v1' and state.get('phase') == 'ready' and
            state.get('stage') == 'complete' and type(state.get('schema')) is int and state['schema'] == 27,
            'fixture_not_ready')
    require(state.get('work') == str(WORK) and state.get('cluster') == {'data': '/var/lib/postgresql/goby-workspace-v1/data',
            'port': 15432, 'system_identifier': '7684040109719526738'} and
            all(type(state.get(key)) is int and state[key] > 0 for key in ('database_oid', 'role_oid')), 'fixture_cluster_changed')
    actors = {'A': completed['users']['av_user_id'], 'B': completed['users']['viewer_id']}
    require(len(set(actors.values())) == 2 and all(isinstance(value, str) and ID.fullmatch(value) for value in actors.values()) and
            state.get('viewer_id') == actors['B'] and state.get('added_viewer', {}).get('user_id') == actors['A'] and
            completed['users']['admin_id'] == state.get('admin_id'), 'profile_actor_mismatch')
    item = completed['items']['positive']
    require(ID.fullmatch(item.get('id', '')) and item['id'] == completed['item_ids']['positive'] and
            item.get('type') == item.get('api_type') == 'Movie' and isinstance(item.get('media_source_id'), str) and
            item['media_source_id'] and item.get('path', '').startswith('/opt/goby-fixtures/client-special-features-m3e-v1/Movies/'),
            'profile_target_mismatch')
    candidate = completed.get('candidate', {})
    require(candidate == {'binary_sha256': state['binary_sha256'], 'process': state['process'], 'runtime_sha256': state['runtime_sha256']} and
            HASH.fullmatch(candidate['binary_sha256']) and HASH.fullmatch(candidate['runtime_sha256']), 'profile_candidate_mismatch')
    process = candidate['process']
    require(set(process) == {'pid', 'start_ticks', 'boot_id'} and type(process['pid']) is int and process['pid'] > 1 and
            type(process['start_ticks']) is int and process['start_ticks'] > 0 and
            re.fullmatch(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}', process.get('boot_id', '')),
            'profile_process_identity_invalid')
    profile = state.get('special_features_profile', {})
    require(profile.get('marker') == PROFILE and profile.get('phase') == 'complete' and
            profile.get('receipt_path') == ledger['profile']['path'] and profile.get('receipt_sha256') == ledger['profile']['sha256'] and
            profile.get('library_id') == completed['item_ids']['library'], 'state_profile_binding_mismatch')
    for report, marker in ((setup, 'goby-client-special-features-setup-report-v1'), (inspection, inspection_marker)):
        require(report.get('marker') == marker and report.get('result') == 'passed' and report.get('phase') == 'complete' and
                report.get('state_sha256') == ledger['fixture_state']['sha256'] and
                report.get('receipt_path') == ledger['profile']['path'] and report.get('receipt_sha256') == ledger['profile']['sha256'],
                'profile_report_binding_mismatch')
    require(inspection.get('schema') == 27 and inspection.get('profile_marker') == PROFILE and
            inspection.get('process') == state['process'] and inspection.get('binary_sha256') == state['binary_sha256'] and
            inspection.get('runtime_sha256') == state['runtime_sha256'] and
            all(inspection.get('verification', {}).get(key) is True for key in ('complete_schema27', 'exact_nonempty_profile',
                'setup_old_rows_preserved', 'current_matches_completed', 'credentials_recovery_media_preserved', 'new_setup_sessions_revoked')),
            'inspection_not_complete')
    return actors, item


def authority(record, *, live=False):
    ledger = read_record(record)
    state, completed, setup, inspection = (read_record(ledger[key]) for key in
                                           ('fixture_state', 'profile', 'profile_report', 'profile_inspection'))
    actors, item = profile_contract(ledger, state, completed, setup, inspection)
    # Link the immutable upgrade to the subsequent root extension, not to its old PID.
    histories = {}
    for name in ('upgrade', 'extension'):
        binding = completed[name]
        histories[name] = read_record({'path': binding['completed_path'], 'sha256': binding['completed_sha256']})
        read_record({'path': binding['report_path'], 'sha256': binding['report_sha256']})
        require(histories[name].get('phase') == 'complete' and
                all(same(histories[name].get(key), binding[key]) for key in ('old_process', 'new_process')),
                'history_process_binding_mismatch')
    upgrade, extension = histories['upgrade'], histories['extension']
    require(same(upgrade, state.get('upgrade')) and upgrade.get('from_schema') == 26 and upgrade.get('to_schema') == 27 and
            upgrade.get('to_sha256') == upgrade.get('installed_binary_sha256') == state['binary_sha256'] and
            extension.get('marker') == 'goby-client-special-features-root-extension-v1' and extension.get('schema') == 27 and
            extension.get('old_process') == upgrade['new_process'] and extension.get('new_process') == state['process'] and
            extension.get('new_runtime_sha256') == state['runtime_sha256'] and extension.get('binary_sha256') == state['binary_sha256'] and
            extension.get('upgrade_completed_sha256') == completed['upgrade']['completed_sha256'] and
            extension.get('upgrade_report_sha256') == completed['upgrade']['report_sha256'] and
            all(extension.get(key) is True for key in ('old_database_rows_and_sequences_preserved',
                'credentials_recovery_and_all_media_preserved', 'other_runtime_bytes_preserved')), 'upgrade_extension_chain_changed')
    require(state.get('media_root_extension', {}).get('phase') == 'complete' and
            state['media_root_extension'].get('receipt_path') == completed['extension']['completed_path'] and
            state['media_root_extension'].get('receipt_sha256') == completed['extension']['completed_sha256'] and
            all(completed['extension'][key] == extension[key] for key in ('old_runtime_sha256', 'new_runtime_sha256')) and
            extension.get('old_runtime_sha256') != extension.get('new_runtime_sha256'), 'extension_state_binding_changed')
    binding = state['schema27_source']
    require(same(binding, upgrade['schema_artifacts']['schema27_binding']) and binding.get('schema') == 27 and
            extension.get('source_manifest_sha256') == binding['source_manifest_sha256'] and
            upgrade['schema_artifacts']['operator'] == {'path': str(WORK / 'prepare-client-fixture.py'), 'sha256': OPERATOR_SHA},
            'product_source_binding_changed')
    after_pin = completed['after_snapshot']
    declared = completed['after_ledger']
    snapshot = read_record(after_pin)
    inspected = read_record({'path': inspection['current_snapshot_path'], 'sha256': inspection['current_snapshot_sha256']})
    require(snapshot.get('schema') == inspected.get('schema') == 27 and len(snapshot['database']['tables']) == 35 and
            snapshot['database'].get('unsupported') is False and inspected['database'].get('unsupported') is False,
            'incomplete_profile_snapshot')
    baseline = projection(snapshot['database']['tables'], actors, item['id'])
    require(same(baseline, projection(inspected['database']['tables'], actors, item['id'])), 'inspection_ledger_drift')
    setup_ledger_contract(declared, after_pin, actors, baseline, snapshot['database']['tables'])
    finalization_sha256 = None
    continuation_snapshot = snapshot
    if 'finalization' in completed:
        continuation_snapshot, finalization_sha256 = read_finalization(completed, setup, inspection, snapshot)
    continuation_sha256 = read_continuation(completed, setup, inspection, continuation_snapshot) if 'continuation' in completed else None
    ordinary_movie(baseline, item['id'])
    actual = baseline['movie_rows'][0]
    require(all(actual[key] == item[key] for key in ('id', 'type', 'name', 'path', 'relative_path', 'parent_id')) and
            actual['library_id'] == completed['item_ids']['library'] and actual['root_id'] == state['special_features_profile']['root_id'],
            'profile_movie_row_mismatch')
    result = {'scope': SCOPE, 'expected_ledger_sha256': record['sha256'], 'actors': actors,
              'movie': {key: item[key] for key in ('id', 'path', 'media_source_id')},
              'profile': {key: ledger[source][field] for key, source, field in
                 (('receipt_path', 'profile', 'path'), ('receipt_sha256', 'profile', 'sha256'),
                  ('report_path', 'profile_report', 'path'), ('report_sha256', 'profile_report', 'sha256'),
                  ('inspect_path', 'profile_inspection', 'path'), ('inspect_sha256', 'profile_inspection', 'sha256'),
                  ('state_sha256', 'fixture_state', 'sha256'))},
              'process': state['process'], 'binary_sha256': state['binary_sha256'], 'runtime_sha256': state['runtime_sha256'],
              'source': binding['source'], 'source_manifest_sha256': binding['source_manifest_sha256'],
              'upgrade_sha256': completed['upgrade']['completed_sha256'], 'extension_sha256': completed['extension']['completed_sha256'],
              'database_oid': state['database_oid'], 'role_oid': state['role_oid'],
              'baseline_sha256': after_pin['sha256'], 'baseline_projection_sha256': digest(exact(baseline).encode()),
              'baseline_counts': declared['counts']}
    if continuation_sha256 is not None:
        result['continuation_sha256'] = continuation_sha256
    if finalization_sha256 is not None:
        result['finalization_sha256'] = finalization_sha256
    if live:
        raw = read_record({'path': str(OPERATOR), 'sha256': OPERATOR_SHA}, modes=(0o600, 0o644, 0o700, 0o755), raw=True)
        module = types.ModuleType('positive_profile_readonly_service_proof')
        module.__file__ = str(OPERATOR)
        exec(compile(raw, str(OPERATOR), 'exec'), module.__dict__)
        product = upgrade['schema_artifacts']['product_verification']
        require(module.verify_product_upgrade(Path(upgrade['source']), state['binary_sha256'], Path(binding['source']),
                binding['source_manifest_sha256'], Path(product['report_path']), product['report_sha256'], target=27) == product,
                'live_product_proof_changed')
        require(module.verify_service(state) == state['process'], 'live_candidate_process_changed')
    return result, baseline


def expected_identity(proof):
    return {'database': 'goby_client_m3e', 'port': 15432, 'readonly': 'on', 'postgresql': 170011,
            'schema': 27, 'system_identifier': '7684040109719526738', 'database_oid': proof['database_oid'],
            'role_oid': proof['role_oid'], 'database_owner_oid': proof['role_oid']}


def observation_rows(document, phase, proof):
    require(document.get('marker') == MARKER and type(document.get('version')) is int and document['version'] == 1 and
            document.get('preparation_scope') == SCOPE and document.get('phase') == phase and document.get('complete') is True and
            same(document.get('authority'), proof) and same(document.get('identity'), expected_identity(proof)),
            'observation_identity_changed')
    rows = document['rows']
    require(set(rows) == {*TABLE_KEYS, 'users_state', 'user_settings', 'movie_rows', *AUXILIARY}, 'observation_sections_changed')
    tables = {name: keyed(rows[name], key) for name, key in TABLE_KEYS.items()}
    require(same(document['counts'], {name: len(table) for name, table in tables.items()}) and
            all(row.get('user_id') in proof['actors'].values() for table in tables.values() for row in table.values()),
            'observation_population_changed')
    require(document.get('foreign_dependents') == {'references': 0, 'encodings': 0}, 'foreign_dependents_present')
    users = keyed(rows['users_state'], ('id',))
    require(set(users) == {(value,) for value in proof['actors'].values()} and all(
            set(row) == set(USER_FIELDS) and row['is_disabled'] is False and row['is_administrator'] is False and
            row['policy'].get('EnableAllFolders', True) is True and row['policy'].get('EnableMediaPlayback', True) is True
            for row in users.values()), 'ordinary_actor_authority_changed')
    ordinary_movie(rows, proof['movie']['id'])
    return tables


def eligible_expiry(document, proof):
    """Refuse other cleanup effects before authorizing this bounded UI scope."""
    tables = observation_rows(document, 'before', proof)
    now = instant(document['observed_at'])
    require(tables['encoding_jobs'] == {}, 'encoding_activity_outside_scope')
    allowed = []
    for row in tables['play_sessions'].values():
        require(row['state'] in ('Prepared', 'Playing', 'Paused', 'Stopped', 'Expired'), 'unknown_play_state')
        require(instant(row['expires_at']) >= now - dt.timedelta(days=7) + dt.timedelta(minutes=30), 'old_play_pruning_risk')
        if row['state'] in ('Stopped', 'Expired'):
            continue
        auth = tables['sessions'].get((row['auth_session_id'],), {})
        require(row['state'] == 'Prepared' and row['started_at'] is None and row['stopped_at'] is None and
                auth.get('user_id') == row['user_id'] and auth.get('revoked_at') is not None and
                instant(auth['revoked_at']) <= now, 'unreviewed_active_play_cleanup')
        allowed.append(digest(row['id'].encode()))
    for user_id in proof['actors'].values():
        require(sum(row['user_id'] == user_id for row in tables['play_sessions'].values()) <= 254, 'play_pruning_population_risk')
    for row in tables['client_playback_references'].values():
        auth = tables['sessions'].get((row['auth_session_id'],), {})
        require(auth.get('user_id') == row['user_id'] and auth.get('revoked_at') is None and
                instant(auth['expires_at']) > now + dt.timedelta(minutes=30), 'old_reference_pruning_risk')
    return sorted(allowed)


def statement(proof, phase):
    require(phase in ('before', 'after') and set(proof['actors']) == {'A', 'B'} and
            all(ID.fullmatch(value) for value in (*proof['actors'].values(), proof['movie']['id'])), 'invalid_sql_identifiers')
    users = ','.join("'" + value + "'" for value in proof['actors'].values())
    movie = proof['movie']['id']
    sections = []
    for name in TABLE_KEYS:
        if name == 'sessions':
            query = "SELECT " + ','.join(AUTH_FIELDS) + ",encode(token_hash,'hex') AS token_fingerprint_sha256 FROM public.sessions"
        else:
            query = 'SELECT * FROM public.' + name
        sections.append("'" + name + "',(SELECT COALESCE(jsonb_agg(to_jsonb(v)),'[]'::jsonb) FROM (" + query +
                        ' WHERE user_id IN (' + users + ')) v)')
    sections.append("'users_state',(SELECT jsonb_agg(to_jsonb(v)) FROM (SELECT " + ','.join(USER_FIELDS) +
                    ' FROM public.users WHERE id IN (' + users + ')) v)')
    sections.append("'user_settings',(SELECT COALESCE(jsonb_agg(to_jsonb(v)),'[]'::jsonb) FROM public.user_settings v WHERE user_id IN (" + users + '))')
    sections.append("'movie_rows',(SELECT COALESCE(jsonb_agg(to_jsonb(v)),'[]'::jsonb) FROM public.items v WHERE id='" + movie + "')")
    sections.extend("'" + name + "',(SELECT COALESCE(jsonb_agg(to_jsonb(v)),'[]'::jsonb) FROM public." + name + ' v)' for name in AUXILIARY)
    # Every query is in this one repeatable-read snapshot. The final byte cap is
    # independent of the row bounds; no token or credential plaintext is selected.
    return ("""BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='10s';
SET LOCAL lock_timeout='2s';
SET LOCAL search_path=pg_catalog,public;
SELECT jsonb_build_object('observed_at',transaction_timestamp(),
 'identity',jsonb_build_object('database',current_database(),'port',current_setting('port')::int,
 'readonly',current_setting('transaction_read_only'),'postgresql',current_setting('server_version_num')::int,
 'schema',(SELECT max(version) FROM public.schema_migrations),
 'system_identifier',(SELECT system_identifier::text FROM pg_control_system()),
 'database_oid',(SELECT oid::bigint FROM pg_database WHERE datname=current_database()),
 'role_oid',(SELECT oid::bigint FROM pg_roles WHERE rolname='goby_client_m3e'),
 'database_owner_oid',(SELECT datdba::bigint FROM pg_database WHERE datname=current_database())),
 'foreign_dependents',jsonb_build_object(
 'references',(SELECT count(*) FROM public.client_playback_references r JOIN public.play_sessions p ON p.id=r.play_session_id
 WHERE p.user_id IN (__USERS__) AND r.user_id NOT IN (__USERS__)),
 'encodings',(SELECT count(*) FROM public.encoding_jobs e JOIN public.play_sessions p ON p.id=e.play_session_id
 WHERE p.user_id IN (__USERS__) AND (e.user_id IS NULL OR e.user_id NOT IN (__USERS__)))),
 'rows',jsonb_build_object(__SECTIONS__));
ROLLBACK;
""".replace('__USERS__', users).replace('__SECTIONS__', ','.join(sections))).encode()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('phase', choices=('before', 'after'))
    parser.add_argument('--preparation-scope', required=True, choices=(SCOPE,))
    parser.add_argument('--expected-ledger', required=True, type=Path)
    parser.add_argument('--expected-ledger-sha256', required=True)
    parser.add_argument('--output-root', required=True, type=Path)
    parser.add_argument('--before-sha256')
    args = parser.parse_args()
    require(sys.platform == 'linux' and os.getuid() == os.geteuid() == os.getgid() == os.getegid() == 0 and
            os.environ.get('SSH_CONNECTION'), 'authorized_remote_root_required')
    record = {'path': str(args.expected_ledger), 'sha256': args.expected_ledger_sha256}
    proof, baseline = authority(record, live=True)
    root = args.output_root
    require(root.parent == WORK and re.fullmatch(r'positive-prepare-observation-[a-z0-9][a-z0-9_-]{0,63}', root.name), 'output_namespace_mismatch')
    owner = (exact({'marker': MARKER, 'scope': SCOPE, 'expected_ledger': record, 'path': str(root)}) + '\n').encode()
    directory(WORK, 0o700)
    if args.phase == 'before':
        require(args.before_sha256 is None, 'unexpected_before_pin')
        root.mkdir(mode=0o700)
        write_private(root / 'OWNER.json', owner)
    else:
        directory(root, 0o700)
        require(read_record({'path': str(root / 'OWNER.json'), 'sha256': digest(owner)}, raw=True) == owner, 'observation_owner_changed')
        before = read_record({'path': str(root / 'before.json'), 'sha256': args.before_sha256})
        observation_rows(before, 'before', proof)
        require(same(before['rows'], baseline) and same(before.get('approved_expiry_id_sha256'), eligible_expiry(before, proof)),
                'before_observation_changed')
    require(not os.path.lexists(root / (args.phase + '.json')), 'observation_already_exists')
    result = subprocess.run(CONNECTION, input=statement(proof, args.phase), stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=25,
                            env={'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8',
                                 'PGOPTIONS': '-c default_transaction_read_only=on', 'PGAPPNAME': 'goby-positive-prepare-observation'})
    require(result.returncode == 0 and 0 < len(result.stdout) <= MAX_BYTES, 'readonly_observation_failed')
    document = decode(result.stdout.strip())
    document['rows'] = {name: ordered(rows) for name, rows in document['rows'].items()}
    require(all(len(rows) <= (2048 if name == 'sessions' else 512) for name, rows in document['rows'].items()), 'observation_row_bound_exceeded')
    document.update(marker=MARKER, version=1, phase=args.phase, preparation_scope=SCOPE, complete=True, authority=proof,
                    counts={name: len(document['rows'][name]) for name in TABLE_KEYS})
    observation_rows(document, args.phase, proof)
    if args.phase == 'before':
        require(same(document['rows'], baseline), 'fresh_profile_baseline_changed')
        document['approved_expiry_id_sha256'] = eligible_expiry(document, proof)
    current, _ = authority(record, live=True)
    require(same(current, proof), 'authority_changed_during_observation')
    raw = (exact(document) + '\n').encode()
    write_private(root / (args.phase + '.json'), raw)
    print(json.dumps({'result': 'observed', 'scope': SCOPE, 'phase': args.phase, 'counts': document['counts'],
                      'report_sha256': digest(raw), 'expected_ledger_sha256': record['sha256'],
                      'database_mutations': 0, 'http_requests': 0}, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(json.dumps({'result': 'failed', 'reason_code': str(error) if isinstance(error, ScopeError) else 'input_or_read_error',
                          'database_mutations': 0, 'http_requests': 0}, sort_keys=True))
        raise SystemExit(1)
