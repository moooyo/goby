import { test, expect, type Page, type APIRequestContext } from '@playwright/test';
import { resolve } from 'node:path';

const artifacts = resolve(import.meta.dirname, '../../../.artifacts/player-acceptance');
const activePanel = (page: Page) => page.locator('.deck-panel[data-active="true"]');

async function login(page: Page) {
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
  await expect(activePanel(page).getByRole('heading', { name: '深海回响', exact: true })).toBeVisible();
}

async function navigate(page: Page, name: string) {
  const scope = ['个人中心', '搜索'].includes(name) ? page : page.getByRole('navigation', { name: '主导航' });
  await scope.getByRole('button', { name, exact: true }).click();
}

async function state(request: APIRequestContext) {
  return (await request.get('/__fixture/state')).json();
}

async function screenshot(page: Page, name: string) {
  await page.evaluate(async () => { await document.fonts.ready; });
  // Stage transitions deliberately lock paging for 1,050 ms in the handoff.
  await page.waitForTimeout(1100);
  await page.evaluate(async () => {
    await Promise.all(Array.from(document.images).filter((image) => image.getBoundingClientRect().width > 0).map((image) => image.complete ? Promise.resolve() : new Promise<void>((resolve) => { image.addEventListener('load', () => resolve(), { once: true }); image.addEventListener('error', () => resolve(), { once: true }); setTimeout(resolve, 10_000); })));
  });
  await page.screenshot({ path: resolve(artifacts, `${name}.png`), fullPage: true, animations: 'disabled' });
}

async function noOverflow(page: Page) {
  const width = await page.evaluate(() => ({ viewport: innerWidth, content: document.documentElement.scrollWidth, body: document.body.scrollWidth }));
  expect(width.content, JSON.stringify(width)).toBeLessThanOrEqual(width.viewport);
  expect(width.body, JSON.stringify(width)).toBeLessThanOrEqual(width.viewport);
}

test.beforeEach(async ({ request }) => {
  await request.post('/__fixture/reset');
});

test.afterEach(async ({ request }, testInfo) => {
  const snapshot = await state(request);
  await testInfo.attach('fixture-api-audit', { body: JSON.stringify({ unknownRequests: snapshot.unknownRequests, requests: snapshot.requests, events: snapshot.events }, null, 2), contentType: 'application/json' });
  expect(snapshot.unknownRequests).toEqual([]);
});

test('login rejects incorrect credentials and restores an authenticated session', async ({ page, request }) => {
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('incorrect');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('登录失败');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(activePanel(page).getByRole('heading', { name: '深海回响', exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
  expect((await state(request)).requests.filter((entry: { path: string }) => entry.path === '/Users/AuthenticateByName')).toHaveLength(2);
});

test('home pages respond to keyboard and show a recoverable server error', async ({ page, request }) => {
  await login(page);
  await expect(activePanel(page).getByRole('button', { name: '播放', exact: true })).toHaveCount(0);
  await page.waitForTimeout(1100);
  await page.locator('body').click({ position: { x: 12, y: 100 } });
  await page.keyboard.press('PageDown');
  await expect(activePanel(page)).toHaveAttribute('aria-label', '继续观看');
  await page.waitForTimeout(1100);
  await page.keyboard.press('End');
  await expect(activePanel(page)).toHaveAttribute('aria-label', '最新剧集');
  await page.waitForTimeout(1100);
  await page.keyboard.press('Home');
  await expect(activePanel(page)).toHaveAttribute('aria-label', '精选');
  await screenshot(page, 'home-desktop');
  await request.post('/__fixture/config', { data: { failures: { '/Users/fixture-viewer/Items/Latest': 503 } } });
  await page.reload();
  await expect(page.getByText('无法加载片库，请检查服务器连接后重试。')).toBeVisible();
  await request.post('/__fixture/config', { data: { failures: {} } });
  await page.getByRole('button', { name: /重试|重新加载/ }).click();
  await expect(activePanel(page).getByRole('heading', { name: '深海回响', exact: true })).toBeVisible();
});

test('forbidden catalogue requests show an error and expired sessions return to login', async ({ page, request }) => {
  await login(page);
  await request.post('/__fixture/config', { data: { failures: { '/Users/fixture-viewer/Items': 403 } } });
  await navigate(page, '电影');
  await expect(page.getByRole('alert')).toBeVisible();
  await expect(page.getByText('没有符合条件的影片', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
  await request.post('/__fixture/config', { data: { failures: { '/Users/fixture-viewer/Items': 401 } } });
  await page.getByRole('button', { name: '重新加载', exact: true }).click();
  await expect(page.getByRole('button', { name: '登录', exact: true })).toBeVisible();
  expect(await page.evaluate(() => localStorage.getItem('goby.player.session.v1'))).toBeNull();
});

test('an empty library exposes a useful empty state and can refresh', async ({ page, request }) => {
  await login(page);
  await request.post('/__fixture/config', { data: { empty: true } });
  await page.reload();
  await expect(page.getByRole('heading', { name: '片库还空着', exact: true })).toBeVisible();
  await request.post('/__fixture/config', { data: { empty: false } });
  await page.getByRole('button', { name: '刷新片库', exact: true }).click();
  await expect(activePanel(page).getByRole('heading', { name: '深海回响', exact: true })).toBeVisible();
});

test('catalogue applies server library, genre, sorting and unfinished filters', async ({ page, request }) => {
  await login(page);
  await navigate(page, '电影');
  await expect(page.locator('.catalog-count')).toContainText('19 部影片');
  await expect(page.locator('.poster-card')).toHaveCount(19);
  await screenshot(page, 'movies-desktop');
  await page.getByLabel('媒体库', { exact: true }).getByRole('button', { name: '华语电影', exact: true }).click();
  await expect(page.locator('.catalog-count')).toContainText('7 部影片');
  await page.getByLabel('类型筛选', { exact: true }).getByRole('button', { name: '剧情', exact: true }).click();
  await expect.poll(async () => (await state(request)).requests.some((entry: { query: Record<string, string> }) => entry.query?.Genres === '剧情' && entry.query?.ParentId === 'cn')).toBeTruthy();
  await page.getByRole('button', { name: '排序', exact: true }).click();
  await page.getByRole('menuitemradio', { name: '评分', exact: true }).click();
  await expect(page.locator('.poster-card').first()).toContainText('最后的灯塔');
  await page.getByRole('button', { name: '筛选', exact: true }).click();
  await expect(page.getByRole('button', { name: '4K', exact: true })).toBeEnabled();
  await page.getByRole('switch', { name: '只看未看完', exact: true }).click();
  await expect.poll(async () => (await state(request)).requests.some((entry: { query: Record<string, string> }) => entry.query?.IsPlayed === 'false')).toBeTruthy();
  await expect(page.locator('.poster-card').filter({ hasText: '最后的灯塔' })).toHaveCount(0);
  await navigate(page, '剧集');
  await expect(page.locator('.catalog-count')).toContainText('7 部剧集');
  await navigate(page, '电影');
  await expect(page.getByLabel('媒体库', { exact: true }).getByRole('button', { name: '华语电影' })).toHaveAttribute('aria-pressed', 'true');
});

test('favorite and played controls persist through API and reload', async ({ page, request }) => {
  await login(page);
  await page.goto('/#/detail/guitu');
  await expect(activePanel(page).getByRole('heading', { name: '归途', exact: true })).toBeVisible();
  const favorite = activePanel(page).getByRole('button', { name: '收藏', exact: true });
  await favorite.click();
  await expect.poll(async () => (await state(request)).items.find((item: { Id: string }) => item.Id === 'guitu').UserData.IsFavorite).toBe(true);
  await navigate(page, '收藏');
  await expect(page.locator('.poster-card').filter({ hasText: '归途' })).toBeVisible();
  await screenshot(page, 'favorites-desktop');
  await page.reload();
  await expect(page.locator('.poster-card').filter({ hasText: '归途' })).toBeVisible();
});

test('search returns titles and offers explicit actor, year and genre queries', async ({ page, request }) => {
  await login(page);
  await page.keyboard.press('/');
  const search = page.getByRole('textbox', { name: /搜索片名/ });
  await search.fill('深海');
  await expect(page.locator('.poster-card')).toHaveCount(1);
  await expect(page.locator('.poster-card')).toContainText('深海回响');
  await search.fill('周屿');
  await expect(page.getByRole('button', { name: /周屿/ }).first()).toBeVisible();
  await page.getByRole('button', { name: /周屿/ }).first().click();
  await expect.poll(async () => (await state(request)).requests.some((entry: { query: Record<string, string> }) => entry.query?.PersonIds === 'person-周屿')).toBeTruthy();
  await expect(page.locator('.poster-card').filter({ hasText: '深海回响' })).toBeVisible();
  await search.fill('2024');
  await page.getByRole('button', { name: '浏览 2024 年影片', exact: true }).click();
  await expect(page.locator('.catalog-search-count')).toContainText('2024 年');
  await expect.poll(async () => (await state(request)).requests.some((entry: { query: Record<string, string> }) => entry.query?.Years === '2024')).toBeTruthy();
  await search.fill('悬疑');
  await page.locator('.catalog-people').getByRole('button', { name: '悬疑，类型影片', exact: true }).click();
  await expect(page.locator('.catalog-search-count')).toContainText('悬疑');
  await expect.poll(async () => (await state(request)).requests.some((entry: { query: Record<string, string> }) => entry.query?.GenreIds === 'genre-悬疑' && !entry.query?.SearchTerm)).toBeTruthy();
  await search.fill('not-a-film-xyz');
  await expect(page.getByRole('heading', { name: '没有找到“not-a-film-xyz”', exact: true })).toBeVisible();
  await expect(page.locator('.poster-card')).toHaveCount(0);
  await search.fill('深海');
  await expect(page.locator('.poster-card')).toHaveCount(1);
  await screenshot(page, 'search-desktop');
});

test('series details expose seasons, seen state, people and real media metadata', async ({ page, request }) => {
  await login(page);
  await page.goto('/#/detail/deep');
  await expect(activePanel(page).getByRole('heading', { name: '深海回响', exact: true })).toBeVisible();
  await screenshot(page, 'detail-desktop');
  await page.getByRole('navigation', { name: '页面分页' }).getByRole('button', { name: '剧集', exact: true }).click();
  await expect(activePanel(page)).toHaveAttribute('aria-label', '剧集');
  await expect(page.getByRole('tablist')).toBeVisible();
  await page.waitForTimeout(1100);
  await page.getByRole('tab', { name: '第 1 季', exact: true }).click();
  await expect(page.getByRole('button', { name: '播放第 1 集 回声号', exact: true })).toBeVisible();
  await page.getByRole('button', { name: '将第 1 集标记为未看', exact: true }).click();
  await expect.poll(async () => (await state(request)).items.find((item: { Id: string }) => item.Id === 'deep-1-1').UserData.Played).toBe(false);
  await screenshot(page, 'episodes-desktop');
  await page.mouse.move(0, 0);
  await page.waitForTimeout(1100);
  await page.getByRole('navigation', { name: '页面分页' }).getByRole('button', { name: '演职员', exact: true }).click();
  await expect(activePanel(page).getByText('林观澜', { exact: true })).toBeVisible();
  await expect(activePanel(page).getByRole('heading', { name: '相似推荐', exact: true })).toBeVisible();
  await screenshot(page, 'people-desktop');
  await page.waitForTimeout(1100);
  await page.getByRole('navigation', { name: '页面分页' }).getByRole('button', { name: '媒体信息', exact: true }).click();
  await expect(activePanel(page).getByRole('heading', { name: '媒体信息', exact: true })).toBeVisible();
  await expect(activePanel(page)).toContainText('VP8');
  await expect(activePanel(page).locator('svg[aria-label="字幕时间轴"] path')).toHaveCount(2);
  await screenshot(page, 'media-desktop');
  await page.getByRole('navigation', { name: '页面分页' }).getByRole('button', { name: '概览', exact: true }).click();
  await expect(activePanel(page)).toHaveAttribute('aria-label', '概览');
  await page.waitForTimeout(1100);
  await page.getByRole('navigation', { name: '页面分页' }).getByRole('button', { name: '媒体信息', exact: true }).click();
  await expect(activePanel(page).locator('svg[aria-label="字幕时间轴"] path')).toHaveCount(2);
});

test('personal preferences persist to supported configuration and display preferences', async ({ page, request }) => {
  await login(page);
  await navigate(page, '个人中心');
  await expect(page.getByRole('heading', { name: 'Moooyo', exact: true })).toBeVisible();
  await page.getByRole('group', { name: '字幕字号', exact: true }).getByRole('button', { name: '特大', exact: true }).click();
  await page.getByRole('group', { name: '字幕样式', exact: true }).getByRole('button', { name: '底框', exact: true }).click();
  await page.getByRole('switch', { name: /^自动播放下一集/ }).click();
  await expect.poll(async () => (await state(request)).user.Configuration.EnableNextEpisodeAutoPlay).toBe(false);
  await expect.poll(async () => JSON.parse((await state(request)).display['goby-player']?.CustomPrefs?.preferences ?? '{}').subtitleStyle).toBe('background');
  await page.reload();
  await expect(page.getByRole('group', { name: '字幕字号', exact: true }).getByRole('button', { name: '特大', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await expect(page.getByRole('switch', { name: /^自动播放下一集/ })).toHaveAttribute('aria-checked', 'false');
  await screenshot(page, 'settings-desktop');
});

test('real video resumes, decodes, seeks, changes speed and reports a single stopped session', async ({ page, request }) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await login(page);
  await page.goto('/#/detail/guitu');
  await expect(activePanel(page).getByRole('heading', { name: '归途', exact: true })).toBeVisible();
  await page.mouse.move(720, 360);
  await page.locator('.play-disc').click();
  const video = page.locator('video.player-video');
  await expect(video).toBeVisible();
  await expect.poll(async () => video.evaluate((element: HTMLVideoElement) => element.currentTime)).toBeGreaterThan(8);
  await expect.poll(async () => video.evaluate((element: HTMLVideoElement) => element.readyState)).toBeGreaterThanOrEqual(2);
  await expect.poll(async () => (await state(request)).events.some((event: { route: string }) => event.route === '/Sessions/Playing')).toBeTruthy();
  await page.mouse.move(650, 810);
  await page.getByRole('button', { name: '暂停', exact: true }).click();
  await expect.poll(async () => video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(true);
  const slider = page.getByRole('slider', { name: '播放进度', exact: true });
  await slider.focus();
  await page.keyboard.press('Home');
  await expect.poll(async () => video.evaluate((element: HTMLVideoElement) => element.currentTime)).toBeLessThan(1);
  await page.keyboard.press('ArrowRight');
  await expect.poll(async () => video.evaluate((element: HTMLVideoElement) => element.currentTime)).toBeGreaterThanOrEqual(9.5);
  await page.getByRole('button', { name: '播放速度', exact: true }).click();
  await page.getByRole('option', { name: '1.5×', exact: true }).click();
  await expect.poll(async () => video.evaluate((element: HTMLVideoElement) => element.playbackRate)).toBe(1.5);
  await page.getByRole('button', { name: '字幕', exact: true }).click();
  await expect(page.getByRole('dialog', { name: '字幕', exact: true })).toBeVisible();
  await expect(page.getByRole('option', { name: '简体中文', exact: true })).toBeVisible();
  await page.getByRole('button', { name: '关闭', exact: true }).click();
  await screenshot(page, 'player-desktop');
  await page.locator('.player-toggle').click();
  await expect.poll(async () => (await state(request)).events.filter((event: { route: string; PositionTicks: number }) => event.route === '/Sessions/Playing/Progress' && event.PositionTicks > 140_000_000).length, { timeout: 15_000 }).toBeGreaterThan(0);
  await page.keyboard.press('Escape');
  await expect(page).toHaveURL(/#\/detail\/guitu$/);
  await expect.poll(async () => (await state(request)).events.filter((event: { route: string }) => event.route === '/Sessions/Playing/Stopped').length).toBe(1);
  const snapshot = await state(request);
  const started = snapshot.events.find((event: { route: string }) => event.route === '/Sessions/Playing');
  expect(snapshot.events.every((event: { PlaySessionId: string }) => event.PlaySessionId === started.PlaySessionId)).toBe(true);
  const negotiation = snapshot.requests.find((entry: { path: string }) => entry.path === '/Items/guitu/PlaybackInfo');
  expect(negotiation.body).toMatchObject({ UserId: 'fixture-viewer', IsPlayback: true, StartTimeTicks: 80_000_000 });
  expect(negotiation.body.DeviceProfile.DirectPlayProfiles.length).toBeGreaterThan(0);
  expect(snapshot.unknownRequests).toEqual([]);
  expect(errors).toEqual([]);
});

test('mobile pages fit the viewport and preserve usable navigation', async ({ page }) => {
  await page.setViewportSize({ width: 393, height: 852 });
  await login(page);
  await noOverflow(page);
  await screenshot(page, 'home-mobile');
  await navigate(page, '电影');
  await expect(page.locator('.poster-card')).toHaveCount(19);
  await noOverflow(page);
  await screenshot(page, 'movies-mobile');
  await navigate(page, '个人中心');
  await expect(page.getByRole('heading', { name: 'Moooyo', exact: true })).toBeVisible();
  await noOverflow(page);
  await screenshot(page, 'settings-mobile');
  await page.goto('/#/detail/deep');
  await expect(activePanel(page).getByRole('heading', { name: '深海回响', exact: true })).toBeVisible();
  await noOverflow(page);
  await screenshot(page, 'detail-mobile');
});

test.describe('small touchscreens', () => {
  test.use({ viewport: { width: 375, height: 667 }, hasTouch: true });

  test('keeps overview controls above the bottom navigation when the content fits', async ({ page }) => {
    await login(page);
    await page.goto('/#/detail/deep');
    await expect(activePanel(page).getByRole('heading', { name: '深海回响', exact: true })).toBeVisible();
    const lastControl = activePanel(page).getByRole('button', { name: '看过', exact: true });
    await expect(lastControl).toBeVisible();
    const lastBounds = await lastControl.boundingBox();
    const navigationBounds = await page.getByRole('navigation', { name: '主导航' }).boundingBox();
    expect(lastBounds!.y + lastBounds!.height).toBeLessThan(navigationBounds!.y);
  });

  test('detail content scrolls before paging and its last control clears the bottom navigation', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 520 });
    await login(page);
    await page.goto('/#/detail/deep');
    await expect(activePanel(page).getByRole('heading', { name: '深海回响', exact: true })).toBeVisible();
    await page.waitForTimeout(1100);
    const scroll = page.locator('.detail-overview-scroll');
    const overflow = await scroll.evaluate((element) => element.scrollHeight - element.clientHeight);
    expect(overflow).toBeGreaterThan(0);
    await scroll.evaluate((element) => { element.scrollTop = (element.scrollHeight - element.clientHeight) / 2; });
    const scrollBefore = await scroll.evaluate(element => element.scrollTop);
    const cdp = await page.context().newCDPSession(page);
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x: 100, y: 300 }] });
    for (const y of [280, 255, 230, 200, 170]) {
      await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: 100, y }] });
      await page.waitForTimeout(20);
    }
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
    await cdp.detach();
    await expect.poll(() => scroll.evaluate(element => element.scrollTop)).toBeGreaterThan(scrollBefore);
    await expect(activePanel(page)).toHaveAttribute('aria-label', '概览');
    await scroll.evaluate((element) => { element.scrollTop = element.scrollHeight; });
    const lastControl = activePanel(page).getByRole('button', { name: '看过', exact: true });
    await expect(lastControl).toBeVisible();
    const lastBounds = await lastControl.boundingBox();
    const navigationBounds = await page.getByRole('navigation', { name: '主导航' }).boundingBox();
    expect(lastBounds).not.toBeNull();
    expect(navigationBounds).not.toBeNull();
    expect(lastBounds!.y + lastBounds!.height).toBeLessThan(navigationBounds!.y);
    await screenshot(page, 'detail-small-mobile-scrolled');
  });
});
