#!/usr/bin/env node
/** Observe the original reference client without treating non-observation as acceptance. */
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { createReferenceBrowserActor, sanitizeDiagnostic, REFERENCE_HTTP_PHASES, REFERENCE_HTTP_ERROR_CODES,
  REFERENCE_REQUEST_CONTENT_TYPES } from './client-browser-library-changed-reference-runtime.mjs';

const WORK = '/opt/goby-test/exec-work-m3e';
export const REFERENCE_TOOL = WORK + '/reference-library-changed-ui-tool-04';
export const REFERENCE_ROOT = WORK + '/reference-library-changed-ui-v4';
export const REFERENCE_OUTPUT = REFERENCE_ROOT + '/browser';
export const REFERENCE_UNIT = 'goby-reference-library-changed-ui-v4.service';
export const REFERENCE_CONTROLLER_UNIT = 'goby-reference-library-changed-ui-controller-v4.service';
export const REFERENCE_ORIGIN = 'http://127.0.0.1:18197';
export const REFERENCE_USER = 'c5f36699a54f4971a891682cd9de410f';
export const REFERENCE_SERVER = 'f56dec8ff7414847873064c4be9fba74';
export const REFERENCE_PRIOR_RECOVERY = Object.freeze({ path: WORK + '/reference-library-changed-ui-execution-03/recovery-terminal.json',
  sha256: '8bed884bc76dfe2277f0a9f116195a5db28cabfacf40a45bdf0d1886385fbc6e' });
export const REFERENCE_PRIOR_RECOVERY_REPORT = Object.freeze({ path: WORK + '/reference-library-changed-ui-recovery-v3b/report.json',
  sha256: '3e9a555f0a91e42df63657652a5a2f84b3104db76871a340ffca7001ebf67e3e' });
const VIEWER = 'm3e-library-changed-viewer-v1', ITEM = '100', ANCHOR = '96', LIBRARY = '93';
const ADMIN = 'efe2137dc3394ed4a23f9c337598f105';
const MEDIA = '/opt/goby-fixtures/client-library-changed-v1/Movies';
const SELF = fileURLToPath(import.meta.url), SHA = /^[0-9a-f]{64}$/;
const SOURCES = ['client-browser-library-changed-reference.mjs', 'client-browser-library-changed-reference-runtime.mjs', 'client-browser-session-proof.mjs'];
export const REFERENCE_LIMITS = Object.freeze({ work_ms: 780000, cleanup_ms: 110000, quiet_ms: 75000,
  window_ms: 120000, sample_ms: 500, poll_ms: 100, control_ms: 90000, samples: 800,
  quiet_samples: 200, window_samples: 260, http: 2000, messages: 64, raw_messages: 2048, json_bytes: 2 * 1024 * 1024,
  record_bytes: 512 * 1024, report_bytes: 4 * 1024 * 1024 });
const record = value => value !== null && typeof value === 'object' && !Array.isArray(value);
const clone = value => JSON.parse(JSON.stringify(value));
const ordered = value => Array.isArray(value) ? value.map(ordered) : record(value)
  ? Object.fromEntries(Object.keys(value).sort().map(key => [key, ordered(value[key])])) : value;
const same = (left, right) => JSON.stringify(ordered(left)) === JSON.stringify(ordered(right));
const exact = (value, keys) => record(value) && same(Object.keys(value).sort(), [...keys].sort());
const sha = value => createHash('sha256').update(value).digest('hex');
const safeText = (value, maximum = 256) => typeof value === 'string' && value.length > 0 && value.length <= maximum && !/[\x00-\x1f\x7f]/.test(value);
const decimal = value => typeof value === 'string' && /^[1-9][0-9]{0,19}$/.test(value);
function need(value, code = 'reference_guard_rejected') { if (!value) throw new Error(code); }
const delay = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
function bounded(promise, milliseconds, code = 'reference_timeout') {
  let timer;
  return Promise.race([promise, new Promise((_, reject) => { timer = setTimeout(() => reject(new Error(code)), milliseconds); })])
    .finally(() => clearTimeout(timer));
}
const descriptor = value => exact(value, ['path', 'sha256']) && typeof value.path === 'string' && SHA.test(value.sha256) &&
  value.path.startsWith(WORK + '/') && path.posix.normalize(value.path) === value.path && !value.path.includes('\\') && /^[\x21-\x7e]+$/.test(value.path);
function instant(value) {
  need(typeof value === 'string' && /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-]\d\d:\d\d)$/.test(value));
  const result = Date.parse(value); need(Number.isFinite(result)); return result;
}

export function parseReferenceArguments(argv) {
  need(Array.isArray(argv) && argv.length === 6); const result = {};
  for (let index = 0; index < argv.length; index += 2) {
    need(['--input', '--input-sha256', '--output'].includes(argv[index]) && !Object.hasOwn(result, argv[index].slice(2)));
    result[argv[index].slice(2)] = argv[index + 1];
  }
  need(result.input === REFERENCE_ROOT + '/input.json' && result.output === REFERENCE_OUTPUT && SHA.test(result['input-sha256']));
  return result;
}

export function validateReferenceInput(value) {
  need(exact(value, ['marker', 'version', 'mode', 'root', 'output', 'actor', 'reference', 'expected_libraries',
    'target', 'anchor', 'source_closure', 'authority', 'controller']) && value.marker === 'goby-reference-library-changed-input-v1' &&
    value.version === 1 && value.mode === 'reference-movies-name-automatic-refresh' && value.root === REFERENCE_ROOT && value.output === REFERENCE_OUTPUT);
  need(exact(value.actor, ['slot', 'user_id', 'username', 'credentials', 'account_key']) && value.actor.slot === 'B' &&
    value.actor.user_id === REFERENCE_USER && value.actor.username === VIEWER && value.actor.account_key === 'viewer' &&
    descriptor(value.actor.credentials) && value.actor.credentials.path === REFERENCE_ROOT + '/viewer-credentials.json');
  need(exact(value.reference, ['server_id', 'base_url', 'service_identity']) && value.reference.server_id === REFERENCE_SERVER &&
    value.reference.base_url === REFERENCE_ORIGIN);
  const service = value.reference.service_identity;
  need(exact(service, ['pid', 'startTicks', 'bootId', 'uid', 'exe', 'cmdline', 'networkNamespace', 'cgroup', 'invocationId']) &&
    Number.isSafeInteger(service.pid) && service.pid > 1 && /^[1-9]\d*$/.test(service.startTicks) && service.uid === 0 &&
    /^[0-9a-f-]{36}$/.test(service.bootId) && safeText(service.exe, 4096) && Array.isArray(service.cmdline) &&
    service.cmdline.length > 0 && service.cmdline.length <= 32 && service.cmdline.every(value => safeText(value, 4096)) &&
    /^net:\[\d+\]$/.test(service.networkNamespace) && typeof service.cgroup === 'string' && service.cgroup.length <= 4096 &&
    /^0::\/[^\r\n]+\n?$/.test(service.cgroup) && /^[0-9a-f]{32}$/.test(service.invocationId));
  need(exact(value.target, ['id', 'library_id', 'parent_id', 'name', 'path', 'type']) && value.target.id === ITEM &&
    value.target.library_id === LIBRARY && value.target.parent_id === '99' && value.target.type === 'Movie' && safeText(value.target.name) &&
    value.target.path === MEDIA + '/LibraryChanged Observed (2031)/LibraryChanged Observed (2031).mp4');
  need(exact(value.anchor, ['id', 'name', 'path', 'type']) && value.anchor.id === ANCHOR && value.anchor.type === 'Movie' &&
    safeText(value.anchor.name) && value.anchor.name !== value.target.name &&
    value.anchor.path === MEDIA + '/LibraryChanged Anchor (2030)/LibraryChanged Anchor (2030).mp4');
  need(Array.isArray(value.expected_libraries) && value.expected_libraries.length > 0 && value.expected_libraries.length <= 16 &&
    value.expected_libraries.every(row => exact(row, ['id', 'name']) && decimal(row.id) && safeText(row.name)) &&
    new Set(value.expected_libraries.map(row => row.id)).size === value.expected_libraries.length &&
    value.expected_libraries.some(row => row.id === LIBRARY && row.name === 'M3e Controlled LibraryChanged Movies'));
  need(exact(value.authority, ['owner', 'preflight', 'before_snapshot', 'prior_recovery']) && Object.values(value.authority).every(descriptor) &&
    value.authority.owner.path === WORK + '/reference-owner.json' &&
    value.authority.preflight.path === WORK + '/reference-library-changed-ui-preflight-v4/report.json' &&
    value.authority.before_snapshot.path === REFERENCE_ROOT + '/before-public.json' && same(value.authority.prior_recovery, REFERENCE_PRIOR_RECOVERY));
  need(record(value.source_closure) && same(Object.keys(value.source_closure).sort(), SOURCES.map(name => REFERENCE_TOOL + '/' + name).sort()) &&
    Object.values(value.source_closure).every(value => SHA.test(value)));
  const controller = value.controller;
  need(exact(controller, ['pid', 'start_ticks', 'boot_id', 'unit', 'invocation_id']) && Number.isSafeInteger(controller.pid) && controller.pid > 1 &&
    /^[1-9]\d*$/.test(controller.start_ticks) && controller.boot_id === service.bootId && controller.unit === REFERENCE_CONTROLLER_UNIT &&
    /^[0-9a-f]{32}$/.test(controller.invocation_id));
  return value;
}

export function validateReferencePublicBaseline(input, owner, preflight, before) {
  validateReferenceInput(input);
  need(owner.serverId === REFERENCE_SERVER && same(owner.serviceIdentity, input.reference.service_identity));
  need(exact(preflight, ['marker', 'version', 'mode', 'root', 'reference', 'script_sha256', 'source_closure_sha256', 'public_snapshot', 'prior_recovery',
    'target', 'anchor', 'expected_libraries', 'admin', 'ledger', 'preservation', 'errors', 'status', 'completed_at', 'evidence']) &&
    preflight.marker === 'goby-reference-library-changed-preflight-v1' && preflight.version === 1 &&
    preflight.mode === 'business-read-only-preflight' && preflight.root === WORK + '/reference-library-changed-ui-preflight-v4' &&
    preflight.status === 'passed' && same(preflight.errors, []) && preflight.preservation?.passed === true &&
    SHA.test(preflight.script_sha256) && preflight.source_closure_sha256 === null && same(preflight.prior_recovery, input.authority.prior_recovery) &&
    same(preflight.reference, input.reference) && same(preflight.target, input.target) && same(preflight.anchor, input.anchor) &&
    same(preflight.expected_libraries, input.expected_libraries) && preflight.admin?.closed === true &&
    preflight.admin.login_status === 200 && preflight.admin.login_complete === true && preflight.admin.logout_status === 204 &&
    preflight.admin.logout_complete === true && preflight.admin.exact401_status === 401 && preflight.admin.exact401_complete === true &&
    preflight.ledger?.authentication_posts === 2 && preflight.ledger.metadata_posts === 0 && preflight.ledger.http_requests > 0 &&
    descriptor(preflight.public_snapshot) && preflight.public_snapshot.path === preflight.root + '/after-public.json');
  need(exact(before, ['marker', 'version', 'captured_at', 'credential_context', 'server', 'roster', 'configuration', 'libraries', 'catalog_by_library',
    'items_by_user', 'preferences', 'details', 'devices']) && before.marker === 'goby-reference-library-changed-public-snapshot-v1' && before.version === 1 && before.server?.Id === REFERENCE_SERVER &&
    instant(before.captured_at) >= instant(preflight.completed_at) && record(before.devices));
  const context = before.credential_context;
  need(exact(context, ['channel', 'authenticated_user_id', 'token_sha256', 'user_id_semantics']) && context.channel === 'controller_api' &&
    context.authenticated_user_id === ADMIN && SHA.test(context.token_sha256) && context.user_id_semantics === 'subject_projection',
  'reference_snapshot_credential_context');
  const roster = Array.isArray(before.roster) ? before.roster : Object.values(before.roster ?? {});
  const viewer = roster.find(row => row.Id === REFERENCE_USER), administrator = roster.find(row => row.Id === ADMIN);
  need(viewer?.Name === VIEWER && viewer.Policy?.IsAdministrator === false && viewer.Policy?.IsDisabled === false &&
    administrator?.Policy?.IsAdministrator === true && administrator.Policy.IsDisabled === false);
  need(record(before.libraries) && same(Object.entries(before.libraries).map(([id, row]) => ({ id, name: row.Name })).sort((left, right) => left.id.localeCompare(right.id)),
    [...input.expected_libraries].sort((left, right) => left.id.localeCompare(right.id))));
  const catalog = before.catalog_by_library?.[LIBRARY];
  const rows = Array.isArray(catalog) ? catalog : record(catalog?.Items) ? Object.values(catalog.Items)
    : Array.isArray(catalog?.Items) ? catalog.Items : Object.values(catalog ?? {});
  const movies = rows.filter(row => row.Type === 'Movie' && row.IsFolder !== true);
  need(movies.length === 2 && same(movies.map(row => row.Id).sort(), [ITEM, ANCHOR].sort()));
  // These are admin-authenticated UserId subject projections, not the later UI-token baseline.
  for (const target of [input.target, input.anchor]) {
    const item = movies.find(row => row.Id === target.id), detail = before.details?.viewer?.[target.id];
    need(item?.Name === target.name && detail?.Id === target.id && detail.Name === target.name && detail.Type === 'Movie' &&
      detail.IsFolder !== true && detail.Path === target.path && rows.filter(row => row.Name === target.name).length === 1);
    if (target.id === ITEM) need(detail.ParentId === input.target.parent_id);
  }
  return { device_ids: [...new Set(Object.entries(before.devices).flatMap(([key, value]) => [key, value.Id, value.ReportedDeviceId])
    .filter(value => typeof value === 'string' && value.length > 0))] };
}

export function validateReferenceRecoveryTerminal(terminal) {
  need(exact(terminal, ['administrator_logout204_and_same_token401_verified', 'anchor_unchanged', 'captured_at', 'full_target_restored_except_etag',
    'library_changed_client_acceptance', 'main_acceptance', 'marker', 'media_source_name_restored', 'media_unchanged', 'original_failed_report',
    'original_report_rewritten', 'recursive_cgroup_members', 'report', 'restore_posts', 'restore_status', 'source', 'status', 'systemd', 'unit',
    'viewer_context_read_authentication', 'viewer_ui_login_performed']) && terminal.marker === 'goby-reference-ui-recovery-terminal-v3' &&
    terminal.status === 'restoration_independently_confirmed' && Number.isFinite(instant(terminal.captured_at)) &&
    terminal.administrator_logout204_and_same_token401_verified === true && terminal.anchor_unchanged === true &&
    terminal.full_target_restored_except_etag === true && terminal.media_source_name_restored === true && terminal.media_unchanged === true &&
    terminal.library_changed_client_acceptance === false && terminal.main_acceptance === false && terminal.original_report_rewritten === false &&
    terminal.viewer_ui_login_performed === false && terminal.viewer_context_read_authentication === 'administrator' &&
    terminal.restore_posts === 1 && terminal.restore_status === 204 && same(terminal.recursive_cgroup_members, []) &&
    same(terminal.report, REFERENCE_PRIOR_RECOVERY_REPORT) &&
    same(terminal.original_failed_report, { path: WORK + '/reference-library-changed-ui-v3/report.json',
      sha256: 'a7290d22100b63f919324b7dded7d15228458b9ebf456384b6b6b4de5fa4e23a' }) &&
    same(terminal.source, { path: WORK + '/reference-library-changed-ui-recovery-v3b/recover.py',
      sha256: 'b230b22d29a34a154745c1d822ead6e2fcf68084ea78ff889eb0e1196aafea1b' }) &&
    terminal.unit === 'goby-reference-library-changed-ui-recovery-v3b.service' && same(terminal.systemd, {
      ActiveState: 'active', ControlGroup: '', ExecMainStatus: '0', Id: 'goby-reference-library-changed-ui-recovery-v3b.service',
      InvocationID: 'c7b4694665e841ce98024e61107a1d47', MainPID: '0', RemainAfterExit: 'yes', Result: 'success', SubState: 'exited' }),
  'reference_prior_recovery_terminal'); return terminal;
}

export function validateReferencePriorRecovery(terminal, report) {
  validateReferenceRecoveryTerminal(terminal);
  need(exact(report, ['administrator_closed', 'captured_at', 'errors', 'evidence', 'http_requests', 'library_changed_client_acceptance', 'marker',
    'media_unchanged', 'original_scope_replayed', 'protocol_observation_complete', 'restoration_confirmed', 'restore_acknowledged', 'restore_posts',
    'results', 'status', 'viewer_projection_channel', 'viewer_ui_login_performed']) && report.marker === 'goby-reference-ui-name-recovery-v3' &&
    report.status === 'restored' && instant(report.captured_at) <= instant(terminal.captured_at) && report.administrator_closed === true &&
    same(report.errors, []) && record(report.evidence) && report.http_requests === 9 && report.restore_posts === 1 && report.restore_acknowledged === true &&
    report.restoration_confirmed === true && report.media_unchanged === true && report.original_scope_replayed === false &&
    report.library_changed_client_acceptance === false && report.protocol_observation_complete === false && report.viewer_ui_login_performed === false &&
    report.viewer_projection_channel === 'administrator_token_with_viewer_UserId', 'reference_prior_recovery_report');
  const statuses = { login: 200, current: 200, 'anchor-before': 200, restore: 204, restored: 200, 'viewer-context': 200, 'anchor-after': 200, logout: 204, exact401: 401 };
  need(exact(report.results, Object.keys(statuses)) && SHA.test(report.results.logout.token_sha256), 'reference_prior_recovery_results');
  const token = report.results.logout.token_sha256;
  for (const [name, status] of Object.entries(statuses)) {
    const result = report.results[name];
    need(exact(result, ['body_bytes', 'body_sha256', 'complete', 'status', 'token_sha256']) && result.complete === true && result.status === status &&
      Number.isSafeInteger(result.body_bytes) && result.body_bytes >= 0 && result.body_bytes <= REFERENCE_LIMITS.json_bytes && SHA.test(result.body_sha256) &&
      result.token_sha256 === (name === 'login' ? null : token), 'reference_prior_recovery_results');
  }
  need(report.results.logout.body_bytes === 0 && report.results.restore.body_bytes === 0 && report.results.logout.body_sha256 === sha('') &&
    report.results.restore.body_sha256 === sha('') && report.results['anchor-before'].body_sha256 === report.results['anchor-after'].body_sha256 &&
    report.results['anchor-before'].body_bytes === report.results['anchor-after'].body_bytes, 'reference_prior_recovery_results');
  return { terminal, report };
}

/** Read only the two fixed safe recovery records; never follow their private evidence descriptors. */
export async function readReferencePriorRecovery(input, read = ownedJSON) {
  validateReferenceInput(input);
  const terminal = validateReferenceRecoveryTerminal(await read(input.authority.prior_recovery));
  const report = await read(REFERENCE_PRIOR_RECOVERY_REPORT);
  return validateReferencePriorRecovery(terminal, report);
}

export function referenceRoute(raw) {
  const url = new URL(raw); need(url.origin === REFERENCE_ORIGIN && !url.username && !url.password &&
    ['/web', '/web/', '/web/index.html'].includes(url.pathname));
  for (const query of [url.searchParams, new URLSearchParams(url.hash.split('?').slice(1).join('?'))])
    for (const [key] of query) need(!/token|password|api_key|authorization/i.test(key));
  need(url.href.length <= 4096); return url.pathname + url.search + url.hash;
}
export function referenceMoviesRoute(route) {
  try {
    const url = new URL(REFERENCE_ORIGIN + route); if (referenceRoute(url.href) !== route || url.hash.split('?')[0] !== '#!/videos') return false;
    const values = [...new URLSearchParams(url.hash.split('?').slice(1).join('?'))];
    const parents = values.filter(([key]) => key.toLowerCase() === 'parentid'), servers = values.filter(([key]) => key.toLowerCase() === 'serverid');
    return parents.length === 1 && parents[0][1] === LIBRARY && servers.length <= 1 && (!servers.length || servers[0][1] === REFERENCE_SERVER);
  } catch { return false; }
}
export function referenceFullMovieQuery(row) {
  if (row?.kind !== 'items' || !record(row.query)) return false;
  const query = {};
  for (const [key, value] of Object.entries(row.query)) { const name = key.toLowerCase(); if (Object.hasOwn(query, name)) return false; query[name] = value; }
  return query.parentid === LIBRARY && query.includeitemtypes === 'Movie' && query.recursive === 'true' &&
    query.startindex === '0' && query.limit === '50' && query.ids === undefined;
}

export function projectReferenceItems(response, kind) {
  need(record(response) && SHA.test(response.body_sha256) && Number.isSafeInteger(response.bytes) && response.bytes > 0 && response.bytes <= REFERENCE_LIMITS.json_bytes);
  const items = kind === 'items' ? response.json?.Items : [response.json];
  need(Array.isArray(items) && items.length <= 128 && items.every(row => record(row) && decimal(row.Id) && safeText(row.Name) && safeText(row.Type, 80)));
  return { items: items.map(row => ({ Id: row.Id, Name: row.Name, Type: row.Type, IsFolder: row.IsFolder === true,
    ParentId: typeof row.ParentId === 'string' ? row.ParentId : null })), count: items.length,
  body_sha256: response.body_sha256, body_bytes: response.bytes };
}

export function pairReferenceReads(physical, frames) {
  need(Array.isArray(physical) && Array.isArray(frames) && physical.length <= REFERENCE_LIMITS.http && frames.length <= REFERENCE_LIMITS.http &&
    new Set(physical.map(row => row.id)).size === physical.length && new Set(frames.map(row => row.index)).size === frames.length);
  const eligible = frames.filter(row => SHA.test(row.token_sha256) && SHA.test(row.request_sha256) && SHA.test(row.shape_sha256) &&
    row.completed === true && row.failed === false && row.status === 200 && row.content_type === 'application/json' && row.from_service_worker === false &&
    (row.main_frame === true && row.sourceworker === false || row.main_frame === false && row.sourceworker === true));
  const fits = (frame, wire) => wire.completed === true && wire.failed === false && wire.status === 200 && wire.projection && frame.kind === wire.kind &&
    frame.token_sha256 === wire.token_sha256 && frame.request_sha256 === wire.request_sha256 && frame.shape_sha256 === wire.shape_sha256 &&
    frame.start_elapsed_ms <= wire.start_elapsed_ms && wire.start_elapsed_ms <= frame.finished_elapsed_ms &&
    wire.finished_elapsed_ms <= frame.finished_elapsed_ms + 1000;
  return eligible.map(frame => {
    const matches = physical.filter(wire => fits(frame, wire)), wire = matches.length === 1 ? matches[0] : null;
    const unique = wire !== null && eligible.filter(row => fits(row, wire)).length === 1;
    return { frame: clone(frame), physical: unique ? clone(wire) : null, complete: unique, unambiguous: unique };
  });
}
export function referencePairReferences(physical, frames) {
  return pairReferenceReads(physical, frames).map(pair => ({ frame_request_index: pair.frame.index,
    physical_exchange_id: pair.physical?.id ?? null, complete: pair.complete, unambiguous: pair.unambiguous }));
}

function responseMatches(wire, input, expected, full = false) {
  const items = wire?.projection?.items;
  if (!Array.isArray(items) || wire.projection.count !== items.length || !SHA.test(wire.projection.body_sha256) ||
    !Number.isSafeInteger(wire.projection.body_bytes) || wire.projection.body_bytes <= 0 || wire.projection.body_bytes > REFERENCE_LIMITS.json_bytes ||
    items.some(row => row.Type !== 'Movie' || row.IsFolder === true)) return false;
  const target = items.filter(row => row.Id === ITEM);
  if (target.length !== 1 || target[0].Name !== expected) return false;
  if (full || wire.kind === 'items') return items.length === 2 && same(items.map(row => row.Id).sort(), [ITEM, ANCHOR].sort()) &&
    items.find(row => row.Id === ANCHOR)?.Name === input.anchor.name && expected !== input.anchor.name;
  return wire.kind === 'target' && items.length === 1 && [`/Items/${ITEM}`, `/Users/${REFERENCE_USER}/Items/${ITEM}`].includes(wire.route);
}

export function pairReferenceMessages(events, boundary) {
  const selected = referenceLibraryChangedMessages(events);
  const inside = event => Number.isSafeInteger(event.sequence) && Number.isFinite(event.elapsed_ms) && event.phase === boundary.name &&
    event.sequence > boundary.started_sequence && event.elapsed_ms >= boundary.started_elapsed_ms &&
    event.elapsed_ms <= boundary.end_elapsed_ms && event.connection_id === boundary.connection_id && event.token_sha256 === boundary.token_sha256;
  const arrays = ['ItemsAdded', 'ItemsRemoved', 'ItemsUpdated', 'CollectionFolders', 'FoldersAddedTo', 'FoldersRemovedFrom'];
  const relevant = event => {
    if (!inside(event) || event.direction !== 'server' || event.original_message !== true || event.payload_retained !== true ||
      !SHA.test(event.body_sha256) || !Number.isSafeInteger(event.bytes) || event.bytes <= 0 || event.bytes > 65536 ||
      typeof event.json_text !== 'string' || Buffer.byteLength(event.json_text) !== event.bytes || sha(event.json_text) !== event.body_sha256 ||
      event.json?.MessageType !== 'LibraryChanged' || !/^[0-9a-f]{32}$/.test(event.json.MessageId) ||
      !exact(event.json.Data, [...arrays, 'IsEmpty']) || event.json.Data.IsEmpty !== false ||
      !exact(event.json, ['Data', 'MessageId', 'MessageType']) ||
      !arrays.every(key => Array.isArray(event.json.Data[key]) && event.json.Data[key].length <= 128 && event.json.Data[key].every(id => safeText(id, 128))) ||
      !same(event.json.Data.ItemsUpdated, [ITEM])) return false;
    try { return same(JSON.parse(event.json_text), event.json); } catch { return false; }
  };
  return selected.browser.filter(relevant).flatMap(browser => {
    const wires = selected.physical.filter(wire => relevant(wire) && wire.forwarded === true && wire.elapsed_ms <= browser.elapsed_ms &&
      wire.body_sha256 === browser.body_sha256 && wire.bytes === browser.bytes && wire.json_text === browser.json_text && same(wire.json, browser.json));
    if (wires.length !== 1 || selected.browser.filter(other => relevant(other) && other.body_sha256 === browser.body_sha256).length !== 1 ||
      browser.document_id !== boundary.document_id || browser.page_route !== boundary.route) return [];
    return [{ physical: wires[0], browser }];
  });
}

export function referenceLibraryChangedMessages(events) {
  need(exact(events, ['physical', 'browser']) && Object.values(events).every(rows => Array.isArray(rows) && rows.length <= REFERENCE_LIMITS.raw_messages));
  const result = Object.fromEntries(Object.entries(events).map(([key, rows]) => [key, rows.filter(row => row.direction === 'server' && row.json?.MessageType === 'LibraryChanged')]));
  need(Object.values(result).every(rows => rows.length <= REFERENCE_LIMITS.messages), 'reference_library_changed_message_limit'); return result;
}

export function referenceMessageEvidence(events, boundary) {
  const selected = referenceLibraryChangedMessages(events), reasons = new Set();
  const complete = event => {
    if (event.original_message !== true || event.payload_retained !== true || typeof event.json_text !== 'string' ||
      !SHA.test(event.body_sha256) || Buffer.byteLength(event.json_text) !== event.bytes || sha(event.json_text) !== event.body_sha256) return false;
    try { return same(JSON.parse(event.json_text), event.json); } catch { return false; }
  };
  const inWindow = event => event.sequence > boundary.started_sequence && event.elapsed_ms >= boundary.started_elapsed_ms && event.elapsed_ms <= boundary.end_elapsed_ms &&
    event.phase === boundary.name && event.connection_id === boundary.connection_id && event.token_sha256 === boundary.token_sha256;
  const received = selected.browser.filter(inWindow), transport = received.filter(browser => complete(browser) &&
    browser.page_route === boundary.route && browser.document_id === boundary.document_id &&
    selected.physical.filter(wire => inWindow(wire) && complete(wire) && wire.forwarded === true && wire.elapsed_ms <= browser.elapsed_ms &&
      wire.json_text === browser.json_text && wire.body_sha256 === browser.body_sha256).length === 1 &&
    received.filter(other => other.body_sha256 === browser.body_sha256).length === 1);
  const qualified = pairReferenceMessages(events, boundary), qualifiedHashes = new Set(qualified.map(pair => pair.browser.body_sha256));
  for (const event of received) {
    if (!complete(event)) reasons.add('message_integrity_not_proven');
    else if (!transport.includes(event)) reasons.add('transport_not_paired');
    else if (!qualifiedHashes.has(event.body_sha256)) {
      if (!/^[0-9a-f]{32}$/.test(event.json.MessageId)) reasons.add('message_id_not_proven');
      else if (!record(event.json.Data)) reasons.add('data_shape_not_proven');
      else if (!same(event.json.Data.ItemsUpdated, [ITEM])) reasons.add('target_update_not_proven');
      else reasons.add('data_shape_not_proven');
    }
  }
  return { received_count: received.length, transport_paired_count: transport.length, qualified_count: qualified.length,
    unsupported_schema_count: transport.filter(event => !qualifiedHashes.has(event.body_sha256)).length, reasons: [...reasons].sort() };
}

export function bindReferenceDOM(raw, context) {
  const result = { ...clone(raw), target_id: null, anchor_id: null, identity_mode: 'unbound', wire_identity: null, identity_proven: false, passed: false };
  delete result.container_observations;
  if (!raw?.structural_match || !referenceMoviesRoute(raw.route) || !safeText(raw.document_id, 80) || !safeText(raw.expected_name) ||
    !Number.isSafeInteger(raw.visible_items_containers) || raw.visible_items_containers < 0 || raw.visible_items_containers > 32 ||
    raw.visible_cards !== 2 || raw.visible_title_buttons !== 2 || raw.visible_card_containers !== 1 || raw.target_title_count !== 1 ||
    raw.anchor_title_count !== 1 || raw.forbidden_title_count !== 0 || raw.observed_title !== raw.expected_name ||
    raw.observed_anchor_title !== context.input.anchor.name || raw.explicit_identity_consistent !== true || raw.media_inactive !== true ||
    !Number.isFinite(raw.started_elapsed_ms) || !SHA.test(context.token_sha256)) return result;
  const initial = context.phase === 'discovery'; let provedAnchor = null;
  if (!initial && (!context.discovery?.dom?.passed || context.discovery.dom.route !== raw.route || context.discovery.dom.document_id !== raw.document_id ||
    context.discovery.socket.token_sha256 !== context.token_sha256)) return result;
  if (!initial) {
    const anchor = context.discovery;
    if (!Array.isArray(anchor.reads) || !anchor.reads.length || !anchor.reads.every(pair => pair?.physical && pair?.frame) ||
      !Array.isArray(anchor.query_allowlist) || !anchor.query_allowlist.length || !anchor.query_allowlist.every(row => anchor.reads.some(pair =>
        referenceFullMovieQuery(pair.physical) && row.shape_sha256 === pair.physical.shape_sha256 && row.route === pair.physical.route &&
        same(row.query, pair.physical.query) && same(row.hidden_query, pair.physical.hidden_query)))) return result;
    provedAnchor = bindReferenceDOM(anchor.dom, { phase: 'discovery', input: context.input, physical: anchor.reads.map(pair => pair.physical),
      frames: anchor.reads.map(pair => pair.frame), token_sha256: context.token_sha256, after_sequence: anchor.navigation?.before_sequence });
    if (!provedAnchor.passed || !same(provedAnchor.wire_identity, anchor.dom.wire_identity)) return result;
  }
  const messages = initial ? [null] : pairReferenceMessages(context.events, context.boundary);
  const pairs = pairReferenceReads(context.physical, context.frames);
  for (const pair of pairs) for (const message of messages) {
    if (!pair.complete) continue;
    const wire = pair.physical, frame = pair.frame, after = message?.browser.sequence ?? context.after_sequence;
    const full = referenceFullMovieQuery(wire) && (initial || context.discovery.query_allowlist.some(row => row.shape_sha256 === wire.shape_sha256));
    if ((!full && (initial || wire.kind !== 'target')) || !responseMatches(wire, context.input, raw.expected_name, initial) ||
      wire.phase !== context.phase || frame.phase !== context.phase || wire.method !== 'GET' || frame.route !== wire.route ||
      wire.token_sha256 !== context.token_sha256 || frame.token_sha256 !== context.token_sha256 || frame.page_route !== raw.route ||
      frame.document_id !== raw.document_id || frame.request_sequence <= after || wire.request_sequence <= after ||
      frame.start_elapsed_ms < (message?.browser.elapsed_ms ?? 0) || wire.start_elapsed_ms < (message?.browser.elapsed_ms ?? 0) ||
      frame.finished_elapsed_ms > raw.started_elapsed_ms || wire.finished_elapsed_ms > raw.started_elapsed_ms) continue;
    return { ...result, target_id: wire.projection.items.find(row => row.Id === ITEM).Id,
      anchor_id: initial ? wire.projection.items.find(row => row.Id === ANCHOR).Id : provedAnchor.anchor_id,
      identity_mode: 'reference-two-movie-wire-and-cards', identity_proven: true, passed: true,
      wire_identity: { phase: context.phase, physical_exchange_id: wire.id, frame_request_index: frame.index,
        body_sha256: wire.projection.body_sha256, shape_sha256: wire.shape_sha256, request_sha256: wire.request_sha256,
        token_sha256: wire.token_sha256, message_id: message?.browser.json.MessageId ?? null } };
  }
  return result;
}

export function referenceWindowEvidence(window, input, reservation, discovery) {
  need(['forward', 'restored'].includes(window?.name));
  const b = window.boundary, expected = window.name === 'forward' ? reservation.marker_name : reservation.original_name;
  need(b?.name === window.name && SHA.test(b.token_sha256) && safeText(b.connection_id, 80) && safeText(b.document_id, 80) &&
    Number.isSafeInteger(b.started_sequence) && b.started_sequence >= 0 &&
    [b.started_elapsed_ms, b.response_completed_elapsed_ms, b.end_elapsed_ms, b.completed_elapsed_ms].every(Number.isFinite) &&
    b.end_elapsed_ms - b.response_completed_elapsed_ms === REFERENCE_LIMITS.window_ms &&
    b.completed_elapsed_ms >= b.end_elapsed_ms && b.completed_elapsed_ms <= b.end_elapsed_ms + 5000 &&
    b.started_elapsed_ms <= b.response_completed_elapsed_ms + 1000 && window.actions.length === 0 && window.lifecycle.length === 0 &&
    window.dom.length > 0 && window.dom.length <= REFERENCE_LIMITS.window_samples && same(window.http.pairs, referencePairReferences(window.http.physical, window.http.frames)));
  for (const sample of window.dom) need(Number.isSafeInteger(sample.sequence) && sample.sequence > b.started_sequence &&
    [sample.elapsed_ms, sample.started_elapsed_ms, sample.observation?.started_elapsed_ms].every(Number.isFinite) &&
    sample.elapsed_ms <= b.end_elapsed_ms && sample.started_elapsed_ms >= b.started_elapsed_ms &&
    sample.started_elapsed_ms <= sample.observation.started_elapsed_ms && sample.observation.started_elapsed_ms <= sample.elapsed_ms &&
    sample.observation.route === b.route && sample.observation.document_id === b.document_id && sample.observation.media_inactive === true &&
    sample.observation.expected_name === expected && sample.observation.anchor_name === input.anchor.name);
  for (let index = 1; index < window.dom.length; index++) need(window.dom[index].sequence > window.dom[index - 1].sequence &&
    window.dom[index].started_elapsed_ms >= window.dom[index - 1].elapsed_ms && window.dom[index].started_elapsed_ms - window.dom[index - 1].elapsed_ms <= 4000);
  need(window.dom[0].started_elapsed_ms <= b.started_elapsed_ms + 4000 && window.dom.at(-1).elapsed_ms >= b.end_elapsed_ms - 4000);
  const messages = pairReferenceMessages(window.events, b), accepted = referenceAcceptedReads(window, input, expected, discovery, messages);
  const context = { phase: window.name, input, discovery, boundary: b,
    token_sha256: b.token_sha256, physical: window.http.physical, frames: window.http.frames, events: window.events };
  const bound = window.dom.map(sample => ({ sample, proof: bindReferenceDOM(sample.observation, context) }));
  const final = window.dom.at(-1).observation;
  const matches = (row, title = expected) => row.structural_match === true && row.explicit_identity_consistent === true &&
    row.visible_cards === 2 && row.visible_title_buttons === 2 && row.visible_card_containers === 1 && row.anchor_title_count === 1 &&
    row.observed_title === title && row.observed_anchor_title === input.anchor.name;
  const completeDOM = ({ sample, proof }) => proof.passed === true && sample.observation.passed === true && sample.observation.identity_proven === true &&
    sample.observation.identity_mode === proof.identity_mode && sample.observation.target_id === proof.target_id && sample.observation.anchor_id === proof.anchor_id &&
    same(proof.wire_identity, sample.observation.wire_identity);
  const beforeName = window.name === 'forward' ? reservation.original_name : reservation.marker_name;
  const firstChanged = bound.find(entry => completeDOM(entry) &&
    window.dom.some(prior => prior.elapsed_ms < entry.sample.started_elapsed_ms && matches(prior.observation, beforeName)));
  const stable = firstChanged && bound.filter(row => row.sample.started_elapsed_ms >= firstChanged.sample.started_elapsed_ms).every(row => matches(row.sample.observation));
  const changed = Boolean(firstChanged && stable && matches(final) && completeDOM(bound.at(-1)));
  const notifications = window.events.browser.filter(row => row.json?.MessageType === 'LibraryChanged' && record(row.json.Data)).map(row => row.json.Data);
  const messageEvidence = referenceMessageEvidence(window.events, b);
  return { result: changed ? 'changed' : 'not_observed', outcome: changed ? 'automatic_http_and_visible_transition_observed'
    : matches(final) ? 'matches_expected_without_proven_transition' : 'automatic_refresh_not_observed_within_window',
  notification_context: { additional_items: notifications.some(value => ['ItemsAdded', 'ItemsRemoved'].some(key => Array.isArray(value[key]) && value[key].length > 0) ||
    Array.isArray(value.ItemsUpdated) && value.ItemsUpdated.some(id => id !== ITEM)),
  parent_context: notifications.some(value => ['CollectionFolders', 'FoldersAddedTo', 'FoldersRemovedFrom'].some(key => Array.isArray(value[key]) && value[key].length > 0)) },
  message_evidence: messageEvidence,
  status: { message_observed: messageEvidence.received_count > 0, http_observed: accepted.length > 0,
    dom_matches_expected: Boolean(matches(final)), visible_transition_observed: changed },
  proof: changed ? { ...firstChanged.proof.wire_identity, first_dom_sequence: firstChanged.sample.sequence,
    last_dom_sequence: window.dom.at(-1).sequence } : null };
}

export function referenceAcceptedReads(window, input, expected, discovery, messages = pairReferenceMessages(window.events, window.boundary)) {
  const b = window.boundary;
  return pairReferenceReads(window.http.physical, window.http.frames).filter(pair => {
    if (!pair.complete) return false;
    const wire = pair.physical, frame = pair.frame;
    return wire.method === 'GET' && wire.phase === window.name && frame.phase === window.name && frame.route === wire.route &&
      wire.token_sha256 === b.token_sha256 && frame.token_sha256 === b.token_sha256 && frame.document_id === b.document_id && frame.page_route === b.route &&
      wire.finished_elapsed_ms <= b.end_elapsed_ms && frame.finished_elapsed_ms <= b.end_elapsed_ms &&
      responseMatches(wire, input, expected) && (wire.kind === 'target' || referenceFullMovieQuery(wire) &&
        discovery.query_allowlist.some(row => row.shape_sha256 === wire.shape_sha256)) &&
      messages.some(message => wire.request_sequence > message.browser.sequence && frame.request_sequence > message.browser.sequence &&
        wire.start_elapsed_ms >= message.browser.elapsed_ms && frame.start_elapsed_ms >= message.browser.elapsed_ms);
  });
}

/** Read only bounded, visible public card structure; no application objects or storage. */
export async function observeReferenceDOM(page, input, expected, forbidden, documentID) {
  const route = referenceRoute(page.url());
  const observed = await page.locator('.itemsContainer').evaluateAll((elements, values) => {
    if (elements.length > 32) return { overflow: true };
    const inside = (box, parent) => {
      let left = Math.max(0, box.left), right = Math.min(innerWidth, box.right), top = Math.max(0, box.top), bottom = Math.min(innerHeight, box.bottom), depth = 0;
      if (box.width <= 0 || box.height <= 0 || right <= left || bottom <= top) return false;
      while (parent) {
        if (++depth > 64) return false;
        const style = getComputedStyle(parent);
        if (style.display !== 'contents') {
          const clips = value => ['hidden', 'clip', 'scroll', 'auto'].includes(value), x = clips(style.overflowX ?? style.overflow), y = clips(style.overflowY ?? style.overflow);
          if (x || y) { const bounds = parent.getBoundingClientRect();
            if (x) { if (bounds.width <= 0) return false; left = Math.max(left, bounds.left); right = Math.min(right, bounds.right); }
            if (y) { if (bounds.height <= 0) return false; top = Math.max(top, bounds.top); bottom = Math.min(bottom, bounds.bottom); }
            if (right <= left || bottom <= top) return false;
          }
        }
        parent = parent.parentElement;
      }
      return true;
    };
    const visible = element => { const style = getComputedStyle(element);
      return style.display !== 'none' && style.visibility !== 'hidden' && inside(element.getBoundingClientRect(), element.parentElement); };
    const allCards = document.querySelectorAll('.card'), allButtons = document.querySelectorAll('button.cardTextActionButton');
    if (allCards.length > 64 || allButtons.length > 64) return { overflow: true };
    const cards = [...allCards].filter(visible), buttons = [...allButtons].filter(visible), pool = new Set(elements);
    const owners = elements.map((container, index) => ({ container, index, visible: visible(container),
      cards: cards.filter(card => card.closest('.itemsContainer') === container), buttons: buttons.filter(button => button.closest('.itemsContainer') === container) }));
    let overflow = false;
    const title = button => {
      const texts = [], walker = document.createTreeWalker(button, NodeFilter.SHOW_TEXT); let node, count = 0;
      while ((node = walker.nextNode())) {
        if (++count > 64 || typeof node.nodeValue !== 'string' || node.nodeValue.length > 4096) { overflow = true; return null; }
        if (!node.nodeValue.trim() || !visible(node.parentElement)) continue;
        const range = document.createRange(); range.selectNodeContents(node); const box = range.getBoundingClientRect(); range.detach();
        if (inside(box, node.parentElement)) texts.push(node.nodeValue);
      }
      const text = texts.join(' ').replace(/\s+/g, ' ').trim(); if (text.length > 256) { overflow = true; return null; } return text;
    };
    const titles = buttons.map(title), anchorButtons = buttons.filter((_, index) => titles[index] === values.anchor_name);
    const targets = buttons.filter((_, index) => titles[index] !== values.anchor_name), targetButton = targets.length === 1 ? targets[0] : null;
    const cardOwners = owners.filter(owner => owner.cards.length > 0);
    const belongs = (button, expectedID) => {
      const card = button?.closest('.card'), text = button?.closest('.cardText'), box = button?.closest('.cardBox'), container = card?.closest('.itemsContainer');
      return Boolean(card && pool.has(container) && cards.includes(card) && button.closest('.itemsContainer') === container &&
        buttons.filter(other => other.closest('.card') === card).length === 1 && text?.closest('.cardBox') === box && box?.closest('.card') === card &&
        [button, text, box, card].every(node => { const id = node.getAttribute('data-id'), type = node.getAttribute('data-type');
          return (id === null || id === expectedID) && (type === null || type === 'Movie'); }) &&
        (container.getAttribute('data-id') === null || container.getAttribute('data-id') === values.library));
    };
    const consistent = cards.length === 2 && buttons.length === 2 && cardOwners.length === 1 && anchorButtons.length === 1 && targetButton &&
      ![...cards, ...buttons].some(node => !pool.has(node.closest('.itemsContainer'))) && belongs(targetButton, values.target) && belongs(anchorButtons[0], values.anchor) &&
      targetButton.closest('.card') !== anchorButtons[0].closest('.card');
    return { overflow, containers: owners.filter(owner => owner.visible).length, owners: cardOwners.length, cards: cards.length, buttons: buttons.length,
      consistent: Boolean(consistent), observed_title: targetButton ? titles[buttons.indexOf(targetButton)] : null,
      anchor_title: anchorButtons.length === 1 ? values.anchor_name : null, target_titles: titles.filter(title => title === values.expected).length,
      anchor_titles: anchorButtons.length, forbidden_titles: values.forbidden === null ? 0 : titles.filter(title => title === values.forbidden).length,
      containers_public: owners.map(owner => ({ index: owner.index, visible: owner.visible, card_count: owner.cards.length, title_count: owner.buttons.length })) };
  }, { target: ITEM, anchor: ANCHOR, library: LIBRARY, expected, forbidden, anchor_name: input.anchor.name });
  need(!observed.overflow, 'reference_dom_limit');
  const active = await page.locator('audio,video').evaluateAll(elements => elements.some(element => !element.paused && !element.ended || element.currentTime > 0));
  return { route, document_id: documentID, started_elapsed_ms: null, expected_name: expected, anchor_name: input.anchor.name,
    observed_title: observed.observed_title, observed_anchor_title: observed.anchor_title, visible_items_containers: observed.containers,
    visible_card_containers: observed.owners, visible_cards: observed.cards, visible_title_buttons: observed.buttons,
    target_title_count: observed.target_titles, anchor_title_count: observed.anchor_titles, forbidden_title_count: observed.forbidden_titles,
    explicit_identity_consistent: observed.consistent, media_inactive: !active, structural_match: observed.consistent && !active,
    target_id: null, anchor_id: null, identity_mode: 'unbound', wire_identity: null, identity_proven: false, passed: false,
    container_observations: observed.containers_public };
}

export function validateReferenceReservation(value, input) {
  need(exact(value, ['target_id', 'library_id', 'original_name', 'marker_name', 'original_body_sha256', 'forward_body_sha256', 'restore_body_sha256']) &&
    value.target_id === ITEM && value.library_id === LIBRARY && value.original_name === input.target.name && safeText(value.marker_name) &&
    value.marker_name !== value.original_name && value.marker_name !== input.anchor.name &&
    ['original_body_sha256', 'forward_body_sha256', 'restore_body_sha256'].every(key => SHA.test(value[key])) &&
    value.original_body_sha256 === value.restore_body_sha256 && value.forward_body_sha256 !== value.original_body_sha256);
  return value;
}
export function validateReferenceControl(value, binding, state) {
  need(exact(value, ['marker', 'version', 'name', 'input_sha256', 'source_closure_sha256', 'controller', 'node_process',
    'previous_stage_sha256', 'reservation', 'commit', 'restoration']) && value.marker === 'goby-reference-library-changed-control-v1' && value.version === 1 &&
    ['reserved', 'forward', 'restored', 'close'].includes(value.name) && value.input_sha256 === binding.input_sha256 &&
    value.source_closure_sha256 === binding.source_closure_sha256 && same(value.controller, binding.controller) && same(value.node_process, binding.node_process) &&
    value.previous_stage_sha256 === state.previous_stage_sha256);
  validateReferenceReservation(value.reservation, state.input);
  if (state.reservation) need(same(value.reservation, state.reservation));
  if (['forward', 'restored'].includes(value.name)) need(exact(value.commit, ['write_completed_at', 'native_result_sha256', 'admin_readback_sha256', 'viewer_readback_sha256']) &&
    Number.isFinite(instant(value.commit.write_completed_at)) && ['native_result_sha256', 'admin_readback_sha256', 'viewer_readback_sha256'].every(key => SHA.test(value.commit[key])));
  else need(value.commit === null);
  need(value.name === 'close' ? ['confirmed', ...(state.allow_not_required ? ['not_required'] : [])].includes(value.restoration)
    : value.restoration === (value.name === 'restored' ? 'confirmed' : 'pending'));
  return value;
}

export function referenceStageRecord(binding, name, observation, tokenSHA, session, previousControl) {
  need(['discovery', 'armed', 'restore-armed', 'restored'].includes(name) && SHA.test(tokenSHA) && descriptor(session));
  return { marker: 'goby-reference-library-changed-stage-v1', version: 1, ...clone(binding), name,
    token_sha256: tokenSHA, session_private: clone(session), previous_control_sha256: previousControl, observation };
}

export function validateReferenceAbort(value, binding, state) {
  need(exact(value, ['marker', 'version', 'input_sha256', 'source_closure_sha256', 'controller', 'node_process', 'name', 'failure',
    'previous_stage_sha256', 'previous_control_sha256', 'token_sha256', 'session_private']) &&
    value.marker === 'goby-reference-library-changed-abort-v1' && value.version === 1 && value.input_sha256 === binding.input_sha256 &&
    value.source_closure_sha256 === binding.source_closure_sha256 && same(value.controller, binding.controller) && same(value.node_process, binding.node_process) &&
    typeof value.name === 'string' && /^[a-z][a-z_-]{0,63}$/.test(value.name) && value.failure === 'reference_controller_failed' &&
    [null, ...state.stages.map(row => row.sha256)].includes(value.previous_stage_sha256) &&
    [null, ...state.controls.map(row => row.sha256)].includes(value.previous_control_sha256) &&
    [null, state.token_sha256].includes(value.token_sha256) && (value.session_private === null || same(value.session_private, state.session_private)),
  'reference_controller_abort_rejected'); return value;
}

/** Protected, fixed-scope reads reject path and inode changes before parsing data. */
async function ownedJSON(item, maximum = REFERENCE_LIMITS.report_bytes) {
  need(descriptor(item)); const info = await fs.lstat(item.path);
  need(info.isFile() && !info.isSymbolicLink() && info.uid === 0 && info.gid === 0 && info.nlink === 1 && (info.mode & 0o777) === 0o600 && info.size > 0 && info.size <= maximum);
  const handle = await fs.open(item.path, constants.O_RDONLY | constants.O_NOFOLLOW); let bytes;
  try {
    const opened = await handle.stat(); need(opened.dev === info.dev && opened.ino === info.ino && opened.size === info.size && opened.mtimeMs === info.mtimeMs);
    bytes = await handle.readFile(); const after = await handle.stat(); need(after.ino === info.ino && after.size === info.size && after.mtimeMs === info.mtimeMs && sha(bytes) === item.sha256);
    return JSON.parse(bytes.toString('utf8'));
  } finally { bytes?.fill(0); await handle.close(); }
}
export function encodeReferenceRecord(value) { return Buffer.from(JSON.stringify(value) + '\n'); }
export async function publishReferenceRecord(filename, value, io = fs) {
  need(path.posix.dirname(filename) === REFERENCE_OUTPUT && /^(?:report|session-private|abort|failure-diagnostic|stage-(?:discovery|armed|restore-armed|restored))\.json$/.test(path.posix.basename(filename)));
  const bytes = encodeReferenceRecord(value), maximum = filename.endsWith('/report.json') || filename.endsWith('/failure-diagnostic.json')
    ? REFERENCE_LIMITS.report_bytes : REFERENCE_LIMITS.record_bytes;
  need(bytes.length > 0 && bytes.length <= maximum, 'reference_record_limit');
  let handle;
  const pending = filename + '.pending';
  try {
    handle = await io.open(pending, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
    await handle.writeFile(bytes); await handle.sync(); await handle.close(); handle = null;
    await io.link(pending, filename);
    const parent = await io.open(REFERENCE_OUTPUT, constants.O_RDONLY | constants.O_DIRECTORY | constants.O_NOFOLLOW);
    try { await parent.sync(); await io.unlink(pending); await parent.sync(); } finally { await parent.close(); }
    return { path: filename, sha256: sha(bytes) };
  } finally { bytes.fill(0); await handle?.close(); }
}

function catalogKind(row) {
  if (row.method !== 'GET') return row.kind;
  if ([`/Items/${ITEM}`, `/Users/${REFERENCE_USER}/Items/${ITEM}`].includes(row.route)) return 'target';
  if (!['/Items', `/Users/${REFERENCE_USER}/Items`].includes(row.route)) return row.kind;
  const query = Object.fromEntries(Object.entries(row.query ?? {}).map(([key, value]) => [key.toLowerCase(), value]));
  return query.parentid === LIBRARY || query.ids === ITEM ? 'items' : row.kind;
}
const TRANSPORT_FACT_FIELDS = ['transport_phase', 'error_code', 'request_content_type', 'request_body_sha256',
  'upstream_create_attempted', 'upstream_created', 'upstream_end_attempted', 'upstream_end_returned', 'upstream_response_received'];
export function validateReferenceTransportFacts(row, physical) {
  if (!physical) { need(TRANSPORT_FACT_FIELDS.every(key => row[key] === null), 'reference_frame_transport_facts'); return row; }
  const flags = TRANSPORT_FACT_FIELDS.slice(4);
  need(REFERENCE_HTTP_PHASES.includes(row.transport_phase) && (row.error_code === null || REFERENCE_HTTP_ERROR_CODES.includes(row.error_code)) &&
    REFERENCE_REQUEST_CONTENT_TYPES.includes(row.request_content_type) && (row.request_body_sha256 === null || SHA.test(row.request_body_sha256)) &&
    flags.every(key => typeof row[key] === 'boolean'), 'reference_physical_transport_facts');
  need((!row.upstream_created || row.upstream_create_attempted) && (!row.upstream_end_attempted || row.upstream_created) &&
    (!row.upstream_end_returned || row.upstream_end_attempted) && (!row.upstream_response_received || row.upstream_create_attempted),
  'reference_physical_transport_order');
  need((row.error_code !== null) === (row.failed === true), 'reference_physical_transport_error');
  if (row.completed === true) need(row.transport_phase === 'completed' && flags.every(key => row[key] === true) &&
    SHA.test(row.request_body_sha256) && row.error_code === null, 'reference_physical_transport_completion');
  else need(row.transport_phase !== 'completed', 'reference_physical_transport_completion');
  return row;
}
export function normalizeReferenceSnapshot(snapshot) {
  const result = clone(snapshot);
  const keys = ['id', 'index', 'method', 'kind', 'route', 'query', 'hidden_query', 'shape_sha256', 'request_sha256', 'token_sha256',
    'request_sequence', 'start_elapsed_ms', 'response_elapsed_ms', 'finished_elapsed_ms', 'phase', 'document_id', 'page_route',
    'sourceworker', 'main_frame', 'from_service_worker', 'content_type', 'status', 'completed', 'reason', 'request_bytes', 'response_bytes', ...TRANSPORT_FACT_FIELDS];
  const project = (row, physical) => {
    const value = Object.fromEntries(keys.map(key => [key, row[key] ?? null])); value.kind = catalogKind(row);
    value.outcome = row.outcome ?? (row.failed === true ? 'failed' : row.completed === true ? 'completed' : null);
    need(value.outcome === null || ['completed', 'failed', 'rejected'].includes(value.outcome));
    need(value.reason === null || typeof value.reason === 'string' && /^[a-z][a-z0-9_]{0,127}$/.test(value.reason));
    value.failed = row.failed === true || ['failed', 'rejected'].includes(value.outcome);
    validateReferenceTransportFacts(value, physical);
    value.projection = ['items', 'target'].includes(value.kind) && row.response && row.status === 200 ? projectReferenceItems(row.response, value.kind) : null;
    return value;
  };
  result.http.physical = result.http.physical.map(row => project(row, true)); result.http.frames = result.http.frames.map(row => project(row, false));
  delete result.report;
  return result;
}

/** Validate the single unified hierarchy before exposing its exact systemd path. */
export function canonicalReferenceCgroup(raw, unit) {
  need([REFERENCE_UNIT, REFERENCE_CONTROLLER_UNIT].includes(unit), 'reference_process_cgroup');
  const canonical = '/system.slice/' + unit;
  need(typeof raw === 'string' && (raw === '0::' + canonical + '\n' || raw === '0::' + canonical), 'reference_process_cgroup');
  return canonical;
}

/** Only the published worker identity changes representation; service pins keep raw proc data. */
export function projectReferenceNodeProcess(value, expectedBootID) {
  need(exact(value, ['pid', 'start_ticks', 'boot_id', 'uid', 'gid', 'executable_path', 'executable_sha256', 'cgroup']) &&
    Number.isSafeInteger(value.pid) && value.pid > 1 && /^[1-9]\d*$/.test(value.start_ticks) &&
    value.boot_id === expectedBootID && /^[0-9a-f-]{36}$/.test(value.boot_id) && value.uid === 0 && value.gid === 0 &&
    safeText(value.executable_path, 4096) && SHA.test(value.executable_sha256), 'reference_node_process');
  return { ...clone(value), cgroup: canonicalReferenceCgroup(value.cgroup, REFERENCE_UNIT) };
}

async function processIdentity(pid, executableHash = false) {
  need(Number.isSafeInteger(pid) && pid > 1);
  const prefix = '/proc/' + pid, stat = await fs.readFile(prefix + '/stat', 'utf8'), start = stat.slice(stat.lastIndexOf(')') + 2).trim().split(/\s+/)[19];
  const status = await fs.readFile(prefix + '/status', 'utf8');
  const result = { pid, start_ticks: start, boot_id: (await fs.readFile('/proc/sys/kernel/random/boot_id', 'utf8')).trim(),
    uid: Number(/^Uid:\s+(\d+)/m.exec(status)?.[1]), gid: Number(/^Gid:\s+(\d+)/m.exec(status)?.[1]),
    executable_path: await fs.readlink(prefix + '/exe'), cgroup: await fs.readFile(prefix + '/cgroup', 'utf8') };
  if (executableHash) { const bytes = await fs.readFile(prefix + '/exe'); try { result.executable_sha256 = sha(bytes); } finally { bytes.fill(0); } }
  return result;
}
async function checkDirectory(filename) {
  const info = await fs.lstat(filename); need(info.isDirectory() && !info.isSymbolicLink() && info.uid === 0 && info.gid === 0 && (info.mode & 0o777) === 0o700);
  return { dev: info.dev, ino: info.ino };
}
async function sourcePins(input) {
  for (const [filename, expected] of Object.entries(input.source_closure)) {
    const info = await fs.lstat(filename); need(info.isFile() && !info.isSymbolicLink() && info.uid === 0 && info.gid === 0 && info.nlink === 1 &&
      [0o600, 0o644].includes(info.mode & 0o777) && info.size > 0 && info.size <= 2 * 1024 * 1024);
    const handle = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW); let bytes;
    try { const open = await handle.stat(); need(open.dev === info.dev && open.ino === info.ino && open.size === info.size);
      bytes = await handle.readFile(); need(sha(bytes) === expected); const after = await handle.stat(); need(after.size === info.size && after.mtimeMs === info.mtimeMs);
    } finally { bytes?.fill(0); await handle.close(); }
  }
}

/** The controller owns metadata; this workflow owns only the ordinary browser. */
export class ReferenceWorkflow {
  constructor({ actor, input, binding, report, session, secrets = [], publish = publishReferenceRecord, readControl, readAbort = async () => null,
    now = () => Date.now(), wait = delay, dom = observeReferenceDOM }) {
    Object.assign(this, { actor, input, binding, report, session, secrets, publish, readControl, readAbort, now, wait, dom });
    this.started = actor.started; this.deadline = this.started + REFERENCE_LIMITS.work_ms; this.phase = 'discovery';
    this.reservation = null; this.last = null; this.previousStage = null; this.previousControl = null; this.boundary = null;
    this.samples = []; this.actions = []; this.lastContainers = []; this.closed = false;
    Object.assign(report, { stages: [], controls: [], discovery: null, armed: null, forward: null, restore_armed: null, restored: null });
  }
  active() { need(this.now() < this.deadline && !this.closed, 'reference_deadline'); }
  async noticeControllerAbort() {
    const source = await this.readAbort(); if (!source) return;
    validateReferenceAbort(source.value, this.binding, { stages: this.report.stages, controls: this.report.controls,
      token_sha256: this.report.login_proof?.token_sha256, session_private: this.session() });
    this.report.controller_abort = { path: source.path, sha256: source.sha256 }; throw new Error('reference_controller_aborted');
  }
  snapshot() { return normalizeReferenceSnapshot(this.actor.snapshot()); }
  async stable() {
    this.active(); await bounded(this.actor.settled(), 5000); await bounded(this.actor.assertPinned(), 15000);
    const snapshot = this.snapshot(), socket = snapshot.socket;
    need(socket.active === 1 && socket.failed === 0 && socket.connection_id && socket.token_sha256 === this.report.login_proof.token_sha256,
      'reference_socket_not_bound');
    if (this.boundary) need(referenceRoute(this.actor.page.url()) === this.boundary.route && snapshot.document_id === this.boundary.document_id &&
      socket.connection_id === this.boundary.connection_id && socket.seen === this.boundary.websocket_seen && socket.opened === this.boundary.websocket_opened &&
      snapshot.lifecycle.every(event => event.sequence <= this.boundary.started_sequence), 'reference_window_intervention');
    return snapshot;
  }
  async capture(expected, forbidden) {
    await this.noticeControllerAbort(); await this.stable(); const started = this.actor.elapsed(), snapshot = this.snapshot();
    const raw = await bounded(this.dom(this.actor.page, this.input, expected, forbidden, snapshot.document_id), 3000);
    this.lastContainers = raw.container_observations ?? []; delete raw.container_observations; raw.started_elapsed_ms = started;
    for (const key of ['observed_title', 'observed_anchor_title']) if (raw[key] !== null &&
      ![this.input.target.name, this.input.anchor.name, this.reservation?.marker_name].includes(raw[key])) raw[key] = sanitizeDiagnostic(raw[key], this.secrets);
    const current = this.snapshot(), after = this.boundary?.started_sequence ?? this.navigation.before_sequence;
    const context = { phase: this.boundary?.name ?? 'discovery', input: this.input, discovery: this.report.discovery, boundary: this.boundary && {
      ...this.boundary, end_elapsed_ms: Number.MAX_SAFE_INTEGER }, token_sha256: current.socket.token_sha256,
    after_sequence: after, physical: current.http.physical, frames: current.http.frames,
    events: { physical: current.events.physical.filter(row => row.sequence > after), browser: current.events.browser.filter(row => row.sequence > after) } };
    this.last = bindReferenceDOM(raw, context); return clone(this.last);
  }
  async stage(name, observation) {
    const value = referenceStageRecord(this.binding, name, observation, this.report.login_proof.token_sha256, this.session(), this.previousControl);
    const source = await this.publish(REFERENCE_OUTPUT + '/stage-' + name + '.json', value);
    this.previousStage = source.sha256; this.report.stages.push({ name, ...source }); return source;
  }
  async control(name, sample = undefined) {
    const until = Math.min(this.deadline, this.now() + REFERENCE_LIMITS.control_ms);
    while (this.now() < until) {
      this.active(); if (name !== 'close') await this.noticeControllerAbort(); const found = await this.readControl(name);
      if (found) {
        const value = validateReferenceControl(found.value, this.binding, { input: this.input, reservation: this.reservation, previous_stage_sha256: this.previousStage });
        need(value.name === name, 'reference_control_phase'); this.reservation ??= clone(value.reservation);
        this.previousControl = found.sha256; this.report.controls.push({ name, path: found.path, sha256: found.sha256 }); return found;
      }
      if (sample) await sample(); await this.wait(REFERENCE_LIMITS.poll_ms);
    }
    throw new Error('reference_control_timeout');
  }
  async discover() {
    await this.actor.loginUI(); this.report.login_proof = clone(this.actor.snapshot().report.login.proof);
    need(this.session() && this.report.login_proof.user_id === REFERENCE_USER, 'reference_login_not_durable');
    await this.actor.settled();
    const page = this.actor.page, library = this.input.expected_libraries.find(row => row.id === LIBRARY);
    const titles = page.getByText(library.name, { exact: true }).filter({ visible: true });
    const controls = titles.locator('xpath=ancestor-or-self::*[(self::button or self::a or @role="button") and @data-action="link" and ancestor::*[contains(concat(" ", normalize-space(@class), " "), " card ") or contains(concat(" ", normalize-space(@class), " "), " cardBox ")]][1]').filter({ visible: true });
    await controls.first().waitFor({ state: 'visible', timeout: 25000 }); need(await controls.count() === 1, 'reference_library_control_ambiguous');
    const ids = await controls.evaluate(element => [element, element.closest('.card'), element.closest('.cardBox')].filter(Boolean)
      .map(node => node.getAttribute('data-id')).filter(value => value !== null));
    need(ids.length > 0 && ids.every(value => value === LIBRARY), 'reference_library_control_identity');
    const before = this.snapshot(); this.navigation = { before_route: referenceRoute(page.url()), after_route: null, before_sequence: before.sequence };
    const home = { route: this.navigation.before_route, document_id: before.document_id, library_id: LIBRARY, library_name: library.name,
      control_count: 1, identity_proven: true };
    this.actor.setPhase('discovery'); this.actions.push({ ...this.actor.stamp(), kind: 'click-observed-movies-library' });
    await controls.click({ timeout: 8000 }); const until = Math.min(this.deadline, this.now() + 30000); let observation;
    do { observation = await this.capture(this.input.target.name, null); if (observation.passed && observation.route !== home.route) break;
      await this.wait(100); } while (this.now() < until);
    need(observation?.passed && observation.route !== home.route, 'reference_movies_not_proven');
    const snapshot = await this.stable(), reads = pairReferenceReads(snapshot.http.physical, snapshot.http.frames).filter(pair => pair.complete &&
      referenceFullMovieQuery(pair.physical) && responseMatches(pair.physical, this.input, this.input.target.name, true) &&
      pair.physical.request_sequence > this.navigation.before_sequence && pair.frame.request_sequence > this.navigation.before_sequence &&
      pair.frame.page_route === observation.route && pair.frame.document_id === observation.document_id &&
      pair.physical.phase === 'discovery' && pair.frame.phase === 'discovery' && pair.frame.token_sha256 === snapshot.socket.token_sha256);
    need(reads.length > 0 && reads.length <= 8, 'reference_movies_query_not_proven');
    const shapes = new Map(); for (const pair of reads) shapes.set(pair.physical.shape_sha256, { kind: 'items', route: pair.physical.route,
      query: pair.physical.query, hidden_query: pair.physical.hidden_query, shape_sha256: pair.physical.shape_sha256 });
    this.navigation.after_route = observation.route;
    this.report.discovery = { home, dom: observation, reads, query_allowlist: [...shapes.values()],
      socket: { connection_id: snapshot.socket.connection_id, token_sha256: snapshot.socket.token_sha256, seen: snapshot.socket.seen, opened: snapshot.socket.opened },
      navigation: clone(this.navigation) };
    await this.stage('discovery', this.report.discovery);
  }
  async quiet() {
    const first = await this.stable(), started = this.actor.elapsed(), samples = [];
    const catalog = rows => rows.filter(row => ['items', 'target'].includes(row.kind));
    const firstMessages = referenceLibraryChangedMessages(first.events);
    need(catalog(first.http.physical).every(row => row.completed) && catalog(first.http.frames).every(row => row.completed || row.failed), 'reference_pending_http');
    while (this.actor.elapsed() < started + REFERENCE_LIMITS.quiet_ms) {
      const began = this.actor.elapsed(), observation = await this.capture(this.reservation.original_name, this.reservation.marker_name);
      samples.push({ ...this.actor.stamp(), started_elapsed_ms: began, observation });
      const now = this.snapshot(), messages = referenceLibraryChangedMessages(now.events);
      need(observation.passed && catalog(now.http.physical).length === catalog(first.http.physical).length && catalog(now.http.frames).length === catalog(first.http.frames).length &&
        messages.physical.length === firstMessages.physical.length && messages.browser.length === firstMessages.browser.length &&
        now.document_id === first.document_id && now.route === first.route && now.socket.connection_id === first.socket.connection_id &&
        now.socket.seen === first.socket.seen && samples.length <= REFERENCE_LIMITS.quiet_samples, 'reference_quiet_changed');
      await this.wait(Math.min(REFERENCE_LIMITS.sample_ms, Math.max(1, started + REFERENCE_LIMITS.quiet_ms - this.actor.elapsed())));
    }
    const final = await this.stable(), completed = this.actor.elapsed();
    const requested = row => row.request_sequence > first.sequence && row.start_elapsed_ms >= started && row.start_elapsed_ms <= completed;
    const received = row => row.sequence > first.sequence && row.elapsed_ms >= started && row.elapsed_ms <= completed;
    const http = { physical: final.http.physical.filter(requested), frames: final.http.frames.filter(requested) };
    const events = { physical: final.events.physical.filter(received), browser: final.events.browser.filter(received) };
    const selected = referenceLibraryChangedMessages(events), catalogRequests = catalog(http.physical).length + catalog(http.frames).length;
    const libraryMessages = selected.physical.length + selected.browser.length;
    need(catalogRequests === 0 && libraryMessages === 0, 'reference_quiet_changed');
    return { started_elapsed_ms: started, completed_elapsed_ms: completed, started_sequence: first.sequence, duration_ms: REFERENCE_LIMITS.quiet_ms,
      catalog_requests: catalogRequests, library_changed_messages: libraryMessages, http, events, samples, passed: true };
  }
  async arm(name) {
    await this.stable(); this.actor.setPhase(name); const snapshot = this.snapshot();
    this.boundary = { name, started_sequence: snapshot.sequence, started_elapsed_ms: snapshot.elapsed_ms,
      route: referenceRoute(this.actor.page.url()), document_id: snapshot.document_id, connection_id: snapshot.socket.connection_id,
      token_sha256: snapshot.socket.token_sha256, websocket_seen: snapshot.socket.seen, websocket_opened: snapshot.socket.opened };
    this.samples = []; return clone(this.boundary);
  }
  async sample() {
    if (this.samples.length && this.actor.elapsed() - this.samples.at(-1).elapsed_ms < REFERENCE_LIMITS.sample_ms) return;
    const expected = this.boundary.name === 'forward' ? this.reservation.marker_name : this.reservation.original_name;
    const forbidden = this.boundary.name === 'forward' ? this.reservation.original_name : this.reservation.marker_name;
    const started = this.actor.elapsed(), observation = await this.capture(expected, forbidden);
    this.samples.push({ ...this.actor.stamp(), started_elapsed_ms: started, observation });
    need(this.samples.length <= REFERENCE_LIMITS.window_samples, 'reference_sample_limit');
  }
  async finish(name, control) {
    const end = instant(control.value.commit.write_completed_at) - this.started + REFERENCE_LIMITS.window_ms;
    need(end > this.actor.elapsed() && this.started + end <= this.deadline, 'reference_window_ack_expired');
    while (this.actor.elapsed() < end) { await this.sample(); await this.wait(Math.min(100, Math.max(1, end - this.actor.elapsed()))); }
    const snapshot = await this.stable(), inside = row => row.sequence > this.boundary.started_sequence && row.elapsed_ms >= this.boundary.started_elapsed_ms && row.elapsed_ms <= end;
    const started = row => row.request_sequence > this.boundary.started_sequence && row.start_elapsed_ms >= this.boundary.started_elapsed_ms && row.start_elapsed_ms <= end;
    const physical = snapshot.http.physical.filter(started), frames = snapshot.http.frames.filter(started);
    const result = { name, control_sha256: control.sha256, commit: clone(control.value.commit), boundary: { ...clone(this.boundary),
      response_completed_elapsed_ms: end - REFERENCE_LIMITS.window_ms, end_elapsed_ms: end, completed_elapsed_ms: this.actor.elapsed(), duration_ms: REFERENCE_LIMITS.window_ms },
    events: { physical: snapshot.events.physical.filter(inside), browser: snapshot.events.browser.filter(inside) },
    http: { physical, frames, pairs: referencePairReferences(physical, frames) }, dom: this.samples.filter(inside),
    actions: this.actions.filter(inside), lifecycle: snapshot.lifecycle.filter(inside) };
    Object.assign(result, referenceWindowEvidence(result, this.input, this.reservation, this.report.discovery)); this.report[name] = result;
    this.boundary = null; return result;
  }
  async execute() {
    await this.discover(); this.phase = 'reserved'; await this.control('reserved');
    const quiet = await this.quiet(); this.phase = 'armed'; const forwardBoundary = await this.arm('forward');
    this.report.armed = { quiet, boundary: forwardBoundary }; await this.stage('armed', this.report.armed);
    const forwardControl = await this.control('forward', () => this.sample()); this.phase = 'forward'; const forward = await this.finish('forward', forwardControl);
    this.phase = 'restore-armed'; const restoredBoundary = await this.arm('restored'); this.report.restore_armed = restoredBoundary;
    await this.stage('restore-armed', { forward, boundary: restoredBoundary });
    const restoredControl = await this.control('restored', () => this.sample()); this.phase = 'restored'; const restored = await this.finish('restored', restoredControl);
    await this.stage('restored', { restored }); this.phase = 'close'; await this.control('close');
    this.report.restoration = 'confirmed'; this.report.protocol_observation_complete = true;
  }
}

export async function runReferenceBrowser(options) {
  const runStarted = Date.now();
  const args = parseReferenceArguments(Object.entries(options).flatMap(([key, value]) => ['--' + key, value]));
  need(process.platform === 'linux' && process.getuid?.() === 0 && SELF === REFERENCE_TOOL + '/client-browser-library-changed-reference.mjs', 'reference_execution_scope');
  const inputItem = { path: args.input, sha256: args['input-sha256'] }, input = validateReferenceInput(await ownedJSON(inputItem));
  const recovery = await readReferencePriorRecovery(input);
  const rootIdentity = await checkDirectory(REFERENCE_ROOT), owner = await ownedJSON(input.authority.owner), preflight = await ownedJSON(input.authority.preflight);
  need(instant(preflight.completed_at) >= instant(recovery.terminal.captured_at), 'reference_preflight_before_recovery');
  const baseline = validateReferencePublicBaseline(input, owner, preflight, await ownedJSON(input.authority.before_snapshot));
  const credentials = await ownedJSON(input.actor.credentials);
  need(exact(credentials, ['viewer']) && exact(credentials.viewer, ['username', 'password', 'userId']) && credentials.viewer.username === VIEWER &&
    credentials.viewer.userId === REFERENCE_USER && /^[0-9a-f]{64}$/.test(credentials.viewer.password), 'reference_credentials_scope');
  await sourcePins(input); const nodeProcess = projectReferenceNodeProcess(await processIdentity(process.pid, true), input.controller.boot_id);
  const binding = { input_sha256: args['input-sha256'], source_closure_sha256: sha(JSON.stringify(ordered(input.source_closure))),
    controller: clone(input.controller), node_process: nodeProcess };
  const pins = [inputItem, ...Object.values(input.authority), REFERENCE_PRIOR_RECOVERY_REPORT, input.actor.credentials];
  const pin = async () => {
    need(same(await checkDirectory(REFERENCE_ROOT), rootIdentity));
    for (const item of pins) await ownedJSON(item);
    await sourcePins(input); const controller = await processIdentity(input.controller.pid);
    need(controller.start_ticks === input.controller.start_ticks && controller.boot_id === input.controller.boot_id && controller.uid === 0 &&
      canonicalReferenceCgroup(controller.cgroup, REFERENCE_CONTROLLER_UNIT) === '/system.slice/' + REFERENCE_CONTROLLER_UNIT);
    const service = input.reference.service_identity, current = await processIdentity(service.pid);
    need(current.start_ticks === service.startTicks && current.boot_id === service.bootId && current.uid === service.uid &&
      current.executable_path === service.exe && current.cgroup === service.cgroup && await fs.readlink('/proc/' + service.pid + '/ns/net') === service.networkNamespace &&
      same((await fs.readFile('/proc/' + service.pid + '/cmdline', 'utf8')).split('\0').filter(Boolean), service.cmdline));
  };
  await pin(); await fs.mkdir(REFERENCE_OUTPUT, { mode: 0o700 });
  const report = { marker: 'goby-reference-library-changed-browser-v1', version: 1, ...binding,
    reference: clone(input.reference), target: clone(input.target), anchor: clone(input.anchor), prior_recovery: clone(input.authority.prior_recovery), started_at: new Date().toISOString(),
    protocol_observation_complete: false, library_changed_client_acceptance: false, main_acceptance: false, restoration: 'pending', actor: {}, failure: null };
  let actor, workflow, session = null, cleanupDeadline = null;
  const secrets = [credentials.viewer.password], publishing = new Map();
  const readJournal = async name => {
    need(['control-reserved', 'control-forward', 'control-restored', 'control-close', 'abort'].includes(name)); const filename = REFERENCE_ROOT + '/' + name + '.json';
    try { const info = await fs.lstat(filename);
      need(info.isFile() && !info.isSymbolicLink() && info.uid === 0 && info.gid === 0 && [1, 2].includes(info.nlink) && (info.mode & 0o777) === 0o600 && info.size <= REFERENCE_LIMITS.record_bytes);
      if (!publishing.has(name)) publishing.set(name, Date.now());
      need(Date.now() - publishing.get(name) <= 5000, 'reference_control_publication_timeout');
      let pending = null; try { pending = await fs.lstat(filename + '.pending'); } catch (error) { if (error?.code !== 'ENOENT') throw error; }
      if (pending) {
        need(pending.isFile() && !pending.isSymbolicLink() && pending.uid === 0 && pending.gid === 0 && (pending.mode & 0o777) === 0o600 &&
          pending.dev === info.dev && pending.ino === info.ino && pending.nlink === 2 && info.nlink === 2, 'reference_foreign_control_publication');
        return null;
      }
      if (info.nlink === 2) { const current = await fs.lstat(filename); if (current.nlink === 1 && current.dev === info.dev && current.ino === info.ino) return null;
        throw new Error('reference_foreign_control_publication'); }
      const handle = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW); let bytes;
      try { const opened = await handle.stat(); need(opened.dev === info.dev && opened.ino === info.ino);
        bytes = await handle.readFile(); const after = await handle.stat();
        if (bytes.length === 0 || after.size !== info.size || after.mtimeMs !== info.mtimeMs) return null;
        let value; try { value = JSON.parse(bytes.toString('utf8')); } catch (error) { if (error?.name === 'SyntaxError') return null; throw error; }
        publishing.delete(name); return { path: filename, sha256: sha(bytes), value };
      } finally { bytes?.fill(0); await handle.close(); }
    } catch (error) { if (error?.code === 'ENOENT') return null; throw error; }
  };
  const readControl = name => readJournal('control-' + name), readAbort = () => readJournal('abort');
  try {
    actor = await createReferenceBrowserActor({ account: { id: REFERENCE_USER, username: VIEWER, password: credentials.viewer.password },
      existingDeviceIDs: baseline.device_ids, pin, report: report.actor, observer: {}, onLogin: async ({ token, proof }) => {
        need(!session && proof.user_id === REFERENCE_USER && proof.server_id === REFERENCE_SERVER && proof.token_sha256 === sha(token));
        secrets.push(token);
        session = await publishReferenceRecord(REFERENCE_OUTPUT + '/session-private.json', { marker: 'goby-reference-library-changed-session-private-v1',
          version: 1, ...binding, token, proof }); report.session_private = session; report.login_proof = clone(proof);
      } });
    report.started_at = new Date(actor.started).toISOString(); workflow = new ReferenceWorkflow({ actor, input, binding, report, session: () => session, secrets, readControl, readAbort });
    workflow.deadline = Math.min(workflow.deadline, runStarted + REFERENCE_LIMITS.work_ms); await workflow.execute();
  } catch (error) {
    cleanupDeadline = Math.min(Date.now() + REFERENCE_LIMITS.cleanup_ms, runStarted + REFERENCE_LIMITS.work_ms + REFERENCE_LIMITS.cleanup_ms);
    report.failure = /^reference_[a-z_]+$/.test(error?.message ?? '') ? error.message : 'reference_browser_failed';
    if (workflow) {
      try { await publishReferenceRecord(REFERENCE_OUTPUT + '/abort.json', { marker: 'goby-reference-library-changed-abort-v1', version: 1, ...binding,
        name: workflow.phase, failure: report.failure, previous_stage_sha256: workflow.previousStage, previous_control_sha256: workflow.previousControl,
        token_sha256: report.login_proof?.token_sha256 ?? null, session_private: session }); } catch { report.abort_publication_failed = true; }
      try { const deadline = cleanupDeadline - 40000;
        while (Date.now() < deadline) { const close = await readControl('close'); if (close) {
          validateReferenceControl(close.value, binding, { input, reservation: workflow.reservation, previous_stage_sha256: workflow.previousStage, allow_not_required: true });
          report.restoration = close.value.restoration; break; } await delay(100); }
      } catch { report.cleanup_control_failed = true; }
    }
  } finally {
    cleanupDeadline ??= Math.min(Date.now() + REFERENCE_LIMITS.cleanup_ms, runStarted + REFERENCE_LIMITS.work_ms + REFERENCE_LIMITS.cleanup_ms);
    if (report.failure && actor) {
      try { const snapshot = normalizeReferenceSnapshot(actor.snapshot());
        const value = { marker: 'goby-reference-library-changed-failure-v1', version: 1, reason: report.failure,
        phase: workflow?.phase ?? 'login', last_dom: workflow?.last ?? null, containers: workflow?.lastContainers ?? [],
        counts: { physical_http: snapshot.http.physical.length, frame_http: snapshot.http.frames.length,
          physical_messages: snapshot.events.physical.length, browser_messages: snapshot.events.browser.length },
        last_responses: snapshot.http.physical.filter(row => row.projection).slice(-8).map(row => ({ id: row.id, phase: row.phase,
          status: row.status, outcome: row.outcome, projection: row.projection })), runtime_failures: report.actor.failures ?? [] };
        const artifact = await publishReferenceRecord(REFERENCE_OUTPUT + '/failure-diagnostic.json', value);
        report.diagnostics = { ...artifact, bytes: encodeReferenceRecord(value).length };
      } catch { report.diagnostics = { reason: 'failure_diagnostic_unavailable' }; }
    }
    if (actor) { try { await bounded(actor.logoutUI(), Math.max(1, cleanupDeadline - Date.now() - 15000)); } catch { report.failure ??= 'reference_ui_logout_failed'; }
      try { await bounded(actor.close(), Math.max(1, cleanupDeadline - Date.now())); } catch { report.failure ??= 'reference_browser_close_failed'; }
      report.observations = normalizeReferenceSnapshot(actor.snapshot()); }
    credentials.viewer.password = null; report.completed_at = new Date().toISOString();
    if (report.failure) report.protocol_observation_complete = false;
    report.result = report.failure ? 'failed' : 'protocol_observation_complete';
    try { const encoded = JSON.stringify(report); need(secrets.every(secret => !secret || !encoded.includes(secret)), 'reference_report_secret_guard');
      await publishReferenceRecord(REFERENCE_OUTPUT + '/report.json', report);
    } finally { secrets.fill(null); }
  }
  return report;
}

if (process.argv[1] && path.resolve(process.argv[1]) === SELF) {
  try { const report = await runReferenceBrowser(parseReferenceArguments(process.argv.slice(2)));
    process.stdout.write(JSON.stringify({ marker: report.marker, result: report.result, protocol_observation_complete: report.protocol_observation_complete,
      browser_report: REFERENCE_OUTPUT + '/report.json' }) + '\n'); if (report.result === 'failed') process.exitCode = 1;
  } catch { process.stdout.write(JSON.stringify({ marker: 'goby-reference-library-changed-browser-v1', result: 'failed', failure: 'reference_setup_failed' }) + '\n'); process.exitCode = 1; }
}
