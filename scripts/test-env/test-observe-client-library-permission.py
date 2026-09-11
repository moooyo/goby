#!/usr/bin/env python3
"""Pure guards for the one-shot B permission controller.

Only pinned sources and completed Home v3 evidence are read before the cases.
Network, process and filesystem effects are fenced throughout every test.
The optional exclusive private report is the only guard output file.
"""

import argparse
import copy
import datetime as dt
import hashlib
import os
from pathlib import Path
import socket
import stat
import subprocess
import sys
import types
import unittest
from unittest import mock

sys.dont_write_bytecode = True
TARGET = H = RESTRICTION = SNAPSHOT = HOME_REPORT = HOME_BROWSER = STATE = None
BASELINE_SHA = '8095db0dd2e96c7f3a8e194b3f66d71c7366e756b12f1d1f549a19c336acb29a'
TOKEN = 'Z' * 43
TOKEN_SHA = hashlib.sha256(TOKEN.encode()).hexdigest()
SESSION = 'd7' * 16
ADMIN_TOKEN = 'a' * 43
ADMIN_SHA = hashlib.sha256(ADMIN_TOKEN.encode()).hexdigest()
ADMIN_SESSION = 'c2' * 16
DEVICE = 'permission-guard-new-device'
INPUT_SHA = 'e3' * 32


def denied(*_, **__):
    raise AssertionError('A pure permission guard attempted real I/O or process execution.')


def codec():
    return types.SimpleNamespace(equal_json=lambda left, right: H.canonical(left) == H.canonical(right),
        canonical_json=H.canonical, ENV={'PATH': '/usr/bin:/bin'})


def sample():
    before = copy.deepcopy(SNAPSHOT)
    authenticated = copy.deepcopy(before)
    tables = authenticated['database']['tables']
    users = {row['id']: row for row in tables['users']}
    administrator = next(row for row in users.values() if row['is_administrator'])
    start = H.instant(before['database']['metadata']['captured_at']) + dt.timedelta(seconds=1)
    at = lambda offset: (start + dt.timedelta(seconds=offset)).isoformat()
    device_id = before['database']['sequences']['devices_id_seq']['last_value'] + int(before['database']['sequences']['devices_id_seq']['is_called'])
    audit_id = before['database']['sequences']['activity_entries_id_seq']['last_value'] + int(before['database']['sequences']['activity_entries_id_seq']['is_called'])
    proof = {'token_sha256': TOKEN_SHA, 'session_id': SESSION, 'user_id': H.B, 'device_id': DEVICE,
        'server_id': STATE['server_id'], 'client_name': 'Emby Web', 'device_name': 'Observed browser', 'client_version': '4.9.5.0',
        'created_at': at(1), 'created_at_source': 'SessionInfo.LastActivityDate', 'kind': 'emby', 'slot': 'B',
        'frame_login_finished': True, 'physical_login_completed': True, 'request_metadata_matches': True}
    ordinary = copy.deepcopy(next(row for row in tables['sessions'] if row['user_id'] == H.B and row['kind'] == 'emby'))
    ordinary.update(id=SESSION, token_hash='\\x' + TOKEN_SHA, device_id=DEVICE, device_registry_id=device_id,
        client_name=proof['client_name'], device_name=proof['device_name'], client_version=proof['client_version'],
        created_at=at(1), last_seen_at=at(1), expires_at=at(1 + 30 * 86400), revoked_at=None, client_capabilities={})
    admin = copy.deepcopy(next(row for row in tables['sessions'] if row['user_id'] == administrator['id'] and row['kind'] == 'admin'))
    admin.update(id=ADMIN_SESSION, token_hash='\\x' + ADMIN_SHA, device_registry_id=None, created_at=at(2),
        last_seen_at=at(2), expires_at=at(2 + 86400), revoked_at=None, client_capabilities={})
    tables['sessions'].extend((ordinary, admin))
    device = copy.deepcopy(tables['devices'][0])
    device.update(id=device_id, reported_device_id=DEVICE, reported_name=proof['device_name'], app_name=proof['client_name'],
        app_version=proof['client_version'], last_user_id=H.B, ip_address='127.0.0.1', custom_name=None,
        deleted_at=None, revision=1, created_at=at(0), last_seen_at=at(1))
    tables['devices'].append(device)
    original = {'Revision': str(users[H.B]['management_revision']), 'Name': users[H.B]['name'],
        'IsAdministrator': users[H.B]['is_administrator'], 'IsDisabled': users[H.B]['is_disabled'],
        'Policy': RESTRICTION.projected_policy(users[H.B]['policy'])}
    restricted = RESTRICTION.restricted_body(original)
    fields = RESTRICTION.changed_fields(original, restricted)
    event_template = next(row for row in tables['activity_entries'] if row['action'] == 'session.login')

    def event(actor, action, index, offset, revision=0):
        row = copy.deepcopy(event_template)
        row.update(id=audit_id + index, action=action, source='native' if actor['kind'] == 'admin' else 'emby',
            actor_kind='user', actor_id=actor['user_id'], actor_credential_id=actor['id'],
            resource_kind='user' if action == 'user.updated' else 'session',
            resource_id=H.B if action == 'user.updated' else actor['id'], revision=revision,
            changed_fields=fields if action == 'user.updated' else [], severity='Info', affected_count=1, state='', created_at=at(offset))
        return row

    tables['activity_entries'].extend((event(ordinary, 'session.login', 0, 1), event(admin, 'session.login', 1, 2)))
    authenticated['database']['sequences'].update(devices_id_seq={'last_value': device_id, 'is_called': True},
        activity_entries_id_seq={'last_value': audit_id + 1, 'is_called': True})
    authenticated['database']['metadata']['captured_at'] = at(3)
    after = copy.deepcopy(authenticated)
    final = after['database']['tables']
    final['activity_entries'].extend((event(admin, 'user.updated', 2, 5, 4), event(admin, 'user.updated', 3, 8, 5),
        event(ordinary, 'session.revoked', 4, 11), event(admin, 'session.revoked', 5, 12)))
    for identifier, seen, revoked in ((SESSION, 10, 11), (ADMIN_SESSION, 9, 12)):
        next(row for row in final['sessions'] if row['id'] == identifier).update(last_seen_at=at(seen), revoked_at=at(revoked))
    next(row for row in final['devices'] if row['id'] == device_id)['last_seen_at'] = at(10)
    next(row for row in final['users'] if row['id'] == H.B).update(management_revision=5, updated_at=at(8))
    after['database']['metadata']['captured_at'] = at(30)
    after['database']['sequences']['activity_entries_id_seq'] = {'last_value': audit_id + 5, 'is_called': True}
    return before, authenticated, after, copy.deepcopy(admin), proof, original, restricted


def bindings():
    controller = {'pid': 100, 'start_ticks': 123456, 'boot_id': H.PROCESS['boot_id'], 'unit': TARGET.UNIT}
    child = {'pid': 101, 'start_ticks': 123457, 'boot_id': H.PROCESS['boot_id'], 'uid': 0, 'gid': 0,
        'executable_path': '/usr/bin/node', 'executable_sha256': 'a' * 64, 'cgroup': TARGET.CGROUP}
    record = {'mode': TARGET.MODE, 'controller': controller, 'candidate': {'server_id': STATE['server_id']},
        'source_closure': {str(TARGET.HOME_SOURCE): TARGET.HOME_SHA, '/owned/permission.mjs': 'b' * 64},
        'expected_libraries': [{'id': row['id'], 'name': row['name']} for row in SNAPSHOT['database']['tables']['libraries']]}
    descriptor = {'path': str(TARGET.BROWSER_ROOT / 'session-private.json'), 'sha256': 'd' * 64}
    return record, child, descriptor


def views(libraries, stage_name='initial', invoked_at=0):
    frame = {'kind': 'views', 'finished': True, 'failed': False, 'from_service_worker': False, 'status': 200,
        'content_type': 'application/json', 'request_sha256': 'a' * 64, 'token_sha256': TOKEN_SHA}
    physical = {'kind': 'views', 'completed': True, 'terminal_status': 200, 'request_sha256': 'a' * 64,
        'token_sha256': TOKEN_SHA, 'projection': {'matches_expected': True, 'TotalRecordCount': len(libraries),
            'Items': [{'Id': row['id'], 'Name': row['name'], 'name_matches': True} for row in libraries]}}
    for event in (frame, physical):
        event.update(stage=stage_name, request_elapsed_ms=invoked_at + 1, response_elapsed_ms=invoked_at + 2, finished_elapsed_ms=invoked_at + 3)
    return {'result': 'passed', 'stage': stage_name, 'frame_count': 1, 'physical_count': 1,
        'pairs': [{'complete': True, 'unambiguous': True, 'frame': frame, 'physical': physical}]}


def dom(libraries, excluded=()):
    return {'passed': True, 'media_inactive': True, 'location': {'same_origin': True, 'supported_path': True, 'route': 'home'},
        'libraries': [{**row, 'passed': True, 'visible_card_count': 1, 'card_id_present': True,
            'card_id_matches': True, 'visible_title_count': 1} for row in libraries],
        'excluded_libraries': [{**row, 'absent': True, 'visible_card_count': 0, 'matching_id_card_count': 0} for row in excluded]}


def baseline_stage():
    record, child, descriptor = bindings()
    libraries = record['expected_libraries']
    stage = {'marker': 'goby-client-library-permission-stage-v1', 'version': 1, 'input_sha256': INPUT_SHA,
        'source_closure_sha256': TARGET.sha(H.canonical(record['source_closure'])), 'controller': record['controller'],
        'node_process': child, 'name': 'baseline', 'token_sha256': TOKEN_SHA, 'session_private': descriptor,
        'previous_control_sha256': None, 'observation': {'dom': dom(libraries), 'views': views(libraries)}}
    return stage, record, child, descriptor


def spontaneous(write_completed_at, start=100):
    return {'window': {'write_completed_at': write_completed_at, 'start_elapsed_ms': start, 'end_elapsed_ms': start + 10000,
        'duration_ms': 10000, 'completed_elapsed_ms': start + 10001, 'completed': True, 'ui_actions': 0},
        'earlier': {'dom': [], 'views': {'result': 'not_observed'}}, 'dom': [], 'views': {'result': 'not_observed'},
        'outcome': 'not_observed_within_window'}


def changed_stage(name, predecessor, write_completed_at, start=100):
    stage, record, child, descriptor = baseline_stage()
    libraries = TARGET.expected_libraries(record, name)
    excluded = [row for row in record['expected_libraries'] if row['id'] == TARGET.MOVIES] if name == 'restricted' else []
    invoked_at = start + 10002
    stage.update(name=name, previous_control_sha256=predecessor,
        observation={'spontaneous': spontaneous(write_completed_at, start), 'reload': {
            'action': {'kind': 'page.reload', 'count': 1, 'completed': True, 'status': 200, 'control_sha256': predecessor,
                'invoked_elapsed_ms': invoked_at, 'completed_elapsed_ms': invoked_at + 4},
            'dom': dom(libraries, excluded), 'views': views(libraries, name + '_reload', invoked_at)}})
    return stage, record, child, descriptor


def file_stat(**changes):
    values = {'st_mode': stat.S_IFREG | 0o600, 'st_uid': 0, 'st_gid': 0, 'st_nlink': 1, 'st_dev': 5, 'st_ino': 100,
        'st_size': 32, 'st_mtime_ns': 1000, 'st_ctime_ns': 1000}
    values.update(changes)
    return types.SimpleNamespace(**values)


def permission_run(proven=True):
    before, authenticated, after, admin, proof, original, restricted = sample()
    run = TARGET.Run(types.SimpleNamespace(check_only=False))
    run.op, run.restriction = codec(), RESTRICTION
    run.state, run.before, run.authenticated, run.after = copy.deepcopy(STATE), before, authenticated, after
    run.state['admin_id'] = admin['user_id']
    run.old_b = copy.deepcopy(next(row for row in before['database']['tables']['users'] if row['id'] == H.B))
    run.account = {'username': next(row['name'] for row in before['database']['tables']['users'] if row['id'] == admin['user_id']),
        'password': 'synthetic-only-password'}
    run.input, run.child, run.b_descriptor = bindings()
    run.input_sha, run.sources = INPUT_SHA, run.input['source_closure']
    stage, _, _, _ = baseline_stage()
    run.stages['baseline'] = {'document': stage, 'artifact': {'path': str(TARGET.BROWSER_ROOT / 'stage-baseline.json'), 'sha256': '1' * 64}}
    run.last_stage, run.admin = 'baseline', admin
    run.admin_proven, run.admin_token, run.admin_csrf, run.admin_sha = proven, ADMIN_TOKEN if proven else None, 'c' * 64 if proven else None, ADMIN_SHA if proven else None
    run.b_proof, run.b_token = proof, TOKEN
    run.b_private = {'marker': 'goby-client-library-home-session-v1', 'version': 1, 'input_sha256': INPUT_SHA,
        'source_closure_sha256': TARGET.sha(H.canonical(run.sources)), 'node_process': run.child,
        'controller': run.input['controller'], 'proof': proof, 'token': TOKEN}
    run.original, run.restricted = original, restricted
    run.saved = {}

    def save(name, value, soft=False):
        run.saved[name] = copy.deepcopy(value)
        raw = value if isinstance(value, bytes) else H.canonical(value)
        return {'path': str(TARGET.ROOT / name), 'sha256': TARGET.sha(raw)}

    run.save = save
    for field in ('restore_reservation', 'cleanup_reservation'):
        value = {'marker': TARGET.MARKER, 'input_sha256': INPUT_SHA, 'kind': field, 'maximum_attempts': 1}
        setattr(run, field, {'value': value, 'artifact': save(field.replace('_', '-') + '.json', value)})
    run.check = mock.Mock(return_value=None)
    run.read_ipc = mock.Mock(return_value=None)
    return run


def read_reservation(run, artifact):
    for value in (run.restore_reservation, run.cleanup_reservation):
        if value is not None and artifact == value['artifact']: return copy.deepcopy(value['value'])
    raise AssertionError('An unreserved private record was requested.')


def phase_snapshot(run, name):
    snapshot = copy.deepcopy(run.authenticated)
    final = snapshot['database']['tables']
    current = copy.deepcopy(run.restricted if name == 'restricted' else run.original)
    current['Revision'] = '4' if name == 'restricted' else '5'
    updates = [copy.deepcopy(row) for row in run.after['database']['tables']['activity_entries']
        if row['action'] == 'user.updated' and row['actor_credential_id'] == ADMIN_SESSION and row['revision'] <= int(current['Revision'])]
    final['activity_entries'].extend(updates)
    row = next(row for row in final['users'] if row['id'] == H.B)
    row.update(policy=RESTRICTION.raw_restored_policy(run.old_b, current), management_revision=int(current['Revision']), updated_at=updates[-1]['created_at'])
    snapshot['database']['metadata']['captured_at'] = (H.instant(row['updated_at']) + dt.timedelta(seconds=1)).isoformat()
    snapshot['database']['sequences']['activity_entries_id_seq'] = {'last_value': updates[-1]['id'], 'is_called': True}
    return snapshot, current


def managed_response(current):
    return {'User': {'Id': H.B, **current}, 'CurrentSessionRevoked': False}


class Response:
    def __init__(self, status, body=None, cookie=None):
        self.status, self.raw, self.cookie = status, b'' if body is None else H.canonical(body), cookie

    def getheader(self, name):
        return str(len(self.raw)) if name == 'Content-Length' else self.cookie if name == 'Set-Cookie' else None

    def read(self, maximum):
        return self.raw[:maximum]


class Wire:
    def __init__(self, responses):
        self.responses, self.calls = list(responses), []

    def factory(self, host, port, timeout):
        if (host, port, timeout) != ('127.0.0.1', 18198, 8): raise AssertionError('A synthetic request left the exact candidate route.')
        owner = self
        class Connection:
            def request(self, method, path, body, headers): owner.calls.append((method, path, body, headers))
            def getresponse(self):
                value = owner.responses.pop(0)
                if isinstance(value, Exception): raise value
                return value
            def close(self): pass
        return Connection()


class PermissionGuards(unittest.TestCase):
    def setUp(self):
        patches = [mock.patch.object(subprocess, 'run', side_effect=denied), mock.patch.object(subprocess, 'Popen', side_effect=denied),
            mock.patch.object(socket, 'socket', side_effect=denied), mock.patch.object(socket, 'create_connection', side_effect=denied),
            mock.patch.object(TARGET.http.client, 'HTTPConnection', side_effect=denied), mock.patch.object(os, 'open', side_effect=denied),
            mock.patch.object(os, 'mkdir', side_effect=denied), mock.patch.object(os, 'unlink', side_effect=denied),
            mock.patch.object(os, 'link', side_effect=denied), mock.patch.object(Path, 'read_bytes', side_effect=denied),
            mock.patch.object(Path, 'read_text', side_effect=denied), mock.patch.object(Path, 'write_bytes', side_effect=denied),
            mock.patch.object(Path, 'write_text', side_effect=denied), mock.patch('builtins.open', side_effect=denied),
            mock.patch.object(Path, 'stat', side_effect=denied), mock.patch.object(Path, 'lstat', side_effect=denied),
            mock.patch.object(TARGET.time, 'sleep', side_effect=denied), mock.patch.object(TARGET.signal, 'getsignal', return_value=None),
            mock.patch.object(TARGET.signal, 'signal', return_value=None), mock.patch.object(TARGET.signal, 'setitimer', return_value=None)]
        for patch in patches: patch.start()
        self.addCleanup(lambda: [patch.stop() for patch in reversed(patches)])

    def rejected_delta(self, mutate):
        values = sample()
        mutate(*values)
        with self.assertRaises(Exception): TARGET.validate_full_delta(codec(), RESTRICTION, *values, [{}])

    def test_actual_home_v3_is_the_only_latest_complete_baseline(self):
        self.assertEqual(TARGET.AUTHORITY['current_snapshot'], {'path': str(TARGET.W / 'client-library-ui-baseline-v3/after-full.json'), 'sha256': BASELINE_SHA})
        TARGET.validate_latest(HOME_REPORT, HOME_BROWSER, SNAPSHOT, STATE)
        RESTRICTION.quiescent(SNAPSHOT)
        tables = SNAPSHOT['database']['tables']
        self.assertEqual(tuple(len(tables[name]) for name in ('sessions', 'devices', 'activity_entries')), (71, 61, 157))
        self.assertEqual(sum(row['user_id'] in (H.A, H.B) for row in tables['sessions']), 62)
        self.assertEqual(next(row['management_revision'] for row in tables['users'] if row['id'] == H.B), 3)

    def test_old_scope_wrong_acceptance_and_unrevoked_prior_login_are_rejected(self):
        for variant in ('old_count', 'old_revision', 'old_path', 'client', 'permission', 'failed', 'unrevoked', 'process'):
            report, browser, baseline, state = map(copy.deepcopy, (HOME_REPORT, HOME_BROWSER, SNAPSHOT, STATE))
            if variant == 'old_count': baseline['database']['tables']['sessions'].pop()
            if variant == 'old_revision': next(row for row in baseline['database']['tables']['users'] if row['id'] == H.B)['management_revision'] = 2
            if variant == 'old_path': report['evidence']['after-full.json']['path'] = str(TARGET.W / 'client-library-ui-baseline-v2/after-full.json')
            if variant in ('client', 'permission'): report[variant + '_acceptance' if variant == 'client' else 'permission_ui_acceptance'] = True
            if variant == 'failed': report['result'] = 'failed'
            if variant == 'unrevoked': next(row for row in baseline['database']['tables']['sessions'] if row['id'] == browser['login_proof']['session_id'])['revoked_at'] = None
            if variant == 'process': state['process']['start_ticks'] += 1
            with self.subTest(variant=variant), self.assertRaises(Exception): TARGET.validate_latest(report, browser, baseline, state)

    def test_complete_delta_has_two_tokens_one_device_six_audits_and_restored_b(self):
        values = sample()
        caps = {'SupportedCommands': ['DisplayMessage'], 'DeviceProfile': {'MaxStreamingBitrate': 0}}
        next(row for row in values[2]['database']['tables']['sessions'] if row['id'] == SESSION)['client_capabilities'] = caps
        result = TARGET.validate_full_delta(codec(), RESTRICTION, *values, [caps])
        self.assertEqual(tuple(result[key] for key in ('new_sessions', 'new_devices', 'new_session_audits', 'new_user_update_audits')), (2, 1, 4, 2))
        self.assertEqual((result['b_revision_before'], result['b_revision_after']), ('3', '5'))
        self.assertTrue(result['raw_policy_restored_exactly'])
        self.assertTrue(result['old_rows_sequences_private_preserved_except_declared_b_fields'])

    def test_new_device_issuance_is_bounded_without_equating_it_to_login_ack(self):
        before, authenticated, _, _, proof, _, _ = sample()
        device = next(row for row in authenticated['database']['tables']['devices'] if row['reported_device_id'] == DEVICE)
        self.assertNotEqual(device['created_at'], proof['created_at'])
        for variant in ('before_capture', 'after_login_ack', 'touch_after_authentication'):
            def mutate(before, authenticated, after, _, proof, *rest):
                key = 'last_seen_at' if variant == 'touch_after_authentication' else 'created_at'
                at = H.instant(authenticated['database']['metadata']['captured_at'] if variant == 'touch_after_authentication' else
                    proof['created_at'] if variant == 'after_login_ack' else before['database']['metadata']['captured_at'])
                value = (at + dt.timedelta(seconds=-1 if variant == 'before_capture' else 1)).isoformat()
                for snapshot in (authenticated, after):
                    next(row for row in snapshot['database']['tables']['devices'] if row['reported_device_id'] == DEVICE)[key] = value
            with self.subTest(variant=variant): self.rejected_delta(mutate)

    def test_old_complete_rows_and_private_catalog_values_cannot_change(self):
        for table in ('sessions', 'devices', 'play_sessions', 'user_item_data', 'items'):
            def mutate(_, __, after, *rest): after['database']['tables'][table][0]['guard_unapproved'] = True
            with self.subTest(table=table): self.rejected_delta(mutate)
        for key in ('runtime_sha256', 'browser_sha256', 'added_viewer_credentials', 'recovery'):
            def mutate(_, __, after, *rest): after[key] = {'unapproved': True}
            with self.subTest(key=key): self.rejected_delta(mutate)
        self.rejected_delta(lambda _, __, after, *rest: after['database'].update(catalog={'unapproved': True}))

    def test_only_declared_b_revision_timestamp_changes_are_allowed(self):
        for variant in ('policy', 'name', 'revision', 'timestamp', 'other_user'):
            def mutate(_, __, after, *rest):
                row = next(row for row in after['database']['tables']['users'] if row['id'] == (H.A if variant == 'other_user' else H.B))
                key, value = {'policy': ('policy', {}), 'name': ('name', 'unapproved'), 'revision': ('management_revision', 6),
                    'timestamp': ('updated_at', '2000-01-01T00:00:00Z'), 'other_user': ('policy', {'guard_unapproved': True})}[variant]
                row[key] = value
            with self.subTest(variant=variant): self.rejected_delta(mutate)

    def test_third_token_second_device_and_new_business_rows_are_rejected(self):
        for table in ('sessions', 'devices', 'play_sessions', 'user_item_data', 'client_playback_references', 'encoding_jobs'):
            def mutate(_, __, after, *rest): after['database']['tables'][table].append({'id': 'unapproved'})
            with self.subTest(table=table): self.rejected_delta(mutate)

    def test_exact_owned_audits_sequences_and_token_capabilities_are_required(self):
        for variant in ('actor', 'revision', 'fields', 'sequence', 'inventory', 'admin_caps', 'browser_caps', 'unrevoked', 'old_token'):
            def mutate(before, _, after, admin, proof, *rest):
                if variant in ('actor', 'revision', 'fields'):
                    event = next(row for row in after['database']['tables']['activity_entries'] if row['actor_credential_id'] == ADMIN_SESSION and row['action'] == 'user.updated')
                    event[{'actor': 'actor_credential_id', 'revision': 'revision', 'fields': 'changed_fields'}[variant]] = {'actor': SESSION, 'revision': 7, 'fields': []}[variant]
                if variant == 'sequence': after['database']['sequences']['activity_entries_id_seq']['last_value'] += 1
                if variant == 'inventory': after['database']['sequences']['unapproved_seq'] = {'last_value': 1, 'is_called': True}
                if variant in ('admin_caps', 'browser_caps', 'unrevoked'):
                    row = next(row for row in after['database']['tables']['sessions'] if row['id'] == (ADMIN_SESSION if variant == 'admin_caps' else SESSION))
                    row['revoked_at' if variant == 'unrevoked' else 'client_capabilities'] = None if variant == 'unrevoked' else {'Unexpected': True}
                if variant == 'old_token': before['database']['tables']['sessions'][0]['token_hash'] = '\\x' + proof['token_sha256']
            with self.subTest(variant=variant): self.rejected_delta(mutate)

    def test_owned_update_requires_one_exact_native_b_revision_audit(self):
        _, _, after, admin, _, original, restricted = sample()
        fields = RESTRICTION.changed_fields(original, restricted)
        self.assertTrue(TARGET.owned_update(after, admin['id'], admin['user_id'], 4, fields))
        for variant in ('foreign_actor', 'foreign_user', 'wrong_revision', 'duplicate', 'wrong_fields'):
            changed = copy.deepcopy(after)
            event = next(row for row in changed['database']['tables']['activity_entries'] if row['action'] == 'user.updated' and row['revision'] == 4)
            if variant == 'foreign_actor': event['actor_credential_id'] = SESSION
            if variant == 'foreign_user': event['resource_id'] = H.A
            if variant == 'wrong_revision': event['revision'] = 6
            if variant == 'duplicate': changed['database']['tables']['activity_entries'].append(copy.deepcopy(event))
            if variant == 'wrong_fields': event['changed_fields'] = ['IsAdministrator']
            with self.subTest(variant=variant): self.assertFalse(TARGET.owned_update(changed, admin['id'], admin['user_id'], 4, fields))

    def test_atomic_publication_reads_only_complete_owned_single_link(self):
        self.assertEqual(TARGET.publication_state(None, None), 'missing')
        self.assertEqual(TARGET.publication_state(None, file_stat()), 'publishing')
        self.assertEqual(TARGET.publication_state(file_stat(), None), 'ready')
        self.assertEqual(TARGET.publication_state(file_stat(st_nlink=2), file_stat(st_nlink=2)), 'publishing')
        for final, pending in ((file_stat(st_nlink=2), None), (file_stat(st_nlink=2), file_stat(st_nlink=2, st_ino=101)),
                (file_stat(st_nlink=2), file_stat(st_nlink=2, st_dev=6)), (file_stat(st_nlink=2), file_stat()),
                (file_stat(), file_stat()), (file_stat(st_nlink=3), file_stat(st_nlink=3)),
                (file_stat(st_mode=stat.S_IFLNK | 0o600), None), (file_stat(st_uid=1), None),
                (file_stat(st_mode=stat.S_IFREG | 0o644), None), (file_stat(st_nlink=2), file_stat(st_nlink=2, st_gid=1))):
            with self.subTest(final=vars(final), pending=None if pending is None else vars(pending)), self.assertRaises(Exception):
                TARGET.publication_state(final, pending)
        for pending in (file_stat(st_nlink=2), file_stat(st_uid=1), file_stat(st_mode=stat.S_IFLNK | 0o600)):
            with self.subTest(unpublished=vars(pending)), self.assertRaises(TARGET.PermissionError): TARGET.publication_state(None, pending)

    def test_baseline_stage_binds_exact_source_process_token_and_predecessor(self):
        stage, record, child, descriptor = baseline_stage()
        TARGET.validate_stage(stage, 'baseline', record, INPUT_SHA, child, None, descriptor, TOKEN_SHA)
        for key, value in (('input_sha256', '0' * 64), ('source_closure_sha256', '0' * 64), ('token_sha256', ADMIN_SHA),
                ('controller', {**record['controller'], 'pid': 200}), ('node_process', {**child, 'start_ticks': 1}),
                ('session_private', {**descriptor, 'sha256': '0' * 64}), ('previous_control_sha256', 'a' * 64),
                ('name', 'restored'), ('extra', True)):
            changed = copy.deepcopy(stage); changed[key] = value
            with self.subTest(key=key), self.assertRaises(Exception):
                TARGET.validate_stage(changed, 'baseline', record, INPUT_SHA, child, None, descriptor, TOKEN_SHA)

    def test_physical_views_are_not_inferred_from_frame_or_cached_success(self):
        record, _, _ = bindings(); libraries = record['expected_libraries']
        for variant in ('physical_missing', 'cached', 'foreign_token', 'ambiguous', 'missing_library', 'wrong_name'):
            value = views(libraries); pair = value['pairs'][0]
            if variant == 'physical_missing': value['physical_count'] = 0
            if variant == 'cached': pair['frame']['from_service_worker'] = True
            if variant == 'foreign_token': pair['physical']['token_sha256'] = ADMIN_SHA
            if variant == 'ambiguous': pair['unambiguous'] = False
            if variant == 'missing_library': pair['physical']['projection']['Items'].pop()
            if variant == 'wrong_name': pair['physical']['projection']['Items'][0]['Name'] = 'unapproved'
            with self.subTest(variant=variant), self.assertRaises(Exception): TARGET.validate_views(value, libraries, TOKEN_SHA)

    def test_restriction_removes_only_original_movies_and_requires_card_absence(self):
        record, _, _ = bindings(); all_libraries = record['expected_libraries']
        libraries = TARGET.expected_libraries(record, 'restricted')
        excluded = [row for row in all_libraries if row['id'] == TARGET.MOVIES]
        self.assertEqual({row['id'] for row in libraries}, H.LIBRARIES - {TARGET.MOVIES})
        self.assertEqual(TARGET.expected_libraries(record, 'restored'), all_libraries)
        value = dom(libraries, excluded)
        TARGET.validate_views(views(libraries), libraries, TOKEN_SHA)
        TARGET.validate_dom(value, libraries, True, all_libraries)
        for variant in ('still_visible', 'absent_missing', 'title_only', 'media', 'movie_navigation'):
            changed = copy.deepcopy(value)
            if variant == 'still_visible': changed['excluded_libraries'][0]['matching_id_card_count'] = 1
            if variant == 'absent_missing': changed['excluded_libraries'] = []
            if variant == 'title_only': changed['libraries'][0]['card_id_present'] = False
            if variant == 'media': changed['media_inactive'] = False
            if variant == 'movie_navigation': changed['location']['route'] = 'movies'
            with self.subTest(variant=variant), self.assertRaises(Exception): TARGET.validate_dom(changed, libraries, True, all_libraries)

    def test_passive_absence_cannot_be_promoted_and_ack_window_is_exact(self):
        acknowledged = '2026-09-12T00:00:00+00:00'
        value = spontaneous(acknowledged)
        TARGET.validate_spontaneous(value, acknowledged)
        for variant in ('promoted', 'wrong_ack', 'short', 'action', 'early_sample', 'outside_sample'):
            changed = copy.deepcopy(value)
            if variant == 'promoted': changed['outcome'] = 'observed_correct_membership'
            if variant == 'wrong_ack': changed['window']['write_completed_at'] = '2026-09-12T00:00:01+00:00'
            if variant == 'short': changed['window']['end_elapsed_ms'] -= 1
            if variant == 'action': changed['window']['ui_actions'] = 1
            if variant in ('early_sample', 'outside_sample'):
                changed['dom'] = [{'started_elapsed_ms': 99 if variant == 'early_sample' else 10100,
                    'elapsed_ms': 101 if variant == 'early_sample' else 10101, 'observation': {'passed': True}}]
            with self.subTest(variant=variant), self.assertRaises(Exception): TARGET.validate_spontaneous(changed, acknowledged)
        stage, record, _, _ = changed_stage('restricted', 'a' * 64, acknowledged)
        passive = stage['observation']['spontaneous']
        passive['views'] = views(TARGET.expected_libraries(record, 'restricted'), 'restricted_passive', 101)
        passive['dom'] = [{'started_elapsed_ms': 110, 'elapsed_ms': 111, 'observation': stage['observation']['reload']['dom']}]
        passive['outcome'] = 'observed_correct_membership'
        TARGET.validate_spontaneous(passive, acknowledged, 'restricted', record, TOKEN_SHA)
        passive['views']['pairs'][0]['physical']['finished_elapsed_ms'] = 10101
        with self.assertRaises(Exception): TARGET.validate_spontaneous(passive, acknowledged, 'restricted', record, TOKEN_SHA)

    def test_changed_stage_requires_its_control_reload_and_fresh_views(self):
        acknowledged = '2026-09-12T00:00:00+00:00'
        for name in ('restricted', 'restored'):
            stage, record, child, descriptor = changed_stage(name, 'a' * 64, acknowledged)
            TARGET.validate_stage(stage, name, record, INPUT_SHA, child, 'a' * 64, descriptor, TOKEN_SHA, acknowledged)
            for variant in ('old_views', 'before_reload', 'wrong_control', 'second_reload', 'before_passive', 'wrong_previous'):
                changed = copy.deepcopy(stage); reload = changed['observation']['reload']
                if variant == 'old_views': reload['views'] = views(TARGET.expected_libraries(record, name), 'initial')
                if variant == 'before_reload': reload['views']['pairs'][0]['physical']['request_elapsed_ms'] = 1
                if variant == 'wrong_control': reload['action']['control_sha256'] = 'b' * 64
                if variant == 'second_reload': reload['action']['count'] = 2
                if variant == 'before_passive': reload['action']['invoked_elapsed_ms'] = 10100
                if variant == 'wrong_previous': changed['previous_control_sha256'] = 'b' * 64
                with self.subTest(name=name, variant=variant), self.assertRaises(Exception):
                    TARGET.validate_stage(changed, name, record, INPUT_SHA, child, 'a' * 64, descriptor, TOKEN_SHA, acknowledged)

    def test_abort_repeat_and_cleanup_windows_never_authorize_restriction(self):
        run = permission_run()
        run.approve_request('restrict-user', 'restrict', run.restricted)
        for variant in ('abort', 'repeat', 'restored', 'wrong_stage', 'unproven', 'unreserved'):
            run = permission_run()
            if variant == 'abort': run.aborted = True
            if variant == 'repeat': run.restrict_sent = True
            if variant == 'restored': run.restore_sent = True
            if variant == 'wrong_stage': run.last_stage = 'restricted'
            if variant == 'unproven': run.admin_proven = False
            if variant == 'unreserved': run.restore_reservation = None
            with self.subTest(variant=variant), self.assertRaises(TARGET.PermissionError): run.approve_request('restrict-user', 'restrict', run.restricted)
        run = permission_run()
        run.read_ipc.return_value = ({'marker': 'goby-client-library-permission-abort-v1', 'version': 1,
            'input_sha256': INPUT_SHA, 'source_closure_sha256': TARGET.sha(H.canonical(run.sources)), 'controller': run.input['controller'],
            'node_process': run.child, 'stage': 'baseline', 'failure': 'synthetic_observation_failure'}, {'path': 'owned-abort', 'sha256': 'f' * 64})
        with self.assertRaises(TARGET.PermissionError): run.native_request('restrict-user', 'restrict', run.restricted)
        self.assertTrue(run.aborted)
        self.assertFalse(run.restrict_sent)
        for purpose, body, recovery, cleanup in (('restrict', run.restricted, True, False), ('restrict', run.restricted, False, True),
                ('login', {'Name': run.account['username'], 'Password': run.account['password']}, True, False), ('restore', {}, False, False)):
            with self.subTest(purpose=purpose, recovery=recovery, cleanup=cleanup), self.assertRaises(TARGET.PermissionError):
                run.native_request('invalid-window', purpose, body, recovery=recovery, cleanup=cleanup)
        self.assertEqual(run.request_count, 0)
        run = permission_run(); run.control_attempts.add('restricted')
        with self.assertRaises(TARGET.PermissionError): run.publish_control('restricted')
        run.restoration = 'pending'
        with self.assertRaises(TARGET.PermissionError): run.publish_control('close')
        with self.assertRaises(TARGET.PermissionError): run.wait_stage('baseline', 1)

    def test_foreign_revision_and_unowned_update_never_authorize_restore(self):
        for variant in ('foreign_revision', 'foreign_audit'):
            run = permission_run(); run.restrict_sent = True; run.restoration = 'pending'
            snapshot, current = phase_snapshot(run, 'restricted')
            if variant == 'foreign_revision': current['Revision'] = '6'
            if variant == 'foreign_audit': snapshot['database']['tables']['activity_entries'][-1]['actor_credential_id'] = SESSION
            run.snapshot = mock.Mock(return_value=snapshot)
            wire = Wire([Response(200, managed_response(current))])
            with self.subTest(variant=variant), mock.patch.object(TARGET.http.client, 'HTTPConnection', side_effect=wire.factory):
                with self.assertRaises(TARGET.PermissionError): run.restore()
            self.assertEqual([method for method, _, _, _ in wire.calls], ['GET'])
            self.assertFalse(run.restore_sent)
            self.assertNotEqual(run.restoration, 'confirmed')

    def test_committed_restriction_with_lost_ack_restores_once_from_owned_state(self):
        for lost_restore_ack in (False, True):
            run = permission_run(); run.restrict_sent = True; run.restrict_status = None; run.restoration = 'pending'
            restricted, current = phase_snapshot(run, 'restricted'); restored, final = phase_snapshot(run, 'restored')
            run.snapshot = mock.Mock(side_effect=[restricted, restored])
            wire = Wire([Response(200, managed_response(current)), OSError('Synthetic lost response') if lost_restore_ack else Response(200, managed_response(final)),
                Response(200, managed_response(final))])
            with self.subTest(lost_restore_ack=lost_restore_ack), mock.patch.object(H, 'read_record', side_effect=lambda value: read_reservation(run, value)), \
                    mock.patch.object(TARGET.http.client, 'HTTPConnection', side_effect=wire.factory):
                run.restore(); run.restore()
            self.assertEqual([method for method, _, _, _ in wire.calls], ['GET', 'PUT', 'GET'])
            self.assertEqual(H.decode(wire.calls[1][2]), {**run.original, 'Revision': '4'})
            self.assertEqual(run.restoration, 'confirmed')
            self.assertTrue(run.restore_sent)
            self.assertEqual(run.reconcile_count, 1)
            if lost_restore_ack:
                self.assertIsNone(run.restore_ack_time)
                self.assertTrue(any(error['stage'] == 'restore_acknowledgement' for error in run.errors))
        run = permission_run(); run.restrict_sent = run.restore_sent = True; run.restoration = 'pending'
        snapshot, current = phase_snapshot(run, 'restored'); run.snapshot = mock.Mock(return_value=snapshot)
        wire = Wire([Response(200, managed_response(current))])
        with mock.patch.object(TARGET.http.client, 'HTTPConnection', side_effect=wire.factory): run.restore()
        self.assertEqual(run.restoration, 'confirmed')
        self.assertEqual([method for method, _, _, _ in wire.calls], ['GET'])

    def test_login_ack_journal_failure_retains_only_new_native_cleanup(self):
        run = permission_run(proven=False)
        original_save = run.save
        def save(name, value, soft=False):
            if name == 'native-login-response-private.json': raise OSError('Synthetic login journal failure')
            return original_save(name, value, soft)
        run.save = save
        login = {'User': {'Id': run.state['admin_id'], 'Name': run.account['username'], 'IsAdministrator': True, 'IsDisabled': False}, 'CSRFToken': 'c' * 64}
        wire = Wire([Response(200, login, 'goby_session=' + ADMIN_TOKEN + '; Path=/admin'), Response(204), Response(401)])
        with mock.patch.object(H, 'read_record', side_effect=lambda value: read_reservation(run, value)), \
                mock.patch.object(TARGET.http.client, 'HTTPConnection', side_effect=wire.factory):
            with self.assertRaises(OSError): run.native_request('native-login', 'login', {'Name': run.account['username'], 'Password': run.account['password']})
            self.assertTrue(run.admin_proven)
            self.assertEqual(run.admin_sha, ADMIN_SHA)
            run.close_admin()
        self.assertTrue(run.admin_closed)
        self.assertEqual([(method, path) for method, path, _, _ in wire.calls],
            [('POST', '/admin/v1/session'), ('DELETE', '/admin/v1/session'), ('GET', '/admin/v1/session')])
        self.assertTrue(all(headers.get('Cookie') == 'goby_session=' + ADMIN_TOKEN for _, _, _, headers in wire.calls[1:]))
        run = permission_run(proven=False)
        run.before['database']['tables']['sessions'][0]['token_hash'] = '\\x' + ADMIN_SHA
        with self.assertRaises(TARGET.PermissionError): run.admin_acknowledge({'complete': True, 'status': 200, 'body': login, 'set_cookie': 'goby_session=' + ADMIN_TOKEN})
        self.assertFalse(run.admin_proven)
        run = permission_run(proven=False); wire = Wire([OSError('Synthetic lost login response')])
        body = {'Name': run.account['username'], 'Password': run.account['password']}
        with mock.patch.object(TARGET.http.client, 'HTTPConnection', side_effect=wire.factory):
            result = run.native_request('native-login', 'login', body)
            with self.assertRaises(TARGET.PermissionError): run.native_request('another-login', 'login', body)
        self.assertFalse(result['complete'] or run.admin_proven)
        self.assertTrue(run.login_sent)
        self.assertEqual(len(wire.calls), 1)

    def test_persistent_journal_failure_still_restores_and_closes_exact_two_tokens(self):
        run = permission_run(); run.restrict_sent = True; run.restoration = 'pending'; run.root_fd = 88
        restricted, current = phase_snapshot(run, 'restricted'); restored, final = phase_snapshot(run, 'restored')
        run.snapshot = mock.Mock(side_effect=[restricted, restored])
        run.save = types.MethodType(TARGET.Run.save, run)
        wire = Wire([Response(200, managed_response(current)), Response(200, managed_response(final)), Response(200, managed_response(final)),
            Response(204), Response(401), Response(204), Response(401)])
        fallback = H.fallback_http
        with mock.patch.object(os, 'open', side_effect=OSError('Synthetic persistent journal failure')), \
                mock.patch.object(H, 'read_record', side_effect=lambda value: read_reservation(run, value)), \
                mock.patch.object(TARGET.http.client, 'HTTPConnection', side_effect=wire.factory), \
                mock.patch.object(H, 'fallback_http', side_effect=lambda token, revoked: fallback(token, revoked, wire.factory)):
            run.restore()
            run.worker_closed = True
            run.fallback_b(run.authenticated)
            run.close_admin()
        self.assertEqual(run.restoration, 'confirmed')
        self.assertTrue(run.admin_closed and run.b_fallback)
        self.assertTrue(run.errors)
        self.assertTrue(all(error['stage'].startswith('journal_') for error in run.errors))
        self.assertEqual([(method, path) for method, path, _, _ in wire.calls],
            [('GET', '/admin/v1/users/' + H.B), ('PUT', '/admin/v1/users/' + H.B), ('GET', '/admin/v1/users/' + H.B),
             ('POST', '/emby/Sessions/Logout'), ('GET', '/emby/System/Info'), ('DELETE', '/admin/v1/session'), ('GET', '/admin/v1/session')])
        for _, path, _, headers in wire.calls:
            if path.startswith('/emby/'):
                self.assertEqual(headers['X-Emby-Token'], TOKEN)
                self.assertNotIn('Cookie', headers)
            else:
                self.assertEqual(headers['Cookie'], 'goby_session=' + ADMIN_TOKEN)
                self.assertNotIn('X-Emby-Token', headers)
        completed = permission_run(); completed.child = None; completed.admin_closed = True; completed.restoration = 'confirmed'
        completed.delta, completed.errors = {'synthetic_complete_delta': True}, copy.deepcopy(run.errors)
        completed.load = completed.prepare = completed.run_flow = completed.restore = completed.collect = mock.Mock(return_value=None)
        result = completed.execute()
        self.assertEqual(result['result'], 'failed')
        self.assertFalse(result['permission_ui_acceptance'] or result['client_acceptance'])
        self.assertTrue(result['administrator_closed'])
        self.assertEqual(result['restoration'], 'confirmed')

    def test_cleanup_requires_worker_release_new_receipt_and_original_reservations(self):
        run = permission_run(); run.launched = True; run.worker_started = 0
        run.poll_worker = mock.Mock(return_value={'MainPID': '0', 'ActiveState': 'inactive', 'ExecMainStatus': '0'})
        run.cgroup_empty = mock.Mock(return_value=False)
        with mock.patch.object(TARGET.time, 'monotonic', side_effect=[0, 701]), mock.patch.object(TARGET.time, 'sleep', return_value=None):
            with self.assertRaises(TARGET.PermissionError): run.await_worker()
        self.assertFalse(run.worker_closed)
        run.cgroup_empty.return_value = True
        with mock.patch.object(TARGET.time, 'monotonic', return_value=0): run.await_worker()
        self.assertTrue(run.worker_closed)
        for variant in ('worker_alive', 'already_used', 'old_token', 'foreign_receipt', 'changed_reservation'):
            run = permission_run(); run.worker_closed = variant != 'worker_alive'
            if variant == 'already_used': run.b_fallback = True
            if variant == 'old_token': run.before['database']['tables']['sessions'][0]['token_hash'] = '\\x' + TOKEN_SHA
            if variant == 'foreign_receipt': run.b_private['node_process'] = {**run.child, 'pid': 999}
            with self.subTest(variant=variant), mock.patch.object(H, 'read_record', return_value={'unapproved': True}):
                with self.assertRaises(Exception): run.fallback_b(run.authenticated)
            self.assertEqual(run.request_count, 0)
        run = permission_run(); run.restrict_sent = True; run.restore_authorized = True; run.restore_body = {**run.original, 'Revision': '4'}
        with mock.patch.object(H, 'read_record', return_value={'unapproved': True}):
            with self.assertRaises(TARGET.PermissionError): run.native_request('restore-user', 'restore', run.restore_body, recovery=True)
            run.close_admin()
        self.assertFalse(run.restore_sent or run.admin_closed)
        self.assertEqual(run.request_count, 0)

    def test_ipc_waits_only_for_bounded_publication_and_never_parses_torn_pairs(self):
        path = TARGET.BROWSER_ROOT / 'stage-baseline.json'
        run = TARGET.Run(types.SimpleNamespace())
        with mock.patch.object(Path, 'lstat', side_effect=[file_stat(st_nlink=2), file_stat(st_nlink=2), file_stat(st_nlink=2)]), \
                mock.patch.object(TARGET.time, 'monotonic', return_value=100), mock.patch.object(H, 'protected', side_effect=denied):
            self.assertIsNone(run.read_ipc(path))
        with mock.patch.object(Path, 'lstat', side_effect=[file_stat(st_nlink=2), file_stat(st_nlink=2), file_stat(st_nlink=2)]), \
                mock.patch.object(TARGET.time, 'monotonic', return_value=106), mock.patch.object(H, 'protected', side_effect=denied):
            with self.assertRaises(TARGET.PermissionError): run.read_ipc(path)
        run = TARGET.Run(types.SimpleNamespace())
        with mock.patch.object(Path, 'lstat', side_effect=[file_stat(st_nlink=2), FileNotFoundError(), file_stat()]), \
                mock.patch.object(H, 'protected', side_effect=denied):
            self.assertIsNone(run.read_ipc(path))
        raw = b'{"complete":true}\n'
        with mock.patch.object(Path, 'lstat', side_effect=[file_stat(), FileNotFoundError(), file_stat()]), \
                mock.patch.object(H, 'protected', return_value=raw):
            self.assertEqual(run.read_ipc(path), ({'complete': True}, {'path': str(path), 'sha256': TARGET.sha(raw)}))

    def test_failure_collection_still_captures_after_state_and_media_drift(self):
        run = permission_run(); run.authenticated = None; run.media = {'files': [{'sha256': 'a' * 64}]}
        run.error('observation', TARGET.PermissionError('Synthetic observation failure'))
        run.snapshot = mock.Mock(return_value=copy.deepcopy(run.before))
        run.read_browser_final = mock.Mock(); run.close_admin = mock.Mock()
        run.ext = types.SimpleNamespace(media_witness=mock.Mock(return_value={'files': [{'sha256': 'b' * 64}]}))
        run.collect()
        self.assertEqual([call.args[0] for call in run.snapshot.call_args_list], ['after-browser-full.json', 'after-full.json'])
        self.assertEqual(run.after, run.before)
        self.assertIn('media-after.json', run.saved)
        self.assertEqual([error['stage'] for error in run.errors], ['observation', 'private_source_media_preservation'])

    def test_ui_acceptance_keeps_passive_absence_and_exact_stage_control_chain(self):
        run = permission_run(); run.admin_closed = run.worker_closed = True; run.restoration = 'confirmed'
        run.terminal = {'ExecMainStatus': '0'}
        started = H.instant(run.before['database']['metadata']['captured_at'])
        for index, (name, start) in enumerate((('restricted', 100), ('restored', 20000))):
            acknowledged = (started + dt.timedelta(milliseconds=start)).isoformat()
            document = {'name': name, 'revision': str(4 + index), 'write_completed_at': acknowledged,
                'previous_stage_sha256': run.stages[run.last_stage]['artifact']['sha256'], 'restoration': 'pending' if name == 'restricted' else 'confirmed'}
            artifact = {'path': str(TARGET.ROOT / ('control-' + name + '.json')), 'sha256': TARGET.sha(H.canonical(document))}
            run.controls[name] = {'document': document, 'artifact': artifact}
            stage, _, _, _ = changed_stage(name, artifact['sha256'], acknowledged, start)
            run.stages[name] = {'document': stage, 'artifact': {'path': str(TARGET.BROWSER_ROOT / ('stage-' + name + '.json')), 'sha256': TARGET.sha(H.canonical(stage))}}
            run.last_stage = name
        close = {'name': 'close', 'restoration': 'confirmed', 'previous_stage_sha256': run.stages['restored']['artifact']['sha256']}
        run.controls['close'] = {'document': close, 'artifact': {'path': str(TARGET.ROOT / 'control-close.json'), 'sha256': TARGET.sha(H.canonical(close))}}
        run.browser_report = {'result': 'passed', 'outcome': 'permission_observation_after_explicit_reload', 'permission_ui_acceptance': True,
            'client_acceptance': False, 'login_proof': run.b_proof, 'restoration': 'confirmed', 'closed_after_restoration_confirmation': True,
            'started_at': started.isoformat(), **{name: run.stages[name]['document']['observation'] for name in ('baseline', 'restricted', 'restored')},
            'control_close': {**run.controls['close']['artifact'], 'value': close},
            'stages': [{'name': name, **run.stages[name]['artifact']} for name in ('baseline', 'restricted', 'restored')],
            'controls': [{'name': name, **run.controls[name]['artifact'], 'value': run.controls[name]['document']} for name in ('restricted', 'restored')],
            'actor': {'websocket_handshake_budget': 3, 'logout': {'status': 204, 'login_view_visible': True},
                'proxy_logout': {'status': 204, 'completed': True, 'token_fingerprint': TOKEN_SHA},
                'session_proof': {'outcome': 'all_observed_logout_tokens_rejected', 'entries': [{'result': 'logout_token_rejected', 'token_fingerprint': TOKEN_SHA}]}},
            'closure': {'context_closed': True, 'browser_closed': True, 'proxy_closed': True, 'http_pending': 0, 'websocket_pending': 0,
                'websocket_active': 0, 'sockets_remaining': 0, 'websocket_opened': 3, 'websocket_closed': 3, 'cleanup_failures': []}}
        TARGET.Run.validate_ui_result(run)
        self.assertEqual(run.browser_report['restricted']['spontaneous']['outcome'], 'not_observed_within_window')
        valid = copy.deepcopy(run.browser_report)
        for variant in ('client', 'close', 'clock', 'handshakes'):
            run.browser_report = copy.deepcopy(valid)
            if variant == 'client': run.browser_report['client_acceptance'] = True
            if variant == 'close': run.browser_report['control_close']['sha256'] = '0' * 64
            if variant == 'clock': run.browser_report['started_at'] = (started + dt.timedelta(milliseconds=1)).isoformat()
            if variant == 'handshakes': run.browser_report['closure'].update(websocket_opened=4, websocket_closed=4)
            with self.subTest(variant=variant), self.assertRaises(TARGET.PermissionError): run.validate_ui_result()


def read_pinned(path, digest, maximum):
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or info.st_nlink != 1 or info.st_mode & 0o022 or info.st_size > maximum:
        raise RuntimeError('A pure guard input is not an owned bounded regular file.')
    raw = path.read_bytes()
    if hashlib.sha256(raw).hexdigest() != digest: raise RuntimeError('A pure guard input digest differs.')
    return raw


def module(path, digest, name):
    raw = read_pinned(path, digest, 2 << 20)
    value = types.ModuleType(name); value.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), value.__dict__)
    return value


def main():
    global TARGET, H, RESTRICTION, SNAPSHOT, HOME_REPORT, HOME_BROWSER, STATE
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--operator', type=Path, required=True)
    parser.add_argument('--operator-sha256', required=True)
    parser.add_argument('--baseline-snapshot', type=Path, required=True)
    parser.add_argument('--report', type=Path)
    args = parser.parse_args()
    TARGET = module(args.operator, args.operator_sha256, 'permission_controller_under_test')
    H = module(TARGET.HOME_SOURCE, TARGET.HOME_SHA, 'frozen_home_permission_helpers'); TARGET.H = H
    path, digest = H.HELPERS['restriction']
    RESTRICTION = module(path, digest, 'frozen_permission_restriction_helpers')
    if args.baseline_snapshot != Path(TARGET.AUTHORITY['current_snapshot']['path']) or TARGET.AUTHORITY['current_snapshot']['sha256'] != BASELINE_SHA:
        raise RuntimeError('Only the fixed latest Home v3 after-snapshot is guard data.')
    SNAPSHOT = H.decode(read_pinned(args.baseline_snapshot, BASELINE_SHA, 32 << 20))
    HOME_REPORT, HOME_BROWSER = (H.read_record(TARGET.AUTHORITY[key]) for key in ('home_report', 'home_browser'))
    STATE = H.decode(read_pinned(H.STATE, H.STATE_SHA, 32 << 20))
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(PermissionGuards))
    report = {'marker': 'goby-client-library-permission-guards-v1', 'operator_sha256': args.operator_sha256,
        'home_sha256': TARGET.HOME_SHA, 'restriction_sha256': digest, 'baseline_sha256': BASELINE_SHA,
        'tests_run': result.testsRun, 'failures': len(result.failures), 'errors': len(result.errors), 'skipped': len(result.skipped),
        'result': 'passed' if result.wasSuccessful() and not result.skipped else 'failed',
        'business_http_requests': 0, 'database_commands': 0, 'service_commands': 0, 'browser_started': False}
    if args.report:
        if not args.report.is_relative_to(TARGET.W) or '..' in args.report.parts or args.report.suffix != '.json':
            raise RuntimeError('The exclusive guard report must be in the private workspace.')
        fd = os.open(args.report, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'wb') as handle:
            handle.write(H.canonical(report) + b'\n'); handle.flush(); os.fsync(handle.fileno())
    print(H.exact(report))
    return 0 if report['result'] == 'passed' else 1


if __name__ == '__main__':
    sys.exit(main())
