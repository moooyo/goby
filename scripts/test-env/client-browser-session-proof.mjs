/** Bind each original UI logout to a separate, bounded token rejection check. */

import http from 'node:http';
import { createHash } from 'node:crypto';

const DEFAULT_LIMITS = Object.freeze({
  captureTimeoutMs: 1500,
  logoutWaitMs: 12000,
  httpTimeoutMs: 8000,
  httpIdleTimeoutMs: 3000,
  responseByteLimit: 65536,
  responseHeaderByteLimit: 16384,
});
const MAX_LOGOUTS = 16;
const MAX_REQUEST_BYTES = 16384;
const MAX_TOKEN_BYTES = 4096;
const TOKEN_HEADERS = new Set(['x-emby-token', 'x-mediabrowser-token']);
const TOKEN_QUERY = new Set(['api_key', 'x-emby-token', 'x-mediabrowser-token']);
const AUTHORIZATION_HEADERS = new Set(['authorization', 'x-emby-authorization']);
const METADATA_FIELDS = new Set([
  'x-emby-client', 'x-emby-device-name', 'x-emby-device-id', 'x-emby-client-version',
]);

function failure(code) {
  return new Error(code);
}

function bounded(promise, milliseconds, code) {
  let timer;
  return Promise.race([
    promise,
    new Promise((_, reject) => { timer = setTimeout(() => reject(failure(code)), milliseconds); }),
  ]).finally(() => clearTimeout(timer));
}

function validHeader(value, maximum = MAX_REQUEST_BYTES) {
  return typeof value === 'string' && Buffer.byteLength(value) <= maximum &&
    !/[\x00-\x1f\x7f-\uffff]/.test(value);
}

function authorizationToken(value) {
  const scheme = /^(?:Emby|MediaBrowser)\s+/i.exec(value);
  if (!scheme) throw failure('unsupported_authorization_scheme');
  let position = scheme[0].length;
  let token = null;
  const fields = new Set();
  while (position < value.length) {
    const field = /^([A-Za-z][A-Za-z0-9_-]{0,63})\s*=\s*/.exec(value.slice(position));
    if (!field) throw failure('unsupported_authorization_fields');
    const name = field[1].toLowerCase();
    if (fields.has(name) || fields.size >= 16) throw failure('duplicate_authorization_field');
    fields.add(name);
    position += field[0].length;
    let content = '';
    if (value[position] === '"') {
      position += 1;
      let closed = false;
      while (position < value.length) {
        const character = value[position++];
        if (character === '"') { closed = true; break; }
        if (character === '\\') {
          if (position >= value.length) throw failure('unsupported_authorization_fields');
          content += value[position++];
        } else content += character;
      }
      if (!closed) throw failure('unsupported_authorization_fields');
    } else {
      const end = value.indexOf(',', position);
      content = value.slice(position, end < 0 ? value.length : end).trim();
      position = end < 0 ? value.length : end;
      if (!content || /["\\]/.test(content)) throw failure('unsupported_authorization_fields');
    }
    if (name === 'token') token = content;
    while (value[position] === ' ') position += 1;
    if (position === value.length) break;
    if (value[position++] !== ',') throw failure('unsupported_authorization_fields');
    while (value[position] === ' ') position += 1;
    if (position === value.length) throw failure('unsupported_authorization_fields');
  }
  return token;
}

function selectedTarget(value) {
  let target;
  try { target = new URL(value); }
  catch { throw failure('invalid_session_proof_target'); }
  if (target.protocol !== 'http:' || !['127.0.0.1', 'localhost', '[::1]'].includes(target.hostname) ||
      target.username || target.password || target.search || target.hash) {
    throw failure('session_proof_requires_loopback_http');
  }
  return target;
}

function selectedLimits(values) {
  const limits = { ...DEFAULT_LIMITS };
  if (values === undefined) return limits;
  if (!values || typeof values !== 'object' || Array.isArray(values)) throw failure('invalid_session_proof_limits');
  for (const [name, value] of Object.entries(values)) {
    if (!Object.hasOwn(DEFAULT_LIMITS, name) || !Number.isInteger(value) || value < 1 || value > DEFAULT_LIMITS[name]) {
      throw failure('invalid_session_proof_limits');
    }
    limits[name] = value;
  }
  return limits;
}

async function captureAuthority(request, target, limits) {
  let url;
  const rawURL = request.url();
  if (typeof rawURL !== 'string' || Buffer.byteLength(rawURL) > MAX_REQUEST_BYTES) throw failure('request_url_exceeds_limit');
  try { url = new URL(rawURL); }
  catch { throw failure('invalid_logout_request_url'); }
  if (url.origin !== target.origin || url.username || url.password || url.hash) throw failure('invalid_logout_request_origin');
  let headers;
  try { headers = await bounded(request.allHeaders(), limits.captureTimeoutMs, 'request_headers_timeout'); }
  catch { throw failure('request_headers_unavailable'); }
  if (!headers || typeof headers !== 'object' || Array.isArray(headers) || Object.keys(headers).length > 128) {
    throw failure('request_headers_exceed_limit');
  }
  const replayHeaders = {};
  const replayQuery = new URLSearchParams();
  const queryValues = new Map();
  const sources = [];
  let token = null;
  let headerBytes = 0;
  function addToken(value, source) {
    // Reject a coalesced duplicate header instead of treating its comma as part
    // of one credential. Unsupported token syntax never becomes passing proof.
    if (!validHeader(value, MAX_TOKEN_BYTES) || !value || /[\s,"\\]/.test(value)) throw failure('unsupported_token_shape');
    if (token !== null && value !== token) throw failure('conflicting_token_sources');
    token = value;
    if (!sources.includes(source)) sources.push(source);
  }
  for (const [name, value] of Object.entries(headers)) {
    if (!/^[A-Za-z0-9-]{1,128}$/.test(name) || typeof value !== 'string') throw failure('unsupported_request_headers');
    headerBytes += Buffer.byteLength(name) + Buffer.byteLength(value) + 4;
    if (headerBytes > MAX_REQUEST_BYTES) throw failure('request_headers_exceed_limit');
    const lower = name.toLowerCase();
    if (!TOKEN_HEADERS.has(lower) && !AUTHORIZATION_HEADERS.has(lower) && !METADATA_FIELDS.has(lower)) continue;
    if (!validHeader(value) || Object.hasOwn(replayHeaders, lower)) throw failure('unsupported_authority_header');
    replayHeaders[lower] = value;
    if (TOKEN_HEADERS.has(lower)) addToken(value, `header:${lower}`);
    if (AUTHORIZATION_HEADERS.has(lower)) {
      const embedded = authorizationToken(value);
      if (embedded !== null) addToken(embedded, `header:${lower}:token`);
    }
  }
  let queryCount = 0;
  for (const [name, value] of url.searchParams) {
    if (++queryCount > 128) throw failure('request_query_exceeds_limit');
    const lower = name.toLowerCase();
    if (!TOKEN_QUERY.has(lower) && !METADATA_FIELDS.has(lower)) continue;
    if (!validHeader(value, MAX_TOKEN_BYTES)) throw failure('unsupported_authority_query');
    if (queryValues.has(lower) && queryValues.get(lower) !== value) throw failure('conflicting_authority_query');
    queryValues.set(lower, value);
    replayQuery.append(name, value);
    if (TOKEN_QUERY.has(lower)) addToken(value, `query:${lower}`);
  }
  if (token === null) throw failure('logout_token_not_observed');
  const query = replayQuery.toString();
  if (Buffer.byteLength(query) > MAX_REQUEST_BYTES) throw failure('authority_query_exceeds_limit');
  return { headers: replayHeaders, query, fingerprint: createHash('sha256').update(token).digest('hex'), sources };
}

function verifyRejectedToken(target, authority, limits) {
  return new Promise(resolve => {
    let outgoing;
    let incoming;
    let settled = false;
    let received = 0;
    const result = { status: null, response_bytes: 0, result: 'transport_failed' };
    const timer = setTimeout(() => finish('request_timeout'), limits.httpTimeoutMs);
    function finish(outcome) {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      result.response_bytes = received;
      result.result = outcome;
      if (incoming && !incoming.complete) incoming.destroy();
      if (outgoing && !outgoing.destroyed) outgoing.destroy();
      resolve(result);
    }
    try {
      const requestPath = '/emby/System/Info' + (authority.query ? `?${authority.query}` : '');
      const requestHeaders = { ...authority.headers, Host: target.host, Connection: 'close',
        Accept: 'application/json', 'Accept-Encoding': 'identity',
        'User-Agent': 'GobyBrowserPostLogoutVerification/1' };
      const requestBytes = Buffer.byteLength(`GET ${requestPath} HTTP/1.1\r\n\r\n`) +
        Object.entries(requestHeaders).reduce((total, [name, content]) => total + Buffer.byteLength(`${name}: ${content}\r\n`), 0);
      if (requestBytes > MAX_REQUEST_BYTES) { finish('request_exceeds_limit'); return; }
      // Resolve localhost to a literal loopback address without invoking DNS.
      outgoing = http.request({
        hostname: target.hostname === '[::1]' ? '::1' : '127.0.0.1',
        port: target.port || 80,
        method: 'GET',
        path: requestPath,
        agent: false,
        maxHeaderSize: limits.responseHeaderByteLimit,
        headers: requestHeaders,
      }, response => {
        incoming = response;
        result.status = response.statusCode ?? null;
        response.on('error', () => finish('response_failed'));
        response.on('aborted', () => finish('response_aborted'));
        response.on('close', () => { if (!response.complete) finish('response_incomplete'); });
        if (response.rawHeaders.length > 256) { finish('response_headers_exceed_limit'); return; }
        if (result.status >= 300 && result.status < 400) { finish('redirect_rejected'); return; }
        const declared = response.headers['content-length'];
        if (declared !== undefined && (!/^\d+$/.test(declared) || Number(declared) > limits.responseByteLimit)) {
          finish('response_exceeds_limit');
          return;
        }
        response.on('data', bytes => {
          received += bytes.length;
          if (received > limits.responseByteLimit) finish('response_exceeds_limit');
        });
        response.on('end', () => finish(result.status === 401 ? 'token_rejected' : 'token_not_rejected'));
      });
      outgoing.maxHeadersCount = 128;
      outgoing.setTimeout(limits.httpIdleTimeoutMs, () => finish('request_idle_timeout'));
      outgoing.on('error', () => finish('transport_failed'));
      outgoing.on('upgrade', (_response, socket) => { socket.destroy(); finish('upgrade_rejected'); });
      outgoing.end();
    } catch { finish('request_setup_failed'); }
  });
}

/**
 * Attach immediately after a fresh, target-only BrowserContext is created and
 * before newPage(), navigation, or UI login. The caller owns the browser guard
 * and must not load storageState. This module never performs UI or login work.
 *
 * Attach report to the caller's report. After its UI workflow, await drain();
 * require outcome === 'all_observed_logout_tokens_rejected', then dispose().
 * Limits may only be reduced, primarily for independent loopback guard tests.
 */
export function createBrowserSessionProof({ context, target: value, limits: requestedLimits } = {}) {
  const target = selectedTarget(value);
  const limits = selectedLimits(requestedLimits);
  if (!context || typeof context.on !== 'function' || typeof context.off !== 'function' ||
      typeof context.pages !== 'function' || context.pages().length !== 0) {
    throw failure('session_proof_requires_context_before_first_page');
  }
  const started = Date.now();
  const report = {
    format: 1,
    outcome: 'no_logout_observed',
    entries: [],
    logout_overflow: 0,
    observer_errors: 0,
    policy: {
      source: 'original-client-request-events',
      token_source: 'Exact observed logout request headers or query fields, retained only in memory',
      token_fingerprint: 'SHA-256 of the decoded observed token',
      token_response_body_read: false,
      client_internal_state_read: false,
      browser_context_requirement: 'Fresh context, no storageState, attached before first page',
      browser_network_requirement: 'Caller-enforced selected loopback origin only',
      verification_source: 'independent-node-http-post-logout-verification',
      verification_is_ui_request: false,
      verification_method: 'GET',
      verification_route: '/emby/System/Info',
      verification_expected_status: 401,
      redirects: 'Rejected without following',
      localhost_resolution: 'Pinned to 127.0.0.1 without DNS',
      payload_logging: false,
      metadata_policy: 'Only observed authorization and allowlisted Emby client metadata are replayed',
      max_request_bytes: MAX_REQUEST_BYTES,
      max_logout_requests: MAX_LOGOUTS,
      limits: { ...limits },
    },
  };
  const bindings = new WeakMap();
  const states = [];
  let disposed = false;

  function summarize() {
    report.outcome = report.entries.length === 0 ? 'no_logout_observed'
      : !report.logout_overflow && !report.observer_errors && report.entries.every(entry => entry.result === 'logout_token_rejected')
        ? 'all_observed_logout_tokens_rejected' : 'logout_token_proof_incomplete';
    return report;
  }

  function beginVerification(state, trigger) {
    if (state.started) return;
    state.started = true;
    clearTimeout(state.timer);
    const entry = state.entry;
    entry.verification.trigger = trigger;
    void (async () => {
      let authority = null;
      try {
        authority = await state.authority;
        state.authority = null;
        entry.token_fingerprint = authority.fingerprint;
        entry.token_sources = authority.sources;
        entry.verification.started_elapsed_ms = Date.now() - started;
        entry.verification.eligible_at_request_start = entry.ui_request.response_status >= 200 && entry.ui_request.response_status < 300;
        Object.assign(entry.verification, await verifyRejectedToken(target, authority, limits));
        entry.result = entry.verification.eligible_at_request_start && entry.verification.result === 'token_rejected'
          ? 'logout_token_rejected' : 'logout_token_proof_incomplete';
      } catch {
        // Never propagate a browser or HTTP exception containing credentials.
        entry.verification.result = 'logout_authority_unavailable_or_ambiguous';
        entry.result = 'logout_token_proof_incomplete';
      } finally {
        if (authority) { authority.headers = null; authority.query = null; }
        authority = null;
        state.authority = null;
        entry.verification.finished_elapsed_ms = Date.now() - started;
        state.complete();
        summarize();
      }
    })();
  }

  function observeRequest(request) {
    if (disposed) return;
    try {
      const rawURL = request.url();
      if (typeof rawURL !== 'string' || Buffer.byteLength(rawURL) > MAX_REQUEST_BYTES || request.method() !== 'POST') return;
      const url = new URL(rawURL);
      if (url.origin !== target.origin || !/^(?:\/emby)?\/sessions\/logout\/?$/i.test(url.pathname)) return;
      if (bindings.has(request)) return;
      if (states.length >= MAX_LOGOUTS) { report.logout_overflow += 1; summarize(); return; }
      const entry = {
        index: states.length,
        ui_request: { method: 'POST', route: /^\/emby\//i.test(url.pathname) ? '/emby/Sessions/Logout' : '/Sessions/Logout',
          elapsed_ms: Date.now() - started, response_status: null, client_request_finished: false, client_request_failed: false },
        token_fingerprint: null,
        token_sources: [],
        verification: { source: 'independent-node-http-post-logout-verification', is_ui_request: false,
          method: 'GET', route: '/emby/System/Info', status: null, eligible_at_request_start: false, result: 'pending' },
        result: 'pending',
      };
      const state = { entry, started: false, authority: null, timer: null, complete: null, done: null };
      state.done = new Promise(resolve => { state.complete = resolve; });
      bindings.set(request, state);
      states.push(state);
      report.entries.push(entry);
      state.authority = captureAuthority(request, target, limits);
      // A rejected capture must remain handled while awaiting a UI response.
      state.authority.catch(() => {});
      state.timer = setTimeout(() => beginVerification(state, 'logout_response_deadline'), limits.logoutWaitMs);
      summarize();
    } catch { report.observer_errors += 1; summarize(); }
  }

  function observeResponse(response) {
    try {
      const state = bindings.get(response.request());
      if (!state) return;
      state.entry.ui_request.response_status = response.status();
      state.entry.ui_request.response_elapsed_ms = Date.now() - started;
      beginVerification(state, 'ui_logout_response');
    } catch { report.observer_errors += 1; summarize(); }
  }

  function observeFailed(request) {
    const state = bindings.get(request);
    if (!state) return;
    state.entry.ui_request.client_request_failed = true;
    state.entry.ui_request.failure_elapsed_ms = Date.now() - started;
    beginVerification(state, 'ui_logout_request_failed');
  }

  function observeFinished(request) {
    const state = bindings.get(request);
    if (!state) return;
    state.entry.ui_request.client_request_finished = true;
    beginVerification(state, 'ui_logout_request_finished');
  }

  context.on('request', observeRequest);
  context.on('response', observeResponse);
  context.on('requestfailed', observeFailed);
  context.on('requestfinished', observeFinished);

  async function drain() {
    for (;;) {
      const observedCount = states.length;
      await Promise.all(states.map(state => state.done));
      if (states.length === observedCount) return summarize();
    }
  }

  async function dispose() {
    if (disposed) return;
    await drain();
    disposed = true;
    context.off('request', observeRequest);
    context.off('response', observeResponse);
    context.off('requestfailed', observeFailed);
    context.off('requestfinished', observeFinished);
    for (const state of states) { clearTimeout(state.timer); state.authority = null; }
  }

  return Object.freeze({ report, drain, dispose });
}
