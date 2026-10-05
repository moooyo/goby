import { expect, test, type Page } from '@playwright/test';
import type { Chapter, GobyCreditsInterval } from '../src/types';
import { creditsIntervals, creditsStart, gobyCreditsIntervals, resolveCreditsIntervals, TICKS_PER_SECOND } from '../src/pages/playerUtils';

const marker = (seconds: number): Chapter => ({ MarkerType: 'CreditsStart', StartPositionTicks: seconds * TICKS_PER_SECOND });
const endMarker = (seconds: number): Chapter => ({ MarkerType: 'CreditsEnd', StartPositionTicks: seconds * TICKS_PER_SECOND });
const interval = (from: number, to: number, Source = 'detected'): GobyCreditsInterval => ({ StartPositionTicks: from * TICKS_PER_SECOND, EndPositionTicks: to * TICKS_PER_SECOND, Source });

async function signIn(page: Page) {
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
}

async function sourceChapters(page: Page, chapters: Chapter[], itemChapters?: Chapter[]) {
  await page.route(/\/Items\/deep-2-5\/PlaybackInfo(?:\?|$)/, async (route) => {
    const response = await route.fetch();
    const body = await response.json();
    body.MediaSources[0].Chapters = chapters;
    delete body.MediaSources[0].GobyCreditsIntervals;
    await route.fulfill({ response, json: body });
  });
  if (itemChapters) {
    await page.route(/\/Users\/[^/]+\/Items\/deep-2-5(?:\?|$)/, async (route) => {
      const response = await route.fetch();
      await route.fulfill({ response, json: { ...await response.json(), Chapters: itemChapters } });
    });
  }
}

async function sourceIntervals(page: Page, intervals: GobyCreditsInterval[], itemIntervals?: GobyCreditsInterval[], itemChapters?: Chapter[]) {
  await page.route(/\/Items\/deep-2-5\/PlaybackInfo(?:\?|$)/, async route => {
    const response = await route.fetch();
    const body = await response.json();
    body.MediaSources[0].GobyCreditsIntervals = intervals;
    // Even a valid legacy start cannot override an authoritative extension array.
    body.MediaSources[0].Chapters = [marker(5)];
    await route.fulfill({ response, json: body });
  });
  if (itemIntervals || itemChapters) await page.route(/\/Users\/[^/]+\/Items\/deep-2-5(?:\?|$)/, async route => {
    const response = await route.fetch();
    await route.fulfill({ response, json: { ...await response.json(), ...(itemIntervals ? { GobyCreditsIntervals: itemIntervals } : {}), ...(itemChapters ? { Chapters: itemChapters } : {}) } });
  });
}

async function openEpisode(page: Page) {
  await page.goto('/#/player/deep-2-5');
  await page.waitForFunction(() => {
    const video = document.querySelector<HTMLVideoElement>('.player-video');
    return video && video.readyState >= 2 && !video.paused && video.currentTime > 0;
  });
}

async function pauseAt(page: Page, seconds: number) {
  await page.locator('.player-video').evaluate((video: HTMLVideoElement, at) => { video.pause(); video.currentTime = at; }, seconds);
  await expect.poll(() => page.locator('.player-video').evaluate((video: HTMLVideoElement) => video.currentTime)).toBeCloseTo(seconds, 1);
}

test('requires one finite credits marker strictly inside the active duration', () => {
  expect(creditsStart(undefined, 30)).toBeNull();
  expect(creditsStart([{ Name: 'CreditsStart', StartPositionTicks: 5 * TICKS_PER_SECOND }], 30)).toBeNull();
  expect(creditsStart([marker(5), marker(5)], 30)).toBeNull();
  for (const seconds of [-1, 30, 31, Number.NaN, Number.POSITIVE_INFINITY]) expect(creditsStart([marker(seconds)], 30)).toBeNull();
  for (const duration of [0, -1, Number.NaN, Number.POSITIVE_INFINITY]) expect(creditsStart([marker(5)], duration)).toBeNull();
  expect(creditsStart([marker(0)], 30)).toBe(0);
  expect(creditsStart([marker(5)], 30)).toBe(5);
});

test('parses ordered credits pairs without merging the scenes between them', () => {
  expect(creditsIntervals([marker(5), endMarker(9), marker(18), endMarker(25)], 30)).toEqual([{ from: 5, to: 9 }, { from: 18, to: 25 }]);
  expect(creditsIntervals([marker(0), { MarkerType: 'Chapter', StartPositionTicks: 2 * TICKS_PER_SECOND }, endMarker(5), marker(5), endMarker(30)], 30)).toEqual([{ from: 0, to: 5 }, { from: 5, to: 30 }]);
  expect(creditsIntervals([marker(5)], 30)).toEqual([{ from: 5, to: 30 }]);
  expect(creditsIntervals([marker(5), endMarker(12)], 30)).toEqual([{ from: 5, to: 12 }]);
  expect(creditsStart([marker(5), endMarker(12)], 30)).toBe(5);
});

test('rejects incomplete, duplicated, reordered, overlapping, and out-of-range credits pairs', () => {
  const invalid = [
    [], [endMarker(9)], [marker(5), marker(18)], [marker(5), endMarker(9), marker(18)],
    [marker(5), endMarker(9), endMarker(25)], [marker(5), marker(5), endMarker(9), endMarker(9)],
    [endMarker(9), marker(5)], [marker(18), endMarker(25), marker(5), endMarker(9)],
    [marker(5), endMarker(19), marker(18), endMarker(25)], [marker(5), endMarker(5)],
    [marker(5), endMarker(4)], [marker(-1), endMarker(9)], [marker(5), endMarker(31)],
    [marker(5), endMarker(Number.NaN)], [marker(5), endMarker(Number.POSITIVE_INFINITY)],
    [{ ...marker(5), StartPositionTicks: .5 }, endMarker(9)],
  ];
  for (const chapters of invalid) expect(creditsIntervals(chapters, 30), JSON.stringify(chapters)).toBeNull();
  for (const duration of [0, -1, Number.NaN, Number.POSITIVE_INFINITY]) expect(creditsIntervals([marker(5), endMarker(9)], duration)).toBeNull();
});

test('validates the Goby interval extension and preserves negotiated-source authority', () => {
  const item = { GobyCreditsIntervals: [interval(5, 9), interval(18, 25)], Chapters: [marker(5)] };
  expect(gobyCreditsIntervals(item.GobyCreditsIntervals, 30)).toEqual([{ from: 5, to: 9 }, { from: 18, to: 25 }]);
  expect(resolveCreditsIntervals(undefined, item, 30)).toEqual([{ from: 5, to: 9 }, { from: 18, to: 25 }]);
  expect(resolveCreditsIntervals({ GobyCreditsIntervals: [], Chapters: [marker(5)] }, item, 30)).toBeNull();
  expect(resolveCreditsIntervals({ Chapters: [] }, item, 30)).toBeNull();
  expect(resolveCreditsIntervals({}, item, 30)).toBeNull();
  expect(resolveCreditsIntervals({ Chapters: [marker(12)] }, item, 30)).toEqual([{ from: 12, to: 30 }]);
  expect(resolveCreditsIntervals({ GobyCreditsIntervals: [interval(5, 9), interval(8, 25)], Chapters: [marker(5)] }, item, 30)).toBeNull();
  expect(resolveCreditsIntervals({ GobyCreditsIntervals: [interval(12, 30, 'manual')] }, item, 30)).toEqual([{ from: 12, to: 30 }]);
  for (const value of [null, [], {}, [interval(5, 9), interval(8, 25)], [interval(18, 25), interval(5, 9)], [interval(5, 9), interval(5, 9)], [interval(5, 5)], [interval(-1, 9)], [interval(5, 31)], [interval(5, Number.NaN)], [interval(5, 9, '')], [{ StartPositionTicks: 5 * TICKS_PER_SECOND, Source: 'detected' }]]) {
    expect(gobyCreditsIntervals(value, 30), JSON.stringify(value)).toBeNull();
  }
});

test.describe('credits playback', () => {
  test.beforeEach(async ({ page, request }) => {
    await request.post('/__fixture/reset');
    await signIn(page);
  });

  test('uses the source marker and pauses, buffers, and resets the countdown', async ({ page }) => {
    await sourceChapters(page, [marker(5)], [marker(25)]);
    await openEpisode(page);
    await pauseAt(page, 1);
    await expect(page.locator('.player-next')).toBeHidden();
    await pauseAt(page, 5);
    const cue = page.locator('.player-next');
    await expect(cue).toContainText('10 秒后自动播放');
    await page.waitForTimeout(1200);
    await expect(cue).toContainText('10 秒后自动播放');
    await page.locator('.player-video').evaluate((video: HTMLVideoElement) => video.play());
    await expect(cue).toContainText('9 秒后自动播放', { timeout: 3000 });
    await page.locator('.player-video').evaluate((video) => video.dispatchEvent(new Event('waiting')));
    const bufferedCountdown = await cue.locator('small').textContent();
    await page.waitForTimeout(1200);
    await expect(cue.locator('small')).toHaveText(bufferedCountdown!);
    await pauseAt(page, 1);
    await expect(cue).toBeHidden();
    await pauseAt(page, 5);
    await expect(cue).toContainText('10 秒后自动播放');
    await expect(page).toHaveURL(/#\/player\/deep-2-5$/);
  });

  test('advances after ten playing seconds and reports the actual credits position', async ({ page, request }) => {
    await sourceChapters(page, [marker(5)]);
    await openEpisode(page);
    await pauseAt(page, 1);
    await expect(page.locator('.player-next')).toBeHidden();
    await pauseAt(page, 5);
    await expect(page.locator('.player-next')).toContainText('10 秒后自动播放');
    await page.locator('.player-video').evaluate((video: HTMLVideoElement) => video.play());
    await expect(page).toHaveURL(/#\/player\/deep-2-6$/, { timeout: 15_000 });
    await expect.poll(async () => {
      const state = await (await request.get('/__fixture/state')).json();
      return state.events.filter((entry: { route: string; ItemId: string }) => entry.route === '/Sessions/Playing/Stopped' && entry.ItemId === 'deep-2-5').length;
    }).toBe(1);
    const state = await (await request.get('/__fixture/state')).json();
    const stopped = state.events.find((entry: { route: string; ItemId: string }) => entry.route === '/Sessions/Playing/Stopped' && entry.ItemId === 'deep-2-5');
    expect(stopped.PositionTicks).toBeGreaterThanOrEqual(14 * TICKS_PER_SECOND);
    expect(stopped.PositionTicks).toBeLessThan(18 * TICKS_PER_SECOND);
    expect(state.events.filter((entry: { route: string; ItemId: string }) => entry.route === '/Sessions/Playing' && entry.ItemId === 'deep-2-6')).toHaveLength(1);
  });

  test('keeps cancellation across seeking and stream replacement until the next episode', async ({ page }) => {
    await sourceChapters(page, [marker(5)]);
    await openEpisode(page);
    await page.locator('.player-next').getByRole('button', { name: '取消', exact: true }).click();
    await pauseAt(page, 1);
    await pauseAt(page, 6);
    await expect(page.locator('.player-next')).toBeHidden();
    await page.mouse.move(640, 250);
    await page.getByRole('button', { name: '音轨', exact: true }).click();
    await page.getByRole('option', { name: '国语 · AC3 5.1' }).click();
    await page.waitForFunction(() => {
      const video = document.querySelector<HTMLVideoElement>('.player-video');
      return video && video.readyState >= 2 && !video.paused && video.currentTime >= 6;
    });
    await expect(page.locator('.player-next')).toBeHidden();
    await page.locator('.player-video').evaluate((video: HTMLVideoElement) => { video.currentTime = video.duration - 0.5; });
    await expect(page.getByRole('heading', { name: '播放结束', exact: true })).toBeVisible();
    await expect(page).toHaveURL(/#\/player\/deep-2-5$/);
    await page.getByRole('button', { name: '下一集', exact: true }).click();
    await expect(page).toHaveURL(/#\/player\/deep-2-6$/);
  });

  test('treats empty source chapters as authoritative and retains end-of-file behavior', async ({ page }) => {
    await sourceChapters(page, [], [marker(5)]);
    await openEpisode(page);
    await pauseAt(page, 8);
    await expect(page.locator('.player-next')).toBeHidden();
    await page.locator('.player-video').evaluate(async (video: HTMLVideoElement) => { video.currentTime = video.duration - 1.2; await video.play(); });
    await expect(page.locator('.player-next')).toContainText('下一集');
    await expect(page).toHaveURL(/#\/player\/deep-2-6$/, { timeout: 5000 });
  });

  test('keeps the credits cue manual when automatic next-episode playback is disabled', async ({ page }) => {
    await page.goto('/#/settings');
    const automatic = page.getByRole('switch', { name: /^自动播放下一集/ });
    await automatic.click();
    await expect(automatic).toHaveAttribute('aria-checked', 'false');
    await sourceChapters(page, [marker(5)]);
    await openEpisode(page);
    await expect(page.locator('.player-next small')).toHaveText('下一集');
    await page.locator('.player-video').evaluate((video: HTMLVideoElement) => { video.currentTime = video.duration - 0.5; });
    await expect.poll(() => page.locator('.player-video').evaluate((video: HTMLVideoElement) => video.ended)).toBe(true);
    await expect(page).toHaveURL(/#\/player\/deep-2-5$/);
    await page.locator('.player-next').getByRole('button', { name: '立即播放', exact: true }).click();
    await expect(page).toHaveURL(/#\/player\/deep-2-6$/);
  });

  test('resets the countdown between credits intervals and hides it throughout the coda', async ({ page }) => {
    await sourceIntervals(page, [interval(5, 9), interval(18, 23)]);
    await openEpisode(page);
    await pauseAt(page, 5);
    const cue = page.locator('.player-next');
    await expect(cue).toContainText('10 秒后自动播放');
    await page.locator('.player-video').evaluate((video: HTMLVideoElement) => video.play());
    await expect(cue).toContainText('9 秒后自动播放', { timeout: 3000 });
    await pauseAt(page, 9);
    await expect(cue).toBeHidden();
    await pauseAt(page, 17);
    await expect(cue).toBeHidden();
    await pauseAt(page, 18);
    await expect(cue).toContainText('10 秒后自动播放');
    await page.locator('.player-video').evaluate((video: HTMLVideoElement) => video.play());
    await expect(cue).toContainText('9 秒后自动播放', { timeout: 3000 });
    // A direct seek to another interval must reset even when inCredits stays true.
    await pauseAt(page, 5);
    await expect(cue).toContainText('10 秒后自动播放');
    await pauseAt(page, 23);
    await expect(cue).toBeHidden();
    await pauseAt(page, 29);
    await expect(cue).toBeHidden();
    await expect(page).toHaveURL(/#\/player\/deep-2-5$/);
  });

  test('advances after ten playing seconds inside the second credits interval', async ({ page }) => {
    await sourceIntervals(page, [interval(2, 5), interval(10, 25)]);
    await openEpisode(page);
    await pauseAt(page, 7);
    await expect(page.locator('.player-next')).toBeHidden();
    await pauseAt(page, 10);
    await expect(page.locator('.player-next')).toContainText('10 秒后自动播放');
    await page.locator('.player-video').evaluate((video: HTMLVideoElement) => video.play());
    await expect(page).toHaveURL(/#\/player\/deep-2-6$/, { timeout: 15_000 });
  });

  test('keeps cancellation effective in every later credits interval of the episode', async ({ page }) => {
    await sourceIntervals(page, [interval(5, 9), interval(18, 25)]);
    await openEpisode(page);
    await pauseAt(page, 5);
    await page.locator('.player-next').getByRole('button', { name: '取消', exact: true }).click();
    await pauseAt(page, 12);
    await pauseAt(page, 18);
    await expect(page.locator('.player-next')).toBeHidden();
    await page.locator('.player-video').evaluate(async (video: HTMLVideoElement) => { video.currentTime = video.duration - .5; await video.play(); });
    await expect(page.getByRole('heading', { name: '播放结束', exact: true })).toBeVisible();
    await expect(page).toHaveURL(/#\/player\/deep-2-5$/);
  });

  test('falls back to the final fifteen seconds when Goby intervals are malformed', async ({ page }) => {
    await sourceIntervals(page, [interval(5, 19), interval(18, 25)]);
    await openEpisode(page);
    await pauseAt(page, 8);
    await expect(page.locator('.player-next')).toBeHidden();
    await pauseAt(page, 20);
    await expect(page.locator('.player-next')).toContainText('下一集');
    await expect(page).toHaveURL(/#\/player\/deep-2-5$/);
  });

  test('treats empty source intervals as authoritative over both legacy and item intervals', async ({ page }) => {
    await sourceIntervals(page, [], [interval(5, 25)], [marker(5)]);
    await openEpisode(page);
    await pauseAt(page, 8);
    await expect(page.locator('.player-next')).toBeHidden();
    await pauseAt(page, 20);
    await expect(page.locator('.player-next')).toContainText('下一集');
  });

  test('uses source legacy chapters when the item extension describes a different source', async ({ page }) => {
    await sourceChapters(page, [marker(12)]);
    await page.route(/\/Users\/[^/]+\/Items\/deep-2-5(?:\?|$)/, async route => {
      const response = await route.fetch();
      await route.fulfill({ response, json: { ...await response.json(), GobyCreditsIntervals: [interval(5, 9)], Chapters: [marker(5)] } });
    });
    await openEpisode(page);
    await pauseAt(page, 6);
    await expect(page.locator('.player-next')).toBeHidden();
    await pauseAt(page, 12);
    await expect(page.locator('.player-next')).toContainText('10 秒后自动播放');
  });
});
