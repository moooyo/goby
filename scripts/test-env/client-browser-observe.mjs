#!/usr/bin/env node
/** Observe an unmodified Emby Web Client on the remote Linux test environment. */

import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
import { createHash } from 'node:crypto';
import http from 'node:http';
import { createBrowserSessionProof } from './client-browser-session-proof.mjs';

const defaults = {
  url: 'http://127.0.0.1:18197/web/index.html',
  'playwright-module': '/opt/goby-test/inactive-dependencies-m5h/node_modules/playwright',
  'observe-ms': '15000',
  mode: 'observe-only',
  account: 'viewer',
  workflow: 'none',
  'runtime-exceptions': 'false',
  'preference-mode': 'round-trip',
};
const options = { ...defaults };
for (let index = 2; index < process.argv.length; index += 2) {
  const name = process.argv[index];
  const value = process.argv[index + 1];
  if (!name?.startsWith('--') || !value || !['url', 'output', 'playwright-module', 'observe-ms', 'mode', 'credentials', 'account', 'workflow', 'runtime-exceptions', 'preference-mode'].includes(name.slice(2))) {
    throw new Error('Unsupported observation argument.');
  }
  options[name.slice(2)] = value;
}

const target = new URL(options.url);
const duration = Number(options['observe-ms']);
if (options.credentials && options.mode === 'observe-only') options.mode = 'ui-login';
if (process.platform !== 'linux' || !options.output || !path.isAbsolute(options.output) ||
    !path.isAbsolute(options['playwright-module']) || target.protocol !== 'http:' ||
    !['127.0.0.1', 'localhost', '[::1]'].includes(target.hostname) || target.username || target.password ||
    target.search || !Number.isInteger(duration) || duration < 1000 || duration > 60000 ||
    !['observe-only', 'manual-login-form', 'ui-login'].includes(options.mode) ||
    options.account !== 'viewer' || (options.mode === 'ui-login') !== Boolean(options.credentials) ||
    !['false', 'true'].includes(options['runtime-exceptions']) ||
    !['round-trip', 'retain-for-restart', 'verify-restart-restore'].includes(options['preference-mode']) ||
    options['preference-mode'] !== 'round-trip' && options.workflow !== 'preferences' ||
    !['none', 'movie', 'preferences'].includes(options.workflow) || options.workflow !== 'none' && options.mode !== 'ui-login') {
  throw new Error('An explicit private Linux output path and loopback HTTP target are required.');
}
process.umask(0o077);
const parent = await fs.lstat(path.dirname(options.output));
if (!parent.isDirectory() || parent.isSymbolicLink() || parent.uid !== process.getuid() || (parent.mode & 0o077)) {
  throw new Error('The observation parent directory must be private and owned.');
}
await fs.mkdir(options.output, { mode: 0o700 });
const output = await fs.realpath(options.output);
const require = createRequire(import.meta.url);
const { chromium } = require(options['playwright-module']);
const packageInfo = require(path.join(options['playwright-module'], 'package.json'));
const knownSegments = new Set(('emby System Info Public Ping Endpoint Users AuthenticateByName Authenticate Sessions Logout ' +
  'Capabilities Full Playing Progress Stopped Items Root Views Latest Resume PlaybackInfo Videos Audio stream universal ' +
  'original master main hls Subtitles Stream Images Primary Backdrop Logo Thumb Shows NextUp Seasons Episodes Library ' +
  'VirtualFolders Query DisplayPreferences UserSettings TypedSettings Configuration Partial Playback BitrateTest ' +
  'Devices Auth Keys Intros ThemeMedia AdditionalParts SpecialFeatures RemoteImages Artists AlbumArtists Genres Tags ' +
  'Studios Persons Search Hints UserData FavoriteItems PlayedItems HideFromResume PlayQueue Branding Localization ' +
  'Css Culture Countries Options Languages Ratings socket embywebsocket').toLowerCase().split(' '));

function safeRoute(value) {
  try {
    const url = new URL(value);
    if (url.pathname.startsWith('/web/')) return '/web/{asset}';
    return url.pathname.split('/').map(segment => {
      if (!segment) return '';
      const base = segment.split('.')[0];
      return knownSegments.has(base.toLowerCase()) ? segment.replace(/[^A-Za-z0-9.]/g, '') : '{value}';
    }).join('/');
  } catch {
    return '{unavailable}';
  }
}

function normalizedOrigin(value) {
  const url = new URL(value);
  if (url.protocol === 'ws:') url.protocol = 'http:';
  if (url.protocol === 'wss:') url.protocol = 'https:';
  return url.origin;
}

function allowedNetworkURL(value) {
  try {
    const url = new URL(value);
    return ['data:', 'blob:'].includes(url.protocol) ||
      (['http:', 'https:', 'ws:', 'wss:'].includes(url.protocol) && normalizedOrigin(value) === target.origin);
  } catch { return false; }
}

function originKind(value) {
  try {
    const url = new URL(value);
    if (['data:', 'blob:'].includes(url.protocol)) return 'non-network';
    return normalizedOrigin(value) === target.origin ? 'target' : 'external';
  }
  catch { return 'unavailable'; }
}

function queryFieldNames(value) {
  const names = [];
  try {
    let examined = 0;
    for (const name of new URL(value).searchParams.keys()) {
      if (names.length >= 64 || examined++ >= 128) break;
      const safe = /^[A-Za-z][A-Za-z0-9_.-]{0,95}$/.test(name) ? name : '{redacted field name}';
      if (!names.includes(safe)) names.push(safe);
    }
  } catch { /* Invalid URLs have no inspectable field names. */ }
  return names;
}

function safeExternalRoute(value) {
  try {
    return new URL(value).pathname.split('/').map(segment => {
      if (!segment) return '';
      const safe = redactSecrets(segment);
      if (safe !== segment || /[0-9a-f]{16,}/i.test(segment) || /^[A-Za-z0-9_-]{32,}$/.test(segment)) return '{redacted value}';
      return /^[A-Za-z][A-Za-z0-9_.-]{0,95}$/.test(segment) ? segment : '{value}';
    }).join('/');
  } catch { return '{unavailable}'; }
}

function safeFailure(value) {
  const code = String(value ?? '').match(/^net::ERR_[A-Z0-9_]+$/);
  return code ? code[0] : 'browser_request_failed';
}

const report = {
  format: 1,
  mode: options.mode,
  started_at: new Date().toISOString(),
  requested_url: target.origin + target.pathname,
  playwright_version: packageInfo.version,
  observe_ms: duration,
  navigation: null,
  requests: [],
  request_overflow: 0,
  websocket_events: [],
  page_error_count: 0,
  page_errors: [],
  console_error_count: 0,
  console_errors: [],
  snapshots: [],
  blocked_external_count: 0,
  blocked_external: [],
  network_policy_guard_errors: 0,
  explicit_network_policy: {
    mode: 'selected-loopback-origin-only',
    target_origin: target.origin,
    allowed_non_network_schemes: ['data:', 'blob:'],
    websocket_origin_mapping: 'ws to http and wss to https, preserving host and port',
    external_http_action: 'Abort without replacement responses',
    external_socket_action: 'Deny-only local proxy; no upstream connection or successful WebSocket handshake',
    service_workers: 'Allowed; target-only socket bypass and the deny-only proxy also apply to worker traffic',
    local_responses_and_websocket_frames: 'Unmodified',
  },
  evidence_boundary: options.mode === 'observe-only'
    ? 'No login, client interaction, API substitute, response interception, or application writes were performed.'
    : options.mode === 'manual-login-form'
      ? 'Only the visible Manual Login button may be clicked; no credentials, login submission, internal API, or token injection are used.'
      : 'Credentials are entered only into the observed original client form. No login bodies, tokens, internal APIs, response rewriting, or token injection are used.',
  login: { attempted: false, request_observed: false, http_status: null, form_closed: false, result: 'not_attempted' },
  logout: { attempted: false, result: 'not_attempted', owned_session: 'not_created_by_this_observer' },
};
const entries = new WeakMap();
const blockedRequests = new WeakSet();
const MAX_ASSET_BYTES = 16 * 1024 * 1024;
const MAX_TOTAL_ASSET_BYTES = 64 * 1024 * 1024;
report.static_assets = [];
report.asset_hash_policy = {
  algorithm: 'SHA-256',
  byte_representation: 'Browser-decoded response entity bytes',
  per_asset_limit: MAX_ASSET_BYTES,
  total_limit: MAX_TOTAL_ASSET_BYTES,
  source_inspection: false,
  queries_and_headers_retained: false,
};
report.http_connection_policy = 'The acceptance proxy forces ordinary upstream HTTP Connection: close; this chain does not verify keepalive.';
let assetChain = Promise.resolve();
let assetBytes = 0;
let assetReadsStopped = false;
let authenticationDiagnostics = Promise.resolve();
let browser;
let context;
let page;
let failure = null;
let stage = 'observation';
let credentials = null;
let networkGuard = null;
let exceptionSession = null;
let exceptionChain = Promise.resolve();
let sessionProof = null;
const guardSockets = new Set();

function recordBlockedNetwork(value, method, resourceType, transport) {
  report.blocked_external_count += 1;
  if (report.blocked_external.length >= 256) return;
  let origin = '{unavailable}';
  try { origin = new URL(value).origin; } catch { /* Never retain a malformed raw URL. */ }
  report.blocked_external.push({ elapsed_ms: Date.now() - started, origin: redactSecrets(origin),
    route: safeExternalRoute(value), query_field_names: queryFieldNames(value), method,
    resource_type: resourceType, transport, action: 'blocked_without_upstream_connection' });
}

async function startNetworkGuard() {
  function deny(request, socket, websocket = false, tunnel = false) {
    let value = request.url ?? '';
    if (tunnel) value = `https://${value}/`;
    else if (websocket) value = value.replace(/^http:/, 'ws:').replace(/^https:/, 'wss:');
    if (allowedNetworkURL(value)) report.network_policy_guard_errors += 1;
    else recordBlockedNetwork(value, request.method ?? 'GET', websocket ? 'websocket' : tunnel ? 'tls-tunnel' : 'network', 'deny-only-proxy');
    socket.end('HTTP/1.1 403 Forbidden\r\nConnection: close\r\nContent-Length: 0\r\n\r\n');
  }
  networkGuard = http.createServer({ maxHeaderSize: 16384 }, (request, response) => {
    if (allowedNetworkURL(request.url ?? '')) report.network_policy_guard_errors += 1;
    else recordBlockedNetwork(request.url ?? '', request.method ?? 'GET', 'network', 'deny-only-proxy');
    response.writeHead(403, { Connection: 'close', 'Content-Length': '0' });
    response.end();
  });
  networkGuard.maxConnections = 32;
  networkGuard.maxHeadersCount = 64;
  networkGuard.headersTimeout = 5000;
  networkGuard.requestTimeout = 5000;
  networkGuard.on('connect', (request, socket) => deny(request, socket, false, true));
  networkGuard.on('upgrade', (request, socket) => deny(request, socket, true));
  networkGuard.on('clientError', (_error, socket) => socket.destroy());
  networkGuard.on('connection', socket => {
    guardSockets.add(socket);
    socket.on('error', () => {});
    socket.on('close', () => guardSockets.delete(socket));
  });
  await new Promise((resolve, reject) => {
    networkGuard.once('error', reject);
    networkGuard.listen(0, '127.0.0.1', resolve);
  });
  return `http://127.0.0.1:${networkGuard.address().port}`;
}

function redactSecrets(value) {
  if (typeof value === 'string') {
    if (!credentials) return value;
    let result = value;
    for (const secret of [credentials.password, credentials.username].filter(Boolean)) {
      result = result.split(secret).join('[redacted credential]');
    }
    return result;
  }
  if (Array.isArray(value)) return value.map(redactSecrets);
  if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).map(([key, content]) => [key, redactSecrets(content)]));
  return value;
}

function controlledPageError(error) {
  const name = /^[A-Za-z]{1,50}Error$/.test(String(error.name)) ? error.name : 'Error';
  const message = redactSecrets(String(error.message ?? ''))
    .replace(/(?:https?|wss?|file|blob|data):[^\s<>"']+/gi, '[redacted URL]')
    .replace(/\/\/[^\s<>"']+/g, '[redacted URL]')
    .replace(/\/(?:[^\s<>"'?]+\/)*[^\s<>"'?]*\?[^\s<>"']*/g, '[redacted URL]')
    .replace(/\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b/gi, '[redacted ID]')
    .replace(/\b[0-9a-f]{16,}\b/gi, '[redacted ID]')
    .replace(/\b[A-Za-z0-9_-]{40,}\b/g, '[redacted opaque value]')
    .slice(0, 1200);
  return { name, message, elapsed_ms: Date.now() - started };
}

function authorizationShape(value) {
  const initial = /^\s*([A-Za-z][A-Za-z0-9_-]{0,31})(?:\s+|$)/.exec(value);
  if (!initial) return { scheme: 'unrecognized', field_names: [] };
  if (!/^(?:Emby|MediaBrowser)$/i.test(initial[1])) return { scheme: initial[1], field_names: [] };
  const names = [];
  let position = initial[0].length;
  while (position < value.length && names.length < 32) {
    while (/[\s,]/.test(value[position] ?? '') && position < value.length) position += 1;
    const field = /^([A-Za-z][A-Za-z0-9_-]{0,63})\s*=\s*/.exec(value.slice(position));
    if (!field) break;
    names.push(field[1]);
    position += field[0].length;
    if (value[position] === '"') {
      position += 1;
      while (position < value.length) {
        if (value[position] === '\\') position += 2;
        else if (value[position++] === '"') break;
      }
    } else {
      while (position < value.length && value[position] !== ',') position += 1;
    }
  }
  return { scheme: initial[1], field_names: names };
}

function safePlaybackProfile(value) {
  const scalarFields = new Set(('SupportedMediaTypes MaxStaticBitrate MaxStreamingBitrate MusicStreamingTranscodingBitrate MaxStaticMusicBitrate ' +
    'Container AudioCodec VideoCodec Type Protocol EstimateContentLength EnableMpegtsM2TsMode TranscodeSeekInfo ' +
    'CopyTimestamps Context MaxAudioChannels MinSegments SegmentLength BreakOnNonKeyFrames AllowInterlacedVideoStreamCopy ' +
    'ManifestSubtitles MaxManifestSubtitles MaxWidth MaxHeight FillEmptySubtitleSegments Codec Condition Property Value ' +
    'IsRequired OrgPn MimeType Format Method DidlMode Language AllowChunkedResponse').split(' '));
  let nodes = 0;
  function visit(node, key, depth) {
    if (++nodes > 2048 || depth > 12) return '{inspection limit}';
    if (node === null) return null;
    if (Array.isArray(node)) return node.slice(0, 64).map(entry => visit(entry, key, depth + 1));
    if (typeof node === 'object') return Object.fromEntries(Object.entries(node).slice(0, 64).map(([name, content]) => [
      /^[A-Za-z][A-Za-z0-9_]{0,95}$/.test(name) ? name : '{redacted field name}', visit(content, name, depth + 1),
    ]));
    if (!scalarFields.has(key)) return `{${typeof node}}`;
    if (typeof node === 'boolean' || typeof node === 'number') return node;
    if (typeof node === 'string' && /^[A-Za-z0-9_,.+ /|-]{0,128}$/.test(node) &&
        !/[0-9a-f]{16,}/i.test(node) && !/[A-Za-z0-9_-]{40,}/.test(node)) return redactSecrets(node);
    return `{${typeof node}}`;
  }
  return { policy: 'Only bounded standard codec/profile scalar fields retain values. Names, IDs, unknown values, URLs, and opaque strings are masked; this is request evidence, not client source inspection.',
    profile: visit(value, '', 0), visited_nodes: nodes };
}

async function recordAuthenticationFailure(response, entry) {
  const diagnostic = entry.authentication_failure = { status: response.status(), result: 'unavailable' };
  const contentType = response.headers()['content-type'] ?? '';
  if (!/^(?:application\/json|text\/plain)(?:;|$)/i.test(contentType)) {
    diagnostic.result = 'unsupported_error_media_type';
    return;
  }
  const declared = response.headers()['content-length'];
  const encoding = response.headers()['content-encoding'];
  const transfer = response.headers()['transfer-encoding'];
  if (!declared || !/^\d+$/.test(declared) || Number(declared) > 8192 ||
      transfer || encoding && encoding.toLowerCase() !== 'identity') {
    diagnostic.result = 'error_body_not_provably_bounded';
    return;
  }
  let timer;
  try {
    const bytes = await Promise.race([
      response.body(),
      new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('bounded_authentication_diagnostic_timeout')), 3000); }),
    ]);
    if (bytes.length > 8192) { diagnostic.result = 'error_body_exceeds_limit'; return; }
    const safeText = value => controlledPageError({ name: 'AuthenticationError', message: value }).message.slice(0, 512);
    if (/^application\/json/i.test(contentType)) {
      const payload = JSON.parse(bytes.toString('utf8'));
      const status = payload?.ResponseStatus;
      diagnostic.result = 'json_error_fields_only';
      if (typeof status?.ErrorCode === 'string') diagnostic.error_code = safeText(status.ErrorCode);
      if (typeof status?.Message === 'string') diagnostic.message = safeText(status.Message);
    } else {
      diagnostic.result = 'plain_text_error';
      diagnostic.message = safeText(bytes.toString('utf8'));
    }
  } catch { diagnostic.result = 'error_body_unavailable_or_timed_out'; }
  finally { clearTimeout(timer); }
}

async function loadCredentials() {
  const filename = options.credentials;
  if (process.getuid() !== 0 || !path.isAbsolute(filename)) throw new Error('Invalid private credentials path.');
  const directory = path.dirname(filename);
  const directoryInfo = await fs.lstat(directory);
  if (!directoryInfo.isDirectory() || directoryInfo.isSymbolicLink() || directoryInfo.uid !== 0 ||
      (directoryInfo.mode & 0o077) || await fs.realpath(directory) !== path.resolve(directory)) {
    throw new Error('Invalid private credentials directory.');
  }
  const handle = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const info = await handle.stat();
    if (!info.isFile() || info.uid !== 0 || (info.mode & 0o777) !== 0o600 || info.nlink !== 1 ||
        info.size < 1 || info.size > 16384) throw new Error('Invalid private credentials file.');
    const buffer = Buffer.alloc(16385);
    const { bytesRead } = await handle.read(buffer, 0, buffer.length, 0);
    if (bytesRead !== info.size || bytesRead > 16384) throw new Error('Credentials changed while reading.');
    const parsed = JSON.parse(buffer.subarray(0, bytesRead).toString('utf8'));
    buffer.fill(0);
    const validAccount = account => account && typeof account.username === 'string' &&
      account.username.length > 0 && account.username.length <= 256 && typeof account.password === 'string' &&
      account.password.length <= 4096;
    if (!parsed || typeof parsed.marker !== 'string' || !parsed.marker || parsed.marker.length > 256 ||
        !validAccount(parsed.viewer) || !validAccount(parsed.admin) ||
        typeof parsed.base_url !== 'string' || typeof parsed.direct_url !== 'string') {
      throw new Error('Unsupported credentials schema.');
    }
    const bound = new URL(parsed.base_url);
    const direct = new URL(parsed.direct_url);
    if (bound.origin !== target.origin || bound.username || bound.password || bound.search || bound.hash ||
        direct.protocol !== 'http:' || !['127.0.0.1', 'localhost', '[::1]'].includes(direct.hostname) ||
        direct.username || direct.password || direct.search || direct.hash) {
      throw new Error('Credentials do not belong to the selected loopback origin.');
    }
    report.credentials = { account: options.account, bound_origin: bound.origin, owner_marker_present: true };
    return { username: parsed.viewer.username, password: parsed.viewer.password };
  } finally {
    await handle.close();
  }
}

function isLoginRequest(request) {
  const url = new URL(request.url());
  return request.method() === 'POST' && url.origin === target.origin &&
    /^(?:\/emby)?\/users\/authenticatebyname\/?$/i.test(url.pathname);
}

async function snapshot(name) {
  const data = await page.evaluate(() => ({
    title: document.title,
    text: (document.body?.innerText ?? '').slice(0, 30000),
    controls: [...document.querySelectorAll('input,button,select,a,[role="button"]')].slice(0, 180).map(element => ({
      tag: element.tagName.toLowerCase(),
      role: element.getAttribute('role'),
      type: element.getAttribute('type'),
      name: element.getAttribute('name'),
      id: element.id || null,
      label: element.getAttribute('aria-label'),
      placeholder: element.getAttribute('placeholder'),
      text: (element.innerText ?? '').trim().slice(0, 180),
      disabled: Boolean(element.disabled),
      visible: Boolean(element.getClientRects().length),
    })),
  }));
  const screenshot = `${name}.png`;
  const masks = [page.locator('input, textarea')];
  if (credentials) masks.push(page.getByText(credentials.username, { exact: true }));
  await page.screenshot({ path: path.join(output, screenshot), fullPage: true, timeout: 10000, mask: masks });
  await fs.chmod(path.join(output, screenshot), 0o600);
  report.snapshots.push(redactSecrets({ name, elapsed_ms: Date.now() - started, route: safeRoute(page.url()), ...data, screenshot }));
}

const started = Date.now();

async function hashAsset(response, index) {
  const url = new URL(response.url());
  const asset = { request_index: index, pathname: url.pathname, status: response.status(), result: null };
  report.static_assets.push(asset);
  if (assetReadsStopped || Date.now() - started > duration + 45000) {
    asset.result = 'read_deadline_reached';
    return;
  }
  const declared = response.headers()['content-length'];
  if (declared && (!/^\d+$/.test(declared) || Number(declared) > MAX_ASSET_BYTES)) {
    asset.result = 'declared_size_exceeds_limit_or_invalid';
    return;
  }
  // Reserve one full permitted entity before each read. The queue is serial,
  // and a timed-out read stops later reads instead of multiplying allocations.
  if (assetBytes + MAX_ASSET_BYTES > MAX_TOTAL_ASSET_BYTES) {
    asset.result = 'total_budget_reservation_unavailable';
    return;
  }
  let timer;
  try {
    const bytes = await Promise.race([
      response.body(),
      new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('asset_read_timeout')), 5000); }),
    ]);
    if (bytes.length > MAX_ASSET_BYTES || assetBytes + bytes.length > MAX_TOTAL_ASSET_BYTES) {
      asset.result = 'decoded_size_exceeds_limit';
      return;
    }
    asset.bytes = bytes.length;
    asset.sha256 = createHash('sha256').update(bytes).digest('hex');
    asset.result = 'hashed';
    assetBytes += bytes.length;
  } catch {
    asset.result = 'body_unavailable_or_timed_out';
    assetReadsStopped = true;
  } finally {
    clearTimeout(timer);
  }
}

async function acquireLoginForm(snapshotName) {
  stage = 'manual_login_view';
  const loginForm = page.locator('form:has(input[type="password"]:visible)');
  const manualLogin = page.getByText('Manual Login', { exact: true });
  await Promise.any([
    loginForm.waitFor({ state: 'visible', timeout: 10000 }),
    manualLogin.waitFor({ state: 'visible', timeout: 10000 }),
  ]);
  if (!await loginForm.isVisible()) {
    stage = 'manual_login_control';
    await manualLogin.locator('xpath=..').getByRole('button').click({ timeout: 10000 });
  }
  stage = 'manual_login_form';
  await loginForm.waitFor({ state: 'visible', timeout: 10000 });
  const username = loginForm.locator('input[type="text"]:visible');
  const password = loginForm.locator('input[type="password"]:visible');
  const submit = loginForm.getByRole('button', { name: 'Sign In', exact: true });
  await username.waitFor({ state: 'visible', timeout: 10000 });
  await password.waitFor({ state: 'visible', timeout: 10000 });
  await submit.waitFor({ state: 'visible', timeout: 10000 });
  if (await username.count() !== 1 || await password.count() !== 1 || await submit.count() !== 1) {
    throw new Error('The observed login controls are not unique.');
  }
  await snapshot(snapshotName);
  return { username, password, submit };
}

async function loginThroughUI(prefix = '') {
  report.login = { attempted: false, request_observed: false, http_status: null, form_closed: false, result: 'not_attempted' };
  (report.login_history ??= []).push(report.login);
  report.logout = { attempted: false, result: 'not_attempted', owned_session: 'not_created_by_this_observer' };
  const { username, password, submit } = await acquireLoginForm(`${prefix}before-login`);
  stage = 'login_form_fill';
  await username.fill(credentials.username);
  await password.fill(credentials.password);
  const loginResponse = page.waitForResponse(response => isLoginRequest(response.request()), { timeout: 15000 }).catch(() => null);
  stage = 'login_submit';
  report.login.attempted = true;
  report.login.result = 'submitted_outcome_unknown';
  report.logout.owned_session = 'possibly_retained_owned_session_no_ui_logout';
  await submit.click({ timeout: 10000 });
  const authentication = await loginResponse;
  if (!authentication) throw new Error('No login HTTP result was observed.');
  report.login.http_status = authentication.status();
  if (authentication.status() < 200 || authentication.status() >= 300) {
    report.login.result = 'http_rejected';
    report.logout.owned_session = 'no_successful_login_observed';
    stage = 'login_http_rejected';
    throw new Error('The original client login was rejected.');
  }
  report.login.result = 'http_accepted';
  report.logout.owned_session = 'retained_owned_session_no_ui_logout';
  stage = 'login_ui_transition';
  await password.waitFor({ state: 'hidden', timeout: 10000 });
  report.login.form_closed = true;
  report.login.result = 'http_accepted_and_login_form_closed';
  await snapshot(`${prefix}after-login`);
}

try {
  if (options.mode === 'ui-login') {
    stage = 'credentials_file';
    credentials = await loadCredentials();
  }
  stage = 'browser_start';
  const guardOrigin = await startNetworkGuard();
  browser = await chromium.launch({ headless: true, args: ['--no-sandbox', '--disable-dev-shm-usage', '--disable-background-networking'] });
  report.browser_version = browser.version();
  context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US',
    serviceWorkers: 'allow', proxy: { server: guardOrigin, bypass: `<-loopback>,${target.host}` } });
  sessionProof = createBrowserSessionProof({ context, target: target.origin });
  report.session_proof = sessionProof.report;
  await context.route('**/*', async route => {
    const request = route.request();
    if (allowedNetworkURL(request.url())) {
      await route.continue();
      return;
    }
    blockedRequests.add(request);
    const entry = entries.get(request);
    if (entry) entry.blocked_by_network_policy = true;
    recordBlockedNetwork(request.url(), request.method(), request.resourceType(), 'request-route');
    await route.abort('blockedbyclient');
  });
  page = await context.newPage();
  if (options['runtime-exceptions'] === 'true') {
    report.runtime_exception_diagnostics = { enabled: true, diagnostic_only: true, count: 0,
      limit: 256, errors: [], control_errors: 0,
      boundary: 'Immediately resume each exception; retain only sanitized error first lines. No source, stack, scope, object properties, or runtime values are inspected. Debugger timing is not playback acceptance.' };
    exceptionSession = await context.newCDPSession(page);
    exceptionSession.on('Debugger.paused', event => {
      exceptionChain = exceptionChain.then(async () => {
        const diagnostic = report.runtime_exception_diagnostics;
        diagnostic.count += 1;
        try {
          const line = typeof event.data?.description === 'string'
            ? event.data.description.split(/[\r\n]/, 1)[0].slice(0, 4096) : '';
          const error = /^((?:Type|Reference|Range|Syntax|URI|Eval)?Error): (.*)$/.exec(line);
          if (error && diagnostic.errors.length < diagnostic.limit) {
            diagnostic.errors.push(controlledPageError({ name: error[1], message: error[2] }));
          }
          if (diagnostic.count >= diagnostic.limit) {
            await exceptionSession.send('Debugger.setPauseOnExceptions', { state: 'none' });
          }
        } finally {
          await exceptionSession.send('Debugger.resume');
        }
      }).catch(() => { report.runtime_exception_diagnostics.control_errors += 1; });
    });
    await exceptionSession.send('Debugger.enable');
    await exceptionSession.send('Debugger.setPauseOnExceptions', { state: 'all' });
  }
  context.on('request', request => {
    if (options.mode === 'ui-login' && isLoginRequest(request)) report.login.request_observed = true;
    if (report.requests.length >= 3000) { report.request_overflow += 1; return; }
    const entry = { index: report.requests.length, elapsed_ms: Date.now() - started,
      method: request.method(), origin: originKind(request.url()), route: safeRoute(request.url()),
      query_field_names: queryFieldNames(request.url()),
      browse_query: [...new URL(request.url()).searchParams].filter(([name]) =>
        ['fields', 'includeitemtypes', 'recursive'].includes(name.toLowerCase())).map(([name, value]) =>
        ({ name, value: value.length <= 1024 && /^[A-Za-z0-9_,.-]*$/.test(value) ? value : '{unsupported-value}' })),
      blocked_by_network_policy: blockedRequests.has(request),
      resource_type: request.resourceType(), content_type: request.headers()['content-type'] ?? null, status: null };
    if (isLoginRequest(request)) {
      const headers = request.headers();
      entry.header_names = Object.keys(headers).slice(0, 64).sort();
      entry.authorization_shape = ['authorization', 'x-emby-authorization']
        .filter(name => typeof headers[name] === 'string')
        .map(name => ({ header: name, ...authorizationShape(headers[name]) }));
    }
    if (isLoginRequest(request) && entry.content_type?.startsWith('application/x-www-form-urlencoded')) {
      entry.form_field_names = [...new URLSearchParams(request.postData() ?? '').keys()].slice(0, 64)
        .map(name => /^[A-Za-z][A-Za-z0-9_.-]{0,95}$/.test(name) ? name : '{redacted field name}');
    }
    if (request.method() === 'POST' && originKind(request.url()) === 'target' && !isLoginRequest(request)) {
      const body = request.postDataBuffer();
      if (body && body.length <= 65536) {
        if (entry.content_type?.startsWith('application/json') || entry.content_type?.startsWith('text/plain')) {
          try {
            const decoded = JSON.parse(body.toString('utf8'));
            if (decoded && !Array.isArray(decoded) && typeof decoded === 'object') {
              entry.json_field_names = Object.keys(decoded).slice(0, 64)
                .map(name => /^[A-Za-z][A-Za-z0-9_.-]{0,95}$/.test(name) ? name : '{redacted field name}');
              if (/^(?:\/emby)?\/items\/[^/]+\/playbackinfo\/?$/i.test(new URL(request.url()).pathname) && decoded.DeviceProfile) {
                entry.playback_profile = safePlaybackProfile(decoded.DeviceProfile);
              }
            }
          } catch { entry.body_field_names_unavailable = 'invalid_json'; }
        } else if (entry.content_type?.startsWith('application/x-www-form-urlencoded')) {
          entry.form_field_names = [...new URLSearchParams(body.toString('utf8')).keys()].slice(0, 64)
            .map(name => /^[A-Za-z][A-Za-z0-9_.-]{0,95}$/.test(name) ? name : '{redacted field name}');
        }
      } else if (body) entry.body_field_names_unavailable = 'body_exceeds_inspection_limit';
    }
    if (originKind(request.url()) === 'target' && (request.resourceType() === 'media' || /^(?:\/emby)?\/(?:videos|audio)\//i.test(new URL(request.url()).pathname))) {
      const range = request.headers().range;
      if (range) entry.range = range.length <= 128 && /^bytes=\d*-\d*(?:,\s*\d*-\d*){0,7}$/.test(range) ? range : '{invalid range}';
    }
    entries.set(request, entry);
    report.requests.push(entry);
  });
  context.on('response', response => {
    if (options.mode === 'ui-login' && isLoginRequest(response.request())) report.login.http_status = response.status();
    const entry = entries.get(response.request());
    if (entry) entry.status = response.status();
    if (entry && response.status() >= 400 && isLoginRequest(response.request())) {
      authenticationDiagnostics = authenticationDiagnostics.then(() => recordAuthenticationFailure(response, entry))
        .catch(() => { entry.authentication_failure = { status: response.status(), result: 'error_body_unavailable' }; });
    }
    const url = new URL(response.url());
    if (entry && url.origin === target.origin && url.pathname.startsWith('/web/') &&
        response.status() === 200 && response.request().method() === 'GET') {
      assetChain = assetChain.then(() => hashAsset(response, entry.index)).catch(() => { assetReadsStopped = true; });
    }
  });
  context.on('requestfailed', request => {
    const entry = entries.get(request);
    if (entry) entry.failure = safeFailure(request.failure()?.errorText);
  });
  page.on('pageerror', error => {
    report.page_error_count += 1;
    if (report.page_errors.length < 64) report.page_errors.push(controlledPageError(error));
  });
  page.on('console', message => {
    if (message.type() !== 'error') return;
    report.console_error_count += 1;
    // Read only the displayed first line of JavaScript errors. Do not inspect
    // client source, console object properties, request payloads, or stacks.
    const line = message.text().split(/[\r\n]/, 1)[0].slice(0, 4096);
    const error = /^(?:Uncaught (?:\(in promise\) )?)?((?:Type|Reference|Range|Syntax|URI|Eval)?Error): (.*)$/.exec(line);
    if (error && report.console_errors.length < 64) {
      report.console_errors.push(controlledPageError({ name: error[1], message: error[2] }));
    }
  });
  page.on('websocket', websocket => {
    if (report.websocket_events.length >= 100) return;
    const entry = { route: safeRoute(websocket.url()), origin: originKind(websocket.url()),
      query_field_names: queryFieldNames(websocket.url()),
      opened_ms: Date.now() - started, sent_frames: 0, received_frames: 0, closed: false, error: false };
    report.websocket_events.push(entry);
    websocket.on('framesent', () => { entry.sent_frames += 1; });
    websocket.on('framereceived', () => { entry.received_frames += 1; });
    websocket.on('close', () => { entry.closed = true; });
    websocket.on('socketerror', () => { entry.error = true; });
  });
  try {
    const response = await page.goto(target.toString(), { waitUntil: 'domcontentloaded', timeout: 30000 });
    report.navigation = { status: response?.status() ?? null, result: 'domcontentloaded' };
  } catch {
    report.navigation = { status: null, result: 'navigation_failed_or_timed_out' };
  }
  await snapshot('initial');
  if (options.mode === 'manual-login-form') await acquireLoginForm('manual-login-form');
  if (options.mode === 'ui-login') {
    await loginThroughUI();
    if (options.workflow === 'movie') {
      stage = 'movie_workflow';
      const workflow = await import('./client-browser-playback.mjs');
      await workflow.runMovieWorkflow({ page, context, report, snapshot, target, output,
        repeatLogin: async () => { await loginThroughUI('movie-relogin-'); stage = 'movie_workflow'; } });
    } else if (options.workflow === 'preferences') {
      stage = 'preferences_workflow';
      const workflow = await import('./client-browser-preferences.mjs');
      await workflow.runPreferencesWorkflow({ page, context, report, snapshot, target, output, redactSecrets,
        preferenceMode: options['preference-mode'] });
    }
  }
  stage = 'observation';
  await page.waitForTimeout(duration);
  await snapshot('observed');
  await assetChain;
  await authenticationDiagnostics;
} catch {
  failure = `${stage}_failed`;
  if (page) await snapshot('failure').catch(() => {});
} finally {
  if (failure) assetReadsStopped = true;
  await assetChain;
  await authenticationDiagnostics;
  await exceptionChain;
  if (exceptionSession) await exceptionSession.detach().catch(() => {});
  if (sessionProof) {
    await sessionProof.drain();
    const expectedLogoutCount = report.logout_history?.length ??
      (report.logout.owned_session === 'ui_logout_observed' ? 1 : 0);
    report.session_proof.expected_ui_logout_count = expectedLogoutCount;
    if (report.logout.owned_session === 'ui_logout_observed' &&
        (sessionProof.report.outcome !== 'all_observed_logout_tokens_rejected' ||
         sessionProof.report.entries.length !== expectedLogoutCount) && !failure) {
      failure = 'post_logout_verification_failed';
    }
    await sessionProof.dispose();
  }
  // A diagnostic login also needs an exact private credential binding for
  // later cleanup; retaining a session without state loses that association.
  if (context && report.logout.owned_session.includes('retained')) {
    const filename = 'private-browser-storage-state.json';
    try {
      await context.storageState({ path: path.join(output, filename) });
      await fs.chmod(path.join(output, filename), 0o600);
      report.private_session_recovery = { filename, mode: '0600', publish: false,
        purpose: 'Owned session recovery or UI logout only; contains credentials and must remain private' };
    } catch { report.private_session_recovery = { result: 'storage_state_save_failed' }; }
  }
  if (browser) await browser.close().catch(() => {});
  if (networkGuard) {
    for (const socket of guardSockets) socket.destroy();
    await new Promise(resolve => networkGuard.close(resolve));
  }
  report.finished_at = new Date().toISOString();
  report.elapsed_ms = Date.now() - started;
  if (report.network_policy_guard_errors && !failure) failure = 'network_policy_guard_failed';
  report.failure = failure;
  report.asset_hashed_bytes = assetBytes;
  await fs.writeFile(path.join(output, 'observation.json'), JSON.stringify(redactSecrets(report), null, 2) + '\n', { mode: 0o600, flag: 'wx' });
  process.stdout.write(JSON.stringify({ result: failure ?? report.navigation?.result ?? 'unavailable',
    browser_version: report.browser_version ?? null, requests: report.requests.length,
    api_failures: report.requests.filter(entry => entry.route !== '/web/{asset}' && (entry.failure || entry.status >= 400)).length,
    page_error_count: report.page_error_count, login_http_status: report.login.http_status,
    blocked_external_count: report.blocked_external_count,
    owned_session: report.logout.owned_session,
    output }) + '\n');
}
if (failure || report.navigation?.result !== 'domcontentloaded') process.exitCode = 1;
