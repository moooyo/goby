import { request, chromium } from '@playwright/test';
import assert from 'node:assert/strict';
import { readFile, writeFile, realpath, stat, lstat, chmod } from 'node:fs/promises';
import { basename, dirname, resolve } from 'node:path';
import { createHash, randomUUID } from 'node:crypto';
import { execFileSync } from 'node:child_process';

assert.equal(process.platform, 'linux');
const root = await realpath('/opt/goby-test/player-live-20261004-165c/external-timeline-check/live');
const owner = 'goby-external-timeline-165c-20261005';
assert.equal((await readFile(resolve(root, '.owner'), 'utf8')).trim(), owner);
const privatePath = resolve(root, 'live.private.json');
const config = JSON.parse(await readFile(privatePath, 'utf8'));
assert.equal(config.container, 'goby-external-timeline-165c-live-backend');
assert.equal(execFileSync('docker', ['inspect', '--format={{index .Config.Labels "goby.owner"}}', config.container], { encoding: 'utf8' }).trim(), owner);
const oracle = JSON.parse(await readFile(resolve(root, 'artifacts/oracle.json'), 'utf8'));
const output = resolve(root, 'artifacts');
const source = resolve(root, 'media/ExternalBitmap/External Bitmap (2026)/External Bitmap.mp4');
const subtitle = resolve(dirname(source), 'External Bitmap.sub');
const sidecar = resolve(dirname(source), 'backdrops/goby-subtitle-timelines', createHash('sha256').update(basename(source)).digest('hex'));
const active = new Set(['pending', 'running', 'stopping']);
const evidence = { format: 'goby-external-bitmap-live-v1', phases: [], runs: [], failures: [] };
const administrator = await request.newContext();
const consumer = await request.newContext();
let csrf = '', item, library, taskId, tracks, browser;
const secretValues = () => [config.password, config.token, config.setupToken, config.databasePassword, config.restrictedPassword, csrf].filter(Boolean);
function safe(error) {
  let value = String(error?.stack || error);
  for (const secret of secretValues()) value = value.replaceAll(secret, '[redacted]');
  return value;
}
async function save() { await writeFile(resolve(output, 'results.json'), JSON.stringify(evidence, null, 2)); }
async function phase(name, action) {
  const result = await action(); evidence.phases.push({ name, passed: true, ...result }); await save();
  console.log(JSON.stringify({ phase: name, passed: true }));
}
async function json(context, method, path, body, expected, extraHeaders = {}) {
  const response = await context.fetch(config.baseURL + path, { method, data: body, headers: {
    ...(path.startsWith('/admin/') ? { Origin: config.playerURL, 'X-CSRF-Token': csrf } : {
      'X-Emby-Token': config.token || '', 'X-Emby-Authorization': 'Emby Client="External bitmap acceptance", Device="Remote browser", DeviceId="external-bitmap-live-165c", Version="1.0"',
    }), ...extraHeaders } });
  assert(expected === undefined ? response.ok() : response.status() === expected, `${method} ${path.split('?')[0]} returned ${response.status()}: ${(await response.text()).slice(0, 500)}`);
  const text = await response.text(); return text ? JSON.parse(text) : undefined;
}
const admin = (method, path, body, expected) => json(administrator, method, path, body, expected);
const api = path => json(consumer, 'GET', path);
const detail = () => admin('GET', `/admin/v1/items/${item.Id}/subtitle-timelines`);
const descriptor = () => api(`/emby/Items/${item.Id}/SubtitleTimelines?UserId=${config.userId}`);
async function wait(check, message, timeout = 120_000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) { const result = await check(); if (result) return result; await new Promise(done => setTimeout(done, 150)); }
  throw new Error(message);
}
async function idle() {
  await wait(async () => { const run = (await admin('GET', `/admin/v1/tasks/${taskId}`)).Task.CurrentRun; return !run || !active.has(run.State); }, 'The analysis task did not become idle.');
}
async function scan() {
  const receipt = await admin('POST', `/admin/v1/libraries/${library.Id}/scan`, { ForceProbe: false });
  await wait(async () => {
    const job = (await admin('GET', '/admin/v1/jobs')).Items.find(value => value.Id === receipt.Job.Id);
    assert(!['failed', 'cancelled', 'canceled'].includes(job?.Status), 'The live library scan failed.');
    return job && ['completed', 'succeeded'].includes(job.Status);
  }, 'The live library scan did not finish.');
}
async function generate(force = false, expectedState = 'ready') {
  await idle(); const before = await detail();
  const input = { Kind: 'subtitle-timeline', RequestId: randomUUID(), LibraryIds: [], ItemIds: [item.Id], Force: force };
  const receipt = await admin('POST', '/admin/v1/media-analysis/runs', input, 202);
  assert.equal(receipt.TaskId, taskId); assert.equal(receipt.Queued, 1);
  await wait(async () => { const run = (await admin('GET', `/admin/v1/task-runs/${receipt.RunId}`)).Run; return !active.has(run.State) && run; }, 'The admitted task did not finish.');
  const expectedRevision = String(BigInt(before.RequestedRevision) + 1n);
  const completed = await wait(async () => {
    const value = await detail(); return value.RequestedRevision === expectedRevision && value.CompletedRevision === expectedRevision && !['pending', 'running'].includes(value.State) && value;
  }, 'The selected item revision did not finish.');
  assert.equal(completed.State, expectedState, JSON.stringify(completed));
  await idle();
  evidence.runs.push({ input, receipt, state: completed.State, errorCode: completed.ErrorCode, reused: completed.Reused, revision: completed.CompletedRevision });
  return completed;
}
async function disk() {
  const path = resolve(sidecar, 'manifest.json');
  if (!(await lstat(path).catch(error => { if (error.code === 'ENOENT') return null; throw error; }))) return null;
  assert.equal(await realpath(sidecar), sidecar);
  const manifestData = await readFile(path), manifest = JSON.parse(manifestData);
  assert.equal(manifest.SourceName, basename(source)); assert.equal(manifest.Format, 'goby-subtitle-timelines-v1');
  assert(/^gen-[a-f0-9]{32}\.gstl$/.test(manifest.Generation));
  const file = resolve(sidecar, manifest.Generation), data = await readFile(file);
  assert.equal(createHash('sha256').update(data).digest('hex'), manifest.SHA256);
  assert.equal(data.subarray(0, 4).toString(), 'GSTL'); assert.equal(data.readUInt16LE(4), 2); assert.equal(data.readUInt16LE(6), 0);
  assert.equal(Number(data.readBigInt64LE(40)), oracle.DurationTicks); assert.equal(data.readUInt16LE(48), 3);
  const actual = []; let offset = 50;
  for (let index = 0; index < 3; index++) {
    const StreamIndex = data.readInt32LE(offset), Codec = data.readUInt8(offset + 4) === 2 ? 'dvd_subtitle' : 'hdmv_pgs_subtitle';
    const warnings = data.readUInt8(offset + 5), count = data.readUInt32LE(offset + 6); offset += 10 + warnings;
    const Intervals = [];
    for (let i = 0; i < count; i++, offset += 16) Intervals.push({ StartTicks: Number(data.readBigInt64LE(offset)), EndTicks: Number(data.readBigInt64LE(offset + 8)) });
    actual.push({ StreamIndex, Codec, Intervals });
  }
  assert.equal(offset, data.length); assert.deepEqual(actual, tracks);
  return { generation: manifest.Generation, sha256: manifest.SHA256, size: data.length, mtimeNs: String((await stat(file, { bigint: true })).mtimeNs), manifestHash: createHash('sha256').update(manifestData).digest('hex') };
}
async function checkTracks(value) {
  assert.equal(value.Available, true); assert.equal(value.DurationTicks, oracle.DurationTicks);
  assert.deepEqual(value.Streams.map(track => ({ StreamIndex: track.StreamIndex, Codec: track.Codec, count: track.IntervalCount })),
    tracks.map(track => ({ StreamIndex: track.StreamIndex, Codec: track.Codec, count: track.Intervals.length })));
  const actual = [];
  for (const track of tracks) {
    const stream = value.Streams.find(entry => entry.StreamIndex === track.StreamIndex);
    const payload = await api(stream.Url);
    assert.deepEqual(payload, { MediaSourceId: value.MediaSourceId, SourceVersion: value.SourceVersion, StreamIndex: track.StreamIndex, DurationTicks: oracle.DurationTicks, Intervals: track.Intervals });
    actual.push(payload);
  }
  return actual;
}
async function history() {
  return (await admin('GET', `/admin/v1/tasks/${taskId}/runs?Limit=1&StartIndex=0`)).TotalRecordCount;
}
async function quietHistory() {
  let previous = -1, stableAt = Date.now();
  return wait(async () => {
    const current = await history();
    const run = (await admin('GET', `/admin/v1/tasks/${taskId}`)).Task.CurrentRun;
    if (current !== previous || run && active.has(run.State)) { previous = current; stableAt = Date.now(); }
    // Manual admission records a system event as well as starting its run.
    // Let that preexisting event retire before measuring browser side effects.
    return Date.now() - stableAt >= 2200 ? { count: current } : false;
  }, 'Prior manual work did not reach a stable task history.', 30_000);
}
async function image(name, expectedCount, viewport) {
  const page = await browser.newPage({ viewport, locale: 'zh-CN', reducedMotion: 'reduce' });
  const failures = [], requests = [];
  const before = (await quietHistory()).count;
  page.on('pageerror', error => failures.push(safe(error)));
  page.on('request', value => requests.push({ method: value.method(), path: new URL(value.url()).pathname }));
  await page.goto(config.playerURL);
  await page.getByLabel('用户名', { exact: true }).fill(config.username);
  await page.getByLabel('密码', { exact: true }).fill(config.password);
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await page.getByRole('navigation', { name: '主导航' }).waitFor();
  await page.goto(`${config.playerURL}/#/detail/${item.Id}`);
  await page.waitForFunction(() => {
    const stage = document.querySelector('.stage-deck');
    const until = Number(stage?.getAttribute('data-locked-until'));
    return stage?.querySelector('.deck-panel[aria-label="媒体信息"]') && until > 0 && until <= performance.now();
  });
  await page.getByRole('navigation', { name: '页面分页', exact: true }).getByRole('button', { name: '媒体信息', exact: true }).click();
  await page.waitForFunction(count => document.querySelectorAll('.deck-panel[data-active=true] .detail-subtitle-lane').length === count, expectedCount);
  await page.evaluate(() => document.fonts.ready);
  const visual = await page.locator('.deck-panel[data-active=true]').evaluate(panel => ({
    lanes: [...panel.querySelectorAll('.detail-subtitle-lane')].map(lane => ({ index: Number(lane.dataset.streamIndex), path: lane.querySelector('path')?.getAttribute('d') })),
    labels: [...panel.querySelectorAll('.detail-track-label.subtitle')].map(label => label.textContent),
    overflow: document.documentElement.scrollWidth > innerWidth,
  }));
  assert.equal(visual.overflow, false); assert.equal(visual.lanes.length, expectedCount);
  assert.deepEqual(visual.labels, expectedCount ? ['S1英语DVD', 'S2中文DVD', 'S3未知语言PGS'] : []);
  for (const lane of visual.lanes) {
    const expected = tracks.find(track => track.StreamIndex === lane.index);
    const commands = [...lane.path.matchAll(/M([\d.]+) 2H([\d.]+)V8H([\d.]+)Z/g)];
    assert.equal(commands.length, expected.Intervals.length);
    commands.forEach((command, index) => {
      assert(Math.abs(Number(command[1]) / 1000 * oracle.DurationTicks - expected.Intervals[index].StartTicks) < 1);
      assert(Math.abs(Number(command[2]) / 1000 * oracle.DurationTicks - expected.Intervals[index].EndTicks) < 1);
    });
  }
  await page.screenshot({ path: resolve(output, name + '.png'), fullPage: true });
  await page.close(); assert.deepEqual(failures, []);
  assert.equal(await history(), before);
  assert(!requests.some(value => /\/PlaybackInfo$|\/Sessions\/Playing|\/media-analysis\/runs$/.test(value.path)));
  return { ...visual, requestCount: requests.length, taskHistoryUnchanged: true, taskCountBefore: before,
    taskCountAfter: before, noPlaybackRequests: true, preflightStableMilliseconds: 2200 };
}

try {
  await wait(async () => {
    try { return (await consumer.get(config.baseURL + '/admin/v1/bootstrap')).ok(); } catch { return false; }
  }, 'The new isolated backend did not become ready.', 30_000);
  if (!config.bootstrapped) {
    const bootstrap = await admin('POST', '/admin/v1/bootstrap', { SetupToken: config.setupToken, Name: config.username, Password: config.password }, 201);
    config.userId = bootstrap.User.Id; config.bootstrapped = true;
    await writeFile(privatePath, JSON.stringify(config)); await chmod(privatePath, 0o600);
  }
  csrf = (await admin('POST', '/admin/v1/session', { Name: config.username, Password: config.password })).CSRFToken;
  const auth = await json(consumer, 'POST', '/emby/Users/AuthenticateByName', { Username: config.username, Pw: config.password });
  config.token = auth.AccessToken; config.userId = auth.User.Id;
  await writeFile(privatePath, JSON.stringify(config)); await chmod(privatePath, 0o600);
  const runtime = (await admin('GET', '/admin/v1/media-analysis')).Runtime;
  assert.equal(runtime.SubtitleTimelineAvailable, true, JSON.stringify(runtime));
  taskId = (await admin('GET', '/admin/v1/tasks')).Items.find(value => value.Key === 'media.subtitle_timeline_generation')?.Id; assert(taskId);
  library = (await admin('GET', '/admin/v1/libraries')).Items.find(value => value.Name === 'External bitmap acceptance');
  if (!library) library = (await admin('POST', '/admin/v1/libraries', { Name: 'External bitmap acceptance', CollectionType: 'movies', Paths: ['/media/ExternalBitmap'], Scan: false })).Library;
  assert.equal(library.LibraryOptions.EnableSubtitleTimelineGeneration, false);
  await scan();
  const catalog = await api(`/emby/Items?ParentId=${library.Id}&Recursive=true&IncludeItemTypes=Movie&Fields=MediaSources,MediaStreams`);
  assert.equal(catalog.Items.length, 1); item = catalog.Items[0]; assert.equal(item.RunTimeTicks, oracle.DurationTicks);
  const subtitles = item.MediaSources[0].MediaStreams.filter(value => value.Type === 'Subtitle');
  assert.equal(subtitles.length, 3); assert(subtitles.every(track => track.IsExternal && track.GobySubtitleTimelineOnly && !track.SupportsExternalStream));
  tracks = subtitles.map(value => {
    const expected = oracle.Sources.flatMap(source => source.Tracks).find(track => track.Codec === value.Codec && (track.Language === value.Language || !track.Language && !value.Language));
    assert(expected, JSON.stringify(value)); return { StreamIndex: value.Index, Codec: value.Codec, Intervals: expected.Intervals };
  }).sort((a, b) => a.StreamIndex - b.StreamIndex);
  assert(new Set(tracks.map(track => track.StreamIndex)).size === 3 && tracks.every(track => Number.isSafeInteger(track.StreamIndex) && track.StreamIndex > 0));
  config.itemId = item.Id; config.libraryId = library.Id;
  await writeFile(privatePath, JSON.stringify(config)); await chmod(privatePath, 0o600);
  await writeFile(resolve(output, 'scanned-tracks.json'), JSON.stringify({ itemId: item.Id, durationTicks: item.RunTimeTicks, subtitles, tracks }, null, 2));
  if (process.env.GOBY_EXTERNAL_UI_ONLY === '1') {
    browser = await chromium.launch({ headless: true, args: ['--no-sandbox'] });
    await phase('real browser hides stale external rows without generating work', async () => {
      const saved = await disk(), original = await readFile(subtitle);
      assert.equal(await realpath(subtitle), subtitle);
      await writeFile(subtitle, Buffer.concat([original, Buffer.alloc(1)]));
      let visual;
      try {
        assert.deepEqual(await descriptor(), { Available: false, Stale: true });
        visual = await image('desktop-stale-hidden', 0, { width: 1440, height: 900 });
        assert.deepEqual(await disk(), saved);
      } finally { await writeFile(subtitle, original); await scan(); await generate(true); }
      await checkTracks(await descriptor());
      return { visual, stalePublicationRetained: true, recoveredByExplicitForce: true };
    });
    await phase('real desktop and mobile render three external coverage lanes', async () => ({
      desktop: await image('desktop-generated', 3, { width: 1440, height: 900 }),
      mobile: await image('mobile-generated', 3, { width: 393, height: 852 }),
    }));
    await writeFile(resolve(output, 'browser-results.json'), JSON.stringify(evidence, null, 2));
  } else {
    await phase('native scan discovers three external bitmap tracks and reads never generate', async () => {
      const before = await history(); assert.equal(await disk(), null);
      for (let i = 0; i < 3; i++) assert.deepEqual(await descriptor(), { Available: false });
      assert.equal(await history(), before); assert.equal(await disk(), null);
      return { subtitles, tracks, automaticGeneration: false, taskRunsUnchanged: true };
    });
    await phase('native administrator task publishes real three-track GSTL v2 beside the source', async () => {
      const result = await generate(); assert.equal(result.Reused, false);
      return { file: await disk(), tracks: await checkTracks(await descriptor()) };
    });
    let saved = await disk(), current = await descriptor();
    await phase('ordinary generation preserves the existing publication', async () => {
      assert.equal((await generate()).Reused, true); assert.deepEqual(await disk(), saved); assert.deepEqual(await descriptor(), current);
      return { unchangedGeneration: saved.generation };
    });
    await phase('anonymous and library-restricted users cannot read timelines', async () => {
      const anonymous = await request.newContext();
      const urls = [`/emby/Items/${item.Id}/SubtitleTimelines`, current.Streams[0].Url];
      for (const path of urls) assert.equal((await anonymous.get(config.baseURL + path)).status(), 401);
      await anonymous.dispose();
      config.restrictedPassword ||= randomUUID();
      const created = await admin('POST', '/admin/v1/users', { Name: 'external-restricted', Password: config.restrictedPassword, IsAdministrator: false }, 201);
      const user = (await admin('GET', `/admin/v1/users/${created.User.Id}`)).User;
      await admin('PUT', `/admin/v1/users/${user.Id}`, { Revision: user.Revision, Name: user.Name, IsAdministrator: false, IsDisabled: false, Policy: { ...user.Policy, EnableAllFolders: false, EnabledFolders: [] } });
      const restricted = await json(consumer, 'POST', '/emby/Users/AuthenticateByName', { Username: 'external-restricted', Pw: config.restrictedPassword });
      const statuses = [];
      for (const path of urls) {
        const target = new URL(config.baseURL + path); target.searchParams.delete('UserId');
        const response = await consumer.get(target.href, { headers: { 'X-Emby-Token': restricted.AccessToken } });
        assert([403, 404].includes(response.status())); statuses.push(response.status());
      }
      await writeFile(privatePath, JSON.stringify(config)); await chmod(privatePath, 0o600);
      return { anonymous: [401, 401], restricted: statuses };
    });
    await phase('changed SUB bytes hide stale coverage and ordinary requests preserve the old generation', async () => {
      assert.equal(await realpath(subtitle), subtitle);
      const data = await readFile(subtitle); await writeFile(resolve(root, 'original-sub.private.bin'), data, { flag: 'wx', mode: 0o600 });
      const changed = Buffer.concat([data, Buffer.alloc(1)]); await writeFile(subtitle, changed);
      try {
        assert.deepEqual(await descriptor(), { Available: false, Stale: true });
        assert.equal((await consumer.get(config.baseURL + current.Streams[0].Url, { headers: { 'X-Emby-Token': config.token } })).status(), 404);
        assert.deepEqual(await disk(), saved); assert.equal((await generate()).Reused, true); assert.deepEqual(await disk(), saved);
      } finally { await writeFile(subtitle, data); }
      assert.deepEqual(await descriptor(), { Available: false, Stale: true });
      return { staleOnCompanionChange: true, oldGenerationRetained: saved.generation, oldURLStatus: 404 };
    });
    await phase('rescan and explicit Force publish current external intervals', async () => {
      await scan(); const before = saved; assert.equal((await generate(true)).Reused, false);
      saved = await disk(); current = await descriptor(); assert.notEqual(saved.generation, before.generation);
      return { oldGeneration: before.generation, newGeneration: saved.generation, tracks: await checkTracks(current) };
    });
    await phase('failed Force keeps the prior complete publication readable', async () => {
      const mode = (await stat(sidecar)).mode & 0o777;
      try {
        await chmod(sidecar, 0o500); const failed = await generate(true, 'failed');
        assert.deepEqual(await disk(), saved); await checkTracks(await descriptor());
        return { state: failed.State, code: failed.ErrorCode, publicationPreserved: true };
      } finally { await chmod(sidecar, mode); }
    });
    await phase('restart preserves source-side material and consumer intervals', async () => {
      execFileSync('docker', ['restart', config.container], { encoding: 'utf8', timeout: 120_000 });
      await wait(async () => { try { return (await consumer.get(config.baseURL + '/admin/v1/bootstrap')).ok(); } catch { return false; } }, 'The isolated backend did not restart.');
      assert.deepEqual(await disk(), saved); await checkTracks(await descriptor());
      return { generation: saved.generation, sourceSideBytes: saved.size, persistsAcrossRestart: true };
    });
    await writeFile(resolve(output, 'api-results.json'), JSON.stringify(evidence, null, 2));
  }
} catch (error) {
  evidence.failures.push(safe(error)); await save(); console.error(safe(error)); process.exitCode = 1;
} finally {
  await browser?.close(); await administrator.dispose(); await consumer.dispose();
}
