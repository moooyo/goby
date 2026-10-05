import { chromium } from '@playwright/test';
import assert from 'node:assert/strict';
import { readFile, writeFile, realpath, lstat, mkdir } from 'node:fs/promises';
import { isAbsolute, relative, resolve } from 'node:path';
import { createHash, randomUUID } from 'node:crypto';
import { execFile } from 'node:child_process';
import { isDeepStrictEqual, promisify } from 'node:util';

// This bounded acceptance runs only on test-env against an already refreshed
// deployment. Container replacement and retained sidecar audits belong to the
// deployment owner; this driver never restarts containers or kills processes.
assert.equal(process.platform, 'linux', 'Run this acceptance in the remote Linux environment.');
const owner = 'goby-player-release-165c-20261005';
assert(process.env.GOBY_RELEASE_ROOT, 'Provide the explicitly owned release root.');
const requestedRoot = resolve(process.env.GOBY_RELEASE_ROOT);
const root = await realpath(requestedRoot);
assert.equal(root, requestedRoot, 'The owned root must not traverse symlinks.');
assert.equal((await readFile(resolve(root, '.owner'), 'utf8')).trim(), owner);
function owned(path) {
  const suffix = relative(root, path);
  assert(suffix && suffix !== '..' && !suffix.startsWith('../') && !isAbsolute(suffix), 'An acceptance path escaped its owned root.');
  return path;
}
const configPath = owned(resolve(root, process.env.GOBY_RELEASE_REFRESH_CONFIG || 'refresh-20261006/amd.browser.private.json'));
assert.equal(await realpath(configPath), configPath, 'The private configuration must not traverse symlinks.');
let config;
try { config = JSON.parse(await readFile(configPath, 'utf8')); } catch { throw new Error('The private refresh configuration is not valid JSON.'); }
assert.equal(config.owner, owner);
assert.equal(config.bootstrap, false, 'Refresh acceptance must reuse the existing account and database.');
assert(Array.isArray(config.helperCommand) && config.helperCommand.length >= 2
  && config.helperCommand.every(value => typeof value === 'string' && value.length), 'Provide the bounded host helper argument vector.');
assert(typeof config.username === 'string' && config.username && typeof config.password === 'string' && config.password, 'Provide the existing login credentials.');
function endpoint(value) {
  let url;
  try { url = new URL(value); } catch { throw new Error('Provide a valid loopback endpoint.'); }
  assert(['http:', 'https:'].includes(url.protocol) && ['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname)
    && !url.username && !url.password && !url.search && !url.hash && url.pathname === '/', 'Use a loopback-only endpoint without credentials or a path.');
  return url.origin;
}
const playerURL = endpoint(config.playerURL);
const baseURL = endpoint(config.baseURL || config.playerURL);
const retainedPaths = config.retainedPaths ?? [];
assert(Array.isArray(retainedPaths) && (config.retainedPaths === undefined || retainedPaths.length === 3)
  && retainedPaths.every(path => typeof path === 'string' && path.startsWith('/') && !path.split('/').includes('..'))
  && new Set(retainedPaths).size === retainedPaths.length, 'Provide exactly three distinct retained source paths when selecting catalog preservation checks.');
async function directory(path) {
  owned(path);
  await mkdir(path, { mode: 0o700 }).catch(error => { if (error.code !== 'EEXIST') throw error; });
  assert((await lstat(path)).isDirectory() && await realpath(path) === path, 'Evidence directories must not traverse symlinks.');
  return path;
}
let evidenceRoot = root;
for (const part of ['refresh-20261006', 'amd', 'evidence']) evidenceRoot = await directory(resolve(evidenceRoot, part));
const output = owned(resolve(evidenceRoot, `live-backgrounds-${Date.now()}-${randomUUID().slice(0, 8)}`));
await mkdir(output, { mode: 0o700 });
const defaults = { DurationSeconds: 25, MaxWidth: 1280, VideoBitrate: 1_500_000, MaxItemRuntimeSeconds: 1200 };
const active = new Set(['pending', 'running', 'stopping']);
const evidence = { format: 'goby-main-refresh-backgrounds-v1', owner, startedAt: new Date().toISOString(), phases: [], runs: [], exports: [], failures: [],
  retainedSidecarAudit: 'The deployment owner records the separate before/after manifest, hash, timestamp, and inode audit.' };
const execute = promisify(execFile);
const sources = new Map(), publications = new Map(), ownedRevisions = new Map(), ownedRuns = new Set(), browserSecrets = new Set();
let csrf = '', token = '', userId = '', taskId, library, browser, administrator, consumer, adminPage, page, processMonitor, scanJobId;
let retainedBefore, identityBefore, hostBefore, catalogBefore, initialDisplayPreferences;
const labels = {
  background: '\u80cc\u666f\u77ed\u7247', start: '\u624b\u52a8\u622a\u53d6\u8d77\u70b9\uff08\u79d2\uff09',
  saveStart: '\u4fdd\u5b58\u8d77\u70b9', generate: '\u751f\u6210\u7f3a\u5931\u77ed\u7247',
  saved: '\u622a\u53d6\u8d77\u70b9\u5df2\u4fdd\u5b58\u3002\u5df2\u6709\u77ed\u7247\u4fdd\u6301\u4e0d\u53d8\uff0c\u53ea\u6709\u660e\u786e\u91cd\u65b0\u751f\u6210\u65f6\u624d\u5e94\u7528\u65b0\u8d77\u70b9\u3002',
  username: '\u7528\u6237\u540d', password: '\u5bc6\u7801', login: '\u767b\u5f55', navigation: '\u4e3b\u5bfc\u822a',
  motion: /^\u52a8\u6001\u80cc\u666f/, promote: '\u63d0\u9ad8\u672c\u5730\u751f\u6210\u77ed\u7247\u4f18\u5148\u7ea7',
};
function safe(value) {
  let text = String(value?.stack || value);
  const secrets = [config.password, config.setupToken, config.token, token, csrf, ...browserSecrets].filter(Boolean);
  for (const secret of secrets) {
    for (const representation of [secret, encodeURIComponent(secret), JSON.stringify(secret).slice(1, -1)]) text = text.replaceAll(representation, '[redacted]');
  }
  return text.replace(/(https?:\/\/[^\s?'"<>]+)\?[^\s'"<>]+/g, '$1?[redacted]')
    .replace(/((?:cookie|set-cookie|authorization|x-emby-token|x-csrf-token)["']?\s*:\s*)[^\r\n]+/gi, '$1[redacted]')
    .replace(/(goby_session=)[^;\s'"<>]+/gi, '$1[redacted]')
    .replace(/((?:api_key|access_token|token|password|csrf)[=:]\s*)[^\s&,'"}]+/gi, '$1[redacted]');
}
async function saveJSON(name, value) {
  const serialized = JSON.stringify(value, (key, item) => /^(AccessToken|CSRFToken|Password|Pw|SetupToken|Token)$/i.test(key) ? '[redacted]'
    : typeof item === 'string' ? safe(item) : item, 2);
  await writeFile(resolve(output, name), serialized + '\n', { mode: 0o600 });
}
const save = () => saveJSON('results.json', evidence);
async function phase(name, action) {
  const result = await action();
  evidence.phases.push({ name, passed: true, ...result });
  await save();
  console.log(JSON.stringify({ phase: name, passed: true }));
}
async function host(action, key) {
  assert(['describe', 'snapshot', 'probe', 'processes'].includes(action), 'Only read-only host helper actions belong to refresh acceptance.');
  const [binary, ...args] = config.helperCommand;
  const result = await execute(binary, [...args, action, ...(key ? ['--fixture', key] : [])], { timeout: 90_000, maxBuffer: 2 * 1024 * 1024 });
  return JSON.parse(result.stdout);
}
const delay = milliseconds => new Promise(done => setTimeout(done, milliseconds));
async function wait(check, message, timeout = 120_000, interval = 250) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) { const result = await check(); if (result) return result; await delay(interval); }
  throw new Error(message);
}
function localURL(path) {
  const value = new URL(path, baseURL);
  assert.equal(value.origin, baseURL, 'An API URL escaped the owned endpoint.');
  assert(!value.username && !value.password, 'An API URL contains credentials.');
  return value.href;
}
async function json(context, method, path, body, expected) {
  const response = await context.request.fetch(localURL(path), { method, data: body, timeout: 120_000,
    headers: path.startsWith('/admin/') ? { Origin: playerURL, 'X-CSRF-Token': csrf }
      : { 'X-Emby-Token': token, 'X-Emby-Authorization': 'Emby Client="Main refresh acceptance", Device="Remote browser", DeviceId="goby-main-refresh-165c", Version="1.0"' } });
  assert(expected === undefined ? response.ok() : response.status() === expected, `${method} ${path.split('?')[0]} returned ${response.status()}.`);
  const text = await response.text();
  try { return text ? JSON.parse(text) : undefined; } catch { throw new Error(`${method} ${path.split('?')[0]} returned invalid JSON.`); }
}
const admin = (method, path, body, expected) => json(administrator, method, path, body, expected);
const api = path => json(consumer, 'GET', path);
const detail = key => admin('GET', `/admin/v1/items/${sources.get(key).Id}/background-preview`);
const descriptor = id => api(`/emby/Items/${id}/BackgroundPreview`);
const userItem = id => api(`/emby/Users/${userId}/Items/${id}?Fields=MediaSources,MediaStreams,Path`);
const preferencesPath = () => `/emby/DisplayPreferences/goby-player?UserId=${userId}&Client=Goby%20Player`;
async function catalog() {
  const items = [];
  for (let start = 0; ; start += 200) {
    const result = await api(`/emby/Items?ParentId=${library.Id}&Recursive=true&IncludeItemTypes=Movie&Fields=MediaSources,MediaStreams,Path&Limit=200&StartIndex=${start}`);
    assert(Array.isArray(result.Items) && Number.isSafeInteger(result.TotalRecordCount), 'The catalog response is incomplete.');
    items.push(...result.Items);
    if (items.length >= result.TotalRecordCount) break;
    assert(result.Items.length, 'The catalog pagination stopped before its reported total.');
  }
  assert.equal(new Set(items.map(item => item.Id)).size, items.length, 'The catalog contains duplicate item identities.');
  return items;
}
function exactItem(items, path) {
  const matches = items.filter(item => item.Path === path || item.MediaSources?.some(source => source.Path === path));
  assert.equal(matches.length, 1, 'Each selected fixture must match its exact source path once.');
  return matches[0];
}
async function identity() {
  const server = await api('/emby/System/Info/Public'), user = await api(`/emby/Users/${userId}`);
  const currentLibrary = (await admin('GET', `/admin/v1/libraries/${library.Id}`)).Library;
  assert(server.Id && user.Id === userId && user.Name === config.username && user.ServerId === server.Id, 'The authenticated server and user identities do not agree.');
  return { server, user: { Id: user.Id, Name: user.Name, ServerId: user.ServerId, Policy: user.Policy, Configuration: user.Configuration },
    library: { Id: currentLibrary.Id, Name: currentLibrary.Name, Paths: currentLibrary.Paths, LibraryOptions: currentLibrary.LibraryOptions } };
}
async function retained(items) {
  const result = [];
  for (const path of retainedPaths) {
    const item = exactItem(items, path), user = await userItem(item.Id), artifact = await descriptor(item.Id);
    assert.equal(user.Id, item.Id); assert(user.UserData && artifact.Available, 'A retained item lost its user data or published background.');
    result.push({ path, catalog: item, userData: user.UserData, descriptor: artifact });
  }
  return result;
}
async function idle() {
  await wait(async () => {
    const current = (await admin('GET', `/admin/v1/tasks/${taskId}`)).Task.CurrentRun;
    return !current || !active.has(current.State);
  }, 'The background task did not retire.', 600_000);
  await wait(async () => !(await host('processes')).media.length, 'Media helpers remained after task retirement.', 30_000, 500);
}
function strictGPU(value) {
  return value.media.some(item => item.name === 'ffmpeg' && item.encoder === true && item.strictDolby === true && item.vulkan === true
    && Array.isArray(item.renderNodes) && item.renderNodes.some(node => /^\/dev\/dri\/renderD[0-9]+$/.test(node)));
}
function monitorProcesses(key) {
  let stopped = false, failure;
  const observations = [];
  const running = (async () => {
    try {
      while (!stopped) {
        observations.push({ observedAt: new Date().toISOString(), ...await host('processes') });
        if (!stopped) await delay(200);
      }
    } catch (error) { failure = error; }
  })();
  return { observations, async stop() {
    stopped = true; await running;
    await saveJSON(`${key}-processes.json`, observations);
    if (failure) throw failure;
  } };
}
async function openEditor(key) {
  await adminPage.bringToFront();
  await adminPage.goto(`${playerURL}/admin/media/libraries/${library.Id}/items`);
  await adminPage.getByRole('button', { name: `More actions for ${sources.get(key).Name}`, exact: true }).click();
  await adminPage.getByRole('menuitem', { name: labels.background, exact: true }).click();
  const dialog = adminPage.getByRole('dialog', { name: labels.background, exact: true });
  await dialog.getByLabel(labels.start, { exact: true }).waitFor();
  return dialog;
}
async function uiStart(fixture) {
  await idle();
  const key = fixture.key, before = await detail(key), startSeconds = fixture.startSeconds ?? 1;
  assert.equal(before.Artifact.Available, false, 'Use a fresh item without a retained background.');
  assert(Number.isInteger(startSeconds) && startSeconds > 0 && startSeconds <= 600, 'Use a bounded nonzero fixture start.');
  const dialog = await openEditor(key);
  await dialog.getByLabel(labels.start, { exact: true }).fill(String(startSeconds));
  await dialog.getByRole('button', { name: labels.saveStart, exact: true }).click();
  await dialog.getByText(labels.saved, { exact: true }).waitFor();
  const saved = await detail(key);
  assert.equal(saved.RequestedRevision, before.RequestedRevision, 'Saving the start scheduled generation.');
  assert.equal(saved.StartTicks, startSeconds * 10_000_000);
  const expectedRevision = String(BigInt(before.RequestedRevision) + 1n);
  const attempt = { key, expectedRevision, startSeconds };
  evidence.runs.push(attempt);
  processMonitor = monitorProcesses(key);
  const admissionRequest = request => new URL(request.url()).pathname === '/admin/v1/media-analysis/runs' && request.method() === 'POST';
  const inputPromise = adminPage.waitForRequest(admissionRequest).then(request => {
    const input = request.postDataJSON();
    assert.equal(input.Kind, 'background'); assert.equal(input.Force, false);
    assert.deepEqual(input.ItemIds, [sources.get(key).Id]); assert.deepEqual(input.LibraryIds, []);
    assert.match(input.RequestId, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    // A witnessed, scoped UI submission also permits cleanup when its response
    // is lost: only this item's exact next requested revision is owned.
    ownedRevisions.set(key, expectedRevision); attempt.input = input;
    return input;
  });
  const responsePromise = adminPage.waitForResponse(response => admissionRequest(response.request()));
  const [, response] = await Promise.all([inputPromise, responsePromise, dialog.getByRole('button', { name: labels.generate, exact: true }).click()]);
  const receipt = await response.json(); attempt.receipt = receipt;
  if (typeof receipt.RunId === 'string' && receipt.RunId && receipt.TaskId === taskId && receipt.Admitted === true) ownedRuns.add(receipt.RunId);
  assert.equal(response.status(), 202);
  assert(receipt.RunId && receipt.TaskId === taskId && typeof receipt.Admitted === 'boolean');
  // The scheduler can consume this request's event before the explicit start.
  // A coalesced receipt is valid; only the matching item revision owns its run.
  await save();
  return attempt;
}
async function finish(attempt) {
  const completed = await wait(async () => {
    const value = await detail(attempt.key);
    if (value.RequestedRevision === attempt.expectedRevision && value.RunId) ownedRuns.add(value.RunId);
    return value.RequestedRevision === attempt.expectedRevision && value.CompletedRevision === attempt.expectedRevision && !active.has(value.State) && value;
  }, 'The admitted fixture revision did not finish.', 600_000, 350);
  await processMonitor.stop();
  const observations = processMonitor.observations.filter(strictGPU);
  processMonitor = undefined;
  await saveJSON(`${attempt.key}-completion.json`, completed);
  assert.equal(completed.State, 'ready', `Background result: ${completed.State}, ${completed.ErrorCode}`);
  assert.equal(completed.Reused, false, 'The fresh fixture reused an existing generation.');
  assert(observations.length, 'No strict libplacebo/Vulkan encoder with a live DRM render handle was observed.');
  await idle();
  Object.assign(attempt, { completed, gpuObservations: observations.slice(0, 3) });
  return completed;
}
async function screenshot(target, name) {
  await target.screenshot({ path: resolve(output, `${name}.png`), fullPage: true, mask: [target.locator('input[type="password"]')] });
}
function probeSummary(value) {
  assert.equal(value.streams.length, 1, 'The publication must contain only one silent video stream.');
  const video = value.streams[0];
  assert.equal(video.codec_type, 'video'); assert.equal(video.codec_name, 'h264');
  assert.equal(video.width, 1280); assert.equal(video.height, 720); assert.equal(video.pix_fmt, 'yuv420p'); assert.equal(video.color_range, 'tv');
  for (const field of ['color_space', 'color_transfer', 'color_primaries']) assert.equal(video[field], 'bt709');
  assert.equal(Number(video.nb_read_frames), 600); assert(Math.abs(Number(value.format.duration) - 25) < 0.1);
  assert(!(video.side_data_list || []).some(item => /dovi|dolby|mastering|content light/i.test(item.side_data_type)));
  return { codec: 'h264', width: video.width, height: video.height, duration: Number(value.format.duration), frames: Number(video.nb_read_frames),
    pixelFormat: video.pix_fmt, color: 'bt709', range: 'limited', audioStreams: 0 };
}
async function configurePlayer() {
  initialDisplayPreferences = await api(preferencesPath());
  await saveJSON('player-preferences-before.json', initialDisplayPreferences);
  await page.bringToFront(); await page.goto(playerURL);
  await page.getByLabel(labels.username, { exact: true }).fill(config.username);
  await page.getByLabel(labels.password, { exact: true }).fill(config.password);
  await page.getByRole('button', { name: labels.login, exact: true }).click();
  await page.getByRole('navigation', { name: labels.navigation }).waitFor();
  const session = await page.evaluate(() => {
    const value = JSON.parse(localStorage.getItem('goby.player.session.v1') || 'null');
    return value && { userId: value.User?.Id, serverId: value.ServerId };
  });
  assert.equal(session?.userId, userId); assert.equal(session?.serverId, identityBefore.server.Id);
  await page.goto(`${playerURL}/#/settings`);
  const motion = page.getByRole('switch', { name: labels.motion });
  if (await motion.getAttribute('aria-checked') !== 'true') await motion.click();
  for (let index = 0; index < 2; index++) {
    const button = page.getByRole('button', { name: labels.promote, exact: true });
    if (await button.isEnabled()) await button.click();
  }
  await wait(async () => {
    const saved = JSON.parse((await api(preferencesPath())).CustomPrefs?.preferences || '{}');
    return saved.backgroundMotion === true && saved.backgroundSources?.[0] === 'generated';
  }, 'The generated background preference did not persist.');
  await screenshot(page, 'generated-source-preference');
  return session;
}
async function playback(key) {
  const item = sources.get(key), before = (await userItem(item.Id)).UserData, requests = [];
  assert(before, 'Capture actual user playback data before the background begins.');
  const listener = request => requests.push({ method: request.method(), path: new URL(request.url()).pathname });
  page.on('request', listener);
  try {
    await page.bringToFront(); await page.goto(`${playerURL}/#/detail/${item.Id}`);
    await page.waitForFunction(id => {
      const video = document.querySelector('.background-preview');
      return !document.hidden && video?.dataset.previewOwner === id && video.dataset.previewKind === 'generated'
        && video.readyState >= 2 && !video.paused && video.currentTime >= 1 && Number(getComputedStyle(video).opacity) >= 0.95;
    }, item.Id, { timeout: 30_000 });
    await screenshot(page, `${key}-background-playing`);
    const result = await page.locator('.background-preview').evaluate(video => new Promise((resolve, reject) => {
      const start = video.currentTime, startedAt = performance.now();
      const timer = setTimeout(() => reject(new Error('The generated background did not naturally end.')), 45_000);
      video.addEventListener('ended', () => {
        clearTimeout(timer);
        const quality = video.getVideoPlaybackQuality();
        resolve({ width: video.videoWidth, height: video.videoHeight, duration: video.duration, startTime: start, finalTime: video.currentTime,
          muted: video.muted, playbackRate: video.playbackRate, ended: video.ended, elapsedMs: performance.now() - startedAt,
          totalVideoFrames: quality.totalVideoFrames, droppedVideoFrames: quality.droppedVideoFrames,
          displayedFrames: quality.totalVideoFrames - quality.droppedVideoFrames });
      }, { once: true });
      video.addEventListener('error', () => { clearTimeout(timer); reject(new Error('The generated background failed to decode.')); }, { once: true });
    }));
    assert.equal(result.width, 1280); assert.equal(result.height, 720); assert.equal(result.duration, 25);
    assert.equal(result.muted, true); assert.equal(result.playbackRate, 1); assert.equal(result.ended, true);
    assert(result.startTime < 8 && result.finalTime >= 24.9 && result.totalVideoFrames >= 590 && result.displayedFrames >= 570,
      'The complete background did not render with an acceptable frame count.');
    const expectedElapsed = (result.duration - result.startTime) * 1000;
    assert(result.elapsedMs >= expectedElapsed - 350 && result.elapsedMs <= expectedElapsed + 5000, 'Background playback did not finish at its natural real-time cadence.');
    assert(requests.some(value => /\/BackgroundPreview\/stream\.mp4$/.test(value.path)));
    assert(!requests.some(value => /\/PlaybackInfo$|\/Sessions\/Playing/.test(value.path)), 'Background playback created a feature playback session.');
    const after = (await userItem(item.Id)).UserData;
    await saveJSON(`${key}-playback.json`, { ...result, requests, userDataBefore: before, userDataAfter: after });
    assert.deepEqual(after, before, 'Background playback changed user playback data.');
    await page.goto('about:blank');
    return { ...result, playbackHistoryUnchanged: true };
  } finally { page.off('request', listener); }
}

try {
  hostBefore = await host('describe'); await saveJSON('host-before.json', hostBefore);
  assert.equal(hostBefore.owner, owner); assert.match(hostBefore.imageId, /^sha256:[a-f0-9]{64}$/);
  assert.deepEqual(hostBefore.fixtures.map(fixture => fixture.key), ['p5', 'p84', 'p82']);
  assert.equal(new Set(hostBefore.fixtures.map(fixture => fixture.path)).size, 3);
  assert(hostBefore.fixtures.every(fixture => !retainedPaths.includes(fixture.path)), 'Refresh fixtures must use fresh source paths.');
  if (config.imageId !== undefined) assert.equal(hostBefore.imageId, config.imageId, 'The helper did not identify the selected refreshed image.');
  browser = await chromium.launch({ headless: true, args: ['--no-sandbox'] });
  administrator = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN', reducedMotion: 'reduce' });
  consumer = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: 'zh-CN', reducedMotion: 'no-preference' });
  adminPage = await administrator.newPage(); page = await consumer.newPage();
  for (const [target, surface] of [[adminPage, 'administrator'], [page, 'consumer']]) {
    target.on('pageerror', error => evidence.failures.push({ surface, kind: 'page', message: safe(error) }));
    target.on('response', async response => {
      if (new URL(response.url()).pathname === '/emby/Users/AuthenticateByName' && response.ok()) {
        try { const value = await response.json(); if (typeof value.AccessToken === 'string' && value.AccessToken) browserSecrets.add(value.AccessToken); } catch { /* The login phase checks the response outcome. */ }
      }
    });
  }
  const auth = await json(consumer, 'POST', '/emby/Users/AuthenticateByName', { Username: config.username, Pw: config.password });
  token = auth.AccessToken; userId = auth.User?.Id;
  assert(typeof token === 'string' && token && userId, 'The existing account did not authenticate.');
  csrf = (await admin('POST', '/admin/v1/session', { Name: config.username, Password: config.password })).CSRFToken;
  assert(typeof csrf === 'string' && csrf, 'The existing administrator session did not authenticate.');
  for (const cookie of await administrator.cookies()) if (cookie.value) browserSecrets.add(cookie.value);
  await phase('the refreshed deployment retains its enabled Vulkan policy', async () => {
    const settings = await admin('GET', '/admin/v1/settings');
    assert.equal(settings.Runtime?.Effective?.VulkanToneMapping, true, 'The selected deployment must already permit Vulkan tone mapping.');
    return { enabled: true, source: settings.Runtime.Sources.VulkanToneMapping, changed: false, imageId: hostBefore.imageId };
  });
  const libraries = (await admin('GET', '/admin/v1/libraries')).Items;
  const matches = libraries.filter(value => value.Paths.includes(hostBefore.libraryPath));
  assert.equal(matches.length, 1, 'Reuse exactly one existing library for the owned media root.');
  [library] = matches;
  assert.deepEqual(library.Paths, [hostBefore.libraryPath]);
  assert.equal(library.LibraryOptions.EnableBackgroundPreviewGeneration, false, 'The selected library must keep automatic generation disabled.');
  taskId = (await admin('GET', '/admin/v1/tasks')).Items.find(value => value.Key === 'media.background_preview_generation')?.Id;
  assert(taskId); await idle();
  const configuration = await admin('GET', '/admin/v1/background-previews/configuration');
  assert.deepEqual(configuration.Defaults, defaults); assert.deepEqual(configuration.Profile, defaults, 'Use the existing 25-second background profile.');
  identityBefore = await identity(); await saveJSON('identity-before.json', identityBefore);
  catalogBefore = await catalog(); await saveJSON('catalog-before.json', catalogBefore);
  retainedBefore = await retained(catalogBefore); await saveJSON('retained-items-before.json', retainedBefore);
  for (const fixture of hostBefore.fixtures) assert.equal(await host('snapshot', fixture.key), null, 'Use a fresh owned fixture for first-generation acceptance.');
  await phase('a real scan adds the fresh fixtures to the existing library', async () => {
    const receipt = await admin('POST', `/admin/v1/libraries/${library.Id}/scan`, { ForceProbe: false });
    scanJobId = receipt.Job.Id;
    const completed = await wait(async () => {
      const job = (await admin('GET', '/admin/v1/jobs')).Items.find(value => value.Id === receipt.Job.Id);
      assert(!['failed', 'canceled', 'cancelled'].includes(job?.Status), 'The refresh fixture scan failed.');
      return job && ['completed', 'succeeded'].includes(job.Status) && job;
    }, 'The refresh fixture scan timed out.', 300_000);
    scanJobId = undefined;
    const items = await catalog(); await saveJSON('catalog-after-scan.json', items);
    for (const fixture of hostBefore.fixtures) {
      const item = exactItem(items, fixture.path);
      assert.equal(item.Name, fixture.name); sources.set(fixture.key, item);
      assert.equal((await descriptor(item.Id)).Available, false, 'A fresh fixture already exposes a publication.');
      assert.equal(await host('snapshot', fixture.key), null, 'Scanning generated a background without explicit admission.');
    }
    return { receipt, completed, fixtures: [...sources].map(([key, item]) => ({ key, Id: item.Id, Path: item.Path })) };
  });
  await phase('the independent player authenticates the preserved account', configurePlayer);
  for (const fixture of hostBefore.fixtures) {
    await phase(`${fixture.key}: the administrator UI creates a fresh strict GPU background`, async () => {
      const attempt = await uiStart(fixture); await finish(attempt);
      const snapshot = await host('snapshot', fixture.key); await saveJSON(`${fixture.key}-publication.json`, snapshot);
      assert(snapshot); assert.deepEqual(snapshot.temporaryFiles, []); assert.equal(snapshot.manifest.StartTicks, attempt.startSeconds * 10_000_000);
      publications.set(fixture.key, snapshot);
      const probe = await host('probe', fixture.key); await saveJSON(`${fixture.key}-probe.json`, probe);
      const summary = probeSummary(probe), artifact = await descriptor(sources.get(fixture.key).Id);
      assert(artifact.Available && artifact.StreamUrl);
      const range = await consumer.request.get(localURL(artifact.StreamUrl), { headers: { 'X-Emby-Token': token, Range: 'bytes=0-31' } });
      const rangeEvidence = { status: range.status(), bytes: (await range.body()).length, contentType: range.headers()['content-type'], contentRange: range.headers()['content-range'] };
      await saveJSON(`${fixture.key}-range.json`, rangeEvidence);
      assert.equal(rangeEvidence.status, 206); assert.equal(rangeEvidence.bytes, 32); assert.equal(rangeEvidence.contentType, 'video/mp4');
      assert.match(rangeEvidence.contentRange, /^bytes 0-31\/[1-9][0-9]*$/);
      const response = await consumer.request.get(localURL(artifact.StreamUrl), { headers: { 'X-Emby-Token': token } });
      assert.equal(response.status(), 200); assert.equal(response.headers()['content-type'], 'video/mp4');
      const data = await response.body(), file = resolve(output, `${fixture.key}-publication.mp4`);
      assert.equal(data.length, snapshot.size); assert(data.length > 0 && data.length <= 64 * 1024 * 1024);
      assert.equal(createHash('sha256').update(data).digest('hex'), snapshot.sha256, 'The exported consumer stream differs from its published source-adjacent artifact.');
      await writeFile(file, data, { mode: 0o600, flag: 'wx' });
      const exported = { key: fixture.key, file, bytes: data.length, sha256: snapshot.sha256, generation: snapshot.generation,
        sourceManifestSHA256: snapshot.manifestSHA256, manifest: snapshot.manifest, readOnlyConsumerExport: true };
      evidence.exports.push(exported);
      await saveJSON(`${fixture.key}-export.json`, exported);
      await saveJSON('export-receipt.json', { format: 'goby-main-refresh-output-export-v1', owner, exports: evidence.exports,
        sourceManifestBindingChecked: true, newGenerationRequestedByExport: false });
      await openEditor(fixture.key); await screenshot(adminPage, `${fixture.key}-administrator-ready`);
      return { ...summary, generation: snapshot.generation, sha256: snapshot.sha256, nonzeroStartSeconds: attempt.startSeconds, rangeStatus: 206 };
    });
    await phase(`${fixture.key}: the independent player naturally completes the 25-second background`, () => playback(fixture.key));
  }
  assert.deepEqual(evidence.failures, []);
  evidence.acceptancePassed = true;
} catch (error) {
  evidence.failures.push({ kind: 'acceptance', message: safe(error) });
  if (adminPage) await screenshot(adminPage, 'administrator-failure').catch(() => undefined);
  if (page) await screenshot(page, 'consumer-failure').catch(() => undefined);
  console.error(safe(error)); process.exitCode = 1;
} finally {
  const cleanup = async action => {
    try { await action(); } catch (error) { evidence.failures.push({ kind: 'closeout', message: safe(error) }); process.exitCode = 1; }
  };
  if (processMonitor) await cleanup(async () => { await processMonitor.stop(); processMonitor = undefined; });
  if (scanJobId && csrf) await cleanup(async () => {
    const terminal = new Set(['completed', 'succeeded', 'failed', 'canceled', 'cancelled']);
    const ownJob = async () => (await admin('GET', '/admin/v1/jobs')).Items.find(value => value.Id === scanJobId);
    const current = await ownJob();
    assert(current, 'The owned scan disappeared before its cleanup state was established.');
    if (!terminal.has(current.Status)) await admin('POST', `/admin/v1/jobs/${scanJobId}/cancel`, {});
    await wait(async () => terminal.has((await ownJob())?.Status), 'The owned scan did not retire after cancellation.', 120_000);
  });
  if (taskId && csrf) {
    for (const [key, revision] of ownedRevisions) {
      await cleanup(async () => {
        const value = await detail(key);
        if (value.RequestedRevision === revision && value.RunId) ownedRuns.add(value.RunId);
      });
    }
    for (const id of ownedRuns) {
      await cleanup(async () => {
        if (active.has((await admin('GET', `/admin/v1/task-runs/${id}`)).Run.State)) await admin('POST', `/admin/v1/task-runs/${id}/cancel`, {});
      });
    }
    await cleanup(idle);
  }
  if (page) await cleanup(() => page.goto('about:blank'));
  if (initialDisplayPreferences && token) await cleanup(async () => {
    const current = await api(preferencesPath());
    const changed = !isDeepStrictEqual(current.CustomPrefs, initialDisplayPreferences.CustomPrefs);
    if (changed) await json(consumer, 'POST', preferencesPath(), { Revision: String(current.Revision), CustomPrefs: initialDisplayPreferences.CustomPrefs });
    const restored = await api(preferencesPath()); await saveJSON('player-preferences-after.json', restored);
    assert.deepEqual(restored.CustomPrefs, initialDisplayPreferences.CustomPrefs, 'The existing player preferences were not restored.');
    evidence.playerPreferences = { restored: true, changedDuringAcceptance: changed };
  });
  if (identityBefore && catalogBefore && retainedBefore) await cleanup(async () => {
    const afterIdentity = await identity(), afterCatalog = await catalog(), afterRetained = await retained(afterCatalog);
    await saveJSON('identity-after.json', afterIdentity); await saveJSON('catalog-after.json', afterCatalog); await saveJSON('retained-items-after.json', afterRetained);
    assert.deepEqual(afterIdentity, identityBefore, 'The existing server, user, or library identity changed.');
    const itemsById = new Map(afterCatalog.map(item => [item.Id, item]));
    for (const item of catalogBefore) assert.deepEqual(itemsById.get(item.Id), item, 'An existing catalog item disappeared or changed.');
    assert.deepEqual(afterRetained, retainedBefore, 'A retained catalog item, descriptor, or playback history changed.');
    evidence.preservation = { identitiesUnchanged: true, catalogItemsChecked: catalogBefore.length, existingCatalogUnchanged: true,
      retainedItemsChecked: retainedPaths.length, retainedUserDataUnchanged: retainedPaths.length ? true : null };
  });
  if (hostBefore) {
    await cleanup(async () => {
      const after = await host('describe'); await saveJSON('host-after.json', after);
      assert.deepEqual(after, hostBefore, 'The selected image or exact source fixtures changed during acceptance.');
    });
    let checkedPublications = 0;
    for (const fixture of hostBefore.fixtures) {
      await cleanup(async () => {
        const snapshot = await host('snapshot', fixture.key); await saveJSON(`${fixture.key}-publication-after.json`, snapshot);
        if (snapshot) { assert.deepEqual(snapshot.temporaryFiles, [], 'A temporary publication remains.'); checkedPublications++; }
        if (publications.has(fixture.key)) assert.deepEqual(snapshot, publications.get(fixture.key), 'A completed refresh publication changed during playback.');
        if (evidence.acceptancePassed) assert(snapshot, 'A completed refresh publication disappeared.');
      });
    }
    await cleanup(async () => {
      const processes = await host('processes'); await saveJSON('processes-after.json', processes);
      assert.deepEqual(processes.media, [], 'Owned media helpers remained at closeout.');
      evidence.resources = { remainingMediaHelpers: 0, publishedDirectoriesChecked: checkedPublications, temporaryFilesInPublishedDirectories: 0 };
    });
  }
  await cleanup(async () => { await browser?.close(); });
  evidence.complete = evidence.acceptancePassed === true && evidence.failures.length === 0 && !process.exitCode;
  evidence.finishedAt = new Date().toISOString(); await save();
  if (!evidence.complete) process.exitCode = 1;
  console.log(JSON.stringify({ complete: evidence.complete, phases: evidence.phases.length, directory: output }));
}
