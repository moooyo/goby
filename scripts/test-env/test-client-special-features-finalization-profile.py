#!/usr/bin/env python3
"""Pure fourth-stage gates built on the retained complete 22-item snapshot."""

import argparse
import copy
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import sys
import types
import unittest

sys.dont_write_bytecode = True
WORK = Path('/opt/goby-test/exec-work-m3e')
TARGET = OLD = SCAN = OP = STATE = ORIGINAL = CREATED = SCANNED = FAILURE = SCAN_RECEIPT = MEDIA = None


def read(path, expected):
    raw = path.read_bytes()
    if hashlib.sha256(raw).hexdigest() != expected:
        raise RuntimeError('A pinned complete guard input changed.')
    return json.loads(raw)


def load(path, expected, name):
    raw = path.read_bytes()
    if hashlib.sha256(raw).hexdigest() != expected:
        raise RuntimeError('A frozen pure source changed.')
    result = types.ModuleType(name)
    result.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), result.__dict__)
    return result


def sample():
    final = copy.deepcopy(SCANNED)
    tables = final['database']['tables']
    previous = next(row for row in tables['sessions'] if row['id'] == SCAN_RECEIPT['authentication']['viewer']['session_id'])
    viewer = copy.deepcopy(previous)
    start = dt.datetime.fromisoformat(SCANNED['database']['metadata']['captured_at']) + dt.timedelta(seconds=1)
    moment = lambda seconds: (start + dt.timedelta(seconds=seconds)).isoformat()
    device_id = SCANNED['database']['sequences']['devices_id_seq']['last_value'] + 1
    audit_id = SCANNED['database']['sequences']['activity_entries_id_seq']['last_value'] + 1
    viewer.update(id='f' * 32, token_hash='\\x' + 'e' * 64, device_id='finalization-synthetic-owned-device', device_registry_id=device_id,
                  created_at=moment(1), last_seen_at=moment(2), revoked_at=moment(3), expires_at=moment(86400), client_capabilities={})
    tables['sessions'].append(viewer)
    device = copy.deepcopy(next(row for row in tables['devices'] if row['id'] == previous['device_registry_id']))
    device.update(id=device_id, reported_device_id=viewer['device_id'], created_at=moment(1), last_seen_at=moment(2))
    tables['devices'].append(device)
    for index, action in enumerate(('session.login', 'session.revoked')):
        event = copy.deepcopy(next(row for row in tables['activity_entries'] if row['actor_credential_id'] == previous['id'] and row['action'] == action))
        event.update(id=audit_id + index, actor_credential_id=viewer['id'], resource_id=viewer['id'], created_at=moment(1 + index * 2))
        tables['activity_entries'].append(event)
    final['database']['sequences']['devices_id_seq'] = {'last_value': device_id, 'is_called': True}
    final['database']['sequences']['activity_entries_id_seq'] = {'last_value': audit_id + 1, 'is_called': True}
    receipt = {'authentication': {'viewer': {'login_status': 200, 'logout_status': 204, 'exact_status': 401,
                   'session_id': viewer['id'], 'token_sha256': 'e' * 64}}, 'device_id': viewer['device_id'],
               'client_name': viewer['client_name'], 'window': {'before': moment(0), 'after': moment(4)}}
    return final, receipt


def validate(final, receipt):
    return TARGET.validate(OP, OLD, SCAN, ORIGINAL, CREATED, SCANNED, final, STATE, FAILURE, SCAN_RECEIPT, receipt, MEDIA)


class FinalizationGates(unittest.TestCase):
    def test_01_exact_one_viewer_and_two_audits(self):
        retained, proof = validate(*sample())
        self.assertEqual((retained['aggregate_session_count'], retained['aggregate_audit_count']), (3, 9))
        self.assertEqual((proof['aggregate_session_count'], proof['aggregate_audit_count']), (4, 11))
        self.assertEqual(proof['finalizer_session_id'], 'f' * 32)

    def test_02_catalog_and_userdata_are_not_modified(self):
        for table, field, value in (('items', 'name', 'Changed'), ('libraries', 'last_scan_at', None), ('user_item_data', 'play_count', 999)):
            final, receipt = sample()
            final['database']['tables'][table][0][field] = value
            with self.subTest(table=table), self.assertRaises((TARGET.FinalizationError, OLD.ProfileError)):
                validate(final, receipt)

    def test_03_no_old_session_or_device_can_be_touched(self):
        for table, field in (('sessions', 'revoked_at'), ('devices', 'last_seen_at')):
            final, receipt = sample()
            final['database']['tables'][table][0][field] = 'changed'
            with self.subTest(table=table), self.assertRaises((TARGET.FinalizationError, OLD.ProfileError)):
                validate(final, receipt)

    def test_04_new_scope_cannot_be_admin_reused_or_unrevoked(self):
        for key, value in (('kind', 'admin'), ('device_id', 'wrong-device'), ('revoked_at', None), ('client_capabilities', {'unexpected': True})):
            final, receipt = sample()
            final['database']['tables']['sessions'][-1][key] = value
            with self.subTest(field=key), self.assertRaises((TARGET.FinalizationError, SCAN.ContinuationError)):
                validate(final, receipt)
        final, receipt = sample()
        receipt['authentication']['viewer']['exact_status'] = 200
        with self.assertRaises(SCAN.ContinuationError):
            validate(final, receipt)

    def test_05_login_count_and_sequence_increments_are_exact(self):
        final, receipt = sample()
        final['database']['tables']['activity_entries'][-2]['affected_count'] = 0
        with self.assertRaises(SCAN.ContinuationError):
            validate(final, receipt)
        for name in ('theme_owner_ids_id_seq', 'activity_entries_id_seq', 'devices_id_seq'):
            final, receipt = sample()
            final['database']['sequences'][name]['last_value'] += 1
            with self.subTest(sequence=name), self.assertRaises(SCAN.ContinuationError):
                validate(final, receipt)

    def test_06_scanned_layout_and_activity_gates(self):
        TARGET.require_quiescent(SCANNED)
        for table, field, value in (('scan_jobs', 'status', 'Running'), ('task_runs', 'state', 'waiting'),
                                   ('task_run_children', 'state', 'stopping'), ('play_sessions', 'state', 'Playing'),
                                   ('play_sessions', 'state', 'Paused')):
            changed = copy.deepcopy(SCANNED)
            row = dict.fromkeys(changed['database']['metadata']['columns'][table])
            row[field] = value
            changed['database']['tables'][table].append(row)
            with self.subTest(table=table, state=value), self.assertRaises(TARGET.FinalizationError):
                TARGET.require_quiescent(changed)
        changed = copy.deepcopy(SCANNED)
        changed['database']['tables']['encoding_jobs'] = [{}]
        with self.assertRaises(TARGET.FinalizationError):
            TARGET.require_quiescent(changed)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--profile', required=True, type=Path)
    parser.add_argument('--profile-sha256', required=True)
    parser.add_argument('--state', required=True, type=Path)
    args = parser.parse_args()
    if sys.platform != 'linux' or os.geteuid() != 0 or not os.environ.get('SSH_CONNECTION'):
        raise RuntimeError('Run pure guards only through authorized root SSH.')
    global TARGET, OLD, SCAN, OP, STATE, ORIGINAL, CREATED, SCANNED, FAILURE, SCAN_RECEIPT, MEDIA
    TARGET = load(args.profile, args.profile_sha256, 'finalization_profile')
    STATE = read(args.state, TARGET.PINS['continuation_state'])
    inputs = read(WORK / 'client-special-features-continuation-tool-03/execution-inputs.json', '8fdd6037e4bf0ca314833f3962b3253722be94319eaa6df9ef65df9b83c27adf')['args']
    SCAN = load(Path(inputs['profile-path']), TARGET.PINS['continuation_profile_source'], 'scanned_profile')
    OLD = load(Path(inputs['origin-profile-path']), SCAN.PINS['origin_profile_source'], 'original_profile')
    base = load(Path(inputs['operator-path']), inputs['operator-sha256'], 'frozen_base')
    first, second = WORK / 'client-special-features-fixture-v1', WORK / 'client-special-features-fixture-continuation-v1'
    ORIGINAL = read(first / 'before-full.json', SCAN.PINS['origin_before_snapshot'])
    CREATED = read(first / 'failed-current-full.json', SCAN.PINS['origin_current_snapshot'])
    SCANNED = read(second / 'failed-current-full.json', TARGET.PINS['continuation_snapshot'])
    FAILURE = read(first / 'failure.json', SCAN.PINS['origin_failure'])
    failed = read(second / 'failure.json', TARGET.PINS['continuation_failure'])
    before = json.loads((second / 'before-full.json').read_bytes())
    MEDIA = json.loads((second / 'media-before.json').read_bytes())
    viewer = next(row for row in SCANNED['database']['tables']['sessions'] if row['id'] == failed['authentication']['viewer']['session_id'])
    SCAN_RECEIPT = {'library_id': failed['library_id'], 'root_id': failed['root_id'], 'job_id': failed['job_id'],
        'authentication': failed['authentication'], 'device_id': viewer['device_id'], 'client_name': 'Goby Special Features Fixture Recorder',
        'window': {'before': ORIGINAL['database']['metadata']['captured_at'], 'after': SCANNED['database']['metadata']['captured_at']},
        'continuation_window': {'before': before['database']['metadata']['captured_at'], 'after': SCANNED['database']['metadata']['captured_at']}}
    catalog_path = Path(STATE['schema27_source']['source']) / 'internal/backuppg/catalogs/schema-27-postgresql-17.json'
    catalog = read(catalog_path, '1fc91c2e380805bff0f87867547d307bc7830ffeb49c3489da4e1713a5c0047d')
    OP = types.SimpleNamespace(ROLE=base.ROLE, canonical_json=base.canonical_json, equal_json=base.equal_json,
        trusted_schema_baseline=lambda *_: catalog, schema27_binding=lambda _: {}, schema26_binding=lambda _: {}, schema25_binding=lambda _: {},
        baseline_table_columns=base.baseline_table_columns, validate_theme_state=base.validate_theme_state,
        added_viewer_credentials=lambda _: SCANNED['added_viewer_credentials'])
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(FinalizationGates))
    print(json.dumps({'result': 'passed' if result.wasSuccessful() else 'failed', 'tests': result.testsRun,
        'failures': len(result.failures), 'errors': len(result.errors), 'profile_sha256': args.profile_sha256,
        'http_requests': 0, 'database_writes': 0, 'service_actions': 0}), flush=True)
    return 0 if result.wasSuccessful() else 1


if __name__ == '__main__':
    raise SystemExit(main())
