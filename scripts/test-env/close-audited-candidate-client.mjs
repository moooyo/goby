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
const RETAINED_ROOT = '/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14';
export const RETAINED_BASELINE = { path: RETAINED_ROOT + '/candidate-core-movie04-failure-closeout.json', sha256: '5843a790e3c4ba09109d145b64fbda58f94c7ee94092e898ab584bab37880e8d' };
export const RETAINED_SNAPSHOT = { path: RETAINED_ROOT + '/candidate-core-client-movie-04/private/source-after.json', sha256: '7209b3845d8290cd3883c76f5a95f00066d85d61103b8c5765493472196ad01c' };
export const RETAINED_PLAY = 'play_40969548543a02735b847a89bc34671b';
export const RETAINED_AUTH = 'aadd2636638e28cc6ceaf5bb8f2cb132';
export const OCCUPIED_BASELINE = { path: RETAINED_ROOT + '/candidate-core-movie05-owned-state-closeout.json', sha256: '7c38d00bcb4ece5e056e06a22be848cbd5e76c77f7d5b642d06f656be3dc27ab' };
export const OCCUPIED_SNAPSHOT = { path: RETAINED_ROOT + '/candidate-core-client-movie-05/private/source-after.json', sha256: 'be0dbd70d4f7ea7d4343a3ea6259f80be216b9239e7acfa6552b2c7858b33bb0' };
export const OCCUPIED_PLAY = 'play_b2977a52015916374e8a8af16188c191';
export const OCCUPIED_AUTH = 'e5649a6dc8451164243a4213f12d0ba2';
export const CANDIDATE_LOG = '/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b/private/server-unit.log';
const RETAINED_EPOCH = { path: RETAINED_ROOT + '/candidate-backup-limits-revision-01/private/runtime-epoch.json', sha256: '72e25f907619fbdf82879070c6fce6178cc8c7881e8015a99991f62e64a2a73e' };
export const PREVIOUS_BINARY_EPOCH = { path: RETAINED_ROOT + '/candidate-cancellation-transition-01/private/runtime-epoch.json', sha256: '7bcdbc529fd1ba3f6a62f66585e6788cc9efa1aac22a4accc8d339d69ccf6ae2' };
export const BINARY_SUCCESSOR = {
  previousEpoch: RETAINED_EPOCH,
  previousBinding: { path: RETAINED_ROOT + '/candidate-backup-limits-revision-01/private/seed-runtime-binding.json', sha256: '92ee92478475390e514f39e554619f322be81062a6d0256820c00bf4c8e0969f' },
  reviewedSummary: { path: RETAINED_ROOT + '/candidate-tv-parent-transition-state-review-01/summary.json', sha256: 'be2bd9a3081c9f84fb71ed617d2d3a025415ee2faf762d89e512e26bcd0dff18' },
  reviewedState: { path: RETAINED_ROOT + '/candidate-tv-parent-transition-state-review-01/private/state.json', sha256: '82cabde1a8d8dcf73a0e19da5b7c680fd977cace56d0d597bac0c59513b170a6' },
  priorCloseout: { path: RETAINED_ROOT + '/candidate-core-tv-browse01-owned-state-closeout.json', sha256: 'b9cbc7ad57f7381c1c6c8d267827cb24a907376a17944fe5b3bd5d8dcdd3f4a0' },
  priorSource: { path: RETAINED_ROOT + '/candidate-core-client-tv-browse-01/private/source-after.json', sha256: '541dc489d1592612485aa4e18955cec3adac3cbb8070b1405d4af8f6818a92a6' },
  fullRoot: '/opt/goby-test/audit-fixes-20260913-20260913T141732Z-a393812c3356',
  archiveSha256: 'b363afdcf707471c3a95288d04441bb7be89699010b09ca89c4c783e10436177',
  sourceManifest: { path: '/opt/goby-test/audit-fixes-20260913-20260913T141732Z-a393812c3356/source-manifest.json', sha256: 'bce4d22a4c51dacca4660a6c8e8e3fac816141cd612a7b32b87367799e495cff' },
};
export const REUSED_ADMISSION04 = { path: RETAINED_ROOT + '/candidate-live-admission-04/private/report.json', sha256: '05083c7cc5c65c62e30018136b6a7d383c144c9742d96eaecc52e9c2cfc19653' };
export const TV_POST_BROWSE_ADMISSION = { path: RETAINED_ROOT + '/candidate-live-admission-05/private/report.json', sha256: 'b73a2d30926c68886bd1674a356e6330eab2072afb53fb1fa1f695c5337f8535' };
const PUBLIC_ADMISSION03_CLOSEOUT = { path: RETAINED_ROOT + '/candidate-admission03-failure-closeout.json', sha256: '090d04421c8fb847822695143483b153f096e2707571c14a01d4860d48233e2a' };
export const AFFECTED_TV_TRANSITION_CLOSEOUT = { path: RETAINED_ROOT + '/candidate-tv-parent-transition-closeout.json', sha256: 'c9e03c008d0d1dbf0b66b8070692738e50a2ca57c29f89cbd40c1fba38d597ba' };
const AFFECTED_TV_CHECKS = ['runtimeIdentity', 'tvDefaultParents', 'tvDetailParents', 'ordinaryAuthorization', 'healthWindow60Seconds', 'sourceAndInactivePreserved', 'sessionCleanup'];
const REUSED_ADMISSION_CONTRACTS = ['native_authentication_and_query_carriers', 'storage_and_library_access', 'backup_create_download', 'restore_ready_cancel_retained_stage'];
const CLOSEOUT_PINS = ['manifest', 'observation', 'summary', 'gatewayAttestation', 'gatewayIndex', 'runtimeEpoch', 'admission', 'seedBinding', 'sourceBefore', 'sourceAfter', 'boundary'];
const TABLES = 'activity_entries application_key_clients application_key_devices application_keys catalog_entities client_playback_references devices encoding_jobs extra_reserved_paths item_entities item_extra_resources item_images item_metadata_state item_subtitles item_theme_resources items libraries library_roots managed_settings play_sessions scan_jobs schema_migrations server_settings sessions task_definitions task_occurrences task_run_children task_run_requests task_runs task_triggers theme_owner_ids theme_reserved_paths user_item_data user_settings users'.split(' ').sort();
const SOURCE_FILES = { closer: 'close-audited-candidate-client.mjs', adapter: 'client-browser-audited-candidate.mjs', gateway: 'client-acceptance-gateway.py', proxy: 'client-acceptance-proxy.py',
  sessionProof: 'client-browser-session-proof.mjs', movie: 'client-browser-playback.mjs', audio: 'client-browser-audio-flow.mjs', subtitles: 'client-browser-subtitle-flow.mjs', tv: 'client-browser-tv-flow.mjs' };
const EPOCH_KEYS = 'kind version status transitionInput transitionHelper runtimeHelper originalProvision seedProvenance currentSource candidate candidateProcess postgresProcess lease before after preservation calls helpers candidateAdmissionComplete'.split(' ');
const BINDING_KEYS = 'kind version runtimeEpoch originalSeed seedExecutor seedInput seedSessionAddendum admission02 serverId admin actors controlQ catalog catalogFile actualCatalogDtos libraries roots resources seedCleanup currentSessions candidateAdmissionComplete'.split(' ');
const ENV_EPOCH_KEYS = ['previousEpoch', 'productInput', 'operationKind', 'configurationChange'];
const ENV_BINDING_KEYS = ['previousBinding', 'admission03', 'failureCloseout', 'closedState'];
const SUCCESSOR_EPOCH_KEYS = ['previousEpoch', 'productInput', 'configurationInput', 'operationKind', 'reviewedState', 'reviewedSummary'];
const SUCCESSOR_BINDING_KEYS = ['previousBinding', 'reviewedSummary', 'reviewedState', 'priorCloseout', 'priorSource'];
const SUCCESSOR_INPUT_KEYS = 'kind version output previousEpoch previousBinding reviewedSummary reviewedState priorCloseout priorSource newFullReport newSourceManifest newBinary compiledCatalog frontendReport helpers budgets'.split(' ');
const SUCCESSOR_LIMITS = { maximumSeconds: 900, stopSeconds: 60, readySeconds: 60, maximumPublicRequests: 10, stopCalls: 1, replaceCalls: 1, startCalls: 1 };
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
function scanJSON(bytes, maximum, int64At = () => null) {
  need(Buffer.isBuffer(bytes) && bytes.length > 0 && bytes.length <= maximum, 'json_size_limit');
  const source = new TextDecoder('utf-8', { fatal: true }).decode(bytes); let index = 0, nodes = 0;
  const replacements = [], scalarPattern = /^(?:true|false|null|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)/;
  const space = () => { while (index < source.length && /[\t\r\n ]/.test(source[index])) index++; };
  const string = () => { const start = index++; need(source[start] === '"', 'json_string_required');
    while (index < source.length) { const character = source[index++]; if (character === '"') return JSON.parse(source.slice(start, index)); if (character === '\\') index++; }
    throw new Error('json_unterminated_string'); };
  const value = (depth, location) => {
    need(depth <= 40 && ++nodes <= 500000, 'json_complexity_limit'); space();
    const minimum = int64At(location);
    if (minimum !== null) {
      const scalar = scalarPattern.exec(source.slice(index)), token = scalar?.[0];
      need(typeof token === 'string' && /^-?(?:0|[1-9]\d*)$/.test(token), 'json_int64_numeric_token_required');
      need(token.length <= 20, 'json_int64_range');
      const integer = BigInt(token);
      need(integer >= minimum && integer <= 9223372036854775807n, 'json_int64_range');
      replacements.push({ start: index, end: index + token.length, value: JSON.stringify(integer.toString()) });
      index += token.length; return;
    }
    if (source[index] === '{') { index++; space(); const keys = new Set(); if (source[index] === '}') { index++; return; }
      for (;;) { space(); const key = string(); need(!keys.has(key), 'json_duplicate_key'); keys.add(key); space(); need(source[index++] === ':', 'json_colon_required'); value(depth + 1, [...location, key]); space(); const next = source[index++]; if (next === '}') return; need(next === ',', 'json_comma_required'); } }
    if (source[index] === '[') { index++; space(); if (source[index] === ']') { index++; return; }
      let position = 0;
      for (;;) { value(depth + 1, [...location, position++]); space(); const next = source[index++]; if (next === ']') return; need(next === ',', 'json_comma_required'); } }
    if (source[index] === '"') { string(); return; }
    const scalar = scalarPattern.exec(source.slice(index)); need(scalar, 'json_scalar_invalid');
    if (/^-?\d/.test(scalar[0])) { const number = Number(scalar[0]); need(Number.isFinite(number) && (!Number.isInteger(number) || Number.isSafeInteger(number)), 'json_unsafe_number'); }
    index += scalar[0].length;
  };
  value(0, []); space(); need(index === source.length, 'json_trailing_bytes');
  let cursor = 0; const pieces = [];
  for (const replacement of replacements) { pieces.push(source.slice(cursor, replacement.start), replacement.value); cursor = replacement.end; }
  pieces.push(source.slice(cursor)); return JSON.parse(pieces.join(''));
}

/** Generic and public/API JSON keep their original safe-number requirement. */
export function strictJSON(bytes, maximum = MAX_FILE) { return scanJSON(bytes, maximum); }

/** Preserve stored int64 file metadata as canonical decimal strings in memory. */
export function sourceSnapshotJSON(bytes, maximum = MAX_FILE) {
  return scanJSON(bytes, maximum, location => {
    if (location[0] !== 'tables' || typeof location[2] !== 'number') return null;
    if (location.length === 4 && location[1] === 'item_subtitles' && location[3] === 'change_time_ns') return 0n;
    if (location.length === 5 && location[1] === 'items' && location[3] === 'media' && location[4] === 'FileChangeTimeNs') return -9223372036854775808n;
    return null;
  });
}

/** Only the fixed predecessor role may contain these two legacy stat fields. */
export function previousBinaryEpochJSON(bytes, pin, maximum = MAX_FILE) {
  need(equal(pin, PREVIOUS_BINARY_EPOCH), 'previous_binary_epoch_reader_authority');
  return scanJSON(bytes, maximum, location => location.length === 3 && location[0] === 'preservation' && location[1] === 'oldBinaryFacts' &&
    ['ctimeNs', 'mtimeNs'].includes(location[2]) ? -9223372036854775808n : null);
}

export function runtimeEpochJSON(bytes, pin, maximum = MAX_FILE) {
  need(descriptor(pin), 'runtime_epoch_reader_authority');
  return equal(pin, PREVIOUS_BINARY_EPOCH) ? previousBinaryEpochJSON(bytes, pin, maximum) : strictJSON(bytes, maximum);
}

const PLAYBACK_REPORT_ROUTE = /^\/Sessions\/Playing(?:\/(?:Progress|Stopped))?\/?$/i;
function bodyMediaType(raw, allowed) {
  need(typeof raw === 'string', 'request_content_type_required');
  const match = /^\s*([a-z0-9!#$&^_.+-]+\/[a-z0-9!#$&^_.+-]+)\s*(?:;\s*charset\s*=\s*(?:"utf-8"|utf-8)\s*)?$/i.exec(raw);
  need(match && allowed.includes(match[1].toLowerCase()), 'request_content_type_unsupported');
  return match[1].toLowerCase();
}

function playbackJSON(bytes) {
  const value = scanJSON(bytes, MAX_BODY, location => location.length === 1 && location[0] === 'PlaybackStartTimeTicks' ? -9223372036854775808n : null);
  need(own(value), 'playback_body_not_object'); return value;
}

/** Only actual report-route observations may contain this opaque int64 hint. */
export function observationJSON(bytes, manifest, maximum = MAX_FILE) {
  const rows = new Set();
  const value = scanJSON(bytes, maximum, location => {
    if (location.length === 4 && location[0] === 'requests' && typeof location[1] === 'number' && location[2] === 'body' && location[3] === 'PlaybackStartTimeTicks') {
      rows.add(location[1]); return -9223372036854775808n;
    }
    return null;
  });
  for (const index of rows) {
    const row = value.requests[index], scope = candidateRequestScope(row.url, row.method, manifest);
    need(row.method === 'POST' && row.allowed === true && row.origin === 'target' && ['frame', 'service_worker'].includes(row.scope) &&
      row.kind === 'playback_report' && scope.allowed === true && scope.kind === 'playback_report' && row.route === scope.route &&
      PLAYBACK_REPORT_ROUTE.test(scope.route), 'observation_playback_int64_scope');
    bodyMediaType(row.headers?.['content-type'], ['application/json', 'text/plain']);
    const payload = bytes64(row.payload_base64);
    need(equal(row.body, playbackJSON(payload)), 'observation_playback_body_payload_mismatch');
  }
  return value;
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

function requestEntity(exchange) {
  need(exchange.complete && exchange.result.bodyEvidenceComplete === true, 'critical_api_not_complete');
  need(Buffer.isBuffer(exchange.requestEntity) && exchange.requestEntity.length > 0 && exchange.requestEntity.length <= MAX_BODY, 'critical_request_body_missing');
  return exchange.requestEntity;
}

function bodyRoute(exchange, kind, route) {
  need(exchange.request?.kind === 'api' && exchange.original?.method === 'POST' && exchange.scope?.allowed === true && exchange.scope.kind === kind &&
    typeof exchange.scope.route === 'string' && route.test(exchange.scope.route) && exchange.url &&
    decodeURIComponent(exchange.url.pathname).replace(/^\/emby(?=\/)/i, '') === exchange.scope.route, 'request_body_route_scope');
}

/** Decode only the body, never query credentials; retain original wire bytes. */
export function loginRequestBody(exchange) {
  bodyRoute(exchange, 'login', /^\/Users\/AuthenticateByName\/?$/i);
  const bytes = requestEntity(exchange), type = bodyMediaType(exchange.original.get('content-type'), ['application/json', 'application/x-www-form-urlencoded']);
  if (type === 'application/json') { const value = strictJSON(bytes, MAX_BODY); need(own(value), 'login_body_not_object'); return value; }
  need(exchange.original.get('content-encoding') === null, 'login_form_content_encoding');
  const text = new TextDecoder('utf-8', { fatal: true }).decode(bytes), parts = text.split('&');
  need(parts.length <= 16 && !text.includes(';'), 'login_form_invalid');
  const fields = new Map();
  for (const part of parts.filter(Boolean)) {
    const at = part.indexOf('='), keyBytes = at < 0 ? part : part.slice(0, at), valueBytes = at < 0 ? '' : part.slice(at + 1);
    let key, value;
    try { key = decodeURIComponent(keyBytes.replace(/\+/g, ' ')).toLowerCase(); value = decodeURIComponent(valueBytes.replace(/\+/g, ' ')); }
    catch { throw new Error('login_form_invalid_utf8_or_escape'); }
    need(['username', 'pw'].includes(key) && !fields.has(key) && !value.includes('\0'), 'login_form_duplicate_or_unknown_field');
    fields.set(key, value);
  }
  need(fields.size > 0 && fields.size <= 2, 'login_form_invalid');
  return { Username: fields.get('username') ?? '', Pw: fields.get('pw') ?? '' };
}

export function playbackRequestBody(exchange) {
  bodyRoute(exchange, 'playback_report', PLAYBACK_REPORT_ROUTE);
  bodyMediaType(exchange.original.get('content-type'), ['application/json', 'text/plain']);
  return playbackJSON(requestEntity(exchange));
}

export function protocolRequestBody(exchange) {
  if (exchange.scope?.kind === 'login') return loginRequestBody(exchange);
  if (exchange.scope?.kind === 'playback_report') return playbackRequestBody(exchange);
  bodyRoute(exchange, 'playback_info', /^\/Items\/[a-f0-9]{32}\/PlaybackInfo\/?$/i);
  bodyMediaType(exchange.original.get('content-type'), ['application/json', 'text/plain']);
  const value = completeJSON(exchange); need(own(value), 'playback_body_not_object'); return value;
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

/** Missing IDs are legacy-compatible; false denotes an ambiguous or malformed ID. */
function responseRequestId(headers) {
  if (headers === undefined || headers === null) return null;
  const fields = headers instanceof Map ? [...headers] : own(headers) ? Object.entries(headers) : null;
  if (!fields) return false;
  const matching = fields.filter(([key]) => typeof key === 'string' && key.toLowerCase() === 'x-request-id');
  if (!matching.length) return null;
  if (matching.length !== 1) return false;
  const values = headers instanceof Map ? matching[0][1] : [matching[0][1]];
  if (!Array.isArray(values) || values.length !== 1 || typeof values[0] !== 'string') return false;
  const value = values[0].replace(/^[\t ]+|[\t ]+$/g, '');
  // Header names are case-insensitive; the opaque ID remains case-sensitive.
  // Commas and whitespace cannot distinguish one ID from coalesced headers.
  return /^[\x21-\x7e]+$/.test(value) && !value.includes(',') ? value : false;
}

export function matchesContext(exchange, report, requireExactRange = true) {
  if (!exchange.url) return [];
  const physicalId = responseRequestId(exchange.response?.headers);
  if (physicalId === false) return [];
  const start = ns(report.started_monotonic_ns), observed = ns(exchange.intent.startedMonotonicNs);
  return report.requests.filter(row => {
    if (!['frame', 'service_worker'].includes(row.scope) || row.method !== exchange.original.method || row.token_sha256 !== exchange.tokenHash || !integer(row.elapsed_ms)) return false;
    try { if (new URL(row.url).href !== exchange.url.href || requireExactRange && (row.headers?.range ?? null) !== exchange.original.get('range')) return false; } catch { return false; }
    const observedId = responseRequestId(row.response_headers);
    if (observedId === false || physicalId !== null && observedId !== null && physicalId !== observedId) return false;
    const delta = start + BigInt(row.elapsed_ms) * 1000000n - observed;
    if (delta < -5000000000n || delta > 5000000000n) return false;
    if (exchange.requestEntity && !bytes64(row.payload_base64 ?? '').equals(exchange.requestEntity)) return false;
    return true;
  });
}

function singleByteRange(value) {
  const match = typeof value === 'string' && /^bytes=(\d+)-(\d*)$/i.exec(value);
  if (!match) return null;
  const start = Number(match[1]), end = match[2] === '' ? null : Number(match[2]);
  return integer(start) && (end === null || integer(end) && end >= start) ? { start, end } : null;
}

/** A contained physical range is associated only with one terminal event per scope. */
export function mediaContextEvidence(exchange, report) {
  if (exchange.scope?.allowed !== true || exchange.scope.kind !== 'media' || exchange.original?.method !== 'GET' ||
      ![200, 206].includes(exchange.response?.status)) return [];
  const exact = matchesContext(exchange, report);
  if (exact.length) return exact.map(row => ({ row, association: 'exact_range' }));
  const physical = singleByteRange(exchange.original.get('range'));
  if (!physical) return [];
  const started = ns(report.started_monotonic_ns), wireStart = ns(exchange.intent.startedMonotonicNs), wireEnd = ns(exchange.result.completedMonotonicNs);
  const candidates = matchesContext(exchange, report, false).filter(row => {
    const logical = singleByteRange(row.headers?.range);
    if (row.kind !== 'media' || row.allowed !== true || row.origin !== 'target' || ![200, 206].includes(row.status) ||
        row.failed !== true || row.failure_error_text !== 'net::ERR_ABORTED' || !integer(row.failed_elapsed_ms) ||
        row.failed_elapsed_ms < row.elapsed_ms || !logical || physical.start < logical.start ||
        logical.end !== null && (physical.end === null || physical.end > logical.end)) return false;
    const failed = started + BigInt(row.failed_elapsed_ms) * 1000000n, delta = failed - wireEnd;
    // The browser timestamp is truncated to milliseconds; do not attach a later
    // physical request to an already terminal logical request.
    return wireStart < failed + 1000000n && delta >= -2000000000n && delta <= 2000000000n;
  });
  if (!candidates.length || new Set(candidates.map(row => row.scope)).size !== candidates.length) return [];
  return candidates.map(row => ({ row, association: 'unique_contained_range_with_terminal_time' }));
}

function reportBody(exchange) { return protocolRequestBody(exchange); }
function routeItem(exchange) { return /^\/Items\/([^/]+)\/PlaybackInfo\/?$/i.exec(exchange.scope.route)?.[1] ?? null; }
function bodyPosition(body, fallback) { if (!Object.hasOwn(body, 'PositionTicks')) return fallback; need(integer(body.PositionTicks), 'position_ticks_invalid'); return body.PositionTicks; }
function mediaSourceMatches(value, canonical, item, correlated) { return value === canonical || item?.type === 'Audio' && correlated && value === item.id; }

export function validateServerLog(receipt, beforeBytes, afterBytes, { manifest, epoch, input, approval, observation, authorizedInput, currentIdentity = null }) {
  const currentProcess = input.version === 5 ? currentIdentity?.candidateProcess : epoch.candidateProcess;
  need(own(currentProcess) && exact(receipt, ['kind', 'version', 'runId', 'runtimeEpoch', 'input', 'controller', 'before', 'after', ...(input.version === 5 ? ['currentRuntime'] : [])]) &&
    receipt.kind === 'audited-candidate-client-server-log' && receipt.version === (input.version === 5 ? 2 : 1) && receipt.runId === manifest.runId &&
    (input.version !== 5 || equal(receipt.currentRuntime, input.currentRuntime)) &&
    equal(receipt.runtimeEpoch, input.runtimeEpoch) && descriptor(receipt.input) && descriptor(receipt.controller) &&
    receipt.controller.path.startsWith(RETAINED_ROOT + '/') && path.basename(receipt.controller.path) === 'run-audited-candidate-client.py', 'server_log_receipt_binding');
  need(approval?.kind === 'audited-candidate-client-approval' && approval.runId === manifest.runId && approval.scenario === manifest.scenario &&
    approval.sourceManifestSha256 === manifest.source.manifestSha256 && approval.binarySha256 === manifest.source.binarySha256 &&
    approval.serverId === manifest.serverId && approval.isolatedCandidate === true && equal(approval.authorizedRunInput, receipt.input) &&
    authorizedInput?.kind === 'audited-candidate-client-run-input' && authorizedInput.version === ([4, 5].includes(input.version) ? input.version : 3) && authorizedInput.runId === manifest.runId &&
    authorizedInput.scenario === manifest.scenario && authorizedInput.output === path.dirname(manifest.output) &&
    equal(authorizedInput.runtimeEpoch, input.runtimeEpoch) && equal(authorizedInput.retainedBaseline, input.retainedBaseline) &&
    (input.version !== 5 || descriptor(input.currentRuntime) && equal(authorizedInput.currentRuntime, input.currentRuntime)) &&
    equal(authorizedInput.sources, input.sources), 'server_log_authorized_input');
  for (const [snapshot, bytes] of [[receipt.before, beforeBytes], [receipt.after, afterBytes]]) {
    need(exact(snapshot, ['capturedAt', 'candidateBefore', 'candidateAfter', 'file', 'length', 'content']) &&
      equal(snapshot.candidateBefore, currentProcess) && equal(snapshot.candidateAfter, currentProcess) &&
      exact(snapshot.file, ['path', 'device', 'inode', 'uid', 'mode', 'links']) && snapshot.file.path === CANDIDATE_LOG &&
      typeof snapshot.file.device === 'string' && typeof snapshot.file.inode === 'string' && /^[1-9][0-9]*$/.test(snapshot.file.device) && /^[1-9][0-9]*$/.test(snapshot.file.inode) &&
      snapshot.file.uid === 0 && snapshot.file.mode === 0o600 && snapshot.file.links === 1 &&
      descriptor(snapshot.content) && path.dirname(snapshot.content.path) === path.dirname(input.serverLog.path) &&
      integer(snapshot.length) && snapshot.length > 0 && snapshot.length <= MAX_FILE && Buffer.isBuffer(bytes) &&
      bytes.length === snapshot.length && sha(bytes) === snapshot.content.sha256 && bytes.at(-1) === 10, 'server_log_capture_invalid');
  }
  need(equal(receipt.before.file, receipt.after.file) && afterBytes.length >= beforeBytes.length &&
    afterBytes.subarray(0, beforeBytes.length).equals(beforeBytes), 'server_log_prefix_changed');
  need(timestampNs(receipt.before.capturedAt) <= timestampNs(observation.started_at) && integer(observation.elapsed_ms) &&
    timestampNs(receipt.after.capturedAt) + 1000000n >= timestampNs(observation.started_at) + BigInt(observation.elapsed_ms) * 1000000n &&
    timestampNs(receipt.before.capturedAt) <= timestampNs(receipt.after.capturedAt), 'server_log_capture_window');
  need(receipt.before.content.path !== receipt.after.content.path, 'server_log_capture_alias');
  const appended = afterBytes.subarray(beforeBytes.length), events = [], byRequestId = new Map();
  const text = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(appended);
  for (const bytes of appended.length ? text.slice(0, -1).split('\n').map(line => Buffer.from(line)) : []) {
    const event = strictJSON(bytes, MAX_BODY);
    need(own(event), 'server_log_entry_invalid');
    if (event.msg !== 'request completed') continue;
    // Unrelated requests can append while the before prefix is being read.
    // A cancellation's lower time bound is its exact physical request below.
    need(event.level === 'INFO' && typeof event.request_id === 'string' && /^[a-f0-9]{32}$/.test(event.request_id) && !byRequestId.has(event.request_id) &&
      typeof event.route === 'string' && event.route.length <= 256 && typeof event.method === 'string' &&
      integer(event.bytes) && integer(event.duration_ms) && ['completed', 'cancelled', 'aborted'].includes(event.outcome) &&
      (event.status === undefined || integer(event.status) && event.status >= 100 && event.status <= 599) &&
      timestampNs(event.time) <= timestampNs(receipt.after.capturedAt) + 1000000n,
    'server_log_request_event_invalid');
    events.push(event); byRequestId.set(event.request_id, event);
  }
  return { events, byRequestId, receipt };
}

export function cancelledEmptyMedia(exchange, serverLog) {
  if (!serverLog || exchange.scope?.allowed !== true || exchange.scope.kind !== 'media' || exchange.original?.method !== 'GET' ||
      exchange.complete !== true || exchange.response?.status !== 200 || exchange.deliveredBodyBytes !== 0 ||
      exchange.result.responseBodyWireBytes !== 0 || exchange.result.requestBodyWireBytes !== 0 || exchange.requestDelivered !== true ||
      exchange.result.requestForwardedComplete !== true || exchange.response.get('content-length') !== '0' || exchange.response.get('transfer-encoding') !== null) return null;
  const ids = exchange.response.headers.get('x-request-id');
  if (!Array.isArray(ids) || ids.length !== 1 || !/^[a-f0-9]{32}$/.test(ids[0])) return null;
  const event = serverLog.byRequestId.get(ids[0]), resource = /^\/(?:emby\/)?(Videos|Audio)\//i.exec(exchange.url.pathname)?.[1];
  if (!event || !resource || event.method !== 'GET' || event.status !== 200 || event.bytes !== 0 || event.outcome !== 'cancelled' ||
      !new RegExp('^GET /(?:emby/)?' + resource + '/\\{Id\\}/(?:\\{StreamFileName\\}|stream|universal(?:\\.[a-z0-9_-]+)?)/?$', 'i').test(event.route) ||
      timestampNs(event.time) + 1000000n < timestampNs(exchange.intent.startedAt) || timestampNs(event.time) > timestampNs(exchange.result.completedAt) + 1000000n) return null;
  return { ordinal: exchange.ordinal, requestId: ids[0], interpretation: 'server_cancelled_empty_response', status: 200,
    deliveredBodyBytes: 0, contributesMediaDelivery: false, serverEventTime: event.time };
}

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
export function analyzePhysical(exchanges, manifest, report, after, serverLog = null) {
  const selected = selectedCandidateItem(manifest), logins = [], cancelledEmptyResponses = [];
  for (const exchange of exchanges.filter(row => row.scope?.kind === 'login')) {
    requireACK(exchange, 200); const body = reportBody(exchange), response = completeJSON(exchange, true);
    need(body.Username === manifest.actor.username && typeof body.Pw === 'string' && exchange.token === null, 'physical_login_actor_mismatch');
    const identity = validateCandidateLogin(response, manifest);
    need(matchesContext(exchange, report).some(row => row.scope === 'frame' && row.main_frame), 'physical_login_not_observed_by_ui');
    need(!logins.some(row => row.tokenHash === identity.token_sha256 || row.sessionId === identity.session_id), 'physical_login_identity_reused');
    logins.push({ ...identity, tokenHash: identity.token_sha256, sessionId: identity.session_id, token: response.AccessToken,
      login: exchange, logout: null, verification: null, infos: [], chains: [], media: [], cancelledEmpty: [] });
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
      } else if (exchange.original.method === 'GET' && exchange.complete && [200, 206].includes(exchange.response?.status)) {
        const cancellation = cancelledEmptyMedia(exchange, serverLog);
        need(cancellation, 'completed_media_get_has_no_entity_bytes');
        cancelledEmptyResponses.push(cancellation); login.cancelledEmpty.push(cancellation);
      }
    }
  }
  for (const cancellation of cancelledEmptyResponses) need(exchanges.filter(row => row.response?.headers.get('x-request-id')?.includes(cancellation.requestId)).length === 1,
    'cancelled_media_request_id_reused');
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
    need(login.cancelledEmpty.every(row => row.ordinal < login.logout.ordinal), 'cancelled_media_after_logout');
    verifyPostLogoutTraffic(login, exchanges);
  }
  if (manifest.scenario === 'movie') need(logins[0].verification.ordinal < logins[1].login.ordinal, 'movie_relogin_before_revocation_proof');
  return { logins, chains: [...chains.values()], exchanges, cancelledEmptyResponses };
}

function visibleVideo(report, label) { const step = one(report.playback.steps.filter(row => row.label === label), 'movie_step_missing');
  return one(step.videos.filter(row => row.visible), 'unique_movie_video_missing'); }

export function explainMediaPartial(exchange, report, login) {
  const associated = mediaContextEvidence(exchange, report), matches = associated.map(value => value.row), groups = new Map();
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
    contextAssociation: associated[0].association, contextOrdinals: matches.map(row => row.ordinal ?? null),
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

/** The one consumed movie04 baseline is an explicit authority, not an empty actor. */
export function validateRetainedMovieBaseline(closeout, snapshot, seed) {
  if (closeout?.kind === 'audited-movie05-owned-state-closeout') return validateOccupiedMovieBaseline(closeout, snapshot, seed);
  need(closeout?.kind === 'audited-core-movie04-failure-closeout' && closeout.status === 'closed_failed_attempt_with_retained_unstarted_preparation' &&
    equal(closeout.runtimeEpoch, RETAINED_EPOCH) && equal(closeout.sourceAfter, RETAINED_SNAPSHOT) && closeout.scenario === 'movie' && closeout.runId === 'movie-04' &&
    closeout.browserAndGatewayClosed === true && closeout.allElevenSessionsRevoked === true && closeout.clientAcceptance === false && closeout.playbackStarted === false &&
    closeout.clientPlaybackReferences === 0 && closeout.encodingJobs === 0, 'retained_movie_closeout_invalid');
  need(exact(snapshot, ['capturedAt', 'tables', 'sequences']) && equal(Object.keys(snapshot.tables).sort(), TABLES), 'retained_movie_snapshot_inventory');
  const tables = snapshot.tables, actor = seed.actors.movie, movie = seed.catalog.movie;
  const user = one(tables.users.filter(row => row.id === actor.id), 'retained_movie_actor_changed');
  need(user.name === actor.username && user.is_administrator === false && user.is_disabled === false && equal(user.policy, {}), 'retained_movie_actor_changed');
  need(tables.play_sessions.length === 1 && tables.client_playback_references.length === 0 && tables.encoding_jobs.length === 0 &&
    tables.sessions.length === 11 && tables.sessions.every(row => row.revoked_at !== null), 'retained_movie_row_inventory');
  const play = tables.play_sessions[0], auth = one(tables.sessions.filter(row => row.id === RETAINED_AUTH), 'retained_movie_preparation_changed');
  need(play.id === RETAINED_PLAY && play.auth_session_id === RETAINED_AUTH && play.user_id === actor.id && play.item_id === movie.id &&
    play.media_source_id === 'mediasource_' + movie.id && play.duration_ticks === movie.runtimeTicks && play.application_client_id === null &&
    play.state === 'Prepared' && play.counted === false && play.started_at === null && play.stopped_at === null && play.position_ticks === 0 &&
    auth.kind === 'emby' && auth.user_id === actor.id && auth.device_id === play.device_id, 'retained_movie_preparation_changed');
  const data = one(tables.user_item_data.filter(row => row.user_id === actor.id), 'retained_movie_history_not_zero');
  need(data.item_id === movie.id && data.playback_position_ticks === 0 && data.play_count === 0 && data.is_favorite === false &&
    data.played === false && data.last_played_at === null, 'retained_movie_history_not_zero');
  const residue = closeout.retainedPreparation;
  need(residue?.id === RETAINED_PLAY && residue.authSessionId === RETAINED_AUTH && residue.itemId === movie.id && residue.ownerCredentialRevoked === true,
    'retained_movie_closeout_row_binding');
  return play;
}

export function validateOccupiedMovieBaseline(closeout, snapshot, seed) {
  const data = closeout?.checks?.ownedData, state = closeout?.checks?.savedRuntimeAndWorkers;
  need(closeout?.kind === 'audited-movie05-owned-state-closeout' && closeout.status === 'owned_state_closed_client_acceptance_pending' &&
    closeout.clientAcceptance === false && closeout.failure === undefined && equal(closeout.inputEvidence?.after, OCCUPIED_SNAPSHOT) &&
    equal(closeout.inputEvidence?.epoch, RETAINED_EPOCH) && closeout.browserOutcome === 'failed' &&
    closeout.browserFailure === 'candidate_playback_chain_incomplete' && closeout.browserExitCode === 1 && closeout.gatewayExitCode === 0 &&
    state?.candidateContinuous === true && state.postgresContinuous === true && state.leaseExact === true &&
    ['browser', 'gateway'].every(role => state.workers?.[role]?.pidAbsentAsRecorded === true && state.workers[role].recursiveCgroupEmptyAsRecorded === true),
  'occupied_movie_closeout_invalid');
  need(data?.ownedTables === 35 && data.unchangedTables === 30 && data.oldRowsDeleted === 0 && data.clientPlaybackReferences === 0 && data.encodingJobs === 0 &&
    closeout.checks.authentication?.allSessionsRevoked === 13 && exact(snapshot, ['capturedAt', 'tables', 'sequences']) &&
    equal(Object.keys(snapshot.tables).sort(), TABLES), 'occupied_movie_inventory');
  const tables = snapshot.tables, actor = seed.actors.movie, movie = seed.catalog.movie;
  const user = one(tables.users.filter(row => row.id === actor.id), 'occupied_movie_actor');
  need(user.name === actor.username && user.is_administrator === false && user.is_disabled === false && equal(user.policy, {}) &&
    tables.sessions.length === 13 && tables.sessions.every(row => row.revoked_at !== null) && tables.play_sessions.length === 5 &&
    tables.client_playback_references.length === 0 && tables.encoding_jobs.length === 0, 'occupied_movie_actor_or_residue');
  const stopped = data.countedStopped, unstarted = data.unstartedPreparations;
  need(Array.isArray(stopped) && stopped.length === 2 && Array.isArray(unstarted) && unstarted.length === 2 &&
    data.oldPreparation?.id === RETAINED_PLAY && data.oldPreparation.state === 'Expired', 'occupied_movie_history_shape');
  const expected = new Set([RETAINED_PLAY, ...stopped.map(row => row.id), ...unstarted.map(row => row.id)]);
  need(expected.size === 5 && tables.play_sessions.every(row => expected.has(row.id)), 'occupied_movie_play_set');
  for (const row of tables.play_sessions) {
    const auth = one(tables.sessions.filter(value => value.id === row.auth_session_id), 'occupied_movie_auth_missing');
    need(row.user_id === actor.id && row.item_id === movie.id && row.media_source_id === 'mediasource_' + movie.id &&
      row.duration_ticks === movie.runtimeTicks && row.application_client_id === null && row.client_correlated === false &&
      auth.user_id === actor.id && auth.kind === 'emby' && auth.device_id === row.device_id && auth.revoked_at !== null, 'occupied_movie_play_owner');
    const prior = stopped.find(value => value.id === row.id);
    if (prior) need(row.state === 'Stopped' && row.counted === true && row.position_ticks === prior.positionTicks &&
      row.auth_session_id === prior.authSessionId && row.started_at === prior.startedAt && row.stopped_at === prior.stoppedAt, 'occupied_movie_counted_history');
    else need(row.counted === false && row.started_at === null && ['Prepared', 'Expired'].includes(row.state) &&
      (row.state === 'Prepared' ? row.stopped_at === null : row.stopped_at !== null), 'occupied_movie_unstarted_history');
  }
  const play = one(tables.play_sessions.filter(row => row.state === 'Prepared'), 'occupied_movie_prepared_count');
  need(play.id === OCCUPIED_PLAY && play.auth_session_id === OCCUPIED_AUTH && play.position_ticks === 1217878390 &&
    unstarted.some(row => row.id === play.id && row.state === 'Prepared' && row.counted === false && row.positionTicks === play.position_ticks), 'occupied_movie_prepared_binding');
  const userdata = one(tables.user_item_data.filter(row => row.user_id === actor.id), 'occupied_movie_userdata_count');
  need(equal(userdata, data.userData) && userdata.item_id === movie.id && userdata.play_count === 2 && userdata.playback_position_ticks === 1217878390 &&
    userdata.is_favorite === false && userdata.played === false && userdata.last_played_at !== null, 'occupied_movie_userdata');
  return play;
}

export function validateReviewedMovieBaselineInput(review) {
  need(exact(review, ['kind', 'version', 'status', 'scenario', 'closeout', 'snapshot', 'sourceEpoch', 'runtimeEpoch', 'seedBinding', 'manifest', 'boundary', 'movieProvenance', 'actor', 'item', 'preparedExpirations']) &&
    review.kind === 'audited-candidate-reviewed-movie-baseline' && review.version === 1 && review.status === 'reviewed_closed_state' && review.scenario === 'movie' &&
    ['closeout', 'snapshot', 'sourceEpoch', 'runtimeEpoch', 'seedBinding', 'manifest'].every(key => descriptor(review[key])), 'reviewed_movie_input_invalid');
  need((review.boundary === null || descriptor(review.boundary)) && exact(review.movieProvenance, ['closeout', 'snapshot', 'sourceEpoch', 'manifest']) &&
    Object.values(review.movieProvenance).every(descriptor), 'reviewed_movie_provenance_input_invalid');
  need(exact(review.actor, ['id', 'username']) && /^[a-f0-9]{32}$/.test(review.actor.id) && typeof review.actor.username === 'string' && review.actor.username.length > 0 &&
    exact(review.item, ['id', 'mediaSourceId', 'runtimeTicks']) && /^[a-f0-9]{32}$/.test(review.item.id) && review.item.mediaSourceId === 'mediasource_' + review.item.id &&
    integer(review.item.runtimeTicks) && review.item.runtimeTicks > 0, 'reviewed_movie_identity_invalid');
  need(Array.isArray(review.preparedExpirations) && review.preparedExpirations.length < 256 && review.preparedExpirations.every(row =>
    exact(row, ['playSessionId', 'authSessionId']) && typeof row.playSessionId === 'string' && /^play_[A-Za-z0-9_]+$/.test(row.playSessionId) &&
    typeof row.authSessionId === 'string' && /^[A-Za-z0-9_-]+$/.test(row.authSessionId)) &&
    new Set(review.preparedExpirations.map(row => row.playSessionId)).size === review.preparedExpirations.length, 'reviewed_movie_expiration_allowlist_invalid');
  return review;
}

function validateReviewedClosedState(evidence, pins, seed, boundary = null) {
  const { closeout, snapshot, sourceEpoch, manifest } = evidence, source = sourceEpoch?.currentSource;
  need(sourceEpoch?.kind === 'audited-candidate-runtime-epoch' && [2, 3].includes(sourceEpoch.version) &&
    exact(source, ['archiveSha256', 'sourceManifest', 'binary', 'fullReport', 'schema']) && SHA.test(source.archiveSha256) && source.schema === 28 &&
    ['sourceManifest', 'binary', 'fullReport'].every(key => descriptor(source[key])), 'reviewed_movie_source_epoch');
  const actor = seed.actors[manifest?.scenario];
  need(manifest?.kind === 'audited-candidate-client-input' && manifest.version === 1 && ['movie', 'subtitles'].includes(manifest.scenario) &&
    typeof manifest.runId === 'string' && manifest.serverId === seed.serverId && actor &&
    equal(manifest.actor, { id: actor.id, username: actor.username }) && equal(manifest.catalog?.movie, seed.catalog.movie) &&
    equal(manifest.source, { manifestSha256: source.sourceManifest.sha256, binarySha256: source.binary.sha256, schema: source.schema }), 'reviewed_movie_manifest_binding');
  need(closeout?.status === 'owned_state_closed_client_acceptance_pending' && closeout.clientAcceptance === false && !Object.hasOwn(closeout, 'failure'), 'reviewed_movie_closeout_not_closed');
  need(exact(snapshot, ['capturedAt', 'tables', 'sequences']) && equal(Object.keys(snapshot.tables).sort(), TABLES) && TABLES.every(table => Array.isArray(snapshot.tables[table]) && snapshot.tables[table].every(own)) &&
    own(snapshot.sequences), 'reviewed_movie_snapshot_inventory');
  timestampNs(snapshot.capturedAt);
  const tables = snapshot.tables;
  indexed(tables.sessions); indexed(tables.play_sessions);
  need(tables.sessions.every(row => typeof row.id === 'string' && row.id.length > 0 && typeof row.revoked_at === 'string') &&
    tables.play_sessions.every(row => typeof row.id === 'string' && row.id.length > 0), 'reviewed_movie_credentials_or_residue');
  for (const row of tables.sessions) timestampNs(row.revoked_at);
  if (manifest.scenario === 'movie') {
    const data = closeout.checks?.ownedData, runtime = closeout.checks?.savedRuntimeAndWorkers;
    need((pins.boundary ?? null) === null && boundary === null && /^movie-[0-9]{2,}$/.test(manifest.runId) &&
      closeout.kind === 'audited-' + manifest.runId.replaceAll('-', '') + '-owned-state-closeout' && closeout.browserOutcome === 'failed' && closeout.browserExitCode === 1 && closeout.gatewayExitCode === 0 &&
      runtime?.candidateContinuous === true && runtime.postgresContinuous === true && runtime.leaseExact === true && ['browser', 'gateway'].every(role =>
        runtime.workers?.[role]?.pidAbsentAsRecorded === true && runtime.workers[role].recursiveCgroupEmptyAsRecorded === true), 'reviewed_movie_closeout_not_closed');
    need(equal(closeout.inputEvidence?.after, pins.snapshot) && equal(closeout.inputEvidence?.epoch, pins.sourceEpoch) && equal(closeout.inputEvidence?.manifest, pins.manifest), 'reviewed_movie_closeout_binding');
    need(data?.ownedTables === 35 && data.oldRowsDeleted === 0 && data.clientPlaybackReferences === tables.client_playback_references.length && data.encodingJobs === tables.encoding_jobs.length &&
      closeout.checks.authentication?.allSessionsRevoked === tables.sessions.length, 'reviewed_movie_closed_state_counts');
    need(equal(one(tables.user_item_data.filter(row => row.user_id === actor.id), 'reviewed_movie_userdata_count'), data.userData), 'reviewed_movie_userdata_changed');
  } else {
    const input = closeout.inputEvidence, state = closeout.sourceState;
    need(closeout.kind === 'audited-candidate-subtitles-owned-state-closeout' && !Object.hasOwn(closeout, 'checks') && /^subtitles-[0-9]{2,}$/.test(manifest.runId) &&
      closeout.sourcePinsUnchanged === true && own(closeout.originalUIRejection) && closeout.originalUIRejection.originalObservationUnchanged === true &&
      input?.version === 3 && input.retainedBaseline === null, 'reviewed_movie_closed_state_kind');
    validateCloseoutInput(input);
    need(descriptor(pins.boundary) && equal(input.sourceAfter, pins.snapshot) && equal(input.runtimeEpoch, pins.sourceEpoch) && equal(input.manifest, pins.manifest) &&
      equal(input.seedBinding, pins.seedBinding) && equal(input.boundary, pins.boundary), 'reviewed_movie_closeout_binding');
    need(state?.allSessionsRevoked === true && state.afterSessions === tables.sessions.length && state.afterPlays === tables.play_sessions.length &&
      state.afterUserData === tables.user_item_data.length && state.afterClientReferences === tables.client_playback_references.length && state.afterEncodingJobs === tables.encoding_jobs.length,
    'reviewed_movie_closed_state_counts');
    need(own(sourceEpoch.candidateProcess) && own(sourceEpoch.postgresProcess) && own(sourceEpoch.lease) &&
      typeof sourceEpoch.candidate?.database === 'string' && sourceEpoch.candidate.database.length > 0 && own(manifest.processes?.candidate?.listener), 'reviewed_movie_closed_state_runtime');
    need(exact(boundary, ['kind', 'version', 'runId', 'runtimeEpoch', 'sourceBefore', 'sourceAfter', 'candidateBefore', 'candidateAfter', 'postgresBefore', 'postgresAfter',
      'leaseBefore', 'leaseAfter', 'database', 'beforeMonotonicNs', 'afterMonotonicNs', 'clientWorker', 'gatewayWorker']) &&
      boundary.kind === 'audited-candidate-client-boundary' && boundary.version === 1 && boundary.runId === manifest.runId &&
      equal(boundary.runtimeEpoch, pins.sourceEpoch) && equal(boundary.sourceBefore, input.sourceBefore) && equal(boundary.sourceAfter, pins.snapshot) &&
      equal(boundary.candidateBefore, manifest.processes?.candidate) && equal(boundary.candidateAfter, manifest.processes?.candidate) &&
      equal(without(boundary.candidateBefore, ['listener']), sourceEpoch.candidateProcess) && equal(boundary.postgresBefore, sourceEpoch.postgresProcess) &&
      equal(boundary.postgresAfter, sourceEpoch.postgresProcess) && equal(boundary.leaseBefore, sourceEpoch.lease) && equal(boundary.leaseAfter, sourceEpoch.lease) &&
      equal(boundary.database, sourceEpoch.candidate.database) && typeof boundary.beforeMonotonicNs === 'string' && typeof boundary.afterMonotonicNs === 'string' &&
      ns(boundary.beforeMonotonicNs) <= ns(boundary.afterMonotonicNs), 'reviewed_movie_closed_state_boundary');
    need(equal(boundary.clientWorker, { exitCode: 0, mainPID: 0, workerPidAbsent: true, remainingBrowserPids: [] }) &&
      equal(boundary.gatewayWorker, { exitCode: 0, mainPID: 0, workerPidAbsent: true, index: input.gatewayIndex }), 'reviewed_movie_workers_not_closed');
  }
}

/** The pinned movie provenance and review supply authority, not the latest snapshot. */
export function validateReviewedMovieBaseline(retained, seed) {
  need(exact(retained, ['review', 'closeout', 'snapshot', 'sourceEpoch', 'manifest', 'boundary', 'movieProvenance', 'inputBinding']), 'reviewed_movie_evidence_invalid');
  const { review, snapshot, inputBinding, movieProvenance } = retained;
  validateReviewedMovieBaselineInput(review);
  need(exact(inputBinding, ['runtimeEpoch', 'seedBinding']) && descriptor(inputBinding.runtimeEpoch) && descriptor(inputBinding.seedBinding) &&
    equal(review.runtimeEpoch, inputBinding.runtimeEpoch) && equal(review.seedBinding, inputBinding.seedBinding) && equal(seed.runtimeEpoch, inputBinding.runtimeEpoch), 'reviewed_movie_current_binding');
  need(exact(movieProvenance, ['closeout', 'snapshot', 'sourceEpoch', 'manifest']) && movieProvenance.manifest?.scenario === 'movie', 'reviewed_movie_provenance_invalid');
  validateReviewedClosedState(movieProvenance, review.movieProvenance, seed);
  validateReviewedClosedState(retained, review, seed, retained.boundary);
  need(timestampNs(snapshot.capturedAt) >= timestampNs(movieProvenance.snapshot.capturedAt), 'reviewed_movie_provenance_time_reversed');
  const actor = seed.actors.movie, movie = seed.catalog.movie, tables = snapshot.tables, sessions = indexed(tables.sessions), plays = indexed(tables.play_sessions);
  need(equal(movieProvenance.manifest.actor, review.actor) && equal(review.actor, { id: actor.id, username: actor.username }) &&
    equal(review.item, { id: movie.id, mediaSourceId: 'mediasource_' + movie.id, runtimeTicks: movie.runtimeTicks }), 'reviewed_movie_manifest_binding');
  for (const table of ['users', 'sessions', 'play_sessions', 'user_item_data']) {
    const owned = row => (table === 'users' ? row.id : row.user_id) === actor.id;
    need(equal(tables[table].filter(owned), movieProvenance.snapshot.tables[table].filter(owned)), 'reviewed_movie_provenance_state_changed');
  }
  const ownedAuth = new Set(tables.sessions.filter(row => row.user_id === actor.id).map(row => row.id)), ownedPlays = new Set(tables.play_sessions.filter(row => row.user_id === actor.id).map(row => row.id));
  need(['client_playback_references', 'encoding_jobs'].every(table => tables[table].every(row => row.user_id !== actor.id &&
    !ownedAuth.has(row.auth_session_id) && !ownedPlays.has(row.play_session_id))), 'reviewed_movie_credentials_or_residue');
  const user = one(tables.users.filter(row => row.id === actor.id), 'reviewed_movie_actor_missing');
  need(user.name === actor.username && user.is_administrator === false && user.is_disabled === false && equal(user.policy, {}), 'reviewed_movie_actor_changed');
  const owned = tables.play_sessions.filter(row => row.user_id === actor.id);
  need(owned.length < 256 && tables.play_sessions.every(row => typeof row.id === 'string'), 'reviewed_movie_play_inventory');
  for (const row of owned) {
    const session = sessions.get(row.auth_session_id);
    need(row.item_id === movie.id && row.media_source_id === review.item.mediaSourceId && row.duration_ticks === movie.runtimeTicks &&
      integer(row.position_ticks) && row.position_ticks <= row.duration_ticks && row.application_client_id === null && row.client_correlated === false &&
      session?.kind === 'emby' && session.user_id === actor.id && session.device_id === row.device_id &&
      (row.state === 'Stopped' ? row.counted === true && typeof row.started_at === 'string' && typeof row.stopped_at === 'string' :
        ['Prepared', 'Expired'].includes(row.state) && row.counted === false && row.started_at === null &&
        (row.state === 'Prepared' ? row.stopped_at === null : typeof row.stopped_at === 'string')), 'reviewed_movie_play_identity');
    timestampNs(row.expires_at);
  }
  const prepared = owned.filter(row => row.state === 'Prepared');
  need(equal(prepared.map(row => row.id).sort(), review.preparedExpirations.map(row => row.playSessionId).sort()) &&
    review.preparedExpirations.every(row => plays.get(row.playSessionId)?.auth_session_id === row.authSessionId), 'reviewed_movie_prepared_authority');
  const userdata = one(tables.user_item_data.filter(row => row.user_id === actor.id), 'reviewed_movie_userdata_count');
  need(userdata.item_id === movie.id && integer(userdata.play_count) && integer(userdata.playback_position_ticks) &&
    typeof userdata.is_favorite === 'boolean' && typeof userdata.played === 'boolean', 'reviewed_movie_userdata_changed');
  return prepared;
}

export function validateReviewedTVBaselineInput(review) {
  need(exact(review, ['kind', 'version', 'status', 'scenario', 'closeout', 'snapshot', 'sourceEpoch', 'runtimeEpoch', 'seedBinding', 'manifest', 'boundary', 'tvProvenance', 'postBrowseAdmission', 'actor', 'item', 'preparedExpirations']) &&
    review.kind === 'audited-candidate-reviewed-tv-baseline' && review.version === 1 && review.status === 'reviewed_closed_state' && review.scenario === 'tv-browse' &&
    ['closeout', 'snapshot', 'sourceEpoch', 'runtimeEpoch', 'seedBinding', 'manifest', 'boundary'].every(key => descriptor(review[key])) &&
    exact(review.tvProvenance, ['closeout', 'snapshot', 'sourceEpoch', 'manifest']) && Object.values(review.tvProvenance).every(descriptor) &&
    equal(review.postBrowseAdmission, TV_POST_BROWSE_ADMISSION), 'reviewed_tv_input_invalid');
  need(exact(review.actor, ['id', 'username']) && /^[a-f0-9]{32}$/.test(review.actor.id) && typeof review.actor.username === 'string' && review.actor.username.length > 0 &&
    exact(review.item, ['id', 'mediaSourceId', 'runtimeTicks']) && /^[a-f0-9]{32}$/.test(review.item.id) && review.item.mediaSourceId === 'mediasource_' + review.item.id &&
    integer(review.item.runtimeTicks) && review.item.runtimeTicks > 0, 'reviewed_tv_identity_invalid');
  need(Array.isArray(review.preparedExpirations) && review.preparedExpirations.length === 1 && review.preparedExpirations.every(row =>
    exact(row, ['playSessionId', 'authSessionId']) && typeof row.playSessionId === 'string' && /^play_[A-Za-z0-9_]+$/.test(row.playSessionId) &&
    typeof row.authSessionId === 'string' && /^[A-Za-z0-9_-]+$/.test(row.authSessionId)), 'reviewed_tv_expiration_allowlist_invalid');
  return review;
}

function tvDetailItem(seed) {
  const item = one(seed.catalog.episodes.filter(row => row.type === 'Episode' && row.indexNumber === 1 && row.parentIndexNumber === 2), 'reviewed_tv_detail_item_missing');
  need(integer(item.runtimeTicks) && item.runtimeTicks > 0 && /^[a-f0-9]{32}$/.test(item.id), 'reviewed_tv_detail_item_invalid');
  return item;
}

/** The TV actor keeps its own provenance, independent of the latest run's actor. */
export function validateReviewedTVBaseline(retained, seed) {
  need(exact(retained, ['review', 'closeout', 'snapshot', 'sourceEpoch', 'manifest', 'boundary', 'tvProvenance', 'postBrowseAdmission', 'inputBinding']), 'reviewed_tv_evidence_invalid');
  const { review, snapshot, inputBinding, tvProvenance: prior } = retained;
  validateReviewedTVBaselineInput(review);
  need(exact(inputBinding, ['runtimeEpoch', 'seedBinding']) && descriptor(inputBinding.runtimeEpoch) && descriptor(inputBinding.seedBinding) &&
    equal(review.runtimeEpoch, inputBinding.runtimeEpoch) && equal(review.seedBinding, inputBinding.seedBinding) && equal(seed.runtimeEpoch, inputBinding.runtimeEpoch), 'reviewed_tv_current_binding');
  need(exact(prior, ['closeout', 'snapshot', 'sourceEpoch', 'manifest']) && retained.manifest?.scenario === 'subtitles', 'reviewed_tv_provenance_invalid');
  validateReviewedClosedState(retained, review, seed, retained.boundary);
  const actor = seed.actors['tv-browse'], item = tvDetailItem(seed), source = prior.sourceEpoch?.currentSource, manifest = prior.manifest, closeout = prior.closeout;
  need(equal(review.actor, { id: actor.id, username: actor.username }) &&
    equal(review.item, { id: item.id, mediaSourceId: 'mediasource_' + item.id, runtimeTicks: item.runtimeTicks }), 'reviewed_tv_actor_or_item_binding');
  need(prior.sourceEpoch?.kind === 'audited-candidate-runtime-epoch' && [2, 3].includes(prior.sourceEpoch.version) &&
    exact(source, ['archiveSha256', 'sourceManifest', 'binary', 'fullReport', 'schema']) && SHA.test(source.archiveSha256) && source.schema === 28 &&
    ['sourceManifest', 'binary', 'fullReport'].every(key => descriptor(source[key])), 'reviewed_tv_source_epoch');
  need(manifest?.kind === 'audited-candidate-client-input' && manifest.version === 1 && manifest.scenario === 'tv-browse' &&
    typeof manifest.runId === 'string' && /^tv-browse-[0-9]{2,}$/.test(manifest.runId) && manifest.serverId === seed.serverId && equal(manifest.actor, review.actor) &&
    equal(manifest.source, { manifestSha256: source.sourceManifest.sha256, binarySha256: source.binary.sha256, schema: source.schema }) &&
    ['tvLibrary', 'series', 'seasons', 'episodes'].every(key => equal(manifest.catalog?.[key], seed.catalog[key]) && equal(retained.manifest.catalog?.[key], seed.catalog[key])), 'reviewed_tv_manifest_binding');
  const data = closeout?.checks?.ownedData, runtime = closeout?.checks?.savedRuntimeAndWorkers;
  need(closeout?.kind === 'audited-tv-browse' + manifest.runId.slice('tv-browse-'.length) + '-owned-state-closeout' && closeout.runId === manifest.runId &&
    closeout.status === 'owned_state_closed_client_acceptance_pending' && closeout.clientAcceptance === false && !Object.hasOwn(closeout, 'failure') &&
    closeout.browserOutcome === 'failed' && closeout.browserExitCode === 1 && closeout.gatewayExitCode === 0 && closeout.physicalMediaRequests === 0 && closeout.playingReports === 0 &&
    runtime?.candidateContinuous === true && runtime.postgresContinuous === true && runtime.leaseExact === true && ['browser', 'gateway'].every(role =>
      runtime.workers?.[role]?.pidAbsentAsRecorded === true && runtime.workers[role].recursiveCgroupEmptyAsRecorded === true), 'reviewed_tv_provenance_not_closed');
  need(equal(closeout.inputEvidence?.after, review.tvProvenance.snapshot) && equal(closeout.inputEvidence?.epoch, review.tvProvenance.sourceEpoch) &&
    equal(closeout.inputEvidence?.manifest, review.tvProvenance.manifest), 'reviewed_tv_provenance_binding');
  need(exact(prior.snapshot, ['capturedAt', 'tables', 'sequences']) && equal(Object.keys(prior.snapshot.tables).sort(), TABLES) &&
    TABLES.every(table => Array.isArray(prior.snapshot.tables[table]) && prior.snapshot.tables[table].every(own)) && own(prior.snapshot.sequences), 'reviewed_tv_snapshot_inventory');
  const tables = prior.snapshot.tables, sessions = indexed(tables.sessions); indexed(tables.play_sessions);
  need(tables.sessions.every(row => typeof row.id === 'string' && row.id.length > 0 && typeof row.revoked_at === 'string') &&
    tables.play_sessions.every(row => typeof row.id === 'string' && row.id.length > 0) && closeout.checks.authentication?.allSessionsRevoked === tables.sessions.length &&
    data?.ownedTables === 35 && data.oldRowsDeleted === 0 && data.newCountedPlays === 0 &&
    data.clientPlaybackReferences === tables.client_playback_references.length && data.encodingJobs === tables.encoding_jobs.length, 'reviewed_tv_inventory_or_credentials');
  for (const row of tables.sessions) timestampNs(row.revoked_at);
  const user = one(tables.users.filter(row => row.id === actor.id), 'reviewed_tv_actor_missing');
  need(user.name === actor.username && user.is_administrator === false && user.is_disabled === false && equal(user.policy, {}), 'reviewed_tv_actor_changed');
  const play = one(tables.play_sessions.filter(row => row.user_id === actor.id), 'reviewed_tv_preparation_count'), session = sessions.get(play.auth_session_id);
  need(play.item_id === item.id && play.media_source_id === review.item.mediaSourceId && play.duration_ticks === item.runtimeTicks && play.position_ticks === 0 &&
    play.state === 'Prepared' && play.counted === false && play.started_at === null && play.stopped_at === null && play.application_client_id === null && play.client_correlated === false &&
    session?.kind === 'emby' && session.user_id === actor.id && session.device_id === play.device_id &&
    equal(review.preparedExpirations, [{ playSessionId: play.id, authSessionId: play.auth_session_id }]), 'reviewed_tv_preparation_identity');
  timestampNs(play.expires_at);
  const declared = one(data.unstartedPreparations, 'reviewed_tv_declared_preparation_missing');
  need(declared.id === play.id && declared.authSessionId === play.auth_session_id && declared.itemId === item.id && declared.state === 'Prepared' &&
    declared.counted === false && declared.positionTicks === 0 && Array.isArray(declared.prepareOrdinals) && declared.prepareOrdinals.length === 1 &&
    integer(declared.prepareOrdinals[0]) && declared.prepareOrdinals[0] > 0, 'reviewed_tv_declared_preparation_changed');
  const userdata = one(tables.user_item_data.filter(row => row.user_id === actor.id), 'reviewed_tv_userdata_count');
  need(equal(userdata, data.newUserData) && userdata.item_id === item.id && userdata.play_count === 0 && userdata.playback_position_ticks === 0 &&
    userdata.is_favorite === false && userdata.played === false && userdata.last_played_at === null, 'reviewed_tv_userdata_not_zero');
  const admission = retained.postBrowseAdmission, proof = admission?.controllerSessions?.P;
  need(admission?.kind === 'audited-candidate-live-admission' && admission.version === 3 && admission.status === 'admitted_for_core_client' &&
    admission.candidateAdmissionComplete === true && admission.failure === null && equal(admission.cleanupFailures, []) &&
    equal(admission.runtimeEpoch, review.runtimeEpoch) && equal(admission.seedRuntimeBinding, review.seedBinding), 'reviewed_tv_post_browse_admission');
  need(exact(proof, ['credentialId', 'tokenSha256', 'sameTokenRejected']) && /^[a-f0-9]{32}$/.test(proof.credentialId) && SHA.test(proof.tokenSha256) &&
    proof.sameTokenRejected === true, 'reviewed_tv_post_browse_credential');
  for (const table of ['users', 'sessions', 'play_sessions', 'user_item_data']) {
    const owned = row => (table === 'users' ? row.id : row.user_id) === actor.id;
    let currentRows = snapshot.tables[table].filter(owned); const historical = tables[table].filter(owned);
    if (table === 'sessions') {
      const extra = one(currentRows.filter(row => row.id === proof.credentialId), 'reviewed_tv_post_browse_session');
      need(!historical.some(row => row.id === proof.credentialId) && extra.kind === 'emby' && extra.token_hash === '\\x' + proof.tokenSha256 &&
        !snapshot.tables.play_sessions.some(row => row.auth_session_id === proof.credentialId), 'reviewed_tv_post_browse_session');
      timestampNs(extra.revoked_at);
      currentRows = currentRows.filter(row => row.id !== proof.credentialId);
    }
    need(equal(currentRows, historical), 'reviewed_tv_provenance_state_changed');
  }
  const ownedAuth = new Set(snapshot.tables.sessions.filter(row => row.user_id === actor.id).map(row => row.id));
  for (const state of [snapshot, prior.snapshot]) need(['client_playback_references', 'encoding_jobs'].every(table => state.tables[table].every(row =>
    row.user_id !== actor.id && !ownedAuth.has(row.auth_session_id) && row.play_session_id !== play.id)), 'reviewed_tv_owned_residue');
  need(timestampNs(snapshot.capturedAt) >= timestampNs(prior.snapshot.capturedAt), 'reviewed_tv_provenance_time_reversed');
  return play;
}

export function verifyReviewedTVBefore(before, manifest, seed, retained) {
  need(manifest.scenario === 'tv-browse' && equal(manifest.actor, retained?.review?.actor), 'reviewed_tv_scenario_required');
  const play = validateReviewedTVBaseline(retained, seed), prior = retained.snapshot;
  need(exact(before, ['capturedAt', 'tables', 'sequences']) && equal(before.tables, prior.tables) && equal(before.sequences, prior.sequences), 'reviewed_tv_fresh_state_changed');
  need(timestampNs(before.capturedAt) >= timestampNs(prior.capturedAt) &&
    timestampNs(before.capturedAt) + 1200n * 1000000000n < timestampNs(play.expires_at) + 7n * 86400n * 1000000000n, 'reviewed_tv_pruning_deadline');
  return play;
}

export function verifyRetainedMovieBefore(before, manifest, seed, retained) {
  if (own(retained) && Object.hasOwn(retained, 'review')) {
    need(manifest.scenario === 'movie' && equal(manifest.actor, retained.review.actor), 'reviewed_movie_scenario_required');
    const prepared = validateReviewedMovieBaseline(retained, seed), prior = retained.snapshot;
    need(exact(before, ['capturedAt', 'tables', 'sequences']) && equal(before.tables, prior.tables) && equal(before.sequences, prior.sequences), 'retained_movie_fresh_state_changed');
    need(timestampNs(before.capturedAt) >= timestampNs(prior.capturedAt) && prior.tables.play_sessions.filter(row => row.user_id === manifest.actor.id).every(row =>
      timestampNs(before.capturedAt) + 1200n * 1000000000n < timestampNs(row.expires_at) + 7n * 86400n * 1000000000n), 'retained_movie_pruning_deadline');
    return prepared;
  }
  need(manifest.scenario === 'movie' && exact(retained, ['closeout', 'snapshot']), 'retained_movie_scenario_required');
  const prior = retained.snapshot, play = validateRetainedMovieBaseline(retained.closeout, prior, seed);
  need(exact(before, ['capturedAt', 'tables', 'sequences']) && equal(before.tables, prior.tables) && equal(before.sequences, prior.sequences), 'retained_movie_fresh_state_changed');
  need(timestampNs(before.capturedAt) >= timestampNs(prior.capturedAt) && prior.tables.play_sessions.filter(row => row.user_id === manifest.actor.id).every(row =>
    timestampNs(before.capturedAt) + 1200n * 1000000000n < timestampNs(row.expires_at) + 7n * 86400n * 1000000000n), 'retained_movie_pruning_deadline');
  return play;
}

export function verifyRetainedMovieTransition(before, after, manifest, seed, physical, retained) {
  if (own(retained) && Object.hasOwn(retained, 'review')) return verifyReviewedMovieTransition(before, after, manifest, seed, physical, retained);
  const old = verifyRetainedMovieBefore(before, manifest, seed, retained), actor = manifest.actor.id;
  need(actor === seed.actors.movie.id, 'retained_movie_actor_changed');
  const current = one(after.tables.play_sessions.filter(row => row.id === old.id), 'retained_movie_row_missing');
  need(equal(without(current, ['state', 'stopped_at', 'updated_at']), without(old, ['state', 'stopped_at', 'updated_at'])) &&
    current.state === 'Expired' && current.stopped_at !== null && current.updated_at !== null, 'retained_movie_expiration_fields');
  // These are upper bounds on creations, including requests that reused a row.
  const creators = physical.exchanges.filter(row => !row.rejected && (row.scope?.kind === 'playback_info' ||
    row.scope?.kind === 'playback_report' && /^\/Sessions\/Playing\/?$/i.test(row.scope.route)));
  const oldPlays = before.tables.play_sessions.filter(row => row.user_id === actor), oldIds = new Set(oldPlays.map(row => row.id)), oldAuth = new Set(before.tables.sessions.map(row => row.id));
  need(oldPlays.length + creators.length <= 256 && after.tables.play_sessions.filter(row => row.user_id === actor).length <= 256 &&
    oldPlays.every(row => timestampNs(after.capturedAt) < timestampNs(row.expires_at) + 7n * 86400n * 1000000000n), 'retained_movie_pruning_capacity');
  const infos = physical.logins.flatMap(login => login.infos.map(info => ({ login, info })))
    .filter(({ info }) => info.itemId === old.item_id && info.exchange.complete && info.exchange.response?.status === 200)
    .sort((left, right) => left.info.exchange.ordinal - right.info.exchange.ordinal);
  need(infos.length > 0, 'retained_movie_prepare_window_missing');
  const { login, info } = infos[0], exchange = info.exchange;
  const authentication = one(after.tables.sessions.filter(row => row.id === login.sessionId), 'retained_movie_new_auth_missing');
  need(!oldAuth.has(login.sessionId) && authentication.user_id === actor && authentication.kind === 'emby' && exchange.tokenHash === login.tokenHash &&
    !oldIds.has(info.value.PlaySessionId) && !physical.chains.some(chain => oldIds.has(chain.play.id)) &&
    physical.logins.every(row => !oldAuth.has(row.sessionId)), 'retained_movie_old_auth_or_play_reused');
  need(within(current.stopped_at, exchange.intent.startedAt, exchange.result.completedAt) &&
    within(current.updated_at, exchange.intent.startedAt, exchange.result.completedAt), 'retained_movie_expiration_outside_prepare');
  return { playSessionId: old.id, previousState: 'Prepared', state: 'Expired', prepareOrdinal: exchange.ordinal,
    changedFields: ['state', 'stopped_at', 'updated_at'], excludedFromNewPlayCounts: true, creationUpperBound: oldPlays.length + creators.length };
}

export function verifyReviewedMovieTransition(before, after, manifest, seed, physical, retained) {
  const prepared = verifyRetainedMovieBefore(before, manifest, seed, retained), actor = manifest.actor.id;
  const oldPlays = before.tables.play_sessions.filter(row => row.user_id === actor), oldIds = new Set(oldPlays.map(row => row.id)), oldAuth = new Set(before.tables.sessions.map(row => row.id));
  const creators = physical.exchanges.filter(row => !row.rejected && (row.scope?.kind === 'playback_info' ||
    row.scope?.kind === 'playback_report' && /^\/Sessions\/Playing\/?$/i.test(row.scope.route)));
  need(oldPlays.length + creators.length <= 256 && after.tables.play_sessions.filter(row => row.user_id === actor).length <= 256 && oldPlays.every(row =>
    timestampNs(after.capturedAt) < timestampNs(row.expires_at) + 7n * 86400n * 1000000000n), 'retained_movie_pruning_capacity');
  need(physical.logins.every(login => !oldAuth.has(login.sessionId)) && !physical.chains.some(chain => oldIds.has(chain.play.id)), 'retained_movie_old_auth_or_play_reused');
  const expirations = [];
  for (const old of prepared) {
    const current = one(after.tables.play_sessions.filter(row => row.id === old.id), 'retained_movie_row_missing');
    if (equal(old, current)) continue;
    need(equal(without(current, ['state', 'stopped_at', 'updated_at']), without(old, ['state', 'stopped_at', 'updated_at'])) &&
      current.state === 'Expired' && current.stopped_at !== null && current.updated_at !== null, 'retained_movie_expiration_fields');
    const candidates = physical.logins.flatMap(login => login.infos.map(info => ({ login, info }))).filter(({ login, info }) => {
      const exchange = info.exchange, authentication = after.tables.sessions.filter(row => row.id === login.sessionId), next = after.tables.play_sessions.filter(row => row.id === info.value.PlaySessionId);
      return info.itemId === old.item_id && (info.body?.UserId === undefined || info.body.UserId === actor) && exchange.scope?.allowed === true &&
        exchange.scope.kind === 'playback_info' && routeItem(exchange) === old.item_id && exchange.original?.method === 'POST' && exchange.complete === true && exchange.response?.status === 200 &&
        exchange.tokenHash === login.tokenHash && authentication.length === 1 && authentication[0].user_id === actor && authentication[0].kind === 'emby' &&
        !oldIds.has(info.value.PlaySessionId) && next.length === 1 && next[0].auth_session_id === login.sessionId && next[0].user_id === actor &&
        next[0].item_id === old.item_id && next[0].media_source_id === old.media_source_id && next[0].device_id === login.device_id &&
        within(next[0].created_at, exchange.intent.startedAt, exchange.result.completedAt) &&
        within(current.stopped_at, exchange.intent.startedAt, exchange.result.completedAt) && within(current.updated_at, exchange.intent.startedAt, exchange.result.completedAt);
    });
    const { info } = one(candidates, 'reviewed_movie_prepare_window_not_unique');
    expirations.push({ playSessionId: old.id, previousState: 'Prepared', state: 'Expired', prepareOrdinal: info.exchange.ordinal,
      changedFields: ['state', 'stopped_at', 'updated_at'], excludedFromNewPlayCounts: true });
  }
  const changed = new Set(expirations.map(row => row.playSessionId));
  additions(before.tables.play_sessions, after.tables.play_sessions, undefined, changed);
  return { playSessionIds: [...changed], expirations, creationUpperBound: oldPlays.length + creators.length };
}

export function verifyTVNoPlayback(report, exchanges) {
  need(Array.isArray(report.requests) && !report.requests.some(row => ['media', 'playback_report'].includes(row.kind)) &&
    exchanges.every(row => !['media', 'playback_report'].includes(row.scope?.kind) && row.request?.kind !== 'media' &&
      !/^\/(?:emby\/)?Sessions\/Playing(?:\/|$)/i.test(row.request?.path ?? '')), 'reviewed_tv_playback_forbidden');
}

export function verifyReviewedTVTransition(before, after, manifest, seed, physical, retained) {
  const old = verifyReviewedTVBefore(before, manifest, seed, retained), actor = manifest.actor.id;
  verifyTVNoPlayback({ requests: [] }, physical.exchanges);
  need(physical.chains.length === 0 && physical.logins.every(login => login.media.length === 0) &&
    equal(before.tables.user_item_data, after.tables.user_item_data) && equal(before.tables.client_playback_references, after.tables.client_playback_references) &&
    equal(before.tables.encoding_jobs, after.tables.encoding_jobs), 'reviewed_tv_playback_state_changed');
  const oldIds = new Set(before.tables.play_sessions.map(row => row.id)), oldAuth = new Set(before.tables.sessions.map(row => row.id));
  need(physical.logins.every(login => !oldAuth.has(login.sessionId)), 'reviewed_tv_old_credential_reused');
  const current = one(after.tables.play_sessions.filter(row => row.id === old.id), 'reviewed_tv_old_preparation_missing');
  const creations = after.tables.play_sessions.filter(row => !oldIds.has(row.id));
  need(creations.length <= 1 && creations.every(row => row.user_id === actor && row.item_id === old.item_id && row.media_source_id === old.media_source_id &&
    row.duration_ticks === old.duration_ticks && row.position_ticks === 0 && row.state === 'Prepared' && row.counted === false && row.started_at === null &&
    row.stopped_at === null && row.application_client_id === null && row.client_correlated === false), 'reviewed_tv_new_preparation_invalid');
  const infos = physical.logins.flatMap(login => login.infos.map(info => ({ login, info })));
  need(infos.every(({ info }) => info.itemId === old.item_id), 'reviewed_tv_other_detail_preparation');
  const preparationWindows = [];
  for (const row of creations) {
    const candidates = infos.filter(({ login, info }) => {
      const exchange = info.exchange, sessions = after.tables.sessions.filter(session => session.id === login.sessionId);
      return info.value.PlaySessionId === row.id && (info.body?.UserId === undefined || info.body.UserId === actor) &&
        exchange.scope?.allowed === true && exchange.scope.kind === 'playback_info' && routeItem(exchange) === old.item_id && exchange.original?.method === 'POST' &&
        exchange.complete === true && exchange.response?.status === 200 && exchange.tokenHash === login.tokenHash && sessions.length === 1 &&
        sessions[0].user_id === actor && sessions[0].kind === 'emby' && row.auth_session_id === login.sessionId && row.device_id === login.device_id &&
        within(row.created_at, exchange.intent.startedAt, exchange.result.completedAt) && within(row.updated_at, exchange.intent.startedAt, exchange.result.completedAt);
    });
    const { info } = one(candidates, 'reviewed_tv_creation_window_not_unique');
    need(timestampNs(row.expires_at) > timestampNs(row.created_at), 'reviewed_tv_new_preparation_expiry');
    preparationWindows.push(info.exchange);
  }
  const expirations = [];
  if (!equal(current, old)) {
    need(equal(without(current, ['state', 'stopped_at', 'updated_at']), without(old, ['state', 'stopped_at', 'updated_at'])) &&
      current.state === 'Expired' && current.stopped_at !== null && current.updated_at !== null, 'reviewed_tv_expiration_fields');
    const exchange = one(preparationWindows.filter(row => within(current.stopped_at, row.intent.startedAt, row.result.completedAt) &&
      within(current.updated_at, row.intent.startedAt, row.result.completedAt)), 'reviewed_tv_expiration_window_not_unique');
    expirations.push({ playSessionId: old.id, previousState: 'Prepared', state: 'Expired', prepareOrdinal: exchange.ordinal,
      changedFields: ['state', 'stopped_at', 'updated_at'], excludedFromNewPlayCounts: true });
  }
  const creators = physical.exchanges.filter(row => !row.rejected && row.scope?.kind === 'playback_info');
  need(before.tables.play_sessions.filter(row => row.user_id === actor).length + creators.length <= 256 &&
    timestampNs(after.capturedAt) < timestampNs(old.expires_at) + 7n * 86400n * 1000000000n, 'reviewed_tv_pruning_capacity');
  const changed = new Set(expirations.map(row => row.playSessionId));
  additions(before.tables.play_sessions, after.tables.play_sessions, undefined, changed);
  return { playSessionIds: [...changed], expirations, creationUpperBound: creators.length + 1 };
}

/** Match the existing native hook to retained exchanges without collecting anything. */
export function tvNativeResponseDiagnostic(report, exchanges) {
  const events = Array.isArray(report.native_rejections) && report.native_rejections.length <= 16 ? report.native_rejections : [], matches = [];
  for (const event of events) {
    if (!own(event)) continue;
    const reason = event.reason;
    if (event.diagnostic_state !== 'captured_redacted' || reason?.kind !== 'response' || event.association?.state !== 'matched' ||
        typeof event.id !== 'string' || !/^native-rejection-[1-9][0-9]*$/.test(event.id) || typeof reason.request_id !== 'string' ||
        !integer(event.association.request_ordinal) || !integer(event.elapsed_ms) || !integer(report.elapsed_ms) || event.elapsed_ms > report.elapsed_ms || !own(reason.location)) continue;
    const observed = report.requests.filter(row => row.ordinal === event.association.request_ordinal);
    if (observed.length !== 1) continue;
    const row = observed[0]; let url;
    try { url = new URL(row.url); } catch { continue; }
    if (responseRequestId(row.response_headers) !== reason.request_id || row.status !== reason.status || !integer(row.elapsed_ms) || row.elapsed_ms > event.elapsed_ms ||
        url.origin !== reason.location.origin || url.pathname !== reason.location.path || row.allowed !== true || row.origin !== 'target') continue;
    const physical = exchanges.filter(exchange => responseRequestId(exchange.response?.headers) === reason.request_id);
    if (physical.length !== 1) continue;
    const exchange = physical[0];
    if (exchange.rejected || exchange.result?.backend !== 'goby' || exchange.complete !== true || exchange.response?.status !== reason.status || exchange.scope?.allowed !== true ||
        !matchesContext(exchange, report).includes(row)) continue;
    matches.push({ nativeEventId: event.id, contextOrdinal: row.ordinal, physicalOrdinal: exchange.ordinal, requestId: reason.request_id,
      route: exchange.scope.route, status: reason.status, pageErrorAttribution: false });
  }
  return { status: matches.length ? 'response_identified' : 'inconclusive', responseMatches: matches,
    reason: matches.length ? null : events.length ? 'no_unique_response_association' : 'native_evidence_unavailable_or_no_recurrence',
    nativeCollectionComplete: report.native_rejection_diagnostics?.diagnostics_complete === true,
    pageErrorCount: Array.isArray(report.page_errors) ? report.page_errors.length : null, clientAcceptance: false };
}

export function tvDiagnosticUIResult(report, manifest, physical) {
  need(manifest.scenario === 'tv-browse' && report.cleanup?.browser_closed === true && report.cleanup.tokens_rejected === true && report.cleanup.media_stopped === true &&
    Array.isArray(report.cleanup.ui_failures ?? []) && (report.cleanup.ui_failures ?? []).length === 0 &&
    report.browser_environment?.service_workers === 'allow' && report.browser_environment.proxy_bypass === '<-loopback>' &&
    report.browser_environment.quic === 'disabled' && report.browser_environment.nonproxied_webrtc_udp === 'disabled', 'reviewed_tv_cleanup_or_environment_incomplete');
  need((report.outcome === 'scenario_completed' && report.failure === null || report.outcome === 'failed' && typeof report.failure === 'string' && /^[a-z0-9_]+$/i.test(report.failure)) &&
    Array.isArray(report.page_errors) && Array.isArray(report.observer?.pending) && report.observer.pending.length === 0 && Array.isArray(report.observer.drain_timeouts) && report.observer.drain_timeouts.length === 0,
  'reviewed_tv_observation_not_closed');
  let uiAccepted = true, uiFailure = null;
  try { verifyUI(report, manifest, physical); }
  catch (error) {
    if (!['client_ui_or_cleanup_incomplete', 'tv_browse_ui_incomplete'].includes(error.message)) throw error;
    uiAccepted = false; uiFailure = error.message;
  }
  return { uiAccepted, uiFailure, originalBrowserOutcome: report.outcome, originalBrowserFailure: report.failure,
    diagnostic: tvNativeResponseDiagnostic(report, physical.exchanges), clientAcceptance: false };
}

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
export function verifyDurable(before, after, manifest, seed, physical, retained = null) {
  for (const value of [before, after]) need(exact(value, ['capturedAt', 'tables', 'sequences']) && equal(Object.keys(value.tables).sort(), TABLES), 'source_snapshot_inventory');
  need(instant(before.capturedAt) <= instant(after.capturedAt), 'source_snapshot_order');
  const actor = manifest.actor.id, control = seed.controlQ.id, selected = selectedCandidateItem(manifest), auth = new Map(physical.logins.map(row => [row.sessionId, row]));
  need(control !== actor, 'scenario_actor_playback_baseline_not_fresh');
  let retainedExpiration = null;
  if (retained === null) need(!before.tables.play_sessions.some(row => row.user_id === actor) && !before.tables.client_playback_references.some(row => row.user_id === actor), 'scenario_actor_playback_baseline_not_fresh');
  else if (retained.review?.kind === 'audited-candidate-reviewed-tv-baseline') retainedExpiration = verifyReviewedTVTransition(before, after, manifest, seed, physical, retained);
  else retainedExpiration = verifyRetainedMovieTransition(before, after, manifest, seed, physical, retained);
  const retainedTV = retained?.review?.kind === 'audited-candidate-reviewed-tv-baseline';
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
  const plays = additions(before.tables.play_sessions, after.tables.play_sessions, undefined, new Set(retainedExpiration ? retainedExpiration.playSessionIds ?? [retainedExpiration.playSessionId] : [])),
    references = additions(before.tables.client_playback_references, after.tables.client_playback_references, referenceKey);
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
    if (retainedTV) { need(oldData.has(key) && equal(oldData.get(key), row), 'reviewed_tv_userdata_changed'); continue; }
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
  return { sessionsRevoked: sessions.length, countedPlays: plays.filter(row => row.counted).length, retainedPreparations, ...(retainedExpiration ? { retainedBaselineExpiration: retainedExpiration } : {}),
    retainedTerminalEncodingJobs: encoding.map(row => ({ id: row.id, state: row.state })), controlQUnchanged: true,
    selectedUserdata: selected ? Object.fromEntries(['item_id', 'playback_position_ticks', 'play_count', 'is_favorite', 'played', 'last_played_at', 'updated_at']
      .map(key => [key, currentData.get(actor + '/' + selected.id)?.[key]])) : null };
}

function validateBinarySuccessorInput(input) {
  need(exact(input, SUCCESSOR_INPUT_KEYS) && input.kind === 'audited-candidate-transition-input' && input.version === 2 &&
    SUCCESSOR_INPUT_KEYS.filter(key => !['kind', 'version', 'output', 'helpers', 'budgets'].includes(key)).every(key => descriptor(input[key])), 'binary_successor_input_schema');
  for (const key of ['previousEpoch', 'previousBinding', 'reviewedSummary', 'reviewedState', 'priorCloseout', 'priorSource'])
    need(equal(input[key], BINARY_SUCCESSOR[key]), 'binary_successor_input_authority');
  need(equal(input.newSourceManifest, BINARY_SUCCESSOR.sourceManifest) && input.newFullReport.path === BINARY_SUCCESSOR.fullRoot + '/report.json' &&
    input.newBinary.path === BINARY_SUCCESSOR.fullRoot + '/bin/goby-linux-amd64' &&
    input.compiledCatalog.path === BINARY_SUCCESSOR.fullRoot + '/source/internal/backuppg/catalogs/schema-28-postgresql-17.json' &&
    typeof input.output === 'string' && path.posix.dirname(input.output) === RETAINED_ROOT && /^candidate-tv-parent-transition-[0-9]{2}$/.test(path.posix.basename(input.output)) &&
    exact(input.helpers, ['seed', 'provision', 'gateway', 'admission', 'reconcile']) && Object.values(input.helpers).every(descriptor) &&
    equal(input.budgets, SUCCESSOR_LIMITS), 'binary_successor_input_scope');
  return input;
}

function binarySuccessorSessions(source) {
  need(exact(source, ['capturedAt', 'tables', 'sequences']) && exact(source.tables, TABLES) && own(source.sequences) &&
    Array.isArray(source.tables.sessions) && source.tables.sessions.length === 15, 'binary_successor_prior_source');
  instant(source.capturedAt);
  const rows = source.tables.sessions.map(row => {
    need(['admin', 'emby'].includes(row.kind) && /^[0-9a-f]{32}$/.test(row.id) && /^[0-9a-f]{32}$/.test(row.user_id) &&
      /^\\x[0-9a-f]{64}$/.test(row.token_hash), 'binary_successor_session_identity');
    instant(row.revoked_at);
    return { kind: row.kind, credentialId: row.id, tokenSha256: row.token_hash.slice(2), userId: row.user_id, revokedAt: row.revoked_at };
  });
  need(new Set(rows.map(row => row.credentialId)).size === 15 && new Set(rows.map(row => row.tokenSha256)).size === 15, 'binary_successor_session_collision');
  return rows.sort((a, b) => a.credentialId < b.credentialId ? -1 : a.credentialId > b.credentialId ? 1 : 0);
}

function validateBinarySuccessorFullReport(report, worker, input) {
  need(report?.status === 'passed' && worker?.status === 'passed' && report.mode === 'full' && worker.mode === 'full' &&
    report.archive_sha256 === BINARY_SUCCESSOR.archiveSha256 && report.scope === BINARY_SUCCESSOR.fullRoot && worker.scope === BINARY_SUCCESSOR.fullRoot &&
    report.unit_exit_code === 0 && report.recursive_cgroup_empty === true && report.existing_services_modified === false &&
    SHA.test(report.worker_report_sha256) && equal(report.worker, worker), 'binary_successor_full_report_incomplete');
  need(exact(worker.cleanup, ['only_worker_process_remains', 'owned_postgres_stopped', 'private_bind_removed', 'source_unchanged']) &&
    Object.values(worker.cleanup).every(value => value === true), 'binary_successor_full_cleanup');
  const expected = worker.expected_packages, packages = worker.packages;
  need(Array.isArray(expected) && expected.length === 25 && expected.every(value => typeof value === 'string') && new Set(expected).size === 25 &&
    Array.isArray(packages) && packages.length === 25 && equal(packages.map(row => row.package).sort(), [...expected].sort()) &&
    packages.every(row => row.result === 'pass' && row.exit_code === 0 && row.failed === 0 && row.skipped === 0) &&
    integer(worker.test_counts?.passed) && worker.test_counts.passed > 0 && worker.test_counts.failed === 0 && worker.test_counts.skipped === 0,
  'binary_successor_full_package_coverage');
  need(worker.binary?.path === 'bin/goby-linux-amd64' && worker.binary.sha256 === input.newBinary.sha256 &&
    integer(worker.binary.bytes) && worker.binary.bytes > 0, 'binary_successor_full_binary');
}

function validateBinarySuccessorLineage(epoch, seed, lineage) {
  const { previousEpoch: previous, previousBinding, previousLineage, transitionInput, reviewedSummary: summary, priorCloseout, priorSource, fullReport, fullWorker } = lineage;
  need(previous?.version === 2 && previousBinding?.version === 2 && equal(epoch.previousEpoch, BINARY_SUCCESSOR.previousEpoch) &&
    equal(seed.previousBinding, BINARY_SUCCESSOR.previousBinding) && equal(previousBinding.runtimeEpoch, epoch.previousEpoch) &&
    equal(previous.previousEpoch, PREVIOUS_BINARY_EPOCH), 'binary_successor_ancestor');
  validateRuntimeLineage(previous, previousBinding, previousLineage);
  const input = validateBinarySuccessorInput(transitionInput);
  need(epoch.operationKind === 'binary_successor' && equal(epoch.calls, { stop: 1, replace: 1, start: 1 }) &&
    ['transitionInput', 'transitionHelper', 'runtimeHelper', 'productInput', 'configurationInput', 'before', 'after', 'preservation'].every(key => descriptor(epoch[key])) &&
    equal(epoch.productInput, epoch.transitionInput) && equal(epoch.configurationInput, previous.transitionInput) &&
    equal(epoch.helpers, input.helpers) && equal(epoch.helpers, previous.helpers) &&
    equal(epoch.originalProvision, previous.originalProvision) && equal(epoch.seedProvenance, previous.seedProvenance), 'binary_successor_product_configuration');
  const source = epoch.currentSource;
  need(source.archiveSha256 === BINARY_SUCCESSOR.archiveSha256 && equal(source.sourceManifest, BINARY_SUCCESSOR.sourceManifest) &&
    equal(source.fullReport, input.newFullReport) && descriptor(source.binary) && source.binary.path === previous.currentSource.binary.path &&
    source.binary.sha256 === input.newBinary.sha256 && source.binary.sha256 !== previous.currentSource.binary.sha256 &&
    source.binary.sha256 !== 'a9b25b6b3e9f04b528ca77cd0a0dd548ae4c2715c06a1a56a6def23c6e00e2d7', 'binary_successor_product_source');
  validateBinarySuccessorFullReport(fullReport, fullWorker, input);
  const current = epoch.candidate, old = previous.candidate;
  const changed = ['input', 'productInput', 'binary', 'currentSourceManifest', 'backendReport', 'processes', 'serverIdentity', 'listener', 'databases'];
  need(exact(current, Object.keys(old)) && equal(without(current, changed), without(old, changed)) &&
    equal(current.input, epoch.productInput) && equal(current.productInput, epoch.productInput) && equal(current.backendReport, source.fullReport) &&
    exact(current.processes, Object.keys(old.processes)) && equal(without(current.processes, ['server']), without(old.processes, ['server'])) &&
    equal(epoch.postgresProcess, previous.postgresProcess), 'binary_successor_candidate_configuration');
  need(['bootId', 'uid', 'exe', 'cmdline', 'networkNamespace', 'cgroup'].every(key => Object.hasOwn(previous.candidateProcess, key) &&
    equal(epoch.candidateProcess[key], previous.candidateProcess[key])), 'binary_successor_process_sandbox');
  need(exact(current.databases, Object.keys(old.databases)) && ['source', 'recovery'].every(slot => own(old.databases[slot]) &&
    exact(current.databases[slot], Object.keys(old.databases[slot])) &&
    equal(without(current.databases[slot], ['afterStart']), without(old.databases[slot], ['afterStart']))), 'binary_successor_database_identity');
  for (const key of ['reviewedState', 'reviewedSummary']) need(equal(epoch[key], BINARY_SUCCESSOR[key]) && equal(seed[key], BINARY_SUCCESSOR[key]), 'binary_successor_review_authority');
  for (const key of ['priorCloseout', 'priorSource']) need(equal(seed[key], BINARY_SUCCESSOR[key]), 'binary_successor_prior_authority');
  need(summary?.kind === 'audited-tv-parent-transition-startup-state-review' && summary.status === 'captured_state_supports_bounded_transition_contract' &&
    equal(summary.state, epoch.reviewedState) && equal(summary.runtimeEpoch, epoch.previousEpoch) && equal(summary.seedBinding, seed.previousBinding) &&
    equal(summary.priorSource, seed.priorSource) && summary.source?.ownedTablesExactToTvCloseout === 35 && summary.source.sequencesExact === true &&
    priorCloseout?.status === 'owned_state_closed_client_acceptance_pending' && priorCloseout.clientAcceptance === false &&
    equal(priorCloseout.inputEvidence?.after, seed.priorSource) && equal(priorCloseout.inputEvidence?.epoch, epoch.previousEpoch), 'binary_successor_review_binding');
  need(equal(without(seed, ['version', 'runtimeEpoch', 'currentSessions', ...SUCCESSOR_BINDING_KEYS]),
    without(previousBinding, ['version', 'runtimeEpoch', 'currentSessions', ...ENV_BINDING_KEYS])), 'binary_successor_seed_provenance');
  const sessions = binarySuccessorSessions(priorSource);
  need(equal(seed.currentSessions, sessions) && previousBinding.currentSessions.every(old => {
    if (!own(old) || !Object.hasOwn(old, 'credentialId') || typeof old.tokenSha256 !== 'string' || !SHA.test(old.tokenSha256) ||
      old.credentialId !== null && (typeof old.credentialId !== 'string' || !/^[0-9a-f]{32}$/.test(old.credentialId))) return false;
    const matches = sessions.filter(row => row.kind === old.kind && row.tokenSha256 === old.tokenSha256);
    // Initial seed cleanup recorded literal null IDs; only their unique
    // kind/token binding may supply the already verified current credential ID.
    return matches.length === 1 && (old.credentialId === null || matches[0].credentialId === old.credentialId);
  }), 'binary_successor_session_history');
}

export function validateRuntimeLineage(epoch, seed, lineage = {}) {
  need([1, 2, 3].includes(epoch.version) && exact(epoch, [...EPOCH_KEYS, ...(epoch.version === 2 ? ENV_EPOCH_KEYS : epoch.version === 3 ? SUCCESSOR_EPOCH_KEYS : [])]) &&
    epoch.kind === 'audited-candidate-runtime-epoch' && epoch.status === 'running_awaiting_live_acceptance' && epoch.candidateAdmissionComplete === false &&
    seed.version === epoch.version && exact(seed, [...BINDING_KEYS, ...(seed.version === 2 ? ENV_BINDING_KEYS : seed.version === 3 ? SUCCESSOR_BINDING_KEYS : [])]) &&
    seed.kind === 'audited-candidate-seed-runtime-binding' && seed.candidateAdmissionComplete === false, 'runtime_lineage_schema');
  need(exact(epoch.currentSource, ['archiveSha256', 'sourceManifest', 'binary', 'fullReport', 'schema']) && epoch.currentSource.schema === 28 &&
    epoch.candidate.bootstrapExecuted === true && equal(epoch.candidate.sourceState, { users: 8, schema: 28, migrations: 28 }) &&
    equal(epoch.candidate.binary, epoch.currentSource.binary) && equal(epoch.candidate.currentSourceManifest, epoch.currentSource.sourceManifest), 'runtime_lineage_product');
  if (epoch.version === 1) { need(equal(epoch.calls, { stop: 1, replace: 1, start: 1 }) && seed.currentSessions.length === 3, 'binary_epoch_calls_or_sessions'); return; }
  if (epoch.version === 3) return validateBinarySuccessorLineage(epoch, seed, lineage);
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

/** A historical admission proves its own epoch; successors also need fresh TV checks. */
export function validateCandidateAdmissionBinding(input, epoch, admission, lineage = {}, reusedAdmission04 = null) {
  need([1, 2, 3].includes(epoch.version) && admission?.kind === 'audited-candidate-live-admission' &&
    admission.version === (epoch.version === 3 ? 3 : 2) && admission.status === 'admitted_for_core_client' &&
    admission.candidateAdmissionComplete === true && admission.failure === null && equal(admission.cleanupFailures, []) &&
    equal(admission.runtimeEpoch, input.runtimeEpoch) && equal(admission.seedRuntimeBinding, input.seedBinding) && equal(admission.currentSource, epoch.currentSource),
  'candidate_live_admission_binding');
  if (epoch.version !== 3) return;
  need(admission.admissionKind === 'affected_tv_parent' && admission.clientAcceptance === false && exact(admission.freshChecks, AFFECTED_TV_CHECKS) &&
    Object.values(admission.freshChecks).every(value => value === true) && equal(admission.transitionCloseout, AFFECTED_TV_TRANSITION_CLOSEOUT),
  'candidate_affected_tv_admission_checks');
  const reused = admission.reusedAdmission04, previous = lineage.previousEpoch, previousBinding = lineage.previousBinding;
  need(exact(reused, ['report', 'runtimeEpoch', 'seedRuntimeBinding', 'currentSource', 'contracts']) && equal(reused.report, REUSED_ADMISSION04) &&
    equal(reused.contracts, REUSED_ADMISSION_CONTRACTS) && equal(reused.runtimeEpoch, BINARY_SUCCESSOR.previousEpoch) &&
    equal(reused.seedRuntimeBinding, BINARY_SUCCESSOR.previousBinding) && equal(epoch.previousEpoch, reused.runtimeEpoch) &&
    previous?.version === 2 && previousBinding?.version === 2 && equal(previousBinding.runtimeEpoch, reused.runtimeEpoch) &&
    equal(reused.currentSource, previous.currentSource), 'candidate_reused_admission04_binding');
  validateCandidateAdmissionBinding({ runtimeEpoch: reused.runtimeEpoch, seedBinding: reused.seedRuntimeBinding }, previous, reusedAdmission04);
  need(reusedAdmission04.clientAcceptance === false && reusedAdmission04.inactiveStageRetained === true && reusedAdmission04.activeGenerationChanged === false &&
    reusedAdmission04.playbackRequests === 0 && reusedAdmission04.applyRequests === 0 && reusedAdmission04.rollbackRequests === 0 &&
    exact(reusedAdmission04.controllerSessions, ['admin', 'P', 'Q']) && Object.values(reusedAdmission04.controllerSessions).every(row =>
      /^[0-9a-f]{32}$/.test(row.credentialId) && SHA.test(row.tokenSha256) && row.sameTokenRejected === true), 'candidate_reused_admission04_cleanup');
}

export function validateCurrentRuntime(envelope, input, epoch, authorizedInput, records) {
  need(input.version === 5 && authorizedInput?.version === 5 && authorizedInput.scenario === 'tv-browse' && descriptor(input.currentRuntime) &&
    equal(authorizedInput.currentRuntime, input.currentRuntime) && exact(envelope, ['kind', 'version', 'status', 'runtimeEpoch', 'seedBinding', 'admission', 'admissionCloseout', 'hosting',
      'recovery', 'current', 'preserved', 'observation', 'observationReview']) && envelope.kind === 'audited-candidate-current-runtime-binding' && envelope.version === 1 &&
    envelope.status === 'reviewed_current_runtime' && epoch.version === 3, 'current_runtime_authority');
  for (const key of ['runtimeEpoch', 'seedBinding', 'admission', 'admissionCloseout', 'hosting']) need(descriptor(envelope[key]) &&
    equal(envelope[key], ['admissionCloseout', 'hosting'].includes(key) ? authorizedInput[key] : input[key]), 'current_runtime_historical_binding');
  need(exact(envelope.recovery, ['execution', 'independentReview', 'configuration', 'selectedStartIntent', 'selectedResult']) && Object.values(envelope.recovery).every(descriptor) &&
    descriptor(envelope.observation) && descriptor(envelope.observationReview) && equal(envelope.preserved, Object.fromEntries(['binary', 'runtime', 'units'].map(key => [key, epoch.candidate[key]]))), 'current_runtime_preserved_source');
  const current = envelope.current, candidate = epoch.candidate, previous = epoch.candidateProcess;
  need(exact(current, ['candidateProcess', 'serverProperties', 'serverIdentity', 'listener', 'postgresProcess', 'postgresProperties', 'lease']), 'current_runtime_identity_schema');
  const process = current.candidateProcess;
  need(exact(process, Object.keys(previous)) && equal(without(process, ['pid', 'startTicks']), without(previous, ['pid', 'startTicks'])) &&
    integer(process.pid) && process.pid > 1 && process.pid !== previous.pid && typeof process.startTicks === 'string' && /^[1-9][0-9]*$/.test(process.startTicks) &&
    ns(process.startTicks) > ns(previous.startTicks), 'current_runtime_process_boundary');
  const properties = current.serverProperties, oldProperties = candidate.processes.server;
  need(exact(properties, Object.keys(oldProperties)) && equal(without(properties, ['MainPID', 'InvocationID']), without(oldProperties, ['MainPID', 'InvocationID'])) &&
    properties.MainPID === String(process.pid) && /^[a-f0-9]{32}$/.test(properties.InvocationID) && properties.InvocationID !== oldProperties.InvocationID &&
    equal(current.serverIdentity, { ...candidate.serverIdentity, pid: process.pid, startTicks: process.startTicks, invocationId: properties.InvocationID }), 'current_runtime_server_identity');
  need(exact(current.listener, Object.keys(candidate.listener)) && equal(without(current.listener, ['pid', 'socketInode']), without(candidate.listener, ['pid', 'socketInode'])) &&
    current.listener.pid === process.pid && typeof current.listener.socketInode === 'string' && /^[1-9][0-9]*$/.test(current.listener.socketInode) &&
    equal(current.postgresProcess, epoch.postgresProcess) && equal(current.postgresProperties, candidate.processes.postgres), 'current_runtime_listener_or_postgres');
  const lease = current.lease;
  need(exact(lease, ['backendPid', 'user', 'database', 'application', 'clientHost', 'clientPort', 'backendStart', 'mode', 'granted', 'candidateConnection']) &&
    equal(without(lease, ['backendPid', 'backendStart', 'clientPort', 'candidateConnection']), without(epoch.lease, ['backendPid', 'backendStart', 'clientPort', 'candidateConnection'])) &&
    integer(lease.backendPid) && lease.backendPid > 1 && integer(lease.clientPort) && lease.clientPort > 0 && lease.clientPort < 65536 &&
    lease.granted === true && lease.mode === 'ExclusiveLock' && exact(lease.candidateConnection, ['pid', 'localPort', 'remotePort', 'socketInode']) &&
    lease.candidateConnection.pid === process.pid && lease.candidateConnection.localPort === lease.clientPort && lease.candidateConnection.remotePort === candidate.ports.postgres &&
    typeof lease.candidateConnection.socketInode === 'string' && /^[1-9][0-9]*$/.test(lease.candidateConnection.socketInode), 'current_runtime_lease_identity');
  const observation = records?.observation, review = records?.observationReview;
  need(exact(observation, ['kind', 'version', 'runtimeEpoch', 'recoveryExecution', 'hosting', 'source', 'verification', 'capturedAt', 'before', 'after', 'preserved', 'hostingBefore', 'hostingAfter', 'leaseQueryResult', 'calls']) &&
    observation.kind === 'audited-candidate-current-runtime-observation' && observation.version === 1 && equal(observation.runtimeEpoch, envelope.runtimeEpoch) &&
    equal(observation.recoveryExecution, envelope.recovery.execution) && equal(observation.hosting, envelope.hosting) && equal(observation.before, current) && equal(observation.after, current) &&
    equal(observation.preserved, envelope.preserved) && timestampNs(observation.capturedAt) >= timestampNs(lease.backendStart) &&
    equal(observation.calls, { sql: 2, http: 0, browser: 0, service: 0, seed: 0, admission: 0, provision: 0 }), 'current_runtime_observation_binding');
  const host = records.hosting, hostFields = 'Id LoadState ActiveState SubState MainPID InvocationID Result ExecMainStatus ControlGroup NRestarts'.split(' ');
  need(own(host?.process) && own(host.listener) && own(host.unitProperties), 'current_runtime_hosting_record');
  const hostIdentity = { process: host.process, listener: host.listener, unit: Object.fromEntries(hostFields.map(key => [key, host.unitProperties[key]])) };
  need(equal(observation.hostingBefore, hostIdentity) && equal(observation.hostingAfter, hostIdentity) &&
    equal(records.leaseQueryResult, [without(lease, ['candidateConnection'])]), 'current_runtime_hosting_or_lease_observation');
  need(exact(review, ['kind', 'version', 'status', 'observation', 'source', 'verification', 'checks', 'calls']) &&
    review.kind === 'audited-candidate-current-runtime-observation-review' && review.version === 1 && review.status === 'passed' &&
    equal(review.observation, envelope.observation) && descriptor(observation.source) && descriptor(observation.verification) &&
    equal(review.source, observation.source) && equal(review.verification, observation.verification) &&
    exact(review.checks, ['recordPins', 'sourceAndVerification', 'currentIdentity', 'recoveryBinding', 'preservedHashes', 'hostingContinuity', 'uniqueLease', 'privateEvidence']) &&
    Object.values(review.checks).every(value => value === true) && equal(review.calls, { sql: 0, http: 0, browser: 0, service: 0, seed: 0, admission: 0, provision: 0 }), 'current_runtime_independent_review');
  return current;
}

export function validateCloseoutBindings(input, evidence) {
  const { manifest, observation, summary, gatewayAttestation: gateway, runtimeEpoch: epoch, admission, seedBinding: seed, boundary } = evidence;
  validateCandidateManifest(manifest);
  if (input.version === 5) {
    need(descriptor(input.retainedBaseline) && descriptor(input.currentRuntime) && equal(evidence.retained?.inputBinding, { runtimeEpoch: input.runtimeEpoch, seedBinding: input.seedBinding }), 'reviewed_tv_current_binding');
    verifyReviewedTVBefore(evidence.sourceBefore, manifest, seed, evidence.retained);
  } else if (input.version === 4) {
    need(descriptor(input.retainedBaseline) && equal(evidence.retained?.inputBinding, { runtimeEpoch: input.runtimeEpoch, seedBinding: input.seedBinding }), 'reviewed_movie_current_binding');
    verifyRetainedMovieBefore(evidence.sourceBefore, manifest, seed, evidence.retained);
  } else if (input.version === 2 || input.version === 3 && input.retainedBaseline !== null) {
    need(equal(input.retainedBaseline, input.version === 2 ? RETAINED_BASELINE : OCCUPIED_BASELINE) && exact(evidence.retained, ['closeout', 'snapshot']), 'retained_movie_input_authority');
    verifyRetainedMovieBefore(evidence.sourceBefore, manifest, seed, evidence.retained);
  } else need((input.version === 1 && !Object.hasOwn(input, 'retainedBaseline') || input.version === 3 && input.retainedBaseline === null && manifest.scenario !== 'movie') &&
    evidence.retained === undefined, 'retained_movie_input_authority');
  need(equal(observation.gateway?.admission_request_counts, { normal: 0, cleanup: 0 }), 'gateway_not_fresh_at_client_admission');
  validateCandidateGateway(gateway, manifest, ns(observation.started_monotonic_ns), { normal: 0, cleanup: 0 });
  validateRuntimeLineage(epoch, seed, evidence.lineage);
  validateCandidateAdmissionBinding(input, epoch, admission, evidence.lineage, evidence.reusedAdmission04);
  const current = input.version === 5 ? validateCurrentRuntime(evidence.currentRuntime, input, epoch, evidence.authorizedInput, evidence.currentRuntimeRecords) :
    { candidateProcess: epoch.candidateProcess, postgresProcess: epoch.postgresProcess, lease: epoch.lease, listener: epoch.candidate.listener };
  if (input.version === 5) need(timestampNs(evidence.currentRuntimeRecords.observation.capturedAt) <= timestampNs(observation.started_at), 'current_runtime_observation_after_client');
  const currentSource = { manifestSha256: epoch.currentSource.sourceManifest.sha256, binarySha256: epoch.currentSource.binary.sha256, schema: epoch.currentSource.schema };
  need(equal(manifest.source, currentSource) && equal(without(manifest.processes.candidate, ['listener']), current.candidateProcess) &&
    manifest.processes.candidate.listener.port === current.listener.port && manifest.processes.candidate.listener.socketInode === current.listener.socketInode,
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
    equal(gateway.upstreams.goby.process, current.candidateProcess) && equal(gateway.upstreams.goby.listener, manifest.processes.candidate.listener) &&
    gateway.upstreams.goby.executableSha256 === currentSource.binarySha256 && equal(gateway.sources.gateway, input.sources.gateway) && equal(gateway.sources.proxy, input.sources.proxy) &&
    gateway.policy.version === 'core-av-transparent-gateway-v1' && equal(gateway.policy.allowedOrigins, [manifest.browserOrigin]) &&
    observation.gateway.attestation_sha256 === input.gatewayAttestation.sha256 && observation.gateway.input_sha256 === gateway.inputSha256, 'gateway_runtime_or_policy_binding');
  need(exact(boundary, ['kind', 'version', 'runId', 'runtimeEpoch', 'sourceBefore', 'sourceAfter', 'candidateBefore', 'candidateAfter', 'postgresBefore', 'postgresAfter',
    'leaseBefore', 'leaseAfter', 'database', 'beforeMonotonicNs', 'afterMonotonicNs', 'clientWorker', 'gatewayWorker', ...(input.version === 5 ? ['currentRuntime'] : [])]) &&
    boundary.kind === 'audited-candidate-client-boundary' && boundary.version === (input.version === 5 ? 2 : 1) && boundary.runId === manifest.runId &&
    (input.version !== 5 || equal(boundary.currentRuntime, input.currentRuntime)) &&
    equal(boundary.runtimeEpoch, input.runtimeEpoch) && equal(boundary.sourceBefore, input.sourceBefore) && equal(boundary.sourceAfter, input.sourceAfter) &&
    equal(boundary.candidateBefore, manifest.processes.candidate) && equal(boundary.candidateAfter, manifest.processes.candidate) &&
    equal(boundary.postgresBefore, current.postgresProcess) && equal(boundary.postgresAfter, current.postgresProcess) &&
    equal(boundary.leaseBefore, current.lease) && equal(boundary.leaseAfter, current.lease) && equal(boundary.database, epoch.candidate.database), 'source_capture_epoch_binding');
  need(exact(boundary.clientWorker, ['exitCode', 'mainPID', 'workerPidAbsent', 'remainingBrowserPids']) && exact(boundary.gatewayWorker, ['exitCode', 'mainPID', 'workerPidAbsent', 'index']) &&
    boundary.clientWorker.exitCode === (input.version === 5 && observation.outcome === 'failed' ? 1 : 0) && boundary.clientWorker.mainPID === 0 && boundary.clientWorker.workerPidAbsent === true &&
    equal(boundary.clientWorker.remainingBrowserPids, []) && boundary.gatewayWorker.exitCode === 0 && boundary.gatewayWorker.mainPID === 0 &&
    boundary.gatewayWorker.workerPidAbsent === true && equal(boundary.gatewayWorker.index, input.gatewayIndex), 'workers_not_independently_closed');
  need(ns(boundary.beforeMonotonicNs) <= ns(observation.started_monotonic_ns) &&
    ns(boundary.afterMonotonicNs) >= ns(observation.started_monotonic_ns) + BigInt(observation.elapsed_ms) * 1000000n, 'boundary_time_interval');
  const serverLog = [3, 4, 5].includes(input.version) ? validateServerLog(evidence.serverLog, evidence.serverLogBefore, evidence.serverLogAfter,
    { manifest, epoch, input, approval: evidence.approval, observation, authorizedInput: evidence.authorizedInput, currentIdentity: current }) : null;
  if (serverLog) need(timestampNs(serverLog.receipt.before.capturedAt) >= timestampNs(evidence.sourceBefore.capturedAt) &&
    timestampNs(serverLog.receipt.after.capturedAt) >= timestampNs(evidence.sourceAfter.capturedAt), 'server_log_source_capture_order');
  return { serverLog };
}

export function closeEvidence(input, evidence, ledgerRows) {
  const bindings = validateCloseoutBindings(input, evidence);
  const manifest = evidence.manifest, report = evidence.observation;
  const exchanges = verifyLedgerRows(evidence.gatewayAttestation, evidence.gatewayIndex, ledgerRows, manifest);
  need(exchanges.length > 0 && exchanges.every(row => ns(row.intent.startedMonotonicNs) >= ns(evidence.boundary.beforeMonotonicNs) &&
    ns(row.result.completedMonotonicNs) <= ns(evidence.boundary.afterMonotonicNs)), 'ledger_outside_capture_interval');
  if (input.version === 5) verifyTVNoPlayback(report, exchanges);
  const physical = analyzePhysical(exchanges, manifest, report, evidence.sourceAfter, bindings.serverLog);
  const diagnosticResult = input.version === 5 ? tvDiagnosticUIResult(report, manifest, physical) : null;
  const partialMedia = input.version === 5 ? [] : verifyUI(report, manifest, physical);
  const partialOrdinals = new Set(partialMedia.map(row => row.ordinal));
  for (const row of exchanges) if (!row.rejected && !row.complete && row.request.kind !== 'websocket')
    need(partialOrdinals.has(row.ordinal) || row.request.kind === 'web' && row.original.method === 'GET' && matchesContext(row, report).some(event => event.failed && event.failure_error_text === 'net::ERR_ABORTED'), 'unexplained_upstream_interruption');
  const durable = verifyDurable(evidence.sourceBefore, evidence.sourceAfter, manifest, evidence.seedBinding, physical, evidence.retained ?? null);
  return { kind: 'audited-candidate-client-closeout', version: 1, status: input.version === 5 ? 'owned_state_closed_diagnostic_result' : 'core_scenario_closed',
    ...(diagnosticResult ?? {}), runId: manifest.runId, scenario: manifest.scenario,
    source: manifest.source, physicalRequests: exchanges.length, logins: physical.logins.length,
    lifecycles: physical.chains.map(chain => ({ playSessionId: chain.play.id, authSessionId: chain.login.sessionId, itemId: chain.play.item_id,
      mediaSourceId: chain.play.media_source_id, preparation: chain.preparation, startedOrdinals: chain.started.map(row => row.exchange.ordinal), progressOrdinals: chain.progress.map(row => row.exchange.ordinal),
      stoppedOrdinals: chain.stopped.map(row => row.exchange.ordinal), effectiveStopOrdinals: chain.effectiveStopOrdinals,
      countingReportOrdinals: chain.countingReports.map(row => row.ordinal), mediaGetOrdinals: chain.media.map(row => row.ordinal) })),
    logoutVerification: physical.logins.map(login => ({ authSessionId: login.sessionId, loginOrdinal: login.login.ordinal, logoutOrdinal: login.logout.ordinal,
      rejectionOrdinal: login.verification.ordinal, rejectionStatus: 401, rejectionBodyReconstructed: login.verification.responseEntity !== null })),
    partialMedia, cancelledEmptyResponses: physical.cancelledEmptyResponses, durable, auxiliaryRejections: exchanges.filter(row => row.response?.status >= 400 && row.scope?.kind !== 'login' && row.original.get('user-agent') !== 'GobyBrowserPostLogoutVerification/1')
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

export function validateCloseoutInput(input) {
  need([1, 2, 3, 4, 5].includes(input.version) && exact(input, ['kind', 'version', ...CLOSEOUT_PINS, 'sources', 'output', ...(input.version >= 2 ? ['retainedBaseline'] : []),
    ...([3, 4, 5].includes(input.version) ? ['serverLog'] : []), ...(input.version === 5 ? ['currentRuntime'] : [])]) &&
    input.kind === 'audited-candidate-client-closeout-input' && CLOSEOUT_PINS.every(key => descriptor(input[key])) &&
    exact(input.sources, Object.keys(SOURCE_FILES)) && typeof input.output === 'string' && input.output.startsWith('/opt/goby-test/'), 'closeout_input_invalid');
  if (input.version === 2) need(equal(input.retainedBaseline, RETAINED_BASELINE), 'retained_movie_input_authority');
  if (input.version === 3) need((input.retainedBaseline === null || equal(input.retainedBaseline, OCCUPIED_BASELINE)) && descriptor(input.serverLog), 'version3_evidence_authority');
  if (input.version === 4) need(descriptor(input.retainedBaseline) && descriptor(input.serverLog), 'version4_evidence_authority');
  if (input.version === 5) need(descriptor(input.retainedBaseline) && descriptor(input.serverLog) && descriptor(input.currentRuntime), 'version5_evidence_authority');
  return input;
}

async function readEnvironmentLineage(epoch, seed) {
  need(equal(epoch.previousEpoch, PREVIOUS_BINARY_EPOCH), 'previous_binary_epoch_reader_authority');
  const lineage = { previousEpoch: previousBinaryEpochJSON(await readPin(epoch.previousEpoch), epoch.previousEpoch), previousBinding: strictJSON(await readPin(seed.previousBinding)),
    configurationInput: strictJSON(await readPin(epoch.transitionInput)), failedAdmission03: strictJSON(await readPin(seed.admission03)) };
  await readPin(epoch.productInput);
  // Only this fixed public receipt permits its recorded 0644 mode.
  await readPin(seed.failureCloseout, MAX_FILE, !equal(seed.failureCloseout, PUBLIC_ADMISSION03_CLOSEOUT));
  await readPin(seed.closedState);
  return lineage;
}

export async function readBinarySuccessorLineage(epoch, seed) {
  need(equal(epoch.previousEpoch, BINARY_SUCCESSOR.previousEpoch) && equal(seed.previousBinding, BINARY_SUCCESSOR.previousBinding) &&
    equal(epoch.reviewedState, BINARY_SUCCESSOR.reviewedState) && equal(epoch.reviewedSummary, BINARY_SUCCESSOR.reviewedSummary) &&
    equal(seed.priorSource, BINARY_SUCCESSOR.priorSource) && equal(seed.priorCloseout, BINARY_SUCCESSOR.priorCloseout), 'binary_successor_reader_authority');
  const previousEpoch = runtimeEpochJSON(await readPin(epoch.previousEpoch), epoch.previousEpoch), previousBinding = strictJSON(await readPin(seed.previousBinding));
  need(previousEpoch.version === 2 && previousBinding.version === 2, 'binary_successor_ancestor');
  const previousLineage = await readEnvironmentLineage(previousEpoch, previousBinding);
  const transitionInput = validateBinarySuccessorInput(strictJSON(await readPin(epoch.transitionInput)));
  const fullReport = strictJSON(await readPin(transitionInput.newFullReport, MAX_FILE, false));
  const fullWorker = strictJSON(await readPin({ path: BINARY_SUCCESSOR.fullRoot + '/worker-report.json', sha256: fullReport.worker_report_sha256 }, MAX_FILE, false));
  // These fixed/private proofs contain filesystem nanoseconds. Their bytes are
  // authenticated here; the Python runtime owns their full preservation check.
  for (const pin of [epoch.reviewedState, epoch.before, epoch.after, epoch.preservation]) await readPin(pin);
  for (const pin of [transitionInput.newSourceManifest, transitionInput.compiledCatalog, transitionInput.frontendReport]) await readPin(pin, MAX_FILE, false);
  return { previousEpoch, previousBinding, previousLineage, transitionInput, fullReport, fullWorker,
    reviewedSummary: strictJSON(await readPin(epoch.reviewedSummary)), priorCloseout: strictJSON(await readPin(seed.priorCloseout)),
    priorSource: sourceSnapshotJSON(await readPin(seed.priorSource)) };
}

export async function readReviewedMovieBaseline(reviewPin, inputBinding, seed) {
  const review = validateReviewedMovieBaselineInput(strictJSON(await readPin(reviewPin)));
  const readState = async pins => ({ closeout: strictJSON(await readPin(pins.closeout)), snapshot: sourceSnapshotJSON(await readPin(pins.snapshot)),
    sourceEpoch: runtimeEpochJSON(await readPin(pins.sourceEpoch), pins.sourceEpoch), manifest: strictJSON(await readPin(pins.manifest)) });
  const retained = { review, ...await readState(review), movieProvenance: await readState(review.movieProvenance),
    boundary: review.boundary === null ? null : strictJSON(await readPin(review.boundary)), inputBinding };
  validateReviewedMovieBaseline(retained, seed);
  return retained;
}

export async function readReviewedTVBaseline(reviewPin, inputBinding, seed) {
  const review = validateReviewedTVBaselineInput(strictJSON(await readPin(reviewPin)));
  const readState = async pins => ({ closeout: strictJSON(await readPin(pins.closeout)), snapshot: sourceSnapshotJSON(await readPin(pins.snapshot)),
    sourceEpoch: runtimeEpochJSON(await readPin(pins.sourceEpoch), pins.sourceEpoch), manifest: strictJSON(await readPin(pins.manifest)) });
  const retained = { review, ...await readState(review), tvProvenance: await readState(review.tvProvenance),
    boundary: strictJSON(await readPin(review.boundary)), postBrowseAdmission: strictJSON(await readPin(review.postBrowseAdmission)), inputBinding };
  validateReviewedTVBaseline(retained, seed);
  return retained;
}

export async function runCloseout(inputPin) {
  need(process.platform === 'linux' && process.getuid?.() === 0 && process.env.SSH_CONNECTION, 'remote_only_closeout'); process.umask(0o077);
  const input = validateCloseoutInput(strictJSON(await readPin(inputPin)));
  const names = CLOSEOUT_PINS;
  const ownDirectory = path.dirname(fileURLToPath(import.meta.url));
  for (const [key, filename] of Object.entries(SOURCE_FILES)) { need(path.basename(input.sources[key].path) === filename &&
    (!filename.endsWith('.mjs') || input.sources[key].path === path.join(ownDirectory, filename)), 'closeout_source_path'); await readPin(input.sources[key], MAX_FILE, false); }
  await protectedDirectory(input.output, true); need((await fs.readdir(input.output)).length === 0, 'fresh_closeout_output_required');
  const evidence = {}; for (const key of names) evidence[key] = ['sourceBefore', 'sourceAfter'].includes(key)
    ? sourceSnapshotJSON(await readPin(input[key])) : key === 'runtimeEpoch' ? runtimeEpochJSON(await readPin(input[key]), input[key]) :
      key === 'observation' ? observationJSON(await readPin(input[key]), evidence.manifest) : strictJSON(await readPin(input[key]));
  if (input.version === 5) {
    evidence.currentRuntime = strictJSON(await readPin(input.currentRuntime));
    const current = evidence.currentRuntime;
    need(exact(current.recovery, ['execution', 'independentReview', 'configuration', 'selectedStartIntent', 'selectedResult']), 'current_runtime_recovery_schema');
    evidence.currentRuntimeRecords = {};
    for (const key of ['observation', 'observationReview', 'hosting']) evidence.currentRuntimeRecords[key] = strictJSON(await readPin(current[key], MAX_FILE, false));
    const observed = evidence.currentRuntimeRecords.observation;
    evidence.currentRuntimeRecords.leaseQueryResult = strictJSON(await readPin(observed.leaseQueryResult, MAX_FILE, false));
    for (const pin of Object.values(current.recovery)) await readPin(pin, MAX_FILE, false);
    await readPin(observed.source, MAX_FILE, false); await readPin(observed.verification, MAX_FILE, false);
    evidence.retained = await readReviewedTVBaseline(input.retainedBaseline, { runtimeEpoch: input.runtimeEpoch, seedBinding: input.seedBinding }, evidence.seedBinding);
  } else if (input.version === 4) {
    evidence.retained = await readReviewedMovieBaseline(input.retainedBaseline, { runtimeEpoch: input.runtimeEpoch, seedBinding: input.seedBinding }, evidence.seedBinding);
  } else if (input.version >= 2 && input.retainedBaseline !== null) {
    const closeout = strictJSON(await readPin(input.retainedBaseline));
    const snapshot = input.version === 2 ? closeout.sourceAfter : closeout.inputEvidence?.after;
    need(equal(snapshot, input.version === 2 ? RETAINED_SNAPSHOT : OCCUPIED_SNAPSHOT), 'retained_movie_snapshot_changed');
    evidence.retained = { closeout, snapshot: sourceSnapshotJSON(await readPin(snapshot)) };
  }
  if ([3, 4, 5].includes(input.version)) {
    evidence.serverLog = strictJSON(await readPin(input.serverLog));
    evidence.serverLogBefore = await readPin(evidence.serverLog.before.content);
    evidence.serverLogAfter = await readPin(evidence.serverLog.after.content);
    evidence.approval = strictJSON(await readPin(evidence.manifest.approval));
    evidence.authorizedInput = strictJSON(await readPin(evidence.serverLog.input));
    await readPin(evidence.serverLog.controller, MAX_FILE, false);
  }
  if (evidence.runtimeEpoch.version === 2) evidence.lineage = await readEnvironmentLineage(evidence.runtimeEpoch, evidence.seedBinding);
  if (evidence.runtimeEpoch.version === 3) {
    evidence.lineage = await readBinarySuccessorLineage(evidence.runtimeEpoch, evidence.seedBinding);
    need(equal(evidence.admission.reusedAdmission04?.report, REUSED_ADMISSION04) &&
      equal(evidence.admission.transitionCloseout, AFFECTED_TV_TRANSITION_CLOSEOUT), 'candidate_affected_admission_reader_authority');
    evidence.reusedAdmission04 = strictJSON(await readPin(REUSED_ADMISSION04));
    await readPin(AFFECTED_TV_TRANSITION_CLOSEOUT, MAX_FILE, false);
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
    ...(input.version === 5 && result.status === 'owned_state_closed_diagnostic_result' ? { clientAcceptance: false, uiAccepted: result.uiAccepted,
      originalBrowserOutcome: result.originalBrowserOutcome, originalBrowserFailure: result.originalBrowserFailure, diagnostic: result.diagnostic } : {}),
    physicalRequests: result.physicalRequests ?? null, logins: result.logins ?? null, lifecycles: result.lifecycles?.length ?? null, sqlCalls: 0, httpCalls: 0, serviceCalls: 0 };
  await saveNew(path.join(input.output, 'summary.json'), summary); return summary;
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  try { const args = process.argv.slice(2); need(args.length === 4 && args[0] === '--input' && args[2] === '--input-sha256', 'closeout_arguments');
    const summary = await runCloseout({ path: args[1], sha256: args[3] }); process.stdout.write(JSON.stringify(summary) + '\n');
    if (!['core_scenario_closed', 'owned_state_closed_diagnostic_result'].includes(summary.status)) process.exitCode = 1;
  } catch (error) { process.stderr.write(JSON.stringify({ status: 'incomplete', failedCheck: /^[a-z0-9_]+$/.test(error.message ?? '') ? error.message : 'closeout_input_or_receipt_unavailable',
    sqlCalls: 0, httpCalls: 0, serviceCalls: 0 }) + '\n'); process.exitCode = 1; }
}
