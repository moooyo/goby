#!/usr/bin/env python3
"""Compare the positive Movie UI ledger offline without promoting UI acceptance.

The sibling observer is an explicitly pinned tool input. Its offline authority
reader validates retained receipts only; no SQL, HTTP or process probe is run.
"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import sys
import types

SCOPE = 'schema27-positive-special-features-01'
WORK = Path('/opt/goby-test/exec-work-m3e')
PLAY_FIELDS = {'id', 'user_id', 'auth_session_id', 'device_id', 'item_id', 'media_source_id', 'state',
               'position_ticks', 'duration_ticks', 'counted', 'created_at', 'updated_at', 'expires_at',
               'started_at', 'stopped_at', 'client_correlated', 'player_state', 'application_client_id'}
UD_FIELDS = {'user_id', 'item_id', 'playback_position_ticks', 'play_count', 'is_favorite', 'played', 'last_played_at', 'updated_at'}


def load_observer(path, expected):
    # The first load cannot rely on the code it is about to authenticate.
    if not (path.name == 'inspect-client-positive-prepare-scope.py' and path.is_absolute() and WORK in path.parents and
            '..' not in path.parts and re.fullmatch(r'[0-9a-f]{64}', expected or '')):
        raise ValueError('observer_input_pin_invalid')
    for parent in reversed(path.parents):
        info = parent.lstat()
        if not (stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022):
            raise ValueError('observer_parent_untrusted')
    before = path.lstat()
    if not (stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and before.st_nlink == 1 and
            stat.S_IMODE(before.st_mode) in (0o600, 0o644) and 0 < before.st_size <= 128 << 10):
        raise ValueError('observer_file_untrusted')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as stream:
        opened = os.fstat(stream.fileno())
        if (opened.st_dev, opened.st_ino) != (before.st_dev, before.st_ino):
            raise ValueError('observer_open_changed')
        raw = stream.read((128 << 10) + 1)
    after = path.lstat()
    if ((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) !=
            (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns) or
            len(raw) != before.st_size or hashlib.sha256(raw).hexdigest() != expected):
        raise ValueError('observer_digest_changed')
    module = types.ModuleType('positive_prepare_offline_contract')
    module.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), module.__dict__)
    return module


def compare(op, before, after, report, proof, baseline):
    require, same, instant = op.require, op.same, op.instant
    old = op.observation_rows(before, 'before', proof)
    new = op.observation_rows(after, 'after', proof)
    lower, upper = instant(before['observed_at']), instant(after['observed_at'])
    require(lower <= upper <= lower + dt.timedelta(minutes=30), 'observation_window_outside_scope')
    require(same(before['rows'], baseline) and same(before['counts'], proof['baseline_counts']) and
            op.digest(op.exact(baseline).encode()) == proof['baseline_projection_sha256'], 'reviewed_baseline_changed')
    permitted = op.eligible_expiry(before, proof)
    require(same(before.get('approved_expiry_id_sha256'), permitted), 'fresh_expiry_declaration_changed')
    for name in ('users_state', 'user_settings', 'movie_rows', *op.AUXILIARY):
        require(same(before['rows'][name], after['rows'][name]), name + '_changed')
    require(report.get('marker') == 'goby-client-cross-user-m3e-v1' and type(report.get('format')) is int and report['format'] == 6 and
            report.get('mode') == 'acceptance-preparation' and report.get('preparation_scope') == SCOPE and
            report.get('result') in ('passed', 'failed') and type(report.get('client_acceptance')) is bool and
            lower <= instant(report['started_at']) <= instant(report['finished_at']) <= upper and
            same(report.get('fixture', {}).get('profile'), proof['profile']), 'ui_report_scope_or_profile_mismatch')
    accounts = op.keyed(report.get('accounts'), ('slot',))
    require(set(accounts) == {(slot,) for slot in proof['actors']}, 'ui_actor_population_mismatch')
    for name in ('encoding_jobs', 'client_playback_references'):
        require(set(old[name]) == set(new[name]) and all(same(row, new[name][key]) for key, row in old[name].items()), name + '_changed')
    for key, row in old['sessions'].items():
        require(key in new['sessions'] and same(row, new['sessions'][key]), 'old_auth_changed_or_deleted')
    for key, row in old['user_item_data'].items():
        require(key in new['user_item_data'] and same(row, new['user_item_data'][key]), 'old_userdata_changed_or_deleted')
    fresh_auth = {key: row for key, row in new['sessions'].items() if key not in old['sessions']}
    added = {key: row for key, row in new['play_sessions'].items() if key not in old['play_sessions']}
    require(len(fresh_auth) == len(added) == 2, 'unexpected_new_auth_or_play_count')
    fingerprints = {row['token_fingerprint_sha256'] for row in old['sessions'].values()}
    auth_by_user, play_by_user = {}, {}
    target = proof['movie']
    duration = before['rows']['movie_rows'][0]['media']['DurationTicks']

    def window(value, start=lower, end=upper):
        return start <= instant(value) <= end

    for slot, user_id in proof['actors'].items():
        actor = accounts[(slot,)]
        fingerprint = actor.get('token_fingerprint')
        require(actor.get('id') == user_id and isinstance(fingerprint, str) and op.HASH.fullmatch(fingerprint) and
                fingerprint not in fingerprints and actor.get('principal_confirmed') is True and
                actor.get('ordinary_authority_confirmed') is True and actor.get('login', {}).get('status') == 200 and
                actor['login'].get('request_count') == 1, 'fresh_ui_authority_unproven')
        matches = [row for row in fresh_auth.values() if row.get('user_id') == user_id and row.get('token_fingerprint_sha256') == fingerprint]
        require(len(matches) == 1, 'new_auth_fingerprint_mismatch')
        auth = matches[0]
        require(set(auth) == {*op.AUTH_FIELDS, 'token_fingerprint_sha256'} and auth.get('kind') == 'emby' and
                isinstance(auth.get('device_id'), str) and auth['device_id'] and window(auth['created_at']) and
                window(auth['last_seen_at'], instant(auth['created_at'])) and
                window(auth['revoked_at'], instant(auth['last_seen_at'])) and instant(auth['expires_at']) > upper,
                'new_auth_lifecycle_mismatch')
        logout, proxy = actor.get('session_proof', {}), actor.get('proxy_logout', {})
        entries = logout.get('entries', [])
        require(actor.get('logout', {}).get('status') == 204 and actor.get('closed') is True and
                proxy.get('completed') is True and proxy.get('status') == 204 and proxy.get('token_fingerprint') == fingerprint and
                logout.get('outcome') == 'all_observed_logout_tokens_rejected' and len(entries) == 1 and
                entries[0].get('token_fingerprint') == fingerprint and entries[0].get('ui_request', {}).get('response_status') == 204 and
                entries[0].get('verification', {}).get('status') == 401, 'new_auth_logout_unproven')
        source, preparation = actor.get('preparation_source', {}), actor.get('preparation', {})
        response = preparation.get('response', {})
        require(source.get('item_id') == target['id'] and source.get('source_id') == target['media_source_id'] and
                source.get('path') == target['path'] and preparation.get('request_validated') is True and
                preparation.get('completed') is True and preparation.get('ui_status') == 200 and preparation.get('ui_finished') is True and
                preparation.get('existing_play_or_live_session_requested') is False and preparation.get('item_id') == target['id'] and
                preparation.get('source_id') == target['media_source_id'] and preparation.get('user_id') == user_id and
                preparation.get('token_fingerprint') == fingerprint and actor.get('proxy', {}).get('preparation') == 1 and
                response.get('status') == 200 and response.get('validated') is True and
                re.fullmatch(r'play_[0-9a-f]{32}', response.get('play_session_id', '')), 'owned_preparation_unproven')
        matches = [row for row in added.values() if row.get('user_id') == user_id]
        require(len(matches) == 1, 'new_play_actor_mismatch')
        play = matches[0]
        require(set(play) == PLAY_FIELDS and play['id'] == response['play_session_id'] and play['auth_session_id'] == auth['id'] and
                play['device_id'] == auth['device_id'] and play['item_id'] == target['id'] and play['media_source_id'] == target['media_source_id'] and
                play['state'] == 'Prepared' and play['started_at'] is None and play['stopped_at'] is None and
                play['counted'] is False and play['client_correlated'] is False and play['application_client_id'] is None and
                same(play['player_state'], {}) and type(play['duration_ticks']) is int and play['duration_ticks'] == duration and
                type(play['position_ticks']) is int and window(play['created_at'], instant(auth['created_at']), instant(auth['revoked_at'])) and
                window(play['updated_at'], instant(play['created_at']), instant(auth['revoked_at'])) and
                lower + dt.timedelta(minutes=30) <= instant(play['expires_at']) <= upper + dt.timedelta(minutes=30),
                'new_play_scope_or_state_mismatch')
        previous = old['user_item_data'].get((user_id, target['id']))
        require(play['position_ticks'] == min(previous['playback_position_ticks'] if previous else 0, duration),
                'new_play_resume_position_mismatch')
        auth_by_user[user_id], play_by_user[user_id] = auth, play
    require(len({row['token_fingerprint_sha256'] for row in auth_by_user.values()}) == 2, 'shared_new_authentication')

    changed = []
    for key, row in old['play_sessions'].items():
        require(key in new['play_sessions'], 'old_play_deleted')
        current = new['play_sessions'][key]
        identifier = op.digest(row['id'].encode())
        if identifier not in permitted:
            require(same(row, current), 'unapproved_old_play_change')
            continue
        require(set(row) == set(current) and current['state'] == 'Expired' and
                same({field: value for field, value in row.items() if field not in ('state', 'stopped_at', 'updated_at')},
                     {field: value for field, value in current.items() if field not in ('state', 'stopped_at', 'updated_at')}) and
                window(current['stopped_at'], lower, instant(play_by_user[row['user_id']]['created_at'])) and
                window(current['updated_at'], instant(current['stopped_at']), instant(play_by_user[row['user_id']]['created_at'])),
                'approved_expiry_change_mismatch')
        changed.append(identifier)
    require(sorted(changed) == permitted, 'approved_expiry_not_observed')

    # lockUserData inserts only these defaults; it never updates a retained row.
    expected_new_data = {(user_id, target['id']) for user_id in proof['actors'].values()
                         if (user_id, target['id']) not in old['user_item_data']}
    added_data = {key: row for key, row in new['user_item_data'].items() if key not in old['user_item_data']}
    require(set(added_data) == expected_new_data, 'unexpected_new_userdata_keys')
    for key, row in added_data.items():
        auth, play = auth_by_user[row['user_id']], play_by_user[row['user_id']]
        require(set(row) == UD_FIELDS and same({field: value for field, value in row.items() if field != 'updated_at'},
                {'user_id': key[0], 'item_id': target['id'], 'playback_position_ticks': 0, 'play_count': 0,
                 'is_favorite': False, 'played': False, 'last_played_at': None}) and
                window(row['updated_at'], instant(auth['created_at']), instant(play['created_at'])), 'new_userdata_not_exact_default')
    return {'before_counts': before['counts'], 'after_counts': after['counts'],
            'old_auth_rows_unchanged': len(old['sessions']), 'old_play_rows_retained': len(old['play_sessions']),
            'old_play_rows_unchanged': len(old['play_sessions']) - len(changed),
            'expired_old_play_id_sha256': sorted(changed), 'old_userdata_rows_unchanged': len(old['user_item_data']),
            'new_default_userdata_rows': len(added_data), 'new_auth_rows': 2, 'new_prepared_rows': 2,
            'new_play_id_sha256': sorted(op.digest(row['id'].encode()) for row in added.values()),
            'new_auth_id_sha256': sorted(op.digest(row['id'].encode()) for row in fresh_auth.values()),
            'reference_rows_unchanged': len(old['client_playback_references']), 'encoding_rows_unchanged': len(old['encoding_jobs']),
            'old_actor_configuration_policy_preferences_unchanged': True, 'positive_movie_and_auxiliary_rows_unchanged': True}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--preparation-scope', choices=(SCOPE,), required=True)
    parser.add_argument('--observer-sha256', required=True)
    for name in ('before', 'after', 'report', 'expected-ledger'):
        parser.add_argument('--' + name, type=Path, required=True)
        parser.add_argument('--' + name + '-sha256', required=True)
    args = parser.parse_args()
    summary = {'marker': 'goby-client-positive-prepare-comparison-v1', 'scope': SCOPE, 'database_scope_result': 'failed',
               'database_wide_preservation_claimed': False, 'ui_result_promoted': False, 'reason_codes': []}
    op = None
    try:
        if not (sys.platform == 'linux' and os.getuid() == os.geteuid() == os.getgid() == os.getegid() == 0):
            raise ValueError('authorized_remote_root_required')
        op = load_observer(Path(__file__).with_name('inspect-client-positive-prepare-scope.py'), args.observer_sha256)
        op.require(args.before.name == 'before.json' and args.after.name == 'after.json' and
                   args.before.parent == args.after.parent and args.report.name == 'report.json', 'input_roles_mismatch')
        records = {name: {'path': str(getattr(args, name)), 'sha256': getattr(args, name + '_sha256')}
                   for name in ('before', 'after', 'report', 'expected_ledger')}
        proof, baseline = op.authority(records['expected_ledger'], live=False)
        before, after, report = (op.read_record(records[name]) for name in ('before', 'after', 'report'))
        summary['input_sha256'] = {name: record['sha256'] for name, record in records.items()}
        summary['input_sha256']['observer'] = args.observer_sha256
        summary['ui_report_result'] = report.get('result') if report.get('result') in ('passed', 'failed') else None
        summary['ui_client_acceptance'] = report.get('client_acceptance') if type(report.get('client_acceptance')) is bool else None
        summary['ui_failure_sha256'] = op.digest(op.exact(report.get('failure')).encode())
        summary['counts_and_hashes'] = compare(op, before, after, report, proof, baseline)
        summary['database_scope_result'] = 'passed'
    except Exception as error:
        summary['reason_codes'] = [str(error) if op is not None and isinstance(error, op.ScopeError) else 'input_shape_or_read_error']
    print(json.dumps(summary, sort_keys=True))
    return 0 if summary['database_scope_result'] == 'passed' else 1


if __name__ == '__main__':
    raise SystemExit(main())
