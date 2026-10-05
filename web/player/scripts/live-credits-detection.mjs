import { chromium } from '@playwright/test';
import assert from 'node:assert/strict';
import { readFile, writeFile, open, realpath, lstat, stat, readdir, mkdir, chmod, chown } from 'node:fs/promises';
import { createReadStream } from 'node:fs';
import { basename, dirname, isAbsolute, relative, resolve } from 'node:path';
import { createHash, randomUUID } from 'node:crypto';
import { execFileSync } from 'node:child_process';

assert.equal(process.platform, 'linux', 'Use only the designated remote Linux environment.');
assert(process.env.GOBY_LIVE_ROOT, 'Provide the explicitly owned acceptance root.');
const root = await realpath(process.env.GOBY_LIVE_ROOT);
assert.equal((await readFile(resolve(root, '.owner'), 'utf8')).trim(), 'goby-player-live-20261004-165c');
const config = JSON.parse(await readFile(resolve(root, 'browser.private.json'), 'utf8'));
assert.equal(config.baseURL, 'http://127.0.0.1:38974');
const container = 'goby-player-165c-goby-1';
const image = 'goby-player-accepted-base:20261004';
const format = 'goby-credits-detection-fixture-v1';
const ticks = 10_000_000;
const active = new Set(['pending', 'running', 'stopping']);
const results = [], failures = [], optionalResponses = [], requests = [], runs = [], created = [], negotiations = [], detections = [];
const mediaRoot = resolve(root, 'media');
const fixtureRoot = resolve(mediaRoot, 'CreditsAcceptance');
const inputRoot = resolve(root, 'artifacts/credits-fixture-inputs');
const output = resolve(root, 'artifacts/credits-detection-live');
const labels = { chapter: 'Credits Chapter', visual: 'Credits Visual', negative: 'Credits Negative', series: 'Credits Audio Series' };
const paths = {
  chapter: resolve(fixtureRoot, `Movies/${labels.chapter} (2026)/${labels.chapter}.mp4`),
  visual: resolve(fixtureRoot, `Movies/${labels.visual} (2026)/${labels.visual}.mp4`),
  negative: resolve(fixtureRoot, `Movies/${labels.negative} (2026)/${labels.negative}.mp4`),
  first: resolve(fixtureRoot, `Shows/${labels.series}/Season 01/${labels.series} S01E01.mp4`),
  second: resolve(fixtureRoot, `Shows/${labels.series}/Season 01/${labels.series} S01E02.mp4`),
};
let csrf = '', taskId, movieLibrary, televisionLibrary, pendingRun;
let chapter, visual, negative, episodes, series;
let oldCatalog, originalManual;
const touched = new Map();
const userStates = new Map();
const playbackSessions = new Map();
let currentPlayback;
let uiToken = '';
let browser, adminContext, consumerContext, adminPage, page;

function owned(path) {
  const suffix = relative(root, path);
  assert(suffix !== '..' && !suffix.startsWith('../') && !isAbsolute(suffix), 'An acceptance path escaped its owned root.');
  return path;
}
async function directory(path, writable = false) {
  owned(path);
  if (path !== root) {
    const parent = await lstat(dirname(path));
    assert(parent.isDirectory() && !parent.isSymbolicLink());
  }
  await mkdir(path).catch(error => { if (error.code !== 'EEXIST') throw error; });
  const entry = await lstat(path);
  assert(entry.isDirectory() && !entry.isSymbolicLink());
  assert.equal(await realpath(path), path);
  if (writable) { await chmod(path, 0o755); await chown(path, 10001, 10001); }
}
async function ownedDirectory(path, writable = false) {
  const existing = await lstat(path).catch(error => { if (error.code === 'ENOENT') return undefined; throw error; });
  if (existing) { assert(existing.isDirectory() && !existing.isSymbolicLink()); assert.equal((await readFile(resolve(path, '.owner'), 'utf8')).trim(), format); }
  await directory(path, writable);
  if (!existing) await writeFile(resolve(path, '.owner'), format + '\n', { flag: 'wx' });
}
function safe(value) {
  let text = String(value);
  for (const secret of [config.password, config.token, csrf, uiToken].filter(Boolean)) text = text.replaceAll(secret, '[redacted]');
  return text.replace(/(https?:\/\/[^\s?'"<>]+)\?[^\s'"<>]+/g, '$1?[redacted]');
}
async function digest(path) { const hash = createHash('sha256'); for await (const chunk of createReadStream(path)) hash.update(chunk); return hash.digest('hex'); }
function fixturePath(path) {
  const suffix = relative(fixtureRoot, path);
  assert(!suffix.startsWith('..') && !isAbsolute(suffix));
  return '/fixture/' + suffix;
}
async function audio(path, duration, seed) {
  const sampleRate = 16000, frames = duration * sampleRate, bytes = frames * 2;
  const existing = await lstat(path).catch(error => { if (error.code === 'ENOENT') return undefined; throw error; });
  if (existing) { assert(existing.isFile() && !existing.isSymbolicLink() && existing.size === bytes + 44); return; }
  const file = await open(path, 'wx', 0o644);
  try {
    const header = Buffer.alloc(44);
    header.write('RIFF'); header.writeUInt32LE(bytes + 36, 4); header.write('WAVEfmt ', 8); header.writeUInt32LE(16, 16);
    header.writeUInt16LE(1, 20); header.writeUInt16LE(1, 22); header.writeUInt32LE(sampleRate, 24); header.writeUInt32LE(sampleRate * 2, 28);
    header.writeUInt16LE(2, 32); header.writeUInt16LE(16, 34); header.write('data', 36); header.writeUInt32LE(bytes, 40);
    await file.write(header);
    const tune = [0, 4, 7, 12, 9, 5, 2, 7, 4, 11, 7, 2, 0, 5, 9, 14, 12, 7, 3, 10, 5, 2, 9, 7];
    const frequencies = new Map();
    const note = midi => { if (!frequencies.has(midi)) frequencies.set(midi, 440 * 2 ** ((midi - 69) / 12)); return frequencies.get(midi); };
    for (let second = 0; second < duration; second++) {
      const block = Buffer.alloc(sampleRate * 2);
      for (let sample = 0; sample < sampleRate; sample++) {
        const time = second + sample / sampleRate;
        const ending = time >= duration - 60;
        const local = ending ? time - (duration - 60) : time;
        const beat = Math.floor(local * 2), fraction = local * 2 - beat;
        // Avalanche each beat with an independent body seed. The common ending
        // uses its own seed and a non-periodic melody so a repeated short riff
        // cannot accidentally establish a different absolute source offset.
        let hash = (beat ^ Math.imul(ending ? 457 : seed, 0x9e3779b9)) >>> 0;
        hash = Math.imul(hash ^ (hash >>> 16), 0x85ebca6b) >>> 0;
        hash = Math.imul(hash ^ (hash >>> 13), 0xc2b2ae35) >>> 0;
        hash = (hash ^ (hash >>> 16)) >>> 0;
        const midi = ending ? 48 + tune[hash % tune.length] : 38 + hash % 39;
        const rootNote = note(midi), third = note(midi + (ending ? 4 : 3)), fifth = note(midi + 7);
        const envelope = Math.min(1, fraction * 30) * Math.min(1, (1 - fraction) * 12);
        const value = envelope * (.15 * Math.sin(2 * Math.PI * rootNote * local) + .1 * Math.sin(2 * Math.PI * third * local)
          + .08 * Math.sin(2 * Math.PI * fifth * local) + .04 * Math.sin(2 * Math.PI * rootNote * 2 * local));
        block.writeInt16LE(Math.round(Math.max(-1, Math.min(1, value)) * 32767), sample * 2);
      }
      await file.write(block);
    }
  } finally { await file.close(); }
}
async function generate(name, args) {
  const path = paths[name];
  const present = await lstat(path).catch(error => { if (error.code === 'ENOENT') return undefined; throw error; });
  if (present) { assert(present.isFile() && !present.isSymbolicLink()); return; }
  execFileSync('docker', ['run', '--rm', '--network', 'none', '--user', '10001:10001', '--cpus', '1', '--memory', '384m', '--pids-limit', '64',
    '-v', `${fixtureRoot}:/fixture`, '-v', `${inputRoot}:/inputs:ro`, '--entrypoint', '/opt/ffmpeg/9.0.1/bin/ffmpeg', image,
    '-hide_banner', '-nostdin', '-loglevel', 'error', '-n', '-filter_threads', '1', ...args, '-c:v', 'libx264', '-preset', 'veryfast', '-crf', '24',
    '-pix_fmt', 'yuv420p', '-g', '2', '-keyint_min', '2', '-sc_threshold', '0', '-threads', '1', '-movflags', '+faststart', fixturePath(path)],
  { stdio: ['ignore', 'ignore', 'pipe'], timeout: 240_000, maxBuffer: 4 << 20 });
  assert.equal(await realpath(path), path);
  console.log(JSON.stringify({ fixture: name, generated: true, bytes: (await stat(path)).size }));
}
async function fixtures() {
  assert.equal(await realpath(mediaRoot), mediaRoot);
  await ownedDirectory(inputRoot);
  await ownedDirectory(fixtureRoot, true);
  for (const folder of ['Movies', 'Shows', `Movies/${labels.chapter} (2026)`, `Movies/${labels.visual} (2026)`, `Movies/${labels.negative} (2026)`, `Shows/${labels.series}`, `Shows/${labels.series}/Season 01`]) await directory(resolve(fixtureRoot, folder), true);
  const metadata = ';FFMETADATA1\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=60000\ntitle=Story\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=60000\nEND=90000\ntitle=Credits\n';
  const metadataPath = resolve(inputRoot, 'chapter.ffmetadata');
  const previous = await readFile(metadataPath, 'utf8').catch(error => { if (error.code === 'ENOENT') return undefined; throw error; });
  if (previous !== undefined) assert.equal(previous, metadata); else await writeFile(metadataPath, metadata, { flag: 'wx' });
  const color = 'color=c=cyan:s=160x90:r=2';
  await generate('chapter', ['-f', 'lavfi', '-i', color + ':d=90', '-i', '/inputs/chapter.ffmetadata', '-map', '0:v', '-map_metadata', '1', '-map_chapters', '1', '-an']);
  await generate('visual', ['-f', 'lavfi', '-i', color + ':d=60', '-f', 'lavfi', '-i', 'color=c=black:s=160x90:r=2:d=30',
    '-filter_complex_threads', '1', '-filter_complex', '[1:v]drawbox=x=40:y=26:w=80:h=2:color=white:t=fill,drawbox=x=48:y=36:w=64:h=2:color=white:t=fill,drawbox=x=44:y=46:w=72:h=2:color=white:t=fill,drawbox=x=52:y=56:w=56:h=2:color=white:t=fill[credits];[0:v][credits]concat=n=2:v=1:a=0[out]', '-map', '[out]', '-an']);
  await generate('negative', ['-f', 'lavfi', '-i', color + ':d=90', '-an']);
  for (const [name, duration, seed] of [['first', 600, 17], ['second', 612, 83]]) {
    await audio(resolve(inputRoot, `${name}.wav`), duration, seed);
    await generate(name, ['-f', 'lavfi', '-i', color + `:d=${duration}`, '-i', `/inputs/${name}.wav`, '-map', '0:v', '-map', '1:a', '-c:a', 'aac', '-b:a', '96k', '-ar', '16000', '-ac', '2', '-t', String(duration)]);
  }
  const files = [];
  for (const [name, path] of Object.entries(paths)) files.push({ name, relative: relative(fixtureRoot, path), bytes: (await stat(path)).size, sha256: await digest(path) });
  assert.notEqual(files.find(file => file.name === 'first').sha256, files.find(file => file.name === 'second').sha256);
  return files;
}
async function request(context, method, path, body, token = config.token) {
  const native = path.startsWith('/admin/');
  const response = await context.request.fetch(config.baseURL + path, { method, data: body,
    headers: native ? { Origin: config.baseURL, 'X-CSRF-Token': csrf } : { 'X-Emby-Token': token } });
  assert(response.ok(), `${method} ${path.split('?')[0]} returned ${response.status()}.`);
  if (method === 'POST' && path === '/admin/v1/media-analysis/runs') assert.equal(response.status(), 202);
  const text = await response.text();
  return text ? JSON.parse(text) : undefined;
}
const admin = (method, path, body) => request(adminContext, method, path, body);
const api = path => request(consumerContext, 'GET', path);
const credits = id => admin('GET', `/admin/v1/items/${id}/credits`);
const item = id => api(`/emby/Users/${config.userId}/Items/${id}?Fields=MediaSources,MediaStreams,Chapters`);
async function waitFor(check, message, timeout = 180_000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) { const value = await check(); if (value) return value; await new Promise(resolve => setTimeout(resolve, 250)); }
  throw new Error(message);
}
function noMediaProcess() { return !execFileSync('docker', ['top', container, '-eo', 'pid,args'], { encoding: 'utf8' }).split('\n').some(line => /\/proc\/self\/fd\/|\bffmpeg\b|\bffprobe\b/.test(line)); }
async function idle() {
  await waitFor(async () => { const value = (await admin('GET', `/admin/v1/tasks/${taskId}`)).Task.CurrentRun; return !value || !active.has(value.State); }, 'The credits task did not become idle.');
  await waitFor(noMediaProcess, 'A media helper remained after credits analysis.', 30_000);
}
async function scan(library) {
  const receipt = await admin('POST', `/admin/v1/libraries/${library.Id}/scan`, { ForceProbe: false });
  await waitFor(async () => {
    const job = (await admin('GET', '/admin/v1/jobs')).Items.find(value => value.Id === receipt.Job.Id);
    assert(!['failed', 'cancelled', 'canceled'].includes(job?.Status), 'The isolated fixture scan failed.');
    return job && ['completed', 'succeeded'].includes(job.Status);
  }, 'The isolated fixture scan did not finish.');
}
async function library(name, type, path) {
  const saved = (await admin('GET', '/admin/v1/libraries')).Items.find(value => value.Paths?.length === 1 && value.Paths[0] === path);
  if (saved) { assert.equal(saved.Name, name); return saved; }
  const value = (await admin('POST', '/admin/v1/libraries', { Name: name, CollectionType: type, Paths: [path], Scan: false })).Library;
  created.push({ Id: value.Id, Name: value.Name });
  return value;
}
async function enabled(library, value) {
  const current = (await admin('GET', `/admin/v1/libraries/${library.Id}`)).Library;
  return admin('PATCH', `/admin/v1/libraries/${library.Id}`, { Revision: current.Revision, LibraryOptions: { EnableCreditsDetection: value } });
}
async function analyze(libraries, items = [], force = true) {
  await idle();
  const input = { Kind: 'credits', RequestId: randomUUID(), LibraryIds: libraries.map(value => value.Id), ItemIds: items.map(value => value.Id), Force: force };
  let receipt;
  for (let attempt = 0; attempt < 3; attempt++) {
    const response = await adminContext.request.post(config.baseURL + '/admin/v1/media-analysis/runs', { data: input, headers: { Origin: config.baseURL, 'X-CSRF-Token': csrf } });
    const value = await response.json();
    if (response.status() === 409 && value.Error?.Code === 'active_run_conflict') {
      // A just-dispatched automatic cohort can win between idle inspection and
      // admission. Its explicit rejection permits retrying the same request ID.
      await idle();
      continue;
    }
    assert.equal(response.status(), 202, `Credits admission failed: ${value.Error?.Code || response.status()}`);
    receipt = value; break;
  }
  assert(receipt, 'Automatic analysis repeatedly raced the explicit credits admission.');
  assert.deepEqual(Object.keys(receipt).sort(), ['Admitted', 'RunId', 'TaskId']);
  assert.equal(receipt.TaskId, taskId);
  pendingRun = receipt.RunId;
  const completed = await waitFor(async () => { const result = (await admin('GET', `/admin/v1/task-runs/${receipt.RunId}`)).Run; return !active.has(result.State) && result; }, 'Credits analysis did not reach a terminal state.');
  runs.push({ input, receipt, state: completed.State });
  assert.equal(completed.State, 'completed');
  pendingRun = undefined;
  await idle();
  return receipt;
}
function intervals(detail) { return detail.Detected.map(value => ({ StartPositionTicks: value.StartTicks, EndPositionTicks: value.EndTicks, Source: value.Source })); }
async function projection(target, expected) {
  const value = await item(target.Id);
  assert.deepEqual(value.GobyCreditsIntervals, expected);
  assert.deepEqual(value.MediaSources[0].GobyCreditsIntervals, expected);
  const starts = (value.Chapters || []).filter(chapter => chapter.MarkerType === 'CreditsStart');
  assert.equal(starts.length, expected.length ? 1 : 0);
  if (expected.length) assert.equal(starts[0].StartPositionTicks, expected[0].StartPositionTicks);
  assert(!(value.Chapters || []).some(chapter => chapter.MarkerType === 'CreditsEnd'));
  const playback = await request(consumerContext, 'POST', `/emby/Items/${target.Id}/PlaybackInfo`, { UserId: config.userId, IsPlayback: false, EnableTranscoding: false, EnableDirectPlay: true, SubtitleStreamIndex: -1 });
  playbackSessions.set(playback.PlaySessionId, { ItemId: target.Id, MediaSourceId: playback.MediaSources[0].Id });
  assert.deepEqual(playback.MediaSources[0].GobyCreditsIntervals, expected);
  assert(!(playback.MediaSources[0].Chapters || []).some(chapter => chapter.MarkerType === 'CreditsEnd'));
  await request(consumerContext, 'POST', '/emby/Sessions/Playing/Stopped', { ...playbackSessions.get(playback.PlaySessionId), PlaySessionId: playback.PlaySessionId, PositionTicks: 0 });
  playbackSessions.delete(playback.PlaySessionId);
  return { itemId: target.Id, intervals: expected, standardCreditsStartCount: starts.length };
}
async function setManual(target, point) {
  const current = await credits(target.Id);
  return admin(point ? 'PUT' : 'DELETE', `/admin/v1/items/${target.Id}/credits`, { Revision: current.Revision, SourceRevision: current.SourceRevision, ...(point ? point : {}) });
}
async function rememberState(target) { if (!userStates.has(target.Id)) userStates.set(target.Id, (await item(target.Id)).UserData); }
async function restoreState(id, value) {
  const fields = ['PlaybackPositionTicks', 'PlayCount', 'IsFavorite', 'Played', 'HideFromResume', 'LastPlayedDate', 'Rating', 'Likes'];
  const patch = Object.fromEntries(fields.map(name => [name, value?.[name] ?? (['LastPlayedDate', 'Rating', 'Likes'].includes(name) ? null : ['IsFavorite', 'Played', 'HideFromResume'].includes(name) ? false : 0)]));
  await request(consumerContext, 'POST', `/emby/Users/${config.userId}/Items/${id}/UserData`, patch);
  assert.deepEqual((await item(id)).UserData, value);
}
async function closePlayback() {
  if (!currentPlayback) return;
  const ended = page.waitForResponse(response => /\/Sessions\/Playing\/Stopped$/.test(new URL(response.url()).pathname), { timeout: 15_000 });
  await page.goto(`${config.baseURL}/#/settings`);
  assert((await ended).ok());
  currentPlayback = undefined;
  await waitFor(noMediaProcess, 'Playback left an encoder behind.', 30_000);
}
async function shot(target, name) { await target.screenshot({ path: resolve(output, `${name}.png`), fullPage: true }); }
async function persist() { await writeFile(resolve(output, 'results.json'), JSON.stringify({ format, results, failures, optionalResponses, requests, runs, created, negotiations, detections }, null, 2)); }
async function phase(name, operation) { const value = await operation(); results.push({ name, passed: true, ...(value || {}) }); await persist(); console.log(JSON.stringify({ phase: name, passed: true })); }
async function touch(path) {
  assert(Object.values(paths).includes(path)); assert.equal(await realpath(path), path);
  const value = await stat(path, { bigint: true });
  if (!touched.has(path)) touched.set(path, { atime: value.atimeNs, mtime: value.mtimeNs });
  execFileSync('python3', ['-c', 'import os,sys;os.utime(sys.argv[1],ns=(int(sys.argv[2]),int(sys.argv[3])))', path, String(value.atimeNs), String(value.mtimeNs + 1_000_000_000n)]);
}
async function restoreTimes() {
  for (const [path, value] of touched) {
    assert.equal(await realpath(path), path);
    execFileSync('python3', ['-c', 'import os,sys;os.utime(sys.argv[1],ns=(int(sys.argv[2]),int(sys.argv[3])))', path, String(value.atime), String(value.mtime)]);
  }
}
async function showEditor(target, library) {
  await adminPage.goto(`${config.baseURL}/admin/media/libraries/${library.Id}/items`);
  await adminPage.getByRole('button', { name: `More actions for ${target.Name}`, exact: true }).click();
  await adminPage.getByRole('menuitem', { name: '片尾标记', exact: true }).click();
  await adminPage.getByRole('region', { name: '自动片尾识别结果', exact: true }).waitFor();
}

try {
  assert.equal(execFileSync('docker', ['inspect', '--format', '{{index .Config.Labels "com.docker.compose.project"}}', container], { encoding: 'utf8' }).trim(), 'goby-player-165c');
  await directory(output);
  for (const entry of await readdir(output)) assert(!(await lstat(resolve(output, entry))).isSymbolicLink());
  browser = await chromium.launch({ headless: true, args: ['--no-sandbox'] });
  adminContext = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN', reducedMotion: 'reduce' });
  consumerContext = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: 'zh-CN', reducedMotion: 'reduce' });
  adminPage = await adminContext.newPage(); page = await consumerContext.newPage();
  for (const [target, surface] of [[adminPage, 'administrator'], [page, 'consumer']]) {
    target.on('pageerror', error => failures.push({ surface, kind: 'page', message: safe(error.message) }));
    target.on('request', value => requests.push({ surface, method: value.method(), path: new URL(value.url()).pathname }));
    target.on('response', async response => {
      const path = new URL(response.url()).pathname;
      if (response.status() === 404 && /\/ThumbnailSet$/.test(path)) optionalResponses.push({ surface, path, status: 404 });
      else if (response.status() >= 400 && /^\/(admin\/v1|emby)\//.test(path)) failures.push({ surface, kind: 'http', path, status: response.status() });
      if (surface === 'consumer' && path.endsWith('/AuthenticateByName') && response.ok()) uiToken = (await response.json()).AccessToken;
      if (surface === 'consumer' && path.endsWith('/PlaybackInfo') && response.ok()) { const value = await response.json(); negotiations.push({ itemId: path.split('/')[3], source: value.MediaSources[0]?.Id, intervals: value.MediaSources[0]?.GobyCreditsIntervals }); }
    });
  }
  csrf = (await admin('POST', '/admin/v1/session', { Name: config.username, Password: config.password })).CSRFToken;
  const runtime = (await admin('GET', '/admin/v1/media-analysis')).Runtime;
  assert.equal(runtime.CreditsAvailable, true, `Credits detection is unavailable: ${(runtime.CreditsReasons || []).join(',')}`);
  taskId = (await admin('GET', '/admin/v1/tasks')).Items.find(value => value.Key === 'media.credits_analysis')?.Id;
  assert(taskId);
  oldCatalog = (await api(`/emby/Users/${config.userId}/Items?Recursive=true&IncludeItemTypes=Movie,Episode&Limit=1000`)).Items.map(value => ({ Id: value.Id, UserData: value.UserData }));
  const files = await fixtures();
  movieLibrary = await library('Player credits movies', 'movies', '/media/CreditsAcceptance/Movies');
  televisionLibrary = await library('Player credits audio', 'tvshows', '/media/CreditsAcceptance/Shows');
  await scan(movieLibrary); await scan(televisionLibrary);
  const movies = (await api(`/emby/Items?ParentId=${movieLibrary.Id}&Recursive=true&IncludeItemTypes=Movie&Fields=MediaSources,MediaStreams,Chapters`)).Items;
  chapter = movies.find(value => value.Name === labels.chapter); visual = movies.find(value => value.Name === labels.visual); negative = movies.find(value => value.Name === labels.negative);
  assert(chapter && visual && negative && movies.length === 3);
  const shows = (await api(`/emby/Items?ParentId=${televisionLibrary.Id}&Recursive=true&IncludeItemTypes=Series,Episode&Fields=MediaSources,MediaStreams,Chapters`)).Items;
  series = shows.find(value => value.Type === 'Series'); episodes = shows.filter(value => value.Type === 'Episode').sort((a, b) => a.IndexNumber - b.IndexNumber);
  assert(series && episodes.length === 2);
  assert(episodes[0].RunTimeTicks > 599 * ticks && episodes[1].RunTimeTicks > 611 * ticks);
  for (const target of [chapter, visual, negative, ...episodes]) await rememberState(target);
  originalManual = (await credits(chapter.Id)).Override;

  await phase('default disabled and read-only endpoints do not create credits analysis', async () => {
    assert.equal(Boolean(movieLibrary.LibraryOptions.EnableCreditsDetection), false); assert.equal(Boolean(televisionLibrary.LibraryOptions.EnableCreditsDetection), false);
    await idle();
    const before = (await admin('GET', `/admin/v1/tasks/${taskId}/runs?Limit=50&StartIndex=0`)).TotalRecordCount;
    const states = [];
    for (const target of [chapter, visual, negative, ...episodes]) {
      const value = await credits(target.Id); states.push({ Id: target.Id, status: value.DetectedStatus, revision: value.DetectedRevision }); assert.equal(value.Effective, null);
      const projected = await item(target.Id); assert.deepEqual(projected.GobyCreditsIntervals, []); assert.deepEqual(projected.MediaSources[0].GobyCreditsIntervals, []);
      assert(!(projected.Chapters || []).some(chapter => chapter.MarkerType === 'CreditsStart' || chapter.MarkerType === 'CreditsEnd'));
    }
    assert.equal((await admin('GET', `/admin/v1/tasks/${taskId}/runs?Limit=50&StartIndex=0`)).TotalRecordCount, before);
    assert(noMediaProcess());
    await adminPage.goto(`${config.baseURL}/admin/media/libraries`);
    await adminPage.getByRole('button', { name: `More actions for ${movieLibrary.Name}`, exact: true }).click();
    await adminPage.getByRole('menuitem', { name: 'Edit library', exact: true }).click();
    const editor = adminPage.getByRole('dialog', { name: 'Edit library', exact: true });
    assert.equal(await editor.getByRole('checkbox', { name: '自动识别片尾', exact: true }).isChecked(), false);
    await shot(adminPage, 'credits-default-disabled');
    await editor.getByRole('button', { name: 'Cancel', exact: true }).click();
    return { automaticEnabled: false, runsCreatedByReads: 0, states, files };
  });

  await phase('the pinned credits pass recognizes chapter visual and independent audio endings', async () => {
    await enabled(movieLibrary, true); await enabled(televisionLibrary, true);
    await new Promise(resolve => setTimeout(resolve, 600)); await idle();
    const automaticRuns = (await admin('GET', `/admin/v1/tasks/${taskId}/runs?Limit=50&StartIndex=0`)).Items.map(value => ({ Id: value.Id, Source: value.Source, State: value.State }));
    await analyze([movieLibrary, televisionLibrary]);
    const found = [];
    for (const target of [chapter, visual, ...episodes]) {
      const value = await credits(target.Id);
      detections.push({ name: target.Name, itemId: target.Id, detail: value });
      assert.equal(value.DetectedStatus, 'qualified'); assert.equal(value.DetectedStale, false); assert(value.Detected.length > 0);
      assert.equal(value.Automatic?.Provenance, 'Detected'); assert.equal(value.Effective?.StartTicks, value.Detected[0].StartTicks);
      const tail = target.Id === chapter.Id || target.Id === visual.Id ? 60 : target.IndexNumber === 1 ? 540 : 552;
      assert(Math.abs(value.Detected[0].StartTicks / ticks - tail) < 8, 'The detected start is outside the independently constructed fixture ending.');
      const expected = target.Id === chapter.Id ? 'Chapter' : target.Id === visual.Id ? 'BlackFrame' : 'Chromaprint';
      assert(value.Detected.every(segment => segment.Source === expected), `Expected the isolated ${expected} evidence path.`);
      found.push({ name: target.Name, method: expected, detail: value, wire: await projection(target, intervals(value)) });
    }
    const absent = await credits(negative.Id);
    assert.equal(absent.DetectedStatus, 'no_result'); assert.deepEqual(absent.Detected, []); assert.equal(absent.Effective, null); await projection(negative, []);
    await showEditor(visual, movieLibrary); await shot(adminPage, 'visual-detection');
    return { found, negative: { status: absent.DetectedStatus, intervals: absent.Detected }, rawFingerprintsWindowSeconds: 450, runsAfterOptIn: automaticRuns };
  });

  await phase('the real player shows its next episode cue only inside the detected ending', async () => {
    const target = episodes[0], value = await credits(target.Id), segment = value.Detected[0];
    await page.goto(config.baseURL); await page.getByLabel('用户名', { exact: true }).fill(config.username); await page.getByLabel('密码', { exact: true }).fill(config.password);
    await page.getByRole('button', { name: '登录', exact: true }).click(); await page.getByRole('navigation', { name: '主导航' }).waitFor();
    await page.goto(`${config.baseURL}/#/player/${target.Id}`); currentPlayback = target.Id;
    await page.waitForFunction(() => { const video = document.querySelector('.player-video'); return video && video.readyState >= 2 && !video.paused && video.currentTime > 0; }, undefined, { timeout: 60_000 });
    const video = page.locator('.player-video');
    await video.evaluate((element, at) => { element.pause(); element.currentTime = at; }, Math.max(1, segment.StartTicks / ticks - 20));
    await waitFor(() => page.locator('.player-next').count().then(count => count === 0), 'The next episode cue appeared before credits.');
    await video.evaluate((element, at) => { element.currentTime = at; }, segment.StartTicks / ticks + 3);
    await page.locator('.player-next').waitFor();
    await shot(page, 'detected-credits-player-cue');
    await video.evaluate((element, at) => { element.currentTime = at; }, Math.max(1, segment.StartTicks / ticks - 20));
    await waitFor(() => page.locator('.player-next').count().then(count => count === 0), 'Seeking out of credits left the next episode cue visible.');
    await closePlayback();
    return { cueInsideInterval: true, hiddenBeforeAndAfterSeek: true, interval: segment, multiIntervalGap: 'Covered by the separate browser interval suite; this isolated audio fixture has one ending.' };
  });

  await phase('manual override takes priority and reset restores automatic intervals', async () => {
    const before = await credits(chapter.Id);
    const manual = await setManual(chapter, { StartTicks: 70 * ticks, Provenance: 'Manual' });
    assert.equal(manual.Effective.Provenance, 'Manual'); assert.deepEqual(manual.Detected, before.Detected);
    await projection(chapter, [{ StartPositionTicks: 70 * ticks, EndPositionTicks: chapter.RunTimeTicks, Source: 'Manual' }]);
    const reset = await setManual(chapter, null); assert.equal(reset.Override, null); assert.equal(reset.Effective.Provenance, 'Detected');
    await projection(chapter, intervals(reset)); await showEditor(chapter, movieLibrary); await shot(adminPage, 'automatic-after-manual-reset');
    return { manualStartSeconds: 70, detectedEvidencePreserved: true, resetRestoredAutomatic: true };
  });

  await phase('disabling a library retracts automatic markers without deleting detection evidence', async () => {
    const before = await credits(chapter.Id); await enabled(movieLibrary, false);
    const disabled = await credits(chapter.Id); assert.deepEqual(disabled.Detected, before.Detected); assert.equal(disabled.DetectedRevision, before.DetectedRevision); assert.equal(disabled.Effective, null); assert.equal(disabled.DetectedStale, false);
    await projection(chapter, []); await showEditor(chapter, movieLibrary); await shot(adminPage, 'disabled-retains-evidence');
    await enabled(movieLibrary, true); await analyze([movieLibrary]);
    return { retainedRevision: disabled.DetectedRevision, evidenceRetained: true, publicIntervalsWhileDisabled: [] };
  });

  await phase('changed target and independently matched support invalidate old automatic results', async () => {
    await adminPage.goto('about:blank');
    const beforeChapter = await credits(chapter.Id), beforeAudio = await credits(episodes[0].Id);
    await touch(paths.chapter);
    assert.deepEqual((await item(chapter.Id)).GobyCreditsIntervals, []);
    await touch(paths.second);
    const supported = await credits(episodes[0].Id); assert.equal(supported.DetectedStale, true); assert.equal(supported.Effective, null); assert.deepEqual(supported.Detected, beforeAudio.Detected); await projection(episodes[0], []);
    // Stop automatic repair before scanning so the stale evidence can be
    // inspected deterministically without replacing the task's global schedule.
    await enabled(movieLibrary, false); await enabled(televisionLibrary, false);
    await scan(movieLibrary); await scan(televisionLibrary);
    const changed = await credits(chapter.Id); assert.equal(changed.DetectedStale, true); assert.equal(changed.Effective, null); assert.deepEqual(changed.Detected, beforeChapter.Detected); await projection(chapter, []);
    return { targetReason: changed.DetectedReason, supportReason: supported.DetectedReason, targetHiddenBeforeRescan: true, supportHiddenBeforeRescan: true, targetEvidencePreserved: true, supportEvidencePreserved: true };
  });

  await phase('restored timestamps and explicit reanalysis produce current source-bound results', async () => {
    await restoreTimes(); await scan(movieLibrary); await scan(televisionLibrary);
    await enabled(movieLibrary, true); await enabled(televisionLibrary, true); await new Promise(resolve => setTimeout(resolve, 600)); await idle();
    await analyze([movieLibrary, televisionLibrary]);
    const outcomes = [];
    for (const target of [chapter, visual, ...episodes]) { const value = await credits(target.Id); assert.equal(value.DetectedStale, false); assert.equal(value.DetectedStatus, 'qualified'); assert.equal(value.Effective.Provenance, 'Detected'); outcomes.push(await projection(target, intervals(value))); }
    touched.clear();
    await showEditor(chapter, movieLibrary); await shot(adminPage, 'current-after-reanalysis');
    return { outcomes, sourceMtimesRestored: true, sourceCtimeRestorable: false };
  });
  assert.deepEqual(failures, []);
} catch (error) {
  if (adminPage) await shot(adminPage, 'administrator-failure').catch(() => undefined);
  if (page) await shot(page, 'consumer-failure').catch(() => undefined);
  console.error(safe(error.message)); process.exitCode = 1;
  await writeFile(resolve(output, 'failure.json'), JSON.stringify({ message: safe(error.message), results, failures, optionalResponses, runs, detections }, null, 2)).catch(() => undefined);
} finally {
  const cleanup = async operation => { try { await operation(); } catch (error) { failures.push({ kind: 'cleanup', message: safe(error.message) }); console.error(safe(error.message)); process.exitCode = 1; } };
  if (currentPlayback && page) await cleanup(closePlayback);
  if (adminPage) await adminPage.goto('about:blank').catch(() => undefined);
  if (page) await page.goto('about:blank').catch(() => undefined);
  if (pendingRun && csrf) await cleanup(async () => { if (active.has((await admin('GET', `/admin/v1/task-runs/${pendingRun}`)).Run.State)) await admin('POST', `/admin/v1/task-runs/${pendingRun}/cancel`, {}); });
  if (taskId && csrf) await cleanup(idle);
  if (touched.size && movieLibrary && televisionLibrary && csrf) await cleanup(async () => { await restoreTimes(); await scan(movieLibrary); await scan(televisionLibrary); await enabled(movieLibrary, true); await enabled(televisionLibrary, true); await new Promise(resolve => setTimeout(resolve, 600)); await analyze([movieLibrary, televisionLibrary]); touched.clear(); });
  if (chapter && originalManual !== undefined && csrf) await cleanup(async () => { const current = await credits(chapter.Id); if (JSON.stringify(current.Override) !== JSON.stringify(originalManual)) await setManual(chapter, originalManual); });
  for (const value of [movieLibrary, televisionLibrary].filter(Boolean)) if (csrf) await cleanup(() => enabled(value, false));
  for (const [sessionId, source] of playbackSessions) await cleanup(() => request(consumerContext, 'POST', '/emby/Sessions/Playing/Stopped', { ...source, PlaySessionId: sessionId, PositionTicks: 0 }));
  for (const [id, value] of userStates) await cleanup(() => restoreState(id, value));
  if (oldCatalog) await cleanup(async () => { for (const before of oldCatalog) assert.deepEqual((await item(before.Id)).UserData, before.UserData, 'Credits acceptance changed previous user playback state.'); });
  if (taskId && csrf) await cleanup(idle);
  await persist().catch(() => undefined);
  await adminContext?.close(); await consumerContext?.close(); await browser?.close();
  if (!process.exitCode) console.log(JSON.stringify({ complete: true, phases: results.length, realBackend: true, originalFixtures: true, isolatedLibrariesDisabled: true }));
}
