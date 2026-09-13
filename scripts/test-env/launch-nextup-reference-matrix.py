import sys
assert sys.flags.isolated and sys.flags.dont_write_bytecode
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import time
from datetime import datetime, timezone

assert sys.platform == 'linux' and os.geteuid() == 0 and os.environ.get('SSH_CONNECTION')
assert len(sys.argv) == 4 and all(re.fullmatch(r'[0-9a-f]{64}', value) for value in sys.argv[1:])
finalized_sha, before_sha, attestation_sha = sys.argv[1:]
os.umask(0o077)
W = Path('/opt/goby-test/exec-work-m3e')
E = W / 'reference-nextup-global-matrix-execution-07'
A = W / 'reference-nextup-global-matrix-attestation-07/attestation.json'
UNIT = 'goby-nextup-global-reference-matrix-07.service'
UNIT_FILE = Path('/run/systemd/system') / UNIT
ATTESTATION_SHA = attestation_sha


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def identity(value):
    return (value.st_dev, value.st_ino, value.st_mode, value.st_nlink, value.st_uid,
            value.st_gid, value.st_size, value.st_mtime_ns, value.st_ctime_ns)


def read(path, checksum):
    before = path.lstat()
    assert stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0
    assert before.st_nlink == 1 and not before.st_mode & 0o022 and before.st_size <= 64 * 1024 * 1024
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as stream:
        assert identity(os.fstat(stream.fileno())) == identity(before)
        raw = stream.read()
        assert identity(os.fstat(stream.fileno())) == identity(before)
    assert identity(path.lstat()) == identity(before) and digest(raw) == checksum
    return raw


def save(name, value):
    raw = (json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + '\n').encode()
    with (E / name).open('xb') as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    directory = os.open(E, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)
    return {'path': str(E / name), 'sha256': digest(raw)}


finalized = json.loads(read(E / 'unit-finalized.json', finalized_sha))
attestation = json.loads(read(A, ATTESTATION_SHA))
preserved = json.loads(read(E / 'preservation-before.json', before_sha))
assert finalized['unitName'] == UNIT and finalized['notStarted'] is True and finalized['placeholderHash'] is False
assert finalized['startCalls'] == 0 and finalized['attestation'] == {'path': str(A), 'sha256': ATTESTATION_SHA}
assert preserved['phase'] == 'before' and preserved['matrixAttestation'] == finalized['attestation']
assert preserved['root_count'] == len(preserved['roots']) == 186 and preserved['old181RootsEqual'] is True
assert preserved['matrixUnitFile']['sha256'] == finalized['unitFile']['sha256']
assert preserved['goby_counts'] == {'sessions': 83, 'devices': 70, 'activity_entries': 185}
assert read(UNIT_FILE, finalized['unitFile']['sha256'])
source = attestation['sources']['operator']
assert source['path'] == str(W / 'nextup-global-reference-operator-tool-05/revision-01/run-nextup-global-reference.py')
assert source['sha256'] == 'ece90b7ff8a0d56a1b08825bd0dd7ec5b4603da432af6410a4af8ebe4dd968ce'
read(Path(source['path']), source['sha256'])
for root in (W / 'nextup-global-reference-runs-05/matrix-05', W / 'nextup-global-reference-operator-runs-07/operator-07'):
    assert not os.path.lexists(root)
    info = root.parent.lstat()
    assert stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and stat.S_IMODE(info.st_mode) == 0o700
keys = set(finalized['properties'])
observed = subprocess.run(['/usr/bin/systemctl', 'show', UNIT, '--property=' + ','.join(sorted(keys))],
                          capture_output=True, text=True, timeout=10)
assert observed.returncode == 0
properties = dict(line.split('=', 1) for line in observed.stdout.splitlines() if '=' in line)
assert properties == finalized['properties']
assert [properties[key] for key in ('ActiveState', 'SubState', 'MainPID', 'ExecMainPID', 'InvocationID')] == ['inactive', 'dead', '0', '0', '']
intent = save('launch-intent.json', {'schemaVersion': 1, 'kind': 'nextup-global-reference-matrix-launch-intent',
    'createdAt': datetime.now(timezone.utc).isoformat(), 'unitName': UNIT, 'attestation': finalized['attestation'],
    'unitFile': finalized['unitFile'], 'finalizedUnit': {'path': str(E / 'unit-finalized.json'), 'sha256': finalized_sha},
    'preservationBefore': {'path': str(E / 'preservation-before.json'), 'sha256': before_sha},
    'operatorSource': source, 'oneShot': True, 'preparationReplayRequiredBeforeHttp': True})
read(UNIT_FILE, finalized['unitFile']['sha256'])
launched = subprocess.run(['/usr/bin/systemctl', '--no-block', 'start', UNIT], capture_output=True, text=True, timeout=20)
fields = 'Id,LoadState,ActiveState,SubState,MainPID,ExecMainPID,Result,ExecMainStatus,InvocationID,ControlGroup'
for attempt in range(10):
    observed = subprocess.run(['/usr/bin/systemctl', 'show', UNIT, '--property=' + fields], capture_output=True, text=True, timeout=10)
    assert observed.returncode == 0
    properties = dict(line.split('=', 1) for line in observed.stdout.splitlines() if '=' in line)
    if properties.get('InvocationID') or launched.returncode != 0:
        break
    time.sleep(0.1)
record = save('launch-result.json', {'schemaVersion': 1, 'intent': intent, 'capturedAt': datetime.now(timezone.utc).isoformat(),
    'launchReturnCode': launched.returncode, 'launchStdout': launched.stdout, 'launchStderr': launched.stderr, 'properties': properties})
print(json.dumps({'record': record, 'launchReturnCode': launched.returncode, 'properties': properties}))
