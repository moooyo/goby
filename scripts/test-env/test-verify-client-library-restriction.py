#!/usr/bin/env python3
"""Pure restriction, recovery, transport and complete-delta guards.

The optional remote baseline is read as immutable test data. All HTTP, service
and database behavior is mocked; no business request or mutation is performed.
"""

import argparse
import copy
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import sys
import time
import types
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
TARGET = SNAPSHOT = None


def encoded(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':')).encode()


def body(number='1'):
    return {'Revision': number, 'Name': 'm3e-client-viewer', 'IsAdministrator': False, 'IsDisabled': False,
            'Policy': {key: [] if key == 'EnabledFolders' else True for key in TARGET.POLICY_KEYS}}


class Response:
    def __init__(self, status, value=None, mime='application/json'):
        self.status = status
        self.raw = b'' if value is None else encoded(value)
        self.headers = {'Content-Type': mime, 'Content-Length': str(len(self.raw))}

    def getheader(self, name):
        return self.headers.get(name)

    def read(self, maximum):
        return self.raw[:maximum]


class Wire:
    def __init__(self, replies):
        self.replies, self.sent = list(replies), []

    def connection(self, host, port, timeout):
        if (host, port, timeout) != ('127.0.0.1', 18198, 10):
            raise AssertionError('The transport left the fixed candidate.')
        owner = self

        class Connection:
            def request(self, method, path, payload, headers):
                owner.sent.append((method, path, payload, headers))

            def getresponse(self):
                if not owner.replies:
                    raise AssertionError('An unplanned request was sent.')
                return owner.replies.pop(0)

            def close(self):
                pass

        return Connection()


def run():
    value = TARGET.Run(types.SimpleNamespace())
    value.state_bytes, value.sources = b'state', []
    value.state = {'server_id': 'd' * 32, 'admin_id': 'c' * 32, 'process': {'pid': 123}}
    value.saved = {}

    def create(path, payload):
        if str(path) in value.saved:
            raise AssertionError('An exclusive journal path was reused.')
        value.saved[str(path)] = copy.deepcopy(payload)

    value.op = types.SimpleNamespace(PORT=18198, PUBLIC='http://127.0.0.1:18196', STATE_FILE=Path('/private/state.json'),
        canonical_json=encoded, precise_json=json.loads, create=create, load=lambda _: copy.deepcopy(value.restore_reservation),
        read=lambda _: value.state_bytes, verify_fixture_directories=lambda _: None, verify_database=lambda _: None,
        verify_service=lambda _: value.state['process'])
    value.actors = {slot: TARGET.Actor(value, slot, {'username': 'owned-' + slot, 'password': 'synthetic-password-' + slot},
                    value.state['admin_id'] if slot == 'admin' else TARGET.ACTORS[slot]) for slot in ('admin', 'A', 'B')}
    value.item_ids = {'alpha': '1' * 32, 'deleted': '2' * 32, 'zeta': '3' * 32, 'trailer': '4' * 32}
    return value


def prove(value, slot):
    actor = value.actors[slot]
    token = {'admin': 'a', 'A': 'b', 'B': 'c'}[slot] * 43
    if slot == 'admin':
        response = {'status': 200, 'body': {'User': {'Id': actor.user_id, 'Name': actor.account['username'], 'IsAdministrator': True, 'IsDisabled': False},
                                          'CSRFToken': 'synthetic-csrf'}}
        cookie = 'goby_session=' + token + '; Path=/admin'
    else:
        response = {'status': 200, 'body': {'AccessToken': token, 'ServerId': value.state['server_id'],
            'User': {'Id': actor.user_id, 'Name': actor.account['username'], 'Policy': {'IsAdministrator': False}},
            'SessionInfo': {'Id': ('a' if slot == 'A' else 'b') * 32, 'UserId': actor.user_id, 'DeviceId': actor.device}}}
        cookie = None
    actor.acknowledge(response, cookie)
    actor.session_id = actor.session_id or 'c' * 32
    actor.status['login_status'] = 200
    actor.status['session_id'] = actor.session_id
    return actor


def recovery_run():
    value = run()
    for slot in value.actors:
        prove(value, slot)
    value.original, value.original_a = body(), dict(body(), Name='owned-A')
    value.restricted = TARGET.restricted_body(value.original)
    value.restrict_sent, value.restrict_status = True, 200
    value.recovery_deadline = time.monotonic() + 30
    value.restore_reservation = {'original': value.original, 'restricted': value.restricted}
    value.audit_owned = lambda _: True
    value.matrix = lambda phase, recovery=False: value.matrix_results.update({phase: True})
    return value


def final_sample():
    before = copy.deepcopy(SNAPSHOT)
    authenticated = copy.deepcopy(before)
    tables = authenticated['database']['tables']
    users = {row['id']: row for row in tables['users']}
    admin = next(row for row in users.values() if row['is_administrator'])
    start = dt.datetime.fromisoformat(before['database']['metadata']['captured_at']) + dt.timedelta(seconds=1)
    at = lambda offset: (start + dt.timedelta(seconds=offset)).isoformat()
    device_start = before['database']['sequences']['devices_id_seq']['last_value'] + int(before['database']['sequences']['devices_id_seq']['is_called'])
    audit_start = before['database']['sequences']['activity_entries_id_seq']['last_value'] + int(before['database']['sequences']['activity_entries_id_seq']['is_called'])
    actors = {}
    event_template = next(row for row in tables['activity_entries'] if row['action'] == 'session.login')

    def event(actor, action, index, moment, number=0, fields=()):
        row = copy.deepcopy(event_template)
        row.update(id=audit_start + index, action=action, source='native' if actor.slot == 'admin' else 'emby',
            actor_kind='user', actor_id=actor.user_id, actor_credential_id=actor.session_id,
            resource_kind='user' if action == 'user.updated' else 'session', resource_id=TARGET.ACTORS['B'] if action == 'user.updated' else actor.session_id,
            severity='Info', revision=number, affected_count=1, state='', changed_fields=list(fields), created_at=at(moment))
        return row

    for index, slot in enumerate(('admin', 'A', 'B')):
        user_id = admin['id'] if slot == 'admin' else TARGET.ACTORS[slot]
        session = copy.deepcopy(next(row for row in tables['sessions'] if row['user_id'] == user_id and row['kind'] == ('admin' if slot == 'admin' else 'emby')))
        session_id, token_sha = f'{9000 + index:032x}', str(index + 1) * 64
        reported = 'goby-dashboard' if slot == 'admin' else 'restriction-guard-' + slot
        session.update(id=session_id, token_hash='\\x' + token_sha, client_name='Goby Dashboard' if slot == 'admin' else TARGET.CLIENT,
            device_id=reported, device_name='Web browser' if slot == 'admin' else 'Linux Recorder', client_version='1.0',
            device_registry_id=None if slot == 'admin' else device_start + index - 1, created_at=at(index + 1), last_seen_at=at(index + 1),
            expires_at=at(3600), revoked_at=None, client_capabilities={})
        tables['sessions'].append(session)
        actor = types.SimpleNamespace(slot=slot, user_id=user_id, session_id=session_id, device=reported, proven=True, closed=True,
            status={'login_status': 200, 'logout_status': 204, 'exact_status': 401, 'token_sha256': token_sha, 'session_id': session_id})
        actors[slot] = actor
        if slot != 'admin':
            device = copy.deepcopy(tables['devices'][0])
            device.update(id=session['device_registry_id'], reported_device_id=reported, reported_name='Linux Recorder',
                app_name=TARGET.CLIENT, app_version='1.0', last_user_id=user_id, custom_name=None, deleted_at=None, revision=1,
                ip_address='127.0.0.1', created_at=at(index + 1), last_seen_at=at(index + 1))
            tables['devices'].append(device)
        tables['activity_entries'].append(event(actor, 'session.login', index, index + 1))
    authenticated['database']['metadata']['captured_at'] = at(4)
    after = copy.deepcopy(authenticated)
    final = after['database']['tables']
    original = body(str(users[TARGET.ACTORS['B']]['management_revision']))
    original['Name'] = users[TARGET.ACTORS['B']]['name']
    original['Policy'] = TARGET.projected_policy(users[TARGET.ACTORS['B']]['policy'])
    restricted = TARGET.restricted_body(original)
    number = int(original['Revision'])
    for offset, moment in ((1, 5), (2, 8)):
        final['activity_entries'].append(event(actors['admin'], 'user.updated', 2 + offset, moment, number + offset, TARGET.changed_fields(original, restricted)))
    for index, slot in enumerate(('B', 'A', 'admin')):
        actor = actors[slot]
        row = next(row for row in final['sessions'] if row['id'] == actor.session_id)
        row['revoked_at'] = at(11 + index)
        if slot != 'admin':
            row['last_seen_at'] = at(9)
            next(device for device in final['devices'] if device['id'] == row['device_registry_id'])['last_seen_at'] = at(9)
        final['activity_entries'].append(event(actor, 'session.revoked', 5 + index, 11 + index))
    b = next(row for row in final['users'] if row['id'] == TARGET.ACTORS['B'])
    b.update(policy=TARGET.raw_restored_policy(users[TARGET.ACTORS['B']], original), management_revision=number + 2, updated_at=at(8))
    after['database']['metadata']['captured_at'] = at(30)
    after['database']['sequences'].update(devices_id_seq={'last_value': device_start + 1, 'is_called': True},
                                         activity_entries_id_seq={'last_value': audit_start + 7, 'is_called': True})
    op = types.SimpleNamespace(equal_json=lambda a, b: a == b, canonical_json=encoded)
    schema = types.SimpleNamespace(validate_structure=lambda _op, snapshot, _state: TARGET.quiescent(snapshot))
    return op, schema, before, authenticated, after, {}, original, restricted, actors, {'before': at(0), 'after': at(30)}


class Guards(unittest.TestCase):
    def wire(self, wire):
        self.enterContext(patch.object(TARGET.http.client, 'HTTPConnection', wire.connection))
        self.enterContext(patch.object(TARGET.signal, 'setitimer', lambda *_: None))

    def test_01_exact_four_libraries_and_extras_preserved(self):
        rows = [{'id': value} for value in TARGET.LIBRARIES.values()]
        TARGET.validate_libraries(rows)
        rows[-1]['id'] = 'f' * 32
        with self.assertRaises(TARGET.RestrictionError):
            TARGET.validate_libraries(rows)
        restricted = TARGET.restricted_body(body())
        self.assertEqual(restricted['Policy']['EnabledFolders'], sorted(TARGET.LIBRARIES[key] for key in ('extras', 'music', 'tv')))
        limited = body()
        limited['Policy'].update(EnableAllFolders=False, EnabledFolders=[TARGET.LIBRARIES['movies']])
        with self.assertRaises(TARGET.RestrictionError):
            TARGET.restricted_body(limited)

    def test_02_native_complete_fields_and_canonical_revision(self):
        original = body()
        parsed = TARGET.managed({'User': dict(original, Id=TARGET.ACTORS['B'])}, TARGET.ACTORS['B'])
        self.assertEqual(parsed, original)
        for value in ('01', '0', '-1', 1, str(1 << 63)):
            with self.subTest(revision=value), self.assertRaises(TARGET.RestrictionError):
                TARGET.revision(value)
        del original['Policy']['EnableMediaPlayback']
        with self.assertRaises(TARGET.RestrictionError):
            TARGET.managed({'User': dict(original, Id=TARGET.ACTORS['B'])}, TARGET.ACTORS['B'])

    def test_03_literal_native_and_emby_error_contracts(self):
        self.assertEqual(TARGET.error_code({'ResponseStatus': {'ErrorCode': 'access_denied', 'Message': 'Denied.'}}, '/emby/Users/id/Items/id'), 'access_denied')
        self.assertEqual(TARGET.error_code({'ResponseStatus': {'ErrorCode': 'not_found'}}, '/emby/Items/id/Similar'), 'not_found')
        self.assertEqual(TARGET.error_code({'Error': {'Code': 'revision_conflict'}, 'RequestId': 'owned'}, '/admin/v1/users/id'), 'revision_conflict')
        self.assertIsNone(TARGET.error_code({'Error': {'Code': 'not_found'}}, '/emby/Items/id'))

    def test_04_revision_classification_and_foreign_drift(self):
        original = body()
        restricted = TARGET.restricted_body(original)
        self.assertEqual(TARGET.classify(original, original, restricted), 'original')
        self.assertEqual(TARGET.classify(dict(restricted, Revision='2'), original, restricted), 'restricted')
        self.assertEqual(TARGET.classify(dict(original, Revision='3'), original, restricted), 'restored')
        for current in (dict(restricted, Revision='3'), dict(restricted, Revision='2', Name='foreign')):
            self.assertEqual(TARGET.classify(current, original, restricted), 'foreign')

    def test_05_conflict_never_authorizes_restore(self):
        for owned in (True, False):
            self.assertEqual(TARGET.restore_decision(True, 409, False, 'restricted', owned), 'not_committed')
        self.assertEqual(TARGET.restore_decision(True, None, False, 'restricted', False), 'recovery_required')
        self.assertEqual(TARGET.restore_decision(True, None, False, 'restricted', True), 'restore_once')
        self.assertEqual(TARGET.restore_decision(True, 200, True, 'restricted', True), 'recovery_required')

    def test_06_exact_raw_policy_merge_keeps_unknown_keys(self):
        old = {'policy': {'Unknown': {'value': ['retained']}, 'EnabledFolders': ['old'], 'IsAdministrator': True}}
        expected = copy.deepcopy(old['policy'])
        expected.update(body()['Policy'], IsAdministrator=False, IsDisabled=False)
        self.assertEqual(TARGET.raw_restored_policy(old, body()), expected)
        self.assertEqual(TARGET.raw_restored_policy({'policy': {}}, body()), dict(body()['Policy'], IsAdministrator=False, IsDisabled=False))

    def test_07_lost_restriction_ack_restores_once_from_owned_state(self):
        value = recovery_run()
        value.restrict_status = None
        states = [dict(value.restricted, Revision='2'), dict(value.original, Revision='3'), value.original_a]
        value.native = lambda *args, **kwargs: states.pop(0)
        calls = []

        def request(label, method, path, actor, payload, **kwargs):
            calls.append((label, payload['Revision']))
            value.restore_sent = True
            return {'status': 200, 'complete': True, 'failure_type': None,
                    'body': {'CurrentSessionRevoked': False, 'User': dict(value.original, Revision='3', Id=TARGET.ACTORS['B'])}}

        value.request = request
        value.restore()
        self.assertEqual(value.restore_result, 'confirmed')
        self.assertEqual(calls, [('restore-user', '2')])

    def test_08_foreign_restore_state_sends_no_put(self):
        value = recovery_run()
        value.native = lambda *args, **kwargs: dict(value.restricted, Revision='7')
        value.request = lambda *_args, **_kwargs: self.fail('A foreign state was overwritten.')
        with self.assertRaises(TARGET.RestrictionError):
            value.restore()

    def test_09_restrict_response_journal_failure_keeps_status_for_recovery(self):
        value = recovery_run()
        value.restrict_status = None
        value.records['restrict-user'] = {'response': {'status': 200}}
        value.journal_failures.append({'name': 'restricted-response.json', 'failure_type': 'OSError'})
        states = [dict(value.restricted, Revision='2'), dict(value.original, Revision='3'), value.original_a]
        value.native = lambda *args, **kwargs: states.pop(0)

        def request(*args, **kwargs):
            value.restore_sent = True
            return {'status': 200, 'complete': True, 'failure_type': None,
                    'body': {'CurrentSessionRevoked': False, 'User': dict(value.original, Revision='3', Id=TARGET.ACTORS['B'])}}

        value.request = request
        value.restore()
        self.assertEqual(value.restrict_status, 200)
        self.assertEqual(value.restore_result, 'confirmed')
        self.assertTrue(value.journal_failures)

    def test_10_same_b_token_in_all_matrix_phases(self):
        value = run()
        actor = prove(value, 'B')
        responses = [Response(200, {'Id': TARGET.POSITIVE}) for _ in range(3)]
        wire = Wire(responses)
        self.wire(wire)
        path = '/emby/Users/' + TARGET.ACTORS['B'] + '/Items/' + TARGET.POSITIVE
        value.allowed_reads.add(('B', path))
        for phase in ('baseline', 'restricted', 'restored'):
            value.request(phase + '-positive', 'GET', path, actor, purpose='matrix', recovery=phase == 'restored')
        self.assertEqual({entry[3]['X-Emby-Token'] for entry in wire.sent}, {actor.token})
        self.assertEqual({record['token_fingerprint'] for record in value.records.values()}, {actor.status['token_sha256']})

    def test_11_exhausted_business_and_recovery_budgets_do_not_block_six_cleanup_calls(self):
        value = run()
        for slot in value.actors:
            prove(value, slot)
        value.request_counts.update(business=44, recovery=30)
        value.sequence, value.response_bytes, value.recovery_bytes = 74, 17 << 20, 9 << 20
        value.deadline = value.recovery_deadline = time.monotonic() - 1
        wire = Wire([Response(code) for code in (204, 401, 204, 401, 204, 401)])
        self.wire(wire)
        for slot in ('B', 'A', 'admin'):
            value.actors[slot].logout()
        self.assertEqual(len(wire.sent), 6)
        self.assertEqual(value.sequence, 80)
        self.assertTrue(all(actor.closed for actor in value.actors.values()))

    def test_12_cleanup_journal_failure_still_closes_owned_token(self):
        value = run()
        actor = prove(value, 'B')
        value.private = lambda *_args: (_ for _ in ()).throw(OSError('Synthetic journal failure.'))
        wire = Wire([Response(204), Response(401)])
        self.wire(wire)
        actor.logout()
        self.assertTrue(actor.closed)
        self.assertEqual(len(value.journal_failures), 4)

    def test_13_logout_204_is_not_posted_again(self):
        value = run()
        actor = prove(value, 'B')
        actor.status['logout_status'] = 204
        wire = Wire([Response(401)])
        self.wire(wire)
        actor.logout()
        self.assertEqual([entry[0:2] for entry in wire.sent], [('GET', '/emby/System/Info')])

    def test_14_unproven_acknowledgement_never_logs_out(self):
        value = run()
        actor = value.actors['B']
        actor.acknowledge({'status': 200, 'body': {'AccessToken': 'a' * 43, 'ServerId': 'foreign'}}, None)
        wire = Wire([])
        self.wire(wire)
        with self.assertRaises(TARGET.RestrictionError):
            actor.logout()
        self.assertEqual(wire.sent, [])

    def test_15_complete_snapshot_ignores_only_typed_capture_clock(self):
        before, after = copy.deepcopy(SNAPSHOT), copy.deepcopy(SNAPSHOT)
        when = dt.datetime.fromisoformat(before['database']['metadata']['captured_at'])
        after['database']['metadata']['captured_at'] = (when + dt.timedelta(seconds=1)).isoformat()
        op = types.SimpleNamespace(equal_json=lambda a, b: a == b)
        TARGET.compare_fixed_snapshot(op, before, after)
        after['database']['metadata']['public_schema'] = {'foreign': True}
        with self.assertRaises(TARGET.RestrictionError):
            TARGET.compare_fixed_snapshot(op, before, after)

    def test_16_actual_baseline_quiescence_and_preserved_play_rows(self):
        TARGET.quiescent(SNAPSHOT)
        before, after = copy.deepcopy(SNAPSHOT), copy.deepcopy(SNAPSHOT)
        after['database']['tables']['play_sessions'][0]['state'] = 'unapproved'
        op = types.SimpleNamespace(equal_json=lambda a, b: a == b, canonical_json=encoded)
        with self.assertRaises(TARGET.RestrictionError):
            TARGET.unchanged_rows(op, before, after)

    def test_17_complete_delta_has_three_sessions_two_devices_eight_audits(self):
        result = TARGET.validate_final(*final_sample())
        self.assertEqual((result['new_sessions'], result['new_devices'], result['new_session_audits'], result['new_user_update_audits']), (3, 2, 6, 2))
        self.assertTrue(result['raw_policy_matches_exact_merge'])

    def test_18_final_delta_does_not_ignore_raw_policy_or_old_rows(self):
        for table, field, value in (('users', 'policy', {}), ('user_item_data', 'play_count', 999), ('play_sessions', 'state', 'changed'),
                                    ('devices', 'custom_name', 'unowned-change')):
            values = final_sample()
            rows = values[4]['database']['tables'][table]
            row = next(row for row in rows if row['id'] == TARGET.ACTORS['B']) if table == 'users' else rows[0]
            row[field] = value
            with self.subTest(table=table), self.assertRaises(TARGET.RestrictionError):
                TARGET.validate_final(*values)

    def test_19_final_delta_rejects_extra_admin_touch_audit_and_sequence(self):
        for case in ('admin_touch', 'audit_count', 'audit_actor', 'sequence'):
            values = final_sample()
            after, actors = values[4], values[8]
            if case == 'admin_touch':
                next(row for row in after['database']['tables']['sessions'] if row['id'] == actors['admin'].session_id)['last_seen_at'] = values[9]['after']
            elif case == 'audit_count':
                after['database']['tables']['activity_entries'][-1]['affected_count'] = 0
            elif case == 'audit_actor':
                next(row for row in after['database']['tables']['activity_entries'] if row['action'] == 'user.updated' and
                     row['revision'] == int(values[6]['Revision']) + 1)['actor_credential_id'] = 'f' * 32
            else:
                after['database']['sequences']['devices_id_seq']['last_value'] += 1
            with self.subTest(case=case), self.assertRaises(TARGET.RestrictionError):
                TARGET.validate_final(*values)

    def test_20_final_delta_preserves_unknown_policy_keys_exactly(self):
        values = final_sample()
        marker = {'nested': ['unchanged', 3]}
        for index in (2, 3):
            next(row for row in values[index]['database']['tables']['users'] if row['id'] == TARGET.ACTORS['B'])['policy']['GuardUnknown'] = copy.deepcopy(marker)
        final_b = next(row for row in values[4]['database']['tables']['users'] if row['id'] == TARGET.ACTORS['B'])
        final_b['policy']['GuardUnknown'] = copy.deepcopy(marker)
        TARGET.validate_final(*values)
        del final_b['policy']['GuardUnknown']
        with self.assertRaises(TARGET.RestrictionError):
            TARGET.validate_final(*values)

    def test_21_lost_restore_ack_is_reconciled_without_another_put(self):
        value = recovery_run()
        states = [dict(value.restricted, Revision='2'), dict(value.original, Revision='3'), value.original_a]
        value.native = lambda *args, **kwargs: states.pop(0)
        sent = []

        def request(*args, **kwargs):
            sent.append(args[0])
            value.restore_sent = True
            return {'status': None, 'complete': False, 'failure_type': 'TimeoutError', 'body': None}

        value.request = request
        value.restore()
        self.assertEqual(sent, ['restore-user'])
        self.assertEqual(value.restore_result, 'confirmed')
        self.assertIn('restore_response_unconfirmed', value.errors)

    def test_22_conditional_reserved_restore_survives_new_journal_failure(self):
        value = recovery_run()
        states = [dict(value.restricted, Revision='2'), dict(value.original, Revision='3'), value.original_a]
        value.native = lambda *args, **kwargs: states.pop(0)
        value.private = lambda *_args: (_ for _ in ()).throw(OSError('Synthetic new journal failure.'))
        wire = Wire([Response(200, {'CurrentSessionRevoked': False, 'User': dict(value.original, Revision='3', Id=TARGET.ACTORS['B'])})])
        self.wire(wire)
        value.restore()
        self.assertEqual([entry[0] for entry in wire.sent], ['PUT'])
        self.assertEqual(value.restore_result, 'confirmed')
        self.assertEqual(len(value.journal_failures), 2)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--operator', type=Path, required=True)
    parser.add_argument('--operator-sha256', required=True)
    parser.add_argument('--baseline-snapshot', type=Path, required=True)
    parser.add_argument('--baseline-snapshot-sha256', required=True)
    args = parser.parse_args()
    if sys.platform != 'linux' or os.geteuid() != 0:
        raise RuntimeError('Run guards only on the authorized remote Linux host.')
    global TARGET, SNAPSHOT
    raw = args.operator.read_bytes()
    if hashlib.sha256(raw).hexdigest() != args.operator_sha256:
        raise RuntimeError('The frozen operator source changed.')
    TARGET = types.ModuleType('restriction_under_guard')
    TARGET.__file__ = str(args.operator)
    exec(compile(raw, str(args.operator), 'exec'), TARGET.__dict__)
    raw = args.baseline_snapshot.read_bytes()
    if args.baseline_snapshot_sha256 != TARGET.BASELINE_SHA or hashlib.sha256(raw).hexdigest() != TARGET.BASELINE_SHA:
        raise RuntimeError('The actual complete baseline guard input changed.')
    SNAPSHOT = json.loads(raw)
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(Guards))
    print(json.dumps({'result': 'passed' if result.wasSuccessful() else 'failed', 'tests': result.testsRun,
        'failures': len(result.failures), 'errors': len(result.errors), 'operator_sha256': args.operator_sha256,
        'http_requests': 0, 'database_writes': 0, 'service_actions': 0}), flush=True)
    return 0 if result.wasSuccessful() else 1


if __name__ == '__main__':
    raise SystemExit(main())
