#!/usr/bin/env python3
"""Pure continuation gates using the already pinned complete catalog model."""

import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import sys
import types
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
TARGET = MODEL = BASE_PROFILE = OBSERVED = None
OBSERVED_SHA256 = '38ff26fdaceff5bf2e0673846c4dfb8de6ec9c52ad6f5985da3c758c2a3858b8'


def sample():
    state, original, final, receipt, media = MODEL.model()
    replacements = {receipt['library_id']: TARGET.LIBRARY_ID, receipt['root_id']: TARGET.ROOT_ID}

    def replace(value):
        if isinstance(value, dict):
            return {key: replace(child) for key, child in value.items()}
        if isinstance(value, list):
            return [replace(child) for child in value]
        return replacements.get(value, value) if isinstance(value, str) else value

    final, receipt = replace(final), replace(receipt)
    tables = final['database']['tables']
    failed = copy.deepcopy(original)
    old = failed['database']['tables']
    for name, key in (('libraries', 'id'), ('library_roots', 'library_id'), ('items', 'id'),
                      ('item_metadata_state', 'item_id'), ('theme_owner_ids', 'item_id')):
        old[name].extend(copy.deepcopy(row) for row in tables[name] if row[key] == TARGET.LIBRARY_ID)
    old['libraries'][-1]['last_scan_at'] = None
    creator = copy.deepcopy(next(row for row in tables['sessions'] if row['kind'] == 'admin'))
    creator.update(id='9' * 32, token_hash='\\x' + '8' * 64)
    old['sessions'].append(copy.deepcopy(creator))
    tables['sessions'].append(creator)
    original_events = tables['activity_entries']
    created = copy.deepcopy(next(row for row in original_events if row['action'] == 'library.created'))
    created['actor_credential_id'] = creator['id']
    creation_events = []
    for action in ('session.login', 'session.revoked'):
        event = copy.deepcopy(next(row for row in original_events if row['action'] == action and row['actor_id'] == state['admin_id']))
        event.update(actor_credential_id=creator['id'], resource_id=creator['id'])
        creation_events.append(event)
    creation_events.append(created)
    continued_events = [row for row in original_events if row['action'] != 'library.created']
    tables['activity_entries'] = creation_events + continued_events
    for index, event in enumerate(tables['activity_entries']):
        event['id'] = 21 + index
        if event['action'] == 'session.login':
            event['affected_count'] = 1
    old['activity_entries'].extend(copy.deepcopy(creation_events))
    failed['database']['sequences'].update(theme_owner_ids_id_seq={'last_value': 15, 'is_called': True},
                                         activity_entries_id_seq={'last_value': 23, 'is_called': True})
    final['database']['sequences']['activity_entries_id_seq'] = {'last_value': 29, 'is_called': True}
    failed['database']['metadata']['captured_at'] = '2026-09-12T00:01:30Z'
    final['database']['metadata']['captured_at'] = '2026-09-12T00:02:00Z'
    receipt['continuation_window'] = copy.deepcopy(receipt['window'])
    failure = {'result': 'retained_for_review', 'phase': 'library_acknowledged', 'library_id': TARGET.LIBRARY_ID,
        'root_id': None, 'job_id': None, 'old_rows_preserved': True, 'retry_permitted': False,
        'authentication': {'admin': {'login_status': 200, 'logout_status': 204, 'exact_status': 401, 'token_sha256': '8' * 64},
                           'viewer': {'login_status': None, 'logout_status': None, 'exact_status': None}}}
    return MODEL.OP, BASE_PROFILE, original, failed, final, state, failure, receipt, media


class ContinuationGates(unittest.TestCase):
    def setUp(self):
        for owner, name in ((socket, 'socket'), (subprocess, 'run'), (os, 'replace'), (MODEL.BASE, 'create'), (MODEL.BASE, 'postgres')):
            self.enterContext(patch.object(owner, name, side_effect=AssertionError('Unexpected external effect.')))

    def test_01_three_stage_positive(self):
        result = TARGET.validate_transition(*sample())
        self.assertEqual((result['aggregate_session_count'], result['aggregate_audit_count']), (3, 9))
        self.assertEqual(result['creation_admin_session_id'], '9' * 32)
        self.assertNotEqual(result['creation_admin_session_id'], result['native_session_id'])

    def test_02_origin_must_be_create_only_and_closed(self):
        changes = (lambda x: x[6].update(job_id='f' * 32), lambda x: x[6]['authentication']['admin'].update(exact_status=200),
                   lambda x: x[3]['database']['tables']['libraries'][-1].update(last_scan_at=MODEL.NOW),
                   lambda x: x[6]['authentication']['viewer'].update(login_status=200),
                   lambda x: x[3]['database']['tables']['scan_jobs'].append(copy.deepcopy(x[4]['database']['tables']['scan_jobs'][-1])))
        for index, change in enumerate(changes):
            values = sample()
            change(values)
            with self.subTest(case=index), self.assertRaises((TARGET.ContinuationError, BASE_PROFILE.ProfileError)):
                TARGET.validate_transition(*values)

    def test_03_only_owned_library_last_scan_at_may_change(self):
        changes = (lambda x: x[4]['database']['tables']['libraries'][0].update(last_scan_at=MODEL.NOW),
                   lambda x: x[4]['database']['tables']['libraries'][-1].update(name='Unowned replacement'),
                   lambda x: x[4]['database']['tables']['libraries'][-1].update(last_scan_at=MODEL.OLD),
                   lambda x: x[4]['database']['tables']['library_roots'][-1].update(path='/foreign'))
        for index, change in enumerate(changes):
            values = sample()
            change(values)
            with self.subTest(case=index), self.assertRaises((TARGET.ContinuationError, BASE_PROFILE.ProfileError)):
                TARGET.validate_transition(*values)

    def test_04_creation_actor_cannot_be_reused_or_rewritten(self):
        for column in ('revoked_at', 'last_seen_at'):
            values = sample()
            target = next(row for row in values[4]['database']['tables']['sessions'] if row['id'] == '9' * 32)
            target[column] = None
            with self.subTest(column=column), self.assertRaises((TARGET.ContinuationError, BASE_PROFILE.ProfileError)):
                TARGET.validate_transition(*values)
        values = sample()
        values[7]['authentication']['admin']['token_sha256'] = '8' * 64
        with self.assertRaises(TARGET.ContinuationError):
            TARGET.validate_transition(*values)

    def test_05_nine_audits_keep_two_distinct_administrator_roles(self):
        for action in ('library.created', 'scan.requested'):
            values = sample()
            event = next(row for row in values[4]['database']['tables']['activity_entries'] if row['action'] == action)
            event['actor_credential_id'] = '9' * 32 if action == 'scan.requested' else values[7]['authentication']['admin']['session_id']
            with self.subTest(action=action), self.assertRaises((TARGET.ContinuationError, BASE_PROFILE.ProfileError)):
                TARGET.validate_transition(*values)

    def test_06_old_userdata_sequences_and_no_new_preparation(self):
        for change in (lambda x: x[4]['database']['tables']['user_item_data'][0].update(play_count=99),
                       lambda x: x[4]['database']['sequences']['activity_entries_id_seq'].update(last_value=27),
                       lambda x: x[4]['database']['tables']['user_item_data'].append(copy.deepcopy(x[4]['database']['tables']['user_item_data'][0]))):
            values = sample()
            change(values)
            with self.assertRaises((TARGET.ContinuationError, BASE_PROFILE.ProfileError)):
                TARGET.validate_transition(*values)

    def test_07_every_real_checkpoint_filename_is_admitted(self):
        self.assertEqual(set(TARGET.PHASE_FILES), {'continuation_prepared', 'scan_acknowledged', 'scan_complete', 'protocol_complete'})
        self.assertEqual(len(set(TARGET.PHASE_FILES.values())), 4)
        for name in TARGET.PHASE_FILES.values():
            self.assertIsNotNone(re.fullmatch('[a-z0-9-]+[.](json|bin)', name))
            self.assertNotIn('_', name)

    def test_08_retained_actual_creation_audits_and_zero_count_rejection(self):
        actor = '0dd576d477e8acea871cb4b06cb11153'
        credential = '8152df67289b37d77cb8278cc6414604'
        expected = [('session.login', 'native', 'user', actor, credential, 'session', credential),
                    ('library.created', 'native', 'user', actor, credential, 'library', TARGET.LIBRARY_ID),
                    ('session.revoked', 'native', 'user', actor, credential, 'session', credential)]
        self.assertEqual([row['id'] for row in OBSERVED['new_audits']], [127, 128, 129])
        self.assertEqual([row['affected_count'] for row in OBSERVED['new_audits']], [1, 1, 1])
        TARGET.audits(BASE_PROFILE, OBSERVED['new_audits'], expected, OBSERVED['window'])
        changed = copy.deepcopy(OBSERVED['new_audits'])
        changed[0]['affected_count'] = 0
        with self.assertRaises(TARGET.ContinuationError):
            TARGET.audits(BASE_PROFILE, changed, expected, OBSERVED['window'])
        for role in ('admin', 'viewer'):
            values = sample()
            session_id = values[7]['authentication'][role]['session_id']
            row = next(row for row in values[4]['database']['tables']['activity_entries'] if
                       row['action'] == 'session.login' and row['actor_credential_id'] == session_id)
            row['affected_count'] = 0
            with self.subTest(role=role), self.assertRaises(TARGET.ContinuationError):
                TARGET.validate_transition(*values)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('profile', 'origin-profile', 'origin-guards', 'fixture-operator', 'catalog', 'observed-audits'):
        parser.add_argument('--' + name, required=True, type=Path)
        parser.add_argument('--' + name + '-sha256', required=True)
    args = parser.parse_args()
    if sys.platform != 'linux' or os.geteuid() != 0 or not os.environ.get('SSH_CONNECTION'):
        raise RuntimeError('Run the pure guards only through authorized root SSH.')
    global TARGET, MODEL, BASE_PROFILE, OBSERVED

    def load(path, digest, name):
        raw = path.read_bytes()
        if hashlib.sha256(raw).hexdigest() != digest:
            raise RuntimeError('A frozen pure guard source changed.')
        result = types.ModuleType(name)
        result.__file__ = str(path)
        exec(compile(raw, str(path), 'exec'), result.__dict__)
        return result

    TARGET = load(args.profile, args.profile_sha256, 'continuation_profile')
    BASE_PROFILE = load(args.origin_profile, args.origin_profile_sha256, 'origin_profile')
    MODEL = load(args.origin_guards, args.origin_guards_sha256, 'complete_catalog_guard_model')
    MODEL.PROFILE, MODEL.BASE = BASE_PROFILE, load(args.fixture_operator, args.fixture_operator_sha256, 'base_fixture')
    raw = args.catalog.read_bytes()
    if hashlib.sha256(raw).hexdigest() != args.catalog_sha256 or args.catalog_sha256 != MODEL.CATALOG_SHA:
        raise RuntimeError('The generated complete schema27 catalog changed.')
    MODEL.CATALOG = MODEL.BASE.precise_json(raw)
    MODEL.COLUMNS = MODEL.BASE.baseline_table_columns(MODEL.CATALOG)
    raw = args.observed_audits.read_bytes()
    if args.observed_audits_sha256 != OBSERVED_SHA256 or hashlib.sha256(raw).hexdigest() != OBSERVED_SHA256:
        raise RuntimeError('The independently observed retained creation audit input changed.')
    OBSERVED = MODEL.BASE.precise_json(raw)
    MODEL.OP = types.SimpleNamespace(ROLE=MODEL.BASE.ROLE, canonical_json=MODEL.BASE.canonical_json, equal_json=MODEL.BASE.equal_json,
        trusted_schema_baseline=lambda *_: MODEL.CATALOG, schema27_binding=lambda _: {}, schema26_binding=lambda _: {}, schema25_binding=lambda _: {},
        baseline_table_columns=MODEL.BASE.baseline_table_columns, validate_theme_state=MODEL.BASE.validate_theme_state,
        added_viewer_credentials=lambda state: state['credentials_binding'])
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(ContinuationGates))
    print(json.dumps({'tests': result.testsRun, 'failures': len(result.failures), 'errors': len(result.errors),
        'result': 'passed' if result.wasSuccessful() else 'failed', 'profile_sha256': args.profile_sha256,
        'http_requests': 0, 'database_writes': 0, 'service_actions': 0}), flush=True)
    return 0 if result.wasSuccessful() else 1


if __name__ == '__main__':
    raise SystemExit(main())
