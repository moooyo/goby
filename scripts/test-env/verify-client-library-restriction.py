#!/usr/bin/env python3
"""Verify one candidate-only library restriction and conditional restoration.

Read-only preflight precedes every new run. Only one folder-only restriction,
one conditional restoration, three owned logins and the documented API matrix
are supported. There is no playback preparation, scan, password reset, service
action, old-session cleanup, retry or adoption of an occupied evidence root.
"""

from __future__ import annotations

import argparse
from collections import Counter
import copy
import datetime as dt
import fcntl
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import secrets
import signal
import stat
import subprocess
import sys
import time
import types
from urllib.parse import urlencode

sys.dont_write_bytecode = True
WORK = Path('/opt/goby-test/exec-work-m3e')
OUTPUT = WORK / 'client-library-restriction-v1'
MARKER = 'goby-client-library-restriction-v1'
CLIENT = 'Goby Library Restriction Recorder'
SOURCE_SHA = 'af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620'
STATE_SHA = '5319bc49b2753b84ca04f279523f2482a49a94fabd9944dc369347b6d87224e1'
MANIFEST_SHA = 'a65070315ce3b31dd70143267cbf774a838bc0e5b1ed99f34c3c759d392daa65'
PROFILE_SHA = 'b443e5f6d5faceb3486d68644298b0a1527f6dcc1e4ac1109ff6d0c252fdfb36'
AFTER_SHA = '74640ff8ce353ad61b41364fbeedaff4fa2c14e55075ce55869e8be85963a10b'
BASELINE_SHA = '3320e35d1ad9ce4a3583ac264954dd097e03a8c8a2fa28a772eb0a57524f5f7b'
LIBRARIES = {'movies': 'a9993591e72f0f2e7babcbf8b9c50790', 'music': '6383d20008836e137559698c29b10395',
             'tv': 'a34ce665fb75421ef7551570f353d705', 'extras': '57a85c1ca5b6c7ae602c587755250b2f'}
ACTORS = {'A': '34b4c24f6568659af7ce17938fae7f81', 'B': 'ecbbe4cb82403879bc4b4f78894c5738'}
MOVIE = '268051d3ca734aefcf94e245fb25ad55'
POSITIVE = '1ac8b0f4188531e8b701bfda055d7a34'
POLICY_KEYS = ('EnableAllFolders', 'EnabledFolders', 'EnableMediaPlayback', 'EnablePlaybackRemuxing',
               'EnableAudioPlaybackTranscoding', 'EnableVideoPlaybackTranscoding')
MAX_REVISION = (1 << 63) - 1
MAX_REQUESTS, BUSINESS_REQUESTS = 80, 44


class RestrictionError(Exception):
    """A specific identity, request, recovery or preservation boundary failed."""


def require(value, message):
    if not value:
        raise RestrictionError(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def instant(value):
    require(isinstance(value, str), 'A required timestamp is missing.')
    result = dt.datetime.fromisoformat(value.replace('Z', '+00:00'))
    require(result.tzinfo is not None, 'A timestamp has no timezone.')
    return result


def revision(value):
    require(isinstance(value, str) and re.fullmatch('[1-9][0-9]*', value) and int(value) <= MAX_REVISION, 'A native revision is not canonical.')
    return int(value)


def managed(value, user_id):
    require(isinstance(value, dict) and isinstance(value.get('User'), dict), 'A native user response is incomplete.')
    user = value['User']
    require(user.get('Id') == user_id and isinstance(user.get('Name'), str) and user['Name'] and
            user.get('IsAdministrator') is False and user.get('IsDisabled') is False, 'A selected user changed identity or ordinary authority.')
    revision(user.get('Revision'))
    policy = user.get('Policy')
    require(isinstance(policy, dict) and set(policy) == set(POLICY_KEYS) and
            all(type(policy[key]) is bool for key in POLICY_KEYS if key != 'EnabledFolders'), 'The supported native Policy shape changed.')
    folders = policy['EnabledFolders']
    require(isinstance(folders, list) and len(folders) <= 256 and all(isinstance(value, str) and value in LIBRARIES.values() for value in folders) and
            folders == sorted(set(folders)), 'The native folder population is not canonical and current.')
    return copy.deepcopy({key: user[key] for key in ('Revision', 'Name', 'IsAdministrator', 'IsDisabled', 'Policy')})


def supported(body):
    return {key: value for key, value in body.items() if key != 'Revision'}


def restricted_body(original):
    require(revision(original['Revision']) <= MAX_REVISION - 2, 'There is no revision capacity for both writes.')
    result = copy.deepcopy(original)
    current = set(LIBRARIES.values()) if original['Policy']['EnableAllFolders'] else set(original['Policy']['EnabledFolders'])
    require({LIBRARIES['movies'], LIBRARIES['extras']} <= current, 'Both original Movies and positive Extras must already be accessible.')
    result['Policy']['EnableAllFolders'] = False
    result['Policy']['EnabledFolders'] = sorted(current - {LIBRARIES['movies']})
    require(LIBRARIES['extras'] in result['Policy']['EnabledFolders'] and result != original, 'The exact one-library restriction is not effective.')
    return result


def classify(current, original, restricted):
    number, start = revision(current['Revision']), revision(original['Revision'])
    if number == start and supported(current) == supported(original):
        return 'original'
    if number == start + 1 and supported(current) == supported(restricted):
        return 'restricted'
    if number == start + 2 and supported(current) == supported(original):
        return 'restored'
    return 'foreign'


def restore_decision(sent, status, restore_sent, classification, restriction_owned, restoration_owned=False):
    if not sent or status in (400, 401, 403, 404, 409, 415, 422):
        return 'not_committed'
    if classification == 'original' and not restore_sent:
        return 'not_committed'
    if classification == 'restricted' and restriction_owned and not restore_sent:
        return 'restore_once'
    if classification == 'restored' and restore_sent and restriction_owned and restoration_owned:
        return 'restored'
    return 'recovery_required'


def raw_restored_policy(row, original):
    result = copy.deepcopy(row['policy']) if isinstance(row['policy'], dict) else {}
    result.update(copy.deepcopy(original['Policy']))
    result.update(IsAdministrator=original['IsAdministrator'], IsDisabled=original['IsDisabled'])
    return result


def projected_policy(raw):
    if not isinstance(raw, dict):
        return {key: [] if key == 'EnabledFolders' else False for key in POLICY_KEYS}
    result = {key: raw.get(key, True) is True for key in POLICY_KEYS if key != 'EnabledFolders'}
    folders = raw.get('EnabledFolders', [])
    valid_flag = 'EnableAllFolders' not in raw or type(raw['EnableAllFolders']) is bool
    result['EnabledFolders'] = (sorted(set(folders)) if valid_flag and isinstance(folders, list) and len(folders) <= 256 and
        all(isinstance(value, str) and value in LIBRARIES.values() for value in folders) else [])
    return result


def changed_fields(original, restricted):
    return [key for key in ('EnableAllFolders', 'EnabledFolders') if original['Policy'][key] != restricted['Policy'][key]]


def validate_libraries(rows):
    require(isinstance(rows, list) and len(rows) == 4 and {row['id'] for row in rows} == set(LIBRARIES.values()), 'The exact four-library population changed.')


def compare_fixed_snapshot(op, before, after):
    for key in ('schema', 'runtime_sha256', 'browser_sha256', 'added_viewer_credentials', 'recovery'):
        require(op.equal_json(before[key], after[key]), 'A private or schema binding changed.')
    for key in ('tables', 'sequences', 'catalog', 'unsupported'):
        require(op.equal_json(before['database'][key], after['database'][key]), 'A prior complete database state changed.')
    left, right = before['database']['metadata'], after['database']['metadata']
    require(set(left) == set(right) and instant(left['captured_at']) <= instant(right['captured_at']), 'Snapshot metadata shape or capture order changed.')
    require(all(op.equal_json(left[key], right[key]) for key in left if key != 'captured_at'), 'A database/catalog identity changed.')


def quiescent(snapshot):
    tables = snapshot['database']['tables']
    require(snapshot['schema'] == 27 and len(tables) == 35 and len(tables['items']) == 22 and tables['encoding_jobs'] == [], 'The complete idle schema27 profile differs.')
    validate_libraries(tables['libraries'])
    for table, field in (('scan_jobs', 'status'), ('task_runs', 'state'), ('task_run_children', 'state')):
        require(not any(str(row[field]).lower() in ('waiting', 'pending', 'queued', 'running', 'stopping') for row in tables[table]), 'A scan or task is active.')
    sessions = {row['id']: row for row in tables['sessions']}
    for play in tables['play_sessions']:
        require(play['state'] in ('Prepared', 'Stopped', 'Expired'), 'Playback is active or unknown.')
        if play['state'] == 'Prepared':
            auth = sessions.get(play['auth_session_id'], {})
            require(play['started_at'] is None and play['counted'] is False and play['application_client_id'] is None and
                    auth.get('kind') == 'emby' and auth.get('user_id') == play['user_id'] and auth.get('revoked_at') is not None,
                    'Prepared history is not bound to an already revoked ordinary session.')


def protected(path, expected=None, limit=64 << 20):
    require(path.is_absolute() and path.is_relative_to(WORK) and '..' not in path.parts, 'A selected path is outside the protected workspace.')
    for entry in reversed((path, *path.parents)):
        info = entry.lstat()
        require(not entry.is_symlink() and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022, 'An input is not root protected.')
    info = path.stat()
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_size <= limit, 'An input exceeds its file bound.')
    raw = path.read_bytes()
    require(expected is None or sha(raw) == expected, 'An explicitly selected input changed.')
    return raw


def source(path, expected, name):
    raw = protected(path, expected, 1 << 20)
    result = types.ModuleType(name)
    result.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), result.__dict__)
    return result


def error_code(body, route):
    section, field = ('ResponseStatus', 'ErrorCode') if route.startswith('/emby/') else ('Error', 'Code')
    code = body.get(section, {}).get(field) if isinstance(body, dict) and isinstance(body.get(section), dict) else None
    return code if isinstance(code, str) and re.fullmatch('[a-z_]{1,64}', code) else None


class Actor:
    def __init__(self, run, slot, account, user_id):
        self.run, self.slot, self.account, self.user_id = run, slot, account, user_id
        self.device = 'goby-restriction-' + slot.lower() + '-' + secrets.token_hex(16)
        self.token = self.cookie = self.csrf = self.session_id = None
        self.proven = self.closed = False
        self.status = {'login_status': None, 'logout_status': None, 'exact_status': None}

    def login(self):
        native = self.slot == 'admin'
        body = {'Name': self.account['username'], 'Password': self.account['password']} if native else {'Username': self.account['username'], 'Pw': self.account['password']}
        response = self.run.request(self.slot + '-login', 'POST', '/admin/v1/session' if native else '/emby/Users/AuthenticateByName', self, body, purpose='login')
        require(response['complete'] and response['failure_type'] is None and response['status'] == 200 and self.proven,
                'A new login was not completely acknowledged and owned.')
        self.status['login_status'] = 200

    def acknowledge(self, response, cookie):
        body, status = response['body'], response['status']
        if self.slot == 'admin':
            match = re.fullmatch(r'goby_session=([A-Za-z0-9_-]{43})', (cookie or '').split(';', 1)[0])
            if match:
                self.token, self.cookie = match[1], match[0]
            user = body.get('User', {}) if isinstance(body, dict) else {}
            csrf = body.get('CSRFToken') if isinstance(body, dict) else None
            self.proven = bool(status == 200 and self.token and user.get('Id') == self.user_id and user.get('Name') == self.account['username'] and
                user.get('IsAdministrator') is True and user.get('IsDisabled') is False and isinstance(csrf, str) and 1 <= len(csrf) <= 256 and
                all(32 <= ord(value) < 127 for value in csrf))
            if self.proven:
                self.csrf = csrf
                self.run.secrets.add(csrf)
        else:
            if isinstance(body, dict) and isinstance(body.get('AccessToken'), str) and re.fullmatch('[A-Za-z0-9_-]{43}', body['AccessToken']):
                self.token = body['AccessToken']
            user = body.get('User', {}) if isinstance(body, dict) else {}
            session = body.get('SessionInfo', {}) if isinstance(body, dict) else {}
            self.proven = bool(status == 200 and self.token and body.get('ServerId') == self.run.state['server_id'] and user.get('Id') == self.user_id and
                user.get('Name') == self.account['username'] and user.get('Policy', {}).get('IsAdministrator') is False and
                session.get('UserId') == self.user_id and session.get('DeviceId') == self.device and re.fullmatch('[0-9a-f]{32}', session.get('Id', '')))
            if self.proven:
                self.session_id = session['Id']
        if self.token:
            self.run.secrets.add(self.token)
            self.status['token_sha256'] = sha(self.token.encode())
        self.run.journal(self.slot.lower() + '-login-ack.json', {'body': body, 'cookie': cookie, 'identity_proven': self.proven})

    def logout(self):
        if self.closed:
            return
        if not self.proven:
            require(self.token is None, 'An unproven credential remains quarantined.')
            return
        native = self.slot == 'admin'
        if self.status['logout_status'] != 204:
            result = self.run.request(self.slot + '-logout', 'DELETE' if native else 'POST', '/admin/v1/session' if native else '/emby/Sessions/Logout',
                                      self, purpose='logout', recovery=True)
            self.status['logout_status'] = result['status']
            require(result['complete'] and result['status'] == 204 and result['body'] is None, 'Owned logout did not complete with 204.')
        result = self.run.request(self.slot + '-exact', 'GET', '/admin/v1/session' if native else '/emby/System/Info', self, purpose='exact', recovery=True)
        self.status['exact_status'] = result['status']
        require(result['complete'] and result['status'] == 401, 'The exact owned credential remains accepted.')
        self.closed = True


def matrix_cases(phase, item_ids):
    hidden = phase == 'restricted'
    b, a = '/emby/Users/' + ACTORS['B'], '/emby/Users/' + ACTORS['A']
    return [
        ('movie', 'B', b + '/Items/' + MOVIE, 404 if hidden else 200, 'movie'),
        ('parent', 'B', b + '/Items?' + urlencode({'ParentId': LIBRARIES['movies'], 'Recursive': 'true', 'IncludeItemTypes': 'Movie', 'Fields': 'Path', 'Limit': 64}), 404 if hidden else 200, 'movie_list'),
        ('views', 'B', b + '/Views', 200, 'views'),
        ('ids', 'B', b + '/Items?' + urlencode({'Ids': MOVIE}), 200, 'empty_list' if hidden else 'movie_list'),
        ('control-a', 'A', a + '/Items/' + MOVIE, 200, 'movie'),
        ('foreign-a', 'A', b + '/Items/' + MOVIE, 403, 'foreign'),
        ('foreign-b', 'B', a + '/Items/' + MOVIE, 403, 'foreign'),
        ('similar', 'B', '/emby/Items/' + MOVIE + '/Similar?' + urlencode({'UserId': ACTORS['B']}), 404 if hidden else 200, 'similar'),
        ('theme', 'B', '/emby/Items/' + MOVIE + '/ThemeMedia?' + urlencode({'UserId': ACTORS['B']}), 404 if hidden else 200, 'theme'),
        ('movie-sf', 'B', b + '/Items/' + MOVIE + '/SpecialFeatures', 404 if hidden else 200, 'empty_array'),
        ('movie-lt', 'B', b + '/Items/' + MOVIE + '/LocalTrailers', 404 if hidden else 200, 'empty_array'),
        ('positive', 'B', b + '/Items/' + POSITIVE, 200, 'positive'),
        ('positive-sf', 'B', b + '/Items/' + POSITIVE + '/SpecialFeatures', 200, 'features'),
        ('positive-lt', 'B', b + '/Items/' + POSITIVE + '/LocalTrailers', 200, 'trailers'),
    ]


def validate_matrix(response, case, phase, item_ids):
    _, _, route, status, kind = case
    require(response['complete'] and response['failure_type'] is None and response['status'] == status, 'A matrix response did not complete with the expected status.')
    body = response['body']
    if status in (403, 404):
        require(error_code(body, route) == ('access_denied' if status == 403 else 'not_found'), 'The denial is not the intended authority/visibility error.')
    elif kind in ('movie', 'positive'):
        expected = MOVIE if kind == 'movie' else POSITIVE
        require(isinstance(body, dict) and body.get('Id') == expected and body.get('Type') == 'Movie', 'A matrix detail identifies another item.')
    elif kind in ('movie_list', 'empty_list', 'views'):
        expected = ([MOVIE] if kind == 'movie_list' else [] if kind == 'empty_list' else
                    sorted(set(LIBRARIES.values()) - ({LIBRARIES['movies']} if phase == 'restricted' else set())))
        require(isinstance(body, dict) and isinstance(body.get('Items'), list) and len(body['Items']) == len(expected) and
                sorted(row.get('Id') for row in body['Items']) == sorted(expected) and body.get('TotalRecordCount') == len(expected),
                'A matrix list lost its exact permitted membership.')
    elif kind in ('empty_array', 'features', 'trailers'):
        expected = [] if kind == 'empty_array' else [item_ids[key] for key in ('alpha', 'deleted', 'zeta')] if kind == 'features' else [item_ids['trailer']]
        require(isinstance(body, list) and [row.get('Id') for row in body] == expected, 'An Extras control changed membership or ordering.')
    else:
        require(isinstance(body, dict), 'An auxiliary matrix response is not a JSON object.')


class Run:
    def __init__(self, args):
        self.args, self.op, self.observer, self.ext, self.schema_profile = args, None, None, None, None
        self.state = self.before = self.after = self.media = self.receipt = self.proof = None
        self.state_bytes = self.lock = self.output_identity = None
        self.actors, self.records, self.secrets, self.intents = {}, {}, set(), set()
        self.allowed_reads = set()
        self.sequence, self.response_bytes, self.recovery_bytes, self.cleanup_bytes = 0, 0, 0, 0
        self.request_counts = {'business': 0, 'recovery': 0, 'cleanup': 0}
        self.journal_failures, self.errors, self.matrix_results = [], [], {}
        self.phase = 'preflight'
        self.original = self.restricted = self.restored = None
        self.restore_body = self.restore_reservation = None
        self.restore_authorized = False
        self.restrict_sent = self.restore_sent = False
        self.restrict_status = self.restore_status = None
        self.restore_result = 'not_required'
        self.deadline, self.recovery_deadline, self.cleanup_deadline = time.monotonic() + 300, None, None

    def command(self, arguments, text=None, environment=None, timeout=30):
        values = [str(value) for value in arguments]
        if values and Path(values[0]).name == 'systemctl':
            require(values[1:2] == ['show'], 'This operator cannot change a service.')
        result = subprocess.run(values, input=text, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=timeout, check=False, env=environment or self.op.ENV)
        require(result.returncode == 0, 'A read-only helper command failed.')
        return result.stdout.strip()

    def private(self, name, value):
        require(re.fullmatch('[a-z0-9-]+[.]json', name), 'An evidence filename escaped its scope.')
        self.op.create(OUTPUT / 'private' / name, value)

    def journal(self, name, value, best_effort=False):
        try:
            self.private(name, value)
        except Exception as error:
            self.journal_failures.append({'name': name, 'failure_type': type(error).__name__})
            if not best_effort:
                raise

    def check(self, recovery=False, cleanup=False):
        deadline = self.cleanup_deadline if cleanup else self.recovery_deadline if recovery and self.recovery_deadline is not None else self.deadline
        require(time.monotonic() < deadline, 'The bounded request window expired.')
        for path, expected in self.sources:
            protected(path, expected, 1 << 20)
        require(self.op.read(self.op.STATE_FILE) == self.state_bytes, 'The pinned fixture state changed.')
        self.op.verify_fixture_directories(self.state)
        self.op.verify_database(self.state)
        require(self.op.verify_service(self.state) == self.state['process'], 'The exact candidate process changed.')
        if self.output_identity is not None and not recovery:
            require(self.op.directory(OUTPUT, 0, 0o700, 0) == self.output_identity, 'The new evidence directory changed identity.')

    def load(self):
        a = self.args
        require(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0 and os.environ.get('SSH_CONNECTION'), 'Use root SSH on test-env only.')
        require(a.fixture_operator_path == WORK / 'prepare-client-fixture.py' and a.state_sha256 == STATE_SHA, 'The current operator/state pin differs.')
        self.op = op = source(a.fixture_operator_path, a.fixture_operator_sha256, 'fixture_reader')
        self.observer = observer = source(a.positive_observer, a.positive_observer_sha256, 'accepted_positive_scope')
        self.ext = source(a.extension_operator, a.extension_operator_sha256, 'media_receipt_reader')
        require(a.fixture_operator_sha256 == observer.OPERATOR_SHA and op.WORK == WORK and op.PORT == 18198 and op.UNIT == 'goby-client-m3e.service', 'A selected helper targets another candidate.')
        op.command = self.command
        op.host_inputs(initial=False)
        self.lock = os.open(op.LOCK, os.O_RDONLY | os.O_NOFOLLOW)
        require(op.identity(os.fstat(self.lock)) == op.identity(op.regular(op.LOCK)), 'The fixture lock changed.')
        fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        require(not op.exists(OUTPUT), 'This evidence root already exists; no retry or adoption is permitted.')
        self.state_bytes = op.read(op.STATE_FILE)
        require(sha(self.state_bytes) == STATE_SHA, 'The selected current state changed.')
        self.state = op.precise_json(self.state_bytes)
        require(self.state.get('schema') == 27 and self.state.get('phase') == 'ready' and self.state.get('stage') == 'complete' and
                self.state.get('binary_sha256') == SOURCE_SHA and self.state.get('process') ==
                {'pid': 748513, 'start_ticks': 6996875, 'boot_id': '6bdfc486-7bc8-412f-82b5-70095a09dde7'} and
                self.state['viewer_id'] == ACTORS['B'] and self.state['added_viewer']['user_id'] == ACTORS['A'], 'The current source32 ordinary fixture identity differs.')
        ledger_record = {'path': str(a.positive_ledger), 'sha256': a.positive_ledger_sha256}
        self.proof, _ = observer.authority(ledger_record, live=False)
        ledger = observer.read_record(ledger_record)
        self.receipt = observer.read_record(ledger['profile'])
        require(ledger['profile']['sha256'] == PROFILE_SHA and ledger['fixture_state']['sha256'] == STATE_SHA and self.proof['actors'] == ACTORS and
                self.proof['source_manifest_sha256'] == MANIFEST_SHA and self.proof['binary_sha256'] == SOURCE_SHA and
                self.proof['movie']['id'] == POSITIVE and self.receipt['profile_version'] == 3, 'The final positive profile chain changed.')
        self.item_ids = self.receipt['item_ids']
        self.sources = [(Path(__file__).absolute(), a.script_sha256), (a.fixture_operator_path, a.fixture_operator_sha256),
                        (a.positive_observer, a.positive_observer_sha256), (a.extension_operator, a.extension_operator_sha256)]
        old_source = self.receipt['continuation']['origin_profile_source']
        self.schema_profile = source(Path(old_source['path']), old_source['sha256'], 'complete_schema_reader')
        self.sources.append((Path(old_source['path']), old_source['sha256']))
        require(a.extension_operator_sha256 == self.state['media_root_extension']['script_sha256'], 'The selected media reader differs from the accepted extension.')
        product = self.state['upgrade']['schema_artifacts']['product_verification']
        require(op.equal_json(op.verify_product_upgrade(Path(self.state['upgrade']['source']), SOURCE_SHA, Path(self.proof['source']), MANIFEST_SHA,
            Path(product['report_path']), product['report_sha256'], 27), product), 'The complete source32 product proof changed.')
        self.check()
        after = observer.read_record({'path': str(a.positive_after), 'sha256': a.positive_after_sha256})
        require(a.positive_after_sha256 == AFTER_SHA, 'The selected latest accepted after-observation differs.')
        observer.observation_rows(after, 'after', self.proof)
        comparison = observer.read_record({'path': str(a.positive_comparison), 'sha256': a.positive_comparison_sha256})
        ui = observer.read_record({'path': str(a.positive_report), 'sha256': a.positive_report_sha256})
        require(comparison.get('database_scope_result') == 'passed' and comparison.get('ui_client_acceptance') is True and
                comparison['input_sha256']['after'] == AFTER_SHA and comparison['input_sha256']['report'] == a.positive_report_sha256 and
                comparison['input_sha256']['expected_ledger'] == a.positive_ledger_sha256 and ui.get('result') == 'passed' and ui.get('client_acceptance') is True,
                'The accepted positive UI/comparison evidence differs.')
        self.before = op.preservation_snapshot(self.state, 27)
        self.schema_profile.validate_structure(op, self.before, self.state)
        baseline = observer.read_record({'path': str(a.baseline_snapshot), 'sha256': a.baseline_snapshot_sha256})
        require(a.baseline_snapshot_sha256 == BASELINE_SHA, 'The accepted complete baseline record differs.')
        compare_fixed_snapshot(op, baseline, self.before)
        require(observer.same(observer.projection(self.before['database']['tables'], ACTORS, POSITIVE), after['rows']), 'The fresh full snapshot is not continuous with the accepted positive after-observation.')
        quiescent(self.before)
        self.media = self.ext.media_witness(op, self.state)
        self.user_rows = {row['id']: row for row in self.before['database']['tables']['users']}

    def approved(self, label, method, path, actor, body, purpose, recovery):
        require(actor is self.actors.get(actor.slot) and label not in self.intents and re.fullmatch('[A-Za-z0-9-]{1,96}', label), 'A request is repeated or has another actor.')
        require(path.startswith('/') and not path.startswith('//') and '://' not in path and '#' not in path and all(32 <= ord(c) < 127 for c in path), 'A route is not local and canonical.')
        budget = 'cleanup' if purpose in ('logout', 'exact') else 'recovery' if recovery else 'business'
        require(self.sequence < MAX_REQUESTS and self.request_counts[budget] < {'business': BUSINESS_REQUESTS, 'recovery': 30, 'cleanup': 6}[budget],
                'The independent request budget is exhausted.')
        native = actor.slot == 'admin'
        if purpose == 'login':
            expected = {'Name': actor.account['username'], 'Password': actor.account['password']} if native else {'Username': actor.account['username'], 'Pw': actor.account['password']}
            require(not actor.token and method == 'POST' and path == ('/admin/v1/session' if native else '/emby/Users/AuthenticateByName') and body == expected,
                    'Login left its one fresh credential scope.')
        else:
            require(actor.proven, 'An unproven credential cannot be used.')
            if purpose in ('logout', 'exact'):
                expected_method = ('DELETE' if native else 'POST') if purpose == 'logout' else 'GET'
                expected_path = '/admin/v1/session' if native else '/emby/Sessions/Logout' if purpose == 'logout' else '/emby/System/Info'
                require(recovery and method == expected_method and path == expected_path and body is None, 'Cleanup left the exact owned credential scope.')
            elif purpose in ('restrict', 'restore'):
                require(native and not actor.closed and method == 'PUT' and path == '/admin/v1/users/' + ACTORS['B'] and
                        body == (self.restricted if purpose == 'restrict' else self.restore_body) and
                        not (self.restrict_sent if purpose == 'restrict' else self.restore_sent), 'A policy mutation is unowned, repeated or not the exact body.')
            else:
                require(not actor.closed and method == 'GET' and body is None and (actor.slot, path) in self.allowed_reads, 'An unapproved business read was requested.')

    def request(self, label, method, path, actor, body=None, *, purpose='read', recovery=False):
        cleanup = purpose in ('logout', 'exact')
        if cleanup and self.cleanup_deadline is None:
            self.cleanup_deadline = time.monotonic() + 120
        self.check(recovery, cleanup)
        self.approved(label, method, path, actor, body, purpose, recovery)
        self.sequence += 1
        budget = 'cleanup' if cleanup else 'recovery' if recovery else 'business'
        self.request_counts[budget] += 1
        self.intents.add(label)
        stem = f'{self.sequence:04d}-{label.lower()}'
        safe_intent = {'method': method, 'path': path, 'actor': actor.slot, 'purpose': purpose, 'body': None if purpose == 'login' else body}
        if purpose == 'restore':
            require(self.op.load(OUTPUT / 'restore-reservation.json') == self.restore_reservation and self.restore_authorized, 'The durable conditional restoration reservation changed.')
        self.journal(stem + '-intent.json', safe_intent, best_effort=recovery)
        headers = {'Accept': 'application/json', 'Accept-Encoding': 'identity', 'Origin': self.op.PUBLIC}
        if actor.slot == 'admin' and purpose != 'login':
            headers.update(Cookie=actor.cookie, **{'X-CSRF-Token': actor.csrf})
        elif actor.slot != 'admin':
            headers['Authorization'] = f'Emby Client="{CLIENT}", Device="Linux Recorder", DeviceId="{actor.device}", Version="1.0"'
            if purpose != 'login':
                headers['X-Emby-Token'] = actor.token
        payload = self.op.canonical_json(body) if body is not None else None
        if payload is not None:
            headers['Content-Type'] = 'application/json'
        connection = http.client.HTTPConnection('127.0.0.1', self.op.PORT, timeout=10)
        result = {'status': None, 'body': None, 'complete': False, 'failure_type': None, 'bytes': 0}
        signal.setitimer(signal.ITIMER_REAL, 12)
        try:
            if purpose == 'restrict':
                self.restrict_sent = True
            elif purpose == 'restore':
                self.restore_sent = True
            connection.request(method, path, payload, headers)
            response = connection.getresponse()
            result['status'] = response.status
            length = response.getheader('Content-Length')
            limit = 64 << 10 if cleanup else 1 << 20
            require(length is None or length.isdigit() and int(length) <= limit, 'A response exceeds its length bound.')
            raw = response.read(limit + 1)
            require(len(raw) <= limit and (length is None or len(raw) == int(length)), 'A response was incomplete or oversized.')
            if cleanup:
                self.cleanup_bytes += len(raw)
                require(self.cleanup_bytes <= 6 * (64 << 10), 'The independent owned-cleanup byte budget is exhausted.')
            elif recovery:
                self.recovery_bytes += len(raw)
                require(self.recovery_bytes <= 8 << 20, 'The independent restoration/cleanup response budget is exhausted.')
            else:
                self.response_bytes += len(raw)
                require(self.response_bytes <= 16 << 20, 'The business response budget is exhausted.')
            mime = response.getheader('Content-Type')
            value = None if not raw else self.op.precise_json(raw) if mime and mime.split(';', 1)[0] == 'application/json' else raw.decode('utf-8')
            result.update(body=value, complete=True, bytes=len(raw), sha256=sha(raw), content_type=mime)
            if purpose == 'login':
                actor.acknowledge(result, response.getheader('Set-Cookie'))
        except Exception as error:
            result['failure_type'] = type(error).__name__
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            connection.close()
            self.records[label] = {'request': {key: value for key, value in safe_intent.items() if key != 'body'}, 'response': result,
                'token_fingerprint': actor.status.get('token_sha256') if purpose != 'login' else None}
            self.journal(stem + '-response.json', self.records[label], best_effort=recovery)
        self.check(recovery, cleanup)
        return result

    def get(self, label, path, actor, recovery=False):
        self.allowed_reads.add((actor.slot, path))
        response = self.request(label, 'GET', path, actor, recovery=recovery)
        require(response['complete'] and response['failure_type'] is None and response['status'] == 200, 'A required native read failed.')
        return response['body']

    def begin(self):
        op = self.op
        self.check()
        fresh = op.preservation_snapshot(self.state, 27)
        compare_fixed_snapshot(op, self.before, fresh)
        self.before = fresh
        OUTPUT.mkdir(mode=0o700)
        (OUTPUT / 'private').mkdir(mode=0o700)
        self.output_identity = op.directory(OUTPUT, 0, 0o700, 0)
        op.create(OUTPUT / 'OWNER.json', {'marker': MARKER, 'identity': self.output_identity, 'state_sha256': STATE_SHA})
        op.create(OUTPUT / 'before-full.json', self.before)
        op.create(OUTPUT / 'media-before.json', self.media)
        self.window = {'before': self.before['database']['metadata']['captured_at']}
        browser, av = op.load(op.BROWSER), op.load(op.AV_BROWSER)
        self.actors = {'admin': Actor(self, 'admin', browser['admin'], self.state['admin_id']),
                       'A': Actor(self, 'A', av['viewer'], ACTORS['A']), 'B': Actor(self, 'B', browser['viewer'], ACTORS['B'])}
        self.secrets.update(actor.account['password'] for actor in self.actors.values())
        existing = {row['reported_device_id'] for row in self.before['database']['tables']['devices']}
        require(all(self.actors[slot].device not in existing for slot in ACTORS), 'An ordinary device is not fresh.')
        self.phase = 'login'

    def bind_authentication(self):
        current = self.op.preservation_snapshot(self.state, 27)
        delta = unchanged_rows(self.op, self.before, current)
        require(all(len(rows) == {'sessions': 3, 'devices': 2, 'activity_entries': 3}.get(table, 0) for table, rows in delta.items()),
                'Login changed unrelated business state or did not create three fresh sessions.')
        for slot, actor in self.actors.items():
            matches = [row for row in delta['sessions'] if row['token_hash'] == '\\x' + actor.status['token_sha256']]
            require(len(matches) == 1 and matches[0]['user_id'] == actor.user_id and matches[0]['kind'] == ('admin' if slot == 'admin' else 'emby') and
                    matches[0]['revoked_at'] is None, 'A fresh acknowledged session is not independently bound to its stored identity.')
            require(actor.session_id is None or actor.session_id == matches[0]['id'], 'An Emby acknowledgement has another stored session ID.')
            actor.session_id = matches[0]['id']
            actor.status['session_id'] = actor.session_id
        self.private('owned-login-bindings.json', {slot: actor.status for slot, actor in self.actors.items()})
        self.op.create(OUTPUT / 'authenticated-full.json', current)
        self.auth_baseline = current

    def native(self, label, slot='B', recovery=False):
        return managed(self.get(label, '/admin/v1/users/' + ACTORS[slot], self.actors['admin'], recovery), ACTORS[slot])

    def audit_owned(self, number):
        actor = self.actors['admin']
        require(actor.session_id and re.fullmatch('[0-9a-f]{32}', actor.session_id) and type(number) is int and 0 < number <= MAX_REVISION,
                'An owned audit lookup lacks a safe exact identity.')
        sql = ("BEGIN READ ONLY; SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY id),'[]'::jsonb) FROM activity_entries a WHERE "
               "action='user.updated' AND resource_kind='user' AND resource_id='" + ACTORS['B'] + "' AND revision=" + str(number) +
               " AND actor_credential_id='" + actor.session_id + "'; COMMIT;")
        rows = self.op.precise_json(self.op.postgres(sql, self.op.ROLE))
        expected_fields = changed_fields(self.original, self.restricted)
        return len(rows) == 1 and all(rows[0].get(key) == value for key, value in {
            'source': 'native', 'actor_kind': 'user', 'actor_id': actor.user_id, 'actor_credential_id': actor.session_id,
            'severity': 'Info', 'affected_count': 1, 'state': '', 'changed_fields': expected_fields}.items())

    def matrix(self, phase, recovery=False):
        self.phase = phase
        result = []
        for case in matrix_cases(phase, self.item_ids):
            label, slot, path, _, _ = case
            actor = self.actors[slot]
            self.allowed_reads.add((slot, path))
            response = self.request(phase + '-' + label, 'GET', path, actor, purpose='matrix', recovery=recovery)
            validate_matrix(response, case, phase, self.item_ids)
            result.append({'case': label, 'actor': slot, 'method': 'GET', 'path': path, 'status': response['status'],
                           'token_fingerprint': actor.status['token_sha256']})
        self.matrix_results[phase] = result

    def restrict(self):
        fresh = self.native('pre-restriction-user')
        require(fresh == self.original, 'B changed after its baseline; do not rebase a concurrent revision.')
        self.restore_reservation = {'marker': MARKER, 'original': self.original, 'restricted': self.restricted,
            'restore_supported_fields': supported(self.original), 'condition': 'fresh owned restricted revision and matching administrator audit',
            'administrator_session_id': self.actors['admin'].session_id, 'maximum_restoration_attempts': 1}
        self.op.create(OUTPUT / 'restore-reservation.json', self.restore_reservation)
        response = self.request('restrict-user', 'PUT', '/admin/v1/users/' + ACTORS['B'], self.actors['admin'], self.restricted, purpose='restrict')
        self.restrict_status = response['status']
        require(response['complete'] and response['failure_type'] is None and response['status'] == 200 and
                isinstance(response['body'], dict) and response['body'].get('CurrentSessionRevoked') is False and
                classify(managed(response['body'], ACTORS['B']), self.original, self.restricted) == 'restricted',
                'Restriction did not have a complete expected native acknowledgement.')
        current = self.native('restriction-readback')
        require(classify(current, self.original, self.restricted) == 'restricted' and self.audit_owned(revision(self.original['Revision']) + 1),
                'The restricted state lacks exact revision and owned transactional audit proof.')

    def restore(self):
        if not self.restrict_sent:
            self.restore_result = 'not_required'
            return
        # An HTTP response may have arrived before its journal write failed.
        if self.restrict_status is None and 'restrict-user' in self.records:
            self.restrict_status = self.records['restrict-user']['response']['status']
        current = self.native('restore-reconcile-user', recovery=True)
        classification = classify(current, self.original, self.restricted)
        number = revision(self.original['Revision'])
        owned = self.audit_owned(number + 1)
        decision = restore_decision(self.restrict_sent, self.restrict_status, self.restore_sent, classification, owned)
        if decision == 'not_committed':
            self.restore_result = 'restriction_not_committed'
            return
        require(decision == 'restore_once', 'Unexpected revision/account state cannot be overwritten during restoration.')
        self.restore_body = copy.deepcopy(self.original)
        # The write uses the fresh returned string, never a fabricated revision.
        self.restore_body['Revision'] = current['Revision']
        self.restore_authorized = True
        response = self.request('restore-user', 'PUT', '/admin/v1/users/' + ACTORS['B'], self.actors['admin'], self.restore_body, purpose='restore', recovery=True)
        self.restore_status = response['status']
        acknowledged = False
        if (response['complete'] and response['failure_type'] is None and response['status'] == 200 and
                isinstance(response['body'], dict) and response['body'].get('CurrentSessionRevoked') is False):
            try:
                acknowledged = classify(managed(response['body'], ACTORS['B']), self.original, self.restricted) == 'restored'
            except RestrictionError:
                pass
        if not acknowledged:
            self.errors.append('restore_response_unconfirmed')
        restored = self.native('restore-readback', recovery=True)
        decision = restore_decision(True, self.restrict_status, True, classify(restored, self.original, self.restricted), owned,
                                    self.audit_owned(number + 2))
        require(decision == 'restored', 'Restoration could not be confirmed without another write.')
        self.restored, self.restore_result = restored, 'confirmed'
        self.matrix('restored', recovery=True)
        require(self.native('control-a-after', 'A', recovery=True) == self.original_a, 'A changed during B restriction.')

    def execute(self):
        for slot in ('admin', 'A', 'B'):
            self.actors[slot].login()
        self.bind_authentication()
        self.original_a = self.native('native-a-baseline', 'A')
        self.original = self.native('native-b-baseline')
        self.restricted = restricted_body(self.original)
        for slot, body in (('A', self.original_a), ('B', self.original)):
            row = self.user_rows[ACTORS[slot]]
            require(body['Revision'] == str(row['management_revision']) and body['Name'] == row['name'] and
                    body['IsAdministrator'] == row['is_administrator'] and body['IsDisabled'] == row['is_disabled'] and
                    body['Policy'] == projected_policy(row['policy']), 'A native baseline differs from the fresh full user row.')
        native_libraries = self.get('native-libraries', '/admin/v1/libraries', self.actors['admin'])
        require(isinstance(native_libraries, dict) and native_libraries.get('TotalRecordCount') == 4 and
                isinstance(native_libraries.get('Items'), list), 'The native library inventory is incomplete.')
        validate_libraries([{'id': row.get('Id')} for row in native_libraries['Items']])
        self.op.create(OUTPUT / 'native-original.json', {'A': self.original_a, 'B': self.original})
        self.matrix('baseline')
        self.restrict()
        self.matrix('restricted')

    def sanitize(self, value):
        if isinstance(value, dict):
            return {key: '[redacted]' if key.lower() in ('password', 'pw', 'token', 'accesstoken', 'cookie', 'authorization', 'csrftoken') else self.sanitize(child)
                    for key, child in value.items()}
        if isinstance(value, list):
            return [self.sanitize(child) for child in value]
        if isinstance(value, str):
            for secret in sorted(self.secrets, key=len, reverse=True):
                value = value.replace(secret, '[redacted]')
            return re.sub(r'(?:https?|wss?)://[^\s"\'<>]+', '[redacted URL]', value)
        return value

    def run(self):
        failure = None
        try:
            self.load()
            if self.args.check_only:
                print(json.dumps({'result': 'preflight_passed', 'http_requests': 0, 'new_evidence_writes': 0,
                    'baseline_counts': {name: len(self.before['database']['tables'][name]) for name in ('items', 'libraries', 'sessions', 'devices', 'activity_entries')},
                    'baseline_b_revision': self.user_rows[ACTORS['B']]['management_revision']}), flush=True)
                return 0
            self.begin()
            try:
                self.execute()
            finally:
                self.recovery_deadline = time.monotonic() + 180
                try:
                    self.restore()
                except Exception as error:
                    self.restore_result = 'recovery_required'
                    self.errors.append('restoration_' + type(error).__name__)
                if self.cleanup_deadline is None:
                    self.cleanup_deadline = time.monotonic() + 120
                for slot in ('B', 'A', 'admin'):
                    try:
                        self.actors[slot].logout()
                    except Exception as error:
                        self.errors.append(slot + '_cleanup_' + type(error).__name__)
            require(self.restore_result == 'confirmed' and set(self.matrix_results) == {'baseline', 'restricted', 'restored'} and
                    all(actor.closed for actor in self.actors.values()) and not self.errors and not self.journal_failures,
                    'The matrix, restoration or owned cleanup is incomplete.')
            self.after = self.op.preservation_snapshot(self.state, 27)
            self.op.create(OUTPUT / 'after-full.json', self.after)
            self.window['after'] = self.after['database']['metadata']['captured_at']
            proof = validate_final(self.op, self.schema_profile, self.before, self.auth_baseline, self.after, self.state,
                                   self.original, self.restricted, self.actors, self.window)
            require(self.op.equal_json(self.ext.media_witness(self.op, self.state), self.media), 'A protected media member changed.')
            self.phase = 'complete'
            snapshots = {name: {'path': str(OUTPUT / filename), 'sha256': self.op.sha(OUTPUT / filename, limit=self.op.MAX_SNAPSHOT_BYTES)}
                         for name, filename in (('before', 'before-full.json'), ('authenticated', 'authenticated-full.json'), ('after', 'after-full.json'))}
            require(self.op.read(self.op.STATE_FILE) == self.state_bytes and self.op.verify_service(self.state) == self.state['process'], 'The candidate changed before report publication.')
            inputs = {name: {'path': str(getattr(self.args, name)), 'sha256': getattr(self.args, name + '_sha256')}
                      for name in ('positive_observer', 'extension_operator', 'positive_ledger', 'positive_after', 'positive_report',
                                   'positive_comparison', 'baseline_snapshot')}
            inputs['fixture_operator'] = {'path': str(self.args.fixture_operator_path), 'sha256': self.args.fixture_operator_sha256}
            inputs['operator'] = {'path': str(Path(__file__).absolute()), 'sha256': self.args.script_sha256}
            self.op.create(OUTPUT / 'report.json', self.sanitize({'marker': MARKER, 'result': 'passed', 'phase': self.phase,
                'candidate': {'state_sha256': STATE_SHA, 'process': self.state['process'], 'binary_sha256': SOURCE_SHA},
                'matrices': self.matrix_results, 'restoration': self.restore_result, 'proof': proof,
                'snapshots': snapshots, 'inputs': inputs,
                'authentication': {slot: actor.status for slot, actor in self.actors.items()}, 'request_count': self.sequence,
                'boundary': 'Candidate API policy matrix only. No UI acceptance or exact pre-run raw Policy equality claim.'}))
        except Exception as error:
            failure = type(error).__name__
            if self.output_identity is not None:
                try:
                    self.op.create(OUTPUT / 'failed-current-full.json', self.op.preservation_snapshot(self.state, 27))
                except Exception:
                    pass
                try:
                    self.op.create(OUTPUT / 'failure.json', {'marker': MARKER, 'result': 'retained_for_review', 'phase': self.phase,
                        'failure_type': failure, 'restoration': self.restore_result, 'errors': self.errors, 'journal_failures': self.journal_failures,
                        'authentication': {slot: actor.status for slot, actor in self.actors.items()}, 'request_count': self.sequence,
                        'restriction_sent': self.restrict_sent, 'restoration_sent': self.restore_sent, 'retry_permitted': False})
                except Exception:
                    pass
        finally:
            if self.lock is not None:
                os.close(self.lock)
                self.lock = None
        print(json.dumps({'result': 'passed' if failure is None else 'retained_for_review', 'phase': self.phase, 'failure_type': failure,
            'restoration': self.restore_result, 'request_count': self.sequence, 'evidence_directory': str(OUTPUT),
            'owned_cleanup': {slot: {key: actor.status[key] for key in ('logout_status', 'exact_status')} for slot, actor in self.actors.items()},
            'journal_failures': self.journal_failures}), flush=True)
        return 0 if failure is None else 1


def unchanged_rows(op, before, after, changed_user=None):
    for key in ('schema', 'runtime_sha256', 'browser_sha256', 'added_viewer_credentials', 'recovery'):
        require(op.equal_json(before[key], after[key]), 'A private/schema value changed.')
    left, right = before['database'], after['database']
    require(set(left['tables']) == set(right['tables']) and op.equal_json(left['catalog'], right['catalog']) and
            left['unsupported'] is right['unsupported'] is False, 'The complete database catalog changed.')
    require(set(left['metadata']) == set(right['metadata']) and instant(left['metadata']['captured_at']) <= instant(right['metadata']['captured_at']) and
            all(op.equal_json(left['metadata'][key], right['metadata'][key]) for key in left['metadata'] if key != 'captured_at'), 'A database identity or capture ordering changed.')
    additions = {}
    for name, rows in left['tables'].items():
        if name == 'users' and changed_user is not None:
            old = {row['id']: row for row in rows}
            new = {row['id']: row for row in right['tables'][name]}
            require(len(old) == len(rows) and len(new) == len(right['tables'][name]) and set(old) == set(new) and
                    all(op.equal_json(value, new[key]) for key, value in old.items() if key != changed_user), 'An unapproved complete user row changed.')
            additions[name] = []
            continue
        old = Counter(op.canonical_json(row) for row in rows)
        new = Counter(op.canonical_json(row) for row in right['tables'][name])
        require(not old - new, 'An old complete row changed: ' + name)
        extra = new - old
        additions[name] = [row for row in right['tables'][name] if extra[op.canonical_json(row)] > 0]
    return additions


def validate_final(op, schema_profile, before, authenticated, after, state, original, restricted, actors, window):
    schema_profile.validate_structure(op, before, state)
    schema_profile.validate_structure(op, after, state)
    additions = unchanged_rows(op, before, after, ACTORS['B'])
    require(all(len(rows) == {'sessions': 3, 'devices': 2, 'activity_entries': 8}.get(table, 0) for table, rows in additions.items()),
            'The final increment exceeds three sessions, two devices and eight owned audits.')
    old_b = next(row for row in before['database']['tables']['users'] if row['id'] == ACTORS['B'])
    new_b = next(row for row in after['database']['tables']['users'] if row['id'] == ACTORS['B'])
    expected_b = copy.deepcopy(old_b)
    expected_b.update(policy=raw_restored_policy(old_b, original), management_revision=old_b['management_revision'] + 2, updated_at=new_b['updated_at'])
    require(op.equal_json(new_b, expected_b) and original['Revision'] == str(old_b['management_revision']) and
            instant(window['before']) <= instant(new_b['updated_at']) <= instant(window['after']), 'B did not restore the exact supported Policy and permitted row delta.')
    auth_at_login = {row['id']: row for row in authenticated['database']['tables']['sessions']}
    devices_at_login = {row['id']: row for row in authenticated['database']['tables']['devices']}
    expected_events, new_ids = [], set()
    for slot, actor in actors.items():
        require(actor.proven and actor.closed and [actor.status[key] for key in ('login_status', 'logout_status', 'exact_status')] == [200, 204, 401],
                'A new owned actor did not close with exact evidence.')
        rows = [row for row in additions['sessions'] if row['token_hash'] == '\\x' + actor.status['token_sha256']]
        require(len(rows) == 1, 'An acknowledged token does not bind one new stored session.')
        row = rows[0]
        require(row['id'] == actor.session_id and row['user_id'] == actor.user_id and row['kind'] == ('admin' if slot == 'admin' else 'emby') and
                row['id'] not in new_ids and row['revoked_at'] is not None and row['client_capabilities'] == {}, 'A new actor session has an unexpected identity.')
        new_ids.add(row['id'])
        original_session = auth_at_login[row['id']]
        require(all(op.equal_json(value, row[key]) for key, value in original_session.items() if key not in ('last_seen_at', 'revoked_at')) and
                instant(original_session['last_seen_at']) <= instant(row['last_seen_at']) <= instant(window['after']) and
                instant(window['before']) <= instant(row['revoked_at']) <= instant(window['after']), 'A new session changed outside Touch/revocation fields.')
        if slot == 'admin':
            require(row['device_registry_id'] is None and row['client_name'] == 'Goby Dashboard' and row['device_id'] == 'goby-dashboard' and
                    row['last_seen_at'] == original_session['last_seen_at'], 'The native actor acquired a device or changed its login timestamp.')
        else:
            matches = [device for device in additions['devices'] if device['id'] == row['device_registry_id']]
            require(len(matches) == 1, 'An ordinary actor lacks one new device registry row.')
            device = matches[0]
            require(device['reported_device_id'] == actor.device and device['last_user_id'] == actor.user_id and
                    device['reported_name'] == row['device_name'] == 'Linux Recorder' and device['app_name'] == row['client_name'] == CLIENT and
                    device['app_version'] == row['client_version'] == '1.0' and device['ip_address'] == '127.0.0.1' and
                    device['custom_name'] is None and device['deleted_at'] is None and device['revision'] == 1,
                    'A new ordinary device differs from its owned client metadata.')
            initial = devices_at_login[device['id']]
            require(all(op.equal_json(value, device[key]) for key, value in initial.items() if key != 'last_seen_at') and
                    instant(initial['last_seen_at']) <= instant(device['last_seen_at']) <= instant(window['after']), 'A new device changed outside its permitted activity timestamp.')
        source_name = 'native' if slot == 'admin' else 'emby'
        expected_events.extend((action, source_name, 'user', actor.user_id, actor.session_id, 'session', actor.session_id, 0, ())
                               for action in ('session.login', 'session.revoked'))
    administrator = actors['admin']
    fields = tuple(changed_fields(original, restricted))
    for number in (revision(original['Revision']) + 1, revision(original['Revision']) + 2):
        expected_events.append(('user.updated', 'native', 'user', administrator.user_id, administrator.session_id, 'user', ACTORS['B'], number, fields))
    event_keys = ('action', 'source', 'actor_kind', 'actor_id', 'actor_credential_id', 'resource_kind', 'resource_id', 'revision')
    actual_events = [tuple(row[key] for key in event_keys) + (tuple(row['changed_fields']),) for row in additions['activity_entries']]
    require(Counter(actual_events) == Counter(expected_events) and all(row['severity'] == 'Info' and row['affected_count'] == 1 and row['state'] == '' and
            instant(window['before']) <= instant(row['created_at']) <= instant(window['after']) for row in additions['activity_entries']), 'The audit delta is not exactly six session and two B update facts.')
    old_sequences, new_sequences = before['database']['sequences'], after['database']['sequences']
    require(set(old_sequences) == set(new_sequences), 'The sequence population changed.')
    for name, old in old_sequences.items():
        if name in ('devices_id_seq', 'activity_entries_id_seq'):
            table, count = ('devices', 2) if name == 'devices_id_seq' else ('activity_entries', 8)
            first = old['last_value'] + int(old['is_called'])
            require(new_sequences[name] == {'last_value': first + count - 1, 'is_called': True} and
                    sorted(row['id'] for row in additions[table]) == list(range(first, first + count)), 'An owned sequence increment differs.')
        else:
            require(op.equal_json(old, new_sequences[name]), 'An unrelated sequence changed.')
    return {'old_rows_preserved_except_declared_b_user_fields': True, 'raw_policy_matches_exact_merge': True,
        'b_revision_before': original['Revision'], 'b_revision_after': str(new_b['management_revision']), 'new_sessions': 3, 'new_devices': 2,
        'new_session_audits': 6, 'new_user_update_audits': 2, 'old_play_userdata_references_encoding_unchanged': True,
        'same_token_fingerprints': {slot: actor.status['token_sha256'] for slot, actor in actors.items() if slot != 'admin'}}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--script-sha256', required=True)
    parser.add_argument('--state-sha256', required=True)
    for name in ('fixture-operator-path', 'positive-observer', 'extension-operator', 'positive-ledger', 'positive-after',
                 'positive-report', 'positive-comparison', 'baseline-snapshot'):
        parser.add_argument('--' + name, type=Path, required=True)
        parser.add_argument('--' + name.removesuffix('-path') + '-sha256', required=True)
    parser.add_argument('--check-only', action='store_true')
    args = parser.parse_args()
    for key, value in vars(args).items():
        if key.endswith('_sha256'):
            require(re.fullmatch('[0-9a-f]{64}', value), 'An explicit digest is malformed.')
    os.umask(0o077)
    signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(TimeoutError('A bounded HTTP operation expired.')))
    return Run(args).run()


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except Exception as error:
        print(json.dumps({'result': 'retained_for_review', 'failure_type': type(error).__name__, 'retry_permitted': False}), file=sys.stderr)
        raise SystemExit(1)
