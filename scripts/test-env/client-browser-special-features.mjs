#!/usr/bin/env node
/** Observe the receipted positive extras profile with two ordinary users and scoped preparation. */
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import http from 'node:http';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import { TextDecoder } from 'node:util';
import { loadSpecialFeaturesFixture } from './client-browser-special-features-fixture.mjs';
import { browserRequestDiagnostic, safeBrowserFailure, sanitizeBrowserMessage, browserEventDiagnostic, recordBrowserPageError, resanitizeBrowserDiagnostics, sanitizeBootstrapDOM, proxyResponsePlan, frameRequestContext, forwardWebSocketConnect, forwardBrowserWebSocket, observedAuthority, validateReadPrincipal, itemState, foreignAccessDenied, administratorAccessDenied, preparationResponseEvidence, homeNavigationLocation, selectHomeControl, confirmedHomeNavigation, readBoundedJSON } from './client-browser-cross-user.mjs';

import { createBrowserSessionProof } from './client-browser-session-proof.mjs';

const DIAGNOSTIC_INPUT_LIMIT = 8192;
const DIAGNOSTIC_OUTPUT_LIMIT = 512;
const DIAGNOSTIC_ENTRY_LIMIT = 16;
const WS_LIMITS = Object.freeze({ handshakes: 2, active: 1, handshakeMs: 10000, lifetimeMs: 240000, idleMs: 65000,
  frameBytes: 65536, messageBytes: 65536, wireBytes: 8 * 1024 * 1024, queuedBytes: 1024 * 1024,
  framesPerSecond: 64, framesTotal: 512 });
function normalizeDiagnosticEncoding(value) {
  const character = number => Number.isSafeInteger(number) && number >= 0 && number <= 0x10ffff
    ? String.fromCodePoint(number) : ' ';
  for (let pass = 0; pass < 4; pass += 1) {
    const before = value;
    value = value.replace(/\\u\{([0-9a-f]{1,6})\}|\\u([0-9a-f]{4})|\\x([0-9a-f]{2})/gi,
      (_match, point, unicode, byte) => character(parseInt(point ?? unicode ?? byte, 16)))
      .replace(/%u([0-9a-f]{4})/gi, (_match, unicode) => character(parseInt(unicode, 16)))
      .replace(/%([0-9a-f]{2})/gi, (_match, byte) => character(parseInt(byte, 16)))
      .replace(/&#(?:x([0-9a-f]{1,6})|([0-9]{1,7}));?/gi,
        (_match, hex, decimal) => character(parseInt(hex ?? decimal, hex ? 16 : 10)))
      .replace(/&(amp|quot|apos|colon|sol|bsol|quest|equals|num);/gi,
        (_match, name) => ({ amp: '&', quot: '"', apos: "'", colon: ':', sol: '/', bsol: '\\',
          quest: '?', equals: '=', num: '#' })[name.toLowerCase()])
      .replace(/\\\//g, '/');
    if (value === before) break;
  }
  return value;
}

function diagnosticSecretVariants(secrets) {
  requireThat(Array.isArray(secrets) && secrets.length <= 128);
  const values = new Set();
  for (const secret of secrets) {
    if (secret === null || secret === undefined || secret === '') continue;
    requireThat(typeof secret === 'string' && secret.length <= 4096);
    const bytes = Buffer.from(secret, 'utf8');
    for (const variant of [secret, normalizeDiagnosticEncoding(secret), JSON.stringify(secret).slice(1, -1),
      bytes.toString('base64'), bytes.toString('base64').replace(/=+$/, ''), bytes.toString('base64url'), bytes.toString('hex'),
      [...bytes].map(byte => '%' + byte.toString(16).padStart(2, '0')).join('')]) {
      if (variant) values.add(variant);
    }
  }
  return [...values].sort((left, right) => right.length - left.length);
}

function redactDiagnosticSecrets(value, variants) {
  for (const variant of variants) value = value.replace(new RegExp(variant.replace(/[.*+?^$(){}|[\]\\]/g, '\\$&'), 'gi'), '[secret]');
  return value;
}

const ROOT = '/opt/goby-test/exec-work-m3e';
const ORIGIN = 'http://127.0.0.1:18196';
const DIRECT = 'http://127.0.0.1:18198';
const MARKER = 'goby-m3e-client-acceptance-v1';
const AV_MARKER = 'goby-m3e-goby-av-user-v1';
const PLAYWRIGHT = '/opt/goby-test/inactive-dependencies-m5h/node_modules/playwright';
const SELF = fileURLToPath(import.meta.url);
const STATE = `${ROOT}/client-fixture.json`;
const A_CREDENTIALS = `${ROOT}/goby-av-browser.json`;
const B_CREDENTIALS = `${ROOT}/browser.json`;
const OLD_MOVIE_ITEM = '268051d3ca734aefcf94e245fb25ad55';
const PREPARATION_SCOPE = 'schema27-positive-special-features-01';
const LIMIT = 2 * 1024 * 1024;
const INPUT_NAMES = ['client-browser-special-features.mjs', 'client-browser-special-features-fixture.mjs',
  'client-browser-cross-user.mjs', 'client-browser-goby-fixture.mjs', 'client-browser-session-proof.mjs'];
const FROZEN_SHARED_INPUTS = Object.freeze({
  'client-browser-cross-user.mjs': '53fa6e9eb40f96ff7ace6821a0c64629ead1c95b5fc37188f28ab3d84b58b76b',
  'client-browser-goby-fixture.mjs': '551312bb5b55f2b1330631b10fbc12000d96ffa5a9113b179e71f1fb2dc489b3',
  'client-browser-session-proof.mjs': 'fea90503a3e279d1ec63723f8785b3421c72d756af4db95f1762fbd67ece2472',
});
const ID = /^[0-9a-f]{32}$/;
const SHA = /^[0-9a-f]{64}$/;
const record = value => value !== null && typeof value === 'object' && !Array.isArray(value);
const hash = value => createHash('sha256').update(value).digest('hex');
const stable = value => Array.isArray(value) ? value.map(stable) : record(value)
  ? Object.fromEntries(Object.keys(value).sort().map(key => [key, stable(value[key])])) : value;
const same = (left, right) => JSON.stringify(stable(left)) === JSON.stringify(stable(right));
const digest = value => hash(JSON.stringify(stable(value)));
const acceptanceMode = value => value === 'acceptance-preparation';
function requireThat(value) { if (!value) throw new Error('cross_user_guard_rejected'); }
function bounded(promise, milliseconds) {
  let timer;
  return Promise.race([promise, new Promise((_, reject) => {
    timer = setTimeout(() => reject(new Error('cross_user_operation_timeout')), milliseconds);
  })]).finally(() => clearTimeout(timer));
}
function remoteEnvironment() {
  requireThat(process.platform === 'linux' && process.getuid?.() === 0 && typeof process.env.SSH_CONNECTION === 'string' &&
    process.env.SSH_CONNECTION.length <= 512 && process.env.SSH_CONNECTION.trim().split(/\s+/).length === 4 &&
    !process.env.DEBUG && !process.env.PWDEBUG);
}

export function parseSpecialFeaturesArguments(argv) {
  const names = ['candidate-sha256', 'source', 'source-manifest-sha256', 'music-scan-receipt-sha256',
    'music-upgrade-chain', 'music-upgrade-chain-sha256', 'profile-receipt-sha256', 'profile-report-sha256',
    'profile-inspect-sha256', 'mode', 'preparation-scope', 'output'];
  requireThat(Array.isArray(argv) && argv.length === names.length * 2);
  const result = {};
  for (let index = 0; index < argv.length; index += 2) {
    const name = argv[index]?.slice(2), value = argv[index + 1];
    requireThat(argv[index]?.startsWith('--') && names.includes(name) && !Object.hasOwn(result, name) && typeof value === 'string');
    result[name] = value;
  }
  requireThat(names.every(name => Object.hasOwn(result, name)));
  requireProxyExecutionMode(result.mode, result['preparation-scope']);
  for (const name of names.filter(name => name.endsWith('sha256'))) requireThat(SHA.test(result[name]));
  requireThat(new RegExp(`^${ROOT}/source-attempt-[1-9][0-9]*$`).test(result.source) &&
    new RegExp(`^${ROOT}/client-music-upgrade-chain-[A-Za-z0-9_-]{1,64}\\.json$`).test(result['music-upgrade-chain']) &&
    new RegExp(`^${ROOT}/client-special-features-browser-[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`).test(result.output));
  return result;
}

/** Preparation requires this run's explicit authority before any external effects. */
export function requireProxyExecutionMode(mode, preparationScope = undefined) {
  requireThat(mode === 'acceptance-preparation' && preparationScope === PREPARATION_SCOPE);
}

export function requirePreparationFixture(fixture) {
  requireThat(fixture?.evidence?.schema === 27 && fixture.evidence.schema_binding?.schema === 27 &&
    record(fixture.evidence.profile) && SHA.test(fixture.evidence.profile.receipt_sha256) &&
    ID.test(fixture.items?.positive?.id) && fixture.items.positive.id !== OLD_MOVIE_ITEM &&
    record(fixture.library) && ID.test(fixture.library.id));
}

export function bindCrossUserCredentials(stateFile, aFile, bFile, fixture) {
  const state = stateFile?.value, a = aFile?.value, b = bFile?.value, evidence = fixture?.evidence;
  requireThat(record(state) && record(a) && record(b) && record(evidence) &&
    stateFile.sha256 === evidence.fixture_state_sha256 && aFile.sha256 === evidence.browser_alias_sha256 &&
    bFile.sha256 === state.browser_sha256 && state.marker === MARKER && state.server_id === fixture.serverId &&
    state.added_viewer?.user_id === fixture.accountId && ID.test(state.viewer_id) && ID.test(fixture.accountId) &&
    ID.test(state.admin_id) && new Set([state.admin_id, state.viewer_id, fixture.accountId]).size === 3 &&
    a.marker === MARKER && a.account_marker === AV_MARKER && b.marker === MARKER &&
    a.base_url === ORIGIN && b.base_url === ORIGIN && a.direct_url === DIRECT && b.direct_url === DIRECT &&
    same(a.admin, b.admin) && a.admin?.username === 'm3e-client-administrator' &&
    a.viewer?.username === 'm3e-goby-av-client' && b.viewer?.username === 'm3e-client-viewer' &&
    a.viewer.userId === fixture.accountId && a.viewer.serverId === fixture.serverId &&
    same(Object.keys(b.viewer).sort(), ['password', 'username']) &&
    [a.viewer.password, b.viewer.password].every(value => typeof value === 'string' && /^[0-9a-f]{48}$/.test(value)));
  return [
    { slot: 'A', id: fixture.accountId, username: a.viewer.username, password: a.viewer.password,
      credentialsPath: A_CREDENTIALS, credentialsSHA: aFile.sha256 },
    { slot: 'B', id: state.viewer_id, username: b.viewer.username, password: b.viewer.password,
      credentialsPath: B_CREDENTIALS, credentialsSHA: bFile.sha256 },
  ];
}

export function classifyBrowserRequest(raw, method, resourceType = '', mode = 'acceptance-preparation', itemId = null) {
  try {
    if (mode !== 'acceptance-preparation') return { allow: false, kind: 'invalid_mode' };
    const url = new URL(raw);
    if (['data:', 'blob:'].includes(url.protocol)) return { allow: true, kind: 'non_network' };
    const origin = url.protocol === 'ws:' ? `http://${url.host}` : url.origin;
    if (!['http:', 'ws:'].includes(url.protocol) || origin !== ORIGIN || url.username || url.password || url.hash) {
      return { allow: false, kind: 'external' };
    }
    const route = url.pathname.replace(/^\/emby(?=\/)/i, '');
    if (resourceType === 'media' || /^\/(?:Videos|Audio)\//i.test(route) || /\/Playing(?:\/|$)/i.test(route)) {
      return { allow: false, kind: 'playback' };
    }
    if (/\/PlaybackInfo(?:\/|$)/i.test(route)) return typeof itemId === 'string' && ID.test(itemId) && itemId !== OLD_MOVIE_ITEM && method === 'POST' &&
      new RegExp(`^/Items/${itemId}/PlaybackInfo/?$`, 'i').test(route)
      ? { allow: true, kind: 'preparation' } : { allow: false, kind: 'playback' };
    if (['GET', 'HEAD', 'OPTIONS'].includes(method)) return { allow: true, kind: url.protocol === 'ws:' ? 'websocket' : 'read' };
    if (method === 'POST' && /^\/Users\/AuthenticateByName\/?$/i.test(route)) return { allow: true, kind: 'login' };
    if (method === 'POST' && /^\/Sessions\/Capabilities(?:\/Full)?\/?$/i.test(route)) return { allow: true, kind: 'capabilities' };
    if (method === 'POST' && /^\/Sessions\/Logout\/?$/i.test(route)) return { allow: true, kind: 'logout' };
    return { allow: false, kind: 'state_mutation' };
  } catch { return { allow: false, kind: 'invalid_url' }; }
}

function responseContentType(value) {
  const mime = typeof value === 'string' ? value.split(';', 1)[0].trim().toLowerCase() : '';
  return ['text/html', 'text/css', 'text/javascript', 'application/javascript', 'application/json', 'image/png',
    'image/jpeg', 'image/svg+xml', 'image/webp', 'image/x-icon', 'font/woff', 'font/woff2', 'application/octet-stream'].includes(mime) ? mime : 'other';
}

const PROXY_LIMITS = Object.freeze({ requests: 2000, concurrent: 8, connections: 32,
  requestBytes: 131072, responseBytes: 16 * 1024 * 1024, totalRequestBytes: 4 * 1024 * 1024,
  totalResponseBytes: 128 * 1024 * 1024, headers: 64, headerBytes: 16384, absoluteMs: 20000, idleMs: 5000 });
const PROXY_HOP_HEADERS = new Set(['connection', 'proxy-connection', 'keep-alive', 'transfer-encoding',
  'te', 'trailer', 'upgrade', 'proxy-authenticate', 'proxy-authorization']);

function proxyHeaders(raw, response = false, websocket = false) {
  requireThat(Array.isArray(raw) && raw.length % 2 === 0 && raw.length <= PROXY_LIMITS.headers * 2);
  const values = new Map(), forwarded = [];
  let bytes = 0;
  for (let index = 0; index < raw.length; index += 2) {
    const name = raw[index], value = raw[index + 1];
    requireThat(typeof name === 'string' && /^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,128}$/.test(name) &&
      typeof value === 'string' && !/[\x00-\x1f\x7f-\uffff]/.test(value));
    bytes += Buffer.byteLength(name) + Buffer.byteLength(value) + 4; requireThat(bytes <= PROXY_LIMITS.headerBytes);
    const lower = name.toLowerCase();
    requireThat(!values.has(lower) || response && !['content-length', 'transfer-encoding', 'connection', 'host'].includes(lower));
    values.set(lower, [...(values.get(lower) ?? []), value]);
    if (!PROXY_HOP_HEADERS.has(lower) || websocket && ['connection', 'upgrade'].includes(lower)) forwarded.push(name, value);
  }
  for (const connection of ['connection', 'proxy-connection']) {
    for (const value of values.get(connection) ?? []) requireThat(value.split(',').every(token =>
      (websocket ? ['close', 'keep-alive', 'upgrade'] : ['close', 'keep-alive']).includes(token.trim().toLowerCase())));
  }
  requireThat((websocket || !values.has('upgrade')) && !values.has('trailer') && !values.has('proxy-authorization'));
  const length = values.get('content-length')?.[0];
  requireThat(length === undefined || /^(?:0|[1-9][0-9]{0,9})$/.test(length));
  const transfer = values.get('transfer-encoding')?.[0];
  requireThat(transfer === undefined || response && transfer.toLowerCase() === 'chunked' && length === undefined);
  return { values, forwarded, contentLength: length === undefined ? null : Number(length) };
}

/** Validate one absolute-form browser request before opening any upstream connection. */
export function proxyRequestPlan(raw, method, rawHeaders, mode, itemId) {
  requireThat(typeof raw === 'string' && Buffer.byteLength(raw) <= 16384 && raw.startsWith(ORIGIN + '/') &&
    !raw.includes('\\') && mode === 'acceptance-preparation');
  const url = new URL(raw), policy = classifyBrowserRequest(raw, method, '', mode, itemId);
  requireThat(url.protocol === 'http:' && url.href === raw && policy.allow && policy.kind !== 'websocket');
  const headers = proxyHeaders(rawHeaders);
  requireThat(headers.values.get('host')?.length === 1 && headers.values.get('host')[0] === '127.0.0.1:18196' &&
    !headers.values.has('expect') && !headers.values.has('transfer-encoding') &&
    !(headers.values.get('sec-fetch-dest') ?? []).some(value => ['audio', 'video'].includes(value.toLowerCase())));
  const length = headers.contentLength ?? 0;
  requireThat(length <= PROXY_LIMITS.requestBytes && (method === 'POST' || length === 0));
  if (method === 'POST') requireThat(headers.contentLength !== null);
  return { mode, kind: policy.kind, method, path: raw.slice(ORIGIN.length), contentLength: length,
    headers: [...headers.forwarded, 'Connection', 'close'] };
}

/** Preserve end-to-end response headers and entity bytes; regenerate only hop framing. */
/** A proxy admission consumes a UI write intent once, even if its result is unknown. */
export function reserveProxyRequest(plan, state, intents) {
  requireThat(plan.mode === state.mode && !intents.ownershipLost && state.active < PROXY_LIMITS.concurrent &&
    state.seen <= PROXY_LIMITS.requests && state.requestBytes <= PROXY_LIMITS.totalRequestBytes &&
    state.responseBytes <= PROXY_LIMITS.totalResponseBytes);
  if (plan.kind === 'login') { requireThat(intents.login === true && state.login === 0); state.login += 1; }
  else if (plan.kind === 'logout') { requireThat(intents.logout === true && state.login === 1 && state.logout === 0); state.logout += 1; }
  else if (plan.kind === 'capabilities') { requireThat(intents.login === true && state.capabilities < 16); state.capabilities += 1; }
  else if (plan.kind === 'preparation') {
    requireThat(state.mode === 'acceptance-preparation' && intents.preparation === true && state.preparation === 0);
    state.preparation += 1;
  }
  else requireThat(plan.kind === 'read');
  state.active += 1; state.admitted += 1;
}

/** Fixed-target HTTP forwarding with no redirect following, retries or payload logging. */
export function forwardBrowserHTTP(request, response, policy, transport = http) {
  const state = policy.state;
  const diagnostic = browserRequestDiagnostic(request.url ?? '', 'other', request.method ?? '');
  const classification = classifyBrowserRequest(request.url ?? '', request.method ?? '', '', state.mode, policy.itemId);
  return new Promise(resolve => {
    let plan, admitted = false, settled = false, terminating = false, upstream, incoming, body, outgoingBody, requestBytes = 0, responseBytes = 0;
    const requestChunks = [], responseChunks = [];
    const timer = setTimeout(() => fail('absolute_timeout'), PROXY_LIMITS.absoluteMs);
    const finish = (outcome, reason, status = null) => {
      if (settled) return;
      settled = true; clearTimeout(timer);
      if (admitted) state.active -= 1;
      if (outcome === 'completed') state.completed += 1;
      else if (outcome === 'rejected') state.rejected += 1;
      else state.failed += 1;
      if (incoming && !incoming.complete) incoming.destroy();
      if (upstream && !upstream.destroyed) upstream.destroy();
      for (const chunk of [...requestChunks, ...responseChunks]) chunk.fill(0);
      body?.fill(0); outgoingBody?.fill(0);
      try { policy.onEvent({ ...diagnostic, method: ['GET', 'HEAD', 'OPTIONS', 'POST'].includes(request.method) ? request.method : 'OTHER',
        kind: classification.kind, outcome, reason, status, request_bytes: requestBytes, response_bytes: responseBytes }); }
      catch { state.failed += 1; }
      resolve();
    };
    const fail = (reason, rejected = false) => {
      if (settled || terminating) return;
      terminating = true;
      try {
        if (incoming) incoming.destroy();
        if (upstream) upstream.destroy();
        if (!response.headersSent && !response.destroyed) {
          response.writeHead(rejected ? 403 : 502, { Connection: 'close', 'Content-Length': '0' }); response.end();
        } else if (!response.writableFinished) response.destroy();
      } catch { response.destroy(); }
      finally { finish(rejected ? 'rejected' : 'failed', reason); }
    };
    request.on('error', () => fail('request_error'));
    request.on('aborted', () => fail('request_aborted'));
    response.on('error', () => fail('response_error'));
    response.on('close', () => { if (!response.writableFinished) fail('downstream_closed'); });
    try {
      state.seen += 1;
      plan = proxyRequestPlan(request.url, request.method, request.rawHeaders, state.mode, policy.itemId);
      reserveProxyRequest(plan, state, policy.intents()); admitted = true;
    } catch { fail('admission_rejected', true); return; }
    request.on('data', chunk => {
      if (settled) return;
      requestBytes += chunk.length; state.requestBytes += chunk.length;
      if (requestBytes > plan.contentLength || requestBytes > PROXY_LIMITS.requestBytes || state.requestBytes > PROXY_LIMITS.totalRequestBytes) {
        fail('request_bytes_exceeded'); return;
      }
      requestChunks.push(Buffer.from(chunk));
    });
    request.on('end', () => {
      if (settled) return;
      if (!request.complete || requestBytes !== plan.contentLength || request.rawTrailers?.length || policy.intents().ownershipLost) { fail('request_incomplete'); return; }
      body = Buffer.concat(requestChunks);
      try {
        policy.onRequest?.(request, plan, body);
        upstream = transport.request({ hostname: '127.0.0.1', port: 18196, method: plan.method, path: plan.path,
          agent: false, maxHeaderSize: PROXY_LIMITS.headerBytes, headers: plan.headers }, received => {
          incoming = received;
          if (settled) { incoming.destroy(); return; }
          incoming.on('error', () => fail('upstream_error'));
          incoming.on('aborted', () => fail('upstream_aborted'));
          incoming.on('close', () => { if (!incoming.complete) fail('upstream_incomplete'); });
          let reply;
          try { reply = proxyResponsePlan(incoming.statusCode, incoming.rawHeaders, plan.method); }
          catch { fail('response_headers_rejected'); return; }
          incoming.on('data', chunk => {
            if (settled) return;
            responseBytes += chunk.length; state.responseBytes += chunk.length;
            if (reply.noBody || responseBytes > PROXY_LIMITS.responseBytes || state.responseBytes > PROXY_LIMITS.totalResponseBytes) {
              fail('response_bytes_exceeded'); return;
            }
            responseChunks.push(Buffer.from(chunk));
          });
          incoming.on('end', () => {
            if (settled) return;
            if (!incoming.complete || incoming.rawTrailers?.length || !reply.noBody && reply.contentLength !== null && responseBytes !== reply.contentLength) {
              fail('upstream_incomplete'); return;
            }
            if (policy.intents().ownershipLost) { fail('ownership_lost'); return; }
            outgoingBody = Buffer.concat(responseChunks);
            try {
              policy.onResponse?.(request, plan, reply.status, outgoingBody);
              response.writeHead(reply.status, reply.headers);
              response.end(outgoingBody, error => {
                if (error || !response.writableFinished) fail('downstream_write_failed');
                else finish('completed', null, reply.status);
              });
            } catch { fail('downstream_write_failed'); }
          });
        });
        upstream.maxHeadersCount = 129;
        upstream.setTimeout(PROXY_LIMITS.idleMs, () => fail('upstream_idle_timeout'));
        upstream.on('error', () => fail('upstream_request_error'));
        upstream.on('upgrade', (_reply, socket) => { socket.destroy(); fail('upstream_upgrade_rejected'); });
        upstream.end(body, () => body?.fill(0));
      } catch { fail('upstream_request_failed'); }
    });
  });
}

/** Parse observed authority using the same strict header grammar as the frozen driver. */
function authorizationToken(value) {
  const scheme = /^(?:Emby|MediaBrowser)\s+/i.exec(value);
  requireThat(scheme);
  let position = scheme[0].length, token = null;
  const fields = new Set();
  while (position < value.length) {
    const field = /^([A-Za-z][A-Za-z0-9_-]{0,63})\s*=\s*/.exec(value.slice(position));
    requireThat(field && !fields.has(field[1].toLowerCase()) && fields.size < 16);
    const name = field[1].toLowerCase(); fields.add(name); position += field[0].length;
    let content = '';
    if (value[position] === '"') {
      position += 1; let closed = false;
      while (position < value.length) {
        const character = value[position++];
        if (character === '"') { closed = true; break; }
        if (character === '\\') { requireThat(position < value.length); content += value[position++]; }
        else content += character;
      }
      requireThat(closed);
    } else {
      const end = value.indexOf(',', position);
      content = value.slice(position, end < 0 ? value.length : end).trim();
      position = end < 0 ? value.length : end;
      requireThat(content && !/["\\]/.test(content));
    }
    if (name === 'token') token = content;
    while (value[position] === ' ') position += 1;
    if (position === value.length) break;
    requireThat(value[position++] === ',');
    while (value[position] === ' ') position += 1;
    requireThat(position < value.length);
  }
  return token;
}

function authorityForURL(url, headers, expectedToken = undefined) {
  requireThat(record(headers) && Object.keys(headers).length <= 128);
  const seen = new Set(), tokens = [], sources = [];
  let bytes = 0;
  for (const [name, value] of Object.entries(headers)) {
    const lower = name.toLowerCase();
    requireThat(/^[A-Za-z0-9-]{1,128}$/.test(name) && typeof value === 'string' && !seen.has(lower));
    seen.add(lower); bytes += Buffer.byteLength(name) + Buffer.byteLength(value) + 4; requireThat(bytes <= 16384);
    if (['x-emby-token', 'x-mediabrowser-token'].includes(lower)) { tokens.push(value); sources.push(`header:${lower}`); }
    if (['authorization', 'x-emby-authorization'].includes(lower)) {
      requireThat(!/[\x00-\x1f\x7f-\uffff]/.test(value));
      const token = authorizationToken(value);
      if (token !== null) { tokens.push(token); sources.push(`header:${lower}:token`); }
    }
  }
  const queryNames = new Set(); let count = 0;
  for (const [name, value] of url.searchParams) {
    requireThat(++count <= 128);
    const lower = name.toLowerCase();
    if (!['api_key', 'x-emby-token', 'x-mediabrowser-token'].includes(lower)) continue;
    requireThat(!queryNames.has(lower)); queryNames.add(lower); tokens.push(value); sources.push(`query:${lower}`);
  }
  if (!tokens.length) return null;
  requireThat(tokens.every(value => typeof value === 'string' && value.length > 0 && Buffer.byteLength(value) <= 4096 &&
    !/[\x00-\x20\x7f-\uffff,"\\]/.test(value) && value === tokens[0]));
  if (expectedToken !== undefined) requireThat(tokens[0] === expectedToken);
  return { token: tokens[0], fingerprint: hash(tokens[0]), sources };
}

export function bindOwnedMovieSource(value, expected) {
  requireThat(record(value) && ID.test(expected.id) && expected.id !== OLD_MOVIE_ITEM && value.Id === expected.id && value.Type === 'Movie' &&
    value.Name === expected.name && value.Path === expected.path && Array.isArray(value.MediaSources) && value.MediaSources.length === 1);
  const source = value.MediaSources[0];
  requireThat(record(source) && typeof source.Id === 'string' && /^[A-Za-z0-9_-]{1,256}$/.test(source.Id) &&
    source.ItemId === expected.id && source.Id === expected.media_source_id && source.Path === expected.path &&
    source.Protocol === 'File' && source.IsRemote === false);
  return { item_id: expected.id, source_id: source.Id, path: expected.path };
}

function scopedPreparationObject(bytes) {
  requireThat(Buffer.isBuffer(bytes) && bytes.length > 0 && bytes.length <= PROXY_LIMITS.requestBytes);
  const text = new TextDecoder('utf-8', { fatal: true }).decode(bytes), value = JSON.parse(text);
  requireThat(record(value));
  let depth = 0; const names = new Set();
  for (let index = 0; index < text.length; index += 1) {
    if (text[index] === '{' || text[index] === '[') { depth += 1; requireThat(depth <= 16); }
    else if (text[index] === '}' || text[index] === ']') depth -= 1;
    else if (text[index] === '"') {
      const start = index++;
      while (index < text.length && text[index] !== '"') { if (text[index] === '\\') index += 1; index += 1; }
      let after = index + 1; while (after < text.length && /\s/.test(text[after])) after += 1;
      if (depth === 1 && text[after] === ':') {
        const name = JSON.parse(text.slice(start, index + 1));
        requireThat(/^[\x21-\x7e]{1,128}$/.test(name) && names.size < 64 && !names.has(name.toLowerCase()));
        names.add(name.toLowerCase());
      }
    }
  }
  return Object.fromEntries(Object.entries(value).map(([name, content]) => [name.toLowerCase(), content]));
}

/** Validate the explicitly authorized preparation without rewriting its body. */
export function preparationRequestEvidence(raw, rawHeaders, bytes, scope) {
  requireThat(scope?.mode === 'acceptance-preparation' && scope.phase === 'ui_movie' && scope.ordinary === true &&
    scope.preparationScope === PREPARATION_SCOPE && scope.loginCompleted === true && scope.logoutStarted === false &&
    ID.test(scope.itemId) && scope.itemId !== OLD_MOVIE_ITEM && scope.source?.item_id === scope.itemId && ID.test(scope.userId));
  const url = new URL(raw), requestPolicy = classifyBrowserRequest(raw, 'POST', 'fetch', scope.mode, scope.itemId);
  requireThat(url.origin === ORIGIN && !url.username && !url.password && !url.hash && requestPolicy.kind === 'preparation' && requestPolicy.allow);
  const headers = proxyHeaders(rawHeaders), first = name => headers.values.get(name)?.[0];
  requireThat(['application/json', 'text/plain'].includes((first('content-type') ?? '').split(';', 1)[0].trim().toLowerCase()));
  const authority = authorityForURL(url, Object.fromEntries([...headers.values].map(([name, values]) => [name, values[0]])), scope.token);
  requireThat(authority);
  const body = scopedPreparationObject(bytes), query = new Map();
  for (const [name, value] of url.searchParams) {
    const key = name.toLowerCase(); requireThat(/^[A-Za-z][A-Za-z0-9_-]{0,127}$/.test(name) && !query.has(key)); query.set(key, value);
  }
  const selectors = { userid: scope.userId, id: scope.source.item_id, mediasourceid: scope.source.source_id,
    currentplaysessionid: '', livestreamid: '' };
  for (const [name, expected] of Object.entries(selectors)) {
    const bodyValue = Object.hasOwn(body, name) ? body[name] : null;
    requireThat(bodyValue === null || typeof bodyValue === 'string');
    const present = bodyValue ?? '';
    requireThat(present === '' || present === expected);
    if (query.has(name)) requireThat((query.get(name) === '' || query.get(name) === expected) &&
      (present === '' || present === query.get(name)));
  }
  const result = { item_id: scope.source.item_id, source_id: scope.source.source_id, user_id: scope.userId,
    token_fingerprint: authority.fingerprint, request_bytes: bytes.length, request_sha256: hash(bytes),
    user_id_source: body.userid || query.get('userid') ? 'explicit_request' : 'authenticated_user_default',
    existing_play_or_live_session_requested: false };
  authority.token = null;
  return result;
}

/** Record bounded response facts while the proxy forwards the original status and bytes. */
export function ownAuxiliaryRequest(raw, method, userId, itemId, kind) {
  try {
    if (method !== 'GET' || !ID.test(userId) || !ID.test(itemId) || itemId === OLD_MOVIE_ITEM ||
        !['SpecialFeatures', 'LocalTrailers'].includes(kind)) return false;
    const url = new URL(raw), route = url.pathname.replace(/^\/emby(?=\/)/i, '').replace(/\/$/, '');
    const users = [...url.searchParams].filter(([name]) => name.toLowerCase() === 'userid').map(([, value]) => value);
    return url.origin === ORIGIN && !url.username && !url.password && !url.hash &&
      (route === '/Users/' + userId + '/Items/' + itemId + '/' + kind || route === '/Items/' + itemId + '/' + kind) &&
      (users.length === 0 || users.length === 1 && users[0] === userId);
  } catch { return false; }
}

function expectedAuxiliaryItems(profile, kind) {
  requireThat(['SpecialFeatures', 'LocalTrailers'].includes(kind));
  return (kind === 'SpecialFeatures' ? ['alpha', 'deleted', 'zeta'] : ['trailer']).map(key => profile.items[key]);
}

export function auxiliaryResponseEvidence(status, bytes, profile, kind) {
  const result = { status, response_bytes: Buffer.isBuffer(bytes) ? bytes.length : null,
    json_array: false, item_count: null, item_ids: [], validated: false, reason: 'unexpected_response' };
  if (status !== 200 || !Buffer.isBuffer(bytes) || bytes.length === 0 || bytes.length > LIMIT) return result;
  try {
    const expected = expectedAuxiliaryItems(profile, kind);
    const value = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes));
    result.json_array = Array.isArray(value);
    if (!result.json_array) { result.reason = 'expected_json_array'; return result; }
    result.item_count = value.length;
    requireThat(value.length <= 64);
    const known = new Map(expected.map(item => [item.id, item]));
    result.item_ids = value.filter(item => record(item) && known.has(item.Id)).map(item => item.Id);
    result.validated = value.length === expected.length && new Set(result.item_ids).size === expected.length &&
      value.every(item => {
        const entry = record(item) ? known.get(item.Id) : null;
        return entry && item.Type === entry.api_type && item.Name === entry.api_name &&
          (!Object.hasOwn(item, 'Path') || item.Path === entry.path) &&
          (!Object.hasOwn(item, 'ParentId') || item.ParentId === entry.parent_id);
      });
    result.reason = result.validated ? null : 'profile_items_or_projection_mismatch';
  } catch { result.reason = 'invalid_json_array'; }
  return result;
}

/** Decode only the exact owned auxiliary response after its original client transfer finishes. */
export async function captureAuxiliaryResponse(response, userId, profile, kind) {
  const request = response.request();
  requireThat(request.serviceWorker() === null && ownAuxiliaryRequest(request.url(), request.method(), userId, profile.items.positive.id, kind));
  const status = response.status();
  if (status !== 200) return auxiliaryResponseEvidence(status, null, profile, kind);
  const headers = response.headers(), length = headers['content-length'];
  requireThat(length === undefined || /^(?:0|[1-9][0-9]{0,9})$/.test(length) && Number(length) <= LIMIT);
  requireThat(responseContentType(headers['content-type']) === 'application/json');
  return bounded((async () => {
    requireThat(await response.finished() === null);
    let bytes;
    try {
      bytes = await response.body();
      return auxiliaryResponseEvidence(status, bytes, profile, kind);
    } finally { if (Buffer.isBuffer(bytes)) bytes.fill(0); }
  })(), 5000);
}

export function auxiliaryEvidence(requests, userId, tokenFingerprint, profile, kind) {
  const selected = Array.isArray(requests) ? requests.filter(entry => entry.own_auxiliary === true && entry.auxiliary_kind === kind) : [];
  const expected = expectedAuxiliaryItems(profile, kind);
  const bound = typeof userId === 'string' && ID.test(userId) && typeof tokenFingerprint === 'string' && SHA.test(tokenFingerprint);
  const valid = entry => bound && entry.method === 'GET' && entry.phase === 'ui_movie' && entry.frame_owned === true &&
    entry.auxiliary_user_id === userId && entry.auxiliary_item_id === profile.items.positive.id &&
    Number.isSafeInteger(entry.index) && entry.index >= 0 && entry.index < 2000 && entry.status === 200 &&
    entry.finished === true && entry.failed === false && entry.token_matches_session === true &&
    entry.token_fingerprint === tokenFingerprint && entry.auxiliary_response?.status === 200 &&
    entry.auxiliary_response.validated === true && entry.auxiliary_response.json_array === true &&
    entry.auxiliary_response.item_count === expected.length && entry.auxiliary_response.reason === null &&
    Array.isArray(entry.auxiliary_response.item_ids) &&
    same([...entry.auxiliary_response.item_ids].sort(), expected.map(item => item.id).sort()) &&
    Number.isSafeInteger(entry.auxiliary_response.response_bytes) && entry.auxiliary_response.response_bytes > 0 &&
    entry.auxiliary_response.response_bytes <= LIMIT;
  const completed = selected.filter(valid), validated = bound && selected.length >= 1 && selected.length <= 4 &&
    completed.length === selected.length && new Set(selected.map(entry => entry.index)).size === selected.length;
  return { source: 'original_client_response', endpoint: kind, user_id: bound ? userId : null, item_id: profile.items.positive.id,
    token_fingerprint: bound ? tokenFingerprint : null, observed_requests: selected.length, completed_requests: completed.length,
    original_ui_request_indexes: selected.slice(0, 4).map(entry => Number.isSafeInteger(entry.index) ? entry.index : null),
    validated, result: selected.length === 0 ? 'not_observed' : validated ? 'passed' : 'failed',
    expected_item_ids: expected.map(item => item.id), client_evidence: validated };
}

export function comparePositiveState(before, after) {
  requireThat(record(before) && record(after) && before.user_id === after.user_id && before.items.length === 10 && after.items.length === 10 &&
    new Set(before.items.map(item => item.id)).size === 10);
  return { items_unchanged: same(before.items, after.items), preferences_unchanged: same(before.preferences, after.preferences),
    configuration_unchanged: same(before.configuration, after.configuration), policy_unchanged: same(before.policy, after.policy) };
}
async function privateDirectory(filename) {
  const info = await fs.lstat(filename);
  requireThat(info.isDirectory() && !info.isSymbolicLink() && info.uid === 0 && info.gid === 0 &&
    (info.mode & 0o777) === 0o700 && await fs.realpath(filename) === filename);
}
async function writePrivate(filename, encoded) {
  const file = await fs.open(filename, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
  try { await file.writeFile(encoded); await file.sync(); } finally { await file.close(); }
}
async function readOwnedFile(filename, modes = [0o600], parse = true) {
  requireThat(await fs.realpath(filename) === filename);
  const before = await fs.lstat(filename, { bigint: true });
  const unchanged = value => ['dev', 'ino', 'uid', 'gid', 'mode', 'nlink', 'size', 'mtimeNs', 'ctimeNs'].every(key => before[key] === value[key]);
  requireThat(before.isFile() && !before.isSymbolicLink() && before.uid === 0n && before.gid === 0n && before.nlink === 1n &&
    modes.includes(Number(before.mode & 0o777n)) && before.size > 0n && before.size <= BigInt(LIMIT));
  const file = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW), buffer = Buffer.alloc(LIMIT + 1);
  try {
    requireThat(unchanged(await file.stat({ bigint: true })));
    let size = 0;
    while (size < buffer.length) { const { bytesRead } = await file.read(buffer, size, buffer.length - size, size); if (!bytesRead) break; size += bytesRead; }
    requireThat(size === Number(before.size) && unchanged(await file.stat({ bigint: true })) && unchanged(await fs.lstat(filename, { bigint: true })));
    const raw = buffer.subarray(0, size);
    return { value: parse ? JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(raw)) : null, sha256: hash(raw) };
  } finally { buffer.fill(0); await file.close(); }
}

function ownRequest(raw, userId, itemId) {
  try {
    const url = new URL(raw), route = url.pathname.replace(/^\/emby(?=\/)/i, '');
    if (url.origin !== ORIGIN) return false;
    if (route === `/Users/${userId}/Items/${itemId}`) return true;
    const users = [...url.searchParams].filter(([key]) => key.toLowerCase() === 'userid').map(([, value]) => value);
    return route === `/Items/${itemId}` && (users.length === 0 || users.length === 1 && users[0] === userId);
  } catch { return false; }
}

function stateEvidence(value) {
  return { user_id: value.user_id, items: value.items.map(item => ({ id: item.id, user_data_sha256: digest(item.user_data) })),
    preferences_sha256: digest(value.preferences), configuration_sha256: digest(value.configuration), policy_sha256: digest(value.policy) };
}

export function positiveSpecialFeaturesResult(report) {
  const profile = { items: report.profile_items, library: report.profile_library };
  if (report.format !== 6 || report.mode !== 'acceptance-preparation' || report.preparation_scope !== PREPARATION_SCOPE ||
      report.fixture?.schema !== 27 || !record(report.fixture.profile) || !record(profile.items) ||
      !['positive', 'alpha', 'deleted', 'zeta', 'trailer'].every(key => ID.test(profile.items[key]?.id)) ||
      profile.items.positive.id === OLD_MOVIE_ITEM || report.failure !== null || report.accounts?.length !== 2 ||
      report.accounts[0].id === report.accounts[1].id) return false;
  return report.accounts.every((actor, index) => ID.test(actor.id) && SHA.test(actor.token_fingerprint) &&
    actor.login?.status === 200 && actor.login.request_count === 1 && actor.principal_confirmed === true &&
    actor.ordinary_authority_confirmed === true && actor.ui?.outcome === 'passed' && actor.page_error_count === 0 &&
    actor.own_item_reads === 20 && actor.foreign_read?.status === 403 && actor.foreign_read.error_code === 'access_denied' &&
    actor.foreign_read.target_user_id === report.accounts[1-index].id && record(actor.comparison) &&
    same(Object.keys(actor.comparison).sort(), ['items_unchanged', 'preferences_unchanged', 'configuration_unchanged', 'policy_unchanged'].sort()) &&
    Object.values(actor.comparison).every(value => value === true) &&
    actor.logout?.status === 204 && actor.logout.login_view_visible === true && actor.closed === true &&
    actor.session_proof?.outcome === 'all_observed_logout_tokens_rejected' && actor.session_proof.entries.length === 1 &&
    actor.session_proof.entries[0].ui_request.response_status === 204 && actor.session_proof.entries[0].verification.status === 401 &&
    actor.session_proof.entries[0].token_fingerprint === actor.token_fingerprint &&
    actor.proxy?.mode === report.mode && actor.proxy.login === 1 && actor.proxy.logout === 1 && actor.proxy.active === 0 &&
    actor.proxy_login_status === 200 && actor.proxy.seen === actor.proxy.admitted && actor.proxy.admitted === actor.proxy.completed &&
    actor.proxy.failed === 0 && actor.proxy.rejected === 0 && actor.proxy.upgrade_rejections === 0 &&
    actor.proxy_logout?.completed === true && actor.proxy_logout.status === 204 && actor.proxy_logout.token_fingerprint === actor.token_fingerprint &&
    actor.proxy.preparation === 1 && actor.preparation?.request_validated === true && actor.preparation.item_id === profile.items.positive.id &&
    actor.preparation.user_id === actor.id && actor.preparation.token_fingerprint === actor.token_fingerprint &&
    actor.preparation.completed === true && actor.preparation.response?.status === 200 && actor.preparation.response.validated === true &&
    actor.preparation.ui_status === 200 && actor.preparation.ui_finished === true &&
    actor.special_features?.validated === true &&
    same(actor.special_features, auxiliaryEvidence(actor.requests, actor.id, actor.token_fingerprint, profile, 'SpecialFeatures')) &&
    same(actor.local_trailers, auxiliaryEvidence(actor.requests, actor.id, actor.token_fingerprint, profile, 'LocalTrailers')) &&
    (actor.local_trailers.result === 'not_observed' || actor.local_trailers.validated === true) &&
    Array.isArray(actor.ui.special_features_cards) && actor.ui.special_features_cards.length === 3 &&
    same(actor.ui.special_features_cards.map(card => card.item_id).sort(), ['alpha', 'deleted', 'zeta'].map(key => profile.items[key].id).sort()) &&
    actor.ui.special_features_cards.every(card => card.visible === true && card.title_matches === true &&
      card.card_present === true && card.in_viewport === true &&
      (card.card_id_present === true && card.id_matches_when_present === true ||
        card.card_id_present === false && card.id_matches_when_present === null)) &&
    actor.websocket?.opened >= 1 && actor.websocket.opened <= WS_LIMITS.handshakes && actor.websocket.closed === actor.websocket.opened &&
    actor.websocket.seen === actor.websocket.admitted && actor.websocket.admitted === actor.websocket.opened &&
    actor.websocket.active === 0 && actor.websocket.failed === 0 && actor.websocket.control_attempts === 0 &&
    actor.websocket.entries.length === actor.websocket.seen && actor.websocket.entries.every(entry =>
      entry.outcome === 'closed' && entry.handshake_delivered === true && entry.upstream_status === 101 &&
      (!Object.hasOwn(entry, 'connect_transport') || entry.connect_transport === true && entry.proxy_connect_status === 200) &&
      entry.token_fingerprint === actor.token_fingerprint && entry.server_control_attempted === false) &&
    actor.network?.forbidden_mutations === 0 && actor.network.playback_attempts === 0 && actor.network.observer_errors === 0 &&
    actor.network.overflow === 0 && actor.network.guard_errors === 0) &&
    report.accounts[0].token_fingerprint !== report.accounts[1].token_fingerprint;
}
class BrowserActor {
  constructor(account, pin, report, profile) {
    requirePreparationFixture(profile); this.profile = profile;
    this.mode = 'acceptance-preparation'; this.preparationScope = PREPARATION_SCOPE;
    this.account = account; this.pin = pin; this.report = report; this.token = null; this.proven = false;
    this.diagnosticSecrets = () => [this.account.password, this.token]; this.rememberSecret = () => {};
    this.pending = new Set(); this.entries = new WeakMap(); this.sockets = new Set(); this.closed = false;
    this.loginIntent = false; this.logoutIntent = false; this.ownershipLost = false; this.phase = 'not_started';
    this.started = Date.now(); this.preloginOnly = false;
    this.loginCompleted = new Promise(resolve => { this.confirmLoginResponse = resolve; });
    Object.assign(report, { slot: account.slot, id: account.id, credentials_sha256: account.credentialsSHA,
      login: { status: null, request_count: 0, attempted: false, credentials_filled: false }, logout: { status: null, login_view_visible: false },
      own_item_reads: 0, requests: [], ui: { outcome: 'not_started', observations: [] }, closed: false,
      network: { external_blocked: 0, forbidden_mutations: 0, playback_attempts: 0, observer_errors: 0,
        overflow: 0, guard_errors: 0 }, page_error_count: 0, page_errors: [], console_diagnostics: [], console_warning_error_count: 0,
      bootstrap_diagnostics: [], bootstrap_capture_errors: [] });
  }
  async bootstrapDiagnostic(label) {
    if (!this.page || this.report.bootstrap_diagnostics.length >= 8) return;
    requireThat(['after_navigation', 'login_entry_ready', 'login_form_ready', 'before_credentials', 'open_failure'].includes(label));
    try {
      const raw = await bounded(this.page.evaluate(async () => {
        const visible = element => Boolean(element.getClientRects().length) && getComputedStyle(element).visibility !== 'hidden';
        const elements = selector => [...document.querySelectorAll(selector)].filter(visible);
        const text = (document.body?.innerText ?? '').slice(0, 1000000), title = document.title.trim().toLowerCase();
        const buttons = elements('button,[role="button"]'), links = elements('a');
        const label = element => (element.getAttribute('aria-label') ?? element.innerText ?? '').trim().toLowerCase();
        const scripts = [...document.scripts];
        const service = { available: 'serviceWorker' in navigator, controller_present: false, registration_count: null, query_failed: false };
        if (service.available) {
          let timer;
          try {
            service.controller_present = Boolean(navigator.serviceWorker.controller);
            service.registration_count = (await Promise.race([navigator.serviceWorker.getRegistrations(),
              new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('service_worker_query_timeout')), 500); })])).length;
          } catch { service.query_failed = true; }
          finally { clearTimeout(timer); }
        }
        return { ready_state: document.readyState, title_kind: !title ? 'empty' : title.includes('emby') ? 'emby' : title.includes('goby') ? 'goby' : 'other',
          body_present: Boolean(document.body), body_character_count: text.length,
          visible: { forms: elements('form').length, password_forms: elements('form').filter(form =>
            [...form.querySelectorAll('input[type="password"]')].some(visible)).length,
          text_inputs: elements('input[type="text"]').length, password_inputs: elements('input[type="password"]').length,
          buttons: buttons.length, links: links.length,
          sign_in_buttons: buttons.filter(element => label(element) === 'sign in').length,
          manual_login_controls: [...buttons, ...links].filter(element => label(element) === 'manual login').length },
          milestones: { manual_login: /\bmanual login\b/i.test(text), sign_in: /\bsign in\b/i.test(text),
            select_server: /\bselect (?:a )?server\b/i.test(text), connect_to_server: /\bconnect (?:to )?(?:a )?server\b/i.test(text),
            loading: /\bloading\b|\bplease wait\b/i.test(text), connection_error: /unable to connect|connection (?:failed|failure)|server (?:unavailable|not found)/i.test(text) },
          scripts: { total: scripts.length, external: scripts.filter(script => script.hasAttribute('src')).length,
            module: scripts.filter(script => script.type === 'module').length }, service_worker: service };
      }), 4000);
      this.report.bootstrap_diagnostics.push({ label, phase: this.phase, elapsed_ms: Date.now() - this.started,
        requests_observed: this.report.requests.length, ...sanitizeBootstrapDOM(raw) });
    } catch (error) {
      if (this.report.bootstrap_capture_errors.length < 8) this.report.bootstrap_capture_errors.push({ label,
        ...safeBrowserFailure(error, this.phase), elapsed_ms: Date.now() - this.started });
    }
  }
  async assertPinned() {
    requireThat(!this.ownershipLost);
    try { await this.pin(); }
    catch {
      this.ownershipLost = true;
      if (this.context) await this.context.setOffline(true).catch(() => {});
      throw new Error('cross_user_target_changed');
    }
  }
  deny(kind) {
    const key = kind === 'external' ? 'external_blocked' : kind === 'playback' ? 'playback_attempts' : 'forbidden_mutations';
    this.report.network[key] += 1;
  }
  preparationAllowed() {
    return this.mode === 'acceptance-preparation' && this.preparationScope === PREPARATION_SCOPE && this.phase === 'ui_movie' && this.proven &&
      this.report.ordinary_authority_confirmed === true && this.report.proxy_login_status === 200 && !this.logoutIntent &&
      this.movie?.id === this.profile.items.positive.id && this.preparationSource?.item_id === this.profile.items.positive.id && typeof this.token === 'string';
  }
  async guardProxy() {
    this.proxyPending = new Set(); this.websocketPending = new Set(); this.socketGuards = new Map();
    this.report.proxy_requests = [];
    this.report.proxy = { mode: this.mode, seen: 0, admitted: 0, completed: 0,
      rejected: 0, failed: 0, active: 0, login: 0, capabilities: 0, logout: 0, preparation: 0, requestBytes: 0, responseBytes: 0, upgrade_rejections: 0 };
    this.report.websocket = { seen: 0, admitted: 0, opened: 0, closed: 0, failed: 0, active: 0, control_attempts: 0, entries: [] };
    const policy = { state: this.report.proxy, itemId: this.profile.items.positive.id,
      intents: () => ({ login: this.loginIntent, logout: this.logoutIntent, ownershipLost: this.ownershipLost, preparation: this.preparationAllowed() }),
      onRequest: (request, plan, body) => {
        if (plan.kind === 'preparation') {
          requireThat(this.preparationAllowed() && !this.report.preparation);
          this.report.preparation = { request_validated: false, completed: false, response: null, ui_status: null, ui_finished: false };
          const evidence = preparationRequestEvidence(request.url, request.rawHeaders, body, { mode: this.mode, phase: this.phase,
            preparationScope: this.preparationScope, itemId: this.profile.items.positive.id,
            userId: this.account.id, token: this.token, ordinary: this.proven && this.report.ordinary_authority_confirmed === true,
            loginCompleted: this.report.proxy_login_status === 200, logoutStarted: this.logoutIntent, source: this.preparationSource });
          Object.assign(this.report.preparation, evidence, { request_validated: true }); return;
        }
        if (plan.kind !== 'logout') return;
        requireThat(this.proven && this.logoutIntent && typeof this.token === 'string' && !this.report.proxy_logout);
        const headers = proxyHeaders(request.rawHeaders), actual = Object.fromEntries([...headers.values].map(([name, values]) => [name, values[0]]));
        const authority = authorityForURL(new URL(request.url), actual, this.token); requireThat(authority);
        this.report.proxy_logout = { source: 'physical-http-forwarding-proxy-request', token_fingerprint: authority.fingerprint,
          status: null, completed: false };
        authority.token = null;
      },
      onResponse: (_request, plan, status, body) => {
        if (plan.kind === 'preparation' && this.report.preparation) {
          this.report.preparation.response = preparationResponseEvidence(status, body, this.preparationSource);
        }
      },
      onEvent: event => {
        this.report.login.request_count = this.report.proxy.login;
        if (event.kind === 'login') {
          this.report.proxy_login_status = event.outcome === 'completed' ? event.status : null;
          this.confirmLoginResponse();
        }
        if (event.kind === 'logout' && this.report.proxy_logout) {
          this.report.proxy_logout.status = event.status; this.report.proxy_logout.completed = event.outcome === 'completed';
        }
        if (event.kind === 'preparation' && event.outcome === 'completed' && this.report.preparation) this.report.preparation.completed = true;
        if (this.report.proxy_requests.length < PROXY_LIMITS.requests) this.report.proxy_requests.push({ ...event, elapsed_ms: Date.now() - this.started });
        else this.report.network.overflow += 1;
        if (event.outcome === 'rejected') this.deny(event.kind);
        else if (event.outcome !== 'completed') this.report.network.guard_errors += 1;
      } };
    const denyUpgrade = (_request, socket) => {
      this.report.proxy.upgrade_rejections += 1; this.deny('state_mutation');
      socket.end('HTTP/1.1 403 Forbidden\r\nConnection: close\r\nContent-Length: 0\r\n\r\n');
    };
    this.guard = http.createServer({ maxHeaderSize: PROXY_LIMITS.headerBytes }, (request, response) => {
      const pending = forwardBrowserHTTP(request, response, policy);
      this.proxyPending.add(pending); pending.finally(() => this.proxyPending.delete(pending));
    });
    this.guard.maxConnections = PROXY_LIMITS.connections; this.guard.maxHeadersCount = 129;
    this.guard.maxRequestsPerSocket = 1;
    this.guard.headersTimeout = 5000; this.guard.requestTimeout = 5000;
    const startWebSocket = (request, socket, head, connect = false) => {
      if (this.preloginOnly) { denyUpgrade(request, socket); return; }
      if (this.report.websocket.entries.length >= WS_LIMITS.handshakes + 1) {
        this.report.network.overflow += 1; socket.destroy(); return;
      }
      const control = this.socketGuards.get(socket);
      if (control) { clearTimeout(control.deadline); socket.off('timeout', control.idle); this.socketGuards.delete(socket); socket.setTimeout(0); }
      const entry = {}; this.report.websocket.entries.push(entry);
      const socketPolicy = { mode: this.mode, userId: this.account.id,
        state: this.report.websocket, entry, ownershipLost: () => this.ownershipLost,
        logoutInProgress: () => this.logoutIntent && this.report.proxy.logout === 1,
        connectAllowed: async () => {
          await bounded(this.loginCompleted, 5000);
          requireThat(this.loginIntent && this.report.proxy_login_status === 200 && this.report.proxy.login === 1 &&
            !this.logoutIntent && !this.ownershipLost);
        },
        authorize: async token => {
          this.rememberSecret(token);
          await bounded(this.loginCompleted, 5000);
          requireThat(this.loginIntent && this.report.proxy_login_status === 200 && this.report.proxy.login === 1 &&
            !this.logoutIntent && typeof this.authorizeSocket === 'function' && (this.token === null || this.token === token));
          this.token = token; this.report.token_fingerprint = hash(token);
          await this.authorizeSocket();
        },
        onEvent: value => { if (value.outcome !== 'closed') this.report.network.guard_errors += 1; },
      };
      const pending = connect ? forwardWebSocketConnect(request, socket, head, socketPolicy)
        : forwardBrowserWebSocket(request, socket, head, socketPolicy);
      this.websocketPending.add(pending); pending.finally(() => this.websocketPending.delete(pending));
    };
    this.guard.on('connect', (request, socket, head) => startWebSocket(request, socket, head, true));
    this.guard.on('upgrade', (request, socket, head) => startWebSocket(request, socket, head));
    const denyExpectation = (_request, response) => {
      this.report.network.guard_errors += 1; response.writeHead(417, { Connection: 'close', 'Content-Length': '0' }); response.end();
    };
    this.guard.on('checkContinue', denyExpectation); this.guard.on('checkExpectation', denyExpectation);
    this.guard.on('drop', () => { this.report.network.overflow += 1; });
    this.guard.on('clientError', (_error, socket) => { this.report.network.guard_errors += 1; socket.destroy(); });
    this.guard.on('connection', socket => {
      const deadline = setTimeout(() => { this.report.network.guard_errors += 1; socket.destroy(); }, PROXY_LIMITS.absoluteMs);
      const idle = () => { this.report.network.guard_errors += 1; socket.destroy(); };
      this.socketGuards.set(socket, { deadline, idle });
      this.sockets.add(socket); socket.on('error', () => {});
      socket.on('close', () => { clearTimeout(deadline); this.sockets.delete(socket); this.socketGuards.delete(socket); });
      socket.setTimeout(PROXY_LIMITS.idleMs, idle);
    });
    await bounded(new Promise((resolve, reject) => {
      this.guard.once('error', reject); this.guard.listen(0, '127.0.0.1', resolve);
    }), 5000);
    return `http://127.0.0.1:${this.guard.address().port}`;
  }
  observe(request) {
    try {
      const classification = classifyBrowserRequest(request.url(), request.method(), request.resourceType(), this.mode, this.profile.items.positive.id);
      if (this.report.requests.length >= 2000) { this.report.network.overflow += 1; return; }
      const auxiliaryKind = this.movie && request.serviceWorker() === null ? ['SpecialFeatures', 'LocalTrailers'].find(kind =>
        ownAuxiliaryRequest(request.url(), request.method(), this.account.id, this.movie.id, kind)) : null;
      const entry = { index: this.report.requests.length, phase: this.phase, kind: classification.kind,
        method: ['GET', 'HEAD', 'OPTIONS', 'POST'].includes(request.method()) ? request.method() : 'OTHER',
        status: null, finished: false, failed: false, elapsed_ms: Date.now() - this.started,
        ...browserRequestDiagnostic(request.url(), request.resourceType(), request.method()),
        owned_preparation: Boolean(this.movie?.id === this.profile.items.positive.id && request.method() === 'POST' && classification.kind === 'preparation'),
        own_auxiliary: Boolean(auxiliaryKind),
        ...(auxiliaryKind ? { auxiliary_kind: auxiliaryKind, frame_owned: true, auxiliary_user_id: this.account.id, auxiliary_item_id: this.movie.id } : {}),
        own_movie: Boolean(this.movie && request.method() === 'GET' && ownRequest(request.url(), this.account.id, this.movie.id)) };
      this.report.requests.push(entry); this.entries.set(request, entry);
      if (!entry.owned_preparation && (request.method() !== 'GET' || classification.kind !== 'read' || !this.loginIntent)) return;
      const url = new URL(request.url()), route = url.pathname.replace(/^\/emby(?=\/)/i, '');
      const explicitUser = route === `/Users/${this.account.id}` || route.startsWith(`/Users/${this.account.id}/`) || route === `/UserSettings/${this.account.id}`;
      const boundItem = entry.own_movie && this.proven && typeof this.token === 'string';
      if (!entry.owned_preparation && !entry.own_auxiliary && !explicitUser && !boundItem) return;
      if (this.pending.size >= 16) { this.report.network.observer_errors += 1; return; }
      const capture = (async () => {
        const headers = await bounded(request.allHeaders(), 1500);
        const authority = entry.owned_preparation || entry.own_auxiliary ? authorityForURL(url, headers, this.token)
          : observedAuthority(request.url(), headers, this.account.id, boundItem ? this.token : undefined);
        if (!authority) return;
        this.rememberSecret(authority.token);
        requireThat(this.token === null || this.token === authority.token);
        this.token = authority.token; this.report.token_fingerprint = authority.fingerprint;
        this.report.token_sources = authority.sources; entry.token_matches_session = true;
        if (entry.own_auxiliary) entry.token_fingerprint = authority.fingerprint;
      })().catch(() => { this.report.network.observer_errors += 1; });
      this.pending.add(capture); capture.finally(() => this.pending.delete(capture));
    } catch { this.report.network.observer_errors += 1; }
  }
  async settled() {
    await bounded((async () => { while (this.pending.size) await Promise.all([...this.pending]); })(), 5000);
    requireThat(this.report.network.observer_errors === 0 && this.report.network.overflow === 0);
  }
  async proxyIdle() {
    await bounded((async () => {
      let stable = 0;
      while (stable < 2) {
        stable = this.report.proxy.active === 0 && this.proxyPending.size === 0 ? stable + 1 : 0;
        await this.page.waitForTimeout(100);
      }
    })(), 25000);
  }
  async open({ preloginOnly = false } = {}) {
    requireThat(typeof preloginOnly === 'boolean' && preloginOnly === (this.mode === 'prelogin')); this.preloginOnly = preloginOnly;
    try {
    this.phase = 'browser_start'; await this.assertPinned();
    const require = createRequire(import.meta.url), { chromium } = require(PLAYWRIGHT);
    this.report.playwright_version = require(path.join(PLAYWRIGHT, 'package.json')).version;
    const guard = await this.guardProxy();
    this.browser = await chromium.launch({ headless: true, timeout: 30000,
      args: ['--no-sandbox', '--disable-dev-shm-usage', '--disable-background-networking'] });
    this.report.browser_version = this.browser.version();
    this.phase = 'browser_context';
    this.context = await this.browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', serviceWorkers: 'allow',
      proxy: { server: guard, bypass: '<-loopback>' } });
    let requests = 0;
    await this.context.route('**/*', async route => {
      try {
        const request = route.request(), policy = classifyBrowserRequest(request.url(), request.method(), request.resourceType(),
          this.mode, this.profile.items.positive.id);
        if (this.ownershipLost || ++requests > 2000) {
          if (!this.ownershipLost) this.report.network.overflow += 1;
          await route.abort('blockedbyclient'); return;
        }
        if (!policy.allow) { this.deny(policy.kind); await route.abort('blockedbyclient'); return; }
        if (policy.kind === 'websocket' && this.preloginOnly || policy.kind === 'login' && !this.loginIntent ||
            policy.kind === 'logout' && !this.logoutIntent || policy.kind === 'capabilities' && !this.loginIntent ||
            policy.kind === 'preparation' && !this.preparationAllowed()) {
          this.deny('state_mutation'); await route.abort('blockedbyclient'); return;
        }
        await route.continue();
      } catch { this.report.network.guard_errors += 1; await route.abort('blockedbyclient').catch(() => {}); }
    });
    this.proof = createBrowserSessionProof({ context: frameRequestContext(this.context, () => { this.report.network.observer_errors += 1; }), target: ORIGIN });
    this.report.session_proof = this.proof.report;
    this.report.session_proof_event_scope = 'Original frame-owned request events; separate physical proxy logout evidence must agree';
    this.context.on('request', request => this.observe(request));
    this.context.on('response', response => {
      try {
        const entry = this.entries.get(response.request());
        if (entry) Object.assign(entry, { status: response.status(), response_content_type: responseContentType(response.headers()['content-type']),
          from_service_worker: response.fromServiceWorker(), response_elapsed_ms: Date.now() - this.started });
        if (entry?.own_auxiliary) {
          requireThat(this.proven && this.report.ordinary_authority_confirmed === true && this.movie?.id === this.profile.items.positive.id &&
            this.report.requests.filter(value => value.auxiliary_kind === entry.auxiliary_kind).length <= 4 && this.pending.size < 16);
          const capture = captureAuxiliaryResponse(response, this.account.id, this.profile, entry.auxiliary_kind)
            .then(evidence => { entry.auxiliary_response = evidence; })
            .catch(() => {
              entry.auxiliary_response = { validated: false, reason: 'response_observation_failed' };
              this.report.network.observer_errors += 1;
            });
          this.pending.add(capture); capture.finally(() => this.pending.delete(capture));
        }
      } catch { this.report.network.observer_errors += 1; }
    });
    this.context.on('requestfinished', request => {
      try { const entry = this.entries.get(request); if (entry) Object.assign(entry, { finished: true, finished_elapsed_ms: Date.now() - this.started }); }
      catch { this.report.network.observer_errors += 1; }
    });
    this.context.on('requestfailed', request => {
      try {
        const entry = this.entries.get(request);
        if (entry) Object.assign(entry, { failed: true, failure_elapsed_ms: Date.now() - this.started,
          failure_class: safeBrowserFailure({ message: request.failure()?.errorText ?? '' }, this.phase).category });
      } catch { this.report.network.observer_errors += 1; }
    });
    this.phase = 'new_page';
    this.page = await this.context.newPage();
    this.page.on('pageerror', error => recordBrowserPageError(this.report, error, this.phase,
      Date.now() - this.started, this.diagnosticSecrets()));
    this.page.on('console', message => {
      let type;
      try { type = message.type(); } catch { this.report.network.observer_errors += 1; return; }
      if (!['error', 'warning'].includes(type)) return;
      this.report.console_warning_error_count += 1;
      if (this.report.console_diagnostics.length >= DIAGNOSTIC_ENTRY_LIMIT) return;
      let text;
      try { text = message.text(); } catch { /* Keep a fixed unavailable diagnostic without inspecting other properties. */ }
      this.report.console_diagnostics.push({ type,
        ...browserEventDiagnostic({ name: 'Error', message: text }, this.phase, this.diagnosticSecrets()),
        elapsed_ms: Date.now() - this.started });
    });
    this.phase = 'navigation';
    const navigation = await this.page.goto(`${ORIGIN}/web/index.html`, { waitUntil: 'domcontentloaded', timeout: 30000 });
    this.report.navigation = { status: navigation?.status() ?? null, result: 'domcontentloaded', elapsed_ms: Date.now() - this.started };
    await this.bootstrapDiagnostic('after_navigation');
    this.phase = 'login_entry_wait';
    const form = this.page.locator('form:has(input[type="password"]:visible)');
    const manual = this.page.getByText('Manual Login', { exact: true });
    await Promise.any([form.waitFor({ state: 'visible', timeout: 15000 }), manual.waitFor({ state: 'visible', timeout: 15000 })]);
    this.phase = 'login_entry_ready'; await this.bootstrapDiagnostic('login_entry_ready');
    if (this.preloginOnly) {
      this.phase = 'prelogin_complete'; this.report.prelogin = { ready: true, credentials_submitted: false,
        expected_account_only: true, service_workers: 'allow', all_http_via_proxy: true, client_acceptance: false };
      await this.assertPinned(); await this.proxyIdle(); return;
    }
    if (!await form.isVisible()) { this.phase = 'manual_login_click'; await manual.locator('xpath=..').getByRole('button').click({ timeout: 8000 }); }
    this.phase = 'login_form_wait';
    await form.waitFor({ state: 'visible', timeout: 10000 });
    await this.bootstrapDiagnostic('login_form_ready'); this.phase = 'login_controls';
    const username = form.locator('input[type="text"]:visible'), password = form.locator('input[type="password"]:visible');
    const submit = form.getByRole('button', { name: 'Sign In', exact: true });
    requireThat(await username.count() === 1 && await password.count() === 1 && await submit.count() === 1);
    await this.bootstrapDiagnostic('before_credentials'); this.phase = 'credential_fill';
    await username.fill(this.account.username); await password.fill(this.account.password); this.report.login.credentials_filled = true;
    this.phase = 'login_pin'; await this.assertPinned();
    const observed = this.page.waitForResponse(response => classifyBrowserRequest(response.url(), response.request().method()).kind === 'login',
      { timeout: 15000 }).catch(() => null);
    this.loginIntent = true; this.report.login.attempted = true;
    this.phase = 'login_submit';
    await submit.click({ timeout: 10000 });
    this.phase = 'login_response';
    const response = await observed; this.report.login.status = response?.status() ?? null;
    requireThat(response?.status() === 200);
    this.phase = 'login_form_closed';
    await password.waitFor({ state: 'hidden', timeout: 15000 });
    this.phase = 'token_capture';
    const until = Date.now() + 15000;
    while (!this.token && Date.now() < until) { await this.page.waitForTimeout(100); await this.settled(); }
    requireThat(this.token && this.report.login.request_count === 1);
    await this.assertPinned(); this.phase = 'authenticated';
    } catch (error) {
      this.report.failure = { ...safeBrowserFailure(error, this.phase), elapsed_ms: Date.now() - this.started };
      if (this.preloginOnly) this.report.prelogin = { ready: false, credentials_submitted: false,
        expected_account_only: true, service_workers: 'allow', all_http_via_proxy: true, client_acceptance: false };
      await this.bootstrapDiagnostic('open_failure');
      throw new Error('cross_user_open_failed');
    }
  }
  async inactiveMedia(label) {
    const active = await this.page.locator('audio,video').evaluateAll(elements => elements.some(element =>
      !element.paused && !element.ended || element.currentTime > 0));
    this.report.ui.observations.push({ label, media_inactive: !active }); requireThat(!active);
  }
  async home(label) {
    const expected = 'Latest ' + this.profile.library.name;
    const heading = this.page.locator('a.sectionTitleTextButton').filter({ hasText: expected }).filter({ visible: true });
    await heading.waitFor({ state: 'visible', timeout: 20000 });
    requireThat(await heading.count() === 1 && (await heading.innerText()).replace(/\ue5e1/g, '').trim() === expected);
    this.report.ui.observations.push({ label, owned_movies_heading_visible: true });
    await this.inactiveMedia(label);
    return heading;
  }
  async returnHome(movie, libraryId) {
    requireThat(ID.test(libraryId));
    const previous = this.page.url(), evidence = { control: null, links: 0, buttons: 0, clicked: false };
    this.report.ui.home_navigation = evidence; this.phase = 'ui_home_control_wait';
    const until = Date.now() + 15000;
    let control;
    while (Date.now() < until) {
      const links = this.page.getByRole('link', { name: 'Home', exact: true }).filter({ visible: true });
      const buttons = this.page.getByRole('button', { name: 'Home', exact: true }).filter({ visible: true });
      evidence.links = await links.count(); evidence.buttons = await buttons.count();
      evidence.control = selectHomeControl(evidence.links, evidence.buttons);
      if (evidence.control) { control = evidence.control === 'link' ? links : buttons; break; }
      await this.page.waitForTimeout(100);
    }
    requireThat(control); this.phase = 'ui_home_click'; await control.click({ timeout: 8000 }); evidence.clicked = true;
    this.phase = 'ui_home_confirmation';
    const deadline = Date.now() + 20000;
    do {
      Object.assign(evidence, homeNavigationLocation(this.page.url(), previous));
      const titles = this.page.getByText(this.profile.library.name, { exact: true }).filter({ visible: true });
      const cards = titles.locator('xpath=ancestor-or-self::*[(self::button or self::a or @role="button") and @data-action="link" and ancestor::*[contains(concat(" ", normalize-space(@class), " "), " card ") or contains(concat(" ", normalize-space(@class), " "), " cardBox ")]][1]').filter({ visible: true });
      evidence.library_card_count = await cards.count();
      evidence.detail_heading_count = await this.page.getByRole('heading', { name: movie.name, exact: true }).filter({ visible: true }).count();
      if (evidence.library_card_count === 1) {
        const identity = await cards.evaluate(element => {
          const card = element.closest('.card') ?? element.closest('.cardBox'), id = card?.getAttribute('data-id') ?? null;
          return { card_present: Boolean(card), card_id_present: id !== null,
            card_id: id !== null && /^[0-9a-f]{32}$/i.test(id) ? id : null };
        });
        Object.assign(evidence, { card_present: identity.card_present, card_id_present: identity.card_id_present,
          card_id_matches: identity.card_id_present ? identity.card_id === libraryId : null });
      }
      if (confirmedHomeNavigation(evidence)) {
        this.report.ui.observations.push({ label: 'home_after', owned_movies_library_card_visible: true,
          card_id_matches_when_present: evidence.card_id_matches, detail_heading_absent: true, home_route_confirmed: true });
        await this.inactiveMedia('home_after'); return;
      }
      await this.page.waitForTimeout(100);
    } while (Date.now() < deadline);
    throw new Error('cross_user_home_confirmation_failed');
  }
  async browse(movie, libraryId) {
    requireThat(this.proven); this.movie = movie; this.phase = 'ui_home'; await this.assertPinned();
    const heading = await this.home('home_before');
    const section = heading.locator('xpath=ancestor::*[contains(concat(" ", normalize-space(@class), " "), " verticalSection ")][1]');
    requireThat(await section.count() === 1);
    const title = section.getByText(movie.name, { exact: true }).filter({ visible: true });
    const control = title.locator('xpath=ancestor-or-self::*[(self::button or self::a or @role="button") and @data-action="link" and ancestor::*[contains(concat(" ", normalize-space(@class), " "), " card ") or contains(concat(" ", normalize-space(@class), " "), " cardBox ")]][1]').filter({ visible: true });
    await control.waitFor({ state: 'visible', timeout: 15000 }); requireThat(await control.count() === 1);
    const identity = await control.evaluate(element => {
      const card = element.closest('.card,.cardBox');
      return { card: Boolean(card), id: card?.getAttribute('data-id') ?? null, type: card?.getAttribute('data-type') ?? null };
    });
    requireThat(identity.card && (identity.id === null || identity.id === movie.id) && (identity.type === null || identity.type === 'Movie'));
    this.report.ui.observations.push({ label: 'owned_movie_card', title_matches: true, id_matches_when_present: identity.id === null ? null : true });
    this.phase = 'ui_movie'; const start = this.report.requests.length;
    await control.click({ timeout: 8000 });
    await this.page.getByText(movie.name, { exact: true }).filter({ visible: true }).first().waitFor({ state: 'visible', timeout: 15000 });
    const until = Date.now() + 15000;
    let observed;
    do {
      await this.settled();
      observed = this.report.requests.slice(start).find(entry => entry.own_movie && entry.status === 200 && entry.finished && !entry.failed && entry.token_matches_session);
      if (observed) break;
      await this.page.waitForTimeout(100);
    } while (Date.now() < until);
    const current = new URL(this.page.url()), selected = [...new URLSearchParams(current.hash.split('?').slice(1).join('?'))]
      .filter(([key]) => key.toLowerCase() === 'id').map(([, value]) => value);
    requireThat(current.origin === ORIGIN && selected.length === 1 && selected[0] === movie.id && observed);
    this.report.ui.observations.push({ label: 'owned_movie_detail', item_id_matches: true, original_ui_request_index: observed.index,
      original_ui_status: observed.status, original_ui_finished: true });
    await this.inactiveMedia('movie_detail');
    if (this.mode === 'acceptance-preparation') {
      const deadline = Date.now() + 20000; let prepared;
      do {
        await this.settled();
        prepared = this.report.requests.find(entry => entry.owned_preparation && entry.phase === 'ui_movie' && entry.status === 200 &&
          entry.finished && !entry.failed && entry.token_matches_session);
        this.report.special_features = auxiliaryEvidence(this.report.requests, this.account.id, this.report.token_fingerprint, this.profile, 'SpecialFeatures');
        this.report.local_trailers = auxiliaryEvidence(this.report.requests, this.account.id, this.report.token_fingerprint, this.profile, 'LocalTrailers');
        if (prepared && this.report.preparation?.completed && this.report.preparation.response?.validated && this.report.special_features.validated) break;
        await this.page.waitForTimeout(100);
      } while (Date.now() < deadline);
      requireThat(prepared && this.report.preparation?.completed && this.report.preparation.response?.status === 200 &&
        this.report.preparation.response.validated && this.report.proxy.preparation === 1 && this.report.special_features?.validated === true);
      Object.assign(this.report.preparation, { ui_status: prepared.status, ui_finished: true, ui_request_index: prepared.index });
      this.report.ui.observations.push({ label: 'owned_movie_special_features', original_ui: true,
        user_id: this.account.id, item_id: this.movie.id, json_array_count: 3, original_ui_finished: true,
        original_ui_request_indexes: [...this.report.special_features.original_ui_request_indexes] });
      await this.inactiveMedia('movie_preparation_completed');
    }
    await this.observeSpecialFeaturesCards();
    this.phase = 'ui_return_home'; await this.returnHome(movie, libraryId);
    await this.assertPinned(); this.report.ui.outcome = 'passed';
  }
  async observeSpecialFeaturesCards() {
    const cards = [];
    for (const item of expectedAuxiliaryItems(this.profile, 'SpecialFeatures')) {
      const text = this.page.getByText(item.api_name, { exact: true }).filter({ visible: true });
      const card = text.locator('xpath=ancestor::*[contains(concat(" ",normalize-space(@class)," ")," card ") or contains(concat(" ",normalize-space(@class)," ")," cardBox ")][1]');
      await card.waitFor({ state: 'visible', timeout: 12000 });
      requireThat(await card.count() === 1);
      await card.scrollIntoViewIfNeeded({ timeout: 5000 });
      const observed = await card.evaluate(element => {
        const container = element.closest('.card') ?? element.closest('.cardBox');
        const id = container?.getAttribute('data-id') ?? null, bounds = element.getBoundingClientRect();
        return { card_present: Boolean(container), card_id_present: id !== null,
          card_id: id !== null && /^[0-9a-f]{32}$/i.test(id) ? id : null,
          visible: Boolean(element.getClientRects().length) && getComputedStyle(element).visibility !== 'hidden',
          in_viewport: bounds.width > 0 && bounds.height > 0 && bounds.right > 0 && bounds.bottom > 0 &&
            bounds.left < innerWidth && bounds.top < innerHeight };
      });
      requireThat(observed.card_present && observed.visible && observed.in_viewport &&
        (!observed.card_id_present || observed.card_id === item.id));
      cards.push({ item_id: item.id, api_name: item.api_name, title_matches: true, card_present: observed.card_present,
        card_id_present: observed.card_id_present, id_matches_when_present: observed.card_id_present ? true : null,
        visible: observed.visible, in_viewport: observed.in_viewport, elapsed_ms: Date.now() - this.started });
    }
    this.report.ui.special_features_cards = cards;
    await this.inactiveMedia('special_features_cards_observed');
  }
  async signOut() {
    if (!this.page || !this.loginIntent) return;
    this.phase = 'ui_logout';
    await this.assertPinned();
    let control = this.page.getByRole('button', { name: /\bSign Out\b/i }).filter({ visible: true });
    if (!await control.count()) {
      await this.page.getByRole('button', { name: 'Settings', exact: true }).filter({ visible: true }).click({ timeout: 8000 });
      control = this.page.getByRole('button', { name: /\bSign Out\b/i }).filter({ visible: true });
    }
    await control.waitFor({ state: 'visible', timeout: 10000 }); requireThat(await control.count() === 1);
    const observed = this.page.waitForResponse(response => classifyBrowserRequest(response.url(), response.request().method()).kind === 'logout',
      { timeout: 15000 }).catch(() => null);
    this.logoutIntent = true; this.report.logout.attempted = true;
    await control.click({ timeout: 8000 });
    const response = await observed; this.report.logout.status = response?.status() ?? null;
    requireThat(response?.status() === 204);
    await Promise.any([this.page.getByText('Manual Login', { exact: true }).waitFor({ state: 'visible', timeout: 10000 }),
      this.page.locator('form:has(input[type="password"]:visible)').waitFor({ state: 'visible', timeout: 10000 })]);
    this.report.logout.login_view_visible = true;
    await this.proxyIdle();
  }
  async close() {
    const failures = [];
    try { await this.signOut(); } catch { failures.push('ui_logout'); }
    if (this.proof) {
      try { await bounded(this.proof.drain(), 25000); } catch { failures.push('logout_proof'); }
    }
    let contextClosed = !this.context, browserClosed = !this.browser, guardClosed = !this.guard;
    if (this.context) try { await bounded(this.context.close(), 10000); contextClosed = true; } catch { failures.push('context_close'); }
    if (this.browser) try { await bounded(this.browser.close(), 10000); browserClosed = true; } catch { failures.push('browser_close'); }
    if (this.guard) {
      for (const socket of this.sockets) socket.destroy();
      try { await bounded(new Promise(resolve => this.guard.close(resolve)), 5000); guardClosed = true; } catch { failures.push('guard_close'); }
    }
    if (this.proxyPending?.size) try { await bounded(Promise.all([...this.proxyPending]), 25000); } catch { failures.push('proxy_drain'); }
    if (this.websocketPending?.size) try { await bounded(Promise.all([...this.websocketPending]), 25000); } catch { failures.push('websocket_drain'); }
    if (this.proof) try { await bounded(this.proof.dispose(), 25000); } catch { failures.push('proof_dispose'); }
    try { await this.settled(); } catch { failures.push('observer_drain'); }
    this.token = null; this.account.password = null;
    this.report.closed = contextClosed && browserClosed && guardClosed;
    this.report.cleanup_failures = failures;
    return failures.length === 0;
  }
}

/** Run the new receipted positive profile once; old driver inputs and outputs remain independent. */
export async function runSpecialFeaturesAcceptance(input) {
  remoteEnvironment();
  const options = parseSpecialFeaturesArguments(Object.entries(input).flatMap(([name, value]) => ['--' + name, value]));
  requireProxyExecutionMode(options.mode, options['preparation-scope']);
  await privateDirectory(ROOT); process.umask(0o077);
  await fs.mkdir(options.output, { mode: 0o700 });
  const deadline = Date.now() + 240000;
  const report = { marker: 'goby-client-special-features-browser-v1', format: 6, mode: options.mode,
    preparation_scope: PREPARATION_SCOPE, client_acceptance: false, result: 'in_progress', failure: null,
    started_at: new Date().toISOString(), accounts: [], api_reads: [],
    scope: { accounts: 'Existing ordinary AV user A and initial viewer B; independent fresh browsers',
      ui: 'Manual login, Home, positive Movie, three visible SpecialFeatures cards, Home and UI signout; no card playback',
      api: 'Explicit own-user ten-item UserData and preference/configuration/policy reads; foreign-user Movie denials remain separate API evidence',
      state_window: 'Each user baseline follows its login; both final snapshots precede either signout',
      state_mutations: 'Only fresh UI authentication, bounded capabilities, one positive-Movie preparation per actor and signout; no explicit policy/metadata/preference/UserData writes',
      userdata_projection: 'Ten UserData projections must stay identical; this does not claim no new physical default row',
      userdata_storage: 'When absent before, preparation may insert exactly one source32 default row for that actor and this positive Movie; the separate fresh ledger must attribute it. No API initialization is allowed',
      database_wide_preservation_claimed: false,
      local_trailers: 'Passive original-client observation only. An absent request remains not_observed and supplies no client evidence',
      service_workers: 'Allowed; all browser and worker HTTP crosses the fixed forwarding proxy without target bypass',
      websocket_scope: 'Reuse the pinned genuine WebSocket/CONNECT transport; fresh ordinary authority is required and server control commands fail the run',
      normal_login_effects: 'Fresh authentication sessions, devices, audit and their presence updates are allowed and separately accounted',
      token_storage: 'Authority stays in memory. No auth-response decoding, browser storage, HAR, trace, stack or raw payload logging',
      preparation: { id: PREPARATION_SCOPE, item_id: null, physical_posts_per_actor: 1,
        existing_rows: 'No old play or reference deletion. Only previously enumerated revoked-auth Prepared rows may expire; all other old play/auth/UserData/reference/encoding rows remain unchanged',
        default_userdata: 'Only an absent actor+positiveMovie row may become its exact default row; physical additions are proven by the fresh ledger',
        database_proof: 'A new scope-specific before/after ledger binds the completed nonempty profile and actual current candidate; no consumed origin or row count is reused' } },
    limits: { work_ms: 240000, api_requests: 64, api_timeout_ms: 10000, api_response_bytes: LIMIT,
      browser_requests_per_user: 2000, proxy: PROXY_LIMITS, websocket: WS_LIMITS,
      auxiliary: { reads_per_kind_per_actor: 4, response_bytes: LIMIT, observation_timeout_ms: 5000 },
      browser_diagnostics: { events_per_kind_per_actor: DIAGNOSTIC_ENTRY_LIMIT, input_characters: DIAGNOSTIC_INPUT_LIMIT,
        output_characters: DIAGNOSTIC_OUTPUT_LIMIT, success_requires_page_error_count: 0 } } };
  let phase = 'input_closure', fixture, accounts = [], items = [], reads = 0, trustLost = false, terminalReport = report;
  const pins = [], sessions = [], secrets = [], observedBefore = new Map();
  const rememberSecret = value => { if (typeof value === 'string' && value.length && !secrets.includes(value)) secrets.push(value); };
  const fail = value => { report.failure ??= value; };
  async function pin() {
    requireThat(!trustLost);
    try {
      await fixture.assertPinned();
      for (const [filename, expected, modes, parse] of pins) requireThat((await readOwnedFile(filename, modes, parse)).sha256 === expected);
    } catch {
      trustLost = true;
      for (const session of sessions) {
        session.ownershipLost = true;
        if (session.context) await session.context.setOffline(true).catch(() => {});
      }
      throw new Error('cross_user_target_changed');
    }
  }
  async function read(session, route, label, query = {}) {
    requireThat(session.token && Date.now() < deadline && ++reads <= 64);
    await session.assertPinned();
    const url = new URL(route, ORIGIN); for (const [key, value] of Object.entries(query)) url.searchParams.set(key, value);
    const response = await readBoundedJSON(url, session.token);
    report.api_reads.push({ actor: session.account.slot, label, method: 'GET', status: response.status,
      response_bytes: response.bytes, is_ui_request: false, query_field_names: Object.keys(query) });
    await session.assertPinned(); return response;
  }
  async function snapshot(session, label) {
    const actor = session.account;
    const user = await read(session, '/emby/Users/' + actor.id, label + '_own_user');
    requireThat(user.status === 200 && validateReadPrincipal(user.data, actor, fixture.serverId));
    const prefs = await read(session, '/emby/UserSettings/' + actor.id, label + '_own_preferences');
    requireThat(prefs.status === 200 && record(prefs.data));
    const rows = [];
    for (const expected of items) {
      const actual = await read(session, '/emby/Users/' + actor.id + '/Items/' + expected.id, label + '_own_' + expected.key,
        { Fields: 'Path,MediaSources,MediaStreams' });
      requireThat(actual.status === 200); rows.push(itemState(actual.data, expected)); session.report.own_item_reads += 1;
      if (expected.key === 'movie') {
        const source = bindOwnedMovieSource(actual.data, expected);
        requireThat(!session.preparationSource || same(session.preparationSource, source));
        session.preparationSource = source; session.report.preparation_source = source;
      }
    }
    return { user_id: actor.id, items: rows, preferences: prefs.data, configuration: user.data.Configuration, policy: user.data.Policy };
  }
  async function ordinaryPrincipal(session) {
    if (!session.authorityPromise) {
      const token = session.token;
      session.authorityPromise = (async () => {
        const principal = await read(session, '/emby/Users/' + session.account.id, 'explicit_own_user_authority');
        requireThat(principal.status === 200 && validateReadPrincipal(principal.data, session.account, fixture.serverId));
        const restricted = await read(session, '/emby/Users/Query', 'explicit_nonadministrator_authority', { Limit: '0' });
        requireThat(administratorAccessDenied(restricted) && token === session.token);
        session.proven = session.report.principal_confirmed = session.report.ordinary_authority_confirmed = true;
      })();
    }
    await session.authorityPromise;
  }
  try {
    report.input_closure = {};
    for (const name of INPUT_NAMES) {
      const filename = fileURLToPath(new URL(name, import.meta.url)), file = await readOwnedFile(filename, [0o600, 0o644, 0o700, 0o755], false);
      if (Object.hasOwn(FROZEN_SHARED_INPUTS, name)) requireThat(file.sha256 === FROZEN_SHARED_INPUTS[name]);
      report.input_closure[name] = file.sha256; pins.push([filename, file.sha256, [0o600, 0o644, 0o700, 0o755], false]);
    }
    phase = 'fixture_binding';
    fixture = await loadSpecialFeaturesFixture({ candidateSHA256: options['candidate-sha256'], source: options.source,
      sourceManifestSHA256: options['source-manifest-sha256'], receiptSHA256: options['profile-receipt-sha256'],
      reportSHA256: options['profile-report-sha256'], inspectSHA256: options['profile-inspect-sha256'],
      musicChainPath: options['music-upgrade-chain'], musicChainSHA256: options['music-upgrade-chain-sha256'],
      musicScanReceiptSHA256: options['music-scan-receipt-sha256'] });
    requireThat(fixture.origin === ORIGIN); requirePreparationFixture(fixture);
    const stateFile = await readOwnedFile(STATE), aFile = await readOwnedFile(A_CREDENTIALS), bFile = await readOwnedFile(B_CREDENTIALS);
    for (const credentials of [aFile.value, bFile.value]) { rememberSecret(credentials?.admin?.password); rememberSecret(credentials?.viewer?.password); }
    accounts = bindCrossUserCredentials(stateFile, aFile, bFile, fixture);
    pins.push([B_CREDENTIALS, bFile.sha256, [0o600], true]);
    report.fixture = fixture.evidence; report.profile_items = fixture.items; report.profile_library = fixture.library;
    const project = (key, profileKey = key) => {
      const value = fixture.items[profileKey];
      return { key, id: value.id, name: value.api_name, type: value.api_type, path: value.path,
        parentId: value.parent_id, media_source_id: value.media_source_id };
    };
    items = [project('movie', 'positive'),
      { key: 'original_movie', id: OLD_MOVIE_ITEM, name: 'M3e Client Movie', type: 'Movie',
        path: '/opt/goby-fixtures/client-m3e/Movies/M3e Client Movie.mp4' },
      ...['mp3', 'flac'].map(key => ({ key, ...fixture.music.tracks[key] })),
      { key: 'album', ...fixture.music.album },
      ...['empty', 'alpha', 'deleted', 'zeta', 'trailer'].map(key => project(key))];
    requireThat(items.length === 10 && new Set(items.map(item => item.id)).size === 10);
    report.owned_items = items.map(item => ({ key: item.key, id: item.id, type: item.type }));
    report.scope.preparation.item_id = fixture.items.positive.id;
    report.scope.preparation.media_source_id = fixture.items.positive.media_source_id;
    await writePrivate(path.join(options.output, 'intent.json'), JSON.stringify({ marker: report.marker, format: report.format,
      mode: options.mode, preparation_scope: PREPARATION_SCOPE, candidate_sha256: options['candidate-sha256'],
      fixture: fixture.evidence.profile, account_ids: accounts.map(actor => actor.id), input_closure: report.input_closure,
      preparation: report.scope.preparation, retry_policy: 'Never reuse this output or replay a consumed preparation scope' }, null, 2) + '\n');
    for (const account of accounts) {
      const actorReport = {}; report.accounts.push(actorReport);
      const session = new BrowserActor(account, pin, actorReport, fixture); sessions.push(session);
      session.diagnosticSecrets = () => secrets; session.rememberSecret = rememberSecret;
      session.authorizeSocket = () => ordinaryPrincipal(session);
      phase = 'login_' + account.slot;
      requireThat(Date.now() < deadline); await session.open(); rememberSecret(session.token);
      await ordinaryPrincipal(session);
      phase = 'before_' + account.slot;
      const before = await snapshot(session, 'before'); observedBefore.set(account.slot, before); actorReport.before = stateEvidence(before);
    }
    requireThat(sessions.length === 2 && sessions[0].token !== sessions[1].token);
    for (const session of sessions) {
      phase = 'browse_' + session.account.slot;
      try {
        requireThat(Date.now() < deadline); await session.browse(items[0], fixture.library.id);
      } catch (error) {
        session.report.ui.outcome = 'failed';
        session.report.ui.failure = { ...safeBrowserFailure(error, session.phase), elapsed_ms: Date.now() - session.started };
        fail(phase);
      }
      try {
        phase = 'foreign_read_' + session.account.slot;
        const other = accounts.find(actor => actor.id !== session.account.id);
        const foreign = await read(session, '/emby/Users/' + other.id + '/Items/' + items[0].id, 'explicit_foreign_user_positive_movie_denial');
        session.report.foreign_read = { target_user_id: other.id, status: foreign.status,
          error_code: foreign.data?.ResponseStatus?.ErrorCode === 'access_denied' ? 'access_denied' : 'unexpected', is_ui_request: false };
        requireThat(foreignAccessDenied(foreign));
      } catch { fail(phase); }
    }
  } catch { fail(phase); }
  finally {
    for (const session of sessions) rememberSecret(session.token);
    for (const session of sessions) if (session.proven && observedBefore.has(session.account.slot) && !session.ownershipLost) {
      try {
        const after = await snapshot(session, 'after'); session.report.after = stateEvidence(after);
        session.report.comparison = comparePositiveState(observedBefore.get(session.account.slot), after);
        if (!Object.values(session.report.comparison).every(value => value === true)) fail('state_changed_' + session.account.slot);
      } catch { fail('after_' + session.account.slot); }
    }
    for (const session of [...sessions].reverse()) {
      try { if (!await session.close()) fail('cleanup_' + session.account.slot); }
      catch { fail('cleanup_' + session.account.slot); session.report.closed = false; }
    }
    if (fixture) {
      try { await pin(); } catch { fail('final_fixture_pin'); }
      for (const actor of report.accounts) {
        actor.special_features = auxiliaryEvidence(actor.requests, actor.id, actor.token_fingerprint, fixture, 'SpecialFeatures');
        actor.local_trailers = auxiliaryEvidence(actor.requests, actor.id, actor.token_fingerprint, fixture, 'LocalTrailers');
      }
    }
    resanitizeBrowserDiagnostics(report, secrets);
    report.finished_at = new Date().toISOString();
    report.client_interventions = {
      external_posts_blocked: report.accounts.flatMap(actor => actor.requests ?? []).filter(entry => entry.category === 'external_post').length,
      external_network_permission_granted: false, physical_userdata_increment: 'Requires the separate fresh ledger; API equality does not prove no row insertion',
    };
    report.result = positiveSpecialFeaturesResult(report) ? 'passed' : 'failed';
    report.client_acceptance = report.result === 'passed';
    if (report.result === 'failed') report.failure ??= report.accounts.some(actor => actor.page_error_count !== 0)
      ? 'page_errors_observed' : 'positive_special_features_acceptance_incomplete';
    let encoded = JSON.stringify(report, null, 2) + '\n', secretPresent = true;
    try { const lower = encoded.toLowerCase(); secretPresent = diagnosticSecretVariants(secrets).some(secret => lower.includes(secret.toLowerCase())); }
    catch { /* An invalid secret union cannot release the report. */ }
    if (secretPresent) {
      terminalReport = { marker: report.marker, format: 6, mode: options.mode, preparation_scope: PREPARATION_SCOPE,
        result: 'failed', failure: 'report_secret_guard', client_acceptance: false,
        closed: sessions.every(session => session.report.closed === true) };
      encoded = JSON.stringify(terminalReport) + '\n';
    }
    await writePrivate(path.join(options.output, 'report.json'), encoded);
    for (const account of accounts) account.password = null;
    secrets.fill(null); observedBefore.clear();
  }
  return terminalReport;
}
if (process.argv[1] && path.resolve(process.argv[1]) === SELF) {
  try {
    const report = await runSpecialFeaturesAcceptance(parseSpecialFeaturesArguments(process.argv.slice(2)));
    process.stdout.write(JSON.stringify({ result: report.result, mode: report.mode, client_acceptance: report.client_acceptance,
      marker: report.marker, failure: report.failure }) + '\n');
    if (!['passed', 'diagnostic_only'].includes(report.result)) process.exitCode = 1;
  } catch {
    process.stdout.write(JSON.stringify({ result: 'failed', marker: 'goby-client-special-features-browser-v1', failure: 'setup_or_terminal_report' }) + '\n');
    process.exitCode = 1;
  }
}
