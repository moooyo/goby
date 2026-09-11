#!/usr/bin/env node
/** Pure protocol, publication and choreography guards; no browser, network or database. */
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';
import { EventEmitter } from 'node:events';
import { inputFixture as homeInput, passedReport as closedHomeReport } from './test-client-browser-library-home.mjs';
import { observeHomeDOM } from './client-browser-library-home.mjs';
import { websocketHandshakeBudget, forwardWebSocketConnect, createHomeOnlyBrowserActor, createPermissionBrowserActor } from './client-browser-cross-user.mjs';
import { parsePermissionArguments, validatePermissionInput, permissionLibraries, validatePermissionBaseline,
  validatePermissionControl, permissionPublication, permissionPublicationSnapshot, publishPermissionRecord,
  permissionStageRecord, spontaneousPermissionEvidence, PermissionWorkflow, permissionObservationPassed } from './client-browser-library-permission.mjs';

const WORK = '/opt/goby-test/exec-work-m3e';
const ROOT = WORK + '/client-library-permission-ui-v1', OUTPUT = ROOT + '/browser';
const SELF = fileURLToPath(import.meta.url);
const USER = 'ecbbe4cb82403879bc4b4f78894c5738';
const TOKEN = 'synthetic-home-token-0123456789';
const MOVIES = 'a9993591e72f0f2e7babcbf8b9c50790';
const START = Date.parse('2026-09-12T08:00:00.000Z');
const sha = value => createHash('sha256').update(value).digest('hex');
const clone = value => JSON.parse(JSON.stringify(value));
const desc = (name, digest = 'a'.repeat(64)) => ({ path: WORK + '/' + name, sha256: digest });
function check(value) { if (!value) throw new Error('synthetic_permission_assertion'); }
function equal(a, b) { check(JSON.stringify(a) === JSON.stringify(b)); }
function rejects(fn) { let caught = false; try { fn(); } catch { caught = true; } check(caught); }
async function rejectsAsync(fn) { let caught = false; try { await fn(); } catch { caught = true; } check(caught); }
const tests = [];
function test(name, fn) { tests.push({ name, fn }); }

function inputFixture() {
  const input = homeInput();
  Object.assign(input, { marker: 'goby-client-library-permission-input-v1', mode: 'b-home-permission-reload', root: ROOT, output: OUTPUT,
    authority: {
      home_report: desc('client-library-ui-baseline-v3/report.json', 'a8117846d80eeed8e714b8951103632acfc05105d8a14c362e99f0f9d9e62270'),
      home_browser: desc('client-library-ui-baseline-v3/browser/report.json', 'a5e4cd3a16c625c85b459c20c2349375a22bf63d889d7ac2e89f0e9237a91e28'),
      current_snapshot: desc('client-library-ui-baseline-v3/after-full.json', '8095db0dd2e96c7f3a8e194b3f66d71c7366e756b12f1d1f549a19c336acb29a'),
      before_snapshot: desc('client-library-permission-ui-v1/before-full.json'),
    } });
  input.source_closure[WORK + '/synthetic-tool/client-browser-library-permission.mjs'] = 'b'.repeat(64);
  input.controller.unit = 'goby-client-library-permission-ui-v1.service'; return input;
}
function bindings(input = inputFixture()) {
  return { input_sha256: 'b'.repeat(64), source_closure_sha256: 'c'.repeat(64), controller: clone(input.controller),
    node_process: { pid: 999, start_ticks: '1000', boot_id: input.controller.boot_id, uid: 0, gid: 0,
      executable_path: '/synthetic/node', executable_sha256: 'd'.repeat(64), cgroup: '/system.slice/' + input.controller.unit } };
}
function stateFixture() {
  return { input: inputFixture(), revision: '3', stages: [{ name: 'baseline', sha256: '1'.repeat(64), publication_started_at: START + 10 }],
    consumed: [], aborted: false, started_at: START };
}
function controlFixture(name = 'restricted', state = stateFixture(), binding = bindings()) {
  return { marker: 'goby-client-library-permission-control-v1', version: 1, ...clone(binding), name,
    previous_stage_sha256: state.stages.at(-1)?.sha256 ?? null, revision: name === 'restricted' ? '4' : '5',
    write_completed_at: new Date(START + 20).toISOString(), expected_libraries: permissionLibraries(state.input, name === 'restricted'),
    restoration: name === 'restricted' ? 'pending' : 'confirmed' };
}
const session = desc('client-library-permission-ui-v1/browser/session-private.json');

test('new exact input binds the successful v3 authority and no administrator', () => {
  validatePermissionInput(inputFixture());
  for (const change of [value => { value.actor.admin = {}; }, value => { value.root = WORK + '/client-library-ui-baseline-v3'; },
    value => { value.authority.current_snapshot.sha256 = '0'.repeat(64); }, value => { delete value.authority.home_browser; },
    value => { value.expected_libraries.pop(); }, value => { value.controller.unit = 'goby-client-library-ui-baseline-v3.service'; }]) {
    const input = inputFixture(); change(input); rejects(() => validatePermissionInput(input));
  }
});
test('CLI cannot enter a consumed Home output', () => {
  parsePermissionArguments(['--input', ROOT + '/input.json', '--input-sha256', 'a'.repeat(64), '--output', OUTPUT]);
  rejects(() => parsePermissionArguments(['--input', ROOT + '/input.json', '--input-sha256', 'a'.repeat(64), '--output', WORK + '/client-library-ui-baseline-v3/browser']));
});
test('restriction removes only original Movies and retains Extras, Music and TV', () => {
  const input = inputFixture(), restricted = permissionLibraries(input, true);
  equal(restricted.length, 3); check(!restricted.some(item => item.id === MOVIES));
  check(restricted.some(item => item.id === '57a85c1ca5b6c7ae602c587755250b2f'));
});
test('fresh baseline is continuous with accepted Home v3 and requires 71 authentications', () => {
  const input = inputFixture(), token = '51965e61bf44dfa417dd5d2f545847aed9f0aba4636e0733fcd3e545639c7feb';
  const sid = '83be430b189b6ef0a4f685381dc493b8', browser = closedHomeReport();
  Object.assign(browser, { marker: 'goby-client-library-home-report-v1', result: 'passed', outcome: 'baseline_observation',
    client_acceptance: false, permission_ui_acceptance: false });
  browser.reload.action.kind = 'page.reload'; browser.login_proof = { session_id: sid, token_sha256: token };
  browser.actor.proxy_logout.token_fingerprint = token; browser.actor.session_proof.entries[0].token_fingerprint = token;
  const home = { marker: 'goby-client-library-home-observation-v1', result: 'passed', phase: 'complete', outcome: 'baseline_observation',
    worker_closed: true, fallback_attempted: false, client_acceptance: false, permission_ui_acceptance: false,
    candidate: clone(input.candidate), evidence: { 'browser-report.json': input.authority.home_browser, 'after-full.json': input.authority.current_snapshot },
    proof: { new_b_authentication: 1, new_devices: 1, new_play_userdata_references_encodings: 0, new_session_audits: 2,
      observed_capabilities_bound: true, old_rows_sequences_private_unchanged: true, users_and_policies_unchanged: true, session_id: sid, token_sha256: token } };
  const names = ['activity_entries', 'application_key_clients', 'application_key_devices', 'application_keys', 'catalog_entities', 'client_playback_references',
    'devices', 'encoding_jobs', 'extra_reserved_paths', 'item_entities', 'item_extra_resources', 'item_images', 'item_metadata_state', 'item_subtitles',
    'item_theme_resources', 'items', 'libraries', 'library_roots', 'managed_settings', 'play_sessions', 'scan_jobs', 'schema_migrations', 'server_settings',
    'sessions', 'task_definitions', 'task_occurrences', 'task_run_children', 'task_run_requests', 'task_runs', 'task_triggers', 'theme_owner_ids',
    'theme_reserved_paths', 'user_item_data', 'user_settings', 'users'];
  const tables = Object.fromEntries(names.map(name => [name, []]));
  for (const [name, count] of Object.entries({ activity_entries: 157, devices: 61, items: 22, play_sessions: 26, user_item_data: 7 })) {
    tables[name] = Array.from({ length: count }, (_, index) => ({ id: index + 1, ...(name === 'devices' ? { reported_device_id: 'old-' + index } : {}) }));
  }
  tables.sessions = Array.from({ length: 71 }, (_, index) => ({ id: index.toString(16).padStart(32, '0'), token_hash: '\\x' + sha('old-' + index) }));
  tables.sessions[0] = { id: sid, token_hash: '\\x' + token, user_id: USER, kind: 'emby', revoked_at: new Date(START).toISOString() };
  tables.users = [{ id: USER, management_revision: 3, is_disabled: false, is_administrator: false }]; tables.libraries = clone(input.expected_libraries);
  const current = { database: { tables, catalog: [], sequences: {}, metadata: { captured_at: new Date(START).toISOString() } } };
  const before = clone(current); before.database.metadata.captured_at = new Date(START + 1).toISOString();
  equal(validatePermissionBaseline(input, home, browser, before, current).revision, '3');
  const wrong = clone(current); wrong.database.tables.sessions.pop(); rejects(() => validatePermissionBaseline(input, home, browser, clone(wrong), wrong));
  const failed = clone(home); failed.result = 'failed'; rejects(() => validatePermissionBaseline(input, failed, browser, before, current));
  const mutated = clone(before); mutated.database.tables.user_item_data[0].changed = true;
  rejects(() => validatePermissionBaseline(input, home, browser, mutated, current));
});
test('normal restriction control requires exact input, process, stage, revision and membership', () => {
  const state = stateFixture(), binding = bindings(), control = controlFixture();
  validatePermissionControl(control, binding, state, START + 30);
  for (const change of [value => { value.input_sha256 = '0'.repeat(64); }, value => { value.node_process.start_ticks = '1001'; },
    value => { value.previous_stage_sha256 = '0'.repeat(64); }, value => { value.revision = '3'; },
    value => { value.expected_libraries = permissionLibraries(state.input); }, value => { value.write_completed_at = null; },
    value => { value.extra = true; }]) {
    const bad = clone(control); change(bad); rejects(() => validatePermissionControl(bad, binding, state, START + 30));
  }
});
test('abort cannot authorize restriction or another restriction control', () => {
  const state = stateFixture(); state.aborted = true;
  rejects(() => validatePermissionControl(controlFixture(), bindings(), state, START + 30));
  state.aborted = false; state.consumed = ['restricted']; rejects(() => validatePermissionControl(controlFixture(), bindings(), state, START + 30));
});
test('restored control uses the restricted stage and original four libraries', () => {
  const state = stateFixture(); state.stages.push({ name: 'restricted', sha256: '2'.repeat(64), publication_started_at: START + 15 }); state.consumed = ['restricted'];
  const restored = controlFixture('restored', state); validatePermissionControl(restored, bindings(), state, START + 30);
  restored.restoration = 'pending'; rejects(() => validatePermissionControl(restored, bindings(), state, START + 30));
});
test('failure close accepts confirmed r+2 with a genuinely missing write acknowledgement time', () => {
  const state = stateFixture(); state.aborted = true; state.consumed = ['restricted'];
  const close = controlFixture('close', state); close.write_completed_at = null;
  validatePermissionControl(close, bindings(), state, START + 30);
  state.aborted = false; rejects(() => validatePermissionControl(close, bindings(), state, START + 30));
});
test('normal close needs the final stage and actual restore acknowledgement', () => {
  const state = stateFixture(); state.stages.push({ name: 'restricted', sha256: '2'.repeat(64), publication_started_at: START + 15 },
    { name: 'restored', sha256: '3'.repeat(64), publication_started_at: START + 25 }); state.consumed = ['restricted', 'restored'];
  const close = controlFixture('close', state); validatePermissionControl(close, bindings(), state, START + 30);
  close.write_completed_at = null; rejects(() => validatePermissionControl(close, bindings(), state, START + 30));
});
test('not-required close is cleanup-only before restriction and cannot follow a consumed restriction', () => {
  const state = stateFixture(); state.aborted = true;
  const close = { ...controlFixture('close', state), restoration: 'not_required', revision: null, write_completed_at: null };
  validatePermissionControl(close, bindings(), state, START + 30);
  state.consumed = ['restricted']; rejects(() => validatePermissionControl(close, bindings(), state, START + 30));
});
test('stage records carry the same B token and consumed control digest', () => {
  const binding = { ...bindings(), token_sha256: sha(TOKEN) };
  const baseline = permissionStageRecord('baseline', { dom: {}, views: {} }, binding, session, null);
  const restricted = permissionStageRecord('restricted', { spontaneous: {}, reload: {} }, binding, session, '1'.repeat(64));
  equal(baseline.token_sha256, restricted.token_sha256); equal(restricted.previous_control_sha256, '1'.repeat(64));
  rejects(() => permissionStageRecord('restored', { spontaneous: {}, reload: {} }, binding, session, null));
});

function stat(nlink = 1, extra = {}) { return { isFile: true, symbolic: false, uid: 0, gid: 0, mode: 0o600,
  nlink, size: 100, dev: 2, ino: 30, mtimeMs: 1, ctimeMs: 1, ...extra }; }
test('only a fully published private single-link record is ready', () => {
  equal(permissionPublication(stat(), null), 'ready'); equal(permissionPublication(null, stat()), 'publishing');
  equal(permissionPublication(null, null), 'missing');
  equal(permissionPublication(stat(2), stat(2)), 'publishing');
  rejects(() => permissionPublication(stat(), stat())); rejects(() => permissionPublication(stat(2), stat(2, { ino: 31 })));
  rejects(() => permissionPublication(stat(1, { uid: 9 }), null));
});
test('link and unlink between reader snapshots are retried without parsing', () => {
  equal(permissionPublicationSnapshot(null, stat(2), stat(2)), 'publishing');
  equal(permissionPublicationSnapshot(stat(2), null, stat()), 'publishing');
  equal(permissionPublicationSnapshot(stat(), null, stat()), 'ready');
  rejects(() => permissionPublicationSnapshot(stat(), stat(), stat()));
});
test('publication synchronizes complete bytes and durable final name before removing pending', async () => {
  const events = [], names = new Map(); let bytes;
  const io = {
    open: async (name, flags) => {
      if (name === OUTPUT) return { sync: async () => events.push('directory-sync'), close: async () => {} };
      check(name.endsWith('.pending') && flags !== 0); check(!names.has(name)); names.set(name, stat());
      return { writeFile: async value => { bytes = Buffer.from(value); events.push('write'); }, sync: async () => events.push('file-sync'),
        stat: async () => ({ dev: 2, ino: 30 }), close: async () => events.push('file-close') };
    },
    link: async (pending, final) => { check(bytes.length > 0 && !names.has(final)); events.push('link-exclusive'); names.set(pending, stat(2)); names.set(final, stat(2)); },
    lstat: async name => ({ ...names.get(name), isFile: () => true, isSymbolicLink: () => false }),
    unlink: async name => { events.push('unlink-owned-pending'); names.delete(name); for (const [key, value] of names) names.set(key, { ...value, nlink: 1 }); },
  };
  const result = await publishPermissionRecord(OUTPUT + '/stage-baseline.json', { complete: true }, io);
  equal(events, ['write', 'file-sync', 'file-close', 'link-exclusive', 'directory-sync', 'unlink-owned-pending', 'directory-sync']);
  equal(result.sha256, sha(bytes)); check(!names.has(OUTPUT + '/stage-baseline.json.pending'));
});
test('occupied final name or journal failure never removes a pending record or overwrites final', async () => {
  for (const failAt of ['write', 'link']) {
    let removed = false;
    const io = { open: async () => ({ writeFile: async () => { if (failAt === 'write') throw new Error('synthetic_io'); },
      sync: async () => {}, stat: async () => ({ dev: 2, ino: 30 }), close: async () => {} }),
      link: async () => { throw new Error('synthetic_exists'); }, unlink: async () => { removed = true; } };
    await rejectsAsync(() => publishPermissionRecord(OUTPUT + '/abort.json', { failed: true }, io)); check(!removed);
  }
});

test('spontaneous absence remains not-observed despite a cached correct DOM', () => {
  const control = { write_completed_at: new Date(START + 1000).toISOString() };
  const samples = [{ started_elapsed_ms: 200, elapsed_ms: 220, observation: { passed: false } },
    { started_elapsed_ms: 1200, elapsed_ms: 1220, observation: { passed: true } }];
  const calls = [], observer = { viewEvidence: (stage, window) => { calls.push({ stage, window }); return { result: 'not_observed' }; } };
  const result = spontaneousPermissionEvidence('restricted_passive', control, samples, observer, START, 11000);
  equal(result.outcome, 'not_observed_within_window'); equal(result.window.duration_ms, 10000); equal(result.earlier.dom.length, 1);
  equal(calls[0].window, { from_ms: 1000, until_ms: 11000, completed_by_ms: 11000 });
});
test('a completed fresh response plus correct DOM can be recorded independently as spontaneous', () => {
  const control = { write_completed_at: new Date(START + 1000).toISOString() };
  const result = spontaneousPermissionEvidence('restored_passive', control,
    [{ started_elapsed_ms: 1200, elapsed_ms: 1220, observation: { passed: true } }],
    { viewEvidence: () => ({ result: 'passed' }) }, START, 11000);
  equal(result.outcome, 'observed_correct_membership');
  rejects(() => spontaneousPermissionEvidence('restored_passive', control, [], { viewEvidence: () => ({}) }, START, 10999));
});
test('restricted DOM requires correct retained IDs and independent original Movies card absence', async () => {
  const input = inputFixture(), expected = permissionLibraries(input, true), original = input.expected_libraries.find(item => item.id === MOVIES);
  let visibleOriginal = 0;
  const collection = result => ({ filter() { return this; }, count: async () => result.length, evaluateAll: async () => result });
  const page = { url: () => 'http://127.0.0.1:18196/web/index.html#!/home',
    getByText: name => { const item = input.expected_libraries.find(value => value.name === name); return { filter() { return this; }, count: async () => 2,
      locator: () => collection(item.id === MOVIES ? [] : [{ ids: [item.id] }]) }; },
    getByRole: () => collection([]), locator: selector => {
      if (selector === 'audio,video') return { evaluateAll: async () => false };
      check(selector.includes('[data-action="link"]') && selector.includes(MOVIES));
      return collection(Array(visibleOriginal).fill({}));
    } };
  const absent = await observeHomeDOM(page, expected, { require_ids: true, excluded: [original] });
  check(absent.passed && absent.excluded_libraries[0].absent); equal(absent.excluded_libraries[0].visible_title_count, 2);
  visibleOriginal = 1; check(!(await observeHomeDOM(page, expected, { require_ids: true, excluded: [original] })).passed);
});

class FakeSocket extends EventEmitter { pause() {} destroy() { this.destroyed = true; } }
async function connectAdmission(scope, seen) {
  const state = { seen, active: 0, admitted: 0, failed: 0 }, entry = {}, client = new FakeSocket();
  await forwardWebSocketConnect({ url: '127.0.0.1:18196', method: 'CONNECT', rawHeaders: ['Host', '127.0.0.1:18196'] }, client,
    Buffer.alloc(0), { state, entry, mode: 'acceptance', userId: USER, handshakeScope: scope,
      ownershipLost: () => false, logoutInProgress: () => false, onEvent: () => {},
      connectAllowed: async () => { throw new Error('synthetic_stop_after_admission'); } });
  return { state, entry };
}
test('old Home remains at two handshakes, fixed permission allows three, and the fourth is rejected', async () => {
  equal(websocketHandshakeBudget(), 2); equal(websocketHandshakeBudget('library-permission-ui-v1'), 3);
  rejects(() => websocketHandshakeBudget('arbitrary'));
  equal((await connectAdmission(undefined, 2)).entry.reason, 'connect_admission_rejected');
  const third = await connectAdmission('library-permission-ui-v1', 2); equal(third.state.admitted, 1); equal(third.entry.reason, 'connect_permission_rejected');
  equal((await connectAdmission('library-permission-ui-v1', 3)).entry.reason, 'connect_admission_rejected');
});
test('only the new factory selects the permission handshake scope', () => {
  const observer = Object.fromEntries(['admitLogin', 'physicalRequest', 'physicalResponse', 'physicalFinished', 'frameRequest', 'frameResponse', 'frameFinished'].map(name => [name, () => true]));
  const options = () => ({ account: { slot: 'B', id: USER }, pin: async () => {}, report: {}, observer });
  equal(createHomeOnlyBrowserActor(options()).websocketScope, undefined);
  equal(createPermissionBrowserActor(options()).websocketScope, 'library-permission-ui-v1');
});

function workflowFixture({ badRestricted = false, failJournal = false, earlyClose = false, publishDelay = 0, lostStageReturn = false, consumeLostStage = true } = {}) {
  const input = inputFixture(), binding = bindings(input), files = new Map(), events = [], report = { session_private: session, failure: null };
  let clock = START + 50, restoredAck = null, flow;
  const observer = { bound: { token_sha256: sha(TOKEN) }, phase: 'initial',
    armStage(name) { events.push('arm:' + name); this.phase = name; },
    viewEvidence: stage => ({ result: stage.endsWith('_passive') ? 'not_observed' : 'passed', stage }) };
  const actor = { started: START, token: TOKEN, report: { proxy: { login: 1 }, page_error_count: 0 },
    open: async () => events.push('login'), settled: async () => {}, proxyIdle: async () => {}, assertPinned: async () => {},
    page: { reload: async () => { events.push('reload:' + observer.phase); return { status: () => 200 }; } } };
  const publish = async (filename, value) => {
    const name = path.posix.basename(filename); events.push('publish:' + name);
    if (failJournal && name === 'abort.json') throw new Error('synthetic_journal');
    const source = { path: filename, sha256: sha(JSON.stringify(value, null, 2) + '\n') };
    if (name.startsWith('stage-')) {
      const stage = value.name;
      const temporaryState = { ...flow.state, stages: [...flow.state.stages, { name: stage, sha256: source.sha256, publication_started_at: clock }] };
      const target = stage === 'baseline' ? earlyClose ? 'close' : 'restricted' : stage === 'restricted' ? 'restored' : 'close';
      const control = controlFixture(target, temporaryState, binding);
      control.write_completed_at = new Date(target === 'close' && restoredAck ? restoredAck : clock).toISOString();
      if (target === 'restored') restoredAck = clock;
      if (earlyClose) Object.assign(control, { restoration: 'not_required', revision: null, write_completed_at: null });
      files.set(target, { publication: 'ready', path: ROOT + '/control-' + target + '.json', sha256: sha(JSON.stringify(control)), value: control });
      clock += publishDelay;
      if (lostStageReturn && stage === 'baseline') throw new Error('synthetic_final_directory_sync');
    }
    if (name === 'abort.json' && !earlyClose) {
      const close = controlFixture('close', flow.state, binding); close.write_completed_at = null;
      if (lostStageReturn) {
        if (consumeLostStage) close.previous_stage_sha256 = flow.state.stage_attempts[0].sha256;
        else Object.assign(close, { previous_stage_sha256: null, restoration: 'not_required', revision: null });
      }
      files.set('close', { publication: 'ready', path: ROOT + '/control-close.json', sha256: sha(JSON.stringify(close)), value: close });
    }
    return source;
  };
  flow = new PermissionWorkflow({ input, binding, revision: '3', actor, observer, report, publish,
    read: async name => files.get(name) ?? { publication: 'missing' }, now: () => clock,
    wait: async milliseconds => { clock += milliseconds; }, dom: async (_page, expected, options) => ({ passed: !(badRestricted && observer.phase === 'restricted_reload'),
      libraries: expected.map(item => ({ ...item, card_id_present: true, card_id_matches: true, visible_card_count: 1 })),
      ...(options.excluded.length ? { excluded_libraries: [{ id: MOVIES, absent: true, visible_card_count: 0, matching_id_card_count: 0 }] } : {}) }) });
  return { flow, report, events, files, observer, now: () => clock };
}
test('normal choreography arms before each ready stage and retains passive absence separately from two reloads', async () => {
  const fixture = workflowFixture(); await fixture.flow.execute();
  equal(fixture.events.filter(value => value.startsWith('reload:')), ['reload:restricted_reload', 'reload:restored_reload']);
  check(fixture.events.indexOf('arm:restricted_passive') < fixture.events.indexOf('publish:stage-baseline.json'));
  check(fixture.events.indexOf('arm:restored_passive') < fixture.events.indexOf('publish:stage-restricted.json'));
  equal(fixture.report.restricted.spontaneous.outcome, 'not_observed_within_window'); equal(fixture.report.restoration, 'confirmed');
});
test('a controller ACK may precede the publisher return after final visibility', async () => {
  const fixture = workflowFixture({ publishDelay: 500 }); await fixture.flow.execute();
  check(fixture.flow.state.stages[0].publication_started_at < Date.parse(fixture.report.controls[0].value.write_completed_at) + 500);
  equal(fixture.report.restoration, 'confirmed'); equal(fixture.report.stages.length, 3);
});
test('a lost stage publisher return permits only failure close to its exact attempted bytes', async () => {
  const fixture = workflowFixture({ lostStageReturn: true }); await rejectsAsync(() => fixture.flow.execute());
  equal(fixture.flow.state.stages.length, 0); equal(fixture.flow.state.stage_attempts.length, 1);
  await fixture.flow.abort('permission_flow_failed'); equal(fixture.report.restoration, 'confirmed');
  equal(fixture.report.stages.length, 0); equal(fixture.events.filter(value => value.startsWith('reload:')).length, 0);
  check(fixture.report.stage_publication_attempts[0].completed === false);
});
test('a lost stage return that the controller never consumed permits not-required cleanup only', async () => {
  const fixture = workflowFixture({ lostStageReturn: true, consumeLostStage: false }); await rejectsAsync(() => fixture.flow.execute());
  await fixture.flow.abort('permission_flow_failed'); equal(fixture.report.restoration, 'not_required');
  equal(fixture.report.stages.length, 0); check(fixture.report.stage_publication_attempts[0].completed === false);
  equal(fixture.events.filter(value => value.startsWith('reload:')).length, 0);
});
test('failed membership publishes abort and consumes only a recovery close without another reload', async () => {
  const fixture = workflowFixture({ badRestricted: true }); await rejectsAsync(() => fixture.flow.execute());
  await fixture.flow.abort('permission_membership_incomplete');
  equal(fixture.events.filter(value => value.startsWith('reload:')), ['reload:restricted_reload']);
  check(fixture.events.includes('publish:abort.json')); equal(fixture.report.restoration, 'confirmed'); check(fixture.flow.state.aborted);
});
test('legitimate early close becomes an aborted cleanup signal instead of waiting for a missing restore stage', async () => {
  const fixture = workflowFixture({ earlyClose: true }); await rejectsAsync(() => fixture.flow.execute());
  await fixture.flow.abort('permission_controller_closed_early');
  equal(fixture.report.restoration, 'not_required'); equal(fixture.events.filter(value => value.startsWith('reload:')).length, 0);
});
test('abort journal failure still waits for restoration through its independent budget', async () => {
  const fixture = workflowFixture({ failJournal: true }); fixture.flow.state.aborted = true;
  const close = { ...controlFixture('close', fixture.flow.state), previous_stage_sha256: null, write_completed_at: null };
  fixture.files.set('close', { publication: 'ready', path: ROOT + '/control-close.json', sha256: 'f'.repeat(64), value: close });
  await fixture.flow.abort('permission_flow_failed'); check(fixture.report.abort_journal_failed); equal(fixture.report.restoration, 'confirmed');
});
test('missing restoration confirmation terminates finitely and never invents a close record', async () => {
  const fixture = workflowFixture({ failJournal: true }); const start = fixture.now();
  await fixture.flow.abort('permission_flow_failed'); check(fixture.now() - start <= 170000);
  check(fixture.report.restoration_wait_failed && fixture.report.restoration === 'unconfirmed' && !fixture.report.control_close);
});
test('an unpublished pending control uses the five-second publication bound rather than the full control wait', async () => {
  const fixture = workflowFixture({ failJournal: true }); fixture.files.set('close', { publication: 'publishing' });
  const start = fixture.now(); await fixture.flow.abort('permission_flow_failed');
  check(fixture.now() - start > 5000 && fixture.now() - start <= 5250 && fixture.report.restoration_wait_failed);
});
test('successful reload observation cannot hide an abort, missing restoration or failed session closure', async () => {
  const fixture = workflowFixture(); await fixture.flow.execute();
  const report = { ...closedHomeReport(), ...fixture.report, actor: { ...closedHomeReport().actor, websocket_handshake_budget: 3 },
    final_stage_checks: ['baseline', 'restricted', 'restored'].map(name => ({ name, passed: true })) };
  check(permissionObservationPassed(report));
  for (const change of [value => { value.abort = session; }, value => { value.restoration = 'unconfirmed'; },
    value => { value.actor.page_error_count = 1; }, value => { value.restricted.reload.dom.excluded_libraries[0].absent = false; },
    value => { value.restored.reload.action.control_sha256 = '0'.repeat(64); },
    value => { value.restored.reload.views.stage = 'initial'; }, value => { value.restricted.reload.action.invoked_elapsed_ms = 0; }]) {
    const altered = clone(report); change(altered); check(!permissionObservationPassed(altered));
  }
});

if (process.argv[1] && path.resolve(process.argv[1]) === SELF) {
  if (process.platform !== 'linux' || process.getuid?.() !== 0) throw new Error('remote_permission_guards_only');
  const argv = process.argv.slice(2);
  if (argv.length !== 2 || argv[0] !== '--report' || !new RegExp('^' + WORK + '/client-library-permission-[A-Za-z0-9_-]+/[^/]+[.]json$').test(argv[1])) {
    throw new Error('permission_guard_report_rejected');
  }
  const cases = [];
  for (const { name, fn } of tests) {
    try { await fn(); cases.push({ name, passed: true }); }
    catch { cases.push({ name, passed: false }); }
  }
  const report = { marker: 'goby-client-library-permission-pure-guards-v1', tests: cases.length, passed: cases.filter(item => item.passed).length,
    failed: cases.filter(item => !item.passed).length, browser_runs: 0, http_requests: 0, database_connections: 0, cases };
  await fs.writeFile(argv[1], JSON.stringify(report, null, 2) + '\n', { flag: 'wx', mode: 0o600 });
  process.stdout.write(JSON.stringify({ tests: report.tests, passed: report.passed, failed: report.failed }) + '\n');
  if (report.failed) process.exitCode = 1;
}
