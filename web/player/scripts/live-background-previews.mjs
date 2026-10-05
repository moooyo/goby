import { chromium } from '@playwright/test';
import assert from 'node:assert/strict';
import { readFile, writeFile, realpath, lstat, stat, readdir, mkdir, chmod, chown } from 'node:fs/promises';
import { createReadStream } from 'node:fs';
import { basename, dirname, isAbsolute, relative, resolve } from 'node:path';
import { createHash, randomUUID } from 'node:crypto';
import { execFileSync } from 'node:child_process';

assert.equal(process.platform, 'linux', 'Run this acceptance only in the designated remote Linux environment.');
const requestedRoot = process.env.GOBY_LIVE_ROOT;
assert(requestedRoot, 'Provide the explicitly owned live environment.');
const root = await realpath(requestedRoot);
assert.equal((await readFile(resolve(root, '.owner'), 'utf8')).trim(), 'goby-player-live-20261004-165c');
const config = JSON.parse(await readFile(resolve(root, 'browser.private.json'), 'utf8'));
assert.equal(config.baseURL, 'http://127.0.0.1:38974');
const container = 'goby-player-165c-goby-1';
const baseImage = 'goby-player-accepted-base:20261004';
const ticks = 10_000_000;
const defaults = { DurationSeconds: 25, MaxWidth: 1280, VideoBitrate: 1_500_000, MaxItemRuntimeSeconds: 1200 };
const activeStates = new Set(['pending', 'running', 'stopping']);
const output = resolve(root, 'artifacts/background-preview-live');
function owned(path) {
  const suffix = relative(root, path);
  assert(suffix !== '..' && !suffix.startsWith('../') && !isAbsolute(suffix), 'An acceptance path escaped its owned root.');
  return path;
}
owned(await realpath(dirname(output)));
await mkdir(output, { recursive: false }).catch(error => { if (error.code !== 'EEXIST') throw error; });
owned(await realpath(output));
assert(!(await lstat(output)).isSymbolicLink());
for (const name of await readdir(output)) assert(!(await lstat(resolve(output, name))).isSymbolicLink());
const media = owned(await realpath(resolve(root, 'media')));
const source = resolve(media, 'Movies/Player Integration (2026)/Player Integration.mp4');
assert.equal(await realpath(source), source);
const sidecarFor = path => resolve(dirname(path), 'backdrops/goby', createHash('sha256').update(basename(path)).digest('hex'));
const sidecar = sidecarFor(source);
const results = [];
const failures = [];
const requests = [];
const admissions = [];
const runs = [];
const browser = await chromium.launch({ headless: true, args: ['--no-sandbox'] });
const administrator = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN', reducedMotion: 'reduce' });
const consumer = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: 'zh-CN', reducedMotion: 'no-preference' });
const adminPage = await administrator.newPage();
const page = await consumer.newPage();
let csrf = '';
let movie;
let movieLibrary;
let automaticLibrary;
let taskId;
let initialProfile;
let initialStart;
let sourceTimes;
let sourceTouched = false;
let restrictedMode;
let pendingRun;
let firstInput;
let firstReceipt;
let originalOrder;
let originalMotion;

function safe(value) {
  let text = String(value);
  for (const secret of [config.password, config.token, csrf].filter(Boolean)) text = text.replaceAll(secret, '[redacted]');
  return text.replace(/(https?:\/\/[^\s?'"<>]+)\?[^\s'"<>]+/g, '$1?[redacted]');
}
for (const [target, surface] of [[adminPage, 'administrator'], [page, 'consumer']]) {
  target.on('pageerror', error => failures.push({ surface, kind: 'page', message: safe(error.message) }));
  target.on('request', request => requests.push({ surface, path: new URL(request.url()).pathname, method: request.method() }));
  target.on('response', async response => {
    const path = new URL(response.url()).pathname;
    if (response.status() >= 400 && /^\/(emby|admin\/v1)\//.test(path)) failures.push({ surface, kind: 'http', path, status: response.status() });
    if (path === '/admin/v1/media-analysis/runs' && response.ok()) {
      admissions.push({ input: response.request().postDataJSON(), receipt: await response.json() });
    }
  });
}

async function request(context, method, path, body, extraHeaders = {}, expected = undefined) {
  const native = path.startsWith('/admin/');
  const response = await context.request.fetch(config.baseURL + path, {
    method, data: body, headers: { ...(native ? { Origin: config.baseURL, 'X-CSRF-Token': csrf } : { 'X-Emby-Token': config.token }), ...extraHeaders },
  });
  assert(expected === undefined ? response.ok() : response.status() === expected, `${method} ${path.split('?')[0]} returned ${response.status()}.`);
  const payload = await response.text();
  return payload ? JSON.parse(payload) : undefined;
}
const admin = (method, path, body) => request(administrator, method, path, body);
const api = path => request(consumer, 'GET', path);
const itemDetail = () => admin('GET', `/admin/v1/items/${movie.Id}/background-preview`);
const descriptor = id => api(`/emby/Items/${id}/BackgroundPreview`);
async function loginAdministrator() {
  csrf = (await admin('POST', '/admin/v1/session', { Name: config.username, Password: config.password })).CSRFToken;
}
async function waitFor(check, message, timeout = 120_000, interval = 200) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const value = await check();
    if (value) return value;
    await new Promise(resolve => setTimeout(resolve, interval));
  }
  throw new Error(message);
}
function processArguments() {
  return execFileSync('docker', ['top', container, '-eo', 'pid,args'], { encoding: 'utf8' }).split('\n').slice(1);
}
function noMediaProcess() { return !processArguments().some(line => /\bffmpeg\b|\bffprobe\b|\/proc\/self\/fd\//.test(line)); }
function encoding() { return processArguments().some(line => line.includes('libx264') && line.includes('-frames:v')); }
async function taskIdle() {
  await waitFor(async () => {
    const value = await admin('GET', `/admin/v1/tasks/${taskId}`);
    return !value.Task.CurrentRun || !activeStates.has(value.Task.CurrentRun.State);
  }, 'The background task did not become idle.');
  await waitFor(noMediaProcess, 'A media helper remained after task retirement.', 30_000);
}
async function waitRun(attempt, allowed = ['ready']) {
  const { receipt, expectedRevision } = attempt;
  pendingRun = receipt.RunId;
  const run = await waitFor(async () => {
    const value = await admin('GET', `/admin/v1/task-runs/${receipt.RunId}`);
    return !activeStates.has(value.Run.State) && value.Run;
  }, 'The admitted background task did not reach a terminal state.');
  const detail = await waitFor(async () => {
    const value = await itemDetail();
    return value.RequestedRevision === expectedRevision && value.CompletedRevision === expectedRevision && !['pending', 'running'].includes(value.State) && value;
  }, 'The selected movie did not finish this admitted operation.');
  assert(allowed.includes(detail.State), `The movie finished as ${detail.State}: ${detail.ErrorCode}`);
  await taskIdle();
  pendingRun = undefined;
  runs.push({ receiptRunId: run.Id, actualRunId: detail.RunId, requestedRevision: expectedRevision, receiptState: run.State, itemState: detail.State, reused: detail.Reused, errorCode: detail.ErrorCode });
  return detail;
}
async function start(force = false, input = undefined) {
  await taskIdle();
  const before = await itemDetail();
  const selected = input || { Kind: 'background', RequestId: randomUUID(), LibraryIds: [], ItemIds: [movie.Id], Force: force };
  const receipt = await admin('POST', '/admin/v1/media-analysis/runs', selected);
  pendingRun = receipt.RunId;
  return { input: selected, receipt, expectedRevision: String(BigInt(before.RequestedRevision) + 1n) };
}
async function configure(profile) {
  const value = await admin('GET', '/admin/v1/background-previews/configuration');
  return admin('PUT', '/admin/v1/background-previews/configuration', { Revision: value.Revision, Profile: profile });
}
async function libraryAutomatic(id, enabled) {
  const value = await admin('GET', `/admin/v1/libraries/${id}`);
  return admin('PATCH', `/admin/v1/libraries/${id}`, { Revision: value.Library.Revision, LibraryOptions: { EnableBackgroundPreviewGeneration: enabled } });
}
async function scan(id) {
  const receipt = await admin('POST', `/admin/v1/libraries/${id}/scan`, { ForceProbe: false });
  await waitFor(async () => {
    const value = await admin('GET', '/admin/v1/jobs');
    const job = value.Items.find(item => item.Id === receipt.Job.Id);
    assert(!['failed', 'canceled', 'cancelled'].includes(job?.Status), 'The owned fixture scan failed.');
    return job && ['completed', 'succeeded'].includes(job.Status);
  }, 'The owned fixture scan did not finish.');
}
async function digest(path) {
  const hash = createHash('sha256');
  for await (const chunk of createReadStream(path)) hash.update(chunk);
  return hash.digest('hex');
}
async function disk(path = source) {
  const directory = sidecarFor(path);
  const information = await lstat(directory).catch(error => { if (error.code === 'ENOENT') return undefined; throw error; });
  if (!information) return null;
  assert(information.isDirectory() && !information.isSymbolicLink());
  assert.equal(owned(await realpath(directory)), directory);
  const owner = JSON.parse(await readFile(resolve(directory, '.owner.json'), 'utf8'));
  assert.equal(owner.Format, 'goby-background-clip-v1');
  assert.equal(owner.SourceName, basename(path));
  const manifestPath = resolve(directory, 'manifest.json');
  const manifestInfo = await lstat(manifestPath).catch(error => { if (error.code === 'ENOENT') return undefined; throw error; });
  if (!manifestInfo) return null;
  assert(manifestInfo.isFile() && !manifestInfo.isSymbolicLink());
  const manifest = JSON.parse(await readFile(manifestPath, 'utf8'));
  assert.equal(manifest.SourceName, basename(path));
  assert(/^gen-[a-f0-9]{32}\.mp4$/.test(manifest.Generation));
  const file = resolve(directory, manifest.Generation);
  assert.equal(await realpath(file), file);
  const fileStat = await stat(file, { bigint: true });
  const manifestStat = await stat(manifestPath, { bigint: true });
  const sha256 = await digest(file);
  assert.equal(sha256, manifest.SHA256);
  assert.equal(Number(fileStat.size), manifest.Size);
  return { generation: manifest.Generation, sha256, size: Number(fileStat.size), fileMtimeNs: String(fileStat.mtimeNs), manifestMtimeNs: String(manifestStat.mtimeNs), manifestSHA256: await digest(manifestPath), manifest };
}
async function unchanged(previous, path = source) {
  const current = await disk(path);
  assert.deepEqual(current, previous, 'A retained background clip or its manifest changed.');
  return current;
}
function probe(snapshot, path = source) {
  const input = '/media/' + relative(media, resolve(sidecarFor(path), snapshot.generation));
  return JSON.parse(execFileSync('docker', ['run', '--rm', '--network', 'none', '--cpus', '0.5', '--memory', '256m',
    '-v', `${media}:/media:ro`, '--entrypoint', '/opt/ffmpeg/9.0.1/bin/ffprobe', baseImage,
    '-v', 'error', '-show_entries', 'format=duration:stream=codec_type,codec_name,width,height', '-of', 'json', input], { encoding: 'utf8' }));
}
function assertProbe(snapshot, expectedDuration, expectedWidth, path = source) {
  const value = probe(snapshot, path);
  assert.equal(value.streams.length, 1);
  assert.equal(value.streams[0].codec_type, 'video');
  assert.equal(value.streams[0].codec_name, 'h264');
  assert.equal(value.streams[0].width, expectedWidth);
  assert(Math.abs(Number(value.format.duration) - expectedDuration) < 0.1);
  return { duration: Number(value.format.duration), width: value.streams[0].width, height: value.streams[0].height, codec: 'h264', audioStreams: 0 };
}
async function screenshot(target, name) { await target.screenshot({ path: resolve(output, `${name}.png`), fullPage: true }); }
async function persist() { await writeFile(resolve(output, 'results.json'), JSON.stringify({ results, failures, requests, admissions, runs }, null, 2)); }
async function phase(name, action) {
  const evidence = await action();
  results.push({ name, passed: true, ...(evidence || {}) });
  await persist();
  console.log(JSON.stringify({ phase: name, passed: true }));
}
async function openEditor() {
  await adminPage.bringToFront();
  await adminPage.goto(`${config.baseURL}/admin/media/libraries/${movieLibrary.Id}/items`);
  await adminPage.getByRole('button', { name: `More actions for ${movie.Name}`, exact: true }).click();
  await adminPage.getByRole('menuitem', { name: '背景短片', exact: true }).click();
  const dialog = adminPage.getByRole('dialog', { name: '背景短片', exact: true });
  await dialog.getByLabel('手动截取起点（秒）', { exact: true }).waitFor();
  assert.equal(await adminPage.locator('#background-editor-title').count(), 1);
  return dialog;
}
async function uiGenerate(force) {
  await taskIdle();
  const before = await itemDetail();
  const dialog = await openEditor();
  const response = adminPage.waitForResponse(value => new URL(value.url()).pathname === '/admin/v1/media-analysis/runs' && value.request().method() === 'POST');
  if (force) {
    await dialog.getByRole('button', { name: '重新生成', exact: true }).click();
    await adminPage.getByRole('dialog', { name: '重新生成背景短片？', exact: true }).getByRole('button', { name: '确认重新生成', exact: true }).click();
  } else await dialog.getByRole('button', { name: '生成缺失短片', exact: true }).click();
  const submitted = await response;
  assert.equal(submitted.status(), 202);
  const receipt = await submitted.json();
  return { input: submitted.request().postDataJSON(), receipt, expectedRevision: String(BigInt(before.RequestedRevision) + 1n) };
}
async function waitPreferences(order, motion) {
  await waitFor(async () => {
    const value = await api(`/emby/DisplayPreferences/goby-player?UserId=${config.userId}&Client=Goby%20Player`);
    const saved = JSON.parse(value.CustomPrefs?.preferences || '{}');
    return JSON.stringify(saved.backgroundSources) === JSON.stringify(order) && saved.backgroundMotion === motion;
  }, 'The background preferences did not finish saving.');
}
async function setSourceTimes(atimeNs, mtimeNs) {
  assert.equal(owned(await realpath(source)), source);
  execFileSync('python3', ['-c', 'import os,sys; os.utime(sys.argv[1], ns=(int(sys.argv[2]),int(sys.argv[3])))', source, String(atimeNs), String(mtimeNs)]);
}
async function automaticFixture() {
  const directory = resolve(media, 'BackgroundAcceptance/Auto Background (2026)');
  for (const path of [dirname(directory), directory]) {
    const entry = await lstat(path).catch(error => { if (error.code === 'ENOENT') return undefined; throw error; });
    if (entry) assert(entry.isDirectory() && !entry.isSymbolicLink());
    else await mkdir(path);
  }
  assert.equal(owned(await realpath(directory)), directory);
  for (const path of [dirname(directory), directory]) { await chmod(path, 0o755); await chown(path, 10001, 10001); }
  const file = resolve(directory, 'Auto Background.mp4');
  const present = await lstat(file).catch(error => { if (error.code === 'ENOENT') return undefined; throw error; });
  if (present) assert(present.isFile() && !present.isSymbolicLink());
  else execFileSync('docker', ['run', '--rm', '--network', 'none', '--user', '10001:10001', '--cpus', '1', '--memory', '256m',
    '-v', `${media}:/media`, '--entrypoint', '/opt/ffmpeg/9.0.1/bin/ffmpeg', baseImage,
    '-hide_banner', '-loglevel', 'error', '-f', 'lavfi', '-i', 'testsrc2=size=320x180:rate=24', '-t', '64', '-an',
    '-c:v', 'libx264', '-preset', 'veryfast', '-crf', '32', '-pix_fmt', 'yuv420p', '-threads', '1', '-movflags', '+faststart',
    '/media/' + relative(media, file)], { stdio: ['ignore', 'ignore', 'pipe'] });
  await chmod(file, 0o644);
  const libraries = await admin('GET', '/admin/v1/libraries');
  let library = libraries.Items.find(item => item.Paths.includes('/media/BackgroundAcceptance'));
  if (!library) library = (await admin('POST', '/admin/v1/libraries', { Name: 'Player background automatic', CollectionType: 'movies', Paths: ['/media/BackgroundAcceptance'], Scan: false })).Library;
  automaticLibrary = library;
  assert.equal(library.LibraryOptions.EnableBackgroundPreviewGeneration, false, 'Automatic generation must start disabled.');
  await scan(library.Id);
  const catalog = await api(`/emby/Items?ParentId=${library.Id}&Recursive=true&IncludeItemTypes=Movie`);
  assert.equal(catalog.Items.length, 1);
  return { library, file, item: catalog.Items[0] };
}

try {
  await loginAdministrator();
  const catalog = await api('/emby/Items?Recursive=true&IncludeItemTypes=Movie,Series');
  movie = catalog.Items.find(item => item.Name === 'Player Integration' && item.Type === 'Movie');
  const series = catalog.Items.find(item => item.Name === 'Player Series' && item.Type === 'Series');
  assert(movie && series);
  const libraries = await admin('GET', '/admin/v1/libraries');
  movieLibrary = libraries.Items.find(item => item.Paths.includes('/media/Movies'));
  assert(movieLibrary);
  taskId = (await admin('GET', '/admin/v1/tasks')).Items.find(item => item.Key === 'media.background_preview_generation')?.Id;
  assert(taskId, 'The background generation task must be registered.');
  const initialConfiguration = await admin('GET', '/admin/v1/background-previews/configuration');
  initialProfile = initialConfiguration.Profile;
  assert.deepEqual(initialConfiguration.Defaults, defaults);
  await configure(defaults);
  initialStart = (await itemDetail()).StartTicks;

  await phase('default off and descriptor reads do not schedule generation', async () => {
    assert.equal(movieLibrary.LibraryOptions.EnableBackgroundPreviewGeneration, false);
    await taskIdle();
    const before = await disk();
    const jobs = await admin('GET', `/admin/v1/tasks/${taskId}/runs?Limit=50&StartIndex=0`);
    for (let index = 0; index < 3; index++) await descriptor(movie.Id);
    assert.equal((await descriptor(series.Id)).Available, false);
    await new Promise(resolve => setTimeout(resolve, 800));
    assert.equal((await admin('GET', `/admin/v1/tasks/${taskId}/runs?Limit=50&StartIndex=0`)).TotalRecordCount, jobs.TotalRecordCount);
    await unchanged(before);
    assert(noMediaProcess());
    await adminPage.bringToFront();
    await adminPage.goto(`${config.baseURL}/admin/system/tasks`);
    const available = adminPage.getByRole('list', { name: 'Available server tasks', exact: true });
    const task = available.getByRole('listitem').filter({ has: adminPage.getByRole('heading', { name: 'Generate background clips', exact: true }) });
    await task.getByText('请求生成背景短片时', { exact: true }).waitFor();
    await task.getByRole('button', { name: 'Edit schedule', exact: true }).click();
    const schedule = adminPage.getByRole('dialog', { name: 'Edit schedule · Generate background clips', exact: true });
    await schedule.getByRole('combobox', { name: 'System event', exact: true }).filter({ hasText: '请求生成背景短片' }).waitFor();
    assert.equal(await schedule.getByRole('button', { name: 'Save schedule', exact: true }).isEnabled(), false);
    await screenshot(adminPage, 'task-background-schedule');
    await schedule.getByRole('button', { name: 'Close', exact: true }).click();
    return { automaticEnabled: false, descriptorTriggeredRuns: 0, initiallyAvailable: before !== null, taskCenterAndSystemEventVisible: true };
  });

  await phase('opt in automatic generation and toggles retain the adjacent clip', async () => {
    const automatic = await automaticFixture();
    const before = await disk(automatic.file);
    assert.equal((await descriptor(automatic.item.Id)).Available, Boolean(before));
    await libraryAutomatic(automatic.library.Id, true);
    await waitFor(async () => (await descriptor(automatic.item.Id)).Available, 'Enabling automatic generation did not publish a clip.');
    await taskIdle();
    const saved = await disk(automatic.file);
    assert(saved);
    const mediaInfo = assertProbe(saved, 25, 1280, automatic.file);
    await libraryAutomatic(automatic.library.Id, false);
    await scan(automatic.library.Id);
    await taskIdle();
    await unchanged(saved, automatic.file);
    await libraryAutomatic(automatic.library.Id, true);
    await scan(automatic.library.Id);
    await taskIdle();
    await unchanged(saved, automatic.file);
    await libraryAutomatic(automatic.library.Id, false);
    return { ...mediaInfo, initiallyAvailable: before !== null, generation: saved.generation, disableEnableAndScanPreserved: true };
  });

  await phase('manual start saves without starting generation', async () => {
    const before = await disk();
    const previous = await itemDetail();
    const dialog = await openEditor();
    const start = previous.StartTicks === 125_000_000 ? '12.6' : '12.5';
    await dialog.getByLabel('手动截取起点（秒）', { exact: true }).fill(start);
    await dialog.getByRole('button', { name: '保存起点', exact: true }).click();
    await dialog.getByText('截取起点已保存。已有短片保持不变，只有明确重新生成时才应用新起点。', { exact: true }).waitFor();
    const saved = await itemDetail();
    assert.equal(saved.StartTicks, Number(start) * ticks);
    assert.equal(saved.RequestedRevision, previous.RequestedRevision);
    await unchanged(before);
    assert(noMediaProcess());
    await screenshot(adminPage, 'manual-start-saved');
    return { startTicks: saved.StartTicks, queuedWork: false };
  });

  let original;
  await phase('administrator generates a persistent silent H264 clip', async () => {
    const before = await disk();
    const attempt = await uiGenerate(Boolean(before));
    ({ input: firstInput, receipt: firstReceipt } = attempt);
    const detail = await waitRun(attempt);
    assert.equal(detail.Reused, false);
    original = await disk();
    assert(original);
    const mediaInfo = assertProbe(original, 25, 1280);
    const artifact = await descriptor(movie.Id);
    assert(artifact.Available && artifact.StreamUrl);
    assert.equal(artifact.StartPositionTicks, detail.StartTicks);
    assert.equal(new URL(artifact.StreamUrl, config.baseURL).searchParams.get('tag'), `"background-${original.generation}-${original.sha256}"`);
    const response = await consumer.request.get(config.baseURL + artifact.StreamUrl, { headers: { 'X-Emby-Token': config.token, Range: 'bytes=0-31' } });
    assert.equal(response.status(), 206);
    assert.equal((await response.body()).length, 32);
    assert.equal(response.headers()['content-type'], 'video/mp4');
    await openEditor();
    await screenshot(adminPage, 'generated-ready');
    return { ...mediaInfo, initialCreation: !before, rangeStatus: 206, adjacentDirectory: relative(media, sidecar), generation: original.generation, sha256: original.sha256 };
  });

  await phase('request replay and ordinary generation preserve bytes and timestamps', async () => {
    const replay = await admin('POST', '/admin/v1/media-analysis/runs', firstInput);
    assert.equal(replay.RunId, firstReceipt.RunId);
    assert.equal(replay.Admitted, false);
    await unchanged(original);
    const normal = await start();
    const detail = await waitRun(normal);
    assert.equal(detail.Reused, true);
    await unchanged(original);
    return { idempotentRequest: true, ordinaryRequestReused: true };
  });

  await phase('profile changes and disabling automatic generation keep the saved clip', async () => {
    await adminPage.goto(`${config.baseURL}/admin/media/analysis`);
    await adminPage.getByRole('button', { name: '生成设置', exact: true }).click();
    const dialog = adminPage.getByRole('dialog', { name: '背景短片生成设置', exact: true });
    await dialog.getByLabel('短片时长（秒）', { exact: true }).fill('5');
    await dialog.getByRole('button', { name: '保存生成设置', exact: true }).click();
    await dialog.getByText('生成规格已保存，只影响后续首次生成和明确执行的重新生成。已有短片保持不变。', { exact: true }).waitFor();
    assert.equal((await admin('GET', '/admin/v1/background-previews/configuration')).Profile.DurationSeconds, 5);
    await libraryAutomatic(movieLibrary.Id, false);
    await unchanged(original);
    const normal = await start();
    assert.equal((await waitRun(normal)).Reused, true);
    await unchanged(original);
    await screenshot(adminPage, 'profile-change-retains-clip');
    return { configuredDuration: 5, retainedDuration: original.manifest.DurationTicks / ticks };
  });

  await phase('generated first user priority decodes a visible background without history', async () => {
    const history = (await api(`/emby/Users/${config.userId}/Items/${movie.Id}`)).UserData;
    const requestIndex = requests.length;
    await page.bringToFront();
    await page.goto(config.baseURL);
    await page.getByLabel('用户名', { exact: true }).fill(config.username);
    await page.getByLabel('密码', { exact: true }).fill(config.password);
    await page.getByRole('button', { name: '登录', exact: true }).click();
    await page.getByRole('navigation', { name: '主导航' }).waitFor();
    await page.goto(`${config.baseURL}/#/settings`);
    const order = page.getByRole('list', { name: '背景素材优先级', exact: true });
    await order.waitFor();
    originalOrder = await order.locator('li[data-source]').evaluateAll(items => items.map(item => item.dataset.source));
    const motion = page.getByRole('switch', { name: /^动态背景/ });
    originalMotion = await motion.getAttribute('aria-checked');
    if (originalMotion !== 'true') await motion.click();
    for (let index = 0; index < 2; index++) {
      const button = page.getByRole('button', { name: '提高本地生成短片优先级', exact: true });
      if (await button.isEnabled()) await button.click();
    }
    const configuredOrder = await order.locator('li[data-source]').evaluateAll(items => items.map(item => item.dataset.source));
    await waitPreferences(configuredOrder, true);
    await screenshot(page, 'generated-first-setting');
    await page.reload();
    await waitFor(async () => (await page.locator('.settings-background-order li[data-source]').first().getAttribute('data-source')) === 'generated', 'The priority setting did not persist.');
    await page.goto(`${config.baseURL}/#/detail/${movie.Id}`);
    await page.waitForFunction(id => {
      const video = document.querySelector('.background-preview');
      return video?.dataset.previewOwner === id && video.dataset.previewKind === 'generated' && video.readyState >= 2 && !video.paused && video.currentTime >= 1 && video.currentTime <= 4 && Number(getComputedStyle(video).opacity) >= 0.95;
    }, movie.Id, { timeout: 30_000 });
    const visual = await page.locator('.background-preview').evaluate(video => ({ kind: video.dataset.previewKind, opacity: Number(getComputedStyle(video).opacity), width: video.videoWidth, height: video.videoHeight, duration: video.duration, muted: video.muted }));
    assert.equal(visual.width, 1280);
    assert.equal(visual.duration, 25);
    assert.equal(visual.muted, true);
    await screenshot(page, 'generated-background');
    const actual = requests.slice(requestIndex).filter(value => value.surface === 'consumer');
    assert(actual.some(value => /\/BackgroundPreview\/stream\.mp4$/.test(value.path)));
    assert(!actual.some(value => /\/PlaybackInfo$|\/Sessions\/Playing/.test(value.path)));
    assert.deepEqual((await api(`/emby/Users/${config.userId}/Items/${movie.Id}`)).UserData, history);
    assert(noMediaProcess());
    await page.goto('about:blank');
    return { ...visual, playbackHistoryUnchanged: true };
  });

  await phase('container restart reuses the original-adjacent publication', async () => {
    await adminPage.goto('about:blank');
    execFileSync('docker', ['restart', container], { stdio: 'ignore', timeout: 60_000 });
    await waitFor(async () => {
      try { return (await consumer.request.get(config.baseURL + '/emby/System/Info/Public')).ok(); } catch { return false; }
    }, 'The owned backend did not return after its restart.');
    await loginAdministrator();
    assert.equal((await descriptor(movie.Id)).Available, true);
    await unchanged(original);
    await taskIdle();
    return { generation: original.generation, sha256: original.sha256, restartReused: true };
  });

  await phase('source change is audited while the previous clip remains available', async () => {
    sourceTimes = await stat(source, { bigint: true });
    sourceTouched = true;
    await setSourceTimes(sourceTimes.atimeNs, sourceTimes.mtimeNs + 1_000_000_000n);
    await scan(movieLibrary.Id);
    const changed = await descriptor(movie.Id);
    assert.equal(changed.Available, true);
    assert.equal(changed.SourceChanged, true);
    await unchanged(original);
    await openEditor();
    await screenshot(adminPage, 'source-change-retains-clip');
    await setSourceTimes(sourceTimes.atimeNs, sourceTimes.mtimeNs);
    sourceTouched = false;
    await scan(movieLibrary.Id);
    return { sourceChangedWasReported: true, generation: original.generation };
  });

  await phase('failed explicit regeneration preserves the previous publication', async () => {
    await disk();
    restrictedMode = (await stat(sidecar)).mode & 0o777;
    try {
      await chmod(sidecar, 0o500);
      const attempt = await start(true);
      const detail = await waitRun(attempt, ['failed']);
      assert.equal((await descriptor(movie.Id)).Available, true);
      await unchanged(original);
      return { itemState: detail.State, errorCode: detail.ErrorCode, oldGenerationRetained: true };
    } finally { await chmod(sidecar, restrictedMode); restrictedMode = undefined; }
  });

  await phase('cancelling a running encoder preserves the previous publication', async () => {
    await configure({ ...defaults, DurationSeconds: 60, MaxWidth: 1920, VideoBitrate: 4_000_000 });
    const attempt = await start(true);
    const running = await waitFor(async () => {
      const value = await itemDetail();
      return value.RequestedRevision === attempt.expectedRevision && value.State === 'running' && encoding() && value;
    }, 'A real background encoder was not observed before cancellation.', 30_000, 80);
    pendingRun = running.RunId;
    await admin('POST', `/admin/v1/task-runs/${running.RunId}/cancel`, {});
    const detail = await waitRun(attempt, ['cancelled']);
    await unchanged(original);
    assert(noMediaProcess());
    assert(!(await readdir(sidecar)).some(name => name.endsWith('.part')), 'A cancelled operation left temporary output files.');
    return { itemState: detail.State, observedRunningEncoder: true, oldGenerationRetained: true };
  });

  await phase('successful explicit regeneration atomically replaces the old clip', async () => {
    await configure({ ...defaults, DurationSeconds: 5 });
    const beforeDescriptor = await descriptor(movie.Id);
    const attempt = await uiGenerate(true);
    assert.equal(attempt.input.Force, true);
    assert.equal((await waitRun(attempt)).Reused, false);
    const replacement = await disk();
    assert(replacement && replacement.generation !== original.generation && replacement.sha256 !== original.sha256);
    assert.equal((await readdir(sidecar)).includes(original.generation), false);
    const mediaInfo = assertProbe(replacement, 5, 1280);
    const after = await descriptor(movie.Id);
    assert.equal(after.Available, true);
    assert.notEqual(after.StreamUrl, beforeDescriptor.StreamUrl);
    const old = await consumer.request.get(config.baseURL + beforeDescriptor.StreamUrl, { headers: { 'X-Emby-Token': config.token } });
    assert.equal(old.status(), 404);
    await openEditor();
    await screenshot(adminPage, 'explicit-regeneration-ready');
    return { ...mediaInfo, newGeneration: replacement.generation, oldGenerationRemoved: true, staleVersionStatus: 404 };
  });
  assert.deepEqual(failures, []);
} catch (error) {
  await screenshot(adminPage, 'administrator-failure').catch(() => undefined);
  await screenshot(page, 'consumer-failure').catch(() => undefined);
  await writeFile(resolve(output, 'failure.json'), JSON.stringify({ message: safe(error.message), results, failures, requests, admissions, runs }, null, 2));
  console.error(safe(error.message));
  process.exitCode = 1;
} finally {
  const cleanup = async action => { try { await action(); } catch (error) { failures.push({ kind: 'cleanup', message: safe(error.message) }); console.error(safe(error.message)); process.exitCode = 1; } };
  if (restrictedMode !== undefined) await cleanup(() => chmod(sidecar, restrictedMode));
  if (automaticLibrary && csrf) await cleanup(() => libraryAutomatic(automaticLibrary.Id, false));
  if (taskId && csrf) await cleanup(async () => {
    const current = (await admin('GET', `/admin/v1/tasks/${taskId}`)).Task.CurrentRun;
    for (const id of new Set([pendingRun, current?.Id].filter(Boolean))) {
      if (activeStates.has((await admin('GET', `/admin/v1/task-runs/${id}`)).Run.State)) await admin('POST', `/admin/v1/task-runs/${id}/cancel`, {});
    }
    await taskIdle();
  });
  if (sourceTouched && sourceTimes) await cleanup(async () => { await setSourceTimes(sourceTimes.atimeNs, sourceTimes.mtimeNs); await scan(movieLibrary.Id); });
  if (initialProfile && csrf) await cleanup(() => configure(initialProfile));
  if (movie && initialStart !== undefined && csrf) await cleanup(async () => {
    const current = await itemDetail();
    if (current.StartTicks !== initialStart) await admin('PUT', `/admin/v1/items/${movie.Id}/background-preview`, { Revision: current.Revision, SourceRevision: current.SourceRevision, StartTicks: initialStart });
  });
  if (originalOrder) await cleanup(async () => {
    await page.bringToFront();
    await page.goto(`${config.baseURL}/#/settings`);
    const labels = { theme: '主题视频', generated: '本地生成短片', trailer: '本地预告片' };
    for (let index = originalOrder.length - 1; index >= 0; index--) {
      const button = page.getByRole('button', { name: `提高${labels[originalOrder[index]]}优先级`, exact: true });
      for (let step = 0; step < 2; step++) if (await button.isEnabled()) await button.click();
    }
    const motion = page.getByRole('switch', { name: /^动态背景/ });
    if (originalMotion !== undefined && await motion.getAttribute('aria-checked') !== originalMotion) await motion.click();
    await waitPreferences(originalOrder, originalMotion === 'true');
  });
  await persist();
  await administrator.close();
  await consumer.close();
  await browser.close();
  if (!process.exitCode) console.log(JSON.stringify({ complete: true, phases: results.length, realBackend: true, persistentSidecars: true }));
}
