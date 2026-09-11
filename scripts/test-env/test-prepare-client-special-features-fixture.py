#!/usr/bin/env python3
"""Pure in-memory gates for the dedicated nonempty-fixture operator.

These gates use synthetic acknowledgements, mocked HTTP connections and pinned
source bytes. They do not access a server, PostgreSQL, media or service controls,
and do not replace the separately verified full-profile/catalog guards.
"""

from __future__ import annotations

import argparse
import copy
import hashlib
import json
from pathlib import Path
import sys
import types
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
OP = None


def encoded(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':')).encode()


class Response:
    def __init__(self, status=200, body=None, cookie=None, *, mime='application/json', length=None, truncate=False):
        self.status = status
        self.raw = body if isinstance(body, bytes) else b'' if body is None else encoded(body)
        self.headers = {'Content-Type': mime, 'Content-Length': str(len(self.raw)) if length is None else str(length)}
        if cookie is not None:
            self.headers['Set-Cookie'] = cookie
        self.truncate = truncate

    def getheader(self, key):
        return self.headers.get(key)

    def read(self, limit):
        return self.raw[:max(0, len(self.raw) - 1)] if self.truncate else self.raw[:limit]


class Wire:
    def __init__(self, replies):
        self.replies = list(replies)
        self.sent = []
        self.closed = 0

    def connection(self, host, port, timeout):
        if host != '127.0.0.1' or port != 18198 or timeout != 12:
            raise AssertionError('The transport left its fixed candidate endpoint.')
        owner = self

        class Connection:
            def request(self, method, path, payload, headers):
                owner.sent.append((method, path, payload, headers))

            def getresponse(self):
                if not owner.replies:
                    raise AssertionError('An unplanned mock request was sent.')
                return owner.replies.pop(0)

            def close(self):
                owner.closed += 1

        return Connection()


def job():
    result = OP.Setup(types.SimpleNamespace())
    result.op = types.SimpleNamespace(PORT=18198, PUBLIC='http://127.0.0.1:18196', canonical_json=encoded,
                                      precise_json=json.loads)
    result.profile = types.SimpleNamespace(LIBRARY_NAME='M3e Client Special Features Movies',
        ROOT='/opt/goby-fixtures/client-special-features-m3e-v1/Movies')
    result.state = {'admin_id': 'a' * 32, 'viewer_id': 'b' * 32, 'server_id': 'c' * 32,
                    'added_viewer': {'user_id': 'd' * 32}}
    result.checks, result.saved = [], {}
    result.pin_valid = True

    def check(*, cleanup=False):
        result.checks.append(cleanup)
        OP.require(result.pin_valid, 'A synthetic pinned candidate identity changed.')

    def private(name, value):
        if name in result.saved:
            raise AssertionError('An exclusive evidence name was reused.')
        result.saved[name] = copy.deepcopy(value)

    result.check, result.private = check, private
    result.actors = {role: OP.Actor(result, role, {'username': role + '-owned', 'password': role + '-synthetic-password'})
                     for role in ('admin', 'viewer')}
    return result


def acknowledgement(task, role):
    actor = task.actors[role]
    if role == 'admin':
        return {'User': {'Id': task.state['admin_id'], 'Name': actor.user['username'], 'IsAdministrator': True,
                         'IsDisabled': False}, 'CSRFToken': 'synthetic-csrf'}, 'goby_session=' + 'A' * 43 + '; Path=/admin; HttpOnly'
    return {'AccessToken': 'B' * 43, 'ServerId': task.state['server_id'],
            'User': {'Id': task.state['added_viewer']['user_id'], 'Name': actor.user['username'], 'Policy': {'IsAdministrator': False}},
            'SessionInfo': {'Id': 'e' * 32, 'UserId': task.state['added_viewer']['user_id'], 'DeviceId': task.device}}, None


def prove(task, role='viewer'):
    actor = task.actors[role]
    actor.acknowledge(200, *acknowledgement(task, role))
    actor.status['login_status'] = 200
    return actor


def chain():
    args = types.SimpleNamespace(candidate_pid=748513, candidate_start_ticks=6996875, candidate_boot_id='fixed-boot',
        candidate_sha256='1' * 64, extension_dir=Path('/opt/goby-test/exec-work-m3e/client-special-features-root-extension-v1'),
        extension_completed_sha256='2' * 64, extension_operator_sha256='3' * 64, operator_sha256='4' * 64,
        upgrade_completed_sha256='5' * 64, upgrade_report_sha256='6' * 64)
    old = {'pid': 746709, 'start_ticks': 6930051, 'boot_id': args.candidate_boot_id}
    current = {'pid': args.candidate_pid, 'start_ticks': args.candidate_start_ticks, 'boot_id': args.candidate_boot_id}
    upgraded = dict.fromkeys(OP.UPGRADE_REPORT_KEYS)
    upgraded.update(phase='complete', to_schema=27, to_sha256=args.candidate_sha256, new_process=old,
                    after_preservation={'runtime_sha256': '7' * 64})
    state = {'marker': 'base-fixture', 'phase': 'ready', 'stage': 'complete', 'schema': 27, 'process': current,
             'binary_sha256': args.candidate_sha256, 'runtime_sha256': '8' * 64, 'upgrade': copy.deepcopy(upgraded)}
    extension = {'marker': 'goby-client-special-features-root-extension-v1', 'phase': 'complete', 'schema': 27,
        'added_root': '/opt/goby-fixtures/client-special-features-m3e-v1/Movies',
        'script_sha256': args.extension_operator_sha256, 'operator_sha256': args.operator_sha256,
        'upgrade_completed_sha256': args.upgrade_completed_sha256, 'upgrade_report_sha256': args.upgrade_report_sha256,
        'old_process': old, 'new_process': current, 'binary_sha256': args.candidate_sha256,
        'old_runtime_sha256': '7' * 64, 'new_runtime_sha256': '8' * 64,
        'old_database_rows_and_sequences_preserved': True, 'other_runtime_bytes_preserved': True,
        'credentials_recovery_and_all_media_preserved': True, 'http_requests': 0, 'library_creates': 0, 'scan_dispatches': 0}
    state['media_root_extension'] = {key: extension[key] for key in ('marker', 'phase', 'added_root', 'new_process')}
    state['media_root_extension'].update(receipt_path=str(args.extension_dir / 'completed.json'),
                                         receipt_sha256=args.extension_completed_sha256)
    report = {'marker': 'base-fixture', 'phase': 'ready', 'result': 'ready', 'schema': 27, 'process': old,
              'binary_sha256': args.candidate_sha256, 'upgrade': copy.deepcopy(upgraded)}
    return types.SimpleNamespace(MARKER='base-fixture', equal_json=lambda a, b: a == b), state, extension, {'result': 'passed', 'completed': copy.deepcopy(extension)}, upgraded, report, args


class Gates(unittest.TestCase):
    def wire(self, value):
        self.enterContext(patch.object(OP.http.client, 'HTTPConnection', value.connection))
        self.enterContext(patch.object(OP.signal, 'setitimer', lambda *_: None))

    def test_01_exact_create_and_mutation_scope(self):
        task = job()
        actor = prove(task, 'admin')
        body = OP.create_body(task.profile)
        self.assertEqual(body, {'Name': task.profile.LIBRARY_NAME, 'CollectionType': 'movies', 'Paths': [task.profile.ROOT], 'Scan': False})
        task.approved('create-library', 'POST', '/admin/v1/libraries', actor, body, False, False, None)
        for changed in (dict(body, Scan=True), dict(body, Paths=['/unowned']), dict(body, Other=True)):
            with self.subTest(body=changed), self.assertRaises(OP.SetupError):
                task.approved('create-library', 'POST', '/admin/v1/libraries', actor, changed, False, False, None)
        for method, route in (('POST', '/emby/Items/owned/PlaybackInfo'), ('DELETE', '/admin/v1/libraries/owned'),
                              ('POST', '/emby/Library/Refresh'), ('POST', '/admin/v1/users')):
            with self.subTest(route=route), self.assertRaises(OP.SetupError):
                task.approved('forbidden', method, route, actor, {}, False, False, None)
        task.library_id = 'f' * 32
        task.approved('scan-library', 'POST', '/admin/v1/libraries/' + task.library_id + '/scan', actor, {}, False, False, None)
        with self.assertRaises(OP.SetupError):
            task.approved('scan-library', 'POST', '/admin/v1/libraries/foreign/scan', actor, {}, False, False, None)

    def test_02_upgrade_projection_and_extension_chain(self):
        OP.validate_extension_chain(*chain())
        mutations = (lambda x: x[5].update(upgrade={}), lambda x: x[5]['upgrade'].pop('schema_artifacts'),
            lambda x: x[5].update(process=x[1]['process']), lambda x: x[5].update(binary_sha256='9' * 64),
            lambda x: x[2].update(old_process=x[1]['process']), lambda x: x[2].update(new_runtime_sha256='9' * 64),
            lambda x: x[2].update(added_root='/other'), lambda x: x[2].update(upgrade_completed_sha256='9' * 64),
            lambda x: x[1].update(start_pending=True), lambda x: x[1]['media_root_extension'].update(receipt_sha256='9' * 64))
        for number, change in enumerate(mutations):
            values = chain()
            change(values)
            with self.subTest(case=number), self.assertRaises(OP.SetupError):
                OP.validate_extension_chain(*values)

    def test_03_owned_acknowledgements_and_exact_cleanup(self):
        for role in ('admin', 'viewer'):
            with self.subTest(role=role):
                task = job()
                body, cookie = acknowledgement(task, role)
                wire = Wire([Response(200, body, cookie), Response(204), Response(401, {'ErrorCode': 'unauthorized'})])
                with patch.object(OP.http.client, 'HTTPConnection', wire.connection), patch.object(OP.signal, 'setitimer', lambda *_: None):
                    actor = task.actors[role]
                    actor.login()
                    actor.logout()
                    actor.logout()
                self.assertTrue(actor.proven and actor.closed)
                self.assertEqual(actor.status['logout_status'], 204)
                self.assertEqual(actor.status['exact_status'], 401)
                self.assertEqual(len(wire.sent), 3)
                self.assertEqual(wire.sent[-2][3].get('Cookie', wire.sent[-2][3].get('X-Emby-Token')),
                                 wire.sent[-1][3].get('Cookie', wire.sent[-1][3].get('X-Emby-Token')))

    def test_04_unproven_ack_never_uses_unknown_token(self):
        for role, field in (('admin', 'Id'), ('admin', 'IsAdministrator'), ('admin', 'IsDisabled'),
                            ('viewer', 'ServerId'), ('viewer', 'UserId'), ('viewer', 'DeviceId'), ('viewer', 'Id')):
            with self.subTest(role=role, field=field):
                task = job()
                value, cookie = acknowledgement(task, role)
                if role == 'admin':
                    value['User'][field] = 'wrong' if field == 'Id' else field == 'IsDisabled'
                elif field == 'ServerId':
                    value[field] = 'wrong'
                else:
                    value['SessionInfo'][field] = 'wrong'
                actor = task.actors[role]
                actor.acknowledge(200, value, cookie)
                wire = Wire([])
                with patch.object(OP.http.client, 'HTTPConnection', wire.connection), self.assertRaises(OP.SetupError):
                    actor.logout()
                self.assertFalse(actor.proven)
                self.assertEqual(wire.sent, [])

    def test_05_ack_write_failure_keeps_owned_cleanup_eligible(self):
        task = job()
        original = task.private

        def fail_ack(name, value):
            if name.endswith('login-ack.json'):
                raise OSError('Synthetic journal write failure.')
            original(name, value)

        task.private = fail_ack
        body, cookie = acknowledgement(task, 'viewer')
        wire = Wire([Response(200, body, cookie), Response(204), Response(401)])
        self.wire(wire)
        with self.assertRaises(OP.SetupError):
            task.actors['viewer'].login()
        actor = task.actors['viewer']
        self.assertTrue(actor.proven)
        actor.logout()
        self.assertTrue(actor.closed)
        self.assertEqual(len(wire.sent), 3)
        self.assertEqual(len(task.journal_failures), 1)
        with self.assertRaises(OP.SetupError):
            task.finish()

    def test_06_business_intent_write_failure_sends_no_business_request(self):
        task = job()
        actor = prove(task)
        original = task.private

        def fail_intent(name, value):
            if name.endswith('owned-read-intent.json'):
                raise OSError('Synthetic intent write failure.')
            original(name, value)

        task.private = fail_intent
        task.allowed.add(('viewer', '/owned', None))
        wire = Wire([Response(204), Response(401)])
        self.wire(wire)
        with self.assertRaises(OSError):
            task.request('owned-read', 'GET', '/owned', actor)
        actor.logout()
        self.assertTrue(actor.closed)
        self.assertEqual([row[1] for row in wire.sent], ['/emby/Sessions/Logout', '/emby/System/Info'])
        with self.assertRaises(OP.SetupError):
            task.request('owned-read', 'GET', '/owned', actor)

    def test_07_business_response_write_failure_does_not_repeat_request(self):
        task = job()
        actor = prove(task)
        original = task.private

        def fail_response(name, value):
            if name.endswith('owned-read-response.json'):
                raise OSError('Synthetic response write failure.')
            original(name, value)

        task.private = fail_response
        task.allowed.add(('viewer', '/owned', None))
        wire = Wire([Response(200, {'Id': 'owned'}), Response(204), Response(401)])
        self.wire(wire)
        with self.assertRaises(OSError):
            task.request('owned-read', 'GET', '/owned', actor)
        actor.logout()
        self.assertTrue(actor.closed)
        self.assertEqual(len(wire.sent), 3)
        self.assertEqual([row[1] for row in wire.sent].count('/owned'), 1)

    def test_08_cleanup_journal_failure_still_proves_401_but_cannot_pass(self):
        task = job()
        actor = prove(task)
        task.private = lambda *_: (_ for _ in ()).throw(OSError('Synthetic disk failure.'))
        wire = Wire([Response(204), Response(401)])
        self.wire(wire)
        actor.logout()
        self.assertTrue(actor.closed)
        self.assertEqual(len(task.journal_failures), 4)
        self.assertEqual([actor.status['logout_status'], actor.status['exact_status']], [204, 401])
        self.assertTrue(all(task.checks))
        with self.assertRaises(OP.SetupError):
            task.finish()

    def test_09_changed_service_pin_blocks_even_cleanup(self):
        task = job()
        actor = prove(task)
        task.pin_valid = False
        wire = Wire([])
        self.wire(wire)
        with self.assertRaises(OP.SetupError):
            actor.logout()
        self.assertEqual(wire.sent, [])
        self.assertFalse(actor.closed)

    def test_10_incomplete_or_oversized_response_is_retained_without_retry(self):
        for response in (Response(200, {'a': 1}, truncate=True), Response(200, b'a', length=OP.JSON_LIMIT + 1)):
            with self.subTest(truncated=response.truncate):
                task = job()
                actor = prove(task)
                task.allowed.add(('viewer', '/owned', None))
                wire = Wire([response])
                with patch.object(OP.http.client, 'HTTPConnection', wire.connection), patch.object(OP.signal, 'setitimer', lambda *_: None):
                    with self.assertRaises(OP.SetupError):
                        task.request('owned-read', 'GET', '/owned', actor)
                    with self.assertRaises(OP.SetupError):
                        task.request('owned-read', 'GET', '/owned', actor)
                self.assertEqual(len(wire.sent), 1)
                self.assertFalse(task.records['owned-read']['response']['complete'])

    def test_11_cleanup_and_login_cannot_widen_scope(self):
        task = job()
        actor = prove(task)
        invalid = (('viewer-logout', 'POST', '/emby/Sessions/Logout', {}, False, True, None),
                   ('viewer-logout', 'POST', '/emby/Sessions/Logout', None, False, True, 'full'),
                   ('viewer-logout', 'POST', '/emby/Sessions/foreign/Logout', None, False, True, None),
                   ('viewer-exact-token', 'GET', '/emby/Users/foreign', None, False, True, None))
        for values in invalid:
            label, method, path, body, login, cleanup, mode = values
            with self.subTest(path=path, mode=mode), self.assertRaises(OP.SetupError):
                task.approved(label, method, path, actor, body, login, cleanup, mode)
        fresh = job()
        with self.assertRaises(OP.SetupError):
            fresh.approved('viewer-login', 'POST', '/emby/Users/AuthenticateByName', fresh.actors['viewer'],
                           {'Username': 'foreign', 'Pw': 'synthetic'}, True, False, None)

    def test_12_scoped_ledger_does_not_count_admin_or_initialize_userdata(self):
        task = job()
        ids = [task.state['viewer_id'], task.state['added_viewer']['user_id'], task.state['admin_id']]
        tables = {name: [{'user_id': item} for item in ids] for name in ('sessions', 'play_sessions', 'client_playback_references')}
        tables.update(user_item_data=[], encoding_jobs=[])
        before = copy.deepcopy(tables)
        result = task.ledger({'database': {'tables': tables}}, Path('/owned/after-full.json'), 'f' * 64)
        self.assertEqual(result['counts']['sessions'], 2)
        self.assertEqual(result['global_counts']['sessions'], 3)
        self.assertEqual(result['counts']['user_item_data'], 0)
        self.assertEqual(result['scope_user_ids'], sorted(ids[:2]))
        self.assertEqual(tables, before)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--operator', required=True, type=Path)
    parser.add_argument('--operator-sha256', required=True)
    args = parser.parse_args()
    raw = args.operator.read_bytes()
    if hashlib.sha256(raw).hexdigest() != args.operator_sha256:
        raise RuntimeError('The selected operator source digest changed.')
    global OP
    OP = types.ModuleType('special_features_fixture_guard_target')
    OP.__file__ = str(args.operator)
    exec(compile(raw, str(args.operator), 'exec'), OP.__dict__)
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(Gates))
    print(json.dumps({'result': 'passed' if result.wasSuccessful() else 'failed', 'tests': result.testsRun,
        'failures': len(result.failures), 'errors': len(result.errors), 'skipped': len(result.skipped),
        'operator_sha256': args.operator_sha256, 'guards_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        'network_requests': 0, 'database_connections': 0, 'service_actions': 0,
        'boundary': 'Pure synthetic operator guards; no actual candidate setup or protocol result.'}), flush=True)
    return 0 if result.wasSuccessful() else 1


if __name__ == '__main__':
    raise SystemExit(main())
