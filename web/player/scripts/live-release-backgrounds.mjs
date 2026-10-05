import { chromium } from '@playwright/test';
import assert from 'node:assert/strict';
import { readFile, writeFile, realpath, mkdir } from 'node:fs/promises';
import { resolve } from 'node:path';
import { randomUUID } from 'node:crypto';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';

// Browser execution belongs on test-env. Host operations are delegated through
// an explicit argument vector to the bounded helper on the owned Docker host.
assert.equal(process.platform, 'linux', 'Run this acceptance in the remote Linux environment.');
const owner = 'goby-player-release-165c-20261005';
assert(process.env.GOBY_RELEASE_ROOT, 'Provide the explicitly owned release root.');
const root = await realpath(process.env.GOBY_RELEASE_ROOT);
assert.equal((await readFile(resolve(root, '.owner'), 'utf8')).trim(), owner);
const config = JSON.parse(await readFile(resolve(root, 'browser.private.json'), 'utf8'));
assert.equal(config.owner, owner);
assert(Array.isArray(config.helperCommand) && config.helperCommand.length >= 2 && config.helperCommand.every(value => typeof value === 'string' && value.length));
const playerURL = new URL(config.playerURL).origin;
const baseURL = new URL(config.baseURL || config.playerURL).origin;
assert(['127.0.0.1', 'localhost', '[::1]'].includes(new URL(playerURL).hostname), 'Use the loopback-only release endpoint.');
assert(['127.0.0.1', 'localhost', '[::1]'].includes(new URL(baseURL).hostname), 'Use the loopback-only API endpoint.');
const artifacts = resolve(root, 'artifacts');
assert.equal(await realpath(artifacts), artifacts);
const output = resolve(artifacts, `live-backgrounds-${Date.now()}`);
await mkdir(output, { mode: 0o700 });
const defaults = { DurationSeconds: 25, MaxWidth: 1280, VideoBitrate: 1_500_000, MaxItemRuntimeSeconds: 1200 };
const active = new Set(['pending', 'running', 'stopping']);
const evidence = { format: 'goby-player-release-backgrounds-v1', owner, phases: [], runs: [], failures: [] };
const execute = promisify(execFile);
let csrf = '', token = '', userId = '', taskId, library, pendingRun, restrictedKey, initialProfile;
let browser, administrator, consumer, adminPage, page;
const sources = new Map();
const originals = new Map();
const ownedRuns = new Set();
const ownedRevisions = new Map();
const secrets = () => [config.password, config.setupToken, config.token, token, csrf].filter(Boolean);
function safe(error) {
  let value = String(error?.stack || error);
  for (const secret of secrets()) value = value.replaceAll(secret, '[redacted]');
  return value.replace(/(https?:\/\/[^\s?'"<>]+)\?[^\s'"<>]+/g, '$1?[redacted]');
}
async function save() {
  await writeFile(resolve(output, 'results.json'), JSON.stringify(evidence, null, 2));
  await writeFile(resolve(artifacts, 'live-release-latest.json'), JSON.stringify({ directory: output, complete: evidence.complete === true }, null, 2));
}
async function phase(name, action) {
  const result = await action();
  evidence.phases.push({ name, passed: true, ...result });
  await save();
  console.log(JSON.stringify({ phase: name, passed: true }));
}
async function host(action, key) {
  const [binary, ...args] = config.helperCommand;
  const result = await execute(binary, [...args, action, ...(key ? ['--fixture', key] : [])], { timeout: 90_000, maxBuffer: 2 * 1024 * 1024 });
  return JSON.parse(result.stdout);
}
async function wait(check, message, timeout = 120_000, interval = 250) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const value = await check();
    if (value) return value;
    await new Promise(done => setTimeout(done, interval));
  }
  throw new Error(message);
}
function localURL(path) {
  const value = new URL(path, baseURL);
  assert.equal(value.origin, baseURL, 'An API URL escaped the owned endpoint.');
  return value.href;
}
async function json(context, method, path, body, expected) {
  const native = path.startsWith('/admin/');
  const response = await context.request.fetch(localURL(path), { method, data: body, timeout: 120_000, headers: native ? {
    Origin: playerURL, 'X-CSRF-Token': csrf,
  } : { 'X-Emby-Token': token, 'X-Emby-Authorization': 'Emby Client="Release acceptance", Device="Remote browser", DeviceId="goby-player-release-165c", Version="1.0"' } });
  assert(expected === undefined ? response.ok() : response.status() === expected, `${method} ${path.split('?')[0]} returned ${response.status()}.`);
  const text = await response.text();
  return text ? JSON.parse(text) : undefined;
}
const admin = (method, path, body, expected) => json(administrator, method, path, body, expected);
const api = path => json(consumer, 'GET', path);
const detail = key => admin('GET', `/admin/v1/items/${sources.get(key).Id}/background-preview`);
const descriptor = key => api(`/emby/Items/${sources.get(key).Id}/BackgroundPreview`);
async function login() {
  csrf = (await admin('POST', '/admin/v1/session', { Name: config.username, Password: config.password })).CSRFToken;
}
async function configure(profile) {
  const value = await admin('GET', '/admin/v1/background-previews/configuration');
  return admin('PUT', '/admin/v1/background-previews/configuration', { Revision: value.Revision, Profile: profile });
}
async function idle() {
  await wait(async () => {
    const current = (await admin('GET', `/admin/v1/tasks/${taskId}`)).Task.CurrentRun;
    return !current || !active.has(current.State);
  }, 'The background task did not retire.', 600_000);
  await wait(async () => !(await host('processes')).media.length, 'Media helpers remained after task retirement.', 30_000, 500);
}
async function quietHistory() {
  await idle();
  let previous = -1, stableAt = Date.now();
  return wait(async () => {
    const current = (await admin('GET', `/admin/v1/tasks/${taskId}/runs?Limit=1&StartIndex=0`)).TotalRecordCount;
    if (current !== previous) { previous = current; stableAt = Date.now(); }
    return Date.now() - stableAt >= 2500 ? { count: current } : false;
  }, 'The task event history did not settle.', 30_000);
}
async function scan() {
  const receipt = await admin('POST', `/admin/v1/libraries/${library.Id}/scan`, { ForceProbe: false });
  await wait(async () => {
    const job = (await admin('GET', '/admin/v1/jobs')).Items.find(value => value.Id === receipt.Job.Id);
    assert(!['failed', 'canceled', 'cancelled'].includes(job?.Status), 'The release fixture scan failed.');
    return job && ['completed', 'succeeded'].includes(job.Status);
  }, 'The release fixture scan timed out.', 300_000);
}
async function start(key, force = false) {
  await idle();
  const before = await detail(key);
  const input = { Kind: 'background', RequestId: randomUUID(), LibraryIds: [], ItemIds: [sources.get(key).Id], Force: force };
  const receipt = await admin('POST', '/admin/v1/media-analysis/runs', input, 202);
  assert.equal(receipt.TaskId, taskId);
  pendingRun = receipt.RunId;
  const expectedRevision = String(BigInt(before.RequestedRevision) + 1n);
  ownedRuns.add(receipt.RunId); ownedRevisions.set(key, expectedRevision);
  return { input, receipt, key, expectedRevision };
}
async function openEditor(key) {
  const item = sources.get(key);
  await adminPage.bringToFront();
  await adminPage.goto(`${playerURL}/admin/media/libraries/${library.Id}/items`);
  await adminPage.getByRole('button', { name: `More actions for ${item.Name}`, exact: true }).click();
  await adminPage.getByRole('menuitem', { name: '背景短片', exact: true }).click();
  const dialog = adminPage.getByRole('dialog', { name: '背景短片', exact: true });
  await dialog.getByLabel('手动截取起点（秒）', { exact: true }).waitFor();
  return dialog;
}
async function uiStart(key) {
  await idle();
  const before = await detail(key);
  const fixture = evidence.host.fixtures.find(value => value.key === key);
  const startSeconds = fixture.startSeconds ?? 1;
  assert(Number.isInteger(startSeconds) && startSeconds > 0 && startSeconds <= 600, 'Use a bounded nonzero fixture start.');
  const dialog = await openEditor(key);
  await dialog.getByLabel('手动截取起点（秒）', { exact: true }).fill(String(startSeconds));
  await dialog.getByRole('button', { name: '保存起点', exact: true }).click();
  await dialog.getByText('截取起点已保存。已有短片保持不变，只有明确重新生成时才应用新起点。', { exact: true }).waitFor();
  assert.equal((await detail(key)).RequestedRevision, before.RequestedRevision, 'Saving a start must not schedule generation.');
  const responsePromise = adminPage.waitForResponse(value => new URL(value.url()).pathname === '/admin/v1/media-analysis/runs' && value.request().method() === 'POST');
  await dialog.getByRole('button', { name: '生成缺失短片', exact: true }).click();
  const response = await responsePromise;
  assert.equal(response.status(), 202);
  const receipt = await response.json();
  pendingRun = receipt.RunId;
  const expectedRevision = String(BigInt(before.RequestedRevision) + 1n);
  ownedRuns.add(receipt.RunId); ownedRevisions.set(key, expectedRevision);
  return { input: response.request().postDataJSON(), receipt, key, expectedRevision };
}
async function finish(attempt, expected = 'ready', requireGPU = false, observed = []) {
  const completed = await wait(async () => {
    const value = await detail(attempt.key);
    if (value.RequestedRevision === attempt.expectedRevision && value.RunId) ownedRuns.add(value.RunId);
    if (value.RequestedRevision !== attempt.expectedRevision || value.CompletedRevision !== attempt.expectedRevision || active.has(value.State)) {
      const process = await host('processes');
      if (process.gpuEncoding) observed.push(process);
      return false;
    }
    return value;
  }, 'The admitted fixture revision did not finish.', 600_000, 450);
  assert.equal(completed.State, expected, `Background result: ${completed.State}, ${completed.ErrorCode}`);
  await idle();
  if (requireGPU) assert(observed.some(value => value.gpuEncoding), 'No strict Dolby encoder with a live DRM handle was observed.');
  evidence.runs.push({ key: attempt.key, input: attempt.input, receipt: attempt.receipt, state: completed.State, errorCode: completed.ErrorCode,
    reused: completed.Reused, requestedRevision: attempt.expectedRevision, actualRunId: completed.RunId, gpuObservations: observed.slice(0, 3) });
  pendingRun = undefined;
  return completed;
}
async function unchanged(key) {
  const current = await host('snapshot', key);
  assert.deepEqual(current, originals.get(key), 'A retained publication or its timestamps changed.');
  return current;
}
async function screenshot(target, name) {
  await target.screenshot({ path: resolve(output, `${name}.png`), fullPage: true });
}
function probeSummary(value) {
  assert.equal(value.streams.length, 1);
  const video = value.streams[0];
  assert.equal(video.codec_type, 'video'); assert.equal(video.codec_name, 'h264');
  assert.equal(video.width, 1280); assert.equal(video.height, 720); assert.equal(video.pix_fmt, 'yuv420p');
  assert.equal(video.color_range, 'tv');
  for (const field of ['color_space', 'color_transfer', 'color_primaries']) assert.equal(video[field], 'bt709');
  assert.equal(Number(video.nb_read_frames), 600);
  assert(Math.abs(Number(value.format.duration) - 25) < 0.1);
  assert(!(video.side_data_list || []).some(item => /dovi|dolby|mastering|content light/i.test(item.side_data_type)));
  return { codec: 'h264', width: video.width, height: video.height, duration: Number(value.format.duration), frames: Number(video.nb_read_frames), pixelFormat: video.pix_fmt, color: 'bt709', range: 'limited', audioStreams: 0 };
}
async function configurePlayer() {
  await page.bringToFront();
  await page.goto(playerURL);
  await page.getByLabel('用户名', { exact: true }).fill(config.username);
  await page.getByLabel('密码', { exact: true }).fill(config.password);
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await page.getByRole('navigation', { name: '主导航' }).waitFor();
  await page.goto(`${playerURL}/#/settings`);
  const motion = page.getByRole('switch', { name: /^动态背景/ });
  if (await motion.getAttribute('aria-checked') !== 'true') await motion.click();
  for (let index = 0; index < 2; index++) {
    const button = page.getByRole('button', { name: '提高本地生成短片优先级', exact: true });
    if (await button.isEnabled()) await button.click();
  }
  await wait(async () => {
    const saved = JSON.parse((await api(`/emby/DisplayPreferences/goby-player?UserId=${userId}&Client=Goby%20Player`)).CustomPrefs?.preferences || '{}');
    return saved.backgroundMotion === true && saved.backgroundSources?.[0] === 'generated';
  }, 'The source preference did not persist.');
  await screenshot(page, 'generated-source-preference');
}
async function playback(key) {
  const item = sources.get(key);
  const before = (await api(`/emby/Users/${userId}/Items/${item.Id}`)).UserData;
  const requests = [];
  const listener = request => requests.push({ method: request.method(), path: new URL(request.url()).pathname });
  page.on('request', listener);
  try {
    await page.bringToFront();
    await page.goto(`${playerURL}/#/detail/${item.Id}`);
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
        resolve({ width: video.videoWidth, height: video.videoHeight, duration: video.duration, startTime: start,
          finalTime: video.currentTime, muted: video.muted, playbackRate: video.playbackRate, ended: video.ended,
          elapsedMs: performance.now() - startedAt, totalVideoFrames: quality.totalVideoFrames,
          droppedVideoFrames: quality.droppedVideoFrames, displayedFrames: quality.totalVideoFrames - quality.droppedVideoFrames });
      }, { once: true });
      video.addEventListener('error', () => { clearTimeout(timer); reject(new Error('The generated background failed to decode.')); }, { once: true });
    }));
    assert.equal(result.width, 1280); assert.equal(result.height, 720); assert.equal(result.duration, 25);
    assert.equal(result.muted, true); assert.equal(result.playbackRate, 1); assert.equal(result.ended, true);
    assert(result.startTime < 8 && result.finalTime >= 24.9 && result.totalVideoFrames >= 590 && result.displayedFrames >= 570, 'The complete background did not render with an acceptable frame count.');
    const expectedElapsed = (result.duration - result.startTime) * 1000;
    assert(result.elapsedMs >= expectedElapsed - 350 && result.elapsedMs <= expectedElapsed + 5000, 'Background playback did not finish at the natural real-time cadence.');
    assert(requests.some(value => /\/BackgroundPreview\/stream\.mp4$/.test(value.path)));
    assert(!requests.some(value => /\/PlaybackInfo$|\/Sessions\/Playing/.test(value.path)), 'Background playback created a feature playback session.');
    assert.deepEqual((await api(`/emby/Users/${userId}/Items/${item.Id}`)).UserData, before);
    await page.goto('about:blank');
    return { ...result, requests, playbackHistoryUnchanged: true };
  } finally { page.off('request', listener); }
}

try {
  evidence.host = await host('describe');
  assert.deepEqual(evidence.host.fixtures.map(value => value.key), ['p5', 'p84', 'p82']);
  browser = await chromium.launch({ headless: true, args: ['--no-sandbox'] });
  administrator = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN', reducedMotion: 'reduce' });
  consumer = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: 'zh-CN', reducedMotion: 'no-preference' });
  adminPage = await administrator.newPage(); page = await consumer.newPage();
  for (const [target, surface] of [[adminPage, 'administrator'], [page, 'consumer']]) {
    target.on('pageerror', error => evidence.failures.push({ surface, kind: 'page', message: safe(error) }));
  }
  if (config.bootstrap === true) await admin('POST', '/admin/v1/bootstrap', { SetupToken: config.setupToken, Name: config.username, Password: config.password });
  const auth = await json(consumer, 'POST', '/emby/Users/AuthenticateByName', { Username: config.username, Pw: config.password });
  token = auth.AccessToken; userId = auth.User.Id;
  await login();
  let libraries = (await admin('GET', '/admin/v1/libraries')).Items;
  library = libraries.find(value => value.Paths.includes(evidence.host.libraryPath));
  if (!library) library = (await admin('POST', '/admin/v1/libraries', { Name: 'Release Dolby profiles', CollectionType: 'movies', Paths: [evidence.host.libraryPath], Scan: false })).Library;
  assert.deepEqual(library.Paths, [evidence.host.libraryPath], 'The acceptance library must contain only the owned media root.');
  assert.equal(library.LibraryOptions.EnableBackgroundPreviewGeneration, false, 'The release library must start with automatic generation disabled.');
  await scan();
  const catalog = await api(`/emby/Items?ParentId=${library.Id}&Recursive=true&IncludeItemTypes=Movie&Fields=MediaSources,MediaStreams,Path`);
  for (const fixture of evidence.host.fixtures) {
    const matches = catalog.Items.filter(value => value.Path === fixture.path || value.MediaSources?.some(source => source.Path === fixture.path));
    assert.equal(matches.length, 1, `The scanned release fixture ${fixture.key} must match its exact source path once.`);
    const [item] = matches;
    assert.equal(item.Name, fixture.name);
    sources.set(fixture.key, item);
    assert.equal(await host('snapshot', fixture.key), null, 'Use a fresh owned fixture for first-generation acceptance.');
  }
  taskId = (await admin('GET', '/admin/v1/tasks')).Items.find(value => value.Key === 'media.background_preview_generation')?.Id;
  assert(taskId);
  const configuration = await admin('GET', '/admin/v1/background-previews/configuration');
  initialProfile = configuration.Profile;
  assert.deepEqual(configuration.Defaults, defaults);
  await configure(defaults);
  await phase('automatic generation defaults off and GET requests never enqueue work', async () => {
    const before = (await quietHistory()).count;
    for (const key of sources.keys()) {
      for (let index = 0; index < 3; index++) assert.equal((await descriptor(key)).Available, false);
    }
    const after = (await quietHistory()).count;
    assert.equal(after, before);
    for (const key of sources.keys()) assert.equal(await host('snapshot', key), null);
    return { automaticEnabled: false, descriptorTriggeredRuns: 0, mediaHelpers: (await host('processes')).media.length };
  });
  await configurePlayer();
  for (const fixture of evidence.host.fixtures) {
    await phase(`${fixture.key}: administrator UI produces a persistent 25-second GPU-converted clip`, async () => {
      const attempt = await uiStart(fixture.key);
      assert.equal(attempt.input.Force, false);
      const completed = await finish(attempt, 'ready', true);
      assert.equal(completed.Reused, false);
      const snapshot = await host('snapshot', fixture.key);
      assert(snapshot); assert.deepEqual(snapshot.temporaryFiles, []);
      assert.equal(snapshot.manifest.StartTicks, (fixture.startSeconds ?? 1) * 10_000_000);
      originals.set(fixture.key, snapshot);
      const artifact = await descriptor(fixture.key);
      assert(artifact.Available && artifact.StreamUrl);
      const range = await consumer.request.get(localURL(artifact.StreamUrl), { headers: { 'X-Emby-Token': token, Range: 'bytes=0-31' } });
      assert.equal(range.status(), 206); assert.equal((await range.body()).length, 32);
      assert.equal(range.headers()['content-type'], 'video/mp4');
      await openEditor(fixture.key); await screenshot(adminPage, `${fixture.key}-administrator-ready`);
      return { ...probeSummary(await host('probe', fixture.key)), generation: snapshot.generation, sha256: snapshot.sha256,
        relativeDirectory: snapshot.relativeDirectory, rangeStatus: 206, nonzeroStartSeconds: fixture.startSeconds ?? 1 };
    });
    await phase(`${fixture.key}: the independent player naturally completes the real background video`, () => playback(fixture.key));
  }
  await phase('ordinary requests reuse all three permanent publications', async () => {
    for (const key of sources.keys()) { assert.equal((await finish(await start(key))).Reused, true); await unchanged(key); }
    return { profiles: [...sources.keys()], bytesAndTimestampsPreserved: true };
  });
  await phase('an explicit regeneration failure retains the previous publication', async () => {
    const key = 'p5'; restrictedKey = key; await host('restrict', key);
    try {
      const completed = await finish(await start(key, true), 'failed');
      assert.equal((await descriptor(key)).Available, true); await unchanged(key);
      return { profile: key, errorCode: completed.ErrorCode, previousPublicationRetained: true };
    } finally { await host('restore', key); restrictedKey = undefined; }
  });
  await phase('cancelling an observed GPU encoder retains the previous publication and retires helpers', async () => {
    const key = 'p5', attempt = await start(key, true), observed = [];
    const running = await wait(async () => {
      const value = await detail(key), process = await host('processes');
      if (process.gpuEncoding) observed.push(process);
      return value.RequestedRevision === attempt.expectedRevision && value.State === 'running' && process.gpuEncoding && value;
    }, 'A real GPU encoder was not observed before cancellation.', 60_000, 100);
    pendingRun = running.RunId;
    ownedRuns.add(running.RunId);
    await admin('POST', `/admin/v1/task-runs/${running.RunId}/cancel`, {});
    await finish(attempt, 'cancelled', true, observed);
    await unchanged(key);
    assert.equal((await descriptor(key)).Available, true);
    return { profile: key, observedGPUEncoder: true, previousPublicationRetained: true, remainingMediaHelpers: (await host('processes')).media.length };
  });
  await phase('container restart preserves and reuses every original-adjacent publication', async () => {
    await adminPage.goto('about:blank'); await page.goto('about:blank'); await idle();
    await host('restart');
    await wait(async () => {
      try { return (await consumer.request.get(baseURL + '/emby/System/Info/Public', { timeout: 3000 })).ok(); } catch { return false; }
    }, 'The owned backend did not return after restart.', 120_000, 500);
    await login();
    for (const key of sources.keys()) {
      assert.equal((await descriptor(key)).Available, true); await unchanged(key);
      assert.equal((await finish(await start(key))).Reused, true); await unchanged(key);
    }
    return { profiles: [...sources.keys()], bytesAndTimestampsPreserved: true, sameImageId: (await host('describe')).imageId };
  });
  assert.deepEqual(evidence.failures, []);
  evidence.complete = true;
} catch (error) {
  evidence.failures.push({ kind: 'acceptance', message: safe(error) });
  if (adminPage) await screenshot(adminPage, 'administrator-failure').catch(() => undefined);
  if (page) await screenshot(page, 'consumer-failure').catch(() => undefined);
  console.error(safe(error));
  process.exitCode = 1;
} finally {
  const cleanup = async action => {
    try { await action(); } catch (error) { evidence.failures.push({ kind: 'cleanup', message: safe(error) }); process.exitCode = 1; }
  };
  if (restrictedKey) await cleanup(() => host('restore', restrictedKey));
  if (taskId && csrf) await cleanup(async () => {
    for (const [key, revision] of ownedRevisions) {
      const value = await detail(key);
      if (value.RequestedRevision === revision && value.RunId) ownedRuns.add(value.RunId);
    }
    for (const id of ownedRuns) {
      if (active.has((await admin('GET', `/admin/v1/task-runs/${id}`)).Run.State)) await admin('POST', `/admin/v1/task-runs/${id}/cancel`, {});
    }
    await idle();
  });
  if (initialProfile && csrf) await cleanup(() => configure(initialProfile));
  if (process.exitCode) evidence.complete = false;
  await save();
  await browser?.close();
  console.log(JSON.stringify({ complete: evidence.complete === true, phases: evidence.phases.length, directory: output }));
}
