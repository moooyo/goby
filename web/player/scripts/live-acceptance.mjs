import { chromium } from '@playwright/test';
import assert from 'node:assert/strict';
import { readFile, mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { execFileSync } from 'node:child_process';

const root = process.env.GOBY_LIVE_ROOT;
assert(root, 'Provide the explicitly owned remote acceptance root.');
const config = JSON.parse(await readFile(resolve(root, 'browser.private.json'), 'utf8'));
const output = resolve(root, 'artifacts/live-browser');
await mkdir(output, { recursive: true });
const results = [];
const failures = [];
const negotiations = [];
const reports = [];
const mediaRequests = [];
const browser = await chromium.launch({ headless: true, args: ['--no-sandbox'] });
const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: 'zh-CN', reducedMotion: 'reduce' });
const page = await context.newPage();
page.on('pageerror', error => failures.push({ kind: 'page', message: error.message }));
page.on('response', async response => {
  const path = new URL(response.url()).pathname;
  if (response.status() >= 400 && path.startsWith('/emby')) failures.push({ kind: 'http', path, status: response.status() });
  if (/PlaybackInfo$/.test(path) && response.ok()) {
    const value = await response.json().catch(() => ({}));
    negotiations.push({ path, session: value.PlaySessionId, request: response.request().postDataJSON(), sources: value.MediaSources?.map(source => ({
      id: source.Id, direct: source.SupportsDirectPlay, stream: source.SupportsDirectStream, transcode: source.SupportsTranscoding,
      audio: source.DefaultAudioStreamIndex, subtitle: source.DefaultSubtitleStreamIndex,
      path: (source.TranscodingUrl || source.DirectStreamUrl || '').split('?')[0],
    })) });
  }
  if (/\/Sessions\/Playing/.test(path)) reports.push({ path, status: response.status(), ...response.request().postDataJSON() });
  if (/\.m3u8$|\.ts$|\.m4s$|\/Subtitles\//.test(path)) mediaRequests.push({ path, status: response.status() });
});

async function api(path) {
  const response = await context.request.get(config.baseURL + path, { headers: { 'X-Emby-Token': config.token } });
  assert(response.ok(), `API read failed: ${path} (${response.status()})`);
  return response.json();
}
async function phase(name, action) {
  await action();
  results.push({ name, passed: true });
  console.log(JSON.stringify({ phase: name, passed: true }));
  await writeFile(resolve(output, 'results.json'), JSON.stringify({ results, failures, negotiations, reports, mediaRequests }, null, 2));
}
async function playing(minimum = 0) {
  await page.waitForFunction(value => {
    const video = document.querySelector('.player-video');
    return Boolean(document.querySelector('.player-error')) || video && video.readyState >= 2 && !video.paused && video.currentTime > value;
  }, minimum, { timeout: 90_000 });
  if (await page.locator('.player-error').count()) throw new Error(await page.locator('.player-error p').innerText());
}
async function shot(name) {
  await page.screenshot({ path: resolve(output, `${name}.png`), fullPage: true });
}
async function waitFor(check, message, timeout = 20_000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    if (await check()) return;
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  throw new Error(message);
}

try {
  const catalog = await api('/emby/Items?IncludeItemTypes=Movie,Series,Episode&Recursive=true');
  const movie = catalog.Items.find(item => item.Type === 'Movie' && item.Name === 'Player Integration');
  const series = catalog.Items.find(item => item.Type === 'Series' && item.Name === 'Player Series');
  assert(movie && series, 'The real scanned catalogue must contain movie and series items.');
  await phase('login and real catalogue', async () => {
    await page.goto(config.baseURL);
    await page.getByLabel('用户名', { exact: true }).fill(config.username);
    await page.getByLabel('密码', { exact: true }).fill(config.password);
    await page.getByRole('button', { name: '登录', exact: true }).click();
    await page.getByRole('navigation', { name: '主导航' }).waitFor();
    await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '电影', exact: true }).click();
    await page.locator('.poster-card').filter({ hasText: movie.Name }).first().click();
    await page.locator('.detail-overview-info h1').waitFor();
    await page.getByTitle('选择画质', { exact: true }).click();
    await page.getByRole('menuitemradio').filter({ hasText: /^原画/ }).click();
    await page.getByTitle('选择音轨', { exact: true }).click();
    await page.getByRole('menuitemradio').filter({ hasText: /英语/ }).click();
    await page.getByTitle('选择字幕', { exact: true }).click();
    await page.getByRole('menuitemradio', { name: /^关闭/ }).click();
    await shot('real-detail');
  });
  await phase('original stream and decoded video', async () => {
    await page.mouse.move(720, 360);
    if (await page.locator('.restart-button').count()) await page.locator('.restart-button').click();
    else await page.locator('.deck-panel[data-active=true] .play-disc').click();
    await playing(1);
    const dimensions = await page.locator('.player-video').evaluate(video => ({ width: video.videoWidth, height: video.videoHeight, duration: video.duration }));
    assert.equal(dimensions.width, 1920);
    assert(dimensions.duration >= 179);
    assert(negotiations.at(-1).sources.some(source => source.direct));
    await shot('direct-play');
  });
  await phase('HLS video conversion and subtitle rendering', async () => {
    await page.mouse.move(700, 810);
    await page.getByRole('button', { name: '画质', exact: true }).click();
    await page.getByRole('option', { name: /720p · 8 Mbps/ }).click();
    await playing(2);
    await page.waitForFunction(() => document.querySelector('.player-video')?.videoWidth <= 1280, null, { timeout: 90_000 });
    assert(negotiations.at(-1).sources.some(source => source.transcode && /m3u8/.test(source.path)));
    assert(mediaRequests.some(request => /\.ts$|\.m4s$/.test(request.path) && request.status === 200));
    await page.mouse.move(700, 810);
    await page.getByRole('button', { name: '字幕', exact: true }).click();
    const options = await page.getByRole('option').allTextContents();
    const option = options.find(text => /英|eng|English/i.test(text));
    assert(option, 'Expected a real indexed English subtitle.');
    await page.getByRole('option').filter({ hasText: option }).click();
    await playing(2);
    await page.getByText('Real server subtitle, first window.', { exact: true }).waitFor({ timeout: 35_000 });
    await shot('hls-subtitles');
  });
  let resumePosition;
  await phase('HLS seek, audio selection and persisted progress', async () => {
    await page.mouse.move(700, 810);
    const slider = page.getByRole('slider', { name: '播放进度', exact: true });
    await slider.focus();
    await page.keyboard.press('Home');
    await page.keyboard.press('ArrowRight');
    await page.keyboard.press('ArrowRight');
    await page.keyboard.press('ArrowRight');
    await playing(29);
    await page.getByText('Real server subtitle, second window.', { exact: true }).waitFor({ timeout: 35_000 });
    await page.mouse.move(700, 810);
    await page.getByRole('button', { name: '音轨', exact: true }).click();
    await page.getByRole('option').filter({ hasText: /中文|国语/ }).click();
    await playing(29);
    await waitFor(() => reports.some(report => /Progress$/.test(report.path) && report.AudioStreamIndex === 2 && report.PositionTicks > 290_000_000), 'Audio/progress reporting did not persist.');
    await page.keyboard.press('Escape');
    await page.locator('.detail-overview-info').waitFor();
    await waitFor(async () => {
      const detail = await api(`/emby/Users/${config.userId}/Items/${movie.Id}`);
      resumePosition = detail.UserData.PlaybackPositionTicks;
      return resumePosition >= 290_000_000;
    }, 'Stopped playback did not preserve a resume position.');
    const resume = await api(`/emby/Users/${config.userId}/Items/Resume`);
    assert(resume.Items.some(item => item.Id === movie.Id));
    await waitFor(() => !execFileSync('docker', ['top', 'goby-player-165c-goby-1', '-eo', 'pid,comm'], { encoding: 'utf8' }).split('\n').some(line => /\bffmpeg$/.test(line.trim())), 'FFmpeg remained active after stopping.', 30_000);
  });
  await phase('resume from server state', async () => {
    await page.reload();
    await page.locator('.detail-overview-info').waitFor();
    await page.mouse.move(720, 360);
    await page.locator('.deck-panel[data-active=true] .play-disc').click();
    await playing(resumePosition / 10_000_000 - 1);
    assert(negotiations.at(-1).request.StartTimeTicks >= resumePosition);
    await page.keyboard.press('Escape');
    await page.locator('.detail-overview-info').waitFor();
  });
  await phase('episode drawer and session retirement', async () => {
    await page.goto(`${config.baseURL}/#/detail/${series.Id}`);
    await page.locator('.detail-overview-info').waitFor();
    await page.mouse.move(720, 360);
    await page.locator('.deck-panel[data-active=true] .play-disc').click();
    await playing(1);
    await page.mouse.move(700, 810);
    await page.getByRole('button', { name: '选集', exact: true }).click();
    await page.locator('.player-episode').last().click();
    await playing(1);
    await page.keyboard.press('Escape');
    await page.locator('.detail-overview-info').waitFor();
    await waitFor(() => reports.filter(report => /Stopped$/.test(report.path)).length >= 5, 'Missing terminal playback reports.');
    assert(reports.every(report => report.status < 400));
    await waitFor(() => !execFileSync('docker', ['top', 'goby-player-165c-goby-1', '-eo', 'pid,comm'], { encoding: 'utf8' }).split('\n').some(line => /\bffmpeg$/.test(line.trim())), 'FFmpeg remained active after episode exit.', 30_000);
  });
  assert.deepEqual(failures, []);
  console.log(JSON.stringify({ complete: true, phases: results.length, realBackend: true, docker: true }));
} catch (error) {
  await shot('failure').catch(() => undefined);
  await writeFile(resolve(output, 'failure.json'), JSON.stringify({ message: error.message, results, failures, negotiations, reports, mediaRequests }, null, 2));
  console.error(error.message);
  process.exitCode = 1;
} finally {
  await context.close();
  await browser.close();
}
