#!/usr/bin/env node
/** Run one explicitly bound candidate UI scenario without historical fixture loaders. */
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
import { createBrowserSessionProof } from './client-browser-session-proof.mjs';
import { runMovieWorkflow } from './client-browser-playback.mjs';
import { runAudioUI } from './client-browser-audio-flow.mjs';
import { runSubtitleUI } from './client-browser-subtitle-flow.mjs';
import { runTVBrowseUI } from './client-browser-tv-flow.mjs';

const SHA = /^[0-9a-f]{64}$/, ID = /^[0-9a-f]{32}$/;
const PLAYWRIGHT = '/opt/goby-test/inactive-dependencies-m5h/node_modules/playwright';
const SCENARIOS = ['movie', 'episode', 'mp3', 'flac', 'subtitles', 'tv-browse'];
export const GATEWAY_CLOSEOUT_SECONDS = 30;
export const REQUEST_HEADER_TIMEOUT_MS = 5000;
const sha = value => createHash('sha256').update(value).digest('hex');
const object = value => value !== null && typeof value === 'object' && !Array.isArray(value);
const need = (value, reason = 'candidate_guard_rejected') => { if (!value) throw new Error(reason); };
const exact = (value, keys) => object(value) && Object.keys(value).sort().join('|') === [...keys].sort().join('|');
function canonical(value) {
  if (Array.isArray(value)) return value.map(canonical);
  return object(value) ? Object.fromEntries(Object.keys(value).sort().map(key => [key, canonical(value[key])])) : value;
}
const equal = (left, right) => JSON.stringify(canonical(left)) === JSON.stringify(canonical(right));
const descriptor = value => exact(value, ['path', 'sha256']) && typeof value.path === 'string' &&
  value.path.startsWith('/') && !value.path.split('/').includes('..') && SHA.test(value.sha256);
function bounded(promise, milliseconds, reason = 'candidate_operation_timeout') {
  let timer;
  return Promise.race([promise, new Promise((_, reject) => { timer = setTimeout(() => reject(new Error(reason)), milliseconds); })])
    .finally(() => clearTimeout(timer));
}
function origin(value) {
  const url = new URL(value);
  need(url.protocol === 'http:' && url.hostname === '127.0.0.1' &&
    !url.username && !url.password && !url.search && !url.hash && url.pathname === '/' && url.origin === value,
  'candidate_loopback_origin_required');
  return url;
}
function item(value, type, name) {
  need(object(value) && ID.test(value.id) && value.type === type && value.name === name &&
    typeof value.path === 'string' && value.path.startsWith('/') && !value.path.includes('\0'), 'candidate_item_mapping_required');
  if (['Movie', 'Episode', 'Audio'].includes(type)) need(Number.isSafeInteger(value.runtimeTicks) && value.runtimeTicks > 0 && SHA.test(value.mediaSha256));
}

export function validateCandidateManifest(value) {
  need(exact(value, ['kind', 'version', 'runId', 'scenario', 'clientUrl', 'browserOrigin', 'directOrigin', 'serverId',
    'actor', 'credentials', 'source', 'processes', 'catalog', 'budgets', 'output', 'approval', 'gatewayAttestation']));
  need(value.kind === 'audited-candidate-client-input' && value.version === 1 && /^[a-z0-9][a-z0-9-]{1,79}$/.test(value.runId) &&
    SCENARIOS.includes(value.scenario) && ID.test(value.serverId));
  const browser = origin(value.browserOrigin), direct = origin(value.directOrigin), client = new URL(value.clientUrl);
  need(client.origin === browser.origin && client.pathname === '/web/index.html' && !client.search && !client.hash &&
    !client.username && !client.password, 'candidate_client_url_required');
  need(exact(value.actor, ['id', 'username']) && ID.test(value.actor.id) && typeof value.actor.username === 'string' &&
    value.actor.username.length > 0 && value.actor.username.length <= 128 && descriptor(value.credentials) && descriptor(value.approval) &&
    descriptor(value.gatewayAttestation));
  need(exact(value.source, ['manifestSha256', 'binarySha256', 'schema']) && SHA.test(value.source.manifestSha256) &&
    SHA.test(value.source.binarySha256) && Number.isSafeInteger(value.source.schema) && value.source.schema > 0);
  need(exact(value.processes, ['candidate', 'gateway']));
  for (const [name, identity] of Object.entries(value.processes)) {
    need(exact(identity, ['bootId', 'pid', 'startTicks', 'uid', 'exe', 'exeDevice', 'exeInode', 'cmdline', 'cgroup', 'networkNamespace', 'listener']) &&
      /^[a-f0-9-]{36}$/.test(identity.bootId) && Number.isSafeInteger(identity.pid) && identity.pid > 1 && /^\d+$/.test(identity.startTicks) &&
      Number.isSafeInteger(identity.uid) && identity.uid >= 0 && typeof identity.exe === 'string' && identity.exe.startsWith('/') &&
      Number.isSafeInteger(identity.exeDevice) && identity.exeDevice > 0 && Number.isSafeInteger(identity.exeInode) && identity.exeInode > 0 &&
      Array.isArray(identity.cmdline) && identity.cmdline.length > 0 && identity.cmdline.every(argument => typeof argument === 'string' && argument.length <= 4096 && !argument.includes('\0')) &&
      typeof identity.cgroup === 'string' && identity.cgroup.includes('/system.slice/') && /^net:\[\d+\]$/.test(identity.networkNamespace));
    const endpoint = name === 'candidate' ? direct : browser;
    need(exact(identity.listener, ['host', 'port', 'socketInode']) && identity.listener.host === '127.0.0.1' &&
      Number.isSafeInteger(identity.listener.port) && identity.listener.port === Number(endpoint.port || 80) &&
      /^\d+$/.test(identity.listener.socketInode), 'candidate_listener_binding_required');
  }
  need(exact(value.budgets, ['maximumSeconds', 'cleanupSeconds']) && Number.isSafeInteger(value.budgets.maximumSeconds) &&
    value.budgets.maximumSeconds >= 120 && value.budgets.maximumSeconds <= 1200 && Number.isSafeInteger(value.budgets.cleanupSeconds) &&
    value.budgets.cleanupSeconds >= 60 && value.budgets.cleanupSeconds < value.budgets.maximumSeconds);
  need(typeof value.output === 'string' && value.output.startsWith('/opt/goby-test/') && !value.output.split('/').includes('..'));
  need(object(value.catalog));
  if (['movie', 'subtitles'].includes(value.scenario)) {
    item(value.catalog.movie, 'Movie', 'M3e Client Movie');
    need(value.catalog.movie.runtimeTicks === 6000000000, 'candidate_movie_duration_required');
  }
  if (value.scenario === 'subtitles') {
    need(Array.isArray(value.catalog.subtitles) && value.catalog.subtitles.length === 2 &&
      value.catalog.subtitles.every((track, index) => exact(track, ['index', 'codec', 'language', 'external', 'sha256']) &&
        Number.isSafeInteger(track.index) && track.index >= 0 && track.codec === ['srt', 'vtt'][index] && ['en', 'eng'].includes(track.language) &&
        track.external === true && SHA.test(track.sha256)) && value.catalog.subtitles[0].index !== value.catalog.subtitles[1].index);
  }
  if (['mp3', 'flac'].includes(value.scenario)) {
    const audio = value.catalog[value.scenario];
    need(object(audio) && ['M3e Client Audio', value.scenario === 'mp3' ? 'M3e MP3' : 'M3e FLAC'].includes(audio.name));
    item(audio, 'Audio', audio.name);
    need(audio.runtimeTicks === 1800000000 && audio.container === value.scenario);
    need(exact(value.catalog.musicLibrary, ['id', 'name']) && ID.test(value.catalog.musicLibrary.id) && value.catalog.musicLibrary.name === 'M3e Client Music');
    need(exact(value.catalog.album, ['id', 'name']) && ID.test(value.catalog.album.id) && ['Music', 'M3e Synthetic Album'].includes(value.catalog.album.name));
  }
  if (['episode', 'tv-browse'].includes(value.scenario)) {
    need(exact(value.catalog.tvLibrary, ['id', 'name']) && ID.test(value.catalog.tvLibrary.id) && value.catalog.tvLibrary.name === 'M3e Client Television');
    item(value.catalog.series, 'Series', 'M3e Client Series');
    need(Array.isArray(value.catalog.seasons) && value.catalog.seasons.length === 2 && Array.isArray(value.catalog.episodes) && value.catalog.episodes.length === 3);
    for (let index = 0; index < 2; index += 1) {
      const season = value.catalog.seasons[index];
      need(object(season) && ID.test(season.id) && season.type === 'Season' && season.indexNumber === index + 1 &&
        season.seriesId === value.catalog.series.id && season.parentId === value.catalog.series.id);
    }
    for (const [index, numbers] of [[0, [1, 1]], [1, [1, 2]], [2, [2, 1]]]) {
      const episode = value.catalog.episodes[index];
      item(episode, 'Episode', 'Episode ' + numbers.join('-'));
      need(episode.seriesId === value.catalog.series.id && episode.parentIndexNumber === numbers[0] && episode.indexNumber === numbers[1] &&
        episode.parentId === value.catalog.seasons[numbers[0] - 1].id && episode.runtimeTicks >= 1200000000);
    }
    need(new Set([value.catalog.series.id, ...value.catalog.seasons.map(row => row.id), ...value.catalog.episodes.map(row => row.id)]).size === 6);
  }
  return value;
}

export function selectedCandidateItem(manifest) {
  if (manifest.scenario === 'tv-browse') return null;
  return manifest.scenario === 'episode' ? manifest.catalog.episodes[2] :
    ['movie', 'subtitles'].includes(manifest.scenario) ? manifest.catalog.movie : manifest.catalog[manifest.scenario] ?? null;
}

export function candidateRequestScope(raw, method, manifest) {
  try {
    if (/^(?:data|blob):/.test(raw)) return { allowed: true, kind: 'non_network' };
    const url = new URL(raw), endpoint = url.protocol === 'ws:' ? 'http://' + url.host : url.origin;
    if (endpoint !== manifest.browserOrigin || !['http:', 'ws:'].includes(url.protocol) || url.username || url.password || url.hash) return { allowed: false, kind: 'external' };
    const route = decodeURIComponent(url.pathname).replace(/^\/emby(?=\/)/i, '');
    if (/[\\\x00-\x20]/.test(route)) return { allowed: false, kind: 'invalid' };
    if ([...url.searchParams].some(([key, value]) => key.toLowerCase() === 'userid' && value !== manifest.actor.id)) return { allowed: false, kind: 'foreign_user', route };
    const user = /^\/Users\/([^/]+)/i.exec(route)?.[1];
    if (user && !['public', 'authenticatebyname'].includes(user.toLowerCase()) && user !== manifest.actor.id) return { allowed: false, kind: 'foreign_user', route };
    if (url.protocol === 'ws:') return { allowed: method === 'GET', kind: 'websocket', route };
    const media = /^\/(?:Videos|Audio)\/([^/]+)\/(?:stream(?:\.[a-z0-9_-]+)?|universal(?:\.[a-z0-9_-]+)?|original\.[a-z0-9_-]+|(?:master|main)\.m3u8|hls(?:\/[^?]*)?)\/?$/i.exec(route);
    const mediaNamespace = /^\/(?:Videos|Audio)\/([^/]+)\//i.exec(route);
    const subtitle = /^\/Videos\/([^/]+)\/[^/]+\/Subtitles\/\d+(?:\/\d+)?\/Stream\.(?:srt|vtt)\/?$/i.exec(route);
    if (['GET', 'HEAD', 'OPTIONS'].includes(method) && subtitle) return { allowed: manifest.scenario === 'subtitles' &&
      subtitle[1] === manifest.catalog.movie?.id, kind: 'subtitle', route };
    if (['GET', 'HEAD', 'OPTIONS'].includes(method) && mediaNamespace && !media) return { allowed: false, kind: 'unsupported_media_route', route };
    if (/\/Download(?:\/|$)/i.test(route)) return { allowed: false, kind: 'download', route };
    if (['GET', 'HEAD', 'OPTIONS'].includes(method)) return media
      ? { allowed: manifest.scenario !== 'tv-browse' && media[1] === selectedCandidateItem(manifest)?.id, kind: 'media', route }
      : { allowed: true, kind: 'read', route };
    if (method !== 'POST') return { allowed: false, kind: 'mutation', route };
    if (/^\/Users\/AuthenticateByName\/?$/i.test(route)) return { allowed: true, kind: 'login', route };
    if (/^\/Sessions\/Logout\/?$/i.test(route)) return { allowed: true, kind: 'logout', route };
    if (/^\/Sessions\/Capabilities(?:\/Full)?\/?$/i.test(route)) return { allowed: true, kind: 'capabilities', route };
    const info = /^\/Items\/([a-f0-9]{32})\/PlaybackInfo\/?$/i.exec(route);
    if (info) return { allowed: (manifest.scenario === 'tv-browse' ? manifest.catalog.episodes.map(row => row.id)
      : [selectedCandidateItem(manifest)?.id]).includes(info[1]), kind: 'playback_info', route };
    if (/^\/Sessions\/Playing(?:\/(?:Progress|Stopped))?\/?$/i.test(route)) return { allowed: manifest.scenario !== 'tv-browse', kind: 'playback_report', route };
    return { allowed: false, kind: 'mutation', route };
  } catch { return { allowed: false, kind: 'invalid' }; }
}

export function validateCandidateLogin(value, manifest) {
  need(object(value) && typeof value.AccessToken === 'string' && value.AccessToken.length >= 16 && value.AccessToken.length <= 4096 &&
    value.ServerId === manifest.serverId && value.User?.Id === manifest.actor.id && value.User.Name === manifest.actor.username &&
    value.User.Policy?.IsAdministrator === false && value.SessionInfo?.UserId === manifest.actor.id &&
    typeof value.SessionInfo.Id === 'string' && value.SessionInfo.Id.length > 0 && typeof value.SessionInfo.DeviceId === 'string' &&
    value.SessionInfo.DeviceId.length > 0, 'candidate_login_identity_rejected');
  return { user_id: value.User.Id, server_id: value.ServerId, session_id: value.SessionInfo.Id,
    device_id: value.SessionInfo.DeviceId, client: value.SessionInfo.Client ?? null,
    client_version: value.SessionInfo.ApplicationVersion ?? null, token_sha256: sha(value.AccessToken) };
}

export function candidateRequestBody(scope, method, contentType, bytes) {
  if (!Buffer.isBuffer(bytes) || bytes.length > 1048576) return {};
  const type = typeof contentType === 'string' ? contentType : '';
  const playbackPlain = scope?.allowed === true && method === 'POST' &&
    ['playback_report', 'playback_info'].includes(scope.kind) && type.split(';', 1)[0].trim().toLowerCase() === 'text/plain';
  if (!/json/i.test(type) && !playbackPlain) return {};
  // This is an observation of the existing payload, never a request rewrite.
  try { return { body: JSON.parse(bytes.toString('utf8')) }; }
  catch { return { body_parse_failed: true }; }
}

export async function candidateLoginControls(form, manual, setPhase = () => {}) {
  const step = async (phase, operation) => {
    setPhase(phase);
    try { return await operation(); }
    catch (error) {
      if (error.message === 'candidate_time_budget_exhausted') throw error;
      throw new Error('candidate_login_' + phase + '_failed');
    }
  };
  await step('entry_wait', () => Promise.any([
    form.waitFor({ state: 'visible', timeout: 15000 }), manual.waitFor({ state: 'visible', timeout: 15000 }),
  ]));
  await step('manual_open', async () => {
    if (!await form.isVisible()) await manual.locator('xpath=..').getByRole('button').click({ timeout: 10000 });
  });
  // SPA navigation may finish its click before the manual form is mounted.
  await step('form_wait', () => form.waitFor({ state: 'visible', timeout: 10000 }));
  const user = form.locator('input[type="text"]:visible'), password = form.locator('input[type="password"]:visible');
  const submit = form.getByRole('button', { name: 'Sign In', exact: true });
  await step('controls_wait', () => Promise.all([user, password, submit].map(control => control.waitFor({ state: 'visible', timeout: 10000 }))));
  const counts = await step('controls_count', () => Promise.all([user.count(), password.count(), submit.count()]));
  need(counts[0] === 1, 'candidate_login_username_not_unique');
  need(counts[1] === 1, 'candidate_login_password_not_unique');
  need(counts[2] === 1, 'candidate_login_submit_not_unique');
  return { user, password, submit };
}

export async function candidateSignOutControl(control) {
  try { await control.waitFor({ state: 'visible', timeout: 10000 }); }
  catch (error) {
    if (error.message === 'candidate_time_budget_exhausted') throw error;
    throw new Error('candidate_logout_control_wait_failed');
  }
  need(await control.count() === 1, 'candidate_logout_control_not_unique');
  return control;
}

export async function candidateAuthenticationAction(control, response, kind, setPhase = () => {}) {
  const observed = response.catch(() => null);
  need(['login', 'logout'].includes(kind), 'candidate_authentication_action_invalid');
  setPhase('submit'); await control.click({ timeout: kind === 'login' ? 10000 : 8000 });
  setPhase('response'); const reply = await observed, expected = kind === 'login' ? 200 : 204;
  need(reply !== null, 'candidate_' + kind + '_response_unavailable');
  need(reply.status() === expected, 'candidate_' + kind + '_response_not_' + expected);
}

export async function candidateRequestHeaders(request, row, manifest, milliseconds = REQUEST_HEADER_TIMEOUT_MS) {
  need(Number.isSafeInteger(milliseconds) && milliseconds > 0 && milliseconds <= REQUEST_HEADER_TIMEOUT_MS, 'candidate_header_deadline_invalid');
  let headers;
  try { headers = await bounded(Promise.resolve().then(() => request.allHeaders()), milliseconds, 'candidate_request_headers_timeout'); }
  catch (error) {
    const reason = error.message === 'candidate_request_headers_timeout' ? error.message : 'candidate_request_headers_unavailable';
    let bootstrap = false;
    try {
      const url = new URL(row.url);
      bootstrap = row.allowed === true && row.origin === 'target' && row.scope === 'service_worker' && row.main_frame === false &&
        row.method === 'GET' && row.completed === true && row.failed !== true && row.kind === 'read' &&
        url.origin === manifest.browserOrigin && url.pathname === '/web/serviceworker.js' && !url.search && !url.hash && !url.username && !url.password;
    } catch { /* An invalid URL cannot qualify as bootstrap metadata. */ }
    row.metadata_unavailable = { operation: 'request_headers', reason, accepted_bootstrap: bootstrap };
    if (!bootstrap) throw new Error(reason);
    return null;
  }
  need(object(headers) && Object.values(headers).every(value => typeof value === 'string'), 'candidate_request_headers_invalid');
  return headers;
}

export function createCandidateObserverQueue({ elapsed = () => 0, onFailure = () => {}, onTimeout = () => {} } = {}) {
  const pending = new Set(), timedOut = new WeakSet();
  const snapshot = () => [...pending].map(({ ordinal, operation, started_elapsed_ms }) => ({ ordinal, operation, started_elapsed_ms }));
  return {
    snapshot,
    track(ordinal, operation, observe) {
      const entry = { ordinal, operation, started_elapsed_ms: elapsed(), promise: null };
      pending.add(entry);
      entry.promise = Promise.resolve().then(() => observe(value => { entry.operation = value; })).catch(error => {
        const reason = /^candidate_[a-z0-9_]+$/.test(error.message ?? '') ? error.message : 'candidate_observation_failed';
        onFailure({ ordinal: entry.ordinal, operation: entry.operation, started_elapsed_ms: entry.started_elapsed_ms,
          finished_elapsed_ms: elapsed(), reason });
      }).finally(() => pending.delete(entry));
      return entry.promise;
    },
    async drain(milliseconds = 15000) {
      need(Number.isSafeInteger(milliseconds) && milliseconds > 0 && milliseconds <= 15000, 'candidate_observer_deadline_invalid');
      need(![...pending].some(entry => timedOut.has(entry)), 'candidate_observer_previous_timeout');
      try { await bounded((async () => { while (pending.size) await Promise.all([...pending].map(entry => entry.promise)); })(),
        milliseconds, 'candidate_observer_drain_timeout'); }
      catch (error) {
        for (const entry of pending) timedOut.add(entry);
        onTimeout({ elapsed_ms: elapsed(), pending: snapshot() });
        throw error;
      }
    },
  };
}

export async function candidateOwnedUICleanup({ observe, stopMedia, logout, onFailure }) {
  for (const [operation, action] of [['observer', observe], ['media', stopMedia], ['logout', logout]]) {
    try { await action(); }
    catch (error) { onFailure(operation, error); }
  }
}

export function candidateLogoutProven(proof, tokenHash) {
  const entries = proof?.entries?.filter(row => row.token_fingerprint === tokenHash) ?? [];
  return proof?.observer_errors === 0 && proof?.logout_overflow === 0 && entries.length === 1 &&
    entries[0].result === 'logout_token_rejected' && entries[0].ui_request?.response_status === 204 &&
    entries[0].ui_request.client_request_finished === true && entries[0].ui_request.client_request_failed === false &&
    entries[0].verification?.status === 401 && entries[0].verification.result === 'token_rejected';
}

export function candidateRemainingMilliseconds(budgets, elapsedMilliseconds, cleanup = false) {
  need(Number.isFinite(elapsedMilliseconds) && elapsedMilliseconds >= 0, 'candidate_invalid_elapsed_time');
  const remaining = Math.floor((budgets.maximumSeconds - (cleanup ? 0 : budgets.cleanupSeconds)) * 1000 - elapsedMilliseconds);
  need(remaining > 0, 'candidate_time_budget_exhausted');
  return remaining;
}

export function validateCandidateGateway(gateway, manifest, currentMonotonicNs, usage) {
  const { listener: candidateListener, ...candidateProcess } = manifest.processes.candidate;
  const { listener: gatewayListener, ...gatewayProcess } = manifest.processes.gateway;
  need(gateway?.runId === manifest.runId && gateway.proxyOrigin === manifest.browserOrigin && gateway.browserOrigin === manifest.browserOrigin &&
    gateway.directOrigin === manifest.directOrigin && SHA.test(gateway.inputSha256) && descriptor(gateway.sources?.gateway) && descriptor(gateway.sources?.proxy) &&
    gateway.policy?.version === 'core-av-transparent-gateway-v1' && typeof gateway.ledgerRoot === 'string' && gateway.ledgerRoot.startsWith('/opt/goby-test/') &&
    equal(gateway.process, gatewayProcess) && equal(gateway.listener, gatewayListener) &&
    equal(gateway.upstreams?.goby?.process, candidateProcess) && equal(gateway.upstreams?.goby?.listener, candidateListener) &&
    gateway.upstreams?.goby?.executableSha256 === manifest.source.binarySha256, 'candidate_gateway_binding_required');
  const limits = { maxRequests: [2, 10000], cleanupRequests: [1, 1000], maxApiBodyBytes: [1024, 1048576], maxApiTotalBytes: [1024, 67108864],
    maxSeconds: [30, 7200], idleSeconds: [10, 600], maxConcurrent: [1, 64] };
  need(exact(gateway.budgets, Object.keys(limits)) && Object.entries(limits).every(([key, range]) =>
    Number.isSafeInteger(gateway.budgets[key]) && gateway.budgets[key] >= range[0] && gateway.budgets[key] <= range[1]) &&
    gateway.budgets.cleanupRequests < gateway.budgets.maxRequests && gateway.budgets.maxApiTotalBytes >= gateway.budgets.maxApiBodyBytes,
  'candidate_gateway_budgets_invalid');
  const minimumCleanup = manifest.scenario === 'movie' ? 6 : manifest.scenario === 'tv-browse' ? 2 : 3;
  need(exact(usage, ['normal', 'cleanup']) && Object.values(usage).every(value => Number.isSafeInteger(value) && value >= 0) &&
    usage.normal <= gateway.budgets.maxRequests - gateway.budgets.cleanupRequests &&
    gateway.budgets.cleanupRequests - usage.cleanup >= minimumCleanup, 'candidate_gateway_cleanup_reserve_insufficient');
  need(typeof gateway.startedMonotonicNs === 'string' && typeof gateway.deadlineMonotonicNs === 'string' &&
    /^[1-9][0-9]*$/.test(gateway.startedMonotonicNs) && /^[1-9][0-9]*$/.test(gateway.deadlineMonotonicNs) &&
    typeof currentMonotonicNs === 'bigint' && currentMonotonicNs > 0n && gateway.process.bootId === manifest.processes.candidate.bootId,
  'candidate_gateway_monotonic_binding_required');
  const start = BigInt(gateway.startedMonotonicNs), deadline = BigInt(gateway.deadlineMonotonicNs);
  need(deadline - start === BigInt(gateway.budgets.maxSeconds) * 1000000000n && currentMonotonicNs >= start &&
    deadline - currentMonotonicNs >= BigInt(manifest.budgets.maximumSeconds + GATEWAY_CLOSEOUT_SECONDS) * 1000000000n,
  'candidate_gateway_remaining_lifetime_insufficient');
  return gateway;
}

export function candidatePlaybackEvidence(requests, manifest, sessions) {
  const selected = selectedCandidateItem(manifest);
  if (!selected) return { required: false, passed: true };
  const tokens = new Set(sessions.map(row => row.token_sha256)), sessionIds = new Set(sessions.map(row => row.session_id));
  const completed = requests.filter(row => row.completed && row.status >= 200 && row.status < 300 && tokens.has(row.token_sha256));
  const playback = completed.filter(row => row.kind === 'playback_report' && row.body?.ItemId === selected.id &&
    typeof row.body.PlaySessionId === 'string' && row.body.PlaySessionId.length > 0 && typeof row.body.MediaSourceId === 'string' &&
    row.body.MediaSourceId.length > 0 && (!row.body.SessionId || sessionIds.has(row.body.SessionId)));
  const starts = playback.filter(row => /\/Playing\/?$/i.test(row.route)), stops = playback.filter(row => /\/Stopped\/?$/i.test(row.route));
  const pairs = [], seen = new Set();
  for (const start of starts) {
    const key = start.token_sha256 + ':' + start.body.PlaySessionId;
    const stop = stops.find(stop => stop.body.PlaySessionId === start.body.PlaySessionId && stop.body.MediaSourceId === start.body.MediaSourceId &&
      stop.token_sha256 === start.token_sha256 && stop.ordinal > start.ordinal);
    if (stop && !seen.has(key)) { seen.add(key); pairs.push({ started: start.ordinal, stopped: stop.ordinal, item_id: selected.id,
      media_source_id: start.body.MediaSourceId, play_session_id: start.body.PlaySessionId, token_sha256: start.token_sha256 }); }
  }
  const media = completed.filter(row => row.kind === 'media' && new RegExp('^/(?:Videos|Audio)/' + selected.id + '/','i').test(row.route));
  return { required: true, passed: pairs.length >= (manifest.scenario === 'movie' ? 2 : 1) && media.length > 0 &&
      (manifest.scenario !== 'movie' || new Set(pairs.map(row => row.token_sha256)).size === 2),
    event_scope: 'BrowserContext frame and Service Worker events; physical request cardinality belongs to the gateway ledger.',
    item_id: selected.id, lifecycles: pairs, media_request_ordinals: media.map(row => row.ordinal),
    progress_request_ordinals: playback.filter(row => /\/Progress\/?$/i.test(row.route)).map(row => row.ordinal) };
}

async function readPrivate(description) {
  need(descriptor(description));
  const handle = await fs.open(description.path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const stat = await handle.stat();
    need(stat.isFile() && stat.uid === 0 && stat.nlink === 1 && (stat.mode & 0o777) === 0o600 && stat.size > 0 && stat.size <= 2097152);
    const bytes = await handle.readFile(); need(sha(bytes) === description.sha256);
    return JSON.parse(bytes.toString('utf8'));
  } finally { await handle.close(); }
}

async function freshGatewayLedger(gateway) {
  need(typeof gateway.ledgerRoot === 'string' && gateway.ledgerRoot.startsWith('/opt/goby-test/') &&
    await fs.realpath(gateway.ledgerRoot) === gateway.ledgerRoot, 'candidate_gateway_ledger_path_invalid');
  for (const directory of [gateway.ledgerRoot, path.join(gateway.ledgerRoot, 'private')]) {
    const info = await fs.lstat(directory);
    need(info.isDirectory() && !info.isSymbolicLink() && info.uid === 0 && (info.mode & 0o777) === 0o700,
      'candidate_gateway_ledger_ownership_changed');
  }
  need(equal((await fs.readdir(gateway.ledgerRoot)).sort(), ['gateway-attestation.json', 'private']) &&
    (await fs.readdir(path.join(gateway.ledgerRoot, 'private'))).length === 0, 'candidate_gateway_ledger_already_used');
  return { normal: 0, cleanup: 0 };
}

export async function runAuditedCandidate({ manifestPath, manifestSha256 }) {
  need(process.platform === 'linux' && process.getuid?.() === 0 && !process.env.DEBUG && !process.env.PWDEBUG, 'candidate_remote_only');
  process.umask(0o077);
  const manifest = validateCandidateManifest(await readPrivate({ path: manifestPath, sha256: manifestSha256 }));
  const approval = await readPrivate(manifest.approval);
  need(approval?.kind === 'audited-candidate-client-approval' && approval.runId === manifest.runId && approval.scenario === manifest.scenario &&
    approval.sourceManifestSha256 === manifest.source.manifestSha256 && approval.binarySha256 === manifest.source.binarySha256 &&
    approval.serverId === manifest.serverId && approval.isolatedCandidate === true, 'candidate_independent_approval_required');
  const gateway = await readPrivate(manifest.gatewayAttestation);
  const gatewayUsage = await freshGatewayLedger(gateway);
  validateCandidateGateway(gateway, manifest, process.hrtime.bigint(), gatewayUsage);
  const credential = await readPrivate(manifest.credentials);
  need(exact(credential, ['actorId', 'serverId', 'username', 'password']) && credential.actorId === manifest.actor.id &&
    credential.serverId === manifest.serverId && credential.username === manifest.actor.username && typeof credential.password === 'string' &&
    credential.password.length >= 16 && credential.password.length <= 4096, 'candidate_credential_binding_required');
  const outputInfo = await fs.lstat(manifest.output);
  need(outputInfo.isDirectory() && !outputInfo.isSymbolicLink() && outputInfo.uid === 0 && (outputInfo.mode & 0o777) === 0o700 &&
    await fs.realpath(manifest.output) === manifest.output && (await fs.readdir(manifest.output)).length === 0, 'candidate_fresh_output_required');
  const started = process.hrtime.bigint(), elapsed = () => Number((process.hrtime.bigint() - started) / 1000000n);
  const normalLimit = (manifest.budgets.maximumSeconds - manifest.budgets.cleanupSeconds) * 1000;
  const report = { kind: 'audited-candidate-client-report', version: 1, run_id: manifest.runId, scenario: manifest.scenario,
    manifest_sha256: manifestSha256, source: manifest.source, started_at: new Date().toISOString(), started_monotonic_ns: started.toString(), requests: [], sessions: [],
    snapshots: [], page_errors: [], blocked_external: [], external_attempts: [], scope_observations: [], observed_rejections: [],
    logout: { attempted: false }, cleanup: {}, observer: { failures: [], drain_timeouts: [], pending: [] }, failure: null,
    browser_environment: { service_workers: 'allow', fresh_context: true, proxy_bypass: '<-loopback>', quic: 'disabled', nonproxied_webrtc_udp: 'disabled' },
    gateway: { attestation_sha256: manifest.gatewayAttestation.sha256, input_sha256: gateway.inputSha256,
      ledger_root: gateway.ledgerRoot, started_monotonic_ns: gateway.startedMonotonicNs, deadline_monotonic_ns: gateway.deadlineMonotonicNs,
      closeout_reserve_seconds: GATEWAY_CLOSEOUT_SECONDS, admission_request_counts: gatewayUsage,
      admission_cleanup_remaining: gateway.budgets.cleanupRequests - gatewayUsage.cleanup,
      external_validation: 'pending_outer_gateway_closure' },
    evidence_boundary: 'Original UI and frame/Service Worker context observations; physical gateway validation and P/Q state checks are external. No vendor asset bodies are read.' };
  let browser, context, page, sessionProof, activeSession, episodeLocation = null, cleanupMode = false, requestOrdinal = 0;
  const secrets = [credential.password], entries = new WeakMap(), processAnchors = {};
  const remaining = () => candidateRemainingMilliseconds(manifest.budgets, elapsed(), cleanupMode);
  const observations = createCandidateObserverQueue({ elapsed,
    onFailure: failure => { report.observer.failures.push(failure); report.failure ??= failure.reason; },
    onTimeout: timeout => report.observer.drain_timeouts.push(timeout) });
  // Existing UI functions keep their own control logic. Bound each native action
  // by its remaining phase budget instead of racing an entire live workflow.
  const wrappedObjects = new WeakMap();
  function budgetedUI(target, kind = 'page') {
    if (wrappedObjects.has(target)) return wrappedObjects.get(target);
    const optionsAt = kind === 'locator'
      ? { click: 0, dblclick: 0, fill: 1, press: 1, waitFor: 0, hover: 0, selectOption: 1, check: 0, uncheck: 0 }
      : { goto: 1, waitForURL: 1, waitForFunction: 2, screenshot: 0 };
    const proxy = new Proxy(target, { get(actual, key) {
      const value = Reflect.get(actual, key, actual);
      if (['mouse', 'keyboard'].includes(key)) return budgetedUI(value, key);
      if (typeof value !== 'function') return value;
      return (...args) => {
        if (key === 'waitForTimeout') {
          const available = remaining(), requested = args[0];
          return value.call(actual, Math.min(requested, available)).then(() => { if (requested >= available) throw new Error('candidate_time_budget_exhausted'); });
        }
        if (Object.hasOwn(optionsAt, key)) {
          const index = optionsAt[key], options = args[index] ?? {};
          args[index] = { ...options, timeout: Math.min(options.timeout > 0 ? options.timeout : 30000, remaining()) };
        } else if (!['url', 'mainFrame', 'on', 'off', 'once', 'locator', 'getByRole', 'getByText', 'getByLabel', 'filter', 'first', 'last', 'nth'].includes(key)) remaining();
        const result = value.apply(actual, args);
        if (result && typeof result.click === 'function' && typeof result.count === 'function') return budgetedUI(result, 'locator');
        if (result && typeof result.then === 'function' && !Object.hasOwn(optionsAt, key) && key !== 'waitForTimeout')
          return bounded(result, remaining(), 'candidate_time_budget_exhausted');
        return result;
      };
    } });
    wrappedObjects.set(target, proxy); return proxy;
  }
  async function save(name, value) {
    need(/^[a-z0-9_.-]+$/i.test(name));
    const bytes = Buffer.from(JSON.stringify(value) + '\n'), handle = await fs.open(path.join(manifest.output, name), 'wx', 0o600);
    try { await handle.writeFile(bytes); await handle.sync(); } finally { await handle.close(); }
    const directory = await fs.open(manifest.output, constants.O_RDONLY | constants.O_DIRECTORY);
    try { await directory.sync(); } finally { await directory.close(); }
    return { filename: name, sha256: sha(bytes) };
  }
  const track = (ordinal, operation, observe) => observations.track(ordinal, operation, observe);
  async function drain() {
    await observations.drain(Math.min(15000, remaining()));
  }
  async function drainForCleanup() {
    try { await drain(); }
    catch (error) { report.failure ??= /^candidate_[a-z0-9_]+$/.test(error.message ?? '') ? error.message : 'candidate_observer_drain_failed'; }
  }
  async function pin() {
    need(elapsed() < (cleanupMode ? manifest.budgets.maximumSeconds * 1000 : normalLimit), 'candidate_time_budget_exhausted');
    const boot = (await fs.readFile('/proc/sys/kernel/random/boot_id', 'utf8')).trim();
    need(await fs.readlink('/proc/self/ns/net') === manifest.processes.gateway.networkNamespace, 'candidate_worker_network_namespace_changed');
    for (const [name, expected] of Object.entries(manifest.processes)) {
      const root = '/proc/' + expected.pid, raw = await fs.readFile(root + '/stat', 'utf8'), fields = raw.slice(raw.lastIndexOf(')') + 2).trim().split(/\s+/);
      const [exe, file, cgroup, namespace, cmdline] = await Promise.all([fs.readlink(root + '/exe'), fs.stat(root + '/exe'),
        fs.readFile(root + '/cgroup', 'utf8'), fs.readlink(root + '/ns/net'), fs.readFile(root + '/cmdline')]);
      need(boot === expected.bootId && fields[19] === expected.startTicks && fields[0] !== 'Z' && exe === expected.exe &&
        file.dev === expected.exeDevice && file.ino === expected.exeInode && cgroup === expected.cgroup && namespace === expected.networkNamespace &&
        JSON.stringify(cmdline.toString().split('\0').filter(Boolean)) === JSON.stringify(expected.cmdline),
      'candidate_process_binding_changed');
      const status = await fs.readFile(root + '/status', 'utf8'); need(Number(/^Uid:\s+(\d+)/m.exec(status)?.[1]) === expected.uid);
      const metadata = { dev: file.dev, ino: file.ino, size: file.size, mtime: file.mtimeMs, ctime: file.ctimeMs, mode: file.mode };
      if (processAnchors[name]) need(JSON.stringify(processAnchors[name]) === JSON.stringify(metadata));
      else { processAnchors[name] = metadata; if (name === 'candidate') need(sha(await fs.readFile(root + '/exe')) === manifest.source.binarySha256); }
      const tcp = (await fs.readFile(root + '/net/tcp', 'utf8')).trim().split('\n').slice(1).map(line => line.trim().split(/\s+/));
      const endpoint = '0100007F:' + expected.listener.port.toString(16).toUpperCase().padStart(4, '0');
      need(tcp.filter(row => row[1] === endpoint && row[3] === '0A' && row[9] === expected.listener.socketInode).length === 1);
      const links = await Promise.all((await fs.readdir(root + '/fd')).map(fd => fs.readlink(root + '/fd/' + fd).catch(() => null)));
      need(links.includes('socket:[' + expected.listener.socketInode + ']'));
      const after = await fs.readFile(root + '/stat', 'utf8');
      need(after.slice(after.lastIndexOf(')') + 2).trim().split(/\s+/)[19] === expected.startTicks, 'candidate_process_changed_during_pin');
    }
  }
  async function snapshot(label) {
    const location = page.url();
    if (label === 'tv-episode-detail') episodeLocation = location;
    const file = 'snapshot-' + report.snapshots.length + '.png';
    await page.screenshot({ path: path.join(manifest.output, file), fullPage: false, timeout: 10000 });
    await fs.chmod(path.join(manifest.output, file), 0o600);
    report.snapshots.push({ label, location, elapsed_ms: elapsed(), filename: file, sha256: sha(await fs.readFile(path.join(manifest.output, file))) });
  }
  function tokenFrom(headers, url) {
    const values = [headers['x-emby-token'], headers['x-mediabrowser-token'],
      ...[...new URL(url).searchParams].filter(([key]) => ['api_key', 'x-emby-token', 'x-mediabrowser-token', 'token', 'accesstoken', 'access_token'].includes(key.toLowerCase()))
        .map(([, value]) => value)].filter(Boolean);
    const authorization = headers['x-emby-authorization'] ?? headers['authorization'] ?? '';
    const match = /\bToken=(?:"([^"]+)"|([^,\s]+))/i.exec(authorization); if (match) values.push(match[1] ?? match[2]);
    need(new Set(values).size <= 1, 'candidate_conflicting_authority');
    return values[0] ?? null;
  }
  async function loginUI(initial = false) {
    const phase = value => { report.login_phase = value; };
    try {
      phase('pin'); await pin();
      const maximum = manifest.scenario === 'movie' ? 2 : 1;
      need(report.sessions.length < maximum && (!activeSession || activeSession.logged_out), 'candidate_login_budget_exhausted');
      phase('navigation');
      if (initial) await page.goto(manifest.clientUrl, { waitUntil: 'domcontentloaded', timeout: 30000 });
      const form = page.locator('form:has(input[type="password"]:visible)'), manual = page.getByText('Manual Login', { exact: true });
      const { user, password, submit } = await candidateLoginControls(form, manual, phase);
      phase('fill_credentials'); await user.fill(credential.username); await password.fill(credential.password);
      phase('intent'); await save('login-' + report.sessions.length + '-intent.json', { actor_id: manifest.actor.id, elapsed_ms: elapsed() });
      const count = report.sessions.length;
      phase('response_arm');
      const response = page.waitForResponse(row => candidateRequestScope(row.url(), row.request().method(), manifest).kind === 'login', { timeout: 15000 });
      await candidateAuthenticationAction(submit, response, 'login', phase);
      phase('form_hidden'); await password.waitFor({ state: 'hidden', timeout: 15000 });
      phase('observation'); await drain();
      need(report.sessions.length === count + 1 && activeSession && !report.failure, 'candidate_login_not_proven');
      phase('snapshot'); await snapshot('authenticated-' + count);
      phase('authenticated');
    } catch (error) {
      report.login_failure_phase = report.login_phase;
      const reason = /^candidate_[a-z0-9_]+$/.test(error.message ?? '') && error.message !== 'candidate_guard_rejected'
        ? error.message : 'candidate_login_' + report.login_phase + '_failed';
      throw new Error(reason);
    }
  }
  async function logoutUI() {
    if (!activeSession || activeSession.logged_out) return;
    const phase = value => { report.logout_phase = value; };
    try {
      phase('pin'); await pin();
      let signOut = page.getByRole('button', { name: /\bSign Out\b/i }).filter({ visible: true });
      phase('settings');
      if (!await signOut.count()) await page.getByRole('button', { name: 'Settings', exact: true }).filter({ visible: true }).click({ timeout: 8000 });
      phase('control_wait');
      signOut = await candidateSignOutControl(page.getByRole('button', { name: /\bSign Out\b/i }).filter({ visible: true }));
      phase('response_arm');
      const response = page.waitForResponse(row => candidateRequestScope(row.url(), row.request().method(), manifest).kind === 'logout', { timeout: 15000 });
      await candidateAuthenticationAction(signOut, response, 'logout', phase);
      phase('login_view');
      await Promise.any([page.getByText('Manual Login', { exact: true }).waitFor({ state: 'visible', timeout: 15000 }),
        page.locator('form:has(input[type="password"]:visible)').waitFor({ state: 'visible', timeout: 15000 })]);
      phase('observation'); await drain(); need(activeSession.logged_out, 'candidate_logout_not_observed');
      phase('snapshot'); await snapshot('logged-out'); phase('logged_out');
    } catch (error) {
      report.logout_failure_phase = report.logout_phase;
      const reason = /^candidate_[a-z0-9_]+$/.test(error.message ?? '') && error.message !== 'candidate_guard_rejected'
        ? error.message : 'candidate_logout_' + report.logout_phase + '_failed';
      throw new Error(reason);
    }
  }
  async function stopVisibleMedia() {
    if (!page) return;
    await drainForCleanup();
    const selected = selectedCandidateItem(manifest);
    const starts = report.requests.filter(row => row.kind === 'playback_report' && /\/Playing\/?$/i.test(row.route ?? '') && row.body?.ItemId === selected?.id && row.status >= 200 && row.status < 300);
    const unfinished = starts.filter(start => !report.requests.some(stop => stop.kind === 'playback_report' && /\/Stopped\/?$/i.test(stop.route ?? '') &&
      stop.body?.ItemId === selected?.id && stop.body.PlaySessionId === start.body.PlaySessionId && stop.token_sha256 === start.token_sha256 &&
      stop.completed && stop.status >= 200 && stop.status < 300));
    const active = await page.locator('video,audio').evaluateAll(rows => rows.some(row => !row.paused));
    if (!active && !unfinished.length) return;
    const matchingStops = start => report.requests.filter(stop => stop.kind === 'playback_report' && /\/Stopped\/?$/i.test(stop.route ?? '') &&
      stop.body?.ItemId === selected?.id && stop.body.PlaySessionId === start.body.PlaySessionId && stop.token_sha256 === start.token_sha256);
    const pendingStops = unfinished.filter(start => matchingStops(start).length > 0);
    if (!active && pendingStops.length) {
      const until = Math.min(elapsed() + 15000, manifest.budgets.maximumSeconds * 1000);
      while (pendingStops.some(start => !matchingStops(start).some(stop => stop.completed && stop.status >= 200 && stop.status < 300)) && elapsed() < until) {
        await page.waitForTimeout(100); await drainForCleanup();
      }
      need(pendingStops.every(start => matchingStops(start).some(stop => stop.completed && stop.status >= 200 && stop.status < 300)),
        'candidate_existing_stopped_report_unresolved');
      if (pendingStops.length === unfinished.length) return;
    }
    const video = page.locator('video:visible').first(), box = await video.count() ? await video.boundingBox() : null;
    if (box) await page.mouse.move(box.x + box.width / 2, box.y + box.height * 0.75);
    let clicked = false;
    for (const label of ['Stop', 'Close', 'Back']) {
      const control = page.getByRole('button', { name: label, exact: true }).filter({ visible: true });
      if (await control.count() === 1) { await control.click({ timeout: 8000 }); clicked = true; break; }
    }
    need(clicked, 'candidate_owned_stop_control_missing');
    await page.waitForFunction(() => [...document.querySelectorAll('video,audio')].every(row => row.paused), null, { timeout: 15000 });
    const deadline = Math.min(elapsed() + 15000, manifest.budgets.maximumSeconds * 1000);
    while (unfinished.some(start => !report.requests.some(stop => /\/Stopped\/?$/i.test(stop.route ?? '') && stop.body?.ItemId === selected?.id &&
      stop.body.PlaySessionId === start.body.PlaySessionId && stop.token_sha256 === start.token_sha256 && stop.completed && stop.status >= 200 && stop.status < 300)) && elapsed() < deadline) {
      await page.waitForTimeout(100); await drainForCleanup();
    }
    need(unfinished.every(start => report.requests.some(stop => /\/Stopped\/?$/i.test(stop.route ?? '') && stop.body?.ItemId === selected?.id &&
      stop.body.PlaySessionId === start.body.PlaySessionId && stop.token_sha256 === start.token_sha256 && stop.completed && stop.status >= 200 && stop.status < 300)),
    'candidate_owned_stopped_report_missing');
  }
  async function episodePlayback() {
    await runTVBrowseUI({ page, context, target: new URL(manifest.clientUrl), report, snapshot, library: manifest.catalog.tvLibrary.name });
    await drain();
    const episode = selectedCandidateItem(manifest);
    need(episodeLocation && report.tv_browse?.episode_detail?.season_and_episode_metadata_observed, 'candidate_episode_location_unobserved');
    need(report.requests.some(row => row.detail?.Id === episode.id && row.detail.Type === 'Episode'), 'candidate_episode_dto_unobserved');
    await save('episode-navigation-intent.json', { observed_location: episodeLocation, item_id: episode.id });
    await page.goto(episodeLocation, { waitUntil: 'domcontentloaded', timeout: 30000 });
    need(page.url() === episodeLocation); await page.getByRole('heading', { name: report.tv_browse.episode_detail.detail_heading_raw, exact: true }).waitFor({ state: 'visible', timeout: 15000 });
    const metrics = async () => {
      const values = await page.locator('video:visible').evaluateAll((rows, expected) => rows.map(row => {
        let sourceMatches = false;
        try { const url = new URL(row.currentSrc, document.baseURI); sourceMatches = url.origin === expected.origin &&
          new RegExp('^/(?:emby/)?Videos/' + expected.id + '/', 'i').test(url.pathname); } catch { /* Missing sources cannot prove identity. */ }
        return { current_time: row.currentTime, duration: row.duration, paused: row.paused, ready_state: row.readyState,
          width: row.videoWidth, frames: row.getVideoPlaybackQuality?.().totalVideoFrames ?? null, source_matches_item: sourceMatches };
      }), { origin: manifest.browserOrigin, id: episode.id });
      need(values.length === 1 && values[0].source_matches_item, 'candidate_unique_episode_video_unproven');
      return values;
    };
    const control = async label => { const found = page.getByRole('button', { name: label, exact: true }).filter({ visible: true }); need(await found.count() === 1); await found.click({ timeout: 8000 }); };
    const show = async () => { const box = await page.locator('video:visible').first().boundingBox(); need(box); await page.mouse.move(box.x + box.width / 2, box.y + box.height * 0.75); };
    const beginning = page.getByRole('button', { name: 'From Beginning', exact: true }).filter({ visible: true });
    if (await beginning.count() === 1) await beginning.click(); else await control('Play');
    await page.waitForFunction(() => [...document.querySelectorAll('video')].some(row => !row.paused && row.currentTime > 1 && row.videoWidth > 0), null, { timeout: 30000 });
    const first = (await metrics())[0]; await page.waitForTimeout(2200); const advanced = (await metrics())[0];
    need(first && advanced && advanced.current_time > first.current_time + 1 && advanced.frames > first.frames &&
      Math.abs(advanced.duration - episode.runtimeTicks / 10000000) <= 2);
    await show(); await control('Pause');
    await page.waitForFunction(() => [...document.querySelectorAll('video')].filter(row => row.getClientRects().length).every(row => row.paused), null, { timeout: 5000 });
    const paused = (await metrics())[0]; need(paused.paused);
    await page.waitForTimeout(700); const held = (await metrics())[0]; need(held.paused && Math.abs(held.current_time - paused.current_time) <= 0.35);
    const seeks = [];
    report.episode_phase = 'seek';
    for (const fraction of [0.30, 0.10]) {
      await show(); const slider = page.locator('input.videoOsdPositionSlider:visible'); need(await slider.count() === 1);
      const box = await slider.boundingBox(); need(box && box.width > 50); await slider.click({ position: { x: box.width * fraction, y: box.height / 2 } });
      await page.waitForTimeout(700); const current = (await metrics())[0];
      need(Math.abs(current.current_time - current.duration * fraction) <= Math.max(5, current.duration * 0.03)); seeks.push(current);
    }
    report.episode_phase = 'resume';
    await show(); if ((await metrics())[0].paused) await control('Play'); await page.waitForTimeout(2200); const resumed = (await metrics())[0];
    need(!resumed.paused && resumed.current_time > seeks[1].current_time + 1 && resumed.frames > seeks[1].frames);
    await snapshot('episode-playing'); await show(); await control('Back'); await page.waitForTimeout(1000);
    await stopVisibleMedia(); report.episode_playback = { item_id: episode.id, first, advanced, paused, held, seeks, resumed, stopped: true };
  }
  try {
    await pin();
    need(equal(await freshGatewayLedger(gateway), gatewayUsage), 'candidate_gateway_ledger_changed_before_browser');
    validateCandidateGateway(gateway, manifest, process.hrtime.bigint(), gatewayUsage);
    await save('admission.json', { manifest_sha256: manifestSha256, processes: processAnchors, source: manifest.source,
      gateway_attestation_sha256: manifest.gatewayAttestation.sha256, gateway_request_counts: gatewayUsage,
      gateway_cleanup_remaining: gateway.budgets.cleanupRequests - gatewayUsage.cleanup });
    const require = createRequire(import.meta.url), { chromium } = require(PLAYWRIGHT);
    browser = await chromium.launch({ headless: true, timeout: 30000, proxy: { server: gateway.proxyOrigin, bypass: '<-loopback>' },
      args: ['--no-sandbox', '--disable-dev-shm-usage', '--disable-background-networking',
      '--disable-quic', '--force-webrtc-ip-handling-policy=disable_non_proxied_udp'] });
    report.browser_version = browser.version(); report.playwright_version = require(path.join(PLAYWRIGHT, 'package.json')).version;
    context = await browser.newContext({ locale: 'en-US', viewport: { width: 1440, height: 1000 }, serviceWorkers: 'allow',
      proxy: { server: gateway.proxyOrigin, bypass: '<-loopback>' } });
    // Keep all context events below; only the proof helper sees logical main-frame logout events.
    const proofListeners = new Map();
    const proofContext = { pages: () => context.pages(),
      on(event, callback) {
        const wrapped = value => {
          const request = event === 'response' ? value.request() : value;
          if (request.serviceWorker() !== null) return;
          try { if (request.frame() !== page?.mainFrame()) return; } catch { return; }
          callback(value);
        };
        if (!proofListeners.has(event)) proofListeners.set(event, new Map()); proofListeners.get(event).set(callback, wrapped);
        context.on(event, wrapped);
      },
      off(event, callback) { const wrapped = proofListeners.get(event)?.get(callback); if (wrapped) context.off(event, wrapped); }
    };
    sessionProof = createBrowserSessionProof({ context: proofContext, target: manifest.browserOrigin }); report.session_proof = sessionProof.report;
    report.session_proof_event_scope = 'Logical main-frame events; original request/response objects. Worker events remain in requests and gateway ledger.';
    context.on('request', request => {
      const worker = request.serviceWorker(); let mainFrame = false;
      if (worker === null) try { mainFrame = request.frame() === page?.mainFrame(); } catch { /* Workerless requests need not have a frame. */ }
      const requestScope = candidateRequestScope(request.url(), request.method(), manifest);
      const row = { ordinal: ++requestOrdinal, ...requestScope,
        origin: ['external', 'invalid', 'non_network'].includes(requestScope.kind) ? requestScope.kind : 'target',
        method: request.method(), url: request.url(), scope: worker ? 'service_worker' : 'frame', main_frame: mainFrame,
        elapsed_ms: elapsed(), status: null, completed: false };
      entries.set(request, row); report.requests.push(row);
      if (!row.allowed) {
        const observation = { ordinal: row.ordinal, kind: row.kind, route: row.route ?? null, scope: row.scope, main_frame: row.main_frame,
          elapsed_ms: row.elapsed_ms, policy_enforcement: 'external_gateway', review_required: true };
        report.scope_observations.push(observation);
        if (row.kind === 'external') report.external_attempts.push(observation);
        else if (row.kind !== 'mutation') report.failure ??= 'candidate_observed_scope_violation';
      }
      track(row.ordinal, 'request_headers', async operation => {
        try {
          const headers = await candidateRequestHeaders(request, row, manifest, Math.min(REQUEST_HEADER_TIMEOUT_MS, remaining()));
          if (headers !== null) {
            row.headers = headers; const token = tokenFrom(headers, request.url()); row.token_sha256 = token ? sha(token) : null;
            const bytes = request.postDataBuffer(); row.payload_base64 = bytes?.toString('base64') ?? null;
            Object.assign(row, candidateRequestBody(row, row.method, headers['content-type'], bytes));
          }
        } catch (error) {
          operation('request_save'); await save('request-' + row.ordinal + '.json', row);
          operation('request_headers'); throw error;
        }
        operation('request_save'); await save('request-' + row.ordinal + '.json', row);
      });
    });
    context.on('response', response => {
      const row = entries.get(response.request()); if (!row) return;
      row.status = response.status(); row.response_headers = response.headers(); row.from_service_worker = response.fromServiceWorker();
      if (row.status >= 400) {
        const rejection = { ordinal: row.ordinal, kind: row.kind, route: row.route ?? null, status: row.status, failed: false,
          attribution: 'browser_context_response; gateway association pending' };
        report.observed_rejections.push(rejection); if (row.kind === 'external') report.blocked_external.push(rejection);
      }
      track(row.ordinal, 'response_observation', async operation => {
        const capture = row.kind === 'login' || row.kind === 'playback_info' || new RegExp('^/Users/' + manifest.actor.id + '/Items/[a-f0-9]{32}/?$', 'i').test(row.route ?? '');
        if (capture) {
          operation('response_body');
          const bytes = await bounded(response.body(), Math.min(15000, remaining())); need(bytes.length <= 1048576, 'candidate_public_body_limit');
          operation('response_save');
          await save('response-' + row.ordinal + '.json', { status: row.status, headers: row.response_headers, body_base64: bytes.toString('base64'), body_sha256: sha(bytes) });
          if (row.status === 200) {
            const value = JSON.parse(bytes.toString('utf8'));
            if (row.kind === 'login' && row.scope === 'frame' && row.main_frame) {
              const proof = validateCandidateLogin(value, manifest); need(!report.sessions.some(prior => prior.token_sha256 === proof.token_sha256), 'candidate_token_reused');
              secrets.push(value.AccessToken); activeSession = { ...proof, token: value.AccessToken, logged_out: false };
              report.sessions.push(proof); await save('session-' + report.sessions.length + '-private.json', activeSession);
            } else if (row.kind === 'playback_info') row.playback_info = value;
            else if (value.Id) {
              const known = [manifest.catalog.movie, manifest.catalog.series, manifest.catalog.mp3, manifest.catalog.flac,
                ...(manifest.catalog.episodes ?? []), ...(manifest.catalog.seasons ?? [])].filter(Boolean).find(item => item.id === value.Id);
              if (known) need(value.Type === known.type && (!known.name || value.Name === known.name) &&
                (!known.path || value.Path === known.path) && (!known.seriesId || value.SeriesId === known.seriesId) &&
                (!known.parentId || value.ParentId === known.parentId) && (known.indexNumber === undefined || value.IndexNumber === known.indexNumber) &&
                (known.parentIndexNumber === undefined || value.ParentIndexNumber === known.parentIndexNumber) &&
                (known.runtimeTicks === undefined || value.RunTimeTicks === known.runtimeTicks), 'candidate_item_detail_binding_changed');
              row.detail = value;
            }
          }
        }
        if (row.kind === 'logout' && row.scope === 'frame' && row.main_frame && row.status >= 200 && row.status < 300 && activeSession) {
          operation('response_logout_headers');
          const headers = await candidateRequestHeaders(response.request(), row, manifest, Math.min(REQUEST_HEADER_TIMEOUT_MS, remaining()));
          need(sha(tokenFrom(headers, response.request().url()) ?? '') === activeSession.token_sha256, 'candidate_logout_authority_mismatch');
          activeSession.logged_out = true;
        }
      });
    });
    context.on('requestfinished', request => { const row = entries.get(request); if (row) { row.completed = true; row.finished_elapsed_ms = elapsed(); } });
    context.on('requestfailed', request => {
      const row = entries.get(request); if (!row) return; row.failed = true;
      row.failed_elapsed_ms = elapsed();
      row.failure_error_text = request.failure()?.errorText ?? null;
      row.failure_ui_phase = cleanupMode ? 'cleanup' : report.playback?.phase ?? report.audio_flow?.phase ?? report.subtitle_flow?.phase ?? report.episode_phase ?? null;
      const rejection = { ordinal: row.ordinal, kind: row.kind, route: row.route ?? null, status: row.status, failed: true,
        attribution: 'browser_context_failure; gateway association pending' };
      report.observed_rejections.push(rejection); if (row.kind === 'external') report.blocked_external.push(rejection);
    });
    page = budgetedUI(await context.newPage()); page.on('pageerror', () => report.page_errors.push({ elapsed_ms: elapsed() }));
    await loginUI(true);
    const shared = { page, context, report, snapshot, target: new URL(manifest.clientUrl) };
    if (manifest.scenario === 'movie') await runMovieWorkflow({ ...shared, repeatLogin: async () => {
      await drain(); await sessionProof.drain(); need(activeSession && candidateLogoutProven(sessionProof.report, activeSession.token_sha256), 'candidate_prior_logout_not_proven');
      await loginUI();
    } });
    else if (manifest.scenario === 'episode') await episodePlayback();
    else if (manifest.scenario === 'tv-browse') await runTVBrowseUI({ ...shared, library: manifest.catalog.tvLibrary.name });
    else if (manifest.scenario === 'subtitles') await runSubtitleUI({ ...shared, movieId: manifest.catalog.movie.id });
    else {
      const audio = selectedCandidateItem(manifest);
      await runAudioUI({ ...shared, album: manifest.catalog.album.name, albumId: manifest.catalog.album.id,
        homeLibrary: manifest.catalog.musicLibrary, track: manifest.scenario === 'mp3' ? 'M3e MP3' : 'M3e FLAC',
        itemIdentity: { id: audio.id, name: audio.name, container: audio.container } });
    }
  } catch (error) { report.failure ??= /^[a-z0-9_]+$/i.test(error.message ?? '') ? error.message : 'candidate_scenario_failed'; }
  finally {
    cleanupMode = true;
    if (page) {
      await candidateOwnedUICleanup({ observe: drain,
        stopMedia: async () => { await pin(); await stopVisibleMedia(); report.cleanup.media_stopped = true; }, logout: logoutUI,
        onFailure(operation, error) {
          const reason = /^candidate_[a-z0-9_]+$/.test(error.message ?? '') ? error.message :
            { observer: 'candidate_observer_drain_failed', media: 'candidate_media_cleanup_failed', logout: 'candidate_ui_logout_failed' }[operation];
          (report.cleanup.ui_failures ??= []).push({ operation, reason }); report.failure ??= reason;
        } });
    }
    if (sessionProof) {
      try { await sessionProof.drain(); need(sessionProof.report.outcome === 'all_observed_logout_tokens_rejected' &&
        sessionProof.report.entries.length === report.sessions.length && report.sessions.every(session => candidateLogoutProven(sessionProof.report, session.token_sha256)));
        report.cleanup.tokens_rejected = true; } catch { report.failure ??= 'candidate_token_cleanup_failed'; }
    }
    try { await drain(); } catch { report.failure ??= 'candidate_observer_drain_failed'; }
    report.playback_evidence = candidatePlaybackEvidence(report.requests, manifest, report.sessions);
    if (!report.playback_evidence.passed) report.failure ??= 'candidate_playback_chain_incomplete';
    if (report.sessions.length !== (manifest.scenario === 'movie' ? 2 : 1)) report.failure ??= 'candidate_login_count_incomplete';
    if (context && report.failure) try { await context.storageState({ path: path.join(manifest.output, 'private-browser-state.json') }); }
    catch { report.cleanup.private_state_unavailable = true; }
    try { if (sessionProof) await sessionProof.dispose(); } catch { report.failure ??= 'candidate_proof_close_failed'; }
    try { if (browser) await bounded(browser.close(), 15000); report.cleanup.browser_closed = true; } catch { report.failure ??= 'candidate_browser_close_failed'; }
    report.cleanup.gateway_closure = 'owned_by_outer_controller';
    try { await pin(); } catch { report.failure ??= 'candidate_final_pin_failed'; }
    report.observer.pending = observations.snapshot();
    report.elapsed_ms = elapsed();
    if (report.elapsed_ms > manifest.budgets.maximumSeconds * 1000) report.failure ??= 'candidate_time_budget_exhausted';
    report.outcome = report.failure ? 'failed' : 'scenario_completed';
    const receipt = await save('observation.json', report);
    const summary = { kind: report.kind, run_id: manifest.runId, scenario: manifest.scenario, outcome: report.outcome, failure: report.failure,
      source: manifest.source, elapsed_ms: report.elapsed_ms, browser_version: report.browser_version, playwright_version: report.playwright_version,
      browser_environment: report.browser_environment, gateway_attestation_sha256: manifest.gatewayAttestation.sha256,
      physical_gateway_validation: 'pending_outer_gateway_closure',
      scope_observation_count: report.scope_observations.length, scope_review_required: report.scope_observations.length > 0,
      scope_observation_source: 'BrowserContext events; process-wide and physical scope are verified from the gateway ledger.',
      context_external_attempt_count: report.external_attempts.length, context_external_rejection_event_count: report.blocked_external.length,
      context_observed_rejection_event_count: report.observed_rejections.length,
      login_count: report.sessions.length, login_phase: report.login_phase ?? null, login_failure_phase: report.login_failure_phase ?? null,
      logout_phase: report.logout_phase ?? null, logout_failure_phase: report.logout_failure_phase ?? null,
      observer: report.observer,
      playback_evidence: { required: report.playback_evidence.required, passed: report.playback_evidence.passed,
        lifecycle_count: report.playback_evidence.lifecycles?.length ?? 0, media_event_count: report.playback_evidence.media_request_ordinals?.length ?? 0,
        progress_event_count: report.playback_evidence.progress_request_ordinals?.length ?? 0 }, cleanup: report.cleanup,
      private_report_sha256: receipt.sha256, evidence_boundary: report.evidence_boundary };
    const text = JSON.stringify(summary); need(secrets.every(secret => !text.includes(secret)), 'candidate_summary_contains_secret');
    await save('summary.json', summary);
    return summary;
  }
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  try {
    const args = process.argv.slice(2); need(args.length === 4 && args[0] === '--manifest' && args[2] === '--manifest-sha256');
    const result = await runAuditedCandidate({ manifestPath: args[1], manifestSha256: args[3] });
    process.stdout.write(JSON.stringify(result) + '\n'); if (result.outcome !== 'scenario_completed') process.exitCode = 1;
  } catch { process.stderr.write('candidate_adapter_failed\n'); process.exitCode = 1; }
}
