#!/usr/bin/env python3
"""Prepare or inspect one strictly receipted nonempty Extras candidate profile.

The legacy empty-profile helper remains unchanged and is not the inspector for
the resulting profile. Only one new Movies library and its returned normal scan
job may be created. No service/environment, account, policy, preference,
PlaybackInfo, playstate, or media-file mutation is supported. Inspection is a
setup-point proof; later client history requires a separate explicit ledger.
"""

from __future__ import annotations

import argparse
import copy
import fcntl
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import secrets
import signal
import subprocess
import sys
import time
import types
from urllib.parse import urlencode

sys.dont_write_bytecode = True
WORK = Path('/opt/goby-test/exec-work-m3e')
OUTPUT = WORK / 'client-special-features-fixture-v1'
INSPECTION = WORK / 'client-special-features-inspection-v1'
MARKER = 'goby-client-special-features-fixture-v1'
PROFILE_KEY = 'special_features_profile'
JSON_LIMIT, JSON_TOTAL = 2 << 20, 16 << 20
FULL_TOTAL, RANGE_TOTAL, CLEANUP_TOTAL = 8 << 20, 16 << 10, 256 << 10
CLIENT = 'Goby Special Features Fixture Recorder'
UPGRADE_REPORT_KEYS = {'id', 'phase', 'from_sha256', 'to_sha256', 'old_process', 'new_process', 'rollback_binary',
    'evidence_directory', 'protocol_version', 'product_version', 'preservation', 'after_preservation',
    'from_schema', 'to_schema', 'schema_artifacts'}


class SetupError(Exception):
    """An explicit new-fixture identity, intent or preservation check failed."""


def require(value, message):
    if not value:
        raise SetupError(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def source_bytes(path, expected):
    require(path.is_absolute() and path.is_relative_to(WORK) and '..' not in path.parts, 'A source path is outside the protected workspace.')
    for entry in reversed((path, *path.parents)):
        info = entry.lstat()
        require(not entry.is_symlink() and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022, 'A source path is not protected.')
    require(path.is_file() and path.stat().st_nlink == 1 and path.stat().st_size <= 1 << 20, 'A source file exceeds its bound.')
    raw = path.read_bytes()
    require(sha(raw) == expected, 'A selected reviewed source changed.')
    return raw


def module(path, expected, name):
    raw = source_bytes(path, expected)
    result = types.ModuleType(name)
    result.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), result.__dict__)
    return result


def create_body(profile):
    return {'Name': profile.LIBRARY_NAME, 'CollectionType': 'movies', 'Paths': [profile.ROOT], 'Scan': False}


def validate_extension_chain(op, state, extension, extension_report, upgraded, upgrade_report, args):
    current = {'pid': args.candidate_pid, 'start_ticks': args.candidate_start_ticks, 'boot_id': args.candidate_boot_id}
    require(state.get('marker') == op.MARKER and state.get('phase') == 'ready' and state.get('stage') == 'complete' and
            state.get('schema') == 27 and state.get('process') == current and state.get('binary_sha256') == args.candidate_sha256 and
            not any(state.get(key) for key in ('start_pending', 'credentials_pending', 'binary_pending', 'unit_pending', 'directory_pending')),
            'The candidate is not the explicitly pinned ready schema27 process.')
    projection = upgrade_report.get('upgrade', {})
    require(op.equal_json(state.get('upgrade'), upgraded) and upgraded.get('phase') == 'complete' and upgraded.get('to_schema') == 27 and
            upgraded.get('to_sha256') == args.candidate_sha256 and upgrade_report.get('result') == 'ready' and
            upgrade_report.get('marker') == op.MARKER and upgrade_report.get('phase') == 'ready' and upgrade_report.get('schema') == 27 and
            upgrade_report.get('process') == upgraded.get('new_process') and upgrade_report.get('binary_sha256') == args.candidate_sha256 and
            isinstance(projection, dict) and set(projection) == UPGRADE_REPORT_KEYS and
            all(op.equal_json(value, upgraded.get(key)) for key, value in projection.items()), 'The historical upgrade proof changed.')
    record = state.get('media_root_extension', {})
    require(record.get('marker') == 'goby-client-special-features-root-extension-v1' and record.get('phase') == 'complete' and
            record.get('added_root') == extension.get('added_root') == '/opt/goby-fixtures/client-special-features-m3e-v1/Movies' and
            record.get('receipt_path') == str(args.extension_dir / 'completed.json') and
            record.get('receipt_sha256') == args.extension_completed_sha256 and extension.get('phase') == 'complete' and
            extension.get('marker') == 'goby-client-special-features-root-extension-v1' and extension.get('schema') == 27 and
            extension.get('script_sha256') == args.extension_operator_sha256 and extension.get('operator_sha256') == args.operator_sha256 and
            extension.get('upgrade_completed_sha256') == args.upgrade_completed_sha256 and
            extension.get('upgrade_report_sha256') == args.upgrade_report_sha256 and
            extension.get('old_process') == upgraded.get('new_process') and extension.get('new_process') == record.get('new_process') == current and
            extension.get('binary_sha256') == args.candidate_sha256 and extension.get('new_runtime_sha256') == state.get('runtime_sha256') and
            extension.get('old_runtime_sha256') == upgraded.get('after_preservation', {}).get('runtime_sha256') and
            extension.get('old_database_rows_and_sequences_preserved') is True and extension.get('other_runtime_bytes_preserved') is True and
            extension.get('credentials_recovery_and_all_media_preserved') is True and extension.get('http_requests') == 0 and
            extension.get('library_creates') == 0 and extension.get('scan_dispatches') == 0 and
            extension_report.get('result') == 'passed' and op.equal_json(extension_report.get('completed'), extension),
            'The exact root-extension process/runtime/preservation chain is incomplete.')


class Actor:
    def __init__(self, job, role, user):
        self.job, self.role, self.user = job, role, user
        self.token = self.cookie = self.csrf = self.session_id = None
        self.proven = self.closed = False
        self.status = {'login_status': None, 'logout_status': None, 'exact_status': None}

    def login(self):
        if self.role == 'admin':
            response = self.job.request('admin-login', 'POST', '/admin/v1/session', self,
                                        {'Name': self.user['username'], 'Password': self.user['password']}, login=True)
        else:
            response = self.job.request('viewer-login', 'POST', '/emby/Users/AuthenticateByName', self,
                                        {'Username': self.user['username'], 'Pw': self.user['password']}, login=True)
        require(response['status'] == 200 and self.proven, 'The new actor login identity was not proven.')
        self.status['login_status'] = 200

    def acknowledge(self, status, value, cookie):
        if self.role == 'admin':
            match = re.fullmatch(r'goby_session=([A-Za-z0-9_-]{43})', (cookie or '').split(';', 1)[0])
            if match:
                self.token, self.cookie = match[1], match[0]
            user = value.get('User', {}) if isinstance(value, dict) else {}
            csrf = value.get('CSRFToken') if isinstance(value, dict) else None
            self.proven = bool(status == 200 and self.token and user.get('Id') == self.job.state['admin_id'] and
                user.get('Name') == self.user['username'] and user.get('IsAdministrator') is True and user.get('IsDisabled') is False and
                isinstance(csrf, str) and 1 <= len(csrf) <= 256 and all(32 <= ord(character) < 127 for character in csrf))
            if self.proven:
                self.csrf = csrf
                self.job.secrets.add(csrf)
        else:
            if isinstance(value, dict) and isinstance(value.get('AccessToken'), str) and re.fullmatch('[A-Za-z0-9_-]{43}', value['AccessToken']):
                self.token = value['AccessToken']
            user = value.get('User', {}) if isinstance(value, dict) else {}
            session = value.get('SessionInfo', {}) if isinstance(value, dict) else {}
            self.proven = bool(status == 200 and self.token and value.get('ServerId') == self.job.state['server_id'] and
                user.get('Id') == self.job.state['added_viewer']['user_id'] and user.get('Name') == self.user['username'] and
                user.get('Policy', {}).get('IsAdministrator') is False and session.get('UserId') == user.get('Id') and
                session.get('DeviceId') == self.job.device and re.fullmatch('[0-9a-f]{32}', session.get('Id', '')))
            if self.proven:
                self.session_id = session['Id']
        if self.token:
            self.job.secrets.add(self.token)
            self.status['token_sha256'] = sha(self.token.encode())
            if self.session_id:
                self.status['session_id'] = self.session_id
        # I/O failure cannot erase a completed in-memory ownership proof; it
        # still stops business work while permitting exact owned cleanup.
        self.job.journal(self.role + '-login-ack.json', {'body': value, 'cookie': cookie, 'identity_proven': self.proven})

    def logout(self):
        if self.closed:
            return
        if not self.proven:
            require(self.token is None, 'An unproven login acknowledgement remains quarantined; no unknown token is used.')
            return
        route, method = ('/admin/v1/session', 'DELETE') if self.role == 'admin' else ('/emby/Sessions/Logout', 'POST')
        result = self.job.request(self.role + '-logout', method, route, self, cleanup=True)
        self.status['logout_status'] = result['status']
        require(result['status'] == 204 and result['body'] is None, 'The exact new session logout was not acknowledged.')
        route = '/admin/v1/session' if self.role == 'admin' else '/emby/System/Info'
        result = self.job.request(self.role + '-exact-token', 'GET', route, self, cleanup=True)
        self.status['exact_status'] = result['status']
        require(result['status'] == 401, 'The exact logged-out credential remains accepted.')
        self.closed = True


class Setup:
    def __init__(self, args):
        self.args, self.op, self.ext, self.profile, self.state = args, None, None, None, None
        self.state_bytes = self.original_state = self.before = self.after = self.media = self.completed = None
        self.lock = self.output_identity = None
        self.phase, self.sequence, self.json_bytes, self.full_bytes, self.range_bytes, self.cleanup_bytes = 'preflight', 0, 0, 0, 0, 0
        self.intents, self.records, self.secrets, self.actors = set(), {}, set(), {}
        self.journal_failures = []
        self.library_id = self.root_id = self.job_id = None
        self.device = 'goby-m3e-special-features-fixture-' + secrets.token_hex(16)
        self.allowed = set()
        self.deadline = time.monotonic() + 900

    def command(self, arguments, text=None, environment=None, timeout=30):
        args = [str(value) for value in arguments]
        if args and Path(args[0]).name == 'systemctl':
            require(len(args) > 1 and args[1] == 'show', 'This operator cannot change any service.')
        result = subprocess.run(args, input=text, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                check=False, timeout=timeout, env=environment or self.op.ENV)
        require(result.returncode == 0, 'A read-only helper command failed.')
        return result.stdout.strip()

    def private(self, name, value):
        require(re.fullmatch('[a-z0-9-]+[.](json|bin)', name), 'A private evidence name escaped this scope.')
        self.op.create(OUTPUT / 'private' / name, value)

    def journal(self, name, value, *, cleanup=False):
        try:
            self.private(name, value)
        except Exception as error:
            self.journal_failures.append({'name': name, 'failure_type': type(error).__name__, 'cleanup': cleanup})
            # Exact owned cleanup is independent of a failed business journal.
            # Its current process/state/source pins are still checked, and the
            # operation remains failed even if logout and its 401 proof succeed.
            if not cleanup:
                raise

    def checkpoint(self, phase):
        self.phase = phase
        require(self.op.read(self.op.STATE_FILE) == self.state_bytes, 'The candidate state changed outside this operation.')
        self.state['phase'], self.state['stage'] = 'preparing_special_features_fixture', phase
        self.state[PROFILE_KEY] = {'marker': MARKER, 'phase': phase, 'evidence_directory': str(OUTPUT),
            'library_id': self.library_id, 'root_id': self.root_id, 'job_id': self.job_id, 'source_state_sha256': self.args.state_sha256}
        require(self.op.equal_json({key: value for key, value in self.original_state.items() if key not in ('phase', 'stage', PROFILE_KEY)},
                                  {key: value for key, value in self.state.items() if key not in ('phase', 'stage', PROFILE_KEY)}),
                'A base fixture or historical upgrade/extension field changed.')
        self.op.save_state(self.state)
        self.state_bytes = self.op.read(self.op.STATE_FILE)
        self.private('phase-' + phase + '.json', {'phase': phase, 'state_sha256': sha(self.state_bytes),
            'library_id': self.library_id, 'root_id': self.root_id, 'job_id': self.job_id})

    def check(self, *, cleanup=False):
        a, op = self.args, self.op
        require(time.monotonic() < self.deadline, 'The bounded operation deadline expired.')
        source_bytes(Path(__file__).absolute(), a.script_sha256)
        source_bytes(a.operator_path, a.operator_sha256)
        source_bytes(a.extension_operator_path, a.extension_operator_sha256)
        source_bytes(a.profile_path, a.profile_sha256)
        require(op.read(op.STATE_FILE) == self.state_bytes and op.sha(op.RUNTIME) == self.state['runtime_sha256'] and
                op.sha(a.extension_dir / 'completed.json') == a.extension_completed_sha256 and
                op.sha(a.extension_dir / 'report.json') == a.extension_report_sha256,
                'A current state/runtime or root-extension receipt changed.')
        op.verify_fixture_directories(self.state)
        op.verify_database(self.state)
        require(op.verify_service(self.state) == self.state['process'], 'The exact candidate process changed.')
        if not cleanup and self.output_identity is not None:
            require(op.directory(OUTPUT, 0, 0o700, 0) == self.output_identity, 'The exclusive setup directory changed identity.')

    def load(self):
        a = self.args
        require(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0 and os.environ.get('SSH_CONNECTION'), 'Run only through authorized root SSH.')
        source_bytes(Path(__file__).absolute(), a.script_sha256)
        require(a.operator_path == WORK / 'prepare-client-fixture.py', 'The frozen base helper must retain its actual WORK path.')
        self.op = op = module(a.operator_path, a.operator_sha256, 'frozen_empty_fixture_reader')
        self.ext = module(a.extension_operator_path, a.extension_operator_sha256, 'accepted_extension_reader')
        self.profile = module(a.profile_path, a.profile_sha256, 'strict_nonempty_profile')
        require(op.WORK == WORK and op.PORT == 18198 and op.UNIT == 'goby-client-m3e.service' and self.profile.MARKER == MARKER and
                self.ext.OUTPUT == a.extension_dir and self.ext.ADDED_ROOT == self.profile.ROOT, 'The selected code identifies another fixture.')
        op.command = self.command
        op.host_inputs(initial=False)
        self.lock = os.open(op.LOCK, os.O_RDONLY | os.O_NOFOLLOW)
        require(op.identity(os.fstat(self.lock)) == op.identity(op.regular(op.LOCK)), 'The existing fixture lock changed.')
        fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        self.state_bytes = op.read(op.STATE_FILE)
        require(sha(self.state_bytes) == a.state_sha256, 'The explicitly pinned current state changed.')
        self.state = op.precise_json(self.state_bytes)
        self.original_state = copy.deepcopy(self.state)
        ext_raw, ext_report_raw = op.read(a.extension_dir / 'completed.json'), op.read(a.extension_dir / 'report.json')
        require(sha(ext_raw) == a.extension_completed_sha256 and sha(ext_report_raw) == a.extension_report_sha256, 'The explicit extension proof changed.')
        self.extension = op.precise_json(ext_raw)
        upgrade_dir = Path(self.state['upgrade']['evidence_directory'])
        require(upgrade_dir.parent == WORK and re.fullmatch('client-upgrade-[0-9a-f]{32}', upgrade_dir.name) and
                a.upgrade_report.parent == WORK and re.fullmatch('client-fixture-report-[0-9a-f]{24}[.]json', a.upgrade_report.name), 'An upgrade receipt path is outside its grammar.')
        upgraded_raw, upgrade_report_raw = op.read(upgrade_dir / 'completed.json'), op.read(a.upgrade_report)
        require(sha(upgraded_raw) == a.upgrade_completed_sha256 and sha(upgrade_report_raw) == a.upgrade_report_sha256, 'The historical upgrade evidence changed.')
        self.upgraded = op.precise_json(upgraded_raw)
        validate_extension_chain(op, self.state, self.extension, op.precise_json(ext_report_raw), self.upgraded, op.precise_json(upgrade_report_raw), a)
        require(self.upgraded['schema_artifacts']['operator'] == {'path': str(a.operator_path), 'sha256': a.operator_sha256}, 'The completed upgrade used another operator.')
        artifact = self.upgraded['schema_artifacts']
        self.source_binding = {'path': artifact['source'], 'manifest_sha256': artifact['source_manifest_sha256'],
            'full_report_path': artifact['product_verification']['report_path'], 'full_report_sha256': artifact['product_verification']['report_sha256']}
        proof = op.verify_product_upgrade(Path(self.upgraded['source']), a.candidate_sha256, Path(self.source_binding['path']),
            self.source_binding['manifest_sha256'], Path(self.source_binding['full_report_path']), self.source_binding['full_report_sha256'], 27)
        require(op.equal_json(proof, artifact['product_verification']) and self.extension['full_report_sha256'] == self.source_binding['full_report_sha256'], 'The full source verification binding changed.')
        require(self.extension['source_manifest_sha256'] == self.source_binding['manifest_sha256'], 'The extension source manifest changed.')
        self.upgrade_binding = {'completed_path': str(upgrade_dir / 'completed.json'), 'completed_sha256': a.upgrade_completed_sha256,
            'report_path': str(a.upgrade_report), 'report_sha256': a.upgrade_report_sha256,
            'old_process': self.upgraded['old_process'], 'new_process': self.upgraded['new_process']}
        self.extension_binding = {'completed_path': str(a.extension_dir / 'completed.json'), 'completed_sha256': a.extension_completed_sha256,
            'report_path': str(a.extension_dir / 'report.json'), 'report_sha256': a.extension_report_sha256,
            **{key: self.extension[key] for key in ('old_process', 'new_process', 'old_runtime_sha256', 'new_runtime_sha256')}}
        self.check()
        self.media = self.ext.media_witness(op, self.state)
        self.current = op.preservation_snapshot(self.state, 27)
        if a.mode == 'prepare':
            require(a.receipt_sha256 is None and a.setup_report_sha256 is None and PROFILE_KEY not in self.state and not op.exists(OUTPUT), 'A prior setup cannot be adopted or retried.')
            op.validate_preservation_snapshot(self.current, 27, self.state)
            self.ext.require_quiescent(self.current, self.state)
            prior = op.precise_json(op.read(a.extension_dir / 'after-full.json', limit=op.MAX_SNAPSHOT_BYTES))
            require(op.sha(a.extension_dir / 'after-full.json', limit=op.MAX_SNAPSHOT_BYTES) == self.extension['after_snapshot_sha256'], 'The extension snapshot changed.')
            op.compare_preservation_snapshots(prior, self.current, 27, 27, self.state)
            tables = self.current['database']['tables']
            require(not any(row['reported_device_id'] == self.device for row in tables['devices']), 'The new recorder device ID already exists.')
            require(tables['item_theme_resources'] == tables['theme_reserved_paths'] == [], 'This new profile does not adopt Theme history.')
            users = {row['id']: row for row in tables['users']}
            for user_id in (self.state['viewer_id'], self.state['added_viewer']['user_id']):
                policy = users[user_id]['policy']
                require(isinstance(policy, dict) and all(key not in policy or policy[key] is True for key in ('EnableAllFolders', 'EnableMediaPlayback')),
                        'Both existing ordinary actors must already have all-folder playback access; this operator cannot change policy.')
            self.before = self.current
        else:
            require(not a.check_only and a.receipt_sha256 is not None and a.setup_report_sha256 is not None,
                    'Inspection requires explicit completed profile and setup report digests.')

    def approved(self, label, method, path, actor, body, login, cleanup, media_mode):
        require(actor.role in ('admin', 'viewer') and not (login and cleanup) and
                (not (login or cleanup) or media_mode is None), 'A request combined incompatible actor or transport scopes.')
        if login:
            expected = '/admin/v1/session' if actor.role == 'admin' else '/emby/Users/AuthenticateByName'
            expected_body = ({'Name': actor.user['username'], 'Password': actor.user['password']} if actor.role == 'admin' else
                             {'Username': actor.user['username'], 'Pw': actor.user['password']})
            require(label == actor.role + '-login' and method == 'POST' and path == expected and body == expected_body and not actor.token,
                    'A login is repeated, redirected or does not use the one selected actor.')
        elif cleanup:
            require(body is None and actor.proven and ((label == actor.role + '-logout' and method == ('DELETE' if actor.role == 'admin' else 'POST') and
                    path == ('/admin/v1/session' if actor.role == 'admin' else '/emby/Sessions/Logout')) or
                    (label == actor.role + '-exact-token' and method == 'GET' and path == ('/admin/v1/session' if actor.role == 'admin' else '/emby/System/Info'))),
                    'Cleanup left the exact owned credential scope.')
        else:
            require(actor.proven and not actor.closed, 'A business request lacks its proven live owned actor.')
            if method == 'POST':
                require(actor.role == 'admin' and ((label == 'create-library' and path == '/admin/v1/libraries' and body == create_body(self.profile)) or
                        (label == 'scan-library' and self.library_id and path == '/admin/v1/libraries/' + self.library_id + '/scan' and body == {})),
                        'A mutation differs from the one new library or returned scan scope.')
            else:
                require(method == 'GET' and body is None and (actor.role, path, media_mode) in self.allowed, 'An unapproved read was requested.')
        require(label not in self.intents and re.fullmatch('[a-z0-9-]{1,96}', label), 'A request label is repeated or malformed.')
        require('://' not in path and path.startswith('/') and not path.startswith('//') and '#' not in path and
                all(32 <= ord(character) < 127 for character in path), 'A route is not local and canonical.')

    def request(self, label, method, path, actor, body=None, *, login=False, cleanup=False, media_mode=None):
        self.check(cleanup=cleanup)
        self.approved(label, method, path, actor, body, login, cleanup, media_mode)
        require(self.sequence < (190 if cleanup else 180), 'The request count budget is exhausted.')
        self.sequence += 1
        self.intents.add(label)
        self.journal(f'{self.sequence:04d}-{label}-intent.json', {'method': method, 'path': path, 'actor': actor.role,
            'body': None if login else body, 'login': login, 'cleanup': cleanup, 'media_mode': media_mode}, cleanup=cleanup)
        headers = {'Accept': '*/*' if media_mode else 'application/json', 'Accept-Encoding': 'identity', 'Origin': self.op.PUBLIC}
        if actor.role == 'admin' and not login:
            headers.update({'Cookie': actor.cookie, 'X-CSRF-Token': actor.csrf})
        if actor.role == 'viewer':
            headers['Authorization'] = f'Emby Client="{CLIENT}", Device="Linux Recorder", DeviceId="{self.device}", Version="1.0"'
            if not login:
                headers['X-Emby-Token'] = actor.token
        if media_mode == 'range':
            headers['Range'] = 'bytes=0-1023'
        payload = self.op.canonical_json(body) if body is not None else None
        if payload is not None:
            headers['Content-Type'] = 'application/json'
        bound = (64 << 10) if cleanup else (4096 if media_mode == 'range' else JSON_LIMIT)
        raw, status, value, mime, response_headers, complete, failure = b'', None, None, None, {}, False, None
        connection = http.client.HTTPConnection('127.0.0.1', self.op.PORT, timeout=12)
        signal.setitimer(signal.ITIMER_REAL, 15)
        try:
            connection.request(method, path, payload, headers)
            response = connection.getresponse()
            status, mime = response.status, response.getheader('Content-Type')
            length = response.getheader('Content-Length')
            require(length is None or length.isdigit() and int(length) <= bound, 'A response length exceeds its bound.')
            raw = response.read(bound + 1)
            require(len(raw) <= bound and (length is None or len(raw) == int(length)), 'A response is incomplete or oversized.')
            if cleanup:
                self.cleanup_bytes += len(raw)
                require(self.cleanup_bytes <= CLEANUP_TOTAL, 'The independent cleanup byte budget is exhausted.')
            elif media_mode == 'full':
                self.full_bytes += len(raw)
                require(self.full_bytes <= FULL_TOTAL, 'The full-media byte budget is exhausted.')
            elif media_mode == 'range':
                self.range_bytes += len(raw)
                require(self.range_bytes <= RANGE_TOTAL, 'The media-range byte budget is exhausted.')
            else:
                self.json_bytes += len(raw)
                require(self.json_bytes <= JSON_TOTAL, 'The JSON byte budget is exhausted.')
            value = None if media_mode or not raw else self.op.precise_json(raw) if mime and mime.lower().split(';', 1)[0] == 'application/json' else raw.decode('utf-8')
            response_headers = {key: response.getheader(key) for key in ('Content-Length', 'Content-Range', 'Accept-Ranges', 'ETag') if response.getheader(key) is not None}
            complete = True
            if login:
                actor.acknowledge(status, value, response.getheader('Set-Cookie'))
        except Exception as error:
            failure = type(error).__name__
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            connection.close()
            record = {'request': {'method': method, 'path': path, 'actor': actor.role, 'media_mode': media_mode},
                'response': {'status': status, 'content_type': mime, 'headers': response_headers, 'bytes': len(raw),
                    'sha256': sha(raw), 'body': value, 'complete': complete, 'failure_type': failure}}
            self.records[label] = record
            self.journal(f'{self.sequence:04d}-{label}-response.json', record, cleanup=cleanup)
        require(complete and failure is None, 'A response did not complete; no request may be retried.')
        self.check(cleanup=cleanup)
        return record['response']

    def get(self, label, path, actor):
        self.allowed.add((actor.role, path, None))
        response = self.request(label, 'GET', path, actor)
        require(response['status'] == 200, 'An expected successful protocol read failed.')
        return response['body']

    def begin(self):
        op = self.op
        OUTPUT.mkdir(mode=0o700)
        (OUTPUT / 'private').mkdir(mode=0o700)
        self.output_identity = op.directory(OUTPUT, 0, 0o700, 0)
        op.create(OUTPUT / 'OWNER.json', {'marker': MARKER, 'identity': self.output_identity, 'source_state_sha256': self.args.state_sha256})
        op.create(OUTPUT / 'before-state.json', self.state_bytes)
        op.create(OUTPUT / 'before-full.json', self.before)
        op.create(OUTPUT / 'media-before.json', self.media)
        self.window = {'before': op.postgres('SELECT clock_timestamp()::text;', op.ROLE)}
        browser, alias = op.load(op.BROWSER), op.load(op.AV_BROWSER)
        self.secrets.update((browser['admin']['password'], browser['viewer']['password'], alias['viewer']['password']))
        self.actors = {'admin': Actor(self, 'admin', browser['admin']), 'viewer': Actor(self, 'viewer', alias['viewer'])}
        self.checkpoint('prepared')

    def scan(self):
        admin = self.actors['admin']
        admin.login()
        listed = self.get('libraries-before', '/admin/v1/libraries', admin)
        expected = {value['id'] for value in self.state['libraries'].values()}
        require(isinstance(listed, dict) and listed.get('TotalRecordCount') == 3 and isinstance(listed.get('Items'), list) and
                {row.get('Id') for row in listed['Items']} == expected and all(row.get('Name') != self.profile.LIBRARY_NAME and
                self.profile.ROOT not in row.get('Paths', []) for row in listed['Items']), 'The native inventory does not prove one unoccupied new library scope.')
        created = self.request('create-library', 'POST', '/admin/v1/libraries', admin, create_body(self.profile))
        value = created['body'].get('Library', {}) if isinstance(created['body'], dict) else {}
        require(created['status'] == 201 and re.fullmatch('[0-9a-f]{32}', value.get('Id', '')) and value['Id'] not in expected and
                value.get('Name') == self.profile.LIBRARY_NAME and value.get('CollectionType') == 'movies' and value.get('Paths') == [self.profile.ROOT],
                'The create response has no exact new owned library acknowledgement.')
        self.library_id = value['Id']
        self.private('library-ack.json', value)
        self.checkpoint('library_acknowledged')
        roots = self.op.precise_json(self.op.postgres("SELECT coalesce(jsonb_agg(to_jsonb(r)),'[]'::jsonb) FROM library_roots r WHERE library_id='" + self.library_id + "';", self.op.ROLE))
        require(len(roots) == 1 and roots[0].get('path') == roots[0].get('allowed_path') == self.profile.ROOT and
                roots[0].get('relative_path') == '.' and re.fullmatch('[0-9a-f]{32}', roots[0].get('id', '')), 'The new library lacks its one exact registered root.')
        self.root_id = roots[0]['id']
        response = self.request('scan-library', 'POST', '/admin/v1/libraries/' + self.library_id + '/scan', admin, {})
        job = response['body'].get('Job', {}) if isinstance(response['body'], dict) else {}
        old_jobs = {row['id'] for row in self.before['database']['tables']['scan_jobs']}
        require(response['status'] == 202 and re.fullmatch('[0-9a-f]{32}', job.get('Id', '')) and job['Id'] not in old_jobs and
                job.get('LibraryId') == self.library_id and job.get('ForceProbe') is False, 'The scan response lacks its exact new normal job acknowledgement.')
        self.job_id = job['Id']
        self.private('scan-ack.json', job)
        self.checkpoint('scan_acknowledged')
        deadline = time.monotonic() + 300
        for number in range(150):
            result = self.get(f'jobs-{number:03d}', '/admin/v1/jobs', admin)
            require(isinstance(result.get('Items'), list) and len(result['Items']) <= 1000, 'The job inventory exceeds its bound.')
            found = [row for row in result['Items'] if row.get('Id') == self.job_id]
            require(len(found) == 1 and found[0].get('LibraryId') == self.library_id and found[0].get('ForceProbe') is False, 'The exact acknowledged scan changed ownership.')
            if found[0].get('Status') not in ('queued', 'pending', 'running'):
                require(found[0].get('Status') == 'completed' and found[0].get('Error') == '' and
                        (found[0].get('Scanned'), found[0].get('Added'), found[0].get('Updated')) == (6, 6, 0), 'The normal scan did not complete exactly six new files without warnings.')
                self.private('scan-result.json', found[0])
                break
            require(time.monotonic() < deadline, 'The single scan exceeded its observation window; it will not be restarted.')
            time.sleep(2)
        else:
            raise SetupError('The single scan did not reach a terminal result.')
        admin.logout()
        require(not self.journal_failures, 'An evidence write failed; business work cannot continue after owned cleanup.')
        self.checkpoint('scan_complete')

    def capture(self):
        current = self.op.preservation_snapshot(self.state, 27)
        rows = [row for row in current['database']['tables']['items'] if row['library_id'] == self.library_id]
        require(len(rows) == 9, 'The new library did not produce exactly its owned nine-item layout.')
        by_path = {row['path']: row for row in rows if row['path']}
        selected = {}
        for key, (relative, kind, name) in self.profile.FILES.items():
            matches = [row for row in rows if row['path'] == self.profile.ROOT + '/' + relative and row['type'] == kind and row['name'] == name]
            require(len(matches) == 1, 'A selected media identity lacks its exact generated path/type/name.')
            selected[key] = matches[0]
        viewer = self.actors['viewer']
        viewer.login()
        base = '/emby/Users/' + self.state['added_viewer']['user_id'] + '/Items/'
        self.api_items = {}
        for key, row in selected.items():
            detail = self.get('detail-' + key, base + row['id'], viewer)
            api_type = 'Trailer' if key == 'trailer' else row['type']
            api_name = 'M3e Special Features Positive - Trailer' if key == 'trailer' else row['name']
            sources = detail.get('MediaSources')
            require(detail.get('Id') == row['id'] and detail.get('Type') == api_type and detail.get('Name') == api_name and
                    detail.get('Path') == row['path'] and detail.get('ParentId') == row['parent_id'] and
                    isinstance(sources, list) and len(sources) == 1 and sources[0].get('Id') == 'mediasource_' + row['id'],
                    'A direct DTO/media-source identity differs from the owned indexed item.')
            self.api_items[key] = {'id': row['id'], 'type': row['type'], 'api_type': api_type, 'name': row['name'], 'api_name': api_name,
                'path': row['path'], 'relative_path': row['relative_path'], 'parent_id': row['parent_id'], 'media_source_id': sources[0]['Id']}
        expected_sf = [selected[key]['id'] for key in ('alpha', 'deleted', 'zeta')]
        for key, family, expected in (('positive', 'SpecialFeatures', expected_sf), ('positive', 'LocalTrailers', [selected['trailer']['id']]),
                                      ('empty', 'SpecialFeatures', []), ('empty', 'LocalTrailers', [])):
            for fields in (False, True):
                path = base + selected[key]['id'] + '/' + family + ('?' + urlencode({'Fields': 'Path,ParentId,MediaSources,MediaStreams'}) if fields else '')
                body = self.get(key + '-' + family.lower() + ('-fields' if fields else '-default'), path, viewer)
                require(isinstance(body, list) and [row.get('Id') for row in body] == expected, 'The actual extras membership/order differs from the positive reference.')
                if fields:
                    require(all(row.get('ParentId') == selected['positive']['id'] for row in body), 'A detailed extra has another owner.')
        counts = self.get('positive-counts', base + selected['positive']['id'] + '?' + urlencode({'Fields': 'ItemCounts'}), viewer)
        require(counts.get('SpecialFeatureCount') == 3 and counts.get('LocalTrailerCount') == 1, 'The positive parent does not report its three features and one trailer.')
        for key in self.profile.KINDS:
            path = '/emby/Videos/' + selected[key]['id'] + '/original.mp4'
            source = self.media['special']['files']['Movies/' + self.profile.FILES[key][0]]
            for mode, expected_status in (('full', 200), ('range', 206)):
                self.allowed.add(('viewer', path, mode))
                response = self.request(key + '-' + mode, 'GET', path, viewer, media_mode=mode)
                require(response['status'] == expected_status and isinstance(response['content_type'], str) and
                        response['content_type'].split(';', 1)[0] == 'video/mp4', 'Original resource delivery failed.')
                if mode == 'full':
                    require(response['bytes'] == source['bytes'] and response['sha256'] == source['sha256'], 'Full resource bytes differ from the immutable fixture.')
                else:
                    source_path = Path(selected[key]['path'])
                    original = self.ext.protected_bytes(source_path, (0o644,), JSON_LIMIT)
                    require(len(original) == source['bytes'] and sha(original) == source['sha256'] and
                            self.ext.file_identity(source_path.lstat()) == source['identity'], 'The exact local resource changed before Range comparison.')
                    first = original[:1024]
                    require(response['bytes'] == 1024 and response['sha256'] == sha(first) and
                            response['headers'].get('Content-Range') == 'bytes 0-1023/' + str(source['bytes']), 'The resource Range response differs from its exact file prefix.')
        viewer.logout()
        require(not self.journal_failures, 'An evidence write failed; the profile cannot complete.')
        self.checkpoint('protocol_complete')

    def candidate(self):
        return {key: self.state[key] for key in ('binary_sha256', 'process', 'runtime_sha256')}

    def source_records(self):
        return {'setup_source': {'path': str(Path(__file__).absolute()), 'sha256': self.args.script_sha256},
                'profile_source': {'path': str(self.args.profile_path), 'sha256': self.args.profile_sha256}}

    def ledger(self, snapshot, path, expected_sha):
        actors = {'primary': self.state['viewer_id'], 'av': self.state['added_viewer']['user_id']}
        selected = set(actors.values())
        names = ('sessions', 'play_sessions', 'user_item_data', 'client_playback_references', 'encoding_jobs')
        tables = snapshot['database']['tables']
        return {'path': str(path), 'sha256': expected_sha, 'scope_user_ids': sorted(selected), 'actors': actors,
            'counts': {name: sum(row['user_id'] in selected for row in tables[name]) for name in names},
            'global_counts': {name: len(tables[name]) for name in names}}

    def finish(self):
        op = self.op
        require(not self.journal_failures and all(actor.closed for actor in self.actors.values()), 'Incomplete evidence or owned cleanup prevents completion.')
        self.window['after'] = op.postgres('SELECT clock_timestamp()::text;', op.ROLE)
        self.after = op.preservation_snapshot(self.state, 27)
        op.create(OUTPUT / 'after-full.json', self.after)
        receipt = {'marker': MARKER, 'phase': 'complete', 'schema': 27, 'profile_version': 1, 'fixture_tag': self.state['tag'],
            'server_id': self.state['server_id'], 'source_state_sha256': self.args.state_sha256, 'candidate': self.candidate(),
            'source': self.source_binding, 'upgrade': self.upgrade_binding, 'extension': self.extension_binding,
            'library_id': self.library_id, 'root_id': self.root_id, 'job_id': self.job_id, 'device_id': self.device, 'client_name': CLIENT,
            'authentication': {role: actor.status for role, actor in self.actors.items()}, 'window': self.window,
            **self.source_records()}
        proof = self.profile.validate_transition(op, self.before, self.after, self.state, receipt, self.media)
        require(op.equal_json(self.ext.media_witness(op, self.state), self.media), 'An original, auxiliary or new media file changed.')
        receipt['item_ids'] = proof['item_ids']
        for key in ('library', 'positive_folder', 'empty_folder'):
            item = next(row for row in self.after['database']['tables']['items'] if row['id'] == proof['item_ids'][key])
            self.api_items[key] = {field: item[field] for field in ('id', 'type', 'name', 'path', 'relative_path', 'parent_id')}
            self.api_items[key].update(api_type=item['type'], api_name=item['name'])
        receipt.update(items=self.api_items, library={'id': self.library_id, 'root_id': self.root_id, 'name': self.profile.LIBRARY_NAME,
            'collection_type': 'movies', 'path': self.profile.ROOT}, users={'admin_id': self.state['admin_id'], 'viewer_id': self.state['viewer_id'],
            'av_user_id': self.state['added_viewer']['user_id']}, ordinary_access={'primary': {'EnableAllFolders': True, 'EnableMediaPlayback': True},
            'av': {'EnableAllFolders': True, 'EnableMediaPlayback': True}}, aliases={
            'primary': {'path': str(op.BROWSER), 'sha256': self.state['browser_sha256'], 'account_key': 'viewer'},
            'av': {'path': str(op.AV_BROWSER), 'sha256': self.state['added_viewer']['browser_sha256'], 'account_key': 'viewer'}})
        receipt['before_snapshot'] = {'path': str(OUTPUT / 'before-full.json'), 'sha256': op.sha(OUTPUT / 'before-full.json', limit=op.MAX_SNAPSHOT_BYTES)}
        receipt['after_snapshot'] = {'path': str(OUTPUT / 'after-full.json'), 'sha256': op.sha(OUTPUT / 'after-full.json', limit=op.MAX_SNAPSHOT_BYTES)}
        receipt['after_ledger'] = self.ledger(self.after, OUTPUT / 'after-full.json', receipt['after_snapshot']['sha256'])
        receipt['proof'] = proof
        op.create(OUTPUT / 'completed.json', receipt)
        receipt_sha = op.sha(OUTPUT / 'completed.json')
        require(op.read(op.STATE_FILE) == self.state_bytes, 'The candidate state changed before final profile publication.')
        self.state[PROFILE_KEY] = {'marker': MARKER, 'phase': 'complete', 'receipt_path': str(OUTPUT / 'completed.json'),
            'receipt_sha256': receipt_sha, 'library_id': self.library_id, 'root_id': self.root_id}
        self.state.update(phase='ready', stage='complete')
        op.save_state(self.state)
        self.state_bytes = op.read(op.STATE_FILE)
        self.completed = receipt
        self.phase = 'complete'
        self.check()
        report = {'marker': 'goby-client-special-features-setup-report-v1', 'result': 'passed', 'phase': 'complete',
            'state_sha256': sha(self.state_bytes), 'receipt_path': str(OUTPUT / 'completed.json'), 'receipt_sha256': receipt_sha,
            **self.source_records(), 'candidate': self.candidate(), 'proof': proof,
            'protocol': {key: value for key, value in self.records.items() if not key.endswith('login')},
            'json_bytes': self.json_bytes, 'full_media_bytes': self.full_bytes, 'range_media_bytes': self.range_bytes,
            'boundary': 'Controlled API setup/DTO/original-byte proof only. No PlaybackInfo, playstate or original-client UI acceptance claim.'}
        op.create(OUTPUT / 'report.json', self.sanitize(report))

    def sanitize(self, value):
        if isinstance(value, dict):
            return {key: '[redacted]' if key.lower() in ('password', 'pw', 'token', 'accesstoken', 'cookie', 'authorization', 'csrftoken', 'stack', 'stacktrace')
                    else self.sanitize(child) for key, child in value.items()}
        if isinstance(value, list):
            return [self.sanitize(child) for child in value]
        if isinstance(value, str):
            for secret in sorted(self.secrets, key=len, reverse=True):
                value = value.replace(secret, '[redacted]')
            return re.sub(r'(?:https?|wss?)://[^\s"\'<>]+', '[redacted URL]', value)
        return value

    def inspect(self):
        a, op = self.args, self.op
        require(not op.exists(INSPECTION), 'This inspection output is already occupied.')
        raw = op.read(OUTPUT / 'completed.json')
        require(sha(raw) == a.receipt_sha256 and self.state.get(PROFILE_KEY, {}).get('receipt_sha256') == a.receipt_sha256 and
                self.state[PROFILE_KEY].get('phase') == 'complete' and self.state[PROFILE_KEY].get('receipt_path') == str(OUTPUT / 'completed.json'),
                'Inspection lacks the exact completed nonempty profile receipt.')
        receipt = op.precise_json(raw)
        require(receipt.get('marker') == MARKER and receipt.get('phase') == 'complete' and receipt.get('candidate') == self.candidate() and
                receipt.get('extension') == self.extension_binding and receipt.get('upgrade') == self.upgrade_binding and
                receipt.get('source') == self.source_binding and receipt.get('profile_source') == self.source_records()['profile_source'] and
                receipt.get('setup_source') == self.source_records()['setup_source'],
                'The current profile lost its source/process/extension identity.')
        snapshots = {}
        for key in ('before_snapshot', 'after_snapshot'):
            expected_path = OUTPUT / ('before-full.json' if key == 'before_snapshot' else 'after-full.json')
            require(receipt[key]['path'] == str(expected_path) and op.sha(expected_path, limit=op.MAX_SNAPSHOT_BYTES) == receipt[key]['sha256'],
                    'A setup preservation snapshot changed.')
            snapshots[key] = op.precise_json(op.read(expected_path, limit=op.MAX_SNAPSHOT_BYTES))
        proof = self.profile.validate_transition(op, snapshots['before_snapshot'], snapshots['after_snapshot'], self.state, receipt, self.media)
        self.profile.validate_structure(op, self.current, self.state)
        prior, current = snapshots['after_snapshot'], self.current
        for key in ('tables', 'sequences', 'catalog', 'unsupported'):
            require(op.equal_json(prior['database'][key], current['database'][key]), 'The current snapshot differs from the setup-point profile; a later client ledger is required.')
        self.profile.old_rows_and_additions(op, prior, current)
        require(op.equal_json(proof, receipt['proof']), 'The completed profile proof differs.')
        require(op.sha(OUTPUT / 'report.json') == a.setup_report_sha256, 'The explicitly pinned setup report changed.')
        setup_report = op.load(OUTPUT / 'report.json')
        require(setup_report.get('result') == 'passed' and setup_report.get('state_sha256') == sha(self.state_bytes) and
                setup_report.get('receipt_sha256') == a.receipt_sha256, 'The setup report does not bind the current state and receipt.')
        INSPECTION.mkdir(mode=0o700)
        op.create(INSPECTION / 'current-full.json', current)
        summary = op.preservation_summary(current)
        report = {'marker': 'goby-client-special-features-inspection-v1', 'result': 'passed', 'phase': 'complete', 'schema': 27,
            'profile_marker': MARKER, 'process': self.state['process'], 'binary_sha256': self.state['binary_sha256'],
            'runtime_sha256': self.state['runtime_sha256'], 'state_sha256': sha(self.state_bytes),
            'receipt_path': str(OUTPUT / 'completed.json'), 'receipt_sha256': a.receipt_sha256,
            'setup_source': receipt['setup_source'], 'profile_source': receipt['profile_source'],
            'source': self.source_binding, 'upgrade': self.upgrade_binding, 'extension': self.extension_binding,
            'profile': {'receipt_path': str(OUTPUT / 'completed.json'), 'receipt_sha256': a.receipt_sha256,
                'library_id': receipt['library_id'], 'root_id': receipt['root_id'], 'item_ids': receipt['item_ids'], 'resource_count': 4, 'reserved_path_count': 3},
            'verification': dict.fromkeys(('complete_schema27', 'exact_nonempty_profile', 'setup_old_rows_preserved', 'current_matches_completed',
                                          'credentials_recovery_media_preserved', 'new_setup_sessions_revoked'), True),
            'inspection': {'table_count': 35, 'item_count': 22, 'library_count': 4, 'database_sha256': summary['database_sha256'],
                'current_snapshot_path': str(INSPECTION / 'current-full.json'), 'current_snapshot_sha256': op.sha(INSPECTION / 'current-full.json', limit=op.MAX_SNAPSHOT_BYTES)},
            'boundary': 'Fresh setup-point profile proof. Later client writes are not automatically accepted against this snapshot.'}
        report.update(current_snapshot_path=report['inspection']['current_snapshot_path'],
                      current_snapshot_sha256=report['inspection']['current_snapshot_sha256'])
        op.create(INSPECTION / 'report.json', report)
        self.phase = 'inspection_complete'

    def run(self):
        failure = None
        try:
            self.load()
            if self.args.mode == 'inspect':
                self.inspect()
            elif self.args.check_only:
                print(json.dumps({'result': 'preflight_passed', 'http_requests': 0, 'new_evidence_writes': 0}), flush=True)
                return 0
            else:
                self.begin()
                try:
                    self.scan()
                    self.capture()
                finally:
                    self.deadline = time.monotonic() + 120
                    for actor in reversed(list(self.actors.values())):
                        try:
                            actor.logout()
                        except Exception:
                            failure = failure or 'OwnedSessionCleanupFailed'
                require(failure is None and not self.journal_failures and all(actor.closed for actor in self.actors.values()),
                        'Owned cleanup or its evidence is incomplete.')
                self.finish()
        except Exception as error:
            failure = failure or type(error).__name__
            if self.output_identity is not None:
                try:
                    observed = self.op.preservation_snapshot(self.state, 27)
                    self.op.create(OUTPUT / 'failed-current-full.json', observed)
                    self.profile.old_rows_and_additions(self.op, self.before, observed)
                    old_rows = True
                except Exception:
                    old_rows = False
                try:
                    self.op.create(OUTPUT / 'failure.json', {'marker': MARKER, 'result': 'retained_for_review', 'phase': self.phase,
                        'failure_type': failure, 'old_rows_preserved': old_rows, 'authentication': {key: value.status for key, value in self.actors.items()},
                        'journal_failures': self.journal_failures,
                        'library_id': self.library_id, 'root_id': self.root_id, 'job_id': self.job_id, 'retry_permitted': False})
                except Exception:
                    pass
        finally:
            if self.lock is not None:
                os.close(self.lock)
                self.lock = None
        destination = INSPECTION if self.args.mode == 'inspect' else OUTPUT
        print(json.dumps({'result': 'passed' if failure is None else 'retained_for_review', 'phase': self.phase,
                          'failure_type': failure, 'report': str(destination / 'report.json'),
                          'journal_failures': self.journal_failures,
                          'owned_cleanup': {role: {key: actor.status[key] for key in ('logout_status', 'exact_status')}
                                            for role, actor in self.actors.items()}}), flush=True)
        return 0 if failure is None else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('prepare', 'inspect'))
    for name in ('script', 'operator', 'extension-operator', 'profile', 'state', 'extension-completed', 'extension-report', 'upgrade-completed', 'upgrade-report', 'candidate'):
        parser.add_argument('--' + name + '-sha256', required=True)
    for name in ('operator-path', 'extension-operator-path', 'profile-path', 'extension-dir', 'upgrade-report'):
        parser.add_argument('--' + name, type=Path, required=True)
    parser.add_argument('--candidate-pid', type=int, required=True)
    parser.add_argument('--candidate-start-ticks', type=int, required=True)
    parser.add_argument('--candidate-boot-id', required=True)
    parser.add_argument('--receipt-sha256')
    parser.add_argument('--setup-report-sha256')
    parser.add_argument('--check-only', action='store_true')
    args = parser.parse_args()
    for key, value in vars(args).items():
        if key.endswith('_sha256') and value is not None:
            require(re.fullmatch('[0-9a-f]{64}', value), 'An explicit digest is malformed.')
    require(args.candidate_pid > 1 and args.candidate_start_ticks > 0 and re.fullmatch('[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}', args.candidate_boot_id), 'The explicit process pin is malformed.')
    os.umask(0o077)
    signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(TimeoutError('A bounded request expired.')))
    return Setup(args).run()


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except Exception as error:
        print(json.dumps({'result': 'retained_for_review', 'failure_type': type(error).__name__, 'retry_permitted': False}), file=sys.stderr)
        raise SystemExit(1)
