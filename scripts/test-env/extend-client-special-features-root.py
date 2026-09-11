#!/usr/bin/env python3
"""Append one receipted media root to the isolated schema27 candidate.

This exclusive operation does not create libraries, scan, authenticate over
HTTP, replace binaries, migrate, restore, or change the primary service. It
retains every failure and never rolls back, resumes, or retries a service action.
The original-Movie schema27 client run precedes positive-library setup. That
later setup requires a receipt-backed nonempty-fixture inspection contract;
removing the existing empty-extra validation is not an accepted substitute.
"""

from __future__ import annotations

import argparse
import copy
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import types

sys.dont_write_bytecode = True
WORK = Path('/opt/goby-test/exec-work-m3e')
OUTPUT = WORK / 'client-special-features-root-extension-v1'
MARKER = 'goby-client-special-features-root-extension-v1'
STATE_KEY = 'media_root_extension'
ADDED_ROOT = '/opt/goby-fixtures/client-special-features-m3e-v1/Movies'
OLD_ROOTS = tuple('/opt/goby-fixtures/client-m3e/' + name for name in ('Movies', 'TV', 'Music'))
OLD_RUNTIME_SHA = '746b2092897829b15a169131059d3792767b3539aca9d53ddb9e79f954dd1874'
MEDIA = Path('/opt/goby-fixtures/client-special-features-m3e-v1')
AUX_MEDIA = Path('/opt/goby-fixtures/client-aux-m3e-v1')
MEDIA_RECEIPTS = {
    MEDIA / 'mains-manifest.json': ('d53dde28a3c5ed5a67c310e0520639bd8f54233f93b368cf2539b88091ae426a', 0o644),
    WORK / 'special-features-media-v1/mains/completed.json': ('b753494e27902d62bfc6021f232c38c5e69fd4374f8e4a3a92b92fe5622a1f97', 0o600),
    MEDIA / 'extras-manifest.json': ('5bfb18a68440d0144f9eba8915561647094208664a287f263ae9d0d1f83b6e38', 0o644),
    WORK / 'special-features-media-v1/extras/completed.json': ('dae031f6fe222faa28900b65d62271823a565e9866cf758f3f45212115dcd187', 0o600),
    WORK / 'reference-special-features-v1/mains-indexed.json': ('53e883235f6dbcbe15cfd4046ddc8f99578f64e4266bb6623775028d0d4668f7', 0o600),
    WORK / 'reference-special-features-v1/mains/export/report.json': ('42f94024a5780ad94fc0c0f4ad525e65faa1b02494d823574e964b1d8a0cf3c4', 0o600),
    WORK / 'reference-special-features-v1/extras/export/report.json': ('8e4f9323495408540e6d97ecba19a6ba31f679ca0129a4c8eaf4d5277019c51e', 0o600),
    AUX_MEDIA / 'manifest.json': ('dad99c4883fde8bba00c9061179341b1a5869dd20212de55b92e0a53353703a7', 0o644),
    WORK / 'reference-auxiliary-libraries-v1/export/report.json': ('457a4d88f86617c448b980f31b7dd5b7d8bbeb31e3cfe807103371859f2d7998', 0o600),
}
PUBLIC_UPGRADE_FIELDS = {'id', 'phase', 'from_sha256', 'to_sha256', 'old_process', 'new_process', 'rollback_binary',
    'evidence_directory', 'protocol_version', 'product_version', 'preservation', 'after_preservation',
    'from_schema', 'to_schema', 'schema_artifacts'}
HASH = re.compile('[0-9a-f]{64}')


class ExtensionError(Exception):
    """A specific extension intent or preservation boundary was not proven."""


def require(condition, message):
    if not condition:
        raise ExtensionError(message)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def protected_bytes(path, modes=(0o600, 0o644), limit=2 << 20):
    require(path.is_absolute() and '..' not in path.parts, 'A selected input path is not canonical and absolute.')
    for parent in reversed(path.parents):
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                'An input ancestor is not protected and root-owned.')
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == info.st_gid == 0 and info.st_nlink == 1 and
            stat.S_IMODE(info.st_mode) in modes and info.st_size <= limit, 'A selected input file is not protected.')
    def version(value):
        return (value.st_dev, value.st_ino, value.st_uid, value.st_gid, value.st_mode, value.st_nlink,
                value.st_size, value.st_mtime_ns, value.st_ctime_ns)
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as handle:
        require(version(os.fstat(handle.fileno())) == version(info), 'An input changed while opening.')
        raw = handle.read(limit + 1)
        require(version(os.fstat(handle.fileno())) == version(info), 'An input changed while reading.')
    require(len(raw) == info.st_size and version(path.lstat()) == version(info), 'An input changed after reading.')
    return raw


def extend_runtime(raw):
    """Preserve every byte except the one exact allowed-root value."""
    require(isinstance(raw, bytes) and 0 < len(raw) <= 65536, 'The private runtime file exceeds its bound.')
    lines, keys, replacement = raw.splitlines(keepends=True), set(), []
    expected = b"GOBY_MEDIA_ROOTS='" + ':'.join(OLD_ROOTS).encode() + b"'\n"
    updated = b"GOBY_MEDIA_ROOTS='" + ':'.join((*OLD_ROOTS, ADDED_ROOT)).encode() + b"'\n"
    for line in lines:
        match = re.fullmatch(rb"([A-Z][A-Z0-9_]*)='([^'\r\n\x00]*)'\n", line)
        require(match is not None and match[1] not in keys, 'The runtime syntax or key population is ambiguous.')
        keys.add(match[1])
        if match[1] == b'GOBY_MEDIA_ROOTS':
            require(line == expected, 'The allowed roots are not the original exact three paths.')
            replacement.append(updated)
        else:
            replacement.append(line)
    require(b'GOBY_MEDIA_ROOTS' in keys, 'The allowed-root field is missing.')
    return b''.join(replacement)


def validate_receipt_chain(op, args, state, completed, report):
    process = {'pid': args.candidate_pid, 'start_ticks': args.candidate_start_ticks, 'boot_id': args.candidate_boot_id}
    require(state.get('marker') == op.MARKER and state.get('phase') == 'ready' and state.get('schema') == 27 and
            state.get('process') == process and state.get('binary_sha256') == args.candidate_sha256 and
            STATE_KEY not in state and state.get('runtime_sha256') == OLD_RUNTIME_SHA and
            not any(state.get(key) for key in ('start_pending', 'credentials_pending', 'binary_pending', 'unit_pending', 'directory_pending')),
            'The candidate is not the exact untouched, completed schema27 starting state.')
    require(op.equal_json(state.get('upgrade'), completed) and completed.get('phase') == 'complete' and
            completed.get('to_schema') == 27 and completed.get('to_sha256') == args.candidate_sha256 and
            completed.get('new_process') == process and completed.get('installed_binary_sha256') == args.candidate_sha256 and
            completed.get('installed_binary_identity') == state.get('binary_identity') and
            completed.get('evidence_directory') == str(args.upgrade_dir), 'The completed upgrade does not bind this installed process.')
    artifacts = completed.get('schema_artifacts', {})
    require(artifacts.get('source') == str(args.source) and artifacts.get('source_manifest_sha256') == args.source_manifest_sha256 and
            artifacts.get('operator') == {'path': str(args.operator_path), 'sha256': args.operator_sha256} and
            artifacts.get('product_verification', {}).get('report_path') == str(args.full_report) and
            artifacts['product_verification'].get('report_sha256') == args.full_report_sha256,
            'The product source, full verification or reviewed upgrade operator is not explicitly bound.')
    projection = report.get('upgrade', {})
    require(report.get('marker') == op.MARKER and report.get('result') == 'ready' and report.get('phase') == 'ready' and
            report.get('process') == process and report.get('binary_sha256') == args.candidate_sha256 and
            set(projection) == PUBLIC_UPGRADE_FIELDS and all(op.equal_json(projection[key], completed.get(key)) for key in PUBLIC_UPGRADE_FIELDS),
            'The public upgrade result does not match the completed private receipt.')


def require_quiescent(snapshot, state):
    tables = snapshot['database']['tables']
    require(snapshot.get('schema') == 27 and len(tables) == 35 and len(tables.get('items', [])) == 13 and
            {row['id'] for row in tables.get('libraries', [])} == {entry['id'] for entry in state['libraries'].values()} and
            len(tables['libraries']) == 3, 'The extension requires the original thirteen-item, three-library schema27 fixture.')
    require(tables.get('encoding_jobs') == [], 'Encoding state exists; no encoder work may cross this restart.')
    for table, field in (('scan_jobs', 'status'), ('task_runs', 'state'), ('task_run_children', 'state')):
        require(not any(str(row.get(field, '')).lower() in {'waiting', 'pending', 'queued', 'running', 'stopping'} for row in tables[table]),
                'A scan or scheduled operation is still active.')
    sessions = {row['id']: row for row in tables['sessions']}
    for row in tables['play_sessions']:
        require(row.get('state') not in ('Playing', 'Paused'), 'Playback is still active.')
        if row.get('state') == 'Prepared':
            credential = sessions.get(row.get('auth_session_id'), {})
            require(isinstance(row.get('user_id'), str) and row['user_id'] and isinstance(row.get('auth_session_id'), str) and
                    row['auth_session_id'] and 'started_at' in row and row['started_at'] is None and row.get('counted') is False and
                    'application_client_id' in row and row['application_client_id'] is None and
                    credential.get('kind') == 'emby' and credential.get('user_id') == row.get('user_id') and
                    credential.get('revoked_at') is not None, 'Prepared history lacks an unstarted, already-revoked ordinary credential.')


def compare_after_extension(op, before, after, old_state, new_state, new_runtime_sha):
    op.validate_preservation_snapshot(before, 27, old_state)
    op.validate_preservation_snapshot(after, 27, new_state)
    require(before['runtime_sha256'] == old_state['runtime_sha256'] and
            after['runtime_sha256'] == new_state['runtime_sha256'] == new_runtime_sha,
            'The snapshot runtime delta is not the exact approved replacement.')
    normalized = copy.deepcopy(after)
    normalized['runtime_sha256'] = before['runtime_sha256']
    # Both real snapshots are validated first. Only the separately proven env
    # hash is normalized for the existing same-schema row/sequence comparator.
    op.compare_preservation_snapshots(before, normalized, 27, 27, old_state)


def validate_state_delta(op, before, after):
    allowed = {'phase', 'stage', 'runtime_sha256', 'process', 'start_pending', STATE_KEY}
    require(op.equal_json({key: value for key, value in before.items() if key not in allowed},
                          {key: value for key, value in after.items() if key not in allowed}),
            'An unrelated fixture state field changed.')
    extension = after.get(STATE_KEY, {})
    require(extension.get('marker') == MARKER and extension.get('evidence_directory') == str(OUTPUT) and
            extension.get('added_root') == ADDED_ROOT, 'The state extension points outside the one approved scope.')


def file_identity(info):
    return {'device': info.st_dev, 'inode': info.st_ino, 'uid': info.st_uid, 'gid': info.st_gid,
            'mode': stat.S_IMODE(info.st_mode), 'links': info.st_nlink, 'bytes': info.st_size,
            'mtimeNs': info.st_mtime_ns, 'ctimeNs': info.st_ctime_ns}


def tree_snapshot(op, root, maximum_files):
    info = op.canonical(root)
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and stat.S_IMODE(info.st_mode) == 0o755,
            'A media root is not the protected generated directory.')
    anchor = {key: file_identity(info)[key] for key in ('device', 'inode', 'uid', 'gid', 'mode')}
    files, directories, total = {}, {}, 0
    for path in sorted(root.rglob('*')):
        info, relative = op.canonical(path), path.relative_to(root).as_posix()
        require(info.st_uid == info.st_gid == 0 and len(files) <= maximum_files and len(directories) <= 64,
                'A media tree changed ownership or exceeded its inventory bound.')
        if stat.S_ISDIR(info.st_mode):
            require(stat.S_IMODE(info.st_mode) == 0o755, 'A generated media directory changed mode.')
            directories[relative] = {key: file_identity(info)[key] for key in anchor}
        else:
            raw = protected_bytes(path, (0o644,), 16 << 20)
            total += len(raw)
            require(total <= 160 << 20, 'A generated media tree exceeds its byte bound.')
            files[relative] = {'sha256': digest(raw), 'bytes': len(raw), 'identity': file_identity(info)}
    require(len(files) == maximum_files, 'A generated media file population changed.')
    return {'root_identity': anchor, 'directories': directories, 'files': files}


def media_witness(op, state):
    documents = {}
    for path, (expected, mode) in MEDIA_RECEIPTS.items():
        raw = protected_bytes(path, (mode,))
        require(digest(raw) == expected, 'A generated-media or completed reference receipt changed.')
        documents[str(path)] = op.precise_json(raw)
    original = op.media_snapshot()
    require(op.equal_json(original, state['media']), 'The original fourteen-file media fixture changed.')
    auxiliary = tree_snapshot(op, AUX_MEDIA, 44)
    manifest = documents[str(AUX_MEDIA / 'manifest.json')]
    auxiliary_files = {name: {'sha256': row['sha256'], 'bytes': row['bytes'],
        **{key: row['identity'][key] for key in ('device', 'inode', 'links')}} for name, row in auxiliary['files'].items() if name != 'manifest.json'}
    require(op.equal_json(auxiliary_files, manifest.get('files')) and sorted(auxiliary['directories']) == manifest.get('directories'),
            'An auxiliary media member differs from the accepted manifest.')
    special = tree_snapshot(op, MEDIA, 13)
    completed = documents[str(WORK / 'special-features-media-v1/extras/completed.json')]
    manifest = documents[str(MEDIA / 'extras-manifest.json')]
    require(completed.get('status') == 'completed' and completed.get('protected_media_preserved') is True and
            completed.get('http_requests') == 0 and completed.get('library_scan_executed') is False and
            op.equal_json(special, completed.get('media_snapshot')) and
            op.equal_json(special['root_identity'], manifest.get('root_identity')) and
            op.equal_json(special['directories'], manifest.get('directories')) and
            op.equal_json({key: value for key, value in special['files'].items() if key != 'extras-manifest.json'}, manifest.get('files')),
            'The completed generated extras tree changed bytes, identities or membership.')
    indexed = documents[str(WORK / 'reference-special-features-v1/mains-indexed.json')]
    captured = documents[str(WORK / 'reference-special-features-v1/extras/export/report.json')]
    require(indexed.get('status') == 'indexed' and indexed.get('no_extras') is True and
            indexed.get('reference', {}).get('library_path') == ADDED_ROOT and captured.get('result') == 'complete' and
            captured.get('allMediaPreserved') is True and captured.get('newTokensRevoked') is True and
            captured.get('mainItems') == indexed.get('items'), 'The completed reference indexing/capture lineage is incomplete.')
    return {'original': original, 'auxiliary': auxiliary, 'special': special,
            'receipt_sha256s': {str(path): value[0] for path, value in MEDIA_RECEIPTS.items()}}


class ServiceFence:
    """Reserve only one exact stop and one later exact start."""

    def __init__(self):
        self.stage, self.reserved = 'preflight', set()

    def approve(self, arguments, unit):
        if arguments and Path(str(arguments[0])).name == 'systemctl':
            require(str(arguments[0]) == '/usr/bin/systemctl' and len(arguments) > 1, 'A noncanonical service command is forbidden.')
            if arguments[1] == 'show':
                return
            action = arguments[1]
            require(arguments == ['/usr/bin/systemctl', action, unit] and action in ('stop', 'start') and
                    self.stage == action + '_requested' and action not in self.reserved and
                    (action != 'start' or 'stop' in self.reserved), 'A service action is unowned, repeated or out of sequence.')
            self.reserved.add(action)


class Extension:
    def __init__(self, args):
        self.args, self.op, self.state, self.old_state = args, None, None, None
        self.state_bytes, self.runtime, self.new_runtime = None, None, None
        self.output_identity, self.before, self.stopped, self.after, self.media = None, None, None, None, None
        self.fence, self.events, self.lock = ServiceFence(), 0, None
        self.phase, self.completed = 'preflight', None
        self.owner_document = None
        self.new_runtime_identity = None

    def command(self, arguments, text=None, environment=None, timeout=30):
        arguments = [str(value) for value in arguments]
        self.fence.approve(arguments, self.op.UNIT)
        # The frozen helper's failure logger writes outside this operation.
        # Preserve its command behavior but keep failed preflight read-only.
        result = subprocess.run(arguments, input=text, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                check=False, timeout=timeout, env=environment or self.op.ENV)
        require(result.returncode == 0, 'A bounded helper command failed; the current operation remains retained.')
        return result.stdout.strip()

    def save_state(self, state):
        require(state is self.state and self.op.read(self.op.STATE_FILE) == self.state_bytes, 'The active state changed outside this operation.')
        validate_state_delta(self.op, self.old_state, state)
        self.original_save_state(state)
        self.state_bytes = self.op.read(self.op.STATE_FILE)
        require(self.op.equal_json(self.op.precise_json(self.state_bytes), state), 'The new state did not persist exactly.')

    def publish(self, phase):
        self.phase = self.fence.stage = phase
        self.events += 1
        require(self.events <= 32, 'The operation exceeded its journal bound.')
        if self.state is not None and STATE_KEY in self.state:
            self.state[STATE_KEY]['phase'] = phase
            self.state['stage'] = 'media_root_extension_' + phase
            self.save_state(self.state)
        self.op.create(OUTPUT / f'event-{self.events:04d}.json', {'marker': MARKER, 'phase': phase,
            'at': self.op.utc(), 'state_sha256': digest(self.state_bytes), 'service_actions_reserved': sorted(self.fence.reserved)})

    def check(self, live=True, product=False):
        op, args = self.op, self.args
        require(digest(protected_bytes(Path(__file__).absolute())) == args.script_sha256 and
                digest(protected_bytes(args.operator_path, (0o600, 0o644, 0o700, 0o755))) == args.operator_sha256 and
                op.read(op.STATE_FILE) == self.state_bytes and op.sha(op.RUNTIME) == self.state['runtime_sha256'] and
                digest(protected_bytes(args.upgrade_dir / 'completed.json')) == args.upgrade_completed_sha256 and
                digest(protected_bytes(args.upgrade_report)) == args.upgrade_report_sha256 and
                digest(protected_bytes(args.full_report)) == args.full_report_sha256,
                'A pinned input, active state or runtime changed.')
        op.verify_fixture_directories(self.state)
        op.verify_database(self.state)
        if self.new_runtime_identity is not None:
            require(file_identity(op.regular(op.RUNTIME)) == self.new_runtime_identity and
                    protected_bytes(op.RUNTIME, (0o600,)) == self.new_runtime, 'The exact replaced runtime file changed.')
        if live:
            require(op.verify_service(self.state) == self.state['process'], 'The currently owned candidate process changed.')
        else:
            op.require_candidate_stopped()
        if self.output_identity is not None:
            require(op.directory(OUTPUT, 0, 0o700, 0) == self.output_identity, 'The exclusive evidence directory changed identity.')
            require(op.equal_json(op.load(OUTPUT / 'OWNER.json'), self.owner_document), 'The exclusive owner record changed.')
        if product:
            proof = op.verify_product_upgrade(Path(self.upgrade['source']), args.candidate_sha256, args.source,
                args.source_manifest_sha256, args.full_report, args.full_report_sha256, 27)
            require(op.equal_json(proof, self.upgrade['schema_artifacts']['product_verification']), 'The complete product proof changed.')

    def prepare(self):
        args = self.args
        require(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0 and os.environ.get('SSH_CONNECTION'),
                'Run only through authorized root SSH on test-env.')
        require(args.operator_path == WORK / 'prepare-client-fixture.py', 'The completed upgrade operator must be selected explicitly at its fixed path.')
        raw = protected_bytes(args.operator_path, (0o600, 0o644, 0o700, 0o755))
        require(digest(raw) == args.operator_sha256 and digest(protected_bytes(Path(__file__).absolute())) == args.script_sha256,
                'An explicitly pinned operator source changed.')
        op = types.ModuleType('frozen_candidate_fixture')
        op.__file__ = str(args.operator_path)
        exec(compile(raw, str(args.operator_path), 'exec'), op.__dict__)
        self.op = op
        require(op.WORK == WORK and op.UNIT == 'goby-client-m3e.service' and op.RUNTIME == WORK / 'runtime.env' and
                op.STATE_FILE == WORK / 'client-fixture.json' and op.PORT == 18198 and op.SCHEMA_TABLE_COUNTS.get(27) == 35,
                'The selected helper identifies another candidate or schema.')
        op.command = self.command
        op.host_inputs(initial=False)
        self.lock = os.open(op.LOCK, os.O_RDONLY | os.O_NOFOLLOW)
        require(op.identity(os.fstat(self.lock)) == op.identity(op.regular(op.LOCK)), 'The existing candidate lock changed.')
        fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        require(not op.exists(OUTPUT), 'This extension scope already exists; no retry, recovery or adoption is permitted.')
        self.state_bytes = op.read(op.STATE_FILE)
        require(digest(self.state_bytes) == args.state_sha256, 'The explicitly selected starting state changed.')
        self.state = op.precise_json(self.state_bytes)
        self.old_state = copy.deepcopy(self.state)
        require(args.upgrade_dir.parent == WORK and re.fullmatch('client-upgrade-[0-9a-f]{32}', args.upgrade_dir.name) and
                args.upgrade_report.parent == WORK and re.fullmatch('client-fixture-report-[0-9a-f]{24}[.]json', args.upgrade_report.name),
                'Upgrade evidence paths are outside their fixed grammar.')
        completed_raw, report_raw = protected_bytes(args.upgrade_dir / 'completed.json'), protected_bytes(args.upgrade_report)
        require(digest(completed_raw) == args.upgrade_completed_sha256 and digest(report_raw) == args.upgrade_report_sha256,
                'The explicitly selected upgrade evidence changed.')
        self.upgrade = op.precise_json(completed_raw)
        validate_receipt_chain(op, args, self.state, self.upgrade, op.precise_json(report_raw))
        require(self.state.get('work_identity') == op.directory(WORK, 0, 0o700, 0) and
                self.state.get('cluster') == op.cluster_identity(), 'The workspace or dedicated PostgreSQL cluster changed.')
        self.runtime = protected_bytes(op.RUNTIME, (0o600,), 65536)
        require(digest(self.runtime) == OLD_RUNTIME_SHA, 'The original runtime bytes changed before this extension.')
        self.runtime_identity = file_identity(op.regular(op.RUNTIME))
        self.new_runtime = extend_runtime(self.runtime)
        self.new_runtime_sha = digest(self.new_runtime)
        self.check(product=True)
        self.media = media_witness(op, self.state)
        self.before = op.preservation_snapshot(self.state, 27)
        op.validate_preservation_snapshot(self.before, 27, self.state)
        require_quiescent(self.before, self.state)
        self.original_save_state = op.save_state
        op.save_state = self.save_state

    def execute(self):
        op = self.op
        OUTPUT.mkdir(mode=0o700)
        self.output_identity = op.directory(OUTPUT, 0, 0o700, 0)
        self.owner_document = {'marker': MARKER, 'identity': self.output_identity, 'state_sha256': self.args.state_sha256,
            'candidate_sha256': self.args.candidate_sha256, 'old_process': self.old_state['process'], 'added_root': ADDED_ROOT}
        op.create(OUTPUT / 'OWNER.json', self.owner_document)
        op.create(OUTPUT / 'before-state.json', self.state_bytes)
        op.create(OUTPUT / 'before-runtime.env', self.runtime)
        op.create(OUTPUT / 'before-live-full.json', self.before)
        op.create(OUTPUT / 'media-before.json', self.media)
        self.state.update(phase='extending_media_root', stage='media_root_extension_prepared')
        self.state[STATE_KEY] = {'marker': MARKER, 'phase': 'prepared', 'added_root': ADDED_ROOT, 'evidence_directory': str(OUTPUT),
            'source_state_sha256': self.args.state_sha256, 'old_runtime_sha256': OLD_RUNTIME_SHA, 'new_runtime_sha256': self.new_runtime_sha,
            'old_process': self.old_state['process'], 'script_sha256': self.args.script_sha256, 'operator_sha256': self.args.operator_sha256}
        self.publish('prepared')
        self.check(product=True)
        fresh = op.preservation_snapshot(self.state, 27)
        require_quiescent(fresh, self.state)
        op.compare_preservation_snapshots(self.before, fresh, 27, 27, self.old_state)
        op.create(OUTPUT / 'before-stop-full.json', fresh)
        require(op.equal_json(media_witness(op, self.state), self.media), 'A media receipt or byte changed before stop.')
        self.publish('stop_requested')
        require(op.verify_service(self.state) == self.old_state['process'], 'The exact candidate changed immediately before stop.')
        op.command(['/usr/bin/systemctl', 'stop', op.UNIT], timeout=45)
        op.require_candidate_stopped()
        self.publish('stopped')
        self.stopped = op.preservation_snapshot(self.state, 27)
        require_quiescent(self.stopped, self.state)
        op.compare_preservation_snapshots(self.before, self.stopped, 27, 27, self.old_state)
        op.create(OUTPUT / 'stopped-full.json', self.stopped)
        self.check(live=False)
        require(file_identity(op.regular(op.RUNTIME)) == self.runtime_identity and op.read(op.RUNTIME) == self.runtime,
                'The original runtime changed before atomic replacement.')
        staged = OUTPUT / 'runtime.next'
        op.create(staged, self.new_runtime)
        require(protected_bytes(staged, (0o600,)) == self.new_runtime, 'The staged runtime differs from the one-field transformation.')
        self.publish('runtime_replace_requested')
        op.require_candidate_stopped()
        os.replace(staged, op.RUNTIME)
        op.sync(WORK)
        require(protected_bytes(op.RUNTIME, (0o600,)) == self.new_runtime, 'The atomic runtime replacement did not preserve its exact bytes.')
        self.new_runtime_identity = file_identity(op.regular(op.RUNTIME))
        self.state['runtime_sha256'] = self.new_runtime_sha
        self.publish('runtime_replaced')
        before_start = op.preservation_snapshot(self.state, 27)
        compare_after_extension(op, self.stopped, before_start, self.old_state, self.state, self.new_runtime_sha)
        op.create(OUTPUT / 'before-start-full.json', before_start)
        self.check(live=False, product=True)
        self.publish('start_requested')
        op.require_candidate_stopped()
        op.start_service(self.state)
        current = self.state['process']
        require(current != self.old_state['process'] and current['boot_id'] == self.old_state['process']['boot_id'] and
                current['start_ticks'] > self.old_state['process']['start_ticks'], 'Startup did not establish a new process in the same boot.')
        self.publish('started')
        self.check()
        with os.fdopen(os.open(Path('/proc') / str(current['pid']) / 'environ', os.O_RDONLY | os.O_NOFOLLOW), 'rb') as stream:
            environment = stream.read(65537)
        require(len(environment) <= 65536 and [value for value in environment.split(b'\0') if value.startswith(b'GOBY_MEDIA_ROOTS=')] ==
                [b'GOBY_MEDIA_ROOTS=' + ':'.join((*OLD_ROOTS, ADDED_ROOT)).encode()], 'The new process did not load the exact expanded root value.')
        del environment
        self.after = op.preservation_snapshot(self.state, 27)
        compare_after_extension(op, self.stopped, self.after, self.old_state, self.state, self.new_runtime_sha)
        require_quiescent(self.after, self.state)
        op.create(OUTPUT / 'after-full.json', self.after)
        after_media = media_witness(op, self.state)
        require(op.equal_json(after_media, self.media), 'An original, auxiliary or new media member changed during extension.')
        op.create(OUTPUT / 'media-after.json', after_media)
        self.check()
        require(self.fence.reserved == {'stop', 'start'}, 'The operation did not perform exactly its two acknowledged service actions.')
        self.completed = {'marker': MARKER, 'phase': 'complete', 'added_root': ADDED_ROOT, 'completed_at': op.utc(),
            'script_sha256': self.args.script_sha256, 'operator_sha256': self.args.operator_sha256,
            'source_state_sha256': self.args.state_sha256, 'upgrade_completed_sha256': self.args.upgrade_completed_sha256,
            'upgrade_report_sha256': self.args.upgrade_report_sha256, 'full_report_sha256': self.args.full_report_sha256,
            'source_manifest_sha256': self.args.source_manifest_sha256, 'binary_sha256': self.args.candidate_sha256, 'schema': 27,
            'old_process': self.old_state['process'], 'new_process': current, 'old_runtime_sha256': OLD_RUNTIME_SHA,
            'new_runtime_sha256': self.new_runtime_sha, 'old_database_rows_and_sequences_preserved': True,
            'old_runtime_identity': self.runtime_identity, 'new_runtime_identity': self.new_runtime_identity,
            'other_runtime_bytes_preserved': True, 'credentials_recovery_and_all_media_preserved': True,
            'http_requests': 0, 'library_creates': 0, 'scan_dispatches': 0, 'service_actions': sorted(self.fence.reserved),
            'before_snapshot_sha256': op.sha(OUTPUT / 'stopped-full.json', limit=op.MAX_SNAPSHOT_BYTES),
            'after_snapshot_sha256': op.sha(OUTPUT / 'after-full.json', limit=op.MAX_SNAPSHOT_BYTES)}
        op.create(OUTPUT / 'completed.json', self.completed)
        self.state[STATE_KEY].update(phase='complete', receipt_path=str(OUTPUT / 'completed.json'),
            receipt_sha256=op.sha(OUTPUT / 'completed.json'), new_process=current)
        self.state.update(phase='ready', stage='complete')
        self.save_state(self.state)
        # This is the unchanged base inspect path's state and snapshot contract.
        self.check()
        op.validate_preservation_snapshot(self.after, op.target_schema_version(self.state), self.state)
        self.phase = self.fence.stage = 'complete'

    def run_locked(self):
        failure = None
        try:
            self.prepare()
            if self.args.check_only:
                print(json.dumps({'result': 'preflight_passed', 'schema': 27, 'new_evidence_writes': 0, 'http_requests': 0,
                                  'added_root': ADDED_ROOT, 'new_runtime_sha256': self.new_runtime_sha}), flush=True)
                return 0
            self.execute()
        except Exception as error:
            failure = type(error).__name__
        result = 'passed' if failure is None else 'retained_for_review'
        report = {'marker': MARKER, 'result': result, 'phase': self.phase, 'failure_type': failure,
                  'completed': self.completed, 'http_requests': 0, 'rollback_performed': False, 'retry_permitted': False,
                  'second_phase': 'Run the original-Movie schema27 client before adding a library. Positive setup needs a receipt-backed nonempty-fixture validator; never remove the existing empty-extra check. No library, scan, protocol/media or client run occurs in this phase.'}
        if self.output_identity is not None:
            try:
                self.op.create(OUTPUT / 'report.json', report)
            except Exception:
                result = 'retained_for_review'
        print(json.dumps({'result': result, 'phase': self.phase, 'failure_type': failure,
                          'evidence': str(OUTPUT) if self.output_identity else None}), flush=True)
        return 0 if result == 'passed' else 1

    def run(self):
        try:
            return self.run_locked()
        finally:
            if self.lock is not None:
                os.close(self.lock)
                self.lock = None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('script', 'operator', 'state', 'upgrade-completed', 'upgrade-report', 'candidate', 'source-manifest', 'full-report'):
        parser.add_argument('--' + name + '-sha256', required=True)
    for name in ('operator-path', 'upgrade-dir', 'upgrade-report', 'source', 'full-report'):
        parser.add_argument('--' + name, required=True, type=Path)
    parser.add_argument('--candidate-pid', required=True, type=int)
    parser.add_argument('--candidate-start-ticks', required=True, type=int)
    parser.add_argument('--candidate-boot-id', required=True)
    parser.add_argument('--check-only', action='store_true')
    args = parser.parse_args()
    for key, value in vars(args).items():
        if key.endswith('_sha256'):
            require(HASH.fullmatch(value), 'An explicit digest is malformed.')
    require(args.candidate_pid > 1 and args.candidate_start_ticks > 0 and
            re.fullmatch('[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}', args.candidate_boot_id), 'An explicit process identity is malformed.')
    os.umask(0o077)
    return Extension(args).run()


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except Exception as error:
        print(json.dumps({'result': 'retained_for_review', 'failure_type': type(error).__name__, 'retry_permitted': False}), file=sys.stderr)
        raise SystemExit(1)
