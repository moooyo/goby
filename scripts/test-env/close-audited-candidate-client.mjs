#!/usr/bin/env node
/** Close one core-client scenario from retained evidence; never perform live work. */
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { gunzipSync, inflateSync, brotliDecompressSync } from 'node:zlib';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { validateCandidateManifest, validateCandidateGateway, selectedCandidateItem, candidateRequestScope, validateCandidateLogin, candidateLogoutProven } from './client-browser-audited-candidate.mjs';

const SHA = /^[0-9a-f]{64}$/, FIELD = /^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/;
const MAX_BODY = 1048576, MAX_FILE = 32 * MAX_BODY;
const TABLES = 'activity_entries application_key_clients application_key_devices application_keys catalog_entities client_playback_references devices encoding_jobs extra_reserved_paths item_entities item_extra_resources item_images item_metadata_state item_subtitles item_theme_resources items libraries library_roots managed_settings play_sessions scan_jobs schema_migrations server_settings sessions task_definitions task_occurrences task_run_children task_run_requests task_runs task_triggers theme_owner_ids theme_reserved_paths user_item_data user_settings users'.split(' ').sort();
const SOURCE_FILES = { closer: 'close-audited-candidate-client.mjs', adapter: 'client-browser-audited-candidate.mjs', gateway: 'client-acceptance-gateway.py', proxy: 'client-acceptance-proxy.py',
  sessionProof: 'client-browser-session-proof.mjs', movie: 'client-browser-playback.mjs', audio: 'client-browser-audio-flow.mjs', subtitles: 'client-browser-subtitle-flow.mjs', tv: 'client-browser-tv-flow.mjs' };
const EPOCH_KEYS = 'kind version status transitionInput transitionHelper runtimeHelper originalProvision seedProvenance currentSource candidate candidateProcess postgresProcess lease before after preservation calls helpers candidateAdmissionComplete'.split(' ');
const BINDING_KEYS = 'kind version runtimeEpoch originalSeed seedExecutor seedInput seedSessionAddendum admission02 serverId admin actors controlQ catalog catalogFile actualCatalogDtos libraries roots resources seedCleanup currentSessions candidateAdmissionComplete'.split(' ');
const ENV_EPOCH_KEYS = ['previousEpoch', 'productInput', 'operationKind', 'configurationChange'];
const ENV_BINDING_KEYS = ['previousBinding', 'admission03', 'failureCloseout', 'closedState'];
const ENV_ADDITIONS = { GOBY_BACKUP_MAX_OBJECT_BYTES: '67108864', GOBY_BACKUP_MAX_TOTAL_BYTES: '268435456' };
const own = value => value !== null && typeof value === 'object' && !Array.isArray(value);
const need = (value, code) => { if (!value) throw new Error(code); };
const sha = bytes => createHash('sha256').update(bytes).digest('hex');
const integer = value => Number.isSafeInteger(value) && value >= 0;
const ordered = value => Array.isArray(value) ? value.map(ordered) : own(value) ? Object.fromEntries(Object.keys(value).sort().map(key => [key, ordered(value[key])])) : value;
const equal = (left, right) => JSON.stringify(ordered(left)) === JSON.stringify(ordered(right));
const exact = (value, keys) => own(value) && equal(Object.keys(value).sort(), [...keys].sort());
const descriptor = value => exact(value, ['path', 'sha256']) && typeof value.path === 'string' && path.posix.isAbsolute(value.path) && !value.path.split('/').includes('..') && SHA.test(value.sha256);
const one = (values, code) => { need(Array.isArray(values) && values.length === 1, code); return values[0]; };
const bytes64 = value => { need(typeof value === 'string' && /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(value), 'invalid_base64'); return Buffer.from(value, 'base64'); };
const instant = value => { const result = Date.parse(value); need(typeof value === 'string' && /(?:Z|[+-]\d\d:\d\d)$/.test(value) && Number.isFinite(result), 'timestamp_invalid'); return result; };
const ns = value => { need(integer(value) || typeof value === 'string' && /^[0-9]+$/.test(value), 'monotonic_integer_invalid'); return BigInt(value); };

/** Reuse the bounded duplicate-key scan used by the existing client observers. */
export function strictJSON(bytes, maximum = MAX_FILE) {
  need(Buffer.isBuffer(bytes) && bytes.length > 0 && bytes.length <= maximum, 'json_size_limit');
  const source = new TextDecoder('utf-8', { fatal: true }).decode(bytes); let index = 0, nodes = 0;
  const space = () => { while (index < source.length && /[\t\r\n ]/.test(source[index])) index++; };
  const string = () => { const start = index++; need(source[start] === '"', 'json_string_required');
    while (index < source.length) { const character = source[index++]; if (character === '"') return JSON.parse(source.slice(start, index)); if (character === '\\') index++; }
    throw new Error('json_unterminated_string'); };
  const value = depth => {
    need(depth <= 40 && ++nodes <= 500000, 'json_complexity_limit'); space();
    if (source[index] === '{') { index++; space(); const keys = new Set(); if (source[index] === '}') { index++; return; }
      for (;;) { space(); const key = string(); need(!keys.has(key), 'json_duplicate_key'); keys.add(key); space(); need(source[index++] === ':', 'json_colon_required'); value(depth + 1); space(); const next = source[index++]; if (next === '}') return; need(next === ',', 'json_comma_required'); } }
    if (source[index] === '[') { index++; space(); if (source[index] === ']') { index++; return; }
      for (;;) { value(depth + 1); space(); const next = source[index++]; if (next === ']') return; need(next === ',', 'json_comma_required'); } }
    if (source[index] === '"') { string(); return; }
    const scalar = /^(?:true|false|null|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)/.exec(source.slice(index)); need(scalar, 'json_scalar_invalid');
    if (/^-?\d/.test(scalar[0])) { const number = Number(scalar[0]); need(Number.isFinite(number) && (!Number.isInteger(number) || Number.isSafeInteger(number)), 'json_unsafe_number'); }
    index += scalar[0].length;
  };
  value(0); space(); need(index === source.length, 'json_trailing_bytes'); return JSON.parse(source);
}

export function parseHead(bytes, response = false) {
  need(Buffer.isBuffer(bytes) && bytes.length <= 65536 && bytes.subarray(-4).equals(Buffer.from('\r\n\r\n')), 'http_head_boundary');
  const lines = bytes.subarray(0, -4).toString('latin1').split('\r\n'), line = lines.shift(), headers = new Map();
  const match = response ? /^HTTP\/1\.[01] ([1-5][0-9]{2})(?: [^\r\n]*)?$/.exec(line) : /^([A-Z]{1,20}) ([^\x00-\x20\x7f]+) HTTP\/1\.[01]$/.exec(line);
  need(match && lines.length <= 100, 'http_start_line');
  for (const field of lines) { const at = field.indexOf(':'); need(at > 0 && FIELD.test(field.slice(0, at)) && !/[\x00-\x08\x0a-\x1f\x7f]/.test(field.slice(at + 1)), 'http_header_invalid');
    const key = field.slice(0, at).toLowerCase(), value = field.slice(at + 1).trim(); if (!headers.has(key)) headers.set(key, []); headers.get(key).push(value); }
  for (const key of ['host', 'content-length', 'transfer-encoding', 'content-encoding', 'content-type']) need((headers.get(key) ?? []).length <= 1, 'ambiguous_http_header');
  need(!(headers.has('content-length') && headers.has('transfer-encoding')), 'ambiguous_http_framing');
  const get = key => headers.get(key)?.[0] ?? null;
  if (get('content-length') !== null) need(/^(?:0|[1-9][0-9]*)$/.test(get('content-length')) && integer(Number(get('content-length'))), 'invalid_content_length');
  if (get('transfer-encoding') !== null) need(get('transfer-encoding').toLowerCase() === 'chunked', 'unsupported_transfer_encoding');
  return { line, lines, headers, get, ...(response ? { status: Number(match[1]) } : { method: match[1], target: match[2] }) };
}

export function decodeEntity(wire, head, maximum = MAX_BODY) {
  need(Buffer.isBuffer(wire) && wire.length <= maximum, 'wire_body_limit'); let entity = wire;
  if (head.get('transfer-encoding')) {
    let offset = 0, size = 0, chunks = 0; const pieces = [];
    for (;;) { const end = wire.indexOf('\r\n', offset); need(end >= offset && end - offset <= 1024 && ++chunks <= 65536, 'chunk_size_boundary');
      const text = wire.subarray(offset, end).toString('ascii'); need(/^[0-9a-fA-F]+(?:;[^\x00-\x1f\x7f]*)?$/.test(text), 'chunk_size_invalid');
      const length = Number.parseInt(text.split(';')[0], 16); need(integer(length), 'chunk_size_overflow'); offset = end + 2;
      if (length === 0) { const rest = wire.subarray(offset); if (rest.equals(Buffer.from('\r\n'))) break;
        const trailers = parseHead(Buffer.concat([Buffer.from('GET / HTTP/1.1\r\n'), rest]));
        need(!['host', 'content-length', 'transfer-encoding', 'authorization'].some(key => trailers.headers.has(key)), 'forbidden_chunk_trailer'); break; }
      need(offset + length + 2 <= wire.length && wire.subarray(offset + length, offset + length + 2).equals(Buffer.from('\r\n')), 'chunk_data_boundary');
      size += length; need(size <= maximum, 'entity_body_limit'); pieces.push(wire.subarray(offset, offset + length)); offset += length + 2;
    }
    entity = Buffer.concat(pieces);
  } else if (head.get('content-length') !== null) need(Number(head.get('content-length')) === wire.length, 'entity_content_length_mismatch');
  const encoding = (head.get('content-encoding') ?? 'identity').toLowerCase();
  const decompress = { gzip: gunzipSync, deflate: inflateSync, br: brotliDecompressSync }[encoding];
  if (encoding !== 'identity') { need(decompress, 'unsupported_content_encoding'); entity = decompress(entity, { maxOutputLength: maximum }); }
  need(entity.length <= maximum, 'decoded_body_limit'); return entity;
}

function authority(head, url) {
  const values = [];
  for (const key of ['x-emby-token', 'x-mediabrowser-token']) values.push(...(head.headers.get(key) ?? []));
  for (const [key, value] of url.searchParams) if (['api_key', 'x-emby-token', 'x-mediabrowser-token', 'token', 'accesstoken', 'access_token'].includes(key.toLowerCase())) values.push(value);
  for (const key of ['authorization', 'x-emby-authorization']) for (const value of head.headers.get(key) ?? []) {
    const matches = [...value.matchAll(/\bToken=(?:"([^"]*)"|([^,\s]+))/gi)]; for (const match of matches) values.push(match[1] ?? match[2]);
  }
  need(values.every(value => typeof value === 'string' && value.length > 0) && new Set(values).size <= 1, 'conflicting_token_carriers');
  return values[0] ?? null;
}

function requestURL(head, origin) {
  const url = new URL(head.target, origin); need(url.origin === origin && !url.username && !url.password && !url.hash && head.get('host') === url.host, 'physical_origin_mismatch'); return url;
}

function expectedForwardHead(head, url, websocket) {
  const connection = new Set((head.headers.get('connection') ?? []).join(',').split(',').map(value => value.trim().toLowerCase()).filter(Boolean));
  need(!['host', 'content-length', 'transfer-encoding'].some(key => connection.has(key)), 'connection_framing_conflict');
  const omitted = new Set(['connection', 'proxy-connection', 'proxy-authorization', 'keep-alive', ...[...connection].filter(key => key !== 'upgrade')]);
  if (!websocket) omitted.add('upgrade');
  return Buffer.from([head.line.replace(head.target, url.pathname + url.search), ...head.lines.filter(line => !omitted.has(line.slice(0, line.indexOf(':')).toLowerCase())),
    websocket ? 'Connection: Upgrade' : 'Connection: close', '', ''].join('\r\n'), 'latin1');
}

function retainedBody(result, prefix, head, required = false) {
  const retained = result[prefix + 'BodyRetained'] === true, count = result[prefix + 'BodyWireBytes']; need(integer(count), 'body_count_invalid');
  if (!retained) { need(result[prefix + 'BodyBase64'] === null, 'excluded_body_present'); need(!required || count === 0, 'required_body_not_retained'); return count === 0 ? Buffer.alloc(0) : null; }
  const wire = bytes64(result[prefix + 'BodyBase64']); need(wire.length <= count, 'retained_count_exceeds_wire');
  if (required) need(result[prefix + 'BodyTruncated'] === false && wire.length === count, 'required_body_truncated');
  return result[prefix + 'BodyTruncated'] || wire.length !== count ? null : decodeEntity(wire, head);
}

/** Verify counts separately from the gateway's pairing-only index.complete. */
export function verifyExchange(intent, result, manifest) {
  const request = intent.request, rawHead = bytes64(request.rawRequestHeadBase64), original = rawHead.length ? parseHead(rawHead) : null;
  if (result.upstreamConnected !== true) { need((result.upstreamBytesWritten ?? 0) === 0, 'rejected_request_reached_upstream');
    if (result.outcome === 'connect_handshake') need(request.kind === 'connect' && original?.method === 'CONNECT' && original.target === new URL(manifest.browserOrigin).host &&
      original.get('host') === original.target && request.parentOrdinal === null && result.transportHandshakeOnly === true && result.responseStatus === 200 &&
      bytes64(result.responseHeadBase64).equals(Buffer.from('HTTP/1.1 200 Connection Established\r\n\r\n')), 'connect_handshake_mismatch');
    return { ordinal: intent.ordinal, rejected: true, handshake: result.outcome === 'connect_handshake', request, original, intent, result }; }
  need(equal(result.request, request) && ['observed', 'rejected_or_interrupted'].includes(result.outcome), 'request_result_binding');
  need(result.bodyStorage === 'http-transfer-wire' && result.webMediaAndWebSocketBodiesRetained === false, 'body_storage_policy_mismatch');
  need(original, 'forwarded_request_head_missing');
  const url = requestURL(original, manifest.browserOrigin), websocket = request.kind === 'websocket';
  const requestedUpgrade = Boolean((original.headers.get('connection') ?? []).join(',').split(',').some(value => value.trim().toLowerCase() === 'upgrade') &&
    original.get('upgrade')?.toLowerCase() === 'websocket');
  need(websocket === requestedUpgrade, 'websocket_request_class_mismatch');
  need(original.method === request.method && original.target === request.target && url.pathname === request.path, 'request_facts_changed');
  const forwarded = bytes64(request.forwardedRequestHeadBase64); need(forwarded.equals(expectedForwardHead(original, url, websocket)), 'forwarded_head_changed');
  need(result.backend === (url.pathname.startsWith('/web/') ? 'reference' : 'goby') || result.outcome === 'rejected_or_interrupted' && result.backend === undefined, 'upstream_backend_mismatch');
  const response = result.responseHeadBase64 === null ? null : parseHead(bytes64(result.responseHeadBase64), true);
  need((response?.status ?? null) === result.responseStatus, 'response_status_mismatch');
  if (response) need(equal(response.lines.map(line => [line.slice(0, line.indexOf(':')), line.slice(line.indexOf(':') + 1).trim()]), result.responseHeaders), 'response_header_facts_changed');
  const interim = (result.interimHeadsBase64 ?? []).map(bytes64); need(interim.length <= 4 && interim.every(raw => { const status = parseHead(raw, true).status; return status >= 100 && status < 200 && status !== 101; }), 'interim_response_invalid');
  const headerBytes = (response ? bytes64(result.responseHeadBase64).length : 0) + interim.reduce((total, raw) => total + raw.length, 0);
  for (const key of ['requestBodyWireBytes', 'responseBodyWireBytes', 'upstreamBytesWritten', 'clientBytesWritten', 'webSocketClientBytes', 'webSocketUpstreamBytes']) need(integer(result[key]), 'wire_count_invalid');
  const upgraded = response?.status === 101;
  if (upgraded) { const key = original.get('sec-websocket-key');
    need(websocket && bytes64(key).length === 16 && original.get('sec-websocket-version') === '13' && response.get('upgrade')?.toLowerCase() === 'websocket' &&
      (response.get('connection') ?? '').toLowerCase().split(',').map(value => value.trim()).includes('upgrade') &&
      response.get('sec-websocket-accept') === createHash('sha1').update(key + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').digest('base64'), 'websocket_response_handshake'); }
  const requestBytes = result.requestBodyWireBytes + result.webSocketClientBytes, responseBytes = result.responseBodyWireBytes + result.webSocketUpstreamBytes;
  need(result.upstreamBytesWritten <= forwarded.length + requestBytes && result.clientBytesWritten <= headerBytes + responseBytes, 'write_count_exceeds_observation');
  if (result.requestForwardedComplete) need(result.requestBodyComplete && result.upstreamBytesWritten === forwarded.length + requestBytes, 'request_forward_incomplete');
  if (result.responseForwardedComplete) need(result.completeHTTP && result.clientBytesWritten === headerBytes + responseBytes, 'response_forward_incomplete');
  if (response && !upgraded && result.completeHTTP && original.method !== 'HEAD' && ![204, 304].includes(response.status) && response.get('content-length') !== null)
    need(Number(response.get('content-length')) === result.responseBodyWireBytes, 'complete_response_length_mismatch');
  if (response && (original.method === 'HEAD' || [204, 304].includes(response.status))) need(result.responseBodyWireBytes === 0, 'bodyless_response_payload');
  if (upgraded || ['web', 'media'].includes(request.kind)) need(result.requestBodyRetained === false && result.responseBodyRetained === false, 'excluded_payload_retained');
  const requestEntity = retainedBody(result, 'request', original), responseEntity = response ? retainedBody(result, 'response', response) : null;
  const token = authority(original, url), scope = candidateRequestScope(websocket ? 'ws://' + url.host + url.pathname + url.search : url.href, original.method, manifest);
  return { ordinal: intent.ordinal, request, original, response, url, token, tokenHash: token ? sha(token) : null, scope,
    requestEntity, responseEntity, deliveredBodyBytes: Math.max(0, result.clientBytesWritten - headerBytes), headerBytes, intent, result,
    requestDelivered: result.requestBodyComplete === true && result.upstreamBytesWritten === forwarded.length + requestBytes,
    complete: result.outcome === 'observed' && result.requestBodyComplete === true && result.completeHTTP === true && result.requestForwardedComplete === true && result.responseForwardedComplete === true };
}

function completeJSON(exchange, response = false) {
  need(exchange.complete && exchange.result.bodyEvidenceComplete === true, 'critical_api_not_complete');
  const entity = response ? exchange.responseEntity : exchange.requestEntity;
  need(Buffer.isBuffer(entity) && entity.length > 0, 'critical_json_missing'); return strictJSON(entity, MAX_BODY);
}

function requireACK(exchange, status) { need(exchange.complete && exchange.response?.status === status && exchange.requestEntity !== null, 'critical_ack_incomplete'); }

export function verifyLedgerRows(attestation, index, rows, manifest) {
  need(index.kind === 'core-av-gateway-ledger-index' && index.schemaVersion === 1 && index.complete === true && index.failure === null &&
    index.unrecordedConnectionCount === 0 && equal(index.deniedRequestCounts, { normal: 0, cleanup: 0 }) && index.webMediaAndWebSocketBodiesRetained === false,
  'physical_ledger_not_closed');
  need(rows.length === index.requestCount && rows.length === index.entries.length && rows.length <= attestation.budgets.maxRequests, 'ledger_cardinality');
  const counts = { normal: 0, cleanup: 0 }; let retained = 0; const exchanges = [];
  for (let offset = 0; offset < rows.length; offset++) {
    const { entry, intent, result } = rows[offset], ordinal = offset + 1;
    need(equal(entry, index.entries[offset]) && entry.ordinal === ordinal && intent.ordinal === ordinal && result.ordinal === ordinal &&
      ['normal', 'cleanup'].includes(entry.budgetClass) && intent.budgetClass === entry.budgetClass && result.budgetClass === entry.budgetClass &&
      intent.request.budgetClass === entry.budgetClass && equal(result.intent, entry.intent), 'ledger_ordinal_or_intent_mismatch');
    const request = intent.request, cleanup = request.kind === 'api' && (request.method === 'POST' && /^\/(?:emby\/)?Sessions\/(?:Playing\/Stopped|Logout)\/?$/i.test(request.path) ||
      request.method === 'GET' && /^\/(?:emby\/)?System\/Info\/?$/i.test(request.path));
    need(entry.budgetClass === (cleanup ? 'cleanup' : 'normal'), 'ledger_budget_class_mismatch');
    need(ns(intent.startedMonotonicNs) <= ns(result.completedMonotonicNs), 'request_time_reversed'); counts[entry.budgetClass]++;
    for (const prefix of ['request', 'response']) if (typeof result[prefix + 'BodyBase64'] === 'string') retained += bytes64(result[prefix + 'BodyBase64']).length;
    exchanges.push(verifyExchange(intent, result, manifest));
  }
  need(equal(counts, index.requestCounts) && counts.normal <= attestation.budgets.maxRequests - attestation.budgets.cleanupRequests &&
    counts.cleanup <= attestation.budgets.cleanupRequests && retained === index.retainedApiBytes && retained <= attestation.budgets.maxApiTotalBytes, 'ledger_budget_reconstruction');
  for (const exchange of exchanges) if (exchange.request.parentOrdinal !== null) {
    const parent = exchanges[exchange.request.parentOrdinal - 1]; need(parent?.handshake && parent.ordinal < exchange.ordinal && exchange.request.kind === 'websocket', 'connect_parent_binding');
  }
  return exchanges;
}

function matchesContext(exchange, report) {
  if (!exchange.url) return [];
  const start = ns(report.started_monotonic_ns), observed = ns(exchange.intent.startedMonotonicNs);
  return report.requests.filter(row => {
    if (!['frame', 'service_worker'].includes(row.scope) || row.method !== exchange.original.method || row.token_sha256 !== exchange.tokenHash || !integer(row.elapsed_ms)) return false;
    try { if (new URL(row.url).href !== exchange.url.href || (row.headers?.range ?? null) !== exchange.original.get('range')) return false; } catch { return false; }
    const delta = start + BigInt(row.elapsed_ms) * 1000000n - observed;
    if (delta < -5000000000n || delta > 5000000000n) return false;
    if (exchange.requestEntity && !bytes64(row.payload_base64 ?? '').equals(exchange.requestEntity)) return false;
    return true;
  });
}

function reportBody(exchange) { const value = completeJSON(exchange); need(own(value), 'playback_body_not_object'); return value; }
function routeItem(exchange) { return /^\/Items\/([^/]+)\/PlaybackInfo\/?$/i.exec(exchange.scope.route)?.[1] ?? null; }
function bodyPosition(body, fallback) { if (!Object.hasOwn(body, 'PositionTicks')) return fallback; need(integer(body.PositionTicks), 'position_ticks_invalid'); return body.PositionTicks; }
function mediaSourceMatches(value, canonical, item, correlated) { return value === canonical || item?.type === 'Audio' && correlated && value === item.id; }

export function requireMediaGET(exchange) {
  need(exchange.original.method === 'GET' && [200, 206].includes(exchange.response?.status) && exchange.deliveredBodyBytes > 0 &&
    (exchange.requestDelivered === true || exchange.result.requestForwardedComplete === true) && exchange.result.requestBodyComplete && exchange.response.get('content-length') !== null &&
    Number(exchange.response.get('content-length')) > 0 && !exchange.response.get('transfer-encoding'), 'actual_media_entity_bytes_missing');
  const range = exchange.response.get('content-range');
  if (exchange.response.status === 206) { const parsed = /^bytes (\d+)-(\d+)\/(\d+)$/.exec(range ?? '');
    need(parsed && parsed.slice(1).every(value => integer(Number(value))) && Number(parsed[1]) <= Number(parsed[2]) && Number(parsed[2]) < Number(parsed[3]) &&
      Number(parsed[2]) - Number(parsed[1]) + 1 === Number(exchange.response.get('content-length')), 'media_content_range_invalid'); }
}

export function universalAudioPreparation(play, login, manifest, references, beforeOrdinal) {
  const selected = selectedCandidateItem(manifest);
  if (!integer(beforeOrdinal) || beforeOrdinal === 0 || !['mp3', 'flac'].includes(manifest.scenario) || selected?.type !== 'Audio' || play.client_correlated !== true ||
      play.user_id !== manifest.actor.id || play.auth_session_id !== login.sessionId || play.device_id !== login.device_id ||
      (play.application_client_id ?? null) !== null || play.item_id !== selected.id || play.media_source_id !== 'mediasource_' + selected.id) return null;
  for (const exchange of login.media) {
    if (exchange.scope?.allowed !== true || exchange.scope.kind !== 'media' || exchange.original.method !== 'GET' ||
        exchange.tokenHash !== login.tokenHash || exchange.url.origin !== manifest.browserOrigin || exchange.ordinal >= beforeOrdinal ||
        !new RegExp('^/(?:emby/)?Audio/' + selected.id + '/universal(?:\\.[a-z0-9_-]+)?/?$', 'i').test(exchange.url.pathname)) continue;
    const values = key => [...exchange.url.searchParams].filter(([name]) => name.toLowerCase() === key).map(([, value]) => value);
    const nonces = values('playsessionid'), users = values('userid'), devices = [...values('deviceid'), ...values('x-emby-device-id')], sources = values('mediasourceid');
    if (nonces.length !== 1 || !nonces[0].trim() || Buffer.byteLength(nonces[0]) > 256 || /[\x00-\x1f\x7f]/.test(nonces[0]) || nonces[0].startsWith('play_') ||
        users.length > 1 || users.some(value => value !== manifest.actor.id) || devices.some(value => value !== login.device_id) ||
        sources.length > 1 || sources.some(value => !mediaSourceMatches(value, play.media_source_id, selected, true))) continue;
    const matches = references.filter(row => row.user_id === manifest.actor.id && row.auth_session_id === login.sessionId &&
      row.device_id === login.device_id && (row.application_client_id ?? null) === null && row.client_nonce === nonces[0] && row.play_session_id === play.id);
    if (matches.length !== 1) continue;
    requireMediaGET(exchange);
    return { kind: 'universal_audio', ordinal: exchange.ordinal, clientNonceSha256: sha(nonces[0]) };
  }
  return null;
}

export function verifyPostLogoutTraffic(login, exchanges) {
  const completed = ns(login.logout.result.completedMonotonicNs);
  for (const exchange of exchanges.filter(row => row.tokenHash === login.tokenHash && row !== login.verification &&
    ns(row.intent.startedMonotonicNs) >= completed))
    need(exchange.complete === true && exchange.response?.status === 401, 'revoked_token_used_successfully');
}

/** All cardinality here is physical ordinal cardinality, never frame/SW counts. */
export function analyzePhysical(exchanges, manifest, report, after) {
  const selected = selectedCandidateItem(manifest), logins = [];
  for (const exchange of exchanges.filter(row => row.scope?.kind === 'login')) {
    requireACK(exchange, 200); const body = reportBody(exchange), response = completeJSON(exchange, true);
    need(body.Username === manifest.actor.username && typeof body.Pw === 'string' && exchange.token === null, 'physical_login_actor_mismatch');
    const identity = validateCandidateLogin(response, manifest);
    need(matchesContext(exchange, report).some(row => row.scope === 'frame' && row.main_frame), 'physical_login_not_observed_by_ui');
    need(!logins.some(row => row.tokenHash === identity.token_sha256 || row.sessionId === identity.session_id), 'physical_login_identity_reused');
    logins.push({ ...identity, tokenHash: identity.token_sha256, sessionId: identity.session_id, token: response.AccessToken,
      login: exchange, logout: null, verification: null, infos: [], chains: [], media: [] });
  }
  need(logins.length === (manifest.scenario === 'movie' ? 2 : 1) && equal(logins.map(row => row.tokenHash), report.sessions.map(row => row.token_sha256)), 'physical_login_count_or_order');
  const byToken = new Map(logins.map(row => [row.tokenHash, row]));
  for (const exchange of exchanges) {
    if (exchange.rejected) continue;
    need(exchange.scope.allowed, 'forwarded_scope_violation');
    if (exchange.request.kind === 'web' || exchange.scope.kind === 'login') continue;
    const login = byToken.get(exchange.tokenHash);
    if (exchange.tokenHash) need(login && exchange.ordinal > login.login.ordinal, 'foreign_or_premature_token');
    if (['playback_info', 'playback_report', 'logout', 'media', 'subtitle', 'capabilities'].includes(exchange.scope.kind)) need(login, 'owned_request_missing_token');
    if (exchange.scope.kind === 'logout') { requireACK(exchange, 204); need(!login.logout && matchesContext(exchange, report).some(row => row.scope === 'frame' && row.main_frame), 'physical_logout_not_unique_ui'); login.logout = exchange; }
    if (exchange.scope.kind === 'playback_info') {
      requireACK(exchange, 200); const body = exchange.result.requestBodyWireBytes ? reportBody(exchange) : {}, value = completeJSON(exchange, true);
      need((body.UserId === undefined || body.UserId === manifest.actor.id) && typeof value.PlaySessionId === 'string' && value.PlaySessionId.length > 0 &&
        Array.isArray(value.MediaSources) && value.MediaSources.length === 1 && matchesContext(exchange, report).length > 0, 'playback_info_identity_missing');
      const itemId = routeItem(exchange); need((selected ? [selected.id] : manifest.catalog.episodes.map(row => row.id)).includes(itemId), 'playback_info_foreign_item');
      need(value.MediaSources.every(source => typeof source.Id === 'string' && source.Id === 'mediasource_' + itemId), 'playback_info_source_mismatch');
      login.infos.push({ exchange, body, value, itemId });
    }
    if (exchange.scope.kind === 'media') {
      need(exchange.original.method === 'GET' || ['HEAD', 'OPTIONS'].includes(exchange.original.method), 'media_method_invalid');
      if (exchange.original.method === 'GET' && [200, 206].includes(exchange.response?.status) && exchange.deliveredBodyBytes > 0) {
        requireMediaGET(exchange); login.media.push(exchange);
      } else if (exchange.original.method === 'GET' && exchange.complete && [200, 206].includes(exchange.response?.status))
        throw new Error('completed_media_get_has_no_entity_bytes');
    }
  }
  const newPlays = after.tables.play_sessions, references = after.tables.client_playback_references;
  const resolvePlay = (body, login) => {
    need(typeof body.PlaySessionId === 'string' && body.PlaySessionId.length > 0 && body.ItemId === selected?.id &&
      typeof body.MediaSourceId === 'string' && (!body.SessionId || body.SessionId === login.sessionId) &&
      (body.UserId === undefined || body.UserId === manifest.actor.id), 'play_report_scope');
    let id = body.PlaySessionId;
    if (!id.startsWith('play_')) id = one(references.filter(row => row.user_id === manifest.actor.id && row.auth_session_id === login.sessionId &&
      (row.application_client_id ?? null) === null && row.device_id === login.device_id && row.client_nonce === id), 'missing_or_ambiguous_client_reference').play_session_id;
    const play = one(newPlays.filter(row => row.id === id), 'canonical_play_missing');
    need(play.user_id === manifest.actor.id && play.auth_session_id === login.sessionId && play.device_id === login.device_id &&
      (play.application_client_id ?? null) === null && play.item_id === selected.id && play.media_source_id === 'mediasource_' + selected.id &&
      mediaSourceMatches(body.MediaSourceId, play.media_source_id, selected, play.client_correlated), 'canonical_play_owner_mismatch');
    return play;
  };
  const chains = new Map();
  for (const exchange of exchanges.filter(row => row.scope?.kind === 'playback_report')) {
    requireACK(exchange, 204); const login = byToken.get(exchange.tokenHash), body = reportBody(exchange), play = resolvePlay(body, login);
    need(matchesContext(exchange, report).length > 0, 'physical_play_report_not_observed_by_ui');
    const event = /\/Stopped\/?$/i.test(exchange.scope.route) ? 'stopped' : /\/Progress\/?$/i.test(exchange.scope.route) ? 'progress' : 'started';
    if (!chains.has(play.id)) chains.set(play.id, { play, login, reports: [], started: [], progress: [], stopped: [], media: [] });
    const chain = chains.get(play.id); chain.reports.push({ exchange, body, event }); chain[event].push({ exchange, body });
  }
  for (const chain of chains.values()) {
    need(chain.started.length > 0 && chain.progress.length > 0 && chain.stopped.length > 0, 'physical_play_lifecycle_missing');
    const first = chain.started[0].exchange, stop = chain.stopped[0].exchange;
    need(first.ordinal < stop.ordinal && chain.progress.some(row => row.exchange.ordinal > first.ordinal && row.exchange.ordinal < stop.ordinal) && chain.play.state === 'Stopped' && chain.play.stopped_at !== null,
      'play_not_durably_stopped');
    const info = chain.login.infos.find(info => info.itemId === selected.id && info.exchange.ordinal < first.ordinal &&
      (info.value.PlaySessionId === chain.play.id || chain.reports.some(row => row.body.PlaySessionId === info.value.PlaySessionId)));
    chain.preparation = info ? { kind: 'playback_info', ordinal: info.exchange.ordinal } :
      universalAudioPreparation(chain.play, chain.login, manifest, references, first.ordinal);
    need(chain.preparation, 'playback_preparation_start_chain_missing');
    chain.media = chain.login.media.filter(exchange => {
      const values = [...exchange.url.searchParams].filter(([key]) => key.toLowerCase() === 'playsessionid').map(([, value]) => value);
      const sources = [...exchange.url.searchParams].filter(([key]) => key.toLowerCase() === 'mediasourceid').map(([, value]) => value);
      if (!sources.every(value => mediaSourceMatches(value, chain.play.media_source_id, selected, chain.play.client_correlated))) return false;
      return values.length ? values.every(value => value === chain.play.id || chain.reports.some(row => row.body.PlaySessionId === value)) :
        [...chains.values()].filter(other => other.login === chain.login).length === 1;
    });
    need(chain.media.length > 0, 'play_chain_missing_get_media'); chain.login.chains.push(chain);
  }
  for (const login of logins) {
    need(login.logout && candidateLogoutProven(report.session_proof, login.tokenHash), 'logical_logout_proof_incomplete');
    login.verification = one(exchanges.filter(row => !row.rejected && row.tokenHash === login.tokenHash && row.original.method === 'GET' &&
      /^\/(?:emby\/)?System\/Info\/?$/i.test(row.url.pathname) && row.original.get('user-agent') === 'GobyBrowserPostLogoutVerification/1' && row.response?.status === 401), 'physical_post_logout_401_missing');
    requireACK(login.verification, 401);
    need(login.logout.ordinal < login.verification.ordinal, 'logout_verification_order');
    if (selected) need(login.chains.length > 0 && login.media.length > 0 && login.chains.every(chain => chain.stopped[0].exchange.ordinal < login.logout.ordinal), 'login_playback_cleanup_incomplete');
    else need(login.media.length === 0 && login.chains.length === 0, 'browse_performed_playback');
    verifyPostLogoutTraffic(login, exchanges);
  }
  if (manifest.scenario === 'movie') need(logins[0].verification.ordinal < logins[1].login.ordinal, 'movie_relogin_before_revocation_proof');
  return { logins, chains: [...chains.values()], exchanges };
}

function visibleVideo(report, label) { const step = one(report.playback.steps.filter(row => row.label === label), 'movie_step_missing');
  return one(step.videos.filter(row => row.visible), 'unique_movie_video_missing'); }

export function explainMediaPartial(exchange, report, login) {
  const matches = matchesContext(exchange, report), groups = new Map();
  for (const row of matches) { need(integer(row.failed_elapsed_ms) && row.failed === true && row.failure_error_text === 'net::ERR_ABORTED', 'media_partial_not_browser_abort');
    need(!groups.has(row.scope), 'media_partial_ambiguous_context'); groups.set(row.scope, row); }
  need(groups.size > 0 && groups.size <= 2, 'media_partial_context_missing');
  const ends = [...groups.values()].map(row => ns(report.started_monotonic_ns) + BigInt(row.failed_elapsed_ms) * 1000000n);
  need(ends.every(end => { const delta = end - ns(exchange.result.completedMonotonicNs); return delta >= -2000000000n && delta <= 2000000000n; }), 'media_partial_time_mismatch');
  const end = ends[0];
  const nearbyStop = login.chains.some(chain => chain.stopped.some(row => { const delta = ns(row.exchange.intent.startedMonotonicNs) - end; return delta >= -3000000000n && delta <= 5000000000n; }));
  const nearbySeek = [...groups.values()].every(row => typeof row.failure_ui_phase === 'string' && /seek/.test(row.failure_ui_phase)) &&
    login.media.some(next => next.ordinal !== exchange.ordinal && next.original.get('range') !== exchange.original.get('range') &&
    ns(next.intent.startedMonotonicNs) >= end - 1000000000n && ns(next.intent.startedMonotonicNs) <= end + 5000000000n);
  need(nearbyStop || nearbySeek, 'media_partial_without_seek_or_stop');
  return { ordinal: exchange.ordinal, interpretation: nearbyStop ? 'browser_abort_near_owned_stop' : 'browser_abort_near_range_seek',
    completeHTTP: exchange.result.completeHTTP, responseForwardedComplete: exchange.result.responseForwardedComplete, deliveredBodyBytes: exchange.deliveredBodyBytes };
}

export function verifyUI(report, manifest, physical) {
  need(report.outcome === 'scenario_completed' && report.failure === null && report.page_errors.length === 0 &&
    report.cleanup.browser_closed === true && report.cleanup.tokens_rejected === true && report.cleanup.media_stopped === true &&
    report.browser_environment.service_workers === 'allow' && report.browser_environment.proxy_bypass === '<-loopback>' &&
    report.browser_environment.quic === 'disabled' && report.browser_environment.nonproxied_webrtc_udp === 'disabled', 'client_ui_or_cleanup_incomplete');
  if (manifest.scenario === 'movie') {
    need(report.playback?.outcome === 'movie_ui_flow_completed', 'movie_ui_incomplete');
    for (const [first, last] of [['playing-start', 'playing-advanced'], ['resumed', 'resumed-advanced']]) {
      const a = visibleVideo(report, first), b = visibleVideo(report, last);
      need(!b.paused && b.current_time > a.current_time && b.total_video_frames > a.total_video_frames && b.video_width > 0 &&
        Math.abs(b.duration - manifest.catalog.movie.runtimeTicks / 10000000) <= 2, 'movie_frames_not_advancing');
    }
    const stop = visibleVideo(report, 'before-stop'), resumed = visibleVideo(report, 'resumed');
    need(Math.abs(stop.current_time - resumed.current_time) <= 15 && visibleVideo(report, 'paused').paused, 'movie_pause_or_resume_mismatch');
    for (const label of ['movie-seek-forward', 'movie-seek-backward']) { const a = visibleVideo(report, label + '-before'), b = visibleVideo(report, label + '-after');
      need(label.endsWith('forward') ? b.current_time > a.current_time + 10 : b.current_time < a.current_time - 10, 'movie_seek_not_observed'); }
  } else if (manifest.scenario === 'episode') {
    const row = report.episode_playback;
    need(row?.item_id === selectedCandidateItem(manifest).id && row.stopped && row.first.source_matches_item && row.advanced.source_matches_item &&
      row.advanced.current_time > row.first.current_time + 1 && row.advanced.frames > row.first.frames && row.paused.paused && row.held.paused &&
      Math.abs(row.held.current_time - row.paused.current_time) <= 0.35 && row.seeks.length === 2 && !row.resumed.paused &&
      row.resumed.current_time > row.seeks[1].current_time + 1 && row.resumed.frames > row.seeks[1].frames, 'episode_ui_incomplete');
  } else if (['mp3', 'flac'].includes(manifest.scenario)) {
    const row = report.audio_flow;
    need(row?.outcome === 'audio_ui_flow_completed' && row.source_container === manifest.scenario && row.playback_evidence?.current_time_delta_seconds > 2 &&
      row.stop_evidence?.all_media_paused_or_removed, 'audio_ui_incomplete');
  } else if (manifest.scenario === 'subtitles') {
    const row = report.subtitle_flow;
    need(row?.outcome === 'external_srt_vtt_ui_selection_and_seek_completed' && row.restored_selection === true, 'subtitle_ui_incomplete');
    for (const track of manifest.catalog.subtitles) {
      need(row.network_subtitles.some(value => value.item_matches_owned_movie && value.subtitle_index === track.index && value.request_method === 'GET' &&
        value.status === 200 && value.body_result === 'owned_subtitle_hashed' && value.bytes > 0 && value.starts_with_webvtt === true && SHA.test(value.sha256)), 'subtitle_delivery_unproven');
      need(physical.exchanges.some(value => value.scope?.kind === 'subtitle' && value.original.method === 'GET' && value.response?.status === 200 &&
        value.complete && value.deliveredBodyBytes > 0 && new RegExp('/Subtitles/' + track.index + '(?:/|$)', 'i').test(value.url.pathname)), 'physical_subtitle_missing');
    }
  } else need(['tv_browse_ui_flow_completed', 'tv_browse_ui_flow_completed_with_cross_season_list'].includes(report.tv_browse?.outcome), 'tv_browse_ui_incomplete');
  const partial = [];
  for (const exchange of physical.exchanges.filter(row => row.scope?.kind === 'media' && row.original.method === 'GET' && !row.complete)) {
    partial.push(explainMediaPartial(exchange, report, physical.logins.find(row => row.tokenHash === exchange.tokenHash)));
  }
  return partial;
}

export function stopPosition(position, duration) {
  need(integer(position) && integer(duration), 'stop_position_invalid'); const p = BigInt(Math.min(position, duration)), d = BigInt(duration);
  if (p <= 0n || d <= 0n) return { position: 0, completed: false };
  if (p >= (90n * d + 99n) / 100n) return { position: 0, completed: true };
  return { position: d < 1200000000n || p < (2n * d + 99n) / 100n ? 0 : Number(p), completed: false };
}

function indexed(rows, key = row => String(row.id)) {
  need(Array.isArray(rows), 'snapshot_rows_missing'); const result = new Map();
  for (const row of rows) { need(own(row) && !result.has(key(row)), 'duplicate_snapshot_row'); result.set(key(row), row); } return result;
}
const referenceKey = row => JSON.stringify([row.user_id, row.auth_session_id, row.application_client_id ?? null, row.device_id, row.client_nonce]);
const userdataKey = row => row.user_id + '/' + row.item_id;
const without = (row, fields) => Object.fromEntries(Object.entries(row).filter(([key]) => !fields.includes(key)));
function timestampNs(value) { const match = /^(.*T\d\d:\d\d:\d\d)(?:\.(\d{1,9}))?(Z|[+-]\d\d:\d\d)$/.exec(value ?? '');
  need(match, 'precise_timestamp_invalid'); return BigInt(instant(match[1] + match[3])) * 1000000n + BigInt((match[2] ?? '').padEnd(9, '0')); }
function within(value, before, after) { const time = timestampNs(value); return time >= timestampNs(before) && time <= timestampNs(after); }
function additions(before, after, key, allowedChanged = new Set()) { const old = indexed(before, key), current = indexed(after, key);
  for (const [id, row] of old) need(current.has(id) && (allowedChanged.has(id) || equal(row, current.get(id))), 'preexisting_source_row_changed');
  return [...current].filter(([id]) => !old.has(id)).map(([, row]) => row); }

export function effectiveStopEvidence(play, stops) {
  const terminal = timestampNs(play.stopped_at), matches = [];
  for (const stop of stops) {
    const begin = timestampNs(stop.exchange.intent.startedAt), end = timestampNs(stop.exchange.result.completedAt);
    need(begin <= end && end >= terminal, 'stop_ack_before_durable_terminal');
    const position = bodyPosition(stop.body, null);
    if (begin <= terminal && integer(position) && Math.min(position, play.duration_ticks) === play.position_ticks) matches.push(stop.exchange.ordinal);
  }
  need(matches.length > 0, 'effective_stop_not_bound_to_sql');
  return matches;
}

export function countedStartEvidence(play, reports) {
  const started = timestampNs(play.started_at), matches = [];
  for (const report of reports) {
    const counts = ['started', 'progress'].includes(report.event) || report.event === 'stopped' && bodyPosition(report.body, play.position_ticks) > 0;
    if (!counts) continue;
    const begin = timestampNs(report.exchange.intent.startedAt), end = timestampNs(report.exchange.result.completedAt);
    if (begin <= started && started <= end) matches.push({ ordinal: report.exchange.ordinal, completedAt: report.exchange.result.completedAt });
  }
  need(matches.length > 0, 'counted_start_not_bound_to_report'); return matches;
}

/** Compare only the current scenario's declared writes; protect every other row. */
export function verifyDurable(before, after, manifest, seed, physical) {
  for (const value of [before, after]) need(exact(value, ['capturedAt', 'tables', 'sequences']) && equal(Object.keys(value.tables).sort(), TABLES), 'source_snapshot_inventory');
  need(instant(before.capturedAt) <= instant(after.capturedAt), 'source_snapshot_order');
  const actor = manifest.actor.id, control = seed.controlQ.id, selected = selectedCandidateItem(manifest), auth = new Map(physical.logins.map(row => [row.sessionId, row]));
  need(control !== actor && !before.tables.play_sessions.some(row => row.user_id === actor) && !before.tables.client_playback_references.some(row => row.user_id === actor), 'scenario_actor_playback_baseline_not_fresh');
  const mutable = new Set(['sessions', 'devices', 'activity_entries', 'play_sessions', 'client_playback_references', 'user_item_data', 'encoding_jobs']);
  for (const table of TABLES) if (!mutable.has(table)) need(equal(before.tables[table], after.tables[table]), 'unowned_table_changed_' + table);
  for (const table of ['sessions', 'play_sessions', 'client_playback_references', 'user_item_data', 'encoding_jobs'])
    need(equal(before.tables[table].filter(row => row.user_id !== actor), after.tables[table].filter(row => row.user_id !== actor)), 'foreign_actor_state_changed_' + table);
  const sessions = additions(before.tables.sessions, after.tables.sessions);
  need(sessions.length === auth.size, 'unexpected_auth_session_count');
  const deviceIds = new Set();
  for (const session of sessions) { const login = auth.get(session.id);
    need(login && session.user_id === actor && session.kind === 'emby' && session.token_hash === '\\x' + login.tokenHash &&
      session.device_id === login.device_id && session.client_name === login.client && session.client_version === login.client_version &&
      session.revoked_at !== null && within(session.created_at, before.capturedAt, after.capturedAt) && within(session.revoked_at, session.created_at, after.capturedAt) &&
      within(session.last_seen_at, session.created_at, after.capturedAt) && instant(session.expires_at) > instant(session.created_at) &&
      session.device_registry_id !== null, 'durable_auth_binding_or_revocation');
    deviceIds.add(String(session.device_registry_id));
  }
  const oldDevices = indexed(before.tables.devices), currentDevices = indexed(after.tables.devices);
  additions(before.tables.devices, after.tables.devices, undefined, deviceIds);
  need([...currentDevices].every(([id]) => oldDevices.has(id) || deviceIds.has(id)), 'unexpected_new_device');
  for (const id of deviceIds) { const row = currentDevices.get(id), scoped = sessions.filter(session => String(session.device_registry_id) === id);
    need(row && row.deleted_at === null && row.last_user_id === actor && scoped.every(session => session.device_id === row.reported_device_id) &&
      within(row.last_seen_at, before.capturedAt, after.capturedAt), 'device_owner_or_activity_changed');
    if (oldDevices.has(id)) need(equal(without(oldDevices.get(id), ['reported_name', 'app_name', 'app_version', 'last_user_id', 'last_seen_at', 'ip_address']),
      without(row, ['reported_name', 'app_name', 'app_version', 'last_user_id', 'last_seen_at', 'ip_address'])), 'device_registry_identity_changed');
    else need(row.custom_name === null && row.revision === 1 && within(row.created_at, before.capturedAt, after.capturedAt), 'new_device_defaults_changed');
  }
  const activity = additions(before.tables.activity_entries, after.tables.activity_entries), activityKeys = new Set();
  need(activity.length === auth.size * 2, 'activity_count_mismatch');
  for (const row of activity) { const key = row.actor_credential_id + '/' + row.action;
    need(auth.has(row.actor_credential_id) && ['session.login', 'session.revoked'].includes(row.action) && !activityKeys.has(key) &&
      row.source === 'emby' && row.severity === 'Info' && row.actor_kind === 'user' && row.actor_id === actor &&
      row.resource_kind === 'session' && row.resource_id === row.actor_credential_id && row.affected_count === 1 &&
      row.request_id === '' && row.observation_fingerprint === '' && row.state === '' && row.revision === 0 && row.previous_revision === 0 &&
      equal(row.changed_fields, []) && within(row.created_at, before.capturedAt, after.capturedAt), 'unexpected_activity_entry'); activityKeys.add(key); }
  const plays = additions(before.tables.play_sessions, after.tables.play_sessions), references = additions(before.tables.client_playback_references, after.tables.client_playback_references, referenceKey);
  const reported = new Map(physical.chains.map(chain => [chain.play.id, chain]));
  const retainedPreparations = [];
  for (const row of plays) { const login = auth.get(row.auth_session_id);
    const mapped = selected ?? manifest.catalog.episodes.find(item => item.id === row.item_id);
    need(login && row.user_id === actor && row.device_id === login.device_id && (row.application_client_id ?? null) === null &&
      row.media_source_id === 'mediasource_' + row.item_id && integer(row.position_ticks) && integer(row.duration_ticks) && row.position_ticks <= row.duration_ticks &&
      mapped?.id === row.item_id && row.duration_ticks === mapped.runtimeTicks &&
      within(row.created_at, before.capturedAt, after.capturedAt) && within(row.updated_at, row.created_at, after.capturedAt), 'new_play_identity_changed');
    const chain = reported.get(row.id);
    if (chain) {
      need(row.state === 'Stopped' && row.counted === true && row.started_at !== null && row.stopped_at !== null &&
        within(row.started_at, row.created_at, after.capturedAt) && within(row.stopped_at, row.started_at, after.capturedAt), 'reported_play_not_terminal');
      chain.effectiveStopOrdinals = effectiveStopEvidence(row, chain.stopped);
      chain.countingReports = countedStartEvidence(row, chain.reports);
    } else {
      need(['Prepared', 'Expired'].includes(row.state) && row.counted === false && row.started_at === null &&
        (row.state === 'Prepared' ? row.stopped_at === null : row.stopped_at !== null && within(row.stopped_at, row.created_at, after.capturedAt)) &&
        (login.infos.some(info => info.itemId === row.item_id && info.value.PlaySessionId === row.id) ||
          universalAudioPreparation(row, login, manifest, after.tables.client_playback_references, Math.max(...physical.exchanges.map(exchange => exchange.ordinal)) + 1)), 'unproven_prepared_residue');
      retainedPreparations.push({ playSessionId: row.id, itemId: row.item_id, state: row.state, counted: false });
    }
  }
  need(reported.size === plays.filter(row => row.counted).length && [...reported.keys()].every(id => plays.some(row => row.id === id)), 'counted_play_without_physical_chain');
  for (const row of references) { const login = auth.get(row.auth_session_id), play = plays.find(value => value.id === row.play_session_id);
    need(login && play && row.user_id === actor && row.device_id === login.device_id && (row.application_client_id ?? null) === null && play.client_correlated === true &&
      play.user_id === row.user_id && play.auth_session_id === row.auth_session_id && play.device_id === row.device_id && (play.application_client_id ?? null) === null &&
      physical.exchanges.some(exchange => exchange.tokenHash === login.tokenHash && ([...(exchange.url?.searchParams ?? [])].some(([key, value]) => key.toLowerCase() === 'playsessionid' && value === row.client_nonce) ||
        reported.get(play.id)?.reports.some(event => event.body.PlaySessionId === row.client_nonce))), 'unproven_client_reference'); }
  const oldData = indexed(before.tables.user_item_data, userdataKey), currentData = indexed(after.tables.user_item_data, userdataKey);
  const affected = new Set(plays.map(row => actor + '/' + row.item_id));
  additions(before.tables.user_item_data, after.tables.user_item_data, userdataKey, affected);
  need([...affected].every(key => currentData.has(key)), 'affected_userdata_missing');
  for (const [key, row] of currentData) {
    if (!affected.has(key)) { need(oldData.has(key) && equal(oldData.get(key), row), 'unrelated_item_userdata_changed'); continue; }
    const old = oldData.get(key) ?? { user_id: actor, item_id: row.item_id, playback_position_ticks: 0, play_count: 0, is_favorite: false, played: false, last_played_at: null };
    const itemPlays = plays.filter(play => play.item_id === row.item_id), counted = itemPlays.filter(play => play.counted), stopped = counted.filter(play => play.state === 'Stopped');
    need(row.user_id === actor && row.is_favorite === old.is_favorite && row.play_count === Math.min(2147483647, old.play_count + counted.length) &&
      within(row.updated_at, before.capturedAt, after.capturedAt), 'userdata_count_or_scope_mismatch');
    if (!counted.length) need(row.playback_position_ticks === old.playback_position_ticks && row.played === old.played && row.last_played_at === old.last_played_at, 'prepare_changed_playback_history');
    else { need(stopped.length === counted.length && row.last_played_at !== null && within(row.last_played_at, before.capturedAt, after.capturedAt), 'counted_play_timestamp_missing');
      const latestStart = counted.reduce((value, play) => timestampNs(play.started_at) > value ? timestampNs(play.started_at) : value, 0n);
      need(timestampNs(row.last_played_at) >= latestStart && counted.filter(play => timestampNs(play.started_at) === latestStart).some(play =>
        reported.get(play.id).countingReports.some(report => timestampNs(row.last_played_at) <= timestampNs(report.completedAt))), 'last_played_not_bound_to_counted_start');
      stopped.sort((left, right) => timestampNs(left.stopped_at) < timestampNs(right.stopped_at) ? -1 : 1);
      const last = stopped.at(-1), normalized = stopPosition(last.position_ticks, last.duration_ticks);
      need(stopped.length === 1 || timestampNs(last.stopped_at) !== timestampNs(stopped.at(-2).stopped_at), 'durable_stop_order_ambiguous');
      need(row.playback_position_ticks === normalized.position && row.played === (old.played || stopped.some(play => stopPosition(play.position_ticks, play.duration_ticks).completed)), 'userdata_stop_policy_mismatch'); }
  }
  const encoding = additions(before.tables.encoding_jobs, after.tables.encoding_jobs);
  for (const row of encoding) need(auth.has(row.auth_session_id) && row.user_id === actor && reported.has(row.play_session_id) && row.item_id === selected?.id &&
    row.device_id === auth.get(row.auth_session_id).device_id && row.media_source_id === 'mediasource_' + row.item_id && ['completed', 'cancelled'].includes(row.state), 'encoding_job_not_owned_and_terminal');
  need(equal(Object.keys(before.sequences).sort(), Object.keys(after.sequences).sort()), 'sequence_inventory_changed');
  for (const [name, old] of Object.entries(before.sequences)) { const current = after.sequences[name];
    if (!['devices_id_seq', 'activity_entries_id_seq'].includes(name)) { need(equal(old, current), 'unowned_sequence_changed'); continue; }
    const expected = name === 'devices_id_seq' ? auth.size : activity.length;
    need(/^[0-9]+$/.test(old.lastValue) && /^[0-9]+$/.test(current.lastValue) && current.isCalled === true &&
      BigInt(current.lastValue) - BigInt(old.lastValue) + (old.isCalled ? 0n : 1n) === BigInt(expected), 'owned_sequence_delta_mismatch');
  }
  if (manifest.scenario === 'movie') {
    const [firstLogin, secondLogin] = physical.logins, firstPlays = firstLogin.chains.map(chain => chain.play)
      .sort((left, right) => timestampNs(left.stopped_at) < timestampNs(right.stopped_at) ? -1 : 1);
    const last = firstPlays.at(-1), position = stopPosition(last.position_ticks, last.duration_ticks).position;
    const expectedCount = Math.min(2147483647, (oldData.get(actor + '/' + selected.id)?.play_count ?? 0) + firstPlays.filter(row => row.counted).length);
    const secondStart = Math.min(...secondLogin.chains.map(chain => chain.started[0].exchange.ordinal));
    const readbacks = physical.exchanges.filter(exchange => exchange.tokenHash === secondLogin.tokenHash && exchange.original?.method === 'GET' &&
      exchange.ordinal > secondLogin.login.ordinal && exchange.ordinal < secondStart && exchange.complete && exchange.response?.status === 200 &&
      new RegExp('^/(?:Users/' + actor + '/)?Items/' + selected.id + '/?$', 'i').test(exchange.scope.route));
    need(readbacks.some(exchange => { const value = completeJSON(exchange, true); return value.Id === selected.id &&
      value.UserData?.PlaybackPositionTicks === position && value.UserData?.PlayCount === expectedCount; }), 'movie_relogin_durable_readback_missing');
  }
  return { sessionsRevoked: sessions.length, countedPlays: plays.filter(row => row.counted).length, retainedPreparations,
    retainedTerminalEncodingJobs: encoding.map(row => ({ id: row.id, state: row.state })), controlQUnchanged: true,
    selectedUserdata: selected ? Object.fromEntries(['item_id', 'playback_position_ticks', 'play_count', 'is_favorite', 'played', 'last_played_at', 'updated_at']
      .map(key => [key, currentData.get(actor + '/' + selected.id)?.[key]])) : null };
}

export function validateRuntimeLineage(epoch, seed, lineage = {}) {
  need([1, 2].includes(epoch.version) && exact(epoch, [...EPOCH_KEYS, ...(epoch.version === 2 ? ENV_EPOCH_KEYS : [])]) &&
    epoch.kind === 'audited-candidate-runtime-epoch' && epoch.status === 'running_awaiting_live_acceptance' && epoch.candidateAdmissionComplete === false &&
    seed.version === epoch.version && exact(seed, [...BINDING_KEYS, ...(seed.version === 2 ? ENV_BINDING_KEYS : [])]) &&
    seed.kind === 'audited-candidate-seed-runtime-binding' && seed.candidateAdmissionComplete === false, 'runtime_lineage_schema');
  need(exact(epoch.currentSource, ['archiveSha256', 'sourceManifest', 'binary', 'fullReport', 'schema']) && epoch.currentSource.schema === 28 &&
    epoch.candidate.bootstrapExecuted === true && equal(epoch.candidate.sourceState, { users: 8, schema: 28, migrations: 28 }) &&
    equal(epoch.candidate.binary, epoch.currentSource.binary) && equal(epoch.candidate.currentSourceManifest, epoch.currentSource.sourceManifest), 'runtime_lineage_product');
  if (epoch.version === 1) { need(equal(epoch.calls, { stop: 1, replace: 1, start: 1 }) && seed.currentSessions.length === 3, 'binary_epoch_calls_or_sessions'); return; }
  const { previousEpoch: previous, previousBinding, configurationInput, failedAdmission03 } = lineage, change = epoch.configurationChange;
  need(previous?.version === 1 && previousBinding?.version === 1, 'environment_predecessor_missing');
  validateRuntimeLineage(previous, previousBinding);
  need(epoch.operationKind === 'environment_revision' && equal(epoch.calls, { stop: 1, replaceEnvironment: 1, start: 1 }) &&
    epoch.previousEpoch.sha256 === '7bcdbc529fd1ba3f6a62f66585e6788cc9efa1aac22a4accc8d339d69ccf6ae2' &&
    seed.previousBinding.sha256 === 'e00c5a3ae5bf45da5f774b2f71cbecda9150ee876cba082c7fc23a42616db552' &&
    equal(previousBinding.runtimeEpoch, epoch.previousEpoch) && equal(epoch.productInput, previous.transitionInput) &&
    equal(epoch.currentSource, previous.currentSource) && equal(epoch.helpers, previous.helpers) && equal(epoch.postgresProcess, previous.postgresProcess) &&
    equal(epoch.originalProvision, previous.originalProvision) && equal(epoch.seedProvenance, previous.seedProvenance), 'environment_product_lineage_changed');
  need(exact(change, ['before', 'after', 'preservedCopy', 'additions', 'requiredFreeBytes', 'observedFreeBytesBefore']) &&
    [change.before, change.after, change.preservedCopy].every(descriptor) && equal(change.before, previous.candidate.runtime) &&
    change.before.path === change.after.path && change.preservedCopy.sha256 === change.before.sha256 && change.after.sha256 !== change.before.sha256 &&
    equal(change.additions, ENV_ADDITIONS) && change.requiredFreeBytes === 234946560 && integer(change.observedFreeBytesBefore) && change.observedFreeBytesBefore >= change.requiredFreeBytes &&
    equal(epoch.candidate.runtime, change.after) && equal(epoch.candidate.input, epoch.transitionInput) && equal(epoch.candidate.productInput, epoch.productInput), 'environment_configuration_changed');
  const candidateChanges = ['runtime', 'input', 'productInput', 'processes', 'serverIdentity', 'listener'];
  need(equal(without(epoch.candidate, candidateChanges), without(previous.candidate, candidateChanges)) &&
    equal(epoch.candidate.processes.postgres, previous.candidate.processes.postgres), 'environment_changed_unrelated_candidate_fields');
  need(configurationInput?.kind === 'audited-candidate-environment-revision-input' && configurationInput.version === 1 &&
    equal(configurationInput.previousEpoch, epoch.previousEpoch) && equal(configurationInput.previousSeedBinding, seed.previousBinding) &&
    equal(configurationInput.runtimeHelper, epoch.runtimeHelper) && equal(configurationInput.additions, ENV_ADDITIONS), 'environment_input_binding');
  for (const key of ['admission03', 'failureCloseout', 'closedState']) need(equal(configurationInput[key], seed[key]), 'environment_history_cross_binding');
  need(seed.admission03.sha256 === 'a59638abe364b89e9c44482986698d0b1d310147178cbbe2ab4a0c1412c29f97' &&
    seed.failureCloseout.sha256 === '090d04421c8fb847822695143483b153f096e2707571c14a01d4860d48233e2a' &&
    seed.closedState.sha256 === '9c2a6660fda9f2da5c33df8c93fbbc4786c397d158755d1de3712eda6f5e01d0' &&
    equal(without(seed, ['version', 'runtimeEpoch', 'currentSessions', ...ENV_BINDING_KEYS]), without(previousBinding, ['version', 'runtimeEpoch', 'currentSessions'])), 'environment_seed_provenance_changed');
  need(failedAdmission03?.kind === 'audited-candidate-live-admission' && failedAdmission03.status === 'admission_failed_resources_retained' &&
    equal(failedAdmission03.runtimeEpoch, epoch.previousEpoch) && equal(failedAdmission03.cleanupFailures, []) &&
    failedAdmission03.playbackRequests === 0 && failedAdmission03.applyRequests === 0 && failedAdmission03.rollbackRequests === 0 &&
    exact(failedAdmission03.controllerSessions, ['admin', 'P', 'Q']), 'environment_failed_admission_history');
  const added = ['admin', 'P', 'Q'].map(role => { const row = failedAdmission03.controllerSessions[role]; need(row.sameTokenRejected === true, 'environment_controller_session_not_revoked');
    return { kind: role === 'admin' ? 'admin' : 'emby', tokenSha256: row.tokenSha256, credentialId: row.credentialId }; });
  need(equal(seed.currentSessions, [...previousBinding.currentSessions, ...added]) && seed.currentSessions.length === 6, 'environment_seed_session_history');
}

export function validateCloseoutBindings(input, evidence) {
  const { manifest, observation, summary, gatewayAttestation: gateway, runtimeEpoch: epoch, admission, seedBinding: seed, boundary } = evidence;
  validateCandidateManifest(manifest);
  need(equal(observation.gateway?.admission_request_counts, { normal: 0, cleanup: 0 }), 'gateway_not_fresh_at_client_admission');
  validateCandidateGateway(gateway, manifest, ns(observation.started_monotonic_ns), { normal: 0, cleanup: 0 });
  validateRuntimeLineage(epoch, seed, evidence.lineage);
  need(admission.kind === 'audited-candidate-live-admission' && admission.version === 2 && admission.status === 'admitted_for_core_client' &&
    admission.candidateAdmissionComplete === true && admission.failure === null && equal(admission.cleanupFailures, []) &&
    equal(admission.runtimeEpoch, input.runtimeEpoch) && equal(admission.seedRuntimeBinding, input.seedBinding) && equal(admission.currentSource, epoch.currentSource), 'candidate_live_admission_binding');
  const currentSource = { manifestSha256: epoch.currentSource.sourceManifest.sha256, binarySha256: epoch.currentSource.binary.sha256, schema: epoch.currentSource.schema };
  need(equal(manifest.source, currentSource) && equal(without(manifest.processes.candidate, ['listener']), epoch.candidateProcess) &&
    manifest.processes.candidate.listener.port === epoch.candidate.listener.port && manifest.processes.candidate.listener.socketInode === epoch.candidate.listener.socketInode,
  'candidate_current_epoch_mismatch');
  need(equal(seed.runtimeEpoch, input.runtimeEpoch) &&
    equal(seed.originalSeed, epoch.seedProvenance) && seed.serverId === manifest.serverId && seed.actors[manifest.scenario].id === manifest.actor.id &&
    seed.actors[manifest.scenario].username === manifest.actor.username && equal(seed.actors[manifest.scenario].credentials, manifest.credentials), 'seed_actor_or_epoch_mismatch');
  need(Object.entries(manifest.catalog).every(([key, value]) => Object.hasOwn(seed.catalog, key) && equal(value, seed.catalog[key])), 'manifest_catalog_not_seeded_mapping');
  need(observation.kind === 'audited-candidate-client-report' && observation.version === 1 && observation.run_id === manifest.runId && observation.scenario === manifest.scenario &&
    observation.manifest_sha256 === input.manifest.sha256 && equal(observation.source, manifest.source) && summary.private_report_sha256 === input.observation.sha256 &&
    summary.run_id === manifest.runId && summary.scenario === manifest.scenario && summary.outcome === observation.outcome && summary.failure === observation.failure,
  'client_report_binding');
  need(gateway.kind === 'core-av-client-gateway' && gateway.schemaVersion === 1 && gateway.runId === manifest.runId && gateway.browserOrigin === manifest.browserOrigin &&
    gateway.proxyOrigin === manifest.browserOrigin && gateway.directOrigin === manifest.directOrigin && equal(manifest.gatewayAttestation, input.gatewayAttestation) &&
    equal(gateway.process, without(manifest.processes.gateway, ['listener'])) && equal(gateway.listener, manifest.processes.gateway.listener) &&
    equal(gateway.upstreams.goby.process, epoch.candidateProcess) && equal(gateway.upstreams.goby.listener, manifest.processes.candidate.listener) &&
    gateway.upstreams.goby.executableSha256 === currentSource.binarySha256 && equal(gateway.sources.gateway, input.sources.gateway) && equal(gateway.sources.proxy, input.sources.proxy) &&
    gateway.policy.version === 'core-av-transparent-gateway-v1' && equal(gateway.policy.allowedOrigins, [manifest.browserOrigin]) &&
    observation.gateway.attestation_sha256 === input.gatewayAttestation.sha256 && observation.gateway.input_sha256 === gateway.inputSha256, 'gateway_runtime_or_policy_binding');
  need(exact(boundary, ['kind', 'version', 'runId', 'runtimeEpoch', 'sourceBefore', 'sourceAfter', 'candidateBefore', 'candidateAfter', 'postgresBefore', 'postgresAfter',
    'leaseBefore', 'leaseAfter', 'database', 'beforeMonotonicNs', 'afterMonotonicNs', 'clientWorker', 'gatewayWorker']) &&
    boundary.kind === 'audited-candidate-client-boundary' && boundary.version === 1 && boundary.runId === manifest.runId &&
    equal(boundary.runtimeEpoch, input.runtimeEpoch) && equal(boundary.sourceBefore, input.sourceBefore) && equal(boundary.sourceAfter, input.sourceAfter) &&
    equal(boundary.candidateBefore, manifest.processes.candidate) && equal(boundary.candidateAfter, manifest.processes.candidate) &&
    equal(boundary.postgresBefore, epoch.postgresProcess) && equal(boundary.postgresAfter, epoch.postgresProcess) &&
    equal(boundary.leaseBefore, epoch.lease) && equal(boundary.leaseAfter, epoch.lease) && equal(boundary.database, epoch.candidate.database), 'source_capture_epoch_binding');
  need(exact(boundary.clientWorker, ['exitCode', 'mainPID', 'workerPidAbsent', 'remainingBrowserPids']) && exact(boundary.gatewayWorker, ['exitCode', 'mainPID', 'workerPidAbsent', 'index']) &&
    boundary.clientWorker.exitCode === 0 && boundary.clientWorker.mainPID === 0 && boundary.clientWorker.workerPidAbsent === true &&
    equal(boundary.clientWorker.remainingBrowserPids, []) && boundary.gatewayWorker.exitCode === 0 && boundary.gatewayWorker.mainPID === 0 &&
    boundary.gatewayWorker.workerPidAbsent === true && equal(boundary.gatewayWorker.index, input.gatewayIndex), 'workers_not_independently_closed');
  need(ns(boundary.beforeMonotonicNs) <= ns(observation.started_monotonic_ns) &&
    ns(boundary.afterMonotonicNs) >= ns(observation.started_monotonic_ns) + BigInt(observation.elapsed_ms) * 1000000n, 'boundary_time_interval');
}

export function closeEvidence(input, evidence, ledgerRows) {
  validateCloseoutBindings(input, evidence);
  const manifest = evidence.manifest, report = evidence.observation;
  const exchanges = verifyLedgerRows(evidence.gatewayAttestation, evidence.gatewayIndex, ledgerRows, manifest);
  need(exchanges.length > 0 && exchanges.every(row => ns(row.intent.startedMonotonicNs) >= ns(evidence.boundary.beforeMonotonicNs) &&
    ns(row.result.completedMonotonicNs) <= ns(evidence.boundary.afterMonotonicNs)), 'ledger_outside_capture_interval');
  const physical = analyzePhysical(exchanges, manifest, report, evidence.sourceAfter), partialMedia = verifyUI(report, manifest, physical);
  const partialOrdinals = new Set(partialMedia.map(row => row.ordinal));
  for (const row of exchanges) if (!row.rejected && !row.complete && row.request.kind !== 'websocket')
    need(partialOrdinals.has(row.ordinal) || row.request.kind === 'web' && row.original.method === 'GET' && matchesContext(row, report).some(event => event.failed && event.failure_error_text === 'net::ERR_ABORTED'), 'unexplained_upstream_interruption');
  const durable = verifyDurable(evidence.sourceBefore, evidence.sourceAfter, manifest, evidence.seedBinding, physical);
  return { kind: 'audited-candidate-client-closeout', version: 1, status: 'core_scenario_closed', runId: manifest.runId, scenario: manifest.scenario,
    source: manifest.source, physicalRequests: exchanges.length, logins: physical.logins.length,
    lifecycles: physical.chains.map(chain => ({ playSessionId: chain.play.id, authSessionId: chain.login.sessionId, itemId: chain.play.item_id,
      mediaSourceId: chain.play.media_source_id, preparation: chain.preparation, startedOrdinals: chain.started.map(row => row.exchange.ordinal), progressOrdinals: chain.progress.map(row => row.exchange.ordinal),
      stoppedOrdinals: chain.stopped.map(row => row.exchange.ordinal), effectiveStopOrdinals: chain.effectiveStopOrdinals,
      countingReportOrdinals: chain.countingReports.map(row => row.ordinal), mediaGetOrdinals: chain.media.map(row => row.ordinal) })),
    logoutVerification: physical.logins.map(login => ({ authSessionId: login.sessionId, loginOrdinal: login.login.ordinal, logoutOrdinal: login.logout.ordinal,
      rejectionOrdinal: login.verification.ordinal, rejectionStatus: 401, rejectionBodyReconstructed: login.verification.responseEntity !== null })),
    partialMedia, durable, auxiliaryRejections: exchanges.filter(row => row.response?.status >= 400 && row.scope?.kind !== 'login' && row.original.get('user-agent') !== 'GobyBrowserPostLogoutVerification/1')
      .map(row => ({ ordinal: row.ordinal, kind: row.scope?.kind ?? null, status: row.response.status })),
    rejectedBeforeUpstream: exchanges.filter(row => row.rejected && !row.handshake).length,
    physicalCounting: 'One gateway ordinal per admitted physical exchange; frame and Service Worker observations are not added to this count.',
    limitations: ['Original-client core scenario only; no NextUp or automatic-refresh claim.', 'Excluded web/media/WebSocket bodies are not reconstructed or hashed.',
      'Audio source-container and HTMLMediaElement advancement do not prove audible output or the output codec.'], evidence: without(input, ['kind', 'version', 'output']) };
}

async function protectedDirectory(directory, privateMode = false) {
  need(path.posix.isAbsolute(directory) && !directory.split('/').includes('..'), 'directory_path_invalid');
  for (let current = directory;; current = path.dirname(current)) { const info = await fs.lstat(current);
    need(info.isDirectory() && !info.isSymbolicLink() && info.uid === 0 && !(info.mode & 0o022), 'directory_not_protected');
    if (current === path.dirname(current)) break; }
  if (privateMode) need(((await fs.stat(directory)).mode & 0o777) === 0o700, 'directory_not_private');
}

async function readPin(pin, maximum = MAX_FILE, privateMode = true) {
  need(descriptor(pin), 'descriptor_invalid'); await protectedDirectory(path.dirname(pin.path));
  const handle = await fs.open(pin.path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try { const before = await handle.stat({ bigint: true });
    need(before.isFile() && before.uid === 0n && before.nlink === 1n && before.size > 0n && before.size <= BigInt(maximum) &&
      (privateMode ? (before.mode & 0o777n) === 0o600n : !(before.mode & 0o022n)), 'evidence_file_metadata');
    const bytes = await handle.readFile(), after = await handle.stat({ bigint: true }), current = await fs.lstat(pin.path, { bigint: true });
    need(['dev', 'ino', 'size', 'mtimeNs', 'ctimeNs', 'mode'].every(key => before[key] === after[key] && before[key] === current[key]) && sha(bytes) === pin.sha256, 'evidence_file_changed');
    return bytes;
  } finally { await handle.close(); }
}

async function saveNew(filename, value) {
  const bytes = Buffer.from(JSON.stringify(value) + '\n'), handle = await fs.open(filename, 'wx', 0o600);
  try { await handle.writeFile(bytes); await handle.sync(); } finally { await handle.close(); }
  const directory = await fs.open(path.dirname(filename), constants.O_RDONLY | constants.O_DIRECTORY);
  try { await directory.sync(); } finally { await directory.close(); }
  return { path: filename, sha256: sha(bytes) };
}

export async function runCloseout(inputPin) {
  need(process.platform === 'linux' && process.getuid?.() === 0 && process.env.SSH_CONNECTION, 'remote_only_closeout'); process.umask(0o077);
  const input = strictJSON(await readPin(inputPin));
  const names = ['manifest', 'observation', 'summary', 'gatewayAttestation', 'gatewayIndex', 'runtimeEpoch', 'admission', 'seedBinding', 'sourceBefore', 'sourceAfter', 'boundary'];
  need(exact(input, ['kind', 'version', ...names, 'sources', 'output']) && input.kind === 'audited-candidate-client-closeout-input' && input.version === 1 &&
    names.every(key => descriptor(input[key])) && exact(input.sources, Object.keys(SOURCE_FILES)) && typeof input.output === 'string' && input.output.startsWith('/opt/goby-test/'), 'closeout_input_invalid');
  const ownDirectory = path.dirname(fileURLToPath(import.meta.url));
  for (const [key, filename] of Object.entries(SOURCE_FILES)) { need(path.basename(input.sources[key].path) === filename &&
    (!filename.endsWith('.mjs') || input.sources[key].path === path.join(ownDirectory, filename)), 'closeout_source_path'); await readPin(input.sources[key], MAX_FILE, false); }
  await protectedDirectory(input.output, true); need((await fs.readdir(input.output)).length === 0, 'fresh_closeout_output_required');
  const evidence = {}; for (const key of names) evidence[key] = strictJSON(await readPin(input[key]));
  if (evidence.runtimeEpoch.version === 2) {
    const epoch = evidence.runtimeEpoch, seed = evidence.seedBinding;
    evidence.lineage = { previousEpoch: strictJSON(await readPin(epoch.previousEpoch)), previousBinding: strictJSON(await readPin(seed.previousBinding)),
      configurationInput: strictJSON(await readPin(epoch.transitionInput)), failedAdmission03: strictJSON(await readPin(seed.admission03)) };
    await readPin(epoch.productInput); await readPin(seed.failureCloseout); await readPin(seed.closedState);
  }
  const gateway = evidence.gatewayAttestation, index = evidence.gatewayIndex;
  need(Array.isArray(index.entries) && index.entries.length <= 10000, 'ledger_inventory_limit');
  need(input.gatewayAttestation.path === path.join(gateway.ledgerRoot, 'gateway-attestation.json') && input.gatewayIndex.path === path.join(gateway.ledgerRoot, 'index.json'), 'ledger_paths_not_bound');
  await protectedDirectory(gateway.ledgerRoot, true); await protectedDirectory(path.join(gateway.ledgerRoot, 'private'), true);
  need(equal((await fs.readdir(gateway.ledgerRoot)).sort(), ['gateway-attestation.json', 'index.json', 'private']), 'ledger_root_membership');
  const expected = [], rows = [];
  for (const entry of index.entries) { const prefix = 'request-' + String(entry.ordinal).padStart(6, '0');
    for (const name of ['intent', 'result']) { const filename = prefix + '-' + name + '.json'; need(entry[name]?.path === path.join(gateway.ledgerRoot, 'private', filename), 'ledger_entry_path'); expected.push(filename); }
    rows.push({ entry, intent: strictJSON(await readPin(entry.intent)), result: strictJSON(await readPin(entry.result)) }); }
  need(equal((await fs.readdir(path.join(gateway.ledgerRoot, 'private'))).sort(), expected.sort()), 'ledger_private_membership');
  let result;
  try { result = closeEvidence(input, evidence, rows); }
  catch (error) { result = { kind: 'audited-candidate-client-closeout', version: 1, status: 'incomplete', runId: evidence.manifest?.runId ?? null,
    scenario: evidence.manifest?.scenario ?? null, failedCheck: /^[a-z0-9_]+$/.test(error.message ?? '') ? error.message : 'evidence_validation_failed', input: inputPin, clientAcceptanceClaim: false }; }
  // The output deliberately contains only projections and pinned references.
  result.input = inputPin; result.completedAt = new Date().toISOString();
  const output = await saveNew(path.join(input.output, 'closeout.json'), result);
  const summary = { status: result.status, runId: result.runId, scenario: result.scenario, failedCheck: result.failedCheck ?? null, output,
    physicalRequests: result.physicalRequests ?? null, logins: result.logins ?? null, lifecycles: result.lifecycles?.length ?? null, sqlCalls: 0, httpCalls: 0, serviceCalls: 0 };
  await saveNew(path.join(input.output, 'summary.json'), summary); return summary;
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  try { const args = process.argv.slice(2); need(args.length === 4 && args[0] === '--input' && args[2] === '--input-sha256', 'closeout_arguments');
    const summary = await runCloseout({ path: args[1], sha256: args[3] }); process.stdout.write(JSON.stringify(summary) + '\n'); if (summary.status !== 'core_scenario_closed') process.exitCode = 1;
  } catch (error) { process.stderr.write(JSON.stringify({ status: 'incomplete', failedCheck: /^[a-z0-9_]+$/.test(error.message ?? '') ? error.message : 'closeout_input_or_receipt_unavailable',
    sqlCalls: 0, httpCalls: 0, serviceCalls: 0 }) + '\n'); process.exitCode = 1; }
}
