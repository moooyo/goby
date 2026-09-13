#!/usr/bin/env node
/** Exercise the production native hook with pure fixtures and optional synthetic pages. */
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { EventEmitter } from 'node:events';
import { createHash } from 'node:crypto';
import { createRequire } from 'node:module';
import { fileURLToPath, pathToFileURL } from 'node:url';
import http from 'node:http';

const ORIGIN = 'http://native-rejection.invalid', OTHER = 'http://native-rejection-other.invalid';
const SECRET = 'synthetic-native-secret-value', LATE = 'synthetic-late-native-secret';
const REQUEST_ID = 'synthetic-native-request-01';
const sha = bytes => createHash('sha256').update(bytes).digest('hex');
const clone = value => structuredClone(value);
const wire = (event, fields = {}) => JSON.stringify({ version: 1, event, ...fields });
const primitive = (type, value = null) => ({ kind: 'primitive', type, value });
const responseReason = overrides => ({ kind: 'response', url: ORIGIN + '/api/item', status: 404, type: 'basic', redirected: false, requestId: REQUEST_ID, ...overrides });
const request = overrides => ({ ordinal: 7, url: ORIGIN + '/api/item', status: 404, response_headers: { 'X-Request-Id': REQUEST_ID }, ...overrides });
const pause = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));

async function bounded(promise, milliseconds, code) {
  let timer;
  try { return await Promise.race([promise, new Promise((_, reject) => { timer = setTimeout(() => reject(new Error(code)), milliseconds); })]); }
  finally { clearTimeout(timer); }
}

async function until(check, code, milliseconds = 3000) {
  const end = Date.now() + milliseconds;
  do { if (await check()) return; await pause(20); } while (Date.now() < end);
  throw new Error(code);
}

function collector(api, secrets = [SECRET]) {
  const value = api.createCandidateNativeRejectionCollector({ secrets, context: () => ({ elapsed_ms: 17, phase: 'synthetic', operation: 'reject' }) });
  value.ingest(wire('installed'), 1);
  value.setBridgeState({ setupComplete: true, teardownComplete: true });
  return value;
}

function ingest(value, reason, sequence = 1, context = 1) { value.ingest(wire('rejection', { sequence, reason }), context); }
function snapshot(value, requests = []) { return value.snapshot({ authorityComplete: true, requests }); }

function assertReadable(closer, value) {
  const bytes = Buffer.from(JSON.stringify(value));
  assert.deepEqual(closer.strictJSON(bytes), value);
  const report = { requests: [], native_rejections: value.events, native_rejection_diagnostics: value.summary };
  assert.deepEqual(closer.observationJSON(Buffer.from(JSON.stringify(report)), { browserOrigin: ORIGIN }), report);
}

function assertOriginalUIFailure(closer, native, pageErrors) {
  assert(pageErrors.length > 0);
  assert(native.events.some(row => row.reason.kind === 'response' && row.association.state === 'matched'));
  const report = { outcome: 'scenario_completed', failure: null, page_errors: pageErrors,
    native_rejections: native.events, native_rejection_diagnostics: native.summary,
    cleanup: { browser_closed: true, tokens_rejected: true, media_stopped: true },
    browser_environment: { service_workers: 'allow', proxy_bypass: '<-loopback>', quic: 'disabled', nonproxied_webrtc_udp: 'disabled' },
    tv_browse: { outcome: 'tv_browse_ui_flow_completed' } };
  assert.throws(() => closer.verifyUI(report, { scenario: 'tv-browse' }, { exchanges: [], logins: [] }), /client_ui_or_cleanup_incomplete/);
  assert.deepEqual(closer.verifyUI({ ...report, page_errors: [] }, { scenario: 'tv-browse' }, { exchanges: [], logins: [] }), []);
}

function pureCases(api, closer) {
  return [
    ['primitive_undefined_and_string_undefined_remain_distinct', () => {
      const value = collector(api);
      [primitive('undefined'), primitive('string', 'undefined'), primitive('null'), primitive('boolean', false),
        primitive('number', 3.5), primitive('number'), primitive('bigint'), primitive('symbol')].forEach((reason, index) => ingest(value, reason, index + 1));
      const result = snapshot(value);
      assert.deepEqual(result.events[0].reason, primitive('undefined'));
      assert.deepEqual(result.events[1].reason, primitive('string', 'undefined'));
      assert.equal(result.summary.count, 8); assert.equal(result.summary.diagnostics_complete, true);
      assertReadable(closer, result);
    }],
    ['response_binding_requires_unique_request_id_and_exact_url_status', () => {
      for (const [rows, state, ordinal] of [
        [[request()], 'matched', 7],
        [[request(), request({ ordinal: 8 })], 'ambiguous', null],
        [[request({ url: ORIGIN + '/other' })], 'mismatch', null],
        [[request({ status: 403 })], 'mismatch', null],
        [[request({ response_headers: { 'X-Request-Id': 'another-id' } })], 'unattributed', null],
        [[request({ response_headers: { 'X-Request-Id': REQUEST_ID, 'x-request-id': REQUEST_ID } })], 'unattributed', null],
      ]) {
        const value = collector(api); ingest(value, responseReason()); const result = snapshot(value, rows);
        assert.deepEqual(result.events[0].association, { state, request_ordinal: ordinal });
        assert.deepEqual(result.events[0].reason.location, { origin: ORIGIN, path: '/api/item' });
        assertReadable(closer, result);
      }
    }],
    ['redirect_and_opaque_metadata_do_not_invent_a_request_match', () => {
      const value = collector(api);
      ingest(value, responseReason({ redirected: true }));
      ingest(value, responseReason({ url: '', status: 0, type: 'opaque', requestId: null }), 2);
      ingest(value, responseReason({ url: '', status: 0, type: 'opaqueredirect', requestId: null }), 3);
      const result = snapshot(value, [request()]);
      assert.equal(result.events[0].reason.redirected, true); assert.equal(result.events[0].association.state, 'matched');
      for (const row of result.events.slice(1)) {
        assert.equal(row.reason.status, 0); assert.equal(row.reason.location, null); assert.equal(row.reason.request_id, null);
        assert.equal(row.association.state, 'unattributed');
      }
    }],
    ['late_secret_registration_removes_urls_credentials_and_fragments', () => {
      const secrets = [SECRET], value = collector(api, secrets);
      const url = ORIGIN + '/path/' + LATE + '?api_key=' + LATE + '#private-fragment';
      ingest(value, responseReason({ url }));
      ingest(value, primitive('string', 'Failure ' + url + ' password=' + LATE), 2);
      const incomplete = value.snapshot({ authorityComplete: false, requests: [request({ url })] });
      assert.equal(incomplete.events[0].reason.location, null); assert.equal(incomplete.summary.diagnostics_complete, false);
      assert(api.registerCandidateSecret(secrets, LATE));
      const result = snapshot(value, [request({ url })]), encoded = JSON.stringify(result);
      assert(!encoded.includes(LATE)); assert(!encoded.includes('api_key=')); assert(!encoded.includes('private-fragment'));
      assert.equal(result.events[0].association.state, 'matched'); assert(result.events[0].reason.location.path.includes('[redacted]'));
      assertReadable(closer, result);
    }],
    ['invalid_payloads_hostile_coercion_and_context_failures_remain_safe', () => {
      let touched = 0;
      const hostile = { toString() { touched++; throw new Error(SECRET); }, toJSON() { touched++; throw new Error(SECRET); } };
      const value = collector(api); value.ingest(hostile, 1); value.markFailure(SECRET);
      assert.equal(touched, 0); assert(snapshot(value).summary.failures.includes('payload_invalid'));
      assert(!JSON.stringify(snapshot(value)).includes(SECRET));
      const badContext = api.createCandidateNativeRejectionCollector({ secrets: [SECRET], context: () => { throw new Error(SECRET); } });
      badContext.ingest(wire('installed'), 1); ingest(badContext, { kind: 'object' });
      assert(snapshot(badContext).summary.failures.includes('context_failed'));
      assert(!JSON.stringify(snapshot(badContext)).includes(SECRET));
      for (const reason of [primitive('number', Number.MAX_SAFE_INTEGER + 1), { kind: 'object', message: SECRET },
        responseReason({ status: 700 }), responseReason({ redirected: 1 })]) {
        const current = collector(api); ingest(current, reason);
        assert(snapshot(current).summary.failures.includes('payload_invalid'));
      }
    }],
    ['context_and_sequence_errors_are_reported_without_merging_events', () => {
      for (const feed of [
        value => ingest(value, primitive('undefined'), 1, 2),
        value => { ingest(value, primitive('undefined')); ingest(value, primitive('undefined')); },
        value => value.ingest(wire('installed'), 1),
      ]) { const value = collector(api); feed(value); assert(snapshot(value).summary.failures.includes('payload_invalid')); }
      const gap = collector(api); ingest(gap, primitive('undefined'), 2);
      assert(snapshot(gap).summary.failures.includes('capture_failed'));
      const separate = collector(api); separate.ingest(wire('installed'), 2);
      ingest(separate, primitive('undefined')); ingest(separate, primitive('undefined'), 1, 2);
      assert.equal(snapshot(separate).events.length, 2);
      assert.notEqual(snapshot(separate).events[0].id, snapshot(separate).events[1].id);
    }],
    ['event_and_byte_overflow_keep_a_bounded_lower_bound_receipt', () => {
      const events = collector(api); events.ingest(wire('installed'), 2);
      for (let index = 1; index <= api.NATIVE_REJECTION_LIMITS.events; index++) ingest(events, primitive('undefined'), index);
      ingest(events, primitive('undefined'), 1, 2);
      const eventResult = snapshot(events);
      assert.equal(eventResult.events.length, 16); assert(eventResult.summary.overflow_count > 0);
      assert.equal(eventResult.summary.count_is_lower_bound, true); assert.equal(eventResult.summary.diagnostics_complete, false);
      const bytes = collector(api);
      for (let index = 1; index <= 16; index++) ingest(bytes, primitive('string', 'x'.repeat(4096)), index);
      const byteResult = snapshot(bytes);
      assert(byteResult.summary.failures.includes('byte_limit')); assert(byteResult.summary.overflow_count > 0);
      assert(Buffer.byteLength(JSON.stringify(byteResult)) <= api.NATIVE_REJECTION_LIMITS.bytes);
      assertReadable(closer, byteResult);
    }],
    ['native_response_match_does_not_waive_the_original_pageerror', () => {
      const value = collector(api); ingest(value, responseReason());
      assertOriginalUIFailure(closer, snapshot(value, [request()]), [{ name: 'Error', message: 'synthetic original pageerror' }]);
    }],
  ];
}

class FakeSession extends EventEmitter {
  constructor(send = async () => ({})) { super(); this.action = send; this.calls = []; this.detaches = 0; }
  async send(method, params) { this.calls.push({ method, params }); return this.action(method, params); }
  async detach() { this.detaches++; }
}

function bridgeCases(api) {
  const cleanup = async (bridge, calls) => {
    await api.candidateOwnedUICleanup({ observe: async () => {}, stopMedia: async () => { calls.push('stop'); },
      logout: async () => { calls.push('logout'); }, onFailure: () => { throw new Error('unexpected_core_cleanup_failure'); } });
    await bounded(bridge.dispose(), 1000, 'bridge_disposal_blocked_cleanup');
    assert.deepEqual(calls, ['stop', 'logout']);
  };
  return [
    ['same_production_attach_installs_the_exported_hook_and_filters_binding_names', async () => {
      const value = api.createCandidateNativeRejectionCollector({ secrets: [SECRET] }), session = new FakeSession();
      let installed;
      const page = { addInitScript: async (fn, args) => { installed = { fn, args }; }, isClosed: () => true };
      const bridge = await api.attachCandidateNativeRejections({ context: { newCDPSession: async () => session }, page, collector: value, timeoutMs: 30 });
      assert.equal(installed.fn, api.installCandidateNativeRejectionHook);
      assert.deepEqual(installed.args.limits, api.NATIVE_REJECTION_LIMITS);
      session.emit('Runtime.bindingCalled', { name: 'wrong_binding', payload: wire('installed'), executionContextId: 1 });
      assert.equal(snapshot(value).summary.installed_contexts, 0);
      session.emit('Runtime.bindingCalled', { name: installed.args.bindingName, payload: wire('installed'), executionContextId: 1 });
      session.emit('Runtime.bindingCalled', { name: installed.args.bindingName, payload: wire('rejection', { sequence: 1, reason: primitive('undefined') }), executionContextId: 1 });
      await cleanup(bridge, []); await bridge.dispose();
      assert.equal(snapshot(value).summary.diagnostics_complete, true); assert.equal(session.listenerCount('Runtime.bindingCalled'), 0);
      assert.equal(session.calls.filter(row => row.method === 'Runtime.addBinding').length, 1);
    }],
    ['setup_failure_non_error_rejection_and_timeout_do_not_block_core_cleanup', async () => {
      for (const action of [async () => { throw undefined; }, async () => { throw 'synthetic failure'; }, () => new Promise(() => {})]) {
        const value = api.createCandidateNativeRejectionCollector({ secrets: [SECRET] });
        const session = new FakeSession(method => method === 'Runtime.enable' ? action() : Promise.resolve({}));
        const page = { addInitScript: async () => {}, isClosed: () => false };
        const bridge = await bounded(api.attachCandidateNativeRejections({ context: { newCDPSession: async () => session }, page, collector: value, timeoutMs: 20 }), 1000, 'setup_timeout_not_bounded');
        await cleanup(bridge, []);
        const result = snapshot(value);
        assert(result.summary.failures.some(code => ['setup_failed', 'setup_timeout'].includes(code)));
        assert.equal(result.summary.diagnostics_complete, false); assert.equal(session.detaches, 1);
      }
    }],
    ['late_cdp_session_is_detached_after_setup_timeout', async () => {
      let deliver;
      const session = new FakeSession(), value = api.createCandidateNativeRejectionCollector({ secrets: [SECRET] });
      const pending = new Promise(resolve => { deliver = resolve; });
      const bridge = await api.attachCandidateNativeRejections({ context: { newCDPSession: () => pending },
        page: { addInitScript: async () => {}, isClosed: () => true }, collector: value, timeoutMs: 20 });
      assert(snapshot(value).summary.failures.includes('setup_timeout'));
      deliver(session); await until(() => session.detaches === 1, 'late_session_not_detached', 1000);
      await cleanup(bridge, []);
    }],
    ['open_page_and_teardown_timeout_are_incomplete_without_throwing', async () => {
      const value = api.createCandidateNativeRejectionCollector({ secrets: [SECRET] });
      const session = new FakeSession(method => method === 'Runtime.removeBinding' ? new Promise(() => {}) : Promise.resolve({}));
      const bridge = await api.attachCandidateNativeRejections({ context: { newCDPSession: async () => session },
        page: { addInitScript: async () => {}, isClosed: () => false }, collector: value, timeoutMs: 20 });
      await cleanup(bridge, []);
      const result = snapshot(value);
      assert(result.summary.failures.includes('page_disposal_incomplete')); assert(result.summary.failures.includes('teardown_timeout'));
      assert.equal(result.summary.diagnostics_complete, false);
    }],
  ];
}

async function browserCases(api, closer, run, outputDirectory, sources, receipt) {
  const require = createRequire(import.meta.url), root = '/opt/goby-test/inactive-dependencies-m5h/node_modules/playwright';
  process.env.PLAYWRIGHT_BROWSERS_PATH ??= '/root/.cache/ms-playwright';
  const { chromium } = require(root), playwrightVersion = require(root + '/package.json').version;
  let browser, context, redirectServer, browserOrigin;
  const serverSockets = new Set();
  Object.assign(receipt, { kind: 'synthetic-native-rejection-browser-verification', version: 1, browserLaunchAttempted: false, browserRuns: 0, browserClosed: false,
    source: sources.adapter, playwrightVersion, browserVersion: null, browserType: 'chromium', headless: true,
    pagesCreated: 0, syntheticRequestsFulfilled: 0, unexpectedRequestsDenied: 0, contextsClosed: false, closeCompleted: false,
    browserConnectedAfterClose: null, closeFailure: null, cases: [], businessRequests: 0, candidateAccess: false, referenceAccess: false, clientAcceptance: false,
    selfOwnedRedirectServer: { ownerPid: process.pid, host: '127.0.0.1', port: null, started: false, closed: false,
      requests: [], unexpectedRequests: 0, routeContinuations: 0, remainingSockets: 0, closeFailure: null } });
  const pageCase = async (name, action) => {
    const safe = { name, phase: 'page_creation', failed: true, native: null, pageErrorCount: 0,
      originalPageErrors: [], unhandledEventCount: null, defaultPrevented: null };
    receipt.cases.push(safe);
    const page = await bounded(context.newPage(), 10000, 'synthetic_page_creation_timeout'); receipt.pagesCreated++;
    const secrets = [SECRET], requests = [], rawErrors = [], started = Date.now(); let ordinal = 0, bridge;
    let failure, failed = false, defaults = null, native, errors;
    const recordFailure = error => { if (!failed) failure = error; failed = true; };
    const byRequest = new WeakMap();
    const collector = api.createCandidateNativeRejectionCollector({ secrets, context: () => ({ elapsed_ms: Date.now() - started, phase: 'synthetic_browser', operation: 'native_rejection' }) });
    const pageErrors = api.createCandidatePageErrorCollector({ secrets, context: () => ({ elapsed_ms: Date.now() - started, phase: 'synthetic_browser', operation: 'native_rejection', location: page.url(), requests }) });
    page.on('pageerror', error => { rawErrors.push(error); pageErrors.record(error); });
    page.on('request', request => { const row = { ordinal: ++ordinal, url: request.url(), status: null, response_headers: {} }; requests.push(row); byRequest.set(request, row); });
    page.on('response', response => { const row = byRequest.get(response.request()); if (row) { row.status = response.status(); row.response_headers = response.headers(); } });
    try {
      safe.phase = 'attach_hook';
      bridge = await api.attachCandidateNativeRejections({ context, page, collector });
      safe.phase = 'page_navigation';
      await page.goto(browserOrigin + '/page', { waitUntil: 'load', timeout: 10000 });
      await until(() => collector.snapshot().summary.installed_contexts > 0, 'native_hook_not_installed');
      await page.evaluate(() => {
        globalThis.syntheticUnhandled = [];
        addEventListener('unhandledrejection', event => { syntheticUnhandled.push({ defaultPrevented: event.defaultPrevented, reasonType: typeof event.reason }); });
      });
      safe.phase = 'action';
      await bounded(action({ page, collector, secrets, requests, rawErrors, phase: value => { safe.phase = value; } }), 15000, 'synthetic_action_timeout');
    } catch (error) {
      recordFailure(error);
    } finally {
      if (!failed) safe.phase = 'close_page';
      if (!page.isClosed()) {
        try { defaults = await bounded(page.evaluate(() => Array.isArray(globalThis.syntheticUnhandled) ? syntheticUnhandled.map(row => row.defaultPrevented) : null), 2000, 'synthetic_monitor_timeout'); }
        catch (error) { recordFailure(error); }
        try { await bounded(page.close(), 5000, 'synthetic_page_close_timeout'); }
        catch (error) { recordFailure(error); }
      }
      if (!failed) safe.phase = 'dispose_bridge';
      if (bridge) try { await bounded(bridge.dispose(), 2000, 'browser_bridge_disposal_timeout'); } catch (error) { recordFailure(error); }
      // Failure snapshots still use the complete set of known synthetic secrets.
      api.registerCandidateSecret(secrets, LATE);
      if (!failed) safe.phase = 'snapshot_compatibility';
      try {
        native = snapshot(collector, requests); errors = pageErrors.snapshot({ authorityComplete: true }).events;
        Object.assign(safe, { native, pageErrorCount: rawErrors.length, originalPageErrors: errors,
          unhandledEventCount: defaults?.length ?? null, defaultPrevented: defaults === null ? null : defaults.some(Boolean) });
        assert(Array.isArray(defaults) && defaults.every(value => value === false), 'native_hook_prevented_default');
        assertReadable(closer, native);
      } catch (error) { recordFailure(error); }
      safe.failed = failed;
      if (!failed) safe.phase = 'complete';
    }
    if (failed) throw failure;
    return { native, errors };
  };
  try {
    // Playwright routes only the initial redirected request. These two GETs
    // therefore use a real server owned solely by this test in its private namespace.
    const serverState = receipt.selfOwnedRedirectServer;
    redirectServer = http.createServer({ requestTimeout: 2000, headersTimeout: 2000, keepAliveTimeout: 1000 }, (request, response) => {
      const route = request.url;
      if (request.method !== 'GET' || !['/redirect', '/redirect-final'].includes(route)) {
        serverState.unexpectedRequests++; response.writeHead(404, { Connection: 'close', 'Content-Length': '0' }); response.end(); return;
      }
      const first = route === '/redirect', body = first ? '' : 'synthetic redirect final response';
      const row = { method: 'GET', path: route, status: first ? 302 : 404, responseFinished: false };
      serverState.requests.push(row);
      response.writeHead(row.status, { 'Access-Control-Allow-Origin': browserOrigin, 'Content-Type': 'text/plain',
        'Content-Length': Buffer.byteLength(body), 'Cache-Control': 'no-store', Connection: 'close',
        'X-Request-Id': first ? 'synthetic-loopback-redirect' : 'synthetic-loopback-final', ...(first ? { Location: '/redirect-final' } : {}) });
      response.end(body, () => { row.responseFinished = true; });
    });
    redirectServer.on('connection', socket => { serverSockets.add(socket); socket.on('close', () => serverSockets.delete(socket)); });
    await bounded(new Promise((resolve, reject) => {
      redirectServer.once('error', reject);
      redirectServer.listen(0, '127.0.0.1', () => { redirectServer.off('error', reject); resolve(); });
    }), 5000, 'synthetic_redirect_server_start_timeout');
    const address = redirectServer.address();
    assert(address && typeof address !== 'string' && address.address === '127.0.0.1');
    serverState.port = address.port; serverState.started = true; browserOrigin = 'http://127.0.0.1:' + address.port;
    receipt.browserLaunchAttempted = true;
    browser = await chromium.launch({ headless: true, timeout: 20000, args: ['--disable-quic', '--force-webrtc-ip-handling-policy=disable_non_proxied_udp'] });
    receipt.browserRuns++;
    receipt.browserVersion = browser.version();
    context = await bounded(browser.newContext({ serviceWorkers: 'block' }), 10000, 'synthetic_context_creation_timeout');
    await context.route('**/*', async route => {
      const url = new URL(route.request().url());
      if (url.origin === browserOrigin && route.request().method() === 'GET' && url.search === '' && ['/redirect', '/redirect-final'].includes(url.pathname)) {
        serverState.routeContinuations++; await route.continue(); return;
      }
      if (![browserOrigin, OTHER].includes(url.origin)) { receipt.unexpectedRequestsDenied++; await route.abort(); return; }
      const headers = { 'Access-Control-Allow-Origin': '*', 'Content-Type': 'text/plain', 'X-Request-Id': 'synthetic-' + url.pathname.replaceAll('/', '-') };
      if (url.pathname === '/page') { receipt.syntheticRequestsFulfilled++; await route.fulfill({ status: 200, contentType: 'text/html', body: '<!doctype html><title>Synthetic native rejection</title><link rel="icon" href="data:,">' }); return; }
      if (!['/handled-404', '/response/' + LATE, '/opaque'].includes(url.pathname)) { receipt.unexpectedRequestsDenied++; await route.abort(); return; }
      receipt.syntheticRequestsFulfilled++; await route.fulfill({ status: 404, headers, body: 'synthetic response' });
    });
    await run('browser_native_response_handled_control_pageerror_and_late_redaction', async () => {
      const result = await pageCase('response', async ({ page, collector, secrets, requests, rawErrors }) => {
        assert.equal(await page.evaluate(async () => { const response = await fetch('/handled-404'); await Promise.reject(response).catch(() => {}); return response.status; }), 404);
        await pause(100); assert.equal(collector.snapshot().summary.count, 0); assert.equal(rawErrors.length, 0);
        await page.evaluate(async late => { const response = await fetch('/response/' + late + '?api_key=' + late); void Promise.reject(response); }, LATE);
        await until(() => collector.snapshot().summary.count === 1 && rawErrors.length > 0, 'response_rejection_not_observed');
        assert(api.registerCandidateSecret(secrets, LATE));
        await until(() => snapshot(collector, requests).events[0]?.association.state === 'matched', 'response_request_not_bound');
      });
      assert.equal(result.native.summary.diagnostics_complete, true);
      assert(!JSON.stringify(result).includes(LATE)); assert(!JSON.stringify(result).includes('api_key='));
      assertOriginalUIFailure(closer, result.native, result.errors);
    });
    await run('browser_native_undefined_and_string_undefined_propagate', async () => {
      const result = await pageCase('undefined', async ({ page, collector, rawErrors }) => {
        await page.evaluate(() => { void Promise.reject(undefined); void Promise.reject('undefined'); });
        await until(() => collector.snapshot().summary.count === 2 && rawErrors.length >= 2, 'undefined_rejections_not_observed');
      });
      assert(result.native.events.some(row => row.reason.type === 'undefined' && row.reason.value === null));
      assert(result.native.events.some(row => row.reason.type === 'string' && row.reason.value === 'undefined'));
      assert.equal(result.native.summary.diagnostics_complete, true);
    });
    await run('browser_native_hostile_reason_is_not_inspected_or_stringified', async () => {
      const result = await pageCase('hostile', async ({ page, collector }) => {
        await page.evaluate(secret => {
          globalThis.syntheticGetterCalls = 0;
          const value = Object.create(null), fail = () => { syntheticGetterCalls++; throw new Error(secret); };
          for (const key of ['name', 'message', 'stack', 'status', 'url', 'type', 'redirected', 'headers', 'toJSON', 'toString']) Object.defineProperty(value, key, { get: fail });
          Object.defineProperty(value, Symbol.toPrimitive, { get: fail });
          void Promise.reject(value);
        }, SECRET);
        await until(() => collector.snapshot().summary.count === 1, 'hostile_rejection_not_observed');
        assert.equal(await page.evaluate(() => syntheticGetterCalls), 0);
      });
      assert.deepEqual(result.native.events[0].reason, { kind: 'object' });
      assert(!JSON.stringify(result).includes(SECRET));
    });
    await run('browser_native_redirect_and_opaque_keep_native_metadata', async () => {
      const result = await pageCase('filtered_responses', async ({ page, collector, phase }) => {
        phase('fetch_redirect');
        await page.evaluate(async () => { const redirected = await fetch('/redirect'); void Promise.reject(redirected); });
        phase('fetch_opaque');
        await page.evaluate(async other => { const opaque = await fetch(other + '/opaque', { mode: 'no-cors' }); void Promise.reject(opaque); }, OTHER);
        phase('observe_filtered_rejections');
        await until(() => collector.snapshot().summary.count === 2, 'filtered_response_rejections_not_observed');
      });
      assert(result.native.events.some(row => row.reason.kind === 'response' && row.reason.redirected === true && row.reason.status === 404));
      const opaque = result.native.events.find(row => row.reason.type === 'opaque');
      assert(opaque); assert.equal(opaque.reason.status, 0); assert.equal(opaque.reason.request_id, null); assert.equal(opaque.association.state, 'unattributed');
    });
    await run('browser_native_overflow_is_bounded_without_suppressing_pageerrors', async () => {
      const result = await pageCase('overflow', async ({ page, collector, rawErrors }) => {
        await page.evaluate(() => { for (let index = 0; index < 20; index++) void Promise.reject('synthetic-overflow-' + index); });
        await until(() => collector.snapshot().summary.overflow_count > 0 && rawErrors.length >= 20, 'native_overflow_not_reported');
      });
      assert(result.native.events.length <= api.NATIVE_REJECTION_LIMITS.events);
      assert.equal(result.native.summary.count_is_lower_bound, true); assert.equal(result.native.summary.diagnostics_complete, false);
      assert(Buffer.byteLength(JSON.stringify(result.native)) <= api.NATIVE_REJECTION_LIMITS.bytes);
    });
    assert.equal(receipt.unexpectedRequestsDenied, 0);
    assert.deepEqual(serverState.requests.map(row => ({ method: row.method, path: row.path, status: row.status })), [
      { method: 'GET', path: '/redirect', status: 302 }, { method: 'GET', path: '/redirect-final', status: 404 },
    ]);
    assert(serverState.requests.every(row => row.responseFinished)); assert.equal(serverState.unexpectedRequests, 0);
  } finally {
    if (context) { await bounded(context.close(), 10000, 'synthetic_context_close_timeout').catch(() => {}); receipt.contextsClosed = browser?.contexts().length === 0; }
    if (browser) {
      try { await bounded(browser.close(), 15000, 'synthetic_browser_close_timeout'); receipt.closeCompleted = true; }
      catch { receipt.closeFailure = 'synthetic_browser_close_failed'; }
      finally { receipt.browserConnectedAfterClose = browser.isConnected(); receipt.browserClosed = receipt.closeCompleted && !receipt.browserConnectedAfterClose; }
    }
    if (redirectServer) {
      let closeCompleted = false;
      const closed = new Promise(resolve => { redirectServer.close(() => { closeCompleted = true; resolve(); }); });
      redirectServer.closeIdleConnections();
      try { await bounded(closed, 2000, 'synthetic_redirect_server_close_timeout'); }
      catch {
        for (const socket of serverSockets) socket.destroy();
        try { await bounded(closed, 2000, 'synthetic_redirect_server_force_close_timeout'); }
        catch { receipt.selfOwnedRedirectServer.closeFailure = 'synthetic_redirect_server_close_failed'; }
      }
      receipt.selfOwnedRedirectServer.remainingSockets = serverSockets.size;
      receipt.selfOwnedRedirectServer.closed = closeCompleted && !redirectServer.listening && serverSockets.size === 0;
    }
    await writeOnce(path.join(outputDirectory, 'native-browser-report.json'), receipt);
  }
  assert.equal(receipt.browserRuns, 1); assert.equal(receipt.browserClosed, true); assert.equal(receipt.selfOwnedRedirectServer.closed, true);
  return receipt;
}

async function writeOnce(filename, value) {
  const bytes = Buffer.from(JSON.stringify(value, null, 2) + '\n');
  const handle = await fs.open(filename, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
  try { await handle.writeFile(bytes); await handle.sync(); } finally { await handle.close(); }
  const directory = await fs.open(path.dirname(filename), constants.O_RDONLY | constants.O_DIRECTORY);
  try { await directory.sync(); } finally { await directory.close(); }
  return { path: filename, sha256: sha(bytes) };
}

async function main() {
  assert(process.platform === 'linux' && process.getuid?.() === 0 && process.env.SSH_CONNECTION, 'remote_test_environment_required');
  const args = process.argv.slice(2), withBrowser = args.length === 5 && args[4] === '--browser';
  assert((args.length === 4 || withBrowser) && args[0] === '--source' && args[2] === '--output', 'arguments_required');
  const source = path.resolve(args[1]), output = path.resolve(args[3]), own = fileURLToPath(import.meta.url);
  assert.equal(path.basename(source), 'client-browser-audited-candidate.mjs');
  const parent = await fs.lstat(path.dirname(output));
  assert(parent.isDirectory() && !parent.isSymbolicLink() && parent.uid === 0 && !(parent.mode & 0o022), 'protected_output_required');
  assert.equal(await fs.realpath(path.dirname(output)), path.dirname(output)); process.umask(0o077);
  const closerPath = path.join(path.dirname(source), 'close-audited-candidate-client.mjs');
  const sources = Object.fromEntries(await Promise.all([['adapter', source], ['test', own], ['closer', closerPath]].map(async ([role, filename]) => [role, { path: filename, sha256: sha(await fs.readFile(filename)) }])));
  const api = await import(pathToFileURL(source).href), closer = await import(pathToFileURL(closerPath).href), tests = [];
  const run = async (name, action) => {
    try { await action(); tests.push({ name, outcome: 'passed' }); }
    catch (error) { tests.push({ name, outcome: 'failed', errorType: error?.name ?? 'NonError',
      failedCheck: /^[a-zA-Z0-9_]{1,80}$/.test(error?.message ?? '') ? error.message : 'assertion_failed' }); }
  };
  for (const [name, action] of [...pureCases(api, closer), ...bridgeCases(api)]) await run(name, action);
  const browserReport = withBrowser ? {} : null;
  if (withBrowser) await run('synthetic_browser_scope_and_final_cleanup', async () => { await browserCases(api, closer, run, path.dirname(output), sources, browserReport); });
  const sourceUnchanged = (await Promise.all(Object.values(sources).map(async pin => sha(await fs.readFile(pin.path)) === pin.sha256))).every(Boolean);
  const report = { kind: 'client-native-rejection-verification', version: 1, sources, testCount: tests.length,
    passed: tests.filter(row => row.outcome === 'passed').length, failed: tests.filter(row => row.outcome === 'failed').length, sourceUnchanged, tests,
    browserRequested: withBrowser, browserReport: withBrowser ? { path: path.join(path.dirname(output), 'native-browser-report.json') } : null,
    browserLaunchAttempted: browserReport?.browserLaunchAttempted ?? false,
    browserRuns: browserReport?.browserRuns ?? (withBrowser ? null : 0), browserClosed: browserReport?.browserClosed ?? null,
    businessRequests: 0, candidateAccess: false, referenceAccess: false, clientAcceptance: false };
  const pin = await writeOnce(output, report);
  process.stdout.write(JSON.stringify({ ...pin, testCount: report.testCount, passed: report.passed, failed: report.failed, sourceUnchanged }) + '\n');
  if (report.failed || !sourceUnchanged || withBrowser && !report.browserClosed) process.exitCode = 1;
}

try { await main(); } catch { process.stderr.write('native_rejection_verification_setup_or_publication_failed\n'); process.exitCode = 1; }
