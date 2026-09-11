#!/usr/bin/env python3
"""Pure guards for the B-only Home controller; no browser or business requests.

The fixed private snapshot is immutable test data. Every test fences real
network, subprocess and filesystem access after importing the pinned sources.
Only the caller-selected new guard report is written after the tests finish.
"""

import argparse
import copy
import datetime as dt
import hashlib
import json
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
TARGET = RESTRICTION = SNAPSHOT = RECOVERY = HISTORY = API = INSPECTION = RECOVERY_BASELINE = PRIOR_HOME = None
SERVER = 'c7cfd76b1dee728b2bad523793a37ccb'
TOKEN = 'Z' * 43
FINGERPRINT = hashlib.sha256(TOKEN.encode('utf-8')).hexdigest()
SESSION = 'd7' * 16
DEVICE = 'home-guard-new-device'
INPUT_SHA = 'e3' * 32


def denied(*_, **__):
    raise AssertionError('A pure guard attempted real I/O or subprocess execution.')


def codec():
    return types.SimpleNamespace(equal_json=lambda left, right: TARGET.canonical(left) == TARGET.canonical(right),
        canonical_json=TARGET.canonical, ENV={'PATH': '/usr/bin:/bin'})


def sample():
    before, after = copy.deepcopy(SNAPSHOT), copy.deepcopy(SNAPSHOT)
    tables = after['database']['tables']
    start = TARGET.instant(before['database']['metadata']['captured_at']) + dt.timedelta(seconds=1)
    at = lambda seconds: (start + dt.timedelta(seconds=seconds)).isoformat()
    proof = {'token_sha256': FINGERPRINT, 'session_id': SESSION, 'user_id': TARGET.B, 'device_id': DEVICE,
        'server_id': SERVER, 'client_name': 'Emby Web', 'device_name': 'Observed browser', 'client_version': '4.9.5.0',
        'created_at': at(1), 'created_at_source': 'SessionInfo.LastActivityDate', 'kind': 'emby', 'slot': 'B',
        'frame_login_finished': True, 'physical_login_completed': True, 'request_metadata_matches': True}
    device_id = before['database']['sequences']['devices_id_seq']['last_value'] + int(before['database']['sequences']['devices_id_seq']['is_called'])
    audit_id = before['database']['sequences']['activity_entries_id_seq']['last_value'] + int(before['database']['sequences']['activity_entries_id_seq']['is_called'])
    row = copy.deepcopy(next(value for value in tables['sessions'] if value['user_id'] == TARGET.B and value['kind'] == 'emby'))
    row.update(id=SESSION, token_hash='\\x' + FINGERPRINT, device_id=DEVICE, client_name=proof['client_name'],
        device_name=proof['device_name'], client_version=proof['client_version'], device_registry_id=device_id,
        created_at=at(1), last_seen_at=at(25), expires_at=at(1 + 30 * 86400), revoked_at=at(26), client_capabilities={})
    tables['sessions'].append(row)
    device = copy.deepcopy(tables['devices'][0])
    device.update(id=device_id, reported_device_id=DEVICE, reported_name=proof['device_name'], app_name=proof['client_name'],
        app_version=proof['client_version'], last_user_id=TARGET.B, ip_address='127.0.0.1', custom_name=None,
        deleted_at=None, revision=1, created_at=at(0), last_seen_at=at(25))
    tables['devices'].append(device)
    template = next(value for value in tables['activity_entries'] if value['action'] == 'session.login')
    for offset, action in enumerate(('session.login', 'session.revoked')):
        event = copy.deepcopy(template)
        event.update(id=audit_id + offset, action=action, source='emby', actor_kind='user', actor_id=TARGET.B,
            actor_credential_id=SESSION, resource_kind='session', resource_id=SESSION, revision=0, changed_fields=[],
            severity='Info', affected_count=1, state='', created_at=at(1 if offset == 0 else 26))
        tables['activity_entries'].append(event)
    after['database']['sequences']['devices_id_seq'] = {'last_value': device_id, 'is_called': True}
    after['database']['sequences']['activity_entries_id_seq'] = {'last_value': audit_id + 1, 'is_called': True}
    after['database']['metadata']['captured_at'] = at(30)
    return before, after, proof


def bindings(proof):
    controller = {'pid': 100, 'start_ticks': '123456', 'boot_id': TARGET.PROCESS['boot_id'], 'unit': TARGET.UNIT}
    child = {'pid': 101, 'start_ticks': '123457', 'boot_id': TARGET.PROCESS['boot_id'], 'uid': 0, 'gid': 0,
        'executable_path': '/usr/bin/node', 'executable_sha256': 'a' * 64, 'cgroup': TARGET.CGROUP}
    record = {'mode': 'b-home-views-reload', 'source_closure': {'/owned/home.mjs': 'b' * 64}, 'controller': controller,
        'candidate': {'server_id': SERVER}, 'authority': {**{key: TARGET.AUTHORITY[key] for key in ('api_report', 'inspection', 'recovery', 'prior_home', 'current_snapshot')},
            'before_snapshot': {'path': str(TARGET.ROOT / 'before-full.json'), 'sha256': 'a' * 64}}}
    private = {'marker': 'goby-client-library-home-session-v1', 'version': 1, 'input_sha256': INPUT_SHA,
        'source_closure_sha256': TARGET.sha(TARGET.canonical(record['source_closure'])), 'node_process': child,
        'controller': controller, 'proof': proof, 'token': TOKEN}
    caps = {'marker': 'goby-client-library-home-capabilities-v1', 'version': 1, 'input_sha256': INPUT_SHA,
        'source_closure_sha256': private['source_closure_sha256'], 'node_process': child,
        'session_id': SESSION, 'token_sha256': FINGERPRINT, 'entries': []}
    return record, child, private, caps


def entry(number, value, start=10, response=20, finished=21):
    raw = TARGET.exact(value)
    return {'physical_id': number, 'kind': 'full', 'request_elapsed_ms': start, 'response_elapsed_ms': response,
        'finished_elapsed_ms': finished, 'token_matches_session': True, 'status': 204, 'completed': True,
        'body_utf8': raw, 'body_sha256': TARGET.sha(raw.encode('utf-8')), 'query': {}}


def browser_report(record, child, proof):
    return {'marker': 'goby-client-library-home-report-v1', 'version': 1, 'mode': record['mode'], 'input_sha256': INPUT_SHA,
        'source_closure_sha256': TARGET.sha(TARGET.canonical(record['source_closure'])), 'candidate': record['candidate'],
        'authority': copy.deepcopy(record['authority']),
        'controller': record['controller'], 'node_process': child, 'client_acceptance': False, 'permission_ui_acceptance': False,
        'result': 'passed', 'outcome': 'baseline_observation', 'failure': None, 'login_proof': proof,
        'session_private': {'path': 'owned-session', 'sha256': 'a' * 64}, 'capabilities_private': {'path': 'owned-capabilities'},
        'initial': {'views': {'result': 'passed'}, 'dom': {'passed': True}},
        'reload': {'views': {'result': 'passed'}, 'dom': {'passed': True}, 'action': {'kind': 'page.reload', 'count': 1, 'completed': True}},
        'actor': {'login': {'request_count': 1, 'status': 200}, 'ordinary_authority_confirmed': True, 'page_error_count': 0,
            'logout': {'status': 204, 'login_view_visible': True},
            'proxy_logout': {'status': 204, 'completed': True, 'token_fingerprint': FINGERPRINT},
            'session_proof': {'outcome': 'all_observed_logout_tokens_rejected',
                'entries': [{'result': 'logout_token_rejected', 'token_fingerprint': FINGERPRINT}]}},
        'closure': {'context_closed': True, 'browser_closed': True, 'proxy_closed': True, 'http_pending': 0,
            'websocket_pending': 0, 'websocket_active': 0, 'websocket_opened': 2, 'websocket_closed': 2,
            'sockets_remaining': 0, 'cleanup_failures': []}}


class Response:
    def __init__(self, status, raw=b''):
        self.status, self.raw = status, raw

    def getheader(self, name):
        return str(len(self.raw)) if name == 'Content-Length' else None

    def read(self, maximum):
        return self.raw[:maximum]


class Wire:
    def __init__(self, statuses):
        self.statuses, self.calls = list(statuses), []

    def factory(self, host, port, timeout):
        if (host, port, timeout) != ('127.0.0.1', 18198, 8): raise AssertionError('The exact candidate transport changed.')
        owner = self
        class Connection:
            def request(self, method, path, body, headers):
                owner.calls.append((method, path, body, headers))
            def getresponse(self):
                return Response(owner.statuses.pop(0))
            def close(self):
                pass
        return Connection()


class HomeGuards(unittest.TestCase):
    def setUp(self):
        self.patches = [mock.patch.object(subprocess, 'run', side_effect=denied), mock.patch.object(subprocess, 'Popen', side_effect=denied),
            mock.patch.object(socket, 'socket', side_effect=denied), mock.patch.object(socket, 'create_connection', side_effect=denied),
            mock.patch.object(TARGET.http.client, 'HTTPConnection', side_effect=denied), mock.patch.object(os, 'open', side_effect=denied),
            mock.patch.object(os, 'mkdir', side_effect=denied), mock.patch.object(os, 'unlink', side_effect=denied),
            mock.patch.object(Path, 'read_bytes', side_effect=denied), mock.patch.object(Path, 'read_text', side_effect=denied),
            mock.patch('builtins.open', side_effect=denied)]
        for value in self.patches: value.start()
        self.addCleanup(lambda: [value.stop() for value in reversed(self.patches)])

    def rejected_delta(self, mutation):
        before, after, proof = sample()
        mutation(before, after, proof)
        with self.assertRaises(Exception): TARGET.validate_delta(codec(), RESTRICTION, before, after, proof, SERVER, [{}])

    def test_current_complete_baseline(self):
        self.assertEqual(TARGET.AUTHORITY['current_snapshot']['sha256'], '27e4627e774d96ab87fdb51fd5093dafcb01c529b0a8753fd50db566aadbfd26')
        TARGET.validate_baseline(SNAPSHOT)
        RESTRICTION.quiescent(SNAPSHOT)
        for value in ('1', 1, 2):
            changed = copy.deepcopy(SNAPSHOT)
            next(row for row in changed['database']['tables']['users'] if row['id'] == TARGET.B)['management_revision'] = value
            with self.subTest(revision=value), self.assertRaises(Exception): TARGET.validate_baseline(changed)

    def test_old_unmaterialized_policy_is_not_current(self):
        changed = copy.deepcopy(SNAPSHOT)
        next(row for row in changed['database']['tables']['users'] if row['id'] == TARGET.B)['policy'] = {}
        with self.assertRaises(Exception): TARGET.validate_baseline(changed)

    def test_actual_recovery_and_historical_inspection_have_distinct_authority(self):
        self.assertEqual(TARGET.AUTHORITY['recovery'], {'path': '/opt/goby-test/exec-work-m3e/client-library-ui-session-recovery-01/report.json',
            'sha256': '09b2fad0de6dcce6e1d6ecb85a700e94673642f3c5641d3d7dff6bab43e66ffb'})
        state = {'schema': 27, 'phase': 'ready', 'stage': 'complete', 'process': TARGET.PROCESS,
            'binary_sha256': TARGET.BINARY_SHA, 'viewer_id': TARGET.B, 'added_viewer': {'user_id': TARGET.A}}
        TARGET.validate_authority(API, INSPECTION, state, SNAPSHOT, RECOVERY, RECOVERY_BASELINE, PRIOR_HOME)
        self.assertNotEqual(INSPECTION['current_snapshot_sha256'], TARGET.AUTHORITY['current_snapshot']['sha256'])
        with self.assertRaises(Exception): TARGET.validate_authority(API, INSPECTION, state, HISTORY, RECOVERY, RECOVERY_BASELINE, PRIOR_HOME)

    def test_old_67_session_baseline_cannot_be_reused(self):
        self.assertEqual(len(HISTORY['database']['tables']['sessions']), 67)
        with self.assertRaises(Exception): TARGET.validate_baseline(HISTORY)

    def test_wrong_recovery_descriptor_or_unrevoked_historical_session_is_rejected(self):
        for variant in ('hash', 'path', 'failed', 'target', 'admin'):
            recovery, baseline = copy.deepcopy(RECOVERY), copy.deepcopy(RECOVERY_BASELINE)
            if variant == 'hash': recovery['evidence']['after-full.json']['sha256'] = '0' * 64
            if variant == 'path': recovery['evidence']['after-full.json']['path'] = TARGET.AUTHORITY['inspection_snapshot']['path']
            if variant == 'failed': recovery['result'] = 'failed'
            if variant in ('target', 'admin'):
                identifier = TARGET.RECOVERED_B if variant == 'target' else TARGET.RECOVERY_ADMIN
                next(row for row in baseline['database']['tables']['sessions'] if row['id'] == identifier)['revoked_at'] = None
            with self.subTest(variant=variant), self.assertRaises(Exception): TARGET.validate_recovery(recovery, baseline)

    def test_old_69_session_recovery_baseline_is_historical(self):
        self.assertEqual(len(RECOVERY_BASELINE['database']['tables']['sessions']), 69)
        TARGET.validate_recovery(RECOVERY, RECOVERY_BASELINE)
        with self.assertRaises(Exception): TARGET.validate_baseline(RECOVERY_BASELINE)

    def test_actual_v2_home_failure_preserves_failed_ui_and_verified_delta(self):
        self.assertEqual(TARGET.AUTHORITY['prior_home'], {'path': '/opt/goby-test/exec-work-m3e/client-library-ui-baseline-v2/report.json',
            'sha256': '54c02dbc41273a5043853d31126dec7455641d5efbf73ee20b97b13d8826ec23'})
        TARGET.validate_prior_home(PRIOR_HOME, SNAPSHOT)
        self.assertEqual(PRIOR_HOME['result'], 'failed')
        self.assertFalse(PRIOR_HOME['client_acceptance'])
        changed = copy.deepcopy(PRIOR_HOME); changed['result'] = 'passed'
        with self.assertRaises(Exception): TARGET.validate_prior_home(changed, SNAPSHOT)

    def test_wrong_prior_home_binding_error_scope_and_unrevoked_target_are_rejected(self):
        for variant in ('hash', 'path', 'error', 'proof', 'fallback', 'target'):
            prior, baseline = copy.deepcopy(PRIOR_HOME), copy.deepcopy(SNAPSHOT)
            if variant == 'hash': prior['evidence']['after-full.json']['sha256'] = '0' * 64
            if variant == 'path': prior['evidence']['after-full.json']['path'] = TARGET.AUTHORITY['recovery_snapshot']['path']
            if variant == 'error': prior['errors'] = []
            if variant == 'proof': prior['proof']['old_rows_sequences_private_unchanged'] = False
            if variant == 'fallback': prior['fallback_attempted'] = True
            if variant == 'target': next(row for row in baseline['database']['tables']['sessions'] if row['id'] == TARGET.PRIOR_HOME_B)['revoked_at'] = None
            with self.subTest(variant=variant), self.assertRaises(Exception): TARGET.validate_prior_home(prior, baseline)

    def test_complete_one_login_touch_capabilities_logout_delta(self):
        before, after, proof = sample()
        caps = {'SupportedCommands': ['DisplayMessage'], 'DeviceProfile': {'MaxStreamingBitrate': 0}}
        after['database']['tables']['sessions'][-1]['client_capabilities'] = caps
        result = TARGET.validate_delta(codec(), RESTRICTION, before, after, proof, SERVER, [caps])
        self.assertEqual((result['new_b_authentication'], result['new_devices'], result['new_session_audits']), (1, 1, 2))

    def test_old_rows_and_private_values_are_not_touched(self):
        for table, key, value in [('sessions', 'last_seen_at', '2000-01-01T00:00:00Z'), ('users', 'policy', {}),
                ('play_sessions', 'state', 'Expired'), ('user_item_data', 'played', 'wrong')]:
            with self.subTest(table=table):
                before, after, proof = sample()
                # Ensure every variant differs even when its initial state is Expired.
                after['database']['tables'][table][0][key] = {'unapproved': True}
                with self.assertRaises(Exception): TARGET.validate_delta(codec(), RESTRICTION, before, after, proof, SERVER, [{}])
        self.rejected_delta(lambda _, after, __: after.update(runtime_sha256='0' * 64))

    def test_new_play_userdata_or_second_login_is_rejected(self):
        for table in ('play_sessions', 'user_item_data', 'sessions', 'encoding_jobs'):
            with self.subTest(table=table):
                self.rejected_delta(lambda _, after, __: after['database']['tables'][table].append({'id': 'unapproved'}))

    def test_old_session_id_and_token_hash_cannot_be_adopted(self):
        for key in ('id', 'token_hash'):
            before, after, proof = sample()
            before['database']['tables']['sessions'][0][key] = SESSION if key == 'id' else '\\x' + FINGERPRINT
            with self.subTest(key=key), self.assertRaises(Exception): TARGET.owned_session(before, after, proof, SERVER)

    def test_account_kind_client_metadata_and_ack_timestamp_are_exact(self):
        for key, value in [('user_id', TARGET.A), ('kind', 'admin'), ('device_id', 'another'), ('client_version', 'guessed'),
                ('created_at', '2000-01-01T00:00:00Z'), ('expires_at', '2050-01-01T00:00:00Z'), ('revoked_at', None)]:
            with self.subTest(key=key):
                self.rejected_delta(lambda _, after, __: after['database']['tables']['sessions'][-1].update({key: value}))

    def test_device_timestamp_is_not_authentication_created_at(self):
        before, after, proof = sample()
        self.assertNotEqual(after['database']['tables']['devices'][-1]['created_at'], proof['created_at'])
        TARGET.validate_delta(codec(), RESTRICTION, before, after, proof, SERVER, [{}])
        for key, value in [('custom_name', 'not-owned'), ('last_user_id', TARGET.A), ('ip_address', '192.0.2.1'), ('revision', 2)]:
            with self.subTest(key=key):
                self.rejected_delta(lambda _, after, __: after['database']['tables']['devices'][-1].update({key: value}))

    def test_wrong_sequence_and_audit_actor_are_rejected(self):
        self.rejected_delta(lambda _, after, __: after['database']['sequences']['devices_id_seq'].update(last_value=999999))
        self.rejected_delta(lambda _, after, __: after['database']['tables']['activity_entries'][-1].update(actor_id=TARGET.A))

    def test_private_login_file_binds_source_process_and_original_token(self):
        before, after, proof = sample()
        record, child, private, _ = bindings(proof)
        token, row = TARGET.validate_private_session(private, record, INPUT_SHA, child, before, after)
        self.assertEqual((token, row['id']), (TOKEN, SESSION))
        for key, value in [('input_sha256', 'a' * 64), ('source_closure_sha256', 'a' * 64), ('token', 'a' * 43),
                ('node_process', {**child, 'pid': 500}), ('controller', {**record['controller'], 'start_ticks': '1'})]:
            changed = copy.deepcopy(private); changed[key] = value
            with self.subTest(key=key), self.assertRaises(Exception):
                TARGET.validate_private_session(changed, record, INPUT_SHA, child, before, after)

    def test_frame_or_physical_proof_cannot_be_partial(self):
        _, _, proof = sample()
        for key in ('frame_login_finished', 'physical_login_completed', 'request_metadata_matches'):
            changed = dict(proof, **{key: False})
            with self.subTest(key=key), self.assertRaises(Exception): TARGET.validate_login(changed, SERVER)

    def test_capability_normalization_preserves_real_stored_web_values(self):
        rows = [row for row in SNAPSHOT['database']['tables']['sessions'] if row['user_id'] == TARGET.B and row['kind'] == 'emby' and
            row['client_name'] == 'Emby Web' and row['client_capabilities']]
        self.assertTrue(rows, 'The fixed snapshot must include an actual B Web capability projection.')
        for row in rows:
            with self.subTest(session_id=row['id']):
                self.assertEqual(TARGET.canonical(TARGET.normalize_capabilities(TARGET.canonical(row['client_capabilities']))),
                    TARGET.canonical(row['client_capabilities']))

    def test_capability_unknown_push_and_top_level_omitempty(self):
        value = {'Unknown': 1, 'PushToken': 'private', 'PushTokenType': 'private', 'SupportsSync': False,
            'PlayableMediaTypes': [], 'IconUrl': '', 'DeviceProfile': {'Unknown': 2, 'MaxStreamingBitrate': 0,
                'SubtitleProfiles': [{'Format': 'vtt', 'Method': 'External', 'AllowChunkedResponse': False}], 'DirectPlayProfiles': []}}
        self.assertEqual(TARGET.normalize_capabilities(TARGET.canonical(value)), {'DeviceProfile': {
            'MaxStreamingBitrate': 0, 'SubtitleProfiles': [{'Format': 'vtt', 'Method': 'External', 'AllowChunkedResponse': False}], 'DirectPlayProfiles': []}})

    def test_capability_wrong_type_bounds_null_and_duplicate_key(self):
        for raw in (b'{"SupportsSync":1}', b'{"SupportedCommands":[null]}', b'{"SupportsSync":true,"SupportsSync":false}',
                b'{"DeviceProfile":{"MaxStreamingBitrate":1.0}}', b'{"Unknown":' + b'[' * 9 + b'0' + b']' * 9 + b'}',
                TARGET.canonical({'Unknown': 'x' * 2049}), b'{}' + b' ' * 65535):
            with self.subTest(length=len(raw)), self.assertRaises(Exception): TARGET.normalize_capabilities(raw)
        self.assertEqual(TARGET.normalize_capabilities(b'{"SupportsSync":null}'), {})

    def test_capability_actual_request_order_and_concurrent_ambiguity(self):
        _, _, proof = sample(); record, child, _, caps = bindings(proof)
        caps['entries'] = [entry(1, {'SupportsSync': True}), entry(2, {'SupportsMediaControl': True}, 30, 40, 41)]
        self.assertEqual(TARGET.capability_values(caps, proof, record, INPUT_SHA, child), [{'SupportsMediaControl': True}])
        caps['entries'][1].update(request_elapsed_ms=15)
        with self.assertRaises(Exception): TARGET.capability_values(caps, proof, record, INPUT_SHA, child)
        caps['entries'][1] = entry(2, {'SupportsSync': True}, 15, 40, 41)
        self.assertEqual(TARGET.capability_values(caps, proof, record, INPUT_SHA, child), [{'SupportsSync': True}, {'SupportsSync': True}])

    def test_capability_failed_foreign_repeated_and_hash_drift(self):
        _, _, proof = sample(); record, child, _, caps = bindings(proof)
        caps['entries'] = [entry(1, {'SupportsSync': True})]
        caps['entries'][0]['status'] = 400
        self.assertEqual(TARGET.capability_values(caps, proof, record, INPUT_SHA, child), [{}])
        for mutation in (lambda value: value.update(token_sha256='a' * 64),
                lambda value: value['entries'][0].update(token_matches_session=False),
                lambda value: value['entries'].append(copy.deepcopy(value['entries'][0])),
                lambda value: value['entries'][0].update(body_sha256='0' * 64)):
            changed = copy.deepcopy(caps); changed['entries'][0]['status'] = 204; mutation(changed)
            with self.assertRaises(Exception): TARGET.capability_values(changed, proof, record, INPUT_SHA, child)

    def test_query_capability_uses_actual_four_fields(self):
        row = entry(1, {}); row.update(kind='query', body_utf8='', body_sha256=TARGET.sha(b''),
            query={'playablemediatypes': 'Audio, Video,Audio', 'supportsmediacontrol': 'true', 'supportssync': 'false'})
        self.assertEqual(TARGET.capability_entry_value(row), {'PlayableMediaTypes': ['Audio', 'Video', 'Audio'], 'SupportsMediaControl': True})
        row['query']['arbitrary'] = 'not-permitted'
        with self.assertRaises(Exception): TARGET.capability_entry_value(row)

    def test_no_arbitrary_stored_capabilities(self):
        self.rejected_delta(lambda _, after, __: after['database']['tables']['sessions'][-1].update(client_capabilities={'Unexpected': True}))

    def test_source_closure_is_exact_and_has_no_dynamic_import(self):
        driver = TARGET.WORK / 'client-library-ui-tool-01/client-browser-library-home.mjs'
        record = {'marker': 'goby-client-library-home-sources-v1', 'files': {str(driver.parent / name): 'a' * 64 for name in TARGET.JS_NAMES}}
        self.assertEqual(len(TARGET.validate_source_manifest(record, driver)), 5)
        record['files'][str(driver.parent / 'unreviewed.mjs')] = 'b' * 64
        with self.assertRaises(Exception): TARGET.validate_source_manifest(record, driver)

    def test_browser_input_identity_and_acceptance_scope_are_exact(self):
        _, _, proof = sample(); record, child, _, _ = bindings(proof)
        report = browser_report(record, child, proof)
        TARGET.validate_node_report(report, record, INPUT_SHA, child)
        for key, value in [('mode', 'playback'), ('client_acceptance', True), ('permission_ui_acceptance', True),
                ('node_process', {**child, 'start_ticks': '999'})]:
            changed = copy.deepcopy(report); changed[key] = value
            with self.subTest(key=key), self.assertRaises(Exception): TARGET.validate_node_report(changed, record, INPUT_SHA, child)

    def test_reload_logout_and_browser_tree_must_all_close(self):
        _, _, proof = sample(); record, child, _, _ = bindings(proof)
        report = browser_report(record, child, proof)
        terminal = {'MainPID': '0', 'ExecMainStatus': '0', 'cgroup_empty': True}
        TARGET.validate_observation(report, proof, terminal)
        for mutate in (lambda value: value['reload']['action'].update(count=2),
                lambda value: value['closure'].update(websocket_closed=1), lambda value: value['closure'].update(sockets_remaining=1),
                lambda value: value['actor']['logout'].update(login_view_visible=False),
                lambda value: value['actor']['session_proof']['entries'][0].update(token_fingerprint='f' * 64)):
            changed = copy.deepcopy(report); mutate(changed)
            with self.assertRaises(Exception): TARGET.validate_observation(changed, proof, terminal)
        with self.assertRaises(Exception): TARGET.validate_observation(report, proof, {**terminal, 'cgroup_empty': False})

    def test_fallback_is_only_one_owned_logout_and_exact401(self):
        wire = Wire([204, 401])
        result = TARGET.fallback_http(TOKEN, False, wire.factory)
        self.assertEqual([row['status'] for row in result], [204, 401])
        self.assertEqual([(row[0], row[1]) for row in wire.calls], [('POST', '/emby/Sessions/Logout'), ('GET', '/emby/System/Info')])
        self.assertTrue(all(row[2] is None and row[3]['X-Emby-Token'] == TOKEN for row in wire.calls))
        wire = Wire([401]); TARGET.fallback_http(TOKEN, True, wire.factory)
        self.assertEqual(len(wire.calls), 1)
        with self.assertRaises(Exception): TARGET.fallback_http('invalid', False, wire.factory)

    def test_uncertain_logout_still_attempts_exact401_without_retry(self):
        wire = Wire([500, 401])
        result = TARGET.fallback_http(TOKEN, False, wire.factory)
        self.assertIn('failure_type', result[0]); self.assertEqual(result[1]['status'], 401)
        self.assertEqual(len(wire.calls), 2)

    def test_fallback_requires_closed_child_and_cannot_run_twice(self):
        run = TARGET.Run(types.SimpleNamespace())
        run.check = lambda: None
        for closed, attempted in ((False, False), (True, True)):
            run.closed, run.fallback_attempted, run.session = closed, attempted, {'proof': {}}
            with self.subTest(closed=closed), self.assertRaises(Exception): run.fallback(TOKEN, {'revoked_at': None})

    def test_readonly_helper_rejects_services_main_db_and_writes(self):
        run = TARGET.Run(types.SimpleNamespace()); run.op = codec()
        for command in (['/usr/bin/systemctl', 'stop', 'goby-client-m3e.service'],
                ['/usr/bin/systemctl', 'start', 'goby-foundation-test.service'], ['/usr/bin/psql', '-p', '5432'],
                ['/bin/sh', '-c', 'anything']):
            with self.subTest(command=command), self.assertRaises(TARGET.ObservationError): run.command(command)

    def test_check_only_never_creates_output_or_launches_browser(self):
        run = TARGET.Run(types.SimpleNamespace(check_only=True))
        run.load = lambda: None
        run.prepare = run.launch = run.save = denied
        result = run.execute()
        self.assertEqual(result['result'], 'checked')
        self.assertFalse(result['output_created'])
        self.assertEqual(result['http_requests'], 0)

    def test_terminal_log_descriptors_use_appended_bytes_without_rewriting(self):
        run = TARGET.Run(types.SimpleNamespace())
        run.closed = run.launched = True
        run.records = {name: {'path': str(TARGET.ROOT / name), 'sha256': TARGET.sha(b'')} for name in ('node.stdout', 'node.stderr')}
        payloads = {TARGET.ROOT / 'node.stdout': b'actual final worker output\n', TARGET.ROOT / 'node.stderr': b'actual final diagnostic\n'}
        with mock.patch.object(TARGET, 'protected', side_effect=lambda path, **_: payloads[path]) as read:
            run.refresh_worker_logs()
            self.assertEqual(read.call_count, 2)
        for name in ('node.stdout', 'node.stderr'):
            self.assertEqual(run.records[name]['sha256'], TARGET.sha(payloads[TARGET.ROOT / name]))
            self.assertNotEqual(run.records[name]['sha256'], TARGET.sha(b''))
        run.closed = False
        with self.assertRaises(Exception): run.refresh_worker_logs()


def read_pinned(path, digest, maximum):
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or info.st_nlink != 1 or info.st_mode & 0o022 or info.st_size > maximum:
        raise RuntimeError('A pure guard input is not an owned bounded regular file.')
    raw = path.read_bytes()
    if hashlib.sha256(raw).hexdigest() != digest: raise RuntimeError('A pure guard input digest differs.')
    return raw


def main():
    global TARGET, RESTRICTION, SNAPSHOT, RECOVERY, HISTORY, API, INSPECTION, RECOVERY_BASELINE, PRIOR_HOME
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--operator', type=Path, required=True)
    parser.add_argument('--operator-sha256', required=True)
    parser.add_argument('--restriction-operator', type=Path, required=True)
    parser.add_argument('--baseline-snapshot', type=Path, required=True)
    parser.add_argument('--report', type=Path)
    args = parser.parse_args()
    raw = read_pinned(args.operator, args.operator_sha256, 2 << 20)
    TARGET = types.ModuleType('home_controller_under_test'); TARGET.__file__ = str(args.operator)
    exec(compile(raw, str(args.operator), 'exec'), TARGET.__dict__)
    if args.baseline_snapshot != Path(TARGET.AUTHORITY['current_snapshot']['path']) or args.restriction_operator != TARGET.HELPERS['restriction'][0]:
        raise RuntimeError('Only the fixed latest inspection snapshot and accepted pure comparator are test inputs.')
    raw_restriction = read_pinned(args.restriction_operator, TARGET.HELPERS['restriction'][1], 2 << 20)
    RESTRICTION = types.ModuleType('accepted_pure_restriction'); RESTRICTION.__file__ = str(args.restriction_operator)
    exec(compile(raw_restriction, str(args.restriction_operator), 'exec'), RESTRICTION.__dict__)
    SNAPSHOT = TARGET.decode(read_pinned(args.baseline_snapshot, TARGET.AUTHORITY['current_snapshot']['sha256'], 32 << 20))
    RECOVERY, HISTORY, API, INSPECTION, RECOVERY_BASELINE, PRIOR_HOME = (TARGET.read_record(TARGET.AUTHORITY[key]) for key in
        ('recovery', 'inspection_snapshot', 'api_report', 'inspection', 'recovery_snapshot', 'prior_home'))
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(HomeGuards))
    report = {'marker': 'goby-client-library-home-guards-v1', 'operator_sha256': args.operator_sha256,
        'restriction_sha256': TARGET.HELPERS['restriction'][1], 'baseline_sha256': TARGET.AUTHORITY['current_snapshot']['sha256'],
        'tests_run': result.testsRun, 'failures': len(result.failures), 'errors': len(result.errors), 'skipped': len(result.skipped),
        'result': 'passed' if result.wasSuccessful() and not result.skipped else 'failed',
        'business_http_requests': 0, 'database_commands': 0, 'service_commands': 0, 'browser_started': False}
    if args.report:
        if not args.report.is_relative_to(TARGET.WORK) or '..' in args.report.parts or args.report.suffix != '.json':
            raise RuntimeError('The exclusive guard report must be in the private workspace.')
        fd = os.open(args.report, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'wb') as handle:
            handle.write(TARGET.canonical(report) + b'\n'); handle.flush(); os.fsync(handle.fileno())
    print(json.dumps(report, sort_keys=True))
    return 0 if report['result'] == 'passed' else 1


if __name__ == '__main__':
    sys.exit(main())
