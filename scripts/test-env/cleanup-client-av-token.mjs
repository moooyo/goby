#!/usr/bin/env node
/** Revoke one exact saved Goby AV token; this is not client UI acceptance. */
import fs from 'node:fs/promises';
import http from 'node:http';
import { createHash } from 'node:crypto';
import { loadGobyAVFixture } from './client-browser-goby-fixture.mjs';

const ROOT = '/opt/goby-test/exec-work-m3e';
const [runName, expectedSHA256] = process.argv.slice(2);
const hash = value => createHash('sha256').update(value).digest('hex');
let phase = 'arguments', output, report, fixture;
function requireThat(condition) { if (!condition) throw new Error('owned_av_cleanup_guard_failed'); }
async function privateJSON(filename) {
  const info = await fs.lstat(filename);
  requireThat(info.isFile() && !info.isSymbolicLink() && info.uid === 0 && info.nlink === 1 &&
    (info.mode & 0o777) === 0o600 && info.size > 0 && info.size <= 2 * 1024 * 1024);
  const raw = await fs.readFile(filename);
  return { value: JSON.parse(raw), sha256: hash(raw) };
}
try {
  requireThat(process.platform === 'linux' && process.getuid() === 0 && process.argv.length === 4 &&
    /^goby-av-(?:source[1-9]\d{0,3}-)?(?:mp3|flac|subtitles|tv|album-diagnostic|subtitle-diagnostic)-\d{2}$/.test(runName ?? '') && /^[0-9a-f]{64}$/.test(expectedSHA256 ?? ''));
  process.umask(0o077);
  fixture = await loadGobyAVFixture({ expectedSHA256 });
  const directory = `${ROOT}/${runName}`;
  const info = await fs.lstat(directory);
  requireThat(info.isDirectory() && !info.isSymbolicLink() && info.uid === 0 && (info.mode & 0o777) === 0o700 &&
    await fs.realpath(directory) === directory);
  phase = 'owned_state';
  const observation = await privateJSON(`${directory}/observation.json`);
  const saved = await privateJSON(`${directory}/private-browser-storage-state.json`);
  requireThat(observation.value.failure && observation.value.av_scope.account_id === fixture.accountId &&
    observation.value.av_scope.account_marker === fixture.accountMarker && observation.value.login.http_status === 200 &&
    observation.value.goby_fixture_evidence.receipt_sha256 === fixture.evidence.receipt_sha256 &&
    observation.value.goby_fixture_evidence.binary_sha256 === expectedSHA256);
  requireThat(saved.value.origins.length === 1 && saved.value.origins[0].origin === fixture.origin);
  const entries = saved.value.origins[0].localStorage.filter(entry => entry.name === 'servercredentials3');
  requireThat(entries.length === 1);
  const servers = JSON.parse(entries[0].value).Servers;
  requireThat(servers.length === 1 && servers[0].Id === fixture.serverId && servers[0].Users.length === 1 &&
    servers[0].Users[0].UserId === fixture.accountId);
  let token = servers[0].Users[0].AccessToken;
  requireThat(typeof token === 'string' && token.length > 0);
  report = { format: 1, scope: 'Exact owned saved AV token cleanup only; not UI acceptance', run: runName,
    source_report_sha256: observation.sha256, storage_state_sha256: saved.sha256, token_sha256: hash(token),
    account_id: fixture.accountId, fixture: fixture.evidence, requests: [], outcome: 'not_completed' };
  output = await fs.open(`${directory}/api-session-cleanup.json`, 'wx', 0o600);
  async function request(method, route) {
    await fixture.assertPinned();
    const entry = { method, route, status: null, is_ui_request: false }; report.requests.push(entry);
    const status = await new Promise((resolve, reject) => {
      const outgoing = http.request(new URL(route, fixture.origin), { method,
        headers: { 'X-Emby-Token': token, Origin: fixture.origin, 'Content-Length': '0' } }, incoming => {
        let bytes = 0;
        incoming.on('data', chunk => { bytes += chunk.length; if (bytes > 8192) { reject(new Error('cleanup_response_limit')); incoming.destroy(); outgoing.destroy(); } });
        incoming.on('error', () => reject(new Error('cleanup_response_failed')));
        incoming.on('aborted', () => reject(new Error('cleanup_response_aborted')));
        incoming.on('close', () => { if (!incoming.complete) reject(new Error('cleanup_response_incomplete')); });
        incoming.on('end', () => resolve(incoming.statusCode));
      });
      outgoing.setTimeout(10000, () => outgoing.destroy(new Error('cleanup_timeout')));
      outgoing.on('error', () => reject(new Error('cleanup_transport_failed'))); outgoing.end();
    });
    entry.status = status; await fixture.assertPinned(); return status;
  }
  phase = 'logout';
  const logout = await request('POST', '/emby/Sessions/Logout');
  phase = 'verification';
  const rejected = await request('GET', '/emby/System/Info');
  token = null;
  requireThat(logout === 204 && rejected === 401);
  report.outcome = 'owned_token_revoked';
  process.stdout.write(JSON.stringify({ outcome: report.outcome, run: runName, logout_status: logout, verification_status: rejected }) + '\n');
} catch {
  if (report) report.failure_phase = phase;
  process.stdout.write(JSON.stringify({ outcome: 'blocked', run: /^[a-z0-9-]{1,80}$/.test(runName ?? '') ? runName : null, phase }) + '\n');
  process.exitCode = 1;
} finally {
  if (output) { await output.writeFile(JSON.stringify(report, null, 2) + '\n'); await output.close(); }
}
