#!/usr/bin/env python3
"""Start the prepared client-discovery unit exactly once after preservation."""

from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time

W = Path('/opt/goby-test/exec-work-m3e')
E = W / 'reference-nextup-client-discovery-execution-02'
INPUT = W / 'nextup-client-discovery-inputs-02/input.json'
ROOT = W / 'reference-nextup-client-discovery-02'
UNIT = 'goby-nextup-client-discovery-02.service'
UNIT_FILE = Path('/run/systemd/system') / UNIT


def require(condition, message):
    if not condition:
        raise ValueError(message)


def identity(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_nlink, info.st_uid,
            info.st_gid, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def read(path, expected):
    path = Path(path)
    require(path.is_absolute() and path.resolve(strict=True) == path, 'An authority path traverses a symlink.')
    before = path.lstat()
    require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_uid == before.st_gid == 0 and
            not before.st_mode & 0o022 and before.st_size <= 256 * 1024 * 1024, 'Unsafe authority metadata.')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as stream:
        require(identity(os.fstat(stream.fileno())) == identity(before), 'An authority changed during open.')
        raw = stream.read(256 * 1024 * 1024 + 1)
        require(identity(os.fstat(stream.fileno())) == identity(before), 'An authority changed during read.')
    require(identity(path.lstat()) == identity(before) and hashlib.sha256(raw).hexdigest() == expected,
            'An authority changed or has another digest.')
    return raw


def save(name, value):
    raw = (json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + '\n').encode()
    path = E / name
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb') as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    directory = os.open(E, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)
    return {'path': str(path), 'sha256': hashlib.sha256(raw).hexdigest()}


def show(keys):
    result = subprocess.run(['/usr/bin/systemctl', 'show', UNIT, '--property=' + ','.join(sorted(keys))],
                            capture_output=True, text=True, check=True, timeout=15)
    require(len(result.stdout) <= 65536 and result.stderr == '', 'Unit metadata observation failed.')
    properties = dict(row.split('=', 1) for row in result.stdout.splitlines() if '=' in row)
    require(set(properties) == set(keys), 'Required unit metadata is missing.')
    return properties


def main():
    require(sys.platform == 'linux' and os.geteuid() == 0 and os.environ.get('SSH_CONNECTION') and
            sys.flags.isolated and sys.flags.dont_write_bytecode, 'Use authorized remote Python -I -B.')
    require(len(sys.argv) == 3 and all(re.fullmatch(r'[0-9a-f]{64}', value) for value in sys.argv[1:]),
            'Usage: launch.py PREPARATION_SHA256 PRESERVATION_SHA256')
    preparation_sha, preservation_sha = sys.argv[1:]
    os.umask(0o077)
    prepared = json.loads(read(E / 'preparation.json', preparation_sha))
    before = json.loads(read(E / 'preservation-before.json', preservation_sha))
    require(prepared['kind'] == 'nextup-client-discovery-preparation' and prepared['unitName'] == UNIT and
            prepared['notStarted'] is True and prepared['startCalls'] == 0 and prepared['outputRootEmpty'] is True,
            'The prepared unit has already started or names another scope.')
    require(prepared['input']['path'] == str(INPUT) and prepared['unitFile']['path'] == str(UNIT_FILE),
            'The preparation names another input or unit.')
    read(INPUT, prepared['input']['sha256'])
    read(UNIT_FILE, prepared['unitFile']['sha256'])
    require(before['kind'] == 'nextup-client-preservation' and before['phase'] == 'before' and
            before['clientInput'] == prepared['input'] and before['clientUnitFile']['sha256'] == prepared['unitFile']['sha256'] and
            before['rootCount'] == len(before['roots']) == 198 and before['historical192RootsEqual'] is True and
            before['completeGobyStateEqualExceptCaptureTime'] is True and before['gobyCounts'] ==
            {'sessions': 83, 'devices': 70, 'activity_entries': 185}, 'Complete fresh preservation is missing.')
    for row in prepared['sourceClosure'].values():
        read(row['path'], row['sha256'])
    read(prepared['node']['path'], prepared['node']['sha256'])
    info = ROOT.lstat()
    require(stat.S_ISDIR(info.st_mode) and not ROOT.is_symlink() and info.st_uid == info.st_gid == 0 and
            stat.S_IMODE(info.st_mode) == 0o700 and not list(ROOT.iterdir()), 'The client output is no longer fresh and empty.')
    require(show(prepared['properties']) == prepared['properties'], 'The prepared inactive unit changed before launch.')
    intent = save('launch-intent.json', {'kind': 'nextup-client-discovery-launch-intent', 'version': 1,
        'createdAt': datetime.now(timezone.utc).isoformat(), 'unitName': UNIT, 'input': prepared['input'],
        'unitFile': prepared['unitFile'], 'sourceClosure': prepared['sourceClosure'], 'node': prepared['node'],
        'preparation': {'path': str(E / 'preparation.json'), 'sha256': preparation_sha},
        'preservationBefore': {'path': str(E / 'preservation-before.json'), 'sha256': preservation_sha},
        'oneShot': True, 'permittedPlaybackAttempts': 0, 'maximumSeconds': 1189})
    result = subprocess.run(['/usr/bin/systemctl', '--no-block', 'start', UNIT], capture_output=True, text=True, timeout=20)
    fields = {'Id', 'LoadState', 'ActiveState', 'SubState', 'MainPID', 'ExecMainPID', 'Result',
              'ExecMainStatus', 'InvocationID', 'ControlGroup'}
    for _ in range(10):
        properties = show(fields)
        if properties.get('InvocationID') or result.returncode != 0:
            break
        time.sleep(0.1)
    record = save('launch-result.json', {'version': 1, 'intent': intent, 'capturedAt': datetime.now(timezone.utc).isoformat(),
        'launchReturnCode': result.returncode, 'launchStdout': result.stdout, 'launchStderr': result.stderr,
        'properties': properties})
    print(json.dumps({'record': record, 'launchReturnCode': result.returncode, 'properties': properties}))


if __name__ == '__main__':
    main()
