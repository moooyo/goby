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
CONT = BASE = PROFILE = TRANSPORT = SETUP = None


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


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('operator', 'origin-operator', 'origin-guards', 'profile'):
        parser.add_argument('--' + name, type=Path, required=True)
        parser.add_argument('--' + name + '-sha256', required=True)
    args = parser.parse_args()
    if sys.platform != 'linux' or os.geteuid() != 0 or not os.environ.get('SSH_CONNECTION'):
        raise RuntimeError('Run the pure guards only through authorized root SSH.')
    global CONT, BASE, PROFILE, TRANSPORT, SETUP
    CONT = load(args.operator, args.operator_sha256, 'continuation_under_guard')
    BASE = load(args.origin_operator, args.origin_operator_sha256, 'retained_origin_transport')
    PROFILE = load(args.profile, args.profile_sha256, 'continuation_profile')
    TRANSPORT = load(args.origin_guards, args.origin_guards_sha256, 'frozen_transport_guards')
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
