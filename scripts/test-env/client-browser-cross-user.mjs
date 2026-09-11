#!/usr/bin/env node
/** Observe two existing ordinary users without playing media or changing user state. */
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import http from 'node:http';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import { TextDecoder } from 'node:util';
import { loadGobyAVFixture } from './client-browser-goby-fixture.mjs';
import { createBrowserSessionProof } from './client-browser-session-proof.mjs';

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
const PREPARATION_ITEM = '268051d3ca734aefcf94e245fb25ad55';
const PREPARATION_SCOPE = 'source28-page-error-01';
const LIMIT = 2 * 1024 * 1024;
const INPUT_NAMES = ['client-browser-cross-user.mjs', 'client-browser-goby-fixture.mjs', 'client-browser-session-proof.mjs'];
const ID = /^[0-9a-f]{32}$/;
const SHA = /^[0-9a-f]{64}$/;
const record = value => value !== null && typeof value === 'object' && !Array.isArray(value);
const hash = value => createHash('sha256').update(value).digest('hex');
const stable = value => Array.isArray(value) ? value.map(stable) : record(value)
  ? Object.fromEntries(Object.keys(value).sort().map(key => [key, stable(value[key])])) : value;
const same = (left, right) => JSON.stringify(stable(left)) === JSON.stringify(stable(right));
const digest = value => hash(JSON.stringify(stable(value)));
const acceptanceMode = value => ['acceptance', 'acceptance-preparation'].includes(value);
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

export function parseCrossUserArguments(argv) {
  const names = ['candidate-sha256', 'source', 'source-manifest-sha256', 'music-scan-receipt-sha256',
    'music-upgrade-chain', 'music-upgrade-chain-sha256', 'output'];
  requireThat(Array.isArray(argv) && [names.length * 2, (names.length + 1) * 2, (names.length + 2) * 2].includes(argv.length));
  const result = {};
  for (let index = 0; index < argv.length; index += 2) {
    const name = argv[index]?.slice(2), value = argv[index + 1];
    requireThat(argv[index]?.startsWith('--') && [...names, 'mode', 'preparation-scope'].includes(name) && !Object.hasOwn(result, name) && typeof value === 'string');
    result[name] = value;
  }
  requireThat(names.every(name => Object.hasOwn(result, name)));
  result.mode ??= 'acceptance';
  requireProxyExecutionMode(result.mode, result['preparation-scope']);
  for (const name of names.filter(name => name.endsWith('sha256'))) requireThat(SHA.test(result[name]));
  requireThat(new RegExp(`^${ROOT}/source-attempt-[1-9][0-9]*$`).test(result.source) &&
    new RegExp(`^${ROOT}/client-music-upgrade-chain-[A-Za-z0-9_-]{1,64}\\.json$`).test(result['music-upgrade-chain']) &&
    new RegExp(`^${ROOT}/client-cross-user-[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`).test(result.output));
  return result;
}

/** Preparation requires this run's explicit authority before any external effects. */
export function requireProxyExecutionMode(mode, preparationScope = undefined) {
  requireThat(['prelogin', 'acceptance', 'acceptance-preparation'].includes(mode));
  requireThat(mode === 'acceptance-preparation' ? preparationScope === PREPARATION_SCOPE : preparationScope === undefined);
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

export function classifyBrowserRequest(raw, method, resourceType = '', mode = 'acceptance') {
  try {
    if (!['acceptance', 'acceptance-preparation', 'prelogin'].includes(mode)) return { allow: false, kind: 'invalid_mode' };
    if (mode === 'prelogin' && !['GET', 'HEAD', 'OPTIONS'].includes(method)) return { allow: false, kind: 'state_mutation' };
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
    if (/\/PlaybackInfo(?:\/|$)/i.test(route)) return mode === 'acceptance-preparation' && method === 'POST' &&
      new RegExp(`^/Items/${PREPARATION_ITEM}/PlaybackInfo/?$`, 'i').test(route)
      ? { allow: true, kind: 'preparation' } : { allow: false, kind: 'playback' };
    if (['GET', 'HEAD', 'OPTIONS'].includes(method)) return { allow: true, kind: url.protocol === 'ws:' ? 'websocket' : 'read' };
    if (method === 'POST' && /^\/Users\/AuthenticateByName\/?$/i.test(route)) return { allow: true, kind: 'login' };
    if (method === 'POST' && /^\/Sessions\/Capabilities(?:\/Full)?\/?$/i.test(route)) return { allow: true, kind: 'capabilities' };
    if (method === 'POST' && /^\/Sessions\/Logout\/?$/i.test(route)) return { allow: true, kind: 'logout' };
    return { allow: false, kind: 'state_mutation' };
  } catch { return { allow: false, kind: 'invalid_url' }; }
}

export function browserRequestDiagnostic(raw, resourceType = '', method = '') {
  const resource = ['document', 'stylesheet', 'image', 'media', 'font', 'script', 'texttrack', 'xhr', 'fetch',
    'eventsource', 'websocket', 'manifest', 'other'].includes(resourceType) ? resourceType : 'other';
  const result = { resource_type: resource, category: 'invalid_url', origin: 'unknown', path_sha256: null,
    query_field_count: 0, query_fields_truncated: false };
  try {
    const url = new URL(raw);
    if (['data:', 'blob:'].includes(url.protocol)) return { ...result, category: 'non_network', origin: 'non_network' };
    result.origin = url.origin === ORIGIN || url.protocol === 'ws:' && `http://${url.host}` === ORIGIN ? 'target' : 'external';
    result.path_sha256 = hash(url.pathname);
    for (const _entry of url.searchParams) {
      if (++result.query_field_count > 128) { result.query_fields_truncated = true; break; }
    }
    const route = url.pathname.replace(/^\/emby(?=\/)/i, '').toLowerCase();
    if (result.origin === 'external' && method === 'POST') result.category = 'external_post';
    else if (/^\/items\/[^/]+\/playbackinfo\/?$/.test(route)) result.category = 'playback_preparation';
    else if (route === '/web/index.html' || route === '/web/' || route === '/') result.category = 'web_document';
    else if (/\/service[-_]?worker(?:\.[a-z0-9_-]+)?\.js$/.test(route)) result.category = 'service_worker_script';
    else if (route.startsWith('/web/')) result.category = /\.js$/.test(route) ? 'web_script' : /\.css$/.test(route) ? 'web_stylesheet'
      : /\.(?:woff2?|ttf|otf)$/.test(route) ? 'web_font' : /\.(?:png|jpe?g|svg|webp|ico)$/.test(route) ? 'web_image'
        : /\.json$/.test(route) ? 'web_json' : 'web_other';
    else result.category = ({ '/system/info/public': 'system_info_public', '/system/ping': 'system_ping',
      '/users/public': 'public_users', '/branding/css': 'branding_css', '/branding/configuration': 'branding_configuration',
      '/users/authenticatebyname': 'login', '/sessions/logout': 'logout' })[route] ?? 'other_endpoint';
  } catch { /* Only fixed categories and a pathname digest leave this function. */ }
  return result;
}

export function safeBrowserFailure(error, phase) {
  const describe = value => {
    const name = ['Error', 'TimeoutError', 'TypeError', 'ReferenceError', 'SyntaxError', 'RangeError', 'AggregateError']
      .includes(value?.name) ? value.name : 'other';
    const message = typeof value?.message === 'string' ? value.message.slice(0, 16384) : '';
    const category = name === 'TimeoutError' || /timed?\s*out|timeout/i.test(message) ? 'timeout'
      : /service[\s_-]*worker/i.test(message) ? 'service_worker'
        : /content security policy|\bcsp\b|refused to (?:load|execute).*script/i.test(message) ? 'content_security_policy'
          : /net::err_|failed to fetch|network/i.test(message) ? 'network'
            : /(?:target|page|browser|context).*closed/i.test(message) ? 'closed_target'
              : /cross_user_guard_rejected|cross_user_target_changed/i.test(message) ? 'guard' : 'other';
    return { name, category };
  };
  const causes = Array.isArray(error?.errors) ? error.errors.slice(0, 2).map(describe) : [];
  const phases = ['not_started', 'browser_start', 'browser_context', 'new_page', 'navigation', 'manual_login',
    'login_entry_wait', 'login_entry_ready', 'manual_login_click', 'login_form_wait', 'login_controls',
    'credential_fill', 'login_pin', 'login_submit', 'login_response', 'login_form_closed', 'token_capture',
    'authenticated', 'prelogin_complete', 'ui_home', 'ui_movie', 'ui_return_home', 'ui_home_control_wait',
    'ui_home_click', 'ui_home_confirmation', 'ui_logout'];
  return { phase: phases.includes(phase) ? phase : 'unknown',
    ...describe(error), causes, causes_truncated: Array.isArray(error?.errors) && error.errors.length > 2 };
}

const DIAGNOSTIC_INPUT_LIMIT = 8192;
const DIAGNOSTIC_OUTPUT_LIMIT = 512;
const DIAGNOSTIC_ENTRY_LIMIT = 16;

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

/** Only bounded diagnostic text is retained; an oversized input is never partially copied. */
export function sanitizeBrowserMessage(value, secrets = []) {
  try {
    if (typeof value !== 'string') return '[diagnostic unavailable]';
    if (value.length > DIAGNOSTIC_INPUT_LIMIT) return '[diagnostic omitted: input limit]';
    const variants = diagnosticSecretVariants(secrets);
    let result = redactDiagnosticSecrets(value, variants);
    result = redactDiagnosticSecrets(normalizeDiagnosticEncoding(result), variants);
    result = result
      .replace(/\b(?:https?|wss?|file|blob|data):\S*/gi, '[url]')
      .replace(/\b[a-z][a-z0-9+.-]{1,15}:\/+\S*/gi, '[url]')
      .replace(/(?:\/\/|\\\\)[^\s"'<>()[\]{}]+/g, '[path]')
      .replace(/(^|[\s"'<>()[\]{}])[^\s"'<>()[\]{}]*[\/\\][^\s"'<>()[\]{}]+/g, '$1[path]')
      .replace(/\b(?:localhost|(?:[0-9]{1,3}\.){3}[0-9]{1,3}|(?:[a-z0-9-]+\.)+[a-z]{2,63})(?::[0-9]+)?(?:[?#][^\s"'<>()[\]{}]*)?/gi, '[address]')
      .replace(/[?#]\S*/g, '[query]')
      .replace(/\b[A-Za-z_$][A-Za-z0-9_.$-]{0,63}\s*=\s*(?:"[^"]*"|'[^']*'|[^\s,;)\]}]+)/g, '[parameter]')
      .replace(/\b(?:api[_-]?key|access[_-]?token|x[_-]emby[_-]token|authorization|password|token)\s*:\s*(?:"[^"]*"|'[^']*'|[^\s,;)\]}]+)/gi, '[credential]')
      .replace(/(?:%[0-9a-f]{2})+|\\(?:u\{[0-9a-f]+\}|u[0-9a-f]{4}|x[0-9a-f]{2})/gi, '[encoded]')
      .replace(/[A-Za-z0-9][A-Za-z0-9_+/.=-]{23,}/g, '[opaque]')
      .replace(/[^\x20-\x7e]/g, ' ').replace(/\s+/g, ' ').trim();
    result = redactDiagnosticSecrets(result, variants);
    if (result.length <= DIAGNOSTIC_OUTPUT_LIMIT) return result;
    const boundary = result.lastIndexOf(' ', DIAGNOSTIC_OUTPUT_LIMIT - 12);
    return boundary < 0 ? '[diagnostic omitted: output limit]' : result.slice(0, boundary) + ' [truncated]';
  } catch { return '[diagnostic unavailable]'; }
}

function sanitizeDiagnosticName(value, secrets) {
  const name = sanitizeBrowserMessage(value, secrets);
  return name.length <= 80 ? name : '[diagnostic omitted: name limit]';
}

/** Read only the ordinary error name and message; never inspect stacks, causes or browser objects. */
export function browserEventDiagnostic(error, phase, secrets = []) {
  let name = '', message = '', unavailable = false;
  try { const value = error?.name; if (typeof value === 'string') name = value; else unavailable = true; }
  catch { unavailable = true; }
  try { const value = error?.message; if (typeof value === 'string') message = value; else unavailable = true; }
  catch { unavailable = true; }
  return { ...safeBrowserFailure({ name, message }, phase),
    diagnostic_name: sanitizeDiagnosticName(name, secrets),
    message: sanitizeBrowserMessage(message, secrets), diagnostic_unavailable: unavailable };
}

export function recordBrowserPageError(report, error, phase, elapsedMs, secrets = []) {
  // The count must survive missing messages, throwing getters and the diagnostic entry limit.
  report.page_error_count += 1;
  if (report.page_errors.length < DIAGNOSTIC_ENTRY_LIMIT) report.page_errors.push({
    ...browserEventDiagnostic(error, phase, secrets), elapsed_ms: elapsedMs });
}

/** Reapply the final secret union after logout; late token capture cannot expose an earlier message. */
export function resanitizeBrowserDiagnostics(report, secrets) {
  for (const actor of report.accounts ?? []) {
    for (const entries of [actor.page_errors ?? [], actor.console_diagnostics ?? []]) for (const entry of entries) {
      if (Object.hasOwn(entry, 'message')) entry.message = sanitizeBrowserMessage(entry.message, secrets);
      if (Object.hasOwn(entry, 'diagnostic_name')) entry.diagnostic_name = sanitizeDiagnosticName(entry.diagnostic_name, secrets);
    }
  }
}

export function sanitizeBootstrapDOM(raw) {
  const source = record(raw) ? raw : {};
  const count = value => Number.isSafeInteger(value) && value >= 0 && value <= 1000000 ? value : null;
  const flag = value => typeof value === 'boolean' ? value : null;
  const counts = value => Object.fromEntries(['forms', 'password_forms', 'text_inputs', 'password_inputs', 'buttons', 'links',
    'sign_in_buttons', 'manual_login_controls'].map(key => [key, count(value?.[key])]));
  return {
    ready_state: ['loading', 'interactive', 'complete'].includes(source.ready_state) ? source.ready_state : 'unknown',
    title_kind: ['emby', 'goby', 'empty', 'other'].includes(source.title_kind) ? source.title_kind : 'other',
    body_present: flag(source.body_present), body_character_count: count(source.body_character_count),
    visible: counts(source.visible),
    milestones: Object.fromEntries(['manual_login', 'sign_in', 'select_server', 'connect_to_server', 'loading', 'connection_error']
      .map(key => [key, flag(source.milestones?.[key])])),
    scripts: Object.fromEntries(['total', 'external', 'module'].map(key => [key, count(source.scripts?.[key])])),
    service_worker: { available: flag(source.service_worker?.available), controller_present: flag(source.service_worker?.controller_present),
      registration_count: count(source.service_worker?.registration_count), query_failed: flag(source.service_worker?.query_failed) },
  };
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
export function proxyRequestPlan(raw, method, rawHeaders, mode) {
  requireThat(typeof raw === 'string' && Buffer.byteLength(raw) <= 16384 && raw.startsWith(ORIGIN + '/') &&
    !raw.includes('\\') && ['acceptance', 'acceptance-preparation', 'prelogin'].includes(mode));
  const url = new URL(raw), policy = classifyBrowserRequest(raw, method, '', mode);
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
export function proxyResponsePlan(status, rawHeaders, method) {
  requireThat(Number.isInteger(status) && status >= 200 && status <= 599);
  const headers = proxyHeaders(rawHeaders, true), noBody = method === 'HEAD' || status === 204 || status === 304;
  requireThat(noBody || headers.contentLength === null || headers.contentLength <= PROXY_LIMITS.responseBytes);
  return { status, contentLength: headers.contentLength, noBody, headers: [...headers.forwarded, 'Connection', 'close'] };
}

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
  const classification = classifyBrowserRequest(request.url ?? '', request.method ?? '', '', state.mode);
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
      plan = proxyRequestPlan(request.url, request.method, request.rawHeaders, state.mode);
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

const WS_LIMITS = Object.freeze({ handshakes: 2, active: 1, handshakeMs: 10000, lifetimeMs: 240000, idleMs: 65000,
  frameBytes: 65536, messageBytes: 65536, wireBytes: 8 * 1024 * 1024, queuedBytes: 1024 * 1024,
  framesPerSecond: 64, framesTotal: 512 });
const WS_GUID = '258EAFA5-E914-47DA-95CA-C5AB0DC85B11';
const WS_PATHS = new Set(['/', '/emby', '/emby/', '/embywebsocket', '/emby/socket']);
const WS_RESERVATIONS = new WeakSet();

/** Filter genuine frame request events without fabricating a context, page or request. */
export function frameRequestContext(context, onError) {
  requireThat(context && typeof context.pages === 'function' && typeof context.on === 'function' && typeof context.off === 'function' &&
    typeof onError === 'function');
  const bindings = new Map();
  return Object.freeze({
    pages: () => context.pages(),
    on(event, listener) {
      requireThat(['request', 'response', 'requestfailed', 'requestfinished'].includes(event) && typeof listener === 'function');
      const entries = bindings.get(event) ?? new Map(); requireThat(!entries.has(listener));
      const filter = value => {
        try {
          const request = event === 'response' ? value.request() : value;
          requireThat(typeof request.serviceWorker === 'function');
          const worker = request.serviceWorker();
          requireThat(worker === null || worker && typeof worker === 'object');
          if (worker === null) listener(value);
        } catch { onError(); }
      };
      entries.set(listener, filter); bindings.set(event, entries); context.on(event, filter);
    },
    off(event, listener) {
      const entries = bindings.get(event), filter = entries?.get(listener);
      if (filter) { context.off(event, filter); entries.delete(listener); }
    },
  });
}

/** Bind one genuine RFC 6455 request to the selected actor and target. */
export function websocketRequestPlan(raw, method, rawHeaders, userId, mode) {
  requireThat(acceptanceMode(mode) && method === 'GET' && ID.test(userId) && typeof raw === 'string' &&
    Buffer.byteLength(raw) <= 16384 && !raw.includes('\\'));
  const url = new URL(raw), origin = url.protocol === 'ws:' ? `http://${url.host}` : url.origin;
  requireThat(['http:', 'ws:'].includes(url.protocol) && origin === ORIGIN && url.href === raw &&
    !url.username && !url.password && !url.hash && WS_PATHS.has(url.pathname.toLowerCase()));
  const users = [...url.searchParams].filter(([name]) => name.toLowerCase() === 'userid').map(([, value]) => value);
  requireThat(users.length === 0 || users.length === 1 && users[0] === userId);
  const headers = proxyHeaders(rawHeaders, false, true), first = name => headers.values.get(name)?.[0];
  requireThat(first('host') === '127.0.0.1:18196' && first('upgrade')?.toLowerCase() === 'websocket' &&
    first('connection')?.split(',').some(value => value.trim().toLowerCase() === 'upgrade') &&
    !first('connection')?.split(',').some(value => value.trim().toLowerCase() === 'close') && first('origin') === ORIGIN &&
    first('sec-websocket-version') === '13' && !headers.values.has('expect') && !headers.values.has('transfer-encoding') &&
    (headers.contentLength === null || headers.contentLength === 0));
  const key = first('sec-websocket-key');
  requireThat(typeof key === 'string' && /^[A-Za-z0-9+/]{22}==$/.test(key) && Buffer.from(key, 'base64').length === 16 &&
    Buffer.from(key, 'base64').toString('base64') === key);
  const actual = Object.fromEntries([...headers.values].map(([name, values]) => [name, values[0]]));
  const authority = authorityForURL(url, actual); requireThat(authority);
  const protocols = (first('sec-websocket-protocol') ?? '').split(',').map(value => value.trim()).filter(Boolean);
  requireThat(protocols.length <= 16 && new Set(protocols).size === protocols.length &&
    protocols.every(value => /^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,128}$/.test(value)));
  return { path: url.pathname + url.search, headers: headers.forwarded, key, authority,
    protocols };
}

export function websocketResponsePlan(status, rawHeaders, requestPlan) {
  requireThat(status === 101);
  const headers = proxyHeaders(rawHeaders, true, true), first = name => headers.values.get(name)?.[0];
  for (const name of ['upgrade', 'sec-websocket-accept', 'sec-websocket-protocol']) requireThat((headers.values.get(name)?.length ?? 0) <= 1);
  requireThat(first('upgrade')?.toLowerCase() === 'websocket' &&
    first('connection')?.split(',').some(value => value.trim().toLowerCase() === 'upgrade') &&
    !first('connection')?.split(',').some(value => value.trim().toLowerCase() === 'close') &&
    first('sec-websocket-accept') === createHash('sha1').update(requestPlan.key + WS_GUID).digest('base64') &&
    !headers.values.has('sec-websocket-extensions') && !headers.values.has('transfer-encoding') && headers.contentLength === null &&
    (!first('sec-websocket-protocol') || requestPlan.protocols.includes(first('sec-websocket-protocol'))));
  return headers.forwarded;
}

export function websocketFrameState(direction) {
  requireThat(['client', 'server'].includes(direction));
  return { direction, pending: Buffer.alloc(0), messageParts: [], messageBytes: 0, fragmentOpcode: null,
    wireBytes: 0, frames: 0, messages: 0, windowAt: null, windowFrames: 0, sawClose: false,
    messageKinds: { other: 0, user_data_changed: 0 }, controlAttempt: false };
}

function clearWebSocketFrameState(state) {
  state.pending.fill(0); for (const part of state.messageParts) part.fill(0);
  state.pending = Buffer.alloc(0); state.messageParts = []; state.messageBytes = 0;
}

function websocketMessageKind(bytes) {
  const text = new TextDecoder('utf-8', { fatal: true }).decode(bytes), envelope = JSON.parse(text);
  requireThat(record(envelope) && typeof envelope.MessageType === 'string' && envelope.MessageType.trim().length > 0 &&
    Buffer.byteLength(envelope.MessageType) <= 128 && !/\p{Cc}/u.test(envelope.MessageType));
  let depth = 0, count = 0;
  for (let index = 0; index < text.length; index += 1) {
    if (text[index] === '{' || text[index] === '[') depth += 1;
    else if (text[index] === '}' || text[index] === ']') depth -= 1;
    else if (text[index] === '"') {
      const start = index++;
      while (index < text.length && text[index] !== '"') { if (text[index] === '\\') index += 1; index += 1; }
      let after = index + 1; while (/\s/.test(text[after] ?? '') && after < text.length) after += 1;
      if (depth === 1 && text[after] === ':' && JSON.parse(text.slice(start, index + 1)).toLowerCase() === 'messagetype') count += 1;
    }
  }
  requireThat(count === 1);
  return envelope.MessageType.toLowerCase();
}

/** Return only unchanged wire frames; message inspection emits no application data. */
export function inspectWebSocketFrames(state, chunk, now) {
  requireThat(Buffer.isBuffer(chunk) && Number.isFinite(now) && chunk.length + state.wireBytes <= WS_LIMITS.wireBytes);
  state.wireBytes += chunk.length; state.pending = Buffer.concat([state.pending, chunk]);
  const frames = [];
  while (state.pending.length >= 2) {
    const first = state.pending[0], second = state.pending[1], final = Boolean(first & 0x80), opcode = first & 15;
    const masked = Boolean(second & 0x80); let length = second & 127, offset = 2;
    requireThat((first & 0x70) === 0 && [0, 1, 2, 8, 9, 10].includes(opcode) && masked === (state.direction === 'client') && !state.sawClose);
    if (length === 126) {
      if (state.pending.length < 4) break;
      length = state.pending.readUInt16BE(2); offset = 4; requireThat(length >= 126);
    } else if (length === 127) {
      if (state.pending.length < 10) break;
      const extended = state.pending.readBigUInt64BE(2); requireThat(extended >= 65536n && extended <= BigInt(WS_LIMITS.frameBytes));
      length = Number(extended); offset = 10;
    }
    requireThat(length <= WS_LIMITS.frameBytes && (opcode < 8 || final && length <= 125 && (opcode !== 8 || length !== 1)));
    const maskOffset = offset; if (masked) offset += 4;
    if (state.pending.length < offset + length) break;
    if (state.windowAt === null || now - state.windowAt >= 1000) { state.windowAt = now; state.windowFrames = 0; }
    requireThat(++state.windowFrames <= WS_LIMITS.framesPerSecond && ++state.frames <= WS_LIMITS.framesTotal);
    const wire = Buffer.from(state.pending.subarray(0, offset + length)), payload = Buffer.from(state.pending.subarray(offset, offset + length));
    state.pending = state.pending.subarray(offset + length);
    if (masked) for (let index = 0; index < payload.length; index += 1) payload[index] ^= wire[maskOffset + index % 4];
    if (opcode < 8) {
      requireThat(opcode === 0 ? state.fragmentOpcode !== null : state.fragmentOpcode === null);
      if (opcode !== 0 && !final) state.fragmentOpcode = opcode;
      state.messageBytes += payload.length; requireThat(state.messageBytes <= WS_LIMITS.messageBytes);
      state.messageParts.push(payload);
      if (final) {
        const message = Buffer.concat(state.messageParts);
        try {
          const kind = websocketMessageKind(message);
          if (state.direction === 'server' && ['play', 'playstate', 'generalcommand'].includes(kind)) {
            state.controlAttempt = true; throw new Error('cross_user_server_control_message');
          }
          state.messageKinds[kind === 'userdatachanged' ? 'user_data_changed' : 'other'] += 1;
          state.messages += 1;
        } finally {
          message.fill(0); for (const part of state.messageParts) part.fill(0);
          state.messageParts = []; state.messageBytes = 0; state.fragmentOpcode = null;
        }
      }
    } else {
      if (opcode === 8 && payload.length >= 2) {
        const code = payload.readUInt16BE(0);
        requireThat(code >= 1000 && code <= 4999 && ![1004, 1005, 1006, 1015].includes(code) && (code <= 1014 || code >= 3000));
        new TextDecoder('utf-8', { fatal: true }).decode(payload.subarray(2));
      }
      payload.fill(0); if (opcode === 8) state.sawClose = true;
    }
    frames.push(wire);
  }
  return frames;
}

function websocketHTTPHead(status, message, headers) {
  requireThat(Number.isInteger(status) && status >= 100 && status <= 599 && typeof message === 'string' &&
    message.length <= 128 && !/[\x00-\x1f\x7f-\uffff]/.test(message));
  let head = `HTTP/1.1 ${status} ${message}\r\n`;
  for (let index = 0; index < headers.length; index += 2) head += `${headers[index]}: ${headers[index + 1]}\r\n`;
  return Buffer.from(head + '\r\n', 'latin1');
}

export function websocketConnectPlan(raw, method, rawHeaders, mode) {
  requireThat(acceptanceMode(mode) && method === 'CONNECT' && raw === '127.0.0.1:18196');
  const headers = proxyHeaders(rawHeaders), first = name => headers.values.get(name)?.[0];
  requireThat(first('host') === '127.0.0.1:18196' && !headers.values.has('expect') && !headers.values.has('transfer-encoding') &&
    (headers.contentLength === null || headers.contentLength === 0));
}

/** Decode one bounded plaintext handshake inside CONNECT, never a general HTTP tunnel. */
export function websocketConnectInner(bytes, userId) {
  requireThat(Buffer.isBuffer(bytes) && bytes.length <= PROXY_LIMITS.headerBytes + WS_LIMITS.queuedBytes);
  if (bytes.length >= 4) requireThat(bytes.subarray(0, 4).equals(Buffer.from('GET ')));
  const boundary = bytes.indexOf('\r\n\r\n');
  if (boundary < 0) { requireThat(bytes.length <= PROXY_LIMITS.headerBytes); return null; }
  requireThat(boundary + 4 <= PROXY_LIMITS.headerBytes);
  const lines = bytes.subarray(0, boundary).toString('latin1').split('\r\n');
  const first = /^GET (\/[^\s]*) HTTP\/1\.1$/.exec(lines.shift()); requireThat(first);
  const rawHeaders = [];
  for (const line of lines) {
    const colon = line.indexOf(':'); requireThat(colon > 0 && !/^[ \t]/.test(line) && !/[\r\n]/.test(line));
    rawHeaders.push(line.slice(0, colon), line.slice(colon + 1).replace(/^ +| +$/g, ''));
  }
  const request = { url: ORIGIN + first[1], method: 'GET', rawHeaders };
  const plan = websocketRequestPlan(request.url, request.method, rawHeaders, userId, 'acceptance');
  plan.authority.token = null;
  return { request, head: Buffer.from(bytes.subarray(boundary + 4)) };
}

/** Accept CONNECT only as a bounded parser for the same authorized WebSocket path. */
export function forwardWebSocketConnect(request, client, head, policy, transport = http) {
  const state = policy.state, entry = policy.entry;
  return new Promise(resolve => {
    let settled = false, transferred = false, connected = false, admitted = false, reservation, permission, headerTimer, pending = Buffer.alloc(0);
    const totalTimer = setTimeout(() => finish('connect_timeout'), 10000);
    const detach = () => {
      client.off('data', collect); client.off('error', error); client.off('end', ended); client.off('close', ended);
      clearTimeout(totalTimer); clearTimeout(headerTimer);
    };
    const finish = reason => {
      if (settled || transferred) return;
      settled = true; detach(); pending.fill(0);
      if (admitted) { state.active -= 1; WS_RESERVATIONS.delete(reservation); }
      state.failed += 1;
      Object.assign(entry, { outcome: 'failed', reason, closed: true, handshake_delivered: false, upstream_status: null });
      client.destroy();
      try { policy.onEvent(entry); } catch { state.failed += 1; }
      Promise.resolve(permission).catch(() => {}).finally(resolve);
    };
    const error = () => finish('connect_client_error'), ended = () => finish('connect_client_closed');
    const parse = () => {
      if (settled || transferred || !connected) return;
      try {
        requireThat(!policy.ownershipLost() && !policy.logoutInProgress());
        const parsed = websocketConnectInner(pending, policy.userId);
        if (!parsed) return;
        client.pause(); transferred = true; detach(); pending.fill(0); pending = Buffer.alloc(0);
        const running = forwardBrowserWebSocket(parsed.request, client, parsed.head, { ...policy, reservation }, transport);
        running.then(resolve, () => { state.failed += 1; client.destroy(); resolve(); });
      } catch { finish('connect_inner_rejected'); }
    };
    const collect = bytes => {
      if (settled || transferred) return;
      if (pending.length + bytes.length > PROXY_LIMITS.headerBytes + WS_LIMITS.queuedBytes) { finish('connect_bytes_exceeded'); return; }
      pending = Buffer.concat([pending, bytes]); parse();
    };
    client.on('error', error); client.on('end', ended); client.on('close', ended); client.on('data', collect);
    try {
      client.pause(); state.seen += 1;
      websocketConnectPlan(request.url, request.method, request.rawHeaders, policy.mode);
      requireThat(!policy.ownershipLost() && !policy.logoutInProgress() && state.seen <= WS_LIMITS.handshakes &&
        state.active < WS_LIMITS.active && Buffer.isBuffer(head) && head.length <= PROXY_LIMITS.headerBytes + WS_LIMITS.queuedBytes);
      state.active += 1; state.admitted += 1; admitted = true;
      reservation = { state, entry, index: state.seen - 1 }; WS_RESERVATIONS.add(reservation);
      Object.assign(entry, { index: reservation.index, connect_transport: true, proxy_connect_status: null,
        outcome: 'pending', upstream_status: null, handshake_delivered: false, closed: false });
      pending = Buffer.from(head);
    } catch { finish('connect_admission_rejected'); return; }
    permission = Promise.resolve().then(() => policy.connectAllowed());
    void (async () => {
      try {
        await permission;
        if (settled) return;
        requireThat(!policy.ownershipLost() && !policy.logoutInProgress());
        client.write('HTTP/1.1 200 Connection Established\r\n\r\n', error => {
          if (settled) return;
          if (error) { finish('connect_write_failed'); return; }
          connected = true; entry.proxy_connect_status = 200;
          headerTimer = setTimeout(() => finish('connect_header_timeout'), 5000);
          parse(); if (!settled && !transferred) client.resume();
        });
      } catch { finish('connect_permission_rejected'); }
    })();
  });
}

/** Relay a genuine, authorized upgrade and unchanged bounded wire frames. */
export function forwardBrowserWebSocket(request, client, head, policy, transport = http) {
  const state = policy.state, entry = policy.entry;
  return new Promise(resolve => {
    let plan, admitted = false, settled = false, opened = false, closing = false, headsQueued = false, outgoing, peer, authorityTask;
    let lifeTimer, idleTimer, flushTimer, queued = 0;
    const buffers = new Set(), blockedSources = new Set(), clientFrames = websocketFrameState('client'), serverFrames = websocketFrameState('server');
    const handshakeTimer = setTimeout(() => finish('failed', 'handshake_timeout'), WS_LIMITS.handshakeMs);
    const updateCounters = () => Object.assign(entry, {
      client_wire_bytes: clientFrames.wireBytes, server_wire_bytes: serverFrames.wireBytes,
      client_frames: clientFrames.frames, server_frames: serverFrames.frames,
      client_messages: clientFrames.messages, server_messages: serverFrames.messages,
      server_message_kinds: { ...serverFrames.messageKinds }, server_control_attempted: serverFrames.controlAttempt,
    });
    const finish = (outcome, reason) => {
      if (settled) return;
      settled = true; clearTimeout(handshakeTimer); clearTimeout(lifeTimer); clearTimeout(idleTimer); clearTimeout(flushTimer);
      if (admitted) state.active -= 1;
      if (outcome === 'closed') state.closed += 1; else state.failed += 1;
      updateCounters();
      if (serverFrames.controlAttempt) state.control_attempts += 1;
      Object.assign(entry, { outcome, reason, closed: true });
      if (outgoing && !outgoing.destroyed) outgoing.destroy();
      peer?.destroy(); client.destroy();
      clearWebSocketFrameState(clientFrames); clearWebSocketFrameState(serverFrames);
      for (const value of buffers) value.fill(0); buffers.clear();
      if (plan) { plan.authority.token = null; plan.authority = null; plan.headers = null; }
      try { policy.onEvent(entry); } catch { state.failed += 1; }
      Promise.resolve(authorityTask).catch(() => {}).finally(resolve);
    };
    const expectedClose = () => (opened || entry.upstream_status === 101) &&
      (clientFrames.sawClose || serverFrames.sawClose || policy.logoutInProgress());
    const closeNormally = (destination, reason) => {
      if (settled || closing) return;
      if (!expectedClose()) { finish('failed', 'unexpected_close'); return; }
      closing = true;
      flushTimer = setTimeout(() => finish('failed', 'close_flush_timeout'), 5000);
      if (!destination || destination.destroyed) { finish('closed', reason); return; }
      destination.end(error => finish(error ? 'failed' : 'closed', error ? 'close_flush_failed' : reason));
    };
    const activity = () => {
      clearTimeout(idleTimer);
      if (opened) idleTimer = setTimeout(() => finish('failed', 'idle_timeout'), WS_LIMITS.idleMs);
    };
    const resumeSources = () => {
      if (!opened || !headsQueued || settled || closing) return;
      if (!blockedSources.has(client)) client.resume();
      if (peer && !blockedSources.has(peer)) peer.resume();
    };
    const write = (destination, bytes, source = null, completed = null) => {
      if (settled) { bytes.fill(0); return; }
      if (queued + bytes.length > WS_LIMITS.queuedBytes) { bytes.fill(0); finish('failed', 'write_queue_exceeded'); return; }
      queued += bytes.length; buffers.add(bytes);
      try {
        const flowing = destination.write(bytes, error => {
          queued -= bytes.length; buffers.delete(bytes); bytes.fill(0);
          if (settled) return;
          if (error) { finish('failed', 'socket_write_failed'); return; }
          completed?.();
        });
        if (!flowing && source && !blockedSources.has(source)) {
          blockedSources.add(source); source.pause();
          destination.once('drain', () => { blockedSources.delete(source); resumeSources(); });
        }
      } catch { finish('failed', 'socket_write_failed'); }
    };
    const relay = (direction, bytes) => {
      if (settled || closing) return;
      if (policy.ownershipLost()) { finish('failed', 'ownership_lost'); return; }
      const frames = direction === 'client' ? clientFrames : serverFrames;
      try {
        const accepted = inspectWebSocketFrames(frames, bytes, Date.now()); activity();
        for (const frame of accepted) write(direction === 'client' ? peer : client, frame, direction === 'client' ? client : peer);
        updateCounters();
      } catch { finish('failed', serverFrames.controlAttempt ? 'server_control_message' : 'invalid_frames'); }
    };
    client.on('error', () => { if (expectedClose()) closeNormally(peer, 'client_closed_after_logout_or_close'); else finish('failed', 'client_error'); });
    client.on('end', () => closeNormally(peer, 'client_end'));
    client.on('close', () => { if (!settled && !closing) closeNormally(peer, 'client_close'); });
    try {
      client.pause();
      let index;
      if (policy.reservation !== undefined) {
        const reserved = policy.reservation;
        requireThat(WS_RESERVATIONS.has(reserved) && reserved.state === state && reserved.entry === entry);
        WS_RESERVATIONS.delete(reserved); admitted = true; index = reserved.index;
      } else { state.seen += 1; index = state.seen - 1; }
      requireThat(acceptanceMode(policy.mode) && !policy.ownershipLost() && !policy.logoutInProgress() &&
        state.seen <= WS_LIMITS.handshakes && (admitted ? state.active === 1 : state.active < WS_LIMITS.active) &&
        Buffer.isBuffer(head) && head.length <= WS_LIMITS.queuedBytes);
      plan = websocketRequestPlan(request.url, request.method, request.rawHeaders, policy.userId, policy.mode);
      if (!admitted) { state.admitted += 1; state.active += 1; admitted = true; }
      Object.assign(entry, { index, token_fingerprint: plan.authority.fingerprint, upstream_status: null,
        handshake_delivered: false, outcome: 'pending', closed: false });
    } catch { finish('failed', 'admission_rejected'); return; }
    const capturedToken = plan.authority.token;
    authorityTask = Promise.resolve().then(() => policy.authorize(capturedToken));
    void (async () => {
      try {
        await authorityTask;
        if (settled) return;
        requireThat(!policy.ownershipLost() && !policy.logoutInProgress());
        outgoing = transport.request({ hostname: '127.0.0.1', port: 18196, method: 'GET', path: plan.path,
          headers: plan.headers, maxHeaderSize: PROXY_LIMITS.headerBytes, agent: false }, response => {
          if (settled) { response.destroy(); return; }
          entry.upstream_status = response.statusCode;
          const chunks = []; let count = 0, reply;
          response.on('error', () => finish('failed', 'handshake_response_error'));
          response.on('aborted', () => finish('failed', 'handshake_response_aborted'));
          try { reply = proxyResponsePlan(response.statusCode, response.rawHeaders, 'GET'); }
          catch { response.destroy(); finish('failed', 'handshake_response_headers'); return; }
          response.on('data', chunk => {
            if (settled) return;
            count += chunk.length;
            if (count > 65536) { response.destroy(); finish('failed', 'handshake_response_size'); return; }
            const copy = Buffer.from(chunk); chunks.push(copy); buffers.add(copy);
          });
          response.on('end', () => {
            if (settled) return;
            if (!response.complete || response.rawTrailers?.length || reply.contentLength !== null && reply.contentLength !== count) {
              finish('failed', 'handshake_response_incomplete'); return;
            }
            try {
              const bytes = Buffer.concat([websocketHTTPHead(reply.status, response.statusMessage ?? '', reply.headers), ...chunks]);
              write(client, bytes, null, () => finish('failed', 'handshake_not_accepted'));
            } catch { finish('failed', 'handshake_response_invalid'); }
          });
        });
        outgoing.maxHeadersCount = 129;
        outgoing.on('error', () => { if (!opened) finish('failed', 'handshake_transport_error'); });
        outgoing.on('upgrade', (response, socket, incomingHead) => {
          if (settled) { socket.destroy(); return; }
          peer = socket; entry.upstream_status = response.statusCode;
          peer.pause();
          peer.on('error', () => { if (expectedClose()) closeNormally(client, 'server_closed_after_logout_or_close'); else finish('failed', 'server_error'); });
          peer.on('end', () => closeNormally(client, 'server_end'));
          peer.on('close', () => { if (!settled && !closing) closeNormally(client, 'server_close'); });
          try {
            requireThat(!policy.ownershipLost() && !policy.logoutInProgress() && Buffer.isBuffer(incomingHead) && incomingHead.length <= WS_LIMITS.queuedBytes);
            const headers = websocketResponsePlan(response.statusCode, response.rawHeaders, plan);
            const serverHeadFrames = inspectWebSocketFrames(serverFrames, incomingHead, Date.now());
            const clientHeadFrames = inspectWebSocketFrames(clientFrames, head, Date.now());
            client.setTimeout(0); peer.setTimeout(0);
            client.on('data', bytes => relay('client', bytes)); peer.on('data', bytes => relay('server', bytes));
            write(client, websocketHTTPHead(response.statusCode, response.statusMessage ?? 'Switching Protocols', headers), null, () => {
              if (settled) return;
              opened = true; state.opened += 1; entry.handshake_delivered = true; clearTimeout(handshakeTimer);
              lifeTimer = setTimeout(() => finish('failed', 'lifetime_timeout'), WS_LIMITS.lifetimeMs); activity();
              resumeSources();
            });
            for (const frame of serverHeadFrames) write(client, frame, peer);
            for (const frame of clientHeadFrames) write(peer, frame, client);
            headsQueued = true; updateCounters(); resumeSources();
          } catch { finish('failed', serverFrames.controlAttempt ? 'server_control_message' : 'handshake_or_head_invalid'); }
        });
        outgoing.end();
      } catch { if (!settled) finish('failed', 'authority_or_transport_rejected'); }
    })();
  });
}

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

export function observedAuthority(raw, headers, userId, expectedToken = undefined) {
  requireThat(typeof raw === 'string' && Buffer.byteLength(raw) <= 16384 && ID.test(userId));
  const url = new URL(raw), route = url.pathname.replace(/^\/emby(?=\/)/i, '');
  const selectedUsers = [...url.searchParams].filter(([key]) => key.toLowerCase() === 'userid').map(([, value]) => value);
  const explicitUser = route === `/Users/${userId}` || route.startsWith(`/Users/${userId}/`) || route === `/UserSettings/${userId}`;
  const boundItem = typeof expectedToken === 'string' && /^\/Items\/[0-9a-f]{32}$/.test(route) &&
    (selectedUsers.length === 0 || selectedUsers.length === 1 && selectedUsers[0] === userId);
  requireThat(url.origin === ORIGIN && !url.username && !url.password && !url.hash &&
    (selectedUsers.length === 0 || selectedUsers.length === 1 && selectedUsers[0] === userId) && (explicitUser || boundItem));
  return authorityForURL(url, headers, expectedToken);
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

export function validateReadPrincipal(value, actor, serverId) {
  requireThat(record(value) && value.Id === actor.id && value.Name === actor.username && value.ServerId === serverId &&
    value.HasPassword === true && record(value.Policy) && value.Policy.IsAdministrator === false &&
    value.Policy.IsDisabled === false && record(value.Configuration));
  return true;
}

export function itemState(value, expected) {
  requireThat(record(value) && value.Id === expected.id && value.Type === expected.type && value.Name === expected.name &&
    (expected.pathOmitted ? !Object.hasOwn(value, 'Path') : value.Path === expected.path) &&
    (!expected.parentId || value.ParentId === expected.parentId) && record(value.UserData));
  return { id: expected.id, user_data: value.UserData };
}

export function compareCrossUserState(before, after) {
  requireThat(record(before) && record(after) && before.user_id === after.user_id && before.items.length === 4 && after.items.length === 4);
  return { items_unchanged: same(before.items, after.items), preferences_unchanged: same(before.preferences, after.preferences),
    configuration_unchanged: same(before.configuration, after.configuration), policy_unchanged: same(before.policy, after.policy) };
}

export function foreignAccessDenied(response) {
  return deniedResponse(response, 'access_denied');
}

export function administratorAccessDenied(response) {
  return deniedResponse(response, 'administrator_required');
}

function deniedResponse(response, code) {
  return response?.status === 403 && record(response.data) && same(Object.keys(response.data), ['ResponseStatus']) &&
    record(response.data.ResponseStatus) && response.data.ResponseStatus.ErrorCode === code &&
    typeof response.data.ResponseStatus.Message === 'string' &&
    same(Object.keys(response.data.ResponseStatus).sort(), ['ErrorCode', 'Message']);
}

export function bindOwnedMovieSource(value, expected) {
  requireThat(record(value) && expected.id === PREPARATION_ITEM && value.Id === expected.id && value.Type === 'Movie' &&
    value.Name === expected.name && value.Path === expected.path && Array.isArray(value.MediaSources) && value.MediaSources.length === 1);
  const source = value.MediaSources[0];
  requireThat(record(source) && typeof source.Id === 'string' && /^[A-Za-z0-9_-]{1,256}$/.test(source.Id) &&
    source.ItemId === expected.id && source.Path === expected.path && source.Protocol === 'File' && source.IsRemote === false);
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
    scope.loginCompleted === true && scope.logoutStarted === false && scope.source?.item_id === PREPARATION_ITEM && ID.test(scope.userId));
  const url = new URL(raw), requestPolicy = classifyBrowserRequest(raw, 'POST', 'fetch', scope.mode);
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
export function preparationResponseEvidence(status, bytes, source) {
  const result = { status, response_bytes: bytes.length, validated: false, play_session_id: null, reason: 'http_status_not_200' };
  if (status !== 200) return result;
  try {
    requireThat(Buffer.isBuffer(bytes) && bytes.length > 0 && bytes.length <= LIMIT);
    const value = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes));
    requireThat(record(value) && typeof value.PlaySessionId === 'string' && /^play_[0-9a-f]{32}$/.test(value.PlaySessionId) &&
      Array.isArray(value.MediaSources) && value.MediaSources.length === 1);
    const actual = value.MediaSources[0];
    requireThat(record(actual) && actual.Id === source.source_id && actual.ItemId === source.item_id && actual.Path === source.path);
    Object.assign(result, { validated: true, play_session_id: value.PlaySessionId, reason: null });
  } catch { result.reason = 'response_identity_or_shape_rejected'; }
  return result;
}

export function homeNavigationLocation(raw, previous) {
  try {
    const url = new URL(raw), route = url.hash.split('?')[0];
    return { same_origin: url.origin === ORIGIN && !url.username && !url.password,
      supported_path: url.pathname === '/web/index.html', navigation_observed: raw !== previous,
      route: ['', '#!/home', '#/home', '#!/home.html', '#/home.html'].includes(route) ? 'home' : 'other' };
  } catch { return { same_origin: false, supported_path: false, navigation_observed: false, route: 'invalid' }; }
}

export function selectHomeControl(links, buttons) {
  requireThat([links, buttons].every(value => Number.isSafeInteger(value) && value >= 0 && value <= 64));
  if (links === 1) return 'link';
  requireThat(links === 0 && buttons <= 1);
  return buttons === 1 ? 'button' : null;
}

export function confirmedHomeNavigation(value) {
  return value?.same_origin === true && value.supported_path === true && value.navigation_observed === true && value.route === 'home' &&
    value.library_card_count === 1 && value.detail_heading_count === 0 && value.card_present === true &&
    (value.card_id_present === false || value.card_id_present === true && value.card_id_matches === true);
}

export function readBoundedJSON(url, token, transport = http) {
  requireThat(url instanceof URL && url.origin === ORIGIN && !url.username && !url.password && !url.hash &&
    url.pathname.startsWith('/emby/') && typeof token === 'string' && token.length > 0 && token.length <= 4096 &&
    !/[\x00-\x20\x7f-\uffff,"\\]/.test(token));
  return new Promise((resolve, reject) => {
    let request, response, settled = false, count = 0;
    const chunks = [], timer = setTimeout(() => finish(null), 10000);
    function finish(value) {
      if (settled) return;
      settled = true; clearTimeout(timer);
      if (response && !response.complete) response.destroy();
      if (request && !request.destroyed) request.destroy();
      for (const chunk of chunks) chunk.fill(0);
      if (value) resolve(value); else reject(new Error('cross_user_read_failed'));
    }
    try {
      request = transport.request({ hostname: '127.0.0.1', port: 18196, method: 'GET',
        path: url.pathname + url.search, agent: false, maxHeaderSize: 16384,
        headers: { Host: '127.0.0.1:18196', Accept: 'application/json', 'Accept-Encoding': 'identity',
          Connection: 'close', 'X-Emby-Token': token } }, incoming => {
        response = incoming;
        const declared = incoming.headers['content-length'];
        if (incoming.statusCode >= 300 && incoming.statusCode < 400 ||
            declared !== undefined && (!/^[0-9]+$/.test(declared) || Number(declared) > LIMIT)) { finish(null); return; }
        incoming.on('error', () => finish(null)); incoming.on('aborted', () => finish(null));
        incoming.on('close', () => { if (!incoming.complete) finish(null); });
        incoming.on('data', chunk => { if (settled) return; count += chunk.length; if (count > LIMIT) finish(null); else chunks.push(Buffer.from(chunk)); });
        incoming.on('end', () => {
          if (settled) return;
          if (!incoming.complete) { finish(null); return; }
          try {
            const bytes = Buffer.concat(chunks), data = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes));
            bytes.fill(0); finish({ status: incoming.statusCode, bytes: count, data });
          } catch { finish(null); }
        });
      });
      request.maxHeadersCount = 128;
      request.setTimeout(3000, () => finish(null)); request.on('error', () => finish(null));
      request.on('upgrade', (_response, socket) => { socket.destroy(); finish(null); }); request.end();
    } catch { finish(null); }
  });
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

export function crossUserResult(report) {
  return acceptanceMode(report.mode) &&
    (report.mode === 'acceptance-preparation' ? report.preparation_scope === PREPARATION_SCOPE : report.preparation_scope == null) &&
    report.failure === null && report.accounts?.length === 2 && report.accounts[0].id !== report.accounts[1].id &&
    report.accounts.every((actor, index) => ID.test(actor.id) && SHA.test(actor.token_fingerprint) &&
      actor.login?.status === 200 && actor.login?.request_count === 1 && actor.principal_confirmed === true && actor.ui?.outcome === 'passed' &&
      actor.ordinary_authority_confirmed === true && actor.page_error_count === 0 &&
      actor.own_item_reads === 8 && actor.foreign_read?.status === 403 && actor.foreign_read?.error_code === 'access_denied' &&
      actor.foreign_read.target_user_id === report.accounts[1 - index].id &&
      record(actor.comparison) && same(Object.keys(actor.comparison).sort(),
        ['items_unchanged', 'preferences_unchanged', 'configuration_unchanged', 'policy_unchanged'].sort()) &&
      Object.values(actor.comparison).every(value => value === true) &&
      actor.logout?.status === 204 && actor.logout?.login_view_visible === true && actor.closed === true &&
      actor.session_proof?.outcome === 'all_observed_logout_tokens_rejected' && actor.session_proof.entries.length === 1 &&
      actor.session_proof.entries[0].ui_request.response_status === 204 && actor.session_proof.entries[0].verification.status === 401 &&
      actor.session_proof.entries[0].token_fingerprint === actor.token_fingerprint &&
      actor.proxy?.mode === report.mode && actor.proxy.login === 1 && actor.proxy.logout === 1 && actor.proxy.active === 0 &&
      actor.proxy_login_status === 200 &&
      actor.proxy.seen === actor.proxy.admitted && actor.proxy.admitted === actor.proxy.completed &&
      actor.proxy.failed === 0 && actor.proxy.rejected === 0 && actor.proxy.upgrade_rejections === 0 &&
      actor.proxy_logout?.completed === true && actor.proxy_logout.status === 204 && actor.proxy_logout.token_fingerprint === actor.token_fingerprint &&
      (report.mode === 'acceptance-preparation' ? actor.proxy.preparation === 1 && actor.preparation?.request_validated === true &&
        actor.preparation.item_id === PREPARATION_ITEM && actor.preparation.token_fingerprint === actor.token_fingerprint &&
        actor.preparation.completed === true && actor.preparation.response?.status === 200 && actor.preparation.response.validated === true &&
        actor.preparation.ui_status === 200 && actor.preparation.ui_finished === true
        : (actor.proxy.preparation ?? 0) === 0) &&
      actor.websocket?.opened >= 1 && actor.websocket.opened <= WS_LIMITS.handshakes && actor.websocket.closed === actor.websocket.opened &&
      actor.websocket.seen === actor.websocket.admitted && actor.websocket.admitted === actor.websocket.opened &&
      actor.websocket.active === 0 && actor.websocket.failed === 0 && actor.websocket.control_attempts === 0 &&
      actor.websocket.entries.length === actor.websocket.seen && actor.websocket.entries.every(entry =>
        entry.outcome === 'closed' && entry.handshake_delivered === true && entry.upstream_status === 101 &&
        (!Object.hasOwn(entry, 'connect_transport') || entry.connect_transport === true && entry.proxy_connect_status === 200) &&
        entry.token_fingerprint === actor.token_fingerprint && entry.server_control_attempted === false) &&
      actor.network?.forbidden_mutations === 0 && actor.network?.playback_attempts === 0 &&
      actor.network?.observer_errors === 0 && actor.network?.overflow === 0 && actor.network?.guard_errors === 0) &&
    report.accounts[0].token_fingerprint !== report.accounts[1].token_fingerprint;
}

export function preloginDiagnosticsComplete(report) {
  const actor = report.accounts?.[0];
  return report.mode === 'prelogin' && report.client_acceptance === false && report.failure === null && report.accounts?.length === 1 &&
    report.api_reads?.length === 0 && actor?.prelogin?.ready === true && actor.prelogin.credentials_submitted === false &&
    actor.prelogin.expected_account_only === true && actor.prelogin.service_workers === 'allow' && actor.prelogin.client_acceptance === false &&
    actor.prelogin.all_http_via_proxy === true && actor.proxy?.mode === 'prelogin' && actor.proxy.seen > 0 &&
    actor.proxy.seen === actor.proxy.admitted && actor.proxy.admitted === actor.proxy.completed &&
    actor.proxy.active === 0 && actor.proxy.rejected === 0 && actor.proxy.failed === 0 && actor.proxy.login === 0 &&
    actor.proxy.capabilities === 0 && actor.proxy.logout === 0 && actor.proxy.upgrade_rejections === 0 &&
    (actor.proxy.preparation ?? 0) === 0 &&
    actor.websocket?.entries?.length === 0 && ['seen', 'admitted', 'opened', 'closed', 'failed', 'active', 'control_attempts']
      .every(key => actor.websocket[key] === 0) &&
    actor.closed === true && actor.login?.attempted === false && actor.login.status === null &&
    actor.login.credentials_filled === false && actor.login.request_count === 0 && actor.logout?.status === null &&
    actor.logout.attempted !== true && actor.own_item_reads === 0 && !Object.hasOwn(actor, 'foreign_read') &&
    !Object.hasOwn(actor, 'principal_confirmed') && !Object.hasOwn(actor, 'ordinary_authority_confirmed') && !Object.hasOwn(actor, 'token_fingerprint') &&
    actor.session_proof?.outcome === 'no_logout_observed' && actor.session_proof.entries.length === 0 &&
    actor.bootstrap_diagnostics?.length > 0 && ['forbidden_mutations', 'playback_attempts', 'observer_errors', 'overflow', 'guard_errors']
      .every(key => actor.network?.[key] === 0);
}

class BrowserActor {
  constructor(account, pin, report, mode = 'acceptance', preparationScope = undefined) {
    requireProxyExecutionMode(mode, preparationScope); this.mode = mode; this.preparationScope = preparationScope;
    this.account = account; this.pin = pin; this.report = report; this.token = null; this.proven = false;
    this.diagnosticSecrets = () => [this.account.password, this.token]; this.rememberSecret = () => {};
    this.pending = new Set(); this.entries = new WeakMap(); this.sockets = new Set(); this.closed = false;
    this.loginIntent = false; this.logoutIntent = false; this.ownershipLost = false; this.phase = 'not_started';
    this.started = Date.now(); this.preloginOnly = mode === 'prelogin';
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
      this.movie?.id === PREPARATION_ITEM && this.preparationSource?.item_id === PREPARATION_ITEM && typeof this.token === 'string';
  }
  async guardProxy() {
    this.proxyPending = new Set(); this.websocketPending = new Set(); this.socketGuards = new Map();
    this.report.proxy_requests = [];
    this.report.proxy = { mode: this.mode, seen: 0, admitted: 0, completed: 0,
      rejected: 0, failed: 0, active: 0, login: 0, capabilities: 0, logout: 0, preparation: 0, requestBytes: 0, responseBytes: 0, upgrade_rejections: 0 };
    this.report.websocket = { seen: 0, admitted: 0, opened: 0, closed: 0, failed: 0, active: 0, control_attempts: 0, entries: [] };
    const policy = { state: this.report.proxy,
      intents: () => ({ login: this.loginIntent, logout: this.logoutIntent, ownershipLost: this.ownershipLost, preparation: this.preparationAllowed() }),
      onRequest: (request, plan, body) => {
        if (plan.kind === 'preparation') {
          requireThat(this.preparationAllowed() && !this.report.preparation);
          this.report.preparation = { request_validated: false, completed: false, response: null, ui_status: null, ui_finished: false };
          const evidence = preparationRequestEvidence(request.url, request.rawHeaders, body, { mode: this.mode, phase: this.phase,
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
      const classification = classifyBrowserRequest(request.url(), request.method(), request.resourceType(), this.mode);
      if (this.report.requests.length >= 2000) { this.report.network.overflow += 1; return; }
      const entry = { index: this.report.requests.length, phase: this.phase, kind: classification.kind,
        method: ['GET', 'HEAD', 'OPTIONS', 'POST'].includes(request.method()) ? request.method() : 'OTHER',
        status: null, finished: false, failed: false, elapsed_ms: Date.now() - this.started,
        ...browserRequestDiagnostic(request.url(), request.resourceType(), request.method()),
        owned_preparation: Boolean(this.movie?.id === PREPARATION_ITEM && request.method() === 'POST' && classification.kind === 'preparation'),
        own_movie: Boolean(this.movie && request.method() === 'GET' && ownRequest(request.url(), this.account.id, this.movie.id)) };
      this.report.requests.push(entry); this.entries.set(request, entry);
      if (!entry.owned_preparation && (request.method() !== 'GET' || classification.kind !== 'read' || !this.loginIntent)) return;
      const url = new URL(request.url()), route = url.pathname.replace(/^\/emby(?=\/)/i, '');
      const explicitUser = route === `/Users/${this.account.id}` || route.startsWith(`/Users/${this.account.id}/`) || route === `/UserSettings/${this.account.id}`;
      const boundItem = entry.own_movie && this.proven && typeof this.token === 'string';
      if (!entry.owned_preparation && !explicitUser && !boundItem) return;
      if (this.pending.size >= 16) { this.report.network.observer_errors += 1; return; }
      const capture = (async () => {
        const headers = await bounded(request.allHeaders(), 1500);
        const authority = entry.owned_preparation ? authorityForURL(url, headers, this.token)
          : observedAuthority(request.url(), headers, this.account.id, boundItem ? this.token : undefined);
        if (!authority) return;
        this.rememberSecret(authority.token);
        requireThat(this.token === null || this.token === authority.token);
        this.token = authority.token; this.report.token_fingerprint = authority.fingerprint;
        this.report.token_sources = authority.sources; entry.token_matches_session = true;
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
          this.mode);
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
    const heading = this.page.locator('a.sectionTitleTextButton').filter({ hasText: 'Latest M3e Client Movies' }).filter({ visible: true });
    await heading.waitFor({ state: 'visible', timeout: 20000 });
    requireThat(await heading.count() === 1 && (await heading.innerText()).replace(/\ue5e1/g, '').trim() === 'Latest M3e Client Movies');
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
      const titles = this.page.getByText('M3e Client Movies', { exact: true }).filter({ visible: true });
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
        if (prepared && this.report.preparation?.completed && this.report.preparation.response?.validated) break;
        await this.page.waitForTimeout(100);
      } while (Date.now() < deadline);
      requireThat(prepared && this.report.preparation?.completed && this.report.preparation.response?.status === 200 &&
        this.report.preparation.response.validated && this.report.proxy.preparation === 1);
      Object.assign(this.report.preparation, { ui_status: prepared.status, ui_finished: true, ui_request_index: prepared.index });
      await this.inactiveMedia('movie_preparation_completed');
    }
    this.phase = 'ui_return_home'; await this.returnHome(movie, libraryId);
    await this.assertPinned(); this.report.ui.outcome = 'passed';
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

/** Fresh browsers and explicit read-only API checks; never save browser storage. */
export async function runCrossUserAcceptance(input) {
  remoteEnvironment();
  const options = parseCrossUserArguments(Object.entries(input).flatMap(([name, value]) => ['--' + name, value]));
  requireProxyExecutionMode(options.mode, options['preparation-scope']);
  await privateDirectory(ROOT); process.umask(0o077);
  await fs.mkdir(options.output, { mode: 0o700 });
  const deadline = Date.now() + 240000;
  const report = { marker: 'goby-client-cross-user-m3e-v1', format: 5, mode: options.mode, client_acceptance: false,
    preparation_scope: options['preparation-scope'] ?? null,
    diagnostic_only: options.mode === 'prelogin', result: 'in_progress', failure: null,
    started_at: new Date().toISOString(), accounts: [], api_reads: [],
    scope: { accounts: 'Existing AV viewer A and initial viewer B; two independent fresh browsers',
      ui: 'Manual login, Home, owned Movie title navigation, Home, UI signout',
      api: 'Explicit own-user state reads and two foreign-user Movie denials; never UI evidence',
      state_mutations: 'No policy, metadata, preference or UserData writes; no playback',
      state_window: 'Per-user baseline after its UI login; both final snapshots before either UI signout',
      service_workers: 'Allowed; all browser and worker HTTP passes a fixed-target forwarding proxy with no target bypass',
      websocket_scope: 'Genuine fixed-target upgrades, directly or inside one bounded plaintext CONNECT handshake, require the fresh UI login and an explicit ordinary-user authority check; no general tunnel; wire frames remain unchanged and server control commands fail the observation',
      normal_login_effects: 'Authentication sessions, devices and audit records may be added; authenticated HTTP and WebSocket presence may update session and device activity',
      database_wide_preservation_claimed: false, library_policy_restriction_milestone: 'not_exercised',
      token_storage: 'Observed request authority and opaque relay buffers stay in memory; no storageState, HAR, trace, authentication-response decoding or token extraction from authentication responses' },
    limits: { work_ms: 240000, api_requests: 40, api_timeout_ms: 10000, api_response_bytes: LIMIT, browser_requests_per_user: 2000, proxy: PROXY_LIMITS, websocket: WS_LIMITS,
      browser_diagnostics: { events_per_kind_per_actor: DIAGNOSTIC_ENTRY_LIMIT, input_characters: DIAGNOSTIC_INPUT_LIMIT,
        output_characters: DIAGNOSTIC_OUTPUT_LIMIT, success_requires_page_error_count: 0,
        privacy: 'Only error name and message; no stacks or browser internals; bounded messages are redacted at capture and again before writing' },
      cleanup: 'Cleanup has its own bounded UI, proof and browser-close waits after the work deadline' } };
  if (options.mode === 'prelogin') Object.assign(report.scope, {
    accounts: 'One fresh anonymous browser; the AV account identifier is an expected fixture binding only',
    ui: 'Load the initial page and observe login controls; no click, credential fill, authentication or signout',
    api: 'No explicit API reads and no identity-isolation claim', state_window: 'Pre-login diagnostics only',
    normal_login_effects: 'No login is submitted in this mode', websocket_scope: 'All Upgrade and CONNECT requests are rejected',
  });
  if (options.mode === 'acceptance-preparation') Object.assign(report.scope, {
    state_mutations: 'One physical owned-Movie PlaybackInfo POST per actor is explicitly allowed during detail preparation; no Playing, media delivery, policy, metadata, preference or UserData value writes',
    preparation: { id: PREPARATION_SCOPE, item_id: PREPARATION_ITEM, physical_posts_per_actor: 1,
      source_binding: 'Unique MediaSources entry from the existing authenticated before-snapshot Movie read',
      acknowledged_effects: ['Prepare or refresh a Prepared row under each new UI authentication session',
        'Expire only the two input05 Prepared rows whose authentication sessions have been revoked; preserve the other 18 prior play rows exactly',
        'A negotiated HLS descriptor may be registered in memory without starting an encoder'],
      existing_database_baseline: { play_sessions: 20, auth_sessions: 49, playback_references: 0, user_data: 5, encoding_jobs: 0 },
      existing_rows: 'No old play or reference deletion; all 49 prior authentication rows, all five UserData rows and zero encoding jobs remain unchanged',
      existing_movie_userdata_rows: 'Confirmed present by the root-owned database ledger preflight',
      database_proof: 'A fresh root-owned before ledger must match the input05 after ledger; use the source28-page-error-01 comparison authority, never the original 18-play/47-auth authority; no whole-database equality claim' },
  });
  let terminalReport = report;
  let phase = 'input_closure', fixture, accounts = [], pins = [], observedBefore = new Map(), items, reads = 0, trustLost = false;
  const sessions = [], secrets = [];
  const rememberSecret = value => {
    if (typeof value === 'string' && value.length > 0 && !secrets.includes(value)) secrets.push(value);
  };
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
    requireThat(session.token && Date.now() < deadline && ++reads <= 40);
    await session.assertPinned();
    const url = new URL(route, ORIGIN); for (const [key, value] of Object.entries(query)) url.searchParams.set(key, value);
    const response = await readBoundedJSON(url, session.token);
    report.api_reads.push({ actor: session.account.slot, label, method: 'GET', status: response.status,
      response_bytes: response.bytes, is_ui_request: false, query_field_names: Object.keys(query) });
    await session.assertPinned(); return response;
  }
  async function snapshot(session, label) {
    const actor = session.account;
    const user = await read(session, `/emby/Users/${actor.id}`, `${label}_own_user`);
    requireThat(user.status === 200 && validateReadPrincipal(user.data, actor, fixture.serverId));
    session.proven = session.report.principal_confirmed = true;
    const prefs = await read(session, `/emby/UserSettings/${actor.id}`, `${label}_own_preferences`);
    requireThat(prefs.status === 200 && record(prefs.data));
    const rows = [];
    for (const expected of items) {
      const actual = await read(session, `/emby/Users/${actor.id}/Items/${expected.id}`, `${label}_own_${expected.key}`);
      requireThat(actual.status === 200); rows.push(itemState(actual.data, expected)); session.report.own_item_reads += 1;
      if (options.mode === 'acceptance-preparation' && expected.key === 'movie') {
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
        const principal = await read(session, `/emby/Users/${session.account.id}`, 'explicit_session_own_user_authority');
        requireThat(principal.status === 200 && validateReadPrincipal(principal.data, session.account, fixture.serverId));
        const restricted = await read(session, '/emby/Users/Query', 'explicit_session_nonadministrator_authority', { Limit: '0' });
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
      report.input_closure[name] = file.sha256; pins.push([filename, file.sha256, [0o600, 0o644, 0o700, 0o755], false]);
    }
    phase = 'fixture_binding';
    fixture = await loadGobyAVFixture({ expectedSHA256: options['candidate-sha256'], expectedMusicScanReceiptSHA256: options['music-scan-receipt-sha256'],
      musicUpgradeChain: { path: options['music-upgrade-chain'], sha256: options['music-upgrade-chain-sha256'] } });
    requireThat(fixture.origin === ORIGIN && fixture.music && fixture.evidence.schema_binding.source === options.source &&
      fixture.evidence.schema_binding.source_manifest_sha256 === options['source-manifest-sha256']);
    await privateDirectory(options.source);
    const manifestPath = `${options.source}/backup-source-inputs.json`, manifest = await readOwnedFile(manifestPath, [0o600, 0o644]);
    requireThat(manifest.sha256 === options['source-manifest-sha256']); pins.push([manifestPath, manifest.sha256, [0o600, 0o644], true]);
    const stateFile = await readOwnedFile(STATE);
    requireThat(stateFile.sha256 === fixture.evidence.fixture_state_sha256);
    if (options.mode === 'prelogin') {
      accounts = [{ slot: 'A', id: fixture.accountId, username: fixture.username, password: null,
        credentialsPath: A_CREDENTIALS, credentialsSHA: fixture.evidence.browser_alias_sha256 }];
    } else {
      const aFile = await readOwnedFile(A_CREDENTIALS), bFile = await readOwnedFile(B_CREDENTIALS);
      for (const credentials of [aFile.value, bFile.value]) {
        rememberSecret(credentials?.admin?.password); rememberSecret(credentials?.viewer?.password);
      }
      accounts = bindCrossUserCredentials(stateFile, aFile, bFile, fixture);
      pins.push([B_CREDENTIALS, bFile.sha256, [0o600], true]);
      report.initial_viewer_credentials_sha256 = bFile.sha256;
    }
    report.fixture = fixture.evidence;
    for (const account of accounts) rememberSecret(account.password);
    await writePrivate(path.join(options.output, 'intent.json'), JSON.stringify({ marker: report.marker, mode: options.mode, candidate_sha256: options['candidate-sha256'],
      source: options.source, source_manifest_sha256: options['source-manifest-sha256'], fixture_state_sha256: stateFile.sha256,
      account_ids: acceptanceMode(options.mode) ? accounts.map(actor => actor.id) : [], input_closure: report.input_closure,
      preparation_scope: report.preparation_scope, preparation: options.mode === 'acceptance-preparation' ? report.scope.preparation : null,
      retry_policy: 'Never reuse this output' }, null, 2) + '\n');
    for (const account of accounts) {
      const actorReport = {}; report.accounts.push(actorReport);
      const session = new BrowserActor(account, pin, actorReport, options.mode, options['preparation-scope']); sessions.push(session);
      session.diagnosticSecrets = () => secrets; session.rememberSecret = rememberSecret;
      session.authorizeSocket = () => ordinaryPrincipal(session);
      phase = options.mode === 'prelogin' ? 'prelogin_diagnostic' : `login_${account.slot}`;
      requireThat(Date.now() < deadline); await session.open({ preloginOnly: options.mode === 'prelogin' });
      if (options.mode === 'prelogin') break;
      rememberSecret(session.token);
      await ordinaryPrincipal(session);
      if (!items) {
        const catalog = await read(session, `/emby/Users/${account.id}/Items`, 'owned_movie_discovery',
          { ParentId: fixture.evidence.libraries.movies, Recursive: 'true', IncludeItemTypes: 'Movie', Fields: 'Path', Limit: '64' });
        requireThat(catalog.status === 200 && catalog.data.TotalRecordCount === 1 && catalog.data.Items?.length === 1);
        const movie = catalog.data.Items[0];
        requireThat(ID.test(movie.Id) && movie.Type === 'Movie' && movie.Name === 'M3e Client Movie' && movie.Path === '/opt/goby-fixtures/client-m3e/Movies/M3e Client Movie.mp4');
        items = [{ key: 'movie', id: movie.Id, name: movie.Name, path: movie.Path, type: 'Movie' },
          ...['mp3', 'flac'].map(key => ({ key, ...fixture.music.tracks[key], type: 'Audio' })),
          { key: 'album', id: fixture.music.album.id, name: fixture.music.album.name, type: 'MusicAlbum',
            pathOmitted: true, parentId: fixture.evidence.libraries.music }];
        requireThat(new Set(items.map(item => item.id)).size === 4);
        report.owned_items = items.map(item => ({ key: item.key, id: item.id, type: item.type }));
      }
      phase = `before_${account.slot}`;
      const before = await snapshot(session, 'before'); observedBefore.set(account.slot, before); actorReport.before = stateEvidence(before);
    }
    if (acceptanceMode(options.mode)) {
    requireThat(sessions.length === 2 && sessions[0].token !== sessions[1].token);
    report.initial_states_differ = !same([...observedBefore.values()][0].items.map(item => item.user_data),
      [...observedBefore.values()][1].items.map(item => item.user_data)) || !same([...observedBefore.values()][0].preferences, [...observedBefore.values()][1].preferences);
    report.isolation_basis = 'Each independently proven user must receive own-item 200 and foreign-user 403; identical state is not isolation proof';
    for (const session of sessions) {
      phase = `browse_${session.account.slot}`;
      try {
        requireThat(Date.now() < deadline);
        await session.browse(items[0], fixture.evidence.libraries.movies);
      } catch (error) {
        session.report.ui.outcome = 'failed';
        session.report.ui.failure = { ...safeBrowserFailure(error, session.phase), elapsed_ms: Date.now() - session.started };
        fail(phase);
      }
      try {
        phase = `foreign_read_${session.account.slot}`;
        const other = accounts.find(account => account.id !== session.account.id);
        const foreign = await read(session, `/emby/Users/${other.id}/Items/${items[0].id}`, 'explicit_foreign_user_movie_denial');
        session.report.foreign_read = { target_user_id: other.id, status: foreign.status,
          error_code: foreign.data?.ResponseStatus?.ErrorCode === 'access_denied' ? 'access_denied' : 'unexpected', is_ui_request: false };
        requireThat(foreignAccessDenied(foreign));
      } catch { fail(phase); }
    }
    }
  } catch { fail(phase); }
  finally {
    for (const session of sessions) rememberSecret(session.token);
    for (const session of sessions) {
      if (session.proven && observedBefore.has(session.account.slot) && !session.ownershipLost) {
        try {
          const after = await snapshot(session, 'after'); session.report.after = stateEvidence(after);
          session.report.comparison = compareCrossUserState(observedBefore.get(session.account.slot), after);
          if (!Object.values(session.report.comparison).every(Boolean)) fail(`state_changed_${session.account.slot}`);
        } catch { fail(`after_${session.account.slot}`); }
      }
    }
    for (const session of [...sessions].reverse()) {
      try { if (!await session.close()) fail(`cleanup_${session.account.slot}`); }
      catch { fail(`cleanup_${session.account.slot}`); session.report.closed = false; }
    }
    if (fixture) try { await pin(); } catch { fail('final_fixture_pin'); }
    resanitizeBrowserDiagnostics(report, secrets);
    report.finished_at = new Date().toISOString();
    const observedRequests = report.accounts.flatMap(actor => actor.requests ?? []);
    report.client_interventions = {
      external_posts_blocked: observedRequests.filter(entry => entry.category === 'external_post').length,
      playback_preparations_blocked: observedRequests.filter(entry => entry.category === 'playback_preparation' && entry.kind === 'playback').length,
      external_network_permission_granted: false,
      impact: 'Blocked requests remain reported client interventions and are not counted as successful endpoint responses',
    };
    report.result = options.mode === 'prelogin' ? preloginDiagnosticsComplete(report) ? 'diagnostic_only' : 'failed'
      : crossUserResult(report) ? 'passed' : 'failed';
    report.client_acceptance = report.result === 'passed';
    if (report.result === 'failed') report.failure ??= options.mode === 'prelogin' ? 'prelogin_diagnostic_incomplete'
      : report.accounts.some(actor => actor.page_error_count !== 0) ? 'page_errors_observed'
        : report.client_interventions.playback_preparations_blocked > 0 ? 'needs_preparation_scope'
        : options.mode === 'acceptance-preparation' && report.accounts.some(actor => !actor.preparation?.completed) ? 'preparation_incomplete' : 'acceptance_incomplete';
    let encoded = JSON.stringify(report, null, 2) + '\n';
    let secretPresent = true;
    try {
      const lower = encoded.toLowerCase();
      secretPresent = diagnosticSecretVariants(secrets).some(secret => lower.includes(secret.toLowerCase()));
    } catch { /* Invalid secret bounds must never permit a diagnostic report to escape. */ }
    if (secretPresent) {
      terminalReport = { marker: report.marker, mode: options.mode, preparation_scope: report.preparation_scope,
        client_acceptance: false, result: 'failed', failure: 'report_secret_guard',
        closed: sessions.every(session => session.report.closed === true) };
      encoded = JSON.stringify(terminalReport) + '\n';
    }
    await writePrivate(path.join(options.output, 'report.json'), encoded);
    for (const actor of accounts) actor.password = null;
    secrets.fill(null); observedBefore.clear();
  }
  return terminalReport;
}

if (process.argv[1] && path.resolve(process.argv[1]) === SELF) {
  try {
    const report = await runCrossUserAcceptance(parseCrossUserArguments(process.argv.slice(2)));
    process.stdout.write(JSON.stringify({ result: report.result, mode: report.mode, client_acceptance: report.client_acceptance,
      marker: report.marker, failure: report.failure }) + '\n');
    if (!['passed', 'diagnostic_only'].includes(report.result)) process.exitCode = 1;
  } catch {
    process.stdout.write(JSON.stringify({ result: 'failed', marker: 'goby-client-cross-user-m3e-v1', failure: 'setup_or_terminal_report' }) + '\n');
    process.exitCode = 1;
  }
}
