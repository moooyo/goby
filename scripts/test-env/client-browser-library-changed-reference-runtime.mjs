/** A fixed-reference browser transport with passive, bounded public evidence. */
import http from 'node:http';
import { createHash } from 'node:crypto';
import { createRequire } from 'node:module';
import { TextDecoder } from 'node:util';
import { gunzipSync, inflateSync, brotliDecompressSync } from 'node:zlib';
import { createBrowserSessionProof } from './client-browser-session-proof.mjs';

export const REFERENCE_ORIGIN = 'http://127.0.0.1:18197';
export const REFERENCE_VIEWER = 'c5f36699a54f4971a891682cd9de410f';
export const REFERENCE_SERVER = 'f56dec8ff7414847873064c4be9fba74';
export const REFERENCE_RUNTIME_LIMITS = Object.freeze({ requests: 2000, concurrent: 16, connections: 40,
  requestBytes: 131072, responseBytes: 16777216, totalResponseBytes: 134217728, headerBytes: 16384,
  captureBytes: 1048576, httpMs: 25000, idleMs: 8000, capabilities: 16, sockets: 2,
  messageBytes: 65536, messages: 2048, wireBytes: 8388608, frames: 4096, framesPerSecond: 128,
  socketMs: 900000, socketIdleMs: 130000, queuedBytes: 1048576,
  cleanupRequests: 64, cleanupConcurrent: 8, cleanupRequestBytes: 65536, cleanupResponseBytes: 16777216 });
export const REFERENCE_HTTP_PHASES = Object.freeze(['admission', 'request_body', 'login_form', 'client_metadata', 'pin',
  'upstream_create', 'upstream_setup', 'upstream_end', 'upstream_response', 'response_body', 'downstream_write', 'completed']);
export const REFERENCE_HTTP_ERROR_CODES = Object.freeze(['login_content_type_rejected', 'login_form_encoding_rejected',
  'login_form_fields_rejected', 'login_username_mismatch', 'login_password_mismatch', 'admission_rejected',
  'request_body_rejected', 'client_metadata_rejected', 'pin_rejected', 'upstream_create_failed', 'upstream_setup_failed',
  'upstream_end_failed', 'upstream_response_rejected', 'response_body_rejected', 'downstream_write_failed', 'http_timeout',
  'request_transport_error', 'request_aborted', 'downstream_transport_error', 'downstream_closed', 'upstream_idle_timeout',
  'upstream_transport_error', 'unexpected_upgrade', 'incomplete_exchange']);
const LOGIN_FORM_CONTENT_TYPE = 'application/x-www-form-urlencoded; charset=UTF-8';
export const REFERENCE_REQUEST_CONTENT_TYPES = Object.freeze([null, LOGIN_FORM_CONTENT_TYPE, 'application/json', 'text/html',
  'text/plain', 'text/css', 'text/javascript', 'application/javascript', 'image/png', 'image/jpeg', 'image/svg+xml',
  'image/webp', 'image/x-icon', 'font/woff', 'font/woff2', 'application/octet-stream', 'other']);
const PLAYWRIGHT = '/opt/goby-test/inactive-dependencies-m5h/node_modules/playwright';
const TOKEN_KEYS = new Set(['token', 'accesstoken', 'api_key', 'x-emby-token', 'x-mediabrowser-token']);
const SECRET_KEYS = new Set([...TOKEN_KEYS, 'password', 'pw', 'passwordhash', 'authorization', 'x-emby-authorization']);
const PUBLIC_QUERY = new Set(['userid', 'parentid', 'ids', 'includeitemtypes', 'excludeitemtypes', 'recursive',
  'fields', 'sortby', 'sortorder', 'limit', 'startindex', 'filters', 'enabletotalrecordcount', 'imagetypelimit',
  'enableimages', 'enableuserdata', 'collapseboxset', 'isfolder']);
const HOP_HEADERS = new Set(['connection', 'proxy-connection', 'keep-alive', 'transfer-encoding', 'te', 'trailer',
  'upgrade', 'proxy-authenticate', 'proxy-authorization']);
const WS_PATHS = new Set(['/', '/emby', '/emby/', '/embywebsocket', '/emby/socket']);
const WS_GUID = '258EAFA5-E914-47DA-95CA-C5AB0DC85B11';
const FORBIDDEN_MESSAGES = new Set(['play', 'playstate', 'generalcommand']);
const BOOTSTRAP_EXTERNAL_ORIGIN = 'https://mb3admin.com';
const BOOTSTRAP_EXTERNAL_ROUTE = '/admin/service/registration/validateDevice';
const BOOTSTRAP_EXTERNAL_KEYS = ['serverId', 'deviceId', 'deviceName', 'appName', 'appVersion', 'viewOnly'];
const L = REFERENCE_RUNTIME_LIMITS;
const record = value => value !== null && typeof value === 'object' && !Array.isArray(value);
const sha = value => createHash('sha256').update(value).digest('hex');
const safeMethod = value => ['GET', 'HEAD', 'OPTIONS', 'POST', 'PUT', 'PATCH', 'DELETE', 'CONNECT', 'TRACE'].includes(value) ? value : 'OTHER';
function safeContentType(value) {
  const type = typeof value === 'string' ? value.split(';', 1)[0].trim().toLowerCase() : null;
  return ['application/json', 'text/html', 'text/plain', 'text/css', 'text/javascript', 'application/javascript',
    'image/png', 'image/jpeg', 'image/svg+xml', 'image/webp', 'image/x-icon', 'font/woff', 'font/woff2',
    'application/octet-stream'].includes(type) ? type : type === null ? null : 'other';
}
function guard(value, reason = 'reference_runtime_guard_rejected') { if (!value) throw new Error(reason); }
function bounded(promise, ms, reason = 'reference_operation_timeout') {
  let timer;
  return Promise.race([promise, new Promise((_, reject) => { timer = setTimeout(() => reject(new Error(reason)), ms); })])
    .finally(() => clearTimeout(timer));
}

/** This guard performs no probe and never starts a local browser. */
export function requireReferenceRuntimeEnvironment(environment = process) {
  guard(environment.platform === 'linux' && environment.getuid?.() === 0 && !environment.env?.DEBUG && !environment.env?.PWDEBUG,
    'reference_runtime_requires_remote_linux_root');
}

function normalizedURL(raw, websocket = false) {
  guard(typeof raw === 'string' && Buffer.byteLength(raw) <= L.headerBytes && !raw.includes('\\'));
  const url = new URL(raw);
  guard((url.protocol === 'http:' || websocket && url.protocol === 'ws:') &&
    `${url.protocol === 'ws:' ? 'http:' : url.protocol}//${url.host}` === REFERENCE_ORIGIN &&
    !url.username && !url.password && !url.hash && url.href === raw);
  const decoded = decodeURIComponent(url.pathname);
  guard(!/[\\\x00-\x20\x7f]/.test(decoded) && !/%[0-9a-f]{2}/i.test(decoded));
  const route = decoded.replace(/^\/emby(?=\/)/i, '');
  return { url, route };
}

/** All playback preparation, media transport, and non-UI writes are denied. */
export function classifyReferenceRequest(raw, method, resourceType = '') {
  try {
    if (/^(?:data|blob):/.test(raw)) return { allow: true, kind: 'non_network' };
    const selected = new URL(raw), selectedOrigin = selected.protocol === 'ws:' ? `http://${selected.host}` : selected.origin;
    if (selectedOrigin !== REFERENCE_ORIGIN) return { allow: false, kind: 'external', route: null };
    const { url, route } = normalizedURL(raw, true);
    guard(['GET', 'HEAD', 'OPTIONS', 'POST'].includes(method));
    if (resourceType === 'media' || /^\/(?:Videos|Audio)\//i.test(route) ||
      /\/(?:Playing|PlaybackInfo)(?:\/|$)/i.test(route)) return { allow: false, kind: 'playback', route };
    for (const [name, value] of url.searchParams) {
      if (name.toLowerCase() === 'userid' && value !== REFERENCE_VIEWER) return { allow: false, kind: 'foreign_user', route };
    }
    const user = /^\/Users\/([^/]+)(?:\/|$)/i.exec(route)?.[1];
    if (user && !['public', 'authenticatebyname'].includes(user.toLowerCase()) && user !== REFERENCE_VIEWER)
      return { allow: false, kind: 'foreign_user', route };
    if (url.protocol === 'ws:') return { allow: method === 'GET' && WS_PATHS.has(url.pathname.toLowerCase()), kind: 'websocket', route };
    if (['GET', 'HEAD', 'OPTIONS'].includes(method)) return { allow: true, kind: 'read', route };
    if (/^\/Users\/AuthenticateByName\/?$/i.test(route)) return { allow: true, kind: 'login', route };
    if (/^\/Sessions\/Capabilities(?:\/Full)?\/?$/i.test(route)) return { allow: true, kind: 'capabilities', route };
    if (/^\/Sessions\/Logout\/?$/i.test(route)) return { allow: true, kind: 'logout', route };
    return { allow: false, kind: 'state_mutation', route };
  } catch { return { allow: false, kind: 'invalid_request', route: null }; }
}

/** A known bootstrap attempt is still aborted; only its narrow rejection is non-fatal. */
export function referenceBootstrapExternalRequest(raw, method, resourceType, metadata, scope, secrets = []) {
  guard(record(metadata) && record(scope) && scope.loginIntent === true && scope.loginCount === 1 && scope.bootstrapOpen === true &&
    scope.sourceworker === false && scope.cleanup === false && scope.poisoned === false && scope.identityLost === false &&
    ['manual_login', 'authenticated'].includes(scope.phase) && Number.isSafeInteger(scope.seen) && scope.seen >= 0 && scope.seen < 3,
  'reference_bootstrap_external_scope_rejected');
  guard(typeof raw === 'string' && Buffer.byteLength(raw) <= L.headerBytes && method === 'POST' && resourceType === 'fetch');
  const url = new URL(raw), entries = [...url.searchParams];
  guard(url.origin === BOOTSTRAP_EXTERNAL_ORIGIN && url.pathname === BOOTSTRAP_EXTERNAL_ROUTE && url.href === raw &&
    !url.username && !url.password && !url.hash && entries.length === BOOTSTRAP_EXTERNAL_KEYS.length &&
    new Set(entries.map(([key]) => key)).size === entries.length && entries.every(([key]) => BOOTSTRAP_EXTERNAL_KEYS.includes(key)),
  'reference_bootstrap_external_shape_rejected');
  const known = { serverId: REFERENCE_SERVER, deviceId: metadata.device_id, deviceName: metadata.device_name,
    appName: metadata.client_name, appVersion: metadata.client_version };
  for (const [key, value] of Object.entries(known)) guard(typeof value === 'string' && value.length > 0 && value.length <= 256 &&
    url.searchParams.get(key) === value, 'reference_bootstrap_external_metadata_mismatch');
  const viewOnly = url.searchParams.get('viewOnly');
  guard(typeof viewOnly === 'string' && Buffer.byteLength(viewOnly) <= 128 && !/[\x00-\x1f\x7f]/.test(viewOnly),
    'reference_bootstrap_external_view_only_limit');
  const evidence = { origin: url.origin, route: url.pathname, method, resource_type: resourceType,
    query_field_names: entries.map(([key]) => key), request_sha256: sha(method + '\n' + raw),
    known_binding: { server_id: REFERENCE_SERVER, device_id: metadata.device_id, device_name: metadata.device_name,
      client_name: metadata.client_name, client_version: metadata.client_version }, view_only: viewOnly };
  return assertPublicJSON(evidence, secrets);
}

/** Cleanup admits only UI reads and the separately reserved one-time logout. */
export function referenceTrafficAllowed({ poisoned, cleanup, identityLost, closed }, kind) {
  if (closed || identityLost) return false;
  if (cleanup) return ['read', 'logout', 'non_network'].includes(kind);
  return !poisoned && ['read', 'login', 'capabilities', 'logout', 'websocket', 'non_network'].includes(kind);
}

export function referenceHTTPBudgetAvailable(state, cleanup, cleanupState, kind = 'read') {
  if (cleanup && kind === 'logout') return state.logout === 0 && cleanupState.active <= L.cleanupConcurrent;
  return cleanup
    ? cleanupState.seen <= L.cleanupRequests && cleanupState.active < L.cleanupConcurrent &&
      cleanupState.request_bytes <= L.cleanupRequestBytes && cleanupState.response_bytes <= L.cleanupResponseBytes
    : state.seen <= L.requests && state.active < L.concurrent && state.request_bytes <= 4194304 && state.response_bytes <= L.totalResponseBytes;
}

function normalizeEncoding(value) {
  for (let count = 0; count < 4; count += 1) {
    const before = value;
    value = value.replace(/\\u([0-9a-f]{4})|\\x([0-9a-f]{2})|%([0-9a-f]{2})/gi,
      (_match, unicode, byte, percent) => String.fromCharCode(parseInt(unicode ?? byte ?? percent, 16)))
      .replace(/&#(?:x([0-9a-f]{1,6})|([0-9]{1,7}));?/gi, (_match, hex, decimal) => {
        const point = parseInt(hex ?? decimal, hex ? 16 : 10);
        return point <= 0x10ffff ? String.fromCodePoint(point) : ' ';
      }).replace(/\\\//g, '/');
    if (before === value) break;
  }
  return value;
}

function secretVariants(secrets) {
  const values = new Set();
  guard(Array.isArray(secrets) && secrets.length <= 128);
  for (const secret of secrets) {
    if (!secret) continue;
    guard(typeof secret === 'string' && secret.length <= 4096);
    const bytes = Buffer.from(secret);
    for (const value of [secret, normalizeEncoding(secret), JSON.stringify(secret).slice(1, -1), bytes.toString('hex'),
      bytes.toString('base64'), bytes.toString('base64').replace(/=+$/, ''), bytes.toString('base64url'),
      [...bytes].map(byte => `%${byte.toString(16).padStart(2, '0')}`).join('')])
      if (value) values.add(value);
  }
  return [...values].sort((left, right) => right.length - left.length);
}

/** Diagnostic errors are bounded and credential variants are never emitted. */
export function sanitizeDiagnostic(value, secrets = []) {
  if (typeof value !== 'string' || value.length > 8192) return '[diagnostic omitted]';
  let result = normalizeEncoding(value);
  for (const secret of secretVariants(secrets)) result = result.replace(new RegExp(secret.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'gi'), '[secret]');
  return result.replace(/\b(?:https?|wss?|file|blob|data):\S*/gi, '[url]')
    .replace(/\b(?:password|pw|token|access_token|api_key)\s*[:=]\s*[^\s,;]+/gi, '[credential]').slice(0, 512);
}

function hasSecret(value, secrets) {
  const text = typeof value === 'string' ? value : JSON.stringify(value);
  guard(typeof text === 'string' && Buffer.byteLength(text) <= L.captureBytes);
  const normalized = normalizeEncoding(text).toLowerCase(), original = text.toLowerCase();
  return secretVariants(secrets).some(secret => original.includes(secret.toLowerCase()) || normalized.includes(secret.toLowerCase()));
}

function assertPublicJSON(value, secrets) {
  guard(!hasSecret(value, secrets), 'reference_message_contains_secret');
  let count = 0;
  function visit(current, depth) {
    guard(depth <= 24 && ++count <= 16384, 'reference_public_json_limit');
    if (Array.isArray(current)) current.forEach(item => visit(item, depth + 1));
    else if (record(current)) for (const [key, item] of Object.entries(current)) {
      guard(!SECRET_KEYS.has(key.toLowerCase()) || item === null || item === '', 'reference_message_contains_credential_field');
      visit(item, depth + 1);
    }
  }
  visit(value, 0);
  return value;
}

function parseHeaders(raw, { response = false, websocket = false } = {}) {
  guard(Array.isArray(raw) && raw.length % 2 === 0 && raw.length <= 256);
  const values = new Map(), forwarded = [];
  let bytes = 0;
  for (let index = 0; index < raw.length; index += 2) {
    const name = raw[index], value = raw[index + 1], lower = name?.toLowerCase();
    guard(typeof name === 'string' && /^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,128}$/.test(name) &&
      typeof value === 'string' && !/[\x00-\x1f\x7f-\uffff]/.test(value));
    bytes += Buffer.byteLength(name) + Buffer.byteLength(value) + 4;
    guard(bytes <= L.headerBytes && (!values.has(lower) || response && !['content-length', 'transfer-encoding', 'connection', 'host'].includes(lower)));
    values.set(lower, [...(values.get(lower) ?? []), value]);
    if (!HOP_HEADERS.has(lower) || websocket && ['connection', 'upgrade'].includes(lower)) forwarded.push(name, value);
  }
  for (const name of ['connection', 'proxy-connection']) for (const value of values.get(name) ?? [])
    guard(value.split(',').every(part => (websocket ? ['upgrade', 'close', 'keep-alive'] : ['close', 'keep-alive']).includes(part.trim().toLowerCase())));
  guard(!values.has('trailer') && !values.has('proxy-authorization') && (websocket || !values.has('upgrade')));
  const length = values.get('content-length')?.[0], transfer = values.get('transfer-encoding')?.[0];
  guard(length === undefined || /^(?:0|[1-9][0-9]{0,9})$/.test(length));
  guard(transfer === undefined || response && transfer.toLowerCase() === 'chunked' && length === undefined);
  return { values, forwarded, length: length === undefined ? null : Number(length), first: name => values.get(name)?.[0] };
}

function authorizationFields(value) {
  guard(/^(?:Emby|MediaBrowser)\s+/i.test(value), 'reference_authorization_scheme');
  const tail = value.replace(/^(?:Emby|MediaBrowser)\s+/i, '');
  let position = 0;
  const names = new Set(), fields = {};
  while (position < tail.length) {
    const field = /^([A-Za-z][A-Za-z0-9_-]{0,63})\s*=\s*(?:"((?:[^"\\]|\\.)*)"|([^,\s]+))\s*(?:,\s*|$)/.exec(tail.slice(position));
    guard(field && !names.has(field[1].toLowerCase()), 'reference_authorization_fields');
    names.add(field[1].toLowerCase()); position += field[0].length;
    const content = field[2] === undefined ? field[3] : field[2].replace(/\\(.)/g, '$1');
    fields[field[1].toLowerCase()] = content;
  }
  return fields;
}

function embeddedToken(value) { return authorizationFields(value).token ?? null; }

/** Metadata comes from the real login request, never from injected browser state. */
export function referenceClientMetadata(raw, headers) {
  const { url } = normalizedURL(raw, true), values = new Map();
  const fields = { client: 'client_name', device: 'device_name', deviceid: 'device_id', version: 'client_version',
    'x-emby-client': 'client_name', 'x-emby-device-name': 'device_name', 'x-emby-device-id': 'device_id',
    'x-emby-client-version': 'client_version' };
  function add(key, value) {
    const name = fields[key.toLowerCase()]; if (!name) return;
    guard(typeof value === 'string' && value.length > 0 && value.length <= 256 && !/[\x00-\x1f\x7f]/.test(value));
    guard(!values.has(name) || values.get(name) === value, 'reference_conflicting_client_metadata'); values.set(name, value);
  }
  for (const [key, value] of url.searchParams) add(key, value);
  for (const [key, value] of Object.entries(headers)) {
    if (['authorization', 'x-emby-authorization'].includes(key.toLowerCase()))
      for (const [name, content] of Object.entries(authorizationFields(value))) add(name, content);
    else add(key, value);
  }
  guard(['client_name', 'device_name', 'device_id', 'client_version'].every(key => values.has(key)), 'reference_client_metadata_missing');
  return Object.fromEntries(values);
}

/** The returned credential is private transport state and must never be reported. */
export function referenceAuthority(raw, headers, expected = undefined) {
  const { url } = normalizedURL(raw, true), candidates = [];
  for (const [key, value] of url.searchParams) if (TOKEN_KEYS.has(key.toLowerCase())) candidates.push(value);
  for (const [key, value] of Object.entries(headers)) {
    const lower = key.toLowerCase();
    if (TOKEN_KEYS.has(lower)) candidates.push(value);
    if (['authorization', 'x-emby-authorization'].includes(lower)) {
      const token = embeddedToken(value); if (token !== null) candidates.push(token);
    }
  }
  if (!candidates.length) return null;
  guard(candidates.every(value => typeof value === 'string' && value.length > 0 && value.length <= 4096 &&
    !/[\s,"\\\x00-\x1f\x7f-\uffff]/.test(value) && value === candidates[0]), 'reference_conflicting_authority');
  guard(expected === undefined || candidates[0] === expected, 'reference_foreign_authority');
  return { token: candidates[0], token_sha256: sha(candidates[0]) };
}

function captureRoute(route, query) {
  const own = `/Users/${REFERENCE_VIEWER}`;
  if ([`${own}/Views`, '/Views', ...['93', '96', '100'].flatMap(id => [`${own}/Items/${id}`, `/Items/${id}`])].includes(route)) return true;
  if (![`${own}/Items`, '/Items'].includes(route)) return false;
  const entries = Object.entries(query).map(([key, value]) => [key.toLowerCase(), value]);
  const parent = entries.filter(([key]) => key === 'parentid').map(([, value]) => value);
  const ids = entries.filter(([key]) => key === 'ids').map(([, value]) => value);
  return parent.length === 1 && parent[0] === '93' || ids.length === 1 && ['93', '96', '100'].includes(ids[0]);
}

/** Request digests bind the original URL while public shapes omit authority. */
export function referenceRequestRecord(raw, method, headers = {}, secrets = []) {
  const { url, route } = normalizedURL(raw, true), query = {}, hidden = [];
  const catalog = /^\/(?:Items(?:\/(?:93|96|100))?|Views)\/?$/.test(route) ||
    new RegExp(`^/Users/${REFERENCE_VIEWER}/(?:Items(?:/(?:93|96|100))?|Views)/?$`).test(route);
  guard(!hasSecret(route, secrets), 'reference_route_contains_secret');
  let count = 0;
  for (const [key, value] of url.searchParams) {
    guard(++count <= 128 && key.length <= 128 && value.length <= 4096, 'reference_query_limit');
    if (SECRET_KEYS.has(key.toLowerCase())) continue;
    guard(!hasSecret(key, secrets), 'reference_query_name_contains_secret');
    if (catalog && PUBLIC_QUERY.has(key.toLowerCase())) {
      guard(!Object.hasOwn(query, key) && !hasSecret(value, secrets), 'reference_public_query_rejected');
      query[key] = value;
    } else hidden.push({ key, value_sha256: sha(value) });
  }
  const authority = referenceAuthority(raw, headers);
  const sorted = Object.entries(query).sort(([left], [right]) => left < right ? -1 : left > right ? 1 : 0);
  hidden.sort((left, right) => left.key < right.key ? -1 : left.key > right.key ? 1 :
    left.value_sha256 < right.value_sha256 ? -1 : left.value_sha256 > right.value_sha256 ? 1 : 0);
  return { method: safeMethod(method), kind: classifyReferenceRequest(raw, method).kind, route, query, hidden_query: hidden,
    shape_sha256: sha(route + '\n' + JSON.stringify(sorted) + '\n' + JSON.stringify(hidden)), request_sha256: sha(method + '\n' + raw),
    token_sha256: authority?.token_sha256 ?? null, capture: captureRoute(route, query) };
}

/** Only public item identity fields are retained; response entity bytes stay intact. */
function decodedJSONBytes(bytes, encoding = null) {
  guard(Buffer.isBuffer(bytes) && bytes.length <= L.captureBytes, 'reference_projection_limit');
  const normalized = encoding?.trim().toLowerCase() ?? 'identity';
  const decode = { gzip: gunzipSync, deflate: inflateSync, br: brotliDecompressSync }[normalized];
  if (normalized === 'identity') return bytes;
  guard(decode, 'reference_json_encoding_rejected');
  return decode(bytes, { maxOutputLength: L.captureBytes });
}

export function referenceResponseProjection(bytes, secrets = [], encoding = null) {
  guard(Buffer.isBuffer(bytes) && bytes.length <= L.captureBytes, 'reference_projection_limit');
  const decoded = decodedJSONBytes(bytes, encoding);
  const json = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(decoded));
  function item(value) {
    guard(record(value), 'reference_item_projection_shape');
    const projected = {};
    for (const key of ['Id', 'Name', 'Type', 'IsFolder', 'ParentId']) if (Object.hasOwn(value, key)) {
      guard(['string', 'boolean', 'number'].includes(typeof value[key]) || value[key] === null, 'reference_item_projection_field');
      projected[key] = value[key];
    }
    return assertPublicJSON(projected, secrets);
  }
  let projected;
  if (Array.isArray(json)) { guard(json.length <= 10000); projected = json.map(item); }
  else if (record(json) && Array.isArray(json.Items)) {
    guard(json.Items.length <= 10000);
    projected = { Items: json.Items.map(item) };
    for (const key of ['TotalRecordCount', 'StartIndex']) if (Object.hasOwn(json, key)) {
      guard(Number.isSafeInteger(json[key]) && json[key] >= 0); projected[key] = json[key];
    }
  } else projected = item(json);
  return { body_sha256: sha(bytes), bytes: bytes.length, decoded_bytes: decoded.length,
    content_encoding: encoding ?? 'identity', json: projected, projection: 'public_item_identity_fields' };
}

export function referenceHTTPPlan(raw, method, rawHeaders) {
  const { url, route } = normalizedURL(raw), classification = classifyReferenceRequest(raw, method);
  guard(classification.allow && classification.kind !== 'non_network');
  const headers = parseHeaders(rawHeaders);
  guard(headers.first('host') === url.host && !headers.values.has('expect') && !headers.values.has('transfer-encoding') &&
    !['audio', 'video'].includes(headers.first('sec-fetch-dest')?.toLowerCase()));
  const length = headers.length ?? 0;
  guard(length <= L.requestBytes && (method === 'POST' || length === 0) && (method !== 'POST' || headers.length !== null));
  return { method, kind: classification.kind, route, path: raw.slice(REFERENCE_ORIGIN.length), length,
    headers: [...headers.forwarded, 'Connection', 'close'], authorityHeaders: Object.fromEntries([...headers.values].map(([name, values]) => [name, values[0]])) };
}

export function referenceHTTPResponsePlan(status, rawHeaders, method) {
  guard(Number.isInteger(status) && status >= 200 && status <= 599);
  const headers = parseHeaders(rawHeaders, { response: true }), noBody = method === 'HEAD' || status === 204 || status === 304;
  guard(noBody || headers.length === null || headers.length <= L.responseBytes);
  return { status, noBody, length: headers.length, headers: [...headers.forwarded, 'Connection', 'close'],
    contentType: safeContentType(headers.first('content-type')),
    contentEncoding: headers.first('content-encoding') ?? null };
}

function physicalHTTPError(code) {
  const error = new Error('reference_physical_http_failure');
  error.code = REFERENCE_HTTP_ERROR_CODES.includes(code) ? code : 'incomplete_exchange';
  return error;
}

function physicalHTTPErrorCode(error, phase, reason = null) {
  if (REFERENCE_HTTP_ERROR_CODES.includes(error?.code)) return error.code;
  const transport = { http_timeout: 'http_timeout', http_request_error: 'request_transport_error',
    http_request_aborted: 'request_aborted', http_response_error: 'downstream_transport_error',
    http_downstream_closed: 'downstream_closed', http_upstream_idle: 'upstream_idle_timeout',
    http_upstream_error: 'upstream_transport_error', http_unexpected_upgrade: 'unexpected_upgrade', http_incomplete: 'incomplete_exchange' };
  if (reason && Object.hasOwn(transport, reason)) return transport[reason];
  return ({ admission: 'admission_rejected', request_body: 'request_body_rejected', login_form: 'login_form_encoding_rejected',
    client_metadata: 'client_metadata_rejected', pin: 'pin_rejected', upstream_create: 'upstream_create_failed',
    upstream_setup: 'upstream_setup_failed', upstream_end: 'upstream_end_failed', upstream_response: 'upstream_response_rejected',
    response_body: 'response_body_rejected', downstream_write: 'downstream_write_failed' })[phase] ?? 'incomplete_exchange';
}

function physicalHTTPPhase(evidence, phase) {
  const current = REFERENCE_HTTP_PHASES.indexOf(evidence.transport_phase), next = REFERENCE_HTTP_PHASES.indexOf(phase);
  guard(next >= 0);
  if (next >= current) evidence.transport_phase = phase;
}

export function referencePhysicalHTTPFacts() {
  return { transport_phase: 'admission', error_code: null, request_content_type: null, request_body_sha256: null,
    upstream_create_attempted: false, upstream_created: false, upstream_end_attempted: false,
    upstream_end_returned: false, upstream_response_received: false };
}

function loginFormContentType(value) {
  return typeof value === 'string' && /^application\/x-www-form-urlencoded; *charset=UTF-8$/i.test(value.trim());
}

function requestContentType(value) {
  return loginFormContentType(value) ? LOGIN_FORM_CONTENT_TYPE : safeContentType(value);
}

/** Validate only the observed explicit form contract; credentials never leave this function. */
export function validateReferenceLoginForm(contentType, bytes, account) {
  if (!loginFormContentType(contentType)) throw physicalHTTPError('login_content_type_rejected');
  if (!Buffer.isBuffer(bytes) || bytes.length === 0 || bytes.length > L.requestBytes) throw physicalHTTPError('login_form_encoding_rejected');
  let text;
  try { text = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(bytes); }
  catch { throw physicalHTTPError('login_form_encoding_rejected'); }
  if (!/^[\x20-\x7e]+$/.test(text)) throw physicalHTTPError('login_form_encoding_rejected');
  const values = new Map();
  for (const field of text.split('&')) {
    const split = field.indexOf('=');
    if (split < 1) throw physicalHTTPError('login_form_encoding_rejected');
    let name, value;
    try {
      name = decodeURIComponent(field.slice(0, split).replace(/\+/g, ' '));
      value = decodeURIComponent(field.slice(split + 1).replace(/\+/g, ' '));
    } catch { throw physicalHTTPError('login_form_encoding_rejected'); }
    if (!['Username', 'Pw'].includes(name) || values.has(name)) throw physicalHTTPError('login_form_fields_rejected');
    values.set(name, value);
  }
  if (values.size !== 2 || !values.has('Username') || !values.has('Pw')) throw physicalHTTPError('login_form_fields_rejected');
  if (typeof account?.username !== 'string' || values.get('Username') !== account.username) throw physicalHTTPError('login_username_mismatch');
  if (typeof account?.password !== 'string' || values.get('Pw') !== account.password) throw physicalHTTPError('login_password_mismatch');
  return Object.freeze({ content_type: LOGIN_FORM_CONTENT_TYPE, body_sha256: sha(bytes), bytes: bytes.length });
}

/** The production dispatcher validates first, then passes the exact original headers and body to transport. */
export async function dispatchReferenceHTTP({ plan, bytes, account, pin, canDispatch, evidence, onLoginMetadata, onCreated,
  onResponse, onError, onIdle, onUpgrade }, transport = http) {
  try {
    evidence.request_content_type = requestContentType(plan.authorityHeaders['content-type']);
    evidence.request_body_sha256 = sha(bytes);
    if (plan.kind === 'login') {
      physicalHTTPPhase(evidence, 'login_form');
      validateReferenceLoginForm(plan.authorityHeaders['content-type'], bytes, account);
      physicalHTTPPhase(evidence, 'client_metadata');
      onLoginMetadata(referenceClientMetadata(REFERENCE_ORIGIN + plan.path, plan.authorityHeaders));
    }
    if (plan.kind !== 'read') { physicalHTTPPhase(evidence, 'pin'); await pin(); }
    if (!canDispatch()) throw physicalHTTPError(plan.kind === 'read' ? 'request_body_rejected' : 'pin_rejected');
    physicalHTTPPhase(evidence, 'upstream_create'); evidence.upstream_create_attempted = true;
    const outgoing = transport.request({ hostname: '127.0.0.1', port: 18197, method: plan.method, path: plan.path,
      headers: plan.headers, agent: false, maxHeaderSize: L.headerBytes }, response => {
      evidence.upstream_response_received = true; physicalHTTPPhase(evidence, 'upstream_response'); onResponse(response);
    });
    evidence.upstream_created = true; physicalHTTPPhase(evidence, 'upstream_setup'); onCreated(outgoing);
    outgoing.setTimeout(L.idleMs, onIdle); outgoing.on('error', onError); outgoing.on('upgrade', onUpgrade);
    physicalHTTPPhase(evidence, 'upstream_end'); evidence.upstream_end_attempted = true;
    outgoing.end(bytes); evidence.upstream_end_returned = true;
    return outgoing;
  } catch (error) {
    throw physicalHTTPError(physicalHTTPErrorCode(error, evidence.transport_phase));
  }
}

export function referenceWebSocketPlan(raw, method, rawHeaders) {
  const { url } = normalizedURL(raw, true), headers = parseHeaders(rawHeaders, { websocket: true });
  guard(classifyReferenceRequest(`ws://${url.host}${url.pathname}${url.search}`, method).allow);
  guard(method === 'GET' && WS_PATHS.has(url.pathname.toLowerCase()) && headers.first('host') === url.host &&
    headers.first('origin') === REFERENCE_ORIGIN && headers.first('upgrade')?.toLowerCase() === 'websocket' &&
    headers.first('connection')?.toLowerCase().split(',').map(value => value.trim()).includes('upgrade') &&
    headers.first('sec-websocket-version') === '13' && !headers.values.has('expect') &&
    !headers.values.has('transfer-encoding') && (headers.length === null || headers.length === 0));
  const key = headers.first('sec-websocket-key');
  guard(typeof key === 'string' && /^[A-Za-z0-9+/]{22}==$/.test(key) && Buffer.from(key, 'base64').length === 16);
  const protocols = (headers.first('sec-websocket-protocol') ?? '').split(',').map(value => value.trim()).filter(Boolean);
  guard(protocols.length <= 8 && protocols.every(value => /^[A-Za-z0-9._-]{1,64}$/.test(value)));
  const authorityHeaders = Object.fromEntries([...headers.values].map(([name, values]) => [name, values[0]]));
  return { path: url.pathname + url.search, key, protocols, headers: headers.forwarded, authorityHeaders };
}

export function referenceWebSocketResponsePlan(status, rawHeaders, plan) {
  const headers = parseHeaders(rawHeaders, { response: true, websocket: true });
  for (const name of ['upgrade', 'sec-websocket-accept', 'sec-websocket-protocol']) guard((headers.values.get(name)?.length ?? 0) <= 1);
  guard(status === 101 && headers.first('upgrade')?.toLowerCase() === 'websocket' &&
    headers.first('connection')?.toLowerCase().split(',').map(value => value.trim()).includes('upgrade') &&
    headers.first('sec-websocket-accept') === createHash('sha1').update(plan.key + WS_GUID).digest('base64') &&
    !headers.values.has('sec-websocket-extensions') && !headers.values.has('transfer-encoding') && headers.length === null &&
    (!headers.first('sec-websocket-protocol') || plan.protocols.includes(headers.first('sec-websocket-protocol'))));
  return headers.forwarded;
}

/** Plaintext CONNECT admits exactly one fixed-target WebSocket handshake. */
export function referenceConnectInner(bytes, onCompleteHead = undefined, secrets = []) {
  guard(onCompleteHead === undefined || typeof onCompleteHead === 'function');
  guard(Buffer.isBuffer(bytes) && bytes.length <= L.headerBytes + L.queuedBytes);
  const boundary = bytes.indexOf('\r\n\r\n');
  if (boundary < 0) { guard(bytes.length <= L.headerBytes); return null; }
  guard(boundary <= L.headerBytes);
  const lines = bytes.subarray(0, boundary).toString('latin1').split('\r\n'), requestLine = lines.shift();
  const observed = /^(\S+) (\S+) HTTP\/\S+$/.exec(requestLine);
  const observedMethod = observed?.[1] ?? 'OTHER';
  const observedRaw = observed ? observed[2].startsWith('/') ? REFERENCE_ORIGIN + observed[2] : observed[2] : null;
  let observedRoute = null;
  if (observedRaw) try {
    const value = normalizedURL(observedRaw, true);
    if (!hasSecret(value.route, secrets)) observedRoute = value.route;
  } catch { /* A malformed target contributes only its original request digest. */ }
  if (onCompleteHead) onCompleteHead(Object.freeze({ method: safeMethod(observedMethod), route: observedRoute,
    request_sha256: observedRaw === null ? sha(bytes.subarray(0, boundary + 4)) : sha(observedMethod + '\n' + observedRaw) }));
  const request = /^GET (\/\S*) HTTP\/1\.1$/.exec(requestLine);
  guard(request && !request[1].startsWith('//'));
  const rawHeaders = [];
  for (const line of lines) { const field = /^([^:\s]+):[ \t]*([^\r\n]*)$/.exec(line); guard(field); rawHeaders.push(field[1], field[2]); }
  const raw = REFERENCE_ORIGIN + request[1];
  referenceWebSocketPlan(raw, 'GET', rawHeaders);
  return { raw, method: 'GET', rawHeaders, head: Buffer.from(bytes.subarray(boundary + 4)) };
}

export function createReferenceFrameState(direction) {
  guard(['client', 'server'].includes(direction));
  return { direction, pending: Buffer.alloc(0), held: [], parts: [], opcode: null, messageBytes: 0,
    wireBytes: 0, frames: 0, messages: 0, windowAt: null, windowFrames: 0, closed: false };
}

function messageJSON(bytes) {
  const text = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(bytes);
  let json = null, parsed = false;
  try { json = JSON.parse(text); parsed = true; } catch { /* Non-JSON protocol text remains observable as text. */ }
  const types = record(json) ? Object.entries(json).filter(([key]) => key.toLowerCase() === 'messagetype').map(([, value]) => value) : [];
  guard(types.length <= 1, 'reference_ambiguous_message_type');
  const type = types[0];
  guard(!(typeof type === 'string' && FORBIDDEN_MESSAGES.has(type.trim().toLowerCase())) &&
    !FORBIDDEN_MESSAGES.has(text.trim().toLowerCase()), 'reference_websocket_control_denied');
  return { text, parsed, json, type };
}

/** Only observed read-only subscription messages have an application-level contract. */
export function requireReferenceClientMessage(decoded) {
  guard(decoded.parsed === true && record(decoded.json) &&
    (decoded.json.MessageType === 'SessionsStart' && decoded.json.Data === '1000,1000' ||
      decoded.json.MessageType === 'SessionsStop' && decoded.json.Data === ''), 'reference_client_message_not_allowlisted');
}

/** Complete data messages are admitted before their original wire frames are released. */
export function decodeWebSocketFrames(state, chunk, now = Date.now(), onMessage = () => {}) {
  guard(Buffer.isBuffer(chunk) && Number.isFinite(now) && state.wireBytes + chunk.length <= L.wireBytes);
  state.wireBytes += chunk.length;
  state.pending = Buffer.concat([state.pending, chunk]);
  guard(state.pending.length <= L.queuedBytes);
  const output = [];
  while (state.pending.length >= 2) {
    const first = state.pending[0], second = state.pending[1], final = Boolean(first & 128), opcode = first & 15;
    const masked = Boolean(second & 128); let length = second & 127, offset = 2;
    guard((first & 112) === 0 && [0, 1, 8, 9, 10].includes(opcode) && masked === (state.direction === 'client') && !state.closed);
    if (length === 126) { if (state.pending.length < 4) break; length = state.pending.readUInt16BE(2); offset = 4; guard(length >= 126); }
    else if (length === 127) {
      if (state.pending.length < 10) break;
      const extended = state.pending.readBigUInt64BE(2); guard(extended >= 65536n && extended <= BigInt(L.messageBytes));
      length = Number(extended); offset = 10;
    }
    guard(length <= L.messageBytes && (opcode < 8 || final && length <= 125 && (opcode !== 8 || length !== 1)));
    const maskOffset = offset; if (masked) offset += 4;
    if (state.pending.length < offset + length) break;
    if (state.windowAt === null || now - state.windowAt >= 1000) { state.windowAt = now; state.windowFrames = 0; }
    guard(++state.frames <= L.frames && ++state.windowFrames <= L.framesPerSecond);
    const wire = Buffer.from(state.pending.subarray(0, offset + length)), payload = Buffer.from(state.pending.subarray(offset, offset + length));
    state.pending = Buffer.from(state.pending.subarray(offset + length));
    if (masked) for (let index = 0; index < payload.length; index += 1) payload[index] ^= wire[maskOffset + index % 4];
    state.held.push(wire);
    if (opcode < 8) {
      guard(opcode === 0 ? state.opcode !== null : state.opcode === null);
      if (opcode !== 0) state.opcode = opcode;
      state.messageBytes += payload.length; guard(state.messageBytes <= L.messageBytes);
      state.parts.push(payload);
      if (!final) continue;
      const bytes = Buffer.concat(state.parts);
      const decoded = messageJSON(bytes);
      if (state.direction === 'client') requireReferenceClientMessage(decoded);
      state.messages += 1;
      onMessage({ index: state.messages, direction: state.direction, frame_number: state.frames, bytes, ...decoded });
      state.parts = []; state.messageBytes = 0; state.opcode = null;
    } else if (opcode === 8) {
      if (payload.length >= 2) {
        const code = payload.readUInt16BE(0);
        guard(code >= 1000 && code <= 4999 && ![1004, 1005, 1006, 1015].includes(code) && (code <= 1014 || code >= 3000));
        new TextDecoder('utf-8', { fatal: true }).decode(payload.subarray(2));
      }
      guard(state.opcode === null, 'reference_close_inside_fragment'); state.closed = true;
    }
    if (state.opcode === null) { output.push(...state.held); state.held = []; }
  }
  return output;
}

export function referenceSocketMessageRecord(message, secrets = []) {
  guard(!hasSecret(message.text, secrets), 'reference_message_contains_secret');
  const evidence = { index: message.index, direction: message.direction, frame_number: message.frame_number ?? null,
    body_sha256: sha(message.bytes), bytes: message.bytes.length, original_message: true, payload_retained: true };
  if (message.direction === 'client') requireReferenceClientMessage(message);
  if (message.parsed) {
    Object.assign(evidence, { json: assertPublicJSON(message.json, secrets), json_text: message.text,
      original_message: true, payload_retained: true });
    if (record(message.json)) for (const key of ['MessageType', 'MessageId', 'Data'])
      if (Object.hasOwn(message.json, key)) evidence[key] = message.json[key];
  } else evidence.text = message.text;
  return evidence;
}

function publicRead(route, method) {
  return method === 'GET' && route === '/Branding/Css.css' || route === '/' || /^\/web(?:\/|$)/i.test(route) ||
    /^\/(?:System\/(?:Info\/Public|Ping)|Users\/Public|Branding\/(?:Css|Configuration))\/?$/i.test(route);
}

/** Apply the same authority guard used by the actual context request observer. */
export function referenceObservedRequestAuthority(raw, method, headers, expectedToken, evidence) {
  const actual = referenceAuthority(raw, headers);
  guard(!actual || expectedToken !== null && actual.token === expectedToken, 'reference_foreign_authority');
  const required = evidence.kind !== 'login' && !publicRead(evidence.route ?? '', method);
  guard(!required || actual !== null, 'reference_missing_authority');
  return actual;
}

/** Associate asynchronous failures without retaining exception text or stack traces. */
export function referenceObservationFailure(error, association) {
  const channels = ['context_request', 'context_response', 'context_finished', 'context_failed', 'physical_http',
    'physical_websocket_setup', 'physical_websocket_write', 'login_private_capture'];
  const reasons = new Map([
    ['reference_missing_authority', 'missing_authority'], ['reference_foreign_authority', 'foreign_authority'],
    ['reference_conflicting_authority', 'conflicting_authority'], ['reference_authorization_scheme', 'authorization_scheme_rejected'],
    ['reference_authorization_fields', 'authorization_fields_rejected'], ['reference_operation_timeout', 'operation_timeout'],
    ['reference_bootstrap_abort_unobserved', 'bootstrap_abort_unobserved'], ['reference_bootstrap_abort_failed', 'bootstrap_abort_failed'],
    ['reference_client_metadata_missing', 'client_metadata_missing'], ['reference_conflicting_client_metadata', 'client_metadata_conflict'],
    ['reference_runtime_guard_rejected', 'guard_rejected'],
  ]);
  return { request_id: typeof association?.request_id === 'string' && /^(?:physical|frame|service_worker|handshake)-[0-9]{1,5}$/.test(association.request_id)
    ? association.request_id : null, channel: channels.includes(association?.channel) ? association.channel : 'runtime_async',
    observation_reason: reasons.get(error?.message) ?? (error?.name === 'TypeError' ? 'observation_api_error' :
      error?.name === 'TimeoutError' ? 'observation_timeout' : 'async_operation_failed') };
}

/** Delay proof capture until a genuine frame logout response has status 204. */
export function createReferenceLogoutProofContext(context, onError, authorize = undefined) {
  guard(context && typeof context.pages === 'function' && typeof context.on === 'function' && typeof context.off === 'function' &&
    typeof onError === 'function' && context.pages().length === 0 && (authorize === undefined || typeof authorize === 'function'));
  const listeners = new Map(['request', 'response', 'requestfinished', 'requestfailed'].map(name => [name, new Set()]));
  const accepted = new WeakSet(), completedRequests = new WeakSet(), failedRequests = new WeakSet(), pending = new Set();
  let admittedRequest = null;
  const response = value => {
    try {
      const request = value.request();
      if (request.serviceWorker() !== null || value.status() !== 204 ||
        classifyReferenceRequest(request.url(), request.method()).kind !== 'logout') return;
      if (admittedRequest === request) return;
      guard(admittedRequest === null, 'reference_duplicate_logout_proof'); admittedRequest = request;
      const deliver = () => {
        accepted.add(request);
        for (const listener of listeners.get('request')) listener(request);
        for (const listener of listeners.get('response')) listener(value);
        if (completedRequests.has(request)) for (const listener of listeners.get('requestfinished')) listener(request);
        if (failedRequests.has(request)) for (const listener of listeners.get('requestfailed')) listener(request);
      };
      if (authorize) {
        const operation = Promise.resolve().then(() => authorize(request, value)).then(result => { guard(result === true); deliver(); }).catch(onError);
        pending.add(operation); operation.finally(() => pending.delete(operation));
      } else deliver();
    } catch { onError(); }
  };
  const finished = request => { completedRequests.add(request); if (accepted.has(request)) for (const listener of listeners.get('requestfinished')) listener(request); };
  const failed = request => { failedRequests.add(request); if (accepted.has(request)) for (const listener of listeners.get('requestfailed')) listener(request); };
  context.on('response', response); context.on('requestfinished', finished); context.on('requestfailed', failed);
  return Object.freeze({ pages: () => context.pages(),
    on(name, listener) { guard(listeners.has(name) && typeof listener === 'function'); listeners.get(name).add(listener); },
    off(name, listener) { listeners.get(name)?.delete(listener); },
    drain: async () => { await Promise.all([...pending]); },
    dispose() { context.off('response', response); context.off('requestfinished', finished); context.off('requestfailed', failed); } });
}

/** No API client, fixture loader, database handle, or browser storage access is exposed. */
export async function createReferenceBrowserActor(options = {}) {
  requireReferenceRuntimeEnvironment();
  const { account, pin, observer = {}, onLogin, report = {}, existingDeviceIDs } = options;
  guard(record(account) && account.id === REFERENCE_VIEWER && typeof account.username === 'string' &&
    account.username.length > 0 && account.username.length <= 128 && typeof account.password === 'string' &&
    account.password.length > 0 && account.password.length <= 4096 && typeof pin === 'function' &&
    typeof onLogin === 'function' && record(observer) && record(report) && Array.isArray(existingDeviceIDs) &&
    existingDeviceIDs.length <= 10000 && existingDeviceIDs.every(value => typeof value === 'string' && value.length <= 256));
  const started = Date.now(), monotonicStarted = process.hrtime.bigint(), secrets = [account.password],
    pending = new Set(), sockets = new Set(), requestRecords = new WeakMap();
  const physicalHTTP = [], frameHTTP = [], physicalMessages = [], browserMessages = [], lifecycle = [], handshakes = [],
    browserBindings = new Set(), blockedExternal = [], bootstrapRequests = new WeakMap();
  let phase = 'browser_start', documentId = 0, browser, context, page, proxy, proof, proofContext, token = null, loginIntent = false,
    logoutIntent = false, closed = false, poisoned = false, cleanupMode = false, identityLost = false, clockChanged = false, bootstrapOpen = true,
    pinPending = null, frameIndex = 0, physicalIndex = 0, sequence = 0,
    pendingLoginProof = null, physicalLoginMetadata = null, frameLoginMetadata = null, loginPublication = null,
    physicalLoginCompleted = false, frameLoginFinished = false;
  Object.assign(report, { format: 1, origin: REFERENCE_ORIGIN, user_id: REFERENCE_VIEWER, server_id: REFERENCE_SERVER,
    service_workers: 'allow', proxy_bypass: '<-loopback>', phase, pin_checks: 0, observer_errors: 0,
    started_utc: new Date(started).toISOString(), elapsed_clock: 'process.hrtime.bigint', clock_changed: false,
    login: { attempted: false, status: null }, logout: { attempted: false, status: null, login_view_visible: false },
    http: { seen: 0, admitted: 0, completed: 0, failed: 0, rejected: 0, active: 0, login: 0, logout: 0,
      capabilities: 0, request_bytes: 0, response_bytes: 0 },
    cleanup_http: { seen: 0, admitted: 0, completed: 0, failed: 0, rejected: 0, active: 0, request_bytes: 0, response_bytes: 0 },
    websocket: { seen: 0, admitted: 0, opened: 0, closed: 0, failed: 0, active: 0, control_attempts: 0,
      browser_seen: 0, browser_observations: [], entries: [] },
    context_http: { requests: 0, responses: 0, finished: 0, failed: 0, service_worker_requests: 0, cleanup_requests: 0 },
    session_proof_owner: 'runtime_viewer_only', failures: [] });
  const elapsed = () => Number((process.hrtime.bigint() - monotonicStarted) / 1000000n);
  const pageRoute = () => {
    if (!page) return null;
    try {
      const value = new URL(page.url()), route = value.pathname + value.search + value.hash;
      if ([...value.searchParams].some(([key]) => SECRET_KEYS.has(key.toLowerCase()))) return null;
      return hasSecret(route, secrets) ? null : route;
    }
    catch { return null; }
  };
  const metadata = () => ({ phase, document_id: `document-${documentId}`, page_route: pageRoute() });
  const stamp = () => {
    const elapsedMs = elapsed();
    if (!clockChanged && Math.abs(Date.now() - started - elapsedMs) > 1000) {
      clockChanged = true; report.clock_changed = true;
      fail('clock_changed', { sequence: ++sequence, elapsed_ms: elapsedMs, ...metadata() });
    }
    return { sequence: ++sequence, elapsed_ms: elapsedMs, ...metadata() };
  };
  function lifecycleEvent(type, extra = {}) {
    guard(lifecycle.length < L.requests * 4 + L.frames * 4);
    const value = { type, ...stamp(), ...extra }; lifecycle.push(value); return value;
  }
  function fail(reason, timing = undefined, association = undefined) {
    poisoned = true;
    if (report.failures.length < 16) report.failures.push({ reason, ...(timing ?? stamp()), ...(association ?? {}) });
    if (!cleanupMode || identityLost) for (const socket of sockets) socket.destroy();
  }
  function notify(name, value, stamped = false) {
    if (!stamped) {
      const timing = stamp();
      Object.assign(value, Object.hasOwn(value, 'request_sequence') ? { sequence: timing.sequence, elapsed_ms: timing.elapsed_ms } : timing);
    }
    if (typeof observer[name] !== 'function') return;
    try {
      const result = observer[name](structuredClone(value));
      guard(result === undefined, 'reference_observer_must_be_synchronous');
    } catch { report.observer_errors += 1; fail('observer_failed'); }
  }
  function track(promise, association) {
    const observed = Promise.resolve(promise).catch(error => {
      fail('passive_observation_failed', undefined, referenceObservationFailure(error, association));
    });
    pending.add(observed); observed.finally(() => pending.delete(observed)); return observed;
  }
  const trafficAllowed = kind => referenceTrafficAllowed({ poisoned, cleanup: cleanupMode, identityLost, closed }, kind);
  async function verifyPin(forCleanup) {
    guard(!identityLost && !closed && (forCleanup || !poisoned), identityLost ? 'reference_identity_pin_lost' : 'reference_actor_unavailable');
    if (!pinPending) pinPending = bounded(Promise.resolve().then(pin), 15000, 'reference_pin_timeout').then(value => {
      guard(value !== false, 'reference_pin_changed'); report.pin_checks += 1;
    }).catch(() => { identityLost = true; fail('pin_changed'); throw new Error('reference_identity_pin_lost'); }).finally(() => { pinPending = null; });
    await pinPending;
  }
  const assertPinned = () => verifyPin(false);
  const assertCleanupPinned = () => verifyPin(true);
  function authority(raw, headers, required) {
    const actual = referenceAuthority(raw, headers);
    if (actual && !secrets.includes(actual.token)) secrets.push(actual.token);
    guard(!actual || token !== null && actual.token === token, 'reference_foreign_authority');
    guard(!required || actual !== null, 'reference_missing_authority');
    return actual;
  }
  function observedRequestAuthority(raw, method, headers, evidence) {
    const actual = referenceAuthority(raw, headers);
    if (actual && !secrets.includes(actual.token)) secrets.push(actual.token);
    return referenceObservedRequestAuthority(raw, method, headers, token, evidence);
  }
  function requestEvidence(raw, method, headers, scope, index, extra = {}) {
    const timing = stamp();
    let evidence;
    try { evidence = referenceRequestRecord(raw, method, headers, secrets); }
    catch { evidence = { method: safeMethod(method), kind: classifyReferenceRequest(raw, method).kind, route: null, query: {}, hidden_query: [], shape_sha256: null,
      request_sha256: typeof raw === 'string' ? sha(method + '\n' + raw) : null, token_sha256: null, capture: false }; }
    return { id: `${scope}-${index}`, index, ...evidence, scope, ...timing, request_sequence: timing.sequence,
      start_elapsed_ms: timing.elapsed_ms, response_elapsed_ms: null,
      finished_elapsed_ms: null, ...metadata(), sourceworker: scope === 'physical' ? null : false, main_frame: null, from_service_worker: null,
      content_type: null, status: null, completed: false, ...(scope === 'physical' ? referencePhysicalHTTPFacts() : {}), ...extra };
  }
  function bootstrapRequest(request, evidence = null) {
    let state = bootstrapRequests.get(request);
    if (!state) {
      let facts;
      try {
        facts = referenceBootstrapExternalRequest(request.url(), request.method(), request.resourceType(), physicalLoginMetadata,
          { phase, loginIntent, loginCount: report.http.login, bootstrapOpen, sourceworker: request.serviceWorker() !== null,
            cleanup: cleanupMode, poisoned, identityLost, seen: blockedExternal.length }, secrets);
      } catch { return null; }
      const timing = evidence ? { sequence: evidence.request_sequence, elapsed_ms: evidence.start_elapsed_ms,
        phase: evidence.phase, document_id: evidence.document_id, page_route: evidence.page_route } : stamp();
      const entry = { index: blockedExternal.length, count: blockedExternal.length + 1, request_id: evidence?.id ?? null,
        request_sequence: evidence?.request_sequence ?? timing.sequence, ...timing, ...facts, transport: null,
        action: 'pending_route_abort', abort_completed: false, aborted_elapsed_ms: null };
      let complete;
      const done = new Promise(resolve => { complete = resolve; });
      state = { entry, done, complete, abortRequested: false }; bootstrapRequests.set(request, state); blockedExternal.push(entry);
    }
    if (evidence) {
      state.entry.request_id = evidence.id; state.entry.request_sequence = evidence.request_sequence;
      const hidden = [...new URL(request.url()).searchParams].map(([key, value]) => ({ key, value_sha256: sha(value) }))
        .sort((left, right) => left.key < right.key ? -1 : left.key > right.key ? 1 : 0);
      Object.assign(evidence, { kind: 'blocked_external_registration', route: state.entry.route, query: {}, hidden_query: hidden,
        shape_sha256: sha(state.entry.route + '\n[]\n' + JSON.stringify(hidden)), token_sha256: null, capture: false });
    }
    return state;
  }
  function startHandshake(raw, method, kind, observedHead = null) {
    guard(handshakes.length < 64, 'reference_handshake_observation_limit');
    const timing = stamp();
    let route = observedHead?.route ?? null;
    if (!observedHead) {
      if (kind === 'connect') route = raw === '127.0.0.1:18197' ? raw : null;
      else try { const value = normalizedURL(raw, true); if (!hasSecret(value.route, secrets)) route = value.route; } catch { /* Keep only the original request digest. */ }
    }
    const evidence = { id: `handshake-${handshakes.length}`, index: handshakes.length, kind, method: safeMethod(method), route,
      request_sha256: observedHead?.request_sha256 ?? sha(method + '\n' + raw), token_sha256: null, ...timing, request_sequence: timing.sequence,
      start_elapsed_ms: timing.elapsed_ms, response_elapsed_ms: null, finished_elapsed_ms: null,
      source: 'physical_forward_proxy', status: null, admitted: false, completed: false, rejected: false, failed: false,
      outcome: 'pending', reason: null };
    handshakes.push(evidence); notify('onHandshakeStart', evidence, true); return evidence;
  }
  function handshakeResponse(evidence, status, source) {
    evidence.status = status; evidence.response_elapsed_ms = elapsed(); evidence.response_source = source;
    notify('onHandshakeResponse', evidence);
  }
  function finishHandshake(evidence, outcome, reason = null) {
    if (!evidence || evidence.outcome !== 'pending') return;
    Object.assign(evidence, { completed: outcome === 'completed', rejected: outcome === 'rejected', failed: outcome === 'failed',
      outcome, reason, finished_elapsed_ms: elapsed() });
    notify('onHandshakeFinished', evidence);
  }
  async function captureLogin(bytes, encoding) {
    guard(bytes.length <= L.captureBytes);
    const value = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(decodedJSONBytes(bytes, encoding)));
    guard(record(value) && typeof value.AccessToken === 'string' && value.AccessToken.length > 0 && value.AccessToken.length <= 4096,
      'reference_login_token_missing');
    if (!secrets.includes(value.AccessToken)) secrets.push(value.AccessToken);
    guard(value.User?.Id === REFERENCE_VIEWER && value.ServerId === REFERENCE_SERVER &&
      value.User?.Policy?.IsAdministrator === false && value.SessionInfo?.UserId === REFERENCE_VIEWER &&
      typeof value.SessionInfo.DeviceId === 'string' && value.SessionInfo.DeviceId.length > 0 && value.SessionInfo.DeviceId.length <= 256 &&
      typeof value.SessionInfo.Id === 'string' && value.SessionInfo.Id.length > 0 && value.SessionInfo.Id.length <= 256,
    'reference_login_principal_rejected');
    guard(physicalLoginMetadata && physicalLoginMetadata.client_name === value.SessionInfo.Client &&
      physicalLoginMetadata.device_id === value.SessionInfo.DeviceId && physicalLoginMetadata.device_name === value.SessionInfo.DeviceName &&
      physicalLoginMetadata.client_version === value.SessionInfo.ApplicationVersion && !existingDeviceIDs.includes(value.SessionInfo.DeviceId) &&
      typeof value.SessionInfo.LastActivityDate === 'string' && Number.isFinite(Date.parse(value.SessionInfo.LastActivityDate)),
    'reference_login_metadata_rejected');
    pendingLoginProof = { token_sha256: sha(value.AccessToken), session_id: value.SessionInfo.Id, user_id: value.User.Id,
      ...physicalLoginMetadata, server_id: value.ServerId, created_at: value.SessionInfo.LastActivityDate,
      created_at_source: 'SessionInfo.LastActivityDate', kind: 'emby', slot: 'B', frame_login_finished: false,
      physical_login_completed: false, request_metadata_matches: false };
    assertPublicJSON(pendingLoginProof, secrets);
    token = value.AccessToken; report.token_sha256 = pendingLoginProof.token_sha256;
    report.login.principal = { user_id: value.User.Id, server_id: value.ServerId, is_administrator: false,
      response_body_sha256: sha(bytes), response_bytes: bytes.length };
  }
  function publishLogin() {
    if (loginPublication || !pendingLoginProof || !physicalLoginCompleted || !frameLoginFinished) return;
    loginPublication = (async () => {
      guard(frameLoginMetadata && Object.keys(physicalLoginMetadata).every(key => physicalLoginMetadata[key] === frameLoginMetadata[key]),
        'reference_frame_physical_metadata_mismatch');
      Object.assign(pendingLoginProof, { frame_login_finished: true, physical_login_completed: true, request_metadata_matches: true });
      await bounded(Promise.resolve(onLogin({ token, proof: structuredClone(pendingLoginProof) })), 15000, 'reference_private_login_capture_failed');
      report.login.proof = structuredClone(pendingLoginProof); lifecycleEvent('login_private_capture_complete');
    })();
    track(loginPublication, { channel: 'login_private_capture', request_id: physicalHTTP.find(value => value.kind === 'login')?.id ?? null });
  }
  function reserve(plan) {
    const state = report.http, cleanup = report.cleanup_http;
    plan.cleanup = cleanupMode;
    if (plan.cleanup) cleanup.seen += 1;
    guard(trafficAllowed(plan.kind) && referenceHTTPBudgetAvailable(state, plan.cleanup, cleanup, plan.kind));
    if (plan.kind === 'login') { guard(loginIntent && state.login === 0 && !logoutIntent); state.login += 1; }
    else if (plan.kind === 'logout') { guard(logoutIntent && token && state.login === 1 && state.logout === 0); state.logout += 1; }
    else if (plan.kind === 'capabilities') { guard(token && loginIntent && !logoutIntent && state.capabilities < L.capabilities); state.capabilities += 1; }
    else guard(plan.kind === 'read');
    if (plan.kind !== 'login') authority(REFERENCE_ORIGIN + plan.path, plan.authorityHeaders, !publicRead(plan.route, plan.method));
    state.admitted += 1; state.active += 1;
    if (plan.cleanup) { cleanup.admitted += 1; cleanup.active += 1; }
  }
  async function forwardHTTP(request, response, requestIndex) {
    const state = report.http;
    let plan, evidence, upstream, incoming, admitted = false, finished = false, requestBytes = 0, responseBytes = 0;
    const timer = setTimeout(() => stop('http_timeout'), L.httpMs);
    function finish(outcome, reason = null, errorCode = null) {
      if (finished) return;
      if (outcome === 'completed' && (!evidence || !evidence.upstream_create_attempted || !evidence.upstream_created ||
        !evidence.upstream_end_attempted || !evidence.upstream_end_returned || !evidence.upstream_response_received ||
        !/^[0-9a-f]{64}$/.test(evidence.request_body_sha256 ?? ''))) { stop('http_incomplete'); return; }
      finished = true; clearTimeout(timer);
      if (admitted) state.active -= 1;
      state[outcome] += 1;
      if (plan?.cleanup) {
        if (admitted) report.cleanup_http.active -= 1;
        report.cleanup_http[outcome] += 1;
      }
      if (evidence) {
        if (outcome === 'completed') physicalHTTPPhase(evidence, 'completed');
        Object.assign(evidence, { finished_elapsed_ms: elapsed(), completed: outcome === 'completed', outcome, reason,
          failed: outcome !== 'completed', error_code: outcome === 'completed' ? null : errorCode,
          request_bytes: requestBytes, response_bytes: responseBytes });
        notify('onHTTPFinished', evidence);
        if (plan?.kind === 'login' && outcome === 'completed' && evidence.status === 200) {
          physicalLoginCompleted = true; publishLogin();
        }
        if (plan?.kind === 'logout' && outcome === 'completed' && evidence.status === 204) report.logout.physical_completed = true;
      }
    }
    function stop(reason, rejected = false, error = null) {
      if (finished) return;
      const code = physicalHTTPErrorCode(error, evidence?.transport_phase ?? 'admission', reason);
      upstream?.destroy(); incoming?.destroy();
      if (!response.headersSent && !response.destroyed) { response.writeHead(rejected ? 403 : 502, { Connection: 'close', 'Content-Length': '0' }); response.end(); }
      else response.destroy();
      finish(rejected ? 'rejected' : 'failed', reason, code);
      fail(reason, undefined, { request_id: evidence?.id ?? `physical-${requestIndex}`, channel: 'physical_http', observation_reason: code });
    }
    request.on('error', () => stop('http_request_error'));
    request.on('aborted', () => stop('http_request_aborted'));
    response.on('error', () => stop('http_response_error'));
    response.on('close', () => { if (!response.writableFinished) stop('http_downstream_closed'); });
    try {
      state.seen += 1;
      const rawHeaders = Object.fromEntries(Array.from({ length: request.rawHeaders.length / 2 }, (_, index) =>
        [request.rawHeaders[index * 2].toLowerCase(), request.rawHeaders[index * 2 + 1]]));
      evidence = requestEvidence(request.url, request.method, rawHeaders, 'physical', requestIndex);
      physicalHTTP.push(evidence); notify('onHTTPStart', evidence, true);
      plan = referenceHTTPPlan(request.url, request.method, request.rawHeaders);
      evidence.request_content_type = requestContentType(plan.authorityHeaders['content-type']);
      reserve(plan); admitted = true;
      physicalHTTPPhase(evidence, 'request_body');
      const chunks = [];
      for await (const chunk of request) {
        requestBytes += chunk.length; state.request_bytes += chunk.length;
        if (plan.cleanup) report.cleanup_http.request_bytes += chunk.length;
        guard(requestBytes <= plan.length && requestBytes <= L.requestBytes && (plan.cleanup
          ? plan.kind === 'logout' || report.cleanup_http.request_bytes <= L.cleanupRequestBytes : state.request_bytes <= 4194304));
        chunks.push(chunk);
      }
      guard(!finished && request.complete && requestBytes === plan.length && !request.rawTrailers?.length);
      const bytes = Buffer.concat(chunks);
      await new Promise(resolve => {
        void dispatchReferenceHTTP({ plan, bytes, account, evidence,
          pin: () => plan.cleanup ? assertCleanupPinned() : assertPinned(),
          canDispatch: () => !finished && trafficAllowed(plan.kind),
          onLoginMetadata: value => { physicalLoginMetadata = value; }, onCreated: value => { upstream = value; },
          onError: () => { stop('http_upstream_error'); resolve(); },
          onIdle: () => { stop('http_upstream_idle'); resolve(); },
          onUpgrade: (_reply, socket) => { socket.destroy(); stop('http_unexpected_upgrade'); resolve(); },
          onResponse: received => {
          incoming = received;
          void (async () => {
            try {
              const reply = referenceHTTPResponsePlan(incoming.statusCode, incoming.rawHeaders, plan.method);
              Object.assign(evidence, { status: reply.status, response_elapsed_ms: elapsed(), content_type: reply.contentType });
              const responseStamp = stamp();
              const capture = reply.status === 200 && (plan.kind === 'login' || evidence.capture);
              const responseChunks = [];
              physicalHTTPPhase(evidence, 'response_body');
              for await (const chunk of incoming) {
                responseBytes += chunk.length; state.response_bytes += chunk.length;
                if (plan.cleanup) report.cleanup_http.response_bytes += chunk.length;
                guard(!reply.noBody && responseBytes <= L.responseBytes && (plan.cleanup
                  ? plan.kind === 'logout' ? responseBytes <= L.captureBytes : report.cleanup_http.response_bytes <= L.cleanupResponseBytes
                  : state.response_bytes <= L.totalResponseBytes));
                responseChunks.push(chunk);
              }
              guard(incoming.complete && !incoming.rawTrailers?.length && (reply.noBody || reply.length === null || responseBytes === reply.length));
              const body = Buffer.concat(responseChunks);
              if (capture) {
                guard(responseBytes <= L.captureBytes && reply.contentType === 'application/json');
                if (plan.kind === 'login') await captureLogin(body, reply.contentEncoding);
                else evidence.response = referenceResponseProjection(body, secrets, reply.contentEncoding);
              }
              if (plan.kind === 'login') report.login.status = reply.status;
              if (plan.kind === 'logout') { report.logout.status = reply.status; report.logout.token_sha256 = evidence.token_sha256; }
              guard(trafficAllowed(plan.kind) && !finished);
              Object.assign(evidence, { sequence: responseStamp.sequence, elapsed_ms: responseStamp.elapsed_ms }); notify('onHTTPResponse', evidence, true);
              guard(trafficAllowed(plan.kind));
              physicalHTTPPhase(evidence, 'downstream_write');
              response.writeHead(reply.status, incoming.statusMessage, reply.headers);
              response.end(body, error => { if (error) stop('http_response_error', false, error); else finish('completed'); resolve(); });
            } catch (error) { stop('http_response_guard_failed', false, error); resolve(); }
          })();
        } }).catch(error => { stop('http_admission_or_request_failed', false, error); resolve(); });
      });
    } catch (error) { stop('http_admission_or_request_failed', !admitted, error); }
    finally { if (!finished) stop('http_incomplete'); }
  }
  function forwardWebSocket(raw, method, rawHeaders, client, head, connected = false, observedHandshake = null) {
    let handshake = observedHandshake;
    try { handshake ??= startHandshake(raw, method, connected ? 'connect_upgrade' : 'upgrade'); }
    catch { client.destroy(); fail('handshake_observation_limit'); return; }
    const state = report.websocket, index = state.seen++;
    let peer, upstream, established = false, finished = false, admitted = false;
    const clientState = createReferenceFrameState('client'), serverState = createReferenceFrameState('server');
    const entry = { id: `ws-${index}`, index, token_sha256: null, opened: false, closed: false, connect_transport: connected,
      handshake_id: handshake.id, handshake_delivered: false,
      browser_opened: false, physical_messages: 0, browser_messages: 0, start_elapsed_ms: elapsed() };
    state.entries.push(entry);
    let timer = setTimeout(() => stop('websocket_handshake_timeout'), 10000);
    function stop(reason = null) {
      if (finished) return; finished = true; clearTimeout(timer);
      if (admitted) state.active -= 1;
      if (established) state.closed += 1;
      entry.closed = true; entry.finished_elapsed_ms = elapsed(); entry.reason = reason;
      finishHandshake(handshake, admitted ? 'failed' : 'rejected', reason ?? 'closed_before_handshake_completed');
      peer?.destroy(); upstream?.destroy(); client.destroy();
      if (reason && !logoutIntent && !closed) { state.failed += 1; fail(reason); }
      notify('onPhysicalSocketClosed', entry);
    }
    function transfer(frameState, chunk, destination) {
      const messages = [];
      try {
        const frames = decodeWebSocketFrames(frameState, chunk, elapsed(), message => {
          guard(physicalMessages.length < L.messages, 'reference_physical_message_limit');
          const evidence = { socket_id: entry.id, connection_id: entry.id, token_sha256: entry.token_sha256,
            received_elapsed_ms: elapsed(), ...metadata(), ...referenceSocketMessageRecord(message, secrets), source: 'physical_websocket',
            forwarded: false, forwarded_elapsed_ms: null };
          entry.physical_messages += 1; Object.assign(evidence, stamp()); physicalMessages.push(evidence);
          messages.push(evidence); guard(!poisoned);
        });
        const writes = [];
        for (const frame of frames) {
          guard(destination.writableLength + frame.length <= L.queuedBytes);
          let resolveWrite;
          writes.push(new Promise(resolve => { resolveWrite = resolve; }));
          if (!destination.write(frame, error => resolveWrite(!error))) { (frameState.direction === 'client' ? client : peer).pause();
            destination.once('drain', () => (frameState.direction === 'client' ? client : peer)?.resume()); }
        }
        track(Promise.all(writes).then(results => {
          const forwarded = results.every(Boolean);
          for (const evidence of messages) {
            evidence.forwarded = forwarded;
            evidence.forwarded_elapsed_ms = forwarded ? elapsed() : null;
            notify('onPhysicalMessage', evidence, true);
          }
          if (!forwarded) stop('websocket_frame_write_failed');
        }), { channel: 'physical_websocket_write', request_id: handshake.id });
      } catch (error) {
        for (const evidence of messages) notify('onPhysicalMessage', evidence, true);
        if (error?.message === 'reference_websocket_control_denied') state.control_attempts += 1;
        stop(error?.message === 'reference_client_message_not_allowlisted' ? 'websocket_client_message_denied' :
          error?.message === 'reference_websocket_control_denied' ? 'websocket_control_message_denied' : 'websocket_frame_guard_failed');
      }
    }
    client.on('error', () => stop('websocket_client_error'));
    client.on('close', () => stop(clientState.closed || serverState.closed || logoutIntent || closed ? null : 'websocket_client_closed'));
    client.pause();
    track((async () => {
      try {
        guard(trafficAllowed('websocket') && token && loginIntent && !logoutIntent && index < L.sockets && state.active === 0);
        const plan = referenceWebSocketPlan(raw, method, rawHeaders), actual = authority(raw, plan.authorityHeaders, true);
        entry.token_sha256 = actual.token_sha256; handshake.token_sha256 = actual.token_sha256;
        await assertPinned();
        guard(trafficAllowed('websocket') && !logoutIntent && state.active === 0);
        state.admitted += 1; state.active += 1; admitted = true; handshake.admitted = true;
        upstream = http.request({ hostname: '127.0.0.1', port: 18197, method: 'GET', path: plan.path,
          headers: plan.headers, agent: false, maxHeaderSize: L.headerBytes });
        upstream.on('error', () => stop('websocket_upstream_error'));
        upstream.on('response', response => {
          void (async () => {
            try {
              const reply = referenceHTTPResponsePlan(response.statusCode, response.rawHeaders, 'GET'), chunks = [];
              handshakeResponse(handshake, reply.status, 'upstream_response');
              let bytes = 0;
              for await (const chunk of response) { bytes += chunk.length; guard(bytes <= L.captureBytes); chunks.push(chunk); }
              guard(response.complete && !response.rawTrailers?.length && (reply.length === null || reply.length === bytes));
              let header = `HTTP/1.1 ${reply.status} ${response.statusMessage}\r\n`;
              for (let field = 0; field < reply.headers.length; field += 2) header += `${reply.headers[field]}: ${reply.headers[field + 1]}\r\n`;
              client.end(Buffer.concat([Buffer.from(header + '\r\n', 'latin1'), ...chunks]), error => {
                if (!error) finishHandshake(handshake, 'completed');
                stop(error ? 'websocket_upgrade_response_write_failed' : 'websocket_upgrade_refused');
              });
            } catch { stop('websocket_upgrade_response_failed'); }
          })();
        });
        upstream.on('upgrade', (response, socket, incomingHead) => {
          peer = socket; sockets.add(peer); peer.once('close', () => sockets.delete(peer));
          try {
            handshakeResponse(handshake, response.statusCode, 'upstream_response');
            const headers = referenceWebSocketResponsePlan(response.statusCode, response.rawHeaders, plan);
            let header = `HTTP/1.1 101 ${response.statusMessage}\r\n`;
            for (let field = 0; field < headers.length; field += 2) header += `${headers[field]}: ${headers[field + 1]}\r\n`;
            guard(trafficAllowed('websocket') && !logoutIntent && !finished);
            client.write(header + '\r\n', error => {
              if (error || finished) { stop('websocket_handshake_write_failed'); return; }
              established = true; state.opened += 1; entry.opened = true; entry.handshake_delivered = true;
              entry.opened_elapsed_ms = elapsed(); finishHandshake(handshake, 'completed'); notify('onPhysicalSocketOpened', entry);
              clearTimeout(timer); timer = setTimeout(() => stop('websocket_lifetime_timeout'), L.socketMs);
              for (const binding of browserBindings) binding.tryBind();
            });
            peer.setTimeout(L.socketIdleMs, () => stop('websocket_idle_timeout'));
            peer.on('data', chunk => transfer(serverState, chunk, client));
            client.on('data', chunk => transfer(clientState, chunk, peer));
            peer.on('error', () => stop('websocket_peer_error'));
            peer.on('close', () => stop(clientState.closed || serverState.closed || logoutIntent || closed ? null : 'websocket_peer_closed'));
            if (head.length) transfer(clientState, head, peer);
            if (incomingHead.length) transfer(serverState, incomingHead, client);
            client.resume();
          } catch { stop('websocket_handshake_guard_failed'); }
        });
        upstream.end();
      } catch { stop('websocket_admission_failed'); }
    })(), { channel: 'physical_websocket_setup', request_id: handshake.id });
  }
  function observePageSocket(socket) {
    const timing = stamp(), index = report.websocket.browser_seen++;
    const observation = { id: `browser-ws-${index}`, index, ...timing, request_sequence: timing.sequence,
      request_sha256: null, token_sha256: null, connection_id: null, bound: false, closed: false, reason: null };
    report.websocket.browser_observations.push(observation);
    const queued = [];
    let entry = null, received = 0, sent = 0, queuedBytes = 0, disposed = false, raw = null, timer = null;
    function dispose() {
      if (disposed) return; disposed = true; clearTimeout(timer); browserBindings.delete(binding);
      observation.unbound_message_count = queued.length;
      queued.length = 0; queuedBytes = 0; raw = null;
    }
    function reject(reason) { observation.reason = reason; dispose(); fail(reason); }
    function publish(evidence) {
      guard(entry, 'reference_browser_message_not_bound');
      Object.assign(evidence, { socket_id: entry.id, connection_id: entry.id, pending_binding: false, bound_elapsed_ms: elapsed() });
      entry.browser_messages += 1; notify('onBrowserMessage', evidence, true);
    }
    function tryBind() {
      if (disposed || entry || raw === null) return;
      try {
        const actual = authority(raw, {}, true);
        guard(actual.token_sha256 === observation.token_sha256);
        const matches = report.websocket.entries.filter(value => value.opened && value.handshake_delivered && !value.closed &&
          !value.browser_opened && value.token_sha256 === actual.token_sha256);
        guard(matches.length <= 1, 'reference_browser_physical_socket_ambiguous');
        if (!matches.length) return;
        entry = matches[0]; entry.browser_opened = true; entry.browser_opened_elapsed_ms = elapsed();
        entry.browser_observation_id = observation.id;
        Object.assign(observation, { bound: true, connection_id: entry.id, bound_elapsed_ms: elapsed() });
        clearTimeout(timer); notify('onBrowserSocketOpened', { ...entry, browser_socket_id: observation.id, source: 'browser_page_websocket' });
        for (const message of queued.splice(0)) publish(message);
        queuedBytes = 0;
      } catch { reject('browser_websocket_binding_failed'); }
    }
    const binding = { tryBind, dispose };
    function observe(event, direction) {
      if (disposed) return;
      const receivedAt = stamp();
      try {
        guard(typeof event.payload === 'string', 'reference_browser_binary_message_denied');
        const bytes = Buffer.from(event.payload);
        guard(bytes.length <= L.messageBytes && browserMessages.length < L.messages);
        const decoded = messageJSON(bytes); if (direction === 'client') requireReferenceClientMessage(decoded);
        const evidence = { socket_id: null, connection_id: null, browser_socket_id: observation.id, pending_binding: true,
          token_sha256: observation.token_sha256, ...receivedAt, received_elapsed_ms: receivedAt.elapsed_ms,
          ...referenceSocketMessageRecord({ ...decoded, bytes, direction, index: direction === 'server' ? ++received : ++sent }, secrets),
          source: 'browser_page_websocket' };
        browserMessages.push(evidence); bytes.fill(0);
        if (entry) publish(evidence);
        else {
          guard(queued.length < 128 && queuedBytes + evidence.bytes <= L.queuedBytes, 'reference_browser_pending_message_limit');
          queued.push(evidence); queuedBytes += evidence.bytes; tryBind();
        }
      } catch (error) { reject(error?.message === 'reference_client_message_not_allowlisted' ?
        'browser_client_message_denied' : 'browser_websocket_message_guard_failed'); }
    }
    // Browser events may precede the physical upgrade. Attach all listeners before binding.
    socket.on('framereceived', event => observe(event, 'server'));
    socket.on('framesent', event => observe(event, 'client'));
    socket.on('socketerror', () => { if (!logoutIntent && !closed) reject('browser_websocket_error'); });
    socket.on('close', () => {
      Object.assign(observation, { closed: true, closed_elapsed_ms: elapsed() });
      notify('onBrowserSocketClosed', { socket_id: entry?.id ?? null, browser_socket_id: observation.id,
        token_sha256: observation.token_sha256, received_elapsed_ms: elapsed(), ...metadata(), source: 'browser_page_websocket' });
      if (!entry && !cleanupMode && !closed) reject('browser_socket_closed_before_binding');
      else dispose();
    });
    browserBindings.add(binding);
    try {
      guard(index < L.sockets && trafficAllowed('websocket'), 'reference_browser_socket_admission');
      raw = socket.url(); const actual = authority(raw, {}, true);
      observation.request_sha256 = sha('GET\n' + raw); observation.token_sha256 = actual.token_sha256;
      notify('onBrowserSocketObserved', observation, true);
      timer = setTimeout(() => reject('browser_physical_binding_timeout'), 15000); tryBind();
    } catch { reject('browser_websocket_authority_failed'); }
  }
  function observeContextRequest(request) {
    const raw = request.url();
    if (/^(?:data|blob):/.test(raw)) return;
    const worker = request.serviceWorker();
    let mainFrame = null;
    if (worker === null) { try { mainFrame = request.frame() === page?.mainFrame(); } catch { mainFrame = null; } }
    if (mainFrame === true && request.isNavigationRequest()) documentId += 1;
    const evidence = requestEvidence(raw, request.method(), {}, worker ? 'service_worker' : 'frame', frameIndex++,
      { sourceworker: Boolean(worker), main_frame: mainFrame, resource_type: request.resourceType(), cleanup: cleanupMode });
    const external = bootstrapRequest(request, evidence);
    frameHTTP.push(evidence);
    report.context_http.requests += 1; if (worker) report.context_http.service_worker_requests += 1;
    if (evidence.cleanup) report.context_http.cleanup_requests += 1;
    notify('onFrameRequest', evidence, true);
    const operation = (async () => {
      if (external) {
        guard(await bounded(external.done, 5000, 'reference_bootstrap_abort_unobserved'), 'reference_bootstrap_abort_failed');
        return evidence;
      }
      const headers = await bounded(request.allHeaders(), 3000);
      const actual = observedRequestAuthority(raw, request.method(), headers, evidence);
      evidence.token_sha256 = actual?.token_sha256 ?? null;
      if (evidence.kind === 'login' && worker === null) frameLoginMetadata = referenceClientMetadata(raw, headers);
      guard(evidence.cleanup ? evidence.kind === 'logout' || report.context_http.cleanup_requests <= L.cleanupRequests * 2
        : report.context_http.requests <= L.requests * 2);
      return evidence;
    })();
    requestRecords.set(request, { operation, evidence });
    track(operation, { channel: 'context_request', request_id: evidence.id });
  }
  function observeContextResponse(response) {
    const timing = stamp();
    const state = requestRecords.get(response.request());
    track((async () => {
      const evidence = await state?.operation; if (!evidence) return;
      const headers = response.headers();
      Object.assign(evidence, { sequence: timing.sequence, elapsed_ms: timing.elapsed_ms,
        status: response.status(), response_elapsed_ms: timing.elapsed_ms, from_service_worker: response.fromServiceWorker(),
        content_type: safeContentType(headers['content-type']) });
      report.context_http.responses += 1;
      notify('onFrameResponse', evidence, true);
      if (evidence.kind === 'blocked_external_registration') fail('blocked_external_received_response');
      if (evidence.kind === 'login') guard(evidence.status === 200 && token && pendingLoginProof);
      if (evidence.kind === 'logout') { guard(evidence.token_sha256 === report.token_sha256); report.logout.frame_token_sha256 = evidence.token_sha256; }
    })(), { channel: 'context_response', request_id: state?.evidence.id ?? null });
  }
  function observeContextFinished(request, failed) {
    const timing = stamp();
    const state = requestRecords.get(request);
    track((async () => {
      const evidence = await state?.operation; if (!evidence) return;
      Object.assign(evidence, { sequence: timing.sequence, elapsed_ms: timing.elapsed_ms,
        finished_elapsed_ms: timing.elapsed_ms, completed: !failed, failed });
      report.context_http[failed ? 'failed' : 'finished'] += 1; notify('onFrameFinished', evidence, true);
      if (evidence.kind === 'login' && evidence.scope === 'frame' && !failed && evidence.status === 200) {
        frameLoginFinished = true; publishLogin();
      }
    })(), { channel: failed ? 'context_failed' : 'context_finished', request_id: state?.evidence.id ?? null });
  }
  async function drainTransport(forCleanup = false) {
    await bounded((async () => {
      for (;;) { const observed = [...pending]; await Promise.all(observed); if (!pending.size && report.http.active === 0) break;
        await new Promise(resolve => setTimeout(resolve, 25)); }
    })(), 30000, 'reference_transport_drain_timeout');
    guard(!identityLost && (!poisoned || forCleanup && cleanupMode), 'reference_actor_failed');
  }
  const settled = () => drainTransport(false);
  function recordViewerProof() {
    if (proof?.report.outcome !== 'all_observed_logout_tokens_rejected' || proof.report.entries.length !== 1 ||
      proof.report.entries[0].ui_request.response_status !== 204 || proof.report.entries[0].verification.status !== 401 ||
      proof.report.entries[0].token_fingerprint !== report.token_sha256) return false;
    if (report.logout.post_logout_status !== 401) {
      report.logout.post_logout_status = 401;
      lifecycleEvent('viewer_post_logout_401', { token_sha256: report.token_sha256, source: 'independent_node_http_cleanup', is_ui_request: false });
    }
    return true;
  }
  async function loginUI() {
    guard(!loginIntent && !logoutIntent && !closed); phase = 'manual_login'; report.phase = phase;
    await assertPinned();
    await page.goto(`${REFERENCE_ORIGIN}/web/index.html`, { waitUntil: 'domcontentloaded', timeout: 30000 });
    const form = page.locator('form:has(input[type="password"]:visible)'), manual = page.getByText('Manual Login', { exact: true });
    await Promise.any([form.waitFor({ state: 'visible', timeout: 15000 }), manual.waitFor({ state: 'visible', timeout: 15000 })]);
    if (!await form.isVisible()) await manual.locator('xpath=..').getByRole('button').click({ timeout: 8000 });
    await form.waitFor({ state: 'visible', timeout: 10000 });
    const username = form.locator('input[type="text"]:visible'), password = form.locator('input[type="password"]:visible');
    const submit = form.getByRole('button', { name: 'Sign In', exact: true });
    guard(await username.count() === 1 && await password.count() === 1 && await submit.count() === 1);
    await username.fill(account.username); await password.fill(account.password); await assertPinned();
    loginIntent = true; report.login.attempted = true;
    const response = page.waitForResponse(value => classifyReferenceRequest(value.url(), value.request().method()).kind === 'login', { timeout: 15000 }).catch(() => null);
    await submit.click({ timeout: 10000 }); guard((await response)?.status() === 200);
    await password.waitFor({ state: 'hidden', timeout: 15000 }); await settled();
    guard(loginPublication); await loginPublication;
    guard(token && report.http.login === 1 && report.login.proof); await assertPinned();
    phase = 'authenticated'; report.phase = phase; return structuredClone(report.login);
  }
  async function logoutUI() {
    try {
    guard(loginIntent && token && !logoutIntent && !closed); phase = 'ui_logout'; report.phase = phase;
    await assertCleanupPinned();
    cleanupMode = true;
    report.logout.cleanup = { identity_confirmed: true, entered_after_failure: poisoned, failure_reason: null };
    let control = page.getByRole('button', { name: /\bSign Out\b/i }).filter({ visible: true });
    if (!await control.count()) { await page.getByRole('button', { name: 'Settings', exact: true }).filter({ visible: true }).click({ timeout: 8000 });
      control = page.getByRole('button', { name: /\bSign Out\b/i }).filter({ visible: true }); }
    await control.waitFor({ state: 'visible', timeout: 10000 }); guard(await control.count() === 1);
    logoutIntent = true; report.logout.attempted = true;
    const response = page.waitForResponse(value => classifyReferenceRequest(value.url(), value.request().method()).kind === 'logout', { timeout: 15000 }).catch(() => null);
    await control.click({ timeout: 8000 }); guard((await response)?.status() === 204);
    await Promise.any([page.getByText('Manual Login', { exact: true }).waitFor({ state: 'visible', timeout: 10000 }),
      page.locator('form:has(input[type="password"]:visible)').waitFor({ state: 'visible', timeout: 10000 })]);
    await drainTransport(true); guard(report.http.logout === 1 && report.logout.token_sha256 === report.token_sha256 &&
      report.logout.frame_token_sha256 === report.token_sha256);
    report.logout.login_view_visible = true;
    await bounded(proofContext.drain(), 5000, 'reference_viewer_authority_timeout');
    await bounded(proof.drain(), 25000, 'reference_viewer_proof_timeout');
    guard(recordViewerProof(), 'reference_viewer_proof_failed');
    await assertCleanupPinned(); return structuredClone(report.logout);
    } catch {
      const reason = identityLost ? 'identity_pin_lost' : !token ? 'fresh_viewer_token_unavailable' : closed ? 'browser_already_closed' :
        report.logout.status !== null && report.logout.status !== 204 ? 'logout_status_not_204' :
          report.logout.status === 204 && report.logout.post_logout_status !== 401 ? 'viewer_401_proof_incomplete' : 'ui_logout_not_completed';
      report.logout.failure_reason = reason;
      if (report.logout.cleanup) report.logout.cleanup.failure_reason = reason;
      lifecycleEvent('ui_logout_unavailable', { reason });
      throw new Error(`reference_${reason}`);
    }
  }
  async function close() {
    if (closed) return;
    const failures = [];
    if (loginIntent && token && !logoutIntent) try { await logoutUI(); } catch { failures.push('ui_logout'); }
    else if (loginIntent && report.logout.post_logout_status !== 401) {
      report.logout.failure_reason ??= token ? 'logout_already_attempted' : 'fresh_viewer_token_unavailable';
      failures.push('ui_logout');
    }
    try {
      if (report.logout.status === 204 && !identityLost) await assertCleanupPinned();
      if (proofContext) await bounded(proofContext.drain(), 5000);
      if (proof) await bounded(proof.dispose(), 25000); proofContext?.dispose();
      if (report.logout.status === 204 && recordViewerProof() && report.logout.cleanup) {
        report.logout.cleanup.proof_complete_on_close = true;
        if (report.logout.failure_reason === 'viewer_401_proof_incomplete') {
          report.logout.failure_reason = report.logout.login_view_visible ? null : 'ui_logout_view_not_confirmed';
          report.logout.cleanup.failure_reason = report.logout.failure_reason;
        }
      }
    } catch { failures.push('viewer_proof_dispose'); }
    closed = true;
    try { await bounded(context?.close() ?? Promise.resolve(), 10000); } catch { failures.push('context_close'); }
    try { await bounded(browser?.close() ?? Promise.resolve(), 10000); } catch { failures.push('browser_close'); }
    for (const binding of browserBindings) binding.dispose();
    for (const socket of sockets) socket.destroy();
    try { if (proxy) await bounded(new Promise(resolve => proxy.close(resolve)), 10000); } catch { failures.push('proxy_close'); }
    try { await bounded(Promise.all([...pending]), 10000); } catch { failures.push('observer_drain'); }
    report.closed = true; report.cleanup_failures = failures; phase = 'closed'; report.phase = phase; token = null;
  }
  try {
    await assertPinned();
    proxy = http.createServer({ maxHeaderSize: L.headerBytes }, (request, response) => {
      const index = physicalIndex++;
      track(forwardHTTP(request, response, index), { channel: 'physical_http', request_id: `physical-${index}` });
    });
    proxy.maxHeadersCount = 128; proxy.requestTimeout = L.httpMs; proxy.headersTimeout = 10000;
    proxy.on('connection', socket => {
      if (sockets.size >= L.connections || !trafficAllowed('read')) { socket.destroy(); return; }
      sockets.add(socket); socket.once('close', () => sockets.delete(socket));
    });
    proxy.on('clientError', (_error, socket) => { socket.destroy(); fail('proxy_client_error'); });
    proxy.on('upgrade', (request, socket, head) => forwardWebSocket(request.url, request.method, request.rawHeaders, socket, head));
    proxy.on('connect', (request, socket, head) => {
      let handshake;
      try { handshake = startHandshake(request.url, request.method, 'connect'); }
      catch { socket.destroy(); fail('handshake_observation_limit'); return; }
      let bytes = Buffer.from(head), done = false, responseWritten = false, innerAccepted = false, innerHandshake = null;
      const timer = setTimeout(() => reject(), 10000);
      function reject() {
        if (handshake.outcome !== 'pending') return;
        done = true; clearTimeout(timer); finishHandshake(handshake, handshake.admitted ? 'failed' : 'rejected', 'proxy_connect_rejected');
        socket.destroy(); fail('proxy_connect_rejected');
      }
      function complete() { if (responseWritten && innerAccepted) finishHandshake(handshake, 'completed'); }
      function read(chunk) {
        if (done) return;
        try {
          bytes = Buffer.concat([bytes, chunk]);
          const parsed = referenceConnectInner(bytes, observed => {
            innerHandshake = startHandshake(null, observed.method, 'connect_upgrade', observed);
          }, secrets);
          if (!parsed) return;
          done = true; innerAccepted = true; clearTimeout(timer); socket.off('data', read); complete();
          forwardWebSocket(parsed.raw, parsed.method, parsed.rawHeaders, socket, parsed.head, true, innerHandshake);
        } catch {
          finishHandshake(innerHandshake, 'rejected', 'proxy_connect_inner_admission_rejected');
          reject();
        }
      }
      try {
        guard(request.method === 'CONNECT' && request.url === '127.0.0.1:18197' && token && !logoutIntent && trafficAllowed('websocket'));
        const headers = parseHeaders(request.rawHeaders); guard(headers.first('host') === '127.0.0.1:18197' &&
          !headers.values.has('transfer-encoding') && (headers.length === null || headers.length === 0));
        handshake.admitted = true;
        socket.write('HTTP/1.1 200 Connection Established\r\n\r\n', error => {
          if (error) { reject(); return; }
          responseWritten = true; handshakeResponse(handshake, 200, 'forward_proxy_connect_response'); complete();
        });
        socket.once('error', reject); socket.once('close', () => { if (!innerAccepted) reject(); });
        socket.on('data', read); read(Buffer.alloc(0));
      } catch { reject(); }
    });
    await new Promise((resolve, reject) => { proxy.once('error', reject); proxy.listen(0, '127.0.0.1', resolve); });
    const address = proxy.address(); guard(address && typeof address === 'object');
    report.forward_proxy_port = address.port;
    const { chromium } = createRequire(import.meta.url)(PLAYWRIGHT);
    browser = await chromium.launch({ headless: true, timeout: 30000,
      args: ['--no-sandbox', '--disable-dev-shm-usage', '--disable-background-networking'] });
    context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', serviceWorkers: 'allow',
      proxy: { server: `http://127.0.0.1:${address.port}`, bypass: '<-loopback>' } });
    proofContext = createReferenceLogoutProofContext(context, () => { report.observer_errors += 1; fail('viewer_proof_observation_failed'); },
      async request => {
        const headers = await bounded(request.allHeaders(), 3000), actual = referenceAuthority(request.url(), headers, token);
        guard(trafficAllowed('logout') && logoutIntent && token && actual?.token_sha256 === report.token_sha256 &&
          report.http.logout === 1 && report.logout.status === 204 && report.logout.physical_completed === true &&
          report.logout.token_sha256 === actual.token_sha256, 'reference_viewer_proof_authority_rejected');
        return true;
      });
    proof = createBrowserSessionProof({ context: proofContext, target: REFERENCE_ORIGIN });
    report.session_proof = proof.report;
    report.session_proof_event_scope = 'Genuine frame logout events admitted only after an original HTTP 204 response';
    context.on('request', observeContextRequest); context.on('response', observeContextResponse);
    context.on('requestfinished', request => observeContextFinished(request, false));
    context.on('requestfailed', request => observeContextFinished(request, true));
    await context.route('**/*', async route => {
      try {
        const request = route.request(), external = bootstrapRequest(request);
        if (external) {
          guard(!external.abortRequested, 'reference_duplicate_bootstrap_route'); external.abortRequested = true;
          external.entry.transport = 'request-route';
          try {
            await route.abort('blockedbyclient');
            Object.assign(external.entry, { action: 'blocked_without_upstream_connection', abort_completed: true, aborted_elapsed_ms: elapsed() });
            notify('onBlockedExternal', external.entry); external.complete(true);
          } catch { external.entry.action = 'abort_failed'; external.complete(false); fail('bootstrap_external_abort_failed'); }
          return;
        }
        const policy = classifyReferenceRequest(request.url(), request.method(), request.resourceType());
        if (!policy.allow || !trafficAllowed(policy.kind)) { fail('frame_policy_denied'); await route.abort('blockedbyclient'); return; }
        await route.continue();
      } catch { fail('frame_route_guard_failed'); await route.abort('blockedbyclient').catch(() => {}); }
    });
    page = await context.newPage();
    page.on('framenavigated', frame => { if (frame === page.mainFrame()) lifecycleEvent('main_frame_navigation'); });
    page.on('websocket', observePageSocket);
    page.on('pageerror', () => { report.page_error_count = (report.page_error_count ?? 0) + 1; });
    page.on('console', message => {
      if (['warning', 'error'].includes(message.type())) report.console_warning_error_count = (report.console_warning_error_count ?? 0) + 1;
    });
    await assertPinned();
  } catch { await close(); throw new Error('reference_browser_setup_failed'); }
  function snapshot() {
    const active = report.websocket.entries.find(entry => entry.opened && !entry.closed) ?? report.websocket.entries.at(-1);
    return structuredClone({ http: { physical: physicalHTTP, frames: frameHTTP }, events: { physical: physicalMessages, browser: browserMessages },
      lifecycle, handshakes, blocked_external: blockedExternal, sequence, elapsed_ms: elapsed(), document_id: `document-${documentId}`, route: pageRoute(), phase,
      socket: { connection_id: active?.id ?? null, token_sha256: active?.token_sha256 ?? null, ...report.websocket }, report });
  }
  function publicMethod(method, label) {
    return async (...args) => {
      try { return await method(...args); }
      catch { lifecycleEvent(`${label}_failed`); throw new Error(`reference_${label}_failed`); }
    };
  }
  return Object.freeze({ page, context, report, loginUI: publicMethod(loginUI, 'ui_login'), logoutUI: publicMethod(logoutUI, 'ui_logout'),
    signOut: publicMethod(logoutUI, 'ui_logout'), assertPinned, settled, close, started, elapsed,
    get sequence() { return sequence; }, get documentID() { return `document-${documentId}`; }, get token_sha256() { return report.token_sha256 ?? null; },
    open: async () => {}, snapshot, stamp() { const value = stamp(); return { sequence: value.sequence, elapsed_ms: value.elapsed_ms }; },
    setPhase(value) { guard(typeof value === 'string' && /^[a-z][a-z0-9_]{0,63}$/.test(value)); bootstrapOpen = false;
      phase = value; report.phase = value; lifecycleEvent('phase_changed'); } });
}
