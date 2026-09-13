#!/usr/bin/env node
/** Observe one owned reference actor through Home and TV without playback. */
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import { createHash } from 'node:crypto';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { createReferenceBrowserActor, REFERENCE_ORIGIN, REFERENCE_SERVER, REFERENCE_VIEWER }
  from './client-browser-nextup-discovery-runtime.mjs';

export const WORK = '/opt/goby-test/exec-work-m3e';
export const ROOT = WORK + '/reference-nextup-client-discovery-01';
export const TOOL = WORK + '/nextup-client-discovery-tool-01/revision-01';
export const FILES = ['client-browser-nextup-discovery.mjs', 'client-browser-nextup-discovery-runtime.mjs', 'client-browser-session-proof.mjs'];
export const EPISODES = ['A1', 'A2', 'A3', 'B1', 'B2', 'B3'];
export const SUMMARIES = ['A', 'AS1', 'AS2', 'B', 'BS1', 'BS2'];
const SHA = /^[0-9a-f]{64}$/;
const sha = value => createHash('sha256').update(value).digest('hex');
const record = value => value !== null && typeof value === 'object' && !Array.isArray(value);
function need(value, reason = 'nextup_discovery_guard_rejected') { if (!value) throw new Error(reason); }
function stable(value) {
  if (Array.isArray(value)) return value.map(stable);
  return record(value) ? Object.fromEntries(Object.keys(value).sort().map(key => [key, stable(value[key])])) : value;
}
const same = (left, right) => JSON.stringify(stable(left)) === JSON.stringify(stable(right));
export function assertSafeExport(value, secrets) {
  const variants = secrets.filter(Boolean).flatMap(secret => { const bytes = Buffer.from(secret); return [secret,
    JSON.stringify(secret).slice(1, -1), encodeURIComponent(secret), bytes.toString('base64'), bytes.toString('base64url'),
    bytes.toString('hex'), [...bytes].map(byte => '%' + byte.toString(16).padStart(2, '0')).join('')]; });
  function inspect(current) {
    if (typeof current === 'string') {
      const candidate = current.toLowerCase();
      need(!variants.some(secret => candidate.includes(secret.toLowerCase())), 'nextup_export_contains_secret');
      need(!/(?:^|[\s"':])\/(?:opt|dev|proc|root|home|var)\//i.test(current), 'nextup_export_contains_native_path');
    } else if (Array.isArray(current)) current.forEach(inspect);
    else if (record(current)) for (const [key, field] of Object.entries(current)) { inspect(key); inspect(field); }
  }
  inspect(value);
}
const descriptor = value => record(value) && Object.keys(value).length === 2 && typeof value.path === 'string' &&
  value.path.startsWith(WORK + '/') && !value.path.split('/').includes('..') && SHA.test(value.sha256);
function exact(value, keys) { return record(value) && same(Object.keys(value).sort(), [...keys].sort()); }

export function validateInput(input) {
  need(exact(input, ['kind', 'version', 'runId', 'root', 'execution', 'release', 'sourceClosure', 'existingDeviceIds', 'libraries', 'budgets']));
  need(input.kind === 'nextup-reference-client-discovery-input' && input.version === 1 &&
    input.runId === 'nextup-reference-client-discovery-01' && input.root === ROOT);
  need(descriptor(input.execution) && descriptor(input.release));
  need(exact(input.sourceClosure, FILES) && FILES.every(name => descriptor(input.sourceClosure[name]) &&
    input.sourceClosure[name].path === TOOL + '/' + name));
  need(Array.isArray(input.existingDeviceIds) && input.existingDeviceIds.length <= 9998 &&
    input.existingDeviceIds.every(value => typeof value === 'string' && value.length > 0 && value.length <= 256) &&
    new Set(input.existingDeviceIds).size === input.existingDeviceIds.length);
  need(['before', 'after'].every(phase => !input.existingDeviceIds.includes('goby-nextup-client-discovery-01-' + phase)),
    'nextup_state_observer_device_already_exists');
  need(exact(input.libraries, ['LA', 'LB']) && Object.values(input.libraries).every(value =>
    exact(value, ['viewId', 'name']) && /^\d+$/.test(value.viewId) && typeof value.name === 'string' &&
    value.name.length > 0 && value.name.length <= 256));
  need(same(input.budgets, { maximumSeconds: 1200, normalSeconds: 840, maximumPlaybackAttempts: 2,
    permittedPlaybackAttempts: 0, recorderRequests: 34, recorderBodyBytes: 2097152, browserNavigationActions: 3 }));
  return input;
}

export function validateFreshOutput(value) {
  need(value.isDirectory === true && value.isSymbolicLink === false && value.uid === 0 &&
    (value.mode & 0o777) === 0o700 && value.realpath === ROOT && Array.isArray(value.entries) && value.entries.length === 0,
    'nextup_output_not_new_empty_owned_directory');
}

export function ownedStateRoutes(matrix) {
  const user = matrix.actors.P.userId;
  return [
    ...EPISODES.map(symbol => ({ symbol, kind: 'episode', route: '/emby/Users/' + user + '/Items/' + matrix.items[symbol].id })),
    ...SUMMARIES.map(symbol => ({ symbol, kind: 'summary', route: '/emby/Users/' + user + '/Items/' + matrix.items[symbol].id })),
    { symbol: 'profile', kind: 'profile', route: '/emby/Users/' + user },
    { symbol: 'preferences', kind: 'preferences', route: '/emby/UserSettings/' + user },
  ];
}

export function compareOwnedState(before, after) {
  const changes = [];
  for (const symbol of [...EPISODES, ...SUMMARIES]) if (!same(before[symbol], after[symbol])) changes.push(symbol);
  for (const field of ['Id', 'Name', 'Policy', 'Configuration'])
    if (!same(before.profile[field], after.profile[field])) changes.push('profile.' + field);
  if (!same(before.preferences, after.preferences)) changes.push('preferences');
  return { preserved: changes.length === 0, changed: changes,
    full_profile_equal: same(before.profile, after.profile),
    profile_comparison: 'Full profiles retained; acceptance compares Id, Name, Policy, and Configuration.' };
}

export function summarizeNextUp(physical, frames, tokenHash) {
  const global = physical.filter(value => typeof value.route === 'string' && value.route.replace(/\/$/, '') === '/Shows/NextUp' &&
    value.method === 'GET' && value.completed && value.status === 200 && value.token_sha256 === tokenHash &&
    Array.isArray(value.exact_public_query) && !value.exact_public_query.some(([name]) => name.toLowerCase() === 'seriesid'));
  return global.map(value => {
    const candidates = frames.filter(frame => frame.request_sha256 === value.request_sha256 &&
      frame.token_sha256 === tokenHash && frame.completed && frame.status === 200 && frame.sourceworker === false &&
      frame.from_service_worker === false && frame.main_frame === true &&
      frame.frame_body?.decoded_body_sha256 === value.response?.decoded_body_sha256 &&
      frame.frame_body?.decoded_body_bytes === value.response?.decoded_bytes && SHA.test(frame.frame_body?.decoded_body_sha256 ?? '') &&
      frame.document_id === value.document_id && frame.phase === value.phase);
    const samePhysical = global.filter(other => other.request_sha256 === value.request_sha256 &&
      other.document_id === value.document_id && other.phase === value.phase);
    const items = value.response?.json?.Items;
    return { physical_id: value.id, frame_id: candidates.length === 1 && samePhysical.length === 1 ? candidates[0].id : null,
      actual_client_response_bound: candidates.length === 1 && samePhysical.length === 1,
      request_sha256: value.request_sha256, token_sha256: tokenHash, exact_public_query: value.exact_public_query,
      frame_decoded_body_sha256: candidates.length === 1 ? candidates[0].frame_body.decoded_body_sha256 : null,
      navigation_cause: value.phase, document_id: value.document_id, status: value.status,
      ordered_ids: Array.isArray(items) ? items.map(item => item.Id) : null,
      response: value.response, response_elapsed_ms: value.response_elapsed_ms, finished_elapsed_ms: value.finished_elapsed_ms,
      positive: Array.isArray(items) && items.length > 0 };
  });
}

async function privateFile(filename, expectedHash = null, max = 32 * 1024 * 1024) {
  need(typeof filename === 'string' && filename.startsWith(WORK + '/') && !filename.split('/').includes('..'));
  const ancestors = []; let parent = path.dirname(filename);
  while (parent !== '/' && parent !== '.') { ancestors.push(parent); parent = path.dirname(parent); }
  for (const directory of ancestors) { const info = await fs.lstat(directory); need(info.isDirectory() && !info.isSymbolicLink() && info.uid === 0 && !(info.mode & 0o022)); }
  const handle = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const before = await handle.stat(); need(before.isFile() && before.uid === 0 && before.nlink === 1 &&
      !(before.mode & 0o077) && before.size > 0 && before.size <= max);
    const bytes = await handle.readFile(), after = await handle.stat();
    need(before.size === after.size && before.mtimeMs === after.mtimeMs && bytes.length === before.size &&
      (!expectedHash || sha(bytes) === expectedHash));
    return { bytes, value: JSON.parse(bytes.toString('utf8')) };
  } finally { await handle.close(); }
}

async function sourceFile(descriptorValue) {
  const info = await fs.lstat(descriptorValue.path);
  need(info.isFile() && !info.isSymbolicLink() && info.uid === 0 && !(info.mode & 0o022) &&
    await fs.realpath(descriptorValue.path) === descriptorValue.path);
  need(sha(await fs.readFile(descriptorValue.path)) === descriptorValue.sha256, 'nextup_source_changed');
}

async function processMetadata(pid) {
  const root = '/proc/' + pid;
  const readStat = async () => { const raw = await fs.readFile(root + '/stat', 'utf8'); return raw.slice(raw.lastIndexOf(')') + 2).trim().split(/\s+/); };
  const before = await readStat();
  const [cmdline, cgroup, status, exe, networkNamespace, executable] = await Promise.all([
    fs.readFile(root + '/cmdline'), fs.readFile(root + '/cgroup', 'utf8'), fs.readFile(root + '/status', 'utf8'),
    fs.readlink(root + '/exe'), fs.readlink(root + '/ns/net'), fs.stat(root + '/exe')]);
  const after = await readStat(); need(before[19] === after[19] && before[0] !== 'Z' && after[0] !== 'Z');
  return { pid, startTicks: before[19], cmdline: cmdline.toString().split('\0').filter(Boolean), cgroup,
    uid: Number(/^Uid:\s+(\d+)/m.exec(status)?.[1]), exe, networkNamespace,
    executable: Object.fromEntries(['dev', 'ino', 'mode', 'nlink', 'uid', 'gid', 'size', 'mtimeMs', 'ctimeMs'].map(key => [key, executable[key]])) };
}

async function ownedLock(expected) {
  const info = await fs.lstat(expected.path);
  need(info.isFile() && !info.isSymbolicLink() && info.dev === expected.device && info.ino === expected.inode,
    'nextup_fixture_lock_changed');
  const lines = (await fs.readFile('/proc/locks', 'utf8')).trim().split('\n');
  const device = BigInt(info.dev), major = Number((device >> 8n & 0xfffn) | (device >> 32n & 0xfffff000n)),
    minor = Number((device & 0xffn) | (device >> 12n & 0xffffff00n));
  const own = lines.filter(line => {
    const fields = line.trim().split(/\s+/);
    const [deviceMajor, deviceMinor, inode] = (fields[5] ?? '').split(':');
    return fields[1] === 'FLOCK' && fields[3] === 'WRITE' && Number(fields[4]) === process.pid &&
      parseInt(deviceMajor, 16) === major && parseInt(deviceMinor, 16) === minor && Number(inode) === expected.inode;
  });
  need(own.length === 1, 'nextup_driver_requires_existing_exclusive_flock');
}

export async function runDiscovery(filename, inputHash) {
  need(process.platform === 'linux' && process.getuid?.() === 0 && SHA.test(inputHash) &&
    !process.env.DEBUG && !process.env.PWDEBUG, 'nextup_remote_only');
  process.umask(0o077);
  const input = validateInput((await privateFile(filename, inputHash)).value);
  need(fileURLToPath(import.meta.url) === input.sourceClosure[FILES[0]].path, 'nextup_driver_not_frozen');
  for (const item of Object.values(input.sourceClosure)) await sourceFile(item);
  const execution = (await privateFile(input.execution.path, input.execution.sha256)).value;
  const release = (await privateFile(input.release.path, input.release.sha256)).value;
  need(exact(release, ['kind', 'version', 'execution', 'matrixTerminal', 'independentTerminal', 'independentRuntimeTerminal', 'operatorCommit', 'apiClosed', 'revokedActors',
    'zeroBaselineVerified', 'unitInactive', 'originalClientAllowed']) &&
    release.kind === 'nextup-reference-client-discovery-release' && release.version === 1 && same(release.execution, input.execution) &&
    descriptor(release.matrixTerminal) && descriptor(release.independentTerminal) && descriptor(release.independentRuntimeTerminal) &&
    descriptor(release.operatorCommit) && release.apiClosed === true &&
    same(release.revokedActors, ['P', 'Q']) && release.zeroBaselineVerified === true && release.unitInactive === true &&
    release.originalClientAllowed === true, 'nextup_matrix_release_missing');
  await privateFile(release.matrixTerminal.path, release.matrixTerminal.sha256);
  await privateFile(release.independentTerminal.path, release.independentTerminal.sha256);
  await privateFile(release.independentRuntimeTerminal.path, release.independentRuntimeTerminal.sha256);
  await privateFile(release.operatorCommit.path, release.operatorCommit.sha256);
  const matrix = execution.matrix, identity = execution.process;
  need(matrix?.target === 'reference' && matrix.serverId === REFERENCE_SERVER && matrix.actors?.P?.userId === REFERENCE_VIEWER &&
    matrix.binding?.endpoint === REFERENCE_ORIGIN && descriptor(execution.credentials) &&
    execution.lock?.path === WORK + '/reference.lock');
  need(same(execution.endpoint, { scheme: 'http', host: '127.0.0.1', port: 18197 }));
  for (const symbol of ['LA', 'LB']) need(input.libraries[symbol].viewId === matrix.libraries[symbol].viewId);
  const credentialDocument = (await privateFile(execution.credentials.path, execution.credentials.sha256)).value;
  const credential = credentialDocument.actors?.P;
  need(record(credential) && ['credentialRef', 'userId', 'username'].every(key => credential[key] === matrix.actors.P[key]) &&
    typeof credential.password === 'string' && credential.password.length >= 32);
  const bootId = (await fs.readFile('/proc/sys/kernel/random/boot_id', 'utf8')).trim();
  const started = process.hrtime.bigint();
  const elapsed = () => Number((process.hrtime.bigint() - started) / 1000000n);
  const report = { kind: 'nextup-reference-client-discovery-report', version: 1, run_id: input.runId,
    input_sha256: inputHash, source_sha256: Object.fromEntries(Object.entries(input.sourceClosure).map(([key, value]) => [key, value.sha256])),
    outcome: 'in_progress', node_version: process.version, playback_attempts: 0, elapsed_ms: 0, recorder_requests: 0, pin_checks: 0,
    phases: [], state: {}, browser: {}, visible: [], nextup: [], cleanup: {}, failure: null,
    evidence_boundary: { initial_episode_history: 'verified_zero_after_matrix_cleanup', playback_attempts: 0,
      establishes: 'Actual original-client parameters, initialization, and Home/TV UI in zero history.',
      does_not_establish: 'Client results equivalent to watched matrix R1-R4, real playback, or playback-driven refresh.' } };
  const anchors = {}, secrets = [credential.password];
  let actor, browserStarted = false, recorderActive = false, sequence = 0, privateQueue = Promise.resolve(), queueFailed = false;
  const physical = new Map(), frames = new Map();
  const privateDirectory = ROOT + '/private';
  async function syncDirectory(directory) {
    const handle = await fs.open(directory, constants.O_RDONLY | constants.O_DIRECTORY);
    try { await handle.sync(); } finally { await handle.close(); }
  }
  const rootInfo = await fs.lstat(ROOT);
  need(rootInfo.isDirectory() && !rootInfo.isSymbolicLink() && rootInfo.uid === 0 && (rootInfo.mode & 0o777) === 0o700,
    'nextup_output_not_new_empty_owned_directory');
  validateFreshOutput({ isDirectory: rootInfo.isDirectory(), isSymbolicLink: rootInfo.isSymbolicLink(), uid: rootInfo.uid,
    mode: rootInfo.mode, realpath: await fs.realpath(ROOT), entries: await fs.readdir(ROOT) });
  await fs.mkdir(privateDirectory, { mode: 0o700 });
  await fs.mkdir(ROOT + '/export', { mode: 0o700 });
  await syncDirectory(privateDirectory); await syncDirectory(ROOT + '/export');
  await syncDirectory(ROOT);
  async function save(name, value) {
    need(/^[a-zA-Z0-9_.-]+$/.test(name));
    const bytes = Buffer.from(JSON.stringify(value) + '\n');
    const handle = await fs.open(privateDirectory + '/' + name, 'wx', 0o600);
    try { await handle.writeFile(bytes); await handle.sync(); } finally { await handle.close(); }
    await syncDirectory(privateDirectory);
    return { path: privateDirectory + '/' + name, sha256: sha(bytes) };
  }
  async function pin(cleanup = false) {
    need(elapsed() <= 1200000 && (cleanup || elapsed() < 840000), 'nextup_time_budget_exhausted');
    await ownedLock(execution.lock);
    need((await fs.readFile('/proc/sys/kernel/random/boot_id', 'utf8')).trim() === bootId);
    for (const role of ['application', 'endpoint']) {
      const expected = identity[role], observed = await processMetadata(expected.pid);
      need(expected.bootId === bootId && observed.startTicks === expected.startTicks && observed.uid === expected.uid &&
        observed.cgroup === expected.cgroup && observed.networkNamespace === expected.networkNamespace && same(observed.cmdline, expected.cmdline) &&
        observed.executable.dev === expected.exeDevice && observed.executable.ino === expected.exeInode, 'nextup_process_identity_changed');
      need(observed.exe === expected.exe || role === 'endpoint' && observed.exe === expected.exe + ' (deleted)' &&
        observed.executable.nlink === 0, 'nextup_executable_metadata_changed');
      if (anchors[role]) need(same(anchors[role], observed), 'nextup_process_anchor_changed');
      else anchors[role] = observed;
    }
    need(await fs.readlink('/proc/self/ns/net') === identity.workerNetworkNamespace);
    const sockets = (await fs.readdir('/proc/' + identity.endpoint.pid + '/fd')).map(name => '/proc/' + identity.endpoint.pid + '/fd/' + name);
    const links = await Promise.all(sockets.map(name => fs.readlink(name).catch(() => null)));
    need(links.includes('socket:[' + identity.endpoint.listener.socketInode + ']'), 'nextup_proxy_listener_changed');
    const tcp = (await fs.readFile('/proc/' + identity.endpoint.pid + '/net/tcp', 'utf8')).trim().split('\n').slice(1)
      .map(line => line.trim().split(/\s+/));
    const listener = tcp.filter(row => row[9] === identity.endpoint.listener.socketInode && row[1] === '0100007F:4715' && row[3] === '0A');
    need(listener.length === 1 && identity.endpoint.listener.port === 18197, 'nextup_proxy_listener_address_changed');
    report.pin_checks += 1;
  }
  async function phase(name) { report.phases.push({ name, elapsed_ms: elapsed() }); await save('phase-' + report.phases.length + '.json', report.phases.at(-1)); }
  async function call(label, method, route, body, token, device, expectedStatus, cleanup = false, captureLogin = null) {
    need(!browserStarted && recorderActive && report.recorder_requests < input.budgets.recorderRequests, 'nextup_protocol_browser_overlap');
    const allowed = ownedStateRoutes(matrix).some(value => value.route === route) || route === '/emby/Users/AuthenticateByName' || route === '/emby/Sessions/Logout';
    need(allowed && (method === 'GET' && body === null || method === 'POST' && ['/emby/Users/AuthenticateByName', '/emby/Sessions/Logout'].includes(route)));
    await pin(cleanup);
    const ordinal = ++sequence; report.recorder_requests += 1;
    const payload = body === null ? null : Buffer.from(new URLSearchParams(body).toString());
    const headers = { Accept: 'application/json', 'X-Emby-Authorization': 'Emby Client="Goby NextUp Client State", Device="Remote observer", DeviceId="' + device + '", Version="1"' };
    if (token) headers['X-Emby-Token'] = token;
    if (payload) { headers['Content-Type'] = 'application/x-www-form-urlencoded'; headers['Content-Length'] = String(payload.length); }
    const intent = await save('request-' + ordinal + '-intent.json', { label, method, route, body, headers,
      payload_base64: payload?.toString('base64') ?? null, token_sha256: token ? sha(token) : null, device,
      input_sha256: inputHash, elapsed_ms: elapsed() });
    let absoluteTimer;
    const response = await new Promise((resolve, reject) => {
      const request = http.request(REFERENCE_ORIGIN + route, { method, headers }, incoming => {
        const chunks = []; let size = 0;
        incoming.on('data', chunk => { size += chunk.length; if (size > input.budgets.recorderBodyBytes) request.destroy(new Error('nextup_body_limit')); else chunks.push(chunk); });
        incoming.on('aborted', () => reject(new Error('nextup_response_aborted')));
        incoming.on('error', () => reject(new Error('nextup_response_error')));
        incoming.on('end', () => incoming.complete ? resolve({ status: incoming.statusCode, bytes: Buffer.concat(chunks), headers: incoming.headers }) : reject(new Error('nextup_response_incomplete')));
      });
      request.setTimeout(15000, () => request.destroy(new Error('nextup_http_timeout')));
      absoluteTimer = setTimeout(() => request.destroy(new Error('nextup_http_absolute_deadline')),
        Math.max(1, Math.min(15000, (cleanup ? 1200000 : 840000) - elapsed())));
      request.on('error', () => reject(new Error('nextup_request_outcome_unknown')));
      request.end(payload);
    }).finally(() => clearTimeout(absoluteTimer));
    let decoded = null, parseFailed = false;
    try { if (expectedStatus === 200 && response.bytes.length) decoded = JSON.parse(response.bytes.toString('utf8')); }
    catch { parseFailed = true; }
    if (captureLogin && response.status === 200) captureLogin(decoded);
    const receipt = await save('request-' + ordinal + '-response.json', { intent, status: response.status,
      headers: response.headers, body_base64: response.bytes.toString('base64'), body_sha256: sha(response.bytes),
      json_parse_failed: parseFailed, elapsed_ms: elapsed() });
    need(response.status === expectedStatus, 'nextup_unexpected_state_status');
    need(!parseFailed, 'nextup_state_response_not_json');
    await pin(cleanup);
    return { data: decoded, receipt };
  }
  async function statePhase(name) {
    need(!browserStarted && !recorderActive); recorderActive = true;
    const device = 'goby-nextup-client-discovery-01-' + name;
    let token = null, tokenVerified = false, loginAttempted = false, closed = false;
    const state = {};
    try {
      await phase(name + '_state'); loginAttempted = true;
      const login = await call(name + '-login', 'POST', '/emby/Users/AuthenticateByName',
        { Username: credential.username, Pw: credential.password }, null, device, 200, name === 'after', value => {
          if (typeof value?.AccessToken === 'string' && value.AccessToken.length > 0 && value.AccessToken.length <= 4096) {
            token = value.AccessToken; secrets.push(token);
            tokenVerified = value.User?.Id === REFERENCE_VIEWER && value.ServerId === REFERENCE_SERVER && value.User.Policy?.IsAdministrator === false &&
              value.SessionInfo?.UserId === REFERENCE_VIEWER && value.SessionInfo?.DeviceId === device;
          }
        });
      need(typeof login.data?.AccessToken === 'string' && login.data.AccessToken.length > 0, 'nextup_recorder_token_missing');
      need(tokenVerified, 'nextup_recorder_actor_changed');
      for (const item of ownedStateRoutes(matrix)) {
        const response = await call(name + '-' + item.symbol, 'GET', item.route, null, token, device, 200, name === 'after');
        const value = response.data;
        if (item.kind === 'episode' || item.kind === 'summary') {
          const expected = matrix.items[item.symbol];
          need(value?.Id === expected.id && value.Type === expected.type && value.ParentId === expected.parentId && record(value.UserData), 'nextup_state_item_identity');
          if (expected.seriesId) need(value.SeriesId === expected.seriesId);
          if (expected.indexNumber !== undefined) need(value.IndexNumber === expected.indexNumber);
          if (expected.parentIndexNumber !== undefined) need(value.ParentIndexNumber === expected.parentIndexNumber);
          if (item.kind === 'episode') {
            need(value.RunTimeTicks === expected.runtimeTicks);
            if (name === 'before') need(value.UserData.Played === false && value.UserData.PlayCount === 0 && value.UserData.PlaybackPositionTicks === 0 &&
              (!Object.hasOwn(value.UserData, 'LastPlayedDate') || value.UserData.LastPlayedDate === null), 'nextup_before_not_zero');
          }
          state[item.symbol] = { Id: value.Id, Type: value.Type, ParentId: value.ParentId, SeriesId: value.SeriesId,
            IndexNumber: value.IndexNumber, ParentIndexNumber: value.ParentIndexNumber, RunTimeTicks: value.RunTimeTicks, UserData: value.UserData };
        } else { state[item.symbol] = value; }
      }
      need(state.profile.Id === REFERENCE_VIEWER && state.profile.Name === credential.username && state.profile.Policy?.IsAdministrator === false &&
        state.profile.Policy.EnableAllFolders === false && same([...state.profile.Policy.EnabledFolders].sort(), [...matrix.actors.P.allowedFolderIds].sort()));
      report.state[name] = await save(name + '-owned-state.json', state);
      return state;
    } finally {
      try {
        if (token && tokenVerified) {
          try {
            await call(name + '-logout', 'POST', '/emby/Sessions/Logout', null, token, device, 204, true);
            await call(name + '-invalid', 'GET', '/emby/Users/' + REFERENCE_VIEWER, null, token, device, 401, true);
            closed = true;
          } finally { report.cleanup[name + '_recorder'] = { token_sha256: sha(token), exact_token_rejected: closed }; }
        } else if (loginAttempted) report.cleanup[name + '_recorder'] = { ownership: 'unknown_login_outcome_or_principal', recovery_required: true };
      } finally {
        recorderActive = false;
        if (token && !closed) {
          report.cleanup[name + '_recorder'] = { ...report.cleanup[name + '_recorder'], recovery_required: true, token_sha256: sha(token) };
          try { await save(name + '-private-token-recovery.json', { token, actor_verified: tokenVerified, device, input_sha256: inputHash }); }
          catch { report.cleanup[name + '_recorder'].private_recovery_write_failed = true; }
        }
      }
      need(!loginAttempted || closed, 'nextup_recorder_cleanup_incomplete');
    }
  }
  async function visible(label) {
    await pin(); await actor.settled();
    const view = await actor.page.locator('body').evaluate(element => {
      const shown = node => Boolean(node.getClientRects().length) && getComputedStyle(node).visibility !== 'hidden';
      const identify = node => {
        for (let current = node, depth = 0; current && depth < 7; current = current.parentElement, depth += 1) {
          if (current.getAttribute('data-id')) return current.getAttribute('data-id');
          const href = current.getAttribute('href');
          if (href) { const match = /(?:[?&#]|\b)(?:id|parentId)=([^&#]+)/i.exec(href); if (match) return decodeURIComponent(match[1]); }
        }
        return null;
      };
      const controls = [...element.querySelectorAll('a,button,[role="button"],[role="tab"]')].filter(shown).slice(0, 1000).map(node => ({
        tag: node.tagName.toLowerCase(), text: (node.innerText || '').trim().slice(0, 256),
        label: node.getAttribute('aria-label'), title: node.getAttribute('title'), id: identify(node),
        section_label: (node.closest('section,.verticalSection,.homePageSection')?.querySelector('h1,h2,h3,[role="heading"]')?.innerText || '').trim().slice(0, 256),
        action: node.getAttribute('data-action'), href: node.getAttribute('href') }));
      return { headings: [...element.querySelectorAll('h1,h2,h3,[role="heading"]')].filter(shown).map(node => (node.innerText || '').trim()).slice(0, 80), controls };
    });
    const receipt = await save('visible-' + label + '.json', { label, phase: actor.report.phase, route: actor.page.url(), elapsed_ms: elapsed(), ...view });
    const screenshotPath = privateDirectory + '/visible-' + label + '.png';
    await actor.page.screenshot({ path: screenshotPath, fullPage: false, timeout: 10000 });
    await fs.chmod(screenshotPath, 0o600);
    const screenshotHandle = await fs.open(screenshotPath, 'r+');
    try { await screenshotHandle.sync(); } finally { await screenshotHandle.close(); }
    await syncDirectory(privateDirectory);
    const screenshotHash = sha(await fs.readFile(screenshotPath));
    report.visible.push({ label, elapsed_ms: elapsed(), receipt_sha256: receipt.sha256, headings: view.headings,
      screenshot_sha256: screenshotHash,
      cards: view.controls.filter(row => row.id && [...EPISODES, ...SUMMARIES].some(symbol => matrix.items[symbol].id === row.id))
        .map(({ text, label: aria, title, id, section_label }) => ({ text, label: aria, title, id, section_label })) });
    return view;
  }
  async function navigateLibrary(symbol) {
    const library = input.libraries[symbol];
    const view = await visible('before-' + symbol.toLowerCase());
    const observedViews = [...physical.values()].filter(row => row.completed && row.status === 200 && /\/Views\/?$/.test(row.route))
      .flatMap(row => row.response?.json?.Items ?? (Array.isArray(row.response?.json) ? row.response.json : []));
    need(observedViews.some(item => item.Id === library.viewId && item.Name === library.name), 'nextup_library_public_identity_missing');
    need(view.controls.some(row => row.id === library.viewId && (row.text === library.name || row.title === library.name)), 'nextup_library_visible_identity_missing');
    const selector = actor.page.locator('a:visible,button:visible,[role="button"]:visible').filter({ hasText: library.name });
    const indices = await selector.evaluateAll((elements, expected) => elements.flatMap((node, index) => {
      if ((node.innerText || '').trim() !== expected.name) return [];
      for (let current = node, depth = 0; current && depth < 7; current = current.parentElement, depth += 1) {
        const raw = current.getAttribute('data-id');
        if (raw) return raw === expected.viewId ? [index] : [];
        const href = current.getAttribute('href'), match = href && /(?:[?&#]|\b)(?:id|parentId)=([^&#]+)/i.exec(href);
        if (match) return decodeURIComponent(match[1]) === expected.viewId ? [index] : [];
      }
      return [];
    }), library);
    need(indices.length > 0 && indices.length <= 3, 'nextup_library_control_ambiguous');
    await phase('navigate_' + symbol.toLowerCase()); actor.setPhase('navigate_' + symbol.toLowerCase());
    await save('navigation-' + symbol + '-intent.json', { action: 'click_visible_library_title', symbol, viewId: library.viewId,
      candidateCount: indices.length, selectedIndex: indices[0], elapsed_ms: elapsed() });
    const beforeURL = actor.page.url();
    await selector.nth(indices[0]).click({ timeout: 10000 });
    await actor.page.waitForURL(value => value.href !== beforeURL, { timeout: 20000 });
    await actor.page.waitForTimeout(1500); const destination = await visible('after-' + symbol.toLowerCase());
    const route = new URL(actor.page.url()), hash = route.hash.replace(/^#!/, '').replace(/^#/, ''),
      query = new URLSearchParams(hash.includes('?') ? hash.slice(hash.indexOf('?') + 1) : '');
    need([...query].some(([key, value]) => ['parentid', 'topparentid', 'id', 'viewid'].includes(key.toLowerCase()) && value === library.viewId) &&
      destination.controls.some(row => [row.text, row.label, row.title].some(value => /^(?:Shows|Series)$/i.test(value ?? ''))),
      'nextup_tv_destination_unconfirmed');
    report.phases.push({ name: 'tv_destination_confirmed', symbol, view_id: library.viewId, elapsed_ms: elapsed() });
  }
  async function logoutVisible() {
    await pin(true);
    const observed = { route: actor.page.url(), elapsed_ms: elapsed(),
      manual_login_visible: await actor.page.getByText('Manual Login', { exact: true }).filter({ visible: true }).count() > 0,
      password_forms: await actor.page.locator('form:has(input[type="password"]:visible)').count(),
      visible_sign_in_buttons: await actor.page.getByRole('button', { name: 'Sign In', exact: true }).filter({ visible: true }).count() };
    need(observed.manual_login_visible || observed.password_forms > 0, 'nextup_logout_view_not_observed');
    const receipt = await save('logout-visible.json', observed), screenshotPath = privateDirectory + '/logout-visible.png';
    await actor.page.screenshot({ path: screenshotPath, fullPage: false, timeout: 10000 });
    await fs.chmod(screenshotPath, 0o600);
    const handle = await fs.open(screenshotPath, 'r+');
    try { await handle.sync(); } finally { await handle.close(); }
    await syncDirectory(privateDirectory);
    return { receipt_sha256: receipt.sha256, screenshot_sha256: sha(await fs.readFile(screenshotPath)),
      manual_login_visible: observed.manual_login_visible, password_forms: observed.password_forms,
      visible_sign_in_buttons: observed.visible_sign_in_buttons };
  }
  const enqueue = (name, value) => { privateQueue = privateQueue.then(() => save(name, value)).catch(() => { queueFailed = true; }); };
  let before = null, after = null;
  try {
    await pin(); await save('admission.json', { input_sha256: inputHash, execution: input.execution, release: input.release,
      process: anchors, pid: process.pid, boot_id: bootId, lock: execution.lock, browser_only_playback_attempts: 0 });
    before = await statePhase('before');
    await phase('browser_setup');
    actor = await createReferenceBrowserActor({ account: { id: REFERENCE_VIEWER, username: credential.username, password: credential.password },
      pin: () => pin(actor?.report.phase === 'ui_logout' || actor?.report.phase === 'closed'), report: report.browser,
      existingDeviceIDs: [...input.existingDeviceIds, 'goby-nextup-client-discovery-01-before', 'goby-nextup-client-discovery-01-after'],
      onLogin: async ({ token, proof }) => { need(!secrets.includes(token), 'nextup_browser_token_not_independent');
        secrets.push(token); await save('browser-login-private.json', { token, proof }); },
      beforePrivateMutation: async value => {
        await privateQueue;
        need(value.request.kind === 'logout' || !queueFailed, 'nextup_private_observer_write_failed');
        await save('browser-mutation-' + value.request.id + '-intent.json', value);
      },
      observer: {
        onHTTPFinished(value) { physical.set(value.id, value); },
        onFrameResponse(value) { frames.set(value.id, value); },
        onFrameFinished(value) { frames.set(value.id, value); },
        onPrivateResponse(value) { enqueue('browser-response-' + value.request.id + '.json', value); },
        onPrivateLoginResponse(value) { enqueue('browser-login-response-' + value.request.id + '.json', value); },
      } });
    browserStarted = true;
    await save('browser-login-intent.json', { actor: 'P', user_id: REFERENCE_VIEWER, input_sha256: inputHash, elapsed_ms: elapsed() });
    await actor.loginUI();
    actor.setPhase('home_after_login'); await visible('home-after-login');
    await navigateLibrary('LA');
    await phase('navigate_home'); actor.setPhase('navigate_home');
    await save('navigation-home-intent.json', { action: 'click_visible_home_link', elapsed_ms: elapsed() });
    const home = actor.page.getByRole('link', { name: 'Home', exact: true }).filter({ visible: true });
    need(await home.count() === 1, 'nextup_home_control_ambiguous');
    const beforeURL = actor.page.url(); await home.click({ timeout: 10000 });
    await actor.page.waitForURL(value => value.href !== beforeURL, { timeout: 20000 });
    await actor.page.waitForTimeout(1500); const homeView = await visible('home-between-libraries');
    need(/^#!?\/home(?:[/?]|$)/i.test(new URL(actor.page.url()).hash) && Object.values(input.libraries).every(library =>
      homeView.controls.some(row => row.id === library.viewId && (row.text === library.name || row.title === library.name))),
      'nextup_home_destination_unconfirmed');
    await navigateLibrary('LB');
    await actor.settled();
  } catch (error) {
    report.failure = /^[a-z0-9_]+$/.test(error.message ?? '') ? error.message : 'nextup_discovery_failed';
  } finally {
    if (actor) {
      try { await phase('browser_cleanup'); await save('browser-logout-intent.json', { token_sha256: actor.token_sha256, elapsed_ms: elapsed() });
        await actor.logoutUI(); report.cleanup.logout_visible = await logoutVisible(); } catch { report.failure ??= 'nextup_browser_logout_failed'; }
      try { await actor.close(); } catch { report.failure ??= 'nextup_browser_close_failed'; }
      browserStarted = false;
      report.cleanup.browser = { closed: actor.report.closed === true, ui_logout_status: actor.report.logout.status,
        exact_token_status: actor.report.logout.post_logout_status, login_view_visible: actor.report.logout.login_view_visible,
        cleanup_failures: actor.report.cleanup_failures, visible_receipt: report.cleanup.logout_visible ?? null };
      if (report.cleanup.browser.closed !== true || report.cleanup.browser.ui_logout_status !== 204 || report.cleanup.browser.exact_token_status !== 401 ||
        report.cleanup.browser.login_view_visible !== true || actor.report.cleanup_failures?.length) report.failure ??= 'nextup_browser_cleanup_incomplete';
      report.nextup = summarizeNextUp([...physical.values()], [...frames.values()], actor.report.token_sha256);
      report.client = { playwright_version: actor.report.playwright_version ?? null, browser_version: actor.report.browser_version ?? null,
        login_proof: actor.report.login.proof ?? null, ui_logout: report.cleanup.browser };
      try { await save('browser-network-private.json', { physical: [...physical.values()], frames: [...frames.values()], snapshot: actor.snapshot() }); }
      catch { report.failure ??= 'nextup_browser_evidence_write_failed'; }
    }
    await privateQueue; if (queueFailed) report.failure ??= 'nextup_private_observer_write_failed';
    if (before && (!actor || report.cleanup.browser?.exact_token_status === 401 && report.cleanup.browser?.closed &&
      report.cleanup.browser.ui_logout_status === 204 && report.cleanup.browser.login_view_visible === true &&
      Array.isArray(report.cleanup.browser.cleanup_failures) && report.cleanup.browser.cleanup_failures.length === 0)) {
      try { after = await statePhase('after'); report.state.comparison = compareOwnedState(before, after);
        if (!report.state.comparison.preserved) report.failure ??= 'nextup_owned_state_drift'; }
      catch { report.failure ??= 'nextup_final_state_or_cleanup_failed'; }
    }
    try { await pin(true); } catch { report.failure ??= 'nextup_final_identity_failed'; }
    const bound = report.nextup.filter(value => value.actual_client_response_bound && Array.isArray(value.ordered_ids));
    report.outcome = report.failure ? 'failed' : !bound.length ? 'actual_client_global_request_unobserved' :
      bound.some(value => value.positive) ? 'client_positive_requires_separate_frozen_public_control' : 'reference_global_positive_unresolved';
    report.elapsed_ms = elapsed();
    if (report.elapsed_ms > 1200000) { report.failure ??= 'nextup_time_budget_exhausted'; report.outcome = 'failed'; }
    report.playback_attempts = 0;
    report.disposition = { accounts_libraries_media: 'retained', fresh_browser_device: 'retained', recorder_devices: 'retained',
      protocol_authentication_and_audit_history: 'retained', playback: 'none_authorized', preferences_restored: false };
    const terminal = await save('report.json', report);
    const sanitized = structuredClone(report);
    delete sanitized.browser; sanitized.state = { comparison: report.state.comparison ?? null,
      before_sha256: report.state.before?.sha256 ?? null, after_sha256: report.state.after?.sha256 ?? null };
    sanitized.private_report_sha256 = terminal.sha256;
    assertSafeExport(sanitized, secrets);
    const text = JSON.stringify(sanitized, null, 2) + '\n';
    const exportHandle = await fs.open(ROOT + '/export/report.json', 'wx', 0o600);
    try { await exportHandle.writeFile(text); await exportHandle.sync(); } finally { await exportHandle.close(); }
    await syncDirectory(ROOT + '/export');
  }
  return { outcome: report.outcome, failure: report.failure, elapsed_ms: report.elapsed_ms, playback_attempts: 0 };
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  const args = process.argv.slice(2);
  try {
    need(args.length === 4 && args[0] === '--input' && args[2] === '--input-sha256', 'nextup_invalid_arguments');
    const result = await runDiscovery(args[1], args[3]);
    process.stdout.write(JSON.stringify(result) + '\n');
    if (result.failure) process.exitCode = 1;
  } catch { process.stderr.write('nextup_discovery_failed_before_terminal\n'); process.exitCode = 1; }
}
