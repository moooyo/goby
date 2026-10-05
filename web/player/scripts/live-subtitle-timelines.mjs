import { chromium } from '@playwright/test';
import assert from 'node:assert/strict';
import { readFile, writeFile, realpath, lstat, stat, readdir, mkdir, chmod, chown } from 'node:fs/promises';
import { createReadStream } from 'node:fs';
import { basename, dirname, isAbsolute, relative, resolve } from 'node:path';
import { createHash, randomUUID } from 'node:crypto';
import { execFileSync } from 'node:child_process';

assert.equal(process.platform, 'linux', 'Run only in the designated remote Linux environment.');
assert(process.env.GOBY_LIVE_ROOT, 'Provide the explicitly owned live environment.');
const root = await realpath(process.env.GOBY_LIVE_ROOT);
assert.equal((await readFile(resolve(root, '.owner'), 'utf8')).trim(), 'goby-player-live-20261004-165c');
const fixtureFormat = 'goby-subtitle-timeline-acceptance-v1';
const artifactFormat = 'goby-subtitle-timelines-v1';
const container = 'goby-player-165c-goby-1';
const image = 'goby-player-accepted-base:20261004';
const ticks = 10_000_000;
const active = new Set(['pending', 'running', 'stopping']);
const fixtureRoot = resolve(root, 'media/SubtitleTimelineAcceptance');
const inputRoot = resolve(root, 'subtitle-timeline-fixtures');
const outputRoot = resolve(root, 'artifacts/subtitle-timelines-live');
const output = resolve(outputRoot, `run-${new Date().toISOString().replaceAll(':', '-').replaceAll('.', '-')}`);
const source = resolve(fixtureRoot, 'Subtitle Timeline (2026)/Subtitle Timeline.mkv');
const sidecar = resolve(dirname(source), 'backdrops/goby-subtitle-timelines', createHash('sha256').update(basename(source)).digest('hex'));
const results = [], failures = [], optionalResponses = [], requests = [], runs = [];
let config, csrf = '', uiToken = '', browser, administrator, consumer, adminPage, page;
let movie, movieLibrary, taskId, originalTimes, originalCatalog, originalUserData, pendingRun, restrictedMode;
let sourceTouched = false, sourceNeedsRefresh = false, cleanupComplete = false;

function owned(path) {
  const suffix = relative(root, path);
  assert(suffix !== '..' && !suffix.startsWith('../') && !isAbsolute(suffix), 'An acceptance path escaped its owned root.');
  return path;
}
async function directory(path, writable = false) {
  owned(path);
  assert.equal(owned(await realpath(dirname(path))), dirname(path));
  await mkdir(path).catch(error => { if (error.code !== 'EEXIST') throw error; });
  assert((await lstat(path)).isDirectory() && !(await lstat(path)).isSymbolicLink());
  assert.equal(await realpath(path), path);
  if (writable) { await chmod(path, 0o755); await chown(path, 10001, 10001); }
}
async function ownedDirectory(path, writable = false) {
  const present = await lstat(path).catch(error => { if (error.code === 'ENOENT') return undefined; throw error; });
  if (present) {
    assert(present.isDirectory() && !present.isSymbolicLink());
    assert.equal((await readFile(resolve(path, '.owner'), 'utf8')).trim(), fixtureFormat);
  }
  await directory(path, writable);
  if (!present) await writeFile(resolve(path, '.owner'), fixtureFormat + '\n', { flag: 'wx' });
}
async function digest(path) {
  const hash = createHash('sha256');
  for await (const block of createReadStream(path)) hash.update(block);
  return hash.digest('hex');
}
function safe(value) {
  let text = String(value);
  for (const secret of [config?.password, config?.token, csrf, uiToken].filter(Boolean)) text = text.replaceAll(secret, '[redacted]');
  return text.replace(/(https?:\/\/[^\s?'"<>]+)\?[^\s'"<>]+/g, '$1?[redacted]');
}
function tool(name, args, writable = false) {
  return execFileSync('docker', ['run', '--rm', '--network', 'none', '--user', '10001:10001', '--cpus', '1', '--memory', '256m', '--pids-limit', '64',
    '-v', `${fixtureRoot}:/fixture${writable ? '' : ':ro'}`, '-v', `${inputRoot}:/inputs:ro`, '--entrypoint', `/opt/ffmpeg/9.0.1/bin/${name}`, image, ...args],
  { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], timeout: 60_000, maxBuffer: 8 << 20 });
}
function probe(path) {
  return JSON.parse(tool('ffprobe', ['-v', 'error', '-show_packets', '-show_data_hash', 'sha256', '-show_streams', '-show_format',
    '-show_entries', 'packet=stream_index,pts_time,duration_time,size,data_hash:format=start_time,duration:stream=index,codec_name,codec_type,start_time,time_base:stream_tags=language,title:stream_disposition=forced,default', '-of', 'json', path]));
}
function union(values) {
  const output = [];
  for (const value of values.sort((a, b) => a.StartTicks - b.StartTicks || a.EndTicks - b.EndTicks)) {
    const last = output.at(-1);
    if (last && value.StartTicks <= last.EndTicks) last.EndTicks = Math.max(last.EndTicks, value.EndTicks);
    else output.push({ ...value });
  }
  return output;
}
async function fixtures() {
  assert.equal(await realpath(inputRoot), inputRoot);
  await ownedDirectory(fixtureRoot, true);
  await directory(dirname(source), true);
  const manifest = JSON.parse(await readFile(resolve(inputRoot, 'manifest.json'), 'utf8'));
  const selected = [
    { file: 'overlap-pgs.mks', caseName: 'overlap', type: 'pgs', index: 1, language: 'eng', forced: 0 },
    { file: 'chinese-forced-dvd.mks', caseName: 'chinese-forced', type: 'dvd', index: 2, language: 'zho', forced: 1 },
  ];
  for (const input of selected) {
    const path = resolve(inputRoot, input.file);
    assert.equal(await realpath(path), path);
    assert.equal(await digest(path), manifest.files[input.file].sha256, 'An authored input differs from its independent manifest.');
  }
  const existing = await lstat(source).catch(error => { if (error.code === 'ENOENT') return undefined; throw error; });
  if (existing) assert(existing.isFile() && !existing.isSymbolicLink());
  else tool('ffmpeg', ['-hide_banner', '-nostdin', '-loglevel', 'error', '-n', '-filter_threads', '1', '-copyts',
    '-f', 'lavfi', '-i', 'color=c=0x124860:s=320x180:r=12:d=12', '-i', '/inputs/overlap-pgs.mks', '-i', '/inputs/chinese-forced-dvd.mks',
    '-map', '0:v:0', '-map', '1:s:0', '-map', '2:s:0', '-an', '-c:v', 'libx264', '-preset', 'veryfast', '-crf', '24', '-pix_fmt', 'yuv420p',
    '-threads', '1', '-g', '24', '-bf', '0', '-c:s', 'copy', '-metadata:s:s:0', 'language=eng', '-metadata:s:s:0', 'title=Original PGS intervals',
    '-metadata:s:s:1', 'language=zho', '-metadata:s:s:1', 'title=Chinese forced DVD', '-disposition:s:0', '0', '-disposition:s:1', 'forced',
    '-avoid_negative_ts', 'disabled', '/fixture/Subtitle Timeline (2026)/Subtitle Timeline.mkv'], true);
  assert.equal(await realpath(source), source);
  const mux = probe('/fixture/Subtitle Timeline (2026)/Subtitle Timeline.mkv');
  await writeFile(resolve(output, 'mux-probe.json'), JSON.stringify(mux, null, 2));
  const originTicks = Math.round(Number(mux.format.start_time) * ticks);
  const durationTicks = Math.round(Number(mux.format.duration) * ticks);
  assert(Number.isSafeInteger(originTicks));
  assert.equal(durationTicks, 12 * ticks);
  assert.deepEqual(mux.streams.map(value => value.codec_type), ['video', 'subtitle', 'subtitle']);
  const tracks = [];
  for (const input of selected) {
    const before = probe('/inputs/' + input.file);
    await writeFile(resolve(output, `input-${input.type}-probe.json`), JSON.stringify(before, null, 2));
    const packets = mux.packets.filter(value => value.stream_index === input.index);
    assert.equal(packets.length, before.packets.length);
    const deltas = packets.map((packet, index) => {
      assert.equal(packet.data_hash, before.packets[index].data_hash, 'Copy mux changed the authored subtitle packet.');
      return Math.round((Number(packet.pts_time) - Number(before.packets[index].pts_time)) * ticks);
    });
    assert(deltas.length && deltas.every(value => value === deltas[0]), 'Copy mux changed relative subtitle timing.');
    const stream = mux.streams.find(value => value.index === input.index);
    assert.equal(stream.tags.language, input.language);
    assert.equal(stream.disposition.forced, input.forced);
    const authored = manifest.cases.find(value => value.name === input.caseName);
    const sourceOrigin = authored[`${input.type}_container_origin_ticks`] || 0;
    const intervals = union(authored[`${input.type}_intervals`].map(value => ({
      StartTicks: value.start_ticks + sourceOrigin + deltas[0] - originTicks,
      EndTicks: value.end_ticks + sourceOrigin + deltas[0] - originTicks,
    })));
    assert(intervals.every(value => value.StartTicks >= 0 && value.EndTicks <= durationTicks));
    tracks.push({ StreamIndex: input.index, Codec: stream.codec_name, Language: stream.tags.language, Forced: Boolean(stream.disposition.forced),
      Intervals: intervals, inputSHA256: manifest.files[input.file].sha256, packetCount: packets.length, muxDeltaTicks: deltas[0] });
  }
  assert.equal(tracks[0].Intervals.length, 2);
  assert(tracks[0].Intervals[1].StartTicks > tracks[0].Intervals[0].EndTicks);
  const oracle = { format: fixtureFormat, sourceSHA256: await digest(source), sourceBytes: (await stat(source)).size, originTicks, durationTicks, tracks };
  await writeFile(resolve(output, 'oracle.json'), JSON.stringify(oracle, null, 2));
  console.log(JSON.stringify({ fixturePrepared: true, existing: Boolean(existing), bytes: oracle.sourceBytes, durationTicks, tracks: tracks.map(value => ({ index: value.StreamIndex, intervals: value.Intervals, muxDeltaTicks: value.muxDeltaTicks })) }));
  return oracle;
}
await ownedDirectory(outputRoot);
await directory(output);
const oracle = await fixtures();
if (process.env.GOBY_PREPARE_ONLY === '1') process.exit(0);

config = JSON.parse(await readFile(resolve(root, 'browser.private.json'), 'utf8'));
assert.equal(config.baseURL, 'http://127.0.0.1:38974');
assert.equal(execFileSync('docker', ['inspect', '--format', '{{index .Config.Labels "com.docker.compose.project"}}', container], { encoding: 'utf8' }).trim(), 'goby-player-165c');
assert.equal(execFileSync('docker', ['exec', 'goby-player-165c-db-1', 'psql', '-U', 'fixture_admin', '-d', 'player', '-Atc', 'SELECT max(version) FROM schema_migrations'], { encoding: 'utf8' }).trim(), '60');
originalTimes = await stat(source, { bigint: true });

async function request(context, method, path, body, expected) {
  const native = path.startsWith('/admin/');
  const response = await context.request.fetch(config.baseURL + path, { method, data: body,
    headers: native ? { Origin: config.baseURL, 'X-CSRF-Token': csrf } : { 'X-Emby-Token': config.token } });
  assert(expected === undefined ? response.ok() : response.status() === expected, `${method} ${path.split('?')[0]} returned ${response.status()}.`);
  if (path === '/admin/v1/media-analysis/runs' && method === 'POST') assert.equal(response.status(), 202);
  const text = await response.text();
  return text ? JSON.parse(text) : undefined;
}
const admin = (method, path, body) => request(administrator, method, path, body);
const api = path => request(consumer, 'GET', path);
const item = () => api(`/emby/Users/${config.userId}/Items/${movie.Id}?Fields=MediaSources,MediaStreams`);
const detail = () => admin('GET', `/admin/v1/items/${movie.Id}/subtitle-timelines`);
const descriptor = () => api(`/emby/Items/${movie.Id}/SubtitleTimelines?UserId=${config.userId}`);
async function waitFor(check, message, timeout = 120_000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) { const value = await check(); if (value) return value; await new Promise(resolve => setTimeout(resolve, 150)); }
  throw new Error(message);
}
function noMediaProcess() {
  return !execFileSync('docker', ['top', container, '-eo', 'pid,args'], { encoding: 'utf8' }).split('\n').slice(1).some(line => /\bffmpeg\b|\bffprobe\b|\/proc\/self\/fd\//.test(line));
}
async function idle() {
  await waitFor(async () => { const current = (await admin('GET', `/admin/v1/tasks/${taskId}`)).Task.CurrentRun; return !current || !active.has(current.State); }, 'The subtitle timeline task did not become idle.');
  await waitFor(noMediaProcess, 'A media helper remained after task retirement.', 30_000);
}
async function taskHistory() {
  const tasks = (await admin('GET', '/admin/v1/tasks')).Items.filter(value => value.Key.startsWith('media.'));
  const history = [];
  for (const value of tasks) history.push({ key: value.Key, count: (await admin('GET', `/admin/v1/tasks/${value.Id}/runs?Limit=1&StartIndex=0`)).TotalRecordCount });
  return history.sort((a, b) => a.key.localeCompare(b.key));
}
async function scan() {
  const receipt = await admin('POST', `/admin/v1/libraries/${movieLibrary.Id}/scan`, { ForceProbe: false });
  await waitFor(async () => {
    const job = (await admin('GET', '/admin/v1/jobs')).Items.find(value => value.Id === receipt.Job.Id);
    assert(!['failed', 'cancelled', 'canceled'].includes(job?.Status), 'The isolated subtitle library scan failed.');
    return job && ['completed', 'succeeded'].includes(job.Status);
  }, 'The isolated subtitle library scan did not finish.');
}
async function libraryEnabled(enabled) {
  const current = (await admin('GET', `/admin/v1/libraries/${movieLibrary.Id}`)).Library;
  return admin('PATCH', `/admin/v1/libraries/${movieLibrary.Id}`, { Revision: current.Revision, LibraryOptions: { EnableSubtitleTimelineGeneration: enabled } });
}
async function start(force = false, library = false) {
  await idle();
  const before = await detail();
  const input = { Kind: 'subtitle-timeline', RequestId: randomUUID(), LibraryIds: library ? [movieLibrary.Id] : [], ItemIds: library ? [] : [movie.Id], Force: force };
  const receipt = await admin('POST', '/admin/v1/media-analysis/runs', input);
  assert.deepEqual(Object.keys(receipt).sort(), ['Admitted', 'Queued', 'RunId', 'TaskId']);
  assert.equal(receipt.TaskId, taskId);
  assert.equal(receipt.Queued, 1);
  pendingRun = receipt.RunId;
  return { input, receipt, expectedRevision: String(BigInt(before.RequestedRevision) + 1n) };
}
async function waitRun(attempt, states = ['ready']) {
  pendingRun = attempt.receipt.RunId;
  const run = await waitFor(async () => { const value = (await admin('GET', `/admin/v1/task-runs/${pendingRun}`)).Run; return !active.has(value.State) && value; }, 'The admitted subtitle timeline run did not finish.');
  const completed = await waitFor(async () => {
    const value = await detail();
    return value.RequestedRevision === attempt.expectedRevision && value.CompletedRevision === attempt.expectedRevision && !['pending', 'running'].includes(value.State) && value;
  }, 'The selected movie did not complete its admitted subtitle timeline revision.');
  assert(states.includes(completed.State), `The subtitle timeline finished as ${completed.State}: ${completed.ErrorCode}`);
  await idle();
  pendingRun = undefined;
  runs.push({ input: attempt.input, receipt: attempt.receipt, state: run.State, actualRunId: completed.RunId, requestedRevision: completed.RequestedRevision, itemState: completed.State, errorCode: completed.ErrorCode, reused: completed.Reused });
  return completed;
}
async function disk() {
  const present = await lstat(sidecar).catch(error => { if (error.code === 'ENOENT') return undefined; throw error; });
  if (!present) return null;
  assert(present.isDirectory() && !present.isSymbolicLink());
  assert.equal(owned(await realpath(sidecar)), sidecar);
  const owner = JSON.parse(await readFile(resolve(sidecar, '.owner.json'), 'utf8'));
  assert.equal(owner.Format, artifactFormat); assert.equal(owner.SourceName, basename(source));
  const manifestPath = resolve(sidecar, 'manifest.json');
  const exists = await lstat(manifestPath).catch(error => { if (error.code === 'ENOENT') return undefined; throw error; });
  if (!exists) return null;
  assert(exists.isFile() && !exists.isSymbolicLink());
  const manifest = JSON.parse(await readFile(manifestPath, 'utf8'));
  assert.equal(manifest.Format, artifactFormat); assert.equal(manifest.SourceName, basename(source));
  assert(/^gen-[a-f0-9]{32}\.gstl$/.test(manifest.Generation));
  const file = resolve(sidecar, manifest.Generation);
  assert.equal(await realpath(file), file);
  const data = await readFile(file);
  assert.equal(createHash('sha256').update(data).digest('hex'), manifest.SHA256);
  assert.equal(data.length, manifest.Summary.Bytes);
  assert.equal(data.subarray(0, 4).toString(), 'GSTL');
  assert.equal(data.readUInt16LE(4), 1); assert.equal(data.readUInt16LE(6), 0);
  assert.equal(data.subarray(8, 40).toString('hex'), manifest.Summary.FFprobeSHA256);
  assert.equal(data.readBigInt64LE(40), BigInt(oracle.durationTicks));
  assert.equal(data.readUInt16LE(48), oracle.tracks.length);
  let offset = 50;
  for (const expected of oracle.tracks) {
    assert.equal(data.readUInt16LE(offset), expected.StreamIndex);
    assert.equal(data.readUInt8(offset + 2), expected.Codec === 'dvd_subtitle' ? 2 : 1);
    const warnings = data.readUInt8(offset + 3), count = data.readUInt32LE(offset + 4);
    assert.equal(count, expected.Intervals.length);
    offset += 8 + warnings;
    const intervals = [];
    for (let index = 0; index < count; index++, offset += 16) intervals.push({ StartTicks: Number(data.readBigInt64LE(offset)), EndTicks: Number(data.readBigInt64LE(offset + 8)) });
    assert.deepEqual(intervals, expected.Intervals, 'The persistent binary differs from the independent mux oracle.');
  }
  assert.equal(offset, data.length);
  return { generation: manifest.Generation, sha256: manifest.SHA256, size: data.length, fileMtimeNs: String((await stat(file, { bigint: true })).mtimeNs),
    manifestMtimeNs: String((await stat(manifestPath, { bigint: true })).mtimeNs), manifestSHA256: await digest(manifestPath), manifest };
}
const unchanged = async previous => assert.deepEqual(await disk(), previous, 'Persistent subtitle data changed without explicit regeneration.');
async function checkTracks(value) {
  assert.equal(value.Available, true); assert.equal(value.Stale, undefined);
  assert.equal(value.DurationTicks, oracle.durationTicks);
  assert.equal(value.MediaSourceId, movie.MediaSources[0].Id);
  assert.deepEqual(value.Streams.map(stream => [stream.StreamIndex, stream.Codec, stream.IntervalCount]), oracle.tracks.map(track => [track.StreamIndex, track.Codec, track.Intervals.length]));
  const tracks = [];
  for (const expected of oracle.tracks) {
    const stream = value.Streams.find(entry => entry.StreamIndex === expected.StreamIndex);
    const response = await consumer.request.get(config.baseURL + stream.Url, { headers: { 'X-Emby-Token': config.token } });
    assert.equal(response.status(), 200); assert.equal(response.headers()['content-type'], 'application/json');
    const track = await response.json();
    assert.deepEqual(track, { MediaSourceId: value.MediaSourceId, SourceVersion: value.SourceVersion, StreamIndex: expected.StreamIndex, DurationTicks: oracle.durationTicks, Intervals: expected.Intervals });
    tracks.push({ ...track, ETag: response.headers().etag });
  }
  return tracks;
}
async function shot(target, name) { await target.screenshot({ path: resolve(output, name + '.png'), fullPage: true }); }
async function persist() { await writeFile(resolve(output, 'results.json'), JSON.stringify({ format: fixtureFormat, results, failures, optionalResponses, requests, runs, cleanupComplete }, null, 2)); }
async function phase(name, action) { const evidence = await action(); results.push({ name, passed: true, ...(evidence || {}) }); await persist(); console.log(JSON.stringify({ phase: name, passed: true })); }
async function showEditor() {
  await adminPage.goto(`${config.baseURL}/admin/media/libraries/${movieLibrary.Id}/items`);
  await adminPage.getByRole('button', { name: `More actions for ${movie.Name}`, exact: true }).click();
  await adminPage.getByRole('menuitem', { name: '字幕时间轴', exact: true }).click();
  const dialog = adminPage.getByRole('dialog', { name: '字幕时间轴', exact: true });
  await dialog.getByRole('region', { name: '已保存的字幕时间轴', exact: true }).waitFor();
  return dialog;
}
async function showTimelines(count, viewport, name) {
  await page.setViewportSize(viewport);
  // A repeated identical hash is not a new route activation. Load a fresh
  // document so each observed descriptor belongs to this exact source state.
  await page.goto('about:blank');
  // The deck can restore its active media stage before the navigation settles.
  // Subscribe before loading, rather than waiting only for the later click.
  const response = page.waitForResponse(value => new URL(value.url()).pathname === `/emby/Items/${movie.Id}/SubtitleTimelines`);
  await page.goto(`${config.baseURL}/#/detail/${movie.Id}`);
  await page.waitForFunction(() => {
    const stage = document.querySelector('.stage-deck');
    const until = Number(stage?.getAttribute('data-locked-until'));
    return stage?.querySelector('.deck-panel[aria-label="媒体信息"]') && until > 0 && until <= performance.now();
  });
  await page.getByRole('navigation', { name: '页面分页', exact: true }).getByRole('button', { name: '媒体信息', exact: true }).click();
  assert((await response).ok());
  await page.waitForFunction(expected => document.querySelectorAll('.deck-panel[data-active=true] .detail-subtitle-lane').length === expected, count);
  await page.evaluate(() => document.fonts.ready);
  const visual = await page.locator('.deck-panel[data-active=true]').evaluate(panel => ({
    lanes: [...panel.querySelectorAll('.detail-subtitle-lane')].map(lane => ({ streamIndex: Number(lane.dataset.streamIndex), path: lane.querySelector('path')?.getAttribute('d'), box: lane.getBoundingClientRect().toJSON() })),
    labels: [...panel.querySelectorAll('.detail-track-label.subtitle')].map(label => ({ streamIndex: Number(label.dataset.streamIndex), text: label.textContent })),
    overflow: document.documentElement.scrollWidth > innerWidth,
  }));
  assert.equal(visual.lanes.length, count); assert.equal(visual.labels.length, count); assert.equal(visual.overflow, false);
  for (const lane of visual.lanes) {
    const expected = oracle.tracks.find(value => value.StreamIndex === lane.streamIndex);
    assert(expected);
    const commands = [...lane.path.matchAll(/M([\d.]+) 2H([\d.]+)V8H([\d.]+)Z/g)];
    assert.equal(commands.length, expected.Intervals.length);
    commands.forEach((command, index) => {
      assert(Math.abs(Number(command[1]) / 1000 * oracle.durationTicks - expected.Intervals[index].StartTicks) < 1);
      assert(Math.abs(Number(command[2]) / 1000 * oracle.durationTicks - expected.Intervals[index].EndTicks) < 1);
    });
    assert(lane.box.width > 0 && lane.box.height > 0);
  }
  if (name) await shot(page, name);
  return visual;
}
async function setTimes(mtime) {
  assert.equal(owned(await realpath(source)), source);
  execFileSync('python3', ['-c', 'import os,sys;os.utime(sys.argv[1],ns=(int(sys.argv[2]),int(sys.argv[3])))', source, String(originalTimes.atimeNs), String(mtime)]);
  sourceTouched = mtime !== originalTimes.mtimeNs; sourceNeedsRefresh = true;
}

try {
  browser = await chromium.launch({ headless: true, args: ['--no-sandbox'] });
  administrator = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN', reducedMotion: 'reduce' });
  consumer = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: 'zh-CN', reducedMotion: 'reduce' });
  adminPage = await administrator.newPage(); page = await consumer.newPage();
  for (const [target, surface] of [[adminPage, 'administrator'], [page, 'consumer']]) {
    target.on('pageerror', error => failures.push({ surface, kind: 'page', message: safe(error.message) }));
    target.on('request', value => requests.push({ surface, method: value.method(), path: new URL(value.url()).pathname }));
    target.on('response', async response => {
      const path = new URL(response.url()).pathname;
      if (response.status() === 404 && /\/ThumbnailSet$/.test(path)) optionalResponses.push({ surface, path, status: 404, reason: 'No generated seek thumbnails exist for this synthetic source.' });
      else if (response.status() >= 400 && /^\/(emby|admin\/v1)\//.test(path)) failures.push({ surface, kind: 'http', path, status: response.status() });
      if (surface === 'consumer' && path.endsWith('/AuthenticateByName') && response.ok()) uiToken = (await response.json()).AccessToken;
    });
  }
  csrf = (await admin('POST', '/admin/v1/session', { Name: config.username, Password: config.password })).CSRFToken;
  const runtime = (await admin('GET', '/admin/v1/media-analysis')).Runtime;
  assert.equal(runtime.SubtitleTimelineAvailable, true, `Subtitle timelines are unavailable: ${(runtime.SubtitleTimelineReasons || []).join(',')}`);
  taskId = (await admin('GET', '/admin/v1/tasks')).Items.find(value => value.Key === 'media.subtitle_timeline_generation')?.Id;
  assert(taskId);
  originalCatalog = (await api(`/emby/Users/${config.userId}/Items?Recursive=true&IncludeItemTypes=Movie,Episode&Limit=1000`)).Items.map(value => ({ Id: value.Id, UserData: value.UserData }));
  movieLibrary = (await admin('GET', '/admin/v1/libraries')).Items.find(value => value.Paths?.length === 1 && value.Paths[0] === '/media/SubtitleTimelineAcceptance');
  if (movieLibrary) assert.equal(movieLibrary.Name, 'Player subtitle timelines');
  else movieLibrary = (await admin('POST', '/admin/v1/libraries', { Name: 'Player subtitle timelines', CollectionType: 'movies', Paths: ['/media/SubtitleTimelineAcceptance'], Scan: false })).Library;
  assert.equal(movieLibrary.LibraryOptions.EnableSubtitleTimelineGeneration, false);
  await scan();
  const catalog = await api(`/emby/Items?ParentId=${movieLibrary.Id}&Recursive=true&IncludeItemTypes=Movie&Fields=MediaSources,MediaStreams`);
  assert.equal(catalog.Items.length, 1); movie = catalog.Items[0];
  assert.equal(movie.Name, 'Subtitle Timeline'); assert.equal(movie.RunTimeTicks, oracle.durationTicks);
  assert.deepEqual(movie.MediaSources[0].MediaStreams.filter(value => value.Type === 'Subtitle').map(value => [value.Index, value.Codec, value.Language, value.IsForced]), oracle.tracks.map(value => [value.StreamIndex, value.Codec, value.Language, value.Forced]));
  originalUserData = (await item()).UserData;
  await page.goto(config.baseURL);
  await page.getByLabel('用户名', { exact: true }).fill(config.username);
  await page.getByLabel('密码', { exact: true }).fill(config.password);
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await page.getByRole('navigation', { name: '主导航' }).waitFor();

  await phase('automatic generation defaults off and reads do not create tasks', async () => {
    await idle();
    const before = await disk(), history = await taskHistory();
    const initial = await descriptor();
    if (!before) assert.deepEqual(initial, { Available: false });
    for (let index = 0; index < 3; index++) await descriptor();
    const visual = await showTimelines(before ? oracle.tracks.length : 0, { width: 1440, height: 900 }, 'desktop-before-generation');
    await unchanged(before); assert.deepEqual(await taskHistory(), history); assert(noMediaProcess());
    await adminPage.goto(`${config.baseURL}/admin/media/libraries`);
    await adminPage.getByRole('button', { name: `More actions for ${movieLibrary.Name}`, exact: true }).click();
    await adminPage.getByRole('menuitem', { name: 'Edit library', exact: true }).click();
    const dialog = adminPage.getByRole('dialog', { name: 'Edit library', exact: true });
    assert.equal(await dialog.getByRole('checkbox', { name: '自动生成字幕时间轴', exact: true }).isChecked(), false);
    await shot(adminPage, 'automatic-default-off');
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
    return { initiallyAvailable: Boolean(before), generatedDuringReads: false, visual, taskHistory: history };
  });
  await phase('administrator generates persistent PGS and forced DVD intervals', async () => {
    const before = await disk();
    let attempt;
    if (before) attempt = await start(false);
    else {
      const previous = await detail();
      const dialog = await showEditor();
      const pending = adminPage.waitForResponse(value => new URL(value.url()).pathname === '/admin/v1/media-analysis/runs' && value.request().method() === 'POST');
      await dialog.getByRole('button', { name: '生成缺失时间轴', exact: true }).click();
      const response = await pending; assert.equal(response.status(), 202);
      const input = response.request().postDataJSON();
      assert.equal(input.Kind, 'subtitle-timeline'); assert.equal(input.Force, false); assert.deepEqual(input.ItemIds, [movie.Id]);
      attempt = { input, receipt: await response.json(), expectedRevision: String(BigInt(previous.RequestedRevision) + 1n) };
    }
    const completed = await waitRun(attempt);
    const saved = await disk(); assert(saved);
    const tracks = await checkTracks(await descriptor());
    await writeFile(resolve(output, 'generated-checkpoint.json'), JSON.stringify({ initiallyAvailable: Boolean(before), generation: saved.generation,
      bytes: saved.size, reused: completed.Reused, tracks, ffprobeSHA256: saved.manifest.Summary.FFprobeSHA256 }, null, 2));
    await showEditor(); await adminPage.getByRole('list', { name: '已生成时间轴的字幕轨', exact: true }).waitFor();
    await shot(adminPage, 'administrator-generated');
    return { generation: saved.generation, bytes: saved.size, reused: completed.Reused, tracks, ffprobeSHA256: saved.manifest.Summary.FFprobeSHA256 };
  });
  let current = await disk(), currentDescriptor = await descriptor();
  await phase('consumer renders real subtitle coverage on desktop and mobile without playback', async () => {
    const history = await taskHistory();
    const desktop = await showTimelines(2, { width: 1440, height: 900 }, 'desktop-generated');
    const mobile = await showTimelines(2, { width: 393, height: 852 }, 'mobile-generated');
    assert.deepEqual((await item()).UserData, originalUserData);
    assert.deepEqual(await taskHistory(), history);
    assert(!requests.some(value => value.surface === 'consumer' && /\/PlaybackInfo$|\/Sessions\/Playing|\/media-analysis\/runs$/.test(value.path)));
    const anonymous = await browser.newContext();
    const unauthorized = await anonymous.request.get(config.baseURL + `/emby/Items/${movie.Id}/SubtitleTimelines`);
    assert.equal(unauthorized.status(), 401);
    const unauthorizedTrack = await anonymous.request.get(config.baseURL + currentDescriptor.Streams[0].Url);
    assert.equal(unauthorizedTrack.status(), 401);
    const noAdminCookie = await consumer.request.get(config.baseURL + `/admin/v1/items/${movie.Id}/subtitle-timelines`, { headers: { 'X-Emby-Token': config.token } });
    assert.equal(noAdminCookie.status(), 401);
    await anonymous.close();
    return { desktop, mobile, anonymousDescriptorStatus: 401, anonymousTrackStatus: 401, nativeAdminRequiresSession: true, playbackHistoryUnchanged: true };
  });
  await phase('ordinary batch reuses files and explicit Force publishes a new generation', async () => {
    assert.equal((await waitRun(await start(false, true))).Reused, true);
    await unchanged(current); assert.deepEqual(await descriptor(), currentDescriptor);
    await libraryEnabled(true); await scan(); await idle(); await unchanged(current);
    await libraryEnabled(false); await idle(); await unchanged(current);
    const old = current, oldDescriptor = currentDescriptor;
    assert.equal((await waitRun(await start(true))).Reused, false);
    current = await disk(); currentDescriptor = await descriptor();
    assert.notEqual(current.generation, old.generation); assert.equal(current.sha256, old.sha256);
    assert.notEqual(currentDescriptor.Streams[0].Url, oldDescriptor.Streams[0].Url);
    assert.equal((await consumer.request.get(config.baseURL + oldDescriptor.Streams[0].Url, { headers: { 'X-Emby-Token': config.token } })).status(), 404);
    await checkTracks(currentDescriptor);
    return { ordinaryBatchReused: true, enablingAndDisablingRetained: true, previousGeneration: old.generation, newGeneration: current.generation, byteIdenticalForSameSource: true, oldVersionStatus: 404 };
  });
  await phase('failed explicit regeneration preserves the usable publication', async () => {
    assert.equal(execFileSync('docker', ['exec', container, 'id', '-u'], { encoding: 'utf8' }).trim(), '10001');
    restrictedMode = (await stat(sidecar)).mode & 0o777;
    try {
      await chmod(sidecar, 0o500);
      const completed = await waitRun(await start(true), ['failed']);
      await unchanged(current); await checkTracks(await descriptor());
      return { state: completed.State, errorCode: completed.ErrorCode, oldGenerationRetained: true, oldIntervalsRemainReadable: true };
    } finally { await chmod(sidecar, restrictedMode); restrictedMode = undefined; }
  });
  await phase('source changes hide stale rows while ordinary requests preserve the old file', async () => {
    await adminPage.goto('about:blank'); await page.goto('about:blank');
    await setTimes(originalTimes.mtimeNs + 1_000_000_000n); await scan();
    assert.deepEqual(await descriptor(), { Available: false, Stale: true });
    assert.equal((await consumer.request.get(config.baseURL + currentDescriptor.Streams[0].Url, { headers: { 'X-Emby-Token': config.token } })).status(), 404);
    await unchanged(current);
    assert.equal((await waitRun(await start(false))).Reused, true);
    await unchanged(current); assert.deepEqual(await descriptor(), { Available: false, Stale: true });
    const visual = await showTimelines(0, { width: 1440, height: 900 }, 'desktop-stale-hidden');
    await showEditor(); await adminPage.getByText('已过期', { exact: true }).waitFor();
    await shot(adminPage, 'administrator-stale-preserved');
    return { ordinaryRequestReused: true, oldGenerationRetained: true, oldVersionStatus: 404, visual };
  });
  await phase('restored mtime and explicit regeneration publish current rows', async () => {
    await adminPage.goto('about:blank'); await page.goto('about:blank');
    await setTimes(originalTimes.mtimeNs); await scan();
    assert.deepEqual(await descriptor(), { Available: false, Stale: true });
    const old = current;
    assert.equal((await waitRun(await start(true))).Reused, false);
    current = await disk(); currentDescriptor = await descriptor();
    assert.notEqual(current.generation, old.generation);
    const tracks = await checkTracks(currentDescriptor);
    assert.equal((await stat(source, { bigint: true })).mtimeNs, originalTimes.mtimeNs);
    sourceNeedsRefresh = false;
    const desktop = await showTimelines(2, { width: 1440, height: 900 }, 'desktop-restored');
    const mobile = await showTimelines(2, { width: 393, height: 852 }, 'mobile-restored');
    return { generation: current.generation, tracks, desktop, mobile, sourceMtimeRestored: true, sourceCtimeRestorable: false };
  });
  assert.deepEqual(failures, []);
} catch (error) {
  if (adminPage) await shot(adminPage, 'administrator-failure').catch(() => undefined);
  if (page) await shot(page, 'consumer-failure').catch(() => undefined);
  failures.push({ kind: 'acceptance', message: safe(error.message) });
  console.error(safe(error.message)); process.exitCode = 1;
} finally {
  const cleanup = async action => { try { await action(); } catch (error) { failures.push({ kind: 'cleanup', message: safe(error.message) }); console.error(safe(error.message)); process.exitCode = 1; } };
  await adminPage?.goto('about:blank').catch(() => undefined); await page?.goto('about:blank').catch(() => undefined);
  if (restrictedMode !== undefined) await cleanup(() => chmod(sidecar, restrictedMode));
  if (movieLibrary && csrf) await cleanup(() => libraryEnabled(false));
  if (taskId && csrf) await cleanup(async () => {
    if (pendingRun && active.has((await admin('GET', `/admin/v1/task-runs/${pendingRun}`)).Run.State)) await admin('POST', `/admin/v1/task-runs/${pendingRun}/cancel`, {});
    await idle();
  });
  if (sourceTouched) await cleanup(() => setTimes(originalTimes.mtimeNs));
  if (sourceNeedsRefresh && movie && taskId && csrf) await cleanup(async () => { await scan(); await waitRun(await start(true)); await checkTracks(await descriptor()); sourceNeedsRefresh = false; });
  if (movie && originalUserData) await cleanup(async () => assert.deepEqual((await item()).UserData, originalUserData, 'Acceptance changed the synthetic movie playback state.'));
  if (originalCatalog) await cleanup(async () => {
    const current = (await api(`/emby/Users/${config.userId}/Items?Recursive=true&IncludeItemTypes=Movie,Episode&Limit=1000`)).Items;
    for (const value of originalCatalog) assert.deepEqual(current.find(item => item.Id === value.Id)?.UserData, value.UserData, 'Acceptance changed an existing movie playback state.');
  });
  if (movieLibrary && csrf) await cleanup(async () => assert.equal((await admin('GET', `/admin/v1/libraries/${movieLibrary.Id}`)).Library.LibraryOptions.EnableSubtitleTimelineGeneration, false));
  cleanupComplete = !sourceTouched && !sourceNeedsRefresh && restrictedMode === undefined && !failures.some(value => value.kind === 'cleanup');
  await persist(); await browser?.close();
  console.log(JSON.stringify({ completedPhases: results.length, failures: failures.length, cleanupComplete, evidence: relative(root, output) }));
}
