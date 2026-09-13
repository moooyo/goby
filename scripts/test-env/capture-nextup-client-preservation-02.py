#!/usr/bin/env python3
"""Capture unchanged Goby state and sealed roots around one client discovery."""

import copy
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import types

W = Path('/opt/goby-test/exec-work-m3e')
E = W / 'reference-nextup-client-discovery-execution-02'
INPUT = W / 'nextup-client-discovery-inputs-02/input.json'
OUTPUT = W / 'reference-nextup-client-discovery-02'
UNIT = 'goby-nextup-client-discovery-02.service'
UNIT_FILE = Path('/run/systemd/system') / UNIT
PREVIOUS = W / 'reference-nextup-client-discovery-execution-01/preservation-after.json'
PREVIOUS_SHA = '4c7c359ba8a8c5f67451c37bc5c357bc27a17486f8e524eb21daa3d95ed8478e'
PREVIOUS_ATTESTATION = W / 'reference-nextup-global-matrix-attestation-07/attestation.json'
PREVIOUS_ATTESTATION_SHA = 'c2dfa248e6823b0e9bc4aa04f9034d9ca0c872de80895f4ce5a50db32c66021a'
MAX_BYTES = 256 * 1024 * 1024


def require(condition, message):
    if not condition:
        raise ValueError(message)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def identity(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid,
            info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def metadata(path):
    info = path.lstat()
    return {'device': info.st_dev, 'inode': info.st_ino, 'uid': info.st_uid, 'gid': info.st_gid,
            'mode': info.st_mode, 'links': info.st_nlink, 'bytes': info.st_size,
            'mtime_ns': info.st_mtime_ns, 'ctime_ns': info.st_ctime_ns}


def read(path, pin=None, expected=None):
    path = Path(path)
    require(path.is_absolute() and path.resolve(strict=True) == path, 'An authority path traverses a symlink.')
    before = path.lstat()
    require(stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and
            not before.st_mode & 0o022 and before.st_size <= MAX_BYTES, 'An authority has unsafe metadata.')
    if expected is None:
        require(before.st_nlink == 1, 'Unattested hard links cannot be read.')
    else:
        require(metadata(path) == {k: expected[k] for k in metadata(path)}, 'Sealed metadata changed before reading.')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as stream:
        require(identity(os.fstat(stream.fileno())) == identity(before), 'An authority changed before reading.')
        raw = stream.read(MAX_BYTES + 1)
        require(identity(os.fstat(stream.fileno())) == identity(before), 'An authority changed while reading.')
    require(identity(path.lstat()) == identity(before) and len(raw) == before.st_size and
            (pin is None or digest(raw) == pin), 'An authority changed or its digest differs.')
    return raw


def info(path, expected=None):
    before = path.lstat()
    value = metadata(path)
    if expected is not None:
        require(value == {k: expected[k] for k in value}, 'Sealed metadata changed before byte access.')
    if stat.S_ISLNK(before.st_mode):
        value['symlink'] = os.readlink(path)
    elif stat.S_ISREG(before.st_mode):
        value['sha256'] = digest(read(path, expected=expected))
    else:
        require(stat.S_ISDIR(before.st_mode), 'A protected tree contains an unsupported node.')
    require(identity(path.lstat()) == identity(before), 'A protected entry changed during capture.')
    return value


def publish(name, value):
    path = E / name
    raw = (json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + '\n').encode()
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb') as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    parent = os.open(E, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(parent)
    finally:
        os.close(parent)
    return {'path': str(path), 'sha256': digest(raw)}


def main():
    require(sys.platform == 'linux' and os.geteuid() == 0 and os.environ.get('SSH_CONNECTION') and
            sys.flags.isolated and sys.flags.dont_write_bytecode and not sys.flags.optimize,
            'Use authorized remote Python -I -B with assertions enabled.')
    require(len(sys.argv) == 4 and sys.argv[1] in ('before', 'after') and
            all(re.fullmatch(r'[0-9a-f]{64}', v) for v in sys.argv[2:]),
            'Usage: capture.py before|after INPUT_SHA256 UNIT_SHA256')
    phase, input_sha, unit_sha = sys.argv[1:]
    os.umask(0o077)
    require(E.is_dir() and not E.is_symlink() and E.stat().st_uid == E.stat().st_gid == 0 and
            not E.stat().st_mode & 0o077, 'The capture output must be an owned private directory.')
    previous = json.loads(read(PREVIOUS, PREVIOUS_SHA))
    attestation = json.loads(read(PREVIOUS_ATTESTATION, PREVIOUS_ATTESTATION_SHA))
    require(len(previous['roots']) == previous['rootCount'] == 192, 'The accepted baseline root count differs.')
    client_input = {'path': str(INPUT), 'sha256': digest(read(INPUT, input_sha))}
    unit_file = info(UNIT_FILE)
    require(unit_file['sha256'] == unit_sha, 'The client unit file differs from its publication.')
    roots = sorted(previous['roots']) + [str(W / name) for name in (
        'reference-nextup-client-discovery-execution-01', 'reference-nextup-client-discovery-01',
        'nextup-client-discovery-verification-01', 'nextup-client-discovery-verification-02',
        'nextup-client-discovery-tool-02', 'nextup-client-discovery-inputs-02')]
    require(len(roots) == len(set(roots)) == 198, 'The client preservation root inventory differs.')
    trees = {}
    for name in roots:
        root = Path(name)
        require(root.parent == W or root == Path('/opt/goby-fixtures/client-m3e'), 'A protected root escapes the owned fixture.')
        require(root.is_dir() and not root.is_symlink() and
                all(target != root and root not in target.parents and target not in root.parents for target in (E, OUTPUT)) and
                all(root != Path(excluded) and Path(excluded) not in root.parents and root not in Path(excluded).parents
                    for excluded in attestation['forbiddenOriginalRoots']), 'A protected root overlaps a forbidden scope.')
        paths = [root, *sorted(root.rglob('*'))]
        require(len(paths) <= 2500, 'A protected root exceeds the bounded inventory.')
        old = previous['roots'].get(name)
        if old is not None:
            require({str(path.relative_to(root)) for path in paths} == set(old), 'A sealed root changed its complete path set.')
            for path in paths:
                expected = old[str(path.relative_to(root))]
                require(metadata(path) == {k: expected[k] for k in metadata(path)}, 'Sealed metadata changed before tree byte access.')
        trees[name] = {str(path.relative_to(root)): info(path, None if old is None else old[str(path.relative_to(root))]) for path in paths}
        require(old is None or trees[name] == old, 'A historical protected root changed.')
    reader = W / 'prepare-client-fixture.py'
    source = read(reader, '84d21e8ac0b48c5dfd3d2c7ae35b0aec0d7f4f811e65658081490aa44947b49c')
    module = types.ModuleType('nextup_client02_owned_goby_snapshot')
    module.__file__ = str(reader)
    exec(compile(source, str(reader), 'exec'), module.__dict__)
    fixture = module.precise_json(read(W / 'client-fixture.json', 'bb78a846d2d4b69b7e2550ed9e367549cbe2d0b763e2b69270ea98bed6570d82'))
    current = module.preservation_snapshot(fixture, 28)
    baseline = module.precise_json(read(previous['gobySnapshot']['path'], previous['gobySnapshot']['sha256']))
    stable = []
    for value in (current, baseline):
        value = copy.deepcopy(value)
        value['database']['metadata'].pop('captured_at')
        stable.append(module.canonical_json(value))
    require(stable[0] == stable[1], 'The complete Goby state changed beyond capture time.')
    counts = {key: len(current['database']['tables'][key]) for key in ('sessions', 'devices', 'activity_entries')}
    require(counts == {'sessions': 83, 'devices': 70, 'activity_entries': 185}, 'Goby preservation counts differ.')
    services = {}
    fields = 'Id,LoadState,ActiveState,SubState,MainPID,Result,ExecMainStatus,InvocationID,ControlGroup'
    for name, expected in previous['services'].items():
        observed = subprocess.run(['/usr/bin/systemctl', 'show', name, '--property=' + fields], capture_output=True, text=True, check=True, timeout=15)
        services[name] = dict(line.split('=', 1) for line in observed.stdout.splitlines() if '=' in line)
        require(services[name] == expected, 'A protected service changed: ' + name)
    main_files = {name: info(Path(name), expected) for name, expected in previous['mainFiles'].items()}
    require(main_files == previous['mainFiles'] and info(UNIT_FILE) == unit_file, 'A protected file changed.')
    snapshot = publish('goby-' + phase + '.json', current)
    record = {'schemaVersion': 1, 'kind': 'nextup-client-preservation', 'phase': phase,
              'capturedAt': datetime.now(timezone.utc).isoformat(), 'previous': {'path': str(PREVIOUS), 'sha256': PREVIOUS_SHA},
              'clientInput': client_input, 'clientUnitFile': unit_file, 'roots': trees, 'rootCount': len(trees),
              'services': services, 'mainFiles': main_files, 'gobySnapshot': snapshot, 'gobyCounts': counts,
              'historical192RootsEqual': True, 'completeGobyStateEqualExceptCaptureTime': True,
              'referenceDatabaseRead': False, 'originalImplementationBytesRead': False, 'businessHttpRequests': 0}
    if phase == 'after':
        before = json.loads(read(E / 'preservation-before.json'))
        require(all(before[key] == record[key] for key in ('previous', 'clientInput', 'clientUnitFile', 'roots', 'rootCount',
                    'services', 'mainFiles', 'gobyCounts')), 'Complete client before/after preservation differs.')
    receipt = publish('preservation-' + phase + '.json', record)
    print(json.dumps({'phase': phase, 'rootCount': len(trees), 'gobyCounts': counts, 'receipt': receipt}))


if __name__ == '__main__':
    main()
