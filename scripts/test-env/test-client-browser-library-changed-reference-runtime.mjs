/** Pure guards for the fixed reference browser runtime; no browser or transport is started. */
import test from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { gzipSync } from 'node:zlib';
import { EventEmitter } from 'node:events';
import { REFERENCE_ORIGIN as ORIGIN, REFERENCE_VIEWER as VIEWER, requireReferenceRuntimeEnvironment,
  classifyReferenceRequest, sanitizeDiagnostic, referenceAuthority, referenceClientMetadata, referenceRequestRecord,
  referenceResponseProjection, referenceHTTPPlan, referenceHTTPResponsePlan, referenceWebSocketPlan,
  referenceWebSocketResponsePlan, referenceConnectInner, createReferenceFrameState, decodeWebSocketFrames,
  referenceSocketMessageRecord, createReferenceLogoutProofContext, referenceTrafficAllowed,
  referenceHTTPBudgetAvailable, referenceBootstrapExternalRequest, REFERENCE_SERVER,
  referenceObservedRequestAuthority, referenceObservationFailure, dispatchReferenceHTTP,
  referencePhysicalHTTPFacts } from './client-browser-library-changed-reference-runtime.mjs';

const hash = value => createHash('sha256').update(value).digest('hex');
const token = 'private-token-for-pure-reference-tests';
const metadata = 'MediaBrowser Client="Emby Web", Device="Chrome", DeviceId="fresh-device", Version="4.9"';
const socketKey = 'dGhlIHNhbXBsZSBub25jZQ==';
const wsHeaders = ['Host', '127.0.0.1:18197', 'Origin', ORIGIN, 'Connection', 'Upgrade', 'Upgrade', 'websocket',
  'Sec-WebSocket-Version', '13', 'Sec-WebSocket-Key', socketKey];

function frame(payload, { masked = false, final = true, opcode = 1 } = {}) {
  const bytes = Buffer.isBuffer(payload) ? payload : Buffer.from(payload);
  const extended = bytes.length < 126 ? 0 : bytes.length <= 65535 ? 2 : 8;
  const header = Buffer.alloc(2 + extended + (masked ? 4 : 0));
  header[0] = (final ? 128 : 0) | opcode;
  header[1] = (masked ? 128 : 0) | (extended === 0 ? bytes.length : extended === 2 ? 126 : 127);
  if (extended === 2) header.writeUInt16BE(bytes.length, 2);
  if (extended === 8) header.writeBigUInt64BE(BigInt(bytes.length), 2);
  const encoded = Buffer.from(bytes);
  if (masked) {
    const mask = Buffer.from([11, 22, 33, 44]); mask.copy(header, 2 + extended);
    for (let index = 0; index < encoded.length; index += 1) encoded[index] ^= mask[index % 4];
  }
  return Buffer.concat([header, encoded]);
}

function formDispatchFixture(body = undefined, contentType = 'application/x-www-form-urlencoded; charset=UTF-8') {
  const account = { username: 'v'.repeat(29), password: 'a'.repeat(64) };
  const bytes = Buffer.isBuffer(body) ? body : Buffer.from(body ?? `Username=${account.username}&Pw=${account.password}`);
  const rawHeaders = ['Host', '127.0.0.1:18197', 'Content-Type', contentType, 'Content-Length', String(bytes.length), 'Authorization', metadata];
  const plan = referenceHTTPPlan(`${ORIGIN}/emby/Users/authenticatebyname`, 'POST', rawHeaders);
  const evidence = referencePhysicalHTTPFacts(); evidence.transport_phase = 'request_body';
  return { plan, bytes, account, evidence, pin: async () => {}, canDispatch: () => true, onLoginMetadata: () => {},
    onCreated: () => {}, onResponse: () => {}, onError: () => {}, onIdle: () => {}, onUpgrade: () => {} };
}

function recordingHTTPTransport(mode = 'normal') {
  const calls = []; let respond = null;
  return { calls, response(value) { respond(value); }, transport: { request(options, onResponse) {
    calls.push({ kind: 'request', options });
    if (mode === 'create_error') throw new Error('Private constructor detail');
    respond = onResponse;
    const outgoing = new EventEmitter();
    outgoing.setTimeout = () => outgoing;
    outgoing.end = bytes => {
      calls.push({ kind: 'end', bytes });
      if (mode === 'end_error') throw new Error('Private end detail');
      return outgoing;
    };
    return outgoing;
  } } };
}

test('the runtime can only start on a remote Linux root environment', () => {
  assert.throws(() => requireReferenceRuntimeEnvironment({ platform: 'win32', getuid: () => 0, env: {} }));
  assert.throws(() => requireReferenceRuntimeEnvironment({ platform: 'linux', getuid: () => 1000, env: {} }));
  assert.throws(() => requireReferenceRuntimeEnvironment({ platform: 'linux', getuid: () => 0, env: { DEBUG: '1' } }));
  assert.doesNotThrow(() => requireReferenceRuntimeEnvironment({ platform: 'linux', getuid: () => 0, env: {} }));
});

test('the selected origin and ordinary principal are fixed', () => {
  assert.equal(classifyReferenceRequest(`${ORIGIN}/emby/Users/${VIEWER}/Items?ParentId=93`, 'GET').allow, true);
  assert.equal(classifyReferenceRequest('http://127.0.0.1:18196/emby/Items', 'GET').allow, false);
  assert.equal(classifyReferenceRequest('https://127.0.0.1:18197/emby/Items', 'GET').allow, false);
  assert.equal(classifyReferenceRequest(`${ORIGIN}/emby/Users/other/Items`, 'GET').kind, 'foreign_user');
  assert.equal(classifyReferenceRequest(`${ORIGIN}/emby/Items?UserId=other`, 'GET').allow, false);
  assert.equal(classifyReferenceRequest(`${ORIGIN}/emby/Items?UserId=${VIEWER}&UserId=other`, 'GET').allow, false);
  assert.equal(classifyReferenceRequest(`${ORIGIN}/emby/Items#fragment`, 'GET').allow, false);
});

test('the production request observer accepts only the observed public branding CSS GET before login', () => {
  for (const path of ['/Branding/Css.css', '/emby/Branding/Css.css']) {
    const raw = ORIGIN + path, evidence = referenceRequestRecord(raw, 'GET');
    assert.equal(referenceObservedRequestAuthority(raw, 'GET', {}, null, evidence), null);
    assert.equal(evidence.capture, false);
    assert.equal(referenceHTTPPlan(raw, 'GET', ['Host', '127.0.0.1:18197']).kind, 'read');
    assert.throws(() => referenceObservedRequestAuthority(raw, 'GET', { 'X-Emby-Token': token }, null, evidence), /foreign_authority/);
    for (const method of ['HEAD', 'OPTIONS', 'POST'])
      assert.throws(() => referenceObservedRequestAuthority(raw, method, {}, null, referenceRequestRecord(raw, method)), /missing_authority/);
    assert.equal(classifyReferenceRequest(raw, 'POST').allow, false);
  }
  for (const path of ['/Branding/Css.css/', '/Branding/Other.css', '/Branding/Css.js', '/Items/100']) {
    const raw = ORIGIN + path;
    assert.throws(() => referenceObservedRequestAuthority(raw, 'GET', {}, null, referenceRequestRecord(raw, 'GET')), /missing_authority/);
  }
  for (const path of ['/Branding/Css', '/Branding/Configuration']) {
    const raw = ORIGIN + path;
    assert.equal(referenceObservedRequestAuthority(raw, 'GET', {}, null, referenceRequestRecord(raw, 'GET')), null);
  }
});

test('async observation failures keep the safe request association and fixed reason only', async () => {
  const raw = `${ORIGIN}/Items/100`, evidence = referenceRequestRecord(raw, 'GET');
  const operation = Promise.resolve().then(() => referenceObservedRequestAuthority(raw, 'GET', {}, null, evidence));
  for (const channel of ['context_request', 'context_failed']) {
    const failure = await operation.catch(error => referenceObservationFailure(error, { request_id: 'frame-156', channel }));
    assert.deepEqual(failure, { request_id: 'frame-156', channel, observation_reason: 'missing_authority' });
  }
  const failure = referenceObservationFailure(new Error(`Private ${token} at ${ORIGIN}/Items?api_key=${token}`),
    { request_id: 'service_worker-7', channel: 'context_response' });
  assert.deepEqual(failure, { request_id: 'service_worker-7', channel: 'context_response', observation_reason: 'async_operation_failed' });
  assert.equal(JSON.stringify(failure).includes(token), false); assert.equal(JSON.stringify(failure).includes(ORIGIN), false);
  assert.equal(Object.hasOwn(failure, 'stack'), false);
});

test('all playback preparation and other mutations are denied', () => {
  for (const method of ['GET', 'POST']) for (const route of ['/emby/Items/100/PlaybackInfo', '/emby/Items/100/%50laybackInfo'])
    assert.equal(classifyReferenceRequest(ORIGIN + route, method).kind, 'playback');
  for (const route of ['/emby/Videos/100/stream', '/emby/Audio/100/stream', '/emby/Sessions/Playing', '/emby/Sessions/Playing/Progress'])
    assert.equal(classifyReferenceRequest(ORIGIN + route, 'GET').allow, false);
  assert.equal(classifyReferenceRequest(`${ORIGIN}/emby/Items/100`, 'POST').allow, false);
  assert.equal(classifyReferenceRequest(`${ORIGIN}/emby/Items/100`, 'DELETE').allow, false);
  assert.equal(classifyReferenceRequest(`${ORIGIN}/emby/Items/100`, 'GET', 'media').allow, false);
  for (const route of ['/emby/Users/AuthenticateByName', '/emby/Sessions/Capabilities/Full', '/emby/Sessions/Logout'])
    assert.equal(classifyReferenceRequest(ORIGIN + route, 'POST').allow, true);
});

test('a poisoned actor may only enter verified read-and-logout cleanup', () => {
  const state = { poisoned: true, cleanup: false, identityLost: false, closed: false };
  for (const kind of ['read', 'logout', 'login', 'capabilities', 'websocket']) assert.equal(referenceTrafficAllowed(state, kind), false);
  state.cleanup = true;
  assert.equal(referenceTrafficAllowed(state, 'read'), true); assert.equal(referenceTrafficAllowed(state, 'logout'), true);
  for (const kind of ['login', 'capabilities', 'websocket', 'playback', 'state_mutation'])
    assert.equal(referenceTrafficAllowed(state, kind), false);
  state.identityLost = true;
  assert.equal(referenceTrafficAllowed(state, 'read'), false); assert.equal(referenceTrafficAllowed(state, 'logout'), false);
  state.identityLost = false; state.closed = true; assert.equal(referenceTrafficAllowed(state, 'logout'), false);
});

test('cleanup has a small independent reserve when the normal read budget is exhausted', () => {
  const exhausted = { seen: 2001, active: 16, request_bytes: 4194305, response_bytes: 134217729, logout: 0 };
  const cleanup = { seen: 1, active: 0, request_bytes: 0, response_bytes: 0 };
  assert.equal(referenceHTTPBudgetAvailable(exhausted, false, cleanup), false);
  assert.equal(referenceHTTPBudgetAvailable(exhausted, true, cleanup), true);
  assert.equal(referenceHTTPBudgetAvailable(exhausted, true, { ...cleanup, seen: 65 }), false);
  assert.equal(referenceHTTPBudgetAvailable(exhausted, true, { ...cleanup, active: 8 }), false);
  assert.equal(referenceHTTPBudgetAvailable(exhausted, true, { ...cleanup, response_bytes: 16777217 }), false);
  const exhaustedCleanup = { seen: 65, active: 8, request_bytes: 65537, response_bytes: 16777217 };
  assert.equal(referenceHTTPBudgetAvailable(exhausted, true, exhaustedCleanup, 'logout'), true);
  assert.equal(referenceHTTPBudgetAvailable({ ...exhausted, logout: 1 }, true, exhaustedCleanup, 'logout'), false);
});

test('the known bootstrap registration attempt is bound to actual login metadata and stays external', () => {
  const currentClient = { device_id: 'fresh-device', device_name: 'Chrome', client_name: 'Emby Web', client_version: '4.9' };
  const url = new URL('https://mb3admin.com/admin/service/registration/validateDevice');
  for (const [key, value] of Object.entries({ serverId: REFERENCE_SERVER, deviceId: currentClient.device_id,
    deviceName: currentClient.device_name, appName: currentClient.client_name, appVersion: currentClient.client_version,
    viewOnly: 'observed-public-value' })) url.searchParams.append(key, value);
  const scope = { phase: 'manual_login', loginIntent: true, loginCount: 1, bootstrapOpen: true, sourceworker: false,
    cleanup: false, poisoned: false, identityLost: false, seen: 0 };
  const evidence = referenceBootstrapExternalRequest(url.href, 'POST', 'fetch', currentClient, scope, [token]);
  assert.equal(evidence.view_only, 'observed-public-value');
  assert.deepEqual(evidence.known_binding, { server_id: REFERENCE_SERVER, ...currentClient });
  assert.equal(evidence.request_sha256, hash('POST\n' + url.href));
  assert.equal(classifyReferenceRequest(url.href, 'POST', 'fetch').allow, false);
  assert.equal(classifyReferenceRequest(url.href, 'POST', 'fetch').kind, 'external');
  assert.equal(classifyReferenceRequest(`${ORIGIN}/emby/DisplayPreferences/test`, 'POST').allow, false);
  assert.doesNotThrow(() => referenceBootstrapExternalRequest(url.href, 'POST', 'fetch', currentClient,
    { ...scope, phase: 'authenticated', seen: 2 }));
});

test('the bootstrap rejection exception cannot expand beyond its exact initial scope', () => {
  const currentClient = { device_id: 'fresh-device', device_name: 'Chrome', client_name: 'Emby Web', client_version: '4.9' };
  const url = new URL('https://mb3admin.com/admin/service/registration/validateDevice');
  for (const [key, value] of Object.entries({ serverId: REFERENCE_SERVER, deviceId: currentClient.device_id,
    deviceName: currentClient.device_name, appName: currentClient.client_name, appVersion: currentClient.client_version,
    viewOnly: '' })) url.searchParams.append(key, value);
  const scope = { phase: 'manual_login', loginIntent: true, loginCount: 1, bootstrapOpen: true, sourceworker: false,
    cleanup: false, poisoned: false, identityLost: false, seen: 0 };
  for (const change of [{ bootstrapOpen: false }, { loginIntent: false }, { sourceworker: true }, { seen: 3 },
    { cleanup: true }, { poisoned: true }, { identityLost: true }, ...['discovery', 'quiet', 'forward', 'restored', 'ui_logout'].map(phase => ({ phase }))])
    assert.throws(() => referenceBootstrapExternalRequest(url.href, 'POST', 'fetch', currentClient, { ...scope, ...change }));
  assert.throws(() => referenceBootstrapExternalRequest(url.href, 'POST', 'fetch', null, scope));
  assert.throws(() => referenceBootstrapExternalRequest(url.href, 'GET', 'fetch', currentClient, scope));
  assert.throws(() => referenceBootstrapExternalRequest(url.href, 'POST', 'xhr', currentClient, scope));
  const changed = new URL(url); changed.searchParams.set('deviceId', 'unrelated-device');
  assert.throws(() => referenceBootstrapExternalRequest(changed.href, 'POST', 'fetch', currentClient, scope));
  changed.searchParams.set('deviceId', currentClient.device_id); changed.searchParams.set('api_key', token);
  assert.throws(() => referenceBootstrapExternalRequest(changed.href, 'POST', 'fetch', currentClient, scope));
  changed.searchParams.delete('api_key'); changed.searchParams.set('viewOnly', token);
  assert.throws(() => referenceBootstrapExternalRequest(changed.href, 'POST', 'fetch', currentClient, scope, [token]));
});

test('request framing refuses ambiguity and preserves end-to-end header values', () => {
  const raw = `${ORIGIN}/emby/Items?ParentId=93`;
  const plan = referenceHTTPPlan(raw, 'GET', ['Host', '127.0.0.1:18197', 'Accept-Encoding', 'gzip', 'X-Test', 'exact-value']);
  assert.equal(plan.path, '/emby/Items?ParentId=93');
  assert.deepEqual(plan.headers, ['Host', '127.0.0.1:18197', 'Accept-Encoding', 'gzip', 'X-Test', 'exact-value', 'Connection', 'close']);
  assert.throws(() => referenceHTTPPlan(raw, 'GET', ['Host', '127.0.0.1:18197', 'Host', '127.0.0.1:18197']));
  assert.throws(() => referenceHTTPPlan(raw, 'GET', ['Host', '127.0.0.1:18197', 'Content-Length', '1']));
  assert.throws(() => referenceHTTPPlan(raw, 'GET', ['Host', '127.0.0.1:18197', 'Transfer-Encoding', 'chunked']));
  assert.throws(() => referenceHTTPPlan(raw, 'GET', ['Host', '127.0.0.1:18197', 'Sec-Fetch-Dest', 'video']));
  assert.throws(() => referenceHTTPPlan(raw, 'GET', ['Host', '127.0.0.1:18197', 'Connection', 'x-secret', 'X-Secret', token]));
  const response = referenceHTTPResponsePlan(200, ['Content-Type', 'application/json', 'Content-Encoding', 'gzip', 'Content-Length', '123'], 'GET');
  assert.equal(response.status, 200); assert.equal(response.contentEncoding, 'gzip'); assert.equal(response.length, 123);
  assert.throws(() => referenceHTTPResponsePlan(200, ['Content-Length', '3', 'Transfer-Encoding', 'chunked'], 'GET'));
});

test('the production dispatcher forwards the exact 106-byte form and original content type', async () => {
  const fixture = formDispatchFixture(), original = Buffer.from(fixture.bytes), transport = recordingHTTPTransport();
  let clientMetadata, response;
  fixture.onLoginMetadata = value => { clientMetadata = value; };
  fixture.onResponse = value => { response = value; };
  assert.equal(fixture.bytes.length, 106);
  await dispatchReferenceHTTP(fixture, transport.transport);
  assert.deepEqual(transport.calls.map(value => value.kind), ['request', 'end']);
  assert.strictEqual(transport.calls[0].options.headers, fixture.plan.headers);
  assert.equal(transport.calls[0].options.headers[3], 'application/x-www-form-urlencoded; charset=UTF-8');
  assert.strictEqual(transport.calls[1].bytes, fixture.bytes); assert.deepEqual(fixture.bytes, original);
  assert.deepEqual(clientMetadata, { client_name: 'Emby Web', device_name: 'Chrome', device_id: 'fresh-device', client_version: '4.9' });
  assert.equal(fixture.evidence.request_body_sha256, hash(original));
  assert.equal(fixture.evidence.request_content_type, 'application/x-www-form-urlencoded; charset=UTF-8');
  assert.equal(fixture.evidence.upstream_create_attempted, true); assert.equal(fixture.evidence.upstream_created, true);
  assert.equal(fixture.evidence.upstream_end_attempted, true); assert.equal(fixture.evidence.upstream_end_returned, true);
  assert.equal(fixture.evidence.upstream_response_received, false);
  const received = { statusCode: 200 }; transport.response(received);
  assert.strictEqual(response, received); assert.equal(fixture.evidence.upstream_response_received, true);
  assert.equal(fixture.evidence.transport_phase, 'upstream_response');
});

test('the production dispatcher rejects malformed or unapproved login bodies before creating an upstream request', async () => {
  const base = formDispatchFixture(), form = base.bytes.toString();
  const cases = [
    { body: form, type: 'application/json', code: 'login_content_type_rejected' },
    { body: form, type: 'application/x-www-form-urlencoded', code: 'login_content_type_rejected' },
    { body: form, type: 'application/x-www-form-urlencoded; charset=latin1', code: 'login_content_type_rejected' },
    { body: '{"Username":"dummy","Pw":"dummy"}', code: 'login_form_encoding_rejected' },
    { body: `Username=${base.account.username}&Pw=%GG`, code: 'login_form_encoding_rejected' },
    { body: `Username=${base.account.username}&Pw=%FF`, code: 'login_form_encoding_rejected' },
    { body: `Username=${base.account.username}&Pw=%`, code: 'login_form_encoding_rejected' },
    { body: Buffer.from([255, 254]), code: 'login_form_encoding_rejected' },
    { body: form + '&Pw=duplicate', code: 'login_form_fields_rejected' },
    { body: form + '&%50w=duplicate', code: 'login_form_fields_rejected' },
    { body: form + '&extra=value', code: 'login_form_fields_rejected' },
    { body: `Username=${base.account.username}`, code: 'login_form_fields_rejected' },
    { body: `Username=different&Pw=${base.account.password}`, code: 'login_username_mismatch' },
    { body: `Username=${base.account.username}&Pw=different`, code: 'login_password_mismatch' },
  ];
  for (const value of cases) {
    const fixture = formDispatchFixture(value.body, value.type);
    fixture.pin = async () => assert.fail('Rejected forms must not reach the pin or transport stage');
    await assert.rejects(dispatchReferenceHTTP(fixture, { request() { assert.fail('Rejected forms must not call the upstream factory'); } }),
      error => error.code === value.code && error.message === 'reference_physical_http_failure');
    assert.equal(fixture.evidence.transport_phase, 'login_form');
    assert.equal(fixture.evidence.request_body_sha256, hash(fixture.bytes));
    for (const field of ['upstream_create_attempted', 'upstream_created', 'upstream_end_attempted', 'upstream_end_returned', 'upstream_response_received'])
      assert.equal(fixture.evidence[field], false);
  }
});

test('the production dispatcher distinguishes constructor and end failures without inventing server receipt', async () => {
  for (const [mode, code, created, endAttempted] of [
    ['create_error', 'upstream_create_failed', false, false], ['end_error', 'upstream_end_failed', true, true],
  ]) {
    const fixture = formDispatchFixture(), transport = recordingHTTPTransport(mode);
    await assert.rejects(dispatchReferenceHTTP(fixture, transport.transport), error =>
      error.code === code && error.message === 'reference_physical_http_failure' && !error.message.includes('Private'));
    assert.equal(fixture.evidence.upstream_create_attempted, true);
    assert.equal(fixture.evidence.upstream_created, created); assert.equal(fixture.evidence.upstream_end_attempted, endAttempted);
    assert.equal(fixture.evidence.upstream_end_returned, false); assert.equal(fixture.evidence.upstream_response_received, false);
    assert.equal(Object.hasOwn(fixture.evidence, 'server_received'), false);
  }
});

test('authority must agree across all carriers and only fingerprints are public', () => {
  const raw = `${ORIGIN}/emby/Items?ParentId=93&api_key=${token}`;
  const authority = referenceAuthority(raw, { 'X-Emby-Token': token, Authorization: `${metadata}, Token="${token}"` }, token);
  assert.equal(authority.token_sha256, hash(token));
  assert.throws(() => referenceAuthority(raw, { 'X-Emby-Token': 'different-token' }));
  assert.throws(() => referenceAuthority(raw, {}, 'different-token'));
  assert.throws(() => referenceAuthority(raw, { Authorization: 'Bearer opaque' }));
  const evidence = referenceRequestRecord(raw, 'GET', { 'X-Emby-Token': token }, [token]);
  assert.equal(evidence.token_sha256, hash(token));
  assert.deepEqual(evidence.query, { ParentId: '93' });
  assert.equal(JSON.stringify(evidence).includes(token), false);
  assert.equal(Object.hasOwn(evidence, 'url'), false);
});

test('public and hidden query changes are both bound by the request shape', () => {
  const first = referenceRequestRecord(`${ORIGIN}/emby/Items?ParentId=93&UserId=${VIEWER}&Opaque=one`, 'GET');
  const reordered = referenceRequestRecord(`${ORIGIN}/emby/Items?Opaque=one&UserId=${VIEWER}&ParentId=93`, 'GET');
  const changed = referenceRequestRecord(`${ORIGIN}/emby/Items?ParentId=93&UserId=${VIEWER}&Opaque=two`, 'GET');
  assert.equal(first.shape_sha256, reordered.shape_sha256);
  assert.notEqual(first.request_sha256, reordered.request_sha256);
  assert.notEqual(first.shape_sha256, changed.shape_sha256);
  assert.deepEqual(first.hidden_query, [{ key: 'Opaque', value_sha256: hash('one') }]);
  assert.equal(JSON.stringify(first).includes('"one"'), false);
  const unrelated = referenceRequestRecord(`${ORIGIN}/web/index.html?ParentId=private&Other=value`, 'GET');
  assert.deepEqual(unrelated.query, {}); assert.equal(unrelated.hidden_query.length, 2);
});

test('request metadata is captured from actual carriers and conflicts fail closed', () => {
  const actual = referenceClientMetadata(`${ORIGIN}/emby/Users/AuthenticateByName`, { Authorization: metadata });
  assert.deepEqual(actual, { client_name: 'Emby Web', device_name: 'Chrome', device_id: 'fresh-device', client_version: '4.9' });
  assert.throws(() => referenceClientMetadata(`${ORIGIN}/emby/Users/AuthenticateByName`, {
    Authorization: metadata, 'X-Emby-Device-Id': 'different-device' }));
  assert.throws(() => referenceClientMetadata(`${ORIGIN}/emby/Users/AuthenticateByName`, {}));
});

test('catalog projections retain every returned identity without exposing unrelated fields', () => {
  const original = { Items: [
    { Id: '96', Name: 'First movie', Type: 'Movie', IsFolder: false, ParentId: '93', Path: '/private/path' },
    { Id: '100', Name: 'Second movie', Type: 'Movie', IsFolder: false, ParentId: '99', ProviderIds: { secret: 'hidden' } },
  ], TotalRecordCount: 2, StartIndex: 0 };
  const bytes = Buffer.from(JSON.stringify(original));
  const compressed = gzipSync(bytes), before = Buffer.from(compressed);
  const evidence = referenceResponseProjection(compressed, [], 'gzip');
  assert.deepEqual(evidence.json.Items.map(item => item.Id), ['96', '100']);
  assert.equal(evidence.json.Items[1].ParentId, '99'); assert.equal(evidence.json.TotalRecordCount, 2);
  assert.equal(evidence.body_sha256, hash(compressed)); assert.equal(evidence.bytes, compressed.length);
  assert.equal(Object.hasOwn(evidence.json.Items[0], 'Path'), false);
  assert.deepEqual(compressed, before);
  assert.throws(() => referenceResponseProjection(Buffer.from(JSON.stringify({ Id: '100', Name: token })), [token]));
});

test('WebSocket handshakes are bound to the fixed origin and ordinary authority', () => {
  const raw = `ws://127.0.0.1:18197/emby?api_key=${token}&UserId=${VIEWER}`;
  const plan = referenceWebSocketPlan(raw, 'GET', wsHeaders);
  assert.equal(plan.path, `/emby?api_key=${token}&UserId=${VIEWER}`);
  const accept = createHash('sha1').update(socketKey + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').digest('base64');
  const reply = ['Connection', 'Upgrade', 'Upgrade', 'websocket', 'Sec-WebSocket-Accept', accept];
  assert.deepEqual(referenceWebSocketResponsePlan(101, reply, plan), reply);
  assert.throws(() => referenceWebSocketResponsePlan(101, [...reply, 'Sec-WebSocket-Accept', accept], plan));
  assert.throws(() => referenceWebSocketResponsePlan(101, [...reply, 'Sec-WebSocket-Extensions', 'permessage-deflate'], plan));
  assert.throws(() => referenceWebSocketPlan('ws://127.0.0.1:18197/emby?UserId=foreign', 'GET', wsHeaders));
  assert.throws(() => referenceWebSocketPlan('ws://127.0.0.1:18197/web/index.html', 'GET', wsHeaders));
});

test('CONNECT only parses one bounded original WebSocket handshake', () => {
  const header = `GET /emby?api_key=${token} HTTP/1.1\r\n` +
    wsHeaders.reduce((value, field, index) => index % 2 === 0 ? value + `${field}: ${wsHeaders[index + 1]}\r\n` : value, '') + '\r\n';
  assert.equal(referenceConnectInner(Buffer.from(header.slice(0, 15))), null);
  const trailing = frame('{"MessageType":"SessionsStop","Data":""}', { masked: true });
  const parsed = referenceConnectInner(Buffer.concat([Buffer.from(header), trailing]));
  assert.equal(parsed.raw, `${ORIGIN}/emby?api_key=${token}`); assert.deepEqual(parsed.head, trailing);
  assert.throws(() => referenceConnectInner(Buffer.from(header.replace('/emby?', '//elsewhere/emby?'))));
  assert.throws(() => referenceConnectInner(Buffer.from(header.replace('GET ', 'POST '))));
});

test('complete rejected CONNECT upgrades are observed before method, path, origin, or header admission', () => {
  const target = `/emby?api_key=${token}`;
  const header = `GET ${target} HTTP/1.1\r\n` +
    wsHeaders.reduce((value, field, index) => index % 2 === 0 ? value + `${field}: ${wsHeaders[index + 1]}\r\n` : value, '') + '\r\n';
  const variants = [
    { head: header.replace('/emby?', '/not-a-socket?'), method: 'GET', path: target.replace('/emby?', '/not-a-socket?'), route: '/not-a-socket' },
    { head: header.replace(`Origin: ${ORIGIN}`, 'Origin: https://unrelated.invalid'), method: 'GET', path: target, route: '/emby' },
    { head: header.replace('Host: 127.0.0.1:18197', 'Host: unrelated.invalid'), method: 'GET', path: target, route: '/emby' },
    { head: header.replace('Sec-WebSocket-Version: 13', 'malformed-header'), method: 'GET', path: target, route: '/emby' },
    { head: header.replace('GET ', 'POST '), method: 'POST', path: target, route: '/emby' },
  ];
  for (const variant of variants) {
    const observed = [];
    assert.throws(() => referenceConnectInner(Buffer.from(variant.head), entry => { observed.push(entry); }, [token]));
    assert.deepEqual(observed, [{ method: variant.method, route: variant.route,
      request_sha256: hash(variant.method + '\n' + ORIGIN + variant.path) }]);
    assert.equal(Object.isFrozen(observed[0]), true); assert.equal(JSON.stringify(observed).includes(token), false);
  }
  let incompleteObserved = false;
  assert.equal(referenceConnectInner(Buffer.from(header.slice(0, 20)), () => { incompleteObserved = true; }, [token]), null);
  assert.equal(incompleteObserved, false);
});

test('complete and fragmented server messages retain the original wire bytes', () => {
  const message = '{"MessageType":"LibraryChanged","MessageId":"unassumed-format","Data":{"FoldersAddedTo":["99"],"Other":{"nested":[1,null]}}}';
  const state = createReferenceFrameState('server'), observed = [];
  const one = frame(message.slice(0, 25), { final: false }), two = frame(message.slice(25), { opcode: 0 });
  assert.deepEqual(decodeWebSocketFrames(state, one, 1000, value => observed.push(value)), []);
  const forwarded = decodeWebSocketFrames(state, two, 1001, value => observed.push(value));
  assert.deepEqual(Buffer.concat(forwarded), Buffer.concat([one, two]));
  assert.equal(observed.length, 1); assert.equal(observed[0].bytes.toString(), message);
  const evidence = referenceSocketMessageRecord(observed[0]);
  assert.equal(evidence.json_text, message); assert.equal(evidence.body_sha256, hash(message));
  assert.deepEqual(evidence.Data, { FoldersAddedTo: ['99'], Other: { nested: [1, null] } });
  assert.equal(evidence.original_message, true);
  const bomText = '\uFEFF' + message;
  let bomMessage;
  decodeWebSocketFrames(createReferenceFrameState('server'), frame(bomText), 1002, value => { bomMessage = value; });
  const bomEvidence = referenceSocketMessageRecord(bomMessage);
  assert.equal(bomEvidence.text, bomText); assert.equal(bomEvidence.body_sha256, hash(bomText));
});

test('original LibraryChanged evidence fails closed instead of redacting its body', () => {
  const text = JSON.stringify({ MessageType: 'LibraryChanged', MessageId: 'not-a-guessed-id', Data: { FoldersAddedTo: [], value: token } });
  let observed;
  decodeWebSocketFrames(createReferenceFrameState('server'), frame(text), 1000, value => { observed = value; });
  assert.throws(() => referenceSocketMessageRecord(observed, [token]), /contains_secret/);
  const credential = '{"MessageType":"LibraryChanged","Data":{"AccessToken":"unknown-sensitive-value"}}';
  decodeWebSocketFrames(createReferenceFrameState('server'), frame(credential), 1000, value => { observed = value; });
  assert.throws(() => referenceSocketMessageRecord(observed), /credential_field/);
});

test('the raw server stream retains ordinary Sessions payloads and rejects credential disclosure', () => {
  const text = JSON.stringify({ MessageType: 'Sessions', Data: [{ Id: 'actual-session', Name: 'Ordinary user' }] });
  let observed;
  decodeWebSocketFrames(createReferenceFrameState('server'), frame(text), 1000, value => { observed = value; });
  const evidence = referenceSocketMessageRecord(observed, [token]);
  assert.equal(evidence.MessageType, 'Sessions'); assert.equal(evidence.body_sha256, hash(text));
  assert.equal(evidence.payload_retained, true); assert.equal(evidence.json_text, text);
  assert.deepEqual(evidence.Data, [{ Id: 'actual-session', Name: 'Ordinary user' }]);
  const unsafe = JSON.stringify({ MessageType: 'Sessions', Data: [{ AccessToken: token }] });
  decodeWebSocketFrames(createReferenceFrameState('server'), frame(unsafe), 1000, value => { observed = value; });
  assert.throws(() => referenceSocketMessageRecord(observed, [token]), /contains_secret/);
});

test('only the two proven client subscription messages may pass', () => {
  for (const json of [{ MessageType: 'SessionsStart', Data: '1000,1000' }, { MessageType: 'SessionsStop', Data: '' }]) {
    const wire = frame(JSON.stringify(json), { masked: true }), before = Buffer.from(wire);
    const forwarded = decodeWebSocketFrames(createReferenceFrameState('client'), wire, 1000);
    assert.deepEqual(forwarded, [before]); assert.deepEqual(wire, before);
  }
  for (const json of [{ MessageType: 'SessionsStart', Data: '0,1000' }, { MessageType: 'KeepAlive' },
    { MessageType: 'ReportPlaybackProgress', Data: {} }, { MessageType: 'Unknown', Data: '' }])
    assert.throws(() => decodeWebSocketFrames(createReferenceFrameState('client'), frame(JSON.stringify(json), { masked: true }), 1000));
  assert.throws(() => decodeWebSocketFrames(createReferenceFrameState('client'), frame(Buffer.from([1, 2]), { masked: true, opcode: 2 }), 1000));
});

test('playback commands, masking violations, compression, and invalid UTF-8 fail closed', () => {
  for (const MessageType of ['Play', 'Playstate', 'GeneralCommand']) for (const direction of ['client', 'server'])
    assert.throws(() => decodeWebSocketFrames(createReferenceFrameState(direction),
      frame(JSON.stringify({ MessageType }), { masked: direction === 'client' }), 1000), /control_denied/);
  assert.throws(() => decodeWebSocketFrames(createReferenceFrameState('server'), frame('{}', { masked: true }), 1000));
  const compressed = frame('{}'); compressed[0] |= 64;
  assert.throws(() => decodeWebSocketFrames(createReferenceFrameState('server'), compressed, 1000));
  assert.throws(() => decodeWebSocketFrames(createReferenceFrameState('server'), frame(Buffer.from([255])), 1000));
  const ping = frame('ping', { masked: true, opcode: 9 });
  assert.deepEqual(decodeWebSocketFrames(createReferenceFrameState('client'), ping, 1000), [ping]);
});

test('diagnostics remove raw and encoded known secrets', () => {
  for (const variant of [token, Buffer.from(token).toString('base64'), Buffer.from(token).toString('hex'),
    [...Buffer.from(token)].map(byte => `%${byte.toString(16).padStart(2, '0')}`).join('')]) {
    const safe = sanitizeDiagnostic(`Failure ${variant}`, [token]);
    assert.equal(safe.includes(token), false); assert.equal(safe.includes(variant), false);
  }
  assert.equal(sanitizeDiagnostic('a'.repeat(9000), [token]), '[diagnostic omitted]');
});

test('viewer proof receives only genuine frame logout events after exact HTTP 204', () => {
  const context = new EventEmitter(); context.pages = () => [];
  let errors = 0; const gate = createReferenceLogoutProofContext(context, () => { errors += 1; }), seen = [];
  for (const name of ['request', 'response', 'requestfinished', 'requestfailed']) gate.on(name, value => seen.push([name, value]));
  const request = { serviceWorker: () => null, url: () => `${ORIGIN}/emby/Sessions/Logout`, method: () => 'POST' };
  const rejectedRequest = { ...request }, rejected = { request: () => rejectedRequest, status: () => 401 };
  context.emit('request', rejectedRequest); context.emit('response', rejected); context.emit('requestfinished', rejectedRequest);
  assert.deepEqual(seen, []);
  const accepted = { request: () => request, status: () => 204 };
  context.emit('response', accepted); context.emit('requestfinished', request);
  assert.deepEqual(seen, [['request', request], ['response', accepted], ['requestfinished', request]]);
  const workerRequest = { ...request, serviceWorker: () => ({}) };
  context.emit('response', { request: () => workerRequest, status: () => 204 });
  assert.equal(seen.length, 3); assert.equal(errors, 0); gate.dispose();
});

test('post-logout proof waits for the original authority check and preserves a finished event', async () => {
  const context = new EventEmitter(); context.pages = () => [];
  let allow, errors = 0;
  const gate = createReferenceLogoutProofContext(context, () => { errors += 1; }, () => new Promise(resolve => { allow = resolve; }));
  const seen = [], request = { serviceWorker: () => null, url: () => `${ORIGIN}/emby/Sessions/Logout`, method: () => 'POST' };
  for (const name of ['request', 'response', 'requestfinished']) gate.on(name, value => seen.push([name, value]));
  const response = { request: () => request, status: () => 204 };
  context.emit('response', response); context.emit('requestfinished', request);
  assert.deepEqual(seen, []); await Promise.resolve(); allow(true);
  await new Promise(resolve => setImmediate(resolve));
  assert.deepEqual(seen, [['request', request], ['response', response], ['requestfinished', request]]);
  assert.equal(errors, 0); gate.dispose();
});
