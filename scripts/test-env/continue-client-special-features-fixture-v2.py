#!/usr/bin/env python3
"""Continue only the acknowledged, unscanned library from the retained failure.

This is a new exclusive operation, not a retry of the original setup. The old
operator, state capture, acknowledgements and failed evidence are read-only.
There is no create/delete/global-refresh, service action, or state rollback.
"""

import argparse
import copy
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import stat
import sys
import time
import types

sys.dont_write_bytecode = True
WORK = Path('/opt/goby-test/exec-work-m3e')
ORIGIN = WORK / 'client-special-features-fixture-v1'
OUTPUT = WORK / 'client-special-features-fixture-continuation-v1'
INSPECTION = WORK / 'client-special-features-continuation-inspection-v1'
MARKER = 'goby-client-special-features-fixture-continuation-v1'
PROFILE_MARKER = 'goby-client-special-features-fixture-v1'
PROFILE_KEY = 'special_features_profile'
INSPECT_MARKER = 'goby-client-special-features-continuation-inspection-v1'


class ContinuationError(Exception):
    """The explicit failed-attempt or continuation identity is not proven."""


def require(value, message):
    if not value:
        raise ContinuationError(message)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def source(path, expected):
    require(path.is_absolute() and path.is_relative_to(WORK) and '..' not in path.parts, 'A reviewed source is outside WORK.')
    for entry in reversed((path, *path.parents)):
        info = entry.lstat()
        require(not entry.is_symlink() and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022, 'A source is not protected.')
    info = path.stat()
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_size <= 1 << 20, 'A reviewed source exceeds its bound.')
    raw = path.read_bytes()
    require(digest(raw) == expected, 'A frozen source digest changed.')
    result = types.ModuleType(path.stem.replace('-', '_'))
    result.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), result.__dict__)
    return result


def preserved(op, left, right):
    for key in ('schema', 'runtime_sha256', 'browser_sha256', 'added_viewer_credentials', 'recovery'):
        require(op.equal_json(left[key], right[key]), 'A preserved private binding changed.')
    for key in ('tables', 'sequences', 'catalog', 'unsupported'):
        require(op.equal_json(left['database'][key], right['database'][key]), 'The complete current business snapshot changed.')
    for key in ('database', 'server_version_num', 'schemas', 'public_schema', 'relations', 'columns'):
        require(op.equal_json(left['database']['metadata'][key], right['database']['metadata'][key]), 'A current catalog identity changed.')


def require_continuation_quiescent(snapshot, state, profile):
    """Check the real create-only layout and all retained activity fences."""
    tables = snapshot['database']['tables']
    original_libraries = {entry['id'] for entry in state['libraries'].values()}
    expected_libraries = original_libraries | {profile.LIBRARY_ID}
    libraries = tables.get('libraries', [])
    items = tables.get('items', [])
    require(snapshot.get('schema') == 27 and len(tables) == 35 and len(original_libraries) == 3 and
            profile.LIBRARY_ID not in original_libraries and len(libraries) == 4 and
            {row['id'] for row in libraries} == expected_libraries and len(items) == 14 and len({row['id'] for row in items}) == 14,
            'Continuation requires the exact thirteen original items plus its acknowledged library item and four libraries.')
    owned_items = [row for row in items if row['library_id'] == profile.LIBRARY_ID]
    require(len(owned_items) == 1 and owned_items[0]['id'] == profile.LIBRARY_ID and owned_items[0]['type'] == 'CollectionFolder' and
            owned_items[0]['is_folder'] is True and owned_items[0]['root_id'] is None and owned_items[0]['parent_id'] is None and
            owned_items[0]['path'] == owned_items[0]['relative_path'] == '' and owned_items[0]['media'] is None and
            all(row['library_id'] in original_libraries for row in items if row['id'] != profile.LIBRARY_ID),
            'The acknowledged library no longer contains exactly its unscanned CollectionFolder.')
    roots = [row for row in tables['library_roots'] if row['library_id'] == profile.LIBRARY_ID]
    require(roots == [{'id': profile.ROOT_ID, 'library_id': profile.LIBRARY_ID,
                       'path': '/opt/goby-fixtures/client-special-features-m3e-v1/Movies',
                       'allowed_path': '/opt/goby-fixtures/client-special-features-m3e-v1/Movies', 'relative_path': '.'}],
            'The acknowledged continuation root changed.')
    require(tables.get('encoding_jobs') == [], 'Encoding work is present before continuation.')
    for table, field in (('scan_jobs', 'status'), ('task_runs', 'state'), ('task_run_children', 'state')):
        require(not any(str(row.get(field, '')).lower() in {'waiting', 'pending', 'queued', 'running', 'stopping'} for row in tables[table]),
                'A scan or scheduled operation remains active.')
    require(not any(row['library_id'] == profile.LIBRARY_ID for row in tables['scan_jobs']), 'The acknowledged library already has a scan job.')
    sessions = {row['id']: row for row in tables['sessions']}
    for row in tables['play_sessions']:
        require(row.get('state') not in ('Playing', 'Paused'), 'Playback remains active.')
        if row.get('state') == 'Prepared':
            credential = sessions.get(row.get('auth_session_id'), {})
            require(isinstance(row.get('user_id'), str) and row['user_id'] and isinstance(row.get('auth_session_id'), str) and row['auth_session_id'] and
                    'started_at' in row and row['started_at'] is None and row.get('counted') is False and
                    'application_client_id' in row and row['application_client_id'] is None and credential.get('kind') == 'emby' and
                    credential.get('user_id') == row['user_id'] and credential.get('revoked_at') is not None,
                    'Prepared history lacks an unstarted, already-revoked ordinary credential.')


def origin_evidence(op):
    root_identity = op.directory(ORIGIN, 0, 0o700, 0)
    op.directory(ORIGIN / 'private', 0, 0o700, 0)
    files, total = {}, 0
    for path in sorted(ORIGIN.rglob('*')):
        if path.is_dir():
            require(path == ORIGIN / 'private', 'An unexpected directory entered the retained origin.')
            continue
        info = op.regular(path, limit=op.MAX_SNAPSHOT_BYTES)
        raw = op.read(path, limit=op.MAX_SNAPSHOT_BYTES)
        total += len(raw)
        require(total <= 128 << 20 and len(files) < 64, 'The retained evidence exceeds its bound.')
        files[path.relative_to(ORIGIN).as_posix()] = {'sha256': digest(raw), 'bytes': len(raw),
            'identity': {'device': info.st_dev, 'inode': info.st_ino, 'uid': info.st_uid, 'gid': info.st_gid,
                         'mode': stat.S_IMODE(info.st_mode), 'mtime_ns': info.st_mtime_ns, 'ctime_ns': info.st_ctime_ns}}
    expected = {'OWNER.json', 'before-state.json', 'before-full.json', 'media-before.json', 'failure.json', 'failed-current-full.json',
                'private/admin-login-ack.json', 'private/library-ack.json', 'private/phase-prepared.json'}
    for number, label in enumerate(('admin-login', 'libraries-before', 'create-library', 'admin-logout', 'admin-exact-token'), 1):
        expected.update(f'private/{number:04d}-{label}-{kind}.json' for kind in ('intent', 'response'))
    require(set(files) == expected, 'The retained attempt is not the exact five-request create-only evidence tree.')
    return {'marker': 'goby-client-special-features-origin-evidence-v1', 'path': str(ORIGIN), 'root_identity': root_identity, 'files': files}


def make_setup(base):
    class Setup(base.Setup):
        def __init__(self, args):
            super().__init__(args)
            self.cont = self.origin_state_bytes = self.failure = self.origin_original = self.origin_failed = None
            self.origin_tree = self.authority = self.history_state = None

        def private(self, name, value):
            require(re.fullmatch('[a-z0-9-]+[.](json|bin)', name), 'A new evidence name is not canonical.')
            self.op.create(OUTPUT / 'private' / name, value)

        def source_records(self):
            return {'setup_source': {'path': str(Path(__file__).absolute()), 'sha256': self.args.script_sha256},
                    'profile_source': {'path': str(self.args.profile_path), 'sha256': self.args.profile_sha256}}

        def check(self, *, cleanup=False):
            a, op = self.args, self.op
            require(time.monotonic() < self.deadline, 'The continuation deadline expired.')
            for path, expected in ((Path(__file__).absolute(), a.script_sha256), (a.origin_setup_path, self.cont.PINS['origin_setup_source']),
                (a.origin_profile_path, self.cont.PINS['origin_profile_source']), (a.profile_path, a.profile_sha256),
                (a.operator_path, a.operator_sha256), (a.extension_operator_path, a.extension_operator_sha256)):
                base.source_bytes(path, expected)
            require(op.read(op.STATE_FILE) == self.state_bytes and op.sha(op.RUNTIME) == self.state['runtime_sha256'] and
                    op.sha(a.extension_dir / 'completed.json') == a.extension_completed_sha256 and
                    op.sha(a.extension_dir / 'report.json') == a.extension_report_sha256, 'The current process/state/runtime proof changed.')
            op.verify_fixture_directories(self.state)
            op.verify_database(self.state)
            require(op.verify_service(self.state) == self.state['process'], 'The candidate process changed.')
            if not cleanup and self.output_identity is not None:
                require(op.directory(OUTPUT, 0, 0o700, 0) == self.output_identity, 'The new evidence root changed identity.')

        def checkpoint(self, phase):
            require(phase in self.cont.PHASE_FILES, 'An unsupported continuation checkpoint was requested.')
            name = self.cont.PHASE_FILES[phase]
            require(re.fullmatch('[a-z0-9-]+[.](json|bin)', name), 'A checkpoint filename is not admissible before state mutation.')
            self.check()
            self.private(name.replace('.json', '-intent.json'), {'phase': phase, 'previous_state_sha256': digest(self.state_bytes)})
            self.state.update(phase='continuing_special_features_fixture', stage=phase)
            self.state[PROFILE_KEY] = {'marker': PROFILE_MARKER, 'phase': phase, 'evidence_directory': str(OUTPUT),
                'library_id': self.library_id, 'root_id': self.root_id, 'job_id': self.job_id,
                'origin_failure': self.authority['origin_failure'], 'source_state_sha256': self.cont.PINS['origin_state']}
            self.same_base_state()
            self.op.save_state(self.state)
            self.state_bytes = self.op.read(self.op.STATE_FILE)
            self.phase = phase
            self.private(name, {'phase': phase, 'state_sha256': digest(self.state_bytes),
                                'library_id': self.library_id, 'root_id': self.root_id, 'job_id': self.job_id})

        def same_base_state(self):
            excluded = ('phase', 'stage', PROFILE_KEY)
            require(self.op.equal_json({key: value for key, value in self.history_state.items() if key not in excluded},
                                      {key: value for key, value in self.state.items() if key not in excluded}),
                    'An original fixture or historical upgrade field changed.')

        def approved(self, label, method, path, actor, body, login, cleanup, media_mode):
            require(cleanup or (not (method == 'POST' and path == '/admin/v1/libraries') and method != 'DELETE'),
                    'Continuation cannot create or delete a library.')
            super().approved(label, method, path, actor, body, login, cleanup, media_mode)

        def read_origin(self):
            op, pins = self.op, self.cont.PINS
            paths = {'origin_failure': ORIGIN / 'failure.json', 'origin_before_state': ORIGIN / 'before-state.json',
                'origin_before_snapshot': ORIGIN / 'before-full.json', 'origin_current_snapshot': ORIGIN / 'failed-current-full.json',
                'origin_library_ack': ORIGIN / 'private/library-ack.json'}
            documents = {}
            self.authority = {key: {'path': str(path), 'sha256': pins[key]} for key, path in paths.items()}
            for key, path in paths.items():
                raw = op.read(path, limit=op.MAX_SNAPSHOT_BYTES)
                require(digest(raw) == pins[key], 'A pinned original failed-attempt artifact changed.')
                documents[key] = op.precise_json(raw)
            self.failure = documents['origin_failure']
            self.history_state = documents['origin_before_state']
            self.origin_original, self.origin_failed = documents['origin_before_snapshot'], documents['origin_current_snapshot']
            ack = documents['origin_library_ack']
            require(ack.get('Id') == self.cont.LIBRARY_ID and ack.get('Name') == self.profile.LIBRARY_NAME and
                    ack.get('CollectionType') == 'movies' and ack.get('Paths') == [self.profile.ROOT] and ack.get('LastScanAt') is None,
                    'The retained 201 acknowledgement identifies another library.')
            self.origin_tree = origin_evidence(op)
            for number, label, method, path, status in ((1, 'admin-login', 'POST', '/admin/v1/session', 200),
                (2, 'libraries-before', 'GET', '/admin/v1/libraries', 200), (3, 'create-library', 'POST', '/admin/v1/libraries', 201),
                (4, 'admin-logout', 'DELETE', '/admin/v1/session', 204), (5, 'admin-exact-token', 'GET', '/admin/v1/session', 401)):
                stem = ORIGIN / 'private' / f'{number:04d}-{label}'
                intent = op.load(Path(str(stem) + '-intent.json'))
                record = op.load(Path(str(stem) + '-response.json'))
                require(intent['method'] == record['request']['method'] == method and intent['path'] == record['request']['path'] == path and
                        intent['actor'] == record['request']['actor'] == 'admin' and record['response']['status'] == status and
                        record['response']['complete'] is True and record['response']['failure_type'] is None,
                        'A retained request/response does not prove the exact closed create-only sequence.')
                if label == 'create-library':
                    require(intent['body'] == base.create_body(self.profile) and record['response']['body']['Library'] == ack,
                            'The exact retained create request/201 response changed.')
            # Inspect only the owned admin acknowledgement in memory. Its raw
            # credential is never used on the network or copied to a report.
            acknowledged = op.load(ORIGIN / 'private/admin-login-ack.json')
            match = re.fullmatch(r'goby_session=([A-Za-z0-9_-]{43})', acknowledged['cookie'].split(';', 1)[0])
            user = acknowledged['body']['User']
            require(acknowledged.get('identity_proven') is True and match and user['Id'] == self.state['admin_id'] and
                    user['IsAdministrator'] is True and user['IsDisabled'] is False and
                    digest(match[1].encode()) == self.failure['authentication']['admin']['token_sha256'], 'The retained revoked admin acknowledgement is not owned.')
            self.creation_proof = self.cont.validate_creation(op, self.profile, self.origin_original, self.origin_failed, self.state, self.failure)

        def load(self):
            a = self.args
            require(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0 and os.environ.get('SSH_CONNECTION'), 'Use only root SSH on test-env.')
            require(a.operator_path == WORK / 'prepare-client-fixture.py', 'The frozen helper must retain its actual WORK path.')
            self.cont = source(a.profile_path, a.profile_sha256)
            require(self.cont.MARKER == MARKER, 'The selected profile identifies another continuation.')
            self.profile = source(a.origin_profile_path, self.cont.PINS['origin_profile_source'])
            self.op = op = source(a.operator_path, a.operator_sha256)
            self.ext = source(a.extension_operator_path, a.extension_operator_sha256)
            require(op.WORK == WORK and op.PORT == 18198 and op.UNIT == 'goby-client-m3e.service' and self.ext.OUTPUT == a.extension_dir and
                    self.ext.ADDED_ROOT == self.profile.ROOT, 'A selected source identifies another candidate.')
            op.command = self.command
            op.host_inputs(initial=False)
            self.lock = os.open(op.LOCK, os.O_RDONLY | os.O_NOFOLLOW)
            require(op.identity(os.fstat(self.lock)) == op.identity(op.regular(op.LOCK)), 'The fixture lock identity changed.')
            fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            self.state_bytes = op.read(op.STATE_FILE)
            require(digest(self.state_bytes) == a.state_sha256, 'The explicit current candidate state changed.')
            self.state = op.precise_json(self.state_bytes)
            self.read_origin()
            self.same_base_state()
            self.extension = op.load(a.extension_dir / 'completed.json')
            ext_report = op.load(a.extension_dir / 'report.json')
            require(op.sha(a.extension_dir / 'completed.json') == a.extension_completed_sha256 and
                    op.sha(a.extension_dir / 'report.json') == a.extension_report_sha256, 'The extension receipt changed.')
            upgrade_dir = Path(self.history_state['upgrade']['evidence_directory'])
            require(upgrade_dir.parent == WORK and re.fullmatch('client-upgrade-[0-9a-f]{32}', upgrade_dir.name) and a.upgrade_report.parent == WORK and
                    re.fullmatch('client-fixture-report-[0-9a-f]{24}[.]json', a.upgrade_report.name), 'The historical upgrade paths changed.')
            self.upgraded = op.load(upgrade_dir / 'completed.json')
            upgraded_report = op.load(a.upgrade_report)
            require(op.sha(upgrade_dir / 'completed.json') == a.upgrade_completed_sha256 and op.sha(a.upgrade_report) == a.upgrade_report_sha256,
                    'The historical completed upgrade evidence changed.')
            # This is the actual retained ready state, not a fabricated copy of
            # the failed state. The current state's other fields match it above.
            base.validate_extension_chain(op, self.history_state, self.extension, ext_report, self.upgraded, upgraded_report, a)
            artifacts = self.upgraded['schema_artifacts']
            require(artifacts['operator'] == {'path': str(a.operator_path), 'sha256': a.operator_sha256}, 'The historical helper differs.')
            self.source_binding = {'path': artifacts['source'], 'manifest_sha256': artifacts['source_manifest_sha256'],
                'full_report_path': artifacts['product_verification']['report_path'], 'full_report_sha256': artifacts['product_verification']['report_sha256']}
            proof = op.verify_product_upgrade(Path(self.upgraded['source']), a.candidate_sha256, Path(self.source_binding['path']),
                self.source_binding['manifest_sha256'], Path(self.source_binding['full_report_path']), self.source_binding['full_report_sha256'], 27)
            require(op.equal_json(proof, artifacts['product_verification']) and self.extension['source_manifest_sha256'] == self.source_binding['manifest_sha256'] and
                    self.extension['full_report_sha256'] == self.source_binding['full_report_sha256'], 'The complete source/full-test evidence changed.')
            self.upgrade_binding = {'completed_path': str(upgrade_dir / 'completed.json'), 'completed_sha256': a.upgrade_completed_sha256,
                'report_path': str(a.upgrade_report), 'report_sha256': a.upgrade_report_sha256,
                'old_process': self.upgraded['old_process'], 'new_process': self.upgraded['new_process']}
            self.extension_binding = {'completed_path': str(a.extension_dir / 'completed.json'), 'completed_sha256': a.extension_completed_sha256,
                'report_path': str(a.extension_dir / 'report.json'), 'report_sha256': a.extension_report_sha256,
                **{key: self.extension[key] for key in ('old_process', 'new_process', 'old_runtime_sha256', 'new_runtime_sha256')}}
            self.check()
            self.media = self.ext.media_witness(op, self.state)
            require(op.equal_json(self.media, op.load(ORIGIN / 'media-before.json')), 'A media identity changed since the retained creation.')
            self.current = op.preservation_snapshot(self.state, 27)
            self.profile.validate_structure(op, self.current, self.state)
            self.library_id, self.root_id = self.cont.LIBRARY_ID, self.cont.ROOT_ID
            if a.mode == 'prepare':
                require(a.receipt_sha256 is None and a.setup_report_sha256 is None and not op.exists(OUTPUT) and
                        digest(self.state_bytes) == self.cont.PINS['origin_state'] and self.state['phase'] == 'preparing_special_features_fixture' and
                        self.state['stage'] == 'library_acknowledged', 'Only the exact unconsumed failed state can be continued.')
                record = self.state[PROFILE_KEY]
                require(record == {'evidence_directory': str(ORIGIN), 'job_id': None, 'library_id': self.library_id, 'marker': PROFILE_MARKER,
                    'phase': 'library_acknowledged', 'root_id': None, 'source_state_sha256': self.cont.PINS['origin_before_state']}, 'The retained state is not the exact creation checkpoint.')
                preserved(op, self.origin_failed, self.current)
                require_continuation_quiescent(self.current, self.state, self.cont)
                require(not any(row['reported_device_id'] == self.device for row in self.current['database']['tables']['devices']), 'The new viewer device is not fresh.')
                users = {row['id']: row for row in self.current['database']['tables']['users']}
                for user_id in (self.state['viewer_id'], self.state['added_viewer']['user_id']):
                    policy = users[user_id]['policy']
                    require(users[user_id]['is_administrator'] is False and users[user_id]['is_disabled'] is False and isinstance(policy, dict) and
                            all(key not in policy or policy[key] is True for key in ('EnableAllFolders', 'EnableMediaPlayback')),
                            'An ordinary actor no longer has the existing required access.')
                self.origin_state_bytes, self.before = self.state_bytes, self.current
            else:
                require(not a.check_only and a.receipt_sha256 and a.setup_report_sha256 and self.state['phase'] == 'ready' and self.state['stage'] == 'complete',
                        'Inspection requires an explicitly completed new profile.')

        def begin(self):
            op = self.op
            OUTPUT.mkdir(mode=0o700)
            (OUTPUT / 'private').mkdir(mode=0o700)
            self.output_identity = op.directory(OUTPUT, 0, 0o700, 0)
            op.create(OUTPUT / 'OWNER.json', {'marker': MARKER, 'identity': self.output_identity, 'origin_failure': self.authority['origin_failure']})
            op.create(OUTPUT / 'origin-state.json', self.origin_state_bytes)
            op.create(OUTPUT / 'origin-evidence.json', self.origin_tree)
            op.create(OUTPUT / 'before-full.json', self.before)
            op.create(OUTPUT / 'media-before.json', self.media)
            self.continuation_window = {'before': op.postgres('SELECT clock_timestamp()::text;', op.ROLE)}
            self.window = {'before': self.origin_original['database']['metadata']['captured_at']}
            browser, alias = op.load(op.BROWSER), op.load(op.AV_BROWSER)
            self.secrets.update((browser['admin']['password'], browser['viewer']['password'], alias['viewer']['password']))
            self.actors = {'admin': base.Actor(self, 'admin', browser['admin']), 'viewer': base.Actor(self, 'viewer', alias['viewer'])}
            self.checkpoint('continuation_prepared')

        def scan(self):
            admin = self.actors['admin']
            admin.login()
            listed = self.get('libraries-before-continuation', '/admin/v1/libraries', admin)
            expected = {value['id'] for value in self.state['libraries'].values()} | {self.library_id}
            require(isinstance(listed, dict) and listed.get('TotalRecordCount') == 4 and isinstance(listed.get('Items'), list) and
                    {row.get('Id') for row in listed['Items']} == expected, 'The live inventory no longer matches the four owned libraries.')
            owned = [row for row in listed['Items'] if row.get('Id') == self.library_id]
            require(len(owned) == 1 and owned[0]['Name'] == self.profile.LIBRARY_NAME and owned[0]['Paths'] == [self.profile.ROOT] and
                    owned[0]['CollectionType'] == 'movies' and owned[0]['LastScanAt'] is None, 'The acknowledged library was modified or scanned.')
            self.private('continued-scan-intent.json', {'library_id': self.library_id, 'root_id': self.root_id,
                'origin_library_ack': self.authority['origin_library_ack'], 'force_probe': False, 'library_create_permitted': False})
            response = self.request('scan-library', 'POST', '/admin/v1/libraries/' + self.library_id + '/scan', admin, {})
            job = response['body'].get('Job', {}) if isinstance(response['body'], dict) else {}
            prior_jobs = {row['id'] for row in self.before['database']['tables']['scan_jobs']}
            require(response['status'] == 202 and re.fullmatch('[0-9a-f]{32}', job.get('Id', '')) and job['Id'] not in prior_jobs and
                    job.get('LibraryId') == self.library_id and job.get('ForceProbe') is False, 'The one scan lacks its exact owned acknowledgement.')
            self.job_id = job['Id']
            self.private('scan-ack.json', job)
            self.checkpoint('scan_acknowledged')
            deadline = time.monotonic() + 300
            for number in range(150):
                result = self.get(f'jobs-{number:03d}', '/admin/v1/jobs', admin)
                require(isinstance(result, dict) and isinstance(result.get('Items'), list) and len(result['Items']) <= 1000, 'The job inventory exceeds its bound.')
                matches = [row for row in result['Items'] if row.get('Id') == self.job_id]
                require(len(matches) == 1 and matches[0].get('LibraryId') == self.library_id and matches[0].get('ForceProbe') is False,
                        'The acknowledged scan changed ownership.')
                job = matches[0]
                if job.get('Status') not in ('pending', 'queued', 'running'):
                    require(job.get('Status') == 'completed' and job.get('Error') == '' and
                            (job.get('Scanned'), job.get('Added'), job.get('Updated')) == (6, 6, 0), 'The scan did not complete its exact six-file scope.')
                    self.private('scan-result.json', job)
                    break
                require(time.monotonic() < deadline, 'The acknowledged scan exceeded its bounded observation window.')
                time.sleep(2)
            else:
                raise ContinuationError('The acknowledged scan did not reach a terminal result.')
            admin.logout()
            require(not self.journal_failures, 'A journal failure prevents further business work.')
            self.checkpoint('scan_complete')

        def continuation(self, proof):
            return {'marker': MARKER, **self.authority,
                'origin_state': {'path': str(OUTPUT / 'origin-state.json'), 'sha256': self.cont.PINS['origin_state']},
                'origin_evidence': {'path': str(OUTPUT / 'origin-evidence.json'), 'sha256': self.op.sha(OUTPUT / 'origin-evidence.json')},
                'origin_setup_source': {'path': str(self.args.origin_setup_path), 'sha256': self.cont.PINS['origin_setup_source']},
                'origin_profile_source': {'path': str(self.args.origin_profile_path), 'sha256': self.cont.PINS['origin_profile_source']},
                'library_id': self.library_id, 'root_id': self.root_id, 'creation_admin_session_id': proof['creation_admin_session_id'],
                'new_scan_admin_session_id': proof['native_session_id'], 'new_viewer_session_id': proof['viewer_session_id'], 'scan_job_id': self.job_id,
                'creation_requests': 5, 'continuation_library_creates': 0, 'continuation_scan_dispatches': 1,
                'original_failure_preserved': True, 'creation_admin_revoked': True, 'continuation_sessions_revoked': True}

        def finish(self):
            op = self.op
            require(not self.journal_failures and all(actor.closed for actor in self.actors.values()), 'Complete evidence and owned cleanup are required.')
            self.continuation_window['after'] = op.postgres('SELECT clock_timestamp()::text;', op.ROLE)
            self.window['after'] = self.continuation_window['after']
            self.after = op.preservation_snapshot(self.state, 27)
            receipt = {'marker': PROFILE_MARKER, 'phase': 'complete', 'schema': 27, 'profile_version': 2,
                'fixture_tag': self.state['tag'], 'server_id': self.state['server_id'], 'source_state_sha256': self.cont.PINS['origin_state'],
                'candidate': self.candidate(), 'source': self.source_binding, 'upgrade': self.upgrade_binding, 'extension': self.extension_binding,
                'library_id': self.library_id, 'root_id': self.root_id, 'job_id': self.job_id, 'device_id': self.device, 'client_name': base.CLIENT,
                'authentication': {role: actor.status for role, actor in self.actors.items()}, 'window': self.window,
                'continuation_window': self.continuation_window, **self.source_records()}
            proof = self.cont.validate_transition(op, self.profile, self.origin_original, self.origin_failed, self.after, self.state, self.failure, receipt, self.media)
            require(op.equal_json(origin_evidence(op), self.origin_tree) and op.equal_json(self.ext.media_witness(op, self.state), self.media),
                    'An original evidence or media member changed.')
            op.create(OUTPUT / 'after-full.json', self.after)
            receipt['continuation'] = self.continuation(proof)
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
            self.check()
            op.create(OUTPUT / 'completed.json', receipt)
            receipt_sha = op.sha(OUTPUT / 'completed.json')
            self.state[PROFILE_KEY] = {'marker': PROFILE_MARKER, 'phase': 'complete', 'receipt_path': str(OUTPUT / 'completed.json'),
                'receipt_sha256': receipt_sha, 'library_id': self.library_id, 'root_id': self.root_id}
            self.state.update(phase='ready', stage='complete')
            self.same_base_state()
            op.save_state(self.state)
            self.state_bytes = op.read(op.STATE_FILE)
            self.completed, self.phase = receipt, 'complete'
            self.check()
            report = {'marker': 'goby-client-special-features-setup-report-v1', 'result': 'passed', 'phase': 'complete',
                'state_sha256': digest(self.state_bytes), 'receipt_path': str(OUTPUT / 'completed.json'), 'receipt_sha256': receipt_sha,
                **self.source_records(), 'candidate': self.candidate(), 'continuation': receipt['continuation'], 'proof': proof,
                'protocol': {key: value for key, value in self.records.items() if not key.endswith('login')},
                'json_bytes': self.json_bytes, 'full_media_bytes': self.full_bytes, 'range_media_bytes': self.range_bytes,
                'boundary': 'One acknowledged library continued by API. The original failure is retained; no original-client UI or playback acceptance claim.'}
            op.create(OUTPUT / 'report.json', self.sanitize(report))

        def inspect(self):
            a, op = self.args, self.op
            require(not op.exists(INSPECTION), 'This new continuation inspection output already exists.')
            require(op.sha(OUTPUT / 'completed.json') == a.receipt_sha256 and op.sha(OUTPUT / 'report.json') == a.setup_report_sha256,
                    'The explicit completed continuation evidence changed.')
            receipt, setup_report = op.load(OUTPUT / 'completed.json'), op.load(OUTPUT / 'report.json')
            require(self.state[PROFILE_KEY] == {'marker': PROFILE_MARKER, 'phase': 'complete', 'receipt_path': str(OUTPUT / 'completed.json'),
                'receipt_sha256': a.receipt_sha256, 'library_id': self.library_id, 'root_id': self.root_id} and
                receipt.get('marker') == PROFILE_MARKER and receipt.get('phase') == 'complete' and receipt.get('profile_version') == 2 and
                receipt.get('candidate') == self.candidate() and receipt.get('source') == self.source_binding and
                receipt.get('extension') == self.extension_binding and receipt.get('upgrade') == self.upgrade_binding and
                all(receipt.get(key) == value for key, value in self.source_records().items()), 'The current completed profile identity differs.')
            require(op.sha(OUTPUT / 'origin-state.json') == self.cont.PINS['origin_state'] and
                    op.equal_json(op.load(OUTPUT / 'origin-evidence.json'), self.origin_tree), 'The origin state or full evidence tree changed.')
            snapshots = {}
            for key, name in (('before_snapshot', 'before-full.json'), ('after_snapshot', 'after-full.json')):
                path = OUTPUT / name
                require(receipt[key]['path'] == str(path) and op.sha(path, limit=op.MAX_SNAPSHOT_BYTES) == receipt[key]['sha256'], 'A continuation snapshot changed.')
                snapshots[key] = op.precise_json(op.read(path, limit=op.MAX_SNAPSHOT_BYTES))
            preserved(op, self.origin_failed, snapshots['before_snapshot'])
            proof = self.cont.validate_transition(op, self.profile, self.origin_original, self.origin_failed, snapshots['after_snapshot'],
                                                 self.state, self.failure, receipt, self.media)
            preserved(op, snapshots['after_snapshot'], self.current)
            self.job_id = receipt['job_id']
            require(op.equal_json(receipt['proof'], proof) and receipt['continuation'] == self.continuation(proof) and
                    setup_report.get('result') == 'passed' and setup_report.get('phase') == 'complete' and
                    setup_report.get('receipt_sha256') == a.receipt_sha256 and setup_report.get('state_sha256') == digest(self.state_bytes) and
                    setup_report.get('continuation') == receipt['continuation'], 'The setup proof or report differs from the exact current profile.')
            self.check()
            INSPECTION.mkdir(mode=0o700)
            op.create(INSPECTION / 'current-full.json', self.current)
            summary = op.preservation_summary(self.current)
            snapshot_path, snapshot_sha = str(INSPECTION / 'current-full.json'), op.sha(INSPECTION / 'current-full.json', limit=op.MAX_SNAPSHOT_BYTES)
            report = {'marker': INSPECT_MARKER, 'result': 'passed', 'phase': 'complete', 'schema': 27, 'profile_marker': PROFILE_MARKER,
                'process': self.state['process'], 'binary_sha256': self.state['binary_sha256'], 'runtime_sha256': self.state['runtime_sha256'],
                'state_sha256': digest(self.state_bytes), 'receipt_path': str(OUTPUT / 'completed.json'), 'receipt_sha256': a.receipt_sha256,
                **self.source_records(), 'source': self.source_binding, 'upgrade': self.upgrade_binding, 'extension': self.extension_binding,
                'continuation': receipt['continuation'], 'profile': {'receipt_path': str(OUTPUT / 'completed.json'), 'receipt_sha256': a.receipt_sha256,
                'library_id': self.library_id, 'root_id': self.root_id, 'item_ids': receipt['item_ids'], 'resource_count': 4, 'reserved_path_count': 3},
                'verification': dict.fromkeys(('complete_schema27', 'exact_nonempty_profile', 'setup_old_rows_preserved', 'current_matches_completed',
                                              'credentials_recovery_media_preserved', 'new_setup_sessions_revoked'), True),
                'inspection': {'table_count': 35, 'item_count': 22, 'library_count': 4, 'database_sha256': summary['database_sha256'],
                               'current_snapshot_path': snapshot_path, 'current_snapshot_sha256': snapshot_sha},
                'current_snapshot_path': snapshot_path, 'current_snapshot_sha256': snapshot_sha,
                'boundary': 'Fresh continuation setup-point proof. Later client preparation requires its separately scoped ledger.'}
            op.create(INSPECTION / 'report.json', report)
            self.phase = 'inspection_complete'

        def run(self):
            failure = None
            try:
                self.load()
                if self.args.mode == 'inspect':
                    self.inspect()
                elif self.args.check_only:
                    print(json.dumps({'result': 'preflight_passed', 'http_requests': 0, 'new_evidence_writes': 0,
                        'creation_proof': self.creation_proof, 'origin_evidence_sha256': digest(self.op.canonical_json(self.origin_tree))}), flush=True)
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
                    require(failure is None and not self.journal_failures and all(actor.closed for actor in self.actors.values()), 'Owned cleanup or evidence is incomplete.')
                    self.finish()
            except Exception as error:
                failure = failure or type(error).__name__
                if self.output_identity is not None:
                    try:
                        observed = self.op.preservation_snapshot(self.state, 27)
                        self.op.create(OUTPUT / 'failed-current-full.json', observed)
                    except Exception:
                        pass
                    try:
                        self.op.create(OUTPUT / 'failure.json', {'marker': MARKER, 'result': 'retained_for_review', 'phase': self.phase,
                            'failure_type': failure, 'library_id': self.library_id, 'root_id': self.root_id, 'job_id': self.job_id,
                            'authentication': {role: actor.status for role, actor in self.actors.items()}, 'journal_failures': self.journal_failures,
                            'origin_failure': self.authority['origin_failure'], 'retry_permitted': False})
                    except Exception:
                        pass
            finally:
                if self.lock is not None:
                    os.close(self.lock)
                    self.lock = None
            print(json.dumps({'result': 'passed' if failure is None else 'retained_for_review', 'phase': self.phase,
                'failure_type': failure, 'evidence': str(INSPECTION if self.args.mode == 'inspect' else OUTPUT),
                'owned_cleanup': {role: {key: actor.status[key] for key in ('logout_status', 'exact_status')} for role, actor in self.actors.items()},
                'journal_failures': self.journal_failures}), flush=True)
            return 0 if failure is None else 1

    return Setup


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('prepare', 'inspect'))
    for name in ('script', 'operator', 'extension-operator', 'profile', 'state', 'extension-completed', 'extension-report', 'upgrade-completed', 'upgrade-report', 'candidate'):
        parser.add_argument('--' + name + '-sha256', required=True)
    for name in ('operator-path', 'extension-operator-path', 'profile-path', 'origin-setup-path', 'origin-profile-path', 'extension-dir', 'upgrade-report'):
        parser.add_argument('--' + name, required=True, type=Path)
    parser.add_argument('--candidate-pid', required=True, type=int)
    parser.add_argument('--candidate-start-ticks', required=True, type=int)
    parser.add_argument('--candidate-boot-id', required=True)
    parser.add_argument('--receipt-sha256')
    parser.add_argument('--setup-report-sha256')
    parser.add_argument('--check-only', action='store_true')
    args = parser.parse_args()
    for key, value in vars(args).items():
        if key.endswith('_sha256') and value is not None:
            require(re.fullmatch('[0-9a-f]{64}', value), 'An explicit digest is malformed.')
    require(args.candidate_pid > 1 and args.candidate_start_ticks > 0 and re.fullmatch('[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}', args.candidate_boot_id),
            'The process identity is malformed.')
    require(digest(Path(__file__).read_bytes()) == args.script_sha256, 'The selected continuation operator changed.')
    os.umask(0o077)
    signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(TimeoutError('A bounded request expired.')))
    continuation_profile = source(args.profile_path, args.profile_sha256)
    require(args.origin_setup_path.name == 'prepare-client-special-features-fixture.py' and args.origin_profile_path.name == 'client-special-features-profile.py',
            'The original frozen source filenames changed.')
    base = source(args.origin_setup_path, continuation_profile.PINS['origin_setup_source'])
    return make_setup(base)(args).run()


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except Exception as error:
        print(json.dumps({'result': 'retained_for_review', 'failure_type': type(error).__name__, 'retry_permitted': False}), file=sys.stderr)
        raise SystemExit(1)
