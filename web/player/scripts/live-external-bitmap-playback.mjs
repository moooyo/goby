import { chromium } from '@playwright/test';
import assert from 'node:assert/strict';
import { mkdir, readFile, realpath, writeFile } from 'node:fs/promises';
import { basename, resolve } from 'node:path';
import { createHash, randomUUID } from 'node:crypto';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { analyzeBitmapFrame, AUTHORED_BITMAP_INTERVALS, BITMAP_PIXEL_THRESHOLDS } from './bitmap-playback-pixels.mjs';

// Browser evidence uses the real application and the decoded video element.
// API offset checks consume actual encoded media with the same authored oracle.
assert.equal(process.platform, 'linux', 'Run only on the designated remote Linux host.');
const root = '/opt/goby-test/external-bitmap-playback-20261006-165c';
const owner = 'goby-external-bitmap-playback-165c-20261006';
assert.equal(await realpath(root), root);
assert.equal((await readFile(resolve(root, '.owner'), 'utf8')).trim(), owner);
const config = JSON.parse(await readFile(resolve(root, 'live.private.json'), 'utf8'));
assert.equal(config.owner, owner);
const fixture = JSON.parse(await readFile(resolve(root, 'logs/pixel-fixture.json'), 'utf8'));
assert.equal(fixture.canvas.width, 320); assert.equal(fixture.canvas.height, 192);
const base = new URL(config.baseURL).origin, player = new URL(config.playerURL).origin;
for (const url of [base, player]) assert.equal(new URL(url).hostname, '127.0.0.1');
const artifacts = resolve(root, 'browser-output');
await mkdir(artifacts, { mode: 0o700 }).catch(error => { if (error.code !== 'EEXIST') throw error; });
assert.equal(await realpath(artifacts), artifacts);
const output = resolve(artifacts, `live-${Date.now()}-${randomUUID().slice(0, 8)}`);
await mkdir(output, { mode: 0o700 });
const evidence = { format: 'goby-external-bitmap-playback-live-v1', owner, phases: [], failures: [], negotiations: [], frames: [],
  authoredIntervals: AUTHORED_BITMAP_INTERVALS, pixelThresholds: BITMAP_PIXEL_THRESHOLDS, startedAt: new Date().toISOString() };
const execute = promisify(execFile), sensitive = new Set([config.password, config.databasePassword, config.setupToken]);
const negotiations = [], requests = [], sessions = new Set(), mutations = new Set();
let browser, administrator, consumer, page, csrf = '', token = '', userId = '', library, item, streams, beforeSnapshot, timelineRunsBefore;
let monitor, browserDeviceId, browserToken;
const labels = { username: '\u7528\u6237\u540d', password: '\u5bc6\u7801', login: '\u767b\u5f55', navigation: '\u4e3b\u5bfc\u822a',
  chooseSubtitle: '\u9009\u62e9\u5b57\u5e55', off: '\u5173\u95ed', pause: '\u6682\u505c', play: '\u64ad\u653e',
  subtitles: '\u5b57\u5e55', progress: '\u64ad\u653e\u8fdb\u5ea6' };
function safe(error) {
  let text = String(error?.stack || error);
  for (const value of [...sensitive, csrf, token].filter(Boolean)) {
    text = text.replaceAll(value, '[redacted]').replaceAll(encodeURIComponent(value), '[redacted]');
  }
  return text.replace(/(https?:\/\/[^\s?'"<>]+)\?[^\s'"<>]+/g, '$1?[redacted]')
    .replace(/((?:cookie|set-cookie|authorization|x-emby-token|x-csrf-token)["']?\s*:\s*)[^\r\n]+/gi, '$1[redacted]');
}
async function saveJSON(name, value) {
  await writeFile(resolve(output, name), JSON.stringify(value, (key, value) => /^(AccessToken|CSRFToken|Password|Pw|SetupToken)$/i.test(key)
    ? '[redacted]' : typeof value === 'string' ? safe(value) : value, 2) + '\n', { mode: 0o600 });
}
async function phase(name, action) {
  const result = await action(); evidence.phases.push({ name, passed: true, ...result });
  await saveJSON('results.json', evidence); console.log(JSON.stringify({ phase: name, passed: true }));
}
const delay = ms => new Promise(done => setTimeout(done, ms));
async function wait(check, message, timeout = 120_000, interval = 200) {
  const until = Date.now() + timeout;
  while (Date.now() < until) { const result = await check(); if (result) return result; await delay(interval); }
  throw new Error(message);
}
async function host(action, extra = []) {
  assert(['describe', 'snapshot', 'processes', 'mutate', 'restore', 'decode'].includes(action));
  const result = await execute('python3', [resolve(root, 'external-bitmap-playback-live.py'), action, ...extra], {
    timeout: action === 'decode' ? 165_000 : 120_000, maxBuffer: 2 * 1024 * 1024,
  });
  return JSON.parse(result.stdout);
}
function url(path, origin = base) {
  const value = new URL(path, origin); assert.equal(value.origin, origin); return value.href;
}
async function json(context, method, path, body, status) {
  const response = await context.request.fetch(url(path), { method, data: body,
    headers: path.startsWith('/admin/') ? { Origin: player, 'X-CSRF-Token': csrf } : {
      'X-Emby-Token': token, 'X-Emby-Authorization': 'Emby Client="Bitmap playback acceptance", Device="Remote browser", DeviceId="external-bitmap-playback-api", Version="1"',
    } });
  assert(status === undefined ? response.ok() : response.status() === status, `${method} ${path.split('?')[0]} returned ${response.status()}.`);
  const data = await response.text(); return data ? JSON.parse(data) : undefined;
}
const admin = (method, path, body, status) => json(administrator, method, path, body, status);
const api = path => json(consumer, 'GET', path);
async function stopEncoding(id, device = 'external-bitmap-playback-api', authentication = token) {
  assert.match(device, /^[A-Za-z0-9._-]{1,128}$/);
  const browserSession = device === browserDeviceId;
  const authorization = browserSession
    ? `Emby Client="Goby Player", Device="Web Browser", DeviceId="${device}", Version="1.0.0"`
    : `Emby Client="Bitmap playback acceptance", Device="Remote browser", DeviceId="${device}", Version="1"`;
  const response = await consumer.request.delete(url(`/emby/Videos/ActiveEncodings?PlaySessionId=${encodeURIComponent(id)}&DeviceId=${encodeURIComponent(device)}`), {
    headers: { 'X-Emby-Token': authentication, 'X-Emby-Authorization': authorization },
  });
  assert.equal(response.status(), 204, 'Owned encoding cleanup must use the same authentication session and device.');
}
async function screenshot(name) {
  await page.screenshot({ path: resolve(output, `${name}.png`), fullPage: true, mask: [page.locator('input[type="password"]')] });
}
async function scan() {
  const receipt = await admin('POST', `/admin/v1/libraries/${library.Id}/scan`, { ForceProbe: false }, 202);
  const job = await wait(async () => {
    const value = (await admin('GET', '/admin/v1/jobs')).Items.find(entry => entry.Id === receipt.Job.Id);
    assert(!value || !['failed', 'canceled', 'cancelled'].includes(value.Status), 'The actual library scan failed.');
    return value && ['completed', 'succeeded'].includes(value.Status) && value;
  }, 'The actual library scan did not finish.');
  return { jobId: receipt.Job.Id, status: job.Status };
}
async function timelineState() {
  const definition = (await admin('GET', '/admin/v1/tasks')).Items.find(entry => entry.Key === 'media.subtitle_timeline_generation');
  assert(definition, 'The timeline task must exist even though this acceptance does not run it.');
  const history = await admin('GET', `/admin/v1/tasks/${definition.Id}/runs?Limit=1&StartIndex=0`);
  const descriptor = await api(`/emby/Items/${item.Id}/SubtitleTimelines`);
  assert.equal(descriptor.Available, false, 'Playback acceptance must not depend on a generated timeline.');
  const snapshot = await host('snapshot');
  assert.equal(snapshot.timelineSidecarExists, false, 'Playback created an unrequested persistent timeline.');
  return { runs: history.TotalRecordCount, descriptor, snapshot };
}
function monitorProcesses() {
  let stopped = false, failure; const observations = [];
  const promise = (async () => {
    try { while (!stopped) { observations.push({ at: new Date().toISOString(), ...await host('processes') }); if (!stopped) await delay(180); } }
    catch (error) { failure = error; }
  })();
  return { observations, async stop() { stopped = true; await promise; if (failure) throw failure; } };
}
async function paused() {
  await page.mouse.move(720, 820);
  await page.waitForFunction(() => {
    const video = document.querySelector('.player-video'); return video?.readyState >= 2 && video.videoWidth > 0 && !video.error;
  }, null, { timeout: 90_000 });
  if (!(await page.locator('.player-video').evaluate(video => video.paused))) await page.getByRole('button', { name: labels.pause, exact: true }).click();
  await page.waitForFunction(() => document.querySelector('.player-video')?.paused);
}
async function frameAt(time, kind, name) {
  await paused();
  const serial = randomUUID();
  await page.locator('.player-video').evaluate((video, { time, serial }) => {
    window.__bitmapObservedFrame = undefined;
    const observe = (_now, metadata) => {
      if (Math.abs(metadata.mediaTime - time) <= 0.10) window.__bitmapObservedFrame = { serial, mediaTime: metadata.mediaTime, presentedFrames: metadata.presentedFrames };
      else video.requestVideoFrameCallback(observe);
    };
    video.requestVideoFrameCallback(observe);
  }, { time, serial });
  const slider = page.getByRole('slider', { name: labels.progress, exact: true });
  const box = await slider.boundingBox(); assert(box);
  const duration = Number(await slider.getAttribute('aria-valuemax')); assert.equal(duration, 35);
  await page.mouse.click(box.x + box.width * time / duration, box.y + box.height / 2);
  await page.waitForFunction(({ time, serial }) => {
    const video = document.querySelector('.player-video');
    return video && !video.seeking && video.readyState >= 2 && Math.abs(video.currentTime - time) <= .15 && window.__bitmapObservedFrame?.serial === serial;
  }, { time, serial }, { timeout: 90_000 });
  await paused();
  const frame = await page.locator('.player-video').evaluate(video => {
    const canvas = document.createElement('canvas'); canvas.width = video.videoWidth; canvas.height = video.videoHeight;
    const context = canvas.getContext('2d', { willReadFrequently: true }); context.drawImage(video, 0, 0);
    return { width: canvas.width, height: canvas.height, rgba: Array.from(context.getImageData(0, 0, canvas.width, canvas.height).data),
      png: canvas.toDataURL('image/png'), currentTime: video.currentTime, frame: window.__bitmapObservedFrame };
  });
  const result = analyzeBitmapFrame({ rgba: frame.rgba, width: frame.width, height: frame.height, kind, timeSeconds: frame.frame.mediaTime });
  const record = { name, kind, requestedSeconds: time, mediaTime: frame.frame.mediaTime, currentTime: frame.currentTime, width: frame.width, height: frame.height, result };
  evidence.frames.push(record); await saveJSON(`${name}.json`, record);
  await writeFile(resolve(output, `${name}-decoded.png`), Buffer.from(frame.png.split(',')[1], 'base64'), { mode: 0o600 });
  assert(result.passed, `${name}: ${JSON.stringify(result.failures)}`);
  return record;
}
async function select(kind) {
  await paused();
  const before = negotiations.length;
  await page.getByRole('button', { name: labels.subtitles, exact: true }).click();
  const options = page.getByRole('option');
  assert.equal(await options.count(), streams.all.length + 1, 'The player must offer the three actual external bitmap tracks and Off.');
  const index = kind === 'off' ? -1 : streams[kind].Index;
  const ordinal = kind === 'off' ? 0 : streams.all.findIndex(entry => entry.Index === index) + 1;
  assert(ordinal >= 0); await options.nth(ordinal).click();
  const negotiation = await wait(() => negotiations.slice(before).find(value => value.request.SubtitleStreamIndex === index), 'The player did not negotiate the selected public subtitle index.');
  assert(!negotiation.info.ErrorCode);
  if (kind !== 'off') {
    const source = negotiation.info.MediaSources[0];
    assert.equal(source.SupportsDirectPlay, false); assert.equal(source.SupportsTranscoding, true);
    assert.equal(source.DefaultSubtitleStreamIndex, index);
    const encoded = source.MediaStreams.find(entry => entry.Index === index && entry.Type === 'Subtitle');
    assert.equal(encoded.DeliveryMethod, 'Encode');
    assert(!encoded.DeliveryUrl, 'A bitmap track must not be exposed as an external text URL.');
  }
  await paused(); return { kind, publicStreamIndex: index, playSessionId: negotiation.info.PlaySessionId };
}
async function naturalSUPPlayback() {
  await frameAt(1.5, 'sup', 'sup-natural-start');
  const observation = page.locator('.player-video').evaluate(video => new Promise((resolve, reject) => {
    const began = performance.now(), startTime = video.currentTime, samples = [], targets = [3.0, 4.5, 6.0];
    let observed = 0, previous = -1, previousPresented = -1;
    const timer = setTimeout(() => reject(new Error('Natural bitmap playback did not reach its clear event.')), 15_000);
    const observe = (_now, metadata) => {
      if (video.seeking || metadata.mediaTime < previous || metadata.presentedFrames <= previousPresented || video.playbackRate !== 1) {
        clearTimeout(timer); reject(new Error('Natural bitmap playback lost its continuous source clock.')); return;
      }
      previous = metadata.mediaTime; previousPresented = metadata.presentedFrames; observed++;
      if (metadata.mediaTime >= targets[samples.length]) {
        const canvas = document.createElement('canvas'); canvas.width = video.videoWidth; canvas.height = video.videoHeight;
        const context = canvas.getContext('2d', { willReadFrequently: true }); context.drawImage(video, 0, 0);
        samples.push({ mediaTime: metadata.mediaTime, currentTime: video.currentTime, presentedFrames: metadata.presentedFrames,
          width: canvas.width, height: canvas.height, rgba: Array.from(context.getImageData(0, 0, canvas.width, canvas.height).data), png: canvas.toDataURL('image/png') });
      }
      if (samples.length === targets.length) {
        clearTimeout(timer); resolve({ startTime, finalTime: video.currentTime, elapsedMs: performance.now() - began, observedFrames: observed,
          playbackRate: video.playbackRate, paused: video.paused, samples });
      } else video.requestVideoFrameCallback(observe);
    };
    video.requestVideoFrameCallback(observe);
  }));
  // Retain the observation's rejection for the enclosing phase even if the UI
  // click itself fails before the observation is awaited.
  observation.catch(() => undefined);
  assert.equal(await page.locator('.player-toggle').getAttribute('aria-label'), labels.play);
  await page.locator('.player-toggle').click();
  const value = await observation; await paused();
  assert.equal(value.paused, false); assert.equal(value.playbackRate, 1); assert(value.observedFrames >= 70);
  const expectedMs = (value.finalTime - value.startTime) * 1000;
  assert(value.elapsedMs >= expectedMs - 200 && value.elapsedMs <= expectedMs + 3500, 'Bitmap playback did not advance at natural wall-clock cadence.');
  const samples = [];
  for (let index = 0; index < value.samples.length; index++) {
    const frame = value.samples[index], name = `sup-natural-${index}`;
    const result = analyzeBitmapFrame({ rgba: frame.rgba, width: frame.width, height: frame.height, kind: 'sup', timeSeconds: frame.mediaTime });
    const record = { name, mediaTime: frame.mediaTime, currentTime: frame.currentTime, presentedFrames: frame.presentedFrames, result };
    samples.push(record); evidence.frames.push(record); assert(result.passed, `${name}: ${JSON.stringify(result.failures)}`);
    await writeFile(resolve(output, `${name}-decoded.png`), Buffer.from(frame.png.split(',')[1], 'base64'), { mode: 0o600 });
  }
  const record = { ...value, samples }; await saveJSON('sup-natural-playback.json', record); return record;
}
async function endPlayer() {
  if (await page.locator('.player-video').count()) {
    await page.keyboard.press('Escape'); await page.locator('.detail-overview-info').waitFor();
  }
  await wait(async () => !(await host('processes')).media.length, 'The player did not release its media helpers.', 30_000);
}
const apiProfile = { Name: 'Bitmap pixel HTTP acceptance', SupportedMediaTypes: 'Video', MaxStreamingBitrate: 3_000_000,
  DirectPlayProfiles: [], TranscodingProfiles: [{ Type: 'Video', Container: 'mp4', Protocol: 'http', VideoCodec: 'h264', AudioCodec: 'aac', Context: 'Streaming', MaxAudioChannels: '2' }],
  SubtitleProfiles: [{ Format: 'hdmv_pgs_subtitle,dvd_subtitle', Method: 'Encode', Container: 'mp4', Protocol: 'http' }] };
async function apiPlayback(kind, { offset = 0, start = 0 } = {}) {
  const input = { UserId: userId, IsPlayback: true, StartTimeTicks: Math.round(start * 10_000_000), MediaSourceId: item.MediaSources[0].Id,
    SubtitleStreamIndex: streams[kind].Index, EnableDirectPlay: false, EnableDirectStream: false, EnableTranscoding: true,
    AllowVideoStreamCopy: false, MaxStreamingBitrate: 3_000_000, DeviceProfile: apiProfile };
  const info = await json(consumer, 'POST', `/emby/Items/${item.Id}/PlaybackInfo`, input);
  assert(!info.ErrorCode && info.PlaySessionId); sessions.add(info.PlaySessionId);
  const source = info.MediaSources[0]; assert.equal(source.SupportsTranscoding, true); assert(source.TranscodingUrl);
  const selected = new URL(source.TranscodingUrl, base);
  assert.equal(selected.origin, base); assert(/\/stream\.mp4$/.test(selected.pathname), 'The API pixel case requires an actual progressive MP4 negotiation.');
  selected.searchParams.set('PlaySessionId', info.PlaySessionId); selected.searchParams.set('DeviceId', 'external-bitmap-playback-api');
  selected.searchParams.set('SubtitleOffsetTicks', String(Math.round(offset * 10_000_000)));
  return { kind, input, info, url: selected.href, offset, start };
}
async function apiPixels(attempt, points, name) {
  const response = await consumer.request.get(attempt.url, { headers: { 'X-Emby-Token': token }, timeout: 120_000 });
  assert.equal(response.status(), 200); assert.match(response.headers()['content-type'], /^video\/mp4/);
  const data = await response.body(); assert(data.length > 0 && data.length <= 64 * 1024 * 1024);
  const path = resolve(output, `${name}.mp4`); await writeFile(path, data, { flag: 'wx', mode: 0o600 });
  const results = [];
  for (const absolute of points) {
    const relative = absolute - attempt.start; assert(relative >= 0);
    const framePath = resolve(root, 'frames', basename(output), `${name}-${String(absolute).replace('.', '_')}.rgba`);
    const decoded = await host('decode', ['--input', path, '--seconds', String(relative), '--output', framePath]);
    const receipt = JSON.parse(await readFile(framePath + '.json', 'utf8'));
    const rgba = await readFile(framePath);
    const time = attempt.start + receipt.frameSeconds;
    assert(Math.abs(time - absolute) <= 1 / 24 + .002, 'The API sample did not decode the intended source clock.');
    const result = analyzeBitmapFrame({ rgba, width: 320, height: 192, kind: attempt.kind, timeSeconds: time, offsetSeconds: attempt.offset });
    const record = { requestedAbsoluteSeconds: absolute, actualAbsoluteSeconds: time, relativeSeconds: relative, result, decoded };
    results.push(record); assert(result.passed, `${name} at ${time}: ${JSON.stringify(result.failures)}`);
  }
  const value = { kind: attempt.kind, offsetSeconds: attempt.offset, startSeconds: attempt.start, publicStreamIndex: streams[attempt.kind].Index,
    bytes: data.length, sha256: createHash('sha256').update(data).digest('hex'), results };
  await saveJSON(`${name}.json`, value); return value;
}

try {
  await wait(async () => { try { return (await fetch(base + '/readyz')).ok; } catch { return false; } }, 'The new backend did not become ready.');
  evidence.host = await host('describe'); beforeSnapshot = await host('snapshot'); await saveJSON('source-before.json', beforeSnapshot);
  browser = await chromium.launch({ headless: true, args: ['--no-sandbox'] });
  administrator = await browser.newContext(); consumer = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: 'zh-CN' });
  if (!(await admin('GET', '/admin/v1/bootstrap')).Initialized) await admin('POST', '/admin/v1/bootstrap', { SetupToken: config.setupToken, Name: config.username, Password: config.password }, 201);
  const login = await admin('POST', '/admin/v1/session', { Name: config.username, Password: config.password }); csrf = login.CSRFToken;
  for (const cookie of await administrator.cookies()) if (cookie.value) sensitive.add(cookie.value);
  const authenticated = await json(consumer, 'POST', '/emby/Users/AuthenticateByName', { Username: config.username, Pw: config.password });
  token = authenticated.AccessToken; userId = authenticated.User.Id;
  const libraries = (await admin('GET', '/admin/v1/libraries')).Items;
  assert(libraries.length <= 1 && libraries.every(value => value.Name === 'External bitmap playback acceptance'
    && value.Paths.length === 1 && value.Paths[0] === '/media'), 'The owned application database contains an unrelated library.');
  library = libraries[0] ?? (await admin('POST', '/admin/v1/libraries', { Name: 'External bitmap playback acceptance', CollectionType: 'movies', Paths: ['/media'], Scan: false }, 201)).Library;
  assert.equal(library.LibraryOptions.EnableSubtitleTimelineGeneration, false);
  const scanned = await scan();
  const catalog = await api(`/emby/Items?ParentId=${library.Id}&Recursive=true&IncludeItemTypes=Movie&Fields=MediaSources,MediaStreams,Path`);
  const matches = catalog.Items.filter(value => value.Path === config.mediaPath || value.MediaSources?.some(source => source.Path === config.mediaPath));
  assert.equal(matches.length, 1); [item] = matches;
  const subtitles = item.MediaSources[0].MediaStreams.filter(value => value.Type === 'Subtitle');
  assert.equal(subtitles.length, 3); assert(subtitles.every(value => value.IsExternal && value.IsTextSubtitleStream === false && !value.GobySubtitleTimelineOnly));
  streams = { all: subtitles, sup: subtitles.find(value => value.Codec === 'hdmv_pgs_subtitle'),
    en: subtitles.find(value => value.Codec === 'dvd_subtitle' && /^(en|eng)$/i.test(value.Language)),
    zh: subtitles.find(value => value.Codec === 'dvd_subtitle' && /^(zh|chi|zho)$/i.test(value.Language)) };
  assert(streams.sup && streams.en && streams.zh); assert.equal(new Set(subtitles.map(value => value.Index)).size, 3);
  await saveJSON('scanned-tracks.json', { scanned, itemId: item.Id, streams });
  timelineRunsBefore = (await timelineState()).runs;
  page = await consumer.newPage();
  page.on('pageerror', error => evidence.failures.push({ kind: 'page', message: safe(error) }));
  page.on('request', request => requests.push({ method: request.method(), path: new URL(request.url()).pathname }));
  page.on('response', async response => {
    try {
      const path = new URL(response.url()).pathname;
      if (path === '/emby/Users/AuthenticateByName' && response.ok()) {
        const result = await response.json(); if (result.AccessToken) { browserToken = result.AccessToken; sensitive.add(result.AccessToken); }
      }
      if (path === `/emby/Items/${item.Id}/PlaybackInfo` && response.ok()) {
        const request = response.request().postDataJSON(), info = await response.json(); negotiations.push({ request, info });
        evidence.negotiations.push({ subtitleIndex: request.SubtitleStreamIndex, startTicks: request.StartTimeTicks, playSessionId: info.PlaySessionId,
          errorCode: info.ErrorCode, sources: info.MediaSources?.map(source => ({ Id: source.Id, direct: source.SupportsDirectPlay, transcode: source.SupportsTranscoding,
            selectedSubtitleIndex: source.DefaultSubtitleStreamIndex, subtitleMethods: source.MediaStreams?.filter(stream => stream.Type === 'Subtitle').map(stream => ({ index: stream.Index, method: stream.DeliveryMethod })) })) });
      }
    } catch (error) { evidence.failures.push({ kind: 'response', message: safe(error) }); }
  });
  await phase('native scan offers three bitmap tracks and an explicit Off selection plays without timelines', async () => {
    await page.goto(player); await page.getByLabel(labels.username, { exact: true }).fill(config.username);
    await page.getByLabel(labels.password, { exact: true }).fill(config.password); await page.getByRole('button', { name: labels.login, exact: true }).click();
    await page.getByRole('navigation', { name: labels.navigation }).waitFor();
    browserDeviceId = await page.evaluate(() => localStorage.getItem('goby.player.device.v1'));
    await page.goto(`${player}/#/detail/${item.Id}`); await page.locator('.detail-overview-info h1').waitFor();
    const selectionBefore = await page.getByTitle(labels.chooseSubtitle, { exact: true }).innerText();
    await page.getByTitle(labels.chooseSubtitle, { exact: true }).click();
    await page.getByRole('menuitemradio', { name: new RegExp('^' + labels.off) }).click();
    await wait(async () => {
      const saved = await api(`/emby/DisplayPreferences/goby-player-item-${item.Id}?UserId=${userId}&Client=Goby%20Player`);
      return JSON.parse(saved.CustomPrefs?.selection || '{}').subtitleStreamIndex === -1;
    }, 'The explicit Off selection did not persist.');
    await page.locator('.deck-panel[data-active=true] .play-disc').click(); await paused();
    const first = await wait(() => negotiations[0], 'The independent player did not negotiate playback.');
    assert.equal(first.request.SubtitleStreamIndex, -1); assert.equal(first.info.MediaSources[0].SupportsDirectPlay, true);
    await frameAt(1.5, 'off', 'default-off-active-caption-window'); await screenshot('default-off');
    return { itemId: item.Id, tracks: subtitles.length, explicitSubtitleIndex: -1, detailSelectionBefore: selectionBefore,
      existingSmartLanguagePolicyPreserved: true, timelineRuns: timelineRunsBefore };
  });
  monitor = monitorProcesses();
  await phase('SUP playback renders authored top, overlapping, bottom, and clear events', async () => {
    await frameAt(.75, 'off', 'before-sup'); const selected = await select('sup');
    for (const time of [1.5, 3.5, 4.5, 6.5]) await frameAt(time, 'sup', `sup-${time}`);
    const naturalPlayback = await naturalSUPPlayback();
    await screenshot('sup-controls'); return { ...selected, naturalPlayback };
  });
  await phase('IDX English selection follows its delayed first cue and its distinct second cue', async () => {
    await frameAt(.75, 'sup', 'before-en'); const selected = await select('en');
    for (const time of [1.5, 4.5, 11.5]) await frameAt(time, 'en', `en-${time}`);
    await screenshot('idx-english'); return selected;
  });
  await phase('IDX Chinese-language selection uses its distinct source stream and clock', async () => {
    await frameAt(.75, 'en', 'before-zh'); const selected = await select('zh');
    for (const time of [1.5, 4.5, 11.5]) await frameAt(time, 'zh', `zh-${time}`);
    await screenshot('idx-chinese-label'); return { ...selected, authoredGlyphText: 'Hello world', languageSelectionProvedByDisjointIntervals: true };
  });
  await phase('turning subtitles off and switching them back leaves no previous bitmap', async () => {
    await select('off'); await frameAt(1.5, 'off', 'off-after-zh');
    await frameAt(.75, 'off', 'before-sup-switch'); await select('sup'); await frameAt(3.5, 'sup', 'sup-after-off');
    await select('off'); await frameAt(3.5, 'off', 'off-after-overlapping-sup');
    return { previousBitmapCleared: true };
  });
  await phase('real player seeks into active and blank intervals and restores an already active cue', async () => {
    await frameAt(.75, 'off', 'before-seek-sup'); await select('sup');
    for (const time of [3.0, 7.0, 1.5, 4.5]) await frameAt(time, 'sup', `seek-sup-${time}`);
    await screenshot('seek-inside-sup'); await endPlayer();
    await monitor.stop(); await saveJSON('media-processes.json', monitor.observations);
    assert(monitor.observations.some(value => value.media.some(process => process.encoder && process.bitmapBurn)), 'No real bitmap burn encoder was observed.');
    monitor = undefined; return { observedRealBitmapEncoder: true, pixelsFollowSourceClock: true };
  });
  await phase('positive and negative offsets affect real API-encoded pixels without a new UI control', async () => {
    const positive = await apiPixels(await apiPlayback('sup', { offset: .5 }), [1.25, 6.0], 'api-sup-offset-positive');
    const negative = await apiPixels(await apiPlayback('sup', { offset: -.5 }), [.75, 5.35], 'api-sup-offset-negative');
    const seek = await apiPixels(await apiPlayback('en', { start: 4.5 }), [4.5, 7.0], 'api-en-seek-active');
    return { positive, negative, nonzeroSeek: seek };
  });
  await phase('changed SUP and IDX companions reject prior negotiation before fresh rescanning', async () => {
    const results = [];
    for (const [extension, kind] of [['sup', 'sup'], ['idx', 'en'], ['sub', 'zh']]) {
      const attempt = await apiPlayback(kind);
      const sample = kind === 'sup' ? 3.5 : kind === 'en' ? 4.5 : 1.5;
      const before = await apiPixels(attempt, [sample], `before-mutating-${extension}`);
      mutations.add(extension); const mutation = await host('mutate', ['--extension', extension]);
      try {
        const response = await consumer.request.get(attempt.url, { headers: { 'X-Emby-Token': token } });
        const type = response.headers()['content-type'] || '';
        assert.equal(response.status(), 503, 'A changed source must follow the existing progressive source-unavailable contract.');
        assert(!type.startsWith('video/'), 'A changed subtitle source returned encoded media for an old negotiation.');
        const error = await response.json();
        assert.equal(error.ResponseStatus?.ErrorCode, 'video_unavailable');
        assert.equal((await consumer.request.get(base + '/readyz')).status(), 200, 'The source failure must not be a backend outage.');
        results.push({ extension, before, status: response.status(), error, mutation, backendRemainedReady: true });
      } finally { await host('restore', ['--extension', extension]); mutations.delete(extension); }
      await scan();
      const refreshed = await api(`/emby/Users/${userId}/Items/${item.Id}?Fields=MediaSources,MediaStreams,Path`); item = refreshed;
      await apiPixels(await apiPlayback(kind), [sample], `after-restoring-${extension}`);
    }
    return { cases: results };
  });
  await phase('playback releases media resources and never generates subtitle timelines', async () => {
    for (const id of sessions) await stopEncoding(id);
    await wait(async () => !(await host('processes')).media.length, 'Media helpers remained after playback.', 30_000);
    const after = await timelineState(); assert.equal(after.runs, timelineRunsBefore);
    const original = beforeSnapshot.sourceIdentities, final = after.snapshot.sourceIdentities;
    for (const extension of ['mp4', 'sup', 'idx', 'sub']) {
      assert.equal(final[extension].identity.sha256, original[extension].identity.sha256);
      assert.equal(final[extension].identity.bytes, original[extension].identity.bytes);
    }
    assert(!requests.some(value => value.method === 'POST' && value.path === '/admin/v1/media-analysis/runs'));
    assert(!requests.some(value => /\/Subtitles\/.*\/Stream\.vtt$/.test(value.path)), 'Bitmap playback incorrectly fetched a text subtitle.');
    await saveJSON('source-after.json', after.snapshot);
    return { timelineRuns: after.runs, timelineAvailable: false, timelineSidecarExists: false, remainingMediaHelpers: 0, fixtureBytesRestored: true };
  });
  assert.deepEqual(evidence.failures, []); evidence.acceptancePassed = true;
} catch (error) {
  evidence.failures.push({ kind: 'acceptance', message: safe(error) }); console.error(safe(error));
  if (page) await screenshot('failure').catch(() => undefined);
  process.exitCode = 1;
} finally {
  const cleanup = async action => { try { await action(); } catch (error) { evidence.failures.push({ kind: 'cleanup', message: safe(error) }); process.exitCode = 1; } };
  if (monitor) await cleanup(async () => { await monitor.stop(); await saveJSON('media-processes.json', monitor.observations); });
  for (const extension of mutations) await cleanup(() => host('restore', ['--extension', extension]));
  if (page) await cleanup(async () => { await page.goto('about:blank'); });
  if (token) {
    for (const id of sessions) await cleanup(() => stopEncoding(id));
    if (browserDeviceId && browserToken) for (const attempt of negotiations) {
      if (attempt.info.PlaySessionId) await cleanup(() => stopEncoding(attempt.info.PlaySessionId, browserDeviceId, browserToken));
    }
  }
  await cleanup(async () => { await browser?.close(); });
  evidence.finishedAt = new Date().toISOString(); evidence.complete = evidence.acceptancePassed === true && evidence.failures.length === 0 && !process.exitCode;
  await saveJSON('requests.json', requests); await saveJSON('results.json', evidence);
  if (!evidence.complete) process.exitCode = 1;
  console.log(JSON.stringify({ complete: evidence.complete, phases: evidence.phases.length, directory: output }));
}
