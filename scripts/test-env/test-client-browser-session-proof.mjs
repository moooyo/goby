import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { EventEmitter } from 'node:events';
import http from 'node:http';
import test from 'node:test';

import { createBrowserSessionProof } from './client-browser-session-proof.mjs';

const LINUX_ONLY = { skip: process.platform !== 'linux', timeout: 10000 };
const LOGOUT_ROUTE = '/emby/Sessions/Logout';
const INFO_ROUTE = '/emby/System/Info';
const PROOF_SOURCE = 'independent-node-http-post-logout-verification';
const PASS = 'all_observed_logout_tokens_rejected';
const INCOMPLETE = 'logout_token_proof_incomplete';
const ENTRY_PASS = 'logout_token_rejected';
const TEST_LIMITS = {
  captureTimeoutMs: 1500,
  logoutWaitMs: 500,
  httpTimeoutMs: 1000,
  httpIdleTimeoutMs: 500,
  responseByteLimit: 1024,
  responseHeaderByteLimit: 2048,
};

function fingerprint(value) {
  return createHash('sha256').update(value).digest('hex');
}

function deferred() {
  let resolve;
  const promise = new Promise(complete => { resolve = complete; });
  return { promise, resolve };
}

async function withDeadline(promise, milliseconds, message) {
  let timer;
  try {
    return await Promise.race([
      promise,
      new Promise((_, reject) => { timer = setTimeout(() => reject(new Error(message)), milliseconds); }),
    ]);
  } finally {
    clearTimeout(timer);
  }
}

class FakeContext extends EventEmitter {
  constructor(openPages = []) {
    super();
    this.openPages = openPages;
  }

  pages() {
    return this.openPages;
  }
}

function makeRequest(target, {
  route = LOGOUT_ROUTE,
  method = 'POST',
  headers = {},
  headersPromise,
} = {}) {
  const url = new URL(route, target).href;
  return {
    url: () => url,
    method: () => method,
    allHeaders: () => headersPromise ?? Promise.resolve({ ...headers }),
    failure: () => ({ errorText: 'net::ERR_ABORTED' }),
  };
}

function emitResponse(context, request, status) {
  // Deliberately omit body APIs and finished(): only the observed status is needed.
  context.emit('response', { request: () => request, status: () => status });
}

function emitLogout(context, request, status = 204) {
  context.emit('request', request);
  emitResponse(context, request, status);
  context.emit('requestfinished', request);
}

function incomingSummary(request) {
  const url = new URL(request.url, 'http://127.0.0.1');
  let token = null;
  let tokenSource = null;
  for (const name of ['x-emby-token', 'x-mediabrowser-token']) {
    if (typeof request.headers[name] === 'string') {
      token = request.headers[name];
      tokenSource = `header:${name}`;
      break;
    }
  }
  if (token === null) {
    for (const name of ['authorization', 'x-emby-authorization']) {
      const value = request.headers[name];
      const match = typeof value === 'string' ? /\bToken\s*=\s*"([^"]+)"/i.exec(value) : null;
      if (match) {
        token = match[1];
        tokenSource = `header:${name}:token`;
        break;
      }
    }
  }
  if (token === null) {
    for (const name of ['api_key', 'x-emby-token', 'x-mediabrowser-token']) {
      const value = url.searchParams.get(name);
      if (value !== null) {
        token = value;
        tokenSource = `query:${name}`;
        break;
      }
    }
  }
  return {
    method: request.method,
    fixedRoute: url.pathname === INFO_ROUTE,
    tokenPresent: token !== null,
    tokenFingerprint: token === null ? null : fingerprint(token),
    tokenSource,
    cookiePresent: request.headers.cookie !== undefined,
    unrelatedQueryPresent: url.searchParams.has('ignored_parameter'),
    clientFingerprint: typeof request.headers['x-emby-client'] === 'string'
      ? fingerprint(request.headers['x-emby-client']) : null,
  };
}

async function startPeer(responder = (_request, response) => response.writeHead(401).end()) {
  const requests = [];
  const sockets = new Set();
  const arrivals = new EventEmitter();
  const server = http.createServer((request, response) => {
    const summary = incomingSummary(request);
    requests.push(summary);
    request.resume();
    responder(request, response, summary);
    arrivals.emit('arrival');
  });
  server.on('connection', socket => {
    sockets.add(socket);
    socket.once('close', () => sockets.delete(socket));
  });
  server.on('clientError', (_error, socket) => socket.destroy());
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => {
      server.off('error', reject);
      resolve();
    });
  });
  const target = `http://127.0.0.1:${server.address().port}`;
  let closed = false;
  return {
    target,
    requests,
    async waitForCount(count) {
      if (requests.length >= count) return;
      let onArrival;
      try {
        await withDeadline(new Promise(resolve => {
          onArrival = () => { if (requests.length >= count) resolve(); };
          arrivals.on('arrival', onArrival);
          onArrival();
        }), 2000, 'The loopback peer did not receive the expected request count.');
      } finally {
        arrivals.off('arrival', onArrival);
      }
    },
    async close() {
      if (closed) return;
      closed = true;
      const closing = new Promise(resolve => server.close(() => resolve()));
      for (const socket of sockets) socket.destroy();
      await closing;
    },
  };
}

async function fixture(t, responder, limits = {}) {
  const peer = await startPeer(responder);
  let proof;
  t.after(async () => {
    // Close held responses first so cleanup also terminates failing HTTP checks.
    await peer.close();
    if (proof) await withDeadline(proof.dispose(), 3000, 'Session proof cleanup did not settle.');
  });
  const context = new FakeContext();
  proof = createBrowserSessionProof({ context, target: peer.target, limits: { ...TEST_LIMITS, ...limits } });
  return { context, proof, peer };
}

function assertNoSensitiveReport(report, values) {
  const serialized = JSON.stringify(report);
  for (const value of values) {
    assert.equal(serialized.includes(value), false,
      'The public report must not contain authority values or original request URLs.');
  }
}

function assertIndependent(entry, { status, eligible }) {
  assert.equal(entry.verification.source, PROOF_SOURCE);
  assert.equal(entry.verification.is_ui_request, false);
  assert.equal(entry.verification.method, 'GET');
  assert.equal(entry.verification.route, INFO_ROUTE);
  assert.equal(entry.verification.status, status);
  assert.equal(entry.verification.eligible_at_request_start, eligible);
}

function assertPeerRequest(summary, token) {
  assert.equal(summary.method, 'GET');
  assert.equal(summary.fixedRoute, true);
  assert.equal(summary.tokenPresent, true);
  assert.equal(summary.tokenFingerprint, fingerprint(token));
}

test('requires a context before its first page and rejects unsafe targets or relaxed limits', LINUX_ONLY, () => {
  assert.throws(() => createBrowserSessionProof({
    context: new FakeContext([{}]), target: 'http://127.0.0.1:1',
  }));
  for (const target of [
    'http://example.invalid',
    'http://127.0.0.1.example.invalid',
    'https://127.0.0.1',
    'http://user:password@127.0.0.1',
  ]) {
    assert.throws(() => createBrowserSessionProof({ context: new FakeContext(), target }));
  }
  for (const limits of [
    { captureTimeoutMs: 1501 }, { logoutWaitMs: 12001 }, { httpTimeoutMs: 8001 },
    { httpIdleTimeoutMs: 3001 }, { responseByteLimit: 65537 }, { responseHeaderByteLimit: 16385 },
    { httpTimeoutMs: 0 }, { httpTimeoutMs: 1.5 }, { unsupported: 1 },
  ]) {
    assert.throws(() => createBrowserSessionProof({
      context: new FakeContext(), target: 'http://127.0.0.1:1', limits,
    }));
  }
});

test('does not pass an empty observation and detaches all listeners on disposal', LINUX_ONLY, async t => {
  const { context, proof, peer } = await fixture(t);
  const report = await proof.drain();
  assert.equal(report, proof.report);
  assert.equal(report.outcome, 'no_logout_observed');
  assert.equal(report.entries.length, 0);
  assert.equal(peer.requests.length, 0);
  await proof.dispose();
  for (const event of ['request', 'response', 'requestfailed', 'requestfinished']) {
    assert.equal(context.listenerCount(event), 0);
  }
  emitLogout(context, makeRequest(peer.target, { headers: { 'x-emby-token': 'ignored-after-disposal' } }));
  assert.equal((await proof.drain()).entries.length, 0);
  assert.equal(peer.requests.length, 0);
});

test('binds concurrent logout authority, response status, and failure flags to the original request', LINUX_ONLY, async t => {
  const tokenA = 'synthetic-session-alpha';
  const tokenB = 'synthetic-session-beta';
  const headersA = deferred();
  const { context, proof, peer } = await fixture(t, (_request, response, summary) => {
    response.writeHead(summary.tokenFingerprint === fingerprint(tokenA) ? 401 : 403).end();
  });
  const requestA = makeRequest(peer.target, { headersPromise: headersA.promise });
  const requestB = makeRequest(peer.target, { route: '/Sessions/Logout', headers: { 'x-emby-token': tokenB } });
  context.emit('request', requestA);
  context.emit('request', requestB);
  emitResponse(context, requestA, 204);
  context.emit('requestfailed', requestA);
  emitResponse(context, requestB, 503);
  context.emit('requestfinished', requestB);
  await peer.waitForCount(1);
  assertPeerRequest(peer.requests[0], tokenB);
  headersA.resolve({ 'x-emby-token': tokenA });
  const report = await proof.drain();
  assert.equal(report.outcome, INCOMPLETE);
  assert.equal(report.entries.length, 2);
  assert.equal(peer.requests.length, 2);
  assertPeerRequest(peer.requests[1], tokenA);
  const entryA = report.entries.find(entry => entry.ui_request.route === LOGOUT_ROUTE);
  const entryB = report.entries.find(entry => entry.ui_request.route === '/Sessions/Logout');
  assert.equal(entryA.token_fingerprint, fingerprint(tokenA));
  assert.equal(entryA.ui_request.response_status, 204);
  assert.equal(entryA.ui_request.client_request_failed, true);
  assertIndependent(entryA, { status: 401, eligible: true });
  assert.equal(entryA.result, ENTRY_PASS);
  assert.equal(entryB.token_fingerprint, fingerprint(tokenB));
  assert.equal(entryB.ui_request.response_status, 503);
  assert.equal(entryB.ui_request.client_request_failed, false);
  assertIndependent(entryB, { status: 403, eligible: false });
  assert.equal(entryB.result, INCOMPLETE);
  assertNoSensitiveReport(report, [tokenA, tokenB, requestA.url(), requestB.url()]);
});

for (const authority of [
  { name: 'token header', source: 'header:x-emby-token', headers: token => ({ 'x-emby-token': token }) },
  { name: 'query-only token', source: 'query:api_key', query: true, headers: () => ({}) },
  { name: 'Emby authorization', source: 'header:x-emby-authorization:token',
    headers: token => ({ 'x-emby-authorization': `Emby Client="private-auth-client", DeviceId="private-auth-device", Token="${token}"` }) },
  { name: 'MediaBrowser authorization', source: 'header:authorization:token',
    headers: token => ({ authorization: `MediaBrowser Client="private-auth-client", Token="${token}"` }) },
]) {
  test(`replays the exact ${authority.name} without unrelated cookies or query values`, LINUX_ONLY, async t => {
    const token = 'synthetic-authority-token+percent%equals=';
    const privateClient = 'private-client-metadata';
    const privateCookie = 'private-cookie-value';
    const privateQuery = 'private-unrelated-query';
    const { context, proof, peer } = await fixture(t);
    const url = new URL(LOGOUT_ROUTE, peer.target);
    url.searchParams.set('ignored_parameter', privateQuery);
    if (authority.query) url.searchParams.set('api_key', token);
    const request = makeRequest(peer.target, {
      route: url.href,
      headers: { ...authority.headers(token), 'x-emby-client': privateClient, cookie: `session=${privateCookie}` },
    });
    emitLogout(context, request);
    const report = await proof.drain();
    assert.equal(report.outcome, PASS);
    assert.equal(report.entries.length, 1);
    assert.equal(peer.requests.length, 1);
    assertPeerRequest(peer.requests[0], token);
    assert.equal(peer.requests[0].tokenSource, authority.source);
    assert.equal(peer.requests[0].cookiePresent, false);
    assert.equal(peer.requests[0].unrelatedQueryPresent, false);
    assert.equal(peer.requests[0].clientFingerprint, fingerprint(privateClient));
    assert.deepEqual(report.entries[0].token_sources, [authority.source]);
    assert.equal(report.entries[0].token_fingerprint, fingerprint(token));
    assertIndependent(report.entries[0], { status: 401, eligible: true });
    assert.equal(report.entries[0].result, ENTRY_PASS);
    assertNoSensitiveReport(report, [token, privateClient, privateCookie, privateQuery,
      'private-auth-client', 'private-auth-device', request.url()]);
  });
}

for (const conflict of [
  { name: 'conflicting header and query tokens', headers: { 'x-emby-token': 'synthetic-first' }, query: 'synthetic-second' },
  { name: 'a coalesced duplicate token header', headers: { 'x-emby-token': 'synthetic-first, synthetic-second' } },
  { name: 'duplicate authorization Token attributes',
    headers: { 'x-emby-authorization': 'Emby Token="synthetic-first", Token="synthetic-second"' } },
]) {
  test(`rejects ${conflict.name} before making an independent request`, LINUX_ONLY, async t => {
    const { context, proof, peer } = await fixture(t);
    const url = new URL(LOGOUT_ROUTE, peer.target);
    if (conflict.query) url.searchParams.set('api_key', conflict.query);
    const request = makeRequest(peer.target, { route: url.href, headers: conflict.headers });
    emitLogout(context, request);
    const report = await proof.drain();
    assert.equal(report.outcome, INCOMPLETE);
    assert.equal(report.entries.length, 1);
    assert.equal(report.entries[0].ui_request.response_status, 204);
    assert.equal(report.entries[0].verification.status, null);
    assert.equal(report.entries[0].verification.result, 'logout_authority_unavailable_or_ambiguous');
    assert.equal(report.entries[0].result, INCOMPLETE);
    assert.equal(peer.requests.length, 0);
    assertNoSensitiveReport(report, ['synthetic-first', 'synthetic-second', request.url()]);
  });
}

test('ignores another origin, unrelated routes, and requests that are not POST', LINUX_ONLY, async t => {
  const { context, proof, peer } = await fixture(t);
  const otherPeer = await startPeer();
  t.after(() => otherPeer.close());
  const headers = { 'x-emby-token': 'synthetic-ignored-token' };
  emitLogout(context, makeRequest(otherPeer.target, { headers }));
  emitLogout(context, makeRequest(peer.target, { route: '/emby/System/Info', headers }));
  emitLogout(context, makeRequest(peer.target, { method: 'GET', headers }));
  const report = await proof.drain();
  assert.equal(report.outcome, 'no_logout_observed');
  assert.equal(report.entries.length, 0);
  assert.equal(peer.requests.length, 0);
  assert.equal(otherPeer.requests.length, 0);
});

test('accepts a 2xx logout followed by ERR_ABORTED and starts only one independent request', LINUX_ONLY, async t => {
  const token = 'synthetic-aborted-client-token';
  const { context, proof, peer } = await fixture(t);
  const request = makeRequest(peer.target, { headers: { 'x-emby-token': token } });
  context.emit('request', request);
  emitResponse(context, request, 200);
  context.emit('requestfailed', request);
  context.emit('requestfailed', request);
  context.emit('requestfinished', request);
  emitResponse(context, request, 200);
  const report = await proof.drain();
  assert.equal(report.outcome, PASS);
  assert.equal(report.entries.length, 1);
  assert.equal(peer.requests.length, 1);
  assertPeerRequest(peer.requests[0], token);
  assert.equal(report.entries[0].ui_request.response_status, 200);
  assert.equal(report.entries[0].ui_request.client_request_failed, true);
  assertIndependent(report.entries[0], { status: 401, eligible: true });
  assert.equal(report.entries[0].result, ENTRY_PASS);
});

for (const observation of [
  { name: 'request failure without a response', status: null, failure: true },
  { name: 'request completion without a response', status: null, finished: true },
  { name: 'a non-2xx logout response', status: 503 },
  { name: 'the logout response deadline', status: null },
]) {
  test(`performs only diagnostic verification after ${observation.name}`, LINUX_ONLY, async t => {
    const { context, proof, peer } = await fixture(t, undefined, { logoutWaitMs: 80 });
    const request = makeRequest(peer.target, { headers: { 'x-emby-token': 'synthetic-diagnostic-token' } });
    context.emit('request', request);
    if (observation.status !== null) emitResponse(context, request, observation.status);
    if (observation.failure) context.emit('requestfailed', request);
    if (observation.finished) context.emit('requestfinished', request);
    const report = await proof.drain();
    assert.equal(report.outcome, INCOMPLETE);
    assert.equal(report.entries.length, 1);
    assert.equal(peer.requests.length, 1);
    assert.equal(report.entries[0].ui_request.response_status, observation.status);
    assertIndependent(report.entries[0], { status: 401, eligible: false });
    assert.equal(report.entries[0].verification.result, 'token_rejected');
    assert.equal(report.entries[0].result, INCOMPLETE);
  });
}

test('does not upgrade an earlier diagnostic GET when a 2xx logout response arrives later', LINUX_ONLY, async t => {
  let heldResponse;
  const { context, proof, peer } = await fixture(t, (_request, response) => { heldResponse = response; });
  const request = makeRequest(peer.target, { headers: { 'x-emby-token': 'synthetic-late-response-token' } });
  context.emit('request', request);
  context.emit('requestfailed', request);
  await peer.waitForCount(1);
  emitResponse(context, request, 204);
  context.emit('requestfinished', request);
  heldResponse.writeHead(401).end();
  const report = await proof.drain();
  assert.equal(report.outcome, INCOMPLETE);
  assert.equal(peer.requests.length, 1);
  assert.equal(report.entries[0].ui_request.response_status, 204);
  assertIndependent(report.entries[0], { status: 401, eligible: false });
  assert.equal(report.entries[0].result, INCOMPLETE);
});

for (const declaredLength of [true, false]) {
  test(`rejects an oversized 401 response with ${declaredLength ? 'Content-Length' : 'an unfinished chunked body'}`, LINUX_ONLY, async t => {
    const { context, proof, peer } = await fixture(t, (_request, response) => {
      if (declaredLength) {
        response.writeHead(401, { 'Content-Length': 2048 }).end(Buffer.alloc(2048, 'x'));
      } else {
        response.writeHead(401, { 'Transfer-Encoding': 'chunked' });
        response.write(Buffer.alloc(2048, 'x'));
      }
    }, { responseByteLimit: 256 });
    emitLogout(context, makeRequest(peer.target, { headers: { 'x-emby-token': 'synthetic-large-body-token' } }));
    const report = await proof.drain();
    assert.equal(report.outcome, INCOMPLETE);
    assert.equal(peer.requests.length, 1);
    assertIndependent(report.entries[0], { status: 401, eligible: true });
    assert.equal(report.entries[0].verification.result, 'response_exceeds_limit');
    assert.equal(report.entries[0].result, INCOMPLETE);
  });
}

test('rejects a 401 response whose headers exceed the configured byte limit', LINUX_ONLY, async t => {
  const { context, proof, peer } = await fixture(t, (_request, response) => {
    response.writeHead(401, { 'X-Synthetic-Padding': 'x'.repeat(4096) }).end();
  }, { responseHeaderByteLimit: 1024 });
  emitLogout(context, makeRequest(peer.target, { headers: { 'x-emby-token': 'synthetic-large-header-token' } }));
  const report = await proof.drain();
  assert.equal(report.outcome, INCOMPLETE);
  assert.equal(peer.requests.length, 1);
  assert.equal(report.entries[0].verification.result, 'transport_failed');
  assert.equal(report.entries[0].result, INCOMPLETE);
});

test('rejects a redirect without following its location', LINUX_ONLY, async t => {
  const { context, proof, peer } = await fixture(t, (_request, response, summary) => {
    if (summary.fixedRoute) response.writeHead(302, { Location: '/redirect-must-not-be-followed' }).end();
    else response.writeHead(401).end();
  });
  emitLogout(context, makeRequest(peer.target, { headers: { 'x-emby-token': 'synthetic-redirect-token' } }));
  const report = await proof.drain();
  assert.equal(report.outcome, INCOMPLETE);
  assert.equal(peer.requests.length, 1);
  assert.equal(peer.requests.every(request => request.fixedRoute), true);
  assertIndependent(report.entries[0], { status: 302, eligible: true });
  assert.equal(report.entries[0].verification.result, 'redirect_rejected');
});

test('enforces the idle timeout after receiving 401 headers with an unfinished body', LINUX_ONLY, async t => {
  const { context, proof, peer } = await fixture(t, (_request, response) => {
    response.writeHead(401, { 'Transfer-Encoding': 'chunked' });
    response.flushHeaders();
    const fallback = setTimeout(() => response.end(), 900);
    response.once('close', () => clearTimeout(fallback));
  }, { httpTimeoutMs: 1500, httpIdleTimeoutMs: 100 });
  emitLogout(context, makeRequest(peer.target, { headers: { 'x-emby-token': 'synthetic-idle-timeout-token' } }));
  const report = await proof.drain();
  assert.equal(report.outcome, INCOMPLETE);
  assertIndependent(report.entries[0], { status: 401, eligible: true });
  assert.equal(report.entries[0].verification.result, 'request_idle_timeout');
});

test('enforces the total timeout while response bytes keep the connection active', LINUX_ONLY, async t => {
  const { context, proof, peer } = await fixture(t, (_request, response) => {
    response.writeHead(401, { 'Transfer-Encoding': 'chunked' });
    response.write('x');
    let chunks = 1;
    const interval = setInterval(() => {
      if (++chunks >= 40) {
        clearInterval(interval);
        response.end('x');
      } else response.write('x');
    }, 25);
    response.once('close', () => clearInterval(interval));
  }, { httpTimeoutMs: 200, httpIdleTimeoutMs: 700 });
  emitLogout(context, makeRequest(peer.target, { headers: { 'x-emby-token': 'synthetic-total-timeout-token' } }));
  const report = await proof.drain();
  assert.equal(report.outcome, INCOMPLETE);
  assertIndependent(report.entries[0], { status: 401, eligible: true });
  assert.equal(report.entries[0].verification.result, 'request_timeout');
});

for (const capture of ['timeout', 'rejection']) {
  test(`bounds a headers ${capture} without leaking its error or making a probe`, LINUX_ONLY, async t => {
    const secret = 'synthetic-private-capture-error';
    const { context, proof, peer } = await fixture(t, undefined, { captureTimeoutMs: 50 });
    const headersPromise = capture === 'timeout'
      ? new Promise(() => {}) : Promise.reject(new Error(secret));
    const request = makeRequest(peer.target, { headersPromise });
    emitLogout(context, request);
    const report = await proof.drain();
    assert.equal(report.outcome, INCOMPLETE);
    assert.equal(report.entries.length, 1);
    assert.equal(report.entries[0].verification.result, 'logout_authority_unavailable_or_ambiguous');
    assert.equal(report.entries[0].verification.status, null);
    assert.equal(peer.requests.length, 0);
    assertNoSensitiveReport(report, [secret, request.url()]);
  });
}
