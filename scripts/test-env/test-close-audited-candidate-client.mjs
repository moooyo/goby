#!/usr/bin/env node
/** Pure closeout guards over synthetic receipts; never perform client work. */
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { gzipSync, brotliCompressSync } from 'node:zlib';
import { fileURLToPath, pathToFileURL } from 'node:url';

const ORIGIN = 'http://127.0.0.1:19180';
const ACTOR = 'a'.repeat(32), CONTROL = 'b'.repeat(32), ITEM = 'c'.repeat(32);
const TOKEN = 'synthetic-owned-token-with-no-live-authority';
const sha = bytes => createHash('sha256').update(bytes).digest('hex');
const jsonBytes = value => Buffer.from(JSON.stringify(value));
const clone = value => structuredClone(value);
const manifest = { browserOrigin: ORIGIN, actor: { id: ACTOR, username: 'synthetic-actor' }, scenario: 'mp3',
  catalog: { mp3: { id: ITEM, type: 'Audio', container: 'mp3', runtimeTicks: 6000000000 } } };
const TABLES = 'activity_entries application_key_clients application_key_devices application_keys catalog_entities client_playback_references devices encoding_jobs extra_reserved_paths item_entities item_extra_resources item_images item_metadata_state item_subtitles item_theme_resources items libraries library_roots managed_settings play_sessions scan_jobs schema_migrations server_settings sessions task_definitions task_occurrences task_run_children task_run_requests task_runs task_triggers theme_owner_ids theme_reserved_paths user_item_data user_settings users'.split(' ');

function responseHead(status, headers) {
  return Buffer.from(['HTTP/1.1 ' + status, ...headers.map(([key, value]) => key + ': ' + value), '', ''].join('\r\n'), 'latin1');
}

function chunked(bytes) {
  const middle = Math.max(1, Math.floor(bytes.length / 2));
  return Buffer.concat([bytes.subarray(0, middle), bytes.subarray(middle)].filter(piece => piece.length).flatMap((piece, index) => [
    Buffer.from(piece.length.toString(16) + (index === 0 ? ';synthetic=yes' : '') + '\r\n'), piece, Buffer.from('\r\n'),
  ]).concat([Buffer.from('0\r\nX-Synthetic-Trailer: retained\r\n\r\n')]));
}

function exchangeFixture({ media = false, method = 'GET', delivered = media ? 128 : 12, target: suppliedTarget,
  contentRange = 'bytes 0-999/4096' } = {}) {
  const target = suppliedTarget ?? (media ? '/emby/Audio/' + ITEM + '/stream.mp3?PlaySessionId=play_synthetic' : '/emby/System/Info');
  const rawHead = Buffer.from([method + ' ' + target + ' HTTP/1.1', 'Host: 127.0.0.1:19180',
    'X-Emby-Token: ' + TOKEN, ...(media ? ['Range: bytes=0-999'] : ['User-Agent: GobyBrowserPostLogoutVerification/1']),
    'Connection: close', '', ''].join('\r\n'));
  const head = responseHead(media ? '206 Partial Content' : '401 Unauthorized', [
    ['Content-Length', media ? '1000' : '12'], ['Content-Type', media ? 'audio/mpeg' : 'text/plain'],
    ...(media ? [['Content-Range', contentRange]] : []), ['Connection', 'close'],
  ]);
  const request = { kind: media ? 'media' : 'api', method, origin: ORIGIN, target, path: target.split('?')[0],
    parentOrdinal: null, budgetClass: media ? 'normal' : 'cleanup', rawRequestHeadBase64: rawHead.toString('base64'),
    forwardedRequestHeadBase64: rawHead.toString('base64') };
  const intent = { ordinal: 7, budgetClass: request.budgetClass, request, startedMonotonicNs: '10000000000' };
  const result = { ordinal: 7, budgetClass: request.budgetClass, request: clone(request), backend: 'goby',
    outcome: media ? 'rejected_or_interrupted' : 'observed', upstreamConnected: true,
    responseStatus: media ? 206 : 401, responseHeadBase64: head.toString('base64'),
    responseHeaders: head.subarray(0, -4).toString('latin1').split('\r\n').slice(1).map(line => {
      const offset = line.indexOf(':'); return [line.slice(0, offset), line.slice(offset + 1).trim()];
    }), interimHeadsBase64: [], completeHTTP: !media, requestBodyComplete: true,
    requestBodyWireBytes: 0, responseBodyWireBytes: delivered, upstreamBytesWritten: rawHead.length,
    clientBytesWritten: head.length + delivered, webSocketClientBytes: 0, webSocketUpstreamBytes: 0,
    requestForwardedComplete: !media, responseForwardedComplete: !media,
    requestBodyRetained: false, responseBodyRetained: false, requestBodyBase64: null, responseBodyBase64: null,
    requestBodyTruncated: false, responseBodyTruncated: false, bodyEvidenceComplete: false,
    bodyStorage: 'http-transfer-wire', webMediaAndWebSocketBodiesRetained: false, completedMonotonicNs: '12000000000' };
  return { intent, result, rawHead, head };
}

function partialFixture(closer) {
  const fixture = exchangeFixture({ media: true });
  const exchange = closer.verifyExchange(fixture.intent, fixture.result, manifest);
  const row = { method: 'GET', url: exchange.url.href, token_sha256: exchange.tokenHash, elapsed_ms: 1000,
    failed_elapsed_ms: 3000, failed: true, failure_error_text: 'net::ERR_ABORTED', headers: { range: 'bytes=0-999' }, payload_base64: '' };
  const report = { started_monotonic_ns: '9000000000', requests: [
    { ...clone(row), scope: 'frame', main_frame: true },
    { ...clone(row), scope: 'service_worker', elapsed_ms: 1001, failed_elapsed_ms: 3001 },
  ] };
  const login = { chains: [{ stopped: [{ exchange: { intent: { startedMonotonicNs: '12000000000' } } }] }], media: [] };
  return { fixture, exchange, report, login };
}

function durableFixture() {
  const time = second => '2026-01-01T00:00:' + String(second).padStart(2, '0') + '.000000000Z';
  const before = { capturedAt: time(0), tables: Object.fromEntries(TABLES.map(name => [name, []])), sequences: {
    devices_id_seq: { lastValue: '1', isCalled: false }, activity_entries_id_seq: { lastValue: '1', isCalled: false },
    theme_owner_ids_id_seq: { lastValue: '7', isCalled: true },
  } };
  before.tables.users = [{ id: ACTOR, name: 'synthetic-actor' }, { id: CONTROL, name: 'synthetic-control' }];
  before.tables.user_item_data = [{ user_id: CONTROL, item_id: ITEM, playback_position_ticks: 77, play_count: 4,
    is_favorite: true, played: false, last_played_at: '2025-12-31T00:00:00Z', updated_at: '2025-12-31T00:00:00Z' }];
  const after = clone(before); after.capturedAt = time(30);
  const login = { sessionId: 'session_synthetic', tokenHash: sha(TOKEN), device_id: 'synthetic-device',
    client: 'Synthetic Web Client', client_version: '1.0', infos: [] };
  after.tables.sessions.push({ id: login.sessionId, user_id: ACTOR, kind: 'emby', token_hash: '\\x' + login.tokenHash,
    device_id: login.device_id, client_name: login.client, client_version: login.client_version,
    revoked_at: time(25), created_at: time(1), last_seen_at: time(24), expires_at: '2026-01-01T01:00:00Z', device_registry_id: 1 });
  after.tables.devices.push({ id: 1, deleted_at: null, last_user_id: ACTOR, reported_device_id: login.device_id,
    last_seen_at: time(25), custom_name: null, revision: 1, created_at: time(1) });
  after.tables.activity_entries = ['session.login', 'session.revoked'].map((action, index) => ({ id: index + 1,
    actor_credential_id: login.sessionId, action, source: 'emby', severity: 'Info', actor_kind: 'user', actor_id: ACTOR,
    resource_kind: 'session', resource_id: login.sessionId, affected_count: 1, request_id: '', observation_fingerprint: '',
    state: '', revision: 0, previous_revision: 0, changed_fields: [], created_at: time(index === 0 ? 1 : 25) }));
  const play = { id: 'play_synthetic', user_id: ACTOR, auth_session_id: login.sessionId, device_id: login.device_id,
    application_client_id: null, item_id: ITEM, media_source_id: 'mediasource_' + ITEM,
    position_ticks: 120000000, duration_ticks: 6000000000, created_at: time(2), updated_at: time(20),
    started_at: time(3), stopped_at: time(20), state: 'Stopped', counted: true, client_correlated: false };
  after.tables.play_sessions.push(play);
  after.tables.user_item_data.push({ user_id: ACTOR, item_id: ITEM, playback_position_ticks: play.position_ticks,
    play_count: 1, is_favorite: false, played: false, last_played_at: time(3), updated_at: time(20) });
  after.sequences.devices_id_seq = { lastValue: '1', isCalled: true };
  after.sequences.activity_entries_id_seq = { lastValue: '2', isCalled: true };
  const started = { event: 'started', body: { PositionTicks: 0 }, exchange: { ordinal: 2,
    intent: { startedAt: time(2) }, result: { completedAt: time(4) } } };
  const stopped = { event: 'stopped', body: { PositionTicks: play.position_ticks }, exchange: { ordinal: 9,
    intent: { startedAt: time(19) }, result: { completedAt: time(21) } } };
  return { before, after, seed: { controlQ: { id: CONTROL } }, physical: { logins: [login],
    chains: [{ play, stopped: [stopped], reports: [started, stopped] }], exchanges: [] } };
}

function retainedFixture(closer) {
  const value = durableFixture(), at = second => '2026-01-01T00:00:' + String(second).padStart(2, '0') + '.000000000Z';
  const movie = { id: ITEM, type: 'Movie', runtimeTicks: 6000000000 };
  value.manifest = { ...clone(manifest), scenario: 'movie', catalog: { movie } };
  value.seed = { controlQ: { id: CONTROL }, actors: { movie: { id: ACTOR, username: 'synthetic-actor' } }, catalog: { movie } };
  for (const state of [value.before, value.after]) Object.assign(state.tables.users[0], { is_administrator: false, is_disabled: false, policy: {} });
  const old = { id: closer.RETAINED_PLAY, user_id: ACTOR, auth_session_id: closer.RETAINED_AUTH, device_id: 'old-device', application_client_id: null,
    item_id: ITEM, media_source_id: 'mediasource_' + ITEM, duration_ticks: movie.runtimeTicks, state: 'Prepared', counted: false,
    position_ticks: 0, started_at: null, stopped_at: null, player_state: {}, client_correlated: false,
    created_at: '2025-12-31T23:00:00Z', updated_at: '2025-12-31T23:00:00Z', expires_at: '2025-12-31T23:30:00Z' };
  const oldAuth = [{ id: closer.RETAINED_AUTH, user_id: ACTOR, kind: 'emby', device_id: old.device_id, revoked_at: '2025-12-31T23:01:00Z' },
    ...Array.from({ length: 10 }, (_, index) => ({ id: 'old-' + index, user_id: CONTROL, revoked_at: '2025-12-31T23:01:00Z' }))];
  for (const state of [value.before, value.after]) state.tables.sessions.unshift(...clone(oldAuth));
  value.before.tables.play_sessions.push(clone(old));
  value.after.tables.play_sessions.unshift({ ...clone(old), state: 'Expired', stopped_at: at(1), updated_at: '2026-01-01T00:00:01.000001000Z' });
  value.before.tables.user_item_data.push({ user_id: ACTOR, item_id: ITEM, playback_position_ticks: 0, play_count: 0,
    is_favorite: false, played: false, last_played_at: null, updated_at: old.created_at });
  const first = value.physical.logins[0], firstChain = value.physical.chains[0];
  first.login = { ordinal: 0 }; first.chains = [firstChain];
  const info = (login, play, ordinal, begin, end) => ({ itemId: ITEM, value: { PlaySessionId: play.id }, exchange: {
    ordinal, tokenHash: login.tokenHash, scope: { kind: 'playback_info', route: '/Items/' + ITEM + '/PlaybackInfo' }, complete: true,
    response: { status: 200 }, intent: { startedAt: at(begin) }, result: { completedAt: at(end) } } });
  first.infos = [info(first, firstChain.play, 1, 1, 2)];
  const second = { sessionId: 'session_second', tokenHash: sha('second-synthetic-token'), device_id: 'second-device',
    client: first.client, client_version: first.client_version, login: { ordinal: 11 }, infos: [], chains: [] };
  const play = { ...clone(firstChain.play), id: 'play_second', auth_session_id: second.sessionId, device_id: second.device_id,
    created_at: at(26), started_at: at(27), stopped_at: at(28), updated_at: at(28) };
  const started = { event: 'started', body: { PositionTicks: 120000000 }, exchange: { ordinal: 20, intent: { startedAt: at(26) }, result: { completedAt: at(27) } } };
  const stopped = { event: 'stopped', body: { PositionTicks: 120000000 }, exchange: { ordinal: 21, intent: { startedAt: at(28) }, result: { completedAt: at(29) } } };
  const secondChain = { play, started: [started], stopped: [stopped], reports: [started, stopped] };
  firstChain.started = [firstChain.reports[0]];
  second.chains = [secondChain]; second.infos = [info(second, play, 13, 25, 26)];
  value.physical.logins.push(second); value.physical.chains.push(secondChain);
  value.after.tables.play_sessions.push(play);
  value.after.tables.sessions.push({ ...clone(value.after.tables.sessions[11]), id: second.sessionId, token_hash: '\\x' + second.tokenHash,
    device_id: second.device_id, device_registry_id: 2, created_at: at(26), revoked_at: at(29), last_seen_at: at(29) });
  value.after.tables.devices.push({ ...clone(value.after.tables.devices[0]), id: 2, reported_device_id: second.device_id, created_at: at(26), last_seen_at: at(29) });
  value.after.tables.activity_entries.push(...value.after.tables.activity_entries.map((row, index) => ({ ...clone(row), id: index + 3,
    actor_credential_id: second.sessionId, resource_id: second.sessionId, created_at: at(index === 0 ? 26 : 29) })));
  Object.assign(value.after.tables.user_item_data.find(row => row.user_id === ACTOR), { play_count: 2, last_played_at: at(27), updated_at: at(28) });
  value.after.sequences.devices_id_seq.lastValue = '2'; value.after.sequences.activity_entries_id_seq.lastValue = '4';
  const readback = { ordinal: 12, tokenHash: second.tokenHash, original: { method: 'GET' }, scope: { route: '/Users/' + ACTOR + '/Items/' + ITEM },
    complete: true, response: { status: 200 }, result: { bodyEvidenceComplete: true }, responseEntity: jsonBytes({ Id: ITEM, UserData: { PlaybackPositionTicks: 120000000, PlayCount: 1 } }) };
  value.physical.exchanges = [first.infos[0].exchange, second.infos[0].exchange, readback];
  const closeout = { kind: 'audited-core-movie04-failure-closeout', status: 'closed_failed_attempt_with_retained_unstarted_preparation',
    runtimeEpoch: { path: '/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/candidate-backup-limits-revision-01/private/runtime-epoch.json', sha256: '72e25f907619fbdf82879070c6fce6178cc8c7881e8015a99991f62e64a2a73e' },
    sourceAfter: clone(closer.RETAINED_SNAPSHOT), scenario: 'movie', runId: 'movie-04', browserAndGatewayClosed: true, allElevenSessionsRevoked: true,
    clientAcceptance: false, playbackStarted: false, clientPlaybackReferences: 0, encodingJobs: 0,
    retainedPreparation: { id: closer.RETAINED_PLAY, authSessionId: closer.RETAINED_AUTH, itemId: ITEM, ownerCredentialRevoked: true } };
  value.retained = { closeout, snapshot: clone(value.before) };
  return value;
}

function universalFixture(closer, scenario = 'mp3') {
  const currentManifest = { ...clone(manifest), scenario, catalog: { [scenario]: {
    id: ITEM, type: 'Audio', container: scenario, runtimeTicks: 6000000000 } } };
  const nonce = 'synthetic-client-playback-nonce';
  const url = new URL('/emby/Audio/' + ITEM + '/universal.' + scenario, ORIGIN);
  for (const [key, value] of Object.entries({ PlaySessionId: nonce, UserId: ACTOR, DeviceId: 'synthetic-device',
    MediaSourceId: scenario === 'mp3' ? ITEM : 'mediasource_' + ITEM })) url.searchParams.set(key, value);
  const fixture = exchangeFixture({ media: true, target: url.pathname + url.search });
  const exchange = closer.verifyExchange(fixture.intent, fixture.result, currentManifest);
  const login = { sessionId: 'session_synthetic', tokenHash: sha(TOKEN), device_id: 'synthetic-device', infos: [], media: [exchange] };
  const play = { id: 'play_synthetic', client_correlated: true, user_id: ACTOR, auth_session_id: login.sessionId,
    device_id: login.device_id, application_client_id: null, item_id: ITEM, media_source_id: 'mediasource_' + ITEM };
  const references = [{ user_id: ACTOR, auth_session_id: login.sessionId, device_id: login.device_id,
    application_client_id: null, client_nonce: nonce, play_session_id: play.id }];
  return { manifest: currentManifest, login, play, references, exchange, nonce, beforeOrdinal: 8 };
}

function runtimeLineageFixture() {
  // Contract digests below are values in synthetic references, not assertions
  // that any retained artifact was read or that live admission succeeded.
  const pin = (name, digest = sha('synthetic-lineage-' + name)) => ({ path: '/opt/goby-test/synthetic-lineage/' + name, sha256: digest });
  const previousEpochPin = pin('epoch-v1.json', '7bcdbc529fd1ba3f6a62f66585e6788cc9efa1aac22a4accc8d339d69ccf6ae2');
  const previousBindingPin = pin('seed-binding-v1.json', 'e00c5a3ae5bf45da5f774b2f71cbecda9150ee876cba082c7fc23a42616db552');
  const additions = { GOBY_BACKUP_MAX_OBJECT_BYTES: '67108864', GOBY_BACKUP_MAX_TOTAL_BYTES: '268435456' };
  const postgres = { pid: 2001, startTicks: '100', bootId: 'synthetic-boot' };
  const candidateProcess = { pid: 2002, startTicks: '101', bootId: 'synthetic-boot' };
  const currentSource = { archiveSha256: sha('synthetic-archive'), sourceManifest: pin('source-manifest.json'),
    binary: pin('goby'), fullReport: pin('full-report.json'), schema: 28 };
  const transitionInput = pin('binary-transition-input.json'), runtimeHelper = pin('runtime-helper.py');
  const originalSeed = pin('original-seed.json'), runtime = pin('runtime.env');
  const previousEpoch = { kind: 'audited-candidate-runtime-epoch', version: 1, status: 'running_awaiting_live_acceptance',
    transitionInput, transitionHelper: pin('binary-transition-helper.py'), runtimeHelper,
    originalProvision: pin('original-provision.json'), seedProvenance: originalSeed, currentSource,
    candidate: { bootstrapExecuted: true, sourceState: { users: 8, schema: 28, migrations: 28 },
      binary: clone(currentSource.binary), currentSourceManifest: clone(currentSource.sourceManifest), runtime,
      input: transitionInput, processes: { postgres, candidate: candidateProcess }, serverIdentity: { id: 'd'.repeat(32) },
      listener: { host: '127.0.0.1', port: 19181, socketInode: '7001' }, database: { name: 'synthetic_candidate' } },
    candidateProcess, postgresProcess: postgres, lease: { backendPid: 2003 }, before: pin('binary-before.json'),
    after: pin('binary-after.json'), preservation: { retained: true }, calls: { stop: 1, replace: 1, start: 1 },
    helpers: { runtime: runtimeHelper }, candidateAdmissionComplete: false };
  const previousBinding = { kind: 'audited-candidate-seed-runtime-binding', version: 1, runtimeEpoch: previousEpochPin,
    originalSeed, seedExecutor: pin('seed-executor.py'), seedInput: pin('seed-input.json'),
    seedSessionAddendum: pin('seed-session-addendum.json'), admission02: pin('admission02.json'), serverId: 'd'.repeat(32),
    admin: { id: 'e'.repeat(32) }, actors: { mp3: { id: ACTOR, username: 'synthetic-actor' } }, controlQ: { id: CONTROL },
    catalog: { mp3: { id: ITEM } }, catalogFile: pin('catalog.json'), actualCatalogDtos: pin('catalog-dtos.json'),
    libraries: [], roots: [], resources: {}, seedCleanup: { complete: true },
    currentSessions: ['admin', 'P', 'Q'].map(role => ({ kind: role === 'admin' ? 'admin' : 'emby',
      tokenSha256: sha('synthetic-old-' + role), credentialId: 'synthetic-old-' + role })), candidateAdmissionComplete: false };
  const admission03 = pin('admission03.json', 'a59638abe364b89e9c44482986698d0b1d310147178cbbe2ab4a0c1412c29f97');
  const failureCloseout = pin('failure-closeout.json', '090d04421c8fb847822695143483b153f096e2707571c14a01d4860d48233e2a');
  const closedState = pin('closed-state.json', '9c2a6660fda9f2da5c33df8c93fbbc4786c397d158755d1de3712eda6f5e01d0');
  const configurationPin = pin('environment-revision-input.json');
  const failedAdmission03 = { kind: 'audited-candidate-live-admission', status: 'admission_failed_resources_retained',
    runtimeEpoch: clone(previousEpochPin), cleanupFailures: [], playbackRequests: 0, applyRequests: 0, rollbackRequests: 0,
    controllerSessions: Object.fromEntries(['admin', 'P', 'Q'].map(role => [role, { sameTokenRejected: true,
      tokenSha256: sha('synthetic-new-' + role), credentialId: 'synthetic-new-' + role }])) };
  const epoch = { ...clone(previousEpoch), version: 2, previousEpoch: previousEpochPin, productInput: transitionInput,
    transitionInput: configurationPin, transitionHelper: pin('environment-revision-helper.py'), operationKind: 'environment_revision',
    calls: { stop: 1, replaceEnvironment: 1, start: 1 }, configurationChange: {
      before: clone(runtime), after: { path: runtime.path, sha256: sha('synthetic-revised-environment') },
      preservedCopy: pin('runtime-before.env', runtime.sha256), additions: clone(additions),
      requiredFreeBytes: 234946560, observedFreeBytesBefore: 300000000 } };
  epoch.candidate.runtime = clone(epoch.configurationChange.after);
  epoch.candidate.input = clone(configurationPin); epoch.candidate.productInput = clone(transitionInput);
  epoch.candidateProcess = { pid: 2012, startTicks: '201', bootId: 'synthetic-boot' };
  epoch.candidate.processes.candidate = clone(epoch.candidateProcess);
  epoch.candidate.listener.socketInode = '7002';
  const addedSessions = ['admin', 'P', 'Q'].map(role => ({ kind: role === 'admin' ? 'admin' : 'emby',
    tokenSha256: failedAdmission03.controllerSessions[role].tokenSha256,
    credentialId: failedAdmission03.controllerSessions[role].credentialId }));
  const seed = { ...clone(previousBinding), version: 2, runtimeEpoch: pin('epoch-v2.json'), previousBinding: previousBindingPin,
    admission03, failureCloseout, closedState, currentSessions: [...clone(previousBinding.currentSessions), ...addedSessions] };
  const configurationInput = { kind: 'audited-candidate-environment-revision-input', version: 1,
    previousEpoch: clone(previousEpochPin), previousSeedBinding: clone(previousBindingPin), runtimeHelper: clone(runtimeHelper),
    additions: clone(additions), admission03: clone(admission03), failureCloseout: clone(failureCloseout), closedState: clone(closedState) };
  return { epoch, seed, lineage: { previousEpoch, previousBinding, configurationInput, failedAdmission03 } };
}

function guardCases(closer) {
  return [
    ['snapshot_int64_metadata_decodes_exactly_without_rounding_or_changing_other_types', () => {
      const raw = Buffer.from('{"tables":{"item_subtitles":[{"change_time_ns":1789286066604301243},{"change_time_ns":0}],"items":[{"media":{"FileChangeTimeNs":1789286066604301244}},{"media":null},{"media":{"FileChangeTimeNs":-9223372036854775808}},{"media":{"FileChangeTimeNs":9223372036854775807}}]},"count":7}');
      const copy = Buffer.from(raw), value = closer.sourceSnapshotJSON(raw);
      assert.equal(value.tables.item_subtitles[0].change_time_ns, '1789286066604301243');
      assert.equal(value.tables.item_subtitles[1].change_time_ns, '0');
      assert.equal(value.tables.items[0].media.FileChangeTimeNs, '1789286066604301244');
      assert.notEqual(value.tables.item_subtitles[0].change_time_ns, value.tables.items[0].media.FileChangeTimeNs);
      assert.equal(value.tables.items[1].media, null);
      assert.equal(value.tables.items[2].media.FileChangeTimeNs, '-9223372036854775808');
      assert.equal(value.tables.items[3].media.FileChangeTimeNs, '9223372036854775807');
      assert.equal(value.count, 7); assert.deepEqual(raw, copy);
      assert.throws(() => closer.strictJSON(raw), /json_unsafe_number/);
    }],
    ['snapshot_int64_metadata_rejects_type_confusion_range_overflow_and_business_unsafe_numbers', () => {
      const source = token => Buffer.from('{"tables":{"items":[{"media":{"FileChangeTimeNs":' + token + '}}]}}');
      const subtitle = token => Buffer.from('{"tables":{"item_subtitles":[{"change_time_ns":' + token + '}]}}');
      for (const token of ['"1789286066604301243"', 'null', 'true', '{}', '[]', '1.0', '1e3']) {
        assert.throws(() => closer.sourceSnapshotJSON(source(token)), /json_int64_numeric_token_required/);
        assert.throws(() => closer.sourceSnapshotJSON(subtitle(token)), /json_int64_numeric_token_required/);
      }
      for (const token of ['9223372036854775808', '-9223372036854775809', '999999999999999999999999999999'])
        assert.throws(() => closer.sourceSnapshotJSON(source(token)), /json_int64_range/);
      assert.throws(() => closer.sourceSnapshotJSON(subtitle('-1')), /json_int64_range/);
      assert.throws(() => closer.sourceSnapshotJSON(subtitle('-9223372036854775808')), /json_int64_range/);
      assert.equal(closer.sourceSnapshotJSON(subtitle('9223372036854775807')).tables.item_subtitles[0].change_time_ns, '9223372036854775807');
      for (const raw of ['{"tables":{"user_item_data":[{"play_count":9007199254740992}]}}',
        '{"tables":{"items":[{"media":{"DurationTicks":9007199254740992}}]}}',
        '{"wrapper":{"tables":{"items":[{"media":{"FileChangeTimeNs":9007199254740992}}]}}}',
        '{"tables":{"items":{"0":{"media":{"FileChangeTimeNs":9007199254740992}}}}}',
        '{"FileChangeTimeNs":9007199254740992}']) assert.throws(() => closer.sourceSnapshotJSON(Buffer.from(raw)), /json_unsafe_number/);
    }],
    ['typed_readers_preserve_duplicate_utf8_depth_and_size_guards', () => {
      assert.throws(() => closer.sourceSnapshotJSON(Buffer.from('{"tables":{"items":[{"media":{"FileChangeTimeNs":1,"FileChangeTimeNs":2}}]}}')), /json_duplicate_key/);
      assert.throws(() => closer.sourceSnapshotJSON(Buffer.from([0xff])));
      assert.throws(() => closer.sourceSnapshotJSON(Buffer.from('['.repeat(42) + '0' + ']'.repeat(42))), /json_complexity_limit/);
      assert.throws(() => closer.sourceSnapshotJSON(Buffer.from('{"safe":1}'), 4), /json_size_limit/);
      assert.deepEqual(closer.sourceSnapshotJSON(Buffer.from('{"note":"FileChangeTimeNs:9007199254740992","safe":1.5}')),
        { note: 'FileChangeTimeNs:9007199254740992', safe: 1.5 });
    }],
    ['previous_binary_epoch_reader_is_bound_to_its_two_fixed_stat_paths', () => {
      const raw = Buffer.from('{"preservation":{"oldBinaryFacts":{"ctimeNs":1789284802646009498,"mtimeNs":-9223372036854775808,"bytes":99},"other":true}}');
      const value = closer.previousBinaryEpochJSON(raw, closer.PREVIOUS_BINARY_EPOCH);
      assert.deepEqual(closer.runtimeEpochJSON(raw, closer.PREVIOUS_BINARY_EPOCH), value);
      assert.deepEqual(value.preservation.oldBinaryFacts, { ctimeNs: '1789284802646009498', mtimeNs: '-9223372036854775808', bytes: 99 });
      assert.equal(value.preservation.other, true);
      assert.throws(() => closer.strictJSON(raw), /json_unsafe_number/);
      assert.throws(() => closer.sourceSnapshotJSON(raw), /json_unsafe_number/);
      assert.throws(() => closer.previousBinaryEpochJSON(raw, { ...closer.PREVIOUS_BINARY_EPOCH, sha256: sha('different') }), /previous_binary_epoch_reader_authority/);
      assert.throws(() => closer.previousBinaryEpochJSON(raw, { ...closer.PREVIOUS_BINARY_EPOCH, path: '/opt/goby-test/another-epoch.json' }), /previous_binary_epoch_reader_authority/);
      assert.throws(() => closer.runtimeEpochJSON(raw, { ...closer.PREVIOUS_BINARY_EPOCH, sha256: sha('different') }), /json_unsafe_number/);
      assert.deepEqual(closer.runtimeEpochJSON(Buffer.from('{"ordinary":1}'), { ...closer.PREVIOUS_BINARY_EPOCH, sha256: sha('different') }), { ordinary: 1 });
      for (const token of ['null', '"1"', '1e2', '1.0', '{}']) assert.throws(() => closer.previousBinaryEpochJSON(
        Buffer.from('{"preservation":{"oldBinaryFacts":{"mtimeNs":' + token + '}}}'), closer.PREVIOUS_BINARY_EPOCH), /json_int64_numeric_token_required/);
      assert.throws(() => closer.previousBinaryEpochJSON(Buffer.from('{"preservation":{"oldBinaryFacts":{"ctimeNs":9223372036854775808}}}'), closer.PREVIOUS_BINARY_EPOCH), /json_int64_range/);
      assert.throws(() => closer.previousBinaryEpochJSON(Buffer.from('{"preservation":{"oldBinaryFacts":{"bytes":9007199254740992}}}'), closer.PREVIOUS_BINARY_EPOCH), /json_unsafe_number/);
    }],
    ['retained_movie_input_is_explicit_and_other_scenarios_keep_empty_baseline', () => {
      const names = 'manifest observation summary gatewayAttestation gatewayIndex runtimeEpoch admission seedBinding sourceBefore sourceAfter boundary'.split(' ');
      const pin = { path: '/opt/goby-test/synthetic.json', sha256: sha('synthetic') };
      const input = { kind: 'audited-candidate-client-closeout-input', version: 1, ...Object.fromEntries(names.map(name => [name, pin])),
        sources: Object.fromEntries('closer adapter gateway proxy sessionProof movie audio subtitles tv'.split(' ').map(name => [name, pin])), output: '/opt/goby-test/synthetic-output' };
      closer.validateCloseoutInput(input);
      assert.throws(() => closer.validateCloseoutInput({ ...input, retainedBaseline: closer.RETAINED_BASELINE }), /closeout_input_invalid/);
      closer.validateCloseoutInput({ ...input, version: 2, retainedBaseline: closer.RETAINED_BASELINE });
      assert.throws(() => closer.validateCloseoutInput({ ...input, version: 2, retainedBaseline: { ...pin } }), /retained_movie_input_authority/);
      const value = retainedFixture(closer);
      assert.throws(() => closer.verifyDurable(value.before, value.after, value.manifest, value.seed, value.physical), /scenario_actor_playback_baseline_not_fresh/);
      for (const scenario of ['episode', 'mp3', 'flac', 'subtitles', 'tv-browse'])
        assert.throws(() => closer.verifyRetainedMovieBefore(value.before, { ...value.manifest, scenario }, value.seed, value.retained), /retained_movie_scenario_required/);
    }],
    ['retained_movie_full_durable_comparison_keeps_old_data_and_excludes_expired_row', () => {
      const value = retainedFixture(closer), original = jsonBytes(value.before);
      const proof = closer.verifyDurable(value.before, value.after, value.manifest, value.seed, value.physical, value.retained);
      assert.equal(proof.countedPlays, 2); assert.equal(proof.selectedUserdata.play_count, 2);
      assert.deepEqual(proof.retainedBaselineExpiration.changedFields, ['state', 'stopped_at', 'updated_at']);
      assert.equal(proof.retainedBaselineExpiration.playSessionId, closer.RETAINED_PLAY);
      assert.equal(proof.retainedBaselineExpiration.prepareOrdinal, 1);
      assert.deepEqual(jsonBytes(value.before), original);
    }],
    ['retained_movie_rejects_deletion_extra_fields_wrong_auth_count_and_window', () => {
      const mutations = [
        value => { value.after.tables.play_sessions.shift(); },
        value => { value.after.tables.play_sessions[0].counted = true; },
        value => { value.after.tables.play_sessions[0].auth_session_id = 'wrong'; },
        value => { delete value.after.tables.play_sessions[0].player_state; },
        value => { value.after.tables.play_sessions[0].extra = true; },
        value => { value.after.tables.play_sessions[0].updated_at = '2026-01-01T00:00:03Z'; },
        value => { value.after.tables.play_sessions[0].stopped_at = '2026-01-01T00:00:00Z'; },
        value => { value.after.tables.sessions[0].revoked_at = null; },
        value => { value.physical.logins[0].sessionId = closer.RETAINED_AUTH; },
        value => { value.physical.logins[0].infos[0].exchange.tokenHash = sha('wrong'); },
        value => { value.after.tables.play_sessions.push({ ...clone(value.after.tables.play_sessions[0]), id: 'unknown-old' }); },
        value => { value.physical.exchanges.push(...Array.from({ length: 255 }, () => ({ scope: { kind: 'playback_info' } }))); },
      ];
      for (const mutate of mutations) { const value = retainedFixture(closer); mutate(value);
        assert.throws(() => closer.verifyDurable(value.before, value.after, value.manifest, value.seed, value.physical, value.retained)); }
      for (const mutate of [value => { value.before.tables.play_sessions[0].counted = true; },
        value => { value.before.tables.user_item_data.find(row => row.user_id === ACTOR).play_count = 1; },
        value => { value.before.tables.sessions[0].revoked_at = null; },
        value => { value.before.tables.client_playback_references.push({ user_id: ACTOR }); },
        value => { value.before.tables.encoding_jobs.push({ user_id: ACTOR }); }]) {
        const value = retainedFixture(closer); mutate(value);
        assert.throws(() => closer.verifyRetainedMovieBefore(value.before, value.manifest, value.seed, value.retained));
      }
      const stale = retainedFixture(closer); stale.before.capturedAt = '2026-01-07T23:15:00Z';
      assert.throws(() => closer.verifyRetainedMovieBefore(stale.before, stale.manifest, stale.seed, stale.retained), /retained_movie_pruning_deadline/);
    }],
    ['strict_json_rejects_duplicate_keys_and_unsafe_numbers', () => {
      assert.deepEqual(closer.strictJSON(Buffer.from('{"safe":9007199254740991,"nested":{"fraction":1.5}}')),
        { safe: Number.MAX_SAFE_INTEGER, nested: { fraction: 1.5 } });
      for (const source of ['{"a":1,"a":2}', String.raw`{"a":1,"\u0061":2}`])
        assert.throws(() => closer.strictJSON(Buffer.from(source)), /json_duplicate_key/);
      for (const source of ['9007199254740993', '-9007199254740993', '1e999'])
        assert.throws(() => closer.strictJSON(Buffer.from(source)), /json_unsafe_number/);
      assert.throws(() => closer.strictJSON(Buffer.from([0xff])));
      assert.throws(() => closer.strictJSON(Buffer.from('true false')), /json_trailing_bytes/);
    }],
    ['entity_decoding_preserves_bytes_and_enforces_boundaries_and_limits', () => {
      const entity = Buffer.from(' { "synthetic" : "UTF-8 π bytes" } \n');
      for (const [encoding, compressed] of [['identity', entity], ['gzip', gzipSync(entity)], ['br', brotliCompressSync(entity)]]) {
        const wire = chunked(compressed), before = Buffer.from(wire);
        const head = closer.parseHead(responseHead('200 OK', [['Transfer-Encoding', 'chunked'], ['Content-Encoding', encoding]]), true);
        assert.deepEqual(closer.decodeEntity(wire, head), entity);
        assert.deepEqual(wire, before);
      }
      const chunkHead = closer.parseHead(responseHead('200 OK', [['Transfer-Encoding', 'chunked']]), true);
      assert.throws(() => closer.decodeEntity(Buffer.from('3\r\nabc!\r\n0\r\n\r\n'), chunkHead), /chunk_data_boundary/);
      assert.throws(() => closer.decodeEntity(Buffer.from('1\r\nx\r\n0\r\nAuthorization: forbidden\r\n\r\n'), chunkHead), /forbidden_chunk_trailer/);
      const lengthHead = closer.parseHead(responseHead('200 OK', [['Content-Length', '3']]), true);
      assert.throws(() => closer.decodeEntity(Buffer.from('abcd'), lengthHead), /entity_content_length_mismatch/);
      assert.throws(() => closer.decodeEntity(Buffer.alloc(5), lengthHead, 4), /wire_body_limit/);
      for (const [encoding, compress] of [['gzip', gzipSync], ['br', brotliCompressSync]]) {
        const head = closer.parseHead(responseHead('200 OK', [['Content-Encoding', encoding]]), true);
        assert.throws(() => closer.decodeEntity(compress(Buffer.alloc(4096, 120)), head, 1024));
      }
    }],
    ['complete_plain_text_401_needs_metadata_not_a_retained_body', () => {
      const { intent, result } = exchangeFixture();
      const before = jsonBytes({ intent, result });
      const exchange = closer.verifyExchange(intent, result, manifest);
      assert.equal(exchange.complete, true); assert.equal(exchange.response.status, 401);
      assert.equal(exchange.responseEntity, null); assert.equal(result.bodyEvidenceComplete, false);
      assert.equal(exchange.tokenHash, sha(TOKEN)); assert.equal(exchange.deliveredBodyBytes, 12);
      assert.deepEqual(jsonBytes({ intent, result }), before);
      for (const mutate of [value => value.upstreamBytesWritten++, value => value.upstreamBytesWritten--,
        value => value.clientBytesWritten--, value => { value.requestBodyComplete = false; }]) {
        const changed = clone(result); mutate(changed);
        assert.throws(() => closer.verifyExchange(intent, changed, manifest), /write_count_exceeds_observation|request_forward_incomplete|response_forward_incomplete/);
      }
    }],
    ['media_get_requires_positive_delivery_and_valid_range_without_promoting_flags', () => {
      const { intent, result } = exchangeFixture({ media: true });
      const exchange = closer.verifyExchange(intent, result, manifest);
      assert.equal(exchange.requestDelivered, true);
      assert.equal(result.requestForwardedComplete, false);
      assert.equal(closer.requireMediaGET(exchange), undefined);
      assert.equal(exchange.complete, false); assert.equal(result.completeHTTP, false);
      assert.equal(result.responseForwardedComplete, false); assert.equal(exchange.deliveredBodyBytes, 128);
      const headOnly = exchangeFixture({ media: true, method: 'HEAD', delivered: 0 });
      assert.throws(() => closer.requireMediaGET(closer.verifyExchange(headOnly.intent, headOnly.result, manifest)), /actual_media_entity_bytes_missing/);
      const empty = exchangeFixture({ media: true, delivered: 0 });
      assert.throws(() => closer.requireMediaGET(closer.verifyExchange(empty.intent, empty.result, manifest)), /actual_media_entity_bytes_missing/);
      for (const contentRange of ['bytes 0-998/4096', 'bytes 1-1000/1000', 'bytes */4096']) {
        const bad = exchangeFixture({ media: true, contentRange });
        assert.throws(() => closer.requireMediaGET(closer.verifyExchange(bad.intent, bad.result, manifest)), /media_content_range_invalid/);
      }
    }],
    ['frame_and_service_worker_abort_observations_bind_one_partial_ordinal', () => {
      const { exchange, report, login } = partialFixture(closer);
      const before = jsonBytes(exchange.result);
      const proof = closer.explainMediaPartial(exchange, report, login);
      assert.deepEqual(proof, { ordinal: 7, interpretation: 'browser_abort_near_owned_stop', completeHTTP: false,
        responseForwardedComplete: false, deliveredBodyBytes: 128 });
      assert.equal(report.requests.length, 2); assert.deepEqual(jsonBytes(exchange.result), before);
      const seekLogin = { chains: [], media: [{ ordinal: 8, original: { get: key => key === 'range' ? 'bytes=1000-' : null },
        intent: { startedMonotonicNs: '12500000000' } }] };
      const seekReport = clone(report);
      for (const row of seekReport.requests) row.failure_ui_phase = 'movie-seek-forward';
      const seek = closer.explainMediaPartial(exchange, seekReport, seekLogin);
      assert.equal(seek.ordinal, 7); assert.equal(seek.interpretation, 'browser_abort_near_range_seek');
      assert.equal(seek.completeHTTP, false); assert.equal(seek.responseForwardedComplete, false);
    }],
    ['partial_media_rejects_missing_abort_time_wrong_error_duplicates_or_no_action', () => {
      for (const [name, mutate, expected] of [
        ['missing_time', value => { delete value.report.requests[0].failed_elapsed_ms; }, /media_partial_not_browser_abort/],
        ['wrong_error', value => { value.report.requests[0].failure_error_text = 'net::ERR_CONNECTION_RESET'; }, /media_partial_not_browser_abort/],
        ['duplicate_frame', value => { value.report.requests.push(clone(value.report.requests[0])); }, /media_partial_ambiguous_context/],
        ['no_action', value => { value.login.chains = []; value.login.media = []; }, /media_partial_without_seek_or_stop/],
        ['range_without_seek_phase', value => { value.login.chains = []; value.login.media = [{ ordinal: 8,
          original: { get: key => key === 'range' ? 'bytes=1000-' : null }, intent: { startedMonotonicNs: '12500000000' } }]; }, /media_partial_without_seek_or_stop/],
        ['distant_abort', value => { value.report.requests[0].failed_elapsed_ms = 9000; }, /media_partial_time_mismatch/],
      ]) {
        const value = partialFixture(closer); mutate(value);
        assert.throws(() => closer.explainMediaPartial(value.exchange, value.report, value.login), expected, name);
      }
    }],
    ['stop_position_uses_exact_two_percent_ninety_percent_and_120_second_boundaries', () => {
      for (const [position, duration, expected] of [
        [119999999, 6000000000, { position: 0, completed: false }],
        [120000000, 6000000000, { position: 120000000, completed: false }],
        [5399999999, 6000000000, { position: 5399999999, completed: false }],
        [5400000000, 6000000000, { position: 0, completed: true }],
        [24000000, 1199999999, { position: 0, completed: false }],
        [24000000, 1200000000, { position: 24000000, completed: false }],
        [24000000, 1200000001, { position: 0, completed: false }],
        [24000001, 1200000001, { position: 24000001, completed: false }],
        [1080000000, 1200000001, { position: 1080000000, completed: false }],
        [1080000001, 1200000001, { position: 0, completed: true }],
        [6000000001, 6000000000, { position: 0, completed: true }],
        [0, 0, { position: 0, completed: false }],
      ]) assert.deepEqual(closer.stopPosition(position, duration), expected);
      assert.throws(() => closer.stopPosition(Number.MAX_SAFE_INTEGER + 1, 6000000000), /stop_position_invalid/);
      assert.throws(() => closer.stopPosition(-1, 6000000000), /stop_position_invalid/);
    }],
    ['durable_single_play_preserves_q_and_requires_userdata_count_position_and_revocation', () => {
      const value = durableFixture();
      const before = jsonBytes(value.before);
      const proof = closer.verifyDurable(value.before, value.after, manifest, value.seed, value.physical);
      assert.equal(proof.sessionsRevoked, 1); assert.equal(proof.countedPlays, 1); assert.equal(proof.controlQUnchanged, true);
      assert.equal(proof.selectedUserdata.play_count, 1); assert.equal(proof.selectedUserdata.playback_position_ticks, 120000000);
      assert.deepEqual(jsonBytes(value.before), before);
      for (const [name, mutate, expected] of [
        ['q_changed', after => { after.tables.user_item_data[0].play_count++; }, /foreign_actor_state_changed_user_item_data/],
        ['wrong_count', after => { after.tables.user_item_data[1].play_count++; }, /userdata_count_or_scope_mismatch/],
        ['wrong_position', after => { after.tables.user_item_data[1].playback_position_ticks++; }, /userdata_stop_policy_mismatch/],
        ['not_revoked', after => { after.tables.sessions[0].revoked_at = null; }, /durable_auth_binding_or_revocation/],
        ['missing_userdata', after => { after.tables.user_item_data = after.tables.user_item_data.filter(row => row.user_id !== ACTOR); }, /affected_userdata_missing/],
      ]) {
        const current = durableFixture(); mutate(current.after);
        assert.throws(() => closer.verifyDurable(current.before, current.after, manifest, current.seed, current.physical), expected, name);
      }
    }],
    ['runtime_lineage_accepts_only_the_bound_binary_and_environment_epochs', () => {
      const value = runtimeLineageFixture(), before = jsonBytes(value);
      assert.equal(closer.validateRuntimeLineage(value.lineage.previousEpoch, value.lineage.previousBinding), undefined);
      assert.equal(closer.validateRuntimeLineage(value.epoch, value.seed, value.lineage), undefined);
      assert.deepEqual(jsonBytes(value), before);
      for (const [name, mutate, expected] of [
        ['operation_kind', row => { row.epoch.operationKind = 'binary_replacement'; }, /environment_product_lineage_changed/],
        ['environment_calls', row => { row.epoch.calls.replaceEnvironment = 2; }, /environment_product_lineage_changed/],
        ['current_source', row => { row.epoch.currentSource.archiveSha256 = sha('another-synthetic-product'); }, /environment_product_lineage_changed/],
        ['generalized_additions', row => { row.epoch.configurationChange.additions.GOBY_MEDIA_ROOTS = '/synthetic/new-root';
          row.lineage.configurationInput.additions.GOBY_MEDIA_ROOTS = '/synthetic/new-root'; }, /environment_configuration_changed/],
        ['missing_current_session', row => { row.seed.currentSessions.pop(); }, /environment_seed_session_history/],
        ['wrong_previous_binding', row => { row.seed.previousBinding.sha256 = sha('wrong-synthetic-binding'); }, /environment_product_lineage_changed/],
        ['controller_not_revoked', row => { row.lineage.failedAdmission03.controllerSessions.P.sameTokenRejected = false; }, /environment_controller_session_not_revoked/],
        ['unsupported_version', row => { row.epoch.version = 3; row.seed.version = 3; }, /runtime_lineage_schema/],
        ['binary_calls', row => { row.lineage.previousEpoch.calls.replace = 2; }, /binary_epoch_calls_or_sessions/],
      ]) {
        const current = runtimeLineageFixture(); mutate(current);
        assert.throws(() => closer.validateRuntimeLineage(current.epoch, current.seed, current.lineage), expected, name);
      }
    }],
    ['universal_audio_preparation_requires_the_same_owned_nonce_and_prior_media_get', () => {
      for (const scenario of ['mp3', 'flac']) {
        const value = universalFixture(closer, scenario), original = jsonBytes(value.exchange.result);
        assert.deepEqual(value.login.infos, []);
        assert.deepEqual(closer.universalAudioPreparation(value.play, value.login, value.manifest, value.references, value.beforeOrdinal),
          { kind: 'universal_audio', ordinal: 7, clientNonceSha256: sha(value.nonce) });
        assert.equal(value.exchange.complete, false); assert.equal(value.exchange.result.completeHTTP, false);
        assert.equal(value.exchange.result.responseForwardedComplete, false); assert.deepEqual(jsonBytes(value.exchange.result), original);
      }
      for (const [name, mutate] of [
        ['not_universal', value => { value.exchange.url.pathname = '/emby/Audio/' + ITEM + '/stream.mp3'; }],
        ['head_only', value => { value.exchange.original.method = 'HEAD'; }],
        ['foreign_token', value => { value.exchange.tokenHash = sha('foreign-token'); }],
        ['foreign_play_user', value => { value.play.user_id = CONTROL; }],
        ['foreign_auth', value => { value.play.auth_session_id = 'foreign-session'; }],
        ['foreign_device', value => { value.play.device_id = 'foreign-device'; }],
        ['foreign_source', value => { value.play.media_source_id = 'foreign-source'; }],
        ['foreign_query_user', value => { value.exchange.url.searchParams.set('UserId', CONTROL); }],
        ['foreign_query_device', value => { value.exchange.url.searchParams.set('DeviceId', 'foreign-device'); }],
        ['foreign_query_source', value => { value.exchange.url.searchParams.set('MediaSourceId', 'foreign-source'); }],
        ['missing_nonce', value => { value.exchange.url.searchParams.delete('PlaySessionId'); }],
        ['duplicate_nonce', value => { value.exchange.url.searchParams.append('playsessionid', value.nonce); }],
        ['wrong_reference_nonce', value => { value.references[0].client_nonce = 'other-nonce'; }],
        ['wrong_reference_play', value => { value.references[0].play_session_id = 'play_other'; }],
        ['wrong_reference_owner', value => { value.references[0].auth_session_id = 'other-session'; }],
        ['wrong_reference_user', value => { value.references[0].user_id = CONTROL; }],
        ['wrong_reference_device', value => { value.references[0].device_id = 'other-device'; }],
        ['reference_application_client', value => { value.references[0].application_client_id = 'other-client'; }],
        ['duplicate_reference', value => { value.references.push(clone(value.references[0])); }],
        ['not_correlated', value => { value.play.client_correlated = false; }],
        ['not_prior', value => { value.beforeOrdinal = value.exchange.ordinal; }],
        ['movie_scenario', value => { value.manifest.scenario = 'movie'; value.manifest.catalog.movie = { id: ITEM, type: 'Movie' }; }],
      ]) {
        const value = universalFixture(closer); mutate(value);
        assert.equal(closer.universalAudioPreparation(value.play, value.login, value.manifest, value.references, value.beforeOrdinal), null, name);
      }
      for (const mutate of [value => { value.exchange.deliveredBodyBytes = 0; }, value => { value.exchange.response.status = 401; }]) {
        const value = universalFixture(closer); mutate(value);
        assert.throws(() => closer.universalAudioPreparation(value.play, value.login, value.manifest, value.references, value.beforeOrdinal), /actual_media_entity_bytes_missing/);
      }
    }],
    ['logout_completion_allows_only_existing_websockets_or_new_complete_401', () => {
      const tokenHash = sha(TOKEN), oldWebSocket = { ordinal: 99, tokenHash, complete: false, response: { status: 101 },
        request: { kind: 'websocket' }, intent: { startedMonotonicNs: '19999999999' } };
      const accepted401 = { ordinal: 1, tokenHash, complete: true, response: { status: 401 },
        request: { kind: 'api' }, intent: { startedMonotonicNs: '20000000000' } };
      const verification = { ordinal: 51, tokenHash, complete: true, response: { status: 401 }, intent: { startedMonotonicNs: '21000000000' } };
      const login = { tokenHash, logout: { ordinal: 50, result: { completedMonotonicNs: '20000000000' } }, verification };
      const before = jsonBytes({ login, oldWebSocket, accepted401, verification });
      assert.equal(closer.verifyPostLogoutTraffic(login, [oldWebSocket, accepted401, verification]), undefined);
      assert.deepEqual(jsonBytes({ login, oldWebSocket, accepted401, verification }), before);
      for (const [status, complete, kind] of [[101, true, 'websocket'], [401, false, 'api'], [200, true, 'api']]) {
        const current = { ...clone(accepted401), response: { status }, complete, request: { kind } };
        assert.throws(() => closer.verifyPostLogoutTraffic(login, [oldWebSocket, current, verification]), /revoked_token_used_successfully/);
      }
    }],
    ['durable_stop_and_counting_bind_real_response_windows_without_rewriting_repeated_stops', () => {
      const at = second => '2026-01-01T00:00:' + String(second).padStart(2, '0') + '.000000000Z';
      const value = durableFixture(), chain = value.physical.chains[0], play = chain.play;
      const duplicate = { event: 'stopped', body: { PositionTicks: play.position_ticks + 10000000 }, exchange: {
        ordinal: 10, intent: { startedAt: at(22) }, result: { completedAt: at(23) } } };
      chain.stopped.push(duplicate); chain.reports.push(duplicate);
      assert.deepEqual(closer.effectiveStopEvidence(play, chain.stopped), [9]);
      assert.deepEqual(closer.countedStartEvidence(play, chain.reports), [{ ordinal: 2, completedAt: at(4) }]);
      const full = { ...clone(play), position_ticks: play.duration_ticks };
      const clamped = clone(chain.stopped[0]); clamped.body.PositionTicks = play.duration_ticks + 1;
      assert.deepEqual(closer.effectiveStopEvidence(full, [clamped]), [9]);
      for (const [name, mutate, expected] of [
        ['ack_too_early', stop => { stop.exchange.result.completedAt = at(19); }, /stop_ack_before_durable_terminal/],
        ['wrong_position', stop => { stop.body.PositionTicks++; }, /effective_stop_not_bound_to_sql/],
        ['window_too_late', stop => { stop.exchange.intent.startedAt = at(21); stop.exchange.result.completedAt = at(22); }, /effective_stop_not_bound_to_sql/],
      ]) {
        const stop = clone(chain.stopped[0]); mutate(stop);
        assert.throws(() => closer.effectiveStopEvidence(play, [stop]), expected, name);
      }
      for (const event of ['started', 'progress', 'stopped']) {
        const report = clone(chain.reports[0]); report.event = event; report.body.PositionTicks = event === 'stopped' ? 1 : 0;
        assert.deepEqual(closer.countedStartEvidence(play, [report]), [{ ordinal: 2, completedAt: at(4) }]);
      }
      const zeroStop = clone(chain.reports[0]); zeroStop.event = 'stopped'; zeroStop.body.PositionTicks = 0;
      assert.throws(() => closer.countedStartEvidence(play, [zeroStop]), /counted_start_not_bound_to_report/);
      const lateReport = clone(chain.reports[0]); lateReport.exchange.intent.startedAt = at(4); lateReport.exchange.result.completedAt = at(5);
      assert.throws(() => closer.countedStartEvidence(play, [lateReport]), /counted_start_not_bound_to_report/);
      const proof = closer.verifyDurable(value.before, value.after, manifest, value.seed, value.physical);
      assert.equal(proof.countedPlays, 1); assert.equal(proof.selectedUserdata.play_count, 1);
      assert.equal(proof.selectedUserdata.playback_position_ticks, play.position_ticks);
      assert.deepEqual(chain.effectiveStopOrdinals, [9]);
      value.after.tables.user_item_data.find(row => row.user_id === ACTOR).last_played_at = at(10);
      assert.throws(() => closer.verifyDurable(value.before, value.after, manifest, value.seed, value.physical), /last_played_not_bound_to_counted_start/);
    }],
  ];
}

async function protectedOutput(filename) {
  assert.equal(path.isAbsolute(filename), true, 'absolute_report_required');
  assert.equal(filename.split(path.sep).includes('..'), false, 'normalized_report_required');
  const parent = path.dirname(filename);
  for (let current = parent;; current = path.dirname(current)) {
    const info = await fs.lstat(current);
    assert(info.isDirectory() && !info.isSymbolicLink() && info.uid === 0 && !(info.mode & 0o022), 'protected_report_ancestor_required');
    if (current === path.dirname(current)) break;
  }
  assert.equal((await fs.stat(parent)).mode & 0o777, 0o700, 'private_report_parent_required');
  try { await fs.lstat(filename); assert.fail('fresh_report_required'); } catch (error) { if (error.code !== 'ENOENT') throw error; }
}

async function savedRetainedReplay(closer) {
  const seedPin = { path: '/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/candidate-backup-limits-revision-01/private/seed-runtime-binding.json',
    sha256: '92ee92478475390e514f39e554619f322be81062a6d0256820c00bf4c8e0969f' };
  const pins = [closer.RETAINED_BASELINE, closer.RETAINED_SNAPSHOT, seedPin];
  const documents = await Promise.all(pins.map(async pin => { const bytes = await fs.readFile(pin.path);
    assert.equal(sha(bytes), pin.sha256, 'saved_replay_digest_changed');
    return pin.path === closer.RETAINED_SNAPSHOT.path ? closer.sourceSnapshotJSON(bytes) : closer.strictJSON(bytes); }));
  const [closeout, saved, seed] = documents, synthetic = retainedFixture(closer);
  const read = async (pin, decoder = closer.strictJSON) => { const bytes = await fs.readFile(pin.path);
    assert.equal(sha(bytes), pin.sha256, 'saved_replay_digest_changed'); pins.push(pin); return decoder(bytes); };
  const epoch = await read(seed.runtimeEpoch, bytes => closer.runtimeEpochJSON(bytes, seed.runtimeEpoch));
  assert.deepEqual(epoch.previousEpoch, closer.PREVIOUS_BINARY_EPOCH);
  const previousEpoch = await read(epoch.previousEpoch, bytes => { const value = closer.previousBinaryEpochJSON(bytes, epoch.previousEpoch);
    assert.deepEqual(closer.runtimeEpochJSON(bytes, epoch.previousEpoch), value); return value; });
  const lineage = { previousEpoch, previousBinding: await read(seed.previousBinding), configurationInput: await read(epoch.transitionInput), failedAdmission03: await read(seed.admission03) };
  closer.validateRuntimeLineage(epoch, seed, lineage);
  // Exercise both live-source roles on the saved bytes as well as the retained role.
  const beforeRaw = await fs.readFile(closeout.sourceBefore.path);
  assert.equal(sha(beforeRaw), closeout.sourceBefore.sha256, 'saved_replay_digest_changed');
  const priorSource = closer.sourceSnapshotJSON(beforeRaw);
  assert.equal(priorSource.tables.item_subtitles[0].change_time_ns, saved.tables.item_subtitles[0].change_time_ns);
  pins.push(closeout.sourceBefore);
  const offset = Date.parse(saved.capturedAt) - Date.parse(synthetic.before.capturedAt);
  const translate = value => {
    if (Buffer.isBuffer(value)) return jsonBytes(translate(closer.strictJSON(value)));
    if (Array.isArray(value)) return value.map(translate);
    if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, translate(item)]));
    if (typeof value !== 'string') return value;
    if (/^\d{4}-\d\d-\d\dT/.test(value)) return new Date(Date.parse(value) + offset).toISOString();
    return value.split(ACTOR).join(seed.actors.movie.id).split(CONTROL).join(seed.controlQ.id).split(ITEM).join(seed.catalog.movie.id);
  };
  const mapped = translate(synthetic), after = clone(saved), firstNext = name => {
    const old = saved.sequences[name]; return Number(BigInt(old.lastValue) + (old.isCalled ? 1n : 0n)); };
  const deviceNext = firstNext('devices_id_seq'), activityNext = firstNext('activity_entries_id_seq');
  const nextSessions = mapped.after.tables.sessions.filter(row => !mapped.before.tables.sessions.some(old => old.id === row.id));
  for (const row of nextSessions) { assert(!after.tables.sessions.some(old => old.id === row.id)); row.device_registry_id += deviceNext - 1; }
  after.tables.sessions.push(...nextSessions);
  after.tables.devices.push(...mapped.after.tables.devices.map(row => ({ ...row, id: row.id + deviceNext - 1 })));
  after.tables.activity_entries.push(...mapped.after.tables.activity_entries.map(row => ({ ...row, id: row.id + activityNext - 1 })));
  const retired = after.tables.play_sessions.find(row => row.id === closer.RETAINED_PLAY), expected = mapped.after.tables.play_sessions.find(row => row.id === closer.RETAINED_PLAY);
  for (const key of ['state', 'stopped_at', 'updated_at']) retired[key] = expected[key];
  after.tables.play_sessions.push(...mapped.after.tables.play_sessions.filter(row => row.id !== closer.RETAINED_PLAY));
  const data = after.tables.user_item_data.find(row => row.user_id === seed.actors.movie.id && row.item_id === seed.catalog.movie.id);
  const mappedData = mapped.after.tables.user_item_data.find(row => row.user_id === seed.actors.movie.id);
  for (const key of ['playback_position_ticks', 'play_count', 'played', 'last_played_at', 'updated_at']) data[key] = mappedData[key];
  after.capturedAt = mapped.after.capturedAt;
  after.sequences.devices_id_seq = { lastValue: String(deviceNext + 1), isCalled: true };
  after.sequences.activity_entries_id_seq = { lastValue: String(activityNext + 3), isCalled: true };
  const currentManifest = { ...mapped.manifest, actor: seed.actors.movie, catalog: seed.catalog }, retained = { closeout, snapshot: saved };
  const unchanged = sha(jsonBytes(saved));
  const proof = closer.verifyDurable(saved, after, currentManifest, seed, mapped.physical, retained);
  assert.equal(proof.countedPlays, 2); assert.equal(proof.selectedUserdata.play_count, 2);
  assert.equal(proof.retainedBaselineExpiration.playSessionId, closer.RETAINED_PLAY);
  assert.equal(sha(jsonBytes(saved)), unchanged);
  for (const mutate of [value => { value.tables.play_sessions.find(row => row.id === closer.RETAINED_PLAY).counted = true; },
    value => { value.tables.play_sessions = value.tables.play_sessions.filter(row => row.id !== closer.RETAINED_PLAY); },
    value => { value.tables.sessions.find(row => row.id === closer.RETAINED_AUTH).revoked_at = null; }]) {
    const changed = clone(after); mutate(changed);
    assert.throws(() => closer.verifyDurable(saved, changed, currentManifest, seed, mapped.physical, retained));
  }
  return pins;
}

async function main() {
  const args = process.argv.slice(2);
  const replay = args.length === 5 && args[4] === '--replay-retained-snapshot';
  assert((args.length === 4 || replay) && args[0] === '--source' && args[2] === '--output', 'arguments_required');
  assert(process.platform === 'linux' && process.getuid?.() === 0, 'remote_linux_root_required');
  const source = path.resolve(args[1]), output = args[3], ownPath = fileURLToPath(import.meta.url);
  await protectedOutput(output); process.umask(0o077);
  const adapter = path.join(path.dirname(source), 'client-browser-audited-candidate.mjs');
  const sources = Object.fromEntries(await Promise.all([source, ownPath, adapter].map(async filename => [filename, sha(await fs.readFile(filename))])));
  const closer = await import(pathToFileURL(source).href);
  const tests = [];
  for (const [name, run] of guardCases(closer)) {
    try { run(); tests.push({ name, outcome: 'passed' }); }
    catch (error) { tests.push({ name, outcome: 'failed', errorType: error.name,
      failedCheck: /^[a-z0-9_]+$/.test(error.message ?? '') ? error.message : 'pure_guard_assertion_failed' }); }
  }
  let replayedPins = [];
  if (replay) {
    try { replayedPins = await savedRetainedReplay(closer); tests.push({ name: 'saved_movie04_complete_durable_comparison_with_synthetic_new_play', outcome: 'passed' }); }
    catch (error) { tests.push({ name: 'saved_movie04_complete_durable_comparison_with_synthetic_new_play', outcome: 'failed', errorType: error.name,
      failedCheck: /^[a-z0-9_]+$/.test(error.message ?? '') ? error.message : 'saved_replay_assertion_failed' }); }
  }
  const unchanged = (await Promise.all(Object.entries(sources).map(async ([filename, digest]) => sha(await fs.readFile(filename)) === digest))).every(Boolean);
  const report = { kind: 'audited-candidate-client-closeout-pure-guards', version: 1,
    scope: 'Exported pure functions on synthetic fixtures; optional saved movie04 baseline plus simulated new rows. No live or client acceptance claim.', replayedPins,
    source: { path: source, sha256: sources[source] }, sources, output, testCount: tests.length,
    passed: tests.filter(row => row.outcome === 'passed').length, failed: tests.filter(row => row.outcome !== 'passed').length,
    sourceUnchanged: unchanged, tests, clientAcceptanceClaim: false, endToEndExecuted: false,
    httpCalls: 0, sqlCalls: 0, serviceCalls: 0 };
  const bytes = Buffer.from(JSON.stringify(report, null, 2) + '\n');
  const handle = await fs.open(output, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
  try { await handle.writeFile(bytes); await handle.sync(); } finally { await handle.close(); }
  const directory = await fs.open(path.dirname(output), constants.O_RDONLY | constants.O_DIRECTORY);
  try { await directory.sync(); } finally { await directory.close(); }
  process.stdout.write(JSON.stringify({ path: output, sha256: sha(bytes), testCount: report.testCount,
    passed: report.passed, failed: report.failed, sourceUnchanged: unchanged, clientAcceptanceClaim: false }) + '\n');
  if (tests.length !== (replay ? 20 : 19) || report.failed || !unchanged) process.exitCode = 1;
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  try { await main(); } catch { process.stderr.write('pure_closeout_guard_setup_failed\n'); process.exitCode = 1; }
}
