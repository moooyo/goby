#!/usr/bin/env python3
"""Pure guards for one precisely owned failed-Home session recovery.

The completed failed observation is immutable test data. All database, HTTP,
process and filesystem effects are fenced while the guard cases execute.
"""

import argparse
import copy
import datetime as dt
import hashlib
import os
from pathlib import Path
import socket
import subprocess
import sys
import types
import unittest
from unittest import mock

sys.dont_write_bytecode = True
TARGET = HOME = RESTRICTION = OP = BEFORE = FAILED = BROWSER = None
ADMIN_TOKEN = 'a' * 43
ADMIN_SHA = hashlib.sha256(ADMIN_TOKEN.encode()).hexdigest()
ADMIN_SESSION = 'c2' * 16


def denied(*_, **__):
    raise AssertionError('A pure recovery guard attempted actual I/O.')


def sample():
    before = copy.deepcopy(FAILED)
    authenticated, after = copy.deepcopy(before), copy.deepcopy(before)
    target = next(row for row in before['database']['tables']['sessions'] if row['token_hash'] == '\\x' + TARGET.TARGET_SHA)
    admin_user = next(row for row in before['database']['tables']['users'] if row['is_administrator'])
    client = next(row for row in before['database']['tables']['sessions'] if row['kind'] == 'admin')
    start = HOME.instant(before['database']['metadata']['captured_at']) + dt.timedelta(seconds=1)
    at = lambda number: (start + dt.timedelta(seconds=number)).isoformat()
    admin = copy.deepcopy(client)
    admin.update(id=ADMIN_SESSION, user_id=admin_user['id'], token_hash='\\x' + ADMIN_SHA, kind='admin', device_registry_id=None,
        created_at=at(1), last_seen_at=at(1), expires_at=at(1 + 86400), revoked_at=None, client_capabilities={})
    audit_id = before['database']['sequences']['activity_entries_id_seq']['last_value'] + int(before['database']['sequences']['activity_entries_id_seq']['is_called'])
    template = next(row for row in before['database']['tables']['activity_entries'] if row['action'] == 'session.login')
    events = []
    for index, (action, resource) in enumerate((('session.login', ADMIN_SESSION), ('session.revoked', target['id']), ('session.revoked', ADMIN_SESSION))):
        event = copy.deepcopy(template)
        event.update(id=audit_id + index, action=action, source='native', actor_kind='user', actor_id=admin_user['id'],
            actor_credential_id=ADMIN_SESSION, resource_kind='session', resource_id=resource, revision=0, changed_fields=[],
            severity='Info', affected_count=1, state='', created_at=at(index + 1))
        events.append(event)
    authenticated['database']['tables']['sessions'].append(copy.deepcopy(admin))
    authenticated['database']['tables']['activity_entries'].append(copy.deepcopy(events[0]))
    authenticated['database']['sequences']['activity_entries_id_seq'] = {'last_value': audit_id, 'is_called': True}
    authenticated['database']['metadata']['captured_at'] = at(1.5)
    after['database']['tables']['sessions'].append({**admin, 'last_seen_at': at(2), 'revoked_at': at(3)})
    next(row for row in after['database']['tables']['sessions'] if row['id'] == target['id'])['revoked_at'] = at(2)
    after['database']['tables']['activity_entries'].extend(events)
    after['database']['sequences']['activity_entries_id_seq'] = {'last_value': audit_id + 2, 'is_called': True}
    after['database']['metadata']['captured_at'] = at(4)
    response = {'SessionId': target['id'], 'UserId': HOME.B, 'Kind': 'emby', 'RevokedAt': at(2), 'CurrentSessionRevoked': False}
    return before, authenticated, after, target, admin, response, client


def recovery_run():
    before, _, _, target, admin, _, _ = sample()
    run = TARGET.Run(types.SimpleNamespace())
    run.before, run.target, run.admin = before, target, admin
    run.state = {'admin_id': admin['user_id']}
    username = next(row['name'] for row in before['database']['tables']['users'] if row['id'] == admin['user_id'])
    run.account = {'username': username, 'password': 'synthetic-only-password'}
    run.check = lambda: None
    run.reserved_cleanup = lambda: None
    run.saved = {}
    run.save = lambda name, value: run.saved.update({name: copy.deepcopy(value)})
    return run


class Wire:
    def __init__(self, run, statuses):
        self.run, self.statuses, self.sent = run, list(statuses), []

    def factory(self, host, port, timeout):
        if (host, port, timeout) != ('127.0.0.1', 18198, 8): raise AssertionError('The native route left the exact candidate.')
        owner = self
        class Connection:
            def request(self, method, path, payload, headers): owner.sent.append((method, path, payload, headers))
            def getresponse(self):
                status = owner.statuses.pop(0)
                body = {'User': {'Id': owner.run.state['admin_id'], 'Name': owner.run.account['username'],
                    'IsAdministrator': True, 'IsDisabled': False}, 'CSRFToken': 'c' * 64} if status == 200 else None
                raw = b'' if body is None else HOME.canonical(body)
                return types.SimpleNamespace(status=status, read=lambda maximum: raw[:maximum],
                    getheader=lambda name: ('goby_session=' + ADMIN_TOKEN + '; Path=/admin') if name == 'Set-Cookie' and status == 200 else
                        str(len(raw)) if name == 'Content-Length' else None)
            def close(self): pass
        return Connection()


class RecoveryGuards(unittest.TestCase):
    def setUp(self):
        patches = [mock.patch.object(subprocess, 'run', side_effect=denied), mock.patch.object(subprocess, 'Popen', side_effect=denied),
            mock.patch.object(socket, 'socket', side_effect=denied), mock.patch.object(socket, 'create_connection', side_effect=denied),
            mock.patch.object(TARGET.http.client, 'HTTPConnection', side_effect=denied), mock.patch.object(os, 'open', side_effect=denied),
            mock.patch.object(os, 'mkdir', side_effect=denied), mock.patch.object(Path, 'read_bytes', side_effect=denied),
            mock.patch.object(Path, 'read_text', side_effect=denied), mock.patch('builtins.open', side_effect=denied)]
        for patch in patches: patch.start()
        self.addCleanup(lambda: [patch.stop() for patch in reversed(patches)])

    def test_actual_failed_snapshot_is_exactly_one_new_unrevoked_b_login(self):
        target = TARGET.validate_failed_session(OP, RESTRICTION, BEFORE, FAILED, BROWSER)
        self.assertEqual(target['token_hash'], '\\x9f71d2771f1a0d177041d6815f46d39ecea9a54bf92099a13b89dbef172afb81')
        self.assertIsNone(target['revoked_at'])

    def test_preexisting_target_other_user_or_duplicate_fingerprint_is_rejected(self):
        for variant in ('old', 'foreign', 'duplicate', 'revoked'):
            before, failed = copy.deepcopy(BEFORE), copy.deepcopy(FAILED)
            target = next(row for row in failed['database']['tables']['sessions'] if row['token_hash'] == '\\x' + TARGET.TARGET_SHA)
            if variant == 'old': before['database']['tables']['sessions'].append(copy.deepcopy(target))
            if variant == 'foreign': target['user_id'] = HOME.A
            if variant == 'duplicate': failed['database']['tables']['sessions'].append(copy.deepcopy(target))
            if variant == 'revoked': target['revoked_at'] = target['created_at']
            with self.subTest(variant=variant), self.assertRaises(Exception): TARGET.validate_failed_session(OP, RESTRICTION, before, failed, BROWSER)

    def test_failure_with_play_or_old_policy_change_is_rejected(self):
        for table, key, value in (('users', 'policy', {}), ('play_sessions', 'state', 'Running')):
            failed = copy.deepcopy(FAILED); failed['database']['tables'][table][0][key] = {'not': 'allowed'}
            with self.subTest(table=table), self.assertRaises(Exception): TARGET.validate_failed_session(OP, RESTRICTION, BEFORE, failed, BROWSER)

    def test_complete_native_recovery_preserves_every_old_field_except_target_revocation(self):
        before, authenticated, after, target, admin, response, client = sample()
        actual = TARGET.validate_admin_login(OP, RESTRICTION, before, authenticated, admin['user_id'], ADMIN_SHA, client)
        proof = TARGET.validate_final(OP, RESTRICTION, before, authenticated, after, target, actual, response)
        self.assertEqual(proof['new_native_session_audits'], 3)
        self.assertTrue(proof['target_only_revoked_at_changed'])
        self.assertFalse(proof['lost_target_token_401_observed'])

    def test_target_other_fields_and_old_credentials_cannot_change(self):
        for variant in ('target_seen', 'target_caps', 'old_auth', 'old_device', 'policy', 'sequence'):
            before, authenticated, after, target, admin, response, _ = sample()
            if variant.startswith('target_'):
                row = next(row for row in after['database']['tables']['sessions'] if row['id'] == target['id'])
                row['last_seen_at' if variant == 'target_seen' else 'client_capabilities'] = 'unapproved'
            if variant == 'old_auth': after['database']['tables']['sessions'][0]['revoked_at'] = 'unapproved'
            if variant == 'old_device': after['database']['tables']['devices'][0]['reported_name'] = 'unapproved'
            if variant == 'policy': after['database']['tables']['users'][0]['policy'] = {'unapproved': True}
            if variant == 'sequence': after['database']['sequences']['devices_id_seq']['last_value'] += 1
            with self.subTest(variant=variant), self.assertRaises(Exception): TARGET.validate_final(OP, RESTRICTION, before, authenticated, after, target, admin, response)

    def test_revoke_response_requires_exact_target_timestamp_and_not_self(self):
        for key, value in (('SessionId', ADMIN_SESSION), ('UserId', HOME.A), ('Kind', 'admin'),
                ('CurrentSessionRevoked', True), ('RevokedAt', '2000-01-01T00:00:00Z')):
            before, authenticated, after, target, admin, response, _ = sample()
            response[key] = value
            with self.subTest(key=key), self.assertRaises(Exception): TARGET.validate_final(OP, RESTRICTION, before, authenticated, after, target, admin, response)

    def test_second_admin_login_new_device_or_wrong_audit_actor_is_rejected(self):
        for variant in ('session', 'device', 'audit'):
            before, authenticated, after, target, admin, response, _ = sample()
            if variant == 'session': after['database']['tables']['sessions'].append(copy.deepcopy(admin))
            if variant == 'device': after['database']['tables']['devices'].append({'id': 900000})
            if variant == 'audit': after['database']['tables']['activity_entries'][-2]['actor_credential_id'] = target['id']
            with self.subTest(variant=variant), self.assertRaises(Exception): TARGET.validate_final(OP, RESTRICTION, before, authenticated, after, target, admin, response)

    def test_wrong_failure_scope_or_target_pin_cannot_be_selected(self):
        value = {'marker': TARGET.INPUT_MARKER, 'target_token_sha256': TARGET.TARGET_SHA,
            **{key: {'path': str(path), 'sha256': TARGET.PINS[key]} for key, path in TARGET.PATHS.items()}}
        TARGET.validate_inputs(value)
        for key in ('target_token_sha256', 'after'):
            changed = copy.deepcopy(value)
            changed[key] = 'b' * 64 if key == 'target_token_sha256' else {'path': str(TARGET.ROOT / 'invented.json'), 'sha256': 'a' * 64}
            with self.subTest(key=key), self.assertRaises(Exception): TARGET.validate_inputs(changed)

    def test_request_gate_rejects_bulk_policy_repeat_and_unproven_actor(self):
        run = TARGET.Run(types.SimpleNamespace()); run.target = {'id': 'a' * 32}
        run.check = run.save = denied
        for purpose in ('bulk', 'policy', 'revoke', 'logout', 'exact'):
            with self.subTest(purpose=purpose), self.assertRaises(TARGET.RecoveryError): run.request(purpose)
        run.sent.add('login')
        with self.assertRaises(TARGET.RecoveryError): run.request('login')

    def test_check_only_has_no_output_or_http(self):
        run = TARGET.Run(types.SimpleNamespace(check_only=True)); run.target = {'id': 'a' * 32}
        run.load = lambda: None
        run.prepare = run.request = run.save = denied
        self.assertEqual(run.execute()['http_requests'], 0)

    def test_login_ack_journal_failure_still_closes_only_new_admin(self):
        run = recovery_run()
        def save(name, value):
            if name == 'login-response-private.json': raise OSError('Synthetic journal failure')
            run.saved[name] = copy.deepcopy(value)
        run.save = save
        wire = Wire(run, [200, 204, 401])
        with mock.patch.object(TARGET.http.client, 'HTTPConnection', side_effect=wire.factory):
            with self.assertRaises(OSError): run.login()
            self.assertTrue(run.proven)
            self.assertEqual(run.token_sha, ADMIN_SHA)
            run.close()
        self.assertTrue(run.closed)
        self.assertEqual([(method, path) for method, path, _, _ in wire.sent],
            [('POST', '/admin/v1/session'), ('DELETE', '/admin/v1/session'), ('GET', '/admin/v1/session')])

    def test_cleanup_journal_failure_uses_reservation_but_target_intent_stays_mandatory(self):
        run = recovery_run(); run.proven = True; run.token = ADMIN_TOKEN; run.csrf = 'c' * 64; run.sent = {'login'}
        run.save = mock.Mock(side_effect=OSError('Synthetic journal failure'))
        wire = Wire(run, [204, 401])
        with mock.patch.object(TARGET.http.client, 'HTTPConnection', side_effect=wire.factory):
            with self.assertRaises(OSError): run.request('revoke')
            self.assertEqual(wire.sent, [])
            run.close()
        self.assertTrue(run.closed)
        self.assertEqual(len(run.errors), 4)
        self.assertEqual([(method, path) for method, path, _, _ in wire.sent],
            [('DELETE', '/admin/v1/session'), ('GET', '/admin/v1/session')])


def module(path, sha256, name):
    raw = path.read_bytes()
    if hashlib.sha256(raw).hexdigest() != sha256: raise RuntimeError('A frozen guard source changed.')
    value = types.ModuleType(name); value.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), value.__dict__)
    return value


def main():
    global TARGET, HOME, RESTRICTION, OP, BEFORE, FAILED, BROWSER
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--operator', type=Path, required=True)
    parser.add_argument('--operator-sha256', required=True)
    parser.add_argument('--failure-inputs', type=Path, required=True)
    parser.add_argument('--failure-inputs-sha256', required=True)
    parser.add_argument('--report', type=Path)
    args = parser.parse_args()
    TARGET = module(args.operator, args.operator_sha256, 'session_recovery_under_test')
    HOME = module(TARGET.HOME_PATH, TARGET.HOME_SHA, 'frozen_home_pure_helpers'); TARGET.HOME = HOME
    path, sha256 = HOME.HELPERS['restriction']
    RESTRICTION = module(path, sha256, 'frozen_restriction_pure_helpers')
    OP = types.SimpleNamespace(equal_json=lambda a, b: HOME.canonical(a) == HOME.canonical(b), canonical_json=HOME.canonical)
    inputs = HOME.decode(HOME.protected(args.failure_inputs, args.failure_inputs_sha256)); TARGET.validate_inputs(inputs)
    BEFORE, FAILED, BROWSER = (HOME.read_record(inputs[key]) for key in ('before', 'after', 'browser_report'))
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(RecoveryGuards))
    report = {'marker': 'goby-client-library-home-session-recovery-guards-v1', 'result': 'passed' if result.wasSuccessful() and not result.skipped else 'failed',
        'operator_sha256': args.operator_sha256, 'failure_inputs_sha256': args.failure_inputs_sha256,
        'tests_run': result.testsRun, 'failures': len(result.failures), 'errors': len(result.errors), 'skipped': len(result.skipped),
        'http_requests': 0, 'database_commands': 0, 'service_commands': 0, 'browser_started': False}
    if args.report:
        if not args.report.is_relative_to(TARGET.WORK) or '..' in args.report.parts: raise RuntimeError('The guard report is outside the private workspace.')
        fd = os.open(args.report, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'wb') as handle: handle.write(HOME.canonical(report) + b'\n'); handle.flush(); os.fsync(handle.fileno())
    print(HOME.exact(report))
    return 0 if report['result'] == 'passed' else 1


if __name__ == '__main__':
    sys.exit(main())
