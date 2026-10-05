import { expect, test, type Locator, type Page } from '@playwright/test';

async function signIn(page: Page) {
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
}

async function openEpisode(page: Page) {
  await page.goto('/#/player/deep-2-5');
  await page.waitForFunction(() => {
    const video = document.querySelector<HTMLVideoElement>('.player-video');
    return video && video.readyState >= 2 && !video.paused;
  });
  await page.locator('.player-video').evaluate((video: HTMLVideoElement) => video.pause());
}

async function moveInside(page: Page, locator: Locator) {
  const bounds = (await locator.boundingBox())!;
  await page.mouse.move(bounds.x + bounds.width / 2, Math.min(page.viewportSize()!.height - 8, Math.max(8, bounds.y + bounds.height / 2)));
}

test.beforeEach(async ({ page, request }) => {
  await request.post('/__fixture/reset');
  await signIn(page);
});

test('keeps vertical and zoom wheels out of the episode rail while horizontal gestures and arrows still work', async ({ page }) => {
  await openEpisode(page);
  await page.getByRole('button', { name: '选集', exact: true }).click();
  const row = page.locator('.player-episodes-row');
  await expect(row.locator('[data-current="true"]')).toBeVisible();
  await expect.poll(() => row.evaluate(element => element.scrollLeft)).toBeGreaterThan(100);
  const initial = await row.evaluate(element => element.scrollLeft);
  await moveInside(page, row);
  await page.mouse.wheel(-240, 0);
  await expect.poll(() => row.evaluate(element => element.scrollLeft)).toBeLessThan(initial - 100);
  await page.waitForTimeout(200);
  const before = await row.evaluate(element => element.scrollLeft);
  await page.mouse.wheel(0, 240);
  await page.waitForTimeout(200);
  expect(await row.evaluate(element => element.scrollLeft)).toBeCloseTo(before, 0);
  const zoom = await row.evaluate(element => {
    const left = element.scrollLeft;
    const event = new WheelEvent('wheel', { bubbles: true, cancelable: true, ctrlKey: true, deltaY: 120 });
    const defaultAllowed = element.dispatchEvent(event);
    return { defaultAllowed, moved: element.scrollLeft - left };
  });
  expect(zoom).toEqual({ defaultAllowed: true, moved: 0 });
  await page.getByRole('button', { name: '向后浏览剧集', exact: true }).click();
  await expect.poll(() => row.evaluate(element => element.scrollLeft)).toBeGreaterThan(before + 100);
  await page.keyboard.press('Escape');
  await expect(page.getByRole('button', { name: '选集', exact: true })).toBeFocused();
});

test('lets a short episode drawer scroll vertically from its rail without losing its heading or card details', async ({ page }) => {
  await page.route('**/emby/Shows/deep/Seasons?*', route => route.fulfill({ json: {
    Items: Array.from({ length: 30 }, (_, index) => ({ Id: `deep-season-${index + 1}`, Name: `Season ${index + 1}`, Type: 'Season', SeriesId: 'deep', IndexNumber: index + 1 })), TotalRecordCount: 30,
  } }));
  await page.setViewportSize({ width: 390, height: 220 });
  await openEpisode(page);
  await page.getByRole('button', { name: '选集', exact: true }).click();
  const content = page.locator('.player-episodes-content');
  const row = page.locator('.player-episodes-row');
  await expect(row.locator('[data-current="true"]')).toBeVisible();
  expect(await content.evaluate(element => element.scrollHeight - element.clientHeight)).toBeGreaterThan(10);
  expect((await content.boundingBox())!.y).toBeGreaterThanOrEqual(0);
  await moveInside(page, row);
  await page.mouse.wheel(0, -1000);
  await expect.poll(() => content.evaluate(element => element.scrollTop)).toBe(0);
  await expect(page.getByRole('button', { name: '关闭选集（Esc）', exact: true })).toBeInViewport();
  const left = await row.evaluate(element => element.scrollLeft);
  await moveInside(page, row);
  await page.mouse.wheel(0, 1000);
  await expect.poll(() => content.evaluate(element => element.scrollHeight - element.clientHeight - element.scrollTop)).toBeLessThanOrEqual(1);
  expect(await row.evaluate(element => element.scrollLeft)).toBeCloseTo(left, 0);
  const title = await row.locator('[data-current="true"] .player-episode-title').boundingBox();
  expect(title!.y + title!.height).toBeLessThanOrEqual(220);
  await page.mouse.wheel(0, -1000);
  await expect.poll(() => content.evaluate(element => element.scrollTop)).toBe(0);
  await page.getByRole('button', { name: '关闭选集（Esc）', exact: true }).click();
  await expect(page.getByRole('dialog', { name: '选集' })).toHaveCount(0);
});

test('fits long playback menus inside a short viewport and scrolls to the last option without moving the page', async ({ page }) => {
  await page.route('**/emby/Items/deep-2-5/PlaybackInfo*', async route => {
    const response = await route.fetch();
    const result = await response.json();
    result.MediaSources[0].MediaStreams.push(...Array.from({ length: 24 }, (_, index) => ({ Index: 10 + index, Type: 'Audio', Codec: 'opus', Language: 'eng', DisplayTitle: `Additional audio ${index + 1}`, Channels: 2 })));
    await route.fulfill({ response, json: result });
  });
  await page.setViewportSize({ width: 840, height: 240 });
  await openEpisode(page);
  await page.getByRole('button', { name: '音轨', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '音轨', exact: true });
  const options = dialog.getByRole('listbox');
  const bounds = (await dialog.boundingBox())!;
  expect(bounds.y).toBeGreaterThanOrEqual(11);
  expect(bounds.y + bounds.height).toBeLessThan(240);
  await expect(dialog.getByRole('button', { name: '关闭', exact: true })).toBeInViewport();
  await moveInside(page, options);
  await page.mouse.wheel(0, 10000);
  await expect.poll(() => options.evaluate(element => element.scrollHeight - element.clientHeight - element.scrollTop)).toBeLessThanOrEqual(1);
  const last = await options.getByRole('option').last().boundingBox();
  expect(last!.y).toBeGreaterThanOrEqual(bounds.y);
  expect(last!.y + last!.height).toBeLessThanOrEqual(bounds.y + bounds.height);
  await page.mouse.wheel(0, 1000);
  expect(await page.evaluate(() => window.scrollY)).toBe(0);
  await expect(page).toHaveURL(/#\/player\/deep-2-5$/);
  await page.keyboard.press('Escape');
  await expect(page.getByRole('button', { name: '音轨', exact: true })).toBeFocused();
});
