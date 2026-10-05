import { expect, test, type Page } from '@playwright/test';

async function openMovies(page: Page) {
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '电影', exact: true }).click();
  await expect(page.locator('.poster-card').first()).toBeVisible();
}

const poster = (page: Page, name = '归途') => page.locator('.poster-card').filter({ has: page.locator('strong', { hasText: name }) });
const card = (page: Page) => page.locator('.poster-preview.is-visible');

async function hoverPoster(page: Page, name = '归途') {
  const target = poster(page, name);
  await target.scrollIntoViewIfNeeded();
  // Complete the viewport scroll before beginning the intentional hover dwell.
  await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
  await target.hover();
}

test.beforeEach(async ({ request }) => { await request.post('/__fixture/reset'); });

test('delays the preview, enriches real metadata, and never negotiates playback', async ({ page, request }) => {
  let detailRequests = 0;
  await page.route('**/emby/Users/fixture-viewer/Items/guitu?*', async route => {
    detailRequests += 1;
    const response = await route.fetch();
    const item = await response.json();
    await route.fulfill({ json: { ...item, Overview: '完整条目提供的预览简介。', BackdropImageTags: ['first-still', 'second-still'] } });
  });
  await openMovies(page);
  await hoverPoster(page);
  await page.waitForTimeout(350);
  await expect(card(page)).toHaveCount(0);
  expect(detailRequests).toBe(0);
  await expect(card(page)).toBeVisible();
  await expect(card(page).locator('.poster-preview-overview')).toHaveText('完整条目提供的预览简介。');
  await expect(card(page).locator('.poster-preview-image-link img')).toHaveCount(2);
  const images = await card(page).locator('.poster-preview-image-link img').evaluateAll(elements => elements.map(element => (element as HTMLImageElement).src));
  expect(images.map(value => new URL(value).pathname)).toEqual(['/emby/Items/guitu/Images/Backdrop/0', '/emby/Items/guitu/Images/Backdrop/1']);
  expect(detailRequests).toBe(1);
  const rectangle = await card(page).boundingBox();
  expect(rectangle!.x).toBeGreaterThanOrEqual(12);
  expect(rectangle!.y).toBeGreaterThanOrEqual(12);
  expect(rectangle!.x + rectangle!.width).toBeLessThanOrEqual(1428);
  expect(rectangle!.y + rectangle!.height).toBeLessThanOrEqual(888);
  const state = await (await request.get('/__fixture/state')).json();
  expect(state.events).toEqual([]);
  expect(state.requests.filter((entry: { path: string }) => /PlaybackInfo|ActiveEncodings|\/Sessions\/Playing|\/Generate|\/Tasks/.test(entry.path))).toEqual([]);
});

test('aborts delayed metadata on navigation and dismisses on external scroll and resize', async ({ page }) => {
  let release: (() => void) | undefined;
  let started = false;
  await page.route('**/emby/Users/fixture-viewer/Items/guitu?*', async route => {
    const response = await route.fetch();
    const item = await response.json();
    started = true;
    await new Promise<void>(resolve => { release = resolve; });
    await route.fulfill({ json: { ...item, Name: '过期响应不应显示' } }).catch(() => undefined);
  });
  await openMovies(page);
  await hoverPoster(page);
  await expect(card(page)).toBeVisible();
  await expect.poll(() => started).toBe(true);
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '收藏', exact: true }).click();
  release?.();
  await expect(page.locator('.poster-preview')).toHaveCount(0);
  await expect(page.getByText('过期响应不应显示', { exact: true })).toHaveCount(0);
  await hoverPoster(page, '最后的灯塔');
  await expect(card(page)).toBeVisible();
  await page.evaluate(() => window.dispatchEvent(new Event('scroll')));
  await expect(page.locator('.poster-preview')).toHaveCount(0);
  await page.mouse.move(0, 0);
  await hoverPoster(page, '最后的灯塔');
  await expect(card(page)).toBeVisible();
  await page.setViewportSize({ width: 1280, height: 800 });
  await expect(page.locator('.poster-preview')).toHaveCount(0);
});

test('opens from keyboard focus and Escape restores the trigger without reopening', async ({ page }) => {
  await openMovies(page);
  await page.keyboard.press('Tab');
  await poster(page).focus();
  await expect(card(page)).toBeVisible();
  await expect(card(page).locator('.poster-preview-play')).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(page.locator('.poster-preview')).toHaveCount(0);
  await expect(poster(page)).toBeFocused();
  await page.waitForTimeout(750);
  await expect(page.locator('.poster-preview')).toHaveCount(0);
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '电影', exact: true }).focus();
  await poster(page).focus();
  await expect(card(page)).toBeVisible();
});

test('preserves favorite state on failure and updates only after the server succeeds', async ({ page, request }) => {
  await openMovies(page);
  await request.post('/__fixture/config', { data: { failures: { '/Users/fixture-viewer/FavoriteItems/guitu': 503 } } });
  await hoverPoster(page);
  await expect(card(page)).toBeVisible();
  await card(page).getByRole('button', { name: '收藏', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('收藏状态保存失败');
  await expect(card(page).getByRole('button', { name: '收藏', exact: true })).toHaveAttribute('aria-pressed', 'false');
  await request.post('/__fixture/config', { data: { failures: {} } });
  await card(page).getByRole('button', { name: '收藏', exact: true }).click();
  await expect.poll(async () => (await (await request.get('/__fixture/state')).json()).items.find((item: { Id: string }) => item.Id === 'guitu').UserData.IsFavorite).toBe(true);
  await expect(card(page).getByRole('button', { name: '取消收藏', exact: true })).toHaveAttribute('aria-pressed', 'true');
});

test('does not open a preview for touch input or a narrow viewport', async ({ page }) => {
  await openMovies(page);
  await poster(page).dispatchEvent('pointerover', { pointerType: 'touch', bubbles: true });
  await page.waitForTimeout(750);
  await expect(page.locator('.poster-preview')).toHaveCount(0);
  await page.setViewportSize({ width: 600, height: 800 });
  await hoverPoster(page);
  await page.waitForTimeout(750);
  await expect(page.locator('.poster-preview')).toHaveCount(0);
});
