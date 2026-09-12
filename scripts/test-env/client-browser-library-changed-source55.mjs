#!/usr/bin/env node
/** Observe one real catalog edit and restoration through an unchanged original client. */
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { TextDecoder } from 'node:util';
import { createLibraryChangedSource55BrowserActor, authorityForURL,
  safeBrowserFailure, resanitizeBrowserDiagnostics, sanitizeBrowserMessage } from './client-browser-cross-user.mjs';
import { HomeViewsObserver, waitHomeLoginProof, observeHomeDOM, checkedHomeFile,
  homeSourceDigest, homeSessionClosed, procStartTicks, validateHomeExpectedLibraries } from './client-browser-library-home.mjs';
import { loadLibraryChangedSource55Fixture, readLibraryChangedSource55Snapshot,
  libraryChangedSetupDiagnostic } from './client-library-changed-source55-fixture.mjs';

const WORK = '/opt/goby-test/exec-work-m3e';
export const CHANGED_ROOT = WORK + '/client-library-changed-ui-source55-v6';
export const CHANGED_OUTPUT = CHANGED_ROOT + '/browser';
export const CHANGED_UNIT = 'goby-client-library-changed-ui-source55-v6.service';
export const CHANGED_CONTROLLER_UNIT = 'goby-client-library-changed-ui-source55-controller-v6.service';
const ROOT = CHANGED_ROOT, OUTPUT = CHANGED_OUTPUT;
const ORIGIN = 'http://127.0.0.1:18196', DIRECT = 'http://127.0.0.1:18198';
const USER = 'ecbbe4cb82403879bc4b4f78894c5738';
const LIBRARY = 'a9993591e72f0f2e7babcbf8b9c50790';
const ITEM = '268051d3ca734aefcf94e245fb25ad55';
const SERVER = 'c7cfd76b1dee728b2bad523793a37ccb';
const SOURCE_CREDENTIALS_SHA = '0be6df4acef0565537b3b3a6e8c1518f6bed4023bfdfb5dea60b67dd32fee790';
const SELF = fileURLToPath(import.meta.url);
const SHA = /^[0-9a-f]{64}$/, ID = /^[0-9a-f]{32}$/;
const SUBVIEWS = ['movies', 'movies', 'folders'];
const ARRAYS = ['ItemsAdded', 'ItemsRemoved', 'ItemsUpdated', 'FoldersAddedTo', 'FoldersRemovedFrom', 'CollectionFolders'];
const STAGES = ['discovery', 'armed', 'restore-armed', 'restored'];
const CONTROLS = ['reserved', 'forward', 'restored', 'close'];
const SOURCES = ['client-browser-library-changed-source55.mjs', 'client-library-changed-source55-fixture.mjs',
  'client-browser-library-home.mjs', 'client-browser-cross-user.mjs', 'client-browser-session-proof.mjs',
  'client-browser-goby-fixture.mjs', 'client-browser-special-features-fixture.mjs'];
export const CHANGED_LIMITS = Object.freeze({ work_ms: 390000, cleanup_ms: 90000, abort_wait_ms: 170000,
  control_wait_ms: 90000, quiet_ms: 20000, window_ms: 120000, sample_ms: 500, poll_ms: 100,
  samples: 600, catalog_per_window: 20, catalog_total: 64, events: 64, lifecycle: 128,
  message_bytes: 65536, json_bytes: 2 * 1024 * 1024, record_bytes: 512 * 1024,
  report_bytes: 4 * 1024 * 1024, publication_wait_ms: 5000 });
export const CHANGED_DIAGNOSTIC_LIMITS = Object.freeze({ capture_ms: 3000, publish_ms: 3000,
  screenshot_bytes: 4 * 1024 * 1024, target_nodes: 24, structure_nodes: 96, text_chars: 256,
  viewport_elements: 4096, viewport_text_nodes: 2048, viewport_chars: 8192, masks: 24 });
const record = value => value !== null && typeof value === 'object' && !Array.isArray(value);
const clone = value => Array.isArray(value) ? value.map(clone) : record(value)
  ? Object.fromEntries(Object.entries(value).map(([key, child]) => [key, clone(child)])) : value;
const ordered = value => Array.isArray(value) ? value.map(ordered) : record(value)
  ? Object.fromEntries(Object.keys(value).sort().map(key => [key, ordered(value[key])])) : value;
const canonical = value => typeof value === 'bigint' ? value.toString() + 'n' : Array.isArray(value)
  ? '[' + value.map(canonical).join(',') + ']' : record(value)
    ? '{' + Object.keys(value).sort().map(key => JSON.stringify(key) + ':' + canonical(value[key])).join(',') + '}' : JSON.stringify(value);
const same = (left, right) => canonical(left) === canonical(right);
const exact = (value, names) => record(value) && same(Object.keys(value).sort(), [...names].sort());
const sha = value => createHash('sha256').update(value).digest('hex');
function need(value, code = 'library_changed_guard_rejected') {
  if (!value) { const error = new Error(code); Error.captureStackTrace?.(error, need); throw error; }
}
const delay = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
const safeText = (value, maximum = 256) => typeof value === 'string' && value.length > 0 && value.length <= maximum &&
  !/[\x00-\x1f\x7f]/.test(value);
const descriptor = value => exact(value, ['path', 'sha256']) && typeof value.path === 'string' &&
  value.path.startsWith(WORK + '/') && !value.path.includes('\\') && path.posix.normalize(value.path) === value.path &&
  /^[\x21-\x7e]+$/.test(value.path) && SHA.test(value.sha256);
function instant(value) {
  need(typeof value === 'string' && /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+]00:00)$/.test(value));
  const result = Date.parse(value); need(Number.isFinite(result)); return result;
}
function bounded(promise, milliseconds, code = 'library_changed_operation_timeout') {
  let timer;
  return Promise.race([promise, new Promise((_, reject) => {
    timer = setTimeout(() => reject(new Error(code)), milliseconds);
  })]).finally(() => clearTimeout(timer));
}

export function parseLibraryChangedArguments(argv) {
  need(Array.isArray(argv) && argv.length === 6);
  const result = {};
  for (let index = 0; index < argv.length; index += 2) {
    const key = argv[index]?.slice(2);
    need(argv[index]?.startsWith('--') && ['input', 'input-sha256', 'output'].includes(key) && !Object.hasOwn(result, key));
    result[key] = argv[index + 1];
  }
  need(result.input === ROOT + '/input.json' && result.output === OUTPUT && SHA.test(result['input-sha256']));
  return result;
}

export function validateLibraryChangedInput(input) {
  need(exact(input, ['marker', 'version', 'mode', 'root', 'output', 'actor', 'candidate', 'fixture',
    'expected_libraries', 'target', 'source_closure', 'authority', 'controller']) &&
    input.marker === 'goby-client-library-changed-input-v1' && input.version === 1 &&
    input.mode === 'b-movies-name-automatic-refresh' && input.root === ROOT && input.output === OUTPUT);
  need(exact(input.actor, ['slot', 'user_id', 'credentials', 'account_key', 'source_credentials_sha256']) &&
    input.actor.slot === 'B' && input.actor.user_id === USER && input.actor.account_key === 'viewer' &&
    input.actor.source_credentials_sha256 === SOURCE_CREDENTIALS_SHA && descriptor(input.actor.credentials) &&
    input.actor.credentials.path === ROOT + '/viewer-credentials.json');
  const candidate = input.candidate;
  need(exact(candidate, ['binary_sha256', 'process', 'runtime_sha256', 'state_sha256', 'server_id',
    'base_url', 'direct_url', 'source', 'source_manifest_sha256', 'invocation_id', 'publication']) &&
    ['binary_sha256', 'runtime_sha256', 'state_sha256'].every(key =>
      SHA.test(candidate[key]) && !/^([0-9a-f])\1{63}$/.test(candidate[key])) &&
    exact(candidate.process, ['pid', 'start_ticks', 'boot_id']) && Number.isSafeInteger(candidate.process.pid) && candidate.process.pid > 1 &&
    Number.isSafeInteger(candidate.process.start_ticks) && candidate.process.start_ticks > 0 &&
    /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/.test(candidate.process.boot_id) &&
    ID.test(candidate.invocation_id) && !/^([0-9a-f])\1{31}$/.test(candidate.invocation_id) &&
    /^[0-9a-f]{40}$/.test(candidate.publication) && !/^([0-9a-f])\1{39}$/.test(candidate.publication) &&
    candidate.server_id === SERVER && candidate.base_url === ORIGIN && candidate.direct_url === DIRECT &&
    candidate.source === WORK + '/source-attempt-55' &&
    candidate.source_manifest_sha256 === '7d2548603e209ebce6154853321147aeec33ccbb40cc12b3ca37418ad765937a');
  need(exact(input.fixture, ['profile_receipt', 'profile_report', 'profile_inspection', 'music_chain', 'music_scan_receipt']) &&
    Object.values(input.fixture).every(descriptor));
  validateHomeExpectedLibraries(input.expected_libraries);
  need(input.expected_libraries.length === 4 && input.expected_libraries.some(value => value.id === LIBRARY && value.name === 'M3e Client Movies'));
  need(exact(input.target, ['id', 'library_id', 'name', 'type', 'parent_id', 'root_id', 'relative_path']) &&
    input.target.id === ITEM && input.target.library_id === LIBRARY && input.target.type === 'Movie' &&
    ID.test(input.target.parent_id) && ID.test(input.target.root_id) && safeText(input.target.name, 256) &&
    safeText(input.target.relative_path, 4096) && !input.target.relative_path.startsWith('/') &&
    !input.target.relative_path.includes('\\') && path.posix.normalize(input.target.relative_path) === input.target.relative_path &&
    !input.target.relative_path.split('/').some(value => value === '..' || value === '.' || value === ''));
  need(exact(input.authority, ['upgrade_intent', 'upgrade_report', 'upgrade_attestation', 'current_snapshot', 'before_snapshot', 'history']) &&
    ['upgrade_intent', 'upgrade_report', 'upgrade_attestation', 'current_snapshot', 'before_snapshot'].every(key => descriptor(input.authority[key])));
  const upgradeRoot = path.posix.dirname(input.authority.upgrade_report.path);
  need(path.posix.dirname(upgradeRoot) === WORK &&
    /^client-schema28-source55-upgrade-\d{8}_\d{6}_[0-9a-f]{12}$/.test(path.posix.basename(upgradeRoot)) &&
    input.authority.upgrade_intent.path === WORK + '/client-schema28-source55-tool-05/intent.json' &&
    input.authority.upgrade_report.path === upgradeRoot + '/report.json' &&
    input.authority.upgrade_attestation.path === upgradeRoot + '/attestation.json' &&
    input.authority.current_snapshot.path === upgradeRoot + '/after-full.json' &&
    input.authority.before_snapshot.path === ROOT + '/before-full.json');
  const historyKeys = ['input', 'browser_report', 'controller_report', 'terminal', 'before_snapshot', 'after_snapshot'];
  const historyHashes = [
    ['c88794dbd9bdedcb6c16fea4dd8a7d8af2d09012728084b30e24701db5872c73', 'c980edd73bbf08c6190391f2c5368abd7973c5c1381bcec70d7be93477f8c743',
      '467f473ef20b1b05bfc76c863b41f69eeb77f1f2a783f57d6549beaa46f28104', 'b23a1a156e781c771e3bb4b1ba31bfb048e29e77504569d129397c79445265d6',
      '36ff8634f85a58841c1c6e4558de5e4dcfea1a842bae8e40a26c1d37c12ff32f', 'a13f976b7097e33337527ef2cf10ad9203d755e0fd2cec43307edfa6efbfe8bc'],
    ['eab8c899d06b525271afec345d4137b3e0adec70bb5e8ecf911fdf4dae7e9f6e', '9360e3b5c60b16f714b80b2da3f1f7a31d58313e196abee99dfbb4f8e3bc9d1f',
      'e9b931c568c98e4438917d8c204922267b0931a2de4c9f6bba40f2155c196fb4', 'e53e9777337c8c0b33a0cc86dad2e22c79d5e7f2601aa64dd68c6ed91cd9ecf1',
      '10563f9b12e62a321bbda67c49bfbfc1a6e3d2c2e304d3c2e1e7d64502b2c897', 'a84e5e480a70b7d84e49001aaae791a522cd38c6b1055dcb503f0312f933c2dd'],
    ['963bf8d50a68ffd9534146f66d53df10b099818307f2496517cc89c97161a94d', 'f99ff64ced6abb41a0c5f913a2a66cf18e36bb6527a46a43172f0e7c78a89cf4',
      '6d4acb05e4c0f7328c1cc28c41b17062c08bdd05eb441bcd70786649fa6d4a88', '021a36cc9e6311107acf751b1bba7ca6b2bf1e6618c4a32b6b48c17866404012',
      'f37fe8d9134a98cc780ecafb20053a0c140429efc18df47f5e4106ffefa501f9', 'bd992669d5c9ade22768f210009e983b46384c2611a8d8d91986cb729f41236f'],
    ['093dfa5fc0d82ba509a0092c425ce64d33eebf3625441751427248065bdf2176', '86e0dafc48e8e9a54b83c4588f7a06cabf951eab4a7d250b3d326cd9271d2abe',
      '154bd9857248eec76e921b2f34f3896e9a1fa73aeb078a64d03d06b54ca7e444', '348be31960bb19a9905c4cef2b6f19df9af25466e28959f00d1bd5e0a6a2183d',
      '6bd9998f16407711b5b3cb46a4f3b233822a7147203f353ff55bb2092950e46e', 'a3d633f0d351001eff02d013c2ccc224adec9c5f5a659cab7bad3f4e9a4a20d6'] ];
  need(Array.isArray(input.authority.history) && input.authority.history.length === 4);
  input.authority.history.forEach((entry, index) => {
    const version = index + 2, historicalRoot = WORK + '/client-library-changed-ui-source55-v' + version;
    need(exact(entry, ['version', ...historyKeys]) && entry.version === version);
    const filenames = [historicalRoot + '/input.json', historicalRoot + '/browser/report.json', historicalRoot + '/report.json',
      WORK + '/client-library-changed-source55-execution-0' + version + '/failed-terminal.json', historicalRoot + '/before-full.json', historicalRoot + '/after-full.json'];
    historyKeys.forEach((key, at) => need(descriptor(entry[key]) && entry[key].path === filenames[at] && entry[key].sha256 === historyHashes[index][at]));
  });
  need(exact(input.controller, ['pid', 'start_ticks', 'boot_id', 'unit']) && Number.isSafeInteger(input.controller.pid) && input.controller.pid > 1 &&
    /^[1-9]\d*$/.test(input.controller.start_ticks) && input.controller.boot_id === candidate.process.boot_id &&
    input.controller.unit === CHANGED_CONTROLLER_UNIT);
  need(record(input.source_closure) && Object.keys(input.source_closure).length === SOURCES.length &&
    Object.entries(input.source_closure).every(([filename, hash]) => descriptor({ path: filename, sha256: hash })));
  const scripts = Object.keys(input.source_closure).filter(filename => filename.endsWith('.mjs'));
  need(same(scripts.sort(), SOURCES.map(name => WORK + '/client-library-changed-source55-tool-06b/' + name).sort()));
  return input;
}

export function validateLibraryChangedViewer(value, input) {
  need(exact(value, ['marker', 'slot', 'user_id', 'base_url', 'direct_url', 'viewer']) &&
    value.marker === 'goby-client-library-changed-viewer-v1' && value.slot === 'B' && value.user_id === USER &&
    value.base_url === ORIGIN && value.direct_url === DIRECT && exact(value.viewer, ['username', 'password']) &&
    value.viewer.username === 'm3e-client-viewer' && /^[0-9a-f]{48}$/.test(value.viewer.password));
  return { slot: 'B', id: USER, username: value.viewer.username, password: value.viewer.password,
    credentialsPath: input.actor.credentials.path, credentialsSHA: input.actor.credentials.sha256 };
}

export function validateLibraryChangedBaseline(input, before, current) {
  const first = before?.database, previous = current?.database;
  need(record(first) && record(previous) && before.schema === 28 && current.schema === 28);
  const captured = value => {
    const result = clone(value); instant(result.database.metadata.captured_at);
    delete result.database.metadata.captured_at; return result;
  };
  need(same(captured(before), captured(current)) && instant(first.metadata.captured_at) > instant(previous.metadata.captured_at));
  const tables = first.tables;
  need(Object.keys(tables).length === 35 && Object.values(tables).every(Array.isArray) && Object.keys(first.sequences).length === 5 &&
    tables.sessions.length === 79 && tables.devices.length === 68 &&
    tables.activity_entries.length === 175 && tables.play_sessions.length === 26 && tables.user_item_data.length === 7 &&
    tables.libraries.length === 4 && tables.items.length === 22 && tables.client_playback_references.length === 0 && tables.encoding_jobs.length === 0);
  const user = tables.users.find(row => row.id === USER), target = tables.items.find(row => row.id === ITEM);
  const scopedMovies = tables.items.filter(row => row.library_id === LIBRARY && row.type === 'Movie' && row.is_folder === false);
  need(scopedMovies.length === 1 && scopedMovies[0].id === ITEM &&
    tables.items.filter(row => row.library_id === LIBRARY && row.name === input.target.name).length === 1,
  'library_changed_singleton_scope_required');
  need(user && user.is_disabled === false && user.is_administrator === false && user.management_revision === 5 && target &&
    target.library_id === LIBRARY && target.name === input.target.name && target.type === 'Movie' && target.is_folder === false &&
    target.parent_id === input.target.parent_id && target.root_id === input.target.root_id && target.relative_path === input.target.relative_path &&
    tables.item_metadata_state.filter(row => row.item_id === ITEM).length === 1 &&
    tables.item_entities.every(row => row.item_id !== ITEM) &&
    tables.item_extra_resources.every(row => row.owner_item_id !== ITEM && row.resource_item_id !== ITEM) &&
    tables.item_theme_resources.every(row => row.owner_item_id !== ITEM && row.resource_item_id !== ITEM));
  need(same(tables.libraries.map(row => ({ id: row.id, name: row.name })).sort((a, b) => a.id.localeCompare(b.id)),
    [...input.expected_libraries].sort((a, b) => a.id.localeCompare(b.id))));
  return { devices: tables.devices.map(row => row.reported_device_id), session_ids: tables.sessions.map(row => row.id),
    token_hashes: tables.sessions.map(row => { need(/^\\x[0-9a-f]{64}$/.test(row.token_hash)); return row.token_hash.slice(2); }) };
}

/** Read only the three declared snapshot paths without rounding PostgreSQL integers. */
export async function readLibraryChangedSnapshot(input, key, read = readLibraryChangedSource55Snapshot) {
  validateLibraryChangedInput(input);
  need(['current_snapshot', 'before_snapshot', 'history_after_snapshot'].includes(key) && typeof read === 'function');
  return read(input, key);
}

const SETUP_PHASES = Object.freeze(['arguments', 'input', 'root_identity', 'process_identity', 'source_closure',
  'baseline', 'credentials', 'fixture', 'initial_pin', 'output']);

export function libraryChangedSetupFailure(error, phase) {
  const diagnostic = libraryChangedSetupDiagnostic(error, SETUP_PHASES.includes(phase) ? phase : 'arguments');
  return Object.assign(new Error('library_changed_setup_failed'), { diagnostic });
}

function validateReservation(value, input) {
  need(exact(value, ['target_id', 'library_id', 'revision', 'original_name', 'marker_name', 'original_controls_sha256',
    'forward_body_sha256', 'restore_body_sha256']) && value.target_id === ITEM && value.library_id === LIBRARY &&
    typeof value.revision === 'string' && /^[1-9]\d{0,18}$/.test(value.revision) && BigInt(value.revision) <= 9223372036854775805n &&
    value.original_name === input.target.name && safeText(value.marker_name, 256) && value.marker_name !== value.original_name &&
    [value.original_controls_sha256, value.forward_body_sha256, value.restore_body_sha256].every(hash => SHA.test(hash)) &&
    value.forward_body_sha256 !== value.restore_body_sha256);
}

export function validateLibraryChangedControl(value, binding, state, now = Date.now()) {
  need(exact(value, ['marker', 'version', 'input_sha256', 'source_closure_sha256', 'controller', 'node_process', 'name',
    'previous_stage_sha256', 'reservation', 'commit', 'restoration']) &&
    value.marker === 'goby-client-library-changed-control-v1' && value.version === 1 && CONTROLS.includes(value.name) &&
    ['input_sha256', 'source_closure_sha256', 'controller', 'node_process'].every(key => same(value[key], binding[key])));
  const previous = state.stages.at(-1), close = value.name === 'close';
  if (close) {
    need(['confirmed', 'not_required'].includes(value.restoration));
    if (!state.aborted) need(previous?.name === 'restored' && value.previous_stage_sha256 === previous.sha256 && value.restoration === 'confirmed');
    else need(value.previous_stage_sha256 === null && state.stages.length === 0 && state.stage_attempts.length === 0 ||
      [...state.stages, ...state.stage_attempts].some(stage => stage.sha256 === value.previous_stage_sha256));
  } else {
    const stages = { reserved: 'discovery', forward: 'armed', restored: 'restore-armed' };
    need(!state.aborted && previous?.name === stages[value.name] && value.previous_stage_sha256 === previous.sha256 &&
      !state.consumed.includes(value.name));
  }
  if (value.reservation === null) {
    need(close && state.aborted && !state.reservation && value.commit === null && value.restoration === 'not_required');
    return clone(value);
  }
  validateReservation(value.reservation, state.input);
  if (state.reservation) need(same(value.reservation, state.reservation));
  if (value.name === 'reserved') need(value.commit === null && value.restoration === 'pending');
  else if (close && value.restoration === 'not_required') need(state.aborted && value.commit === null &&
    !state.consumed.includes('forward') && !state.stages.some(stage => ['restore-armed', 'restored'].includes(stage.name)));
  else {
    need(exact(value.commit, ['revision', 'write_completed_at', 'native_result_sha256', 'readback_sha256']) &&
      typeof value.commit.revision === 'string' && /^[1-9]\d{0,18}$/.test(value.commit.revision) &&
      BigInt(value.commit.revision) === BigInt(value.reservation.revision) + (value.name === 'forward' ? 1n : 2n) &&
      SHA.test(value.commit.native_result_sha256) && SHA.test(value.commit.readback_sha256) &&
      instant(value.commit.write_completed_at) <= now + 1000 && instant(value.commit.write_completed_at) >= state.started_at - 1000 &&
      value.restoration === (value.name === 'forward' ? 'pending' : 'confirmed'));
    if (!close) need(instant(value.commit.write_completed_at) >= previous.publication_started_at - 1000);
  }
  return clone(value);
}

/** Reject duplicate JSON object keys before interpreting a bounded public response. */
export function parseLibraryChangedJSON(bytes, maximum = CHANGED_LIMITS.json_bytes) {
  need(Buffer.isBuffer(bytes) && bytes.length > 0 && bytes.length <= maximum, 'library_changed_payload_limit');
  const source = new TextDecoder('utf-8', { fatal: true }).decode(bytes);
  let index = 0, nodes = 0;
  const space = () => { while (/[\t\r\n ]/.test(source[index] ?? '') && index < source.length) index++; };
  const string = () => {
    const start = index++; need(source[start] === '"');
    while (index < source.length) {
      const character = source[index++];
      if (character === '"') return JSON.parse(source.slice(start, index));
      if (character === '\\') index++;
    }
    throw new Error('library_changed_invalid_json');
  };
  const value = depth => {
    need(depth <= 32 && ++nodes <= 65536, 'library_changed_json_complexity'); space();
    if (source[index] === '{') {
      index++; space(); const keys = new Set();
      if (source[index] === '}') { index++; return; }
      for (;;) {
        space(); need(source[index] === '"'); const key = string();
        need(!keys.has(key), 'library_changed_duplicate_json_key'); keys.add(key); space(); need(source[index++] === ':');
        value(depth + 1); space(); const next = source[index++];
        if (next === '}') return; need(next === ',');
      }
    }
    if (source[index] === '[') {
      index++; space(); if (source[index] === ']') { index++; return; }
      for (;;) { value(depth + 1); space(); const next = source[index++]; if (next === ']') return; need(next === ','); }
    }
    if (source[index] === '"') { string(); return; }
    const scalar = /^(?:true|false|null|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)/.exec(source.slice(index));
    need(scalar); index += scalar[0].length;
  };
  value(0); space(); need(index === source.length);
  return JSON.parse(source);
}

export function projectLibraryChangedMessage(bytes, targetID = ITEM) {
  const value = parseLibraryChangedJSON(bytes, CHANGED_LIMITS.message_bytes);
  need(exact(value, ['MessageType', 'MessageId', 'Data']) && value.MessageType === 'LibraryChanged' &&
    safeText(value.MessageId, 256) && exact(value.Data, [...ARRAYS, 'IsEmpty']) && value.Data.IsEmpty === false);
  for (const name of ARRAYS) need(Array.isArray(value.Data[name]) && value.Data[name].length <= 256 &&
    value.Data[name].every(id => ID.test(id)) && same(value.Data[name], name === 'ItemsUpdated' ? [targetID] : []));
  return { MessageType: value.MessageType, MessageId: value.MessageId, Data: clone(value.Data),
    body_sha256: sha(bytes), body_bytes: bytes.length, projection_sha256: sha(JSON.stringify(ordered(value))) };
}

/** Classify public catalog reads without exporting authentication carriers. */
export function libraryChangedCatalogRequest(raw, method, input) {
  if (method !== 'GET' || typeof raw !== 'string' || raw.length > 16384) return null;
  const url = new URL(raw);
  if (url.origin !== ORIGIN || url.username || url.password || url.hash || url.href !== raw) return null;
  const pathname = url.pathname.replace(/^\/emby(?=\/)/i, '');
  const ownBase = `/Users/${USER}/Items`, query = {}, publicQuery = [];
  for (const [key, value] of url.searchParams) {
    const lower = key.toLowerCase(); need(!Object.hasOwn(query, lower) && key.length <= 128 && value.length <= 4096);
    query[lower] = value;
    need(!/password|credential|csrf/i.test(lower));
    if (!['api_key', 'x-emby-token', 'x-mediabrowser-token', 'token', 'access_token', 'authorization'].includes(lower) &&
      !lower.startsWith('x-emby-')) publicQuery.push([key, value]);
  }
  need(Object.keys(query).length <= 64);
  if (query.userid !== undefined && query.userid !== USER) return null;
  let kind;
  if (pathname === ownBase + '/' + ITEM || pathname === '/Items/' + ITEM) kind = 'target';
  else if (pathname === ownBase + '/' + LIBRARY || pathname === '/Items/' + LIBRARY) kind = 'collection-folder';
  else if ((pathname === ownBase || pathname === '/Items') &&
    (query.parentid === LIBRARY && (query.ids === undefined || query.ids === ITEM) ||
      query.ids === ITEM && (query.parentid === undefined || query.parentid === LIBRARY))) kind = 'items';
  else return null;
  if (query.ids !== undefined && query.ids !== ITEM || query.limit !== undefined && !/^(?:[1-9]\d?|1[01]\d|12[0-8])$/.test(query.limit)) return null;
  publicQuery.sort((left, right) => left[0].localeCompare(right[0]) || left[1].localeCompare(right[1]));
  need(Buffer.byteLength(JSON.stringify(publicQuery)) <= 4096, 'library_changed_query_limit');
  return { kind, route: pathname, query: publicQuery, shape_sha256: sha(JSON.stringify([pathname, publicQuery])),
    request_sha256: sha(method + '\n' + url.href) };
}

export function projectLibraryChangedItems(bytes, kind, targetID = ITEM) {
  if (kind === 'collection-folder') {
    const folder = parseLibraryChangedJSON(bytes);
    need(record(folder) && folder.Id === LIBRARY && folder.Name === 'M3e Client Movies' && folder.Type === 'CollectionFolder' &&
      folder.IsFolder === true && folder.CollectionType === 'movies' &&
      same(folder.Subviews, SUBVIEWS), 'library_changed_collection_folder_shape');
    return { collection_folder: { Id: folder.Id, Name: folder.Name, Type: folder.Type, Subviews: clone(folder.Subviews) },
      count: 1, body_sha256: sha(bytes), body_bytes: bytes.length };
  }
  need(kind === 'items' || kind === 'target');
  const value = parseLibraryChangedJSON(bytes), items = kind === 'target' ? [value] : value.Items;
  need(Array.isArray(items) && items.length <= 128 && items.every(item => record(item) && ID.test(item.Id)));
  const selected = items.filter(item => item.Id === targetID); need(selected.length === 1 && safeText(selected[0].Name, 256));
  const target = selected[0]; need(target.Type === 'Movie' && target.IsFolder !== true);
  return { target: { Id: target.Id, Name: target.Name, Type: target.Type }, count: items.length,
    body_sha256: sha(bytes), body_bytes: bytes.length };
}

/** Project only a public DTO field outline beside the unchanged acceptance projection. */
export function libraryChangedResponseDiagnostic(bytes, kind, secrets = []) {
  if (!['items', 'target'].includes(kind)) return null;
  const value = parseLibraryChangedJSON(bytes), items = kind === 'target' ? [value] : value.Items;
  need(Array.isArray(items) && items.length <= 128);
  const item = items.find(value => record(value) && value.Id === ITEM); if (!item) return null;
  const type = value => value === null ? 'null' : Array.isArray(value) ? 'array' : typeof value;
  const fields = ['Id', 'Name', 'Type', 'ServerId', 'LocationType', 'IsFolder', 'MediaType', 'ImageTags', 'UserData',
    'MediaSources', 'Path', 'ProviderIds', 'RunTimeTicks', 'ProductionYear', 'Overview', 'ParentId', 'ChildCount', 'IsVirtualItem'];
  const outline = Object.fromEntries(fields.map(key => [key, { present: Object.hasOwn(item, key), type: Object.hasOwn(item, key) ? type(item[key]) : 'missing' }]));
  const variants = libraryChangedSecretVariants(secrets), keys = Object.keys(item);
  const topLevelFields = keys.slice(0, 128).map(key => ({ key: /token|password|secret|credential|authorization|path/i.test(key) ? '[sensitive-key]'
    : !/^[A-Z][A-Za-z0-9]{0,63}$/.test(key) || knownDiagnosticSecret(key, variants) ? '[redacted-key]'
      : sanitizeLibraryChangedText(key, secrets, variants), type: type(item[key]) }));
  const unknown = {};
  for (const key of Object.keys(item)) if (!fields.includes(key)) { const category = type(item[key]); unknown[category] = (unknown[category] ?? 0) + 1; }
  return { target_id: ITEM, top_level_count: keys.length, top_level_fields: topLevelFields, fields_truncated: keys.length > topLevelFields.length,
    fields: outline, unknown_field_types: unknown,
    public_values: { ServerId: item.ServerId === SERVER ? SERVER : null,
      LocationType: ['FileSystem', 'Virtual', 'Remote', 'Offline'].includes(item.LocationType) ? item.LocationType : null,
      IsFolder: typeof item.IsFolder === 'boolean' ? item.IsFolder : null,
      MediaType: ['Video', 'Audio', 'Photo', 'Book', 'Game'].includes(item.MediaType) ? item.MediaType : null } };
}

export function libraryChangedRoute(raw) {
  need(typeof raw === 'string' && raw.length <= 4096);
  const url = new URL(raw);
  need(url.origin === ORIGIN && !url.username && !url.password &&
    (url.pathname === '/web/index.html' || url.pathname === '/web/' || url.pathname === '/web'));
  for (const values of [url.searchParams, new URLSearchParams(url.hash.split('?').slice(1).join('?'))]) {
    for (const [key] of values) need(!/token|password|api_key|authorization/i.test(key));
  }
  return url.pathname + url.search + url.hash;
}

export function pairLibraryChangedReads(physical, frames) {
  need(Array.isArray(physical) && Array.isArray(frames) && physical.length <= CHANGED_LIMITS.catalog_total &&
    frames.length <= CHANGED_LIMITS.catalog_total * 2);
  need(new Set(physical.map(item => item.id)).size === physical.length && new Set(frames.map(item => item.index)).size === frames.length);
  const eligible = frames.filter(frame => SHA.test(frame.token_sha256) && SHA.test(frame.request_sha256) && SHA.test(frame.shape_sha256) &&
    frame.finished && !frame.failed && frame.status === 200 &&
    frame.content_type === 'application/json' && frame.from_service_worker === false &&
    (frame.source === 'page' && frame.main_frame === true ||
      frame.source === 'worker' && frame.main_frame === false && safeText(frame.worker_id, 80)));
  const fits = (frame, transfer) => frame.kind === transfer.kind && transfer.completed && transfer.terminal_status === 200 && transfer.status === 200 &&
    transfer.projection && frame.token_sha256 === transfer.token_sha256 && frame.request_sha256 === transfer.request_sha256 &&
    frame.shape_sha256 === transfer.shape_sha256 && frame.request_elapsed_ms <= transfer.request_elapsed_ms &&
    transfer.request_elapsed_ms <= frame.finished_elapsed_ms && transfer.finished_elapsed_ms <= frame.finished_elapsed_ms + 1000;
  return eligible.map(frame => {
    const matches = physical.filter(transfer => fits(frame, transfer));
    const transfer = matches.length === 1 ? matches[0] : null;
    const unique = transfer !== null && eligible.filter(other => fits(other, transfer)).length === 1;
    return { frame: clone(frame), physical: unique ? clone(transfer) : null, unambiguous: unique,
      complete: Boolean(unique && frame.finished && transfer.completed) };
  });
}

/** Preserve every independently derived pairing outcome without duplicating wire records. */
export function pairLibraryChangedReadReferences(physical, frames) {
  return pairLibraryChangedReads(physical, frames).map(pair => ({ frame_request_index: pair.frame.index,
    physical_exchange_id: pair.physical?.id ?? null, complete: pair.complete, unambiguous: pair.unambiguous }));
}

export function libraryChangedFullMovieQuery(transfer) {
  if (transfer?.kind !== 'items' || !Array.isArray(transfer.query)) return false;
  const query = {};
  for (const entry of transfer.query) {
    if (!Array.isArray(entry) || entry.length !== 2 || typeof entry[0] !== 'string' || typeof entry[1] !== 'string') return false;
    const key = entry[0].toLowerCase(); if (Object.hasOwn(query, key)) return false; query[key] = entry[1];
  }
  return query.parentid === LIBRARY && query.includeitemtypes === 'Movie' && query.recursive === 'true' &&
    query.startindex === '0' && query.limit === '50' && query.ids === undefined;
}

function libraryChangedMoviesRoute(route) {
  try {
    const url = new URL(ORIGIN + route); if (libraryChangedRoute(url.href) !== route || url.hash.split('?')[0] !== '#!/videos') return false;
    const values = [...new URLSearchParams(url.hash.split('?').slice(1).join('?'))];
    const parents = values.filter(([key]) => key.toLowerCase() === 'parentid'), servers = values.filter(([key]) => key.toLowerCase() === 'serverid');
    return parents.length === 1 && parents[0][1] === LIBRARY && servers.length <= 1 && (!servers.length || servers[0][1] === SERVER);
  } catch { return false; }
}

export function libraryChangedIdentityMessage(boundary, events) {
  if (!boundary || !events || events.browser?.length !== 1 || events.physical?.length !== 1) return null;
  const browser = events.browser[0], physical = events.physical[0];
  const inside = value => value.sequence > boundary.started_sequence && value.elapsed_ms >= boundary.started_elapsed_ms &&
    value.token_sha256 === boundary.token_sha256 && value.connection_id === boundary.connection_id && value.complete === true && value.received === true;
  if (!inside(browser) || !inside(physical) || physical.forwarded !== true || !same(browser.message, physical.message) ||
    physical.elapsed_ms > browser.elapsed_ms || browser.route !== boundary.route || browser.document_id !== boundary.document_id ||
    browser.message?.MessageType !== 'LibraryChanged' || !safeText(browser.message.MessageId, 256)) return null;
  const message = browser.message;
  if (!exact(message.Data, [...ARRAYS, 'IsEmpty']) || message.Data.IsEmpty !== false ||
    !ARRAYS.every(key => same(message.Data[key], key === 'ItemsUpdated' ? [ITEM] : [])) || !SHA.test(message.body_sha256) ||
    !SHA.test(message.projection_sha256) || !Number.isSafeInteger(message.body_bytes) || message.body_bytes <= 0 ||
    message.body_bytes > CHANGED_LIMITS.message_bytes || message.projection_sha256 !== sha(JSON.stringify(ordered({
      MessageType: message.MessageType, MessageId: message.MessageId, Data: message.Data })))) return null;
  return browser;
}

/** Bind a raw visible card only to a completed same-phase wire response and a proved library anchor. */
export function bindLibraryChangedDOM(raw, context) {
  const result = { ...clone(raw), target_id: null, identity_mode: 'unbound', wire_identity: null, identity_proven: false, passed: false };
  delete result.container_observations;
  if (!record(raw) || !['discovery', 'forward', 'restored'].includes(context?.phase) || !libraryChangedMoviesRoute(raw.route) ||
    !safeText(raw.document_id, 80) || !safeText(raw.expected_name, 256) ||
    !Number.isSafeInteger(raw.visible_items_containers) || raw.visible_items_containers < 0 || raw.visible_items_containers > 32 ||
    raw.visible_card_containers !== 1 || raw.visible_cards !== 1 || raw.visible_target_cards !== 1 || raw.visible_title_buttons !== 1 ||
    raw.target_title_count !== 1 || raw.forbidden_title_count !== 0 || raw.explicit_identity_consistent !== true || raw.media_inactive !== true ||
    raw.observed_title !== raw.expected_name || !Number.isFinite(raw.started_elapsed_ms) || raw.started_elapsed_ms < 0 || !SHA.test(context.token_sha256)) return result;
  let afterSequence = context.after_sequence, afterElapsed = 0, messageID = null;
  if (context.phase !== 'discovery') {
    const anchor = context.discovery;
    if (!anchor?.dom?.passed || anchor.dom.identity_mode !== 'singleton-movie-list-wire-and-card' ||
      anchor.dom.route !== raw.route || anchor.dom.document_id !== raw.document_id || anchor.socket?.token_sha256 !== context.token_sha256) return result;
    if (context.boundary?.name !== context.phase || context.boundary.token_sha256 !== context.token_sha256 ||
      context.boundary.route !== raw.route || context.boundary.document_id !== raw.document_id) return result;
    if (!Array.isArray(anchor.reads) || !anchor.reads.length || !anchor.reads.every(value => value?.physical && value?.frame) ||
      !Array.isArray(anchor.query_allowlist) || !anchor.query_allowlist.length ||
      !anchor.query_allowlist.every(value => anchor.reads.some(pair => libraryChangedFullMovieQuery(pair.physical) &&
        value.kind === 'items' && value.shape_sha256 === pair.physical.shape_sha256 && value.route === pair.physical.route && same(value.query, pair.physical.query)))) return result;
    const rebound = bindLibraryChangedDOM(anchor.dom, { phase: 'discovery', physical: anchor.reads.map(value => value.physical),
      frames: anchor.reads.map(value => value.frame), token_sha256: context.token_sha256, after_sequence: anchor.navigation?.before_sequence });
    if (!rebound.passed || !same(rebound.wire_identity, anchor.dom.wire_identity)) return result;
    if (!anchor.reads.every(pair => bindLibraryChangedDOM(anchor.dom, { phase: 'discovery', physical: [pair.physical], frames: [pair.frame],
      token_sha256: context.token_sha256, after_sequence: anchor.navigation.before_sequence }).passed)) return result;
    const message = libraryChangedIdentityMessage(context.boundary, context.events); if (!message) return result;
    afterSequence = message.sequence; afterElapsed = message.elapsed_ms; messageID = message.message.MessageId;
  }
  if (!Number.isSafeInteger(afterSequence) || afterSequence < 0) return result;
  const pairs = pairLibraryChangedReads(context.physical, context.frames);
  const selected = pairs.filter(pair => {
    const transfer = pair.physical, frame = pair.frame, target = transfer?.projection?.target;
    const fullItems = libraryChangedFullMovieQuery(transfer) && [`/Users/${USER}/Items`, '/Items'].includes(transfer.route) && (context.phase === 'discovery' ||
      context.discovery.query_allowlist.some(value => value.kind === 'items' && value.shape_sha256 === transfer.shape_sha256));
    const targetRead = context.phase !== 'discovery' && transfer?.kind === 'target' &&
      [`/Users/${USER}/Items/${ITEM}`, `/Items/${ITEM}`].includes(transfer.route);
    return pair.complete && (fullItems || targetRead) && transfer.method === 'GET' && transfer.terminal === 'completed' &&
      frame.route === transfer.route && SHA.test(transfer.projection.body_sha256) &&
      Number.isSafeInteger(transfer.projection.body_bytes) && transfer.projection.body_bytes > 0 && transfer.projection.body_bytes <= CHANGED_LIMITS.json_bytes &&
      transfer.phase === context.phase && frame.phase === context.phase && transfer.projection.count === 1 &&
      target?.Id === ITEM && target.Type === 'Movie' && target.Name === raw.observed_title &&
      transfer.token_sha256 === context.token_sha256 && frame.token_sha256 === context.token_sha256 &&
      frame.page_route === raw.route && frame.document_id === raw.document_id &&
      frame.request_sequence > afterSequence && transfer.request_sequence > afterSequence &&
      frame.request_elapsed_ms >= afterElapsed && transfer.request_elapsed_ms >= afterElapsed &&
      frame.finished_elapsed_ms <= raw.started_elapsed_ms && transfer.finished_elapsed_ms <= raw.started_elapsed_ms;
  }).sort((left, right) => left.frame.finished_elapsed_ms - right.frame.finished_elapsed_ms);
  if (!selected.length) return result;
  const pair = selected[0];
  return { ...result, target_id: pair.physical.projection.target.Id, identity_mode: 'singleton-movie-list-wire-and-card',
    wire_identity: { phase: context.phase, physical_exchange_id: pair.physical.id, frame_request_index: pair.frame.index,
      body_sha256: pair.physical.projection.body_sha256, shape_sha256: pair.physical.shape_sha256,
      request_sha256: pair.physical.request_sha256, token_sha256: pair.physical.token_sha256, message_id: messageID },
    identity_proven: true, passed: true };
}

/** Prove Movies navigation from the actual completed CollectionFolder exchange. */
export function libraryChangedNavigationEvidence(discovery, input) {
  need(input.target?.id === ITEM && input.target.library_id === LIBRARY && record(discovery) &&
    discovery.dom?.passed === true && discovery.dom.target_id === ITEM && discovery.dom.expected_name === input.target.name &&
    safeText(discovery.dom.route, 4096) && safeText(discovery.dom.document_id, 80) &&
    exact(discovery.navigation, ['before_route', 'after_route', 'before_sequence']) &&
    safeText(discovery.navigation.before_route, 4096) && discovery.navigation.after_route === discovery.dom.route &&
    discovery.navigation.before_route !== discovery.navigation.after_route && Number.isSafeInteger(discovery.navigation.before_sequence) &&
    discovery.navigation.before_sequence >= 0 &&
    SHA.test(discovery.socket?.token_sha256) && Array.isArray(discovery.collection_folder_reads) &&
    discovery.collection_folder_reads.length > 0 && discovery.collection_folder_reads.length <= 8,
  'library_changed_collection_folder_not_proven');
  need(Array.isArray(discovery.reads) && discovery.reads.length > 0 && discovery.reads.length <= 8 &&
    discovery.reads.every(value => value?.physical && value?.frame), 'library_changed_singleton_anchor_missing');
  const rebound = bindLibraryChangedDOM(discovery.dom, { phase: 'discovery', physical: discovery.reads.map(value => value.physical),
    frames: discovery.reads.map(value => value.frame), token_sha256: discovery.socket.token_sha256,
    after_sequence: discovery.navigation.before_sequence });
  need(rebound.passed && same(rebound.wire_identity, discovery.dom.wire_identity), 'library_changed_singleton_anchor_missing');
  need(discovery.reads.every(pair => bindLibraryChangedDOM(discovery.dom, { phase: 'discovery', physical: [pair.physical], frames: [pair.frame],
    token_sha256: discovery.socket.token_sha256, after_sequence: discovery.navigation.before_sequence }).passed), 'library_changed_singleton_anchor_missing');
  const supplied = discovery.collection_folder_reads;
  need(supplied.every(pair => record(pair) && record(pair.physical) && record(pair.frame)));
  const pairs = pairLibraryChangedReads(supplied.map(pair => pair.physical), supplied.map(pair => pair.frame));
  const routes = [`/Users/${USER}/Items/${LIBRARY}`, `/Items/${LIBRARY}`];
  const accepted = pairs.filter(pair => {
    const physical = pair.physical, frame = pair.frame, projection = physical?.projection, folder = projection?.collection_folder;
    return pair.complete && physical.kind === 'collection-folder' && frame.kind === 'collection-folder' &&
      physical.method === 'GET' && physical.terminal === 'completed' && routes.includes(physical.route) && frame.route === physical.route &&
      [discovery.navigation.before_route, discovery.navigation.after_route].includes(frame.page_route) &&
      frame.request_sequence > discovery.navigation.before_sequence && physical.request_sequence > discovery.navigation.before_sequence &&
      frame.document_id === discovery.dom.document_id &&
      physical.token_sha256 === discovery.socket.token_sha256 && frame.token_sha256 === discovery.socket.token_sha256 &&
      projection.count === 1 && SHA.test(projection.body_sha256) && projection.body_bytes > 0 && projection.body_bytes <= CHANGED_LIMITS.json_bytes &&
      folder?.Id === LIBRARY && folder.Name === 'M3e Client Movies' && folder.Type === 'CollectionFolder' && same(folder.Subviews, SUBVIEWS);
  });
  need(accepted.length === supplied.length, 'library_changed_collection_folder_not_proven');
  const selected = accepted.sort((left, right) => left.frame.finished_elapsed_ms - right.frame.finished_elapsed_ms)[0];
  return { id: LIBRARY, type: 'CollectionFolder', subviews: clone(SUBVIEWS),
    physical_exchange_id: selected.physical.id, frame_request_index: selected.frame.index, passed: true };
}

/** Recompute success from the recorded events, transfers and DOM, never from a passed flag. */
export function libraryChangedWindowEvidence(window, input, reservation, discovery) {
  validateReservation(reservation, input);
  need(record(window) && ['forward', 'restored'].includes(window.name) && SHA.test(window.control_sha256));
  const b = window.boundary, expected = window.name === 'forward' ? reservation.marker_name : reservation.original_name;
  need(record(b) && b.name === window.name && safeText(b.route, 4096) && safeText(b.document_id, 80) &&
    safeText(b.connection_id, 80) && SHA.test(b.token_sha256) && Number.isSafeInteger(b.started_sequence) && b.started_sequence >= 0 &&
    [b.started_elapsed_ms, b.response_completed_elapsed_ms, b.end_elapsed_ms, b.completed_elapsed_ms].every(value => Number.isFinite(value) && value >= 0) &&
    b.duration_ms === CHANGED_LIMITS.window_ms && b.response_completed_elapsed_ms >= b.started_elapsed_ms - 1000 &&
    b.end_elapsed_ms === b.response_completed_elapsed_ms + CHANGED_LIMITS.window_ms && b.completed_elapsed_ms >= b.end_elapsed_ms &&
    b.completed_elapsed_ms <= b.end_elapsed_ms + 5000 && b.websocket_seen >= 1 && b.websocket_seen <= 2 && b.websocket_opened >= 1 &&
    b.websocket_opened <= b.websocket_seen);
  need(exact(window.commit, ['revision', 'write_completed_at', 'native_result_sha256', 'readback_sha256']) &&
    typeof window.commit.revision === 'string' && /^[1-9]\d{0,18}$/.test(window.commit.revision) &&
    BigInt(window.commit.revision) === BigInt(reservation.revision) + (window.name === 'forward' ? 1n : 2n) &&
    SHA.test(window.commit.native_result_sha256) && SHA.test(window.commit.readback_sha256));
  instant(window.commit.write_completed_at);
  need(exact(window.events, ['physical', 'browser']) && Array.isArray(window.events.physical) && Array.isArray(window.events.browser) &&
    window.events.physical.length <= CHANGED_LIMITS.events && window.events.browser.length <= CHANGED_LIMITS.events &&
    record(window.http) && Array.isArray(window.http.physical) && Array.isArray(window.http.frames) &&
    window.http.physical.length <= CHANGED_LIMITS.catalog_per_window && window.http.frames.length <= CHANGED_LIMITS.catalog_per_window * 2 &&
    Array.isArray(window.dom) && window.dom.length <= CHANGED_LIMITS.samples &&
    Array.isArray(window.actions) && window.actions.length === 0 && Array.isArray(window.lifecycle) && window.lifecycle.length === 0,
  'library_changed_window_intervention');
  const inWindow = event => event.sequence > b.started_sequence && event.elapsed_ms >= b.started_elapsed_ms && event.elapsed_ms <= b.end_elapsed_ms;
  const bind = event => event.connection_id === b.connection_id && event.token_sha256 === b.token_sha256;
  for (const entry of [...window.events.physical, ...window.events.browser]) {
    need(inWindow(entry) && bind(entry) && entry.complete === true && entry.received === true && record(entry.message) &&
      entry.message.MessageType === 'LibraryChanged' && safeText(entry.message.MessageId, 256) &&
      SHA.test(entry.message.body_sha256) && SHA.test(entry.message.projection_sha256) && entry.message.body_bytes > 0 &&
      entry.message.body_bytes <= CHANGED_LIMITS.message_bytes && exact(entry.message.Data, [...ARRAYS, 'IsEmpty']) && entry.message.Data.IsEmpty === false);
    for (const name of ARRAYS) need(same(entry.message.Data[name], name === 'ItemsUpdated' ? [ITEM] : []));
    need(entry.message.projection_sha256 === sha(JSON.stringify(ordered({ MessageType: entry.message.MessageType,
      MessageId: entry.message.MessageId, Data: entry.message.Data }))));
  }
  for (const sample of window.dom) {
    need(inWindow(sample) && Number.isFinite(sample.started_elapsed_ms) && sample.started_elapsed_ms <= sample.elapsed_ms &&
      sample.observation?.route === b.route && sample.observation.document_id === b.document_id &&
      [null, ITEM].includes(sample.observation.target_id) && sample.observation.expected_name === expected && sample.observation.media_inactive === true &&
      Number.isFinite(sample.observation.started_elapsed_ms) && sample.observation.started_elapsed_ms >= sample.started_elapsed_ms &&
      sample.observation.started_elapsed_ms <= sample.elapsed_ms,
    'library_changed_document_or_route_changed');
  }
  for (let index = 1; index < window.dom.length; index++) {
    need(window.dom[index].sequence > window.dom[index - 1].sequence &&
      window.dom[index].started_elapsed_ms >= window.dom[index - 1].elapsed_ms &&
      window.dom[index].started_elapsed_ms - window.dom[index - 1].elapsed_ms <= CHANGED_LIMITS.sample_ms + 3500,
    'library_changed_sampling_incomplete');
  }
  if (window.dom.length) need(window.dom[0].started_elapsed_ms <= b.started_elapsed_ms + CHANGED_LIMITS.sample_ms + 3500,
    'library_changed_sampling_incomplete');
  need(same(window.http.pairs, pairLibraryChangedReadReferences(window.http.physical, window.http.frames)),
    'library_changed_pair_references_mismatch');
  const failure = outcome => ({ result: 'not_observed_within_window', outcome, proof: null });
  if (window.events.browser.length === 0 || window.events.physical.length === 0) return failure('websocket_not_observed_within_window');
  need(window.events.browser.length === 1 && window.events.physical.length === 1, 'library_changed_ambiguous_event');
  const received = window.events.browser[0], upstream = window.events.physical[0];
  need(upstream.forwarded === true && same(received.message, upstream.message) && upstream.elapsed_ms <= received.elapsed_ms &&
    received.document_id === b.document_id && received.route === b.route, 'library_changed_delivery_not_bound');
  need(discovery?.dom?.expected_name === reservation.original_name && discovery.dom.route === b.route &&
    discovery.dom.document_id === b.document_id && discovery.socket?.token_sha256 === b.token_sha256,
  'library_changed_singleton_anchor_missing');
  const pairs = pairLibraryChangedReads(window.http.physical, window.http.frames);
  const accepted = pairs.filter(pair => pair.complete && ['items', 'target'].includes(pair.physical.kind) &&
    pair.physical.phase === window.name && pair.frame.phase === window.name && pair.physical.projection?.count === 1 &&
    pair.physical.method === 'GET' && pair.physical.terminal === 'completed' &&
    SHA.test(pair.physical.projection.body_sha256) && Number.isSafeInteger(pair.physical.projection.body_bytes) &&
    pair.physical.projection.body_bytes > 0 && pair.physical.projection.body_bytes <= CHANGED_LIMITS.json_bytes &&
    pair.frame.route === pair.physical.route &&
    (pair.physical.kind === 'target' && [`/Users/${USER}/Items/${ITEM}`, `/Items/${ITEM}`].includes(pair.physical.route) ||
      libraryChangedFullMovieQuery(pair.physical) && [`/Users/${USER}/Items`, '/Items'].includes(pair.physical.route) &&
      discovery.query_allowlist.some(value => value.kind === 'items' && value.shape_sha256 === pair.physical.shape_sha256)) &&
    record(pair.physical.projection?.target) && pair.frame.request_sequence > received.sequence &&
    pair.physical.request_sequence > received.sequence && pair.physical.request_elapsed_ms >= received.elapsed_ms &&
    pair.frame.request_elapsed_ms >= received.elapsed_ms && pair.frame.finished_elapsed_ms <= b.end_elapsed_ms &&
    pair.physical.finished_elapsed_ms <= b.end_elapsed_ms && pair.frame.token_sha256 === b.token_sha256 &&
    pair.physical.token_sha256 === b.token_sha256 && pair.frame.document_id === b.document_id && pair.frame.page_route === b.route &&
    pair.physical.projection.target.Id === ITEM && pair.physical.projection.target.Type === 'Movie' && pair.physical.projection.target.Name === expected);
  if (accepted.length === 0) return failure('automatic_http_not_observed_within_window');
  const selected = accepted.sort((a, z) => a.frame.finished_elapsed_ms - z.frame.finished_elapsed_ms)[0];
  const targetDOM = sample => {
    const rebound = bindLibraryChangedDOM(sample.observation, { phase: window.name, physical: window.http.physical, frames: window.http.frames,
      token_sha256: b.token_sha256, boundary: b, events: window.events, discovery });
    return rebound.passed && sample.observation.passed === true && sample.observation.identity_proven === true &&
      sample.observation.identity_mode === rebound.identity_mode && sample.observation.target_id === rebound.target_id &&
      same(sample.observation.wire_identity, rebound.wire_identity);
  };
  const after = window.dom.filter(sample => sample.started_elapsed_ms >= selected.frame.finished_elapsed_ms && targetDOM(sample));
  const firstPair = after.find((sample, index) => index + 1 < after.length &&
    after[index + 1].started_elapsed_ms - sample.elapsed_ms >= CHANGED_LIMITS.sample_ms - 100 &&
    window.dom.indexOf(after[index + 1]) === window.dom.indexOf(sample) + 1);
  const last = window.dom.at(-1);
  if (!firstPair || !last || !targetDOM(last) || last.elapsed_ms < b.end_elapsed_ms - CHANGED_LIMITS.sample_ms - 100) {
    return failure('dom_update_not_observed_within_window');
  }
  return { result: 'passed', outcome: 'automatic_http_and_visible_title_observed', proof: {
    message_id: received.message.MessageId, physical_exchange_id: selected.physical.id, frame_request_index: selected.frame.index,
    first_dom_sequence: firstPair.sequence, last_dom_sequence: last.sequence, identity_bound: true, ordered: true } };
}

async function privateDirectory(filename) {
  const info = await fs.lstat(filename);
  need(info.isDirectory() && !info.isSymbolicLink() && info.uid === 0 && info.gid === 0 && (info.mode & 0o777) === 0o700);
  return { dev: info.dev, ino: info.ino };
}
const statValue = value => value && ({ file: value.isFile(), symbolic: value.isSymbolicLink(), uid: value.uid, gid: value.gid,
  mode: value.mode & 0o777, nlink: value.nlink, size: value.size, dev: value.dev, ino: value.ino, mtimeMs: value.mtimeMs, ctimeMs: value.ctimeMs });
async function optionalStat(filename) { try { return await fs.lstat(filename); } catch (error) { if (error.code === 'ENOENT') return null; throw error; } }

export function libraryChangedPublication(final, pending) {
  const owned = value => value && value.file && !value.symbolic && value.uid === 0 && value.gid === 0 && value.mode === 0o600 &&
    value.size >= 0 && value.size <= CHANGED_LIMITS.record_bytes;
  if (!final) { need(!pending || owned(pending) && pending.nlink === 1); return pending ? 'publishing' : 'missing'; }
  need(owned(final) && final.size > 0);
  if (final.nlink === 1 && pending === null) return 'ready';
  need(final.nlink === 2 && owned(pending) && pending.nlink === 2 && final.dev === pending.dev && final.ino === pending.ino && final.size === pending.size);
  return 'publishing';
}

export function encodeLibraryChangedRecord(value) { return Buffer.from(JSON.stringify(value) + '\n'); }
async function syncDirectory(filename, io) {
  const handle = await io.open(filename, constants.O_RDONLY | constants.O_DIRECTORY | constants.O_NOFOLLOW);
  try { await handle.sync(); } finally { await handle.close(); }
}

export async function publishLibraryChangedRecord(filename, value, io = fs) {
  need(path.posix.dirname(filename) === OUTPUT && /^[a-z][a-z0-9-]*\.json$/.test(path.posix.basename(filename)));
  const maximum = path.posix.basename(filename) === 'report.json' ? CHANGED_LIMITS.report_bytes : CHANGED_LIMITS.record_bytes;
  const bytes = encodeLibraryChangedRecord(value);
  return publishLibraryChangedBytes(filename, bytes, maximum, io);
}

export async function publishLibraryChangedScreenshot(bytes, io = fs) {
  need(Buffer.isBuffer(bytes) && bytes.length >= 8 && bytes.subarray(0, 8).equals(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10])));
  const size = bytes.length;
  const source = await publishLibraryChangedBytes(OUTPUT + '/discovery-failure.png', bytes, CHANGED_DIAGNOSTIC_LIMITS.screenshot_bytes, io);
  return { ...source, bytes: size };
}

async function publishLibraryChangedBytes(filename, bytes, maximum, io) {
  try {
    need(bytes.length > 0 && bytes.length <= maximum);
    const pending = filename + '.pending';
    const handle = await io.open(pending, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
    let identity;
    try { await handle.writeFile(bytes); await handle.sync(); identity = await handle.stat(); } finally { await handle.close(); }
    await io.link(pending, filename); await syncDirectory(OUTPUT, io);
    const [temporary, final] = await Promise.all([io.lstat(pending), io.lstat(filename)]);
    need([temporary, final].every(info => info.dev === identity.dev && info.ino === identity.ino && info.nlink === 2 && info.isFile() && !info.isSymbolicLink()));
    await io.unlink(pending); await syncDirectory(OUTPUT, io);
    const published = await io.lstat(filename);
    need(published.dev === identity.dev && published.ino === identity.ino && published.nlink === 1 && published.isFile() && !published.isSymbolicLink());
    return { path: filename, sha256: sha(bytes) };
  } finally { bytes.fill(0); }
}

async function readLibraryChangedControl(name) {
  need(CONTROLS.includes(name) || name === 'abort'); await privateDirectory(ROOT);
  const filename = name === 'abort' ? ROOT + '/abort.json' : ROOT + '/control-' + name + '.json';
  const first = await optionalStat(filename), temporary = await optionalStat(filename + '.pending'), last = await optionalStat(filename);
  const publication = same(statValue(first), statValue(last)) ? libraryChangedPublication(statValue(last), statValue(temporary)) : 'publishing';
  if (publication !== 'ready') return { publication };
  const handle = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW); let bytes;
  try {
    need(same(statValue(await handle.stat()), statValue(first))); bytes = await handle.readFile();
    need(bytes.length === first.size && same(statValue(await handle.stat()), statValue(first)) && same(statValue(await fs.lstat(filename)), statValue(first)));
    return { publication: 'ready', value: parseLibraryChangedJSON(bytes, CHANGED_LIMITS.record_bytes), path: filename, sha256: sha(bytes) };
  } finally { bytes?.fill(0); await handle.close(); }
}

async function libraryChangedProcessIdentity(input) {
  need(process.platform === 'linux' && process.getuid() === 0 && process.getgid() === 0 && !process.env.DEBUG && !process.env.PWDEBUG);
  const boot = (await fs.readFile('/proc/sys/kernel/random/boot_id', 'utf8')).trim();
  need(boot === input.controller.boot_id && procStartTicks(await fs.readFile(`/proc/${input.controller.pid}/stat`, 'utf8')) === input.controller.start_ticks);
  const group = '/system.slice/' + CHANGED_UNIT, controllerGroup = '/system.slice/' + CHANGED_CONTROLLER_UNIT;
  const groups = (await fs.readFile('/proc/self/cgroup', 'utf8')).trim().split('\n');
  const parentGroups = (await fs.readFile(`/proc/${input.controller.pid}/cgroup`, 'utf8')).trim().split('\n');
  need(groups.some(line => line.split(':')[2] === group) && parentGroups.some(line => line.split(':')[2] === controllerGroup));
  const executable = await fs.readlink('/proc/self/exe'); need(path.posix.isAbsolute(executable) && !executable.endsWith(' (deleted)'));
  const handle = await fs.open('/proc/self/exe', constants.O_RDONLY); let executableSHA;
  try {
    const info = await handle.stat(); need(info.isFile() && info.size > 0 && info.size <= 256 * 1024 * 1024 && info.uid === 0 && (info.mode & 0o022) === 0);
    executableSHA = sha(await handle.readFile());
  } finally { await handle.close(); }
  return { pid: process.pid, start_ticks: procStartTicks(await fs.readFile('/proc/self/stat', 'utf8')), boot_id: boot,
    uid: 0, gid: 0, executable_path: executable, executable_sha256: executableSHA, cgroup: group };
}

function headersObject(raw) {
  if (record(raw)) return raw;
  need(Array.isArray(raw) && raw.length <= 128 && raw.length % 2 === 0);
  const result = {};
  for (let index = 0; index < raw.length; index += 2) {
    need(typeof raw[index] === 'string' && typeof raw[index + 1] === 'string');
    const name = raw[index].toLowerCase(); need(!Object.hasOwn(result, name)); result[name] = raw[index + 1];
  }
  return result;
}

/** A single monotonic event recorder surrounds only public network and visible DOM observations. */
export class LibraryChangedObserver {
  constructor({ input, login, now = () => Date.now(), monotonic = () => Number(process.hrtime.bigint()) / 1000000 }) {
    Object.assign(this, { input, login, now, monotonic });
    this.sequence = 0; this.origin = monotonic(); this.started = now(); this.phase = 'initial';
    this.physical = []; this.frames = []; this.wireMessages = []; this.browserMessages = []; this.lifecycle = []; this.actions = [];
    this.responseDiagnostics = [];
    this.frameMap = new WeakMap(); this.workerMap = new WeakMap(); this.workers = 0;
    this.documentEpoch = 0; this.route = null; this.failure = null; this.disposed = false;
    this.discovery = null; this.window = null; this.actor = null;
  }
  setActor(actor) { this.actor = actor; this.started = actor.started; this.origin = this.monotonic() - (this.now() - actor.started); }
  stamp() {
    need(!this.disposed, 'library_changed_observer_disposed');
    const elapsed = this.monotonic() - this.origin;
    need(elapsed >= 0 && elapsed <= CHANGED_LIMITS.work_ms + CHANGED_LIMITS.abort_wait_ms + CHANGED_LIMITS.cleanup_ms &&
      Math.abs(this.now() - this.started - elapsed) <= 1000, 'library_changed_clock_changed');
    return { sequence: ++this.sequence, elapsed_ms: elapsed };
  }
  elapsed() { return this.monotonic() - this.origin; }
  get documentID() {
    const value = this.actor?.libraryChangedDocumentID;
    if (value !== undefined) { need(safeText(value, 80)); return value; }
    return 'document-' + this.documentEpoch;
  }
  guard(name, ...args) {
    try {
      const result = this[name](...args);
      if (result?.then) return result.catch(error => { this.failure ??= 'library_changed_observer_failed'; throw error; });
      return result;
    } catch (error) { this.failure ??= 'library_changed_observer_failed'; throw error; }
  }
  hooks() {
    const hooks = this.login.hooks();
    for (const name of ['frameRequest', 'frameResponse', 'frameFinished']) {
      const original = hooks[name];
      hooks[name] = value => {
        const own = this.guard(name, value), login = original(value);
        return Promise.all([own, login]);
      };
    }
    for (const name of ['catalogRequest', 'catalogResponse', 'catalogFinished', 'physicalLibraryChanged', 'browserLibraryChanged']) {
      hooks[name] = (...args) => this.guard(name, ...args);
    }
    return hooks;
  }
  boundToken() { need(this.login.bound && SHA.test(this.login.bound.token_sha256), 'library_changed_login_not_bound'); return this.login.bound.token_sha256; }
  scope(raw, method) {
    const selected = libraryChangedCatalogRequest(raw, method, this.input);
    if (selected && this.discovery && selected.kind !== 'target') {
      const allowed = selected.kind === 'collection-folder'
        ? this.discovery.collection_folder_reads.map(pair => pair.physical) : this.discovery.query_allowlist;
      need(allowed.some(item => item.kind === selected.kind && item.shape_sha256 === selected.shape_sha256), 'library_changed_query_scope_changed');
    }
    return selected;
  }
  catalogRequest(value, bytes) {
    need(record(value) && !this.physical.some(item => item.id === value.id) && this.physical.length < CHANGED_LIMITS.catalog_total &&
      (bytes === undefined || Buffer.isBuffer(bytes) && bytes.length === 0));
    const selected = this.scope(value.url, value.method); need(selected, 'library_changed_catalog_scope_rejected');
    const authority = authorityForURL(new URL(value.url), headersObject(value.headers)); need(authority);
    const token = authority.fingerprint; authority.token = null;
    if (this.login.bound) need(token === this.boundToken(), 'library_changed_token_changed');
    const stamp = this.stamp();
    const entry = { id: value.id, ...selected, phase: this.phase, method: value.method, token_sha256: token,
      request_elapsed_ms: stamp.elapsed_ms, request_sequence: stamp.sequence, status: null, completed: false, terminal: null };
    need(Number.isSafeInteger(entry.id) && entry.id >= 0);
    this.physical.push(entry);
    if (this.window) need(this.physical.filter(item => item.request_sequence > this.window.started_sequence).length <= CHANGED_LIMITS.catalog_per_window,
      'library_changed_catalog_limit');
  }
  catalogResponse(value, bytes) {
    const entry = this.physical.find(item => item.id === value.id); need(entry && entry.status === null);
    const stamp = this.stamp(); entry.status = value.status; entry.response_elapsed_ms = stamp.elapsed_ms;
    if (value.status === 200) {
      entry.projection = projectLibraryChangedItems(bytes, entry.kind);
      try {
        const diagnostic = libraryChangedResponseDiagnostic(bytes, entry.kind, this.actor?.diagnosticSecrets?.() ?? []);
        if (diagnostic && this.responseDiagnostics.length < CHANGED_LIMITS.catalog_total)
          this.responseDiagnostics.push({ physical_exchange_id: entry.id, ...diagnostic });
      } catch {
        if (this.responseDiagnostics.length < CHANGED_LIMITS.catalog_total)
          this.responseDiagnostics.push({ physical_exchange_id: entry.id, reason: 'response_diagnostic_unavailable' });
      }
    }
  }
  catalogFinished(value) {
    const entry = this.physical.find(item => item.id === value.id); need(entry && !entry.terminal);
    const stamp = this.stamp(); Object.assign(entry, { completed: value.outcome === 'completed', terminal: value.outcome,
      finished_elapsed_ms: stamp.elapsed_ms, terminal_status: value.status, response_bytes: value.response_bytes });
  }
  frameRequest(value) {
    const request = value.request, selected = this.scope(request.url(), request.method());
    if (!selected) return;
    need(this.frames.length < CHANGED_LIMITS.catalog_total * 2 && !this.frameMap.has(request));
    const stamp = this.stamp(), worker = request.serviceWorker(); let workerID = null;
    if (worker !== null) {
      if (!this.workerMap.has(worker)) { need(++this.workers <= 4); this.workerMap.set(worker, 'worker-' + this.workers); }
      workerID = this.workerMap.get(worker);
    }
    if (worker === null) need(this.actor?.page && request.frame() === this.actor.page.mainFrame(), 'library_changed_foreign_frame');
    const { query: _query, ...frameSelection } = selected;
    const entry = { index: value.index, ...frameSelection, phase: this.phase, source: worker ? 'worker' : 'page', worker_id: workerID,
      main_frame: worker === null,
      request_elapsed_ms: stamp.elapsed_ms, request_sequence: stamp.sequence, document_id: this.documentID,
      page_route: this.actor?.page ? libraryChangedRoute(this.actor.page.url()) : null,
      status: null, finished: false, failed: false, from_service_worker: null, token_sha256: null };
    need(Number.isSafeInteger(entry.index) && entry.index >= 0);
    this.frames.push(entry); this.frameMap.set(request, entry);
    return (async () => {
      const headers = await bounded(request.allHeaders(), 1500);
      await waitHomeLoginProof(this.login);
      const expectedToken = this.actor?.token ?? this.login.owned?.token;
      need(typeof expectedToken === 'string');
      const authority = authorityForURL(new URL(request.url()), headers, expectedToken); need(authority);
      entry.token_sha256 = authority.fingerprint; authority.token = null;
      need(entry.token_sha256 === this.boundToken(), 'library_changed_token_changed');
    })();
  }
  frameResponse(value) {
    const entry = this.frameMap.get(value.response.request()); if (!entry) return;
    const stamp = this.stamp(); entry.status = value.response.status(); entry.from_service_worker = value.response.fromServiceWorker();
    const mime = (value.response.headers()['content-type'] ?? '').split(';', 1)[0].trim().toLowerCase();
    entry.content_type = mime === 'application/json' ? mime : 'other'; entry.response_elapsed_ms = stamp.elapsed_ms;
  }
  frameFinished(value) {
    const entry = this.frameMap.get(value.request); if (!entry) return;
    const stamp = this.stamp(); entry.finished = !value.failed; entry.failed = Boolean(value.failed); entry.finished_elapsed_ms = stamp.elapsed_ms;
  }
  message(value, bytes, browser) {
    need(record(value) && value.complete === true && value.received === true && safeText(String(value.connection_id), 80) &&
      value.token_sha256 === this.boundToken(), 'library_changed_message_not_complete_or_bound');
    const message = projectLibraryChangedMessage(bytes);
    if (value.message_id !== undefined) need(value.message_id === message.MessageId);
    const collection = browser ? this.browserMessages : this.wireMessages;
    const prior = collection.find(entry => entry.connection_id === String(value.connection_id) && entry.message.MessageId === message.MessageId);
    const stamp = this.stamp();
    if (prior) {
      need(!browser && !prior.forwarded && value.forwarded === true && same(prior.message, message), 'library_changed_duplicate_message');
      prior.forwarded = true; prior.forwarded_elapsed_ms = stamp.elapsed_ms; return;
    }
    need(collection.length < CHANGED_LIMITS.events, 'library_changed_message_limit');
    const entry = { ...stamp, connection_id: String(value.connection_id), token_sha256: value.token_sha256,
      complete: true, received: true, message, forwarded: browser ? true : value.forwarded === true };
    if (browser) {
      const route = this.actor?.page ? libraryChangedRoute(this.actor.page.url()) : this.route;
      need(value.document_id === undefined || value.document_id === this.documentID);
      if (value.route !== undefined) need(libraryChangedRoute(value.route.startsWith('http') ? value.route : ORIGIN + value.route) === route);
      Object.assign(entry, { document_id: this.documentID, route });
    } else if (value.received_elapsed_ms !== undefined) {
      need(Number.isFinite(value.received_elapsed_ms) && value.received_elapsed_ms >= 0 && value.received_elapsed_ms <= stamp.elapsed_ms + 1000 &&
        Number.isFinite(value.forwarded_elapsed_ms) && value.forwarded_elapsed_ms >= value.received_elapsed_ms &&
        value.forwarded_elapsed_ms <= stamp.elapsed_ms + 1000);
      Object.assign(entry, { elapsed_ms: value.received_elapsed_ms, forwarded_elapsed_ms: value.forwarded_elapsed_ms });
    }
    collection.push(entry);
  }
  physicalLibraryChanged(value, bytes) { return this.message(value, bytes, false); }
  browserLibraryChanged(value, bytes) { return this.message(value, bytes, true); }
  noteLifecycle(kind) {
    const stamp = this.stamp(); need(this.lifecycle.length < CHANGED_LIMITS.lifecycle);
    if (kind === 'document' || kind === 'route') this.documentEpoch++;
    this.route = this.actor?.page ? libraryChangedRoute(this.actor.page.url()) : null;
    this.lifecycle.push({ ...stamp, kind, document_id: this.documentID, route: this.route });
    if (this.window) this.failure = 'library_changed_document_or_connection_changed';
  }
  attachPage() {
    need(this.actor?.page && !this.pageAttached); this.pageAttached = true; this.documentEpoch = 1;
    this.route = libraryChangedRoute(this.actor.page.url());
    this.actor.page.on('framenavigated', frame => {
      if (frame === this.actor.page.mainFrame()) { try { this.noteLifecycle('route'); } catch { this.failure = 'library_changed_lifecycle_failed'; } }
    });
    this.actor.page.on('domcontentloaded', () => { try { this.noteLifecycle('document'); } catch { this.failure = 'library_changed_lifecycle_failed'; } });
    for (const name of ['close', 'popup', 'download', 'websocket']) this.actor.page.on(name, () => {
      try { this.noteLifecycle(name === 'websocket' ? 'handshake' : name); } catch { this.failure = 'library_changed_lifecycle_failed'; }
    });
  }
  noteAction(kind) {
    need(!this.window, 'library_changed_action_while_armed');
    need(this.actions.length < 8); this.actions.push({ ...this.stamp(), kind });
  }
  socket() {
    const websocket = this.actor?.report.websocket; need(websocket && websocket.active === 1 && websocket.failed === 0 &&
      websocket.seen >= 1 && websocket.seen <= 2 && websocket.opened >= 1 && websocket.opened <= 2);
    const live = websocket.entries.filter(entry => entry.handshake_delivered && !entry.closed);
    need(live.length === 1 && live[0].upstream_status === 101 && live[0].token_fingerprint === this.boundToken() &&
      live[0].browser_closed !== true && !live[0].browser_socket_error &&
      live[0].browser_handshake_observed === true && Number.isSafeInteger(live[0].browser_observation_id) &&
      live[0].browser_observation_id === live[0].index && live[0].index >= 0 && live[0].index < 2 &&
      live[0].connection_id !== undefined && safeText(String(live[0].connection_id), 80), 'library_changed_socket_not_bound');
    return { connection_id: String(live[0].connection_id), token_sha256: this.boundToken(), seen: websocket.seen, opened: websocket.opened };
  }
  assertStable() {
    need(!this.failure && !this.disposed, this.failure ?? 'library_changed_observer_disposed');
    need(this.actor && this.actor.report.page_error_count === 0 && this.actor.report.network.observer_errors === 0 &&
      this.actor.report.network.guard_errors === 0 && this.actor.report.network.overflow === 0 && this.actor.report.network.playback_attempts === 0 &&
      this.actor.report.network.forbidden_mutations === 0, 'library_changed_browser_guard_failed');
    need(this.actor.token && sha(this.actor.token) === this.boundToken() && this.actor.report.proxy.login === 1, 'library_changed_token_changed');
    need(this.physical.every(entry => entry.token_sha256 === this.boundToken()) &&
      this.frames.every(entry => entry.token_sha256 === null || entry.token_sha256 === this.boundToken()), 'library_changed_token_changed');
    if (this.window) {
      const socket = this.socket(), route = libraryChangedRoute(this.actor.page.url());
      need(route === this.window.route && this.documentID === this.window.document_id && socket.connection_id === this.window.connection_id &&
        socket.token_sha256 === this.window.token_sha256 && socket.seen === this.window.websocket_seen && socket.opened === this.window.websocket_opened,
      'library_changed_document_or_connection_changed');
    }
  }
  startWindow(name) {
    need(['forward', 'restored'].includes(name)); this.assertStable(); const socket = this.socket();
    const boundary = { name, started_elapsed_ms: this.elapsed(), started_sequence: this.sequence,
      route: libraryChangedRoute(this.actor.page.url()), document_id: this.documentID, connection_id: socket.connection_id,
      token_sha256: socket.token_sha256, websocket_seen: socket.seen, websocket_opened: socket.opened };
    this.window = boundary; this.phase = name; return clone(boundary);
  }
  reads(fromSequence = 0) {
    return pairLibraryChangedReads(this.physical.filter(entry => entry.request_sequence > fromSequence),
      this.frames.filter(entry => entry.request_sequence > fromSequence));
  }
  windowData(boundary, control, samples) {
    const end = instant(control.value.commit.write_completed_at) - this.started + CHANGED_LIMITS.window_ms;
    const between = entry => entry.sequence > boundary.started_sequence && entry.elapsed_ms >= boundary.started_elapsed_ms && entry.elapsed_ms <= end;
    const started = entry => entry.request_sequence > boundary.started_sequence && entry.request_elapsed_ms >= boundary.started_elapsed_ms && entry.request_elapsed_ms <= end;
    const physical = this.physical.filter(started), frames = this.frames.filter(started);
    return { name: boundary.name, control_sha256: control.sha256, commit: clone(control.value.commit),
      boundary: { ...clone(boundary), response_completed_elapsed_ms: end - CHANGED_LIMITS.window_ms, end_elapsed_ms: end,
        completed_elapsed_ms: this.elapsed(), duration_ms: CHANGED_LIMITS.window_ms },
      events: { physical: clone(this.wireMessages.filter(between)), browser: clone(this.browserMessages.filter(between)) },
      http: { physical: clone(physical), frames: clone(frames), pairs: pairLibraryChangedReadReferences(physical, frames) },
      dom: clone(samples.filter(between)), actions: clone(this.actions.filter(between)), lifecycle: clone(this.lifecycle.filter(between)) };
  }
  dispose() { this.disposed = true; this.login.dispose(); }
}

/** Inspect rendered identities and exact visible titles, without application internals. */
export async function observeLibraryChangedDOM(page, target, expectedName, forbiddenName, documentID) {
  const route = libraryChangedRoute(page.url()), containers = page.locator('.itemsContainer');
  need(await containers.count() <= 32, 'library_changed_dom_limit');
  const observed = await containers.evaluateAll((elements, values) => {
    const insideClips = (box, ancestor) => {
      let left = Math.max(0, box.left), right = Math.min(innerWidth, box.right), top = Math.max(0, box.top), bottom = Math.min(innerHeight, box.bottom), depth = 0;
      if (box.width <= 0 || box.height <= 0 || right <= left || bottom <= top) return false;
      while (ancestor) {
        if (++depth > 64) return false;
        const style = getComputedStyle(ancestor);
        if (style.display !== 'contents') {
          const clips = value => ['hidden', 'clip', 'scroll', 'auto'].includes(value);
          const x = clips(style.overflowX ?? style.overflow), y = clips(style.overflowY ?? style.overflow);
          if (x || y) {
            const bounds = ancestor.getBoundingClientRect();
            if (x) { if (bounds.width <= 0) return false; left = Math.max(left, bounds.left); right = Math.min(right, bounds.right); }
            if (y) { if (bounds.height <= 0) return false; top = Math.max(top, bounds.top); bottom = Math.min(bottom, bounds.bottom); }
            if (right <= left || bottom <= top) return false;
          }
        }
        ancestor = ancestor.parentElement;
      }
      return true;
    };
    const visible = element => { const box = element.getBoundingClientRect(), style = getComputedStyle(element);
      return style.display !== 'none' && style.visibility !== 'hidden' && insideClips(box, element.parentElement); };
    const pool = new Set(elements), allCards = document.querySelectorAll('.card'), allButtons = document.querySelectorAll('button.cardTextActionButton');
    let overflow = allCards.length > 64 || allButtons.length > 64;
    const cards = allCards.length <= 64 ? [...allCards].filter(visible) : [];
    const buttons = allButtons.length <= 64 ? [...allButtons].filter(visible) : [];
    const orphan = [...cards, ...buttons].some(value => !pool.has(value.closest('.itemsContainer')));
    const owners = elements.map((container, index) => {
      return { container, index, cards: cards.filter(value => value.closest('.itemsContainer') === container),
        buttons: buttons.filter(value => value.closest('.itemsContainer') === container), visible: visible(container) };
    });
    const cardOwners = owners.filter(value => value.cards.length > 0), card = cards.length === 1 ? cards[0] : null;
    const container = card ? card.closest('.itemsContainer') : null;
    overflow ||= cards.length > 64 || buttons.length > 64;
    const title = button => {
      const texts = [], walker = document.createTreeWalker(button, NodeFilter.SHOW_TEXT); let node, count = 0;
      while ((node = walker.nextNode())) {
        if (++count > 64 || typeof node.nodeValue !== 'string' || node.nodeValue.length > 4096) { overflow = true; return null; }
        if (!node.nodeValue.trim() || !visible(node.parentElement)) continue;
        const range = document.createRange(); range.selectNodeContents(node); const box = range.getBoundingClientRect(); range.detach();
        if (insideClips(box, node.parentElement)) texts.push(node.nodeValue);
      }
      const text = texts.join(' ').replace(/\s+/g, ' ').trim(); if (text.length > 256) { overflow = true; return null; } return text;
    };
    const titles = buttons.slice(0, 64).map(title), buttonTitles = new Map(buttons.slice(0, 64).map((value, index) => [value, titles[index]]));
    const button = buttons.length === 1 ? buttons[0] : null;
    const box = button?.closest('.cardBox'), text = button?.closest('.cardText');
    const consistent = Boolean(!orphan && button && card && container && button.closest('.card') === card && button.closest('.itemsContainer') === container &&
      card.tagName.toLowerCase() === 'div' && box?.closest('.card') === card &&
      text?.closest('.cardBox') === box && [button, text, box, card].every(value => {
        const id = value.getAttribute('data-id'), type = value.getAttribute('data-type');
        return (id === null || id === values.target) && (type === null || type === 'Movie');
      }) && (container.getAttribute('data-id') === null || container.getAttribute('data-id') === values.library));
    const number = value => Number.isFinite(value) ? Math.max(-100000, Math.min(100000, Math.round(value * 10) / 10)) : null;
    const containerObservations = owners.map(value => { const bounds = value.container.getBoundingClientRect();
      return { index: value.index, visible: value.visible, tag: value.container.tagName.toLowerCase() === 'div' ? 'div' : 'other',
        classes: ['itemsContainer'], rect: { x: number(bounds.x), y: number(bounds.y), width: number(bounds.width), height: number(bounds.height) },
        visible_cards: value.cards.length, visible_title_buttons: value.buttons.length,
        target_title_count: value.buttons.filter(button => buttonTitles.get(button) === values.expected).length,
        forbidden_title_count: values.forbidden === null ? 0 : value.buttons.filter(button => buttonTitles.get(button) === values.forbidden).length }; });
    return { containers: owners.filter(value => value.visible).length, card_containers: cardOwners.length,
      cards: cards.length, buttons: buttons.length, consistent, overflow, container_observations: containerObservations,
      observed_title: titles.length === 1 ? titles[0] : null,
      target_titles: titles.filter(value => value === values.expected).length,
      forbidden_titles: values.forbidden === null ? 0 : titles.filter(value => value === values.forbidden).length };
  }, { target: ITEM, library: LIBRARY, expected: expectedName, forbidden: forbiddenName });
  need(!observed.overflow, 'library_changed_dom_limit');
  const active = await page.locator('audio,video').evaluateAll(elements => elements.some(element =>
    !element.paused && !element.ended || element.currentTime > 0));
  return { route, document_id: documentID, started_elapsed_ms: null, target_id: null, expected_name: expectedName,
    visible_items_containers: observed.containers, visible_card_containers: observed.card_containers,
    visible_cards: observed.cards, visible_title_buttons: observed.buttons, container_observations: observed.container_observations,
    visible_target_cards: observed.cards, target_title_count: observed.target_titles, forbidden_title_count: observed.forbidden_titles,
    explicit_identity_consistent: observed.consistent, observed_title: observed.observed_title,
    identity_mode: 'unbound', wire_identity: null, identity_proven: false, media_inactive: !active, passed: false,
    selector: { identity_attribute: 'composite-wire-card', title_mode: 'exact-visible-title-button', card_tag: observed.cards === 1 ? 'div' : null,
      card_class: observed.cards === 1 ? 'card' : null, context: 'current-visible-dom' } };
}
/** Keep bounded public structure for diagnosis without changing the acceptance matcher. */
export async function observeLibraryChangedCandidates(page, secrets = []) {
  const variants = libraryChangedSecretVariants(secrets);
  const locator = page.locator(`[data-id="${ITEM}"]`);
  const total = await locator.count();
  need(Number.isSafeInteger(total) && total >= 0 && total <= 256, 'library_changed_diagnostic_dom_limit');
  const value = await locator.evaluateAll((elements, limits) => {
    const entries = [], seen = new Map();
    const describe = (element, relation, owner) => {
      if (!element || entries.length >= limits.structure_nodes) return null;
      if (seen.has(element)) return seen.get(element);
      const box = element.getBoundingClientRect(), style = getComputedStyle(element), text = element.innerText;
      const visible = box.width > 0 && box.height > 0 && box.bottom > 0 && box.right > 0 && box.top < innerHeight && box.left < innerWidth &&
        style.display !== 'none' && style.visibility !== 'hidden';
      const coordinate = value => Number.isFinite(value) ? Math.max(-100000, Math.min(100000, Math.round(value * 10) / 10)) : null;
      const id = element.getAttribute('data-id'), type = element.getAttribute('data-type');
      const tag = element.tagName.toLowerCase();
      const entry = { index: entries.length, relation, owner, tag: /^(?:div|span|a|button|img|h[1-6]|p|section|li|ul|article)$/.test(tag) ? tag : 'other',
        classes: [...element.classList].filter(value => /^(?:card|item|emby|button|text|section|list)[A-Za-z0-9_-]{0,58}$/.test(value)).slice(0, 12),
        data_id: [limits.target_id, limits.library_id].includes(id) ? id : null,
        data_type: ['Movie', 'CollectionFolder', 'Folder'].includes(type) ? type : null,
        data_type_present: type !== null, type_matches_expected: type === 'Movie',
        type_casefold_matches_movie: typeof type === 'string' && type.toLowerCase() === 'movie',
        rect: { x: coordinate(box.x), y: coordinate(box.y), width: coordinate(box.width), height: coordinate(box.height) },
        visible, text: visible && typeof text === 'string' && text.length <= limits.text_chars ? text : null,
        text_length: typeof text === 'string' ? Math.min(text.length, 1000000) : 0,
        leading_whitespace: typeof text === 'string' && /^\s/.test(text), trailing_whitespace: typeof text === 'string' && /\s$/.test(text) };
      seen.set(element, entry.index); entries.push(entry); return entry.index;
    };
    const targets = [];
    for (const element of elements.slice(0, limits.target_nodes)) {
      const index = describe(element, 'target', null); targets.push(index);
      entries[index].parent_index = describe(element.parentElement, 'parent', index);
      entries[index].grandparent_index = describe(element.parentElement?.parentElement, 'grandparent', index);
      entries[index].card_index = describe(element.closest('.card, .cardBox'), 'card', index);
    }
    return { targets, entries, truncated: elements.length > limits.target_nodes };
  }, { ...CHANGED_DIAGNOSTIC_LIMITS, target_id: ITEM, library_id: LIBRARY });
  need(record(value) && Array.isArray(value.entries) && value.entries.length <= CHANGED_DIAGNOSTIC_LIMITS.structure_nodes &&
    Array.isArray(value.targets) && value.targets.length <= CHANGED_DIAGNOSTIC_LIMITS.target_nodes);
  for (const entry of value.entries) {
    entry.text = entry.text === null ? '[text omitted]' : sanitizeLibraryChangedText(entry.text, secrets, variants);
    entry.classes = entry.classes.map(value => sanitizeLibraryChangedText(value, secrets, variants));
  }
  return { target_id: ITEM, total_target_nodes: total, ...value };
}

export function libraryChangedSecretVariants(secrets) {
  need(Array.isArray(secrets) && secrets.length <= 128);
  const variants = new Set();
  for (const secret of secrets) {
    if (secret === null || secret === undefined || secret === '') continue;
    need(typeof secret === 'string' && secret.length <= 4096);
    const bytes = Buffer.from(secret), encoded = encodeURIComponent(secret);
    const percent = [...bytes].map(value => '%' + value.toString(16).padStart(2, '0')).join('');
    for (const value of [secret, encoded, encodeURIComponent(encoded), percent, encodeURIComponent(percent),
      JSON.stringify(secret).slice(1, -1), bytes.toString('base64'), bytes.toString('base64').replace(/=+$/, ''),
      bytes.toString('base64url'), bytes.toString('hex'), [...bytes].map(value => '\\x' + value.toString(16).padStart(2, '0')).join(''),
      [...secret].map(value => '\\u' + value.charCodeAt(0).toString(16).padStart(4, '0')).join('')]) {
      variants.add(value); variants.add(JSON.stringify(value).slice(1, -1));
    }
  }
  need([...variants].reduce((total, value) => total + value.length, 0) <= 1024 * 1024);
  return [...variants].sort((left, right) => right.length - left.length);
}

function knownDiagnosticSecret(text, variants) {
  const lower = text.toLowerCase(); return variants.some(value => value && lower.includes(value.toLowerCase()));
}

function sanitizeLibraryChangedText(text, secrets, variants) {
  for (const value of variants) text = text.replace(new RegExp(value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'gi'), '[secret]');
  return sanitizeBrowserMessage(text, secrets);
}

/** Read public text ranges and identity-carrier categories; never inspect form values. */
export function collectLibraryChangedPublicDOM(bodies, limits) {
  if (bodies.length !== 1) return { fragments: [], structures: [], target_name_matches: [], target_id_carriers: [],
    flags: { viewport_valid: false, elements_truncated: true, text_truncated: true, password_visible: false, iframe_count: 0, visible_iframe_count: 0 } };
  const body = bodies[0], elements = [], elementWalker = document.createTreeWalker(body, NodeFilter.SHOW_ELEMENT);
  let nextElement;
  while (elements.length < limits.viewport_elements && (nextElement = elementWalker.nextNode())) elements.push(nextElement);
  const elementsTruncated = elementWalker.nextNode() !== null;
  const indexes = new Map(elements.map((element, index) => [element, index]));
  const boxVisible = box => box.width > 0 && box.height > 0 && box.bottom > 0 && box.right > 0 && box.top < innerHeight && box.left < innerWidth;
  const visible = element => { const style = getComputedStyle(element); return boxVisible(element.getBoundingClientRect()) && style.display !== 'none' && style.visibility !== 'hidden'; };
  const fragments = [], groups = new Map(), carriers = [], structures = new Map();
  let textCount = 0, chars = 0, textTruncated = false, passwordVisible = false, iframeCount = 0, visibleIframes = 0;
  const walker = document.createTreeWalker(body, NodeFilter.SHOW_TEXT); let textNode;
  while ((textNode = walker.nextNode())) {
    if (++textCount > limits.viewport_text_nodes) { textTruncated = true; break; }
    const owner = textNode.parentElement;
    if (!owner || owner !== body && !indexes.has(owner) || owner.closest('input, textarea, [contenteditable], [data-userid], [data-user-id], script, style, noscript, template') || !visible(owner)) continue;
    const text = textNode.nodeValue; if (typeof text !== 'string' || !text.trim()) continue;
    const range = document.createRange(); range.selectNodeContents(textNode); const inViewport = boxVisible(range.getBoundingClientRect()); range.detach();
    if (!inViewport) continue;
    if (text.length > limits.viewport_chars || chars + text.length + 1 > limits.viewport_chars) { textTruncated = true; continue; }
    const index = indexes.get(owner) ?? null; chars += text.length + 1;
    fragments.push({ element_index: index, leaf_owner: owner !== body && owner.childElementCount === 0, text });
    if (index !== null) { const values = groups.get(index) ?? []; values.push(text); groups.set(index, values); }
  }
  const keyClass = key => ['id', 'itemid', 'item-id', 'parentid', 'parent-id'].includes(key.toLowerCase()) ? key.toLowerCase() : 'other';
  for (const element of elements) {
    const tag = element.tagName.toLowerCase(), index = indexes.get(element);
    if (tag === 'input' && element.getAttribute('type')?.toLowerCase() === 'password' && visible(element)) passwordVisible = true;
    if (tag === 'iframe') { iframeCount++; if (visible(element)) visibleIframes++; }
    if (carriers.length >= 64) continue;
    for (let at = 0; at < Math.min(element.attributes.length, 64); at++) { const attribute = element.attributes[at];
      if (!attribute.name.startsWith('data-') || attribute.value !== limits.target_id) continue;
      const name = attribute.name.toLowerCase();
      carriers.push({ element_index: index, kind: 'data-attribute', key: ['data-id', 'data-itemid', 'data-item-id', 'data-parentid', 'data-parent-id'].includes(name) ? name : 'data-other',
        key_name: /^data-[A-Za-z][A-Za-z0-9-]{0,58}$/.test(attribute.name) ? attribute.name : null, target_match: true });
      if (carriers.length >= 64) break;
    }
    const href = element.getAttribute('href');
    if (carriers.length < 64 && typeof href === 'string' && href.length <= 4096) try {
      const url = new URL(href, location.href), hashQuery = url.hash.includes('?') ? url.hash.slice(url.hash.indexOf('?') + 1) : '';
      for (const query of [url.searchParams, new URLSearchParams(hashQuery)]) for (const [key, value] of [...query].slice(0, 64))
        if (value === limits.target_id && carriers.length < 64) carriers.push({ element_index: index, kind: 'href-query', key: keyClass(key),
          key_name: /^[A-Za-z][A-Za-z0-9_-]{0,63}$/.test(key) ? key : null, target_match: true });
    } catch { /* Malformed public links supply no identity evidence. */ }
  }
  const names = [...groups].filter(([, values]) => values.join(' ').replace(/\s+/g, ' ').trim() === limits.target_name).map(([index]) => index).slice(0, 24);
  const describe = element => {
    const index = indexes.get(element); if (index === undefined || structures.has(index) || structures.size >= limits.structure_nodes) return;
    const box = element.getBoundingClientRect(), tag = element.tagName.toLowerCase(), type = element.getAttribute('data-type');
    const number = value => Number.isFinite(value) ? Math.max(-100000, Math.min(100000, Math.round(value * 10) / 10)) : null;
    structures.set(index, { element_index: index, parent_index: indexes.get(element.parentElement) ?? null,
      tag: /^(?:div|span|a|button|img|h[1-6]|p|section|li|ul|ol|article)$/.test(tag) ? tag : 'other',
      classes: Array.from({ length: Math.min(element.classList.length, 24) }, (_, at) => element.classList[at])
        .filter(value => /^(?:card|item|emby|button|text|section|list)[A-Za-z0-9_-]{0,58}$/.test(value)).slice(0, 12),
      visible: visible(element), rect: { x: number(box.x), y: number(box.y), width: number(box.width), height: number(box.height) },
      data_type_present: type !== null, type_matches_expected: type === 'Movie', type_casefold_matches_movie: typeof type === 'string' && type.toLowerCase() === 'movie' });
  };
  for (const index of [...names, ...carriers.map(value => value.element_index)]) {
    const element = elements[index]; let parent = element;
    for (let depth = 0; parent && depth < 4; depth++, parent = parent.parentElement) describe(parent);
    describe(element.closest('.card, .cardBox, [role="listitem"], li'));
    describe(element.closest('[role="list"], ul, ol, .itemsContainer'));
  }
  return { fragments, structures: [...structures.values()], target_name_matches: names, target_id_carriers: carriers,
    flags: { viewport_valid: innerWidth > 0 && innerHeight > 0, elements_truncated: elementsTruncated,
      text_truncated: textTruncated, password_visible: passwordVisible, iframe_count: iframeCount, visible_iframe_count: visibleIframes },
    element_count: elements.length + (elementsTruncated ? 1 : 0), text_nodes_scanned: Math.min(textCount, limits.viewport_text_nodes), visible_text_chars: chars };
}

export async function observeLibraryChangedViewport(page, targetName, secrets) {
  need(safeText(targetName, 256)); const variants = libraryChangedSecretVariants(secrets);
  const raw = await page.locator('body').evaluateAll(collectLibraryChangedPublicDOM, { ...CHANGED_DIAGNOSTIC_LIMITS, target_id: ITEM, target_name: targetName });
  try {
    need(record(raw) && Array.isArray(raw.fragments) && raw.fragments.length <= CHANGED_DIAGNOSTIC_LIMITS.viewport_text_nodes &&
      raw.fragments.every(value => record(value) && typeof value.text === 'string' && value.text.length <= CHANGED_DIAGNOSTIC_LIMITS.viewport_chars));
    const joined = raw.fragments.map(value => value.text).join(' '), compact = raw.fragments.map(value => value.text).join('');
    need(joined.length <= CHANGED_DIAGNOSTIC_LIMITS.viewport_chars && Array.isArray(raw.structures) && raw.structures.length <= 96 &&
      Array.isArray(raw.target_name_matches) && raw.target_name_matches.length <= 24 && Array.isArray(raw.target_id_carriers) && raw.target_id_carriers.length <= 64);
    const affected = new Set();
    for (const separator of ['', ' ']) {
      const text = separator ? joined : compact, lower = text.toLowerCase(), ranges = []; let offset = 0;
      for (const value of raw.fragments) { ranges.push([offset, offset + value.text.length]); offset += value.text.length + separator.length; }
      for (const variant of variants) {
        let at = lower.indexOf(variant.toLowerCase());
        while (at >= 0) {
          for (let index = 0; index < ranges.length; index++) if (ranges[index][0] < at + variant.length && ranges[index][1] > at) affected.add(index);
          at = lower.indexOf(variant.toLowerCase(), at + Math.max(1, variant.length));
        }
      }
    }
    const found = affected.size > 0;
    const masks = new Set(); let unmaskable = false;
    for (const value of raw.fragments) if (/\b[A-Za-z0-9][A-Za-z0-9_+/.=-]{23,}\b|\b(?:password|authorization|api[_-]?key|access[_-]?token)\b/i.test(value.text)) {
      if (value.leaf_owner && Number.isSafeInteger(value.element_index) && value.element_index >= 0) masks.add(value.element_index); else unmaskable = true;
    }
    const fragments = raw.fragments.map((value, index) => ({ element_index: value.element_index,
      text: affected.has(index) ? '[text redacted: known credential]' : sanitizeLibraryChangedText(value.text, secrets, variants) }));
    const structures = raw.structures.map(value => ({ ...value, classes: value.classes.map(text => sanitizeLibraryChangedText(text, secrets, variants)) }));
    const carriers = raw.target_id_carriers.map(value => ({ ...value, key_name: typeof value.key_name === 'string'
      ? sanitizeLibraryChangedText(value.key_name, secrets, variants) : '[redacted-key]' }));
    const result = { fragments, structures, target_name_matches: raw.target_name_matches, target_id_carriers: carriers,
      flags: { ...raw.flags, known_secret_found: found, unmaskable_text: unmaskable, mask_limit_exceeded: masks.size > CHANGED_DIAGNOSTIC_LIMITS.masks,
        masked_text_nodes: masks.size }, element_count: raw.element_count, text_nodes_scanned: raw.text_nodes_scanned, visible_text_chars: raw.visible_text_chars };
    return { public: result, mask_indices: [...masks], stability: canonical({ structures, fragments, masks: [...masks], count: raw.element_count }) };
  } finally { for (const value of raw.fragments ?? []) value.text = null; }
}

export async function captureLibraryChangedFailureScreenshot(actor, login, secrets, publish = publishLibraryChangedScreenshot, observe = observeLibraryChangedViewport) {
  let flags = { login_proven: false, pre_post_stable: false, viewport_valid: null, password_visible: null,
    iframe_count: null, visible_iframe_count: null, known_secret_found: null, elements_truncated: null,
    text_truncated: null, unmaskable_text: null, mask_limit_exceeded: null, masked_text_nodes: 0 };
  const omitted = reason => ({ status: 'omitted', reason, flags, artifact: null });
  if (!actor?.proven || !actor.page || !login?.bound || typeof actor.token !== 'string' || sha(actor.token) !== login.bound.token_sha256)
    return omitted('login_not_proven');
  flags.login_proven = true;
  let bytes;
  try {
    const route = libraryChangedRoute(actor.page.url()), documentID = actor.libraryChangedDocumentID;
    const before = await bounded(observe(actor.page, actor.libraryChangedTargetName, secrets), CHANGED_DIAGNOSTIC_LIMITS.capture_ms);
    flags = { ...flags, ...before.public.flags };
    const rejection = value => !value.viewport_valid ? 'viewport_unavailable' : value.known_secret_found ? 'known_credential_visible'
      : value.elements_truncated || value.text_truncated ? 'viewport_scan_incomplete'
        : value.unmaskable_text ? 'text_mask_unavailable' : value.mask_limit_exceeded ? 'text_mask_limit' : null;
    const reason = rejection(flags); if (reason) return omitted(reason);
    const mask = [actor.page.locator('input, textarea, [contenteditable], [data-userid], [data-user-id], iframe'),
      actor.page.getByText('m3e-client-viewer', { exact: true }), ...before.mask_indices.map(index => actor.page.locator('body *').nth(index))];
    bytes = await bounded(actor.page.screenshot({ type: 'png', fullPage: false, timeout: 2000, mask }), CHANGED_DIAGNOSTIC_LIMITS.capture_ms);
    const after = await bounded(observe(actor.page, actor.libraryChangedTargetName, secrets), CHANGED_DIAGNOSTIC_LIMITS.capture_ms);
    flags = { ...flags, ...after.public.flags, pre_post_stable: libraryChangedRoute(actor.page.url()) === route &&
      actor.libraryChangedDocumentID === documentID && before.stability === after.stability };
    if (rejection(flags)) return omitted(rejection(flags));
    if (!flags.pre_post_stable) return omitted('visible_content_changed');
    if (!Buffer.isBuffer(bytes) || bytes.length > CHANGED_DIAGNOSTIC_LIMITS.screenshot_bytes) return omitted('screenshot_size_limit');
    const artifact = await bounded(publish(bytes), CHANGED_DIAGNOSTIC_LIMITS.publish_ms);
    return { status: 'saved', reason: null, flags, artifact };
  } catch { return omitted('screenshot_unavailable'); }
  finally { bytes?.fill(0); }
}

export function libraryChangedStageRecord(name, observation, binding, session, previousControl) {
  const shapes = { discovery: ['home', 'dom', 'reads', 'query_allowlist', 'socket', 'collection_folder_reads', 'collection_folder', 'navigation'],
    armed: ['quiet', 'boundary'],
    'restore-armed': ['forward', 'boundary'], restored: ['restored'] };
  need(STAGES.includes(name) && exact(observation, shapes[name]) && descriptor(session) && SHA.test(binding.token_sha256) &&
    (name === 'discovery' ? previousControl === null : SHA.test(previousControl)));
  return { marker: 'goby-client-library-changed-stage-v1', version: 1,
    input_sha256: binding.input_sha256, source_closure_sha256: binding.source_closure_sha256,
    controller: clone(binding.controller), node_process: clone(binding.node_process), name,
    token_sha256: binding.token_sha256, session_private: clone(session), previous_control_sha256: previousControl,
    observation: clone(observation) };
}

export function validateLibraryChangedAbort(value, binding, state, loginProof, session) {
  need(exact(value, ['marker', 'version', 'input_sha256', 'source_closure_sha256', 'controller', 'node_process', 'name',
    'failure', 'previous_stage_sha256', 'previous_control_sha256', 'token_sha256', 'session_private']) &&
    value.marker === 'goby-client-library-changed-abort-v1' && value.version === 1 &&
    ['input_sha256', 'source_closure_sha256', 'controller', 'node_process'].every(key => same(value[key], binding[key])) &&
    typeof value.name === 'string' && /^[a-z][a-z0-9_-]{0,79}$/.test(value.name) &&
    typeof value.failure === 'string' && /^[a-z][a-z0-9_-]{0,127}$/.test(value.failure));
  need(value.previous_stage_sha256 === null && state.stages.length === 0 && state.stage_attempts.length === 0 ||
    [...state.stages, ...state.stage_attempts].some(stage => stage.sha256 === value.previous_stage_sha256));
  need(value.previous_control_sha256 === null || SHA.test(value.previous_control_sha256));
  need(value.token_sha256 === null || SHA.test(value.token_sha256) && value.token_sha256 === loginProof?.token_sha256);
  need(value.session_private === null || descriptor(value.session_private) && same(value.session_private, session));
  return clone(value);
}

export class LibraryChangedWorkflow {
  constructor({ input, binding, actor, observer, report, publish = publishLibraryChangedRecord, read = readLibraryChangedControl,
    now = () => Date.now(), wait = delay, dom = observeLibraryChangedDOM, homeDOM = observeHomeDOM }) {
    Object.assign(this, { input, binding, actor, observer, report, publish, read, now, wait, dom, homeDOM });
    this.started = actor.started; this.deadline = this.started + CHANGED_LIMITS.work_ms;
    this.state = { input, started_at: this.started, stages: [], stage_attempts: [], consumed: [], reservation: null, aborted: false };
    this.publishing = new Map(); this.windowSamples = new Map(); this.samples = 0;
    this.phase = 'discovery'; this.closeControl = null; this.disposed = false;
    this.lastDiscovery = { home: null, dom: null, navigation: { before_route: null, current_route: null, before_sequence: 0 },
      catalog: { physical: [], frames: [], pairs: [], correlation_error: null } };
    this.failureDiagnosticsAttempted = false;
    Object.assign(report, { stages: [], controls: [], stage_publication_attempts: [], abort: null, restoration: 'pending' });
  }
  active() { need(!this.disposed && !this.state.aborted && this.now() < this.deadline, 'library_changed_work_deadline'); }
  async controlRecord(name) {
    const source = await bounded(this.read(name), 3000, 'library_changed_control_read_timeout');
    if (source.publication === 'publishing') {
      if (!this.publishing.has(name)) this.publishing.set(name, this.now());
      need(this.now() - this.publishing.get(name) <= CHANGED_LIMITS.publication_wait_ms, 'library_changed_publication_timeout');
    } else this.publishing.delete(name);
    return source;
  }
  async noticeClose() {
    const source = this.closeControl ?? await this.controlRecord('close');
    if (source.publication !== 'ready') return;
    validateLibraryChangedControl(source.value, this.binding, { ...this.state, aborted: true }, this.now());
    this.closeControl = source; throw new Error('library_changed_controller_closed_early');
  }
  async noticeAbort() {
    const source = await this.controlRecord('abort');
    if (source.publication !== 'ready') return;
    const value = validateLibraryChangedAbort(source.value, this.binding, this.state, this.report.login_proof, this.report.session_private);
    this.report.controller_abort = { path: source.path, sha256: source.sha256, value };
    throw new Error('library_changed_controller_aborted');
  }
  async control(name, { failing = false, sample = null } = {}) {
    const until = this.now() + (failing ? CHANGED_LIMITS.abort_wait_ms : CHANGED_LIMITS.control_wait_ms);
    while (!this.disposed && this.now() < until) {
      if (!failing) { this.active(); await this.noticeAbort(); }
      const close = this.closeControl ?? await this.controlRecord('close');
      if (close.publication === 'ready') {
        validateLibraryChangedControl(close.value, this.binding, name === 'close' ? this.state : { ...this.state, aborted: true }, this.now());
        this.closeControl = close;
        if (name !== 'close') throw new Error('library_changed_controller_closed_early');
        return close;
      }
      if (name !== 'close') {
        const next = await this.controlRecord(name);
        if (next.publication === 'ready') {
          validateLibraryChangedControl(next.value, this.binding, this.state, this.now());
          await bounded(this.actor.assertPinned(), 10000); this.state.consumed.push(name);
          if (name === 'reserved') this.state.reservation = clone(next.value.reservation);
          this.report.controls.push({ name, path: next.path, sha256: next.sha256, value: clone(next.value) }); return next;
        }
      }
      if (sample) await sample();
      await this.wait(Math.min(CHANGED_LIMITS.poll_ms, Math.max(1, until - this.now())));
    }
    throw new Error(failing ? 'library_changed_restoration_confirmation_timeout' : 'library_changed_control_timeout');
  }
  async capture(expected, forbidden) {
    this.active(); await this.noticeAbort(); await this.noticeClose(); await bounded(this.actor.settled(), 3000);
    this.observer.assertStable();
    const started = this.observer.elapsed();
    const raw = await bounded(this.dom(this.actor.page, this.input.target, expected, forbidden, this.observer.documentID), 3000);
    if (Array.isArray(raw.container_observations)) {
      this.lastDiscovery.container_observations = clone(raw.container_observations.slice(0, 32));
      delete raw.container_observations;
    }
    raw.started_elapsed_ms = started;
    if (libraryChangedRoute(this.actor.page.url()) !== raw.route || this.observer.documentID !== raw.document_id) return raw;
    const boundary = this.observer.window, after = boundary?.started_sequence ?? this.lastDiscovery.navigation.before_sequence;
    return bindLibraryChangedDOM(raw, { phase: boundary?.name ?? 'discovery', physical: this.observer.physical, frames: this.observer.frames,
      token_sha256: this.observer.boundToken(), after_sequence: after, boundary, discovery: this.report.discovery,
      events: { physical: this.observer.wireMessages.filter(value => value.sequence > after),
        browser: this.observer.browserMessages.filter(value => value.sequence > after) } });
  }
  async publishStage(name, observation, previousControl) {
    this.active(); need(name === STAGES[this.state.stages.length] && this.report.session_private);
    const value = libraryChangedStageRecord(name, observation, { ...this.binding, token_sha256: this.observer.boundToken() },
      this.report.session_private, previousControl);
    const publicationStarted = this.now(), bytes = encodeLibraryChangedRecord(value), expectedHash = sha(bytes); bytes.fill(0);
    const filename = OUTPUT + '/stage-' + name + '.json';
    const attempt = { name, path: filename, sha256: expectedHash, publication_started_at: publicationStarted };
    this.state.stage_attempts.push(attempt);
    const diagnostic = { name, path: filename, sha256: expectedHash, publication_started_elapsed_ms: publicationStarted - this.started, completed: false };
    this.report.stage_publication_attempts.push(diagnostic);
    const source = await bounded(this.publish(filename, value), 10000, 'library_changed_publication_timeout');
    need(source.path === filename && source.sha256 === expectedHash, 'library_changed_stage_publication_mismatch');
    diagnostic.completed = true; this.state.stages.push({ name, ...source, publication_started_at: publicationStarted });
    this.report.stages.push({ name, ...source });
  }
  retainDiscovery(values = {}) {
    const previous = this.lastDiscovery;
    if (values.home !== undefined) previous.home = clone(values.home);
    if (values.dom !== undefined) {
      previous.dom = clone(values.dom);
      if (previous.dom?.selector?.card_class) previous.dom.selector.card_class =
        sanitizeLibraryChangedText(previous.dom.selector.card_class, this.actor.diagnosticSecrets?.() ?? [],
          libraryChangedSecretVariants(this.actor.diagnosticSecrets?.() ?? []));
    }
    if (values.before_route !== undefined) previous.navigation.before_route = values.before_route;
    if (values.before_sequence !== undefined) previous.navigation.before_sequence = values.before_sequence;
    try { previous.navigation.current_route = libraryChangedRoute(this.actor.page.url()); } catch { /* Keep the last safe route. */ }
    previous.catalog.physical = clone(this.observer.physical.slice(0, CHANGED_LIMITS.catalog_total));
    previous.catalog.frames = clone(this.observer.frames.slice(0, CHANGED_LIMITS.catalog_total * 2));
    previous.catalog.response_diagnostics = clone((this.observer.responseDiagnostics ?? []).slice(0, CHANGED_LIMITS.catalog_total));
    try { previous.catalog.pairs = pairLibraryChangedReads(previous.catalog.physical, previous.catalog.frames).map(pair => ({
      frame_request_index: pair.frame.index, physical_exchange_id: pair.physical?.id ?? null,
      unambiguous: pair.unambiguous, complete: pair.complete })); }
    catch { previous.catalog.pairs = []; previous.catalog.correlation_error = 'correlation_unavailable'; }
  }
  async captureFailureDiagnostics(failure, secrets, dependencies = {}) {
    if (this.failureDiagnosticsAttempted) return;
    this.failureDiagnosticsAttempted = true;
    const candidates = dependencies.candidates ?? observeLibraryChangedCandidates;
    const viewport = dependencies.viewport ?? observeLibraryChangedViewport;
    const screenshot = dependencies.screenshot ?? captureLibraryChangedFailureScreenshot;
    const publish = dependencies.publish ?? this.publish;
    const result = { discovery_failure: null, screenshot: null, status: 'unavailable', reason: null };
    this.report.diagnostics = result;
    try {
      this.retainDiscovery();
      let targetCandidates = null, candidatesReason = null, publicViewport = null, viewportReason = null;
      try { targetCandidates = await bounded(candidates(this.actor.page, secrets), CHANGED_DIAGNOSTIC_LIMITS.capture_ms); }
      catch { candidatesReason = 'target_candidates_unavailable'; }
      try { publicViewport = (await bounded(viewport(this.actor.page, this.input.target.name, secrets), CHANGED_DIAGNOSTIC_LIMITS.capture_ms)).public; }
      catch { viewportReason = 'viewport_diagnostic_unavailable'; }
      const capture = await bounded(screenshot(this.actor, this.observer.login, secrets), CHANGED_DIAGNOSTIC_LIMITS.capture_ms * 4 + CHANGED_DIAGNOSTIC_LIMITS.publish_ms)
        .catch(() => ({ status: 'omitted', reason: 'screenshot_unavailable', artifact: null }));
      result.screenshot = capture.artifact;
      const value = { marker: 'goby-client-library-changed-discovery-failure-v1', version: 1,
        phase: 'discovery', failure: /^library_changed_[a-z_]+$/.test(failure) ? failure : 'library_changed_discovery_failed',
        last: clone(this.lastDiscovery), target_candidates: targetCandidates, candidates_reason: candidatesReason,
        viewport: publicViewport, viewport_reason: viewportReason, screenshot: capture };
      const encoded = encodeLibraryChangedRecord(value), size = encoded.length;
      try { need(size <= CHANGED_LIMITS.record_bytes && !knownDiagnosticSecret(encoded.toString('utf8'), libraryChangedSecretVariants(secrets))); }
      finally { encoded.fill(0); }
      const artifact = await bounded(publish(OUTPUT + '/discovery-failure.json', value), CHANGED_DIAGNOSTIC_LIMITS.publish_ms);
      result.discovery_failure = { ...artifact, bytes: size }; result.status = 'saved';
    } catch { result.reason = 'failure_diagnostic_unavailable'; }
  }
  async discovery() {
    await bounded(this.actor.open(), 120000); await waitHomeLoginProof(this.observer.login, { settle: () => bounded(this.actor.settled(), 3000) });
    need(this.report.session_private, 'library_changed_login_receipt_missing'); this.observer.attachPage();
    this.actor.phase = 'ui_home'; let home;
    const until = Math.min(this.deadline, this.now() + 20000);
    do {
      this.active(); home = await bounded(this.homeDOM(this.actor.page, this.input.expected_libraries, { require_ids: true }), 3000);
      this.retainDiscovery({ home });
      await bounded(this.actor.settled(), 3000);
      if (home.passed && this.observer.login.viewEvidence('initial').result === 'passed') break;
      await this.wait(100);
    } while (this.now() < until);
    need(home?.passed && this.observer.login.viewEvidence('initial').result === 'passed', 'library_changed_home_prerequisite');
    await bounded(this.actor.proxyIdle(), 10000); this.observer.assertStable(); await bounded(this.actor.assertPinned(), 10000);
    const previousRoute = libraryChangedRoute(this.actor.page.url()), beforeSequence = this.observer.sequence;
    this.retainDiscovery({ before_route: previousRoute, before_sequence: beforeSequence });
    const titles = this.actor.page.getByText('M3e Client Movies', { exact: true }).filter({ visible: true });
    const cards = titles.locator('xpath=ancestor-or-self::*[(self::button or self::a or @role="button") and @data-action="link" and ancestor::*[contains(concat(" ", normalize-space(@class), " "), " card ") or contains(concat(" ", normalize-space(@class), " "), " cardBox ")]][1]').filter({ visible: true });
    need(await cards.count() === 1, 'library_changed_library_control_ambiguous');
    const ids = await cards.evaluate(element => [element, element.closest('.card'), element.closest('.cardBox')]
      .filter(Boolean).map(node => node.getAttribute('data-id')).filter(value => value !== null));
    need(ids.length > 0 && ids.every(id => id === LIBRARY), 'library_changed_library_control_identity');
    this.actor.phase = 'ui_movies_list'; this.observer.phase = 'discovery'; this.observer.noteAction('click-observed-movies-library');
    await cards.click({ timeout: 8000 });
    let dom, reads; const end = Math.min(this.deadline, this.now() + 25000);
    do {
      this.retainDiscovery();
      dom = await this.capture(this.input.target.name, null); this.retainDiscovery({ dom }); reads = this.observer.reads(beforeSequence);
      if (dom.passed && dom.route !== previousRoute && !dom.route.includes(ITEM) && reads.some(pair => pair.complete && libraryChangedFullMovieQuery(pair.physical) &&
        pair.physical.projection.count === 1 &&
        pair.frame.page_route === dom.route && pair.frame.document_id === dom.document_id &&
        pair.physical.projection.target.Name === this.input.target.name && pair.frame.token_sha256 === this.observer.boundToken()) &&
        reads.some(pair => pair.complete && pair.physical.kind === 'collection-folder' &&
          [previousRoute, dom.route].includes(pair.frame.page_route) && pair.frame.document_id === dom.document_id &&
          pair.frame.token_sha256 === this.observer.boundToken())) break;
      await this.wait(100);
    } while (this.now() < end);
    need(dom?.passed && dom.route !== previousRoute && !dom.route.includes(ITEM), 'library_changed_target_card_not_observed');
    const collectionFolderReads = reads.filter(pair => pair.complete && pair.physical.kind === 'collection-folder' &&
      pair.frame.token_sha256 === this.observer.boundToken() && [previousRoute, dom.route].includes(pair.frame.page_route) &&
      pair.frame.document_id === dom.document_id);
    reads = reads.filter(pair => pair.complete && libraryChangedFullMovieQuery(pair.physical) && pair.physical.projection.count === 1 &&
      pair.physical.projection.target.Type === 'Movie' && pair.frame.token_sha256 === this.observer.boundToken() &&
      pair.frame.page_route === dom.route && pair.frame.document_id === dom.document_id &&
      pair.physical.projection.target.Name === this.input.target.name);
    need(reads.length > 0 && reads.length <= 8, 'library_changed_list_read_not_proven');
    await bounded(this.actor.proxyIdle(), 10000); this.observer.assertStable(); await bounded(this.actor.assertPinned(), 10000);
    const queries = new Map();
    for (const pair of reads) queries.set(pair.physical.shape_sha256, { kind: pair.physical.kind, route: pair.physical.route,
      query: clone(pair.physical.query), shape_sha256: pair.physical.shape_sha256 });
    const observation = { home, dom, reads, collection_folder_reads: collectionFolderReads,
      query_allowlist: [...queries.values()], socket: this.observer.socket(),
      navigation: { before_route: previousRoute, after_route: dom.route, before_sequence: beforeSequence } };
    observation.collection_folder = libraryChangedNavigationEvidence(observation, this.input);
    this.observer.discovery = clone(observation); this.report.discovery = clone(observation);
    await this.publishStage('discovery', observation, null);
  }
  async quiet() {
    const reservation = this.state.reservation; need(reservation);
    await bounded(this.actor.proxyIdle(), 10000); await bounded(this.actor.settled(), 3000); this.observer.assertStable();
    need(this.observer.physical.every(entry => entry.terminal) && this.observer.frames.every(entry => entry.finished || entry.failed), 'library_changed_pending_catalog_read');
    const sequence = this.observer.sequence, started = this.observer.elapsed(), route = libraryChangedRoute(this.actor.page.url());
    const document = this.observer.documentID, socket = this.observer.socket(), samples = [];
    const unchanged = () => same(this.observer.socket(), socket) && libraryChangedRoute(this.actor.page.url()) === route &&
      this.observer.documentID === document && this.observer.lifecycle.every(event => event.sequence <= sequence) &&
      this.observer.actions.every(event => event.sequence <= sequence) && this.observer.physical.every(event => event.request_sequence <= sequence) &&
      this.observer.frames.every(event => event.request_sequence <= sequence) &&
      [...this.observer.wireMessages, ...this.observer.browserMessages].every(event => event.sequence <= sequence);
    while (this.observer.elapsed() < started + CHANGED_LIMITS.quiet_ms) {
      const sampleStarted = this.observer.elapsed(), observation = await this.capture(reservation.original_name, reservation.marker_name);
      const stamp = this.observer.stamp(); need(++this.samples <= CHANGED_LIMITS.samples, 'library_changed_sample_limit');
      samples.push({ ...stamp, started_elapsed_ms: sampleStarted, observation });
      need(observation.passed && observation.route === route && observation.document_id === document && unchanged(), 'library_changed_quiet_window_changed');
      await this.wait(Math.min(CHANGED_LIMITS.sample_ms, Math.max(1, started + CHANGED_LIMITS.quiet_ms - this.observer.elapsed())));
    }
    this.observer.assertStable(); await bounded(this.actor.assertPinned(), 10000); need(unchanged(), 'library_changed_quiet_window_changed');
    return { started_elapsed_ms: started, completed_elapsed_ms: this.observer.elapsed(), duration_ms: CHANGED_LIMITS.quiet_ms,
      catalog_requests: 0, library_changed_messages: 0, samples, passed: true };
  }
  async sampleWindow(name) {
    const samples = this.windowSamples.get(name); need(samples);
    if (samples.length && this.observer.elapsed() - samples.at(-1).elapsed_ms < CHANGED_LIMITS.sample_ms) return;
    const reservation = this.state.reservation, expected = name === 'forward' ? reservation.marker_name : reservation.original_name;
    const forbidden = name === 'forward' ? reservation.original_name : reservation.marker_name;
    const started = this.observer.elapsed(), observation = await this.capture(expected, forbidden);
    need(++this.samples <= CHANGED_LIMITS.samples, 'library_changed_sample_limit');
    samples.push({ ...this.observer.stamp(), started_elapsed_ms: started, observation });
  }
  async finishWindow(name, boundary, control) {
    const end = instant(control.value.commit.write_completed_at) - this.started + CHANGED_LIMITS.window_ms;
    need(end > this.observer.elapsed() && this.started + end <= this.deadline, 'library_changed_window_ack_expired');
    while (this.observer.elapsed() < end) {
      this.active(); await this.sampleWindow(name);
      await this.wait(Math.min(CHANGED_LIMITS.poll_ms, Math.max(1, end - this.observer.elapsed())));
    }
    this.observer.assertStable(); await bounded(this.actor.settled(), 3000);
    const observation = this.observer.windowData(boundary, control, this.windowSamples.get(name));
    Object.assign(observation, libraryChangedWindowEvidence(observation, this.input, this.state.reservation, this.report.discovery));
    this.report[name] = clone(observation);
    need(observation.result === 'passed', observation.outcome);
    await bounded(this.actor.assertPinned(), 10000); return observation;
  }
  async execute() {
    await this.discovery(); this.phase = 'reserved';
    const reserved = await this.control('reserved');
    const quiet = await this.quiet(); this.phase = 'armed';
    const forwardBoundary = this.observer.startWindow('forward'); this.windowSamples.set('forward', []);
    this.report.armed = { quiet, boundary: forwardBoundary };
    await this.publishStage('armed', this.report.armed, reserved.sha256);
    const forwardControl = await this.control('forward', { sample: () => this.sampleWindow('forward') });
    this.phase = 'forward'; const forward = await this.finishWindow('forward', forwardBoundary, forwardControl);
    this.phase = 'restore-armed';
    const restoredBoundary = this.observer.startWindow('restored'); this.windowSamples.set('restored', []);
    this.report.restore_armed = clone(restoredBoundary);
    await this.publishStage('restore-armed', { forward, boundary: restoredBoundary }, forwardControl.sha256);
    const restoredControl = await this.control('restored', { sample: () => this.sampleWindow('restored') });
    this.phase = 'restored'; const restored = await this.finishWindow('restored', restoredBoundary, restoredControl);
    await this.publishStage('restored', { restored }, restoredControl.sha256);
    this.phase = 'close'; const close = await this.control('close');
    this.observer.assertStable();
    this.report.control_close = { path: close.path, sha256: close.sha256, value: clone(close.value) };
    this.report.restoration = close.value.restoration; this.observer.window = null;
  }
  async abort(failure) {
    this.state.aborted = true; this.report.failure = failure; this.report.restoration = 'unconfirmed';
    const previous = this.state.stage_attempts.at(-1) ?? this.state.stages.at(-1);
    try {
      this.report.abort = await bounded(this.publish(OUTPUT + '/abort.json', { marker: 'goby-client-library-changed-abort-v1', version: 1,
        ...clone(this.binding), name: this.phase, failure, previous_stage_sha256: previous?.sha256 ?? null,
        previous_control_sha256: this.report.controls.at(-1)?.sha256 ?? null,
        token_sha256: this.observer.login.bound?.token_sha256 ?? null, session_private: this.report.session_private }), 10000, 'library_changed_publication_timeout');
    } catch { this.report.abort_journal_failed = true; }
    try {
      const close = await this.control('close', { failing: true });
      this.report.control_close = { path: close.path, sha256: close.sha256, value: clone(close.value) };
      this.report.restoration = close.value.restoration;
    } catch { this.report.restoration_wait_failed = true; }
    this.observer.window = null;
  }
}

export function libraryChangedObservationPassed(report) {
  try {
    need(report.failure === null && !report.abort && !report.controller_abort && !report.abort_journal_failed && report.restoration === 'confirmed' &&
      same(report.stages.map(value => value.name), STAGES) && same(report.controls.map(value => value.name), ['reserved', 'forward', 'restored']) &&
      report.control_close?.value?.restoration === 'confirmed' && report.control_close.value.previous_stage_sha256 === report.stages[3].sha256 &&
      report.discovery?.dom?.passed && report.discovery.reads.some(pair => pair.complete) &&
      report.armed?.quiet?.passed && report.armed.quiet.duration_ms === CHANGED_LIMITS.quiet_ms &&
      report.armed.quiet.catalog_requests === 0 && report.armed.quiet.library_changed_messages === 0 &&
      report.catalog_observation?.observer_failure === null && homeSessionClosed(report));
    need(same(report.discovery.collection_folder, libraryChangedNavigationEvidence(report.discovery, { target: report.target })));
    const quietSamples = report.armed.quiet.samples;
    need(Array.isArray(quietSamples) && quietSamples.length > 0 && quietSamples.length <= CHANGED_LIMITS.samples);
    for (const sample of quietSamples) {
      need(Number.isFinite(sample.started_elapsed_ms) && Number.isFinite(sample.elapsed_ms) &&
        sample.observation?.started_elapsed_ms >= sample.started_elapsed_ms && sample.observation.started_elapsed_ms <= sample.elapsed_ms &&
        sample.observation.expected_name === report.target.name);
      const rebound = bindLibraryChangedDOM(sample.observation, { phase: 'discovery', physical: report.discovery.reads.map(value => value.physical),
        frames: report.discovery.reads.map(value => value.frame), token_sha256: report.discovery.socket.token_sha256,
        after_sequence: report.discovery.navigation.before_sequence });
      need(rebound.passed && sample.observation.passed && sample.observation.identity_proven &&
        sample.observation.identity_mode === rebound.identity_mode && same(sample.observation.wire_identity, rebound.wire_identity));
    }
    const reservation = report.controls[0].value.reservation;
    need(report.forward.control_sha256 === report.controls[1].sha256 && report.restored.control_sha256 === report.controls[2].sha256 &&
      same(report.forward.commit, report.controls[1].value.commit) && same(report.restored.commit, report.controls[2].value.commit) &&
      same(report.forward.boundary.route, report.restored.boundary.route) && same(report.forward.boundary.document_id, report.restored.boundary.document_id) &&
      same(report.forward.boundary.connection_id, report.restored.boundary.connection_id) &&
      same(report.forward.boundary.token_sha256, report.restored.boundary.token_sha256) &&
      report.forward.proof.message_id !== report.restored.proof.message_id);
    for (const name of ['forward', 'restored']) need(libraryChangedWindowEvidence(report[name], { target: report.target }, reservation, report.discovery).result === 'passed');
    return true;
  } catch { return false; }
}

export function disposeLibraryChangedSecrets(observer, secrets, credentials) {
  try { observer.dispose(); } finally { secrets.fill(null); credentials.viewer.password = null; }
}

export async function runLibraryChanged(options) {
  let setupPhase = 'arguments', parsed, input, rootIdentity, node, baseline, credentials, account, fixture, outputIdentity;
  try {
    process.umask(0o077);
    parsed = parseLibraryChangedArguments(Object.entries(options).flatMap(([key, value]) => ['--' + key, value]));
    setupPhase = 'input'; input = validateLibraryChangedInput(await checkedHomeFile(parsed.input, parsed['input-sha256']));
    setupPhase = 'root_identity'; rootIdentity = await privateDirectory(ROOT);
    setupPhase = 'process_identity'; node = await libraryChangedProcessIdentity(input);
    setupPhase = 'source_closure'; need(Object.hasOwn(input.source_closure, SELF));
    for (const [filename, hash] of Object.entries(input.source_closure)) await checkedHomeFile(filename, hash, 2 * 1024 * 1024, false, false);
    setupPhase = 'baseline';
    const current = await readLibraryChangedSnapshot(input, 'history_after_snapshot');
    const before = await readLibraryChangedSnapshot(input, 'before_snapshot');
    baseline = validateLibraryChangedBaseline(input, before, current);
    need(Date.now() >= instant(before.database.metadata.captured_at) && Date.now() - instant(before.database.metadata.captured_at) <= 180000);
    setupPhase = 'credentials'; credentials = await checkedHomeFile(input.actor.credentials.path, input.actor.credentials.sha256);
    account = validateLibraryChangedViewer(credentials, input);
    setupPhase = 'fixture'; fixture = await loadLibraryChangedSource55Fixture({ input, inputSHA256: parsed['input-sha256'] });
    setupPhase = 'initial_pin'; need(fixture.serverId === SERVER && typeof fixture.assertPinned === 'function'); await fixture.assertPinned();
    setupPhase = 'output'; await fs.mkdir(OUTPUT, { mode: 0o700 }); outputIdentity = await privateDirectory(OUTPUT);
  } catch (error) {
    if (credentials?.viewer) credentials.viewer.password = null;
    throw libraryChangedSetupFailure(error, setupPhase);
  }
  const sourceDigest = homeSourceDigest(input.source_closure);
  const binding = { input_sha256: parsed['input-sha256'], source_closure_sha256: sourceDigest, controller: clone(input.controller), node_process: node };
  const report = { marker: 'goby-client-library-changed-report-v1', version: 1, mode: input.mode, result: 'failed', outcome: 'failed',
    library_changed_client_acceptance: false, client_acceptance: false, full_m3_complete: false, ...clone(binding),
    candidate: clone(input.candidate), authority: clone(input.authority), target: clone(input.target), limits: clone(CHANGED_LIMITS),
    scope: { native_credentials: false, metadata_writes_by_browser: 0, playback_info: false, media_playback: false,
      windows: 'One native Name edit and one restoration, with no browser interaction or reload while armed' },
    login_proof: null, session_private: null, capabilities_private: null, control_close: null,
    discovery: null, armed: null, forward: null, restore_armed: null, restored: null, actor: {}, closure: null, failure: null };
  const secrets = [account.password]; let actor, flow;
  const login = new HomeViewsObserver({ account, expected: input.expected_libraries, existingDevices: baseline.devices,
    existingSessions: baseline.session_ids, existingTokens: baseline.token_hashes, serverId: SERVER, started: Date.now(), scope: 'permission',
    onProven: ({ token, proof }) => {
      need(actor.token === null || actor.token === token); actor.token = token; actor.proven = true;
      actor.report.token_fingerprint = proof.token_sha256; actor.report.ordinary_authority_confirmed = true;
      report.login_proof = clone(proof); if (!secrets.includes(token)) secrets.push(token);
    },
    persistLogin: async ({ token, proof }) => {
      report.session_private = await publishLibraryChangedRecord(OUTPUT + '/session-private.json', {
        marker: 'goby-client-library-home-session-v1', version: 1, ...clone(binding), proof, token }); return report.session_private;
    } });
  const observer = new LibraryChangedObserver({ input, login });
  const pin = async () => {
    need(same(await privateDirectory(ROOT), rootIdentity) && same(await privateDirectory(OUTPUT), outputIdentity));
    await checkedHomeFile(parsed.input, parsed['input-sha256'], 2 * 1024 * 1024, false);
    for (const source of [...report.stages, ...report.controls, report.session_private, report.control_close].filter(Boolean)) {
      await checkedHomeFile(source.path, source.sha256, CHANGED_LIMITS.record_bytes, false);
    }
    need(same(await libraryChangedProcessIdentity(input), node)); await fixture.assertPinned();
  };
  actor = createLibraryChangedSource55BrowserActor({ account, pin, report: report.actor, observer: observer.hooks() }); observer.setActor(actor);
  actor.libraryChangedTargetName = input.target.name;
  actor.diagnosticSecrets = () => secrets;
  actor.rememberSecret = value => { if (!secrets.includes(value)) secrets.push(value); };
  actor.authorizeSocket = async () => {
    await waitHomeLoginProof(login); need(login.bound.token_sha256 === sha(actor.token) && report.session_private);
  };
  report.started_at = new Date(actor.started).toISOString();
  report.elapsed_clock = 'Monotonic actor-offset timestamps and sequences for client receive, request and DOM; physical receive offsets also retain the core transport clock';
  flow = new LibraryChangedWorkflow({ input, binding, actor, observer, report });
  try { await flow.execute(); }
  catch (error) {
    const reason = typeof error?.message === 'string' && /^(?:library_changed_[a-z_]+|(?:websocket|automatic_http|dom_update)_not_observed_within_window)$/.test(error.message)
      ? error.message : 'library_changed_flow_failed';
    report.failure_diagnostic = safeBrowserFailure(error, actor.phase);
    if (flow.phase === 'discovery') await flow.captureFailureDiagnostics(reason, secrets);
    await flow.abort(reason);
  } finally {
    try {
      if (flow.phase === 'discovery' && !flow.failureDiagnosticsAttempted) {
        try { await flow.captureFailureDiagnostics('library_changed_discovery_failed', secrets); }
        catch { report.diagnostics = { status: 'unavailable', reason: 'failure_diagnostic_unavailable', discovery_failure: null, screenshot: null }; }
      }
      login.phase = 'cleanup'; observer.phase = 'cleanup'; observer.window = null; flow.disposed = true;
      try { await bounded(actor.close(), CHANGED_LIMITS.cleanup_ms, 'library_changed_cleanup_timeout'); }
      catch { report.failure ??= 'library_changed_browser_close_failed'; }
      report.closure = actor.report.home_closure ?? null; report.observation = login.safeEvidence();
      report.catalog_observation = { physical_count: observer.physical.length, frame_count: observer.frames.length,
        physical_message_count: observer.wireMessages.length, browser_message_count: observer.browserMessages.length,
        lifecycle_count: observer.lifecycle.length, action_count: observer.actions.length, observer_failure: observer.failure };
      if (login.bound) {
        try {
          const entries = login.privateCapabilities();
          const source = await publishLibraryChangedRecord(OUTPUT + '/capabilities-private.json', {
            marker: 'goby-client-library-home-capabilities-v1', version: 1, input_sha256: binding.input_sha256,
            source_closure_sha256: sourceDigest, node_process: node, session_id: login.bound.session_id,
            token_sha256: login.bound.token_sha256, entries });
          const successes = entries.filter(entry => entry.completed && entry.status === 204);
          report.capabilities_private = { ...source, request_count: entries.length, last_successful_body_sha256: successes.at(-1)?.body_sha256 ?? null };
          need(entries.length === actor.report.proxy.capabilities && entries.every(entry => entry.completed && entry.status === 204));
        } catch { report.failure ??= 'library_changed_capabilities_evidence_failed'; }
      }
      resanitizeBrowserDiagnostics({ accounts: [actor.report] }, secrets); report.completed_at = new Date().toISOString();
      const passed = libraryChangedObservationPassed(report);
      report.result = passed ? 'passed' : 'failed'; report.library_changed_client_acceptance = passed;
      report.outcome = passed ? 'automatic_catalog_refresh_and_restoration_observed' : 'failed';
      if (!passed) report.failure ??= 'library_changed_evidence_incomplete';
      const encoded = JSON.stringify(report); need(secrets.every(secret => !secret || !encoded.includes(secret)), 'library_changed_report_secret_guard');
      await publishLibraryChangedRecord(OUTPUT + '/report.json', report);
    } finally {
      disposeLibraryChangedSecrets(observer, secrets, credentials);
    }
  }
  return report;
}

if (process.argv[1] && path.resolve(process.argv[1]) === SELF) {
  let terminalPhase = 'arguments';
  try {
    const options = parseLibraryChangedArguments(process.argv.slice(2)); terminalPhase = 'output';
    const report = await runLibraryChanged(options);
    process.stdout.write(JSON.stringify({ marker: report.marker, result: report.result, outcome: report.outcome,
      library_changed_client_acceptance: report.library_changed_client_acceptance, client_acceptance: false, failure: report.failure }) + '\n');
    if (report.result !== 'passed') process.exitCode = 1;
  } catch (error) {
    const retained = record(error) && Object.getOwnPropertyDescriptor(error, 'diagnostic')?.value;
    const phase = record(retained) && Object.getOwnPropertyDescriptor(retained, 'phase')?.value;
    const diagnostic = libraryChangedSetupDiagnostic(error, SETUP_PHASES.includes(phase) ? phase : terminalPhase);
    process.stdout.write(JSON.stringify({ marker: 'goby-client-library-changed-report-v1', result: 'failed',
      library_changed_client_acceptance: false, client_acceptance: false, failure: 'library_changed_setup_or_terminal_report',
      setup_diagnostic: diagnostic }) + '\n');
    process.exitCode = 1;
  }
}
