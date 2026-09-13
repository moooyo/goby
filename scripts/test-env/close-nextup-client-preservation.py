#!/usr/bin/env python3
"""Independently combine client closure with fresh protected-state observation."""

import argparse
from copy import deepcopy
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import types

W = Path('/opt/goby-test/exec-work-m3e')
FORBIDDEN = (W / 'reference-data', Path('/dev/shm/goby-emby-reference/package'))
CAPTURE_PINS = {
    2: ('capture-nextup-client-preservation-02.py', 'cb3dd2cc7a4fa817e67c697d04106e0125044208f5f43a35bbff5664c1a513ed'),
}
READER_SHA = '84d21e8ac0b48c5dfd3d2c7ae35b0aec0d7f4f811e65658081490aa44947b49c'
FIXTURE_SHA = 'bb78a846d2d4b69b7e2550ed9e367549cbe2d0b763e2b69270ea98bed6570d82'


def need(value, message):
    if not value:
        raise ValueError(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), allow_nan=False)


def read(path, pin=None):
    path = Path(path)
    need(path.resolve(strict=True) == path and all(path != root and root not in path.parents for root in FORBIDDEN), 'Unsafe evidence path.')
    before = path.lstat()
    need(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_uid == before.st_gid == 0 and
         not before.st_mode & 0o022 and before.st_size <= 256 * 1024 * 1024, 'Unsafe evidence metadata.')
    def identity(info):
        return (info.st_dev, info.st_ino, info.st_mode, info.st_nlink, info.st_uid, info.st_gid,
                info.st_size, info.st_mtime_ns, info.st_ctime_ns)
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as stream:
        need(identity(os.fstat(stream.fileno())) == identity(before), 'Evidence changed during open.')
        raw = stream.read(256 * 1024 * 1024 + 1)
        need(identity(os.fstat(stream.fileno())) == identity(before), 'Evidence changed during read.')
    need(identity(path.lstat()) == identity(before) and len(raw) == before.st_size and (pin is None or sha(raw) == pin),
         'Evidence bytes or identity changed.')
    return raw


def module(path, pin, name):
    raw = read(path, pin)
    result = types.ModuleType(name)
    result.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), result.__dict__)
    return result


def main():
    need(sys.platform == 'linux' and os.geteuid() == 0 and os.environ.get('SSH_CONNECTION') and
         sys.flags.isolated and sys.flags.dont_write_bytecode, 'Use authorized remote Python -I -B.')
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--run-number', type=int, choices=(2,), required=True)
    parser.add_argument('--client-terminal', type=Path, required=True)
    parser.add_argument('--client-terminal-sha256', required=True)
    parser.add_argument('--before-sha256', required=True)
    parser.add_argument('--after-sha256', required=True)
    args = parser.parse_args()
    os.umask(0o077)
    number = str(args.run_number).zfill(2)
    execution = W / ('reference-nextup-client-discovery-execution-' + number)
    output = execution / 'independent-preservation-closeout.json'
    need(not os.path.lexists(output) and args.client_terminal.parent == execution and
         args.client_terminal.name.startswith('independent-client-terminal') and args.client_terminal.suffix == '.json',
         'Use a fresh closeout and the exact independent client terminal.')
    client = json.loads(read(args.client_terminal, args.client_terminal_sha256))
    need(client['clientDiscoveryEvidenceVerified'] is True and client['failure'] is None and
         client['uiLoginAndLogoutDualChannelBound'] is True and client['exactUIToken401MetadataVerified'] is True and
         client['recorderTokensIndependentlyRejected'] is True and client['fullEpisodeSummaryUserDataPreserved'] is True and
         client['fullPreferencesPreserved'] is True and client['profileAcceptedFieldsPreserved'] is True and
         client['unit']['formerPidAbsent'] is True and client['unit']['cgroupAbsent'] is True,
         'The client does not have complete independent cleanup and state proof.')
    before_path, after_path = execution / 'preservation-before.json', execution / 'preservation-after.json'
    before = json.loads(read(before_path, args.before_sha256))
    after = json.loads(read(after_path, args.after_sha256))
    need(before['kind'] == after['kind'] == 'nextup-client-preservation' and before['phase'] == 'before' and after['phase'] == 'after',
         'The preservation phases differ.')
    count = 198
    need(before['rootCount'] == after['rootCount'] == len(before['roots']) == len(after['roots']) == count and
         before['clientInput'] == after['clientInput'] == client['input'] and
         before['clientUnitFile']['sha256'] == client['unit']['unitFile']['sha256'], 'Preservation names another client or root set.')
    for key in ('previous', 'clientInput', 'clientUnitFile', 'roots', 'rootCount', 'services', 'mainFiles', 'gobyCounts'):
        need(canonical(before[key]) == canonical(after[key]), 'A complete preservation field differs: ' + key)
    capture_name, capture_sha = CAPTURE_PINS[args.run_number]
    capture = module(execution / capture_name, capture_sha, 'independent_client_capture_reader_' + number)
    for name, expected in after['roots'].items():
        root = Path(name)
        need(root.parent == W or root == Path('/opt/goby-fixtures/client-m3e'), 'A protected root escapes the fixture.')
        need(root.is_dir() and root.resolve(strict=True) == root and all(root != excluded and excluded not in root.parents and
             root not in excluded.parents for excluded in FORBIDDEN), 'A protected root overlaps original data.')
        paths = [root, *sorted(root.rglob('*'))]
        need(len(paths) <= 2500 and {str(path.relative_to(root)) for path in paths} == set(expected), 'A complete protected path set changed.')
        for path in paths:
            metadata = capture.metadata(path)
            need(metadata == {key: expected[str(path.relative_to(root))][key] for key in metadata}, 'Protected metadata changed before byte access.')
        current = {str(path.relative_to(root)): capture.info(path, expected[str(path.relative_to(root))]) for path in paths}
        need(current == expected, 'A fresh protected tree differs from the captured bytes.')
    for name, expected in after['mainFiles'].items():
        need(capture.info(Path(name), expected) == expected, 'A protected main file changed.')
    unit_file = Path('/run/systemd/system') / client['unit']['name']
    need(capture.info(unit_file, after['clientUnitFile']) == after['clientUnitFile'], 'The client unit file changed.')
    for name, expected in after['services'].items():
        observed = subprocess.check_output(['/usr/bin/systemctl', 'show', name, '--property=' + ','.join(sorted(expected))], text=True, timeout=15)
        values = dict(row.split('=', 1) for row in observed.splitlines() if '=' in row)
        need(values == expected, 'A protected service changed.')
    def stable(snapshot):
        result = deepcopy(snapshot)
        result['database']['metadata'].pop('captured_at')
        return canonical(result)
    snapshots = []
    for value in (before, after):
        row = value['gobySnapshot']
        need(Path(row['path']).parent == execution, 'The Goby capture escaped its execution scope.')
        snapshots.append(json.loads(read(row['path'], row['sha256'])))
    need(stable(snapshots[0]) == stable(snapshots[1]), 'The complete Goby state changed between captures.')
    reader = module(W / 'prepare-client-fixture.py', READER_SHA, 'independent_client_goby_reader_' + number)
    fixture = reader.precise_json(read(W / 'client-fixture.json', FIXTURE_SHA))
    fresh = reader.preservation_snapshot(fixture, 28)
    need(stable(fresh) == stable(snapshots[1]), 'Fresh complete Goby state differs from client closure.')
    counts = {key: len(fresh['database']['tables'][key]) for key in ('sessions', 'devices', 'activity_entries')}
    need(counts == after['gobyCounts'] == {'sessions': 83, 'devices': 70, 'activity_entries': 185}, 'Goby counts differ.')
    read(args.client_terminal, args.client_terminal_sha256)
    read(before_path, args.before_sha256)
    read(after_path, args.after_sha256)
    result = {'kind': 'nextup-client-independent-preservation-closeout', 'version': 1,
              'capturedAt': datetime.now(timezone.utc).isoformat(), 'runNumber': args.run_number, 'status': 'verified',
              'clientTerminal': {'path': str(args.client_terminal), 'sha256': args.client_terminal_sha256},
              'preservationBefore': {'path': str(before_path), 'sha256': args.before_sha256},
              'preservationAfter': {'path': str(after_path), 'sha256': args.after_sha256},
              'rootCount': count, 'completeBeforeAfterEqualityVerified': True, 'freshProtectedTreesExact': True,
              'freshGobyCompleteStateEqualExceptCaptureTime': True, 'gobyCounts': counts,
              'referenceDatabaseRead': False, 'boundReferenceExecutableBytesRead': False, 'businessHttpRequests': 0,
              'clientDiscoveryOutcome': client['discoveryOutcome'], 'positiveClientAcceptanceClaim': False,
              'playbackAcceptance': False, 'refreshAcceptance': False, 'resumeOrRetryAllowed': False}
    raw = (json.dumps(result, sort_keys=True, indent=2, allow_nan=False) + '\n').encode()
    with os.fdopen(os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb') as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    parent = os.open(execution, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(parent)
    finally:
        os.close(parent)
    print(json.dumps({'path': str(output), 'sha256': sha(raw), 'status': 'verified', 'rootCount': count}))


if __name__ == '__main__':
    main()
