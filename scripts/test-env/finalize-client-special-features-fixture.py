#!/usr/bin/env python3
"""Finish only four original-resource byte proofs after two retained failures.

Exactly one fresh ordinary AV login, four full reads, four Range reads and its
logout/401 proof are permitted. Catalog and parent projections are replayed
from immutable completed responses; no admin login, scan, library operation,
PlaybackInfo, playstate or metadata mutation is implemented.
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
FIRST = WORK / 'client-special-features-fixture-v1'
SCANNED = WORK / 'client-special-features-fixture-continuation-v1'
OUTPUT = WORK / 'client-special-features-protocol-finalization-v1'
INSPECTION = WORK / 'client-special-features-finalization-inspection-v1'
REVIEW = WORK / 'client-special-features-protocol-review-01'
MARKER = 'goby-client-special-features-protocol-finalization-v1'
PROFILE_MARKER = 'goby-client-special-features-fixture-v1'
INSPECT_MARKER = 'goby-client-special-features-finalization-inspection-v1'
INPUT_SHA = '8fdd6037e4bf0ca314833f3962b3253722be94319eaa6df9ef65df9b83c27adf'
PROFILE_KEY = 'special_features_profile'


class FinalizerError(Exception):
    """The exact finalizer identity, request or preserved state differs."""


def require(value, message):
    if not value:
        raise FinalizerError(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def protected(path, expected=None, limit=2 << 20):
    require(path.is_absolute() and '..' not in path.parts and path.is_relative_to(WORK), 'An evidence/source path is outside WORK.')
    for entry in reversed((path, *path.parents)):
        info = entry.lstat()
        require(not entry.is_symlink() and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022, 'An input is not root protected.')
    require(path.is_file() and path.stat().st_nlink == 1 and path.stat().st_size <= limit, 'An input file exceeds its bound.')
    raw = path.read_bytes()
    require(expected is None or sha(raw) == expected, 'A pinned input changed.')
    return raw


def load_module(path, expected, name):
    raw = protected(path, expected, 1 << 20)
    result = types.ModuleType(name)
    result.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), result.__dict__)
    return result


def validate_media_response(response, mode, source, prefix_sha256):
    require(response['status'] == (200 if mode == 'full' else 206) and response['complete'] is True and response['failure_type'] is None and
            response['body'] is None and isinstance(response['content_type'], str) and response['content_type'].split(';', 1)[0] == 'video/mp4',
            'An original-resource response is incomplete or has the wrong status/MIME.')
    if mode == 'full':
        require(response['bytes'] == source['bytes'] and response['sha256'] == source['sha256'], 'A full original response differs from the immutable file.')
    else:
        require(mode == 'range' and response['bytes'] == 1024 and response['sha256'] == prefix_sha256 and
                response['headers'].get('Content-Range') == 'bytes 0-1023/' + str(source['bytes']), 'A Range response differs from the immutable prefix.')


def scanned_evidence(op):
    root = op.directory(SCANNED, 0, 0o700, 0)
    op.directory(SCANNED / 'private', 0, 0o700, 0)
    files, total = {}, 0
    for path in sorted(SCANNED.rglob('*')):
        if path.is_dir():
            require(path == SCANNED / 'private', 'An unexpected directory entered the retained scan evidence.')
            continue
        info = op.regular(path, limit=op.MAX_SNAPSHOT_BYTES)
        raw = op.read(path, limit=op.MAX_SNAPSHOT_BYTES)
        total += len(raw)
        require(total <= 160 << 20 and len(files) < 96, 'The retained scan evidence exceeds its bound.')
        files[path.relative_to(SCANNED).as_posix()] = {'sha256': sha(raw), 'bytes': len(raw),
            'identity': {'device': info.st_dev, 'inode': info.st_ino, 'uid': info.st_uid, 'gid': info.st_gid,
                         'mode': stat.S_IMODE(info.st_mode), 'mtime_ns': info.st_mtime_ns, 'ctime_ns': info.st_ctime_ns}}
    require(len(files) == 68 and 'failure.json' in files and 'failed-current-full.json' in files and
            'completed.json' not in files and 'report.json' not in files and
            len([name for name in files if name.endswith('-response.json')]) == 25 and
            not any('full-response' in name or 'range-response' in name for name in files), 'The retained scan is not the exact unfinished 25-request evidence tree.')
    return {'marker': 'goby-client-special-features-scanned-evidence-v1', 'path': str(SCANNED), 'root_identity': root, 'files': files}


def make_finalizer(base, prior):
    class Finalizer(base.Setup):
        def __init__(self, args):
            super().__init__(args)
            self.deadline = time.monotonic() + 300
            self.profile = self.scan_profile = self.old_profile = None
            self.cfg = self.inputs = self.history = self.scanned = self.original = self.created = None
            self.failure1 = self.failure2 = self.scan_receipt = self.first_tree = self.scan_tree = None
            self.output_identity = None

        def private(self, name, value):
            require(re.fullmatch('[a-z0-9-]+[.](json|bin)', name), 'A finalizer evidence name is not canonical.')
            self.op.create(OUTPUT / 'private' / name, value)

        def source_records(self):
            return {'setup_source': {'path': str(Path(__file__).absolute()), 'sha256': self.args.script_sha256},
                    'profile_source': {'path': str(self.args.profile_path), 'sha256': self.args.profile_sha256}}

        def same_base_state(self):
            excluded = ('phase', 'stage', PROFILE_KEY)
            require(self.op.equal_json({key: value for key, value in self.history.items() if key not in excluded},
                                      {key: value for key, value in self.state.items() if key not in excluded}),
                    'An original fixture/source/runtime/account field changed.')

        def check(self, *, cleanup=False):
            op = self.op
            require(time.monotonic() < self.deadline, 'The finalizer deadline expired.')
            for path, expected in self.sources:
                protected(path, expected, 1 << 20)
            require(op.read(op.STATE_FILE) == self.state_bytes and op.sha(op.RUNTIME) == self.state['runtime_sha256'], 'The active state or runtime changed.')
            op.verify_fixture_directories(self.state)
            op.verify_database(self.state)
            require(op.verify_service(self.state) == self.state['process'], 'The exact candidate process changed.')
            if self.output_identity is not None and not cleanup:
                require(op.directory(OUTPUT, 0, 0o700, 0) == self.output_identity, 'The finalizer evidence directory changed identity.')

        def approved(self, label, method, path, actor, body, login, cleanup, media_mode):
            require(actor.role == 'viewer' and self.sequence < 11, 'The one-viewer eleven-request scope was exceeded.')
            if not login and not cleanup:
                expected = {key + '-' + mode: ('/emby/Videos/' + self.item_ids[key] + '/original.mp4', mode)
                            for key in self.old_profile.KINDS for mode in ('full', 'range')}
                require(label in expected and method == 'GET' and body is None and (path, media_mode) == expected[label], 'Only the eight exact original-resource requests are permitted.')
            super().approved(label, method, path, actor, body, login, cleanup, media_mode)

        def record(self, number, label, route):
            path = SCANNED / 'private' / f'{number:04d}-{label}-response.json'
            raw = self.op.read(path)
            witness = self.protocol_review['records'][label]
            require(witness['path'] == str(path) and witness['sha256'] == sha(raw), 'A retained protocol response changed.')
            value = self.op.precise_json(raw)
            require(value['request']['method'] == 'GET' and value['request']['path'] == route and value['request']['actor'] == 'viewer' and
                    value['response']['status'] == 200 and value['response']['complete'] is True and value['response']['failure_type'] is None,
                    'A retained protocol response is incomplete or unowned.')
            return value['response']['body']

        def replay_protocol(self):
            op = self.op
            self.api_items = {}
            route = '/emby/Users/' + self.state['added_viewer']['user_id'] + '/Items/'
            items = {row['id']: row for row in self.scanned['database']['tables']['items']}
            for number, (key, (relative, kind, name)) in enumerate(self.old_profile.FILES.items(), 9):
                item = items[self.item_ids[key]]
                body = self.record(number, 'detail-' + key, route + item['id'])
                api_type = 'Trailer' if key == 'trailer' else kind
                api_name = 'M3e Special Features Positive - Trailer' if key == 'trailer' else name
                require(body['Id'] == item['id'] and body['Type'] == api_type and body['Name'] == api_name and
                        body['Path'] == item['path'] == self.old_profile.ROOT + '/' + relative and body['ParentId'] == item['parent_id'] and
                        len(body['MediaSources']) == 1 and body['MediaSources'][0]['Id'] == 'mediasource_' + item['id'], 'A retained detail lost its exact indexed identity.')
                self.api_items[key] = {field: item[field] for field in ('id', 'type', 'name', 'path', 'relative_path', 'parent_id')}
                self.api_items[key].update(api_type=api_type, api_name=api_name, media_source_id=body['MediaSources'][0]['Id'])
            number = 15
            for key, family, expected in (('positive', 'SpecialFeatures', [self.item_ids[k] for k in ('alpha', 'deleted', 'zeta')]),
                ('positive', 'LocalTrailers', [self.item_ids['trailer']]), ('empty', 'SpecialFeatures', []), ('empty', 'LocalTrailers', [])):
                for fields in (False, True):
                    label = key + '-' + family.lower() + ('-fields' if fields else '-default')
                    url = route + self.item_ids[key] + '/' + family + ('?Fields=Path%2CParentId%2CMediaSources%2CMediaStreams' if fields else '')
                    body = self.record(number, label, url)
                    require(isinstance(body, list) and [row['Id'] for row in body] == expected and
                            (not fields or all(row['ParentId'] == self.item_ids['positive'] for row in body)), 'A retained extra list lost membership, order or ownership.')
                    number += 1
            counts = self.record(23, 'positive-counts', route + self.item_ids['positive'] + '?Fields=ItemCounts')
            require('SpecialFeatureCount' not in counts and counts.get('LocalTrailerCount') == 1, 'The retained parent projection differs from the observed reference.')
            for key in ('library', 'positive_folder', 'empty_folder'):
                item = items[self.item_ids[key]]
                self.api_items[key] = {field: item[field] for field in ('id', 'type', 'name', 'path', 'relative_path', 'parent_id')}
                self.api_items[key].update(api_type=item['type'], api_name=item['name'])

        def load(self):
            a = self.args
            require(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0 and os.environ.get('SSH_CONNECTION'), 'Use root SSH on test-env only.')
            self.profile = load_module(a.profile_path, a.profile_sha256, 'finalizer_profile')
            require(self.profile.MARKER == MARKER, 'The selected finalizer profile differs.')
            self.inputs = json.loads(protected(a.source_inputs, INPUT_SHA))
            values = {key.replace('-', '_'): value for key, value in self.inputs['args'].items()}
            for key in ('operator_path', 'extension_operator_path', 'profile_path', 'origin_setup_path', 'origin_profile_path', 'extension_dir', 'upgrade_report'):
                values[key] = Path(values[key])
            for key in ('candidate_pid', 'candidate_start_ticks'):
                values[key] = int(values[key])
            self.cfg = cfg = types.SimpleNamespace(**values)
            require(cfg.operator_path == WORK / 'prepare-client-fixture.py', 'The historical helper must retain its actual WORK path.')
            self.op = op = load_module(cfg.operator_path, cfg.operator_sha256, 'base_fixture')
            self.scan_profile = load_module(cfg.profile_path, self.profile.PINS['continuation_profile_source'], 'scanned_profile')
            self.old_profile = load_module(cfg.origin_profile_path, self.scan_profile.PINS['origin_profile_source'], 'original_profile')
            self.ext = load_module(cfg.extension_operator_path, cfg.extension_operator_sha256, 'root_extension')
            require(op.WORK == WORK and op.PORT == 18198 and op.UNIT == 'goby-client-m3e.service' and self.ext.OUTPUT == cfg.extension_dir and
                    self.ext.ADDED_ROOT == self.old_profile.ROOT, 'A support source targets another fixture.')
            self.sources = [(Path(__file__).absolute(), a.script_sha256), (a.profile_path, a.profile_sha256),
                (Path(self.inputs['tool']), self.profile.PINS['continuation_setup_source']), (cfg.operator_path, cfg.operator_sha256),
                (cfg.profile_path, self.profile.PINS['continuation_profile_source']), (cfg.origin_setup_path, self.scan_profile.PINS['origin_setup_source']),
                (cfg.origin_profile_path, self.scan_profile.PINS['origin_profile_source']), (cfg.extension_operator_path, cfg.extension_operator_sha256)]
            op.command = self.command
            op.host_inputs(initial=False)
            self.lock = os.open(op.LOCK, os.O_RDONLY | os.O_NOFOLLOW)
            require(op.identity(os.fstat(self.lock)) == op.identity(op.regular(op.LOCK)), 'The fixture lock changed.')
            fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            self.state_bytes = op.read(op.STATE_FILE)
            require(sha(self.state_bytes) == a.state_sha256, 'The explicitly selected active state changed.')
            self.state = op.precise_json(self.state_bytes)
            first_files = {'history': ('before-state.json', 'origin_before_state'), 'original': ('before-full.json', 'origin_before_snapshot'),
                           'created': ('failed-current-full.json', 'origin_current_snapshot'), 'failure1': ('failure.json', 'origin_failure')}
            for attribute, (name, pin) in first_files.items():
                raw = op.read(FIRST / name, limit=op.MAX_SNAPSHOT_BYTES)
                require(sha(raw) == self.scan_profile.PINS[pin], 'An original creation artifact changed.')
                setattr(self, attribute, op.precise_json(raw))
            for attribute, name, pin in (('scanned', 'failed-current-full.json', 'continuation_snapshot'), ('failure2', 'failure.json', 'continuation_failure')):
                raw = op.read(SCANNED / name, limit=op.MAX_SNAPSHOT_BYTES)
                require(sha(raw) == self.profile.PINS[pin], 'A retained scan artifact changed.')
                setattr(self, attribute, op.precise_json(raw))
            self.same_base_state()
            self.first_tree = prior.origin_evidence(op)
            self.scan_tree = scanned_evidence(op)
            require(op.equal_json(self.first_tree, op.load(SCANNED / 'origin-evidence.json')) and
                    op.sha(SCANNED / 'origin-state.json') == self.scan_profile.PINS['origin_state'], 'The original failure lineage changed.')
            self.library_id, self.root_id, self.job_id = self.scan_profile.LIBRARY_ID, self.scan_profile.ROOT_ID, self.profile.SCAN_JOB_ID
            require(self.failure2['result'] == 'retained_for_review' and self.failure2['phase'] == 'scan_complete' and
                    (self.failure2['library_id'], self.failure2['root_id'], self.failure2['job_id']) == (self.library_id, self.root_id, self.job_id) and
                    all([row.get(key) for key in ('login_status', 'logout_status', 'exact_status')] == [200, 204, 401]
                        for row in self.failure2['authentication'].values()), 'The retained scan failure or its closed actors differs.')
            self.extension = op.load(cfg.extension_dir / 'completed.json')
            ext_report = op.load(cfg.extension_dir / 'report.json')
            require(op.sha(cfg.extension_dir / 'completed.json') == cfg.extension_completed_sha256 and op.sha(cfg.extension_dir / 'report.json') == cfg.extension_report_sha256,
                    'The root-extension proof changed.')
            upgrade_dir = Path(self.history['upgrade']['evidence_directory'])
            require(upgrade_dir.parent == WORK and re.fullmatch('client-upgrade-[0-9a-f]{32}', upgrade_dir.name), 'The historical upgrade directory differs.')
            upgraded = op.load(upgrade_dir / 'completed.json')
            require(op.sha(upgrade_dir / 'completed.json') == cfg.upgrade_completed_sha256 and op.sha(cfg.upgrade_report) == cfg.upgrade_report_sha256,
                    'The historical completed upgrade changed.')
            base.validate_extension_chain(op, self.history, self.extension, ext_report, upgraded, op.load(cfg.upgrade_report), cfg)
            artifacts = upgraded['schema_artifacts']
            require(artifacts['operator'] == {'path': str(cfg.operator_path), 'sha256': cfg.operator_sha256}, 'The historical upgrade operator differs.')
            self.source_binding = {'path': artifacts['source'], 'manifest_sha256': artifacts['source_manifest_sha256'],
                'full_report_path': artifacts['product_verification']['report_path'], 'full_report_sha256': artifacts['product_verification']['report_sha256']}
            verified = op.verify_product_upgrade(Path(upgraded['source']), cfg.candidate_sha256, Path(self.source_binding['path']), self.source_binding['manifest_sha256'],
                Path(self.source_binding['full_report_path']), self.source_binding['full_report_sha256'], 27)
            require(op.equal_json(verified, artifacts['product_verification']) and self.extension['source_manifest_sha256'] == self.source_binding['manifest_sha256'] and
                    self.extension['full_report_sha256'] == self.source_binding['full_report_sha256'], 'The complete product/source report changed.')
            self.upgrade_binding = {'completed_path': str(upgrade_dir / 'completed.json'), 'completed_sha256': cfg.upgrade_completed_sha256,
                'report_path': str(cfg.upgrade_report), 'report_sha256': cfg.upgrade_report_sha256, 'old_process': upgraded['old_process'], 'new_process': upgraded['new_process']}
            self.extension_binding = {'completed_path': str(cfg.extension_dir / 'completed.json'), 'completed_sha256': cfg.extension_completed_sha256,
                'report_path': str(cfg.extension_dir / 'report.json'), 'report_sha256': cfg.extension_report_sha256,
                **{key: self.extension[key] for key in ('old_process', 'new_process', 'old_runtime_sha256', 'new_runtime_sha256')}}
            self.check()
            self.media = self.ext.media_witness(op, self.state)
            require(op.equal_json(self.media, op.load(SCANNED / 'media-before.json')), 'A media member changed since the scan.')
            scan_before = op.precise_json(op.read(SCANNED / 'before-full.json', limit=op.MAX_SNAPSHOT_BYTES))
            prior.preserved(op, self.created, scan_before)
            scan_viewer = next(row for row in self.scanned['database']['tables']['sessions'] if row['id'] == self.failure2['authentication']['viewer']['session_id'])
            self.scan_receipt = {'library_id': self.library_id, 'root_id': self.root_id, 'job_id': self.job_id, 'authentication': self.failure2['authentication'],
                'device_id': scan_viewer['device_id'], 'client_name': base.CLIENT,
                'window': {'before': self.original['database']['metadata']['captured_at'], 'after': self.scanned['database']['metadata']['captured_at']},
                'continuation_window': {'before': scan_before['database']['metadata']['captured_at'], 'after': self.scanned['database']['metadata']['captured_at']}}
            self.retained_proof = self.scan_profile.validate_transition(op, self.old_profile, self.original, self.created, self.scanned, self.state,
                self.failure1, self.scan_receipt, self.media)
            self.item_ids = self.retained_proof['item_ids']
            raw = op.read(REVIEW / 'report.json')
            require(sha(raw) == self.profile.PINS['retained_structure_proof'] and op.equal_json(op.precise_json(raw)['proof'], self.retained_proof), 'The retained structure proof changed.')
            raw = op.read(REVIEW / 'protocol-proof.json')
            require(sha(raw) == self.profile.PINS['retained_protocol_proof'], 'The retained protocol proof changed.')
            self.protocol_review = op.precise_json(raw)
            require(self.protocol_review['result'] == 'passed' and self.protocol_review['direct_count'] == 6 and self.protocol_review['list_count'] == 8 and
                    self.protocol_review['full_media_reads'] == self.protocol_review['range_reads'] == 0, 'The retained protocol proof overstates completed media work.')
            self.replay_protocol()
            self.current = op.preservation_snapshot(self.state, 27)
            self.old_profile.validate_structure(op, self.current, self.state)
            if a.mode == 'prepare':
                require(not op.exists(OUTPUT) and a.receipt_sha256 is None and a.setup_report_sha256 is None and
                        sha(self.state_bytes) == self.profile.PINS['continuation_state'] and self.state['phase'] == 'continuing_special_features_fixture' and
                        self.state['stage'] == 'scan_complete' and self.state[PROFILE_KEY]['evidence_directory'] == str(SCANNED) and
                        (self.state[PROFILE_KEY]['library_id'], self.state[PROFILE_KEY]['root_id'], self.state[PROFILE_KEY]['job_id']) == (self.library_id, self.root_id, self.job_id),
                        'Only the exact unconsumed scanned failure may be finalized.')
                prior.preserved(op, self.scanned, self.current)
                self.profile.require_quiescent(self.current)
                require(not any(row['reported_device_id'] == self.device for row in self.current['database']['tables']['devices']), 'The finalizer device is not fresh.')
                self.before, self.failed_state_bytes = self.current, self.state_bytes
            else:
                require(not a.check_only and a.receipt_sha256 and a.setup_report_sha256 and self.state['phase'] == 'ready' and self.state['stage'] == 'complete',
                        'Inspection requires an explicitly completed finalization.')

        def begin(self):
            op = self.op
            OUTPUT.mkdir(mode=0o700)
            (OUTPUT / 'private').mkdir(mode=0o700)
            self.output_identity = op.directory(OUTPUT, 0, 0o700, 0)
            op.create(OUTPUT / 'OWNER.json', {'marker': MARKER, 'identity': self.output_identity, 'source_state_sha256': self.profile.PINS['continuation_state']})
            op.create(OUTPUT / 'continuation-state.json', self.failed_state_bytes)
            op.create(OUTPUT / 'continuation-evidence.json', self.scan_tree)
            op.create(OUTPUT / 'before-full.json', self.before)
            op.create(OUTPUT / 'media-before.json', self.media)
            self.window = {'before': op.postgres('SELECT clock_timestamp()::text;', op.ROLE)}
            self.private('verification-window.json', self.window)
            alias = op.load(op.AV_BROWSER)
            self.secrets.add(alias['viewer']['password'])
            self.actors = {'viewer': base.Actor(self, 'viewer', alias['viewer'])}
            self.check()
            self.private('phase-prepared-intent.json', {'source_state_sha256': sha(self.state_bytes), 'request_budget': 11})
            self.state.update(phase='finalizing_special_features_fixture', stage='finalization_prepared')
            self.state[PROFILE_KEY] = {'marker': PROFILE_MARKER, 'phase': 'finalization_prepared', 'evidence_directory': str(OUTPUT),
                'library_id': self.library_id, 'root_id': self.root_id, 'job_id': self.job_id,
                'source_state_sha256': self.profile.PINS['continuation_state']}
            self.same_base_state()
            op.save_state(self.state)
            self.state_bytes = op.read(op.STATE_FILE)
            self.phase = 'prepared'
            self.private('phase-prepared.json', {'state_sha256': sha(self.state_bytes)})

        def media_capture(self):
            viewer = self.actors['viewer']
            viewer.login()
            self.phase = 'media'
            for key in self.old_profile.KINDS:
                path = '/emby/Videos/' + self.item_ids[key] + '/original.mp4'
                item = self.api_items[key]
                source = self.media['special']['files']['Movies/' + item['relative_path']]
                local = self.ext.protected_bytes(Path(item['path']), (0o644,), 2 << 20)
                require(len(local) == source['bytes'] and sha(local) == source['sha256'], 'An original resource changed before its read.')
                for mode in ('full', 'range'):
                    self.allowed.add(('viewer', path, mode))
                    response = self.request(key + '-' + mode, 'GET', path, viewer, media_mode=mode)
                    validate_media_response(response, mode, source, sha(local[:1024]))
            viewer.logout()
            require(viewer.closed and not self.journal_failures and self.sequence == 11, 'The exact finalizer requests or cleanup are incomplete.')

        def media_proof(self):
            result = {}
            for index, key in enumerate(self.old_profile.KINDS):
                item = self.api_items[key]
                source = self.media['special']['files']['Movies/' + item['relative_path']]
                local = self.ext.protected_bytes(Path(item['path']), (0o644,), 2 << 20)
                require(len(local) == source['bytes'] and sha(local) == source['sha256'] and
                        self.ext.file_identity(Path(item['path']).lstat()) == source['identity'], 'An owned resource changed during byte verification.')
                result[key] = {}
                for offset, mode in enumerate(('full', 'range'), 2):
                    path = OUTPUT / 'private' / f'{index * 2 + offset:04d}-{key}-{mode}-response.json'
                    raw = self.op.read(path)
                    record = self.op.precise_json(raw)
                    response = record['response']
                    require(record['request'] == {'method': 'GET', 'path': '/emby/Videos/' + item['id'] + '/original.mp4',
                        'actor': 'viewer', 'media_mode': mode}, 'An original-resource response is unowned.')
                    validate_media_response(response, mode, source, sha(local[:1024]))
                    result[key][mode] = {'path': str(path), 'sha256': sha(raw), 'status': response['status'],
                        'bytes': response['bytes'], 'body_sha256': response['sha256'], 'complete': True}
            return result

        def continuation_chain(self):
            p = self.scan_profile.PINS
            return {'marker': self.scan_profile.MARKER,
                'origin_failure': {'path': str(FIRST / 'failure.json'), 'sha256': p['origin_failure']},
                'origin_state': {'path': str(SCANNED / 'origin-state.json'), 'sha256': p['origin_state']},
                'origin_before_state': {'path': str(FIRST / 'before-state.json'), 'sha256': p['origin_before_state']},
                'origin_before_snapshot': {'path': str(FIRST / 'before-full.json'), 'sha256': p['origin_before_snapshot']},
                'origin_current_snapshot': {'path': str(FIRST / 'failed-current-full.json'), 'sha256': p['origin_current_snapshot']},
                'origin_library_ack': {'path': str(FIRST / 'private/library-ack.json'), 'sha256': p['origin_library_ack']},
                'origin_evidence': {'path': str(SCANNED / 'origin-evidence.json'), 'sha256': self.op.sha(SCANNED / 'origin-evidence.json')},
                'origin_setup_source': {'path': str(self.cfg.origin_setup_path), 'sha256': p['origin_setup_source']},
                'origin_profile_source': {'path': str(self.cfg.origin_profile_path), 'sha256': p['origin_profile_source']},
                'library_id': self.library_id, 'root_id': self.root_id,
                'creation_admin_session_id': self.retained_proof['creation_admin_session_id'],
                'new_scan_admin_session_id': self.retained_proof['native_session_id'], 'new_viewer_session_id': self.retained_proof['viewer_session_id'],
                'scan_job_id': self.job_id, 'creation_requests': 5, 'continuation_library_creates': 0, 'continuation_scan_dispatches': 1,
                'original_failure_preserved': True, 'creation_admin_revoked': True, 'continuation_sessions_revoked': True}

        def finalization_chain(self, proof):
            p = self.profile.PINS
            return {'marker': MARKER,
                'continuation_failure': {'path': str(SCANNED / 'failure.json'), 'sha256': p['continuation_failure']},
                'continuation_state': {'path': str(OUTPUT / 'continuation-state.json'), 'sha256': p['continuation_state']},
                'continuation_snapshot': {'path': str(SCANNED / 'failed-current-full.json'), 'sha256': p['continuation_snapshot']},
                'continuation_evidence': {'path': str(OUTPUT / 'continuation-evidence.json'), 'sha256': self.op.sha(OUTPUT / 'continuation-evidence.json')},
                'continuation_setup_source': {'path': self.inputs['tool'], 'sha256': p['continuation_setup_source']},
                'continuation_profile_source': {'path': str(self.cfg.profile_path), 'sha256': p['continuation_profile_source']},
                'retained_structure_proof': {'path': str(REVIEW / 'report.json'), 'sha256': p['retained_structure_proof']},
                'retained_protocol_proof': {'path': str(REVIEW / 'protocol-proof.json'), 'sha256': p['retained_protocol_proof']},
                'scan_job_id': self.job_id, 'viewer_user_id': self.state['added_viewer']['user_id'],
                'new_viewer_session_id': proof['finalizer_session_id'], 'new_device_id': proof['finalizer_device_id'],
                'request_count': 11, 'full_resource_count': 4, 'range_resource_count': 4, 'library_creates': 0, 'scan_dispatches': 0,
                'retained_failures_preserved': True, 'new_viewer_revoked': True,
                'parent_projection': {'special_feature_count_present': False, 'local_trailer_count': 1}}

        def finish(self):
            op = self.op
            require(not self.journal_failures and self.actors['viewer'].closed and self.sequence == 11, 'The one owned finalizer did not close completely.')
            self.window['after'] = op.postgres('SELECT clock_timestamp()::text;', op.ROLE)
            self.after = op.preservation_snapshot(self.state, 27)
            receipt = {'marker': PROFILE_MARKER, 'phase': 'complete', 'schema': 27, 'profile_version': 3,
                'fixture_tag': self.state['tag'], 'server_id': self.state['server_id'], 'source_state_sha256': self.profile.PINS['continuation_state'],
                'candidate': self.candidate(), 'source': self.source_binding, 'upgrade': self.upgrade_binding, 'extension': self.extension_binding,
                'library_id': self.library_id, 'root_id': self.root_id, 'job_id': self.job_id, 'device_id': self.device, 'client_name': base.CLIENT,
                'authentication': {'viewer': self.actors['viewer'].status}, 'window': self.window, **self.source_records()}
            retained, proof = self.profile.validate(op, self.old_profile, self.scan_profile, self.original, self.created, self.scanned, self.after,
                self.state, self.failure1, self.scan_receipt, receipt, self.media)
            require(op.equal_json(retained, self.retained_proof) and op.equal_json(prior.origin_evidence(op), self.first_tree) and
                    op.equal_json(scanned_evidence(op), self.scan_tree) and op.equal_json(self.ext.media_witness(op, self.state), self.media),
                    'A prior failure tree or media member changed.')
            receipt.update(retained_scan_proof=retained, proof=proof, continuation=self.continuation_chain(), finalization=self.finalization_chain(proof),
                item_ids=self.item_ids, items=self.api_items, media_proof=self.media_proof(),
                library={'id': self.library_id, 'root_id': self.root_id, 'name': self.old_profile.LIBRARY_NAME, 'collection_type': 'movies', 'path': self.old_profile.ROOT},
                users={'admin_id': self.state['admin_id'], 'viewer_id': self.state['viewer_id'], 'av_user_id': self.state['added_viewer']['user_id']},
                ordinary_access={'primary': {'EnableAllFolders': True, 'EnableMediaPlayback': True}, 'av': {'EnableAllFolders': True, 'EnableMediaPlayback': True}},
                aliases={'primary': {'path': str(op.BROWSER), 'sha256': self.state['browser_sha256'], 'account_key': 'viewer'},
                         'av': {'path': str(op.AV_BROWSER), 'sha256': self.state['added_viewer']['browser_sha256'], 'account_key': 'viewer'}})
            op.create(OUTPUT / 'after-full.json', self.after)
            receipt['before_snapshot'] = {'path': str(OUTPUT / 'before-full.json'), 'sha256': op.sha(OUTPUT / 'before-full.json', limit=op.MAX_SNAPSHOT_BYTES)}
            receipt['after_snapshot'] = {'path': str(OUTPUT / 'after-full.json'), 'sha256': op.sha(OUTPUT / 'after-full.json', limit=op.MAX_SNAPSHOT_BYTES)}
            receipt['after_ledger'] = self.ledger(self.after, OUTPUT / 'after-full.json', receipt['after_snapshot']['sha256'])
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
                'state_sha256': sha(self.state_bytes), 'receipt_path': str(OUTPUT / 'completed.json'), 'receipt_sha256': receipt_sha,
                **self.source_records(), 'candidate': self.candidate(), 'continuation': receipt['continuation'], 'finalization': receipt['finalization'],
                'proof': proof, 'retained_scan_proof': retained, 'media_proof': receipt['media_proof'],
                'protocol': {key: value for key, value in self.records.items() if key != 'viewer-login'},
                'boundary': 'Four-stage API fixture/protocol completion with two unchanged failures. No original-client UI acceptance claim.'}
            op.create(OUTPUT / 'report.json', self.sanitize(report))

        def inspect(self):
            op, a = self.op, self.args
            require(not op.exists(INSPECTION) and op.sha(OUTPUT / 'completed.json') == a.receipt_sha256 and op.sha(OUTPUT / 'report.json') == a.setup_report_sha256,
                    'The new inspection scope is occupied or its completion evidence changed.')
            receipt, report = op.load(OUTPUT / 'completed.json'), op.load(OUTPUT / 'report.json')
            require(self.state[PROFILE_KEY] == {'marker': PROFILE_MARKER, 'phase': 'complete', 'receipt_path': str(OUTPUT / 'completed.json'),
                'receipt_sha256': a.receipt_sha256, 'library_id': self.library_id, 'root_id': self.root_id} and
                receipt['marker'] == PROFILE_MARKER and receipt['phase'] == 'complete' and receipt['profile_version'] == 3 and
                receipt['candidate'] == self.candidate() and receipt['source'] == self.source_binding and receipt['upgrade'] == self.upgrade_binding and
                receipt['extension'] == self.extension_binding and all(receipt[key] == value for key, value in self.source_records().items()),
                    'The completed finalization no longer binds the current candidate.')
            require(op.sha(OUTPUT / 'continuation-state.json') == self.profile.PINS['continuation_state'] and
                    op.equal_json(op.load(OUTPUT / 'continuation-evidence.json'), self.scan_tree), 'The retained continuation changed.')
            snapshots = {}
            for key, name in (('before_snapshot', 'before-full.json'), ('after_snapshot', 'after-full.json')):
                path = OUTPUT / name
                require(receipt[key]['path'] == str(path) and op.sha(path, limit=op.MAX_SNAPSHOT_BYTES) == receipt[key]['sha256'], 'A finalizer snapshot changed.')
                snapshots[key] = op.precise_json(op.read(path, limit=op.MAX_SNAPSHOT_BYTES))
            prior.preserved(op, self.scanned, snapshots['before_snapshot'])
            prior.preserved(op, snapshots['after_snapshot'], self.current)
            retained, proof = self.profile.validate(op, self.old_profile, self.scan_profile, self.original, self.created, self.scanned,
                snapshots['after_snapshot'], self.state, self.failure1, self.scan_receipt, receipt, self.media)
            require(op.equal_json(receipt['retained_scan_proof'], retained) and op.equal_json(receipt['proof'], proof) and
                    receipt['continuation'] == self.continuation_chain() and receipt['finalization'] == self.finalization_chain(proof) and
                    op.equal_json(receipt['media_proof'], self.media_proof()) and report['result'] == 'passed' and report['phase'] == 'complete' and
                    report['state_sha256'] == sha(self.state_bytes) and report['receipt_sha256'] == a.receipt_sha256 and
                    all(op.equal_json(report[key], receipt[key]) for key in ('proof', 'retained_scan_proof', 'continuation', 'finalization')),
                    'The completion report or proof changed.')
            self.check()
            INSPECTION.mkdir(mode=0o700)
            op.create(INSPECTION / 'current-full.json', self.current)
            snapshot_path, snapshot_sha = str(INSPECTION / 'current-full.json'), op.sha(INSPECTION / 'current-full.json', limit=op.MAX_SNAPSHOT_BYTES)
            summary = op.preservation_summary(self.current)
            result = {'marker': INSPECT_MARKER, 'result': 'passed', 'phase': 'complete', 'schema': 27, 'profile_marker': PROFILE_MARKER,
                'process': self.state['process'], 'binary_sha256': self.state['binary_sha256'], 'runtime_sha256': self.state['runtime_sha256'],
                'state_sha256': sha(self.state_bytes), 'receipt_path': str(OUTPUT / 'completed.json'), 'receipt_sha256': a.receipt_sha256,
                **self.source_records(), 'source': self.source_binding, 'upgrade': self.upgrade_binding, 'extension': self.extension_binding,
                'proof': proof, 'retained_scan_proof': retained, 'continuation': receipt['continuation'], 'finalization': receipt['finalization'],
                'profile': {'receipt_path': str(OUTPUT / 'completed.json'), 'receipt_sha256': a.receipt_sha256, 'library_id': self.library_id,
                            'root_id': self.root_id, 'item_ids': self.item_ids, 'resource_count': 4, 'reserved_path_count': 3},
                'verification': dict.fromkeys(('complete_schema27', 'exact_nonempty_profile', 'setup_old_rows_preserved', 'current_matches_completed',
                                              'credentials_recovery_media_preserved', 'new_setup_sessions_revoked'), True),
                'inspection': {'table_count': 35, 'item_count': 22, 'library_count': 4, 'database_sha256': summary['database_sha256'],
                               'current_snapshot_path': snapshot_path, 'current_snapshot_sha256': snapshot_sha},
                'current_snapshot_path': snapshot_path, 'current_snapshot_sha256': snapshot_sha,
                'boundary': 'Fresh finalization setup-point proof. Later UI preparation requires its separate ledger.'}
            op.create(INSPECTION / 'report.json', result)
            self.phase = 'inspection_complete'

        def run(self):
            failure = None
            try:
                self.load()
                if self.args.mode == 'inspect':
                    self.inspect()
                elif self.args.check_only:
                    print(json.dumps({'result': 'preflight_passed', 'http_requests': 0, 'new_evidence_writes': 0,
                        'retained_scan_proof': self.retained_proof, 'retained_direct_count': 6, 'retained_list_count': 8,
                        'parent_projection': {'special_feature_count_present': False, 'local_trailer_count': 1}}), flush=True)
                    return 0
                else:
                    self.begin()
                    try:
                        self.media_capture()
                    finally:
                        self.deadline = time.monotonic() + 60
                        try:
                            self.actors['viewer'].logout()
                        except Exception:
                            failure = 'OwnedSessionCleanupFailed'
                    require(failure is None, 'The new owned viewer did not close.')
                    self.finish()
            except Exception as error:
                failure = failure or type(error).__name__
                if self.output_identity is not None:
                    try:
                        self.op.create(OUTPUT / 'failed-current-full.json', self.op.preservation_snapshot(self.state, 27))
                    except Exception:
                        pass
                    try:
                        self.op.create(OUTPUT / 'failure.json', {'marker': MARKER, 'result': 'retained_for_review', 'phase': self.phase,
                            'failure_type': failure, 'authentication': {key: actor.status for key, actor in self.actors.items()},
                            'journal_failures': self.journal_failures, 'retry_permitted': False})
                    except Exception:
                        pass
            finally:
                if self.lock is not None:
                    os.close(self.lock)
                    self.lock = None
            print(json.dumps({'result': 'passed' if failure is None else 'retained_for_review', 'phase': self.phase, 'failure_type': failure,
                'evidence': str(INSPECTION if self.args.mode == 'inspect' else OUTPUT), 'request_count': self.sequence,
                'owned_cleanup': {key: {field: actor.status[field] for field in ('logout_status', 'exact_status')} for key, actor in self.actors.items()},
                'journal_failures': self.journal_failures}), flush=True)
            return 0 if failure is None else 1

    return Finalizer


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('prepare', 'inspect'))
    parser.add_argument('--script-sha256', required=True)
    parser.add_argument('--profile-path', required=True, type=Path)
    parser.add_argument('--profile-sha256', required=True)
    parser.add_argument('--source-inputs', required=True, type=Path)
    parser.add_argument('--state-sha256', required=True)
    parser.add_argument('--receipt-sha256')
    parser.add_argument('--setup-report-sha256')
    parser.add_argument('--check-only', action='store_true')
    args = parser.parse_args()
    for key, value in vars(args).items():
        if key.endswith('_sha256') and value is not None:
            require(re.fullmatch('[0-9a-f]{64}', value), 'An explicit digest is malformed.')
    require(sha(Path(__file__).read_bytes()) == args.script_sha256, 'The selected finalizer source changed.')
    os.umask(0o077)
    signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(TimeoutError('A bounded request expired.')))
    inputs = json.loads(protected(args.source_inputs, INPUT_SHA))
    profile = load_module(args.profile_path, args.profile_sha256, 'finalizer_profile_entry')
    prior = load_module(Path(inputs['tool']), profile.PINS['continuation_setup_source'], 'retained_continuation_reader')
    base = load_module(Path(inputs['args']['origin-setup-path']), '84af95f1d34227dc9c465b73545c6de6939963d44fb3fc2d2df50e7f0f759465', 'retained_transport')
    return make_finalizer(base, prior)(args).run()


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except Exception as error:
        print(json.dumps({'result': 'retained_for_review', 'failure_type': type(error).__name__, 'retry_permitted': False}), file=sys.stderr)
        raise SystemExit(1)
