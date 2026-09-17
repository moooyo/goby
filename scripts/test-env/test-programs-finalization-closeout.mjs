#!/usr/bin/env node
/** Synthetic in-memory finalization contracts; never read the retained r02 artifacts. */
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import test from 'node:test';
import { PROGRAMS_FINALIZATION_ROOT, PROGRAMS_FINALIZATION_PINS, PROGRAMS_ROLLOVER_LOGS,
  readProgramsFinalization, validateProgramsFinalization } from './close-audited-candidate-client.mjs';

const ROOT = '/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14';
const ORIGINAL = ROOT + '/candidate-programs-transition-01';
const DISPATCH = ROOT + '/programs-transition-dispatch-20260917-hosting-namespace-r02';
const PREPARATION = ROOT + '/programs-finalization-preparation';
const BINARY = '/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b/install/goby';
const UNIT = 'goby-audited-20260913T073217Z-ef77f9ffcf0b-server.service';
const TOKEN = '3618ed602bb9a56c12a2c7b0c34cc443';
const OLD_ACTIVE = 'goby-' + TOKEN + '-b25e8ca719092ba041930eb7cfa5004a.jsonl';
const STAT_NS = '1789629340937812431';
const TABLES = 'activity_entries application_key_clients application_key_devices application_keys catalog_entities client_playback_references devices encoding_jobs extra_reserved_paths item_entities item_extra_resources item_images item_metadata_state item_subtitles item_theme_resources items libraries library_roots managed_settings play_sessions scan_jobs schema_migrations server_settings sessions task_definitions task_occurrences task_run_children task_run_requests task_runs task_triggers theme_owner_ids theme_reserved_paths user_item_data user_settings users'.split(' ');
const SEQUENCES = 'activity_entries_id_seq application_keys_id_seq catalog_entities_id_seq devices_id_seq theme_owner_ids_id_seq'.split(' ');
const FRESH_LABELS = ['cluster-identity', 'epoch-read-only', 'cluster-identity', 'transition-read-only',
  'cluster-identity', 'transition-read-only', 'cluster-identity', 'database-identity-goby_candidate_ef77f9ffcf0b',
  'cluster-identity', 'database-objects-goby_candidate_ef77f9ffcf0b', 'cluster-identity', 'database-identity-goby_recovery_ef77f9ffcf0b',
  'cluster-identity', 'database-objects-goby_recovery_ef77f9ffcf0b', 'cluster-identity', 'epoch-read-only'];
const sha = raw => createHash('sha256').update(raw).digest('hex');
const clone = value => structuredClone(value);
const json = value => Buffer.from(JSON.stringify(value).replaceAll('"__STAT_NS__"', STAT_NS));
const barePin = path => ({ path, sha256: sha(path) });
const command = label => ({ label, processGroupClosed: true, outcome: 'acknowledged', timedOut: false,
  descendantsRemained: false, sqlOutcome: 'acknowledged', exitCode: 0, stdinComplete: true, outputLimitExceeded: false });
const snapshot = capturedAt => ({ capturedAt, tables: Object.fromEntries(TABLES.map(name => [name, []])),
  sequences: Object.fromEntries(SEQUENCES.map(name => [name, { lastValue: '1', isCalled: true }])) });
const facts = (ino, bytes, digest) => ({ dev: 2049, ino, uid: 995, gid: 986, mode: 0o600,
  bytes, sha256: digest, mtimeNs: '__STAT_NS__', ctimeNs: '__STAT_NS__' });

function paddedLog(rows, size) {
  const text = rows.map(row => JSON.stringify(row)).join('\n');
  assert(Buffer.byteLength(text) < size, 'synthetic_log_exceeds_fixed_size');
  return Buffer.from(text + ' '.repeat(size - Buffer.byteLength(text) - 1) + '\n');
}

function fixture(change = () => {}) {
  const files = new Map(), values = {}, reads = [];
  const put = (role, path, value) => {
    change(role, value);
    const raw = Buffer.isBuffer(value) ? Buffer.from(value) : json(value);
    files.set(path, raw); values[role] = value;
    return { path, sha256: sha(raw) };
  };
  const fixed = (role, value) => {
    const pin = put(role, PROGRAMS_FINALIZATION_PINS[role].path, value);
    PROGRAMS_FINALIZATION_PINS[role].sha256 = pin.sha256;
    return pin;
  };
  const pin3 = pin => ({ ...pin, bytes: files.get(pin.path).length });
  const pins = {};
  for (const role of ['transitionHelper', 'transitionRuntime', 'oldMainSource', 'oldStoreSource'])
    pins[role] = fixed(role, Buffer.from('# Synthetic source for ' + role + '\n'));
  pins.oldSourceManifest = fixed('oldSourceManifest', {
    'cmd/goby/main.go': { sha256: pins.oldMainSource.sha256, bytes: files.get(pins.oldMainSource.path).length },
    'internal/diagnostics/store_linux.go': { sha256: pins.oldStoreSource.sha256, bytes: files.get(pins.oldStoreSource.path).length },
  });
  pins.producer = put('producer', PREPARATION + '/code/finalize-programs-transition.py', Buffer.from('# Synthetic finalizer\n'));
  pins.validator = put('validator', PREPARATION + '/code/audited-candidate-runtime.py', Buffer.from('# Synthetic validator\n'));
  pins.transitionInput = fixed('transitionInput', { kind: 'audited-candidate-transition-input', version: 3,
    operationKind: 'programs_successor', output: ORIGINAL });

  const shutdown = [{ time: '2026-09-17T07:15:40.810585954Z', level: 'INFO', msg: 'server shutdown completed',
    event: 'server.shutdown.completed', duration_ms: 2 }];
  const startup = [
    { time: '2026-09-17T07:15:41.000000001Z', level: 'INFO', event: 'server.starting' },
    { time: '2026-09-17T07:15:41.100000001Z', level: 'INFO', event: 'server.listening' },
    ...['/readyz', '/healthz', '/emby/System/Info/Public'].map((route, index) => ({
      time: '2026-09-17T07:15:42.' + String(index + 1).padStart(9, '0') + 'Z', level: 'INFO',
      event: 'request.completed', route: 'GET ' + route, method: 'GET', status: 200 })),
  ];
  for (const [role, rows] of [['shutdownLog', shutdown], ['startupLog', startup]]) {
    change(role + 'Events', rows);
    const expected = PROGRAMS_ROLLOVER_LOGS[role];
    pins[role] = pin3(put(role, ROOT + '/programs-rollover-review/private/' + expected.name, paddedLog(rows, expected.bytes)));
    expected.sha256 = pins[role].sha256;
  }
  const oldNames = [OLD_ACTIVE, ...Array.from({ length: 8 }, (_, index) => 'goby-' + TOKEN + '-' + String(index + 1).padStart(32, '0') + '.jsonl')];
  const oldFiles = Object.fromEntries(oldNames.map((name, index) => [name, facts(100 + index, 32, sha(name))]));
  const diagnostics = { registry: { version: 1, token: TOKEN, files: oldNames.map((name, index) => ({ name,
    created: '2026-09-16T10:43:39.749981724Z', identity: { device: 2049, inode: 100 + index }, closed: index !== 0, size: index ? 32 : 0 })) },
    directory: { dev: 2049, ino: 50 }, lock: { dev: 2049, ino: 51 }, registryFile: facts(52, 100, sha('registry-before')), files: oldFiles };
  const afterDiagnostics = clone(diagnostics);
  for (const row of afterDiagnostics.registry.files) { row.closed = true; row.size = 32; }
  for (const [role, expected] of Object.entries(PROGRAMS_ROLLOVER_LOGS)) {
    afterDiagnostics.registry.files.push({ name: expected.name, created: role === 'shutdownLog' ?
      '2026-09-17T07:15:40.810655594Z' : '2026-09-17T07:15:41.009585878Z',
    identity: { device: 2049, inode: expected.ino }, closed: role === 'shutdownLog', size: role === 'shutdownLog' ? expected.bytes : 0 });
    afterDiagnostics.files[expected.name] = facts(expected.ino, expected.bytes, expected.sha256);
  }
  const process = { pid: 1907978, startTicks: '34901535', exeInode: 1580898 };
  const binary = { ...facts(1580898, 30701500, sha('synthetic-binary')), uid: 0, gid: 0, mode: 0o755 };
  const state = (capturedAt, logs) => ({ capturedAt, databaseNow: capturedAt, source: snapshot(capturedAt),
    inactiveStage: snapshot(capturedAt), diagnostics: logs, fixedFiles: { [BINARY]: clone(binary) },
    candidateBefore: clone(process), candidateAfter: clone(process), leaseBefore: { granted: true }, leaseAfter: { granted: true },
    unitLogs: { 'server-unit.log': facts(61, 32, sha('server-log')), 'postgres-unit.log': facts(62, 32, sha('postgres-log')) },
    databases: {}, trees: {}, controlDocuments: {}, loadedUnits: {}, protected: {}, postgresBefore: {}, postgresAfter: {},
    postgresProcess: {}, hostingBefore: {}, hostingAfter: {}, previousEpoch: {}, seedBinding: {}, priorSource: {}, currentRuntime: {} });
  const before = state('2026-09-17T07:15:39.000000000Z', diagnostics);
  before.candidateBefore.pid = before.candidateAfter.pid = 1648477;
  const after = state('2026-09-17T07:15:43.000000000Z', afterDiagnostics);
  pins.before = fixed('before', before); pins.after = fixed('after', after);
  pins.sourceBefore = put('sourceBefore', ORIGINAL + '/private/source-before.json', clone(before.source));
  pins.sourceAfter = put('sourceAfter', ORIGINAL + '/private/source-after.json', clone(after.source));
  const oldBinaryCopy = { path: ORIGINAL + '/private/goby-before.bin', bytes: 29561402,
    sha256: 'b0d6769cadc525b12d2970a206d8e141a39431ee72bb4f7be77bbeecf873ea42' };
  const failure = { kind: 'audited-programs-transition-failure', version: 1, status: 'transition_failed_resources_retained',
    stage: 'after_preservation', code: 'diagnostic_file_membership_changed', output: ORIGINAL, input: pins.transitionInput,
    source: pins.transitionHelper, runtimeHelper: pins.transitionRuntime, before: pins.before,
    calls: { stop: 1, replace: 1, start: 1 }, automaticRetry: false, automaticRollback: false, candidateAdmissionComplete: false,
    oldBinaryCopy: { path: oldBinaryCopy.path, sha256: oldBinaryCopy.sha256 },
    commandResponsibilities: [{ ...command('candidate-stop'), sqlOutcome: null },
      ...Array.from({ length: 38 }, (_, index) => command('original-sql-' + index)),
      { ...command('server-start'), sqlOutcome: null }],
    failureCapture: { status: 'existing_after_state_retained', state: pins.after,
      serverIdentity: clone(process), serverProperties: { Id: UNIT, ActiveState: 'active' } } };
  pins.failure = fixed('failure', failure);
  const publicEvidence = ['/readyz', '/healthz', '/emby/System/Info/Public'].map((route, index) => {
    const prefix = ORIGINAL + '/private/public-' + String(index + 1).padStart(2, '0');
    return { intent: put('publicIntent' + index, prefix + '-intent.json', { method: 'GET', route, process: clone(process) }),
      response: put('publicResponse' + index, prefix + '-response.json', { status: 200, headers: [], bodySha256: sha(route), complete: true }) };
  });
  const installed = { path: BINARY, sha256: after.fixedFiles[BINARY].sha256 };
  const actionPins = Object.entries({
    'stop-intent': { unit: UNIT, process: clone(before.candidateAfter) },
    stopped: { unit: { Id: UNIT, ActiveState: 'inactive', MainPID: '0', Result: 'success', ExecMainStatus: '0' },
      oldProcess: clone(before.candidateAfter), leaseCount: 0 },
    'replace-intent': { destination: BINARY,
      staged: BINARY.slice(0, BINARY.lastIndexOf('/')) + '/.goby-next-candidate-programs-transition-01', oldCopy: clone(failure.oldBinaryCopy) },
    'replace-result': clone(installed),
    'start-intent': { unit: UNIT, binary: clone(installed) },
  }).map(([name, value]) => put(name, ORIGINAL + '/private/' + name + '.json', value));
  pins.currentMetadata = fixed('currentMetadata', { kind: 'synthetic-current-metadata' });
  pins.readerClosure = fixed('readerClosure', { kind: 'programs-transition-independent-metadata-reader-closure', version: 1,
    status: 'independent_metadata_reader_closed', scope: DISPATCH, metadata: pin3(pins.currentMetadata),
    metadataCommandsPidsGroupsGone: true, allNineMetadataCommandsWaitedAndPipesClosed: true,
    allReadDescriptorsClosedBeforePublication: true, publicationDescriptorClosedInTerminal: true, cgroup: { absent: true },
    readDescriptorSummary: { opened: 3, closed: 3, errors: 0 }, originalCliFailurePreserved: true,
    originalCloserEvidenceAccepted: false, currentLeaseGrantRequeried: false,
    pidObservations: [{ pid: 1908000, absent: true }], currentCandidateIntentionallyRetained: [1907978, 1907986] });
  const outputPins = [pins.before, pins.after, pins.failure, pins.sourceBefore, pins.sourceAfter,
    ...publicEvidence.flatMap(row => [row.intent, row.response]), ...actionPins].map(pin3);
  const cli = { execMainCode: '1', execMainStatus: '2', normalExitCode: 2, result: 'exit-code' };
  const primary = { code: 'transition_cli_failed', stage: 'operation', type: 'Fault' };
  const sessionErrors = [{ code: 'saved_resource_closure_incomplete', label: 'resource_receipt_quality' }];
  const outerPin = put('outer-closure', DISPATCH + '/private/outer-closure.json', {
    kind: 'programs-transition-outer-closure', scope: DISPATCH, cliExit: clone(cli), primaryFailure: clone(primary),
    cleanupErrors: [clone(primary)], resourcesClosed: true, sourceInputUnchanged: true, protectedStateUnchanged: true,
    outputInventory: { files: outputPins } });
  const lock = { kind: 'programs-transition-lock-closure', acquired: true, released: true, fdClosed: true,
    metadataPreserved: true, resourcesClosedBeforeRelease: true, unlockAttempted: true, unlockSucceeded: true,
    errors: [], outerClosure: pin3(outerPin) };
  const lockPin = put('lock-closure', DISPATCH + '/private/lock-closure.json', lock);
  pins.callerResult = fixed('callerResult', { kind: 'programs-transition-dispatch-result', scope: DISPATCH,
    status: 'failed_retained_for_review', callerExitCode: 2, launchAttempts: 1, invocations: 1, innerResult: null,
    cliExit: clone(cli), primaryFailure: clone(primary), cleanupErrors: [clone(primary)],
    resourcesClosed: true, sourceInputUnchanged: true, protectedStateUnchanged: true,
    automaticRetry: false, automaticRecovery: false, automaticRollback: false, candidateAdmissionComplete: false,
    outerClosure: pin3(outerPin), lockClosure: pin3(lockPin) });
  const sessionPin = put('session-closure', DISPATCH + '/private/session-closure.json', { resourcesObservedClosed: true,
    evidenceAccepted: false, status: 'incomplete_session_closure_retained_for_review', errors: clone(sessionErrors) });
  const sshPin = put('original-ssh-exit', DISPATCH + '/private/original-ssh-exit.json', {
    kind: 'programs-transition-original-ssh-exit', scope: DISPATCH, waitedToCompletion: true, remoteCallerExitCode: 2, sshExitCode: 2,
    originalToolEvidence: { nativeSshWaitedToCompletion: true, nativeSshExitCode: 2, supervisorTimedOut: false, issues: [] } });
  const otherCritical = [outerPin, lockPin, sessionPin, sshPin,
    ...['source-manifest', 'original-powershell-terminal', 'original-ssh-intent'].map(name => put(name, DISPATCH + '/private/' + name + '.json', {}))];
  const closureReview = { kind: 'programs-transition-independent-failure-review', version: 2,
    status: 'failed_transition_reviewed_owned_closed_current_candidate_retained', scope: DISPATCH, issues: [],
    acceptanceBoundary: 'Synthetic saved-failure fixture; no actual r02 artifact is read or admitted.',
    observedAt: '2026-09-17T07:50:26.486127000Z',
    admissionArtifactsAbsentAtReadback: Object.fromEntries(['preservation.json', 'result.json', 'runtime-epoch.json', 'seed-runtime-binding.json']
      .map(name => [ORIGINAL + '/private/' + name, true])),
    originalOutputReadbackSummary: { allHashesMatched: true, records: outputPins.length },
    originalStreamReadbackSummary: { allHashesMatched: true, records: 0 },
    sourceInputReadbackSummary: { allHashesMatched: true, records: 0 },
    preservation: { newDiagnosticBodyEvidenceBoundary: { newBodiesIndependentlyRead: false } },
    priorIndependentReviewPreserved: { status: 'failure_review_incomplete' },
    actionsByThisReview: Object.fromEntries(['closerReruns', 'http', 'lockActions', 'recovery', 'serviceActions', 'sql', 'tests', 'v4BindingsCreated'].map(key => [key, 0])),
    savedOnlyCorrection: Object.fromEntries(['newCurrentMetadataCapture', 'newHttp', 'newRecovery', 'newServices', 'newSql', 'newTests', 'originalIndependentReviewRewritten', 'originalReceiptsRewritten'].map(key => [key, 0])),
    originalFailureRetained: { failureStage: 'after_preservation', failureCode: 'diagnostic_file_membership_changed',
      dispatchStatus: 'failed_retained_for_review', remoteCallerExitCode: 2, nativeSshExitCode: 2, originalPowerShellExitCode: 1,
      cliExit: clone(cli), originalCloserEvidenceAccepted: false, originalCloserResourcesObservedClosed: true,
      originalCloserStatus: 'incomplete_session_closure_retained_for_review', originalCloserErrors: clone(sessionErrors),
      candidateAdmissionComplete: false },
    actualCompletedActions: { calls: clone(failure.calls), newCandidate: clone(process),
      newCandidatePropertiesAtFailure: clone(failure.failureCapture.serverProperties), oldBinaryCopy, savedAfterLease: clone(after.leaseAfter),
      originalCandidatePid: 1648477, newBinary: clone(binary), publicRequests: ['/readyz', '/healthz', '/emby/System/Info/Public'].map((route, index) => ({
        index: index + 1, method: 'GET', route, status: 200, complete: true, bodySha256: sha(route) })) },
    ownedClosure: { allCommandsAcknowledgedAndClosed: true, cliCommandResponsibilities: 40, currentOriginalOwnedPidCount: 78,
      currentOriginalOwnedPidAndGroupMatches: [], currentOwnedCgroups: [1, 2, 3].map(index => ({ path: '/synthetic/' + index, absent: true })),
      outerMetadataDescriptors: { opened: 37294, closed: 37294, errors: 0 },
      closerMetadataDescriptors: { opened: 267, closed: 267, errors: 0 }, originalLockReceipt: clone(lock) },
    currentMetadataConclusion: { currentLeaseGrantRequeried: false }, currentMetadata: pin3(pins.currentMetadata),
    independentReaderClosure: pin3(pins.readerClosure),
    criticalPins: [...otherCritical, pins.callerResult, pins.before, pins.after, pins.failure, pins.currentMetadata, pins.readerClosure].map(pin3) };
  pins.closureReview = fixed('closureReview', closureReview);
  pins.rolloverReview = fixed('rolloverReview', {
    kind: 'audited-programs-shutdown-rollover-review', version: 1, status: 'exact_saved_rollover_reviewed',
    ...Object.fromEntries(['transitionInput', 'failure', 'before', 'after', 'oldSourceManifest', 'oldMainSource', 'oldStoreSource'].map(key => [key, pins[key]])),
    shutdownLog: pins.shutdownLog, startupLog: pins.startupLog });
  const fresh = clone(after);
  fresh.capturedAt = fresh.databaseNow = fresh.source.capturedAt = '2026-09-17T07:16:00.000000000Z';
  pins.freshState = put('freshState', PROGRAMS_FINALIZATION_ROOT + '/private/fresh.json', fresh);
  pins.freshSource = put('freshSource', PROGRAMS_FINALIZATION_ROOT + '/private/source-fresh.json', clone(fresh.source));
  const input = { kind: 'audited-programs-transition-finalization-input', version: 1, output: PROGRAMS_FINALIZATION_ROOT,
    ...Object.fromEntries(['transitionInput', 'transitionHelper', 'transitionRuntime', 'failure', 'before', 'after',
      'sourceBefore', 'sourceAfter', 'rolloverReview', 'closureReview'].map(key => [key, pins[key]])),
    budgets: { maximumSeconds: 300, maximumSqlCommands: 16, maximumPublicRequests: 0, stopCalls: 0, replaceCalls: 0, startCalls: 0 } };
  pins.input = put('input', PREPARATION + '/private/input.json', input);
  const record = { kind: 'audited-programs-transition-finalization', version: 1, status: 'fresh_checked_awaiting_live_acceptance',
    publicationRoot: PROGRAMS_FINALIZATION_ROOT,
    ...Object.fromEntries(['input', 'producer', 'validator', 'transitionInput', 'transitionHelper', 'transitionRuntime', 'failure',
      'before', 'after', 'sourceBefore', 'sourceAfter', 'rolloverReview', 'closureReview', 'freshState', 'freshSource'].map(key => [key, pins[key]])),
    publicEvidence, calls: { stop: 0, replace: 0, start: 0 }, originalCalls: { stop: 1, replace: 1, start: 1 },
    publicRequests: 0, originalPublicRequests: 3, commandResponsibilities: FRESH_LABELS.map(command) };
  pins.finalization = put('record', PROGRAMS_FINALIZATION_ROOT + '/private/antecedent.json', record);
  const epoch = { ...Object.fromEntries(['transitionInput', 'transitionHelper', 'before', 'after', 'sourceBefore', 'sourceAfter', 'finalization'].map(key => [key, pins[key]])),
    runtimeHelper: pins.validator, preservation: barePin(PROGRAMS_FINALIZATION_ROOT + '/private/preservation.json'),
    candidateProcess: clone(after.candidateAfter), lease: clone(after.leaseAfter), calls: { stop: 1, replace: 1, start: 1 } };
  return { epoch, pins, values, files, reads, read: async (pin, maximum = 4 * 1024 * 1024) => {
    reads.push(pin.path);
    assert(files.has(pin.path), 'synthetic_pin_missing');
    const raw = files.get(pin.path);
    assert(raw.length <= maximum, 'synthetic_read_bound');
    return Buffer.from(raw);
  } };
}

async function withFixture(change, run) {
  // Temporary synthetic digests retain every fixed production path and log identity.
  const originalPins = clone(PROGRAMS_FINALIZATION_PINS), originalLogs = clone(PROGRAMS_ROLLOVER_LOGS);
  try { return await run(fixture(change)); }
  finally {
    for (const [key, pin] of Object.entries(originalPins)) PROGRAMS_FINALIZATION_PINS[key].sha256 = pin.sha256;
    for (const [key, row] of Object.entries(originalLogs)) PROGRAMS_ROLLOVER_LOGS[key].sha256 = row.sha256;
  }
}

test('complete synthetic reader verifies every source and evidence pin', { concurrency: false }, async () => {
  await withFixture(undefined, async value => {
    const bundle = await readProgramsFinalization(value.epoch, value.read);
    assert.equal(value.files.size, 42);
    assert.deepEqual(new Set(value.reads), new Set(value.files.keys()));
    assert.equal(bundle.evidence.closureReview.actualCompletedActions.newBinary.ctimeNs, STAT_NS);
    assert.doesNotThrow(() => validateProgramsFinalization(value.epoch, bundle, bundle.evidence.before, bundle.evidence.after));
  });
});

test('missing or tampered source bytes cannot gain finalization authority', { concurrency: false }, async () => {
  for (const role of ['producer', 'validator', 'transitionHelper', 'transitionRuntime', 'oldMainSource', 'oldStoreSource']) {
    for (const missing of [true, false]) await withFixture(undefined, async value => {
      const path = value.pins[role].path;
      if (missing) value.files.delete(path);
      else value.files.set(path, Buffer.concat([value.files.get(path), Buffer.from('# changed\n')]));
      await assert.rejects(() => readProgramsFinalization(value.epoch, value.read),
        missing ? /synthetic_pin_missing/ : /programs_finalization_readback_pin/);
    });
  }
  await withFixture(undefined, async value => {
    value.files.delete(ORIGINAL + '/private/stop-intent.json');
    await assert.rejects(() => readProgramsFinalization(value.epoch, value.read), /synthetic_pin_missing/);
  });
});

test('calls failure snapshots rollover fresh state and cycles remain strict', { concurrency: false }, async () => {
  const cases = [
    ['record', row => { row.calls.start = 1; }, /programs_finalization_source_or_calls/],
    ['failure', row => { row.code = 'synthetic_other_failure'; }, /programs_finalization_failed_attempt_binding/],
    ['sourceAfter', row => { row.tables.users.push({ id: 'unexpected-user' }); }, /programs_finalization_original_snapshots/],
    ['before', row => { row.diagnostics.registry.files[0].created = '2026-09-17T00:00:00.000000000Z'; }, /programs_finalization_utc_boundary/],
    ['after', row => { row.diagnostics.registry.files.at(-2).closed = false; }, /programs_finalization_new_log_identity/],
    ['startupLogEvents', rows => { rows[2].route = 'GET /unreviewed'; }, /programs_finalization_startup_events/],
    ['freshState', row => { row.candidateAfter.pid++; }, /programs_finalization_fresh_identity/],
    ['callerResult', row => { row.callerExitCode = 0; }, /programs_finalization_original_caller_failure/],
    ['session-closure', row => { row.evidenceAccepted = true; }, /programs_finalization_original_exit_boundary/],
    ['stopped', row => { row.leaseCount = 1; }, /programs_finalization_original_stop_evidence/],
    ['record', row => { row.commandResponsibilities.push(command('extra-sql')); }, /programs_finalization_command_closure/],
    ['record', row => { row.commandResponsibilities[0].label = 'unknown-sql'; }, /programs_finalization_read_only_commands/],
    ['record', row => { row.commandResponsibilities[0].stdinComplete = false; }, /programs_finalization_read_only_commands/],
    ['outer-closure', row => { row.outputInventory.files.find(pin => pin.path.endsWith('/public-01-response.json')).bytes++; },
      /programs_finalization_original_output_bytes/],
    ['record', row => { row.input = barePin(PROGRAMS_FINALIZATION_ROOT + '/private/antecedent.json'); }, /programs_finalization_source_or_calls/],
  ];
  for (const [role, mutate, expected] of cases) await withFixture((name, value) => { if (name === role) mutate(value); }, async value => {
    await assert.rejects(() => readProgramsFinalization(value.epoch, value.read), expected, role);
  });
});

test('copied authorization and edited evidence cannot pass the pure validator', { concurrency: false }, async () => {
  await withFixture(undefined, async value => {
    const bundle = await readProgramsFinalization(value.epoch, value.read);
    const copy = { ...bundle, diagnosticAuthorization: { ...bundle.diagnosticAuthorization } };
    assert.throws(() => validateProgramsFinalization(value.epoch, copy, bundle.evidence.before, bundle.evidence.after),
      /programs_finalization_checked_authority_required/);
    bundle.evidence.failure.code = 'edited_after_read';
    assert.throws(() => validateProgramsFinalization(value.epoch, bundle, bundle.evidence.before, bundle.evidence.after),
      /programs_finalization_evidence_changed/);
  });
});

test('conflicting cached pins and future publication references are rejected', { concurrency: false }, async () => {
  await withFixture((role, row) => {
    if (role === 'closureReview') row.criticalPins.find(pin => pin.path === PROGRAMS_FINALIZATION_PINS.before.path).sha256 = sha('conflicting-cached-before');
  }, async value => {
    await assert.rejects(() => readProgramsFinalization(value.epoch, value.read), /programs_finalization_pin_alias_changed/);
  });
  for (const name of ['runtime-epoch.json', 'seed-runtime-binding.json', 'preservation.json']) {
    await withFixture((role, row) => {
      if (role === 'record') row.input = barePin(PROGRAMS_FINALIZATION_ROOT + '/private/' + name);
    }, async value => {
      await assert.rejects(() => readProgramsFinalization(value.epoch, value.read), /programs_finalization_new_source_scope/);
      assert(!value.reads.includes(PROGRAMS_FINALIZATION_ROOT + '/private/' + name));
    });
  }
});

test('legacy epochs without finalization perform no additional reads', { concurrency: false }, async () => {
  let reads = 0;
  const read = async () => { reads++; throw new Error('legacy_read_forbidden'); };
  for (const version of [1, 2, 3, 4]) {
    const epoch = { version };
    assert.equal(await readProgramsFinalization(epoch, read), null);
    assert.equal(validateProgramsFinalization(epoch, null, {}, {}), null);
  }
  assert.equal(reads, 0);
  await withFixture(undefined, async value => {
    const stripped = { ...value.epoch };
    delete stripped.finalization;
    await assert.rejects(() => readProgramsFinalization(stripped, value.read), /programs_finalization_missing_authority/);
    assert.throws(() => validateProgramsFinalization(stripped, null, {}, {}), /programs_finalization_missing_authority/);
    assert.deepEqual(value.reads, []);
  });
});
