/** Pure v3 guards plus read-only verification of the explicitly pinned retained baseline. */
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import { createHash } from 'node:crypto';
import { CANDIDATE_LOG, OCCUPIED_BASELINE, OCCUPIED_SNAPSHOT, OCCUPIED_PLAY, OCCUPIED_AUTH,
  strictJSON, sourceSnapshotJSON, parseHead, validateServerLog, cancelledEmptyMedia,
  validateOccupiedMovieBaseline, verifyRetainedMovieBefore } from './close-audited-candidate-client.mjs';
import * as closer from './close-audited-candidate-client.mjs';
import { retainedFixture } from './test-close-audited-candidate-client.mjs';

const ROOT = '/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14';
const SEED = { path: ROOT + '/candidate-backup-limits-revision-01/private/seed-runtime-binding.json',
  sha256: '92ee92478475390e514f39e554619f322be81062a6d0256820c00bf4c8e0969f' };
const hash = bytes => createHash('sha256').update(bytes).digest('hex');
const clone = value => structuredClone(value);
const pin = (name, value = name) => ({ path: ROOT + '/synthetic-v3/' + name, sha256: hash(value) });
const line = value => Buffer.from(JSON.stringify(value) + '\n');
const REQUEST_ID = 'a'.repeat(32), ITEM = 'b'.repeat(32);

function serverFixture() {
  const process = { bootId: '00000000-0000-4000-8000-000000000001', pid: 12345, startTicks: '123456', uid: 0,
    exe: '/synthetic/goby', exeDevice: 8, exeInode: 9, cmdline: ['/synthetic/goby'],
    cgroup: '0::/synthetic\n', networkNamespace: 'net:[123456]' };
  const manifest = { runId: 'synthetic-v3-movie', scenario: 'movie', serverId: 'c'.repeat(32),
    source: { manifestSha256: hash('source'), binarySha256: hash('binary') }, output: ROOT + '/synthetic-v3/browser' };
  const input = { runtimeEpoch: pin('epoch.json'), retainedBaseline: OCCUPIED_BASELINE,
    serverLog: pin('private/server-log.json'), sources: { closer: pin('close-audited-candidate-client.mjs') } };
  const authorizedInput = { kind: 'audited-candidate-client-run-input', version: 3, runId: manifest.runId,
    scenario: 'movie', output: ROOT + '/synthetic-v3', runtimeEpoch: input.runtimeEpoch,
    retainedBaseline: input.retainedBaseline, sources: input.sources };
  const authorization = pin('run-input.json');
  const approval = { kind: 'audited-candidate-client-approval', runId: manifest.runId, scenario: manifest.scenario,
    sourceManifestSha256: manifest.source.manifestSha256, binarySha256: manifest.source.binarySha256,
    serverId: manifest.serverId, isolatedCandidate: true, authorizedRunInput: authorization };
  const file = { path: CANDIDATE_LOG, device: '8', inode: '900', uid: 0, mode: 0o600, links: 1 };
  const event = { time: '2026-09-13T00:00:01.500Z', level: 'INFO', msg: 'request completed', event: 'request.completed',
    request_id: REQUEST_ID, route: 'GET /emby/Videos/{Id}/{StreamFileName}', method: 'GET', status: 200,
    bytes: 0, duration_ms: 2, outcome: 'cancelled' };
  const beforeBytes = line({ time: '2026-09-12T23:59:59Z', level: 'INFO', msg: 'server ready' });
  const snapshot = (label, capturedAt, bytes) => ({ capturedAt, candidateBefore: clone(process), candidateAfter: clone(process),
    file: clone(file), length: bytes.length, content: pin('private/server-stdout-' + label + '.raw', bytes) });
  const afterBytes = Buffer.concat([beforeBytes, line(event)]);
  const receipt = { kind: 'audited-candidate-client-server-log', version: 1, runId: manifest.runId,
    runtimeEpoch: input.runtimeEpoch, input: authorization, controller: pin('source/run-audited-candidate-client.py'),
    before: snapshot('before', '2026-09-13T00:00:00Z', beforeBytes), after: snapshot('after', '2026-09-13T00:00:03Z', afterBytes) };
  return { receipt, beforeBytes, afterBytes, event,
    context: { manifest, epoch: { candidateProcess: process }, input, approval,
      observation: { started_at: '2026-09-13T00:00:01Z', elapsed_ms: 1000 }, authorizedInput } };
}

function replaceAppend(fixture, bytes) {
  fixture.afterBytes = Buffer.concat([fixture.beforeBytes, bytes]);
  fixture.receipt.after.length = fixture.afterBytes.length;
  fixture.receipt.after.content.sha256 = hash(fixture.afterBytes);
}

const validate = fixture => validateServerLog(fixture.receipt, fixture.beforeBytes, fixture.afterBytes, fixture.context);

function emptyExchange() {
  const response = parseHead(Buffer.from('HTTP/1.1 200 OK\r\nContent-Length: 0\r\nX-Request-Id: ' + REQUEST_ID + '\r\n\r\n'), true);
  return { ordinal: 9, scope: { allowed: true, kind: 'media' }, original: { method: 'GET' },
    complete: true, requestDelivered: true, deliveredBodyBytes: 0, response,
    url: new URL('http://127.0.0.1:19196/emby/Videos/' + ITEM + '/original.mp4'),
    intent: { startedAt: '2026-09-13T00:00:01.400Z' },
    result: { completedAt: '2026-09-13T00:00:01.600Z', responseBodyWireBytes: 0, requestBodyWireBytes: 0, requestForwardedComplete: true } };
}

test('v3 log validation binds the approved run and indexes only complete appended request records', () => {
  const fixture = serverFixture(), before = Buffer.from(fixture.beforeBytes), after = Buffer.from(fixture.afterBytes);
  const proof = validate(fixture);
  assert.equal(proof.events.length, 1); assert.equal(proof.byRequestId.get(REQUEST_ID).outcome, 'cancelled');
  assert.equal(proof.receipt, fixture.receipt); assert.deepEqual(fixture.beforeBytes, before); assert.deepEqual(fixture.afterBytes, after);
  const ignored = serverFixture(); replaceAppend(ignored, line({ time: '2026-09-13T00:00:01Z', msg: 'another structured event' }));
  assert.equal(validate(ignored).events.length, 0);
  const appendedDuringRead = serverFixture(); appendedDuringRead.event.time = '2026-09-12T23:59:59.999Z';
  replaceAppend(appendedDuringRead, line(appendedDuringRead.event));
  assert.equal(validate(appendedDuringRead).events.length, 1);
  assert.equal(cancelledEmptyMedia(emptyExchange(), validate(appendedDuringRead)), null);
});

test('v3 log receipts reject foreign run, epoch, authorization, process, and stdout authority', () => {
  for (const mutate of [
    row => { row.receipt.runId = 'another-run'; }, row => { row.receipt.runtimeEpoch = pin('foreign-epoch.json'); },
    row => { row.context.approval.authorizedRunInput = pin('foreign-input.json'); },
    row => { row.context.approval.isolatedCandidate = false; }, row => { row.context.authorizedInput.version = 2; },
    row => { row.context.authorizedInput.output += '/another'; },
    row => { row.context.authorizedInput.sources = { closer: pin('different.mjs') }; },
    row => { row.receipt.controller.path = '/tmp/run-audited-candidate-client.py'; },
    row => { row.receipt.before.candidateBefore.pid++; }, row => { row.receipt.after.candidateAfter.startTicks = 'different'; },
    row => { row.receipt.after.file.inode = '901'; }, row => { row.receipt.before.file.path += '.rotated'; },
    row => { row.receipt.before.file.mode = 0o644; }, row => { row.receipt.before.file.links = 2; },
    row => { row.receipt.before.file.uid = 1; }, row => { row.receipt.before.file.device = 8; },
    row => { row.receipt.before.content.path = ROOT + '/foreign-parent/before.raw'; },
  ]) { const fixture = serverFixture(); mutate(fixture); assert.throws(() => validate(fixture)); }
});

test('v3 log validation rejects altered prefixes, missing final newlines, aliases, hashes, and capture windows', () => {
  for (const mutate of [
    row => { row.afterBytes[0] = 32; row.receipt.after.content.sha256 = hash(row.afterBytes); },
    row => { row.afterBytes = row.afterBytes.subarray(0, -1); row.receipt.after.length--; row.receipt.after.content.sha256 = hash(row.afterBytes); },
    row => { row.receipt.after.content.path = row.receipt.before.content.path; },
    row => { row.receipt.after.content.sha256 = hash('changed'); }, row => { row.receipt.before.length = 0; },
    row => { row.receipt.after.length = 32 * 1048576 + 1; },
    row => { row.receipt.before.capturedAt = '2026-09-13T00:00:01.100Z'; },
    row => { row.receipt.after.capturedAt = '2026-09-13T00:00:01.900Z'; },
  ]) { const fixture = serverFixture(); mutate(fixture); assert.throws(() => validate(fixture)); }
});

test('v3 appended logs reject duplicate IDs, duplicate JSON keys, invalid structured fields, and out-of-window events', () => {
  const duplicate = serverFixture(); replaceAppend(duplicate, Buffer.concat([line(duplicate.event), line(duplicate.event)]));
  assert.throws(() => validate(duplicate), /server_log_request_event_invalid/);
  const json = serverFixture(); replaceAppend(json, Buffer.from('{"msg":"request completed","msg":"request completed"}\n'));
  assert.throws(() => validate(json), /json_duplicate_key/);
  const encoding = serverFixture(); replaceAppend(encoding, Buffer.from([0xff, 10])); assert.throws(() => validate(encoding));
  for (const mutate of [
    event => { event.request_id = 'invalid'; }, event => { event.level = 'ERROR'; }, event => { event.bytes = -1; },
    event => { event.duration_ms = -1; }, event => { event.status = 700; }, event => { event.outcome = 'unknown'; },
    event => { event.time = '2026-09-13T00:00:04Z'; },
  ]) { const fixture = serverFixture(); mutate(fixture.event); replaceAppend(fixture, line(fixture.event)); assert.throws(() => validate(fixture)); }
});

test('a cancelled empty GET uses response framing and contributes no media delivery', () => {
  const fixture = serverFixture(), exchange = emptyExchange(), original = JSON.stringify(exchange);
  const result = cancelledEmptyMedia(exchange, validate(fixture));
  assert.deepEqual(result, { ordinal: 9, requestId: REQUEST_ID, interpretation: 'server_cancelled_empty_response', status: 200,
    deliveredBodyBytes: 0, contributesMediaDelivery: false, serverEventTime: fixture.event.time });
  assert.equal(Object.hasOwn(exchange.original, 'content-length'), false);
  assert.equal(JSON.stringify(exchange), original);
});

test('cancelled-empty classification rejects missing, contradictory, duplicate-header, or foreign evidence', () => {
  for (const mutate of [
    row => { row.scope.allowed = false; }, row => { row.original.method = 'HEAD'; }, row => { row.complete = false; },
    row => { row.response.status = 206; }, row => { row.deliveredBodyBytes = 1; }, row => { row.result.responseBodyWireBytes = 1; },
    row => { row.result.requestBodyWireBytes = 1; }, row => { row.requestDelivered = false; }, row => { row.result.requestForwardedComplete = false; },
    row => { row.response.headers.set('content-length', ['1']); }, row => { row.response.headers.set('transfer-encoding', ['chunked']); },
    row => { row.response.headers.delete('x-request-id'); }, row => { row.response.headers.set('x-request-id', [REQUEST_ID, REQUEST_ID]); },
    row => { row.url = new URL('http://127.0.0.1:19196/emby/Audio/' + ITEM + '/universal'); },
    row => { row.intent.startedAt = '2026-09-13T00:00:01.505Z'; }, row => { row.result.completedAt = '2026-09-13T00:00:01.495Z'; },
  ]) { const exchange = emptyExchange(); mutate(exchange); assert.equal(cancelledEmptyMedia(exchange, validate(serverFixture())), null); }
  assert.equal(cancelledEmptyMedia(emptyExchange(), null), null);
  for (const mutate of [event => { event.outcome = 'aborted'; }, event => { event.bytes = 1; }, event => { event.status = 206; },
    event => { event.method = 'HEAD'; }, event => { event.route = 'GET /emby/Users/{Id}'; }, event => { event.request_id = 'd'.repeat(32); }]) {
    const fixture = serverFixture(); mutate(fixture.event); replaceAppend(fixture, line(fixture.event));
    assert.equal(cancelledEmptyMedia(emptyExchange(), validate(fixture)), null);
  }
});

test('cancelled event matching preserves submillisecond precision at the exact one-millisecond tolerance', () => {
  const log = validate(serverFixture());
  for (const [field, timestamp, accepted] of [
    ['start', '2026-09-13T00:00:01.501000Z', true], ['start', '2026-09-13T00:00:01.501001Z', false],
    ['end', '2026-09-13T00:00:01.499000Z', true], ['end', '2026-09-13T00:00:01.498999Z', false],
  ]) {
    const exchange = emptyExchange();
    if (field === 'start') exchange.intent.startedAt = timestamp; else exchange.result.completedAt = timestamp;
    assert.equal(cancelledEmptyMedia(exchange, log) !== null, accepted);
  }
});

async function readPinned(description, parse) {
  const file = await fs.open(description.path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const info = await file.stat(); assert.equal(info.isFile(), true); assert.equal(info.uid, 0); assert.equal(info.nlink, 1);
    const bytes = await file.readFile(); assert.equal(hash(bytes), description.sha256);
    return parse(bytes);
  } finally { await file.close(); }
}

let baselinePromise;
function savedBaseline() {
  return baselinePromise ??= Promise.all([readPinned(OCCUPIED_BASELINE, strictJSON), readPinned(OCCUPIED_SNAPSHOT, sourceSnapshotJSON), readPinned(SEED, strictJSON)])
    .then(([closeout, snapshot, seed]) => ({ closeout, snapshot, seed }));
}

test('the pinned movie05 baseline retains five plays, thirteen revoked credentials, and count-two history', async () => {
  const { closeout, snapshot, seed } = await savedBaseline(), original = JSON.stringify(snapshot);
  const play = validateOccupiedMovieBaseline(closeout, snapshot, seed);
  assert.equal(play.id, OCCUPIED_PLAY); assert.equal(play.auth_session_id, OCCUPIED_AUTH);
  assert.equal(play.state, 'Prepared'); assert.equal(play.counted, false); assert.equal(play.position_ticks, 1217878390);
  assert.equal(snapshot.tables.play_sessions.length, 5); assert.equal(snapshot.tables.sessions.length, 13);
  assert.equal(snapshot.tables.play_sessions.filter(row => row.counted && row.state === 'Stopped').length, 2);
  assert.equal(snapshot.tables.play_sessions.filter(row => row.state === 'Expired').length, 2);
  assert.equal(snapshot.tables.user_item_data[0].play_count, 2); assert.equal(JSON.stringify(snapshot), original);
});

test('occupied baseline validation rejects changed ownership, counted history, or Prepared position', async () => {
  const saved = await savedBaseline();
  for (const mutate of [
    row => { row.snapshot.tables.sessions[0].revoked_at = null; },
    row => { row.snapshot.tables.play_sessions.pop(); },
    row => { row.snapshot.tables.play_sessions.find(play => play.id === OCCUPIED_PLAY).position_ticks = 0; },
    row => { row.snapshot.tables.play_sessions.find(play => play.counted).position_ticks++; },
    row => { row.snapshot.tables.user_item_data[0].play_count = 0; },
    row => { row.seed.actors.movie.id = 'e'.repeat(32); },
    row => { row.closeout.inputEvidence.after = pin('another-snapshot.json'); },
  ]) { const current = clone(saved); mutate(current); assert.throws(() => validateOccupiedMovieBaseline(current.closeout, current.snapshot, current.seed)); }
});

test('a new before snapshot protects every retained row and sequence, allowing only the capture time to advance', async () => {
  const { closeout, snapshot, seed } = await savedBaseline();
  const manifest = { scenario: 'movie', actor: { id: seed.actors.movie.id, username: seed.actors.movie.username } };
  const retained = { closeout, snapshot }, before = clone(snapshot);
  before.capturedAt = new Date(Date.parse(snapshot.capturedAt) + 1000).toISOString();
  assert.equal(verifyRetainedMovieBefore(before, manifest, seed, retained).id, OCCUPIED_PLAY);
  for (const mutate of [
    row => { row.tables.play_sessions.find(play => play.state === 'Expired').position_ticks++; },
    row => { row.tables.sessions[0].last_seen_at = row.capturedAt; },
    row => { row.tables.user_item_data[0].play_count = 0; },
    row => { row.tables.users[0].name += '-changed'; },
    row => { row.sequences.devices_id_seq.lastValue = String(BigInt(row.sequences.devices_id_seq.lastValue) + 1n); },
  ]) { const changed = clone(before); mutate(changed); assert.throws(() => verifyRetainedMovieBefore(changed, manifest, seed, retained), /retained_movie_fresh_state_changed/); }
});

test('the pruning guard checks the earliest old play deadline rather than only the current Prepared row', async () => {
  const { closeout, snapshot, seed } = await savedBaseline(), before = clone(snapshot);
  const manifest = { scenario: 'movie', actor: { id: seed.actors.movie.id, username: seed.actors.movie.username } };
  const firstExpiry = Math.min(...snapshot.tables.play_sessions.map(row => Date.parse(row.expires_at)));
  before.capturedAt = new Date(firstExpiry + 7 * 86400000 - 1199000).toISOString();
  assert.throws(() => verifyRetainedMovieBefore(before, manifest, seed, { closeout, snapshot }), /retained_movie_pruning_deadline/);
});

function occupiedDurableOverlay({ closeout, snapshot, seed }) {
  const synthetic = retainedFixture(closer), offset = Date.parse(snapshot.capturedAt) - Date.parse(synthetic.before.capturedAt);
  const translate = value => {
    if (Buffer.isBuffer(value)) return Buffer.from(JSON.stringify(translate(strictJSON(value))));
    if (Array.isArray(value)) return value.map(translate);
    if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, translate(item)]));
    if (typeof value !== 'string') return value;
    if (/^\d{4}-\d\d-\d\dT/.test(value)) return new Date(Date.parse(value) + offset).toISOString();
    return value.split(synthetic.manifest.actor.id).join(seed.actors.movie.id)
      .split(synthetic.seed.controlQ.id).join(seed.controlQ.id).split(synthetic.seed.catalog.movie.id).join(seed.catalog.movie.id);
  };
  const mapped = translate(synthetic), after = clone(snapshot);
  const additions = table => mapped.after.tables[table].filter(row => !mapped.before.tables[table].some(old => old.id === row.id));
  const firstNext = name => {
    const old = snapshot.sequences[name], next = Number(BigInt(old.lastValue) + (old.isCalled ? 1n : 0n));
    assert.equal(Number.isSafeInteger(next), true); return next;
  };
  const deviceNext = firstNext('devices_id_seq'), activityNext = firstNext('activity_entries_id_seq');
  const sessions = additions('sessions'), plays = additions('play_sessions');
  assert.equal(sessions.length, 2); assert.equal(plays.length, 2);
  for (const row of sessions) {
    assert.equal(after.tables.sessions.some(old => old.id === row.id), false);
    row.device_registry_id += deviceNext - 1;
  }
  for (const row of plays) {
    assert.equal(after.tables.play_sessions.some(old => old.id === row.id), false);
    row.duration_ticks = seed.catalog.movie.runtimeTicks;
  }
  for (const chain of mapped.physical.chains) chain.play.duration_ticks = seed.catalog.movie.runtimeTicks;
  for (const login of mapped.physical.logins) for (const chain of login.chains) chain.play.duration_ticks = seed.catalog.movie.runtimeTicks;
  after.tables.sessions.push(...sessions);
  after.tables.devices.push(...additions('devices').map(row => ({ ...row, id: row.id + deviceNext - 1 })));
  after.tables.activity_entries.push(...additions('activity_entries').map(row => ({ ...row, id: row.id + activityNext - 1 })));
  const expiration = mapped.after.tables.play_sessions.find(row => row.id === closer.RETAINED_PLAY);
  const prepared = after.tables.play_sessions.find(row => row.id === OCCUPIED_PLAY);
  for (const key of ['state', 'stopped_at', 'updated_at']) prepared[key] = expiration[key];
  after.tables.play_sessions.push(...plays);
  const data = after.tables.user_item_data.find(row => row.user_id === seed.actors.movie.id && row.item_id === seed.catalog.movie.id);
  const oldData = snapshot.tables.user_item_data.find(row => row.user_id === data.user_id && row.item_id === data.item_id);
  const firstPlays = mapped.physical.logins[0].chains.map(chain => chain.play), last = plays.at(-1);
  Object.assign(data, { play_count: oldData.play_count + plays.filter(row => row.counted).length,
    playback_position_ticks: closer.stopPosition(last.position_ticks, last.duration_ticks).position,
    played: oldData.played || plays.some(row => closer.stopPosition(row.position_ticks, row.duration_ticks).completed),
    last_played_at: last.started_at, updated_at: last.stopped_at });
  const readback = mapped.physical.exchanges.find(row => row.original?.method === 'GET'), body = strictJSON(readback.responseEntity);
  body.UserData.PlayCount = oldData.play_count + firstPlays.filter(row => row.counted).length;
  body.UserData.PlaybackPositionTicks = closer.stopPosition(firstPlays.at(-1).position_ticks, firstPlays.at(-1).duration_ticks).position;
  readback.responseEntity = Buffer.from(JSON.stringify(body));
  after.capturedAt = mapped.after.capturedAt;
  after.sequences.devices_id_seq = { lastValue: String(deviceNext + 1), isCalled: true };
  after.sequences.activity_entries_id_seq = { lastValue: String(activityNext + 3), isCalled: true };
  return { before: snapshot, after, seed, physical: mapped.physical,
    manifest: { ...mapped.manifest, actor: seed.actors.movie, catalog: seed.catalog }, retained: { closeout, snapshot } };
}

test('the saved occupied baseline supports two simulated new plays while preserving all old terminal history', async () => {
  const saved = await savedBaseline(), unchanged = JSON.stringify(saved), value = occupiedDurableOverlay(saved);
  const verify = current => closer.verifyDurable(current.before, current.after, current.manifest, current.seed, current.physical, current.retained);
  const proof = verify(value), oldTerminal = saved.snapshot.tables.play_sessions.filter(row => row.id !== OCCUPIED_PLAY);
  assert.equal(proof.countedPlays, 2); assert.equal(proof.sessionsRevoked, 2); assert.equal(proof.selectedUserdata.play_count, 4);
  assert.equal(proof.retainedBaselineExpiration.playSessionId, OCCUPIED_PLAY);
  assert.equal(proof.retainedBaselineExpiration.creationUpperBound, 7);
  assert.equal(value.after.tables.play_sessions.length, 7); assert.equal(value.after.tables.sessions.length, 15);
  assert.equal(value.after.tables.play_sessions.filter(row => row.counted).length, 4);
  assert.equal(strictJSON(value.physical.exchanges.find(row => row.original?.method === 'GET').responseEntity).UserData.PlayCount, 3);
  assert.equal(oldTerminal.length, 4);
  for (const row of oldTerminal) assert.deepEqual(value.after.tables.play_sessions.find(current => current.id === row.id), row);
  assert.deepEqual(value.after.tables.items, saved.snapshot.tables.items);
  const oldPrepared = saved.snapshot.tables.play_sessions.find(row => row.id === OCCUPIED_PLAY);
  const newPrepared = value.after.tables.play_sessions.find(row => row.id === OCCUPIED_PLAY);
  for (const [key, field] of Object.entries(oldPrepared)) if (!['state', 'stopped_at', 'updated_at'].includes(key)) assert.deepEqual(newPrepared[key], field);
  for (const [mutate, expected] of [
    [current => { current.after.tables.play_sessions.find(row => row.id === oldTerminal.find(play => play.counted).id).position_ticks++; }, /preexisting_source_row_changed/],
    [current => { current.after.tables.play_sessions.find(row => row.state === 'Expired' && row.id !== OCCUPIED_PLAY).position_ticks++; }, /preexisting_source_row_changed/],
    [current => { const row = oldTerminal.find(play => play.state === 'Stopped'); current.after.tables.play_sessions = current.after.tables.play_sessions.filter(play => play.id !== row.id); }, /preexisting_source_row_changed/],
    [current => { const row = oldTerminal.find(play => play.state === 'Expired'); current.after.tables.play_sessions = current.after.tables.play_sessions.filter(play => play.id !== row.id); }, /preexisting_source_row_changed/],
    [current => { current.after.tables.user_item_data.find(row => row.user_id === saved.seed.actors.movie.id).play_count = 2; }, /userdata_count_or_scope_mismatch/],
    [current => { current.after.tables.user_item_data.find(row => row.user_id === saved.seed.actors.movie.id).play_count = 6; }, /userdata_count_or_scope_mismatch/],
    [current => { const row = current.physical.exchanges.find(exchange => exchange.original?.method === 'GET'), body = strictJSON(row.responseEntity);
      body.UserData.PlayCount = 1; row.responseEntity = Buffer.from(JSON.stringify(body)); }, /movie_relogin_durable_readback_missing/],
    [current => { current.physical.chains.push({ ...current.physical.chains[0], play: oldTerminal.find(row => row.counted) }); }, /retained_movie_old_auth_or_play_reused/],
  ]) { const changed = occupiedDurableOverlay(saved); mutate(changed); assert.throws(() => verify(changed), expected); }
  assert.equal(JSON.stringify(saved), unchanged);
});
