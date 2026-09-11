#!/usr/bin/env python3
"""Observe one original-client permission restriction and conditional restoration.

Only the new B browser uses ordinary authentication. One separately owned native
administrator performs the two exact Movies-folder updates. Every stage and
control is an exclusive, atomically published record. Existing evidence and all
unrelated database, process, credential and media state remain protected.
"""

from __future__ import annotations

import argparse
from collections import Counter
import copy
import datetime as dt
import fcntl
import hashlib
import http.client
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import sys
import time
import types

sys.dont_write_bytecode = True
W = Path('/opt/goby-test/exec-work-m3e')
ROOT = W / 'client-library-permission-ui-v1'
BROWSER_ROOT = ROOT / 'browser'
UNIT = 'goby-client-library-permission-ui-v1.service'
CGROUP = '/system.slice/' + UNIT
MARKER = 'goby-client-library-permission-observation-v1'
INPUT_MARKER = 'goby-client-library-permission-input-v1'
MODE = 'b-home-permission-reload'
HOME_SOURCE = W / 'client-library-ui-py-tool-03/observe-client-library-home.py'
HOME_SHA = '8588fc889935d7efbe85bd99c2062dc8b19d1fd55ff05b4e4c574a66b2939bba'
AUTHORITY = {
    'home_report': {'path': str(W / 'client-library-ui-baseline-v3/report.json'), 'sha256': 'a8117846d80eeed8e714b8951103632acfc05105d8a14c362e99f0f9d9e62270'},
    'home_browser': {'path': str(W / 'client-library-ui-baseline-v3/browser/report.json'), 'sha256': 'a5e4cd3a16c625c85b459c20c2349375a22bf63d889d7ac2e89f0e9237a91e28'},
    'current_snapshot': {'path': str(W / 'client-library-ui-baseline-v3/after-full.json'), 'sha256': '8095db0dd2e96c7f3a8e194b3f66d71c7366e756b12f1d1f549a19c336acb29a'},
}
JS_NAMES = {'client-browser-library-permission.mjs', 'client-browser-library-home.mjs', 'client-browser-cross-user.mjs',
    'client-browser-special-features-fixture.mjs', 'client-browser-goby-fixture.mjs', 'client-browser-session-proof.mjs'}
MOVIES = 'a9993591e72f0f2e7babcbf8b9c50790'
H = None


class PermissionError(Exception):
    """A sanitized, bounded ownership, restoration or observation failure."""


def require(value, message):
    if not value: raise PermissionError(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def validate_latest(report, browser, baseline, state):
    tables = baseline['database']['tables']
    counts = {'sessions': 71, 'devices': 61, 'activity_entries': 157, 'play_sessions': 26, 'user_item_data': 7,
        'libraries': 4, 'items': 22, 'client_playback_references': 0, 'encoding_jobs': 0}
    require(baseline.get('schema') == 27 and len(tables) == 35 and all(len(tables.get(key, [])) == value for key, value in counts.items()) and
        sum(row.get('user_id') in (H.A, H.B) for row in tables['sessions']) == 62 and
        {row['id'] for row in tables['libraries']} == H.LIBRARIES, 'The latest complete Home v3 baseline differs.')
    H.validate_recovered_credentials(baseline)
    require(state.get('schema') == 27 and state.get('phase') == 'ready' and state.get('stage') == 'complete' and
        state.get('process') == H.PROCESS and state.get('binary_sha256') == H.BINARY_SHA and state.get('viewer_id') == H.B,
        'The current candidate is not the accepted source32 ordinary fixture.')
    require(report.get('marker') == 'goby-client-library-home-observation-v1' and report.get('result') == 'passed' and
        report.get('phase') == 'complete' and report.get('outcome') == 'baseline_observation' and report.get('errors') == [] and
        report.get('worker_closed') is True and report.get('fallback_attempted') is False and
        report.get('client_acceptance') is False and report.get('permission_ui_acceptance') is False and
        report.get('candidate', {}).get('process') == H.PROCESS and report['candidate'].get('binary_sha256') == H.BINARY_SHA and
        report['candidate'].get('state_sha256') == H.STATE_SHA and
        report.get('evidence', {}).get('after-full.json') == AUTHORITY['current_snapshot'] and
        report['evidence'].get('browser-report.json') == AUTHORITY['home_browser'], 'Home v3 lacks its exact successful prerequisite evidence.')
    proof = browser.get('login_proof', {})
    H.validate_login(proof, state['server_id'])
    require(browser.get('result') == 'passed' and browser.get('outcome') == 'baseline_observation' and
        H.ui_logout_proven(browser, proof) and report.get('proof') == {
            'old_rows_sequences_private_unchanged': True, 'users_and_policies_unchanged': True,
            'new_b_authentication': 1, 'new_devices': 1, 'new_session_audits': 2, 'new_play_userdata_references_encodings': 0,
            'session_id': proof['session_id'], 'token_sha256': proof['token_sha256'], 'observed_capabilities_bound': True},
        'Home v3 does not bind its real login, complete state delta and exact logout.')
    old = [row for row in tables['sessions'] if row['id'] == proof['session_id']]
    require(len(old) == 1 and old[0]['user_id'] == H.B and old[0]['kind'] == 'emby' and
        old[0]['token_hash'] == '\\x' + proof['token_sha256'] and old[0]['revoked_at'] is not None,
        'The prior successful Home credential is not revoked in the current baseline.')


def publication_state(final, pending):
    """Return missing/publishing/ready for this fixed IPC publication protocol."""
    if final is None:
        if pending is None: return 'missing'
        require(stat.S_ISREG(pending.st_mode) and stat.S_IMODE(pending.st_mode) == 0o600 and pending.st_uid == pending.st_gid == 0 and
            pending.st_nlink == 1, 'An unpublished IPC pending record has another owner or link.')
        return 'publishing'
    require(stat.S_ISREG(final.st_mode) and stat.S_IMODE(final.st_mode) == 0o600 and final.st_uid == final.st_gid == 0,
        'An IPC final record is not a private root-owned regular file.')
    if final.st_nlink == 1 and pending is None: return 'ready'
    require(final.st_nlink == 2 and pending is not None and stat.S_ISREG(pending.st_mode) and
        stat.S_IMODE(pending.st_mode) == 0o600 and pending.st_uid == pending.st_gid == 0 and pending.st_nlink == 2 and
        (final.st_dev, final.st_ino) == (pending.st_dev, pending.st_ino), 'An IPC record has an unknown link or pending owner.')
    return 'publishing'


def expected_libraries(input_record, name):
    return [row for row in input_record['expected_libraries'] if name != 'restricted' or row['id'] != MOVIES]


def elapsed(value):
    require(type(value) is int or isinstance(value, H.Decimal), 'An observed elapsed time is not numeric.')
    require(0 <= value <= 660000, 'An observed elapsed time exceeds this worker scope.')
    return value


def epoch_ms(value):
    instant = H.instant(value).astimezone(dt.timezone.utc)
    delta = instant - dt.datetime(1970, 1, 1, tzinfo=dt.timezone.utc)
    return delta.days * 86400000 + delta.seconds * 1000 + delta.microseconds // 1000


def validate_views(views, libraries, token_sha, stage_name=None, invoked_at=0):
    require(isinstance(views, dict) and views.get('result') == 'passed' and type(views.get('frame_count')) is int and
        1 <= views['frame_count'] <= 4 and views.get('physical_count') == views['frame_count'] and
        isinstance(views.get('pairs'), list) and len(views['pairs']) == views['frame_count'], 'A stage lacks complete real Views observations.')
    expected = {row['id']: row['name'] for row in libraries}
    if stage_name is not None: require(views.get('stage') == stage_name, 'The Views collection belongs to another browser stage.')
    for pair in views['pairs']:
        frame, physical = pair.get('frame', {}), pair.get('physical', {})
        require(pair.get('complete') is True and pair.get('unambiguous') is True and frame.get('kind') == physical.get('kind') == 'views' and
            frame.get('finished') is True and frame.get('failed') is False and frame.get('from_service_worker') is False and
            frame.get('status') == 200 and frame.get('content_type') == 'application/json' and
            physical.get('completed') is True and physical.get('terminal_status') == 200 and
            frame.get('request_sha256') == physical.get('request_sha256') and H.HASH.fullmatch(frame.get('request_sha256', '')) and
            frame.get('token_sha256') == physical.get('token_sha256') == token_sha,
            'A Views response lost its physical/frame/token ownership or terminal success.')
        if stage_name is not None:
            require(frame.get('stage') == physical.get('stage') == stage_name, 'A prior-stage Views pair cannot be reused after reload.')
            for event in (frame, physical):
                times = [elapsed(event.get(key)) for key in ('request_elapsed_ms', 'response_elapsed_ms', 'finished_elapsed_ms')]
                require(invoked_at <= times[0] <= times[1] <= times[2], 'A Views pair predates its explicit reload or has invalid completion order.')
        projection = physical.get('projection', {})
        items = projection.get('Items', [])
        require(projection.get('matches_expected') is True and projection.get('TotalRecordCount') == len(expected) and
            isinstance(items, list) and len(items) == len(expected) and
            {row.get('Id'): row.get('Name') for row in items} == expected and all(row.get('name_matches') is True for row in items),
            'Actual Views membership is not the exact current authorized libraries.')


def validate_dom(dom, libraries, restricted, all_libraries):
    require(isinstance(dom, dict) and dom.get('passed') is True and dom.get('media_inactive') is True and
        dom.get('location', {}).get('same_origin') is True and dom['location'].get('supported_path') is True and
        dom['location'].get('route') == 'home', 'A stage is not an inactive real Home observation.')
    rows = dom.get('libraries', [])
    require(isinstance(rows, list) and len(rows) == len(libraries) and
        {row.get('id'): row.get('name') for row in rows} == {row['id']: row['name'] for row in libraries} and
        all(row.get('passed') is True and row.get('visible_card_count') == 1 and row.get('card_id_present') is True and
            row.get('card_id_matches') is True and row.get('visible_title_count', 0) >= 1 for row in rows),
        'The real visible Home cards do not match their exact library identities.')
    if restricted:
        excluded = dom.get('excluded_libraries')
        movie = next(row for row in all_libraries if row['id'] == MOVIES)
        require(isinstance(excluded, list) and len(excluded) == 1 and excluded[0].get('id') == MOVIES and
            excluded[0].get('name') == movie['name'] and excluded[0].get('absent') is True and
            excluded[0].get('visible_card_count') == 0 and excluded[0].get('matching_id_card_count') == 0,
            'The original Movies card is not explicitly absent while restricted.')


def validate_spontaneous(value, write_completed_at, name=None, input_record=None, token_sha=None):
    require(isinstance(value, dict) and set(value) == {'window', 'earlier', 'dom', 'views', 'outcome'}, 'The passive observation shape differs.')
    window = value['window']
    require(isinstance(window, dict) and set(window) == {'write_completed_at', 'start_elapsed_ms', 'end_elapsed_ms', 'duration_ms',
        'completed_elapsed_ms', 'completed', 'ui_actions'} and window['write_completed_at'] == write_completed_at and
        window['duration_ms'] == 10000 and window['completed'] is True and window['ui_actions'] == 0, 'The passive window is not bound to the actual native ACK and ten action-free seconds.')
    H.instant(write_completed_at)
    start, end, completed = (elapsed(window[key]) for key in ('start_elapsed_ms', 'end_elapsed_ms', 'completed_elapsed_ms'))
    require(end - start == 10000 and completed >= end, 'The declared passive observation did not span its full ten-second window.')
    earlier = value['earlier']
    require(isinstance(earlier, dict) and set(earlier) == {'dom', 'views'} and isinstance(earlier['dom'], list) and
        isinstance(value['dom'], list) and len(value['dom']) + len(earlier['dom']) <= 205 and
        isinstance(value['views'], dict) and isinstance(earlier['views'], dict), 'The passive observation exceeds its fixed sample bound.')
    for post_ack, samples in ((False, earlier['dom']), (True, value['dom'])):
        for sample in samples:
            require(isinstance(sample, dict) and set(sample) == {'started_elapsed_ms', 'elapsed_ms', 'observation'} and
                isinstance(sample['observation'], dict), 'A passive DOM sample is malformed.')
            began, ended = elapsed(sample['started_elapsed_ms']), elapsed(sample['elapsed_ms'])
            require(began <= ended <= completed and (start <= began and ended <= end if post_ack else began < start),
                'A DOM sample was assigned to an unobserved portion of the acknowledgement window.')
    expected = ('not_observed_within_window' if value['views'].get('result') == 'not_observed' else
        'observed_correct_membership' if value['views'].get('result') == 'passed' and value['dom'] and
            value['dom'][-1]['observation'].get('passed') is True else 'observed_incomplete_or_stale_membership')
    require(value['outcome'] == expected, 'An absent or incomplete passive response was promoted to observed automatic membership.')
    if name is not None and value['views'].get('result') == 'passed':
        libraries = expected_libraries(input_record, name)
        validate_views(value['views'], libraries, token_sha, name + '_passive', start)
        require(all(pair[carrier]['finished_elapsed_ms'] <= end for pair in value['views']['pairs'] for carrier in ('frame', 'physical')),
            'A passive response completed outside its declared acknowledgement window.')
        if value['outcome'] == 'observed_correct_membership':
            validate_dom(value['dom'][-1]['observation'], libraries, name == 'restricted', input_record['expected_libraries'])


def validate_stage(stage, name, input_record, input_sha, child, previous_control_sha, session_descriptor, token_sha, write_completed_at=None):
    keys = {'marker', 'version', 'input_sha256', 'source_closure_sha256', 'controller', 'node_process', 'name',
        'token_sha256', 'session_private', 'previous_control_sha256', 'observation'}
    require(isinstance(stage, dict) and set(stage) == keys and stage.get('marker') == 'goby-client-library-permission-stage-v1' and
        stage.get('version') == 1 and stage.get('input_sha256') == input_sha and
        stage.get('source_closure_sha256') == sha(H.canonical(input_record['source_closure'])) and
        stage.get('controller') == input_record['controller'] and stage.get('node_process') == child and stage.get('name') == name and
        stage.get('token_sha256') == token_sha and stage.get('session_private') == session_descriptor and
        stage.get('previous_control_sha256') == previous_control_sha, 'A browser stage belongs to another source, process, credential or predecessor.')
    observation, libraries = stage['observation'], expected_libraries(input_record, name)
    if name == 'baseline':
        require(set(observation) == {'dom', 'views'} and previous_control_sha is None, 'The baseline stage has an unexpected action or predecessor.')
        view = observation
    else:
        require(name in ('restricted', 'restored') and set(observation) == {'spontaneous', 'reload'}, 'A changed-policy stage has another shape.')
        validate_spontaneous(observation['spontaneous'], write_completed_at, name, input_record, token_sha)
        view = observation['reload']
        require(set(view) == {'action', 'dom', 'views'} and view['action'].get('kind') == 'page.reload' and
            view['action'].get('count') == 1 and view['action'].get('completed') is True and view['action'].get('status') == 200 and
            view['action'].get('control_sha256') == previous_control_sha and
            observation['spontaneous']['window']['completed_elapsed_ms'] <= elapsed(view['action'].get('invoked_elapsed_ms')) <=
            elapsed(view['action'].get('completed_elapsed_ms')),
            'A changed-policy phase did not complete exactly one ordinary reload.')
    validate_views(view['views'], libraries, token_sha, 'initial' if name == 'baseline' else name + '_reload',
        0 if name == 'baseline' else view['action']['invoked_elapsed_ms'])
    validate_dom(view['dom'], libraries, name == 'restricted', input_record['expected_libraries'])


def owned_update(snapshot, admin_session_id, admin_user_id, revision, fields):
    rows = [row for row in snapshot['database']['tables']['activity_entries'] if row.get('action') == 'user.updated' and
        row.get('resource_kind') == 'user' and row.get('resource_id') == H.B and row.get('revision') == revision]
    return len(rows) == 1 and all(rows[0].get(key) == value for key, value in {
        'source': 'native', 'actor_kind': 'user', 'actor_id': admin_user_id, 'actor_credential_id': admin_session_id,
        'severity': 'Info', 'affected_count': 1, 'state': '', 'changed_fields': fields}.items())


def validate_full_delta(op, restriction, before, authenticated, after, admin, browser_proof, original, restricted, capability_values):
    additions = restriction.unchanged_rows(op, before, after, H.B)
    require(all(len(rows) == {'sessions': 2, 'devices': 1, 'activity_entries': 6}.get(name, 0) for name, rows in additions.items()),
        'Permission observation changed state outside two sessions, one device and six owned audits.')
    old_b = next(row for row in before['database']['tables']['users'] if row['id'] == H.B)
    new_b = next(row for row in after['database']['tables']['users'] if row['id'] == H.B)
    expected_b = copy.deepcopy(old_b)
    expected_b.update(management_revision=old_b['management_revision'] + 2, updated_at=new_b['updated_at'])
    require(op.equal_json(new_b, expected_b) and old_b['management_revision'] == 3 and original['Revision'] == '3' and
        restriction.raw_restored_policy(old_b, original) == old_b['policy'], 'B did not restore its exact eight-key raw Policy and declared revision-only delta.')
    start, end = (H.instant(snapshot['database']['metadata']['captured_at']) for snapshot in (before, after))
    require(start <= H.instant(new_b['updated_at']) <= end, 'The final B update timestamp lies outside the observation.')
    ordinary = H.owned_session(before, after, browser_proof, browser_proof['server_id'])
    initial_auth = {row['id']: row for row in authenticated['database']['tables']['sessions']}
    native = next((row for row in additions['sessions'] if row['id'] == admin['id']), None)
    require(native is not None and ordinary['id'] != native['id'] and native['kind'] == 'admin' and native['user_id'] == admin['user_id'] and
        native['token_hash'] == admin['token_hash'] and native['client_capabilities'] == {} and native['device_registry_id'] is None,
        'The exact new native administrator authentication changed identity or capabilities.')
    for row in (native, ordinary):
        initial = initial_auth.get(row['id'])
        require(initial is not None and all(op.equal_json(value, row[key]) for key, value in initial.items()
            if key not in ('last_seen_at', 'revoked_at', 'client_capabilities')) and row['revoked_at'] is not None and
            H.instant(initial['last_seen_at']) <= H.instant(row['last_seen_at']) <= end and
            start <= H.instant(row['revoked_at']) <= end, 'A new authentication changed outside its permitted capabilities, Touch or logout.')
    require(any(op.equal_json(ordinary['client_capabilities'], value) for value in capability_values), 'Stored B capabilities lack an exact successful observed request.')
    device = additions['devices'][0]
    initial_device = next((row for row in authenticated['database']['tables']['devices'] if row['id'] == device['id']), None)
    require(initial_device is not None and device['id'] == ordinary['device_registry_id'] and
        device['reported_device_id'] == browser_proof['device_id'] and device['last_user_id'] == H.B and
        device['reported_name'] == browser_proof['device_name'] and device['app_name'] == browser_proof['client_name'] and
        device['app_version'] == browser_proof['client_version'] and device['ip_address'] == '127.0.0.1' and device['custom_name'] is None and
        device['deleted_at'] is None and device['revision'] == 1 and
        all(old['reported_device_id'] != device['reported_device_id'] for old in before['database']['tables']['devices']) and
        all(op.equal_json(value, device[key]) for key, value in initial_device.items() if key != 'last_seen_at') and
        start <= H.instant(initial_device['created_at']) <= H.instant(browser_proof['created_at']) and
        H.instant(initial_device['created_at']) <= H.instant(initial_device['last_seen_at']) <= H.instant(authenticated['database']['metadata']['captured_at']) and
        H.instant(initial_device['last_seen_at']) <= H.instant(device['last_seen_at']) <= end,
        'The new B device changed outside its initial wire identity and Touch timestamp.')
    fields = restriction.changed_fields(original, restricted)
    expected_events = []
    for row, source in ((ordinary, 'emby'), (native, 'native')):
        expected_events += [(action, source, 'user', row['user_id'], row['id'], 'session', row['id'], 0, ())
                           for action in ('session.login', 'session.revoked')]
    expected_events += [('user.updated', 'native', 'user', native['user_id'], native['id'], 'user', H.B, number, tuple(fields)) for number in (4, 5)]
    keys = ('action', 'source', 'actor_kind', 'actor_id', 'actor_credential_id', 'resource_kind', 'resource_id', 'revision')
    require(Counter(tuple(row[key] for key in keys) + (tuple(row['changed_fields']),) for row in additions['activity_entries']) == Counter(expected_events) and
        all(row['severity'] == 'Info' and row['affected_count'] == 1 and row['state'] == '' and
            start <= H.instant(row['created_at']) <= end for row in additions['activity_entries']), 'The six audit events do not identify the exact two logins, logouts and B updates.')
    left, right = before['database']['sequences'], after['database']['sequences']
    require(set(left) == set(right), 'The sequence inventory changed.')
    for name, value in left.items():
        if name in ('devices_id_seq', 'activity_entries_id_seq'):
            table, count = ('devices', 1) if name == 'devices_id_seq' else ('activity_entries', 6)
            first = value['last_value'] + int(value['is_called'])
            require(right[name] == {'last_value': first + count - 1, 'is_called': True} and
                sorted(row['id'] for row in additions[table]) == list(range(first, first + count)), 'An owned sequence increment lacks its exact new rows.')
        else: require(op.equal_json(value, right[name]), 'An unrelated sequence changed.')
    return {'new_sessions': 2, 'new_devices': 1, 'new_session_audits': 4, 'new_user_update_audits': 2,
        'b_revision_before': '3', 'b_revision_after': '5', 'raw_policy_restored_exactly': True,
        'old_rows_sequences_private_preserved_except_declared_b_fields': True, 'play_userdata_references_encodings_unchanged': True,
        'browser_session_id': ordinary['id'], 'browser_token_sha256': browser_proof['token_sha256'], 'administrator_session_id': native['id']}


class Run:
    def __init__(self, args):
        self.args, self.op, self.restriction, self.ext, self.reader = args, None, None, None, None
        self.lock = self.root_fd = None
        self.state = self.before = self.authenticated = self.after = self.input = self.child = self.terminal = None
        self.input_sha = self.invocation = None
        self.sources, self.artifacts, self.records, self.errors = {}, {}, {}, []
        self.stages, self.controls, self.control_attempts, self.pending_seen = {}, {}, set(), {}
        self.last_stage = None
        self.sent, self.responses = set(), {}
        self.login_sent, self.request_count = False, 0
        self.launched = self.worker_closed = self.aborted = self.b_fallback = False
        self.admin_token = self.admin_csrf = self.admin_sha = self.admin = None
        self.admin_proven = self.admin_closed = False
        self.b_private = self.b_descriptor = self.b_proof = None
        self.b_token = self.browser_report = self.capability_document = None
        self.original = self.restricted = self.restored = self.restore_body = None
        self.restore_reservation = self.cleanup_reservation = None
        self.restrict_sent = self.restore_sent = self.restore_authorized = False
        self.restrict_status = self.restore_status = self.restrict_ack_time = self.restore_ack_time = None
        self.restoration, self.reconcile_count = 'not_required', 0
        self.restore_deadline = self.cleanup_deadline = self.worker_started = None
        self.work_deadline = time.monotonic() + 420
        self.phase = 'preflight'
        self.delta = None

    def error(self, stage, error):
        row = {'stage': stage, 'failure_type': type(error).__name__}
        if type(error) is PermissionError: row['reason'] = str(error)
        self.errors.append(row)

    def save(self, name, value, soft=False):
        try:
            require(self.root_fd is not None and re.fullmatch(r'[a-z][a-z0-9-]*\.(json|stdout|stderr)', name), 'A record escaped the new controller root.')
            raw = value if isinstance(value, bytes) else H.canonical(value) + b'\n'
            require(len(raw) <= 32 << 20, 'A private controller record exceeds its byte bound.')
            fd = os.open(name, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW, 0o600, dir_fd=self.root_fd)
            with os.fdopen(fd, 'wb') as handle: handle.write(raw); handle.flush(); os.fsync(handle.fileno())
            os.fsync(self.root_fd)
            record = {'path': str(ROOT / name), 'sha256': sha(raw)}
            self.records[name] = record
            return record
        except Exception as error:
            if not soft: raise
            self.error('journal_' + name, error)
            return None

    def read_ipc(self, path):
        require(path.parent in (ROOT, BROWSER_ROOT) and path.name in {'stage-baseline.json', 'stage-restricted.json', 'stage-restored.json',
            'control-restricted.json', 'control-restored.json', 'control-close.json', 'abort.json'}, 'An IPC read left the fixed record set.')
        def observe(selected):
            try: return selected.lstat()
            except FileNotFoundError: return None
        pending = path.with_name(path.name + '.pending')
        first = observe(path)
        staged = observe(pending)
        second = observe(path)
        signature = lambda value: None if value is None else H.identity(value)
        # Atomic link/unlink can cross two lstat observations. A changing final
        # sample is publication in progress, not permission to parse a torn pair.
        state = 'publishing' if signature(first) != signature(second) else publication_state(second, staged)
        if state == 'missing': return None
        if state == 'publishing':
            self.pending_seen.setdefault(str(path), time.monotonic())
            require(time.monotonic() - self.pending_seen[str(path)] <= 5, 'An IPC publication did not finish within its narrow link window.')
            return None
        raw = H.protected(path, limit=512 << 10)
        return H.decode(raw), {'path': str(path), 'sha256': sha(raw)}

    def publish_control(self, name):
        require(name in ('restricted', 'restored', 'close') and name not in self.control_attempts and self.child is not None,
            'A control was repeated or lacks the exact worker identity.')
        if name == 'restricted':
            require(self.last_stage == 'baseline' and self.restrict_ack_time is not None and self.restoration == 'pending' and not self.aborted,
                'Restriction control has no acknowledged write and baseline predecessor.')
            revision, completed, restoration = self.current_managed['Revision'], self.restrict_ack_time, 'pending'
        elif name == 'restored':
            require(self.last_stage == 'restricted' and self.restore_ack_time is not None and self.restoration == 'confirmed' and not self.aborted,
                'Restoration control has no acknowledged write and restricted predecessor.')
            revision, completed, restoration = self.restored['Revision'], self.restore_ack_time, 'confirmed'
        else:
            require(self.restoration in ('confirmed', 'not_required'), 'A close control cannot invent completed restoration.')
            revision = self.restored['Revision'] if self.restoration == 'confirmed' else None
            completed, restoration = self.restore_ack_time, self.restoration
        document = {'marker': 'goby-client-library-permission-control-v1', 'version': 1, 'input_sha256': self.input_sha,
            'source_closure_sha256': sha(H.canonical(self.sources)), 'controller': self.input['controller'], 'node_process': self.child,
            'name': name, 'previous_stage_sha256': self.stages[self.last_stage]['artifact']['sha256'] if self.last_stage else None,
            'revision': revision, 'write_completed_at': completed, 'expected_libraries': expected_libraries(self.input, name), 'restoration': restoration}
        self.control_attempts.add(name)
        filename = 'control-' + name + '.json'
        pending, raw = filename + '.pending', H.canonical(document) + b'\n'
        fd = os.open(pending, os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_WRONLY, 0o600, dir_fd=self.root_fd)
        with os.fdopen(fd, 'wb') as handle:
            handle.write(raw); handle.flush(); os.fsync(handle.fileno()); owned = os.fstat(handle.fileno())
        os.link(pending, filename, src_dir_fd=self.root_fd, dst_dir_fd=self.root_fd, follow_symlinks=False)
        os.fsync(self.root_fd)
        final, staged = (os.stat(value, dir_fd=self.root_fd, follow_symlinks=False) for value in (filename, pending))
        require(publication_state(final, staged) == 'publishing' and (final.st_dev, final.st_ino) == (owned.st_dev, owned.st_ino),
            'An IPC publication lost its exact newly created inode.')
        os.unlink(pending, dir_fd=self.root_fd); os.fsync(self.root_fd)
        self.controls[name] = {'document': document, 'artifact': {'path': str(ROOT / filename), 'sha256': sha(raw)}}
        self.records[filename] = self.controls[name]['artifact']

    def notice_abort(self):
        found = self.read_ipc(BROWSER_ROOT / 'abort.json')
        if found is None: return self.aborted
        document, artifact = found
        self.aborted = True
        require(set(document) == {'marker', 'version', 'input_sha256', 'source_closure_sha256', 'controller', 'node_process', 'stage', 'failure'} and
            document['marker'] == 'goby-client-library-permission-abort-v1' and document['version'] == 1 and
            document['input_sha256'] == self.input_sha and document['source_closure_sha256'] == sha(H.canonical(self.sources)) and
            document['controller'] == self.input['controller'] and document['node_process'] == self.child and
            isinstance(document['stage'], str) and isinstance(document['failure'], str) and
            re.fullmatch('[a-z0-9_]{1,96}', document['failure']), 'A browser abort record has another source, process or scope.')
        self.records['browser-abort.json'] = artifact
        return True

    def check(self, recovery=False, cleanup=False):
        deadline = self.cleanup_deadline if cleanup else self.restore_deadline if recovery else self.work_deadline
        require(deadline is not None and time.monotonic() < deadline, 'The independent bounded operation window expired.')
        for path, digest in self.sources.items(): H.protected(Path(path), digest, modes=(0o600, 0o644), limit=2 << 20)
        for path, digest in self.artifacts.items(): H.protected(Path(path), digest, modes=(0o600, 0o644), limit=32 << 20)
        H.protected(H.STATE, H.STATE_SHA)
        H.protected(self.args.node, self.args.node_sha256, modes=(0o755,), limit=256 << 20, workspace=False)
        self.op.verify_fixture_directories(self.state)
        require(self.op.verify_service(self.state) == H.PROCESS, 'The exact candidate process changed.')
        if self.root_fd is not None:
            named, opened = ROOT.lstat(), os.fstat(self.root_fd)
            require((named.st_dev, named.st_ino) == (opened.st_dev, opened.st_ino) and stat.S_ISDIR(named.st_mode) and
                named.st_uid == named.st_gid == 0 and stat.S_IMODE(named.st_mode) == 0o700, 'The new controller root changed identity.')

    def snapshot(self, name, soft=False):
        value = self.op.preservation_snapshot(self.state, 27)
        self.save(name, value, soft=soft)
        return value

    def load(self):
        require(sys.platform == 'linux' and os.getuid() == os.geteuid() == os.getgid() == os.getegid() == 0 and os.environ.get('SSH_CONNECTION'),
            'Use the authorized root SSH environment only.')
        os.umask(0o077)
        manifest = H.decode(H.protected(self.args.source_closure, self.args.source_closure_sha256))
        require(set(manifest) == {'marker', 'files'} and manifest['marker'] == 'goby-client-library-permission-sources-v1' and
            isinstance(manifest['files'], dict) and set(manifest['files']) == {str(self.args.driver.parent / name) for name in JS_NAMES} and
            self.args.driver.name == 'client-browser-library-permission.mjs', 'The browser closure is not the exact six reviewed sources.')
        self.sources = {**manifest['files'], str(Path(__file__).absolute()): self.args.script_sha256, str(HOME_SOURCE): HOME_SHA}
        for name, (path, digest) in H.HELPERS.items():
            self.sources[str(path)] = digest
            setattr(self, {'core': 'op', 'restriction': 'restriction', 'extension': 'ext'}[name], H.load_source(path, digest, 'permission_' + name))
        require(len(self.sources) == 11 and all(H.HASH.fullmatch(value) for value in self.sources.values()), 'The full source closure collides or lacks a digest.')
        self.reader = H.Run(types.SimpleNamespace()); self.reader.op = self.op
        self.op.command = self.reader.command
        self.op.host_inputs(initial=False)
        self.lock = os.open(self.op.LOCK, os.O_RDONLY | os.O_NOFOLLOW)
        require(self.op.identity(os.fstat(self.lock)) == self.op.identity(self.op.regular(self.op.LOCK)), 'The existing fixture lock changed.')
        fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        require(not os.path.lexists(ROOT), 'The new permission root exists; no retry, adoption or overwrite is allowed.')
        status = self.properties()
        require(status.get('LoadState') == 'not-found' and status.get('MainPID') == '0' and not Path('/sys/fs/cgroup' + CGROUP).exists(),
            'The dedicated Node worker name or cgroup is already occupied.')
        self.state = H.decode(H.protected(H.STATE, H.STATE_SHA))
        self.artifacts = {row['path']: row['sha256'] for row in AUTHORITY.values()}
        self.artifacts.update({str(self.args.source_closure): self.args.source_closure_sha256, str(H.BROWSER): H.BROWSER_SHA})
        self.artifacts.update({str(path): digest for path, digest in H.PROFILE_PINS.values()})
        home, browser, baseline = (H.read_record(AUTHORITY[key]) for key in ('home_report', 'home_browser', 'current_snapshot'))
        validate_latest(home, browser, baseline, self.state)
        for name in ('input.json', 'worker-terminal.json'):
            artifact = home['evidence'][name]
            self.artifacts[artifact['path']] = artifact['sha256']
        old_input = H.read_record(home['evidence']['input.json'])
        H.validate_node_report(browser, old_input, home['input_sha256'], home['browser_process'])
        H.validate_observation(browser, browser['login_proof'], H.read_record(home['evidence']['worker-terminal.json']))
        product = self.state['upgrade']['schema_artifacts']['product_verification']
        require(self.op.equal_json(self.op.verify_product_upgrade(Path(self.state['upgrade']['source']), H.BINARY_SHA, H.SOURCE,
            H.MANIFEST_SHA, Path(product['report_path']), product['report_sha256'], 27), product), 'The accepted source32 build/full-test chain changed.')
        H.protected(self.args.node, self.args.node_sha256, modes=(0o755,), limit=256 << 20, workspace=False)
        self.check()
        self.media = self.ext.media_witness(self.op, self.state)
        self.before = self.op.preservation_snapshot(self.state, 27)
        self.restriction.compare_fixed_snapshot(self.op, baseline, self.before)
        self.restriction.quiescent(self.before)
        self.account = H.decode(H.protected(H.BROWSER, H.BROWSER_SHA))['admin']
        self.old_b = next(row for row in self.before['database']['tables']['users'] if row['id'] == H.B)
        administrator = next(row for row in self.before['database']['tables']['users'] if row['id'] == self.state['admin_id'])
        require(administrator['name'] == self.account['username'] and administrator['is_administrator'] is True and administrator['is_disabled'] is False,
            'The separately selected native administrator is not bound to the current database.')

    def prepare(self):
        ROOT.mkdir(mode=0o700)
        self.root_fd = os.open(ROOT, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        parent = os.open(W, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try: os.fsync(parent)
        finally: os.close(parent)
        self.save('intent.json', {'marker': MARKER, 'version': 1, 'root': str(ROOT), 'unit': UNIT, 'authority': AUTHORITY,
            'sources': self.sources, 'scope': 'One new B browser and one new native administrator; original Movies restriction and conditional restoration only.'})
        before_descriptor = self.save('before-full.json', self.before)
        self.save('media-before.json', self.media)
        self.input = {'marker': INPUT_MARKER, 'version': 1, 'mode': MODE, 'root': str(ROOT), 'output': str(BROWSER_ROOT),
            'actor': {'slot': 'B', 'user_id': H.B, 'credentials': {'path': str(H.BROWSER), 'sha256': H.BROWSER_SHA}, 'account_key': 'viewer'},
            'candidate': {'binary_sha256': H.BINARY_SHA, 'process': H.PROCESS, 'runtime_sha256': self.state['runtime_sha256'],
                'state_sha256': H.STATE_SHA, 'server_id': self.state['server_id'], 'base_url': H.BASE_URL, 'direct_url': H.DIRECT_URL,
                'source': str(H.SOURCE), 'source_manifest_sha256': H.MANIFEST_SHA},
            'fixture': {key: {'path': str(path), 'sha256': digest} for key, (path, digest) in H.PROFILE_PINS.items()},
            'expected_libraries': sorted([{'id': row['id'], 'name': row['name']} for row in self.before['database']['tables']['libraries']], key=lambda row: row['id']),
            'source_closure': self.sources, 'authority': {**AUTHORITY, 'before_snapshot': before_descriptor},
            'controller': {**H.proc_identity(os.getpid()), 'unit': UNIT}}
        self.input_sha = self.save('input.json', self.input)['sha256']
        reservation = {'marker': MARKER, 'input_sha256': self.input_sha, 'new_native_logout_requests': 1, 'new_native_exact401_requests': 1,
            'new_b_fallback_requests': 2, 'only_complete_new_login_proofs': True, 'no_login_retry': True, 'owned_exited_worker_release': True}
        self.cleanup_reservation = {'value': reservation, 'artifact': self.save('cleanup-reservation.json', reservation)}
        self.save('node.stdout', b''); self.save('node.stderr', b'')
        self.work_deadline = time.monotonic() + 350

    def admin_acknowledge(self, response):
        match = re.fullmatch(r'goby_session=([A-Za-z0-9_-]{43})', (response.get('set_cookie') or '').split(';', 1)[0])
        body = response.get('body') or {}; user, csrf = body.get('User', {}), body.get('CSRFToken')
        require(response.get('complete') is True and response.get('status') == 200 and match and user.get('Id') == self.state['admin_id'] and
            user.get('Name') == self.account['username'] and user.get('IsAdministrator') is True and user.get('IsDisabled') is False and
            isinstance(csrf, str) and 1 <= len(csrf) <= 256 and all(32 <= ord(char) < 127 for char in csrf), 'The native login lacks complete new administrator ownership.')
        fingerprint = sha(match[1].encode('utf-8'))
        require(all(row['token_hash'] != '\\x' + fingerprint for row in self.before['database']['tables']['sessions']), 'The native cookie belongs to an old authentication.')
        self.admin_token, self.admin_csrf, self.admin_sha, self.admin_proven = match[1], csrf, fingerprint, True

    def approve_request(self, label, purpose, body):
        require(re.fullmatch('[a-z][a-z0-9-]{0,79}', label) and label not in self.sent and len(self.sent) < 14,
            'A native request repeats an intent or exceeds the fixed budget.')
        if purpose == 'login':
            require(not self.login_sent and not self.admin_proven and 'baseline' in self.stages and not self.aborted and
                body == {'Name': self.account['username'], 'Password': self.account['password']}, 'Native login lacks the valid new B baseline stage.')
        else:
            require(self.admin_proven, 'An unproven native credential cannot authorize a request.')
            if purpose == 'restrict':
                require(not self.aborted and not self.restrict_sent and not self.restore_sent and 'baseline' in self.stages and
                    self.last_stage == 'baseline' and self.restore_reservation is not None and self.admin is not None and body == self.restricted,
                    'Restriction is repeated, aborted or not the exact reserved one-library update.')
            elif purpose == 'restore':
                require(self.restrict_sent and not self.restore_sent and self.restore_authorized and self.restore_reservation is not None and
                    body == self.restore_body, 'Restoration lacks fresh owned-state authorization or has already been dispatched.')
            else: require(purpose in ('read', 'logout', 'exact') and body is None, 'A native request left its bounded read/cleanup routes.')

    def native_request(self, label, purpose, body=None, recovery=False, cleanup=False):
        require((not cleanup or purpose in ('logout', 'exact')) and (not recovery or purpose in ('read', 'restore')) and
            (purpose != 'restore' or recovery) and (purpose not in ('login', 'restrict') or not recovery and not cleanup),
            'A native purpose cannot borrow another operation window or bypass an abort.')
        if not recovery and not cleanup and self.child is not None:
            require(not self.notice_abort(), 'The browser requested cleanup; no new business action is authorized.')
        self.check(recovery=recovery, cleanup=cleanup)
        self.approve_request(label, purpose, body)
        if cleanup:
            require(self.cleanup_reservation is not None and H.read_record(self.cleanup_reservation['artifact']) == self.cleanup_reservation['value'],
                'The durable exact-token cleanup reservation changed.')
        if purpose == 'restore':
            require(H.read_record(self.restore_reservation['artifact']) == self.restore_reservation['value'], 'The durable conditional restoration reservation changed.')
        method, path = {'login': ('POST', '/admin/v1/session'), 'read': ('GET', '/admin/v1/users/' + H.B),
            'restrict': ('PUT', '/admin/v1/users/' + H.B), 'restore': ('PUT', '/admin/v1/users/' + H.B),
            'logout': ('DELETE', '/admin/v1/session'), 'exact': ('GET', '/admin/v1/session')}[purpose]
        # Read-only reconciliation is part of the reserved restoration path.
        # A journal outage must not prevent discovering and restoring our write.
        resilient = cleanup or recovery
        self.save(label + '-intent.json', {'method': method, 'path': path, 'purpose': purpose,
            'body': None if purpose == 'login' else body}, soft=resilient)
        self.sent.add(label)
        if purpose == 'restrict':
            require(not self.notice_abort(), 'The browser aborted before restriction dispatch.')
            self.restrict_sent = True; self.restoration = 'pending'
        if purpose == 'restore': self.restore_sent = True
        if purpose == 'login': self.login_sent = True
        headers = {'Origin': H.BASE_URL, 'Accept': 'application/json', 'Accept-Encoding': 'identity', 'Connection': 'close'}
        if purpose != 'login': headers.update(Cookie='goby_session=' + self.admin_token, **{'X-CSRF-Token': self.admin_csrf})
        if body is not None: headers['Content-Type'] = 'application/json'
        connection = http.client.HTTPConnection('127.0.0.1', 18198, timeout=8)
        result = {'status': None, 'complete': False, 'body': None, 'set_cookie': None, 'completed_at': None}
        previous = signal.getsignal(signal.SIGALRM)
        signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(PermissionError('A native request exceeded its total time bound.')))
        signal.setitimer(signal.ITIMER_REAL, 12)
        try:
            self.request_count += 1
            connection.request(method, path, None if body is None else H.canonical(body), headers)
            response = connection.getresponse(); result.update(status=response.status, set_cookie=response.getheader('Set-Cookie'))
            if purpose == 'restrict': self.restrict_status = response.status
            if purpose == 'restore': self.restore_status = response.status
            length = response.getheader('Content-Length')
            require(length is None or length.isdigit() and int(length) <= 65536, 'A native response exceeded its byte bound.')
            raw = response.read(65537)
            require(len(raw) <= 65536 and (length is None or len(raw) == int(length)), 'A native response is incomplete.')
            result.update(complete=True, body=None if not raw else H.decode(raw), bytes=len(raw), sha256=sha(raw),
                completed_at=dt.datetime.now(dt.timezone.utc).isoformat())
            if purpose == 'login': self.admin_acknowledge(result)
            if purpose == 'restrict' and result['status'] == 200: self.restrict_ack_time = result['completed_at']
            if purpose == 'restore' and result['status'] == 200: self.restore_ack_time = result['completed_at']
        except Exception as error:
            result['failure_type'] = type(error).__name__
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0); signal.signal(signal.SIGALRM, previous); connection.close()
            self.responses[label] = result
            # A complete native ACK is already owned in memory. A failed login
            # journal cannot suppress its reserved exact-token cleanup.
            self.save(label + '-response-private.json', result, soft=resilient)
        return result

    def managed_read(self, label, recovery=False):
        result = self.native_request(label, 'read', recovery=recovery)
        require(result['complete'] and result['status'] == 200 and 'failure_type' not in result, 'The fresh managed B read did not complete.')
        return self.restriction.managed(result['body'], H.B)

    def take_browser_receipt(self, artifact, snapshot):
        path, digest = H.descriptor(artifact)
        require(path == BROWSER_ROOT / 'session-private.json', 'The browser credential receipt escaped this new scope.')
        document = H.decode(H.protected(path, digest, limit=32768))
        token, _ = H.validate_private_session(document, self.input, self.input_sha, self.child, self.before, snapshot)
        require(self.b_descriptor is None or self.b_descriptor == artifact, 'The one browser credential receipt changed.')
        self.b_private, self.b_descriptor, self.b_proof, self.b_token = document, artifact, document['proof'], token

    def bind_authentication(self):
        self.authenticated = self.snapshot('authenticated-full.json')
        additions = self.restriction.unchanged_rows(self.op, self.before, self.authenticated)
        require(all(len(rows) == {'sessions': 2, 'devices': 1, 'activity_entries': 2}.get(name, 0) for name, rows in additions.items()),
            'The two new logins changed unrelated state or created another credential.')
        ordinary = H.owned_session(self.before, self.authenticated, self.b_proof, self.state['server_id'])
        native = [row for row in additions['sessions'] if row['token_hash'] == '\\x' + self.admin_sha]
        require(len(native) == 1 and native[0]['id'] != ordinary['id'] and native[0]['user_id'] == self.state['admin_id'] and
            native[0]['kind'] == 'admin' and native[0]['revoked_at'] is None and native[0]['client_capabilities'] == {} and
            native[0]['device_registry_id'] is None and native[0]['client_name'] == 'Goby Dashboard' and
            native[0]['device_id'] == 'goby-dashboard' and native[0]['device_name'] == 'Web browser' and ordinary['revoked_at'] is None,
            'The new native and B authentication identities are not independently owned.')
        self.admin = native[0]
        captured = H.instant(self.authenticated['database']['metadata']['captured_at'])
        for row, days in ((ordinary, 30), (self.admin, 1)):
            require(H.instant(self.before['database']['metadata']['captured_at']) <= H.instant(row['created_at']) <=
                H.instant(row['last_seen_at']) <= captured and H.instant(row['expires_at']) == H.instant(row['created_at']) + dt.timedelta(days=days),
                'A new authentication has an invalid issuance or expiration window.')
        self.save('owned-logins.json', {'administrator_session_id': self.admin['id'], 'administrator_token_sha256': self.admin_sha,
            'browser_session_id': ordinary['id'], 'browser_token_sha256': self.b_proof['token_sha256']})

    def policy_checkpoint(self, name, current, classification, recovery=False):
        snapshot = self.snapshot(name, soft=recovery)
        rows = [row for row in snapshot['database']['tables']['users'] if row['id'] == H.B]
        require(len(rows) == 1 and classification in ('original', 'restricted', 'restored'), 'A foreign B revision cannot be overwritten.')
        row = rows[0]; expected = copy.deepcopy(self.old_b)
        if classification != 'original':
            expected.update(policy=self.restriction.raw_restored_policy(self.old_b, self.restricted if classification == 'restricted' else self.original),
                management_revision=self.old_b['management_revision'] + (1 if classification == 'restricted' else 2), updated_at=row['updated_at'])
            require(H.instant(self.before['database']['metadata']['captured_at']) <= H.instant(row['updated_at']) <=
                H.instant(snapshot['database']['metadata']['captured_at']), 'The owned B update timestamp is outside its actual checkpoint window.')
        require(self.op.equal_json(expected, row) and current['Revision'] == str(row['management_revision']) and
            current['Name'] == row['name'] and current['Policy'] == self.restriction.projected_policy(row['policy']),
            'The full B row differs from its fresh native projection or exact owned update.')
        try:
            additions = self.restriction.unchanged_rows(self.op, self.before, snapshot, H.B)
            if not recovery:
                require(all(len(rows) == {'sessions': 2, 'devices': 1, 'activity_entries': {'original': 2, 'restricted': 3, 'restored': 4}[classification]}.get(table, 0)
                    for table, rows in additions.items()), 'An intermediate phase has unapproved new authentication or business state.')
        except Exception as error:
            if not recovery: raise
            self.error('unrelated_state_drift_during_restoration', error)
        return snapshot

    def restrict(self):
        require(not self.notice_abort() and self.last_stage == 'baseline' and self.admin is not None, 'No validated active baseline authorizes restriction.')
        self.original = self.managed_read('native-original')
        require(self.original['Revision'] == str(self.old_b['management_revision']) and self.original['Name'] == self.old_b['name'] and
            self.original['Policy'] == self.restriction.projected_policy(self.old_b['policy']), 'The fresh native account is not the exact current baseline.')
        self.restricted = self.restriction.restricted_body(self.original)
        self.policy_checkpoint('original-user-full.json', self.original, 'original')
        self.save('original-managed-user.json', {'supported': self.original, 'raw_user': self.old_b})
        reservation = {'marker': MARKER, 'input_sha256': self.input_sha, 'original': self.original, 'restricted': self.restricted,
            'administrator_session_id': self.admin['id'], 'maximum_restoration_attempts': 1,
            'condition': 'Fresh exact restricted revision plus this administrator transactional user.updated audit.'}
        self.restore_reservation = {'value': reservation, 'artifact': self.save('restore-reservation.json', reservation)}
        response = self.native_request('restrict-user', 'restrict', self.restricted)
        require(response['complete'] and response['status'] == 200 and 'failure_type' not in response and
            response['body'].get('CurrentSessionRevoked') is False and
            self.restriction.classify(self.restriction.managed(response['body'], H.B), self.original, self.restricted) == 'restricted',
            'Restriction lacks its complete expected native acknowledgement.')
        self.current_managed = self.managed_read('restriction-readback')
        require(self.restriction.classify(self.current_managed, self.original, self.restricted) == 'restricted', 'Restriction readback has another revision or account.')
        snapshot = self.policy_checkpoint('restriction-readback-full.json', self.current_managed, 'restricted')
        require(owned_update(snapshot, self.admin['id'], self.admin['user_id'], 4, self.restriction.changed_fields(self.original, self.restricted)),
            'Restriction lacks its exact owned transactional audit.')

    def restore(self):
        if self.restoration == 'confirmed': return
        if not self.restrict_sent:
            self.restoration = 'not_required'; return
        if self.restore_deadline is None: self.restore_deadline = time.monotonic() + 150
        require(self.original is not None and self.restore_reservation is not None and self.admin is not None and self.reconcile_count < 2,
            'Restoration lacks its original reservation or exceeded its two read-only reconciliations.')
        self.reconcile_count += 1
        suffix = str(self.reconcile_count)
        current = self.managed_read('restore-reconcile-' + suffix, recovery=True)
        classification = self.restriction.classify(current, self.original, self.restricted)
        snapshot = self.policy_checkpoint('restore-reconcile-' + suffix + '-full.json', current, classification, recovery=True)
        fields = self.restriction.changed_fields(self.original, self.restricted)
        owned = owned_update(snapshot, self.admin['id'], self.admin['user_id'], 4, fields)
        restored_owned = owned_update(snapshot, self.admin['id'], self.admin['user_id'], 5, fields)
        decision = self.restriction.restore_decision(self.restrict_sent, self.restrict_status, self.restore_sent, classification, owned, restored_owned)
        if decision == 'not_committed':
            require(classification == 'original' and not owned, 'An unowned account change cannot be called a noncommitted restriction.')
            self.restoration = 'not_required'; return
        if decision == 'restored':
            self.restored, self.restoration = current, 'confirmed'; return
        require(decision == 'restore_once', 'The current revision and audit cannot authorize an overwrite or a second restoration.')
        self.restore_body = copy.deepcopy(self.original); self.restore_body['Revision'] = current['Revision']
        self.restore_authorized = True
        response = self.native_request('restore-user', 'restore', self.restore_body, recovery=True)
        acknowledged = (response['complete'] and response['status'] == 200 and 'failure_type' not in response and
            isinstance(response['body'], dict) and response['body'].get('CurrentSessionRevoked') is False)
        if acknowledged:
            try: acknowledged = self.restriction.classify(self.restriction.managed(response['body'], H.B), self.original, self.restricted) == 'restored'
            except Exception: acknowledged = False
        if not acknowledged:
            self.restore_ack_time = None
            self.error('restore_acknowledgement', PermissionError('The restoration response was incomplete; fresh state must decide its outcome.'))
        current = self.managed_read('restore-readback', recovery=True)
        classification = self.restriction.classify(current, self.original, self.restricted)
        snapshot = self.policy_checkpoint('restore-readback-full.json', current, classification, recovery=True)
        require(self.restriction.restore_decision(True, self.restrict_status, True, classification,
            owned_update(snapshot, self.admin['id'], self.admin['user_id'], 4, fields),
            owned_update(snapshot, self.admin['id'], self.admin['user_id'], 5, fields)) == 'restored',
            'Restoration cannot be confirmed without a forbidden second write.')
        self.restored, self.restoration = current, 'confirmed'

    def properties(self):
        fields = 'Id,LoadState,ActiveState,SubState,MainPID,ExecMainCode,ExecMainStatus,ControlGroup,InvocationID,Transient,User,Group'
        result = subprocess.run(['/usr/bin/systemctl', 'show', UNIT, '--no-pager', '--property=' + fields],
            capture_output=True, text=True, timeout=10, check=False, env=self.op.ENV)
        require(result.returncode in (0, 1, 4) and len(result.stdout) <= 32768, 'The dedicated worker state cannot be read.')
        return dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)

    def node_arguments(self):
        return [str(self.args.node), str(self.args.driver), '--input', str(ROOT / 'input.json'), '--input-sha256', self.input_sha,
            '--output', str(BROWSER_ROOT)]

    def cgroup_empty(self):
        group = Path('/sys/fs/cgroup' + CGROUP)
        if not group.exists(): return True
        paths = [group, *group.rglob('*')]
        require(len(paths) <= 256 and all(not path.is_symlink() for path in paths), 'The owned cgroup tree exceeds its bound or contains a link.')
        return all(not path.read_text().strip() for path in paths if path.name == 'cgroup.procs')

    def poll_worker(self):
        status = self.properties()
        pid = int(status.get('MainPID', '0'))
        if pid > 1:
            require(status.get('Id') == UNIT and status.get('Transient') == 'yes' and status.get('ControlGroup') == CGROUP and
                status.get('User') == 'root' and status.get('Group') == 'root' and H.ID.fullmatch(status.get('InvocationID', '')),
                'The dedicated transient worker has another identity.')
            current = H.proc_identity(pid)
            if self.child is None:
                process = Path('/proc') / str(pid)
                require(current['boot_id'] == H.PROCESS['boot_id'] and process.stat().st_uid == process.stat().st_gid == 0 and
                    os.readlink(process / 'exe') == str(self.args.node) and sha((process / 'exe').read_bytes()) == self.args.node_sha256 and
                    (process / 'cmdline').read_bytes() == b'\0'.join(value.encode() for value in self.node_arguments()) + b'\0' and
                    any(row.split(':', 2)[-1] == CGROUP for row in (process / 'cgroup').read_text().splitlines()) and H.proc_identity(pid) == current,
                    'The Node PID, executable, invocation or cgroup is not owned.')
                self.invocation = status['InvocationID']
                self.child = {**current, 'uid': 0, 'gid': 0, 'executable_path': str(self.args.node), 'executable_sha256': self.args.node_sha256, 'cgroup': CGROUP}
                self.save('node-process.json', {'process': self.child, 'invocation_id': self.invocation}, soft=True)
            require(current == {key: self.child[key] for key in ('pid', 'start_ticks', 'boot_id')} and status['InvocationID'] == self.invocation,
                'The owned worker restarted or changed identity.')
        elif self.child is not None:
            require(status.get('LoadState') == 'not-found' or status.get('InvocationID') == self.invocation,
                'Another invocation reused the owned worker unit.')
        return status

    def launch(self):
        self.check()
        require(self.properties().get('LoadState') == 'not-found', 'The worker name became occupied before launch.')
        age = dt.datetime.now(dt.timezone.utc) - H.instant(self.before['database']['metadata']['captured_at'])
        require(dt.timedelta(0) <= age <= dt.timedelta(seconds=150), 'The fresh complete before snapshot is too old for browser login.')
        self.save('launch-intent.json', {'unit': UNIT, 'arguments': self.node_arguments(), 'input_sha256': self.input_sha,
            'node_sha256': self.args.node_sha256, 'limits': {'cpu_percent': 150, 'memory_bytes': 1073741824, 'tasks': 128, 'runtime_seconds': 660}})
        arguments = ['/usr/bin/systemd-run', '--unit=' + UNIT, '--service-type=exec', '--quiet', '--property=User=root', '--property=Group=root',
            '--property=WorkingDirectory=' + str(ROOT), '--property=Restart=no', '--property=RemainAfterExit=yes', '--property=CPUQuota=150%',
            '--property=MemoryMax=1G', '--property=TasksMax=128', '--property=RuntimeMaxSec=660', '--property=TimeoutStopSec=15',
            '--property=KillMode=control-group', '--property=UMask=0077', '--property=PrivateTmp=yes', '--property=NoNewPrivileges=yes',
            '--property=ProtectSystem=strict', '--property=ReadWritePaths=' + str(ROOT), '--property=IPAddressDeny=any', '--property=IPAddressAllow=localhost',
            '--property=UnsetEnvironment=DEBUG PWDEBUG NODE_OPTIONS NODE_PATH', '--setenv=HOME=/root', '--setenv=LANG=C.UTF-8',
            '--property=StandardOutput=append:' + str(ROOT / 'node.stdout'), '--property=StandardError=append:' + str(ROOT / 'node.stderr'),
            '--', *self.node_arguments()]
        self.launched, self.worker_started = True, time.monotonic()
        result = subprocess.run(arguments, capture_output=True, timeout=15, check=False, env=self.op.ENV)
        self.save('launch-result.json', {'returncode': result.returncode, 'stdout_sha256': sha(result.stdout), 'stderr_sha256': sha(result.stderr)})
        require(result.returncode == 0, 'The one owned worker start returned failure; no second start is allowed.')
        deadline = time.monotonic() + 10
        while self.child is None and time.monotonic() < deadline:
            try: status = self.poll_worker()
            except FileNotFoundError: continue
            require(int(status.get('MainPID', '0')) > 0 or status.get('ActiveState') in ('activating', 'active'), 'The worker exited before its live identity was captured.')
            if self.child is None: time.sleep(0.1)
        require(self.child is not None, 'The new worker never acquired a proven live identity.')

    def wait_stage(self, name, seconds):
        expected_previous = None if name == 'baseline' else self.controls[name]['artifact']['sha256']
        require(name not in self.stages and (name == 'baseline' and self.last_stage is None or
            name == 'restricted' and self.last_stage == 'baseline' or name == 'restored' and self.last_stage == 'restricted'),
            'The next browser stage is out of order.')
        deadline = min(self.work_deadline, time.monotonic() + seconds)
        while time.monotonic() < deadline:
            require(not self.notice_abort(), 'The browser aborted observation and requested restoration/cleanup.')
            status = self.poll_worker()
            require(int(status.get('MainPID', '0')) > 1, 'The browser worker exited before its next required stage.')
            found = self.read_ipc(BROWSER_ROOT / ('stage-' + name + '.json'))
            if found is not None:
                document, artifact = found
                if name == 'baseline':
                    snapshot = self.snapshot('baseline-browser-full.json')
                    self.take_browser_receipt(document.get('session_private'), snapshot)
                validate_stage(document, name, self.input, self.input_sha, self.child, expected_previous, self.b_descriptor, self.b_proof['token_sha256'],
                    None if name == 'baseline' else self.controls[name]['document']['write_completed_at'])
                self.stages[name] = {'document': document, 'artifact': artifact}
                self.records['browser-stage-' + name + '.json'] = artifact
                self.last_stage = name
                return
            time.sleep(0.2)
        raise PermissionError('The next bounded browser observation stage did not complete.')

    def await_worker(self):
        require(self.launched and self.worker_started is not None, 'No owned worker was dispatched.')
        deadline = self.worker_started + 700
        while time.monotonic() < deadline:
            try: status = self.poll_worker()
            except FileNotFoundError:
                time.sleep(0.2); continue
            if status.get('MainPID') == '0' and self.cgroup_empty():
                if status.get('ActiveState') == 'active' and status.get('SubState') == 'exited':
                    require(self.child is not None and status.get('InvocationID') == self.invocation, 'The exited worker is not this invocation.')
                    self.save('worker-exited.json', status, soft=True)
                    require(H.read_record(self.cleanup_reservation['artifact']) == self.cleanup_reservation['value'], 'The reserved exited-worker cleanup authority changed.')
                    self.save('worker-release-intent.json', {'unit': UNIT, 'invocation_id': self.invocation, 'child': self.child,
                        'main_pid': 0, 'cgroup_empty': True}, soft=True)
                    result = subprocess.run(['/usr/bin/systemctl', 'stop', UNIT], capture_output=True, timeout=20, check=False, env=self.op.ENV)
                    require(result.returncode == 0, 'The exact exited worker unit did not release.')
                    ended = self.properties()
                    require(ended.get('MainPID') == '0' and ended.get('ActiveState') in ('inactive', 'failed') and self.cgroup_empty(),
                        'The released worker is not terminal with an empty cgroup.')
                    self.terminal = {**status, 'ActiveState': ended['ActiveState'], 'SubState': ended['SubState'], 'cgroup_empty': True}
                elif status.get('ActiveState') in ('inactive', 'failed'):
                    self.terminal = {**status, 'cgroup_empty': True}
                if self.terminal is not None:
                    self.worker_closed = True
                    self.save('worker-terminal.json', self.terminal, soft=True)
                    return
            time.sleep(0.5)
        raise PermissionError('The dedicated worker did not close within its full 700-second lifecycle bound.')

    def close_admin(self):
        if not self.admin_proven: return
        self.cleanup_deadline = time.monotonic() + 90
        successful = []
        for label, purpose, expected in (('native-logout', 'logout', 204), ('native-exact', 'exact', 401)):
            try:
                result = self.native_request(label, purpose, cleanup=True)
                require(result['complete'] and result['status'] == expected and 'failure_type' not in result and
                    (expected != 204 or result['body'] is None), 'The new administrator did not close with logout204 and exact401.')
                successful.append(purpose)
            except Exception as error: self.error(label, error)
        self.admin_closed = successful == ['logout', 'exact']

    def read_browser_final(self, snapshot):
        require(self.worker_closed and self.child is not None, 'Final browser evidence cannot authorize actions while its process tree can still run.')
        report_path = BROWSER_ROOT / 'report.json'
        if report_path.exists():
            try:
                raw = H.protected(report_path, limit=4 << 20); report = H.decode(raw)
                require(report.get('marker') == 'goby-client-library-permission-report-v1' and report.get('version') == 1 and
                    report.get('mode') == MODE and report.get('input_sha256') == self.input_sha and
                    report.get('source_closure_sha256') == sha(H.canonical(self.sources)) and report.get('controller') == self.input['controller'] and
                    report.get('node_process') == self.child and report.get('candidate') == self.input['candidate'] and
                    report.get('authority') == self.input['authority'] and report.get('client_acceptance') is False,
                    'The final browser report has another scope, authority or worker identity.')
                self.browser_report = report
                self.records['browser-report.json'] = {'path': str(report_path), 'sha256': sha(raw)}
            except Exception as error: self.error('browser_report', error)
        private_path = BROWSER_ROOT / 'session-private.json'
        if private_path.exists():
            raw = H.protected(private_path, limit=32768)
            self.take_browser_receipt({'path': str(private_path), 'sha256': sha(raw)}, snapshot)
            if self.browser_report and self.browser_report.get('login_proof') != self.b_proof:
                self.error('browser_private_proof', PermissionError('The browser report and durable login receipt disagree.'))
                self.browser_report = None
            if not (self.browser_report and H.ui_logout_proven(self.browser_report, self.b_proof)):
                self.fallback_b(snapshot)
        elif self.browser_report and self.browser_report.get('login_proof'):
            raise PermissionError('The acknowledged browser login lacks its durable private credential receipt.')

    def fallback_b(self, snapshot):
        require(self.worker_closed and self.b_private is not None and not self.b_fallback, 'The exact B fallback is unavailable or already consumed.')
        token, row = H.validate_private_session(self.b_private, self.input, self.input_sha, self.child, self.before, snapshot)
        self.cleanup_deadline = time.monotonic() + 90
        self.check(cleanup=True)
        require(H.read_record(self.cleanup_reservation['artifact']) == self.cleanup_reservation['value'], 'The original exact-token cleanup reservation changed.')
        self.b_fallback = True
        self.save('browser-fallback-intent.json', {'session_id': row['id'], 'token_sha256': self.b_proof['token_sha256'],
            'already_revoked': row['revoked_at'] is not None, 'permission_ui_acceptance': False}, soft=True)
        previous = signal.getsignal(signal.SIGALRM)
        signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(PermissionError('The B cleanup exceeded its total time bound.')))
        signal.setitimer(signal.ITIMER_REAL, 25)
        try:
            records = H.fallback_http(token, row['revoked_at'] is not None)
            self.save('browser-fallback-result.json', {'requests': records, 'permission_ui_acceptance': False}, soft=True)
            require(records and records[-1].get('status') == 401 and records[-1].get('complete') is True and
                all('failure_type' not in record for record in records), 'The exact new B credential did not close completely.')
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0); signal.signal(signal.SIGALRM, previous)

    def collect(self):
        current = None
        try:
            current = self.snapshot('after-browser-full.json', soft=True)
            if self.worker_closed:
                for name in ('node.stdout', 'node.stderr'):
                    raw = H.protected(ROOT / name, limit=4 << 20)
                    self.records[name] = {'path': str(ROOT / name), 'sha256': sha(raw)}
        except Exception as error: self.error('after_browser_snapshot_or_logs', error)
        if current is not None:
            try: self.read_browser_final(current)
            except Exception as error: self.error('browser_owned_cleanup', error)
        self.close_admin()
        try: self.after = self.snapshot('after-full.json', soft=True)
        except Exception as error: self.error('complete_after_snapshot', error)
        try:
            self.cleanup_deadline = time.monotonic() + 90
            self.check(cleanup=True)
            media = self.ext.media_witness(self.op, self.state)
            self.save('media-after.json', media, soft=True)
            require(self.op.equal_json(self.media, media), 'A protected media byte, inode or inventory changed.')
        except Exception as error: self.error('private_source_media_preservation', error)
        if self.after is not None and self.authenticated is not None and self.b_proof is not None:
            try:
                raw = H.protected(BROWSER_ROOT / 'capabilities-private.json', limit=2 << 20)
                self.capability_document = H.decode(raw)
                values = H.capability_values(self.capability_document, self.b_proof, self.input, self.input_sha, self.child)
                successful = [row for row in self.capability_document['entries'] if row['completed'] and row['status'] == 204]
                expected = {'path': str(BROWSER_ROOT / 'capabilities-private.json'), 'sha256': sha(raw),
                    'request_count': len(self.capability_document['entries']), 'last_successful_body_sha256': successful[-1]['body_sha256'] if successful else None}
                require(self.browser_report is not None and self.browser_report.get('capabilities_private') == expected, 'The complete browser capability journal is not report-bound.')
                self.delta = validate_full_delta(self.op, self.restriction, self.before, self.authenticated, self.after,
                    self.admin, self.b_proof, self.original, self.restricted, values)
                self.restriction.quiescent(self.after)
                self.validate_ui_result()
            except Exception as error: self.error('final_state_or_ui_proof', error)
        elif self.after is not None:
            try: self.restriction.compare_fixed_snapshot(self.op, self.before, self.after)
            except Exception as error: self.error('incomplete_protocol_state_delta', error)

    def validate_ui_result(self):
        report = self.browser_report or {}; closure = report.get('closure', {})
        require(not self.aborted and not self.b_fallback and self.admin_closed and self.restoration == 'confirmed' and
            self.worker_closed and self.terminal.get('ExecMainStatus') == '0' and set(self.stages) == {'baseline', 'restricted', 'restored'} and
            set(self.controls) == {'restricted', 'restored', 'close'} and report.get('result') == 'passed' and
            report.get('outcome') == 'permission_observation_after_explicit_reload' and report.get('permission_ui_acceptance') is True and
            report.get('client_acceptance') is False and report.get('login_proof') == self.b_proof and H.ui_logout_proven(report, self.b_proof) and
            report.get('restoration') == 'confirmed' and report.get('closed_after_restoration_confirmation') is True and
            not report.get('failure') and not report.get('abort') and
            all(report.get(name) == self.stages[name]['document']['observation'] for name in ('baseline', 'restricted', 'restored')),
            'The original client did not complete the exact permission observation and owned cleanups.')
        expected_close = {**self.controls['close']['artifact'], 'value': self.controls['close']['document']}
        require(report.get('control_close') == expected_close and
            report.get('stages') == [{'name': name, **self.stages[name]['artifact']} for name in ('baseline', 'restricted', 'restored')] and
            report.get('controls') == [{'name': name, **self.controls[name]['artifact'], 'value': self.controls[name]['document']} for name in ('restricted', 'restored')],
            'The browser did not consume the exact controller publications and stage chain.')
        for name in ('restricted', 'restored'):
            window = report[name]['spontaneous']['window']
            require(window['start_elapsed_ms'] == epoch_ms(self.controls[name]['document']['write_completed_at']) - epoch_ms(report['started_at']),
                'The passive window is not aligned to the real native ACK and browser actor clock.')
        require(all(closure.get(key) is True for key in ('context_closed', 'browser_closed', 'proxy_closed')) and
            all(closure.get(key) == 0 for key in ('http_pending', 'websocket_pending', 'websocket_active', 'sockets_remaining')) and
            report.get('actor', {}).get('websocket_handshake_budget') == 3 and type(closure.get('websocket_opened')) is int and
            1 <= closure['websocket_opened'] <= 3 and closure.get('websocket_closed') == closure['websocket_opened'] and closure.get('cleanup_failures') == [],
            'Browser, proxy or the bounded owned WebSocket lifetimes did not close completely.')

    def run_flow(self):
        self.launch()
        self.wait_stage('baseline', 150)
        response = self.native_request('native-login', 'login', {'Name': self.account['username'], 'Password': self.account['password']})
        require(response['complete'] and response['status'] == 200 and self.admin_proven, 'The separate administrator login did not complete.')
        self.bind_authentication()
        self.restrict()
        self.publish_control('restricted')
        self.wait_stage('restricted', 90)
        self.restore()
        self.publish_control('restored')
        self.wait_stage('restored', 90)

    def execute(self):
        try:
            self.load()
            if self.args.check_only:
                return {'marker': MARKER, 'result': 'checked', 'http_requests': 0, 'output_created': False, 'permission_ui_acceptance': False, 'client_acceptance': False}
            self.prepare(); self.phase = 'observation'
            try: self.run_flow()
            except Exception as error: self.error('observation', error)
            finally:
                self.phase = 'restoration'
                try: self.restore()
                except Exception as error:
                    self.restoration = 'recovery_required'; self.error('conditional_restoration', error)
                if self.launched and self.child is None:
                    try: self.poll_worker()
                    except Exception as error: self.error('worker_identity_after_failure', error)
                if self.child is not None and self.restoration in ('confirmed', 'not_required'):
                    try: self.publish_control('close')
                    except Exception as error: self.error('close_control', error)
                if self.launched:
                    try: self.await_worker()
                    except Exception as error: self.error('worker_lifecycle', error)
                self.phase = 'collection'; self.collect()
            passed = not self.errors and self.delta is not None and self.admin_closed and self.restoration == 'confirmed' and not self.b_fallback and not self.aborted
            result = {'marker': MARKER, 'version': 1, 'result': 'passed' if passed else 'failed', 'phase': 'complete',
                'outcome': 'permission_observation_after_explicit_reload' if passed else 'failed', 'permission_ui_acceptance': passed, 'client_acceptance': False,
                'restoration': self.restoration, 'administrator_closed': self.admin_closed, 'browser_fallback': self.b_fallback,
                'worker_closed': self.worker_closed, 'candidate': self.input['candidate'], 'authority': AUTHORITY, 'input_sha256': self.input_sha,
                'source_closure': self.sources, 'native_http_requests': self.request_count, 'proof': self.delta, 'errors': self.errors,
                'passive_observations': {name: value['document']['observation'].get('spontaneous') for name, value in self.stages.items() if name != 'baseline'},
                'evidence': dict(self.records), 'boundary': 'Only original Movies permission UI after explicit reloads. Passive absence is retained, not an automatic-refresh guarantee or full M3 acceptance.'}
            self.save('report.json', result)
            return result
        except Exception as error:
            self.error(self.phase, error)
            if self.root_fd is not None:
                self.save('failure.json', {'marker': MARKER, 'result': 'failed', 'phase': self.phase, 'restoration': self.restoration,
                    'errors': self.errors, 'evidence': dict(self.records), 'permission_ui_acceptance': False, 'client_acceptance': False}, soft=True)
            raise PermissionError('The permission observation stopped; retain the independent evidence and do not retry.') from None
        finally:
            self.admin_token = self.admin_csrf = self.b_token = None
            if self.b_private is not None: self.b_private['token'] = None
            if self.root_fd is not None: os.close(self.root_fd)
            if self.lock is not None: os.close(self.lock)


def arguments(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--script-sha256', required=True)
    parser.add_argument('--driver', type=Path, required=True)
    parser.add_argument('--source-closure', type=Path, required=True)
    parser.add_argument('--source-closure-sha256', required=True)
    parser.add_argument('--node', type=Path, required=True)
    parser.add_argument('--node-sha256', required=True)
    parser.add_argument('--check-only', action='store_true')
    value = parser.parse_args(argv)
    require(all(re.fullmatch('[0-9a-f]{64}', getattr(value, key)) for key in ('script_sha256', 'source_closure_sha256', 'node_sha256')) and
        value.driver.is_absolute() and value.node.is_absolute() and value.source_closure.is_absolute(), 'Explicit source/runtime arguments are incomplete.')
    return value


def load_home():
    global H
    info = HOME_SOURCE.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == info.st_gid == 0 and info.st_nlink == 1 and stat.S_IMODE(info.st_mode) == 0o600,
        'The frozen Home helper is not the accepted private regular file.')
    raw = HOME_SOURCE.read_bytes(); require(sha(raw) == HOME_SHA, 'The frozen Home helper source changed.')
    H = types.ModuleType('permission_home_primitives'); H.__file__ = str(HOME_SOURCE)
    exec(compile(raw, str(HOME_SOURCE), 'exec'), H.__dict__)


if __name__ == '__main__':
    try:
        options = arguments(); load_home(); result = Run(options).execute()
        print(H.exact({'marker': MARKER, 'result': result['result'], 'permission_ui_acceptance': result['permission_ui_acceptance'], 'client_acceptance': False}))
        sys.exit(0 if result['result'] in ('passed', 'checked') else 1)
    except Exception:
        print('{"marker":"goby-client-library-permission-observation-v1","result":"failed","permission_ui_acceptance":false,"client_acceptance":false}', file=sys.stderr)
        sys.exit(1)
