import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdir, readFile, realpath, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { chromium } from '@playwright/test';
import { createCatalogue, createUser, TOKEN } from '../tests/fixture-data.mjs';

// This harness replays reference artwork and intervals as API fixtures. It never
// changes production DOM, and is not evidence of real-media generation behavior.
assert.equal(process.platform, 'linux', 'Run visual verification on test-env.');
const root = await realpath(process.env.GOBY_LIVE_ROOT ?? '/opt/goby-test/player-live-20261004-165c');
assert.equal((await readFile(resolve(root, '.owner'), 'utf8')).trim(), 'goby-player-live-20261004-165c');
const phase = process.env.GOBY_TIMELINE_PHASE ?? 'before';
assert(['before', 'after'].includes(phase));
const output = resolve(root, 'artifacts/timeline-visual-review-20261005', phase);
await mkdir(output, { recursive: true });
const port = 4201;
const origin = `http://127.0.0.1:${port}`;
const durationTicks = 2880 * 10_000_000;
const positionTicks = 1372 * 10_000_000;
const itemId = 'deep-1-1';
const sourceId = `${itemId}-source`;
const fixtureUser = () => ({ ...createUser(), Name: '\u9648\u9ed8' });
const server = spawn(process.execPath, [resolve(import.meta.dirname, '../tests/fixture-server.mjs')], {
  env: { ...process.env, GOBY_FIXTURE_PORT: String(port), GOBY_HANDOFF_DIR: resolve(root, 'handoff-v3') }, stdio: ['ignore', 'pipe', 'pipe'],
});
let serverLog = '';
server.stdout.on('data', chunk => { serverLog += chunk; });
server.stderr.on('data', chunk => { serverLog += chunk; });
const browser = await chromium.launch({ headless: true });
const records = [];
const failures = [];
const references = new Map();
const widths = [[1440, 900], [393, 852], [375, 852]];
const referenceTimeline = page => page.locator('[data-pgs="media"] svg[viewBox="0 0 1000 10"]').first().locator('xpath=../../../..');
const activePanel = page => page.locator('.deck-panel[data-active="true"]');

async function settle(page) {
  await page.evaluate(() => document.fonts.ready);
  await page.waitForTimeout(1300);
  await page.mouse.move(0, 0);
}

async function geometry(locator, reference) {
  return locator.evaluate((timeline, isReference) => {
    const box = element => { const b = element.getBoundingClientRect(); return { x: b.x, y: b.y, width: b.width, height: b.height, bottom: b.bottom, right: b.right }; };
    const style = element => { const s = getComputedStyle(element); return { color: s.color, background: s.backgroundColor, fontFamily: s.fontFamily, fontSize: s.fontSize, fontWeight: s.fontWeight, lineHeight: s.lineHeight, gap: s.gap, borderRadius: s.borderRadius }; };
    const flex = isReference ? timeline.firstElementChild : timeline;
    const labels = isReference ? flex.children[0] : timeline.querySelector('.detail-track-labels');
    const lanes = isReference ? flex.children[1] : timeline.querySelector('.detail-track-lanes');
    const labelRows = [...labels.children].slice(1);
    const laneRows = [...lanes.children].slice(1).filter(element => isReference ? getComputedStyle(element).position !== 'absolute' : !element.classList.contains('detail-playhead'));
    const ruler = lanes.firstElementChild;
    const ticks = [...ruler.querySelectorAll('span')].filter(element => !element.querySelector('span') && getComputedStyle(element).display !== 'none').map(element => ({ text: element.textContent, ...box(element), ...style(element) }));
    const head = [...lanes.children].find(element => isReference ? getComputedStyle(element).position === 'absolute' : element.classList.contains('detail-playhead'));
    return {
      viewport: [innerWidth, innerHeight], documentWidth: document.documentElement.scrollWidth,
      timeline: { ...box(timeline), ...style(timeline) }, labels: { ...box(labels), ...style(labels) }, lanes: { ...box(lanes), ...style(lanes) },
      labelRows: labelRows.map(element => ({ text: element.textContent.trim(), ...box(element), ...style(element), children: [...element.querySelectorAll('span,strong,small,code')].filter(child => !child.querySelector('span,strong,small,code')).map(child => ({ text: child.textContent, ...style(child), ...box(child) })) })),
      laneRows: laneRows.map(element => ({ ...box(element), ...style(element), path: element.querySelector('svg > path:not(.detail-waveform-silence)')?.getAttribute('d'), images: [...element.querySelectorAll('img')].map(image => ({ src: image.src, loaded: image.complete && image.naturalWidth > 0 })) })),
      aligned: labelRows.length === laneRows.length && labelRows.every((element, index) => Math.abs(element.getBoundingClientRect().y - laneRows[index].getBoundingClientRect().y) < .1 && Math.abs(element.getBoundingClientRect().height - laneRows[index].getBoundingClientRect().height) < .1),
      ticks, ticksOverlap: ticks.some((tick, index) => index > 0 && tick.x < ticks[index - 1].right),
      head: head ? { text: head.textContent, ...box(head), bubble: { ...box(head.firstElementChild), ...style(head.firstElementChild) } } : null,
    };
  }, reference);
}

async function capture(page, name, locator, reference) {
  await settle(page);
  await page.screenshot({ path: resolve(output, `${name}-full.png`), fullPage: false });
  const scroll = reference ? page.locator('[data-pgs="media"]') : activePanel(page).locator('.stage-scroll');
  await scroll.evaluate(element => { element.scrollTop = element.scrollHeight; });
  await settle(page);
  await page.screenshot({ path: resolve(output, `${name}-bottom.png`), fullPage: false });
  await locator.screenshot({ path: resolve(output, `${name}-timeline.png`) });
  const metrics = await geometry(locator, reference);
  records.push({ name, kind: reference ? 'reference' : 'implementation', ...metrics });
  return metrics;
}

function decodeIntervals(path) {
  return [...path.matchAll(/M([\d.]+) 2h([\d.]+)v6h-[\d.]+z/g)].map(match => ({
    StartTicks: Math.round(Number(match[1]) / 1000 * durationTicks),
    EndTicks: Math.round((Number(match[1]) + Number(match[2])) / 1000 * durationTicks),
  }));
}

function waveformBytes(path, streamIndex, count = 4096) {
  const heights = [...path.matchAll(/M[\d.]+ ([\d.]+)h[\d.]+V([\d.]+)h-[\d.]+z/g)].map(match => (Number(match[2]) - Number(match[1])) / 2);
  assert.equal(heights.length, 180);
  const bitmapBytes = Math.ceil(count / 8);
  const body = Buffer.alloc(32 + count * 4 + bitmapBytes);
  body.write('GAWL'); body.writeUInt16LE(1, 4); body.writeUInt16LE(32, 6);
  body.writeUInt32LE(streamIndex, 8); body.writeUInt32LE(count, 12);
  body.writeBigUInt64LE(BigInt(durationTicks), 16); body.writeUInt32LE(bitmapBytes, 24);
  for (let index = 0; index < count; index++) {
    const peak = Math.min(65535, Math.round(heights[Math.min(179, Math.floor(index * 180 / count))] / 19 * 65535));
    body.writeUInt16LE(peak, 32 + index * 4); body.writeUInt16LE(Math.round(peak * .65), 34 + index * 4);
  }
  body.fill(255, 32 + count * 4);
  return body;
}

const stamp = value => {
  const milliseconds = Math.round(value / 10_000);
  return `${String(Math.floor(milliseconds / 3_600_000)).padStart(2, '0')}:${String(Math.floor(milliseconds / 60_000) % 60).padStart(2, '0')}:${String(Math.floor(milliseconds / 1000) % 60).padStart(2, '0')}.${String(milliseconds % 1000).padStart(3, '0')}`;
};

async function configureFixture(reference, state) {
  const catalogue = createCatalogue();
  for (const item of catalogue.items) {
    if (item.SeriesId === 'deep') Object.assign(item.UserData, { Played: false, PlaybackPositionTicks: 0, PlayedPercentage: 0 });
  }
  const item = catalogue.items.find(item => item.Id === itemId);
  item.RunTimeTicks = durationTicks;
  Object.assign(item.UserData, { Played: false, PlaybackPositionTicks: positionTicks, PlayedPercentage: positionTicks / durationTicks * 100 });
  const source = item.MediaSources[0];
  Object.assign(source, { RunTimeTicks: durationTicks, Container: 'mkv', Bitrate: 21_000_000, DefaultAudioStreamIndex: 1, DefaultSubtitleStreamIndex: state === 'unselected' ? -1 : 3 });
  source.MediaStreams = [
    { Index: 0, Type: 'Video', Codec: 'hevc', Width: 3840, Height: 1608, BitRate: 21_000_000, BitDepth: 10, RealFrameRate: 23.976, Profile: 'Main 10', Level: 153, PixelFormat: 'yuv420p10le', VideoRange: 'HDR', VideoRangeType: 'HDR10', ColorPrimaries: 'bt2020', ColorTransfer: 'smpte2084' },
    { Index: 1, Type: 'Audio', Codec: 'truehd', Profile: 'Dolby Atmos', Language: 'eng', DisplayTitle: 'English TrueHD Atmos 7.1', Channels: 8, ChannelLayout: '7.1', SampleRate: 48000, BitRate: 4_608_000, IsDefault: true },
    { Index: 2, Type: 'Audio', Codec: 'ac3', Language: 'cmn', DisplayLanguage: '\u56fd\u8bed', DisplayTitle: 'Mandarin AC3 5.1', Channels: 6, ChannelLayout: '5.1', SampleRate: 48000, BitRate: 640_000 },
    { Index: 3, Type: 'Subtitle', Codec: 'hdmv_pgs_subtitle', Language: 'chi', DisplayTitle: '\u7b80\u4f53\u4e2d\u6587', IsDefault: true, IsExternal: false },
    { Index: 4, Type: 'Subtitle', Codec: 'hdmv_pgs_subtitle', Language: 'chi', DisplayTitle: '\u7e41\u9ad4\u4e2d\u6587', IsExternal: false },
    { Index: 5, Type: 'Subtitle', Codec: 'srt', Language: 'eng', DisplayTitle: '\u82f1\u6587', IsExternal: true, IsTextSubtitleStream: true, DeliveryUrl: `/emby/Videos/${itemId}/${sourceId}/Subtitles/5/Stream.vtt` },
  ];
  item.MediaStreams = source.MediaStreams;
  const selection = { audioStreamIndex: 1, subtitleStreamIndex: state === 'unselected' ? -1 : 3, quality: 'original' };
  const response = await fetch(`${origin}/__fixture/config`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ...catalogue, user: fixtureUser(), display: { 'goby-player-item-deep': { Id: 'goby-player-item-deep', Revision: '1', CustomPrefs: { selection: JSON.stringify(selection) } } } }) });
  assert(response.ok);
  return selection;
}

async function installAPI(context, reference, state) {
  const subtitles = reference.laneRows.slice(3).map(row => decodeIntervals(row.path));
  const frames = reference.laneRows[0].images.map(image => image.src);
  assert.equal(frames.length, 12);
  assert(subtitles.every(intervals => intervals.length > 0));
  await context.route(/\/emby\/Items\/[^/]+\/AudioWaveforms(?:\?|$)/, route => route.fulfill({ json: { Available: true, MediaSourceId: sourceId, SourceVersion: 'reference-visual-fixture-v1', DurationTicks: durationTicks, Streams: [1, 2].map(StreamIndex => ({ StreamIndex, Channels: StreamIndex === 1 ? 8 : 6, SampleRate: 48000, Levels: [{ BucketCount: 4096, Url: `/emby/Items/${itemId}/AudioWaveforms/${StreamIndex}/4096?tag=visual-fixture` }] })) } }));
  await context.route(/\/emby\/Items\/[^/]+\/AudioWaveforms\/\d+\/4096\?/, route => {
    const index = Number(new URL(route.request().url()).pathname.split('/').at(-2));
    return route.fulfill({ contentType: 'application/octet-stream', body: waveformBytes(reference.laneRows[index].path, index) });
  });
  await context.route(/\/emby\/Items\/[^/]+\/SubtitleTimelines(?:\?|$)/, route => route.fulfill({ json: state === 'missing' ? { Available: false } : { Available: true, MediaSourceId: sourceId, SourceVersion: 'reference-visual-fixture-v1', DurationTicks: durationTicks, Streams: [3, 4].map((StreamIndex, index) => ({ StreamIndex, Codec: 'hdmv_pgs_subtitle', IntervalCount: subtitles[index].length, Url: `/emby/Items/${itemId}/SubtitleTimelines/${StreamIndex}?tag=visual-fixture` })) } }));
  await context.route(/\/emby\/Items\/[^/]+\/SubtitleTimelines\/\d+\?/, route => {
    const StreamIndex = Number(new URL(route.request().url()).pathname.split('/').at(-1));
    return route.fulfill({ json: { MediaSourceId: sourceId, SourceVersion: 'reference-visual-fixture-v1', StreamIndex, DurationTicks: durationTicks, Intervals: subtitles[StreamIndex - 3] } });
  });
  await context.route(/\/Subtitles\/5\/Stream\.vtt/, route => route.fulfill({ contentType: 'text/vtt', body: state === 'missing' ? 'WEBVTT\n\n' : `WEBVTT\n\n${subtitles[2].map(interval => `${stamp(interval.StartTicks)} --> ${stamp(interval.EndTicks)}\nVisual fixture cue`).join('\n\n')}\n` }));
  await context.route(/\/emby\/Items\/[^/]+\/ThumbnailSet\?/, route => route.fulfill({ json: { AspectRatio: 16 / 9, Thumbnails: frames.map((_, index) => ({ ImageTag: `frame-${index}`, PositionTicks: Math.round(index * durationTicks / 11) })) } }));
  await context.route(/\/emby\/Items\/[^/]+\/Images\/Thumbnail\?/, async route => {
    const index = Number(new URL(route.request().url()).searchParams.get('tag').replace('frame-', ''));
    const response = await context.request.get(frames[index]);
    await route.fulfill({ response });
  });
  // Both clients derive the accent from the same artwork sample, using their
  // own production extraction. The reference itself is left unmodified.
  await context.route(/\/emby\/Items\/deep\/Images\/Primary\?/, async route => {
    const response = await context.request.get(`${origin}/__fixture/picsum/id/881/64/64`);
    await route.fulfill({ response });
  });
}

try {
  for (let attempt = 0; attempt < 50; attempt++) {
    if (server.exitCode !== null) throw new Error(serverLog);
    try { if ((await fetch(`${origin}/__fixture/health`)).ok) break; } catch { /* Wait for this owned fixture server. */ }
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  for (const [width, height] of widths) {
    for (const state of ['selected', 'unselected']) {
      const context = await browser.newContext({ viewport: { width, height }, locale: 'zh-CN', reducedMotion: 'reduce', timezoneId: 'Asia/Shanghai' });
      await context.addInitScript(({ state }) => {
        localStorage.setItem('goby.proto.v1', JSON.stringify({ favs: { deep: 1 }, sp: { deep: { s: 1, e: 1 } }, prog: { 'deep:1:1': { f: 1372 / 2880 } }, watched: {}, recent: [], sel: { deep: { audio: 0, sub: state === 'unselected' ? 0 : 1, q: 0 } }, set: { motion: false } }));
        localStorage.setItem('goby.route.A', JSON.stringify({ name: 'detail', id: 'deep', page: 3 }));
      }, { state });
      const page = await context.newPage();
      page.on('pageerror', error => failures.push({ phase: 'reference', message: error.message }));
      await page.goto(`${origin}/reference/`);
      await page.locator('[data-pgs="media"] svg[viewBox="0 0 1000 10"]').first().waitFor();
      await settle(page);
      const metrics = await capture(page, `${width}x${height}-reference-${state}`, referenceTimeline(page), true);
      references.set(`${width}-${state}`, metrics);
      await context.close();
    }
  }
  await writeFile(resolve(output, 'reference-inputs.json'), JSON.stringify([...references], null, 2));
  for (const [width, height] of widths) {
    for (const state of ['selected', 'unselected', 'missing']) {
      const reference = references.get(`${width}-${state === 'unselected' ? 'unselected' : 'selected'}`);
      const selection = await configureFixture(reference, state);
      const context = await browser.newContext({ viewport: { width, height }, locale: 'zh-CN', reducedMotion: 'reduce', timezoneId: 'Asia/Shanghai' });
      await context.addInitScript(({ origin, selection, token, user }) => {
        localStorage.setItem('goby.player.session.v1', JSON.stringify({ AccessToken: token, User: user, ServerId: 'fixture-server', serverUrl: origin }));
        localStorage.setItem('goby.player.preferences.v1.fixture-server.fixture-viewer.item.deep', JSON.stringify(selection));
      }, { origin, selection, token: TOKEN, user: fixtureUser() });
      await installAPI(context, reference, state);
      const page = await context.newPage();
      page.on('pageerror', error => failures.push({ phase: 'implementation', state, message: error.message }));
      await page.goto(`${origin}/#/detail/deep`);
      await page.locator('.detail-actions').waitFor();
      await settle(page);
      await page.getByRole('navigation', { name: '\u9875\u9762\u5206\u9875' }).getByRole('button', { name: '\u5a92\u4f53\u4fe1\u606f', exact: true }).click();
      await activePanel(page).locator('.detail-timeline').waitFor();
      await page.waitForFunction(() => [...document.querySelectorAll('.detail-audio-lane')].filter(element => element.dataset.waveformStatus === 'ready').length === 2);
      await page.waitForFunction(expected => document.querySelectorAll('.detail-subtitle-lane').length === expected, state === 'missing' ? 0 : 3);
      const metrics = await capture(page, `${width}x${height}-implementation-${state}`, activePanel(page).locator('.detail-timeline'), false);
      assert(metrics.aligned, `${width} ${state}: labels and lanes must align.`);
      assert.equal(metrics.labelRows.length, state === 'missing' ? 3 : 6);
      assert.equal(metrics.documentWidth, width);
      if (phase === 'after') {
        assert.equal(metrics.laneRows[0].images.length, 12);
        assert(metrics.laneRows[0].images.every(image => image.loaded));
        assert.equal(metrics.ticksOverlap, false);
        if (width < 720) assert(metrics.head.bubble.bottom < Math.min(...metrics.ticks.map(tick => tick.y)), 'The mobile playhead bubble must sit above all ruler timestamps.');
      }
      if (state !== 'missing') {
        assert.equal(metrics.laneRows[3].color, state === 'selected' ? metrics.laneRows[1].color : metrics.laneRows[2].color);
      }
      await context.close();
    }
  }
} catch (error) {
  failures.push({ phase: 'runner', message: error.stack });
  process.exitCode = 1;
} finally {
  await browser.close(); server.kill('SIGTERM');
  await writeFile(resolve(output, 'results.json'), JSON.stringify({ phase, status: failures.length ? 'failed' : 'passed', fixtureOnly: true, notes: ['Reference is the original rendered handoff HTML, initialized only through supported localStorage state.', 'Images and subtitle coverage are shared through isolated API fixtures.', 'Waveforms use valid GAWL binary data sampled from reference paths; display aggregation may differ.', 'The final fixture uses canonical truehd plus a Dolby Atmos profile and cmn plus a Mandarin display-language label. Historical before screenshots used a combined codec label and chi; compare final text directly with the reference, not with those historical raw metadata values.', 'No production DOM was modified. Missing state intentionally has no matching reference because the design has only populated tracks.', 'Real-media sidecar lifecycle is covered separately by live-subtitle-timelines.mjs.'], records, failures }, null, 2));
  await writeFile(resolve(output, 'fixture-server.log'), serverLog);
  console.log(JSON.stringify({ phase, output, captures: records.length, failures }));
}
