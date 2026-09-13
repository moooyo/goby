import sys
if not (sys.flags.isolated and sys.flags.dont_write_bytecode):
    raise SystemExit('Python -I -B is required.')
import copy
import datetime
import hashlib
import json
import os
import re
from pathlib import Path
import stat
import subprocess
import types

W = Path('/opt/goby-test/exec-work-m3e')
E = W / 'reference-nextup-global-matrix-execution-07'
I = W / 'reference-nextup-global-matrix-attestation-07/attestation.json'
R = W / 'nextup-global-reference-runs-05/matrix-05'
O = W / 'nextup-global-reference-operator-runs-07/operator-07'
UNIT = 'goby-nextup-global-reference-matrix-07.service'
UNIT_FILE = Path('/run/systemd/system') / UNIT
assert len(sys.argv) in (4, 5), 'Usage: capture.py before|after ATTESTATION_SHA256 UNIT_FILE_SHA256 [SERVICE_BASELINE_SHA256]'
phase, input_sha256, unit_sha256 = sys.argv[1:4]
service_baseline_sha = sys.argv[4] if len(sys.argv) == 5 else None
assert service_baseline_sha is None or re.fullmatch(r'[0-9a-f]{64}', service_baseline_sha)
assert phase in ('before', 'after') and all(re.fullmatch(r'[0-9a-f]{64}', value) for value in (input_sha256, unit_sha256))
assert sys.platform == 'linux' and os.geteuid() == 0 and os.environ.get('SSH_CONNECTION')
os.umask(0o077)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def identity(value):
    return (value.st_dev, value.st_ino, value.st_mode, value.st_uid, value.st_gid,
            value.st_nlink, value.st_size, value.st_mtime_ns, value.st_ctime_ns)


def read(path, pin=None):
    before = path.lstat()
    assert stat.S_ISREG(before.st_mode) and before.st_size <= 256 << 20
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as stream:
        assert identity(os.fstat(stream.fileno())) == identity(before)
        raw = stream.read(256 * 1024 * 1024 + 1)
        assert identity(os.fstat(stream.fileno())) == identity(before)
    assert identity(path.lstat()) == identity(before)
    assert len(raw) == before.st_size and (pin is None or digest(raw) == pin), str(path)
    return raw


def write(name, value):
    raw = (json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + '\n').encode()
    with (E / name).open('xb') as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    return {'path': str(E / name), 'sha256': digest(raw)}


def info(path):
    before = path.lstat()
    value = {'device': before.st_dev, 'inode': before.st_ino, 'uid': before.st_uid, 'gid': before.st_gid,
             'mode': before.st_mode, 'links': before.st_nlink, 'bytes': before.st_size,
             'mtime_ns': before.st_mtime_ns, 'ctime_ns': before.st_ctime_ns}
    if stat.S_ISLNK(before.st_mode):
        value['symlink'] = os.readlink(path)
    elif stat.S_ISREG(before.st_mode):
        value['sha256'] = digest(read(path))
    else:
        assert stat.S_ISDIR(before.st_mode)
    assert identity(before) == identity(path.lstat())
    return value


attestation = json.loads(read(I, input_sha256))
assert attestation['kind'] == 'nextup-global-reference-matrix-attestation'
assert attestation['runtime']['unitName'] == UNIT
assert attestation['scope']['operatorEvidenceRoot'] == str(O)
previous = json.loads(read(W / 'reference-nextup-global-matrix-diagnostic-04-execution/preservation-after.json',
                          '8937b0f0c83342cf8a07a4a3ba82e74e7f08bc50311e478f6d25f6d0216a5c18'))
service_reconciliation = None
expected_services = copy.deepcopy(previous['services'])
if service_baseline_sha is not None:
    baseline_path = E / 'prelaunch-preservation-failure.json'
    baseline = json.loads(read(baseline_path, service_baseline_sha))
    assert baseline['status'] == 'service_baseline_drift_before_launch'
    assert baseline['matrixStarted'] is False and baseline['businessHttpRequests'] == 0
    assert baseline['matrixEvidenceRootAbsent'] is True and baseline['mainFileHashDifferences'] == []
    assert baseline['previousPreservationSHA256'] == '8937b0f0c83342cf8a07a4a3ba82e74e7f08bc50311e478f6d25f6d0216a5c18'
    assert set(baseline['differences']) == {'goby-client-m3e.service', 'goby-foundation-test.service'}
    for name, changes in baseline['differences'].items():
        for key, value in changes.items():
            assert set(value) == {'expected', 'actual'} and expected_services[name][key] == value['expected']
            expected_services[name][key] = value['actual']
    assert expected_services['goby-client-m3e.service']['MainPID'] == '0'
    assert expected_services['goby-client-m3e.service']['ActiveState'] == 'failed'
    assert expected_services['goby-foundation-test.service']['ActiveState'] == 'active'
    assert expected_services['goby-foundation-test.service']['SubState'] == 'running'
    service_reconciliation = {'path': str(baseline_path), 'sha256': service_baseline_sha}
assert len(previous['roots']) == 181
assert len(attestation['sealedRoots']) == len(set(attestation['sealedRoots'])) == 163
assert set(attestation['sealedRoots']).issubset(previous['roots'])
capture_roots = sorted(previous['roots']) + [str(W / name) for name in (
    'reference-nextup-global-matrix-diagnostic-04-execution', 'reference-nextup-global-matrix-diagnostic-04-output',
    'nextup-global-matrix-assembly-tool-03', 'nextup-global-matrix-assembly-inputs-07',
    'reference-nextup-global-matrix-attestation-07')]
assert len(capture_roots) == len(set(capture_roots)) == 186
assert all(target != Path(root) and Path(root) not in target.parents and target not in Path(root).parents
           for target in (E, R, O, O.parent) for root in capture_roots)
assert digest(read(UNIT_FILE)) == unit_sha256
unit_file = info(UNIT_FILE)
assert unit_file['sha256'] == unit_sha256

reader = W / 'prepare-client-fixture.py'
raw = read(reader, '84d21e8ac0b48c5dfd3d2c7ae35b0aec0d7f4f811e65658081490aa44947b49c')
op = types.ModuleType('matrix07_owned_goby_preservation_reader')
op.__file__ = str(reader)
exec(compile(raw, str(reader), 'exec'), op.__dict__)
fixture = op.precise_json(read(W / 'client-fixture.json', 'bb78a846d2d4b69b7e2550ed9e367549cbe2d0b763e2b69270ea98bed6570d82'))
current = op.preservation_snapshot(fixture, 28)
prior_goby = op.precise_json(read(W / 'client-library-changed-source55-execution-07/independent-after-full.json',
                                '31d08d24047da101774db727f4ea6f0ec0e67ce15b0ce3df40ec7ea819555364'))


def stable(value):
    result = copy.deepcopy(value)
    result['database']['metadata'].pop('captured_at')
    return op.canonical_json(result)


assert stable(current) == stable(prior_goby)
counts = {key: len(current['database']['tables'][key]) for key in ('sessions', 'devices', 'activity_entries')}
assert counts == {'sessions': 83, 'devices': 70, 'activity_entries': 185}
snapshot = write(('goby-reconciled-' if service_reconciliation else 'goby-') + phase + '.json', current)
trees = {}
for name in capture_roots:
    root = Path(name)
    assert root.parent == W or root == Path('/opt/goby-fixtures/client-m3e')
    assert root.is_dir() and not root.is_symlink()
    assert all(root != Path(forbidden) and Path(forbidden) not in root.parents and root not in Path(forbidden).parents
               for forbidden in attestation['forbiddenOriginalRoots'])
    paths = [root, *sorted(root.rglob('*'))]
    assert len(paths) <= 2000
    trees[name] = {str(path.relative_to(root)): info(path) for path in paths}
assert all(trees[name] == value for name, value in previous['roots'].items())
assert trees[str(I.parent)]['attestation.json']['sha256'] == input_sha256
services = {}
fields = 'Id,LoadState,ActiveState,SubState,MainPID,Result,ExecMainStatus,InvocationID,ControlGroup'
for name, expected in expected_services.items():
    result = subprocess.run(['/usr/bin/systemctl', 'show', name, '--property=' + fields], capture_output=True, text=True, timeout=10)
    assert result.returncode == 0
    services[name] = dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)
    assert services[name] == expected, name
prep_unit = 'goby-nextup-global-reference-preparation-05.service'
result = subprocess.run(['/usr/bin/systemctl', 'show', prep_unit, '--property=' + fields], capture_output=True, text=True, timeout=10)
assert result.returncode == 0
services[prep_unit] = dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)
assert all(services[prep_unit][key] == value for key, value in {
    'ActiveState': 'active', 'SubState': 'exited', 'MainPID': '0', 'Result': 'success', 'ExecMainStatus': '0',
    'InvocationID': 'cf198945fa894c6c93d23cd53d1798bf'}.items())
main_files = {name: info(Path(name)) for name in previous['main_files']}
assert main_files == previous['main_files']
assert info(UNIT_FILE) == unit_file
record = {'schemaVersion': 1, 'kind': 'nextup-global-matrix-preservation', 'phase': phase,
          'matrixAttestation': {'path': str(I), 'sha256': input_sha256}, 'matrixUnitFile': unit_file,
          'capturedAt': datetime.datetime.now(datetime.timezone.utc).isoformat(),
          'goby_counts': counts, 'goby_snapshot': snapshot, 'root_count': len(trees), 'roots': trees,
          'services': services, 'main_files': main_files, 'old181RootsEqual': True,
          'serviceBaselineReconciliation': service_reconciliation,
          'historical_v7_goby_state_equal_except_capture_time': True, 'reference_database_read': False,
          'original_implementation_bytes_read': False, 'businessHttpRequests': 0}
if phase == 'after':
    before = json.loads(read(E / 'preservation-before.json'))
    assert all(before[key] == record[key] for key in
               ('matrixAttestation', 'matrixUnitFile', 'goby_counts', 'roots', 'services', 'main_files', 'serviceBaselineReconciliation'))
receipt = write('preservation-' + phase + '.json', record)
print(json.dumps({'phase': phase, 'gobyCounts': counts, 'rootCount': len(trees), 'record': receipt,
                  'gobySnapshot': snapshot, 'old181RootsEqual': True, 'businessHttpRequests': 0,
                  'originalImplementationBytesRead': False, 'referenceDatabaseRead': False}))
