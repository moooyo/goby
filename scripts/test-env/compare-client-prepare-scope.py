#!/usr/bin/env python3
"""Compare private candidate preparation observations with one pinned UI report.

This offline comparator only reads explicitly pinned observation files. It does not
connect to PostgreSQL, send HTTP, modify observations, or turn a database-scope
result into UI acceptance. Exact numeric JSON is retained during comparisons.
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
import sys

WORK = Path('/opt/goby-test/exec-work-m3e')
MARKER = 'goby-client-prepare-scope-observation-v1'
ACTORS = {'A': '34b4c24f6568659af7ce17938fae7f81', 'B': 'ecbbe4cb82403879bc4b4f78894c5738'}
MOVIE = '268051d3ca734aefcf94e245fb25ad55'
SOURCE = 'mediasource_' + MOVIE
EXPIRED_PLAY_SHA = 'cd995d310e36a7df473db87ace16eca87d261cf1df63e0f6f124f3fd1d63966a'
REVOKED_REFERENCE_SHA = '9709926f1bfcdbe5a6f96c201f8dee12bf246e80203b5644ca38b9547093346c'
REFERENCE_PLAY_SHA = 'b94d23d26eb858fd9b1d945fd3d734efa4bae384af11039cadd886aa92cdc0c9'
PAGE_ERROR_SCOPE = 'source28-page-error-01'
PAGE_ERROR_BASELINE_SHA = '4aff79e9798fcab89652a5bdc3b83b408cdc67a1a52b8d086a6ff99c8e3f6a63'
PAGE_ERROR_EXPIRED_SHAS = {
    'be39a4afd1ce9e19c6d7ff1cf0666031839dd4d9f082733947a463d9e163d134',
    'c0ec1dbf6701d6a42c06f5843353d875e07f75613ca0f92a4d6905fc5818ed09',
}
REFERENCE_FIELDS = {'user_id', 'auth_session_id', 'device_id', 'client_nonce', 'play_session_id',
                    'created_at', 'application_client_id'}
HASH = re.compile(r'[0-9a-f]{64}')
EXPECTED_IDENTITY = {'database': 'goby_client_m3e', 'port': 15432, 'readonly': 'on', 'postgresql': 170011,
                     'schema': 26, 'system_identifier': '7684040109719526738'}
TABLE_KEYS = {'play_sessions': ('id',), 'client_playback_references':
              ('user_id', 'auth_session_id', 'application_client_id', 'device_id', 'client_nonce'),
              'user_item_data': ('user_id', 'item_id'), 'sessions': ('id',), 'encoding_jobs': ('id',)}


class ScopeError(Exception):
    pass


def require(condition, code):
    if not condition:
        raise ScopeError(code)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def id_digest(value):
    require(isinstance(value, str) and value, 'invalid_identifier')
    return digest(value.encode())


def exact(value):
    if value is None:
        return 'null'
    if type(value) is bool:
        return 'true' if value else 'false'
    if type(value) is int:
        return str(value)
    if isinstance(value, Decimal):
        require(value.is_finite(), 'invalid_json_number')
        return str(value)
    if isinstance(value, str):
        return json.dumps(value, ensure_ascii=False)
    if isinstance(value, list):
        return '[' + ','.join(exact(item) for item in value) + ']'
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


def read_pinned(path, expected):
    require(isinstance(expected, str) and HASH.fullmatch(expected) and path.is_absolute() and
            WORK in path.parents and '..' not in path.parts, 'invalid_input_pin')
    for parent in reversed(path.parents):
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                'untrusted_input_directory')
    before = path.lstat()
    require(stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and before.st_nlink == 1 and
            stat.S_IMODE(before.st_mode) == 0o600 and 0 < before.st_size <= 16 << 20, 'untrusted_input_file')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as stream:
        opened = os.fstat(stream.fileno())
        require((opened.st_dev, opened.st_ino) == (before.st_dev, before.st_ino), 'input_changed_during_open')
        raw = stream.read((16 << 20) + 1)
    after = path.lstat()
    require((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) ==
            (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns) and
            len(raw) == before.st_size and digest(raw) == expected, 'input_digest_or_identity_changed')
    return json.loads(raw, object_pairs_hook=unique_object, parse_float=Decimal,
                      parse_constant=lambda _: (_ for _ in ()).throw(ScopeError('invalid_json_number')))


def instant(value):
    require(isinstance(value, str), 'missing_timestamp')
    try:
        result = dt.datetime.fromisoformat(value.replace('Z', '+00:00'))
    except ValueError:
        raise ScopeError('invalid_timestamp') from None
    require(result.tzinfo is not None, 'unqualified_timestamp')
    return result


def in_window(value, lower, upper):
    return lower <= instant(value) <= upper


def keyed(rows, fields):
    require(isinstance(rows, list), 'missing_complete_rows')
    result = {}
    for row in rows:
        require(isinstance(row, dict) and all(field in row for field in fields), 'invalid_row_key')
        key = tuple(row[field] for field in fields)
        require(key not in result, 'duplicate_row_key')
        result[key] = row
    return result


def without(row, fields):
    return {key: value for key, value in row.items() if key not in fields}


def reference_digest(row):
    # PostgreSQL jsonb_build_array(... )::text uses comma-space separators.
    values = [row[name] for name in ('user_id', 'auth_session_id', 'device_id', 'client_nonce', 'application_client_id')]
    require(all(value is None or isinstance(value, str) for value in values), 'invalid_reference_key')
    return digest(json.dumps(values, ensure_ascii=False, separators=(', ', ': ')).encode())


def observe_shape(value, phase):
    require(value.get('marker') == MARKER and type(value.get('version')) is int and value['version'] == 1 and
            value.get('phase') == phase and value.get('complete') is True and same(value.get('identity'), EXPECTED_IDENTITY),
            'observation_identity_or_completeness_mismatch')
    require(value.get('foreign_dependents') == {'references': 0, 'encodings': 0}, 'foreign_dependents_present')
    actors = keyed(value.get('actors'), ('label',))
    require(set(actors) == {(slot,) for slot in ACTORS} and all(actors[(slot,)]['user_id'] == identifier
            for slot, identifier in ACTORS.items()), 'observation_actor_mismatch')
    require(all(row.get('exists') is True and row.get('disabled') is False and row.get('administrator') is False and
                row.get('all_folders') is True and row.get('playback_allowed') is True and
                row.get('movie_userdata_exists') is True for row in actors.values()), 'observation_actor_scope_changed')
    movie = value.get('movie', {})
    require(movie.get('id') == MOVIE and movie.get('exists') is True and movie.get('type') == 'Movie' and
            movie.get('folder') is False and movie.get('has_media') is True and
            movie.get('has_theme_association') is False and movie.get('reserved') is False, 'observation_movie_mismatch')
    tables = {name: keyed(value.get(name), fields) for name, fields in TABLE_KEYS.items()}
    require(set(value.get('counts', {})) == set(TABLE_KEYS) and all(type(value['counts'][name]) is int and
            value['counts'][name] == len(rows) for name, rows in tables.items()), 'observation_row_count_mismatch')
    require(all(row.get('user_id') in ACTORS.values() for rows in tables.values() for row in rows.values()), 'foreign_actor_row')
    return tables


def compare(before, after, report, scope='original', baseline_after=None):
    require(scope in ('original', PAGE_ERROR_SCOPE), 'unsupported_preparation_scope')
    page_error_scope = scope == PAGE_ERROR_SCOPE
    old, new = observe_shape(before, 'before'), observe_shape(after, 'after')
    lower, upper = instant(before['observed_at']), instant(after['observed_at'])
    require(lower <= upper and same(before['actors'], after['actors']) and same(before['movie'], after['movie']),
            'actor_movie_or_window_changed')
    expected_counts = {'play_sessions': 20 if page_error_scope else 18,
                       'client_playback_references': 0 if page_error_scope else 1,
                       'user_item_data': 5, 'sessions': 49 if page_error_scope else 47, 'encoding_jobs': 0}
    require(before['counts'] == expected_counts, 'unreviewed_before_population')
    if page_error_scope:
        require(isinstance(baseline_after, dict), 'reviewed_baseline_after_required')
        observe_shape(baseline_after, 'after')
        require(instant(baseline_after['observed_at']) <= lower and
                same(without(before, {'phase', 'observed_at'}), without(baseline_after, {'phase', 'observed_at'})),
                'reviewed_baseline_after_rows_changed')
    else:
        require(baseline_after is None, 'unexpected_baseline_after')
    require(report.get('marker') == 'goby-client-cross-user-m3e-v1' and report.get('format') == 5 and
            report.get('mode') == 'acceptance-preparation' and report.get('result') in ('passed', 'failed') and
            type(report.get('client_acceptance')) is bool and
            lower <= instant(report['started_at']) <= instant(report['finished_at']) <= upper,
            'ui_report_identity_or_window_mismatch')
    require(report.get('preparation_scope') == (PAGE_ERROR_SCOPE if page_error_scope else None), 'ui_preparation_scope_mismatch')
    movie_rows = [row for row in report.get('owned_items', []) if row.get('key') == 'movie']
    require(len(movie_rows) == 1 and movie_rows[0].get('id') == MOVIE and movie_rows[0].get('type') == 'Movie', 'ui_movie_mismatch')
    accounts = keyed(report.get('accounts'), ('slot',))
    require(set(accounts) == {(slot,) for slot in ACTORS}, 'ui_actor_population_mismatch')
    for name in ('user_item_data', 'encoding_jobs'):
        require(set(old[name]) == set(new[name]) and all(same(row, new[name][key]) for key, row in old[name].items()),
                name + '_changed')
    for key, row in old['sessions'].items():
        require(key in new['sessions'] and same(row, new['sessions'][key]), 'old_auth_changed_or_deleted')
    fresh_auth = {key: row for key, row in new['sessions'].items() if key not in old['sessions']}
    require(len(fresh_auth) == 2, 'unexpected_new_auth_count')
    old_fingerprints = {row['token_fingerprint_sha256'] for row in old['sessions'].values()}
    auth_by_actor, accepted_plays = {}, {}
    for slot, identifier in ACTORS.items():
        actor = accounts[(slot,)]
        fingerprint = actor.get('token_fingerprint')
        require(actor.get('id') == identifier and isinstance(fingerprint, str) and HASH.fullmatch(fingerprint) and
                fingerprint not in old_fingerprints and actor.get('principal_confirmed') is True and
                actor.get('ordinary_authority_confirmed') is True and actor.get('login', {}).get('status') == 200 and
                actor['login'].get('request_count') == 1, 'fresh_ui_authority_unproven')
        matches = [row for row in fresh_auth.values() if row.get('user_id') == identifier and row.get('token_fingerprint_sha256') == fingerprint]
        require(len(matches) == 1, 'new_auth_fingerprint_mismatch')
        auth = matches[0]
        require(set(auth) == set(next(iter(old['sessions'].values()))) and
                auth.get('kind') == 'emby' and isinstance(auth.get('device_id'), str) and auth['device_id'] and
                in_window(auth['created_at'], lower, upper) and
                in_window(auth['last_seen_at'], instant(auth['created_at']), upper) and
                in_window(auth['revoked_at'], instant(auth['created_at']), upper) and instant(auth['expires_at']) > upper,
                'new_auth_lifecycle_mismatch')
        proof, proxy = actor.get('session_proof', {}), actor.get('proxy_logout', {})
        entries = proof.get('entries', [])
        require(actor.get('logout', {}).get('status') == 204 and actor.get('closed') is True and
                proxy.get('completed') is True and proxy.get('status') == 204 and proxy.get('token_fingerprint') == fingerprint and
                proof.get('outcome') == 'all_observed_logout_tokens_rejected' and len(entries) == 1 and
                entries[0].get('token_fingerprint') == fingerprint and entries[0].get('ui_request', {}).get('response_status') == 204 and
                entries[0].get('verification', {}).get('status') == 401, 'new_auth_logout_unproven')
        auth_by_actor[identifier] = auth
        source, preparation = actor.get('preparation_source', {}), actor.get('preparation', {})
        require(source.get('item_id') == MOVIE and source.get('source_id') == SOURCE, 'preparation_source_mismatch')
        if preparation:
            response = preparation.get('response', {})
            require(preparation.get('request_validated') is True and preparation.get('completed') is True and
                    preparation.get('existing_play_or_live_session_requested') is False and
                    preparation.get('item_id') == MOVIE and preparation.get('source_id') == SOURCE and
                    preparation.get('user_id') == identifier and preparation.get('token_fingerprint') == fingerprint and
                    actor.get('proxy', {}).get('preparation') == 1 and response.get('status') == 200 and
                    response.get('validated') is True and re.fullmatch(r'play_[0-9a-f]{32}', response.get('play_session_id', '')),
                    'preparation_response_unproven')
            accepted_plays[identifier] = response['play_session_id']
        else:
            require(actor.get('proxy', {}).get('preparation', 0) == 0, 'unacknowledged_preparation')
    require(len({row['token_fingerprint_sha256'] for row in auth_by_actor.values()}) == 2, 'shared_ui_authentication')
    if page_error_scope:
        require(len(accepted_plays) == 2, 'page_error_scope_requires_two_preparations')
    changed_old = []
    expired_shas = PAGE_ERROR_EXPIRED_SHAS if page_error_scope else {EXPIRED_PLAY_SHA}
    approved = [row for row in old['play_sessions'].values() if id_digest(row['id']) in expired_shas]
    expected_owners = set(ACTORS.values()) if page_error_scope else {ACTORS['B']}
    require(len(approved) == len(expired_shas) and {id_digest(row['id']) for row in approved} == expired_shas and
            {row['user_id'] for row in approved} == expected_owners and
            all(row['state'] == 'Prepared' and row['item_id'] == MOVIE and row['media_source_id'] == SOURCE and
                (page_error_scope or instant(row['expires_at']) <= lower) and
                old['sessions'][(row['auth_session_id'],)]['user_id'] == row['user_id'] and
                old['sessions'][(row['auth_session_id'],)]['revoked_at'] is not None and
                instant(old['sessions'][(row['auth_session_id'],)]['revoked_at']) <= lower for row in approved),
            'reviewed_expired_play_missing')
    for key, row in old['play_sessions'].items():
        require(key in new['play_sessions'], 'old_play_deleted')
        current = new['play_sessions'][key]
        if same(row, current):
            continue
        require(id_digest(row['id']) in expired_shas and set(row) == set(current) and current['state'] == 'Expired' and
                same(without(row, {'state', 'stopped_at', 'updated_at'}), without(current, {'state', 'stopped_at', 'updated_at'})) and
                in_window(current['updated_at'], max(lower, instant(row['updated_at'])), upper) and
                (current['stopped_at'] == row['stopped_at'] if row['stopped_at'] is not None else in_window(current['stopped_at'], lower, upper)),
                'unapproved_old_play_change')
        changed_old.append(id_digest(row['id']))
    added = [row for key, row in new['play_sessions'].items() if key not in old['play_sessions']]
    require(len(added) == len(accepted_plays) and len({row['user_id'] for row in added}) == len(added), 'unexpected_new_play_count')
    play_keys = set(next(iter(old['play_sessions'].values())))
    for row in added:
        auth = auth_by_actor[row['user_id']]
        require(set(row) == play_keys and row['id'] == accepted_plays.get(row['user_id']) and row['auth_session_id'] == auth['id'] and
                row['device_id'] == auth['device_id'] and row['item_id'] == MOVIE and row['media_source_id'] == SOURCE and
                row['state'] == 'Prepared' and row['started_at'] is None and row['stopped_at'] is None and
                row['counted'] is False and row['client_correlated'] is False and row['application_client_id'] is None and
                same(row['player_state'], {}) and
                in_window(row['created_at'], instant(auth['created_at']), upper) and
                in_window(row['updated_at'], instant(row['created_at']), instant(auth['revoked_at'])) and
                lower + dt.timedelta(minutes=30) <= instant(row['expires_at']) <= upper + dt.timedelta(minutes=30),
                'new_play_scope_or_state_mismatch')
        require(type(row['position_ticks']) is int and type(row['duration_ticks']) is int and
                0 <= row['position_ticks'] <= row['duration_ticks'], 'new_play_position_invalid')
        data = old['user_item_data'][(row['user_id'], MOVIE)]
        require(row['position_ticks'] == min(data['playback_position_ticks'], row['duration_ticks']), 'new_play_resume_position_mismatch')
    removed_refs = 0
    for key, row in old['client_playback_references'].items():
        require(reference_digest(row) == REVOKED_REFERENCE_SHA and row['user_id'] == ACTORS['A'] and
                id_digest(row['play_session_id']) == REFERENCE_PLAY_SHA and
                old['sessions'][(row['auth_session_id'],)]['revoked_at'] is not None, 'reviewed_revoked_reference_missing')
        if key in new['client_playback_references']:
            require(same(row, new['client_playback_references'][key]), 'old_reference_changed')
        else:
            removed_refs += 1
    fresh_refs = [row for key, row in new['client_playback_references'].items() if key not in old['client_playback_references']]
    require(len(fresh_refs) <= len(added) and len({row['play_session_id'] for row in fresh_refs}) == len(fresh_refs), 'unexpected_new_reference_count')
    for row in fresh_refs:
        auth = auth_by_actor[row['user_id']]
        require(set(row) == REFERENCE_FIELDS and
                row['auth_session_id'] == auth['id'] and row['device_id'] == auth['device_id'] and
                row['application_client_id'] is None and row['play_session_id'] == accepted_plays.get(row['user_id']) and
                in_window(row['created_at'], instant(auth['created_at']), instant(auth['revoked_at'])), 'new_reference_scope_mismatch')
    return {'old_play_rows_retained': expected_counts['play_sessions'],
            'old_play_rows_unchanged': expected_counts['play_sessions'] - len(changed_old), 'expired_old_play_count': len(changed_old),
            'expired_old_play_id_hashes': sorted(changed_old),
            'removed_revoked_reference_count': removed_refs, 'new_play_count': len(added), 'new_auth_count': 2,
            'new_reference_count': len(fresh_refs), 'userdata_rows_unchanged': 5, 'encoding_rows_unchanged': 0,
            'old_auth_rows_unchanged': expected_counts['sessions'], 'new_play_id_hashes': sorted(id_digest(row['id']) for row in added),
            'new_auth_id_hashes': sorted(id_digest(row['id']) for row in fresh_auth.values())}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('before', 'after', 'report'):
        parser.add_argument('--' + name, type=Path, required=True)
        parser.add_argument('--' + name + '-sha256', required=True)
    parser.add_argument('--preparation-scope', choices=('original', PAGE_ERROR_SCOPE), default='original')
    parser.add_argument('--baseline-after', type=Path)
    parser.add_argument('--baseline-after-sha256')
    args = parser.parse_args()
    summary = {'marker': 'goby-client-prepare-scope-comparison-v1', 'database_scope_result': 'failed',
               'preparation_scope': args.preparation_scope,
               'database_wide_preservation_claimed': False, 'ui_result_promoted': False, 'reason_codes': [],
               'input_sha256': {name: getattr(args, name + '_sha256') if HASH.fullmatch(getattr(args, name + '_sha256')) else None
                                for name in ('before', 'after', 'report')}}
    try:
        require(sys.platform == 'linux' and os.getuid() == os.geteuid() == os.getgid() == os.getegid() == 0,
                'authorized_remote_root_required')
        require(args.before.name == 'before.json' and args.after.name == 'after.json' and
                args.before.parent == args.after.parent and args.report.name == 'report.json', 'input_roles_mismatch')
        before = read_pinned(args.before, args.before_sha256)
        after = read_pinned(args.after, args.after_sha256)
        report = read_pinned(args.report, args.report_sha256)
        baseline_after = None
        if args.preparation_scope == PAGE_ERROR_SCOPE:
            require(args.baseline_after is not None and args.baseline_after.name == 'after.json' and
                    args.baseline_after not in (args.before, args.after) and args.baseline_after_sha256 == PAGE_ERROR_BASELINE_SHA,
                    'reviewed_baseline_after_pin_required')
            baseline_after = read_pinned(args.baseline_after, PAGE_ERROR_BASELINE_SHA)
            summary['input_sha256']['baseline_after'] = PAGE_ERROR_BASELINE_SHA
        else:
            require(args.baseline_after is None and args.baseline_after_sha256 is None, 'unexpected_baseline_after')
        require(report.get('marker') == 'goby-client-cross-user-m3e-v1' and report.get('result') in ('passed', 'failed') and
                type(report.get('client_acceptance')) is bool, 'invalid_ui_report_header')
        failure = report.get('failure')
        summary.update(ui_report_result=report.get('result'), ui_client_acceptance=report.get('client_acceptance'),
                       ui_failure_sha256=digest(exact(failure).encode()))
        if failure is None or isinstance(failure, str) and re.fullmatch(r'[A-Za-z0-9_-]{1,160}', failure):
            summary['ui_report_failure'] = failure
        summary['counts_and_hashes'] = compare(before, after, report, args.preparation_scope, baseline_after)
        summary['database_scope_result'] = 'passed'
    except ScopeError as error:
        summary['reason_codes'] = [str(error)]
    except Exception:
        summary['reason_codes'] = ['input_shape_or_read_error']
    print(json.dumps(summary, sort_keys=True))
    return 0 if summary['database_scope_result'] == 'passed' else 1


if __name__ == '__main__':
    raise SystemExit(main())
