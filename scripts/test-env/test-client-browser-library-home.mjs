#!/usr/bin/env node
/** Pure synthetic event, locator and ownership guards. No browser, HTTP or database is opened. */
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';
import { parseHomeArguments, validateHomeInput, homeSourceDigest, homeClientMetadata, homeLoginEvidence,
  projectHomeViews, HomeViewsObserver, homeCardEvidence, observeHomeDOM, procStartTicks,
  bindHomeBaseline, homeObservationPassed, waitHomeLoginProof, validateHomeAuthority, validateHomeExpectedLibraries } from './client-browser-library-home.mjs';
import { createHomeOnlyBrowserActor, classifyBrowserRequest } from './client-browser-cross-user.mjs';

const SELF = fileURLToPath(import.meta.url);
const WORK = '/opt/goby-test/exec-work-m3e';
const ROOT = WORK + '/client-library-ui-baseline-v3';
const ORIGIN = 'http://127.0.0.1:18196';
const USER = 'ecbbe4cb82403879bc4b4f78894c5738';
const SERVER = 'c7cfd76b1dee728b2bad523793a37ccb';
const ACCOUNT = { slot: 'B', id: USER, username: 'm3e-client-viewer', password: 'synthetic-private-password', credentialsSHA: 'a'.repeat(64) };
const TOKEN = 'synthetic-home-token-0123456789';
const WIRE = { client_name: 'Emby Web', device_id: 'synthetic-home-device', device_name: 'Chrome', client_version: '4.9.5.0' };
const TIME = '2026-09-11T20:00:00.123456Z';
const NOW = Date.parse(TIME), START = NOW - 1000;
const LIBRARIES = [
  { id: 'a9993591e72f0f2e7babcbf8b9c50790', name: 'M3e Client Movies' },
  { id: '6383d20008836e137559698c29b10395', name: 'M3e Client Music' },
  { id: 'a34ce665fb75421ef7551570f353d705', name: 'M3e Client TV' },
  { id: '57a85c1ca5b6c7ae602c587755250b2f', name: 'M3e Client Special Features' },
];
const hash = value => createHash('sha256').update(value).digest('hex');
const clone = value => JSON.parse(JSON.stringify(value));
const descriptor = (name, digest = 'a'.repeat(64)) => ({ path: WORK + '/' + name, sha256: digest });
const headers = token => ({ authorization: `Emby Client="${WIRE.client_name}", DeviceId="${WIRE.device_id}", Device="${WIRE.device_name}", Version="${WIRE.client_version}"${token ? `, Token="${token}"` : ''}` });
const rawHeaders = token => Object.entries(headers(token)).flat();
function check(value) { if (!value) throw new Error('synthetic_assertion_failed'); }
function equal(a, b) { check(JSON.stringify(a) === JSON.stringify(b)); }
function rejects(fn) { let failed = false; try { fn(); } catch { failed = true; } check(failed); }
async function rejectsAsync(fn) { let failed = false; try { await fn(); } catch { failed = true; } check(failed); }
const cases = [];
function test(name, fn) { cases.push({ name, fn }); }

export function inputFixture() {
  const sources = Object.fromEntries(['client-browser-library-home.mjs', 'client-browser-cross-user.mjs',
    'client-browser-special-features-fixture.mjs', 'client-browser-goby-fixture.mjs', 'client-browser-session-proof.mjs']
    .map(name => [WORK + '/synthetic-tool/' + name, 'a'.repeat(64)]));
  return { marker: 'goby-client-library-home-input-v1', version: 1, mode: 'b-home-views-reload', root: ROOT, output: ROOT + '/browser',
    actor: { slot: 'B', user_id: USER, credentials: descriptor('browser.json', '0be6df4acef0565537b3b3a6e8c1518f6bed4023bfdfb5dea60b67dd32fee790'), account_key: 'viewer' },
    candidate: { binary_sha256: 'af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620',
      process: { pid: 748513, start_ticks: 6996875, boot_id: '6bdfc486-7bc8-412f-82b5-70095a09dde7' },
      runtime_sha256: 'd8689a4e0b36816ed462816856dfa73af6fba5f31f044632ed173db99c8842df',
      state_sha256: '5319bc49b2753b84ca04f279523f2482a49a94fabd9944dc369347b6d87224e1', server_id: SERVER,
      base_url: ORIGIN, direct_url: 'http://127.0.0.1:18198', source: WORK + '/source-attempt-32',
      source_manifest_sha256: 'a65070315ce3b31dd70143267cbf774a838bc0e5b1ed99f34c3c759d392daa65' },
    fixture: {
      profile_receipt: descriptor('client-special-features-protocol-finalization-v1/completed.json', 'b443e5f6d5faceb3486d68644298b0a1527f6dcc1e4ac1109ff6d0c252fdfb36'),
      profile_report: descriptor('client-special-features-protocol-finalization-v1/report.json', '929e6b6fc0014e6b3afc7ce023ea981b749ce884ed6eb33c34b3617ac116855d'),
      profile_inspection: descriptor('client-special-features-finalization-inspection-v1/report.json', 'fdc8a38f5937964d3dba8e0e47f897108475011e70f4802e30c1803068dc4b5e'),
      music_chain: descriptor('client-music-upgrade-chain-source32.json', 'c235d59a317fe1b95d8d78a51ab32ca556f3ca7872e96bfea51e710e26b0c67a'),
      music_scan_receipt: descriptor('client-music-scan-v1/receipt.json', 'e0b9ba686afb1950d4ab43c3ad5bc39d67090374704379103a29759c70aab93b'),
    }, expected_libraries: clone(LIBRARIES), source_closure: sources,
    authority: {
      api_report: descriptor('client-library-restriction-v1/report.json', '193a5cc5ddaa806575d630a03de1268abed2418347b4e57eb5cc57610a04e8eb'),
      inspection: descriptor('client-library-restriction-inspection-01/report.json', 'e3dcbc0c21edbc484baab89762e11857a7cbc46889b78701af8dd309f12c3441'),
      recovery: descriptor('client-library-ui-session-recovery-01/report.json', '09b2fad0de6dcce6e1d6ecb85a700e94673642f3c5641d3d7dff6bab43e66ffb'),
      prior_home: descriptor('client-library-ui-baseline-v2/report.json', '54c02dbc41273a5043853d31126dec7455641d5efbf73ee20b97b13d8826ec23'),
      current_snapshot: descriptor('client-library-ui-baseline-v2/after-full.json', '27e4627e774d96ab87fdb51fd5093dafcb01c529b0a8753fd50db566aadbfd26'),
      before_snapshot: descriptor('client-library-ui-baseline-v3/before-full.json'),
    }, controller: { pid: 100, start_ticks: '200', boot_id: '6bdfc486-7bc8-412f-82b5-70095a09dde7', unit: 'goby-client-library-ui-baseline-v3.service' } };
}
function loginBody() {
  return { ServerId: SERVER, AccessToken: TOKEN,
    User: { Id: USER, Name: ACCOUNT.username, ServerId: SERVER, HasPassword: true, Policy: { IsAdministrator: false, IsDisabled: false } },
    SessionInfo: { Id: '9'.repeat(32), UserId: USER, UserName: ACCOUNT.username, ServerId: SERVER,
      Client: WIRE.client_name, DeviceId: WIRE.device_id, DeviceName: WIRE.device_name, ApplicationVersion: WIRE.client_version,
      LastActivityDate: TIME } };
}
function viewsBody() { return Buffer.from(JSON.stringify({ Items: LIBRARIES.map(item => ({ Id: item.id, Name: item.name })), TotalRecordCount: 4 })); }
function request(kind = 'views', token = TOKEN) {
  const url = kind === 'login' ? ORIGIN + '/emby/Users/AuthenticateByName' : `${ORIGIN}/emby/Users/${USER}/Views`;
  const method = kind === 'login' ? 'POST' : 'GET';
  return { url: () => url, method: () => method, serviceWorker: () => null, allHeaders: async () => headers(kind === 'login' ? null : token) };
}
function response(req, status = 200, worker = false) { return { request: () => req, status: () => status, fromServiceWorker: () => worker,
  headers: () => ({ 'content-type': 'application/json; charset=utf-8' }) }; }
function observerFixture(options = {}) {
  const saved = [], proven = [];
  const observer = new HomeViewsObserver({ account: clone(ACCOUNT), expected: clone(LIBRARIES), existingDevices: ['old-device'],
    serverId: SERVER, started: START, now: () => NOW, persistLogin: async owned => { saved.push(clone(owned)); return descriptor('session.json'); },
    onProven: owned => { proven.push(clone(owned)); }, ...options });
  return { observer, saved, proven };
}
async function login(observer, finished = true, terminal = 'completed', loginPath = '/emby/Users/AuthenticateByName') {
  const req = request('login');
  req.url = () => ORIGIN + loginPath;
  check(observer.admitLogin({ url: req.url(), headers: rawHeaders(null) }));
  await observer.frameRequest({ request: req, index: 10, phase: 'login_submit', elapsed_ms: 10 });
  observer.physicalRequest({ id: 1, kind: 'login', method: 'POST', url: req.url(), headers: rawHeaders(null), phase: 'login_submit', elapsed_ms: 11 });
  observer.physicalResponse({ id: 1, status: 200, phase: 'login_response', elapsed_ms: 12 }, Buffer.from(JSON.stringify(loginBody())));
  observer.frameResponse({ response: response(req), elapsed_ms: 13 });
  if (finished) await observer.frameFinished({ request: req, failed: false, elapsed_ms: 14 });
  await observer.physicalFinished({ id: 1, outcome: terminal, status: terminal === 'completed' ? 200 : null, response_bytes: 1000, elapsed_ms: 15 });
  return req;
}
async function views(observer, { id = 2, token = TOKEN, terminal = 'completed', status = 200, worker = false, finish = true, body = viewsBody() } = {}) {
  const req = request('views', token);
  await observer.frameRequest({ request: req, index: id + 10, phase: 'login_form_closed', elapsed_ms: id * 10 });
  observer.physicalRequest({ id, kind: 'views', method: 'GET', url: req.url(), headers: rawHeaders(token), phase: 'login_form_closed', elapsed_ms: id * 10 + 1 });
  observer.physicalResponse({ id, status, elapsed_ms: id * 10 + 2 }, body);
  observer.frameResponse({ response: response(req, status, worker), elapsed_ms: id * 10 + 3 });
  if (finish) await observer.frameFinished({ request: req, failed: false, elapsed_ms: id * 10 + 4 });
  await observer.physicalFinished({ id, outcome: terminal, status: terminal === 'completed' ? status : null, response_bytes: 1000, elapsed_ms: id * 10 + 5 });
  return req;
}

test('explicit input, exact four libraries and no password carrier', () => {
  validateHomeInput(inputFixture());
  for (const change of [value => { value.actor.password = 'forbidden'; }, value => { value.expected_libraries.pop(); },
    value => { value.expected_libraries[3].id = 'e'.repeat(32); }, value => { value.candidate.process.pid += 1; },
    value => { value.authority.current_snapshot.sha256 = 'b'.repeat(64); }, value => { value.mode = 'acceptance-preparation'; }]) {
    const input = inputFixture(); change(input); rejects(() => validateHomeInput(input));
  }
});
test('v3 refuses occupied roots, stale snapshots and missing or wrong historical authority', () => {
  for (const change of [value => { value.root = WORK + '/client-library-ui-baseline-v1'; },
    value => { value.root = WORK + '/client-library-ui-baseline-v2'; },
    value => { value.controller.unit = 'goby-client-library-ui-baseline-v1.service'; },
    value => { value.controller.unit = 'goby-client-library-ui-baseline-v2.service'; },
    value => { value.authority.current_snapshot = descriptor('client-library-restriction-inspection-01/current-full.json',
      '1aca0670c3d6f1cd2f45082df89cf4df9930058f9658eb439deef289b39207a3'); },
    value => { delete value.authority.recovery; }, value => { value.authority.recovery.sha256 = '0'.repeat(64); },
    value => { delete value.authority.prior_home; }, value => { value.authority.prior_home.sha256 = '0'.repeat(64); },
    value => { value.authority.current_snapshot = descriptor('client-library-ui-session-recovery-01/after-full.json',
      '5d0f3818abf5617a817541fc4d08cca5492a579b9ce5af99d0cec45b7d5f1ee0'); },
    value => { value.authority.before_snapshot.path = WORK + '/client-library-ui-baseline-v1/before-full.json'; }]) {
    const input = inputFixture(); change(input); rejects(() => validateHomeInput(input));
  }
});
test('the prior closed Home supplies current authority while both UI failures and recovery remain historical', () => {
  const input = inputFixture(), api = { result: 'passed', phase: 'complete', restoration: 'confirmed' };
  const inspection = { status: 'passed', report_sha256: input.authority.api_report.sha256,
    current_snapshot_sha256: '1aca0670c3d6f1cd2f45082df89cf4df9930058f9658eb439deef289b39207a3',
    all_three_owned_sessions_revoked: true, stored_after_matches_current: true };
  const recoveryAfter = descriptor('client-library-ui-session-recovery-01/after-full.json', '5d0f3818abf5617a817541fc4d08cca5492a579b9ce5af99d0cec45b7d5f1ee0');
  const recovery = { marker: 'goby-client-library-home-session-recovery-v1', result: 'passed', phase: 'complete',
    original_ui_result: 'failed', client_acceptance: false, administrator_closed: true, http_requests: 4, errors: [],
    candidate: { binary_sha256: input.candidate.binary_sha256, process: clone(input.candidate.process), state_sha256: input.candidate.state_sha256 },
    evidence: { 'after-full.json': clone(recoveryAfter) },
    proof: { administrator_revoked: true, administrator_session_id: 'ad6695676912f2bd2a1bf68b9cfdd9ce',
      target_session_id: '00fb0b833884308ab946e1c40ff6abdd', target_user_id: USER,
      target_token_sha256: '9f71d2771f1a0d177041d6815f46d39ecea9a54bf92099a13b89dbef172afb81',
      lost_target_token_401_observed: false, new_administrator_sessions: 1, new_native_session_audits: 3,
      new_devices_play_userdata_references_encodings: 0, old_rows_sequences_private_preserved: true,
      target_only_revoked_at_changed: true, target_revoked_at: TIME } };
  const priorHome = { marker: 'goby-client-library-home-observation-v1', version: 1, result: 'failed', phase: 'complete', outcome: 'failed',
    browser_outcome: 'failed', client_acceptance: false, permission_ui_acceptance: false, worker_closed: true, fallback_attempted: false,
    candidate: clone(input.candidate), authority: { api_report: clone(input.authority.api_report), inspection: clone(input.authority.inspection),
      recovery: clone(input.authority.recovery), current_snapshot: clone(recoveryAfter) },
    evidence: { 'after-full.json': clone(input.authority.current_snapshot) },
    errors: [{ stage: 'final_delta_or_ui_observation', failure_type: 'ObservationError' }],
    proof: { new_b_authentication: 1, new_devices: 1, new_play_userdata_references_encodings: 0, new_session_audits: 2,
      observed_capabilities_bound: true, old_rows_sequences_private_unchanged: true, users_and_policies_unchanged: true,
      session_id: '2e355f661132b4ba3a3ed1445a16c3c8', token_sha256: 'e795b8ecf91e9a2b934b28484ce80a9c52e40838389473032e954cbf699c3eb3' } };
  check(validateHomeAuthority(input, api, inspection, recovery, priorHome));
  for (const change of [value => { value.original_ui_result = 'passed'; }, value => { value.administrator_closed = false; },
    value => { value.proof.lost_target_token_401_observed = true; }, value => { value.proof.target_session_id = 'e'.repeat(32); },
    value => { value.evidence['after-full.json'].sha256 = 'a'.repeat(64); }]) {
    const altered = clone(recovery); change(altered); rejects(() => validateHomeAuthority(input, api, inspection, altered, priorHome));
  }
  for (const change of [value => { value.result = 'passed'; }, value => { value.worker_closed = false; },
    value => { value.fallback_attempted = true; }, value => { value.proof.new_devices = 2; },
    value => { value.authority.current_snapshot = clone(input.authority.current_snapshot); }, value => { value.evidence['after-full.json'].sha256 = '0'.repeat(64); }]) {
    const altered = clone(priorHome); change(altered); rejects(() => validateHomeAuthority(input, api, inspection, recovery, altered));
  }
  rejects(() => validateHomeAuthority(input, api, { ...inspection, current_snapshot_sha256: input.authority.current_snapshot.sha256 }, recovery, priorHome));
});
test('CLI rejects changed output, duplicate flags and missing hashes', () => {
  const good = ['--input', ROOT + '/input.json', '--input-sha256', 'a'.repeat(64), '--output', ROOT + '/browser'];
  parseHomeArguments(good);
  rejects(() => parseHomeArguments([...good.slice(0, 4), '--output', ROOT + '/old-browser']));
  rejects(() => parseHomeArguments(['--input', ROOT + '/input.json', '--input', ROOT + '/input.json', '--output', ROOT + '/browser']));
});
test('source closure digest is sorted compact UTF8 with no newline', () => {
  equal(homeSourceDigest({ z: '2', a: '1' }), hash('{"a":"1","z":"2"}'));
});
test('wire metadata accepts actual carriers and rejects conflicts', () => {
  equal(homeClientMetadata(ORIGIN + '/emby/Users/AuthenticateByName', headers(null)), WIRE);
  rejects(() => homeClientMetadata(ORIGIN + '/emby/Users/AuthenticateByName', { ...headers(null), 'X-Emby-Device-Id': 'foreign' }));
  rejects(() => homeClientMetadata('http://127.0.0.1:18198/emby/Users/AuthenticateByName', headers(null)));
});
test('login ACK validates all three server identities, B and raw UTC creation time', () => {
  const valid = homeLoginEvidence(loginBody(), WIRE, ACCOUNT, SERVER, [], START, NOW);
  equal(valid.proof.created_at, TIME); equal(valid.proof.token_sha256, hash(TOKEN));
  for (const change of [value => { value.ServerId = 'e'.repeat(32); }, value => { value.User.Id = 'e'.repeat(32); },
    value => { value.User.ServerId = 'e'.repeat(32); }, value => { value.User.Policy.IsAdministrator = true; },
    value => { value.SessionInfo.DeviceId = 'other'; }, value => { value.SessionInfo.ServerId = 'e'.repeat(32); },
    value => { value.SessionInfo.LastActivityDate = '2026-09-10T20:00:00Z'; }]) {
    const body = loginBody(); change(body); rejects(() => homeLoginEvidence(body, WIRE, ACCOUNT, SERVER, [], START, NOW));
  }
});
test('fresh device collision is rejected before login forwarding', () => {
  const { observer } = observerFixture({ existingDevices: [WIRE.device_id] });
  rejects(() => observer.admitLogin({ url: request('login').url(), headers: rawHeaders(null) }));
});
test('owned-looking ACK cannot reuse an old session ID or token hash', () => {
  rejects(() => homeLoginEvidence(loginBody(), WIRE, ACCOUNT, SERVER, [], START, NOW, ['9'.repeat(32)]));
  rejects(() => homeLoginEvidence(loginBody(), WIRE, ACCOUNT, SERVER, [], START, NOW, [], [hash(TOKEN)]));
});
test('unrelated assets have no asynchronous observation or header reads', () => {
  const { observer } = observerFixture();
  const asset = { url: () => ORIGIN + '/web/asset.js', method: () => 'GET', serviceWorker: () => null,
    allHeaders: () => { throw new Error('unexpected_header_read'); } };
  equal(observer.frameRequest({ request: asset }), undefined); equal(observer.frames.length, 0);
});
test('session private proof waits for both frame and physical completion', async () => {
  const { observer, saved } = observerFixture(); const req = await login(observer, false);
  equal(saved.length, 0); check(!observer.bound);
  await observer.frameFinished({ request: req, failed: false, elapsed_ms: 16 });
  equal(saved.length, 1); check(observer.bound.frame_login_finished && observer.bound.physical_login_completed);
});
test('the observed lowercase login route produces the same complete owned proof', async () => {
  const actualPath = '/emby/Users/authenticatebyname';
  equal(hash(actualPath), 'f2d44dace1ed3809accceea774bd4d56c3ead4c198a693498e8a35da32aa8c76');
  const { observer, saved } = observerFixture(); await login(observer, true, 'completed', actualPath);
  equal(saved.length, 1); equal(observer.frames.filter(entry => entry.kind === 'login').length, 1);
  check(observer.bound.frame_login_finished && observer.bound.physical_login_completed);
});
test('login proof waiter exits on its own deadline without a detached polling loop', async () => {
  let clock = 0, waits = 0;
  const observer = { bound: null, phase: 'initial' };
  await rejectsAsync(() => waitHomeLoginProof(observer, { timeoutMs: 100, now: () => clock,
    wait: async milliseconds => { clock += milliseconds; waits += 1; } }));
  equal(clock, 100); equal(waits, 4);
});
test('login proof waiter cancels during cleanup without scheduling another wait', async () => {
  let clock = 0, waits = 0;
  const observer = { bound: null, phase: 'initial' };
  await rejectsAsync(() => waitHomeLoginProof(observer, { timeoutMs: 100, now: () => clock,
    wait: async milliseconds => { clock += milliseconds; waits += 1; observer.phase = 'cleanup'; } }));
  equal(waits, 1); equal(clock, 25);
});
test('post-login wait allows an already dispatched frame completion to bind durably', async () => {
  const { observer, saved } = observerFixture(); const req = await login(observer, false);
  const proof = await waitHomeLoginProof(observer, { now: () => NOW,
    settle: () => observer.frameFinished({ request: req, failed: false, elapsed_ms: 16 }),
    wait: async () => { throw new Error('unexpected_wait_after_binding'); } });
  equal(saved.length, 1); check(proof === observer.bound);
});
test('successful login body with failed transfer is never a private cleanup authority', async () => {
  const { observer, saved } = observerFixture(); await login(observer, true, 'failed');
  equal(saved.length, 0); check(!observer.bound);
});
test('owned identity is retained for UI cleanup when session journal fails', async () => {
  const { observer, proven } = observerFixture({ persistLogin: async () => { throw new Error('synthetic_journal_failure'); } });
  await rejectsAsync(() => login(observer)); equal(proven.length, 1); check(observer.owned && !observer.bound && !observer.privateDescriptor);
});
test('initial and reload Views are real distinct frame and physical pairs under one token', async () => {
  const { observer } = observerFixture(); await login(observer); await views(observer);
  equal(observer.viewEvidence('initial').result, 'passed');
  observer.phase = 'reload'; await views(observer, { id: 3 });
  equal(observer.viewEvidence('reload').result, 'passed'); equal(observer.frames.filter(entry => entry.kind === 'login').length, 1);
});
test('missing Views after reload remains not observed', async () => {
  const { observer } = observerFixture(); await login(observer); await views(observer); observer.phase = 'reload';
  equal(observer.viewEvidence('reload').result, 'not_observed'); equal(observer.physical.length, 2);
});
test('200 response with physical disconnect cannot pass Views', async () => {
  const { observer } = observerFixture(); await login(observer); await views(observer, { terminal: 'failed' });
  equal(observer.viewEvidence('initial').result, 'failed'); check(observer.viewEvidence('initial').pairs[0].physical.projection.matches_expected);
});
test('unfinished frame cannot pass a completed physical transfer', async () => {
  const { observer } = observerFixture(); await login(observer); await views(observer, { finish: false });
  equal(observer.viewEvidence('initial').result, 'failed');
});
test('changed token, cached worker response and 404 all remain failed', async () => {
  for (const options of [{ token: 'synthetic-other-token-012345' }, { worker: true }, { status: 404 }]) {
    const { observer } = observerFixture(); await login(observer); await views(observer, options);
    equal(observer.viewEvidence('initial').result, 'failed');
  }
});
test('ambiguous duplicate requests are retained without a fabricated pair', async () => {
  const { observer } = observerFixture(); await login(observer); await views(observer); await views(observer, { id: 3 });
  const result = observer.viewEvidence('initial'); equal(result.result, 'failed'); equal(result.frame_count, 2);
  check(result.pairs.every(pair => pair.physical === null && !pair.unambiguous));
});
test('Views projection preserves counts and refuses missing or unowned library success', () => {
  check(projectHomeViews(viewsBody(), LIBRARIES).matches_expected);
  for (const change of [value => { value.Items.pop(); value.TotalRecordCount = 3; }, value => { delete value.TotalRecordCount; },
    value => { value.Items[3].Id = 'e'.repeat(32); }, value => { value.Items[3].Name = TOKEN; }]) {
    const value = JSON.parse(viewsBody()); change(value); const projection = projectHomeViews(Buffer.from(JSON.stringify(value)), LIBRARIES);
    check(!projection.matches_expected && !JSON.stringify(projection).includes(TOKEN));
  }
  rejects(() => projectHomeViews(Buffer.alloc(512 * 1024 + 1), LIBRARIES));
});
test('three-library projection is explicit and removes only original Movies', () => {
  const expected = LIBRARIES.filter(item => item.id !== LIBRARIES[0].id);
  validateHomeExpectedLibraries(expected);
  const body = Buffer.from(JSON.stringify({ Items: expected.map(item => ({ Id: item.id, Name: item.name })), TotalRecordCount: 3 }));
  check(projectHomeViews(body, expected).matches_expected);
  rejects(() => validateHomeExpectedLibraries(LIBRARIES.slice(0, 3)));
  const input = inputFixture(); input.expected_libraries = expected; rejects(() => validateHomeInput(input));
});
test('permission passive and reload stages keep separate explicit memberships', async () => {
  const { observer } = observerFixture({ scope: 'permission' }); await login(observer); await views(observer);
  const expected = LIBRARIES.slice(1); observer.armStage('restricted_passive', expected);
  await views(observer, { id: 3 }); check(observer.viewEvidence('restricted_passive').result === 'failed');
  observer.armStage('restricted_reload', expected);
  const body = Buffer.from(JSON.stringify({ Items: expected.map(item => ({ Id: item.id, Name: item.name })), TotalRecordCount: 3 }));
  await views(observer, { id: 4, body }); check(observer.viewEvidence('restricted_reload').result === 'passed');
  check(observer.viewEvidence('initial').result === 'passed');
});
test('passive window excludes requests before acknowledgement and completions after its end', async () => {
  const { observer } = observerFixture(); await login(observer); await views(observer);
  equal(observer.viewEvidence('initial', { from_ms: 30 }).result, 'not_observed');
  equal(observer.viewEvidence('initial', { from_ms: 20, until_ms: 25, completed_by_ms: 24 }).result, 'failed');
  equal(observer.viewEvidence('initial', { from_ms: 20, until_ms: 26, completed_by_ms: 26 }).result, 'passed');
});
test('card identity is required when present and title fallback is explicitly limited', () => {
  check(homeCardEvidence(LIBRARIES[0], 1, [{ ids: [LIBRARIES[0].id] }]).passed);
  const noId = homeCardEvidence(LIBRARIES[0], 1, [{ ids: [] }]); check(noId.passed && !noId.card_id_present);
  equal(noId.proof, 'unique_visible_title_without_dom_id');
  check(!homeCardEvidence(LIBRARIES[0], 1, [{ ids: [LIBRARIES[1].id] }]).passed);
  check(!homeCardEvidence(LIBRARIES[0], 2, [{ ids: [] }, { ids: [] }]).passed);
});
test('the observed two titles and one exact ID card pass without weakening title-only ownership', () => {
  check(homeCardEvidence(LIBRARIES[0], 2, [{ ids: [LIBRARIES[0].id] }]).passed);
  check(!homeCardEvidence(LIBRARIES[0], 2, [{ ids: [LIBRARIES[1].id] }]).passed);
  check(!homeCardEvidence(LIBRARIES[0], 2, [{ ids: [LIBRARIES[0].id] }, { ids: [LIBRARIES[0].id] }]).passed);
  check(!homeCardEvidence(LIBRARIES[0], 2, [{ ids: [] }]).passed);
  check(!homeCardEvidence(LIBRARIES[0], 0, [{ ids: [LIBRARIES[0].id] }]).passed);
});
test('DOM mock observes actual four cards and reports missing refresh controls', async () => {
  const chain = result => ({ filter() { return this; }, count: async () => result.length, evaluateAll: async () => result });
  const page = { url: () => ORIGIN + '/web/index.html#!/home',
    getByText: name => { const item = LIBRARIES.find(value => value.name === name); return { filter() { return this; }, count: async () => 1,
      locator: () => chain([{ ids: [item.id] }]) }; },
    getByRole: (_role, { name }) => chain(name === 'Home' ? [{}] : []),
    locator: () => ({ evaluateAll: async () => false }) };
  const result = await observeHomeDOM(page, LIBRARIES); check(result.passed); equal(result.controls.refresh, { links: 0, buttons: 0 });
  page.url = () => ORIGIN + '/web/index.html#!/item?id=' + 'e'.repeat(32);
  check(!(await observeHomeDOM(page, LIBRARIES)).passed);
});
test('Home-only factory retains GET and POST PlaybackInfo, media and mutation denials', () => {
  for (const method of ['GET', 'POST']) check(!classifyBrowserRequest(ORIGIN + '/emby/Items/' + 'e'.repeat(32) + '/PlaybackInfo', method, '', 'acceptance').allow);
  check(!classifyBrowserRequest(ORIGIN + '/emby/Videos/id/stream', 'GET', '', 'acceptance').allow);
  check(!classifyBrowserRequest(ORIGIN + '/admin/v1/users/' + USER, 'PUT', '', 'acceptance').allow);
  const { observer } = observerFixture();
  const actor = createHomeOnlyBrowserActor({ account: clone(ACCOUNT), pin: async () => {}, report: {}, observer: observer.hooks() });
  equal(actor.mode, 'acceptance'); equal(actor.preparationScope, undefined); check(!actor.preparationAllowed());
});
test('passive observer clone mutation and failures never modify original bytes', async () => {
  const { observer } = observerFixture(), hooks = observer.hooks(), report = {};
  const original = Buffer.from('synthetic-original-wire-entity'); let captured;
  hooks.physicalResponse = (_meta, bytes) => { captured = bytes; bytes.fill(7); };
  const actor = createHomeOnlyBrowserActor({ account: clone(ACCOUNT), pin: async () => {}, report, observer: hooks });
  actor.notifyHome('physicalResponse', { status: 200 }, original);
  equal(original.toString(), 'synthetic-original-wire-entity'); check(captured.every(value => value === 0));
  actor.homeObserver = { ...hooks, physicalResponse: async (_meta, bytes) => { bytes.fill(9); throw new Error('synthetic_observer_failure'); } };
  actor.notifyHome('physicalResponse', { status: 200 }, original);
  await Promise.all([...actor.pending]); equal(original.toString(), 'synthetic-original-wire-entity'); equal(report.network.observer_errors, 1);
});
test('private capability evidence binds unchanged bytes and exact successful physical completion', async () => {
  const { observer } = observerFixture(); await login(observer);
  const bytes = Buffer.from('{"PlayableMediaTypes":["Video"],"SupportsMediaControl":false}');
  observer.physicalRequest({ id: 2, kind: 'capabilities', method: 'POST', url: ORIGIN + '/emby/Sessions/Capabilities/Full',
    headers: rawHeaders(TOKEN), phase: 'login_form_closed', elapsed_ms: 20 }, bytes);
  observer.physicalResponse({ id: 2, status: 204, elapsed_ms: 21 }, Buffer.alloc(0));
  await observer.physicalFinished({ id: 2, outcome: 'completed', status: 204, response_bytes: 0, elapsed_ms: 22 });
  const entries = observer.privateCapabilities(); equal(entries.length, 1); equal(entries[0].body_utf8, bytes.toString());
  check(entries[0].token_matches_session && entries[0].completed); equal(entries[0].body_sha256, hash(bytes));
  check(!JSON.stringify(observer.safeEvidence()).includes('PlayableMediaTypes'));
});

function baseline() {
  // Names match the schema27 catalog; rows are synthetic and never claim PostgreSQL semantics.
  const names = ['activity_entries', 'application_key_clients', 'application_key_devices', 'application_keys', 'catalog_entities',
    'client_playback_references', 'devices', 'encoding_jobs', 'extra_reserved_paths', 'item_entities', 'item_extra_resources', 'item_images',
    'item_metadata_state', 'item_subtitles', 'item_theme_resources', 'items', 'libraries', 'library_roots', 'managed_settings', 'play_sessions',
    'scan_jobs', 'schema_migrations', 'server_settings', 'sessions', 'task_definitions', 'task_occurrences', 'task_run_children',
    'task_run_requests', 'task_runs', 'task_triggers', 'theme_owner_ids', 'theme_reserved_paths', 'user_item_data', 'user_settings', 'users'];
  const tables = Object.fromEntries(names.map(name => [name, []]));
  for (const [name, count] of Object.entries({ items: 22, sessions: 70, activity_entries: 155, play_sessions: 26, user_item_data: 7 })) {
    tables[name] = Array.from({ length: count }, (_, index) => ({ id: 'synthetic-' + index }));
  }
  tables.libraries = clone(LIBRARIES);
  tables.devices = Array.from({ length: 60 }, (_, index) => ({ id: index + 1, reported_device_id: 'old-device-' + index }));
  tables.users = [{ id: USER, is_administrator: false, is_disabled: false, management_revision: 3 }, { id: 'synthetic-admin', is_administrator: true }];
  tables.sessions[0] = { id: '00fb0b833884308ab946e1c40ff6abdd', user_id: USER, kind: 'emby', revoked_at: TIME,
    token_hash: '\\x9f71d2771f1a0d177041d6815f46d39ecea9a54bf92099a13b89dbef172afb81' };
  tables.sessions[1] = { id: 'ad6695676912f2bd2a1bf68b9cfdd9ce', user_id: 'synthetic-admin', kind: 'admin', revoked_at: TIME };
  tables.sessions[2] = { id: '2e355f661132b4ba3a3ed1445a16c3c8', user_id: USER, kind: 'emby', revoked_at: TIME,
    token_hash: '\\xe795b8ecf91e9a2b934b28484ce80a9c52e40838389473032e954cbf699c3eb3' };
  return { database: { tables, sequences: { synthetic: { last_value: 12, is_called: true } }, catalog: ['synthetic-catalog'],
    metadata: { captured_at: TIME, database: 'synthetic' } } };
}
test('fresh baseline preserves all old rows and allows only metadata capture timestamp progression', () => {
  const current = baseline(), before = clone(current); before.database.metadata.captured_at = '2026-09-11T20:01:00Z';
  equal(bindHomeBaseline(inputFixture(), before, current).length, 60);
  for (const change of [value => { value.database.tables.user_item_data[0].changed = true; },
    value => { value.database.tables.devices[0].reported_device_id = 'changed'; }, value => { value.database.metadata.database = 'foreign'; },
    value => { value.database.tables.libraries[3].name = 'Other'; }, value => { value.database.metadata.captured_at = 'invalid'; }]) {
    const bad = clone(before); change(bad); rejects(() => bindHomeBaseline(inputFixture(), bad, current));
  }
});
test('v3 baseline requires its new counts and all three prior owned sessions revoked', () => {
  for (const change of [value => { value.database.tables.sessions.length = 69; }, value => { value.database.tables.devices.length = 59; },
    value => { value.database.tables.activity_entries.length = 153; }, value => { value.database.tables.sessions[0].revoked_at = null; },
    value => { value.database.tables.sessions[1].revoked_at = null; }, value => { value.database.tables.sessions[2].revoked_at = null; },
    value => { value.database.tables.users[0].management_revision = 4; }]) {
    const current = baseline(); change(current); rejects(() => bindHomeBaseline(inputFixture(), clone(current), current));
  }
});
test('proc ticks remain canonical strings and tolerate parentheses in process names', () => {
  equal(procStartTicks('123 (a b) name) S ' + Array(18).fill('0').join(' ') + ' 777 0 0'), '777');
  rejects(() => procStartTicks('123 bad'));
});

export function passedReport() {
  const fp = hash(TOKEN), closure = { context_closed: true, browser_closed: true, proxy_closed: true, http_pending: 0,
    websocket_pending: 0, websocket_active: 0, websocket_opened: 2, websocket_closed: 2, sockets_remaining: 0, cleanup_failures: [] };
  return { initial: { views: { result: 'passed' }, dom: { passed: true } },
    reload: { views: { result: 'passed' }, dom: { passed: true }, action: { count: 1, completed: true } },
    login_proof: { token_sha256: fp }, session_private: descriptor('session.json'), capabilities_private: descriptor('caps.json'), closure, failure: null,
    observation: { frames: [{ kind: 'login', finished: true, failed: false, status: 200, from_service_worker: false, content_type: 'application/json', request_sha256: 'a'.repeat(64) }],
      physical: [{ kind: 'login', completed: true, terminal_status: 200, request_sha256: 'a'.repeat(64) }] },
    actor: { login: { request_count: 1, status: 200 }, ordinary_authority_confirmed: true, page_error_count: 0, closed: true,
      logout: { status: 204, login_view_visible: true }, proxy: { login: 1, logout: 1, preparation: 0, failed: 0, rejected: 0, active: 0, admitted: 200, completed: 200 },
      proxy_logout: { status: 204, completed: true, token_fingerprint: fp }, console_warning_error_count: 0,
      network: { external_blocked: 0, playback_attempts: 0, observer_errors: 0, forbidden_mutations: 0, overflow: 0, guard_errors: 0 },
      cleanup_failures: [], websocket: { failed: 0, control_attempts: 0 }, session_proof: { outcome: 'all_observed_logout_tokens_rejected',
        entries: [{ result: 'logout_token_rejected', token_fingerprint: fp }] } } };
}
test('baseline success requires exact UI logout, no page error and closed browser and sockets', () => {
  check(homeObservationPassed(passedReport()));
  for (const change of [value => { value.actor.page_error_count = 1; }, value => { value.actor.network.playback_attempts = 1; },
    value => { value.closure.websocket_active = 1; }, value => { value.actor.logout.status = null; },
    value => { value.actor.proxy_logout.token_fingerprint = 'b'.repeat(64); }, value => { value.actor.login.request_count = 2; },
    value => { value.failure = 'parent_fallback_cleanup'; }, value => { value.session_private = null; },
    value => { value.reload.views.result = 'not_observed'; }, value => { value.closure.context_closed = false; }]) {
    const report = passedReport(); change(report); check(!homeObservationPassed(report));
  }
});
test('successfully blocked external posts remain visible without becoming permission to forward', () => {
  const report = passedReport(); report.actor.network.external_blocked = 2; report.actor.console_warning_error_count = 2;
  check(homeObservationPassed(report));
  check(!classifyBrowserRequest('https://synthetic.invalid/telemetry', 'POST', 'fetch', 'acceptance').allow);
  report.actor.network.external_blocked = -1; check(!homeObservationPassed(report));
});

if (process.argv[1] && path.resolve(process.argv[1]) === SELF) {
  if (process.platform !== 'linux' || process.getuid?.() !== 0) throw new Error('remote_guards_only');
  const argv = process.argv.slice(2);
  if (argv.length !== 2 || argv[0] !== '--report' || !new RegExp('^' + WORK + '/client-library-(?:ui|permission)-[A-Za-z0-9_-]+/[^/]+[.]json$').test(argv[1])) {
    throw new Error('guard_report_path_rejected');
  }
  const results = [];
  for (const { name, fn } of cases) {
    try { await fn(); results.push({ name, passed: true }); }
    catch { results.push({ name, passed: false }); }
  }
  const report = { marker: 'goby-client-library-home-pure-guards-v1', tests: results.length,
    passed: results.filter(value => value.passed).length, failed: results.filter(value => !value.passed).length,
    browser_runs: 0, http_requests: 0, database_connections: 0, cases: results };
  await fs.writeFile(argv[1], JSON.stringify(report, null, 2) + '\n', { flag: 'wx', mode: 0o600 });
  process.stdout.write(JSON.stringify({ tests: report.tests, passed: report.passed, failed: report.failed }) + '\n');
  if (report.failed) process.exitCode = 1;
}
