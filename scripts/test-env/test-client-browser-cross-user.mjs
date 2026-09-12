#!/usr/bin/env node
/** Pure guards for the two-user browser observer. Never reads fixture credentials. */
import fs from 'node:fs/promises';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { EventEmitter } from 'node:events';
import { fileURLToPath } from 'node:url';
import { TextDecoder } from 'node:util';
import vm from 'node:vm';

const ROOT = '/opt/goby-test/exec-work-m3e';
const ORIGIN = 'http://127.0.0.1:18196';
const DIRECT = 'http://127.0.0.1:18198';
const SELF = fileURLToPath(import.meta.url);
const SOURCE = fileURLToPath(new URL('./client-browser-cross-user.mjs', import.meta.url));
const A_ID = 'a'.repeat(32), B_ID = 'b'.repeat(32), ADMIN_ID = 'c'.repeat(32), SERVER_ID = 'd'.repeat(32);
const A_TOKEN = 'synthetic-a-token', B_TOKEN = 'synthetic-b-token';
const A_PASSWORD = '1'.repeat(48), B_PASSWORD = '2'.repeat(48);
const ASSET_PAYLOAD = 'synthetic-private-asset-payload';
const PREPARATION_ITEM = '268051d3ca734aefcf94e245fb25ad55';
const PREPARATION_SCOPE = 'schema27-original-movie-01';
const MOVIE_PATH = '/opt/goby-fixtures/client-m3e/Movies/M3e Client Movie.mp4';
const CORE_NAMES = ['parseCrossUserArguments', 'bindCrossUserCredentials', 'classifyBrowserRequest', 'observedAuthority',
  'validateReadPrincipal', 'itemState', 'compareCrossUserState', 'foreignAccessDenied', 'readBoundedJSON', 'crossUserResult',
  'browserRequestDiagnostic', 'safeBrowserFailure', 'sanitizeBootstrapDOM', 'preloginDiagnosticsComplete',
  'requireProxyExecutionMode', 'proxyRequestPlan', 'proxyResponsePlan', 'reserveProxyRequest', 'forwardBrowserHTTP',
  'runCrossUserAcceptance', 'frameRequestContext', 'websocketRequestPlan', 'websocketResponsePlan',
  'websocketFrameState', 'inspectWebSocketFrames', 'forwardBrowserWebSocket', 'administratorAccessDenied',
  'websocketConnectPlan', 'websocketConnectInner', 'forwardWebSocketConnect', 'bindOwnedMovieSource',
  'preparationRequestEvidence', 'preparationResponseEvidence', 'homeNavigationLocation', 'selectHomeControl', 'confirmedHomeNavigation',
  'sanitizeBrowserMessage', 'browserEventDiagnostic', 'recordBrowserPageError', 'resanitizeBrowserDiagnostics',
  'requirePreparationFixture', 'ownSpecialFeaturesRequest', 'specialFeaturesResponseEvidence',
  'captureSpecialFeaturesResponse', 'specialFeaturesEvidence', 'websocketHandshakeBudget', 'websocketLifetimeBudget',
  'libraryChangedCatalogRequest', 'libraryChangedSocketAuthority', 'libraryChangedSocketMatch', 'createLibraryChangedBrowserActor',
  'createLibraryChangedSource55BrowserActor'];
const hash = value => createHash('sha256').update(value).digest('hex');
const syntheticHash = label => hash(`synthetic-cross-user:${label}`);
const clone = value => JSON.parse(JSON.stringify(value));
const normalized = value => Array.isArray(value) ? value.map(normalized)
  : value !== null && typeof value === 'object'
    ? Object.fromEntries(Object.keys(value).sort().map(key => [key, normalized(value[key])])) : value;
const same = (left, right) => JSON.stringify(normalized(left)) === JSON.stringify(normalized(right));
function requireThat(value) { if (!value) throw new Error('cross_user_test_failed'); }

function loadPureCore(raw) {
  requireThat(typeof raw === 'string' && raw.length > 0 && raw.length <= 2 * 1024 * 1024);
  let source = raw;
  const entrypoint = '\nif (process.argv[1] && path.resolve(process.argv[1]) === SELF) {';
  requireThat(source.split(entrypoint).length === 2);
  // The guarded CLI contains top-level await; compile only the importable module body.
  source = source.slice(0, source.indexOf(entrypoint));
  for (const statement of ["import fs from 'node:fs/promises';", "import { constants } from 'node:fs';",
    "import http from 'node:http';", "import path from 'node:path';", "import { createHash } from 'node:crypto';",
    "import { createRequire } from 'node:module';", "import { fileURLToPath } from 'node:url';",
    "import { TextDecoder } from 'node:util';", "import { loadGobyAVFixture } from './client-browser-goby-fixture.mjs';",
    "import { createBrowserSessionProof } from './client-browser-session-proof.mjs';"]) {
    requireThat(source.split(statement).length === 2);
    source = source.replace(statement, '');
  }
  source = source.replace(/^export (?=(?:async )?function )/gm, '');
  requireThat(!/^\s*(?:import|export)\b/m.test(source) && !/\bimport\s*\(/.test(source));
  requireThat(source.includes('import.meta.url'));
  source = source.replaceAll('import.meta.url', JSON.stringify('file:///synthetic/client-browser-cross-user.mjs'));
  const effects = [], timers = new Map();
  let nextTimer = 0;
  const forbidden = name => () => { effects.push(name); throw new Error('cross_user_external_effect'); };
  const denied = name => new Proxy(Object.create(null), {
    get: (_target, key) => forbidden(`${name}.${String(key)}`)(),
    set: () => forbidden(`${name}.write`)(),
  });
  const context = vm.createContext({ URL, Buffer, TextDecoder, createHash, fileURLToPath, path: path.posix,
    fs: denied('fs'), constants: denied('constants'), http: denied('http'),
    createRequire: forbidden('createRequire'), loadGobyAVFixture: forbidden('loadGobyAVFixture'),
    createBrowserSessionProof: forbidden('createBrowserSessionProof'), fetch: forbidden('fetch'),
    console: denied('console'), setInterval: forbidden('setInterval'), setImmediate: forbidden('setImmediate'),
    process: new Proxy(Object.create(null), {
      get: (_target, key) => {
        if (key === 'argv') return Object.freeze(['node', '/synthetic/test-client-browser-cross-user.mjs']);
        if (key === 'platform') return 'linux';
        if (key === 'getuid') return () => 0;
        if (key === 'env') return Object.freeze({ SSH_CONNECTION: '1 2 3 4', DEBUG: '', PWDEBUG: '' });
        return forbidden(`process.${String(key)}`)();
      },
      set: () => forbidden('process.write')(),
    }),
    setTimeout(callback, milliseconds) {
      requireThat(typeof callback === 'function' && Number.isFinite(milliseconds) && milliseconds > 0);
      const id = ++nextTimer; timers.set(id, { callback, milliseconds }); return id;
    },
    clearTimeout(id) { timers.delete(id); },
  }, { codeGeneration: { strings: false, wasm: false } });
  new vm.Script(source + `\nglobalThis.__crossUserCore = Object.freeze({${CORE_NAMES.join(',')}});`,
    { filename: 'client-browser-cross-user.pure.mjs' }).runInContext(context, { timeout: 1000 });
  requireThat(effects.length === 0 && timers.size === 0);
  return { core: context.__crossUserCore, effects, timers,
    fireTimers(milliseconds = null) {
      for (const [id, timer] of [...timers]) if (milliseconds === null || timer.milliseconds === milliseconds) {
        timers.delete(id); timer.callback();
      }
    } };
}

function validArguments() {
  return ['--candidate-sha256', syntheticHash('candidate'), '--source', `${ROOT}/source-attempt-9002`,
    '--source-manifest-sha256', syntheticHash('manifest'), '--music-scan-receipt-sha256', syntheticHash('scan'),
    '--music-upgrade-chain', `${ROOT}/client-music-upgrade-chain-synthetic.json`,
    '--music-upgrade-chain-sha256', syntheticHash('chain'), '--output', `${ROOT}/client-cross-user-synthetic`];
}

function credentials() {
  const admin = { username: 'm3e-client-administrator', password: '3'.repeat(48) };
  return {
    stateFile: { sha256: syntheticHash('state'), value: { marker: 'goby-m3e-client-acceptance-v1', server_id: SERVER_ID,
      admin_id: ADMIN_ID, viewer_id: B_ID, added_viewer: { user_id: A_ID }, browser_sha256: syntheticHash('b-browser') } },
    aFile: { sha256: syntheticHash('a-browser'), value: { marker: 'goby-m3e-client-acceptance-v1',
      account_marker: 'goby-m3e-goby-av-user-v1', base_url: ORIGIN, direct_url: DIRECT, admin: clone(admin),
      viewer: { username: 'm3e-goby-av-client', password: A_PASSWORD, userId: A_ID, serverId: SERVER_ID } } },
    bFile: { sha256: syntheticHash('b-browser'), value: { marker: 'goby-m3e-client-acceptance-v1',
      base_url: ORIGIN, direct_url: DIRECT, admin: clone(admin), viewer: { username: 'm3e-client-viewer', password: B_PASSWORD } } },
    fixture: { accountId: A_ID, serverId: SERVER_ID,
      evidence: { fixture_state_sha256: syntheticHash('state'), browser_alias_sha256: syntheticHash('a-browser') } },
  };
}

function actor(slot = 'A') {
  return { slot, id: slot === 'A' ? A_ID : B_ID, username: slot === 'A' ? 'm3e-goby-av-client' : 'm3e-client-viewer' };
}
function principal(slot = 'A') {
  const user = actor(slot);
  return { Id: user.id, Name: user.username, ServerId: SERVER_ID, HasPassword: true,
    Policy: { IsAdministrator: false, IsDisabled: false, EnableAllFolders: true }, Configuration: { OrderedViews: [] } };
}
function snapshot(slot = 'A') {
  return { user_id: actor(slot).id,
    items: [1, 2, 3, 4].map(number => ({ id: String(number).repeat(32),
      user_data: { Played: false, IsFavorite: false, PlayCount: 0, PlaybackPositionTicks: 0 } })),
    preferences: { HomeSections: ['smalllibrarytiles', 'resume'] }, configuration: { OrderedViews: [] },
    policy: { IsAdministrator: false, IsDisabled: false, EnableAllFolders: true } };
}
function completedReport() {
  return { mode: 'acceptance', failure: null, accounts: ['A', 'B'].map(slot => {
    const tokenFingerprint = hash(slot === 'A' ? A_TOKEN : B_TOKEN);
    return { ...actor(slot), login: { status: 200, request_count: 1 }, principal_confirmed: true,
      ordinary_authority_confirmed: true, proxy_login_status: 200, page_error_count: 0,
      ui: { outcome: 'passed' }, own_item_reads: 8,
      foreign_read: { status: 403, error_code: 'access_denied', target_user_id: slot === 'A' ? B_ID : A_ID },
      comparison: { items_unchanged: true, preferences_unchanged: true, configuration_unchanged: true, policy_unchanged: true },
      logout: { status: 204, login_view_visible: true }, closed: true, token_fingerprint: tokenFingerprint,
      session_proof: { outcome: 'all_observed_logout_tokens_rejected', entries: [{ token_fingerprint: tokenFingerprint,
        ui_request: { response_status: 204 }, verification: { status: 401 } }] },
      proxy: { ...proxyState('acceptance'), seen: 12, admitted: 12, completed: 12, login: 1, capabilities: 1, logout: 1 },
      proxy_logout: { source: 'physical-http-forwarding-proxy-request', completed: true, status: 204, token_fingerprint: tokenFingerprint },
      websocket: { ...websocketState(), seen: 1, admitted: 1, opened: 1, closed: 1, entries: [{ outcome: 'closed',
        handshake_delivered: true, upstream_status: 101, token_fingerprint: tokenFingerprint, server_control_attempted: false, closed: true }] },
      network: { forbidden_mutations: 0, playback_attempts: 0, observer_errors: 0, overflow: 0, guard_errors: 0 } };
  }) };
}

function schema27FixtureEvidence() {
  return { schema: 27, schema_binding: { schema: 27 }, music_scan: { schema: 25, upgrade_lineage: { to_schema: 27 } } };
}

function specialFeaturesEntry(slot = 'A', index = 0) {
  return { index, phase: 'ui_movie', own_special_features: true, frame_owned: true, method: 'GET',
    special_features_user_id: actor(slot).id, special_features_item_id: PREPARATION_ITEM,
    status: 200, finished: true, failed: false, token_matches_session: true,
    token_fingerprint: hash(slot === 'A' ? A_TOKEN : B_TOKEN),
    special_features_response: { status: 200, response_bytes: 2, json_array: true, item_count: 0, validated: true, reason: null } };
}

function completedPreparationReport(api) {
  const report = completedReport(); report.mode = 'acceptance-preparation'; report.preparation_scope = PREPARATION_SCOPE;
  report.fixture = schema27FixtureEvidence();
  for (const [index, account] of report.accounts.entries()) {
    account.proxy.mode = report.mode; account.proxy.preparation = 1;
    account.proxy.seen += 1; account.proxy.admitted += 1; account.proxy.completed += 1;
    const source = ownedMovieSource(), body = JSON.parse(preparationBody().toString('utf8')); body.UserId = account.id;
    const bytes = Buffer.from(JSON.stringify(body));
    account.preparation_source = source;
    account.preparation = { request_validated: true, completed: true, ...source, user_id: account.id,
      token_fingerprint: account.token_fingerprint, request_bytes: bytes.length, request_sha256: hash(bytes),
      existing_play_or_live_session_requested: false, ui_status: 200, ui_finished: true,
      response: { status: 200, validated: true, play_session_id: 'play_' + String(index + 1).repeat(32), reason: null } };
    account.requests = [specialFeaturesEntry(account.slot)];
    account.special_features = api.specialFeaturesEvidence(account.requests, account.id, account.token_fingerprint);
  }
  return report;
}

function specialFeaturesResponseMock(options = {}) {
  const state = { finished_calls: 0, body_calls: 0, bytes: null };
  const request = { url: () => options.url ?? ORIGIN + '/emby/Users/' + A_ID + '/Items/' + PREPARATION_ITEM + '/SpecialFeatures',
    method: () => options.method ?? 'GET', serviceWorker: () => options.worker ?? null };
  return { state, request: () => request, status: () => options.status ?? 200,
    headers: () => options.headers ?? { 'content-type': 'application/json; charset=utf-8', 'content-length': '2' },
    async finished() { state.finished_calls += 1; return options.finish ? options.finish() : null; },
    async body() {
      state.body_calls += 1;
      if (options.readBody) return options.readBody();
      state.bytes = Buffer.from(options.body ?? '[]'); return state.bytes;
    } };
}

function bootstrapDOM() {
  return { ready_state: 'complete', title_kind: 'emby', body_present: true, body_character_count: 37,
    visible: { forms: 1, password_forms: 1, text_inputs: 1, password_inputs: 1, buttons: 2, links: 3,
      sign_in_buttons: 1, manual_login_controls: 0 },
    milestones: { manual_login: false, sign_in: true, select_server: false, connect_to_server: false,
      loading: false, connection_error: false },
    scripts: { total: 3, external: 2, module: 1 },
    service_worker: { available: true, controller_present: false, registration_count: 0, query_failed: false } };
}

function preloginReport() {
  return { mode: 'prelogin', client_acceptance: false, failure: null, api_reads: [], accounts: [{ ...actor(),
    prelogin: { ready: true, credentials_submitted: false, expected_account_only: true,
      service_workers: 'allow', all_http_via_proxy: true, client_acceptance: false },
    proxy: { ...proxyState(), seen: 3, admitted: 3, completed: 3 },
    websocket: { ...websocketState(), entries: [] },
    closed: true, login: { status: null, request_count: 0, attempted: false, credentials_filled: false },
    logout: { status: null, login_view_visible: false }, own_item_reads: 0,
    session_proof: { outcome: 'no_logout_observed', entries: [] },
    bootstrap_diagnostics: [{ label: 'login_entry_ready', phase: 'login_entry_ready', ...bootstrapDOM() }],
    network: { external_blocked: 0, forbidden_mutations: 0, playback_attempts: 0, observer_errors: 0,
      overflow: 0, guard_errors: 0 } }] };
}

class FakeResponse extends EventEmitter {
  constructor(statusCode = 200, headers = {}, rawHeaders = null) {
    super(); this.statusCode = statusCode; this.headers = headers; this.complete = false; this.destroyed = false;
    this.rawHeaders = rawHeaders ?? Object.entries(headers).flat(); this.rawTrailers = [];
  }
  destroy() {
    if (this.destroyed) return;
    this.destroyed = true;
    if (this.destroyEvent) this.emit(this.destroyEvent);
  }
  body(value, complete = true) {
    const bytes = Buffer.isBuffer(value) ? value : Buffer.from(value);
    if (bytes.length) this.emit('data', bytes);
    this.complete = complete; this.emit('end');
  }
}
class FakeRequest extends EventEmitter {
  constructor(receive) { super(); this.receive = receive; this.destroyed = false; this.ended = false; }
  setTimeout(milliseconds, callback) { this.idleMilliseconds = milliseconds; this.idleTimeout = callback; return this; }
  end(body, callback) { this.ended = true; this.sentBody = Buffer.from(body ?? ''); callback?.(); }
  destroy() {
    if (this.destroyed) return;
    this.destroyed = true;
    if (this.destroyEvent) this.emit(this.destroyEvent);
  }
  respond(response) { requireThat(this.ended); this.receive(response); }
}
function transport() {
  return { calls: [], request(options, callback) {
    const request = new FakeRequest(callback); this.calls.push({ options, request }); return request;
  } };
}

function proxyState(mode = 'prelogin') {
  return { mode, seen: 0, admitted: 0, completed: 0, rejected: 0, failed: 0, active: 0,
    login: 0, capabilities: 0, logout: 0, preparation: 0, requestBytes: 0, responseBytes: 0, upgrade_rejections: 0 };
}
function ownedMovieSource() { return { item_id: PREPARATION_ITEM, source_id: 'synthetic-distinct-source', path: MOVIE_PATH }; }
function movieDTO() {
  return { Id: PREPARATION_ITEM, Name: 'M3e Client Movie', Type: 'Movie', Path: MOVIE_PATH,
    MediaSources: [{ Id: ownedMovieSource().source_id, ItemId: PREPARATION_ITEM, Path: MOVIE_PATH, Protocol: 'File', IsRemote: false }] };
}
function preparationScope() {
  return { mode: 'acceptance-preparation', phase: 'ui_movie', userId: A_ID, token: A_TOKEN,
    ordinary: true, loginCompleted: true, logoutStarted: false, source: ownedMovieSource() };
}
function preparationBody() {
  return Buffer.from(JSON.stringify({ UserId: A_ID, Id: PREPARATION_ITEM, MediaSourceId: ownedMovieSource().source_id,
    CurrentPlaySessionId: '', LiveStreamId: '', IsPlayback: false, DeviceProfile: { Name: 'synthetic' } }));
}
function preparationReply() {
  return { PlaySessionId: 'play_' + '1'.repeat(32), MediaSources: [movieDTO().MediaSources[0]] };
}
function requestHeaders(extra = []) { return ['Host', '127.0.0.1:18196', ...extra]; }
function rawHeaderValues(raw, name) {
  const values = [];
  for (let index = 0; index < raw.length; index += 2) if (raw[index].toLowerCase() === name.toLowerCase()) values.push(raw[index + 1]);
  return values;
}
const WS_KEY = 'dGhlIHNhbXBsZSBub25jZQ==';
const WS_ACCEPT = 's3pPLMBiTxaQ9kYGzzhZRbK+xOo=';
function websocketHeaders(extra = []) {
  return requestHeaders(['Connection', 'Upgrade', 'Upgrade', 'websocket', 'Sec-WebSocket-Version', '13',
    'Sec-WebSocket-Key', WS_KEY, 'Origin', ORIGIN, 'X-Emby-Token', A_TOKEN, ...extra]);
}
function websocketReply(extra = []) {
  return ['Connection', 'Upgrade', 'Upgrade', 'websocket', 'Sec-WebSocket-Accept', WS_ACCEPT, ...extra];
}
function replaceHeader(raw, name, value) {
  const result = [];
  for (let index = 0; index < raw.length; index += 2) if (raw[index].toLowerCase() !== name.toLowerCase()) result.push(raw[index], raw[index + 1]);
  if (value !== undefined) result.push(name, value);
  return result;
}
function wireFrame(value = Buffer.alloc(0), { masked = false, final = true, opcode = 1, rsv = 0, width,
  declaredLength = undefined } = {}) {
  const payload = Buffer.isBuffer(value) ? value : Buffer.from(value);
  const length = declaredLength ?? payload.length;
  const extended = width ?? (BigInt(length) >= 65536n ? 8 : Number(length) >= 126 ? 2 : 0);
  const offset = 2 + extended + (masked ? 4 : 0), result = Buffer.alloc(offset + payload.length);
  result[0] = (final ? 0x80 : 0) | rsv | opcode;
  result[1] = (masked ? 0x80 : 0) | (extended === 8 ? 127 : extended === 2 ? 126 : Number(length));
  if (extended === 8) result.writeBigUInt64BE(BigInt(length), 2);
  if (extended === 2) result.writeUInt16BE(Number(length), 2);
  const mask = Buffer.from([0x11, 0x22, 0x33, 0x44]);
  if (masked) mask.copy(result, 2 + extended);
  for (let index = 0; index < payload.length; index += 1) result[offset + index] = payload[index] ^ (masked ? mask[index % 4] : 0);
  return result;
}
function messageFrame(kind = 'UnimplementedClientOperation', options = {}) {
  return wireFrame(JSON.stringify({ MessageType: kind, Data: { synthetic: ASSET_PAYLOAD } }), options);
}
function messageBytesAtSize(size) {
  const overhead = Buffer.byteLength(JSON.stringify({ MessageType: 'Other', Data: '' }));
  requireThat(size >= overhead);
  return Buffer.from(JSON.stringify({ MessageType: 'Other', Data: 'x'.repeat(size - overhead) }));
}
function closePayload(code = 1000, reason = Buffer.alloc(0)) {
  const encoded = Buffer.alloc(2); encoded.writeUInt16BE(code); return Buffer.concat([encoded, Buffer.from(reason)]);
}
function connectInner(target = '/emby/socket', headers = websocketHeaders()) {
  let text = `GET ${target} HTTP/1.1\r\n`;
  for (let index = 0; index < headers.length; index += 2) text += `${headers[index]}: ${headers[index + 1]}\r\n`;
  return Buffer.from(text + '\r\n', 'latin1');
}
function websocketState() { return { seen: 0, admitted: 0, opened: 0, closed: 0, failed: 0, active: 0, control_attempts: 0 }; }
function deferred() {
  let resolve, reject;
  const promise = new Promise((accept, refuse) => { resolve = accept; reject = refuse; });
  return { promise, resolve, reject };
}
async function flushMicrotasks() { for (let index = 0; index < 32; index += 1) await Promise.resolve(); }
class FakeWebSocket extends EventEmitter {
  constructor() {
    super(); this.destroyed = false; this.paused = false; this.autoWrite = true; this.backpressure = false;
    this.setMaxListeners(128);
    this.writes = []; this.pendingWrites = []; this.inbound = []; this.pauseCount = 0; this.resumeCount = 0;
  }
  write(bytes, callback) {
    requireThat(!this.destroyed);
    this.pendingWrites.push({ bytes, callback });
    if (this.autoWrite) this.flushWrites();
    return !this.backpressure;
  }
  flushWrites(error = undefined) {
    for (const pending of this.pendingWrites.splice(0)) {
      if (!error && !this.destroyed) this.writes.push(Buffer.from(pending.bytes));
      pending.callback?.(error);
    }
  }
  end(value, callback) {
    this.ended = true; const completed = typeof value === 'function' ? value : callback;
    if (this.holdEnd) this.pendingEnd = completed; else completed?.(this.endError);
    return this;
  }
  destroy() { if (!this.destroyed) { this.destroyed = true; this.emit('close'); } }
  setTimeout(milliseconds, callback) { this.timeoutMilliseconds = milliseconds; this.timeoutCallback = callback; return this; }
  pause() { this.paused = true; this.pauseCount += 1; return this; }
  resume() {
    if (this.destroyed) return this;
    this.paused = false; this.resumeCount += 1;
    while (this.inbound.length && !this.paused && !this.destroyed) this.emit('data', this.inbound.shift());
    return this;
  }
  receive(bytes) { if (this.paused) this.inbound.push(Buffer.from(bytes)); else this.emit('data', bytes); }
}
class FakeProxyRequest extends EventEmitter {
  constructor(url, method, rawHeaders) {
    super(); this.url = url; this.method = method; this.rawHeaders = rawHeaders; this.rawTrailers = []; this.complete = false;
  }
  finish(body = Buffer.alloc(0), complete = true) {
    const bytes = Buffer.isBuffer(body) ? body : Buffer.from(body);
    if (bytes.length) this.emit('data', bytes);
    this.complete = complete; this.emit('end');
  }
}
class FakeProxyResponse extends EventEmitter {
  constructor() {
    super(); this.headersSent = false; this.destroyed = false; this.writableFinished = false;
    this.writes = []; this.entity = Buffer.alloc(0); this.holdCallback = false;
  }
  writeHead(status, headers) {
    this.headersSent = true; this.statusCode = status; this.rawHeaders = Array.isArray(headers) ? [...headers] : Object.entries(headers).flat();
    this.writes.push({ status, headers: [...this.rawHeaders] }); return this;
  }
  end(body, callback) {
    this.pendingEntity = Buffer.isBuffer(body) ? body : Buffer.from(body ?? '');
    this.pendingCallback = callback;
    if (!this.holdCallback) this.completeWrite();
    return this;
  }
  completeWrite(error, finished = !error) {
    this.writableFinished = finished;
    if (finished && !this.destroyed) this.entity = Buffer.from(this.pendingEntity ?? '');
    const callback = this.pendingCallback; this.pendingCallback = null; callback?.(error);
  }
  destroy() {
    if (this.destroyed) return;
    this.destroyed = true;
    if (this.destroyEvent) this.emit(this.destroyEvent);
  }
}

/** Run synthetic predicate and transport guards without starting the observer. */
export async function runCrossUserGuards(source) {
  const loaded = loadPureCore(source), api = loaded.core, cases = [];
  let checks = 0;
  const check = value => { checks += 1; requireThat(value); };
  const rejected = (action, message = 'cross_user_guard_rejected') => {
    let refused = false;
    try { action(); } catch (error) { refused = error?.message === message; }
    check(refused);
  };
  const rejectsRead = async promise => {
    let refused = false;
    try { await promise; } catch (error) { refused = error?.message === 'cross_user_read_failed'; }
    check(refused);
  };
  const bind = value => api.bindCrossUserCredentials(value.stateFile, value.aFile, value.bFile, value.fixture);
  const test = (name, action) => cases.push({ name, action });
  const startRead = (url = new URL(`/emby/Users/${A_ID}`, ORIGIN), token = A_TOKEN) => {
    const peer = transport(), promise = api.readBoundedJSON(url, token, peer);
    check(peer.calls.length === 1);
    return { peer, promise, request: peer.calls[0].request, options: peer.calls[0].options };
  };
  const startForward = (options = {}) => {
    const state = options.state ?? proxyState(options.mode), intents = options.intents ?? { login: false, logout: false, ownershipLost: false };
    const request = new FakeProxyRequest(options.url ?? `${ORIGIN}/web/index.html`, options.method ?? 'GET',
      options.rawHeaders ?? requestHeaders());
    const response = options.response ?? new FakeProxyResponse(), peer = options.peer ?? transport(), events = [];
    const policy = { state, intents: () => intents, onRequest: options.onRequest, onResponse: options.onResponse,
      onResponseHeaders: options.onResponseHeaders,
      onEvent: event => events.push(clone(event)) };
    const promise = api.forwardBrowserHTTP(request, response, policy, peer);
    return { state, intents, request, response, peer, events, promise };
  };
  const respondForward = (active, status = 200, rawHeaders = [], entity = Buffer.from('synthetic entity'), complete = true) => {
    check(active.peer.calls.length === 1);
    const incoming = new FakeResponse(status, {}, rawHeaders);
    active.peer.calls[0].request.respond(incoming); incoming.body(entity, complete); return incoming;
  };
  const forwardFinished = async (active, outcome = 'completed') => {
    await active.promise;
    check(active.state.active === 0 && active.events.length === 1 && active.events[0].outcome === outcome && loaded.timers.size === 0);
  };
  const rejectedFrame = (state, bytes, names = []) => {
    let failure;
    try { api.inspectWebSocketFrames(state, bytes, 0); } catch (error) { failure = error; }
    check(Boolean(failure) && (failure.message === 'cross_user_guard_rejected' || names.includes(failure.name)) && loaded.effects.length === 0);
  };
  const startSocket = (options = {}) => {
    const state = options.state ?? websocketState(), entry = {}, events = [], authorizations = [], permissions = [];
    const client = options.client ?? new FakeWebSocket(), peer = options.peer ?? new FakeWebSocket(), wire = options.transport ?? transport();
    const flags = options.flags ?? { ownershipLost: false, logoutInProgress: false };
    const request = { url: options.url ?? (options.connect ? '127.0.0.1:18196' : 'ws://127.0.0.1:18196/emby/socket'),
      method: options.connect ? 'CONNECT' : 'GET', rawHeaders: options.rawHeaders ?? (options.connect ? requestHeaders() : websocketHeaders()) };
    const policy = { mode: options.mode ?? 'acceptance', userId: options.userId ?? A_ID, state, entry,
      handshakeScope: options.scope, onLibraryChanged: options.onLibraryChanged,
      onLibraryChangedHandshake: options.onLibraryChangedHandshake,
      ownershipLost: () => flags.ownershipLost, logoutInProgress: () => flags.logoutInProgress,
      connectAllowed: async () => { permissions.push(true); await options.connectAllowed?.(); },
      authorize: async token => { authorizations.push(token); await options.authorize?.(token); },
      onEvent: value => events.push(clone(value)) };
    const forward = options.connect ? api.forwardWebSocketConnect : api.forwardBrowserWebSocket;
    const promise = forward(request, client, options.head ?? Buffer.alloc(0), policy, wire);
    return { state, entry, events, authorizations, permissions, client, peer, wire, flags, promise };
  };
  const upgradeSocket = async (active, head = Buffer.alloc(0), headers = websocketReply()) => {
    await flushMicrotasks(); check(active.wire.calls.length === 1);
    const response = new FakeResponse(101, {}, headers); response.statusMessage = 'Switching Protocols';
    active.wire.calls[0].request.emit('upgrade', response, active.peer, head);
    await flushMicrotasks(); return response;
  };
  const finishSocket = async (active, outcome = 'closed') => {
    await active.promise;
    check(active.state.active === 0 && active.events.length === 1 && active.entry.outcome === outcome && active.entry.closed === true);
    check(active.client.destroyed && active.peer.destroyed || active.client.destroyed && active.state.opened === 0);
    check(loaded.timers.size === 0);
  };

  test('arguments_accept_only_explicit_selection', () => {
    const result = api.parseCrossUserArguments(validArguments());
    check(result.source === `${ROOT}/source-attempt-9002` && result.output === `${ROOT}/client-cross-user-synthetic`);
    check(Object.keys(result).length === 8 && result.mode === 'acceptance' && result['candidate-sha256'] === syntheticHash('candidate'));
  });
  test('mode_is_optional_explicit_and_never_duplicate', () => {
    const legacy = api.parseCrossUserArguments(validArguments());
    check(same(legacy, api.parseCrossUserArguments([...validArguments(), '--mode', 'acceptance'])));
    check(api.parseCrossUserArguments([...validArguments(), '--mode', 'prelogin']).mode === 'prelogin');
    check(api.parseCrossUserArguments([...validArguments(), '--mode', 'acceptance-preparation',
      '--preparation-scope', PREPARATION_SCOPE]).mode === 'acceptance-preparation');
    for (const value of ['', 'diagnostic', 'Prelogin', 'prelogin/acceptance']) {
      rejected(() => api.parseCrossUserArguments([...validArguments(), '--mode', value]));
    }
    const duplicate = [...validArguments(), '--mode', 'prelogin']; duplicate[0] = '--mode'; duplicate[1] = 'acceptance';
    rejected(() => api.parseCrossUserArguments(duplicate));
    rejected(() => api.parseCrossUserArguments([...validArguments(), '--mode', 'prelogin', '--mode', 'acceptance']));
  });
  test('arguments_reject_duplicate_unknown_and_partial_flags', () => {
    const duplicate = validArguments(); duplicate[2] = duplicate[0];
    const unknown = validArguments(); unknown[0] = '--unknown';
    for (const args of [duplicate, unknown, validArguments().slice(0, -2), [...validArguments(), '--extra', 'value'], []]) {
      rejected(() => api.parseCrossUserArguments(args));
    }
  });
  test('arguments_reject_source_chain_and_output_escape', () => {
    for (const [index, value] of [[3, `${ROOT}/source-attempt-0`], [3, `${ROOT}/source-attempt-23/../24`],
      [3, '/tmp/source-attempt-23'], [9, `${ROOT}/nested/client-music-upgrade-chain-synthetic.json`],
      [9, `${ROOT}/client-music-upgrade-chain-synthetic.json/..`], [13, `${ROOT}/client-cross-user-synthetic/..`],
      [13, '/tmp/client-cross-user-synthetic']]) {
      const args = validArguments(); args[index] = value; rejected(() => api.parseCrossUserArguments(args));
    }
  });
  test('arguments_reject_malformed_sha_pins', () => {
    for (const index of [1, 5, 7, 11]) for (const invalid of ['', 'f'.repeat(63), 'F'.repeat(64), 'g'.repeat(64)]) {
      const args = validArguments(); args[index] = invalid; rejected(() => api.parseCrossUserArguments(args));
    }
  });
  test('credentials_bind_two_existing_distinct_users', () => {
    const result = bind(credentials());
    check(result.length === 2 && result[0].id === A_ID && result[1].id === B_ID);
    check(result[0].username === 'm3e-goby-av-client' && result[1].username === 'm3e-client-viewer');
    check(result[0].credentialsPath === `${ROOT}/goby-av-browser.json` && result[1].credentialsPath === `${ROOT}/browser.json`);
    check(result[0].credentialsSHA === syntheticHash('a-browser') && result[1].credentialsSHA === syntheticHash('b-browser'));
  });
  test('credentials_reject_b_identity_marker_origin_and_hash_confusion', () => {
    for (const mutate of [
      value => { value.stateFile.value.viewer_id = A_ID; },
      value => { value.bFile.value.viewer.userId = A_ID; },
      value => { value.bFile.value.viewer.username = 'm3e-goby-av-client'; },
      value => { value.bFile.value.marker = 'goby-m3e-goby-av-user-v1'; },
      value => { value.bFile.value.base_url = DIRECT; },
      value => { value.bFile.value.direct_url = ORIGIN; },
      value => { value.bFile.sha256 = syntheticHash('wrong-b-browser'); },
      value => { value.stateFile.value.browser_sha256 = syntheticHash('wrong-state-browser'); },
      value => { value.bFile.value.admin.username = 'unreceipted-administrator'; },
      value => { value.bFile.value.viewer.password = 'invalid'; },
    ]) { const value = credentials(); mutate(value); rejected(() => bind(value)); }
  });
  test('credentials_reject_state_and_av_alias_drift', () => {
    for (const mutate of [
      value => { value.stateFile.sha256 = syntheticHash('wrong-state'); },
      value => { value.aFile.sha256 = syntheticHash('wrong-a-browser'); },
      value => { value.stateFile.value.marker = 'unowned'; },
      value => { value.stateFile.value.server_id = 'e'.repeat(32); },
      value => { value.stateFile.value.admin_id = B_ID; },
      value => { value.stateFile.value.added_viewer.user_id = B_ID; },
      value => { value.aFile.value.account_marker = 'unowned'; },
      value => { value.aFile.value.viewer.userId = B_ID; },
      value => { value.aFile.value.viewer.serverId = 'e'.repeat(32); },
      value => { value.aFile.value.viewer.username = 'm3e-client-viewer'; },
    ]) { const value = credentials(); mutate(value); rejected(() => bind(value)); }
  });
  test('principals_must_be_the_named_enabled_ordinary_users', () => {
    for (const slot of ['A', 'B']) check(api.validateReadPrincipal(principal(slot), actor(slot), SERVER_ID) === true);
    for (const mutate of [value => { value.Id = B_ID; }, value => { value.Name = 'other'; },
      value => { value.ServerId = 'e'.repeat(32); }, value => { value.HasPassword = false; },
      value => { value.Policy.IsAdministrator = true; }, value => { value.Policy.IsDisabled = true; },
      value => { delete value.Policy.IsAdministrator; }, value => { value.Configuration = null; }]) {
      const value = principal(); mutate(value); rejected(() => api.validateReadPrincipal(value, actor(), SERVER_ID));
    }
  });
  test('browser_read_classification_omits_asset_payloads', () => {
    for (const [url, method, type] of [[`${ORIGIN}/web/index.html`, 'GET', 'document'],
      [`${ORIGIN}/emby/Users/${A_ID}/Items`, 'GET', 'xhr'], [`${ORIGIN}/web/client.js`, 'HEAD', 'script'],
      [`${ORIGIN}/emby/System/Info`, 'OPTIONS', 'fetch'], [`ws://127.0.0.1:18196/embywebsocket`, 'GET', 'websocket'],
      [`data:text/plain,${ASSET_PAYLOAD}`, 'GET', 'image'], [`blob:${ORIGIN}/${ASSET_PAYLOAD}`, 'GET', 'image']]) {
      const result = api.classifyBrowserRequest(url, method, type);
      check(result.allow === true && same(Object.keys(result).sort(), ['allow', 'kind']));
      check(!JSON.stringify(result).includes(ASSET_PAYLOAD));
    }
  });
  test('browser_allows_only_required_post_routes', () => {
    for (const route of ['/Users/AuthenticateByName', '/Sessions/Capabilities', '/Sessions/Capabilities/Full', '/Sessions/Logout']) {
      check(api.classifyBrowserRequest(`${ORIGIN}/emby${route}`, 'POST').allow === true);
      check(api.classifyBrowserRequest(`${ORIGIN}${route}/`, 'POST').allow === true);
      check(api.classifyBrowserRequest(`${ORIGIN}/emby${route}`, 'PUT').allow === false);
    }
  });
  test('browser_rejects_external_playback_and_state_mutations', () => {
    for (const raw of ['https://127.0.0.1:18196/web/', `${DIRECT}/web/`, 'http://example.invalid/',
      `http://user@127.0.0.1:18196/web/`, `${ORIGIN}/web/#fragment`, 'not a URL']) {
      check(api.classifyBrowserRequest(raw, 'GET').allow === false);
    }
    for (const route of ['/Videos/123/stream.mp4', '/Audio/123/stream.mp3', '/Items/123/PlaybackInfo', '/Sessions/Playing',
      '/Sessions/Playing/Progress', '/Sessions/Playing/Stopped']) {
      for (const method of ['GET', 'POST']) check(api.classifyBrowserRequest(`${ORIGIN}/emby${route}`, method).allow === false);
    }
    check(api.classifyBrowserRequest(`${ORIGIN}/arbitrary`, 'GET', 'media').allow === false);
    for (const [route, method] of [[`/Users/${A_ID}/Items/123/UserData`, 'POST'], [`/Users/${A_ID}/Configuration`, 'POST'],
      [`/Users/${A_ID}/Policy`, 'POST'], ['/Items/123', 'POST'], ['/Items/123', 'DELETE'],
      ['/Library/Refresh', 'POST'], ['/Sessions/Capabilities/Other', 'POST'], ['/Users/AuthenticateByName/Other', 'POST']]) {
      check(api.classifyBrowserRequest(`${ORIGIN}/emby${route}`, method).allow === false);
    }
  });
  test('prelogin_browser_guard_forbids_every_credential_or_session_submission', () => {
    for (const method of ['GET', 'HEAD', 'OPTIONS']) {
      check(api.classifyBrowserRequest(`${ORIGIN}/web/index.html`, method, 'document', 'prelogin').allow === true);
    }
    for (const route of ['/Users/AuthenticateByName', '/Sessions/Capabilities', '/Sessions/Capabilities/Full',
      '/Sessions/Logout', `/Users/${A_ID}/Configuration`, `/Users/${A_ID}/Items/123/UserData`]) {
      for (const method of ['POST', 'PUT', 'PATCH', 'DELETE']) {
        check(api.classifyBrowserRequest(`${ORIGIN}/emby${route}`, method, 'fetch', 'prelogin').allow === false);
      }
    }
    for (const raw of [`data:text/plain,${ASSET_PAYLOAD}`, `blob:${ORIGIN}/${ASSET_PAYLOAD}`]) {
      check(api.classifyBrowserRequest(raw, 'POST', 'fetch', 'prelogin').allow === false);
    }
    check(api.classifyBrowserRequest(`${ORIGIN}/emby/Audio/123/stream.mp3`, 'GET', 'fetch', 'prelogin').allow === false);
    check(api.classifyBrowserRequest(`${DIRECT}/web/index.html`, 'GET', 'document', 'prelogin').allow === false);
    check(api.classifyBrowserRequest(`${ORIGIN}/web/index.html`, 'GET', 'document', 'invalid').allow === false);
    check(api.classifyBrowserRequest(`${ORIGIN}/emby/Users/AuthenticateByName`, 'POST', 'fetch', 'acceptance').allow === true);
  });
  test('request_diagnostics_hash_only_paths_and_never_expose_queries_or_payloads', () => {
    const pathname = `/web/${ASSET_PAYLOAD}.js`;
    const first = api.browserRequestDiagnostic(`${ORIGIN}${pathname}?password=${A_PASSWORD}&token=${A_TOKEN}`, 'script');
    const second = api.browserRequestDiagnostic(`${ORIGIN}${pathname}?password=${B_PASSWORD}&token=${B_TOKEN}`, 'script');
    check(same(first, { resource_type: 'script', category: 'web_script', origin: 'target', path_sha256: hash(pathname),
      query_field_count: 2, query_fields_truncated: false }));
    check(same(first, second));
    for (const result of [first, api.browserRequestDiagnostic(`${ORIGIN}/web/index.html?${ASSET_PAYLOAD}=${A_TOKEN}`, A_PASSWORD),
      api.browserRequestDiagnostic(`data:text/html,<html>${A_PASSWORD}${A_TOKEN}</html>`, 'document'),
      api.browserRequestDiagnostic(`blob:${ORIGIN}/${ASSET_PAYLOAD}`, 'image'),
      api.browserRequestDiagnostic(`${ASSET_PAYLOAD} is not a URL`, ASSET_PAYLOAD)]) {
      check(![A_TOKEN, B_TOKEN, A_PASSWORD, B_PASSWORD, ASSET_PAYLOAD, '<html>', ORIGIN].some(secret => JSON.stringify(result).includes(secret)));
      check(same(Object.keys(result).sort(), ['category', 'origin', 'path_sha256', 'query_field_count', 'query_fields_truncated', 'resource_type']));
    }
    const largeQuery = Array.from({ length: 200 }, (_, index) => `field${index}=${A_TOKEN}`).join('&');
    const boundedResult = api.browserRequestDiagnostic(`${ORIGIN}/web/index.html?${largeQuery}`, 'document');
    check(boundedResult.query_field_count === 129 && boundedResult.query_fields_truncated === true);
    check(boundedResult.path_sha256 === hash('/web/index.html'));
  });
  test('request_diagnostics_return_only_fixed_endpoint_categories', () => {
    for (const [route, resource, category] of [['/web/index.html', 'document', 'web_document'],
      ['/web/service-worker.js', 'script', 'service_worker_script'], ['/web/client.css', 'stylesheet', 'web_stylesheet'],
      ['/web/font.woff2', 'font', 'web_font'], ['/web/icon.svg', 'image', 'web_image'], ['/web/config.json', 'fetch', 'web_json'],
      ['/emby/System/Info/Public', 'xhr', 'system_info_public'], ['/emby/Users/Public', 'xhr', 'public_users'],
      ['/emby/Users/AuthenticateByName', 'xhr', 'login'], ['/emby/Sessions/Logout', 'xhr', 'logout'],
      ['/emby/Unknown', ASSET_PAYLOAD, 'other_endpoint']]) {
      const result = api.browserRequestDiagnostic(`${ORIGIN}${route}`, resource);
      check(result.category === category && result.path_sha256 === hash(route) && result.origin === 'target');
      check(result.resource_type === (resource === ASSET_PAYLOAD ? 'other' : resource));
    }
    const external = api.browserRequestDiagnostic('http://example.invalid/web/index.html', 'document');
    check(external.origin === 'external' && !JSON.stringify(external).includes('example.invalid'));
  });
  test('safe_failures_keep_fixed_categories_without_messages_stacks_or_urls', () => {
    for (const [name, message, category] of [['TimeoutError', A_PASSWORD, 'timeout'],
      ['Error', `Service Worker ${A_TOKEN}`, 'service_worker'], ['Error', `Content Security Policy ${ASSET_PAYLOAD}`, 'content_security_policy'],
      ['Error', `net::ERR_FAILED ${ORIGIN}/secret?token=${A_TOKEN}`, 'network'],
      ['Error', `Target page closed ${A_PASSWORD}`, 'closed_target'], ['Error', `cross_user_guard_rejected ${ASSET_PAYLOAD}`, 'guard'],
      [ASSET_PAYLOAD, `${A_PASSWORD} ${A_TOKEN}`, 'other']]) {
      const result = api.safeBrowserFailure({ name, message, stack: `${A_PASSWORD}\n${A_TOKEN}`, cause: { secret: ASSET_PAYLOAD } }, 'navigation');
      check(same(result, { phase: 'navigation', name: name === ASSET_PAYLOAD ? 'other' : name, category,
        causes: [], causes_truncated: false }));
      check(![A_PASSWORD, A_TOKEN, ASSET_PAYLOAD, ORIGIN].some(secret => JSON.stringify(result).includes(secret)));
    }
    const unknown = api.safeBrowserFailure({ name: A_PASSWORD, message: ASSET_PAYLOAD }, A_TOKEN);
    check(unknown.name === 'other' && unknown.phase === 'unknown' && unknown.category === 'other');
  });
  test('aggregate_failures_bound_cause_and_message_inspection', () => {
    const result = api.safeBrowserFailure({ name: 'AggregateError', message: ASSET_PAYLOAD, stack: A_PASSWORD,
      errors: [{ name: 'TimeoutError', message: A_TOKEN }, { name: 'TypeError', message: `Failed to fetch ${B_TOKEN}` },
        { name: 'SyntaxError', message: ASSET_PAYLOAD }] }, 'login_entry_wait');
    check(same(result, { phase: 'login_entry_wait', name: 'AggregateError', category: 'other',
      causes: [{ name: 'TimeoutError', category: 'timeout' }, { name: 'TypeError', category: 'network' }], causes_truncated: true }));
    check(![A_PASSWORD, A_TOKEN, B_TOKEN, ASSET_PAYLOAD].some(secret => JSON.stringify(result).includes(secret)));
    const boundedMessage = api.safeBrowserFailure({ name: 'Error', message: 'x'.repeat(16384) + ' timeout' }, 'navigation');
    check(boundedMessage.category === 'other' && boundedMessage.causes.length === 0);
    const invalid = api.safeBrowserFailure({ name: { secret: A_TOKEN }, message: { html: ASSET_PAYLOAD }, errors: { message: A_PASSWORD } }, 'invalid');
    check(same(invalid, { phase: 'unknown', name: 'other', category: 'other', causes: [], causes_truncated: false }));
  });
  test('bootstrap_dom_sanitizer_drops_html_credentials_urls_and_unknown_fields', () => {
    const raw = bootstrapDOM();
    raw.html = `<input value="${A_PASSWORD}"><script>${A_TOKEN}</script>`;
    raw.title = ASSET_PAYLOAD; raw.url = `${ORIGIN}/web/index.html?token=${A_TOKEN}`;
    raw.visible.password_value = A_PASSWORD; raw.milestones.raw_text = ASSET_PAYLOAD;
    raw.scripts.source_urls = [`${ORIGIN}/web/client.js?token=${A_TOKEN}`]; raw.service_worker.script_url = ASSET_PAYLOAD;
    const result = api.sanitizeBootstrapDOM(raw);
    check(same(result, bootstrapDOM()));
    check(![A_PASSWORD, A_TOKEN, ASSET_PAYLOAD, ORIGIN, '<input', '<script'].some(secret => JSON.stringify(result).includes(secret)));
    const invalid = api.sanitizeBootstrapDOM(null);
    check(invalid.ready_state === 'unknown' && invalid.title_kind === 'other' && invalid.body_present === null && invalid.body_character_count === null);
    check(Object.values(invalid.visible).every(value => value === null) && Object.values(invalid.service_worker).every(value => value === null));
  });
  test('bootstrap_dom_sanitizer_enforces_count_boolean_and_enum_bounds', () => {
    const countPaths = [['body_character_count'], ...Object.keys(bootstrapDOM().visible).map(key => ['visible', key]),
      ...Object.keys(bootstrapDOM().scripts).map(key => ['scripts', key]), ['service_worker', 'registration_count']];
    const set = (value, keys, content) => { const target = keys.length === 1 ? value : value[keys[0]]; target[keys.at(-1)] = content; };
    const get = (value, keys) => keys.length === 1 ? value[keys[0]] : value[keys[0]][keys[1]];
    for (const keys of countPaths) for (const value of [-1, 0.5, 1000001, '1', false, null, NaN, Infinity]) {
      const raw = bootstrapDOM(); set(raw, keys, value); check(get(api.sanitizeBootstrapDOM(raw), keys) === null);
    }
    for (const value of [0, 1000000]) {
      const raw = bootstrapDOM(); raw.body_character_count = value; check(api.sanitizeBootstrapDOM(raw).body_character_count === value);
    }
    for (const keys of [['body_present'], ...Object.keys(bootstrapDOM().milestones).map(key => ['milestones', key]),
      ['service_worker', 'available'], ['service_worker', 'controller_present'], ['service_worker', 'query_failed']]) {
      const raw = bootstrapDOM(); set(raw, keys, 'true'); check(get(api.sanitizeBootstrapDOM(raw), keys) === null);
    }
    const raw = bootstrapDOM(); raw.ready_state = A_TOKEN; raw.title_kind = A_PASSWORD;
    const result = api.sanitizeBootstrapDOM(raw); check(result.ready_state === 'unknown' && result.title_kind === 'other');
  });
  test('prelogin_completion_requires_zero_credentials_api_and_logout_effects', () => {
    check(api.preloginDiagnosticsComplete(preloginReport()) === true);
    for (const mutate of [value => { value.mode = 'acceptance'; }, value => { value.client_acceptance = true; },
      value => { value.failure = 'synthetic_failure'; }, value => { value.accounts.push(clone(value.accounts[0])); },
      value => { value.api_reads.push({ status: 200 }); }, value => { value.accounts[0].prelogin.ready = false; },
      value => { value.accounts[0].prelogin.credentials_submitted = true; }, value => { value.accounts[0].closed = false; },
      value => { value.accounts[0].prelogin.expected_account_only = false; },
      value => { value.accounts[0].prelogin.service_workers = 'block'; },
      value => { value.accounts[0].prelogin.all_http_via_proxy = false; },
      value => { value.accounts[0].prelogin.client_acceptance = true; },
      value => { value.accounts[0].login.attempted = true; }, value => { value.accounts[0].login.credentials_filled = true; },
      value => { value.accounts[0].login.request_count = 1; }, value => { value.accounts[0].login.status = 200; },
      value => { value.accounts[0].logout.status = 204; }, value => { value.accounts[0].logout.attempted = true; },
      value => { value.accounts[0].own_item_reads = 1; }, value => { value.accounts[0].principal_confirmed = false; },
      value => { value.accounts[0].ordinary_authority_confirmed = false; },
      value => { value.accounts[0].foreign_read = { status: 403, error_code: 'access_denied', target_user_id: B_ID }; },
      value => { value.accounts[0].token_fingerprint = null; }, value => { value.accounts[0].token_fingerprint = hash(A_TOKEN); },
      value => { value.accounts[0].session_proof.outcome = 'all_observed_logout_tokens_rejected'; },
      value => { value.accounts[0].session_proof.entries.push({ verification: { status: 401 } }); },
      value => { value.accounts[0].bootstrap_diagnostics = []; },
      ...['forbidden_mutations', 'playback_attempts', 'observer_errors', 'overflow', 'guard_errors']
        .map(key => value => { value.accounts[0].network[key] = 1; })]) {
      const report = preloginReport(); mutate(report); check(api.preloginDiagnosticsComplete(report) === false);
    }
  });
  test('prelogin_diagnostics_can_never_satisfy_acceptance_even_with_forged_success_fields', () => {
    check(api.crossUserResult(preloginReport()) === false);
    const forged = completedReport(); forged.mode = 'prelogin'; forged.client_acceptance = true;
    check(api.crossUserResult(forged) === false && api.preloginDiagnosticsComplete(forged) === false);
    const missingMode = completedReport(); delete missingMode.mode; check(api.crossUserResult(missingMode) === false);
    check(api.preloginDiagnosticsComplete(completedReport()) === false);
  });
  test('authority_uses_only_observed_own_user_header_or_query_tokens', () => {
    const own = `${ORIGIN}/emby/Users/${A_ID}`;
    for (const [raw, headers] of [[own, { 'X-Emby-Token': A_TOKEN }], [own, { 'x-mediabrowser-token': A_TOKEN }],
      [own, { Authorization: `MediaBrowser Client="synthetic", Token="${A_TOKEN}"` }],
      [own, { 'X-Emby-Authorization': `Emby Token=${A_TOKEN}` }], [`${own}/Items?api_key=${A_TOKEN}`, {}],
      [`${ORIGIN}/emby/UserSettings/${A_ID}?X-Emby-Token=${A_TOKEN}`, { 'X-Emby-Token': A_TOKEN }]]) {
      const value = api.observedAuthority(raw, headers, A_ID);
      check(value.token === A_TOKEN && value.fingerprint === hash(A_TOKEN) && value.sources.length >= 1);
    }
    check(api.observedAuthority(own, {}, A_ID) === null);
    check(api.observedAuthority(own, { Authorization: 'Emby Client="Token=embedded-decoy"' }, A_ID) === null);
  });
  test('authority_rejects_conflicts_duplicates_and_malformed_auth', () => {
    const own = `${ORIGIN}/emby/Users/${A_ID}`;
    for (const [raw, headers] of [[own, { 'X-Emby-Token': A_TOKEN, 'x-emby-token': A_TOKEN }],
      [own, { 'X-Emby-Token': A_TOKEN, 'X-MediaBrowser-Token': B_TOKEN }],
      [`${own}?api_key=${B_TOKEN}`, { 'X-Emby-Token': A_TOKEN }],
      [`${own}?api_key=${A_TOKEN}&API_KEY=${A_TOKEN}`, {}],
      [own, { Authorization: `Emby Token="${A_TOKEN}", token="${A_TOKEN}"` }],
      [own, { Authorization: `Bearer ${A_TOKEN}` }], [own, { Authorization: `Emby Token="${A_TOKEN}` }],
      [own, { Authorization: `Emby Client="synthetic", Token="${A_TOKEN}",` }],
      [own, { Authorization: `Emby Client="Token=embedded", Token="${A_TOKEN}", Bad` }],
      [own, { 'X-Emby-Token': '' }], [own, { 'X-Emby-Token': 'token with spaces' }],
      [own, { 'X-Emby-Token': 'x'.repeat(4097) }]]) {
      rejected(() => api.observedAuthority(raw, headers, A_ID));
    }
  });
  test('authority_rejects_other_origin_user_and_authentication_routes', () => {
    for (const raw of [`${DIRECT}/emby/Users/${A_ID}`, `${ORIGIN}/emby/Users/${B_ID}`, `${ORIGIN}/emby/Users/${B_ID}/Items`,
      `${ORIGIN}/emby/Users/AuthenticateByName`, `${ORIGIN}/emby/Users/${A_ID}extra`,
      `http://user@127.0.0.1:18196/emby/Users/${A_ID}`, `${ORIGIN}/emby/Users/${A_ID}#fragment`]) {
      rejected(() => api.observedAuthority(raw, { 'X-Emby-Token': A_TOKEN }, A_ID));
    }
  });
  test('bare_item_authority_only_confirms_an_existing_own_token', () => {
    const item = `${ORIGIN}/emby/Items/${'1'.repeat(32)}`;
    for (const raw of [item, `${item}?UserId=${A_ID}`]) {
      rejected(() => api.observedAuthority(raw, { 'X-Emby-Token': A_TOKEN }, A_ID));
      const result = api.observedAuthority(raw, { 'X-Emby-Token': A_TOKEN }, A_ID, A_TOKEN);
      check(result.token === A_TOKEN && result.fingerprint === hash(A_TOKEN));
      rejected(() => api.observedAuthority(raw, { 'X-Emby-Token': B_TOKEN }, A_ID, A_TOKEN));
      check(api.observedAuthority(raw, {}, A_ID, A_TOKEN) === null);
    }
    for (const raw of [`${item}?UserId=${B_ID}`, `${item}?UserId=${A_ID}&userid=${A_ID}`,
      `${item}?UserId=${A_ID}&UserId=${B_ID}`, `${ORIGIN}/emby/Items/not-an-item-id`,
      `${DIRECT}/emby/Items/${'1'.repeat(32)}`]) {
      rejected(() => api.observedAuthority(raw, { 'X-Emby-Token': A_TOKEN }, A_ID, A_TOKEN));
    }
  });
  test('item_snapshot_requires_identity_and_preserves_complete_user_data', () => {
    const expected = { id: '1'.repeat(32), name: 'Synthetic Movie', type: 'Movie', path: '/synthetic/movie.mkv', parentId: '2'.repeat(32) };
    const item = { Id: expected.id, Name: expected.name, Type: expected.type, Path: expected.path, ParentId: expected.parentId,
      UserData: { Played: false, PlaybackPositionTicks: 42, PlayCount: 1, IsFavorite: true, LastPlayedDate: '2099-01-01T00:00:00Z' },
      Overview: ASSET_PAYLOAD };
    const projected = api.itemState(item, expected);
    check(same(projected, { id: expected.id, user_data: item.UserData }) && !JSON.stringify(projected).includes(ASSET_PAYLOAD));
    for (const key of ['Id', 'Name', 'Type', 'Path', 'ParentId']) {
      const value = clone(item); value[key] = 'wrong'; rejected(() => api.itemState(value, expected));
    }
    rejected(() => api.itemState({ ...item, UserData: null }, expected));
    const omitted = { ...item }; delete omitted.Path;
    check(api.itemState(omitted, { ...expected, pathOmitted: true }).id === expected.id);
    rejected(() => api.itemState(item, { ...expected, pathOmitted: true }));
  });
  test('state_comparison_detects_each_user_data_and_preference_drift', () => {
    const before = snapshot();
    check(same(api.compareCrossUserState(before, clone(before)),
      { items_unchanged: true, preferences_unchanged: true, configuration_unchanged: true, policy_unchanged: true }));
    for (const [field, mutate] of [['items_unchanged', value => { value.items[0].user_data.PlayCount += 1; }],
      ['items_unchanged', value => { value.items[0].user_data.Played = true; }],
      ['items_unchanged', value => { value.items[0].user_data.IsFavorite = true; }],
      ['items_unchanged', value => { value.items[0].user_data.PlaybackPositionTicks = 1; }],
      ['preferences_unchanged', value => { value.preferences.HomeSections.reverse(); }],
      ['configuration_unchanged', value => { value.configuration.OrderedViews.push('unexpected'); }],
      ['policy_unchanged', value => { value.policy.EnableAllFolders = false; }]]) {
      const after = clone(before); mutate(after);
      const result = api.compareCrossUserState(before, after);
      check(result[field] === false && Object.values(result).filter(value => value === false).length === 1);
    }
    rejected(() => api.compareCrossUserState(before, snapshot('B')));
    const short = clone(before); short.items.pop(); rejected(() => api.compareCrossUserState(before, short));
  });
  test('foreign_access_requires_exact_403_access_denied_shape', () => {
    const response = { status: 403, data: { ResponseStatus: { ErrorCode: 'access_denied', Message: 'Denied' } } };
    check(api.foreignAccessDenied(response) === true);
    for (const mutate of [value => { value.status = 200; }, value => { value.status = 401; }, value => { value.status = 404; },
      value => { value.data.ResponseStatus.ErrorCode = 'invalid_credentials'; }, value => { value.data.Items = []; },
      value => { value.data.ResponseStatus.extra = true; }, value => { delete value.data.ResponseStatus.Message; }]) {
      const value = clone(response); mutate(value); check(api.foreignAccessDenied(value) === false);
    }
  });
  test('ordinary_authority_requires_the_exact_administrator_query_denial', () => {
    const response = { status: 403, data: { ResponseStatus: { ErrorCode: 'administrator_required', Message: 'Denied' } } };
    check(api.administratorAccessDenied(response) === true);
    check(api.administratorAccessDenied({ status: 200, data: principal() }) === false);
    for (const mutate of [value => { value.status = 200; }, value => { value.status = 401; },
      value => { value.data.ResponseStatus.ErrorCode = 'access_denied'; }, value => { value.data.ResponseStatus.ErrorCode = 'invalid_credentials'; },
      value => { value.data.Items = []; }, value => { value.data.ResponseStatus.extra = true; }]) {
      const value = clone(response); mutate(value); check(api.administratorAccessDenied(value) === false);
    }
  });
  test('completed_result_requires_both_ui_read_and_foreign_checks', () => {
    check(api.crossUserResult(completedReport()) === true);
    for (const slot of [0, 1]) for (const mutate of [value => { value.login.status = 401; },
      value => { value.login.request_count = 0; }, value => { value.login.request_count = 2; },
      value => { value.ordinary_authority_confirmed = false; }, value => { value.proxy_login_status = 401; },
      value => { value.id = 'invalid'; }, value => { value.principal_confirmed = false; },
      value => { value.ui.outcome = 'not_run'; }, value => { value.own_item_reads = 0; }, value => { value.own_item_reads = 7; },
      value => { value.foreign_read.status = 200; }, value => { value.foreign_read.status = 401; },
      value => { value.foreign_read.error_code = 'invalid_credentials'; }, value => { value.foreign_read.target_user_id = value.id; }]) {
      const report = completedReport(); mutate(report.accounts[slot]); check(api.crossUserResult(report) === false);
    }
    const unchangedOnly = completedReport();
    for (const value of unchangedOnly.accounts) { value.own_item_reads = 0; value.foreign_read = null; value.logout = null; }
    check(api.crossUserResult(unchangedOnly) === false);
    const duplicate = completedReport(); duplicate.accounts[1].id = duplicate.accounts[0].id; check(api.crossUserResult(duplicate) === false);
  });
  test('completed_result_requires_own_logout_tokens_and_clean_terminal_state', () => {
    for (const slot of [0, 1]) for (const mutate of [value => { value.logout.status = 200; },
      value => { value.logout.login_view_visible = false; }, value => { value.closed = false; },
      value => { value.session_proof.outcome = 'logout_token_proof_incomplete'; },
      value => { value.session_proof.entries[0].verification.status = 200; },
      value => { value.session_proof.entries[0].ui_request.response_status = 401; },
      value => { value.session_proof.entries[0].token_fingerprint = syntheticHash('foreign-token'); },
      value => { value.session_proof.entries.push(clone(value.session_proof.entries[0])); },
      value => { value.token_fingerprint = 'invalid'; value.session_proof.entries[0].token_fingerprint = 'invalid'; },
      value => { delete value.comparison.items_unchanged; value.comparison.unrelated = true; },
      ...['items_unchanged', 'preferences_unchanged', 'configuration_unchanged', 'policy_unchanged'].map(key => value => { value.comparison[key] = false; }),
      ...['forbidden_mutations', 'playback_attempts', 'observer_errors', 'overflow', 'guard_errors'].map(key => value => { value.network[key] = 1; })]) {
      const report = completedReport(); mutate(report.accounts[slot]); check(api.crossUserResult(report) === false);
    }
    const sameToken = completedReport(); sameToken.accounts[1].token_fingerprint = sameToken.accounts[0].token_fingerprint;
    sameToken.accounts[1].session_proof.entries[0].token_fingerprint = sameToken.accounts[0].token_fingerprint;
    sameToken.accounts[1].proxy_logout.token_fingerprint = sameToken.accounts[0].token_fingerprint;
    sameToken.accounts[1].websocket.entries[0].token_fingerprint = sameToken.accounts[0].token_fingerprint;
    check(api.crossUserResult(sameToken) === false);
    const failed = completedReport(); failed.failure = 'synthetic_failure'; check(api.crossUserResult(failed) === false);
  });
  test('bounded_http_preserves_200_and_403_with_fixed_get_authority', async () => {
    for (const status of [200, 403]) {
      const active = startRead(), response = new FakeResponse(status), data = status === 200 ? principal()
        : { ResponseStatus: { ErrorCode: 'access_denied', Message: 'Denied' } };
      check(active.options.hostname === '127.0.0.1' && active.options.port === 18196 && active.options.method === 'GET');
      check(active.options.agent === false && active.options.maxHeaderSize === 16384 && active.request.maxHeadersCount === 128);
      check(active.options.path === `/emby/Users/${A_ID}` && active.options.headers['X-Emby-Token'] === A_TOKEN);
      check(active.options.headers.Host === '127.0.0.1:18196' && !Object.hasOwn(active.options.headers, 'Cookie'));
      active.request.respond(response); response.body(JSON.stringify(data));
      const result = await active.promise;
      check(result.status === status && same(result.data, data) && result.bytes === Buffer.byteLength(JSON.stringify(data)));
      check(active.request.destroyed && loaded.timers.size === 0);
    }
  });
  test('bounded_http_rejects_redirects_oversize_and_invalid_json', async () => {
    for (const [status, headers, body] of [[302, { location: 'http://example.invalid/' }, null],
      [200, { 'content-length': String(2 * 1024 * 1024 + 1) }, null], [200, { 'content-length': '-1' }, null],
      [200, {}, Buffer.alloc(2 * 1024 * 1024 + 1, 65)], [200, {}, Buffer.from([0xff])], [200, {}, '{invalid']]) {
      const active = startRead(), response = new FakeResponse(status, headers);
      active.request.respond(response);
      if (body !== null) response.body(body);
      await rejectsRead(active.promise);
      check(active.peer.calls.length === 1 && active.request.destroyed && loaded.timers.size === 0);
    }
  });
  test('bounded_http_rejects_incomplete_and_interrupted_responses', async () => {
    for (const event of ['aborted', 'error', 'close', 'incomplete_end']) {
      const active = startRead(), response = new FakeResponse(); active.request.respond(response);
      if (event === 'incomplete_end') response.body('{}', false);
      else if (event === 'error') response.emit('error', new Error('synthetic interruption'));
      else response.emit(event);
      await rejectsRead(active.promise);
      check(response.destroyed && active.request.destroyed && loaded.timers.size === 0);
    }
  });
  test('bounded_http_timeouts_and_upgrade_use_only_manual_mock_callbacks', async () => {
    for (const event of ['absolute_timeout', 'idle_timeout', 'request_error', 'upgrade']) {
      const active = startRead();
      if (event === 'absolute_timeout') loaded.fireTimers();
      else if (event === 'idle_timeout') { check(active.request.idleMilliseconds === 3000); active.request.idleTimeout(); }
      else if (event === 'request_error') active.request.emit('error', new Error('synthetic request failure'));
      else {
        const socket = { destroyed: false, destroy() { this.destroyed = true; } };
        active.request.emit('upgrade', {}, socket); check(socket.destroyed);
      }
      await rejectsRead(active.promise); check(active.request.destroyed && loaded.timers.size === 0);
    }
  });
  test('bounded_http_invalid_targets_never_reach_the_transport', () => {
    for (const raw of [`${DIRECT}/emby/Users/${A_ID}`, `https://127.0.0.1:18196/emby/Users/${A_ID}`,
      `http://user@127.0.0.1:18196/emby/Users/${A_ID}`, `${ORIGIN}/Users/${A_ID}`, `${ORIGIN}/emby/Users/${A_ID}#fragment`]) {
      const peer = transport(); rejected(() => api.readBoundedJSON(new URL(raw), A_TOKEN, peer)); check(peer.calls.length === 0);
    }
    for (const token of ['', 'token with spaces', 'x'.repeat(4097)]) {
      const peer = transport(); rejected(() => api.readBoundedJSON(new URL(`/emby/Users/${A_ID}`, ORIGIN), token, peer));
      check(peer.calls.length === 0);
    }
  });
  test('forwarding_execution_accepts_known_modes_and_rejects_unknown_modes_without_effects', async () => {
    for (const mode of ['prelogin', 'acceptance']) check(api.requireProxyExecutionMode(mode) === undefined);
    check(api.requireProxyExecutionMode('acceptance-preparation', PREPARATION_SCOPE) === undefined);
    for (const mode of [undefined, '', 'Prelogin', 'unsupported']) rejected(() => api.requireProxyExecutionMode(mode));
    const argv = validArguments(), input = {};
    for (let index = 0; index < argv.length; index += 2) input[argv[index].slice(2)] = argv[index + 1];
    for (const options of [{ ...input, mode: 'unsupported' }, { ...input, mode: '' }]) {
      let refused = false;
      try { await api.runCrossUserAcceptance(options); } catch (error) { refused = error?.message === 'cross_user_guard_rejected'; }
      check(refused && loaded.effects.length === 0 && loaded.timers.size === 0);
    }
  });
  test('proxy_request_plan_preserves_exact_entity_headers_and_query_in_memory', () => {
    const raw = `${ORIGIN}/web/client.js?token=${A_TOKEN}&version=1`;
    const plan = api.proxyRequestPlan(raw, 'GET', requestHeaders(['Connection', 'keep-alive', 'Accept-Encoding', 'gzip',
      'Cookie', `synthetic=${A_TOKEN}`, 'X-Emby-Token', A_TOKEN]), 'prelogin');
    check(plan.mode === 'prelogin' && plan.kind === 'read' && plan.method === 'GET' && plan.contentLength === 0);
    check(plan.path === `/web/client.js?token=${A_TOKEN}&version=1` && same(rawHeaderValues(plan.headers, 'Host'), ['127.0.0.1:18196']));
    check(same(rawHeaderValues(plan.headers, 'Connection'), ['close']) && same(rawHeaderValues(plan.headers, 'Accept-Encoding'), ['gzip']));
    check(same(rawHeaderValues(plan.headers, 'Cookie'), [`synthetic=${A_TOKEN}`]) && same(rawHeaderValues(plan.headers, 'X-Emby-Token'), [A_TOKEN]));
  });
  test('proxy_request_plan_rejects_target_method_host_and_tunnel_confusion', () => {
    for (const raw of [`${DIRECT}/web/`, `${ORIGIN}@example.invalid/web/`, 'http://localhost:18196/web/',
      `https://127.0.0.1:18196/web/`, `ws://127.0.0.1:18196/emby/socket`, '/web/index.html', `${ORIGIN}/web/#fragment`,
      `${ORIGIN}/web/../index.html`, `${ORIGIN}/web\\index.html`, 'http://user@127.0.0.1:18196/web/']) {
      rejected(() => api.proxyRequestPlan(raw, 'GET', requestHeaders(), 'prelogin'));
    }
    for (const method of ['CONNECT', 'POST', 'PUT', 'PATCH', 'DELETE']) {
      rejected(() => api.proxyRequestPlan(`${ORIGIN}/emby/Users/AuthenticateByName`, method, requestHeaders(), 'prelogin'));
    }
    for (const headers of [[], ['Host', 'localhost:18196'], ['Host', '127.0.0.1:18198'],
      requestHeaders(['Connection', 'Upgrade', 'Upgrade', 'websocket']), requestHeaders(['Upgrade', 'websocket']),
      requestHeaders(['Connection', 'keep-alive, upgrade'])]) {
      rejected(() => api.proxyRequestPlan(`${ORIGIN}/web/`, 'GET', headers, 'prelogin'));
    }
    rejected(() => api.proxyRequestPlan(`${ORIGIN}/emby/Audio/123/stream.mp3`, 'GET', requestHeaders(), 'prelogin'));
  });
  test('proxy_request_plan_rejects_ambiguous_or_unsupported_framing', () => {
    for (const extra of [['host', '127.0.0.1:18196'], ['Content-Length', '0', 'content-length', '0'],
      ['Transfer-Encoding', 'chunked'], ['Content-Length', '0', 'Transfer-Encoding', 'chunked'], ['Expect', '100-continue'],
      ['Trailer', 'X-Synthetic'], ['Proxy-Authorization', A_TOKEN], ['Connection', 'x-private', 'X-Private', A_TOKEN],
      ['Sec-Fetch-Dest', 'audio'], ['Sec-Fetch-Dest', 'Video'],
      ['Content-Length', '-1'], ['Content-Length', '01'], ['Content-Length', '1.0'], ['Content-Length', '1'],
      ['X-Header', 'line\r\nbreak'], ['X Header', 'value']]) {
      rejected(() => api.proxyRequestPlan(`${ORIGIN}/web/`, 'GET', requestHeaders(extra), 'prelogin'));
    }
    rejected(() => api.proxyRequestPlan(`${ORIGIN}/web/`, 'GET', ['Host'], 'prelogin'));
    rejected(() => api.proxyRequestPlan(`${ORIGIN}/emby/Users/AuthenticateByName`, 'POST', requestHeaders(), 'acceptance'));
    check(api.proxyRequestPlan(`${ORIGIN}/web/`, 'GET', requestHeaders(['Content-Length', '0']), 'prelogin').contentLength === 0);
  });
  test('proxy_request_header_and_declared_entity_limits_have_exact_boundaries', () => {
    const extra = Array.from({ length: 63 }, (_, index) => [`X-${index}`, 'v']).flat();
    check(api.proxyRequestPlan(`${ORIGIN}/web/`, 'GET', requestHeaders(extra), 'prelogin').kind === 'read');
    rejected(() => api.proxyRequestPlan(`${ORIGIN}/web/`, 'GET', requestHeaders([...extra, 'X-64', 'v']), 'prelogin'));
    rejected(() => api.proxyRequestPlan(`${ORIGIN}/web/`, 'GET', requestHeaders(['X-Large', 'x'.repeat(16384)]), 'prelogin'));
    const route = `${ORIGIN}/emby/Users/AuthenticateByName`;
    check(api.proxyRequestPlan(route, 'POST', requestHeaders(['Content-Length', '131072']), 'acceptance').contentLength === 131072);
    rejected(() => api.proxyRequestPlan(route, 'POST', requestHeaders(['Content-Length', '131073']), 'acceptance'));
  });
  test('proxy_response_plan_preserves_status_encoding_and_repeated_end_to_end_headers', () => {
    const plan = api.proxyResponsePlan(206, ['Content-Type', 'application/octet-stream', 'Content-Encoding', 'gzip',
      'Content-Length', '4', 'Content-Range', 'bytes 0-3/20', 'Set-Cookie', 'a=1', 'Set-Cookie', 'b=2', 'Connection', 'keep-alive'], 'GET');
    check(plan.status === 206 && plan.contentLength === 4 && plan.noBody === false);
    check(same(rawHeaderValues(plan.headers, 'Set-Cookie'), ['a=1', 'b=2']) && same(rawHeaderValues(plan.headers, 'Content-Encoding'), ['gzip']));
    check(same(rawHeaderValues(plan.headers, 'Content-Range'), ['bytes 0-3/20']) && same(rawHeaderValues(plan.headers, 'Connection'), ['close']));
    const chunked = api.proxyResponsePlan(200, ['Transfer-Encoding', 'chunked', 'Content-Type', 'text/javascript'], 'GET');
    check(chunked.contentLength === null && rawHeaderValues(chunked.headers, 'Transfer-Encoding').length === 0);
    for (const [status, method] of [[200, 'HEAD'], [204, 'GET'], [304, 'GET']]) {
      check(api.proxyResponsePlan(status, [], method).noBody === true);
    }
  });
  test('proxy_response_plan_rejects_upgrade_ambiguous_framing_and_oversize', () => {
    for (const status of [101, 199, 600, '200']) rejected(() => api.proxyResponsePlan(status, [], 'GET'));
    for (const headers of [['Content-Length', '2', 'content-length', '2'], ['Content-Length', '2', 'Transfer-Encoding', 'chunked'],
      ['Transfer-Encoding', 'gzip'], ['Upgrade', 'websocket'], ['Connection', 'upgrade'], ['Trailer', 'X-Secret'],
      ['Proxy-Authorization', A_TOKEN], ['Content-Length', '16777217'], ['Content-Length', '01']]) {
      rejected(() => api.proxyResponsePlan(200, headers, 'GET'));
    }
    check(api.proxyResponsePlan(200, ['Content-Length', '16777216'], 'GET').contentLength === 16777216);
  });
  test('proxy_admission_enforces_concurrency_total_budgets_and_ownership', () => {
    const plan = api.proxyRequestPlan(`${ORIGIN}/web/`, 'GET', requestHeaders(), 'prelogin');
    for (const [field, value] of [['active', 8], ['seen', 2001], ['requestBytes', 4194305], ['responseBytes', 134217729], ['mode', 'acceptance']]) {
      const state = proxyState(); state[field] = value; const before = clone(state);
      rejected(() => api.reserveProxyRequest(plan, state, { ownershipLost: false })); check(same(state, before));
    }
    const lost = proxyState(); rejected(() => api.reserveProxyRequest(plan, lost, { ownershipLost: true })); check(lost.admitted === 0);
    const last = proxyState(); last.active = 7; last.seen = 2000;
    api.reserveProxyRequest(plan, last, { ownershipLost: false }); check(last.active === 8 && last.admitted === 1);
  });
  test('proxy_write_intents_are_consumed_once_and_capabilities_are_bounded', () => {
    const plan = route => api.proxyRequestPlan(`${ORIGIN}/emby${route}`, 'POST', requestHeaders(['Content-Length', '0']), 'acceptance');
    const login = plan('/Users/AuthenticateByName'), logout = plan('/Sessions/Logout'), capabilities = plan('/Sessions/Capabilities');
    const state = proxyState('acceptance'), intents = { login: false, logout: false, ownershipLost: false };
    rejected(() => api.reserveProxyRequest(login, state, intents)); rejected(() => api.reserveProxyRequest(logout, state, intents));
    intents.login = true; api.reserveProxyRequest(login, state, intents); state.active -= 1;
    check(state.login === 1); rejected(() => api.reserveProxyRequest(login, state, intents));
    for (let index = 0; index < 16; index += 1) { api.reserveProxyRequest(capabilities, state, intents); state.active -= 1; }
    rejected(() => api.reserveProxyRequest(capabilities, state, intents)); check(state.capabilities === 16);
    intents.logout = true; api.reserveProxyRequest(logout, state, intents); state.active -= 1;
    check(state.logout === 1); rejected(() => api.reserveProxyRequest(logout, state, intents));
  });
  test('forwarding_preserves_binary_entities_and_status_without_decoding', async () => {
    const entity = Buffer.from([0x1f, 0x8b, 0x08, 0x00, 0xff, 0x00, 0xc3, 0x28]);
    for (const status of [200, 206, 403, 404, 500]) {
      const active = startForward({ url: `${ORIGIN}/web/client.js?token=${A_TOKEN}`, rawHeaders: requestHeaders(['Accept-Encoding', 'gzip']) });
      active.request.finish();
      respondForward(active, status, ['Content-Length', String(entity.length), 'Content-Encoding', 'gzip', 'Content-Type', 'application/octet-stream'], entity);
      await forwardFinished(active);
      const options = active.peer.calls[0].options;
      check(options.hostname === '127.0.0.1' && options.port === 18196 && options.agent === false && options.method === 'GET');
      check(options.path === `/web/client.js?token=${A_TOKEN}` && active.response.statusCode === status && active.response.entity.equals(entity));
      check(same(rawHeaderValues(active.response.rawHeaders, 'Content-Encoding'), ['gzip']) && active.state.responseBytes === entity.length);
      check(active.peer.calls[0].request.destroyed && active.state.completed === 1 && active.state.failed === 0);
    }
    const requestEntity = Buffer.from(JSON.stringify({ Username: 'synthetic', Password: A_PASSWORD }));
    const upload = startForward({ mode: 'acceptance', method: 'POST', url: `${ORIGIN}/emby/Users/AuthenticateByName`,
      rawHeaders: requestHeaders(['Content-Length', String(requestEntity.length), 'Content-Type', 'application/json']),
      intents: { login: true, logout: false, ownershipLost: false } });
    upload.request.finish(requestEntity);
    check(upload.peer.calls.length === 1 && upload.peer.calls[0].request.sentBody.equals(requestEntity));
    respondForward(upload, 200, ['Content-Length', '2'], Buffer.from('{}')); await forwardFinished(upload);
    check(upload.state.requestBytes === requestEntity.length && upload.events[0].request_bytes === requestEntity.length);
    check(!JSON.stringify(upload.events).includes(A_PASSWORD));
  });
  test('forwarding_redirect_is_returned_and_never_followed_by_the_proxy', async () => {
    const active = startForward(); active.request.finish();
    respondForward(active, 302, ['Location', `http://example.invalid/?token=${A_TOKEN}`, 'Content-Length', '0'], Buffer.alloc(0));
    await forwardFinished(active);
    check(active.peer.calls.length === 1 && active.response.statusCode === 302);
    check(same(rawHeaderValues(active.response.rawHeaders, 'Location'), [`http://example.invalid/?token=${A_TOKEN}`]));
    const next = startForward({ url: `http://example.invalid/?token=${A_TOKEN}` });
    await forwardFinished(next, 'rejected'); check(next.peer.calls.length === 0 && next.response.statusCode === 403);
  });
  test('forwarding_request_completeness_and_trailers_precede_upstream_creation', async () => {
    for (const change of ['short', 'long', 'incomplete', 'trailers', 'aborted', 'error']) {
      const active = startForward({ mode: 'acceptance', url: `${ORIGIN}/emby/Users/AuthenticateByName`, method: 'POST',
        rawHeaders: requestHeaders(['Content-Length', '2']), intents: { login: true, logout: false, ownershipLost: false } });
      if (change === 'trailers') active.request.rawTrailers = ['X-Synthetic', ASSET_PAYLOAD];
      if (change === 'aborted') active.request.emit('aborted');
      else if (change === 'error') active.request.emit('error', new Error(A_TOKEN));
      else active.request.finish(change === 'short' ? '{' : change === 'long' ? '{} ' : '{}', change !== 'incomplete');
      await forwardFinished(active, 'failed');
      check(active.peer.calls.length === 0 && active.state.login === 1 && active.state.completed === 0);
    }
  });
  test('forwarding_head_and_bodyless_statuses_never_forward_entity_bytes', async () => {
    for (const [method, status] of [['HEAD', 200], ['GET', 204], ['GET', 304]]) {
      const active = startForward({ method }); active.request.finish();
      respondForward(active, status, method === 'HEAD' ? ['Content-Length', '20000000'] : [], Buffer.alloc(0));
      await forwardFinished(active); check(active.response.entity.length === 0 && active.response.statusCode === status);
      const invalid = startForward({ method }); invalid.request.finish(); respondForward(invalid, status, [], Buffer.from('x'));
      await forwardFinished(invalid, 'failed'); check(invalid.state.completed === 0 && invalid.response.entity.length === 0);
    }
  });
  test('forwarding_rejects_upstream_truncation_trailers_and_bad_framing', async () => {
    for (const change of ['incomplete', 'short', 'long', 'trailers', 'framing']) {
      const active = startForward(); active.request.finish();
      const incoming = new FakeResponse(200, {}, change === 'framing' ? ['Content-Length', '2', 'Transfer-Encoding', 'chunked'] : ['Content-Length', '2']);
      if (change === 'trailers') incoming.rawTrailers = ['X-Synthetic', A_TOKEN];
      active.peer.calls[0].request.respond(incoming);
      if (change !== 'framing') incoming.body(change === 'short' ? '{' : change === 'long' ? '{} ' : '{}', change !== 'incomplete');
      await forwardFinished(active, 'failed'); check(active.state.completed === 0 && active.response.entity.length === 0);
    }
  });
  test('forwarding_enforces_actual_per_request_and_total_entity_budgets', async () => {
    const requestState = proxyState('acceptance'); requestState.requestBytes = 4194303;
    const upload = startForward({ state: requestState, url: `${ORIGIN}/emby/Users/AuthenticateByName`, method: 'POST',
      rawHeaders: requestHeaders(['Content-Length', '2']), intents: { login: true, logout: false, ownershipLost: false } });
    upload.request.finish('{}'); await forwardFinished(upload, 'failed'); check(upload.peer.calls.length === 0);
    for (const [initial, entity] of [[0, Buffer.alloc(16777217, 65)], [134217727, Buffer.from('xx')]]) {
      const state = proxyState(); state.responseBytes = initial;
      const active = startForward({ state }); active.request.finish(); respondForward(active, 200, [], entity);
      await forwardFinished(active, 'failed'); check(active.response.entity.length === 0 && active.state.completed === 0);
    }
  });
  test('forwarding_concurrent_limit_rejects_ninth_request_without_losing_slots', async () => {
    const state = proxyState(), active = Array.from({ length: 8 }, () => startForward({ state }));
    check(state.active === 8 && state.admitted === 8);
    const extra = startForward({ state }); await extra.promise;
    check(extra.peer.calls.length === 0 && extra.events[0].outcome === 'rejected' && state.active === 8);
    for (const pending of active) { pending.request.finish(); respondForward(pending, 200, ['Content-Length', '2'], Buffer.from('{}')); }
    await Promise.all(active.map(pending => pending.promise));
    check(state.active === 0 && state.completed === 8 && state.rejected === 1 && state.failed === 0 && loaded.timers.size === 0);
  });
  test('forwarding_aborts_timeouts_and_upgrade_destroy_owned_transports', async () => {
    for (const event of ['absolute_timeout', 'idle_timeout', 'upstream_error', 'upstream_aborted', 'upstream_close', 'downstream_close', 'upgrade']) {
      const active = startForward(); active.request.finish(); const outgoing = active.peer.calls[0].request;
      let incoming;
      if (event.startsWith('upstream_') && event !== 'upstream_error') {
        incoming = new FakeResponse(); outgoing.respond(incoming);
      }
      if (event === 'absolute_timeout') loaded.fireTimers();
      else if (event === 'idle_timeout') { check(outgoing.idleMilliseconds === 5000); outgoing.idleTimeout(); }
      else if (event === 'upstream_error') outgoing.emit('error', new Error(A_TOKEN));
      else if (event === 'upstream_aborted') incoming.emit('aborted');
      else if (event === 'upstream_close') incoming.emit('close');
      else if (event === 'downstream_close') { active.response.destroyed = true; active.response.emit('close'); }
      else { const socket = { destroyed: false, destroy() { this.destroyed = true; } }; outgoing.emit('upgrade', {}, socket); check(socket.destroyed); }
      await forwardFinished(active, 'failed'); check(outgoing.destroyed && active.state.completed === 0);
      const late = new FakeResponse(); outgoing.respond(late); check(late.destroyed && active.events.length === 1);
    }
  });
  test('forwarding_ownership_loss_prevents_admission_upstream_or_delivery', async () => {
    for (const stage of ['admission', 'request_end', 'response_end']) {
      const intents = { login: false, logout: false, ownershipLost: stage === 'admission' }, active = startForward({ intents });
      if (stage === 'request_end') { intents.ownershipLost = true; active.request.finish(); }
      if (stage === 'response_end') {
        active.request.finish(); const incoming = new FakeResponse(); active.peer.calls[0].request.respond(incoming);
        intents.ownershipLost = true; incoming.body('{}');
      }
      await forwardFinished(active, stage === 'admission' ? 'rejected' : 'failed');
      check(active.response.entity.length === 0 && active.state.completed === 0);
      if (stage !== 'response_end') check(active.peer.calls.length === 0);
    }
  });
  test('forwarding_failed_login_and_logout_cannot_retry_consumed_intents', async () => {
    for (const kind of ['login', 'logout']) {
      const state = proxyState('acceptance'); if (kind === 'logout') state.login = 1;
      const options = { state, method: 'POST', url: `${ORIGIN}/emby/${kind === 'login' ? 'Users/AuthenticateByName' : 'Sessions/Logout'}`,
        rawHeaders: requestHeaders(['Content-Length', '0']), intents: { login: true, logout: true, ownershipLost: false },
        peer: { calls: [], request() { throw new Error(A_TOKEN); } } };
      const first = startForward(options); first.request.finish(); await forwardFinished(first, 'failed');
      check(state[kind] === 1);
      const second = startForward(options); await forwardFinished(second, 'rejected');
      check(state[kind] === 1 && state.admitted === 1 && second.events[0].reason === 'admission_rejected');
    }
  });
  test('forwarding_write_callback_errors_and_close_races_never_count_completion', async () => {
    const response = new FakeProxyResponse(); response.holdCallback = true;
    const delayed = startForward({ response }), entity = Buffer.from(ASSET_PAYLOAD);
    delayed.request.finish(); respondForward(delayed, 200, ['Content-Length', String(entity.length)], entity);
    const pending = response.pendingEntity;
    check(pending.equals(entity) && delayed.state.active === 1 && delayed.state.completed === 0);
    response.completeWrite(); await forwardFinished(delayed);
    check(response.entity.equals(entity) && pending.every(value => value === 0));
    for (const failure of ['callback_error', 'not_finished', 'close_first', 'reentrant_destroy']) {
      const response = new FakeProxyResponse(); response.holdCallback = true;
      const active = startForward({ response }); active.request.finish();
      const incoming = new FakeResponse(200, {}, ['Content-Length', '2']);
      active.peer.calls[0].request.respond(incoming);
      if (failure === 'reentrant_destroy') {
        incoming.destroyEvent = 'close'; response.destroyEvent = 'close'; active.peer.calls[0].request.destroyEvent = 'error';
        incoming.emit('aborted');
      } else {
        incoming.body('{}'); check(active.state.active === 1 && active.state.completed === 0);
        if (failure === 'callback_error') response.completeWrite(new Error(A_TOKEN));
        else if (failure === 'not_finished') response.completeWrite(undefined, false);
        else { response.destroyed = true; response.emit('close'); response.completeWrite(); }
      }
      await forwardFinished(active, 'failed'); check(active.state.completed === 0 && active.state.failed === 1 && active.events.length === 1);
    }
  });
  test('forwarding_reports_only_safe_categories_counts_and_hashes', async () => {
    const active = startForward({ url: `${ORIGIN}/web/${ASSET_PAYLOAD}.js?password=${A_PASSWORD}&token=${A_TOKEN}`,
      rawHeaders: requestHeaders(['Cookie', `synthetic=${A_TOKEN}`, 'X-Emby-Token', A_TOKEN]) });
    active.request.finish(); respondForward(active, 200, ['Set-Cookie', `secret=${B_TOKEN}`], Buffer.from(ASSET_PAYLOAD));
    await forwardFinished(active);
    const event = active.events[0];
    check(same(Object.keys(event).sort(), ['category', 'kind', 'method', 'origin', 'outcome', 'path_sha256', 'query_field_count',
      'query_fields_truncated', 'reason', 'request_bytes', 'resource_type', 'response_bytes', 'status'].sort()));
    check(![A_TOKEN, B_TOKEN, A_PASSWORD, ASSET_PAYLOAD, ORIGIN].some(secret => JSON.stringify(active.events).includes(secret)));
  });
  test('prelogin_completion_requires_every_proxy_admission_to_complete', () => {
    check(api.preloginDiagnosticsComplete(preloginReport()) === true);
    for (const mutate of [value => { value.mode = 'acceptance'; }, value => { value.seen = 0; value.admitted = 0; value.completed = 0; },
      value => { value.seen += 1; }, value => { value.admitted += 1; }, value => { value.completed -= 1; },
      ...['active', 'rejected', 'failed', 'login', 'capabilities', 'logout', 'upgrade_rejections'].map(key => value => { value[key] = 1; })]) {
      const report = preloginReport(); mutate(report.accounts[0].proxy); check(api.preloginDiagnosticsComplete(report) === false);
    }
    for (const key of ['seen', 'admitted', 'opened', 'closed', 'failed', 'active', 'control_attempts']) {
      const report = preloginReport(); report.accounts[0].websocket[key] = 1;
      check(api.preloginDiagnosticsComplete(report) === false);
    }
    const entry = preloginReport(); entry.accounts[0].websocket.entries.push({ outcome: 'closed' });
    check(api.preloginDiagnosticsComplete(entry) === false);
  });
  test('frame_request_view_preserves_original_objects_and_excludes_worker_events', () => {
    const context = new EventEmitter(), pages = []; context.pages = () => pages;
    let errors = 0; const view = api.frameRequestContext(context, () => { errors += 1; });
    check(view.pages() === pages);
    const frame = { serviceWorker: () => null }, worker = { serviceWorker: () => ({}) };
    for (const event of ['request', 'response', 'requestfailed', 'requestfinished']) {
      const received = [], listener = value => received.push(value); view.on(event, listener);
      const original = event === 'response' ? { request: () => frame } : frame;
      const workerEvent = event === 'response' ? { request: () => worker } : worker;
      context.emit(event, workerEvent); context.emit(event, original); context.emit(event, original);
      check(received.length === 2 && received[0] === original && received[1] === original);
      view.off(event, listener); check(context.listenerCount(event) === 0);
    }
    pages.push({}); check(view.pages() === pages && view.pages().length === 1 && errors === 0);
  });
  test('frame_request_view_never_merges_distinct_requests_and_unsubscribes_exactly', () => {
    const context = new EventEmitter(); context.pages = () => [];
    let errors = 0; const view = api.frameRequestContext(context, () => { errors += 1; }), received = [], second = [];
    const listener = value => received.push(value), other = value => second.push(value);
    view.on('request', listener); view.on('request', other); view.on('requestfinished', listener);
    const first = { serviceWorker: () => null, url: () => `${ORIGIN}/emby/Sessions/Logout?api_key=${A_TOKEN}` };
    const next = { ...first };
    context.emit('request', first); context.emit('request', next);
    check(received.length === 2 && received[0] === first && received[1] === next && first !== next);
    rejected(() => view.on('request', listener));
    view.off('request', listener); context.emit('request', next); context.emit('requestfinished', first);
    check(received.length === 3 && received[2] === first && second.length === 3);
    view.off('request', other); view.off('requestfinished', listener); view.off('request', listener);
    check(context.listenerCount('request') === 0 && context.listenerCount('requestfinished') === 0 && errors === 0);
  });
  test('frame_request_view_fails_closed_on_missing_or_throwing_worker_identity', () => {
    const context = new EventEmitter(); context.pages = () => [];
    let errors = 0, delivered = 0; const view = api.frameRequestContext(context, () => { errors += 1; });
    view.on('request', () => { delivered += 1; }); view.on('response', () => { delivered += 1; });
    for (const request of [{}, { serviceWorker: () => undefined }, { serviceWorker: () => false },
      { serviceWorker() { throw new Error(A_TOKEN); } }]) context.emit('request', request);
    context.emit('response', { request() { throw new Error(A_PASSWORD); } });
    check(delivered === 0 && errors === 5);
    rejected(() => api.frameRequestContext({ pages: () => [] }, () => {}));
    rejected(() => api.frameRequestContext(context, undefined));
    rejected(() => view.on('unrelated', () => {}));
  });
  test('websocket_request_binds_real_key_target_alias_and_observed_authority', () => {
    for (const scheme of ['http', 'ws']) for (const route of ['/', '/emby', '/emby/', '/embywebsocket', '/emby/socket']) {
      const raw = `${scheme}://127.0.0.1:18196${route}?UserId=${A_ID}&api_key=${A_TOKEN}`;
      const plan = api.websocketRequestPlan(raw, 'GET', websocketHeaders(['Sec-WebSocket-Extensions', 'permessage-deflate',
        'Sec-WebSocket-Protocol', 'alpha, beta']), A_ID, 'acceptance');
      check(plan.path === `${route}?UserId=${A_ID}&api_key=${A_TOKEN}` && plan.key === WS_KEY);
      check(plan.authority.token === A_TOKEN && plan.authority.fingerprint === hash(A_TOKEN) && same(plan.protocols, ['alpha', 'beta']));
      check(same(rawHeaderValues(plan.headers, 'Sec-WebSocket-Key'), [WS_KEY]) && same(rawHeaderValues(plan.headers, 'Upgrade'), ['websocket']));
    }
    const queryOnly = api.websocketRequestPlan(`ws://127.0.0.1:18196/emby/socket?api_key=${A_TOKEN}`, 'GET',
      replaceHeader(websocketHeaders(), 'X-Emby-Token', undefined), A_ID, 'acceptance');
    check(queryOnly.authority.token === A_TOKEN && same(queryOnly.authority.sources, ['query:api_key']));
  });
  test('websocket_request_rejects_other_modes_targets_users_and_ambiguous_tokens', () => {
    const base = `ws://127.0.0.1:18196/emby/socket`;
    for (const raw of [`ws://127.0.0.1:18198/emby/socket`, 'wss://127.0.0.1:18196/emby/socket',
      'ws://user@127.0.0.1:18196/emby/socket', `${base}#fragment`, `${base}/../socket`, `${base}/extra`,
      `${base}?UserId=${B_ID}`, `${base}?UserId=${A_ID}&userid=${A_ID}`, `${base}?api_key=${B_TOKEN}`,
      `${base}?api_key=${A_TOKEN}&API_KEY=${A_TOKEN}`]) {
      rejected(() => api.websocketRequestPlan(raw, 'GET', websocketHeaders(), A_ID, 'acceptance'));
    }
    for (const mode of ['prelogin', 'invalid']) rejected(() => api.websocketRequestPlan(base, 'GET', websocketHeaders(), A_ID, mode));
    for (const method of ['POST', 'CONNECT', 'HEAD']) rejected(() => api.websocketRequestPlan(base, method, websocketHeaders(), A_ID, 'acceptance'));
    rejected(() => api.websocketRequestPlan(base, 'GET', websocketHeaders(), 'invalid-id', 'acceptance'));
    rejected(() => api.websocketRequestPlan(base, 'GET', replaceHeader(websocketHeaders(), 'X-Emby-Token', undefined), A_ID, 'acceptance'));
  });
  test('websocket_request_rejects_invalid_upgrade_key_version_and_http_framing', () => {
    const raw = `ws://127.0.0.1:18196/emby/socket`;
    for (const [name, value] of [['Host', 'localhost:18196'], ['Upgrade', 'h2c'], ['Connection', 'keep-alive'],
      ['Sec-WebSocket-Version', '12'], ['Sec-WebSocket-Key', 'invalid'], ['Sec-WebSocket-Key', Buffer.alloc(15).toString('base64')],
      ['X-Emby-Token', 'token with spaces'], ['Origin', undefined], ['Origin', DIRECT], ['Connection', 'close, upgrade'],
      ['Sec-WebSocket-Protocol', 'alpha, alpha'], ['Sec-WebSocket-Protocol', 'not a token'],
      ['Sec-WebSocket-Protocol', Array.from({ length: 17 }, (_, index) => `protocol${index}`).join(',')]]) {
      rejected(() => api.websocketRequestPlan(raw, 'GET', replaceHeader(websocketHeaders(), name, value), A_ID, 'acceptance'));
    }
    for (const extra of [['Upgrade', 'websocket'], ['Content-Length', '1'], ['Expect', '100-continue'],
      ['Transfer-Encoding', 'chunked'], ['Trailer', 'X-Data'], ['Proxy-Authorization', A_TOKEN], ['Connection', 'upgrade, x-private']]) {
      rejected(() => api.websocketRequestPlan(raw, 'GET', websocketHeaders(extra), A_ID, 'acceptance'));
    }
  });
  test('websocket_response_preserves_the_real_101_and_requires_the_original_key', () => {
    const plan = api.websocketRequestPlan(`ws://127.0.0.1:18196/emby/socket`, 'GET',
      websocketHeaders(['Sec-WebSocket-Protocol', 'alpha, beta']), A_ID, 'acceptance');
    const headers = websocketReply(['Sec-WebSocket-Protocol', 'beta', 'Set-Cookie', 'a=1', 'Set-Cookie', 'b=2']);
    check(same(api.websocketResponsePlan(101, headers, plan), headers));
    for (const status of [200, 301, 401, '101']) rejected(() => api.websocketResponsePlan(status, headers, plan));
    for (const bad of [replaceHeader(headers, 'Sec-WebSocket-Accept', 'wrong'), replaceHeader(headers, 'Connection', 'close'),
      replaceHeader(headers, 'Upgrade', 'h2c'), replaceHeader(headers, 'Sec-WebSocket-Protocol', 'unoffered'),
      [...headers, 'Sec-WebSocket-Accept', WS_ACCEPT], [...headers, 'Sec-WebSocket-Extensions', 'permessage-deflate'],
      [...headers, 'Content-Length', '0'], [...headers, 'Transfer-Encoding', 'chunked']]) {
      rejected(() => api.websocketResponsePlan(101, bad, plan));
    }
  });
  test('websocket_frames_preserve_masked_and_unmasked_original_wire_bytes', () => {
    for (const direction of ['client', 'server']) for (const opcode of [1, 2]) {
      const state = api.websocketFrameState(direction), input = messageFrame('UserDataChanged', { opcode, masked: direction === 'client' });
      const before = Buffer.from(input), output = api.inspectWebSocketFrames(state, input, 0);
      check(output.length === 1 && output[0].equals(before) && input.equals(before));
      check(state.frames === 1 && state.messages === 1 && state.wireBytes === input.length && state.messageKinds.user_data_changed === 1);
      check(state.pending.length === 0 && state.messageParts.length === 0 && state.messageBytes === 0);
    }
    const client = api.websocketFrameState('client'), outbound = messageFrame('Play', { masked: true });
    check(api.inspectWebSocketFrames(client, outbound, 0)[0].equals(outbound) && client.controlAttempt === false);
    rejected(() => api.websocketFrameState('unknown'));
  });
  test('websocket_parser_handles_arbitrary_transport_splits_without_early_frame_delivery', () => {
    for (const direction of ['client', 'server']) {
      const state = api.websocketFrameState(direction), input = messageFrame('Other', { masked: direction === 'client' });
      for (let index = 0; index < input.length - 1; index += 1) {
        check(api.inspectWebSocketFrames(state, input.subarray(index, index + 1), 0).length === 0);
      }
      const frames = api.inspectWebSocketFrames(state, input.subarray(input.length - 1), 0);
      check(frames.length === 1 && frames[0].equals(input) && state.frames === 1 && state.messages === 1);
    }
  });
  test('websocket_fragmented_json_and_interleaved_control_frames_keep_their_wire', () => {
    const state = api.websocketFrameState('server'), data = Buffer.from(JSON.stringify({ MessageType: 'Other', Data: ASSET_PAYLOAD }));
    const first = wireFrame(data.subarray(0, 12), { final: false }), ping = wireFrame(A_TOKEN, { opcode: 9 }),
      last = wireFrame(data.subarray(12), { opcode: 0 });
    check(api.inspectWebSocketFrames(state, first, 0)[0].equals(first) && state.messages === 0);
    check(api.inspectWebSocketFrames(state, ping, 0)[0].equals(ping) && state.fragmentOpcode === 1);
    check(api.inspectWebSocketFrames(state, last, 0)[0].equals(last) && state.frames === 3 && state.messages === 1 && state.fragmentOpcode === null);
  });
  test('websocket_server_control_messages_never_release_their_final_frame', () => {
    for (const kind of ['Play', 'Playstate', 'GeneralCommand', 'pLaY']) {
      const state = api.websocketFrameState('server');
      rejected(() => api.inspectWebSocketFrames(state, messageFrame(kind), 0), 'cross_user_server_control_message');
      check(state.controlAttempt === true && state.messages === 0);
      const fragmented = api.websocketFrameState('server'), data = Buffer.from(JSON.stringify({ MessageType: kind, Data: ASSET_PAYLOAD }));
      const prefix = wireFrame(data.subarray(0, 10), { final: false }), final = wireFrame(data.subarray(10), { opcode: 0 });
      check(api.inspectWebSocketFrames(fragmented, prefix, 0)[0].equals(prefix));
      check(api.inspectWebSocketFrames(fragmented, final.subarray(0, -1), 0).length === 0);
      rejected(() => api.inspectWebSocketFrames(fragmented, final.subarray(-1), 0), 'cross_user_server_control_message');
      check(fragmented.controlAttempt === true && fragmented.messageParts.length === 0 && fragmented.messageBytes === 0);
    }
  });
  test('websocket_message_type_duplicates_and_case_aliases_are_rejected', () => {
    for (const text of ['{"MessageType":"Play","MessageType":"Other"}',
      '{"MessageType":"Other","MessageType":"Play"}', '{"MessageType":"Play","messagetype":"Other"}',
      '{"Message\\u0054ype":"Play","MessageType":"Other"}', '{"MessageType":"Other","MESSAGETYPE":"Play"}']) {
      rejectedFrame(api.websocketFrameState('server'), wireFrame(text));
    }
    const nested = wireFrame('{"MessageType":"Other","Data":{"MessageType":"Play","text":"\\\"MessageType\\\":\\\"Play\\\""}}');
    const state = api.websocketFrameState('server'); check(api.inspectWebSocketFrames(state, nested, 0)[0].equals(nested) && !state.controlAttempt);
  });
  test('websocket_frames_reject_wrong_direction_rsv_opcodes_and_noncanonical_lengths', () => {
    rejectedFrame(api.websocketFrameState('client'), messageFrame('Other'));
    rejectedFrame(api.websocketFrameState('server'), messageFrame('Other', { masked: true }));
    for (const frame of [messageFrame('Other', { rsv: 0x40 }), wireFrame('{}', { opcode: 3 }),
      wireFrame('{}', { width: 2 }), wireFrame('{}', { width: 8 }), wireFrame('', { declaredLength: 65537n }),
      wireFrame('', { declaredLength: 1n << 63n }), wireFrame('x', { opcode: 9, final: false }),
      wireFrame(Buffer.alloc(126), { opcode: 9 }), wireFrame(Buffer.alloc(1), { opcode: 8 })]) {
      rejectedFrame(api.websocketFrameState('server'), frame);
    }
  });
  test('websocket_close_and_fragmentation_rules_reject_invalid_terminal_or_sequence_state', () => {
    const close = wireFrame(closePayload(1000, 'normal'), { opcode: 8 }), state = api.websocketFrameState('server');
    check(api.inspectWebSocketFrames(state, close, 0)[0].equals(close) && state.sawClose === true);
    rejectedFrame(state, wireFrame('', { opcode: 9 }));
    rejectedFrame(api.websocketFrameState('server'), wireFrame(closePayload(1005), { opcode: 8 }));
    rejectedFrame(api.websocketFrameState('server'), wireFrame(closePayload(1000, Buffer.from([0xff])), { opcode: 8 }), ['TypeError']);
    rejectedFrame(api.websocketFrameState('server'), wireFrame('{}', { opcode: 0 }));
    const fragmented = api.websocketFrameState('server'); api.inspectWebSocketFrames(fragmented, wireFrame('{', { final: false }), 0);
    rejectedFrame(fragmented, messageFrame('Other'));
  });
  test('websocket_json_requires_valid_utf8_and_a_bounded_string_message_type', () => {
    for (const text of ['null', '[]', '{}', '{"MessageType":1}', '{"MessageType":" "}',
      JSON.stringify({ MessageType: 'x'.repeat(129) }), JSON.stringify({ MessageType: '\u00e9'.repeat(65) }),
      JSON.stringify({ MessageType: 'Other\u0085' })]) {
      rejectedFrame(api.websocketFrameState('server'), wireFrame(text));
    }
    rejectedFrame(api.websocketFrameState('server'), wireFrame('{invalid'), ['SyntaxError']);
    rejectedFrame(api.websocketFrameState('server'), wireFrame(Buffer.from([0xff])), ['TypeError']);
    const boundary = messageFrame('\u00e9'.repeat(64)), state = api.websocketFrameState('server');
    check(api.inspectWebSocketFrames(state, boundary, 0)[0].equals(boundary) && state.messages === 1);
  });
  test('websocket_frame_and_fragmented_message_limits_are_independently_enforced', () => {
    const frame = wireFrame(messageBytesAtSize(65536)), state = api.websocketFrameState('server');
    check(api.inspectWebSocketFrames(state, frame, 0)[0].equals(frame) && state.messages === 1);
    rejectedFrame(api.websocketFrameState('server'), wireFrame(messageBytesAtSize(65537)));
    const fragmented = api.websocketFrameState('server');
    api.inspectWebSocketFrames(fragmented, wireFrame(Buffer.alloc(65536, 65), { final: false }), 0);
    rejectedFrame(fragmented, wireFrame('x', { opcode: 0 }));
  });
  test('websocket_limits_count_control_frames_total_frames_and_all_wire_bytes', () => {
    const ping = wireFrame('', { opcode: 9 }), rate = api.websocketFrameState('server');
    check(api.inspectWebSocketFrames(rate, Buffer.concat(Array.from({ length: 64 }, () => ping)), 0).length === 64);
    rejectedFrame(rate, ping);
    const reset = api.websocketFrameState('server'); api.inspectWebSocketFrames(reset, Buffer.concat(Array.from({ length: 64 }, () => ping)), 0);
    check(api.inspectWebSocketFrames(reset, ping, 1000).length === 1 && reset.frames === 65);
    const total = api.websocketFrameState('server');
    for (let index = 0; index < 512; index += 1) check(api.inspectWebSocketFrames(total, ping, index * 1000).length === 1);
    rejected(() => api.inspectWebSocketFrames(total, ping, 513000));
    const wire = api.websocketFrameState('server'), maximum = wireFrame(messageBytesAtSize(65536));
    for (let index = 0; index < 127; index += 1) api.inspectWebSocketFrames(wire, maximum, index * 1000);
    const remaining = 8 * 1024 * 1024 - wire.wireBytes, last = wireFrame(messageBytesAtSize(remaining - 4));
    check(last.length === remaining && api.inspectWebSocketFrames(wire, last, 127000).length === 1 && wire.wireBytes === 8 * 1024 * 1024);
    rejected(() => api.inspectWebSocketFrames(wire, Buffer.from([0]), 128000));
  });
  test('http_forwarding_calls_the_logout_authority_hook_before_opening_upstream', async () => {
    let called = 0;
    const active = startForward({ mode: 'acceptance', url: `${ORIGIN}/emby/Sessions/Logout`, method: 'POST',
      rawHeaders: requestHeaders(['Content-Length', '0', 'X-Emby-Token', A_TOKEN]),
      intents: { login: true, logout: true, ownershipLost: false }, state: { ...proxyState('acceptance'), login: 1 },
      onRequest(request, plan) { called += 1; check(plan.kind === 'logout' && request === active.request && active.peer.calls.length === 0); throw new Error(A_TOKEN); } });
    active.request.finish(); await forwardFinished(active, 'failed');
    check(called === 1 && active.peer.calls.length === 0 && active.state.logout === 1);
  });
  test('websocket_bridge_waits_for_own_user_authorization_before_any_upstream', async () => {
    const authorization = deferred(), active = startSocket({ authorize: () => authorization.promise });
    await flushMicrotasks(); check(active.authorizations.length === 1 && active.authorizations[0] === A_TOKEN);
    check(active.client.paused && active.client.pauseCount >= 1 && active.wire.calls.length === 0 && active.state.active === 1);
    authorization.resolve(); await upgradeSocket(active);
    check(active.entry.handshake_delivered === true && active.state.opened === 1);
    check(active.client.writes[0].toString('latin1') === 'HTTP/1.1 101 Switching Protocols\r\n' +
      'Connection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Accept: ' + WS_ACCEPT + '\r\n\r\n');
    const options = active.wire.calls[0].options;
    check(options.hostname === '127.0.0.1' && options.port === 18196 && options.method === 'GET' && options.agent === false);
    check(active.client.timeoutMilliseconds === 0 && active.peer.timeoutMilliseconds === 0);
    active.flags.logoutInProgress = true; active.peer.emit('end'); await finishSocket(active);
    check(active.state.closed === 1 && active.state.failed === 0);
  });
  test('websocket_bridge_retains_frames_during_authorization_and_pending_101_write', async () => {
    const authorization = deferred(), client = new FakeWebSocket(); client.autoWrite = false;
    const active = startSocket({ client, authorize: () => authorization.promise }), clientFrame = messageFrame('Other', { masked: true }),
      serverFrame = messageFrame('UserDataChanged');
    client.receive(clientFrame); check(client.inbound.length === 1 && active.wire.calls.length === 0);
    authorization.resolve(); await upgradeSocket(active);
    check(active.state.opened === 0 && client.paused && active.peer.paused);
    active.peer.receive(serverFrame); check(active.peer.inbound.length === 1 && client.writes.length === 0);
    client.autoWrite = true; client.flushWrites(); await flushMicrotasks();
    check(active.state.opened === 1 && !client.paused && !active.peer.paused);
    check(active.peer.writes.length === 1 && active.peer.writes[0].equals(clientFrame));
    check(client.writes.length === 2 && client.writes[1].equals(serverFrame));
    active.flags.logoutInProgress = true; active.peer.emit('end'); await finishSocket(active);
  });
  test('websocket_bridge_checks_both_upgrade_heads_before_sending_a_101', async () => {
    const clientHead = messageFrame('Other', { masked: true }), serverHead = messageFrame('UserDataChanged');
    const normal = startSocket({ head: clientHead }); await upgradeSocket(normal, serverHead);
    check(normal.peer.writes.length === 1 && normal.peer.writes[0].equals(clientHead));
    check(normal.client.writes.length === 2 && normal.client.writes[1].equals(serverHead));
    normal.flags.logoutInProgress = true; normal.peer.emit('end'); await finishSocket(normal);
    for (const options of [{ serverHead: messageFrame('Play') }, { head: messageFrame('Other') }]) {
      const active = startSocket({ head: options.head }); await upgradeSocket(active, options.serverHead);
      await finishSocket(active, 'failed');
      check(active.client.writes.length === 0 && active.state.opened === 0 && active.peer.writes.length === 0);
      if (options.serverHead) check(active.state.control_attempts === 1 && active.entry.reason === 'server_control_message');
    }
  });
  test('websocket_bridge_relays_original_data_and_control_but_blocks_server_commands', async () => {
    const active = startSocket(); await upgradeSocket(active);
    const clientFrame = messageFrame('Other', { masked: true }), ping = wireFrame('ping', { opcode: 9 });
    active.client.receive(clientFrame); active.peer.receive(ping);
    check(active.peer.writes[0].equals(clientFrame) && active.client.writes[1].equals(ping));
    const command = Buffer.from(JSON.stringify({ MessageType: 'GeneralCommand', Data: ASSET_PAYLOAD }));
    const prefix = wireFrame(command.subarray(0, 8), { final: false }), final = wireFrame(command.subarray(8), { opcode: 0 });
    active.peer.receive(prefix); const delivered = active.client.writes.length;
    active.peer.receive(final); await finishSocket(active, 'failed');
    check(active.client.writes.length === delivered && active.state.control_attempts === 1 && active.entry.reason === 'server_control_message');
    check(!JSON.stringify(active.events).includes(ASSET_PAYLOAD));
  });
  test('websocket_bridge_rejection_status_and_entity_come_from_the_real_upstream', async () => {
    const active = startSocket(); await flushMicrotasks(); check(active.wire.calls.length === 1);
    const entity = Buffer.from('synthetic rejected handshake'), response = new FakeResponse(401, {},
      ['Content-Type', 'text/plain', 'Content-Length', String(entity.length)]);
    response.statusMessage = 'Unauthorized'; active.wire.calls[0].request.respond(response); response.body(entity);
    await finishSocket(active, 'failed');
    const received = Buffer.concat(active.client.writes), separator = received.indexOf('\r\n\r\n');
    check(received.subarray(0, separator).toString('latin1').startsWith('HTTP/1.1 401 Unauthorized\r\n'));
    check(received.subarray(separator + 4).equals(entity) && active.state.opened === 0 && active.entry.upstream_status === 401);
    check(active.entry.reason === 'handshake_not_accepted' && active.wire.calls.length === 1);
    response.emit('data', Buffer.from(A_TOKEN)); response.emit('end'); check(active.events.length === 1);
  });
  test('websocket_bridge_failed_or_timed_out_authorization_never_opens_upstream', async () => {
    const rejected = startSocket({ authorize: async () => { throw new Error(A_TOKEN); } });
    await finishSocket(rejected, 'failed'); check(rejected.wire.calls.length === 0 && rejected.state.admitted === 1);
    const authorization = deferred(), timed = startSocket({ authorize: () => authorization.promise });
    let resolved = false; timed.promise.then(() => { resolved = true; });
    await flushMicrotasks(); loaded.fireTimers(10000); await flushMicrotasks();
    check(timed.client.destroyed && timed.state.failed === 1 && timed.state.active === 0 && !resolved && timed.wire.calls.length === 0);
    authorization.resolve(); await finishSocket(timed, 'failed'); check(timed.wire.calls.length === 0 && timed.events.length === 1);
  });
  test('websocket_bridge_handshake_limits_count_pending_and_failed_attempts', async () => {
    const authorization = deferred(), state = websocketState(), first = startSocket({ state, authorize: () => authorization.promise });
    const concurrent = startSocket({ state }); await concurrent.promise;
    check(state.active === 1 && state.seen === 2 && state.admitted === 1 && concurrent.wire.calls.length === 0);
    authorization.reject(new Error(A_TOKEN)); await finishSocket(first, 'failed');
    const third = startSocket({ state }); await finishSocket(third, 'failed'); check(state.seen === 3 && third.wire.calls.length === 0);
    const sequential = websocketState();
    for (let index = 0; index < 2; index += 1) {
      const active = startSocket({ state: sequential }); await upgradeSocket(active);
      active.flags.logoutInProgress = true; active.peer.emit('end'); await finishSocket(active);
    }
    check(sequential.opened === 2 && sequential.closed === 2 && sequential.failed === 0);
  });
  test('websocket_bridge_distinguishes_expected_close_from_errors_and_timeouts', async () => {
    const normal = startSocket(); await upgradeSocket(normal);
    const close = wireFrame(closePayload(1000), { opcode: 8 }); normal.peer.receive(close); normal.peer.emit('end');
    await finishSocket(normal); check(normal.client.writes[1].equals(close));
    for (const event of ['unexpected_end', 'idle_timeout', 'lifetime_timeout', 'ownership_lost', 'client_error',
      'close_flush_error', 'close_flush_timeout']) {
      const active = startSocket(); await upgradeSocket(active);
      if (event === 'unexpected_end') active.peer.emit('end');
      else if (event === 'idle_timeout') loaded.fireTimers(65000);
      else if (event === 'lifetime_timeout') loaded.fireTimers(240000);
      else if (event === 'client_error') active.client.emit('error', new Error(A_TOKEN));
      else if (event === 'close_flush_error') {
        active.flags.logoutInProgress = true; active.client.endError = new Error(A_TOKEN); active.peer.emit('end');
      } else if (event === 'close_flush_timeout') {
        active.flags.logoutInProgress = true; active.client.holdEnd = true; active.peer.emit('end'); loaded.fireTimers(5000);
      }
      else { active.flags.ownershipLost = true; active.peer.receive(wireFrame('', { opcode: 9 })); }
      await finishSocket(active, 'failed'); check(active.state.failed === 1 && active.state.closed === 0);
    }
  });
  test('websocket_bridge_write_errors_and_backpressure_cannot_resume_after_failure', async () => {
    const client = new FakeWebSocket(); client.autoWrite = false;
    const handshake = startSocket({ client }); await upgradeSocket(handshake);
    client.flushWrites(new Error(A_TOKEN)); await finishSocket(handshake, 'failed');
    check(handshake.state.opened === 0 && handshake.entry.handshake_delivered === false && client.writes.length === 0);
    const active = startSocket(); await upgradeSocket(active);
    active.client.autoWrite = false; active.client.backpressure = true;
    active.peer.receive(messageFrame('Other'));
    check(active.peer.paused && active.client.pendingWrites.length === 1);
    const resumes = active.peer.resumeCount;
    active.client.flushWrites(new Error(A_TOKEN)); await finishSocket(active, 'failed');
    active.client.emit('drain'); check(active.peer.resumeCount === resumes && active.state.failed === 1);
    const overflow = startSocket(); await upgradeSocket(overflow);
    overflow.client.autoWrite = false; overflow.client.backpressure = true;
    const large = wireFrame(messageBytesAtSize(65536));
    overflow.peer.receive(Buffer.concat(Array.from({ length: 20 }, () => large)));
    await finishSocket(overflow, 'failed'); check(overflow.entry.reason === 'write_queue_exceeded');
    overflow.client.flushWrites(); overflow.client.emit('drain'); check(overflow.events.length === 1);
  });
  test('acceptance_requires_matching_physical_logout_frame_proof_and_closed_websockets', () => {
    check(api.crossUserResult(completedReport()) === true);
    for (const mutate of [value => { value.proxy_logout.completed = false; }, value => { value.proxy_logout.status = 200; },
      value => { value.proxy_logout.token_fingerprint = syntheticHash('wrong-physical-token'); },
      value => { value.websocket.entries[0].token_fingerprint = syntheticHash('wrong-socket-token'); },
      value => { value.websocket.entries[0].handshake_delivered = false; }, value => { value.websocket.entries[0].upstream_status = 200; },
      value => { value.websocket.entries[0].outcome = 'failed'; }, value => { value.websocket.entries[0].server_control_attempted = true; },
      value => { value.websocket.seen = 0; value.websocket.entries = []; }, value => { value.websocket.admitted = 0; },
      value => { value.websocket.opened = 0; }, value => { value.websocket.closed = 0; }, value => { value.websocket.active = 1; },
      value => { value.websocket.failed = 1; }, value => { value.websocket.control_attempts = 1; },
      value => { value.proxy.completed -= 1; }, value => { value.proxy.login = 0; }, value => { value.proxy.logout = 0; },
      value => { value.proxy.failed = 1; }, value => { value.proxy.upgrade_rejections = 1; }]) {
      const report = completedReport(); mutate(report.accounts[0]); check(api.crossUserResult(report) === false);
    }
    const reconnected = completedReport(), socket = reconnected.accounts[0].websocket;
    socket.seen = socket.admitted = socket.opened = socket.closed = 2; socket.entries.push(clone(socket.entries[0]));
    check(api.crossUserResult(reconnected) === true);
  });
  test('connect_outer_plan_accepts_only_the_fixed_authority_and_readonly_ws_mode', () => {
    check(api.websocketConnectPlan('127.0.0.1:18196', 'CONNECT', requestHeaders(), 'acceptance') === undefined);
    for (const authority of ['localhost:18196', '127.0.0.1:18198', '127.0.0.1:443', '[::1]:18196',
      'http://127.0.0.1:18196', '127.0.0.1:18196/path', 'user@127.0.0.1:18196']) {
      rejected(() => api.websocketConnectPlan(authority, 'CONNECT', requestHeaders(), 'acceptance'));
    }
    for (const mode of ['prelogin', 'unsupported']) rejected(() => api.websocketConnectPlan('127.0.0.1:18196', 'CONNECT', requestHeaders(), mode));
    for (const method of ['GET', 'POST']) rejected(() => api.websocketConnectPlan('127.0.0.1:18196', method, requestHeaders(), 'acceptance'));
    for (const headers of [['Host', '127.0.0.1:18198'], requestHeaders(['Host', '127.0.0.1:18196']),
      requestHeaders(['Content-Length', '1']), requestHeaders(['Transfer-Encoding', 'chunked']), requestHeaders(['Expect', '100-continue']),
      requestHeaders(['Proxy-Authorization', A_TOKEN]), requestHeaders(['Upgrade', 'websocket'])]) {
      rejected(() => api.websocketConnectPlan('127.0.0.1:18196', 'CONNECT', headers, 'acceptance'));
    }
  });
  test('connect_inner_parser_separates_one_bounded_header_from_original_frame_head', () => {
    const header = connectInner(`/emby/socket?UserId=${A_ID}&api_key=${A_TOKEN}`);
    for (const length of [0, 1, 3, 4, header.length - 1]) check(api.websocketConnectInner(header.subarray(0, length), A_ID) === null);
    const extra = websocketHeaders(['X-Padding', '']), empty = connectInner('/emby/socket', extra);
    const exact = connectInner('/emby/socket', replaceHeader(extra, 'X-Padding', 'x'.repeat(16384 - empty.length)));
    check(exact.length === 16384);
    const frame = wireFrame(messageBytesAtSize(65536), { masked: true }), bytes = Buffer.concat([exact, frame]), original = Buffer.from(bytes);
    const parsed = api.websocketConnectInner(bytes, A_ID);
    check(parsed.request.url === `${ORIGIN}/emby/socket` && parsed.request.method === 'GET' && parsed.head.equals(frame));
    check(same(rawHeaderValues(parsed.request.rawHeaders, 'X-Emby-Token'), [A_TOKEN]));
    parsed.head.fill(0); check(bytes.equals(original));
    const noBoundary = Buffer.concat([Buffer.from('GET '), Buffer.alloc(16380, 65)]);
    check(api.websocketConnectInner(noBoundary, A_ID) === null);
    rejected(() => api.websocketConnectInner(Buffer.concat([noBoundary, Buffer.from('x')]), A_ID));
    const tooLargeHeader = connectInner('/emby/socket', replaceHeader(extra, 'X-Padding', 'x'.repeat(16385 - empty.length)));
    rejected(() => api.websocketConnectInner(tooLargeHeader, A_ID));
    rejected(() => api.websocketConnectInner(Buffer.alloc(16384 + 1048576 + 1), A_ID));
  });
  test('connect_waits_for_login_permission_and_200_delivery_before_inner_authorization', async () => {
    const permission = deferred(), client = new FakeWebSocket(); client.autoWrite = false;
    const inner = connectInner(), active = startSocket({ connect: true, client, head: inner.subarray(0, 20),
      connectAllowed: () => permission.promise });
    await flushMicrotasks(); client.emit('data', inner.subarray(20));
    check(active.permissions.length === 1 && client.paused && client.writes.length === 0 && active.wire.calls.length === 0);
    permission.resolve(); await flushMicrotasks();
    check(client.pendingWrites.length === 1 && active.entry.proxy_connect_status === null && active.authorizations.length === 0);
    client.autoWrite = true; client.flushWrites(); await flushMicrotasks();
    check(client.writes[0].toString('latin1') === 'HTTP/1.1 200 Connection Established\r\n\r\n');
    check(active.entry.proxy_connect_status === 200 && active.authorizations.length === 1 && active.authorizations[0] === A_TOKEN);
    check(active.state.seen === 1 && active.state.admitted === 1 && active.state.active === 1 && active.state.opened === 0);
    await upgradeSocket(active); check(client.writes[1].toString('latin1').startsWith('HTTP/1.1 101 Switching Protocols\r\n'));
    active.flags.logoutInProgress = true; active.peer.emit('end'); await finishSocket(active);
    check(active.state.seen === 1 && active.state.admitted === 1 && active.state.opened === 1 && active.state.closed === 1);
    check(active.entry.connect_transport === true && active.entry.proxy_connect_status === 200 && active.entry.upstream_status === 101);
  });
  test('connect_rejects_tls_plain_http_writes_and_outer_authority_substitution', async () => {
    const good = connectInner().toString('latin1');
    for (const input of [Buffer.from([0x16, 0x03, 0x01, 0x00]), Buffer.from(good.replace(/^GET /, 'POST ')),
      Buffer.from(good.replace(/^GET /, 'CONNECT ')), Buffer.from(good.replace('HTTP/1.1', 'HTTP/1.0')),
      connectInner('/web/index.html', requestHeaders()), Buffer.from(good.replace('Host:', ' Host:')),
      Buffer.from(good.replace(/\r\n/g, '\n') + '\r\n\r\n'), connectInner(`/emby/socket?UserId=${B_ID}`),
      connectInner('/emby/socket', replaceHeader(websocketHeaders(), 'X-Emby-Token', undefined)),
      connectInner('/emby/socket', websocketHeaders(['Content-Length', '1']))]) {
      rejected(() => api.websocketConnectInner(input, A_ID));
      const active = startSocket({ connect: true, rawHeaders: requestHeaders(['X-Emby-Token', A_TOKEN]), head: input });
      await finishSocket(active, 'failed'); check(active.wire.calls.length === 0 && active.authorizations.length === 0);
    }
    const secondRequest = Buffer.from('POST /emby/Sessions/Playing HTTP/1.1\r\nHost: 127.0.0.1:18196\r\n\r\n');
    const pipelined = startSocket({ connect: true, head: Buffer.concat([connectInner(), secondRequest]) });
    await upgradeSocket(pipelined); await finishSocket(pipelined, 'failed');
    check(pipelined.wire.calls.length === 1 && pipelined.wire.calls[0].options.method === 'GET' && pipelined.peer.writes.length === 0);
    check(pipelined.client.writes.length === 1 && pipelined.entry.handshake_delivered === false);
  });
  test('connect_timeouts_close_and_lost_ownership_never_continue_a_pending_handshake', async () => {
    const permission = deferred(), delayed = startSocket({ connect: true, connectAllowed: () => permission.promise });
    let complete = false; delayed.promise.then(() => { complete = true; });
    await flushMicrotasks(); loaded.fireTimers(10000); await flushMicrotasks();
    check(delayed.client.destroyed && !complete && delayed.state.active === 0 && delayed.wire.calls.length === 0);
    permission.resolve(); await finishSocket(delayed, 'failed'); check(delayed.client.writes.length === 0);
    for (const event of ['permission_rejected', 'header_timeout', 'client_closed', 'ownership_lost', 'write_failed']) {
      const client = new FakeWebSocket(); if (event === 'write_failed') client.autoWrite = false;
      const flags = { ownershipLost: false, logoutInProgress: false };
      const active = startSocket({ connect: true, client, flags,
        connectAllowed: async () => { if (event === 'permission_rejected') throw new Error(A_TOKEN); } });
      await flushMicrotasks();
      if (event === 'header_timeout') loaded.fireTimers(5000);
      else if (event === 'client_closed') client.destroy();
      else if (event === 'ownership_lost') { flags.ownershipLost = true; client.receive(connectInner()); }
      else if (event === 'write_failed') client.flushWrites(new Error(A_TOKEN));
      await finishSocket(active, 'failed');
      check(active.wire.calls.length === 0 && active.authorizations.length === 0 && client.listenerCount('data') === 0);
      client.emit('data', connectInner()); check(active.events.length === 1 && active.wire.calls.length === 0);
    }
  });
  test('connect_reservation_counts_the_whole_attempt_once_and_requires_both_response_layers', async () => {
    const permission = deferred(), state = websocketState(), pending = startSocket({ connect: true, state, connectAllowed: () => permission.promise });
    const competing = startSocket({ state }); await competing.promise;
    check(state.seen === 2 && state.admitted === 1 && state.active === 1 && competing.wire.calls.length === 0);
    permission.reject(new Error(A_TOKEN)); await finishSocket(pending, 'failed');
    const third = startSocket({ connect: true, state }); await finishSocket(third, 'failed'); check(third.permissions.length === 0 && state.seen === 3);
    const fresh = websocketState(), frame = messageFrame('Other', { masked: true });
    const connected = startSocket({ connect: true, state: fresh, head: Buffer.concat([connectInner(), frame]) });
    await upgradeSocket(connected); check(fresh.seen === 1 && fresh.admitted === 1 && connected.peer.writes[0].equals(frame));
    connected.flags.logoutInProgress = true; connected.peer.emit('end'); await finishSocket(connected);
    const direct = startSocket({ state: fresh }); await upgradeSocket(direct);
    direct.flags.logoutInProgress = true; direct.peer.emit('end'); await finishSocket(direct);
    check(fresh.seen === 2 && fresh.admitted === 2 && fresh.opened === 2 && fresh.closed === 2 && fresh.failed === 0);
    const report = completedReport(), entry = report.accounts[0].websocket.entries[0];
    entry.connect_transport = true; entry.proxy_connect_status = 200; check(api.crossUserResult(report) === true);
    for (const value of [null, 101, 401, undefined]) { entry.proxy_connect_status = value; check(api.crossUserResult(report) === false); }
    entry.proxy_connect_status = 200; entry.connect_transport = false; check(api.crossUserResult(report) === false);
  });
  test('preparation_mode_allows_only_the_explicit_fixed_movie_post', () => {
    const route = `${ORIGIN}/emby/Items/${PREPARATION_ITEM}/PlaybackInfo`;
    check(same(api.classifyBrowserRequest(route, 'POST', 'fetch', 'acceptance-preparation'), { allow: true, kind: 'preparation' }));
    for (const mode of ['acceptance', 'prelogin']) check(api.classifyBrowserRequest(route, 'POST', 'fetch', mode).allow === false);
    for (const method of ['GET', 'PUT', 'PATCH', 'DELETE']) check(api.classifyBrowserRequest(route, method, 'fetch', 'acceptance-preparation').allow === false);
    for (const raw of [route.replace(PREPARATION_ITEM, '1'.repeat(32)), `${route}/extra`, `${DIRECT}/emby/Items/${PREPARATION_ITEM}/PlaybackInfo`,
      `${ORIGIN}/emby/Sessions/Playing`, `${ORIGIN}/emby/Sessions/Playing/Progress`, `${ORIGIN}/emby/Users/${A_ID}/Configuration`,
      `${ORIGIN}/emby/Items/${PREPARATION_ITEM}`]) {
      check(api.classifyBrowserRequest(raw, 'POST', 'fetch', 'acceptance-preparation').allow === false);
    }
    check(api.classifyBrowserRequest(`${ORIGIN}/emby/Videos/${PREPARATION_ITEM}/stream.mp4`, 'GET', 'media', 'acceptance-preparation').allow === false);
    check(api.websocketRequestPlan('ws://127.0.0.1:18196/emby/socket', 'GET', websocketHeaders(), A_ID, 'acceptance-preparation').authority.token === A_TOKEN);
    check(api.websocketConnectPlan('127.0.0.1:18196', 'CONNECT', requestHeaders(), 'acceptance-preparation') === undefined);
    check(api.browserRequestDiagnostic(route, 'fetch', 'POST').category === 'playback_preparation');
    const diagnostic = api.browserRequestDiagnostic(`http://example.invalid/path?token=${A_TOKEN}`, 'fetch', 'POST');
    check(diagnostic.category === 'external_post' && !JSON.stringify(diagnostic).includes(A_TOKEN));
  });
  test('preparation_source_comes_from_one_matching_local_movie_source', () => {
    const expected = { id: PREPARATION_ITEM, name: 'M3e Client Movie', path: MOVIE_PATH };
    check(same(api.bindOwnedMovieSource(movieDTO(), expected), ownedMovieSource()));
    check(ownedMovieSource().source_id !== PREPARATION_ITEM);
    for (const mutate of [value => { value.Id = '1'.repeat(32); }, value => { value.Type = 'Audio'; }, value => { value.Name = 'Other'; },
      value => { value.Path = '/outside/movie.mp4'; }, value => { value.MediaSources = []; },
      value => { value.MediaSources.push(clone(value.MediaSources[0])); }, value => { value.MediaSources[0].Id = ''; },
      value => { value.MediaSources[0].Id = 'x'.repeat(257); }, value => { value.MediaSources[0].Id = 'http://example.invalid/'; },
      value => { value.MediaSources[0].ItemId = '1'.repeat(32); }, value => { value.MediaSources[0].Path = '/outside/movie.mp4'; },
      value => { value.MediaSources[0].Protocol = 'Http'; }, value => { value.MediaSources[0].IsRemote = true; }]) {
      const value = movieDTO(); mutate(value); rejected(() => api.bindOwnedMovieSource(value, expected));
    }
    rejected(() => api.bindOwnedMovieSource(movieDTO(), { ...expected, id: '1'.repeat(32) }));
  });
  test('preparation_request_binds_scope_selectors_and_original_body_without_rewriting', () => {
    const source = ownedMovieSource(), raw = `${ORIGIN}/emby/Items/${PREPARATION_ITEM}/PlaybackInfo?UserId=${A_ID}&MediaSourceId=${source.source_id}`;
    const bytes = preparationBody(), original = Buffer.from(bytes), headers = requestHeaders(['Content-Type', 'application/json; charset=utf-8', 'X-Emby-Token', A_TOKEN]);
    const proof = api.preparationRequestEvidence(raw, headers, bytes, preparationScope());
    check(proof.item_id === PREPARATION_ITEM && proof.source_id === source.source_id && proof.user_id === A_ID && proof.token_fingerprint === hash(A_TOKEN));
    check(proof.request_bytes === bytes.length && proof.request_sha256 === hash(bytes) && proof.user_id_source === 'explicit_request');
    check(proof.existing_play_or_live_session_requested === false && bytes.equals(original));
    const defaulted = api.preparationRequestEvidence(`${ORIGIN}/Items/${PREPARATION_ITEM}/PlaybackInfo`,
      requestHeaders(['Content-Type', 'text/plain', 'X-Emby-Token', A_TOKEN]), Buffer.from('{}'), preparationScope());
    check(defaulted.user_id_source === 'authenticated_user_default' && defaulted.source_id === source.source_id);
    check(!JSON.stringify(proof).includes(A_TOKEN));
  });
  test('preparation_request_rejects_wrong_stage_authority_selectors_and_ambiguous_json', () => {
    const raw = `${ORIGIN}/emby/Items/${PREPARATION_ITEM}/PlaybackInfo`, bytes = preparationBody();
    const headers = requestHeaders(['Content-Type', 'application/json', 'X-Emby-Token', A_TOKEN]);
    for (const [key, value] of [['mode', 'acceptance'], ['phase', 'ui_home'], ['ordinary', false], ['loginCompleted', false],
      ['logoutStarted', true], ['userId', B_ID], ['token', B_TOKEN]]) {
      const scope = preparationScope(); scope[key] = value; rejected(() => api.preparationRequestEvidence(raw, headers, bytes, scope));
    }
    for (const route of [raw.replace(PREPARATION_ITEM, '1'.repeat(32)), raw.replace(ORIGIN, DIRECT),
      `${raw}?UserId=${B_ID}`, `${raw}?UserId=${A_ID}&userid=${A_ID}`, `${raw}?CurrentPlaySessionId=play_${'1'.repeat(32)}`,
      `${raw}?MediaSourceId=wrong-source`, `${raw}?api_key=${B_TOKEN}`]) {
      rejected(() => api.preparationRequestEvidence(route, headers, bytes, preparationScope()));
    }
    for (const [key, value] of [['UserId', B_ID], ['Id', '1'.repeat(32)], ['MediaSourceId', PREPARATION_ITEM],
      ['CurrentPlaySessionId', 'play_' + '1'.repeat(32)], ['LiveStreamId', 'existing-live'], ['UserId', 123]]) {
      const body = JSON.parse(bytes.toString('utf8')); body[key] = value;
      rejected(() => api.preparationRequestEvidence(raw, headers, Buffer.from(JSON.stringify(body)), preparationScope()));
    }
    for (const body of [Buffer.alloc(0), Buffer.alloc(131073), Buffer.from(`{"UserId":"${A_ID}","userid":"${A_ID}"}`),
      Buffer.from('[]'), Buffer.from(JSON.stringify(Object.fromEntries(Array.from({ length: 65 }, (_, index) => [`Field${index}`, true]))))]) {
      rejected(() => api.preparationRequestEvidence(raw, headers, body, preparationScope()));
    }
    for (const body of [Buffer.from([0xff]), Buffer.from('{invalid')]) {
      let refused = false;
      try { api.preparationRequestEvidence(raw, headers, body, preparationScope()); } catch (error) { refused = ['TypeError', 'SyntaxError'].includes(error.name); }
      check(refused);
    }
    rejected(() => api.preparationRequestEvidence(raw, replaceHeader(headers, 'Content-Type', 'application/x-www-form-urlencoded'), bytes, preparationScope()));
    rejected(() => api.preparationRequestEvidence(raw, replaceHeader(headers, 'X-Emby-Token', B_TOKEN), bytes, preparationScope()));
  });
  test('preparation_response_records_only_actual_play_id_and_matching_media_source', () => {
    const source = ownedMovieSource(), bytes = Buffer.from(JSON.stringify(preparationReply())), original = Buffer.from(bytes);
    const result = api.preparationResponseEvidence(200, bytes, source);
    check(result.validated === true && result.play_session_id === preparationReply().PlaySessionId && result.reason === null && bytes.equals(original));
    for (const status of [202, 400, 401, 403, 500]) check(api.preparationResponseEvidence(status, bytes, source).validated === false);
    for (const mutate of [value => { value.PlaySessionId = 'not-a-play-session'; }, value => { delete value.PlaySessionId; },
      value => { value.MediaSources = []; }, value => { value.MediaSources.push(clone(value.MediaSources[0])); },
      value => { value.MediaSources[0].Id = PREPARATION_ITEM; }, value => { value.MediaSources[0].ItemId = '1'.repeat(32); },
      value => { value.MediaSources[0].Path = '/outside/movie.mp4'; }]) {
      const value = preparationReply(); mutate(value);
      check(api.preparationResponseEvidence(200, Buffer.from(JSON.stringify(value)), source).validated === false);
    }
    for (const body of [Buffer.alloc(0), Buffer.from('{invalid'), Buffer.from([0xff]), Buffer.alloc(2 * 1024 * 1024 + 1)]) {
      check(api.preparationResponseEvidence(200, body, source).validated === false);
    }
  });
  test('preparation_forwarding_hooks_preserve_bytes_and_consume_one_physical_intent', async () => {
    const state = proxyState('acceptance-preparation'), body = preparationBody(), reply = Buffer.from(JSON.stringify(preparationReply()));
    state.login = 1;
    const scope = preparationScope(), intents = { login: true, logout: false, ownershipLost: false, preparation: true };
    let requestProof, responseProof;
    const options = { state, method: 'POST', url: `${ORIGIN}/emby/Items/${PREPARATION_ITEM}/PlaybackInfo`,
      rawHeaders: requestHeaders(['Content-Type', 'application/json', 'Content-Length', String(body.length), 'X-Emby-Token', A_TOKEN]), intents,
      onRequest(request, plan, bytes) { check(plan.kind === 'preparation' && bytes.equals(body)); requestProof = api.preparationRequestEvidence(request.url, request.rawHeaders, bytes, scope); },
      onResponse(_request, plan, status, bytes) { check(plan.kind === 'preparation' && bytes.equals(reply)); responseProof = api.preparationResponseEvidence(status, bytes, scope.source); } };
    const first = startForward(options); first.request.finish(body); respondForward(first, 200, ['Content-Length', String(reply.length)], reply);
    await forwardFinished(first); check(state.preparation === 1 && first.peer.calls[0].request.sentBody.equals(body) && first.response.entity.equals(reply));
    check(requestProof.request_sha256 === hash(body) && responseProof.validated === true);
    const second = startForward(options); await forwardFinished(second, 'rejected'); check(second.peer.calls.length === 0 && state.preparation === 1);
    const failedState = proxyState('acceptance-preparation'); failedState.login = 1;
    const failedOptions = { ...options, state: failedState, onRequest() { throw new Error(A_TOKEN); } };
    const failed = startForward(failedOptions); failed.request.finish(body); await forwardFinished(failed, 'failed');
    const retry = startForward(failedOptions); await forwardFinished(retry, 'rejected');
    check(failedState.preparation === 1 && failed.peer.calls.length === 0 && retry.peer.calls.length === 0);
    const disallowed = startForward({ ...options, state: proxyState('acceptance-preparation'), intents: { ...intents, preparation: false } });
    await forwardFinished(disallowed, 'rejected'); check(disallowed.state.preparation === 0 && disallowed.peer.calls.length === 0);
  });
  test('home_navigation_requires_one_real_control_and_confirmed_library_card', () => {
    check(api.selectHomeControl(0, 0) === null && api.selectHomeControl(1, 0) === 'link' && api.selectHomeControl(0, 1) === 'button');
    check(api.selectHomeControl(1, 1) === 'link');
    for (const values of [[2, 0], [0, 2], [-1, 0], [0.5, 1], ['1', 0], [65, 0]]) rejected(() => api.selectHomeControl(...values));
    const previous = `${ORIGIN}/web/index.html#!/item?id=${PREPARATION_ITEM}`;
    for (const route of ['', '#!/home', '#/home', '#!/home.html', '#/home.html']) {
      const location = api.homeNavigationLocation(`${ORIGIN}/web/index.html${route}`, previous);
      const evidence = { ...location, library_card_count: 1, detail_heading_count: 0, card_present: true, card_id_present: true, card_id_matches: true };
      check(api.confirmedHomeNavigation(evidence) === true);
      for (const [key, value] of [['same_origin', false], ['supported_path', false], ['navigation_observed', false], ['route', 'other'],
        ['library_card_count', 0], ['library_card_count', 2], ['detail_heading_count', 1], ['card_present', false], ['card_id_matches', false]]) {
        check(api.confirmedHomeNavigation({ ...evidence, [key]: value }) === false);
      }
      check(api.confirmedHomeNavigation({ ...evidence, card_id_present: false, card_id_matches: null }) === true);
    }
    for (const raw of [`${DIRECT}/web/index.html#!/home`, `${ORIGIN}/other#!/home`, previous, 'not a URL']) {
      check(api.confirmedHomeNavigation({ ...api.homeNavigationLocation(raw, previous), library_card_count: 1, detail_heading_count: 0,
        card_present: true, card_id_present: true, card_id_matches: true }) === false);
    }
  });
  test('preparation_acceptance_requires_each_validated_physical_post_and_real_ui_completion', () => {
    const preparedReport = () => completedPreparationReport(api);
    check(api.crossUserResult(preparedReport()) === true);
    for (const scope of [undefined, null, '', 'source28-page-error-01', false]) {
      const report = preparedReport(); report.preparation_scope = scope; check(api.crossUserResult(report) === false);
    }
    for (const slot of [0, 1]) for (const mutate of [value => { value.proxy.preparation = 0; }, value => { value.proxy.preparation = 2; },
      value => { value.preparation.request_validated = false; }, value => { value.preparation.item_id = '1'.repeat(32); },
      value => { value.preparation.token_fingerprint = syntheticHash('wrong-preparation-token'); },
      value => { value.preparation.completed = false; }, value => { value.preparation.response.status = 202; },
      value => { value.preparation.response.validated = false; }, value => { value.preparation.ui_status = 403; },
      value => { value.preparation.ui_finished = false; }]) {
      const report = preparedReport(); mutate(report.accounts[slot]); check(api.crossUserResult(report) === false);
    }
    const readonly = completedReport(); readonly.accounts[0].proxy.preparation = 1; check(api.crossUserResult(readonly) === false);
    const prelogin = preloginReport(); prelogin.accounts[0].proxy.preparation = 1; check(api.preloginDiagnosticsComplete(prelogin) === false);
  });
  test('acceptance_requires_explicit_zero_page_errors_for_both_actors', () => {
    check(api.crossUserResult(completedReport()) === true);
    for (const slot of [0, 1]) {
      for (const count of [1, 16, 17, -1, null, false, NaN, '0', undefined]) {
        const report = completedReport(); report.accounts[slot].page_error_count = count;
        check(api.crossUserResult(report) === false);
      }
      const report = completedReport(); delete report.accounts[slot].page_error_count;
      check(api.crossUserResult(report) === false);
      report.accounts[slot].page_errors = [];
      check(api.crossUserResult(report) === false);
    }
  });
  test('diagnostic_messages_redact_known_secrets_and_bounded_encoding_variants', () => {
    const secret = 'Secret7!', bytes = Buffer.from(secret), letters = [...secret];
    const hex = value => value.charCodeAt(0).toString(16);
    const percent = letters.map(value => '%' + hex(value).padStart(2, '0')).join('');
    const variants = [secret, secret.toLowerCase(), percent, percent.replaceAll('%', '%25'),
      letters.map((value, index) => index % 2 ? value : '%' + hex(value)).join(''),
      letters.map(value => '\\u' + hex(value).padStart(4, '0')).join(''),
      letters.map(value => '\\u{' + hex(value) + '}').join(''),
      letters.map(value => '\\x' + hex(value).padStart(2, '0')).join(''),
      letters.map(value => '%u' + hex(value).padStart(4, '0')).join(''),
      letters.map(value => '&#' + value.charCodeAt(0) + ';').join(''),
      letters.map(value => '&#x' + hex(value) + ';').join(''),
      bytes.toString('base64'), bytes.toString('base64').replace(/=+$/, ''),
      bytes.toString('base64url'), bytes.toString('hex')];
    for (const value of variants) {
      check(api.sanitizeBrowserMessage('Fault ' + value + ' observed', [secret]) === 'Fault [secret] observed');
      const result = api.sanitizeBrowserMessage('Malformed %zz then ' + value, [secret]);
      check(result.includes('[secret]') && !result.toLowerCase().includes(secret.toLowerCase()) && result.length <= 512);
    }
    const literal = 'Pin.+[7]';
    check(api.sanitizeBrowserMessage('Fault ' + literal + ' observed', [literal]) === 'Fault [secret] observed');
    const union = [A_PASSWORD, B_PASSWORD, A_TOKEN, B_TOKEN, '3'.repeat(48)];
    check(union.every(value => !api.sanitizeBrowserMessage(union.join(' '), union).includes(value)));
  });
  test('diagnostic_messages_omit_oversize_inputs_and_remove_locations_and_opaque_values', () => {
    check(api.sanitizeBrowserMessage('A readable failure') === 'A readable failure');
    check(api.sanitizeBrowserMessage('UniquePrefix ' + 'x'.repeat(8192)) === '[diagnostic omitted: input limit]');
    for (const value of [null, undefined, 7, {}, Buffer.from('private')]) {
      check(api.sanitizeBrowserMessage(value) === '[diagnostic unavailable]');
    }
    for (const value of ['https://host.invalid/private?ticket=SmallKey', 'wss://host.invalid/ws#SmallKey',
      'file:///private/SmallKey', 'blob:https://host.invalid/SmallKey', 'data:text/plain,SmallKey',
      'custom://host.invalid/SmallKey', '//host.invalid/SmallKey', '/private/SmallKey',
      'folder/SmallKey', '\\\\server\\SmallKey', 'localhost:18196?ticket=SmallKey',
      '?ticket=SmallKey', 'ticket=SmallKey', 'token:SmallKey', 'https://host.invalid/"quoted"?ticket=SmallKey']) {
      const result = api.sanitizeBrowserMessage('Failure ' + value + ' observed');
      check(!result.includes('SmallKey') && !result.includes('host.invalid') && result.length <= 512);
    }
    const opaque = 'Ab7cd9EFgh2JKlm4NOPqr6STuv8WXyz0';
    check(api.sanitizeBrowserMessage('Fault ' + opaque + ' observed') === 'Fault [opaque] observed');
    const long = api.sanitizeBrowserMessage('readable '.repeat(160));
    check(long.length <= 512 && long.endsWith(' [truncated]') && !long.includes('readabl [truncated]'));
    const controls = api.sanitizeBrowserMessage('before\u0000\u001b\u2028after');
    check(/^[\x20-\x7e]*$/.test(controls));
    const name = api.browserEventDiagnostic({ name: 'Readable '.repeat(20), message: 'Fault' }, 'ui_movie');
    check(name.diagnostic_name === '[diagnostic omitted: name limit]' && name.diagnostic_name.length <= 80);
  });
  test('browser_event_diagnostics_read_only_name_and_message_even_when_getters_throw', () => {
    const reads = [], error = Object.create(null);
    Object.defineProperties(error, {
      name: { get() { reads.push('name'); return 'TypeError'; } },
      message: { get() { reads.push('message'); return 'Fault ' + A_TOKEN; } },
      stack: { get() { throw new Error('forbidden stack read'); } },
      errors: { get() { throw new Error('forbidden aggregate read'); } },
      cause: { get() { throw new Error('forbidden cause read'); } },
      toString: { get() { throw new Error('forbidden coercion'); } },
    });
    const diagnostic = api.browserEventDiagnostic(error, 'ui_movie', [A_TOKEN]);
    check(same(reads, ['name', 'message']) && diagnostic.phase === 'ui_movie' && diagnostic.diagnostic_name === 'TypeError');
    check(diagnostic.message === 'Fault [secret]' && diagnostic.diagnostic_unavailable === false);
    check(same(Object.keys(diagnostic).sort(), ['phase', 'name', 'category', 'causes', 'causes_truncated',
      'diagnostic_name', 'message', 'diagnostic_unavailable'].sort()));
    const throwing = Object.create(null);
    for (const key of ['name', 'message']) Object.defineProperty(throwing, key, { get() { throw new Error(A_TOKEN); } });
    const unavailable = api.browserEventDiagnostic(throwing, A_TOKEN, [A_TOKEN]);
    check(unavailable.diagnostic_unavailable === true && unavailable.phase === 'unknown');
    check(!JSON.stringify(unavailable).includes(A_TOKEN));
    check(api.browserEventDiagnostic({ name: 3, message: null }, 'ui_movie').diagnostic_unavailable === true);
  });
  test('page_error_count_survives_throwing_messages_and_the_sixteen_entry_limit', () => {
    const actor = { page_error_count: 0, page_errors: [] };
    for (let index = 0; index < 21; index += 1) {
      const error = index === 0 ? Object.defineProperties({}, {
        name: { get() { throw new Error(A_TOKEN); } }, message: { get() { throw new Error(A_TOKEN); } },
      }) : { name: 'Error', message: 'Fault ' + A_TOKEN };
      api.recordBrowserPageError(actor, error, 'ui_movie', index, [A_TOKEN]);
    }
    check(actor.page_error_count === 21 && actor.page_errors.length === 16);
    check(actor.page_errors[0].diagnostic_unavailable === true && actor.page_errors[15].elapsed_ms === 15);
    let accessed = false;
    const beyondLimit = Object.defineProperty({}, 'message', { get() { accessed = true; throw new Error(A_TOKEN); } });
    api.recordBrowserPageError(actor, beyondLimit, 'ui_movie', 22, [A_TOKEN]);
    check(actor.page_error_count === 22 && actor.page_errors.length === 16 && !accessed);
    check(!JSON.stringify(actor).includes(A_TOKEN));
  });
  test('final_diagnostic_resanitization_removes_late_secrets_from_each_actor_and_event_list', () => {
    const late = 'LateKey7', other = 'OtherKey8';
    const report = { accounts: ['A', 'B'].map(slot => ({ ...actor(slot), page_error_count: 1,
      page_errors: [api.browserEventDiagnostic({ name: late, message: 'Fault ' + other }, 'ui_movie')],
      console_diagnostics: [api.browserEventDiagnostic({ name: other, message: 'Fault ' + late }, 'ui_home')] })) };
    check(JSON.stringify(report).includes(late) && JSON.stringify(report).includes(other));
    api.resanitizeBrowserDiagnostics(report, [late, other]);
    check(!JSON.stringify(report).includes(late) && !JSON.stringify(report).includes(other));
    for (const account of report.accounts) {
      check(account.page_error_count === 1 && account.page_errors.length === 1 && account.console_diagnostics.length === 1);
      check(account.page_errors[0].message === 'Fault [secret]' && account.page_errors[0].diagnostic_name === '[secret]');
      check(account.console_diagnostics[0].message === 'Fault [secret]' && account.console_diagnostics[0].diagnostic_name === '[secret]');
    }
    const once = clone(report); api.resanitizeBrowserDiagnostics(report, [late, other]); check(same(report, once));
  });
  test('preparation_scope_is_explicit_mode_bound_and_rejected_before_external_effects', async () => {
    const args = [...validArguments(), '--mode', 'acceptance-preparation', '--preparation-scope', PREPARATION_SCOPE];
    const parsed = api.parseCrossUserArguments(args);
    check(parsed.mode === 'acceptance-preparation' && parsed['preparation-scope'] === PREPARATION_SCOPE);
    for (const invalid of [[...validArguments(), '--mode', 'acceptance-preparation'],
      [...validArguments(), '--preparation-scope', PREPARATION_SCOPE],
      [...validArguments(), '--mode', 'prelogin', '--preparation-scope', PREPARATION_SCOPE],
      [...validArguments(), '--mode', 'acceptance', '--preparation-scope', PREPARATION_SCOPE]]) {
      rejected(() => api.parseCrossUserArguments(invalid));
    }
    for (const value of ['', 'source28-page-error-01', PREPARATION_SCOPE + '/..', 'SCHEMA27-ORIGINAL-MOVIE-01']) {
      const invalid = [...args]; invalid[17] = value; rejected(() => api.parseCrossUserArguments(invalid));
    }
    const duplicate = [...args]; duplicate[14] = '--preparation-scope'; duplicate[15] = PREPARATION_SCOPE;
    rejected(() => api.parseCrossUserArguments(duplicate));
    for (const mode of ['acceptance', 'prelogin']) for (const scope of [PREPARATION_SCOPE, '', null]) {
      rejected(() => api.requireProxyExecutionMode(mode, scope));
    }
    for (const scope of [undefined, null, '', 'source28-page-error-01']) {
      rejected(() => api.requireProxyExecutionMode('acceptance-preparation', scope));
      const options = { ...parsed, 'preparation-scope': scope };
      let refused = false;
      try { await api.runCrossUserAcceptance(options); } catch (error) { refused = error?.message === 'cross_user_guard_rejected'; }
      check(refused && loaded.effects.length === 0 && loaded.timers.size === 0);
    }
    for (const scope of [PREPARATION_SCOPE, '', false]) {
      const report = completedReport(); report.preparation_scope = scope; check(api.crossUserResult(report) === false);
    }
    const report = completedReport(); report.preparation_scope = null; check(api.crossUserResult(report) === true);
  });
  test('schema27_preparation_requires_current_fixture_and_historical_music_lineage', () => {
    check(api.requirePreparationFixture({ evidence: schema27FixtureEvidence() }, 'acceptance-preparation') === undefined);
    for (const mutate of [value => { value.schema = 26; }, value => { value.schema = '27'; },
      value => { value.schema_binding.schema = 26; }, value => { value.music_scan.schema = 27; },
      value => { value.music_scan.upgrade_lineage.to_schema = 26; }, value => { delete value.music_scan.upgrade_lineage; }]) {
      const evidence = schema27FixtureEvidence(); mutate(evidence);
      rejected(() => api.requirePreparationFixture({ evidence }, 'acceptance-preparation'));
    }
    rejected(() => api.requirePreparationFixture({}, 'acceptance-preparation'));
    for (const scope of ['original', 'source28-page-error-01', 'schema26-original-movie-01']) {
      rejected(() => api.requireProxyExecutionMode('acceptance-preparation', scope));
    }
  });
  test('special_features_request_binds_exact_origin_user_movie_and_get', () => {
    const route = ORIGIN + '/emby/Users/' + A_ID + '/Items/' + PREPARATION_ITEM + '/SpecialFeatures';
    for (const raw of [route, route + '/', route + '?UserId=' + A_ID, route.replace('/emby/', '/')]) {
      check(api.ownSpecialFeaturesRequest(raw, 'GET', A_ID, PREPARATION_ITEM) === true);
    }
    for (const raw of [route.replace(A_ID, B_ID), route.replace(PREPARATION_ITEM, '1'.repeat(32)),
      route.replace(ORIGIN, 'http://example.invalid'), route + '#fragment', route + '?UserId=' + B_ID,
      route + '?UserId=' + A_ID + '&userid=' + A_ID,
      ORIGIN + '/emby/Users/AuthenticateByName', ORIGIN + '/emby/Items/' + PREPARATION_ITEM + '/SpecialFeatures']) {
      check(api.ownSpecialFeaturesRequest(raw, 'GET', A_ID, PREPARATION_ITEM) === false);
    }
    for (const method of ['POST', 'HEAD', 'OPTIONS', 'PUT']) check(api.ownSpecialFeaturesRequest(route, method, A_ID, PREPARATION_ITEM) === false);
    check(api.ownSpecialFeaturesRequest(route, 'GET', A_ID, '1'.repeat(32)) === false);
    check(api.browserRequestDiagnostic(route, 'fetch', 'GET').category === 'special_features');
  });
  test('special_features_response_requires_actual_complete_empty_json_array', () => {
    const bytes = Buffer.from(' \n[]\t'), original = Buffer.from(bytes);
    const evidence = api.specialFeaturesResponseEvidence(200, bytes);
    check(evidence.validated === true && evidence.json_array === true && evidence.item_count === 0 && bytes.equals(original));
    for (const status of [201, 204, 304, 401, 403, 404, 500]) check(api.specialFeaturesResponseEvidence(status, Buffer.from('[]')).validated === false);
    for (const body of [Buffer.from(''), Buffer.from('{}'), Buffer.from('{"Items":[]}'), Buffer.from('null'),
      Buffer.from('[1]'), Buffer.from('[{"Id":"private-value"}]'), Buffer.from('[ ] trailing'),
      Buffer.from([0xc3, 0x28]), Buffer.alloc(2 * 1024 * 1024 + 1), '[]', null]) {
      const value = api.specialFeaturesResponseEvidence(200, body);
      check(value.validated === false && !JSON.stringify(value).includes('private-value'));
    }
  });
  test('special_features_capture_never_reads_authentication_foreign_or_worker_bodies', async () => {
    for (const options of [{ url: ORIGIN + '/emby/Users/AuthenticateByName', method: 'POST' },
      { url: ORIGIN + '/emby/Users/' + B_ID + '/Items/' + PREPARATION_ITEM + '/SpecialFeatures' },
      { worker: {} }, { method: 'POST' }, { headers: { 'content-type': 'text/html', 'content-length': '2' } },
      { headers: { 'content-type': 'application/json', 'content-length': String(2 * 1024 * 1024 + 1) } }]) {
      const response = specialFeaturesResponseMock(options); let refused = false;
      try { await api.captureSpecialFeaturesResponse(response, A_ID, PREPARATION_ITEM); } catch { refused = true; }
      check(refused && response.state.body_calls === 0 && response.state.finished_calls === 0);
    }
    const missing = specialFeaturesResponseMock({ status: 404 });
    check((await api.captureSpecialFeaturesResponse(missing, A_ID, PREPARATION_ITEM)).validated === false);
    check(missing.state.body_calls === 0 && missing.state.finished_calls === 0);
  });
  test('special_features_capture_waits_for_finish_bounds_wait_and_clears_private_buffer', async () => {
    const response = specialFeaturesResponseMock(), result = await api.captureSpecialFeaturesResponse(response, A_ID, PREPARATION_ITEM);
    check(result.validated === true && response.state.finished_calls === 1 && response.state.body_calls === 1);
    check(response.state.bytes.every(value => value === 0));
    const unfinished = specialFeaturesResponseMock({ finish: async () => new Error('incomplete') }); let refused = false;
    try { await api.captureSpecialFeaturesResponse(unfinished, A_ID, PREPARATION_ITEM); } catch { refused = true; }
    check(refused && unfinished.state.body_calls === 0);
    const hanging = specialFeaturesResponseMock({ finish: () => new Promise(() => {}) });
    const pending = api.captureSpecialFeaturesResponse(hanging, A_ID, PREPARATION_ITEM);
    check(loaded.timers.size === 1); loaded.fireTimers(5000); refused = false;
    try { await pending; } catch (error) { refused = error?.message === 'cross_user_operation_timeout'; }
    check(refused && hanging.state.body_calls === 0 && loaded.timers.size === 0);
  });
  test('special_features_evidence_rejects_absence_partial_foreign_and_duplicate_requests', () => {
    const fingerprint = hash(A_TOKEN), entry = specialFeaturesEntry();
    check(api.specialFeaturesEvidence([entry], A_ID, fingerprint).validated === true);
    check(api.specialFeaturesEvidence([], A_ID, fingerprint).validated === false);
    check(api.specialFeaturesEvidence([{ ...entry, own_special_features: false }], A_ID, fingerprint).validated === false);
    for (const mutate of [value => { value.status = 404; }, value => { value.finished = false; }, value => { value.failed = true; },
      value => { value.phase = 'ui_home'; }, value => { value.frame_owned = false; }, value => { value.method = 'POST'; },
      value => { value.special_features_user_id = B_ID; }, value => { value.special_features_item_id = '1'.repeat(32); },
      value => { value.token_matches_session = false; }, value => { value.token_fingerprint = hash(B_TOKEN); },
      value => { delete value.special_features_response; }, value => { value.special_features_response.json_array = false; },
      value => { value.special_features_response.item_count = 1; }, value => { value.special_features_response.validated = false; },
      value => { value.special_features_response.response_bytes = 0; }, value => { value.special_features_response.response_bytes = 2097153; },
      value => { value.special_features_response.reason = 'invalid_json_array'; }]) {
      const bad = clone(entry); mutate(bad);
      check(api.specialFeaturesEvidence([bad], A_ID, fingerprint).validated === false);
      check(api.specialFeaturesEvidence([entry, { ...bad, index: 1 }], A_ID, fingerprint).validated === false);
    }
    check(api.specialFeaturesEvidence([entry, entry], A_ID, fingerprint).validated === false);
    check(api.specialFeaturesEvidence(Array.from({ length: 5 }, (_, index) => ({ ...entry, index })), A_ID, fingerprint).validated === false);
    check(api.specialFeaturesEvidence([entry], A_ID, null).validated === false);
  });
  test('schema27_full_acceptance_requires_each_real_special_features_receipt_and_zero_page_errors', () => {
    check(api.crossUserResult(completedPreparationReport(api)) === true);
    for (const slot of [0, 1]) for (const mutate of [value => { delete value.special_features; }, value => { value.special_features.validated = false; },
      value => { value.requests = []; }, value => { value.requests[0].finished = false; }, value => { value.requests[0].status = 404; },
      value => { value.special_features.original_ui_request_indexes = [99]; }, value => { value.page_error_count = 1; }]) {
      const report = completedPreparationReport(api); mutate(report.accounts[slot]); check(api.crossUserResult(report) === false);
    }
    for (const mutate of [value => { value.fixture.schema = 26; }, value => { value.fixture.schema_binding.schema = 26; },
      value => { value.fixture.music_scan.upgrade_lineage.to_schema = 26; },
      value => { value.preparation_scope = 'source28-page-error-01'; }]) {
      const report = completedPreparationReport(api); mutate(report); check(api.crossUserResult(report) === false);
    }
  });
  const changedScope = 'library-changed-ui-source44-v1', changedUser = 'ecbbe4cb82403879bc4b4f78894c5738';
  const changedLibrary = 'a9993591e72f0f2e7babcbf8b9c50790';
  const changedBytes = Buffer.from(JSON.stringify({ MessageType: 'LibraryChanged', MessageId: 'e'.repeat(32),
    Data: { ItemsAdded: [], ItemsUpdated: [PREPARATION_ITEM], ItemsRemoved: [], CollectionFolders: [changedLibrary] } }));
  test('library_changed_scope_preserves_old_handshake_and_lifetime_limits', () => {
    check(api.websocketHandshakeBudget() === 2 && api.websocketLifetimeBudget() === 240000);
    check(api.websocketHandshakeBudget('library-permission-ui-v1') === 3 && api.websocketLifetimeBudget('library-permission-ui-v1') === 240000);
    check(api.websocketHandshakeBudget(changedScope) === 2 && api.websocketLifetimeBudget(changedScope) === 480000);
    for (const value of ['', 'library-changed', 'library-changed-ui-source44-v2']) rejected(() => api.websocketLifetimeBudget(value));
  });
  test('library_changed_catalog_selection_has_one_fixed_library_and_target', () => {
    for (const route of [`/Users/${changedUser}/Items?ParentId=${changedLibrary}`, `/Items?UserId=${changedUser}&ParentId=${changedLibrary}`,
      `/Users/${changedUser}/Items/${PREPARATION_ITEM}`, `/Items/${PREPARATION_ITEM}`, `/Items?Ids=${PREPARATION_ITEM}`]) {
      check(api.libraryChangedCatalogRequest(ORIGIN + route, 'GET', changedUser));
      check(!api.libraryChangedCatalogRequest(ORIGIN + route, 'POST', changedUser));
    }
    for (const route of ['/web/index.html', `/Users/${changedUser}/Views`, `/Items?ParentId=${'1'.repeat(32)}`,
      `/Items/${'1'.repeat(32)}`, `/Users/${changedUser}/Items?ParentId=${changedLibrary}&Ids=${'1'.repeat(32)}`]) {
      check(!api.libraryChangedCatalogRequest(ORIGIN + route, 'GET', changedUser));
    }
    for (const route of [`/Items?ParentId=${changedLibrary}&ParentId=${changedLibrary}`,
      `/Items/${PREPARATION_ITEM}?UserId=${A_ID}`, `/Items?Ids=${PREPARATION_ITEM}&Ids=${PREPARATION_ITEM}`]) {
      rejected(() => api.libraryChangedCatalogRequest(ORIGIN + route, 'GET', changedUser));
    }
    rejected(() => api.libraryChangedCatalogRequest(DIRECT + '/Items', 'GET', changedUser));
    rejected(() => api.libraryChangedCatalogRequest(ORIGIN + '/Items', 'GET', A_ID));
  });
  test('library_changed_complete_fragments_are_borrowed_and_never_change_wire', () => {
    const state = api.websocketFrameState('server'), observed = [], borrowed = [];
    const callback = (value, bytes) => { observed.push({ ...value, body: Buffer.from(bytes) }); borrowed.push(bytes); };
    const first = wireFrame(changedBytes.subarray(0, 18), { final: false });
    const middle = wireFrame('ping', { opcode: 9 });
    const last = wireFrame(changedBytes.subarray(18), { opcode: 0 });
    check(api.inspectWebSocketFrames(state, first, 1, callback)[0].equals(first) && observed.length === 0);
    check(api.inspectWebSocketFrames(state, middle, 2, callback)[0].equals(middle) && observed.length === 0);
    check(api.inspectWebSocketFrames(state, last, 3, callback)[0].equals(last));
    check(observed.length === 1 && observed[0].message_index === 1 && observed[0].frame_number === 3 && observed[0].body.equals(changedBytes));
    check(borrowed.every(bytes => bytes.every(value => value === 0)));
    const standalone = wireFrame(changedBytes);
    check(api.inspectWebSocketFrames(api.websocketFrameState('server'), standalone, 4, (_value, bytes) => { bytes.fill(0); })[0].equals(standalone));
    rejected(() => api.inspectWebSocketFrames(api.websocketFrameState('server'), standalone, 4, async () => {}));
    rejected(() => api.inspectWebSocketFrames(api.websocketFrameState('server'), wireFrame(changedBytes, { opcode: 2 }), 4, callback));
    check(api.inspectWebSocketFrames(api.websocketFrameState('server'), wireFrame(changedBytes, { opcode: 2 }), 4).length === 1);
  });
  test('library_changed_delivery_requires_successful_final_wire_write', async () => {
    const received = [], borrowed = [];
    const active = startSocket({ scope: changedScope, onLibraryChanged: (value, bytes) => { received.push({ ...value, body: Buffer.from(bytes) }); borrowed.push(bytes); } });
    await upgradeSocket(active);
    check(active.entry.connection_id === 'library-changed-ws-0' && [...loaded.timers.values()].some(value => value.milliseconds === 480000));
    active.client.autoWrite = false;
    const first = wireFrame(changedBytes.subarray(0, 15), { final: false }), last = wireFrame(changedBytes.subarray(15), { opcode: 0 });
    active.peer.receive(first); active.peer.receive(last);
    check(received.length === 0);
    active.client.flushWrites();
    check(received.length === 1 && received[0].forwarded === true && received[0].complete === true && received[0].received === true &&
      received[0].physical_message_index === 1 && received[0].body.equals(changedBytes) && received[0].token_sha256 === hash(A_TOKEN));
    check(borrowed.every(bytes => bytes.every(value => value === 0)));
    check(active.client.writes.at(-2).equals(first) && active.client.writes.at(-1).equals(last));
    active.flags.logoutInProgress = true; active.peer.emit('end'); await finishSocket(active);
  });
  test('library_changed_failed_wire_write_has_no_delivery_observation', async () => {
    let delivered = 0;
    const active = startSocket({ scope: changedScope, onLibraryChanged: () => { delivered += 1; } });
    await upgradeSocket(active); active.client.autoWrite = false;
    active.peer.receive(wireFrame(changedBytes)); check(delivered === 0);
    active.client.flushWrites(new Error('synthetic failed write'));
    await finishSocket(active, 'failed'); check(delivered === 0);
  });
  test('library_changed_handshake_head_waits_for_actual_downstream_write', async () => {
    const client = new FakeWebSocket(); client.autoWrite = false;
    let delivered = 0;
    const active = startSocket({ client, scope: changedScope, onLibraryChanged: () => { delivered += 1; } });
    await upgradeSocket(active, wireFrame(changedBytes));
    check(delivered === 0 && !active.entry.handshake_delivered);
    client.flushWrites(); check(delivered === 1 && active.entry.handshake_delivered);
    active.flags.logoutInProgress = true; active.peer.emit('end'); await finishSocket(active);
  });
  test('library_changed_handshake_observer_failure_closes_only_its_stream', async () => {
    const active = startSocket({ scope: changedScope, onLibraryChanged: () => {}, onLibraryChangedHandshake: () => { throw new Error('synthetic observer failure'); } });
    await upgradeSocket(active); await finishSocket(active, 'failed');
    check(active.entry.reason === 'library_changed_handshake_observer_failed');
  });
  test('library_changed_socket_binding_rejects_ambiguous_or_foreign_lifetimes', () => {
    const token = hash(B_TOKEN), entry = { index: 0, connection_id: 'library-changed-ws-0', token_fingerprint: token,
      handshake_delivered: true, closed: false };
    check(api.libraryChangedSocketMatch([entry], token, 0) === entry);
    check(api.libraryChangedSocketMatch([entry], token, 1) === null);
    check(api.libraryChangedSocketMatch([{ ...entry, closed: true }], token, 0) === null);
    check(api.libraryChangedSocketMatch([entry], hash(A_TOKEN)) === null);
    rejected(() => api.libraryChangedSocketMatch([entry, { ...entry, index: 1, connection_id: 'library-changed-ws-1' }], token));
    rejected(() => api.libraryChangedSocketMatch([{ ...entry, connection_id: 'wrong' }], token));
    check(api.libraryChangedSocketAuthority(`ws://127.0.0.1:18196/emby/socket?api_key=${B_TOKEN}`, B_TOKEN).fingerprint === token);
    rejected(() => api.libraryChangedSocketAuthority(`ws://127.0.0.1:18197/emby/socket?api_key=${B_TOKEN}`, B_TOKEN));
    rejected(() => api.libraryChangedSocketAuthority(`ws://127.0.0.1:18196/emby/socket?api_key=${A_TOKEN}`, B_TOKEN));
  });
  test('library_changed_catalog_headers_are_passive_unchanged_copies', async () => {
    let captured;
    const active = startForward({ onResponseHeaders: (_request, _plan, headers) => { captured = headers; check(Object.isFrozen(headers)); } });
    active.request.finish();
    const headers = ['Content-Type', 'application/json', 'ETag', 'synthetic-tag'];
    respondForward(active, 200, headers, Buffer.from('{}')); await forwardFinished(active);
    check(same(captured, headers) && active.response.entity.equals(Buffer.from('{}')));
  });
  test('library_changed_browser_close_is_visible_before_physical_close_flush', () => {
    const observer = Object.fromEntries(['admitLogin', 'physicalRequest', 'physicalResponse', 'physicalFinished', 'frameRequest', 'frameResponse',
      'frameFinished', 'catalogRequest', 'catalogResponse', 'catalogFinished', 'physicalLibraryChanged', 'browserLibraryChanged'].map(name => [name, () => {}]));
    const actor = api.createLibraryChangedBrowserActor({ account: { slot: 'B', id: changedUser, password: B_PASSWORD }, pin: () => {}, report: {}, observer });
    const frame = {}, page = new EventEmitter(), socket = new EventEmitter();
    page.mainFrame = () => frame; page.url = () => ORIGIN + '/web/index.html#synthetic-list';
    socket.url = () => `ws://127.0.0.1:18196/emby/socket?api_key=${B_TOKEN}`;
    actor.page = page; actor.token = B_TOKEN;
    actor.report.websocket = { entries: [{ index: 0, connection_id: 'library-changed-ws-0', token_fingerprint: hash(B_TOKEN),
      handshake_delivered: true, closed: false }] };
    actor.observeLibraryChangedSockets(); page.emit('framenavigated', frame); page.emit('websocket', socket);
    const entry = actor.report.websocket.entries[0];
    check(entry.browser_handshake_observed === true && actor.libraryChangedDocumentID === 'document-1');
    socket.emit('close'); check(entry.browser_closed === true && entry.closed === false);
    rejected(() => api.createLibraryChangedBrowserActor({ account: { slot: 'B', id: A_ID }, pin: () => {}, report: {}, observer }));
  });
  const source55Scope = 'library-changed-ui-source55-v6';
  test('source55_catalog_scope_is_explicit_and_does_not_expand_source44', () => {
    for (const prefix of ['', '/emby']) {
      for (const route of [`/Items/${changedLibrary}`, `/Users/${changedUser}/Items/${changedLibrary}`]) {
        const url = ORIGIN + prefix + route;
        check(api.libraryChangedCatalogRequest(url, 'GET', changedUser, source55Scope));
        check(!api.libraryChangedCatalogRequest(url, 'POST', changedUser, source55Scope));
        check(!api.libraryChangedCatalogRequest(url, 'GET', changedUser));
        check(!api.libraryChangedCatalogRequest(url, 'GET', changedUser, changedScope));
      }
    }
    for (const route of [`/Items?Ids=${changedLibrary}`, `/Items/${changedLibrary}/PlaybackInfo`,
      `/Items/${changedLibrary}/Images/Primary`, `/Items/${'1'.repeat(32)}`, `/Users/${A_ID}/Items/${changedLibrary}`]) {
      check(!api.libraryChangedCatalogRequest(ORIGIN + route, 'GET', changedUser, source55Scope));
    }
    rejected(() => api.libraryChangedCatalogRequest(ORIGIN + `/Items/${changedLibrary}?UserId=${A_ID}`, 'GET', changedUser, source55Scope));
    rejected(() => api.libraryChangedCatalogRequest(DIRECT + `/Items/${changedLibrary}`, 'GET', changedUser, source55Scope));
    rejected(() => api.libraryChangedCatalogRequest(ORIGIN + `/Items/${changedLibrary}`, 'GET', changedUser, 'library-changed-ui-source55-v1'));
    rejected(() => api.libraryChangedCatalogRequest(ORIGIN + `/Items/${changedLibrary}`, 'GET', changedUser, 'library-changed-ui-source55-v2'));
    rejected(() => api.libraryChangedCatalogRequest(ORIGIN + `/Items/${changedLibrary}`, 'GET', changedUser, 'library-changed-ui-source55-v3'));
    rejected(() => api.libraryChangedCatalogRequest(ORIGIN + `/Items/${changedLibrary}`, 'GET', changedUser, 'library-changed-ui-source55-v4'));
    rejected(() => api.libraryChangedCatalogRequest(ORIGIN + `/Items/${changedLibrary}`, 'GET', changedUser, 'library-changed-ui-source55-v5'));
    rejected(() => api.libraryChangedCatalogRequest(ORIGIN + `/Items/${changedLibrary}`, 'GET', changedUser, 'library-changed-ui-source55-v7'));
  });
  test('source55_keeps_existing_websocket_budgets_and_wire_delivery', async () => {
    rejected(() => api.websocketHandshakeBudget('library-changed-ui-source55-v1'));
    rejected(() => api.websocketLifetimeBudget('library-changed-ui-source55-v1'));
    rejected(() => api.websocketHandshakeBudget('library-changed-ui-source55-v2'));
    rejected(() => api.websocketLifetimeBudget('library-changed-ui-source55-v2'));
    rejected(() => api.websocketHandshakeBudget('library-changed-ui-source55-v3'));
    rejected(() => api.websocketLifetimeBudget('library-changed-ui-source55-v3'));
    rejected(() => api.websocketHandshakeBudget('library-changed-ui-source55-v4'));
    rejected(() => api.websocketLifetimeBudget('library-changed-ui-source55-v4'));
    rejected(() => api.websocketHandshakeBudget('library-changed-ui-source55-v5'));
    rejected(() => api.websocketLifetimeBudget('library-changed-ui-source55-v5'));
    check(api.websocketHandshakeBudget(source55Scope) === 2 && api.websocketLifetimeBudget(source55Scope) === 480000);
    check(api.websocketHandshakeBudget(changedScope) === 2 && api.websocketLifetimeBudget(changedScope) === 480000);
    const observed = [], bytes = wireFrame(changedBytes);
    const active = startSocket({ scope: source55Scope, onLibraryChanged: (value, body) => observed.push({ ...value, body: Buffer.from(body) }) });
    await upgradeSocket(active); active.client.autoWrite = false; active.peer.receive(bytes);
    check(observed.length === 0); active.client.flushWrites();
    check(observed.length === 1 && observed[0].forwarded === true && observed[0].body.equals(changedBytes) && active.client.writes.at(-1).equals(bytes));
    active.flags.logoutInProgress = true; active.peer.emit('end'); await finishSocket(active);
  });
  test('source55_actor_passes_its_scope_to_passive_physical_capture', () => {
    const captured = [], observer = Object.fromEntries(['admitLogin', 'physicalRequest', 'physicalResponse', 'physicalFinished',
      'frameRequest', 'frameResponse', 'frameFinished', 'catalogRequest', 'catalogResponse', 'catalogFinished',
      'physicalLibraryChanged', 'browserLibraryChanged'].map(name => [name, () => {}]));
    observer.catalogRequest = value => captured.push(value);
    const options = () => ({ account: { slot: 'B', id: changedUser, password: B_PASSWORD }, pin: () => {}, report: {}, observer });
    const old = api.createLibraryChangedBrowserActor(options()), current = api.createLibraryChangedSource55BrowserActor(options());
    const request = { url: ORIGIN + `/Users/${changedUser}/Items/${changedLibrary}`, rawHeaders: ['X-Emby-Token', B_TOKEN] };
    old.observeLibraryChangedCatalog(request, { method: 'GET' }); check(captured.length === 0 && old.catalogPhysicalCount === 0);
    current.observeLibraryChangedCatalog(request, { method: 'GET' });
    check(captured.length === 1 && captured[0].url === request.url && current.catalogPhysicalCount === 1 &&
      old.websocketScope === changedScope && current.websocketScope === source55Scope &&
      current.report.websocket_handshake_budget === 2 && current.report.websocket_lifetime_ms === 480000);
    rejected(() => api.createLibraryChangedSource55BrowserActor({ ...options(), account: { slot: 'A', id: A_ID } }));
  });
  const report = { format: 1, mode: 'pure', result: 'blocked', harness_guards_only: true, client_acceptance: false,
    source_sha256: hash(source), planned_test_count: cases.length, tests: [] };
  for (const { name, action } of cases) {
    const beforeChecks = checks, result = { name, pass: false, checks: 0 };
    try {
      await action(); requireThat(loaded.effects.length === 0 && loaded.timers.size === 0); result.pass = true;
    } catch { /* Never report exception text, credentials, tokens, request headers, or asset payloads. */ }
    result.checks = checks - beforeChecks; report.tests.push(result);
  }
  report.counts = { executed: report.tests.length, passed: report.tests.filter(value => value.pass).length,
    failed: report.tests.filter(value => !value.pass).length };
  requireThat(![A_TOKEN, B_TOKEN, A_PASSWORD, B_PASSWORD, ASSET_PAYLOAD].some(secret => JSON.stringify(report).includes(secret)));
  report.result = report.counts.failed === 0 && report.counts.executed === report.planned_test_count ? 'passed' : 'blocked';
  return report;
}

if (process.argv[1] && path.resolve(process.argv[1]) === SELF) {
  try {
    requireThat(process.platform === 'linux' && process.getuid?.() === 0 && typeof process.env.SSH_CONNECTION === 'string' &&
      process.env.SSH_CONNECTION.length <= 512 && process.env.SSH_CONNECTION.trim().split(/\s+/).length === 4 && process.argv.length === 2);
    const report = await runCrossUserGuards(await fs.readFile(SOURCE, 'utf8'));
    process.stdout.write(JSON.stringify({ result: report.result, mode: 'pure', harness_guards_only: true, counts: report.counts,
      failed: report.tests.filter(value => !value.pass).map(value => value.name) }) + '\n');
    if (report.result !== 'passed') process.exitCode = 1;
  } catch {
    process.stdout.write(JSON.stringify({ result: 'blocked', mode: 'pure', harness_guards_only: true }) + '\n');
    process.exitCode = 1;
  }
}
