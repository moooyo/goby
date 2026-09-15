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
const RETAINED_ROOT = '/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14';
const ACTOR = 'a'.repeat(32), CONTROL = 'b'.repeat(32), ITEM = 'c'.repeat(32);
const TOKEN = 'synthetic-owned-token-with-no-live-authority';
const sha = bytes => createHash('sha256').update(bytes).digest('hex');
const jsonBytes = value => Buffer.from(JSON.stringify(value));
const clone = value => structuredClone(value);
const manifest = { browserOrigin: ORIGIN, actor: { id: ACTOR, username: 'synthetic-actor' }, scenario: 'mp3',
  catalog: { mp3: { id: ITEM, type: 'Audio', container: 'mp3', runtimeTicks: 6000000000 } } };
const TABLES = 'activity_entries application_key_clients application_key_devices application_keys catalog_entities client_playback_references devices encoding_jobs extra_reserved_paths item_entities item_extra_resources item_images item_metadata_state item_subtitles item_theme_resources items libraries library_roots managed_settings play_sessions scan_jobs schema_migrations server_settings sessions task_definitions task_occurrences task_run_children task_run_requests task_runs task_triggers theme_owner_ids theme_reserved_paths user_item_data user_settings users'.split(' ');

function protocolFixture(closer, body, { route = '/Sessions/Playing', type = 'text/plain;charset=UTF-8', method = 'POST', kind = 'playback_report', encoding = null } = {}) {
  const target = '/emby' + route, bytes = Buffer.isBuffer(body) ? body : Buffer.from(body);
  const original = closer.parseHead(Buffer.from([method + ' ' + target + ' HTTP/1.1', 'Host: 127.0.0.1:19180',
    'Content-Type: ' + type, ...(encoding === null ? [] : ['Content-Encoding: ' + encoding]), 'Content-Length: ' + bytes.length, '', ''].join('\r\n')));
  return { complete: true, result: { bodyEvidenceComplete: true }, request: { kind: 'api' }, original,
    url: new URL(target, ORIGIN), scope: { allowed: true, kind, route }, requestEntity: bytes };
}

function observedBodyBytes(body, overrides = {}) {
  const bytes = Buffer.isBuffer(body) ? body : Buffer.from(body);
  const row = { ordinal: 1, method: 'POST', url: ORIGIN + '/emby/Sessions/Playing', allowed: true, origin: 'target', scope: 'frame',
    main_frame: true, kind: 'playback_report', route: '/Sessions/Playing', headers: { 'content-type': 'text/plain;charset=UTF-8' },
    payload_base64: bytes.toString('base64'), body: '__RAW_PROTOCOL_BODY__', ...overrides };
  return Buffer.from(JSON.stringify({ requests: [row] }).replace('"__RAW_PROTOCOL_BODY__"', bytes.toString('utf8')));
}

function responseHead(status, headers) {
  return Buffer.from(['HTTP/1.1 ' + status, ...headers.map(([key, value]) => key + ': ' + value), '', ''].join('\r\n'), 'latin1');
}

function chunked(bytes) {
  const middle = Math.max(1, Math.floor(bytes.length / 2));
  return Buffer.concat([bytes.subarray(0, middle), bytes.subarray(middle)].filter(piece => piece.length).flatMap((piece, index) => [
    Buffer.from(piece.length.toString(16) + (index === 0 ? ';synthetic=yes' : '') + '\r\n'), piece, Buffer.from('\r\n'),
  ]).concat([Buffer.from('0\r\nX-Synthetic-Trailer: retained\r\n\r\n')]));
}

function exchangeFixture({ media = false, method = 'GET', complete = !media, delivered = media ? (complete ? 1000 : 128) : 12, target: suppliedTarget,
  contentRange = 'bytes 0-999/4096', responseHeaders = [] } = {}) {
  const target = suppliedTarget ?? (media ? '/emby/Audio/' + ITEM + '/stream.mp3?PlaySessionId=play_synthetic' : '/emby/System/Info');
  const rawHead = Buffer.from([method + ' ' + target + ' HTTP/1.1', 'Host: 127.0.0.1:19180',
    'X-Emby-Token: ' + TOKEN, ...(media ? ['Range: bytes=0-999'] : ['User-Agent: GobyBrowserPostLogoutVerification/1']),
    'Connection: close', '', ''].join('\r\n'));
  const head = responseHead(media ? '206 Partial Content' : '401 Unauthorized', [
    ['Content-Length', media ? '1000' : '12'], ['Content-Type', media ? 'audio/mpeg' : 'text/plain'],
    ...(media ? [['Content-Range', contentRange]] : []), ['Connection', 'close'], ...responseHeaders,
  ]);
  const request = { kind: media ? 'media' : 'api', method, origin: ORIGIN, target, path: target.split('?')[0],
    parentOrdinal: null, budgetClass: media ? 'normal' : 'cleanup', rawRequestHeadBase64: rawHead.toString('base64'),
    forwardedRequestHeadBase64: rawHead.toString('base64') };
  const intent = { ordinal: 7, budgetClass: request.budgetClass, request, startedMonotonicNs: '10000000000' };
  const result = { ordinal: 7, budgetClass: request.budgetClass, request: clone(request), backend: 'goby',
    outcome: complete ? 'observed' : 'rejected_or_interrupted', upstreamConnected: true,
    responseStatus: media ? 206 : 401, responseHeadBase64: head.toString('base64'),
    responseHeaders: head.subarray(0, -4).toString('latin1').split('\r\n').slice(1).map(line => {
      const offset = line.indexOf(':'); return [line.slice(0, offset), line.slice(offset + 1).trim()];
    }), interimHeadsBase64: [], completeHTTP: complete, requestBodyComplete: true,
    requestBodyWireBytes: 0, responseBodyWireBytes: delivered, upstreamBytesWritten: rawHead.length,
    clientBytesWritten: head.length + delivered, webSocketClientBytes: 0, webSocketUpstreamBytes: 0,
    requestForwardedComplete: complete, responseForwardedComplete: complete,
    requestBodyRetained: false, responseBodyRetained: false, requestBodyBase64: null, responseBodyBase64: null,
    requestBodyTruncated: false, responseBodyTruncated: false, bodyEvidenceComplete: false,
    bodyStorage: 'http-transfer-wire', webMediaAndWebSocketBodiesRetained: false, completedMonotonicNs: '12000000000' };
  return { intent, result, rawHead, head };
}

function partialFixture(closer, options = {}) {
  const fixture = exchangeFixture({ media: true, ...options });
  const exchange = closer.verifyExchange(fixture.intent, fixture.result, manifest);
  const row = { method: 'GET', kind: 'media', origin: 'target', allowed: true, status: 206, url: exchange.url.href, token_sha256: exchange.tokenHash, elapsed_ms: 1000,
    failed_elapsed_ms: 3000, failed: true, failure_error_text: 'net::ERR_ABORTED', headers: { range: 'bytes=0-999' }, payload_base64: '' };
  const report = { started_monotonic_ns: '9000000000', requests: [
    { ...clone(row), ordinal: 101, scope: 'frame', main_frame: true },
    { ...clone(row), ordinal: 102, scope: 'service_worker', elapsed_ms: 1001, failed_elapsed_ms: 3001 },
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

export function retainedFixture(closer) {
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

function reviewedFixture(closer) {
  const value = retainedFixture(closer), pin = name => ({ path: '/opt/goby-test/synthetic-reviewed/' + name, sha256: sha(name) });
  const actor = value.seed.actors.movie, movie = value.seed.catalog.movie;
  const terminal = { ...clone(value.before.tables.play_sessions[0]), id: 'play_preserved_terminal', state: 'Expired', stopped_at: '2025-12-31T23:01:00Z' };
  for (const state of [value.before, value.after]) state.tables.play_sessions.push(clone(terminal));
  value.seed.runtimeEpoch = pin('current-epoch.json'); value.seed.serverId = 'd'.repeat(32);
  const source = { archiveSha256: sha('archive'), sourceManifest: pin('source-manifest.json'), binary: pin('goby'), fullReport: pin('full-report.json'), schema: 28 };
  const manifest = { ...clone(value.manifest), kind: 'audited-candidate-client-input', version: 1, runId: 'movie-99', serverId: value.seed.serverId,
    source: { manifestSha256: source.sourceManifest.sha256, binarySha256: source.binary.sha256, schema: 28 } };
  const review = { kind: 'audited-candidate-reviewed-movie-baseline', version: 1, status: 'reviewed_closed_state', scenario: 'movie',
    closeout: pin('closed-state.json'), snapshot: pin('source-after.json'), sourceEpoch: pin('historical-epoch.json'), runtimeEpoch: value.seed.runtimeEpoch,
    seedBinding: pin('current-seed.json'), manifest: pin('adapter-input.json'), actor: { id: actor.id, username: actor.username },
    item: { id: movie.id, mediaSourceId: 'mediasource_' + movie.id, runtimeTicks: movie.runtimeTicks },
    preparedExpirations: [{ playSessionId: closer.RETAINED_PLAY, authSessionId: closer.RETAINED_AUTH }] };
  const closeout = { kind: 'audited-movie99-owned-state-closeout', status: 'owned_state_closed_client_acceptance_pending', clientAcceptance: false,
    browserOutcome: 'failed', browserExitCode: 1, gatewayExitCode: 0, inputEvidence: { after: review.snapshot, epoch: review.sourceEpoch, manifest: review.manifest },
    checks: { ownedData: { ownedTables: 35, oldRowsDeleted: 0, clientPlaybackReferences: 0, encodingJobs: 0,
      userData: clone(value.before.tables.user_item_data.find(row => row.user_id === actor.id)) }, authentication: { allSessionsRevoked: value.before.tables.sessions.length },
      savedRuntimeAndWorkers: { candidateContinuous: true, postgresContinuous: true, leaseExact: true,
        workers: Object.fromEntries(['browser', 'gateway'].map(role => [role, { pidAbsentAsRecorded: true, recursiveCgroupEmptyAsRecorded: true }])) } } };
  review.boundary = null;
  review.movieProvenance = Object.fromEntries(['closeout', 'snapshot', 'sourceEpoch', 'manifest'].map(key => [key, clone(review[key])]));
  value.retained = { review, closeout, snapshot: clone(value.before), sourceEpoch: { kind: 'audited-candidate-runtime-epoch', version: 2, currentSource: source },
    manifest, boundary: null, inputBinding: { runtimeEpoch: review.runtimeEpoch, seedBinding: review.seedBinding } };
  value.retained.movieProvenance = Object.fromEntries(['closeout', 'snapshot', 'sourceEpoch', 'manifest'].map(key => [key, clone(value.retained[key])]));
  for (const login of value.physical.logins) for (const info of login.infos) {
    info.exchange.scope.allowed = true; info.exchange.original = { method: 'POST' }; info.body = { UserId: actor.id };
  }
  return value;
}

function reviewedLatestFixture(closer) {
  const value = reviewedFixture(closer), pin = name => ({ path: '/opt/goby-test/synthetic-reviewed/latest-' + name, sha256: sha('latest-' + name) });
  const subtitleActor = { id: 'e'.repeat(32), username: 'synthetic-subtitles' }, review = value.retained.review;
  value.seed.actors.subtitles = subtitleActor;
  for (const state of [value.before, value.after]) {
    state.tables.users.push({ id: subtitleActor.id, name: subtitleActor.username, is_administrator: false, is_disabled: false, policy: {} });
    state.tables.sessions.push({ id: 'foreign_subtitle_session', user_id: subtitleActor.id, revoked_at: '2025-12-31T23:30:00Z' });
    for (const suffix of ['a', 'b']) {
      state.tables.play_sessions.push({ id: 'play_foreign_audio_' + suffix, user_id: CONTROL, state: 'Stopped', counted: true });
      state.tables.client_playback_references.push({ user_id: CONTROL, auth_session_id: 'old-1', play_session_id: 'play_foreign_audio_' + suffix,
        device_id: 'foreign-audio-device', application_client_id: null, client_nonce: 'foreign-' + suffix });
    }
  }
  value.retained.snapshot = clone(value.before);
  review.closeout = pin('closed-state.json'); review.snapshot = pin('source-after.json'); review.sourceEpoch = review.runtimeEpoch;
  review.manifest = pin('adapter-input.json'); review.boundary = pin('boundary.json');
  const process = { pid: 100, bootId: 'synthetic-boot', startTicks: '100' };
  const sourceEpoch = { kind: 'audited-candidate-runtime-epoch', version: 3,
    currentSource: { archiveSha256: sha('latest-archive'), sourceManifest: pin('source-manifest.json'), binary: pin('goby'), fullReport: pin('full-report.json'), schema: 28 },
    candidateProcess: process, postgresProcess: { ...process, pid: 200 }, lease: { owned: true }, candidate: { database: 'synthetic' } };
  const candidate = { ...process, listener: { port: 19181, socketInode: '123' } };
  const manifest = { ...clone(value.retained.manifest), scenario: 'subtitles', runId: 'subtitles-01', actor: subtitleActor, processes: { candidate },
    source: { manifestSha256: sourceEpoch.currentSource.sourceManifest.sha256, binarySha256: sourceEpoch.currentSource.binary.sha256, schema: 28 } };
  const input = { kind: 'audited-candidate-client-closeout-input', version: 3, output: '/opt/goby-test/synthetic-reviewed/closeout', sources: {}, retainedBaseline: null, serverLog: pin('server-log.json') };
  for (const name of ['manifest', 'observation', 'summary', 'gatewayAttestation', 'gatewayIndex', 'runtimeEpoch', 'admission', 'seedBinding', 'sourceBefore', 'sourceAfter', 'boundary']) input[name] = pin(name + '.json');
  Object.assign(input, { manifest: review.manifest, runtimeEpoch: review.sourceEpoch, seedBinding: review.seedBinding, sourceAfter: review.snapshot, boundary: review.boundary });
  for (const name of ['closer', 'adapter', 'gateway', 'proxy', 'sessionProof', 'movie', 'audio', 'subtitles', 'tv']) input.sources[name] = pin(name + '.mjs');
  const boundary = { kind: 'audited-candidate-client-boundary', version: 1, runId: manifest.runId, runtimeEpoch: review.sourceEpoch,
    sourceBefore: input.sourceBefore, sourceAfter: review.snapshot, candidateBefore: candidate, candidateAfter: clone(candidate),
    postgresBefore: sourceEpoch.postgresProcess, postgresAfter: clone(sourceEpoch.postgresProcess), leaseBefore: sourceEpoch.lease, leaseAfter: clone(sourceEpoch.lease),
    database: sourceEpoch.candidate.database, beforeMonotonicNs: '100', afterMonotonicNs: '200',
    clientWorker: { exitCode: 0, mainPID: 0, workerPidAbsent: true, remainingBrowserPids: [] },
    gatewayWorker: { exitCode: 0, mainPID: 0, workerPidAbsent: true, index: input.gatewayIndex } };
  const tables = value.before.tables, closeout = { kind: 'audited-candidate-subtitles-owned-state-closeout', status: 'owned_state_closed_client_acceptance_pending',
    clientAcceptance: false, sourcePinsUnchanged: true, originalUIRejection: { originalObservationUnchanged: true }, inputEvidence: input, sourceState: { allSessionsRevoked: true,
      afterSessions: tables.sessions.length, afterPlays: tables.play_sessions.length, afterUserData: tables.user_item_data.length,
      afterClientReferences: tables.client_playback_references.length, afterEncodingJobs: tables.encoding_jobs.length } };
  Object.assign(value.retained, { sourceEpoch, manifest, closeout, boundary });
  return value;
}

function reviewedTVFixture(closer) {
  const wrapper = reviewedLatestFixture(closer), value = durableFixture(), pin = name => ({ path: '/opt/goby-test/synthetic-tv/' + name, sha256: sha(name) });
  const episodeId = 'f'.repeat(32), actor = { id: ACTOR, username: 'synthetic-actor' }, seed = clone(wrapper.seed);
  seed.actors.movie = { id: 'd'.repeat(32), username: 'foreign-movie' }; seed.actors['tv-browse'] = actor;
  Object.assign(seed.catalog, { tvLibrary: { id: '1'.repeat(32) }, series: { id: '2'.repeat(32) }, seasons: [{ id: '3'.repeat(32) }, { id: '4'.repeat(32) }],
    episodes: [{ id: episodeId, type: 'Episode', indexNumber: 1, parentIndexNumber: 2, runtimeTicks: 6000000000 }] });
  for (const state of [value.before, value.after]) {
    for (const user of state.tables.users) Object.assign(user, { is_administrator: false, is_disabled: false, policy: {} });
    state.tables.users.push({ id: seed.actors.movie.id, name: seed.actors.movie.username, is_administrator: false, is_disabled: false, policy: {} },
      { id: seed.actors.subtitles.id, name: seed.actors.subtitles.username, is_administrator: false, is_disabled: false, policy: {} });
  }
  const oldSession = { id: 'retained_tv_auth', user_id: ACTOR, kind: 'emby', device_id: 'old-tv-device', revoked_at: '2025-12-31T23:30:00Z' };
  const old = { ...clone(value.after.tables.play_sessions[0]), id: 'play_retained_tv', auth_session_id: oldSession.id, device_id: oldSession.device_id,
    item_id: episodeId, media_source_id: 'mediasource_' + episodeId, state: 'Prepared', counted: false, position_ticks: 0, started_at: null, stopped_at: null,
    created_at: '2025-12-31T23:00:00Z', updated_at: '2025-12-31T23:00:00Z', expires_at: '2025-12-31T23:30:00Z', client_correlated: false };
  const userdata = { user_id: ACTOR, item_id: episodeId, playback_position_ticks: 0, play_count: 0, is_favorite: false, played: false,
    last_played_at: null, updated_at: old.created_at };
  value.before.tables.sessions.push(clone(oldSession)); value.after.tables.sessions.unshift(clone(oldSession));
  value.before.tables.play_sessions.push(clone(old));
  value.before.tables.user_item_data.push(clone(userdata));
  value.after.tables.user_item_data = value.after.tables.user_item_data.filter(row => row.user_id !== ACTOR).concat([clone(userdata)]);
  Object.assign(value.after.tables.play_sessions[0], { item_id: episodeId, media_source_id: 'mediasource_' + episodeId, state: 'Prepared', counted: false,
    position_ticks: 0, started_at: null, stopped_at: null, updated_at: '2026-01-01T00:00:02.000000000Z', expires_at: '2026-01-01T00:30:02Z', client_correlated: false });
  value.after.tables.play_sessions.unshift({ ...clone(old), state: 'Expired', stopped_at: '2026-01-01T00:00:01Z', updated_at: '2026-01-01T00:00:01.000001Z' });
  const provenance = { snapshot: clone(value.before), sourceEpoch: clone(wrapper.retained.movieProvenance.sourceEpoch),
    manifest: { ...clone(wrapper.retained.movieProvenance.manifest), scenario: 'tv-browse', runId: 'tv-browse-99', actor, catalog: clone(seed.catalog) } };
  const review = { ...clone(wrapper.retained.review), kind: 'audited-candidate-reviewed-tv-baseline', scenario: 'tv-browse', actor,
    item: { id: episodeId, mediaSourceId: old.media_source_id, runtimeTicks: old.duration_ticks },
    postBrowseAdmission: clone(closer.TV_POST_BROWSE_ADMISSION),
    preparedExpirations: [{ playSessionId: old.id, authSessionId: oldSession.id }],
    tvProvenance: Object.fromEntries(['closeout', 'snapshot', 'sourceEpoch', 'manifest'].map(key => [key, pin('tv-' + key + '.json')])) };
  delete review.movieProvenance;
  provenance.closeout = { kind: 'audited-tv-browse99-owned-state-closeout', runId: 'tv-browse-99', status: 'owned_state_closed_client_acceptance_pending',
    clientAcceptance: false, browserOutcome: 'failed', browserExitCode: 1, gatewayExitCode: 0, physicalMediaRequests: 0, playingReports: 0,
    inputEvidence: { after: review.tvProvenance.snapshot, epoch: review.tvProvenance.sourceEpoch, manifest: review.tvProvenance.manifest },
    checks: { authentication: { allSessionsRevoked: provenance.snapshot.tables.sessions.length },
      savedRuntimeAndWorkers: clone(wrapper.retained.movieProvenance.closeout.checks.savedRuntimeAndWorkers), ownedData: { ownedTables: 35, oldRowsDeleted: 0,
        newCountedPlays: 0, clientPlaybackReferences: 0, encodingJobs: 0, newUserData: clone(userdata),
        unstartedPreparations: [{ id: old.id, authSessionId: oldSession.id, itemId: episodeId, state: 'Prepared', counted: false, positionTicks: 0, prepareOrdinals: [277] }] } } };
  for (const state of [value.before, value.after]) {
    state.tables.sessions.push({ id: '9'.repeat(32), user_id: ACTOR, kind: 'emby', token_hash: '\\x' + sha('post-browse-token'), revoked_at: '2025-12-31T23:45:00Z' });
    state.tables.play_sessions.push(...clone(wrapper.before.tables.play_sessions.filter(row => row.user_id === CONTROL)));
    state.tables.client_playback_references = clone(wrapper.before.tables.client_playback_references);
  }
  value.retained = { ...clone(wrapper.retained), review, tvProvenance: provenance, snapshot: clone(value.before) }; delete value.retained.movieProvenance;
  value.retained.postBrowseAdmission = { kind: 'audited-candidate-live-admission', version: 3, status: 'admitted_for_core_client', candidateAdmissionComplete: true,
    failure: null, cleanupFailures: [], runtimeEpoch: review.runtimeEpoch, seedRuntimeBinding: review.seedBinding,
    controllerSessions: { P: { credentialId: '9'.repeat(32), tokenSha256: sha('post-browse-token'), sameTokenRejected: true } } };
  value.retained.manifest.catalog = clone(seed.catalog);
  Object.assign(value.retained.closeout.sourceState, { afterSessions: value.before.tables.sessions.length, afterPlays: value.before.tables.play_sessions.length,
    afterUserData: value.before.tables.user_item_data.length, afterClientReferences: 2, afterEncodingJobs: 0 });
  const login = value.physical.logins[0], info = { itemId: episodeId, body: { UserId: ACTOR }, value: { PlaySessionId: 'play_synthetic' },
    exchange: { ordinal: 1, tokenHash: login.tokenHash, scope: { allowed: true, kind: 'playback_info', route: '/Items/' + episodeId + '/PlaybackInfo' },
      original: { method: 'POST' }, complete: true, response: { status: 200 }, intent: { startedAt: '2026-01-01T00:00:01Z' }, result: { completedAt: '2026-01-01T00:00:02Z' } } };
  Object.assign(login, { infos: [info], chains: [], media: [] }); value.physical.chains = []; value.physical.exchanges = [info.exchange];
  value.seed = seed; value.manifest = { scenario: 'tv-browse', actor, catalog: clone(seed.catalog) };
  return value;
}

function nativeTVFixture(closer) {
  const value = reviewedTVFixture(closer), id = '1'.repeat(32), url = new URL(ORIGIN + '/emby/LiveTv/Programs');
  const exchange = { ordinal: 2, original: closer.parseHead(Buffer.from('GET /emby/LiveTv/Programs HTTP/1.1\r\nHost: 127.0.0.1:19180\r\n\r\n')),
    response: closer.parseHead(responseHead('404 Not Found', [['X-Request-Id', id]]), true), request: { kind: 'api', path: url.pathname }, url,
    scope: { allowed: true, kind: 'read', route: '/LiveTv/Programs' }, tokenHash: value.physical.logins[0].tokenHash, complete: true,
    intent: { startedMonotonicNs: '10000000000' }, result: { backend: 'goby' }, requestEntity: null };
  const row = { ordinal: 101, scope: 'frame', method: 'GET', url: url.href, token_sha256: exchange.tokenHash, elapsed_ms: 1000,
    allowed: true, origin: 'target', kind: 'read', headers: {}, response_headers: { 'X-Request-Id': id }, status: 404 };
  value.physical.exchanges.push(exchange);
  value.report = { outcome: 'scenario_completed', failure: null, requests: [row], page_errors: [{ kind: 'object' }], started_monotonic_ns: '9000000000', elapsed_ms: 3000,
    cleanup: { browser_closed: true, tokens_rejected: true, media_stopped: true }, observer: { pending: [], drain_timeouts: [] },
    browser_environment: { service_workers: 'allow', proxy_bypass: '<-loopback>', quic: 'disabled', nonproxied_webrtc_udp: 'disabled' },
    tv_browse: { outcome: 'tv_browse_ui_flow_completed' }, native_rejection_diagnostics: { diagnostics_complete: true },
    native_rejections: [{ id: 'native-rejection-1', elapsed_ms: 1200, diagnostic_state: 'captured_redacted', association: { state: 'matched', request_ordinal: 101 },
      reason: { kind: 'response', request_id: id, status: 404, location: { origin: ORIGIN, path: url.pathname } } }] };
  return value;
}

function currentRuntimeFixture() {
  const pin = name => ({ path: '/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/synthetic-current/' + name, sha256: sha(name) });
  const old = { pid: 100, startTicks: '1000', bootId: 'synthetic-boot' }, postgres = { ...old, pid: 200 };
  const epoch = { version: 3, candidateProcess: old, postgresProcess: postgres, candidate: { binary: pin('goby'), runtime: pin('runtime.env'), units: { server: pin('server.service') },
    processes: { server: { MainPID: '100', InvocationID: '1'.repeat(32), ActiveState: 'active' }, postgres: { MainPID: '200' } },
    serverIdentity: { pid: 100, startTicks: '1000', invocationId: '1'.repeat(32) }, listener: { pid: 100, port: 19181, socketInode: '1' }, ports: { postgres: 55432 } },
    lease: { backendPid: 300, backendStart: '2026-01-01T00:00:00Z', user: 'synthetic', database: 'synthetic', application: 'goby', clientHost: '127.0.0.1',
      clientPort: 50000, mode: 'ExclusiveLock', granted: true, candidateConnection: { pid: 100, localPort: 50000, remotePort: 55432, socketInode: '3' } } };
  const current = { candidateProcess: { ...old, pid: 101, startTicks: '2000' }, postgresProcess: clone(postgres), postgresProperties: clone(epoch.candidate.processes.postgres),
    serverProperties: { ...epoch.candidate.processes.server, MainPID: '101', InvocationID: '2'.repeat(32) },
    serverIdentity: { pid: 101, startTicks: '2000', invocationId: '2'.repeat(32) }, listener: { pid: 101, port: 19181, socketInode: '2' },
    lease: { ...clone(epoch.lease), backendPid: 301, backendStart: '2026-01-01T00:00:01Z', clientPort: 50001,
      candidateConnection: { pid: 101, localPort: 50001, remotePort: 55432, socketInode: '4' } } };
  const envelope = { kind: 'audited-candidate-current-runtime-binding', version: 1, status: 'reviewed_current_runtime',
    ...Object.fromEntries(['runtimeEpoch', 'seedBinding', 'admission', 'admissionCloseout', 'hosting'].map(key => [key, pin(key + '.json')])),
    recovery: Object.fromEntries(['execution', 'independentReview', 'configuration', 'selectedStartIntent', 'selectedResult'].map(key => [key, pin(key + '.json')])),
    current, preserved: Object.fromEntries(['binary', 'runtime', 'units'].map(key => [key, clone(epoch.candidate[key])])),
    observation: pin('observation.json'), observationReview: pin('observation-review.json') };
  const input = { version: 5, currentRuntime: pin('current-runtime.json'), runtimeEpoch: envelope.runtimeEpoch, seedBinding: envelope.seedBinding, admission: envelope.admission };
  const authorizedInput = { ...clone(input), scenario: 'tv-browse', admissionCloseout: envelope.admissionCloseout, hosting: envelope.hosting };
  const hostFields = 'Id LoadState ActiveState SubState MainPID InvocationID Result ExecMainStatus ControlGroup NRestarts'.split(' ');
  const hosting = { process: { pid: 400 }, listener: { port: 28497 }, unitProperties: Object.fromEntries(hostFields.map(key => [key, 'synthetic'])) };
  const hostIdentity = { process: hosting.process, listener: hosting.listener, unit: hosting.unitProperties };
  const calls = { sql: 0, http: 0, browser: 0, service: 0, seed: 0, admission: 0, provision: 0 };
  const observation = { kind: 'audited-candidate-current-runtime-observation', version: 1, runtimeEpoch: envelope.runtimeEpoch, recoveryExecution: envelope.recovery.execution,
    hosting: envelope.hosting, source: pin('observe.py'), verification: pin('verification.json'), capturedAt: '2026-01-01T00:00:02Z', before: clone(current), after: clone(current),
    preserved: clone(envelope.preserved), hostingBefore: clone(hostIdentity), hostingAfter: clone(hostIdentity), leaseQueryResult: pin('lease.json'), calls: { ...calls, sql: 2 } };
  const observationReview = { kind: 'audited-candidate-current-runtime-observation-review', version: 1, status: 'passed', observation: envelope.observation,
    source: observation.source, verification: observation.verification, calls, checks: Object.fromEntries('recordPins sourceAndVerification currentIdentity recoveryBinding preservedHashes hostingContinuity uniqueLease privateEvidence'.split(' ').map(key => [key, true])) };
  const leaseRow = clone(current.lease); delete leaseRow.candidateConnection;
  return { envelope, input, epoch, authorizedInput, records: { hosting, observation, observationReview, leaseQueryResult: [leaseRow] } };
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

function binarySuccessorFixture(closer) {
  // This is a synthetic, in-memory envelope. Fixed descriptors are copied only
  // to exercise the contract; the fixture does not claim their contents or hashes.
  const prior = runtimeLineageFixture(), authority = closer.BINARY_SUCCESSOR;
  const pin = name => ({ path: '/opt/goby-test/synthetic-successor/' + name, sha256: sha('synthetic-successor-' + name) });
  const id = index => index.toString(16).padStart(32, '0');
  prior.epoch.previousEpoch = clone(closer.PREVIOUS_BINARY_EPOCH);
  prior.lineage.previousBinding.runtimeEpoch = clone(closer.PREVIOUS_BINARY_EPOCH);
  prior.lineage.configurationInput.previousEpoch = clone(closer.PREVIOUS_BINARY_EPOCH);
  prior.lineage.failedAdmission03.runtimeEpoch = clone(closer.PREVIOUS_BINARY_EPOCH);
  prior.seed.runtimeEpoch = clone(authority.previousEpoch);
  const helpers = Object.fromEntries(['seed', 'provision', 'gateway', 'admission', 'reconcile'].map(name => [name, pin(name + '.py')]));
  for (const epoch of [prior.lineage.previousEpoch, prior.epoch]) {
    epoch.helpers = clone(helpers);
    epoch.currentSource.binary.path = '/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b/install/goby';
    epoch.candidate.binary = clone(epoch.currentSource.binary);
    epoch.candidate.backendReport = clone(epoch.currentSource.fullReport);
    epoch.candidate.originalProvision = clone(epoch.originalProvision);
    epoch.candidate.seedProvenance = clone(epoch.seedProvenance);
    delete epoch.candidate.database;
    epoch.candidate.databases = { source: { name: 'synthetic-source', owner: 'synthetic-source-owner', afterStart: { tables: 28 } },
      recovery: { name: 'synthetic-recovery', owner: 'synthetic-recovery-owner', afterStart: { tables: 0 } } };
    Object.assign(epoch.candidateProcess, { uid: 0, exe: epoch.currentSource.binary.path,
      cmdline: [epoch.currentSource.binary.path], networkNamespace: 'net:[synthetic]', cgroup: '/synthetic-candidate.service' });
    epoch.candidate.processes = { postgres: clone(epoch.postgresProcess), server: { Id: 'synthetic-candidate.service', MainPID: String(epoch.candidateProcess.pid) } };
  }
  prior.lineage.previousBinding.currentSessions.forEach((row, index) => { row.credentialId = index < 2 ? null : id(index + 1); });
  ['admin', 'P', 'Q'].forEach((role, index) => { prior.lineage.failedAdmission03.controllerSessions[role].credentialId = id(index + 4); });
  prior.seed.currentSessions = [...clone(prior.lineage.previousBinding.currentSessions), ...['admin', 'P', 'Q'].map(role => ({
    kind: role === 'admin' ? 'admin' : 'emby', tokenSha256: prior.lineage.failedAdmission03.controllerSessions[role].tokenSha256,
    credentialId: prior.lineage.failedAdmission03.controllerSessions[role].credentialId }))];
  const priorSource = { capturedAt: '2026-09-13T14:00:00.000000Z', tables: Object.fromEntries(TABLES.map(name => [name, []])), sequences: {} };
  priorSource.tables.sessions = Array.from({ length: 15 }, (_, index) => {
    const old = prior.seed.currentSessions[index];
    return { id: id(index + 1), kind: old?.kind ?? 'emby', user_id: index % 3 === 0 ? CONTROL : ACTOR,
      token_hash: '\\x' + (old?.tokenSha256 ?? sha('synthetic-successor-token-' + index)), revoked_at: '2026-09-13T13:59:00.000000Z' };
  });
  const inputPin = pin('transition-input.json'), newBinary = { path: authority.fullRoot + '/bin/goby-linux-amd64', sha256: sha('synthetic-successor-binary') };
  const input = { kind: 'audited-candidate-transition-input', version: 2, output: path.posix.dirname(authority.previousEpoch.path).replace('/candidate-backup-limits-revision-01/private', '/candidate-tv-parent-transition-01'),
    ...Object.fromEntries(['previousEpoch', 'previousBinding', 'reviewedSummary', 'reviewedState', 'priorCloseout', 'priorSource'].map(key => [key, clone(authority[key])])),
    newFullReport: { path: authority.fullRoot + '/report.json', sha256: sha('synthetic-successor-full-report') },
    newSourceManifest: clone(authority.sourceManifest), newBinary, compiledCatalog: { path: authority.fullRoot + '/source/internal/backuppg/catalogs/schema-28-postgresql-17.json', sha256: sha('synthetic-successor-catalog') },
    frontendReport: pin('frontend-report.json'), helpers: clone(helpers),
    budgets: { maximumSeconds: 900, stopSeconds: 60, readySeconds: 60, maximumPublicRequests: 10, stopCalls: 1, replaceCalls: 1, startCalls: 1 } };
  const { configurationChange, ...epochBase } = clone(prior.epoch);
  const epoch = { ...epochBase, version: 3, previousEpoch: clone(authority.previousEpoch), operationKind: 'binary_successor',
    transitionInput: inputPin, productInput: inputPin, configurationInput: clone(prior.epoch.transitionInput), transitionHelper: pin('transition.py'), runtimeHelper: pin('runtime.py'),
    reviewedState: clone(authority.reviewedState), reviewedSummary: clone(authority.reviewedSummary),
    currentSource: { archiveSha256: authority.archiveSha256, sourceManifest: clone(authority.sourceManifest),
      binary: { path: prior.epoch.currentSource.binary.path, sha256: newBinary.sha256 }, fullReport: clone(input.newFullReport), schema: 28 },
    before: pin('before.json'), after: pin('after.json'), preservation: pin('preservation.json'), calls: { stop: 1, replace: 1, start: 1 } };
  epoch.candidate.input = clone(inputPin); epoch.candidate.productInput = clone(inputPin);
  epoch.candidate.binary = clone(epoch.currentSource.binary); epoch.candidate.currentSourceManifest = clone(epoch.currentSource.sourceManifest);
  epoch.candidate.backendReport = clone(epoch.currentSource.fullReport);
  epoch.candidateProcess.pid = 2022; epoch.candidateProcess.startTicks = '301';
  epoch.candidate.processes.server.MainPID = '2022'; epoch.candidate.listener.socketInode = '7003';
  epoch.candidate.databases.source.afterStart = { tables: 35 };
  const { previousBinding, admission03, failureCloseout, closedState, ...seedBase } = clone(prior.seed);
  const seed = { ...seedBase, version: 3, runtimeEpoch: pin('epoch-v3.json'),
    ...Object.fromEntries(['previousBinding', 'reviewedSummary', 'reviewedState', 'priorCloseout', 'priorSource'].map(key => [key, clone(authority[key])])),
    currentSessions: priorSource.tables.sessions.map(row => ({ kind: row.kind, credentialId: row.id, tokenSha256: row.token_hash.slice(2), userId: row.user_id, revokedAt: row.revoked_at })) };
  const packages = Array.from({ length: 25 }, (_, index) => 'synthetic/package-' + index);
  const fullWorker = { status: 'passed', mode: 'full', scope: authority.fullRoot,
    cleanup: { only_worker_process_remains: true, owned_postgres_stopped: true, private_bind_removed: true, source_unchanged: true },
    expected_packages: packages, packages: packages.map(name => ({ package: name, result: 'pass', exit_code: 0, failed: 0, skipped: 0 })),
    test_counts: { passed: 25, failed: 0, skipped: 0 }, binary: { path: 'bin/goby-linux-amd64', sha256: newBinary.sha256, bytes: 1024 } };
  const fullReport = { status: 'passed', mode: 'full', scope: authority.fullRoot, archive_sha256: authority.archiveSha256,
    unit_exit_code: 0, recursive_cgroup_empty: true, existing_services_modified: false, worker_report_sha256: sha('synthetic-successor-worker-report'), worker: clone(fullWorker) };
  const reviewedSummary = { kind: 'audited-tv-parent-transition-startup-state-review', status: 'captured_state_supports_bounded_transition_contract',
    state: clone(authority.reviewedState), runtimeEpoch: clone(authority.previousEpoch), seedBinding: clone(authority.previousBinding), priorSource: clone(authority.priorSource),
    source: { ownedTablesExactToTvCloseout: 35, sequencesExact: true } };
  const priorCloseout = { status: 'owned_state_closed_client_acceptance_pending', clientAcceptance: false,
    inputEvidence: { after: clone(authority.priorSource), epoch: clone(authority.previousEpoch) } };
  return { epoch, seed, lineage: { previousEpoch: prior.epoch, previousBinding: prior.seed, previousLineage: prior.lineage,
    transitionInput: input, reviewedSummary, priorCloseout, priorSource, fullReport, fullWorker } };
}

function affectedAdmissionFixture(closer) {
  const value = binarySuccessorFixture(closer);
  const input = { runtimeEpoch: clone(value.seed.runtimeEpoch), seedBinding: { path: '/opt/goby-test/synthetic-successor/binding-v3.json', sha256: sha('synthetic-binding-v3') } };
  const previous = { kind: 'audited-candidate-live-admission', version: 2, status: 'admitted_for_core_client',
    candidateAdmissionComplete: true, clientAcceptance: false, failure: null, cleanupFailures: [],
    runtimeEpoch: clone(closer.BINARY_SUCCESSOR.previousEpoch), seedRuntimeBinding: clone(closer.BINARY_SUCCESSOR.previousBinding),
    currentSource: clone(value.lineage.previousEpoch.currentSource), inactiveStageRetained: true, activeGenerationChanged: false,
    playbackRequests: 0, applyRequests: 0, rollbackRequests: 0,
    controllerSessions: Object.fromEntries(['admin', 'P', 'Q'].map((role, index) => [role, { credentialId: (index + 1).toString(16).padStart(32, '0'),
      tokenSha256: sha('synthetic-admission04-' + role), sameTokenRejected: true }])) };
  const admission = { ...clone(previous), version: 3, admissionKind: 'affected_tv_parent', runtimeEpoch: clone(input.runtimeEpoch),
    seedRuntimeBinding: clone(input.seedBinding), currentSource: clone(value.epoch.currentSource),
    freshChecks: Object.fromEntries(['runtimeIdentity', 'tvDefaultParents', 'tvDetailParents', 'ordinaryAuthorization', 'healthWindow60Seconds', 'sourceAndInactivePreserved', 'sessionCleanup'].map(key => [key, true])),
    reusedAdmission04: { report: clone(closer.REUSED_ADMISSION04), runtimeEpoch: clone(previous.runtimeEpoch),
      seedRuntimeBinding: clone(previous.seedRuntimeBinding), currentSource: clone(previous.currentSource),
      contracts: ['native_authentication_and_query_carriers', 'storage_and_library_access', 'backup_create_download', 'restore_ready_cancel_retained_stage'] },
    transitionCloseout: clone(closer.AFFECTED_TV_TRANSITION_CLOSEOUT) };
  delete admission.controllerSessions.admin;
  return { input, epoch: value.epoch, lineage: value.lineage, admission, previous };
}

function finalFixture(closer, scenario = 'movie') {
  const value = reviewedFixture(closer), pin = name => ({ path: RETAINED_ROOT + '/synthetic-final/' + name, sha256: sha(name) });
  const sequences = Object.fromEntries('activity_entries_id_seq application_keys_id_seq catalog_entities_id_seq devices_id_seq theme_owner_ids_id_seq'.split(' ').map(name =>
    [name, clone(value.before.sequences[name] ?? { lastValue: '1', isCalled: true })]));
  const moviePins = { ...clone(value.retained.review.movieProvenance), boundary: null }, movieHistory = { ...clone(value.retained.movieProvenance), boundary: null };
  movieHistory.snapshot.sequences = clone(sequences);
  for (const snapshot of [value.before, value.after]) snapshot.sequences = { ...clone(sequences), ...snapshot.sequences };
  const seed = value.seed;
  seed.version = 4; seed.runtimeEpoch = pin('runtime-epoch.json');
  const source = { ...clone(movieHistory.sourceEpoch.currentSource), ...Object.fromEntries(['binary', 'sourceManifest', 'fullReport', 'artifactReceipt', 'buildManifest', 'sourceBridge'].map(key => [key, pin(key + '.json')])) };
  const epoch = { kind: 'audited-candidate-runtime-epoch', version: 4, operationKind: 'programs_successor', currentSource: source };
  let actorPins = moviePins, actorHistory = movieHistory;
  if (scenario !== 'movie') {
    const actor = { id: (scenario === 'episode' ? 'e' : 'f').repeat(32), username: 'synthetic-' + scenario }; seed.actors[scenario] = actor;
    if (scenario === 'episode') seed.catalog.episodes = [{ id: '9'.repeat(32), type: 'Episode', parentIndexNumber: 2, indexNumber: 1, runtimeTicks: 6000000000 }];
    const item = scenario === 'episode' ? seed.catalog.episodes[0] : seed.catalog.movie;
    const template = value.before.tables.play_sessions.find(row => row.user_id === seed.actors.movie.id && row.state === 'Prepared');
    const play = { ...clone(template), id: 'play_' + scenario + '_retained', auth_session_id: scenario + '_retained_auth', user_id: actor.id,
      item_id: item.id, media_source_id: 'mediasource_' + item.id, duration_ticks: item.runtimeTicks, device_id: scenario + '-device' };
    for (const snapshot of [value.before, value.after]) {
      snapshot.tables.users.push({ id: actor.id, name: actor.username, is_administrator: false, is_disabled: false, policy: {} });
      snapshot.tables.sessions.push({ id: play.auth_session_id, user_id: actor.id, kind: 'emby', device_id: play.device_id, revoked_at: '2025-12-31T23:50:00Z' });
      snapshot.tables.play_sessions.push(clone(play));
      snapshot.tables.user_item_data.push({ user_id: actor.id, item_id: item.id, play_count: 1, playback_position_ticks: 123000000,
        is_favorite: false, played: false, last_played_at: '2025-12-31T23:40:00Z' });
    }
    actorPins = Object.fromEntries(['closeout', 'snapshot', 'sourceEpoch', 'manifest', 'boundary'].map(key => [key, pin(scenario + '-' + key + '.json')]));
    const process = { pid: 100, startTicks: '1000', bootId: 'synthetic-boot' }, candidate = { ...process, listener: { port: 19181, socketInode: '10' } };
    const historicEpoch = { ...clone(movieHistory.sourceEpoch), version: 3, candidateProcess: process, postgresProcess: { pid: 200 }, lease: { owned: true } };
    const manifest = { ...clone(movieHistory.manifest), scenario, runId: scenario + '-01', actor, catalog: clone(seed.catalog), processes: { candidate } };
    const evidence = { sourceAfter: actorPins.snapshot, runtimeEpoch: actorPins.sourceEpoch, manifest: actorPins.manifest,
      sourceBefore: pin(scenario + '-before.json'), boundary: actorPins.boundary, gatewayIndex: pin(scenario + '-gateway.json') };
    const boundary = { kind: 'audited-candidate-client-boundary', version: 1, runId: manifest.runId, runtimeEpoch: actorPins.sourceEpoch,
      sourceBefore: evidence.sourceBefore, sourceAfter: actorPins.snapshot, candidateBefore: candidate, candidateAfter: clone(candidate),
      postgresBefore: historicEpoch.postgresProcess, postgresAfter: clone(historicEpoch.postgresProcess), leaseBefore: historicEpoch.lease, leaseAfter: clone(historicEpoch.lease),
      clientWorker: { exitCode: 0, mainPID: 0, workerPidAbsent: true, remainingBrowserPids: [] },
      gatewayWorker: { exitCode: 0, mainPID: 0, workerPidAbsent: true, index: evidence.gatewayIndex } };
    actorHistory = { snapshot: clone(value.before), sourceEpoch: historicEpoch, manifest, boundary,
      closeout: { kind: 'audited-candidate-' + scenario + '-owned-state-closeout', status: 'owned_state_closed_client_acceptance_pending', clientAcceptance: false,
        sourcePinsUnchanged: true, inputEvidence: evidence } };
  }
  for (const snapshot of [value.before, value.after]) {
    snapshot.tables.client_playback_references.push({ user_id: CONTROL, auth_session_id: 'foreign-auth', play_session_id: 'play_foreign', client_nonce: 'foreign-reference' });
    for (const role of ['P', 'Q']) snapshot.tables.sessions.push({ id: 'admission-' + role, user_id: CONTROL, kind: 'emby', revoked_at: '2025-12-31T23:59:59Z' });
  }
  const actor = seed.actors[scenario], item = scenario === 'episode' ? seed.catalog.episodes[0] : seed.catalog.movie;
  const inputBinding = { runtimeEpoch: seed.runtimeEpoch, seedBinding: pin('seed-binding.json'), admission: pin('admission.json'), admissionCloseout: pin('admission-closeout.json') };
  const review = { kind: 'audited-candidate-final-client-baseline', version: 1, status: 'reviewed_closed_state', scenario, actor: { id: actor.id, username: actor.username },
    item: { id: item.id, mediaSourceId: 'mediasource_' + item.id, runtimeTicks: item.runtimeTicks }, runtimeEpoch: inputBinding.runtimeEpoch, seedBinding: inputBinding.seedBinding,
    currentSnapshot: pin('after-admission.json'), currentStateEvidence: { kind: 'programs-admission', receipt: inputBinding.admissionCloseout }, actorProvenance: actorPins, movieProvenance: moviePins,
    preparedExpirations: value.before.tables.play_sessions.filter(row => row.user_id === actor.id && row.state === 'Prepared').map(row => ({ playSessionId: row.id, authSessionId: row.auth_session_id })) };
  const currentStateEvidence = { kind: 'audited-candidate-programs-admission-closeout', version: 1, status: 'admitted_for_core_client', admissionKind: 'affected_programs',
    runtimeEpoch: inputBinding.runtimeEpoch, seedRuntimeBinding: inputBinding.seedBinding, sourceAfter: review.currentSnapshot, currentSource: source };
  value.retained = { review, snapshot: clone(value.before), currentStateEvidence, actorProvenance: actorHistory, movieProvenance: movieHistory, inputBinding, epoch };
  value.manifest = { scenario, actor: review.actor, catalog: clone(seed.catalog) };
  return value;
}

function finalSubtitleFixture() {
  const cues = [
    ['subtitle-srt-opening-cue-0', 'Opening subtitle', 2], ['subtitle-srt-forward-cue-1', 'Forward seek subtitle', 123],
    ['subtitle-vtt-forward-cue-0', 'Forward seek subtitle', 123], ['subtitle-vtt-opening-cue-0', 'Opening subtitle', 2],
  ];
  const video = (text, time) => ({ visible: true, source_matches_item: true, video_width: 640, current_time: time,
    text_tracks: [{ mode: text ? 'showing' : 'disabled', active_cues: text ? [{ text, start: time < 5 ? 0 : 118, end: time < 5 ? 5 : 128 }] : [] }] });
  return { restored_selection: true, selections: [['English (SRT)', 'subtitle-srt'], ['English (VTT)', 'subtitle-vtt'], ['Off', 'subtitle-off']]
    .map(([name, label]) => ({ name, label, at: '2026-01-01T00:00:00Z' })),
    steps: [...cues.map(([label, text, time]) => ({ label, media: [video(text, time)], visible_dom_cues: [] })),
      { label: 'subtitle-off-verified', media: [video(null, 2)], visible_dom_cues: [] }] };
}

function programsArtifactFixture(closer) {
  const pin = name => ({ path: '/opt/goby-test/synthetic-programs-artifact/' + name, sha256: sha(name), bytes: 100 });
  const receipt = { kind: 'goby-internal-amd64-artifact-receipt', version: 1, status: 'verified_and_closed',
    ...Object.fromEntries(['sourceArchive', 'sourceManifest', 'sourceBridge', 'buildManifest', 'newBinary', 'packageManifest', 'packageArchive', 'buildTools', 'execution', 'archive', 'closure', 'independentReview'].map(key => [key, pin(key + '.json')])) };
  receipt.sourceArchive = { ...clone(closer.PROGRAMS_LINEAGE.sourceArchive), bytes: 2715997 };
  receipt.worker = { archive: clone(receipt.archive), member: 'worker/report.json', sha256: sha('worker'), bytes: 100 };
  receipt.fullReport = { execution: clone(receipt.execution), worker: clone(receipt.worker),
    ordinaryBinary: { archive: clone(receipt.archive), member: 'worker/goby-ordinary', sha256: sha('different ordinary binary'), bytes: 200 } };
  const source = { archiveSha256: receipt.sourceArchive.sha256, schema: 28,
    ...Object.fromEntries(['sourceManifest', 'sourceBridge', 'buildManifest'].map(key => [key, { path: receipt[key].path, sha256: receipt[key].sha256 }])),
    binary: { path: '/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b/install/goby', sha256: receipt.newBinary.sha256 },
    fullReport: { path: receipt.execution.path, sha256: receipt.execution.sha256 } };
  const review = { kind: 'programs-final-product-independent-review', version: 1, status: 'verified', input: pin('input.json'), adapter: pin('adapter.py'),
    ...Object.fromEntries(['execution', 'archive', 'closure', 'sourceBridge', 'buildManifest', 'newBinary', 'packageManifest', 'packageArchive', 'buildTools', 'worker'].map(key => [key, clone(receipt[key])])),
    checks: Object.fromEntries('sourceIdentity ordinaryFullSuite ordinaryBuild embeddedBuild packageMembers artifactMaterialization toolPins budgets resourceClosure protectedState recordsBinding'.split(' ').map(key => [key, true])),
    limits: ['Synthetic fixture only; no product execution is asserted.'] };
  const build = { sourceInventory: { files: 924 }, moduleInputs: { go: sha('go.mod') }, administratorAssets: Array.from({ length: 57 }, (_, index) => ({ name: 'asset-' + index })), administratorEntryReferences: [] };
  const bridge = { kind: 'goby-frozen-source-build-bridge', version: 1, status: 'matched', input: clone(review.input), sourceArchive: clone(receipt.sourceArchive),
    sourceManifest: clone(receipt.sourceManifest), sourceCheckpoint: pin('checkpoint.json'), gitCommit: '74a69abacdd9206e51f4e166df2346b5555b5cd9',
    frozenFiles: 924, trackedBuildInputs: 867, generatedAssetCount: 57, frozenBytes: 13253765, ...clone(build), deploymentInputs: [],
    runtimeModeProjection: { umask: 63, rule: 'archiveMode & ~0077', allMatched: true,
      files: Array.from({ length: 924 }, (_, index) => ({ name: 'file-' + index, archiveMode: 0o644, runtimeMode: 0o600 })) },
    buildManifest: { ...clone(receipt.buildManifest), path: '/synthetic-old-ram/build-manifest.json' },
    packageManifest: { ...clone(receipt.packageManifest), path: '/synthetic-old-ram/package-manifest.json' }, reader: pin('reader.py'),
    sourceTreeUnchangedBeforeAfter: true, gitAndFrozenModesDeclaredEqual: false };
  const closure = { kind: 'livetv-programs-final-closure', version: 1, status: 'closed', input: clone(review.input), adapter: clone(review.adapter), archive: clone(receipt.archive),
    ...Object.fromEntries('resourcesClosed ownedProcessesClosed ext4Unmounted loopDetached ramUnmounted lockReleased allOwnedCommandsClosed protectedUnchanged'.split(' ').map(key => [key, true])) };
  return { receipt, review, bridge, build, closure, source };
}

function guardCases(closer) {
  return [
    ['final_v6_schema_keeps_legacy_and_recovery_contracts_separate', () => {
      const value = finalFixture(closer), pin = value.retained.review.currentSnapshot;
      const input = { kind: 'audited-candidate-client-closeout-input', version: 6, output: '/opt/goby-test/synthetic-final/closeout', sources: {}, retainedBaseline: pin, serverLog: pin };
      for (const key of ['manifest', 'observation', 'summary', 'gatewayAttestation', 'gatewayIndex', 'runtimeEpoch', 'admission', 'seedBinding', 'sourceBefore', 'sourceAfter', 'boundary']) input[key] = pin;
      for (const key of ['closer', 'adapter', 'gateway', 'proxy', 'sessionProof', 'movie', 'audio', 'subtitles', 'tv']) input.sources[key] = pin;
      assert.equal(closer.validateCloseoutInput(input), input);
      for (const changed of [{ ...input, currentRuntime: pin }, { ...input, serverLog: null }, { ...input, version: 5 }]) assert.throws(() => closer.validateCloseoutInput(changed));
      for (const scenario of ['tv-browse', 'mp3', 'flac']) assert.throws(() => closer.validateFinalBaselineInput({ ...value.retained.review, scenario }));
    }],
    ['final_v6_retains_all_three_actor_histories_and_foreign_references', () => {
      for (const scenario of ['movie', 'episode', 'subtitles']) {
        const value = finalFixture(closer, scenario), original = jsonBytes(value.retained);
        assert.equal(closer.validateFinalBaseline(value.retained, value.seed, value.retained.epoch).length, 1);
        assert.equal(closer.verifyFinalBefore(value.before, value.manifest, value.seed, value.retained).length, 1);
        assert.equal(value.before.tables.client_playback_references.length, 1); assert.deepEqual(jsonBytes(value.retained), original);
      }
    }],
    ['final_v6_requires_independent_admission_closeout_and_current_after_snapshot', () => {
      for (const mutate of [value => { value.retained.currentStateEvidence.kind = 'audited-candidate-live-admission'; value.retained.currentStateEvidence.version = 5; },
        value => { value.retained.review.currentStateEvidence.receipt = clone(value.retained.inputBinding.admission); },
        value => { value.retained.currentStateEvidence.sourceAfter = { ...value.retained.review.currentSnapshot, sha256: sha('transition-only-state') }; },
        value => { value.retained.currentStateEvidence.status = 'pending_independent_review'; }]) {
        const value = finalFixture(closer); mutate(value); assert.throws(() => closer.validateFinalBaseline(value.retained, value.seed, value.retained.epoch), /final_admission_state_binding/);
      }
    }],
    ['final_v6_fresh_state_checks_every_table_and_sequence', () => {
      const value = finalFixture(closer);
      for (const table of TABLES) { const changed = clone(value.before); changed.tables[table].push({ id: 'unexpected-row' });
        assert.throws(() => closer.verifyFinalBefore(changed, value.manifest, value.seed, value.retained)); }
      for (const name of Object.keys(value.before.sequences)) { const changed = clone(value.before); changed.sequences[name].lastValue = '999';
        assert.throws(() => closer.verifyFinalBefore(changed, value.manifest, value.seed, value.retained), /final_fresh_state_changed/); }
    }],
    ['final_v6_chain_is_same_epoch_and_does_not_replace_actor_or_movie_provenance', () => {
      const value = finalFixture(closer, 'episode'), { review, inputBinding, epoch } = value.retained;
      review.currentStateEvidence.kind = 'final-client';
      value.retained.currentStateEvidence = { kind: 'audited-candidate-client-closeout', contractVersion: 6, status: 'core_scenario_closed', scenario: 'movie',
        source: { binarySha256: epoch.currentSource.binary.sha256, manifestSha256: epoch.currentSource.sourceManifest.sha256, schema: 28 },
        evidence: { ...Object.fromEntries(['runtimeEpoch', 'seedBinding', 'admission'].map(key => [key, clone(inputBinding[key])])), sourceAfter: clone(review.currentSnapshot) } };
      closer.verifyFinalBefore(value.before, value.manifest, value.seed, value.retained);
      for (const key of ['runtimeEpoch', 'seedBinding', 'admission', 'sourceAfter']) { const changed = clone(value.retained);
        changed.currentStateEvidence.evidence[key] = { ...changed.currentStateEvidence.evidence[key], sha256: sha('changed') };
        assert.throws(() => closer.validateFinalBaseline(changed, value.seed, epoch), /final_preceding_client_state_binding/); }
      const changed = clone(value.retained); changed.actorProvenance.manifest.scenario = 'tv-browse';
      assert.throws(() => closer.validateFinalBaseline(changed, value.seed, epoch), /final_history_actor_source/);
    }],
    ['final_v6_prepared_changes_require_exact_new_owned_preparation', () => {
      const value = finalFixture(closer), retained = jsonBytes(value.retained);
      const result = closer.verifyFinalTransition(value.before, value.after, value.manifest, value.seed, value.physical, value.retained);
      assert.equal(result.expirations.length, 1); assert.deepEqual(jsonBytes(value.retained), retained);
      for (const mutate of [row => { row.retained.review.preparedExpirations = []; },
        row => { row.after.tables.play_sessions.find(play => play.id === closer.RETAINED_PLAY).position_ticks++; },
        row => { row.physical.logins[0].infos[0].exchange.result.completedAt = '2025-12-31T23:00:00Z'; }]) {
        const changed = finalFixture(closer); mutate(changed);
        assert.throws(() => closer.verifyFinalTransition(changed.before, changed.after, changed.manifest, changed.seed, changed.physical, changed.retained));
      }
    }],
    ['final_subtitles_require_each_opening_seek_cue_and_an_observed_off_state', () => {
      const value = finalSubtitleFixture(); assert.deepEqual(closer.verifyFinalSubtitleSteps(value), { srtOpening: true, srtSeek: true, vttOpening: true, vttSeek: true, off: true });
      for (const mutate of [row => { row.steps = row.steps.filter(step => !step.label.startsWith('subtitle-vtt-opening-cue')); },
        row => { row.steps[0].media[0].source_matches_item = false; }, row => { row.steps.at(-1).visible_dom_cues.push('Opening subtitle'); },
        row => { row.steps.at(-1).media[0].text_tracks[0].mode = 'showing'; }, row => { row.selections[1].name = 'Off'; }]) {
        const changed = clone(value); mutate(changed); assert.throws(() => closer.verifyFinalSubtitleSteps(changed));
      }
    }],
    ['final_components_do_not_treat_an_old_or_missing_receipt_as_admission', () => {
      assert.throws(() => closer.validateFinalComponentRecords({}, { version: 6, scenario: 'movie' }, { path: '/synthetic/controller', sha256: sha('controller') }, {}), /final_component_schema/);
      const value = finalFixture(closer); value.retained.review.currentStateEvidence.receipt = value.retained.inputBinding.admission;
      assert.throws(() => closer.validateFinalBaseline(value.retained, value.seed, value.retained.epoch), /final_admission_state_binding/);
    }],
    ['final_ui_requires_nonzero_movie_resume_and_complete_episode_browse', () => {
      const base = { outcome: 'scenario_completed', failure: null, page_errors: [], cleanup: { browser_closed: true, tokens_rejected: true, media_stopped: true },
        browser_environment: { service_workers: 'allow', proxy_bypass: '<-loopback>', quic: 'disabled', nonproxied_webrtc_udp: 'disabled' } };
      const video = (time, frames, paused = false) => ({ visible: true, current_time: time, total_video_frames: frames, paused, video_width: 640, duration: 600 });
      const movie = { ...clone(base), playback: { outcome: 'movie_ui_flow_completed', steps: [
        ['playing-start', video(1, 1)], ['playing-advanced', video(3, 3)], ['resumed', video(120, 120)], ['resumed-advanced', video(123, 123)],
        ['paused', video(3, 3, true)], ['before-stop', video(120, 120)], ['movie-seek-forward-before', video(3, 3)], ['movie-seek-forward-after', video(180, 20)],
        ['movie-seek-backward-before', video(180, 20)], ['movie-seek-backward-after', video(120, 30)],
      ].map(([label, value]) => ({ label, videos: [value] })) } };
      const movieManifest = { scenario: 'movie', catalog: { movie: { runtimeTicks: 6000000000 } } };
      closer.verifyUI(movie, movieManifest, { exchanges: [] }, { finalClient: true });
      for (const label of ['before-stop', 'resumed']) movie.playback.steps.find(row => row.label === label).videos[0].current_time = 0;
      assert.throws(() => closer.verifyUI(movie, movieManifest, { exchanges: [] }, { finalClient: true }), /final_movie_resume_must_be_nonzero/);
      closer.verifyUI(movie, movieManifest, { exchanges: [] });
      const episode = { ...clone(base), tv_browse: { outcome: 'tv_browse_ui_flow_completed', episode_detail: { season_and_episode_metadata_observed: true } },
        episode_playback: { item_id: ITEM, stopped: true, first: { current_time: 1, frames: 1, source_matches_item: true },
          advanced: { current_time: 3, frames: 3, source_matches_item: true }, paused: { paused: true, current_time: 3 }, held: { paused: true, current_time: 3 },
          seeks: [{ current_time: 180, frames: 10 }, { current_time: 60, frames: 20 }], resumed: { paused: false, current_time: 63, frames: 23 } } };
      const episodeManifest = { scenario: 'episode', catalog: { episodes: [{ id: 'x' }, { id: 'y' }, { id: ITEM }] } };
      closer.verifyUI(episode, episodeManifest, { exchanges: [] }, { finalClient: true });
      episode.tv_browse.outcome = 'blocked_at_observed_ui_step';
      assert.throws(() => closer.verifyUI(episode, episodeManifest, { exchanges: [] }, { finalClient: true }), /final_episode_browse_incomplete/);
      closer.verifyUI(episode, episodeManifest, { exchanges: [] });
    }],
    ['programs_artifact_binds_one_worker_and_distinct_ordinary_embedded_binaries', () => {
      const value = programsArtifactFixture(closer), verify = row => closer.validateProgramsArtifact(row.receipt, row.review, row.bridge, row.build, row.closure, row.source);
      assert.equal(verify(value).ordinaryAndEmbeddedWorkerBound, true);
      assert.notEqual(value.receipt.fullReport.ordinaryBinary.sha256, value.receipt.newBinary.sha256);
      for (const mutate of [row => { row.receipt.fullReport.worker.sha256 = sha('second worker'); }, row => { row.review.checks.ordinaryFullSuite = false; },
        row => { row.source.binary.sha256 = row.receipt.fullReport.ordinaryBinary.sha256; }, row => { row.bridge.runtimeModeProjection.files[0].runtimeMode = 0o644; },
        row => { row.bridge.buildManifest.sha256 = sha('different manifest'); }, row => { row.closure.lockReleased = false; }]) {
        const changed = programsArtifactFixture(closer); mutate(changed); assert.throws(() => verify(changed));
      }
    }],
    ['login_request_decoder_dispatches_json_and_utf8_body_form_without_query_merge', () => {
      const options = { route: '/Users/AuthenticateByName', kind: 'login', type: 'application/x-www-form-urlencoded; charset=UTF-8' };
      const exchange = protocolFixture(closer, 'Username=synthetic+actor&Pw=p%2B%3D%26%E4%B8%AD', options), raw = Buffer.from(exchange.requestEntity);
      exchange.url.search = '?Username=another&Pw=ignored';
      assert.deepEqual(closer.protocolRequestBody(exchange), { Username: 'synthetic actor', Pw: 'p+=&中' });
      assert.deepEqual(exchange.requestEntity, raw);
      assert.deepEqual(closer.loginRequestBody(protocolFixture(closer, 'uSeRnAmE=actor&pW=secret', options)), { Username: 'actor', Pw: 'secret' });
      const json = protocolFixture(closer, '{"Username":"actor","Pw":"secret"}', { ...options, type: 'application/json; charset="utf-8"' });
      assert.deepEqual(closer.protocolRequestBody(json), { Username: 'actor', Pw: 'secret' });
      for (const body of ['Username=a&Username=b&Pw=p', 'Username=a&username=b&Pw=p', 'User%6Eame=a&Username=b&Pw=p',
        'Username=a&Password=p', 'Username=a&Pw=%FF', 'Username=a&Pw=%C3%28', 'Username=a&Pw=%', 'Username=a&Pw=%00',
        'Username=a&Pw=p;q', '', 'Username=a&' + '&'.repeat(16)])
        assert.throws(() => closer.loginRequestBody(protocolFixture(closer, body, options)));
      assert.throws(() => closer.loginRequestBody(protocolFixture(closer, Buffer.from([0xff]), options)));
      for (const type of ['text/plain', 'application/octet-stream', 'application/x-www-form-urlencoded; charset=latin1',
        'application/x-www-form-urlencoded; charset=utf-8; charset=utf-8', 'application/x-www-form-urlencoded; other=utf-8'])
        assert.throws(() => closer.loginRequestBody(protocolFixture(closer, 'Username=a&Pw=p', { ...options, type })), /request_content_type_unsupported/);
      assert.throws(() => closer.loginRequestBody(protocolFixture(closer, 'Username=a&Pw=p', { ...options, encoding: 'identity' })), /login_form_content_encoding/);
      assert.throws(() => closer.loginRequestBody(protocolFixture(closer, '{"Username":"a","Username":"b","Pw":"p"}', { ...options, type: 'application/json' })), /json_duplicate_key/);
      assert.throws(() => closer.loginRequestBody(protocolFixture(closer, '{}', { ...options, route: '/Users/New' })), /request_body_route_scope/);
    }],
    ['playback_request_int64_hint_is_lossless_only_on_the_three_report_routes', () => {
      for (const route of ['/Sessions/Playing', '/Sessions/Playing/Progress', '/Sessions/Playing/Stopped']) for (const type of ['application/json', 'text/plain; charset=UTF-8']) {
        const raw = Buffer.from('{"ItemId":"' + ITEM + '","PositionTicks":123,"PlaybackStartTimeTicks":639249846659000000}');
        const exchange = protocolFixture(closer, raw, { route, type }), value = closer.protocolRequestBody(exchange);
        assert.equal(value.PlaybackStartTimeTicks, '639249846659000000'); assert.equal(value.PositionTicks, 123); assert.equal(value.ItemId, ITEM);
        assert.deepEqual(exchange.requestEntity, raw); assert.throws(() => closer.strictJSON(raw), /json_unsafe_number/);
      }
      for (const token of ['-9223372036854775808', '9223372036854775807'])
        assert.equal(closer.playbackRequestBody(protocolFixture(closer, '{"PlaybackStartTimeTicks":' + token + '}')).PlaybackStartTimeTicks, token);
      for (const token of ['"639249846659000000"', 'null', '{}', '[]', 'true', '1e3', '1.0', '9223372036854775808', '-9223372036854775809'])
        assert.throws(() => closer.playbackRequestBody(protocolFixture(closer, '{"PlaybackStartTimeTicks":' + token + '}')));
      for (const body of ['{"PositionTicks":9007199254740992}', '{"ItemId":9007199254740992}', '{"nested":{"PlaybackStartTimeTicks":639249846659000000}}'])
        assert.throws(() => closer.playbackRequestBody(protocolFixture(closer, body)), /json_unsafe_number/);
      assert.throws(() => closer.playbackRequestBody(protocolFixture(closer, '{"PlaybackStartTimeTicks":1,"PlaybackStartTimeTicks":2}')), /json_duplicate_key/);
      for (const changes of [{ method: 'GET' }, { route: '/Sessions/Playing/Other' }, { kind: 'metadata' }, { type: 'application/x-www-form-urlencoded' }])
        assert.throws(() => closer.playbackRequestBody(protocolFixture(closer, '{"PlaybackStartTimeTicks":1}', changes)));
      const info = protocolFixture(closer, '{"PlaybackStartTimeTicks":639249846659000000}', { kind: 'playback_info', route: '/Items/' + ITEM + '/PlaybackInfo' });
      assert.throws(() => closer.protocolRequestBody(info), /json_unsafe_number/);
    }],
    ['observation_int64_reader_recomputes_scope_and_matches_original_payload', () => {
      const body = Buffer.from('{"ItemId":"' + ITEM + '","PositionTicks":123,"PlaybackStartTimeTicks":639249846659000000}');
      const raw = observedBodyBytes(body), untouched = Buffer.from(raw), observed = closer.observationJSON(raw, manifest);
      assert.deepEqual(observed.requests[0].body, closer.playbackRequestBody(protocolFixture(closer, body)));
      assert.equal(observed.requests[0].payload_base64, body.toString('base64')); assert.deepEqual(raw, untouched);
      assert.throws(() => closer.strictJSON(raw), /json_unsafe_number/);
      for (const changes of [{ method: 'GET' }, { allowed: false }, { origin: 'external' }, { scope: 'metadata' }, { kind: 'read' },
        { route: '/Sessions/Playing/Progress' }, { url: ORIGIN + '/emby/System/Info' }, { url: ORIGIN + '/emby/Sessions/Playing/Other' },
        { url: 'http://127.0.0.1:19181/emby/Sessions/Playing' }])
        assert.throws(() => closer.observationJSON(observedBodyBytes(body, changes), manifest), /observation_playback_int64_scope/);
      assert.throws(() => closer.observationJSON(observedBodyBytes(body, { headers: { 'content-type': 'application/octet-stream' } }), manifest), /request_content_type_unsupported/);
      const different = Buffer.from(body.toString().replace('639249846659000000', '639249846659000001'));
      assert.throws(() => closer.observationJSON(observedBodyBytes(different, { payload_base64: body.toString('base64') }), manifest), /observation_playback_body_payload_mismatch/);
      assert.throws(() => closer.observationJSON(observedBodyBytes(body, { payload_base64: null }), manifest), /invalid_base64/);
      const worker = closer.observationJSON(observedBodyBytes(body, { scope: 'service_worker', main_frame: false }), manifest);
      assert.equal(worker.requests[0].body.PlaybackStartTimeTicks, '639249846659000000');
    }],
    ['observation_int64_exceptions_do_not_extend_to_metadata_identity_or_position', () => {
      for (const token of ['-9223372036854775808', '9223372036854775807']) {
        const raw = observedBodyBytes('{"PlaybackStartTimeTicks":' + token + '}');
        assert.equal(closer.observationJSON(raw, manifest).requests[0].body.PlaybackStartTimeTicks, token);
      }
      for (const token of ['null', '"1"', '1.0', '1e2', 'true', '{}', '9223372036854775808', '-9223372036854775809'])
        assert.throws(() => closer.observationJSON(observedBodyBytes('{"PlaybackStartTimeTicks":' + token + '}'), manifest));
      for (const body of ['{"PositionTicks":9007199254740992}', '{"UserId":9007199254740992}',
        '{"metadata":{"PlaybackStartTimeTicks":639249846659000000}}'])
        assert.throws(() => closer.observationJSON(observedBodyBytes(body), manifest), /json_unsafe_number/);
      assert.throws(() => closer.observationJSON(Buffer.from('{"metadata":{"PlaybackStartTimeTicks":639249846659000000}}'), manifest), /json_unsafe_number/);
      assert.throws(() => closer.observationJSON(Buffer.from('{"requests":{"0":{"body":{"PlaybackStartTimeTicks":639249846659000000}}}}'), manifest), /json_unsafe_number/);
      assert.throws(() => closer.observationJSON(observedBodyBytes('{"PlaybackStartTimeTicks":1,"PlaybackStartTimeTicks":2}'), manifest), /json_duplicate_key/);
    }],
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
      assert.deepEqual(proof, { ordinal: 7, interpretation: 'browser_abort_near_owned_stop', contextAssociation: 'exact_range', contextOrdinals: [101, 102], completeHTTP: false,
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
    ['response_ids_disambiguate_repeated_complete_and_partial_media', () => {
      for (const complete of [false, true]) {
        const { exchange, report, login } = partialFixture(closer, { complete, responseHeaders: [['X-Request-Id', 'response-A']] });
        for (const row of report.requests) row.response_headers = { 'x-request-id': 'response-B' };
        assert.deepEqual(closer.matchesContext(exchange, report), []);
        assert.deepEqual(closer.mediaContextEvidence(exchange, report), []);
        if (!complete) assert.throws(() => closer.explainMediaPartial(exchange, report, login), /media_partial_context_missing/);
        report.requests.push({ ...clone(report.requests[0]), ordinal: 103, response_headers: { 'X-ReQuEsT-Id': 'response-A' } });
        const before = jsonBytes(exchange.result);
        assert.deepEqual(closer.mediaContextEvidence(exchange, report).map(value => [value.row.ordinal, value.association]), [[103, 'exact_range']]);
        assert.equal(exchange.complete, complete); assert.deepEqual(jsonBytes(exchange.result), before);
      }
    }],
    ['missing_response_ids_preserve_legacy_exact_and_contained_range_evidence', () => {
      for (const [physicalHeaders, observedHeaders] of [
        [[], undefined], [[], {}], [[['X-Request-Id', 'response-A']], undefined],
        [[], { 'x-request-id': 'response-A' }], [[['X-Request-Id', 'response-A']], { 'X-Request-Id': '\tresponse-A ' }],
      ]) {
        const { exchange, report } = partialFixture(closer, { responseHeaders: physicalHeaders });
        for (const row of report.requests) row.response_headers = observedHeaders;
        assert.equal(closer.mediaContextEvidence(exchange, report).length, 2);
        for (const row of report.requests) row.headers.range = 'bytes=0-';
        assert.deepEqual(closer.mediaContextEvidence(exchange, report).map(value => value.association),
          ['unique_contained_range_with_terminal_time', 'unique_contained_range_with_terminal_time']);
      }
    }],
    ['ambiguous_or_malformed_response_ids_cannot_be_treated_as_missing', () => {
      const cases = [
        [[['X-Request-Id', 'response-A'], ['x-request-id', 'response-A']], undefined],
        [[['X-Request-Id', 'response-A'], ['x-request-id', 'response-B']], undefined],
        [[], { 'X-Request-Id': 'response-A', 'x-request-id': 'response-A' }],
        [[], { 'X-Request-Id': 'response-A', 'x-request-id': 'response-B' }],
        [[['X-Request-Id', 'response-A']], { 'x-request-id': 'response-a' }],
        ...['', ' ', 'response-A,response-A', 'response-A, response-B', 'response-A response-B'].map(value => [[['X-Request-Id', value]], undefined]),
        ...['', ' ', 'response-A,response-A', 'response-A\nresponse-B', null, 7, ['response-A']].map(value => [[], { 'x-request-id': value }]),
      ];
      for (const [physicalHeaders, observedHeaders] of cases) {
        const { exchange, report } = partialFixture(closer, { responseHeaders: physicalHeaders });
        for (const row of report.requests) row.response_headers = observedHeaders;
        assert.deepEqual(closer.matchesContext(exchange, report), []);
        for (const row of report.requests) row.headers.range = 'bytes=0-';
        assert.deepEqual(closer.mediaContextEvidence(exchange, report), []);
      }
    }],
    ['matching_response_ids_preserve_request_and_abort_constraints', () => {
      for (const mutate of [
        row => { row.method = 'POST'; }, row => { row.url += '&Other=1'; }, row => { row.token_sha256 = '0'.repeat(64); },
        row => { row.headers.range = 'bytes=1-1999'; }, row => { row.elapsed_ms = 6001; },
      ]) {
        const { exchange, report } = partialFixture(closer, { responseHeaders: [['X-Request-Id', 'response-A']] });
        for (const row of report.requests) { row.response_headers = { 'x-request-id': 'response-A' }; mutate(row); }
        assert.deepEqual(closer.mediaContextEvidence(exchange, report), []);
      }
      const { exchange, report, login } = partialFixture(closer, { responseHeaders: [['X-Request-Id', 'response-A']] });
      for (const row of report.requests) row.response_headers = { 'x-request-id': 'response-A' };
      exchange.requestEntity = Buffer.from('retained-request-body');
      assert.deepEqual(closer.matchesContext(exchange, report), []);
      exchange.requestEntity = null;
      report.requests[0].failed_elapsed_ms = 9000;
      assert.throws(() => closer.explainMediaPartial(exchange, report, login), /media_partial_time_mismatch/);
    }],
    ['contained_range_identity_filter_preserves_terminal_time_and_scope_uniqueness', () => {
      const { exchange, report } = partialFixture(closer, { responseHeaders: [['X-Request-Id', 'response-A']] });
      for (const row of report.requests) row.response_headers = { 'x-request-id': 'response-B' };
      report.requests.push({ ...clone(report.requests[0]), ordinal: 103, headers: { range: 'bytes=0-' },
        response_headers: { 'x-request-id': 'response-A' } });
      assert.deepEqual(closer.mediaContextEvidence(exchange, report).map(value => [value.row.ordinal, value.association]),
        [[103, 'unique_contained_range_with_terminal_time']]);
      const row = report.requests.at(-1);
      row.failed_elapsed_ms = 999;
      assert.deepEqual(closer.mediaContextEvidence(exchange, report), []);
      row.failed_elapsed_ms = 3000;
      report.requests.push({ ...clone(row), ordinal: 104 });
      assert.deepEqual(closer.mediaContextEvidence(exchange, report), []);
    }],
    ['reviewed_movie_v4_input_does_not_relax_legacy_authority', () => {
      const value = reviewedFixture(closer), pin = value.retained.review.closeout;
      const input = { kind: 'audited-candidate-client-closeout-input', version: 4, output: '/opt/goby-test/synthetic-reviewed/output',
        sources: {}, retainedBaseline: pin, serverLog: pin };
      for (const name of ['manifest', 'observation', 'summary', 'gatewayAttestation', 'gatewayIndex', 'runtimeEpoch', 'admission', 'seedBinding', 'sourceBefore', 'sourceAfter', 'boundary']) input[name] = pin;
      for (const name of ['closer', 'adapter', 'gateway', 'proxy', 'sessionProof', 'movie', 'audio', 'subtitles', 'tv']) input.sources[name] = pin;
      closer.validateCloseoutInput(input); closer.validateReviewedMovieBaselineInput(value.retained.review);
      for (const changed of [{ ...input, retainedBaseline: null }, { ...input, serverLog: null }, { ...input, unreviewed: true }, { ...input, version: 3 }])
        assert.throws(() => closer.validateCloseoutInput(changed));
      for (const mutate of [row => { row.version = 2; }, row => { row.scenario = 'mp3'; }, row => { row.extra = true; },
        row => { row.preparedExpirations.push(clone(row.preparedExpirations[0])); }]) {
        const changed = clone(value.retained.review); mutate(changed); assert.throws(() => closer.validateReviewedMovieBaselineInput(changed));
      }
    }],
    ['reviewed_movie_binds_historical_closeout_source_manifest_and_current_actor', () => {
      const value = reviewedFixture(closer);
      assert.deepEqual(closer.validateReviewedMovieBaseline(value.retained, value.seed).map(row => row.id), [closer.RETAINED_PLAY]);
      for (const mutate of [
        row => { row.retained.closeout.inputEvidence.after = clone(row.retained.review.closeout); },
        row => { row.retained.closeout.inputEvidence.epoch = clone(row.retained.review.runtimeEpoch); },
        row => { row.retained.closeout.inputEvidence.manifest = clone(row.retained.review.snapshot); },
        row => { row.retained.inputBinding.runtimeEpoch = clone(row.retained.review.sourceEpoch); },
        row => { row.retained.inputBinding.seedBinding = clone(row.retained.review.snapshot); },
        row => { row.seed.runtimeEpoch = clone(row.retained.review.sourceEpoch); },
        row => { row.retained.sourceEpoch.currentSource.binary.sha256 = '0'.repeat(64); },
        row => { row.retained.manifest.actor.id = 'e'.repeat(32); },
        row => { row.retained.manifest.catalog.movie.runtimeTicks++; },
        row => { row.retained.manifest.serverId = 'e'.repeat(32); },
        row => { row.retained.closeout.failure = null; },
        row => { row.retained.closeout.checks.savedRuntimeAndWorkers.workers.browser.pidAbsentAsRecorded = false; },
      ]) { const changed = reviewedFixture(closer); mutate(changed); assert.throws(() => closer.validateReviewedMovieBaseline(changed.retained, changed.seed)); }
    }],
    ['reviewed_movie_rejects_unlisted_preparation_credentials_and_snapshot_ambiguity', () => {
      for (const mutate of [
        row => { row.retained.review.preparedExpirations = []; },
        row => { row.retained.review.preparedExpirations[0].authSessionId = 'foreign'; },
        row => { row.retained.snapshot.tables.sessions.push(clone(row.retained.snapshot.tables.sessions[0])); },
        row => { row.retained.snapshot.tables.play_sessions.push(clone(row.retained.snapshot.tables.play_sessions[0])); },
        row => { row.retained.snapshot.tables.sessions[1].revoked_at = null; },
        row => { row.retained.snapshot.tables.play_sessions[0].user_id = 'e'.repeat(32); },
        row => { row.retained.snapshot.tables.play_sessions[0].counted = true; },
        row => { row.retained.snapshot.tables.client_playback_references.push({ user_id: CONTROL }); },
        row => { row.retained.snapshot.tables.encoding_jobs.push({ user_id: CONTROL }); },
        row => { row.retained.snapshot.tables.not_a_table = row.retained.snapshot.tables.task_runs; delete row.retained.snapshot.tables.task_runs; },
        row => { row.retained.snapshot.tables.user_item_data.find(data => data.user_id === ACTOR).updated_at = '2026-01-01T00:00:00Z'; },
      ]) { const changed = reviewedFixture(closer); mutate(changed); assert.throws(() => closer.validateReviewedMovieBaseline(changed.retained, changed.seed)); }
    }],
    ['reviewed_movie_before_preserves_all_tables_sequences_and_pruning_bounds', () => {
      const value = reviewedFixture(closer);
      closer.verifyRetainedMovieBefore(value.before, value.manifest, value.seed, value.retained);
      for (const mutate of [
        row => { row.before.tables.users.find(user => user.id === CONTROL).name = 'changed'; },
        row => { row.before.tables.play_sessions.find(play => play.id === 'play_preserved_terminal').player_state.CanSeek = true; },
        row => { row.before.tables.user_item_data.find(data => data.user_id === ACTOR).play_count++; },
        row => { row.before.tables.user_item_data.find(data => data.user_id === CONTROL).is_favorite = 0; },
        row => { row.before.sequences.theme_owner_ids_id_seq.lastValue = '8'; },
        row => { row.before.capturedAt = '2026-01-09T00:00:00Z'; },
      ]) { const changed = reviewedFixture(closer); mutate(changed); assert.throws(() => closer.verifyRetainedMovieBefore(changed.before, changed.manifest, changed.seed, changed.retained)); }
    }],
    ['reviewed_movie_expiration_is_explicit_and_does_not_reclassify_old_terminal_rows', () => {
      const value = reviewedFixture(closer), before = jsonBytes(value.retained);
      const proof = closer.verifyDurable(value.before, value.after, value.manifest, value.seed, value.physical, value.retained);
      assert.equal(proof.countedPlays, 2);
      assert.deepEqual(proof.retainedBaselineExpiration.playSessionIds, [closer.RETAINED_PLAY]);
      assert.equal(proof.retainedBaselineExpiration.expirations[0].prepareOrdinal, 1);
      assert.deepEqual(jsonBytes(value.retained), before);
      const unchanged = closer.verifyReviewedMovieTransition(value.before, clone(value.before), value.manifest, value.seed,
        { logins: [], chains: [], exchanges: [] }, value.retained);
      assert.deepEqual(unchanged.playSessionIds, []);
      const terminalOnly = reviewedFixture(closer);
      for (const state of [terminalOnly.before, terminalOnly.retained.snapshot, terminalOnly.retained.movieProvenance.snapshot]) state.tables.play_sessions = state.tables.play_sessions.filter(row => row.state !== 'Prepared');
      terminalOnly.retained.review.preparedExpirations = [];
      assert.deepEqual(closer.verifyRetainedMovieBefore(terminalOnly.before, terminalOnly.manifest, terminalOnly.seed, terminalOnly.retained), []);
    }],
    ['reviewed_movie_uses_latest_subtitles_state_and_preserves_foreign_audio_references', () => {
      const value = reviewedLatestFixture(closer), retainedBytes = jsonBytes(value.retained);
      assert.notDeepEqual(value.retained.review.sourceEpoch, value.retained.review.movieProvenance.sourceEpoch);
      assert.equal(value.before.tables.client_playback_references.length, 2);
      closer.verifyRetainedMovieBefore(value.before, value.manifest, value.seed, value.retained);
      const proof = closer.verifyDurable(value.before, value.after, value.manifest, value.seed, value.physical, value.retained);
      assert.equal(proof.countedPlays, 2); assert.deepEqual(jsonBytes(value.retained), retainedBytes);
      assert.deepEqual(value.before.tables.client_playback_references, value.after.tables.client_playback_references);
    }],
    ['reviewed_latest_state_cannot_replace_movie_provenance_or_worker_closure', () => {
      for (const replacement of [undefined, null, true, {}, { originalObservationUnchanged: false }, { originalObservationUnchanged: 1 }]) {
        const changed = reviewedLatestFixture(closer);
        changed.retained.closeout.originalObservationUnchanged = true;
        if (replacement === undefined) delete changed.retained.closeout.originalUIRejection;
        else changed.retained.closeout.originalUIRejection = replacement;
        assert.throws(() => closer.validateReviewedMovieBaseline(changed.retained, changed.seed), /reviewed_movie_closed_state_kind/);
      }
      for (const mutate of [
        row => { row.retained.snapshot.tables.play_sessions.find(play => play.id === 'play_preserved_terminal').updated_at = '2026-01-01T00:00:01Z'; },
        row => { row.retained.snapshot.tables.user_item_data.find(data => data.user_id === ACTOR).play_count++; },
        row => { row.retained.boundary.clientWorker.workerPidAbsent = false; },
        row => { row.retained.boundary.candidateAfter.pid++; },
        row => { row.retained.boundary.gatewayWorker.index = clone(row.retained.review.snapshot); },
        row => { row.retained.closeout.inputEvidence.sourceAfter = clone(row.retained.review.movieProvenance.snapshot); },
        row => { row.retained.closeout.sourceState.afterClientReferences = 0; },
        row => { row.retained.snapshot.tables.client_playback_references[0].auth_session_id = closer.RETAINED_AUTH; },
        row => { row.retained.review.movieProvenance.sourceEpoch = clone(row.retained.review.sourceEpoch); },
      ]) { const changed = reviewedLatestFixture(closer); mutate(changed); assert.throws(() => closer.validateReviewedMovieBaseline(changed.retained, changed.seed)); }
      for (const [table, key, field] of [['users', 'id', 'name'], ['sessions', 'user_id', 'client_version']]) {
        const changed = reviewedLatestFixture(closer);
        changed.retained.snapshot.tables[table].find(row => row[key] === ACTOR)[field] = 'changed-history';
        assert.throws(() => closer.validateReviewedMovieBaseline(changed.retained, changed.seed), /reviewed_movie_provenance_state_changed/);
      }
      for (const table of ['client_playback_references', 'encoding_jobs']) for (const owner of [
        { user_id: ACTOR }, { user_id: CONTROL, auth_session_id: closer.RETAINED_AUTH }, { user_id: CONTROL, play_session_id: closer.RETAINED_PLAY },
      ]) {
        const changed = reviewedLatestFixture(closer);
        changed.retained.snapshot.tables[table].push(owner);
        changed.retained.closeout.sourceState[table === 'encoding_jobs' ? 'afterEncodingJobs' : 'afterClientReferences']++;
        assert.throws(() => closer.validateReviewedMovieBaseline(changed.retained, changed.seed), /reviewed_movie_credentials_or_residue/);
      }
      const changed = reviewedLatestFixture(closer);
      changed.after.tables.client_playback_references[0].client_nonce = 'changed-foreign-nonce';
      assert.throws(() => closer.verifyDurable(changed.before, changed.after, changed.manifest, changed.seed, changed.physical, changed.retained), /foreign_actor_state_changed_client_playback_references/);
    }],
    ['reviewed_movie_expiration_requires_one_successful_new_owned_preparation', () => {
      for (const mutate of [
        row => { row.after.tables.play_sessions.find(play => play.id === closer.RETAINED_PLAY).position_ticks++; },
        row => { row.after.tables.play_sessions.find(play => play.id === closer.RETAINED_PLAY).stopped_at = '2026-01-01T00:00:05Z'; },
        row => { row.after.tables.play_sessions.find(play => play.id === 'play_preserved_terminal').updated_at = '2026-01-01T00:00:01Z'; },
        row => { row.physical.logins[0].infos[0].exchange.complete = false; },
        row => { row.physical.logins[0].infos[0].exchange.response.status = 500; },
        row => { row.physical.logins[0].infos[0].exchange.tokenHash = '0'.repeat(64); },
        row => { row.physical.logins[0].infos[0].body.UserId = CONTROL; },
        row => { row.physical.logins[0].infos[0].itemId = CONTROL; },
        row => { row.physical.logins[0].sessionId = closer.RETAINED_AUTH; },
        row => { row.physical.logins[0].infos[0].value.PlaySessionId = closer.RETAINED_PLAY; },
        row => { row.physical.logins[0].infos.push({ ...row.physical.logins[0].infos[0] }); },
      ]) { const changed = reviewedFixture(closer); mutate(changed); assert.throws(() => closer.verifyReviewedMovieTransition(changed.before, changed.after, changed.manifest, changed.seed, changed.physical, changed.retained)); }
    }],
    ['reviewed_tv_is_a_distinct_contract_and_retains_the_consumed_actor', () => {
      const value = reviewedTVFixture(closer), before = sha(jsonBytes(value.retained));
      assert.equal(closer.validateReviewedTVBaseline(value.retained, value.seed).id, 'play_retained_tv');
      closer.verifyReviewedTVBefore(value.before, value.manifest, value.seed, value.retained);
      assert.throws(() => closer.validateReviewedTVBaseline(reviewedLatestFixture(closer).retained, value.seed));
      assert.throws(() => closer.validateReviewedMovieBaseline(value.retained, value.seed));
      for (const mutate of [row => { row.review.scenario = 'movie'; }, row => { row.review.preparedExpirations = []; },
        row => { row.review.preparedExpirations[0].authSessionId = 'foreign'; }, row => { row.tvProvenance.closeout.playingReports = 1; },
        row => { row.tvProvenance.closeout.inputEvidence.epoch = clone(row.review.sourceEpoch); }]) {
        const changed = clone(value.retained); mutate(changed); assert.throws(() => closer.validateReviewedTVBaseline(changed, value.seed));
      }
      assert.equal(sha(jsonBytes(value.retained)), before);
    }],
    ['tv_post_browse_admission_allows_only_the_exact_revoked_probe_session', () => {
      for (const mutate of [row => { row.review.postBrowseAdmission.sha256 = '0'.repeat(64); },
        row => { row.postBrowseAdmission.controllerSessions.P.sameTokenRejected = false; },
        row => { row.snapshot.tables.sessions.find(session => session.id === '9'.repeat(32)).id = '8'.repeat(32); },
        row => { row.snapshot.tables.sessions.find(session => session.id === '9'.repeat(32)).revoked_at = null; },
        row => { row.snapshot.tables.sessions.push(clone(row.snapshot.tables.sessions[0])); },
        row => { row.snapshot.tables.play_sessions[0].auth_session_id = '9'.repeat(32); }]) {
        const value = reviewedTVFixture(closer); mutate(value.retained);
        assert.throws(() => closer.validateReviewedTVBaseline(value.retained, value.seed));
      }
    }],
    ['reviewed_tv_preserves_all_history_foreign_references_and_sequences', () => {
      for (const mutate of [row => { row.before.tables.user_item_data[0].play_count++; },
        row => { row.before.tables.client_playback_references[0].client_nonce += '-changed'; },
        row => { row.before.sequences.theme_owner_ids_id_seq.lastValue = '8'; },
        row => { row.retained.snapshot.tables.sessions[0].revoked_at = null; },
        row => { row.retained.snapshot.tables.play_sessions[0].position_ticks = 1; }]) {
        const value = reviewedTVFixture(closer); mutate(value);
        assert.throws(() => closer.verifyReviewedTVBefore(value.before, value.manifest, value.seed, value.retained));
      }
      const value = reviewedTVFixture(closer);
      const proof = closer.verifyDurable(value.before, value.after, value.manifest, value.seed, value.physical, value.retained);
      assert.equal(proof.countedPlays, 0); assert.equal(proof.sessionsRevoked, 1); assert.equal(proof.retainedPreparations.length, 1);
      assert.deepEqual(proof.retainedBaselineExpiration.playSessionIds, ['play_retained_tv']);
      assert.deepEqual(value.after.tables.user_item_data, value.before.tables.user_item_data);
      assert.deepEqual(value.after.tables.client_playback_references, value.before.tables.client_playback_references);
    }],
    ['reviewed_tv_rejects_playback_userdata_and_unbound_preparation_changes', () => {
      for (const mutate of [row => { row.after.tables.user_item_data.find(data => data.user_id === ACTOR).updated_at = row.after.capturedAt; },
        row => { row.after.tables.play_sessions.find(play => play.id === 'play_synthetic').counted = true; },
        row => { row.after.tables.play_sessions.find(play => play.id === 'play_retained_tv').position_ticks = 1; },
        row => { row.physical.logins[0].infos[0].exchange.complete = false; },
        row => { row.physical.logins[0].infos[0].exchange.tokenHash = '0'.repeat(64); },
        row => { row.physical.logins[0].infos[0].body.UserId = CONTROL; },
        row => { row.physical.logins[0].infos.push(row.physical.logins[0].infos[0]); },
        row => { row.physical.exchanges.push({ request: { kind: 'media' } }); },
        row => { row.physical.exchanges.push({ request: { path: '/emby/Sessions/Playing/Stopped' } }); }]) {
        const value = reviewedTVFixture(closer); mutate(value);
        assert.throws(() => closer.verifyDurable(value.before, value.after, value.manifest, value.seed, value.physical, value.retained));
      }
      assert.throws(() => closer.verifyTVNoPlayback({ requests: [{ kind: 'media' }] }, []), /reviewed_tv_playback_forbidden/);
    }],
    ['tv_diagnostic_preserves_ui_failure_without_waiving_cleanup_or_unknown_exceptions', () => {
      const value = nativeTVFixture(closer), result = closer.tvDiagnosticUIResult(value.report, value.manifest, value.physical);
      assert.equal(result.uiAccepted, false); assert.equal(result.clientAcceptance, false); assert.equal(result.diagnostic.status, 'response_identified');
      assert.equal(result.originalBrowserOutcome, 'scenario_completed'); assert.equal(result.originalBrowserFailure, null);
      value.report.outcome = 'failed'; value.report.failure = 'candidate_scenario_failed';
      assert.equal(closer.tvDiagnosticUIResult(value.report, value.manifest, value.physical).originalBrowserFailure, 'candidate_scenario_failed');
      for (const mutate of [row => { row.cleanup.tokens_rejected = false; }, row => { row.cleanup.browser_closed = false; },
        row => { row.cleanup.ui_failures = [{ operation: 'logout' }]; }, row => { row.observer.pending = [1]; }, row => { row.observer.drain_timeouts = [1]; }]) {
        const report = clone(value.report); mutate(report); assert.throws(() => closer.tvDiagnosticUIResult(report, value.manifest, value.physical));
      }
      const clean = nativeTVFixture(closer); clean.report.page_errors = [];
      const broken = { ...clean.physical, exchanges: [{ get scope() { throw new Error('unexpected_guard_fault'); } }] };
      assert.throws(() => closer.tvDiagnosticUIResult(clean.report, clean.manifest, broken), /unexpected_guard_fault/);
    }],
    ['tv_native_response_requires_unique_physical_identity_and_exact_context', () => {
      const value = nativeTVFixture(closer), result = closer.tvNativeResponseDiagnostic(value.report, value.physical.exchanges);
      assert.equal(result.responseMatches.length, 1); assert.equal(result.responseMatches[0].physicalOrdinal, 2);
      assert.equal(result.responseMatches[0].pageErrorAttribution, false);
      for (const mutate of [row => { row.report.requests[0].status = 403; }, row => { row.report.requests[0].url += '?different=1'; },
        row => { row.report.requests[0].token_sha256 = '0'.repeat(64); }, row => { row.report.requests[0].elapsed_ms = 9000; },
        row => { row.report.native_rejections[0].association.state = 'ambiguous'; },
        row => { row.physical.exchanges.push({ ...row.physical.exchanges[1], ordinal: 3, complete: false }); },
        row => { row.report.native_rejections[0].reason = { kind: 'primitive', type: 'undefined', value: null }; },
        row => { row.report.native_rejections = []; }]) {
        const changed = nativeTVFixture(closer); mutate(changed);
        assert.equal(closer.tvNativeResponseDiagnostic(changed.report, changed.physical.exchanges).status, 'inconclusive');
      }
    }],
    ['current_runtime_selects_recovered_identity_without_rewriting_historical_epoch', () => {
      const value = currentRuntimeFixture(), before = sha(jsonBytes(value.epoch));
      const current = closer.validateCurrentRuntime(value.envelope, value.input, value.epoch, value.authorizedInput, value.records);
      assert.equal(current.candidateProcess.pid, 101); assert.equal(value.epoch.candidateProcess.pid, 100); assert.equal(sha(jsonBytes(value.epoch)), before);
      for (const mutate of [row => { row.envelope.current.candidateProcess.pid = 100; }, row => { row.envelope.current.candidateProcess.bootId = 'other'; },
        row => { row.envelope.current.postgresProcess.pid++; }, row => { row.envelope.current.lease.granted = false; },
        row => { row.envelope.current.lease.candidateConnection.pid = 100; }, row => { row.envelope.preserved.runtime.sha256 = '0'.repeat(64); },
        row => { row.authorizedInput.currentRuntime.sha256 = '0'.repeat(64); }, row => { row.records.observation.after.candidateProcess.pid = 100; },
        row => { row.records.observationReview.checks.uniqueLease = false; }, row => { row.records.leaseQueryResult.push(clone(row.records.leaseQueryResult[0])); }]) {
        const changed = currentRuntimeFixture(); mutate(changed);
        assert.throws(() => closer.validateCurrentRuntime(changed.envelope, changed.input, changed.epoch, changed.authorizedInput, changed.records));
      }
    }],
    ['version5_input_requires_current_runtime_without_relaxing_legacy_versions', () => {
      const value = currentRuntimeFixture(), pin = value.input.currentRuntime;
      const input = { kind: 'audited-candidate-client-closeout-input', version: 5, output: '/opt/goby-test/synthetic-tv/output', sources: {}, retainedBaseline: pin, serverLog: pin, currentRuntime: pin };
      for (const name of ['manifest', 'observation', 'summary', 'gatewayAttestation', 'gatewayIndex', 'runtimeEpoch', 'admission', 'seedBinding', 'sourceBefore', 'sourceAfter', 'boundary']) input[name] = pin;
      for (const name of ['closer', 'adapter', 'gateway', 'proxy', 'sessionProof', 'movie', 'audio', 'subtitles', 'tv']) input.sources[name] = pin;
      closer.validateCloseoutInput(input);
      for (const changed of [{ ...input, currentRuntime: null }, { ...input, version: 4 }, { ...input, serverLog: null }]) assert.throws(() => closer.validateCloseoutInput(changed));
      const missing = { ...input }; delete missing.currentRuntime; assert.throws(() => closer.validateCloseoutInput(missing));
    }],
    ['version5_log_binds_current_runtime_pin_and_recovered_process_only', () => {
      const value = currentRuntimeFixture(), current = closer.validateCurrentRuntime(value.envelope, value.input, value.epoch, value.authorizedInput, value.records);
      const base = path.dirname(value.input.currentRuntime.path), pin = name => ({ path: base + '/' + name, sha256: sha(name) });
      const beforeBytes = Buffer.from('{"msg":"before"}\n'), afterBytes = Buffer.concat([beforeBytes, Buffer.from('{"msg":"after"}\n')]);
      const manifest = { runId: 'tv-browse-02', scenario: 'tv-browse', serverId: 'a'.repeat(32), source: { manifestSha256: sha('manifest'), binarySha256: sha('binary') }, output: base + '/browser' };
      const input = { ...value.input, retainedBaseline: pin('review.json'), serverLog: pin('server-log.json'), sources: {} };
      const authorizedInput = { ...value.authorizedInput, kind: 'audited-candidate-client-run-input', runId: manifest.runId, output: base, retainedBaseline: input.retainedBaseline, sources: {} };
      const authorization = pin('run-input.json'), approval = { kind: 'audited-candidate-client-approval', runId: manifest.runId, scenario: manifest.scenario,
        sourceManifestSha256: manifest.source.manifestSha256, binarySha256: manifest.source.binarySha256, serverId: manifest.serverId, isolatedCandidate: true, authorizedRunInput: authorization };
      const snapshot = (name, capturedAt, bytes) => ({ capturedAt, candidateBefore: clone(current.candidateProcess), candidateAfter: clone(current.candidateProcess),
        file: { path: closer.CANDIDATE_LOG, device: '1', inode: '2', uid: 0, mode: 0o600, links: 1 }, length: bytes.length,
        content: { path: base + '/' + name, sha256: sha(bytes) } });
      const receipt = { kind: 'audited-candidate-client-server-log', version: 2, runId: manifest.runId, runtimeEpoch: input.runtimeEpoch, currentRuntime: input.currentRuntime,
        input: authorization, controller: pin('run-audited-candidate-client.py'), before: snapshot('before.raw', '2026-01-01T00:00:02Z', beforeBytes),
        after: snapshot('after.raw', '2026-01-01T00:00:05Z', afterBytes) };
      const context = { manifest, epoch: value.epoch, input, approval, authorizedInput, currentIdentity: current, observation: { started_at: '2026-01-01T00:00:03Z', elapsed_ms: 1000 } };
      assert.equal(closer.validateServerLog(receipt, beforeBytes, afterBytes, context).events.length, 0);
      for (const mutate of [row => { row.version = 1; }, row => { row.currentRuntime = pin('other.json'); },
        row => { row.before.candidateBefore = clone(value.epoch.candidateProcess); }]) {
        const changed = clone(receipt); mutate(changed); assert.throws(() => closer.validateServerLog(changed, beforeBytes, afterBytes, context));
      }
      assert.throws(() => closer.validateServerLog(receipt, beforeBytes, afterBytes, { ...context, authorizedInput: { ...authorizedInput, currentRuntime: pin('other.json') } }));
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
    ['runtime_binary_successor_preserves_the_fixed_environment_ancestor_and_seed', () => {
      const value = binarySuccessorFixture(closer), before = jsonBytes(value);
      assert.equal(closer.validateRuntimeLineage(value.epoch, value.seed, value.lineage), undefined);
      assert.deepEqual(closer.runtimeEpochJSON(jsonBytes(value.epoch), value.seed.runtimeEpoch), value.epoch);
      assert.deepEqual(jsonBytes(value), before);
      assert.equal(value.seed.currentSessions.length, 15);
      assert.equal(value.lineage.previousBinding.currentSessions.filter(row => row.credentialId === null).length, 2);
      assert(value.seed.currentSessions.every(row => typeof row.credentialId === 'string'));
      assert.notDeepEqual(value.epoch.productInput, value.epoch.configurationInput);
      assert.notDeepEqual(value.seed.runtimeEpoch, closer.BINARY_SUCCESSOR.previousEpoch);
    }],
    ['runtime_binary_successor_rejects_another_or_incomplete_ancestor_chain', () => {
      for (const [name, mutate, expected] of [
        ['other_parent', value => { value.epoch.previousEpoch.sha256 = sha('another-parent'); }, /binary_successor_ancestor/],
        ['other_parent_path', value => { value.epoch.previousEpoch.path += '.other'; }, /binary_successor_ancestor/],
        ['other_binding', value => { value.seed.previousBinding.sha256 = sha('another-binding'); }, /binary_successor_ancestor/],
        ['parent_version', value => { value.lineage.previousEpoch.version = 1; }, /binary_successor_ancestor/],
        ['grandparent_path', value => { value.lineage.previousEpoch.previousEpoch.path += '.other'; }, /binary_successor_ancestor/],
        ['grandparent_failed_calls', value => { value.lineage.previousLineage.previousEpoch.calls.replace = 2; }, /binary_epoch_calls_or_sessions/],
        ['configuration_parent_drift', value => { value.lineage.previousEpoch.configurationChange.additions.GOBY_MEDIA_ROOTS = '/other'; }, /environment_configuration_changed/],
        ['missing_epoch_field', value => { delete value.epoch.configurationInput; }, /runtime_lineage_schema/],
        ['old_epoch_extras', value => { value.epoch.configurationChange = {}; }, /runtime_lineage_schema/],
      ]) {
        const value = binarySuccessorFixture(closer); mutate(value);
        assert.throws(() => closer.validateRuntimeLineage(value.epoch, value.seed, value.lineage), expected, name);
      }
    }],
    ['runtime_binary_successor_rejects_product_configuration_and_full_report_substitution', () => {
      const workerChange = mutate => value => { mutate(value.lineage.fullWorker); value.lineage.fullReport.worker = clone(value.lineage.fullWorker); };
      for (const [name, mutate, expected] of [
        ['old_transition_input', value => { value.lineage.transitionInput.version = 1; }, /binary_successor_input_schema/],
        ['unreviewed_input_key', value => { value.lineage.transitionInput.reviewedStateContract = {}; }, /binary_successor_input_schema/],
        ['another_output_scope', value => { value.lineage.transitionInput.output += '/nested'; }, /binary_successor_input_scope/],
        ['another_review_source', value => { value.lineage.transitionInput.priorSource.sha256 = sha('other-source'); }, /binary_successor_input_authority/],
        ['another_source_manifest', value => { value.lineage.transitionInput.newSourceManifest.sha256 = sha('other-manifest'); }, /binary_successor_input_scope/],
        ['product_is_old_configuration', value => { value.epoch.productInput = clone(value.epoch.configurationInput); }, /binary_successor_product_configuration/],
        ['configuration_is_old_binary_input', value => { value.epoch.configurationInput = clone(value.lineage.previousEpoch.productInput); }, /binary_successor_product_configuration/],
        ['another_archive', value => { value.epoch.currentSource.archiveSha256 = sha('other-archive'); }, /binary_successor_product_source/],
        ['input_binary_mismatch', value => { value.lineage.transitionInput.newBinary.sha256 = sha('other-binary'); }, /binary_successor_product_source/],
        ['full_still_running', value => { value.lineage.fullReport.status = 'running'; }, /binary_successor_full_report_incomplete/],
        ['full_failed_unit', value => { value.lineage.fullReport.unit_exit_code = 1; }, /binary_successor_full_report_incomplete/],
        ['full_worker_mismatch', value => { value.lineage.fullReport.worker.binary.sha256 = sha('other-worker'); }, /binary_successor_full_report_incomplete/],
        ['full_source_changed', workerChange(worker => { worker.cleanup.source_unchanged = false; }), /binary_successor_full_cleanup/],
        ['full_missing_package', workerChange(worker => { worker.packages.pop(); }), /binary_successor_full_package_coverage/],
        ['full_skipped_test', workerChange(worker => { worker.test_counts.skipped = 1; }), /binary_successor_full_package_coverage/],
        ['full_other_binary', workerChange(worker => { worker.binary.sha256 = sha('other-compiled-binary'); }), /binary_successor_full_binary/],
        ['environment_changed', value => { value.epoch.candidate.runtime.sha256 = sha('other-environment'); }, /binary_successor_candidate_configuration/],
        ['postgres_changed', value => { value.epoch.postgresProcess.pid++; }, /binary_successor_candidate_configuration/],
        ['namespace_changed', value => { value.epoch.candidateProcess.networkNamespace = 'net:[other]'; }, /binary_successor_process_sandbox/],
        ['database_owner_changed', value => { value.epoch.candidate.databases.source.owner = 'other-owner'; }, /binary_successor_database_identity/],
      ]) {
        const value = binarySuccessorFixture(closer); mutate(value);
        assert.throws(() => closer.validateRuntimeLineage(value.epoch, value.seed, value.lineage), expected, name);
      }
    }],
    ['runtime_binary_successor_requires_the_reviewed_source_and_all_revoked_sessions', () => {
      for (const [name, mutate, expected] of [
        ['prior_source_pin', value => { value.seed.priorSource.sha256 = sha('other-prior-source'); }, /binary_successor_prior_authority/],
        ['reviewed_state_pin', value => { value.epoch.reviewedState.sha256 = sha('other-state'); }, /binary_successor_review_authority/],
        ['summary_state_binding', value => { value.lineage.reviewedSummary.state.sha256 = sha('other-summary-state'); }, /binary_successor_review_binding/],
        ['summary_not_preserved', value => { value.lineage.reviewedSummary.source.sequencesExact = false; }, /binary_successor_review_binding/],
        ['prior_closeout_epoch', value => { value.lineage.priorCloseout.inputEvidence.epoch.sha256 = sha('other-closeout-epoch'); }, /binary_successor_review_binding/],
        ['unearned_client_acceptance', value => { value.lineage.priorCloseout.clientAcceptance = true; }, /binary_successor_review_binding/],
        ['seed_actor_changed', value => { value.seed.actors.mp3.username = 'other-actor'; }, /binary_successor_seed_provenance/],
        ['source_table_inventory', value => { delete value.lineage.priorSource.tables.users; }, /binary_successor_prior_source/],
        ['missing_session', value => { value.lineage.priorSource.tables.sessions.pop(); }, /binary_successor_prior_source/],
        ['live_session', value => { value.lineage.priorSource.tables.sessions[0].revoked_at = null; }, /timestamp_invalid/],
        ['wrong_credential_kind', value => { value.lineage.priorSource.tables.sessions[0].kind = 'application_key'; }, /binary_successor_session_identity/],
        ['malformed_token_digest', value => { value.lineage.priorSource.tables.sessions[0].token_hash = '\\x00'; }, /binary_successor_session_identity/],
        ['duplicate_credential', value => { value.lineage.priorSource.tables.sessions[1].id = value.lineage.priorSource.tables.sessions[0].id; }, /binary_successor_session_collision/],
        ['duplicate_token', value => { value.lineage.priorSource.tables.sessions[1].token_hash = value.lineage.priorSource.tables.sessions[0].token_hash; }, /binary_successor_session_collision/],
        ['session_order', value => { value.seed.currentSessions.reverse(); }, /binary_successor_session_history/],
        ['session_projection_extra', value => { value.seed.currentSessions[0].privateToken = 'synthetic-extra'; }, /binary_successor_session_history/],
        ['lost_ancestor_credential', value => {
          const token = sha('synthetic-replaced-ancestor-token'); value.lineage.priorSource.tables.sessions[0].token_hash = '\\x' + token;
          value.seed.currentSessions[0].tokenSha256 = token;
        }, /binary_successor_session_history/],
        ['known_ancestor_id_mismatch', value => {
          for (const binding of [value.lineage.previousBinding, value.lineage.previousLineage.previousBinding]) binding.currentSessions[2].credentialId = 'f'.repeat(32);
        }, /binary_successor_session_history/],
        ['ancestor_token_hash_missing', value => {
          for (const binding of [value.lineage.previousBinding, value.lineage.previousLineage.previousBinding]) delete binding.currentSessions[0].tokenSha256;
        }, /binary_successor_session_history/],
        ['ancestor_id_field_missing', value => {
          for (const binding of [value.lineage.previousBinding, value.lineage.previousLineage.previousBinding]) delete binding.currentSessions[0].credentialId;
        }, /binary_successor_session_history/],
        ['ancestor_id_undefined_is_not_literal_null', value => {
          for (const binding of [value.lineage.previousBinding, value.lineage.previousLineage.previousBinding]) binding.currentSessions[0].credentialId = undefined;
        }, /binary_successor_session_history/],
      ]) {
        const value = binarySuccessorFixture(closer); mutate(value);
        assert.throws(() => closer.validateRuntimeLineage(value.epoch, value.seed, value.lineage), expected, name);
      }
    }],
    ['candidate_admission_requires_legacy_v2_or_fresh_affected_v3_for_its_epoch', () => {
      const value = affectedAdmissionFixture(closer), original = jsonBytes(value);
      const oldInput = { runtimeEpoch: value.previous.runtimeEpoch, seedBinding: value.previous.seedRuntimeBinding };
      for (const version of [1, 2]) assert.equal(closer.validateCandidateAdmissionBinding(oldInput, { ...value.lineage.previousEpoch, version }, value.previous), undefined);
      assert.equal(closer.validateCandidateAdmissionBinding(value.input, value.epoch, value.admission, value.lineage, value.previous), undefined);
      assert.deepEqual(jsonBytes(value), original);
      const oldRelabeled = { ...clone(value.previous), runtimeEpoch: clone(value.input.runtimeEpoch), seedRuntimeBinding: clone(value.input.seedBinding), currentSource: clone(value.epoch.currentSource) };
      assert.throws(() => closer.validateCandidateAdmissionBinding(value.input, value.epoch, oldRelabeled, value.lineage, value.previous), /candidate_live_admission_binding/);
      assert.throws(() => closer.validateCandidateAdmissionBinding(oldInput, value.lineage.previousEpoch, { ...clone(value.previous), version: 3 }), /candidate_live_admission_binding/);
      const legacyWithoutClientClaim = clone(value.previous); delete legacyWithoutClientClaim.clientAcceptance;
      assert.equal(closer.validateCandidateAdmissionBinding(oldInput, value.lineage.previousEpoch, legacyWithoutClientClaim), undefined);
      for (const [field, invalid] of [['candidateAdmissionComplete', 1], ['candidateAdmissionComplete', 'true']])
        assert.throws(() => closer.validateCandidateAdmissionBinding(oldInput, value.lineage.previousEpoch, { ...clone(value.previous), [field]: invalid }), /candidate_live_admission_binding/);
    }],
    ['affected_tv_admission_requires_exact_fresh_checks_and_current_epoch_bindings', () => {
      for (const key of Object.keys(affectedAdmissionFixture(closer).admission.freshChecks)) for (const invalid of [false, 1, 'true', null]) {
        const value = affectedAdmissionFixture(closer); value.admission.freshChecks[key] = invalid;
        assert.throws(() => closer.validateCandidateAdmissionBinding(value.input, value.epoch, value.admission, value.lineage, value.previous), /candidate_affected_tv_admission_checks/, key);
      }
      for (const [name, mutate, expected] of [
        ['wrong_admission_kind', value => { value.admission.admissionKind = 'general'; }, /candidate_affected_tv_admission_checks/],
        ['missing_check', value => { delete value.admission.freshChecks.sessionCleanup; }, /candidate_affected_tv_admission_checks/],
        ['extra_check', value => { value.admission.freshChecks.unreviewed = true; }, /candidate_affected_tv_admission_checks/],
        ['transition_closeout_changed', value => { value.admission.transitionCloseout.sha256 = sha('different-transition'); }, /candidate_affected_tv_admission_checks/],
        ['old_epoch', value => { value.admission.runtimeEpoch = clone(value.previous.runtimeEpoch); }, /candidate_live_admission_binding/],
        ['old_binding', value => { value.admission.seedRuntimeBinding = clone(value.previous.seedRuntimeBinding); }, /candidate_live_admission_binding/],
        ['old_source', value => { value.admission.currentSource = clone(value.previous.currentSource); }, /candidate_live_admission_binding/],
        ['failed_cleanup', value => { value.admission.cleanupFailures = [{ phase: 'logout' }]; }, /candidate_live_admission_binding/],
        ['failed_run', value => { value.admission.failure = { phase: 'fresh_tv' }; }, /candidate_live_admission_binding/],
        ['claimed_client_acceptance', value => { value.admission.clientAcceptance = true; }, /candidate_affected_tv_admission_checks/],
        ['numeric_client_acceptance', value => { value.admission.clientAcceptance = 0; }, /candidate_affected_tv_admission_checks/],
        ['numeric_complete', value => { value.admission.candidateAdmissionComplete = 1; }, /candidate_live_admission_binding/],
      ]) {
        const value = affectedAdmissionFixture(closer); mutate(value);
        assert.throws(() => closer.validateCandidateAdmissionBinding(value.input, value.epoch, value.admission, value.lineage, value.previous), expected, name);
      }
    }],
    ['affected_tv_admission_reuse_requires_the_exact_successful_cleaned_admission04', () => {
      for (const [name, mutate, expected] of [
        ['another_report', value => { value.admission.reusedAdmission04.report.sha256 = sha('different-admission'); }, /candidate_reused_admission04_binding/],
        ['another_report_path', value => { value.admission.reusedAdmission04.report.path += '.other'; }, /candidate_reused_admission04_binding/],
        ['missing_contract', value => { value.admission.reusedAdmission04.contracts.pop(); }, /candidate_reused_admission04_binding/],
        ['another_ancestor', value => { value.admission.reusedAdmission04.runtimeEpoch.sha256 = sha('other-parent'); }, /candidate_reused_admission04_binding/],
        ['another_ancestor_binding', value => { value.admission.reusedAdmission04.seedRuntimeBinding.sha256 = sha('other-binding'); }, /candidate_reused_admission04_binding/],
        ['another_ancestor_source', value => { value.admission.reusedAdmission04.currentSource.binary.sha256 = sha('other-product'); }, /candidate_reused_admission04_binding/],
        ['unlinked_current_epoch', value => { value.epoch.previousEpoch.sha256 = sha('other-parent'); }, /candidate_reused_admission04_binding/],
        ['failed_previous_report', value => { value.previous.status = 'admission_failed_resources_retained'; }, /candidate_live_admission_binding/],
        ['previous_source_changed', value => { value.previous.currentSource.binary.sha256 = sha('other-old-binary'); }, /candidate_live_admission_binding/],
        ['previous_numeric_complete', value => { value.previous.candidateAdmissionComplete = 1; }, /candidate_live_admission_binding/],
        ['previous_numeric_false', value => { value.previous.clientAcceptance = 0; }, /candidate_reused_admission04_cleanup/],
        ['previous_live_token', value => { value.previous.controllerSessions.P.sameTokenRejected = false; }, /candidate_reused_admission04_cleanup/],
        ['previous_numeric_rejection', value => { value.previous.controllerSessions.P.sameTokenRejected = 1; }, /candidate_reused_admission04_cleanup/],
        ['previous_stage_not_retained', value => { value.previous.inactiveStageRetained = 1; }, /candidate_reused_admission04_cleanup/],
        ['previous_generation_changed', value => { value.previous.activeGenerationChanged = 0; }, /candidate_reused_admission04_cleanup/],
      ]) {
        const value = affectedAdmissionFixture(closer); mutate(value);
        assert.throws(() => closer.validateCandidateAdmissionBinding(value.input, value.epoch, value.admission, value.lineage, value.previous), expected, name);
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
  const previousAdmission = await read(closer.REUSED_ADMISSION04);
  const previousAdmissionInput = { runtimeEpoch: seed.runtimeEpoch, seedBinding: seedPin };
  closer.validateCandidateAdmissionBinding(previousAdmissionInput, epoch, previousAdmission);
  assert.equal(previousAdmission.clientAcceptance, false);
  assert.throws(() => closer.validateCandidateAdmissionBinding(previousAdmissionInput, epoch, { ...clone(previousAdmission), candidateAdmissionComplete: 1 }), /candidate_live_admission_binding/);
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

async function savedCurrentLineageReplay(closer) {
  const root = '/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14';
  const pins = {
    runtimeEpoch: { path: root + '/candidate-tv-parent-transition-01/private/runtime-epoch.json', sha256: '76d7cc71be87851271272537795255f9ad7a5f5c3920dd6546e573f42d06bfac' },
    seedBinding: { path: root + '/candidate-tv-parent-transition-01/private/seed-runtime-binding.json', sha256: '94bd35e5523a56c60a9b712684d02785b05d6924820bb25f60c48ec8d3496c43' },
    admission05: { path: root + '/candidate-live-admission-05/private/report.json', sha256: 'b73a2d30926c68886bd1674a356e6330eab2072afb53fb1fa1f695c5337f8535' },
    admission04: closer.REUSED_ADMISSION04, transitionCloseout: closer.AFFECTED_TV_TRANSITION_CLOSEOUT,
  };
  const read = async pin => { const bytes = await fs.readFile(pin.path); assert.equal(sha(bytes), pin.sha256, 'current_replay_digest_changed'); return bytes; };
  const epoch = closer.runtimeEpochJSON(await read(pins.runtimeEpoch), pins.runtimeEpoch);
  const seed = closer.strictJSON(await read(pins.seedBinding)), admission = closer.strictJSON(await read(pins.admission05));
  const previousAdmission = closer.strictJSON(await read(pins.admission04));
  await read(pins.transitionCloseout);
  // Call the actual production loader, including its file metadata policy and
  // role-specific decoders. No copied loader or browser fixture is used here.
  const lineage = await closer.readBinarySuccessorLineage(epoch, seed);
  closer.validateRuntimeLineage(epoch, seed, lineage);
  closer.validateCandidateAdmissionBinding({ runtimeEpoch: pins.runtimeEpoch, seedBinding: pins.seedBinding }, epoch, admission, lineage, previousAdmission);
  assert.equal(epoch.version, 3); assert.equal(seed.version, 3); assert.equal(admission.version, 3);
  assert.equal(epoch.currentSource.binary.sha256, 'b0d6769cadc525b12d2970a206d8e141a39431ee72bb4f7be77bbeecf873ea42');
  return { kind: 'actual_saved_current_lineage_and_admission', pins, productionLoader: 'readBinarySuccessorLineage',
    epochVersion: epoch.version, admissionVersion: admission.version, admissionKind: admission.admissionKind,
    binarySha256: epoch.currentSource.binary.sha256, inheritedSessions: seed.currentSessions.length,
    browserInputsCreated: 0, httpCalls: 0, sqlCalls: 0, serviceCalls: 0, clientAcceptance: false };
}

const replayExactKeys = (value, keys) => value !== null && typeof value === 'object' && !Array.isArray(value) &&
  JSON.stringify(Object.keys(value).sort()) === JSON.stringify([...keys].sort());
async function readReplayJSON(closer, pin, decode = bytes => closer.strictJSON(bytes)) {
    assert(replayExactKeys(pin, ['path', 'sha256']) && typeof pin.path === 'string' && path.isAbsolute(pin.path) && !pin.path.split(path.sep).includes('..') && /^[a-f0-9]{64}$/.test(pin.sha256));
    const handle = await fs.open(pin.path, constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const before = await handle.stat({ bigint: true });
      assert(before.isFile() && before.uid === 0n && before.nlink === 1n && (before.mode & 0o777n) === 0o600n && before.size > 0n && before.size <= 33554432n);
      const bytes = await handle.readFile(), after = await handle.stat({ bigint: true }), current = await fs.lstat(pin.path, { bigint: true });
      assert(['dev', 'ino', 'size', 'mtimeNs', 'ctimeNs', 'mode'].every(key => before[key] === after[key] && before[key] === current[key]));
      assert.equal(sha(bytes), pin.sha256); return decode(bytes, pin);
    } finally { await handle.close(); }
}

async function savedReviewedBaselineReplay(closer, inputPin) {
  const exactKeys = replayExactKeys, read = pin => readReplayJSON(closer, pin);
  const input = await read(inputPin);
  assert(exactKeys(input, ['kind', 'version', 'cases']) && input.kind === 'audited-candidate-reviewed-baseline-replay-input' && input.version === 1);
  assert.deepEqual(input.cases.map(row => row.name), ['movie05', 'movie06', 'latest-subtitles']);
  const expected = { movie05: [13, 5, 0], movie06: [14, 6, 0], 'latest-subtitles': [21, 13, 2] }, results = [];
  for (const entry of input.cases) {
    assert(exactKeys(entry, ['name', 'review', 'seed', 'inputBinding']) && exactKeys(entry.inputBinding, ['runtimeEpoch', 'seedBinding']));
    assert.deepEqual(entry.seed, entry.inputBinding.seedBinding);
    const seed = await read(entry.seed), retained = await closer.readReviewedMovieBaseline(entry.review, entry.inputBinding, seed);
    const manifest = { scenario: 'movie', actor: retained.review.actor }, snapshot = retained.snapshot, original = sha(jsonBytes(retained));
    assert.deepEqual([snapshot.tables.sessions.length, snapshot.tables.play_sessions.length, snapshot.tables.client_playback_references.length], expected[entry.name]);
    assert.equal(retained.review.preparedExpirations.length, 1);
    closer.verifyRetainedMovieBefore(clone(snapshot), manifest, seed, retained);
    closer.validateFinalProvenance({ ...retained.movieProvenance, boundary: null }, { ...retained.review.movieProvenance, boundary: null }, 'movie', seed);
    const finalClientProvenanceRoles = ['movie'];
    if (retained.manifest.scenario === 'subtitles') {
      closer.validateFinalProvenance(Object.fromEntries(['closeout', 'snapshot', 'sourceEpoch', 'manifest', 'boundary'].map(key => [key, retained[key]])),
        Object.fromEntries(['closeout', 'snapshot', 'sourceEpoch', 'manifest', 'boundary'].map(key => [key, retained.review[key]])), 'subtitles', seed);
      finalClientProvenanceRoles.push('subtitles');
    }
    let rejectedChanges = 0;
    for (const table of Object.keys(snapshot.tables)) {
      const changed = clone(snapshot); changed.tables[table].push({ unreviewed: true });
      assert.throws(() => closer.verifyRetainedMovieBefore(changed, manifest, seed, retained), /retained_movie_fresh_state_changed/); rejectedChanges++;
    }
    for (const name of Object.keys(snapshot.sequences)) {
      const changed = clone(snapshot); changed.sequences[name].lastValue = changed.sequences[name].lastValue === '0' ? '1' : '0';
      assert.throws(() => closer.verifyRetainedMovieBefore(changed, manifest, seed, retained), /retained_movie_fresh_state_changed/); rejectedChanges++;
    }
    if (entry.name === 'latest-subtitles') {
      assert.equal(retained.manifest.scenario, 'subtitles'); assert.equal(retained.movieProvenance.manifest.scenario, 'movie');
      assert(snapshot.tables.client_playback_references.every(row => row.user_id !== manifest.actor.id));
      const changed = clone(snapshot); changed.tables.client_playback_references[0].client_nonce += '-changed';
      assert.throws(() => closer.verifyRetainedMovieBefore(changed, manifest, seed, retained), /retained_movie_fresh_state_changed/); rejectedChanges++;
    } else assert.equal(retained.manifest.scenario, 'movie');
    assert.equal(sha(jsonBytes(retained)), original);
    results.push({ name: entry.name, review: entry.review, sourceSnapshot: retained.review.snapshot, movieProvenance: retained.review.movieProvenance,
      tablesCompared: Object.keys(snapshot.tables).length, sequencesCompared: Object.keys(snapshot.sequences).length,
      rejectedSnapshotChanges: rejectedChanges, foreignClientReferencesPreserved: snapshot.tables.client_playback_references.length,
      preparedExpirationsExplicitlyReviewed: retained.review.preparedExpirations.length, productionLoader: 'readReviewedMovieBaseline',
      originalInMemoryEvidenceUnchanged: true, finalClientProvenanceRoles, browserInputsCreated: 0, httpCalls: 0, sqlCalls: 0, serviceCalls: 0, clientAcceptance: false });
  }
  return results;
}

async function savedReviewedTVBaselineReplay(closer, inputPin) {
  const input = await readReplayJSON(closer, inputPin);
  assert(replayExactKeys(input, ['kind', 'version', 'review', 'seed', 'inputBinding']) && input.kind === 'audited-candidate-reviewed-tv-baseline-replay-input' && input.version === 1 &&
    replayExactKeys(input.inputBinding, ['runtimeEpoch', 'seedBinding']));
  assert.deepEqual(input.seed, input.inputBinding.seedBinding);
  const seed = await readReplayJSON(closer, input.seed), retained = await closer.readReviewedTVBaseline(input.review, input.inputBinding, seed);
  const snapshot = retained.snapshot, manifest = { scenario: 'tv-browse', actor: retained.review.actor }, before = sha(jsonBytes(retained));
  assert.equal(retained.manifest.scenario, 'subtitles'); assert.equal(retained.tvProvenance.manifest.scenario, 'tv-browse');
  assert.deepEqual([snapshot.tables.sessions.length, snapshot.tables.play_sessions.length, snapshot.tables.client_playback_references.length], [21, 13, 2]);
  assert.equal(Object.keys(snapshot.tables).length, 35); assert.equal(Object.keys(snapshot.sequences).length, 5);
  assert.equal(retained.review.preparedExpirations.length, 1);
  closer.verifyReviewedTVBefore(clone(snapshot), manifest, seed, retained);
  for (const table of Object.keys(snapshot.tables)) {
    const changed = clone(snapshot);
    if (changed.tables[table].length) changed.tables[table][0].unreviewed = true; else changed.tables[table].push({ unreviewed: true });
    assert.throws(() => closer.verifyReviewedTVBefore(changed, manifest, seed, retained), /reviewed_tv_fresh_state_changed/);
  }
  for (const sequence of Object.keys(snapshot.sequences)) {
    const changed = clone(snapshot); changed.sequences[sequence].lastValue = changed.sequences[sequence].lastValue === '0' ? '1' : '0';
    assert.throws(() => closer.verifyReviewedTVBefore(changed, manifest, seed, retained), /reviewed_tv_fresh_state_changed/);
  }
  assert.equal(sha(jsonBytes(retained)), before);
  return { review: input.review, sourceSnapshot: retained.review.snapshot, tvProvenance: retained.review.tvProvenance,
    tablesCompared: 35, sequencesCompared: 5, rejectedTableMutations: 35, rejectedSequenceMutations: 5, foreignClientReferencesPreserved: 2,
    preparedExpirationsExplicitlyReviewed: 1, productionLoader: 'readReviewedTVBaseline', originalInMemoryEvidenceUnchanged: true,
    browserInputsCreated: 0, httpCalls: 0, sqlCalls: 0, serviceCalls: 0, clientAcceptance: false };
}

async function savedEpisodeProvenanceReplay(closer, inputPin) {
  const input = await readReplayJSON(closer, inputPin), roles = ['closeout', 'snapshot', 'sourceEpoch', 'manifest', 'boundary'];
  assert(replayExactKeys(input, ['kind', 'version', 'scenario', 'seed', 'provenance']) && input.kind === 'audited-candidate-episode-provenance-replay-input' &&
    input.version === 1 && input.scenario === 'episode' && replayExactKeys(input.provenance, roles), 'episode_provenance_replay_input_schema');
  const seed = await readReplayJSON(closer, input.seed), pins = input.provenance, bundle = {};
  for (const role of roles) bundle[role] = await readReplayJSON(closer, pins[role], role === 'snapshot' ? bytes => closer.sourceSnapshotJSON(bytes) :
    role === 'sourceEpoch' ? (bytes, pin) => closer.runtimeEpochJSON(bytes, pin) : bytes => closer.strictJSON(bytes));
  const original = sha(jsonBytes({ seed, bundle }));
  assert.equal(seed.version, 3); assert.equal(bundle.sourceEpoch.version, 3); assert.deepEqual(seed.runtimeEpoch, pins.sourceEpoch);
  const rows = closer.validateFinalProvenance(bundle, pins, 'episode', seed), snapshot = bundle.snapshot;
  assert.equal(Object.keys(snapshot.tables).length, 35); assert.equal(Object.keys(snapshot.sequences).length, 5);
  assert.deepEqual([snapshot.tables.sessions.length, snapshot.tables.play_sessions.length, snapshot.tables.client_playback_references.length], [20, 11, 2]);
  assert.equal(rows.play_sessions.length, 2); assert.equal(rows.play_sessions.filter(row => row.state === 'Prepared').length, 1);
  assert.equal(sha(jsonBytes({ seed, bundle })), original);
  return { input: inputPin, provenance: pins, seed: input.seed, productionValidator: 'validateFinalProvenance', sessions: 20, plays: 11,
    foreignClientReferences: 2, actorPlays: 2, actorPrepared: 1, tables: 35, sequences: 5,
    unchangedEvidenceSha256: original, usedAsCurrentBaseline: false,
    browserInputsCreated: 0, httpCalls: 0, sqlCalls: 0, serviceCalls: 0, clientAcceptance: false };
}

async function main() {
  const args = process.argv.slice(2);
  const episode = args.length >= 8 && args.at(-4) === '--episode-provenance' && args.at(-2) === '--episode-provenance-sha256';
  const episodePin = episode ? { path: args.at(-3), sha256: args.at(-1) } : null;
  if (episode) args.splice(-4);
  const replay = args.length === 5 && args[4] === '--replay-retained-snapshot';
  const reviewed = args.length === 8 && args[4] === '--reviewed-baselines' && args[6] === '--reviewed-baselines-sha256';
  const reviewedTV = args.length === 8 && args[4] === '--reviewed-tv-baseline' && args[6] === '--reviewed-tv-baseline-sha256';
  assert((args.length === 4 || replay || reviewed || reviewedTV) && args[0] === '--source' && args[2] === '--output', 'arguments_required');
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
  let replayedPins = [], currentLineageReplay = null, reviewedBaselineReplay = [], reviewedTVBaselineReplay = null, episodeProvenanceReplay = null;
  if (replay) {
    try { replayedPins = await savedRetainedReplay(closer); tests.push({ name: 'saved_movie04_complete_durable_comparison_with_synthetic_new_play', outcome: 'passed' }); }
    catch (error) { tests.push({ name: 'saved_movie04_complete_durable_comparison_with_synthetic_new_play', outcome: 'failed', errorType: error.name,
      failedCheck: /^[a-z0-9_]+$/.test(error.message ?? '') ? error.message : 'saved_replay_assertion_failed' }); }
    try { currentLineageReplay = await savedCurrentLineageReplay(closer); tests.push({ name: 'saved_current_epoch3_admission05_through_production_loader', outcome: 'passed' }); }
    catch (error) { tests.push({ name: 'saved_current_epoch3_admission05_through_production_loader', outcome: 'failed', errorType: error.name,
      failedCheck: /^[a-z0-9_]+$/.test(error.message ?? '') ? error.message : 'current_saved_replay_assertion_failed' }); }
  }
  if (reviewed) {
    try {
      reviewedBaselineReplay = await savedReviewedBaselineReplay(closer, { path: args[5], sha256: args[7] });
      for (const row of reviewedBaselineReplay) tests.push({ name: 'saved_reviewed_baseline_' + row.name.replaceAll('-', '_'), outcome: 'passed' });
    } catch (error) { tests.push({ name: 'saved_reviewed_baselines', outcome: 'failed', errorType: error.name,
      failedCheck: /^[a-z0-9_]+$/.test(error.message ?? '') ? error.message : 'saved_reviewed_baseline_assertion_failed' }); }
  }
  if (reviewedTV) {
    try { reviewedTVBaselineReplay = await savedReviewedTVBaselineReplay(closer, { path: args[5], sha256: args[7] });
      tests.push({ name: 'saved_reviewed_tv_baseline', outcome: 'passed' }); }
    catch (error) { tests.push({ name: 'saved_reviewed_tv_baseline', outcome: 'failed', errorType: error.name,
      failedCheck: /^[a-z0-9_]+$/.test(error.message ?? '') ? error.message : 'saved_reviewed_tv_assertion_failed' }); }
  }
  if (episode) {
    try { episodeProvenanceReplay = await savedEpisodeProvenanceReplay(closer, episodePin);
      tests.push({ name: 'saved_episode_provenance', outcome: 'passed' }); }
    catch (error) { tests.push({ name: 'saved_episode_provenance', outcome: 'failed', errorType: error.name,
      failedCheck: /^[a-z0-9_]+$/.test(error.message ?? '') ? error.message : 'saved_episode_provenance_assertion_failed' }); }
  }
  const unchanged = (await Promise.all(Object.entries(sources).map(async ([filename, digest]) => sha(await fs.readFile(filename)) === digest))).every(Boolean);
  const report = { kind: 'audited-candidate-client-closeout-pure-guards', version: 1,
    scope: 'Pure synthetic fixtures and optional explicitly pinned saved-state replay through production loaders. No browser input, HTTP, SQL, service operation, or client acceptance claim.', replayedPins, currentLineageReplay, reviewedBaselineReplay, reviewedTVBaselineReplay, episodeProvenanceReplay,
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
  if (tests.length !== (reviewedTV ? 63 : reviewed ? 65 : replay ? 64 : 62) + Number(episode) || report.failed || !unchanged) process.exitCode = 1;
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  try { await main(); } catch { process.stderr.write('pure_closeout_guard_setup_failed\n'); process.exitCode = 1; }
}
