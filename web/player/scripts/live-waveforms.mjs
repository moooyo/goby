import { chromium } from '@playwright/test';
import assert from 'node:assert/strict';
import { readFile, writeFile, realpath, lstat, stat, readdir, mkdir, chmod } from 'node:fs/promises';
import { createReadStream } from 'node:fs';
import { basename, dirname, isAbsolute, relative, resolve } from 'node:path';
import { createHash, randomUUID } from 'node:crypto';
import { execFileSync } from 'node:child_process';

assert.equal(process.platform, 'linux', 'Run this acceptance only in the designated remote Linux environment.');
assert(process.env.GOBY_LIVE_ROOT, 'Provide the explicitly owned live environment.');
const root = await realpath(process.env.GOBY_LIVE_ROOT);
assert.equal((await readFile(resolve(root, '.owner'), 'utf8')).trim(), 'goby-player-live-20261004-165c');
const config = JSON.parse(await readFile(resolve(root, 'browser.private.json'), 'utf8'));
assert.equal(config.baseURL, 'http://127.0.0.1:38974');
const container = 'goby-player-165c-goby-1';
assert.equal(execFileSync('docker', ['inspect', '--format', '{{index .Config.Labels "com.docker.compose.project"}}', container], { encoding: 'utf8' }).trim(), 'goby-player-165c');
const activeStates = new Set(['pending', 'running', 'stopping']);
const buckets = [512, 1024, 2048, 4096];
const ticks = 10_000_000;
const results = [];
const failures = [];
const optionalMediaResponses = [];
const requests = [];
const admissions = [];
const runs = [];
const output = resolve(root, 'artifacts/audio-waveforms-live');

function owned(path) {
  const suffix = relative(root, path);
  assert(suffix !== '..' && !suffix.startsWith('../') && !isAbsolute(suffix), 'An acceptance path escaped its owned root.');
  return path;
}
owned(await realpath(dirname(output)));
await mkdir(output).catch(error => { if (error.code !== 'EEXIST') throw error; });
assert.equal(owned(await realpath(output)), output);
assert(!(await lstat(output)).isSymbolicLink());
for (const entry of await readdir(output)) assert(!(await lstat(resolve(output, entry))).isSymbolicLink());
const media = owned(await realpath(resolve(root, 'media')));
const source = resolve(media, 'Movies/Player Integration (2026)/Player Integration.mp4');
assert.equal(await realpath(source), source);
const sourceNameHash = createHash('sha256').update(basename(source)).digest('hex');
const sidecar = resolve(dirname(source), 'backdrops/goby-waveforms', sourceNameHash);
const backgroundSidecar = resolve(dirname(source), 'backdrops/goby', sourceNameHash);
const originalTimes = await stat(source, { bigint: true });
const browser = await chromium.launch({ headless: true, args: ['--no-sandbox'] });
const administrator = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN', reducedMotion: 'reduce' });
const consumer = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: 'zh-CN', reducedMotion: 'reduce' });
const adminPage = await administrator.newPage();
const page = await consumer.newPage();
let csrf = '';
let movie;
let movieLibrary;
let taskId;
let initialAutomatic;
let initialMetadata;
let initialHistory;
let pendingRun;
let restrictedMode;
let sourceTouched = false;
let sourceNeedsRefresh = false;
let backgroundBefore;
let decoderObserved = false;
const observedStreams = new Set();

function safe(value) {
  let result = String(value);
  for (const secret of [config.password, config.token, csrf].filter(Boolean)) result = result.replaceAll(secret, '[redacted]');
  return result.replace(/(https?:\/\/[^\s?'"<>]+)\?[^\s'"<>]+/g, '$1?[redacted]');
}
for (const [target, surface] of [[adminPage, 'administrator'], [page, 'consumer']]) {
  target.on('pageerror', error => failures.push({ surface, kind: 'page', message: safe(error.message) }));
  target.on('request', request => requests.push({ surface, path: new URL(request.url()).pathname, method: request.method() }));
  target.on('response', async response => {
    const path = new URL(response.url()).pathname;
    if (response.status() === 404 && /^\/emby\/Items\/[^/]+\/ThumbnailSet$/.test(path)) {
      optionalMediaResponses.push({ surface, path, status: response.status(), reason: 'Optional seek thumbnails are unavailable after the source snapshot changes.' });
    } else if (response.status() >= 400 && /^\/(emby|admin\/v1)\//.test(path)) failures.push({ surface, kind: 'http', path, status: response.status() });
    if (path === '/admin/v1/media-analysis/runs' && response.ok()) admissions.push({ input: response.request().postDataJSON(), receipt: await response.json() });
  });
}

async function request(context, method, path, body, expected = undefined) {
  const native = path.startsWith('/admin/');
  const response = await context.request.fetch(config.baseURL + path, { method, data: body,
    headers: native ? { Origin: config.baseURL, 'X-CSRF-Token': csrf } : { 'X-Emby-Token': config.token } });
  assert(expected === undefined ? response.ok() : response.status() === expected, `${method} ${path.split('?')[0]} returned ${response.status()}.`);
  const text = await response.text();
  return text ? JSON.parse(text) : undefined;
}
const admin = (method, path, body) => request(administrator, method, path, body);
const api = path => request(consumer, 'GET', path);
const detail = () => admin('GET', `/admin/v1/items/${movie.Id}/audio-waveforms`);
const descriptor = () => api(`/emby/Items/${movie.Id}/AudioWaveforms?UserId=${config.userId}`);
const itemState = () => api(`/emby/Users/${config.userId}/Items/${movie.Id}`);
const statistics = () => api(`/emby/Users/${config.userId}/ViewingStatistics`);
async function loginAdministrator() { csrf = (await admin('POST', '/admin/v1/session', { Name: config.username, Password: config.password })).CSRFToken; }
async function waitFor(check, message, timeout = 120_000, interval = 200) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const value = await check();
    if (value) return value;
    await new Promise(resolve => setTimeout(resolve, interval));
  }
  throw new Error(message);
}
function processes() { return execFileSync('docker', ['top', container, '-eo', 'pid,args'], { encoding: 'utf8' }).split('\n').slice(1); }
function observeDecoder() {
  for (const line of processes()) if (/\bf32le\b/.test(line) && /\/proc\/self\/fd\//.test(line)) {
    decoderObserved = true;
    const index = line.match(/-map\s+0:(\d+)/)?.[1];
    if (index) observedStreams.add(Number(index));
  }
}
function noMediaProcess() { return !processes().some(line => /\bffmpeg\b|\bffprobe\b|\/proc\/self\/fd\//.test(line)); }
async function taskIdle() {
  await waitFor(async () => {
    const value = await admin('GET', `/admin/v1/tasks/${taskId}`);
    return !value.Task.CurrentRun || !activeStates.has(value.Task.CurrentRun.State);
  }, 'The waveform task did not become idle.');
  await waitFor(noMediaProcess, 'A media helper remained after task retirement.', 30_000);
}
async function waitRun(attempt, allowed = ['ready']) {
  pendingRun = attempt.receipt.RunId;
  const run = await waitFor(async () => {
    observeDecoder();
    const result = await admin('GET', `/admin/v1/task-runs/${attempt.receipt.RunId}`);
    return !activeStates.has(result.Run.State) && result.Run;
  }, 'The admitted waveform run did not finish.');
  const item = await waitFor(async () => {
    observeDecoder();
    const value = await detail();
    return value.RequestedRevision === attempt.expectedRevision && value.CompletedRevision === attempt.expectedRevision && !['pending', 'running'].includes(value.State) && value;
  }, 'The selected movie did not complete the admitted waveform operation.');
  assert(allowed.includes(item.State), `The waveform item finished as ${item.State}: ${item.ErrorCode}`);
  await taskIdle();
  pendingRun = undefined;
  runs.push({ receiptRunId: run.Id, actualRunId: item.RunId, requestedRevision: item.RequestedRevision, receiptState: run.State, itemState: item.State, reused: item.Reused, errorCode: item.ErrorCode });
  return item;
}
async function start(force = false, library = false) {
  await taskIdle();
  const before = await detail();
  const input = { Kind: 'waveform', RequestId: randomUUID(), LibraryIds: library ? [movieLibrary.Id] : [], ItemIds: library ? [] : [movie.Id], Force: force };
  const receipt = await admin('POST', '/admin/v1/media-analysis/runs', input);
  pendingRun = receipt.RunId;
  admissions.push({ input, receipt });
  return { receipt, expectedRevision: String(BigInt(before.RequestedRevision) + 1n) };
}
async function setLibraryOptions(options) {
  const current = await admin('GET', `/admin/v1/libraries/${movieLibrary.Id}`);
  return admin('PATCH', `/admin/v1/libraries/${movieLibrary.Id}`, { Revision: current.Library.Revision, LibraryOptions: options });
}
async function scan() {
  const receipt = await admin('POST', `/admin/v1/libraries/${movieLibrary.Id}/scan`, { ForceProbe: false });
  await waitFor(async () => {
    const result = await admin('GET', '/admin/v1/jobs');
    const job = result.Items.find(entry => entry.Id === receipt.Job.Id);
    assert(!['failed', 'cancelled', 'canceled'].includes(job?.Status), 'The synthetic library scan failed.');
    return job && ['completed', 'succeeded'].includes(job.Status);
  }, 'The synthetic library scan did not finish.');
}
async function digest(path) {
  const hash = createHash('sha256');
  for await (const chunk of createReadStream(path)) hash.update(chunk);
  return hash.digest('hex');
}
async function disk(directory = sidecar, format = 'goby-audio-waveforms-v1', extension = 'gawf') {
  const present = await lstat(directory).catch(error => { if (error.code === 'ENOENT') return undefined; throw error; });
  if (!present) return null;
  assert(present.isDirectory() && !present.isSymbolicLink());
  assert.equal(owned(await realpath(directory)), directory);
  const owner = JSON.parse(await readFile(resolve(directory, '.owner.json'), 'utf8'));
  assert.equal(owner.Format, format);
  assert.equal(owner.SourceName, basename(source));
  const manifestPath = resolve(directory, 'manifest.json');
  const information = await lstat(manifestPath).catch(error => { if (error.code === 'ENOENT') return undefined; throw error; });
  if (!information) return null;
  assert(information.isFile() && !information.isSymbolicLink());
  const manifest = JSON.parse(await readFile(manifestPath, 'utf8'));
  assert.equal(manifest.SourceName, basename(source));
  assert(new RegExp(`^gen-[a-f0-9]{32}\\.${extension}$`).test(manifest.Generation));
  const file = resolve(directory, manifest.Generation);
  assert.equal(await realpath(file), file);
  const fileInfo = await stat(file, { bigint: true });
  const manifestInfo = await stat(manifestPath, { bigint: true });
  const sha256 = await digest(file);
  assert.equal(sha256, manifest.SHA256);
  assert.equal(Number(fileInfo.size), extension === 'gawf' ? manifest.Summary.Bytes : manifest.Size);
  return { generation: manifest.Generation, sha256, size: Number(fileInfo.size), fileMtimeNs: String(fileInfo.mtimeNs), manifestMtimeNs: String(manifestInfo.mtimeNs), manifestSHA256: await digest(manifestPath), manifest };
}
async function unchanged(previous) { assert.deepEqual(await disk(), previous, 'A persistent waveform file or manifest changed without explicit regeneration.'); }
async function binaryLevels(value) {
  assert.equal(value.Available, true);
  assert.equal(value.Stale, undefined);
  assert.equal(value.DurationTicks, movie.RunTimeTicks);
  const originalAudio = movie.MediaSources[0].MediaStreams.filter(stream => stream.Type === 'Audio' && !stream.IsExternal);
  assert.deepEqual(value.Streams.map(track => track.StreamIndex), originalAudio.map(track => track.Index));
  const levels = [];
  for (const stream of value.Streams) {
    assert.deepEqual(stream.Levels.map(level => level.BucketCount), buckets);
    for (const level of stream.Levels) {
      const response = await consumer.request.get(config.baseURL + level.Url, { headers: { 'X-Emby-Token': config.token } });
      assert.equal(response.status(), 200);
      assert.equal(response.headers()['content-type'], 'application/octet-stream');
      const data = await response.body();
      assert.equal(data.subarray(0, 4).toString(), 'GAWL');
      assert.equal(data.readUInt16LE(4), 1);
      assert.equal(data.readUInt16LE(6), 32);
      assert.equal(data.readUInt32LE(8), stream.StreamIndex);
      assert.equal(data.readUInt32LE(12), level.BucketCount);
      assert.equal(data.readBigUInt64LE(16), BigInt(movie.RunTimeTicks));
      assert.equal(data.readUInt32LE(24), Math.ceil(level.BucketCount / 8));
      assert.equal(data.readUInt32LE(28), 0);
      assert.equal(data.length, 32 + level.BucketCount * 4 + Math.ceil(level.BucketCount / 8));
      let valid = 0;
      let nonzero = 0;
      for (let index = 0; index < level.BucketCount; index++) {
        const peak = data.readUInt16LE(32 + index * 4);
        const rms = data.readUInt16LE(34 + index * 4);
        const present = Boolean(data[32 + level.BucketCount * 4 + Math.floor(index / 8)] & (1 << (index % 8)));
        assert(rms <= peak);
        if (present) { valid++; if (peak > 0 && rms > 0) nonzero++; }
        else assert.equal(peak + rms, 0);
      }
      assert(valid > level.BucketCount * .95 && nonzero > level.BucketCount * .95, 'The generated tone track must contain real waveform energy over its timeline.');
      levels.push({ streamIndex: stream.StreamIndex, bucketCount: level.BucketCount, bytes: data.length, valid, nonzero, sha256: createHash('sha256').update(data).digest('hex') });
    }
  }
  return levels;
}
async function screenshot(target, name) { await target.screenshot({ path: resolve(output, `${name}.png`), fullPage: true }); }
async function persist() { await writeFile(resolve(output, 'results.json'), JSON.stringify({ results, failures, optionalMediaResponses, requests, admissions, runs, decoderObserved, observedStreams: [...observedStreams] }, null, 2)); }
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
  await adminPage.getByRole('menuitem', { name: '音轨波形', exact: true }).click();
  const dialog = adminPage.getByRole('dialog', { name: '音轨波形', exact: true });
  await dialog.getByRole('button', { name: '生成缺失波形', exact: true }).waitFor();
  await dialog.getByRole('region', { name: '已保存的音轨波形', exact: true }).waitFor();
  return dialog;
}
async function uiGenerate(force) {
  await taskIdle();
  const before = await detail();
  const dialog = await openEditor();
  const response = adminPage.waitForResponse(result => new URL(result.url()).pathname === '/admin/v1/media-analysis/runs' && result.request().method() === 'POST');
  if (force) {
    await dialog.getByRole('button', { name: '重新生成波形', exact: true }).click();
    await adminPage.getByRole('dialog', { name: '重新生成音轨波形？', exact: true }).getByRole('button', { name: '确认重新生成波形', exact: true }).click();
  } else await dialog.getByRole('button', { name: '生成缺失波形', exact: true }).click();
  const submitted = await response;
  assert.equal(submitted.status(), 202);
  assert.equal(submitted.request().postDataJSON().Kind, 'waveform');
  assert.equal(submitted.request().postDataJSON().Force, force);
  const receipt = await submitted.json();
  pendingRun = receipt.RunId;
  return { receipt, expectedRevision: String(BigInt(before.RequestedRevision) + 1n) };
}
async function showWaveforms(expected = 'ready') {
  await page.bringToFront();
  await page.goto(`${config.baseURL}/#/detail/${movie.Id}`);
  await page.waitForFunction(() => {
    const stage = document.querySelector('.stage-deck');
    const lockedUntil = Number(stage?.getAttribute('data-locked-until'));
    return stage?.querySelector('.deck-panel[aria-label="媒体信息"]') && lockedUntil > 0 && lockedUntil <= performance.now();
  });
  await page.getByRole('navigation', { name: '页面分页', exact: true }).getByRole('button', { name: '媒体信息', exact: true }).click();
  await page.waitForFunction(({ count, expected }) => {
    const lanes = [...document.querySelectorAll('.deck-panel[data-active=true] .detail-audio-lane')];
    return lanes.length === count && lanes.every(lane => lane.getAttribute('data-waveform-status') === expected);
  }, { count: movie.MediaSources[0].MediaStreams.filter(stream => stream.Type === 'Audio' && !stream.IsExternal).length, expected }, { timeout: 30_000 });
  return page.locator('.deck-panel[data-active=true] .detail-audio-lane');
}
async function setSourceTimes() {
  assert.equal(owned(await realpath(source)), source);
  execFileSync('python3', ['-c', 'import os,sys; os.utime(sys.argv[1],ns=(int(sys.argv[2]),int(sys.argv[3])))', source, String(originalTimes.atimeNs), String(originalTimes.mtimeNs)]);
  sourceTouched = false;
  sourceNeedsRefresh = true;
}

try {
  await loginAdministrator();
  const catalog = await api(`/emby/Users/${config.userId}/Items?Recursive=true&IncludeItemTypes=Movie,Episode&Fields=MediaSources,MediaStreams,UserData`);
  movie = catalog.Items.find(item => item.Name === 'Player Integration' && item.Type === 'Movie');
  assert(movie?.MediaSources?.length === 1 && movie.RunTimeTicks >= 179 * ticks && movie.RunTimeTicks <= 181 * ticks);
  assert.deepEqual(movie.MediaSources[0].MediaStreams.filter(stream => stream.Type === 'Audio').map(stream => stream.Index), [1, 2]);
  const libraries = await admin('GET', '/admin/v1/libraries');
  movieLibrary = libraries.Items.find(item => item.Paths.length === 1 && item.Paths[0] === '/media/Movies');
  assert(movieLibrary && catalog.Items.filter(item => item.ParentId === movieLibrary.Id && item.Type === 'Movie').every(item => item.Id === movie.Id));
  initialAutomatic = Boolean(movieLibrary.LibraryOptions.EnableAudioWaveformGeneration);
  initialMetadata = movieLibrary.LibraryOptions.EnableLocalMetadata;
  assert.equal(typeof initialMetadata, 'boolean');
  initialHistory = (await itemState()).UserData;
  backgroundBefore = await disk(backgroundSidecar, 'goby-background-clip-v1', 'mp4');
  taskId = (await admin('GET', '/admin/v1/tasks')).Items.find(item => item.Key === 'media.audio_waveform_generation')?.Id;
  assert(taskId, 'The waveform task must be registered.');

  await phase('automatic generation defaults off and descriptor reads do not create work', async () => {
    assert.equal(initialAutomatic, false);
    await taskIdle();
    const before = await disk();
    const history = await admin('GET', `/admin/v1/tasks/${taskId}/runs?Limit=50&StartIndex=0`);
    const value = await descriptor();
    if (!before) assert.deepEqual(value, { Available: false });
    for (let index = 0; index < 3; index++) await descriptor();
    await new Promise(resolve => setTimeout(resolve, 600));
    assert.equal((await admin('GET', `/admin/v1/tasks/${taskId}/runs?Limit=50&StartIndex=0`)).TotalRecordCount, history.TotalRecordCount);
    await unchanged(before);
    assert(noMediaProcess());
    await adminPage.goto(`${config.baseURL}/admin/media/libraries`);
    await adminPage.getByRole('button', { name: `More actions for ${movieLibrary.Name}`, exact: true }).click();
    await adminPage.getByRole('menuitem', { name: 'Edit library', exact: true }).click();
    const dialog = adminPage.getByRole('dialog', { name: 'Edit library', exact: true });
    assert.equal(await dialog.getByRole('checkbox', { name: '自动生成音轨波形', exact: true }).isChecked(), false);
    await screenshot(adminPage, 'automatic-waveforms-default-off');
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
    return { automaticEnabled: false, initiallyAvailable: Boolean(before), descriptorTriggeredRuns: 0 };
  });

  await phase('administrator generates persistent real waveforms for both original audio indexes', async () => {
    const before = await disk();
    const attempt = before ? await start(false) : await uiGenerate(false);
    const item = await waitRun(attempt);
    const saved = await disk();
    assert(saved && /^[a-f0-9]{64}$/.test(saved.manifest.Summary.FFmpegSHA256));
    assert.deepEqual(saved.manifest.Summary.Tracks.map(track => track.StreamIndex), [1, 2]);
    const levels = await binaryLevels(await descriptor());
    if (!before) assert(decoderObserved, 'The real waveform decoder was not observed.');
    await openEditor();
    await adminPage.getByRole('list', { name: '已生成波形的音轨', exact: true }).waitFor();
    await screenshot(adminPage, 'waveforms-generated');
    return { generation: saved.generation, artifactBytes: saved.size, reused: item.Reused, ffmpegSHA256: saved.manifest.Summary.FFmpegSHA256, levels };
  });
  let current = await disk();
  let currentDescriptor = await descriptor();

  await phase('consumer renders real waveform SVG and estimates visible watched content', async () => {
    const fromRequest = requests.length;
    await page.goto(config.baseURL);
    await page.getByLabel('用户名', { exact: true }).fill(config.username);
    await page.getByLabel('密码', { exact: true }).fill(config.password);
    await page.getByRole('button', { name: '登录', exact: true }).click();
    await page.getByRole('navigation', { name: '主导航' }).waitFor();
    const lanes = await showWaveforms();
    const visual = await lanes.evaluateAll(elements => elements.map(element => ({ streamIndex: Number(element.dataset.streamIndex), buckets: Number(element.querySelector('svg')?.getAttribute('data-bucket-count')), peakLength: element.querySelector('.detail-waveform-peaks')?.getAttribute('d')?.length || 0 })));
    assert.deepEqual(visual.map(entry => entry.streamIndex), [1, 2]);
    assert(visual.every(entry => buckets.includes(entry.buckets) && entry.peakLength > 100));
    await screenshot(page, 'consumer-real-waveforms');
    const visible = await api(`/emby/Users/${config.userId}/Items?Recursive=true&IncludeItemTypes=Movie,Episode&Limit=1000`);
    assert.equal(visible.Items.length, visible.TotalRecordCount);
    const total = visible.Items.reduce((sum, item) => {
      const duration = Number.isSafeInteger(item.RunTimeTicks) && item.RunTimeTicks > 0 ? item.RunTimeTicks : 0;
      const position = Math.max(0, Math.min(duration, item.UserData?.PlaybackPositionTicks || 0));
      return sum + BigInt(item.UserData?.Played ? duration : position);
    }, 0n);
    const value = await statistics();
    assert.equal(value.IsEstimate, true);
    assert.equal(value.EstimatedContentTicks, total.toString());
    assert.equal(value.EstimatedContentHours, Number((total + 1800n * BigInt(ticks)) / (3600n * BigInt(ticks))));
    await page.goto(`${config.baseURL}/#/settings`);
    const display = page.getByLabel(`已看内容时长估算 ${value.EstimatedContentHours} 小时`, { exact: true });
    await display.waitFor();
    assert.equal(await display.locator('strong').innerText(), String(value.EstimatedContentHours));
    assert((await display.getAttribute('title')).includes('重复观看不重复累计'));
    await screenshot(page, 'estimated-viewing-hours');
    assert.deepEqual((await itemState()).UserData, initialHistory);
    assert(!requests.slice(fromRequest).some(entry => entry.surface === 'consumer' && /\/PlaybackInfo$|\/Sessions\/Playing|\/media-analysis\/runs$/.test(entry.path)));
    return { visual, statistics: value, playbackHistoryUnchanged: true, noPlaybackOrGenerationRequests: true };
  });

  await phase('ordinary batch requests and library options retain hash and modification times', async () => {
    const before = await detail();
    await adminPage.goto(`${config.baseURL}/admin/media/analysis`);
    await adminPage.getByRole('tab', { name: '音轨波形', exact: true }).click();
    const panel = adminPage.getByRole('tabpanel', { name: '音轨波形', exact: true });
    await panel.getByRole('combobox', { name: '媒体库', exact: true }).click();
    await adminPage.getByRole('option', { name: movieLibrary.Name, exact: true }).click();
    const response = adminPage.waitForResponse(value => new URL(value.url()).pathname === '/admin/v1/media-analysis/runs' && value.request().method() === 'POST');
    await panel.getByRole('button', { name: `为「${movieLibrary.Name}」生成缺失波形`, exact: true }).click();
    const submitted = await response;
    assert.equal(submitted.status(), 202);
    assert.deepEqual(submitted.request().postDataJSON().LibraryIds, [movieLibrary.Id]);
    assert.equal(submitted.request().postDataJSON().Force, false);
    assert.equal((await waitRun({ receipt: await submitted.json(), expectedRevision: String(BigInt(before.RequestedRevision) + 1n) })).Reused, true);
    await unchanged(current);
    await screenshot(adminPage, 'batch-reuses-persistent-waveforms');
    await adminPage.goto('about:blank');
    await setLibraryOptions({ EnableAudioWaveformGeneration: true, EnableLocalMetadata: !initialMetadata });
    await scan();
    await taskIdle();
    await unchanged(current);
    await setLibraryOptions({ EnableAudioWaveformGeneration: false, EnableLocalMetadata: initialMetadata });
    await taskIdle();
    await unchanged(current);
    return { ordinaryBatchReused: true, enabledThenDisabled: true, metadataSettingRestored: true, sha256: current.sha256, fileMtimeNs: current.fileMtimeNs };
  });

  await phase('container restart retains the persistent source-adjacent waveforms', async () => {
    await adminPage.goto('about:blank');
    await page.goto('about:blank');
    execFileSync('docker', ['restart', container], { stdio: 'ignore', timeout: 60_000 });
    await waitFor(async () => { try { return (await consumer.request.get(config.baseURL + '/emby/System/Info/Public')).ok(); } catch { return false; } }, 'The owned backend did not return after restart.');
    await loginAdministrator();
    await taskIdle();
    await unchanged(current);
    assert.equal((await descriptor()).Available, true);
    return { generation: current.generation, sha256: current.sha256, fileMtimeNs: current.fileMtimeNs };
  });

  await phase('failed explicit regeneration preserves the previous usable publication', async () => {
    assert.equal(execFileSync('docker', ['exec', container, 'id', '-u'], { encoding: 'utf8' }).trim(), '10001');
    restrictedMode = (await stat(sidecar)).mode & 0o777;
    try {
      await chmod(sidecar, 0o500);
      const item = await waitRun(await start(true), ['failed']);
      await unchanged(current);
      assert.equal((await descriptor()).Available, true);
      await binaryLevels(await descriptor());
      return { state: item.State, errorCode: item.ErrorCode, oldGenerationRetained: true, oldLevelsRemainReadable: true };
    } finally { await chmod(sidecar, restrictedMode); restrictedMode = undefined; }
  });

  await phase('changed source hides stale axes while ordinary requests preserve the saved files', async () => {
    sourceTouched = true;
    sourceNeedsRefresh = true;
    execFileSync('python3', ['-c', 'import os,sys; os.utime(sys.argv[1],ns=(int(sys.argv[2]),int(sys.argv[3])))', source, String(originalTimes.atimeNs), String(originalTimes.mtimeNs + 1_000_000_000n)]);
    await scan();
    assert.deepEqual(await descriptor(), { Available: false, Stale: true });
    const oldLevel = await consumer.request.get(config.baseURL + currentDescriptor.Streams[0].Levels[0].Url, { headers: { 'X-Emby-Token': config.token } });
    assert.equal(oldLevel.status(), 404);
    await unchanged(current);
    const item = await waitRun(await start(false));
    assert.equal(item.Reused, true);
    await unchanged(current);
    assert.deepEqual(await descriptor(), { Available: false, Stale: true });
    const lanes = await showWaveforms('stale');
    assert.equal(await lanes.locator('svg').count(), 0);
    assert((await lanes.first().innerText()).includes('片源已变化，请重新生成波形'));
    await screenshot(page, 'stale-waveforms-hidden');
    await openEditor();
    await adminPage.getByText('已过期', { exact: true }).waitFor();
    await screenshot(adminPage, 'stale-waveforms-preserved');
    return { descriptor: { Available: false, Stale: true }, oldLevelStatus: 404, ordinaryRequestReused: true, oldGenerationRetained: true };
  });

  await phase('explicit regeneration after restoring source timestamps publishes a current axis', async () => {
    await adminPage.goto('about:blank');
    await page.goto('about:blank');
    await setSourceTimes();
    await scan();
    assert.equal((await descriptor()).Stale, true);
    const item = await waitRun(await uiGenerate(true));
    assert.equal(item.Reused, false);
    const replacement = await disk();
    assert(replacement && replacement.generation !== current.generation);
    assert.notEqual(replacement.manifest.SourceStamp, current.manifest.SourceStamp);
    const next = await descriptor();
    assert.equal(next.Available, true);
    assert.notEqual(next.Streams[0].Levels[0].Url, currentDescriptor.Streams[0].Levels[0].Url);
    const old = await consumer.request.get(config.baseURL + currentDescriptor.Streams[0].Levels[0].Url, { headers: { 'X-Emby-Token': config.token } });
    assert.equal(old.status(), 404);
    const levels = await binaryLevels(next);
    assert.equal((await stat(source, { bigint: true })).mtimeNs, originalTimes.mtimeNs);
    sourceNeedsRefresh = false;
    current = replacement;
    currentDescriptor = next;
    await showWaveforms();
    await screenshot(page, 'current-waveforms-after-explicit-regeneration');
    return { newGeneration: replacement.generation, sourceMtimeRestored: true, sourceCtimeRestorable: false, oldVersionStatus: 404, levels };
  });
  assert.deepEqual(failures, []);
} catch (error) {
  await screenshot(adminPage, 'administrator-failure').catch(() => undefined);
  await screenshot(page, 'consumer-failure').catch(() => undefined);
  await writeFile(resolve(output, 'failure.json'), JSON.stringify({ message: safe(error.message), results, failures, optionalMediaResponses, requests, admissions, runs }, null, 2));
  console.error(safe(error.message));
  process.exitCode = 1;
} finally {
  const cleanup = async action => { try { await action(); } catch (error) { failures.push({ kind: 'cleanup', message: safe(error.message) }); console.error(safe(error.message)); process.exitCode = 1; } };
  await adminPage.goto('about:blank').catch(() => undefined);
  await page.goto('about:blank').catch(() => undefined);
  if (restrictedMode !== undefined) await cleanup(() => chmod(sidecar, restrictedMode));
  if (movieLibrary && csrf && initialAutomatic !== undefined) await cleanup(() => setLibraryOptions({ EnableAudioWaveformGeneration: initialAutomatic, EnableLocalMetadata: initialMetadata }));
  if (taskId && csrf) await cleanup(async () => {
    for (const id of new Set([pendingRun].filter(Boolean))) if (activeStates.has((await admin('GET', `/admin/v1/task-runs/${id}`)).Run.State)) await admin('POST', `/admin/v1/task-runs/${id}/cancel`, {});
    await taskIdle();
  });
  if (sourceTouched) await cleanup(setSourceTimes);
  if (sourceNeedsRefresh && movieLibrary && taskId && csrf) await cleanup(async () => {
    await scan();
    await waitRun(await start(true));
    assert.equal((await descriptor()).Available, true);
    sourceNeedsRefresh = false;
  });
  if (movie && initialHistory) await cleanup(async () => assert.deepEqual((await itemState()).UserData, initialHistory, 'Waveform acceptance changed user playback state.'));
  if (backgroundBefore !== undefined) await cleanup(async () => assert.deepEqual(await disk(backgroundSidecar, 'goby-background-clip-v1', 'mp4'), backgroundBefore, 'Waveform acceptance changed an existing background clip.'));
  await persist();
  await administrator.close();
  await consumer.close();
  await browser.close();
  if (!process.exitCode) console.log(JSON.stringify({ complete: true, phases: results.length, realBackend: true, persistentSidecars: true, waveformLevels: 4, audioTracks: 2 }));
}
