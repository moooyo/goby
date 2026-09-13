#!/usr/bin/env python3
"""Independently close matrix07 runtime, identity, and preservation evidence.

Run only through authorized SSH on test-env with /usr/bin/python3 -I -B.
This reads retained evidence and process metadata, never sends HTTP or opens
reference implementation/database bytes. Full matrix wire replay is a separate
acceptance gate. The output is exclusive and never permits a business retry.
"""

from __future__ import annotations

import argparse
import ast
from copy import deepcopy
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import stat
import subprocess
import sys

W = Path('/opt/goby-test/exec-work-m3e')
A = W / 'reference-nextup-global-matrix-attestation-07/attestation.json'
E = W / 'reference-nextup-global-matrix-execution-07'
O = W / 'nextup-global-reference-operator-runs-07/operator-07'
R = W / 'nextup-global-reference-runs-05/matrix-05'
UNIT = 'goby-nextup-global-reference-matrix-07.service'
UNIT_FILE = Path('/run/systemd/system') / UNIT
OUTPUT = E / 'independent-runtime-terminal.json'
POLICY = 'owned-proxy-executable-file-object-v1'
# Published by docs/development/nextup-live-identity-closeout-05.md.
OPERATOR = W / 'nextup-global-reference-operator-tool-05/revision-01/run-nextup-global-reference.py'
OPERATOR_SHA = 'ece90b7ff8a0d56a1b08825bd0dd7ec5b4603da432af6410a4af8ebe4dd968ce'
TRANSPORT_SHA = '4134c66a58a1542fc3c7dc9007bcd9ae289d094bb7db28a95d59a3557be4ceb8'
MATRIX_SHA = 'a69ff17c26934abbf09375a7832d7e11cd898d5e696c4ba5ce8a718efd19e65d'
PREVIOUS = W / 'reference-nextup-global-matrix-diagnostic-04-execution/preservation-after.json'
PREVIOUS_SHA = '8937b0f0c83342cf8a07a4a3ba82e74e7f08bc50311e478f6d25f6d0216a5c18'
HISTORICAL_GOBY = W / 'client-library-changed-source55-execution-07/independent-after-full.json'
HISTORICAL_GOBY_SHA = '31d08d24047da101774db727f4ea6f0ec0e67ce15b0ce3df40ec7ea819555364'
SERVICE_RECONCILIATION = E / 'prelaunch-preservation-failure.json'
SERVICE_RECONCILIATION_SHA = 'c27b0d3a544ac5b4ca44eaa655f42e4be338ecddb5cf5523ea738f191eff84fa'
FORBIDDEN = (Path('/dev/shm/goby-emby-reference/package'), W / 'reference-data')
MAX_BYTES = 256 * 1024 * 1024
STAT_FIELDS = {'device', 'inode', 'mode', 'nlink', 'uid', 'gid', 'sizeBytes', 'mtimeNs', 'ctimeNs'}


class VerificationError(ValueError):
    """Evidence is missing, inconsistent, or insufficient for independent closure."""


def require(condition, message):
    if not condition:
        raise VerificationError(message)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=True, allow_nan=False)


def same(left, right):
    return canonical(left) == canonical(right)


def checksum(value):
    return isinstance(value, str) and re.fullmatch(r'[0-9a-f]{64}', value) is not None


def instant(value):
    require(isinstance(value, str), 'A captured timestamp is missing.')
    result = datetime.fromisoformat(value.replace('Z', '+00:00'))
    require(result.utcoffset() is not None, 'A captured timestamp lacks its timezone.')
    return result


def exact_json(raw):
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, 'A JSON object repeats a field.')
            result[key] = value
        return result

    def invalid_constant(_value):
        raise VerificationError('Nonfinite JSON is forbidden.')

    return json.loads(raw, object_pairs_hook=pairs, parse_constant=invalid_constant)


def file_identity(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid,
            info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def kernel_stat(info):
    return dict(zip(('device', 'inode', 'mode', 'nlink', 'uid', 'gid', 'sizeBytes', 'mtimeNs', 'ctimeNs'),
                    (info.st_dev, info.st_ino, info.st_mode, info.st_nlink, info.st_uid,
                     info.st_gid, info.st_size, info.st_mtime_ns, info.st_ctime_ns)))


class Verifier:
    def __init__(self, attestation_sha):
        self.attestation_sha = attestation_sha
        self.evidence = {}
        self.stage = 'admission'
        self.endpoint_exe = None
        self.bound_executable_objects = set()
        self.counters = {'networkAttempts': 0, 'forbiddenByteReadAttempts': 0,
                         'unexpectedProcessAttempts': 0, 'outsideOutputWriteAttempts': 0,
                         'metadataOnlyExecutableOpens': 0}
        self.summary = {}

    def audit(self, event, args):
        if event.startswith('socket.') or event.startswith('http.client.'):
            self.counters['networkAttempts'] += 1
            raise VerificationError('Network access is forbidden in runtime closure.')
        if event == 'subprocess.Popen':
            command = args[1]
            allowed = (isinstance(command, list) and len(command) == 4 and
                       command[:3] == ['/usr/bin/systemctl', 'show', UNIT] and
                       command[3].startswith('--property='))
            if not allowed:
                self.counters['unexpectedProcessAttempts'] += 1
                raise VerificationError('Only metadata observation of the exact unit is allowed.')
        if event != 'open' or not isinstance(args[0], (str, bytes, os.PathLike)):
            return
        path = Path(os.path.abspath(os.fsdecode(args[0])))
        flags = args[2]
        if any(path == root or root in path.parents for root in FORBIDDEN):
            self.counters['forbiddenByteReadAttempts'] += 1
            raise VerificationError('Reference implementation and database byte reads are forbidden.')
        executable = (path.parent.parent == Path('/proc') and path.name == 'exe') or path in (
            Path('/usr/bin/python3'), Path('/usr/bin/python3.13'))
        if executable:
            if path == self.endpoint_exe and flags == os.O_PATH | os.O_CLOEXEC:
                self.counters['metadataOnlyExecutableOpens'] += 1
            else:
                self.counters['forbiddenByteReadAttempts'] += 1
                raise VerificationError('Executable bytes cannot be opened by runtime closure.')
        writing = flags & (os.O_WRONLY | os.O_RDWR | os.O_CREAT | os.O_TRUNC | os.O_APPEND)
        if writing and path != OUTPUT:
            self.counters['outsideOutputWriteAttempts'] += 1
            raise VerificationError('Runtime closure may write only its exclusive terminal.')

    def read(self, path, pin=None, *, track=True, expected_identity=None):
        path = Path(path)
        require(path.is_absolute() and '..' not in path.parts and path.resolve(strict=True) == path,
                'Evidence must use an absolute path without symlink traversal.')
        require(not any(path == root or root in path.parents for root in FORBIDDEN), 'Forbidden evidence path.')
        before = path.lstat()
        require(stat.S_ISREG(before.st_mode) and 0 <= before.st_size <= MAX_BYTES,
                'Evidence must be a bounded regular file.')
        require(expected_identity is None or file_identity(before) == expected_identity,
                'A protected file changed before its byte read was admitted.')
        require(expected_identity is not None or before.st_nlink == 1,
                'Unsealed evidence must not have an unapproved hard-link alias.')
        if (before.st_dev, before.st_ino) in self.bound_executable_objects:
            self.counters['forbiddenByteReadAttempts'] += 1
            raise VerificationError('A bound process executable object cannot be read through an alias.')
        with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC), 'rb') as stream:
            require(file_identity(os.fstat(stream.fileno())) == file_identity(before), 'Evidence changed before reading.')
            raw = stream.read(MAX_BYTES + 1)
            require(file_identity(os.fstat(stream.fileno())) == file_identity(before), 'Evidence changed while reading.')
        require(file_identity(path.lstat()) == file_identity(before) and len(raw) == before.st_size,
                'Evidence changed after reading.')
        require(pin is None or checksum(pin) and digest(raw) == pin, 'An evidence SHA-256 binding does not match.')
        if track:
            row = {'path': str(path), 'sha256': digest(raw), 'bytes': len(raw), 'identity': list(file_identity(before))}
            require(str(path) not in self.evidence or same(self.evidence[str(path)], row), 'Previously read evidence changed.')
            self.evidence[str(path)] = row
        return raw

    def doc(self, path, pin=None):
        return exact_json(self.read(path, pin))

    def descriptor(self, row, expected_path=None):
        require(isinstance(row, dict) and set(row) == {'path', 'sha256'} and checksum(row['sha256']),
                'An exact path and SHA-256 descriptor is required.')
        path = Path(row['path'])
        require(W in path.parents and (expected_path is None or path == expected_path), 'An evidence descriptor names another scope.')
        return self.doc(path, row['sha256'])

    def bind_runtime(self, attestation):
        self.stage = 'runtime_binding'
        launch = self.doc(E / 'launch-result.json')
        intent = self.descriptor(launch['intent'], E / 'launch-intent.json')
        expected_attestation = {'path': str(A), 'sha256': self.attestation_sha}
        require(intent['kind'] == 'nextup-global-reference-matrix-launch-intent' and intent['unitName'] == UNIT and
                intent['oneShot'] is True and intent['preparationReplayRequiredBeforeHttp'] is True and
                intent['attestation'] == expected_attestation and intent['operatorSource'] == attestation['sources']['operator'],
                'The launch intent does not bind this one-shot matrix operator.')
        finalized = self.descriptor(intent['finalizedUnit'], E / 'unit-finalized.json')
        require(finalized['unitName'] == UNIT and finalized['attestation'] == expected_attestation and
                finalized['unitFile'] == intent['unitFile'] and finalized['notStarted'] is True and
                finalized['placeholderHash'] is False and finalized['startCalls'] == 0,
                'The final unit publication does not match the launch intent.')
        require(intent['unitFile']['path'] == str(UNIT_FILE), 'The launch intent names another unit file.')
        self.read(UNIT_FILE, intent['unitFile']['sha256'])
        require(launch['launchReturnCode'] == 0 and launch['launchStdout'] == '' and launch['launchStderr'] == '',
                'The recorded unit launch did not complete successfully.')
        commit = self.doc(O / 'private/commit.json')
        terminal = self.doc(O / 'private/terminal.json', commit['terminalPrivateSha256'])
        exported = self.doc(O / 'export/terminal.json', commit['terminalExportSha256'])
        require(commit['kind'] == 'nextup-global-reference-matrix-operator-commit' and commit['schemaVersion'] == 1 and
                commit['runId'] == terminal['runId'] == attestation['runId'] and
                commit['attestationSha256'] == self.attestation_sha, 'The operator commit belongs to another admission.')
        runtime = commit['runtime']
        require(set(runtime) == {'pid', 'invocationId', 'cgroup', 'uid', 'ssh', 'isolated', 'noBytecode'} and
                type(runtime['pid']) is int and runtime['pid'] > 1 and type(runtime['uid']) is int and runtime['uid'] == 0 and
                re.fullmatch(r'[0-9a-f]{32}', runtime['invocationId']) and
                runtime['cgroup'] == '0::/system.slice/' + UNIT + '\n' and
                all(runtime[key] is True for key in ('ssh', 'isolated', 'noBytecode')),
                'The committed runtime is not the exact isolated root SSH service.')
        launched = launch['properties']
        require(launched['Id'] == UNIT and launched['InvocationID'] == runtime['invocationId'] and
                launched['ExecMainPID'] == str(runtime['pid']) and launched['MainPID'] in ('0', str(runtime['pid'])),
                'Launch-result PID or invocation differs from the committed runtime.')
        pins = {'attestationSha256': self.attestation_sha, 'executionSha256': attestation['execution']['sha256'],
                'producerTerminalSha256': attestation['preparation']['terminal']['sha256'],
                'wireIndexSha256': attestation['preparation']['wireIndex']['sha256'],
                'sources': attestation['sources'], 'runtime': runtime}
        require(same(terminal['pins'], pins), 'The private terminal pins differ from the admitted sources or runtime.')
        require(terminal['status'] == exported['status'] == 'awaiting_operator_commit' and
                terminal['completionCommitted'] is exported['completionCommitted'] is False and
                terminal['candidateStatus'] == exported['candidateStatus'] == commit['status'],
                'The durable pre-commit terminal contract is inconsistent.')
        for record in (commit, terminal, exported):
            require(record['independentRuntimeClosureRequired'] is True and record['clientAcceptanceClaim'] is False and
                    record['resumeOrRetryAllowed'] is False, 'An operator record overstates acceptance or allows replay.')
        require(same(commit['liveIdentity'], terminal['liveIdentity']), 'The commit does not bind the terminal identity index.')
        self.summary['workerStatus'] = commit['status']
        require(commit['status'] == 'matrix_protocol_complete' and terminal['failure'] is None and
                terminal['cleanupComplete'] is True, 'The worker did not complete the matrix protocol and cleanup.')
        fields = set(attestation['runtime']['properties']) | {'Id', 'LoadState', 'ActiveState', 'SubState', 'MainPID',
                 'ExecMainPID', 'InvocationID', 'Result', 'ExecMainCode', 'ExecMainStatus', 'ControlGroup', 'ExecStart',
                 'FragmentPath', 'NRestarts'}
        response = subprocess.run(['/usr/bin/systemctl', 'show', UNIT, '--property=' + ','.join(sorted(fields))],
                                  capture_output=True, text=True, check=False, timeout=15,
                                  env={'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C.UTF-8'})
        require(response.returncode == 0 and response.stderr == '' and len(response.stdout) <= 65536,
                'The terminal unit metadata could not be observed.')
        rows = response.stdout.splitlines()
        unit = dict(line.split('=', 1) for line in rows if '=' in line)
        require(len(rows) == len(unit) and set(unit) == fields, 'The unit metadata has missing or duplicate properties.')
        require(all(unit[key] == value for key, value in attestation['runtime']['properties'].items()),
                'The actual unit resource or isolation contract changed.')
        require(unit['Id'] == UNIT and unit['LoadState'] == 'loaded' and unit['MainPID'] == '0' and
                unit['ExecMainPID'] == str(runtime['pid']) and unit['InvocationID'] == runtime['invocationId'] and
                unit['Result'] == 'success' and unit['ExecMainCode'] == '1' and unit['ExecMainStatus'] == '0' and
                unit['ActiveState'] == 'active' and unit['SubState'] == 'exited' and unit['ControlGroup'] == '' and
                unit['FragmentPath'] == str(UNIT_FILE) and unit['NRestarts'] == '0',
                'The exact unit has not reached a successful empty terminal state.')
        require(not os.path.lexists('/proc/' + str(runtime['pid'])), 'The committed operator PID is still present.')
        group = Path('/sys/fs/cgroup/system.slice') / UNIT
        require(not os.path.lexists(group), 'The completed operator cgroup is still present.')
        match = re.fullmatch(
            r'\{ path=/usr/bin/python3 ; argv\[\]=([^{}\r\n]*?) ; ignore_errors=no ; '
            r'start_time=\[[^\[\]{}\r\n]+\] ; stop_time=\[[^\[\]{}\r\n]+\] ; '
            r'pid=(?:0|' + str(runtime['pid']) + r') ; code=(?:exited|\(null\)) ; status=(?:0|0/0) \}',
            unit['ExecStart'])
        require(match is not None, 'The actual unit ExecStart is not a single unambiguous command.')
        argv = shlex.split(match.group(1))
        expected_argv = ['/usr/bin/python3', '-I', '-B', str(OPERATOR), '--attestation', str(A),
                         '--attestation-sha256', self.attestation_sha]
        require(argv == expected_argv, 'The actual unit argv differs from the pinned matrix operator.')
        stdout = self.doc(O.parent / 'unit-stdout.log')
        require(self.read(O.parent / 'unit-stderr.log') == b'', 'The operator retained stderr output.')
        require(stdout['status'] == commit['status'] and stdout['commitSha256'] == self.evidence[str(O / 'private/commit.json')]['sha256'] and
                stdout['cleanupComplete'] is True and stdout['completionCommitted'] is True and
                stdout['independentRuntimeClosureRequired'] is True and stdout['clientAcceptanceClaim'] is False and
                stdout['liveIdentityPolicyVersion'] == POLICY and stdout['canonicalValueIsRawObservation'] is False,
                'The actual operator stdout does not bind the successful commit.')
        self.summary['unit'] = {'name': UNIT, 'runtime': runtime, 'argvSha256': digest(canonical(argv).encode()),
                                'argvExact': True, 'formerPidAbsent': True, 'cgroupAbsent': True,
                                'properties': {key: value for key, value in unit.items() if key != 'ExecStart'}}
        return launch, intent, commit, terminal, stdout, runtime

    def check_kernel(self, raw, proof, expected):
        require(isinstance(raw, dict) and set(raw) == set(expected), 'The raw process identity has another shape.')
        normalized = deepcopy(raw)
        exact = same(raw, expected)
        if not exact:
            require(raw['endpoint']['exe'] == expected['endpoint']['exe'] + ' (deleted)', 'A non-approved executable display changed.')
            normalized['endpoint']['exe'] = expected['endpoint']['exe']
        require(same(normalized, expected), 'A stable application, proxy, listener, or namespace identity changed.')
        require(set(proof) == {'pid', 'procPath', 'readlink', 'descriptorStat', 'namedStat'} and type(proof['pid']) is int and
                proof['pid'] == expected['endpoint']['pid'] and proof['procPath'] == '/proc/' + str(proof['pid']) + '/exe' and
                proof['readlink'] == raw['endpoint']['exe'], 'The kernel observation identifies another executable.')
        opened, named = proof['descriptorStat'], proof['namedStat']
        require(set(opened) == set(named) == STAT_FIELDS and all(type(value) is int for value in opened.values()) and
                all(type(value) is int for value in named.values()) and same(opened, named) and stat.S_ISREG(opened['mode']) and
                opened['nlink'] >= 0 and opened['sizeBytes'] >= 0 and
                opened['device'] == raw['endpoint']['exeDevice'] == expected['endpoint']['exeDevice'] and
                opened['inode'] == raw['endpoint']['exeInode'] == expected['endpoint']['exeInode'] and (exact or opened['nlink'] == 0),
                'The kernel metadata does not establish the same regular executable object.')
        return 'raw-exact' if exact else 'kernel-deleted-display-same-file-object'

    def identity_chain(self, attestation, execution, runtime, terminal, stdout, intent, after, maximum_requests):
        self.stage = 'identity_receipt_chain'
        live = terminal['liveIdentity']
        index = self.descriptor(live['index'], O / 'private/identity-index.json')
        require(live['policyVersion'] == POLICY and live['indexComplete'] is True and live['blocked'] is False and
                live['pending'] is live['persistenceFailure'] is None and live['canonicalValueIsRawObservation'] is False,
                'The worker identity proof did not finalize cleanly.')
        require(index['schemaVersion'] == 1 and index['kind'] == 'nextup-live-identity-index' and index['policyVersion'] == POLICY and
                index['maximumProbeCount'] == 3 * maximum_requests + 4 and index['blocked'] is False and
                index['pending'] is index['persistenceFailure'] is None and index['canonicalValueIsRawObservation'] is False,
                'The retained identity index is incomplete or has another policy.')
        expected = execution['process']
        context = {'runId': attestation['runId'], 'attestationSha256': self.attestation_sha,
                   'executionSha256': attestation['execution']['sha256'], 'operatorSource': attestation['sources']['operator'],
                   'runtime': runtime}
        expected_sha = digest(canonical(expected).encode())
        require(same(index['context'], context) and index['expectedBoundIdentitySha256'] == expected_sha,
                'The identity index is not bound to this exact source, execution and runtime.')
        result = terminal['transportResult']
        state = self.doc(R / 'private/state.json')
        require(result['mode'] == 'closed' and result['failure'] is None and result['failureEvents'] == [] and
                result['cleanupComplete'] is result['evidenceComplete'] is True and result['persistenceFailure'] is None and
                result['unverifiedLoginActors'] == result['unresolvedResponses'] == [] and result['liveAcceptanceClaim'] is False and
                state['blocked'] is False and state['finished'] is True and state['failure'] is state['persistenceFailure'] is None and
                state['failureEvents'] == state['unresolvedResponses'] == [] and state['unverifiedLogins'] == {} and
                state['matrix']['pending'] is None and state['matrix']['revokedActors'] == ['P', 'Q'],
                'The retained transport state has incomplete requests, failure, or unresolved ownership.')
        requests = result['httpAttempts']
        require(type(requests) is int and 0 < requests <= maximum_requests and requests == result['reservedRequests'] ==
                stdout['requestCount'] == state['matrix']['requestCount'] == len(state['attemptedLabels']),
                'The complete actual request counts disagree.')
        labels = set(state['attemptedLabels'])
        require(len(labels) == requests and set(state['matrix']['completedLabels']) == labels,
                'Reserved and completed request responsibilities differ.')
        request_files = {path.name for path in (R / 'private').iterdir()}
        wires = sorted(name for name in request_files if name.endswith('-wire.json'))
        require(len(wires) == requests and [int(name[:4]) for name in wires] == list(range(1, requests + 1)),
                'The retained wire file ordinals are incomplete.')
        for name in wires:
            prefix = name[:-len('-wire.json')]
            require(prefix[5:] in labels and prefix + '-intent.json' in request_files and prefix + '-reserved.json' in request_files,
                    'An actual request lacks its intent or reservation evidence.')
        count = 1 + 3 * requests
        for key, value in (('probeCount', count), ('canonicalReturnCount', count)):
            require(type(index[key]) is int and type(live[key]) is int and index[key] == live[key] == value,
                    'The identity callbacks do not match one initial check plus three checks per actual request.')
        require(type(index['aliasReturnCount']) is int and index['aliasReturnCount'] == live['aliasReturnCount'] and
                len(index['entries']) == len(index['receiptChain']) == count,
                'The complete index/receipt chain counts disagree.')
        previous_sha, anchor, charged, aliases = None, None, 0, 0
        receipts = []
        expected_names = {'admission.json', 'terminal.json', 'commit.json', 'identity-index.json'}
        previous_time = instant(intent['createdAt'])
        for ordinal, entry in enumerate(index['entries'], 1):
            require(set(entry) == {'probeOrdinal', 'status', 'observation', 'failure', 'comparisonDecision',
                                 'postPersistenceCheckPassed', 'rawBeforeReturnSha256', 'kernelBeforeReturnSha256'} and
                    type(entry['probeOrdinal']) is int and entry['probeOrdinal'] == ordinal and
                    entry['status'] == 'canonical-return-authorized' and entry['failure'] is None and
                    entry['postPersistenceCheckPassed'] is True, 'An identity callback was not durably authorized.')
            name = 'identity-%04d-observation.json' % ordinal
            expected_names.add(name)
            row = entry['observation']
            observation = self.descriptor(row, O / 'private' / name)
            charged += self.evidence[row['path']]['bytes']
            require(set(observation) == {'schemaVersion', 'policyVersion', 'context', 'expectedBoundIdentitySha256',
                'probeOrdinal', 'previousReceiptSha256', 'kind', 'capturedAt', 'observations', 'comparisonDecision',
                'rawMetadataChanged', 'canonicalBoundIdentity', 'canonicalBoundIdentitySha256', 'canonicalValueIsRawObservation', 'stage'},
                'An identity observation has an unexpected shape.')
            require(observation['schemaVersion'] == 1 and observation['kind'] == 'nextup-live-identity-observation' and
                    observation['policyVersion'] == POLICY and same(observation['context'], context) and
                    type(observation['probeOrdinal']) is int and observation['probeOrdinal'] == ordinal and
                    observation['previousReceiptSha256'] == previous_sha and
                    observation['expectedBoundIdentitySha256'] == observation['canonicalBoundIdentitySha256'] == expected_sha and
                    same(observation['canonicalBoundIdentity'], expected) and observation['canonicalValueIsRawObservation'] is False and
                    observation['stage'] == 'comparison-passed-awaiting-post-persistence-check',
                    'An identity observation chain or canonical binding differs.')
            captured = instant(observation['capturedAt'])
            require(previous_time <= captured <= instant(after['capturedAt']), 'Identity receipt time lies outside the preserved run.')
            previous_time = captured
            data = observation['observations']
            require(set(data) == {'rawBefore', 'rawAfter', 'kernelBefore', 'kernelAfter'} and
                    same(data['rawBefore'], data['rawAfter']) and same(data['kernelBefore'], data['kernelAfter']),
                    'An identity observation changed across its capture.')
            decision = self.check_kernel(data['rawAfter'], data['kernelAfter'], expected)
            changed = decision != 'raw-exact'
            aliases += int(changed)
            require(observation['comparisonDecision'] == entry['comparisonDecision'] == decision and
                    observation['rawMetadataChanged'] is changed and
                    entry['rawBeforeReturnSha256'] == digest(canonical(data['rawAfter']).encode()) and
                    entry['kernelBeforeReturnSha256'] == digest(canonical(data['kernelAfter']).encode()),
                    'A post-persistence identity check is not bound to its unchanged raw/kernel observation.')
            current = {'raw': data['rawAfter'], 'kernel': data['kernelAfter']}
            require(anchor is None or same(current, anchor), 'The first successful identity anchor changed during the run.')
            anchor = current
            require(index['receiptChain'][ordinal - 1] == {'probeOrdinal': ordinal, **row}, 'The identity receipt chain index differs.')
            previous_sha = row['sha256']
            receipts.append(row)
        require(type(index['chargedReceiptBytes']) is int and index['chargedReceiptBytes'] == charged and
                aliases == index['aliasReturnCount'] and index['rawMetadataChanged'] is bool(aliases) and
                live['rawMetadataChanged'] is bool(aliases) and stdout['rawMetadataChanged'] is bool(aliases),
                'The exact identity receipt byte charge or alias totals differ.')
        require({path.name for path in (O / 'private').iterdir()} == expected_names and
                {path.name for path in (O / 'export').iterdir()} == {'terminal.json'},
                'The complete operator output file set differs from a clean committed run.')
        admission = self.doc(O / 'private/admission.json')
        producer_terminal = self.descriptor(attestation['preparation']['terminal'])
        require(admission['runId'] == attestation['runId'] and same(admission['pins'], terminal['pins']) and
                admission['replayedPreparationRequests'] == producer_terminal['requestCount'] and
                admission['producerOutputsReproduced'] is True and admission['liveIdentityPolicyVersion'] == POLICY and
                admission['canonicalValueIsRawObservation'] is False, 'The admission receipt does not bind its preparation replay.')
        self.summary.update(actualBusinessHttpRequests=requests, rawReceiptCount=count, canonicalReturnCount=count,
                            aliasReturnCount=aliases, observationReceipts=receipts, identityIndex=live['index'],
                            identityCallFormula='1 + 3 * actualBusinessHttpRequests', completeOperatorOutputSetVerified=True,
                            rawAndKernelAnchorStable=True, postPersistenceChecksBoundByIndex=True,
                            preparationReplayCount=admission['replayedPreparationRequests'])
        return anchor

    def process_metadata(self, expected):
        pid = expected['pid']
        require(type(pid) is int and pid > 1, 'A process metadata target lacks its exact PID.')
        process = Path('/proc') / str(pid)
        raw = (process / 'stat').read_bytes()
        require(len(raw) <= 65536, 'A process stat exceeded its metadata bound.')
        text = raw.decode()
        fields = text[text.rfind(')') + 2:].split()
        command = (process / 'cmdline').read_bytes()
        require(len(command) <= 65536, 'A process command line exceeded its metadata bound.')
        executable = (process / 'exe').stat()
        return {'pid': pid, 'startTicks': fields[19], 'bootId': Path('/proc/sys/kernel/random/boot_id').read_text().strip(),
                'uid': process.stat().st_uid, 'exe': os.readlink(process / 'exe'), 'exeDevice': executable.st_dev,
                'exeInode': executable.st_ino, 'cmdline': [part.decode('utf-8') for part in command.split(b'\0') if part],
                'networkNamespace': os.readlink(process / 'ns/net'), 'cgroup': (process / 'cgroup').read_text()}

    def process_identity(self, expected):
        application = self.process_metadata(expected['application'])
        endpoint = self.process_metadata(expected['endpoint'])
        process = Path('/proc') / str(expected['endpoint']['pid'])
        listener = expected['endpoint']['listener']
        require(type(listener['port']) is int and 0 < listener['port'] <= 65535, 'The listener port is not exact.')
        address = '0100007F:' + format(listener['port'], '04X')
        tcp = (process / 'net/tcp').read_bytes()
        require(len(tcp) <= 8 * 1024 * 1024, 'The listener metadata exceeded its bound.')
        rows = [line.split() for line in tcp.decode().splitlines()[1:]]
        require(any(len(row) > 9 and row[1] == address and row[3] == '0A' and row[9] == listener['socketInode'] for row in rows),
                'The exact bound loopback listener is absent.')
        entries = list((process / 'fd').iterdir())
        require(len(entries) <= 65536, 'The proxy descriptor metadata exceeded its bound.')
        owned = False
        for path in entries:
            try:
                owned = owned or os.readlink(path) == 'socket:[' + listener['socketInode'] + ']'
            except FileNotFoundError:
                continue
        require(owned, 'The exact proxy does not own its bound listener socket.')
        endpoint['listener'] = deepcopy(listener)
        return {'application': application, 'endpoint': endpoint, 'workerNetworkNamespace': os.readlink('/proc/self/ns/net')}

    def fresh_identity(self, expected, anchor):
        self.stage = 'fresh_metadata_observation'
        self.endpoint_exe = Path('/proc') / str(expected['endpoint']['pid']) / 'exe'
        fd = os.open(self.endpoint_exe, os.O_PATH | os.O_CLOEXEC)
        try:
            samples = []
            for _ in range(3):
                raw = self.process_identity(expected)
                kernel = {'pid': expected['endpoint']['pid'], 'procPath': str(self.endpoint_exe),
                          'readlink': os.readlink(self.endpoint_exe), 'descriptorStat': kernel_stat(os.fstat(fd)),
                          'namedStat': kernel_stat(os.stat(self.endpoint_exe))}
                self.check_kernel(raw, kernel, expected)
                observed = {'raw': raw, 'kernel': kernel}
                require(same(observed, anchor), 'Fresh process metadata differs from the retained run anchor.')
                samples.append({'rawSha256': digest(canonical(raw).encode()), 'kernelSha256': digest(canonical(kernel).encode())})
        finally:
            os.close(fd)
        self.summary['freshMetadataObservation'] = {'capturedAt': datetime.now(timezone.utc).isoformat(),
            'sampleCount': len(samples), 'samples': samples, 'matchesRetainedAnchor': True,
            'metadataOnlyExecutableOpen': True, 'canonicalValueIsRawObservation': False}

    def tree_info(self, path, expected):
        before = path.lstat()
        result = {'device': before.st_dev, 'inode': before.st_ino, 'uid': before.st_uid, 'gid': before.st_gid,
                  'mode': before.st_mode, 'links': before.st_nlink, 'bytes': before.st_size,
                  'mtime_ns': before.st_mtime_ns, 'ctime_ns': before.st_ctime_ns}
        # Admit the exact sealed object before any byte read. A changed file or
        # new hard-link alias must fail without opening its contents.
        require(isinstance(expected, dict) and all(key in expected for key in result) and
                same(result, {key: expected[key] for key in result}),
                'Protected metadata changed before its byte read was admitted.')
        if stat.S_ISLNK(before.st_mode):
            result['symlink'] = os.readlink(path)
        elif stat.S_ISREG(before.st_mode):
            result['sha256'] = digest(self.read(path, track=False, expected_identity=file_identity(before)))
        else:
            require(stat.S_ISDIR(before.st_mode), 'A protected tree contains an unsupported file type.')
        require(file_identity(path.lstat()) == file_identity(before), 'A protected tree entry changed while observed.')
        return result

    def preservation(self, attestation, intent, launch):
        self.stage = 'preservation'
        before = self.descriptor(intent['preservationBefore'], E / 'preservation-before.json')
        after = self.doc(E / 'preservation-after.json')
        previous = self.doc(PREVIOUS, PREVIOUS_SHA)
        require(before['kind'] == after['kind'] == 'nextup-global-matrix-preservation' and
                before['phase'] == 'before' and after['phase'] == 'after' and
                instant(before['capturedAt']) <= instant(intent['createdAt']) <= instant(launch['capturedAt']) <= instant(after['capturedAt']),
                'The complete preservation records do not bracket this launch.')
        require(len(previous['roots']) == previous['root_count'] == 181, 'The pinned diagnostic04 baseline lacks its complete root set.')
        added = {str(W / name) for name in ('reference-nextup-global-matrix-diagnostic-04-execution',
            'reference-nextup-global-matrix-diagnostic-04-output', 'nextup-global-matrix-assembly-tool-03',
            'nextup-global-matrix-assembly-inputs-07', 'reference-nextup-global-matrix-attestation-07')}
        require(set(before['roots']) == set(previous['roots']) | added and len(before['roots']) == before['root_count'] ==
                len(after['roots']) == after['root_count'] == 186 and len(attestation['sealedRoots']) == 163 and
                len(set(attestation['sealedRoots'])) == 163 and set(attestation['sealedRoots']).issubset(previous['roots']),
                'The exact old181 and new186 protected root inventories differ.')
        require(all(same(before['roots'][name], value) for name, value in previous['roots'].items()),
                'A root preserved by diagnostic04 has changed.')
        for key in ('matrixAttestation', 'matrixUnitFile', 'goby_counts', 'roots', 'services', 'main_files', 'serviceBaselineReconciliation'):
            require(same(before[key], after[key]), 'A complete before/after preservation field differs: ' + key)
        reconciliation = before['serviceBaselineReconciliation']
        require(reconciliation == {'path': str(SERVICE_RECONCILIATION), 'sha256': SERVICE_RECONCILIATION_SHA},
                'Only the explicitly recorded matrix07 prelaunch service baseline reconciliation is accepted.')
        baseline = self.descriptor(reconciliation, SERVICE_RECONCILIATION)
        require(baseline['kind'] == 'nextup-matrix07-prelaunch-preservation-failure' and
                baseline['status'] == 'service_baseline_drift_before_launch' and baseline['matrixStarted'] is False and
                baseline['businessHttpRequests'] == 0 and baseline['matrixEvidenceRootAbsent'] is True and
                baseline['gobyBeforeWritten'] is True and baseline['preservationBeforeWritten'] is False and
                baseline['mainFileHashDifferences'] == [] and baseline['previousPreservationSHA256'] == PREVIOUS_SHA and
                set(baseline['differences']) == {'goby-client-m3e.service', 'goby-foundation-test.service'} and
                instant(baseline['capturedAt']) <= instant(before['capturedAt']),
                'The explicitly pinned prelaunch failure does not authorize this service baseline.')
        expected_services = deepcopy(previous['services'])
        for name, changes in baseline['differences'].items():
            for key, values in changes.items():
                require(set(values) == {'expected', 'actual'} and expected_services[name][key] == values['expected'],
                        'A reconciled service field does not match its old preserved value.')
                expected_services[name][key] = values['actual']
        prep_unit = 'goby-nextup-global-reference-preparation-05.service'
        require(set(before['services']) == set(expected_services) | {prep_unit} and
                all(same(before['services'][name], value) for name, value in expected_services.items()) and
                before['services']['goby-client-m3e.service']['ActiveState'] == 'failed' and
                before['services']['goby-client-m3e.service']['MainPID'] == '0' and
                before['services']['goby-foundation-test.service']['ActiveState'] == 'active' and
                before['services']['goby-foundation-test.service']['SubState'] == 'running',
                'Services differ beyond the two explicitly preserved prelaunch changes.')
        require(all(before['services'][prep_unit][key] == value for key, value in {
            'Id': prep_unit, 'LoadState': 'loaded', 'ActiveState': 'active', 'SubState': 'exited', 'MainPID': '0',
            'Result': 'success', 'ExecMainStatus': '0', 'ControlGroup': '',
            'InvocationID': 'cf198945fa894c6c93d23cd53d1798bf'}.items()),
            'The consumed preparation service terminal state changed.')
        require(same(before['main_files'], previous['main_files']), 'Historical main files differ from diagnostic04.')
        require(before['matrixAttestation'] == {'path': str(A), 'sha256': self.attestation_sha} and
                before['matrixUnitFile']['sha256'] == intent['unitFile']['sha256'] and
                before['roots'][str(A.parent)]['attestation.json']['sha256'] == self.attestation_sha,
                'Preservation did not pin this attestation and actual unit file.')
        for record in (before, after):
            require(record['old181RootsEqual'] is True and record['historical_v7_goby_state_equal_except_capture_time'] is True and
                    record['reference_database_read'] is False and record['original_implementation_bytes_read'] is False and
                    record['businessHttpRequests'] == 0, 'A preservation capture contains an incompatible claim.')
        goby_before = self.descriptor(before['goby_snapshot'], E / 'goby-reconciled-before.json')
        goby_after = self.descriptor(after['goby_snapshot'], E / 'goby-reconciled-after.json')
        historical = self.doc(HISTORICAL_GOBY, HISTORICAL_GOBY_SHA)
        stable = []
        for value in (goby_before, goby_after, historical):
            value = deepcopy(value)
            instant(value['database']['metadata'].pop('captured_at'))
            stable.append(value)
        require(same(stable[0], stable[1]) and same(stable[1], stable[2]),
                'Complete Goby state differs beyond capture time.')
        counts = {key: len(goby_after['database']['tables'][key]) for key in ('sessions', 'devices', 'activity_entries')}
        require(counts == before['goby_counts'] == {'sessions': 83, 'devices': 70, 'activity_entries': 185},
                'The full Goby preservation counts differ.')
        # Reconstruct each inventory; comparing two producer assertions alone is
        # insufficient to establish complete protected-tree preservation.
        for name, expected_tree in after['roots'].items():
            root = Path(name)
            require(root.parent == W or root == Path('/opt/goby-fixtures/client-m3e'), 'A protected root escapes its approved scope.')
            require(root.is_dir() and not root.is_symlink() and root.resolve(strict=True) == root and
                    all(root != blocked and blocked not in root.parents and root not in blocked.parents for blocked in FORBIDDEN),
                    'A protected root overlaps reference implementation/database bytes.')
            require(all(root != output and root not in output.parents and output not in root.parents for output in (E, O, O.parent, R)),
                    'An immutable protected root overlaps matrix output.')
            paths = [root, *sorted(root.rglob('*'))]
            require(len(paths) <= 2000, 'A protected tree exceeds its admitted entry bound.')
            require({str(path.relative_to(root)) for path in paths} == set(expected_tree),
                    'A complete protected tree has missing or additional paths.')
            actual = {str(path.relative_to(root)): self.tree_info(path, expected_tree[str(path.relative_to(root))]) for path in paths}
            require(same(actual, expected_tree), 'A complete current protected tree differs from preservation.')
        require(same(self.tree_info(UNIT_FILE, before['matrixUnitFile']), before['matrixUnitFile']), 'The unit file changed after preservation.')
        for name, expected in after['main_files'].items():
            require(same(self.tree_info(Path(name), expected), expected), 'A preserved main file changed.')
        self.summary.update(rootCount=186, old181RootsEqual=True, all186RootsBeforeAfterEqual=True,
                            all186CurrentTreesExact=True, servicesAndMainFilesEqual=True,
                            completeGobyStateEqualExceptCaptureTime=True, historicalV7GobyEqualExceptCaptureTime=True,
                            gobyCounts=counts, preservationBefore=intent['preservationBefore'],
                            serviceBaselineReconciliation=reconciliation,
                            candidateServiceState=deepcopy(before['services']['goby-client-m3e.service']),
                            primaryServiceState=deepcopy(before['services']['goby-foundation-test.service']),
                            preservationAfter={'path': str(E / 'preservation-after.json'),
                                               'sha256': self.evidence[str(E / 'preservation-after.json')]['sha256']},
                            gobyBefore=before['goby_snapshot'], gobyAfter=after['goby_snapshot'])
        return before, after

    def run(self):
        source_path = Path(__file__).absolute()
        source_raw = self.read(source_path)
        self.summary['verifierSource'] = {'path': str(source_path), 'sha256': digest(source_raw)}
        attestation = self.doc(A, self.attestation_sha)
        require(attestation['schemaVersion'] == 1 and attestation['kind'] == 'nextup-global-reference-matrix-attestation' and
                attestation['scope'] == {'attestationRoot': str(A.parent), 'operatorEvidenceRoot': str(O), 'sourceRoot': str(W)} and
                attestation['runtime']['unitName'] == UNIT and attestation['runtime']['properties']['PrivateNetwork'] == 'no' and
                set(attestation['forbiddenOriginalRoots']) == {str(path) for path in FORBIDDEN}, 'The attestation names another execution scope.')
        require(attestation['sources']['operator'] == {'path': str(OPERATOR), 'sha256': OPERATOR_SHA},
                'The operator is not the frozen TOOL05 source published by the closeout.')
        self.read(OPERATOR, OPERATOR_SHA)
        require(attestation['sources']['transport']['sha256'] == TRANSPORT_SHA and attestation['sources']['matrix']['sha256'] == MATRIX_SHA,
                'The frozen transport or matrix source binding changed.')
        self.read(attestation['sources']['transport']['path'], TRANSPORT_SHA)
        matrix_source = self.read(attestation['sources']['matrix']['path'], MATRIX_SHA)
        limits = [node.value.value for node in ast.parse(matrix_source).body if isinstance(node, ast.Assign) and
                  any(isinstance(target, ast.Name) and target.id == 'MAX_REQUESTS' for target in node.targets) and
                  isinstance(node.value, ast.Constant) and type(node.value.value) is int]
        require(len(limits) == 1 and limits[0] > 0, 'The pinned matrix request bound cannot be read unambiguously.')
        execution = self.descriptor(attestation['execution'], A.parent / 'published-execution.json')
        require(execution['matrix']['binding']['evidenceRoot'] == str(R), 'The execution binds another matrix output root.')
        for role in ('application', 'endpoint'):
            process = execution['process'][role]
            require(type(process['exeDevice']) is int and type(process['exeInode']) is int,
                    'A bound executable object lacks strict device and inode metadata.')
            self.bound_executable_objects.add((process['exeDevice'], process['exeInode']))
        launch, intent, commit, terminal, stdout, runtime = self.bind_runtime(attestation)
        before, after = self.preservation(attestation, intent, launch)
        anchor = self.identity_chain(attestation, execution, runtime, terminal, stdout, intent, after, limits[0])
        self.fresh_identity(execution['process'], anchor)
        self.stage = 'final_evidence_stability'
        for row in list(self.evidence.values()):
            self.read(row['path'], row['sha256'])
        require(self.counters == {'networkAttempts': 0, 'forbiddenByteReadAttempts': 0, 'unexpectedProcessAttempts': 0,
                                  'outsideOutputWriteAttempts': 0, 'metadataOnlyExecutableOpens': 1},
                'Independent closure attempted an operation outside its read-only proof contract.')
        self.summary.update(attestation={'path': str(A), 'sha256': self.attestation_sha},
                            operatorSource=attestation['sources']['operator'],
                            commit={'path': str(O / 'private/commit.json'), 'sha256': self.evidence[str(O / 'private/commit.json')]['sha256']},
                            terminalPrivateSha256=commit['terminalPrivateSha256'], terminalExportSha256=commit['terminalExportSha256'],
                            launchResult={'path': str(E / 'launch-result.json'), 'sha256': self.evidence[str(E / 'launch-result.json')]['sha256']})
        return self.summary

    def publish(self, status, error=None):
        result = {'schemaVersion': 1, 'kind': 'nextup-global-matrix-independent-runtime-terminal',
                  'capturedAt': datetime.now(timezone.utc).isoformat(), 'status': status, 'stage': self.stage,
                  'failure': error, 'runtimeIdentityPreservationVerified': error is None,
                  'matrixWireReplayVerified': False, 'independentWireReplayStillRequired': True,
                  'runtimeMatrixAcceptanceClaim': False, 'clientAcceptanceClaim': False,
                  'referenceDatabaseRead': False, 'originalImplementationBytesRead': False, 'boundProcessExecutableBytesRead': False,
                  'canonicalValueIsRawObservation': False, 'resumeOrRetryAllowed': False,
                  'independentCounters': self.counters, 'evidence': list(self.evidence.values()), **self.summary}
        raw = (json.dumps(result, sort_keys=True, indent=2, allow_nan=False) + '\n').encode()
        fd = os.open(OUTPUT, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
        with os.fdopen(fd, 'wb') as stream:
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())
        directory = os.open(E, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
        print(canonical({'path': str(OUTPUT), 'sha256': digest(raw), 'status': status,
                         'runtimeIdentityPreservationVerified': error is None,
                         'independentWireReplayStillRequired': True, 'clientAcceptanceClaim': False}))


def main():
    require(sys.platform == 'linux' and os.geteuid() == 0 and os.environ.get('SSH_CONNECTION') and
            sys.flags.isolated and sys.flags.dont_write_bytecode, 'Use authorized root SSH with /usr/bin/python3 -I -B.')
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--attestation-sha256', required=True)
    arguments = parser.parse_args()
    require(checksum(arguments.attestation_sha256), 'An exact attestation SHA-256 is required.')
    require(E.is_dir() and not E.is_symlink() and E.resolve(strict=True) == E and
            E.stat().st_uid == E.stat().st_gid == 0 and not E.stat().st_mode & 0o022,
            'The owned execution directory is missing or unsafe.')
    require(not os.path.lexists(OUTPUT), 'The independent runtime terminal already exists; never overwrite or replay it.')
    os.umask(0o077)
    verifier = Verifier(arguments.attestation_sha256)
    sys.addaudithook(verifier.audit)
    try:
        verifier.run()
    except Exception as error:
        verifier.publish('failed', {'type': type(error).__name__,
                                    'message': str(error) if isinstance(error, VerificationError) else 'Required evidence could not be read or decoded.'})
        return 2
    verifier.publish('runtime_identity_preservation_independently_verified')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
