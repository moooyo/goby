#!/usr/bin/env node
/** Retire only evidenced M3e fixture browser sessions; never client acceptance. */

import fs from 'node:fs/promises';
import path from 'node:path';
import http from 'node:http';
import { constants } from 'node:fs';
import { createHash, randomBytes } from 'node:crypto';
import { createRequire } from 'node:module';

const ROOT = '/opt/goby-test/exec-work-m3e';
const REF_ORIGIN = 'http://127.0.0.1:18197';
const GOBY_ORIGIN = 'http://127.0.0.1:18196';
const STATE_RUNS = ['reference-movie-ui-01', 'reference-movie-ui-02', 'reference-movie-ui-03'];
const NO_STATE_RUNS = ['reference-ui-login-01', 'reference-ui-login-network-boundary-02'];
const PINS = {
  reference: { pid: 332054, ticks: '357218', exe: '/dev/shm/goby-emby-reference/package/opt/emby-server/system/EmbyServer', sha256: 'c109c9817dea25cc516b9969a87aa1ffa48e41adcb5e3dbc87d686c7bcb28ac2' },
  goby: { pid: 342622, ticks: '558288', exe: '/opt/goby-client-m3e/goby', sha256: 'dda315423197945bd0a5a6096e823a9e143f6fe71230a33f613aae271af7e05e' },
};
const output = process.argv[2];
if (process.platform !== 'linux' || process.getuid() !== 0 || !output || path.dirname(path.resolve(output)) !== ROOT) {
  throw new Error('A new private cleanup directory under the fixed remote workspace is required.');
}
process.umask(0o077);
await fs.mkdir(output, { mode: 0o700 });
const report = { format: 1, scope: 'Owned fixture session cleanup only; not UI or client acceptance',
  started_at: new Date().toISOString(), pins: PINS, reference_state_cleanup: [],
  remaining_unattributed: [], native_cleanup: null, external_requests_blocked: 0, errors: [] };
const require = createRequire(import.meta.url);
const { chromium } = require('/opt/goby-test/inactive-dependencies-m5h/node_modules/playwright');
const digest = value => createHash('sha256').update(value).digest('hex');

async function privateJSON(filename, maximum = 1024 * 1024) {
  if (!path.resolve(filename).startsWith(ROOT + path.sep)) throw new Error('Private input left the owned workspace.');
  const handle = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const info = await handle.stat();
    if (!info.isFile() || info.uid !== 0 || (info.mode & 0o777) !== 0o600 || info.nlink !== 1 || info.size > maximum) {
      throw new Error('Private input permissions or size are invalid.');
    }
    const raw = await handle.readFile();
    if (raw.length !== info.size) throw new Error('Private input changed while reading.');
    return { value: JSON.parse(raw.toString('utf8')), sha256: digest(raw) };
  } finally { await handle.close(); }
}

async function pin(name, full = false) {
  const expected = PINS[name];
  const stat = await fs.readFile(`/proc/${expected.pid}/stat`, 'utf8');
  const fields = stat.slice(stat.lastIndexOf(')') + 2).split(' ');
  if (fields[19] !== expected.ticks || await fs.readlink(`/proc/${expected.pid}/exe`) !== expected.exe) {
    throw new Error('The selected fixture process changed.');
  }
  if (full && digest(await fs.readFile(`/proc/${expected.pid}/exe`)) !== expected.sha256) {
    throw new Error('The selected fixture executable changed.');
  }
}

async function request(name, pathname, { method = 'GET', headers = {}, body } = {}) {
  await pin(name, method !== 'GET');
  const origin = name === 'reference' ? REF_ORIGIN : GOBY_ORIGIN;
  const url = new URL(pathname, origin);
  if (url.origin !== origin || !pathname.startsWith('/') || pathname.startsWith('//')) throw new Error('Cleanup request left its fixture.');
  const bytes = body === undefined ? null : Buffer.from(JSON.stringify(body));
  const result = await new Promise((resolve, reject) => {
    const outgoing = http.request(url, { method, headers: { Accept: 'application/json', Origin: origin,
      ...(bytes ? { 'Content-Type': 'application/json', 'Content-Length': bytes.length } : {}), ...headers } }, incoming => {
      const chunks = []; let total = 0;
      incoming.on('data', chunk => {
        total += chunk.length;
        if (total > 1024 * 1024) { outgoing.destroy(new Error('Cleanup response exceeded its limit.')); return; }
        chunks.push(chunk);
      });
      incoming.on('end', () => {
        const raw = Buffer.concat(chunks);
        let data = null;
        if (raw.length && /application\/json/i.test(incoming.headers['content-type'] ?? '')) {
          try { data = JSON.parse(raw.toString('utf8')); } catch { /* Error bodies are not retained. */ }
        }
        resolve({ status: incoming.statusCode, data, headers: incoming.headers });
      });
    });
    outgoing.setTimeout(10000, () => outgoing.destroy(new Error('Cleanup request timed out.')));
    outgoing.on('error', () => reject(new Error('Cleanup transport failed.')));
    outgoing.end(bytes);
  });
  await pin(name);
  return result;
}

async function save(name, value) {
  await fs.writeFile(path.join(output, name), JSON.stringify(value, null, 2) + '\n', { mode: 0o600, flag: 'wx' });
}

function sessionView(row) {
  return Object.fromEntries(['Id', 'UserId', 'Kind', 'Client', 'DeviceId', 'DeviceName', 'ApplicationVersion',
    'CreatedAt', 'LastSeenAt', 'LastActivityDate', 'Status', 'RevokedAt'].filter(key => row[key] !== undefined).map(key => [key, row[key]]));
}

function ownedStoredReference(saved, reference, runReport) {
  if (runReport.requested_url !== REF_ORIGIN + '/web/index.html' || runReport.login?.http_status !== 200 ||
      runReport.logout?.owned_session !== 'retained_owned_session_no_ui_logout') throw new Error('Saved run is not an owned retained login.');
  if (saved.cookies.length || saved.origins.length !== 1 || saved.origins[0].origin !== REF_ORIGIN) throw new Error('Saved browser origin differs.');
  const entries = Object.fromEntries(saved.origins[0].localStorage.map(entry => [entry.name, entry.value]));
  const servers = JSON.parse(entries.servercredentials3).Servers;
  if (servers.length !== 1 || servers[0].Id !== reference.serverId || servers[0].Users.length !== 1 ||
      new URL(servers[0].ManualAddress).origin !== REF_ORIGIN) throw new Error('Saved server identity differs.');
  const user = servers[0].Users[0];
  if (user.UserId !== reference.accounts.viewer.userId || typeof user.AccessToken !== 'string' || !user.AccessToken ||
      typeof entries._deviceId2 !== 'string' || !entries._deviceId2) throw new Error('Saved viewer identity differs.');
  return { token: user.AccessToken, userId: user.UserId, deviceId: entries._deviceId2 };
}

async function denyProxy() {
  const sockets = new Set();
  const server = http.createServer({ maxHeaderSize: 16384 }, (_request, response) => {
    report.external_requests_blocked += 1;
    response.writeHead(403, { Connection: 'close', 'Content-Length': '0' }); response.end();
  });
  for (const event of ['connect', 'upgrade']) server.on(event, (_request, socket) => {
    report.external_requests_blocked += 1;
    socket.end('HTTP/1.1 403 Forbidden\r\nConnection: close\r\nContent-Length: 0\r\n\r\n');
  });
  server.on('connection', socket => { sockets.add(socket); socket.on('error', () => {}); socket.on('close', () => sockets.delete(socket)); });
  server.on('clientError', (_error, socket) => socket.destroy());
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  return { address: `http://127.0.0.1:${server.address().port}`, async close() {
    for (const socket of sockets) socket.destroy();
    await new Promise(resolve => server.close(resolve));
  } };
}

async function referenceUI(browser, guard, stateFile, owned, record) {
  await pin('reference', true);
  const context = await browser.newContext({ storageState: stateFile, locale: 'en-US',
    viewport: { width: 1440, height: 1000 }, proxy: { server: guard.address, bypass: '<-loopback>,127.0.0.1:18197' } });
  await context.route('**/*', async route => {
    const url = new URL(route.request().url());
    if (url.origin === REF_ORIGIN || ['data:', 'blob:'].includes(url.protocol)) { await pin('reference'); await route.continue(); }
    else { report.external_requests_blocked += 1; await route.abort('blockedbyclient'); }
  });
  const page = await context.newPage();
  const observed = [];
  page.on('response', response => {
    const url = new URL(response.url());
    if (url.origin === REF_ORIGIN && response.request().method() === 'POST' && /^\/emby\/sessions\/logout\/?$/i.test(url.pathname)) observed.push(response.status());
  });
  try {
    await page.goto(REF_ORIGIN + '/web/index.html', { waitUntil: 'domcontentloaded', timeout: 20000 });
    const settings = page.getByRole('button', { name: 'Settings', exact: true }).filter({ visible: true });
    await settings.waitFor({ state: 'visible', timeout: 10000 });
    await settings.click({ timeout: 8000 });
    const signOut = page.getByRole('button', { name: /\bSign Out\b/i }).filter({ visible: true });
    await signOut.click({ timeout: 8000 });
    await Promise.any([
      page.getByText('Manual Login', { exact: true }).waitFor({ state: 'visible', timeout: 10000 }),
      page.locator('form:has(input[type="password"]:visible)').waitFor({ state: 'visible', timeout: 10000 }),
    ]);
    record.ui = { logout_statuses: observed, login_view_visible: true,
      result: observed.some(status => status >= 200 && status < 300) ? 'sign_out_observed' : 'logout_response_not_observed' };
  } catch { record.ui = { logout_statuses: observed, login_view_visible: false, result: 'original_ui_unavailable' }; }
  finally { await context.close(); }
  const after = await request('reference', '/emby/Users/' + encodeURIComponent(owned.userId), { headers: { 'X-Emby-Token': owned.token } });
  record.token_status_after_ui = after.status;
  if (after.status === 401) { record.result = 'owned_token_revoked'; return; }
  if (after.status !== 200 || after.data?.Id !== owned.userId) throw new Error('Saved token changed its authority.');
  record.result = 'ui_unconfirmed_token_retained';
}

async function nativeInventoryAndCleanup() {
  const fixture = (await privateJSON(path.join(ROOT, 'client-fixture.json'), 4 * 1024 * 1024)).value;
  const credentials = (await privateJSON(path.join(ROOT, 'browser.json'))).value;
  const run = (await privateJSON(path.join(ROOT, 'goby-ui-login-05/observation.json'))).value;
  if (fixture.marker !== 'goby-m3e-client-acceptance-v1' || credentials.marker !== fixture.marker ||
      credentials.base_url !== GOBY_ORIGIN || run.requested_url !== GOBY_ORIGIN + '/web/index.html' || run.login?.http_status !== 200) {
    throw new Error('Candidate cleanup ownership metadata differs.');
  }
  const identity = await request('goby', '/emby/System/Info/Public');
  if (identity.status !== 200 || identity.data?.Id !== fixture.server_id) throw new Error('Candidate server identity differs.');
  let cookie = null; let csrf = null;
  const result = report.native_cleanup = { run: 'goby-ui-login-05', before: [], candidates: [], retired: [], remaining: [] };
  const headers = () => ({ Cookie: cookie, 'X-CSRF-Token': csrf });
  try {
    const login = await request('goby', '/admin/v1/session', { method: 'POST', body: {
      Name: credentials.admin.username, Password: credentials.admin.password } });
    if (login.status !== 200 || login.data?.User?.Id !== fixture.admin_id) throw new Error('Cleanup administrator identity differs.');
    cookie = login.headers['set-cookie']?.find(value => value.startsWith('goby_session='))?.split(';')[0];
    csrf = login.data.CSRFToken;
    if (!cookie || !csrf) throw new Error('Cleanup administrator session is incomplete.');
    await save('private-native-control-session.json', { origin: GOBY_ORIGIN, cookie, csrf,
      marker: 'goby-owned-browser-cleanup-control-v1', admin_user_id: fixture.admin_id });
    const route = '/admin/v1/sessions?UserId=' + encodeURIComponent(fixture.viewer_id) + '&Kind=emby&Status=active&Limit=200';
    const listing = await request('goby', route, { headers: headers() });
    if (listing.status !== 200 || !Array.isArray(listing.data?.Items) || listing.data.TotalRecordCount > 200) throw new Error('Native session inventory is incomplete.');
    result.before = listing.data.Items.map(sessionView);
    const from = Date.parse(run.started_at), through = Date.parse(run.finished_at);
    const candidates = listing.data.Items.filter(row => row.UserId === fixture.viewer_id && row.Kind === 'emby' && row.Status === 'active' &&
      row.Client === 'Emby Web' && row.ApplicationVersion === '4.9.5.0' &&
      Date.parse(row.CreatedAt) >= from && Date.parse(row.CreatedAt) <= through && typeof row.DeviceId === 'string' && row.DeviceId.length >= 16);
    result.candidates = candidates.map(row => ({ ...sessionView(row), ownership_evidence: {
      source_run: 'goby-ui-login-05', exact_owned_user: true, exact_client_build: 'Emby Web 4.9.5.0',
      unique_successful_browser_login_window: candidates.length === 1,
      window_start: run.started_at, window_end: run.finished_at,
      native_session_device_bound: true, scope_excludes_non_browser_recorder_clients: true } }));
    await save('native-before-revocation.json', result);
    if (candidates.length === 1) {
      const candidate = candidates[0];
      const second = await request('goby', route + '&DeviceId=' + encodeURIComponent(candidate.DeviceId), { headers: headers() });
      if (second.status !== 200 || second.data?.Items?.length !== 1 ||
          JSON.stringify(sessionView(second.data.Items[0])) !== JSON.stringify(sessionView(candidate))) throw new Error('Native candidate changed before revocation.');
      const revoked = await request('goby', '/admin/v1/sessions/' + encodeURIComponent(candidate.Id) + '/revoke',
        { method: 'POST', headers: headers(), body: {} });
      if (revoked.status !== 200 || revoked.data?.SessionId !== candidate.Id || revoked.data?.UserId !== fixture.viewer_id || revoked.data?.CurrentSessionRevoked !== false) {
        throw new Error('Native revocation acknowledgement differs.');
      }
      const history = await request('goby', route.replace('Status=active', 'Status=all') + '&DeviceId=' + encodeURIComponent(candidate.DeviceId), { headers: headers() });
      const row = history.data?.Items?.find(entry => entry.Id === candidate.Id);
      if (history.status !== 200 || row?.Status !== 'revoked' || !row.RevokedAt) throw new Error('Native revoked history is not retained.');
      result.retired.push({ ...sessionView(row), method: 'native_admin_exact_session_revocation',
        token_401_probe: 'unavailable_no_owned_token_was_retained', history_preserved: true });
    }
    const after = await request('goby', route, { headers: headers() });
    if (after.status !== 200) throw new Error('Native post-cleanup inventory failed.');
    result.remaining = after.data.Items.map(sessionView);
  } finally {
    if (cookie && csrf) {
      const logout = await request('goby', '/admin/v1/session', { method: 'DELETE', headers: headers() });
      const denied = await request('goby', '/admin/v1/session', { headers: headers() });
      result.control_session_cleanup = { logout_status: logout.status, subsequent_status: denied.status,
        revoked: logout.status === 204 && denied.status === 401 };
      if (!result.control_session_cleanup.revoked) throw new Error('Cleanup administrator revocation was not verified.');
    }
  }
}

let browser = null; let guard = null;
try {
  await pin('reference', true); await pin('goby', true);
  const proxy = (await privateJSON(path.join(ROOT, 'dual-proxy-status-02.json'))).value;
  if (proxy.reference_pid !== PINS.reference.pid || proxy.reference_start_ticks !== PINS.reference.ticks) throw new Error('Proxy reference binding differs.');
  const reference = (await privateJSON(path.join(ROOT, 'reference-browser.json'))).value;
  const identity = await request('reference', '/emby/System/Info/Public');
  if (identity.status !== 200 || identity.data?.Id !== reference.serverId) throw new Error('Reference server identity differs.');
  const owned = [];
  for (const run of STATE_RUNS) {
    const stateFile = path.join(ROOT, run, 'private-browser-storage-state.json');
    const saved = await privateJSON(stateFile);
    const runReport = (await privateJSON(path.join(ROOT, run, 'observation.json'))).value;
    const principal = ownedStoredReference(saved.value, reference, runReport);
    const current = await request('reference', '/emby/Users/' + encodeURIComponent(principal.userId), { headers: { 'X-Emby-Token': principal.token } });
    if (current.status !== 401 && (current.status !== 200 || current.data?.Id !== principal.userId)) throw new Error('Saved state authority differs.');
    const record = { run, source_state_sha256: saved.sha256, token_fingerprint: digest(principal.token),
      user_id: principal.userId, device_id: principal.deviceId, before_status: current.status };
    report.reference_state_cleanup.push(record);
    owned.push({ stateFile, principal, record });
  }
  const live = owned.find(value => value.record.before_status === 200);
  if (live) {
    const sessions = await request('reference', '/emby/Sessions?ActiveWithinSeconds=0', { headers: { 'X-Emby-Token': live.principal.token } });
    if (sessions.status !== 200 || !Array.isArray(sessions.data)) throw new Error('Reference session inventory failed.');
    report.reference_before = sessions.data.filter(row => row.UserId === reference.accounts.viewer.userId).map(sessionView);
    for (const run of NO_STATE_RUNS) {
      const observation = (await privateJSON(path.join(ROOT, run, 'observation.json'))).value;
      const candidates = report.reference_before.filter(row => row.Client === 'Emby Web' && row.ApplicationVersion === '4.9.5.0' &&
        Date.parse(row.LastActivityDate) >= Date.parse(observation.started_at) && Date.parse(row.LastActivityDate) <= Date.parse(observation.finished_at));
      report.remaining_unattributed.push({ target: 'fresh-reference', run,
        reason: 'No saved token or directly recorded browser DeviceId; time-window candidates are retained without registry deletion',
        window_start: observation.started_at, window_end: observation.finished_at, candidates });
    }
  }
  await save('inventory-before.json', report);
  guard = await denyProxy();
  browser = await chromium.launch({ headless: true, args: ['--no-sandbox', '--disable-dev-shm-usage', '--disable-background-networking'] });
  for (const item of owned) {
    if (item.record.before_status === 401) item.record.result = 'already_revoked';
    else await referenceUI(browser, guard, item.stateFile, item.principal, item.record);
    const after = await privateJSON(item.stateFile);
    if (after.sha256 !== item.record.source_state_sha256) throw new Error('Original private browser state changed.');
    await save(item.record.run + '-cleanup.json', item.record);
    process.stdout.write(JSON.stringify({ run: item.record.run, result: item.record.result,
      after_status: item.record.token_status_after_ui ?? item.record.before_status }) + '\n');
  }
  await nativeInventoryAndCleanup();
} catch {
  report.errors.push('Cleanup stopped at a failed ownership, transport, or verification guard; inspect the bounded stage reports.');
} finally {
  if (browser) await browser.close().catch(() => {});
  if (guard) await guard.close();
  report.finished_at = new Date().toISOString();
  await save('cleanup-report.json', report);
  process.stdout.write(JSON.stringify({ reference_state_results: report.reference_state_cleanup.map(row => ({ run: row.run, result: row.result })),
    native_retired: report.native_cleanup?.retired?.length ?? 0, remaining_unattributed_runs: report.remaining_unattributed.length,
    errors: report.errors.length, output }) + '\n');
}
if (report.errors.length) process.exitCode = 1;
