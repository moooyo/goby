import { chromium } from '@playwright/test';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { execFileSync, spawn } from 'node:child_process';

const root = '/opt/goby-test/player-live-20261004-165c';
const base = 'http://127.0.0.1:4225';
const phase = process.env.REVIEW_PHASE ?? 'before';
const output = `${root}/artifacts/other-pages-review-20261005/playback/${phase}`;
await mkdir(output, { recursive: true });
const clip = resolve(import.meta.dirname, '../tests/assets/sample.webm');
execFileSync('ffmpeg', ['-v', 'error', '-y', '-stream_loop', '4', '-i', clip, '-c', 'copy', `${output}/intro-fixture.webm`]);
execFileSync('ffmpeg', ['-v', 'error', '-y', '-ss', '15', '-i', clip, '-frames:v', '1', `${output}/seek-frame.jpg`]);
let extended = false;
let sourceCredits = false;
async function extendedMedia(route) {
  const bytes = await readFile(`${output}/intro-fixture.webm`);
  const range = route.request().headers().range?.match(/^bytes=(\d+)-(\d*)$/);
  if (!range) return route.fulfill({ contentType: 'video/webm', headers: { 'Accept-Ranges': 'bytes' }, body: bytes });
  const start = Number(range[1]);
  const end = range[2] ? Math.min(Number(range[2]), bytes.length - 1) : bytes.length - 1;
  return route.fulfill({ status: 206, contentType: 'video/webm', headers: { 'Accept-Ranges': 'bytes', 'Content-Range': `bytes ${start}-${end}/${bytes.length}` }, body: bytes.subarray(start, end + 1) });
}
const server = spawn(process.execPath, ['tests/fixture-server.mjs'], { cwd: resolve(import.meta.dirname, '..'), env: { ...process.env, GOBY_FIXTURE_PORT: '4225', GOBY_HANDOFF_DIR: `${root}/handoff-v3` }, stdio: 'ignore' });
let browser;
const evidence = { phase, captures: [], errors: [], notes: ['Both players decode the same isolated 30-second WebM fixture. Native media seeking and pause freeze the same frame; production visual DOM is never changed.', 'The prototype alone exposes its existing component state through a mount wrapper for deterministic fixture setup.'] };
try {
  for (let attempt = 0; attempt < 100; attempt++) {
    try { if ((await fetch(`${base}/__fixture/health`)).ok) break; } catch {}
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  browser = await chromium.launch();
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: 'zh-CN', timezoneId: 'Asia/Shanghai', reducedMotion: 'reduce' });
  context.setDefaultTimeout(15000);
  const page = await context.newPage();
  page.on('pageerror', error => evidence.errors.push(error.message));
  await page.route('**/goby-core.js', async route => {
    const response = await route.fetch();
    await route.fulfill({ response, body: `${await response.text()}\n(() => { const originalMount = window.GobyCore.mount; window.GobyCore.mount = function(component, ...args) { window.referenceComponent = component; return originalMount(component, ...args); }; })();` });
  });
  await page.route('https://commondatastorage.googleapis.com/**', async route => extended ? extendedMedia(route) : route.fulfill({ response: await context.request.get(`${base}/__fixture/clip.webm`) }));
  await page.route('**/emby/Videos/*/stream.webm*', async route => extended ? extendedMedia(route) : route.continue());
  await page.route('**/PlaybackInfo*', async route => {
    const response = await route.fetch();
    const info = await response.json();
    if (extended) { info.MediaSources[0].RunTimeTicks = 1500000000; info.MediaSources[0].Chapters = [{ MarkerType: 'IntroStart', StartPositionTicks: 50000000 }, { MarkerType: 'IntroEnd', StartPositionTicks: 105000000 }]; }
    if (sourceCredits) info.MediaSources[0].GobyCreditsIntervals = [{ StartPositionTicks: 200000000, EndPositionTicks: 290000000, Source: 'fixture-reviewed-credits' }];
    await route.fulfill({ response, json: info });
  });
  await page.route('**/Subtitles/*/Stream.vtt*', route => route.fulfill({ contentType: 'text/vtt', body: 'WEBVTT\n\n00:00:06.000 --> 00:00:09.500\n又是那个频率。\n\n00:00:11.000 --> 00:00:14.500\n它在回应我们。\n\n00:00:21.000 --> 00:00:24.000\n我们还有多少时间？\n' }));
  const previewReads = [];
  await page.route('**/ThumbnailSet?*', async route => { previewReads.push(route.request().url()); await route.fulfill({ json: { AspectRatio: 16 / 9, Thumbnails: [{ PositionTicks: 150000000, ImageTag: `goby-preview-400-${'a'.repeat(64)}` }] } }); });
  await page.route('**/Images/Thumbnail?*', async route => route.fulfill({ contentType: 'image/jpeg', body: await readFile(`${output}/seek-frame.jpg`) }));
  await context.request.post(`${base}/__fixture/reset`);
  const fixture = await (await context.request.get(`${base}/__fixture/state`)).json();
  fixture.items.find(item => item.Id === 'deep-2-5').UserData.PlaybackPositionTicks = 0;
  await context.request.post(`${base}/__fixture/config`, { data: { items: fixture.items } });
  await page.goto(base);
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await page.getByRole('navigation', { name: '主导航' }).waitFor();

  async function resetPosition() {
    const state = await (await context.request.get(`${base}/__fixture/state`)).json();
    state.items.find(item => item.Id === 'deep-2-5').UserData.PlaybackPositionTicks = 0;
    await context.request.post(`${base}/__fixture/config`, { data: { items: state.items } });
  }

  async function pauseAt(time, reference) {
    await page.evaluate(async ({ time, reference }) => {
      const video = reference ? window.referenceComponent._v : document.querySelector('.player-video');
      video.pause();
      video.currentTime = time;
      if (video.seeking) await new Promise(resolve => video.addEventListener('seeked', resolve, { once: true }));
      if (reference) {
        const component = window.referenceComponent;
        clearTimeout(component._uiT);
        component.setState({ p: { ...component.state.p, t: time, playing: false, ui: true, resumeTip: false, sub: 1, vol: .8 } });
      }
    }, { time, reference });
    await page.waitForTimeout(180);
    await page.mouse.move(1, 1);
  }
  async function capture(name, reference) {
    await page.evaluate(() => document.fonts.ready);
    await page.waitForTimeout(200);
    const state = await page.evaluate(reference => {
      const video = reference ? window.referenceComponent._v : document.querySelector('.player-video');
      const elements = reference ? [...document.querySelectorAll('[data-screen-label="播放器"] button')].filter(el => el.getBoundingClientRect().width && getComputedStyle(el).visibility !== 'hidden') : [...document.querySelectorAll('.player-page button')].filter(el => el.getBoundingClientRect().width);
      return { video: { time: video?.currentTime, duration: video?.duration, paused: video?.paused, width: video?.videoWidth, height: video?.videoHeight }, viewport: { width: innerWidth, height: innerHeight }, controls: elements.map(el => ({ label: el.getAttribute('aria-label') || el.title || el.textContent.trim(), x: el.getBoundingClientRect().x, y: el.getBoundingClientRect().y, width: el.getBoundingClientRect().width, height: el.getBoundingClientRect().height })), referenceState: reference ? window.referenceComponent.state.p : undefined };
    }, reference);
    evidence.captures.push({ name, ...state });
    await page.screenshot({ path: `${output}/${name}.png` });
  }
  for (const reference of (process.env.REVIEW_EXTRA_ONLY ? [] : process.env.REVIEW_TARGET === 'react' ? [false] : [true, false])) {
    const prefix = reference ? 'reference' : 'react';
    for (const [width, height] of [[1440, 900], [393, 852], [375, 852], [844, 390]].filter(([width]) => !process.env.REVIEW_LAYOUT_ONLY || width !== 375)) {
      console.log(`Capturing ${prefix} at ${width}`);
      await page.setViewportSize({ width, height });
      if (reference) {
        await page.goto(`${base}/reference/`);
        await page.waitForFunction(() => window.referenceComponent?.state.ready);
        await page.evaluate(() => {
          const component = window.referenceComponent;
          component.setState({ route: { name: 'detail', id: 'deep' } });
        });
        await page.evaluate(() => window.GobyCore.vals(window.referenceComponent, 'A').d.play());
      } else {
        await page.goto(`${base}/#/home`);
        await page.getByRole('navigation', { name: '主导航' }).waitFor();
        await resetPosition();
        await page.goto(`${base}/#/player/deep-2-5`);
      }
      try { await page.waitForFunction(reference => {
        const video = reference ? window.referenceComponent?._v : document.querySelector('.player-video');
        return video && video.readyState >= 2 && Number.isFinite(video.duration);
      }, reference); } catch (error) { console.log(await page.locator('body').innerText()); await page.screenshot({ path: `${output}/${prefix}-${width}-error.png` }); throw error; }
      await pauseAt(12, reference);
      await capture(`${prefix}-${width}-controls`, reference);
      if (width === 1440 && !process.env.REVIEW_LAYOUT_ONLY) {
        if (reference) {
          await page.evaluate(() => { const component = window.referenceComponent; const bounds = component._track.getBoundingClientRect(); component._pl.trackMove({ clientX: bounds.x + bounds.width * .6 }); });
        } else {
          const bounds = await page.getByRole('slider', { name: '播放进度' }).boundingBox();
          await page.mouse.move(bounds.x + bounds.width * .6, bounds.y + bounds.height / 2);
        }
        await page.waitForTimeout(180);
        await capture(`${prefix}-${width}-seek-preview`, reference);
        await page.mouse.move(0, 0);
        if (reference) await page.evaluate(() => window.referenceComponent._pl.trackLeave());
      }
      for (const [panel, refTitle, label] of [['subtitles', '字幕 (C)', '字幕'], ['audio', '音轨', '音轨'], ['quality', '画质', '画质'], ['rate', '播放速度', '播放速度'], ['episodes', '选集', '选集']]) {
        if (process.env.REVIEW_LAYOUT_ONLY && (width !== 844 || panel !== 'episodes')) continue;
        if (width < 720 && (panel === 'audio' || panel === 'rate')) {
          if (reference) continue;
          await page.getByRole('button', { name: '更多播放设置', exact: true }).click();
        }
        if (reference) await page.getByTitle(refTitle, { exact: true }).click();
        else await page.getByRole('button', { name: panel === 'rate' && width < 720 ? /^播放速度/ : label, exact: true }).click();
        await page.waitForTimeout(350);
        await capture(`${prefix}-${width}-${panel}`, reference);
        if (panel === 'episodes') {
          await page.getByRole('button', { name: '第 1 季', exact: true }).click();
          await page.waitForTimeout(300);
          await capture(`${prefix}-${width}-season-one`, reference);
        }
        await page.keyboard.press('Escape');
      }
      await pauseAt(22, reference);
      await capture(`${prefix}-${width}-next`, reference);
      await page.getByRole('button', { name: '取消', exact: true }).click();
      await capture(`${prefix}-${width}-next-cancelled`, reference);
      if (!reference && process.env.REVIEW_LAYOUT_ONLY && width !== 844) {
        await pauseAt(10, false);
        await page.locator('.player-video').click({ position: { x: 100, y: 200 } });
        await page.mouse.move(0, 0);
        await page.waitForTimeout(3200);
        await capture(`${prefix}-${width}-auto-hidden-subtitle`, false);
        evidence.hiddenSubtitles ??= [];
        evidence.hiddenSubtitles.push(await page.locator('.player-subtitles').evaluate(element => ({ width: innerWidth, text: element.textContent, bottom: innerHeight - element.getBoundingClientRect().bottom, controlsHidden: document.querySelector('.player-page').classList.contains('is-hidden') })));
      }
      if (!reference && width === 1440 && !process.env.REVIEW_LAYOUT_ONLY) {
        await page.getByRole('button', { name: '选集', exact: true }).click();
        await page.waitForTimeout(150);
        const first = await page.evaluate(() => document.activeElement?.getAttribute('aria-label') || document.activeElement?.textContent.trim());
        await page.getByRole('button', { name: '第 1 季', exact: true }).focus();
        await page.keyboard.press('Shift+Tab');
        const trapped = await page.evaluate(() => Boolean(document.activeElement?.closest('.player-episodes')));
        let arrowEndTrapped;
        const nextArrow = page.locator('.player-episodes-arrow.is-next');
        if (await nextArrow.count()) {
          for (let step = 0; step < 8 && await nextArrow.isEnabled(); step++) { await nextArrow.click(); await page.waitForTimeout(120); }
          await page.keyboard.press('Tab');
          arrowEndTrapped = await page.evaluate(() => Boolean(document.activeElement?.closest('.player-episodes')));
        }
        await page.keyboard.press('Escape');
        await pauseAt(12, false);
        await page.locator('.player-video').click({ position: { x: 100, y: 200 } });
        await page.mouse.move(0, 0);
        await page.waitForTimeout(3200);
        const hidden = await page.locator('.player-page').getAttribute('class');
        await capture(`${prefix}-${width}-auto-hidden`, false);
        await page.keyboard.press('Tab');
        await page.waitForTimeout(80);
        const recovered = await page.evaluate(() => ({ visible: document.querySelector('.player-page').classList.contains('is-visible'), focusedControl: Boolean(document.activeElement?.closest('.player-interface')) }));
        evidence.keyboard = { first, trapped, arrowEndTrapped, hidden, recovered };
        await pauseAt(29.4, false);
        await page.locator('.player-video').evaluate(video => video.play());
        await page.waitForTimeout(900);
        evidence.cancelAtEnd = { url: page.url(), ended: await page.locator('.player-ended').isVisible() };
      }
    }
  }
  if (!process.env.REVIEW_LAYOUT_ONLY) {
  extended = true;
  evidence.skip = [];
  for (const reference of [true, false]) {
    for (const width of [1440, 393]) {
      await page.setViewportSize({ width, height: width === 1440 ? 900 : 852 });
      if (reference) {
        await page.goto(`${base}/reference/`);
        await page.waitForFunction(() => window.referenceComponent?.state.ready);
        await page.evaluate(() => window.referenceComponent.setState({ route: { name: 'detail', id: 'deep' } }));
        await page.evaluate(() => window.GobyCore.vals(window.referenceComponent, 'A').d.play());
      } else {
        await page.goto(`${base}/#/home`);
        await page.getByRole('navigation', { name: '主导航' }).waitFor();
        await resetPosition();
        await page.goto(`${base}/#/player/deep-2-5`);
      }
      await page.waitForFunction(reference => {
        const video = reference ? window.referenceComponent?._v : document.querySelector('.player-video');
        return video?.duration > 120 && video.readyState >= 2 && !video.paused;
      }, reference);
      await pauseAt(7, reference);
      await capture(`${reference ? 'reference' : 'react'}-${width}-skip-intro`, reference);
      await page.getByRole('button', { name: '跳过片头', exact: true }).click();
      await page.waitForTimeout(200);
      evidence.skip.push({ reference, width, position: await page.evaluate(reference => (reference ? window.referenceComponent._v : document.querySelector('.player-video')).currentTime, reference) });
    }
  }
  evidence.previewReads = previewReads;
  extended = false;
  sourceCredits = true;
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto(`${base}/#/home`);
  await page.getByRole('navigation', { name: '主导航' }).waitFor();
  await resetPosition();
  await page.goto(`${base}/#/player/deep-2-5`);
  await page.waitForFunction(() => document.querySelector('.player-video')?.readyState >= 2);
  await pauseAt(22, false);
  const frozenCountdown = await page.locator('.player-next-body small').innerText();
  await page.waitForTimeout(1200);
  evidence.countdown = { frozenCountdown, afterPause: await page.locator('.player-next-body small').innerText() };
  await capture('react-1440-source-credits-paused', false);
  await page.locator('.player-video').evaluate(video => video.play());
  await page.waitForTimeout(1300);
  evidence.countdown.afterPlaying = await page.locator('.player-next-body small').innerText();
  await page.locator('.player-video').evaluate(video => video.pause());
  await capture('react-1440-source-credits-running', false);
  await page.getByRole('button', { name: '取消', exact: true }).click();
  await page.goto(`${base}/#/home`);
  const manualState = await (await context.request.get(`${base}/__fixture/state`)).json();
  manualState.user.Configuration.EnableNextEpisodeAutoPlay = false;
  await context.request.post(`${base}/__fixture/config`, { data: { user: manualState.user } });
  await page.reload();
  await page.getByRole('navigation', { name: '主导航' }).waitFor();
  await resetPosition();
  await page.goto(`${base}/#/player/deep-2-5`);
  await page.waitForFunction(() => document.querySelector('.player-video')?.readyState >= 2);
  await pauseAt(22, false);
  await capture('react-1440-manual-next', false);
  evidence.manualNext = { hint: await page.locator('.player-next-body small').innerText() };
  await page.getByRole('button', { name: '立即播放', exact: true }).click();
  await page.waitForURL('**/#/player/deep-2-6*');
  await page.waitForFunction(() => document.querySelector('.player-video')?.readyState >= 2);
  evidence.manualNext.destination = page.url();
  await page.setViewportSize({ width: 320, height: 852 });
  await pauseAt(12, false);
  await capture('react-320-controls', false);
  evidence.narrowControls = await page.locator('.player-control-row').evaluate(row => [...row.children].filter(element => element.getBoundingClientRect().width > 0).map(element => ({ label: element.getAttribute('aria-label') || element.textContent, right: element.getBoundingClientRect().right, withinViewport: element.getBoundingClientRect().right <= innerWidth })));
  const endState = await (await context.request.get(`${base}/__fixture/state`)).json();
  evidence.sessionEvents = endState.events.map(event => ({ route: event.route, ItemId: event.ItemId, MediaSourceId: event.MediaSourceId, PlaySessionId: event.PlaySessionId, PositionTicks: event.PositionTicks }));
  }
} finally {
  await writeFile(`${output}/evidence.json`, JSON.stringify(evidence, null, 2));
  await browser?.close();
  server.kill('SIGTERM');
}
