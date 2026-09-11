#!/usr/bin/env python3
"""Pure guards for the independent positive Movie preparation ledger.

Run remotely after pinning these three tool files. Every fixture is synthetic;
SQL execution, HTTP, process inspection and filesystem writes are forbidden.
"""

import copy
import importlib.util
from pathlib import Path
import unittest
from unittest import mock


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


observer = load('positive_observer_guards', 'inspect-client-positive-prepare-scope.py')
comparator = load('positive_comparator_guards', 'compare-client-positive-prepare-scope.py')


def timestamp(minute):
    return '2026-09-12T00:' + str(minute).zfill(2) + ':00+00:00'


def fixture(existing_data=False):
    actors = {'A': 'a' * 32, 'B': 'b' * 32}
    target = {'id': 'c' * 32, 'path': '/opt/goby-fixtures/client-special-features-m3e-v1/Movies/Positive/Positive.mp4',
              'media_source_id': 'actual-positive-source'}
    rows = {name: [] for name in (*observer.TABLE_KEYS, *observer.AUXILIARY, 'user_settings', 'movie_rows', 'users_state')}
    rows['movie_rows'] = [{'id': target['id'], 'path': target['path'], 'type': 'Movie', 'is_folder': False,
                           'media': {'DurationTicks': 20000000}, 'root_id': 'd' * 32, 'relative_path': 'Positive/Positive.mp4'}]
    rows['users_state'] = [{'id': user, 'is_administrator': False, 'is_disabled': False, 'policy': {}, 'configuration': {}}
                           for user in actors.values()]
    proof = {'scope': observer.SCOPE, 'actors': actors, 'movie': target, 'database_oid': 987, 'role_oid': 986,
             'profile': {'receipt_path': '/synthetic/receipt', 'receipt_sha256': '1' * 64, 'report_path': '/synthetic/report',
                         'report_sha256': '2' * 64, 'inspect_path': '/synthetic/inspect', 'inspect_sha256': '3' * 64,
                         'state_sha256': '4' * 64}}

    def data(user, item):
        return {'user_id': user, 'item_id': item, 'playback_position_ticks': 0, 'play_count': 0,
                'is_favorite': False, 'played': False, 'last_played_at': None, 'updated_at': timestamp(0)}

    def auth(user, index, fresh):
        return {'id': ('fresh-' if fresh else 'old-') + str(index), 'user_id': user, 'kind': 'emby', 'device_id': 'device-' + str(index),
                'created_at': timestamp(12 if fresh else 0), 'last_seen_at': timestamp(14 if fresh else 1),
                'revoked_at': timestamp(18 if fresh else 2), 'expires_at': '2026-10-12T00:00:00+00:00',
                'token_fingerprint_sha256': str(index + (3 if fresh else 1)) * 64}

    def play(user, authentication, index, fresh):
        return {'id': 'play_' + str(index + (3 if fresh else 1)) * 32, 'user_id': user, 'auth_session_id': authentication['id'],
                'device_id': authentication['device_id'], 'item_id': target['id'] if fresh else 'f' * 32,
                'media_source_id': target['media_source_id'] if fresh else 'old-original-source', 'state': 'Prepared',
                'position_ticks': 0, 'duration_ticks': 20000000, 'counted': False, 'created_at': timestamp(14 if fresh else 0),
                'updated_at': timestamp(14 if fresh else 0), 'expires_at': timestamp(44 if fresh else 30),
                'started_at': None, 'stopped_at': None, 'client_correlated': False, 'application_client_id': None, 'player_state': {}}

    for index, user in enumerate(actors.values()):
        authentication = auth(user, index, False)
        rows['sessions'].append(authentication)
        rows['play_sessions'].append(play(user, authentication, index, False))
        rows['user_item_data'].append(data(user, target['id'] if existing_data else 'e' * 32))
    rows = {key: observer.ordered(value) for key, value in rows.items()}
    baseline = copy.deepcopy(rows)
    proof['baseline_counts'] = {name: len(rows[name]) for name in observer.TABLE_KEYS}
    proof['baseline_projection_sha256'] = observer.digest(observer.exact(baseline).encode())

    def document(phase, body, time):
        return {'marker': observer.MARKER, 'version': 1, 'preparation_scope': observer.SCOPE, 'phase': phase, 'complete': True,
                'authority': copy.deepcopy(proof), 'identity': observer.expected_identity(proof), 'rows': body,
                'counts': {name: len(body[name]) for name in observer.TABLE_KEYS}, 'observed_at': timestamp(time),
                'foreign_dependents': {'references': 0, 'encodings': 0}}

    before = document('before', rows, 10)
    before['approved_expiry_id_sha256'] = observer.eligible_expiry(before, proof)
    final = copy.deepcopy(rows)
    report = {'marker': 'goby-client-special-features-browser-v1', 'format': 6, 'mode': 'acceptance-preparation',
              'preparation_scope': observer.SCOPE, 'result': 'passed', 'client_acceptance': True, 'failure': None,
              'started_at': timestamp(11), 'finished_at': timestamp(19), 'fixture': {'profile': copy.deepcopy(proof['profile'])}, 'accounts': []}
    for row in final['play_sessions']:
        row.update(state='Expired', stopped_at=timestamp(13), updated_at=timestamp(13))
    for index, (slot, user) in enumerate(actors.items()):
        authentication = auth(user, index, True)
        prepared = play(user, authentication, index, True)
        final['sessions'].append(authentication)
        final['play_sessions'].append(prepared)
        if not existing_data:
            new_data = data(user, target['id'])
            new_data['updated_at'] = timestamp(13)
            final['user_item_data'].append(new_data)
        fp = authentication['token_fingerprint_sha256']
        report['accounts'].append({'slot': slot, 'id': user, 'token_fingerprint': fp, 'principal_confirmed': True,
            'ordinary_authority_confirmed': True, 'login': {'status': 200, 'request_count': 1}, 'closed': True,
            'logout': {'status': 204}, 'proxy_logout': {'completed': True, 'status': 204, 'token_fingerprint': fp},
            'session_proof': {'outcome': 'all_observed_logout_tokens_rejected', 'entries': [{'token_fingerprint': fp,
                'ui_request': {'response_status': 204}, 'verification': {'status': 401}}]},
            'preparation_source': {'item_id': target['id'], 'source_id': target['media_source_id'], 'path': target['path']},
            'proxy': {'preparation': 1}, 'preparation': {'request_validated': True, 'completed': True, 'ui_status': 200,
                'ui_finished': True, 'existing_play_or_live_session_requested': False, 'item_id': target['id'],
                'source_id': target['media_source_id'], 'user_id': user, 'token_fingerprint': fp,
                'response': {'status': 200, 'validated': True, 'play_session_id': prepared['id']}}})
    final = {key: observer.ordered(value) for key, value in final.items()}
    return before, document('after', final, 20), report, proof, baseline


class PositiveLedgerGuards(unittest.TestCase):
    def setUp(self):
        self.effects = mock.patch.multiple(observer.subprocess, run=mock.Mock(side_effect=AssertionError('No subprocess in pure guards')))
        self.files = mock.patch.object(observer.os, 'open', side_effect=AssertionError('No file access in pure guards'))
        self.effects.start()
        self.files.start()
        self.addCleanup(self.effects.stop)
        self.addCleanup(self.files.stop)

    def compare(self, values):
        return comparator.compare(observer, *values)

    def reject(self, values, code):
        with self.assertRaisesRegex(observer.ScopeError, '^' + code + '$'):
            self.compare(values)

    def test_dynamic_baseline_and_explicit_default_rows(self):
        result = self.compare(fixture())
        self.assertEqual(result['old_auth_rows_unchanged'], 2)
        self.assertEqual(result['new_default_userdata_rows'], 2)
        self.assertEqual(result['old_play_rows_unchanged'], 0)

    def test_existing_target_userdata_is_preserved_without_new_rows(self):
        self.assertEqual(self.compare(fixture(True))['new_default_userdata_rows'], 0)

    def test_database_success_does_not_rewrite_ui_failure(self):
        values = fixture()
        values[2].update(result='failed', client_acceptance=False, failure='page_error')
        original = copy.deepcopy(values[2])
        self.compare(values)
        self.assertEqual(values[2], original)

    def test_default_userdata_requires_every_default(self):
        for field, value in (('play_count', 1), ('play_count', False), ('playback_position_ticks', 1),
                             ('is_favorite', True), ('played', True), ('last_played_at', timestamp(13)), ('updated_at', timestamp(0))):
            with self.subTest(field=field, value=value):
                values = fixture()
                row = next(row for row in values[1]['rows']['user_item_data'] if row['item_id'] == values[3]['movie']['id'])
                row[field] = value
                self.reject(values, 'new_userdata_not_exact_default')

    def test_unrelated_userdata_insert_is_rejected(self):
        values = fixture()
        next(row for row in values[1]['rows']['user_item_data'] if row['item_id'] == values[3]['movie']['id'])['item_id'] = '9' * 32
        self.reject(values, 'unexpected_new_userdata_keys')

    def test_old_userdata_change_is_rejected(self):
        values = fixture(True)
        values[1]['rows']['user_item_data'][0]['play_count'] = 1
        self.reject(values, 'old_userdata_changed_or_deleted')

    def test_old_auth_cannot_be_touched_or_removed(self):
        values = fixture()
        next(row for row in values[1]['rows']['sessions'] if row['id'].startswith('old-'))['last_seen_at'] = timestamp(15)
        self.reject(values, 'old_auth_changed_or_deleted')

    def test_old_play_cannot_be_deleted(self):
        values = fixture()
        values[1]['rows']['play_sessions'] = [row for row in values[1]['rows']['play_sessions'] if row['state'] != 'Expired']
        values[1]['counts']['play_sessions'] = 2
        self.reject(values, 'old_play_deleted')

    def test_expiry_cannot_change_resume_or_source(self):
        values = fixture()
        next(row for row in values[1]['rows']['play_sessions'] if row['state'] == 'Expired')['position_ticks'] = 1
        self.reject(values, 'approved_expiry_change_mismatch')

    def test_expiry_must_really_happen(self):
        values = fixture()
        next(row for row in values[1]['rows']['play_sessions'] if row['state'] == 'Expired')['state'] = 'Prepared'
        self.reject(values, 'approved_expiry_change_mismatch')

    def test_play_and_auth_bind_to_real_response_token_source_and_device(self):
        for field, value in (('device_id', 'other-device'), ('auth_session_id', 'old-0'), ('media_source_id', 'guessed'),
                             ('state', 'Playing'), ('state', 'Expired'), ('started_at', timestamp(15)), ('duration_ticks', 1)):
            with self.subTest(field=field, value=value):
                values = fixture()
                next(row for row in values[1]['rows']['play_sessions'] if row['state'] == 'Prepared')[field] = value
                self.reject(values, 'new_play_scope_or_state_mismatch')

    def test_logout_must_be_verified_with_exact_fresh_token(self):
        values = fixture()
        values[2]['accounts'][0]['session_proof']['entries'][0]['verification']['status'] = 200
        self.reject(values, 'new_auth_logout_unproven')

    def test_plain_preparation_does_not_create_a_reference(self):
        values = fixture()
        play = next(row for row in values[1]['rows']['play_sessions'] if row['state'] == 'Prepared')
        values[1]['rows']['client_playback_references'].append({'user_id': play['user_id'], 'auth_session_id': play['auth_session_id'],
            'application_client_id': None, 'device_id': play['device_id'], 'client_nonce': 'unexpected', 'play_session_id': play['id'],
            'created_at': timestamp(14)})
        values[1]['counts']['client_playback_references'] = 1
        self.reject(values, 'client_playback_references_changed')

    def test_new_encoding_is_not_allowed(self):
        values = fixture()
        values[1]['rows']['encoding_jobs'].append({'id': 'encoding', 'user_id': values[3]['actors']['A']})
        values[1]['counts']['encoding_jobs'] = 1
        self.reject(values, 'encoding_jobs_changed')

    def test_before_rows_cannot_be_rebased_after_the_fact(self):
        values = fixture()
        values[0]['rows']['user_item_data'][0]['is_favorite'] = True
        self.reject(values, 'reviewed_baseline_changed')

    def test_old_prepared_and_pruning_rules_are_explicit(self):
        for change, code in ((lambda doc: doc['rows']['play_sessions'][0].update(state='Playing'), 'unreviewed_active_play_cleanup'),
                             (lambda doc: doc['rows']['sessions'][0].update(revoked_at=None), 'unreviewed_active_play_cleanup'),
                             (lambda doc: doc['rows']['play_sessions'][0].update(expires_at='2026-09-01T00:00:00+00:00'), 'old_play_pruning_risk')):
            with self.subTest(code=code):
                before, _, _, proof, _ = fixture()
                change(before)
                with self.assertRaisesRegex(observer.ScopeError, code):
                    observer.eligible_expiry(before, proof)

    def test_configuration_preferences_and_profile_cannot_change(self):
        for section in ('users_state', 'user_settings', 'movie_rows', 'item_extra_resources'):
            with self.subTest(section=section):
                values = fixture()
                if section == 'users_state':
                    values[1]['rows'][section][0]['configuration']['EnableNextEpisodeAutoPlay'] = True
                elif section == 'movie_rows':
                    values[1]['rows'][section][0]['path'] += '.changed'
                else:
                    values[1]['rows'][section].append({'user_id': values[3]['actors']['A'], 'resource_item_id': '0' * 32})
                self.reject(values, section + '_changed')

    def test_bool_cannot_masquerade_as_a_count(self):
        values = fixture()
        values[1]['counts']['encoding_jobs'] = False
        self.reject(values, 'observation_population_changed')

    def test_scope_and_profile_are_not_original_movie_evidence(self):
        for mutate in (lambda r: r.update(preparation_scope='schema27-original-movie-01'),
                       lambda r: r.update(format=5), lambda r: r['fixture']['profile'].update(receipt_sha256='0' * 64)):
            values = fixture()
            mutate(values[2])
            self.reject(values, 'ui_report_scope_or_profile_mismatch')

    def test_positive_driver_marker_rejects_the_old_cross_user_report(self):
        values = fixture()
        self.assertEqual(values[2]['marker'], 'goby-client-special-features-browser-v1')
        self.compare(values)
        values[2]['marker'] = 'goby-client-cross-user-m3e-v1'
        self.reject(values, 'ui_report_scope_or_profile_mismatch')

    def test_observer_sql_has_one_readonly_snapshot_and_validated_ids(self):
        proof = fixture()[3]
        sql = observer.statement(proof, 'before').decode()
        self.assertEqual(sql.count('BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;'), 1)
        self.assertTrue(sql.endswith('ROLLBACK;\n'))
        for forbidden in ('FOR UPDATE', 'INSERT ', 'UPDATE ', 'DELETE ', 'ALTER ', 'pg_terminate_backend', 'password_hash'):
            self.assertNotIn(forbidden, sql)
        self.assertIn(proof['movie']['id'], sql)
        self.assertNotIn('268051d3ca734aefcf94e245fb25ad55', sql)
        proof['movie']['id'] = "x'; SELECT 1;--"
        with self.assertRaisesRegex(observer.ScopeError, 'invalid_sql_identifiers'):
            observer.statement(proof, 'before')

    def test_projection_omits_private_auth_fields_and_admin_counts(self):
        before, _, _, proof, _ = fixture()
        tables = copy.deepcopy(before['rows'])
        tables['users'] = tables.pop('users_state')
        tables['items'] = tables.pop('movie_rows')
        for row in tables['sessions']:
            row['token_hash'] = '\\x' + row.pop('token_fingerprint_sha256')
            row['password_hash'] = 'secret-sentinel'
            row['client_capabilities'] = {'secret': 'sentinel'}
        tables['sessions'].append(dict(tables['sessions'][0], id='admin', user_id='0' * 32))
        projected = observer.projection(tables, proof['actors'], proof['movie']['id'])
        self.assertEqual(len(projected['sessions']), 2)
        self.assertEqual(len(tables['sessions']), 3)
        self.assertNotIn('secret-sentinel', observer.exact(projected))
        self.assertNotIn('client_capabilities', observer.exact(projected))

    def test_setup_ledger_uses_sorted_actor_scope_and_separate_global_counts(self):
        before, _, _, proof, baseline = fixture()
        tables = copy.deepcopy(before['rows'])
        tables['sessions'].append(dict(tables['sessions'][0], id='admin', user_id='0' * 32))
        pin = {'path': '/synthetic/after-full.json', 'sha256': '5' * 64}
        declared = dict(pin, scope_user_ids=sorted(proof['actors'].values()),
                        actors={'primary': proof['actors']['B'], 'av': proof['actors']['A']},
                        counts={name: len(baseline[name]) for name in observer.TABLE_KEYS},
                        global_counts={name: len(tables[name]) for name in observer.TABLE_KEYS})
        observer.setup_ledger_contract(declared, pin, proof['actors'], baseline, tables)
        for field in ('counts', 'global_counts'):
            changed = copy.deepcopy(declared)
            changed[field]['sessions'] = 4
            with self.assertRaisesRegex(observer.ScopeError, 'profile_scoped_or_global_counts_mismatch'):
                observer.setup_ledger_contract(changed, pin, proof['actors'], baseline, tables)
        declared['scope_user_ids'].reverse()
        with self.assertRaisesRegex(observer.ScopeError, 'setup_ledger_binding_mismatch'):
            observer.setup_ledger_contract(declared, pin, proof['actors'], baseline, tables)

    def continuation(self):
        chain = {key: {'path': str(path), 'sha256': pin} for key, (path, pin) in observer.ORIGIN_PINS.items()}
        chain.update({key: {'path': str(observer.WORK / 'frozen-origin-tools' / name), 'sha256': pin}
                      for key, (name, pin) in observer.ORIGIN_SOURCE_PINS.items()})
        chain.update(marker=observer.CONTINUATION, library_id=observer.CONTINUATION_LIBRARY, root_id=observer.CONTINUATION_ROOT_ID,
                     origin_evidence={'path': str(observer.CONTINUATION_ROOT / 'origin-evidence.json'), 'sha256': '8' * 64},
                     creation_admin_session_id='1' * 32, new_scan_admin_session_id='2' * 32, new_viewer_session_id='3' * 32,
                     scan_job_id='4' * 32, creation_requests=5, continuation_library_creates=0, continuation_scan_dispatches=1,
                     original_failure_preserved=True, creation_admin_revoked=True, continuation_sessions_revoked=True)
        completed = {'profile_version': 2, 'continuation': chain, 'item_ids': {'library': observer.CONTINUATION_LIBRARY},
                     'library': {'root_id': observer.CONTINUATION_ROOT_ID}, 'job_id': '4' * 32}
        report, inspection = {'continuation': copy.deepcopy(chain)}, {'continuation': copy.deepcopy(chain)}
        return chain, completed, report, inspection

    def test_continuation_has_one_explicit_new_path_and_preserves_legacy_path(self):
        chain, completed, _, _ = self.continuation()
        ledger = {'profile': {'path': str(observer.CONTINUATION_ROOT / 'completed.json')},
                  'profile_report': {'path': str(observer.CONTINUATION_ROOT / 'report.json')},
                  'profile_inspection': {'path': str(observer.WORK / 'client-special-features-continuation-inspection-v1/report.json')},
                  'fixture_state': {'path': str(observer.WORK / 'client-fixture.json')}}
        self.assertEqual(observer.profile_paths(ledger, completed), 'goby-client-special-features-continuation-inspection-v1')
        ledger['profile']['path'] = str(observer.ORIGINAL_ROOT / 'completed.json')
        with self.assertRaisesRegex(observer.ScopeError, 'profile_path_mismatch'):
            observer.profile_paths(ledger, completed)
        ledger['profile_report']['path'] = str(observer.ORIGINAL_ROOT / 'report.json')
        ledger['profile_inspection']['path'] = str(observer.WORK / 'client-special-features-inspection-v1/report.json')
        self.assertEqual(observer.profile_paths(ledger, {'profile_version': 1}), 'goby-client-special-features-inspection-v1')
        with self.assertRaisesRegex(observer.ScopeError, 'profile_path_mismatch'):
            observer.profile_paths(ledger, {'profile_version': True})

    def test_continuation_requires_immutable_failed_origin_and_one_scan(self):
        values = self.continuation()
        observer.continuation_contract(*values)
        for key, value, code in (
                ('continuation_library_creates', 1, 'continuation_effect_scope_changed'),
                ('continuation_scan_dispatches', True, 'continuation_effect_scope_changed'),
                ('library_id', '9' * 32, 'continuation_effect_scope_changed'),
                ('original_failure_preserved', False, 'continuation_effect_scope_changed'),
                ('new_scan_admin_session_id', '1' * 32, 'continuation_actor_or_job_identity_changed')):
            with self.subTest(key=key):
                chain, completed, report, inspection = self.continuation()
                chain[key] = value
                report['continuation'] = copy.deepcopy(chain)
                inspection['continuation'] = copy.deepcopy(chain)
                with self.assertRaisesRegex(observer.ScopeError, code):
                    observer.continuation_contract(chain, completed, report, inspection)
        chain, completed, report, inspection = self.continuation()
        chain['origin_failure']['sha256'] = '0' * 64
        report['continuation'] = copy.deepcopy(chain)
        inspection['continuation'] = copy.deepcopy(chain)
        with self.assertRaisesRegex(observer.ScopeError, 'continuation_origin_pin_changed'):
            observer.continuation_contract(chain, completed, report, inspection)
        inspection['continuation']['origin_failure']['sha256'] = '1' * 64
        with self.assertRaisesRegex(observer.ScopeError, 'continuation_chain_missing_or_changed'):
            observer.continuation_contract(chain, completed, report, inspection)

    def test_continuation_links_creation_auth_and_new_scan_auth_without_relabelling(self):
        chain, completed, report, inspection = self.continuation()
        process = {'pid': 123, 'start_ticks': 456, 'boot_id': '0' * 36}
        completed.update(candidate={'process': process}, users={'admin_id': 'a' * 32, 'av_user_id': 'b' * 32})
        original = {'id': '0' * 32, 'kind': 'emby', 'user_id': 'b' * 32, 'revoked_at': timestamp(0)}
        creation = {'id': chain['creation_admin_session_id'], 'kind': 'admin', 'user_id': 'a' * 32,
                    'revoked_at': timestamp(2), 'token_hash': '\\x' + 'f' * 64}
        admin = dict(creation, id=chain['new_scan_admin_session_id'])
        viewer = dict(original, id=chain['new_viewer_session_id'])
        snapshots = {'origin_before_snapshot': {'database': {'tables': {'sessions': [original]}}},
                     'origin_current_snapshot': {'database': {'tables': {'sessions': [original, creation], 'scan_jobs': [],
                         'library_roots': [{'id': observer.CONTINUATION_ROOT_ID, 'library_id': observer.CONTINUATION_LIBRARY}]}}}}
        documents = {'origin_failure': {'marker': observer.PROFILE, 'result': 'retained_for_review', 'phase': 'library_acknowledged',
                     'retry_permitted': False, 'old_rows_preserved': True, 'library_id': observer.CONTINUATION_LIBRARY,
                     'job_id': None, 'root_id': None, 'authentication': {'admin': {'login_status': 200, 'logout_status': 204,
                         'exact_status': 401, 'token_sha256': 'f' * 64}, 'viewer': {}}},
                     'origin_state': {'phase': 'preparing_special_features_fixture', 'stage': 'library_acknowledged',
                         'special_features_profile': {'library_id': observer.CONTINUATION_LIBRARY}, 'process': process},
                     'origin_before_state': {'phase': 'ready', 'stage': 'complete', 'process': process},
                     'origin_library_ack': {'Id': observer.CONTINUATION_LIBRARY, 'CollectionType': 'movies',
                         'Paths': ['/opt/goby-fixtures/client-special-features-m3e-v1/Movies']}, 'origin_evidence': {}, **snapshots}
        by_path = {chain[key]['path']: value for key, value in documents.items()}
        snapshot = {'database': {'tables': {'sessions': [original, creation, admin, viewer],
                    'scan_jobs': [{'id': chain['scan_job_id'], 'library_id': observer.CONTINUATION_LIBRARY, 'status': 'Completed'}]}}}

        def read(record, **kwargs):
            return b'frozen source bytes' if kwargs.get('raw') else copy.deepcopy(by_path[record['path']])

        with mock.patch.object(observer, 'read_record', side_effect=read):
            self.assertEqual(observer.read_continuation(completed, report, inspection, snapshot), observer.digest(observer.exact(chain).encode()))
            snapshot['database']['tables']['sessions'][1] = dict(creation, revoked_at=None)
            with self.assertRaisesRegex(observer.ScopeError, 'continued_auth_population_changed'):
                observer.read_continuation(completed, report, inspection, snapshot)

    def finalization(self):
        _, completed, report, inspection = self.continuation()
        chain = {key: {'path': str(path), 'sha256': pin} for key, (path, pin) in observer.FINALIZATION_PINS.items()}
        chain.update({key: {'path': str(observer.WORK / 'frozen-scan-tools' / name), 'sha256': pin}
                      for key, (name, pin) in observer.FINALIZATION_SOURCE_PINS.items()})
        chain.update(marker=observer.FINALIZATION, scan_job_id=observer.FINALIZATION_JOB, viewer_user_id='b' * 32,
                     new_viewer_session_id='5' * 32, new_device_id=7, request_count=11, full_resource_count=4, range_resource_count=4,
                     library_creates=0, scan_dispatches=0, retained_failures_preserved=True, new_viewer_revoked=True,
                     parent_projection={'special_feature_count_present': False, 'local_trailer_count': 1},
                     continuation_evidence={'path': str(observer.FINALIZATION_ROOT / 'continuation-evidence.json'), 'sha256': '9' * 64})
        completed.update(profile_version=3, finalization=chain, users={'av_user_id': 'b' * 32}, job_id=observer.FINALIZATION_JOB)
        report['finalization'] = copy.deepcopy(chain)
        inspection['finalization'] = copy.deepcopy(chain)
        return chain, completed, report, inspection

    def test_finalization_is_a_third_explicit_profile_not_scan_success_relabelling(self):
        chain, completed, _, _ = self.finalization()
        ledger = {'profile': {'path': str(observer.FINALIZATION_ROOT / 'completed.json')},
                  'profile_report': {'path': str(observer.FINALIZATION_ROOT / 'report.json')},
                  'profile_inspection': {'path': str(observer.WORK / 'client-special-features-finalization-inspection-v1/report.json')},
                  'fixture_state': {'path': str(observer.WORK / 'client-fixture.json')}}
        self.assertEqual(observer.profile_paths(ledger, completed), 'goby-client-special-features-finalization-inspection-v1')
        for version in (1, 2, True):
            with self.assertRaisesRegex(observer.ScopeError, 'profile_path_mismatch'):
                observer.profile_paths(ledger, dict(completed, profile_version=version))
        ledger['profile']['path'] = str(observer.CONTINUATION_ROOT / 'completed.json')
        with self.assertRaisesRegex(observer.ScopeError, 'profile_path_mismatch'):
            observer.profile_paths(ledger, completed)

    def test_finalization_requires_retained_failure_and_exact_read_scope(self):
        observer.finalization_contract(*self.finalization())
        for key, value, code in (
                ('scan_dispatches', 1, 'finalization_effect_scope_changed'),
                ('request_count', 12, 'finalization_effect_scope_changed'),
                ('full_resource_count', 3, 'finalization_effect_scope_changed'),
                ('new_device_id', True, 'finalization_effect_scope_changed'),
                ('parent_projection', {'special_feature_count_present': True, 'local_trailer_count': 1}, 'finalization_effect_scope_changed'),
                ('continuation_failure', {'path': str(observer.FINALIZATION_ROOT / 'failure.json'), 'sha256': '0' * 64}, 'finalization_origin_pin_changed')):
            with self.subTest(key=key):
                chain, completed, report, inspection = self.finalization()
                chain[key] = value
                report['finalization'] = copy.deepcopy(chain)
                inspection['finalization'] = copy.deepcopy(chain)
                with self.assertRaisesRegex(observer.ScopeError, code):
                    observer.finalization_contract(chain, completed, report, inspection)

    def test_finalization_new_viewer_is_separate_and_cannot_prefill_userdata(self):
        chain, completed, report, inspection = self.finalization()
        process = {'pid': 123, 'start_ticks': 456, 'boot_id': '0' * 36}
        completed['candidate'] = {'process': process}
        scan_proof = {'aggregate_session_count': 3, 'aggregate_audit_count': 9}
        proof = {'aggregate_session_count': 4, 'aggregate_audit_count': 11, 'finalization_session_count': 1, 'finalization_audit_count': 2,
                 'finalizer_session_id': chain['new_viewer_session_id'], 'finalizer_device_id': 7, 'all_scanned_rows_preserved': True}
        for document in (completed, report, inspection):
            document.update(retained_scan_proof=copy.deepcopy(scan_proof), proof=copy.deepcopy(proof))
        old = {name: [] for name in (*observer.TABLE_KEYS, 'devices', 'activity_entries')}
        old['sessions'] = [{'id': '1' * 32, 'revoked_at': timestamp(1)}]
        retained = {'database': {'tables': old}}
        current = copy.deepcopy(old)
        current['sessions'].append({'id': chain['new_viewer_session_id'], 'user_id': chain['viewer_user_id'], 'kind': 'emby',
                                   'revoked_at': timestamp(2), 'device_registry_id': 7, 'device_id': 'new-finalizer'})
        current['devices'].append({'id': 7, 'reported_device_id': 'new-finalizer', 'last_user_id': chain['viewer_user_id']})
        current['activity_entries'] = [{'id': index, 'action': action, 'source': 'emby', 'actor_kind': 'user',
            'actor_id': chain['viewer_user_id'], 'actor_credential_id': chain['new_viewer_session_id'],
            'resource_kind': 'session', 'resource_id': chain['new_viewer_session_id']}
            for index, action in enumerate(('session.login', 'session.revoked'), 1)]
        documents = {'continuation_failure': {'marker': observer.CONTINUATION, 'result': 'retained_for_review', 'phase': 'scan_complete',
                     'retry_permitted': False, 'library_id': observer.CONTINUATION_LIBRARY, 'root_id': observer.CONTINUATION_ROOT_ID,
                     'job_id': observer.FINALIZATION_JOB, 'authentication': {role: {'login_status': 200, 'logout_status': 204, 'exact_status': 401}
                         for role in ('admin', 'viewer')}},
                     'continuation_state': {'phase': 'continuing_special_features_fixture', 'stage': 'scan_complete', 'process': process},
                     'continuation_snapshot': retained, 'retained_structure_proof': {'proof': scan_proof},
                     'retained_protocol_proof': {}, 'continuation_evidence': {}}
        by_path = {chain[key]['path']: value for key, value in documents.items()}

        def read(record, **kwargs):
            return b'frozen source bytes' if kwargs.get('raw') else copy.deepcopy(by_path[record['path']])

        with mock.patch.object(observer, 'read_record', side_effect=read):
            result, pin = observer.read_finalization(completed, report, inspection, {'database': {'tables': current}})
            self.assertEqual(result, retained)
            self.assertEqual(pin, observer.digest(observer.exact(chain).encode()))
            current['user_item_data'].append({'user_id': chain['viewer_user_id'], 'item_id': 'c' * 32})
            with self.assertRaisesRegex(observer.ScopeError, 'finalization_changed_user_item_data'):
                observer.read_finalization(completed, report, inspection, {'database': {'tables': current}})


if __name__ == '__main__':
    unittest.main(verbosity=2)
