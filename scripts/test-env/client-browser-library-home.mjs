#!/usr/bin/env node
/** One existing viewer, real Home navigation, one browser reload, and owned UI logout. */
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { TextDecoder } from 'node:util';
import { createHomeOnlyBrowserActor, authorityForURL, observedAuthority, homeNavigationLocation,
  safeBrowserFailure, resanitizeBrowserDiagnostics } from './client-browser-cross-user.mjs';
import { loadSpecialFeaturesFixture } from './client-browser-special-features-fixture.mjs';

const WORK = '/opt/goby-test/exec-work-m3e';
const ROOT = WORK + '/client-library-ui-baseline-v3';
const OUTPUT = ROOT + '/browser';
const ORIGIN = 'http://127.0.0.1:18196';
const UNIT = 'goby-client-library-ui-baseline-v3.service';
const CGROUP = '/system.slice/' + UNIT;
const USER = 'ecbbe4cb82403879bc4b4f78894c5738';
const SERVER = 'c7cfd76b1dee728b2bad523793a37ccb';
const RECOVERY_AFTER = Object.freeze({ path: WORK + '/client-library-ui-session-recovery-01/after-full.json',
  sha256: '5d0f3818abf5617a817541fc4d08cca5492a579b9ce5af99d0cec45b7d5f1ee0' });
const SELF = fileURLToPath(import.meta.url);
const LIBRARIES = ['a9993591e72f0f2e7babcbf8b9c50790', '6383d20008836e137559698c29b10395',
  'a34ce665fb75421ef7551570f353d705', '57a85c1ca5b6c7ae602c587755250b2f'];
const SOURCES = ['client-browser-library-home.mjs', 'client-browser-cross-user.mjs',
  'client-browser-special-features-fixture.mjs', 'client-browser-goby-fixture.mjs', 'client-browser-session-proof.mjs'];
const SHA = /^[0-9a-f]{64}$/;
const ID = /^[0-9a-f]{32}$/;
const RECORD = value => value !== null && typeof value === 'object' && !Array.isArray(value);
const clone = value => JSON.parse(JSON.stringify(value));
const sha = value => createHash('sha256').update(value).digest('hex');
const sorted = value => Array.isArray(value) ? value.map(sorted) : RECORD(value)
  ? Object.fromEntries(Object.keys(value).sort().map(key => [key, sorted(value[key])])) : value;
const same = (left, right) => JSON.stringify(sorted(left)) === JSON.stringify(sorted(right));
const keys = (value, expected) => RECORD(value) && same(Object.keys(value).sort(), [...expected].sort());
function need(value, code = 'home_observation_guard_rejected') { if (!value) throw new Error(code); }
function bounded(promise, milliseconds) {
  let timer;
  return Promise.race([promise, new Promise((_, reject) => {
    timer = setTimeout(() => reject(new Error('home_observation_timeout')), milliseconds);
  })]).finally(() => clearTimeout(timer));
}
const utc = value => typeof value === 'string' && /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?Z$/.test(value) && Number.isFinite(Date.parse(value));
const text = (value, maximum = 256) => typeof value === 'string' && value.length > 0 && value.length <= maximum &&
  /^[\x20-\x7e]+$/.test(value) && !/https?:\/\/|[\r\n]/i.test(value);
const descriptor = value => keys(value, ['path', 'sha256']) && typeof value.path === 'string' &&
  value.path.startsWith(WORK + '/') && /^[\x21-\x7e]+$/.test(value.path) && path.posix.normalize(value.path) === value.path &&
  !value.path.includes('\\') && SHA.test(value.sha256);

export function parseHomeArguments(argv) {
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

/** The controller supplies current authority explicitly; no historical run is searched. */
export function validateHomeInput(input) {
  need(keys(input, ['marker', 'version', 'mode', 'root', 'output', 'actor', 'candidate', 'fixture',
    'expected_libraries', 'source_closure', 'authority', 'controller']));
  need(input.marker === 'goby-client-library-home-input-v1' && input.version === 1 && input.mode === 'b-home-views-reload' &&
    input.root === ROOT && input.output === OUTPUT);
  need(keys(input.actor, ['slot', 'user_id', 'credentials', 'account_key']) && input.actor.slot === 'B' && input.actor.user_id === USER &&
    input.actor.account_key === 'viewer' && descriptor(input.actor.credentials) && input.actor.credentials.path === WORK + '/browser.json' &&
    input.actor.credentials.sha256 === '0be6df4acef0565537b3b3a6e8c1518f6bed4023bfdfb5dea60b67dd32fee790');
  const c = input.candidate;
  need(keys(c, ['binary_sha256', 'process', 'runtime_sha256', 'state_sha256', 'server_id', 'base_url', 'direct_url', 'source', 'source_manifest_sha256']) &&
    c.binary_sha256 === 'af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620' &&
    same(c.process, { pid: 748513, start_ticks: 6996875, boot_id: '6bdfc486-7bc8-412f-82b5-70095a09dde7' }) &&
    c.runtime_sha256 === 'd8689a4e0b36816ed462816856dfa73af6fba5f31f044632ed173db99c8842df' &&
    c.state_sha256 === '5319bc49b2753b84ca04f279523f2482a49a94fabd9944dc369347b6d87224e1' && c.server_id === SERVER &&
    c.base_url === ORIGIN && c.direct_url === 'http://127.0.0.1:18198' && c.source === WORK + '/source-attempt-32' &&
    c.source_manifest_sha256 === 'a65070315ce3b31dd70143267cbf774a838bc0e5b1ed99f34c3c759d392daa65');
  need(keys(input.fixture, ['profile_receipt', 'profile_report', 'profile_inspection', 'music_chain', 'music_scan_receipt']) &&
    Object.values(input.fixture).every(descriptor));
  const fixtures = {
    profile_receipt: ['client-special-features-protocol-finalization-v1/completed.json', 'b443e5f6d5faceb3486d68644298b0a1527f6dcc1e4ac1109ff6d0c252fdfb36'],
    profile_report: ['client-special-features-protocol-finalization-v1/report.json', '929e6b6fc0014e6b3afc7ce023ea981b749ce884ed6eb33c34b3617ac116855d'],
    profile_inspection: ['client-special-features-finalization-inspection-v1/report.json', 'fdc8a38f5937964d3dba8e0e47f897108475011e70f4802e30c1803068dc4b5e'],
    music_chain: ['client-music-upgrade-chain-source32.json', 'c235d59a317fe1b95d8d78a51ab32ca556f3ca7872e96bfea51e710e26b0c67a'],
    music_scan_receipt: ['client-music-scan-v1/receipt.json', 'e0b9ba686afb1950d4ab43c3ad5bc39d67090374704379103a29759c70aab93b'],
  };
  for (const [key, [name, digest]] of Object.entries(fixtures)) need(same(input.fixture[key], { path: WORK + '/' + name, sha256: digest }));
  need(keys(input.authority, ['api_report', 'inspection', 'recovery', 'prior_home', 'current_snapshot', 'before_snapshot']) && Object.values(input.authority).every(descriptor));
  for (const [key, name, digest] of [
    ['api_report', 'client-library-restriction-v1/report.json', '193a5cc5ddaa806575d630a03de1268abed2418347b4e57eb5cc57610a04e8eb'],
    ['inspection', 'client-library-restriction-inspection-01/report.json', 'e3dcbc0c21edbc484baab89762e11857a7cbc46889b78701af8dd309f12c3441'],
    ['recovery', 'client-library-ui-session-recovery-01/report.json', '09b2fad0de6dcce6e1d6ecb85a700e94673642f3c5641d3d7dff6bab43e66ffb'],
    ['prior_home', 'client-library-ui-baseline-v2/report.json', '54c02dbc41273a5043853d31126dec7455641d5efbf73ee20b97b13d8826ec23'],
    ['current_snapshot', 'client-library-ui-baseline-v2/after-full.json', '27e4627e774d96ab87fdb51fd5093dafcb01c529b0a8753fd50db566aadbfd26'],
  ]) need(same(input.authority[key], { path: WORK + '/' + name, sha256: digest }));
  need(input.authority.before_snapshot.path === ROOT + '/before-full.json');
  need(Array.isArray(input.expected_libraries) && input.expected_libraries.length === 4 &&
    input.expected_libraries.every(value => keys(value, ['id', 'name']) && ID.test(value.id) && text(value.name, 128)) &&
    same(input.expected_libraries.map(value => value.id).sort(), [...LIBRARIES].sort()) &&
    new Set(input.expected_libraries.map(value => value.name)).size === 4);
  need(RECORD(input.source_closure) && Object.keys(input.source_closure).length >= 5 && Object.keys(input.source_closure).length <= 16 &&
    Object.entries(input.source_closure).every(([filename, digest]) => descriptor({ path: filename, sha256: digest })));
  for (const name of SOURCES) need(Object.keys(input.source_closure).filter(filename => path.posix.basename(filename) === name).length === 1);
  need(keys(input.controller, ['pid', 'start_ticks', 'boot_id', 'unit']) && Number.isSafeInteger(input.controller.pid) && input.controller.pid > 1 &&
    typeof input.controller.start_ticks === 'string' && /^[1-9]\d*$/.test(input.controller.start_ticks) &&
    input.controller.boot_id === c.process.boot_id && input.controller.unit === UNIT);
  return input;
}

export function homeSourceDigest(closure) { return sha(JSON.stringify(sorted(closure))); }

/** Both failed UI attempts stay historical; the last closed attempt supplies the current snapshot. */
export function validateHomeAuthority(input, api, inspection, recovery, priorHome) {
  need(api.result === 'passed' && api.phase === 'complete' && api.restoration === 'confirmed' && inspection.status === 'passed' &&
    inspection.report_sha256 === input.authority.api_report.sha256 &&
    inspection.current_snapshot_sha256 === '1aca0670c3d6f1cd2f45082df89cf4df9930058f9658eb439deef289b39207a3' &&
    inspection.all_three_owned_sessions_revoked === true && inspection.stored_after_matches_current === true);
  const proof = recovery?.proof;
  need(recovery?.marker === 'goby-client-library-home-session-recovery-v1' && recovery.result === 'passed' && recovery.phase === 'complete' &&
    recovery.original_ui_result === 'failed' && recovery.client_acceptance === false && recovery.administrator_closed === true &&
    recovery.http_requests === 4 && Array.isArray(recovery.errors) && recovery.errors.length === 0 &&
    same(recovery.candidate, { binary_sha256: input.candidate.binary_sha256, process: input.candidate.process, state_sha256: input.candidate.state_sha256 }) &&
    same(recovery.evidence?.['after-full.json'], RECOVERY_AFTER));
  need(proof?.administrator_revoked === true && proof.administrator_session_id === 'ad6695676912f2bd2a1bf68b9cfdd9ce' &&
    proof.target_session_id === '00fb0b833884308ab946e1c40ff6abdd' && proof.target_user_id === USER &&
    proof.target_token_sha256 === '9f71d2771f1a0d177041d6815f46d39ecea9a54bf92099a13b89dbef172afb81' &&
    proof.lost_target_token_401_observed === false && proof.new_administrator_sessions === 1 && proof.new_native_session_audits === 3 &&
    proof.new_devices_play_userdata_references_encodings === 0 && proof.old_rows_sequences_private_preserved === true &&
    proof.target_only_revoked_at_changed === true && typeof proof.target_revoked_at === 'string' && Number.isFinite(Date.parse(proof.target_revoked_at)));
  need(priorHome?.marker === 'goby-client-library-home-observation-v1' && priorHome.version === 1 && priorHome.result === 'failed' &&
    priorHome.phase === 'complete' && priorHome.outcome === 'failed' && priorHome.browser_outcome === 'failed' &&
    priorHome.client_acceptance === false && priorHome.permission_ui_acceptance === false && priorHome.worker_closed === true &&
    priorHome.fallback_attempted === false && same(priorHome.candidate, input.candidate) &&
    same(priorHome.authority?.api_report, input.authority.api_report) && same(priorHome.authority?.inspection, input.authority.inspection) &&
    same(priorHome.authority?.recovery, input.authority.recovery) && same(priorHome.authority?.current_snapshot, RECOVERY_AFTER) &&
    same(priorHome.evidence?.['after-full.json'], input.authority.current_snapshot) && Array.isArray(priorHome.errors) &&
    priorHome.errors.length === 1 && priorHome.errors[0].stage === 'final_delta_or_ui_observation' && priorHome.errors[0].failure_type === 'ObservationError');
  need(same(priorHome.proof, { new_b_authentication: 1, new_devices: 1, new_play_userdata_references_encodings: 0,
    new_session_audits: 2, observed_capabilities_bound: true, old_rows_sequences_private_unchanged: true,
    session_id: '2e355f661132b4ba3a3ed1445a16c3c8', token_sha256: 'e795b8ecf91e9a2b934b28484ce80a9c52e40838389473032e954cbf699c3eb3',
    users_and_policies_unchanged: true }));
  return true;
}

/** Parse only the four client metadata attributes, with conflicting carriers rejected. */
export function homeClientMetadata(raw, headers) {
  const url = new URL(raw);
  need(url.origin === ORIGIN && !url.username && !url.password && !url.hash && RECORD(headers));
  const values = {}, accepted = new Set(['client', 'deviceid', 'device', 'version']);
  const put = (key, value) => {
    if (!accepted.has(key)) return;
    need(text(value) && (!Object.hasOwn(values, key) || values[key] === value)); values[key] = value;
  };
  const names = { 'x-emby-client': 'client', 'x-emby-device-id': 'deviceid', 'x-emby-device-name': 'device', 'x-emby-client-version': 'version' };
  const seen = new Set();
  for (const [key, value] of Object.entries(headers)) {
    const lower = key.toLowerCase(); need(!seen.has(lower)); seen.add(lower);
    if (names[lower]) put(names[lower], value);
    if (!['authorization', 'x-emby-authorization'].includes(lower)) continue;
    need(typeof value === 'string' && value.length <= 16384);
    const prefix = /^(?:Emby|MediaBrowser)\s+(.+)$/i.exec(value); need(prefix);
    let rest = prefix[1], count = 0;
    while (rest) {
      const attribute = /^\s*([A-Za-z][A-Za-z0-9_-]*)\s*=\s*(?:"([^"\\\r\n]*)"|([^,\s]+))\s*(?:,\s*|$)/.exec(rest);
      need(attribute && ++count <= 32); put(attribute[1].toLowerCase(), attribute[2] ?? attribute[3]); rest = rest.slice(attribute[0].length);
    }
  }
  for (const [key, value] of url.searchParams) if (names[key.toLowerCase()]) put(names[key.toLowerCase()], value);
  need(Object.keys(values).length === 4);
  return { client_name: values.client, device_id: values.deviceid, device_name: values.device, client_version: values.version };
}

function headerObject(raw) {
  need(Array.isArray(raw) && raw.length <= 128 && raw.length % 2 === 0);
  const result = {};
  for (let index = 0; index < raw.length; index += 2) {
    const key = raw[index].toLowerCase(); need(!Object.hasOwn(result, key)); result[key] = raw[index + 1];
  }
  return result;
}

export function homeLoginEvidence(value, wire, account, serverId, existingDevices, started, now, existingSessions = [], existingTokens = []) {
  need(RECORD(value) && RECORD(value.User) && RECORD(value.SessionInfo) && value.ServerId === serverId);
  const user = value.User, session = value.SessionInfo;
  need(user.Id === account.id && user.Name === account.username && user.ServerId === serverId && user.HasPassword === true &&
    user.Policy?.IsAdministrator === false && user.Policy?.IsDisabled === false && session.ServerId === serverId &&
    session.UserId === account.id && session.UserName === account.username && ID.test(session.Id));
  need(same({ client_name: session.Client, device_id: session.DeviceId, device_name: session.DeviceName,
    client_version: session.ApplicationVersion }, wire) && Array.isArray(existingDevices) && !existingDevices.includes(wire.device_id));
  need(utc(session.LastActivityDate) && Date.parse(session.LastActivityDate) >= started - 1000 && Date.parse(session.LastActivityDate) <= now + 1000);
  need(typeof value.AccessToken === 'string' && value.AccessToken.length >= 16 && value.AccessToken.length <= 4096 &&
    !/[\x00-\x20\x7f-\uffff,"\\]/.test(value.AccessToken));
  need(!existingSessions.includes(session.Id) && !existingTokens.includes(sha(value.AccessToken)), 'home_login_not_new');
  return { token: value.AccessToken, proof: { token_sha256: sha(value.AccessToken), session_id: session.Id,
    user_id: account.id, ...wire, server_id: serverId, created_at: session.LastActivityDate,
    created_at_source: 'SessionInfo.LastActivityDate', kind: 'emby', slot: 'B',
    frame_login_finished: true, physical_login_completed: true, request_metadata_matches: true } };
}

/** Only approved library identities and names leave the bounded response parser. */
export function projectHomeViews(bytes, expected) {
  need(Buffer.isBuffer(bytes) && bytes.length > 0 && bytes.length <= 512 * 1024);
  const value = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes));
  need(RECORD(value) && Array.isArray(value.Items) && value.Items.length <= 16);
  const items = value.Items.map(item => {
    need(RECORD(item) && ID.test(item.Id));
    const known = expected.find(library => library.id === item.Id);
    return { Id: item.Id, Name: known && item.Name === known.name ? item.Name : null, name_matches: Boolean(known && item.Name === known.name) };
  });
  const count = Object.hasOwn(value, 'TotalRecordCount') ? value.TotalRecordCount : null;
  need(count === null || Number.isSafeInteger(count) && count >= 0 && count <= 10000);
  return { Items: items, TotalRecordCount: count, total_record_count_present: Object.hasOwn(value, 'TotalRecordCount'),
    body_bytes: bytes.length, body_sha256: sha(bytes), matches_expected: items.length === 4 && count === 4 &&
      items.every(item => item.name_matches) && same(items.map(item => item.Id).sort(), expected.map(item => item.id).sort()) };
}

function selectedRoute(raw, method) {
  const url = new URL(raw);
  if (url.origin !== ORIGIN || url.username || url.password || url.hash) return null;
  const pathname = url.pathname.replace(/^\/emby(?=\/)/i, '');
  return method === 'POST' && /^\/Users\/AuthenticateByName\/?$/i.test(pathname) ? 'login'
    : method === 'GET' && pathname === `/Users/${USER}/Views` ? 'views' : null;
}

/** In-memory correlation uses exact request URL digests, token equality and terminal events. */
export class HomeViewsObserver {
  constructor({ account, expected, existingDevices, existingSessions = [], existingTokens = [], serverId, started, persistLogin, onProven, now = () => Date.now() }) {
    Object.assign(this, { account, expected, existingDevices, existingSessions, existingTokens, serverId, started, persistLogin, onProven, now });
    this.phase = 'initial'; this.frames = []; this.physical = []; this.frameMap = new WeakMap(); this.capabilities = [];
    this.bound = null; this.binding = null; this.owned = null; this.privateDescriptor = null;
  }
  hooks() {
    return Object.fromEntries(['admitLogin', 'physicalRequest', 'physicalResponse', 'physicalFinished', 'frameRequest',
      'frameResponse', 'frameFinished'].map(name => [name, this[name].bind(this)]));
  }
  admitLogin(value) {
    const wire = homeClientMetadata(value.url, headerObject(value.headers));
    need(!this.existingDevices.includes(wire.device_id), 'home_device_not_fresh');
    return true;
  }
  physicalRequest(value, bytes) {
    need(this.physical.length < 25 && ['login', 'views', 'capabilities'].includes(value.kind));
    const headers = headerObject(value.headers), url = new URL(value.url);
    const entry = { id: value.id, kind: value.kind, stage: this.phase, phase: value.phase, method: value.method,
      request_sha256: sha(value.method + '\n' + url.href), request_elapsed_ms: value.elapsed_ms,
      status: null, completed: false, terminal: null };
    if (value.kind === 'login') entry.wire = homeClientMetadata(value.url, headers);
    else {
      const authority = authorityForURL(url, headers); need(authority); entry.token_sha256 = authority.fingerprint;
    }
    this.physical.push(entry);
    if (value.kind === 'capabilities') {
      need(this.capabilities.length < 16 && Buffer.isBuffer(bytes) && bytes.length <= 65536);
      const body = new TextDecoder('utf-8', { fatal: true }).decode(bytes);
      const full = /\/Capabilities\/Full$/.test(url.pathname), query = {};
      for (const [key, value] of url.searchParams) {
        const name = key.toLowerCase();
        if (!['playablemediatypes', 'supportedcommands', 'supportsmediacontrol', 'supportssync'].includes(name)) continue;
        need(!Object.hasOwn(query, name) && value.length <= 16384); query[name] = value;
      }
      if (full) need(RECORD(JSON.parse(body)));
      this.capabilities.push({ physical_id: value.id, kind: full ? 'full' : 'query', request_elapsed_ms: value.elapsed_ms,
        response_elapsed_ms: null, finished_elapsed_ms: null, token_matches_session: false, status: null, completed: false,
        body_utf8: body, body_sha256: sha(bytes), query });
    }
  }
  physicalResponse(value, bytes) {
    const entry = this.physical.find(item => item.id === value.id); need(entry && entry.status === null);
    entry.status = value.status; entry.response_elapsed_ms = value.elapsed_ms;
    if (entry.kind === 'views' && value.status === 200) entry.projection = projectHomeViews(bytes, this.expected);
    if (entry.kind === 'login' && value.status === 200) {
      need(bytes.length > 0 && bytes.length <= 512 * 1024);
      const body = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes));
      entry.login = homeLoginEvidence(body, entry.wire, this.account, this.serverId, this.existingDevices, this.started, this.now(),
        this.existingSessions, this.existingTokens);
    }
    const caps = this.capabilities.find(item => item.physical_id === value.id);
    if (caps) Object.assign(caps, { status: value.status, response_elapsed_ms: value.elapsed_ms });
  }
  physicalFinished(value) {
    const entry = this.physical.find(item => item.id === value.id); need(entry && !entry.terminal);
    Object.assign(entry, { completed: value.outcome === 'completed', terminal: value.outcome, finished_elapsed_ms: value.elapsed_ms,
      terminal_status: value.status, response_bytes: value.response_bytes });
    const caps = this.capabilities.find(item => item.physical_id === value.id);
    if (caps) Object.assign(caps, { completed: entry.completed, finished_elapsed_ms: value.elapsed_ms });
    return this.bindLogin();
  }
  frameRequest(value) {
    const request = value.request, kind = selectedRoute(request.url(), request.method());
    if (!kind || request.serviceWorker() !== null) return;
    need(this.frames.length < 12);
    if (kind === 'login') need(!this.frames.some(entry => entry.kind === 'login'), 'home_second_login_observed');
    const entry = { index: value.index, kind, stage: this.phase, phase: value.phase,
      request_sha256: sha(request.method() + '\n' + new URL(request.url()).href), request_elapsed_ms: value.elapsed_ms,
      status: null, finished: false, failed: false, from_service_worker: null };
    this.frames.push(entry); this.frameMap.set(request, entry);
    return (async () => {
      const headers = await bounded(request.allHeaders(), 1500);
      if (kind === 'views') {
        const authority = observedAuthority(request.url(), headers, USER); need(authority); entry.token_sha256 = authority.fingerprint;
      } else entry.wire = homeClientMetadata(request.url(), headers);
      return this.bindLogin();
    })();
  }
  frameResponse(value) {
    const entry = this.frameMap.get(value.response.request());
    if (!entry) return;
    entry.status = value.response.status(); entry.from_service_worker = value.response.fromServiceWorker();
    const mime = (value.response.headers()['content-type'] ?? '').split(';', 1)[0].trim().toLowerCase();
    entry.content_type = ['application/json', 'text/plain', 'text/html'].includes(mime) ? mime : 'other';
    entry.response_elapsed_ms = value.elapsed_ms;
  }
  frameFinished(value) {
    const entry = this.frameMap.get(value.request); if (!entry) return;
    entry.finished = !value.failed; entry.failed = value.failed; entry.finished_elapsed_ms = value.elapsed_ms;
    return this.bindLogin();
  }
  bindLogin() {
    if (this.binding) return this.binding;
    const frames = this.frames.filter(entry => entry.kind === 'login'), transfers = this.physical.filter(entry => entry.kind === 'login');
    need(frames.length <= 1 && transfers.length <= 1, 'home_second_login_observed');
    const frame = frames[0], transfer = transfers[0];
    if (!frame?.finished || !transfer?.completed || !frame.wire || !transfer.login) return;
    need(!frame.failed && frame.status === 200 && frame.from_service_worker === false && transfer.terminal_status === 200 &&
      frame.content_type === 'application/json' && frame.request_sha256 === transfer.request_sha256 &&
      same(frame.wire, transfer.wire) && frame.stage === 'initial' && transfer.stage === 'initial');
    this.owned = transfer.login;
    this.binding = (async () => {
      // Owned UI cleanup remains possible if the private journal fails after proof.
      await this.onProven(this.owned);
      this.privateDescriptor = await this.persistLogin(this.owned);
      this.bound = this.owned.proof;
      return this.bound;
    })();
    return this.binding;
  }
  viewEvidence(stage) {
    const frames = this.frames.filter(entry => entry.kind === 'views' && entry.stage === stage);
    const physical = this.physical.filter(entry => entry.kind === 'views' && entry.stage === stage);
    const pairs = frames.map(frame => {
      const matches = physical.filter(item => item.request_sha256 === frame.request_sha256 && item.token_sha256 === frame.token_sha256);
      const unique = matches.length === 1 && frames.filter(item => item.request_sha256 === frame.request_sha256).length === 1;
      const transfer = unique ? matches[0] : null;
      return { frame: clone(frame), physical: transfer ? this.safePhysical(transfer) : null, unambiguous: unique,
        complete: Boolean(unique && frame.finished && !frame.failed && frame.status === 200 && frame.from_service_worker === false && frame.content_type === 'application/json' &&
          transfer.completed && transfer.terminal_status === 200 && transfer.projection?.matches_expected &&
          this.bound && frame.token_sha256 === this.bound.token_sha256 && transfer.token_sha256 === this.bound.token_sha256) };
    });
    return { stage, result: frames.length === 0 ? 'not_observed' : frames.length <= 4 && physical.length === frames.length &&
      pairs.every(pair => pair.complete) ? 'passed' : 'failed', frame_count: frames.length, physical_count: physical.length, pairs };
  }
  safePhysical(entry) {
    return Object.fromEntries(Object.entries(entry).filter(([key]) => !['wire', 'login'].includes(key)).map(([key, value]) => [key, clone(value)]));
  }
  safeEvidence() {
    return { frames: this.frames.map(entry => Object.fromEntries(Object.entries(entry).filter(([key]) => key !== 'wire'))),
      physical: this.physical.map(entry => this.safePhysical(entry)) };
  }
  privateCapabilities() {
    need(this.bound);
    for (const entry of this.capabilities) {
      const transfer = this.physical.find(item => item.id === entry.physical_id);
      entry.token_matches_session = transfer.token_sha256 === this.bound.token_sha256;
      need(entry.token_matches_session);
    }
    return clone(this.capabilities);
  }
  dispose() {
    this.disposed = true;
    for (const entry of this.physical) { if (entry.login) entry.login.token = null; delete entry.login; delete entry.wire; }
    for (const entry of this.frames) delete entry.wire;
    for (const entry of this.capabilities) { entry.body_utf8 = null; entry.query = null; }
    if (this.owned) this.owned.token = null;
    this.account.password = null;
  }
}

/** Poll finitely; a Promise.race timeout must not leave an uncancelled inner loop. */
export async function waitHomeLoginProof(observer, { timeoutMs = 5000, now = () => Date.now(),
  wait = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds)), settle = async () => {} } = {}) {
  need(Number.isSafeInteger(timeoutMs) && timeoutMs > 0 && timeoutMs <= 15000);
  const deadline = now() + timeoutMs;
  while (!observer.bound) {
    need(observer.phase !== 'cleanup' && observer.disposed !== true, 'home_login_wait_cancelled');
    need(now() < deadline, 'home_login_proof_timeout');
    await settle();
    if (observer.bound) break;
    const remaining = deadline - now(); need(remaining > 0, 'home_login_proof_timeout');
    await wait(Math.min(25, remaining));
  }
  return observer.bound;
}

export function homeCardEvidence(library, titles, cards) {
  need(Number.isSafeInteger(titles) && titles >= 0 && titles <= 64 && Array.isArray(cards) && cards.length <= 64);
  const card = cards.length === 1 ? cards[0] : null;
  const ids = card?.ids ?? [];
  need(Array.isArray(ids) && ids.length <= 4 && ids.every(value => typeof value === 'string' && value.length <= 256));
  const idPresent = ids.length > 0;
  return { id: library.id, name: library.name, visible_title_count: titles, visible_card_count: cards.length,
    card_id_present: idPresent, card_id_matches: idPresent ? ids.every(value => value === library.id) : null,
    proof: idPresent ? 'visible_card_id_and_title' : 'unique_visible_title_without_dom_id',
    passed: cards.length === 1 && (idPresent ? titles >= 1 && ids.every(value => value === library.id) : titles === 1) };
}

export async function observeHomeDOM(page, expected) {
  const location = homeNavigationLocation(page.url(), page.url()), libraries = [];
  for (const library of expected) {
    const titles = page.getByText(library.name, { exact: true }).filter({ visible: true });
    const count = await titles.count(); need(count <= 64);
    const cards = titles.locator('xpath=ancestor-or-self::*[(self::button or self::a or @role="button") and @data-action="link" and ancestor::*[contains(concat(" ", normalize-space(@class), " "), " card ") or contains(concat(" ", normalize-space(@class), " "), " cardBox ")]][1]').filter({ visible: true });
    need(await cards.count() <= 64);
    const observed = await cards.evaluateAll(elements => elements.map(element => {
      const ancestors = [element, element.closest('.card'), element.closest('.cardBox')].filter(Boolean);
      return { ids: [...new Set(ancestors.map(item => item.getAttribute('data-id')).filter(value => value !== null))] };
    }));
    libraries.push(homeCardEvidence(library, count, observed));
  }
  const controls = {};
  for (const label of ['Home', 'Refresh', 'Reload']) {
    controls[label.toLowerCase()] = { links: await page.getByRole('link', { name: label, exact: true }).filter({ visible: true }).count(),
      buttons: await page.getByRole('button', { name: label, exact: true }).filter({ visible: true }).count() };
  }
  need(Object.values(controls).every(value => [value.links, value.buttons].every(count => Number.isSafeInteger(count) && count >= 0 && count <= 64)));
  const activeMedia = await page.locator('audio,video').evaluateAll(elements => elements.some(element =>
    !element.paused && !element.ended || element.currentTime > 0));
  return { location, libraries, controls, controls_scope: 'Visible exact accessible names Home, Refresh and Reload; unlabeled controls are not inferred',
    media_inactive: !activeMedia, passed: location.same_origin && location.supported_path && location.route === 'home' &&
      libraries.every(library => library.passed) && !activeMedia };
}

async function privateDirectory(filename) {
  const stat = await fs.lstat(filename);
  need(stat.isDirectory() && !stat.isSymbolicLink() && stat.uid === 0 && stat.gid === 0 && (stat.mode & 0o777) === 0o700);
  return { device: stat.dev, inode: stat.ino };
}

async function checkedFile(filename, expected, maximum = 2 * 1024 * 1024, json = true, privateOnly = true) {
  need(filename.startsWith(WORK + '/') && path.posix.normalize(filename) === filename && !filename.includes('\\'));
  let parent = path.posix.dirname(filename);
  while (parent.startsWith(WORK)) {
    const stat = await fs.lstat(parent);
    need(stat.isDirectory() && !stat.isSymbolicLink() && stat.uid === 0 && (stat.mode & 0o022) === 0);
    if (parent === WORK) break; parent = path.posix.dirname(parent);
  }
  const handle = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const before = await handle.stat();
    need(before.isFile() && before.nlink === 1 && before.uid === 0 && before.gid === 0 && before.size > 0 && before.size <= maximum &&
      (privateOnly ? (before.mode & 0o777) === 0o600 : [0o600, 0o644, 0o700, 0o755].includes(before.mode & 0o777)));
    const bytes = await handle.readFile(), after = await handle.stat(), named = await fs.lstat(filename);
    need(before.dev === after.dev && before.ino === after.ino && before.size === after.size && before.mtimeMs === after.mtimeMs &&
      after.dev === named.dev && after.ino === named.ino && !named.isSymbolicLink() && bytes.length === before.size && sha(bytes) === expected);
    const value = json ? JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes)) : null;
    bytes.fill(0); return value;
  } finally { await handle.close(); }
}

async function writeExclusive(filename, value) {
  need(path.posix.dirname(filename) === OUTPUT && /^[a-z-]+\.json$/.test(path.posix.basename(filename)));
  await privateDirectory(OUTPUT);
  const bytes = Buffer.from(JSON.stringify(value, null, 2) + '\n');
  const handle = await fs.open(filename, constants.O_CREAT | constants.O_EXCL | constants.O_WRONLY | constants.O_NOFOLLOW, 0o600);
  try { await handle.writeFile(bytes); await handle.sync(); } finally { await handle.close(); }
  const directory = await fs.open(OUTPUT, constants.O_RDONLY | constants.O_DIRECTORY);
  try { await directory.sync(); } finally { await directory.close(); }
  const result = { path: filename, sha256: sha(bytes) }; bytes.fill(0); return result;
}

export function procStartTicks(raw) {
  need(typeof raw === 'string' && raw.length <= 8192 && raw.lastIndexOf(') ') > 0);
  const fields = raw.slice(raw.lastIndexOf(') ') + 2).trim().split(/\s+/);
  need(fields.length >= 20 && /^[1-9]\d*$/.test(fields[19])); return fields[19];
}

async function processIdentity(input) {
  need(process.platform === 'linux' && process.getuid() === 0 && process.getgid() === 0 && !process.env.DEBUG && !process.env.PWDEBUG);
  const boot = (await fs.readFile('/proc/sys/kernel/random/boot_id', 'utf8')).trim();
  need(boot === input.controller.boot_id && procStartTicks(await fs.readFile(`/proc/${input.controller.pid}/stat`, 'utf8')) === input.controller.start_ticks);
  const cgroups = (await fs.readFile('/proc/self/cgroup', 'utf8')).trim().split('\n');
  need(cgroups.some(line => /^\d+:[^:]*:/.test(line) && line.split(':')[2] === CGROUP));
  const executable = await fs.readlink('/proc/self/exe');
  need(path.posix.isAbsolute(executable) && !executable.endsWith(' (deleted)'));
  const handle = await fs.open('/proc/self/exe', constants.O_RDONLY);
  let executableSHA;
  try {
    const stat = await handle.stat(); need(stat.isFile() && stat.size > 0 && stat.size <= 256 * 1024 * 1024 && stat.uid === 0 && (stat.mode & 0o022) === 0);
    executableSHA = sha(await handle.readFile());
  } finally { await handle.close(); }
  return { pid: process.pid, start_ticks: procStartTicks(await fs.readFile('/proc/self/stat', 'utf8')), boot_id: boot,
    uid: process.getuid(), gid: process.getgid(), executable_path: executable, executable_sha256: executableSHA, cgroup: CGROUP };
}

function metadataWithoutCapture(database) {
  const metadata = clone(database.metadata); need(RECORD(metadata) && typeof metadata.captured_at === 'string' && Number.isFinite(Date.parse(metadata.captured_at)));
  delete metadata.captured_at; return metadata;
}

export function bindHomeBaseline(input, before, current) {
  need(RECORD(before?.database) && RECORD(current?.database) && RECORD(before.database.tables));
  const a = before.database, b = current.database;
  need(same(a.tables, b.tables) && same(a.sequences, b.sequences) && same(a.catalog, b.catalog) &&
    same(metadataWithoutCapture(a), metadataWithoutCapture(b)), 'home_baseline_not_continuous');
  need(Object.keys(a.tables).length === 35 && a.tables.items.length === 22 && a.tables.libraries.length === 4 &&
    a.tables.sessions.length === 70 && a.tables.devices.length === 60 && a.tables.activity_entries.length === 155 &&
    a.tables.play_sessions.length === 26 && a.tables.user_item_data.length === 7 && a.tables.client_playback_references.length === 0 && a.tables.encoding_jobs.length === 0);
  need(same(a.tables.libraries.map(row => ({ id: row.id, name: row.name })).sort((x, y) => x.id.localeCompare(y.id)),
    [...input.expected_libraries].sort((x, y) => x.id.localeCompare(y.id))));
  const user = a.tables.users.find(row => row.id === USER);
  need(user && user.is_administrator === false && user.is_disabled === false && user.management_revision === 3);
  const failedSessions = a.tables.sessions.filter(row => row.id === '00fb0b833884308ab946e1c40ff6abdd');
  const recoverySessions = a.tables.sessions.filter(row => row.id === 'ad6695676912f2bd2a1bf68b9cfdd9ce');
  const priorHomeSessions = a.tables.sessions.filter(row => row.id === '2e355f661132b4ba3a3ed1445a16c3c8');
  need(failedSessions.length === 1 && recoverySessions.length === 1 && failedSessions[0].user_id === USER && failedSessions[0].kind === 'emby' &&
    failedSessions[0].token_hash === '\\x9f71d2771f1a0d177041d6815f46d39ecea9a54bf92099a13b89dbef172afb81' && recoverySessions[0].kind === 'admin' &&
    a.tables.users.some(row => row.id === recoverySessions[0].user_id && row.is_administrator === true) &&
    [...failedSessions, ...recoverySessions].every(row => typeof row.revoked_at === 'string' && Number.isFinite(Date.parse(row.revoked_at))));
  need(priorHomeSessions.length === 1 && priorHomeSessions[0].user_id === USER && priorHomeSessions[0].kind === 'emby' &&
    priorHomeSessions[0].token_hash === '\\xe795b8ecf91e9a2b934b28484ce80a9c52e40838389473032e954cbf699c3eb3' &&
    typeof priorHomeSessions[0].revoked_at === 'string' && Number.isFinite(Date.parse(priorHomeSessions[0].revoked_at)));
  need(Date.parse(a.metadata.captured_at) >= Date.parse(b.metadata.captured_at));
  return a.tables.devices.map(row => { need(text(row.reported_device_id)); return row.reported_device_id; });
}

export function homeObservationPassed(report) {
  const actor = report.actor, closure = report.closure, proof = actor?.session_proof;
  const logins = report.observation?.frames?.filter(entry => entry.kind === 'login') ?? [];
  const transfers = report.observation?.physical?.filter(entry => entry.kind === 'login') ?? [];
  return Boolean(report.initial?.views?.result === 'passed' && report.reload?.views?.result === 'passed' &&
    report.initial.dom?.passed && report.reload.dom?.passed && report.reload.action?.count === 1 && report.reload.action.completed === true &&
    report.login_proof && report.session_private && report.capabilities_private && logins.length === 1 && transfers.length === 1 &&
    logins[0].finished && !logins[0].failed && logins[0].status === 200 && logins[0].from_service_worker === false &&
    logins[0].content_type === 'application/json' && transfers[0].completed && transfers[0].terminal_status === 200 &&
    logins[0].request_sha256 === transfers[0].request_sha256 && actor?.login?.request_count === 1 && actor.login.status === 200 &&
    actor.ordinary_authority_confirmed === true && actor.page_error_count === 0 && actor.closed && actor.logout.status === 204 &&
    actor.logout.login_view_visible && actor.proxy?.login === 1 && actor.proxy.logout === 1 && actor.proxy.preparation === 0 &&
    actor.proxy.failed === 0 && actor.proxy.rejected === 0 && actor.proxy.active === 0 && actor.proxy.completed === actor.proxy.admitted &&
    actor.proxy_logout?.completed === true && actor.proxy_logout.status === 204 &&
    actor.proxy_logout.token_fingerprint === report.login_proof.token_sha256 &&
    Number.isSafeInteger(actor.network.external_blocked) && actor.network.external_blocked >= 0 && actor.network.external_blocked <= 2000 &&
    ['forbidden_mutations', 'playback_attempts', 'observer_errors', 'overflow', 'guard_errors'].every(key => actor.network[key] === 0) &&
    Number.isSafeInteger(actor.console_warning_error_count) && actor.console_warning_error_count >= 0 && actor.console_warning_error_count <= 4000 &&
    actor.cleanup_failures?.length === 0 &&
    proof?.outcome === 'all_observed_logout_tokens_rejected' && proof.entries.length === 1 &&
    proof.entries[0].result === 'logout_token_rejected' && proof.entries[0].token_fingerprint === report.login_proof.token_sha256 &&
    closure?.context_closed && closure.browser_closed && closure.proxy_closed && closure.http_pending === 0 &&
    closure.websocket_pending === 0 && closure.websocket_active === 0 && closure.websocket_opened > 0 &&
    closure.websocket_opened === closure.websocket_closed && closure.sockets_remaining === 0 && closure.cleanup_failures.length === 0 &&
    actor.websocket.failed === 0 && actor.websocket.control_attempts === 0 && !report.failure);
}

/** This is a new run, not a replay of either earlier cross-user entry point. */
export async function runLibraryHome(options) {
  process.umask(0o077);
  const parsed = parseHomeArguments(Object.entries(options).flatMap(([key, value]) => ['--' + key, value]));
  const input = validateHomeInput(await checkedFile(parsed.input, parsed['input-sha256']));
  await privateDirectory(ROOT); const node = await processIdentity(input);
  const closureDigest = homeSourceDigest(input.source_closure);
  need(Object.hasOwn(input.source_closure, SELF));
  for (const name of SOURCES) need(Object.hasOwn(input.source_closure, path.posix.join(path.posix.dirname(SELF), name)));
  for (const [filename, digest] of Object.entries(input.source_closure)) await checkedFile(filename, digest, 2 * 1024 * 1024, false, false);
  for (const source of Object.values(input.fixture)) await checkedFile(source.path, source.sha256, 2 * 1024 * 1024, false);
  const api = await checkedFile(input.authority.api_report.path, input.authority.api_report.sha256);
  const inspection = await checkedFile(input.authority.inspection.path, input.authority.inspection.sha256);
  const recovery = await checkedFile(input.authority.recovery.path, input.authority.recovery.sha256);
  const priorHome = await checkedFile(input.authority.prior_home.path, input.authority.prior_home.sha256);
  validateHomeAuthority(input, api, inspection, recovery, priorHome);
  await checkedFile(RECOVERY_AFTER.path, RECOVERY_AFTER.sha256, 64 * 1024 * 1024, false);
  const current = await checkedFile(input.authority.current_snapshot.path, input.authority.current_snapshot.sha256, 64 * 1024 * 1024);
  const before = await checkedFile(input.authority.before_snapshot.path, input.authority.before_snapshot.sha256, 64 * 1024 * 1024);
  const existingDevices = bindHomeBaseline(input, before, current);
  need(Date.now() - Date.parse(before.database.metadata.captured_at) <= 180000 && Date.now() >= Date.parse(before.database.metadata.captured_at));
  const credentials = await checkedFile(input.actor.credentials.path, input.actor.credentials.sha256);
  need(credentials.marker === 'goby-m3e-client-acceptance-v1' && credentials.base_url === ORIGIN &&
    credentials.direct_url === input.candidate.direct_url && credentials.viewer?.username === 'm3e-client-viewer' && /^[0-9a-f]{48}$/.test(credentials.viewer.password));
  const account = { slot: 'B', id: USER, username: credentials.viewer.username, password: credentials.viewer.password,
    credentialsPath: input.actor.credentials.path, credentialsSHA: input.actor.credentials.sha256 };
  const fixture = await loadSpecialFeaturesFixture({ candidateSHA256: input.candidate.binary_sha256, source: input.candidate.source,
    sourceManifestSHA256: input.candidate.source_manifest_sha256, receiptSHA256: input.fixture.profile_receipt.sha256,
    reportSHA256: input.fixture.profile_report.sha256, inspectSHA256: input.fixture.profile_inspection.sha256,
    musicChainPath: input.fixture.music_chain.path, musicChainSHA256: input.fixture.music_chain.sha256,
    musicScanReceiptSHA256: input.fixture.music_scan_receipt.sha256 });
  need(fixture.serverId === SERVER && fixture.evidence.fixture_state_sha256 === input.candidate.state_sha256 &&
    same(fixture.evidence.process, input.candidate.process) && same([...Object.values(fixture.evidence.libraries), fixture.library.id].sort(), [...LIBRARIES].sort()));
  await fs.mkdir(OUTPUT, { mode: 0o700 }); await privateDirectory(OUTPUT);
  const started = Date.now(), secrets = [account.password];
  const report = { marker: 'goby-client-library-home-report-v1', version: 1, mode: input.mode, result: 'failed', outcome: 'failed',
    client_acceptance: false, permission_ui_acceptance: false, input_sha256: parsed['input-sha256'], source_closure_sha256: closureDigest,
    candidate: clone(input.candidate), controller: clone(input.controller), node_process: node, started_at: new Date(started).toISOString(),
    authority: clone(input.authority),
    login_proof: null, session_private: null, capabilities_private: null, initial: null, reload: null, actor: {}, closure: null, failure: null,
    limits: { work_ms: 180000, cleanup_ms: 90000, login_requests: 1, reloads: 1, views_per_stage: 4, selected_frame_events: 12,
      selected_physical_requests: 25, response_projection_bytes: 512 * 1024, capabilities_requests: 16, capabilities_body_bytes: 65536 },
    boundary: 'B-only Home and one explicit browser reload; no policy change, Movie navigation, media request or PlaybackInfo preparation' };
  let actor, cleanupDeadline = null;
  const observer = new HomeViewsObserver({ account, expected: input.expected_libraries, existingDevices,
    existingSessions: before.database.tables.sessions.map(row => row.id),
    existingTokens: before.database.tables.sessions.map(row => { need(typeof row.token_hash === 'string' && /^\\x[0-9a-f]{64}$/.test(row.token_hash)); return row.token_hash.slice(2); }),
    serverId: SERVER, started,
    onProven: ({ token, proof }) => {
      need(actor.token === null || actor.token === token); actor.token = token; actor.proven = true;
      actor.report.token_fingerprint = proof.token_sha256; actor.report.ordinary_authority_confirmed = true;
      report.login_proof = clone(proof); secrets.push(token);
    },
    persistLogin: async ({ token, proof }) => {
      const source = await writeExclusive(OUTPUT + '/session-private.json', { marker: 'goby-client-library-home-session-v1', version: 1,
        input_sha256: report.input_sha256, source_closure_sha256: closureDigest, node_process: node, controller: input.controller, proof, token });
      report.session_private = source; return source;
    } });
  const pin = async () => {
    need(cleanupDeadline === null ? Date.now() - started <= 180000 : Date.now() <= cleanupDeadline, 'home_run_deadline');
    await checkedFile(parsed.input, parsed['input-sha256'], 2 * 1024 * 1024, false);
    need(same(await processIdentity(input), node)); await fixture.assertPinned();
  };
  actor = createHomeOnlyBrowserActor({ account, pin, report: report.actor, observer: observer.hooks() });
  report.started_at = new Date(actor.started).toISOString();
  report.elapsed_clock = 'All browser frame, forwarding, page diagnostics and action offsets use the actor start time';
  actor.diagnosticSecrets = () => secrets;
  actor.rememberSecret = value => { if (!secrets.includes(value)) secrets.push(value); };
  actor.authorizeSocket = async () => {
    await waitHomeLoginProof(observer);
    need(observer.bound.token_sha256 === sha(actor.token) && report.session_private);
  };
  async function observeStage(stage) {
    actor.phase = 'ui_home'; const deadline = Date.now() + 20000;
    let dom, views;
    do {
      await actor.settled(); dom = await observeHomeDOM(actor.page, input.expected_libraries); views = observer.viewEvidence(stage);
      report[stage] = { ...(report[stage] ?? {}), dom, views };
      if (dom.passed && views.result === 'passed') break;
      if (actor.report.page_error_count > 0) break;
      await actor.page.waitForTimeout(100);
    } while (Date.now() < deadline);
    need(dom?.passed && views?.result === 'passed', views?.result === 'not_observed' ? 'home_views_not_observed' : 'home_stage_incomplete');
    await actor.proxyIdle(); await actor.settled(); await actor.assertPinned();
    report[stage].views = observer.viewEvidence(stage); need(report[stage].views.result === 'passed');
  }
  try {
    await actor.open(); await waitHomeLoginProof(observer, { settle: () => actor.settled() });
    need(observer.bound && report.session_private, 'home_login_proof_incomplete');
    await observeStage('initial');
    observer.phase = 'reload'; report.reload = { action: { kind: 'page.reload', count: 1, completed: false, status: null,
      invoked_elapsed_ms: Date.now() - actor.started } };
    actor.phase = 'ui_home'; await actor.assertPinned();
    const response = await actor.page.reload({ waitUntil: 'domcontentloaded', timeout: 30000 });
    report.reload.action.completed = true; report.reload.action.status = response?.status() ?? null;
    report.reload.action.completed_elapsed_ms = Date.now() - actor.started;
    need(actor.report.proxy.login === 1 && actor.token && sha(actor.token) === observer.bound.token_sha256);
    await observeStage('reload'); actor.report.ui.outcome = 'passed';
  } catch (error) {
    report.failure = ['home_views_not_observed', 'home_stage_incomplete', 'home_login_proof_incomplete'].includes(error?.message)
      ? error.message : 'home_flow_failed';
    report.failure_diagnostic = safeBrowserFailure(error, actor.phase);
  } finally {
    observer.phase = 'cleanup';
    cleanupDeadline = Date.now() + 90000;
    try { await actor.close(); } catch { report.failure ??= 'home_close_failed'; }
    report.closure = actor.report.home_closure ?? null;
    report.login_proof = observer.bound ?? report.login_proof;
    report.observation = observer.safeEvidence();
    for (const stage of ['initial', 'reload']) {
      if (report[stage]) report[stage].views = observer.viewEvidence(stage);
    }
    if (observer.bound) {
      try {
        const entries = observer.privateCapabilities();
        const source = await writeExclusive(OUTPUT + '/capabilities-private.json', { marker: 'goby-client-library-home-capabilities-v1', version: 1,
          input_sha256: report.input_sha256, source_closure_sha256: closureDigest, node_process: node,
          session_id: observer.bound.session_id, token_sha256: observer.bound.token_sha256, entries });
        const successful = entries.filter(entry => entry.completed && entry.status === 204);
        report.capabilities_private = { ...source, request_count: entries.length,
          last_successful_body_sha256: successful.at(-1)?.body_sha256 ?? null };
        need(entries.length === actor.report.proxy.capabilities && entries.every(entry => entry.completed && entry.status === 204));
      } catch { report.failure ??= 'home_capabilities_evidence_failed'; }
    }
    resanitizeBrowserDiagnostics({ accounts: [actor.report] }, secrets);
    report.completed_at = new Date().toISOString();
    report.result = homeObservationPassed(report) ? 'passed' : 'failed';
    report.outcome = report.result === 'passed' ? 'baseline_observation' : ['initial', 'reload'].some(stage => report[stage]?.views?.result === 'not_observed') ? 'not_observed' : 'failed';
    if (report.result === 'failed') report.failure ??= actor.report.page_error_count ? 'page_errors_observed' : 'home_evidence_incomplete';
    const encoded = JSON.stringify(report);
    need(secrets.every(secret => !secret || !encoded.includes(secret)), 'home_report_secret_guard');
    await writeExclusive(OUTPUT + '/report.json', report);
    observer.dispose(); secrets.fill(null); credentials.viewer.password = null;
  }
  return report;
}

if (process.argv[1] && path.resolve(process.argv[1]) === SELF) {
  try {
    const report = await runLibraryHome(parseHomeArguments(process.argv.slice(2)));
    process.stdout.write(JSON.stringify({ marker: report.marker, result: report.result, outcome: report.outcome,
      client_acceptance: false, permission_ui_acceptance: false, failure: report.failure }) + '\n');
    if (report.result !== 'passed') process.exitCode = 1;
  } catch {
    process.stdout.write(JSON.stringify({ marker: 'goby-client-library-home-report-v1', result: 'failed',
      failure: 'home_setup_or_terminal_report', client_acceptance: false }) + '\n'); process.exitCode = 1;
  }
}
