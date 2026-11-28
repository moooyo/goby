import { expect, test, type Page } from '@playwright/test';
import { introInterval, TICKS_PER_SECOND } from '../src/pages/playerUtils';

async function signIn(page: Page) {
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
}

async function openEpisode(page: Page, id = 'deep-2-5') {
  await page.goto(`/#/player/${id}`);
  await page.waitForFunction(() => {
    const video = document.querySelector<HTMLVideoElement>('.player-video');
    return video && video.readyState >= 2 && !video.paused && video.currentTime > 0;
  });
}

test.beforeEach(async ({ page, request }) => {
  await request.post('/__fixture/reset');
  await signIn(page);
});

test('uses the active source duration and hides controls after pointer activity', async ({ page }) => {
  await openEpisode(page);
  const video = page.locator('.player-video');
  const clock = await video.evaluate((element: HTMLVideoElement) => ({ current: element.currentTime, duration: element.duration }));
  expect(clock.current).toBeGreaterThanOrEqual(11.5);
  expect(clock.duration).toBeGreaterThan(29);
  expect(clock.duration).toBeLessThan(32);
  await expect(page.getByRole('slider', { name: '播放进度' })).toHaveAttribute('aria-valuemax', String(Math.round(clock.duration)));
  await page.mouse.move(640, 250);
  await expect(page.locator('.player-page')).toHaveClass(/is-hidden/, { timeout: 5000 });
  await page.keyboard.press('k');
  await expect(page.locator('.player-page')).toHaveClass(/is-visible/);
  await expect.poll(() => video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(true);
});

test('centers the active episode and switches seasons through the drawer', async ({ page }) => {
  await openEpisode(page);
  await page.getByRole('button', { name: '选集', exact: true }).click();
  const drawer = page.getByRole('dialog', { name: '选集' });
  await expect(drawer).toBeVisible();
  const current = drawer.locator('[data-current="true"]');
  await expect(current).toBeVisible();
  await expect(current).toContainText('正在播放');
  await drawer.getByRole('button', { name: '第 1 季', exact: true }).click();
  const first = drawer.locator('.player-episode').first();
  await expect(first).toHaveAttribute('aria-label', /第 1 季第 1 集/);
  await first.click();
  await expect(page).toHaveURL(/#\/player\/deep-1-1$/);
  await expect(page.locator('.player-header')).toContainText('第 1 季第 1 集');
});

test('distinguishes audio tracks with the same title by their codec and index', async ({ page, request }) => {
  await page.route('**/emby/Items/deep-2-5/PlaybackInfo*', async route => {
    const response = await route.fetch();
    const body = await response.json();
    const audio = body.MediaSources[0].MediaStreams.filter((stream: { Type: string }) => stream.Type === 'Audio');
    Object.assign(audio[0], { Title: 'Japanese', Codec: 'dts' });
    Object.assign(audio[1], { Title: 'Japanese', Codec: 'aac' });
    await route.fulfill({ response, json: body });
  });
  await openEpisode(page);
  await page.getByRole('button', { name: '音轨', exact: true }).click();
  await expect(page.getByRole('option', { name: /Japanese DTS/ })).toBeVisible();
  await page.getByRole('option', { name: /Japanese AAC/ }).click();
  await expect.poll(async () => {
    const state = await (await request.get('/__fixture/state')).json();
    return state.events.filter((event: { route: string }) => event.route === '/Sessions/Playing').at(-1)?.AudioStreamIndex;
  }).toBe(2);
});

test('shows the reason that negotiation failed and does not claim an episode is playing', async ({ page }) => {
  let timeout = true;
  await page.route('**/emby/Items/deep-2-5/PlaybackInfo*', route => timeout
    ? route.fulfill({ status: 503, json: { ResponseStatus: { ErrorCode: 'playback_timeout' } } })
    : route.fulfill({ json: { ErrorCode: 'NoCompatibleStream', MediaSources: [] } }));
  await page.goto('/#/player/deep-2-5');
  await expect(page.getByRole('alert')).toContainText('服务器准备播放超时');
  timeout = false;
  await page.getByRole('button', { name: '重试', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('无法为当前浏览器和所选画质、音轨或字幕提供兼容的视频流');
  await page.getByRole('button', { name: '选集', exact: true }).click();
  const current = page.getByRole('dialog', { name: '选集' }).locator('[data-current="true"]');
  await expect(current).toContainText('当前剧集');
  await expect(current).not.toContainText('正在播放');
});

test('uses the same resolution class as the detail page for cropped originals', async ({ page }) => {
  await page.route('**/emby/Items/guitu/PlaybackInfo*', async route => {
    const response = await route.fetch();
    const body = await response.json();
    const video = body.MediaSources[0].MediaStreams.find((stream: { Type: string }) => stream.Type === 'Video');
    Object.assign(video, { Width: 1920, Height: 820 });
    await route.fulfill({ response, json: body });
  });
  await page.goto('/#/player/guitu');
  await expect.poll(() => page.locator('.player-video').evaluate((video: HTMLVideoElement) => video.readyState)).toBeGreaterThanOrEqual(2);
  await page.getByRole('button', { name: '画质', exact: true }).click();
  await expect(page.getByRole('option', { name: /原画 · 1080p/ })).toHaveAttribute('aria-selected', 'true');
});

test('renders a selected text subtitle and removes it when switched off', async ({ page }) => {
  await openEpisode(page);
  await page.getByRole('button', { name: '字幕', exact: true }).click();
  await page.getByRole('option', { name: '英文', exact: true }).click();
  await expect(page.locator('.player-subtitles')).toContainText('又是那个频率。');
  await page.getByRole('button', { name: '字幕', exact: true }).click();
  await page.getByRole('option', { name: '关闭', exact: true }).click();
  await expect.poll(() => page.locator('.player-video').evaluate((video: HTMLVideoElement) => video.readyState >= 2 && !video.paused)).toBe(true);
  await expect(page.locator('.player-subtitles')).toHaveCount(0);
  await expect(page.locator('.player-video track')).toHaveCount(0);
});

test('enters full screen while decoding and returns to the same player', async ({ page }) => {
  await openEpisode(page);
  await page.getByRole('button', { name: '全屏（F）', exact: true }).click();
  await expect.poll(() => page.evaluate(() => document.fullscreenElement?.classList.contains('player-page'))).toBe(true);
  expect(await page.locator('.player-video').evaluate((video: HTMLVideoElement) => video.readyState >= 2 && !video.paused)).toBe(true);
  await page.getByRole('button', { name: '退出全屏（F）', exact: true }).click();
  await expect.poll(() => page.evaluate(() => Boolean(document.fullscreenElement))).toBe(false);
  await expect(page).toHaveURL(/#\/player\/deep-2-5$/);
});

test('keeps stream changes in separate ordered playback sessions', async ({ page, request }) => {
  await openEpisode(page);
  await page.getByRole('button', { name: '音轨', exact: true }).click();
  await page.getByRole('option', { name: '国语 · AC3 5.1' }).click();
  await expect.poll(async () => {
    const state = await (await request.get('/__fixture/state')).json();
    return state.events.filter((event: { route: string }) => event.route === '/Sessions/Playing').length;
  }).toBe(2);
  await page.waitForFunction(() => {
    const video = document.querySelector<HTMLVideoElement>('.player-video');
    return video && !video.paused && video.currentTime > 11;
  });
  await page.getByRole('button', { name: '画质', exact: true }).click();
  await page.getByRole('option', { name: /720p · 8 Mbps/ }).click();
  await expect(page.getByRole('heading', { name: '暂时无法播放', exact: true })).toBeVisible();
  await page.getByRole('button', { name: '选择画质', exact: true }).click();
  await page.getByRole('option', { name: /^原画/ }).click();
  await expect.poll(async () => {
    const state = await (await request.get('/__fixture/state')).json();
    return state.events.filter((event: { route: string }) => event.route === '/Sessions/Playing').length;
  }).toBeGreaterThanOrEqual(3);
  const state = await (await request.get('/__fixture/state')).json();
  const started = state.events.filter((event: { route: string }) => event.route === '/Sessions/Playing');
  expect(new Set(started.map((event: { PlaySessionId: string }) => event.PlaySessionId)).size).toBe(started.length);
  for (const event of started.slice(0, -1)) {
    const stop = state.events.findIndex((entry: { route: string; PlaySessionId: string }) => entry.route === '/Sessions/Playing/Stopped' && entry.PlaySessionId === event.PlaySessionId);
    expect(stop).toBeGreaterThan(state.events.indexOf(event));
    expect(state.events.slice(stop + 1).some((entry: { PlaySessionId: string }) => entry.PlaySessionId === event.PlaySessionId)).toBe(false);
  }
  expect(started.at(-1).PositionTicks).toBeGreaterThan(100_000_000);
  expect(started.at(-1).AudioStreamIndex).toBe(2);
});

test('starts the next episode at the real end and reports the previous completion', async ({ page, request }) => {
  await openEpisode(page);
  await page.locator('.player-video').evaluate((video: HTMLVideoElement) => { video.currentTime = video.duration - 1.2; });
  await expect(page.locator('.player-next')).toContainText('下一集');
  await expect(page).toHaveURL(/#\/player\/deep-2-6$/, { timeout: 12_000 });
  const state = await (await request.get('/__fixture/state')).json();
  const completed = state.events.find((entry: { route: string; ItemId: string }) => entry.route === '/Sessions/Playing/Stopped' && entry.ItemId === 'deep-2-5');
  expect(completed.PositionTicks).toBeGreaterThan(290_000_000);
  expect(completed.PositionTicks).toBeLessThan(320_000_000);
});

test('allows canceling the next episode and keeps mobile controls inside the screen', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await openEpisode(page);
  await page.locator('.player-video').evaluate((video: HTMLVideoElement) => { video.currentTime = video.duration - 7; });
  await page.locator('.player-next').getByRole('button', { name: '取消', exact: true }).click();
  await page.locator('.player-video').evaluate((video: HTMLVideoElement) => { video.currentTime = video.duration - 0.5; });
  await expect(page.getByRole('heading', { name: '播放结束', exact: true })).toBeVisible();
  await expect(page).toHaveURL(/#\/player\/deep-2-5$/);
  const bounds = await page.locator('.player-control-row').evaluate((row) => Array.from(row.querySelectorAll('button, .player-time')).filter((element) => getComputedStyle(element).display !== 'none').map((element) => { const rect = element.getBoundingClientRect(); return { left: rect.left, right: rect.right }; }));
  expect(bounds.every((rect) => rect.left >= 0 && rect.right <= 390)).toBe(true);
  await page.getByRole('button', { name: '更多播放设置', exact: true }).click();
  await expect(page.getByRole('dialog', { name: '播放设置' })).toBeVisible();
  await expect(page.getByRole('slider', { name: '音量', exact: true })).toBeVisible();
});

test('requires source-bound intro markers instead of inventing an interval', () => {
  expect(introInterval([{ StartPositionTicks: 0, Name: 'IntroStart' }], 30)).toBeNull();
  expect(introInterval([{ StartPositionTicks: 12 * TICKS_PER_SECOND, MarkerType: 'IntroStart' }, { StartPositionTicks: 5 * TICKS_PER_SECOND, MarkerType: 'IntroEnd' }], 30)).toBeNull();
  expect(introInterval([{ StartPositionTicks: 5 * TICKS_PER_SECOND, MarkerType: 'IntroStart' }, { StartPositionTicks: 10 * TICKS_PER_SECOND, MarkerType: 'IntroEnd' }], 30)).toEqual({ from: 5, to: 10 });
});
