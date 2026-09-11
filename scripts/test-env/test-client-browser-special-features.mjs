#!/usr/bin/env node
/** Pure positive-profile driver guards. Browser, filesystem and network effects are fenced. */
import fs from 'node:fs/promises';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { TextDecoder } from 'node:util';
import { fileURLToPath } from 'node:url';
import { EventEmitter } from 'node:events';
import vm from 'node:vm';

const ROOT = '/opt/goby-test/exec-work-m3e', ORIGIN = 'http://127.0.0.1:18196';
const SELF = fileURLToPath(import.meta.url);
const DRIVER = fileURLToPath(new URL('./client-browser-special-features.mjs', import.meta.url));
const SHARED = fileURLToPath(new URL('./client-browser-cross-user.mjs', import.meta.url));
const SHARED_SHA = '53fa6e9eb40f96ff7ace6821a0c64629ead1c95b5fc37188f28ab3d84b58b76b';
const SCOPE = 'schema27-positive-special-features-01', OLD = '268051d3ca734aefcf94e245fb25ad55';
const A = 'a'.repeat(32), B = 'b'.repeat(32), TOKEN = 'synthetic-positive-token', TOKEN_B = 'synthetic-positive-token-b';
const hash = value => createHash('sha256').update(value).digest('hex');
const syntheticHash = value => hash('synthetic-positive-driver:' + value);
const clone = value => JSON.parse(JSON.stringify(value));
const normalized = value => Array.isArray(value) ? value.map(normalized) : value && typeof value === 'object'
  ? Object.fromEntries(Object.keys(value).sort().map(key => [key, normalized(value[key])])) : value;
const same = (a, b) => JSON.stringify(normalized(a)) === JSON.stringify(normalized(b));
function requireThat(value) { if (!value) throw new Error('positive_driver_guard_failed'); }

function prepare(raw, name) {
  const end = raw.indexOf('\nif (process.argv[1]');
  requireThat(end > 0);
  const source = raw.slice(0, end).replace(/^#![^\r\n]*(?:\r?\n|$)/, '').replace(/^import [^\n]*;\r?\n/gm, '')
    .replace(/^export (?=(?:async )?function )/gm, '')
    .replaceAll('import.meta.url', JSON.stringify('file:///synthetic/' + name));
  requireThat(!/^\s*(?:import|export)\b/m.test(source) && !/\bimport\s*\(/.test(source));
  return source;
}
function loadCore(raw, sharedRaw) {
  requireThat(hash(sharedRaw) === SHARED_SHA);
  const effects = [], timers = new Map(); let nextTimer = 0;
  const fail = name => () => { effects.push(name); throw new Error('positive_driver_external_effect'); };
  const denied = name => new Proxy({}, { get: (_target, key) => fail(name + '.' + String(key))() });
  const sandbox = { fs: denied('fs'), constants: denied('constants'), http: denied('http'), path: path.posix,
    createHash, TextDecoder, fileURLToPath, Buffer, URL, URLSearchParams,
    createRequire: fail('require'), loadGobyAVFixture: fail('old_loader'), loadSpecialFeaturesFixture: fail('loader'),
    createBrowserSessionProof: fail('session_proof'), fetch: fail('fetch'), console: denied('console'),
    process: Object.freeze({ platform: 'linux', getuid: () => 0, env: { SSH_CONNECTION: '1 2 3 4' }, argv: [] }),
    setTimeout(callback, milliseconds) { const id = ++nextTimer; timers.set(id, { callback, milliseconds }); return id; },
    clearTimeout(id) { timers.delete(id); }, setInterval: fail('interval') };
  const context = vm.createContext(sandbox, { codeGeneration: { strings: false, wasm: false } });
  const sharedNames = ['browserRequestDiagnostic', 'safeBrowserFailure', 'sanitizeBrowserMessage', 'browserEventDiagnostic',
    'recordBrowserPageError', 'resanitizeBrowserDiagnostics', 'sanitizeBootstrapDOM', 'proxyResponsePlan', 'frameRequestContext',
    'forwardWebSocketConnect', 'forwardBrowserWebSocket', 'observedAuthority', 'validateReadPrincipal', 'itemState',
    'foreignAccessDenied', 'administratorAccessDenied', 'preparationResponseEvidence', 'homeNavigationLocation',
    'selectHomeControl', 'confirmedHomeNavigation', 'readBoundedJSON'];
  const coreNames = ['parseSpecialFeaturesArguments', 'requireProxyExecutionMode', 'requirePreparationFixture',
    'classifyBrowserRequest', 'proxyRequestPlan', 'reserveProxyRequest', 'forwardBrowserHTTP', 'bindOwnedMovieSource',
    'preparationRequestEvidence', 'ownAuxiliaryRequest', 'auxiliaryResponseEvidence', 'captureAuxiliaryResponse',
    'auxiliaryEvidence', 'comparePositiveState', 'positiveSpecialFeaturesResult', 'runSpecialFeaturesAcceptance'];
  new vm.Script('globalThis.__shared = (() => {' + prepare(sharedRaw, 'client-browser-cross-user.mjs') +
    '\nreturn {' + sharedNames.join(',') + '};})();').runInContext(context, { timeout: 1000 });
  new vm.Script('globalThis.__core = (() => { const {' + sharedNames.join(',') + '} = __shared;\n' +
    prepare(raw, 'client-browser-special-features.mjs') + '\nreturn {' + coreNames.join(',') + '};})();')
    .runInContext(context, { timeout: 1000 });
  requireThat(effects.length === 0 && timers.size === 0);
  return { api: sandbox.__core, effects, timers,
    fire(milliseconds) { for (const [id, timer] of [...timers]) if (timer.milliseconds === milliseconds) { timers.delete(id); timer.callback(); } } };
}

function profile() {
  const items = {};
  for (const [key, number, name, type] of [['positive', 1, 'Positive Movie', 'Movie'], ['empty', 2, 'Empty Movie', 'Movie'],
    ['alpha', 3, 'Alpha Bonus', 'Video'], ['deleted', 4, 'Middle Deleted Scene', 'Video'],
    ['zeta', 5, 'Zeta Bonus', 'Video'], ['trailer', 6, 'Delta Local Trailer', 'Trailer']]) {
    items[key] = { id: String(number).repeat(32), name, api_name: name, type: type === 'Trailer' ? 'Video' : type,
      api_type: type, path: '/opt/goby-fixtures/client-special-features-m3e-v1/Movies/' + name + '.mp4',
      parent_id: key === 'positive' || key === 'empty' ? '7'.repeat(32) : '1'.repeat(32),
      media_source_id: 'received-source-' + key };
  }
  return { items, library: { id: '8'.repeat(32), name: 'M3e Client Special Features Movies' },
    evidence: { schema: 27, schema_binding: { schema: 27 }, profile: { receipt_sha256: syntheticHash('receipt') } } };
}
function args() {
  return ['--candidate-sha256', syntheticHash('candidate'), '--source', ROOT + '/source-attempt-32',
    '--source-manifest-sha256', syntheticHash('manifest'), '--music-scan-receipt-sha256', 'e0b9ba686afb1950d4ab43c3ad5bc39d67090374704379103a29759c70aab93b',
    '--music-upgrade-chain', ROOT + '/client-music-upgrade-chain-source32.json', '--music-upgrade-chain-sha256', syntheticHash('chain'),
    '--profile-receipt-sha256', syntheticHash('receipt'), '--profile-report-sha256', syntheticHash('report'),
    '--profile-inspect-sha256', syntheticHash('inspect'), '--mode', 'acceptance-preparation', '--preparation-scope', SCOPE,
    '--output', ROOT + '/client-special-features-browser-synthetic'];
}
function source() { const item = profile().items.positive; return { item_id: item.id, source_id: item.media_source_id, path: item.path }; }
function prepScope() { return { mode: 'acceptance-preparation', phase: 'ui_movie', preparationScope: SCOPE,
  itemId: profile().items.positive.id, source: source(), userId: A, token: TOKEN, ordinary: true, loginCompleted: true, logoutStarted: false }; }
function prepURL(item = profile().items.positive.id) { return ORIGIN + '/emby/Items/' + item + '/PlaybackInfo'; }
function headers(body = Buffer.from('{}')) { return ['Host', '127.0.0.1:18196', 'Content-Length', String(body.length), 'Content-Type', 'application/json', 'X-Emby-Token', TOKEN]; }
function proxyState() { return { mode: 'acceptance-preparation', seen: 0, admitted: 0, completed: 0, rejected: 0, failed: 0,
  active: 0, login: 1, capabilities: 0, logout: 0, preparation: 0, requestBytes: 0, responseBytes: 0, upgrade_rejections: 0 }; }
function auxiliaryBytes(kind = 'SpecialFeatures') {
  return Buffer.from(JSON.stringify((kind === 'SpecialFeatures' ? ['alpha', 'deleted', 'zeta'] : ['trailer']).map(key => {
    const item = profile().items[key]; return { Id: item.id, Type: item.api_type, Name: item.api_name, Path: item.path, ParentId: item.parent_id };
  })));
}
function auxiliaryEntry(api, user = A, fingerprint = hash(TOKEN), kind = 'SpecialFeatures', index = 0) {
  return { index, own_auxiliary: true, auxiliary_kind: kind, frame_owned: true, phase: 'ui_movie', method: 'GET',
    auxiliary_user_id: user, auxiliary_item_id: profile().items.positive.id, status: 200, finished: true, failed: false,
    token_matches_session: true, token_fingerprint: fingerprint,
    auxiliary_response: api.auxiliaryResponseEvidence(200, auxiliaryBytes(kind), profile(), kind) };
}
function completeReport(api) {
  const p = profile();
  return { format: 6, mode: 'acceptance-preparation', preparation_scope: SCOPE, failure: null,
    fixture: p.evidence, profile_items: p.items, profile_library: p.library,
    accounts: [A, B].map((id, index) => {
      const fp = hash(index ? TOKEN_B : TOKEN), requests = [auxiliaryEntry(api, id, fp)];
      return { id, token_fingerprint: fp, login: { status: 200, request_count: 1 }, principal_confirmed: true,
        ordinary_authority_confirmed: true, page_error_count: 0, own_item_reads: 20, requests,
        foreign_read: { status: 403, error_code: 'access_denied', target_user_id: index ? A : B },
        comparison: { items_unchanged: true, preferences_unchanged: true, configuration_unchanged: true, policy_unchanged: true },
        logout: { status: 204, login_view_visible: true }, closed: true,
        session_proof: { outcome: 'all_observed_logout_tokens_rejected', entries: [{ token_fingerprint: fp,
          ui_request: { response_status: 204 }, verification: { status: 401 } }] },
        proxy: { ...proxyState(), preparation: 1, logout: 1, seen: 20, admitted: 20, completed: 20 },
        proxy_login_status: 200, proxy_logout: { completed: true, status: 204, token_fingerprint: fp },
        preparation: { item_id: p.items.positive.id, user_id: id, token_fingerprint: fp, request_validated: true, completed: true,
          response: { status: 200, validated: true }, ui_status: 200, ui_finished: true },
        special_features: api.auxiliaryEvidence(requests, id, fp, p, 'SpecialFeatures'),
        local_trailers: api.auxiliaryEvidence(requests, id, fp, p, 'LocalTrailers'),
        ui: { outcome: 'passed', special_features_cards: ['alpha', 'deleted', 'zeta'].map(key => ({ item_id: p.items[key].id,
          api_name: p.items[key].api_name, visible: true, title_matches: true, card_present: true,
          card_id_present: true, id_matches_when_present: true, in_viewport: true })) },
        websocket: { seen: 1, admitted: 1, opened: 1, closed: 1, active: 0, failed: 0, control_attempts: 0,
          entries: [{ outcome: 'closed', handshake_delivered: true, upstream_status: 101, connect_transport: true,
            proxy_connect_status: 200, token_fingerprint: fp, server_control_attempted: false }] },
        network: { forbidden_mutations: 0, playback_attempts: 0, observer_errors: 0, overflow: 0, guard_errors: 0 } };
    }) };
}

class Response extends EventEmitter {
  constructor() { super(); this.writableFinished = false; this.headersSent = false; this.destroyed = false; this.entity = null; }
  writeHead(status, headers) { this.status = status; this.headers = headers; this.headersSent = true; }
  end(body, callback) { this.entity = Buffer.from(body ?? ''); this.writableFinished = true; callback?.(); }
  destroy() { this.destroyed = true; this.emit('close'); }
}
async function relay(api, options = {}) {
  const body = options.body ?? Buffer.from('{}'), state = options.state ?? proxyState(), request = new EventEmitter(), response = new Response(), calls = [];
  Object.assign(request, { url: options.url ?? prepURL(), method: 'POST', rawHeaders: headers(body), complete: true, rawTrailers: [] });
  const transport = { request(input, callback) {
    const outgoing = new EventEmitter(); Object.assign(outgoing, { destroyed: false, setTimeout() {},
      destroy() { this.destroyed = true; }, end(bytes) {
        calls.push({ input, body: Buffer.from(bytes) });
        const incoming = new EventEmitter(); Object.assign(incoming, { statusCode: options.status ?? 200,
          rawHeaders: ['Content-Type', 'application/json', 'Content-Length', String((options.reply ?? Buffer.from('[]')).length)],
          rawTrailers: [], complete: true, setTimeout() {}, destroy() {} });
        callback(incoming); incoming.emit('data', options.reply ?? Buffer.from('[]')); incoming.emit('end');
      } }); return outgoing;
  } };
  const events = [], policy = { itemId: profile().items.positive.id, state,
    intents: () => ({ login: true, logout: false, preparation: true, ownershipLost: false }),
    onEvent: event => events.push(event), onRequest: options.onRequest, onResponse: options.onResponse };
  const pending = api.forwardBrowserHTTP(request, response, policy, transport);
  request.emit('data', body); request.emit('end'); await pending;
  return { state, response, calls, events };
}

export async function runPositiveDriverGuards(raw, sharedRaw) {
  const loaded = loadCore(raw, sharedRaw), api = loaded.api, cases = [], tests = [];
  const test = (name, action) => cases.push({ name, action });
  const reject = action => { let refused = false; try { action(); } catch { refused = true; } requireThat(refused); };
  test('required_positive_scope_and_profile_pins', () => {
    requireThat(api.parseSpecialFeaturesArguments(args())['preparation-scope'] === SCOPE);
    for (const index of [0, 12, 14, 16, 18, 20]) {
      const value = args(); value.splice(index, 2); reject(() => api.parseSpecialFeaturesArguments(value));
    }
    for (const scope of ['schema27-original-movie-01', 'source28-page-error-01', '', null]) reject(() => api.requireProxyExecutionMode('acceptance-preparation', scope));
    for (const mode of ['acceptance', 'prelogin', 'unsupported']) reject(() => api.requireProxyExecutionMode(mode, SCOPE));
  });
  test('current_profile_is_required_and_old_movie_cannot_be_relabelled', () => {
    requireThat(api.requirePreparationFixture(profile()) === undefined);
    for (const mutate of [p => { p.evidence.schema = 26; }, p => { delete p.evidence.profile; }, p => { p.items.positive.id = OLD; }]) {
      const p = profile(); mutate(p); reject(() => api.requirePreparationFixture(p));
    }
  });
  test('only_dynamic_positive_movie_post_is_admitted', () => {
    requireThat(api.classifyBrowserRequest(prepURL(), 'POST', 'fetch', 'acceptance-preparation', profile().items.positive.id).kind === 'preparation');
    for (const item of [OLD, profile().items.empty.id, profile().items.alpha.id]) {
      requireThat(api.classifyBrowserRequest(prepURL(item), 'POST', 'fetch', 'acceptance-preparation', profile().items.positive.id).allow === false);
    }
    for (const method of ['GET', 'PUT', 'PATCH']) requireThat(api.classifyBrowserRequest(prepURL(), method, '', 'acceptance-preparation', profile().items.positive.id).allow === false);
    requireThat(api.classifyBrowserRequest(prepURL(), 'POST').allow === false);
  });
  test('media_playing_control_and_external_writes_remain_denied', () => {
    for (const [url, method] of [[ORIGIN + '/emby/Audio/' + profile().items.alpha.id + '/stream', 'GET'],
      [ORIGIN + '/emby/Sessions/Playing', 'POST'], [ORIGIN + '/emby/Sessions/example/Command/Play', 'POST'],
      [ORIGIN + '/emby/Library/Refresh', 'POST'], ['https://example.invalid/submit', 'POST']]) {
      requireThat(api.classifyBrowserRequest(url, method, '', 'acceptance-preparation', profile().items.positive.id).allow === false);
    }
  });
  test('received_media_source_is_bound_without_assuming_item_id', () => {
    const item = profile().items.positive, value = { Id: item.id, Type: 'Movie', Name: item.api_name, Path: item.path,
      MediaSources: [{ Id: item.media_source_id, ItemId: item.id, Path: item.path, Protocol: 'File', IsRemote: false }] };
    const expected = { id: item.id, name: item.api_name, path: item.path, media_source_id: item.media_source_id };
    requireThat(api.bindOwnedMovieSource(value, expected).source_id === item.media_source_id);
    value.MediaSources[0].Id = item.id; reject(() => api.bindOwnedMovieSource(value, expected));
  });
  test('preparation_binds_scope_user_token_and_existing_session_absence', () => {
    const scope = prepScope(), body = Buffer.from(JSON.stringify({ UserId: A, MediaSourceId: scope.source.source_id }));
    const evidence = api.preparationRequestEvidence(prepURL(), headers(body), body, scope);
    requireThat(evidence.item_id === scope.itemId && evidence.token_fingerprint === hash(TOKEN) && evidence.existing_play_or_live_session_requested === false);
    for (const mutate of [s => { s.preparationScope = 'schema27-original-movie-01'; }, s => { s.ordinary = false; },
      s => { s.phase = 'ui_home'; }, s => { s.itemId = OLD; }, s => { s.token = TOKEN_B; }]) {
      const value = prepScope(); mutate(value); reject(() => api.preparationRequestEvidence(prepURL(), headers(body), body, value));
    }
    for (const payload of [{ CurrentPlaySessionId: 'old' }, { LiveStreamId: 'old' }, { UserId: B }, { MediaSourceId: 'foreign' }]) {
      const bytes = Buffer.from(JSON.stringify(payload)); reject(() => api.preparationRequestEvidence(prepURL(), headers(bytes), bytes, prepScope()));
    }
  });
  test('physical_relay_preserves_bytes_and_consumes_only_one_preparation', async () => {
    const state = proxyState(), body = Buffer.from('{"UserId":"' + A + '"}'), reply = Buffer.from('{"actual":true}');
    const first = await relay(api, { state, body, reply, onRequest(request, _plan, bytes) {
      api.preparationRequestEvidence(request.url, request.rawHeaders, bytes, prepScope());
    } });
    requireThat(first.calls.length === 1 && first.calls[0].body.equals(body) && first.response.entity.equals(reply) &&
      first.calls[0].input.hostname === '127.0.0.1' && first.calls[0].input.port === 18196 &&
      first.calls[0].input.path.includes(profile().items.positive.id) && state.preparation === 1 && state.completed === 1);
    const second = await relay(api, { state, body });
    requireThat(second.calls.length === 0 && state.preparation === 1 && second.events[0].outcome === 'rejected');
  });
  test('rejected_preparation_does_not_relay_or_reset_its_intent', async () => {
    const state = proxyState(), first = await relay(api, { state, onRequest() { throw new Error('rejected'); } });
    requireThat(first.calls.length === 0 && state.preparation === 1 && state.failed === 1);
    const retry = await relay(api, { state }); requireThat(retry.calls.length === 0 && state.preparation === 1);
    const old = await relay(api, { url: prepURL(OLD) }); requireThat(old.calls.length === 0 && old.state.preparation === 0);
  });
  test('auxiliary_routes_require_owned_user_movie_and_real_get', () => {
    for (const kind of ['SpecialFeatures', 'LocalTrailers']) {
      const url = ORIGIN + '/emby/Users/' + A + '/Items/' + profile().items.positive.id + '/' + kind;
      requireThat(api.ownAuxiliaryRequest(url, 'GET', A, profile().items.positive.id, kind));
      requireThat(!api.ownAuxiliaryRequest(url, 'POST', A, profile().items.positive.id, kind));
      requireThat(!api.ownAuxiliaryRequest(url.replace(A, B), 'GET', A, profile().items.positive.id, kind));
      requireThat(!api.ownAuxiliaryRequest(url + '?UserId=' + B, 'GET', A, profile().items.positive.id, kind));
    }
  });
  test('special_features_requires_exact_three_profile_resources', () => {
    const result = api.auxiliaryResponseEvidence(200, auxiliaryBytes(), profile(), 'SpecialFeatures');
    requireThat(result.validated && result.item_count === 3 && result.item_ids.length === 3);
    for (const value of [[], [{ Id: profile().items.trailer.id }], JSON.parse(auxiliaryBytes()).slice(0, 2)]) {
      requireThat(!api.auxiliaryResponseEvidence(200, Buffer.from(JSON.stringify(value)), profile(), 'SpecialFeatures').validated);
    }
    const duplicate = JSON.parse(auxiliaryBytes()); duplicate[2] = duplicate[0];
    requireThat(!api.auxiliaryResponseEvidence(200, Buffer.from(JSON.stringify(duplicate)), profile(), 'SpecialFeatures').validated);
  });
  test('auxiliary_projection_and_transfer_failures_are_not_success', () => {
    for (const mutate of [v => { v[0].Name = 'foreign'; }, v => { v[0].Type = 'Movie'; },
      v => { v[0].ParentId = profile().library.id; }, v => { v[0].Path = '/tmp/foreign'; }]) {
      const value = JSON.parse(auxiliaryBytes()); mutate(value);
      requireThat(!api.auxiliaryResponseEvidence(200, Buffer.from(JSON.stringify(value)), profile(), 'SpecialFeatures').validated);
    }
    for (const status of [204, 304, 401, 404, 500]) requireThat(!api.auxiliaryResponseEvidence(status, auxiliaryBytes(), profile(), 'SpecialFeatures').validated);
    for (const bytes of [Buffer.from('[] trailing'), Buffer.from([0xc3, 0x28]), Buffer.alloc(2097153)]) {
      requireThat(!api.auxiliaryResponseEvidence(200, bytes, profile(), 'SpecialFeatures').validated);
    }
  });
  test('local_trailer_absence_is_explicitly_not_client_evidence', () => {
    const absent = api.auxiliaryEvidence([], A, hash(TOKEN), profile(), 'LocalTrailers');
    requireThat(absent.result === 'not_observed' && absent.client_evidence === false && absent.validated === false);
    const entry = auxiliaryEntry(api, A, hash(TOKEN), 'LocalTrailers');
    requireThat(api.auxiliaryEvidence([entry], A, hash(TOKEN), profile(), 'LocalTrailers').validated);
  });
  test('capture_waits_for_real_completion_and_never_reads_auth_body', async () => {
    let bodies = 0, bytes;
    const request = { serviceWorker: () => null, url: () => ORIGIN + '/emby/Users/' + A + '/Items/' + profile().items.positive.id + '/SpecialFeatures', method: () => 'GET' };
    const response = { request: () => request, status: () => 200, headers: () => ({ 'content-type': 'application/json' }),
      finished: async () => null, body: async () => { bodies += 1; bytes = auxiliaryBytes(); return bytes; } };
    requireThat((await api.captureAuxiliaryResponse(response, A, profile(), 'SpecialFeatures')).validated && bodies === 1 && bytes.every(value => value === 0));
    request.url = () => ORIGIN + '/emby/Users/AuthenticateByName'; let refused = false;
    try { await api.captureAuxiliaryResponse(response, A, profile(), 'SpecialFeatures'); } catch { refused = true; }
    requireThat(refused && bodies === 1);
  });
  test('auxiliary_wait_is_bounded_and_partial_transfer_cannot_pass', async () => {
    const request = { serviceWorker: () => null, url: () => ORIGIN + '/emby/Users/' + A + '/Items/' + profile().items.positive.id + '/SpecialFeatures', method: () => 'GET' };
    let bodies = 0;
    const response = { request: () => request, status: () => 200, headers: () => ({ 'content-type': 'application/json' }),
      finished: () => new Promise(() => {}), body: async () => { bodies += 1; return auxiliaryBytes(); } };
    const pending = api.captureAuxiliaryResponse(response, A, profile(), 'SpecialFeatures');
    loaded.fire(5000); let refused = false;
    try { await pending; } catch { refused = true; }
    requireThat(refused && bodies === 0 && loaded.timers.size === 0);
  });
  test('auxiliary_evidence_requires_matching_frame_user_token_and_all_requests', () => {
    const entry = auxiliaryEntry(api);
    requireThat(api.auxiliaryEvidence([entry], A, hash(TOKEN), profile(), 'SpecialFeatures').validated);
    for (const mutate of [v => { v.finished = false; }, v => { v.failed = true; }, v => { v.frame_owned = false; },
      v => { v.phase = 'ui_home'; }, v => { v.token_fingerprint = hash(TOKEN_B); }, v => { v.auxiliary_user_id = B; }]) {
      const bad = clone(entry); mutate(bad);
      requireThat(!api.auxiliaryEvidence([entry, { ...bad, index: 1 }], A, hash(TOKEN), profile(), 'SpecialFeatures').validated);
    }
    requireThat(!api.auxiliaryEvidence([], A, hash(TOKEN), profile(), 'SpecialFeatures').validated);
  });
  test('ten_userdata_projections_are_preserved_without_claiming_storage_equality', () => {
    const value = { user_id: A, items: Array.from({ length: 10 }, (_, n) => ({ id: String(n).repeat(32), user_data: { Played: false } })),
      preferences: {}, configuration: {}, policy: {} };
    requireThat(Object.values(api.comparePositiveState(value, clone(value))).every(v => v === true));
    const changed = clone(value); changed.items[0].user_data.Played = true;
    requireThat(api.comparePositiveState(value, changed).items_unchanged === false);
    changed.items.pop(); reject(() => api.comparePositiveState(value, changed));
  });
  test('complete_positive_scope_can_pass_with_unobserved_local_trailers', () => {
    const value = completeReport(api);
    requireThat(api.positiveSpecialFeaturesResult(value) === true && value.accounts.every(a => a.local_trailers.result === 'not_observed'));
  });
  test('missing_cards_response_or_pageerror_cannot_be_hidden_by_other_success', () => {
    for (const slot of [0, 1]) for (const mutate of [v => { v.page_error_count = 1; }, v => { delete v.page_error_count; },
      v => { v.ui.special_features_cards.pop(); }, v => { v.ui.special_features_cards[0].id_matches_when_present = false; },
      v => { v.ui.special_features_cards[0].in_viewport = false; }, v => { v.requests = []; },
      v => { v.special_features.validated = false; }, v => { v.preparation.completed = false; }]) {
      const value = completeReport(api); mutate(value.accounts[slot]); requireThat(api.positiveSpecialFeaturesResult(value) === false);
    }
  });
  test('local_trailer_claims_cannot_be_fabricated_or_failed_requests_ignored', () => {
    const absent = completeReport(api); absent.accounts[0].local_trailers.result = 'passed';
    requireThat(api.positiveSpecialFeaturesResult(absent) === false);
    const failed = completeReport(api), entry = auxiliaryEntry(api, A, hash(TOKEN), 'LocalTrailers', 1);
    entry.status = 404; failed.accounts[0].requests.push(entry);
    failed.accounts[0].local_trailers = api.auxiliaryEvidence(failed.accounts[0].requests, A, hash(TOKEN), profile(), 'LocalTrailers');
    requireThat(api.positiveSpecialFeaturesResult(failed) === false);
  });
  test('fresh_auth_logout_ws_and_user_scope_remain_mandatory', () => {
    for (const mutate of [v => { v.ordinary_authority_confirmed = false; }, v => { v.logout.status = 200; },
      v => { v.session_proof.entries[0].verification.status = 200; }, v => { v.websocket.closed = 0; },
      v => { v.proxy.preparation = 2; }, v => { v.foreign_read.status = 200; }, v => { v.own_item_reads = 8; }]) {
      const value = completeReport(api); mutate(value.accounts[0]); requireThat(!api.positiveSpecialFeaturesResult(value));
    }
    const old = completeReport(api); old.format = 5; requireThat(!api.positiveSpecialFeaturesResult(old));
    old.format = 6; old.preparation_scope = 'schema27-original-movie-01'; requireThat(!api.positiveSpecialFeaturesResult(old));
  });
  test('missing_explicit_inputs_stop_before_any_external_effect', async () => {
    let refused = false;
    try { await api.runSpecialFeaturesAcceptance({}); } catch { refused = true; }
    requireThat(refused && loaded.effects.length === 0 && loaded.timers.size === 0);
  });
  for (const { name, action } of cases) {
    let pass = false;
    try { await action(); requireThat(loaded.effects.length === 0 && loaded.timers.size === 0); pass = true; } catch { /* Keep reports free of input and exception text. */ }
    tests.push({ name, pass });
  }
  return { result: tests.every(test => test.pass) ? 'passed' : 'blocked', mode: 'pure', harness_guards_only: true, client_acceptance: false,
    source_sha256: hash(raw), shared_sha256: hash(sharedRaw), counts: { executed: tests.length,
      passed: tests.filter(test => test.pass).length, failed: tests.filter(test => !test.pass).length }, tests };
}

if (process.argv[1] && path.resolve(process.argv[1]) === SELF) {
  try {
    requireThat(process.platform === 'linux' && process.getuid?.() === 0 && process.env.SSH_CONNECTION &&
      process.argv.length === 3 && process.argv[2] === '--pure');
    const report = await runPositiveDriverGuards(await fs.readFile(DRIVER, 'utf8'), await fs.readFile(SHARED, 'utf8'));
    process.stdout.write(JSON.stringify({ result: report.result, mode: 'pure', counts: report.counts,
      failed: report.tests.filter(test => !test.pass).map(test => test.name) }) + '\n');
    if (report.result !== 'passed') process.exitCode = 1;
  } catch (error) {
    process.stdout.write(JSON.stringify({ result: 'blocked', mode: 'pure', phase: 'initialization',
      error_class: ['SyntaxError', 'TypeError', 'RangeError', 'Error'].includes(error?.name) ? error.name : 'other' }) + '\n');
    process.exitCode = 1;
  }
}
