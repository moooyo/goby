/** Pure NextUp discovery guards; no browser or transport is started. */
import test from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { gzipSync } from 'node:zlib';
import {
  NEXTUP_DISCOVERY_SCOPE,
  REFERENCE_ORIGIN as ORIGIN,
  REFERENCE_SERVER as SERVER,
  REFERENCE_VIEWER as VIEWER,
  requireReferenceRuntimeEnvironment,
  classifyReferenceRequest,
  referenceRequestRecord,
  referenceResponseProjection,
  referenceHTTPPlan,
  referenceFrameBodyEvidence,
  deliverReferencePrivateResponse,
} from './client-browser-nextup-discovery-runtime.mjs';

const hash = value => createHash('sha256').update(value).digest('hex');
const token = 'private-token-for-pure-nextup-tests';
const ownedItemIds = ['137', '138', '139', '140', '141', '142', '143', '144', '145', '146', '147', '148'];
const viewIds = ['133', '135'];

test('the frozen discovery scope binds slot P to the fixed principal and catalog', () => {
  assert.equal(ORIGIN, 'http://127.0.0.1:18197');
  assert.equal(VIEWER, 'b886fd2f2c6241db9b973c8f038f9105');
  assert.equal(SERVER, 'f56dec8ff7414847873064c4be9fba74');
  assert.equal(Object.isFrozen(NEXTUP_DISCOVERY_SCOPE), true);
  assert.deepEqual(NEXTUP_DISCOVERY_SCOPE, {
    format: 1, viewerId: VIEWER, serverId: SERVER, origin: ORIGIN, slot: 'P',
    ownedItemIds, viewIds, publicResponseRoute: '/Shows/NextUp', playbackAllowed: false,
    parentRuntimeSource: 'client-browser-library-changed-reference-runtime.mjs',
    parentRuntimeSha256: '209f72fa812647742003b50c311ee358ffc6f1279e29a2e142eec506245fed7c',
  });
});

test('the runtime only admits a Linux root environment without browser debugging', () => {
  for (const environment of [
    { platform: 'win32', getuid: () => 0, env: {} },
    { platform: 'linux', getuid: () => 1000, env: {} },
    { platform: 'linux', env: {} },
    { platform: 'linux', getuid: () => 0, env: { DEBUG: '1' } },
    { platform: 'linux', getuid: () => 0, env: { PWDEBUG: '1' } },
  ]) assert.throws(() => requireReferenceRuntimeEnvironment(environment), /requires_remote_linux_root/);
  assert.doesNotThrow(() => requireReferenceRuntimeEnvironment({ platform: 'linux', getuid: () => 0, env: {} }));
});

test('discovery reads reject a foreign actor or origin at request admission', () => {
  assert.equal(classifyReferenceRequest(`${ORIGIN}/emby/Shows/NextUp?UserId=${VIEWER}`, 'GET').allow, true);
  assert.equal(classifyReferenceRequest(`${ORIGIN}/emby/Users/${VIEWER}/Items?ParentId=133`, 'GET').allow, true);
  for (const route of [
    '/emby/Users/foreign/Items',
    '/emby/Shows/NextUp?UserId=foreign',
    `/emby/Shows/NextUp?UserId=${VIEWER}&UserId=foreign`,
    `/emby/Items?UserId=${VIEWER}&userid=foreign`,
    '/emby/Users/c5f36699a54f4971a891682cd9de410f/Views',
  ]) {
    const raw = ORIGIN + route;
    assert.equal(classifyReferenceRequest(raw, 'GET').allow, false);
    assert.equal(classifyReferenceRequest(raw, 'GET').kind, 'foreign_user');
    assert.throws(() => referenceHTTPPlan(raw, 'GET', ['Host', '127.0.0.1:18197']));
  }
  for (const raw of [
    'http://127.0.0.1:18196/Shows/NextUp',
    'https://127.0.0.1:18197/Shows/NextUp',
    `${ORIGIN}/Shows/NextUp#fragment`,
  ]) assert.equal(classifyReferenceRequest(raw, 'GET').allow, false);
});

test('playback preparation, media transport, and discovery mutations are denied', () => {
  for (const route of [
    '/emby/Items/137/PlaybackInfo', '/emby/Items/137/%50laybackInfo',
    '/emby/Videos/137/stream', '/emby/Audio/138/stream',
    '/emby/Sessions/Playing', '/emby/Sessions/Playing/Progress',
  ]) for (const method of ['GET', 'POST']) {
    const raw = ORIGIN + route;
    assert.equal(classifyReferenceRequest(raw, method).allow, false);
    assert.equal(classifyReferenceRequest(raw, method).kind, 'playback');
    assert.throws(() => referenceHTTPPlan(raw, method, ['Host', '127.0.0.1:18197']));
  }
  assert.equal(classifyReferenceRequest(`${ORIGIN}/emby/Items/137`, 'GET', 'media').allow, false);
  for (const method of ['POST', 'PUT', 'PATCH', 'DELETE'])
    assert.equal(classifyReferenceRequest(`${ORIGIN}/emby/Shows/NextUp`, method).allow, false);
});

test('NextUp query entries preserve public duplicates, empty values, and original order', () => {
  const entries = [
    ['UserId', VIEWER], ['Limit', '12'], ['Fields', 'PrimaryImageAspectRatio'], ['Fields', 'UserData'],
    ['Opaque', 'first'], ['Empty', ''], ['Opaque', 'second'], ['Opaque', ''],
    ['EnableResumable', 'false'], ['MixedCase', 'a+b c'],
  ];
  const url = new URL(`${ORIGIN}/emby/Shows/NextUp`);
  for (const [key, value] of entries) url.searchParams.append(key, value);
  const evidence = referenceRequestRecord(url.href, 'GET');
  assert.deepEqual(evidence.exact_public_query, entries);
  assert.equal(evidence.query.UserId, VIEWER);
  assert.equal(evidence.query.Limit, '12');
  assert.equal(evidence.request_sha256, hash('GET\n' + url.href));
  assert.equal(evidence.capture, true);
  assert.equal(Object.hasOwn(evidence, 'url'), false);
  const reordered = new URL(`${ORIGIN}/emby/Shows/NextUp`);
  for (const [key, value] of [...entries].reverse()) reordered.searchParams.append(key, value);
  const other = referenceRequestRecord(reordered.href, 'GET');
  assert.deepEqual(other.exact_public_query, [...entries].reverse());
  assert.notEqual(evidence.request_sha256, other.request_sha256);
  assert.equal(referenceHTTPPlan(url.href, 'GET', ['Host', '127.0.0.1:18197']).path,
    url.href.slice(ORIGIN.length));
});

test('credential query parameters are omitted and public values cannot disclose known secrets', () => {
  const url = new URL(`${ORIGIN}/Shows/NextUp?UserId=${VIEWER}&Opaque=public&Empty=`);
  const credentialFields = [
    'token', 'access_token', 'apiKey', 'api_key', 'X-Emby-Token', 'x-mediabrowser-token',
    'password', 'pw', 'passwordHash', 'authorization', 'X-Emby-Authorization', 'user-name',
  ];
  for (const key of credentialFields) url.searchParams.append(key, token);
  const evidence = referenceRequestRecord(url.href, 'GET', {}, [token]);
  assert.deepEqual(evidence.exact_public_query, [['UserId', VIEWER], ['Opaque', 'public'], ['Empty', '']]);
  assert.equal(evidence.token_sha256, hash(token));
  assert.equal(JSON.stringify(evidence).includes(token), false);
  const normalize = key => key.replace(/[-_]/g, '').toLowerCase();
  const forbidden = new Set(credentialFields.map(normalize));
  for (const key of Object.keys(evidence.query)) assert.equal(forbidden.has(normalize(key)), false);
  for (const entry of evidence.hidden_query) assert.equal(forbidden.has(normalize(entry.key)), false);
  assert.throws(() => referenceRequestRecord(`${ORIGIN}/Shows/NextUp?Opaque=${token}`, 'GET', {}, [token]), /secret|query_rejected/);
  assert.throws(() => referenceRequestRecord(`${ORIGIN}/Shows/NextUp?api_key=${token}&Opaque=${token}`, 'GET'), /query_rejected/);
  assert.throws(() => referenceRequestRecord(`${ORIGIN}/Shows/NextUp?Opaque=${token}`, 'GET', { 'x-emby-token': token }), /query_rejected/);
});

test('NextUp response capture requires a GET on the exact normalized route', () => {
  for (const route of ['/Shows/NextUp', '/Shows/NextUp/', '/shows/nextup', '/emby/Shows/NextUp'])
    assert.equal(referenceRequestRecord(ORIGIN + route, 'GET').capture, true);
  for (const method of ['HEAD', 'OPTIONS', 'POST'])
    assert.equal(referenceRequestRecord(`${ORIGIN}/Shows/NextUp`, method).capture, false);
  for (const route of ['/Shows/NextUpExtra', '/Shows/NextUp/137', '/Shows/Upcoming', '/Shows'])
    assert.equal(referenceRequestRecord(ORIGIN + route, 'GET').capture, false);
});

test('catalog capture is limited to the owned items and discovery views', () => {
  const captureIds = [...ownedItemIds, ...viewIds];
  for (const prefix of ['', `/Users/${VIEWER}`]) {
    assert.equal(referenceRequestRecord(`${ORIGIN}${prefix}/Views`, 'GET').capture, true);
    for (const id of captureIds) {
      assert.equal(referenceRequestRecord(`${ORIGIN}${prefix}/Items/${id}`, 'GET').capture, true);
      assert.equal(referenceRequestRecord(`${ORIGIN}${prefix}/Items?ParentId=${id}`, 'GET').capture, true);
      assert.equal(referenceRequestRecord(`${ORIGIN}${prefix}/Items?Ids=${id}`, 'GET').capture, true);
    }
    assert.equal(referenceRequestRecord(`${ORIGIN}${prefix}/Items?Ids=137,145,148,133,135`, 'GET').capture, true);
    for (const suffix of [
      '', '/93', '/96', '/100', '/149', '?ParentId=93', '?Ids=100', '?Ids=137,149', '?Ids=',
      '?Ids=137,', '?ParentId=133&ParentId=135', '?Ids=137&Ids=138',
    ]) assert.equal(referenceRequestRecord(`${ORIGIN}${prefix}/Items${suffix}`, 'GET').capture, false);
  }
  assert.equal(referenceRequestRecord(`${ORIGIN}/Users/foreign/Views`, 'GET').capture, false);
  assert.equal(referenceRequestRecord(`${ORIGIN}/Users/foreign/Items/137`, 'GET').capture, false);
});

test('public item projections preserve response order, paging, and the complete UserData tree', () => {
  const userData = {
    PlaybackPositionTicks: 0, PlayCount: 2, IsFavorite: false, Played: true,
    LastPlayedDate: '2026-09-01T10:20:30Z', Key: 'public-item-148',
    UnknownPublicField: { values: [0, false, '', null, { label: 'retained' }] },
  };
  const original = { Items: [
    { Id: '148', Name: 'Later catalog item', Type: 'Episode', IsFolder: false, ParentId: '135',
      UserData: userData, Path: '/private/path', ProviderIds: { private: 'omitted' } },
    { Id: '137', Name: 'Earlier catalog item', Type: 'Episode', IsFolder: false, ParentId: '133', UserData: null },
    { Id: '142', Name: 'Item without user state', Type: 'Episode', IsFolder: false, ParentId: '133' },
  ], TotalRecordCount: 11, StartIndex: 4, ServerPrivateField: 'omitted' };
  const bytes = gzipSync(Buffer.from(JSON.stringify(original))), before = Buffer.from(bytes);
  const evidence = referenceResponseProjection(bytes, [], 'gzip');
  assert.deepEqual(evidence.json, {
    Items: [
      { Id: '148', Name: 'Later catalog item', Type: 'Episode', IsFolder: false, ParentId: '135', UserData: userData },
      { Id: '137', Name: 'Earlier catalog item', Type: 'Episode', IsFolder: false, ParentId: '133', UserData: null },
      { Id: '142', Name: 'Item without user state', Type: 'Episode', IsFolder: false, ParentId: '133' },
    ], TotalRecordCount: 11, StartIndex: 4,
  });
  assert.equal(evidence.projection, 'public_item_identity_and_userdata_fields');
  assert.equal(evidence.body_sha256, hash(bytes));
  assert.equal(evidence.decoded_body_sha256, hash(Buffer.from(JSON.stringify(original))));
  assert.equal(evidence.bytes, bytes.length);
  assert.deepEqual(bytes, before);
  assert.deepEqual(referenceResponseProjection(Buffer.from(JSON.stringify(original.Items))).json, evidence.json.Items);
  assert.deepEqual(referenceResponseProjection(Buffer.from(JSON.stringify(original.Items[0]))).json, evidence.json.Items[0]);
});

test('frame body evidence is bounded and matches decoded entity bytes', () => {
  const bytes = Buffer.from('{"Items":[],"TotalRecordCount":0}');
  assert.deepEqual(referenceFrameBodyEvidence(bytes), { decoded_body_sha256: hash(bytes), decoded_body_bytes: bytes.length });
  assert.throws(() => referenceFrameBodyEvidence(Buffer.alloc(1048577)));
  assert.throws(() => referenceFrameBodyEvidence(bytes.toString()));
});

test('UserData rejects invalid shapes, nested credentials, and known secret values', () => {
  const project = (UserData, secrets = []) => referenceResponseProjection(
    Buffer.from(JSON.stringify({ Id: '137', UserData })), secrets);
  for (const value of [false, 0, 'state', []]) assert.throws(() => project(value));
  for (const key of ['token', 'access_token', 'apiKey', 'password', 'passwordHash', 'authorization', 'user-name'])
    assert.throws(() => project({ PublicNested: [{ [key]: 'private-value' }] }), /credential_field/);
  assert.throws(() => project({ PublicNested: { value: token } }, [token]), /contains_secret/);
  assert.throws(() => project({ PublicNested: { value: Buffer.from(token).toString('base64') } }, [token]), /contains_secret/);
  assert.deepEqual(project({ token: '', authorization: null }).json.UserData, { token: '', authorization: null });
  let nested = { value: 'public' };
  for (let depth = 0; depth < 26; depth += 1) nested = { child: nested };
  assert.throws(() => project(nested), /public_json_limit/);
  assert.throws(() => project({ values: Array.from({ length: 16385 }, () => null) }), /public_json_limit/);
  assert.throws(() => project({ value: 'x'.repeat(1048576) }), /projection_limit/);
});

test('raw login and NextUp responses reach only their explicit private synchronous sinks', () => {
  const calls = [], body = Buffer.from(JSON.stringify({ AccessToken: token, User: { Id: 'unverified-principal' } }));
  const login = { id: 'physical-1', kind: 'login', method: 'POST', route: '/Users/AuthenticateByName', status: 200 };
  const nextUp = { id: 'physical-2', kind: 'read', method: 'GET', route: '/Shows/NextUp', status: 200 };
  const observer = {
    onHTTPResponse() { throw new Error('Public response callbacks cannot receive private bytes'); },
    onPrivateLoginResponse(value) { calls.push(['login', value]); value.request.status = 201; },
    onPrivateResponse(value) { calls.push(['nextup', value]); },
  };
  deliverReferencePrivateResponse(observer, 'onPrivateLoginResponse', login, null, body);
  deliverReferencePrivateResponse(observer, 'onPrivateResponse', nextUp, 'gzip', body);
  assert.deepEqual(calls.map(([kind]) => kind), ['login', 'nextup']);
  assert.equal(calls[0][1].body_base64, body.toString('base64'));
  assert.equal(calls[0][1].encoding, 'identity');
  assert.equal(calls[1][1].encoding, 'gzip');
  assert.equal(login.status, 200);
  assert.equal(Object.hasOwn(login, 'token_sha256'), false);
  assert.equal(Object.hasOwn(login, 'body_base64'), false);
  assert.throws(() => deliverReferencePrivateResponse(observer, 'onHTTPResponse', login, null, body), /callback_rejected/);
  assert.throws(() => deliverReferencePrivateResponse(observer, 'onPrivateResponse', login, null, body), /scope_rejected/);
  assert.throws(() => deliverReferencePrivateResponse(observer, 'onPrivateLoginResponse', nextUp, null, body), /scope_rejected/);
  assert.throws(() => deliverReferencePrivateResponse({ onPrivateLoginResponse: () => 1 },
    'onPrivateLoginResponse', login, null, body), /must_be_synchronous/);
  assert.throws(() => deliverReferencePrivateResponse({ onPrivateLoginResponse: async () => {} },
    'onPrivateLoginResponse', login, null, body), /must_be_synchronous/);
  assert.throws(() => deliverReferencePrivateResponse(observer, 'onPrivateLoginResponse', login, null,
    Buffer.alloc(1048577)), /scope_rejected/);
  assert.doesNotThrow(() => deliverReferencePrivateResponse({}, 'onPrivateLoginResponse', login, null, body));
});
