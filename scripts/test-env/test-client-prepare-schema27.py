#!/usr/bin/env python3
"""Memory-only guards for the fixed schema27 preparation observation scope."""

import copy
import hashlib
import importlib.util
import os
from pathlib import Path
import re
import subprocess
import sys
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
OBSERVER = None
COMPARE = None


def fixture():
    actors = list(COMPARE.ACTORS.values())
    old_time = '2026-09-12T00:00:00Z'
    auth = [{'id': 'old-auth-' + str(index), 'user_id': actors[index % 2] if index < 49 else actors[index - 49],
             'kind': 'emby', 'device_id': 'old-device-' + str(index), 'created_at': old_time, 'last_seen_at': old_time,
             'expires_at': '2026-10-01T00:00:00Z', 'revoked_at': old_time,
             'token_fingerprint_sha256': hashlib.sha256(str(index).encode()).hexdigest()} for index in range(51)]

    def play(identifier, owner, credential, state, created=old_time, updated=old_time, expires=old_time):
        return {'id': identifier, 'user_id': owner, 'auth_session_id': credential['id'], 'device_id': credential['device_id'],
                'item_id': COMPARE.MOVIE, 'media_source_id': COMPARE.SOURCE, 'state': state, 'position_ticks': 20,
                'duration_ticks': 1000, 'counted': False, 'created_at': created, 'updated_at': updated, 'expires_at': expires,
                'started_at': None, 'stopped_at': None, 'player_state': {}, 'client_correlated': False, 'application_client_id': None}

    plays = [play('old-play-' + str(index), actors[index % 2], auth[index], 'Expired') for index in range(20)]
    plays += [play('reviewed-play-' + slot, owner, auth[index + 49], 'Prepared')
              for index, (slot, owner) in enumerate(COMPARE.ACTORS.items())]
    user_data = [{'user_id': owner, 'item_id': COMPARE.MOVIE, 'playback_position_ticks': 20} for owner in actors]
    user_data += [{'user_id': actors[0], 'item_id': 'other-' + str(index), 'playback_position_ticks': 0} for index in range(3)]
    origin = {'marker': COMPARE.MARKER, 'version': 1, 'phase': 'after', 'complete': True, 'observed_at': old_time,
              'identity': copy.deepcopy(COMPARE.EXPECTED_IDENTITY), 'counts': dict(OBSERVER.SCHEMA27_COUNTS),
              'actors': [{'label': slot, 'user_id': owner, 'exists': True, 'disabled': False, 'administrator': False,
                          'all_folders': True, 'playback_allowed': True, 'movie_userdata_exists': True}
                         for slot, owner in COMPARE.ACTORS.items()],
              'movie': {'id': COMPARE.MOVIE, 'exists': True, 'type': 'Movie', 'folder': False, 'has_media': True,
                        'has_theme_association': False, 'reserved': False},
              'foreign_dependents': {'references': 0, 'encodings': 0}, 'play_sessions': plays,
              'client_playback_references': [], 'user_item_data': user_data, 'sessions': auth, 'encoding_jobs': []}
    authority = {'scope': COMPARE.SCHEMA27_SCOPE, 'database_oid': 1234, 'role_oid': 1233,
                 'approved_expiry_id_sha256': OBSERVER.schema27_expiry(origin), 'binary_sha256': 'a' * 64,
                 'fixture_state_sha256': 'b' * 64, 'source': str(COMPARE.WORK / 'source-attempt-32'),
                 'source_manifest_sha256': 'c' * 64, 'process': {'pid': 100, 'start_ticks': 200, 'boot_id': 'synthetic-boot'}}
    before = copy.deepcopy(origin)
    before.update(phase='before', observed_at='2026-09-12T00:01:00Z', preparation_scope=COMPARE.SCHEMA27_SCOPE,
                  authority=authority, identity=dict(origin['identity'], schema=27, database_oid=1234, role_oid=1233, database_owner_oid=1233))
    before['movie'].update(has_extra_association=False, extra_reserved=False)
    after = copy.deepcopy(before)
    after.update(phase='after', observed_at='2026-09-12T00:01:07Z')
    for row in after['play_sessions'][-2:]:
        row.update(state='Expired', updated_at='2026-09-12T00:01:03Z', stopped_at='2026-09-12T00:01:03Z')
    report = {'marker': 'goby-client-cross-user-m3e-v1', 'format': 5, 'mode': 'acceptance-preparation',
              'preparation_scope': COMPARE.SCHEMA27_SCOPE, 'result': 'failed', 'failure': 'page_error_A', 'client_acceptance': False,
              'started_at': '2026-09-12T00:01:01Z', 'finished_at': '2026-09-12T00:01:06Z',
              'owned_items': [{'key': 'movie', 'id': COMPARE.MOVIE, 'type': 'Movie'}], 'accounts': [],
              'fixture': {'schema': 27, 'binary_sha256': authority['binary_sha256'], 'process': authority['process'],
                          'fixture_state_sha256': authority['fixture_state_sha256'], 'schema_binding':
                          {'schema': 27, 'source': authority['source'], 'source_manifest_sha256': authority['source_manifest_sha256']}}}
    for slot, owner in COMPARE.ACTORS.items():
        fingerprint = hashlib.sha256(('synthetic-new-' + slot).encode()).hexdigest()
        fresh = dict(auth[0], id='new-auth-' + slot, user_id=owner, device_id='new-device-' + slot,
                     created_at='2026-09-12T00:01:02Z', last_seen_at='2026-09-12T00:01:04Z',
                     revoked_at='2026-09-12T00:01:05Z', token_fingerprint_sha256=fingerprint)
        identifier = 'play_' + hashlib.sha256(slot.encode()).hexdigest()[:32]
        after['sessions'].append(fresh)
        after['play_sessions'].append(play(identifier, owner, fresh, 'Prepared', '2026-09-12T00:01:03Z',
                                            '2026-09-12T00:01:04Z', '2026-09-12T00:31:04Z'))
        report['accounts'].append({'slot': slot, 'id': owner, 'token_fingerprint': fingerprint,
            'principal_confirmed': True, 'ordinary_authority_confirmed': True, 'login': {'status': 200, 'request_count': 1},
            'logout': {'status': 204}, 'closed': True, 'proxy_logout': {'completed': True, 'status': 204, 'token_fingerprint': fingerprint},
            'session_proof': {'outcome': 'all_observed_logout_tokens_rejected', 'entries': [{'token_fingerprint': fingerprint,
                'ui_request': {'response_status': 204}, 'verification': {'status': 401}}]}, 'proxy': {'preparation': 1},
            'preparation_source': {'item_id': COMPARE.MOVIE, 'source_id': COMPARE.SOURCE},
            'preparation': {'request_validated': True, 'completed': True, 'existing_play_or_live_session_requested': False,
                'item_id': COMPARE.MOVIE, 'source_id': COMPARE.SOURCE, 'user_id': owner, 'token_fingerprint': fingerprint,
                'response': {'status': 200, 'validated': True, 'play_session_id': identifier}}})
    after['counts'].update(play_sessions=24, sessions=53)
    return origin, authority, before, after, report


class Guards(unittest.TestCase):
    def setUp(self):
        self.enterContext(patch.object(subprocess, 'run', side_effect=AssertionError('External command forbidden.')))
        self.enterContext(patch.object(os, 'open', side_effect=AssertionError('Filesystem operation forbidden.')))

    def test_fixed_sql_is_readonly_candidate_scope_with_both_auxiliary_guards(self):
        self.assertIn('READ ONLY', OBSERVER.SQL27)
        self.assertFalse(re.search(r'\b(INSERT|UPDATE|DELETE|ALTER|CREATE|DROP|TRUNCATE|GRANT|REVOKE)\b', OBSERVER.SQL27))
        self.assertIn('public.item_extra_resources', OBSERVER.SQL27)
        self.assertIn('public.extra_reserved_paths', OBSERVER.SQL27)
        self.assertIn('public.item_theme_resources', OBSERVER.SQL27)
        self.assertNotIn('client_capabilities', OBSERVER.SQL27)
        self.assertNotIn('password', OBSERVER.SQL27)
        self.assertEqual(OBSERVER.CONNECTION[-1], 'goby_client_m3e')
        self.assertEqual(OBSERVER.CONNECTION[OBSERVER.CONNECTION.index('-p') + 1], '15432')

    def test_expiry_requires_actual_origin_membership_and_revoked_owners(self):
        origin, authority, *_ = fixture()
        self.assertEqual(OBSERVER.schema27_expiry(origin), authority['approved_expiry_id_sha256'])
        for mutate in (lambda value: value['counts'].update(sessions=56),
                       lambda value: value['play_sessions'][-1].update(state='Playing'),
                       lambda value: value['sessions'][-1].update(revoked_at=None),
                       lambda value: value['play_sessions'].pop()):
            changed = copy.deepcopy(origin)
            mutate(changed)
            with self.assertRaises(ValueError):
                OBSERVER.schema27_expiry(changed)

    def test_fresh_schema27_before_preserves_origin_rows_without_relabelling_it(self):
        origin, _, before, *_ = fixture()
        OBSERVER.schema27_start(before, origin)
        for table in OBSERVER.SCHEMA27_COUNTS:
            changed = copy.deepcopy(before)
            changed[table].append({'unowned': True})
            with self.assertRaises(ValueError):
                OBSERVER.schema27_start(changed, origin)
        self.assertEqual(origin['identity']['schema'], 26)

    def test_schema27_ledger_pass_does_not_change_ui_failure(self):
        origin, authority, before, after, report = fixture()
        result = COMPARE.compare(before, after, report, COMPARE.SCHEMA27_SCOPE, origin, authority)
        self.assertEqual((result['old_play_rows_retained'], result['old_play_rows_unchanged'], result['old_auth_rows_unchanged']), (22, 20, 51))
        self.assertEqual((result['expired_old_play_count'], result['new_play_count'], result['new_auth_count']), (2, 2, 2))
        self.assertEqual(report['result'], 'failed')
        self.assertFalse(report['client_acceptance'])

    def test_schema27_comparison_rejects_old_rows_new_scope_and_auxiliary_changes(self):
        for mutate in (lambda value: value['sessions'][0].update(last_seen_at='2026-09-12T00:01:02Z'),
                       lambda value: value['play_sessions'][0].update(state='Stopped'),
                       lambda value: value['user_item_data'][0].update(playback_position_ticks=0),
                       lambda value: value['play_sessions'][-1].update(device_id='foreign-device'),
                       lambda value: value['play_sessions'][-1].update(state='Playing'),
                       lambda value: value['play_sessions'][-1].update(started_at='2026-09-12T00:01:04Z'),
                       lambda value: value['movie'].update(extra_reserved=True),
                       lambda value: value['identity'].update(schema=26)):
            origin, authority, before, after, report = fixture()
            mutate(after)
            with self.assertRaises(COMPARE.ScopeError):
                COMPARE.compare(before, after, report, COMPARE.SCHEMA27_SCOPE, origin, authority)

    def test_scope_and_current_fixture_proofs_cannot_be_substituted(self):
        origin, authority, before, after, report = fixture()
        with self.assertRaises(COMPARE.ScopeError):
            COMPARE.compare(before, after, report)
        report['fixture']['binary_sha256'] = 'f' * 64
        with self.assertRaises(COMPARE.ScopeError):
            COMPARE.compare(before, after, report, COMPARE.SCHEMA27_SCOPE, origin, authority)


def main():
    global OBSERVER, COMPARE
    if sys.platform != 'linux' or os.geteuid() != 0 or not os.environ.get('SSH_CONNECTION') or len(sys.argv) != 3:
        raise SystemExit('Run through authorized root SSH: test-client-prepare-schema27.py OBSERVER COMPARATOR')
    for name, path in (('observer', Path(sys.argv[1])), ('comparator', Path(sys.argv[2]))):
        spec = importlib.util.spec_from_file_location(name, path)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        if name == 'observer':
            OBSERVER = module
        else:
            COMPARE = module
    if hashlib.sha256(Path(sys.argv[1]).read_bytes()).hexdigest() != COMPARE.SCHEMA27_OBSERVER_SHA:
        raise SystemExit('The comparator does not bind this exact observer source.')
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(Guards))
    print('Memory-only schema27 guards: no SQL, HTTP, filesystem mutation, or service action.')
    return 0 if result.wasSuccessful() and not result.skipped else 1


if __name__ == '__main__':
    raise SystemExit(main())
