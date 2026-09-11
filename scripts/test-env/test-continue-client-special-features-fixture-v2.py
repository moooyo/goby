#!/usr/bin/env python3
"""Pure continuation transport/checkpoint gates; never contact a service."""

import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import sys
import types
import unittest

sys.dont_write_bytecode = True
CONT = BASE = PROFILE = TRANSPORT = SETUP = RETAINED = RETAINED_STATE = None
SNAPSHOT_SHA256 = '86cbffbc1b3c0d623ea894aab12843273adcce5c7b097f3b9878dfc276ee8275'
STATE_SHA256 = '6d719ab6f3cd6c13ebf9fe3d6a927abaf5040e6e87e84be5d81a57318440cefe'


def load(path, expected, name):
    raw = path.read_bytes()
    if hashlib.sha256(raw).hexdigest() != expected:
        raise RuntimeError('A selected guard source changed.')
    result = types.ModuleType(name)
    result.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), result.__dict__)
    return result


class ContinuationGates(unittest.TestCase):
    def test_01_create_delete_and_global_refresh_remain_forbidden(self):
        task = TRANSPORT.job()
        actor = TRANSPORT.prove(task, 'admin')
        for method, path, body in (('POST', '/admin/v1/libraries', BASE.create_body(task.profile)),
                                  ('DELETE', '/admin/v1/libraries/owned', None), ('POST', '/emby/Library/Refresh', {})):
            with self.subTest(path=path), self.assertRaises((CONT.ContinuationError, BASE.SetupError)):
                task.approved('create-library', method, path, actor, body, False, False, None)
        task.library_id = PROFILE.LIBRARY_ID
        task.approved('scan-library', 'POST', '/admin/v1/libraries/' + PROFILE.LIBRARY_ID + '/scan', actor, {}, False, False, None)
        with self.assertRaises(BASE.SetupError):
            task.approved('scan-library', 'POST', '/admin/v1/libraries/foreign/scan', actor, {}, False, False, None)

    def test_02_all_actual_checkpoints_write_only_new_hyphen_names(self):
        task = TRANSPORT.job()
        task.cont = PROFILE
        task.history_state = copy.deepcopy(task.state)
        task.authority = {'origin_failure': {'path': str(CONT.ORIGIN / 'failure.json'), 'sha256': PROFILE.PINS['origin_failure']}}
        task.library_id, task.root_id = PROFILE.LIBRARY_ID, PROFILE.ROOT_ID
        task.state_bytes = b'prior-state'
        events = []
        task.op.STATE_FILE = Path('/private/state.json')
        task.op.equal_json = lambda a, b: a == b
        task.op.read = lambda _: task.state_bytes

        def create(path, value):
            self.assertEqual(path.parent, CONT.OUTPUT / 'private')
            self.assertNotIn('_', path.name)
            self.assertNotEqual(path.parent, CONT.ORIGIN / 'private')
            events.append(('evidence', path.name))

        def save(state):
            events.append(('state', state['stage']))

        task.op.create, task.op.save_state = create, save
        task.private = lambda name, value: SETUP.private(task, name, value)
        for phase in PROFILE.PHASE_FILES:
            if phase == 'scan_acknowledged':
                task.job_id = 'f' * 32
            offset = len(events)
            task.checkpoint(phase)
            name = PROFILE.PHASE_FILES[phase]
            self.assertEqual(events[offset:], [('evidence', name.replace('.json', '-intent.json')), ('state', phase), ('evidence', name)])
        previous = list(events)
        with self.assertRaises(CONT.ContinuationError):
            task.checkpoint('unsupported_phase')
        self.assertEqual(events, previous)

    def test_03_readonly_comparison_ignores_only_capture_clock(self):
        op = types.SimpleNamespace(equal_json=lambda a, b: a == b)
        left = {'schema': 27, 'runtime_sha256': 'r', 'browser_sha256': 'b', 'added_viewer_credentials': {}, 'recovery': {},
                'database': {'tables': {'items': [{'id': 'owned'}]}, 'sequences': {'one': 7}, 'catalog': [], 'unsupported': False,
                'metadata': dict.fromkeys(('database', 'server_version_num', 'schemas', 'public_schema', 'relations', 'columns'))}}
        right = copy.deepcopy(left)
        left['database']['metadata']['captured_at'], right['database']['metadata']['captured_at'] = 'earlier', 'later'
        CONT.preserved(op, left, right)
        for key in ('tables', 'sequences', 'catalog', 'unsupported'):
            mutated = copy.deepcopy(right)
            mutated['database'][key] = 'changed'
            with self.subTest(key=key), self.assertRaises(CONT.ContinuationError):
                CONT.preserved(op, left, mutated)

    def test_04_original_module_paths_and_outputs_are_unchanged(self):
        self.assertEqual(BASE.OUTPUT, CONT.ORIGIN)
        self.assertNotEqual(BASE.OUTPUT, CONT.OUTPUT)
        self.assertEqual(Path(BASE.__file__).name, 'prepare-client-special-features-fixture.py')
        self.assertEqual(PROFILE.PINS['origin_setup_source'], '84af95f1d34227dc9c465b73545c6de6939963d44fb3fc2d2df50e7f0f759465')

    def test_05_actual_fourteen_item_four_library_snapshot(self):
        tables = RETAINED['database']['tables']
        self.assertEqual((len(tables), len(tables['items']), len(tables['libraries'])), (35, 14, 4))
        before = copy.deepcopy(RETAINED)
        CONT.require_continuation_quiescent(RETAINED, RETAINED_STATE, PROFILE)
        self.assertEqual(RETAINED, before)

    def test_06_layout_identity_cannot_be_replaced_or_projected_away(self):
        for case in ('old_layout', 'foreign_library', 'foreign_root', 'published_child'):
            current = copy.deepcopy(RETAINED)
            tables = current['database']['tables']
            if case == 'old_layout':
                tables['items'] = [row for row in tables['items'] if row['id'] != PROFILE.LIBRARY_ID]
                tables['libraries'] = [row for row in tables['libraries'] if row['id'] != PROFILE.LIBRARY_ID]
            elif case == 'foreign_library':
                next(row for row in tables['libraries'] if row['id'] == PROFILE.LIBRARY_ID)['id'] = 'f' * 32
            elif case == 'foreign_root':
                next(row for row in tables['library_roots'] if row['id'] == PROFILE.ROOT_ID)['path'] = '/foreign'
            else:
                next(row for row in tables['items'] if row['id'] == PROFILE.LIBRARY_ID)['type'] = 'Movie'
            with self.subTest(case=case), self.assertRaises(CONT.ContinuationError):
                CONT.require_continuation_quiescent(current, RETAINED_STATE, PROFILE)

    def test_07_missing_or_nonempty_encoding_jobs_is_rejected(self):
        for missing in (True, False):
            current = copy.deepcopy(RETAINED)
            if missing:
                current['database']['tables'].pop('encoding_jobs')
            else:
                current['database']['tables']['encoding_jobs'].append(self.empty_row('encoding_jobs'))
            with self.subTest(missing=missing), self.assertRaises(CONT.ContinuationError):
                CONT.require_continuation_quiescent(current, RETAINED_STATE, PROFILE)

    def empty_row(self, table):
        return dict.fromkeys(RETAINED['database']['metadata']['columns'][table])

    def test_08_scan_and_task_activity_fences_are_all_preserved(self):
        for table, field in (('scan_jobs', 'status'), ('task_runs', 'state'), ('task_run_children', 'state')):
            for state in ('waiting', 'pending', 'queued', 'running', 'stopping'):
                current = copy.deepcopy(RETAINED)
                row = self.empty_row(table)
                row[field] = state.upper()
                current['database']['tables'][table].append(row)
                with self.subTest(table=table, state=state), self.assertRaises(CONT.ContinuationError):
                    CONT.require_continuation_quiescent(current, RETAINED_STATE, PROFILE)
        current = copy.deepcopy(RETAINED)
        row = self.empty_row('scan_jobs')
        row.update(library_id=PROFILE.LIBRARY_ID, status='Completed')
        current['database']['tables']['scan_jobs'].append(row)
        with self.assertRaises(CONT.ContinuationError):
            CONT.require_continuation_quiescent(current, RETAINED_STATE, PROFILE)

    def test_09_playing_and_paused_remain_forbidden(self):
        for state in ('Playing', 'Paused'):
            current = copy.deepcopy(RETAINED)
            row = self.empty_row('play_sessions')
            row['state'] = state
            current['database']['tables']['play_sessions'].append(row)
            with self.subTest(state=state), self.assertRaises(CONT.ContinuationError):
                CONT.require_continuation_quiescent(current, RETAINED_STATE, PROFILE)

    def test_10_prepared_requires_the_exact_revoked_ordinary_credential(self):
        current = copy.deepcopy(RETAINED)
        credential = next(row for row in current['database']['tables']['sessions'] if row['kind'] == 'emby' and row['revoked_at'] is not None)
        row = self.empty_row('play_sessions')
        row.update(state='Prepared', user_id=credential['user_id'], auth_session_id=credential['id'], started_at=None,
                   counted=False, application_client_id=None)
        current['database']['tables']['play_sessions'].append(row)
        CONT.require_continuation_quiescent(current, RETAINED_STATE, PROFILE)
        for field, value in (('started_at', '2026-09-12T00:00:00Z'), ('counted', True), ('application_client_id', 'foreign'),
                             ('auth_session_id', 'unowned'), ('user_id', 'unowned')):
            changed = copy.deepcopy(current)
            changed['database']['tables']['play_sessions'][-1][field] = value
            with self.subTest(field=field), self.assertRaises(CONT.ContinuationError):
                CONT.require_continuation_quiescent(changed, RETAINED_STATE, PROFILE)
        for field, value in (('kind', 'admin'), ('revoked_at', None)):
            changed = copy.deepcopy(current)
            target = next(row for row in changed['database']['tables']['sessions'] if row['id'] == credential['id'])
            target[field] = value
            with self.subTest(credential_field=field), self.assertRaises(CONT.ContinuationError):
                CONT.require_continuation_quiescent(changed, RETAINED_STATE, PROFILE)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('operator', 'origin-operator', 'origin-guards', 'profile', 'retained-snapshot', 'retained-state'):
        parser.add_argument('--' + name, type=Path, required=True)
        parser.add_argument('--' + name + '-sha256', required=True)
    args = parser.parse_args()
    if sys.platform != 'linux' or os.geteuid() != 0 or not os.environ.get('SSH_CONNECTION'):
        raise RuntimeError('Run the pure guards only through authorized root SSH.')
    global CONT, BASE, PROFILE, TRANSPORT, SETUP, RETAINED, RETAINED_STATE
    CONT = load(args.operator, args.operator_sha256, 'continuation_under_guard')
    BASE = load(args.origin_operator, args.origin_operator_sha256, 'retained_origin_transport')
    PROFILE = load(args.profile, args.profile_sha256, 'continuation_profile')
    TRANSPORT = load(args.origin_guards, args.origin_guards_sha256, 'frozen_transport_guards')
    for path, supplied, expected in ((args.retained_snapshot, args.retained_snapshot_sha256, SNAPSHOT_SHA256),
                                     (args.retained_state, args.retained_state_sha256, STATE_SHA256)):
        if supplied != expected or hashlib.sha256(path.read_bytes()).hexdigest() != expected:
            raise RuntimeError('A retained complete snapshot or historical state changed.')
    RETAINED = json.loads(args.retained_snapshot.read_bytes())
    RETAINED_STATE = json.loads(args.retained_state.read_bytes())
    SETUP = CONT.make_setup(BASE)
    TRANSPORT.OP = types.SimpleNamespace(Setup=SETUP, Actor=BASE.Actor, SetupError=(BASE.SetupError, CONT.ContinuationError),
        require=CONT.require, http=BASE.http, signal=BASE.signal, JSON_LIMIT=BASE.JSON_LIMIT)
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(ContinuationGates)
    # Re-run the original ACK/journal/cleanup and request-bounds gates against
    # the real continuation subclass, preserving the already frozen sources.
    for name in unittest.defaultTestLoader.getTestCaseNames(TRANSPORT.Gates):
        if int(name.split('_')[1]) >= 3:
            suite.addTest(TRANSPORT.Gates(name))
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    print(json.dumps({'result': 'passed' if result.wasSuccessful() else 'failed', 'tests': result.testsRun,
        'failures': len(result.failures), 'errors': len(result.errors), 'http_requests': 0, 'database_writes': 0,
        'service_actions': 0, 'operator_sha256': args.operator_sha256}), flush=True)
    return 0 if result.wasSuccessful() else 1


if __name__ == '__main__':
    raise SystemExit(main())
