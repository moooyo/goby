#!/usr/bin/env python3
"""Revoke only the lost credential from the one failed B Home observation.

The failed browser tree remains immutable. One new native administrator login
may revoke the precisely identified new B session, then log itself out. No
browser, login retry, bulk revocation, policy change, playback or service action
is available. A lost B token is never reconstructed or claimed to have a 401.
"""

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
WORK = Path('/opt/goby-test/exec-work-m3e')
FAILED = WORK / 'client-library-ui-baseline-v1'
ROOT = WORK / 'client-library-ui-session-recovery-01'
MARKER = 'goby-client-library-home-session-recovery-v1'
INPUT_MARKER = 'goby-client-library-home-session-recovery-input-v1'
TARGET_SHA = '9f71d2771f1a0d177041d6815f46d39ecea9a54bf92099a13b89dbef172afb81'
TARGET_ID = '00fb0b833884308ab946e1c40ff6abdd'
HOME_PATH = WORK / 'client-library-ui-py-tool-01/observe-client-library-home.py'
HOME_SHA = 'c8fcc3d5fa6a2cdea7092952d77de2c18d26d67821d4247eb88e54e77c1cda3f'
HOME = None
PATHS = {'controller_report': FAILED / 'report.json', 'before': FAILED / 'before-full.json',
    'after': FAILED / 'after-full.json', 'browser_report': FAILED / 'browser/report.json',
    'worker_terminal': FAILED / 'worker-terminal.json'}
PINS = {'controller_report': 'ba5c314aeb8d408e61ebaac10c69ed743c376a89ab542f326acc9ade8b8cdea8',
    'before': '8534805055cdb18ba1585c7637879ddea9b4d4b5cbd38e6b21ca324b7b43a948',
    'after': '6d00f9cd99313762702c212ffdcc612cf88a426da67cfbcb0d1b403e8ef2d4d8',
    'browser_report': '447297492853d3c69e8fc0ae042f6386177a112f4e3eda817c93678e433c37df',
    'worker_terminal': '65fd1b907e1942102da4637ba5cfa5eb6d20e14a17814600f0e40da23f749753'}


class RecoveryError(Exception):
    """A sanitized ownership or preservation condition failed."""


def require(value, message):
    if not value: raise RecoveryError(message)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def validate_inputs(value):
    require(isinstance(value, dict) and set(value) == {'marker', 'target_token_sha256', *PATHS} and
        value['marker'] == INPUT_MARKER and value['target_token_sha256'] == TARGET_SHA, 'The recovery input scope differs.')
    for key, path in PATHS.items():
        selected, sha256 = HOME.descriptor(value[key])
        require(selected == path and sha256 == PINS[key], 'A recovery artifact is not the fixed failed observation.')


def validate_sequences(before, after, additions, counts):
    left, right = before['database']['sequences'], after['database']['sequences']
    require(set(left) == set(right), 'The sequence inventory changed.')
    for name, old in left.items():
        count = counts.get(name, 0)
        if not count:
            require(HOME.canonical(old) == HOME.canonical(right[name]), 'An unrelated sequence changed.')
            continue
        table = 'activity_entries' if name == 'activity_entries_id_seq' else 'devices'
        first = old['last_value'] + int(old['is_called'])
        require(right[name] == {'last_value': first + count - 1, 'is_called': True} and
            sorted(row['id'] for row in additions[table]) == list(range(first, first + count)), 'A sequence lacks the exact permitted new rows.')


def check_audits(rows, expected, start, end):
    keys = ('action', 'source', 'actor_kind', 'actor_id', 'actor_credential_id', 'resource_kind', 'resource_id')
    require(Counter(tuple(row[key] for key in keys) for row in rows) == Counter(expected) and
        all(row['severity'] == 'Info' and row['affected_count'] == 1 and row['revision'] == 0 and
            row['changed_fields'] == [] and row['state'] == '' and start <= HOME.instant(row['created_at']) <= end for row in rows),
        'The recovery audit increment includes an unapproved event or actor.')


def validate_failed_session(op, restriction, before, failed, browser):
    HOME.validate_baseline(before)
    additions = restriction.unchanged_rows(op, before, failed)
    require(all(len(rows) == {'sessions': 1, 'devices': 1, 'activity_entries': 1}.get(name, 0) for name, rows in additions.items()),
        'The failed browser has changes outside its one B login and device.')
    require(browser.get('result') == 'failed' and browser.get('failure') == 'home_login_proof_incomplete' and
        browser.get('login_proof') is None and browser.get('session_private') is None and
        browser.get('actor', {}).get('login', {}).get('request_count') == 1 and browser['actor']['login'].get('status') == 200 and
        browser['actor'].get('token_fingerprint') == TARGET_SHA, 'The failed browser does not identify the one acknowledged physical login.')
    row = additions['sessions'][0]
    all_matches = [value for value in failed['database']['tables']['sessions'] if value['token_hash'] == '\\x' + TARGET_SHA]
    require(len(all_matches) == 1 and all_matches[0] == row and row['id'] == TARGET_ID and row['user_id'] == HOME.B and
        row['kind'] == 'emby' and row['revoked_at'] is None and
        all(value['id'] != row['id'] and value['token_hash'] != row['token_hash'] for value in before['database']['tables']['sessions']),
        'The target is not one unique unrevoked new B authentication absent before the failed run.')
    start, end = (HOME.instant(snapshot['database']['metadata']['captured_at']) for snapshot in (before, failed))
    require(start <= HOME.instant(row['created_at']) <= HOME.instant(row['last_seen_at']) <= end and
        HOME.instant(row['expires_at']) == HOME.instant(row['created_at']) + dt.timedelta(days=30), 'The failed login time window differs.')
    device = additions['devices'][0]
    require(device['id'] == row['device_registry_id'] and device['reported_device_id'] == row['device_id'] and
        device['last_user_id'] == HOME.B and device['reported_name'] == row['device_name'] and device['app_name'] == row['client_name'] and
        device['app_version'] == row['client_version'] and device['ip_address'] == '127.0.0.1' and device['custom_name'] is None and
        device['deleted_at'] is None and device['revision'] == 1 and
        all(old['reported_device_id'] != row['device_id'] for old in before['database']['tables']['devices']),
        'The failed login has no exact newly registered ordinary device.')
    check_audits(additions['activity_entries'], [('session.login', 'emby', 'user', HOME.B, row['id'], 'session', row['id'])], start, end)
    validate_sequences(before, failed, additions, {'devices_id_seq': 1, 'activity_entries_id_seq': 1})
    require(not any(value.get('auth_session_id') == row['id'] for value in failed['database']['tables']['play_sessions']) and
        not failed['database']['tables']['client_playback_references'] and not failed['database']['tables']['encoding_jobs'],
        'The target has playback, reference or encoding state outside this cleanup scope.')
    return row


def validate_admin_login(op, restriction, before, authenticated, admin_id, token_sha, client):
    additions = restriction.unchanged_rows(op, before, authenticated)
    require(all(len(rows) == {'sessions': 1, 'activity_entries': 1}.get(name, 0) for name, rows in additions.items()),
        'The recovery login changed more than one administrator session and audit.')
    row = additions['sessions'][0]
    require(row['token_hash'] == '\\x' + token_sha and HOME.ID.fullmatch(row['id']) and row['user_id'] == admin_id and row['kind'] == 'admin' and
        row['revoked_at'] is None and row['client_capabilities'] == {} and row['device_registry_id'] is None and
        all(row[key] == client[key] for key in ('client_name', 'device_id', 'device_name', 'client_version')) and
        all(old['id'] != row['id'] and old['token_hash'] != row['token_hash'] for old in before['database']['tables']['sessions']),
        'The new administrator authentication is not precisely owned.')
    start, end = (HOME.instant(snapshot['database']['metadata']['captured_at']) for snapshot in (before, authenticated))
    require(start <= HOME.instant(row['created_at']) <= HOME.instant(row['last_seen_at']) <= end and
        HOME.instant(row['expires_at']) == HOME.instant(row['created_at']) + dt.timedelta(days=1), 'The administrator authentication time window differs.')
    check_audits(additions['activity_entries'], [('session.login', 'native', 'user', admin_id, row['id'], 'session', row['id'])], start, end)
    validate_sequences(before, authenticated, additions, {'activity_entries_id_seq': 1})
    return row


def validate_final(op, restriction, before, authenticated, after, target, admin, revoke):
    require(isinstance(revoke, dict) and set(revoke) == {'SessionId', 'UserId', 'Kind', 'RevokedAt', 'CurrentSessionRevoked'} and
        revoke['SessionId'] == target['id'] and revoke['UserId'] == HOME.B and revoke['Kind'] == 'emby' and
        revoke['CurrentSessionRevoked'] is False, 'The native response revoked another target or the current administrator.')
    current = [row for row in after['database']['tables']['sessions'] if row['id'] == target['id']]
    require(len(current) == 1 and current[0]['revoked_at'] is not None and
        HOME.instant(current[0]['revoked_at']) == HOME.instant(revoke['RevokedAt']), 'The target revocation is not persisted with the native response timestamp.')
    changed = copy.deepcopy(before)
    expected = next(row for row in changed['database']['tables']['sessions'] if row['id'] == target['id'])
    expected['revoked_at'] = current[0]['revoked_at']
    additions = restriction.unchanged_rows(op, changed, after)
    require(all(len(rows) == {'sessions': 1, 'activity_entries': 3}.get(name, 0) for name, rows in additions.items()),
        'Recovery changed an old row, target field, policy, device or unapproved table.')
    session = additions['sessions'][0]
    require(session['id'] == admin['id'] and session['revoked_at'] is not None and
        all(op.equal_json(value, session[key]) for key, value in admin.items() if key not in ('last_seen_at', 'revoked_at')),
        'The administrator authentication changed beyond Touch and logout.')
    start, end = (HOME.instant(snapshot['database']['metadata']['captured_at']) for snapshot in (before, after))
    require(start <= HOME.instant(current[0]['revoked_at']) <= end and
        HOME.instant(admin['last_seen_at']) <= HOME.instant(session['last_seen_at']) <= end and
        HOME.instant(admin['created_at']) <= HOME.instant(session['revoked_at']) <= end, 'Recovery timestamps exceed their exact observation window.')
    expected_events = [('session.login', 'native', 'user', admin['user_id'], admin['id'], 'session', admin['id']),
        ('session.revoked', 'native', 'user', admin['user_id'], admin['id'], 'session', target['id']),
        ('session.revoked', 'native', 'user', admin['user_id'], admin['id'], 'session', admin['id'])]
    check_audits(additions['activity_entries'], expected_events, start, end)
    validate_sequences(before, after, additions, {'activity_entries_id_seq': 3})
    return {'target_session_id': target['id'], 'target_token_sha256': TARGET_SHA, 'target_user_id': HOME.B,
        'target_revoked_at': current[0]['revoked_at'], 'target_only_revoked_at_changed': True,
        'old_rows_sequences_private_preserved': True, 'new_administrator_sessions': 1, 'new_native_session_audits': 3,
        'new_devices_play_userdata_references_encodings': 0, 'lost_target_token_401_observed': False,
        'administrator_session_id': admin['id'], 'administrator_revoked': True}


def failure_tree():
    entries, total = {}, 0
    paths = [FAILED, *sorted(FAILED.rglob('*'))]
    require(len(paths) <= 2048, 'The failed observation tree exceeds its recorded scope.')
    for path in paths:
        info = path.lstat()
        require(info.st_uid == info.st_gid == 0 and not stat.S_ISLNK(info.st_mode) and
            ((stat.S_ISDIR(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o700) or
             (stat.S_ISREG(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1)),
            'The failed evidence contains an unowned path, link or permission.')
        row = {'device': info.st_dev, 'inode': info.st_ino, 'uid': info.st_uid, 'gid': info.st_gid,
            'mode': stat.S_IMODE(info.st_mode), 'links': info.st_nlink, 'bytes': info.st_size,
            'mtime_ns': info.st_mtime_ns, 'ctime_ns': info.st_ctime_ns, 'kind': 'directory' if path.is_dir() else 'file'}
        if stat.S_ISREG(info.st_mode):
            raw = HOME.protected(path, limit=32 << 20)
            total += len(raw); require(total <= 256 << 20, 'The failed evidence byte total exceeds its bound.')
            row['sha256'] = digest(raw)
        entries[str(path.relative_to(FAILED))] = row
    return entries


class Run:
    def __init__(self, args):
        self.args, self.op, self.restriction, self.ext, self.reader = args, None, None, None, None
        self.lock = self.root_fd = None
        self.state = self.before = self.authenticated = self.after = self.target = self.admin = None
        self.token = self.csrf = self.token_sha = None
        self.proven = self.closed = False
        self.sources, self.records, self.errors, self.sent = {}, {}, [], set()
        self.deadline = time.monotonic() + 240
        self.revoke_response = self.proof = None
        self.cleanup_reservation = None

    def error(self, stage, error):
        row = {'stage': stage, 'failure_type': type(error).__name__}
        if type(error) is RecoveryError: row['reason'] = str(error)
        self.errors.append(row)

    def save(self, name, value):
        require(self.root_fd is not None and re.fullmatch(r'[a-z][a-z0-9-]*\.json', name), 'A recovery record escaped its new root.')
        raw = HOME.canonical(value) + b'\n'
        fd = os.open(name, os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_WRONLY, 0o600, dir_fd=self.root_fd)
        with os.fdopen(fd, 'wb') as stream:
            stream.write(raw); stream.flush(); os.fsync(stream.fileno())
        os.fsync(self.root_fd)
        self.records[name] = {'path': str(ROOT / name), 'sha256': digest(raw)}
        return self.records[name]

    def worker_closed(self):
        result = subprocess.run(['/usr/bin/systemctl', 'show', HOME.UNIT, '--no-pager', '--property=MainPID,ActiveState,LoadState'],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=10, check=False, env=self.op.ENV)
        require(result.returncode in (0, 1, 4), 'The failed browser worker state cannot be read.')
        status = dict(row.split('=', 1) for row in result.stdout.splitlines() if '=' in row)
        require(status.get('MainPID') == '0' and status.get('ActiveState') in ('inactive', 'failed') and self.reader.cgroup_empty(),
            'The failed browser worker or its child process tree can still act.')

    def check(self):
        require(time.monotonic() < self.deadline, 'The bounded recovery window expired.')
        for path, sha256 in self.sources.items(): HOME.protected(Path(path), sha256, modes=(0o600, 0o644), limit=2 << 20)
        HOME.protected(HOME.STATE, HOME.STATE_SHA)
        HOME.protected(self.args.failure_inputs, self.args.failure_inputs_sha256)
        self.op.verify_fixture_directories(self.state)
        require(self.op.verify_service(self.state) == HOME.PROCESS, 'The exact candidate process changed.')
        self.worker_closed()
        require(failure_tree() == self.original_tree, 'The original failed observation tree changed.')
        if self.root_fd is not None:
            named, opened = ROOT.lstat(), os.fstat(self.root_fd)
            require((named.st_dev, named.st_ino) == (opened.st_dev, opened.st_ino) and stat.S_ISDIR(named.st_mode) and
                named.st_uid == named.st_gid == 0 and stat.S_IMODE(named.st_mode) == 0o700, 'The new recovery evidence root changed identity.')

    def snapshot(self, name):
        value = self.op.preservation_snapshot(self.state, 27)
        self.save(name, value)
        return value

    def load(self):
        require(sys.platform == 'linux' and os.getuid() == os.geteuid() == os.getgid() == os.getegid() == 0 and os.environ.get('SSH_CONNECTION'),
            'Use the authorized root SSH environment only.')
        os.umask(0o077)
        self.sources = {str(Path(__file__).absolute()): self.args.script_sha256, str(HOME_PATH): HOME_SHA}
        for name, (path, sha256) in HOME.HELPERS.items():
            self.sources[str(path)] = sha256
            setattr(self, {'core': 'op', 'restriction': 'restriction', 'extension': 'ext'}[name], HOME.load_source(path, sha256, 'recovery_' + name))
        self.reader = HOME.Run(types.SimpleNamespace()); self.reader.op = self.op
        self.op.command = self.reader.command
        self.op.host_inputs(initial=False)
        self.lock = os.open(self.op.LOCK, os.O_RDONLY | os.O_NOFOLLOW)
        require(self.op.identity(os.fstat(self.lock)) == self.op.identity(self.op.regular(self.op.LOCK)), 'The fixture lock was replaced.')
        fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        require(not os.path.lexists(ROOT), 'This recovery root exists; no retry or adoption is permitted.')
        self.inputs = HOME.decode(HOME.protected(self.args.failure_inputs, self.args.failure_inputs_sha256))
        validate_inputs(self.inputs)
        documents = {key: HOME.read_record(self.inputs[key]) for key in PATHS}
        report, browser, terminal = (documents[key] for key in ('controller_report', 'browser_report', 'worker_terminal'))
        require(report.get('marker') == HOME.MARKER and report.get('result') == 'failed' and report.get('phase') == 'complete' and
            report.get('worker_closed') is True and report.get('fallback_attempted') is False and
            report.get('candidate', {}).get('process') == HOME.PROCESS and report['candidate'].get('binary_sha256') == HOME.BINARY_SHA and
            report['candidate'].get('state_sha256') == HOME.STATE_SHA and report.get('browser_process', {}).get('pid') == 766912 and
            report['browser_process'].get('start_ticks') == '8283655' and
            terminal.get('MainPID') == '0' and terminal.get('ActiveState') in ('inactive', 'failed') and terminal.get('cgroup_empty') is True,
            'The failed controller or terminal process identity differs.')
        require(terminal.get('InvocationID') == 'e34592d2bb604f62a2f3fefcc2bdf5e2' and terminal.get('ExecMainCode') == '2' and
            terminal.get('ExecMainStatus') == '15', 'The actual bounded-worker termination differs.')
        for key in ('before', 'after', 'worker_terminal'):
            require(report.get('evidence', {}).get(PATHS[key].name) == self.inputs[key], 'A recovery snapshot is not linked by the failed report.')
        input_raw = HOME.protected(FAILED / 'input.json', report['input_sha256'])
        HOME.validate_node_report(browser, HOME.decode(input_raw), report['input_sha256'], report['browser_process'])
        require(not os.path.lexists(FAILED / 'browser/session-private.json'), 'The token is not lost; this native recovery path does not apply.')
        self.state = HOME.decode(HOME.protected(HOME.STATE, HOME.STATE_SHA))
        require(self.state.get('phase') == 'ready' and self.state.get('schema') == 27 and self.state.get('process') == HOME.PROCESS and
            self.state.get('binary_sha256') == HOME.BINARY_SHA, 'The current candidate authority differs.')
        self.worker_closed()
        self.original_tree = failure_tree()
        self.check()
        self.target = validate_failed_session(self.op, self.restriction, documents['before'], documents['after'], browser)
        self.before = self.op.preservation_snapshot(self.state, 27)
        self.restriction.compare_fixed_snapshot(self.op, documents['after'], self.before)
        self.restriction.quiescent(self.before)
        self.media = self.ext.media_witness(self.op, self.state)
        accounts = HOME.decode(HOME.protected(HOME.BROWSER, HOME.BROWSER_SHA))
        self.account = accounts['admin']
        user = next(row for row in self.before['database']['tables']['users'] if row['id'] == self.state['admin_id'])
        require(user['is_administrator'] is True and user['is_disabled'] is False and user['name'] == self.account['username'], 'The existing administrator credential binding differs.')
        api = HOME.read_record(HOME.AUTHORITY['api_report'])
        self.client = next(row for row in self.before['database']['tables']['sessions'] if row['id'] == api['authentication']['admin']['session_id'])
        require(self.client['user_id'] == self.state['admin_id'] and self.client['kind'] == 'admin' and self.client['revoked_at'] is not None,
            'The same-binary native client metadata has no accepted prior session.')

    def prepare(self):
        ROOT.mkdir(mode=0o700)
        self.root_fd = os.open(ROOT, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        parent = os.open(WORK, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try: os.fsync(parent)
        finally: os.close(parent)
        self.save('intent.json', {'marker': MARKER, 'failure_inputs': {'path': str(self.args.failure_inputs), 'sha256': self.args.failure_inputs_sha256},
            'target_session_id': self.target['id'], 'target_token_sha256': TARGET_SHA, 'sources': self.sources,
            'allowed_requests': ['native login', 'exact target revoke', 'native logout', 'exact native401'], 'maximum_requests': 4})
        self.save('failure-tree-before.json', self.original_tree)
        self.save('before-full.json', self.before)
        self.save('media-before.json', self.media)
        reservation = {'marker': MARKER, 'only_new_administrator_token': True,
            'maximum_logout_requests': 1, 'maximum_exact401_requests': 1, 'no_login_retry': True}
        descriptor = self.save('cleanup-reservation.json', reservation)
        self.cleanup_reservation = {'value': reservation, 'descriptor': descriptor}

    def reserved_cleanup(self):
        require(self.cleanup_reservation is not None and HOME.read_record(self.cleanup_reservation['descriptor']) == self.cleanup_reservation['value'],
            'The durable conditional administrator cleanup reservation changed.')

    def request(self, purpose):
        routes = {'login': ('POST', '/admin/v1/session'), 'revoke': ('POST', '/admin/v1/sessions/' + self.target['id'] + '/revoke'),
            'logout': ('DELETE', '/admin/v1/session'), 'exact': ('GET', '/admin/v1/session')}
        require(purpose in routes and purpose not in self.sent and len(self.sent) < 4 and
            (purpose == 'login' and not self.sent or purpose != 'login' and self.proven), 'A native request is repeated or outside its precise ownership scope.')
        require(self.target['id'] == TARGET_ID and (purpose != 'revoke' or self.admin is not None), 'The precise target or proven administrator row is unavailable.')
        self.check()
        cleanup = purpose in ('logout', 'exact')
        if cleanup: self.reserved_cleanup()
        method, path = routes[purpose]
        body = {'Name': self.account['username'], 'Password': self.account['password']} if purpose == 'login' else {} if purpose == 'revoke' else None
        try:
            self.save(purpose + '-intent.json', {'method': method, 'path': path, 'body': None if purpose == 'login' else body,
                'target_session_id': self.target['id'] if purpose == 'revoke' else None})
        except Exception as error:
            if not cleanup: raise
            self.error(purpose + '_intent_journal', error)
        self.sent.add(purpose)
        headers = {'Origin': HOME.BASE_URL, 'Accept': 'application/json', 'Accept-Encoding': 'identity', 'Connection': 'close'}
        if purpose != 'login': headers.update(Cookie='goby_session=' + self.token, **{'X-CSRF-Token': self.csrf})
        if body is not None: headers['Content-Type'] = 'application/json'
        connection = http.client.HTTPConnection('127.0.0.1', 18198, timeout=8)
        record = {'status': None, 'complete': False, 'body': None, 'set_cookie': None}
        prior = signal.getsignal(signal.SIGALRM)
        signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(RecoveryError('The native request exceeded its total deadline.')))
        signal.setitimer(signal.ITIMER_REAL, 12)
        try:
            connection.request(method, path, None if body is None else HOME.canonical(body), headers)
            response = connection.getresponse()
            record.update(status=response.status, set_cookie=response.getheader('Set-Cookie'))
            length = response.getheader('Content-Length')
            require(length is None or length.isdigit() and int(length) <= 65536, 'A native response exceeded its length bound.')
            raw = response.read(65537)
            require(len(raw) <= 65536 and (length is None or len(raw) == int(length)), 'A native response was incomplete or oversized.')
            record.update(complete=True, bytes=len(raw), sha256=digest(raw), body=None if not raw else HOME.decode(raw))
            if purpose == 'login': self.acknowledge(record)
        except Exception as error:
            record['failure_type'] = type(error).__name__
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0); signal.signal(signal.SIGALRM, prior); connection.close()
            # A complete new ACK is proven in memory before journaling, so an I/O
            # failure cannot strand its token. Target revocation still requires
            # both this durable ACK and its independent durable request intent.
            try: self.save(purpose + '-response-private.json', record)
            except Exception as error:
                if not cleanup: raise
                self.error(purpose + '_response_journal', error)
        return record

    def acknowledge(self, value):
        match = re.fullmatch(r'goby_session=([A-Za-z0-9_-]{43})', (value.get('set_cookie') or '').split(';', 1)[0])
        user = (value.get('body') or {}).get('User', {})
        csrf = (value.get('body') or {}).get('CSRFToken')
        require(value['complete'] and value['status'] == 200 and match and user.get('Id') == self.state['admin_id'] and
            user.get('Name') == self.account['username'] and user.get('IsAdministrator') is True and user.get('IsDisabled') is False and
            isinstance(csrf, str) and 1 <= len(csrf) <= 256 and all(32 <= ord(char) < 127 for char in csrf), 'The new native login was not completely owned.')
        token_sha = digest(match[1].encode('utf-8'))
        require(all(row['token_hash'] != '\\x' + token_sha for row in self.before['database']['tables']['sessions']), 'The native cookie belongs to a preexisting credential.')
        self.token, self.csrf, self.token_sha, self.proven = match[1], csrf, token_sha, True

    def login(self):
        value = self.request('login')
        require(value['complete'] and value['status'] == 200 and self.proven, 'The new native login lacks its complete durable ACK.')
        self.authenticated = self.snapshot('authenticated-full.json')
        self.admin = validate_admin_login(self.op, self.restriction, self.before, self.authenticated,
            self.state['admin_id'], self.token_sha, self.client)

    def close(self):
        if not self.proven: return
        # Cleanup has its own bounded window; the failed primary action cannot
        # consume the one newly owned administrator's logout opportunity.
        self.deadline = time.monotonic() + 90
        statuses = {}
        for purpose, expected in (('logout', 204), ('exact', 401)):
            try:
                response = self.request(purpose)
                require(response['complete'] and response['status'] == expected and (expected != 204 or response['body'] is None),
                    'The new administrator did not complete its owned logout or exact401.')
                statuses[purpose] = response['status']
            except Exception as error: self.error(purpose, error)
        self.closed = statuses == {'logout': 204, 'exact': 401}

    def execute(self):
        try:
            self.load()
            if self.args.check_only:
                return {'marker': MARKER, 'result': 'checked', 'target_session_id': self.target['id'], 'http_requests': 0, 'output_created': False}
            self.prepare()
            try:
                self.login()
                value = self.request('revoke')
                require(value['complete'] and value['status'] == 200, 'The exact native revocation did not return 200.')
                self.revoke_response = value['body']
            except Exception as error: self.error('native_recovery', error)
            finally: self.close()
            try:
                self.after = self.snapshot('after-full.json')
                require(self.admin is not None and self.closed and self.revoke_response is not None, 'The recovery lacks complete owned protocol evidence.')
                self.proof = validate_final(self.op, self.restriction, self.before, self.authenticated, self.after,
                    self.target, self.admin, self.revoke_response)
                self.restriction.quiescent(self.after)
            except Exception as error: self.error('complete_database_preservation', error)
            try:
                self.check()
                media = self.ext.media_witness(self.op, self.state)
                self.save('media-after.json', media)
                require(self.op.equal_json(self.media, media), 'A protected media byte or identity changed.')
                self.save('failure-tree-after.json', failure_tree())
            except Exception as error: self.error('private_media_failure_tree_preservation', error)
            result = {'marker': MARKER, 'result': 'passed' if not self.errors and self.proof else 'failed', 'phase': 'complete',
                'candidate': {'process': HOME.PROCESS, 'binary_sha256': HOME.BINARY_SHA, 'state_sha256': HOME.STATE_SHA},
                'proof': self.proof, 'http_requests': len(self.sent), 'administrator_closed': self.closed, 'errors': self.errors,
                'evidence': dict(self.records), 'original_ui_result': 'failed', 'client_acceptance': False,
                'boundary': 'Native cleanup only. Lost B token was not recovered or tested; its exact durable session was revoked.'}
            self.save('report.json', result)
            return result
        except Exception as error:
            self.error('preparation', error)
            if self.root_fd is not None:
                try: self.save('failure.json', {'marker': MARKER, 'result': 'failed', 'errors': self.errors, 'evidence': dict(self.records)})
                except Exception: pass
            raise RecoveryError('Recovery stopped; retain the new evidence and do not retry.') from None
        finally:
            self.token = self.csrf = None
            if self.root_fd is not None: os.close(self.root_fd)
            if self.lock is not None: os.close(self.lock)


def arguments(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--script-sha256', required=True)
    parser.add_argument('--failure-inputs', type=Path, required=True)
    parser.add_argument('--failure-inputs-sha256', required=True)
    parser.add_argument('--check-only', action='store_true')
    value = parser.parse_args(argv)
    require(all(re.fullmatch('[0-9a-f]{64}', getattr(value, key)) for key in ('script_sha256', 'failure_inputs_sha256')),
        'The explicit source or failure-input digest is invalid.')
    return value


def load_home():
    global HOME
    # Bootstrap only this previously verified, fixed helper source. Its runtime
    # main/Run.load entry points are never called by the recovery operator.
    info = HOME_PATH.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == info.st_gid == 0 and info.st_nlink == 1 and stat.S_IMODE(info.st_mode) == 0o600,
        'The frozen Home helper is not a private regular file.')
    raw = HOME_PATH.read_bytes()
    require(digest(raw) == HOME_SHA, 'The frozen Home helper source changed.')
    HOME = types.ModuleType('frozen_home_recovery_primitives'); HOME.__file__ = str(HOME_PATH)
    exec(compile(raw, str(HOME_PATH), 'exec'), HOME.__dict__)


if __name__ == '__main__':
    try:
        args = arguments(); load_home()
        result = Run(args).execute()
        print(HOME.exact({'marker': MARKER, 'result': result['result'], 'http_requests': result['http_requests'], 'client_acceptance': False}))
        sys.exit(0 if result['result'] in ('passed', 'checked') else 1)
    except Exception as error:
        print('{"marker":"goby-client-library-home-session-recovery-v1","result":"failed","client_acceptance":false}', file=sys.stderr)
        sys.exit(1)
